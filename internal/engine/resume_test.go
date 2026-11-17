// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const gateNoVerdict = "Looks good.\nTests: 1 passed, 0 failed\n\n## Owner needed\n\nnone\n"

// requestNamed returns the request of the role's round, as the harness runner
// names them ("gate-2").
func requestNamed(t *testing.T, h *harness, name string) Request {
	t.Helper()
	for i, n := range h.runner.roles() {
		if n == name {
			return h.runner.requests()[i]
		}
	}
	t.Fatalf("no request %s in %v", name, h.runner.roles())
	return Request{}
}

// gateShapePause runs the harness until the gate breaks the report shape.
func gateShapePause(t *testing.T, h *harness) *Engine {
	t.Helper()
	bad := 2
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate && bad > 0 {
			bad--
			return Result{Exit: ExitOK, Session: "sess-gate", Report: gateNoVerdict}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape || rec.Pending.Role != RoleGate {
		t.Fatalf("pending: %+v", rec.Pending)
	}
	return e
}

func TestResumedGateAndFixRoundBriefsSayToReReadTheTree(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleReviewer {
			return Result{Exit: ExitOK, Session: "sess-reviewer", Report: findingsReview("1. engine.go:42 drops the error.")}, nil
		}
		return Result{}, nil
	}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	for name, want := range map[string]bool{"implementer-1": false, "reviewer-1": false, "implementer-2": true, "gate-2": true} {
		req := requestNamed(t, h, name)
		if got := strings.Contains(req.Brief, "## Resumed session"); got != want {
			t.Errorf("%s (resume %q): resumed section %v, want %v", name, req.Resume, got, want)
		}
		if (req.Resume != "") != want {
			t.Errorf("%s: resume %q", name, req.Resume)
		}
	}
}

func TestReadOnlyRoundsAreToldTheFinalMessageIsTheReport(t *testing.T) {
	h := newHarness(t)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	for _, req := range h.runner.requests() {
		if got := strings.Contains(req.Brief, "(ork saves your final message there; you cannot write it)"); got != req.ReadOnly {
			t.Errorf("%s: final message line %v, read-only %v", req.Role, got, req.ReadOnly)
		}
	}
	if reqs := h.runner.requests(); len(reqs) != 3 || reqs[0].ReadOnly || !reqs[1].ReadOnly || !reqs[2].ReadOnly {
		t.Errorf("want an implementer and two read-only rounds: %v", h.runner.roles())
	}
}

func TestShapeRetryOfTheGateReReadsTheTree(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{{Tool: "git", Args: []string{"-C", W, "status", "--porcelain"}, Stdout: " M a.go\n?? b.go\n"}}
		}
	})
	e := gateShapePause(t, h)
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	again := lastRequest(t, h, RoleGate) // the newest gate request is the rerun
	if again.Resume != "sess-gate" {
		t.Errorf("resume %q, want sess-gate", again.Resume)
	}
	for _, want := range []string{"## Report shape", "## Resumed session", "git status --porcelain when this round started:\n\n```\n M a.go\n?? b.go\n```"} {
		if !strings.Contains(again.Brief, want) {
			t.Errorf("rerun brief lacks %q:\n%s", want, again.Brief)
		}
	}
}

func TestFreshGateGetsEarlierReportsAndNoResumedSection(t *testing.T) {
	h := newHarness(t)
	h.cfg.Lineup[RoleGate] = []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	gate := requestNamed(t, h, "gate-1")
	if gate.Resume != "" || !strings.Contains(gate.Brief, "## Earlier reports") || strings.Contains(gate.Brief, "## Resumed session") {
		t.Errorf("fresh gate: resume %q\n%s", gate.Resume, gate.Brief)
	}
}

// lastRequest returns the newest request of a role.
func lastRequest(t *testing.T, h *harness, role string) Request {
	t.Helper()
	reqs := h.runner.requests()
	for i := len(reqs) - 1; i >= 0; i-- {
		if reqs[i].Role == role {
			return reqs[i]
		}
	}
	t.Fatalf("no %s request in %v", role, h.runner.roles())
	return Request{}
}

