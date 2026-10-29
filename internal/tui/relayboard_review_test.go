// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestGateFailureOmitsEmptyReason(t *testing.T) {
	h := treeHarness(t, nil, treeEngine)
	v := treePaneView(h.m, 120, 60)
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "gate FAIL") {
			if strings.TrimSpace(line) != "✗ gate FAIL -> implementer round 2" {
				t.Fatalf("empty gate reason: %q", line)
			}
			return
		}
	}
	t.Fatal("missing gate failure")
}

func TestQueuedRoleHasNoDuplicateWaitingRow(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	v := treePaneView(h.m, 120, 60)
	if strings.Contains(v, "waiting") {
		t.Fatalf("queued gate duplicated:\n%s", v)
	}
}

func TestPipelineOmitsZeroRound(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	if line := h.m.pipelineLine(nil, false, 120); strings.Contains(line, "round") {
		t.Fatalf("zero round: %q", line)
	}
	for _, line := range h.m.runTreeLines(ledgerTreeSteps([]slicercli.Step{{Role: "reviewer"}}), 120, 0) {
		if strings.Contains(line, "round 0") {
			t.Fatalf("zero node round: %q", line)
		}
	}
}

func TestInactiveBoardDetails(t *testing.T) {
	for _, state := range []string{"stopped", "paused", "committed", "ended"} {
		t.Run(state, func(t *testing.T) {
			h := treeHarness(t, nil, treeEngine)
			h.m.cfg.Theme = NewTheme(false)
			if state == "ended" {
				r := slicercli.RelayRun{ID: "ended", State: state, UpdatedAt: h.now.Add(-time.Hour).Format(time.RFC3339), Steps: []slicercli.Step{{Role: "implementer", Phase: "done"}}}
				h.m.byName["ork"].snap.BoardRuns.Value = []slicercli.RelayRun{r}
				h.m.openPane(ovRow{name: "ork", ledgerID: "ended"})
			} else {
				r := &h.m.byName["ork"].snap.EngineRuns[0]
				r.State = state
				r.Roles = r.Roles[:1]
			}
			v := treePaneView(h.m, 120, 60)
			// The paused cell says "waiting since"; a waiting row would not.
			if strings.Contains(strings.ReplaceAll(v, "waiting since", ""), "waiting") {
				t.Errorf("inactive waiting rows:\n%s", v)
			}
			steps := h.m.paneSteps()
			line := h.m.boardLines(steps, 120)[1]
			th := h.m.cfg.Theme
			want := th.Dim.Render("implementer") + " > " + th.Dim.Render("reviewer") + " > " + th.Dim.Render("gate")
			if !strings.HasPrefix(line, want) {
				t.Errorf("inactive pipeline not dim: %q", line)
			}
			r := slicercli.RelayRun{State: state, UpdatedAt: h.now.Add(-time.Hour).Format(time.RFC3339)}
			// An owner wait names when it began, which does not age (S154).
			wantFirst, wantSecond := "heard 1h00m", "heard 2h00m"
			if state == "paused" {
				wantFirst, wantSecond = "waiting since 15:42", "waiting since 15:42"
			}
			first, style := h.m.lastHeard(r)
			if first != wantFirst || strings.Contains(first, "!") || style.Render("x") != h.m.cfg.Theme.Dim.Render("x") {
				t.Errorf("inactive heard age/style: got %q, want %q dim", first, wantFirst)
			}
			h.now = h.now.Add(time.Hour)
			second, secondStyle := h.m.lastHeard(r)
			if second != wantSecond || strings.Contains(second, "!") || secondStyle.Render("x") != h.m.cfg.Theme.Dim.Render("x") {
				t.Errorf("inactive heard age/style: got %q, want %q dim", second, wantSecond)
			}
		})
	}
}

func TestStaleCompactPipelineHasNoRunningBrackets(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	r := slicercli.RelayRun{ID: "stale", State: "stale", Steps: []slicercli.Step{{Role: "implementer", Phase: "start"}}}
	h.src.set(snap(proj("ork", false), nil, nil, r))
	h.start()
	_, rows := h.m.rows()
	if v := rowText(rows[0], 200); strings.Contains(v, "[implementer]") {
		t.Fatalf("stale brackets: %s", v)
	}
}

