// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// S296 (*proposed*): a stopped run resumes from its worktree.

// reset starts the scripted outputs over, for a check that the test changes
// between a stop and a resume. The calls so far are dropped.
func (f *fakeChecks) reset(outputs ...CheckOutput) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls, f.outputs = nil, outputs
}

// redRounds scripts n red rounds. Each round's output is run twice (S239) and
// differs from the next, so no round is a stall.
func redRounds(prefix string, n int) []CheckOutput {
	var out []CheckOutput
	for i := 1; i <= n; i++ {
		red := CheckOutput{OK: false, Output: fmt.Sprintf("%s %d\n", prefix, i)}
		out = append(out, red, red)
	}
	return out
}

// resumeHarness is a harness whose worktree is a real directory that the fake
// git accepts as the run's worktree, and whose slicer hands out two ledger
// runs: run-1 and, for a resume, run-2.
func resumeHarness(t *testing.T, opts ...func(*harnessOpts)) *harness {
	t.Helper()
	h := newHarness(t, append([]func(*harnessOpts){func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			var diffs []clitest.Reply
			for n := 1; n <= 24; n++ {
				diffs = append(diffs, clitest.Reply{Stdout: fmt.Sprintf("diff --git a/a.go b/a.go\n-old\n+value%d\n", n)})
			}
			return []clitest.Route{
				{Tool: "slicer", Args: []string{"relay-run", "begin"}, Prefix: true, Seq: []clitest.Reply{{Stdout: `{"run":{"id":"run-1"}}`}, {Stdout: `{"run":{"id":"run-2"}}`}}},
				{Tool: "slicer", Args: []string{"relay-run", "step", "run-2"}, Prefix: true, Seq: stepReplies(40)},
				{Tool: "slicer", Args: []string{"relay-run", "end", "run-2"}, Prefix: true, Stdout: `{"run":{"id":"run-2","state":"ended"}}`},
				{Tool: "git", Args: []string{"-C", W, "diff", "HEAD", "--binary", "--no-ext-diff"}, Seq: diffs},
				{Tool: "git", Args: []string{"-C", W, "ls-files", "--others", "--exclude-standard", "-z"}},
				{Tool: "git", Args: []string{"-C", W, "rev-parse", "--show-toplevel"}, Stdout: W + "\n"},
				{Tool: "git", Args: []string{"-C", W, "symbolic-ref", "--short", "HEAD"}, Stdout: item + "\n"},
				{Tool: "git", Args: []string{"-C", W, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
				{Tool: "git", Args: []string{"-C", P, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
			}
		}
	}}, opts...)...)
	return h
}

func (h *harness) mkWorktree() {
	h.t.Helper()
	if err := os.MkdirAll(h.W, 0o755); err != nil {
		h.t.Fatal(err)
	}
}

// stoppedAtTheRoundsPause runs a red loop to its rounds pause (round 3) and
// stops it as the owner does. Round 2's implementer leaves a file in the
// worktree.
func stoppedAtTheRoundsPause(t *testing.T) (*harness, runrec.Record) {
	t.Helper()
	h := resumeHarness(t)
	h.mkWorktree()
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && req.Round == 2 {
			if err := os.WriteFile(filepath.Join(h.W, "round2.txt"), []byte("work\n"), 0o644); err != nil {
				t.Error(err)
			}
		}
		return Result{}, nil
	}
	h.checks.outputs = redRounds("original failure", 3)
	e := h.start()
	requireRoundsPause(t, e, 3)
	if err := e.Stop(context.Background(), "fix the check first"); err != nil {
		t.Fatal(err)
	}
	return h, requireState(t, e, runrec.StateStopped)
}

// resume loads the stopped run with proj, as the daemon does, and retries it.
func (h *harness) resume(id string, proj registry.Project, note string, rounds ...int) (*Engine, error) {
	h.t.Helper()
	saved, err := h.store.Open(id)
	if err != nil {
		h.t.Fatal(err)
	}
	e := Load(h.deps, proj, saved)
	return e, e.RetryNote(context.Background(), note, rounds...)
}

func TestResumeAStoppedRunReentersChecksWithMoreRounds(t *testing.T) {
	h, stopped := stoppedAtTheRoundsPause(t)
	if stopped.StoppedFrom != runrec.StateImplement {
		t.Fatalf("stopped_from %q, want the paused step implement", stopped.StoppedFrom)
	}
	// The owner dismissed the stopped run (the old way on); resume clears it.
	saved, err := h.store.Open(stopped.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := saved.Dismiss(); err != nil {
		t.Fatal(err)
	}
	h.checks.reset(CheckOutput{OK: true, Output: "ok\n"})
	rolesBefore := len(h.runner.roles())

	e, err := h.resume(stopped.ID, h.proj, "", 2)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	// No implementer round runs first: the roles after the resume are the
	// reviewer's and the gate's, in round 4.
	if got, want := h.runner.roles()[rolesBefore:], []string{"reviewer-4", "gate-4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles after the resume %v, want %v", got, want)
	}
	if len(h.checks.calls) != 1 || h.checks.calls[0] != h.W+"|go test ./..." {
		t.Fatalf("check calls after the resume: %v", h.checks.calls)
	}
	last := rec.Checks[len(rec.Checks)-1]
	if !last.OK || last.Round != 4 || last.Log != "checks-4.log" {
		t.Fatalf("last check %+v", last)
	}
	if b, err := os.ReadFile(filepath.Join(h.W, "round2.txt")); err != nil || string(b) != "work\n" {
		t.Fatalf("the worktree file from round 2: %q, %v", b, err)
	}
	if !rec.DismissedAt.IsZero() || rec.StoppedFrom != "" || rec.Reason != "" || rec.Pending != nil || rec.Feedback != nil {
		t.Errorf("resume leaves a stop behind: dismissed %v stopped_from %q reason %q pending %+v feedback %+v", rec.DismissedAt, rec.StoppedFrom, rec.Reason, rec.Pending, rec.Feedback)
	}
	if rec.RoundLimit != 6 || rec.Round != 4 {
		t.Errorf("round %d limit %d, want round 4 and limit 6 (the resumed round plus 2)", rec.Round, rec.RoundLimit)
	}
	// A second ledger run: the stopped one ended as stopped, the resume began its own.
	if begins := h.calls("slicer", "relay-run", "begin"); len(begins) != 2 || join(begins[1]) != "relay-run begin S04 --worktree "+h.W+" --json --root "+h.W {
		t.Fatalf("relay-run begin calls: %v", begins)
	}
	if rec.RelayRun != "run-2" || rec.OrchestratorStep == "" {
		t.Errorf("ledger run %q step %q, want run-2 and its orchestrator step", rec.RelayRun, rec.OrchestratorStep)
	}
	if ends := h.calls("slicer", "relay-run", "end"); len(ends) != 1 || !strings.Contains(join(ends[0]), "run-1 --outcome stopped") {
		t.Errorf("the stopped run's ledger run must stay ended as stopped, once: %v", ends)
	}
	// The resume is on the record in the open: nothing else was written.
	reread, err := h.store.Open(stopped.ID)
	if err != nil || reread.Snapshot().State != runrec.StateAwaitingApproval {
		t.Fatalf("stored state: %+v, %v", reread.Snapshot().State, err)
	}
}

func TestResumeRedChecksStartsAFixRoundWithTheLastFeedback(t *testing.T) {
	h, stopped := stoppedAtTheRoundsPause(t)
	h.checks.reset(redRounds("after the resume", 4)...)

	e, err := h.resume(stopped.ID, h.proj, "", 2)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingRounds {
		t.Fatalf("pending %+v, want the rounds pause", rec.Pending)
	}
	// Resumed at round 4 with 2 rounds: fix rounds 5 and 6, then the pause.
	if rec.Round != 6 || rec.RoundLimit != 6 {
		t.Errorf("round %d, round_limit %d; want 6 and 6 (the resumed round 4 plus 2)", rec.Round, rec.RoundLimit)
	}
	if got, want := h.runner.roles()[3:], []string{"implementer-5", "implementer-6"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles after the resume %v, want %v", got, want)
	}
	brief := h.runner.requests()[3].Brief
	if !strings.Contains(brief, "Checks failed") || !strings.Contains(brief, "after the resume 1") {
		t.Errorf("the fix round's brief lacks the new check log:\n%s", brief)
	}
	if strings.Contains(brief, "original failure") {
		t.Errorf("the fix round's brief holds the log from before the stop:\n%s", brief)
	}
	if !strings.Contains(rec.Pending.Reason, "round 6 ended red") {
		t.Errorf("pause reason %q", rec.Pending.Reason)
	}
}

func TestResumeRefusesACommittedOrSetupStoppedRun(t *testing.T) {
	oneLine := func(t *testing.T, err error, want string) {
		t.Helper()
		if err == nil || !IsRefusal(err) || strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v, want a one-line refusal naming %q", err, want)
		}
		if !strings.HasPrefix(err.Error(), "run ") {
			t.Fatalf("a refusal has the form run ID <fact>; <action>: %v", err)
		}
		if strings.Contains(err.Error(), "not paused") {
			t.Fatalf("the text is the paused-run refusal: %v", err)
		}
	}
	t.Run("stopped at setup", func(t *testing.T) {
		h := resumeHarness(t, func(o *harnessOpts) {
			o.routesFor = func(P, W string) []clitest.Route {
				return []clitest.Route{{Tool: "git", Args: []string{"-C", P, "worktree", "add", W, "-b", item}, Exit: 128, Stderr: "fatal: no\n"}}
			}
		})
		e := h.start()
		rec := requireState(t, e, runrec.StateStopped)
		if rec.StoppedFrom != runrec.StateSetup {
			t.Fatalf("stopped_from %q, want setup", rec.StoppedFrom)
		}
		_, err := h.resume(rec.ID, h.proj, "")
		oneLine(t, err, "stopped during setup")
		if got := h.calls("slicer", "relay-run", "begin"); len(got) != 0 {
			t.Errorf("a refused resume began a ledger run: %v", got)
		}
	})
	t.Run("committed", func(t *testing.T) {
		h := resumeHarness(t)
		h.mkWorktree()
		e := h.start()
		if err := e.Approve(context.Background()); err != nil {
			t.Fatal(err)
		}
		rec := requireState(t, e, runrec.StateCommitted)
		_, err := h.resume(rec.ID, h.proj, "")
		oneLine(t, err, "committed")
	})
	t.Run("stopped after its commit", func(t *testing.T) {
		h := resumeHarness(t)
		h.mkWorktree()
		h.seed("run-a", runrec.StateStopped)
		run, err := h.store.Open("run-a")
		if err != nil {
			t.Fatal(err)
		}
		if err := run.Update(func(r *runrec.Record) { r.StoppedFrom = runrec.StateLand; r.Commit = &runrec.Commit{SHA: "0123abc"} }); err != nil {
			t.Fatal(err)
		}
		_, err = h.resume("run-a", h.proj, "")
		oneLine(t, err, "commit")
	})
	t.Run("worktree gone", func(t *testing.T) {
		h := resumeHarness(t)
		h.seed("run-a", runrec.StateStopped)
		run, _ := h.store.Open("run-a")
		if err := run.Update(func(r *runrec.Record) { r.StoppedFrom = runrec.StateReview }); err != nil {
			t.Fatal(err)
		}
		_, err := h.resume("run-a", h.proj, "")
		oneLine(t, err, "ork dismiss run-a to start a new run")
		if got := h.calls("git", "-C", h.P, "worktree", "add"); len(got) != 0 {
			t.Errorf("a resume must not create the worktree: %v", got)
		}
	})
	t.Run("worktree on another branch", func(t *testing.T) {
		h := resumeHarness(t, func(o *harnessOpts) {
			o.routesFor = func(P, W string) []clitest.Route {
				return []clitest.Route{
					{Tool: "git", Args: []string{"-C", W, "rev-parse", "--show-toplevel"}, Stdout: W + "\n"},
					{Tool: "git", Args: []string{"-C", W, "symbolic-ref", "--short", "HEAD"}, Stdout: "other\n"},
					{Tool: "git", Args: []string{"-C", W, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
					{Tool: "git", Args: []string{"-C", P, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
				}
			}
		})
		h.mkWorktree()
		h.seed("run-a", runrec.StateStopped)
		run, _ := h.store.Open("run-a")
		if err := run.Update(func(r *runrec.Record) { r.StoppedFrom = runrec.StateReview }); err != nil {
			t.Fatal(err)
		}
		_, err := h.resume("run-a", h.proj, "")
		oneLine(t, err, "does not match the run")
		if !strings.Contains(err.Error(), "ork dismiss run-a to start a new run") {
			t.Errorf("a foreign worktree names the way on: %v", err)
		}
	})
}

func TestResumeWithNoImplementerRoundEntersImplement(t *testing.T) {
	h := resumeHarness(t)
	h.mkWorktree()
	// A run that S295's setup pause left: a worktree and a ledger run, no
	// implementer round. The owner stops it, fixes the project and resumes.
	h.seed("run-a", runrec.StatePaused)
	run, err := h.store.Open("run-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Update(func(r *runrec.Record) {
		r.Pending = &runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Reason: "the check command failed verification"}
	}); err != nil {
		t.Fatal(err)
	}
	if err := Load(h.deps, h.proj, run).Stop(context.Background(), "fix the check"); err != nil {
		t.Fatal(err)
	}
	if got := run.Snapshot().StoppedFrom; got != runrec.StateImplement {
		t.Fatalf("stopped_from %q, want implement (the paused step)", got)
	}
	var checksBeforeRound1 []string
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer {
			checksBeforeRound1 = append(checksBeforeRound1, h.checks.calls...)
		}
		return Result{}, nil
	}

	e, err := h.resume("run-a", h.proj, "")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "gate-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles %v, want %v: implementer round 1 first, then the checks", got, want)
	}
	if len(checksBeforeRound1) != 0 {
		t.Errorf("checks ran before implementer round 1: %v", checksBeforeRound1)
	}
	if brief := h.runner.requests()[0].Brief; !strings.Contains(brief, "This is the first round.") || strings.Contains(brief, "Checks failed") {
		t.Errorf("implementer round 1 did not get the first-round brief:\n%s", brief)
	}
	// Round 1 is the first of the 2 granted rounds: the limit is 2. (The
	// first-pause floor of 3 rounds still applies to a red loop.)
	if rec.Round != 1 || rec.RoundLimit != 2 {
		t.Errorf("round %d limit %d, want 1 and 2", rec.Round, rec.RoundLimit)
	}
	if len(rec.Checks) != 1 || rec.Checks[0].Round != 1 {
		t.Errorf("checks %+v", rec.Checks)
	}
}

