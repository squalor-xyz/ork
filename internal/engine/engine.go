// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/roles"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// DefaultMaxRounds is how many implementer rounds a slice may take before
// the engine stops it (*proposed*; budgets are S05).
const DefaultMaxRounds = 8

// CheckOutput is the result of the project's check command.
type CheckOutput struct {
	OK     bool
	Output string
}

// CheckFunc runs the project's check command in dir.
type CheckFunc func(ctx context.Context, dir, command string, timeout time.Duration) (CheckOutput, error)

// Proc looks at and kills process groups. The engine uses it only on the
// process groups its own run records name (engine.restart).
type Proc interface {
	Alive(pgid int) bool
	Kill(pgid int) error
}

// Deps are what an Engine calls out to.
type Deps struct {
	Runner    Runner
	Slicer    slicercli.Client
	GitBin    string // empty means "git"
	Store     *runrec.Store
	Check     CheckFunc // nil means RunCheck
	Proc      Proc      // nil means the operating system's
	Now       func() time.Time
	Sleep     func(ctx context.Context, d time.Duration) error // nil sleeps for real
	Backoff   []time.Duration                                  // locked-retry delays; nil means runrec.DefaultBackoff
	RandHex   func() string                                    // four hex digits for run ids; nil is random
	MaxRounds int                                              // 0 means DefaultMaxRounds
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

func (d Deps) gitBin() string {
	if d.GitBin != "" {
		return d.GitBin
	}
	return "git"
}

func (d Deps) maxRounds() int {
	if d.MaxRounds > 0 {
		return d.MaxRounds
	}
	return DefaultMaxRounds
}

func (d Deps) proc() Proc {
	if d.Proc != nil {
		return d.Proc
	}
	return osProc{}
}

func (d Deps) randHex() string {
	if d.RandHex != nil {
		return d.RandHex()
	}
	b := make([]byte, 2)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Config starts one slice run.
type Config struct {
	Project registry.Project
	Item    string
	Lineup  map[string][]runrec.LineupEntry // by role; the gate defaults to the reviewer's
	Budgets registry.Budgets
	Wave    string
	Queued  bool // create the run in state queued; Dequeue moves it to setup
}

// Engine runs one slice. The methods that move it (Run, Approve, Reject,
// Answer, Retry, Stop, Reconcile) run one at a time; Snapshot may be called
// at any moment.
type Engine struct {
	mu   sync.Mutex
	d    Deps
	proj registry.Project
	run  *runrec.Run
	wr   runrec.Writer
}

func newEngine(d Deps, proj registry.Project, run *runrec.Run) *Engine {
	return &Engine{d: d, proj: proj, run: run, wr: runrec.Writer{Sink: run, Now: d.now, Sleep: d.Sleep, Backoff: d.Backoff}}
}

// worktreePath is the project's worktree pattern with {id} filled, relative
// to the project directory unless it is absolute (git.worktrees).
func worktreePath(p registry.Project, id string) string {
	pattern := p.WorktreePattern
	if pattern == "" {
		pattern = ".worktrees/{id}"
	}
	pattern = strings.ReplaceAll(pattern, "{id}", id)
	if filepath.IsAbs(pattern) {
		return filepath.Clean(pattern)
	}
	return filepath.Join(p.Path, pattern)
}

func branchName(p registry.Project, id string) string {
	pattern := p.BranchPattern
	if pattern == "" {
		pattern = "{id}"
	}
	return strings.ReplaceAll(pattern, "{id}", id)
}

// lineup returns the primary entry for a role. The gate is the resumed
// reviewer, so it defaults to the reviewer's entry.
func lineup(l map[string][]runrec.LineupEntry, role string) (runrec.LineupEntry, bool) {
	if e := l[role]; len(e) > 0 {
		return e[0], true
	}
	if role == RoleGate {
		return lineup(l, RoleReviewer)
	}
	return runrec.LineupEntry{}, false
}

// New checks the contract, resolves permissions and creates the run record in
// the setup state (the queued state when cfg.Queued is set). It changes nothing in the project.
func New(ctx context.Context, d Deps, cfg Config) (*Engine, error) {
	if cfg.Item == "" {
		return nil, errors.New("no slice id")
	}
	if d.Store == nil || d.Runner == nil {
		return nil, errors.New("engine needs a run store and a runner")
	}
	for _, role := range []string{RoleImplementer, RoleReviewer} {
		if _, ok := lineup(cfg.Lineup, role); !ok {
			return nil, fmt.Errorf("the lineup has no %s", role)
		}
	}
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate} {
		if _, err := ResolvePermission(cfg.Project, role); err != nil {
			return nil, err
		}
	}
	line, err := d.Slicer.Contract(ctx)
	if err != nil {
		return nil, fmt.Errorf("slicer ai relay: %w", err)
	}
	version, err := roles.CheckContractLine(line)
	if err != nil {
		return nil, err
	}
	copied := map[string][]runrec.LineupEntry{}
	for role, entries := range cfg.Lineup {
		copied[role] = append([]runrec.LineupEntry(nil), entries...)
	}
	id := "ork-" + d.now().Format("20060102-150405") + "-" + d.randHex()
	state := runrec.StateSetup
	if cfg.Queued {
		state = runrec.StateQueued
	}
	budgets := cfg.Project.Budgets
	if cfg.Budgets.Minutes != 0 {
		budgets.Minutes = cfg.Budgets.Minutes
	}
	if cfg.Budgets.Tokens != 0 {
		budgets.Tokens = cfg.Budgets.Tokens
	}
	run, err := d.Store.Create(runrec.Record{
		ID: id, Wave: cfg.Wave, Project: cfg.Project.Name, Item: cfg.Item,
		Worktree: worktreePath(cfg.Project, cfg.Item), Branch: branchName(cfg.Project, cfg.Item),
		Contract: version, State: state, Round: 1, Lineup: copied, Budgets: budgets,
	})
	if err != nil {
		return nil, err
	}
	return newEngine(d, cfg.Project, run), nil
}

// Load returns the engine of a stored run.
func Load(d Deps, proj registry.Project, run *runrec.Run) *Engine { return newEngine(d, proj, run) }

// Snapshot returns a copy of the run record.
func (e *Engine) Snapshot() runrec.Record { return e.run.Snapshot() }

// AdjRoot is the directory where `adj ask` runs: the project's main checkout,
// never a worktree, so record IDs do not collide across worktrees. Filing a
// question is S06.
func (e *Engine) AdjRoot() string { return e.proj.Path }

// resting reports whether a state waits for the owner or for a slot, or is
// final.
func resting(state string) bool {
	switch state {
	case runrec.StateAwaitingApproval, runrec.StatePaused, runrec.StateStopped, runrec.StateCommitted, runrec.StateQueued:
		return true
	}
	return false
}

// Run advances the slice until it rests: awaiting approval, paused, stopped or
// committed. On a slice that already rests it does nothing.
func (e *Engine) Run(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.advance(ctx)
}

// advance runs the state machine until a rest. The caller holds e.mu. An
// error means the engine could not go on at all (the context ended or the run
// record cannot be written); a stop or a pause is not an error.
func (e *Engine) advance(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		switch state := e.run.Snapshot().State; state {
		case runrec.StateSetup:
			err = e.setup(ctx)
		case runrec.StateImplement:
			err = e.implement(ctx)
		case runrec.StateChecks:
			err = e.checks(ctx)
		case runrec.StateReview:
			err = e.review(ctx)
		case runrec.StateGate:
			err = e.gate(ctx)
		case runrec.StateCommit:
			err = e.commit(ctx)
		case runrec.StateFinish:
			err = e.finish(ctx)
		default:
			if resting(state) {
				return nil
			}
			return fmt.Errorf("run %s is in unknown state %q", e.run.Snapshot().ID, state)
		}
		if err != nil {
			return err
		}
	}
}

