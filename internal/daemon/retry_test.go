// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const retryReport = "Tests: 1 passed, 0 failed\n\n## Owner needed\nnone\n"

func retryRunner(_ context.Context, r engine.Request) (engine.Result, error) {
	report := retryReport
	if r.Role == engine.RoleReviewer {
		report = "(A): nothing\n(B): nothing\n(D): nothing\n\n## Owner needed\nnone\n"
	}
	if r.Role == engine.RoleGate {
		report = "VERDICT: PASS\n" + report
	}
	return engine.Result{Exit: engine.ExitOK, Report: report}, nil
}

func retryDaemon(t *testing.T, state string, pending *runrec.Pending) (*Daemon, *runrec.Store) {
	t.Helper()
	d, store, _ := retryDaemonWith(t, state, pending, retryRunner, nil)
	return d, store
}

// retryDaemonWith is retryDaemon with its own runner, and a change to the
// record before the engine loads it.
func retryDaemonWith(t *testing.T, state string, pending *runrec.Pending, runner roundRunner, change func(*runrec.Record)) (*Daemon, *runrec.Store, registry.Project) {
	t.Helper()
	d, store, p := runDaemon(t, runner)
	lineup := map[string][]runrec.LineupEntry{}
	for _, role := range []string{engine.RoleImplementer, engine.RoleReviewer, engine.RoleGate} {
		lineup[role] = []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet"}}
	}
	rec := runrec.Record{
		ID: "r1", Project: p.Name, Item: "S1", Worktree: filepath.Join(p.Path, ".worktrees", "S1"),
		State: state, Round: 1, Lineup: lineup, Pending: pending,
	}
	if change != nil {
		change(&rec)
	}
	r, err := store.Create(rec)
	if err != nil {
		t.Fatal(err)
	}
	d.runs.deps.Check = func(context.Context, string, string, time.Duration) (engine.CheckOutput, error) {
		return engine.CheckOutput{OK: true, Output: "retry checks\n"}, nil
	}
	d.runs.runs["r1"] = &managedRun{engine: engine.Load(d.runDeps(p), p, r)}
	return d, store, p
}

func TestRunRetryResumesPausedRole(t *testing.T) {
	for _, kind := range []string{runrec.PendingShape, runrec.PendingError, runrec.PendingLost, runrec.PendingInterrupted} {
		t.Run(kind, func(t *testing.T) {
			d, store := retryDaemon(t, runrec.StatePaused, &runrec.Pending{Kind: kind, State: runrec.StateImplement, Role: engine.RoleImplementer, Round: 1, Reason: "bad report"})
			b := request(t, d, "run.retry", `{"run":"r1"}`)
			var rec runrec.Record
			if err := json.Unmarshal(b, &rec); err != nil {
				t.Fatal(err)
			}
			if rec.State != runrec.StateAwaitingApproval {
				t.Fatalf("retry did not leave paused for approval: %s", b)
			}
			report, err := os.ReadFile(filepath.Join(store.RunDir("r1"), "implementer-1.md"))
			if err != nil || string(report) != retryReport {
				t.Fatalf("retried implementer report = %q, err = %v", report, err)
			}
		})
	}
}

func TestRunRetryCarriesTheText(t *testing.T) {
	for _, tc := range []struct{ params, note string }{
		{`{"run":"r1","text":"re-read testdata"}`, "## Owner note\n\nre-read testdata\n"},
		{`{"run":"r1"}`, ""},
	} {
		var mu sync.Mutex
		var briefs []string
		runner := func(ctx context.Context, r engine.Request) (engine.Result, error) {
			mu.Lock()
			briefs = append(briefs, r.Brief)
			mu.Unlock()
			return retryRunner(ctx, r)
		}
		d, _, _ := retryDaemonWith(t, runrec.StatePaused, &runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateImplement, Role: engine.RoleImplementer, Round: 1, Reason: "bad report"}, runner, nil)
		request(t, d, "run.retry", tc.params)
		mu.Lock()
		if len(briefs) == 0 {
			t.Fatalf("%s: the role did not run again", tc.params)
		}
		if got := strings.Contains(briefs[0], "## Owner note"); got != (tc.note != "") || !strings.Contains(briefs[0], tc.note) {
			t.Errorf("%s: owner note in the rerun brief %v, want %q:\n%s", tc.params, got, tc.note, briefs[0])
		}
		mu.Unlock()
	}
}

