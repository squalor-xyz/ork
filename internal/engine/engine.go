// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
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
	Adj            adjcli.Client
	Runner         Runner
	Slicer         slicercli.Client
	GitBin         string // empty means "git"
	Store          *runrec.Store
	Check          CheckFunc // nil means RunCheck
	Proc           Proc      // nil means the operating system's
	Now            func() time.Time
	Sleep          func(ctx context.Context, d time.Duration) error // nil sleeps for real
	Backoff        []time.Duration                                  // locked-retry delays; nil means runrec.DefaultBackoff
	RandHex        func() string                                    // four hex digits for run ids; nil is random
	HeartbeatEvery time.Duration                                    // 0 means heartbeatInterval
	HungAfter      time.Duration                                    // 0 means DefaultHungAfter; negative turns the hung-role watchdog off
	MaxRounds      int                                              // 0 means DefaultMaxRounds
	Landings       *Landings                                        // one landing per project at a time; nil means one shared by the process
	Vendors        *Vendors                                         // the per-vendor slot cap and vendor-out memory (S169); nil means no cap and no memory
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
	StopAfter string
	Project   registry.Project
	Item      string
	Lineup    map[string][]runrec.LineupEntry // by role; the gate defaults to the reviewer's
	Budgets   registry.Budgets
	Wave      string
	Queued    bool // create the run in state queued; Dequeue moves it to setup
	// Decisions is the "Owner decisions" paragraph that a spec check recorded
	// in the main checkout's spec of the item (S116). The run keeps it and
	// gives it to every role, because its worktree lacks the edit.
	Decisions string
}

// Engine runs one slice. The methods that move it (Run, Approve, Reject,
// Answer, Retry, Stop, Reconcile) run one at a time; Snapshot may be called
// at any moment.
type Engine struct {
	activity       sync.Map // role name -> *activityRing
	heartbeatEvery time.Duration
	mu             sync.Mutex
	fileMu         sync.Mutex // one filing of a later finding at a time
	d              Deps
	proj           registry.Project
	run            *runrec.Run
	wr             runrec.Writer
	noted          string      // the checks note written for an approval (guarded by mu)
	vendorWait     vendorWaits // the vendors whose slot a round waits for (S169); own lock, because Run holds mu
}

func newEngine(d Deps, proj registry.Project, run *runrec.Run) *Engine {
	return &Engine{d: d, heartbeatEvery: d.HeartbeatEvery, proj: proj, run: run, wr: runrec.Writer{Sink: run, Now: d.now, Sleep: d.Sleep, Backoff: d.Backoff}}
}

