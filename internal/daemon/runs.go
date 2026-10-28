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
	"slices"
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
	MethodRunDismiss = "run.dismiss"
	MethodRunPlan    = "run.plan"
	MethodRunStart   = "run.start"
	MethodRunApprove = "run.approve"
	MethodRunStop    = "run.stop"
	MethodRunAnswer  = "run.answer"
	MethodRunRetry   = "run.retry"
	MethodRunFile    = "run.file"   // proposed: file a later finding as a slice (adj 163)
	MethodRunFix     = "run.fix"    // proposed: send the owner's note to a fix round (S118)
	MethodRunCheck   = "run.check"  // proposed: tick a human check or mark it later (S118)
	MethodRunRender  = "run.render" // proposed: the text of one UI pass render of a run (S121)
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
	Check   int    `json:"check,omitempty"`   // run.check: the one-based human check
	State   string `json:"state,omitempty"`   // run.check: "ticked", "later" or "" to reopen
	Finding int    `json:"finding,omitempty"` // run.file: the one-based finding to file
	Name    string `json:"name,omitempty"`    // run.render: the render's file name, as the card lists it
	Adj     bool   `json:"adj,omitempty"`
	By      string `json:"by,omitempty"`
	Run     string `json:"run"`
	Text    string `json:"text,omitempty"`
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
	Note         string                          `json:"note,omitempty"` // why rule-based roles could not be identified
}

// EngineRun is the owner's status of one engine run. Wire fields are proposed.
type EngineRun = source.EngineRun

type runResult struct {
	err    error
	record runrec.Record
}

