// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	MethodRunLandCheck = "run.landcheck" // read-only landing warning (S217)
	MethodRunDismiss   = "run.dismiss"
	MethodRunPlan      = "run.plan"
	MethodRunStart     = "run.start"
	MethodRunApprove   = "run.approve"
	MethodRunStop      = "run.stop"
	MethodRunAnswer    = "run.answer"
	MethodRunRetry     = "run.retry"
	MethodRunFile      = "run.file"   // proposed: file a later finding as a slice (adj 163)
	MethodRunFix       = "run.fix"    // proposed: send the owner's note to a fix round (S118)
	MethodRunCheck     = "run.check"  // proposed: tick a human check or mark it later (S118)
	MethodRunReport    = "run.report" // proposed: role report text (S176)
	MethodRunRender    = "run.render" // proposed: the text of one UI pass render of a run (S121)
	MethodRunShow      = "run.show"   // proposed: a run's pause, roles, checks and findings in full (S297)

	MethodRunEndLedger = "run.end-ledger" // proposed: end a stale ledger run (S175)
)

// defaultGlobalCap is the global cap when the registry sets none (adj 172).
const defaultGlobalCap = 2

// RunParams are the owner's launch inputs (run.lineup-inputs, run.launch-wire).
type RunParams struct {
	StopAfter     string                          `json:"stop_after,omitempty"`
	chained       bool                            // daemon-assigned: this plan is one sliver of a chain, which is never carved (S359)
	wave          string                          // daemon-assigned; never accepted from a run.start client
	waveName      string                          // daemon-assigned from WaveStartParams.Name (S215, *proposed*)
	Project       string                          `json:"project"`
	Item          string                          `json:"item"`
	WellSpecified bool                            `json:"well_specified"`
	Split         bool                            `json:"split"`
	Slivers       bool                            `json:"slivers,omitempty"` // ork run PARENT --slivers (S62)
	Expected      *RunPlan                        `json:"expected,omitempty"`
	Lineup        map[string][]runrec.LineupEntry `json:"lineup,omitempty"`
	Budgets       *registry.Budgets               `json:"budgets,omitempty"`
	AllowNoCheck  bool                            `json:"allow_no_check,omitempty"` // proposed (S265, adj 191): start although the project has no check command
}

// RunAction names a run, and an owner answer when answering.
type RunAction struct {
	Check   int    `json:"check,omitempty"`   // run.check: the one-based human check
	State   string `json:"state,omitempty"`   // run.check: "ticked", "later" or "" to reopen
	Finding int    `json:"finding,omitempty"` // run.file: the one-based finding to file
	Name    string `json:"name,omitempty"`    // run.render or run.report: a file name listed in the stored record
	Project string `json:"project,omitempty"` // run.end-ledger: the registered project that holds the ledger run
	Adj     bool   `json:"adj,omitempty"`
	By      string `json:"by,omitempty"`
	Run     string `json:"run"`
	Text    string `json:"text,omitempty"`
	Rounds  int    `json:"rounds,omitempty"` // run.retry: additional implementer rounds; 0 means 2
	// run.retry at a capped shape pause (S298, *proposed*): keep the refused
	// report's owner findings that have no options, or run the role once more.
	AcceptShape bool `json:"accept_shape,omitempty"`
	Again       bool `json:"again,omitempty"`
	NoWait      bool `json:"no_wait,omitempty"` // proposed, run.answer and run.retry: return once the action is applied and the run has left its pause (S237)
	// S316 (*proposed*; adj 233, 234): run.check with All marks every open check
	// later (State must be "later"); run.approve with LaterChecks does the same
	// and then approves.
	All         bool `json:"all,omitempty"`
	LaterChecks bool `json:"later_checks,omitempty"`
}

// RunPlan is checkpoint 1 (run.launch-wire).
type RunPlan struct {
	StopAfter         string                          `json:"stop_after,omitempty"`
	project           registry.Project                // registry settings are private to the daemon
	effective         registry.Project                // project with the check command the run uses (S246); the engine gets this one
	Project           registry.Project                `json:"project"`
	Item              string                          `json:"item"`
	Parent            string                          `json:"parent,omitempty"`  // set when the item is a sliver, or the parent of a sliver chain (S62)
	Split             bool                            `json:"split,omitempty"`   // a lead and parts in one worktree (S61, *proposed*); the lineup then has a part chain
	Slivers           []SliverPlan                    `json:"slivers,omitempty"` // the chain, in slicer's order; empty for one item
	carved            []engine.CarvedSliver           // the saved carve report whose filing a spec check finishes (S359); private to the daemon
	Carve             bool                            `json:"carve,omitempty"` // a local implementer on a slice above size S with its slivers not filed yet: the spec check carves them, and the plan is made again to list them (S359, *proposed*)
	Worktree          string                          `json:"worktree"`
	Branch            string                          `json:"branch"`
	CheckCommand      string                          `json:"check_command"`
	CheckSource       string                          `json:"check_source,omitempty"`        // S246: where CheckCommand came from, "slicer", "registry" or "none"
	RoundCheckCommand string                          `json:"round_check_command,omitempty"` // S285, *proposed*: the faster check after implementer rounds
	Lineup            map[string][]runrec.LineupEntry `json:"lineup"`
	Budgets           registry.Budgets                `json:"budgets"`
	Permissions       map[string]engine.Permission    `json:"permissions"`
	Note              string                          `json:"note,omitempty"`     // why rule-based roles could not be identified
	Warnings          []string                        `json:"warnings,omitempty"` // proposed (S265): setup problems, see engine.SetupWarnings
}

// SliverPlan is one sliver of a chain the owner confirms as one plan (S62).
type SliverPlan struct {
	Item   string                          `json:"item"`
	Parent string                          `json:"parent,omitempty"`
	Index  int                             `json:"index"`
	Count  int                             `json:"count"`
	Lineup map[string][]runrec.LineupEntry `json:"lineup,omitempty"`
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
	filing         bool          // an owner answer is being filed in adj for a resume that waits; no other owner action starts (S274)
	filed          chan struct{} // closed when that filing ends; a stop waits on it
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
	notifications   *notifier
	approvals       map[string]*approvalSession // live local attempts, guarded by mu
	permissionSeq   uint64                      // monotonically orders live permission questions
	recordLocks     map[string]*sync.Mutex      // per-run record writes; guarded by mu
	cleaning        map[string]bool             // cleanup calls in flight; guarded by mu
	heads           map[string]string
	merged          map[string]*bool
	mergeLogged     map[string]string          // run ID to the last merge detection error logged for it; guarded by mu (S263)
	lastRegistry    *registry.File             // last successful run-manager read; guarded by mu
	waveStops       map[string]int             // suppress queued members while a wave stop is in progress
	specs           map[string]specEntry       // the spec checks the daemon remembers, by project and item; guarded by mu
	drafts          map[string]specDraftEntry  // the spec drafts the owner has not approved yet, by project and item; guarded by mu (S179)
	specJobs        int                        // spec checks in flight; guarded by mu
	specSeq         int                        // numbers the spec check jobs; guarded by mu
	specCancels     map[int]context.CancelFunc // cancels each spec check job; guarded by mu
	specMu          sync.Mutex                 // serializes spec checks and answers
	specStateMu     sync.Mutex                 // short state operations; lock order: specMu -> specStateMu -> mu
	closed          bool
	localWakeCancel context.CancelFunc
	mu              sync.Mutex
	ctx             context.Context
	deps            engine.Deps
	runs            map[string]*managedRun
	wg              sync.WaitGroup
}

func (d *Daemon) initRunManager() {
	deps := engine.Deps{}
	if d.cfg.Engine != nil {
		deps = *d.cfg.Engine
	}
	if deps.Store == nil {
		deps.Store = &runrec.Store{Dir: filepath.Join(d.cfg.Paths.Dir, "runs")}
	}
	if deps.Vendors == nil {
		// One value for every run of the daemon, so the cap holds across runs (S169).
		deps.Vendors = engine.NewVendors(defaultVendorCaps)
		if d.cfg.Clock != nil {
			deps.Vendors.WithClock(d.cfg.Clock.Now)
		}
		// The owner's marks outlive the daemon (S299, *proposed*). A store that cannot be
		// read leaves the daemon with no marks, and says so.
		if d.cfg.Paths.Dir != "" {
			if _, err := deps.Vendors.WithStore(filepath.Join(d.cfg.Paths.Dir, vendorMarksFile), supportsVendor); err != nil {
				fmt.Fprintf(os.Stderr, "vendor marks: %v\n", err)
			}
		}
	}
	if deps.Hold == nil {
		// One hold for every run and spec check of the daemon (S431): a drain for
		// a rebuild sets it, and a role start waits while it is set.
		deps.Hold = engine.NewHold(d.rebuildKick)
	}
	if deps.Wake == nil {
		// One detector for every run of the daemon (S235); recoverRuns starts it.
		now := time.Now
		if d.cfg.Clock != nil {
			now = d.cfg.Clock.Now
		}
		deps.Wake = engine.NewWake(now)
	}
	d.runs = runManager{recordLocks: map[string]*sync.Mutex{}, cleaning: map[string]bool{}, heads: map[string]string{}, merged: map[string]*bool{}, mergeLogged: map[string]string{}, ctx: context.Background(), deps: deps, runs: map[string]*managedRun{}, waveStops: map[string]int{}, specs: map[string]specEntry{}, specCancels: map[int]context.CancelFunc{}}
	d.runs.notifications = newNotifier(d)
	deps.Store.OnChange = func(run *runrec.Run, before, after runrec.Record) {
		d.runs.notifications.changed(run, before, after)
		if runNoticeDue(before, after) {
			d.changedRuns(after.Project, source.Runs, []string{after.ID})
		}
		// A landing can leave the daemon behind main (S431, *proposed*; adj 464).
		if before.LandedAt.IsZero() && !after.LandedAt.IsZero() {
			go d.checkRebuild()
		}
	}
}

// runNoticeDue reports whether a record write is one a waiting ork wait must
// learn of: the state, the pending kind or key, or the number of role rounds
// with an end differs (S403, *proposed*; adj 451, cli.wait-signal). A write of
// tokens or updated_at alone is not.
func runNoticeDue(before, after runrec.Record) bool {
	if before.State != after.State {
		return true
	}
	var bk, ak, bkey, akey string
	if before.Pending != nil {
		bk, bkey = before.Pending.Kind, before.Pending.Key
	}
	if after.Pending != nil {
		ak, akey = after.Pending.Kind, after.Pending.Key
	}
	if bk != ak || bkey != akey {
		return true
	}
	return endedRoles(before) != endedRoles(after)
}

// endedRoles counts the role rounds of a record that have an end.
func endedRoles(rec runrec.Record) int {
	n := 0
	for _, role := range rec.Roles {
		if !role.EndedAt.IsZero() {
			n++
		}
	}
	return n
}

// watchLocalSlots admits queued runs when a local role/spec check frees a
// slot, even when no managed run job ends. closeRuns cancels and joins it.
// The caller holds the manager lock. It starts lazily for local runs only.
func (d *Daemon) watchLocalSlots() {
	if d.runs.localWakeCancel != nil || d.runs.closed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.runs.localWakeCancel = cancel
	changes := d.runs.deps.Vendors.SlotChanges()
	d.runs.wg.Add(1)
	go func() {
		defer d.runs.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-changes:
				d.runs.mu.Lock()
				if !d.runs.closed && len(d.runs.runs) > 0 {
					if f, err := d.loadRunRegistryLocked(); err == nil {
						d.startQueuedLocked(f)
					}
				}
				d.runs.mu.Unlock()
			}
		}
	}()
}

// toolProject resolves the registered project that a tool feedback entry's tool
// slug names (S387, *proposed*; adj 412). The registry is read when an entry is
// filed; a project is matched by its name's slug, so case does not matter. An
// unreadable registry resolves nothing, and the entry is filed in the run's own
// project.
func (d *Daemon) toolProject(slug string) (registry.Project, bool) {
	f, err := d.loadRegistry()
	if err != nil {
		return registry.Project{}, false
	}
	for _, p := range f.Projects {
		if engine.ToolFeedbackSlug(p.Name) == slug {
			return p, true
		}
	}
	return registry.Project{}, false
}

