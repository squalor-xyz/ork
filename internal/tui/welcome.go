// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/projscan"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

type welcomeState struct {
	editing bool
	field   textField
	confirm string
	pending bool
	scan    *scanState
}

// scanState is the add checklist (S302, *proposed*): the projects found below
// a folder, which of them are ticked, and after the add what each did.
type scanState struct {
	root    string
	rows    []projscan.Row
	ticked  []bool
	cursor  int
	top     int    // first row drawn
	note    string // where a scan that quit at a limit stopped
	done    bool
	results []projscan.Outcome
}

// scanAddedMsg is the result of adding the ticked rows.
type scanAddedMsg struct{ outcomes []projscan.Outcome }

type projectAddedMsg struct {
	project registry.Project
	err     error
}

func (m *Model) welcomeKey(msg tea.KeyPressMsg) tea.Cmd {
	w := &m.welcome
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	if w.pending {
		return nil
	}
	if w.scan != nil {
		return m.scanKey(msg)
	}
	if w.confirm != "" {
		switch msg.String() {
		case "y", "Y":
			path := w.confirm
			w.confirm = ""
			live := m.cfg.Live
			if live == nil {
				m.actionStatus = "No daemon connection; action unavailable"
				return nil
			}
			w.pending = true
			m.actionStatus = "Adding project..."
			return func() tea.Msg {
				p, err := live.ProjectsAdd(context.Background(), path)
				return projectAddedMsg{project: p, err: err}
			}
		case "enter", "n", "N", "esc":
			w.confirm = ""
			m.actionStatus = "Cancelled"
		}
		return nil
	}
	if w.editing {
		switch msg.String() {
		case "esc":
			w.editing = false
			m.actionStatus = ""
		case "enter":
			path := strings.TrimSpace(w.field.Value())
			if path == "" {
				var err error
				path, err = m.cfg.Getwd()
				if err != nil {
					m.actionStatus = actionError(err)
					return nil
				}
				if !hasProjectMarker(path) {
					// A folder that holds projects is not itself one: offer them.
					if m.openScan(path) {
						return nil
					}
					m.actionStatus = path + " holds neither .slicer nor .adjudicate.json; type a path"
					return nil
				}
			}
			if path == "~" || strings.HasPrefix(path, "~/") {
				home, err := os.UserHomeDir()
				if err != nil {
					m.actionStatus = actionError(err)
					return nil
				}
				path = filepath.Join(home, strings.TrimPrefix(path, "~"))
			}
			if m.openScan(path) {
				return nil
			}
			w.confirm = path
			m.actionStatus = ""
		default:
			w.field.update(msg)
			m.actionStatus = ""
		}
		return nil
	}
	switch msg.String() {
	case "+":
		m.showHelp = false
		w.editing = true
		m.actionStatus = ""
	case "q":
		return tea.Quit
	case "?":
		m.showHelp = !m.showHelp
	case "esc":
		m.showHelp = false
		m.actionStatus = ""
	}
	return nil
}

// openScan opens the checklist when path holds projects below it. It leaves
// the single-add confirmation to the caller when path is itself a project,
// holds none, or cannot be scanned (the daemon then refuses a bad path with
// its own message).
func (m *Model) openScan(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	found, err := m.cfg.Scan(path)
	cands := found.Candidates
	if err != nil || len(cands) == 0 || (len(cands) == 1 && filepath.Clean(cands[0].Path) == filepath.Clean(path)) {
		return false
	}
	var registered []registry.Project
	for _, p := range m.projects {
		if !p.snap.Unavailable {
			registered = append(registered, p.reg)
		}
	}
	s := &scanState{root: filepath.Clean(path), rows: projscan.Classify(cands, registered), note: found.Note()}
	for _, r := range s.rows {
		s.ticked = append(s.ticked, r.Preselected())
	}
	m.welcome.scan = s
	m.actionStatus = ""
	return true
}

func (s *scanState) tickedCount() (ticked, tickable int) {
	for i, r := range s.rows {
		if !r.Blocked() {
			tickable++
		}
		if s.ticked[i] {
			ticked++
		}
	}
	return ticked, tickable
}

