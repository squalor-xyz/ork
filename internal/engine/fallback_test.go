// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

func fallbackHarness(t *testing.T, role string) *harness {
	t.Helper()
	h := newHarness(t)
	h.cfg.Lineup[role] = []runrec.LineupEntry{
		{Vendor: "codex", Model: "sol", Effort: "high"},
		{Vendor: "claude", Model: "sonnet", Effort: "medium"},
	}
	return h
}

func checkFallback(t *testing.T, class ExitClass) {
	t.Helper()
	h := fallbackHarness(t, RoleImplementer)
	h.runner.exits = map[string]ExitClass{"codex": class}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if len(rec.Roles) != 4 || rec.Roles[0].Exit != string(class) || rec.Roles[1].Exit != string(ExitOK) || rec.Roles[1].Vendor != "claude" || rec.Roles[1].Round != 1 {
		t.Fatalf("fallback records: %+v", rec.Roles)
	}
	reqs := h.runner.requests()
	if reqs[1].Resume != "" || filepath.Base(reqs[1].ReportPath) != "implementer-1-attempt-2.md" || rec.Roles[1].Report != "implementer-1-attempt-2.md" {
		t.Fatalf("fallback request/report: %+v / %+v", reqs[1], rec.Roles[1])
	}
	if h.readReport(rec, rec.Roles[1].Report) != implReport {
		t.Fatal("fallback report was not saved")
	}
	steps := h.steps()
	if steps[2].Phase != "fail" || steps[2].Step != rec.Roles[0].Step || steps[3].Vendor != "claude" {
		t.Fatalf("fallback ledger: %+v", steps)
	}
	if len(h.calls("slicer", "relay-run", "end")) != 0 {
		t.Fatal("fallback ended the relay")
	}
}

func TestRateLimitFallsBackToTheNextRow(t *testing.T) { checkFallback(t, ExitRateLimited) }
func TestErrorExitFallsBackToTheNextRow(t *testing.T) { checkFallback(t, ExitError) }

func TestAuthAndUnclassifiedExitsStillStopTheSlice(t *testing.T) {
	for _, class := range []ExitClass{ExitAuth, ExitUnclassified} {
		t.Run(string(class), func(t *testing.T) {
			h := fallbackHarness(t, RoleImplementer)
			h.runner.script = func(req Request, n int) (Result, error) {
				res := Result{Exit: class, Report: implReport}
				return res, nil
			}
			rec := requireState(t, h.start(), runrec.StateStopped)
			if len(rec.Roles) != 1 || len(h.runner.requests()) != 1 || !strings.Contains(rec.Reason, string(class)) {
				t.Fatalf("stop: %+v", rec)
			}
		})
	}
}

func TestFallbackIsSticky(t *testing.T) {
	h := fallbackHarness(t, RoleImplementer)
	h.runner.exits = map[string]ExitClass{"codex": ExitRateLimited}
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleReviewer {
			return Result{Exit: ExitOK, Session: "review", Report: findingsReview("engine.go:10 fix it")}, nil
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	var impl []Request
	for _, req := range h.runner.requests() {
		if req.Role == RoleImplementer {
			impl = append(impl, req)
		}
	}
	if len(impl) != 3 || impl[2].Round != 2 || impl[2].Vendor != "claude" || impl[2].Resume != "sess-implementer" {
		t.Fatalf("sticky requests: %+v (records %+v)", impl, rec.Roles)
	}
}

func TestResumeOnlyWithTheSameVendorAndModel(t *testing.T) {
	for _, tc := range []struct{ name, vendor, model, want string }{
		{"same", "claude", "opus", "sess-reviewer"},
		{"vendor", "codex", "opus", ""},
		{"model", "claude", "sonnet", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.cfg.Lineup[RoleGate] = []runrec.LineupEntry{{Vendor: tc.vendor, Model: tc.model, Effort: "high"}}
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			req := h.runner.requests()[2]
			if req.Resume != tc.want {
				t.Fatalf("resume %q, want %q", req.Resume, tc.want)
			}
			if tc.want == "" && !strings.Contains(req.Brief, cleanReview) {
				t.Fatal("fresh session missing earlier report")
			}
		})
	}
}

