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
	"github.com/squalor-xyz/ork/internal/source"
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
			if text != "waiting since Oct 18 21:42" || style.Render("x") != h.m.cfg.Theme.Dim.Render("x") {
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

// ownerWaitPane is the open pane of the run, as plain text.
func ownerWaitPane(h *harness, w, rows int) string {
	return strings.Join(plainLines(h.m.paneLinesAs(w, rows, "", false)), "\n")
}

func ledgerWaitHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, proj("ork", false))
	h.src.set(snap(proj("ork", false), nil, nil, fix1LedgerQuestion()))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return h
}

func TestAwaitingApprovalRowShowsWaitingSince(t *testing.T) {
	// The wait began 19 hours before t0 (16:42): 21:42 the day before, so the
	// pane names the day (S154).
	for _, state := range []string{"awaiting-approval", "paused"} {
		t.Run(state, func(t *testing.T) {
			h := staleApprovalHarness(t, state)
			h.deliver(press("enter"))
			pane := ownerWaitPane(h, 120, 40)
			if !strings.Contains(pane, "waiting since Oct 18 21:42") || strings.Contains(pane, "last heard") {
				t.Fatalf("pane should say waiting since Oct 18 21:42 in place of last heard:\n%s", pane)
			}
		})
	}
	t.Run("ledger wait step", func(t *testing.T) {
		h := ledgerWaitHarness(t)
		if got := detailText(h.m, 100); !strings.Contains(got, "waiting since 00:02") || strings.Contains(got, "heard") {
			t.Fatalf("detail should say waiting since 00:02: %q", got)
		}
		h.deliver(press("enter"))
		pane := ownerWaitPane(h, 100, 30)
		if !strings.Contains(pane, "waiting since 00:02") || strings.Contains(pane, "last heard") {
			t.Fatalf("pane should say waiting since 00:02 in place of last heard:\n%s", pane)
		}
	})
	t.Run("a running run keeps last heard", func(t *testing.T) {
		h := staleApprovalHarness(t, "implement")
		h.deliver(press("enter"))
		pane := ownerWaitPane(h, 120, 40)
		if !strings.Contains(pane, "last heard") || strings.Contains(pane, "waiting since") {
			t.Fatalf("a running run keeps last heard:\n%s", pane)
		}
	})
}

func TestWaitSinceUsesTheStepUpdatedAt(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	// The newest wait step decides, not the run's own update or a done step.
	r := slicercli.RelayRun{State: "active", UpdatedAt: "2026-10-19T12:00:00Z", Steps: []slicercli.Step{
		{ID: "1", Role: "implementer", Phase: "done", UpdatedAt: "2026-10-19T09:15:00Z"},
		{ID: "2", Role: "reviewer", Phase: "wait", WaitingOn: "owner", UpdatedAt: "2026-10-19T10:20:00Z"},
	}}
	text, style := h.m.lastHeard(r)
	if text != "waiting since 10:20" || style.Render("x") != h.m.cfg.Theme.Dim.Render("x") {
		t.Fatalf("ledger wait: %q style=%q", text, style.Render("x"))
	}
	// A run waiting for approval or paused has only its own update time.
	for _, state := range []string{"awaiting-approval", "paused"} {
		text, _ := h.m.lastHeard(slicercli.RelayRun{State: state, UpdatedAt: "2026-10-19T08:05:00Z"})
		if text != "waiting since 08:05" {
			t.Fatalf("%s: %q", state, text)
		}
	}
	// The time is shown in the clock's zone, as the refresh time is.
	zone := time.FixedZone("EST", -5*3600)
	h.m.cfg.Now = func() time.Time { return t0.In(zone) }
	if text, _ := h.m.lastHeard(r); text != "waiting since 05:20" {
		t.Fatalf("clock zone: %q", text)
	}
}

func TestWaitTextHiddenWhenItRestatesTheState(t *testing.T) {
	h := staleApprovalHarness(t, "awaiting-approval")
	needs, _ := h.m.rows()
	if len(needs) != 1 || strings.Contains(needs[0].text, "owner approval") || !strings.Contains(needs[0].text, "awaiting approval  19h00m") {
		t.Fatalf("row repeats the state: %+v", needs)
	}
	h.deliver(press("enter"))
	if pane := ownerWaitPane(h, 120, 40); strings.Contains(pane, "owner approval") {
		t.Fatalf("pane repeats the state:\n%s", pane)
	}
	// A wait text that says more than the state stays.
	h = staleApprovalHarness(t, "awaiting-approval")
	h.m.byName["ork"].snap.EngineRuns[0].Wait = "owner approval; gate passed with deviations"
	needs, _ = h.m.rows()
	if len(needs) != 1 || !strings.Contains(needs[0].text, "owner approval; gate passed with deviations") {
		t.Fatalf("wait text with more to say was hidden: %+v", needs)
	}
}

