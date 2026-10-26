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

func questionsHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, proj("zeta", true), proj("alpha", true), proj("plain", false))
	for _, p := range h.m.projects {
		s := snap(p.reg, []adjcli.Record{openRec(9, "owner.first", "First choice"), openRec(2, "owner.second", "Second choice")}, nil)
		s.EngineRuns = []source.EngineRun{{ID: "engine-" + p.reg.Name, Item: "S27", State: "paused", Pending: "question", Wait: "Choose storage"}}
		h.src.set(s)
	}
	h.start()
	h.m.view = questionsView
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return h
}

func TestQuestionsRowsAndScope(t *testing.T) {
	h := questionsHarness(t)
	body := func() string {
		lines := strings.Split(h.view(), "\n")[2:14]
		for i, line := range lines {
			_, lines[i], _ = strings.Cut(line, "│")
		}
		return strings.Join(lines, "\n")
	}
	v := body()
	last := -1
	for _, text := range []string{"zeta   engine-zeta", "zeta   #9 owner.first", "zeta   #2 owner.second", "alpha  engine-alpha", "alpha  #9 owner.first", "alpha  #2 owner.second", "plain  engine-plain"} {
		i := strings.Index(v, text)
		if i <= last {
			t.Fatalf("missing or out-of-order question %q:\n%s", text, v)
		}
		last = i
	}
	if strings.Contains(v, "plain  #") {
		t.Fatal("non-adj project supplied adj rows")
	}
	for _, k := range []string{"]", "]", "[", "3", "/", "z", "enter", "esc"} {
		h.deliver(press(k))
		if got := body(); got != v {
			t.Fatalf("%s changed all-project questions:\n%s", k, got)
		}
	}
}

func TestQuestionsSkipsIneligible(t *testing.T) {
	h := questionsHarness(t)
	p := h.m.projects[0]
	p.snap.EngineRuns = []source.EngineRun{
		{ID: "removed-run", State: "paused", Pending: "question", Unavailable: true},
		{ID: "not-paused", State: "implement", Pending: "question"},
		{ID: "not-question", State: "paused", Pending: "shape"},
		{ID: "superseded", Item: "S1", State: "stopped", Pending: "question", CreatedAt: t0},
		{ID: "newer", Item: "S1", State: "implement", CreatedAt: t0.Add(1)},
	}
	h.m.projects[1].snap.Unavailable = true
	v := h.view()
	for _, text := range []string{"removed-run", "not-paused", "not-question", "superseded", "newer", "engine-alpha", "alpha  #"} {
		if strings.Contains(v, text) {
			t.Errorf("ineligible question %q displayed:\n%s", text, v)
		}
	}
}

func TestQuestionsEngineAnswer(t *testing.T) {
	h := actionHarness(t, "paused", "question")
	h.m.view = 1
	h.deliver(press("enter"))
	if h.m.pane == nil || !strings.Contains(h.view(), "Answer:") {
		t.Fatal("engine question did not open answer pane")
	}
	typeAnswer(h, "use local storage")
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "answered") || strings.Contains(v, "Answer:") {
		t.Fatalf("answer failed:\n%s", v)
	}
	h.deliver(press("esc"))
	if h.m.pane != nil || !strings.Contains(h.view(), "No open questions") {
		t.Fatal(h.view())
	}
}

func TestQuestionsAdjReadOnlyDetail(t *testing.T) {
	h := questionsHarness(t)
	r := &h.m.projects[0].snap.Open.Value[0]
	r.Context = strings.Repeat("Long context for scrolling. ", 200)
	r.Options = []adjcli.Option{{Label: "A", Summary: "Files"}}
	r.Recommendation = &adjcli.Recommendation{Label: "A", Text: "Use files"}
	h.deliver(press("down"))
	h.src.take()
	h.deliver(press("enter"))
	if h.m.pane != nil || h.m.decisionPane == nil || !strings.Contains(h.view(), "zeta #9") {
		t.Fatal("adj question did not open record detail")
	}
	if calls := h.src.take(); len(calls) != 0 {
		t.Fatalf("opening detail fetched snapshots: %v", calls)
	}
	for _, k := range []string{"down", "j", "pgdown"} {
		old := h.m.decisionScroll
		h.deliver(press(k))
		if h.m.decisionScroll <= old {
			t.Fatalf("%s did not scroll detail", k)
		}
	}
	for _, k := range []string{"up", "k", "pgup"} {
		old := h.m.decisionScroll
		h.deliver(press(k))
		if h.m.decisionScroll >= old {
			t.Fatalf("%s did not scroll back", k)
		}
	}
	for _, k := range []string{"a", "x", "t", "c", "enter"} {
		_, cmd := h.m.Update(press(k))
		if cmd != nil || h.m.confirm != nil || h.m.pane != nil || h.m.actionStatus != "" {
			t.Fatalf("%s acted on read-only record", k)
		}
	}
	for range 10 {
		h.deliver(press("pgdown"))
	}
	if v := h.view(); !strings.Contains(v, "A: Files") || !strings.Contains(v, "Recommendation: A") {
		t.Fatal("missing record recommendation")
	}
	h.deliver(press("esc"))
	if h.m.decisionPane != nil || !strings.Contains(h.view(), "engine-zeta") {
		t.Fatal("esc did not return to questions")
	}
}