func TestFallbackOnTheLastRowStopsWithTheReasons(t *testing.T) {
	for _, last := range []ExitClass{ExitError, ExitRateLimited} {
		t.Run(string(last), func(t *testing.T) {
			h := fallbackHarness(t, RoleImplementer)
			h.runner.exits = map[string]ExitClass{"codex": ExitRateLimited, "claude": last}
			rec := requireState(t, h.start(), runrec.StateStopped)
			if len(rec.Roles) != 2 {
				t.Fatalf("roles: %+v", rec.Roles)
			}
			for _, text := range []string{"codex", "sol", "high", "rate_limited", "claude", "sonnet", "medium", string(last)} {
				if !strings.Contains(rec.Reason, text) {
					t.Errorf("reason %q missing %s", rec.Reason, text)
				}
			}
		})
	}
}

func TestFallbackReadOnlyTreeChangeStillStops(t *testing.T) {
	h := newHarness(t, statusRoute("", " M changed.go\n"))
	h.cfg.Lineup[RoleReviewer] = []runrec.LineupEntry{{Vendor: "codex", Model: "sol"}, {Vendor: "claude", Model: "sonnet"}}
	h.runner.exits = map[string]ExitClass{"codex": ExitRateLimited}
	rec := requireState(t, h.start(), runrec.StateStopped)
	if len(rec.Roles) != 2 {
		t.Fatalf("fallback ran after tree change: %+v", rec.Roles)
	}
}

func TestFallbackRunnerErrorUsesNextRow(t *testing.T) {
	h := fallbackHarness(t, RoleImplementer)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Vendor == "codex" {
			return Result{}, context.DeadlineExceeded
		}
		return Result{}, nil
	}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
}

func TestFallbackMatchesEffortAndSurvivesReopen(t *testing.T) {
	h := fallbackHarness(t, RoleImplementer)
	h.cfg.Lineup[RoleImplementer] = []runrec.LineupEntry{
		{Vendor: "claude", Model: "sonnet", Effort: "high"},
		{Vendor: "claude", Model: "sonnet", Effort: "medium"},
		{Vendor: "codex", Model: "sol", Effort: "high"},
	}
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && req.Effort == "high" {
			return Result{Exit: ExitError}, nil
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	stored, err := h.store.Open(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := currentEntry(stored.Snapshot(), RoleImplementer); got != 1 {
		t.Fatalf("persisted sticky row %d, want 1", got)
	}
}

func TestFallbackCurrentEntry(t *testing.T) {
	entries := []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}, {Vendor: "claude", Model: "sonnet", Effort: "medium"}}
	for _, tc := range []struct {
		name, exit string
		want       int
	}{
		{"sticky", "ok", 1}, {"rate", "rate_limited", 2}, {"error", "error", 2}, {"auth", "auth", 1}, {"unclassified", "unclassified", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := runrec.Record{Lineup: map[string][]runrec.LineupEntry{RoleReviewer: entries}, Roles: []runrec.RoleRecord{
				{Role: RoleGate, Vendor: "claude", Model: "sonnet", Effort: "medium", Exit: tc.exit},
				{Role: RoleImplementer, Vendor: "claude", Model: "sonnet", Effort: "high", Exit: "error"},
			}}
			if got := currentEntry(rec, RoleGate); got != tc.want {
				t.Fatalf("row %d, want %d", got, tc.want)
			}
		})
	}
}

func TestResumeSkipsNewerIncompatibleSessions(t *testing.T) {
	rec := runrec.Record{Roles: []runrec.RoleRecord{
		{Role: RoleReviewer, Vendor: "claude", Model: "sonnet", Session: "compatible"},
		{Role: RoleGate, Vendor: "codex", Model: "sonnet", Session: "wrong-vendor"},
		{Role: RoleGate, Vendor: "claude", Model: "opus", Session: "wrong-model"},
		{Role: RoleImplementer, Vendor: "claude", Model: "sonnet", Session: "wrong-role"},
	}}
	if got := latestSession(rec, []string{RoleGate, RoleReviewer}, "claude", "sonnet"); got != "compatible" {
		t.Fatalf("session %q", got)
	}
}

func TestUnsuccessfulPermissionDenialsStillStopWithANextRow(t *testing.T) {
	for _, class := range []ExitClass{ExitError, ExitRateLimited} {
		t.Run(string(class), func(t *testing.T) {
			h := fallbackHarness(t, RoleImplementer)
			h.runner.script = func(Request, int) (Result, error) {
				return Result{Exit: class, Denials: []runrec.DeniedCommand{{Tool: "Write", Command: "blocked.txt"}}}, nil
			}
			rec := requireState(t, h.start(), runrec.StateStopped)
			if len(rec.Roles) != 1 || len(h.runner.requests()) != 1 || !strings.Contains(rec.Reason, "Write: blocked.txt") {
				t.Fatalf("denial did not stop with its reason: %+v", rec)
			}
		})
	}
}

