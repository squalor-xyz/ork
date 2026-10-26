// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func staleApprovalHarness(t *testing.T, state string) *harness {
	t.Helper()
	h := newHarness(t, proj("ork", false))
	s := engineSnapshot(t, state, "ledger")
	s.EngineRuns[0].CreatedAt = h.now.Add(-20 * time.Hour)
	// The last metadata update is newer than the start of this wait.
	s.EngineRuns[0].UpdatedAt = h.now.Add(-time.Minute)
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"wait_started_at":%q}`, h.now.Add(-19*time.Hour).Format(time.RFC3339Nano))), &s.EngineRuns[0]); err != nil {
		t.Fatal(err)
	}
	s.EngineRuns[0].Wait = "owner approval"
	ledger := s.Runs.Value[0]
	ledger.State = "stale"
	ledger.BeganAt = h.now.Add(-20 * time.Hour).Format(time.RFC3339Nano)
	ledger.UpdatedAt = h.now.Add(-20*time.Hour + 23*time.Minute).Format(time.RFC3339Nano)
	ledger.Steps = []slicercli.Step{{Role: "orchestrator", Phase: "start"}}
	s.Runs.Value = nil
	s.BoardRuns.Value = []slicercli.RelayRun{ledger}
	h.src.set(s)
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return h
}

func TestAwaitingApprovalWithStaleLedgerIsNotDimmed(t *testing.T) {
	for _, state := range []string{"awaiting-approval", "paused", "implement"} {
		t.Run(state, func(t *testing.T) {
			h := staleApprovalHarness(t, state)
			needs, running := h.m.rows()
			rows := append(needs, running...)
			if len(rows) != 1 {
				t.Fatalf("want one joined row, got %d", len(rows))
			}
			row := rows[0]
			word := state
			if state == "awaiting-approval" {
				word = "awaiting approval"
			}
			if row.dim || strings.Contains(row.text, "stale") || !strings.Contains(row.text, word) || row.live.ledger.State != state {
				t.Fatalf("engine state lost: dim=%t text=%q ledger=%q", row.dim, row.text, row.live.ledger.State)
			}
			h.m.openPane(row)
			if got := h.m.paneBoardRun().State; got != state {
				t.Fatalf("pane state %q, want %q", got, state)
			}
		})
	}
}

func TestApproveRowAgeCountsFromWaitStart(t *testing.T) {
	for _, state := range []string{"awaiting-approval", "paused"} {
		t.Run(state, func(t *testing.T) {
			h := staleApprovalHarness(t, state)
			for _, hours := range []int{19, 20} {
				needs, _ := h.m.rows()
				if len(needs) != 1 {
					t.Fatalf("want one approval row, got %d", len(needs))
				}
				if !strings.Contains(needs[0].text, stateWordForTest(state)+"  "+fmt.Sprintf("%dh00m", hours)) {
					t.Fatalf("want wait age %dh00m, got %q", hours, needs[0].text)
				}
				h.now = h.now.Add(time.Hour)
			}
		})
	}
}

func TestApproveRowOpensThePane(t *testing.T) {
	h := staleApprovalHarness(t, "awaiting-approval")
	h.deliver(press("enter"))
	if h.m.pane == nil || h.m.pane.run != "engine" || h.m.pane.ledger {
		t.Fatalf("Enter did not open engine pane: %+v", h.m.pane)
	}
	if _, _, ok := h.m.paneRow(); !ok {
		t.Fatal("pane has no engine row")
	}
	if steps := h.m.paneSteps(); len(steps) != 1 || steps[0].Phase == "stale" {
		t.Fatalf("pane lost engine state on ledger steps: %+v", steps)
	}
}

func TestStaleApprovalGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h := staleApprovalHarness(t, "awaiting-approval")
		h.m.cfg.Theme = NewTheme(mono)
		checkGolden(t, fmt.Sprintf("stale-approval-mono-%t.txt", mono), h.m.View().Content)
		h.deliver(press("enter"))
		checkGolden(t, fmt.Sprintf("stale-approval-pane-mono-%t.txt", mono), h.m.View().Content)
	}
}

func TestApprovalAgeForOlderRecords(t *testing.T) {
	for _, state := range []string{"awaiting-approval", "paused"} {
		for _, updated := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s-updated-%t", state, updated), func(t *testing.T) {
				h := staleApprovalHarness(t, state)
				r := &h.m.byName["ork"].snap.EngineRuns[0]
				r.WaitStartedAt = time.Time{}
				r.UpdatedAt = h.now.Add(-18 * time.Hour)
				want := "18h00m"
				if !updated {
					r.UpdatedAt = time.Time{}
					want = "20h00m"
				}
				needs, _ := h.m.rows()
				if len(needs) != 1 || !strings.Contains(needs[0].text, stateWordForTest(state)+"  "+want) {
					t.Fatalf("older record age should be %s", want)
				}
			})
		}
	}
}

func TestWaitingPaneLastHeardHasNoAlarm(t *testing.T) {
	for _, state := range []string{"awaiting-approval", "paused"} {
		t.Run(state, func(t *testing.T) {
			h := staleApprovalHarness(t, state)
			text, style := h.m.lastHeard(slicercli.RelayRun{State: state, UpdatedAt: h.now.Add(-19 * time.Hour).Format(time.RFC3339Nano)})
			if text != "heard 19h00m" || style.Render("x") != h.m.cfg.Theme.Dim.Render("x") {
				t.Fatalf("wait last heard must be quiet and dim: %q style=%q", text, style.Render("x"))
			}
		})
	}
}

func TestApprovalPaneTitleUsesStateWords(t *testing.T) {
	h := staleApprovalHarness(t, "awaiting-approval")
	h.deliver(press("enter"))
	text := sgr.ReplaceAllString(strings.Join(h.m.paneLinesAs(200, 60, "", false), "\n"), "")
	if !strings.Contains(text, "Run engine  S22  awaiting approval") || strings.Contains(text, "awaiting-approval") {
		t.Fatal(text)
	}
}

func TestElapsedDaysAndHours(t *testing.T) {
	for _, tc := range []struct {
		duration time.Duration
		want     string
	}{
		{-time.Second, "0s"}, {59 * time.Second, "59s"}, {time.Minute, "1m"}, {time.Hour, "1h00m"},
		{24*time.Hour - time.Second, "23h59m"}, {24 * time.Hour, "1d00h"}, {74*time.Hour + 59*time.Minute, "3d02h"},
	} {
		if got := elapsed(tc.duration); got != tc.want {
			t.Errorf("elapsed(%s)=%q, want %q", tc.duration, got, tc.want)
		}
	}
	h := staleApprovalHarness(t, "implement")
	h.now = h.now.Add(54 * time.Hour)
	_, running := h.m.rows()
	if len(running) != 1 || !strings.Contains(running[0].text, "3d02h") {
		t.Fatalf("running age missing days: %+v", running)
	}
}

func TestWaitAgeSurvivesAt80Columns(t *testing.T) {
	for _, state := range []string{"awaiting-approval", "paused"} {
		t.Run(state, func(t *testing.T) {
			h := staleApprovalHarness(t, state)
			h.m.byName["ork"].snap.EngineRuns[0].Wait = "owner approval; a very long explanation that should be clipped at narrow widths"
			h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			v := h.m.View().Content
			row := strings.Split(v, "\n")[4]
			word := state
			if state == "awaiting-approval" {
				word = "awaiting approval"
			}
			if !strings.Contains(row, word+"  19h00m") {
				t.Fatalf("age cut from row: %s", row)
			}
			checkGolden(t, fmt.Sprintf("wait-age-%s-80x24.txt", state), v)
		})
	}
}

func stateWordForTest(state string) string {
	if state == "awaiting-approval" {
		return "awaiting approval"
	}
	return state
}