func TestOwnerWaitRowsSayNOfMSteps(t *testing.T) {
	for _, state := range []string{"awaiting-approval", "paused"} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			h.start()
			h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, state, "ledger")})
			needs, _ := h.m.rows()
			if len(needs) != 1 || !strings.Contains(needs[0].text, "2 of 6 steps") || strings.Contains(needs[0].text, "running") {
				t.Fatalf("owner wait row: %+v", needs)
			}
		})
	}
	t.Run("implement keeps running", func(t *testing.T) {
		h := newHarness(t, proj("ork", false))
		h.start()
		h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, "implement", "ledger")})
		_, running := h.m.rows()
		if len(running) != 1 || !strings.Contains(running[0].text, "2 of 6 steps running") {
			t.Fatalf("running row: %+v", running)
		}
	})
	t.Run("ledger owner wait keeps running", func(t *testing.T) {
		r := fix1LedgerQuestion()
		r.Counts = slicercli.Counts{Steps: 6, Running: 2, Waiting: 1}
		// A relay started elsewhere keeps slicer's running count (owner decision).
		if _, rest, _ := runParts(r, t0); !strings.Contains(rest, "active, 2 of 6 steps running") {
			t.Fatalf("ledger owner wait: %q", rest)
		}
	})
}

func TestOwnerWaitDoesNotAnimateTheStageBar(t *testing.T) {
	if boardLive("awaiting-approval") || boardLive("paused") || !boardLive("implement") {
		t.Fatal("an owner wait is not live; a working run is")
	}
	h := staleApprovalHarness(t, "awaiting-approval")
	ledger := &h.m.byName["ork"].snap.BoardRuns.Value[0]
	ledger.Steps = []slicercli.Step{
		{ID: "1", Role: "orchestrator", Phase: "start"},
		{ID: "2", Role: "implementer", Phase: "start", Parent: "1"},
	}
	h.deliver(press("enter"))
	var first string
	for frame := 0; frame < 4; frame++ {
		h.m.treeFrame = frame
		pane := ownerWaitPane(h, 120, 40)
		if frame == 0 {
			first = pane
			if !strings.Contains(pane, "[~~") || !strings.Contains(pane, "| orchestrator") {
				t.Fatalf("expected the bar and the start row:\n%s", pane)
			}
		}
		if pane != first {
			t.Fatalf("pane moved at frame %d:\n%s\nwant\n%s", frame, pane, first)
		}
	}
	// No tick is armed for a pane that holds an owner wait.
	h.m.frameArmed = false
	if h.m.frameVisible() {
		t.Fatal("frame animation armed for an owner wait")
	}
}

func TestOwnerWaitGoldens(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		h := staleApprovalHarness(t, "awaiting-approval")
		h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		h.m.byName["ork"].snap.BoardRuns.Value[0].Steps = []slicercli.Step{
			{ID: "1", Role: "orchestrator", Phase: "start"},
			{ID: "2", Role: "implementer", Phase: "done", Parent: "1"},
			{ID: "3", Role: "reviewer", Phase: "done", Parent: "1"},
			{ID: "4", Role: "gate", Phase: "done", Parent: "1"},
		}
		h.deliver(press("enter"))
		checkGolden(t, fmt.Sprintf("owner-wait-pane-%dx%d.txt", size[0], size[1]), h.m.View().Content)

		w := ledgerWaitHarness(t)
		w.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		checkGolden(t, fmt.Sprintf("owner-wait-ledger-%dx%d.txt", size[0], size[1]), w.m.View().Content)
	}
}

