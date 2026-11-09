// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

const MethodRunCleanup = "run.cleanup"

// landingFetcher shares the cache's HEAD read and lifetime, including scheduled
// refreshes. No merge reads are added when HEAD is unchanged.
type landingFetcher struct {
	source.Fetcher
	d *Daemon
}

func (f landingFetcher) Fetch(ctx context.Context, p registry.Project, part source.Part) (any, error) {
	v, err := f.Fetcher.Fetch(ctx, p, part)
	if part == source.Head && err == nil {
		if head, ok := v.(string); ok && head != "" {
			f.d.checkMerged(ctx, p, head)
		}
	}
	return v, err
}

func (d *Daemon) git(ctx context.Context, p registry.Project, args ...string) (clirun.Result, error) {
	d.runs.mu.Lock()
	bin := d.runs.deps.GitBin
	d.runs.mu.Unlock()
	if bin == "" {
		bin = "git"
	}
	return clirun.Run(ctx, clirun.Call{Bin: bin, Args: append([]string{"-C", p.Path}, args...)})
}
func (d *Daemon) landed(ctx context.Context, p registry.Project, rec runrec.Record) (*bool, error) {
	if rec.State != runrec.StateCommitted || rec.Branch == "" || strings.HasPrefix(rec.Branch, "-") || strings.HasPrefix(rec.Worktree, "-") {
		return nil, nil
	}
	result, err := d.git(ctx, p, "merge-base", "--is-ancestor", rec.Branch, "HEAD")
	if err == nil {
		merged := true
		return &merged, nil
	}
	var exit *clirun.ExitError
	if errors.As(err, &exit) && result.ExitCode == 1 {
		merged := false
		return &merged, nil
	}
	return nil, err
}

// branchGone reports whether the run's worktree path and its branch ref no
// longer exist. A git error, a branch that exists or a worktree that is still
// there is false: the run keeps its state (S153, S263).
func (d *Daemon) branchGone(ctx context.Context, p registry.Project, rec runrec.Record) bool {
	if !worktreeGone(rec) || rec.Branch == "" || strings.HasPrefix(rec.Branch, "-") {
		return false
	}
	result, err := d.git(ctx, p, "rev-parse", "--verify", "--quiet", "refs/heads/"+rec.Branch)
	var exit *clirun.ExitError
	return errors.As(err, &exit) && result.ExitCode == 1
}

// commitInHead says whether the commit ork made on the run's branch is in
// HEAD. A run with no usable commit has no merged work to find: false and
// known. Only a git error leaves it unknown (S263).
func (d *Daemon) commitInHead(ctx context.Context, p registry.Project, rec runrec.Record) (inHead, known bool) {
	if rec.Commit == nil || rec.Commit.SHA == "" || strings.HasPrefix(rec.Commit.SHA, "-") {
		return false, true
	}
	result, err := d.git(ctx, p, "merge-base", "--is-ancestor", rec.Commit.SHA, "HEAD")
	if err == nil {
		return true, true
	}
	var exit *clirun.ExitError
	return false, errors.As(err, &exit) && result.ExitCode == 1
}

// markGone marks a run cleaned when its branch and worktree are gone and git
// can say whether its commit is in HEAD: a run from before S95 whose commit is
// in HEAD was merged and cleaned up (S153); one whose work is not in HEAD has
// nothing left to merge or clean, and keeps a reason (S263, adj 204 and 205).
// Anything else, including a git error, leaves the run as it was.
func (d *Daemon) markGone(ctx context.Context, p registry.Project, rec runrec.Record) {
	if !d.branchGone(ctx, p, rec) {
		return
	}
	inHead, known := d.commitInHead(ctx, p, rec)
	if !known {
		return
	}
	reason := ""
	if !inHead {
		reason = fmt.Sprintf("branch %s is gone and not merged into HEAD", rec.Branch)
	}
	if err := d.markCleanedWith(rec.ID, reason); err != nil {
		fmt.Fprintf(os.Stderr, "merge detection %s: mark cleaned: %v\n", rec.ID, err)
	}
}

