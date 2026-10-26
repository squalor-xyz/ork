// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"fmt"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"strings"
	"testing"
)

func TestPaneShowsTheTailAndTokens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h := treeHarness(t, nil, liveDetailEngine)
		h.m.cfg.Theme = NewTheme(mono)
		r := &h.m.byName["ork"].snap.EngineRuns[0]
		r.Tail = "discarded\nfirst\n" + strings.Repeat("界", 80) + "\nlast"
		r.Tokens = 1734
		r.Roles[0].Tokens = 500
		r.Roles[0].CostUSD = .25
		for _, state := range []string{"implement", "stopped", "committed", "paused", "ended", "stale"} {
			r.State = state
			v := treePaneView(h.m, 80, 60)
			if strings.Contains(v, "\n      > last") != (state == "implement") {
				t.Fatalf("tail visibility %s: %s", state, v)
			}
			if strings.Contains(v, "discarded") {
				t.Fatal(v)
			}
			if state == "implement" && (!strings.Contains(v, "tokens 1234  $-") || !strings.Contains(v, "...")) {
				t.Fatal(v)
			}
			if !strings.Contains(v, "tokens 500  $0.25") {
				t.Fatal(v)
			}
			for _, line := range strings.Split(v, "\n") {
				if lipgloss.Width(line) > 80 {
					t.Fatalf("overflow: %q", line)
				}
			}
		}
	}
}

func TestRunningRowShowsTheTokenTotal(t *testing.T) {
	h := treeHarness(t, nil, liveDetailEngine)
	h.m.byName["ork"].snap.EngineRuns[0].Tokens = 1234
	_, rows := h.m.rows()
	r := rows[0]
	if !strings.Contains(rowText(r, 400), "tokens 1234") {
		t.Fatal(rowText(r, 400))
	}
	saw := false
	for w := 20; w < 400; w++ {
		v := rowText(r, w)
		if strings.Contains(v, "tokens 1234") && !strings.Contains(v, "heard ") {
			t.Fatalf("tokens survived heard: %s", v)
		}
		if strings.Contains(v, "heard ") && !strings.Contains(v, "tokens 1234") {
			saw = true
		}
	}
	if !saw {
		t.Fatal("no width dropped tokens before heard")
	}
}

func TestPaneTailGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, width := range []int{80, 100} {
			h := treeHarness(t, nil, liveDetailEngine)
			h.m.cfg.Theme = NewTheme(mono)
			r := &h.m.byName["ork"].snap.EngineRuns[0]
			r.Tail = "Checking rollback paths\n" + strings.Repeat("long output ", 12) + "\nRunning focused tests"
			r.Tokens = 1234
			r.Roles[0].Tokens = 500
			r.Roles[0].CostUSD = .25
			checkGolden(t, fmt.Sprintf("pane-tail-%d-mono-%t.txt", width, mono), strings.Join(h.m.paneLines(width, 32, ""), "\n"))
		}
	}
}

func TestLinkedPaneJoinsLiveUsageByStepID(t *testing.T) {
	h := treeHarness(t, nil, liveDetailEngine)
	p := h.m.byName["ork"]
	r := &p.snap.EngineRuns[0]
	r.RelayRun = "linked"
	r.Roles[0].Step = "old"
	r.Roles[3].Step = "current"
	r.Roles[0].Tokens = 20
	r.Roles[0].CostUSD = .15
	r.Tokens = 62
	r.Tail = "linked-live-tail"
	p.snap.Runs.Value = []slicercli.RelayRun{{ID: "linked", State: "active", Steps: []slicercli.Step{
		{ID: "outer", Role: "orchestrator", Phase: "start"},
		{ID: "old", Parent: "outer", Role: "implementer", Phase: "done"},
		{ID: "subagent", Parent: "current", Role: "implementer", Phase: "start"},
		{ID: "current", Parent: "outer", Role: "implementer", Phase: "start"},
	}}}
	steps := h.m.paneSteps()
	if steps[0].hasUsage || steps[2].hasUsage {
		t.Fatal("usage joined to unrelated ledger node")
	}
	if !steps[1].hasUsage || steps[1].tokens != 20 || steps[3].tokens != 42 || steps[3].tail != "linked-live-tail" {
		t.Fatalf("joined steps: %+v", steps)
	}
	v := treePaneView(h.m, 100, 60)
	if strings.Count(v, "linked-live-tail") != 1 || !strings.Contains(v, "tokens 42  $-") || !strings.Contains(v, "tokens 20  $0.15") {
		t.Fatal(v)
	}
	p.snap.Runs.Value[0].State = "stale"
	if v = treePaneView(h.m, 100, 60); strings.Contains(v, "linked-live-tail") {
		t.Fatal(v)
	}
}