func TestResumeUsesTheCurrentCheckCommand(t *testing.T) {
	h, stopped := stoppedAtTheRoundsPause(t)
	fixed := h.proj
	fixed.CheckCommand = "make verify"
	h.checks.reset(CheckOutput{OK: true, Output: "ok\n"})

	e, err := h.resume(stopped.ID, fixed, "")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if len(h.checks.calls) != 1 || h.checks.calls[0] != h.W+"|make verify" {
		t.Fatalf("check calls %v, want the fixed command", h.checks.calls)
	}
	if last := rec.Checks[len(rec.Checks)-1]; last.Command != "make verify" || last.Round != 4 {
		t.Errorf("checks[] row %+v does not name the fixed command", last)
	}
	if got := rec.Checks[0].Command; got != "go test ./..." {
		t.Errorf("the checks from before the stop were rewritten: %q", got)
	}
}

func TestResumeGrantRules(t *testing.T) {
	for name, tc := range map[string]struct {
		note   string
		rounds []int
		want   string
	}{
		"a note": {note: "look at the logs", want: "a stopped run resumes without a note; run ork retry "},
		"zero":   {rounds: []int{-1}, want: "rounds"},
	} {
		t.Run(name, func(t *testing.T) {
			h, stopped := stoppedAtTheRoundsPause(t)
			h.checks.reset(CheckOutput{OK: true})
			e, err := h.resume(stopped.ID, h.proj, tc.note, tc.rounds...)
			if err == nil || !IsRefusal(err) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want a refusal naming %q", err, tc.want)
			}
			rec := requireState(t, e, runrec.StateStopped)
			if rec.RoundLimit != 0 || len(h.checks.calls) != 0 || len(h.calls("slicer", "relay-run", "begin")) != 1 {
				t.Errorf("a refused resume changed the run: limit %d, checks %v", rec.RoundLimit, h.checks.calls)
			}
		})
	}
	t.Run("a grant of 5", func(t *testing.T) {
		h, stopped := stoppedAtTheRoundsPause(t)
		h.checks.reset(redRounds("again", 8)...)
		e, err := h.resume(stopped.ID, h.proj, "", 5)
		if err != nil {
			t.Fatal(err)
		}
		if rec := requireState(t, e, runrec.StatePaused); rec.RoundLimit != 9 || rec.Round != 9 {
			t.Fatalf("round %d limit %d, want 9 (round 4 plus 5)", rec.Round, rec.RoundLimit)
		}
	})
}

