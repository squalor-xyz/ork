// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// The run card (S106): the pane opens inline under the selected row, round and
// last heard sit beside the stage bar, last heard turns amber past 10 minutes
// and red past 30, and a terminal narrower than 100 columns drops the run id
// and the vendor/model column from the row. The Overview section order does
// not change (adj 149; the owner decided on 2026-10-26 that S110's Running
// entry covers "Running first").

const runCardID = "ork-20261019-164000-abcd"

// runCardRoles are an implementer and a reviewer that finished and an implementer
// in its second round, running: the bar reads [~~....] r2, last heard 2m.
func runCardRoles() []source.EngineRole {
	second := roleRun("implementer", 2*time.Minute, 0)
	second.Round = 2
	return []source.EngineRole{
		roleRun("implementer", 9*time.Minute, 6*time.Minute),
		roleRun("reviewer", 6*time.Minute, 4*time.Minute),
		second,
	}
}

// runCardHarness is one project with two open decisions above a live engine run,
// so the cursor starts on a decision and the run is not the first row.
func runCardHarness(t *testing.T, w, h int, mono bool) *harness {
	t.Helper()
	p := proj("ork", false)
	hn := newHarness(t, p)
	hn.m.cfg.Theme = NewTheme(mono)
	s := engineSnap(p, liveEngine(runCardID, "S30", "review", testActivity, runCardRoles()...))
	s.Open = source.Result[[]adjcli.Record]{Value: []adjcli.Record{openRec(11, "ui.first", "First?"), openRec(12, "ui.second", "Second?")}}
	hn.src.set(s)
	hn.start()
	hn.m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return hn
}

// selectRun moves the row cursor onto the engine run.
func (h *harness) selectRun() {
	h.t.Helper()
	for i := 0; i < 20; i++ {
		if r, ok := h.m.selectedRow(); ok && r.engineID == runCardID {
			return
		}
		h.send(press("down"))
	}
	h.t.Fatal("the cursor never reached the run")
}

// mainLines is the view without the sidebar column.
func mainLines(view string) []string {
	var out []string
	for _, line := range strings.Split(view, "\n") {
		_, main, _ := strings.Cut(line, "│")
		out = append(out, main)
	}
	return out
}

// lineWith is the index of the first line from start that holds every word,
// or -1.
func lineWith(lines []string, start int, words ...string) int {
next:
	for i := start; i < len(lines); i++ {
		for _, w := range words {
			if !strings.Contains(lines[i], w) {
				continue next
			}
		}
		return i
	}
	return -1
}

func TestPaneOpensInlineUnderTheSelectedRow(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			h := runCardHarness(t, size[0], size[1], true)
			h.selectRun()
			h.send(press("enter"))
			if h.m.pane == nil {
				t.Fatal("enter did not open the pane")
			}
			v := plainView(h.m)
			lines := mainLines(v)
			title := lineWith(lines, 0, "Run "+runCardID)
			if title < 0 {
				t.Fatalf("no pane title:\n%s", v)
			}
			if again := lineWith(lines, title+1, "Run "+runCardID); again >= 0 {
				t.Fatalf("the pane is drawn twice:\n%s", v)
			}
			// The line above the pane is the selected row itself: no blank run.
			row := lineWith(lines, 0, "S30", "review")
			if row < 0 || row != title-1 {
				t.Fatalf("the pane is not directly under the row (row %d, title %d):\n%s", row, title, v)
			}
			// The sections keep their order around it: Needs you, Running, the
			// pane, then Recent.
			needs := lineWith(lines, 0, "Needs you")
			running := lineWith(lines, 0, "Running 1")
			recent := lineWith(lines, 0, "Recent")
			// At 80x24 the pane fills the screen and Recent scrolls off below it.
			if !(needs >= 0 && needs < running && running < row && (recent < 0 || title < recent)) {
				t.Fatalf("sections out of order (needs %d, running %d, row %d, title %d, recent %d):\n%s", needs, running, row, title, recent, v)
			}
			// The pane is whole: its closing hint is on screen.
			if !strings.Contains(v, "esc close pane") {
				t.Fatalf("the pane's hint is cut:\n%s", v)
			}
		})
	}
}

