// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// hintLine is the line above the status line, without the sidebar column.
func hintLine(view string) string {
	lines := strings.Split(view, "\n")
	line := lines[len(lines)-2]
	if _, rest, ok := strings.Cut(line, "│"); ok {
		line = rest
	}
	return strings.TrimSpace(line)
}

// wideHint is the hint line on a screen wide enough to hold every hint.
func wideHint(h *harness) string {
	h.send(tea.WindowSizeMsg{Width: 200, Height: 40})
	return hintLine(plainView(h.m))
}

func isRule(line string) bool {
	if _, rest, ok := strings.Cut(line, "│"); ok {
		line = rest
	}
	line = strings.TrimSpace(line)
	return line != "" && strings.Trim(line, "─") == ""
}

// The hint line is one extra line above the status line (S149). The status line
// keeps its place, and the body loses exactly one row.
func TestHintBarSitsAboveTheStatusLine(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			h := newHarness(t, four...)
			h.start()
			h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			lines := strings.Split(plainView(h.m), "\n")
			if len(lines) != size[1] {
				t.Fatalf("%d lines, want %d", len(lines), size[1])
			}
			last := lines[len(lines)-1]
			if !strings.Contains(last, "4 projects") || !strings.Contains(last, "refreshed") {
				t.Errorf("the status line is not last: %q", last)
			}
			hint := hintLine(strings.Join(lines, "\n"))
			for _, want := range []string{"tab", "? help", "q quit"} {
				if !strings.Contains(hint, want) {
					t.Errorf("hint line lacks %q: %q", want, hint)
				}
			}
			// The view bar, a rule, the body, a rule, the hint and the status line.
			if !isRule(lines[size[1]-3]) || !isRule(lines[1]) {
				t.Errorf("rules are not where the layout puts them:\n%s", strings.Join(lines, "\n"))
			}
			if body := size[1] - 5; !isRule(lines[1+body+1]) {
				t.Errorf("the body is not %d rows:\n%s", body, strings.Join(lines, "\n"))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > size[0] {
					t.Errorf("line %d is %d wide: %q", i, w, l)
				}
			}
		})
	}
}

// pressedOnRow reports whether key a, x or t is accepted on the row under the
// cursor: it opens a confirmation.
func pressedOnRow(t *testing.T, state, pending, k string) bool {
	t.Helper()
	h := actionHarness(t, state, pending)
	h.deliver(press(k))
	return h.m.confirm != nil
}

func TestHintBarFollowsTheCursorRow(t *testing.T) {
	t.Run("decision row", func(t *testing.T) {
		h := newHarness(t, four...)
		h.src.set(snap(four[0], []adjcli.Record{openRec(1, "a.b", "a question")}, nil))
		h.start()
		hint := hintLine(h.view())
		for _, want := range []string{"enter go to project", "tab views", "q quit", "? help"} {
			if !strings.Contains(hint, want) {
				t.Errorf("hint lacks %q: %q", want, hint)
			}
		}
		for _, bad := range []string{"a approve", "x stop", "t retry", "esc"} {
			if strings.Contains(hint, bad) {
				t.Errorf("a decision row offers %q: %q", bad, hint)
			}
		}
	})

	// The hint lists a, x and t exactly when the row accepts them: the same
	// checks refuse the key and hide it from the hint.
	for _, tc := range []struct{ state, pending string }{
		{"awaiting-approval", ""},
		{"paused", ""},
		{"paused", "question"},
		{"paused", "budget"},
		{"stopped", ""},
		{"implement", ""},
		{"committed", ""},
	} {
		t.Run(tc.state+"/"+tc.pending, func(t *testing.T) {
			h := actionHarness(t, tc.state, tc.pending)
			hint := hintLine(h.view())
			for _, k := range []struct{ key, word string }{{"a", "a approve"}, {"x", "x stop"}, {"t", "t retry"}} {
				listed := strings.Contains(hint, k.word)
				if accepted := pressedOnRow(t, tc.state, tc.pending, k.key); listed != accepted {
					t.Errorf("hint %q: lists %q = %v, but the row accepts the key = %v", hint, k.word, listed, accepted)
				}
			}
		})
	}

	t.Run("named rows", func(t *testing.T) {
		for _, tc := range []struct {
			state, pending, want string
		}{
			{"awaiting-approval", "", "a approve"},
			{"awaiting-approval", "", "x stop"},
			{"paused", "", "t retry"},
		} {
			if hint := hintLine(actionHarness(t, tc.state, tc.pending).view()); !strings.Contains(hint, tc.want) || !strings.Contains(hint, "enter open") {
				t.Errorf("%s row: hint %q lacks %q or enter open", tc.state, hint, tc.want)
			}
		}
	})

	t.Run("open pane", func(t *testing.T) {
		h := paneHarness(t, "awaiting-approval", "")
		hint := hintLine(h.view())
		for _, want := range []string{"esc close pane", "pgup/pgdown", "ctrl+d/u", "a approve", "x stop"} {
			if !strings.Contains(hint, want) {
				t.Errorf("pane hint lacks %q: %q", want, hint)
			}
		}
		if strings.Contains(hint, "enter") {
			t.Errorf("an approval pane offers enter: %q", hint)
		}
	})

	t.Run("question pane field", func(t *testing.T) {
		h := paneHarness(t, "paused", "question")
		hint := hintLine(h.view())
		// ctrl+u edits the answer field, so only ctrl+d scrolls.
		if !strings.Contains(hint, "enter") || !strings.Contains(hint, "esc") || strings.Contains(hint, "ctrl+d/u") || !strings.Contains(hint, "ctrl+d") {
			t.Errorf("question pane hint: %q", hint)
		}
	})

	t.Run("roadmap", func(t *testing.T) {
		h := roadmapHarness(t)
		h.m.cfg.Live = &actionLive{src: h.src}
		hint := hintLine(h.view())
		if !strings.Contains(hint, "l launch") || !strings.Contains(hint, "enter") {
			t.Errorf("roadmap hint: %q", hint)
		}
		h.deliver(press("l"))
		if h.m.launch == nil {
			t.Fatal("l did not open the launch flow")
		}
	})

	t.Run("roadmap without a daemon launches nothing it lists", func(t *testing.T) {
		h := roadmapHarness(t)
		h.m.projects[0].snap.Unavailable = true
		if hint := hintLine(h.view()); strings.Contains(hint, "l launch") {
			t.Errorf("a removed project offers l: %q", hint)
		}
	})

	t.Run("read-only pane", func(t *testing.T) {
		h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
		h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
		v := plainView(h.m)
		hint := hintLine(v)
		if !strings.Contains(hint, "started elsewhere, read-only; esc close pane") {
			t.Errorf("the read-only message is not on the hint line: %q", hint)
		}
		if n := strings.Count(v, "started elsewhere, read-only"); n != 1 {
			t.Errorf("the message is on screen %d times:\n%s", n, v)
		}
		if !strings.Contains(hint, "pgup/pgdown") {
			t.Errorf("the read-only pane scrolls: %q", hint)
		}
		for _, bad := range []string{"a approve", "x stop", "t retry", "enter"} {
			if strings.Contains(hint, bad) {
				t.Errorf("a read-only pane offers %q: %q", bad, hint)
			}
		}
	})
}