func TestQuestionsEmptyAndDisplayOnly(t *testing.T) {
	h := newHarness(t, proj("pending", true), proj("failed", true), proj("ready", true))
	h.m.projects[1].loaded = true
	h.m.projects[1].snap.Open.Err = &clirun.ToolError{Code: "locked", Message: "Inbox busy"}
	h.m.projects[2].loaded = true
	h.m.projects[2].snap.Open.Value = []adjcli.Record{openRec(8, "owner.ready", "Ready choice")}
	h.m.view = 1
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	v := h.view()
	for _, text := range []string{"~ pending  pending  waiting for the first reply", "! error    failed   inbox: locked: Inbox busy"} {
		if !strings.Contains(v, text) {
			t.Fatalf("missing %q:\n%s", text, v)
		}
	}
	for _, k := range []string{"up", "down", "enter"} {
		h.deliver(press(k))
	}
	if h.m.decisionPane == nil || h.m.decisionPane.record.ID != 8 {
		t.Fatal("display-only row took cursor")
	}
	h.deliver(press("esc"))
	for _, p := range h.m.projects {
		p.loaded = true
		p.snap = source.Snapshot{}
	}
	if !strings.Contains(h.view(), "No open questions") {
		t.Fatal("empty state missing")
	}
	h.deliver(press("enter"))
	if h.m.pane != nil || h.m.decisionPane != nil {
		t.Fatal("empty list opened pane")
	}
}

func TestQuestionsNoListActions(t *testing.T) {
	for _, state := range []string{"awaiting-approval", "paused", "implement", "committed"} {
		for _, k := range []string{"a", "x", "t", "c"} {
			t.Run(state+"/"+k, func(t *testing.T) {
				h := actionHarness(t, state, "shape")
				h.m.view = 1
				if _, ok := h.m.selectedRow(); ok {
					t.Fatal("Questions selected a hidden Overview row")
				}
				_, cmd := h.m.Update(press(k))
				if cmd != nil || h.m.confirm != nil || h.m.actionStatus != "" || h.m.allProjects {
					t.Fatalf("%s acted on hidden Overview row", k)
				}
			})
		}
	}
}

func TestQuestionsViewOrderAndReset(t *testing.T) {
	h := questionsHarness(t)
	h.m.view = 0
	for _, want := range []int{1, 2, 3, 4, 0} {
		h.deliver(press("tab"))
		if h.m.view != want {
			t.Fatalf("tab view = %d, want %d", h.m.view, want)
		}
	}
	for _, want := range []int{4, 3, 2, 1, 0} {
		h.deliver(press("shift+tab"))
		if h.m.view != want {
			t.Fatalf("shift+tab view = %d, want %d", h.m.view, want)
		}
	}
	h.m.view = 1
	h.deliver(press("down"))
	h.deliver(press("]"))
	h.deliver(press("enter"))
	if h.m.pane == nil || h.m.pane.run != "engine-zeta" {
		t.Fatal("project switch did not reset Questions cursor")
	}
	h.deliver(press("tab"))
	if h.m.pane == nil || h.m.view != questionsView {
		t.Fatal("tab escaped the engine pane")
	}
	h.deliver(press("esc"))
	h.deliver(press("tab"))
	if h.m.pane != nil || h.m.decisionPane != nil {
		t.Fatal("view switch retained pane")
	}
}

func TestQuestionsGolden(t *testing.T) {
	for _, mono := range []bool{false, true} {
		name := "color"
		if mono {
			name = "mono"
		}
		t.Run(name, func(t *testing.T) {
			h := questionsHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			checkGolden(t, fmt.Sprintf("questions-100x30-%s.txt", name), h.m.View().Content)
		})
	}
}

func TestQuestionsNameWidthAndHelp(t *testing.T) {
	h := newHarness(t, proj("abcdefghijklmnopqrstuvwxyz", true), proj("short", true))
	for _, p := range h.m.projects {
		h.src.set(snap(p.reg, []adjcli.Record{openRec(1, "owner.choice", "Choose")}, nil))
	}
	h.start()
	h.m.view = questionsView
	lines := h.m.questions(72).body
	for i, prefix := range []string{"  ? decision abcdefghijklmnopq...  #1", "  ? decision short                 #1"} {
		if !strings.HasPrefix(sgr.ReplaceAllString(lines[i+1], ""), prefix) {
			t.Fatalf("project column = %q, want prefix %q", lines[i+1], prefix)
		}
	}
	if strings.Contains(strings.Join(h.m.helpLines(120, 60), "\n"), "a approve") || strings.Contains(strings.Join(h.m.helpLines(120, 60), "\n"), "toggle all projects") {
		t.Fatal("Questions advertises an a binding")
	}
	h.deliver(press("enter"))
	h.deliver(press("]"))
	if h.m.decisionPane != nil || h.m.pane != nil || h.m.questionsCursor != 0 {
		t.Fatal("project switch retained question detail")
	}
	h.deliver(press("down"))
	h.deliver(press("enter"))
	h.deliver(press("tab"))
	if h.m.decisionPane != nil || h.m.pane != nil || h.m.questionsCursor != 0 {
		t.Fatal("view switch retained question detail or cursor")
	}
}