func TestOverviewSectionOrderUnchanged(t *testing.T) {
	h := runCardHarness(t, 100, 30, true)
	lines := mainLines(h.view())
	needs, running := lineWith(lines, 0, "Needs you 2"), lineWith(lines, 0, "Running 1")
	if needs < 0 || running < 0 || needs >= running {
		t.Fatalf("Needs you must stay above Running while a run is live (needs %d, running %d)", needs, running)
	}
	// Opening the pane puts it inside Running, not above Needs you.
	h.selectRun()
	h.send(press("enter"))
	lines = mainLines(plainView(h.m))
	needs, running = lineWith(lines, 0, "Needs you 2"), lineWith(lines, 0, "Running 1")
	title, recent := lineWith(lines, 0, "Run "+runCardID), lineWith(lines, 0, "Recent")
	if !(needs >= 0 && needs < running && running < title && title < recent) {
		t.Fatalf("order with the pane open: needs %d, running %d, pane %d, recent %d", needs, running, title, recent)
	}
}

func TestRoundAndLastHeardSitNextToTheBar(t *testing.T) {
	h := runCardHarness(t, 100, 30, true)
	// In the Overview row the cell after the bar (and its round) is last heard.
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	row := mainRow(plainView(h.m), "S30", "review")
	if !strings.Contains(row, "[~~....] r2  heard 2m") {
		t.Fatalf("the row must hold round and last heard beside the bar: %q", row)
	}
	// So it is in the Running entry.
	h.send(press("["))
	if row := mainRow(h.wide(), "ork", "S30"); !strings.Contains(row, "[~~....] r2  heard 2m") {
		t.Fatalf("the Running entry must hold round and last heard beside the bar: %q", row)
	}
	h.send(press("]"))
	// In the pane they are on the bar's own line, and last heard has no line of its own.
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.selectRun()
	h.send(press("enter"))
	v := plainView(h.m)
	lines := mainLines(v)
	title := lineWith(lines, 0, "Run "+runCardID)
	bar := lineWith(lines, title+1, "[~~....] r2")
	if title < 0 || bar != title+1 {
		t.Fatalf("the bar must be the line under the pane title (title %d, bar %d):\n%s", title, bar, v)
	}
	if !strings.Contains(lines[bar], "[~~....] r2  last heard 2m ago") {
		t.Fatalf("the bar line lacks round and last heard: %q", lines[bar])
	}
	if n := strings.Count(strings.Join(lines[title:], "\n"), "last heard"); n != 1 {
		t.Fatalf("last heard is shown %d times in the pane, want once:\n%s", n, v)
	}
}

func TestLastHeardIsAmberPast10MinutesAndRedPast30(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
		h.m.cfg.Theme = NewTheme(mono)
		th := h.m.cfg.Theme
		for _, tc := range []struct {
			age        time.Duration
			state, cue string
		}{
			{9 * time.Minute, "active", ""},
			{10 * time.Minute, "active", ""},
			{11 * time.Minute, "active", "! "},
			{29 * time.Minute, "active", "! "},
			{30 * time.Minute, "active", "! "},
			{31 * time.Minute, "active", "!! "},
			{5 * time.Minute, "stale", "!! "},
			{40 * time.Minute, "ended", ""},
		} {
			r := h.m.byName["ork"].snap.Runs.Value[0]
			r.State = tc.state
			r.UpdatedAt = h.now.Add(-tc.age).Format(time.RFC3339)
			text, style := h.m.lastHeard(r)
			if want := tc.cue + "heard " + elapsed(tc.age); text != want {
				t.Fatalf("mono %t, %s %s: text %q, want %q", mono, tc.state, tc.age, text, want)
			}
			want := th.Dim
			switch tc.cue {
			case "! ":
				want = th.Warn
			case "!! ":
				want = th.Error
			}
			if style.Render("x") != want.Render("x") {
				t.Fatalf("mono %t, %s %s: wrong style", mono, tc.state, tc.age)
			}
		}
		// Mono keeps the cue in the words; amber is plain text and red is bold.
		if mono && (colourCodes(th.Warn.Render("x")) != nil || colourCodes(th.Error.Render("x")) != nil) {
			t.Fatal("the mono theme sets a colour")
		}
		if !mono && (th.Warn.Render("x") == th.Error.Render("x") || th.Warn.Render("x") == "x") {
			t.Fatal("amber and red must differ from each other and from plain text in colour")
		}
	}
}