func (m *Model) scanKey(msg tea.KeyPressMsg) tea.Cmd {
	w := &m.welcome
	s := w.scan
	if s.done {
		switch msg.String() {
		case "enter", "esc":
			*w = welcomeState{}
		}
		return nil
	}
	switch msg.String() {
	case "j", "down":
		s.cursor = min(s.cursor+1, len(s.rows)-1)
	case "k", "up":
		s.cursor = max(s.cursor-1, 0)
	case " ", "space":
		if !s.rows[s.cursor].Blocked() {
			s.ticked[s.cursor] = !s.ticked[s.cursor]
		}
	case "a":
		for i, r := range s.rows {
			s.ticked[i] = !r.Blocked()
		}
	case "n":
		for i := range s.ticked {
			s.ticked[i] = false
		}
	case "esc":
		w.scan = nil
		m.actionStatus = ""
	case "y", "Y": // Enter adds nothing here (owner, 2026-11-07)
		var picked []projscan.Candidate
		for i, r := range s.rows {
			if s.ticked[i] && !r.Blocked() {
				picked = append(picked, r.Candidate)
			}
		}
		if len(picked) == 0 {
			m.actionStatus = "Nothing ticked"
			return nil
		}
		live := m.cfg.Live
		if live == nil {
			m.actionStatus = "No daemon connection; action unavailable"
			return nil
		}
		w.pending = true
		m.actionStatus = ""
		return func() tea.Msg {
			out := projscan.AddAll(context.Background(), picked, func(ctx context.Context, c projscan.Candidate) (registry.Project, error) {
				return live.ProjectsAdd(ctx, c.Path)
			})
			return scanAddedMsg{outcomes: out}
		}
	}
	m.actionStatus = ""
	return nil
}

func (m *Model) applyScanAdded(msg scanAddedMsg) tea.Cmd {
	w := &m.welcome
	w.pending = false
	var cmds []tea.Cmd
	added := 0
	for _, o := range msg.outcomes {
		if o.Err != nil {
			continue
		}
		added++
		if m.registerProject(o.Project) {
			cmds = append(cmds, m.fetch(o.Project.Name, false))
		}
	}
	m.actionStatus = scanSummary(added, len(msg.outcomes))
	if w.scan != nil {
		w.scan.done = true
		w.scan.results = msg.outcomes
	}
	if added > 0 {
		cmds = append(cmds, m.focus())
	}
	return tea.Batch(cmds...)
}

func scanSummary(added, total int) string {
	word := "projects"
	if total == 1 {
		word = "project"
	}
	if added == total {
		return fmt.Sprintf("Added %d %s", added, word)
	}
	return fmt.Sprintf("Added %d of %d %s", added, total, word)
}

// plainOne is a name, path or error as one plain line: no escape sequence, no
// line break, so it cannot move anything else on the screen.
func plainOne(s string) string {
	return strings.Join(strings.Fields(runrec.PlainText(s)), " ")
}

// hang wraps text to room columns after a label of the label's width, and
// indents the lines after the first under the text.
func hang(label, text string, room int) []string {
	pad := lipgloss.Width(label)
	parts := wrapText(text, max(room-pad, 1))
	for i := range parts {
		if i == 0 {
			parts[i] = label + parts[i]
		} else {
			parts[i] = strings.Repeat(" ", pad) + parts[i]
		}
	}
	return parts
}

