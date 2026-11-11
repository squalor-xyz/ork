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
	"time"

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

// misnumbered is an owner finding whose second option is numbered 3.
const misnumbered = "- [owner] Which format?\n  Options:\n  1. json: machine readable\n  3. text: human readable\n  Recommend: 1 - tools read it"

// noOptions is an owner finding that gives a detail and no options.
const noOptions = "- [owner] Which format?\n  Needs a call from the owner; no options yet."

// reviewerReports makes the reviewer give each report in turn, and the last one
// for every later call. The session is sess.
func reviewerReports(h *harness, sess string, reports ...string) {
	calls := 0
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role != RoleReviewer {
			return Result{}, nil
		}
		i := min(calls, len(reports)-1)
		calls++
		return Result{Exit: ExitOK, Session: sess, Report: reports[i]}, nil
	}
}

func TestShapeRetryBriefCarriesTheCauseAndTheFindingText(t *testing.T) {
	h := newHarness(t)
	reviewerReports(h, "sess-reviewer", review(misnumbered, "nothing", "nothing", "nothing"), cleanReview)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	reqs := shapeRequests(h, RoleReviewer)
	if len(reqs) != 2 {
		t.Fatalf("reviewer calls = %d, want 2", len(reqs))
	}
	section := shapeSection(reqs[1].Brief)
	for _, want := range []string{"option 2 is numbered 3", "[owner] Which format?", "1. json: machine readable", "3. text: human readable", "Recommend: 1 - tools read it"} {
		if !strings.Contains(section, want) {
			t.Errorf("Report shape section lacks %q:\n%s", want, section)
		}
	}
}

func TestShapeRetryBriefHoldsWithoutAResumableSession(t *testing.T) {
	h := newHarness(t)
	// The vendor gave no session id, so the retry resumes nothing.
	reviewerReports(h, "", review(misnumbered, "nothing", "nothing", "nothing"), cleanReview)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	reqs := shapeRequests(h, RoleReviewer)
	if len(reqs) != 2 || reqs[1].Resume != "" {
		t.Fatalf("reviewer requests %+v, want 2 and no resumed session", reqs)
	}
	// The error quotes only the offending line; the finding's other lines are in
	// the brief only because it carries the finding.
	// A brief with no session also lists the earlier reports, the refused one
	// whole, so only the section itself shows what the check fix is for.
	section := shapeSection(reqs[1].Brief)
	for _, want := range []string{"option 2 is numbered 3", "[owner] Which format?", "1. json: machine readable", "Recommend: 1 - tools read it"} {
		if !strings.Contains(section, want) {
			t.Errorf("Report shape section lacks %q:\n%s", want, section)
		}
	}
}

// shapeSection is the "Report shape" section of a brief.
func shapeSection(brief string) string {
	_, rest, _ := strings.Cut(brief, "## Report shape\n")
	body, _, _ := strings.Cut(rest, "\n## ")
	return body
}

func TestShapeFindingTextIsCutTo2KiB(t *testing.T) {
	if got := shapeFindingText([]string{"- [owner] a", "- [owner] b"}); got != "- [owner] a\n\n- [owner] b" {
		t.Errorf("short findings %q", got)
	}
	got := shapeFindingText([]string{strings.Repeat("é", 3000)})
	if !strings.HasSuffix(got, "\n(cut)") || len(got) > runrec.MaxShapeFindingBytes+len("\n(cut)") || strings.ContainsRune(got, '�') {
		t.Errorf("long finding: %d bytes, suffix %q", len(got), got[max(0, len(got)-8):])
	}
}

