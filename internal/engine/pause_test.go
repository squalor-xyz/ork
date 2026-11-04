// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const question = "Should the cap be 4 or 8?\nOptions: 4, 8.\nI recommend 4."

func withQuestion(report string) string {
	return strings.Replace(report, "## Owner needed\n\nnone\n", "## Owner needed\n\n"+question+"\n", 1)
}

func TestANonEmptyOwnerNeededPausesTheSliceAndAnAnswerResumesTheRole(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if n == 0 {
			return Result{Exit: ExitOK, Session: "sess-implementer", Report: withQuestion(implReport)}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	p := rec.Pending
	if p == nil || p.Kind != runrec.PendingQuestion || p.Role != RoleImplementer || p.Round != 1 || p.State != runrec.StateImplement || p.Question != question {
		t.Fatalf("pending: %+v", p)
	}
	if len(rec.Questions) != 1 || rec.Questions[0].Text != question || rec.Questions[0].Role != RoleImplementer || rec.Questions[0].At.IsZero() || rec.Questions[0].Answer != "" {
		t.Fatalf("questions: %+v", rec.Questions)
	}
	if len(h.checks.calls) != 0 || len(h.runner.requests()) != 1 {
		t.Fatalf("a paused slice runs nothing: checks %v, roles %v", h.checks.calls, h.runner.roles())
	}
	if st := h.steps(); st[len(st)-1].Phase != "wait" {
		t.Errorf("orchestrator waits after a role asked a question: %+v", st[len(st)-1])
	}
	if len(h.calls("slicer", "relay-run", "end")) != 0 {
		t.Errorf("a paused slice does not end its run")
	}

	if err := e.Answer(context.Background(), "  8  "); err != nil {
		t.Fatal(err)
	}
	rec = requireState(t, e, runrec.StateAwaitingApproval)
	if rec.Pending != nil {
		t.Errorf("pending kept: %+v", rec.Pending)
	}
	if rec.Questions[0].Answer != "8" || rec.Questions[0].AnsweredAt.IsZero() {
		t.Errorf("answer not recorded: %+v", rec.Questions[0])
	}
	resumed := h.runner.requests()[1]
	if resumed.Role != RoleImplementer || resumed.Round != 1 || resumed.Resume != "sess-implementer" {
		t.Errorf("the role must be resumed with its session: %+v", resumed)
	}
	if !strings.Contains(resumed.Brief, "Should the cap be 4 or 8?") || !strings.Contains(resumed.Brief, "\n8\n") {
		t.Errorf("the brief must carry the question and the answer:\n%s", resumed.Brief)
	}
}

func TestOwnerQuestionsFromReviewerAndGateResumeThatRole(t *testing.T) {
	for role, stateWant := range map[string]string{RoleReviewer: runrec.StateReview, RoleGate: runrec.StateGate} {
		h := newHarness(t)
		asked := false
		h.runner.script = func(req Request, n int) (Result, error) {
			if req.Role == role && !asked {
				asked = true
				res := defaultResult(req)
				res.Report = withQuestion(res.Report)
				return res, nil
			}
			return Result{}, nil
		}
		e := h.start()
		rec := requireState(t, e, runrec.StatePaused)
		if rec.Pending.Role != role || rec.Pending.State != stateWant {
			t.Fatalf("%s: pending %+v", role, rec.Pending)
		}
		if err := e.Answer(context.Background(), "4"); err != nil {
			t.Fatal(err)
		}
		requireState(t, e, runrec.StateAwaitingApproval)
		var again Request
		for _, r := range h.runner.requests() {
			if r.Role == role {
				again = r
			}
		}
		if again.Resume == "" || !strings.Contains(again.Brief, "\n4\n") || !again.ReadOnly {
			t.Errorf("%s: resumed request %+v", role, again)
		}
	}
}

func TestAnswerNeedsAPausedQuestionAndAnAnswer(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	if err := e.Answer(context.Background(), "x"); err == nil {
		t.Error("Answer on a slice that awaits approval was accepted")
	}
	h = newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		return Result{Exit: ExitOK, Session: "s", Report: withQuestion(implReport)}, nil
	}
	e = h.start()
	if err := e.Answer(context.Background(), "   "); err == nil {
		t.Error("an empty answer was accepted")
	}
	requireState(t, e, runrec.StatePaused)
	if err := e.Retry(context.Background()); err == nil {
		t.Error("Retry on a question was accepted; the owner must answer or stop")
	}
	if err := e.Stop(context.Background(), "not worth it"); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateStopped)
	if !strings.Contains(rec.Reason, "not worth it") || rec.Pending != nil {
		t.Errorf("stopped: %+v", rec)
	}
	if err := e.Stop(context.Background(), "again"); err == nil {
		t.Error("Stop on a stopped slice was accepted")
	}
}

