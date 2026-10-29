// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestApprovePromptNamesTheStopPoint(t *testing.T) {
	a := runAction{verb: "approve", item: "S1", stopAfter: "commit", push: true}
	prompt := a.promptWidth(100)
	if !strings.Contains(prompt, "Commit S1 on its branch?") || strings.Contains(prompt, "push") || strings.Contains(prompt, "remove") {
		t.Fatal(prompt)
	}
	a.outcome = approveOutcome(runrec.Record{State: runrec.StateCommitted})
	if a.doneText() != "committed S1; ready to land" {
		t.Fatal(a.doneText())
	}
	a.stopAfter = "land"
	if !strings.Contains(a.promptWidth(100), "Land and push S1, then remove its worktree and branch?") {
		t.Fatal(a.promptWidth(100))
	}
}

func TestApprovalCardLandNoteFollowsStopAfter(t *testing.T) {
	h := cardHarness(t, approvalCard())
	for _, push := range []bool{false, true} {
		h.m.byName["ork"].reg.Push = push
		h.m.byName["ork"].snap.EngineRuns[0].StopAfter = "commit"
		if got := h.m.cardLandNote(); got != "Approve commits it; its worktree and branch stay for you to land." {
			t.Fatal(got)
		}
		h.m.byName["ork"].snap.EngineRuns[0].StopAfter = "land"
		want := "Approve lands it, then removes its worktree and branch."
		if push {
			want = "Approve lands and pushes it, then removes its worktree and branch."
		}
		if got := h.m.cardLandNote(); got != want {
			t.Fatal(got)
		}
	}
}

func TestLineupEditorStopRowCyclesAndSaves(t *testing.T) {
	h, l := lineupHarness(t)
	if !strings.Contains(h.view(), "stop: land") {
		t.Fatal(h.view())
	}
	e := h.m.waves.lineup
	e.row = len(lineupRoles)
	h.deliver(press("left"))
	if !strings.Contains(h.view(), "stop*: < commit >") {
		t.Fatal(h.view())
	}
	h.deliver(press("right"))
	if !strings.Contains(h.view(), "stop: < land >") {
		t.Fatal(h.view())
	}
	h.deliver(press("right"))
	_, cmd := h.m.Update(press("enter"))
	if cmd == nil {
		t.Fatal("no replan")
	}
	h.m.Update(cmd())
	if len(l.plans) == 0 || l.plans[len(l.plans)-1].StopAfter != "commit" {
		t.Fatalf("plans: %+v", l.plans)
	}
	p := h.m.waves.picked["A"]
	if p.StopAfter != "commit" || !strings.Contains(strings.Join(waveLineup(p), "\n"), "stop*: commit") {
		t.Fatalf("params: %+v", p)
	}
}

func TestLaunchPlanShowsTheStopPoint(t *testing.T) {
	for _, stop := range []string{"land", "commit"} {
		t.Run(stop, func(t *testing.T) {
			h, l := launchHarness(t)
			l.plan.StopAfter = "land"
			launchConfirmed(t, h)
			if !strings.Contains(h.view(), "stop: after landing") {
				t.Fatal(h.view())
			}
			if stop == "commit" {
				h.m.Update(press("e"))
				for range len(lineupRoles) {
					h.m.Update(press("down"))
				}
				h.m.Update(press("right"))
				_, replan := h.m.Update(press("enter"))
				if replan == nil {
					t.Fatal("no replan")
				}
				l.plan.StopAfter = stop
				h.m.Update(replan())
				if l.plans[len(l.plans)-1].StopAfter != stop || !strings.Contains(h.view(), "stop: after commit (you land it)") {
					t.Fatal("edited stop point lost")
				}
			}
			_, cmd := h.m.Update(press("y"))
			if cmd == nil {
				t.Fatal("no Start")
			}
			cmd()
			if len(l.starts) != 1 || (stop == "commit" && l.starts[0].StopAfter != stop) || l.starts[0].Expected.StopAfter != stop {
				t.Fatalf("starts: %+v", l.starts)
			}
		})
	}
}