// worktreePath is the project's worktree pattern with its item placeholders filled, relative
// to the project directory unless it is absolute (git.worktrees).
func worktreePath(p registry.Project, id string) string {
	pattern := p.WorktreePattern
	if pattern == "" {
		pattern = ".worktrees/{id}"
	}
	pattern = registry.FillPattern(pattern, id)
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
	return registry.FillPattern(pattern, id)
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

// currentEntry derives the sticky row from the role history. A failed row
// advances the chain; a successful row stays selected for later rounds.
// A gate with no history follows the reviewer's current row when its lineup
// is empty or a copy of the reviewer's. Its own records take precedence.
// If its first record matches the reviewer seed, walk from that seed so a
// repeated entry cannot send the gate back through earlier failed rows.
// Walking forward through records distinguishes identical lineup entries.
func currentEntry(rec runrec.Record, role string) int {
	entries := rec.Lineup[role]
	cur := 0
	if role == RoleGate && (len(entries) == 0 || slices.Equal(entries, rec.Lineup[RoleReviewer])) {
		entries = rec.Lineup[RoleReviewer]
		seed := currentEntry(rec, RoleReviewer)
		firstGate := slices.IndexFunc(rec.Roles, func(record runrec.RoleRecord) bool { return record.Role == RoleGate })
		if firstGate < 0 {
			cur = seed
		} else if seed < len(entries) {
			first, entry := rec.Roles[firstGate], entries[seed]
			if first.Vendor == entry.Vendor && first.Model == entry.Model && first.Effort == entry.Effort {
				cur = seed
			}
		}
	}
	exhausted := map[string]bool{}
	for _, record := range rec.Roles {
		if record.Role != role {
			continue
		}
		if record.Exit == string(ExitOutOfCredits) {
			exhausted[record.Vendor] = true
		}
		matched := false
		for j := cur; j < len(entries); j++ {
			entry := entries[j]
			if record.Vendor == entry.Vendor && record.Model == entry.Model && record.Effort == entry.Effort {
				cur = j
				if fallbackExit(ExitClass(record.Exit)) {
					cur = nextEntry(entries, j, ExitClass(record.Exit))
				}
				matched = true
				break
			}
		}
		// Lost attempts may lack row identity; retry them at the current row.
		if !matched && record.Exit != "lost" {
			// Other incompatible history exhausts the chain instead of replaying it.
			return len(entries)
		}
	}
	for cur < len(entries) && exhausted[entries[cur].Vendor] {
		cur++
	}
	return cur
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
	roleList := []string{RoleImplementer, RoleReviewer, RoleGate}
	if _, ok := lineup(cfg.Lineup, RoleUI); ok {
		roleList = append(roleList, RoleUI)
	}
	for _, role := range roleList {
		if _, err := ResolvePermission(cfg.Project, role); err != nil {
			return nil, err
		}
	}
	line, err := d.Slicer.Contract(ctx, cfg.Project.Path)
	if err != nil {
		return nil, fmt.Errorf("slicer ai relay: %w", err)
	}
	version, err := roles.CheckContractLine(line)
	if err != nil {
		return nil, err
	}
	contract, err := d.Slicer.RelayContract(ctx, cfg.Project.Path)
	if err != nil {
		return nil, fmt.Errorf("slicer ai relay --json: %w", err)
	}
	if contract.Contract != fmt.Sprintf("relay v%d", version) {
		return nil, errors.New("slicer contract changed during setup")
	}
	ledgerRoles := map[string]string{}
	for _, role := range append(roles.Names(), "orchestrator") {
		ledgerRoles[role] = contract.LedgerRole(role)
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
		ID: id, StopAfter: cfg.StopAfter, Wave: cfg.Wave, Project: cfg.Project.Name, Item: cfg.Item,
		Worktree: worktreePath(cfg.Project, cfg.Item), Branch: branchName(cfg.Project, cfg.Item),
		Contract: version, GateCriteria: contract.Gate.Criteria, LedgerRoles: ledgerRoles, State: state, Round: 1, Lineup: copied, Budgets: budgets, Decisions: cfg.Decisions,
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

// Dismiss records the owner's dismissal on the managed run handle.
func (e *Engine) Dismiss() error { return e.run.Dismiss() }

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
	defer e.ownerWait(context.WithoutCancel(ctx))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rec := e.run.Snapshot()
		if !resting(rec.State) && len(rec.QueuedQuestions) > 0 {
			return e.run.Update(func(r *runrec.Record) {
				p := r.QueuedQuestions[0]
				r.QueuedQuestions = r.QueuedQuestions[1:]
				if r.Pending != nil && p.Retry == nil {
					p.Retry = r.Pending.Retry // a retry context waits through every queued question
				}
				r.Pending = &p
				r.State = runrec.StatePaused
				r.Reason = p.Reason
			})
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
		case runrec.StateArchitect:
			err = e.architect(ctx)
		case runrec.StateGate:
			err = e.gate(ctx)
		case runrec.StateCommit:
			err = e.commit(ctx)
		case runrec.StateFinish:
			err = e.finish(ctx)
		case runrec.StateLand:
			err = e.land(ctx)
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
	rec := e.run.Snapshot()
	if rec.State != runrec.StateAwaitingApproval {
		return refusef("the slice is %s, not awaiting approval", rec.State)
	}
	if open := openChecks(rec.Card); len(open) > 0 {
		noun := "checks"
		if len(open) == 1 {
			noun = "check"
		}
		return refusef("%d human %s still open; tick each or mark it later: %s", len(open), noun, strings.Join(open, "; "))
	}
	// The owner's marks are recorded before the finish step closes the slice;
	// when the card could not read the checks, the note says so (S141). A failed
	// note refuses the approval: the owner approves again. A second approval,
	// after a landing fix round, finds the note already written; so does one
	// after the approval record could not be saved (e.noted).
	if note := checksNote(rec.Item, rec.Card); note != "" && len(rec.Approvals) == 0 && note != e.noted {
		if err := e.writeChecksNote(ctx, rec, note); err != nil {
			return fmt.Errorf("record the human checks as a slicer note: %w", err)
		}
	}
	err := e.run.Update(func(r *runrec.Record) {
		r.Approvals = append(r.Approvals, runrec.Approval{What: "slice", At: e.d.now()})
		r.State = runrec.StateFinish
	})
	if err != nil {
		return err
	}
	e.orchestratorPhase(ctx, "start", "")
	return e.advance(ctx)
}

// Reject stops a slice that awaits approval.
func (e *Engine) Reject(ctx context.Context, why string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state := e.run.Snapshot().State; state != runrec.StateAwaitingApproval {
		return refusef("the slice is %s, not awaiting approval", state)
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
		return refusef("the slice has no owner question to answer")
	}
	if text == "" {
		return refusef("the answer is empty")
	}
	err := e.run.Update(func(r *runrec.Record) {
		for i := len(r.Questions) - 1; i >= 0; i-- {
			if r.Questions[i].Answer == "" && r.Questions[i].Role == r.Pending.Role && r.Questions[i].Round == r.Pending.Round && r.Questions[i].Text == r.Pending.Question {
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
	if rec.Pending.LedgerQuestion != "" {
		e.d.Slicer.RelayAnswer(ctx, e.wr, rec.Worktree, rec.RelayRun, rec.Pending.LedgerQuestion, slicercli.RelayAnswerSpec{Text: text})
	}
	e.orchestratorPhase(ctx, "start", "")
	return e.advance(ctx)
}

// Retry runs again the role, or the step, that a paused slice stopped at. A
// question needs an answer instead.
func (e *Engine) Retry(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.run.Snapshot()
	if rec.State != runrec.StatePaused || rec.Pending == nil {
		return refusef("the slice is not paused")
	}
	for _, role := range rec.Roles {
		if role.Running() {
			return refusef("a role is still running; reconcile it before retrying")
		}
	}
	if rec.Pending.Kind == runrec.PendingQuestion {
		return refusef("the slice waits for an answer; answer it or stop the slice")
	}
	if rec.Pending.Retry == nil && len(rec.QueuedQuestions) > 0 {
		// A sibling's owner question is asked before the role runs again. The
		// pause that is retried rides on the question, so the rerun still has
		// its context (report shape or lost process section; a question or
		// shape pause also resumes the session) after the answer (S159).
		defer e.ownerWait(ctx)
		return e.run.Update(func(r *runrec.Record) {
			q := r.QueuedQuestions[0]
			r.QueuedQuestions = r.QueuedQuestions[1:]
			retry := *r.Pending
			q.Retry = &retry
			r.Pending = &q
			r.Reason = q.Reason
		})
	}
	if err := e.run.Update(func(r *runrec.Record) { r.State = r.Pending.State; r.Reason = "" }); err != nil {
		return err
	}
	e.orchestratorPhase(ctx, "start", "")
	return e.advance(ctx)
}

// Stop stops a slice that is not finished and records why. It waits for a
// round that is running to end; cancel the context given to Run to interrupt
// one.
func (e *Engine) Stop(ctx context.Context, why string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state := e.run.Snapshot().State; runrec.IsFinal(state) {
		return refusef("the slice is already %s", state)
	}
	return e.stop(ctx, "stopped by the owner: "+why)
}

// stop ends the slice: state stopped, the reason, and the ledger run ended
// with outcome stopped. The caller holds e.mu.
//
// Two roles of one state run at the same time (the reviewer and the UI pass),
// and both may ask to stop: the first stop is kept.
func (e *Engine) stop(ctx context.Context, reason string) error {
	already, ended := false, false
	err := e.run.Update(func(r *runrec.Record) {
		if already = r.State == runrec.StateStopped; already {
			return
		}
		ended = r.Commit != nil // the finish step closed the ledger run with PASS
		r.State = runrec.StateStopped
		r.Reason = reason
		r.Pending = nil
		r.QueuedQuestions = nil
	})
	if err != nil || already {
		return err
	}
	if !ended {
		// A sibling can cancel the review context after the stop is recorded.
		// The ledger end must finish; each CLI call still has its own timeout.
		e.endRelay(context.WithoutCancel(ctx), "stopped", "fail")
	}
	return nil
}

// pause moves the slice to paused, waiting for the owner. The caller holds
// e.mu. A slice that is already paused or stopped keeps its first pause or
// stop: two roles of one state (the reviewer and the UI pass) may both ask.
// A sibling owner question is persisted in the queue until its turn.
// stop() is the stronger of the two: it refuses only when already stopped.
func (e *Engine) pause(p runrec.Pending) error {
	return e.run.Update(func(r *runrec.Record) {
		if r.State == runrec.StateStopped {
			return
		}
		if r.State == runrec.StatePaused {
			if p.Kind == runrec.PendingQuestion {
				r.QueuedQuestions = append(r.QueuedQuestions, p)
			}
			return
		}
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

// StopQueued stops a queued run and records the reason as given.
func (e *Engine) StopQueued(ctx context.Context, reason string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state := e.run.Snapshot().State; state != runrec.StateQueued {
		return fmt.Errorf("the slice is %s, not queued", state)
	}
	return e.stop(ctx, reason)
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