// Approve accepts a slice that awaits approval: finish, then one commit (adj #81).
func (e *Engine) Approve(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state := e.run.Snapshot().State; state != runrec.StateAwaitingApproval {
		return fmt.Errorf("the slice is %s, not awaiting approval", state)
	}
	err := e.run.Update(func(r *runrec.Record) {
		r.Approvals = append(r.Approvals, runrec.Approval{What: "slice", At: e.d.now()})
		r.State = runrec.StateFinish
	})
	if err != nil {
		return err
	}
	return e.advance(ctx)
}

// Reject stops a slice that awaits approval.
func (e *Engine) Reject(ctx context.Context, why string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state := e.run.Snapshot().State; state != runrec.StateAwaitingApproval {
		return fmt.Errorf("the slice is %s, not awaiting approval", state)
	}
	return e.stop(ctx, "rejected by the owner: "+why)
}

// Answer gives the owner's answer to the question a paused slice shows, and
// resumes the role with it.
func (e *Engine) Answer(ctx context.Context, text string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	text = strings.TrimSpace(text)
	rec := e.run.Snapshot()
	if rec.State != runrec.StatePaused || rec.Pending == nil || rec.Pending.Kind != runrec.PendingQuestion {
		return errors.New("the slice has no owner question to answer")
	}
	if text == "" {
		return errors.New("the answer is empty")
	}
	err := e.run.Update(func(r *runrec.Record) {
		for i := len(r.Questions) - 1; i >= 0; i-- {
			if r.Questions[i].Answer == "" {
				r.Questions[i].Answer = text
				r.Questions[i].AnsweredAt = e.d.now()
				break
			}
		}
		r.Pending.Answer = text
		r.State = r.Pending.State
		r.Reason = ""
	})
	if err != nil {
		return err
	}
	return e.advance(ctx)
}