func (d *Daemon) runDeps(p registry.Project) engine.Deps {
	deps := d.runs.deps
	if deps.HungAfter == 0 {
		deps.HungAfter = engine.DefaultHungAfter // a role silent for 30 minutes is killed (S182); not a setting
	}
	if deps.Project == nil {
		deps.Project = d.toolProject
	}
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
			claude:      claudeRunner{fixtureDir: filepath.Join(d.cfg.Paths.Dir, "fixtures", "claude"), env: envFor("claude"), approval: d.prepareApproval},
			codex:       codexRunner{fixtureDir: filepath.Join(d.cfg.Paths.Dir, "fixtures", "codex"), env: envFor("codex")},
			grok:        grokRunner{fixtureDir: filepath.Join(d.cfg.Paths.Dir, "fixtures", "grok"), env: envFor("grok")},
			claudeLocal: claudeRunner{fixtureDir: filepath.Join(d.cfg.Paths.Dir, "fixtures", "claude-local"), approval: d.prepareApproval},
			codexLocal:  codexRunner{fixtureDir: filepath.Join(d.cfg.Paths.Dir, "fixtures", "codex-local")},
			grokLocal:   grokRunner{fixtureDir: filepath.Join(d.cfg.Paths.Dir, "fixtures", "grok-local")},
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
	d.runs.deps.Wake.Start(ctx) // samples until the daemon's context ends
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
	var rebuildResumes []string
	for _, r := range stored {
		rec := r.Snapshot()
		if rec.RebuildResume {
			// Consume before retrying, including failed recovery or a refused
			// retry. A later daemon must not repeat this automatic attempt.
			if err := r.Update(func(rec *runrec.Record) { rec.RebuildResume = false }); err != nil {
				fmt.Fprintf(os.Stderr, "clear rebuild resume %s: %v\n", rec.ID, err)
			} else {
				rebuildResumes = append(rebuildResumes, rec.ID)
			}
		}
		if runrec.IsFinal(rec.State) {
			if !rec.LandedAt.IsZero() {
				d.runs.specStateMu.Lock()
				d.runs.mu.Lock()
				d.removeSpecDirLocked(rec.Project, rec.Item)
				d.runs.mu.Unlock()
				d.runs.specStateMu.Unlock()
			}
			// A stopped sliver keeps its chain's branch for a retry; its later
			// slivers must stay queued after a restart too. A committed sliver
			// that is not the last is loaded so its chain keeps its one cap slot
			// while the next sliver waits (S62). Neither is reconciled or started.
			chainRest := rec.State == runrec.StateStopped || (rec.State == runrec.StateCommitted && rec.SliverIndex < rec.SliverCount)
			if rec.Chain != "" && chainRest && rec.DismissedAt.IsZero() {
				if p, ok := find(f, rec.Project); ok {
					ep := d.runProject(ctx, p)
					d.runs.mu.Lock()
					d.runs.runs[rec.ID] = &managedRun{engine: engine.Load(d.runDeps(ep), ep, r)}
					d.runs.mu.Unlock()
				}
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
		// A run loaded again uses the same check command rule as a new one (S246).
		ep := d.runProject(ctx, p)
		e := engine.Load(d.runDeps(ep), ep, r)
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
		// Restore local reservations for nonterminal runs, including owner
		// waits. A run that cannot reserve waits on its next active job.
		e.TryReserveLocal()
		d.runs.mu.Lock()
		d.runs.runs[rec.ID] = &managedRun{engine: e}
		d.runs.mu.Unlock()
	}
	// All recovered runs are published before retry uses the ordinary owner
	// action path, including its cap and durable waiting-resume handling.
	for _, id := range rebuildResumes {
		if _, err := d.ownerRunReply(ctx, MethodRunRetry, RunAction{Run: id, NoWait: true}); err != nil {
			fmt.Fprintf(os.Stderr, "rebuild resume %s: %v\n", id, err)
		}
	}
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	d.startQueuedLocked(f)
	return nil
}
func (d *Daemon) closeRuns() {
	d.runs.mu.Lock()
	d.runs.closed = true
	if d.runs.localWakeCancel != nil {
		d.runs.localWakeCancel()
	}
	for _, r := range d.runs.runs {
		if r.cancel != nil {
			r.cancel()
		}
	}
	for _, cancel := range d.runs.specCancels {
		cancel()
	}
	d.runs.mu.Unlock()
	d.runs.notifications.close()
	d.runs.wg.Wait()
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	for _, r := range d.runs.runs {
		if r.engine != nil {
			r.engine.ReleaseLocal()
		}
	}
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
var matrixAdapters = map[string]string{"claude code": "claude", "codex": "codex", "grok": "grok", "claude-local": "claude-local", "codex-local": "codex-local", "grok-local": "grok-local"}

// launcherMissing checks only local launchers. Cloud binaries retain their
// existing adapter launch and fallback behavior.
func launcherMissing(vendor string) string {
	if engine.IsLocalVendor(vendor) {
		if _, err := exec.LookPath(vendor); err != nil {
			return "skipped: launcher " + vendor + " is not on PATH"
		}
	}
	return ""
}

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
	if p.StopAfter == "" {
		p.StopAfter = runrec.StopAfterLand
	}
	if p.StopAfter != runrec.StopAfterLand && p.StopAfter != runrec.StopAfterCommit {
		return nil, badParams(errors.New("stop_after must be land or commit"))
	}
	if p.Project == "" || p.Item == "" {
		return nil, badParams(errors.New("run needs project and item"))
	}
	d.runs.mu.Lock()
	f, err := d.loadRunRegistryLocked()
	d.runs.mu.Unlock()
	if err != nil {
		return nil, err
	}
	registered, ok := find(f, p.Project)
	if !ok {
		return nil, notFound(p.Project, f)
	}
	// proj is the project the run uses: the registry entry with the check
	// command slicer names, when it names one (S246). registered stays the
	// entry the plan-changed checks compare.
	proj, checkSource, checkErr := d.effectiveProject(ctx, registered)
	if p.Slivers {
		if p.Split {
			return nil, badParams(errors.New("--slivers cannot be combined with --split y"))
		}
		return d.planSliverChain(ctx, p, proj)
	}
	// A split run (S61, *proposed*) asks the matrix twice: split no for the lead
	// (the implementer's slot, the reviewer, the gate and the architect, which the
	// matrix gives to size L only) and split yes for the parts, whose rows only the
	// part chain takes.
	rec, err := d.runs.deps.Slicer.RecommendRoles(ctx, proj.Path, p.Item, p.WellSpecified, false)
	if err != nil {
		return nil, err
	}
	var partRec *slicercli.Recommendation
	if p.Split {
		if partRec, err = d.runs.deps.Slicer.RecommendRoles(ctx, proj.Path, p.Item, p.WellSpecified, true); err != nil {
			return nil, err
		}
	}
	rules, rulesErr := d.runs.deps.Slicer.RelayRules(ctx, proj.Path)
	if rulesErr != nil {
		rules = nil
	}
	// UI rows are selected in project or rule order. All UI rows, including
	// classified trial rows, stay out of reviewer and gate fallbacks.
	uiIDs := uiPassRows(proj, rules)
	uiSet := uiPassRowSet(proj, rules)
	// The spec check row is a reviewer row too (rule u11). It runs only the
	// spec check, so a run's chains never hold it. The project's
	// spec_check_rows are spec check rows too (S258).
	specRows := specCheckRowSet(proj, rules)
	var uiRows []slicercli.Row
	// The chains are built here, each entry with whether its row is local
	// (vendor column Local), and become plan.Lineup once their models are
	// resolved (S244).
	chains := map[string][]chainEntry{}
	plan := &RunPlan{Split: p.Split, StopAfter: p.StopAfter, project: registered, effective: proj, Project: registry.Project{Name: proj.Name, Path: proj.Path}, Item: p.Item, CheckCommand: proj.CheckCommand, CheckSource: checkSource, RoundCheckCommand: proj.RoundCheckCommand, Lineup: map[string][]runrec.LineupEntry{}, Permissions: map[string]engine.Permission{}}
	if checkErr != nil {
		plan.Note = checkErr.Error()
	}
	if rulesErr != nil {
		if plan.Note != "" {
			plan.Note += "; "
		}
		plan.Note += "slicer relay-roles show failed; planning without UI or spec-check rules: " + rulesErr.Error()
	}
	for _, role := range []string{engine.RoleImplementer, engine.RoleReviewer, engine.RoleGate, engine.RoleArchitect} {
		sourceRole := role
		if role == engine.RoleGate {
			sourceRole = engine.RoleReviewer
		}
		recommended := false
		// An error from the permission waits for the check below.
		perm, permErr := engine.ResolvePermission(proj, role)
		var skipped []string
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
				if sourceRole == engine.RoleReviewer && slices.Contains(specRows, row.ID) {
					continue
				}
				if sourceRole == engine.RoleReviewer && slices.Contains(uiSet, row.ID) {
					if role == engine.RoleReviewer {
						uiRows = append(uiRows, row)
					}
					continue
				}
				if vendor := matrixAdapters[strings.ToLower(row.Agent)]; vendor != "" {
					entry := runrec.LineupEntry{Vendor: vendor, Model: row.Model, Effort: row.Reasoning}
					chains[role] = appendChain(chains[role], chainEntry{entry, isLocalRow(row)})
				}
			}
		}
		if role == engine.RoleArchitect && !recommended {
			continue
		}
		if len(chains[role]) == 0 {
			if len(skipped) > 0 {
				return nil, fmt.Errorf("lineup has no row that can run as %s: %s", sourceRole, strings.Join(skipped, "; "))
			}
			return nil, fmt.Errorf("lineup has no row with an adapter for %s", sourceRole)
		}
		if permErr != nil {
			return nil, permErr
		}
		if len(skipped) > 0 {
			if plan.Note != "" {
				plan.Note += "; "
			}
			plan.Note += strings.Join(skipped, "; ")
		}
		plan.Permissions[role] = perm
	}
	if p.Split {
		// The parts' chain is the implementer rows the matrix gives a part of a
		// split slice (split yes). A part has the implementer's permission, less
		// the slicer commands that write.
		for _, choice := range partRec.Roles {
			if choice.Role != engine.RoleImplementer {
				continue
			}
			rows := choice.Alternatives
			if choice.Row != nil {
				rows = append([]slicercli.Row{*choice.Row}, rows...)
			}
			for _, row := range rows {
				if vendor := matrixAdapters[strings.ToLower(row.Agent)]; vendor != "" {
					entry := runrec.LineupEntry{Vendor: vendor, Model: row.Model, Effort: row.Reasoning}
					chains[engine.RolePart] = appendChain(chains[engine.RolePart], chainEntry{entry, isLocalRow(row)})
				}
			}
		}
		if len(chains[engine.RolePart]) == 0 {
			return nil, errors.New("lineup has no row with an adapter for a part")
		}
		perm, err := engine.ResolvePermission(proj, engine.RolePart)
		if err != nil {
			return nil, err
		}
		plan.Permissions[engine.RolePart] = perm
	}
	var uiChain []chainEntry
	for _, id := range uiIDs {
		for _, row := range uiRows {
			if row.ID != id {
				continue
			}
			if vendor := matrixAdapters[strings.ToLower(row.Agent)]; vendor != "" {
				entry := runrec.LineupEntry{Vendor: vendor, Model: row.Model, Effort: row.Reasoning}
				uiChain = appendChain(uiChain, chainEntry{entry, isLocalRow(row)})
			}
		}
	}
	if len(uiChain) > 0 {
		perm, err := engine.ResolvePermission(proj, engine.RoleUI)
		if err != nil {
			return nil, err
		}
		chains[engine.RoleUI] = uiChain
		plan.Permissions[engine.RoleUI] = perm
	}
	for role, entries := range p.Lineup {
		if role != engine.RoleImplementer && role != engine.RoleReviewer && role != engine.RoleArchitect && !(role == engine.RolePart && p.Split) {
			return nil, badParams(fmt.Errorf("cannot override lineup role %q", role))
		}
		if role == engine.RoleArchitect && len(entries) == 0 {
			delete(chains, role)
			delete(plan.Permissions, role)
			continue
		}
		if len(entries) == 0 {
			return nil, badParams(fmt.Errorf("lineup for %s needs at least one entry", role))
		}
		// The wire may send a vendor in any case; it is lowercase from here on.
		entries = slices.Clone(entries)
		for i := range entries {
			typed := entries[i].Vendor
			entries[i].Vendor = strings.ToLower(strings.TrimSpace(typed))
			entry := entries[i]
			if !supportsVendor(entry.Vendor) {
				return nil, badParams(fmt.Errorf("unsupported vendor %q for %s; use %s", typed, role, vendorList()))
			}
			if strings.TrimSpace(entry.Model) == "" || strings.TrimSpace(entry.Effort) == "" {
				return nil, badParams(fmt.Errorf("lineup for %s needs a model and effort", role))
			}
		}
		// An override replaces the role's chain, and with it every local mark.
		replaced := make([]chainEntry, len(entries))
		for i, entry := range entries {
			replaced[i] = chainEntry{LineupEntry: entry}
		}
		chains[role] = replaced
		if role == engine.RoleArchitect {
			perm, err := engine.ResolvePermission(proj, role)
			if err != nil {
				return nil, err
			}
			plan.Permissions[role] = perm
		}
	}
	for _, role := range slices.Sorted(maps.Keys(chains)) {
		var available []chainEntry
		for _, entry := range chains[role] {
			if reason := launcherMissing(entry.Vendor); reason != "" {
				if plan.Note != "" {
					plan.Note += "; "
				}
				plan.Note += reason
				continue
			}
			available = append(available, entry)
		}
		if len(available) == 0 {
			return nil, fmt.Errorf("lineup for %s has no available launcher: %s", role, plan.Note)
		}
		chains[role] = available
	}
	// Models are resolved last, after the overrides replaced their roles' rows
	// and the missing launchers dropped theirs, so a family that cannot be
	// resolved in a row the owner overrides does not refuse the plan. The gate
	// is the reviewer's chain, copied below.
	for _, role := range slices.Sorted(maps.Keys(chains)) {
		if role == engine.RoleGate {
			continue
		}
		resolved, err := d.resolveEntries(ctx, chains[role])
		if err != nil {
			return nil, fmt.Errorf("lineup for %s: %w", role, err)
		}
		plan.Lineup[role] = resolved
	}
	plan.Lineup[engine.RoleGate] = append([]runrec.LineupEntry(nil), plan.Lineup[engine.RoleReviewer]...)
	if note := d.vendorNote(plan.Lineup); note != "" {
		if plan.Note != "" {
			plan.Note += "; "
		}
		plan.Note += note
	}
	plan.Warnings = engine.SetupWarnings(proj, func(name string) bool {
		_, err := os.Stat(filepath.Join(proj.Path, name))
		return err == nil
	})
	// S295 (*proposed*): a check command that cannot run in the project's
	// folder shows in the plan, before the start. The worktree does not exist
	// yet, so the main checkout's folder stands in for the folder the check
	// runs in (the same folder, S293); setup verifies again in the worktree.
	if info, err := os.Stat(proj.Path); err == nil && info.IsDir() && !proj.NoVerifyCheck {
		if problems, flag := engine.CheckCommandProblems(ctx, proj, proj.Path, nil); len(problems) > 0 {
			// A command that came from slicer is fixed in slicer's config: ork
			// projects set --check writes the registry, which slicer's value
			// overrides (S246, adj 367). The round check stays in the registry.
			fix := fmt.Sprintf("ork projects set %s %s CMD", engine.ShellWord(proj.Name), flag)
			if checkSource == "slicer" && flag == "--check" {
				fix = fmt.Sprintf("slicer config check_command CMD --root %s", engine.ShellWord(proj.Path))
			}
			plan.Warnings = append(plan.Warnings, fmt.Sprintf(engine.CheckCannotRunPrefix+"%s: %s (fix: %s)",
				proj.Path, strings.Join(problems, "; "), fix))
		}
	}
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
	if rec.Sliver {
		detail, showErr := d.runs.deps.Slicer.Show(ctx, proj.Path, p.Item)
		if showErr != nil {
			return nil, showErr
		}
		if detail.Parent == "" {
			return nil, fmt.Errorf("%s is a sliver with no parent", p.Item)
		}
		plan.Parent = detail.Parent
	}
	// A local implementer takes a slice above size S as slivers (S359, *proposed*).
	if !rec.Sliver && !p.Split && !p.chained && engine.IsLocalRun(plan.Lineup) {
		chain, err := d.localSliverGate(ctx, p, proj, plan)
		if err != nil {
			return nil, err
		}
		if chain != nil {
			return chain, nil
		}
	}
	treeID := p.Item
	if plan.Parent != "" {
		treeID = plan.Parent
	}
	plan.Worktree, plan.Branch = filledTree(proj, treeID)
	return plan, nil
}

