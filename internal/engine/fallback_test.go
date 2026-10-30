// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// Replays the successful repeated-start output observed in an isolated scratch
// project on 2026-10-26. The S92 owner condition therefore keeps only the
// earlier-row and partial-work note, without a skip-start instruction.
func TestRepeatedSlicerStartStrictSucceeds(t *testing.T) {
	const recorded = `{"status":"started","fields":{"attempts":1}}`
	args := []string{"start", "S04", "--render", "--strict", "--json"}
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: args, Stdout: recorded})
	for range 2 {
		res, err := clirun.Run(context.Background(), clirun.Call{Bin: fake.Bin("slicer"), Args: args})
		if err != nil || string(res.Stdout) != recorded {
			t.Fatalf("repeated start: %s, %v", res.Stdout, err)
		}
	}
}

func TestFallbackBriefNamesTheEarlierRowAndPartialWork(t *testing.T) {
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate} {
		t.Run(role, func(t *testing.T) {
			h := fallbackHarness(t, role)
			// Two rows with the same vendor and model must retain their efforts.
			h.cfg.Lineup[role] = []runrec.LineupEntry{
				{Vendor: "codex", Model: "sol", Effort: "high"},
				{Vendor: "codex", Model: "sol", Effort: "medium"},
				{Vendor: "claude", Model: "sonnet", Effort: "medium"},
			}
			h.runner.script = func(req Request, _ int) (Result, error) {
				if req.Role == role && req.Vendor == "codex" {
					if req.Effort == "high" {
						return Result{Exit: ExitRateLimited}, nil
					}
					return Result{Exit: ExitError}, nil
				}
				return Result{}, nil
			}
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			var briefs []string
			for _, req := range h.runner.requests() {
				if req.Role == role {
					briefs = append(briefs, req.Brief)
				}
			}
			if len(briefs) != 3 {
				t.Fatalf("got %d role briefs, want 3", len(briefs))
			}
			for i := 1; i < 3; i++ {
				for _, want := range []string{"## Earlier lineup rows", role + " round 1 (codex sol high) exited rate_limited", "worktree may hold partial work", "git status"} {
					if !strings.Contains(briefs[i], want) {
						t.Errorf("row %d brief lacks %q", i+1, want)
					}
				}
			}
			if !strings.Contains(briefs[2], role+" round 1 (codex sol medium) exited error") {
				t.Error("third row brief lacks the second row and its exit")
			}
			if strings.Contains(briefs[1], "(codex sol medium) exited") {
				t.Error("second row brief includes a later row")
			}
		})
	}
}

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
	_, note, _ := strings.Cut(impl[2].Brief, "## Earlier lineup rows\n\n")
	note, _, _ = strings.Cut(note, "\n## ")
	if !strings.Contains(note, "implementer round 1 (codex sol high) exited rate_limited") || strings.Contains(note, "claude") || strings.Contains(note, "reviewer") {
		t.Fatalf("resumed fallback must name only earlier rows of this role: %q", note)
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
			if !allFail && strings.Count(h.runner.requests()[2].Brief, "(codex sol high) exited error") != 2 {
				t.Fatal("fallback brief must keep both identical earlier rows")
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

// gateFallbackHarness makes the reviewer fail row 1 and succeed on row 2.
// Rows share vendor/model so effort and session assertions catch row drift.
func gateFallbackHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.cfg.Lineup[RoleReviewer] = []runrec.LineupEntry{
		{Vendor: "claude", Model: "opus", Effort: "high"},
		{Vendor: "claude", Model: "opus", Effort: "medium"},
		{Vendor: "codex", Model: "sol", Effort: "high"},
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleReviewer && req.Effort == "high" {
			return Result{Exit: ExitRateLimited}, nil
		}
		return Result{}, nil
	}
	return h
}

func TestGateStartsOnTheReviewersCurrentRow(t *testing.T) {
	h := gateFallbackHarness(t)
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	for _, req := range h.runner.requests() {
		if req.Role != RoleGate {
			continue
		}
		if req.Vendor != "claude" || req.Model != "opus" || req.Effort != "medium" || req.Resume != "sess-reviewer" {
			t.Fatalf("gate row/session = %s %s %s %q, want reviewer row 2 and session", req.Vendor, req.Model, req.Effort, req.Resume)
		}
		if strings.Contains(req.Brief, "## Earlier lineup rows") {
			t.Fatal("first gate brief must omit the earlier lineup rows heading")
		}
		if strings.Contains(req.Brief, "\n## \n") {
			t.Fatal("first gate brief must omit empty section headings")
		}
		stored, err := h.store.Open(rec.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got := currentEntry(stored.Snapshot(), RoleGate); got != 1 {
			t.Fatalf("persisted gate row %d, want 1", got)
		}
		return
	}
	t.Fatal("gate did not run")
}

func TestGateFallsBackFromTheReviewersRow(t *testing.T) {
	h := gateFallbackHarness(t)
	reviewerScript := h.runner.script
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate && req.Vendor == "claude" {
			return Result{Exit: ExitRateLimited}, nil
		}
		return reviewerScript(req, n)
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	var gates []Request
	for _, req := range h.runner.requests() {
		if req.Role == RoleGate {
			gates = append(gates, req)
		}
	}
	if len(gates) != 2 {
		t.Fatalf("got %d gate attempts, want row 2 then row 3", len(gates))
	}
	if gates[0].Effort != "medium" || gates[0].Vendor != "claude" || gates[1].Vendor != "codex" || gates[1].Model != "sol" || gates[1].Resume != "" {
		t.Fatalf("gate did not advance from reviewer row 2 to row 3")
	}
	if !strings.Contains(gates[1].Brief, "gate round 1 (claude opus medium) exited rate_limited") {
		t.Fatal("gate fallback brief lacks its own failed row")
	}
	if got := currentEntry(rec, RoleGate); got != 2 {
		t.Fatalf("sticky gate row %d, want 2", got)
	}
}

func TestGateWithItsOwnLineupIsUnchanged(t *testing.T) {
	h := gateFallbackHarness(t)
	h.cfg.Lineup[RoleGate] = []runrec.LineupEntry{
		{Vendor: "claude", Model: "opus", Effort: "high"},
		{Vendor: "codex", Model: "sol", Effort: "medium"},
	}
	reviewerScript := h.runner.script
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate && req.Vendor == "claude" {
			return Result{Exit: ExitRateLimited}, nil
		}
		return reviewerScript(req, n)
	}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	var gates []Request
	for _, req := range h.runner.requests() {
		if req.Role == RoleGate {
			gates = append(gates, req)
		}
	}
	if len(gates) != 2 || gates[0].Vendor != "claude" || gates[0].Effort != "high" || gates[1].Vendor != "codex" || gates[1].Effort != "medium" {
		t.Fatal("explicit gate lineup must start at its own row 1 and advance")
	}
}

func TestFallbackSectionSkipsEmptyHistory(t *testing.T) {
	got := fallbackSection(runrec.Record{}, RoleGate, []runrec.LineupEntry{{Vendor: "claude", Model: "opus"}})
	if got != (BriefSection{}) {
		t.Fatalf("empty failure history produced section: %+v", got)
	}
}

func TestGateCopiedLineupStartsOnReviewersCurrentRow(t *testing.T) {
	h := gateFallbackHarness(t)
	h.cfg.Lineup[RoleGate] = append([]runrec.LineupEntry(nil), h.cfg.Lineup[RoleReviewer]...)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	var gates []Request
	for _, req := range h.runner.requests() {
		if req.Role == RoleGate {
			gates = append(gates, req)
		}
	}
	if len(gates) != 1 || gates[0].Vendor != "claude" || gates[0].Model != "opus" || gates[0].Effort != "medium" || gates[0].Resume != "sess-reviewer" {
		t.Fatal("copied gate lineup must start on reviewer row 2 and resume its session")
	}
	if strings.Contains(gates[0].Brief, "## Earlier lineup rows") {
		t.Fatal("first copied-lineup gate brief must omit the earlier lineup rows heading")
	}
}

func TestGateOwnHistoryTakesPrecedenceOverReviewerSeed(t *testing.T) {
	for _, copied := range []bool{false, true} {
		name := "empty-lineup"
		if copied {
			name = "copied-lineup"
		}
		t.Run(name, func(t *testing.T) {
			for _, exit := range []ExitClass{ExitOK, ExitRateLimited} {
				t.Run(string(exit), func(t *testing.T) {
					h := gateFallbackHarness(t)
					if copied {
						h.cfg.Lineup[RoleGate] = append([]runrec.LineupEntry(nil), h.cfg.Lineup[RoleReviewer]...)
					}
					e, err := New(context.Background(), h.deps, h.cfg)
					if err != nil {
						t.Fatal(err)
					}
					if err := e.run.Update(func(rec *runrec.Record) {
						rec.State = runrec.StateGate
						rec.Roles = []runrec.RoleRecord{
							{Role: RoleReviewer, Round: 1, Vendor: "claude", Model: "opus", Effort: "high", Exit: string(ExitRateLimited), EndedAt: h.deps.now()},
							{Role: RoleReviewer, Round: 1, Vendor: "claude", Model: "opus", Effort: "medium", Exit: string(ExitOK), EndedAt: h.deps.now()},
							{Role: RoleGate, Round: 1, Vendor: "claude", Model: "opus", Effort: "high", Exit: string(exit), EndedAt: h.deps.now()},
						}
					}); err != nil {
						t.Fatal(err)
					}
					out, err := e.runRound(context.Background(), round{role: RoleGate, n: 1, state: runrec.StateGate, task: "S04 gate"}, nil)
					if err != nil || !out.ok {
						t.Fatalf("gate with own history must continue: ok=%v err=%v state=%s reason=%q", out.ok, err, e.Snapshot().State, e.Snapshot().Reason)
					}
					want := "high"
					if exit == ExitRateLimited {
						want = "medium"
					}
					reqs := h.runner.requests()
					if len(reqs) != 1 || reqs[0].Effort != want {
						t.Fatalf("gate history must select effort %s", want)
					}
				})
			}
		})
	}
}

// vendorHarness is a fallback harness whose clock the test moves by hand and
// whose engines share one Vendors with codex capped at one slot (S169).
func vendorHarness(t *testing.T) (*harness, *Vendors, *vendorClock) {
	t.Helper()
	h := fallbackHarness(t, RoleImplementer)
	clock := newVendorClock()
	vendors := NewVendors(map[string]int{"codex": 1}).WithClock(clock.Now)
	h.deps.Now = clock.Now
	h.deps.Vendors = vendors
	return h, vendors, clock
}

// waitForVendorWait polls until the engine reports a wait for the vendor.
func waitForVendorWait(t *testing.T, e *Engine, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for e.VendorWait() != want {
		if time.Now().After(deadline) {
			t.Fatalf("VendorWait() = %q, want %q", e.VendorWait(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func noRoleSteps(t *testing.T, h *harness, what string) {
	t.Helper()
	for _, s := range h.steps() {
		if s.Agent == "codex" || s.Agent == "claude" {
			t.Fatalf("%s: a role step was recorded: %+v", what, s)
		}
	}
}

func TestRoundWaitsForTheVendorSlotWithoutSpendingBudget(t *testing.T) {
	h, vendors, clock := vendorHarness(t)
	h.cfg.Budgets.Minutes = 60
	holder, _, err := vendors.Acquire(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.Run(context.Background()) }()

	waitForVendorWait(t, e, "codex")
	if rec := e.Snapshot(); len(rec.Roles) != 0 || len(h.runner.requests()) != 0 {
		t.Fatalf("a waiting round recorded a role or started one: %+v", rec.Roles)
	}
	noRoleSteps(t, h, "while waiting")

	// Two hours pass at the slot. They are not role run time.
	clock.Advance(2 * time.Hour)
	holder()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if e.VendorWait() != "" {
		t.Fatalf("VendorWait() = %q after the round started", e.VendorWait())
	}
	if len(rec.Roles) == 0 || rec.Roles[0].Vendor != "codex" || rec.Roles[0].Exit != string(ExitOK) {
		t.Fatalf("the waiting round must run on codex once it has the slot: %+v", rec.Roles)
	}
}

func TestStopWhileWaitingForAVendorSlotStopsTheRun(t *testing.T) {
	h, vendors, _ := vendorHarness(t)
	holder, _, err := vendors.Acquire(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer holder()
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	waitForVendorWait(t, e, "codex")

	// The daemon cancels the run's context, waits for Run, and then stops.
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after the cancel")
	}
	if e.VendorWait() != "" {
		t.Fatalf("VendorWait() = %q after the cancel", e.VendorWait())
	}
	if err := e.Stop(context.Background(), "CLI stop"); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateStopped)
	if len(rec.Roles) != 0 || len(h.runner.requests()) != 0 {
		t.Fatalf("a stopped wait left a role record: %+v", rec.Roles)
	}
	noRoleSteps(t, h, "stopped wait")
	if vendors.waiters("codex") != 0 {
		t.Fatal("the cancelled wait still holds a place in the queue")
	}
}

func TestRateLimitedExitMarksTheVendorOutAndErrorDoesNot(t *testing.T) {
	const limitText = "You've hit your usage limit"
	for _, tc := range []struct {
		class ExitClass
		out   bool
	}{
		{ExitRateLimited, true}, {ExitOutOfCredits, true}, {ExitError, false},
	} {
		t.Run(string(tc.class), func(t *testing.T) {
			h, vendors, clock := vendorHarness(t)
			h.runner.script = func(req Request, _ int) (Result, error) {
				if req.Vendor == "codex" {
					return Result{Exit: tc.class, Reason: limitText}, nil
				}
				return Result{}, nil
			}
			rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
			if rec.Roles[0].Exit != string(tc.class) || rec.Roles[1].Vendor != "claude" {
				t.Fatalf("the round must fall back as before: %+v", rec.Roles)
			}
			if got := vendors.IsOut("codex"); got != tc.out {
				t.Fatalf("IsOut(codex) = %v, want %v", got, tc.out)
			}
			if vendors.IsOut("claude") {
				t.Fatal("claude succeeded and must not be out")
			}
			if !tc.out {
				if len(vendors.Out()) != 0 {
					t.Fatalf("an error exit marked something: %+v", vendors.Out())
				}
				return
			}
			out := vendors.Out()
			if len(out) != 1 || out[0].Reason != limitText || !out[0].Since.Equal(clock.Now()) {
				t.Fatalf("out entry: %+v", out)
			}
		})
	}
}

func TestMarkOutReasonFallsBackToTheExitClass(t *testing.T) {
	h, vendors, _ := vendorHarness(t)
	h.runner.exits = map[string]ExitClass{"codex": ExitRateLimited}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	out := vendors.Out()
	if len(out) != 1 || out[0].Reason != "rate_limited" {
		t.Fatalf("out entry: %+v", out)
	}
}

func TestNewRoundSkipsRowsOfAVendorThatIsOut(t *testing.T) {
	h, vendors, clock := vendorHarness(t)
	h.cfg.Lineup[RoleReviewer] = []runrec.LineupEntry{
		{Vendor: "codex", Model: "sol", Effort: "high"},
		{Vendor: "claude", Model: "opus", Effort: "medium"},
	}
	vendors.MarkOut("codex", "out of credits", clock.Now())
	reviews := 0
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleReviewer {
			reviews++
			if reviews == 1 {
				return Result{Exit: ExitOK, Session: "review", Report: findingsReview("engine.go:10 fix it")}, nil
			}
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	var implementers, reviewers, gates []Request
	for _, req := range h.runner.requests() {
		if req.Vendor != "claude" {
			t.Fatalf("a round started on the vendor that is out: %+v", req)
		}
		switch req.Role {
		case RoleImplementer:
			implementers = append(implementers, req)
		case RoleReviewer:
			reviewers = append(reviewers, req)
		case RoleGate:
			gates = append(gates, req)
		}
	}
	if len(implementers) != 2 || implementers[0].Model != "sonnet" || implementers[1].Round != 2 || implementers[1].Resume != "sess-implementer" {
		t.Fatalf("implementer rounds: %+v", implementers)
	}
	if len(reviewers) != 1 || reviewers[0].Model != "opus" || reviewers[0].Effort != "medium" {
		t.Fatalf("reviewer rounds: %d", len(reviewers))
	}
	if len(gates) == 0 || gates[0].Model != "opus" || gates[0].Resume != "review" {
		t.Fatalf("gate rounds: %+v", gates)
	}
	// The later row's record matches forward, also after a reopen.
	stored, err := h.store.Open(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	for role, want := range map[string]int{RoleImplementer: 1, RoleReviewer: 1, RoleGate: 1} {
		if got := currentEntry(stored.Snapshot(), role); got != want {
			t.Errorf("currentEntry(%s) = %d, want %d", role, got, want)
		}
	}
	for _, role := range rec.Roles {
		if role.Vendor == "codex" {
			t.Fatalf("a codex record exists: %+v", role)
		}
	}
	// No fallback brief: no earlier row failed.
	if strings.Contains(implementers[0].Brief, "## Earlier lineup rows") {
		t.Fatal("a skipped row is not a failed attempt and has no fallback section")
	}
}

func TestVendorThatIsBackIsUsedAgain(t *testing.T) {
	h, vendors, clock := vendorHarness(t)
	vendors.MarkOut("codex", "usage limit", clock.Now())
	clock.Advance(VendorOutFor)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	if reqs := h.runner.requests(); reqs[0].Vendor != "codex" {
		t.Fatalf("first round vendor %s, want codex after the 60 minutes", reqs[0].Vendor)
	}

	// `ork vendor back` ends it earlier.
	h2, vendors2, clock2 := vendorHarness(t)
	vendors2.MarkOut("codex", "usage limit", clock2.Now())
	vendors2.Back("codex")
	requireState(t, h2.start(), runrec.StateAwaitingApproval)
	if reqs := h2.runner.requests(); reqs[0].Vendor != "codex" {
		t.Fatalf("first round vendor %s, want codex after back", reqs[0].Vendor)
	}
}

func TestOutVendorWithNoOtherVendorRowStillRunsTheRow(t *testing.T) {
	h, vendors, clock := vendorHarness(t)
	h.cfg.Lineup[RoleImplementer] = []runrec.LineupEntry{
		{Vendor: "codex", Model: "sol", Effort: "high"},
		{Vendor: "codex", Model: "sol", Effort: "medium"},
	}
	vendors.MarkOut("codex", "usage limit", clock.Now())
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	reqs := h.runner.requests()
	if reqs[0].Role != RoleImplementer || reqs[0].Vendor != "codex" || reqs[0].Effort != "high" {
		t.Fatalf("the first row must run anyway: %+v", reqs[0])
	}
}

func TestSlotIsFreedWhenTheVendorProcessEnds(t *testing.T) {
	h, vendors, clock := vendorHarness(t)
	var returned atomic.Bool
	var mu sync.Mutex
	held := -1 // the slots held at the engine's first look at the clock after the process
	h.deps.Now = func() time.Time {
		if returned.Load() {
			mu.Lock()
			if held < 0 {
				held = vendors.holders("codex")
			}
			mu.Unlock()
		}
		return clock.Now()
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleImplementer {
			defer returned.Store(true)
		}
		return Result{}, nil
	}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	mu.Lock()
	defer mu.Unlock()
	if held != 0 {
		t.Fatalf("%d codex slots held after the process ended, want 0", held)
	}
}

// The mark comes before the slot is freed, so that a round that waits for the
// slot finds the vendor out when it gets it. The clock is read when the mark is
// made, and that is the first read after the vendor process ended.
func TestVendorIsMarkedOutBeforeItsSlotIsFreed(t *testing.T) {
	h, vendors, clock := vendorHarness(t)
	var returned atomic.Bool
	var mu sync.Mutex
	held, out := -1, false // at the engine's first look at the clock after the process
	h.deps.Now = func() time.Time {
		if returned.Load() {
			mu.Lock()
			if held < 0 {
				held, out = vendors.holders("codex"), vendors.IsOut("codex")
			}
			mu.Unlock()
		}
		return clock.Now()
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleImplementer && req.Vendor == "codex" {
			defer returned.Store(true)
			return Result{Exit: ExitRateLimited, Reason: "usage limit"}, nil
		}
		return Result{}, nil
	}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	mu.Lock()
	defer mu.Unlock()
	if held != 1 || out {
		t.Fatalf("at the mark: %d codex slots held, out %v; want the slot held (1) until codex is marked out", held, out)
	}
	if !vendors.IsOut("codex") || vendors.holders("codex") != 0 {
		t.Fatal("after the round codex must be out and its slot free")
	}
}

func TestSlotIsFreedWhenARoundFailsBeforeItsProcess(t *testing.T) {
	// The reviewer is read-only: its round reads the tree first, and that read
	// fails. The round returns after it took the slot and before the process.
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{{Tool: "git", Args: []string{"-C", W, "status", "--porcelain"}, Exit: 1}}
		}
	})
	vendors := NewVendors(map[string]int{"codex": 1})
	h.deps.Vendors = vendors
	h.cfg.Lineup[RoleReviewer] = []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || !strings.Contains(rec.Pending.Reason, "read-only fingerprint") {
		t.Fatalf("the reviewer round must fail on the tree read: %+v", rec.Pending)
	}
	if n := vendors.holders("codex"); n != 0 {
		t.Fatalf("%d codex slots held by a round that never started a process", n)
	}
}

func TestSlotIsFreeBetweenRowsOfOneRun(t *testing.T) {
	h, _, _ := vendorHarness(t)
	h.cfg.Lineup[RoleImplementer] = []runrec.LineupEntry{
		{Vendor: "codex", Model: "sol", Effort: "high"},
		{Vendor: "codex", Model: "sol", Effort: "medium"},
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleImplementer && req.Effort == "high" {
			return Result{Exit: ExitError}, nil
		}
		return Result{}, nil
	}
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.Run(ctx); err != nil {
		t.Fatalf("Run: %v (a failed round must free its slot before the next row)", err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
}

func TestAHungRoleDoesNotMarkItsVendorOut(t *testing.T) {
	// The killed role reports a rate limit as it dies. The watchdog killed it,
	// so the exit is not the vendor's word and marks nothing (S182).
	ended := make(chan struct{})
	var once sync.Once
	hr := &hangRunner{silent: onlyRole(RoleImplementer), script: func(ctx context.Context, req Request) (Result, error) {
		select {
		case <-ctx.Done():
			once.Do(func() { close(ended) })
			return Result{Exit: ExitRateLimited, Reason: "You've hit your usage limit"}, nil
		case <-time.After(safetyStop):
			return Result{}, errors.New("test safety stop: the watchdog never ended the context")
		}
	}}
	h := hungHarness(t, 50*time.Millisecond, hr)
	h.cfg.Lineup[RoleImplementer] = []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}, {Vendor: "claude", Model: "sonnet", Effort: "high"}}
	vendors := NewVendors(map[string]int{"codex": 1})
	h.deps.Vendors = vendors
	e := h.start()
	requireState(t, e, runrec.StatePaused)
	if rec := e.Snapshot(); rec.Roles[0].Exit != "hung" {
		t.Fatalf("role record: %+v", rec.Roles)
	}
	if vendors.IsOut("codex") || len(vendors.Out()) != 0 {
		t.Fatalf("a hung role marked its vendor out: %+v", vendors.Out())
	}
	if n := vendors.holders("codex"); n != 0 {
		t.Fatalf("%d codex slots held after the hung role", n)
	}
}

// startWaiter starts a second run on the harness's fakes, with its own runner,
// and returns once it waits for the codex slot that the test holds. Both runs
// share the Vendors value.
func startWaiter(t *testing.T, h *harness) (*fakeRunner, *Engine, chan error) {
	t.Helper()
	deps := h.deps
	runner := &fakeRunner{}
	deps.Runner = runner
	deps.RandHex = func() string { return "c3d4" }
	e, err := New(context.Background(), deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.Run(context.Background()) }()
	waitForVendorWait(t, e, "codex")
	return runner, e, done
}

func requireFirstRoundOnClaude(t *testing.T, vendors *Vendors, runner *fakeRunner, e *Engine, done chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting run did not finish")
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	reqs := runner.requests()
	for _, req := range reqs {
		if req.Vendor == "codex" {
			t.Fatalf("a round started on codex after codex went out: %+v", req)
		}
	}
	if reqs[0].Role != RoleImplementer || reqs[0].Vendor != "claude" || reqs[0].Model != "sonnet" {
		t.Fatalf("first round: %+v", reqs[0])
	}
	if rec.Roles[0].Vendor != "claude" || strings.Contains(reqs[0].Brief, "## Earlier lineup rows") {
		t.Fatalf("the waiter skipped a row it never tried: %+v", rec.Roles[0])
	}
	if e.VendorWait() != "" {
		t.Fatalf("VendorWait() = %q", e.VendorWait())
	}
	if n := vendors.holders("codex"); n != 0 {
		t.Fatalf("%d codex slots held after the run: the slot of the row it did not take leaked", n)
	}
}

func TestWaiterTakesAnotherRowWhenTheSlotHolderMarksTheVendorOut(t *testing.T) {
	h, vendors, _ := vendorHarness(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleImplementer && req.Vendor == "codex" {
			once.Do(func() { close(started) })
			<-release
			return Result{Exit: ExitRateLimited, Reason: "You've hit your usage limit"}, nil
		}
		return Result{}, nil
	}
	e1, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	done1 := make(chan error, 1)
	go func() { done1 <- e1.Run(context.Background()) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first run's codex round did not start")
	}

	runner2, e2, done2 := startWaiter(t, h)
	close(release)
	requireFirstRoundOnClaude(t, vendors, runner2, e2, done2)
	if err := <-done1; err != nil {
		t.Fatal(err)
	}
	requireState(t, e1, runrec.StateAwaitingApproval)
	if !vendors.IsOut("codex") {
		t.Fatal("the rate limit did not mark codex out")
	}
}

func TestWaiterLooksAgainWhenTheVendorIsMarkedOutWhileItWaits(t *testing.T) {
	h, vendors, clock := vendorHarness(t)
	holder, _, err := vendors.Acquire(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	runner, e, done := startWaiter(t, h)
	vendors.MarkOutOwner("codex", "marked out by the owner", clock.Now())
	holder()
	requireFirstRoundOnClaude(t, vendors, runner, e, done)
}

func TestWaiterKeepsItsRowWhenTheVendorIsStillIn(t *testing.T) {
	h, vendors, _ := vendorHarness(t)
	holder, _, err := vendors.Acquire(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	runner, e, done := startWaiter(t, h)
	holder()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	if reqs := runner.requests(); reqs[0].Vendor != "codex" {
		t.Fatalf("a waiter whose vendor is still in must run its row: %+v", reqs[0])
	}
}
