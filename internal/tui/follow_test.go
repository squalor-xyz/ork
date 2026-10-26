// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// followRun is an engine run linked to a ledger run of the same name.
func followRun(id, item, state string, age time.Duration) source.EngineRun {
	return source.EngineRun{ID: id, Item: item, State: state, RelayRun: "l" + id, CreatedAt: t0.Add(-age)}
}

func followLedger(id, item, state, outcome string, age time.Duration) slicercli.RelayRun {
	r := relay(id, t0.Add(-age).Format(time.RFC3339), item)
	r.State, r.Outcome = state, outcome
	r.UpdatedAt = t0.Add(-time.Minute).Format(time.RFC3339)
	return r
}

// followSnap holds the engine runs, each with an active ledger run unless it
// is terminal, and any extra active ledger runs (started elsewhere).
func followSnap(runs []source.EngineRun, extra ...slicercli.RelayRun) source.Snapshot {
	s := snap(proj("ork", false), nil, nil)
	s.EngineRuns = runs
	for _, r := range runs {
		s.Runs.Value = append(s.Runs.Value, followLedger(r.RelayRun, r.Item, "active", "", t0.Sub(r.CreatedAt)))
	}
	s.Runs.Value = append(s.Runs.Value, extra...)
	return s
}

// followHarness opens a pane on engine run A (S27), with B (S28) also running.
func followHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, proj("ork", false))
	h.src.set(followSnap([]source.EngineRun{followRun("A", "S27", "implement", 10*time.Minute), followRun("B", "S28", "review", time.Minute)}))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.m.openPane(ovRow{engineID: "A", name: "ork"})
	return h
}

func (h *harness) applyRuns(runs []source.EngineRun, extra ...slicercli.RelayRun) {
	h.t.Helper()
	h.m.apply(snapshotMsg{name: "ork", snap: followSnap(runs, extra...)})
}

func TestPaneSwitchesToTheNewestRunningRunWhenItsRunEnds(t *testing.T) {
	for _, tc := range []struct{ state, notice string }{
		{"committed", "S27 committed; now showing S28"},
		{"stopped", "S27 stopped; now showing S28"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			h := followHarness(t)
			if v := h.view(); !strings.Contains(v, "Run A") || strings.Contains(v, "now showing") {
				t.Fatalf("pane must open on A without a notice:\n%s", v)
			}
			h.applyRuns([]source.EngineRun{followRun("A", "S27", tc.state, 10*time.Minute), followRun("B", "S28", "review", time.Minute)})
			v := h.view()
			if h.m.pane == nil || h.m.pane.run != "B" || h.m.pane.ledger {
				t.Fatalf("pane must follow engine run B: %+v", h.m.pane)
			}
			if !strings.Contains(v, "Run B") || !strings.Contains(v, tc.notice) || strings.Contains(v, "Run A") {
				t.Fatalf("pane must show B and the switch notice %q:\n%s", tc.notice, v)
			}
		})
	}
}

func TestPaneSwitchesWhenItsRunIsSuperseded(t *testing.T) {
	h := followHarness(t)
	// A stops and the owner relaunches S27 as C, which is the newest run.
	h.applyRuns([]source.EngineRun{followRun("A", "S27", "stopped", 10*time.Minute), followRun("B", "S28", "review", time.Minute), followRun("C", "S27", "setup", 10*time.Second)})
	v := h.view()
	if h.m.pane.run != "C" || !strings.Contains(v, "S27 superseded; now showing S27") {
		t.Fatalf("a superseded run must hand the pane to the newest run (C):\n%s", v)
	}
}

func TestPaneOnAnEndedLedgerRunSwitchesToTheNewestRunningRun(t *testing.T) {
	for _, tc := range []struct{ outcome, word string }{{"pass", "passed"}, {"fail", "failed"}, {"stopped", "stopped"}, {"", "stopped"}} {
		t.Run(tc.outcome, func(t *testing.T) { endedLedgerPane(t, tc.outcome, tc.word) })
	}
}

func endedLedgerPane(t *testing.T, outcome, word string) {
	h := newHarness(t, proj("ork", false))
	s := followSnap([]source.EngineRun{followRun("B", "S28", "review", time.Minute)}, followLedger("x1", "S27", "active", "", 10*time.Minute))
	h.src.set(s)
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.m.openPane(ovRow{ledgerID: "x1", name: "ork"})
	if !h.m.pane.ledger || !strings.Contains(h.view(), "started elsewhere") {
		t.Fatalf("setup: pane must open on the ledger run:\n%s", h.view())
	}
	s = followSnap([]source.EngineRun{followRun("B", "S28", "review", time.Minute)})
	s.BoardRuns.Value = []slicercli.RelayRun{followLedger("x1", "S27", "ended", outcome, 10*time.Minute)}
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	v := h.view()
	if h.m.pane.run != "B" || h.m.pane.ledger || !strings.Contains(v, "S27 "+word+"; now showing S28") {
		t.Fatalf("ended ledger pane must follow B:\n%s", v)
	}
}

