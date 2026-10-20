// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// runPane keeps its run target while snapshots reorder Overview rows.
type runPane struct {
	run, project string
	input        textField
	offset       int
}

func (m *Model) openPane(r ovRow) tea.Cmd {
	m.pane = &runPane{run: r.engineID, project: r.name}
	m.actionStatus = ""
	return nil
}

func (m *Model) paneRow() (ovRow, string, bool) {
	p := m.byName[m.pane.project]
	if p != nil {
		for _, r := range p.snap.EngineRuns {
			if r.ID == m.pane.run {
				return ovRow{engineID: r.ID, name: p.reg.Name, id: r.Item, state: r.State, pending: r.Pending}, r.Wait, true
			}
		}
	}
	return ovRow{}, "", false
}

func (m *Model) actionRow() (ovRow, bool) {
	if m.pane != nil {
		r, _, ok := m.paneRow()
		return r, ok
	}
	return m.selectedRow()
}

func (m *Model) paneQuestion() bool {
	r, _, ok := m.paneRow()
	return ok && r.state == "paused" && r.pending == "question"
}

func (m *Model) paneKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.pane = nil
		m.actionStatus = ""
		return nil
	case "pgup":
		m.pane.offset = max(m.pane.offset-1, 0)
		return nil
	case "pgdown":
		m.pane.offset++
		return nil
	}
	if m.paneQuestion() {
		if msg.String() == "enter" {
			return m.requestAction("answer")
		}
		m.pane.input.update(msg)
		m.actionStatus = ""
		return nil
	}
	r, _, ok := m.paneRow()
	if !ok || r.state == "stopped" || r.state == "committed" {
		return nil
	}
	switch {
	case key.Matches(msg, m.keys.Approve):
		return m.requestAction("approve")
	case key.Matches(msg, m.keys.Stop):
		return m.requestAction("stop")
	case key.Matches(msg, m.keys.Retry):
		return m.requestAction("retry")
	}
	return nil
}

// paneLines expands the selected run below the Overview. The field, command
// and status stay visible while Page Up/Down scroll the full Wait reason.
func (m *Model) paneLines(w, h int, status string) []string {
	r, reason, ok := m.paneRow()
	if !ok {
		return []string{indent + "Run " + m.pane.run + ": no longer in snapshot", indent + "esc close pane"}
	}
	width := max(w-len(indent), 1)
	header := wrapText("Run "+r.engineID+"  "+r.id+"  "+r.state, width)
	footer := []string{}
	switch {
	case r.state == "paused" && r.pending == "question":
		footer = append(footer, "Answer: "+m.pane.input.view(max(width-len("Answer: "), 1), m.cfg.Theme))
		footer = append(footer, wrapText("ork answer "+r.engineID+" "+m.pane.input.Value(), width)...)
		footer = append(footer, "enter submit; esc close pane")
	case r.state == "paused":
		footer = append(footer, "t retry; esc close pane")
	case r.state == "stopped":
		footer = append(footer, "Stopped; relaunch to continue", "esc close pane")
	case r.state == "awaiting-approval":
		footer = append(footer, "a approve; x stop; esc close pane")
	case r.state == "committed":
		footer = append(footer, "esc close pane")
	default:
		footer = append(footer, "x stop; esc close pane")
	}
	if status != "" {
		footer = append(footer, wrapText(status, width)...)
	}
	// The action area takes priority if the header no longer fits.
	header = header[:min(len(header), max(h-len(footer), 0))]
	lines := wrapText(reason, width)
	room := max(h-len(header)-len(footer)-1, 0)
	last := max(len(lines)-room, 0)
	if len(lines) > room && room >= 3 {
		last++ // the marker above uses one line at the bottom of the reason
	}
	m.pane.offset = min(m.pane.offset, last)
	// Indent content before window adds styled markers at the same column.
	for _, section := range [][]string{header, lines, footer} {
		for i := range section {
			section[i] = indent + section[i]
		}
	}
	if room == 0 {
		lines = nil
	} else if len(lines) > room {
		lines = window(lines, m.pane.offset, room, m.cfg.Theme)
	}
	out := append(header, lines...)
	if len(out)+len(footer) < h {
		out = append(out, indent)
	}
	out = append(out, footer...)
	return out
}
