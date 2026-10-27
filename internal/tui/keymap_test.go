// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"

	"github.com/squalor-xyz/ork/internal/adjcli"
)

// The keymap of S105 (ui.project-keys, adj 160): [ and ] switch projects in
// every view, left and right belong to the view's own tabs and fields, a is
// Approve, A toggles all projects on Decisions, + adds a project on Welcome,
// and n and p no longer move rows.

var viewNames = []string{"Overview", "Questions", "Waves", "Roadmap", "Decisions"}

// ] and [ step through Running, Needs you and the projects in sidebar order
// and wrap at both ends, in every view.
func TestBracketsSwitchProjectsAndWrapThroughNeedsYou(t *testing.T) {
	steps := []struct{ k, want string }{
		{"]", "slicer"},
		{"]", "adjudicate"},
		{"[", "slicer"},
		{"[", "Needs"},
		{"[", "Running"},
		{"[", "agents"},
		{"]", "Running"},
		{"]", "Needs"},
	}
	for view, name := range viewNames {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, four...)
			h.start()
			h.m.view = view
			for _, s := range steps {
				h.send(press(s.k))
				if got := activeTab(t, h.view()); got != s.want {
					t.Fatalf("after %q the tab is %s, want %s", s.k, got, s.want)
				}
				if h.m.view != view {
					t.Fatalf("after %q the view changed to %d", s.k, h.m.view)
				}
			}
		})
	}
}

// The Running entry sits between the last project and Needs you, so [ and ]
// wrap through it (S110, adj 169).
func TestBracketsWrapThroughTheRunningEntry(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(press("4")) // agents, the last project
	if h.m.active != 3 {
		t.Fatalf("setup: active = %d, want 3", h.m.active)
	}
	h.send(press("]"))
	if h.m.active != runningEntry {
		t.Fatalf("] from the last project: active = %d, want the Running entry", h.m.active)
	}
	h.send(press("]"))
	if h.m.active != needsEntry {
		t.Fatalf("] from Running: active = %d, want Needs you", h.m.active)
	}
	h.send(press("["))
	if h.m.active != runningEntry {
		t.Fatalf("[ from Needs you: active = %d, want the Running entry", h.m.active)
	}
	h.send(press("["))
	if h.m.active != 3 {
		t.Fatalf("[ from Running: active = %d, want the last project", h.m.active)
	}
}

// Left and right never change the project outside Decisions, where they
// switch tabs: not from a project and not from Needs you or Running.
func TestLeftRightDoNotSwitchProjects(t *testing.T) {
	for _, view := range []int{0, 1, 2, 3} {
		for _, start := range []int{0, needsEntry, runningEntry} {
			for _, k := range []string{"left", "right"} {
				h := decisionsHarness(t)
				h.m.view = view
				h.m.switchTo(start)
				h.send(press(k))
				if h.m.active != start {
					t.Errorf("%s: %s moved the project from %d to %d", viewNames[view], k, start, h.m.active)
				}
				if h.m.view != view {
					t.Errorf("%s: %s changed the view to %d", viewNames[view], k, h.m.view)
				}
			}
		}
	}
}

// Left and right stay inert while a launch is open, and [ and ] drop it like
// the other project keys do.
func TestLeftRightInertInLaunchBracketsSwitch(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(press("1"))
	h.m.launch = &launchState{stage: launchList, cancel: func() {}}
	for _, k := range []string{"left", "right"} {
		h.send(press(k))
		if h.m.active != 0 || h.m.launch == nil {
			t.Fatalf("%s left the launch: active %d, launch %v", k, h.m.active, h.m.launch)
		}
	}
	h.send(press("]"))
	if h.m.active != 1 || h.m.launch != nil {
		t.Fatalf("] did not switch and drop the launch: active %d, launch %v", h.m.active, h.m.launch)
	}
}

// A toggles all projects on Decisions and a does not (a is Approve, which
// Decisions never runs).
func TestAllProjectsToggleIsCapitalA(t *testing.T) {
	h := decisionsHarness(t)
	h.send(press("a"))
	if h.m.allProjects || strings.Contains(h.view(), "#91") {
		t.Fatalf("a toggled all projects:\n%s", h.view())
	}
	h.send(press("A"))
	if !h.m.allProjects || !strings.Contains(h.view(), "#91") {
		t.Fatalf("A did not show all projects:\n%s", h.view())
	}
	if !strings.Contains(h.view(), "A zeta only") {
		t.Errorf("the toggle hint does not name A:\n%s", h.view())
	}
	h.send(press("A"))
	if h.m.allProjects || strings.Contains(h.view(), "#91") {
		t.Fatalf("A did not return to one project:\n%s", h.view())
	}
	if !strings.Contains(h.view(), "A all projects") {
		t.Errorf("the toggle hint does not name A:\n%s", h.view())
	}
}