// stalledHarness has three relays started elsewhere: one last heard a minute
// ago (the selected row, drawn reversed), one 11 minutes ago and one 31.
func stalledHarness(t *testing.T, mono bool) *harness {
	t.Helper()
	p := proj("ork", false)
	h := newHarness(t, p)
	h.m.cfg.Theme = NewTheme(mono)
	fresh, amber, red := externalRun("ext-fresh", "S40"), externalRun("ext-amber", "S41"), externalRun("ext-red", "S42")
	amber.UpdatedAt = t0.Add(-11 * time.Minute).Format(time.RFC3339)
	red.UpdatedAt = t0.Add(-31 * time.Minute).Format(time.RFC3339)
	h.src.set(snap(p, nil, nil, fresh, amber, red))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 220, Height: 30})
	return h
}

func TestStallColoursReachTheRows(t *testing.T) {
	h := stalledHarness(t, false)
	th := h.m.cfg.Theme
	// The aggregate Overview and the Running entry draw the same two cells.
	for _, entry := range []string{"Overview", "Running"} {
		if entry == "Running" {
			h.send(press("["))
		}
		raw := h.m.View().Content
		if !strings.Contains(raw, th.Warn.Render("! heard 11m")) {
			t.Errorf("%s: no amber cell for 11m:\n%s", entry, raw)
		}
		if !strings.Contains(raw, th.Error.Render("!! heard 31m")) {
			t.Errorf("%s: no red cell for 31m:\n%s", entry, raw)
		}
	}
	// In mono the cues are plain words and no colour is set.
	h = stalledHarness(t, true)
	raw := h.m.View().Content
	if len(colourCodes(raw)) != 0 {
		t.Fatalf("mono set a colour: %q", colourCodes(raw))
	}
	plain := plainView(h.m)
	if !strings.Contains(plain, "! heard 11m") || !strings.Contains(plain, "!! heard 31m") {
		t.Fatalf("mono lacks the cues:\n%s", plain)
	}
}

