// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestS17GatePassWithFailedTestsCannotReachApproval(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate {
			return Result{Exit: ExitOK, Report: strings.Replace(gatePass, "0 failed", "1 failed", 1)}, nil
		}
		return Result{}, nil
	}
	requireState(t, h.start(), runrec.StatePaused)
}

type refusingProc struct{}

func (refusingProc) Alive(int) bool { return true }
func (refusingProc) Kill(int) error { return errors.New("kill refused") }

func TestS17FailedKillKeepsRoleRunningAndRetryBlocked(t *testing.T) {
	h := newHarness(t)
	h.seed("run-a", runrec.StateImplement, runningRole(RoleImplementer, 111, "2"))
	run, err := h.store.Open("run-a")
	if err != nil {
		t.Fatal(err)
	}
	h.deps.Proc = refusingProc{}
	e := Load(h.deps, h.proj, run)
	if err := e.Reconcile(context.Background()); err == nil {
		t.Fatal("kill failure hidden")
	}
	if !e.Snapshot().Roles[0].Running() {
		t.Fatal("failed kill marked role ended")
	}
	if err := e.Retry(context.Background()); err == nil {
		t.Fatal("retry allowed while original role is live")
	}
}

func TestS17AwaitingApprovalIgnoresStaleRole(t *testing.T) {
	h := newHarness(t)
	h.seed("run-a", runrec.StateAwaitingApproval, runningRole(RoleGate, 111, "2"))
	requireState(t, h.recover()["run-a"], runrec.StateAwaitingApproval)
}

func TestS17AnswerSurvivesCrashDuringResumedRound(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if n == 0 {
			return Result{Exit: ExitOK, Session: "s", Report: withQuestion(implReport)}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	ctx, cancel := context.WithCancel(context.Background())
	h.runner.script = func(req Request, n int) (Result, error) { cancel(); return Result{}, ctx.Err() }
	if err := e.Answer(ctx, "8"); !errors.Is(err, context.Canceled) {
		t.Fatalf("answer: %v", err)
	}
	e = h.recover()[e.Snapshot().ID]
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && !strings.Contains(req.Brief, "\n8\n") {
			t.Error("recovered brief lost owner answer")
		}
		return Result{}, nil
	}
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestS17SetupRetryReusesMatchingWorktree(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{
				{Tool: "git", Args: []string{"-C", W, "rev-parse", "--show-toplevel"}, Stdout: W + "\n"},
				{Tool: "git", Args: []string{"-C", W, "symbolic-ref", "--short", "HEAD"}, Stdout: item + "\n"},
				{Tool: "git", Args: []string{"-C", W, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
				{Tool: "git", Args: []string{"-C", P, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
				{Tool: "git", Args: []string{"-C", P, "worktree", "add"}, Prefix: true, Exit: 128, Stderr: "already exists"},
			}
		}
	})
	if err := os.MkdirAll(h.W, 0755); err != nil {
		t.Fatal(err)
	}
	h.seed("run-a", runrec.StateSetup)
	e := h.recover()["run-a"]
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
}

func TestS17EachAttemptKeepsItsOwnReport(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if n == 0 {
			return Result{Exit: ExitOK, Report: withQuestion(implReport)}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	first := h.runner.requests()[0].ReportPath
	if err := e.Answer(context.Background(), "8"); err != nil {
		t.Fatal(err)
	}
	second := h.runner.requests()[1].ReportPath
	if first == second {
		t.Fatal("resumed attempt reused report path")
	}
	b, err := os.ReadFile(first)
	if err != nil || string(b) != withQuestion(implReport) {
		t.Fatalf("first report overwritten: %q %v", b, err)
	}
}

func TestS17RetryRefusesPausedLiveRole(t *testing.T) {
	h := newHarness(t)
	h.seed("run-a", runrec.StatePaused, runningRole(RoleImplementer, 111, "2"))
	run, err := h.store.Open("run-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Update(func(r *runrec.Record) {
		r.Pending = &runrec.Pending{Kind: runrec.PendingLost, State: runrec.StateImplement}
	}); err != nil {
		t.Fatal(err)
	}
	e := Load(h.deps, h.proj, run)
	if err := e.Retry(context.Background()); err == nil {
		t.Fatal("retry allowed a second role while original role runs")
	}
}

func TestS17AnswerSurvivesCrashBeforeResumedRoleStarts(t *testing.T) {
	h := newHarness(t)
	h.seed("run-a", runrec.StateImplement)
	run, err := h.store.Open("run-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Update(func(r *runrec.Record) {
		r.Pending = &runrec.Pending{Kind: runrec.PendingQuestion, State: runrec.StateImplement, Question: question, Answer: "8"}
	}); err != nil {
		t.Fatal(err)
	}
	e := h.recover()["run-a"]
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && !strings.Contains(req.Brief, "\n8\n") {
			t.Error("pre-start recovery lost owner answer")
		}
		return Result{}, nil
	}
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
}