func TestAReportThatBreaksTheShapePausesWithTheProblemsAndRetryResumesTheRole(t *testing.T) {
	cases := map[string]struct {
		role   string
		report string
		want   string
	}{
		"gate first line": {RoleGate, "Looks good.\nTests: 1 passed, 0 failed\n\n## Owner needed\nnone\n", "VERDICT"},
		"no tests line":   {RoleImplementer, "Done.\n\n## Owner needed\n\nnone\n", "Tests:"},
		"no owner needed": {RoleReviewer, review("nothing", "nothing", "nothing", "nothing")[:strings.Index(review("nothing", "nothing", "nothing", "nothing"), "## Owner needed")], "Owner needed"},
		"no sections":     {RoleReviewer, "Looks fine.\n\n## Owner needed\n\nnone\n", "section (A)"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			bad := 2
			h.runner.script = func(req Request, n int) (Result, error) {
				if req.Role == tc.role && bad > 0 {
					bad--
					return Result{Exit: ExitOK, Session: "sess-" + req.Role, Report: tc.report}, nil
				}
				return Result{}, nil
			}
			e := h.start()
			rec := requireState(t, e, runrec.StatePaused)
			p := rec.Pending
			if p == nil || p.Kind != runrec.PendingShape || p.Role != tc.role || !strings.Contains(p.Reason, tc.want) {
				t.Fatalf("pending: %+v", p)
			}
			if len(rec.Questions) != 0 {
				t.Errorf("a shape error is not an owner question")
			}
			if err := e.Retry(context.Background()); err != nil {
				t.Fatal(err)
			}
			requireState(t, e, runrec.StateAwaitingApproval)
			var again Request
			for _, r := range h.runner.requests() {
				if r.Role == tc.role {
					again = r
				}
			}
			if again.Resume != "sess-"+tc.role || !strings.Contains(again.Brief, tc.want) {
				t.Errorf("the retry must resume the role and tell it what was wrong: resume %q\n%.600s", again.Resume, again.Brief)
			}
		})
	}
}

func TestAToolErrorPausesAndRetryRunsTheStateAgain(t *testing.T) {
	h := newHarness(t)
	h.checks.err = context.DeadlineExceeded
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingError || rec.Pending.State != runrec.StateChecks || !strings.Contains(rec.Pending.Reason, "check") {
		t.Fatalf("pending: %+v", rec.Pending)
	}
	h.checks.err = nil
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	if got := h.runner.roles(); len(got) != 3 {
		t.Errorf("the implementer must not run again: %v", got)
	}
}

func TestAWorktreeStatusFailurePausesBeforeTheReadOnlyRole(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{{Tool: "git", Args: []string{"-C", W, "status", "--porcelain"},
				Seq: []clitest.Reply{{Stderr: "fatal: not a git repository\n", Exit: 128}, {}}}}
		}
	})
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending.Kind != runrec.PendingError || rec.Pending.State != runrec.StateReview {
		t.Fatalf("pending: %+v", rec.Pending)
	}
	if got := h.runner.roles(); len(got) != 1 {
		t.Fatalf("a read-only role must not run when the tree cannot be fingerprinted: %v", got)
	}
}

