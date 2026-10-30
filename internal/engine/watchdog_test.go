// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// The watchdog for a hung role (S182). A role that stays silent for longer
// than Deps.HungAfter is killed by ending its context, and the run pauses as
// lost for the owner. These tests use a fake runner that blocks until its
// context ends and a limit of a few milliseconds, so none of them waits for
// 30 minutes or starts a vendor binary.

const (
	// testHungAfter is long enough for several 20ms events and short enough
	// that a silent role is killed within a fraction of a second.
	testHungAfter = 250 * time.Millisecond
	// testEventEvery is how often a lively fake role emits.
	testEventEvery = 20 * time.Millisecond
	// safetyStop ends a fake role that nothing killed, so a missing watchdog
	// fails a test instead of hanging it.
	safetyStop = 3 * time.Second
)

// hangRunner answers the roles in silent with script, and every other role
// with the harness's default report. It records a fake process group for each
// role, as the vendor adapters do through OnStart.
type hangRunner struct {
	mu     sync.Mutex
	reqs   []Request
	silent func(Request) bool
	script func(ctx context.Context, req Request) (Result, error)
}

func (r *hangRunner) Run(ctx context.Context, req Request) (Result, error) {
	r.mu.Lock()
	n := len(r.reqs)
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	if req.OnStart != nil {
		req.OnStart(4000 + n)
	}
	if r.silent != nil && r.silent(req) {
		return r.script(ctx, req)
	}
	return defaultResult(req), nil
}

func (r *hangRunner) requests() []Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Request(nil), r.reqs...)
}

// blockUntilKilled is a role that emits nothing and waits for its context, as
// a hung vendor process does until its group is killed. ended is closed when
// the context ends.
func blockUntilKilled(ended chan struct{}) func(context.Context, Request) (Result, error) {
	var once sync.Once
	return func(ctx context.Context, req Request) (Result, error) {
		select {
		case <-ctx.Done():
			once.Do(func() { close(ended) })
			return Result{}, ctx.Err()
		case <-time.After(safetyStop):
			return Result{}, errors.New("test safety stop: the watchdog never ended the context")
		}
	}
}

func onlyRole(roles ...string) func(Request) bool {
	return func(req Request) bool {
		for _, r := range roles {
			if req.Role == r {
				return true
			}
		}
		return false
	}
}

// hungHarness is a harness whose clock is the real one, because the watchdog
// compares the engine's clock with the limit and the harness's fake clock
// moves a second on every call.
func hungHarness(t *testing.T, limit time.Duration, hr *hangRunner) *harness {
	t.Helper()
	h := newHarness(t)
	h.deps.Now = time.Now
	h.deps.HungAfter = limit
	h.deps.Runner = hr
	return h
}

func TestSilentRoleIsKilledAfterTheLimitAndTheRunPausesAsLost(t *testing.T) {
	ended := make(chan struct{})
	hr := &hangRunner{silent: onlyRole(RoleImplementer), script: blockUntilKilled(ended)}
	h := hungHarness(t, 50*time.Millisecond, hr)
	e := h.start()
	select {
	case <-ended:
	default:
		t.Fatal("the runner's context did not end")
	}
	rec := requireState(t, e, runrec.StatePaused)
	p := rec.Pending
	if p == nil || p.Kind != runrec.PendingLost || p.Role != RoleImplementer || p.Round != 1 || p.State != runrec.StateImplement {
		t.Fatalf("pending: %+v", p)
	}
	for _, want := range []string{"implementer", "round 1", "hung", "50ms", "process group 4000"} {
		if !strings.Contains(p.Reason, want) {
			t.Errorf("reason %q lacks %q", p.Reason, want)
		}
	}
	if rec.Reason != p.Reason {
		t.Errorf("run reason %q, pending reason %q", rec.Reason, p.Reason)
	}
	if len(rec.Roles) != 1 {
		t.Fatalf("roles: %+v", rec.Roles)
	}
	role := rec.Roles[0]
	if role.Exit != "hung" || role.Running() || role.EndedAt.IsZero() || role.PGID != 4000 {
		t.Errorf("role record: %+v", role)
	}
	if got := len(hr.requests()); got != 1 {
		t.Errorf("rounds run: %d", got)
	}
}

// liveRunner calls emit every testEventEvery for 3 * testHungAfter, which is
// longer than the limit, and then returns a good report. A role that emits is
// never cancelled.
func liveRunner(emit func(Request), cancelled *atomic.Bool) func(context.Context, Request) (Result, error) {
	return func(ctx context.Context, req Request) (Result, error) {
		deadline := time.Now().Add(3 * testHungAfter)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				cancelled.Store(true)
				return Result{}, ctx.Err()
			case <-time.After(testEventEvery):
			}
			emit(req)
		}
		return defaultResult(req), nil
	}
}

func TestEventsKeepARoleAlive(t *testing.T) {
	var cancelled atomic.Bool
	hr := &hangRunner{silent: onlyRole(RoleImplementer), script: liveRunner(func(req Request) { req.OnEvent() }, &cancelled)}
	h := hungHarness(t, testHungAfter, hr)
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if cancelled.Load() {
		t.Error("a role that emits events was cancelled")
	}
	if rec.Roles[0].Exit != string(ExitOK) {
		t.Errorf("implementer exit %q", rec.Roles[0].Exit)
	}
}

