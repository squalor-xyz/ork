// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
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
	red := CheckOutput{OK: false, Output: "--- FAIL: TestGate\n    gate_test.go:12: boom\nFAIL\n"}
	h.checks.outputs = []CheckOutput{red, red, {OK: true, Output: "ok\n"}}
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

// outsideProblem is the finding of the 2026-11-04 wave: the tracking text of
// an item that landed on main, checked against code that lacks its flag.
const outsideProblem = "s443 / Why: unknown flag --stdin in `slicer relay-roles apply --stdin`"

// slicerCheckJSON is `slicer check --json` output with the given problems and
// stale-render, stale-sync and orphan-render lists.
func slicerCheckJSON(t *testing.T, problems []any, stale, sync, orphan []string) string {
	t.Helper()
	if stale == nil {
		stale = []string{}
	}
	if sync == nil {
		sync = []string{}
	}
	if orphan == nil {
		orphan = []string{}
	}
	b, err := json.Marshal(map[string]any{"ok": false, "stale_render": stale, "orphan_render": orphan,
		"stale_sync": sync, "problems": problems, "warnings": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// redThenGreenSlicerCheck answers the first `slicer check` with red and the
// rest with green.
func redThenGreenSlicerCheck(W, red string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"check", "--json", "--root", W}, Seq: []clitest.Reply{
		{Stdout: red, Exit: 1},
		{Stdout: `{"ok":true,"stale_render":[],"orphan_render":[],"stale_sync":[],"problems":[],"warnings":[]}`},
	}}
}

// branchIs answers the worktree's "is main in my branch" question: behind is
// the branch lacking main, which git says with exit 1.
func branchIs(W string, behind bool) clitest.Route {
	r := clitest.Route{Tool: "git", Args: []string{"-C", W, "merge-base", "--is-ancestor", "main", "HEAD"}}
	if behind {
		r.Exit = 1
	}
	return r
}

func TestOutsideProblemsOnABehindBranchDoNotMakeTheRoundRed(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, w string) []clitest.Route {
			return []clitest.Route{branchIs(w, true), {Tool: "slicer", Args: []string{"check", "--json", "--root", w},
				Stdout: slicerCheckJSON(t, []any{outsideProblem}, nil, nil, nil), Exit: 1}}
		}
	})
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "gate-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v (no second implementer round)", got, want)
	}
	if len(rec.Checks) != 1 || !rec.Checks[0].OK || !reflect.DeepEqual(rec.Checks[0].Deferred, []string{outsideProblem}) {
		t.Fatalf("checks: %+v", rec.Checks)
	}
	log := h.readReport(rec, "checks-1.log")
	if want := "deferred to land (branch behind main): " + outsideProblem + "\n"; !strings.Contains(log, want) {
		t.Errorf("the check log lacks the line %q:\n%s", want, log)
	}
	if strings.Index(log, "deferred to land") < strings.Index(log, "$ slicer check --json") {
		t.Errorf("the deferred lines follow the slicer output:\n%s", log)
	}
	if rec.Round != 1 {
		t.Errorf("round %d", rec.Round)
	}
}

func TestADeferralNeverTouchesTheProjectCheck(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, w string) []clitest.Route {
			return []clitest.Route{branchIs(w, true), redThenGreenSlicerCheck(w, slicerCheckJSON(t, []any{outsideProblem}, nil, nil, nil))}
		}
	})
	h.checks.outputs = []CheckOutput{{OK: false, Output: "--- FAIL: TestGate\nFAIL\n"}, {OK: false, Output: "--- FAIL: TestGate\nFAIL\n"}, {OK: true, Output: "ok\n"}}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "implementer-2", "reviewer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a red project check still sends the implementer back: %v, want %v", got, want)
	}
	if rec.Checks[0].OK || !reflect.DeepEqual(rec.Checks[0].Deferred, []string{outsideProblem}) {
		t.Errorf("checks[0]: %+v", rec.Checks[0])
	}
}

func TestAProblemOnTheSlicesOwnItemStaysRed(t *testing.T) {
	for name, own := range map[string]string{
		"same-case":  item + ": slice file has no index row",
		"other-case": strings.ToLower(item) + " / Why: unknown flag --x in `slicer y --x`",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.routesFor = func(P, w string) []clitest.Route {
					return []clitest.Route{branchIs(w, true), redThenGreenSlicerCheck(w, slicerCheckJSON(t, []any{own, outsideProblem}, nil, nil, nil))}
				}
			})
			e := h.start()
			rec := requireState(t, e, runrec.StateAwaitingApproval)
			if got, want := h.runner.roles(), []string{"implementer-1", "implementer-2", "reviewer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("role rounds %v, want %v", got, want)
			}
			if rec.Checks[0].OK || len(rec.Checks[0].Deferred) != 0 {
				t.Errorf("checks[0] must be red with nothing deferred: %+v", rec.Checks[0])
			}
			if strings.Contains(h.readReport(rec, "checks-1.log"), "deferred to land") {
				t.Errorf("a red round defers nothing")
			}
		})
	}
}

