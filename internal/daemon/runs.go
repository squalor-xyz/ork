// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

const (
	MethodRunPlan    = "run.plan"
	MethodRunStart   = "run.start"
	MethodRunApprove = "run.approve"
	MethodRunStop    = "run.stop"
	MethodRunAnswer  = "run.answer"
	MethodRunRetry   = "run.retry"
)

// defaultGlobalCap is the global cap when the registry sets none (adj 20).
const defaultGlobalCap = 4

// RunParams are the owner's launch inputs (run.lineup-inputs, run.launch-wire).
type RunParams struct {
	wave          string                          // daemon-assigned; never accepted from a run.start client
	Project       string                          `json:"project"`
	Item          string                          `json:"item"`
	WellSpecified bool                            `json:"well_specified"`
	Split         bool                            `json:"split"`
	Expected      *RunPlan                        `json:"expected,omitempty"`
	Lineup        map[string][]runrec.LineupEntry `json:"lineup,omitempty"`
	Budgets       *registry.Budgets               `json:"budgets,omitempty"`
}

// RunAction names a run, and an owner answer when answering.
type RunAction struct {
	Run  string `json:"run"`
	Text string `json:"text,omitempty"`
}

// RunPlan is checkpoint 1 (run.launch-wire).
type RunPlan struct {
	project      registry.Project                // registry settings are private to the daemon
	Project      registry.Project                `json:"project"`
	Item         string                          `json:"item"`
	Worktree     string                          `json:"worktree"`
	Branch       string                          `json:"branch"`
	CheckCommand string                          `json:"check_command"`
	Lineup       map[string][]runrec.LineupEntry `json:"lineup"`
	Budgets      registry.Budgets                `json:"budgets"`
	Permissions  map[string]engine.Permission    `json:"permissions"`
}

// EngineRun is the owner's status of one engine run. Wire fields are proposed.
type EngineRun = source.EngineRun

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
	stop     *stopCall // the owner stop in flight, which later stops join
}

// stopCall is an owner stop that has not returned. A stop that arrives while
// it is in flight joins it: the joiner waits for the stop's job and returns
// that job's record and error.
type stopCall struct {
	started chan struct{} // closed once done and result are set
	done    chan struct{} // closed when the stop's job ends
	result  *runResult    // the stop's job outcome, read once done is closed
}

// begin records the stop's job. The caller holds the manager lock.
func (c *stopCall) begin(done chan struct{}, result *runResult) {
	c.done, c.result = done, result
	close(c.started)
}

// abandon ends a stop that gives up before its job starts.
func (c *stopCall) abandon(err error) {
	done := make(chan struct{})
	close(done)
	c.begin(done, &runResult{err: err})
}

// runManager serializes admission and owner actions; the engine serializes
// each run's state transitions. Background jobs use the daemon's lifetime.
type runManager struct {
	cleaning     map[string]bool // cleanup calls in flight; guarded by mu
	heads        map[string]string
	merged       map[string]*bool
	lastRegistry *registry.File // last successful run-manager read; guarded by mu
	waveStops    map[string]int // suppress queued members while a wave stop is in progress
	closed       bool
	mu           sync.Mutex
	ctx          context.Context
	deps         engine.Deps
	runs         map[string]*managedRun
	wg           sync.WaitGroup
}