func TestHintBarFitsNarrowScreens(t *testing.T) {
	for _, width := range []int{80, 60, 40, 30} {
		h := actionHarness(t, "awaiting-approval", "")
		h.send(tea.WindowSizeMsg{Width: width, Height: 24})
		for _, l := range strings.Split(plainView(h.m), "\n") {
			if w := lipgloss.Width(l); w > width {
				t.Errorf("width %d: line is %d wide: %q", width, w, l)
			}
		}
		// A hint is dropped whole or kept whole.
		hint := hintLine(plainView(h.m))
		// q quit never drops (S152), so at the narrowest width it and ? help
		// are all that fit beside the sidebar.
		if !strings.HasPrefix(hint, "enter") && !(width == 30 && hint == "q quit; ? help") {
			t.Errorf("width %d: the row's own key is not first: %q", width, hint)
		}
		if strings.HasSuffix(hint, ";") || strings.HasSuffix(hint, " a") || strings.HasSuffix(hint, "...") {
			t.Errorf("width %d: hint cut inside a key: %q", width, hint)
		}
	}
}

// ctrl+d and ctrl+u scroll an open pane by half of it (S149).
func TestCtrlDAndCtrlUScrollThePane(t *testing.T) {
	h := actionHarness(t, "paused", "shape")
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	var reason []string
	for i := 1; i <= 80; i++ {
		reason = append(reason, fmt.Sprintf("reason line %02d", i))
	}
	s.EngineRuns[0].Wait = strings.Join(reason, "\n")
	h.src.set(s)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.deliver(press("enter"))
	top := h.view()
	if !strings.Contains(top, "reason line 01") || strings.Contains(top, "reason line 80") {
		t.Fatalf("the pane does not start at the top of a long reason:\n%s", top)
	}
	ctrl := func(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }
	h.deliver(ctrl('d'))
	one := h.m.pane.offset
	if one < 2 {
		t.Fatalf("ctrl+d moved the pane %d lines, want about half of it", one)
	}
	if v := h.view(); strings.Contains(v, "reason line 01\n") || v == top {
		t.Fatalf("ctrl+d did not scroll the pane:\n%s", v)
	}
	h.deliver(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if h.m.pane.offset != one+1 {
		t.Errorf("pgdown moved the pane %d lines after ctrl+d, want 1", h.m.pane.offset-one)
	}
	h.deliver(tea.KeyPressMsg{Code: tea.KeyPgUp})
	h.deliver(ctrl('u'))
	if h.m.pane.offset != 0 {
		t.Fatalf("ctrl+u did not scroll back to the top: offset %d", h.m.pane.offset)
	}
	for i := 0; i < 20; i++ {
		h.deliver(ctrl('d'))
	}
	if v := h.view(); !strings.Contains(v, "reason line 80") {
		t.Fatalf("ctrl+d does not reach the end of the pane:\n%s", v)
	}
	h.deliver(ctrl('d')) // stays at the end
	if v := h.view(); !strings.Contains(v, "reason line 80") {
		t.Fatalf("ctrl+d past the end lost the last line:\n%s", v)
	}
}

// In an answer field ctrl+u still clears the field (S149); ctrl+d scrolls.
func TestCtrlUInAnAnswerFieldStillEditsTheField(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	typeAnswer(h, "old words")
	h.deliver(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if got := h.m.pane.input.Value(); got != "" {
		t.Fatalf("ctrl+u left %q in the field", got)
	}
	if h.m.pane.offset != 0 {
		t.Fatalf("ctrl+u scrolled the pane to %d", h.m.pane.offset)
	}
	typeAnswer(h, "new")
	h.deliver(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if got := h.m.pane.input.Value(); got != "new" {
		t.Fatalf("ctrl+d changed the field to %q", got)
	}
}

func TestQuestionsTabShowsItsCount(t *testing.T) {
	h := questionsHarness(t)
	want := len(h.m.selectableQuestions())
	if want == 0 {
		t.Fatal("the fixture has no questions")
	}
	bar := strings.Split(h.view(), "\n")[0]
	if !strings.Contains(bar, fmt.Sprintf("Questions %d", want)) {
		t.Errorf("the Questions tab lacks its count %d: %q", want, bar)
	}
	// The count is the rows the view lets the owner select, on any view.
	h.m.view = 0
	if bar := strings.Split(h.view(), "\n")[0]; !strings.Contains(bar, fmt.Sprintf("Overview │ Questions %d │ Waves", want)) {
		t.Errorf("the count is not on the Overview bar: %q", bar)
	}
	// No questions: the name stands alone.
	empty := newHarness(t, proj("zeta", true))
	empty.start()
	if bar := strings.Split(empty.view(), "\n")[0]; !strings.Contains(bar, "Questions │ Waves") || strings.Contains(bar, "Questions 0") {
		t.Errorf("an empty Questions tab: %q", bar)
	}
}

// With no daemon connection every run key refuses, so the hint lists none.
func TestHintBarListsNoRunKeysWithoutADaemon(t *testing.T) {
	h := actionHarness(t, "awaiting-approval", "")
	h.m.cfg.Live = nil
	hint := hintLine(h.view())
	for _, bad := range []string{"a approve", "x stop", "t retry"} {
		if strings.Contains(hint, bad) {
			t.Errorf("no daemon, yet the hint offers %q: %q", bad, hint)
		}
	}
	if !strings.Contains(hint, "enter open") {
		t.Errorf("enter still opens the run: %q", hint)
	}
}

// A merged Ready row takes c and nothing else; the hint lists exactly what the
// row accepts.
func TestHintBarListsCleanupOnlyOnAMergedReadyRow(t *testing.T) {
	for _, merged := range []string{"true", "false", "null"} {
		t.Run(merged, func(t *testing.T) {
			h, _ := cleanupHarness(t, merged)
			hint := hintLine(h.view())
			for _, k := range []struct{ key, word string }{{"c", "c clean up"}, {"a", "a approve"}, {"x", "x stop"}, {"t", "t retry"}} {
				fresh, _ := cleanupHarness(t, merged)
				fresh.deliver(press(k.key))
				if listed, accepted := strings.Contains(hint, k.word), fresh.m.confirm != nil; listed != accepted {
					t.Errorf("merged %s: hint %q lists %q = %v, but the row accepts the key = %v", merged, hint, k.word, listed, accepted)
				}
			}
			if !strings.Contains(hint, "enter open") {
				t.Errorf("Ready row must offer its run pane: %q", hint)
			}
		})
	}
}

func TestHintBarOnTheOtherViews(t *testing.T) {
	t.Run("questions list and record", func(t *testing.T) {
		h := questionsHarness(t)
		hint := hintLine(h.view())
		if !strings.Contains(hint, "enter open") || strings.Contains(hint, "esc") {
			t.Errorf("questions hint: %q", hint)
		}
		h.m.cursor = 0
		h.m.questionsCursor = 1 // an adj record
		h.deliver(press("enter"))
		if h.m.decisionPane == nil {
			t.Fatal("enter did not open the record")
		}
		hint = hintLine(h.view())
		if !strings.Contains(hint, "esc close") || !strings.Contains(hint, "pgup/pgdown page") || strings.Contains(hint, "enter") {
			t.Errorf("record hint: %q", hint)
		}
	})

	t.Run("decisions", func(t *testing.T) {
		h := decisionsHarness(t)
		hint := hintLine(h.view())
		for _, want := range []string{"enter open", "left/right switch tab", "A all projects"} {
			if !strings.Contains(hint, want) {
				t.Errorf("decisions hint lacks %q: %q", want, hint)
			}
		}
	})

	t.Run("waves", func(t *testing.T) {
		h, _ := wavesHarness(t)
		openWaves(t, h)
		hint := hintLine(h.view())
		if !strings.Contains(hint, "enter item") || !strings.Contains(hint, "space tick item") || strings.Contains(hint, "l launch") {
			t.Errorf("waves hint before a tick: %q", hint)
		}
		h.deliver(press("l"))
		if h.m.confirm != nil || h.m.waves.checking {
			t.Error("l launched with nothing ticked")
		}
		tickWave(t, h, "y", "n")
		hint = hintLine(h.view())
		if !strings.Contains(hint, "l launch") {
			t.Errorf("waves hint after a tick: %q", hint)
		}
		h.deliver(press("l"))
		if h.m.confirm == nil && !h.m.waves.checking {
			t.Error("l did nothing though the hint lists it")
		}
	})

	t.Run("waves without a daemon", func(t *testing.T) {
		h, _ := wavesHarness(t)
		openWaves(t, h)
		h.m.cfg.Live = nil
		hint := hintLine(h.view())
		h.deliver(press(" "))
		if h.m.waves.inputs != nil {
			t.Fatal("space ticked with no daemon")
		}
		if strings.Contains(hint, "space tick") {
			t.Errorf("space is refused, yet the hint lists it: %q", hint)
		}
	})

	t.Run("modal prompts and confirmations", func(t *testing.T) {
		h := actionHarness(t, "awaiting-approval", "")
		h.deliver(press("x"))
		if h.m.confirm == nil {
			t.Fatal("x did not ask")
		}
		hint := hintLine(h.view())
		if !strings.Contains(hint, "y stop; n cancel") || strings.Contains(hint, "q quit") || strings.Contains(hint, "enter is no") {
			t.Errorf("confirmation hint: %q", hint)
		}
		h.deliver(press("n"))
		h.deliver(press("?"))
		if hint = hintLine(h.view()); hint != "? or esc close help; q quit" {
			t.Errorf("help hint: %q", hint)
		}
	})
}

// f file slice is listed exactly when f asks for the confirmation (R2).
func TestHintBarListsFileOnlyWhenFilingActs(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(h *harness)
	}{
		{"a daemon that files", func(h *harness) {}},
		{"a daemon that cannot file", func(h *harness) { h.m.cfg.Live = &actionLive{src: h.src} }},
		{"no daemon", func(h *harness) { h.m.cfg.Live = nil }},
		{"a filing in flight", func(h *harness) {
			h.m.actionFlights = map[string][]runAction{"engine": {{verb: "file", run: "engine"}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := findingsHarness(t, "awaiting-approval", laterOne)
			tc.set(h)
			hint := wideHint(h)
			h.deliver(press("f"))
			if listed, accepted := strings.Contains(hint, "f file slice"), h.m.confirm != nil; listed != accepted {
				t.Errorf("hint %q: lists f = %v, but f acts = %v", hint, listed, accepted)
			}
		})
	}
}

// The answer keys are listed only while the answer would go out (R3).
func TestHintBarListsAnswerKeysOnlyWhenTheAnswerActs(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(h *harness)
		acts bool
	}{
		{"a daemon", func(h *harness) {}, true},
		{"no daemon", func(h *harness) { h.m.cfg.Live = nil }, false},
		{"a call in flight", func(h *harness) {
			h.m.actionFlights = map[string][]runAction{"engine": {{verb: "retry", run: "engine"}}}
		}, false},
	} {
		t.Run("options/"+tc.name, func(t *testing.T) {
			h := optionsHarness(t)
			tc.set(h)
			hint := hintLine(h.view())
			h.deliver(press("1"))
			if listed, accepted := strings.Contains(hint, "1-2 pick"), h.m.confirm != nil; listed != accepted || listed != tc.acts {
				t.Errorf("hint %q: lists the pick = %v, key acts = %v, want %v", hint, listed, accepted, tc.acts)
			}
			for _, want := range []string{"t free text", "esc close pane"} {
				if !strings.Contains(hint, want) {
					t.Errorf("hint %q lacks %q", hint, want)
				}
			}
		})
		t.Run("free text/"+tc.name, func(t *testing.T) {
			h := optionsHarness(t)
			h.deliver(press("t"))
			typeAnswer(h, "use local storage")
			tc.set(h)
			hint := hintLine(h.view())
			h.deliver(press("enter"))
			acted := h.m.pane == nil || h.m.pane.input.Value() == "" // a sent answer empties the field
			if listed := strings.Contains(hint, "enter submit"); listed != acted || listed != tc.acts {
				t.Errorf("hint %q: lists enter submit = %v, enter acts = %v, want %v", hint, listed, acted, tc.acts)
			}
			if !strings.Contains(hint, "esc back to options") {
				t.Errorf("hint %q lacks the way back", hint)
			}
		})
	}
}

// space tick is listed exactly when space would tick the row under the cursor
// (R4): not on an unticked row while a plan is pending.
func TestHintBarListsTickOnlyWhenTickingActs(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	h.deliver(press(" "))
	h.deliver(tea.PasteMsg{Content: "y"})
	h.deliver(press("tab"))
	h.deliver(tea.PasteMsg{Content: "n"})
	_, plan := h.m.Update(press("enter"))
	if plan == nil || h.m.waves.planning == "" {
		t.Fatal("no plan pending")
	}
	// The ticked row can be unticked; the next one cannot be ticked yet.
	if hint := hintLine(h.view()); !strings.Contains(hint, "space tick") {
		t.Errorf("space unticks the pending row, yet the hint lacks it: %q", hint)
	}
	h.deliver(press("down"))
	hint := hintLine(h.view())
	h.deliver(press(" "))
	if h.m.waves.inputs != nil {
		t.Fatal("space ticked another item while a plan was pending")
	}
	if strings.Contains(hint, "space tick") {
		t.Errorf("space is refused while a plan is pending, yet the hint lists it: %q", hint)
	}
}

// The check digits are listed only for a daemon that can mark them (R5).
func TestHintBarListsMarkOnlyWithACardActor(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(h *harness)
		acts bool
	}{
		{"a card actor", func(h *harness) {}, true},
		{"a daemon that cannot mark", func(h *harness) { h.m.cfg.Live = &actionLive{src: h.src} }, false},
		{"no daemon", func(h *harness) { h.m.cfg.Live = nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := cardHarness(t, approvalCard(openChecksThree()...))
			tc.set(h)
			hint := wideHint(h)
			cl, _ := h.m.cfg.Live.(*cardLive)
			h.deliver(press("1"))
			acted := cl != nil && len(cl.marks) > 0
			if listed := strings.Contains(hint, "1-3 tick/later"); listed != acted || listed != tc.acts {
				t.Errorf("hint %q: lists the digits = %v, key acts = %v, want %v", hint, listed, acted, tc.acts)
			}
			if listed := strings.Contains(hint, "F fix round"); listed != tc.acts {
				t.Errorf("hint %q: lists F = %v, want %v", hint, listed, tc.acts)
			}
		})
	}
}

// The view bar drops the Questions count when it would push Decisions off the
// pane (U1), and shows it when it fits.
func TestViewBarDropsTheCountWhenItDoesNotFit(t *testing.T) {
	p := proj("zeta", true)
	var recs []adjcli.Record
	for i := 1; i <= 12; i++ {
		recs = append(recs, openRec(i, "k.q", fmt.Sprintf("question %d", i)))
	}
	h := newHarness(t, p)
	h.src.set(snap(p, recs, nil))
	h.start()
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	bar := plainView(h.m)
	bar = strings.Split(bar, "\n")[0]
	if !strings.Contains(bar, "Questions 12") || !strings.Contains(bar, "Decisions") {
		t.Errorf("the count or Decisions is missing at 80x24: %q", bar)
	}
	checkGolden(t, "hint-questions-12-80x24.txt", h.m.View().Content)

	// "Overview (Running)" leaves no room for the count: the count goes.
	h.m.active = runningEntry
	bar = strings.Split(plainView(h.m), "\n")[0]
	if !strings.HasSuffix(strings.TrimRight(bar, " "), "Decisions") || strings.Contains(bar, "Questions 12") {
		t.Errorf("the Running bar at 80x24: %q", bar)
	}
	// Wider, the count stays with it.
	h.send(tea.WindowSizeMsg{Width: 120, Height: 24})
	bar = strings.Split(plainView(h.m), "\n")[0]
	if !strings.Contains(bar, "Overview (Running) │ Questions 12 │") {
		t.Errorf("the Running bar at 120 wide: %q", bar)
	}
}

// When the hint is too long, the row keys drop first, lowest priority first
// (f, v, F, the check digits, then t and x), and the way out stays at the
// right end (U2).
func TestHintBarKeepsTheWayOutAndDropsRowKeysInOrder(t *testing.T) {
	card := rendersCard()
	card.HumanChecks = []runrec.HumanCheck{{Text: humanOne, State: runrec.CheckTicked}, {Text: humanTwo, State: runrec.CheckLater}}
	h := cardHarness(t, card, laterOne)
	order := []string{"f file slice", "v renders", "F fix round", "1-2 tick/later", "x stop"}
	seenWidths := 0
	for width := 140; width >= 30; width -= 2 {
		h.send(tea.WindowSizeMsg{Width: width, Height: 30})
		hint := hintLine(plainView(h.m))
		if !strings.HasSuffix(hint, "esc close pane") {
			t.Fatalf("width %d: the way out is not at the right end: %q", width, hint)
		}
		for i, word := range order {
			if !strings.Contains(hint, word) {
				continue
			}
			for _, later := range order[i+1:] {
				_, _, side := h.m.layoutSize()
				if !strings.Contains(hint, later) && 1+lipgloss.Width(hint)+2+lipgloss.Width(later) <= width-side {
					t.Errorf("width %d: %q stays while %q goes: %q", width, word, later, hint)
				}
			}
		}
		if !strings.Contains(hint, "f file slice") {
			seenWidths++
		}
	}
	if seenWidths == 0 {
		t.Fatal("the hint never had to drop a key; the test proves nothing")
	}
	// Elsewhere the help key stays at the right end.
	o := newHarness(t, four...)
	o.src.set(snap(four[0], []adjcli.Record{openRec(1, "a.b", "a question")}, nil))
	o.start()
	for _, width := range []int{100, 80, 60, 40, 30} {
		o.send(tea.WindowSizeMsg{Width: width, Height: 24})
		hint := hintLine(plainView(o.m))
		if !strings.HasSuffix(hint, "? help") || (width >= 60 && !strings.HasPrefix(hint, "enter")) {
			t.Errorf("width %d: %q", width, hint)
		}
	}
}

// The hint words are the words of the screen it sits on (U3).
func TestHintBarUsesTheScreensOwnWords(t *testing.T) {
	t.Run("approval card", func(t *testing.T) {
		h := cardHarness(t, approvalCard(openChecksThree()...))
		v := h.view()
		if strings.Count(v, "1-3 tick/later") != 1 || !strings.Contains(wideHint(h), "1-3 tick/later") {
			t.Errorf("card and hint disagree:\n%s", v)
		}
	})
	t.Run("pane footer", func(t *testing.T) {
		h := actionHarness(t, "implement", "")
		h.deliver(press("enter"))
		v := h.view()
		if strings.Count(v, "x stop") != 1 || !strings.HasSuffix(hintLine(v), "esc close pane") {
			t.Errorf("pane footer and hint disagree:\n%s", v)
		}
	})
	t.Run("decisions", func(t *testing.T) {
		h := decisionsHarness(t)
		if hint := hintLine(h.view()); !strings.Contains(hint, "left/right switch tab") {
			t.Errorf("decisions hint: %q", hint)
		}
	})
	t.Run("waves", func(t *testing.T) {
		h, l := wavesHarness(t)
		questionsForB(l)
		twoWaveItems(t, h)
		hint := hintLine(h.view())
		v := h.view()
		for _, want := range []string{"enter item", "space tick item", "l launch"} {
			if !strings.Contains(hint, want) {
				t.Errorf("waves hint lacks %q: %q", want, hint)
			}
		}
		if strings.Contains(v, "[enter] item") || strings.Contains(v, "[l] launch wave") {
			t.Errorf("the waves body changed its words:\n%s", v)
		}
		h.deliver(press("l")) // the spec check holds B with questions
		h.deliver(press("esc"))
		if h.m.confirm != nil {
			h.deliver(press("n"))
		}
		if !h.m.waves.heldWithQuestions() {
			t.Fatalf("B is not held:\n%s", h.view())
		}
		hint = hintLine(h.view())
		if !strings.Contains(hint, "l answer spec questions") || strings.Contains(hint, "l launch") {
			t.Errorf("held waves hint: %q", hint)
		}
	})
}

// A confirmation names what y does (U4); the prompt on screen already carries
// the rest, so the hint does not repeat "enter is no".
func TestHintBarConfirmNamesWhatYDoes(t *testing.T) {
	for verb, want := range map[string]string{
		"launch": "y run; n cancel; e edit lineup", "wave": "y start; n cancel", "approve": "y approve; n cancel",
		"stop": "y stop; n cancel", "retry": "y retry; n cancel", "cleanup": "y clean up; n cancel",
		"answer": "y send; n cancel", "file": "y file; n cancel", "other": "y confirm; n cancel",
	} {
		h := actionHarness(t, "awaiting-approval", "")
		h.m.confirm = &runAction{verb: verb, run: "engine", project: "ork"}
		hint := hintLine(h.view())
		if hint != want {
			t.Errorf("%s: hint %q, want %q", verb, hint, want)
		}
	}
}

func TestHintBarHelpScreenSaysHowToClose(t *testing.T) {
	h := actionHarness(t, "awaiting-approval", "")
	h.deliver(press("?"))
	if hint := hintLine(h.view()); hint != "? or esc close help; q quit" {
		t.Errorf("help hint: %q", hint)
	}
}

func TestHintRound2Prompts(t *testing.T) {
	h, _ := welcomeHarness(t)
	if got := wideHint(h); !strings.Contains(got, "+ add project; q quit") {
		t.Errorf("Welcome: %q", got)
	}
	h.deliver(press("+"))
	if got := wideHint(h); !strings.Contains(got, "enter review path; esc cancel") {
		t.Errorf("path: %q", got)
	}
	w, _ := wavesHarness(t)
	openWaves(t, w)
	if got := wideHint(w); strings.Contains(got, "e edit lineup") {
		t.Errorf("unticked: %q", got)
	}
	w.deliver(press(" "))
	if strings.Contains(plainView(w.m), "[l] launches") {
		t.Error("Waves status repeats launch key")
	}
	if got := wideHint(w); !strings.Contains(got, "tab next field; enter plan; esc cancel") {
		t.Errorf("inputs: %q", got)
	}
	w.deliver(press("esc"))
	tickWave(t, w, "y", "n")
	if got := wideHint(w); !strings.Contains(got, "e edit lineup") {
		t.Errorf("planned: %q", got)
	}
	l, _ := launchHarness(t)
	l.deliver(press("l"))
	if got := wideHint(l); !strings.Contains(got, "enter select slice") || !strings.Contains(got, "esc cancel") {
		t.Errorf("launch list: %q", got)
	}
	l.deliver(press("enter"))
	if got := wideHint(l); !strings.Contains(got, "enter next field; esc cancel") {
		t.Errorf("launch input: %q", got)
	}
}

func TestHintRound2NoBodyKeys(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	for _, text := range []string{"tick/later", "a approve", "esc close pane"} {
		if strings.Contains(strings.Join(h.m.paneLines(100, 30, ""), "\n"), text) {
			t.Errorf("pane repeats %q", text)
		}
	}
	w, _ := wavesHarness(t)
	openWaves(t, w)
	if strings.Contains(strings.Join(w.m.wavesBody(100).body, "\n"), "[enter]") {
		t.Error("Waves repeats keys")
	}
	d := decisionsHarness(t)
	if strings.Contains(strings.Join(d.m.decisions(100).body, "\n"), "[left/right]") {
		t.Error("Decisions repeats keys")
	}
}

func TestHintRound2OpenChecksPriority(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	for width := 30; width <= 160; width++ {
		got := sgr.ReplaceAllString(h.m.hintLine(width), "")
		if !strings.HasSuffix(got, "esc close pane") {
			t.Fatalf("width %d: %q", width, got)
		}
		if strings.Contains(got, "x stop") || strings.Contains(got, "pgup/pgdown") || strings.Contains(got, "ctrl+d/u") {
			for _, want := range []string{"1-3 tick/later", "F fix round"} {
				if !strings.Contains(got, want) && lipgloss.Width(got)+2+lipgloss.Width(want) <= width {
					t.Errorf("width %d: lost %q before stop/scroll: %q", width, want, got)
				}
			}
		}
	}
}

func TestHintRound2StyleAndVisibleWidth(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h := actionHarness(t, "implement", "")
		h.m.cfg.Theme = NewTheme(mono)
		for width := 15; width <= 120; width++ {
			got := h.m.hintLine(width)
			if lipgloss.Width(got) > width {
				t.Errorf("width %d: %q", width, got)
			}
		}
		full := sgr.ReplaceAllString(h.m.hintLine(200), "")
		fit := sgr.ReplaceAllString(h.m.hintLine(lipgloss.Width(full)), "")
		if fit != full {
			t.Errorf("visible-width fit lost hints: %q -> %q", full, fit)
		}
		got := h.m.hintLine(200)
		if !strings.Contains(got, "q \x1b[2mquit\x1b[") || strings.Contains(got, "\x1b[90m") {
			t.Errorf("mono %v: keys/default, descriptions/SGR2: %q", mono, got)
		}
	}
}

func TestHintRound3RefillsAtOriginalPosition(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := hintLine(plainView(h.m)); got != "x stop; 1-3 tick/later; F fix round; esc close pane" {
		t.Errorf("80x24 hint: %q", got)
	}
	if got := sgr.ReplaceAllString(h.m.hintLine(30), ""); got != " F fix round; esc close pane" {
		t.Errorf("refill priority: %q", got)
	}
	// Every offered hint that is still absent must be too wide for the spare room.
	for width := 30; width <= 180; width++ {
		got := sgr.ReplaceAllString(h.m.hintLine(width), "")
		at := 0
		for _, e := range h.m.contextHints() {
			help := e.b.Help()
			text := strings.TrimSpace(help.Key + " " + help.Desc)
			pos := strings.Index(got, text)
			if pos < 0 {
				if lipgloss.Width(got)+2+lipgloss.Width(text) <= width {
					t.Errorf("width %d: %q fits but is absent from %q", width, text, got)
				}
			} else {
				if pos < at {
					t.Errorf("width %d: reordered %q in %q", width, text, got)
				}
				at = pos
			}
		}
	}
}

func TestHintRound3WelcomeConfirm(t *testing.T) {
	for _, live := range []bool{true, false} {
		t.Run(fmt.Sprint(live), func(t *testing.T) {
			h, l := welcomeHarness(t)
			welcomePath(h, "/example/project")
			if !live {
				h.m.cfg.Live = nil
			}
			want := "n cancel"
			if live {
				want = "y add project; " + want
			}
			if got := hintLine(h.view()); got != want {
				t.Errorf("hint: %q, want %q", got, want)
			}
			h.deliver(press("y"))
			if acted := len(l.paths) > 0; acted != live {
				t.Errorf("y acted = %v, want %v", acted, live)
			}
		})
	}
}

func TestHintRound3LineupCycleNeedsAlternatives(t *testing.T) {
	h, _ := lineupHarness(t)
	e := h.m.waves.lineup
	for _, n := range []int{0, 1, 2} {
		e.plan.Lineup["implementer"] = make([]runrec.LineupEntry, n)
		got := wideHint(h)
		if listed := strings.Contains(got, "left/right cycle"); listed != (n > 1) {
			t.Errorf("%d entries: %q", n, got)
		}
	}
}

func TestHintRound3PaneScrollWords(t *testing.T) {
	run := actionHarness(t, "implement", "")
	run.deliver(press("enter"))
	if got := wideHint(run); !strings.Contains(got, "pgup/pgdown line") {
		t.Errorf("run hint: %q", got)
	}
	wave, _ := wavesHarness(t)
	openWaves(t, wave)
	wave.deliver(press("enter"))
	if got := wideHint(wave); !strings.Contains(got, "pgup/pgdown page") || !strings.Contains(got, "j/k line") || !strings.HasSuffix(got, "esc close pane") {
		t.Errorf("wave hint: %q", got)
	}
	decision := decisionsHarness(t)
	decision.deliver(press("enter"))
	if got := wideHint(decision); !strings.Contains(got, "pgup/pgdown page") || !strings.Contains(got, "j/k line") {
		t.Errorf("decision hint: %q", got)
	}
}

// S152: the Questions list ends with its rows, as Decisions does; the hint line
// already names enter.
func TestQuestionsBodyHasNoEnterOpenFooter(t *testing.T) {
	h := questionsHarness(t)
	h.view()
	body := sgr.ReplaceAllString(strings.Join(h.m.questions(72).body, "\n"), "")
	if strings.Contains(body, "[enter] open") {
		t.Errorf("the Questions body keeps its footer:\n%s", body)
	}
	if !strings.Contains(body, "engine-zeta") {
		t.Errorf("the rows are gone:\n%s", body)
	}
	if hint := wideHint(h); !strings.Contains(hint, "enter open") {
		t.Errorf("the hint line lost enter: %q", hint)
	}
}

// The lineup's budget rows take a number, and the hint says so again (S152).
// The role rows do not: they cycle or take a custom entry.
func TestWavesBudgetRowsShowTheWholeNumberHint(t *testing.T) {
	h, _ := lineupHarness(t)
	for row := 0; row < len(lineupRoles)+3; row++ {
		if got := h.m.waves.lineup.row; got != row {
			t.Fatalf("the editor is on row %d, want %d", got, row)
		}
		hint := wideHint(h)
		budget := row > len(lineupRoles)
		if has := strings.Contains(hint, "type a whole number"); has != budget {
			t.Errorf("row %d: type a whole number shown %v, want %v: %q", row, has, budget, hint)
		}
		if budget && (!strings.Contains(hint, "up/down/tab row") || strings.Contains(hint, "left/right cycle") || !strings.HasSuffix(hint, "esc discard")) {
			t.Errorf("row %d: the budget hint is wrong: %q", row, hint)
		}
		// At the standard sizes the row key goes before the number hint does.
		if budget {
			for _, size := range [][2]int{{80, 24}, {100, 30}} {
				h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				if got := hintLine(plainView(h.m)); !strings.Contains(got, "type a whole number; enter save and replan; esc discard") {
					t.Errorf("row %d at %dx%d: the budget hint is %q", row, size[0], size[1], got)
				}
			}
		}
		h.deliver(press("down"))
	}
}

// q quit stands at the right end with ? help on the main views, at both
// standard sizes, with a pane open or not (S152).
func TestQuitHintNeverDropsOnMainViews(t *testing.T) {
	type fixture struct {
		name string
		make func(t *testing.T) *harness
	}
	pane := func(view int, enter int) func(t *testing.T) *harness {
		return func(t *testing.T) *harness {
			var h *harness
			switch view {
			case decisionsView:
				h = decisionsHarness(t)
			case roadmapView:
				h = roadmapHarness(t)
				h.m.cfg.Live = &roadmapLive{detail: &slicercli.Detail{Item: slicercli.Item{ID: "S90"}}}
			}
			for range enter {
				h.deliver(press("enter"))
			}
			return h
		}
	}
	fixtures := []fixture{
		{"Overview", func(t *testing.T) *harness { return actionHarness(t, "awaiting-approval", "") }},
		{"Questions", questionsHarness},
		{"Waves", func(t *testing.T) *harness {
			h, _ := wavesHarness(t)
			openWaves(t, h)
			tickWave(t, h, "y", "n")
			return h
		}},
		{"Roadmap", func(t *testing.T) *harness {
			h := roadmapHarness(t)
			h.m.cfg.Live = &actionLive{src: h.src}
			return h
		}},
		{"Decisions", decisionsHarness},
		{"Decisions pane", pane(decisionsView, 1)},
		{"Roadmap pane", pane(roadmapView, 1)},
	}
	for _, f := range fixtures {
		for _, size := range [][2]int{{80, 24}, {100, 30}} {
			t.Run(fmt.Sprintf("%s %dx%d", f.name, size[0], size[1]), func(t *testing.T) {
				h := f.make(t)
				h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				hint := hintLine(plainView(h.m))
				if !strings.Contains(hint, "q quit") {
					t.Errorf("the hint lost q quit: %q", hint)
				}
				// Whatever else is crowded out, the two stay.
				for w := 68; w >= 54; w -= 2 {
					h.send(tea.WindowSizeMsg{Width: w, Height: size[1]})
					if got := hintLine(plainView(h.m)); !strings.Contains(got, "q quit") || !strings.Contains(got, "help") {
						t.Errorf("width %d: %q", w, got)
					}
				}
			})
		}
	}
}

// ?help leaves the sidebar header: the hint line shows the help key (S152).
func TestSidebarHeaderHasNoHelpCue(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	header := stripANSI(h.m.sidebar(28, 30)[0])
	if strings.Contains(header, "help") || !strings.Contains(header, "ork") {
		t.Errorf("the sidebar header is %q", header)
	}
	if first := strings.Split(h.view(), "\n")[0]; strings.Contains(first, "?help") {
		t.Errorf("the first screen line keeps the cue: %q", first)
	}
	if hint := wideHint(h); !strings.Contains(hint, "? help") {
		t.Errorf("the hint line lost ? help: %q", hint)
	}
}

// The Questions count has its own style, the Badge, and the name keeps the
// view-bar styles (S152).
func TestQuestionsTabCountIsStyled(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, view := range []int{0, questionsView} {
			h := questionsHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			h.m.view = view
			n := len(h.m.selectableQuestions())
			bar := h.m.viewBar()
			if want := h.m.cfg.Theme.Badge.Render(fmt.Sprint(n)); !strings.Contains(bar, want) {
				t.Errorf("mono %v view %d: the count %d is not in the Badge style: %q", mono, view, n, bar)
			}
			if plain := stripANSI(bar); !strings.Contains(plain, fmt.Sprintf("Questions %d", n)) {
				t.Errorf("mono %v view %d: the bar reads %q", mono, view, plain)
			}
		}
	}
}

