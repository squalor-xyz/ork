// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestGateWalkStartsAtTheReviewersSeedRow(t *testing.T) {
	a := runrec.LineupEntry{Vendor: "claude", Model: "opus", Effort: "high"}
	b := runrec.LineupEntry{Vendor: "codex", Model: "sol", Effort: "medium"}
	chain := []runrec.LineupEntry{a, b, a}
	for _, copied := range []bool{false, true} {
		name := "inherited-lineup"
		if copied {
			name = "copied-lineup"
		}
		t.Run(name, func(t *testing.T) {
			for _, exit := range []ExitClass{ExitOK, ExitRateLimited, ExitError, ExitOutOfCredits} {
				t.Run(string(exit), func(t *testing.T) {
					rec := runrec.Record{
						Lineup: map[string][]runrec.LineupEntry{RoleReviewer: chain},
						Roles: []runrec.RoleRecord{
							{Role: RoleReviewer, Vendor: a.Vendor, Model: a.Model, Effort: a.Effort, Exit: string(ExitRateLimited)},
							{Role: RoleReviewer, Vendor: b.Vendor, Model: b.Model, Effort: b.Effort, Exit: string(ExitRateLimited)},
							{Role: RoleReviewer, Vendor: a.Vendor, Model: a.Model, Effort: a.Effort, Exit: string(ExitOK)},
						},
					}
					if copied {
						rec.Lineup[RoleGate] = append([]runrec.LineupEntry(nil), chain...)
					}
					if got := currentEntry(rec, RoleGate); got != 2 {
						t.Fatalf("gate seed row %d, want 2", got)
					}
					rec.Roles = append(rec.Roles, runrec.RoleRecord{Role: RoleGate, Vendor: a.Vendor, Model: a.Model, Effort: a.Effort, Exit: string(exit)})
					want := 2
					if exit != ExitOK {
						want = len(chain)
					}
					if got := currentEntry(rec, RoleGate); got != want {
						t.Fatalf("gate row after %s = %d, want %d; must not replay rows before the reviewer seed", exit, got, want)
					}
				})
			}
		})
	}
}

