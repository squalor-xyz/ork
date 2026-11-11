// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const keyedOwnerFinding = ownerFinding + "\n  Decision key: cache.scope"

// ledgerHarness is a v11 harness; routes answer before the defaults.
func ledgerHarness(t *testing.T, routes ...clitest.Route) *harness {
	t.Helper()
	return newHarness(t, func(o *harnessOpts) {
		o.contract = 11
		o.routes = append(o.routes, routes...)
	})
}

// getRoute answers `relay-run answer run-1 q1 --get`. Several replies answer the
// calls in turn.
func getRoute(replies ...clitest.Reply) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"relay-run", "answer", "run-1", "q1", "--get"}, Prefix: true, Seq: replies}
}

const (
	ledgerOpen     = `{"run":"run-1","step":"1","question":{"id":"q1","title":"T","answer":null}}`
	ledgerAnswered = `{"run":"run-1","step":"1","question":{"id":"q1","title":"T","answered_at":"2026-11-11T01:02:03Z","answer":{"option":"keep","by":"jon"}}}`
)

func reply(stdout string) clitest.Reply { return clitest.Reply{Stdout: stdout} }

func (h *harness) askCalls() [][]string { return h.calls("slicer", "relay-run", "ask") }

func (h *harness) getCalls() [][]string {
	var out [][]string
	for _, c := range h.calls("slicer", "relay-run", "answer") {
		if slices.Contains(c, "--get") {
			out = append(out, c)
		}
	}
	return out
}

func (h *harness) answerWrites() [][]string {
	var out [][]string
	for _, c := range h.calls("slicer", "relay-run", "answer") {
		if !slices.Contains(c, "--get") {
			out = append(out, c)
		}
	}
	return out
}

func TestNonBlockingOwnerFindingIsAskedInTheLedgerWithoutBlocker(t *testing.T) {
	for _, tc := range []struct {
		name, finding, key string
	}{{"no key", ownerFinding, ""}, {"key", keyedOwnerFinding, "cache.scope"}} {
		t.Run(tc.name, func(t *testing.T) {
			h := ledgerHarness(t)
			reviewerSays(h, review(tc.finding, "nothing", "nothing", "nothing"))
			e := h.start()
			rec := requireState(t, e, runrec.StateAwaitingApproval)

			asks := h.askCalls()
			if len(asks) != 1 {
				t.Fatalf("relay-run ask calls: %q", asks)
			}
			want := []string{"relay-run", "ask", "run-1", "--step", rec.OrchestratorStep, "--title", "Should the cache be shared across projects?",
				"--option", "shared:one cache for all", "--option", "per-project:one cache each", "--recommend", "per-project"}
			if tc.key != "" {
				want = append(want, "--key", tc.key)
			}
			want = append(want, "--reasoning", "isolation is simpler", "--json", "--root", h.W)
			if !reflect.DeepEqual(asks[0], want) {
				t.Errorf("ask\n got %q\nwant %q", asks[0], want)
			}
			if slices.Contains(asks[0], "--blocker") {
				t.Error("a non-blocking question is asked with --blocker")
			}
			if len(rec.Findings) != 1 || rec.Findings[0].LedgerQuestion != "q1" {
				t.Errorf("findings %+v", rec.Findings)
			}
			if ledgerHas(h, "--step "+rec.OrchestratorStep+" ", "--phase wait", "--waiting-on owner") {
				t.Error("the finding made the orchestrator step wait on the owner")
			}
		})
	}
}

