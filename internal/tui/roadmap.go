// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/slicercli"
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
	heading("Roadmap")
	if s.Deps.Err != nil {
		showError("deps", s.Deps.Err)
	}
	if s.Items.Err != nil {
		showError("items", s.Items.Err)
		return out
	}
	ready := map[string]bool{}
	for _, it := range s.Deps.Value {
		ready[it.ID] = true
	}
	cursor := clampCursor(m.roadmapCursor, len(s.Items.Value))
	for i, it := range s.Items.Value {
		readiness := "not ready"
		if s.Deps.Err != nil {
			readiness = "?"
		} else if ready[it.ID] && it.Status == "open" && it.HasSlice {
			readiness = "ready"
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
		fields := fmt.Sprintf("  %s  %s  effort %s", it.Status, size, effort)
		prefix := fmt.Sprintf("%-9s %s  ", readiness, it.ID)
		line := indent + prefix + clip(title, max(w-2-lipgloss.Width(prefix)-lipgloss.Width(fields), 1)) + fields
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
