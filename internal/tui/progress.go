// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

// pipelinePhases ignores earlier-round results after a fix round starts. A
// ui step (the UI pass) is part of the reviewer stage.
func pipelinePhases(steps []runTreeStep) map[string]string {
	steps = laneSteps(steps)
	latestImpl := -1
	rounds := 0
	for i, s := range steps {
		if s.Role == "implementer" {
			latestImpl = i
			rounds++
		}
	}
	phases := map[string]string{}
	ui, gated := "", false
	for i, s := range steps {
		if rounds > 1 && i < latestImpl {
			continue
		}
		if s.Role == "ui" {
			// A UI pass that starts after the gate is not part of the review.
			if !gated {
				ui = s.Phase
			}
			continue
		}
		gated = gated || s.Role == "gate"
		phases[s.Role] = s.Phase
	}
	// The UI pass runs beside the reviewer and has no stage of its own: it
	// counts in the reviewer's stage (S136). The stage is running while either
	// runs, then failed if either failed.
	if ui != "" {
		switch reviewer := phases["reviewer"]; {
		case reviewer == "":
			phases["reviewer"] = ui
		case ui == "start" || reviewer == "start":
			phases["reviewer"] = "start"
		case ui == "wait" || reviewer == "wait":
			phases["reviewer"] = "wait"
		case ui == "fail" || reviewer == "fail":
			phases["reviewer"] = "fail"
		}
	}
	return phases
}

func stageBar(steps []runTreeStep, frame int, live bool) string {
	phases := pipelinePhases(steps)
	if !live {
		frame = 0
	}
	var b strings.Builder
	b.WriteByte('[')
	for _, role := range pipelineRoles(laneSteps(steps)) {
		cell := ".."
		switch phases[role] {
		case "done":
			cell = "##"
		case "wait":
			cell = "--"
		case "fail":
			cell = "!!"
		case "start", "stale", "ended", "paused", "stopped", "committed":
			cell = []string{"~~", "~=", "==", "=~"}[frame%4]
		}
		b.WriteString(cell)
	}
	b.WriteByte(']')
	if word := roundWord(steps); word != "" {
		b.WriteString(" " + word)
	}
	return b.String()
}

func sparkline(counts []int, ascii bool) string {
	glyphs := []rune("▁▂▃▄▅▆▇█")
	if ascii {
		glyphs = []rune("._-=+#")
	}
	peak := 0
	for _, n := range counts {
		peak = max(peak, n)
	}
	var b strings.Builder
	for _, n := range counts {
		level := 0
		if peak > 0 {
			level = max(n, 0) * (len(glyphs) - 1) / peak
		}
		if n > 0 {
			level = max(level, 1)
		}
		b.WriteRune(glyphs[level])
	}
	return b.String()
}

// frameVisible uses the same rows and pane targets as the Overview.
func (m *Model) frameVisible() bool {
	if m.showHelp {
		return false
	}
	if m.pane != nil {
		r, _, ok := m.paneRow()
		if ok && boardLive(r.state) && r.state != "stale" {
			state := m.paneBoardRun().State
			if boardLive(state) && state != "stale" {
				return true
			}
		}
	}
	if m.view != 0 {
		return false
	}
	_, rows := m.rows()
	for _, r := range rows {
		if r.engineID != "" && !r.dim {
			return true
		}
		if r.ledgerID != "" && !r.dim && boardLive(r.state) {
			return true
		}
	}
	return false
}

func (m *Model) armFrame() tea.Cmd {
	if m.frameArmed || !m.frameVisible() {
		return nil
	}
	m.frameArmed = true
	return m.cfg.FrameAfter(250 * time.Millisecond)
}

func (m *Model) paneActivity() []int {
	if m.pane == nil || m.pane.ledger {
		return nil
	}
	p := m.byName[m.pane.project]
	if p == nil {
		return nil
	}
	for _, r := range p.snap.EngineRuns {
		if r.ID == m.pane.run && r.Section(p.snap.EngineRuns) == source.SectionRunning {
			return r.Activity
		}
	}
	return nil
}

// reviewSteps are the lane steps that make up a review round, in order: the
// implementer, the reviewers and the gate. An orchestrator or a UI pass takes
// no part in the count.
func reviewSteps(steps []runTreeStep) []runTreeStep {
	var out []runTreeStep
	for _, s := range laneSteps(steps) {
		switch s.Role {
		case "implementer", "reviewer", "architect", "gate":
			out = append(out, s)
		}
	}
	return out
}

// fixRounds counts the fix rounds of a run (S145): the implementer rounds that
// follow a review. A run of implementer steps in a row is one round, so a
// resumed step does not count again. The first round is not a fix round, and
// neither is a finish step, the implementer step after a passed gate.
//
// Every round but the last has a review or a gate after it. The last has none
// yet, so what came before it decides:
//   - after a review or an architect step it is a fix round;
//   - after a gate it is a fix round when the gate step failed or its own task
//     says fix (and not finish);
//   - for an engine run the engine's state decides after a gate (engineFix): the
//     engine ends a failing gate step done and need not say fix in the task, but
//     its state is implement, checks or review in a fix round and finish, commit
//     or later in the finish step.
//
// A ledger-only run whose gate FAIL was recorded as done, with a task that does
// not say fix, reads one fix round low until the next review begins.
func fixRounds(steps []runTreeStep) int {
	seq := reviewSteps(steps)
	rounds, fixes := 0, 0
	for i := 0; i < len(seq); {
		if seq[i].Role != "implementer" {
			i++
			continue
		}
		j := i
		for j < len(seq) && seq[j].Role == "implementer" {
			j++
		}
		rounds++
		switch {
		case rounds == 1:
		case j < len(seq):
			fixes++
		default:
			prev, last := seq[i-1], seq[j-1]
			task := strings.ToLower(last.Task)
			fix, known := engineFix(last.engineState)
			switch {
			case prev.Role != "gate" || prev.Phase == "fail":
				fixes++
			case known:
				if fix {
					fixes++
				}
			case strings.Contains(task, "fix") && !strings.Contains(task, "finish"):
				fixes++
			}
		}
		i = j
	}
	return fixes
}

// engineFix reads an engine run's state: fix is true in the states of an
// implementer fix round (implement, checks and review, the engine's own steps
// after it), false from the finish step on. known is false for a run with no
// engine state and for paused and stopped runs, which say nothing.
func engineFix(state string) (fix, known bool) {
	switch state {
	case runrec.StateImplement, runrec.StateChecks, runrec.StateReview, runrec.StateArchitect, runrec.StateGate:
		return true, true
	case runrec.StateFinish, runrec.StateCommit, runrec.StateAwaitingApproval, runrec.StateCommitted:
		return false, true
	}
	return false, false
}

// roundWord is the round as the owner reads it (S145): "gate" while the gate is
// running, otherwise "fix N" once a fix round has begun. The first round has no
// word. A gate that has ended says nothing: the finish step or a fix round
// follows it.
func roundWord(steps []runTreeStep) string {
	if seq := reviewSteps(steps); len(seq) > 0 && seq[len(seq)-1].Role == "gate" && seq[len(seq)-1].Phase == "start" {
		return "gate"
	}
	if n := fixRounds(steps); n > 0 {
		return fmt.Sprintf("fix %d", n)
	}
	return ""
}