func TestSameOwnerFindingIsAskedOnce(t *testing.T) {
	h := ledgerHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		switch {
		case req.Role == RoleReviewer:
			return Result{Exit: ExitOK, Session: "sess-reviewer", Report: review(ownerFinding, "nothing", "nothing", "nothing")}, nil
		case req.Role == RoleGate && req.Round == 1:
			return Result{Exit: ExitOK, Report: "VERDICT: FAIL\n\n- engine.go:10 missing nil check\n" + ownerFinding + "\nTests: 1 passed, 0 failed\n\n## Owner needed\n\nnone\n"}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if !slices.Contains(h.runner.roles(), "gate-1") || len(h.runner.requests()) < 5 {
		t.Fatalf("the finding was not raised again: %v", h.runner.roles())
	}
	if n := len(h.askCalls()); n != 1 {
		t.Errorf("relay-run ask ran %d times for one finding", n)
	}
	if len(rec.Findings) != 1 || rec.Findings[0].LedgerQuestion != "q1" {
		t.Errorf("findings %+v", rec.Findings)
	}
}

func TestOwnerFindingAskIsAdvisory(t *testing.T) {
	h := ledgerHarness(t, clitest.Route{Tool: "slicer", Args: []string{"relay-run", "ask"}, Prefix: true, Exit: 2, Stderr: "slicer: ledger is locked"})
	reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if len(h.askCalls()) != 1 {
		t.Fatalf("the ask was not tried: %q", h.askCalls())
	}
	if len(rec.Findings) != 1 || rec.Findings[0].LedgerQuestion != "" {
		t.Errorf("findings %+v", rec.Findings)
	}
}

// An answer the owner gave in ork while the ask was in flight finds the finding
// without an id; the ledger question must still get that answer.
func TestAnswerGivenDuringTheAskReachesTheLedger(t *testing.T) {
	for _, committed := range []bool{false, true} {
		h := ledgerHarness(t)
		e := h.start()
		requireState(t, e, runrec.StateAwaitingApproval)
		tri, err := Triage(review(ownerFinding, "nothing", "nothing", "nothing"))
		if err != nil || len(tri.Owner) != 1 {
			t.Fatalf("%+v, %v", tri, err)
		}
		f := runrec.Finding{Kind: runrec.FindingOwner, Title: tri.Owner[0].Title, Text: tri.Owner[0].Text, Ask: tri.Owner[0].Ask, Answer: "keep"}
		e.run.Update(func(r *runrec.Record) {
			r.Findings = append(r.Findings, f)
			if committed {
				r.Commit = &runrec.Commit{SHA: "0123abc", Title: gitTitle}
			}
		})
		if committed {
			// askFinding itself refuses after the commit; the write-through must too.
			e.askFinding(context.Background(), f)
			if len(h.askCalls()) != 0 || len(h.answerWrites()) != 0 {
				t.Errorf("ledger calls after the commit: %q %q", h.askCalls(), h.answerWrites())
			}
			continue
		}
		e.askFinding(context.Background(), f)
		if len(h.askCalls()) != 1 {
			t.Fatalf("ask calls %q", h.askCalls())
		}
		want := []string{"relay-run", "answer", "run-1", "q1", "--text=keep", "--json", "--root", h.W}
		if got := h.answerWrites(); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			t.Errorf("answer writes\n got %q\nwant %q", got, want)
		}
		if got := e.Snapshot().Findings[0]; got.LedgerQuestion != "q1" || got.Answer != "keep" {
			t.Errorf("finding %+v", got)
		}
	}
}