func TestRowDropsRunIdAndModelAt80x24(t *testing.T) {
	// An engine run: the run id leaves the row and stays in the pane title.
	h := runCardHarness(t, 80, 24, true)
	row := mainRow(h.view80(), "S30", "review")
	if row == "" || strings.Contains(row, runCardID) {
		t.Fatalf("the row must drop the run id at 80x24: %q", row)
	}
	h.selectRun()
	if detail := plainView(h.m); !strings.Contains(detail, runCardID) {
		t.Fatalf("the selected row's detail must keep the run id:\n%s", detail)
	}
	h.send(press("enter"))
	if v := plainView(h.m); lineWith(mainLines(v), 0, "Run "+runCardID) < 0 {
		t.Fatalf("the pane title must keep the run id:\n%s", v)
	}
	// At 100x30 the row keeps it.
	wide := runCardHarness(t, 100, 30, true)
	if row := mainRow(wide.view(), "S30", "review"); !strings.Contains(row, runCardID) {
		t.Fatalf("the row must keep the run id at 100x30: %q", row)
	}

	// A relay started elsewhere: the run id and the vendor/model column leave
	// the row. Given room to spare, the text shows what the terminal width kept.
	p := proj("ork", false)
	ext := externalRun("run-20261019-004308-40d4", "S41")
	ext.Steps[1].Vendor, ext.Steps[1].Model, ext.Steps[1].Task = "codex", "gpt-5", "check"
	rowAt := func(w int) string {
		e := newHarness(t, p)
		e.src.set(snap(p, nil, nil, ext))
		e.start()
		e.m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		_, running := e.m.rows()
		if len(running) != 1 {
			t.Fatalf("%d running rows", len(running))
		}
		return rowText(running[0], 400)
	}
	if text := rowAt(80); strings.Contains(text, "run-2026") || strings.Contains(text, "codex/gpt-5") || !strings.Contains(text, "S41") {
		t.Fatalf("the 80x24 row keeps the run id or the vendor/model: %q", text)
	}
	if text := rowAt(100); !strings.Contains(text, "run-2026") || !strings.Contains(text, "codex/gpt-5") {
		t.Fatalf("the 100x30 row must keep the run id and the vendor/model: %q", text)
	}
	// The selected row's detail line keeps the run id at 80x24.
	e := newHarness(t, p)
	e.src.set(snap(p, nil, nil, ext))
	e.start()
	e.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if detail := plainView(e.m); !strings.Contains(detail, "run-20261019-004308-40d4") {
		t.Fatalf("the detail line lost the run id:\n%s", detail)
	}
}

// view80 is the view at 80x24.
func (h *harness) view80() string {
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return plainView(h.m)
}

func TestGoldenRunCard(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		for _, mono := range []bool{false, true} {
			name := fmt.Sprintf("runcard-%dx%d-%s.txt", size[0], size[1], map[bool]string{false: "color", true: "mono"}[mono])
			t.Run(name, func(t *testing.T) {
				h := runCardHarness(t, size[0], size[1], mono)
				h.selectRun()
				h.send(press("enter"))
				checkGolden(t, name, h.m.View().Content)
			})
		}
	}
}

// titleUnder returns the main-column lines of the view and the index of the
// pane title, which must be the line right after lines[row].
func titleUnder(t *testing.T, h *harness, title string, rowWords ...string) []string {
	t.Helper()
	v := plainView(h.m)
	lines := mainLines(v)
	at := lineWith(lines, 0, title)
	row := lineWith(lines, 0, rowWords...)
	if at < 0 || row < 0 || row >= at {
		t.Fatalf("no row %v above title %q (row %d, title %d):\n%s", rowWords, title, row, at, v)
	}
	// Lines between the row and the title may only be the row's own command lines.
	for _, between := range lines[row+1 : at] {
		if !strings.Contains(between, "slicer land") && !strings.Contains(between, "slicer render") {
			t.Fatalf("%q sits between the row and the pane:\n%s", between, v)
		}
	}
	return lines
}

