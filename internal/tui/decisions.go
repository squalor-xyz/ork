// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/adjcli"
)

const decisionsView = 4

type decisionRow struct {
	project string
	record  *adjcli.Record
	message string
	failed  bool
}

// decisionRows keeps registry order and each adj reply's order. Error and
// pending rows are display-only and never take the record cursor.
func (m *Model) decisionRows() []decisionRow {
	var rows []decisionRow
	for i, p := range m.projects {
		if (!m.allProjects && m.active >= 0 && i != m.active) || !p.reg.Adj {
			continue
		}
		if !p.loaded {
			rows = append(rows, decisionRow{project: p.reg.Name, message: "pending: waiting for the first reply"})
			continue
		}
		result, part := p.snap.Open, "inbox"
		if m.decisionsTab == 1 {
			result, part = p.snap.Decided, "list"
		}
		if result.Err != nil {
			code, msg := errParts(result.Err)
			rows = append(rows, decisionRow{project: p.reg.Name, message: (errRow{code: code, parts: []string{part}, msgs: []string{msg}}).text(), failed: true})
			continue
		}
		for _, r := range result.Value {
			rows = append(rows, decisionRow{project: p.reg.Name, record: &r})
		}
	}
	return rows
}

func (m *Model) decisionRecords() []decisionRow {
	var records []decisionRow
	for _, r := range m.decisionRows() {
		if r.record != nil {
			records = append(records, r)
		}
	}
	return records
}

func (m *Model) openDecision() {
	if m.showHelp || m.decisionPane != nil {
		return
	}
	records := m.decisionRecords()
	if i := clampCursor(m.decisionsCursor, len(records)); i >= 0 {
		row := records[i]
		m.decisionPane = &row
		m.decisionScroll = 0
	}
}

// decisionPage matches the main pane's width and body height, including status.
func (m *Model) decisionPage() (w, visible int) {
	width, height, sideW := m.layoutSize()
	w, visible = max(width-sideW, 1), max(height-4, 1)
	status := m.actionStatus
	if m.confirm != nil {
		status = m.confirm.prompt()
	}
	if status != "" {
		lines := min(len(wrapText(status, max(w-len(indent), 1))), max(height-5, 1))
		visible = max(height-4-lines, 1)
	}
	return w, visible
}

func (m *Model) scrollDecision(delta int, page bool) {
	w, visible := m.decisionPage()
	if page {
		delta *= visible
	}
	detail := recordDetailLines(*m.decisionPane, w, m.cfg.Theme)
	last := max(len(detail)-visible, 0)
	m.decisionScroll = min(max(min(m.decisionScroll, last)+delta, 0), last)
}

func (m *Model) decisions(w int) overviewView {
	th := m.cfg.Theme
	out := overviewView{sel: -1}
	add := func(text string) {
		for _, line := range wrapText(text, max(w-len(indent), 1)) {
			out.body = append(out.body, indent+line)
		}
	}
	if pane := m.decisionPane; pane != nil {
		detail := recordDetailLines(*pane, w, th)
		_, visible := m.decisionPage()
		start := min(m.decisionScroll, max(len(detail)-visible, 0))
		out.body = detail[start:]
		return out
	}
	tabs := []string{"Open", "Decided"}
	tabs[m.decisionsTab] = th.ViewActive.Render(tabs[m.decisionsTab])
	out.body = append(out.body, strings.Join(tabs, th.Dim.Render(" | ")))
	all := m.allProjects || m.active < 0
	scope := "All projects using adj"
	if !all {
		scope = m.projects[m.active].reg.Name
	}
	out.body = append(out.body, th.Heading.Render(scope))
	finish := func() overviewView { return out }

	if !all && !m.projects[m.active].reg.Adj {
		add("no adj in this project")
		return finish()
	}
	nameW := 0
	for _, p := range m.projects {
		if p.reg.Adj && (all || p.reg.Name == scope) {
			nameW = max(nameW, lipgloss.Width(p.reg.Name))
		}
	}
	if all && nameW == 0 {
		add("No project uses adj")
		return finish()
	}
	nameW = min(nameW, 20)
	rows := m.decisionRows()
	cursor := clampCursor(m.decisionsCursor, len(m.decisionRecords()))
	index := 0
	for _, row := range rows {
		prefix := indent
		if all {
			name := clip(row.project, nameW)
			prefix += name + strings.Repeat(" ", max(nameW-lipgloss.Width(name), 0)) + "  "
		}
		if row.record == nil {
			for i, line := range wrapText(row.message, max(w-lipgloss.Width(prefix), 1)) {
				lead := prefix
				if i > 0 {
					lead = strings.Repeat(" ", lipgloss.Width(prefix))
				}
				line = lead + line
				if row.failed {
					line = th.Error.Render(line)
				}
				out.body = append(out.body, line)
			}
			continue
		}
		r := row.record
		prefix += fmt.Sprintf("#%d %s  ", r.ID, r.Key)
		line := prefix + clip(r.Title, max(w-lipgloss.Width(prefix), 1))
		if index == cursor {
			out.sel = len(out.body)
			line = th.TabActive.Render(line)
		}
		out.body = append(out.body, line)
		if m.decisionsTab == 0 {
			out.body = append(out.body, recordOptions(*r, w, th, indent+indent)...)
		}
		index++
	}
	if len(rows) == 0 {
		add("No " + strings.ToLower([]string{"Open", "Decided"}[m.decisionsTab]) + " records")
	}
	return finish()
}

func recordOptions(r adjcli.Record, w int, th Theme, prefix string) []string {
	var lines []string
	add := func(text string, subordinate bool) {
		lead := prefix
		if subordinate {
			lead += "   "
		}
		for _, line := range wrapText(text, max(w-lipgloss.Width(lead), 1)) {
			line = lead + line
			if subordinate {
				line = th.Dim.Render(line)
			}
			lines = append(lines, line)
		}
	}
	for _, o := range r.Options {
		add(o.Label+": "+o.Summary, false)
		if o.Tradeoffs != "" {
			add(o.Tradeoffs, true)
		}
	}
	if rec := r.Recommendation; rec != nil {
		add("Recommendation: "+rec.Label, false)
		if rec.Text != "" {
			add(rec.Text, true)
		}
		if rec.Reasoning != "" {
			add(rec.Reasoning, true)
		}
	}
	return lines
}

// recordDetailLines uses the snapshot only; opening a record needs no CLI call.
func recordDetailLines(row decisionRow, w int, th Theme) []string {
	r := row.record
	var lines []string
	heading := func(text string) {
		for _, line := range wrapText(text, max(w, 1)) {
			lines = append(lines, th.Heading.Render(line))
		}
	}
	add := func(text string) {
		for _, line := range wrapText(text, max(w-len(indent), 1)) {
			lines = append(lines, indent+line)
		}
	}
	heading(fmt.Sprintf("%s #%d  (esc returns)", row.project, r.ID))
	heading(r.Key + "  " + r.Title)
	if r.Context != "" {
		add("Context: " + r.Context)
	}
	lines = append(lines, recordOptions(*r, w, th, indent)...)
	if r.Decision != "" {
		add("Decision: " + r.Decision)
	}
	if r.Rationale != "" {
		add("Rationale: " + r.Rationale)
	}
	if len(r.History) > 0 {
		heading("History")
		for _, e := range r.History {
			add(e.Date + " " + e.Event)
		}
	}
	return lines
}
