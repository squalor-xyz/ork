// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

func requireDirectoryMode0700(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Errorf("%s mode %s, want directory 0700", path, info.Mode())
	}
}

func TestRunDirectoriesAre0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix modes")
	}
	h := newHarness(t)
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	requireDirectoryMode0700(t, h.store.Dir)
	requireDirectoryMode0700(t, h.store.RunDir(rec.ID))
	reqs := h.runner.requests()
	if len(reqs) == 0 {
		t.Fatal("no role requests")
	}
	for _, req := range reqs {
		requireDirectoryMode0700(t, filepath.Dir(req.TempDir))
		requireDirectoryMode0700(t, req.TempDir)
	}
}

func TestPermissionCancelledGrokReviewerFallsBackToTheNextRow(t *testing.T) {
	h := newHarness(t)
	h.deps.Vendors = NewVendors(nil).WithClock(h.deps.Now)
	h.cfg.Lineup[RoleReviewer] = append([]runrec.LineupEntry{{Vendor: "grok", Model: "grok-4.7", Effort: "medium"}}, h.cfg.Lineup[RoleReviewer]...)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Vendor == "grok" {
			return Result{Exit: ExitError, Reason: "grok: cancelled on a permission prompt: refused command"}, nil
		}
		return defaultResult(req), nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	reqs := shapeRequests(h, RoleReviewer)
	if len(reqs) != 2 {
		t.Fatalf("reviewer requests = %d, want 2", len(reqs))
	}
	if reqs[0].Vendor != "grok" || reqs[1].Vendor != "claude" || reqs[0].Round != reqs[1].Round {
		t.Errorf("fallback requests = %+v", reqs)
	}
	if h.deps.Vendors.IsOut("grok") {
		t.Error("permission cancellation marked Grok out")
	}
	foundReport := false
	for _, role := range rec.Roles {
		if role.Role == RoleReviewer && role.Vendor == "claude" {
			foundReport = true
			b, err := os.ReadFile(filepath.Join(h.store.RunDir(rec.ID), role.Report))
			if err != nil || string(b) != defaultResult(reqs[1]).Report {
				t.Errorf("fallback report = %q, err=%v", b, err)
			}
		}
	}
	if !foundReport {
		t.Fatal("no Claude reviewer report recorded")
	}
}

func TestSpecCheckTempDirectoryIs0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix modes")
	}
	h := specHarness(t)
	cfg := specConfig(h, t)
	if _, err := RunSpecCheck(context.Background(), h.deps, cfg); err != nil {
		t.Fatal(err)
	}
	requireDirectoryMode0700(t, filepath.Join(cfg.Dir, "tmp"))
}

func TestVendorReportModeIsLeftAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix modes")
	}
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		result := defaultResult(req)
		if err := os.WriteFile(req.ReportPath, []byte(result.Report), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(req.ReportPath, 0o644); err != nil {
			t.Fatal(err)
		}
		return result, nil
	}
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	for _, req := range h.runner.requests() {
		info, err := os.Stat(req.ReportPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode %04o, want 0644", req.ReportPath, info.Mode().Perm())
		}
		data, err := os.ReadFile(req.ReportPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != defaultResult(req).Report {
			t.Errorf("vendor report changed: %q", data)
		}
	}
}

// shapeRequests selects one role without depending on concurrent sibling ordering.
func shapeRequests(h *harness, role string) []Request {
	var out []Request
	for _, req := range h.runner.requests() {
		if req.Role == role {
			out = append(out, req)
		}
	}
	return out
}

func TestShapeProblemRetriesOnceOnItsOwn(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		res := defaultResult(req)
		if n == 0 {
			res.Report = "Done.\n" + ownerNone
		}
		return res, nil
	}
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	reqs := shapeRequests(h, RoleImplementer)
	if len(reqs) != 2 {
		t.Fatalf("implementer calls = %d, want 2", len(reqs))
	}
	if reqs[0].Round != reqs[1].Round || reqs[1].Resume != "sess-implementer" {
		t.Fatalf("retry changed round or session: %+v", reqs[1])
	}
	for _, want := range []string{"## Report shape", `no "Tests: N passed, M failed" line`} {
		if !strings.Contains(reqs[1].Brief, want) {
			t.Errorf("retry brief missing %q", want)
		}
	}
	if strings.Contains(reqs[1].Brief, "## Owner note") {
		t.Error("automatic retry attached an owner note")
	}
}

func TestShapeRetryLabelsOnlyTheRetryAttempt(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		res := defaultResult(req)
		if n == 0 {
			res.Report = "Done.\n" + ownerNone
		}
		return res, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	var attempts []runrec.RoleRecord
	for _, role := range rec.Roles {
		if role.Role == RoleImplementer && role.Round == 1 {
			attempts = append(attempts, role)
		}
	}
	if len(attempts) != 2 {
		t.Fatalf("implementer attempts = %d, want 2", len(attempts))
	}
	if attempts[0].ShapeRetry != nil {
		t.Fatalf("the refused attempt carries the retry label: %+v", attempts[0].ShapeRetry)
	}
	if attempts[1].ShapeRetry == nil {
		t.Fatal("the retry attempt lost its retry label")
	}
}

func TestShapeProblemPausesWhenTheRetryIsStillWrong(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		res := defaultResult(req)
		if n < 2 {
			res.Report = "Done.\n" + ownerNone
		}
		return res, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if got := len(h.runner.requests()); got != 2 {
		t.Fatalf("calls = %d, want 2 before pause", got)
	}
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape {
		t.Fatalf("pending = %+v", rec.Pending)
	}
	b, _ := json.Marshal(rec.Pending)
	if strings.Contains(string(b), `"automatic_shape_retry":true`) {
		t.Fatal("automatic retry marker remained on owner pause")
	}
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	if got := len(shapeRequests(h, RoleImplementer)); got != 3 {
		t.Fatalf("implementer calls = %d, want 3 after owner retry", got)
	}
}