func TestRowCellSaysWaitAge(t *testing.T) {
	// The row cell is short ("wait 16h40m"); the pane and detail keep the clock.
	h := newHarness(t, proj("ork", false))
	row := h.m.ledgerRunRow(source.Snapshot{}, fix1LedgerQuestion())
	if row.heard != "wait 16h40m" || row.heardStyle.Render("x") != h.m.cfg.Theme.Dim.Render("x") {
		t.Fatalf("ledger row cell: %q", row.heard)
	}
	for _, state := range []string{"awaiting-approval", "paused"} {
		h := staleApprovalHarness(t, state)
		needs, _ := h.m.rows()
		if len(needs) != 1 {
			t.Fatalf("%s: want one row", state)
		}
		if got := h.m.liveText(needs[0]).heard; got != "wait 19h00m" {
			t.Fatalf("%s running-view cell: %q", state, got)
		}
	}
	// A working run keeps its heard cell.
	h = staleApprovalHarness(t, "implement")
	_, running := h.m.rows()
	if got := h.m.liveText(running[0]).heard; !strings.HasPrefix(got, "heard ") && !strings.HasPrefix(got, "!") {
		t.Fatalf("working run cell: %q", got)
	}
}

func TestWaitSinceNamesTheDayWhenItIsNotToday(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	step := func(at string) slicercli.RelayRun {
		return slicercli.RelayRun{State: "active", Steps: []slicercli.Step{{Role: "reviewer", Phase: "wait", UpdatedAt: at}}}
	}
	for _, tc := range []struct {
		now, at, want string
	}{
		{"2026-10-29T16:42:00Z", "2026-10-29T09:05:00Z", "waiting since 09:05"},
		{"2026-10-29T16:42:00Z", "2026-10-27T16:42:00Z", "waiting since Oct 27 16:42"},
		// Across midnight: 23 minutes ago, but the day before.
		{"2026-10-29T00:10:00Z", "2026-10-28T23:47:00Z", "waiting since Oct 28 23:47"},
		{"2026-10-29T00:10:00Z", "2026-10-29T00:05:00Z", "waiting since 00:05"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		h.m.cfg.Now = func() time.Time { return now }
		if got, _ := h.m.lastHeard(step(tc.at)); got != tc.want {
			t.Errorf("now %s, wait %s: got %q, want %q", tc.now, tc.at, got, tc.want)
		}
	}
	// The day is the clock's day, not UTC's.
	zone := time.FixedZone("EST", -5*3600)
	now, _ := time.Parse(time.RFC3339, "2026-10-29T03:00:00Z")
	h.m.cfg.Now = func() time.Time { return now.In(zone) }
	// 20:00Z is 15:00 on the clock's day (Oct 28), though UTC's day differs.
	if got, _ := h.m.lastHeard(step("2026-10-28T20:00:00Z")); got != "waiting since 15:00" {
		t.Errorf("zone day: %q", got)
	}
	if got, _ := h.m.lastHeard(step("2026-10-28T02:00:00Z")); got != "waiting since Oct 27 21:00" {
		t.Errorf("zone day before: %q", got)
	}
}

func TestWaitingSinceIsFalseForARunThatIsNotWaiting(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	steps := []slicercli.Step{{Role: "reviewer", Phase: "wait", WaitingOn: "owner", UpdatedAt: "2026-10-19T10:20:00Z"}}
	for _, state := range []string{"stopped", "committed", "ended"} {
		r := slicercli.RelayRun{State: state, UpdatedAt: "2026-10-19T15:42:00Z", Steps: steps}
		if _, ok := waitingSince(r); ok {
			t.Errorf("%s run with a leftover wait step is not waiting", state)
		}
		if text, _ := h.m.lastHeard(r); text != "heard 1h00m" {
			t.Errorf("%s: %q", state, text)
		}
	}
	if _, ok := waitingSince(slicercli.RelayRun{State: "active", Steps: steps}); !ok {
		t.Error("an active run with a wait step is waiting")
	}
}

func TestWaitingSinceStaysTogetherInTheDetail(t *testing.T) {
	h := ledgerWaitHarness(t)
	// At every width that holds the phrase, the detail line breaks between
	// cells, never inside "waiting since 00:02".
	for w := 30; w <= 100; w++ {
		var lines []string
		for _, l := range h.m.overview(w).detail {
			lines = append(lines, strings.TrimPrefix(l, indent))
		}
		if text := strings.Join(lines, "\n"); !strings.Contains(text, "waiting since 00:02") {
			t.Fatalf("width %d breaks waiting since:\n%s", w, text)
		}
	}
}