// The read-only note of a ledger pane has no key, so it takes the Dim style of
// the screen; the esc hint keeps the faint description (S152).
func TestLedgerPaneReadOnlyNoteHasItsOwnStyle(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	h.m.cfg.Theme = NewTheme(false)
	got := h.m.hintLine(200)
	note := h.m.cfg.Theme.Dim.Render("started elsewhere, read-only")
	if !strings.Contains(got, note) {
		t.Errorf("the note lacks the Dim style %q: %q", note, got)
	}
	if !strings.Contains(got, "esc \x1b[2mclose pane\x1b[") {
		t.Errorf("the esc hint lost its faint description: %q", got)
	}
}

// ctrl+d and ctrl+u move the decision, Roadmap item and Waves item panes by
// half of what they show, as they do in a run pane (S152).
func TestCtrlDUScrollDecisionRoadmapAndWavesItemPanes(t *testing.T) {
	var lines []string
	for i := 0; i < 120; i++ {
		lines = append(lines, fmt.Sprintf("long line %03d", i))
	}
	long := strings.Join(lines, "\n")
	ctrl := func(h *harness, k string) { h.deliver(press(k)) }

	t.Run("decision pane", func(t *testing.T) {
		h := decisionsHarness(t)
		h.m.projects[0].snap.Open.Value[0].Context = long
		h.deliver(press("enter"))
		_, visible := h.m.decisionPage()
		want := max(visible/2, 1)
		ctrl(h, "ctrl+d")
		if h.m.decisionScroll != want || want < 5 {
			t.Fatalf("ctrl+d scrolled %d, want half a page, %d", h.m.decisionScroll, want)
		}
		ctrl(h, "ctrl+d")
		if h.m.decisionScroll != 2*want {
			t.Errorf("a second ctrl+d scrolled to %d, want %d", h.m.decisionScroll, 2*want)
		}
		ctrl(h, "ctrl+u")
		ctrl(h, "ctrl+u")
		ctrl(h, "ctrl+u")
		if h.m.decisionScroll != 0 {
			t.Errorf("ctrl+u went to %d, want the top", h.m.decisionScroll)
		}
		for range 40 {
			ctrl(h, "ctrl+d")
		}
		if v := h.view(); !strings.Contains(v, "long line 119") {
			t.Errorf("ctrl+d does not reach the end:\n%s", v)
		}
	})

	t.Run("decision pane on Questions", func(t *testing.T) {
		h := questionsHarness(t)
		h.m.projects[0].snap.Open.Value[0].Context = long
		h.deliver(press("down")) // the first adj record
		h.deliver(press("enter"))
		if h.m.decisionPane == nil {
			t.Fatal("enter did not open the record")
		}
		_, visible := h.m.decisionPage()
		ctrl(h, "ctrl+d")
		if want := max(visible/2, 1); h.m.decisionScroll != want {
			t.Fatalf("ctrl+d scrolled %d, want %d", h.m.decisionScroll, want)
		}
		ctrl(h, "ctrl+u")
		if h.m.decisionScroll != 0 {
			t.Errorf("ctrl+u left %d", h.m.decisionScroll)
		}
	})

	t.Run("roadmap item pane", func(t *testing.T) {
		h := roadmapHarness(t)
		h.m.cfg.Live = &roadmapLive{detail: &slicercli.Detail{Item: slicercli.Item{ID: "S90", Title: "Detail title"}, Slice: &slicercli.Spec{Title: "Spec title", Sections: []slicercli.Section{{Heading: "Why", Body: long}}}}}
		_, cmd := h.m.Update(press("enter"))
		h.m.Update(cmd())
		top := h.view()
		if !strings.Contains(top, "long line 000") || strings.Contains(top, "long line 119") {
			t.Fatalf("the pane does not start at the top of a long item:\n%s", top)
		}
		pane := h.m.itemPane
		want := max(pane.room/2, 1)
		if pane.room < 5 {
			t.Fatalf("the pane drew %d lines of room", pane.room)
		}
		ctrl(h, "ctrl+d")
		if pane.offset != want {
			t.Fatalf("ctrl+d scrolled %d, want half a page, %d", pane.offset, want)
		}
		v := h.view()
		if !strings.Contains(v, "esc returns") || !strings.Contains(v, fmt.Sprintf("long line %03d", want)) || strings.Contains(v, "long line 000") {
			t.Errorf("ctrl+d did not scroll the item under its heading:\n%s", v)
		}
		ctrl(h, "ctrl+u")
		ctrl(h, "ctrl+u")
		if pane.offset != 0 {
			t.Errorf("ctrl+u went to %d, want the top", pane.offset)
		}
		for range 40 {
			ctrl(h, "ctrl+d")
			h.view()
		}
		if v := h.view(); !strings.Contains(v, "long line 119") {
			t.Errorf("ctrl+d does not reach the end:\n%s", v)
		}
		ctrl(h, "ctrl+u")
		if pane.offset == 0 {
			t.Error("ctrl+u after the end went all the way to the top")
		}
	})

	t.Run("waves item pane", func(t *testing.T) {
		h, l := wavesHarness(t)
		l.details = map[string]*slicercli.Detail{"A": {Slice: &slicercli.Spec{Title: "Alpha", Sections: []slicercli.Section{{Heading: "Files", Body: long}}}}}
		openWaves(t, h)
		h.deliver(press("enter"))
		h.view()
		pane := h.m.waves.pane
		want := max(pane.room/2, 1)
		if pane.room < 5 {
			t.Fatalf("the pane drew %d lines of room", pane.room)
		}
		ctrl(h, "ctrl+d")
		if h.m.waves.paneOffset != want {
			t.Fatalf("ctrl+d scrolled %d, want half a page, %d", h.m.waves.paneOffset, want)
		}
		h.view()
		ctrl(h, "ctrl+d")
		if h.m.waves.paneOffset != 2*want {
			t.Errorf("a second ctrl+d scrolled to %d, want %d", h.m.waves.paneOffset, 2*want)
		}
		ctrl(h, "ctrl+u")
		ctrl(h, "ctrl+u")
		ctrl(h, "ctrl+u")
		if h.m.waves.paneOffset != 0 {
			t.Errorf("ctrl+u went to %d, want the top", h.m.waves.paneOffset)
		}
	})
}