func TestAutomaticShapeRetryDoesNotUseARound(t *testing.T) {
	h := newHarness(t)
	h.deps.MaxRounds = 1
	h.runner.script = func(req Request, n int) (Result, error) {
		res := defaultResult(req)
		if n == 0 {
			res.Report = "Done.\n" + ownerNone
		}
		return res, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if rec.Round != 1 {
		t.Fatalf("round = %d, want 1", rec.Round)
	}
	if got := shapeRequests(h, RoleReviewer); len(got) != 1 || got[0].Round != 1 {
		t.Fatalf("reviewer requests = %+v", got)
	}
}

func TestShapeRetryAppliesToEveryReportingRole(t *testing.T) {
	for _, role := range []string{RoleReviewer, RoleGate, RoleArchitect, RoleUI} {
		t.Run(role, func(t *testing.T) {
			h := newHarness(t)
			if role == RoleUI {
				h = uiHarness(t, tuiFiles)
			}
			if role == RoleArchitect {
				h.cfg.Lineup[role] = h.cfg.Lineup[RoleReviewer]
			}
			calls := 0
			h.runner.script = func(req Request, n int) (Result, error) {
				res := defaultResult(req)
				if req.Role == RoleArchitect {
					res.Report = architectReport
				}
				if req.Role == role {
					calls++
					if calls == 1 {
						res.Report = strings.Split(res.Report, ownerHeading)[0]
					}
				}
				return res, nil
			}
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			reqs := shapeRequests(h, role)
			if len(reqs) != 2 {
				t.Fatalf("%s calls = %d, want 2", role, len(reqs))
			}
			if reqs[0].Round != reqs[1].Round || reqs[1].Resume != "sess-"+role || !strings.Contains(reqs[1].Brief, "## Report shape") {
				t.Fatalf("%s retry lost round/session/brief", role)
			}
		})
	}
}

func TestShapeProblemWithAQuestionStillPausesForTheOwner(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		return Result{Exit: ExitOK, Report: "## Owner needed\nWhich format?\n"}, nil
	}
	rec := requireState(t, h.start(), runrec.StatePaused)
	if got := len(h.runner.requests()); got != 1 {
		t.Fatalf("calls = %d, want immediate owner pause", got)
	}
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape {
		t.Fatalf("pending = %+v", rec.Pending)
	}
}

func TestShapeRetryIsRecorded(t *testing.T) {
	for _, roleOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "role-only"}[roleOnly], func(t *testing.T) {
			h := newHarness(t)
			e, err := New(context.Background(), h.deps, h.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.run.Update(func(rec *runrec.Record) { rec.State = runrec.StateImplement }); err != nil {
				t.Fatal(err)
			}
			h.runner.script = func(req Request, n int) (Result, error) {
				report := "Done.\n" + ownerNone
				if n > 0 {
					report = "Still bad.\n" + ownerNone
				}
				return Result{Exit: ExitOK, Session: "first-session", Report: report}, nil
			}
			r := round{role: RoleImplementer, n: 1, state: runrec.StateImplement, task: "implementer"}
			if _, err := e.runRound(context.Background(), r, nil); err != nil {
				t.Fatal(err)
			}
			rec := requireState(t, e, runrec.StateImplement)
			if rec.Pending == nil {
				t.Fatal("automatic retry not recorded")
			}
			data, _ := json.Marshal(rec.Pending)
			if !strings.Contains(string(data), `"automatic_shape_retry":true`) {
				t.Fatalf("missing automatic retry marker: %s", data)
			}
			refused, err := os.ReadFile(filepath.Join(e.run.Dir(), rec.Roles[0].Report))
			if err != nil {
				t.Fatal(err)
			}
			// A concurrent sibling can finish and consume the single pending slot.
			if roleOnly {
				if err := e.run.Update(func(rr *runrec.Record) { rr.Pending = nil }); err != nil {
					t.Fatal(err)
				}
			}
			loaded, err := h.store.Open(rec.ID)
			if err != nil {
				t.Fatal(err)
			}
			e = Load(h.deps, h.proj, loaded)
			if err := e.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			// Nothing relaunches a run the daemon finds active, so it must rest:
			// paused on shape, with no automatic marker left anywhere.
			rec = requireState(t, e, runrec.StatePaused)
			if rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape ||
				rec.Pending.Role != RoleImplementer || rec.Pending.Round != 1 {
				t.Fatalf("pending after restart = %+v", rec.Pending)
			}
			if rec.AutomaticShapeRetry() != nil {
				t.Fatal("automatic marker survived the restart pause")
			}
			if got := len(h.runner.requests()); got != 1 {
				t.Fatalf("calls = %d before the owner retry, want 1", got)
			}
			if err := e.Retry(context.Background()); err != nil {
				t.Fatal(err)
			}
			rec = requireState(t, e, runrec.StatePaused)
			if got := len(h.runner.requests()); got != 2 {
				t.Fatalf("calls = %d after reload, want 2", got)
			}
			if h.runner.requests()[1].Resume != "first-session" {
				t.Fatal("reload lost refused session")
			}
			again, err := os.ReadFile(filepath.Join(e.run.Dir(), rec.Roles[0].Report))
			if err != nil || string(again) != string(refused) {
				t.Fatal("refused report was overwritten")
			}
			if rec.Roles[0].Report == rec.Roles[1].Report {
				t.Fatal("attempt reports share a file")
			}
		})
	}
}