func TestLiveTextCountsAsAnEvent(t *testing.T) {
	var cancelled atomic.Bool
	hr := &hangRunner{silent: onlyRole(RoleImplementer), script: liveRunner(func(req Request) { req.OnLive(Live{Text: "thinking"}) }, &cancelled)}
	h := hungHarness(t, testHungAfter, hr)
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if cancelled.Load() {
		t.Error("a role that emits live text was cancelled")
	}
	if rec.Roles[0].Exit != string(ExitOK) {
		t.Errorf("implementer exit %q", rec.Roles[0].Exit)
	}
}

func TestHungRoleDoesNotFallBackOrMarkAVendorOut(t *testing.T) {
	hr := &hangRunner{silent: onlyRole(RoleImplementer), script: blockUntilKilled(make(chan struct{}))}
	h := hungHarness(t, 50*time.Millisecond, hr)
	h.cfg.Lineup[RoleImplementer] = []runrec.LineupEntry{
		{Vendor: "claude", Model: "sonnet", Effort: "high"},
		{Vendor: "codex", Model: "gpt", Effort: "high"},
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if got := hr.requests(); len(got) != 1 || got[0].Vendor != "claude" {
		t.Fatalf("rounds run: %+v", got)
	}
	if got := currentEntry(rec, RoleImplementer); got != 0 {
		t.Errorf("the chain advanced to row %d", got)
	}
	for _, role := range rec.Roles {
		if role.Exit == string(ExitOutOfCredits) || role.Exit == string(ExitRateLimited) || role.Exit == string(ExitError) {
			t.Errorf("a hang was recorded as a limit: %+v", role)
		}
	}
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingLost {
		t.Errorf("pending: %+v", rec.Pending)
	}
}

func TestRetryAfterAHungRoleRunsTheSameRowWithTheLostBrief(t *testing.T) {
	var silent atomic.Bool
	silent.Store(true)
	hr := &hangRunner{silent: func(req Request) bool { return req.Role == RoleImplementer && silent.Load() }, script: blockUntilKilled(make(chan struct{}))}
	h := hungHarness(t, 50*time.Millisecond, hr)
	h.cfg.Lineup[RoleImplementer] = []runrec.LineupEntry{
		{Vendor: "claude", Model: "sonnet", Effort: "high"},
		{Vendor: "codex", Model: "gpt", Effort: "high"},
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	reason := rec.Pending.Reason
	if currentEntry(rec, RoleImplementer) != 0 {
		t.Fatalf("the chain advanced after a hang")
	}
	silent.Store(false)
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	reqs := hr.requests()
	if len(reqs) < 2 {
		t.Fatalf("rounds run: %d", len(reqs))
	}
	first, second := reqs[0], reqs[1]
	if second.Role != RoleImplementer || second.Vendor != first.Vendor || second.Model != first.Model || second.Effort != first.Effort {
		t.Errorf("retry ran %s %s %s %s, want the same row", second.Role, second.Vendor, second.Model, second.Effort)
	}
	if !strings.Contains(second.Brief, "Earlier process lost") || !strings.Contains(second.Brief, reason) {
		t.Errorf("retry brief lacks the lost section with the hung reason %q", reason)
	}
	if !strings.Contains(second.Brief, "hung") {
		t.Errorf("retry brief does not say hung")
	}
	final := e.Snapshot()
	if got := currentEntry(final, RoleImplementer); got != 0 {
		t.Errorf("the row advanced to %d after the retry", got)
	}
	if final.State != runrec.StateAwaitingApproval {
		t.Errorf("state %q (reason %q) after the retry", final.State, final.Reason)
	}
}

func TestHungRoleEndsItsLedgerStepAsFailAndStopsTheHeartbeat(t *testing.T) {
	hr := &hangRunner{silent: onlyRole(RoleImplementer), script: blockUntilKilled(make(chan struct{}))}
	h, e := heartbeatEngine(t)
	e.d.Now = time.Now
	e.d.HungAfter = 200 * time.Millisecond
	e.d.Runner = hr
	// Hold the fail call open, so a heartbeat that outlives the kill is seen.
	e.wr.Sink = heartbeatEndSink{e.run}
	out, err := e.runRound(context.Background(), round{role: RoleImplementer, n: 1, state: runrec.StateImplement, task: "S04 implementer"}, nil)
	if err != nil || out.ok {
		t.Fatalf("round: ok=%v err=%v", out.ok, err)
	}
	requireState(t, e, runrec.StatePaused)
	time.Sleep(3 * testHeartbeatEvery)
	var fails, starts, startsAfterFail int
	failed := false
	for _, s := range h.steps() {
		switch {
		case s.Phase == "fail":
			fails++
			failed = true
		case s.Phase == "start" && s.Step != "" && s.Task == "":
			starts++
			if failed {
				startsAfterFail++
			}
		}
	}
	if fails != 1 {
		t.Errorf("fail step calls: %d, want 1 (%+v)", fails, h.steps())
	}
	if starts == 0 {
		t.Error("no heartbeat ran while the role was silent; the test proves nothing")
	}
	if startsAfterFail != 0 {
		t.Errorf("%d heartbeats after the kill", startsAfterFail)
	}
	for _, s := range h.steps() {
		if s.Phase == "done" {
			t.Errorf("a hung role's step ended done: %+v", s)
		}
	}
}

func TestOwnerStopDuringARoleIsCancelledNotHung(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit time.Duration
		// waitForKill makes the owner's stop arrive after the watchdog fired.
		waitForKill bool
	}{
		{"before the limit", time.Hour, false},
		{"as the watchdog fires", 30 * time.Millisecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e *Engine
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hr := &hangRunner{silent: onlyRole(RoleImplementer)}
			hr.script = func(rctx context.Context, req Request) (Result, error) {
				if tc.waitForKill {
					select {
					case <-rctx.Done(): // the watchdog ended the role's context
					case <-time.After(safetyStop):
						t.Error("the watchdog never ended the context")
					}
				}
				if err := e.stop(context.Background(), "stopped by the owner: test"); err != nil {
					t.Error(err)
				}
				cancel()
				return Result{}, ctx.Err()
			}
			h := hungHarness(t, tc.limit, hr)
			var err error
			e, err = New(context.Background(), h.deps, h.cfg)
			if err != nil {
				t.Fatal(err)
			}
			_, err = e.runRound(ctx, round{role: RoleImplementer, n: 1, state: runrec.StateImplement, task: "S04 implementer"}, nil)
			if err != nil {
				t.Fatalf("round: %v", err)
			}
			rec := requireState(t, e, runrec.StateStopped)
			if rec.Pending != nil {
				t.Errorf("a stopped run has no pending: %+v", rec.Pending)
			}
			if len(rec.Roles) != 1 || rec.Roles[0].Exit != "cancelled" || rec.Roles[0].Running() {
				t.Errorf("role record: %+v", rec.Roles)
			}
		})
	}
}

