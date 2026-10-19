// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

const (
	MethodRunPlan    = "run.plan"
	MethodRunStart   = "run.start"
	MethodRunApprove = "run.approve"
	MethodRunStop    = "run.stop"
	MethodRunAnswer  = "run.answer"
)

// RunParams are the owner's explicit lineup inputs (adj #86).
type RunParams struct {
	Project       string   `json:"project"`
	Item          string   `json:"item"`
	WellSpecified bool     `json:"well_specified"`
	Split         bool     `json:"split"`
	Expected      *RunPlan `json:"expected,omitempty"`
}

// RunAction names a run, and an owner answer when answering.
type RunAction struct {
	Run  string `json:"run"`
	Text string `json:"text,omitempty"`
}

// RunPlan is checkpoint 1. These wire fields are proposed.
type RunPlan struct {
	project      registry.Project                // registry settings are private to the daemon
	Project      registry.Project                `json:"project"`
	Item         string                          `json:"item"`
	Worktree     string                          `json:"worktree"`
	Branch       string                          `json:"branch"`
	CheckCommand string                          `json:"check_command"`
	Lineup       map[string][]runrec.LineupEntry `json:"lineup"`
	Permissions  map[string]engine.Permission    `json:"permissions"`
}

// EngineRun is the owner's status of one engine run. Wire fields are proposed.
type EngineRun struct {
	ID    string `json:"id"`
	Item  string `json:"item"`
	State string `json:"state"`
	Wait  string `json:"wait,omitempty"`
}

type runResult struct {
	err    error
	record runrec.Record
}

type managedRun struct {
	engine   *engine.Engine
	cancel   context.CancelFunc
	done     chan struct{}
	result   *runResult
	busy     bool
	stopping bool
}

// runManager serializes admission and owner actions; the engine serializes
// each run's state transitions. Background jobs use the daemon's lifetime.
type runManager struct {
	closed bool
	mu     sync.Mutex
	ctx    context.Context
	deps   engine.Deps
	runs   map[string]*managedRun
	wg     sync.WaitGroup
}

