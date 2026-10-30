// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

type relayAnswerLive struct {
	*ledgerLive
	answers   []daemon.RelayAnswerParams
	answerErr error
}

func (l *relayAnswerLive) RelayAnswer(_ context.Context, p daemon.RelayAnswerParams) (string, error) {
	l.answers = append(l.answers, p)
	return "answered in the ledger", l.answerErr
}
func relayAnswerHarness(t *testing.T, state string, blocker bool) (*harness, *relayAnswerLive) {
	t.Helper()
	h, base := ledgerEndHarness(t, state)
	l := &relayAnswerLive{ledgerLive: base}
	h.m.cfg.Live = l
	q := &slicercli.LedgerQuestion{ID: "q1", Title: "Storage", Key: "storage.engine", Blocker: blocker, Options: []runrec.Option{{Label: "Files"}, {Label: "Database"}}, Recommend: "Database"}
	for i := range h.m.projects[0].snap.Runs.Value {
		h.m.projects[0].snap.Runs.Value[i].Steps = []slicercli.Step{{Phase: "start", Question: q}}
	}
	for i := range h.m.projects[0].snap.BoardRuns.Value {
		h.m.projects[0].snap.BoardRuns.Value[i].Steps = []slicercli.Step{{Phase: "start", Question: q}}
	}
	h.m.projects[0].reg.Adj = true
	h.m.projects[0].snap.Open.Value = []adjcli.Record{{ID: 12, Key: "storage.engine", Status: "open"}}
	return h, l
}
func TestExternalQuestionNumberKeyConfirmsThenAnswers(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	h, l := relayAnswerHarness(t, "active", true)
	for _, cancel := range []string{"n", "enter", "esc"} {
		h.send(press("2"))
		if h.m.confirm == nil {
			t.Fatal("2 did not open confirmation")
		}
		for _, want := range []string{"#q1 (Storage)", "on x1", "\x22Database\x22 (recommended)", "and adj #12", "as Jon R? [y/N] (Enter is No)"} {
			if !strings.Contains(h.m.confirm.prompt(), want) {
				t.Fatalf("missing %q: %s", want, h.m.confirm.prompt())
			}
		}
		h.send(press(cancel))
		if len(l.answers) != 0 {
			t.Fatal("cancel wrote")
		}
	}
	h.send(press("2"))
	h.deliver(press("y"))
	want := daemon.RelayAnswerParams{Adj: 12, Project: "ork", Run: "x1", Question: "q1", Option: "Database", By: "Jon R", Via: "tui"}
	if len(l.answers) != 1 || l.answers[0] != want {
		t.Fatalf("%+v", l.answers)
	}
	if h.m.pane != nil || h.m.actionStatus != "answered in the ledger" {
		t.Fatalf("pane/status: %v %s", h.m.pane, h.m.actionStatus)
	}
}
func TestExternalNonBlockerQuestionIsAnswerable(t *testing.T) {
	h, _ := relayAnswerHarness(t, "active", false)
	if !strings.Contains(h.view(), "2. Database") || !strings.Contains(wideHint(h), "1-2 pick") || !strings.Contains(wideHint(h), "t free text") {
		t.Fatal(h.view())
	}
	if ledgerOwnerQuestion(h.m.paneBoardRun()) != nil {
		t.Fatal("non-blocker changed Needs you")
	}
}
func TestStaleOrLinkedRunQuestionStaysReadOnly(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	for _, state := range []string{"stale", "ended", "linked"} {
		h, l := relayAnswerHarness(t, "active", true)
		if state == "linked" {
			h.m.projects[0].snap.EngineRuns = []source.EngineRun{{ID: "engine1", RelayRun: "x1", State: "paused"}}
		} else {
			h.m.projects[0].snap.Runs.Value[0].State = state
		}
		for _, k := range []string{"1", "t"} {
			h.send(press(k))
			if h.m.confirm != nil || h.m.relayInput != nil || len(l.answers) > 0 {
				t.Fatal("read-only wrote")
			}
		}
		hint := wideHint(h)
		if strings.Contains(hint, "pick") || strings.Contains(hint, "free text") {
			t.Fatal(hint)
		}
		if state == "stale" && !strings.Contains(hint, "x end run") {
			t.Fatal(hint)
		}
	}
}
func TestExternalAnswerFreeTextPendingAndMissingBy(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	h, l := relayAnswerHarness(t, "active", true)
	h.send(press("t"))
	if h.m.relayInput == nil {
		t.Fatal("no text field")
	}
	if strings.Contains(wideHint(h), "enter review") {
		t.Fatal("empty review hint")
	}
	h.send(tea.PasteMsg{Content: "custom answer"})
	if !strings.Contains(wideHint(h), "enter review") {
		t.Fatal("missing review hint")
	}
	h.send(press("enter"))
	if h.m.confirm == nil || h.m.confirm.text != "custom answer" {
		t.Fatal("text not reviewed")
	}
	cmd := h.m.key(press("y"))
	if cmd == nil {
		t.Fatal("missing command")
	}
	h.send(press("t"))
	if h.m.relayInput != nil || !strings.Contains(h.m.actionStatus, "pending") {
		t.Fatal("no pending guard")
	}
	l.answerErr = &ipc.Error{Code: daemon.CodeRefused, Message: "question q1 is already answered"}
	h.deliver(cmd())
	if h.m.actionStatus != "question q1 is already answered" || h.m.pane == nil {
		t.Fatal(h.m.actionStatus)
	}
	t.Setenv("ADJ_DECIDED_BY", "")
	h, l = relayAnswerHarness(t, "active", true)
	h.send(press("1"))
	h.send(press("t"))
	if h.m.confirm != nil || h.m.relayInput != nil || len(l.answers) > 0 || !strings.Contains(h.m.actionStatus, "ADJ_DECIDED_BY") {
		t.Fatal("missing By guard")
	}
}
func TestGoldenLedgerAnswerConfirm(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	for _, mono := range []bool{false, true} {
		h, _ := relayAnswerHarness(t, "active", true)
		h.m.cfg.Theme = NewTheme(mono)
		h.send(press("2"))
		if h.m.confirm == nil {
			t.Fatal("missing confirmation")
		}
		name := "color"
		if mono {
			name = "mono"
		}
		checkGolden(t, "ledger-answer-confirm-100x30-"+name+".txt", h.m.View().Content)
	}
}