func TestNextShapePauses(t *testing.T) {
	shape := func(role string, round, n int) *runrec.Pending {
		return &runrec.Pending{Kind: runrec.PendingShape, Role: role, Round: round, ShapePauses: n}
	}
	for name, tc := range map[string]struct {
		pend *runrec.Pending
		want int
	}{
		"no context":                 {nil, 1},
		"after the automatic retry":  {shape(RoleReviewer, 1, 0), 1},
		"after one pause":            {shape(RoleReviewer, 1, 1), 2},
		"after two pauses":           {shape(RoleReviewer, 1, 2), 3},
		"another role":               {shape(RoleGate, 1, 1), 1},
		"another round":              {shape(RoleReviewer, 2, 1), 1},
		"another kind of pause":      {&runrec.Pending{Kind: runrec.PendingBudget, Role: RoleReviewer, Round: 1, ShapePauses: 1}, 1},
		"an answered owner question": {&runrec.Pending{Kind: runrec.PendingQuestion, Role: RoleReviewer, Round: 1}, 1},
	} {
		if got := nextShapePauses(tc.pend, RoleReviewer, 1); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
}

func TestShapePauseCounterCountsConsecutivePausesOfOneRoleAndRound(t *testing.T) {
	bad := review(misnumbered, "nothing", "nothing", "nothing")
	t.Run("two pauses in a row", func(t *testing.T) {
		h := newHarness(t)
		reviewerReports(h, "sess-reviewer", bad)
		e := h.start()
		rec := requireState(t, e, runrec.StatePaused)
		if rec.Pending.Kind != runrec.PendingShape || rec.Pending.ShapePauses != 1 {
			t.Fatalf("first pause %+v, want shape count 1", rec.Pending)
		}
		if err := e.RetryNote(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		rec = requireState(t, e, runrec.StatePaused)
		if rec.Pending.Kind != runrec.PendingShape || rec.Pending.ShapePauses != 2 {
			t.Fatalf("second pause %+v, want shape count 2", rec.Pending)
		}
	})
	t.Run("a clean report ends the count", func(t *testing.T) {
		h := newHarness(t)
		reviewerReports(h, "sess-reviewer", bad, bad, cleanReview)
		e := h.start()
		requireState(t, e, runrec.StatePaused)
		if err := e.RetryNote(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		if rec := requireState(t, e, runrec.StateAwaitingApproval); rec.Pending != nil {
			t.Fatalf("pending %+v after a clean round", rec.Pending)
		}
	})
	t.Run("another role starts again", func(t *testing.T) {
		h := newHarness(t)
		impl := 0
		h.runner.script = func(req Request, n int) (Result, error) {
			switch req.Role {
			case RoleImplementer:
				impl++
				if impl <= 2 {
					return Result{Exit: ExitOK, Session: "sess-implementer", Report: "Done.\n" + ownerNone}, nil
				}
			case RoleReviewer:
				return Result{Exit: ExitOK, Session: "sess-reviewer", Report: bad}, nil
			}
			return Result{}, nil
		}
		e := h.start()
		rec := requireState(t, e, runrec.StatePaused)
		if rec.Pending.Role != RoleImplementer || rec.Pending.ShapePauses != 1 {
			t.Fatalf("implementer pause %+v", rec.Pending)
		}
		if err := e.RetryNote(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		rec = requireState(t, e, runrec.StatePaused)
		if rec.Pending.Role != RoleReviewer || rec.Pending.ShapePauses != 1 {
			t.Fatalf("reviewer pause %+v, want its own count 1", rec.Pending)
		}
	})
}

// cappedReviewer runs a reviewer that always gives report to its second shape
// pause, and returns the engine there.
func cappedReviewer(t *testing.T, h *harness, report string) (*Engine, runrec.Record) {
	t.Helper()
	reviewerReports(h, "sess-reviewer", report)
	e := h.start()
	requireState(t, e, runrec.StatePaused)
	if err := e.RetryNote(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	return e, requireState(t, e, runrec.StatePaused)
}

func TestSecondShapePauseNamesTheReportAndTheWayPast(t *testing.T) {
	h := newHarness(t)
	_, rec := cappedReviewer(t, h, review(misnumbered, "nothing", "nothing", "nothing"))
	for _, want := range []string{
		"option 2 is numbered 3", "reviewer-1-attempt-3.md",
		"ork show " + rec.ID, "ork retry " + rec.ID + " --accept-shape",
	} {
		if !strings.Contains(rec.Reason, want) || !strings.Contains(rec.Pending.Reason, want) {
			t.Errorf("reason %q lacks %q", rec.Reason, want)
		}
	}
	// The role's brief must not carry the owner's commands.
	if strings.Contains(rec.Pending.ShapeError, "ork retry") || !strings.Contains(rec.Pending.ShapeError, "option 2 is numbered 3") {
		t.Errorf("plain shape error %q", rec.Pending.ShapeError)
	}
}

func TestFirstShapePauseIsUnchanged(t *testing.T) {
	h := newHarness(t)
	reviewerReports(h, "sess-reviewer", review(misnumbered, "nothing", "nothing", "nothing"))
	rec := requireState(t, h.start(), runrec.StatePaused)
	if !strings.HasPrefix(rec.Reason, "report shape: ") || !strings.Contains(rec.Reason, "option 2 is numbered 3") {
		t.Errorf("reason %q", rec.Reason)
	}
	for _, not := range []string{"--accept-shape", "ork show", "ork retry"} {
		if strings.Contains(rec.Reason, not) {
			t.Errorf("first pause reason %q names %q", rec.Reason, not)
		}
	}
}

func TestPlainRetryAtTheShapeCapIsRefused(t *testing.T) {
	h := newHarness(t)
	e, rec := cappedReviewer(t, h, review(misnumbered, "nothing", "nothing", "nothing"))
	before := len(h.runner.requests())
	err := e.RetryNote(context.Background(), "go on")
	if !IsRefusal(err) {
		t.Fatalf("err %v, want a refusal", err)
	}
	for _, want := range []string{"ork show " + rec.ID, "ork retry " + rec.ID + " --accept-shape", "ork retry " + rec.ID + " --again", "ork stop " + rec.ID} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("refusal %q is not one line", err)
	}
	if got := len(h.runner.requests()); got != before {
		t.Errorf("a refused retry ran a role: %d requests, want %d", got, before)
	}
	requireState(t, e, runrec.StatePaused)
}

func TestRetryAgainAtTheShapeCapRerunsTheRoleOnce(t *testing.T) {
	h := newHarness(t)
	e, _ := cappedReviewer(t, h, review(misnumbered, "nothing", "nothing", "nothing"))
	before := len(h.runner.requests())
	if err := e.RetryWith(context.Background(), RetryOptions{Again: true}); err != nil {
		t.Fatal(err)
	}
	if got := len(h.runner.requests()); got != before+1 {
		t.Fatalf("requests %d, want %d: --again runs the role once", got, before+1)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending.ShapePauses != 3 || !strings.Contains(rec.Reason, "--accept-shape") {
		t.Errorf("pending %+v: a further failure must pause capped again", rec.Pending)
	}
	if err := e.RetryNote(context.Background(), ""); !IsRefusal(err) {
		t.Errorf("a plain retry after --again: err %v, want a refusal", err)
	}
}

func TestAcceptShapeKeepsAnOwnerFindingWithoutOptions(t *testing.T) {
	h := newHarness(t)
	e, _ := cappedReviewer(t, h, review(noOptions, "nothing", "nothing", "nothing"))
	before := len(shapeRequests(h, RoleReviewer))
	if err := e.RetryWith(context.Background(), RetryOptions{AcceptShape: true}); err != nil {
		t.Fatal(err)
	}
	if got := len(shapeRequests(h, RoleReviewer)); got != before {
		t.Fatalf("reviewer requests %d, want %d: accepting runs no role", got, before)
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if len(rec.Findings) != 1 {
		t.Fatalf("findings %+v", rec.Findings)
	}
	f := rec.Findings[0]
	if f.Kind != runrec.FindingOwner || f.Ask != nil || f.Title != "Which format?" || !strings.Contains(f.Text, "no options yet") {
		t.Errorf("finding %+v, want an owner finding with its text and no ask", f)
	}
	var accepted bool
	for _, role := range rec.Roles {
		if role.Role == RoleReviewer {
			accepted = role.ShapeAccepted
		}
	}
	if !accepted {
		t.Error("the reviewer's role record does not say the report was accepted")
	}
	// The run went on: the gate ran.
	if got := len(shapeRequests(h, RoleGate)); got != 1 {
		t.Errorf("gate requests %d, want 1", got)
	}
}

func TestAcceptShapeKeepsTheFixFindingsOfTheReport(t *testing.T) {
	h := newHarness(t)
	e, _ := cappedReviewer(t, h, review(fixFinding+"\n"+noOptions, "nothing", "nothing", "nothing"))
	if err := e.RetryWith(context.Background(), RetryOptions{AcceptShape: true}); err != nil {
		t.Fatal(err)
	}
	// The fix finding starts round 2; the reviewer of that round gives the same
	// report again, so the run pauses there.
	rec := e.Snapshot()
	if rec.Round != 2 || len(shapeRequests(h, RoleImplementer)) != 2 {
		t.Fatalf("round %d with %d implementer rounds: the fix finding did not start a fix round", rec.Round, len(shapeRequests(h, RoleImplementer)))
	}
	b, err := os.ReadFile(filepath.Join(e.run.Dir(), "findings-1.md"))
	if err != nil || !strings.Contains(string(b), "engine.go:42 drops the error.") {
		t.Errorf("findings file %q, %v", b, err)
	}
}

func TestAcceptShapeOfABlockerAsksAFreeTextQuestion(t *testing.T) {
	h := newHarness(t)
	blocker := "- [owner blocker] Which format?\n  Needs a call from the owner; no options yet."
	reviewerAsked := 0
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role != RoleReviewer {
			return Result{}, nil
		}
		reviewerAsked++
		if strings.Contains(req.Brief, "Owner answer") {
			return Result{Exit: ExitOK, Session: "sess-reviewer", Report: cleanReview}, nil
		}
		return Result{Exit: ExitOK, Session: "sess-reviewer", Report: review(blocker, "nothing", "nothing", "nothing")}, nil
	}
	e := h.start()
	requireState(t, e, runrec.StatePaused)
	if err := e.RetryNote(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StatePaused)
	ran := reviewerAsked
	if err := e.RetryWith(context.Background(), RetryOptions{AcceptShape: true}); err != nil {
		t.Fatal(err)
	}
	if reviewerAsked != ran {
		t.Fatalf("accepting ran the reviewer")
	}
	rec := requireState(t, e, runrec.StatePaused)
	p := rec.Pending
	if p == nil || p.Kind != runrec.PendingQuestion || p.Ask != nil || p.Role != RoleReviewer || !strings.Contains(p.Question, "Which format?") || !strings.Contains(p.Question, "no options yet") {
		t.Fatalf("pending %+v, want a free-text question with the finding text", p)
	}
	if len(rec.Questions) != 1 || rec.Questions[0].Ask != nil {
		t.Fatalf("questions %+v", rec.Questions)
	}
	if err := e.Answer(context.Background(), "json"); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
}

func TestAcceptShapeRefusesOtherProblems(t *testing.T) {
	const want = "only an owner finding's missing options can be accepted"
	noSection := strings.Split(cleanReview, ownerHeading)[0]
	t.Run("no Owner needed section", func(t *testing.T) {
		h := newHarness(t)
		e, _ := cappedReviewer(t, h, noSection)
		before := len(h.runner.requests())
		err := e.RetryWith(context.Background(), RetryOptions{AcceptShape: true})
		if !IsRefusal(err) || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "\n") {
			t.Fatalf("err %v, want a one-line refusal saying %q", err, want)
		}
		if len(h.runner.requests()) != before {
			t.Error("a refused accept ran a role")
		}
		requireState(t, e, runrec.StatePaused)
	})
	t.Run("a missing section beside an owner finding without options", func(t *testing.T) {
		h := newHarness(t)
		e, _ := cappedReviewer(t, h, strings.Split(review(noOptions, "nothing", "nothing", "nothing"), ownerHeading)[0])
		if err := e.RetryWith(context.Background(), RetryOptions{AcceptShape: true}); !IsRefusal(err) || !strings.Contains(err.Error(), want) {
			t.Fatalf("err %v, want a refusal saying %q", err, want)
		}
	})
	t.Run("a gate with no verdict", func(t *testing.T) {
		h := newHarness(t)
		h.runner.script = func(req Request, n int) (Result, error) {
			if req.Role == RoleGate {
				return Result{Exit: ExitOK, Session: "sess-gate", Report: "No verdict.\nTests: 12 passed, 0 failed\n" + ownerNone}, nil
			}
			return Result{}, nil
		}
		e := h.start()
		requireState(t, e, runrec.StatePaused)
		if err := e.RetryNote(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		rec := requireState(t, e, runrec.StatePaused)
		if rec.Pending.Role != RoleGate || rec.Pending.ShapePauses != 2 {
			t.Fatalf("pending %+v", rec.Pending)
		}
		if err := e.RetryWith(context.Background(), RetryOptions{AcceptShape: true}); !IsRefusal(err) || !strings.Contains(err.Error(), want) {
			t.Fatalf("err %v, want a refusal saying %q", err, want)
		}
		requireState(t, e, runrec.StatePaused)
	})
	t.Run("before the cap", func(t *testing.T) {
		h := newHarness(t)
		reviewerReports(h, "sess-reviewer", review(noOptions, "nothing", "nothing", "nothing"))
		e := h.start()
		requireState(t, e, runrec.StatePaused)
		if err := e.RetryWith(context.Background(), RetryOptions{AcceptShape: true}); !IsRefusal(err) {
			t.Fatalf("err %v, want a refusal at the first shape pause", err)
		}
	})
	t.Run("both flags", func(t *testing.T) {
		h := newHarness(t)
		e, _ := cappedReviewer(t, h, review(noOptions, "nothing", "nothing", "nothing"))
		if err := e.RetryWith(context.Background(), RetryOptions{AcceptShape: true, Again: true}); !IsRefusal(err) {
			t.Fatalf("err %v, want a refusal", err)
		}
	})
}

// shapeCount is the shape-pause count a role holds: in the pause when the role
// owns the single pending slot, else on its newest role record.
func shapeCount(rec runrec.Record, role string) int {
	if p := rec.Pending; p != nil && p.Kind == runrec.PendingShape && p.Role == role {
		return p.ShapePauses
	}
	for i := len(rec.Roles) - 1; i >= 0; i-- {
		if r := rec.Roles[i]; r.Role == role && r.Round == rec.Round {
			if r.ShapeRetry != nil {
				return r.ShapeRetry.ShapePauses
			}
			return 0
		}
	}
	return 0
}

func TestTwoRolesKeepTheirOwnShapePauseCount(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	bad := review(misnumbered, "nothing", "nothing", "nothing")
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleReviewer || req.Role == RoleUI {
			return Result{Exit: ExitOK, Session: "sess-" + req.Role, Report: bad}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	for _, role := range []string{RoleReviewer, RoleUI} {
		if got := shapeCount(rec, role); got != 1 {
			t.Fatalf("after the first pause the %s holds count %d, want 1 (pending %+v)", role, got, rec.Pending)
		}
	}
	if err := e.RetryNote(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	rec = requireState(t, e, runrec.StatePaused)
	for _, role := range []string{RoleReviewer, RoleUI} {
		if got := shapeCount(rec, role); got != runrec.ShapePauseCap {
			t.Errorf("after the second pause the %s holds count %d, want %d", role, got, runrec.ShapePauseCap)
		}
	}
	// Both roles are at the cap: a plain retry starts neither of them.
	before := len(h.runner.requests())
	if err := e.RetryNote(context.Background(), ""); !IsRefusal(err) {
		t.Fatalf("a plain retry with both roles at the cap: err %v, want a refusal", err)
	}
	if got := len(h.runner.requests()); got != before {
		t.Errorf("a refused retry started %d role runs", got-before)
	}
}

func TestACappedRoleTakesThePauseFromALowerCount(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	if err := e.run.Update(func(rec *runrec.Record) {
		rec.State = runrec.StateReview
		rec.Roles = []runrec.RoleRecord{
			{Role: RoleReviewer, Round: 1, StartedAt: at, EndedAt: at},
			{Role: RoleUI, Round: 1, StartedAt: at, EndedAt: at},
		}
	}); err != nil {
		t.Fatal(err)
	}
	pending := func(role string, n int) runrec.Pending {
		return runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateReview, Role: role, Round: 1, Reason: role, ShapePauses: n}
	}
	if err := e.pauseShape(pending(RoleReviewer, 1)); err != nil {
		t.Fatal(err)
	}
	if err := e.pauseShape(pending(RoleUI, 1)); err != nil { // equal: the first keeps the slot
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending.Role != RoleReviewer || shapeCount(rec, RoleUI) != 1 {
		t.Fatalf("pending %+v, ui count %d: the first pause keeps the slot and the other keeps its count", rec.Pending, shapeCount(rec, RoleUI))
	}
	if err := e.pauseShape(pending(RoleUI, 2)); err != nil {
		t.Fatal(err)
	}
	rec = e.Snapshot()
	if rec.Pending.Role != RoleUI || rec.Pending.ShapePauses != 2 || rec.Reason != RoleUI {
		t.Fatalf("pending %+v reason %q: the capped role takes the slot", rec.Pending, rec.Reason)
	}
	if got := shapeCount(rec, RoleReviewer); got != 1 {
		t.Errorf("the displaced reviewer keeps count %d on its record, want 1", got)
	}
}

func TestAcceptedGateReportClosesANewLedgerStepWithItsVerdict(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) { o.contract = 11 })
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate {
			return Result{Exit: ExitOK, Session: "sess-gate", Report: "VERDICT: PASS\n\n" + noOptions + "\nTests: 12 passed, 0 failed\n\n## Owner needed\n\nnone\n"}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	requireState(t, e, runrec.StatePaused)
	if err := e.RetryNote(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending.Role != RoleGate || rec.Pending.ShapePauses != 2 {
		t.Fatalf("pending %+v", rec.Pending)
	}
	if _, verdicts := gateLedger(h); len(verdicts) != 0 {
		t.Fatalf("a refused gate round closed with a verdict: %v", verdicts)
	}
	gateRuns := len(shapeRequests(h, RoleGate))
	if err := e.RetryWith(context.Background(), RetryOptions{AcceptShape: true}); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	if got := len(shapeRequests(h, RoleGate)); got != gateRuns {
		t.Fatalf("accepting ran the gate: %d requests, want %d", got, gateRuns)
	}
	steps, verdicts := gateLedger(h)
	if len(steps) != gateRuns+1 {
		t.Fatalf("%d gate steps, want one more than the %d attempts: the accepted round opens its own", len(steps), gateRuns)
	}
	last := steps[len(steps)-1]
	if len(verdicts) != 1 || verdicts[0].verdict != "pass" || verdicts[0].step != last.id {
		t.Fatalf("verdicts %v, want one pass on the accepted round's step %s", verdicts, last.id)
	}
	// The step carries the lineup row of the refused attempt.
	var started stepCall
	for _, s := range h.steps() {
		if s.Role == "gate" {
			started = s
		}
	}
	if started.Vendor != "claude" || started.Model != "opus" {
		t.Errorf("accepted gate step %+v, want the refused attempt's row", started)
	}
}

// retryQuestion is a well-formed Owner needed block.
const retryQuestion = "Question: Which format?\nOptions:\n1. json: machine readable\n2. text: human readable\nRecommend: 1 - tools read it\n"

func TestShapeProblemWithAQuestionRetriesOnce(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		res := defaultResult(req)
		if req.Role == RoleImplementer {
			res.Report = "Diff stat.\n\n## Owner needed\n\n" + retryQuestion
			if n > 0 {
				res.Report = "Diff stat.\n\nTests: 12 passed, 0 failed\n\n## Owner needed\n\n" + retryQuestion
			}
		}
		return res, nil
	}
	rec := requireState(t, h.start(), runrec.StatePaused)
	reqs := shapeRequests(h, RoleImplementer)
	if len(reqs) != 2 {
		t.Fatalf("implementer calls = %d, want 2", len(reqs))
	}
	if reqs[0].Round != reqs[1].Round || reqs[1].Resume != "sess-implementer" {
		t.Fatalf("retry changed round or session: %+v", reqs[1])
	}
	for _, want := range []string{"## Report shape", `no "Tests: N passed, M failed" line`, "Keep your owner question"} {
		if !strings.Contains(reqs[1].Brief, want) {
			t.Errorf("retry brief missing %q:\n%s", want, reqs[1].Brief)
		}
	}
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingQuestion {
		t.Fatalf("pending = %+v", rec.Pending)
	}
	if len(rec.Questions) != 1 || rec.Questions[0].Role != RoleImplementer {
		t.Fatalf("questions = %+v, want the one question once", rec.Questions)
	}
}

func TestShapeProblemWithAQuestionPausesAfterTheRetry(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		res := defaultResult(req)
		if req.Role == RoleImplementer {
			res.Report = "Diff stat.\n\n## Owner needed\n\n" + retryQuestion
		}
		return res, nil
	}
	rec := requireState(t, h.start(), runrec.StatePaused)
	if got := len(shapeRequests(h, RoleImplementer)); got != 2 {
		t.Fatalf("implementer calls = %d, want 2 before the pause", got)
	}
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape {
		t.Fatalf("pending = %+v", rec.Pending)
	}
	if rec.AutomaticShapeRetry() != nil {
		t.Fatal("automatic retry marker remained on the pause")
	}
	if len(rec.Questions) != 0 {
		t.Fatalf("a refused report's question was recorded: %+v", rec.Questions)
	}
	if !rec.Pending.Asked {
		t.Fatalf("the pause lost the note that the report asked: %+v", rec.Pending)
	}
}

func TestShapeRetryBriefSaysKeepTheQuestion(t *testing.T) {
	const reason = `report shape: no "Tests: N passed, M failed" line`
	plain := shapeRetryBrief(reason, false)
	want := "ork checked your last report and refused it: " + reason + "\n\nWrite the report again so that it has the shape the prompt asks for, and exit."
	if plain != want {
		t.Fatalf("brief without a question changed:\n got %q\nwant %q", plain, want)
	}
	asked := shapeRetryBrief(reason, true)
	if !strings.HasPrefix(asked, plain) || !strings.Contains(asked, "Keep your owner question") {
		t.Fatalf("brief with a question = %q", asked)
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