// logMergeError prints a merge detection error unless it is the one last
// printed for the run. The memory is in this process only (adj 206).
func (d *Daemon) logMergeError(id string, err error) {
	text := err.Error()
	d.runs.mu.Lock()
	last, seen := d.runs.mergeLogged[id]
	d.runs.mergeLogged[id] = text
	d.runs.mu.Unlock()
	if !seen || last != text {
		fmt.Fprintf(os.Stderr, "merge detection %s: %s\n", id, text)
	}
}

// cleanedLocked reads the run's record and reports whether it is cleaned.
// The caller holds the manager lock.
func (d *Daemon) cleanedLocked(id string) bool {
	if managed := d.runs.runs[id]; managed != nil && managed.engine != nil {
		return managed.engine.Snapshot().Cleaned()
	}
	r, err := d.runs.deps.Store.Open(id)
	return err == nil && r.Snapshot().Cleaned()
}

// worktreeGone reports whether the run has a worktree path that no longer
// exists, so the cleanup has no git worktree remove to run (S153).
func worktreeGone(rec runrec.Record) bool {
	if rec.Worktree == "" {
		return false
	}
	_, err := os.Stat(rec.Worktree)
	return errors.Is(err, fs.ErrNotExist)
}

func (d *Daemon) checkMerged(ctx context.Context, p registry.Project, head string) {
	d.runs.mu.Lock()
	key := p.Name + "\x00" + p.Path
	if d.runs.heads[key] == head {
		d.runs.mu.Unlock()
		return
	}
	d.runs.heads[key] = head
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "merge detection: %v\n", err)
	}
	var records []runrec.Record
	for _, r := range stored {
		rec := r.Snapshot()
		if managed := d.runs.runs[rec.ID]; managed != nil && managed.engine != nil {
			rec = managed.engine.Snapshot()
		}
		if rec.Project != p.Name || rec.State != runrec.StateCommitted {
			continue
		}
		if rec.Cleaned() {
			// Its branch is gone: there is nothing to read, and no merge state to show.
			delete(d.runs.merged, rec.ID)
			continue
		}
		// The previous value stays until the write-back, so a snapshot and the
		// cleanup key do not see "unknown" while git runs.
		records = append(records, rec)
	}
	d.runs.mu.Unlock()
	results := make(map[string]*bool, len(records))
	for _, rec := range records {
		merged, err := d.landed(ctx, p, rec)
		results[rec.ID] = merged
		if err != nil {
			d.logMergeError(rec.ID, err)
			// A run from before S95 can have no worktree or branch left (S153, S263).
			d.markGone(ctx, p, rec)
			continue
		}
		d.runs.mu.Lock()
		delete(d.runs.mergeLogged, rec.ID)
		d.runs.mu.Unlock()
	}
	// The runs whose merge this pass found, with their items (S229, *proposed*).
	var found []runrec.Record
	d.runs.mu.Lock()
	// A newer HEAD check owns the displayed state if it overtook this pass.
	if d.runs.heads[key] == head {
		byID := make(map[string]runrec.Record, len(records))
		for _, rec := range records {
			byID[rec.ID] = rec
		}
		for id, merged := range results {
			// A cleanup that finished while git ran leaves no branch to have a
			// merge state: do not write one back (S153).
			if merged != nil && d.cleanedLocked(id) {
				delete(d.runs.merged, id)
				continue
			}
			if previous := d.runs.merged[id]; merged != nil && *merged && (previous == nil || !*previous) {
				found = append(found, byID[id])
			}
			d.runs.merged[id] = merged
		}
	}
	d.pruneRunsLocked()
	d.runs.mu.Unlock()
	d.removeMergedSpecDirs(found)
	d.changed(p.Name, source.Runs)
}

// removeMergedSpecDirs drops the spec-check directory of the item of each run
// whose merge was just found. A run stopped after commit never lands through
// ork, so the owner's merge is the only sign that its item is done (S191,
// *proposed*). The lock order is specStateMu before mu, as at recoverRuns; no
// git runs under either.
func (d *Daemon) removeMergedSpecDirs(found []runrec.Record) {
	if len(found) == 0 {
		return
	}
	d.runs.specStateMu.Lock()
	defer d.runs.specStateMu.Unlock()
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	for _, rec := range found {
		d.removeSpecDirLocked(rec.Project, rec.Item)
	}
}

