// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

type noteLive struct {
	roadmapLive
	notes     []string
	refreshes int
}

func (l *noteLive) ItemNote(_ context.Context, p, id, text string) error {
	l.writes++
	l.notes = append(l.notes, p+" "+id+" "+text)
	l.detail = &slicercli.Detail{Item: slicercli.Item{ID: id, Notes: []string{text}}}
	return l.err
}
func (l *noteLive) Refresh(context.Context, string) error { l.refreshes++; return nil }
func sendNote(t *testing.T, h *harness, text string) tea.Cmd {
	t.Helper()
	h.m.Update(press("N"))
	if !strings.Contains(h.view(), "Note for S90:") {
		t.Fatalf("note field missing:\n%s", h.view())
	}
	h.m.Update(tea.PasteMsg{Content: text})
	_, cmd := h.m.Update(press("enter"))
	return cmd
}
func applyNoteCommands(h *harness, cmd tea.Cmd) {
	for _, msg := range exec(cmd) {
		_, next := h.m.Update(msg)
		for _, reply := range exec(next) {
			h.m.Update(reply)
		}
	}
}
func TestRoadmapNoteKeyOpensTheFieldAndSends(t *testing.T) {
	h := roadmapHarness(t)
	l := &noteLive{}
	h.m.cfg.Live = l
	cmd := sendNote(t, h, "owner decision")
	if cmd == nil || l.writes != 0 {
		t.Fatal("write must run in command")
	}
	applyNoteCommands(h, cmd)
	if len(l.notes) != 1 || l.notes[0] != "ork S90 owner decision" || l.refreshes != 1 || !strings.Contains(h.view(), "Noted S90") {
		t.Fatalf("notes=%v refresh=%d\n%s", l.notes, l.refreshes, h.view())
	}
}
func TestRoadmapNoteFieldOwnsItsKeys(t *testing.T) {
	h := roadmapHarness(t)
	l := &noteLive{}
	h.m.cfg.Live = l
	h.m.Update(press("N"))
	if !h.m.projectTextEntry() {
		t.Fatal("field does not own project keys")
	}
	for _, k := range []string{"+", "-", "q", "l", "1", "9"} {
		h.m.Update(press(k))
	}
	if !strings.Contains(h.view(), "+-ql19") {
		t.Fatalf("keys escaped field:\n%s", h.view())
	}
	h.m.Update(press("esc"))
	if strings.Contains(h.view(), "Note for") || l.writes != 0 {
		t.Fatal("cancel wrote or left field open")
	}
}
func TestRoadmapNoteRefusals(t *testing.T) {
	for _, state := range []string{"no daemon", "unavailable", "running", "help", "blank"} {
		t.Run(state, func(t *testing.T) {
			h := roadmapHarness(t)
			l := &noteLive{}
			h.m.cfg.Live = l
			switch state {
			case "no daemon":
				h.m.cfg.Live = nil
			case "unavailable":
				h.m.projects[0].snap.Unavailable = true
			case "running":
				h.m.active = runningEntry
			case "help":
				h.m.showHelp = true
			case "matrix":
				h.m.itemPane = &itemPane{id: matrixPaneID}
			}
			h.m.Update(press("N"))
			if state == "blank" {
				h.m.Update(tea.PasteMsg{Content: "  "})
				_, cmd := h.m.Update(press("enter"))
				applyNoteCommands(h, cmd)
				if !strings.Contains(h.view(), "Note must not be empty") || !strings.Contains(h.view(), "Note for") {
					t.Fatal("blank note did not stay open with refusal")
				}
			} else if strings.Contains(h.view(), "Note for") || h.m.actionStatus == "" {
				t.Fatalf("missing refusal: %q", h.m.actionStatus)
			}
			if l.writes != 0 {
				t.Fatal("refusal wrote note")
			}
		})
	}
}
func TestNoteFromTheItemPaneReloadsTheDetail(t *testing.T) {
	h := roadmapHarness(t)
	l := &noteLive{}
	h.m.cfg.Live = l
	_, cmd := h.m.Update(press("enter"))
	h.m.Update(cmd())
	h.m.roadmapCursor = 1
	cmd = sendNote(t, h, "new note")
	applyNoteCommands(h, cmd)
	if len(l.calls) != 2 || l.calls[1] != "ork S90" || !strings.Contains(h.view(), "new note") || !strings.Contains(h.view(), "Notes (1)") {
		t.Fatalf("calls=%v\n%s", l.calls, h.view())
	}
}
func TestGoldenRoadmapNote(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h := roadmapHarness(t)
		h.m.cfg.Live = &noteLive{}
		h.m.cfg.Theme = NewTheme(mono)
		h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		h.m.Update(press("N"))
		h.m.Update(tea.PasteMsg{Content: "Record the owner's decision"})
		if !strings.Contains(h.view(), "Note for S90:") {
			t.Fatal("field missing")
		}
		name := "color"
		if mono {
			name = "mono"
		}
		checkGolden(t, fmt.Sprintf("roadmap-note-100x30-%s.txt", name), h.m.View().Content)
	}
}