func TestHappyPathReachesAwaitingApproval(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)

	// setup, implement, checks, review, gate (lite, resumed reviewer)
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "gate-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	reqs := h.runner.requests()
	impl, rev, gate := reqs[0], reqs[1], reqs[2]
	if impl.Resume != "" || impl.ReadOnly || !reflect.DeepEqual(impl.Allowlist, DefaultImplementerAllowlist("go test ./...")) {
		t.Errorf("implementer request: %+v", impl)
	}
	if rev.Resume != "" || !rev.ReadOnly || len(rev.Allowlist) != 0 {
		t.Errorf("reviewer request: %+v", rev)
	}
	if gate.Resume != "sess-reviewer" || !gate.ReadOnly || len(gate.Allowlist) != 0 {
		t.Errorf("the gate must resume the reviewer's session, read-only: %+v", gate)
	}
	for _, r := range reqs {
		wantTemp := filepath.Join(h.store.RunDir(rec.ID), "tmp", r.Role+"-1")
		if r.TempDir != wantTemp {
			t.Errorf("%s temp dir %q, want %q", r.Role, r.TempDir, wantTemp)
		}
		if info, err := os.Stat(r.TempDir); err != nil || !info.IsDir() {
			t.Errorf("%s temp directory does not exist: %v", r.Role, err)
		}
		if r.Worktree != h.W || r.Round != 1 {
			t.Errorf("%s: worktree %q round %d", r.Role, r.Worktree, r.Round)
		}
		if want := filepath.Join(h.store.RunDir(rec.ID), r.Role+"-1.md"); r.ReportPath != want {
			t.Errorf("%s report path %q, want %q", r.Role, r.ReportPath, want)
		}
		if !strings.HasPrefix(r.Brief, "Role: "+r.Role+". Contract: relay v11\n") || !strings.Contains(r.Brief, `"id":"S04"`) || !strings.Contains(r.Brief, "Shell: zsh on darwin.") {
			t.Errorf("%s brief is missing its parts:\n%.300s", r.Role, r.Brief)
		}
	}
	// A role that is not resumed gets the earlier reports; a resumed one has them.
	if strings.Contains(impl.Brief, "## Earlier reports") || !strings.Contains(rev.Brief, "## Earlier reports\n\n### implementer-1\n\nDiff stat.") {
		t.Errorf("earlier reports: implementer brief has them: %v; reviewer brief lacks the implementer report", strings.Contains(impl.Brief, "## Earlier reports"))
	}
	if strings.Contains(gate.Brief, "## Earlier reports") || !strings.Contains(gate.Brief, "## Gate") || !strings.Contains(gate.Brief, "VERDICT: PASS") {
		t.Errorf("a resumed gate gets the gate criteria and no earlier reports")
	}
	if impl.Vendor != "claude" || impl.Model != "sonnet" || impl.Effort != "high" || rev.Model != "opus" || gate.Model != "opus" {
		t.Errorf("lineup not applied: %+v %+v %+v", impl, rev, gate)
	}
	if rec.Contract != 10 || rec.Item != item || rec.Worktree != h.W || rec.Branch != item || rec.RelayRun != "run-1" || rec.Project != "ork" {
		t.Errorf("record: %+v", rec)
	}
	if len(rec.Roles) != 3 || len(rec.Checks) != 1 || !rec.Checks[0].OK || rec.Checks[0].Command != "go test ./..." {
		t.Errorf("roles %d, checks %+v", len(rec.Roles), rec.Checks)
	}
	if len(h.checks.calls) != 1 || h.checks.calls[0] != h.W+"|go test ./..." {
		t.Errorf("check command calls: %v", h.checks.calls)
	}
	for i, role := range rec.Roles {
		if role.PGID != 4000+i || role.Session == "" || role.Exit != "ok" || role.Running() || role.Tokens != 1000 || role.Report == "" || role.Step == "" {
			t.Errorf("role record %d: %+v", i, role)
		}
	}
	if len(h.calls("slicer", "relay-run", "end")) != 0 || rec.Commit != nil {
		t.Errorf("the run must not end or commit before the owner approves")
	}
	if got := h.readReport(rec, "implementer-1.md"); got != implReport {
		t.Errorf("report file %q", got)
	}
}

func TestSetupRunsGitWorktreeAddAndExcludesTheWorktreeDirectory(t *testing.T) {
	h := newHarness(t)
	h.start()
	git := h.fake.CallsTo("git")
	if len(git) < 2 || join(git[0].Args) != "-C "+h.P+" rev-parse --git-path info/exclude" || join(git[1].Args) != "-C "+h.P+" worktree add "+h.W+" -b S04" {
		t.Fatalf("git calls: %+v", git)
	}
	b, err := os.ReadFile(filepath.Join(h.P, ".git", "info", "exclude"))
	if err != nil || string(b) != ".worktrees/\n" {
		t.Fatalf("info/exclude: %q, %v", b, err)
	}
	begin := h.calls("slicer", "relay-run", "begin")
	if len(begin) != 1 || join(begin[0]) != "relay-run begin S04 --worktree "+h.W+" --json --root "+h.W {
		t.Fatalf("begin: %v", begin)
	}
}

