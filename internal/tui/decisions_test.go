// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/source"
)

func decisionsHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, proj("zeta", true), proj("alpha", true), proj("plain", false))
	for i, p := range h.m.projects {
		s := snap(p.reg, []adjcli.Record{{ID: 90 + i, Key: "store.engine", Title: fmt.Sprintf("Question %d", i), Options: []adjcli.Option{{Label: "A", Summary: "Files", Tradeoffs: "Simple"}, {Label: "B", Summary: "Database"}}, Recommendation: &adjcli.Recommendation{Label: "A", Text: "Use files", Reasoning: "Easy audit"}, Context: "Storage context"}, {ID: 2 + i, Key: "ui.order", Title: "Second question"}}, nil)
		s.Decided = source.Result[[]adjcli.Record]{Value: []adjcli.Record{{ID: 10 + i, Key: "ui.screen", Title: fmt.Sprintf("Resolved %d", i), Decision: "Use tabs", Rationale: "Fast navigation", History: []adjcli.Event{{Date: "2026-10-24", Event: "decided by owner"}}}}}
		h.src.set(s)
	}
	h.start()
	h.m.switchTo(0)
	h.m.view = 4
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.src.take()
	return h
}

func TestDecisionsListsOpenFirstWithOptionsAndRecommendation(t *testing.T) {
	h := decisionsHarness(t)
	v := h.view()
	for _, want := range []string{"#90", "store.engine", "Question 0", "A: Files", "Simple", "B: Database", "Recommendation: A", "Use files", "Easy audit"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	if strings.Index(v, "#90") >= strings.Index(v, "#2") || strings.Contains(v, "Resolved 0") {
		t.Fatal("open order/tab incorrect:\n" + v)
	}
}

func TestDecisionsSecondTabListsDecided(t *testing.T) {
	h := decisionsHarness(t)
	h.send(press("right"))
	v := h.view()
	if !strings.Contains(v, "#10") || !strings.Contains(v, "Resolved 0") || strings.Contains(v, "Question 0") {
		t.Fatal(v)
	}
}

func TestDecisionsAllProjectsMergesAdjProjectsOnly(t *testing.T) {
	h := decisionsHarness(t)
	h.send(press("A"))
	v := h.view()
	if rowsWith(v, "zeta", "#90") != 1 || rowsWith(v, "alpha", "#91") != 1 || strings.Contains(v, "#92") || strings.Index(v, "#90") >= strings.Index(v, "#91") {
		t.Fatal(v)
	}
	h.send(press("right"))
	v = h.view()
	if rowsWith(v, "zeta", "#10") != 1 || rowsWith(v, "alpha", "#11") != 1 || strings.Contains(v, "#12") {
		t.Fatal(v)
	}
}

func TestDecisionsNoAdjProject(t *testing.T) {
	h := decisionsHarness(t)
	h.send(press("3"))
	if !strings.Contains(h.view(), "no adj in this project") {
		t.Fatal(h.view())
	}
}

func TestDecisionsEnterShowsRecordFromSnapshot(t *testing.T) {
	for _, decided := range []bool{false, true} {
		t.Run(fmt.Sprint(decided), func(t *testing.T) {
			h := decisionsHarness(t)
			live := &roadmapLive{}
			h.m.cfg.Live = live
			if decided {
				h.send(press("right"))
			}
			if cmd := h.m.key(press("enter")); cmd != nil {
				t.Fatal("detail returned a command")
			}
			v := h.view()
			wants := []string{"Storage context", "Files", "Database", "Recommendation: A", "Easy audit"}
			if decided {
				wants = []string{"Use tabs", "Fast navigation", "2026-10-24", "decided by owner"}
			}
			for _, want := range wants {
				if !strings.Contains(v, want) {
					t.Errorf("missing %q:\n%s", want, v)
				}
			}
			if len(live.calls) != 0 || len(h.src.take()) != 0 {
				t.Fatal("detail fetched data")
			}
			h.send(press("esc"))
			if strings.Contains(h.view(), "esc returns") {
				t.Fatal("detail stayed open")
			}
		})
	}
}

func TestDecisionsWritesNothing(t *testing.T) {
	for _, k := range []string{"a", "x", "t", "c", "l", "e", "space", "enter", "y"} {
		t.Run(k, func(t *testing.T) {
			h := actionHarness(t, "awaiting-approval", "")
			live := &roadmapLive{}
			h.m.cfg.Live = live
			h.m.view = 4
			h.deliver(press(k))
			h.deliver(press("y"))
			if live.writes != 0 || h.m.confirm != nil || h.m.pane != nil || h.m.launch != nil {
				t.Fatal("Decisions dispatched a run action")
			}
		})
	}
}

func TestDecisionsErrorsKeepGoodRows(t *testing.T) {
	for _, part := range []string{"inbox", "list"} {
		t.Run(part, func(t *testing.T) {
			h := decisionsHarness(t)
			h.send(press("A"))
			err := &clirun.ToolError{Code: "locked", Message: "Exact failure."}
			if part == "inbox" {
				h.m.projects[0].snap.Open.Err = err
			} else {
				h.m.projects[0].snap.Decided.Err = err
				h.send(press("right"))
			}
			v := h.view()
			if !strings.Contains(v, part+": locked: Exact failure.") || !strings.Contains(v, "alpha") {
				t.Fatal(v)
			}
			if part == "inbox" && (!strings.Contains(v, "#91") || strings.Contains(v, "#90")) {
				t.Fatal(v)
			}
			if part == "list" && (!strings.Contains(v, "#11") || strings.Contains(v, "#10")) {
				t.Fatal(v)
			}
		})
	}
}

func TestDecisionsLeftRightTabs(t *testing.T) {
	h := decisionsHarness(t)
	for _, k := range []string{"right", "left", "left", "right"} {
		h.send(press(k))
		if h.m.active != 0 {
			t.Fatal("tab key changed project")
		}
	}
	if !strings.Contains(h.view(), "Question 0") {
		t.Fatal(h.view())
	}
}

func TestDecisionsAToggle(t *testing.T) {
	h := decisionsHarness(t)
	h.send(press("A"))
	if !strings.Contains(h.view(), "#91") {
		t.Fatal(h.view())
	}
	h.send(press("A"))
	if strings.Contains(h.view(), "#91") {
		t.Fatal(h.view())
	}
}

func TestDecisionsAStillApprovesElsewhere(t *testing.T) {
	h := actionHarness(t, "awaiting-approval", "")
	h.deliver(press("a"))
	h.deliver(press("y"))
	if !strings.Contains(h.view(), "S22  commit") {
		t.Fatal(h.view())
	}
}

func TestDecisionsNeedsYouShowsAll(t *testing.T) {
	h := decisionsHarness(t)
	h.m.switchTo(-1)
	if !strings.Contains(h.view(), "#90") || !strings.Contains(h.view(), "#91") || strings.Contains(h.view(), "#92") {
		t.Fatal(h.view())
	}
}

func TestDecisionsFlagPersists(t *testing.T) {
	h := decisionsHarness(t)
	h.send(press("A"))
	h.send(press("2"))
	if !strings.Contains(h.view(), "#90") {
		t.Fatal(h.view())
	}
	h.send(press("A"))
	if strings.Contains(h.view(), "#90") || !strings.Contains(h.view(), "#91") {
		t.Fatal(h.view())
	}
}

func TestDecisionsRowCursor(t *testing.T) {
	h := decisionsHarness(t)
	h.send(press("down"))
	h.send(press("enter"))
	v := h.view()
	if !strings.Contains(v, "Second question") || strings.Contains(v, "Storage context") {
		t.Fatal(v)
	}
	h.send(press("esc"))
	h.send(press("up"))
	h.send(press("enter"))
	if !strings.Contains(h.view(), "Storage context") {
		t.Fatal(h.view())
	}
}

func TestDecisionsGolden(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h := decisionsHarness(t)
		h.m.cfg.Theme = NewTheme(mono)
		name := "color"
		if mono {
			name = "mono"
		}
		checkGolden(t, "decisions-100x30-"+name+".txt", h.m.View().Content)
	}
}