// ownerWithoutOptions is a reviewer report whose one owner finding has no
// options: a shape problem that ork retry --accept-shape can keep.
const ownerWithoutOptions = "(A): nothing\n(B): nothing\n(D):\n- [owner] Which format?\n  Needs a call from the owner.\n\n## Owner needed\nnone\n"

// cappedShapeDaemon is a run paused at its second shape pause: the reviewer's
// refused report is in the run directory. calls counts the reviewer's reruns.
func cappedShapeDaemon(t *testing.T, report string, calls *int) *Daemon {
	t.Helper()
	var mu sync.Mutex
	runner := func(ctx context.Context, r engine.Request) (engine.Result, error) {
		if r.Role == engine.RoleReviewer {
			mu.Lock()
			*calls++
			mu.Unlock()
			return engine.Result{Exit: engine.ExitOK, Report: report}, nil
		}
		return retryRunner(ctx, r)
	}
	pending := &runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateReview, Role: engine.RoleReviewer, Round: 1, Reason: "report shape: x", ShapePauses: runrec.ShapePauseCap}
	at := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	d, store, _ := retryDaemonWith(t, runrec.StatePaused, pending, runner, func(r *runrec.Record) {
		r.Roles = []runrec.RoleRecord{{Role: engine.RoleReviewer, Round: 1, Vendor: "claude", Model: "sonnet", Exit: string(engine.ExitOK), Report: "reviewer-1.md", StartedAt: at, EndedAt: at.Add(time.Minute)}}
	})
	if err := os.WriteFile(filepath.Join(store.RunDir("r1"), "reviewer-1.md"), []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRunRetryCarriesAcceptShape(t *testing.T) {
	calls := 0
	d := cappedShapeDaemon(t, ownerWithoutOptions, &calls)
	b := request(t, d, "run.retry", `{"run":"r1","accept_shape":true}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("accepting ran the reviewer %d times", calls)
	}
	if len(rec.Findings) != 1 || rec.Findings[0].Kind != runrec.FindingOwner || rec.Findings[0].Ask != nil {
		t.Fatalf("findings %+v, want the owner finding kept without an ask: %s", rec.Findings, b)
	}
	if rec.State != runrec.StateAwaitingApproval {
		t.Errorf("state %s, want the run to go on to approval", rec.State)
	}
}

func TestPlainRetryAtTheShapeCapIsRefused(t *testing.T) {
	calls := 0
	d := cappedShapeDaemon(t, ownerWithoutOptions, &calls)
	_, err := d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
	var wire *ipc.Error
	if !errors.As(err, &wire) || wire.Code != CodeRefused {
		t.Fatalf("err %v, want a refusal", err)
	}
	for _, want := range []string{"ork show r1", "ork retry r1 --accept-shape", "ork retry r1 --again", "ork stop r1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q lacks %q", err, want)
		}
	}
	if calls != 0 {
		t.Errorf("a refused retry ran the reviewer %d times", calls)
	}
	if rec := d.runs.runs["r1"].engine.Snapshot(); rec.State != runrec.StatePaused {
		t.Errorf("state %s, want paused", rec.State)
	}
}

func TestRetryAgainAtTheShapeCapRerunsTheRole(t *testing.T) {
	calls := 0
	// The rerun fails the check again: it pauses at the cap again.
	d := cappedShapeDaemon(t, "(A): nothing\n", &calls)
	b := request(t, d, "run.retry", `{"run":"r1","again":true}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("reviewer reruns = %d, want 1", calls)
	}
	if rec.State != runrec.StatePaused || rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape || rec.Pending.ShapePauses != runrec.ShapePauseCap+1 {
		t.Fatalf("pending %+v in state %s, want a capped shape pause again", rec.Pending, rec.State)
	}
	if _, err := d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`)); err == nil || !strings.Contains(err.Error(), "--again") {
		t.Errorf("a plain retry after --again: %v, want the refusal", err)
	}
}

func TestStatusShowsTheCappedShapePauseAndItsNextStep(t *testing.T) {
	calls := 0
	d := cappedShapeDaemon(t, ownerWithoutOptions, &calls)
	b := request(t, d, "status", `{"project":"p"}`)
	want := "next: ork retry r1 --accept-shape, ork retry r1 --again or ork stop r1"
	if !strings.Contains(string(b), want) {
		t.Fatalf("status lacks %q: %s", want, b)
	}
}

func TestRunRetryResumesPausedStep(t *testing.T) {
	for _, kind := range []string{runrec.PendingError, runrec.PendingInterrupted} {
		t.Run(kind, func(t *testing.T) {
			d, store := retryDaemon(t, runrec.StatePaused, &runrec.Pending{Kind: kind, State: runrec.StateChecks, Reason: "check failed"})
			b := request(t, d, "run.retry", `{"run":"r1"}`)
			var rec runrec.Record
			if err := json.Unmarshal(b, &rec); err != nil {
				t.Fatal(err)
			}
			if rec.State != runrec.StateAwaitingApproval {
				t.Fatalf("step retry did not reach approval: %s", b)
			}
			log, err := os.ReadFile(filepath.Join(store.RunDir("r1"), "checks-1.log"))
			if err != nil || !strings.Contains(string(log), "retry checks") {
				t.Fatalf("retried check log = %q, err = %v", log, err)
			}
		})
	}
}

func TestRunRetryGrantsRoundsAtTheRoundPause(t *testing.T) {
	d, _, _ := retryDaemonWith(t, runrec.StatePaused, &runrec.Pending{Kind: "rounds", State: runrec.StateImplement}, retryRunner, func(r *runrec.Record) { r.Round = 3 })
	b := request(t, d, "run.retry", `{"run":"r1","rounds":4}`)
	var r runrec.Record
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	if r.Round != 4 || r.RoundLimit != 7 || r.State != runrec.StateAwaitingApproval {
		t.Fatalf("grant not carried: %s", b)
	}
}

func TestStatusShowsTheRoundPauseAndItsNextStep(t *testing.T) {
	d, _ := retryDaemon(t, runrec.StatePaused, &runrec.Pending{Kind: "rounds", State: runrec.StateImplement, Reason: "checks failed; grant more rounds, change the check, or stop"})
	b := request(t, d, "status", `{"project":"p"}`)
	for _, want := range []string{`"pending":"rounds"`, "checks failed; grant more rounds, change the check, or stop; next: ork retry r1 [--rounds N] or ork stop r1"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("status lacks %q: %s", want, b)
		}
	}
}

func TestRunRetryUnknownRun(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	_, err := d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"missing"}`))
	var wire *ipc.Error
	if !errors.As(err, &wire) || wire.Code != CodeNotFound || !strings.Contains(wire.Message, "missing") {
		t.Fatalf("retry unknown run: %v", err)
	}
}

