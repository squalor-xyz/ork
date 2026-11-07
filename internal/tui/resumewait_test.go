// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/source"
)

const waitText = "resume waits for a slot under project cap 1; next: ork stop engine"

// waitingHarness has one question run whose answer waits for a slot (S274,
// adj 177): paused, with the resume stored, so it is not the owner's turn.
func waitingHarness(t *testing.T, pending string) *harness {
	t.Helper()
	h := actionHarness(t, "paused", pending)
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
	s.EngineRuns[0].ResumeWaiting = true
	s.EngineRuns[0].Wait = waitText
	h.src.set(s)
	h.deliver(snapshotMsg{name: "ork", snap: s})
	return h
}

func TestAResumeWaitingRunLeavesNeedsYouAndShowsUnderRunning(t *testing.T) {
	h := waitingHarness(t, "question")
	v := h.view()
	if !strings.Contains(v, "Needs you 0") || !strings.Contains(v, "nothing needs you") || strings.Contains(v, "? question") {
		t.Errorf("the run still needs the owner:\n%s", v)
	}
	if !strings.Contains(v, "Running 1") || !strings.Contains(v, "resume waits for a slot under project cap 1") {
		t.Errorf("the run is not shown as waiting for a slot under Running:\n%s", v)
	}
}

// The Running row has a second line of its own for the wait, besides the
// detail line of the selected row. Line 2 drops "; next: ork stop RUN" as the
// pane does, so the cap stays visible when the line is cut (S274).
func TestAResumeWaitingRunRowSaysItWaitsForASlot(t *testing.T) {
	h := waitingHarness(t, "question")
	h.deliver(press("1")) // the project's own Overview: the row has room for its line 2
	v := h.view()
	if n := strings.Count(v, "resume waits for a slot under project cap 1"); n != 2 {
		t.Errorf("the wait text shows %d time(s), want the row's second line and the detail line:\n%s", n, v)
	}
	if n := strings.Count(v, "next: ork stop engine"); n != 1 {
		t.Errorf("the stop hint shows %d time(s), want the detail line only:\n%s", n, v)
	}
}

// Auto-switch does not move the pane to a run that only waits for a slot.
func TestNewestRunningSkipsAResumeWaitingRun(t *testing.T) {
	waiting := source.EngineRun{ID: "w", Item: "S1", State: "paused", Pending: "question", ResumeWaiting: true, CreatedAt: t0}
	if _, _, ok := newestRunning(engineSnap(proj("ork", false), waiting)); ok {
		t.Error("a run that waits for a slot counts as running")
	}
}

func TestAResumeWaitingRunIsNotInTheQuestionsView(t *testing.T) {
	h := waitingHarness(t, "question")
	h.m.view = questionsView
	v := h.view()
	if strings.Contains(v, "engine  S22") || strings.Contains(v, waitText) || !strings.Contains(v, "No open questions") {
		t.Errorf("the Questions view lists a run whose answer waits:\n%s", v)
	}
}

func TestAResumeWaitingPaneHasNoAnswerFieldAndNoRetryHint(t *testing.T) {
	h := waitingHarness(t, "question")
	h.deliver(press("enter"))
	if h.m.pane == nil {
		t.Fatal("the pane did not open")
	}
	v := h.view()
	if strings.Contains(v, "Answer:") || h.m.paneQuestion() {
		t.Errorf("the pane offers an answer field:\n%s", v)
	}
	if strings.Contains(v, "t retry") {
		t.Errorf("the pane offers retry:\n%s", v)
	}
	if !strings.Contains(v, "resume waits for a slot") {
		t.Errorf("the pane does not say the resume waits:\n%s", v)
	}
	// Retry is refused with the reason, and asks nothing: the view already holds
	// the wait text, so only the status line can tell the refusal from it.
	h.deliver(press("t"))
	if want := "resume already waits for a slot; ork stop engine cancels it"; h.m.actionStatus != want || h.m.confirm != nil {
		t.Errorf("retry status %q (confirm %v), want %q", h.m.actionStatus, h.m.confirm, want)
	}
}

