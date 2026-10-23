// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/slicercli"
)

// runTreeStep keeps engine-only display fields out of the ledger wire.
type runTreeStep struct {
	slicercli.Step
	round int
}

func ledgerTreeSteps(steps []slicercli.Step) []runTreeStep {
	out := make([]runTreeStep, len(steps))
	for i, step := range steps {
		out[i].Step = step
	}
	return out
}

// paneSteps prefers the linked ledger when present. A missing ledger falls
// back to engine roles, including ended runs absent from the active snapshot.
func (m *Model) paneSteps() []runTreeStep {
	p := m.byName[m.pane.project]
	if p == nil {
		return nil
	}
	ledger := m.pane.run
	if !m.pane.ledger {
		for _, r := range p.snap.EngineRuns {
			if r.ID != m.pane.run {
				continue
			}
			if r.RelayRun != "" {
				for _, run := range boardLedgers(p.snap) {
					if run.ID == r.RelayRun {
						return boardTreeSteps(run)
					}
				}
			}
			var steps []runTreeStep
			for i, role := range r.Roles {
				phase := "start"
				switch r.State {
				case "stopped", "paused", "committed":
					phase = r.State
				}
				end := ""
				if !role.EndedAt.IsZero() {
					phase = "done"
					if role.Exit != "ok" {
						phase = "fail"
					}
					end = role.EndedAt.Format(time.RFC3339Nano)
				}
				begin := ""
				if !role.StartedAt.IsZero() {
					begin = role.StartedAt.Format(time.RFC3339Nano)
				}
				steps = append(steps, runTreeStep{Step: slicercli.Step{ID: fmt.Sprintf("role-%d", i), Role: role.Role, Vendor: role.Vendor, Model: role.Model, Reasoning: role.Effort, Phase: phase, BeganAt: begin, UpdatedAt: end}, round: role.Round})
			}
			return steps
		}
		return nil
	}
	for _, run := range boardLedgers(p.snap) {
		if run.ID == ledger {
			return boardTreeSteps(run)
		}
	}
	return nil
}

// runTreeLines preserves sibling order and visits every step once, including
// disconnected cycles. Only start is running; unknown phases stay neutral.
func (m *Model) runTreeLines(steps []runTreeStep, width, frame int) []string {
	return m.runTreeLinesWithRoleWidth(steps, width, frame, treeRoleWidth(steps))
}

func treeRoleWidth(steps []runTreeStep) int {
	width := 0
	for _, s := range steps {
		width = max(width, lipgloss.Width(treeText(s.Role)))
	}
	return width
}

func (m *Model) runTreeLinesWithRoleWidth(steps []runTreeStep, width, frame, roleWidth int) []string {
	counts := roleCounts(steps)
	latest := map[string]int{}
	for i, s := range steps {
		latest[s.Role] = i
	}
	phaseWidth := 0
	for _, step := range steps {
		phaseWidth = max(phaseWidth, lipgloss.Width(treeText(step.Phase)))
	}
	byID := make(map[string]int, len(steps))
	for i, s := range steps {
		if s.ID != "" {
			byID[s.ID] = i
		}
	}
	children := make(map[int][]int)
	var roots []int
	for i, s := range steps {
		parent, ok := byID[s.Parent]
		if s.Parent == "" || !ok {
			roots = append(roots, i)
		} else {
			children[parent] = append(children[parent], i)
		}
	}
	now := m.cfg.Now()
	stale := m.pane != nil && m.paneBoardRun().State == "stale"
	visited := make([]bool, len(steps))
	var lines []string
	var visit func(int, int)
	visit = func(i, depth int) {
		if visited[i] {
			return
		}
		visited[i] = true
		s := steps[i]
		prefix := strings.Repeat("  ", min(depth, max((width-12)/2, 0)))
		glyph := "."
		style := lipgloss.NewStyle()
		switch s.Phase {
		case "start":
			glyph = string("|/-\\"[frame%4])
			style = m.cfg.Theme.Heading
		case "done":
			glyph = "+"
			style = m.cfg.Theme.Dim
		case "stale", "ended":
			glyph = "~"
			style = m.cfg.Theme.Dim
		case "fail":
			glyph = "!"
			style = m.cfg.Theme.Error
		}
		if stale {
			style = m.cfg.Theme.Dim
		}
		available := max(width-len(prefix), 1)
		roleColumns := min(roleWidth, max(available-8, 1))
		role := treeClip(treeText(s.Role), roleColumns)
		role += strings.Repeat(" ", max(roleColumns-lipgloss.Width(role), 0))
		phaseColumns := min(phaseWidth, max(available-roleColumns-4, 1))
		phase := treeClip(treeText(s.Phase), phaseColumns)
		phase += strings.Repeat(" ", max(phaseColumns-lipgloss.Width(phase), 0))
		parts := []string{glyph, role, phase}
		if began, err := time.Parse(time.RFC3339Nano, s.BeganAt); err == nil {
			end := now
			valid := s.Phase == "start"
			if !valid {
				end, err = time.Parse(time.RFC3339Nano, s.UpdatedAt)
				valid = err == nil
			}
			if valid {
				parts = append(parts, elapsed(end.Sub(began)))
			}
		}
		text := strings.TrimRight(strings.Join(parts, " "), " ")
		var details []string
		for _, field := range []string{s.Vendor, s.Model, s.Reasoning} {
			if field != "" {
				details = append(details, treeText(field))
			}
		}
		if s.round > 0 {
			details = append(details, fmt.Sprintf("round %d", s.round))
		}
		if counts[s.Role] > 1 && latest[s.Role] == i {
			details = append(details, fmt.Sprintf("x%d", counts[s.Role]))
		}
		if len(details) > 0 {
			text += "  " + strings.Join(details, " ")
		}
		lines = append(lines, prefix+style.Render(treeClip(text, available)))
		if s.Task != "" {
			lines = append(lines, prefix+m.cfg.Theme.Dim.Render(treeClip("    "+treeText(s.Task), available)))
		}
		if s.Role == "gate" && s.Phase == "fail" {
			round := 0
			for _, before := range laneSteps(steps[:i+1]) {
				if before.Role == "implementer" {
					round++
				}
			}
			failure := fmt.Sprintf("    ✗ gate FAIL -> implementer round %d", round+1)
			if task := treeText(s.Task); task != "" {
				failure += ": " + task
			}
			lines = append(lines, prefix+style.Render(treeClip(failure, available)))
		}
		for _, child := range children[i] {
			visit(child, depth+1)
		}
	}
	for _, i := range roots {
		visit(i, 0)
	}
	for i := range steps {
		if !visited[i] {
			visit(i, 0)
		}
	}
	return lines
}

func treeText(s string) string { return strings.Join(strings.Fields(s), " ") }

// clip marks truncation with three columns. Tiny panes have less room.
func treeClip(s string, width int) string {
	if width < 3 {
		return strings.Repeat(".", max(width, 0))
	}
	return clip(s, width)
}

func boardTreeSteps(run slicercli.RelayRun) []runTreeStep {
	steps := ledgerTreeSteps(run.Steps)
	if run.State == "stale" || run.State == "ended" {
		for i := range steps {
			if steps[i].Phase == "start" {
				steps[i].Phase = run.State
				steps[i].UpdatedAt = run.UpdatedAt
			}
		}
	}
	return steps
}
