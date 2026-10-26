// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// boardLedgers includes proposed stale and recent data, preferring the active read.
func boardLedgers(s source.Snapshot) []slicercli.RelayRun {
	out := append([]slicercli.RelayRun(nil), s.Runs.Value...)
	seen := map[string]bool{}
	for _, r := range out {
		seen[r.ID] = true
	}
	for _, r := range s.BoardRuns.Value {
		if !seen[r.ID] {
			out = append(out, r)
			seen[r.ID] = true
		}
	}
	return out
}

func pipelineRoles(steps []runTreeStep) []string {
	roles := []string{"implementer", "reviewer", "gate"}
	for _, s := range steps {
		if s.Role == "architect" {
			return append([]string{"architect"}, roles...)
		}
	}
	return roles
}

func laneSteps(steps []runTreeStep) []runTreeStep {
	orchestrators := map[string]bool{}
	known := map[string]bool{}
	for _, s := range steps {
		known[s.ID] = true
		if s.Role == "orchestrator" {
			orchestrators[s.ID] = true
		}
	}
	var out []runTreeStep
	for _, s := range steps {
		if s.Parent == "" || !known[s.Parent] || orchestrators[s.Parent] {
			out = append(out, s)
		}
	}
	return out
}

func roleCounts(steps []runTreeStep) map[string]int {
	counts := map[string]int{}
	for _, s := range laneSteps(steps) {
		counts[s.Role]++
	}
	return counts
}

func (m *Model) pipeline(steps []runTreeStep, stale, compact bool) string {
	steps = laneSteps(steps)
	var parts []string
	phases := pipelinePhases(steps)
	for _, role := range pipelineRoles(steps) {
		phase := phases[role]
		text := role
		style := m.cfg.Theme.Dim
		switch phase {
		case "done":
			style = m.cfg.Theme.Passed
		case "start":
			if !stale {
				text = "[" + role + "]"
			}
			style = m.cfg.Theme.Running
		case "fail":
			style = m.cfg.Theme.Error
		}
		if stale {
			style = m.cfg.Theme.Dim
		}
		if compact {
			parts = append(parts, text)
		} else {
			parts = append(parts, style.Render(text))
		}
	}
	sep := " > "
	if compact {
		sep = ">"
	}
	return strings.Join(parts, sep)
}

func (m *Model) pipelineLine(steps []runTreeStep, stale bool, width int) string {
	line := m.pipeline(steps, stale, false)
	n := roleCounts(steps)["implementer"]
	round := fmt.Sprintf("round %d", n)
	if n > 0 && lipgloss.Width(line)+2+len(round) <= width {
		return line + strings.Repeat(" ", width-lipgloss.Width(line)-len(round)) + m.cfg.Theme.Dim.Render(round)
	}
	// Drop the round, then whole roles from the right at very narrow widths.
	parts := strings.Split(line, " > ")
	for len(parts) > 0 && lipgloss.Width(strings.Join(parts, " > ")) > width {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, " > ")
}

func (m *Model) lastHeard(r slicercli.RelayRun) (string, lipgloss.Style) {
	at, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if err != nil {
		return "", m.cfg.Theme.Dim
	}
	age := m.cfg.Now().Sub(at)
	if !boardLive(r.State) {
		return "heard " + elapsed(age), m.cfg.Theme.Dim
	}
	cue := ""
	style := m.cfg.Theme.Dim
	if age > 15*time.Minute {
		cue = "! "
		style = m.cfg.Theme.Warn
	}
	if r.State == "stale" {
		cue = "!! "
		style = m.cfg.Theme.Error
	}
	return cue + "heard " + elapsed(age), style
}

func (m *Model) paneBoardRun() slicercli.RelayRun {
	p := m.byName[m.pane.project]
	if p == nil {
		return slicercli.RelayRun{}
	}
	id := m.pane.run
	if !m.pane.ledger {
		for _, r := range p.snap.EngineRuns {
			if r.ID == id {
				for _, ledger := range boardLedgers(p.snap) {
					if r.RelayRun != "" && ledger.ID == r.RelayRun {
						if !boardLive(r.State) {
							ledger.State = r.State
						}
						return ledger
					}
				}
				ledger, _ := engineBoard(r)
				return ledger
			}
		}
	}
	for _, r := range boardLedgers(p.snap) {
		if r.ID == id {
			return r
		}
	}
	return slicercli.RelayRun{}
}

