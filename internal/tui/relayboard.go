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
		case "wait":
			text = role + " waiting"
			style = m.cfg.Theme.Warn
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
	return m.pipelineLineRound(steps, stale, width, true)
}

// pipelineLineRound is pipelineLine; showRound is false when the bar's own
// round word already says it. The round is the fix rounds plus one.
func (m *Model) pipelineLineRound(steps []runTreeStep, stale bool, width int, showRound bool) string {
	line := m.pipeline(steps, stale, false)
	n := roleCounts(steps)["implementer"]
	round := fmt.Sprintf("round %d", fixRounds(steps)+1)
	if showRound && n > 0 && lipgloss.Width(line)+2+len(round) <= width {
		return line + strings.Repeat(" ", width-lipgloss.Width(line)-len(round)) + m.cfg.Theme.Dim.Render(round)
	}
	// Drop the round, then whole roles from the right at very narrow widths.
	parts := strings.Split(line, " > ")
	for len(parts) > 0 && lipgloss.Width(strings.Join(parts, " > ")) > width {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, " > ")
}

// A live run is quiet past heardAmber (amber, the cue "!") and stalled past
// heardRed (red, the cue "!!"): heardRed is slicer's stale cutoff, the glossary's
// "30 minutes with no event". The cue is a word so mono keeps it.
const (
	heardAmber = 10 * time.Minute
	heardRed   = 30 * time.Minute
)

// clockOrDay is the time of day in now's zone, with the day ("Oct 27 16:42")
// when it is not now's day.
func clockOrDay(at, now time.Time) string {
	at = at.In(now.Location())
	if at.Year() == now.Year() && at.YearDay() == now.YearDay() {
		return at.Format("15:04")
	}
	return at.Format("Jan 2 15:04")
}

// heardCell is lastHeard for a row, where room is short: a run that waits says
// how long ("wait 19m"), not the clock. The pane and the detail line keep the
// clock (S154).
func (m *Model) heardCell(r slicercli.RelayRun) (string, lipgloss.Style) {
	if since, ok := waitingSince(r); ok {
		return "wait " + elapsed(m.cfg.Now().Sub(since)), m.cfg.Theme.Dim
	}
	return m.lastHeard(r)
}

// lastHeard is the quiet-time cell of a run. A run that waits on the owner, or
// has a step in phase wait, has not gone quiet: it says when the wait began
// instead, in the clock's zone and dim (S154).
func (m *Model) lastHeard(r slicercli.RelayRun) (string, lipgloss.Style) {
	if since, ok := waitingSince(r); ok {
		return "waiting since " + clockOrDay(since, m.cfg.Now()), m.cfg.Theme.Dim
	}
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
	if age > heardAmber {
		cue = "! "
		style = m.cfg.Theme.Warn
	}
	if age > heardRed || r.State == "stale" {
		cue = "!! "
		style = m.cfg.Theme.Error
	}
	return cue + "heard " + elapsed(age), style
}

// waitingSince is when the run's wait on the owner began. An engine run that
// is awaiting approval or paused carries its wait start in UpdatedAt (see
// engineLedger). Any other run takes the newest wait step's updated_at, or its
// own when the step has none. A run that is neither live nor waiting on the
// owner (ended, stopped, committed) is not waiting, whatever its ledger kept.
func waitingSince(r slicercli.RelayRun) (time.Time, bool) {
	if !boardLive(r.State) && !ownerWaitState(r.State) {
		return time.Time{}, false
	}
	if !ownerWaitState(r.State) {
		if !ledgerWaiting(r) {
			return time.Time{}, false
		}
		for i := len(r.Steps) - 1; i >= 0; i-- {
			if r.Steps[i].Phase != "wait" {
				continue
			}
			if at, err := time.Parse(time.RFC3339Nano, r.Steps[i].UpdatedAt); err == nil {
				return at, true
			}
			break
		}
	}
	at, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	return at, err == nil
}

// waitStart is when an engine run's wait on the owner began. Older records have
// no durable wait-start timestamp, so their last update stands in. It is zero
// when the record has neither.
func waitStart(r source.EngineRun) time.Time {
	if !r.WaitStartedAt.IsZero() {
		return r.WaitStartedAt
	}
	return r.UpdatedAt
}