// A Questions pane keeps its run when the next snapshot changes its state.
func TestQuestionsPaneActionsMatchOverview(t *testing.T) {
	for _, tc := range []struct{ state, pending, key, hint string }{
		{"awaiting-approval", "", "a", "a approve; x stop"},
		{"awaiting-approval", "", "x", "a approve; x stop"},
		{"implement", "", "x", "x stop"},
		{"paused", "shape", "t", "t retry"},
	} {
		t.Run(tc.state+"/"+tc.key, func(t *testing.T) {
			var statuses []string
			var confirmations []string
			for _, view := range []int{0, questionsView} {
				h := actionHarness(t, "paused", "question")
				h.m.view = view
				h.deliver(press("enter"))
				if h.m.pane == nil {
					t.Fatal("question did not open pane")
				}
				s := h.m.projects[0].snap
				s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
				s.EngineRuns[0].State, s.EngineRuns[0].Pending = tc.state, tc.pending
				h.src.set(s)
				h.deliver(snapshotMsg{name: "ork", snap: s})
				if !strings.Contains(h.view(), tc.hint) {
					t.Fatalf("footer missing %q", tc.hint)
				}
				h.deliver(press(tc.key))
				statuses = append(statuses, h.m.actionStatus)
				confirm := ""
				if h.m.confirm != nil {
					confirm = h.m.confirm.prompt()
				}
				confirmations = append(confirmations, confirm)
			}
			if statuses[0] != statuses[1] || confirmations[0] != confirmations[1] {
				t.Fatalf("pane action differs: status %q, confirmation %q", statuses, confirmations)
			}
			if statuses[1] == "" && confirmations[1] == "" {
				t.Fatal("advertised pane action did nothing")
			}
		})
	}
}

func TestQuestionsHelpHidesListActions(t *testing.T) {
	h := questionsHarness(t)
	help := strings.Join(h.m.helpLines(120, 60), "\n")
	for _, binding := range []string{"a approve", "x stop", "t retry", "c remove merged worktree", "toggle all projects"} {
		if strings.Contains(help, binding) {
			t.Errorf("list help advertises inert action %q", binding)
		}
	}
}

func TestQuestionsPresentation(t *testing.T) {
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprint(mono), func(t *testing.T) {
			h := questionsHarness(t)
			th := NewTheme(mono)
			h.m.cfg.Theme = th
			out := h.m.questions(72)
			if out.body[0] != th.Heading.Render("All projects") {
				t.Errorf("missing scope heading")
			}
			if out.sel != 1 {
				t.Errorf("selected body line = %d, want 1", out.sel)
			}
			if out.body[len(out.body)-1] != indent+"[enter] open" {
				t.Errorf("missing open hint")
			}
			for _, text := range []string{"? question zeta", "? decision zeta"} {
				if !strings.Contains(sgr.ReplaceAllString(strings.Join(out.body, "\n"), ""), text) {
					t.Errorf("missing kind %q", text)
				}
			}
			h.deliver(press("down"))
			out = h.m.questions(72)
			if out.sel != 2 {
				t.Errorf("selected body line after down = %d, want 2", out.sel)
			}
			for _, p := range h.m.projects {
				p.snap = source.Snapshot{}
			}
			out = h.m.questions(72)
			if len(out.body) != 2 || out.body[1] != indent+th.Dim.Render("No open questions") || out.sel != -1 {
				t.Errorf("empty presentation = %#v", out)
			}
		})
	}
}

func TestQuestionsScrollKeepsCursorVisible(t *testing.T) {
	h := newHarness(t, proj("many", true))
	var records []adjcli.Record
	for i := 1; i <= 40; i++ {
		records = append(records, openRec(i, "owner.choice", fmt.Sprintf("Choice %d", i)))
	}
	h.src.set(snap(h.m.projects[0].reg, records, nil))
	h.start()
	h.m.view = questionsView
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
	for range 39 {
		h.deliver(press("down"))
	}
	out := h.m.questions(72)
	if out.sel != 40 {
		t.Fatalf("selected body line = %d, want 40", out.sel)
	}
	if v := plainView(h.m); !strings.Contains(v, "#40 owner.choice") {
		t.Fatalf("last question scrolled out of view:\n%s", v)
	}
	for range 39 {
		h.deliver(press("up"))
	}
	if v := plainView(h.m); !strings.Contains(v, "All projects") || !strings.Contains(v, "#1 owner.choice") {
		t.Fatalf("first question scrolled out of view:\n%s", v)
	}
}