func TestRetryNoteReachesTheRerunBrief(t *testing.T) {
	h := newHarness(t)
	e := gateShapePause(t, h)
	if err := e.RetryNote(context.Background(), "  fixtures added under testdata/x  "); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	brief := lastRequest(t, h, RoleGate).Brief
	shape, note := strings.Index(brief, "## Report shape"), strings.Index(brief, "## Owner note\n\nfixtures added under testdata/x\n")
	if shape < 0 || note < shape {
		t.Errorf("owner note must follow the report shape section:\n%s", brief)
	}
	if rec.Pending != nil {
		t.Errorf("the note outlived the round: %+v", rec.Pending)
	}
}

func TestRetryWithoutANoteAddsNoOwnerNote(t *testing.T) {
	h := newHarness(t)
	e := gateShapePause(t, h)
	if err := e.RetryNote(context.Background(), "   "); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	if brief := lastRequest(t, h, RoleGate).Brief; strings.Contains(brief, "## Owner note") {
		t.Errorf("a blank note made a section:\n%s", brief)
	}
}

func TestRetryNoteOnALostAndABudgetPause(t *testing.T) {
	t.Run("lost", func(t *testing.T) {
		h := newHarness(t)
		h.seed("run-a", runrec.StateImplement, runningRole(RoleImplementer, 111, "2"))
		e := h.recover()["run-a"]
		requireState(t, e, runrec.StatePaused)
		if err := e.RetryNote(context.Background(), "the fixtures are in testdata/x"); err != nil {
			t.Fatal(err)
		}
		brief := lastRequest(t, h, RoleImplementer).Brief
		lost, note := strings.Index(brief, "## Earlier process lost"), strings.Index(brief, "## Owner note\n\nthe fixtures are in testdata/x\n")
		if lost < 0 || note < lost {
			t.Errorf("lost pause brief:\n%s", brief)
		}
	})
	t.Run("budget", func(t *testing.T) {
		h := newHarness(t)
		h.cfg.Budgets.Tokens = 999
		e := h.start()
		requireBudgetPause(t, e, RoleReviewer, runrec.StateReview, 1)
		if err := e.RetryNote(context.Background(), "go on, budget raised"); err != nil {
			t.Fatal(err)
		}
		if brief := lastRequest(t, h, RoleReviewer).Brief; !strings.Contains(brief, "## Owner note\n\ngo on, budget raised\n") {
			t.Errorf("budget pause brief:\n%s", brief)
		}
	})
}