func TestSetupPatternsAreApplied(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.project = func(p *registry.Project) { p.WorktreePattern = "wt/{id}-x"; p.BranchPattern = "feat/{id}" }
	})
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	wt := filepath.Join(h.P, "wt", "S04-x")
	if h.W != wt {
		t.Fatalf("harness worktree %q", h.W)
	}
	add := h.calls("git", "-C", h.P, "worktree")
	if len(add) != 1 || !reflect.DeepEqual(add[0], []string{"-C", h.P, "worktree", "add", wt, "-b", "feat/S04"}) {
		t.Fatalf("worktree add: %v", add)
	}
	if rec.Worktree != wt || rec.Branch != "feat/S04" {
		t.Fatalf("record: %q %q", rec.Worktree, rec.Branch)
	}
	// An absolute pattern is used as it is.
	if got := worktreePath(registry.Project{Path: "/p", WorktreePattern: "/elsewhere/{id}"}, "S04"); got != "/elsewhere/S04" {
		t.Errorf("absolute pattern: %q", got)
	}
	if got := worktreePath(registry.Project{Path: "/p"}, "S04"); got != "/p/.worktrees/S04" {
		t.Errorf("default pattern: %q", got)
	}
	if got := branchName(registry.Project{}, "S04"); got != "S04" {
		t.Errorf("default branch: %q", got)
	}
}

func TestSetupAddsTheExcludeLineOnlyWhenMissing(t *testing.T) {
	for name, tc := range map[string]struct{ before, after string }{
		"no file":             {"", ".worktrees/\n"},
		"empty file":          {"\n", "\n.worktrees/\n"},
		"other lines":         {"*.log\n", "*.log\n.worktrees/\n"},
		"no trailing newline": {"*.log", "*.log\n.worktrees/\n"},
		"already there":       {"a\n.worktrees/\nb\n", "a\n.worktrees/\nb\n"},
		"already, slashed":    {"/.worktrees/\n", "/.worktrees/\n"},
		"already, bare":       {".worktrees\n", ".worktrees\n"},
		"commented":           {"# .worktrees/\n", "# .worktrees/\n.worktrees/\n"},
	} {
		t.Run(name, func(t *testing.T) {
			before := tc.before
			h := newHarness(t, func(o *harnessOpts) {
				if name != "no file" {
					o.exclude = &before
				}
			})
			h.start()
			b, _ := os.ReadFile(filepath.Join(h.P, ".git", "info", "exclude"))
			if string(b) != tc.after {
				t.Fatalf("exclude file %q, want %q", b, tc.after)
			}
			var excludeWrites int
			for _, w := range h.runWrites() {
				if w.Tool == "git" && len(w.Argv) > 0 && w.Argv[0] == "exclude" {
					excludeWrites++
				}
			}
			if want := map[bool]int{true: 1, false: 0}[tc.before != tc.after]; excludeWrites != want {
				t.Fatalf("%d exclude writes in writes[], want %d", excludeWrites, want)
			}
		})
	}
}

func (h *harness) runWrites() []runrec.Write {
	runs, _ := h.store.List()
	if len(runs) == 0 {
		return nil
	}
	return runs[len(runs)-1].Snapshot().Writes
}

func TestEveryRoleRoundIsALedgerStepUnderTheOrchestratorStepOrkOwns(t *testing.T) {
	h := newHarness(t)
	h.start()
	steps := h.steps()
	if len(steps) != 8 {
		t.Fatalf("%d step calls: %+v", len(steps), steps)
	}
	orch := steps[0]
	if orch.Role != "orchestrator" || orch.Agent != "ork" || orch.Vendor != "" || orch.Model != "" || orch.Reasoning != "" || orch.Parent != "" || orch.Phase != "start" || orch.Task != "S04 orchestrator" {
		t.Errorf("orchestrator step: %+v", orch)
	}
	// Step ids are the call ordinals: orchestrator 1, implementer 2 (done is
	// call 3), reviewer 4 (done 5), gate 6 (done 7).
	for i, want := range []struct{ role, task, id string }{
		{"implementer", "S04 implementer", "2"}, {"reviewer", "S04 reviewer", "4"}, {"gate", "S04 gate", "6"},
	} {
		start, done := steps[1+2*i], steps[2+2*i]
		if start.Role != want.role || start.Phase != "start" || start.Parent != "1" || start.Task != want.task || start.Agent == "" {
			t.Errorf("%s start: %+v", want.role, start)
		}
		if done.Phase != "done" || done.Step != want.id || done.Role != "" || done.Parent != "" {
			t.Errorf("%s done: %+v (want phase done on step %s)", want.role, done, want.id)
		}
	}
	if s := steps[1]; s.Vendor != "claude" || s.Model != "sonnet" || s.Reasoning != "high" {
		t.Errorf("implementer step lineup: %+v", s)
	}
	rec := h.engineRecord()
	if rec.OrchestratorStep != "1" || rec.Roles[0].Step != "2" || rec.Roles[1].Step != "4" || rec.Roles[2].Step != "6" {
		t.Errorf("step ids in the record: %q %+v", rec.OrchestratorStep, rec.Roles)
	}
}