func TestPaneFollowsTheNewestOfSeveralRunningRuns(t *testing.T) {
	h := followHarness(t)
	// B (1 m old) and C (30 s old) run; an unlinked ledger run D started 5 s ago.
	d := followLedger("D", "S40", "active", "", 5*time.Second)
	h.applyRuns([]source.EngineRun{
		followRun("A", "S27", "committed", 10*time.Minute), followRun("B", "S28", "review", time.Minute),
		followRun("C", "S29", "gate", 30*time.Second),
	}, d)
	if !h.m.pane.ledger || h.m.pane.run != "D" {
		t.Fatalf("newest running (unlinked ledger run D) expected, got %+v", h.m.pane)
	}
	if v := h.view(); !strings.Contains(v, "Run D") || !strings.Contains(v, "S27 committed; now showing S40") {
		t.Fatalf("ledger target must show:\n%s", v)
	}

	h = followHarness(t)
	h.applyRuns([]source.EngineRun{
		followRun("A", "S27", "committed", 10*time.Minute), followRun("B", "S28", "review", time.Minute),
		followRun("C", "S29", "gate", 30*time.Second),
	})
	if h.m.pane.run != "C" || h.m.pane.ledger {
		t.Fatalf("newest engine run C expected, got %+v", h.m.pane)
	}
}

func TestPaneIgnoresLedgerRunsLinkedToAnEngineRun(t *testing.T) {
	h := followHarness(t)
	// Only A (now committed) exists as an engine run; its ledger run is still
	// active in the ledger for a moment. It is A's own run, not a new one.
	h.applyRuns([]source.EngineRun{followRun("A", "S27", "committed", 10*time.Minute)})
	if h.m.pane.run != "A" || h.m.pane.ledger {
		t.Fatalf("the pane's own linked ledger run must not be a target: %+v", h.m.pane)
	}
}

func TestPaneIgnoresStaleLedgerRuns(t *testing.T) {
	h := followHarness(t)
	stale := followLedger("S", "S50", "stale", "", time.Minute)
	h.applyRuns([]source.EngineRun{followRun("A", "S27", "committed", 10*time.Minute)}, stale)
	if h.m.pane.run != "A" {
		t.Fatalf("a stale ledger run is not running: %+v", h.m.pane)
	}
}

func TestPaneStaysOnTheEndedRunWhenNothingElseRuns(t *testing.T) {
	for _, other := range []string{"paused", "awaiting-approval", "stopped", "committed"} {
		t.Run(other, func(t *testing.T) {
			h := followHarness(t)
			h.applyRuns([]source.EngineRun{followRun("A", "S27", "committed", 10*time.Minute), followRun("B", "S28", other, time.Minute)})
			v := h.view()
			if h.m.pane.run != "A" || strings.Contains(v, "now showing") {
				t.Fatalf("nothing is running; the pane must stay on A without a notice:\n%s", v)
			}
			if !strings.Contains(v, "Run A") {
				t.Fatalf("pane must show the final state of A:\n%s", v)
			}
		})
	}
}

func TestPaneDoesNotSwitchFromARunThatIsNotTerminal(t *testing.T) {
	for _, state := range []string{"implement", "review", "paused", "awaiting-approval"} {
		t.Run(state, func(t *testing.T) {
			h := followHarness(t)
			h.applyRuns([]source.EngineRun{followRun("A", "S27", state, 10*time.Minute), followRun("B", "S28", "review", time.Minute)})
			if h.m.pane.run != "A" || strings.Contains(h.view(), "now showing") {
				t.Fatalf("%s is not terminal; the pane must stay: %+v", state, h.m.pane)
			}
		})
	}
}

func TestPaneDoesNotSwitchWhileTheOwnerIsTypingAnAnswer(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	a := followRun("A", "S27", "paused", 10*time.Minute)
	a.Pending = "question"
	b := followRun("B", "S28", "review", time.Minute)
	h.src.set(followSnap([]source.EngineRun{a, b}))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.m.openPane(ovRow{engineID: "A", name: "ork"})
	typeAnswer(h, "use local")
	// Someone stops A from the shell while the owner types: A is terminal now.
	a.State = "stopped"
	h.applyRuns([]source.EngineRun{a, b})
	if h.m.pane.run != "A" || h.m.pane.input.Value() != "use local" {
		t.Fatalf("the answer field has text; the pane must stay on A: %+v", h.m.pane)
	}
	h.m.pane.input = textField{}
	h.applyRuns([]source.EngineRun{a, b})
	if h.m.pane.run != "B" {
		t.Fatalf("with the field empty the pane must move on: %+v", h.m.pane)
	}
}

