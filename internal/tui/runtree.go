// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// runTreeStep keeps engine-only display fields out of the ledger wire.
type runTreeStep struct {
	slicercli.Step
	round         int
	hasUsage      bool
	usageKnown    bool
	costKnown     bool
	tailTruncated bool
	tokens        int
	cost          float64
	live          bool
	tail          string
	// engineState is the state of the engine run these steps belong to, set on
	// every step of an engine run's steps (S145); "" for a ledger-only run.
	engineState string
}

// withEngineState marks the steps as an engine run's, in the engine's state.
func withEngineState(steps []runTreeStep, state string) []runTreeStep {
	out := append([]runTreeStep(nil), steps...)
	for i := range out {
		out[i].engineState = state
	}
	return out
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
						run.State = r.State
						return m.roleDetails(boardTreeSteps(run), r)
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
				stepID := role.Step
				if stepID == "" {
					stepID = fmt.Sprintf("role-%d", i)
				}
				steps = append(steps, runTreeStep{Step: slicercli.Step{ID: stepID, Role: role.Role, Vendor: role.Vendor, Model: role.Model, Reasoning: role.Effort, Phase: phase, BeganAt: begin, UpdatedAt: end}, round: role.Round})
			}
			return m.roleDetails(steps, r)
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
		phaseWidth = max(phaseWidth, lipgloss.Width(treeText(stepPhaseLabel(step.Step))))
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
		case "wait":
			glyph = "?"
			style = m.cfg.Theme.Warn
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
		phase := treeClip(treeText(stepPhaseLabel(s.Step)), phaseColumns)
		phase += strings.Repeat(" ", max(phaseColumns-lipgloss.Width(phase), 0))
		parts := []string{glyph, role, phase}
		if began, err := time.Parse(time.RFC3339Nano, s.BeganAt); err == nil && s.Phase != "wait" {
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
		cost := "$-"
		if s.costKnown {
			cost = fmt.Sprintf("$%.2f", s.cost)
		}
		tokens := "-"
		if s.usageKnown {
			tokens = fmt.Sprintf("%d", s.tokens)
		}
		usage := fmt.Sprintf("tokens %s  %s", tokens, cost)
		running := s.live && s.Phase == "start"
		// Finished and interrupted usage is the first whole field to drop.
		if s.hasUsage && !running && lipgloss.Width(text)+2+len(usage) <= available {
			text += "  " + usage
		}
		lines = append(lines, prefix+style.Render(treeClip(text, available)))
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
		if s.hasUsage && running {
			lines = append(lines, prefix+m.cfg.Theme.Dim.Render(treeClip("    "+usage, available)))
		}
		if s.Task != "" {
			lines = append(lines, prefix+m.cfg.Theme.Dim.Render(treeClip("    "+treeText(s.Task), available)))
		}
		if running && !stale && s.tail != "" {
			for _, line := range tailRows(s.tail, max(available-6, 1), s.tailTruncated) {
				lines = append(lines, prefix+m.cfg.Theme.Dim.Render("    > "+line))
			}
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

// roleDetails joins transient engine data onto the ledger tree by step ID.
// Source-less ledger steps have no usage or tail. A UI pass step, which the
// ledger records under its own role name and shows its round.
func (m *Model) roleDetails(steps []runTreeStep, r source.EngineRun) []runTreeStep {
	steps = withEngineState(steps, r.State)
	active := -1
	recorded := 0
	for i, role := range r.Roles {
		recorded += role.Tokens
		if role.EndedAt.IsZero() {
			active = i
		}
	}
	used := map[int]bool{}
	for i := range steps {
		for j, role := range r.Roles {
			if used[j] {
				continue
			}
			match := role.Step != "" && role.Step == steps[i].ID
			if role.Step == "" {
				match = role.Role == steps[i].Role && (steps[i].round == 0 || role.Round == steps[i].round)
			}
			if !match {
				continue
			}
			used[j] = true
			if role.Role == "ui" && steps[i].round == 0 {
				steps[i].round = role.Round
			}
			steps[i].hasUsage = true
			steps[i].tokens = role.Tokens
			steps[i].cost = role.CostUSD
			steps[i].usageKnown = !role.EndedAt.IsZero() || role.Tokens != 0
			steps[i].costKnown = !role.EndedAt.IsZero() && role.Vendor == "claude"
			steps[i].live = role.EndedAt.IsZero() && boardLive(r.State) && m.paneBoardRun().State != "stale"
			if j == active && boardLive(r.State) && r.Section([]source.EngineRun{r}) == source.SectionRunning && m.paneBoardRun().State != "stale" {
				steps[i].tokens = max(r.Tokens-recorded, 0)
				steps[i].tail = r.Tail
				steps[i].tailTruncated = r.TailTruncated
				steps[i].usageKnown = r.LiveUsageKnown || steps[i].tokens != 0
			}
			break
		}
	}
	return steps
}

// tailRows wraps nonblank assistant text and keeps the newest three physical
// rows. Only the first row marks text removed by the buffer or display window.
func tailRows(text string, width int, truncated bool) []string {
	var rows []string
	for _, line := range strings.Split(text, "\n") {
		if line = treeText(line); line != "" {
			rows = append(rows, wrapText(line, width)...)
		}
	}
	if len(rows) > 3 {
		rows = rows[len(rows)-3:]
		truncated = true
	}
	if len(rows) > 0 && truncated {
		marker := "..."
		if width < 3 {
			marker = strings.Repeat(".", max(width, 0))
		}
		first := []rune(rows[0])
		for len(first) > 0 && lipgloss.Width(string(first))+len(marker)+1 > width {
			first = first[1:]
		}
		rows[0] = marker
		if len(first) > 0 {
			rows[0] += " " + string(first)
		}
	}
	return rows
}

func stepPhaseLabel(s slicercli.Step) string {
	if s.Phase == "wait" {
		if s.WaitingOn != "" {
			return "waiting on " + s.WaitingOn
		}
		return "waiting"
	}
	return s.Phase
}

func ledgerWaiting(r slicercli.RelayRun) bool {
	for _, s := range r.Steps {
		if s.Phase == "wait" {
			return true
		}
	}
	return r.Counts.Waiting > 0
}