// A is not bound outside Decisions.
func TestCapitalAIsInertOutsideDecisions(t *testing.T) {
	for _, view := range []int{0, 1, 3} {
		h := decisionsHarness(t)
		h.m.view = view
		h.send(press("A"))
		if h.m.allProjects || h.m.confirm != nil || h.m.pane != nil {
			t.Errorf("%s: A did something", viewNames[view])
		}
	}
}

// + opens the path field on Welcome; a does not.
func TestWelcomeAddIsPlus(t *testing.T) {
	h, _ := welcomeHarness(t)
	h.deliver(press("a"))
	if h.m.welcome.editing {
		t.Fatal("a opened the path field")
	}
	if !strings.Contains(h.view(), "No projects yet.") {
		t.Fatalf("Welcome does not name +:\n%s", h.view())
	}
	h.deliver(press("+"))
	if !h.m.welcome.editing || !strings.Contains(h.view(), "Project path:") {
		t.Fatalf("+ did not open the path field:\n%s", h.view())
	}
	h.deliver(press("+")) // typed into the field
	if got := h.m.welcome.field.Value(); got != "+" {
		t.Errorf("a + typed in the field = %q", got)
	}
}

// n and p are not row keys any more. n is No in a confirm.
func TestNAndPNoLongerMoveRows(t *testing.T) {
	t.Run("overview", func(t *testing.T) {
		h := newHarness(t, four...)
		h.src.set(snap(four[0], []adjcli.Record{openRec(11, "a.one", "first question"), openRec(12, "a.two", "second question")}, nil))
		h.start()
		h.view()
		for _, k := range []string{"n", "p"} {
			h.send(press(k))
			if h.m.cursor != 0 || h.m.active != needsEntry {
				t.Errorf("%s moved: cursor %d, active %d", k, h.m.cursor, h.m.active)
			}
		}
	})
	t.Run("decisions", func(t *testing.T) {
		h := decisionsHarness(t)
		h.send(press("down"))
		if h.m.decisionsCursor != 1 {
			t.Fatalf("setup: down left the cursor at %d", h.m.decisionsCursor)
		}
		for _, k := range []string{"n", "p"} {
			h.send(press(k))
			if h.m.decisionsCursor != 1 {
				t.Errorf("%s moved the cursor to %d", k, h.m.decisionsCursor)
			}
		}
	})
	t.Run("confirm", func(t *testing.T) {
		h := actionHarness(t, "awaiting-approval", "")
		h.deliver(press("x"))
		if h.m.confirm == nil {
			t.Fatal("setup: no confirm")
		}
		h.deliver(press("n"))
		if h.m.confirm != nil || h.m.actionStatus != "Cancelled" {
			t.Errorf("n did not answer No: confirm %v, status %q", h.m.confirm, h.m.actionStatus)
		}
	})
}

// The help and the hints name the new keys and no longer name the old ones.
func TestHelpNamesTheNewKeys(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(press("?"))
	v := h.view()
	for _, want := range []string{"] next project", "[ previous project", "j/k down/up row", "+ add project", "- remove active project"} {
		if !strings.Contains(v, want) {
			t.Errorf("help misses %q:\n%s", want, v)
		}
	}
	for _, gone := range []string{"down/j/n", "up/k/p", "left previous project", "right next project", "a add project"} {
		if strings.Contains(v, gone) {
			t.Errorf("help still holds %q:\n%s", gone, v)
		}
	}
}

// Where left and right do nothing (Overview, Questions, Waves and Roadmap
// outside a pane or field), the status line says which keys switch projects
// (S144). Decisions uses them for its tabs and says nothing.
func TestStatusNudgeWhenLeftRightDoNothing(t *testing.T) {
	const nudge = "[ and ] switch projects"
	for _, view := range []int{0, 1, 2, 3} {
		for _, k := range []string{"left", "right"} {
			t.Run(viewNames[view]+" "+k, func(t *testing.T) {
				h := decisionsHarness(t)
				h.m.view = view
				h.m.actionStatus = ""
				h.send(press(k))
				if h.m.actionStatus != nudge {
					t.Errorf("status = %q, want %q", h.m.actionStatus, nudge)
				}
				if !strings.Contains(h.view(), nudge) {
					t.Errorf("the nudge is not on screen:\n%s", h.view())
				}
			})
		}
	}
	h := decisionsHarness(t)
	h.m.view = 0
	h.send(press("?"))
	h.send(press("left"))
	if h.m.actionStatus != "" {
		t.Errorf("the nudge was set under the help: %q", h.m.actionStatus)
	}
	h = decisionsHarness(t)
	h.send(press("right"))
	if h.m.decisionsTab != 1 || h.m.actionStatus != "" || strings.Contains(h.view(), "switch projects") {
		t.Errorf("Decisions: tab %d, status %q", h.m.decisionsTab, h.m.actionStatus)
	}
}