// Retry runs again the role, or the step, that a paused slice stopped at. A
// question needs an answer instead.
func (e *Engine) Retry(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.run.Snapshot()
	if rec.State != runrec.StatePaused || rec.Pending == nil {
		return errors.New("the slice is not paused")
	}
	for _, role := range rec.Roles {
		if role.Running() {
			return errors.New("a role is still running; reconcile it before retrying")
		}
	}
	if rec.Pending.Kind == runrec.PendingQuestion {
		return errors.New("the slice waits for an answer; answer it or stop the slice")
	}
	if err := e.run.Update(func(r *runrec.Record) { r.State = r.Pending.State; r.Reason = "" }); err != nil {
		return err
	}
	return e.advance(ctx)
}

// Stop stops a slice that is not finished and records why. It waits for a
// round that is running to end; cancel the context given to Run to interrupt
// one.
func (e *Engine) Stop(ctx context.Context, why string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state := e.run.Snapshot().State; runrec.IsFinal(state) {
		return fmt.Errorf("the slice is already %s", state)
	}
	return e.stop(ctx, "stopped by the owner: "+why)
}

// stop ends the slice: state stopped, the reason, and the ledger run ended
// with outcome stopped. The caller holds e.mu.
func (e *Engine) stop(ctx context.Context, reason string) error {
	err := e.run.Update(func(r *runrec.Record) {
		r.State = runrec.StateStopped
		r.Reason = reason
		r.Pending = nil
	})
	if err != nil {
		return err
	}
	e.endRelay(ctx, "stopped", "fail")
	return nil
}

// pause moves the slice to paused, waiting for the owner. The caller holds
// e.mu.
func (e *Engine) pause(p runrec.Pending) error {
	return e.run.Update(func(r *runrec.Record) {
		r.State = runrec.StatePaused
		r.Reason = p.Reason
		r.Pending = &p
	})
}

// takePending keeps the resume context durable until the round ends.
func (e *Engine) takePending() *runrec.Pending { return e.run.Snapshot().Pending }

// endRelay ends the slicer ledger run. Recording is advisory: a failed call
// is in writes[] and never stops the relay.
func (e *Engine) endRelay(ctx context.Context, outcome, closeSteps string) {
	rec := e.run.Snapshot()
	if rec.RelayRun == "" {
		return
	}
	e.d.Slicer.RelayEnd(ctx, e.wr, rec.Worktree, rec.RelayRun, outcome, closeSteps)
}

// Dequeue moves a queued run to setup, so Run can start it. It writes only the
// run record.
func (e *Engine) Dequeue() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state := e.run.Snapshot().State; state != runrec.StateQueued {
		return fmt.Errorf("the slice is %s, not queued", state)
	}
	return e.run.Update(func(r *runrec.Record) { r.State = runrec.StateSetup })
}
