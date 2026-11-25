// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// settingsClient is optional for Live test doubles, as matrixEditor is (S251,
// *proposed*): the read of every setting of a project, and its setup steps.
type settingsClient interface {
	ProjectsShow(ctx context.Context, project string) (*daemon.ProjectsShowResult, error)
	ProjectSetup(ctx context.Context, p daemon.ProjectSetupParams) (*daemon.ProjectSetupResult, error)
}

var _ settingsClient = (*Daemon)(nil)

// The setup steps of project.setup (S249), in the order the chooser lists them.
const (
	stepSlicerInit     = "slicer-init"
	stepRelayRolesInit = "relay-roles-init"
	stepAdjInit        = "adj-init"
	stepAdjOff         = "adj-off"
)

// settingsState is the project settings pane: a read of projects.show, and
// the setup step the owner is choosing, confirming or running.
type settingsState struct {
	project string
	request uint64 // the projects.show call the pane waits for or shows
	pending bool   // a read is out; the last result stays on screen
	result  *daemon.ProjectsShowResult
	err     error
	offset  int // the first line shown below the heading
	room    int // the lines of the scrolling part the last draw had

	choosing   bool     // the one-line chooser of setup steps is open
	steps      []string // the steps the chooser lists
	pick       int
	call       uint64 // the project.setup call that is out, or 0
	step       string // the step whose command waits for y, or runs
	command    string // the command the daemon showed for the step
	confirming bool
}

// settingsMsg is the daemon's reply to a projects.show read.
type settingsMsg struct {
	project string
	request uint64
	result  *daemon.ProjectsShowResult
	err     error
}

// settingsSetupMsg is the daemon's reply to a preview or a run of project.setup.
type settingsSetupMsg struct {
	project string
	request uint64
	step    string
	run     bool
	result  *daemon.ProjectSetupResult
	err     error
}

// settingsRefusal is why S does not open the settings pane: the status line
// text, or "" for a silent refusal. The key refuses by it and the hint line
// lists S only when it does not.
func (m *Model) settingsRefusal() (status string, refused bool) {
	switch {
	case m.showHelp:
		return "", true
	case m.active < 0:
		return "Select a project to open its settings", true
	case m.projects[m.active].snap.Unavailable:
		return "Select a registered project to open its settings", true
	case m.cfg.Live == nil:
		return "No daemon connection; action unavailable", true
	}
	if _, ok := m.cfg.Live.(settingsClient); !ok {
		return "Project settings unavailable", true
	}
	return "", false
}

// openSettings opens the pane for the active project. The read runs in the
// command, as the matrix pane's does.
func (m *Model) openSettings() tea.Cmd {
	if status, refused := m.settingsRefusal(); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	s := &settingsState{project: m.projects[m.active].reg.Name}
	m.settings = s
	m.actionStatus = ""
	return m.readSettings(s)
}

// readSettings asks for projects.show again. The pane keeps what it shows
// until the reply comes.
func (m *Model) readSettings(s *settingsState) tea.Cmd {
	client, ok := m.cfg.Live.(settingsClient)
	if !ok {
		m.actionStatus = "Project settings unavailable"
		return nil
	}
	m.settingsRequest++
	s.request, s.pending = m.settingsRequest, true
	project, request := s.project, s.request
	return func() tea.Msg {
		result, err := client.ProjectsShow(context.Background(), project)
		return settingsMsg{project: project, request: request, result: result, err: err}
	}
}

// applySettings takes the reply of the read the pane waits for; a reply of an
// earlier read, or of a closed pane, changes nothing.
func (m *Model) applySettings(msg settingsMsg) {
	s := m.settings
	if s == nil || s.project != msg.project || s.request != msg.request || !s.pending {
		return
	}
	s.pending = false
	s.result, s.err = msg.result, msg.err
	if msg.err == nil && msg.result == nil {
		s.err = fmt.Errorf("projects.show returned no settings")
	}
}

