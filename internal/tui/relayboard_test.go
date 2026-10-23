// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestPipelineLineMarksTheRunningRole(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	for _, mono := range []bool{false, true} {
		h.m.cfg.Theme = NewTheme(mono)
		v := treePaneView(h.m, 120, 60)
		if !strings.Contains(v, "implementer > [reviewer] > gate") {
			t.Fatal(v)
		}
		steps := append([]runTreeStep{{Step: slicercli.Step{Role: "architect", Phase: "done"}}}, h.m.paneSteps()...)
		line := h.m.pipelineLine(steps, false, 120)
		if !strings.HasPrefix(sgr.ReplaceAllString(line, ""), "architect > implementer") {
			t.Fatal(line)
		}
	}
}

func TestRoundIsTheNumberOfImplementerSteps(t *testing.T) {
	h := treeHarness(t, nil, treeEngine)
	steps := append(h.m.paneSteps(), runTreeStep{Step: slicercli.Step{Role: "implementer", Phase: "start"}, round: 9})
	if line := sgr.ReplaceAllString(h.m.pipelineLine(steps, false, 120), ""); !strings.Contains(line, "round 2") {
		t.Fatal(line)
	}
}

func TestLastHeardWarnsPastHalfTheStaleCutoffAndErrorsWhenStale(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	for _, tc := range []struct {
		age        time.Duration
		state, cue string
	}{{14 * time.Minute, "active", ""}, {15 * time.Minute, "active", ""}, {16 * time.Minute, "active", "! "}, {30 * time.Minute, "stale", "!! "}} {
		r := h.m.byName["ork"].snap.Runs.Value[0]
		r.State = tc.state
		r.UpdatedAt = h.now.Add(-tc.age).Format(time.RFC3339)
		text, style := h.m.lastHeard(r)
		if !strings.HasPrefix(text, tc.cue+"heard ") {
			t.Fatal(text)
		}
		want := h.m.cfg.Theme.Dim
		if tc.cue == "! " {
			want = h.m.cfg.Theme.Warn
		}
		if tc.cue == "!! " {
			want = h.m.cfg.Theme.Error
		}
		if style.Render("x") != want.Render("x") {
			t.Fatal("wrong last heard style")
		}
	}
}

func TestWaitingRolesShowAsWaiting(t *testing.T) {
	h := treeHarness(t, []slicercli.RelayRun{{ID: "one", State: "active", Steps: []slicercli.Step{{Role: "implementer", Phase: "start"}}}}, "")
	v := treePaneView(h.m, 120, 60)
	for _, want := range []string{"reviewer    waiting", "gate        waiting"} {
		if !strings.Contains(v, want) {
			t.Fatal(v)
		}
	}
}

func TestRepeatedRolesShowTheirCountAndTheGateFailLine(t *testing.T) {
	h := treeHarness(t, []slicercli.RelayRun{{ID: "one", State: "active", Steps: []slicercli.Step{{Role: "implementer", Phase: "done"}, {Role: "gate", Phase: "fail", Task: "mutation stayed green"}, {Role: "implementer", Phase: "start"}}}}, "")
	v := treePaneView(h.m, 120, 60)
	for _, want := range []string{"x2", "✗ gate FAIL -> implementer round 2: mutation stayed green"} {
		if !strings.Contains(v, want) {
			t.Fatal(v)
		}
	}
}

func TestRecentSectionListsRunsEndedWithinTheHour(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	s := snap(proj("ork", false), nil, nil)
	for _, minutes := range []int{20, 61, 5} {
		s.BoardRuns.Value = append(s.BoardRuns.Value, slicercli.RelayRun{ID: elapsed(time.Duration(minutes) * time.Minute), Items: []string{"S97"}, State: "ended", Outcome: "pass", BeganAt: h.now.Add(-time.Duration(minutes+10) * time.Minute).Format(time.RFC3339), UpdatedAt: h.now.Add(-time.Duration(minutes) * time.Minute).Format(time.RFC3339), Steps: []slicercli.Step{{Role: "implementer"}, {Role: "reviewer"}, {Role: "gate"}}})
	}
	h.src.set(s)
	h.start()
	rows := h.m.recentRows()
	if len(rows) != 2 || rows[0].ledgerID != "5m" {
		t.Fatalf("%+v", rows)
	}
	if !strings.Contains(rows[0].text, "✓ pass  S97  10m  ended 5m ago  impl 1  review 1  gate 1") {
		t.Fatal(rows[0].text)
	}
	h.m.openPane(rows[0])
	if _, _, ok := h.m.paneRow(); !ok || len(h.m.paneSteps()) != 3 {
		t.Fatal("ended ledger pane missing")
	}
	h.now = h.now.Add(time.Hour)
	if len(h.m.recentRows()) != 0 {
		t.Fatal("render clock not applied")
	}
}