func TestOutsideProblemsOnAnUpToDateBranchStayRed(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, w string) []clitest.Route {
			return []clitest.Route{branchIs(w, false), redThenGreenSlicerCheck(w, slicerCheckJSON(t, []any{outsideProblem}, nil, nil, nil))}
		}
	})
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "implementer-2", "reviewer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	if rec.Checks[0].OK || len(rec.Checks[0].Deferred) != 0 {
		t.Errorf("checks[0]: %+v", rec.Checks[0])
	}
}

func TestAGitErrorOnTheBaseCheckKeepsTheRoundRed(t *testing.T) {
	for name, route := range map[string]func(P, W string) clitest.Route{
		"merge-base": func(P, W string) clitest.Route {
			return clitest.Route{Tool: "git", Args: []string{"-C", W, "merge-base", "--is-ancestor", "main", "HEAD"}, Exit: 128, Stderr: "fatal: Not a valid object name main"}
		},
		"base-branch": func(P, W string) clitest.Route {
			// Stdout is set on purpose: the error alone must keep the round red.
			return clitest.Route{Tool: "git", Args: []string{"-C", P, "symbolic-ref", "--short", "HEAD"}, Stdout: "main\n", Exit: 128, Stderr: "fatal: ref HEAD is not a symbolic ref"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.routesFor = func(P, w string) []clitest.Route {
					return []clitest.Route{route(P, w), branchIs(w, true), redThenGreenSlicerCheck(w, slicerCheckJSON(t, []any{outsideProblem}, nil, nil, nil))}
				}
			})
			e := h.start()
			rec := requireState(t, e, runrec.StateAwaitingApproval)
			if got, want := h.runner.roles(), []string{"implementer-1", "implementer-2", "reviewer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("role rounds %v, want %v", got, want)
			}
			if rec.Checks[0].OK || len(rec.Checks[0].Deferred) != 0 {
				t.Errorf("checks[0]: %+v", rec.Checks[0])
			}
		})
	}
}

func TestAProblemWithNoItemOrAStaleRenderStaysRed(t *testing.T) {
	cases := map[string]string{
		"no-item":      slicerCheckJSON(t, []any{outsideProblem, "dependency data is unreadable"}, nil, nil, nil),
		"only-no-item": slicerCheckJSON(t, []any{"dependency data is unreadable"}, nil, nil, nil),
		"stale-render": slicerCheckJSON(t, []any{outsideProblem}, []string{"ROADMAP.md"}, nil, nil),
		"stale-sync":   slicerCheckJSON(t, []any{outsideProblem}, nil, []string{"AGENTS.md: out of date"}, nil),
		"orphan":       slicerCheckJSON(t, []any{outsideProblem}, nil, nil, []string{"S99.md"}),
		"not-a-string": slicerCheckJSON(t, []any{outsideProblem, map[string]string{"item": "s443"}}, nil, nil, nil),
		"no-problems":  slicerCheckJSON(t, []any{}, nil, nil, nil),
	}
	for name, red := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.routesFor = func(P, w string) []clitest.Route {
					return []clitest.Route{branchIs(w, true), redThenGreenSlicerCheck(w, red)}
				}
			})
			e := h.start()
			rec := requireState(t, e, runrec.StateAwaitingApproval)
			if got, want := h.runner.roles(), []string{"implementer-1", "implementer-2", "reviewer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("role rounds %v, want %v", got, want)
			}
			if rec.Checks[0].OK || len(rec.Checks[0].Deferred) != 0 {
				t.Errorf("checks[0]: %+v", rec.Checks[0])
			}
			if reads := h.calls("git", "-C", h.P, "symbolic-ref"); len(reads) != 0 {
				t.Errorf("git is read only when the report could defer: %v", reads)
			}
		})
	}
}

func TestTheLandingRecheckDoesNotDefer(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, w string) []clitest.Route {
			return []clitest.Route{branchIs(w, true), {Tool: "slicer", Args: []string{"check", "--json", "--root", w},
				Stdout: slicerCheckJSON(t, []any{outsideProblem}, nil, nil, nil), Exit: 1}}
		}
	})
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := e.Snapshot()
	rec.Worktree, rec.Item = h.W, item
	run, paused, err := e.runChecks(context.Background(), rec, runrec.StateLand, "land-checks")
	if err != nil || paused {
		t.Fatalf("paused %v, err %v", paused, err)
	}
	if run.ok {
		t.Errorf("the landing re-check decides the problem and never defers it")
	}
	if got := e.Snapshot().Checks; len(got) != 1 || got[0].OK || len(got[0].Deferred) != 0 {
		t.Errorf("checks: %+v", got)
	}
	// The same call as a round check defers: the state is the only difference.
	run, _, err = e.runChecks(context.Background(), rec, runrec.StateChecks, "checks")
	if err != nil || !run.ok {
		t.Errorf("as a round check it defers: ok %v, err %v", run.ok, err)
	}
}