// scanBody draws the checklist, or after the add what each row did. height
// is the room the body has; 0 means no limit.
func (m *Model) scanBody(width, height int) []string {
	th := m.cfg.Theme
	s := m.welcome.scan
	room := max(width-len(indent), 1)
	lines := []string{indent + th.Heading.Render("Add projects"), ""}
	if s.done {
		lines = append(lines, indent+scanSummary(countAdded(s.results), len(s.results)), "")
		for _, o := range s.results {
			if o.Err != nil {
				parts := hang("failed  ", plainOne(o.Name)+": "+plainOne(actionError(o.Err)), room)
				parts[0] = th.Error.Render("failed") + strings.TrimPrefix(parts[0], "failed")
				for _, line := range parts {
					lines = append(lines, indent+line)
				}
				continue
			}
			for _, line := range hang("added   ", plainOne(o.Name)+"  "+plainOne(o.Path), room) {
				lines = append(lines, indent+line)
			}
		}
		return lines
	}
	word := "projects"
	if len(s.rows) == 1 {
		word = "project"
	}
	for _, line := range wrapText(fmt.Sprintf("Found %d %s under %s:", len(s.rows), word, plainOne(s.root)), room) {
		lines = append(lines, indent+line)
	}
	if s.note != "" {
		for _, line := range wrapText(strings.ToUpper(s.note[:1])+s.note[1:]+"; scan a narrower folder to see more.", room) {
			lines = append(lines, indent+th.Dim.Render(line))
		}
	}
	lines = append(lines, "")

	names, tags := make([]string, len(s.rows)), make([]string, len(s.rows))
	nameW, tagW := 0, 0
	for i, r := range s.rows {
		names[i], tags[i] = plainOne(r.Name), plainOne(r.Tag())
		nameW = max(nameW, lipgloss.Width(names[i]))
		tagW = max(tagW, lipgloss.Width(tags[i]))
	}
	nameW = min(nameW, 18)
	var rowLines [][]string
	for i, r := range s.rows {
		cursor, box := "  ", "[ ]"
		if i == s.cursor {
			cursor = "> "
		}
		switch {
		case r.Blocked():
			box = "[-]"
		case s.ticked[i]:
			box = "[x]"
		}
		rel, err := filepath.Rel(s.root, r.Path)
		if err != nil {
			rel = r.Path
		}
		head := cursor + box + " " + padRight(clip(names[i], nameW), nameW) + "  " + padRight(tags[i], tagW) + "  "
		text := head + clip(plainOne(rel), max(room-lipgloss.Width(head), 4))
		text = padRight(text, room)
		var line string
		switch {
		case i == s.cursor && r.Blocked():
			// The bar is reversed text, as on Waves; the dim text keeps its mark.
			line = th.TabActive.Render(th.Dim.Render(strings.TrimRight(text, " "))) + th.TabActive.Render(strings.Repeat(" ", room-lipgloss.Width(strings.TrimRight(text, " "))))
		case i == s.cursor:
			line = th.TabActive.Render(text)
		case r.Blocked():
			line = th.Dim.Render(strings.TrimRight(text, " "))
		default:
			line = strings.TrimRight(text, " ")
		}
		block := []string{indent + line}
		if r.Blocked() {
			for _, l := range wrapText(plainOne(r.Reason), max(room-6, 1)) {
				block = append(block, indent+th.Dim.Render("      "+l))
			}
		}
		rowLines = append(rowLines, block)
	}

	ticked, tickable := s.tickedCount()
	footer := []string{"", indent + th.Dim.Render(fmt.Sprintf("%d of %d ticked", ticked, tickable))}
	if m.actionStatus != "" {
		for _, line := range wrapText(plainOne(m.actionStatus), room) {
			footer = append(footer, indent+line)
		}
	}
	if m.welcome.pending {
		footer = append(footer, indent+"Adding projects...")
	}

	avail := 1 << 30
	if height > 0 {
		avail = max(height-len(lines)-len(footer)-1, 1)
	}
	s.cursor = min(max(s.cursor, 0), len(s.rows)-1)
	s.top = min(s.top, s.cursor)
	for used(rowLines[s.top:s.cursor+1]) > avail && s.top < s.cursor {
		s.top++
	}
	end, n := s.top, 0
	for end < len(rowLines) && (end == s.top || n+len(rowLines[end]) <= avail) {
		n += len(rowLines[end])
		end++
	}
	for _, block := range rowLines[s.top:end] {
		lines = append(lines, block...)
	}
	if s.top > 0 || end < len(rowLines) {
		footer[1] += th.Dim.Render(fmt.Sprintf("  (rows %d-%d of %d)", s.top+1, end, len(rowLines)))
	}
	return append(lines, footer...)
}