func TestResumeRefusesARunWithARoleStillRunning(t *testing.T) {
	h := resumeHarness(t)
	h.mkWorktree()
	h.seed("run-a", runrec.StateStopped, runningRole(RoleImplementer, 111, "2"))
	run, _ := h.store.Open("run-a")
	if err := run.Update(func(r *runrec.Record) { r.StoppedFrom = runrec.StateImplement }); err != nil {
		t.Fatal(err)
	}
	_, err := h.resume("run-a", h.proj, "")
	if err == nil || !IsRefusal(err) || !strings.Contains(err.Error(), "role still running") {
		t.Fatalf("error %v", err)
	}
}

func TestStopRecordsTheStateItStoppedFrom(t *testing.T) {
	t.Run("awaiting approval", func(t *testing.T) {
		h := resumeHarness(t)
		e := h.start()
		requireState(t, e, runrec.StateAwaitingApproval)
		if err := e.Reject(context.Background(), "no"); err != nil {
			t.Fatal(err)
		}
		if got := requireState(t, e, runrec.StateStopped).StoppedFrom; got != runrec.StateAwaitingApproval {
			t.Errorf("stopped_from %q", got)
		}
	})
	t.Run("a role stop in review", func(t *testing.T) {
		h := resumeHarness(t)
		h.runner.script = func(req Request, n int) (Result, error) {
			if req.Role == RoleReviewer {
				return Result{Exit: ExitAuth, Session: "s", Report: cleanReview}, nil
			}
			return Result{}, nil
		}
		e := h.start()
		if got := requireState(t, e, runrec.StateStopped).StoppedFrom; got != runrec.StateReview {
			t.Errorf("stopped_from %q", got)
		}
	})
	t.Run("a paused run stops from its pending step", func(t *testing.T) {
		h := resumeHarness(t)
		h.seed("run-a", runrec.StatePaused)
		run, _ := h.store.Open("run-a")
		if err := run.Update(func(r *runrec.Record) {
			r.Pending = &runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateReview, Role: RoleReviewer, Round: 1, Reason: "bad report"}
		}); err != nil {
			t.Fatal(err)
		}
		e := Load(h.deps, h.proj, run)
		if err := e.Stop(context.Background(), "enough"); err != nil {
			t.Fatal(err)
		}
		if got := requireState(t, e, runrec.StateStopped).StoppedFrom; got != runrec.StateReview {
			t.Errorf("stopped_from %q, want the pending state review", got)
		}
	})
	t.Run("a queued run", func(t *testing.T) {
		h := resumeHarness(t)
		h.mkWorktree()
		h.cfg.Queued = true
		e, err := New(context.Background(), h.deps, h.cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.StopQueued(context.Background(), "project was removed"); err != nil {
			t.Fatal(err)
		}
		if got := requireState(t, e, runrec.StateStopped).StoppedFrom; got != runrec.StateQueued {
			t.Errorf("stopped_from %q", got)
		}
		if _, err := h.resume(e.Snapshot().ID, h.proj, ""); err == nil || !IsRefusal(err) || !strings.Contains(err.Error(), "never started") {
			t.Errorf("a queued run never started: %v", err)
		}
	})
}

