// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"errors"
	"os"
	osexec "os/exec"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

type slicerTuiCapture struct {
	calls int
	cmd   *osexec.Cmd
	done  tea.ExecCallback
}

func slicerTuiHarness(t *testing.T) (*harness, *slicerTuiCapture) {
	t.Helper()
	h := roadmapHarness(t)
	c := &slicerTuiCapture{}
	h.m.cfg.SlicerBin = os.Args[0]
	h.m.cfg.Exec = func(cmd *osexec.Cmd, done tea.ExecCallback) tea.Cmd {
		c.calls++
		c.cmd = cmd
		c.done = done
		return func() tea.Msg { return nil }
	}
	return h, c
}
func TestOpenSlicerTuiRunsSlicerTuiWithTheProjectRoot(t *testing.T) {
	for _, k := range []string{"O", "shift+o"} {
		t.Run(k, func(t *testing.T) {
			h, c := slicerTuiHarness(t)
			msg := press(k)
			if k == "shift+o" {
				msg = tea.KeyPressMsg{Code: 'o', Mod: tea.ModShift}
			}
			_, cmd := h.m.Update(msg)
			if c.calls != 1 || cmd == nil {
				t.Fatalf("Exec calls=%d command=%v; key must open slicer tui", c.calls, cmd != nil)
			}
			want := []string{os.Args[0], "tui", "--root", h.m.projects[0].reg.Path}
			if !reflect.DeepEqual(c.cmd.Args, want) || c.cmd.Dir != "" || c.cmd.Env != nil {
				t.Fatalf("command=%+v want Args=%v, inherited Dir/Env", c.cmd, want)
			}
			if msg, ok := c.done(nil).(slicerTuiDoneMsg); !ok || msg.project != "ork" || msg.err != nil {
				t.Fatalf("callback=%+v", c.done(nil))
			}
		})
	}
}
func TestOpenSlicerTuiIsRefusedOffTheRoadmap(t *testing.T) {
	for _, tc := range []struct{ state, want string }{
		{"overview", "slicer tui opens from the Roadmap"}, {"waves", "slicer tui opens from the Roadmap"}, {"questions", "slicer tui opens from the Roadmap"}, {"decisions", "slicer tui opens from the Roadmap"},
		{"needs", "Select a project to open slicer tui"}, {"running", "Select a project to open slicer tui"}, {"help", "Close help to open slicer tui"},
		{"item", "Close the pane to open slicer tui"}, {"matrix", ""}, {"setup", "Project unavailable; cannot open slicer tui"}, {"unavailable", "Project unavailable; cannot open slicer tui"}, {"unloaded", "Project unavailable; cannot open slicer tui"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			h, c := slicerTuiHarness(t)
			switch tc.state {
			case "overview":
				h.m.view = 0
			case "waves":
				h.m.view = wavesView
			case "questions":
				h.m.view = questionsView
			case "decisions":
				h.m.view = decisionsView
			case "needs":
				h.m.active = needsEntry
			case "running":
				h.m.active = runningEntry
			case "help":
				h.m.showHelp = true
			case "item":
				h.m.itemPane = &itemPane{id: "S90"}
			case "matrix":
				h.m.itemPane = &itemPane{id: matrixPaneID}
			case "setup":
				h.m.projects[0].snap = noSlicer(h.m.projects[0].reg)
			case "unavailable":
				h.m.projects[0].snap.Unavailable = true
			case "unloaded":
				h.m.projects[0].loaded = false
			}
			_, cmd := h.m.Update(press("O"))
			if c.calls != 0 || cmd != nil || h.m.actionStatus != tc.want {
				t.Fatalf("calls=%d command=%v status=%q want=%q", c.calls, cmd != nil, h.m.actionStatus, tc.want)
			}
			if strings.Contains(wideHint(h), "O slicer tui") {
				t.Fatal("refused action offered in hints")
			}
		})
	}
}
func TestOpenSlicerTuiNeedsNoDaemon(t *testing.T) {
	h, c := slicerTuiHarness(t)
	h.m.cfg.Live = nil
	_, cmd := h.m.Update(press("O"))
	if c.calls != 1 || cmd == nil {
		t.Fatal("no daemon prevented slicer tui")
	}
}
func TestDashboardRefreshesAfterSlicerTuiExits(t *testing.T) { testSlicerTuiReturn(t, nil) }
func TestSlicerTuiExitErrorShowsOnTheStatusLine(t *testing.T) {
	testSlicerTuiReturn(t, errors.New("exit status 9"))
}
func testSlicerTuiReturn(t *testing.T, exitErr error) {
	t.Helper()
	h, _ := slicerTuiHarness(t)
	l := &noteLive{}
	h.m.cfg.Live = l
	h.src.take()
	h.m.actionStatus = "old status"
	h.m.switchTo(1)
	_, cmd := h.m.Update(slicerTuiDoneMsg{project: "ork", err: exitErr})
	if cmd == nil {
		t.Fatal("return did not fetch the original project")
	}
	h.m.Update(cmd())
	want := ""
	if exitErr != nil {
		want = "slicer tui: " + exitErr.Error()
	}
	if calls := h.src.take(); !reflect.DeepEqual(calls, []string{"ork"}) || l.refreshes != 1 || h.m.actionStatus != want {
		t.Fatalf("reads=%v refreshes=%d status=%q want=%q", calls, l.refreshes, h.m.actionStatus, want)
	}
	// An already-running refresh coalesces the next request.
	pending := h.m.fetch("ork", false)
	if pending == nil {
		t.Fatal("expected pending fetch")
	}
	_, cmd = h.m.Update(slicerTuiDoneMsg{project: "ork"})
	if cmd != nil {
		t.Fatal("started a duplicate snapshot read")
	}
}
func TestMissingSlicerShowsErrorAndDoesNotPanic(t *testing.T) {
	h, c := slicerTuiHarness(t)
	h.m.cfg.SlicerBin = "ork-s69-nonexistent-slicer"
	_, cmd := h.m.Update(press("O"))
	if c.calls != 0 || cmd != nil || h.m.actionStatus != "slicer tui: "+h.m.cfg.SlicerBin+" not found on PATH" || len(h.m.actionStatus) > 58 {
		t.Fatalf("calls=%d command=%v status=%q", c.calls, cmd != nil, h.m.actionStatus)
	}
}
func TestHintBarOffersSlicerTuiOnTheRoadmap(t *testing.T) {
	h, _ := slicerTuiHarness(t)
	h.m.cfg.Live = &noteLive{}
	if !strings.Contains(wideHint(h), "O slicer tui") {
		t.Fatal("Roadmap hint omits O slicer tui")
	}
	if hint := wideHint(h); !strings.Contains(hint, "N note; enter open; l launch; O slicer tui; m matrix") {
		t.Fatalf("slicer tui hint must follow row actions before matrix: %s", hint)
	}
	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		hint := hintLine(plainView(h.m))
		if strings.Contains(hint, "O slicer tui") || strings.Contains(hint, "+ add project") || strings.Contains(hint, "n new slice") {
			t.Fatalf("early tier did not drop: %q", hint)
		}
		h.m.Update(press("?"))
		v := plainView(h.m)
		for _, want := range []string{"O slicer tui", "q quit", "close help; q quit"} {
			if !strings.Contains(v, want) {
				t.Errorf("%v help omits %q:\n%s", size, want, v)
			}
		}
		h.m.Update(press("?"))
	}
	for _, e := range h.m.contextHints() {
		if e.b.Help().Key == "O" && e.drop != dropNew {
			t.Fatal("O does not drop with new slice")
		}
	}
	h.m.view = 0
	h.m.Update(press("?"))
	if strings.Contains(h.view(), "O slicer tui") {
		t.Fatal("Overview help offers Roadmap key")
	}
}