// pruneRunsLocked uses only previously checked results; pruning never adds git
// reads and unknown branches are retained. A cleaned run (S95) has no branch
// left and is pruned like a merged one. The caller holds the manager lock.
func (d *Daemon) pruneRunsLocked() {
	removed, err := d.runs.deps.Store.Prune(100, func(rec runrec.Record) bool {
		merged := d.runs.merged[rec.ID]
		return rec.State == runrec.StateCommitted && (rec.Cleaned() || merged != nil && *merged) && !d.runs.cleaning[rec.ID]
	})
	for _, id := range removed {
		delete(d.runs.merged, id)
		delete(d.runs.mergeLogged, id)
		delete(d.runs.runs, id)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "prune runs: %v\n", err)
	}
}
func (d *Daemon) cleanupRun(ctx context.Context, a RunAction) error {
	d.runs.mu.Lock()
	r, err := d.runs.deps.Store.Open(a.Run)
	if err != nil {
		d.runs.mu.Unlock()
		return badParams(err)
	}
	rec := r.Snapshot()
	if managed := d.runs.runs[rec.ID]; managed != nil && managed.engine != nil {
		rec = managed.engine.Snapshot()
	}
	merged := d.runs.merged[rec.ID]
	if rec.Cleaned() {
		d.runs.mu.Unlock()
		return badParams(errors.New("this run is already cleaned up: its worktree and branch are gone"))
	}
	if rec.State != runrec.StateCommitted || merged == nil || !*merged || rec.Branch == "" || rec.Worktree == "" || strings.HasPrefix(rec.Branch, "-") || strings.HasPrefix(rec.Worktree, "-") {
		d.runs.mu.Unlock()
		return badParams(errors.New("cleanup needs a committed, merged run with a branch and worktree"))
	}
	if d.runs.cleaning[rec.ID] {
		d.runs.mu.Unlock()
		return badParams(errors.New("cleanup is already in progress"))
	}
	d.runs.cleaning[rec.ID] = true
	d.runs.mu.Unlock()
	defer func() { d.runs.mu.Lock(); delete(d.runs.cleaning, rec.ID); d.runs.mu.Unlock() }()
	p, err := d.registered(rec.Project)
	if err != nil {
		return err
	}
	// A cleanup that stopped at branch -d already removed the worktree, and
	// git worktree remove would fail on the path that is gone: skip it.
	steps := [][]string{{"branch", "-d", rec.Branch}}
	if !worktreeGone(rec) {
		steps = append([][]string{{"worktree", "remove", rec.Worktree}}, steps...)
	}
	for _, args := range steps {
		result, err := d.git(ctx, p, args...)
		if err != nil {
			if len(result.Stderr) > 0 {
				return errors.New(string(result.Stderr))
			}
			return err
		}
	}
	return d.markCleaned(rec.ID)
}

// markCleaned records a finished cleanup, and drops the "cleanup failed" reason
// of a landed run, so the row leaves Ready to land (S119, S95).
func (d *Daemon) markCleaned(id string) error { return d.markCleanedWith(id, "") }

// markCleanedWith is markCleaned that also records why the run counts as
// cleaned when ork did not clean it up (S263). An empty reason records none.
func (d *Daemon) markCleanedWith(id, reason string) error {
	lock := d.runRecordLock(id)
	lock.Lock()
	defer lock.Unlock()
	at := time.Now().UTC()
	d.runs.mu.Lock()
	managed := d.runs.runs[id]
	d.runs.mu.Unlock()
	if managed != nil && managed.engine != nil {
		return managed.engine.MarkCleanedWith(at, reason)
	}
	r, err := d.runs.deps.Store.Open(id)
	if err != nil {
		return err
	}
	return r.Update(func(rec *runrec.Record) { runrec.MarkCleanedWith(rec, at, reason) })
}