// engineLedger is the ledger the board shows for an engine run. The engine owns
// the state; a linked ledger supplies only its steps. A run that waits on the
// owner has no quiet time to report, so UpdatedAt carries the start of the wait.
func engineLedger(ledger slicercli.RelayRun, r source.EngineRun) slicercli.RelayRun {
	ledger.State = r.State
	if at := waitStart(r); ownerWaitState(r.State) && !at.IsZero() {
		ledger.UpdatedAt = at.Format(time.RFC3339Nano)
	}
	return ledger
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
						return engineLedger(ledger, r)
					}
				}
				ledger, _ := engineBoard(r)
				return engineLedger(ledger, r)
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
	// The bar says "fix N" or "gate" once there is one to say, so only a run
	// without a round word keeps a round on this line.
	lines := []string{bar, m.pipelineLineRound(steps, !live, width, roundWord(steps) == "")}
	// Last heard sits on the bar's line, after its round word. A
	// pane too narrow for both keeps it on a line of its own.
	if heard, style := m.lastHeard(r); heard != "" {
		if !strings.HasPrefix(heard, "waiting since ") {
			heard = strings.Replace(heard, "heard ", "last heard ", 1) + " ago"
		}
		if lipgloss.Width(bar)+2+lipgloss.Width(heard) <= width {
			lines[0] += "  " + style.Render(heard)
		} else {
			lines = append(lines, style.Render(treeClip(heard, width)))
		}
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
			lines = append(lines, m.cfg.Theme.Dim.Render(treeClip(fmt.Sprintf(". %-*s queued", roleWidth, role), width)))
		}
	}
	return lines
}

// recentActiveTime counts the union of completed worker intervals. A step's
// updated_at is its end only once its phase is done or fail.
func recentActiveTime(steps []slicercli.Step) (time.Duration, bool) {
	type interval struct{ begin, end time.Time }
	var intervals []interval
	for _, step := range steps {
		if step.Phase != "done" && step.Phase != "fail" {
			continue
		}
		switch step.Role {
		case "implementer", "reviewer", "gate", "architect":
		default:
			continue
		}
		begin, beginErr := time.Parse(time.RFC3339Nano, step.BeganAt)
		end, endErr := time.Parse(time.RFC3339Nano, step.UpdatedAt)
		if beginErr != nil || endErr != nil || end.Before(begin) {
			continue
		}
		intervals = append(intervals, interval{begin, end})
	}
	if len(intervals) == 0 {
		return 0, false
	}
	slices.SortFunc(intervals, func(a, b interval) int { return a.begin.Compare(b.begin) })
	current := intervals[0]
	var active time.Duration
	for _, next := range intervals[1:] {
		if next.begin.After(current.end) {
			active += current.end.Sub(current.begin)
			current = next
		} else if next.end.After(current.end) {
			current.end = next.end
		}
	}
	return active + current.end.Sub(current.begin), true
}

func (m *Model) recentRows() []ovRow {
	var rows []ovRow
	for i, p := range m.projects {
		if !p.loaded || p.setup() || (m.active >= 0 && i != m.active) {
			continue
		}
		// The ledger run of a slice that ork landed reads "landed" (S119).
		// A landing whose cleanup failed says so (S148).
		landed, cleanupFailed := map[string]bool{}, map[string]bool{}
		for _, run := range p.snap.EngineRuns {
			if run.Landed && run.RelayRun != "" {
				landed[run.RelayRun] = true
				cleanupFailed[run.RelayRun] = run.CleanupFailed != ""
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
				if cleanupFailed[r.ID] {
					glyph += ", cleanup failed"
				}
			}
			began, _ := time.Parse(time.RFC3339Nano, r.BeganAt)
			duration := elapsed(at.Sub(began))
			if active, ok := recentActiveTime(r.Steps); ok {
				duration += " (active " + elapsed(active) + ")"
			}
			counts := map[string]int{}
			for _, step := range r.Steps {
				counts[step.Role]++
			}
			recorded := func(role string) string {
				if counts[role] == 0 {
					return "-"
				}
				return fmt.Sprint(counts[role])
			}
			cells := []string{glyph, strings.Join(r.Items, ","), duration, "ended " + elapsed(m.cfg.Now().Sub(at)) + " ago", fmt.Sprintf("impl %d  review %s  gate %s", counts["implementer"], recorded("reviewer"), recorded("gate"))}
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
	// Last heard follows the bar (and its round) and goes before the pipeline.
	parts := []relayRowPart{{header: bar, text: bar}}
	if heard, _ := m.lastHeard(r); heard != "" {
		parts = append(parts, relayRowPart{header: heard, text: heard})
	}
	return append(parts, relayRowPart{header: pipeline, text: pipeline})
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

// ownerWaitState reports the engine states in which the run waits on the owner
// and nothing runs.
func ownerWaitState(state string) bool {
	return state == "awaiting-approval" || state == "paused"
}

// boardLive excludes finished, suspended and owner-wait runs from live-only
// board cues: the bar, the queued rows, the spinner and the activity line stay
// still while nothing runs (S154).
func boardLive(state string) bool {
	switch state {
	case "ended", "stopped", "paused", "committed", "awaiting-approval":
		return false
	default:
		return true
	}
}

// ledgerOwnerQuestion identifies an unanswered owner question on an external
// ledger wait. A plain wait or approval wait has no question to show.
func ledgerOwnerQuestion(r slicercli.RelayRun) *slicercli.LedgerQuestion {
	if r.State == "ended" {
		return nil
	}
	for _, s := range r.Steps {
		if s.Phase == "wait" && s.WaitingOn == "owner" && s.Question != nil && s.Question.AnsweredAt == "" && len(s.Question.Answer) == 0 {
			return s.Question
		}
	}
	return nil
}