func TestAResumeWaitingRetryHintIsGoneOnTheOverview(t *testing.T) {
	// The control: a paused run whose resume does not wait offers t retry.
	control := actionHarness(t, "paused", "error")
	control.deliver(press("1"))
	if !strings.Contains(control.view(), "t retry") {
		t.Fatalf("a plain paused run does not offer retry:\n%s", control.view())
	}
	h := waitingHarness(t, "error")
	h.deliver(press("1"))
	if v := h.view(); strings.Contains(v, "t retry") {
		t.Errorf("the Overview offers retry for a run whose resume waits:\n%s", v)
	}
	h.deliver(press("t"))
	if want := "resume already waits for a slot; ork stop engine cancels it"; h.m.actionStatus != want || h.m.confirm != nil {
		t.Errorf("retry status %q (confirm %v), want %q", h.m.actionStatus, h.m.confirm, want)
	}
}

// The answer notice stays on the status line while the resume waits, and shows
// the daemon's text (S274).
func TestAnswerStatusSurvivesAWaitingResume(t *testing.T) {
	h := actionHarness(t, "paused", "question")
	live := &actionLive{src: h.src, waitOnAnswer: true, answerNotice: daemon.ResumeWaitNote}
	h.m.cfg.Live = live
	h.m.view = 1
	h.deliver(press("enter"))
	typeAnswer(h, "use local storage")
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(h.m.actionStatus, "answered") || !strings.Contains(h.m.actionStatus, daemon.ResumeWaitNote) {
		t.Fatalf("the answer notice was wiped or never shown: %q\n%s", h.m.actionStatus, v)
	}
}

// Once the run has started, the status no longer says its resume waits.
func TestAnswerStatusDropsTheWaitNoteWhenTheRunStarts(t *testing.T) {
	h := actionHarness(t, "paused", "question")
	h.m.cfg.Live = &actionLive{src: h.src, waitOnAnswer: true, answerNotice: daemon.ResumeWaitNote}
	h.m.view = 1
	h.deliver(press("enter"))
	typeAnswer(h, "use local storage")
	h.deliver(press("enter"))
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
	s.EngineRuns[0].State, s.EngineRuns[0].ResumeWaiting, s.EngineRuns[0].Wait = "implement", false, ""
	h.src.set(s)
	h.deliver(snapshotMsg{name: "ork", snap: s})
	if got := h.m.actionStatus; !strings.HasPrefix(got, "answered") || strings.Contains(got, "waits for a slot") {
		t.Fatalf("status after the start: %q", got)
	}
}

func TestRetryStatusShowsTheWaitNote(t *testing.T) {
	h := actionHarness(t, "paused", "error")
	h.m.cfg.Live = waitingRetryLive{&actionLive{src: h.src}, daemon.ResumeWaitNote}
	h.deliver(press("t"))
	h.deliver(press("y"))
	want := "ork retry engine: " + daemon.ResumeWaitNote
	if v := h.view(); h.m.actionStatus != want {
		t.Fatalf("retry status %q, want %q:\n%s", h.m.actionStatus, want, v)
	}
}

func TestSummaryGoldenShowsAResumeWaitingRun(t *testing.T) {
	m := newGoldenModel(NewTheme(true))
	now := time.Date(2026, 10, 19, 16, 42, 7, 0, time.UTC)
	waiting := source.EngineRun{ID: "run-2", Item: "S42", State: "paused", Pending: "question", ResumeWaiting: true,
		Wait: "resume waits for a slot under project cap 1; next: ork stop run-2", CreatedAt: now.Add(-20 * time.Minute)}
	running := source.EngineRun{ID: "run-1", Item: "S110", State: "review", CreatedAt: now.Add(-12 * time.Minute), Activity: testActivity,
		Roles: []source.EngineRole{{Role: "reviewer", Round: 1, Vendor: "claude", StartedAt: now.Add(-4 * time.Minute)}}}
	m.Update(snapshotMsg{name: four[0].Name, snap: engineSnap(four[0], running, waiting)})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(press("["))
	m.Update(press("down")) // the waiting run: its detail line names the cap
	checkGolden(t, "resume-waiting-100x30-mono.txt", m.View().Content)
}

// The Overview at 80x24: the waiting run's line 2 keeps the cap and drops the
// stop hint, which the detail line below keeps (S274).
func TestOverviewGoldenCutsAResumeWaitingLine(t *testing.T) {
	m := newGoldenModel(NewTheme(true))
	waiting := source.EngineRun{ID: "run-2", Item: "S42", State: "paused", Pending: "question", ResumeWaiting: true,
		Wait: "resume waits for a slot under project cap 1; next: ork stop run-2", CreatedAt: t0.Add(-20 * time.Minute)}
	m.Update(snapshotMsg{name: four[0].Name, snap: engineSnap(four[0], waiting)})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(press("1"))
	checkGolden(t, "resume-waiting-overview-80x24-mono.txt", m.View().Content)
}
