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

func progressSteps() []runTreeStep {
	return ledgerTreeSteps([]slicercli.Step{{Role: "implementer", Phase: "done"}, {Role: "reviewer", Phase: "start"}})
}
func TestStageBarFillsFinishedRoles(t *testing.T) {
	steps := progressSteps()
	if got := stageBar(steps, 0, true); got != "[##~~..]" {
		t.Fatal(got)
	}
	steps = append([]runTreeStep{{Step: slicercli.Step{Role: "architect", Phase: "done"}}}, steps...)
	if got := stageBar(steps, 0, true); got != "[####~~..]" {
		t.Fatal(got)
	}
	steps = append(steps, runTreeStep{Step: slicercli.Step{Role: "gate", Phase: "fail"}})
	if got := stageBar(steps, 0, true); got != "[####~~!!]" {
		t.Fatal(got)
	}
	steps = append(steps, runTreeStep{Step: slicercli.Step{Role: "implementer", Phase: "start"}, round: 2})
	if got := stageBar(steps, 0, true); got != "[..~~....] r2" {
		t.Fatal(got)
	}
	stopped := ledgerTreeSteps([]slicercli.Step{{Role: "implementer", Phase: "done"}, {Role: "reviewer", Phase: "done"}, {Role: "gate", Phase: "fail"}})
	if got := stageBar(stopped, 2, false); got != "[####!!]" {
		t.Fatal(got)
	}
	h := newHarness(t)
	if got := h.m.pipeline(steps, false, true); got != "architect>[implementer]>reviewer>gate" {
		t.Fatal(got)
	}
	h = treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	h.m.pane = nil
	_, rows := h.m.rows()
	full := rowText(rows[0], 300)
	if !strings.Contains(full, "[##~~..]") {
		t.Fatal(full)
	}
	for width := len(rows[0].id) + 2; width < 300; width++ {
		line := rowText(rows[0], width)
		if strings.Contains(line, "implementer>") && !strings.Contains(line, "[##~~..]") {
			t.Fatalf("pipeline survived bar at %d: %s", width, line)
		}
	}
}
func TestRunningCellPulsesWithTheTicker(t *testing.T) {
	h := newHarness(t)
	for i, want := range []string{"[##~~..]", "[##~=..]", "[##==..]", "[##=~..]"} {
		if i > 0 {
			h.m.Update(frameMsg{})
		}
		if got := stageBar(progressSteps(), h.m.treeFrame, true); got != want {
			t.Fatal(got)
		}
		if got := stageBar(progressSteps(), h.m.treeFrame, false); got != "[##~~..]" {
			t.Fatal(got)
		}
	}
	before := h.m.treeFrame
	h.m.Update(tickMsg{})
	if h.m.treeFrame != before {
		t.Fatal("schedule tick advanced animation")
	}
	live := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	first := live.m.boardLines(live.m.paneSteps(), 120)[0]
	live.m.Update(frameMsg{})
	_, rows := live.m.rows()
	if got := rowText(rows[0], 300); !strings.Contains(got, "[##~=..]") {
		t.Fatalf("overview pulse: %s", got)
	}
	next := live.m.boardLines(live.m.paneSteps(), 120)[0]
	if next == first || sgr.ReplaceAllString(next, "") != "[##~=..]" {
		t.Fatalf("pane pulse: %q -> %q", first, next)
	}
	live.m.byName["ork"].snap.Runs.Value[0].State = "stopped"
	frozen := live.m.boardLines(live.m.paneSteps(), 120)[0]
	live.m.Update(frameMsg{})
	if got := live.m.boardLines(live.m.paneSteps(), 120)[0]; got != frozen || sgr.ReplaceAllString(got, "") != "[##~~..]" {
		t.Fatalf("stopped pane pulse: %q -> %q", frozen, got)
	}
}
func TestTickerRunsOnlyWhileARunIsVisible(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	armed := 0
	h.m.cfg.FrameAfter = func(d time.Duration) tea.Cmd {
		if d != 250*time.Millisecond {
			t.Fatal(d)
		}
		armed++
		return func() tea.Msg { return frameMsg{} }
	}
	h.start()
	if armed != 0 {
		t.Fatal("idle timer")
	}
	s := snap(proj("ork", false), nil, nil, slicercli.RelayRun{ID: "live", State: "active"})
	h.m.Update(snapshotMsg{name: "ork", snap: s})
	if armed != 1 {
		t.Fatalf("arm %d", armed)
	}
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if armed != 1 {
		t.Fatal("duplicate timer")
	}
	h.m.Update(frameMsg{})
	if armed != 2 {
		t.Fatal("did not rearm")
	}
	h.m.view = roadmapView
	h.m.Update(frameMsg{})
	if armed != 2 {
		t.Fatal("offscreen timer")
	}
	h.m.pane = &runPane{project: "ork", run: "live", ledger: true}
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if armed != 3 {
		t.Fatal("live pane did not arm")
	}
	h.m.pane = nil
	h.m.Update(frameMsg{})
	if armed != 3 {
		t.Fatal("closed pane timer")
	}
	h.m.view = 0
	s.Runs.Value[0].State = "stale"
	h.m.Update(snapshotMsg{name: "ork", snap: s})
	if armed != 3 {
		t.Fatal("stale timer")
	}
	engineOnly := snap(proj("ork", false), nil, nil)
	engineOnly.EngineRuns = []source.EngineRun{{ID: "engine-only", Item: "S07", State: "implement"}}
	h.m.Update(snapshotMsg{name: "ork", snap: engineOnly})
	if armed != 4 {
		t.Fatalf("engine-only arm %d", armed)
	}
	h.m.Update(frameMsg{})
	if armed != 5 {
		t.Fatal("engine-only did not rearm")
	}
	engineOnly.EngineRuns[0].State = "stopped"
	h.m.Update(snapshotMsg{name: "ork", snap: engineOnly})
	h.m.Update(frameMsg{})
	if armed != 5 || h.m.frameArmed {
		t.Fatal("ended engine-only timer")
	}
}