func TestCtrlDUHintsShowInThosePanes(t *testing.T) {
	cases := map[string]func(t *testing.T) *harness{
		"decision pane": func(t *testing.T) *harness {
			h := decisionsHarness(t)
			h.deliver(press("enter"))
			return h
		},
		"questions decision pane": func(t *testing.T) *harness {
			h := questionsHarness(t)
			h.deliver(press("down"))
			h.deliver(press("enter"))
			return h
		},
		"roadmap item pane": func(t *testing.T) *harness {
			h := roadmapHarness(t)
			h.m.cfg.Live = &roadmapLive{detail: &slicercli.Detail{Item: slicercli.Item{ID: "S90"}}}
			h.deliver(press("enter"))
			return h
		},
		"waves item pane": func(t *testing.T) *harness {
			h, _ := wavesHarness(t)
			openWaves(t, h)
			h.deliver(press("enter"))
			return h
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			h := build(t)
			hint := wideHint(h)
			if !strings.Contains(hint, "ctrl+d/u half page") {
				t.Errorf("the hint lacks the half page keys: %q", hint)
			}
			// They are among the first hints to go, ahead of the way out.
			h.send(tea.WindowSizeMsg{Width: 70, Height: 24})
			if got := hintLine(plainView(h.m)); strings.Contains(got, "ctrl+d/u") && !strings.Contains(got, "q quit") {
				t.Errorf("ctrl+d/u outlasts q quit: %q", got)
			}
		})
	}
}