type managedRun struct {
	failure        error
	recoveryFailed bool // failed recovery does not hold off idle exit
	engine         *engine.Engine
	cancel         context.CancelFunc
	done           chan struct{}
	result         *runResult
	busy           bool
	stopping       bool
	stop           *stopCall // the owner stop in flight, which later stops join
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
	recordLocks  map[string]*sync.Mutex // per-run record writes; guarded by mu
	cleaning     map[string]bool        // cleanup calls in flight; guarded by mu
	heads        map[string]string
	merged       map[string]*bool
	lastRegistry *registry.File             // last successful run-manager read; guarded by mu
	waveStops    map[string]int             // suppress queued members while a wave stop is in progress
	specs        map[string]specEntry       // the spec checks the daemon remembers, by project and item; guarded by mu
	specJobs     int                        // spec checks in flight; guarded by mu
	specSeq      int                        // numbers the spec check jobs; guarded by mu
	specCancels  map[int]context.CancelFunc // cancels each spec check job; guarded by mu
	specMu       sync.Mutex                 // serializes spec checks and answers
	specStateMu  sync.Mutex                 // short state operations; lock order: specMu -> specStateMu -> mu
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
	d.runs = runManager{recordLocks: map[string]*sync.Mutex{}, cleaning: map[string]bool{}, heads: map[string]string{}, merged: map[string]*bool{}, ctx: context.Background(), deps: deps, runs: map[string]*managedRun{}, waveStops: map[string]int{}, specs: map[string]specEntry{}, specCancels: map[int]context.CancelFunc{}}
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

// recoverRuns reconciles outside the manager lock, then publishes each fully
// initialized run under the lock used by run-manager readers.
func (d *Daemon) recoverRuns(ctx context.Context) error {
	d.sweepSpecDirs(ctx)
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
			if !rec.LandedAt.IsZero() {
				d.runs.specStateMu.Lock()
				d.runs.mu.Lock()
				d.removeSpecDirLocked(rec.Project, rec.Item)
				d.runs.mu.Unlock()
				d.runs.specStateMu.Unlock()
			}
			continue
		}
		p, ok := find(f, rec.Project)
		if !ok {
			err := fmt.Errorf("project %q is not registered", rec.Project)
			fmt.Fprintf(os.Stderr, "run %s: %v\n", rec.ID, err)
			if rec.State == runrec.StateQueued {
				// StopQueued only needs the stored record, not project settings.
				p = registry.Project{Name: rec.Project}
				e := engine.Load(d.runDeps(p), p, r)
				m := &managedRun{engine: e}
				if err := e.StopQueued(ctx, "project was removed"); err != nil {
					m.failure = err
					m.recoveryFailed = true
					fmt.Fprintf(os.Stderr, "stop queued run %s: %v\n", rec.ID, err)
				}
				d.runs.mu.Lock()
				d.runs.runs[rec.ID] = m
				d.runs.mu.Unlock()
			} else {
				d.runs.mu.Lock()
				d.runs.runs[rec.ID] = &managedRun{failure: err, recoveryFailed: true}
				d.runs.mu.Unlock()
			}
			continue
		}
		e := engine.Load(d.runDeps(p), p, r)
		if rec.State == runrec.StateQueued {
			// A queued run has no process and wrote nothing; it keeps its place.
			d.runs.mu.Lock()
			d.runs.runs[rec.ID] = &managedRun{engine: e}
			d.runs.mu.Unlock()
			continue
		}
		if err := e.Reconcile(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "reconcile %s: %v\n", rec.ID, err)
			d.runs.mu.Lock()
			d.runs.runs[rec.ID] = &managedRun{engine: e, failure: err, recoveryFailed: true}
			d.runs.mu.Unlock()
			continue
		}
		d.runs.mu.Lock()
		d.runs.runs[rec.ID] = &managedRun{engine: e}
		d.runs.mu.Unlock()
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
	for _, cancel := range d.runs.specCancels {
		cancel()
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
	if d.runs.specJobs > 0 {
		return true
	}
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
		rec := r.Snapshot()
		if m := d.runs.runs[rec.ID]; m != nil && m.recoveryFailed {
			continue
		}
		switch rec.State {
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

// appendEntry adds an entry to a fallback chain, unless it is identical to the
// entry before it: two matrix rows with the same vendor, model and effort give
// one step of the chain, not two.
func appendEntry(chain []runrec.LineupEntry, e runrec.LineupEntry) []runrec.LineupEntry {
	if n := len(chain); n > 0 && chain[n-1] == e {
		return chain
	}
	return append(chain, e)
}

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
	rules, rulesErr := d.runs.deps.Slicer.RelayRules(ctx, proj.Path)
	if rulesErr != nil {
		rules = nil
	}
	// The UI pass row is a reviewer row that only the rule naming it tells
	// apart (rule u10; slicer s392 will give it a ui role). It runs as the UI
	// pass on a UI slice and is never a fallback of the reviewer or the gate.
	uiRow := slicercli.UIPassRowID(rules)
	// The spec check row is a reviewer row too (rule u11). It runs only the
	// spec check, so a run's chains never hold it.
	specRow := slicercli.SpecCheckRowID(rules)
	var uiRows []slicercli.Row
	plan := &RunPlan{project: proj, Project: registry.Project{Name: proj.Name, Path: proj.Path}, Item: p.Item, CheckCommand: proj.CheckCommand, Lineup: map[string][]runrec.LineupEntry{}, Permissions: map[string]engine.Permission{}}
	if rulesErr != nil {
		plan.Note = "slicer relay-roles show failed; planning without UI or spec-check rules: " + rulesErr.Error()
	}
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
				if specRow != "" && sourceRole == engine.RoleReviewer && row.ID == specRow {
					continue
				}
				if uiRow != "" && sourceRole == engine.RoleReviewer && row.ID == uiRow {
					if role == engine.RoleReviewer {
						uiRows = append(uiRows, row)
					}
					continue
				}
				if vendor := matrixAdapters[strings.ToLower(row.Agent)]; vendor != "" {
					plan.Lineup[role] = appendEntry(plan.Lineup[role], runrec.LineupEntry{Vendor: vendor, Model: row.Model, Effort: row.Reasoning})
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
	var uiChain []runrec.LineupEntry
	for _, row := range uiRows {
		if vendor := matrixAdapters[strings.ToLower(row.Agent)]; vendor != "" {
			uiChain = appendEntry(uiChain, runrec.LineupEntry{Vendor: vendor, Model: row.Model, Effort: row.Reasoning})
		}
	}
	if len(uiChain) > 0 {
		perm, err := engine.ResolvePermission(proj, engine.RoleUI)
		if err != nil {
			return nil, err
		}
		plan.Lineup[engine.RoleUI] = uiChain
		plan.Permissions[engine.RoleUI] = perm
	}
	for role, entries := range p.Lineup {
		if role != engine.RoleImplementer && role != engine.RoleReviewer && role != engine.RoleArchitect {
			return nil, badParams(fmt.Errorf("cannot override lineup role %q", role))
		}
		if role == engine.RoleArchitect && len(entries) == 0 {
			delete(plan.Lineup, role)
			delete(plan.Permissions, role)
			continue
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
		if role == engine.RoleArchitect {
			perm, err := engine.ResolvePermission(proj, role)
			if err != nil {
				return nil, err
			}
			plan.Permissions[role] = perm
		}
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
	plan.Worktree = registry.FillPattern(pattern, p.Item)
	if !filepath.IsAbs(plan.Worktree) {
		plan.Worktree = filepath.Join(proj.Path, plan.Worktree)
	}
	plan.Worktree = filepath.Clean(plan.Worktree)
	pattern = proj.BranchPattern
	if pattern == "" {
		pattern = "{id}"
	}
	plan.Branch = registry.FillPattern(pattern, p.Item)
	return plan, nil
}
func (d *Daemon) startRun(ctx context.Context, p RunParams) (runrec.Record, error) {
	plan, err := d.planRun(ctx, p)
	if err != nil {
		return runrec.Record{}, err
	}
	if err := d.restoreRunSpecAnswers(ctx, plan.project, p.Item); err != nil {
		return runrec.Record{}, err
	}
	if p.Expected != nil {
		shown, _ := json.Marshal(p.Expected)
		current, _ := json.Marshal(plan)
		if string(shown) != string(current) {
			return runrec.Record{}, errors.New("plan changed; run the command again to confirm it")
		}
	}
	d.runs.specStateMu.Lock()
	defer d.runs.specStateMu.Unlock()
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
	if _, ok := find(f, plan.project.Name); !ok {
		return runrec.Record{}, notFound(plan.project.Name, f)
	}
	queued := !d.hasRoomLocked(f, plan.project.Name)
	wave := p.wave
	if wave == "" {
		wave = newWaveID(time.Now())
	}
	// A spec check that the owner answered gave the item decisions that its
	// worktree's spec lacks; the run carries them into every brief (S116).
	cfg := engine.Config{Project: plan.project, Item: p.Item, Lineup: plan.Lineup, Budgets: plan.Budgets, Wave: wave, Queued: queued, Decisions: d.runs.specs[specKey(plan.project.Name, p.Item)].decisions}
	e, err := engine.New(ctx, d.runDeps(plan.project), cfg)
	if err != nil {
		return runrec.Record{}, err
	}
	// The run record now owns the paragraph, including for queued runs.
	if err := os.Remove(filepath.Join(d.specDir(plan.project.Name, p.Item), "answers.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "remove transferred spec answers: %v\n", err)
	}
	delete(d.runs.specs, specKey(plan.project.Name, p.Item))
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
	// Inspect every Files section before admitting any member.
	if len(p.Items) > 1 {
		f, err := d.loadRegistry()
		if err != nil {
			return nil, err
		}
		proj, ok := find(f, project)
		if !ok {
			return nil, notFound(project, f)
		}
		first := ""
		for _, item := range p.Items {
			detail, err := d.runs.deps.Slicer.Show(ctx, proj.Path, item.Item)
			if err != nil {
				return nil, err
			}
			if EngineSlice(detail) {
				if first != "" {
					return nil, badParams(errors.New(EngineConflict(first, item.Item)))
				}
				first = item.Item
			}
		}
	}
	result := &WaveResult{Wave: newWaveID(time.Now()), Items: []WaveItemResult{}}
	for _, item := range p.Items {
		item.wave = result.Wave
		if err := d.specGate(ctx, item.Project, item.Item); err != nil {
			result.Items = append(result.Items, waveItem(item.Item, runrec.Record{}, err))
			continue
		}
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
		if r := d.runs.runs[rec.ID]; r != nil && r.engine != nil {
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
		if r := d.runs.runs[rec.ID]; r != nil && r.engine != nil {
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
	r.failure = nil
	r.recoveryFailed = false
	r.stop = nil // a handle left by an earlier stop must not join this job
	result := r.result
	d.runs.wg.Add(1)
	go func() {
		defer d.runs.wg.Done()
		defer cancel()
		err := fn(ctx)
		if !r.engine.Snapshot().LandedAt.IsZero() {
			d.runs.specStateMu.Lock()
			defer d.runs.specStateMu.Unlock()
		}
		d.runs.mu.Lock()
		result.err = err
		if !errors.Is(err, context.Canceled) {
			r.failure = err
		}
		result.record = r.engine.Snapshot()
		if !result.record.LandedAt.IsZero() {
			d.removeSpecDirLocked(result.record.Project, result.record.Item)
		}
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

// RunReply adds a proposed transient notice to the usual run record reply.
type RunReply struct {
	runrec.Record
	Message string `json:"message,omitempty"`
}

func (d *Daemon) ownerRun(ctx context.Context, method string, p RunAction) (runrec.Record, error) {
	reply, err := d.ownerRunReply(ctx, method, p)
	return reply.Record, err
}

func (d *Daemon) ownerRunReply(ctx context.Context, method string, p RunAction) (RunReply, error) {
	d.runs.mu.Lock()
	if d.runs.closed {
		d.runs.mu.Unlock()
		return RunReply{}, errors.New("run manager is closed")
	}
	r := d.runs.runs[p.Run]
	if r == nil {
		d.runs.mu.Unlock()
		return RunReply{}, &ipc.Error{Code: CodeNotFound, Message: "no run " + p.Run}
	}
	if r.engine == nil {
		run, err := d.runs.deps.Store.Open(p.Run)
		failure := r.failure
		d.runs.mu.Unlock()
		if err != nil {
			return RunReply{}, err
		}
		return RunReply{Record: run.Snapshot()}, failure
	}
	// A stop that arrives while another stop is in flight joins it.
	if method == MethodRunStop && r.stop != nil && (r.busy || r.stopping) {
		rec, err := d.joinStop(ctx, r.stop)
		return RunReply{Record: rec}, err
	}
	if r.stopping {
		d.runs.mu.Unlock()
		return RunReply{}, fmt.Errorf("run %s is stopping", p.Run)
	}
	if r.busy && method != MethodRunStop {
		d.runs.mu.Unlock()
		if method == MethodRunRetry {
			return RunReply{}, fmt.Errorf("run %s is busy; use ork stop %s to stop it", p.Run, p.Run)
		}
		return RunReply{}, fmt.Errorf("run %s is busy", p.Run)
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
			return RunReply{}, errors.New("run manager is closed")
		}
	}
	r.stopping = false
	var fn func(context.Context) error
	var message string
	switch method {
	case MethodRunAnswer:
		rec := r.engine.Snapshot()
		if rec.State == runrec.StatePaused && (rec.Pending == nil || rec.Pending.Kind != runrec.PendingQuestion) {
			d.runs.mu.Unlock()
			return RunReply{}, fmt.Errorf("run %s has no pending question; use ork retry %s", p.Run, p.Run)
		}
		if strings.TrimSpace(p.Text) == "" || rec.State != runrec.StatePaused || rec.Pending == nil || rec.Pending.Kind != runrec.PendingQuestion {
			d.runs.mu.Unlock()
			return RunReply{}, errors.New("run needs a pending question and a non-empty answer")
		}
		if p.Adj && rec.Pending.Key != "" && strings.TrimSpace(p.By) == "" {
			d.runs.mu.Unlock()
			return RunReply{}, errors.New("ADJ_DECIDED_BY is unset; set it in the caller before filing an answer in adj")
		}
		fn = func(ctx context.Context) error {
			if p.Adj {
				var err error
				message, err = r.engine.FileAnswer(ctx, p.Text, p.By)
				if err != nil {
					return err
				}
			}
			return r.engine.Answer(ctx, p.Text)
		}
	case MethodRunRetry:
		rec := r.engine.Snapshot()
		if rec.State != runrec.StatePaused || rec.Pending == nil {
			d.runs.mu.Unlock()
			return RunReply{}, fmt.Errorf("run %s is not paused with a pending step; use ork stop %s to stop it", p.Run, p.Run)
		}
		if rec.Pending.Kind == runrec.PendingQuestion {
			d.runs.mu.Unlock()
			return RunReply{}, fmt.Errorf("run %s waits for an answer; use ork answer %s TEXT or ork stop %s", p.Run, p.Run, p.Run)
		}
		fn = r.engine.Retry
	case MethodRunApprove:
		fn = r.engine.Approve
	case MethodRunFix:
		fn = func(ctx context.Context) error { return r.engine.Fix(ctx, p.Text) }
	case MethodRunCheck:
		fn = func(context.Context) error { return r.engine.MarkCheck(p.Check, p.State) }
	case MethodRunStop:
		fn = func(ctx context.Context) error {
			// A cancelled round leaves its role live in the record. Reconcile checks
			// and kills any surviving group before recording the owner's stop.
			if err := r.engine.Reconcile(ctx); err != nil {
				d.runs.mu.Lock()
				r.recoveryFailed = true
				d.runs.mu.Unlock()
				return err
			}
			return r.engine.Stop(ctx, "CLI stop")
		}
	default:
		d.runs.mu.Unlock()
		return RunReply{}, errors.New("unknown owner action")
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
		return RunReply{}, ctx.Err()
	case <-done:
	}
	return RunReply{Record: result.record, Message: message}, result.err
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

// fileFinding files a later finding of a run as a slice. It does not take the
// run's owner-action turn: the run may be busy, and a role keeps running.
func (d *Daemon) fileFinding(ctx context.Context, p RunAction) (RunReply, error) {
	return d.findingAction(ctx, p, false)
}

func (d *Daemon) findingAction(ctx context.Context, p RunAction, answer bool) (RunReply, error) {
	lock := d.runRecordLock(p.Run)
	lock.Lock()
	defer lock.Unlock()
	d.runs.mu.Lock()
	if d.runs.closed {
		d.runs.mu.Unlock()
		return RunReply{}, errors.New("run manager is closed")
	}
	var eng *engine.Engine
	if r := d.runs.runs[p.Run]; r != nil {
		eng = r.engine
	}
	d.runs.mu.Unlock()
	if eng == nil {
		var err error
		eng, err = d.loadFinishedEngine(p.Run)
		if err != nil {
			return RunReply{}, err
		}
	}
	var id string
	var err error
	if answer {
		id, err = eng.AnswerFinding(ctx, p.Finding, p.Text, p.By)
		if err != nil {
			return RunReply{}, err
		}
		return RunReply{Record: eng.Snapshot(), Message: id}, nil
	}
	id, err = eng.FileLater(ctx, p.Finding)
	if err != nil {
		return RunReply{}, err
	}
	return RunReply{Record: eng.Snapshot(), Message: "filed as " + id}, nil
}

// runRecordLock returns the mutex shared by finding actions, dismissal and
// cleanup. Callers take it before the manager lock and keep it through writes.
func (d *Daemon) runRecordLock(id string) *sync.Mutex {
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	lock := d.runs.recordLocks[id]
	if lock == nil {
		lock = &sync.Mutex{}
		d.runs.recordLocks[id] = lock
	}
	return lock
}

func (d *Daemon) loadFinishedEngine(id string) (*engine.Engine, error) {
	// A stopped or committed run is not rebuilt at a daemon start, but its
	// findings stay actionable. Open it fresh while holding the per-run lock
	// so other finished-run writes cannot be overwritten by an older snapshot.
	run, err := d.runs.deps.Store.Open(id)
	if err != nil {
		return nil, &ipc.Error{Code: CodeNotFound, Message: "no run " + id}
	}
	rec := run.Snapshot()
	if !runrec.IsFinal(rec.State) {
		return nil, &ipc.Error{Code: CodeNotFound, Message: "no run engine for " + id}
	}
	proj, err := d.registered(rec.Project)
	if err != nil {
		return nil, err
	}
	return engine.Load(d.runDeps(proj), proj, run), nil
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
	if method == MethodRunDismiss {
		return d.dismissRun(p)
	}
	if method == MethodRunAnswer && p.Finding != 0 {
		if strings.TrimSpace(p.Text) == "" {
			return nil, errors.New("the answer is empty")
		}
		return d.findingAction(ctx, p, true)
	}
	if method == MethodRunAnswer {
		return d.ownerRunReply(ctx, method, p)
	}
	if method == MethodRunFile {
		return d.fileFinding(ctx, p)
	}
	if method == MethodRunRender {
		return d.runRender(p)
	}
	return d.ownerRun(ctx, method, p)
}

// dismissRun holds the manager lock so dismissal cannot race an owner action.
func (d *Daemon) dismissRun(p RunAction) (runrec.Record, error) {
	lock := d.runRecordLock(p.Run)
	lock.Lock()
	defer lock.Unlock()
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	if d.runs.closed {
		return runrec.Record{}, errors.New("run manager is closed")
	}
	if r := d.runs.runs[p.Run]; r != nil {
		if r.busy || r.stopping {
			return runrec.Record{}, fmt.Errorf("run %s is busy", p.Run)
		}
		if r.engine != nil {
			if err := r.engine.Dismiss(); err != nil {
				return runrec.Record{}, err
			}
			return r.engine.Snapshot(), nil
		}
	}
	r, err := d.runs.deps.Store.Open(p.Run)
	if errors.Is(err, os.ErrNotExist) {
		return runrec.Record{}, &ipc.Error{Code: CodeNotFound, Message: "no run " + p.Run}
	}
	if err != nil {
		return runrec.Record{}, err
	}
	if err := r.Dismiss(); err != nil {
		return runrec.Record{}, err
	}
	return r.Snapshot(), nil
}

// unfinishedRunsLocked returns sorted unfinished run IDs for the project.
// The caller holds d.runs.mu.
func (d *Daemon) unfinishedRunsLocked(project string) ([]string, error) {
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, r := range stored {
		rec := r.Snapshot()
		if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil {
			rec = m.engine.Snapshot()
		}
		if rec.Project == project && !runrec.IsFinal(rec.State) {
			ids = append(ids, rec.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
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
		if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil {
			rec = m.engine.Snapshot()
		}
		if rec.Project != project {
			continue
		}

		var question *runrec.OwnerQuestion
		questionKey := ""
		pending := ""
		if rec.State == runrec.StatePaused && rec.Pending != nil {
			pending = rec.Pending.Kind
			questionKey = rec.Pending.Key
			if pending == runrec.PendingQuestion {
				question = rec.Pending.Ask
			}
		}
		wait := rec.Reason
		if rec.State == runrec.StatePaused && rec.Pending != nil {
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
		denialCounts := map[string]int{}
		var roles []source.EngineRole
		for _, role := range rec.Roles {
			roles = append(roles, source.EngineRole{Role: role.Role, Round: role.Round, Vendor: role.Vendor, Model: role.Model, Effort: role.Effort, Step: role.Step, StartedAt: role.StartedAt, EndedAt: role.EndedAt, Exit: role.Exit, Tokens: role.Tokens, CostUSD: role.CostUSD})
			for _, text := range roleDenials(role) {
				if denialCounts[text] == 0 {
					denials = append(denials, text)
				}
				denialCounts[text]++
			}
		}
		for i, text := range denials {
			if count := denialCounts[text]; count > 1 {
				denials[i] = fmt.Sprintf("%s x%d", text, count)
			}
		}
		tokens := 0
		usageKnown := false
		cost := 0.0
		for _, role := range rec.Roles {
			tokens += role.Tokens
			usageKnown = usageKnown || !role.EndedAt.IsZero() || role.Tokens != 0
			cost += role.CostUSD
		}
		var live engine.Live
		var activity []int
		if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil {
			activity = m.engine.Activity(d.cfg.Clock.Now())
			live = m.engine.Live()
			tokens += live.Tokens
		}
		unavailable := false
		if m := d.runs.runs[rec.ID]; m != nil {
			unavailable = m.engine == nil
		}
		if unavailable {
			wait = source.RemovedProjectHelp(rec.Project)
		}
		failure := ""
		if m := d.runs.runs[rec.ID]; m != nil && !m.busy && m.failure != nil {
			failure = m.failure.Error()
		}
		var findings []source.EngineFinding
		for i, f := range rec.Findings {
			findings = append(findings, source.EngineFinding{N: i + 1, Kind: f.Kind, Title: f.Title, Ask: f.Ask, Filed: f.Filed, Answer: f.Answer, Key: f.Key, Scores: f.Scores})
		}
		var card *runrec.ApprovalCard
		if rec.State == runrec.StateAwaitingApproval {
			card = rec.Card
		}
		runs = append(runs, EngineRun{Findings: findings, Card: card, Question: question, Key: questionKey, Unavailable: unavailable, Error: failure, Activity: activity, Tail: live.Text, TailTruncated: live.TailTruncated, UsageKnown: usageKnown || live.UsageKnown, LiveUsageKnown: live.UsageKnown, Tokens: tokens, CostUSD: cost, Roles: roles, Denials: denials, ID: rec.ID, Item: rec.Item, Branch: rec.Branch, Worktree: rec.Worktree, Merged: d.runs.merged[rec.ID], Landed: !rec.LandedAt.IsZero(), Dismissed: !rec.DismissedAt.IsZero(), CleanupFailed: cleanupFailed(rec), Cleaned: rec.Cleaned(), WorktreeGone: rec.State == runrec.StateCommitted && !rec.Cleaned() && worktreeGone(rec), State: rec.State, Wait: wait, Pending: pending, RelayRun: rec.RelayRun, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt, WaitStartedAt: rec.WaitStartedAt})
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

// cleanupFailed is the reason a landed run's cleanup failed: a run that
// landed and is committed keeps the reason in its record (S119).
func cleanupFailed(rec runrec.Record) string {
	if rec.LandedAt.IsZero() || rec.State != runrec.StateCommitted {
		return ""
	}
	return rec.Reason
}

// RenderReply is the answer of run.render: the render's name and its text,
// plain text of at most 24 KiB.
type RenderReply struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// runRender reads one render of a run (run.render, S121, *proposed*). The
// render must be one the run's card lists, and the engine's ReadRender applies
// its guards to the file: a regular .txt file of the UI pass's renders
// directory, not a link, as plain text of at most 24 KiB. Any run, name or file
// that fails is not_found. It does not wait for the run: it reads the stored
// record and the file.
func (d *Daemon) runRender(p RunAction) (RenderReply, error) {
	run, err := d.runs.deps.Store.Open(p.Run)
	if err != nil {
		return RenderReply{}, &ipc.Error{Code: CodeNotFound, Message: "no run " + p.Run}
	}
	missing := &ipc.Error{Code: CodeNotFound, Message: "no render " + p.Name + " for run " + p.Run}
	card := run.Snapshot().Card
	if card == nil || card.UIRound == 0 || !slices.ContainsFunc(card.Renders, func(r runrec.Render) bool { return r.Name == p.Name }) {
		return RenderReply{}, missing
	}
	text, err := engine.ReadRender(d.runs.deps.Store.RunDir(p.Run), card.UIRound, p.Name)
	if err != nil {
		return RenderReply{}, missing
	}
	return RenderReply{Name: p.Name, Text: text}, nil
}
