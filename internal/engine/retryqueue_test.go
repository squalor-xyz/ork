// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// retryWithQueuedQuestion runs a review where the reviewer breaks the report
// shape and the UI pass asks the owner a question after that pause. The
// reviewer's shape pause is first; the question is queued behind it (S159).
func retryWithQueuedQuestion(t *testing.T) (*harness, *Engine, runrec.Record) {
	t.Helper()
	h := uiHarness(t, tuiFiles)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	runs := map[string]int{}
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role != RoleReviewer && req.Role != RoleUI {
			return Result{}, nil
		}
		mu.Lock()
		runs[req.Role]++
		n := runs[req.Role]
		mu.Unlock()
		if n > 1 {
			return Result{Exit: ExitOK, Session: "sess-" + req.Role, Report: cleanReview}, nil
		}
		if req.Role == RoleReviewer {
			return Result{Exit: ExitOK, Session: "sess-reviewer", Report: noSections}, nil
		}
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			if e.Snapshot().State == runrec.StatePaused {
				break
			}
		}
		return Result{Exit: ExitOK, Session: "sess-ui", Report: strings.Replace(cleanReview, "none", "Which library?", 1)}, nil
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape || rec.Pending.Role != RoleReviewer || len(rec.QueuedQuestions) != 1 {
		t.Fatalf("pending %+v queue %+v, want the reviewer's shape pause with the UI question queued", rec.Pending, rec.QueuedQuestions)
	}
	return h, e, rec
}

// A retry of a pause that is not a question asks the sibling's queued
// question first: no role runs until it is answered.
func TestRetryAsksTheQueuedQuestionFirst(t *testing.T) {
	h, e, _ := retryWithQueuedQuestion(t)
	before := len(h.runner.requests())
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingQuestion || rec.Pending.Role != RoleUI || rec.Pending.Question != "Which library?" {
		t.Fatalf("pending %+v, want the UI pass's question", rec.Pending)
	}
	if len(rec.QueuedQuestions) != 0 {
		t.Errorf("the question is still queued: %+v", rec.QueuedQuestions)
	}
	if got := len(h.runner.requests()); got != before {
		t.Errorf("%d role runs before the question was answered", got-before)
	}
	if rec.Pending.Retry == nil || rec.Pending.Retry.Kind != runrec.PendingShape {
		t.Errorf("the retry context was dropped: %+v", rec.Pending.Retry)
	}
}

// The retry that follows the answer still carries the first pause's context:
// the reviewer's brief has its report shape section and resumes its session,
// and the UI pass gets the owner's answer.
func TestRetryContextSurvivesTheQueuedQuestion(t *testing.T) {
	h, e, rec := retryWithQueuedQuestion(t)
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Reload before the answer to prove the retry context is durable.
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
	if !strings.Contains(reviewer.Brief, "ork checked your last report and refused it") {
		t.Errorf("the reviewer lost its report shape section:\n%s", reviewer.Brief)
	}
	if reviewer.Resume != "sess-reviewer" {
		t.Errorf("the reviewer resumed %q, want sess-reviewer", reviewer.Resume)
	}
	if !strings.Contains(ui.Brief, "stdlib") || !strings.Contains(ui.Brief, "Which library?") {
		t.Errorf("the UI pass lost its answer:\n%s", ui.Brief)
	}
	if ui.Resume != "sess-ui" {
		t.Errorf("the UI pass resumed %q, want sess-ui", ui.Resume)
	}
}

// With more than one question queued, the retry context waits through all of
// them: each answer shows the next question and the context stays.
func TestRetryContextWaitsThroughEveryQueuedQuestion(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	shape := runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateReview, Role: RoleReviewer, Round: 1, Reason: "shape"}
	q := func(text string) runrec.Pending {
		return runrec.Pending{Kind: runrec.PendingQuestion, State: runrec.StateReview, Role: RoleUI, Round: 1, Reason: "ui round 1 needs the owner", Question: text}
	}
	first := q("first?")
	first.Retry = &shape
	run, err := h.store.Create(runrec.Record{
		ID: "ork-queue", Project: "p", Item: item, State: runrec.StatePaused, Round: 1,
		Pending: &first, QueuedQuestions: []runrec.Pending{q("second?")},
		Questions: []runrec.Question{{Role: RoleUI, Round: 1, Text: "first?"}, {Role: RoleUI, Round: 1, Text: "second?"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := Load(h.deps, h.proj, run)
	if err := e.Answer(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	rec := e.Snapshot()
	if rec.State != runrec.StatePaused || rec.Pending == nil || rec.Pending.Question != "second?" {
		t.Fatalf("state %s pending %+v, want the second question", rec.State, rec.Pending)
	}
	if rec.Pending.Retry == nil || rec.Pending.Retry.Kind != runrec.PendingShape || rec.Pending.Retry.Role != RoleReviewer {
		t.Errorf("the retry context was dropped by the second question: %+v", rec.Pending.Retry)
	}
}

// A role that the retried pause names runs again, though its report stands,
// also when a sibling's question was asked before the retry.
func TestReviewDoneHonoursTheRetriedPause(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	run, err := h.store.Create(runrec.Record{
		ID: "ork-done", Project: "p", Item: item, State: runrec.StateReview, Round: 1,
		Roles: []runrec.RoleRecord{{Role: RoleReviewer, Round: 1, Exit: string(ExitOK), Report: "reviewer-1.md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run.Dir(), "reviewer-1.md"), []byte(cleanReview), 0o600); err != nil {
		t.Fatal(err)
	}
	e := Load(h.deps, h.proj, run)
	rec := e.Snapshot()
	if !e.reviewDone(rec, nil, RoleReviewer) {
		t.Fatal("a good report must stand without a pause")
	}
	retried := runrec.Pending{Kind: runrec.PendingError, State: runrec.StateReview, Role: RoleReviewer, Round: 1, Reason: "error"}
	question := runrec.Pending{Kind: runrec.PendingQuestion, State: runrec.StateReview, Role: RoleUI, Round: 1, Question: "q?", Answer: "a", Retry: &retried}
	if e.reviewDone(rec, &question, RoleReviewer) {
		t.Error("the reviewer is named by the retried pause and must run again")
	}
	question.Retry = nil
	if !e.reviewDone(rec, &question, RoleReviewer) {
		t.Error("a question of the UI pass must not rerun the reviewer")
	}
}