func TestRetryNoteRidesOnAQueuedQuestion(t *testing.T) {
	h, e, rec := retryWithQueuedQuestion(t)
	if err := e.RetryNote(context.Background(), "re-read testdata"); err != nil {
		t.Fatal(err)
	}
	paused := requireState(t, e, runrec.StatePaused)
	if paused.Pending == nil || paused.Pending.Retry == nil || paused.Pending.Retry.Note != "re-read testdata" || paused.Pending.Note != "" {
		t.Fatalf("the note must ride in pending.retry: %+v", paused.Pending)
	}
	// Reload to prove the note is durable.
	run, err := h.store.Open(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	e = Load(h.deps, h.proj, run)
	before := len(h.runner.requests())
	if err := e.Answer(context.Background(), "stdlib"); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	var reviewer, ui *Request
	for _, r := range h.runner.requests()[before:] {
		switch r.Role {
		case RoleReviewer:
			reviewer = &r
		case RoleUI:
			ui = &r
		}
	}
	if reviewer == nil || ui == nil {
		t.Fatalf("roles after the answer: %v", h.runner.roles()[before:])
	}
	if !strings.Contains(reviewer.Brief, "## Owner note\n\nre-read testdata\n") {
		t.Errorf("the reviewer lost the note:\n%s", reviewer.Brief)
	}
	if strings.Contains(ui.Brief, "re-read testdata") {
		t.Errorf("the UI pass got the reviewer's note:\n%s", ui.Brief)
	}
}

func TestRetryNoteIsRefusedForAQuestion(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(Request, int) (Result, error) {
		return Result{Exit: ExitOK, Session: "s", Report: withQuestion(implReport)}, nil
	}
	e := h.start()
	requireState(t, e, runrec.StatePaused)
	err := e.RetryNote(context.Background(), "a note")
	if err == nil || !strings.Contains(err.Error(), "the slice waits for an answer") {
		t.Errorf("RetryNote on a question: %v", err)
	}
	if rec := e.Snapshot(); rec.Pending == nil || rec.Pending.Note != "" {
		t.Errorf("a refused note was stored: %+v", rec.Pending)
	}
}

// stopCommittedAtLand is a committed run that stopped in land with its one
// fix round already spent, and a base branch that has moved since the commit.
func stopCommittedAtLand(t *testing.T, env *landEnv) runrec.Record {
	t.Helper()
	gitRun(t, env.W, "add", "-A")
	gitRun(t, env.W, "commit", "-q", "-m", "slice")
	sha := gitRun(t, env.W, "rev-parse", "HEAD")
	env.moveBase("base.txt", "from main\n")
	if err := env.run.Update(func(r *runrec.Record) {
		r.State = runrec.StateStopped
		r.StoppedFrom = runrec.StateLand
		r.Reason = "land blocked: checks red after merging main"
		r.Commit = &runrec.Commit{SHA: sha, Title: "slice"}
		r.LandFixes = 1
		r.LandRecheck = false
	}); err != nil {
		t.Fatal(err)
	}
	return env.run.Snapshot()
}

func TestRetryRelandsACommittedRunStoppedAtLand(t *testing.T) {
	env := newLandEnv(t)
	before := stopCommittedAtLand(t, env)
	var resumed runrec.Record
	var mid runrec.Record
	var rolesAtCheck int
	env.beforeCheck = func() {
		if mid.State == "" {
			mid = env.e.Snapshot()
			rolesAtCheck = len(env.runner.requests())
		}
	}

	err := env.e.Retry(WithApplied(context.Background(), func() { resumed = env.e.Snapshot() }))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if resumed.State != runrec.StateLand || resumed.LandFixes != 0 || !resumed.LandRecheck {
		t.Fatalf("after retry: state %q land_fixes %d recheck %v, want land, 0, true", resumed.State, resumed.LandFixes, resumed.LandRecheck)
	}
	if mid.State != runrec.StateLand || mid.LandFixes != 0 || !mid.LandRecheck {
		t.Fatalf("at the re-check: state %q land_fixes %d recheck %v, want land, 0, true", mid.State, mid.LandFixes, mid.LandRecheck)
	}
	if resumed.Round != before.Round || resumed.RoundLimit != before.RoundLimit || resumed.StoppedFrom != "" || resumed.Reason != "" {
		t.Fatalf("resume changed the round or left the stop: round %d/%d stopped_from %q reason %q", resumed.Round, resumed.RoundLimit, resumed.StoppedFrom, resumed.Reason)
	}
	if mid.Round != before.Round || mid.RoundLimit != before.RoundLimit {
		t.Fatalf("landing changed the round before the re-check: round %d/%d", mid.Round, mid.RoundLimit)
	}
	if rolesAtCheck != 0 {
		t.Fatalf("roles before the landing re-check: %v", env.runner.roles())
	}
	if len(env.seen) < 1 || !env.seen[0].hasBase || env.seen[0].dir != env.W {
		t.Fatalf("re-check did not run on the merged worktree: %+v", env.seen)
	}
	if indexOf(env.gitCalls(), "W: merge -m ") < 0 {
		t.Fatalf("ork did not merge main again: %q", env.gitCalls())
	}
	rec := requireState(t, env.e, runrec.StateCommitted)
	if rec.LandedAt.IsZero() {
		t.Fatal("the record did not end landed")
	}
	if got := gitRun(t, env.P, "show", "main:feature.txt"); got != "the slice" {
		t.Errorf("feature.txt on main: %q", got)
	}
	if got := gitRun(t, env.P, "show", "main:base.txt"); got != "from main" {
		t.Errorf("base.txt on main: %q", got)
	}
	if len(env.runner.requests()) != 0 {
		t.Errorf("roles ran on a green re-check: %v", env.runner.roles())
	}
}

func TestRetryRelandGetsOneFixRound(t *testing.T) {
	env := newLandEnv(t, onAutoLand)
	stopCommittedAtLand(t, env)
	red := CheckOutput{Output: "FAIL TestGolden sidebar\n"}
	env.setCheckOutputs(red, red, CheckOutput{OK: true, Output: "ok\n"}, red, red)
	var kinds []string
	env.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && strings.Contains(req.Brief, "## Landing blocked") {
			kind := ""
			if rec := env.e.Snapshot(); rec.Feedback != nil {
				kind = rec.Feedback.Kind
			}
			kinds = append(kinds, kind)
			env.moveBase("later.txt", "main moves again\n")
		}
		return Result{}, nil
	}

	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(kinds) != 1 || kinds[0] != "land" {
		t.Fatalf("landing fix rounds %q, want one with feedback kind land; roles %v", kinds, env.runner.roles())
	}
	rec := requireState(t, env.e, runrec.StateStopped)
	want := "land blocked: checks red after merging main; ork retry " + rec.ID + " re-lands with one more fix round"
	if rec.Reason != want {
		t.Fatalf("reason %q, want %q", rec.Reason, want)
	}
	if rec.LandFixes != 1 {
		t.Errorf("land fixes %d, want 1", rec.LandFixes)
	}
	if _, err := os.Stat(env.W); err != nil {
		t.Errorf("worktree gone: %v", err)
	}
	if exec.Command("git", "-C", env.P, "merge-base", "--is-ancestor", item, "main").Run() == nil {
		t.Error("the slice branch reached main")
	}
}