// S296, owner decision 2026-11-07 (Q1, option b): ork run and ork wave start
// resume a stopped run only when its worktree holds what the last implementer
// round left, and the engine answers that from tracked content and untracked
// names, as the round's own fingerprint was taken (tree_sha256, S294).
func TestTreeMatchesTheLastImplementerRound(t *testing.T) {
	const diff = "diff --git a/a.go b/a.go\n-old\n+same\n"
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(diff+"\x00")))
	ended := time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)
	row := func(round int, tree string) runrec.RoleRecord {
		return runrec.RoleRecord{Role: RoleImplementer, Round: round, TreeSHA256: tree, StartedAt: ended, EndedAt: ended, Exit: "ok"}
	}
	for name, tc := range map[string]struct {
		roles     []runrec.RoleRecord
		diffExit  int
		wantRound int
		wantMatch bool
		wantErr   string
	}{
		"unchanged":                   {roles: []runrec.RoleRecord{row(3, fingerprint)}, wantRound: 3, wantMatch: true},
		"changed":                     {roles: []runrec.RoleRecord{row(3, "0123")}, wantRound: 3},
		"the last round is compared":  {roles: []runrec.RoleRecord{row(1, "0123"), row(2, fingerprint)}, wantRound: 2, wantMatch: true},
		"an earlier match is ignored": {roles: []runrec.RoleRecord{row(1, fingerprint), row(2, "0123")}, wantRound: 2},
		"no fingerprint":              {roles: []runrec.RoleRecord{row(2, "")}, wantRound: 2, wantErr: "fingerprint"},
		"no implementer round":        {wantErr: "no implementer round"},
		"git cannot read the tree":    {roles: []runrec.RoleRecord{row(2, fingerprint)}, diffExit: 128, wantRound: 2, wantErr: "git diff"},
	} {
		t.Run(name, func(t *testing.T) {
			stderr := ""
			if tc.diffExit != 0 {
				stderr = "fatal: bad object\nsecond line\n"
			}
			h := newHarness(t, func(o *harnessOpts) {
				o.routesFor = func(P, W string) []clitest.Route {
					return []clitest.Route{
						{Tool: "git", Args: []string{"-C", W, "diff", "HEAD", "--binary", "--no-ext-diff"}, Stdout: diff, Exit: tc.diffExit, Stderr: stderr},
						{Tool: "git", Args: []string{"-C", W, "ls-files", "--others", "--exclude-standard", "-z"}},
					}
				}
			})
			h.seed("run-a", runrec.StateStopped, tc.roles...)
			run, _ := h.store.Open("run-a")
			round, match, err := Load(h.deps, h.proj, run).TreeMatchesLastRound(context.Background())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || strings.Contains(err.Error(), "\n") {
					t.Fatalf("error %v, want one line naming %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if match != tc.wantMatch || round != tc.wantRound {
				t.Fatalf("round %d match %v, want round %d match %v", round, match, tc.wantRound, tc.wantMatch)
			}
		})
	}
}

