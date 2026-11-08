// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// NotResumable reports why a stopped run cannot be resumed from what its
// record says, or "" when it can (S296, *proposed*). It reads no file: the
// worktree is checked when the run is resumed. A record from before
// stopped_from has none, and the worktree check decides it.
func NotResumable(rec runrec.Record) string {
	switch {
	case rec.State != runrec.StateStopped:
		return "run " + rec.ID + " is " + rec.State + ", not stopped"
	case rec.Commit != nil:
		return "run " + rec.ID + " committed its slice; there is nothing to resume"
	case rec.StoppedFrom == runrec.StateSetup:
		return "run " + rec.ID + " stopped during setup, before it had a worktree; start a new run"
	case rec.StoppedFrom == runrec.StateQueued:
		return "run " + rec.ID + " never started; start a new run"
	}
	return ""
}

// hasImplementerRound reports whether an implementer round ran in the run.
func hasImplementerRound(rec runrec.Record) bool {
	for _, r := range rec.Roles {
		if r.Role == RoleImplementer {
			return true
		}
	}
	return false
}

// waveKey carries the wave a resume joins (WithWave).
type waveKey struct{}

// WithWave returns a context that makes a resume of a stopped run join the
// wave: a start that resumes the item's stopped run (S296, *proposed*) puts
// the run in the new wave, so wave.stop of that wave reaches it. The wave is
// set in the same record update as the resume.
func WithWave(ctx context.Context, wave string) context.Context {
	return context.WithValue(ctx, waveKey{}, wave)
}

// CanResume reports, as a refusal, why the stopped run cannot be resumed now,
// or nil. It makes the checks a resume makes before it changes anything: the
// run is stopped with no commit and did not stop in setup or while queued, no
// role is still running, and the worktree path exists and is this run's
// worktree and branch (the same check ensureWorktree makes; it never creates
// a worktree). It reads the record, the file system and git, and writes
// nothing.
func (e *Engine) CanResume(ctx context.Context) error {
	return e.checkResumable(ctx, e.run.Snapshot())
}

func (e *Engine) checkResumable(ctx context.Context, rec runrec.Record) error {
	if why := NotResumable(rec); why != "" {
		return refusef("%s", why)
	}
	for _, role := range rec.Roles {
		if role.Running() {
			return refusef("run %s has a role still running; reconcile it before retrying", rec.ID)
		}
	}
	// The path is checked first: ensureWorktree would create a missing one.
	if info, err := os.Stat(rec.Worktree); err != nil || !info.IsDir() {
		return refusef("run %s has no worktree at %s; ork dismiss %s to start a new run", rec.ID, rec.Worktree, rec.ID)
	}
	if err := e.ensureWorktree(ctx, rec); err != nil {
		return refusef("run %s worktree %s does not match the run (%s); ork dismiss %s to start a new run", rec.ID, rec.Worktree, strings.Join(strings.Fields(err.Error()), " "), rec.ID)
	}
	return nil
}

// resumeStopped continues a stopped run from its worktree (S296, *proposed*).
// It grants rounds (2 by default), begins a new ledger run and re-enters the
// checks, or implement when no implementer round has run. The engine must
// have been loaded with the project as it is now, so a fixed check command
// takes effect. The caller holds e.mu.
func (e *Engine) resumeStopped(ctx context.Context, rec runrec.Record, note string, rounds []int) error {
	if why := NotResumable(rec); why != "" {
		return refusef("%s", why)
	}
	grant := 2
	if len(rounds) > 0 && rounds[0] != 0 {
		grant = rounds[0]
	}
	if grant < 1 || grant > int(^uint(0)>>1)-rec.Round-1 {
		return refusef("rounds must be a positive count within the round limit")
	}
	if note != "" {
		return refusef("a stopped run resumes without a note; run ork retry %s [--rounds N]", rec.ID)
	}
	if err := e.checkResumable(ctx, rec); err != nil {
		return err
	}
	// Entering the checks, the entered round is not an implementer round, so N
	// fix rounds follow it. Entering implement, round 1 is itself the first of
	// the N rounds.
	state, round, limit := runrec.StateImplement, rec.Round, rec.Round+grant-1
	if hasImplementerRound(rec) {
		// The work is there: check it. The checks take a round number of
		// their own, so the earlier check log is kept.
		state, round = runrec.StateChecks, rec.Round+1
		limit = round + grant
	}
	wave, _ := ctx.Value(waveKey{}).(string)
	err := e.run.Update(func(r *runrec.Record) {
		r.DismissedAt = time.Time{}
		r.StoppedFrom = ""
		r.Reason = ""
		r.Pending = nil
		r.QueuedQuestions = nil
		r.Resume = nil
		r.Round = round
		r.RoundLimit = limit
		if wave != "" {
			r.Wave = wave
		}
		if state == runrec.StateChecks {
			r.Feedback = nil
		}
		r.State = state
		// The stopped run's ledger run stays ended; the resume has its own.
		r.RelayRun, r.OrchestratorStep = "", ""
	})
	if err != nil {
		return err
	}
	e.beginRelay(ctx, e.run.Snapshot())
	applied(ctx)()
	return e.advance(ctx)
}

// TreeMatchesLastRound reports whether the run's worktree holds what the last
// implementer round left: the fingerprint of its tracked content and
// untracked names equals that round's tree_sha256 (S294). round is that
// round. An error says why the two cannot be compared: no implementer round,
// no fingerprint on it, or git could not read the tree. It reads and writes
// nothing in the worktree. ork run and ork wave start use it to decide
// whether to resume a stopped run (S296, *proposed*; owner decision
// 2026-11-07).
func (e *Engine) TreeMatchesLastRound(ctx context.Context) (round int, match bool, err error) {
	rec := e.run.Snapshot()
	last := -1
	for i, r := range rec.Roles {
		if r.Role == RoleImplementer {
			last = i
		}
	}
	if last < 0 {
		return 0, false, errors.New("no implementer round ran to compare with")
	}
	row := rec.Roles[last]
	if row.TreeSHA256 == "" {
		return row.Round, false, fmt.Errorf("implementer round %d kept no tree fingerprint to compare with", row.Round)
	}
	now, err := e.treeContent(ctx, rec.Worktree)
	if err != nil {
		return row.Round, false, errors.New(strings.Join(strings.Fields(err.Error()), " "))
	}
	return row.Round, now == row.TreeSHA256, nil
}
