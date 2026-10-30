// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strconv"
	"strings"
	"testing"

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