func TestLineupStopRowRejectsTextAndOffersCycle(t *testing.T) {
	for _, width := range []int{80, 200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			h, _ := lineupHarness(t)
			h.m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			for range len(lineupRoles) {
				h.deliver(press("down"))
			}
			if hint := hintLine(plainView(h.m)); !strings.Contains(hint, "left/right cycle") || !strings.Contains(hint, "enter save and replan") || !strings.Contains(hint, "esc discard") || strings.Contains(hint, "whole number") {
				t.Fatal(hint)
			}
			if !strings.Contains(h.view(), "stop: < land >") {
				t.Fatal(h.view())
			}
			h.deliver(tea.PasteMsg{Content: "merge"})
			h.deliver(press("x"))
			if h.m.waves.lineup.stopAfter != "land" {
				t.Fatal("text changed stop point")
			}
			h.deliver(press("right"))
			if !strings.Contains(h.view(), "stop*: < commit >") {
				t.Fatal(h.view())
			}
			for range len(h.m.waves.lineup.budgets) + 1 {
				h.deliver(press("down"))
			}
			if h.m.waves.lineup.row != 0 {
				t.Fatal("down does not wrap")
			}
			h.deliver(press("up"))
			if h.m.waves.lineup.row != len(lineupRoles)+len(h.m.waves.lineup.budgets) {
				t.Fatal("up does not wrap to final budget")
			}
		})
	}
}

func TestLineupStopBeforeBudgets(t *testing.T) {
	h, l := lineupHarness(t)
	body := strings.Join(h.m.lineupBody(100).body, "\n")
	labels := []string{"architect:", "stop:", "Budget minutes:", "Budget tokens:", "Zero budget uses the project default."}
	last := -1
	for _, label := range labels {
		pos := strings.Index(body, label)
		if pos <= last {
			t.Fatalf("row order %q: %s", label, body)
		}
		last = pos
	}
	for range len(lineupRoles) + 1 {
		h.deliver(press("tab"))
	}
	for _, value := range []string{"12", "345"} {
		h.deliver(press("ctrl+u"))
		for _, digit := range value {
			h.deliver(press(string(digit)))
		}
		h.deliver(press("down"))
	}
	// Paste must also reach the budget, while stop rejects it.
	h.deliver(press("up"))
	replaceLineupField(h, "678")
	h.deliver(press("enter"))
	p := l.plans[len(l.plans)-1]
	if p.Budgets == nil || p.Budgets.Minutes != 12 || p.Budgets.Tokens != 678 || p.StopAfter != "" {
		t.Fatalf("budgets or stop: %+v", p)
	}
}

func TestApprovePromptWithUnknownStopPointIsNeutral(t *testing.T) {
	a := runAction{verb: "approve", run: "engine", push: true}
	prompt := a.promptWidth(80)
	if !strings.Contains(prompt, "Approve run engine?") || strings.Contains(prompt, "Land") || strings.Contains(prompt, "push") || strings.Contains(prompt, "remove") {
		t.Fatal(prompt)
	}
}

func TestApproveUsesRunStopPoint(t *testing.T) {
	h := actionHarness(t, runrec.StateAwaitingApproval, "")
	h.m.byName["ork"].snap.EngineRuns[0].StopAfter = "commit"
	h.m.cfg.Live.(*actionLive).approved = runrec.Record{State: runrec.StateCommitted}
	h.deliver(press("a"))
	if h.m.confirm == nil || !strings.Contains(h.m.confirm.prompt(), "Commit S22 on its branch?") {
		t.Fatal("run stop point missing from confirmation")
	}
	h.deliver(press("y"))
	if h.m.actionStatus != "committed S22; ready to land" {
		t.Fatal(h.m.actionStatus)
	}
}

func TestLineupStopRowGolden(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h, _ := lineupHarness(t)
		h.m.cfg.Theme = NewTheme(mono)
		h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		for range len(lineupRoles) {
			h.deliver(press("down"))
		}
		mode := "color"
		if mono {
			mode = "mono"
		}
		checkGolden(t, "lineup-stop-80x24-"+mode+".txt", h.m.View().Content)
	}
}
