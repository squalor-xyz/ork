// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

const roadmapView = 3

type itemPane struct {
	project, id string
	request     uint64
	pending     bool
	detail      *slicercli.Detail
	err         error
}

type itemShowMsg struct {
	project, id string
	request     uint64
	detail      *slicercli.Detail
	err         error
}

func (m *Model) roadmapItems() []slicercli.Item {
	if m.active == runningEntry {
		return m.runningRoadmapItems()
	}
	if m.active < 0 {
		return nil
	}
	s := m.projects[m.active].snap
	if s.Items.Err != nil {
		return nil
	}
	return s.Items.Value
}

func (m *Model) openItem() tea.Cmd {
	if m.active < 0 {
		return nil // the combined Roadmap of the Running entry only lists
	}
	items := m.roadmapItems()
	i := clampCursor(m.roadmapCursor, len(items))
	if i < 0 || m.showHelp || m.itemPane != nil {
		return nil
	}
	m.itemRequest++
	pane := &itemPane{project: m.projects[m.active].reg.Name, id: items[i].ID, request: m.itemRequest, pending: true}
	m.itemPane = pane
	live := m.cfg.Live
	if live == nil {
		pane.pending = false
		pane.err = fmt.Errorf("No daemon connection; detail unavailable")
		return nil
	}
	project, id, request := pane.project, pane.id, pane.request
	return func() tea.Msg {
		detail, err := live.ItemShow(context.Background(), project, id)
		return itemShowMsg{project: project, id: id, request: request, detail: detail, err: err}
	}
}

// roadmap renders only snapshot state; detail reads run in openItem's command.
func (m *Model) roadmap(w int) overviewView {
	th := m.cfg.Theme
	out := overviewView{sel: -1}
	add := func(text string) {
		for _, line := range wrapText(text, max(w-2, 1)) {
			out.body = append(out.body, indent+line)
		}
	}
	heading := func(text string) {
		for _, line := range wrapText(text, max(w, 1)) {
			out.body = append(out.body, th.Heading.Render(line))
		}
	}
	if m.active == runningEntry {
		return m.runningRoadmapView(w)
	}
	if m.active < 0 {
		add("Select a project to see its roadmap.")
		return out
	}
	p := m.projects[m.active]
	if !p.loaded {
		add("pending: waiting for the first reply")
		return out
	}
	if pane := m.itemPane; pane != nil {
		heading("Item " + pane.id + "  (esc returns)")
		switch {
		case pane.pending:
			add("pending: item.show")
		case pane.err != nil:
			add(actionError(pane.err))
		case pane.detail != nil:
			d := pane.detail
			if d.Slice != nil {
				spec := d.Slice
				heading(spec.Title)
				add("Size: " + spec.Size)
				for _, lead := range spec.Lead {
					add(lead)
				}
				for _, section := range spec.Sections {
					heading(section.Heading)
					add(section.Body)
				}
			} else {
				heading(d.Title)
				add("Size: " + d.Fields.Size)
			}
		default:
			add("No item detail returned")
		}
		return out
	}
	s := p.snap
	showError := func(part string, err error) {
		code, msg := errParts(err)
		row := errRow{code: code, parts: []string{part}, msgs: []string{msg}}
		for _, line := range wrapText(row.text(), max(w-2, 1)) {
			out.body = append(out.body, indent+th.Error.Render(line))
		}
	}
	heading("Goals")
	if s.Goals.Err != nil {
		showError("goals", s.Goals.Err)
	} else if g := s.Goals.Value; g != nil {
		if g.Goals != "" {
			add(g.Goals)
		}
		for _, goal := range g.Records.Goal {
			add(goal.ID + " " + goal.Title)
			if goal.Body != "" {
				add(goal.Body)
			}
		}
	}
	out.body = append(out.body, "")
	ready, open := 0, 0
	for _, it := range s.Items.Value {
		if it.Status == "open" {
			open++
		}
		if readySlice(s, it) {
			ready++
		}
	}
	summary := "all open"
	if open != len(s.Items.Value) {
		summary = fmt.Sprintf("%d open, %d other", open, len(s.Items.Value)-open)
	}
	if s.Items.Err != nil {
		heading("Roadmap")
	} else if s.Deps.Err != nil {
		heading(fmt.Sprintf("Roadmap  ? ready of %d, %s", len(s.Items.Value), summary))
	} else {
		heading(fmt.Sprintf("Roadmap  %d ready of %d, %s", ready, len(s.Items.Value), summary))
	}
	if s.Deps.Err != nil {
		showError("deps", s.Deps.Err)
	}
	if s.Items.Err != nil {
		showError("items", s.Items.Err)
		return out
	}
	cursor := clampCursor(m.roadmapCursor, len(s.Items.Value))
	headings := map[int]string{} // item index of a run's first item -> its heading
	for _, run := range passRuns(s.Items.Value) {
		headings[run.start] = fmt.Sprintf("%s (%d)", run.label(), run.n)
	}
	for i, it := range s.Items.Value {
		if text, ok := headings[i]; ok {
			out.body = append(out.body, indent+th.Dim.Render(text))
		}
		readiness := " "
		if s.Deps.Err != nil {
			readiness = "?"
		} else if readySlice(s, it) {
			readiness = "·"
			if m.cfg.ASCII {
				readiness = "*"
			}
		}
		title := it.ShortTitle
		if title == "" {
			title = it.Title
		}
		size := it.Fields.Size
		if size == "" {
			size = "-"
		}
		effort := "-"
		if it.Fields.Effort != 0 {
			effort = fmt.Sprint(it.Fields.Effort)
		}
		fields := size + effort
		marker := readiness
		if i != cursor {
			marker = th.Dim.Render(marker)
		}
		prefix := indent + marker + " " + it.ID + "  "
		room := max(w-lipgloss.Width(prefix)-lipgloss.Width(fields)-2, 1)
		title = clip(title, room)
		line := prefix + title + strings.Repeat(" ", max(w-lipgloss.Width(prefix)-lipgloss.Width(title)-lipgloss.Width(fields), 2)) + fields
		out.rows = append(out.rows, len(out.body))
		if i == cursor {
			out.sel = len(out.body)
			line = th.TabActive.Render(line)
		}
		out.body = append(out.body, line)
	}
	if len(s.Items.Value) == 0 {
		add("No items")
	}
	return out
}

// passRun is a run of consecutive items with one pass: the index of its first
// item and its length.
type passRun struct {
	pass     string
	start, n int
}

// label is the pass as the Roadmap heading names it.
func (r passRun) label() string {
	if r.pass == "" {
		return "No pass"
	}
	return r.pass
}

// passRuns splits items, in slicer's order, into runs of one pass. Slicer
// lists a pass together, but a pass that comes back after another starts a
// new run with its own heading (S162). The headings and the group keys both
// use these runs.
func passRuns(items []slicercli.Item) []passRun {
	var runs []passRun
	for i, it := range items {
		if i > 0 && it.Fields.Pass == runs[len(runs)-1].pass {
			runs[len(runs)-1].n++
			continue
		}
		runs = append(runs, passRun{pass: it.Fields.Pass, start: i, n: 1})
	}
	return runs
}

// readySlice is shared by the Roadmap and launch list. Claims are not exposed
// by slicer (roadmap.readiness, record 140).
func readySlice(s source.Snapshot, it slicercli.Item) bool {
	if s.Deps.Err != nil || it.Status != "open" || !it.HasSlice {
		return false
	}
	for _, dep := range s.Deps.Value {
		if dep.ID == it.ID {
			return true
		}
	}
	return false
}
