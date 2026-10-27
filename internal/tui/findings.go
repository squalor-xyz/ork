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

// findingLines tells the owner what the reviewer, UI pass or gate left for
// them (adj 163): later work to file as a slice, and owner questions that did
// not block the gate.
func (m *Model) findingLines(width int) (later []string, owner [][]string) {
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
	later = lines
	for _, f := range run.Findings {
		if f.Kind != runrec.FindingOwner || f.Answer != "" {
			continue
		}
		text := "Owner question, not blocking: " + f.Title
		if a := f.Ask; a != nil && a.Recommend >= 1 && a.Recommend <= len(a.Options) {
			text += " Recommended: " + a.Options[a.Recommend-1].Label + "."
		}
		owner = append(owner, wrapText(text, width))
	}
	if len(owner) > 4 {
		more := len(owner) - 3
		noun := "findings"
		if more == 1 {
			noun = "finding"
		}
		owner = append(owner[:3], []string{fmt.Sprintf("+%d more owner %s", more, noun)})
	}
	return later, owner
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

// fileRefusal is why f does not ask to file the pane's first later finding: the
// status line text, or "" for a silent refusal (nothing to file). It reports
// refused false when f asks. The key refuses by it and the hint line lists f
// only when it does not.
func (m *Model) fileRefusal() (status string, refused bool) {
	r, ok := m.actionRow()
	if len(m.paneLater()) == 0 || !ok {
		return "", true
	}
	for _, a := range m.actionFlights[r.engineID] {
		if a.verb == "file" {
			return a.command() + " is pending", true
		}
	}
	if m.cfg.Live == nil {
		return "No daemon connection; action unavailable", true
	}
	if _, ok := m.cfg.Live.(laterFiler); !ok {
		return "This daemon connection cannot file slices", true
	}
	return "", false
}

// requestFile asks the owner to confirm filing the first later finding of the
// pane's run. The confirmation shows the command ork will run in the project.
func (m *Model) requestFile() {
	if status, refused := m.fileRefusal(); refused {
		if status != "" {
			m.actionStatus = status
		}
		return
	}
	later := m.paneLater()
	r, _ := m.actionRow()
	m.actionStatus = ""
	m.confirm = &runAction{verb: "file", run: r.engineID, project: r.name, item: r.id, finding: later[0].N, text: later[0].Title, scores: later[0].Scores}
}

// paneOwner returns the selected finding, or the first unanswered owner finding.
func (m *Model) paneOwner() *source.EngineFinding {
	r := m.paneRun()
	if r == nil || r.Unavailable {
		return nil
	}
	for i := range r.Findings {
		f := &r.Findings[i]
		if f.Kind == runrec.FindingOwner && f.Answer == "" && f.Ask != nil && (m.pane.finding == 0 || m.pane.finding == f.N) {
			return f
		}
	}
	return nil
}