func TestReviewerAndGateBriefsNameTheDeferredProblems(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, w string) []clitest.Route {
			return []clitest.Route{branchIs(w, true), {Tool: "slicer", Args: []string{"check", "--json", "--root", w},
				Stdout: slicerCheckJSON(t, []any{outsideProblem}, nil, nil, nil), Exit: 1}}
		}
	})
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	for _, req := range h.runner.requests() {
		if req.Role != RoleReviewer && req.Role != RoleGate {
			if strings.Contains(req.Brief, outsideProblem) {
				t.Errorf("the %s brief does not carry the deferred problems", req.Role)
			}
			continue
		}
		if !strings.Contains(req.Brief, outsideProblem) || !strings.Contains(req.Brief, "the landing merge") {
			t.Errorf("the %s brief must name the deferred problem and say the landing merge decides it:\n%s", req.Role, req.Brief)
		}
		if !strings.Contains(req.Brief, "name them in your report as deferred problems, not as findings") {
			t.Errorf("the %s brief must ask for the problems to be named as deferred, not as findings:\n%s", req.Role, req.Brief)
		}
	}
}

func TestTheFinishBriefNamesTheDeferredProblems(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, w string) []clitest.Route {
			return []clitest.Route{branchIs(w, true), {Tool: "slicer", Args: []string{"check", "--json", "--root", w},
				Stdout: slicerCheckJSON(t, []any{outsideProblem}, nil, nil, nil), Exit: 1}}
		}
	})
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	_ = e.Approve(context.Background()) // the landing after the finish step is not under test
	var finish *Request
	for _, req := range h.runner.requests() {
		if strings.Contains(req.Brief, "## Finish step") {
			finish = &req
		}
	}
	if finish == nil {
		t.Fatal("no finish round ran")
	}
	for _, want := range []string{outsideProblem, "--check", "expected", "not an Owner needed item"} {
		if !strings.Contains(finish.Brief, want) {
			t.Errorf("the finish brief lacks %q:\n%s", want, finishSection(t, finish.Brief))
		}
	}
}

func TestTheFinishBriefHasNoDeferredSectionWhenNothingIsDeferred(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	_ = e.Approve(context.Background())
	var finish string
	for _, req := range h.runner.requests() {
		if strings.Contains(req.Brief, "## Finish step") {
			finish = req.Brief
		}
	}
	if finish == "" || strings.Contains(finish, "Deferred tracking problems") {
		t.Errorf("finish brief: %q", finish)
	}
}

func TestABriefWithNoDeferredProblemsHasNoSection(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	for _, req := range h.runner.requests() {
		if strings.Contains(req.Brief, "the landing merge") {
			t.Errorf("the %s brief has a deferred section with nothing deferred", req.Role)
		}
	}
}

func TestProblemItemReadsTheLeadingID(t *testing.T) {
	for problem, want := range map[string]string{
		"s443 / Why: unknown flag --stdin": "s443",
		"S12: missing the Files section":   "S12",
		"S12 / Files: bad path: x":         "S12",
		"x1.2-b_c: odd id":                 "x1.2-b_c",
		"render is stale":                  "",
		"stale render: ROADMAP.md":         "",
		"":                                 "",
		": no id":                          "",
		"/ Why: x":                         "",
	} {
		if got := problemItem(problem); got != want {
			t.Errorf("problemItem(%q) = %q, want %q", problem, got, want)
		}
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

func TestGateFailsStillStopAtThree(t *testing.T) {
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

func TestTheRoundLimitPausesARedLoop(t *testing.T) {
	h := redLoop(t, true, true)
	h.deps.MaxRounds = 3
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if got := h.runner.roles(); len(got) != 3 || !strings.Contains(rec.Reason, "round") {
		t.Fatalf("roles %v, reason %q", got, rec.Reason)
	}
}

// redLoop scripts tracked content and check output independently. The diff
// changes while status remains the same, as for an already modified file.
func redLoop(t *testing.T, changedTree, changedOutput bool) *harness {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			var seq []clitest.Reply
			for n := 1; n <= 12; n++ {
				content := 1
				if changedTree {
					content = n
				}
				seq = append(seq, clitest.Reply{Stdout: fmt.Sprintf("diff --git a/a.go b/a.go\n-old\n+value%d\n", content)})
			}
			return []clitest.Route{
				{Tool: "git", Args: []string{"-C", W, "diff", "HEAD", "--binary", "--no-ext-diff"}, Seq: seq},
				{Tool: "git", Args: []string{"-C", W, "ls-files", "--others", "--exclude-standard", "-z"}},
			}
		}
	})
	for n := 1; n <= 12; n++ {
		output := "broken\n"
		if changedOutput {
			output = fmt.Sprintf("broken %d\n", n)
		}
		// Each red round is run twice: the rerun gets the same output.
		h.checks.outputs = append(h.checks.outputs, CheckOutput{OK: false, Output: output}, CheckOutput{OK: false, Output: output})
	}
	return h
}