func TestNegativeHungAfterTurnsTheWatchdogOff(t *testing.T) {
	// The role is silent for longer than the smallest limit a mistaken parse
	// of -1 could give, then ends by itself.
	var cancelled atomic.Bool
	hr := &hangRunner{silent: onlyRole(RoleImplementer), script: func(ctx context.Context, req Request) (Result, error) {
		select {
		case <-ctx.Done():
			cancelled.Store(true)
			return Result{}, ctx.Err()
		case <-time.After(300 * time.Millisecond):
			return defaultResult(req), nil
		}
	}}
	h := hungHarness(t, -1, hr)
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if cancelled.Load() || rec.Roles[0].Exit != string(ExitOK) {
		t.Errorf("a silent role was killed with the watchdog off: %+v", rec.Roles[0])
	}
	if _, on := (Deps{HungAfter: -1}).hungLimit(); on {
		t.Error("a negative limit leaves the watchdog on")
	}
}

func TestZeroHungAfterMeansThirtyMinutes(t *testing.T) {
	if DefaultHungAfter != 30*time.Minute {
		t.Fatalf("default %s, want 30m", DefaultHungAfter)
	}
	got, on := (Deps{}).hungLimit()
	if !on || got != 30*time.Minute {
		t.Errorf("zero HungAfter gives %s on=%v", got, on)
	}
	got, on = (Deps{HungAfter: 5 * time.Second}).hungLimit()
	if !on || got != 5*time.Second {
		t.Errorf("explicit HungAfter gives %s on=%v", got, on)
	}
	if DefaultHungAfter < 3*heartbeatInterval {
		t.Errorf("the limit %s must outlast the %s heartbeat", DefaultHungAfter, heartbeatInterval)
	}
}

func TestTwoHungSiblingRoundsPauseOnce(t *testing.T) {
	hr := &hangRunner{silent: onlyRole(RoleReviewer, RoleUI), script: blockUntilKilled(make(chan struct{}))}
	h := uiHarness(t, tuiFiles)
	h.deps.Now = time.Now
	h.deps.HungAfter = 50 * time.Millisecond
	h.deps.Runner = hr
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingLost || (rec.Pending.Role != RoleReviewer && rec.Pending.Role != RoleUI) {
		t.Fatalf("pending: %+v", rec.Pending)
	}
	if len(rec.QueuedQuestions) != 0 {
		t.Errorf("a second pause was queued: %+v", rec.QueuedQuestions)
	}
	hung := map[string]bool{}
	for _, role := range rec.Roles {
		if role.Role == RoleImplementer {
			continue
		}
		if role.Exit != "hung" || role.Running() || role.EndedAt.IsZero() {
			t.Errorf("role record: %+v", role)
		}
		hung[role.Role] = true
	}
	if !hung[RoleReviewer] || !hung[RoleUI] {
		t.Errorf("hung roles: %v", hung)
	}
	if !strings.Contains(rec.Reason, "hung") {
		t.Errorf("reason %q", rec.Reason)
	}
}
