// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
)

type decisionLive struct {
	fakeLive
	answers []daemon.AdjAnswerParams
	err     error
}

func (l *decisionLive) AdjAnswer(_ context.Context, p daemon.AdjAnswerParams) (string, error) {
	l.answers = append(l.answers, p)
	return "decided in adj as #12", l.err
}
func decisionAnswerHarness(t *testing.T) (*harness, *decisionLive) {
	t.Helper()
	h := decisionsHarness(t)
	l := &decisionLive{}
	h.m.cfg.Live = l
	h.m.projects[0].snap.Open.Value[0] = adjcli.Record{ID: 12, Status: "open", Title: "Storage", Options: []adjcli.Option{{Label: "Files"}, {Label: "Database"}}, Recommendation: &adjcli.Recommendation{Label: "Database"}}
	h.send(press("enter"))
	return h, l
}
func TestDecisionPaneNumberKeyConfirmsThenDecides(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	for _, view := range []int{decisionsView, questionsView} {
		h, l := decisionAnswerHarness(t)
		h.m.view = view
		for _, cancel := range []string{"n", "enter", "esc"} {
			h.send(press("2"))
			if h.m.confirm == nil {
				t.Fatal("2 did not open confirmation")
			}
			for _, want := range []string{"#12 (Storage)", `"Database" (recommended)`, "Jon R", "[y/N] (Enter is No)"} {
				if !strings.Contains(h.m.confirm.prompt(), want) {
					t.Fatalf("confirmation missing %q: %s", want, h.m.confirm.prompt())
				}
			}
			if len(l.answers) != 0 || h.m.active != 0 {
				t.Fatal("picked option wrote or switched project")
			}
			h.deliver(press(cancel))
			if h.m.confirm != nil || len(l.answers) != 0 {
				t.Fatal("cancel wrote")
			}
		}
		h.send(press("2"))
		h.deliver(press("y"))
		if len(l.answers) != 1 || l.answers[0] != (daemon.AdjAnswerParams{Project: "zeta", ID: 12, Option: "Database", By: "Jon R", Via: "tui"}) {
			t.Fatalf("answers: %+v", l.answers)
		}
		if h.m.decisionPane != nil || h.m.actionStatus != "decided #12 in adj" || len(h.src.take()) != 1 || len(h.m.actionFlights) != 0 {
			t.Fatalf("result: pane=%v status=%s flights=%v", h.m.decisionPane, h.m.actionStatus, h.m.actionFlights)
		}
	}
}
func TestDecisionPaneFreeTextAndMissingDecidedBy(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	h, l := decisionAnswerHarness(t)
	h.send(press("t"))
	for _, k := range []string{"+", "-", "q"} {
		h.deliver(press(k))
	}
	h.send(press("enter"))
	if h.m.confirm == nil || !strings.Contains(h.m.confirm.prompt(), `"+-q"`) {
		t.Fatal("free text did not lead to confirmation")
	}
	h.deliver(press("y"))
	if len(l.answers) != 1 || l.answers[0].Text != "+-q" || l.answers[0].Option != "" {
		t.Fatalf("answers: %+v", l.answers)
	}
	t.Setenv("ADJ_DECIDED_BY", "")
	for _, k := range []string{"1", "2", "3", "4", "t"} {
		h, l = decisionAnswerHarness(t)
		h.deliver(press(k))
		if h.m.actionStatus != "ADJ_DECIDED_BY is unset; set it before answering in adj" || h.m.confirm != nil || len(l.answers) != 0 {
			t.Fatalf("%s: status=%q", k, h.m.actionStatus)
		}
	}
}
func TestDecisionPaneHintsFollowTheRecord(t *testing.T) {
	for _, state := range []string{"open", "decided", "no-adj", "nil", "unsupported"} {
		t.Run(state, func(t *testing.T) {
			h, _ := decisionAnswerHarness(t)
			switch state {
			case "decided":
				h.m.decisionPane.record.Status = "decided"
			case "no-adj":
				h.m.projects[0].reg.Adj = false
			case "nil":
				h.m.cfg.Live = nil
			case "unsupported":
				h.m.cfg.Live = &fakeLive{}
			}
			hint := wideHint(h)
			for _, want := range []string{"1-2 pick", "t free text"} {
				if strings.Contains(hint, want) != (state == "open") {
					t.Fatalf("%s hint: %s", state, hint)
				}
			}
			if state == "open" {
				if !strings.Contains(h.view(), "2. Database") {
					t.Fatal("option number missing")
				}
				h.send(press("?"))
				if !strings.Contains(h.view(), "pick an adj option") || !strings.Contains(h.view(), "type a free adj answer") {
					t.Fatal("help omits adj keys")
				}
			}
		})
	}
}
func TestGoldenDecisionAnswerConfirm(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	for _, mono := range []bool{false, true} {
		h, _ := decisionAnswerHarness(t)
		h.m.cfg.Theme = NewTheme(mono)
		h.send(press("2"))
		if h.m.confirm == nil {
			t.Fatal("confirmation missing")
		}
		name := "color"
		if mono {
			name = "mono"
		}
		checkGolden(t, "decision-answer-confirm-100x30-"+name+".txt", h.m.View().Content)
	}
}
func TestDecisionAnswerRefusalAndPending(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	h, l := decisionAnswerHarness(t)
	l.err = &ipc.Error{Code: daemon.CodeRefused, Message: "ADJ_DECIDED_BY is unset; set it before answering in adj"}
	h.send(press("1"))
	cmd := h.m.key(press("y"))
	if cmd == nil || len(h.m.actionFlights["adj:zeta#12"]) != 1 {
		t.Fatal("missing per-record flight")
	}
	h.send(press("2"))
	if h.m.confirm != nil {
		t.Fatal("pending answer confirmed twice")
	}
	h.deliver(cmd())
	if h.m.actionStatus != "ADJ_DECIDED_BY is unset; set it before answering in adj" || h.m.decisionPane == nil {
		t.Fatalf("refusal: %q", h.m.actionStatus)
	}
}