// stepsApply is the setup steps that apply to what the read returned: slicer-init
// when slicer has no project there, relay-roles-init when it has no relay
// roles file, and the adj step for the registry flag's other value.
func (s *settingsState) stepsApply() []string {
	r := s.result
	if r == nil {
		return nil
	}
	var out []string
	for _, e := range r.Slicer.Errors {
		switch {
		case e.Part == "config" && e.Code == "state" && strings.HasPrefix(e.Message, "no .slicer/ found"):
			out = append(out, stepSlicerInit)
		case e.Part == "matrix" && e.Code == "no_relay_roles":
			out = append(out, stepRelayRolesInit)
		}
	}
	if r.Adj.Enabled {
		return append(out, stepAdjOff)
	}
	return append(out, stepAdjInit)
}

// settingsKey takes every key while the pane is open, as removeProjectKey does.
// A setup step goes through the chooser, a preview that shows its command, and
// y; n, Enter and esc never run it.
func (m *Model) settingsKey(msg tea.KeyPressMsg) tea.Cmd {
	s, k := m.settings, m.keys
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	switch {
	case s.call != 0:
		// The call is out. esc closes the pane and the reply is dropped.
		if key.Matches(msg, k.Close) {
			m.closeSettings()
		}
		return nil
	case s.confirming:
		switch {
		case key.Matches(msg, k.Yes):
			return m.callSetup(s, s.step, true)
		case key.Matches(msg, k.No):
			s.confirming, s.step, s.command = false, "", ""
			m.actionStatus = "Cancelled"
		}
		return nil
	case s.choosing:
		switch {
		case key.Matches(msg, k.TabNext):
			s.pick = (s.pick + 1) % len(s.steps)
		case key.Matches(msg, k.TabPrev):
			s.pick = (s.pick + len(s.steps) - 1) % len(s.steps)
		case key.Matches(msg, k.Select):
			s.choosing = false
			return m.callSetup(s, s.steps[s.pick], false)
		case key.Matches(msg, k.Close):
			s.choosing = false
			m.actionStatus = ""
		}
		return nil
	}
	switch {
	case key.Matches(msg, k.Close):
		m.closeSettings()
	case key.Matches(msg, k.SettingsSetup):
		if s.pending || s.result == nil {
			return nil
		}
		s.steps = s.stepsApply()
		if len(s.steps) == 0 {
			m.actionStatus = "No setup step applies"
			return nil
		}
		s.choosing, s.pick = true, 0
		m.actionStatus = ""
	case key.Matches(msg, k.Matrix):
		return m.settingsMatrix()
	case key.Matches(msg, k.First):
		s.offset = 0
	case key.Matches(msg, k.Last):
		s.offset = 1 << 30
	case key.Matches(msg, k.RowNext):
		s.offset++
	case key.Matches(msg, k.RowPrev):
		s.offset = max(s.offset-1, 0)
	case key.Matches(msg, k.PageDown):
		s.offset += max(s.room, 1)
	case key.Matches(msg, k.PageUp):
		s.offset = max(s.offset-max(s.room, 1), 0)
	}
	return nil
}

func (m *Model) closeSettings() {
	m.settings = nil
	m.actionStatus = ""
}

// settingsMatrix closes the pane and opens the Roadmap's matrix pane of the
// project.
func (m *Model) settingsMatrix() tea.Cmd {
	m.closeSettings()
	if m.view != roadmapView {
		return tea.Batch(m.showView(roadmapView), m.openMatrix())
	}
	m.itemPane, m.specDraft = nil, nil
	return m.openMatrix()
}

// callSetup is the command of one project.setup call: the preview that shows
// the command, or the run, which only y starts.
func (m *Model) callSetup(s *settingsState, step string, run bool) tea.Cmd {
	client, ok := m.cfg.Live.(settingsClient)
	if !ok {
		m.actionStatus = "Project settings unavailable"
		return nil
	}
	m.settingsRequest++
	s.call, s.step = m.settingsRequest, step
	m.actionStatus = ""
	params := daemon.ProjectSetupParams{Project: s.project, Step: step, Preview: !run}
	project, request := s.project, s.call
	return func() tea.Msg {
		result, err := client.ProjectSetup(context.Background(), params)
		return settingsSetupMsg{project: project, request: request, step: step, run: run, result: result, err: err}
	}
}