func TestNotePendingAndLateReplies(t *testing.T) {
	for _, close := range []string{"esc", "project", "view", "reopen"} {
		t.Run(close, func(t *testing.T) {
			h := roadmapHarness(t)
			l := &noteLive{}
			h.m.cfg.Live = l
			cmd := sendNote(t, h, "first")
			if _, again := h.m.Update(press("enter")); again != nil {
				t.Fatal("pending note sent twice")
			}
			msg := cmd()
			switch close {
			case "project":
				h.m.switchTo(1)
			case "view":
				h.m.moveView(1)
			default:
				h.m.Update(press("esc"))
			}
			if close == "reopen" {
				h.m.Update(press("N"))
				h.m.Update(tea.PasteMsg{Content: "second"})
			}
			_, next := h.m.Update(msg)
			if next != nil || l.refreshes != 0 || h.m.actionStatus != "Noted S90" {
				t.Fatalf("late reply did more than status: %v %q", next, h.m.actionStatus)
			}
			if close == "reopen" && !strings.Contains(h.view(), "second") {
				t.Fatal("late reply closed new field")
			}
		})
	}
}

func TestNoteErrorKeepsTextForRetry(t *testing.T) {
	h := roadmapHarness(t)
	l := &noteLive{}
	h.m.cfg.Live = l
	l.err = fmt.Errorf("write failed")
	applyNoteCommands(h, sendNote(t, h, "keep me"))
	if !strings.Contains(h.view(), "write failed") || !strings.Contains(h.view(), "keep me") || l.refreshes != 0 {
		t.Fatalf("failed note state:\n%s", h.view())
	}
	l.err = nil
	_, cmd := h.m.Update(press("enter"))
	applyNoteCommands(h, cmd)
	if l.writes != 2 || l.refreshes != 1 || h.m.compose != nil {
		t.Fatalf("retry writes=%d refresh=%d", l.writes, l.refreshes)
	}
}

func TestNotePendingHintSaysClose(t *testing.T) {
	h := roadmapHarness(t)
	h.m.cfg.Live = &noteLive{}
	cmd := sendNote(t, h, "text")
	if cmd == nil || !strings.Contains(hintLine(plainView(h.m)), "esc close") || strings.Contains(hintLine(plainView(h.m)), "cancel") {
		t.Fatalf("pending hint: %s", hintLine(plainView(h.m)))
	}
}
func TestNoteKeyIsSilentInMatrixPane(t *testing.T) {
	h := roadmapHarness(t)
	h.m.cfg.Live = &noteLive{}
	h.m.itemPane = &itemPane{id: matrixPaneID}
	h.m.actionStatus = "previous status"
	_, cmd := h.m.Update(press("N"))
	if cmd != nil || h.m.compose != nil || h.m.actionStatus != "previous status" {
		t.Fatalf("N acted in matrix: %q", h.m.actionStatus)
	}
}
