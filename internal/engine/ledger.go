// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strings"

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
	id, err := e.d.Slicer.RelayAsk(ctx, e.wr, rec.Worktree, rec.RelayRun, rec.OrchestratorStep, p.Key, *p.Ask)
	if err == nil {
		e.run.Update(func(r *runrec.Record) {
			if r.Pending != nil {
				r.Pending.LedgerQuestion = id
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