func TestDecisionsEmptyPendingAndFailedParts(t *testing.T) {
	h := decisionsHarness(t)
	h.m.projects[0].loaded = false
	if !strings.Contains(h.view(), "pending: waiting for the first reply") {
		t.Fatal(h.view())
	}
	h.m.projects[0].loaded = true
	h.m.projects[0].snap.Open.Value = nil
	if !strings.Contains(h.view(), "No open records") {
		t.Fatal(h.view())
	}
	h.m.projects[0].snap.Open.Err = &clirun.ToolError{Code: "locked", Message: "Inbox unavailable."}
	h.send(press("right"))
	if !strings.Contains(h.view(), "Resolved 0") {
		t.Fatal("inbox error hid decided rows:\n" + h.view())
	}
	h.m.projects[0].snap.Open.Err = nil
	h.m.projects[0].snap.Open.Value = []adjcli.Record{{ID: 5, Title: "Still open"}}
	h.m.projects[0].snap.Decided.Err = &clirun.ToolError{Code: "state", Message: "List unavailable."}
	h.send(press("left"))
	if !strings.Contains(h.view(), "Still open") {
		t.Fatal("list error hid open rows:\n" + h.view())
	}
}

func TestDecisionsHasNoRunCursor(t *testing.T) {
	h := actionHarness(t, "awaiting-approval", "")
	h.m.view = 4
	if _, ok := h.m.selectedRow(); ok {
		t.Fatal("Decisions retained a run cursor")
	}
	h.send(press("down"))
	if h.m.cursor != 0 {
		t.Fatal("Decisions moved Overview cursor")
	}
}

