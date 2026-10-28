// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// S164: the Running entry opens more than one run at a time. Space toggles the
// row under the cursor, E opens or closes every run, and the open runs show
// their detail under their row.

// space is the space bar as the terminal sends it.
func space() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "} }

// expandProjects is n projects, p1 to pn, each with one live engine run: run-1
// on S11 in p1, run-2 on S12 in p2, and so on.
func expandProjects(n int) []registry.Project {
	var out []registry.Project
	for i := 1; i <= n; i++ {
		out = append(out, proj(fmt.Sprintf("p%d", i), false))
	}
	return out
}

func expandSnap(p registry.Project, i int) source.Snapshot {
	return engineSnap(p, liveEngine(fmt.Sprintf("run-%d", i), fmt.Sprintf("S1%d", i), "review", testActivity))
}

// expandHarness has n live runs and the Running entry selected, the cursor on
// the first, at w x ht.
func expandHarness(t *testing.T, n, w, ht int) *harness {
	t.Helper()
	projects := expandProjects(n)
	h := newHarness(t, projects...)
	for i, p := range projects {
		h.src.set(expandSnap(p, i+1))
	}
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: w, Height: ht})
	h.send(press("["))
	if h.m.active != runningEntry {
		t.Fatalf("selected %d, want the Running entry", h.m.active)
	}
	return h
}

// mainCols is the view without its sidebar column.
func mainCols(v string) []string {
	var out []string
	for _, line := range strings.Split(v, "\n") {
		_, main, _ := strings.Cut(line, "│")
		out = append(out, main)
	}
	return out
}

// colLine is the index of the first line of lines that holds every word, or -1.
func colLine(lines []string, words ...string) int {
next:
	for i, line := range lines {
		for _, w := range words {
			if !strings.Contains(line, w) {
				continue next
			}
		}
		return i
	}
	return -1
}

// opened lists the run ids the view shows, in order of appearance.
func opened(v string, n int) []string {
	var out []string
	for _, line := range mainCols(v) {
		for i := 1; i <= n; i++ {
			if id := fmt.Sprintf("run-%d", i); strings.Contains(line, id) {
				out = append(out, id)
			}
		}
	}
	return out
}

func sameIDs(got []string, want ...string) bool {
	return strings.Join(got, ",") == strings.Join(want, ",")
}

func TestRunningExpandsSeveralRuns(t *testing.T) {
	h := expandHarness(t, 3, 100, 40)
	// Only the selected run is open, in the detail area, as before.
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-1") {
		t.Fatalf("before any key the view opens %v, want only run-1", got)
	}
	// Space opens the row under the cursor and keeps it open when the cursor moves.
	h.send(space())
	h.send(press("j"))
	h.send(space())
	h.send(press("j"))
	v := plainView(h.m)
	if got := opened(v, 3); !sameIDs(got, "run-1", "run-2", "run-3") {
		t.Fatalf("after two spaces the view opens %v, want all three:\n%s", got, v)
	}
	// Each open run sits right under its own row, above the next row.
	lines := mainCols(v)
	r1, r2, r3 := colLine(lines, "p1", "S11"), colLine(lines, "p2", "S12"), colLine(lines, "p3", "S13")
	d1, d2 := colLine(lines, "run-1"), colLine(lines, "run-2")
	if !(r1 < d1 && d1 < r2 && r2 < d2 && d2 < r3) {
		t.Errorf("a run's detail is not under its row (rows %d %d %d, details %d %d):\n%s", r1, r2, r3, d1, d2, v)
	}
	// The detail holds what the bottom area holds: the pipeline and activity.
	if !strings.Contains(strings.Join(lines[d1:r2], "\n"), "implementer>[reviewer]>gate") ||
		!strings.Contains(strings.Join(lines[d1:r2], "\n"), "activity") {
		t.Errorf("run-1's detail lacks its pipeline or activity:\n%s", v)
	}
	// Space again closes it.
	h.send(press("g"))
	h.send(space())
	h.send(press("j"))
	h.send(press("j"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-2", "run-3") {
		t.Errorf("after closing run-1 the view opens %v, want run-2 and run-3", got)
	}
}

