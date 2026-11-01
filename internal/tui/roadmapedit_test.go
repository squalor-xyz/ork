// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

type editLive struct {
	noteLive
	edits   []string
	changed bool
}

func (l *editLive) ItemPark(_ context.Context, project, item string) error {
	l.edits = append(l.edits, "park "+project+" "+item)
	return l.err
}
func (l *editLive) ItemUnpark(_ context.Context, project, item string) error {
	l.edits = append(l.edits, "unpark "+project+" "+item)
	return l.err
}
func (l *editLive) ItemMove(_ context.Context, project, item, other, position string) (bool, error) {
	l.edits = append(l.edits, "move "+project+" "+item+" "+position+" "+other)
	return l.changed, l.err
}
func editHarness(t *testing.T) (*harness, *editLive) {
	t.Helper()
	h := roadmapHarness(t)
	l := &editLive{changed: true}
	h.m.cfg.Live = l
	h.m.projects[0].snap.Items.Value = []slicercli.Item{item("S11", "Eleven"), item("S12", "Twelve"), item("S13", "Thirteen")}
	h.m.roadmapCursor = 1
	return h, l
}
func wantEditConfirm(t *testing.T, h *harness, l *editLive, want string) {
	t.Helper()
	if h.m.confirm == nil {
		t.Fatal("missing confirm")
	}
	if got := h.m.confirm.prompt(); got != want {
		t.Fatalf("confirm %q want %q", got, want)
	}
	if len(l.edits) != 0 {
		t.Fatal("edit before confirmation")
	}
}
func TestParkKeyAsksOnTheConfirmBar(t *testing.T) {
	for _, k := range []string{"P", "shift+p"} {
		t.Run(k, func(t *testing.T) {
			h, l := editHarness(t)
			h.m.Update(press(k))
			wantEditConfirm(t, h, l, "slicer park S12? [y/N] (Enter is No)")
		})
	}
}
func TestParkKeyOnAParkedRowAsksToUnpark(t *testing.T) {
	h, l := editHarness(t)
	h.m.projects[0].snap.Items.Value[1].Status = "parked"
	h.m.Update(press("P"))
	wantEditConfirm(t, h, l, "slicer unpark S12? [y/N] (Enter is No)")
	_, cmd := h.m.Update(press("y"))
	applyNoteCommands(h, cmd)
	if len(l.edits) != 1 || l.edits[0] != "unpark ork S12" {
		t.Fatalf("edits %v", l.edits)
	}
}
func TestEnterOnTheParkConfirmCancels(t *testing.T) {
	for _, k := range []string{"enter", "n", "esc"} {
		t.Run(k, func(t *testing.T) {
			h, l := editHarness(t)
			h.m.Update(press("P"))
			if h.m.confirm == nil {
				t.Fatal("missing confirm")
			}
			_, cmd := h.m.Update(press(k))
			if h.m.confirm != nil || h.m.actionStatus != "Cancelled" || cmd != nil || len(l.edits) != 0 {
				t.Fatalf("cancel status %q edits %v", h.m.actionStatus, l.edits)
			}
		})
	}
}
func TestYesParksAndTheRowShowsParked(t *testing.T) {
	h, l := editHarness(t)
	h.m.Update(press("P"))
	_, cmd := h.m.Update(press("y"))
	if cmd == nil || len(l.edits) != 0 || !strings.Contains(h.m.actionStatus, "pending") {
		t.Fatalf("pending status %q", h.m.actionStatus)
	}
	applyNoteCommands(h, cmd)
	if len(l.edits) != 1 || l.edits[0] != "park ork S12" || l.refreshes != 1 {
		t.Fatalf("edits=%v refresh=%d", l.edits, l.refreshes)
	}
	s := h.m.projects[0].snap
	s.Items.Value = []slicercli.Item{item("S11", "Eleven"), item("S13", "Thirteen"), item("S12", "Twelve")}
	s.Items.Value[2].Status = "parked"
	h.m.Update(snapshotMsg{name: "ork", snap: s})
	h.m.roadmapCursor = 0
	ov := h.m.roadmap(80)
	line := ov.body[ov.rows[2]]
	if !strings.Contains(line, h.m.cfg.Theme.Dim.Render("parked ")) {
		t.Fatalf("parked word missing or not dim: %q", line)
	}
}
func TestParkFailureShowsTheErrorVerbatim(t *testing.T) {
	h, l := editHarness(t)
	l.err = &ipc.Error{Code: "refused", Message: "run-123 is unfinished"}
	h.m.Update(press("P"))
	_, cmd := h.m.Update(press("y"))
	applyNoteCommands(h, cmd)
	if h.m.actionStatus != actionError(l.err) || l.refreshes != 0 {
		t.Fatalf("status %q refresh %d", h.m.actionStatus, l.refreshes)
	}
}
func TestMoveKeysShowTheExactCommand(t *testing.T) {
	for _, tc := range []struct{ k, pos, other string }{{"J", "after", "S13"}, {"shift+j", "after", "S13"}, {"K", "before", "S11"}, {"shift+k", "before", "S11"}} {
		t.Run(tc.k, func(t *testing.T) {
			h, l := editHarness(t)
			h.m.Update(press(tc.k))
			wantEditConfirm(t, h, l, "slicer move S12 --"+tc.pos+" "+tc.other+"? [y/N] (Enter is No)")
		})
	}
}
func TestMoveAtTheFirstOrLastRowDoesNothing(t *testing.T) {
	for _, tc := range []struct {
		k      string
		cursor int
	}{{"K", 0}, {"J", 2}} {
		t.Run(tc.k, func(t *testing.T) {
			h, l := editHarness(t)
			h.m.roadmapCursor = tc.cursor
			h.m.Update(press(tc.k))
			if h.m.confirm != nil || h.m.actionStatus != "" || len(l.edits) != 0 {
				t.Fatal("edge move acted")
			}
		})
	}
}
func TestMoveYesKeepsTheCursorOnTheMovedItem(t *testing.T) {
	h, l := editHarness(t)
	h.m.Update(press("J"))
	_, cmd := h.m.Update(press("y"))
	if cmd == nil {
		t.Fatal("no move command")
	}
	// Deliver just the write reply, then a stale poll, then the new order.
	_, refresh := h.m.Update(cmd())
	if refresh == nil || len(l.edits) != 1 || l.edits[0] != "move ork S12 after S13" || h.m.actionStatus != "Moved S12 after S13" {
		t.Fatalf("edits %v status %q", l.edits, h.m.actionStatus)
	}
	s := h.m.projects[0].snap
	h.m.Update(snapshotMsg{name: "ork", snap: s})
	s.Items.Value = []slicercli.Item{item("S11", "Eleven"), item("S13", "Thirteen"), item("S12", "Twelve")}
	h.m.Update(snapshotMsg{name: "ork", snap: s})
	if h.m.roadmapCursor != 2 {
		t.Fatalf("cursor %d want 2", h.m.roadmapCursor)
	}
	// A later change must not follow again.
	s.Items.Value = []slicercli.Item{item("S12", "Twelve"), item("S11", "Eleven"), item("S13", "Thirteen")}
	h.m.Update(snapshotMsg{name: "ork", snap: s})
	if h.m.roadmapCursor != 2 {
		t.Fatal("follow was not cleared")
	}
}
func TestMoveFollowClearsOnRowKeys(t *testing.T) {
	for _, k := range []string{"j", "k", "g", "G", "pgdown", "pgup", "}", "{"} {
		t.Run(k, func(t *testing.T) {
			h, _ := editHarness(t)
			h.m.Update(press("J"))
			_, cmd := h.m.Update(press("y"))
			if cmd == nil {
				t.Fatal("no move command")
			}
			h.m.Update(cmd())
			h.m.Update(press(k))
			cursor := h.m.roadmapCursor
			s := h.m.projects[0].snap
			s.Items.Value = []slicercli.Item{item("S12", "Twelve"), item("S13", "Thirteen"), item("S11", "Eleven")}
			if cursor == 0 {
				s.Items.Value = []slicercli.Item{item("S11", "Eleven"), item("S13", "Thirteen"), item("S12", "Twelve")}
			}
			h.m.Update(snapshotMsg{name: "ork", snap: s})
			if h.m.roadmapCursor != cursor {
				t.Fatalf("%s allowed late cursor follow", k)
			}
		})
	}
}
func TestMoveThatLeavesTheOrderSaysSo(t *testing.T) {
	for _, tc := range []struct{ key, pos, other string }{{"J", "after", "S113"}, {"K", "before", "S111"}} {
		t.Run(tc.key, func(t *testing.T) {
			h, l := editHarness(t)
			h.m.projects[0].snap.Items.Value = []slicercli.Item{item("S111", "Eleven"), item("S112", "Twelve"), item("S113", "Thirteen")}
			l.changed = false
			h.m.Update(press(tc.key))
			_, cmd := h.m.Update(press("y"))
			applyNoteCommands(h, cmd)
			want := "Moved S112 " + tc.pos + " " + tc.other + " in queue; list order unchanged"
			if h.m.actionStatus != want {
				t.Fatalf("status %q want %q", h.m.actionStatus, want)
			}
			if lipgloss.Width(want) > 54 {
				t.Fatalf("status too wide: %d", lipgloss.Width(want))
			}
			for _, size := range [][2]int{{80, 24}, {100, 30}} {
				h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				if !strings.Contains(plainView(h.m), want) {
					t.Errorf("%v status wraps or loses ids", size)
				}
			}
		})
	}
}
func TestRoadmapEditKeysAreInactiveOffTheList(t *testing.T) {
	for _, state := range []string{"item", "matrix", "help", "off roadmap", "no project", "unavailable", "empty", "no daemon", "unsupported"} {
		for _, k := range []string{"P", "J", "K"} {
			t.Run(state+"/"+k, func(t *testing.T) {
				h, l := editHarness(t)
				switch state {
				case "item":
					h.m.itemPane = &itemPane{id: "S12"}
				case "matrix":
					h.m.itemPane = &itemPane{id: matrixPaneID}
				case "help":
					h.m.showHelp = true
				case "off roadmap":
					h.m.view = 0
				case "no project":
					h.m.active = runningEntry
				case "unavailable":
					h.m.projects[0].snap.Unavailable = true
				case "empty":
					h.m.projects[0].snap.Items.Value = nil
				case "no daemon":
					h.m.cfg.Live = nil
				case "unsupported":
					h.m.cfg.Live = &noteLive{}
				}
				h.m.Update(press(k))
				want := ""
				if state == "no daemon" {
					want = "No daemon connection; action unavailable"
				}
				if state == "unsupported" {
					want = "Roadmap edit unavailable"
				}
				if h.m.confirm != nil || len(l.edits) != 0 || h.m.actionStatus != want {
					t.Fatalf("confirm=%v edits=%v status=%q want %q", h.m.confirm, l.edits, h.m.actionStatus, want)
				}
			})
		}
	}
}
func TestRoadmapEditHintsDropBeforeProjectEdits(t *testing.T) {
	h, _ := editHarness(t)
	for _, tc := range []struct {
		width, height int
		want          string
	}{
		{100, 30, "enter open; l launch; P park; J/K move; m matrix; q quit; ? help"},
		{80, 24, "enter open; l launch; m matrix; tab views; q quit; ? help"},
	} {
		h.m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
		got := hintLine(plainView(h.m))
		t.Logf("%dx%d: %s", tc.width, tc.height, got)
		if got != tc.want {
			t.Errorf("hint %q want %q", got, tc.want)
		}
	}
}