func TestPaneOpensUnderItsRowOnTheRunningEntryRecentAndReadyRows(t *testing.T) {
	// The Running entry.
	h := runningHarness(t)
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	h.send(press("down")) // beta's relay started elsewhere
	if r, _ := h.m.selectedRow(); r.ledgerID != "ext-b" {
		t.Fatalf("setup: selected %+v", r)
	}
	h.send(press("enter"))
	titleUnder(t, h, "Run ext-b", "beta", "S41")

	// A relay started elsewhere on the Overview, and a Recent row.
	p := proj("ork", false)
	e := newHarness(t, p)
	s := snap(p, nil, nil, externalRun("ext-1", "S41"))
	s.BoardRuns.Value = []slicercli.RelayRun{{ID: "ended-1", Items: []string{"S97"}, State: "ended", Outcome: "pass",
		BeganAt: t0.Add(-30 * time.Minute).Format(time.RFC3339), UpdatedAt: t0.Add(-5 * time.Minute).Format(time.RFC3339),
		Steps: []slicercli.Step{{Role: "implementer", Phase: "done"}}}}
	e.src.set(s)
	e.start()
	e.m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	e.send(press("enter"))
	titleUnder(t, e, "Run ext-1", "started elsewhere", "S41")
	e.send(press("esc"))
	e.send(press("down"))
	e.send(press("enter"))
	titleUnder(t, e, "Run ended-1", "✓ pass", "S97")

	// A Ready row: the pane goes under its landing command lines.
	r := landingHarness(t, `[{"id":"ork-ready","item":"S09","state":"committed","branch":"s09","merged":true}]`)
	r.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	// Enter does not open a Ready row; a pane that was open when its run
	// committed stays open on it.
	r.m.openPane(r.m.readyRows()[0])
	lines := titleUnder(t, r, "Run ork-ready", "S09", "s09", "merged")
	if cmd, title := lineWith(lines, 0, "slicer land"), lineWith(lines, 0, "Run ork-ready"); cmd < 0 || cmd > title {
		t.Fatalf("the pane must come after the Ready row's commands (command %d, title %d)", cmd, title)
	}
}

// The pane belongs to its run, not to the cursor: after the pane follows a newer
// run (S109) the cursor can sit on another row, and the pane stays under its own.
func TestPaneStaysUnderItsRunWhenTheCursorIsElsewhere(t *testing.T) {
	// Overview, Running section: two runs in one project; the cursor is on the first.
	p := proj("ork", false)
	h := newHarness(t, p)
	h.src.set(engineSnap(p, liveEngine("run-first", "S30", "review", testActivity), liveEngine("run-second", "S31", "review", testActivity)))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	rows := h.m.cursorRows()
	h.m.openPane(rows[1])
	if r, _ := h.m.selectedRow(); r.engineID != "run-first" {
		t.Fatalf("setup: the cursor is on %q", r.engineID)
	}
	titleUnder(t, h, "Run run-second", "S31", "review")

	// The Running entry.
	rh := runningHarness(t)
	rh.m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	rh.m.openPane(rh.m.cursorRows()[1])
	titleUnder(t, rh, "Run ext-b", "beta", "S41")

	// A Recent row, with the cursor on the running one above it.
	e := newHarness(t, p)
	s := snap(p, nil, nil, externalRun("ext-1", "S41"))
	s.BoardRuns.Value = []slicercli.RelayRun{{ID: "ended-1", Items: []string{"S97"}, State: "ended", Outcome: "pass",
		BeganAt: t0.Add(-30 * time.Minute).Format(time.RFC3339), UpdatedAt: t0.Add(-5 * time.Minute).Format(time.RFC3339)}}
	e.src.set(s)
	e.start()
	e.m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	e.m.openPane(e.m.recentRows()[0])
	titleUnder(t, e, "Run ended-1", "✓ pass", "S97")
	e.send(press("esc"))
	e.send(press("["))
	e.m.openPane(e.m.recentRows()[0])
	titleUnder(t, e, "Run ended-1", "✓ pass", "S97")

	// Two projects with a run of the same ID: the pane is under its own project's row.
	a, b := proj("alpha", false), proj("beta", false)
	two := newHarness(t, a, b)
	two.src.set(engineSnap(a, liveEngine("run-same", "S30", "review", testActivity)))
	two.src.set(engineSnap(b, liveEngine("run-same", "S77", "review", testActivity)))
	two.start()
	two.m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	two.m.openPane(two.m.cursorRows()[1])
	titleUnder(t, two, "Run run-same", "beta", "S77")
	two.send(press("esc"))
	two.m.openPane(two.m.cursorRows()[0])
	titleUnder(t, two, "Run run-same", "alpha", "S30")
}