// A re-land already at the round cap still gets its one land fix. The resume
// raises round_limit by one and leaves round unchanged.
func TestRetryRelandAtTheRoundCapGetsOneFixRound(t *testing.T) {
	for _, limit := range []int{0, 3} {
		t.Run(fmt.Sprintf("limit %d", limit), func(t *testing.T) {
			env := newLandEnv(t, onAutoLand)
			stopCommittedAtLand(t, env)
			if err := env.run.Update(func(r *runrec.Record) {
				r.Round = 3
				r.RoundLimit = limit
			}); err != nil {
				t.Fatal(err)
			}
			before := env.run.Snapshot()
			red := CheckOutput{Output: "FAIL TestGolden sidebar\n"}
			env.setCheckOutputs(red, red, CheckOutput{OK: true, Output: "ok\n"}, red, red)
			var kinds []string
			var resumed runrec.Record
			env.runner.script = func(req Request, n int) (Result, error) {
				if req.Role == RoleImplementer && strings.Contains(req.Brief, "## Landing blocked") {
					kind := ""
					if rec := env.e.Snapshot(); rec.Feedback != nil {
						kind = rec.Feedback.Kind
					}
					kinds = append(kinds, kind)
					env.moveBase("later.txt", "main moves again\n")
				}
				return Result{}, nil
			}

			if err := env.e.Retry(WithApplied(context.Background(), func() { resumed = env.e.Snapshot() })); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if len(kinds) != 1 || kinds[0] != "land" {
				t.Fatalf("landing fix rounds %q, want one with feedback kind land; roles %v state %s reason %q", kinds, env.runner.roles(), env.e.Snapshot().State, env.e.Snapshot().Reason)
			}
			rec := requireState(t, env.e, runrec.StateStopped)
			want := "land blocked: checks red after merging main; ork retry " + rec.ID + " re-lands with one more fix round"
			if rec.Reason != want {
				t.Fatalf("reason %q, want %q", rec.Reason, want)
			}
			if resumed.Round != before.Round || resumed.RoundLimit != before.Round+1 {
				t.Fatalf("at resume: round %d/%d, want round %d and limit %d", resumed.Round, resumed.RoundLimit, before.Round, before.Round+1)
			}
			if rec.RoundLimit != before.Round+1 {
				t.Fatalf("round_limit %d, want %d", rec.RoundLimit, before.Round+1)
			}
		})
	}
}

