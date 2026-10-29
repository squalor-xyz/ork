// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"github.com/squalor-xyz/ork/internal/clitest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

func ledgerHas(h *harness, words ...string) bool {
	for _, c := range h.calls("slicer", "relay-run") {
		s := join(c)
		yes := true
		for _, w := range words {
			yes = yes && strings.Contains(s, w)
		}
		if yes {
			return true
		}
	}
	return false
}

func TestUIPassStepRecordsRoleUI(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	if !ledgerHas(h, "step", "--role ui ") {
		t.Fatal("UI round was not recorded with --role ui")
	}
}

func TestAwaitingApprovalRecordsApprovalWait(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	if !ledgerHas(h, "--step "+e.Snapshot().OrchestratorStep+" ", "--phase wait", "--waiting-on approval") {
		t.Fatal("no orchestrator approval wait")
	}
}

func TestApproveRecordsStartAgain(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	before := len(h.steps())
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := h.steps()[before:]
	if len(got) == 0 || got[0].Step != e.Snapshot().OrchestratorStep || got[0].Phase != "start" {
		t.Fatalf("approval did not end wait first: %+v", got)
	}
}

func TestOwnerQuestionPauseRecordsOwnerWaitAndAsk(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if n == 0 {
			return Result{Exit: ExitOK, Report: strings.Replace(implReport, "## Owner needed\n\nnone\n", "## Owner needed\n\nQuestion: Which cap?\nOptions:\n1. four: cap at 4\n2. eight: cap at 8\nRecommend: 1 - smaller\n", 1)}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	requireState(t, e, runrec.StatePaused)
	if !ledgerHas(h, "--phase wait", "--waiting-on owner") {
		t.Error("no owner wait")
	}
	if !ledgerHas(h, "relay-run ask", "--step "+e.Snapshot().OrchestratorStep) {
		t.Error("no ledger question")
	}
	if err := e.Answer(context.Background(), "8"); err != nil {
		t.Fatal(err)
	}
	if !ledgerHas(h, "relay-run answer", "--text 8") {
		t.Error("no ledger answer")
	}
}

func TestSpecCheckRecordsLedgerRunWithRoleSpecCheck(t *testing.T) {
	h := specHarness(t)
	_, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
	if err != nil {
		t.Fatal(err)
	}
	if !ledgerHas(h, "relay-run begin S10 S11") {
		t.Error("no batch ledger run")
	}
	var found bool
	for _, s := range h.steps() {
		if s.Role == RoleSpecCheck && s.Phase == "start" {
			found = true
			if s.Parent != "" {
				t.Error("spec check has parent")
			}
		}
	}
	if !found {
		t.Error("no spec-check step")
	}
	if !ledgerHas(h, "relay-run end", "--outcome pass") {
		t.Error("no pass end")
	}
}

func TestVendorLimitFallbackRecordsFailReasonOnV10(t *testing.T) {
	for _, version := range []int{9, 10} {
		h := fallbackHarness(t, RoleImplementer)
		h.runner.script = func(req Request, _ int) (Result, error) {
			if req.Role == RoleImplementer && req.Vendor == "codex" {
				return Result{Exit: ExitRateLimited, Reason: "rate_limited: vendor message"}, nil
			}
			return Result{}, nil
		}
		e, err := New(context.Background(), h.deps, h.cfg)
		if err != nil {
			t.Fatal(err)
		}
		e.run.Update(func(r *runrec.Record) { r.Contract = version })
		if err := e.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := ledgerHas(h, "--phase fail", "--reason rate_limited: vendor message")
		if got != (version == 10) {
			t.Errorf("v%d: fail reason present=%v", version, got)
		}
	}
}

func TestOutOfCreditsSkipsSameVendorRows(t *testing.T) {
	checkLimitRows(t, ExitClass("out_of_credits"), 2)
}
func TestRateLimitTakesTheNextRow(t *testing.T) { checkLimitRows(t, ExitRateLimited, 3) }
func checkLimitRows(t *testing.T, class ExitClass, want int) {
	t.Helper()
	h := fallbackHarness(t, RoleImplementer)
	h.cfg.Lineup[RoleImplementer] = []runrec.LineupEntry{
		{Vendor: "codex", Model: "sol", Effort: "high"},
		{Vendor: "codex", Model: "sol", Effort: "medium"},
		{Vendor: "claude", Model: "sonnet", Effort: "medium"},
	}
	h.runner.exits = map[string]ExitClass{"codex": class}
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	var count int
	for _, r := range h.runner.requests() {
		if r.Role == RoleImplementer {
			count++
		}
	}
	if count != want {
		t.Fatalf("%s ran %d rows, want %d", class, count, want)
	}
}

func TestGateCriteriaComeFromTheContractJSON(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cli", "slicer", "relay-v10.json"))
			if err != nil {
				t.Fatal(err)
			}
			data := strings.Replace(string(raw), `"docs matching the code"`, `"the contract supplied criterion"`, 1)
			return []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: data}}
		}
	})
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Load uses the recorded contract data, without a second contract read.
	e = Load(h.deps, h.proj, e.run)
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range h.runner.requests() {
		if r.Role == RoleGate {
			found = true
			if !strings.Contains(r.Brief, "the contract supplied criterion") {
				t.Fatal("gate ignored JSON criteria")
			}
		}
	}
	if !found {
		t.Fatal("gate never ran")
	}
}