func TestStructuredOwnerQuestionPersistsAndResumesTheRole(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if n == 0 {
			return Result{Exit: ExitOK, Session: "structured-session", Report: strings.Replace(implReport, "none", structuredQuestion, 1)}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending.Ask == nil || rec.Pending.Ask.Title != "Which storage?" || rec.Questions[0].Ask == nil || rec.Questions[0].Ask.Options[1].Label != "remote" {
		t.Fatalf("structured question lost: %+v", rec)
	}
	if err := e.Answer(context.Background(), "local: use local storage"); err != nil {
		t.Fatal(err)
	}
	rec = requireState(t, e, runrec.StateAwaitingApproval)
	req := h.runner.requests()[1]
	if req.Role != RoleImplementer || req.Resume != "structured-session" || !strings.Contains(req.Brief, "local: use local storage") || rec.Questions[0].Answer != "local: use local storage" {
		t.Fatalf("resume: %+v", req)
	}
}

func TestFreeTextOwnerNeededStillPausesTheRun(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(Request, int) (Result, error) { return Result{Exit: ExitOK, Report: withQuestion(implReport)}, nil }
	rec := requireState(t, h.start(), runrec.StatePaused)
	if rec.Pending.Kind != runrec.PendingQuestion || rec.Pending.Question != question || rec.Pending.Ask != nil {
		t.Fatalf("free text: %+v", rec.Pending)
	}
}

// gatedRunner makes the harness runner block each round until release closes.
// returned counts the rounds that came back.
type gatedRunner struct {
	release  chan struct{}
	returned atomic.Int32
}

// gate lets first answer a round (a result with an exit class) or pass; every
// round first leaves to the default, and blocks until the test releases it.
func gate(h *harness, first func(req Request, n int) (Result, error)) *gatedRunner {
	g := &gatedRunner{release: make(chan struct{})}
	h.runner.script = func(req Request, n int) (Result, error) {
		if first != nil {
			if res, err := first(req, n); res.Exit != "" || err != nil {
				return res, err
			}
		}
		<-g.release
		g.returned.Add(1)
		return Result{}, nil
	}
	return g
}

// hookProbe is an applied hook that records what the run looked like when it
// fired, and how often it fired.
type hookProbe struct {
	calls    atomic.Int32
	fired    chan struct{}
	snapshot runrec.Record
	returned int32
	g        *gatedRunner
	e        *Engine
}

func newProbe(e *Engine, g *gatedRunner) *hookProbe {
	return &hookProbe{fired: make(chan struct{}, 4), g: g, e: e}
}

func (p *hookProbe) hook() {
	p.calls.Add(1)
	p.snapshot = p.e.Snapshot()
	if p.g != nil {
		p.returned = p.g.returned.Load()
	}
	p.fired <- struct{}{}
}

func waitFired(t *testing.T, p *hookProbe) {
	t.Helper()
	select {
	case <-p.fired:
	case <-time.After(5 * time.Second):
		t.Fatal("the applied hook was not called")
	}
}

func TestAnswerCallsTheAppliedHookBeforeTheRoleRuns(t *testing.T) {
	h := newHarness(t)
	g := gate(h, func(req Request, n int) (Result, error) {
		if n == 0 {
			return Result{Exit: ExitOK, Session: "sess-implementer", Report: withQuestion(implReport)}, nil
		}
		return Result{}, nil
	})
	e := h.start()
	requireState(t, e, runrec.StatePaused)
	p := newProbe(e, g)
	done := make(chan error, 1)
	go func() { done <- e.Answer(WithApplied(context.Background(), p.hook), "8") }()
	waitFired(t, p)
	if p.snapshot.State != runrec.StateImplement {
		t.Errorf("state at the hook %q, want implement", p.snapshot.State)
	}
	if len(p.snapshot.Questions) != 1 || p.snapshot.Questions[0].Answer != "8" {
		t.Errorf("answer not recorded at the hook: %+v", p.snapshot.Questions)
	}
	if p.returned != 0 {
		t.Errorf("%d rounds had returned when the hook fired", p.returned)
	}
	select {
	case err := <-done:
		t.Fatalf("Answer returned before the role was released: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(g.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	if got := p.calls.Load(); got != 1 {
		t.Errorf("hook called %d times, want 1", got)
	}
}

func TestAppliedHookIsNotCalledOnARefusal(t *testing.T) {
	var calls atomic.Int32
	ctx := WithApplied(context.Background(), func() { calls.Add(1) })

	h := newHarness(t)
	e := h.start() // awaiting approval: no question
	if err := e.Answer(ctx, "x"); !IsRefusal(err) {
		t.Errorf("answer with no question: %v", err)
	}
	if err := e.Retry(ctx); !IsRefusal(err) {
		t.Errorf("retry of a run that is not paused: %v", err)
	}

	h = newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		return Result{Exit: ExitOK, Session: "s", Report: withQuestion(implReport)}, nil
	}
	e = h.start()
	requireState(t, e, runrec.StatePaused)
	if err := e.Answer(ctx, "   "); !IsRefusal(err) {
		t.Errorf("empty answer: %v", err)
	}
	if err := e.Retry(ctx); !IsRefusal(err) {
		t.Errorf("retry of a question pause: %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("hook called %d times on refusals", got)
	}
}

func TestAnswerWithoutAHookBehavesAsBefore(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if n == 0 {
			return Result{Exit: ExitOK, Session: "s", Report: withQuestion(implReport)}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	if err := e.Answer(context.Background(), "8"); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
}

func TestRetryCallsTheAppliedHookBeforeTheRoleRuns(t *testing.T) {
	h := newHarness(t)
	h.seed("run-a", runrec.StateImplement, runningRole(RoleImplementer, 111, "2"))
	engines := h.recover()
	e := engines["run-a"]
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingLost {
		t.Fatalf("pending %+v, want a lost pause", rec.Pending)
	}
	g := gate(h, nil)
	p := newProbe(e, g)
	done := make(chan error, 1)
	go func() { done <- e.Retry(WithApplied(context.Background(), p.hook)) }()
	waitFired(t, p)
	if p.snapshot.State != runrec.StateImplement {
		t.Errorf("state at the hook %q, want implement", p.snapshot.State)
	}
	if p.returned != 0 {
		t.Errorf("%d rounds had returned when the hook fired", p.returned)
	}
	select {
	case err := <-done:
		t.Fatalf("Retry returned before the role was released: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(g.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := p.calls.Load(); got != 1 {
		t.Errorf("hook called %d times, want 1", got)
	}
}