func requireRoundsPause(t *testing.T, e *Engine, n int) runrec.Record {
	t.Helper()
	r := requireState(t, e, runrec.StatePaused)
	if r.Round != n || r.Pending == nil || r.Pending.Kind != "rounds" || r.Pending.State != runrec.StateImplement || r.Feedback == nil || r.Feedback.Kind != "checks" {
		t.Fatalf("round pause: round=%d pending=%+v feedback=%+v", r.Round, r.Pending, r.Feedback)
	}
	return r
}

func TestARedLoopPausesAfterRoundThree(t *testing.T) {
	h := redLoop(t, true, true)
	e := h.start()
	r := requireRoundsPause(t, e, 3)
	if len(h.runner.roles()) != 3 {
		t.Fatalf("roles %v", h.runner.roles())
	}
	for _, text := range []string{"round 3 ended red", "checks failed (broken 3)", "more rounds (2 by default)", "change the check", "or stop"} {
		if !strings.Contains(r.Pending.Reason, text) {
			t.Errorf("reason lacks %q: %s", text, r.Pending.Reason)
		}
	}
	if strings.Contains(r.Pending.Reason, "ork ") {
		t.Errorf("pause reason contains a CLI command: %s", r.Pending.Reason)
	}
	if strings.Contains(r.Pending.Reason, "\n") {
		t.Fatal("multiline pause reason")
	}
}

