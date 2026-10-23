// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

const architectReport = "CONFIRMED: states.go:10 keep state ownership in the engine.\nLater: none.\nNo blockers.\n\n## Owner needed\nnone\n"

func architectHarness(t *testing.T, opts ...func(*harnessOpts)) *harness {
	t.Helper()
	h := newHarness(t, opts...)
	h.cfg.Lineup["architect"] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "high"}}
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == "architect" {
			return Result{Exit: ExitOK, Session: "architect-session", Report: architectReport}, nil
		}
		return Result{}, nil
	}
	return h
}

func TestArchitectRunsOnlyWhenTheLineupHasOne(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "present"}[present], func(t *testing.T) {
			h := architectHarness(t)
			if !present {
				delete(h.cfg.Lineup, "architect")
			}
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			want := []string{"implementer-1", "reviewer-1", "gate-1"}
			if present {
				// The harness slice is size M: the lineup alone enables the role (adj 151).
				want = []string{"implementer-1", "reviewer-1", "architect-1", "implementer-2", "gate-2"}
			}
			if got := h.runner.roles(); !reflect.DeepEqual(got, want) {
				t.Fatalf("roles %v, want %v", got, want)
			}
		})
	}
}

func TestArchitectRunsAfterTheFixRoundAndBeforeTheGate(t *testing.T) {
	h := architectHarness(t)
	arch := h.runner.script
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleReviewer {
			return Result{Exit: ExitOK, Session: "review-session", Report: findingsReview("states.go:10 fix the reviewer finding")}, nil
		}
		return arch(req, n)
	}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	want := []string{"implementer-1", "reviewer-1", "implementer-2", "architect-2", "implementer-3", "gate-3"}
	if got := h.runner.roles(); !reflect.DeepEqual(got, want) {
		t.Fatalf("roles %v, want %v", got, want)
	}
	archReq := h.runner.requests()[3]
	for _, text := range []string{h.W, "architect-2.md", "tmp/architect-2", implReport, "states.go:10 fix the reviewer finding", "For each reviewer finding under Earlier reports", "CONFIRMED-FIXED", "PARTIAL", "NOT-FIXED", "with evidence"} {
		if !strings.Contains(archReq.Brief, text) {
			t.Errorf("architect brief missing %q", text)
		}
	}
	if strings.Contains(archReq.Brief, "{{") {
		t.Fatal("architect prompt has an unfilled placeholder")
	}
}

func TestArchitectIsReadOnlyAndFingerprinted(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
			after := " M existing.go\n"
			if changed {
				after += "?? architect.go\n"
			}
			// First two fingerprints belong to the reviewer; next two to the architect.
			h := architectHarness(t, statusRoute("", "", " M existing.go\n", after))
			want := runrec.StateAwaitingApproval
			if changed {
				want = runrec.StateStopped
			}
			rec := requireState(t, h.start(), want)
			reqs := h.runner.requests()
			if len(reqs) < 3 || reqs[2].Role != "architect" || !reqs[2].ReadOnly || len(reqs[2].Allowlist) != 0 {
				t.Fatalf("architect request missing or not read-only: roles %v", h.runner.roles())
			}
			if changed && (len(reqs) != 3 || !strings.Contains(rec.Reason, "architect round 1 changed the tree")) {
				t.Fatalf("fingerprint did not stop before fix/gate: %+v", rec)
			}
		})
	}
}

func TestArchitectFindingsGoBackToTheImplementer(t *testing.T) {
	for _, report := range []string{architectReport, "No blockers.\n\n## Owner needed\nnone\n"} {
		t.Run(strings.Split(report, "\n")[0], func(t *testing.T) {
			h := architectHarness(t)
			h.runner.script = func(req Request, _ int) (Result, error) {
				if req.Role == "architect" {
					return Result{Exit: ExitOK, Report: report}, nil
				}
				return Result{}, nil
			}
			rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
			reqs := h.runner.requests()
			if len(reqs) != 5 || reqs[3].Role != RoleImplementer || reqs[3].Resume != "sess-implementer" || !strings.Contains(reqs[3].Brief, report) || !strings.Contains(reqs[3].Brief, "architect") {
				t.Fatalf("architect report did not reach fix round: %v", h.runner.roles())
			}
			gate := reqs[4]
			// The gate resumes the reviewer, so Earlier reports alone cannot supply this context.
			if gate.Resume != "sess-reviewer" || !strings.Contains(gate.Brief, report) || !strings.Contains(gate.Brief, "## Architect report (architect-1)") {
				t.Fatal("resumed gate missing architect report")
			}
			if h.readReport(rec, "architect-1.md") != report {
				t.Fatal("architect report not preserved")
			}
		})
	}
}

func TestArchitectShapeErrorPausesAndRetryResumesIt(t *testing.T) {
	h := architectHarness(t)
	arch := h.runner.script
	bad := true
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == "architect" && bad {
			bad = false
			return Result{Exit: ExitOK, Session: "architect-session", Report: "No blockers, but missing the owner section."}, nil
		}
		return arch(req, n)
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if p := rec.Pending; p == nil || p.Kind != runrec.PendingShape || p.Role != "architect" || p.State != "architect" || !strings.Contains(p.Reason, "Owner needed") {
		t.Fatalf("pending: %+v", p)
	}
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	req := h.runner.requests()[3]
	if req.Role != "architect" || req.Resume != "architect-session" || !strings.Contains(req.Brief, "Owner needed") || filepath.Base(req.ReportPath) != "architect-1-attempt-2.md" {
		t.Fatalf("retry: %+v", req)
	}
}