func (d *Daemon) initRunManager() {
	deps := engine.Deps{}
	if d.cfg.Engine != nil {
		deps = *d.cfg.Engine
	}
	if deps.Store == nil {
		deps.Store = &runrec.Store{Dir: filepath.Join(d.cfg.Paths.Dir, "runs")}
	}
	d.runs = runManager{cleaning: map[string]bool{}, heads: map[string]string{}, merged: map[string]*bool{}, ctx: context.Background(), deps: deps, runs: map[string]*managedRun{}, waveStops: map[string]int{}}
}
func (d *Daemon) runDeps(p registry.Project) engine.Deps {
	deps := d.runs.deps
	if deps.Runner == nil {
		envFor := func(vendor string) []string {
			if len(p.VendorEnv[vendor]) == 0 {
				return nil
			}
			env := os.Environ()
			for k, v := range p.VendorEnv[vendor] {
				env = append(env, k+"="+v)
			}
			return env
		}
		deps.Runner = vendorRunner{
			claude: claudeRunner{fixtureDir: filepath.Join(d.cfg.Paths.Dir, "fixtures", "claude"), env: envFor("claude")},
			codex:  codexRunner{fixtureDir: filepath.Join(d.cfg.Paths.Dir, "fixtures", "codex"), env: envFor("codex")},
		}
	}
	return deps
}
func (d *Daemon) recoverRuns(ctx context.Context) error {
	defer func() { d.runs.mu.Lock(); defer d.runs.mu.Unlock(); d.pruneRunsLocked() }()
	d.runs.ctx = ctx
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "recover runs: %v\n", err)
	}
	if len(stored) == 0 {
		return nil
	}
	d.runs.mu.Lock()
	f, err := d.loadRunRegistryLocked()
	d.runs.mu.Unlock()
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
		if rec.State == runrec.StateQueued {
			// A queued run has no process and wrote nothing; it keeps its place.
			d.runs.runs[rec.ID] = &managedRun{engine: e}
			continue
		}
		if err := e.Reconcile(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "reconcile %s: %v\n", rec.ID, err)
			continue
		}
		d.runs.runs[rec.ID] = &managedRun{engine: e}
	}
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	d.startQueuedLocked(f)
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
	return d.activeRunsLocked()
}