func TestRoundPauseNamesTheFailedCheckSection(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		commandOK, slicerOK, noCommand bool
		want                           string
	}{
		{"command", false, true, false, "MSB1009: Project file does not exist"},
		{"both", false, false, false, "MSB1009: Project file does not exist"},
		{"slicer", true, false, false, `"tracking render is stale"`},
		{"slicer only", true, false, true, `"tracking render is stale"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				if tc.noCommand {
					o.project = func(p *registry.Project) { p.CheckCommand = "" }
				}
				if !tc.slicerOK {
					o.routesFor = func(P, W string) []clitest.Route {
						return []clitest.Route{{Tool: "slicer", Args: []string{"check", "--json", "--root", W}, Exit: 1,
							Stdout: "{\n  \"ok\": false,\n  \"problems\": [\n    \"tracking render is stale\"\n  ]\n}\n\n"}}
					}
				}
			})
			h.deps.MaxRounds = 1
			h.checks.outputs = []CheckOutput{{OK: tc.commandOK, Output: "starting\nMSB1009: Project file does not exist\n\n"}}
			r := requireRoundsPause(t, h.start(), 1)
			want := "round 1 ended red: checks failed (" + tc.want + ");"
			if !strings.HasPrefix(r.Pending.Reason, want) {
				t.Fatalf("failure summary: %s; want prefix %s", r.Pending.Reason, want)
			}
		})
	}
}

func TestRedFeedbackPausesAtRoundThree(t *testing.T) {
	for _, tc := range []struct{ kind, step string }{{"checks", "checks"}, {"findings", "review"}, {"architect", "architect"}, {"fail", "gate"}} {
		t.Run(tc.kind, func(t *testing.T) {
			h := newHarness(t)
			e, err := New(context.Background(), h.deps, h.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.run.Update(func(r *runrec.Record) { r.Round = 3; r.State = tc.step }); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(e.run.Dir(), "feedback.log"), []byte("failure\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := e.bumpRound(context.Background(), runrec.Feedback{Kind: tc.kind, File: "feedback.log"}); err != nil {
				t.Fatal(err)
			}
			r := requireState(t, e, runrec.StatePaused)
			if r.Pending == nil || r.Pending.Kind != "rounds" || r.Pending.State != runrec.StateImplement || r.Round != 3 || r.Feedback == nil || r.Feedback.Kind != tc.kind {
				t.Fatalf("pause lost feedback: %+v", r)
			}
			if !strings.HasPrefix(r.Pending.Reason, "round 3 ended red: "+tc.step+" failed (failure);") {
				t.Fatal(r.Pending.Reason)
			}
		})
	}
}

func TestARedLoopNeverStopsSilently(t *testing.T) {
	h := redLoop(t, true, true)
	e := h.start()
	requireRoundsPause(t, e, 3)
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.runner.roles()) != 3 {
		t.Fatal("round started without owner retry")
	}
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireRoundsPause(t, e, 5)
	if len(h.runner.roles()) != 5 {
		t.Fatalf("roles %v", h.runner.roles())
	}
}

func TestNoChangeAndTheSameCheckOutputPausesAtOnce(t *testing.T) {
	h := redLoop(t, false, false)
	r := requireRoundsPause(t, h.start(), 2)
	if !strings.Contains(r.Pending.Reason, "no change in the worktree and the same check output as round 1") {
		t.Fatal(r.Pending.Reason)
	}
}

func TestSameOutputButAChangedTreeDoesNotPauseEarly(t *testing.T) {
	h := redLoop(t, true, false)
	requireRoundsPause(t, h.start(), 3)
}

func TestNoChangeButDifferentOutputDoesNotPauseEarly(t *testing.T) {
	h := redLoop(t, false, true)
	requireRoundsPause(t, h.start(), 3)
}

func TestRetryAtTheRoundPauseGrantsRounds(t *testing.T) {
	h := redLoop(t, true, true)
	e := h.start()
	requireRoundsPause(t, e, 3)
	// Reload to exercise the durable grant across daemon restarts.
	if err := e.RetryNote(context.Background(), "continue", 2); err != nil {
		t.Fatal(err)
	}
	requireRoundsPause(t, e, 5)
	saved, err := h.store.Open(e.Snapshot().ID)
	if err != nil {
		t.Fatal(err)
	}
	e = Load(h.deps, h.proj, saved)
	if err := e.RetryNote(context.Background(), "continue", 1); err != nil {
		t.Fatal(err)
	}
	requireRoundsPause(t, e, 6)
	if !strings.Contains(h.runner.requests()[3].Brief, "continue") {
		t.Fatal("retry note lost")
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
				return []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: strings.Replace(string(raw), recordedAs, `"contract": "`+strings.TrimPrefix(line, "Contract: ")+`"`, 1)}}
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

// A project pinned to a version other than the one its slicer serves: slicer
// refuses `ai relay` with the recorded contract_mismatch error (exit 2).
func TestNewRefusesAProjectWhoseSlicerReportsContractMismatch(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, File: filepath.Join("..", "..", "testdata", "cli", "slicer", "error-contract-mismatch.json"), Exit: 2}}
		}
	})
	_, err := New(context.Background(), h.deps, h.cfg)
	var tool *clirun.ToolError
	if !errors.As(err, &tool) || tool.Code != "contract_mismatch" || !strings.Contains(err.Error(), "pins relay v10") || !strings.Contains(err.Error(), "serves relay v11") {
		t.Fatalf("error %v, want contract_mismatch naming both versions", err)
	}
	if n := len(h.fake.CallsTo("git")); n != 0 {
		t.Errorf("%d git calls before the contract was accepted", n)
	}
	if runs, _ := h.store.List(); len(runs) != 0 {
		t.Errorf("a run record was created: %d", len(runs))
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

func TestEngineStartReadsTheContractOnce(t *testing.T) {
	h := newHarness(t)
	if _, err := New(context.Background(), h.deps, h.cfg); err != nil {
		t.Fatal(err)
	}
	if n := len(h.calls("slicer", "ai", "relay")); n != 1 {
		t.Fatalf("contract reads = %d, want 1", n)
	}
}

func TestEngineStartKeepsTheContractErrorTexts(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.routesFor = func(P, W string) []clitest.Route {
					raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cli", "slicer", "relay-v11.json"))
					if err != nil {
						t.Fatal(err)
					}
					s := string(raw)
					if invalid {
						s = strings.ReplaceAll(s, `"key": "finding_kinds"`, `"key": "missing"`)
					} else {
						s = strings.Replace(s, `"contract": "relay v11"`, `"contract": "relay v12"`, 1)
					}
					return []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: s}}
				}
			})
			_, err := New(context.Background(), h.deps, h.cfg)
			if err == nil {
				t.Fatal("missing contract error")
			}
			if invalid {
				if !strings.HasPrefix(err.Error(), "slicer ai relay --json: relay conformance missing finding_kinds") {
					t.Fatalf("conformance error: %v", err)
				}
			} else if !strings.Contains(err.Error(), "v12") || strings.HasPrefix(err.Error(), "slicer ai relay --json:") {
				t.Fatalf("version error: %v", err)
			}
		})
	}
}

// S239: a red project check is run again once before a fix round.

func goFailOutput(tests ...string) string {
	var b strings.Builder
	b.WriteString("ok  \tgithub.com/x/y/clean\t0.1s\n")
	for _, name := range tests {
		fmt.Fprintf(&b, "--- FAIL: %s (0.01s)\n    %s_test.go:12: got 1, want 2\n", name, strings.ToLower(name))
	}
	b.WriteString("FAIL\nFAIL\tgithub.com/x/y/broken\t0.5s\n")
	return b.String()
}

func filler(n int) string {
	var b strings.Builder
	for b.Len() < n {
		b.WriteString("goroutine 17 [select]: unrelated dump line that fills the log\n")
	}
	return b.String()
}

func TestRedCheckIsRunAgainOnceBeforeAFixRound(t *testing.T) {
	h := newHarness(t)
	h.checks.outputs = []CheckOutput{{Output: "first run failed: boom\n"}, {OK: true, Output: "second run passed\n"}}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "gate-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	if len(h.checks.calls) != 2 {
		t.Errorf("the project command ran %d times, want 2: %v", len(h.checks.calls), h.checks.calls)
	}
	if len(rec.Checks) != 1 || !rec.Checks[0].OK || rec.Checks[0].Attempts != 2 || !rec.Checks[0].Flaky {
		t.Fatalf("checks: %+v", rec.Checks)
	}
	log := h.readReport(rec, "checks-1.log")
	first, second := strings.Index(log, "first run failed: boom"), strings.Index(log, "second run passed")
	if first < 0 || second < first {
		t.Fatalf("the log holds both outputs in order:\n%s", log)
	}
	if head := log[first:second]; !strings.Contains(head, "$ go test ./... (run again once: the first run failed)") {
		t.Errorf("a heading says the second output is a rerun:\n%s", log)
	}
}

func TestCheckThatStaysRedSendsOneFixRoundWithTheFailures(t *testing.T) {
	h := newHarness(t)
	h.checks.outputs = []CheckOutput{{Output: goFailOutput("TestSecond")}, {Output: goFailOutput("TestSecond")}, {OK: true, Output: "ok\n"}}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "implementer-2", "reviewer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	// Two runs for the red round, one for the round after the fix.
	if len(h.checks.calls) != 3 {
		t.Errorf("project command calls: %v", h.checks.calls)
	}
	if c := rec.Checks[0]; c.OK || c.Attempts != 2 || c.Flaky {
		t.Errorf("checks[0]: %+v", c)
	}
	if c := rec.Checks[1]; !c.OK || c.Attempts != 0 || c.Flaky {
		t.Errorf("checks[1] ran once: %+v", c)
	}
	brief := h.runner.requests()[1].Brief
	list, tailAt := strings.Index(brief, "The failing tests and assertion lines"), strings.Index(brief, "The end of the output:")
	if list < 0 || tailAt < list {
		t.Fatalf("the failing tests come before the tail:\n%s", brief)
	}
	for _, want := range []string{"--- FAIL: TestSecond (0.01s)", "testsecond_test.go:12: got 1, want 2", "FAIL\tgithub.com/x/y/broken"} {
		if !strings.Contains(brief[list:tailAt], want) {
			t.Errorf("the excerpt lacks %q:\n%s", want, brief[list:tailAt])
		}
	}
	if !strings.Contains(brief, "failed in both runs") {
		t.Errorf("the brief does not say the check failed twice:\n%s", brief)
	}
	if strings.Contains(brief, "Failed on the first run only") {
		t.Errorf("both runs failed the same test:\n%s", brief)
	}
}

func TestFixBriefKeepsTheAssertionWhenTheOutputIsLong(t *testing.T) {
	h := newHarness(t)
	long := "--- FAIL: TestEarly (0.01s)\n    early_test.go:7: got 1, want 2\n" + filler(60000)
	h.checks.outputs = []CheckOutput{{Output: long}, {Output: long}, {OK: true, Output: "ok\n"}}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	brief := h.runner.requests()[1].Brief
	for _, want := range []string{"--- FAIL: TestEarly (0.01s)", "early_test.go:7: got 1, want 2"} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lost %q:\n%.1500s", want, brief)
		}
	}
	if n := len(brief); n > 30000+8000+8000 {
		t.Errorf("the brief grew to %d bytes", n)
	}
}

func TestFixBriefListsTestsThatFailedOnlyOnTheFirstRun(t *testing.T) {
	h := newHarness(t)
	h.checks.outputs = []CheckOutput{{Output: goFailOutput("TestA", "TestB")}, {Output: goFailOutput("TestB")}, {OK: true, Output: "ok\n"}}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	brief := h.runner.requests()[1].Brief
	if !strings.Contains(brief, "Failed on the first run only: `TestA`.") {
		t.Errorf("the brief does not name TestA:\n%s", brief)
	}
	if strings.Contains(brief, "TestB.") {
		t.Errorf("TestB failed in both runs:\n%s", brief)
	}
	list, tailAt := strings.Index(brief, "The failing tests and assertion lines"), strings.Index(brief, "The end of the output:")
	if list < 0 || tailAt < list {
		t.Fatalf("no excerpt ahead of the tail:\n%s", brief)
	}
	if strings.Contains(brief[list:tailAt], "--- FAIL: TestA ") || !strings.Contains(brief[list:tailAt], "--- FAIL: TestB ") {
		t.Errorf("the excerpt shows the second run's failures:\n%s", brief[list:tailAt])
	}
}

func TestTimedOutCheckIsNotRunAgain(t *testing.T) {
	h := newHarness(t)
	h.checks.outputs = []CheckOutput{{TimedOut: true, Output: "\ntimed out after 10m0s\n"}, {OK: true, Output: "ok\n"}}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "implementer-2", "reviewer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	if len(h.checks.calls) != 2 {
		t.Errorf("one call for the timed-out round and one after the fix: %v", h.checks.calls)
	}
	if c := rec.Checks[0]; c.OK || c.Attempts != 0 || c.Flaky {
		t.Errorf("checks[0]: %+v", c)
	}
	if strings.Contains(h.readReport(rec, "checks-1.log"), "run again once") {
		t.Errorf("a timeout is not run again")
	}
}

func TestGreenCheckRunsOnce(t *testing.T) {
	h := newHarness(t)
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if len(h.checks.calls) != 1 || len(h.calls("slicer", "check")) != 1 {
		t.Errorf("project command %d times, slicer check %d times", len(h.checks.calls), len(h.calls("slicer", "check")))
	}
	if c := rec.Checks[0]; c.Attempts != 0 || c.Flaky {
		t.Errorf("a first-time pass records neither field: %+v", c)
	}
}

func TestRedSlicerCheckIsNotRunAgain(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{redThenGreenSlicerCheck(W, `{"ok":false,"stale_render":["ROADMAP.md"],"stale_sync":[],"problems":[],"warnings":[]}`)}
		}
	})
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	// Round 1 is red on slicer check alone and goes to a fix round: each
	// round runs the project command once and slicer check once.
	if len(h.checks.calls) != 2 || len(h.calls("slicer", "check")) != 2 {
		t.Errorf("project command %d times, slicer check %d times, want 2 and 2", len(h.checks.calls), len(h.calls("slicer", "check")))
	}
	if c := rec.Checks[0]; c.OK || c.Attempts != 0 || c.Flaky {
		t.Errorf("checks[0]: %+v", c)
	}
}

func TestRerunThatCannotRunPausesLikeTheFirstRun(t *testing.T) {
	h := newHarness(t)
	h.checks.outputs = []CheckOutput{{Output: "red\n"}}
	h.checks.errAt = map[int]error{1: errors.New("sh went away")}
	rec := requireState(t, h.start(), runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingError || rec.Pending.State != runrec.StateChecks || rec.Pending.Reason != "check command: sh went away" {
		t.Fatalf("pending: %+v", rec.Pending)
	}
	if len(rec.Checks) != 0 || len(h.checks.calls) != 2 {
		t.Errorf("checks %+v, calls %v", rec.Checks, h.checks.calls)
	}
	if got := h.runner.roles(); len(got) != 1 {
		t.Errorf("no fix round starts: %v", got)
	}
}

func TestStalledRoundWithARerunStillPausesAtOnce(t *testing.T) {
	h := redLoop(t, false, false)
	e := h.start()
	r := requireRoundsPause(t, e, 2)
	if !strings.Contains(r.Pending.Reason, "no change in the worktree and the same check output as round 1") {
		t.Fatal(r.Pending.Reason)
	}
	if len(h.checks.calls) != 4 {
		t.Errorf("two rounds with two runs each: %v", h.checks.calls)
	}
	if len(r.Checks) != 2 || r.Checks[0].Attempts != 2 || r.Checks[1].Attempts != 2 || r.Checks[0].LogSHA256 == "" || r.Checks[0].LogSHA256 != r.Checks[1].LogSHA256 {
		t.Errorf("both rounds ran twice and their logs hash the same: %+v", r.Checks)
	}
}

func TestCheckRecordOmitsAttemptsAndFlakyWhenAbsent(t *testing.T) {
	h := newHarness(t)
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	raw := h.readReport(rec, "run.json")
	for _, key := range []string{`"attempts"`, `"flaky"`} {
		if strings.Contains(raw, key) {
			t.Errorf("a first-time pass has no %s key:\n%s", key, raw)
		}
	}
	h = newHarness(t)
	h.checks.outputs = []CheckOutput{{Output: "red\n"}, {OK: true, Output: "ok\n"}}
	rec = requireState(t, h.start(), runrec.StateAwaitingApproval)
	raw = h.readReport(rec, "run.json")
	for _, want := range []string{`"attempts": 2`, `"flaky": true`} {
		if !strings.Contains(raw, want) {
			t.Errorf("a flaky pass records %s:\n%s", want, raw)
		}
	}
}

func TestCancelledRunDoesNotRunAFailedCheckAgain(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	h.deps.Check = func(context.Context, string, string, time.Duration) (CheckOutput, error) {
		calls++
		cancel() // the owner stopped the run while the check ran
		return CheckOutput{Output: "killed\n"}, nil
	}
	e, err := New(ctx, h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Run(ctx)
	if calls != 1 {
		t.Errorf("the project command ran %d times after the run was cancelled, want 1", calls)
	}
}

const rerunHeading = "\n\n$ go test ./..." + rerunMarker + "\n"

func TestCheckLogWithTheRerunMarkerInOutputIsNotSplitThere(t *testing.T) {
	first := "--- FAIL: TestA (0.01s)\n    a_test.go:1: note: (run again once: the first run failed)\n    a_test.go:2: real assertion\na.go:2:3: real build error\nFAIL\n"
	second := "--- FAIL: TestB (0.01s)\n    b_test.go:2: boom\nFAIL\n"
	log := "$ go test ./...\n" + first + "\n" + rerunHeading[1:] + second + "\n\n$ slicer check --json\n{}\n"
	body := checkFailureBody(log, "go test ./...", 2)
	if !strings.Contains(body, "Failed on the first run only: `TestA`.") {
		t.Errorf("the first run is the text before the real heading:\n%s", body)
	}
	list, tailAt := strings.Index(body, "The failing tests and assertion lines"), strings.Index(body, "The end of the output:")
	if list < 0 || tailAt < list || !strings.Contains(body[list:tailAt], "b_test.go:2: boom") || strings.Contains(body[list:tailAt], "TestA") || strings.Contains(body[list:tailAt], "real build error") {
		t.Errorf("the excerpt is of the second run:\n%s", body)
	}
}

func TestSingleRunRedLogWithTheFullHeadingInItsOutputDoesNotSayBothRuns(t *testing.T) {
	spoof := "--- FAIL: TestS (0.01s)\n    s_test.go:1: x\n" + rerunHeading + "--- FAIL: TestSpoof (0s)\n    spoof_test.go:2: y\n"
	// The engine, with a timeout so that there is no rerun.
	h := newHarness(t)
	h.checks.outputs = []CheckOutput{{TimedOut: true, Output: spoof}, {OK: true, Output: "ok\n"}}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	brief := h.runner.requests()[1].Brief
	if strings.Contains(brief, "failed in both runs") || strings.Contains(brief, "first run only") {
		t.Errorf("a single run is not described as two:\n%s", brief)
	}
	if !strings.Contains(brief, "TestSpoof") || !strings.Contains(brief, "TestS (0.01s)") {
		t.Errorf("both tests show in the excerpt of the one run:\n%s", brief)
	}
	// And the helper alone: one attempt means one run, whatever the text says.
	body := checkFailureBody("$ go test ./...\n"+spoof+"\n\n$ slicer check --json\n{}\n", "go test ./...", 0)
	if strings.Contains(body, "both runs") || strings.Contains(body, "first run only") {
		t.Errorf("attempts 0:\n%s", body)
	}
}

func TestFirstRunOnlyLineIsBoundedAndQuoted(t *testing.T) {
	var firstNames []string
	for i := 1; i <= 30; i++ {
		firstNames = append(firstNames, fmt.Sprintf("TestOld%02d", i))
	}
	// A backtick in a name and a name far longer than a line.
	firstNames = append([]string{"Test`evil`", "TestLong" + strings.Repeat("x", 900)}, firstNames...)
	h := newHarness(t)
	h.checks.outputs = []CheckOutput{{Output: goFailOutput(firstNames...)}, {Output: goFailOutput("TestNew")}, {OK: true, Output: "ok\n"}}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	brief := h.runner.requests()[1].Brief
	var line string
	for _, l := range strings.Split(brief, "\n") {
		if strings.HasPrefix(l, "Failed on the first run only: ") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no first-run-only line:\n%.3000s", brief)
	}
	if !strings.Contains(line, "`Test'evil'`, `TestLong") || !strings.Contains(line, "`TestOld01`, `TestOld02`") || !strings.Contains(line, "`TestOld18`") || strings.Contains(line, "TestOld19") || strings.Contains(line, "`evil`") {
		t.Errorf("20 names are listed, in backticks: %.300s", line)
	}
	if !strings.HasSuffix(line, ", and 12 more.") {
		t.Errorf("the rest are counted: ...%s", line[max(0, len(line)-60):])
	}
	if !strings.Contains(line, strings.Repeat("x", 300)+"...`") || strings.Contains(line, strings.Repeat("x", 500)) {
		t.Errorf("a long name is cut like an excerpt line")
	}
	if strings.Count(line, "`")%2 != 0 || len(line) > 1000 {
		t.Errorf("the line is not bounded or its backticks are unbalanced (%d bytes)", len(line))
	}
}