// applySettingsSetup takes the reply of the call that is out. A preview waits
// for y; a run reads the settings again. A refusal shows as it came, and the
// pane reads nothing again.
func (m *Model) applySettingsSetup(msg settingsSetupMsg) tea.Cmd {
	s := m.settings
	if s == nil || s.project != msg.project || s.call == 0 || s.call != msg.request {
		return nil
	}
	s.call = 0
	switch {
	case msg.err != nil:
		s.confirming, s.step, s.command = false, "", ""
		m.actionStatus = actionError(msg.err)
		return nil
	case msg.result == nil || msg.result.Command == "":
		s.confirming, s.step, s.command = false, "", ""
		m.actionStatus = "project.setup returned no command"
		return nil
	case !msg.run:
		s.confirming, s.command = true, msg.result.Command
		return nil
	}
	s.confirming, s.step, s.command = false, "", ""
	m.actionStatus = "Ran: " + msg.result.Command
	return m.readSettings(s)
}

// settingsHints is the hint line of the pane.
func (m *Model) settingsHints(l *hintList) {
	k, s := m.keys, m.settings
	switch {
	case s.call != 0:
		l.add(dropPinned, short(k.Quit, "ctrl+c", "quit"), short(k.Close, "esc", "close"))
	case s.confirming:
		l.add(dropRow, short(k.Yes, "y", "run"))
		l.add(dropPinned, short(k.No, "n", "cancel"))
	case s.choosing:
		l.add(dropRow, offer(len(s.steps) > 1, k.TabNext, "left/right", "step"))
		l.add(dropRow, short(k.Select, "enter", "preview"))
		l.add(dropPinned, short(k.Close, "esc", "cancel"))
	default:
		l.add(dropRow, offer(s.result != nil && !s.pending, k.SettingsSetup, "i", "setup"))
		l.add(dropRow, short(k.Matrix, "m", "matrix"))
		l.add(dropPage, short(k.PageDown, "pgup/pgdown", "page"))
		l.add(dropLine, short(k.RowNext, "j/k", "line"))
		l.add(dropPinned, short(k.Close, "esc", "close"))
	}
}

// settingsBody draws the pane in height lines: the heading, the blocks of the
// read from the pane's offset, and under them what the setup step shows.
func (m *Model) settingsBody(width, height int) []string {
	th, s := m.cfg.Theme, m.settings
	w := max(width-len(indent), 1)
	title := "Project settings: " + plainLine(s.project) + "  (esc closes)"
	if s.pending && s.result != nil {
		title = "Project settings: " + plainLine(s.project) + "  (reloading)"
	}
	head := []string{th.Heading.Render(clip(title, max(width, 1)))}

	var lines, heads []string
	switch {
	case s.result != nil:
		lines, heads = m.settingsLines(s.result, w)
	case s.pending:
		lines = []string{indent + "pending: projects.show"}
	case s.err != nil:
		for _, line := range wrapText(actionError(s.err), w) {
			lines = append(lines, indent+line)
		}
	}

	var foot []string
	wrap := func(text string, dim bool) {
		for _, line := range wrapText(text, w) {
			if dim {
				line = th.Dim.Render(line)
			}
			foot = append(foot, indent+line)
		}
	}
	switch {
	case s.choosing:
		var names []string
		for i, step := range s.steps {
			if i == s.pick {
				step = "[" + step + "]"
			}
			names = append(names, step)
		}
		wrap("Setup step: "+strings.Join(names, "  "), false)
	case s.confirming:
		wrap("Run: "+plainLine(s.command), false)
		wrap("[y/N] (Enter is No)", false)
	case s.call != 0:
		wrap("pending: project.setup", false)
	}
	if m.actionStatus != "" {
		wrap(m.actionStatus, false)
	}
	wrap("Edit a field with: ork projects set NAME --FIELD VALUE", true)

	room := max(height-len(head)-len(foot), 1)
	s.room = room
	last := len(lines) - 1
	// A window that starts inside a block pins the block's heading on the line
	// under the +N lines above marker, and shows one line fewer of the body. The
	// last window ends flush with the body.
	canPin := room >= 4
	if canPin {
		s.offset = min(s.offset, windowStart(last, len(lines), room-1))
	}
	pin := ""
	if canPin && s.offset > 0 && s.offset < len(heads) {
		pin = heads[s.offset]
	}
	var body []string
	if pin != "" {
		body = slices.Insert(window(lines, s.offset, room-1, th, ""), 1, pin)
	} else {
		// The offset is on a heading or a blank line, or the window is too short
		// to pin. Fitting it to room lines must not move it into a block, where
		// its first row would have no heading: then it stays, and the window pads.
		fit := min(s.offset, windowStart(last, len(lines), room))
		if !canPin || fit == s.offset || fit >= len(heads) || heads[fit] == "" {
			s.offset = fit
		}
		body = window(lines, s.offset, room, th, "")
	}
	return append(append(head, body...), foot...)
}