func (d *Daemon) activeRunsLocked() bool {
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

// stopDaemon closes admission under the same lock used to start work. It
// waits for cancelled jobs before replying; the server then ends its context.
func (d *Daemon) stopDaemon(p StopParams) (*StopResult, error) {
	d.runs.mu.Lock()
	if !p.Force && (d.cfg.WaveRunning() || d.activeRunsLocked()) {
		d.runs.mu.Unlock()
		return nil, &ipc.Error{Code: "daemon_busy", Message: "a role or engine job is running; use --force to stop the daemon"}
	}
	d.runs.closed = true
	d.runs.mu.Unlock()
	d.closeRuns()
	return &StopResult{Stopped: true}, nil
}

// matrixAdapters maps matrix agents to the names used by the dispatcher.
var matrixAdapters = map[string]string{"claude code": "claude", "codex": "codex"}

func (d *Daemon) planRun(ctx context.Context, p RunParams) (*RunPlan, error) {
	if p.Project == "" || p.Item == "" {
		return nil, badParams(errors.New("run needs project and item"))
	}
	d.runs.mu.Lock()
	f, err := d.loadRunRegistryLocked()
	d.runs.mu.Unlock()
	if err != nil {
		return nil, err
	}
	proj, ok := find(f, p.Project)
	if !ok {
		return nil, notFound(p.Project, f)
	}
	rec, err := d.runs.deps.Slicer.RecommendRoles(ctx, proj.Path, p.Item, p.WellSpecified, p.Split)
	if err != nil {
		return nil, err
	}
	plan := &RunPlan{project: proj, Project: registry.Project{Name: proj.Name, Path: proj.Path}, Item: p.Item, CheckCommand: proj.CheckCommand, Lineup: map[string][]runrec.LineupEntry{}, Permissions: map[string]engine.Permission{}}
	for _, role := range []string{engine.RoleImplementer, engine.RoleReviewer, engine.RoleGate, engine.RoleArchitect} {
		sourceRole := role
		if role == engine.RoleGate {
			sourceRole = engine.RoleReviewer
		}
		recommended := false
		for _, choice := range rec.Roles {
			if choice.Role != sourceRole {
				continue
			}
			if role == engine.RoleArchitect && choice.Row == nil && len(choice.Alternatives) == 0 {
				continue
			}
			recommended = true
			rows := choice.Alternatives
			if choice.Row != nil {
				rows = append([]slicercli.Row{*choice.Row}, rows...)
			}
			for _, row := range rows {
				if vendor := matrixAdapters[strings.ToLower(row.Agent)]; vendor != "" {
					plan.Lineup[role] = append(plan.Lineup[role], runrec.LineupEntry{Vendor: vendor, Model: row.Model, Effort: row.Reasoning})
				}
			}
		}
		if role == engine.RoleArchitect && !recommended {
			continue
		}
		if len(plan.Lineup[role]) == 0 {
			return nil, fmt.Errorf("lineup has no row with an adapter for %s", sourceRole)
		}
		perm, err := engine.ResolvePermission(proj, role)
		if err != nil {
			return nil, err
		}
		plan.Permissions[role] = perm
	}
	for role, entries := range p.Lineup {
		if role != engine.RoleImplementer && role != engine.RoleReviewer {
			return nil, badParams(fmt.Errorf("cannot override lineup role %q", role))
		}
		if len(entries) == 0 {
			return nil, badParams(fmt.Errorf("lineup for %s needs at least one entry", role))
		}
		for _, entry := range entries {
			if !supportsVendor(entry.Vendor) {
				return nil, badParams(fmt.Errorf("unsupported vendor %q for %s", entry.Vendor, role))
			}
			if strings.TrimSpace(entry.Model) == "" || strings.TrimSpace(entry.Effort) == "" {
				return nil, badParams(fmt.Errorf("lineup for %s needs a model and effort", role))
			}
		}
		plan.Lineup[role] = append([]runrec.LineupEntry(nil), entries...)
	}
	plan.Lineup[engine.RoleGate] = append([]runrec.LineupEntry(nil), plan.Lineup[engine.RoleReviewer]...)
	plan.Budgets = proj.Budgets
	if p.Budgets != nil {
		if p.Budgets.Minutes < 0 || p.Budgets.Tokens < 0 {
			return nil, badParams(errors.New("budgets must not be negative"))
		}
		if p.Budgets.Minutes != 0 {
			plan.Budgets.Minutes = p.Budgets.Minutes
		}
		if p.Budgets.Tokens != 0 {
			plan.Budgets.Tokens = p.Budgets.Tokens
		}
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
	// Admission is here and only here: owner actions never reach startRun, so
	// approve, answer, retry and stop bypass the caps (adj 122).
	f, err := d.loadRunRegistryLocked()
	if err != nil {
		return runrec.Record{}, err
	}
	queued := !d.hasRoomLocked(f, plan.project.Name)
	wave := p.wave
	if wave == "" {
		wave = newWaveID(time.Now())
	}
	cfg := engine.Config{Project: plan.project, Item: p.Item, Lineup: plan.Lineup, Budgets: plan.Budgets, Wave: wave, Queued: queued}
	e, err := engine.New(ctx, d.runDeps(plan.project), cfg)
	if err != nil {
		return runrec.Record{}, err
	}
	r := &managedRun{engine: e}
	rec := e.Snapshot()
	d.runs.runs[rec.ID] = r
	if !queued {
		d.launchRun(r, e.Run)
	}
	return rec, nil
}

// newWaveID returns a wave id in the form docs/state.md shows. A run started
// alone is a wave of one. The generator is proposed.
func newWaveID(now time.Time) string {
	b := make([]byte, 2)
	rand.Read(b)
	return "wave-" + now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// WaveStartParams carries each chosen item's run.plan inputs, including any
// launch overrides and optional confirmed plan (run.launch-wire).
type WaveStartParams struct {
	Items []RunParams `json:"items"`
}

// WaveAction identifies a wave using the wave- prefix (wave.stop-target).
type WaveAction struct {
	Wave string `json:"wave"`
}

// WaveItemResult reports an admission or stop error without hiding peers.
type WaveItemResult struct {
	Item  string         `json:"item"`
	Run   *runrec.Record `json:"run,omitempty"`
	Error *ipc.Error     `json:"error,omitempty"`
}

// WaveResult is the proposed result shape of wave.start and wave.stop.
type WaveResult struct {
	Wave  string           `json:"wave"`
	Items []WaveItemResult `json:"items"`
}

func waveItem(item string, rec runrec.Record, err error) WaveItemResult {
	if err != nil {
		return WaveItemResult{Item: item, Error: ipc.ErrorOf(err)}
	}
	return WaveItemResult{Item: item, Run: &rec}
}

func (d *Daemon) startWave(ctx context.Context, p WaveStartParams) (*WaveResult, error) {
	if len(p.Items) == 0 {
		return nil, badParams(errors.New("wave needs items from one project"))
	}
	project := p.Items[0].Project
	for _, item := range p.Items {
		if project == "" || item.Project != project {
			return nil, badParams(errors.New("wave needs items from one project"))
		}
		if item.Item == "" {
			return nil, badParams(errors.New("wave item is empty"))
		}
	}
	result := &WaveResult{Wave: newWaveID(time.Now()), Items: []WaveItemResult{}}
	for _, item := range p.Items {
		item.wave = result.Wave
		rec, err := d.startRun(ctx, item)
		result.Items = append(result.Items, waveItem(item.Item, rec, err))
	}
	return result, nil
}

func (d *Daemon) stopWave(ctx context.Context, p WaveAction) (*WaveResult, error) {
	if !strings.HasPrefix(p.Wave, "wave-") {
		return nil, badParams(errors.New("wave id needs wave- prefix"))
	}
	d.runs.mu.Lock()
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		d.runs.mu.Unlock()
		return nil, err
	}
	var members []runrec.Record
	found := false
	for _, run := range stored {
		rec := run.Snapshot()
		if r := d.runs.runs[rec.ID]; r != nil {
			rec = r.engine.Snapshot()
		}
		if rec.Wave != p.Wave {
			continue
		}
		found = true
		if !runrec.IsFinal(rec.State) {
			members = append(members, rec)
		}
	}
	if !found {
		d.runs.mu.Unlock()
		return nil, &ipc.Error{Code: CodeNotFound, Message: "no wave " + p.Wave}
	}
	// Stopping an active member frees a slot. Keep its queued peers from
	// starting before their owner stop has been durably applied.
	d.runs.waveStops[p.Wave]++
	d.runs.mu.Unlock()
	defer func() {
		d.runs.mu.Lock()
		d.runs.waveStops[p.Wave]--
		if d.runs.waveStops[p.Wave] == 0 {
			delete(d.runs.waveStops, p.Wave)
		}
		f, err := d.loadRunRegistryLocked()
		if err != nil {
			fmt.Fprintf(os.Stderr, "start queued runs: %v\n", err)
		}
		d.startQueuedLocked(f)
		d.runs.mu.Unlock()
	}()
	result := &WaveResult{Wave: p.Wave, Items: []WaveItemResult{}}
	for _, rec := range members {
		d.runs.mu.Lock()
		final := false
		if r := d.runs.runs[rec.ID]; r != nil {
			final = runrec.IsFinal(r.engine.Snapshot().State)
		}
		d.runs.mu.Unlock()
		if final {
			continue
		}
		stopped, err := d.ownerRun(ctx, MethodRunStop, RunAction{Run: rec.ID})
		result.Items = append(result.Items, waveItem(rec.Item, stopped, err))
	}
	return result, nil
}

// loadRunRegistryLocked remembers successful reads for queue advancement.
// On error it returns the last good registry and the error. Admission still
// requires a fresh read. The caller holds the manager lock.
func (d *Daemon) loadRunRegistryLocked() (*registry.File, error) {
	f, err := d.loadRegistry()
	if err != nil {
		return d.runs.lastRegistry, err
	}
	d.runs.lastRegistry = f
	return f, nil
}

// hasRoomLocked reports whether one more busy run fits under the global cap
// and the project's cap. Zero reads as unset: the global cap then defaults
// and a project has no cap (adj 57, adj 20). Only busy runs hold a slot (adj
// 122). The caller holds the manager lock.
func (d *Daemon) hasRoomLocked(f *registry.File, project string) bool {
	global := f.GlobalCap
	if global == 0 {
		global = defaultGlobalCap
	}
	projectCap := 0
	if p, ok := find(f, project); ok {
		projectCap = p.Cap
	}
	total, inProject := 0, 0
	for _, r := range d.runs.runs {
		if !r.busy || r.engine == nil {
			continue
		}
		total++
		if r.engine.Snapshot().Project == project {
			inProject++
		}
	}
	return total < global && (projectCap == 0 || inProject < projectCap)
}

// startQueuedLocked starts the oldest queued run whose caps have room, and
// repeats until none fits. The caller holds the manager lock.
func (d *Daemon) startQueuedLocked(f *registry.File) {
	if d.runs.closed || f == nil {
		return
	}
	type waiting struct {
		r   *managedRun
		rec runrec.Record
	}
	var queue []waiting
	for _, r := range d.runs.runs {
		if r.busy || r.engine == nil {
			continue
		}
		if rec := r.engine.Snapshot(); rec.State == runrec.StateQueued && d.runs.waveStops[rec.Wave] == 0 {
			queue = append(queue, waiting{r, rec})
		}
	}
	sort.Slice(queue, func(i, j int) bool {
		a, b := queue[i].rec, queue[j].rec
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	for _, w := range queue {
		if _, ok := find(f, w.rec.Project); !ok {
			if err := w.r.engine.StopQueued(d.runs.ctx, "project was removed"); err != nil {
				fmt.Fprintf(os.Stderr, "stop queued run %s: %v\n", w.rec.ID, err)
			}
			continue
		}
		if !d.hasRoomLocked(f, w.rec.Project) {
			continue
		}
		if err := w.r.engine.Dequeue(); err != nil {
			fmt.Fprintf(os.Stderr, "start queued run %s: %v\n", w.rec.ID, err)
			continue
		}
		d.launchRun(w.r, w.r.engine.Run)
	}
}

// launchRun starts a job with the manager lock held. When the job ends it
// starts the queued runs that now fit.
func (d *Daemon) launchRun(r *managedRun, fn func(context.Context) error) {
	ctx, cancel := context.WithCancel(d.runs.ctx)
	r.cancel = cancel
	r.busy = true
	r.done = make(chan struct{})
	r.result = &runResult{}
	r.stop = nil // a handle left by an earlier stop must not join this job
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
		f, err := d.loadRunRegistryLocked()
		if err != nil {
			fmt.Fprintf(os.Stderr, "start queued runs: %v\n", err)
		}
		d.startQueuedLocked(f)
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
	// A stop that arrives while another stop is in flight joins it.
	if method == MethodRunStop && r.stop != nil && (r.busy || r.stopping) {
		return d.joinStop(ctx, r.stop)
	}
	if r.stopping {
		d.runs.mu.Unlock()
		return runrec.Record{}, fmt.Errorf("run %s is stopping", p.Run)
	}
	if r.busy && method != MethodRunStop {
		d.runs.mu.Unlock()
		if method == MethodRunRetry {
			return runrec.Record{}, fmt.Errorf("run %s is busy; use ork stop %s to stop it", p.Run, p.Run)
		}
		return runrec.Record{}, fmt.Errorf("run %s is busy", p.Run)
	}
	// Only a stop leaves a handle for later stops to join; any other job clears it.
	var call *stopCall
	if method == MethodRunStop {
		call = &stopCall{started: make(chan struct{})}
	}
	r.stop = call
	if r.busy {
		r.stopping = true
		r.cancel()
		done := r.done
		d.runs.mu.Unlock()
		<-done
		d.runs.mu.Lock()
		if d.runs.closed {
			call.abandon(errors.New("run manager is closed"))
			d.runs.mu.Unlock()
			return runrec.Record{}, errors.New("run manager is closed")
		}
	}
	r.stopping = false
	var fn func(context.Context) error
	switch method {
	case MethodRunAnswer:
		rec := r.engine.Snapshot()
		if rec.State == runrec.StatePaused && (rec.Pending == nil || rec.Pending.Kind != runrec.PendingQuestion) {
			d.runs.mu.Unlock()
			return runrec.Record{}, fmt.Errorf("run %s has no pending question; use ork retry %s", p.Run, p.Run)
		}
		if strings.TrimSpace(p.Text) == "" || rec.State != runrec.StatePaused || rec.Pending == nil || rec.Pending.Kind != runrec.PendingQuestion {
			d.runs.mu.Unlock()
			return runrec.Record{}, errors.New("run needs a pending question and a non-empty answer")
		}
		fn = func(ctx context.Context) error { return r.engine.Answer(ctx, p.Text) }
	case MethodRunRetry:
		rec := r.engine.Snapshot()
		if rec.State != runrec.StatePaused || rec.Pending == nil {
			d.runs.mu.Unlock()
			return runrec.Record{}, fmt.Errorf("run %s is not paused with a pending step; use ork stop %s to stop it", p.Run, p.Run)
		}
		if rec.Pending.Kind == runrec.PendingQuestion {
			d.runs.mu.Unlock()
			return runrec.Record{}, fmt.Errorf("run %s waits for an answer; use ork answer %s TEXT or ork stop %s", p.Run, p.Run, p.Run)
		}
		fn = r.engine.Retry
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
	r.stop = call // launchRun cleared the handle; a stop sets it again for later stops to join
	if call != nil {
		call.begin(r.done, r.result)
	}
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

// joinStop waits for the stop in flight and returns its outcome. The caller
// holds the manager lock and gives it up.
func (d *Daemon) joinStop(ctx context.Context, c *stopCall) (runrec.Record, error) {
	d.runs.mu.Unlock()
	select {
	case <-ctx.Done():
		return runrec.Record{}, ctx.Err()
	case <-c.started:
	}
	select {
	case <-ctx.Done():
		return runrec.Record{}, ctx.Err()
	case <-c.done:
	}
	return c.result.record, c.result.err
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
func (d *Daemon) engineRuns(project string) []EngineRun {
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "run status: %v\n", err)
	}
	runs := []EngineRun{}
	for _, r := range stored {
		rec := r.Snapshot()
		if m := d.runs.runs[rec.ID]; m != nil {
			rec = m.engine.Snapshot()
		}
		if rec.Project != project {
			continue
		}
		pending := ""
		if rec.State == runrec.StatePaused && rec.Pending != nil {
			pending = rec.Pending.Kind
		}
		wait := rec.Reason
		if rec.Pending != nil {
			wait = rec.Pending.Reason
			if rec.Pending.Kind == runrec.PendingQuestion && rec.Pending.Question != "" {
				wait = rec.Pending.Question
			}
		}
		switch rec.State {
		case runrec.StatePaused:
			next := "ork retry " + rec.ID
			if rec.Pending != nil && rec.Pending.Kind == runrec.PendingQuestion {
				next = "ork answer " + rec.ID + " TEXT"
			}
			wait += "; next: " + next
		case runrec.StateAwaitingApproval:
			wait = "owner approval"
		}
		var denials []string
		var roles []source.EngineRole
		for _, role := range rec.Roles {
			roles = append(roles, source.EngineRole{Role: role.Role, Round: role.Round, Vendor: role.Vendor, Model: role.Model, Effort: role.Effort, Step: role.Step, StartedAt: role.StartedAt, EndedAt: role.EndedAt, Exit: role.Exit})
			denials = append(denials, roleDenials(role)...)
		}
		var activity []int
		if m := d.runs.runs[rec.ID]; m != nil {
			activity = m.engine.Activity(d.cfg.Clock.Now())
		}
		runs = append(runs, EngineRun{Activity: activity, Roles: roles, Denials: denials, ID: rec.ID, Item: rec.Item, Branch: rec.Branch, Worktree: rec.Worktree, Merged: d.runs.merged[rec.ID], State: rec.State, Wait: wait, Pending: pending, RelayRun: rec.RelayRun, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt})
	}
	return runs
}

// roleDenials returns the text of each denial a role had (DeniedCommand.Text).
// A record from before S45 has no denied_commands, so its tool names stand.
func roleDenials(role runrec.RoleRecord) []string {
	if len(role.DeniedCommands) == 0 {
		return role.Denials
	}
	texts := make([]string, 0, len(role.DeniedCommands))
	for _, d := range role.DeniedCommands {
		texts = append(texts, d.Text())
	}
	return texts
}