// localSliverGate decides what a local implementer may take of a slice that is
// not a sliver (S359, *proposed*; adj 338, owner decision 2026-11-16, rule u6):
//   - an effort 3 slice is refused, whatever its size: rule u6 never slivers it;
//   - a slice above size S that already has slivers is planned as the chain of
//     those slivers, which is returned;
//   - a slice above size S with none is marked in the plan to be carved
//     (plan.Carve), and nil is returned: the spec check carves and files the
//     slivers, and the plan is made again to show them for the owner's one
//     approval (owner decision 2026-11-16, adj 396). A start of a plan still
//     marked is refused;
//   - any other slice goes whole, and nil is returned.
//
// A slice with no size or no effort is not above size S and not effort 3.
func (d *Daemon) localSliverGate(ctx context.Context, p RunParams, proj registry.Project, plan *RunPlan) (*RunPlan, error) {
	detail, err := d.runs.deps.Slicer.Show(ctx, proj.Path, p.Item)
	if err != nil {
		return nil, err
	}
	if detail.Fields.Effort >= 3 {
		return nil, &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf("%s is an effort 3 slice. Rule u6 never slivers one, and a local implementer takes only slivers or a slice of size S, so ork does not run it on a local implementer. Pick a cloud implementer for it.", p.Item)}
	}
	size := detail.Fields.Size
	if size == "" && detail.Slice != nil {
		size = detail.Slice.Size
	}
	if size != "M" && size != "L" {
		return nil, nil
	}
	kids, err := d.runs.deps.Slicer.ListChildren(ctx, proj.Path, p.Item)
	if err != nil {
		return nil, err
	}
	if len(kids) > 0 {
		// A carve whose filing failed part way saved its report: when it holds
		// more slivers than are filed, the start files the rest from it, so the
		// chain does not land the parent with part of its work.
		saved := d.savedCarve(proj.Name, p.Item)
		if len(saved) <= len(kids) {
			p.Slivers = true
			return d.planSliverChain(ctx, p, proj)
		}
		plan.Carve, plan.carved = true, saved
		if plan.Note != "" {
			plan.Note += "; "
		}
		plan.Note += fmt.Sprintf("%s has %d of the %d slivers of its saved carve filed: the spec check files the rest from that report, with no new carve, and the plan then lists them as one chain", p.Item, len(kids), len(saved))
		return nil, nil
	}
	plan.Carve = true
	if plan.Note != "" {
		plan.Note += "; "
	}
	plan.Note += fmt.Sprintf("%s is size %s and the implementer is local: the spec check row carves slivers (size S, effort 1) and ork files them under %s with no confirmation, and the plan then lists them as one chain for your approval", p.Item, size, p.Item)
	return nil, nil
}

// filledTree is the project's worktree and branch patterns with id filled.
// A relative worktree is joined to the project directory.
func filledTree(proj registry.Project, id string) (string, string) {
	pattern := proj.WorktreePattern
	if pattern == "" {
		pattern = ".worktrees/{id}"
	}
	worktree := registry.FillPattern(pattern, id)
	if !filepath.IsAbs(worktree) {
		worktree = filepath.Join(proj.Path, worktree)
	}
	pattern = proj.BranchPattern
	if pattern == "" {
		pattern = "{id}"
	}
	return filepath.Clean(worktree), registry.FillPattern(pattern, id)
}

// planSliverChain plans every sliver of p.Item, in slicer's order, as one
// plan. The plan's item is the parent. Each sliver keeps its own lineup. The
// worktree and branch are the parent's (record 124).
func (d *Daemon) planSliverChain(ctx context.Context, p RunParams, proj registry.Project) (*RunPlan, error) {
	kids, err := d.runs.deps.Slicer.ListChildren(ctx, proj.Path, p.Item)
	if err != nil {
		return nil, err
	}
	if len(kids) == 0 {
		return nil, fmt.Errorf("%s has no slivers", p.Item)
	}
	var slivers []SliverPlan
	var first *RunPlan
	for _, kid := range kids {
		child := p
		child.Item = kid.ID
		child.Slivers = false
		child.chained = true
		child.Split = false
		child.Expected = nil
		cp, err := d.planRun(ctx, child)
		if err != nil {
			return nil, err
		}
		if cp.Parent == "" {
			cp.Parent = p.Item
		}
		if cp.Parent != p.Item {
			return nil, fmt.Errorf("sliver %s belongs to %s, not %s", kid.ID, cp.Parent, p.Item)
		}
		slivers = append(slivers, SliverPlan{
			Item: kid.ID, Parent: p.Item, Index: len(slivers) + 1, Count: len(kids), Lineup: cp.Lineup,
		})
		if first == nil {
			first = cp
		}
	}
	worktree, branch := filledTree(proj, p.Item)
	first.Item = p.Item
	first.Parent = p.Item
	first.Worktree = worktree
	first.Branch = branch
	first.Slivers = slivers
	first.Lineup = slivers[0].Lineup
	return first, nil
}

// effectiveProject is p with the check command a run of it uses (S246; adj 32,
// project.check-command, slicer-then-registry): slicer's `check_command` when
// it is set, else the registry's. The source is "slicer", "registry", or
// "none" when both are empty. A failed read keeps the registry value and
// returns the failure for the caller to show. It never writes the registry.
func (d *Daemon) effectiveProject(ctx context.Context, p registry.Project) (registry.Project, string, error) {
	source := "registry"
	if p.CheckCommand == "" {
		source = "none"
	}
	value, err := d.runs.deps.Slicer.Config(ctx, p.Path, "check_command")
	if err != nil {
		return p, source, fmt.Errorf("slicer config check_command failed: %w", err)
	}
	if value = strings.TrimSpace(value); value != "" {
		p.CheckCommand = value
		source = "slicer"
	}
	return p, source, nil
}

// runProject is effectiveProject for a run that is loaded again, where there
// is no plan to show a failed read in: it is logged, and the registry's
// command stays.
func (d *Daemon) runProject(ctx context.Context, p registry.Project) registry.Project {
	eff, _, err := d.effectiveProject(ctx, p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "project %s: %v; using the registry's check command\n", p.Name, err)
	}
	return eff
}

// NoCheckRefusalMessage is why a start on a project with no check command is
// refused, and how to fix it (adj 191, launch.no-check-command). The command
// line and the daemon say it the same way.
func NoCheckRefusalMessage(project string) string {
	return "project " + project + ": " + engine.NoCheckMessage(project)
}

func noCheckRefusal(project string) error {
	return &ipc.Error{Code: CodeRefused, Message: NoCheckRefusalMessage(project)}
}

func (d *Daemon) startRun(ctx context.Context, p RunParams) (runrec.Record, error) {
	reply, err := d.startRunReply(ctx, p)
	return reply.Record, err
}

// startRunReply starts the slice, or resumes the stopped run of the item when
// its worktree is unchanged (S296, *proposed*; owner decision 2026-11-07). The
// message says which, and why a stopped run was not resumed.
func (d *Daemon) startRunReply(ctx context.Context, p RunParams) (RunReply, error) {
	plan, err := d.planRun(ctx, p)
	if err != nil {
		return RunReply{}, err
	}
	if !p.AllowNoCheck && plan.effective.CheckCommand == "" {
		return RunReply{}, noCheckRefusal(plan.project.Name)
	}
	if err := d.restoreRunSpecAnswers(ctx, plan.project, p.Item); err != nil {
		return RunReply{}, err
	}
	if p.Expected != nil {
		shown, _ := json.Marshal(p.Expected)
		current, _ := json.Marshal(plan)
		if string(shown) != string(current) {
			return RunReply{}, errors.New("plan changed; run the command again to confirm it")
		}
	}
	// A local implementer on a slice above size S runs its slivers, which the
	// spec check carves and files (S359, *proposed*; owner decision 2026-11-16,
	// adj 396). A start never carves: with no slivers filed yet, the owner has
	// not seen the chain, so the start refuses instead of running the slice whole.
	if plan.Carve {
		return RunReply{}, &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf("%s has no slivers filed yet, and a local implementer runs a slice above size S only as slivers. The spec check carves them: run it first, then confirm the chain it plans, or pick a cloud implementer.", p.Item)}
	}
	// The tree is fingerprinted here, outside the locks; admitRun re-checks
	// under the lock that the run it probed is unchanged.
	probe := d.probeResume(ctx, plan.effective, p.Item, plan.Worktree)
	rec, message, resumed, err := d.admitRun(ctx, plan, p, probe)
	if err != nil {
		return RunReply{}, err
	}
	if resumed != nil {
		// The reply waits until the engine applied the resume, so it never
		// shows the run as stopped, and never says resumed when the engine
		// refused the resume after all.
		select {
		case <-ctx.Done():
			return RunReply{}, ctx.Err()
		case <-resumed.applied:
		case <-resumed.done:
		}
		select {
		case <-resumed.applied:
		default:
			err := resumed.result.err
			if err == nil {
				err = fmt.Errorf("stopped run %s was not resumed", resumed.m.engine.Snapshot().ID)
			}
			if engine.IsRefusal(err) {
				err = &ipc.Error{Code: CodeRefused, Message: err.Error()}
			}
			return RunReply{}, err
		}
		rec = resumed.m.engine.Snapshot()
	}
	return RunReply{Record: rec, Message: message}, nil
}

