// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func fix1LedgerQuestion() slicercli.RelayRun {
	return slicercli.RelayRun{ID: "external", Items: []string{"S77"}, State: "active", UpdatedAt: "2026-10-19T00:00:00Z", Counts: slicercli.Counts{Steps: 1, Waiting: 1}, Steps: []slicercli.Step{{ID: "1", Role: "reviewer", Model: "opus", Phase: "wait", WaitingOn: "owner", Task: "S77 reviewer", BeganAt: "2026-10-19T00:00:00Z", UpdatedAt: "2026-10-19T00:02:00Z", Question: &slicercli.LedgerQuestion{ID: "q1", Title: "Which store holds the index?", Options: []runrec.Option{{Label: "file", Summary: "one JSON file"}, {Label: "memory", Summary: "rebuild at startup"}}, Recommend: "file", Blocker: true}}}}
}
func TestFix1WaitTreeUsesNeedsMarkAndNoElapsed(t *testing.T) {
	r := fix1LedgerQuestion()
	h := treeHarness(t, []slicercli.RelayRun{r}, "")
	lines := plainLines(h.m.runTreeLines(ledgerTreeSteps(r.Steps), 100, 0))
	if !strings.HasPrefix(lines[0], "? reviewer") || !strings.Contains(lines[0], "waiting on owner") || strings.Contains(lines[0], "2m") {
		t.Fatalf("wait tree row: %q", lines[0])
	}
}
func TestFix1LedgerWaitNamesRole(t *testing.T) {
	r := fix1LedgerQuestion()
	h := newHarness(t, proj("ork", false))
	row := h.m.ledgerRunRow(source.Snapshot{}, r)
	if row.role != "reviewer waiting" {
		t.Fatalf("wait role = %q", row.role)
	}
}
func TestFix1QueuedTailSaysQueued(t *testing.T) {
	r := fix1LedgerQuestion()
	h := treeHarness(t, []slicercli.RelayRun{r}, "")
	tail := strings.Join(plainLines(h.m.boardTail(ledgerTreeSteps(r.Steps), 100, 12)), "\n")
	if strings.Contains(tail, "waiting") || !strings.Contains(tail, "queued") {
		t.Fatalf("queued tail: %q", tail)
	}
}
func TestFix1SwitchCardHasNoDeviationPrefix(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	card := &runrec.ApprovalCard{Verdict: "PASS", Deviations: []string{"role switched: implementer codex to claude (out of credits)"}}
	text := strings.Join(plainLines(h.m.cardSummary(card, 100)), "\n")
	if strings.Contains(text, "Deviation:") || !strings.Contains(text, "role switched:") {
		t.Fatal(text)
	}
}
func TestFix1ExternalOwnerQuestionNeedsYou(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			r := fix1LedgerQuestion()
			h := newHarness(t, proj("ork", false))
			h.src.set(snap(proj("ork", false), nil, nil, r))
			h.start()
			h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			needs, running := h.m.rows()
			if len(needs) != 1 || len(running) != 0 {
				t.Errorf("Needs you=%d Running=%d", len(needs), len(running))
			}
			if got := h.m.byName["ork"].needs(); got != 1 {
				t.Errorf("sidebar needs=%d", got)
			}
			if h.m.byName["ork"].running {
				t.Error("sidebar still marks external wait running")
			}
			view := plainView(h.m)
			if row := mainRow(view, "ork", "S77"); !strings.Contains(row, "?") {
				t.Errorf("rendered row has no Needs-you mark: %q", row)
			}
			for _, want := range []string{"Needs you 1", "Running 0"} {
				if !strings.Contains(view, want) {
					t.Errorf("Overview/sidebar missing %q", want)
				}
			}
			checkGolden(t, fmt.Sprintf("external-owner-question-%dx%d.txt", size[0], size[1]), h.m.View().Content)
			if strings.Contains(view, "nothing needs you") {
				t.Error("empty Needs you message shown")
			}
			if len(needs) > 0 && !strings.HasPrefix(needs[0].kind, "?") {
				t.Errorf("Needs you mark=%q", needs[0].kind)
			}
			h.deliver(press("enter"))
			view = plainView(h.m)
			for _, want := range []string{"Which store holds the index?", "file: one JSON file", "memory: rebuild at startup", "read-only"} {
				if !strings.Contains(view, want) {
					t.Errorf("pane missing %q:\n%s", want, view)
				}
			}
			if h.m.paneQuestion() {
				t.Error("external question is editable")
			}
			checkGolden(t, fmt.Sprintf("external-owner-question-pane-%dx%d.txt", size[0], size[1]), h.m.View().Content)
			// A ledger question has no input or answer action even when number keys are pressed.
			h.deliver(press("1"))
			if h.m.confirm != nil {
				t.Error("external option opened an answer confirmation")
			}

		})
	}
}
func TestFix1ExternalQuestionClassificationEdges(t *testing.T) {
	for _, kind := range []string{"no question", "approval", "answered", "ended", "linked"} {
		t.Run(kind, func(t *testing.T) {
			r := fix1LedgerQuestion()
			s := snap(proj("ork", false), nil, nil, r)
			switch kind {
			case "no question":
				s.Runs.Value[0].Steps[0].Question = nil
			case "approval":
				s.Runs.Value[0].Steps[0].WaitingOn = "approval"
			case "answered":
				s.Runs.Value[0].Steps[0].Question.Answer = map[string]string{"text": "file"}
			case "ended":
				s.Runs.Value[0].State = "ended"
			case "linked":
				s.EngineRuns = []source.EngineRun{{ID: "owned", Item: "S77", RelayRun: r.ID, State: "paused", Pending: "question"}}
			}
			h := newHarness(t, proj("ork", false))
			h.src.set(s)
			h.start()
			needs, _ := h.m.rows()
			want := 0
			if kind == "linked" {
				want = 1
			}
			if len(needs) != want || h.m.byName["ork"].needs() != want {
				t.Fatalf("classification %s: rows=%d count=%d", kind, len(needs), h.m.byName["ork"].needs())
			}
		})
	}
}
