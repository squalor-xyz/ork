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
			fmt.Fprintf(os.Stderr, "merge detection %s: %v\n", rec.ID, err)
		}
	}
	d.runs.mu.Lock()
	// A newer HEAD check owns the displayed state if it overtook this pass.
	if d.runs.heads[key] == head {
		for id, merged := range results {
			d.runs.merged[id] = merged
		}
	}
	d.pruneRunsLocked()
	d.runs.mu.Unlock()
	d.changed(p.Name, source.Runs)
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
	if _, err := os.Stat(rec.Worktree); !errors.Is(err, fs.ErrNotExist) {
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
func (d *Daemon) markCleaned(id string) error {
	lock := d.runRecordLock(id)
	lock.Lock()
	defer lock.Unlock()
	at := time.Now().UTC()
	d.runs.mu.Lock()
	managed := d.runs.runs[id]
	d.runs.mu.Unlock()
	if managed != nil && managed.engine != nil {
		return managed.engine.MarkCleaned(at)
	}
	r, err := d.runs.deps.Store.Open(id)
	if err != nil {
		return err
	}
	return r.Update(func(rec *runrec.Record) { runrec.MarkCleaned(rec, at) })
}