func (d *Daemon) initRunManager() {
	deps := engine.Deps{}
	if d.cfg.Engine != nil {
		deps = *d.cfg.Engine
	}
	if deps.Store == nil {
		deps.Store = &runrec.Store{Dir: filepath.Join(d.cfg.Paths.Dir, "runs")}
	}
	d.runs = runManager{ctx: context.Background(), deps: deps, runs: map[string]*managedRun{}}
}
func (d *Daemon) runDeps(p registry.Project) engine.Deps {
	deps := d.runs.deps
	if deps.Runner == nil {
		var env []string
		if len(p.VendorEnv["claude"]) > 0 {
			env = os.Environ()
			for k, v := range p.VendorEnv["claude"] {
				env = append(env, k+"="+v)
			}
		}
		deps.Runner = claudeRunner{fixtureDir: filepath.Join(d.cfg.Paths.Dir, "fixtures", "claude"), env: env}
	}
	return deps
}
func (d *Daemon) recoverRuns(ctx context.Context) error {
	d.runs.ctx = ctx
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "recover runs: %v\n", err)
	}
	if len(stored) == 0 {
		return nil
	}
	f, err := d.loadRegistry()
	if err != nil {
		return err
	}
	for _, r := range stored {
		rec := r.Snapshot()
		if runrec.IsFinal(rec.State) {
			continue
		}
		p, ok := find(f, rec.Project)
		if !ok {
			fmt.Fprintf(os.Stderr, "run %s: project %q is not registered\n", rec.ID, rec.Project)
			continue
		}
		e := engine.Load(d.runDeps(p), p, r)
		if err := e.Reconcile(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "reconcile %s: %v\n", rec.ID, err)
			continue
		}
		d.runs.runs[rec.ID] = &managedRun{engine: e}
	}
	return nil
}
func (d *Daemon) closeRuns() {
	d.runs.mu.Lock()
	d.runs.closed = true
	for _, r := range d.runs.runs {
		if r.cancel != nil {
			r.cancel()
		}
	}
	d.runs.mu.Unlock()
	d.runs.wg.Wait()
}
func (d *Daemon) activeRuns() bool {
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	for _, r := range d.runs.runs {
		if r.busy {
			return true
		}
	}
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		return true
	} // preserve the daemon when the run state cannot be read
	for _, r := range stored {
		switch r.Snapshot().State {
		case runrec.StateCommitted, runrec.StateStopped, runrec.StatePaused, runrec.StateAwaitingApproval:
		default:
			return true
		}
	}
	return false
}
func (d *Daemon) planRun(ctx context.Context, p RunParams) (*RunPlan, error) {
	if p.Project == "" || p.Item == "" {
		return nil, badParams(errors.New("run needs project and item"))
	}
	f, err := d.loadRegistry()
	if err != nil {
		return nil, err
	}
	proj, ok := find(f, p.Project)
	if !ok {
		return nil, notFound(p.Project)
	}
	rec, err := d.runs.deps.Slicer.RecommendRoles(ctx, proj.Path, p.Item, p.WellSpecified, p.Split)
	if err != nil {
		return nil, err
	}
	plan := &RunPlan{project: proj, Project: registry.Project{Name: proj.Name, Path: proj.Path}, Item: p.Item, CheckCommand: proj.CheckCommand, Lineup: map[string][]runrec.LineupEntry{}, Permissions: map[string]engine.Permission{}}
	for _, role := range []string{engine.RoleImplementer, engine.RoleReviewer, engine.RoleGate} {
		sourceRole := role
		if role == engine.RoleGate {
			sourceRole = engine.RoleReviewer
		}
		for _, choice := range rec.Roles {
			if choice.Role != sourceRole {
				continue
			}
			rows := choice.Alternatives
			if choice.Row != nil {
				rows = append([]slicercli.Row{*choice.Row}, rows...)
			}
			for _, row := range rows {
				if strings.EqualFold(row.Vendor, "claude") {
					plan.Lineup[role] = []runrec.LineupEntry{{Vendor: "claude", Model: row.Model, Effort: row.Reasoning}}
					break
				}
			}
		}
		if len(plan.Lineup[role]) == 0 {
			return nil, fmt.Errorf("lineup has no Claude row for %s", sourceRole)
		}
		perm, err := engine.ResolvePermission(proj, role)
		if err != nil {
			return nil, err
		}
		plan.Permissions[role] = perm
	}
	pattern := proj.WorktreePattern
	if pattern == "" {
		pattern = ".worktrees/{id}"
	}
	plan.Worktree = strings.ReplaceAll(pattern, "{id}", p.Item)
	if !filepath.IsAbs(plan.Worktree) {
		plan.Worktree = filepath.Join(proj.Path, plan.Worktree)
	}
	plan.Worktree = filepath.Clean(plan.Worktree)
	pattern = proj.BranchPattern
	if pattern == "" {
		pattern = "{id}"
	}
	plan.Branch = strings.ReplaceAll(pattern, "{id}", p.Item)
	return plan, nil
}
func (d *Daemon) startRun(ctx context.Context, p RunParams) (runrec.Record, error) {
	plan, err := d.planRun(ctx, p)
	if err != nil {
		return runrec.Record{}, err
	}
	if p.Expected != nil {
		shown, _ := json.Marshal(p.Expected)
		current, _ := json.Marshal(plan)
		if string(shown) != string(current) {
			return runrec.Record{}, errors.New("plan changed; run the command again to confirm it")
		}
	}
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	if d.runs.closed {
		return runrec.Record{}, errors.New("run manager is closed")
	}
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		return runrec.Record{}, err
	}
	for _, r := range stored {
		rec := r.Snapshot()
		if !runrec.IsFinal(rec.State) && rec.Worktree == plan.Worktree {
			return runrec.Record{}, fmt.Errorf("run %s already owns %s", rec.ID, rec.Worktree)
		}
	}
	e, err := engine.New(ctx, d.runDeps(plan.project), engine.Config{Project: plan.project, Item: p.Item, Lineup: plan.Lineup})
	if err != nil {
		return runrec.Record{}, err
	}
	r := &managedRun{engine: e}
	rec := e.Snapshot()
	d.runs.runs[rec.ID] = r
	d.launchRun(r, e.Run)
	return rec, nil
}

