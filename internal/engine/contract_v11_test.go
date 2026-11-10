// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// gateDone returns the `relay-run step` call that closed the gate's ledger
// step: the call that started the step with --role gate gives its id (the
// harness answers each step call with its own sequence number), and the call
// that updates that step to --phase done is the close.
func gateDone(t *testing.T, h *harness) string {
	t.Helper()
	calls := h.calls("slicer", "relay-run", "step")
	gateID := ""
	var closing []string
	for i, c := range calls {
		s := join(c)
		if strings.Contains(s, "--role gate ") {
			gateID = strconv.Itoa(i + 1)
			continue
		}
		if gateID != "" && strings.Contains(s, "--step "+gateID+" ") && strings.Contains(s, "--phase done") {
			closing = c
		}
	}
	if gateID == "" {
		t.Fatal("no gate step was started")
	}
	if closing == nil {
		t.Fatalf("gate step %s was never closed with --phase done", gateID)
	}
	return join(closing)
}

func TestV11GatePassClosesTheStepWithVerdictPass(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) { o.contract = 11 })
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if rec.Contract != 11 {
		t.Fatalf("recorded contract %d, want 11", rec.Contract)
	}
	if got := gateDone(t, h); !strings.Contains(got, "--phase done --verdict pass") {
		t.Fatalf("gate close: %s", got)
	}
	// Only the gate step carries a verdict.
	n := 0
	for _, c := range h.calls("slicer", "relay-run", "step") {
		if strings.Contains(join(c), "--verdict") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d step calls carry --verdict, want 1", n)
	}
}

func TestV11GateFailClosesTheStepWithVerdictFail(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) { o.contract = 11 })
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate && req.Round == 1 {
			return Result{Exit: ExitOK, Session: "sess-gate", Report: gateFail}, nil
		}
		return Result{}, nil
	}
	h.start()
	var verdicts []string
	for _, c := range h.calls("slicer", "relay-run", "step") {
		s := join(c)
		if i := strings.Index(s, "--verdict "); i >= 0 {
			verdicts = append(verdicts, strings.Fields(s[i:])[1])
		}
	}
	if strings.Join(verdicts, ",") != "fail,pass" {
		t.Fatalf("verdicts in order: %v, want fail then pass", verdicts)
	}
}

// A gate report with no valid first line has no verdict to send; the round
// pauses on the report's shape and the step closes without one.
func TestV11GateWithoutAVerdictLineClosesWithoutOne(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) { o.contract = 11 })
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate {
			return Result{Exit: ExitOK, Session: "sess-gate", Report: "no verdict here\n\nTests: 1 passed, 0 failed\n\n## Owner needed\n\nnone\n"}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	requireState(t, e, runrec.StatePaused)
	got := gateDone(t, h)
	if strings.Contains(got, "--verdict") {
		t.Fatalf("gate close carries a verdict: %s", got)
	}
}

func TestV10GateClosesWithoutAVerdictFlag(t *testing.T) {
	for _, pass := range []bool{true, false} {
		h := newHarness(t)
		h.runner.script = func(req Request, n int) (Result, error) {
			if req.Role == RoleGate && req.Round == 1 && !pass {
				return Result{Exit: ExitOK, Session: "sess-gate", Report: gateFail}, nil
			}
			return Result{}, nil
		}
		e := h.start()
		if rec := requireState(t, e, runrec.StateAwaitingApproval); rec.Contract != 10 {
			t.Fatalf("recorded contract %d, want 10", rec.Contract)
		}
		if got := gateDone(t, h); strings.Contains(got, "--verdict") {
			t.Fatalf("v10 gate close sends a verdict (pass=%v): %s", pass, got)
		}
		if ledgerHas(h, "--verdict") {
			t.Fatalf("v10 run sent --verdict (pass=%v)", pass)
		}
	}
}

func TestEngineStartWithV11CreatesARun(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) { o.contract = 11 })
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.Snapshot(); rec.Contract != 11 || rec.State != runrec.StateSetup {
		t.Fatalf("record: %+v", rec)
	}
}

// gateStepLog is one gate ledger step: its id (the harness answers each step
// call with its own sequence number) and the arguments of every call that
// closed it with --phase done.
type gateStepLog struct {
	id    string
	dones []string
}

// verdictCall is a `relay-run step` call that carries --verdict.
type verdictCall struct{ step, verdict string }

// gateLedger reads the gate steps and every verdict call out of the run's
// `relay-run step` calls.
func gateLedger(h *harness) ([]gateStepLog, []verdictCall) {
	var steps []gateStepLog
	var verdicts []verdictCall
	for i, c := range h.calls("slicer", "relay-run", "step") {
		s := join(c)
		if strings.Contains(s, "--role gate ") {
			steps = append(steps, gateStepLog{id: strconv.Itoa(i + 1)})
			continue
		}
		f := strings.Fields(s)
		var step, verdict string
		for j := 0; j < len(f)-1; j++ {
			switch f[j] {
			case "--step":
				step = f[j+1]
			case "--verdict":
				verdict = f[j+1]
			}
		}
		if verdict != "" {
			verdicts = append(verdicts, verdictCall{step, verdict})
		}
		if strings.Contains(s, "--phase done") {
			for k := range steps {
				if steps[k].id == step {
					steps[k].dones = append(steps[k].dones, s)
				}
			}
		}
	}
	return steps, verdicts
}