func TestRoadmapEditHelpFits(t *testing.T) {
	for _, slicerTui := range []bool{false, true} {
		h, _ := editHarness(t)
		if !slicerTui {
			h.m.projects[0].snap.Runs.Err = &clirun.ToolError{Code: "state", Message: "no .slicer/ found"}
		}
		for _, size := range [][2]int{{100, 30}, {80, 24}} {
			h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			h.m.showHelp = true
			v := plainView(h.m)
			for _, want := range []string{"e edit lineup (Waves, launch)", "P park/unpark; J/K move item down/up", "q quit"} {
				if !strings.Contains(v, want) {
					t.Errorf("%v help misses %q:\n%s", size, want, v)
				}
			}
			room := 72
			if size[0] == 80 {
				room = 58
			}
			help := h.m.helpLines(room, 25)
			for _, want := range []string{"e edit lineup (Waves, launch)", "P park/unpark; J/K move item down/up"} {
				if !strings.Contains(strings.Join(help, "\n"), want) {
					t.Errorf("%d-column help clips %q", room, want)
				}
			}
			for _, line := range help {
				if lipgloss.Width(line) > room {
					t.Errorf("help line exceeds %d: %q", room, line)
				}
			}
			if size[0] == 100 && strings.Contains(v, "more lines") {
				t.Errorf("100x30 help is clipped:\n%s", v)
			}
		}
	}
}

