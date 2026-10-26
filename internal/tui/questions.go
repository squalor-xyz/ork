// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/source"
)

const questionsView = 1

type questionRow struct {
	project string
	engine  *source.EngineRun
	record  *adjcli.Record
	message string
	failed  bool
}

// questionRows reads only snapshots, preserving registry and inbox order.
// Pending and error rows are display-only.
func questionRows(projects []*projectState) []questionRow {
	var rows []questionRow
	for _, p := range projects {
		if p.snap.Unavailable {
			continue
		}
		if !p.loaded {
			rows = append(rows, questionRow{project: p.reg.Name, message: "waiting for the first reply"})
			continue
		}
		for _, r := range p.snap.EngineRuns {
			if !r.Unavailable && r.State == "paused" && r.Pending == "question" && r.Section(p.snap.EngineRuns) == source.SectionNeeds {
				rows = append(rows, questionRow{project: p.reg.Name, engine: &r})
			}
		}
		if !p.reg.Adj {
			continue
		}
		if err := p.snap.Open.Err; err != nil {
			code, msg := errParts(err)
			rows = append(rows, questionRow{project: p.reg.Name, message: (errRow{code: code, parts: []string{"inbox"}, msgs: []string{msg}}).text(), failed: true})
			continue
		}
		for _, r := range p.snap.Open.Value {
			rows = append(rows, questionRow{project: p.reg.Name, record: &r})
		}
	}
	return rows
}

func (m *Model) selectableQuestions() []questionRow {
	var rows []questionRow
	for _, r := range questionRows(m.projects) {
		if r.engine != nil || r.record != nil {
			rows = append(rows, r)
		}
	}
	return rows
}

func (m *Model) openQuestion() tea.Cmd {
	if m.showHelp || m.decisionPane != nil {
		return nil
	}
	rows := m.selectableQuestions()
	if i := clampCursor(m.questionsCursor, len(rows)); i >= 0 {
		row := rows[i]
		if row.engine != nil {
			return m.openPane(ovRow{engineID: row.engine.ID, name: row.project})
		}
		m.decisionPane = &decisionRow{project: row.project, record: row.record}
		m.decisionScroll = 0
	}
	return nil
}

func (m *Model) questions(w int) overviewView {
	out := overviewView{sel: -1}
	if m.decisionPane != nil {
		detail := recordDetailLines(*m.decisionPane, w, m.cfg.Theme)
		_, visible := m.decisionPage()
		start := min(m.decisionScroll, max(len(detail)-visible, 0))
		out.body = detail[start:]
		return out
	}
	out.body = append(out.body, m.cfg.Theme.Heading.Render("All projects"))
	nameW := 0
	for _, p := range m.projects {
		if !p.snap.Unavailable {
			nameW = max(nameW, lipgloss.Width(p.reg.Name))
		}
	}
	nameW = min(nameW, 20)
	rows := questionRows(m.projects)
	cursor, index := clampCursor(m.questionsCursor, len(m.selectableQuestions())), 0
	for _, row := range rows {
		name := clip(row.project, nameW)
		kind, style := kindPending, m.cfg.Theme.Pending
		switch {
		case row.engine != nil:
			kind, style = "? question", m.cfg.Theme.Warn.Bold(m.cfg.Theme.Mono)
		case row.record != nil:
			kind, style = kindDecision, m.cfg.Theme.Decision
		case row.failed:
			kind, style = kindError, m.cfg.Theme.Error
		}
		kind = fmt.Sprintf("%-*s", kindWidth, kind)
		project := name + strings.Repeat(" ", max(nameW-lipgloss.Width(name), 0)) + "  "
		prefix := indent + style.Render(kind) + " " + project
		text := row.message
		if row.engine != nil {
			r := row.engine
			text = r.ID + "  " + r.Item + "  " + strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(r.Wait)
		} else if row.record != nil {
			r := row.record
			text = fmt.Sprintf("#%d %s  %s", r.ID, r.Key, r.Title)
		}
		if row.engine == nil && row.record == nil {
			for i, line := range wrapText(text, max(w-lipgloss.Width(prefix), 1)) {
				lead := prefix
				if i > 0 {
					lead = strings.Repeat(" ", lipgloss.Width(prefix))
				}
				line = lead + line
				out.body = append(out.body, line)
			}
			continue
		}
		line := prefix + clip(text, max(w-lipgloss.Width(prefix), 1))
		if index == cursor {
			out.sel = len(out.body)
			line = m.cfg.Theme.TabActive.Render(indent + kind + " " + project + clip(text, max(w-lipgloss.Width(prefix), 1)))
		}
		out.body = append(out.body, line)
		index++
	}
	if len(rows) == 0 {
		out.body = append(out.body, indent+m.cfg.Theme.Dim.Render("No open questions"))
	} else {
		out.body = append(out.body, indent+"[enter] open")
	}
	return out
}
