// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

// laterFiler is the part of the daemon connection that files a later finding
// as a slice. The live connection has it; a test double may not.
type laterFiler interface {
	FileLater(ctx context.Context, run string, n int) (string, error)
}

var _ laterFiler = (*Daemon)(nil)

// paneRun is the engine run the pane shows, as the last snapshot has it.
func (m *Model) paneRun() *source.EngineRun {
	if m.pane == nil || m.pane.ledger {
		return nil
	}
	if p := m.byName[m.pane.project]; p != nil {
		for i := range p.snap.EngineRuns {
			if p.snap.EngineRuns[i].ID == m.pane.run {
				return &p.snap.EngineRuns[i]
			}
		}
	}
	return nil
}

// paneLater returns the later findings of the pane's run that are not filed
// yet, first one first. A removed project offers none. The callers leave out a
// question pane, which keeps its keys for the answer.
func (m *Model) paneLater() []source.EngineFinding {
	run := m.paneRun()
	if run == nil || run.Unavailable {
		return nil
	}
	var out []source.EngineFinding
	for _, f := range run.Findings {
		if f.Kind == runrec.FindingLater && f.Filed == "" {
			out = append(out, f)
		}
	}
	return out
}

// fileHint is the key hint that goes right before "esc close pane" in the key
// line, when there is a later finding to file.
func (m *Model) fileHint() string {
	if len(m.paneLater()) == 0 {
		return ""
	}
	return "f file slice; "
}

// findingLines tells the owner what the reviewer, UI pass or gate left for
// them (adj 163): later work to file as a slice, and owner questions that did
// not block the gate.
func (m *Model) findingLines(width int) (later, owner []string) {
	run := m.paneRun()
	if run == nil || run.Unavailable || m.paneQuestion() {
		return nil, nil
	}
	var lines []string
	if offers := m.paneLater(); len(offers) > 0 {
		noun := "findings"
		if len(offers) == 1 {
			noun = "finding"
		}
		lines = append(lines, laterLine(fmt.Sprintf("%d later %s; next to file: ", len(offers), noun), offers[0].Title, width))
	}
	var filed []string
	for _, f := range run.Findings {
		if f.Kind == runrec.FindingLater && f.Filed != "" {
			filed = append(filed, f.Filed)
		}
	}
	if len(filed) > 0 {
		lines = append(lines, clip("Filed as slices: "+strings.Join(filed, ", "), width))
	}
	later, lines = lines, nil
	for _, f := range run.Findings {
		if f.Kind != runrec.FindingOwner {
			continue
		}
		text := "Owner question, not blocking: " + f.Title
		if a := f.Ask; a != nil && a.Recommend >= 1 && a.Recommend <= len(a.Options) {
			text += " Recommended: " + a.Options[a.Recommend-1].Label + "."
		}
		lines = append(lines, wrapText(text, width)...)
	}
	return later, lines
}

// laterLine is the one line that offers a later finding: the lead, then the
// title in quotes. A long title is cut inside the quotes, so the line shows
// whole or, when the pane is short, drops whole (never ends in an open quote).
func laterLine(lead, title string, width int) string {
	for n := len([]rune(title)); n > 0; n-- {
		t := title
		if n < len([]rune(title)) {
			t = clip(title, n)
		}
		if line := lead + strconv.Quote(t); lipgloss.Width(line) <= width {
			return line
		}
	}
	return clip(lead, width)
}

// requestFile asks the owner to confirm filing the first later finding of the
// pane's run. The confirmation shows the command ork will run in the project.
func (m *Model) requestFile() {
	later := m.paneLater()
	r, ok := m.actionRow()
	if len(later) == 0 || !ok {
		return
	}
	for _, a := range m.actionFlights[r.engineID] {
		if a.verb == "file" {
			m.actionStatus = a.command() + " is pending"
			return
		}
	}
	if m.cfg.Live == nil {
		m.actionStatus = "No daemon connection; action unavailable"
		return
	}
	if _, ok := m.cfg.Live.(laterFiler); !ok {
		m.actionStatus = "This daemon connection cannot file slices"
		return
	}
	m.actionStatus = ""
	m.confirm = &runAction{verb: "file", run: r.engineID, project: r.name, item: r.id, finding: later[0].N, text: later[0].Title}
}