func TestRoadmapParkedGolden(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h, _ := editHarness(t)
		h.m.cfg.Theme = NewTheme(mono)
		h.m.projects[0].snap.Items.Value[2].Status = "parked"
		h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		if !strings.Contains(h.view(), "parked") {
			t.Fatal("missing parked label")
		}
		name := "color"
		if mono {
			name = "mono"
		}
		checkGolden(t, fmt.Sprintf("roadmap-parked-100x30-%s.txt", name), h.m.View().Content)
	}
}

func TestParkRefusalNamesTheUnfinishedRunAndHidesHint(t *testing.T) {
	h, l := editHarness(t)
	h.m.projects[0].snap.EngineRuns = []source.EngineRun{{ID: "unfinished-123", Item: "S12", State: "implement"}}
	h.m.Update(press("P"))
	if h.m.confirm != nil || len(l.edits) != 0 || !strings.Contains(h.m.actionStatus, "unfinished-123") {
		t.Fatalf("refusal %q", h.m.actionStatus)
	}
	for _, e := range h.m.contextHints() {
		if e.b.Help().Key == "P" {
			t.Fatal("park hint offered for an unfinished item")
		}
	}
	h.m.Update(press("J"))
	if h.m.confirm == nil {
		t.Fatal("run blocked a move")
	}
	h.m.confirm = nil
	h.m.projects[0].snap.Items.Value[1].Status = "parked"
	h.m.Update(press("P"))
	wantEditConfirm(t, h, l, "slicer unpark S12? [y/N] (Enter is No)")
}