// resumeLaunch is a stopped run an admission resumed: its job started, and
// applied closes once the engine has applied the resume.
type resumeLaunch struct {
	m       *managedRun
	result  *runResult // the resume job's outcome, read once done is closed
	applied chan struct{}
	done    chan struct{}
}

// admitRun admits the planned run under the manager lock: it resumes the
// item's stopped run, or creates and starts a new run.
func (d *Daemon) admitRun(ctx context.Context, plan *RunPlan, p RunParams, probe *resumeProbe) (runrec.Record, string, *resumeLaunch, error) {
	d.runs.specStateMu.Lock()
	defer d.runs.specStateMu.Unlock()
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	if d.runs.closed {
		return runrec.Record{}, "", nil, errors.New("run manager is closed")
	}
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		return runrec.Record{}, "", nil, err
	}
	// A second ordinary run on a worktree is refused. A sliver, alone or in a
	// chain, queues behind the run that owns the parent's worktree (S62).
	behind := false
	for _, r := range stored {
		rec := r.Snapshot()
		if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil {
			rec = m.engine.Snapshot()
		}
		if runrec.IsFinal(rec.State) || rec.Worktree != plan.Worktree {
			continue
		}
		if plan.Parent == "" && len(plan.Slivers) == 0 {
			return runrec.Record{}, "", nil, fmt.Errorf("run %s already owns %s", rec.ID, rec.Worktree)
		}
		behind = true
		break
	}
	// Admission is here and only here: owner actions never reach startRun, so
	// approve and stop bypass the caps (adj 122). An answer or retry of a
	// paused run waits for a slot instead (ownerRunReply, adj 177).
	f, err := d.loadRunRegistryLocked()
	if err != nil {
		return runrec.Record{}, "", nil, err
	}
	current, ok := find(f, plan.project.Name)
	if !ok || current.Path != plan.project.Path {
		return runrec.Record{}, "", nil, notFound(plan.project.Name, f)
	}
	// projects.set (S161) holds this lock to save. A set between planRun's
	// read and here, including one of the fields it may change while runs are
	// unfinished (the cap, the budgets, the patterns; S265), would leave the plan
	// on the old fields, so refuse.
	if !reflect.DeepEqual(current, plan.project) {
		return runrec.Record{}, "", nil, errors.New("plan changed; run the command again to confirm it")
	}
	// An item whose newest run is a stopped one with work in its worktree
	// resumes that run when the tree is unchanged, and otherwise starts fresh
	// and says so (S296 Q1, option b).
	wave := p.wave
	if wave == "" {
		wave = newWaveID(time.Now())
	}
	if len(plan.Slivers) > 0 {
		return d.admitChainLocked(ctx, plan, p, wave, behind)
	}
	// current equals plan.project here, so the plan's effective project is
	// current with the check command the run uses (S246).
	message, resumed, err := d.autoResumeLocked(ctx, f, plan.effective, p.Item, plan.Worktree, wave, p.waveName, stored, probe)
	if err != nil {
		return runrec.Record{}, "", nil, err
	}
	if resumed != nil {
		return resumed.m.engine.Snapshot(), message, resumed, nil
	}
	local := engine.IsLocalRun(plan.Lineup)
	if local {
		d.watchLocalSlots()
	}
	// Local admission claims its shared slot after New has saved a queued
	// record. This avoids a check/claim race with spec checks and role slots.
	// A sliver whose parent's worktree is already owned waits, even when a
	// cap slot is free (S62).
	queued := local || !d.hasRoomLocked(f, plan.project.Name) || behind
	// A spec check that the owner answered gave the item decisions that its
	// worktree's spec lacks; the run carries them into every brief (S116).
	cfg := engine.Config{StopAfter: plan.StopAfter, Project: plan.effective, Item: p.Item, Lineup: plan.Lineup, Split: plan.Split, Budgets: plan.Budgets, Wave: wave, WaveName: p.waveName, Queued: queued, Decisions: d.runs.specs[specKey(plan.project.Name, p.Item)].decisions, Worktree: plan.Worktree, Branch: plan.Branch, Parent: plan.Parent}
	var orientation []byte
	if local {
		orientation, err = os.ReadFile(filepath.Join(d.specDir(plan.project.Name, p.Item), "orientation.md"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return runrec.Record{}, "", nil, fmt.Errorf("read orientation: %w", err)
		}
	}
	e, err := engine.New(ctx, d.runDeps(plan.effective), cfg)
	if err != nil {
		return runrec.Record{}, "", nil, err
	}
	if len(orientation) > 0 {
		if err := os.WriteFile(filepath.Join(d.runs.deps.Store.RunDir(e.Snapshot().ID), "orientation.md"), orientation, 0600); err != nil {
			return runrec.Record{}, "", nil, fmt.Errorf("copy orientation: %w", err)
		}
	}
	// The run record now owns the paragraph, including for queued runs.
	if err := os.Remove(filepath.Join(d.specDir(plan.project.Name, p.Item), "answers.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "remove transferred spec answers: %v\n", err)
	}
	delete(d.runs.specs, specKey(plan.project.Name, p.Item))
	r := &managedRun{engine: e}
	if local && e.TryReserveLocal() {
		if err := e.Dequeue(); err != nil {
			e.ReleaseLocal()
			return runrec.Record{}, "", nil, err
		}
		queued = false
	}
	rec := e.Snapshot()
	d.runs.runs[rec.ID] = r
	if !queued {
		d.launchRun(r, e.Run)
	}
	return rec, message, nil, nil
}

// admitChainLocked starts one run per sliver in the parent's worktree. The
// first takes a cap slot when one is free and the worktree is free. The rest
// wait, and the chain keeps that one slot until the last sliver ends. The
// caller holds the manager lock.
func (d *Daemon) admitChainLocked(ctx context.Context, plan *RunPlan, p RunParams, wave string, behind bool) (runrec.Record, string, *resumeLaunch, error) {
	chain := newChainID(time.Now())
	f, err := d.loadRunRegistryLocked()
	if err != nil {
		return runrec.Record{}, "", nil, err
	}
	room := d.hasRoomLocked(f, plan.project.Name)
	var first runrec.Record
	for i, sliver := range plan.Slivers {
		local := engine.IsLocalRun(sliver.Lineup)
		if local {
			d.watchLocalSlots()
		}
		queued := behind || i > 0 || local || !room
		var orientation []byte
		if local {
			orientation, err = os.ReadFile(filepath.Join(d.specDir(plan.project.Name, sliver.Item), "orientation.md"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return runrec.Record{}, "", nil, fmt.Errorf("read orientation: %w", err)
			}
		}
		cfg := engine.Config{
			StopAfter: plan.StopAfter, Project: plan.effective, Item: sliver.Item, Lineup: sliver.Lineup,
			Budgets: plan.Budgets, Wave: wave, WaveName: p.waveName, Queued: queued,
			Decisions: d.runs.specs[specKey(plan.project.Name, sliver.Item)].decisions,
			Worktree:  plan.Worktree, Branch: plan.Branch, Parent: plan.Parent,
			Chain: chain, SliverIndex: sliver.Index, SliverCount: sliver.Count,
		}
		e, err := engine.New(ctx, d.runDeps(plan.effective), cfg)
		if err != nil {
			return runrec.Record{}, "", nil, err
		}
		if len(orientation) > 0 {
			if err := os.WriteFile(filepath.Join(d.runs.deps.Store.RunDir(e.Snapshot().ID), "orientation.md"), orientation, 0600); err != nil {
				return runrec.Record{}, "", nil, fmt.Errorf("copy orientation: %w", err)
			}
		}
		if i == 0 && !behind && local && e.TryReserveLocal() {
			if err := e.Dequeue(); err != nil {
				e.ReleaseLocal()
				return runrec.Record{}, "", nil, err
			}
			queued = false
		}
		rec := e.Snapshot()
		d.runs.runs[rec.ID] = &managedRun{engine: e}
		if !queued {
			d.launchRun(d.runs.runs[rec.ID], e.Run)
		}
		if i == 0 {
			first = e.Snapshot()
		}
	}
	for _, id := range append([]string{p.Item}, sliverItems(plan.Slivers)...) {
		if err := os.Remove(filepath.Join(d.specDir(plan.project.Name, id), "answers.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "remove transferred spec answers: %v\n", err)
		}
		delete(d.runs.specs, specKey(plan.project.Name, id))
	}
	return first, fmt.Sprintf("sliver chain %s: %d slivers, one cap slot", chain, len(plan.Slivers)), nil, nil
}

func sliverItems(slivers []SliverPlan) []string {
	ids := make([]string, len(slivers))
	for i, sliver := range slivers {
		ids[i] = sliver.Item
	}
	return ids
}

// newChainID links the run records of one sliver chain.
func newChainID(now time.Time) string {
	b := make([]byte, 2)
	rand.Read(b)
	return "chain-" + now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// resumeProbe is what a start learned about the item's stopped run before it
// took the manager lock: the run it looked at and that run's UpdatedAt, and
// whether the resume would be accepted and whether the worktree is still what
// the last implementer round left. Fingerprinting the tree runs git, which
// the start does not do under the lock (S296, *proposed*).
type resumeProbe struct {
	id        string
	updatedAt time.Time
	canErr    error // CanResume's refusal, or a record that could not be opened
	round     int   // the last implementer round
	match     bool  // the worktree equals that round's tree_sha256
	cmpErr    error // why the tree cannot be compared
}

// resumeCandidateLocked is the item's newest run when a start may consider
// resuming it (S296, *proposed*; owner decision 2026-11-07, Q1 option b).
// Only the newest run counts, and only when it is stopped, not dismissed,
// resumable from its record, idle, and holds the planned worktree on disk. A
// dismissed run is the owner's word that the run is done, so a start never
// resumes it. The caller holds the manager lock.
func (d *Daemon) resumeCandidateLocked(proj registry.Project, item, worktree string, stored []*runrec.Run) *runrec.Record {
	var newest *runrec.Record
	for _, r := range stored {
		rec := r.Snapshot()
		if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil {
			rec = m.engine.Snapshot()
		}
		if rec.Project != proj.Name || rec.Item != item {
			continue
		}
		if newest == nil || rec.CreatedAt.After(newest.CreatedAt) || rec.CreatedAt.Equal(newest.CreatedAt) && rec.ID > newest.ID {
			c := rec
			newest = &c
		}
	}
	if newest == nil || newest.State != runrec.StateStopped || !newest.DismissedAt.IsZero() || engine.NotResumable(*newest) != "" || engine.RelandsOnRetry(*newest) || newest.Worktree != worktree {
		return nil
	}
	if m := d.runs.runs[newest.ID]; m != nil && (m.busy || m.stopping || m.filing || m.engine == nil) {
		return nil
	}
	if info, err := os.Stat(newest.Worktree); err != nil || !info.IsDir() {
		return nil
	}
	return newest
}

// probeResume looks at the item's stopped run without the locks held for the
// git calls: the manager lock is taken only to pick the run.
func (d *Daemon) probeResume(ctx context.Context, proj registry.Project, item, worktree string) *resumeProbe {
	d.runs.mu.Lock()
	var cand *runrec.Record
	if stored, err := d.runs.deps.Store.List(); err == nil {
		cand = d.resumeCandidateLocked(proj, item, worktree, stored)
	}
	d.runs.mu.Unlock()
	if cand == nil {
		return nil
	}
	probe := &resumeProbe{id: cand.ID, updatedAt: cand.UpdatedAt}
	run, err := d.runs.deps.Store.Open(cand.ID)
	if err != nil {
		probe.canErr = err
		return probe
	}
	probe.updatedAt = run.Snapshot().UpdatedAt
	e := engine.Load(d.runDeps(proj), proj, run)
	if probe.canErr = e.CanResume(ctx); probe.canErr == nil {
		probe.round, probe.match, probe.cmpErr = e.TreeMatchesLastRound(ctx)
	}
	return probe
}

// freshStart is the message of a start that did not resume the stopped run.
func freshStart(id, why string) string {
	return fmt.Sprintf("started a new run at implementer round 1 in the same worktree; stopped run %s not resumed: %s", id, why)
}

// autoResumeLocked decides what a start does with the stopped run of the item
// (S296, *proposed*; owner decision 2026-11-07, Q1 option b): it resumes the
// run when its worktree is unchanged (the tree fingerprint equals the last
// implementer round's tree_sha256, probed outside the lock), through the same
// resume as ork retry, with the default grant and in the wave of this start.
// Anything else starts fresh, and the message says why. With no free slot the
// start is refused, so the work is not restarted at round 1 behind the
// owner's back; so it is while another action holds the run's record, or when
// the run changed since the probe. The resume uses the stopped run's lineup,
// budgets and stop-after, not the confirmed plan's. The caller holds the
// manager lock.
func (d *Daemon) autoResumeLocked(ctx context.Context, f *registry.File, proj registry.Project, item, worktree, wave, waveName string, stored []*runrec.Run, probe *resumeProbe) (string, *resumeLaunch, error) {
	cand := d.resumeCandidateLocked(proj, item, worktree, stored)
	if cand == nil && probe == nil {
		return "", nil, nil
	}
	id := ""
	switch {
	case cand != nil:
		id = cand.ID
	case probe != nil:
		id = probe.id
	}
	// A finding action writes the record through a handle of its own while it
	// holds this lock; opening the record beside it could lose its update.
	lock := d.recordLockLocked(id)
	if !lock.TryLock() {
		return "", nil, &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf("stopped run %s is being changed by another action; start again in a moment", id)}
	}
	defer lock.Unlock()
	if cand == nil || probe == nil || probe.id != cand.ID || !probe.updatedAt.Equal(cand.UpdatedAt) {
		return "", nil, &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf("stopped run %s changed while the start was being checked; start again", id)}
	}
	if probe.canErr != nil {
		return freshStart(id, strings.TrimPrefix(probe.canErr.Error(), "run "+id+" ")), nil, nil
	}
	if probe.cmpErr != nil {
		return freshStart(id, probe.cmpErr.Error()), nil, nil
	}
	if !probe.match {
		return freshStart(id, fmt.Sprintf("its worktree changed since implementer round %d", probe.round)), nil, nil
	}
	run, err := d.runs.deps.Store.Open(id)
	if err != nil {
		return "", nil, err
	}
	e := engine.Load(d.runDeps(proj), proj, run)
	if d.runBlockedByLocked(f, e) != "" || !e.TryReserveLocal() {
		return "", nil, &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf("stopped run %s has work in its worktree and no slot is free; resume it now with ork retry %s, or ork dismiss %s to start a new run", id, id, id)}
	}
	m := &managedRun{engine: e}
	d.runs.runs[id] = m
	launch := &resumeLaunch{m: m, applied: make(chan struct{})}
	var once sync.Once
	d.launchRun(m, func(ctx context.Context) error {
		ctx = engine.WithApplied(engine.WithWaveName(engine.WithWave(ctx, wave), waveName), func() { once.Do(func() { close(launch.applied) }) })
		return e.RetryNote(ctx, "")
	})
	launch.done, launch.result = m.done, m.result
	return fmt.Sprintf("resumed stopped run %s: its worktree is unchanged since implementer round %d; %s; granted 2 more implementer rounds; uses the stopped run's lineup, budgets and stop-after", id, probe.round, engine.ResumeEntry(*cand)), launch, nil
}

// unfinishedOfItemLocked names another unfinished run of the item. The caller
// holds the manager lock.
func (d *Daemon) unfinishedOfItemLocked(project, item, except string) (string, error) {
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		return "", err
	}
	for _, r := range stored {
		rec := r.Snapshot()
		if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil {
			rec = m.engine.Snapshot()
		}
		if rec.ID != except && rec.Project == project && rec.Item == item && !runrec.IsFinal(rec.State) {
			return rec.ID, nil
		}
	}
	return "", nil
}