// A shows all waves, as it shows all projects on Decisions; a is Approve and
// does nothing on Waves (S144).
func TestWavesShowAllIsCapitalA(t *testing.T) {
	h, l := wavesHarness(t)
	for range 3 {
		l.suggest.Waves = append(l.suggest.Waves, l.suggest.Waves[1])
	}
	openWaves(t, h)
	hidden := len(h.m.waves.rows())
	body := func() string { return sgr.ReplaceAllString(strings.Join(h.m.wavesBody(72).body, "\n"), "") }
	if !strings.Contains(body(), "more waves; [A] show all") {
		t.Errorf("the hidden-waves line does not name A:\n%s", body())
	}
	h.deliver(press("a"))
	if h.m.waves.showAll || len(h.m.waves.rows()) != hidden || l.writes != 0 {
		t.Fatalf("a showed all waves: showAll %v, rows %d, want %d", h.m.waves.showAll, len(h.m.waves.rows()), hidden)
	}
	h.m.waves.cursor = len(h.m.waves.rows()) - 1 // Unplaced D
	h.deliver(press("A"))
	if !h.m.waves.showAll || len(h.m.waves.rows()) <= hidden {
		t.Fatalf("A did not show all waves: showAll %v, rows %d", h.m.waves.showAll, len(h.m.waves.rows()))
	}
	if got := h.m.waves.rows()[h.m.waves.cursor].ID; got != "D" {
		t.Errorf("A moved the cursor to %s", got)
	}
	if strings.Contains(body(), "show all") {
		t.Errorf("the show-all line stays after A:\n%s", body())
	}
	h.m.waves.showAll = false
	h.deliver(press("?"))
	if !strings.Contains(h.view(), "Waves: A shows all waves.") {
		t.Errorf("the Waves help does not name A:\n%s", h.view())
	}
}

// The tab keys' help strings were never shown (not in FullHelp); the keys
// carry none, and the Decisions help line names left and right (S144).
func TestTabKeysCarryNoHiddenHelp(t *testing.T) {
	k := newKeyMap()
	for name, b := range map[string]key.Binding{"TabNext": k.TabNext, "TabPrev": k.TabPrev} {
		if h := b.Help(); h.Key != "" || h.Desc != "" {
			t.Errorf("%s carries help %q %q that no help view shows", name, h.Key, h.Desc)
		}
		if len(b.Keys()) != 1 {
			t.Errorf("%s keys = %v", name, b.Keys())
		}
	}
}

// [ and ] also land on Running and Needs you, so the help says entries and
// not only projects (S144).
func TestHelpSaysProjectsAndEntries(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(press("?"))
	v := h.view()
	for _, want := range []string{"] next project or entry", "[ previous project or entry"} {
		if !strings.Contains(v, want) {
			t.Errorf("help misses %q:\n%s", want, v)
		}
	}
	h.m.view = decisionsView
	help := strings.Join(h.m.helpLines(120, 60), "\n")
	if !strings.Contains(help, "Decisions: left/right switch tabs; [ and ] switch projects or entries.") {
		t.Errorf("the Decisions line does not say entries:\n%s", help)
	}
}

// The full help fits the standard terminal (100x30; h.view always renders
// that size) without a "more lines" marker (S144). A real 80x24 is shorter and
// cuts the help; there the way out stays visible.
func TestFullHelpGoldenFits(t *testing.T) {
	for _, view := range []int{0, 1, 3} { // Waves and Decisions add lines of their own
		t.Run(viewNames[view], func(t *testing.T) {
			h := newHarness(t, four...)
			h.start()
			h.m.view = view
			h.send(press("?"))
			v := h.view()
			if strings.Contains(v, "more lines") {
				t.Errorf("the help is cut:\n%s", v)
			}
			if !strings.Contains(v, "esc close run, question, item, decision, help or message") {
				t.Errorf("the help misses its last key:\n%s", v)
			}
		})
	}
}

// The view's own help line sits at the top of the help, so the cut at the
// bottom of a standard terminal never hides it (S144): Decisions names
// left/right and the bracket keys, Waves names A.
func TestViewHelpLinesStayVisibleAt100x30(t *testing.T) {
	for _, tc := range []struct {
		view int
		want string
	}{
		{decisionsView, "Decisions: left/right switch tabs; [ and ] switch projects or entries."},
		{wavesView, "Waves: A shows all waves."},
	} {
		t.Run(viewNames[tc.view], func(t *testing.T) {
			h := newHarness(t, four...)
			h.start()
			h.m.view = tc.view
			h.send(press("?"))
			v := h.view()
			at, keys := strings.Index(v, tc.want), strings.Index(v, "tab next view")
			if at < 0 || at > keys {
				t.Errorf("the help misses %q or puts it below the keys:\n%s", tc.want, v)
			}
		})
	}
}