func TestSlicerTuiReturnFollowsTheSelectedItemAfterRefresh(t *testing.T) {
	for _, inflight := range []bool{false, true} {
		name := "new refresh"
		if inflight {
			name = "refresh already in flight"
		}
		t.Run(name, func(t *testing.T) {
			h, c := slicerTuiHarness(t)
			h.m.Update(press("O")) // S90 is selected before handing over the terminal.
			snapshot := h.m.projects[0].snap
			items := append([]slicercli.Item(nil), snapshot.Items.Value...)
			items[0].Status = "parked"
			snapshot.Items.Value = []slicercli.Item{items[1], items[2], items[0], items[3]}
			h.src.set(snapshot)
			var pending tea.Cmd
			if inflight {
				pending = h.m.fetch("ork", false)
			}
			_, refresh := h.m.Update(c.done(nil))
			if h.m.roadmapCursor != 0 {
				t.Fatal("restored before refreshed snapshot arrived")
			}
			if inflight {
				if refresh != nil {
					t.Fatal("duplicate read while refresh in flight")
				}
				refresh = pending
			}
			if refresh == nil {
				t.Fatal("no snapshot command")
			}
			h.m.Update(refresh())
			if h.m.roadmapCursor != 2 {
				t.Fatalf("cursor=%d; want selected S90 at row 2", h.m.roadmapCursor)
			}
			// Restoration is consumed once; polling must not pin the selection.
			h.m.roadmapCursor = 1
			next := h.m.fetch("ork", false)
			h.m.Update(next())
			if h.m.roadmapCursor != 1 {
				t.Fatal("later poll restored the old selection again")
			}
		})
	}
}
func TestSlicerTuiReturnKeepsTheOldRowWhenTheItemIsGone(t *testing.T) {
	for _, remaining := range []int{3, 1, 0} {
		t.Run(string(rune('0'+remaining)), func(t *testing.T) {
			h, c := slicerTuiHarness(t)
			h.m.roadmapCursor = 2 // S03 will disappear.
			h.m.Update(press("O"))
			snapshot := h.m.projects[0].snap
			items := snapshot.Items.Value
			snapshot.Items.Value = append(append([]slicercli.Item(nil), items[:2]...), items[3:]...)[:remaining]
			h.src.set(snapshot)
			_, refresh := h.m.Update(c.done(nil))
			if h.m.roadmapCursor != 2 {
				t.Fatal("clamped before refreshed snapshot arrived")
			}
			h.m.Update(refresh())
			want := clampCursor(2, remaining)
			if h.m.roadmapCursor != want {
				t.Fatalf("cursor=%d; want old row clamped to %d", h.m.roadmapCursor, want)
			}
		})
	}
}
func TestSlicerTuiLookupKeepsOtherErrorsVerbatim(t *testing.T) {
	h, c := slicerTuiHarness(t)
	h.m.cfg.SlicerBin = t.TempDir() + "/slicer"
	_, lookupErr := osexec.LookPath(h.m.cfg.SlicerBin)
	if lookupErr == nil {
		t.Fatal("expected a missing path")
	}
	_, cmd := h.m.Update(press("O"))
	if cmd != nil || c.calls != 0 || h.m.actionStatus != "slicer tui: "+lookupErr.Error() {
		t.Fatalf("calls=%d status=%q; want raw lookup error", c.calls, h.m.actionStatus)
	}
}

func TestSlicerTuiHintFollowsRowActionsWhenEditHintsAreHidden(t *testing.T) {
	h, _ := slicerTuiHarness(t)
	h.m.cfg.Live = nil
	hint := wideHint(h)
	if strings.Contains(hint, "n new slice") || strings.Contains(hint, "N note") {
		t.Fatalf("edit hints should be hidden: %s", hint)
	}
	if strings.HasPrefix(hint, "O slicer tui") || !strings.HasPrefix(hint, "enter open; O slicer tui; tab views") {
		t.Fatalf("O must follow the row action when edit hints are hidden: %s", hint)
	}
	h.m.cfg.Live = &roadmapLive{}
	h.m.projects[0].snap.Items.Value = nil
	hint = wideHint(h)
	if strings.HasPrefix(hint, "O slicer tui") || !strings.HasPrefix(hint, "l launch; O slicer tui; m matrix") {
		t.Fatalf("O must follow launch when there are no items: %s", hint)
	}
}