func TestRunningRowWarnCellAndFullDetail(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.m.cfg.Theme = NewTheme(false)
	r := slicercli.RelayRun{ID: "s97", Items: []string{"S97"}, State: "active", BeganAt: h.now.Add(-20 * time.Minute).Format(time.RFC3339), UpdatedAt: h.now.Add(-16 * time.Minute).Format(time.RFC3339), Steps: []slicercli.Step{{Role: "reviewer", Phase: "start"}}}
	h.src.set(snap(proj("ork", false), nil, nil, r))
	h.start()
	_, rows := h.m.rows()
	row := rows[0]
	v := h.m.rowLine(row, false, 3, 240)
	if !strings.Contains(v, h.m.cfg.Theme.Warn.Render("! heard 16m")) {
		t.Errorf("warn cell lacks color: %q", v)
	}
	want := "s97  S97  active  20m  [..~~..]  ! heard 16m  running: reviewer  implementer>[reviewer]>gate"
	if got := strings.TrimSpace(strings.Join(detailLines(row, 240, "|"), "\n")); got != want {
		t.Errorf("incomplete detail: %q", got)
	}
	// The ! cue outlasts the bar at a width that holds only one of them (S106).
	if narrow := rowText(row, 20); !strings.Contains(narrow, "! heard 16m") || strings.Contains(narrow, "[..~~..]") {
		t.Fatalf("the cue must stay and the bar go at a narrow width: %q", narrow)
	}
	if strings.Contains(rowText(row, 12), "heard") {
		t.Fatal("fixture does not hide heard at a very narrow width")
	}
}

func TestQueuedRowsShareTreePhaseColumn(t *testing.T) {
	h := treeHarness(t, []slicercli.RelayRun{{ID: "live", State: "active", Steps: []slicercli.Step{{Role: "implementer", Phase: "start"}}}}, "")
	v := treePaneView(h.m, 120, 60)
	column := -1
	waiting := 0
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "| implementer") {
			column = strings.Index(line, "start")
		}
		if strings.Contains(line, "queued") {
			waiting++
			if got := strings.Index(line, "queued"); got != column {
				t.Errorf("phase column %d, want %d: %q", got, column, line)
			}
		}
	}
	if waiting != 2 {
		t.Fatalf("queued rows %d", waiting)
	}
}

func TestMonochromeWarnIsPlain(t *testing.T) {
	if got := NewTheme(true).Warn.Render("! heard 16m"); got != "! heard 16m" {
		t.Fatalf("mono warning styled: %q", got)
	}
}

func TestRecentRowHasNoEmptyKindSpan(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.m.cfg.Theme = NewTheme(false)
	row := ovRow{name: "ork", style: h.m.cfg.Theme.Dim, cells: []string{"✓ pass", "S97"}}
	if v := h.m.rowLine(row, false, 3, 100); strings.Contains(v, "\x1b[90m\x1b[m") {
		t.Fatalf("empty kind span: %q", v)
	}
}

func TestNarrowRunningCellsDropInPriorityOrder(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	r := slicercli.RelayRun{ID: "r", Items: []string{"S97"}, State: "active", UpdatedAt: h.now.Format(time.RFC3339), Steps: []slicercli.Step{{Role: "implementer", Phase: "start"}}}
	h.src.set(snap(proj("ork", false), nil, nil, r))
	h.start()
	_, rows := h.m.rows()
	row := rows[0]
	// Line 1 drops, from the right: tokens (an engine run's), then the title and
	// the model (this run has neither), then last heard (no cue here), the role
	// and the bar. The slice id stays.
	base := "S97"
	role := "implementer"
	heard := "heard 0s"
	withBar := base + "  [~~....]"
	withRole := base + "  " + role + "  [~~....]"
	texts := []string{base, withBar, withRole, withRole + "  " + heard}
	for i, want := range texts {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if got := rowText(row, len(want)); got != want {
				t.Fatalf("drop order: got %q, want %q", got, want)
			}
		})
	}
}