func TestRunRetryWrongState(t *testing.T) {
	for _, state := range []string{runrec.StateImplement, runrec.StateAwaitingApproval} {
		t.Run(state, func(t *testing.T) {
			d, _ := retryDaemon(t, state, &runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateImplement})
			_, err := d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
			if err == nil || !strings.Contains(err.Error(), "not paused") || !strings.Contains(err.Error(), "ork stop r1") {
				t.Fatalf("retry wrong state needs stop guidance: %v", err)
			}
		})
	}
}

func TestRunRetryQuestionNeedsAnswer(t *testing.T) {
	d, _ := retryDaemon(t, runrec.StatePaused, &runrec.Pending{Kind: runrec.PendingQuestion, State: runrec.StateImplement, Question: "Which library?"})
	_, err := d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
	if err == nil || !strings.Contains(err.Error(), "ork answer r1 TEXT") || !strings.Contains(err.Error(), "ork stop r1") {
		t.Fatalf("retry question needs answer or stop guidance: %v", err)
	}
}

func TestRunRetryBusyNamesStop(t *testing.T) {
	d, _ := retryDaemon(t, runrec.StateImplement, nil)
	d.runs.runs["r1"].busy = true
	_, err := d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
	if err == nil || !strings.Contains(err.Error(), "busy") || !strings.Contains(err.Error(), "ork stop r1") {
		t.Fatalf("retry busy run needs stop guidance: %v", err)
	}
}