func TestUsagePlacementAndUnknownCost(t *testing.T) {
	h := treeHarness(t, nil, liveDetailEngine)
	r := &h.m.byName["ork"].snap.EngineRuns[0]
	r.Tokens = 1234
	r.Roles[0].Tokens = 500
	r.Roles[0].CostUSD = .25
	r.Roles[1].Vendor = "codex"
	r.Roles[1].Tokens = 200
	lines := h.m.runTreeLines(h.m.paneSteps(), 200, 0)
	plain := func(i int) string { return sgr.ReplaceAllString(lines[i], "") }
	if !strings.Contains(plain(0), "tokens 500  $0.25") {
		t.Fatalf("finished usage not inline: %v", lines)
	}
	if !strings.Contains(plain(1), "tokens 200  $-") {
		t.Fatalf("Codex cost must be unknown: %v", lines)
	}
	gate := -1
	for i, l := range lines {
		if strings.Contains(l, "gate ") && strings.Contains(l, "fail") {
			gate = i
			break
		}
	}
	if gate < 0 || !strings.Contains(plain(gate+1), "gate FAIL ->") {
		t.Fatalf("verdict must follow node: %v", lines)
	}
	wide := plain(0)
	narrow := sgr.ReplaceAllString(h.m.runTreeLines(h.m.paneSteps(), lipgloss.Width(wide)-1, 0)[0], "")
	if strings.Contains(narrow, "tokens") || strings.Contains(narrow, "...") {
		t.Fatalf("usage must drop whole before node clips: %q", narrow)
	}
	r.State = "stopped"
	v := treePaneView(h.m, 200, 60)
	if strings.Contains(v, "\n      tokens") || !strings.Contains(v, "stopped") || !strings.Contains(v, "tokens -  $-") {
		t.Fatalf("interrupted role must be inline and unknown: %s", v)
	}
}

func TestRunningUnknownUsageAndOverviewOmission(t *testing.T) {
	h := treeHarness(t, nil, `{"id":"engine","item":"S99","state":"implement","roles":[{"role":"implementer","vendor":"codex"}]}`)
	v := treePaneView(h.m, 100, 40)
	if !strings.Contains(v, "tokens -  $-") {
		t.Fatal(v)
	}
	_, rows := h.m.rows()
	if strings.Contains(rowText(rows[0], 400), "tokens") {
		t.Fatal(rowText(rows[0], 400))
	}
}

func TestTailWrapGutterBlankLinesAndOrder(t *testing.T) {
	h := treeHarness(t, nil, liveDetailEngine)
	steps := h.m.paneSteps()
	steps = steps[len(steps)-1:]
	steps[0].Task = "task before tail"
	steps[0].tail = "old\n\n" + strings.Repeat("x", 70) + "\n \nnewest\n\n"
	lines := h.m.runTreeLines(steps, 36, 0)
	plain := sgr.ReplaceAllString(strings.Join(lines, "\n"), "")
	task := strings.Index(plain, "task before tail")
	tail := strings.Index(plain, "> ")
	if task < 0 || tail < task {
		t.Fatalf("task must precede tail: %s", plain)
	}
	var rows []string
	for _, line := range strings.Split(plain, "\n") {
		if strings.HasPrefix(line, "    > ") {
			rows = append(rows, line)
		}
	}
	if len(rows) != 3 || rows[2] != "    > newest" || !strings.HasPrefix(rows[0], "    > ...") {
		t.Fatalf("tail rows: %q", rows)
	}
	if strings.Contains(rows[1], "...") || strings.Contains(rows[2], "...") {
		t.Fatal(rows)
	}
	for _, l := range rows {
		if lipgloss.Width(l) > 36 {
			t.Fatal(rows)
		}
	}
	steps[0].tail = "first\n\nsecond\n \nthird\n"
	plain = sgr.ReplaceAllString(strings.Join(h.m.runTreeLines(steps, 36, 0), "\n"), "")
	if !strings.Contains(plain, "    > first\n    > second\n    > third") || strings.Contains(plain[strings.Index(plain, "    > "):], "...") {
		t.Fatal(plain)
	}
}

func TestPaneTailFullFrameKeepsHeadings(t *testing.T) {
	h := treeHarness(t, nil, liveDetailEngine)
	r := &h.m.byName["ork"].snap.EngineRuns[0]
	r.Tokens = 42
	r.Tail = "one\ntwo\nthree"
	h.m.pane.offset = 100
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := h.m.View().Content
	for _, want := range []string{"Needs you 0", "Running 1", "> three"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q: %s", want, v)
		}
	}
	checkGolden(t, "pane-tail-frame-80x24.txt", v)
}

func TestZeroUsageIsKnownAndTailBufferCutIsMarked(t *testing.T) {
	h := treeHarness(t, nil, `{"id":"engine","item":"S99","state":"implement","usage_known":true,"live_usage_known":true,"tail_truncated":true,"tail":"newest","roles":[{"role":"implementer","vendor":"codex"}]}`)
	v := treePaneView(h.m, 100, 40)
	if !strings.Contains(v, "tokens 0  $-") || !strings.Contains(v, "> ... newest") {
		t.Fatal(v)
	}
	_, rows := h.m.rows()
	if !strings.Contains(rowText(rows[0], 400), "tokens 0") {
		t.Fatal(rows)
	}
}

func TestPaneTailASCIIGolden(t *testing.T) {
	h := treeHarness(t, nil, liveDetailEngine)
	h.m.cfg.ASCII = true
	h.m.byName["ork"].snap.EngineRuns[0].Tail = strings.Repeat("wrapped output ", 15) + "\nnewest"
	checkGolden(t, "pane-tail-ascii.txt", strings.Join(h.m.paneLines(80, 32, ""), "\n"))
}