func (h *harness) engineRecord() runrec.Record {
	runs, err := h.store.List()
	if err != nil || len(runs) == 0 {
		h.t.Fatalf("no run record: %v", err)
	}
	return runs[len(runs)-1].Snapshot()
}

func TestAFailedRoleStepIsMarkedFail(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		return Result{Exit: ExitError, Report: implReport}, nil
	}
	e := h.start()
	requireState(t, e, runrec.StateStopped)
	steps := h.steps()
	last := steps[len(steps)-1]
	if last.Phase != "fail" || last.Step != "2" {
		t.Fatalf("the implementer step must end with phase fail: %+v", steps)
	}
}

func TestLedgerBeginFailureIsAdvisory(t *testing.T) {
	var h *harness
	h = newHarness(t)
	failBegin := clitest.Route{Tool: "slicer", Args: []string{"relay-run", "begin"}, Prefix: true,
		Stdout: `{"error":{"code":"state","message":"no ledger","command":"relay-run"}}`, Exit: 3}
	h = newHarness(t, func(o *harnessOpts) { o.routes = []clitest.Route{failBegin} })
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if rec.RelayRun != "" || len(h.calls("slicer", "relay-run", "step")) != 0 {
		t.Fatalf("without a run id nothing can be recorded: %+v", rec)
	}
	if got := h.runner.roles(); len(got) != 3 {
		t.Fatalf("the relay must go on: %v", got)
	}
	var failed int
	for _, w := range rec.Writes {
		if w.Tool == "slicer" && w.Exit == 3 && w.Code == "state" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("the failed begin must still be in writes[]: %+v", rec.Writes)
	}
}

func TestRedChecksSendTheImplementerBackWithoutSpendingAReview(t *testing.T) {
	h := newHarness(t)
	h.checks.outputs = []CheckOutput{{OK: false, Output: "--- FAIL: TestGate\n    gate_test.go:12: boom\nFAIL\n"}, {OK: true, Output: "ok\n"}}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "implementer-2", "reviewer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	fix := h.runner.requests()[1]
	if fix.Resume != "sess-implementer" {
		t.Errorf("the fix round resumes the implementer: %+v", fix.Resume)
	}
	if !strings.Contains(fix.Brief, "--- FAIL: TestGate") || !strings.Contains(fix.Brief, "gate_test.go:12: boom") {
		t.Errorf("the failing output is not in the brief:\n%s", fix.Brief)
	}
	if len(rec.Checks) != 2 || rec.Checks[0].OK || !rec.Checks[1].OK || rec.Checks[0].Round != 1 || rec.Checks[1].Round != 2 {
		t.Errorf("checks: %+v", rec.Checks)
	}
	if rec.Checks[0].Log != "checks-1.log" || !strings.Contains(h.readReport(rec, "checks-1.log"), "boom") {
		t.Errorf("check log: %+v", rec.Checks[0])
	}
	if rec.Round != 2 || rec.Fails != 0 {
		t.Errorf("round %d, fails %d", rec.Round, rec.Fails)
	}
	if steps := h.steps(); steps[3].Task != "S04 implementer fix round 1" {
		t.Errorf("fix round task: %+v", steps[3])
	}
}