func TestRunAnswerWithoutQuestionNamesRetry(t *testing.T) {
	for _, pending := range []*runrec.Pending{nil, {Kind: runrec.PendingShape, State: runrec.StateImplement}} {
		name := "no-pending"
		if pending != nil {
			name = pending.Kind
		}
		t.Run(name, func(t *testing.T) {
			d, _ := retryDaemon(t, runrec.StatePaused, pending)
			_, err := d.handle(context.Background(), "run.answer", json.RawMessage(`{"run":"r1","text":"try again"}`))
			if err == nil || !strings.Contains(err.Error(), "no pending question") || !strings.Contains(err.Error(), "ork retry r1") {
				t.Fatalf("answer without question needs retry guidance: %v", err)
			}
		})
	}
}

func TestPausedStatusNamesNextCommand(t *testing.T) {
	for _, tc := range []struct{ kind, want string }{
		{runrec.PendingShape, "bad report; next: ork retry r1"},
		{runrec.PendingError, "bad report; next: ork retry r1"},
		{runrec.PendingLost, "bad report; next: ork retry r1"},
		{runrec.PendingInterrupted, "bad report; next: ork retry r1"},
		{runrec.PendingQuestion, "Which library?; next: ork answer r1 TEXT"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			pending := &runrec.Pending{Kind: tc.kind, State: runrec.StateImplement, Reason: "bad report", Question: "Which library?"}
			d, _ := retryDaemon(t, runrec.StatePaused, pending)
			b := request(t, d, "status", `{"project":"p"}`)
			if !strings.Contains(string(b), `"wait":"`+tc.want+`"`) {
				t.Fatalf("paused status lacks reason and next command %q: %s", tc.want, b)
			}
		})
	}
}

// roleGate holds each role round until the test opens it, or the job's context
// ends. started has one token per round that began.
type roleGate struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *roleGate) open() { g.once.Do(func() { close(g.release) }) }

func (g *roleGate) run(ctx context.Context, r engine.Request) (engine.Result, error) {
	g.started <- struct{}{}
	select {
	case <-g.release:
	case <-ctx.Done():
		return engine.Result{}, ctx.Err()
	}
	return retryRunner(ctx, r)
}

var askedQuestion = &runrec.Pending{Kind: runrec.PendingQuestion, State: runrec.StateImplement, Role: engine.RoleImplementer, Round: 1, Question: "Which library?"}
var lostPause = &runrec.Pending{Kind: runrec.PendingLost, State: runrec.StateImplement, Role: engine.RoleImplementer, Round: 1, Reason: "lost"}

// gatedRetryDaemon is retryDaemon with a runner that blocks until the test
// opens the gate; the gate opens at the end of the test, too.
func gatedRetryDaemon(t *testing.T, pending *runrec.Pending) (*Daemon, *runrec.Store, *roleGate) {
	t.Helper()
	g := &roleGate{started: make(chan struct{}, 8), release: make(chan struct{})}
	d, store, _ := retryDaemonWith(t, runrec.StatePaused, pending, g.run, nil)
	t.Cleanup(g.open)
	return d, store, g
}

type ownerReply struct {
	runrec.Record
	Message string `json:"message"`
}

type callResult struct {
	rep ownerReply
	err error
}

// startCall makes an owner call in the background. It never touches the test,
// so the caller reports every failure on the test goroutine.
func startCall(d *Daemon, method, raw string) <-chan callResult {
	ch := make(chan callResult, 1)
	go func() {
		var res callResult
		v, err := d.handle(context.Background(), method, json.RawMessage(raw))
		res.err = err
		if err == nil {
			b, merr := json.Marshal(v)
			if merr == nil {
				merr = json.Unmarshal(b, &res.rep)
			}
			res.err = merr
		}
		ch <- res
	}()
	return ch
}

// callWithin makes an owner call and fails the test when it does not return
// within the time.
func callWithin(t *testing.T, d *Daemon, method, raw string, within time.Duration) (ownerReply, error) {
	t.Helper()
	select {
	case res := <-startCall(d, method, raw):
		return res.rep, res.err
	case <-time.After(within):
		t.Fatalf("%s did not return within %s", method, within)
	}
	return ownerReply{}, nil
}

