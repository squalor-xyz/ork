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
// A committed run that stopped in land is resumable here: ork retry re-lands
// it (land.retry-relands, adj 458; land.retry-scope, adj 459). ork run does
// not; RelandsOnRetry is that exclusion.
func NotResumable(rec runrec.Record) string {
	switch {
	case rec.State != runrec.StateStopped:
		return "run " + rec.ID + " is " + rec.State + ", not stopped"
	case rec.Commit != nil && !RelandsOnRetry(rec):
		return "run " + rec.ID + " committed its slice; there is nothing to resume"
	case rec.StoppedFrom == runrec.StateSetup:
		return "run " + rec.ID + " stopped during setup, before it had a worktree; start a new run"
	case rec.StoppedFrom == runrec.StateQueued:
		return "run " + rec.ID + " never started; start a new run"
	}
	return ""
}

// RelandsOnRetry reports a stopped run that ork retry re-lands (adj 458, adj
// 459): it has a commit and it stopped in land. Any other committed run stays
// refused. ork run and ork wave start do not resume it.
func RelandsOnRetry(rec runrec.Record) bool {
	return rec.State == runrec.StateStopped && rec.Commit != nil && rec.StoppedFrom == runrec.StateLand
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

// resumesFixRound reports a stopped run that holds findings for its next
// implementer round: review, architect, gate or owner findings (S349, adj
// 481). Red checks, or no feedback, re-enter the checks.
func resumesFixRound(rec runrec.Record) bool {
	return rec.Feedback != nil && rec.Feedback.Kind != "checks"
}

// ResumeEntry says, for a message, what a resume of the stopped run enters
// (S349): an implementer fix round carrying the stored findings, the checks,
// or implementer round 1 when no implementer round has run.
func ResumeEntry(rec runrec.Record) string {
	switch {
	case resumesFixRound(rec):
		who := map[string]string{"findings": "the reviewer's", "architect": "the architect's", "fail": "the gate's", "owner": "the owner's"}[rec.Feedback.Kind]
		if who == "" {
			who = "the stored"
		}
		return "starts with an implementer fix round carrying " + who + " findings"
	case hasImplementerRound(rec):
		return "re-enters the checks"
	}
	return "starts at implementer round 1"
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

// waveNameKey carries the saved wave's name a resume joins (WithWaveName).
type waveNameKey struct{}

// WithWaveName returns a context that names the wave a resume joins (S215,
// *proposed*). It matters only together with WithWave: the resume then sets
// the record's wave_name beside its wave, so a run that joins an unnamed wave
// does not keep the name of the wave it left.
func WithWaveName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, waveNameKey{}, name)
}

// CanResume reports, as a refusal, why the stopped run cannot be resumed now,
// or nil. It makes the checks a resume makes before it changes anything: the
// run is stopped and did not stop in setup or while queued, no role is still
// running, and the worktree path exists and is this run's worktree and branch
// (the same check ensureWorktree makes; it never creates a worktree). A
// committed run is accepted only when it stopped in land. The --rounds
// refusal and the refusal that names a missing branch belong to resumeLand.
// It reads the record, the file system and git, and writes nothing.
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
// checks, or implement when no implementer round has run, or when the run
// holds findings for its next implementer round (S349, adj 481, adj 482: that
// round starts, and the gate FAIL count is reset). A committed run
// that stopped in land is the exception: resumeLand re-enters landing and
// resets that one fix round (adj 458, adj 459). The engine must have been
// loaded with the project as it is now, so a fixed check command takes
// effect. The caller holds e.mu.
func (e *Engine) resumeStopped(ctx context.Context, rec runrec.Record, note string, rounds []int) error {
	if why := NotResumable(rec); why != "" {
		return refusef("%s", why)
	}
	if RelandsOnRetry(rec) {
		return e.resumeLand(ctx, rec, note, rounds)
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
	switch {
	case resumesFixRound(rec):
		// The stopped run holds the file the next implementer round would have
		// fixed: review, architect, gate or owner findings (S349, adj 481). Keep
		// it and start that round; it is the first of the granted rounds. A
		// gate FAIL does not carry over: the count starts again (adj 482).
		round = rec.Round + 1
		limit = round + grant - 1
	case hasImplementerRound(rec):
		// The work is there: check it. The checks take a round number of
		// their own, so the earlier check log is kept.
		state, round = runrec.StateChecks, rec.Round+1
		limit = round + grant
	}
	wave, _ := ctx.Value(waveKey{}).(string)
	waveName, _ := ctx.Value(waveNameKey{}).(string)
	err := e.run.Update(func(r *runrec.Record) {
		r.DismissedAt = time.Time{}
		r.StoppedFrom = ""
		r.Reason = ""
		r.Pending = nil
		r.QueuedQuestions = nil
		r.Resume = nil
		r.Round = round
		r.RoundLimit = limit
		r.Fails = 0
		r.ChainFrom = len(r.Roles) // the new round restarts the vendor chain (S255, *proposed*)
		if wave != "" {
			r.Wave = wave
			r.WaveName = waveName
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

// resumeLand re-enters landing for a committed run that stopped there (adj
// 458, adj 459). It resets the one land fix round and does not grant
// implementer rounds or begin a ledger run: the finish step already ended
// that run. When the run is already at the round cap, it raises the limit
// by one and leaves the round unchanged, so that one land fix can start.
// The caller holds e.mu.
func (e *Engine) resumeLand(ctx context.Context, rec runrec.Record, note string, rounds []int) error {
	if note != "" {
		return refusef("a stopped run resumes without a note; run ork retry %s", rec.ID)
	}
	if info, err := os.Stat(rec.Worktree); err != nil || !info.IsDir() {
		return refusef("run %s has no worktree at %s; ork dismiss %s to start a new run", rec.ID, rec.Worktree, rec.ID)
	}
	branch, err := e.gitRead(ctx, rec.Worktree, "symbolic-ref", "--short", "HEAD")
	if err != nil || strings.TrimSpace(branch) != rec.Branch {
		return refusef("run %s has no branch %s; ork dismiss %s to start a new run", rec.ID, rec.Branch, rec.ID)
	}
	if err := e.ensureWorktree(ctx, rec); err != nil {
		return refusef("run %s worktree %s does not match the run (%s); ork dismiss %s to start a new run", rec.ID, rec.Worktree, strings.Join(strings.Fields(err.Error()), " "), rec.ID)
	}
	for _, role := range rec.Roles {
		if role.Running() {
			return refusef("run %s has a role still running; reconcile it before retrying", rec.ID)
		}
	}
	if len(rounds) > 0 && rounds[0] != 0 {
		return refusef("run %s is re-landing; landing gets one fix round, so ork retry %s takes no --rounds", rec.ID, rec.ID)
	}
	// At the cap, bumpRound would pause for more rounds and the one land fix
	// would never start. Raise the limit by one. Under the cap, leave it.
	limit := rec.RoundLimit
	if rec.Round >= e.roundLimit(rec) {
		limit = rec.Round + 1
	}
	err = e.run.Update(func(r *runrec.Record) {
		r.DismissedAt = time.Time{}
		r.StoppedFrom = ""
		r.Reason = ""
		r.Pending = nil
		r.QueuedQuestions = nil
		r.Resume = nil
		r.RoundLimit = limit
		r.LandFixes = 0
		r.LandRecheck = true
		r.State = runrec.StateLand
	})
	if err != nil {
		return err
	}
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