func padRight(s string, n int) string {
	if gap := n - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func countAdded(out []projscan.Outcome) int {
	n := 0
	for _, o := range out {
		if o.Err == nil {
			n++
		}
	}
	return n
}

func used(blocks [][]string) int {
	n := 0
	for _, b := range blocks {
		n += len(b)
	}
	return n
}

func hasProjectMarker(path string) bool {
	if info, err := os.Stat(filepath.Join(path, ".slicer")); err == nil && info.IsDir() {
		return true
	}
	info, err := os.Stat(filepath.Join(path, ".adjudicate.json"))
	return err == nil && info.Mode().IsRegular()
}

func (m *Model) welcomeBody(width int) []string {
	th := m.cfg.Theme
	lines := []string{indent + th.Heading.Render("Add project")}
	if m.registeredProjectCount() == 0 {
		lines = []string{indent + th.Heading.Render("Welcome"), "", indent + "No projects yet."}
	}
	if m.welcome.scan != nil {
		return m.scanBody(width, 0)
	}
	if m.welcome.editing {
		value := m.welcome.field.view(max(width-len(indent)-14, 1), th)
		if m.welcome.confirm != "" || m.welcome.pending {
			value = th.Title.Render(m.welcome.field.Value())
		}
		lines = append(lines, "", indent+"Project path: "+value)
		status := m.actionStatus
		if m.welcome.confirm != "" {
			status = "ork projects add " + shellQuote(m.welcome.confirm) + "? [y/N] (Enter is No)"
		}
		if status != "" {
			for _, line := range wrapText(status, max(width-len(indent), 1)) {
				lines = append(lines, indent+line)
			}
		}
		for _, line := range wrapText("Type an absolute path to a project, or to a folder to pick projects from it. Leave it empty and press Enter to use the current directory.", max(width-len(indent), 1)) {
			lines = append(lines, indent+th.Dim.Render(line))
		}
	} else if m.actionStatus != "" {
		lines = append(lines, "")
		for _, line := range wrapText(m.actionStatus, max(width-len(indent), 1)) {
			lines = append(lines, indent+line)
		}
	}
	return lines
}

func (m *Model) applyProjectAdded(msg projectAddedMsg) tea.Cmd {
	m.welcome.pending = false
	if msg.err != nil {
		m.actionStatus = actionError(msg.err)
		return nil
	}
	m.registerProject(msg.project)
	p := m.byName[msg.project.Name]
	m.welcome = welcomeState{}
	m.actionStatus = fmt.Sprintf("Added %s %s", p.reg.Name, p.reg.Path)
	return tea.Batch(m.fetch(p.reg.Name, false), m.focus())
}

// projectReader uses the daemon's existing registry listing without requiring
// offline sources to supply one.
type projectReader interface {
	Projects(context.Context) ([]registry.Project, error)
}

type projectsDiscoveredMsg struct {
	revision uint64
	projects []registry.Project
	err      error
}

func (m *Model) discoverProjects() tea.Cmd {
	reader, ok := m.cfg.Live.(projectReader)
	if !ok || m.projectsReading {
		return nil
	}
	m.projectsReading = true
	revision := m.projectsRevision
	return func() tea.Msg {
		projects, err := reader.Projects(context.Background())
		return projectsDiscoveredMsg{projects: projects, err: err, revision: revision}
	}
}

func (m *Model) registerProject(project registry.Project) bool {
	old := m.byName[project.Name]
	if old != nil && !old.snap.Unavailable {
		return false
	}
	p := &projectState{reg: project}
	if old == nil {
		m.projects = append(m.projects, p)
	} else {
		for i, current := range m.projects {
			if current == old {
				m.projects[i] = p
				break
			}
		}
	}
	m.byName[project.Name] = p
	m.poller.Add(project.Name)
	return true
}

func (m *Model) applyProjectsDiscovered(msg projectsDiscoveredMsg) tea.Cmd {
	m.projectsReading = false
	if msg.revision != m.projectsRevision {
		return nil
	}
	if msg.err != nil {
		return nil
	}
	wasWelcome := m.registeredProjectCount() == 0
	var cmds []tea.Cmd
	var found string
	for _, project := range msg.projects {
		if m.registerProject(project) {
			if found == "" {
				found = fmt.Sprintf("Found %s %s", project.Name, project.Path)
			}
			cmds = append(cmds, m.fetch(project.Name, false))
		}
	}
	if len(cmds) > 0 {
		if wasWelcome {
			m.welcome = welcomeState{}
			m.showHelp = false
			m.actionStatus = found
		}
		cmds = append(cmds, m.focus())
	}
	return tea.Batch(cmds...)
}