func TestSelectedExpandedRunShowsOnceUnderItsRow(t *testing.T) {
	h := expandHarness(t, 2, 100, 40)
	h.send(space())
	v := plainView(h.m)
	if got := opened(v, 2); !sameIDs(got, "run-1") {
		t.Fatalf("the selected open run shows %v, want run-1 once:\n%s", got, v)
	}
	lines := mainCols(v)
	if d, r := colLine(lines, "run-1"), colLine(lines, "p1", "S11"); d != r+1 {
		t.Errorf("the selected run's detail is on line %d, its row on %d:\n%s", d, r, v)
	}
	// With the run closed the detail goes back to the bottom area, below Recent.
	h.send(space())
	lines = mainCols(plainView(h.m))
	if d, recent := colLine(lines, "run-1"), colLine(lines, "Recent"); d < recent {
		t.Errorf("a closed selected run shows its detail above Recent (line %d, Recent %d)", d, recent)
	}
}

func TestEExpandsAndCollapsesEveryRun(t *testing.T) {
	h := expandHarness(t, 3, 100, 40)
	h.send(press("E"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-1", "run-2", "run-3") {
		t.Fatalf("E opens %v, want all three", got)
	}
	h.send(press("E"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-1") {
		t.Fatalf("E again leaves %v open, want only the selected run-1", got)
	}
	if n := len(h.m.expanded); n != 0 {
		t.Fatalf("%d runs stay in the open set after E E: %v", n, h.m.expanded)
	}
	// With one open, E opens the rest rather than closing it.
	h.send(press("j"))
	h.send(space())
	h.send(press("E"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-1", "run-2", "run-3") {
		t.Fatalf("E with one open leaves %v open, want all three", got)
	}
	// The cursor did not move.
	if h.m.cursor != 1 {
		t.Errorf("E moved the cursor to %d", h.m.cursor)
	}
}

func TestExpandedRunsSurviveARefresh(t *testing.T) {
	h := expandHarness(t, 3, 100, 40)
	h.send(space())
	h.send(press("j"))
	h.send(space())
	h.send(press("j")) // the cursor on run-3, not open
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-1", "run-2", "run-3") {
		t.Fatalf("setup opens %v", got)
	}
	// A refresh with the same runs.
	projects := expandProjects(3)
	for i, p := range projects {
		h.src.set(expandSnap(p, i+1))
	}
	h.send(press("r"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-1", "run-2", "run-3") {
		t.Errorf("after a refresh the view opens %v, want all three", got)
	}
	// A view switch and back.
	h.send(press("tab"))
	h.send(press("shift+tab"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-1", "run-2", "run-3") {
		t.Errorf("after a view switch the view opens %v, want all three", got)
	}
	// Another entry and back: the cursor starts over, the open runs do not.
	h.send(press("]"))
	h.send(press("["))
	h.send(press("G"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-1", "run-2", "run-3") {
		t.Errorf("after another entry the view opens %v, want all three", got)
	}
}

func TestAnExpandedRunThatLeavesTheListIsForgotten(t *testing.T) {
	h := expandHarness(t, 3, 100, 40)
	h.send(space()) // run-1
	h.send(press("G"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-1", "run-3") {
		t.Fatalf("setup opens %v, want run-1 and the selected run-3", got)
	}
	projects := expandProjects(3)
	// run-1 ends: p1 has no run.
	h.src.set(snap(projects[0], nil, nil))
	h.send(press("r"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-3") {
		t.Fatalf("with run-1 gone the view opens %v, want only the selected run-3", got)
	}
	if len(h.m.expanded) != 0 {
		t.Errorf("the open set keeps %v for a run that left", h.m.expanded)
	}
	// The same run id comes back: it is closed.
	h.src.set(expandSnap(projects[0], 1))
	h.send(press("r"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-3") {
		t.Errorf("a returning run-1 is open: %v", got)
	}
}

// An open run pane takes the room: the other rows are drawn closed until it closes.
func TestOpenPaneDrawsTheRowsClosed(t *testing.T) {
	h := expandHarness(t, 3, 100, 40)
	h.send(press("E"))
	h.send(press("enter"))
	if h.m.pane == nil {
		t.Fatal("enter did not open the pane")
	}
	if v := plainView(h.m); strings.Contains(v, "run-3") {
		t.Errorf("a row stays open under a pane:\n%s", v)
	}
	h.send(press("esc"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-1", "run-2", "run-3") {
		t.Errorf("after the pane closes the view opens %v, want all three", got)
	}
}

// A refresh while a project entry is selected keeps the runs open in the
// other projects: the open set is pruned against every project's runs.
func TestExpandedRunsSurviveARefreshOnAProjectEntry(t *testing.T) {
	h := expandHarness(t, 3, 100, 40)
	h.send(space()) // run-1
	h.send(press("j"))
	h.send(space()) // run-2
	h.send(press("1"))
	if h.m.active != 0 {
		t.Fatalf("1 selected %d, want the first project", h.m.active)
	}
	for i, p := range expandProjects(3) {
		h.src.set(expandSnap(p, i+1))
	}
	h.send(press("r"))
	h.send(press("0"))
	h.send(press("["))
	if h.m.active != runningEntry {
		t.Fatalf("selected %d, want the Running entry", h.m.active)
	}
	h.send(press("G"))
	if got := opened(plainView(h.m), 3); !sameIDs(got, "run-1", "run-2", "run-3") {
		t.Errorf("after a refresh on a project entry the view opens %v, want all three", got)
	}
	if len(h.m.expanded) != 2 {
		t.Errorf("the open set holds %v, want run-1 and run-2", h.m.expanded)
	}
}

// E needs no run under the cursor: it works from a Recent row, and the hint
// offers it there while any run can be opened.
func TestEWorksFromARecentRow(t *testing.T) {
	h := expandHarness(t, 2, 100, 40)
	p2 := expandSnap(expandProjects(2)[1], 2)
	p2.BoardRuns = source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{{
		ID: "ledger-9", Items: []string{"S19"}, State: "ended", Outcome: "pass",
		BeganAt: t0.Add(-10 * time.Minute).Format(time.RFC3339), UpdatedAt: t0.Add(-time.Minute).Format(time.RFC3339),
	}}}
	h.src.set(p2)
	h.send(press("r"))
	h.send(press("G"))
	r, ok := h.m.selectedRow()
	if !ok || r.live != nil {
		t.Fatalf("the last row is not a Recent row: %+v", r.name)
	}
	hint := wideHint(h)
	if !strings.Contains(hint, "E expand all") || strings.Contains(hint, "space") {
		t.Errorf("on a Recent row the hint is %q, want E expand all and no space", hint)
	}
	h.send(space())
	if len(h.m.expanded) != 0 {
		t.Errorf("space on a Recent row opened %v", h.m.expanded)
	}
	h.send(press("E"))
	if len(h.m.expanded) != 2 {
		t.Errorf("E on a Recent row opened %v, want both live runs", h.m.expanded)
	}
	if hint = wideHint(h); !strings.Contains(hint, "E collapse all") {
		t.Errorf("with every run open the hint is %q", hint)
	}
}

// Under the help overlay the keys do nothing, and shift+e is E.
func TestExpandKeysUnderHelpAndShiftE(t *testing.T) {
	h := expandHarness(t, 2, 100, 40)
	h.send(press("?"))
	h.send(space())
	h.send(press("E"))
	if len(h.m.expanded) != 0 {
		t.Errorf("the help overlay let the keys through: %v", h.m.expanded)
	}
	h.send(press("?"))
	h.send(tea.KeyPressMsg{Code: 'e', Mod: tea.ModShift, Text: "E"})
	if len(h.m.expanded) != 2 {
		t.Errorf("shift+e opened %v, want both runs", h.m.expanded)
	}
	h.send(tea.KeyPressMsg{Code: 'e', Mod: tea.ModShift})
	if len(h.m.expanded) != 0 {
		t.Errorf("shift+e again left %v open", h.m.expanded)
	}
}

// The open runs' detail is faint (SGR 2 in mono, the dim colour otherwise, as
// last heard is); the selected row keeps its highlight.
func TestExpandedDetailIsFaint(t *testing.T) {
	for _, c := range []struct {
		mono bool
		want string
	}{{true, "\x1b[2m"}, {false, "\x1b[90m"}} {
		h := expandHarness(t, 2, 100, 40)
		h.m.cfg.Theme = NewTheme(c.mono)
		h.send(press("E"))
		h.send(press("j"))
		var detail, row string
		for _, line := range strings.Split(h.m.View().Content, "\n") {
			_, main, _ := strings.Cut(line, "│")
			plain := sgr.ReplaceAllString(main, "")
			if strings.Contains(plain, "run-1") {
				detail = main
			}
			if strings.Contains(plain, "p2") && strings.Contains(plain, "S12") {
				row = main
			}
		}
		if !strings.Contains(detail, c.want) || strings.Contains(detail, "\x1b[7m") || strings.Contains(detail, ";7m") {
			t.Errorf("mono %v: an open run's detail is not faint (%q): %q", c.mono, c.want, detail)
		}
		if !strings.Contains(row, "7m") && !strings.Contains(row, ";7;") {
			t.Errorf("mono %v: the selected row lost its highlight: %q", c.mono, row)
		}
	}
}

// The keys belong to the Running entry: a project's Overview ignores them.
func TestExpandKeysDoNothingOutsideTheRunningEntry(t *testing.T) {
	h := expandHarness(t, 2, 100, 40)
	h.send(press("1"))
	if h.m.active != 0 {
		t.Fatalf("1 selected %d, want the first project", h.m.active)
	}
	h.send(space())
	h.send(press("E"))
	if n := len(h.m.expanded); n != 0 {
		t.Errorf("a project's Overview opened %d runs: %v", n, h.m.expanded)
	}
}

// The hint bar and the help name the keys while they work (S149 hints).
func TestExpandKeyIsInTheHint(t *testing.T) {
	h := expandHarness(t, 2, 100, 40)
	hint := wideHint(h)
	for _, want := range []string{"space expand", "E expand all"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint line lacks %q: %q", want, hint)
		}
	}
	h.send(space())
	hint = wideHint(h)
	if !strings.Contains(hint, "space collapse") || !strings.Contains(hint, "E expand all") {
		t.Errorf("with one run open the hint is %q", hint)
	}
	h.send(press("E"))
	hint = wideHint(h)
	if !strings.Contains(hint, "E collapse all") {
		t.Errorf("with every run open the hint is %q", hint)
	}
	// The other views and entries do not offer it.
	h.send(press("tab"))
	if hint = wideHint(h); strings.Contains(hint, "expand") || strings.Contains(hint, "collapse") {
		t.Errorf("Questions offers the expand key: %q", hint)
	}
	h.send(press("shift+tab"))
	h.send(press("]"))
	if hint = wideHint(h); strings.Contains(hint, "expand") || strings.Contains(hint, "collapse") {
		t.Errorf("Needs you offers the expand key: %q", hint)
	}
}

func TestExpandKeysAreInTheHelp(t *testing.T) {
	h := expandHarness(t, 2, 100, 40)
	help := strings.Join(h.m.helpLines(120, 60), "\n")
	if !strings.Contains(help, "space expand or collapse a run (Running); E expand or collapse all") {
		t.Errorf("help does not describe the expand keys:\n%s", help)
	}
}

// fullyVisible checks that the view holds the run at cursor row i (0-based):
// its row line and its detail, under the row, to the last line.
func fullyVisible(t *testing.T, v string, i int) {
	t.Helper()
	lines := mainCols(v)
	row := colLine(lines, fmt.Sprintf("p%d", i+1), fmt.Sprintf("S1%d", i+1))
	id := colLine(lines, fmt.Sprintf("run-%d", i+1))
	if row < 0 || id < 0 {
		t.Fatalf("cursor row %d: row line %d, detail line %d:\n%s", i, row, id, v)
	}
	// The detail's last line is the pipeline with the activity.
	end := id
	for end+1 < len(lines) && !isRule(lines[end+1]) && strings.HasPrefix(lines[end+1], "    ") {
		end++
	}
	if !strings.Contains(lines[end], "activity") {
		t.Fatalf("cursor row %d: detail ends at %q, not on its activity cell:\n%s", i, lines[end], v)
	}
	if id != row+1 {
		t.Errorf("cursor row %d: detail starts %d lines under the row", i, id-row)
	}
}

// At 80x24 the pane scrolls and the cursor row is always whole: its row line
// and every line of its detail are on the screen.
func TestExpandedRunsScrollAt80x24(t *testing.T) {
	h := expandHarness(t, 6, 80, 24)
	h.send(press("E"))
	check := func(step string, i int) {
		t.Helper()
		v := plainView(h.m)
		lines := strings.Split(v, "\n")
		if len(lines) != 24 {
			t.Fatalf("%s: %d lines, want 24", step, len(lines))
		}
		for _, l := range lines {
			if lipgloss.Width(l) > 80 {
				t.Fatalf("%s: line wider than 80: %q", step, l)
			}
		}
		if h.m.cursor != i {
			t.Fatalf("%s: cursor %d, want %d", step, h.m.cursor, i)
		}
		fullyVisible(t, v, i)
	}
	check("start", 0)
	for i := 1; i < 6; i++ {
		h.send(press("j"))
		check(fmt.Sprintf("down %d", i), i)
	}
	for i := 4; i >= 0; i-- {
		h.send(press("k"))
		check(fmt.Sprintf("up %d", i), i)
	}
	h.send(press("G"))
	check("last", 5)
	h.send(press("g"))
	check("first", 0)
}

// The page keys land on rows that are whole, and reach the last run.
func TestPageKeysCountExpandedRows(t *testing.T) {
	h := expandHarness(t, 6, 80, 24)
	h.send(press("E"))
	last := -1
	for n := 0; n < 10 && h.m.cursor != last; n++ {
		last = h.m.cursor
		h.send(tea.KeyPressMsg{Code: tea.KeyPgDown})
		if h.m.cursor < 6 {
			fullyVisible(t, plainView(h.m), h.m.cursor)
		}
	}
	if h.m.cursor < 5 {
		t.Errorf("pgdown stopped on row %d", h.m.cursor)
	}
	for n := 0; n < 10 && h.m.cursor != 0; n++ {
		h.send(tea.KeyPressMsg{Code: tea.KeyPgUp})
		if h.m.cursor < 6 {
			fullyVisible(t, plainView(h.m), h.m.cursor)
		}
	}
	if h.m.cursor != 0 {
		t.Errorf("pgup stopped on row %d", h.m.cursor)
	}
}

func TestGoldenRunningExpanded(t *testing.T) {
	for _, c := range []struct {
		name string
		w, h int
		mono bool
	}{
		{"running-expanded-100x30-color.txt", 100, 30, false},
		{"running-expanded-100x30-mono.txt", 100, 30, true},
		{"running-expanded-80x24-mono.txt", 80, 24, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := runningGoldenModel(NewTheme(c.mono), c.w, c.h)
			m.Update(press("E"))
			m.Update(press("j"))
			checkGolden(t, c.name, m.View().Content)
		})
	}
}