func TestRetryStillRefusesOtherCommittedRuns(t *testing.T) {
	for _, from := range []string{
		runrec.StateImplement, runrec.StateChecks, runrec.StateReview, runrec.StateGate,
		runrec.StateCommit, runrec.StateAwaitingApproval, runrec.StateSetup, runrec.StateFinish,
	} {
		t.Run("stopped from "+from, func(t *testing.T) {
			h := resumeHarness(t)
			h.mkWorktree()
			h.seed("run-a", runrec.StateStopped)
			run, err := h.store.Open("run-a")
			if err != nil {
				t.Fatal(err)
			}
			if err := run.Update(func(r *runrec.Record) {
				r.StoppedFrom = from
				r.Commit = &runrec.Commit{SHA: "0123abc"}
			}); err != nil {
				t.Fatal(err)
			}
			_, err = h.resume("run-a", h.proj, "")
			if err == nil || !IsRefusal(err) || err.Error() != "run run-a committed its slice; there is nothing to resume" {
				t.Fatalf("error %v, want today's committed-run refusal", err)
			}
		})
	}
	t.Run("worktree gone", func(t *testing.T) {
		h := resumeHarness(t)
		h.seed("run-a", runrec.StateStopped)
		run, err := h.store.Open("run-a")
		if err != nil {
			t.Fatal(err)
		}
		if err := run.Update(func(r *runrec.Record) {
			r.StoppedFrom = runrec.StateLand
			r.Commit = &runrec.Commit{SHA: "0123abc"}
			r.LandFixes = 1
		}); err != nil {
			t.Fatal(err)
		}
		_, err = h.resume("run-a", h.proj, "")
		if err == nil || !IsRefusal(err) || !strings.Contains(err.Error(), "no worktree") || !strings.Contains(err.Error(), h.W) {
			t.Fatalf("error %v, want a refusal naming the missing worktree %s", err, h.W)
		}
	})
	t.Run("branch gone", func(t *testing.T) {
		h := resumeHarness(t, func(o *harnessOpts) {
			o.routesFor = func(P, W string) []clitest.Route {
				return []clitest.Route{
					{Tool: "git", Args: []string{"-C", W, "symbolic-ref", "--short", "HEAD"}, Exit: 1, Stderr: "fatal: ref HEAD is not a symbolic ref\n"},
				}
			}
		})
		h.mkWorktree()
		h.seed("run-a", runrec.StateStopped)
		run, err := h.store.Open("run-a")
		if err != nil {
			t.Fatal(err)
		}
		if err := run.Update(func(r *runrec.Record) {
			r.StoppedFrom = runrec.StateLand
			r.Commit = &runrec.Commit{SHA: "0123abc"}
			r.LandFixes = 1
		}); err != nil {
			t.Fatal(err)
		}
		_, err = h.resume("run-a", h.proj, "")
		if err == nil || !IsRefusal(err) || !strings.Contains(err.Error(), "no branch") || !strings.Contains(err.Error(), item) {
			t.Fatalf("error %v, want a refusal naming the missing branch %s", err, item)
		}
	})
	t.Run("rounds", func(t *testing.T) {
		h := resumeHarness(t)
		h.mkWorktree()
		h.seed("run-a", runrec.StateStopped)
		run, err := h.store.Open("run-a")
		if err != nil {
			t.Fatal(err)
		}
		if err := run.Update(func(r *runrec.Record) {
			r.StoppedFrom = runrec.StateLand
			r.Commit = &runrec.Commit{SHA: "0123abc"}
			r.LandFixes = 1
		}); err != nil {
			t.Fatal(err)
		}
		_, err = h.resume("run-a", h.proj, "", 3)
		if err == nil || !IsRefusal(err) || !strings.Contains(err.Error(), "one fix round") {
			t.Fatalf("error %v, want a refusal that landing gets one fix round", err)
		}
		saved, err := h.store.Open("run-a")
		if err != nil {
			t.Fatal(err)
		}
		if got := saved.Snapshot(); got.State != runrec.StateStopped || got.LandFixes != 1 {
			t.Fatalf("a refused retry changed the run: state %s land_fixes %d", got.State, got.LandFixes)
		}
	})
}