func TestDecisionAnswerFieldCancelEmptyPasteAndReadOnlyKeys(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	h, l := decisionAnswerHarness(t)
	h.send(press("t"))
	if !strings.Contains(h.view(), "Answer in adj:") || !strings.Contains(wideHint(h), "esc cancel") {
		t.Fatal("field is not visible")
	}
	h.send(press("enter"))
	if h.m.confirm != nil || h.m.decisionInput == nil || h.m.actionStatus != "Answer must not be empty" {
		t.Fatal("empty answer escaped the field")
	}
	h.send(tea.PasteMsg{Content: "a typed answer"})
	h.send(press("enter"))
	if h.m.confirm == nil || h.m.confirm.text != "a typed answer" {
		t.Fatal("paste did not reach confirmation")
	}
	h.send(press("n"))
	h.send(press("t"))
	h.send(press("esc"))
	if h.m.decisionInput != nil || h.m.decisionPane == nil || len(l.answers) != 0 {
		t.Fatal("escape did not cancel the field locally")
	}
	for _, state := range []string{"decided", "no-adj", "nil", "unsupported"} {
		for _, k := range []string{"1", "2", "3", "4", "t", "a", "x", "c", "y"} {
			h, l = decisionAnswerHarness(t)
			switch state {
			case "decided":
				h.m.decisionPane.record.Status = "decided"
			case "no-adj":
				h.m.projects[0].reg.Adj = false
			case "nil":
				h.m.cfg.Live = nil
			case "unsupported":
				h.m.cfg.Live = &fakeLive{}
			}
			h.deliver(press(k))
			if h.m.confirm != nil || h.m.decisionInput != nil || len(l.answers) != 0 {
				t.Fatalf("%s key %s offered a write", state, k)
			}
		}
	}
}

