// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func requireOneLineReason(t *testing.T, h *harness) {
	t.Helper()
	var found bool
	for _, call := range h.calls("slicer", "relay-run", "step") {
		for i, arg := range call {
			if arg == "--reason" && i+1 < len(call) {
				found = true
				if call[i+1] != "rate limited retry later" {
					t.Errorf("reason = %q, want one line", call[i+1])
				}
			}
		}
	}
	if !found {
		t.Fatal("no fail reason recorded")
	}
}
func TestFix1RoleFailReasonIsOneLine(t *testing.T) {
	h := fallbackHarness(t, RoleImplementer)
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleImplementer && req.Vendor == "codex" {
			return Result{Exit: ExitRateLimited, Reason: "rate limited\nretry\r later"}, nil
		}
		return Result{}, nil
	}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	requireOneLineReason(t, h)
}
func TestFix1SpecFailReasonIsOneLine(t *testing.T) {
	h := specHarness(t)
	cfg := specConfig(h, t)
	cfg.Lineup = append([]runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "medium"}}, cfg.Lineup...)
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Vendor == "codex" {
			return Result{Exit: ExitRateLimited, Reason: "rate limited\nretry\r later"}, nil
		}
		return Result{Exit: ExitOK, Report: specReport}, nil
	}
	if _, err := RunSpecCheck(context.Background(), h.deps, cfg); err != nil {
		t.Fatal(err)
	}
	requireOneLineReason(t, h)
}
func TestFix1OldRunOmitsEmptyContractCriteria(t *testing.T) {
	h := newHarness(t)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.run.Update(func(r *runrec.Record) { r.GateCriteria = nil }); err != nil {
		t.Fatal(err)
	}
	e = Load(h.deps, h.proj, e.run)
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	var found bool
	for _, req := range h.runner.requests() {
		if req.Role == RoleGate {
			found = true
			if strings.Contains(req.Brief, "The criteria from slicer are:") {
				t.Error("old run got an empty criteria section")
			}
		}
	}
	if !found {
		t.Fatal("gate did not run")
	}
}
func TestFix1SwitchReasonsAreWords(t *testing.T) {
	for _, tc := range []struct {
		exit  ExitClass
		words string
	}{{ExitOutOfCredits, "out of credits"}, {ExitRateLimited, "rate limited"}, {ExitError, "error exit"}} {
		rec := runrec.Record{Roles: []runrec.RoleRecord{{Role: RoleImplementer, Vendor: "codex", Model: "sol", Exit: string(tc.exit)}, {Role: RoleImplementer, Vendor: "claude", Model: "sonnet", Exit: string(ExitOK)}}}
		got := roleSwitches(rec)
		if len(got) != 1 || !strings.HasSuffix(got[0], "("+tc.words+")") {
			t.Errorf("%s: %v", tc.exit, got)
		}
	}
}

const fix1Question = "Question: Which cap?\nOptions:\n1. four: cap at 4\n2. eight: cap at 8\nRecommend: 1 - smaller\n"

func TestFix1LedgerFailuresAreAdvisory(t *testing.T) {
	for _, operation := range []string{"ask", "answer", "wait"} {
		for _, exit := range []int{1, 3} {
			t.Run(fmt.Sprintf("%s/%d", operation, exit), func(t *testing.T) {
				args := []string{"relay-run", operation}
				if operation == "wait" {
					args = []string{"relay-run", "step", "run-1", "--step", "1", "--phase", "wait"}
				}
				h := newHarness(t, func(o *harnessOpts) {
					o.routes = []clitest.Route{{Tool: "slicer", Args: args, Prefix: true, Exit: exit, Stderr: "ledger unavailable"}}
				})
				h.runner.script = func(req Request, n int) (Result, error) {
					if n == 0 {
						return Result{Exit: ExitOK, Report: strings.Replace(implReport, "## Owner needed\n\nnone\n", "## Owner needed\n\n"+fix1Question, 1)}, nil
					}
					return Result{}, nil
				}
				e := h.start()
				requireState(t, e, runrec.StatePaused)
				if err := e.Answer(context.Background(), "eight"); err != nil {
					t.Fatal(err)
				}
				rec := requireState(t, e, runrec.StateAwaitingApproval)
				failed := false
				for _, w := range rec.Writes {
					if w.Exit == exit && strings.Contains(join(w.Argv), join(args)) {
						failed = true
					}
				}
				if !failed {
					t.Fatal("fake ledger failure was not exercised")
				}
			})
		}
	}
}
func TestFix1RetryMirrorsQueuedQuestion(t *testing.T) {
	h, e, _ := retryWithQueuedQuestion(t)
	if err := e.run.Update(func(r *runrec.Record) {
		r.QueuedQuestions[0].Ask = &runrec.OwnerQuestion{Title: "Which library?", Options: []runrec.Option{{Label: "stdlib", Summary: "use standard library"}, {Label: "existing", Summary: "reuse existing"}}, Recommend: 1, Reason: "no dependency"}
	}); err != nil {
		t.Fatal(err)
	}
	before := len(h.runner.requests())
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.LedgerQuestion == "" || !ledgerHas(h, "relay-run ask", "--title Which library?") {
		t.Fatal("queued question was not mirrored")
	}
	if len(h.runner.requests()) != before {
		t.Fatal("role ran before the owner answered")
	}
	if err := e.Answer(context.Background(), "stdlib"); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	if !ledgerHas(h, "relay-run answer", "--text stdlib") {
		t.Fatal("queued answer was not mirrored")
	}
}