func TestNarrowRowsDropCellsFromTheRightWhole(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	if line := sgr.ReplaceAllString(h.m.pipelineLine(h.m.paneSteps(), false, 35), ""); strings.Contains(line, "round") || strings.Contains(line, "...") {
		t.Fatal(line)
	}
	h.m.byName["ork"].snap.Runs.Value[0].UpdatedAt = h.now.Format(time.RFC3339)
	_, rows := h.m.rows()
	r := rows[0]
	full := rowText(r, 200)
	if !strings.Contains(full, "heard") {
		t.Fatal(full)
	}
	for width := 30; width < 80; width++ {
		v := rowText(r, width)
		if strings.Contains(v, "hea") && !strings.Contains(v, "heard 0s") {
			t.Fatalf("partial heard at %d: %s", width, v)
		}
	}
}

func TestStaleRunFreezesAtLastEvent(t *testing.T) {
	r := slicercli.RelayRun{ID: "stale", State: "stale", Items: []string{"S97"}, BeganAt: t0.Add(-time.Hour).Format(time.RFC3339), UpdatedAt: t0.Add(-30 * time.Minute).Format(time.RFC3339), Steps: []slicercli.Step{{Role: "implementer", Phase: "start", BeganAt: t0.Add(-time.Hour).Format(time.RFC3339)}}}
	h := newHarness(t, proj("ork", false))
	s := snap(proj("ork", false), nil, nil)
	s.BoardRuns.Value = []slicercli.RelayRun{r}
	h.src.set(s)
	h.start()
	_, rows := h.m.rows()
	if len(rows) != 1 || !strings.Contains(rows[0].text, "stale  30m") {
		t.Fatalf("%+v", rows)
	}
	h.m.openPane(rows[0])
	v := treePaneView(h.m, 120, 60)
	if !strings.Contains(v, "~ implementer stale 30m") || !strings.Contains(v, "!! last heard 30m ago") {
		t.Fatal(v)
	}
	h.now = h.now.Add(time.Hour)
	v = treePaneView(h.m, 120, 60)
	if !strings.Contains(v, "~ implementer stale 30m") || !strings.Contains(v, "!! last heard 1h30m ago") {
		t.Fatal(v)
	}
}

func TestRelayBoardGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, size := range [][2]int{{100, 30}, {80, 24}, {120, 40}} {
			h := newHarness(t, proj("ork", false))
			h.m.cfg.Theme = NewTheme(mono)
			active := slicercli.RelayRun{ID: "s97", Items: []string{"S97"}, State: "active", BeganAt: h.now.Add(-20 * time.Minute).Format(time.RFC3339), UpdatedAt: h.now.Add(-16 * time.Minute).Format(time.RFC3339), Steps: []slicercli.Step{{Role: "implementer", Phase: "done"}, {Role: "reviewer", Phase: "start"}}}
			s := snap(proj("ork", false), nil, nil, active)
			for i, outcome := range []string{"pass", "fail", "stopped"} {
				r := active
				r.ID = outcome
				r.State = "ended"
				r.Outcome = outcome
				r.Items = []string{fmt.Sprintf("S%d", 93+i)}
				r.UpdatedAt = h.now.Add(-time.Duration([]int{20, 5, 35}[i]) * time.Minute).Format(time.RFC3339)
				r.BeganAt = h.now.Add(-time.Duration([]int{30, 15, 45}[i]) * time.Minute).Format(time.RFC3339)
				s.BoardRuns.Value = append(s.BoardRuns.Value, r)
			}
			if size[0] == 120 {
				h.m.sidebarHidden = true
				h.m.cursor = 2
				stale := active
				stale.ID = "stale"
				stale.State = "stale"
				stale.BeganAt = h.now.Add(-50 * time.Minute).Format(time.RFC3339)
				stale.UpdatedAt = h.now.Add(-30 * time.Minute).Format(time.RFC3339)
				s.BoardRuns.Value = append(s.BoardRuns.Value, stale)
			}
			h.src.set(s)
			h.start()
			h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			checkGolden(t, fmt.Sprintf("board-%dx%d-mono-%t.txt", size[0], size[1], mono), h.m.View().Content)
			h.m.openPane(ovRow{ledgerID: "s97", name: "ork"})
			checkGolden(t, fmt.Sprintf("board-pane-%dx%d-mono-%t.txt", size[0], size[1], mono), h.m.View().Content)
		}
	}
}

func TestPipelineRoleStyles(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h := newHarness(t, proj("ork", false))
		h.m.cfg.Theme = NewTheme(mono)
		th := h.m.cfg.Theme
		steps := ledgerTreeSteps([]slicercli.Step{{Role: "architect", Phase: "done"}, {Role: "implementer", Phase: "start"}, {Role: "reviewer", Phase: "fail"}})
		want := th.Passed.Render("architect") + " > " + th.Running.Render("[implementer]") + " > " + th.Error.Render("reviewer") + " > " + th.Dim.Render("gate")
		if got := h.m.pipeline(steps, false, false); got != want {
			t.Fatalf("%q != %q", got, want)
		}
		want = th.Dim.Render("architect") + " > " + th.Dim.Render("implementer") + " > " + th.Dim.Render("reviewer") + " > " + th.Dim.Render("gate")
		if got := h.m.pipeline(steps, true, false); got != want {
			t.Fatalf("stale: %q != %q", got, want)
		}
	}
}