func (g *roleGate) wait(t *testing.T) {
	t.Helper()
	select {
	case <-g.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the role did not start")
	}
}

func busyNow(d *Daemon, id string) bool {
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	return d.runs.runs[id].busy
}

func TestRunAnswerNoWaitReturnsBeforeTheRoleEnds(t *testing.T) {
	d, store, g := gatedRetryDaemon(t, askedQuestion)
	rep, err := callWithin(t, d, "run.answer", `{"run":"r1","text":"8","no_wait":true}`, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rep.State != runrec.StateImplement {
		t.Fatalf("early reply state %q, want implement", rep.State)
	}
	g.wait(t)
	if !busyNow(d, "r1") {
		t.Error("the run is not busy while its role runs")
	}
	g.open()
	waitState(t, store, "r1", runrec.StateAwaitingApproval)
	waitIdle(t, d, "r1")
}

func TestRunRetryNoWaitReturnsBeforeTheRoleEnds(t *testing.T) {
	d, store, g := gatedRetryDaemon(t, lostPause)
	rep, err := callWithin(t, d, "run.retry", `{"run":"r1","no_wait":true}`, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rep.State != runrec.StateImplement {
		t.Fatalf("early reply state %q, want implement", rep.State)
	}
	g.wait(t)
	if !busyNow(d, "r1") {
		t.Error("the run is not busy while its role runs")
	}
	g.open()
	waitState(t, store, "r1", runrec.StateAwaitingApproval)
	waitIdle(t, d, "r1")
}

// requireWaitsUntilRest makes the call, checks that it has not returned while
// the role runs, then lets the role end and checks the reply. Every failure is
// reported on the test goroutine, so a failing call fails the test and does not
// hang it.
func requireWaitsUntilRest(t *testing.T, d *Daemon, g *roleGate, method, raw string) {
	t.Helper()
	ch := startCall(d, method, raw)
	g.wait(t)
	select {
	case res := <-ch:
		t.Fatalf("%s %s returned while the role ran: %+v, %v", method, raw, res.rep.State, res.err)
	case <-time.After(100 * time.Millisecond):
	}
	g.open()
	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("%s %s: %v", method, raw, res.err)
		}
		if res.rep.State != runrec.StateAwaitingApproval {
			t.Errorf("%s %s: reply state %q, want awaiting-approval", method, raw, res.rep.State)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s %s did not return after the role ended", method, raw)
	}
}

func TestRunAnswerWithoutNoWaitStillWaitsUntilTheRunRests(t *testing.T) {
	for _, raw := range []string{`{"run":"r1","text":"8"}`, `{"run":"r1","text":"8","no_wait":false}`} {
		d, _, g := gatedRetryDaemon(t, askedQuestion)
		requireWaitsUntilRest(t, d, g, "run.answer", raw)
	}
}

func TestRunRetryWithoutNoWaitStillWaitsUntilTheRunRests(t *testing.T) {
	d, _, g := gatedRetryDaemon(t, lostPause)
	requireWaitsUntilRest(t, d, g, "run.retry", `{"run":"r1"}`)
}

func TestRunNoWaitRefusalsAreUnchanged(t *testing.T) {
	type tc struct {
		name, method, pendingKind, want string
		busy                            bool
		roleRunning                     bool
	}
	for _, c := range []tc{
		{name: "answer without a question", method: "run.answer", pendingKind: runrec.PendingShape, want: "ork retry r1"},
		{name: "retry of a question", method: "run.retry", pendingKind: runrec.PendingQuestion, want: "ork answer r1 TEXT"},
		{name: "answer of a busy run", method: "run.answer", pendingKind: runrec.PendingQuestion, busy: true, want: "is busy"},
		{name: "retry of a busy run", method: "run.retry", pendingKind: runrec.PendingLost, busy: true, want: "is busy"},
		{name: "retry with a role running", method: "run.retry", pendingKind: runrec.PendingLost, roleRunning: true, want: "still running"},
	} {
		t.Run(c.name, func(t *testing.T) {
			pending := &runrec.Pending{Kind: c.pendingKind, State: runrec.StateImplement, Role: engine.RoleImplementer, Round: 1, Question: "Which library?"}
			change := func(rec *runrec.Record) {
				if c.roleRunning {
					rec.Roles = []runrec.RoleRecord{{Role: engine.RoleImplementer, Round: 1, Vendor: "claude", StartedAt: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}}
				}
			}
			var got [2]*ipc.Error
			var msgs [2]string
			for i, raw := range []string{`{"run":"r1","text":"x"}`, `{"run":"r1","text":"x","no_wait":true}`} {
				d, _, _ := retryDaemonWith(t, runrec.StatePaused, pending, retryRunner, change)
				d.runs.runs["r1"].busy = c.busy
				_, err := callWithin(t, d, c.method, raw, 2*time.Second)
				if err == nil || !strings.Contains(err.Error(), c.want) {
					t.Fatalf("%s: %v, want text %q", raw, err, c.want)
				}
				got[i], msgs[i] = ipc.ErrorOf(err), err.Error()
			}
			if msgs[0] != msgs[1] || got[0].Code != got[1].Code {
				t.Errorf("no_wait changed the refusal: %q (%s) vs %q (%s)", msgs[0], got[0].Code, msgs[1], got[1].Code)
			}
			if c.roleRunning && got[1].Code != CodeRefused {
				t.Errorf("engine refusal has code %q, want %q", got[1].Code, CodeRefused)
			}
		})
	}
}

func TestSecondAnswerAfterAnEarlyReturnIsBusy(t *testing.T) {
	d, store, g := gatedRetryDaemon(t, askedQuestion)
	if _, err := callWithin(t, d, "run.answer", `{"run":"r1","text":"8","no_wait":true}`, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	g.wait(t)
	_, err := callWithin(t, d, "run.answer", `{"run":"r1","text":"again","no_wait":true}`, 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "is busy") {
		t.Fatalf("second answer: %v, want busy", err)
	}
	rep, err := callWithin(t, d, "run.stop", `{"run":"r1"}`, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rep.State != runrec.StateStopped {
		t.Fatalf("stop left state %q", rep.State)
	}
	waitState(t, store, "r1", runrec.StateStopped)
	waitIdle(t, d, "r1")
	stored, err := store.Open("r1")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range stored.Snapshot().Roles {
		if role.Running() {
			t.Errorf("a role is still recorded as running after the stop: %+v", role)
		}
	}
}

func TestNoWaitJobFailureAppearsInStatus(t *testing.T) {
	d, store, g := gatedRetryDaemon(t, askedQuestion)
	if _, err := callWithin(t, d, "run.answer", `{"run":"r1","text":"8","no_wait":true}`, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	g.wait(t)
	// The run record can no longer be written: the job fails after the reply.
	// The record comes back afterwards, so that status can list the run.
	dir := store.RunDir("r1")
	saved, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	g.open()
	waitIdle(t, d, "r1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.json"), saved, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := wireRunError(t, d.engineRuns("p")[0]); got == "" {
		t.Error("the job's failure is not on the run row")
	}
}

func TestOwnerActionOnARelayRunIDNamesTheOrkRun(t *testing.T) {
	d, _, _ := retryDaemonWith(t, runrec.StatePaused, askedQuestion, retryRunner, func(rec *runrec.Record) { rec.RelayRun = "run-20261103-abc" })
	for _, method := range []string{"run.answer", "run.retry"} {
		_, err := d.handle(context.Background(), method, json.RawMessage(`{"run":"run-20261103-abc","text":"x"}`))
		wire := ipc.ErrorOf(err)
		if wire == nil || wire.Code != CodeNotFound || wire.Message != "no run run-20261103-abc; that is the slicer run of ork run r1" {
			t.Errorf("%s with the relay run id: %v", method, err)
		}
		_, err = d.handle(context.Background(), method, json.RawMessage(`{"run":"unknown","text":"x"}`))
		wire = ipc.ErrorOf(err)
		if wire == nil || wire.Code != CodeNotFound || wire.Message != "no run unknown" {
			t.Errorf("%s with an unknown id: %v", method, err)
		}
	}
}