// A run that resumes into implement (no implementer round ran) counts round 1
// as the first granted round, so N rounds run, not N+1.
func TestResumeWithNoImplementerRoundRunsExactlyTheGrantedRounds(t *testing.T) {
	h := resumeHarness(t)
	h.mkWorktree()
	h.seed("run-a", runrec.StateStopped)
	run, _ := h.store.Open("run-a")
	if err := run.Update(func(r *runrec.Record) { r.StoppedFrom = runrec.StateImplement }); err != nil {
		t.Fatal(err)
	}
	h.checks.outputs = redRounds("never green", 8)
	e, err := h.resume("run-a", h.proj, "", 4)
	if err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if got, want := h.runner.roles(), []string{"implementer-1", "implementer-2", "implementer-3", "implementer-4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles %v, want exactly the 4 granted rounds %v", got, want)
	}
	if rec.RoundLimit != 4 || rec.Round != 4 || rec.Pending == nil || rec.Pending.Kind != runrec.PendingRounds {
		t.Fatalf("round %d limit %d pending %+v", rec.Round, rec.RoundLimit, rec.Pending)
	}
}

// The checks path grants N fix rounds after the checks: the same count.
func TestResumeIntoChecksRunsExactlyTheGrantedFixRounds(t *testing.T) {
	h, stopped := stoppedAtTheRoundsPause(t)
	h.checks.reset(redRounds("after the resume", 8)...)
	e, err := h.resume(stopped.ID, h.proj, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StatePaused)
	if got, want := h.runner.roles()[3:], []string{"implementer-5", "implementer-6", "implementer-7"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles after the resume %v, want exactly 3 fix rounds %v", got, want)
	}
}