func TestPausedQuestionStillAsksWithBlocker(t *testing.T) {
	h := ledgerHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if n == 0 {
			return Result{Exit: ExitOK, Report: strings.Replace(implReport, "## Owner needed\n\nnone\n", "## Owner needed\n\nQuestion: Which cap?\nOptions:\n1. four: cap at 4\n2. eight: cap at 8\nRecommend: 1 - smaller\n", 1)}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	requireState(t, e, runrec.StatePaused)
	asks := h.askCalls()
	if len(asks) != 1 {
		t.Fatalf("ask calls %q", asks)
	}
	i := slices.Index(asks[0], "--title")
	if i < 0 || i+2 >= len(asks[0]) || asks[0][i+2] != "--blocker" {
		t.Errorf("a paused question is asked with --blocker right after --title: %q", asks[0])
	}
}

func TestNoLedgerAskAfterTheCommit(t *testing.T) {
	for _, committed := range []bool{false, true} {
		h := ledgerHarness(t)
		e := h.start()
		requireState(t, e, runrec.StateAwaitingApproval)
		if committed {
			e.run.Update(func(r *runrec.Record) { r.Commit = &runrec.Commit{SHA: "0123abc", Title: gitTitle} })
		}
		tri, err := Triage(review(ownerFinding, "nothing", "nothing", "nothing"))
		if err != nil {
			t.Fatal(err)
		}
		if err := e.recordFindings(round{role: RoleReviewer, n: 2}, tri); err != nil {
			t.Fatal(err)
		}
		rec := e.Snapshot()
		if len(rec.Findings) != 1 {
			t.Fatalf("committed %v: the finding is kept either way: %+v", committed, rec.Findings)
		}
		if got := len(h.askCalls()); (got == 1) == committed || got > 1 {
			t.Errorf("committed %v: %d ask calls", committed, got)
		}
		if committed && rec.Findings[0].LedgerQuestion != "" {
			t.Errorf("a ledger id after the commit: %+v", rec.Findings[0])
		}
	}
}

func TestAnswerFindingWritesTheLedgerAnswer(t *testing.T) {
	t.Run("id", func(t *testing.T) {
		h := ledgerHarness(t)
		reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
		e := h.start()
		requireState(t, e, runrec.StateAwaitingApproval)
		if _, err := e.AnswerFinding(context.Background(), 1, "keep", "jon"); err != nil {
			t.Fatal(err)
		}
		want := []string{"relay-run", "answer", "run-1", "q1", "--text=keep", "--by=jon", "--json", "--root", h.W}
		if got := h.answerWrites(); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			t.Errorf("answer writes\n got %q\nwant %q", got, want)
		}
		if f := e.Snapshot().Findings[0]; f.Answer != "keep" || f.AnsweredAt.IsZero() {
			t.Errorf("finding %+v", f)
		}
	})
	t.Run("no id", func(t *testing.T) {
		h := ledgerHarness(t, clitest.Route{Tool: "slicer", Args: []string{"relay-run", "ask"}, Prefix: true, Exit: 2, Stderr: "no"})
		reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
		e := h.start()
		requireState(t, e, runrec.StateAwaitingApproval)
		if _, err := e.AnswerFinding(context.Background(), 1, "keep", "jon"); err != nil {
			t.Fatal(err)
		}
		if got := h.answerWrites(); len(got) != 0 {
			t.Errorf("answer writes without a ledger question: %q", got)
		}
	})
	t.Run("after the commit", func(t *testing.T) {
		h := ledgerHarness(t)
		reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
		e := h.start()
		requireState(t, e, runrec.StateAwaitingApproval)
		e.run.Update(func(r *runrec.Record) { r.Commit = &runrec.Commit{SHA: "0123abc", Title: gitTitle} })
		if _, err := e.AnswerFinding(context.Background(), 1, "keep", "jon"); err != nil {
			t.Fatal(err)
		}
		if got := h.answerWrites(); len(got) != 0 {
			t.Errorf("answer writes after the commit: %q", got)
		}
	})
	t.Run("failed write", func(t *testing.T) {
		h := ledgerHarness(t, clitest.Route{Tool: "slicer", Args: []string{"relay-run", "answer", "run-1", "q1", "--text=keep"}, Prefix: true, Exit: 2, Stderr: "no"})
		reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
		e := h.start()
		requireState(t, e, runrec.StateAwaitingApproval)
		if _, err := e.AnswerFinding(context.Background(), 1, "keep", "jon"); err != nil {
			t.Fatalf("a failed ledger write is advisory: %v", err)
		}
		if f := e.Snapshot().Findings[0]; f.Answer != "keep" {
			t.Errorf("finding %+v", f)
		}
	})
}

func TestLedgerAnswerIsReadBeforeTheCard(t *testing.T) {
	h := ledgerHarness(t, getRoute(reply(ledgerAnswered)))
	reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	f := rec.Findings[0]
	if f.Answer != "keep" || !f.AnsweredAt.Equal(time.Date(2026, 11, 11, 1, 2, 3, 0, time.UTC)) {
		t.Errorf("finding %+v", f)
	}
	want := []string{"relay-run", "answer", "run-1", "q1", "--get", "--json", "--root", h.W}
	if got := h.getCalls(); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("reads\n got %q\nwant %q", got, want)
	}
	if got := h.answerWrites(); len(got) != 0 {
		t.Errorf("a read answer is not written back: %q", got)
	}
}

func TestLedgerTextAnswerIsReadWhenNoOptionWasPicked(t *testing.T) {
	h := ledgerHarness(t, getRoute(reply(`{"question":{"id":"q1","answer":{"text":"keep both"}}}`)))
	reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if f := rec.Findings[0]; f.Answer != "keep both" || f.AnsweredAt.IsZero() {
		t.Errorf("finding %+v", f)
	}
}

func TestLedgerAnswerIsReadAgainAtTheFinishStep(t *testing.T) {
	h := ledgerHarness(t, getRoute(reply(ledgerOpen), reply(ledgerAnswered)))
	reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
	var e *Engine
	var atFinish string
	base := h.runner.script
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && strings.Contains(req.Brief, "## Finish step") {
			atFinish = e.run.Snapshot().Findings[0].Answer
		}
		return base(req, n)
	}
	e = h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if rec.Findings[0].Answer != "" {
		t.Fatalf("answered before the owner gave an answer: %+v", rec.Findings[0])
	}
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if atFinish != "keep" {
		t.Errorf("the finding held %q when the finish role ran", atFinish)
	}
	if f := e.Snapshot().Findings[0]; f.Answer != "keep" || f.AnsweredAt.IsZero() {
		t.Errorf("finding %+v", f)
	}
	if n := len(h.getCalls()); n != 2 {
		t.Errorf("%d reads, want one at the card and one at the finish step", n)
	}
}