// prepareResume readies a retry of a stopped run (S296, *proposed*): it
// refuses a run that cannot be resumed, and replaces the manager's entry of
// the run, or adds one when a daemon restart left none, with an engine built
// from the project as the registry has it now, so a fixed check command
// reaches the resumed run. Anything else, a run that is not stopped, busy or
// unknown, is left to the retry that follows. The record lock keeps a
// finding action from writing through an older handle meanwhile.
//
// The read of slicer's check command (S246) is a command with a deadline, so
// it runs with no lock held: a first pass under the locks decides whether
// there is anything to prepare and copies the registry project, the read
// follows, and a second pass under the locks checks again and installs the
// engine only when the run is still resumable and the project is unchanged.
func (d *Daemon) prepareResume(ctx context.Context, p RunAction) error {
	proj, ok, err := d.resumableProject(p)
	if err != nil || !ok {
		return err
	}
	effective := d.runProject(ctx, proj)
	lock := d.runRecordLock(p.Run)
	lock.Lock()
	defer lock.Unlock()
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	again, ok, err := d.resumableProjectLocked(p)
	if err != nil || !ok {
		return err
	}
	if !reflect.DeepEqual(again, proj) {
		return &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf("project %s or run %s changed while the retry was being prepared; retry again", proj.Name, p.Run)}
	}
	run, err := d.runs.deps.Store.Open(p.Run)
	if err != nil {
		return err
	}
	d.runs.runs[p.Run] = &managedRun{engine: engine.Load(d.runDeps(effective), effective, run)}
	return nil
}

// resumableProject is resumableProjectLocked under the record and manager locks.
func (d *Daemon) resumableProject(p RunAction) (registry.Project, bool, error) {
	lock := d.runRecordLock(p.Run)
	lock.Lock()
	defer lock.Unlock()
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	return d.resumableProjectLocked(p)
}

// resumableProjectLocked refuses a run that cannot be resumed and returns the
// registry project of one that can. ok is false when there is nothing to
// prepare and the retry that follows says the rest. The caller holds the
// record lock and the manager lock.
func (d *Daemon) resumableProjectLocked(p RunAction) (proj registry.Project, ok bool, err error) {
	if d.runs.closed {
		return proj, false, nil
	}
	var rec runrec.Record
	if m := d.runs.runs[p.Run]; m != nil {
		if m.busy || m.stopping || m.filing || m.engine == nil {
			return proj, false, nil
		}
		rec = m.engine.Snapshot()
	} else {
		run, err := d.runs.deps.Store.Open(p.Run)
		if err != nil {
			return proj, false, nil // no such run, or not readable: the retry says so
		}
		rec = run.Snapshot()
	}
	if rec.State == runrec.StateCommitted {
		return proj, false, &ipc.Error{Code: CodeRefused, Message: "run " + p.Run + " is committed; there is nothing to resume"}
	}
	if rec.State != runrec.StateStopped {
		return proj, false, nil
	}
	if why := engine.NotResumable(rec); why != "" {
		return proj, false, &ipc.Error{Code: CodeRefused, Message: why}
	}
	if other, err := d.unfinishedOfItemLocked(rec.Project, rec.Item, rec.ID); err != nil {
		return proj, false, err
	} else if other != "" {
		return proj, false, &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf("run %s cannot resume while run %s of %s is unfinished; ork stop %s first", p.Run, other, rec.Item, other)}
	}
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		return proj, false, err
	}
	if newer := supersededBy(rec, stored); newer != "" {
		return proj, false, &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf("run %s is superseded by run %s, which uses the same worktree; resume or stop %s instead", p.Run, newer, newer)}
	}
	f, err := d.loadRunRegistryLocked()
	if err != nil {
		return proj, false, err
	}
	found, isRegistered := find(f, rec.Project)
	if !isRegistered {
		return proj, false, notFound(rec.Project, f)
	}
	return found, true, nil
}

// supersededBy names a newer run of the same item that uses the same worktree
// as rec, or "". Resuming the older run would continue work the newer run has
// since replaced (S296, *proposed*).
func supersededBy(rec runrec.Record, stored []*runrec.Run) string {
	for _, r := range stored {
		o := r.Snapshot()
		if o.ID == rec.ID || o.Project != rec.Project || o.Item != rec.Item || o.Worktree != rec.Worktree {
			continue
		}
		if o.CreatedAt.After(rec.CreatedAt) || o.CreatedAt.Equal(rec.CreatedAt) && o.ID > rec.ID {
			return o.ID
		}
	}
	return ""
}

// stoppedNext is the command that resumes a stopped run, as ork status and
// ork show print it, or "" when the run cannot be resumed (S296, *proposed*).
func stoppedNext(rec runrec.Record, stored []*runrec.Run) string {
	if engine.NotResumable(rec) != "" || engine.RelandsOnRetry(rec) || supersededBy(rec, stored) != "" {
		return ""
	}
	if info, err := os.Stat(rec.Worktree); err != nil || !info.IsDir() {
		return ""
	}
	return "ork retry " + rec.ID + " [--rounds N] or ork dismiss " + rec.ID
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
	// Name starts a saved wave (S215, *proposed*): the project must have a saved
	// wave of that name and every item must be one of its members. The name is
	// stamped on each run record; the wave id is still a fresh one.
	Name string `json:"name,omitempty"`
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
	// Message says what the start did with a stopped run of the item (S296,
	// *proposed*): resumed it, or started fresh and why.
	Message string `json:"message,omitempty"`
}

// WaveResult is the proposed result shape of wave.start and wave.stop.
type WaveResult struct {
	Wave  string           `json:"wave"`
	Name  string           `json:"name,omitempty"` // proposed (S215): the saved wave's name, when wave.start named one
	Items []WaveItemResult `json:"items"`
}

func waveItem(item string, rec runrec.Record, err error, message ...string) WaveItemResult {
	if err != nil {
		return WaveItemResult{Item: item, Error: ipc.ErrorOf(err)}
	}
	res := WaveItemResult{Item: item, Run: &rec}
	if len(message) > 0 {
		res.Message = message[0]
	}
	return res
}