func TestPaneDoesNotSwitchWhileAConfirmationIsOpen(t *testing.T) {
	h := followHarness(t)
	h.m.confirm = &runAction{verb: "retry", project: "ork", run: "A"}
	h.applyRuns([]source.EngineRun{followRun("A", "S27", "stopped", 10*time.Minute), followRun("B", "S28", "review", time.Minute)})
	if h.m.pane.run != "A" {
		t.Fatalf("an open confirmation names run A; the pane must stay: %+v", h.m.pane)
	}
}

func TestPaneSwitchStaysInTheSameProject(t *testing.T) {
	h := newHarness(t, proj("ork", false), proj("other", false))
	h.src.set(followSnap([]source.EngineRun{followRun("A", "S27", "implement", 10*time.Minute)}))
	o := snap(proj("other", false), nil, nil)
	o.EngineRuns = []source.EngineRun{followRun("Z", "S90", "review", time.Second)}
	h.src.set(o)
	h.start()
	h.m.openPane(ovRow{engineID: "A", name: "ork"})
	h.applyRuns([]source.EngineRun{followRun("A", "S27", "committed", 10*time.Minute)})
	if h.m.pane.run != "A" || h.m.pane.project != "ork" {
		t.Fatalf("a run of another project must not take the pane: %+v", h.m.pane)
	}
}

func TestPaneSwitchResetsScrollOffsetAndAnswerState(t *testing.T) {
	h := followHarness(t)
	h.m.pane.offset = 4
	h.m.pane.answered, h.m.pane.answerState, h.m.pane.adj = true, "paused", true
	h.m.actionStatus = "answered"
	h.applyRuns([]source.EngineRun{followRun("A", "S27", "committed", 10*time.Minute), followRun("B", "S28", "review", time.Minute)})
	p := h.m.pane
	if p.run != "B" || p.offset != 0 || p.answered || p.adj || p.answerState != "" || h.m.actionStatus != "" {
		t.Fatalf("the switch must start a fresh pane: %+v status %q", p, h.m.actionStatus)
	}
}

func TestPaneSwitchNoticeFadesAfterTheNextRefresh(t *testing.T) {
	h := followHarness(t)
	ended := []source.EngineRun{followRun("A", "S27", "committed", 10*time.Minute), followRun("B", "S28", "review", time.Minute)}
	h.applyRuns(ended)
	if !strings.Contains(h.view(), "now showing S28") {
		t.Fatal("notice missing right after the switch")
	}
	// The 1 s ledger refresh sends snapshots every second: the notice has to
	// outlive the next one.
	h.advance(time.Second)
	h.applyRuns(ended)
	if !strings.Contains(h.view(), "now showing S28") {
		t.Fatal("notice faded on the very next 1 s refresh; it could not be read")
	}
	h.advance(noticeFor)
	h.applyRuns(ended)
	if v := h.view(); strings.Contains(v, "now showing") || !strings.Contains(v, "Run B") {
		t.Fatalf("notice must fade after the next refresh past %s:\n%s", noticeFor, v)
	}
}

func TestPaneSwitchGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprintf("%t", mono), func(t *testing.T) {
			h := followHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			h.applyRuns([]source.EngineRun{followRun("A", "S27", "committed", 10*time.Minute), followRun("B", "S28", "review", time.Minute)})
			h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			checkGolden(t, fmt.Sprintf("pane-switch-mono-%t.txt", mono), h.m.View().Content)
		})
	}
}

func TestPaneOpenedFromQuestionsDoesNotFollow(t *testing.T) {
	h := followHarness(t)
	h.m.view = questionsView
	h.applyRuns([]source.EngineRun{followRun("A", "S27", "committed", 10*time.Minute), followRun("B", "S28", "review", time.Minute)})
	if h.m.pane.run != "A" || strings.Contains(h.view(), "now showing") {
		t.Fatalf("only the Overview pane follows: %+v", h.m.pane)
	}
	h.m.view = 0
	h.applyRuns([]source.EngineRun{followRun("A", "S27", "committed", 10*time.Minute), followRun("B", "S28", "review", time.Minute)})
	if h.m.pane.run != "B" {
		t.Fatalf("back on the Overview the pane must follow: %+v", h.m.pane)
	}
}

func TestPaneSwitchNoticeIsDim(t *testing.T) {
	h := followHarness(t)
	h.m.cfg.Theme = NewTheme(false)
	h.applyRuns([]source.EngineRun{followRun("A", "S27", "committed", 10*time.Minute), followRun("B", "S28", "review", time.Minute)})
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	want := h.m.cfg.Theme.Dim.Render("S27 committed; now showing S28")
	if !strings.Contains(h.m.View().Content, want) {
		t.Fatalf("notice must be dim (%q):\n%s", want, h.m.View().Content)
	}
}