func TestDecisionsDetailScrollAndNarrowViews(t *testing.T) {
	h := decisionsHarness(t)
	h.m.projects[0].snap.Open.Value[0].Context = strings.Repeat("context line\n", 40) + "LAST CONTEXT LINE"
	h.send(press("enter"))
	for i := 0; i < 80; i++ {
		h.send(press("down"))
	}
	if !strings.Contains(h.view(), "LAST CONTEXT LINE") {
		t.Fatal(h.view())
	}
	for i := 0; i < 80; i++ {
		h.send(press("up"))
	}
	if !strings.Contains(h.view(), "esc returns") {
		t.Fatal(h.view())
	}
	for _, size := range [][2]int{{100, 30}, {40, 10}, {20, 7}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, line := range strings.Split(plainView(h.m), "\n") {
			if len([]rune(line)) > size[0] {
				t.Fatalf("line wider than %d: %q", size[0], line)
			}
		}
	}
}

func TestDecisionsHelpAndViewCycle(t *testing.T) {
	h := decisionsHarness(t)
	h.send(press("?"))
	for _, want := range []string{"A (Decisions) toggle all projects", "[ previous project", "quit"} {
		if !strings.Contains(h.view(), want) {
			t.Fatalf("missing help %q:\n%s", want, h.view())
		}
	}
	h.send(press("esc"))
	h.send(press("A"))
	h.send(press("tab"))
	if h.m.view != 0 {
		t.Fatal("Decisions did not wrap to Overview")
	}
	h.send(press("shift+tab"))
	if h.m.view != 4 || !strings.Contains(h.view(), "#91") {
		t.Fatal("view cycle lost Decisions or scope")
	}
}

func TestDecisionsFixTabAndScopeHierarchy(t *testing.T) {
	h := decisionsHarness(t)
	th := h.m.cfg.Theme
	body := h.m.decisions(72).body
	if body[0] != th.ViewActive.Render("Open")+th.Dim.Render(" | ")+"Decided" {
		t.Errorf("tab hierarchy: %q", body[0])
	}
	if body[1] != th.Heading.Render("zeta") {
		t.Errorf("scope heading: %q", body[1])
	}
	if body[len(body)-1] != indent+"[left/right] tabs   [A] all projects   [enter] open" {
		t.Errorf("one-project hint: %q", body[len(body)-1])
	}
	h.send(press("A"))
	body = h.m.decisions(72).body
	if body[1] != th.Heading.Render("All projects using adj") {
		t.Errorf("all scope heading: %q", body[1])
	}
	if body[len(body)-1] != indent+"[left/right] tabs   [A] zeta only   [enter] open" {
		t.Errorf("all-project hint: %q", body[len(body)-1])
	}
	h.m.switchTo(-1)
	body = h.m.decisions(72).body
	if body[len(body)-1] != indent+"[left/right] tabs   [enter] open" {
		t.Errorf("Needs you hint: %q", body[len(body)-1])
	}
}

func TestDecisionsFixRowsClipAndAlign(t *testing.T) {
	h := decisionsHarness(t)
	h.send(press("A"))
	h.m.projects[0].snap.Open.Value[0].Title = strings.Repeat("verylong ", 20) + "HIDDEN TITLE TAIL"
	body := h.m.decisions(55).body
	plain := sgr.ReplaceAllString(strings.Join(body, "\n"), "")
	if strings.Contains(plain, "HIDDEN TITLE TAIL") || !strings.Contains(plain, "...") {
		t.Error("title wrapped instead of clipping")
	}
	first, second := "", ""
	for _, line := range body {
		text := sgr.ReplaceAllString(line, "")
		if strings.Contains(text, "#90") {
			first = text
			if line != h.m.cfg.Theme.TabActive.Render(text) {
				t.Error("cursor does not include indent")
			}
		}
		if strings.Contains(text, "#91") {
			second = text
		}
	}
	if strings.Index(first, "#90") != strings.Index(second, "#91") {
		t.Errorf("project columns do not align: %q / %q", first, second)
	}
	h.m.projects[1].reg.Name = strings.Repeat("long", 10)
	body = h.m.decisions(72).body
	for _, line := range body {
		text := sgr.ReplaceAllString(line, "")
		if strings.Contains(text, "#91") && !strings.HasPrefix(text, indent+clip(h.m.projects[1].reg.Name, 20)+"  #91") {
			t.Errorf("project name not capped: %q", text)
		}
	}
}