func TestFallbackDuplicateRowsTerminate(t *testing.T) {
	for _, allFail := range []bool{false, true} {
		name := "third-row-ok"
		if allFail {
			name = "all-fail"
		}
		t.Run(name, func(t *testing.T) {
			h := fallbackHarness(t, RoleImplementer)
			entries := h.cfg.Lineup[RoleImplementer]
			h.cfg.Lineup[RoleImplementer] = []runrec.LineupEntry{entries[0], entries[0], entries[1]}
			calls := 0
			h.runner.script = func(req Request, _ int) (Result, error) {
				if req.Role != RoleImplementer {
					return Result{}, nil
				}
				calls++
				if calls > 3 || (calls == 3 && req.Vendor != "claude") {
					t.Fatalf("duplicate fallback did not reach third row: call %d vendor %s", calls, req.Vendor)
				}
				if req.Vendor == "codex" {
					return Result{Exit: ExitError}, nil
				}
				if allFail {
					return Result{Exit: ExitRateLimited}, nil
				}
				return Result{}, nil
			}
			want := runrec.StateAwaitingApproval
			if allFail {
				want = runrec.StateStopped
			}
			rec := requireState(t, h.start(), want)
			if calls != 3 {
				t.Fatalf("implementer calls %d, want 3", calls)
			}
			for i, vendor := range []string{"codex", "codex", "claude"} {
				if rec.Roles[i].Vendor != vendor {
					t.Fatalf("row %d = %+v", i, rec.Roles[i])
				}
			}
			if allFail {
				if strings.Count(rec.Reason, "(codex sol high) exited error") != 2 || !strings.Contains(rec.Reason, "(claude sonnet medium) exited rate_limited") {
					t.Fatalf("stop reason must name all three failed rows: %s", rec.Reason)
				}
			}
		})
	}
}

func TestFallbackKeepsTheOwnerAnswer(t *testing.T) {
	h := fallbackHarness(t, RoleImplementer)
	h.runner.script = func(req Request, n int) (Result, error) {
		if n == 0 {
			return Result{Exit: ExitOK, Session: "question-session", Report: withQuestion(implReport)}, nil
		}
		if n == 1 {
			return Result{Exit: ExitRateLimited}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	requireState(t, e, runrec.StatePaused)
	const answer = "Use a cap of 8 for this run."
	if err := e.Answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	reqs := h.runner.requests()
	if len(reqs) != 5 || reqs[2].Role != RoleImplementer || reqs[2].Vendor != "claude" || !strings.Contains(reqs[2].Brief, answer) || !strings.Contains(reqs[2].Brief, question) {
		t.Fatal("fallback brief lost owner answer")
	}
	if rec.Pending != nil {
		t.Fatalf("successful fallback kept pending answer: %+v", rec.Pending)
	}
}

func TestFallbackUnmatchedHistoryExhaustsTheChain(t *testing.T) {
	rec := runrec.Record{
		Lineup: map[string][]runrec.LineupEntry{RoleImplementer: {{Vendor: "codex", Model: "sol"}}},
		Roles:  []runrec.RoleRecord{{Role: RoleImplementer, Vendor: "claude", Model: "missing", Exit: string(ExitError)}},
	}
	if got := currentEntry(rec, RoleImplementer); got != 1 {
		t.Fatalf("unmatched history selected row %d, want exhausted chain (1)", got)
	}
}

func TestFallbackUnmatchedLostHistoryKeepsTheCurrentRow(t *testing.T) {
	rec := runrec.Record{
		Lineup: map[string][]runrec.LineupEntry{RoleImplementer: {{Vendor: "codex", Model: "sol"}, {Vendor: "claude", Model: "sonnet"}}},
		Roles: []runrec.RoleRecord{
			{Role: RoleImplementer, Vendor: "codex", Model: "sol", Exit: string(ExitError)},
			{Role: RoleImplementer, Vendor: "claude", Exit: "lost"},
		},
	}
	if got := currentEntry(rec, RoleImplementer); got != 1 {
		t.Fatalf("unmatched lost attempt selected row %d, want current row 1", got)
	}
}