func TestRedSlicerCheckAlsoSendsTheImplementerBack(t *testing.T) {
	red := clitest.Route{Tool: "slicer", Args: []string{"check", "--json"}, Prefix: true,
		Stdout: `{"ok":false,"stale_render":["ROADMAP.md"],"stale_sync":[],"problems":[],"warnings":[]}`, Exit: 1}
	green := clitest.Reply{Stdout: `{"ok":true,"stale_render":[],"stale_sync":[],"problems":[],"warnings":[]}`}
	red.Seq = []clitest.Reply{{Stdout: red.Stdout, Exit: 1}, green}
	red.Stdout = ""
	h := newHarness(t, func(o *harnessOpts) { o.routes = []clitest.Route{red} })
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	if got := h.runner.roles(); !reflect.DeepEqual(got, []string{"implementer-1", "implementer-2", "reviewer-2", "gate-2"}) {
		t.Fatalf("roles %v", got)
	}
	if brief := h.runner.requests()[1].Brief; !strings.Contains(brief, "ROADMAP.md") || !strings.Contains(brief, "slicer check") {
		t.Errorf("the slicer check output is not in the brief:\n%s", brief)
	}
	if rec := e.Snapshot(); rec.Checks[0].OK {
		t.Errorf("checks[0] must be red: %+v", rec.Checks[0])
	}
}

func TestChecksWithoutACheckCommandRunOnlySlicerCheck(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) { o.project = func(p *registry.Project) { p.CheckCommand = "" } })
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	if len(h.checks.calls) != 0 || len(h.calls("slicer", "check")) != 1 {
		t.Fatalf("check calls %v, slicer check %v", h.checks.calls, h.calls("slicer", "check"))
	}
	if req := h.runner.requests()[0]; len(req.Allowlist) != 21 {
		t.Errorf("allowlist without a check command: %v", req.Allowlist)
	}
}