func TestOpenLedgerQuestionStaysOpen(t *testing.T) {
	h := ledgerHarness(t, getRoute(reply(ledgerOpen)))
	reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if len(h.getCalls()) != 1 {
		t.Fatalf("reads: %q", h.getCalls())
	}
	if f := rec.Findings[0]; f.Answer != "" || !f.AnsweredAt.IsZero() || rec.Pending != nil || rec.Reason != "" {
		t.Errorf("finding %+v, pending %+v, reason %q", f, rec.Pending, rec.Reason)
	}
}

func TestLedgerReadFailureIsAdvisory(t *testing.T) {
	h := ledgerHarness(t, getRoute(clitest.Reply{Stderr: "slicer: no such question", Exit: 2}))
	reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if len(h.getCalls()) != 1 {
		t.Fatalf("reads: %q", h.getCalls())
	}
	if f := rec.Findings[0]; f.Answer != "" || rec.Reason != "" || rec.Pending != nil {
		t.Errorf("finding %+v, reason %q, pending %+v", f, rec.Reason, rec.Pending)
	}
	if err := e.Approve(context.Background()); err != nil {
		t.Fatalf("a failed read stops the finish step: %v", err)
	}
	var finish bool
	for _, req := range h.runner.requests() {
		finish = finish || strings.Contains(req.Brief, "## Finish step")
	}
	if !finish {
		t.Error("the finish role did not run")
	}
}

func TestNoLedgerReadAfterTheCommit(t *testing.T) {
	h := ledgerHarness(t, getRoute(reply(ledgerAnswered)))
	reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	e.run.Update(func(r *runrec.Record) {
		r.Commit = &runrec.Commit{SHA: "0123abc", Title: gitTitle}
		r.Findings[0].Answer, r.Findings[0].AnsweredAt = "", time.Time{}
	})
	before := len(h.getCalls())
	e.syncLedgerAnswers(context.Background())
	if n := len(h.getCalls()); n != before {
		t.Errorf("%d reads after the commit", n-before)
	}
	if f := e.Snapshot().Findings[0]; f.Answer != "" {
		t.Errorf("finding %+v", f)
	}
}

func TestLedgerAnswerNeverOverwritesTheOwnersAnswer(t *testing.T) {
	h := ledgerHarness(t, getRoute(reply(ledgerOpen), reply(ledgerAnswered)))
	reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	if _, err := e.AnswerFinding(context.Background(), 1, "shared", "jon"); err != nil {
		t.Fatal(err)
	}
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f := e.Snapshot().Findings[0]; f.Answer != "shared" {
		t.Errorf("finding %+v", f)
	}
	if n := len(h.getCalls()); n != 1 {
		t.Errorf("%d reads: an answered finding is not read again", n)
	}
}

func TestAnswerFromTheLedgerIsNotFiledInAdj(t *testing.T) {
	adjRoutes := []clitest.Route{
		{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: `{}`},
		{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "42\n"},
		{Tool: "adj", Args: []string{"decide"}, Prefix: true},
	}
	build := func(t *testing.T, ledger string) (*harness, *Engine) {
		h := newHarness(t, func(o *harnessOpts) {
			o.contract = 11
			o.project = func(p *registry.Project) { p.Adj = true }
			o.routes = append(o.routes, getRoute(reply(ledger)))
			o.routes = append(o.routes, adjRoutes...)
		})
		h.deps.Adj = adjcli.Client{Bin: h.fake.Bin("adj")}
		reviewerSays(h, review(keyedOwnerFinding, "nothing", "nothing", "nothing"))
		e := h.start()
		requireState(t, e, runrec.StateAwaitingApproval)
		return h, e
	}

	h, e := build(t, ledgerAnswered)
	if f := e.Snapshot().Findings[0]; f.Key != "cache.scope" || f.Answer != "keep" {
		t.Fatalf("finding %+v", f)
	}
	if calls := h.fake.CallsTo("adj"); len(calls) != 0 {
		t.Errorf("a ledger answer was filed in adj: %q", calls)
	}

	// The owner's own answer in ork still goes to adj.
	h, e = build(t, ledgerOpen)
	if _, err := e.AnswerFinding(context.Background(), 1, "shared", "jon"); err != nil {
		t.Fatal(err)
	}
	var verbs []string
	for _, c := range h.fake.CallsTo("adj") {
		verbs = append(verbs, c.Args[0])
	}
	if !reflect.DeepEqual(verbs, []string{"check", "ask", "decide"}) {
		t.Errorf("adj calls %v", verbs)
	}
}
