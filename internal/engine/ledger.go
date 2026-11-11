// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// orchestratorPhase records the owner's wait or the continuation. Ledger writes
// are advisory and must not prevent the run from proceeding.
func (e *Engine) orchestratorPhase(ctx context.Context, phase, waitingOn string) {
	rec := e.run.Snapshot()
	if rec.RelayRun == "" || rec.OrchestratorStep == "" || rec.Commit != nil {
		return
	}
	e.d.Slicer.RelayStep(ctx, e.wr, rec.Worktree, rec.RelayRun, slicercli.StepSpec{Step: rec.OrchestratorStep, Phase: phase, WaitingOn: waitingOn})
}

// ownerWait runs only once the parallel roles have joined and the first pause
// has been chosen. Queued questions are mirrored when they become current.
func (e *Engine) ownerWait(ctx context.Context) {
	rec := e.run.Snapshot()
	p := rec.Pending
	if rec.State != runrec.StatePaused || p == nil || p.Kind != runrec.PendingQuestion || p.Answer != "" || p.LedgerQuestion != "" {
		return
	}
	e.orchestratorPhase(ctx, "wait", "owner")
	if rec.RelayRun == "" || rec.OrchestratorStep == "" || rec.Commit != nil || p.Ask == nil {
		return
	}
	id, err := e.d.Slicer.RelayAsk(ctx, e.wr, rec.Worktree, rec.RelayRun, rec.OrchestratorStep, p.Key, *p.Ask, true)
	if err == nil {
		e.run.Update(func(r *runrec.Record) {
			if r.Pending != nil {
				r.Pending.LedgerQuestion = id
			}
		})
	}
}

// askFinding asks a kept owner finding in the relay ledger without --blocker, so
// the orchestrator step keeps running, and keeps the question's id on the
// finding. It skips what ownerWait skips: a run with no ledger run or
// orchestrator step, and a run after the commit. Ledger writes are advisory: a
// failed call leaves the finding without an id.
func (e *Engine) askFinding(ctx context.Context, f runrec.Finding) {
	rec := e.run.Snapshot()
	if rec.RelayRun == "" || rec.OrchestratorStep == "" || rec.Commit != nil || f.Ask == nil {
		return
	}
	id, err := e.d.Slicer.RelayAsk(ctx, e.wr, rec.Worktree, rec.RelayRun, rec.OrchestratorStep, f.Key, *f.Ask, false)
	if err != nil {
		return
	}
	k := findingKey(f.Kind, f.Title)
	var answered string
	e.run.Update(func(r *runrec.Record) {
		answered = ""
		for i := range r.Findings {
			if findingKey(r.Findings[i].Kind, r.Findings[i].Title) == k && r.Findings[i].LedgerQuestion == "" {
				r.Findings[i].LedgerQuestion = id
				answered = r.Findings[i].Answer
				return
			}
		}
	})
	// The owner may have answered in ork while the ask was in flight:
	// AnswerFinding then found no id to write to, so the answer goes now.
	if answered != "" {
		if now := e.run.Snapshot(); now.Commit == nil {
			e.d.Slicer.RelayAnswer(ctx, e.wr, now.Worktree, now.RelayRun, id, slicercli.RelayAnswerSpec{Text: answered})
		}
	}
}

// syncLedgerAnswers reads each unanswered owner finding that has a ledger
// question (`slicer relay-run answer RUN QID --get`) and records an answer
// given there on the finding: the option, else the text, and the time slicer
// gives. It files nothing in adj (process.owner-answers, adj 170). An answer the
// owner gave in ork is never overwritten, and a failed read leaves the finding
// as it is. It does nothing after the commit.
func (e *Engine) syncLedgerAnswers(ctx context.Context) {
	rec := e.run.Snapshot()
	if rec.RelayRun == "" || rec.Commit != nil {
		return
	}
	for _, f := range rec.Findings {
		if f.Kind != runrec.FindingOwner || f.LedgerQuestion == "" || f.Answer != "" {
			continue
		}
		q, err := e.d.Slicer.RelayAnswerGet(ctx, rec.Worktree, rec.RelayRun, f.LedgerQuestion)
		if err != nil {
			continue
		}
		answer := q.Answer["option"]
		if answer == "" {
			answer = q.Answer["text"]
		}
		if answer == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, q.AnsweredAt)
		if err != nil {
			at = e.d.now()
		}
		id := f.LedgerQuestion
		e.run.Update(func(r *runrec.Record) {
			for i := range r.Findings {
				if r.Findings[i].LedgerQuestion == id && r.Findings[i].Answer == "" {
					r.Findings[i].Answer, r.Findings[i].AnsweredAt = answer, at
					return
				}
			}
		})
	}
}

func fallbackExit(class ExitClass) bool {
	return class == ExitRateLimited || class == ExitOutOfCredits || class == ExitError
}

// nextEntry skips the exhausted vendor for the remainder of the chain.
func nextEntry(entries []runrec.LineupEntry, current int, class ExitClass) int {
	next := current + 1
	if class == ExitOutOfCredits {
		for next < len(entries) && entries[next].Vendor == entries[current].Vendor {
			next++
		}
	}
	return next
}

func (e *Engine) stepFail(ctx context.Context, rec runrec.Record, id string, class ExitClass, reason string, runErr error) {
	if id == "" || rec.RelayRun == "" {
		return
	}
	s := slicercli.StepSpec{Step: id, Phase: "fail"}
	if rec.Contract >= 10 {
		if reason == "" {
			reason = string(class)
		}
		if runErr != nil {
			reason += ": " + runErr.Error()
		}
		s.Reason = strings.Join(strings.Fields(reason), " ")
	}
	e.d.Slicer.RelayStep(ctx, e.wr, rec.Worktree, rec.RelayRun, s)
}