func TestReviewFindingsStartAFixRoundAndTheResumedReviewerGatesIt(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleReviewer {
			return Result{Exit: ExitOK, Session: "sess-reviewer", Report: findingsReview("1. engine.go:42 drops the error.")}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "implementer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	reqs := h.runner.requests()
	if fix := reqs[2]; fix.Resume != "sess-implementer" || !strings.Contains(fix.Brief, "engine.go:42 drops the error.") {
		t.Errorf("fix round: resume %q\n%s", fix.Resume, fix.Brief)
	}
	if gate := reqs[3]; gate.Resume != "sess-reviewer" || !strings.Contains(gate.Brief, "gate") {
		t.Errorf("a small fix round is reviewed and gated in one pass by the resumed reviewer: %+v", gate.Resume)
	}
	if rec.Fails != 0 || len(h.calls("slicer", "note")) != 0 {
		t.Errorf("review findings are not a gate FAIL: fails %d, notes %v", rec.Fails, h.calls("slicer", "note"))
	}
}

func TestGateFailIsNotedAndStartsAFixRound(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate && req.Round == 1 {
			return Result{Exit: ExitOK, Session: "sess-gate", Report: gateFail}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "gate-1", "implementer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	notes := h.calls("slicer", "note")
	if len(notes) != 1 || len(notes[0]) != 8 || notes[0][1] != item || notes[0][2] != "--text" || !strings.HasPrefix(notes[0][3], "Review FAIL: ") || !strings.Contains(notes[0][3], "engine.go:10 missing nil check") {
		t.Fatalf("notes: %q", notes)
	}
	if notes[0][4] != "--render" || notes[0][5] != "--json" || notes[0][6] != "--root" || notes[0][7] != h.W {
		t.Errorf("note flags: %q", notes[0])
	}
	reqs := h.runner.requests()
	if fix := reqs[3]; fix.Resume != "sess-implementer" || !strings.Contains(fix.Brief, "engine.go:10 missing nil check") {
		t.Errorf("fix round after FAIL: %+v", fix.Resume)
	}
	if again := reqs[4]; again.Role != RoleGate || again.Resume == "" {
		t.Errorf("the gate runs again after the fix: %+v", again)
	}
	if rec.Fails != 1 {
		t.Errorf("fails %d", rec.Fails)
	}
}

func TestAThirdGateFailStopsTheSlice(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate {
			return Result{Exit: ExitOK, Session: "sess-gate", Report: gateFail}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateStopped)
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "gate-1", "implementer-2", "gate-2", "implementer-3", "gate-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	if rec.Fails != 3 || !strings.Contains(rec.Reason, "3") || !strings.Contains(rec.Reason, "gate") {
		t.Errorf("fails %d, reason %q", rec.Fails, rec.Reason)
	}
	if n := len(h.calls("slicer", "note")); n != 3 {
		t.Errorf("%d notes, want one per FAIL", n)
	}
	end := h.calls("slicer", "relay-run", "end")
	if len(end) != 1 || join(end[0]) != "relay-run end run-1 --outcome stopped --close-steps fail --json --root "+h.W {
		t.Errorf("relay-run end: %v", end)
	}
}

func TestTwoFailsThenPassIsNotAStop(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate && req.Round < 3 {
			return Result{Exit: ExitOK, Session: "sess-gate", Report: gateFail}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if rec.Fails != 2 {
		t.Errorf("fails %d", rec.Fails)
	}
}

func TestTheRoundLimitStopsARedLoop(t *testing.T) {
	h := newHarness(t)
	h.deps.MaxRounds = 3
	h.checks.outputs = []CheckOutput{{OK: false, Output: "red\n"}}
	e := h.start()
	rec := requireState(t, e, runrec.StateStopped)
	if got := h.runner.roles(); len(got) != 3 || !strings.Contains(rec.Reason, "round") {
		t.Fatalf("roles %v, reason %q", got, rec.Reason)
	}
}

func TestLockedWritesAreRetriedWithBackoffAndLogged(t *testing.T) {
	locked := clitest.Reply{Stdout: `{"error":{"code":"locked","message":"busy","command":"note"}}`, Exit: 3}
	h := newHarness(t, func(o *harnessOpts) {
		o.routes = []clitest.Route{{Tool: "slicer", Args: []string{"note", item}, Prefix: true,
			Seq: []clitest.Reply{locked, locked, {Stdout: `{"id":"S04","added":true}`}}}}
	})
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate && req.Round == 1 {
			return Result{Exit: ExitOK, Session: "sess-gate", Report: gateFail}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if n := len(h.calls("slicer", "note")); n != 3 {
		t.Fatalf("%d note calls, want 3", n)
	}
	if !reflect.DeepEqual(h.sleep.delays, h.deps.Backoff[:2]) {
		t.Errorf("delays %v", h.sleep.delays)
	}
	var notes []runrec.Write
	for _, w := range rec.Writes {
		if w.Tool == "slicer" && len(w.Argv) > 0 && w.Argv[0] == "note" {
			notes = append(notes, w)
		}
	}
	if len(notes) != 3 || notes[0].Code != "locked" || notes[0].Retry != 0 || notes[1].Retry != 1 || notes[1].Code != "locked" || notes[2].Retry != 2 || notes[2].Exit != 0 || notes[2].Code != "" {
		t.Fatalf("note writes: %+v", notes)
	}
}

func TestEveryWriteIsInWritesAndNoReadIs(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate && req.Round == 1 {
			return Result{Exit: ExitOK, Session: "sess-gate", Report: gateFail}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateCommitted)

	isWrite := func(tool string, args []string) bool {
		if tool == "git" {
			a := args[2:] // after -C DIR
			switch a[0] {
			case "worktree", "add", "commit", "merge", "branch", "push":
				return true
			}
			return false
		}
		switch args[0] {
		case "note", "land", "render":
			return true
		case "relay-run":
			return args[1] != "list"
		}
		return false
	}
	strip := func(args []string) []string {
		var out []string
		for i := 0; i < len(args); i++ {
			if (args[i] == "--root" || args[i] == "-C") && i+1 < len(args) {
				i++
				continue
			}
			out = append(out, args[i])
		}
		return out
	}
	var want [][]string
	for _, c := range h.fake.Calls() {
		if isWrite(c.Tool, c.Args) {
			want = append(want, append([]string{c.Tool}, strip(c.Args)...))
		}
	}
	var got [][]string
	for _, w := range rec.Writes {
		if w.Tool == "git" && w.Argv[0] == "exclude" {
			continue // ork's own edit of info/exclude, not a git call
		}
		got = append(got, append([]string{w.Tool}, w.Argv...))
		if w.Root == "" || w.At.IsZero() {
			t.Errorf("incomplete write: %+v", w)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("writes[] differs from the calls that change something\n got: %q\nwant: %q", got, want)
	}
	if len(got) < 14 {
		t.Fatalf("only %d writes: %q", len(got), got)
	}
}

func TestContractCheckAcceptsTheSetAndRefusesOthers(t *testing.T) {
	for line, wantErr := range map[string]string{
		"Contract: relay v7":   "v7",
		"Contract: relay v8":   "v8",
		"Contract: relay v9":   "v9",
		"Contract: relay v10":  "",
		"Contract: relay v11":  "",
		"Contract: relay v12":  "v12",
		"Contract: relay v6":   "v6",
		"something else":       "contract line",
		"Contract: relay v7 x": "contract line",
	} {
		h := newHarness(t, func(o *harnessOpts) {
			o.routesFor = func(P, W string) []clitest.Route {
				file, recordedAs := "relay-v10.json", `"contract": "relay v10"`
				if line == "Contract: relay v11" || line == "Contract: relay v12" {
					file, recordedAs = "relay-v11.json", `"contract": "relay v11"`
				}
				raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cli", "slicer", file))
				if err != nil {
					t.Fatal(err)
				}
				return []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--root", P}, Stdout: line + "\n"}, {Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: strings.Replace(string(raw), recordedAs, `"contract": "`+strings.TrimPrefix(line, "Contract: ")+`"`, 1)}}
			}
		})
		_, err := New(context.Background(), h.deps, h.cfg)
		if wantErr == "" {
			if err != nil {
				t.Errorf("%q: %v", line, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: error %v, want one mentioning %q", line, err, wantErr)
		}
		if n := len(h.fake.CallsTo("git")); n != 0 {
			t.Errorf("%q: %d git calls before the contract was accepted", line, n)
		}
		if runs, _ := h.store.List(); len(runs) != 0 {
			t.Errorf("%q: a run record was created", line)
		}
	}
}

func TestNewRecordsTheRunAndRefusesBadInput(t *testing.T) {
	h := newHarness(t)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := e.Snapshot()
	if rec.ID != "ork-20261019-161201-a1b2" || rec.State != runrec.StateSetup || rec.Contract != 10 || rec.Round != 1 || rec.Item != item {
		t.Fatalf("record: %+v", rec)
	}
	if !reflect.DeepEqual(rec.Lineup, h.cfg.Lineup) || rec.Worktree != h.W || rec.Branch != item {
		t.Fatalf("lineup/worktree: %+v", rec)
	}
	if len(h.fake.CallsTo("git")) != 0 {
		t.Errorf("New must change nothing in the project")
	}

	for name, edit := range map[string]func(*Config){
		"no item":        func(c *Config) { c.Item = "" },
		"no implementer": func(c *Config) { delete(c.Lineup, RoleImplementer) },
		"no reviewer":    func(c *Config) { delete(c.Lineup, RoleReviewer) },
		"reviewer with a list": func(c *Config) {
			c.Project.Permissions = map[string]registry.Permission{"reviewer": {Allow: []string{"Edit"}}}
		},
	} {
		cfg := h.cfg
		cfg.Lineup = map[string][]runrec.LineupEntry{}
		for k, v := range h.cfg.Lineup {
			cfg.Lineup[k] = v
		}
		edit(&cfg)
		if _, err := New(context.Background(), h.deps, cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestNewBudgetsOverrideFallsBackPerField(t *testing.T) {
	for name, tc := range map[string]struct {
		override registry.Budgets
		want     registry.Budgets
	}{
		"unset":   {registry.Budgets{}, registry.Budgets{Minutes: 60, Tokens: 10000}},
		"minutes": {registry.Budgets{Minutes: 90}, registry.Budgets{Minutes: 90, Tokens: 10000}},
		"tokens":  {registry.Budgets{Tokens: 20000}, registry.Budgets{Minutes: 60, Tokens: 20000}},
		"both":    {registry.Budgets{Minutes: 90, Tokens: 20000}, registry.Budgets{Minutes: 90, Tokens: 20000}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.cfg.Project.Budgets = registry.Budgets{Minutes: 60, Tokens: 10000}
			h.cfg.Budgets = tc.override
			e, err := New(context.Background(), h.deps, h.cfg)
			if err != nil {
				t.Fatal(err)
			}
			run, err := h.store.Open(e.Snapshot().ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := run.Snapshot().Budgets; got != tc.want {
				t.Fatalf("recorded budgets: got %+v, want %+v", got, tc.want)
			}
			if h.cfg.Project.Budgets != (registry.Budgets{Minutes: 60, Tokens: 10000}) {
				t.Fatal("override changed the project budgets")
			}
		})
	}
}

func TestAdjAskRunsInTheMainCheckoutNotTheWorktree(t *testing.T) {
	h := newHarness(t)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if e.AdjRoot() != h.P || e.AdjRoot() == e.Snapshot().Worktree {
		t.Fatalf("AdjRoot %q, project %q, worktree %q", e.AdjRoot(), h.P, e.Snapshot().Worktree)
	}
}

func TestTheRunRecordOnDiskFollowsTheEngine(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	run, err := h.store.Open(e.Snapshot().ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(run.Snapshot(), e.Snapshot()) {
		t.Fatalf("disk and memory differ")
	}
}

func TestRunnerErrorIsAnExitOfClassError(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) { return Result{}, errors.New("claude: not found") }
	e := h.start()
	rec := requireState(t, e, runrec.StateStopped)
	if !strings.Contains(rec.Reason, "claude: not found") || rec.Roles[0].Exit != "error" || rec.Roles[0].Running() {
		t.Fatalf("reason %q, role %+v", rec.Reason, rec.Roles[0])
	}
}

func TestChecksLogIsWrittenAt0600EvenOverAnExistingFile(t *testing.T) {
	h := newHarness(t)
	h.checks.outputs = []CheckOutput{{OK: true, Output: "ok\n"}}
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.store.RunDir(e.Snapshot().ID), "checks-1.log")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("checks log mode %v, %v; want 600", fi, err)
	}
	if b, _ := os.ReadFile(path); string(b) == "old" {
		t.Fatalf("the checks log was not rewritten")
	}
}

func TestPatternIdLower(t *testing.T) {
	for _, tc := range []struct{ pattern, want string }{
		{".worktrees/{id_lower}", ".worktrees/s78"},
		{".worktrees/{id}", ".worktrees/S78"},
		{"{id}/{id_lower}/{id_lower}/{other}", "S78/s78/s78/{other}"},
		{"Fixed/{id_lower}", "Fixed/s78"},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			p := registry.Project{Path: "/project", WorktreePattern: tc.pattern, BranchPattern: tc.pattern}
			if got := worktreePath(p, "S78"); got != filepath.Join(p.Path, tc.want) {
				t.Errorf("worktree = %q, want %q", got, filepath.Join(p.Path, tc.want))
			}
			if got := branchName(p, "S78"); got != tc.want {
				t.Errorf("branch = %q, want %q", got, tc.want)
			}
		})
	}
}