func TestExternalQuestionLatestAndPerQuestionFlights(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	h, _ := relayAnswerHarness(t, "active", false)
	run := &h.m.projects[0].snap.Runs.Value[0]
	run.Steps[0].Question.AskedAt = "2026-10-30T12:00:00Z"
	run.Steps = append(run.Steps, slicercli.Step{Question: &slicercli.LedgerQuestion{ID: "new", Title: "New", AskedAt: "2026-10-30T12:00:00.1Z"}})
	if got := h.m.externalQuestion(); got == nil || got.ID != "new" {
		t.Fatalf("latest: %+v", got)
	}
	run.Steps[1].Question.Answer = map[string]string{"text": "done"}
	if got := h.m.externalQuestion(); got == nil || got.ID != "q1" {
		t.Fatalf("answered latest: %+v", got)
	}
	a := runAction{verb: "relay-answer", project: "ork", run: "x1", question: "q1"}
	b := a
	b.question = "q2"
	c := a
	c.project = "other"
	if a.flightKey() == b.flightKey() || a.flightKey() == c.flightKey() {
		t.Fatal("flight keys collide")
	}
	h.send(press("2"))
	if got := h.m.confirm.promptWidth(40); !strings.Contains(got, "\n[y/N] (Enter is No)") {
		t.Fatal(got)
	}
}

func TestExternalAnswerPaneKeepsNavigationAndTextCancel(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	h, l := relayAnswerHarness(t, "active", false)
	for _, k := range []string{"p", "v", "pgdown", "pgup", "x"} {
		h.send(press(k))
		if h.m.confirm != nil || len(l.answers) > 0 {
			t.Fatalf("%s collided with answer keys", k)
		}
	}
	h.send(press("t"))
	h.send(press("enter"))
	if h.m.relayInput == nil || h.m.confirm != nil {
		t.Fatal("empty field reviewed")
	}
	h.send(press("esc"))
	if h.m.relayInput != nil || h.m.pane == nil {
		t.Fatal("field cancel closed pane")
	}
	h.send(press("esc"))
	if h.m.pane != nil {
		t.Fatal("pane did not close")
	}
}

func TestRelayAnswerOverviewDrawsTextAndPreservesLongConfirmation(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	for _, mode := range []string{"field", "confirm"} {
		t.Run(mode, func(t *testing.T) {
			h, _ := relayAnswerHarness(t, "active", true)
			h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
			q := h.m.externalQuestion()
			q.Title = strings.Repeat("Which storage should the relay use for these records? ", 5)
			if mode == "field" {
				q.Title = strings.Repeat("Which storage should the relay use for these records? ", 12)
				h.send(press("t"))
				h.send(tea.PasteMsg{Content: "Keep the files"})
				h.m.actionStatus = "Review this answer"
				view := plainView(h.m)
				for _, want := range []string{"Answer in the ledger:", "Keep the files", "Review this answer"} {
					if !strings.Contains(view, want) {
						t.Fatalf("missing %q:\n%s", want, view)
					}
				}
			} else {
				h.send(press("2"))
				view := plainView(h.m)
				if !strings.Contains(view, "[y/N] (Enter is No)") || !strings.Contains(view, "as Jon R?") {
					t.Fatal(view)
				}
				if !strings.Contains(wideHint(h), "y answer") || strings.Contains(wideHint(h), "y confirm") {
					t.Fatal(wideHint(h))
				}
			}
		})
	}
}

func TestRelayAnswerConfirmWithoutAdjSendsNoAdjID(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	for _, cached := range []bool{false, true} {
		h, l := relayAnswerHarness(t, "active", true)
		if !cached {
			h.m.projects[0].snap.Open.Value = nil
		}
		h.send(press("2"))
		if strings.Contains(h.m.confirm.prompt(), "and adj #12") != cached {
			t.Fatal(h.m.confirm.prompt())
		}
		h.deliver(press("y"))
		raw, err := json.Marshal(l.answers[0])
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"adj":12`) != cached || (!cached && strings.Contains(string(raw), `"adj":`)) {
			t.Fatalf("cached=%t: %s", cached, raw)
		}
	}
}

func TestRelayAnswerOverviewFieldFitsInlinePaneWidth(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	h, _ := relayAnswerHarness(t, "active", true)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(press("t"))
	h.send(tea.PasteMsg{Content: "Keep the files because the records are small and local to the repo"})
	lines := strings.Split(plainView(h.m), "\n")
	at := -1
	for i, l := range lines {
		if strings.Contains(l, "Answer in the ledger:") {
			at = i
		}
	}
	if at < 0 || at+1 >= len(lines) {
		t.Fatalf("no field:\n%s", strings.Join(lines, "\n"))
	}
	// The inline pane is narrower than the main area, so the tail of the typed
	// text must stay whole on the one field line instead of wrapping.
	if !strings.Contains(lines[at+1], "small and local to the repo") {
		t.Fatalf("field wrapped or lost its tail:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(lines[at+2], "repo") {
		t.Fatalf("field continues on a second line:\n%s", strings.Join(lines, "\n"))
	}
}