// blockedItems reads slicer's dependency state before any member is admitted.
func (d *Daemon) blockedItems(ctx context.Context, proj registry.Project, ids []string) error {
	seen := make(map[string]bool, len(ids))
	var deps []slicercli.ItemDeps
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		item, err := d.runs.deps.Slicer.ItemDeps(ctx, proj.Path, id)
		if err != nil {
			return err
		}
		// The requested id names the item even when lean output omits it.
		item.ID = id
		deps = append(deps, *item)
	}
	titles := map[string]string{}
	for _, item := range deps {
		for _, blocker := range item.BlockedBy {
			if _, read := titles[blocker]; read {
				continue
			}
			// Remember failed reads too: titles are optional, the refusal is not.
			titles[blocker] = ""
			if detail, err := d.runs.deps.Slicer.Show(ctx, proj.Path, blocker); err == nil {
				titles[blocker] = detail.Title
			}
		}
	}
	if message := BlockedMessage(ids, deps, titles); message != "" {
		return &ipc.Error{Code: CodeRefused, Message: message}
	}
	return nil
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
	proj, err := d.registered(project)
	if err != nil {
		return nil, err
	}
	// A named wave must be saved, and the items its members, before anything
	// is read from slicer or admitted (S215, *proposed*).
	ids := make([]string, 0, len(p.Items))
	for _, item := range p.Items {
		ids = append(ids, item.Item)
	}
	if p.Name != "" {
		if err := d.checkSavedWave(project, p.Name, ids); err != nil {
			return nil, err
		}
	}
	// A project with no check command refuses the whole wave, before any
	// member is admitted, unless every item carries allow_no_check.
	if proj.CheckCommand == "" && d.runProject(ctx, proj).CheckCommand == "" { // slicer's check_command counts too (S246)
		for _, item := range p.Items {
			if !item.AllowNoCheck {
				return nil, noCheckRefusal(project)
			}
		}
	}
	if err := d.blockedItems(ctx, proj, ids); err != nil {
		return nil, err
	}
	// Inspect every Files section before admitting any member.
	if len(p.Items) > 1 {
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
	result := &WaveResult{Wave: newWaveID(time.Now()), Name: p.Name, Items: []WaveItemResult{}}
	for _, item := range p.Items {
		item.wave = result.Wave
		item.waveName = p.Name
		if err := d.specGate(ctx, item.Project, item.Item); err != nil {
			result.Items = append(result.Items, waveItem(item.Item, runrec.Record{}, err))
			continue
		}
		reply, err := d.startRunReply(ctx, item)
		result.Items = append(result.Items, waveItem(item.Item, reply.Record, err, reply.Message))
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

// hasRoomLocked reports whether one more busy frontier run fits under the global cap
// and the project's cap. Zero reads as unset: the global cap then defaults
// and a project has no cap (adj 57, adj 172). Only busy frontier runs hold
// these slots; local reservations are checked by runBlockedByLocked.
// A paused frontier run holds none (adj 122, adj 177). The caller holds the manager lock.
func (d *Daemon) hasRoomLocked(f *registry.File, project string) bool {
	return d.blockedByLocked(f, project) == ""
}

// blockedByLocked names the cap that leaves no room for one more busy run of
// the project, "project cap N" or "global cap N", or is empty when there is
// room. The project cap is named first when both hold. The caller holds the
// manager lock.
func (d *Daemon) blockedByLocked(f *registry.File, project string) string {
	global := f.GlobalCap
	if global == 0 {
		global = defaultGlobalCap
	}
	projectCap := 0
	if p, ok := find(f, project); ok {
		projectCap = p.Cap
	}
	total, inProject := d.frontierSlotsLocked(project)
	if projectCap != 0 && inProject >= projectCap {
		return fmt.Sprintf("project cap %d", projectCap)
	}
	if total >= global {
		return fmt.Sprintf("global cap %d", global)
	}
	return ""
}

// runBlockedByLocked applies the admitted primary's cap class. Local runs
// that already hold their lifetime reservation need no second slot.
func (d *Daemon) runBlockedByLocked(f *registry.File, e *engine.Engine) string {
	return d.recordBlockedByLocked(f, e.Snapshot(), e.HasLocalReservation())
}

func (d *Daemon) recordBlockedByLocked(f *registry.File, rec runrec.Record, held bool) string {
	if engine.IsLocalRun(rec.Lineup) {
		if held || d.runs.deps.Vendors.HasSlot("claude-local") {
			return ""
		}
		return fmt.Sprintf("local pool cap %d", engine.LocalSlotCap)
	}
	return d.blockedByLocked(f, rec.Project)
}

// frontierSlotsLocked counts cap slots. A busy frontier run holds one. A
// sliver chain holds one between its slivers too: the slot stays taken from
// the first sliver that leaves the queue until the chain has no queued sliver
// left, so another run cannot take it in the gap. A chain that has not
// started, or that a sliver stopped, holds none. The caller holds the manager
// lock.
func (d *Daemon) frontierSlotsLocked(project string) (total, inProject int) {
	seen := map[string]bool{}
	add := func(key, proj string) {
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		total++
		if proj == project {
			inProject++
		}
	}
	chains := map[string][]runrec.Record{}
	for _, r := range d.runs.runs {
		if r.engine == nil {
			continue
		}
		rec := r.engine.Snapshot()
		if rec.Chain != "" && !r.engine.LocalPrimary() {
			chains[rec.Chain] = append(chains[rec.Chain], rec)
		}
		if !r.busy || r.engine.LocalPrimary() {
			continue
		}
		key := rec.ID
		if rec.Chain != "" {
			key = rec.Chain
		}
		add(key, rec.Project)
	}
	for id, members := range chains {
		if chainHolds(members) {
			add(id, members[0].Project)
		}
	}
	return total, inProject
}

// chainHolds reports that a chain has started, has not been stopped, and
// still has a sliver waiting.
func chainHolds(members []runrec.Record) bool {
	admitted, queued, stopped := false, false, false
	for _, m := range members {
		switch m.State {
		case runrec.StateStopped:
			stopped = true
		case runrec.StateQueued:
			queued = true
		default:
			admitted = true
		}
	}
	return admitted && queued && !stopped
}

// sliverBlockedLocked reports that an earlier sliver of this chain has not
// finished, or stopped. The chain does not pass a stopped sliver.
func (d *Daemon) sliverBlockedLocked(rec runrec.Record) bool {
	if rec.Chain == "" || rec.SliverIndex <= 1 {
		return false
	}
	for _, r := range d.runs.runs {
		if r.engine == nil {
			continue
		}
		other := r.engine.Snapshot()
		if other.Chain != rec.Chain || other.SliverIndex >= rec.SliverIndex {
			continue
		}
		if (other.State == runrec.StateStopped && other.DismissedAt.IsZero()) || !runrec.IsFinal(other.State) {
			return true
		}
	}
	return false
}

// worktreeOwnedLocked reports a non-queued run that still holds rec's
// worktree, or a stopped sliver of another chain that keeps the worktree and
// its branch for a retry while a later sliver of that chain has not finished.
func (d *Daemon) worktreeOwnedLocked(rec runrec.Record) bool {
	var all []runrec.Record
	for _, r := range d.runs.runs {
		if r.engine == nil {
			continue
		}
		all = append(all, r.engine.Snapshot())
	}
	for _, other := range all {
		if other.ID == rec.ID || other.Worktree != rec.Worktree {
			continue
		}
		if !runrec.IsFinal(other.State) && other.State != runrec.StateQueued {
			return true
		}
		if other.Chain != "" && other.Chain != rec.Chain && other.State == runrec.StateStopped && other.DismissedAt.IsZero() {
			for _, later := range all {
				if later.Chain == other.Chain && later.SliverIndex > other.SliverIndex && !runrec.IsFinal(later.State) {
					return true
				}
			}
		}
	}
	return false
}

// chainContinuesLocked reports that rec is the waiting sliver of a chain that
// already holds its one cap slot.
func (d *Daemon) chainContinuesLocked(rec runrec.Record) bool {
	if rec.Chain == "" || rec.State != runrec.StateQueued {
		return false
	}
	var members []runrec.Record
	for _, r := range d.runs.runs {
		if r.engine == nil {
			continue
		}
		other := r.engine.Snapshot()
		if other.Chain == rec.Chain {
			members = append(members, other)
		}
	}
	return chainHolds(members)
}

// startQueuedLocked first starts the paused runs whose answer or retry waits
// for a slot, oldest request first, then the oldest queued run whose caps have
// room, and repeats until none fits (adj 177: a waiting resume goes before any
// queued run). The caller holds the manager lock.
func (d *Daemon) startQueuedLocked(f *registry.File) {
	if d.runs.closed || f == nil {
		return
	}
	for _, r := range d.runs.runs {
		if r.engine != nil && r.engine.LocalPrimary() {
			d.watchLocalSlots()
			break
		}
	}
	d.startResumesLocked(f)
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
		if d.sliverBlockedLocked(w.rec) || d.worktreeOwnedLocked(w.rec) {
			continue
		}
		// The next sliver of a chain that already holds its slot does not take
		// a second one (S62).
		if (!d.chainContinuesLocked(w.rec) && d.runBlockedByLocked(f, w.r.engine) != "") || !w.r.engine.TryReserveLocal() {
			continue
		}
		if err := w.r.engine.Dequeue(); err != nil {
			w.r.engine.ReleaseLocal()
			fmt.Fprintf(os.Stderr, "start queued run %s: %v\n", w.rec.ID, err)
			continue
		}
		d.launchRun(w.r, w.r.engine.Run)
	}
}

// startResumesLocked starts the waiting resumes that fit, oldest request first.
// A resume of a project that is no longer registered stays for the owner to
// stop. The caller holds the manager lock.
func (d *Daemon) startResumesLocked(f *registry.File) {
	type waiting struct {
		r   *managedRun
		rec runrec.Record
	}
	var resumes []waiting
	for _, r := range d.runs.runs {
		if r.busy || r.stopping || r.engine == nil {
			continue
		}
		if rec := r.engine.Snapshot(); rec.State == runrec.StatePaused && rec.Resume != nil && d.runs.waveStops[rec.Wave] == 0 {
			resumes = append(resumes, waiting{r, rec})
		}
	}
	sort.Slice(resumes, func(i, j int) bool {
		a, b := resumes[i].rec, resumes[j].rec
		if !a.Resume.At.Equal(b.Resume.At) {
			return a.Resume.At.Before(b.Resume.At)
		}
		return a.ID < b.ID
	})
	for _, w := range resumes {
		if _, ok := find(f, w.rec.Project); !ok || d.runBlockedByLocked(f, w.r.engine) != "" {
			continue
		}
		held := w.r.engine.HasLocalReservation()
		if !w.r.engine.TryReserveLocal() {
			continue
		}
		// The request leaves the record first: a start the engine refuses is
		// not tried again at the next free slot.
		if err := w.r.engine.ClearResume(); err != nil {
			if !held {
				w.r.engine.ReleaseLocal()
			}
			fmt.Fprintf(os.Stderr, "start waiting resume %s: %v\n", w.rec.ID, err)
			continue
		}
		req := w.rec.Resume
		d.launchRun(w.r, d.resumeJob(w.r, req.Method, RunAction{Run: w.rec.ID, Text: req.Text, Rounds: req.Rounds, AcceptShape: req.AcceptShape, Again: req.Again, Adj: req.Adj, By: req.By}, nil))
	}
}

// fileWaitingAnswerLocked files the owner's answer of a run in adj for a resume
// that waits for a slot. The adj calls run outside the manager lock, so
// r.filing keeps every other owner action of the run out, and a stop waits for
// r.filed. It returns with the lock held and r.filing cleared, whatever the
// outcome. Anything that touched the run meanwhile (a stop or job in flight, a
// closed manager, another state) is a change: the caller stores no resume and
// starts no job (S274).
func (d *Daemon) fileWaitingAnswerLocked(ctx context.Context, r *managedRun, p RunAction) (string, error) {
	r.filing, r.filed = true, make(chan struct{})
	d.runs.mu.Unlock()
	notice, err := r.engine.FileAnswer(ctx, p.Text, p.By)
	d.runs.mu.Lock()
	r.filing = false
	close(r.filed)
	if err != nil {
		return "", err
	}
	if d.runs.closed {
		return "", errors.New("run manager is closed")
	}
	rec := r.engine.Snapshot()
	if r.busy || r.stopping || r.stop != nil || rec.State != runrec.StatePaused || rec.Pending == nil || rec.Pending.Kind != runrec.PendingQuestion || rec.Resume != nil {
		return "", fmt.Errorf("run %s changed while its answer was filed in adj; check ork status", p.Run)
	}
	return notice, nil
}

// resumeJob is the job of an answer or a retry of a paused run. Both the owner
// action and a waiting resume that starts later run it. A filed adj notice
// goes to message when it is not nil.
func (d *Daemon) resumeJob(r *managedRun, method string, p RunAction, message *string) func(context.Context) error {
	if method == MethodRunAnswer || method == runrec.ResumeAnswer {
		return func(ctx context.Context) error {
			if p.Adj {
				notice, err := r.engine.FileAnswer(ctx, p.Text, p.By)
				if err != nil {
					return err
				}
				if message != nil {
					*message = notice
				}
			}
			return r.engine.Answer(ctx, p.Text)
		}
	}
	return func(ctx context.Context) error {
		return r.engine.RetryWith(ctx, engine.RetryOptions{Note: p.Text, Rounds: p.Rounds, AcceptShape: p.AcceptShape, Again: p.Again})
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
		// A refusal changed nothing: the caller has the text, and the run row
		// shows no error for it (S141).
		if !errors.Is(err, context.Canceled) && !engine.IsRefusal(err) {
			r.failure = err
		}
		result.record = r.engine.Snapshot()
		if runrec.IsFinal(result.record.State) {
			r.engine.ReleaseLocal()
		}
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
		d.rebuildKick() // a drain looks again: this run's job is over
	}()
}

// ResumeWaitNote is the owner's notice when an answer or retry waits for a slot.
const ResumeWaitNote = "resume waits for a slot; it starts before queued runs"

// RunReply adds a proposed transient notice to the usual run record reply.
type RunReply struct {
	runrec.Record
	Message string `json:"message,omitempty"`
	// Deferred lists the human checks a run.approve with later_checks, or a
	// run.check with all, marked later, as "N. text" (S316, *proposed*).
	Deferred []string `json:"deferred,omitempty"`
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
		text := "no run " + p.Run
		// The slicer relay run id is not an ork run id; name the ork run (S237).
		for id, m := range d.runs.runs {
			if m.engine != nil && p.Run != "" && m.engine.Snapshot().RelayRun == p.Run {
				text += "; that is the slicer run of ork run " + id
				break
			}
		}
		d.runs.mu.Unlock()
		return RunReply{}, &ipc.Error{Code: CodeNotFound, Message: text}
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
	// A stop waits for an answer that is being filed in adj, then stops the run:
	// it must not start a job beside the filing (S274).
	for method == MethodRunStop && r.filing {
		filed := r.filed
		d.runs.mu.Unlock()
		select {
		case <-filed:
		case <-ctx.Done():
			return RunReply{}, ctx.Err()
		}
		d.runs.mu.Lock()
		if d.runs.closed {
			d.runs.mu.Unlock()
			return RunReply{}, errors.New("run manager is closed")
		}
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
	if method == MethodRunAnswer {
		if waiting := d.firstPermissionLocked(p.Run); waiting != nil {
			answer, err := permissionAnswer(p.Text)
			if err != nil {
				d.runs.mu.Unlock()
				return RunReply{}, err
			}
			select {
			case waiting.reply <- answer:
			default:
				d.runs.mu.Unlock()
				return RunReply{}, fmt.Errorf("permission answer is already being delivered")
			}
			waiting.answered = true
			rec := r.engine.Snapshot()
			d.runs.mu.Unlock()
			return RunReply{Record: rec}, nil
		}
	}
	if (r.busy || r.filing) && method != MethodRunStop {
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
	// A resume that waits for a slot blocks a second answer or retry of either
	// kind: the owner hears that one waits, not that the pause has another kind.
	if (method == MethodRunAnswer || method == MethodRunRetry) && r.engine.Snapshot().Resume != nil {
		d.runs.mu.Unlock()
		return RunReply{}, fmt.Errorf("run %s already has a resume waiting; use ork stop %s", p.Run, p.Run)
	}
	var fn func(context.Context) error
	var message string
	var deferred []string // set by the job; read once the job is done
	var resume *runrec.ResumeRequest
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
		fn = d.resumeJob(r, method, p, &message)
		resume = &runrec.ResumeRequest{Method: runrec.ResumeAnswer, Text: p.Text, Adj: p.Adj, By: p.By}
	case MethodRunRetry:
		rec := r.engine.Snapshot()
		if rec.State == runrec.StateStopped {
			// A stopped run resumes from its worktree (prepareResume built
			// this engine from the current project). Owner actions bypass
			// frontier caps (adj 122); a local run reacquires its pool slot
			// when the engine advances.
			fn = d.resumeJob(r, method, p, nil)
			break
		}
		if rec.State != runrec.StatePaused || rec.Pending == nil {
			d.runs.mu.Unlock()
			return RunReply{}, fmt.Errorf("run %s is not paused with a pending step; use ork stop %s to stop it", p.Run, p.Run)
		}
		if rec.Pending.Kind == runrec.PendingQuestion {
			d.runs.mu.Unlock()
			return RunReply{}, fmt.Errorf("run %s waits for an answer; use ork answer %s TEXT or ork stop %s", p.Run, p.Run, p.Run)
		}
		fn = d.resumeJob(r, method, p, nil)
		resume = &runrec.ResumeRequest{Method: runrec.ResumeRetry, Text: p.Text, Rounds: p.Rounds, AcceptShape: p.AcceptShape, Again: p.Again}
	case MethodRunApprove:
		fn = r.engine.Approve
		if p.LaterChecks {
			fn = func(ctx context.Context) (err error) {
				deferred, err = r.engine.ApproveLater(ctx)
				return err
			}
		}
	case MethodRunFix:
		fn = func(ctx context.Context) error { return r.engine.Fix(ctx, p.Text) }
	case MethodRunCheck:
		fn = func(context.Context) error { return r.engine.MarkCheck(p.Check, p.State) }
		if p.All {
			// Ticking says the owner looked, so all marks later only (adj 234).
			if p.State != runrec.CheckLater {
				d.runs.mu.Unlock()
				return RunReply{}, &ipc.Error{Code: CodeRefused, Message: "all marks checks later only; tick each check on its own"}
			}
			fn = func(context.Context) (err error) {
				deferred, err = r.engine.MarkOpenLater()
				return err
			}
		}
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
	// A paused run holds no slot, so its answer or retry needs one (adj 177). With
	// none free the request is kept in the record, the owner is told at once, and
	// the run starts ahead of queued runs when a slot frees (startResumesLocked).
	if resume != nil {
		f, err := d.loadRunRegistryLocked()
		if err != nil {
			fmt.Fprintf(os.Stderr, "resume %s: %v\n", p.Run, err)
		}
		waits := f != nil && d.runBlockedByLocked(f, r.engine) != ""
		if waits && resume.Adj && r.engine.Snapshot().Pending.Key != "" {
			// The answer goes to adj now; only the engine resume waits.
			notice, err := d.fileWaitingAnswerLocked(ctx, r, p)
			if err != nil {
				d.runs.mu.Unlock()
				return RunReply{}, err
			}
			// Filed: neither the waiting resume nor a start files it again.
			message, resume.Adj = notice, false
			fn = d.resumeJob(r, method, RunAction{Run: p.Run, Text: p.Text}, nil)
			if f, err = d.loadRunRegistryLocked(); err != nil {
				fmt.Fprintf(os.Stderr, "resume %s: %v\n", p.Run, err)
			}
			waits = f != nil && d.runBlockedByLocked(f, r.engine) != ""
		}
		if waits {
			resume.At = time.Now()
			err := r.engine.SetResume(*resume)
			rec := r.engine.Snapshot()
			d.runs.mu.Unlock()
			if engine.IsRefusal(err) {
				err = &ipc.Error{Code: CodeRefused, Message: err.Error()}
			}
			if err != nil {
				return RunReply{}, err
			}
			note := ResumeWaitNote
			if message != "" {
				note = message + "; " + note
			}
			return RunReply{Record: rec, Message: note}, nil
		}
	}
	// A caller that asks not to wait is answered once the engine has applied the
	// action and the run has left its pause; the job goes on under the manager's
	// context (S237). A refusal never fires the hook, so it comes back on done.
	var appliedCh chan struct{}
	if p.NoWait && (method == MethodRunAnswer || method == MethodRunRetry) {
		appliedCh = make(chan struct{})
		var once sync.Once
		run := fn
		fn = func(ctx context.Context) error {
			return run(engine.WithApplied(ctx, func() { once.Do(func() { close(appliedCh) }) }))
		}
	}
	d.launchRun(r, fn)
	r.stop = call // launchRun cleared the handle; a stop sets it again for later stops to join
	if call != nil {
		call.begin(r.done, r.result)
	}
	result := r.result
	done := r.done
	d.runs.mu.Unlock()
	// Owner commands return once the engine has durably applied the action
	// and, unless the caller asked not to wait, the run rests.
	select {
	case <-ctx.Done():
		return RunReply{}, ctx.Err()
	case <-appliedCh: // nil when the caller waits
		select {
		case <-done: // the job also ended: its result, and any error, is the reply
		default:
			return RunReply{Record: r.engine.Snapshot(), Message: message}, nil
		}
	case <-done:
	}
	err := result.err
	if engine.IsRefusal(err) {
		// A refusal has its own wire code, so a client shows its text as it is (S141).
		err = &ipc.Error{Code: CodeRefused, Message: err.Error()}
	}
	return RunReply{Record: result.record, Message: message, Deferred: deferred}, err
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
	return d.recordLockLocked(id)
}

// recordLockLocked is runRecordLock for a caller that holds the manager lock.
func (d *Daemon) recordLockLocked(id string) *sync.Mutex {
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
	run, err := d.openRun(id)
	if err != nil {
		return nil, err
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
		// A project with no check command refuses the start before the spec
		// gate asks the owner anything (adj 191). startRun checks it again.
		if proj, err := d.registered(p.Project); err == nil && proj.CheckCommand == "" && !p.AllowNoCheck {
			// slicer's check_command counts too (S246).
			if eff := d.runProject(ctx, proj); eff.CheckCommand == "" {
				return nil, noCheckRefusal(proj.Name)
			}
		}
		if err := d.specGate(ctx, p.Project, p.Item); err != nil {
			return nil, err
		}
		return d.startRunReply(ctx, p)
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
	if method == MethodRunRetry {
		if err := d.prepareResume(ctx, p); err != nil {
			return nil, err
		}
	}
	if method == MethodRunAnswer || method == MethodRunRetry {
		return d.ownerRunReply(ctx, method, p)
	}
	if method == MethodRunFile {
		return d.fileFinding(ctx, p)
	}
	if method == MethodRunReport {
		return d.runReport(p)
	}
	if method == MethodRunShow {
		return d.runShow(p)
	}
	if method == MethodRunRender {
		return d.runRender(p)
	}
	if method == MethodRunLandCheck {
		return d.runLandCheck(ctx, p)
	}
	if method == MethodRunEndLedger {
		return d.endLedger(ctx, p)
	}
	if method == MethodRunApprove || method == MethodRunCheck {
		return d.ownerRunReply(ctx, method, p) // the reply names the checks it deferred (S316)
	}
	return d.ownerRun(ctx, method, p)
}

// runLandCheck reads the stored record without holding a run-manager or
// per-run lock. The git reads must never delay snapshots or owner actions.
func (d *Daemon) runLandCheck(ctx context.Context, p RunAction) (engine.LandCheck, error) {
	run, err := d.openRun(p.Run)
	if err != nil {
		return engine.LandCheck{}, err
	}
	rec := run.Snapshot()
	if rec.State != runrec.StateAwaitingApproval || (rec.StopAfter != "" && rec.StopAfter != runrec.StopAfterLand) {
		return engine.LandCheck{}, nil
	}
	project, err := d.registered(rec.Project)
	if err != nil {
		return engine.LandCheck{}, err
	}
	return engine.CheckLanding(ctx, d.runDeps(project), project, rec), nil
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

func runTokens(roles []runrec.RoleRecord, live map[string]engine.Live) int {
	tokens := 0
	counted := map[string]bool{}
	for _, role := range roles {
		tokens += role.Tokens
		if role.EndedAt.IsZero() && !counted[role.Role] {
			tokens += live[role.Role].Tokens
			counted[role.Role] = true
		}
	}
	return tokens
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
		var byRole map[string]engine.Live
		if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil {
			// Read live first, record second: a role that ends between the reads
			// is counted from the record, ignoring its stale live entry.
			byRole = m.engine.LiveByRole()
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
			wait += "; next: " + pausedNext(rec)
		case runrec.StateAwaitingApproval:
			wait = "owner approval"
		case runrec.StateStopped:
			if next := stoppedNext(rec, stored); next != "" {
				wait += "; next: " + next
			}
		}
		if rec.State == runrec.StatePaused && rec.Resume != nil {
			// The owner has answered or retried; the run waits for a slot (adj 177).
			wait = "resume waits for a slot"
			if f, _ := d.loadRunRegistryLocked(); f != nil {
				held := false
				if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil {
					held = m.engine.HasLocalReservation()
				}
				if why := d.recordBlockedByLocked(f, rec, held); why != "" {
					wait += " under " + why
				}
			}
			wait += "; next: ork stop " + rec.ID
		}
		if permission := d.firstPermissionLocked(rec.ID); permission != nil {
			question = permission.question
			pending = "permission"
			wait = question.Title + "; next: ork answer " + rec.ID + " allow|deny"
		}
		var denials []string
		denialCounts := map[string]int{}
		shapeRetry := rec.AutomaticShapeRetry() != nil
		var roles []source.EngineRole
		for _, role := range rec.Roles {
			roles = append(roles, source.EngineRole{ShapeRetry: role.ShapeRetry != nil && role.ShapeRetry.ShapePauses == 0, Report: role.Report, Role: role.Role, Part: role.Part, Round: role.Round, Vendor: role.Vendor, Model: role.Model, Effort: role.Effort, Step: role.Step, StartedAt: role.StartedAt, EndedAt: role.EndedAt, Exit: role.Exit, Tokens: role.Tokens, CostUSD: role.CostUSD})
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
		tokens := runTokens(rec.Roles, byRole)
		usageKnown := false
		cost := 0.0
		for _, role := range rec.Roles {
			usageKnown = usageKnown || role.Tokens != 0 || role.CostUSD != 0
			cost += role.CostUSD
		}
		var live engine.Live
		var activity []int
		var working []source.EngineWork
		if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil {
			activity = m.engine.Activity(d.cfg.Clock.Now())
			live = m.engine.Live()
			for _, w := range m.engine.Working() {
				working = append(working, source.EngineWork{What: w.What, Since: w.Since, LastOutput: w.LastOutput, SleptSeconds: int(w.Slept.Seconds())})
			}
		}
		// A run whose next role start waits for the daemon's rebuild says so (S431).
		if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil && m.engine.WaitsForHold() && d.runs.deps.Hold.Held() {
			wait = engine.HoldReason
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
		vendorWait := ""
		if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil {
			vendorWait = m.engine.VendorWait()
		}
		var lastCheck *source.EngineCheck
		if n := len(rec.Checks); n > 0 {
			c := rec.Checks[n-1]
			lastCheck = &source.EngineCheck{Round: c.Round, Command: c.Command, OK: c.OK, Log: c.Log}
		}
		runs = append(runs, EngineRun{LastCheck: lastCheck, Lineup: rec.Lineup, VendorWait: vendorWait, StopAfter: rec.StopAfter, Findings: findings, Card: card, Question: question, Key: questionKey, Unavailable: unavailable, Error: failure, Activity: activity, Working: working, Tail: live.Text, TailTruncated: live.TailTruncated, UsageKnown: usageKnown || live.UsageKnown, LiveUsageKnown: live.UsageKnown, Tokens: tokens, CostUSD: cost, Roles: roles, Denials: denials, ID: rec.ID, Item: rec.Item, Parent: rec.Parent, Chain: rec.Chain, SliverIndex: rec.SliverIndex, SliverCount: rec.SliverCount, Branch: rec.Branch, Worktree: rec.Worktree, Merged: d.runs.merged[rec.ID], Landed: !rec.LandedAt.IsZero(), LandFixes: rec.LandFixes, Dismissed: !rec.DismissedAt.IsZero(), CleanupFailed: cleanupFailed(rec), Cleaned: rec.Cleaned(), WorktreeGone: rec.State == runrec.StateCommitted && !rec.Cleaned() && worktreeGone(rec), State: rec.State, ShapeRetry: shapeRetry, Wait: wait, ResumeWaiting: rec.State == runrec.StatePaused && rec.Resume != nil, Pending: pending, RelayRun: rec.RelayRun, Wave: rec.Wave, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt, WaitStartedAt: rec.WaitStartedAt})
	}
	return runs
}

// pausedNext is the command that resumes a paused run, as ork status and
// ork show print it.
func pausedNext(rec runrec.Record) string {
	next := "ork retry " + rec.ID
	if rec.Pending != nil && rec.Pending.Kind == runrec.PendingRounds {
		next += " [--rounds N] or ork stop " + rec.ID
	}
	if rec.Pending != nil && rec.Pending.Kind == runrec.PendingError && strings.HasPrefix(rec.Pending.Reason, engine.CheckCannotRunPrefix) {
		// S295 (*proposed*): a plain retry verifies the same command again; the
		// reason names the fix, which starts with a stop.
		next = "ork stop " + rec.ID
	}
	if rec.Pending != nil && rec.Pending.Kind == runrec.PendingQuestion {
		next = "ork answer " + rec.ID + " TEXT"
	}
	if rec.Pending != nil && rec.Pending.Kind == runrec.PendingShape && rec.Pending.ShapePauses >= runrec.ShapePauseCap {
		// S298 (*proposed*): a plain retry is refused here; the reason names the
		// report, and ork show prints it.
		next = fmt.Sprintf("ork retry %s --accept-shape, ork retry %s --again or ork stop %s", rec.ID, rec.ID, rec.ID)
	}
	return next
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

// ReportReply is the answer of run.report (proposed, S176).
type ReportReply struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// ShowReply is the answer of run.show (proposed, S297): what the stored record
// says of a run, in full. It carries file names, never file text; run.report
// serves the text. It has no owner retry note.
type ShowReply struct {
	ID           string        `json:"id"`
	Item         string        `json:"item"`
	State        string        `json:"state"`
	Reason       string        `json:"reason,omitempty"`
	Next         string        `json:"next,omitempty"` // the command that resumes a paused run, as ork status prints it
	Pending      *ShowPending  `json:"pending,omitempty"`
	Roles        []ShowRole    `json:"roles"`
	Checks       []ShowCheck   `json:"checks"`
	Findings     []ShowFinding `json:"findings"`
	LatestReport string        `json:"latest_report,omitempty"` // the pending role and round's newest report, else the newest report
	Landed       bool          `json:"landed,omitempty"`        // proposed (S257): committed and landed on the base branch by ork
}

// ShowCheck is one run of the project's checks, with the name of its log. It
// has none of the record's other fields (the log's hash).
type ShowCheck struct {
	Round    int      `json:"round"`
	Command  string   `json:"command"`
	OK       bool     `json:"ok"`
	Seconds  int      `json:"seconds"`
	Log      string   `json:"log,omitempty"`
	Deferred []string `json:"deferred,omitempty"`
}

// ShowPending is the pause of a run, with the whole reason.
type ShowPending struct {
	Ask      *runrec.OwnerQuestion `json:"ask,omitempty"`
	Kind     string                `json:"kind"`
	State    string                `json:"state"`
	Role     string                `json:"role,omitempty"`
	Round    int                   `json:"round,omitempty"`
	Reason   string                `json:"reason"`
	Question string                `json:"question,omitempty"`
	Key      string                `json:"key,omitempty"`
}

// ShowRole is one role attempt. Denied counts its denied commands and
// DeniedFirst holds the first three.
type ShowRole struct {
	Role        string    `json:"role"`
	Round       int       `json:"round"`
	Attempt     int       `json:"attempt"`
	Exit        string    `json:"exit,omitempty"`
	EndedAt     time.Time `json:"ended_at,omitzero"` // zero while the round runs (S403, *proposed*)
	Report      string    `json:"report,omitempty"`
	Denied      int       `json:"denied,omitempty"`
	DeniedFirst []string  `json:"denied_first,omitempty"`
}

// ShowFinding is an owner or later finding with its full text.
type ShowFinding struct {
	Ask    *runrec.OwnerQuestion `json:"ask,omitempty"`
	N      int                   `json:"n"`
	Kind   string                `json:"kind"`
	Title  string                `json:"title"`
	Text   string                `json:"text"`
	Answer string                `json:"answer,omitempty"`
	Filed  string                `json:"filed,omitempty"`
}

// openRun opens the stored record of a run for a read-only call. A run id that
// is not a plain name and a run with no record are not_found. A record that is
// there but cannot be read is not "no run": it is a plain error, so the wire
// code is internal, and its message names the run and says why (S147): "the
// record of run ID is unreadable: CAUSE". The cause has no path, as the
// approval card's reason has none (S201); the error still wraps the original.
func (d *Daemon) openRun(id string) (*runrec.Run, error) {
	run, err := d.runs.deps.Store.Open(id)
	switch {
	case err == nil:
		return run, nil
	case errors.Is(err, runrec.ErrBadRunID), errors.Is(err, os.ErrNotExist):
		return nil, &ipc.Error{Code: CodeNotFound, Message: "no run " + id}
	}
	return nil, &unreadableRecordError{id: id, cause: d.unreadableCause(id, err), err: err}
}

// unreadableRecordError is a run record that is there and cannot be read. Its
// text is "the record of run ID is unreadable: CAUSE" with no path; it wraps
// the original error, so errors.Is and errors.As still see the cause.
type unreadableRecordError struct {
	id, cause string
	err       error
}

func (e *unreadableRecordError) Error() string {
	return "the record of run " + e.id + " is unreadable: " + e.cause
}
func (e *unreadableRecordError) Unwrap() error { return e.err }

// unreadableCause is why a run record cannot be read, without a path: the
// system's reason for a file error, else the error text with the record's path
// (or, failing that, the run directory) taken out.
func (d *Daemon) unreadableCause(id string, err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	dir := d.runs.deps.Store.RunDir(id)
	text := err.Error()
	if rest, ok := strings.CutPrefix(text, "run record "+filepath.Join(dir, "run.json")+": "); ok {
		return rest
	}
	return strings.ReplaceAll(text, dir+string(filepath.Separator), "")
}

// runReport reads only a file the stored record lists: a role report
// (roles[].report) or a check log (checks[].log, S297). It does not wait for
// the run; every missing name or file is not_found, and so is a run with no
// record. A record that cannot be read is an error that says so. The reader
// applies its guards to the file (engine.ReadReport).
func (d *Daemon) runReport(p RunAction) (ReportReply, error) {
	run, err := d.openRun(p.Run)
	if err != nil {
		return ReportReply{}, err
	}
	missing := &ipc.Error{Code: CodeNotFound, Message: "no report " + p.Name + " for run " + p.Run}
	rec := run.Snapshot()
	listed := slices.ContainsFunc(rec.Roles, func(r runrec.RoleRecord) bool { return r.Report == p.Name }) ||
		slices.ContainsFunc(rec.Checks, func(c runrec.CheckRecord) bool { return c.Log == p.Name })
	if p.Name == "" || !listed {
		return ReportReply{}, missing
	}
	text, err := engine.ReadReport(d.runs.deps.Store.RunDir(p.Run), p.Name)
	if err != nil {
		return ReportReply{}, missing
	}
	return ReportReply{Name: p.Name, Text: text}, nil
}

// maxShownDenials is how many denied commands a role row of run.show names.
const maxShownDenials = 3

// runShow answers what the stored record says of a run (run.show, S297,
// *proposed*). Like run.report it reads the stored record and does not wait for
// the run. It serves no file and writes nothing. Besides the record it reads
// the run's live permission question, which is memory-only (S370).
func (d *Daemon) runShow(p RunAction) (ShowReply, error) {
	run, err := d.openRun(p.Run)
	if err != nil {
		return ShowReply{}, err
	}
	rec := run.Snapshot()
	reply := ShowReply{ID: rec.ID, Item: rec.Item, State: rec.State, Reason: rec.Reason, Landed: !rec.LandedAt.IsZero() && rec.State == runrec.StateCommitted, Roles: []ShowRole{}, Checks: []ShowCheck{}, Findings: []ShowFinding{}}
	var pendingRole string
	pendingRound := 0
	if rec.State == runrec.StatePaused && rec.Pending != nil {
		pd := rec.Pending
		reply.Pending = &ShowPending{Ask: pd.Ask, Kind: pd.Kind, State: pd.State, Role: pd.Role, Round: pd.Round, Reason: pd.Reason, Question: pd.Question, Key: pd.Key}
		reply.Reason = pd.Reason
		pendingRole, pendingRound = pd.Role, pd.Round
	}
	// A live permission question is not in the record. It shows with the whole
	// tool input, as ork status does (S370).
	d.runs.mu.Lock()
	permission := d.firstPermissionLocked(rec.ID)
	d.runs.mu.Unlock()
	if permission != nil {
		ask := *permission.question
		reply.Pending = &ShowPending{Ask: &ask, Kind: "permission", State: rec.State, Reason: ask.Reason}
		reply.Next = "ork answer " + rec.ID + " allow|deny"
	}
	if rec.State == runrec.StatePaused && permission == nil {
		reply.Next = pausedNext(rec) // as ork status, also for a record with no pending
	}
	if rec.State == runrec.StateStopped {
		stored, _ := d.runs.deps.Store.List()
		reply.Next = stoppedNext(rec, stored) // S296: the command that resumes a stopped run
	}
	attempts := map[string]int{}
	for _, role := range rec.Roles {
		key := fmt.Sprintf("%s\x00%d", role.Role, role.Round)
		attempts[key]++
		row := ShowRole{Role: role.Role, Round: role.Round, Attempt: attempts[key], Exit: role.Exit, EndedAt: role.EndedAt, Report: role.Report}
		denials := roleDenials(role)
		row.Denied = len(denials)
		row.DeniedFirst = denials[:min(len(denials), maxShownDenials)]
		reply.Roles = append(reply.Roles, row)
		if role.Report != "" {
			reply.LatestReport = role.Report
		}
	}
	if pendingRole != "" {
		// The pending role and round's newest report wins over a later report of another role.
		for i := len(reply.Roles) - 1; i >= 0; i-- {
			if r := reply.Roles[i]; r.Role == pendingRole && r.Round == pendingRound && r.Report != "" {
				reply.LatestReport = r.Report
				break
			}
		}
	}
	for _, c := range rec.Checks {
		reply.Checks = append(reply.Checks, ShowCheck{Round: c.Round, Command: c.Command, OK: c.OK, Seconds: c.Seconds, Log: c.Log, Deferred: c.Deferred})
	}
	for i, f := range rec.Findings {
		reply.Findings = append(reply.Findings, ShowFinding{Ask: f.Ask, N: i + 1, Kind: f.Kind, Title: f.Title, Text: f.Text, Answer: f.Answer, Filed: f.Filed})
	}
	return reply, nil
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
// that fails is not_found, except a record that exists and cannot be read: that
// is an error saying so (openRun). It does not wait for the run: it reads the
// stored record and the file.
func (d *Daemon) runRender(p RunAction) (RenderReply, error) {
	run, err := d.openRun(p.Run)
	if err != nil {
		return RenderReply{}, err
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

// LedgerEndReply is the reply of run.end-ledger: the ledger run it ended.
type LedgerEndReply struct {
	Run string `json:"run"`
}

// endLedger ends a stale slicer ledger run with outcome stopped and its running
// steps failed (S175, *proposed*). It re-reads the ledger, so a heartbeat that
// arrived after the owner opened the pane wins. It refuses a ledger run that an
// unfinished engine run owns: the owner stops that run on its own pane.
func (d *Daemon) endLedger(ctx context.Context, p RunAction) (LedgerEndReply, error) {
	proj, err := d.registered(p.Project)
	if err != nil {
		return LedgerEndReply{}, err
	}
	runs, err := d.runs.deps.Slicer.RelayRuns(ctx, proj.Path, false)
	if err != nil {
		return LedgerEndReply{}, err
	}
	i := slices.IndexFunc(runs, func(r slicercli.RelayRun) bool { return r.ID == p.Run })
	if i < 0 {
		return LedgerEndReply{}, &ipc.Error{Code: CodeNotFound, Message: "no ledger run " + p.Run}
	}
	if state := runs[i].State; state != "stale" {
		return LedgerEndReply{}, &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf("ledger run %s is %s, not stale", p.Run, state)}
	}
	d.runs.mu.Lock()
	stored, err := d.runs.deps.Store.List()
	d.runs.mu.Unlock()
	if err != nil {
		return LedgerEndReply{}, err
	}
	for _, r := range stored {
		if rec := r.Snapshot(); rec.RelayRun == p.Run && !runrec.IsFinal(rec.State) {
			return LedgerEndReply{}, &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf("ledger run %s belongs to engine run %s; stop that run instead", p.Run, rec.ID)}
		}
	}
	if err := d.runs.deps.Slicer.RelayEnd(ctx, runrec.Writer{}, proj.Path, p.Run, "stopped", "fail"); err != nil {
		return LedgerEndReply{}, err
	}
	return LedgerEndReply{Run: p.Run}, nil
}
