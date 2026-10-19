// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// Recover loads every unfinished run in the store and reconciles it with the
// live process groups (engine.restart). A run whose project lookup fails is
// named in the returned error; the others are still recovered.
func Recover(ctx context.Context, d Deps, lookup func(project string) (registry.Project, bool)) ([]*Engine, error) {
	runs, err := d.Store.List()
	errs := []error{err}
	var out []*Engine
	for _, run := range runs {
		rec := run.Snapshot()
		if runrec.IsFinal(rec.State) {
			continue
		}
		proj, ok := lookup(rec.Project)
		if !ok {
			errs = append(errs, fmt.Errorf("run %s: project %q is not in the registry", rec.ID, rec.Project))
			continue
		}
		e := Load(d, proj, run)
		if err := e.Reconcile(ctx); err != nil {
			errs = append(errs, fmt.Errorf("run %s: %w", rec.ID, err))
		}
		out = append(out, e)
	}
	return out, errors.Join(errs...)
}

// Reconcile compares the run's running roles with the live process groups.
// ork's own process groups are the ones this run's record names for roles
// that have no end; a role whose group is alive is killed, and every such
// role is marked lost. The slice then pauses for the owner, who retries the
// role or stops (engine.restart). A run that was between roles is paused too.
// Runs that rest, or are finished, are left alone.
func (e *Engine) Reconcile(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.run.Snapshot()
	if runrec.IsFinal(rec.State) {
		return nil
	}
	var lost []runrec.RoleRecord
	killed := map[int]bool{}
	var killErr error
	for _, r := range rec.Roles {
		if !r.Running() {
			continue
		}
		if r.PGID > 1 && r.PGID != selfPGID() && e.d.proc().Alive(r.PGID) {
			if err := e.d.proc().Kill(r.PGID); err != nil {
				killErr = errors.Join(killErr, fmt.Errorf("kill process group %d: %w", r.PGID, err))
			} else {
				killed[r.PGID] = true
			}
		}
		lost = append(lost, r)
	}
	if len(lost) > 0 {
		err := e.run.Update(func(rr *runrec.Record) {
			for i := range rr.Roles {
				if rr.Roles[i].Running() {
					rr.Roles[i].EndedAt = e.d.now()
					rr.Roles[i].Exit = "lost"
				}
			}
		})
		if err != nil {
			return err
		}
		for _, r := range lost {
			if r.Step != "" && rec.RelayRun != "" { // advisory, like every ledger call
				e.d.Slicer.RelayStep(ctx, e.wr, rec.Worktree, rec.RelayRun, slicercli.StepSpec{Step: r.Step, Phase: "fail"})
			}
		}
		last := lost[len(lost)-1]
		how := "its process group was gone"
		if killed[last.PGID] {
			how = fmt.Sprintf("its process group %d was still running and was killed", last.PGID)
		}
		if err := e.pause(runrec.Pending{
			Kind: runrec.PendingLost, State: rec.State, Role: last.Role, Round: last.Round,
			Reason: fmt.Sprintf("%s round %d lost: %s", last.Role, last.Round, how),
		}); err != nil {
			return err
		}
		return killErr
	}
	if !resting(rec.State) {
		if err := e.pause(runrec.Pending{
			Kind: runrec.PendingInterrupted, State: rec.State,
			Reason: fmt.Sprintf("ork stopped while the slice was in %s", rec.State),
		}); err != nil {
			return err
		}
	}
	return killErr
}