func TestRecentEnterOpensReadOnlyEndedPane(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	s := snap(proj("ork", false), nil, nil)
	s.BoardRuns.Value = []slicercli.RelayRun{{ID: "ended", Items: []string{"S97"}, State: "ended", Outcome: "other", UpdatedAt: h.now.Format(time.RFC3339), Steps: []slicercli.Step{{Role: "implementer", Phase: "done"}}}}
	h.src.set(s)
	h.start()
	h.deliver(press("enter"))
	if h.m.pane == nil || !h.m.pane.ledger || h.m.pane.run != "ended" {
		t.Fatalf("Recent Enter missed ended pane: %+v", h.m.pane)
	}
	if _, _, ok := h.m.paneRow(); !ok {
		t.Fatal("ended pane missing")
	}
	for _, key := range []string{"a", "x", "t"} {
		h.m.actionStatus = "unchanged"
		if _, cmd := h.m.Update(press(key)); cmd != nil || h.m.actionStatus != "unchanged" || h.m.confirm != nil || len(h.m.actionFlights) != 0 {
			t.Fatalf("read-only key %s returned action", key)
		}
	}
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	if hint := hintLine(plainView(h.m)); !strings.Contains(hint, "read-only") {
		t.Fatal(hint)
	}
}

func TestEngineRunningDetailIncludesBoardCells(t *testing.T) {
	h := treeHarness(t, nil, treeEngine)
	_, rows := h.m.rows()
	if got := strings.Join(detailLines(rows[0], 240, "|"), "\n"); !strings.Contains(got, "heard 1m  implementer>[reviewer]>gate") {
		t.Fatalf("engine detail missing board cells: %q", got)
	}
}

func TestWarningColorTargetsOnlyTheHeardCell(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.m.cfg.Theme = NewTheme(false)
	task := "! heard 99m is task text"
	r := slicercli.RelayRun{ID: "r", State: "active", UpdatedAt: h.now.Add(-16 * time.Minute).Format(time.RFC3339), Steps: []slicercli.Step{{Role: "implementer", Phase: "start", Task: task}}}
	h.src.set(snap(proj("ork", false), nil, nil, r))
	h.start()
	_, rows := h.m.rows()
	v := h.m.rowLine(rows[0], false, 3, 240)
	if !strings.Contains(v, h.m.cfg.Theme.Warn.Render("! heard 16m")) {
		t.Fatalf("the heard cell lacks the warning style: %q", v)
	}
	// The task, which looks like a heard cell, is line 2. The row is selected,
	// so the line is reversed whole and carries no warning style.
	body := h.m.overview(240).body
	i := rowIndex(body, "! heard 16m")
	if i < 0 || i+1 >= len(body) || !strings.Contains(sgr.ReplaceAllString(body[i+1], ""), task) {
		t.Fatalf("the task must be line 2: %q", body)
	}
	if strings.Contains(body[i+1], h.m.cfg.Theme.Warn.Render("! heard 99m")) || strings.Contains(body[i+1], h.m.cfg.Theme.Warn.Render(task)) {
		t.Fatalf("the warning style escaped to line 2: %q", body[i+1])
	}
}

func TestRecentEndedPaneShowsActualLastHeardAge(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.m.cfg.Theme = NewTheme(false)
	s := snap(proj("ork", false), nil, nil)
	s.BoardRuns.Value = []slicercli.RelayRun{{ID: "ended", Items: []string{"S97"}, State: "ended", Outcome: "pass", BeganAt: h.now.Add(-time.Hour).Format(time.RFC3339), UpdatedAt: h.now.Add(-50 * time.Minute).Format(time.RFC3339)}}
	h.src.set(s)
	h.start()
	h.deliver(press("enter"))
	if h.m.pane == nil || !h.m.pane.ledger || h.m.pane.run != "ended" {
		t.Fatalf("Recent Enter did not open ended pane: %+v", h.m.pane)
	}
	// Last heard sits on the bar's line, dim for an ended run.
	want := indent + h.m.cfg.Theme.Dim.Render("[......]") + "  " + h.m.cfg.Theme.Dim.Render("last heard 50m ago")
	lines := h.m.paneLines(120, 60, "")
	found := false
	for _, line := range lines {
		if strings.Contains(sgr.ReplaceAllString(line, ""), "last heard") {
			found = true
			if line != want {
				t.Fatalf("ended pane last heard: got %q, want %q dim", sgr.ReplaceAllString(line, ""), sgr.ReplaceAllString(want, ""))
			}
		}
	}
	if !found {
		t.Fatal("ended pane omitted last heard")
	}
}