func TestInlinePaneKeepsItsRowAndEndOnShortTerminals(t *testing.T) {
	for _, height := range []int{10, 14, 18, 24} {
		h := runCardHarness(t, 100, height, true)
		h.selectRun()
		h.send(press("enter"))
		v := plainView(h.m)
		lines := mainLines(v)
		if lineWith(lines, 0, "S30", "review") < 0 || lineWith(lines, 0, "Run "+runCardID) < 0 {
			t.Errorf("height %d: the row or the pane title is off screen:\n%s", height, v)
		}
		if !strings.Contains(v, "esc close pane") {
			t.Errorf("height %d: the pane's hint is off screen:\n%s", height, v)
		}
		if n := len(strings.Split(v, "\n")); n != height {
			t.Errorf("height %d: the screen is %d lines", height, n)
		}
	}
}

func TestPaneWhoseRunLeftTheSnapshotStaysUnderTheCursor(t *testing.T) {
	h := runCardHarness(t, 100, 30, true)
	h.selectRun()
	h.send(press("enter"))
	p := proj("ork", false)
	gone := snap(p, []adjcli.Record{openRec(11, "ui.first", "First?"), openRec(12, "ui.second", "Second?")}, nil)
	h.m.Update(snapshotMsg{name: "ork", snap: gone})
	v := plainView(h.m)
	lines := mainLines(v)
	missing := lineWith(lines, 0, "no longer in snapshot")
	if missing < 0 {
		t.Fatalf("the pane for a vanished run is missing:\n%s", v)
	}
	if len(lines) != 30 {
		t.Fatalf("the screen is %d lines", len(lines))
	}
	// The cursor clamped to the last row, the second decision: the pane is under it.
	if !strings.Contains(lines[missing-1], "#12") {
		t.Fatalf("the pane is not under the cursor's row:\n%s", v)
	}
}

// A pane left open on a run that committed is under its Ready row's commands,
// and its end stays on screen on a short terminal.
func TestPaneOnAReadyRowKeepsItsEndOnShortTerminals(t *testing.T) {
	for _, height := range []int{12, 16, 24} {
		r := landingHarness(t, `[{"id":"ork-ready","item":"S09","state":"committed","branch":"s09","merged":true}]`)
		r.send(tea.WindowSizeMsg{Width: 120, Height: height})
		r.m.openPane(r.m.readyRows()[0])
		v := plainView(r.m)
		// The pane's end has priority; its row and commands fit from 24 lines.
		if !strings.Contains(v, "esc close pane") || (height >= 24 && lineWith(mainLines(v), 0, "S09", "s09", "merged") < 0) {
			t.Errorf("height %d: the row or the end of the pane is off screen:\n%s", height, v)
		}
	}
}

func TestPaneLastHeardColourFollowsTheThresholds(t *testing.T) {
	for _, tc := range []struct {
		age  time.Duration
		cue  string
		want func(Theme) string
	}{
		{9 * time.Minute, "", func(th Theme) string { return th.Dim.Render("last heard 9m ago") }},
		{11 * time.Minute, "! ", func(th Theme) string { return th.Warn.Render("! last heard 11m ago") }},
		{31 * time.Minute, "!! ", func(th Theme) string { return th.Error.Render("!! last heard 31m ago") }},
	} {
		h := treeHarness(t, []slicercli.RelayRun{{ID: "one", State: "active", UpdatedAt: t0.Add(-tc.age).Format(time.RFC3339),
			Steps: []slicercli.Step{{Role: "implementer", Phase: "start"}}}}, "")
		h.m.cfg.Theme = NewTheme(false)
		lines := h.m.paneLines(100, 40, "")
		want := tc.want(h.m.cfg.Theme)
		if !strings.Contains(lines[1], "  "+want) || strings.Count(strings.Join(lines, "\n"), "last heard") != 1 {
			t.Errorf("%s: the bar line lacks %q: %q", tc.age, want, lines[1])
		}
	}
}