func (m *Model) boardLines(steps []runTreeStep, width int) []string {
	r := m.paneBoardRun()
	live := r.State != "stale" && boardLive(r.State)
	bar := treeClip(stageBar(steps, m.treeFrame, live), width)
	if !live {
		bar = m.cfg.Theme.Dim.Render(bar)
	}
	lines := []string{bar, m.pipelineLine(steps, !live, width)}
	if heard, style := m.lastHeard(r); heard != "" {
		lines = append(lines, style.Render(treeClip(strings.Replace(heard, "heard ", "last heard ", 1)+" ago", width)))
	}
	if live {
		if activity := m.paneActivity(); len(activity) > 0 {
			lines = append(lines, m.cfg.Theme.Dim.Render(treeClip("activity "+sparkline(activity, m.cfg.ASCII)+"  last 5m", width)))
		}
	}
	return lines
}

func (m *Model) boardTail(steps []runTreeStep, width, roleWidth int) []string {
	if !boardLive(m.paneBoardRun().State) {
		return nil
	}
	var lines []string
	for _, role := range pipelineRoles(steps) {
		begun := false
		for _, step := range laneSteps(steps) {
			if step.Role == role {
				begun = true
			}
		}
		if !begun {
			lines = append(lines, m.cfg.Theme.Dim.Render(treeClip(fmt.Sprintf(". %-*s waiting", roleWidth, role), width)))
		}
	}
	return lines
}

func (m *Model) recentRows() []ovRow {
	var rows []ovRow
	for i, p := range m.projects {
		if !p.loaded || p.setup() || (m.active >= 0 && i != m.active) {
			continue
		}
		// The ledger run of a slice that ork landed reads "landed" (S119).
		landed := map[string]bool{}
		for _, run := range p.snap.EngineRuns {
			if run.Landed && run.RelayRun != "" {
				landed[run.RelayRun] = true
			}
		}
		for _, r := range p.snap.BoardRuns.Value {
			at, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
			if r.State != "ended" || err != nil || m.cfg.Now().Sub(at) > time.Hour {
				continue
			}
			glyph := "~ stopped"
			if r.Outcome == "pass" {
				glyph = "✓ pass"
			}
			if r.Outcome == "fail" {
				glyph = "✗ fail"
			}
			if landed[r.ID] && r.Outcome == "pass" {
				glyph = "✓ landed"
			}
			began, _ := time.Parse(time.RFC3339Nano, r.BeganAt)
			counts := roleCounts(ledgerTreeSteps(r.Steps))
			cells := []string{glyph, strings.Join(r.Items, ","), elapsed(at.Sub(began)), "ended " + elapsed(m.cfg.Now().Sub(at)) + " ago", fmt.Sprintf("impl %d  review %d  gate %d", counts["implementer"], counts["reviewer"], counts["gate"])}
			rows = append(rows, ovRow{ledgerID: r.ID, project: i, name: p.reg.Name, style: m.cfg.Theme.Dim, text: strings.Join(cells, "  "), cells: cells, ended: at})
		}
	}
	slices.SortStableFunc(rows, func(a, b ovRow) int { return b.ended.Compare(a.ended) })
	return rows
}

func runClock(r slicercli.RelayRun, now time.Time) time.Time {
	if r.State == "stale" || r.State == "ended" {
		if at, e := time.Parse(time.RFC3339Nano, r.UpdatedAt); e == nil {
			return at
		}
	}
	return now
}

func (m *Model) boardRowParts(r slicercli.RelayRun, steps []runTreeStep) []relayRowPart {
	pipeline := m.pipeline(steps, r.State == "stale", true)
	bar := stageBar(steps, m.treeFrame, r.State != "stale" && boardLive(r.State))
	parts := []relayRowPart{{header: bar, text: bar}, {header: pipeline, text: pipeline}}
	if heard, _ := m.lastHeard(r); heard != "" {
		parts = append(parts, relayRowPart{header: heard, text: heard})
	}
	return parts
}

func engineBoard(r source.EngineRun) (slicercli.RelayRun, []runTreeStep) {
	updated := r.CreatedAt
	var steps []runTreeStep
	for _, role := range r.Roles {
		phase := "start"
		if !role.EndedAt.IsZero() {
			phase = "done"
			if role.Exit != "ok" {
				phase = "fail"
			}
		}
		steps = append(steps, runTreeStep{Step: slicercli.Step{Role: role.Role, Phase: phase}, round: role.Round})
		if role.StartedAt.After(updated) {
			updated = role.StartedAt
		}
		if role.EndedAt.After(updated) {
			updated = role.EndedAt
		}
	}
	stamp := ""
	if !updated.IsZero() {
		stamp = updated.Format(time.RFC3339Nano)
	}
	return slicercli.RelayRun{State: r.State, UpdatedAt: stamp}, steps
}

// boardLive excludes finished or suspended runs from live-only board cues.
func boardLive(state string) bool {
	switch state {
	case "ended", "stopped", "paused", "committed":
		return false
	default:
		return true
	}
}