// plainLine is s with its control characters taken out, on one line: every
// string of the read comes from a file or a tool.
func plainLine(s string) string {
	return strings.Join(strings.Split(runrec.PlainText(s), "\n"), " ")
}

func settingsOnOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func settingsUnset(s string) string {
	if s == "" {
		return "unset"
	}
	return plainLine(s)
}

func settingsNumber(n int) string {
	if n == 0 {
		return "unset"
	}
	return fmt.Sprint(n)
}

func settingsList(items []string) string {
	if len(items) == 0 {
		return "unset"
	}
	return plainLine(strings.Join(items, ","))
}

// settingsLines is the body, one block per owner: a heading line, then KEY and
// VALUE lines with the values cut to the width. vendor_env shows the names of
// its keys and never a value (docs/state.md). heads[i] is the dim heading of
// the block that line i is in, and "" for a heading and a blank line: the pane
// pins it when the window starts inside the block.
func (m *Model) settingsLines(r *daemon.ProjectsShowResult, w int) (out, heads []string) {
	th := m.cfg.Theme
	var current string
	start := func(heading string) {
		if len(out) > 0 {
			out, heads = append(out, ""), append(heads, "")
		}
		out, heads = append(out, th.Heading.Render(heading)), append(heads, "")
		current = th.Dim.Render(heading)
	}
	add := func(line string) {
		out, heads = append(out, line), append(heads, current)
	}
	block := func(heading string, rows [][2]string) {
		start(heading)
		kw := 0
		for _, row := range rows {
			kw = max(kw, lipgloss.Width(row[0]))
		}
		kw = min(kw, max(w/2, 1))
		for _, row := range rows {
			name := clip(row[0], kw)
			value := clip(row[1], max(w-kw-2, 1))
			add(indent + name + strings.Repeat(" ", kw-lipgloss.Width(name)) + "  " + value)
		}
	}
	// partError is the line of a part slicer could not read: slicer's code and
	// message, dimmed.
	partError := func(part string) string {
		for _, e := range r.Slicer.Errors {
			if e.Part == part {
				return th.Dim.Render(indent + clip(plainLine(e.Code)+": "+plainLine(e.Message), w))
			}
		}
		return th.Dim.Render(indent + "not read")
	}

	p := r.Project
	rows := [][2]string{
		{"name", settingsUnset(p.Name)}, {"path", settingsUnset(p.Path)}, {"adj", settingsOnOff(p.Adj)}, {"push", settingsOnOff(p.Push)},
		{"bell", settingsOnOff(p.BellOn())}, {"auto_land", settingsOnOff(p.AutoLandOn())}, {"cap", settingsNumber(p.Cap)},
		{"check_command", settingsUnset(p.CheckCommand)}, {"round_check_command", settingsUnset(p.RoundCheckCommand)},
		{"build_command", settingsUnset(p.BuildCommand)}, {"contract_probe", settingsUnset(p.ContractProbe)}, {"no_verify_check", settingsOnOff(p.NoVerifyCheck)},
		{"check_minutes", settingsNumber(p.CheckMinutes)}, {"budget_minutes", settingsNumber(p.Budgets.Minutes)},
		{"budget_tokens", settingsNumber(p.Budgets.Tokens)}, {"worktree_pattern", settingsUnset(p.WorktreePattern)},
		{"branch_pattern", settingsUnset(p.BranchPattern)}, {"ui_doc", settingsUnset(p.UIDoc)},
		{"ui_pass_skills", settingsList(p.UIPassSkills)}, {"spec_check_rows", settingsList(p.SpecCheckRows)},
		{"ui_pass_rows", settingsList(p.UIPassRows)}, {"notify_command", settingsUnset(p.NotifyCommand)},
	}
	if len(p.EnvironmentLines) == 0 {
		rows = append(rows, [2]string{"environment_line", "unset"})
	}
	for _, line := range p.EnvironmentLines {
		rows = append(rows, [2]string{"environment_line", settingsUnset(line)})
	}
	if len(p.Permissions) == 0 {
		rows = append(rows, [2]string{"permission", "unset"})
	}
	for _, role := range slices.Sorted(maps.Keys(p.Permissions)) {
		perm, value := p.Permissions[role], ""
		if perm.Preset != "" {
			value = perm.Preset
		} else {
			value = "allow " + strings.Join(perm.Allow, ", ")
		}
		rows = append(rows, [2]string{"permission", plainLine(role) + ": " + plainLine(value)})
	}
	if len(p.VendorEnv) == 0 {
		rows = append(rows, [2]string{"vendor_env", "unset"})
	}
	for _, vendor := range slices.Sorted(maps.Keys(p.VendorEnv)) {
		keys := slices.Sorted(maps.Keys(p.VendorEnv[vendor]))
		rows = append(rows, [2]string{"vendor_env", plainLine(vendor) + ": " + plainLine(strings.Join(keys, ", "))})
	}
	block("ork (projects.json)", rows)

	g := r.Global
	owner := "unset"
	if g.Owner != "" {
		owner = plainLine(g.Owner)
	}
	block("ork (global)", [][2]string{
		{"global_cap", fmt.Sprintf("%s (effective %d)", settingsNumber(g.GlobalCap), g.EffectiveGlobalCap)},
		{"daemon_idle_minutes", fmt.Sprintf("%s (effective %d)", settingsNumber(g.DaemonIdleMinutes), g.EffectiveDaemonIdleMinutes)},
		{"owner", owner}, {"file_answers", settingsOnOff(g.FileAnswers)},
	})

	// Only four keys of slicer's config: `ork projects show --json` has the rest.
	var config map[string]json.RawMessage
	if err := json.Unmarshal(r.Slicer.Config, &config); err != nil {
		config = nil
	}
	start("slicer config")
	if config == nil {
		add(partError("config"))
	} else {
		kw := len("implement_finish")
		for _, name := range []string{"relay_contract", "check_command", "claim_owner", "implement_finish"} {
			value, raw := "unset", config[name]
			var text string
			switch {
			case raw == nil:
			case json.Unmarshal(raw, &text) == nil:
				value = settingsUnset(text)
			default:
				value = plainLine(string(raw))
			}
			add(indent + name + strings.Repeat(" ", kw-len(name)) + "  " + clip(value, max(w-kw-2, 1)))
		}
	}

	start("slicer relay roles")
	if mx := r.Slicer.Matrix; mx == nil {
		add(partError("matrix"))
	} else {
		// The id, role, agent, model and reasoning of each row are padded to the
		// widest value of their column, so the use text starts at one column. A
		// column no row fills takes no room.
		cells := make([][5]string, len(mx.Rows))
		var widths [5]int
		for i, row := range mx.Rows {
			cells[i] = [5]string{plainLine(row.ID), plainLine(row.Role), plainLine(row.Agent), plainLine(row.Model), plainLine(row.Reasoning)}
			for c, cell := range cells[i] {
				widths[c] = max(widths[c], lipgloss.Width(cell))
			}
		}
		headWidth := -1 // the gaps between the columns that take room
		for _, cw := range widths {
			if cw > 0 {
				headWidth += cw + 1
			}
		}
		room := w - max(headWidth, 0) - 2
		for i, row := range mx.Rows {
			var parts []string
			for c, cell := range cells[i] {
				if widths[c] > 0 {
					parts = append(parts, cell+strings.Repeat(" ", widths[c]-lipgloss.Width(cell)))
				}
			}
			head, use := strings.Join(parts, " "), plainLine(row.Use)
			if use != "" && room >= 4 {
				add(indent + head + "  " + th.Dim.Render(clip(use, room)))
			} else {
				add(indent + clip(strings.TrimRight(head, " "), w))
			}
		}
		if len(mx.Rows) == 0 {
			add(indent + "no rows")
		}
	}

	configFile := "absent"
	if r.Adj.ConfigFile {
		configFile = "present"
	}
	block("adj", [][2]string{{"enabled", settingsOnOff(r.Adj.Enabled)}, {"config_file", configFile}})
	return out, heads
}