func TestCanResumeChangesNothingAndMirrorsTheResumeChecks(t *testing.T) {
	h, stopped := stoppedAtTheRoundsPause(t)
	saved, _ := h.store.Open(stopped.ID)
	e := Load(h.deps, h.proj, saved)
	before, err := os.ReadFile(filepath.Join(h.store.RunDir(stopped.ID), "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	writes := len(h.fake.Calls())
	if err := e.CanResume(context.Background()); err != nil {
		t.Fatalf("CanResume of a resumable run: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(h.store.RunDir(stopped.ID), "run.json"))
	if string(before) != string(after) {
		t.Error("CanResume wrote the record")
	}
	for _, c := range h.fake.Calls()[writes:] {
		if c.Tool == "slicer" || len(c.Args) > 2 && c.Args[2] == "worktree" {
			t.Errorf("CanResume made a write-like call: %+v", c)
		}
	}
	// A role still running.
	h2 := resumeHarness(t)
	h2.mkWorktree()
	h2.seed("run-a", runrec.StateStopped, runningRole(RoleImplementer, 111, "2"))
	run, _ := h2.store.Open("run-a")
	if err := run.Update(func(r *runrec.Record) { r.StoppedFrom = runrec.StateImplement }); err != nil {
		t.Fatal(err)
	}
	if err := Load(h2.deps, h2.proj, run).CanResume(context.Background()); err == nil || !IsRefusal(err) || !strings.Contains(err.Error(), "role still running") {
		t.Errorf("running role: %v", err)
	}
	// A committed run, and a run on another branch.
	h3 := resumeHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{
				{Tool: "git", Args: []string{"-C", W, "rev-parse", "--show-toplevel"}, Stdout: W + "\n"},
				{Tool: "git", Args: []string{"-C", W, "symbolic-ref", "--short", "HEAD"}, Stdout: "other\n"},
				{Tool: "git", Args: []string{"-C", W, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
				{Tool: "git", Args: []string{"-C", P, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
			}
		}
	})
	h3.mkWorktree()
	h3.seed("run-a", runrec.StateStopped)
	run, _ = h3.store.Open("run-a")
	if err := run.Update(func(r *runrec.Record) { r.StoppedFrom = runrec.StateReview }); err != nil {
		t.Fatal(err)
	}
	if err := Load(h3.deps, h3.proj, run).CanResume(context.Background()); err == nil || !IsRefusal(err) || !strings.Contains(err.Error(), "does not match the run") {
		t.Errorf("foreign branch: %v", err)
	}
}

func TestResumeJoinsTheWaveOfTheCaller(t *testing.T) {
	h, stopped := stoppedAtTheRoundsPause(t)
	h.checks.reset(CheckOutput{OK: true, Output: "ok\n"})
	saved, _ := h.store.Open(stopped.ID)
	e := Load(h.deps, h.proj, saved)
	if err := e.RetryNote(WithWave(context.Background(), "wave-new"), ""); err != nil {
		t.Fatal(err)
	}
	if got := e.Snapshot().Wave; got != "wave-new" {
		t.Fatalf("wave %q, want wave-new", got)
	}
	// Without a wave in the context the run keeps its own.
	h, stopped = stoppedAtTheRoundsPause(t)
	h.checks.reset(CheckOutput{OK: true, Output: "ok\n"})
	was := stopped.Wave
	e2, err := h.resume(stopped.ID, h.proj, "")
	if err != nil || e2.Snapshot().Wave != was {
		t.Fatalf("wave %q (%v), want %q", e2.Snapshot().Wave, err, was)
	}
}

func TestAFailedCommitStopsFromTheCommitState(t *testing.T) {
	h := resumeHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{{Tool: "git", Args: []string{"-C", W, "commit", "-m", gitTitle}, Exit: 1, Stderr: "fatal: no\n"}}
		}
	})
	e := h.start()
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateStopped)
	if !strings.HasPrefix(rec.Reason, "git commit:") || rec.StoppedFrom != runrec.StateCommit || rec.Pending != nil || len(rec.QueuedQuestions) != 0 {
		t.Fatalf("stop after a failed commit: reason %q stopped_from %q pending %+v queued %v", rec.Reason, rec.StoppedFrom, rec.Pending, rec.QueuedQuestions)
	}
}