func TestPaneTooNarrowForBarAndLastHeardKeepsLastHeardOnItsOwnLine(t *testing.T) {
	h := treeHarness(t, []slicercli.RelayRun{{ID: "one", State: "active", UpdatedAt: t0.Add(-2 * time.Minute).Format(time.RFC3339),
		Steps: []slicercli.Step{{Role: "implementer", Phase: "start"}}}}, "")
	lines := h.m.paneLines(26, 40, "")
	plain := sgr.ReplaceAllString(strings.Join(lines, "\n"), "")
	if strings.Contains(plain, "] last heard") || !strings.Contains(plain, "last heard 2m ago") {
		t.Fatalf("narrow pane:\n%s", plain)
	}
}

// Fix round: compact rows drop only the vendor/model, not the running step.
func TestCompactRowKeepsTheStepAndDropsOnlyVendorAndModel(t *testing.T) {
	p := proj("ork", false)
	ext := externalRun("run-20261019-004308-40d4", "S41")
	ext.Steps[1].Vendor, ext.Steps[1].Model, ext.Steps[1].Task = "codex", "gpt-5", "check"
	ext.Steps = append(ext.Steps, slicercli.Step{ID: "s3", Role: "gate", Phase: "fail", Vendor: "claude", Model: "opus", Task: "bad"})
	rowAt := func(w int) string {
		e := newHarness(t, p)
		e.src.set(snap(p, nil, nil, ext))
		e.start()
		e.m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		_, running := e.m.rows()
		return rowText(running[0], 400)
	}
	narrow := rowAt(80)
	for _, want := range []string{"running: reviewer: check", "failed: gate: bad"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("the compact row lost %q: %q", want, narrow)
		}
	}
	if strings.Contains(narrow, "codex") || strings.Contains(narrow, "gpt-5") {
		t.Errorf("the compact row keeps the vendor/model: %q", narrow)
	}
	if wide := rowAt(100); !strings.Contains(wide, "running: reviewer codex/gpt-5: check") || !strings.Contains(wide, "failed: gate: bad") {
		t.Errorf("the wide row lost the vendor/model: %q", wide)
	}
}

// A heard cue (! or !!) outlasts the bar: when the two do not fit, the bar goes.
func TestHeardCueOutlastsTheBarOnNarrowRows(t *testing.T) {
	sweep := func(age time.Duration) (barOnly, heardOnly bool) {
		p := proj("ork", false)
		r := externalRun("run-20261019-004308-40d4", "S41")
		r.UpdatedAt = t0.Add(-age).Format(time.RFC3339)
		e := newHarness(t, p)
		e.src.set(snap(p, nil, nil, r))
		e.start()
		e.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		_, running := e.m.rows()
		for room := 20; room < 200; room++ {
			text := rowText(running[0], room)
			hasBar, hasHeard := strings.Contains(text, "[##~~..]"), strings.Contains(text, "heard ")
			barOnly = barOnly || (hasBar && !hasHeard)
			heardOnly = heardOnly || (hasHeard && !hasBar)
		}
		return barOnly, heardOnly
	}
	for _, age := range []time.Duration{16 * time.Minute, 31 * time.Minute} {
		if barOnly, heardOnly := sweep(age); barOnly || !heardOnly {
			t.Errorf("%s: with a cue the bar must go before heard (bar without heard %t, heard without bar %t)", age, barOnly, heardOnly)
		}
	}
	if barOnly, heardOnly := sweep(time.Minute); !barOnly || heardOnly {
		t.Errorf("without a cue heard goes before the bar (bar without heard %t, heard without bar %t)", barOnly, heardOnly)
	}
}