func TestArchitectRoundIsALedgerStepUnderTheOrchestrator(t *testing.T) {
	h := architectHarness(t)
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	var starts, ends int
	var step string
	for _, s := range h.steps() {
		if s.Role == "architect" {
			starts++
			if s.Parent != rec.OrchestratorStep || s.Phase != "start" || s.Agent != "claude" || s.Vendor != "claude" || s.Model != "opus" || s.Reasoning != "high" || s.Task != item+" architect" {
				t.Errorf("architect ledger start: %+v", s)
			}
		}
	}
	for _, r := range rec.Roles {
		if r.Role == "architect" {
			step = r.Step
		}
	}
	for _, s := range h.steps() {
		if step != "" && s.Step == step && s.Phase == "done" {
			ends++
		}
	}
	if starts != 1 || ends != 1 {
		t.Fatalf("architect ledger starts %d, ends %d, step %q", starts, ends, step)
	}
}

func TestArchitectFallbackUsesTheNextRow(t *testing.T) {
	for _, class := range []ExitClass{ExitRateLimited, ExitError} {
		t.Run(string(class), func(t *testing.T) {
			h := architectHarness(t)
			h.cfg.Lineup["architect"] = []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}, {Vendor: "claude", Model: "opus", Effort: "high"}}
			arch := h.runner.script
			h.runner.script = func(req Request, n int) (Result, error) {
				if req.Role == "architect" && req.Vendor == "codex" {
					return Result{Exit: class, Session: "failed-architect"}, nil
				}
				return arch(req, n)
			}
			rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
			if got := h.runner.roles(); !reflect.DeepEqual(got, []string{"implementer-1", "reviewer-1", "architect-1", "architect-1", "implementer-2", "gate-2"}) {
				t.Fatalf("fallback roles: %v", got)
			}
			req := h.runner.requests()[3]
			if req.Vendor != "claude" || req.Model != "opus" || req.Effort != "high" || req.Resume != "" || filepath.Base(req.ReportPath) != "architect-1-attempt-2.md" || rec.Roles[2].Exit != string(class) || rec.Roles[3].Report != "architect-1-attempt-2.md" {
				t.Fatalf("fallback request/records: %+v / %+v", req, rec.Roles)
			}
			if currentEntry(rec, "architect") != 1 {
				t.Fatal("architect fallback selection was not sticky")
			}
		})
	}
}

func TestArchitectReportChecksOnlyTheOwnerSection(t *testing.T) {
	if _, err := CheckReport("architect", false, architectReport); err != nil {
		t.Fatal(err)
	}
	_, err := CheckReport("architect", false, "No blockers.")
	var shape *ShapeError
	if !errors.As(err, &shape) || len(shape.Problems) != 1 || !strings.Contains(shape.Problems[0], "Owner needed") {
		t.Fatalf("architect shape: %v", err)
	}
}

func TestArchitectWaitsForGreenChecksAndDoesNotRepeatAfterGateFail(t *testing.T) {
	h := architectHarness(t)
	// First review fix fails its checks; the architect fix also fails once.
	h.checks.outputs = []CheckOutput{{OK: true}, {OK: false}, {OK: true}, {OK: false}, {OK: true}}
	arch := h.runner.script
	failedGate := false
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleReviewer {
			return Result{Exit: ExitOK, Report: findingsReview("states.go:10 fix the reviewer finding")}, nil
		}
		if req.Role == RoleGate && !failedGate {
			failedGate = true
			return Result{Exit: ExitOK, Report: gateFail}, nil
		}
		return arch(req, n)
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	want := []string{"implementer-1", "reviewer-1", "implementer-2", "implementer-3", "architect-3", "implementer-4", "implementer-5", "gate-5", "implementer-6", "gate-6"}
	if got := h.runner.roles(); !reflect.DeepEqual(got, want) {
		t.Fatalf("roles %v, want %v", got, want)
	}
	if rec.Fails != 1 || len(rec.Checks) != 6 {
		t.Fatalf("fails %d, checks %+v", rec.Fails, rec.Checks)
	}
}

func TestArchitectOwnerQuestionResumesAfterReload(t *testing.T) {
	h := architectHarness(t)
	arch := h.runner.script
	asked := false
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == "architect" && !asked {
			asked = true
			return Result{Exit: ExitOK, Session: "architect-question", Report: "No blockers.\n\n## Owner needed\n" + question + "\n"}, nil
		}
		return arch(req, n)
	}
	rec := requireState(t, h.start(), runrec.StatePaused)
	if p := rec.Pending; p == nil || p.Kind != runrec.PendingQuestion || p.Role != "architect" || p.State != "architect" {
		t.Fatalf("pending: %+v", p)
	}
	stored, err := h.store.Open(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	e := Load(h.deps, h.proj, stored)
	if err := e.Answer(context.Background(), "4"); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	req := h.runner.requests()[3]
	if req.Role != "architect" || req.Resume != "architect-question" || !req.ReadOnly || !strings.Contains(req.Brief, question) || !strings.Contains(req.Brief, "\n4\n") {
		t.Fatal("architect retry lost its session, permission, question or answer")
	}
}