// launchRun starts a job with the manager lock held.
func (d *Daemon) launchRun(r *managedRun, fn func(context.Context) error) {
	ctx, cancel := context.WithCancel(d.runs.ctx)
	r.cancel = cancel
	r.busy = true
	r.done = make(chan struct{})
	r.result = &runResult{}
	result := r.result
	d.runs.wg.Add(1)
	go func() {
		defer d.runs.wg.Done()
		defer cancel()
		err := fn(ctx)
		d.runs.mu.Lock()
		result.err = err
		result.record = r.engine.Snapshot()
		r.busy = false
		close(r.done)
		d.runs.mu.Unlock()
	}()
}
func (d *Daemon) ownerRun(ctx context.Context, method string, p RunAction) (runrec.Record, error) {
	d.runs.mu.Lock()
	if d.runs.closed {
		d.runs.mu.Unlock()
		return runrec.Record{}, errors.New("run manager is closed")
	}
	r := d.runs.runs[p.Run]
	if r == nil {
		d.runs.mu.Unlock()
		return runrec.Record{}, &ipc.Error{Code: CodeNotFound, Message: "no run " + p.Run}
	}
	if r.stopping {
		d.runs.mu.Unlock()
		return runrec.Record{}, fmt.Errorf("run %s is stopping", p.Run)
	}
	if r.busy {
		if method != MethodRunStop {
			d.runs.mu.Unlock()
			return runrec.Record{}, fmt.Errorf("run %s is busy", p.Run)
		}
		r.stopping = true
		r.cancel()
		done := r.done
		d.runs.mu.Unlock()
		<-done
		d.runs.mu.Lock()
		if d.runs.closed {
			d.runs.mu.Unlock()
			return runrec.Record{}, errors.New("run manager is closed")
		}
	}
	r.stopping = false
	var fn func(context.Context) error
	switch method {
	case MethodRunAnswer:
		rec := r.engine.Snapshot()
		if strings.TrimSpace(p.Text) == "" || rec.State != runrec.StatePaused || rec.Pending == nil || rec.Pending.Kind != runrec.PendingQuestion {
			d.runs.mu.Unlock()
			return runrec.Record{}, errors.New("run needs a pending question and a non-empty answer")
		}
		fn = func(ctx context.Context) error { return r.engine.Answer(ctx, p.Text) }
	case MethodRunApprove:
		fn = r.engine.Approve
	case MethodRunStop:
		fn = func(ctx context.Context) error {
			// A cancelled round leaves its role live in the record. Reconcile checks
			// and kills any surviving group before recording the owner's stop.
			if err := r.engine.Reconcile(ctx); err != nil {
				return err
			}
			return r.engine.Stop(ctx, "CLI stop")
		}
	default:
		d.runs.mu.Unlock()
		return runrec.Record{}, errors.New("unknown owner action")
	}
	d.launchRun(r, fn)
	result := r.result
	done := r.done
	d.runs.mu.Unlock()
	// Owner commands return once the engine has durably applied the action.
	select {
	case <-ctx.Done():
		return runrec.Record{}, ctx.Err()
	case <-done:
	}
	return result.record, result.err
}
func (d *Daemon) handleRun(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if method == MethodRunPlan || method == MethodRunStart {
		var p RunParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		if method == MethodRunPlan {
			return d.planRun(ctx, p)
		}
		return d.startRun(ctx, p)
	}
	var p RunAction
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	return d.ownerRun(ctx, method, p)
}
func (d *Daemon) addEngineStatus(ps *ProjectStatus) error {
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "run status: %v\n", err)
	}
	ps.EngineRuns = []EngineRun{}
	for _, r := range stored {
		rec := r.Snapshot()
		if m := d.runs.runs[rec.ID]; m != nil {
			rec = m.engine.Snapshot()
		}
		if rec.Project != ps.Name {
			continue
		}
		wait := rec.Reason
		if rec.Pending != nil {
			wait = rec.Pending.Reason
			if rec.Pending.Question != "" {
				wait = rec.Pending.Question
			}
		}
		switch rec.State {
		case runrec.StateAwaitingApproval:
			ps.NeedsYou++
			wait = "owner approval"
		case runrec.StatePaused, runrec.StateStopped:
			ps.NeedsYou++
		}
		ps.EngineRuns = append(ps.EngineRuns, EngineRun{ID: rec.ID, Item: rec.Item, State: rec.State, Wait: wait})
	}
	return nil
}