func TestMoveReplyFollowsSnapshotThatArrivedFirst(t *testing.T) {
	for _, key := range []string{"J", "K"} {
		t.Run(key, func(t *testing.T) {
			h, _ := editHarness(t)
			h.m.Update(press(key))
			_, cmd := h.m.Update(press("y"))
			if cmd == nil {
				t.Fatal("missing move command")
			}
			reply := cmd()
			s := h.m.projects[0].snap
			want := 2
			if key == "J" {
				s.Items.Value = []slicercli.Item{item("S11", "Eleven"), item("S13", "Thirteen"), item("S12", "Twelve")}
			} else {
				want = 0
				s.Items.Value = []slicercli.Item{item("S12", "Twelve"), item("S11", "Eleven"), item("S13", "Thirteen")}
			}
			h.m.Update(snapshotMsg{name: "ork", snap: s})
			h.m.Update(reply)
			if h.m.roadmapCursor != want {
				t.Fatalf("cursor %d want %d", h.m.roadmapCursor, want)
			}
			if h.m.roadmapFollow != nil {
				t.Fatal("reply left a lingering follow")
			}
			s.Items.Value = []slicercli.Item{item("S11", "Eleven"), item("S12", "Twelve"), item("S13", "Thirteen")}
			h.m.Update(snapshotMsg{name: "ork", snap: s})
			if h.m.roadmapCursor != want {
				t.Fatal("later snapshot moved cursor again")
			}
		})
	}
}

func TestMoveHintUnavailableWithFewerThanTwoItems(t *testing.T) {
	for _, n := range []int{0, 1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			h, l := editHarness(t)
			h.m.projects[0].snap.Items.Value = h.m.projects[0].snap.Items.Value[:n]
			for _, verb := range []string{"move", "down", "up"} {
				if status, refused := h.m.roadmapEditRefusal(verb); !refused || status != "" {
					t.Errorf("%s refusal %q %v", verb, status, refused)
				}
			}
			for _, e := range h.m.contextHints() {
				if e.b.Help().Key == "J/K" {
					t.Fatal("move hint offered without a neighbour")
				}
			}
			for _, key := range []string{"J", "K"} {
				h.m.Update(press(key))
			}
			if h.m.confirm != nil || h.m.actionStatus != "" || len(l.edits) != 0 {
				t.Fatal("silent refusal acted")
			}
		})
	}
}

func TestRoadmapEditHelpGolden(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, size := range [][2]int{{100, 30}, {80, 24}} {
			h, _ := editHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			h.m.Update(press("?"))
			name := "color"
			if mono {
				name = "mono"
			}
			checkGolden(t, fmt.Sprintf("roadmap-help-%dx%d-%s.txt", size[0], size[1], name), h.m.View().Content)
		}
	}
}