func TestDecisionAnswerPromptWrapsKeysAndPreservesTitle(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	for _, title := range []string{"Storage", "Which edits should ork make when the owner answers a record across multiple projects with a long descriptive title?"} {
		for _, view := range []int{questionsView, decisionsView} {
			for _, size := range [][2]int{{80, 24}, {100, 30}, {120, 40}} {
				h, _ := decisionAnswerHarness(t)
				h.m.view = view
				h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				h.m.decisionPane.record.Title = title
				h.send(press("2"))
				question := `Decide #12 (` + h.m.decisionPane.record.Title + `) as "Database" (recommended) in adj as Jon R?`
				const keys = "[y/N] (Enter is No)"
				if got := h.m.confirm.prompt(); got != question+" "+keys {
					t.Fatalf("full prompt: %q", got)
				}
				width, height, side := h.m.layoutSize()
				statusWidth := max(width-side-len(indent), 1)
				want := question + " " + keys
				if len(want) > statusWidth {
					want = question + "\n" + keys
				}
				if got := h.m.confirm.promptWidth(statusWidth); got != want {
					t.Fatalf("%v prompt: %q", size, got)
				}
				lines := wrapText(want, statusWidth)
				_, visible := h.m.decisionPage()
				if expected := max(height-4-min(len(lines), max(height-5, 1)), 1); visible != expected {
					t.Fatalf("%v status height: visible=%d want=%d", size, visible, expected)
				}
				rendered := plainView(h.m)
				for _, line := range lines {
					if !strings.Contains(rendered, indent+line) {
						t.Fatalf("%v view %d missing full status line %q:\n%s", size, view, line, rendered)
					}
				}
				if got := h.m.confirm.promptWidth(1000); got != question+" "+keys {
					t.Fatalf("wide prompt: %q", got)
				}
			}
		}
	}

}

func TestDecisionAnswerPendingHidesAnswerHints(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	h, l := decisionAnswerHarness(t)
	h.send(press("2"))
	cmd := h.m.key(press("y"))
	if cmd == nil {
		t.Fatal("no answer command")
	}
	hint := wideHint(h)
	for _, bad := range []string{"pick", "free text"} {
		if strings.Contains(hint, bad) {
			t.Errorf("pending hint offers %q: %s", bad, hint)
		}
	}
	h.send(press("t"))
	if h.m.decisionInput != nil || h.m.actionStatus != "Answer in adj is pending" {
		t.Fatal("pending key changed its refusal")
	}
	h.deliver(cmd())
	if len(l.answers) != 1 {
		t.Fatalf("writes: %d", len(l.answers))
	}
}

func TestDecisionAnswerHelpUsesOptionCount(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 5} {
		h, _ := decisionAnswerHarness(t)
		h.m.decisionPane.record.Options = make([]adjcli.Option, n)
		want := fmt.Sprintf("1-%d", min(n, 4))
		if !strings.Contains(wideHint(h), want+" pick") {
			t.Fatal("hint range incorrect")
		}
		help := strings.Join(h.m.helpLines(120, 60), "\n")
		if !strings.Contains(help, want+" pick an adj option") {
			t.Fatalf("%d options: help range missing:\n%s", n, help)
		}
		if !strings.Contains(help, "enter reviews") || strings.Contains(help, "enter confirms") {
			t.Fatal("help misstates free answer Enter")
		}
	}
}

func TestDecisionAnswerFieldHintsReviewNonemptyText(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "Jon R")
	h, _ := decisionAnswerHarness(t)
	h.send(press("t"))
	for _, text := range []string{"", " \t ", "answer"} {
		h.m.decisionInput = &textField{}
		h.send(tea.PasteMsg{Content: text})
		hint := wideHint(h)
		if strings.Contains(hint, "enter review") != (strings.TrimSpace(text) != "") || strings.Contains(hint, "enter confirm") || !strings.Contains(hint, "esc cancel") {
			t.Fatalf("text %q: %s", text, hint)
		}
	}
}

func TestDecisionAnswerStatusNamesRecordBeforeAdj(t *testing.T) {
	a := runAction{verb: "decide", adjID: 173}
	if got := a.pendingText(); got != "deciding #173 in adj" {
		t.Errorf("pending: %q", got)
	}
	a.adjID = 12
	if got := a.doneText(); got != "decided #12 in adj" {
		t.Errorf("done: %q", got)
	}
}