func TestFallbackSwitchIsOnTheRunCard(t *testing.T) {
	h := fallbackHarness(t, RoleImplementer)
	h.runner.exits = map[string]ExitClass{"codex": ExitError}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if rec.Card == nil {
		t.Fatal("missing card")
	}
	raw, _ := json.Marshal(rec.Card)
	if !strings.Contains(string(raw), "role switched: implementer codex sol high to claude sonnet medium (error exit)") {
		t.Fatalf("card omits switch: %s", raw)
	}
}

func TestEngineRefusesIncompleteContractBeforeCreatingRun(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cli", "slicer", "relay-v10.json"))
			if err != nil {
				t.Fatal(err)
			}
			var data map[string]json.RawMessage
			json.Unmarshal(raw, &data)
			delete(data, "conformance")
			body, _ := json.Marshal(data)
			return []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: string(body)}}
		}
	})
	_, err := New(context.Background(), h.deps, h.cfg)
	if err == nil || !strings.Contains(err.Error(), "conformance") {
		t.Fatalf("%v", err)
	}
	runs, _ := h.store.List()
	if len(runs) != 0 || len(h.runner.requests()) != 0 || len(h.fake.CallsTo("git")) != 0 {
		t.Fatal("invalid contract started work")
	}
}

func TestSpecCheckLedgerStopsAfterFailure(t *testing.T) {
	h := specHarness(t)
	h.runner.script = func(Request, int) (Result, error) { return Result{Exit: ExitAuth}, nil }
	_, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
	if err == nil {
		t.Fatal("spec check succeeded")
	}
	if !ledgerHas(h, "relay-run end", "--outcome stopped", "--close-steps fail") {
		t.Fatal("failed spec check left ledger active")
	}
}

func TestOutOfCreditsStaysSkippedAfterAnotherVendorFails(t *testing.T) {
	h := fallbackHarness(t, RoleImplementer)
	h.cfg.Lineup[RoleImplementer] = []runrec.LineupEntry{
		{Vendor: "codex", Model: "sol", Effort: "high"},
		{Vendor: "claude", Model: "sonnet", Effort: "medium"},
		{Vendor: "codex", Model: "sol", Effort: "medium"},
	}
	h.runner.exits = map[string]ExitClass{"codex": ExitOutOfCredits, "claude": ExitError}
	e := h.start()
	requireState(t, e, runrec.StateStopped)
	var rows []string
	for _, r := range h.runner.requests() {
		if r.Role == RoleImplementer {
			rows = append(rows, r.Vendor)
		}
	}
	if strings.Join(rows, ",") != "codex,claude" {
		t.Fatalf("exhausted vendor reused: %v", rows)
	}
}

func TestSpecCheckRetryKeepsTheFallbackRow(t *testing.T) {
	h := specHarness(t)
	cfg := specConfig(h, t)
	cfg.Lineup = []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "medium"}, {Vendor: "claude", Model: "sonnet", Effort: "medium"}}
	good := false
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Vendor == "codex" {
			return Result{Exit: ExitRateLimited}, nil
		}
		if !good {
			good = true
			return Result{Exit: ExitOK, Report: "malformed"}, nil
		}
		return Result{Exit: ExitOK, Report: specReport}, nil
	}
	if _, err := RunSpecCheck(context.Background(), h.deps, cfg); err != nil {
		t.Fatal(err)
	}
	var vendors []string
	for _, req := range h.runner.requests() {
		vendors = append(vendors, req.Vendor)
	}
	if strings.Join(vendors, ",") != "codex,claude,claude" {
		t.Fatalf("spec check retried an earlier row: %v", vendors)
	}
}
