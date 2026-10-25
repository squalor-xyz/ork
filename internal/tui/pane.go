// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"github.com/squalor-xyz/ork/internal/source"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// runPane keeps its run target while snapshots reorder Overview rows.
type runPane struct {
	run, project string
	adj          bool
	ledger       bool
	input        textField
	offset       int
}

func (m *Model) openPane(r ovRow) tea.Cmd {
	m.pane = &runPane{run: r.engineID, project: r.name}
	if r.ledgerID != "" {
		m.pane.run, m.pane.ledger = r.ledgerID, true
	}
	m.actionStatus = ""
	return nil
}

func (m *Model) paneRow() (ovRow, string, bool) {
	p := m.byName[m.pane.project]
	if p != nil {
		if m.pane.ledger {
			for _, r := range boardLedgers(p.snap) {
				if r.ID == m.pane.run {
					return ovRow{ledgerID: r.ID, name: p.reg.Name, id: strings.Join(r.Items, ", "), state: r.State}, "", true
				}
			}
			return ovRow{}, "", false
		}
		for _, r := range p.snap.EngineRuns {
			if r.ID == m.pane.run {
				return ovRow{unavailable: r.Unavailable, failure: r.Error, engineID: r.ID, name: p.reg.Name, id: r.Item, state: r.State, pending: r.Pending}, r.Wait, true
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
	return ok && !r.unavailable && !m.pane.ledger && r.state == "paused" && r.pending == "question"
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
		if msg.String() == "ctrl+a" {
			if _, reason := m.paneAdjKey(); reason == "" {
				m.pane.adj = !m.pane.adj
			}
			return nil
		}
		if msg.String() == "enter" {
			return m.requestAction("answer")
		}
		m.pane.input.update(msg)
		m.actionStatus = ""
		return nil
	}
	r, _, ok := m.paneRow()
	if !ok || m.pane.ledger || r.state == "stopped" || r.state == "committed" {
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
// and status stay visible while Page Up/Down scroll the tree and Wait reason.
func (m *Model) paneLines(w, h int, status string) []string {
	r, reason, ok := m.paneRow()
	if !ok {
		return []string{indent + "Run " + m.pane.run + ": no longer in snapshot", indent + "esc close pane"}
	}
	width := max(w-len(indent), 1)
	run := r.engineID
	if m.pane.ledger {
		run = r.ledgerID
	}
	displayState := r.state
	stale := m.paneBoardRun().State == "stale"
	if stale {
		displayState = "stale"
	}
	title := "Run " + run + "  " + r.id + "  " + displayState
	if m.pane.ledger {
		title += "  started elsewhere"
	}
	header := wrapText(title, width)
	for i := range header {
		if stale {
			header[i] = m.cfg.Theme.Dim.Render(header[i])
		} else {
			header[i] = m.cfg.Theme.Heading.Render(header[i])
		}
	}
	footer := []string{}
	var priorities []int
	addFooter := func(lines []string, priority int) {
		footer = append(footer, lines...)
		for range lines {
			priorities = append(priorities, priority)
		}
	}
	switch {
	case r.unavailable:
		addFooter(wrapText(source.RemovedProjectHelp(r.name), width), 2)
		addFooter([]string{"Removed project; esc close pane"}, 3)
	case m.pane.ledger:
		addFooter(wrapText("started elsewhere, read-only; esc close pane", width), 2)
	case r.state == "paused" && r.pending == "question":
		addFooter([]string{"Answer: " + m.pane.input.view(max(width-len("Answer: "), 1), m.cfg.Theme)}, 0)
		addFooter(wrapText("ork answer "+r.engineID+" "+m.pane.input.Value(), width), 5)
		decisionKey, disabledReason := m.paneAdjKey()
		on := m.pane.adj && disabledReason == ""
		toggle := "[ ] file answer in adj as " + decisionKey + "  ctrl+a toggles"
		if on {
			toggle = "[x] file answer in adj as " + decisionKey + "  ctrl+a toggles"
		}
		if disabledReason != "" {
			toggle = "adj filing off: " + disabledReason
		}
		warningAt := len(toggle)
		if on && strings.TrimSpace(os.Getenv("ADJ_DECIDED_BY")) == "" {
			toggle += "  ADJ_DECIDED_BY unset"
			warningAt += 2
		}
		// Wrap plain text first so each physical line has its own style.
		toggleLines := wrapText(toggle, width)
		offset := 0
		for i, line := range toggleLines {
			offset += strings.Index(toggle[offset:], line)
			switch {
			case disabledReason != "":
				toggleLines[i] = m.cfg.Theme.Dim.Render(line)
			case on:
				split := min(max(warningAt-offset, 0), len(line))
				toggleLines[i] = ""
				if split > 0 {
					toggleLines[i] = m.cfg.Theme.Heading.Render(line[:split])
				}
				if split < len(line) {
					toggleLines[i] += m.cfg.Theme.Warn.Render(line[split:])
				}
			}
			offset += len(line)
		}
		priority := 4
		if on {
			priority = 2
		}
		addFooter(toggleLines, priority)
		addFooter([]string{"enter submit; esc close pane"}, 3)
	case r.state == "paused" && r.pending == "budget":
		addFooter(wrapText("t retry: one more role round; esc close pane", width), 3)
	case r.state == "paused":
		addFooter([]string{"t retry; esc close pane"}, 3)
	case r.state == "stopped":
		addFooter(wrapText("Stopped; relaunch, or clear with: ork dismiss "+r.engineID, width), 2)
		addFooter([]string{"esc close pane"}, 3)
	case r.state == "awaiting-approval":
		addFooter([]string{"a approve; x stop; esc close pane"}, 3)
	case r.state == "committed":
		addFooter([]string{"esc close pane"}, 3)
	default:
		addFooter([]string{"x stop; esc close pane"}, 3)
	}
	if status != "" {
		addFooter(wrapText(status, width), 1)
	}
	// Drop the lowest priority lines first: preview, off/disabled toggle,
	// submit hint, on toggle, status, then input. Preserve earlier lines
	// within a priority when the terminal cannot fit the whole group.
	for len(footer) > h && len(footer) > 0 {
		drop := 0
		for i, priority := range priorities {
			if priority >= priorities[drop] {
				drop = i
			}
		}
		footer = append(footer[:drop], footer[drop+1:]...)
		priorities = append(priorities[:drop], priorities[drop+1:]...)
	}
	// The action area takes priority if the header no longer fits.
	header = header[:min(len(header), max(h-len(footer), 0))]
	var lines []string
	if reason != "" {
		lines = append(lines, wrapText(reason, width)...)
	}
	steps := m.paneSteps()
	lines = append(lines, m.boardLines(steps, width)...)
	roleWidth := treeRoleWidth(steps)
	if boardLive(m.paneBoardRun().State) {
		for _, role := range pipelineRoles(steps) {
			roleWidth = max(roleWidth, len(role))
		}
	}
	lines = append(lines, m.runTreeLinesWithRoleWidth(steps, width, m.treeFrame, roleWidth)...)
	lines = append(lines, m.boardTail(steps, width, roleWidth)...)
	if len(lines) == 0 {
		lines = wrapText(reason, width)
	}
	room := max(h-len(header)-len(footer)-1, 0)
	last := max(len(lines)-room, 0)
	if len(lines) > room && room >= 3 {
		last++ // the marker above uses one line at the bottom of the content
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
	for i := range out {
		out[i] = lipgloss.NewStyle().MaxWidth(max(w, 1)).Render(out[i])
	}
	return out
}

func (m *Model) paneAdjKey() (string, string) {
	p := m.byName[m.pane.project]
	if p == nil {
		return "", "no adj in this project"
	}
	for _, r := range p.snap.EngineRuns {
		if r.ID == m.pane.run {
			if !p.reg.Adj {
				return r.Key, "no adj in this project"
			}
			if r.Key == "" {
				return "", "question has no decision key"
			}
			return r.Key, ""
		}
	}
	return "", "question has no decision key"
}