// ctrl+u clears the text in a field, and the help says so on the line of the
// scroll keys, which leaves the standard help as long as it was (S152).
func TestHelpMentionsCtrlUClearsAField(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(press("?"))
	v := h.view()
	if !strings.Contains(v, "ctrl+u clears a field") {
		t.Errorf("the help does not say what ctrl+u does in a field:\n%s", v)
	}
	if !strings.Contains(v, "ctrl+d/u scroll a pane half a page") {
		t.Errorf("the help lost the scroll keys:\n%s", v)
	}
	if strings.Contains(v, "more lines") || !strings.Contains(v, "esc close run, question, item, decision, help or message") {
		t.Errorf("the help is cut at 100x30:\n%s", v)
	}
}

// The Waves hint at the standard size holds the edit key beside the way out
// (owner decision 2026-10-29: l launch, not l launch wave).
func TestWavesHintHoldsEditLineupAt100x30(t *testing.T) {
	h, _ := wavesHarness(t)
	twoWaveItems(t, h)
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	hint := hintLine(plainView(h.m))
	for _, want := range []string{"enter item", "l launch", "e edit lineup", "q quit", "? help"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the Waves hint lacks %q: %q", want, hint)
		}
	}
	if strings.Contains(hint, "launch wave") {
		t.Errorf("the Waves hint still says launch wave: %q", hint)
	}
}

func TestLineupHelpIncludesLaunchAndConfirmReusesBinding(t *testing.T) {
	h, _ := launchHarness(t)
	if got := h.m.keys.EditLineup.Help().Desc; got != "edit lineup (Waves, launch)" {
		t.Errorf("help: %q", got)
	}
	launchConfirmed(t, h)
	h.m.keys.EditLineup.SetEnabled(false)
	if hint := hintLine(h.view()); strings.Contains(hint, "e edit lineup") {
		t.Errorf("confirmation ignored disabled shared binding: %s", hint)
	}
}