func TestInlinePaneIsIndentedInsideItsRow(t *testing.T) {
	h := runCardHarness(t, 100, 30, false)
	h.selectRun()
	h.send(press("enter"))
	raw := h.m.View().Content
	th := h.m.cfg.Theme
	lines := mainLines(plainView(h.m))
	title := lineWith(lines, 0, "Run "+runCardID)
	hint := lineWith(lines, title, "esc close pane")
	if title < 0 || hint < 0 {
		t.Fatalf("no pane:\n%s", strings.Join(lines, "\n"))
	}
	for i := title; i <= hint; i++ {
		if lines[i] != "" && !strings.HasPrefix(lines[i], "    ") {
			t.Errorf("pane line %d is not indented by 4: %q", i, lines[i])
		}
	}
	// The blank line before the hint is empty, not two spaces.
	if lines[hint-1] != "" {
		t.Errorf("the blank line before the hint is %q", lines[hint-1])
	}
	// The title is in the row style, not the section heading's colour.
	titleText := "Run " + runCardID
	if !strings.Contains(raw, th.Title.Render(titleText)) || strings.Contains(raw, th.Heading.Render(titleText)) {
		t.Errorf("the pane title is not in the row title style:\n%s", raw)
	}
}

// In mono, red is bold and amber is plain: the cue words stay.
func TestMonoRedIsBoldAndAmberIsPlain(t *testing.T) {
	h := stalledHarness(t, true)
	th := h.m.cfg.Theme
	raw := h.m.View().Content
	if red := th.Error.Render("!! heard 31m"); !strings.Contains(red, "\x1b[1m") || !strings.Contains(raw, red) {
		t.Errorf("mono red is not bold in the row: %q", red)
	}
	if amber := th.Warn.Render("! heard 11m"); amber != "! heard 11m" || !strings.Contains(raw, amber) {
		t.Errorf("mono amber is not plain: %q", amber)
	}
}

// Fix round 3: the inline pane's title is the run ID alone (the row above names
// the item and the stage), and the round is shown once, beside the bar.
func TestInlinePaneTitleIsTheRunIDAlone(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		h := runCardHarness(t, size[0], size[1], false)
		h.selectRun()
		h.send(press("enter"))
		lines := mainLines(plainView(h.m))
		title := lineWith(lines, 0, "Run "+runCardID)
		if title < 0 || strings.TrimSpace(lines[title]) != "Run "+runCardID {
			t.Errorf("%dx%d: the pane title is %q, want the run ID alone", size[0], size[1], strings.TrimSpace(lines[max(title, 0)]))
		}
		if !strings.Contains(h.m.View().Content, h.m.cfg.Theme.Title.Render("Run "+runCardID)) {
			t.Errorf("%dx%d: the pane title is not in the row title style", size[0], size[1])
		}
	}
	// The Questions tab has no row above its pane: it keeps the full title.
	q := runCardHarness(t, 100, 30, true)
	q.selectRun()
	q.send(press("enter"))
	if full := strings.Join(q.m.paneLines(100, 30, ""), "\n"); !strings.Contains(full, "Run "+runCardID+"  S30  review") {
		t.Errorf("the plain pane lost its full title:\n%s", full)
	}
}

func TestPipelineLineDropsRoundWhenTheBarShowsIt(t *testing.T) {
	// Round 2: the bar says r2, so the pipeline line has no round.
	h := runCardHarness(t, 100, 30, true)
	h.selectRun()
	h.send(press("enter"))
	lines := mainLines(plainView(h.m))
	bar := lineWith(lines, 0, "[~~....] r2  last heard")
	if bar < 0 || strings.Contains(lines[bar+1], "round") || !strings.Contains(lines[bar+1], "implementer") {
		t.Fatalf("the pipeline line must carry no round when the bar shows r2:\n%s", strings.Join(lines, "\n"))
	}
	// Round 1: the bar has no rN, so the pipeline line keeps "round 1".
	one := treeHarness(t, []slicercli.RelayRun{{ID: "one", State: "active", Steps: []slicercli.Step{{Role: "implementer", Phase: "start"}}}}, "")
	pane := sgr.ReplaceAllString(strings.Join(one.m.paneLines(100, 40, ""), "\n"), "")
	if !strings.Contains(pane, "round 1") {
		t.Errorf("round 1 lost its pipeline-line round:\n%s", pane)
	}
}