// gatePauseCase is a v11 run whose first gate report pauses the round: the
// gate says PASS, and something else in the report stops the engine accepting
// it. After resolve the gate runs again and passes.
type gatePauseCase struct {
	name    string
	report  string
	pending string
	resolve func(*Engine) error
}

var gatePauseCases = []gatePauseCase{
	{
		name:    "owner question",
		report:  withQuestion(gatePass),
		pending: runrec.PendingQuestion,
		resolve: func(e *Engine) error { return e.Answer(context.Background(), "8") },
	},
	{
		name:    "triage shape error",
		report:  "VERDICT: PASS\n\n- [owner] Which one?\nTests: 12 passed, 0 failed\n\n## Owner needed\n\nnone\n",
		pending: runrec.PendingShape,
		resolve: func(e *Engine) error { return e.Retry(context.Background()) },
	},
	{
		name:    "owner blocker finding",
		report:  "VERDICT: PASS\n\n" + blockerFinding + "\nTests: 12 passed, 0 failed\n\n## Owner needed\n\nnone\n",
		pending: runrec.PendingQuestion,
		resolve: func(e *Engine) error { return e.Answer(context.Background(), "keep") },
	},
}

// runGatePause runs the case to its pause, checks that the paused gate step
// closed without a verdict, resolves the pause and runs the slice to approval.
// The second gate passes.
func runGatePause(t *testing.T, c gatePauseCase, wantNoVerdict bool) (*harness, *Engine) {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) { o.contract = 11 })
	resolved := false
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate && !resolved {
			return Result{Exit: ExitOK, Session: "sess-gate", Report: c.report}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != c.pending || rec.Pending.Role != RoleGate {
		t.Fatalf("%s: pending %+v, want %s from the gate", c.name, rec.Pending, c.pending)
	}
	steps, verdicts := gateLedger(h)
	if len(steps) == 0 {
		t.Fatalf("%s: no gate step was started", c.name)
	}
	if wantNoVerdict && len(verdicts) != 0 {
		t.Fatalf("%s: a round the engine did not accept closed with a verdict: %v", c.name, verdicts)
	}
	resolved = true
	if err := c.resolve(e); err != nil {
		t.Fatalf("%s: %v", c.name, err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	return h, e
}

// The second gate is the accepted one: exactly one verdict in the run, pass,
// on the last gate step.
func requireOnlyTheAcceptedGateHasAVerdict(t *testing.T, h *harness) {
	t.Helper()
	steps, verdicts := gateLedger(h)
	if len(steps) < 2 {
		t.Fatalf("%d gate steps, want a new step for the second gate", len(steps))
	}
	if len(verdicts) != 1 || verdicts[0].verdict != "pass" || verdicts[0].step != steps[len(steps)-1].id {
		t.Fatalf("verdicts %v, want one pass on the last gate step %s", verdicts, steps[len(steps)-1].id)
	}
}

func TestV11GateOwnerNeededClosesTheStepWithoutAVerdict(t *testing.T) {
	h, _ := runGatePause(t, gatePauseCases[0], true)
	requireOnlyTheAcceptedGateHasAVerdict(t, h)
}

func TestV11GateTriageShapeErrorClosesTheStepWithoutAVerdict(t *testing.T) {
	h, _ := runGatePause(t, gatePauseCases[1], true)
	requireOnlyTheAcceptedGateHasAVerdict(t, h)
}

func TestV11GateBlockerFindingClosesTheStepWithoutAVerdict(t *testing.T) {
	h, _ := runGatePause(t, gatePauseCases[2], true)
	requireOnlyTheAcceptedGateHasAVerdict(t, h)
}

// No exit path closes a gate step twice or leaves it open.
func TestV11GateStepCloseIsSingle(t *testing.T) {
	check := func(t *testing.T, h *harness) {
		t.Helper()
		steps, _ := gateLedger(h)
		if len(steps) == 0 {
			t.Fatal("no gate step")
		}
		for _, s := range steps {
			if len(s.dones) != 1 {
				t.Errorf("gate step %s has %d --phase done calls, want 1: %q", s.id, len(s.dones), s.dones)
			}
		}
	}
	for _, c := range gatePauseCases {
		t.Run(c.name, func(t *testing.T) {
			h, _ := runGatePause(t, c, false)
			check(t, h)
		})
	}
	t.Run("accepted pass", func(t *testing.T) {
		h := newHarness(t, func(o *harnessOpts) { o.contract = 11 })
		requireState(t, h.start(), runrec.StateAwaitingApproval)
		check(t, h)
	})
}

func TestV11GateStatusReadFailureClosesTheStepWithoutAVerdict(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.contract = 11
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{{Tool: "git", Args: []string{"-C", W, "status", "--porcelain"}, Seq: []clitest.Reply{{}, {}, {}, {Exit: 1, Stderr: "status failed"}}}}
		}
	})
	rec := requireState(t, h.start(), runrec.StatePaused)
	if !strings.Contains(rec.Reason, "read-only fingerprint") {
		t.Errorf("pause reason: %s", rec.Reason)
	}
	if got := gateDone(t, h); strings.Contains(got, "--verdict") {
		t.Errorf("gate close carries verdict: %s", got)
	}
	if n := len(h.calls("git", "-C", h.W, "status", "--porcelain")); n != 4 {
		t.Errorf("status calls = %d, want 4", n)
	}
}