func TestDecisionsFixErrorContinuationHangs(t *testing.T) {
	h := decisionsHarness(t)
	h.send(press("A"))
	h.m.projects[0].snap.Open.Err = &clirun.ToolError{Code: "locked", Message: "Exact failure with several words that must wrap verbatim across lines."}
	body := h.m.decisions(42).body
	start := -1
	for i, line := range body {
		if strings.Contains(line, "inbox:") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("missing error")
	}
	prefix := indent + "zeta   "
	if text := sgr.ReplaceAllString(body[start], ""); !strings.HasPrefix(text, prefix+"inbox:") {
		t.Errorf("first error line: %q", text)
	}
	if text := sgr.ReplaceAllString(body[start+1], ""); !strings.HasPrefix(text, strings.Repeat(" ", len(prefix))) {
		t.Errorf("error continuation not hung: %q", text)
	}
}

func TestDecisionsFixOptionAndDetailHierarchy(t *testing.T) {
	h := decisionsHarness(t)
	th := h.m.cfg.Theme
	body := strings.Join(h.m.decisions(72).body, "\n")
	for _, want := range []string{th.Dim.Render("       Simple"), th.Dim.Render("       Use files"), th.Dim.Render("       Easy audit")} {
		if !strings.Contains(body, want) {
			t.Errorf("missing dim subordinate %q", want)
		}
	}
	h.send(press("right"))
	h.send(press("enter"))
	body = strings.Join(h.m.decisions(72).body, "\n")
	for _, want := range []string{th.Heading.Render("zeta #10  (esc returns)"), th.Heading.Render("ui.screen  Resolved 0"), th.Heading.Render("History"), indent + "2026-10-24 decided by owner"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing detail hierarchy %q", want)
		}
	}
}

func TestDecisionsFixScrollIsTopLineAndClamped(t *testing.T) {
	h := decisionsHarness(t)
	var context []string
	for i := 0; i < 90; i++ {
		context = append(context, fmt.Sprintf("unique line %02d", i))
	}
	h.m.projects[0].snap.Open.Value[0].Context = strings.Join(context, "\n")
	h.send(press("enter"))
	initial := h.view()
	h.send(press("down"))
	down := h.view()
	if initial == down {
		t.Error("one down did not move the visible top")
	}
	h.send(press("up"))
	if h.view() != initial {
		t.Error("one up did not restore the initial view")
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if h.m.decisionScroll != 26 {
		t.Errorf("pgdown offset %d, want page 26", h.m.decisionScroll)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if h.m.decisionScroll != 0 {
		t.Error("pgup did not return one page")
	}
	for i := 0; i < 250; i++ {
		h.send(press("down"))
	}
	bottom := h.view()
	offset := h.m.decisionScroll
	h.send(press("up"))
	if h.view() == bottom || h.m.decisionScroll != offset-1 {
		t.Error("up after overscroll did not immediately move")
	}
}

func TestDecisionsFixHelpIsReadOnlyAndContextual(t *testing.T) {
	h := decisionsHarness(t)
	help := strings.Join(h.m.helpLines(120, 60), "\n")
	if strings.Contains(help, "a approve") || strings.Contains(help, "add project (Welcome)") {
		t.Error("Decisions help advertises approve or Add")
	}
	footer := "On Decisions, left/right switch tabs; [ and ] switch projects."
	if !strings.Contains(help, footer) || !strings.Contains(help, "1-9 jump to project\n") {
		t.Error("missing Decisions footer or plain project hint")
	}
	h.m.view = 0
	help = strings.Join(h.m.helpLines(120, 60), "\n")
	if strings.Contains(help, footer) || !strings.Contains(help, "a approve") {
		t.Error("Decisions help leaked into Overview")
	}
}

func TestDecisionsFixNoAdjProjectsEmptyState(t *testing.T) {
	h := decisionsHarness(t)
	for _, p := range h.m.projects {
		p.reg.Adj = false
	}
	h.m.switchTo(-1)
	if !strings.Contains(h.view(), "No project uses adj") {
		t.Fatal(h.view())
	}
}

func TestDecisionsFixNavigationClosesDetailAndResetsCursor(t *testing.T) {
	for _, project := range []bool{false, true} {
		t.Run(fmt.Sprint(project), func(t *testing.T) {
			h := decisionsHarness(t)
			h.send(press("down"))
			h.send(press("enter"))
			if !strings.Contains(h.view(), "esc returns") {
				t.Fatal("detail not open")
			}
			if project {
				h.send(press("2"))
			} else {
				h.send(press("tab"))
				h.send(press("shift+tab"))
			}
			if strings.Contains(h.view(), "esc returns") || h.m.decisionsCursor != 0 {
				t.Fatal("navigation kept detail or cursor")
			}
		})
	}
}