func TestPaneBarUsesNormalWeightOnlyWhileLive(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
		h.m.cfg.Theme = NewTheme(mono)
		if got := h.m.boardLines(h.m.paneSteps(), 120)[0]; got != "[##~~..]" {
			t.Errorf("live bar styled: %q", got)
		}
		h.m.byName["ork"].snap.Runs.Value[0].State = "stopped"
		if got, want := h.m.boardLines(h.m.paneSteps(), 120)[0], h.m.cfg.Theme.Dim.Render("[##~~..]"); got != want {
			t.Errorf("frozen bar: %q, want %q", got, want)
		}
	}
}
func TestSparklineDrawsTheBuckets(t *testing.T) {
	for _, tc := range []struct {
		counts []int
		ascii  bool
		want   string
	}{
		{[]int{0, 1, 2, 3, 4, 5, 6, 7}, false, "▁▂▃▄▅▆▇█"},
		{make([]int, 10), false, "▁▁▁▁▁▁▁▁▁▁"},
		{[]int{0, 1, 2, 3, 4, 5}, true, "._-=+#"},
		{[]int{0, 10, 0}, false, "▁█▁"},
		{[]int{0, 3, 40}, false, "▁▂█"},
		{[]int{0, 3, 40}, true, "._#"},
	} {
		if got := sparkline(tc.counts, tc.ascii); got != tc.want {
			t.Fatalf("%v: %q", tc.counts, got)
		}
	}
}
func TestStalledRoleShowsAFlatLine(t *testing.T) {
	h := treeHarness(t, nil, treeEngine)
	r := &h.m.byName["ork"].snap.EngineRuns[0]
	r.Activity = make([]int, 10)
	for _, state := range []string{"implement", "paused", "stopped", "stale", "committed"} {
		r.State = state
		v := treePaneView(h.m, 100, 60)
		want := state == "implement"
		if strings.Contains(v, "activity ▁▁▁▁▁▁▁▁▁▁  last 5m") != want {
			t.Fatalf("%s: %s", state, v)
		}
	}
	r.State = "implement"
	r.Activity = nil
	if v := treePaneView(h.m, 100, 60); strings.Contains(v, "activity ") {
		t.Fatal(v)
	}
}

const liveDetailEngine = `{"id":"engine","item":"S07","state":"implement","roles":[
 {"role":"implementer","round":1,"vendor":"claude","model":"sonnet","effort":"high","started_at":"2026-10-19T16:30:00Z","ended_at":"2026-10-19T16:35:00Z","exit":"ok"},
 {"role":"reviewer","round":1,"vendor":"claude","model":"opus","started_at":"2026-10-19T16:35:00Z","ended_at":"2026-10-19T16:38:00Z","exit":"ok"},
 {"role":"gate","round":1,"vendor":"claude","model":"opus","started_at":"2026-10-19T16:38:00Z","ended_at":"2026-10-19T16:39:00Z","exit":"error"},
 {"role":"implementer","round":2,"vendor":"claude","model":"sonnet","effort":"high","started_at":"2026-10-19T16:40:00Z"}
]}`

func TestLiveDetailGoldens(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		for _, mode := range []string{"color", "mono", "ascii"} {
			if mode == "ascii" && size[0] != 100 {
				continue
			}
			t.Run(fmt.Sprintf("%v/%s", size, mode), func(t *testing.T) {
				h := treeHarness(t, nil, liveDetailEngine)
				h.m.cfg.Theme = NewTheme(mode != "color")
				h.m.cfg.ASCII = mode == "ascii"
				h.m.byName["ork"].snap.EngineRuns[0].Activity = []int{0, 3, 10, 15, 20, 25, 30, 40, 3, 0}
				bar := stageBar(h.m.paneSteps(), 0, true)
				if bar != "[~~....] r2" {
					t.Errorf("fix-round fixture bar: %s", bar)
				}
				if got := h.m.pipeline(h.m.paneSteps(), false, true); got != "[implementer]>reviewer>gate" {
					t.Errorf("fix-round fixture pipeline: %s", got)
				}
				if got := h.m.pipelineLine(h.m.paneSteps(), false, 120); !strings.Contains(got, "round 2") {
					t.Errorf("fix-round fixture round: %s", got)
				}
				h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				checkGolden(t, fmt.Sprintf("live-detail-%dx%d-%s.txt", size[0], size[1], mode), h.m.View().Content)
			})
		}
	}
}
