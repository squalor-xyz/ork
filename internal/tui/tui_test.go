// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/poll"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

var t0 = time.Date(2026, 10, 19, 16, 42, 0, 0, time.UTC)

// fakeSource is a Source that returns what the test put in snaps and logs
// every call. It never touches slicer, adj or git.
type fakeSource struct {
	mu    sync.Mutex
	snaps map[string]source.Snapshot
	calls []string
}

func (f *fakeSource) Snapshot(_ context.Context, p registry.Project) source.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, p.Name)
	if s, ok := f.snaps[p.Name]; ok {
		s.Project = p
		return s
	}
	return source.Snapshot{Project: p}
}

func (f *fakeSource) set(s source.Snapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snaps[s.Project.Name] = s
}

// take returns the calls made since the last take.
func (f *fakeSource) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calls
	f.calls = nil
	return c
}

func proj(name string, adj bool) registry.Project {
	return registry.Project{Name: name, Path: "/projects/" + name, Adj: adj}
}

func openRec(id int, key, title string) adjcli.Record {
	return adjcli.Record{ID: id, Key: key, Title: title, Status: "open"}
}

func item(id, title string, flags ...string) slicercli.Item {
	return slicercli.Item{ID: id, Title: title, Status: "open", Fields: slicercli.Fields{Flags: flags}}
}

func relay(id, begun string, items ...string) slicercli.RelayRun {
	return slicercli.RelayRun{ID: id, Items: items, BeganAt: begun, State: "active",
		Counts: slicercli.Counts{Steps: 6, Running: 2}}
}

func relayWithSteps(steps ...slicercli.Step) slicercli.RelayRun {
	r := relay("ledger", "2026-10-19T16:40:00Z", "S30")
	r.Steps = steps
	return r
}

func snap(p registry.Project, open []adjcli.Record, items []slicercli.Item, runs ...slicercli.RelayRun) source.Snapshot {
	return source.Snapshot{
		Project: p,
		Items:   source.Result[[]slicercli.Item]{Value: items},
		Runs:    source.Result[[]slicercli.RelayRun]{Value: runs},
		Open:    source.Result[[]adjcli.Record]{Value: open},
	}
}

// harness drives a Model as a pure update function: it runs the commands an
// Update returns and feeds snapshot results back in.
type harness struct {
	t     *testing.T
	m     *Model
	src   *fakeSource
	now   time.Time
	bells int
}

func plainTheme() Theme { return NewTheme(true) }

func newHarness(t *testing.T, projects ...registry.Project) *harness {
	t.Helper()
	h := &harness{t: t, src: &fakeSource{snaps: map[string]source.Snapshot{}}, now: t0}
	h.m = New(Config{
		Source:     h.src,
		Projects:   projects,
		Now:        func() time.Time { return h.now },
		Schedule:   poll.DefaultSchedule,
		TickEvery:  time.Second,
		FrameAfter: func(time.Duration) tea.Cmd { return nil },
		After:      func(time.Duration) tea.Cmd { return func() tea.Msg { return tickMsg{} } },
		Bell:       func() { h.bells++ },
		Theme:      plainTheme(),
	})
	return h
}

// exec runs a command and returns the messages it produced, flattening batches.
func exec(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, exec(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// send updates the model with msg, runs the returned commands and feeds each
// snapshot result back in. It returns the other messages the commands made
// (ticks, quit).
func (h *harness) send(msg tea.Msg) []tea.Msg {
	h.t.Helper()
	_, cmd := h.m.Update(msg)
	var rest []tea.Msg
	for _, out := range exec(cmd) {
		if _, ok := out.(snapshotMsg); ok {
			rest = append(rest, h.send(out)...)
			continue
		}
		rest = append(rest, out)
	}
	return rest
}

// sendHeld updates the model with msg and runs the returned commands, but
// returns the snapshot results instead of delivering them: the calls have
// started and not yet returned.
func (h *harness) sendHeld(msg tea.Msg) []tea.Msg {
	h.t.Helper()
	_, cmd := h.m.Update(msg)
	return exec(cmd)
}

// start runs Init and delivers every project's first snapshot.
func (h *harness) start() {
	h.t.Helper()
	for _, out := range exec(h.m.Init()) {
		if _, ok := out.(snapshotMsg); ok {
			h.send(out)
		}
	}
}

func (h *harness) advance(d time.Duration) { h.now = h.now.Add(d) }

func press(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// plainView is the view with its styles removed. The tests check words and
// positions. The S11 test renderer wrote no escape codes, so these checks
// read the same text they read before lipgloss v2, which always writes them.
func plainView(m *Model) string {
	return sgr.ReplaceAllString(m.View().Content, "")
}

func (h *harness) view() string {
	h.t.Helper()
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return plainView(h.m)
}

// rowsWith counts the lines of view that hold every one of the words.
func rowsWith(view string, words ...string) int {
	n := 0
next:
	for _, line := range strings.Split(view, "\n") {
		for _, w := range words {
			if !strings.Contains(line, w) {
				continue next
			}
		}
		n++
	}
	return n
}

// activeTab returns the name inside the bracketed tab.
func activeTab(t *testing.T, view string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(line, "> ") {
			return strings.Fields(strings.TrimPrefix(line, "> "))[0]
		}
	}
	t.Fatalf("no selected sidebar row in %q", view)
	return ""
}

func names(calls []string) []string {
	out := append([]string(nil), calls...)
	// order of concurrent fetches is not defined
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

var four = []registry.Project{proj("slicer", true), proj("adjudicate", true), proj("DataBall", false), proj("agents", true)}

func TestInitFetchesEveryProjectOnce(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	if got := names(h.src.take()); !reflect.DeepEqual(got, []string{"DataBall", "adjudicate", "agents", "slicer"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestTabKeysSwitchTheProject(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	if got := activeTab(t, h.view()); got != "Needs" {
		t.Fatalf("first tab = %s", got)
	}
	steps := []struct{ k, want string }{
		{"]", "slicer"},
		{"]", "adjudicate"},
		{"[", "slicer"},
		{"[", "Needs"},
		{"[", "Running"},
		{"[", "agents"},
		{"]", "Running"},
		{"]", "Needs"},
		{"3", "DataBall"},
		{"9", "DataBall"}, // no such tab
		{"0", "Needs"},
		{"1", "slicer"},
		{"4", "agents"},
	}
	for _, s := range steps {
		h.send(press(s.k))
		if got := activeTab(t, h.view()); got != s.want {
			t.Fatalf("after %q the tab is %s, want %s", s.k, got, s.want)
		}
	}
}

// Tab and shift+tab reach built views without moving the project or Overview cursor
// (ui.views, ui.view-list).
func TestViewKeysDoNotMoveTheProject(t *testing.T) {
	for _, k := range []string{"tab", "shift+tab"} {
		t.Run(k, func(t *testing.T) {
			h := newHarness(t, four...)
			h.src.set(snap(four[0], []adjcli.Record{openRec(11, "a.one", "first question"), openRec(12, "a.two", "second question")}, nil))
			h.start()
			h.send(press("down"))
			if h.m.cursor != 1 {
				t.Fatalf("cursor = %d after n, want 1", h.m.cursor)
			}
			h.send(press(k))
			if h.m.active != -1 || h.m.cursor != 1 {
				t.Errorf("after %s: active %d, cursor %d; want Needs you and cursor 1", k, h.m.active, h.m.cursor)
			}
			v := h.view()
			if !strings.Contains(v, "> Needs you") {
				t.Errorf("after %s the sidebar selection moved:\n%s", k, v)
			}
			if bar := strings.Split(v, "\n")[0]; !strings.Contains(bar, "Overview │ Questions 2 │ Waves │ Roadmap │ Decisions") {
				t.Errorf("after %s the view bar is %q", k, bar)
			}
			want := "Questions 2"
			if k == "shift+tab" {
				want = "Decisions"
			}
			if !strings.Contains(h.m.View().Content, h.m.cfg.Theme.ViewActive.Render(want)) {
				t.Errorf("after %s %s is not the active view", k, want)
			}
		})
	}
}

// View keys set no action message. Returning to Overview keeps its message.
func TestViewKeysSetNoMessage(t *testing.T) {
	const msg = "No engine run on this row; no actions"
	for _, k := range []string{"tab", "shift+tab"} {
		t.Run(k, func(t *testing.T) {
			h := newHarness(t, four...)
			h.start()
			h.send(press("a")) // no engine run under the cursor: a message
			if v := h.view(); !strings.Contains(v, msg) {
				t.Fatalf("setup: no message on screen:\n%s", v)
			}
			h.send(press(k))
			h.send(press(k))
			h.send(press(k)) // return to Overview
			if v := h.view(); !strings.Contains(v, msg) {
				t.Errorf("after %s the message is gone:\n%s", k, v)
			}
		})
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		h := newHarness(t, four...)
		h.start()
		msgs := h.send(press(k))
		quit := false
		for _, m := range msgs {
			if _, ok := m.(tea.QuitMsg); ok {
				quit = true
			}
		}
		if !quit {
			t.Errorf("%q did not quit: %v", k, msgs)
		}
	}
}

func TestHelpKeyTogglesTheHelp(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	if strings.Contains(h.view(), "next project") {
		t.Fatal("help shown before ?")
	}
	h.send(press("?"))
	v := h.view()
	for _, want := range []string{"tab next view", "shift+tab previous view", "] next project", "[ previous project", "jump to project", "j/k down/up row", "refresh now", "quit"} {
		if !strings.Contains(v, want) {
			t.Errorf("help lacks %q:\n%s", want, v)
		}
	}
	h.send(press("?"))
	if strings.Contains(h.view(), "next project") {
		t.Error("? did not close the help")
	}
	h.send(press("?"))
	h.send(press("esc"))
	if strings.Contains(h.view(), "next project") {
		t.Error("esc did not close the help")
	}
}

func TestRefreshKeyRefreshesTheVisibleProjectNow(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(press("1"))
	h.src.take()
	h.advance(time.Second) // nothing is due
	h.send(press("]"))
	h.send(press("r"))
	if got := h.src.take(); !reflect.DeepEqual(got, []string{"adjudicate"}) {
		t.Fatalf("calls after r = %v", got)
	}
}

func TestRefreshKeyDoesNotStartASecondCallInFlight(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(press("1"))
	h.src.take()
	held := h.sendHeld(press("r")) // the call is out and has not returned
	h.sendHeld(press("r"))
	h.advance(10 * time.Second)
	h.sendHeld(tickMsg{}) // the visible project is due by time, still in flight
	if got := h.src.take(); !reflect.DeepEqual(got, []string{"slicer"}) {
		t.Fatalf("calls = %v, want one call for slicer", got)
	}
	for _, m := range held {
		h.send(m)
	}
	h.send(press("r"))
	if got := h.src.take(); !reflect.DeepEqual(got, []string{"slicer"}) {
		t.Fatalf("after the call returned, r made %v", got)
	}
}

func TestPollRefreshesVisibleEvery5sAndOthersEvery30s(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(press("1"))
	h.src.take()
	// 0..4 s: nothing
	h.advance(4 * time.Second)
	h.send(tickMsg{})
	if got := h.src.take(); got != nil {
		t.Fatalf("at 4 s: %v", got)
	}
	// 5 s: only the visible project
	h.advance(time.Second)
	h.send(tickMsg{})
	if got := h.src.take(); !reflect.DeepEqual(got, []string{"slicer"}) {
		t.Fatalf("at 5 s: %v", got)
	}
	// the slicer call returned at 5 s; the others were last refreshed at 0 s
	h.advance(24 * time.Second) // 29 s
	h.send(tickMsg{})
	got := names(h.src.take())
	if !reflect.DeepEqual(got, []string{"slicer"}) {
		t.Fatalf("at 29 s: %v", got)
	}
	h.advance(time.Second) // 30 s
	h.send(tickMsg{})
	got = names(h.src.take())
	if !reflect.DeepEqual(got, []string{"DataBall", "adjudicate", "agents"}) {
		t.Fatalf("at 30 s: %v (slicer returned at 29 s, so it is not due)", got)
	}
}

func TestSwitchingTabsMakesTheNewProjectTheFastOne(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.src.take()
	h.send(press("3")) // DataBall is now visible
	h.advance(5 * time.Second)
	h.send(tickMsg{})
	if got := h.src.take(); !reflect.DeepEqual(got, []string{"DataBall"}) {
		t.Fatalf("at 5 s on DataBall: %v", got)
	}
}

func TestEachTickSchedulesTheNextTick(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	after := 0
	h.m.cfg.After = func(d time.Duration) tea.Cmd {
		after++
		if d != time.Second {
			t.Errorf("tick every %v", d)
		}
		return func() tea.Msg { return tickMsg{} }
	}
	h.send(tickMsg{})
	if after != 1 {
		t.Fatalf("After called %d times", after)
	}
}

func TestPendingCallShowsOnItsRow(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0], []adjcli.Record{openRec(11, "ui.slicer-tui", "A question")}, nil))
	var held []tea.Msg
	for _, m := range exec(h.m.Init()) {
		if s, ok := m.(snapshotMsg); ok && s.name == "slicer" {
			held = append(held, m)
			continue
		}
		if s, ok := m.(snapshotMsg); ok {
			h.send(s)
		}
	}
	v := h.view()
	if n := rowsWith(sgr.ReplaceAllString(strings.Join(h.m.overview(72).body, "\n"), ""), "waiting for the first reply"); n != 2 { // one in Needs you, one in Running
		t.Fatalf("%d pending rows for slicer, want 2:\n%s", n, v)
	}
	if strings.Contains(v, "~slicer") {
		t.Fatalf("a project with no data yet carries the refresh mark, not only its pending row:\n%s", v)
	}
	if strings.Contains(v, "A question") {
		t.Fatalf("data shown before the call returned:\n%s", v)
	}
	if !strings.Contains(v, "pending 1") {
		t.Errorf("status line lacks 'pending 1':\n%s", v)
	}
	h.send(held[0])
	v = h.view()
	if rowsWith(v, "waiting for the first reply") != 0 || !strings.Contains(v, "A question") {
		t.Fatalf("row still pending after the call returned:\n%s", v)
	}
	if strings.Contains(v, "pending 1") {
		t.Errorf("status line still pending:\n%s", v)
	}
}

// Each error row is cut in the Overview, and its full tool text is on the
// detail line once the row is selected: the cursor is on the items error, then
// on the runs error.
func TestFailedCallShowsItsCodeAndMessageVerbatim(t *testing.T) {
	h := newHarness(t, four...)
	bad := snap(four[2], nil, nil)
	bad.Items = source.Result[[]slicercli.Item]{Err: &clirun.ToolError{Code: "no_such_item", Message: "No such item: NOPE.", Command: "slicer show NOPE"}}
	bad.Runs = source.Result[[]slicercli.RelayRun]{Err: errors.New("slicer relay-run list: exit 3: weird  spacing, 100% \"quoted\"")}
	h.src.set(bad)
	h.start()
	v := h.view()
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "! ") && strings.Contains(line, "items:") && !strings.Contains(line, "DataBall") {
			t.Errorf("error is not on the project's row: %q", line)
		}
	}
	if got := detailText(h.m, 72); got != "items: no_such_item: No such item: NOPE." {
		t.Errorf("the items error is not on the detail line: %q", got)
	}
	h.send(press("down"))
	if got := detailText(h.m, 72); got != "runs: slicer relay-run list: exit 3: weird  spacing, 100% \"quoted\"" {
		t.Errorf("the runs error is not verbatim on the detail line: %q", got)
	}
	if v := h.view(); !strings.Contains(v, "runs: slicer relay-run list") {
		t.Errorf("the runs error row is not in the view:\n%s", v)
	}
}

// reverseSGR matches an SGR sequence that sets reverse video, alone or with others.
var reverseSGR = regexp.MustCompile(`\x1b\[(?:[0-9]+;)*7(?:;[0-9]+)*m`)

// reversed reports whether the screen line with text is drawn reversed there.
func reversed(m *Model, text string) bool {
	for _, l := range strings.Split(m.screen(), "\n") {
		if i := strings.Index(l, text); i >= 0 {
			loc := reverseSGR.FindStringIndex(l)
			return loc != nil && loc[0] < i
		}
	}
	return false
}

// detailText is the selected row's full text, read back from the detail lines
// at a pane of w columns. The lines are joined with one space, which is how a
// line break was made (wrapText), so the text is the row's text verbatim.
func detailText(m *Model, w int) string {
	var parts []string
	for _, l := range m.overview(w).detail {
		parts = append(parts, strings.TrimPrefix(l, indent))
	}
	return strings.Join(parts, " ")
}

// A row longer than the pane is cut with "..." and stays one line (decision
// ui.setup-errors). Its full text is on the detail line of the selected row.
func TestLongRowsAreCutWithAnEllipsisNotWrapped(t *testing.T) {
	h := newHarness(t, four...)
	bad := snap(four[2], nil, nil)
	msg := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa quebec romeo"
	bad.Items = source.Result[[]slicercli.Item]{Err: &clirun.ToolError{Code: "too_long", Message: msg}}
	h.src.set(bad)
	h.start()
	h.send(press("s"))
	h.send(tea.WindowSizeMsg{Width: 60, Height: 30})
	v := plainView(h.m)
	var rows []string
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "! ") {
			rows = append(rows, strings.TrimRight(l, " "))
		}
	}
	if len(rows) != 1 {
		t.Fatalf("the error row is %d lines, want 1:\n%s", len(rows), v)
	}
	if !strings.HasSuffix(rows[0], "...") {
		t.Errorf("the row is not cut with an ellipsis: %q", rows[0])
	}
	if w := lipgloss.Width(rows[0]); w > 60 {
		t.Errorf("the row is %d wide in a 60-column pane: %q", w, rows[0])
	}
	if got := detailText(h.m, 60); got != "items: too_long: "+msg {
		t.Errorf("the detail line is not the full text:\n got %q\nwant %q", got, "items: too_long: "+msg)
	}
}

// The row cursor moves through the Overview's rows, Needs you then Running, and
// stops at the first and last row. Its detail line is the row under it.
func TestRowCursorMovesThroughTheRows(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0], []adjcli.Record{openRec(11, "a.one", "first question"), openRec(12, "a.two", "second question")}, nil,
		relay("run-1", "2026-10-19T16:00:00Z", "S01")))
	h.start()
	h.send(press("s")) // no sidebar: its own reverse selection would share the line
	h.view()
	if got := detailText(h.m, 72); got != "#11 open first question" {
		t.Fatalf("the first row is not selected: %q", got)
	}
	if !reversed(h.m, "#11  a.one") || reversed(h.m, "#12  a.two") {
		t.Errorf("the cursor row is not the reversed one")
	}
	h.send(press("down"))
	if got := detailText(h.m, 72); got != "#12 open second question" {
		t.Fatalf("n did not move to the second row: %q", got)
	}
	if reversed(h.m, "#11  a.one") || !reversed(h.m, "#12  a.two") {
		t.Errorf("the reverse does not follow the cursor")
	}
	h.send(press("down"))
	if got := detailText(h.m, 72); !strings.HasPrefix(got, "run-1  S01") {
		t.Fatalf("n did not move to the run row: %q", got)
	}
	h.send(press("down"))
	if h.m.cursor != 2 {
		t.Errorf("the cursor went past the last row: stored %d, want 2", h.m.cursor)
	}
	if got := detailText(h.m, 72); !strings.HasPrefix(got, "run-1  S01") {
		t.Errorf("the cursor left the last row: %q", got)
	}
	h.send(press("up"))
	if got := detailText(h.m, 72); !strings.HasPrefix(got, "#12 open") {
		t.Errorf("p did not move back: %q", got)
	}
	if h.m.active != -1 {
		t.Errorf("row keys moved the project selection to %d", h.m.active)
	}
}

// Project keys leave the row cursor on the first row of the new project, and
// row keys leave the project alone.
func TestProjectKeysResetTheRowCursor(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0], []adjcli.Record{openRec(11, "a.one", "first question"), openRec(12, "a.two", "second question")}, nil))
	h.start()
	h.send(press("down"))
	if h.m.cursor != 1 {
		t.Fatalf("cursor = %d after n, want 1", h.m.cursor)
	}
	h.send(press("]")) // to slicer
	if h.m.active != 0 || h.m.cursor != 0 {
		t.Errorf("right: active %d, cursor %d; want slicer's first row", h.m.active, h.m.cursor)
	}
	h.send(press("[")) // back to Needs you
	if h.m.active != -1 || h.m.cursor != 0 {
		t.Errorf("left: active %d, cursor %d; want Needs you's first row", h.m.active, h.m.cursor)
	}
}

// Down and j, up and k move the row cursor through the Overview's rows and stop
// at the first and last row. They leave the project alone (ui.row-keys).
func TestRowKeysMoveTheCursorNotTheProject(t *testing.T) {
	for _, tc := range []struct{ name, next, prev string }{{"arrows", "down", "up"}, {"letters", "j", "k"}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, four...)
			h.src.set(snap(four[0], []adjcli.Record{openRec(11, "a.one", "first question"), openRec(12, "a.two", "second question")}, nil,
				relay("run-1", "2026-10-19T16:00:00Z", "S01")))
			h.start()
			h.view()
			for _, s := range []struct{ key, row string }{
				{tc.next, "#12 open"}, {tc.next, "run-1  S01"}, {tc.next, "run-1  S01"},
				{tc.prev, "#12 open"}, {tc.prev, "#11 open"}, {tc.prev, "#11 open"},
			} {
				h.send(press(s.key))
				if got := detailText(h.m, 72); !strings.HasPrefix(got, s.row) {
					t.Fatalf("after %s the cursor row is %q, want %q", s.key, got, s.row)
				}
				if v := h.view(); !strings.Contains(v, "> Needs you") {
					t.Fatalf("after %s the project changed:\n%s", s.key, v)
				}
			}
		})
	}
}

// Each navigation key does what the help says. The cursor starts on the second
// row of Needs you. Row keys move the cursor and keep the project. Project keys
// move the project and put the cursor on its first row. Tab and shift+tab switch
// views and move neither (ui.views). 9 has no project to jump to, so nothing
// moves (ui.row-keys).
func TestNavigationKeysDoWhatTheHelpSays(t *testing.T) {
	for _, tc := range []struct{ key, tab, row string }{
		{"down", "Needs", "run-1  S01"},
		{"j", "Needs", "run-1  S01"},
		{"up", "Needs", "#11 open"},
		{"k", "Needs", "#11 open"},
		{"tab", "Needs", "#12 open"},
		{"shift+tab", "Needs", "#12 open"},
		{"]", "slicer", "#11 open"},
		{"2", "adjudicate", ""},
		{"[", "Running", ""},
		{"9", "Needs", "#12 open"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			h := newHarness(t, four...)
			h.src.set(snap(four[0], []adjcli.Record{openRec(11, "a.one", "first question"), openRec(12, "a.two", "second question")}, nil,
				relay("run-1", "2026-10-19T16:00:00Z", "S01")))
			h.start()
			h.m.cursor = 1 // setup without a row key, so each key is tested alone
			if got := detailText(h.m, 72); !strings.HasPrefix(got, "#12 open") {
				t.Fatalf("setup: the cursor row is %q, want #12 open", got)
			}
			h.send(press(tc.key))
			if got := activeTab(t, h.view()); got != tc.tab {
				t.Fatalf("%s selected %s, want %s", tc.key, got, tc.tab)
			}
			if tc.row != "" {
				if got := detailText(h.m, 72); !strings.HasPrefix(got, tc.row) {
					t.Fatalf("after %s the cursor row is %q, want %q", tc.key, got, tc.row)
				}
			}
		})
	}
}

// down reaches a stopped run below the decision on Needs you, and Enter opens its
// pane. The stopped S21 and S29 rows are reached with the keys alone.
func TestDownReachesAStoppedRunForEnter(t *testing.T) {
	h := newHarness(t, four...)
	s := snap(four[0], []adjcli.Record{openRec(3, "owner.choice", "Choose storage")}, nil)
	s.EngineRuns = []source.EngineRun{{ID: "engine", Item: "S21", State: "stopped", Wait: "owner action"}}
	h.src.set(s)
	h.start()
	h.view()
	if got := detailText(h.m, 72); !strings.HasPrefix(got, "#3 open") {
		t.Fatalf("the first row is %q, want the decision", got)
	}
	h.send(press("down"))
	if got := detailText(h.m, 72); !strings.HasPrefix(got, "engine  S21  stopped") {
		t.Fatalf("down did not move the cursor to the stopped run: %q", got)
	}
	h.send(press("enter"))
	if v := h.view(); !strings.Contains(v, "Run engine") || !strings.Contains(v, "Stopped; relaunch, or clear with: ork dismiss engine") {
		t.Fatalf("enter did not open the stopped run's pane:\n%s", v)
	}
}

// The dashboard doc lists the keys decided in ui.row-keys and ui.views.
func TestDashboardDocsListTheNavigationKeys(t *testing.T) {
	b, err := os.ReadFile("../../docs/dashboard.md")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(strings.Fields(string(b)), " ")
	for _, want := range []string{
		"In every view, keys `]` (next) and `[` (previous) move selection and wrap through \"Running\", \"Needs you\" and the sorted projects",
		"`left` and `right` never change the project",
		"The first sidebar entry, above \"Needs you\", opens a live summary of every project that has a live run.",
		"`1`-`9` select the first nine projects in the order the sidebar draws them, and the sidebar shows those numbers dimmed.",
		"`0` selects \"Needs you\"; the Running entry has no number.",
		"\"Needs you\" shows a dimmed `0` there.",
		"`g`/`home` and `G`/`end` move the cursor to the first and last row.",
		"`pgdown`/`pgup` move it by the number of rows the body draws on screen",
		"`}` and `{` move it to the first row of the next group and of the current or previous group",
		"`tab` and `shift+tab` switch views",
		"`space` ticks or unticks the selected item",
		"`e` edits the selected ticked item",
		"`l` launches the ticked items with one y/n confirmation",
		"`down`/`j` and `up`/`k` move it to the next and previous row.",
		"`n` and `p` do not move rows; `n` is No in a confirmation.",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("dashboard.md lacks %q", want)
		}
	}
	if strings.Contains(s, "select projects in registry order") {
		t.Error("dashboard.md still says the number keys follow registry order")
	}
	if strings.Contains(s, "Keys `tab`/`shift+tab` and `left`/`right` move selection") {
		t.Error("dashboard.md still lists tab and shift+tab as project keys")
	}
	if strings.Contains(s, "`up`/`down`, `k`/`j`") {
		t.Error("dashboard.md still lists up, down, k and j as project keys")
	}
}

// enter selects the project of the row under the cursor.
func TestEnterSelectsTheRowsProject(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[1], []adjcli.Record{openRec(3, "output.shape", "Which JSON shape?")}, nil))
	h.start()
	h.send(press("enter"))
	if h.m.active != 1 {
		t.Fatalf("enter selected project %d, want 1 (adjudicate)", h.m.active)
	}
	if got := detailText(h.m, 72); got != "#3 open Which JSON shape?" {
		t.Errorf("the selected project's row is not under the cursor: %q", got)
	}
}

// A selected row's full text is on the detail line, wrapped at the pane's
// width without cutting a word, while the row itself is cut to one line.
func TestSelectedRowShowsItsFullTextOnTheDetailLine(t *testing.T) {
	h := newHarness(t, four...)
	long := "Does ork reimplement slicer's roadmap editor or reuse slicer tui, and which screens come first?"
	h.src.set(snap(four[0], []adjcli.Record{openRec(11, "ui.slicer-tui", long)}, nil))
	h.start()
	h.send(press("s"))
	h.send(tea.WindowSizeMsg{Width: 60, Height: 30})
	for _, d := range h.m.overview(60).detail {
		if w := lipgloss.Width(d); w > 60 {
			t.Errorf("detail line is %d wide in a 60-column pane: %q", w, d)
		}
	}
	if got := detailText(h.m, 60); got != "#11 open "+long {
		t.Errorf("the detail line is not the full text:\n got %q", got)
	}
	v := plainView(h.m)
	if rowsWith(v, "?", "#11") != 1 {
		t.Errorf("the row is not one line with its kind:\n%s", v)
	}
}

// A relay started elsewhere is named by its slices, not by its run ID (S145):
// the row is cut to the pane, and the run ID is on the detail line.
func TestRunRowNamesItsSlicesNotItsID(t *testing.T) {
	const id = "run-20261019-004308-40d4"
	h := newHarness(t, four...)
	h.src.set(snap(four[0], nil, nil, relay(id, "2026-10-19T15:42:00Z", "S01", "S02")))
	h.start()
	h.send(press("s"))
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	sec := runningSection(plainView(h.m))
	i := rowIndex(sec, "S01,S02")
	if i < 0 {
		t.Fatalf("no row for the relay:\n%s", strings.Join(sec, "\n"))
	}
	row := strings.TrimRight(sec[i], " ")
	if strings.Contains(row, id) || strings.Contains(row, "1h00m") || strings.Contains(row, "started elsewhere") {
		t.Errorf("the row carries the run ID, the elapsed time or the label: %q", row)
	}
	if got := detailText(h.m, 100); !strings.Contains(got, id+"  S01,S02  active, 2 of 6 steps running  1h00m") {
		t.Errorf("the detail line lacks the full run text: %q", got)
	}
}

// The body scrolls so that the selected row stays on screen, with a marker for
// the lines hidden above.
func TestCursorScrollsToTheSelectedRow(t *testing.T) {
	h := newHarness(t, four...)
	var recs []adjcli.Record
	for i := 1; i <= 15; i++ {
		recs = append(recs, openRec(i, "k.k", fmt.Sprintf("question %02d", i)))
	}
	h.src.set(snap(four[0], recs, nil))
	h.start()
	h.send(tea.WindowSizeMsg{Width: 100, Height: 12})
	for i := 0; i < 14; i++ {
		h.send(press("down"))
	}
	v := plainView(h.m)
	if rowsWith(v, "?", "question 15") != 1 {
		t.Fatalf("the selected row is not on screen:\n%s", v)
	}
	if !strings.Contains(v, "lines above") {
		t.Errorf("no marker for the lines hidden above:\n%s", v)
	}
	if n := len(strings.Split(v, "\n")); n != 12 {
		t.Errorf("%d lines, want 12", n)
	}
}

// noSlicer is the snapshot of a project with no .slicer/ folder: both slicer
// calls fail with the state error slicer prints there (review-2026-10-19-integration.md).
func noSlicer(p registry.Project) source.Snapshot {
	msg := "no .slicer/ found in " + p.Path
	s := snap(p, nil, nil)
	s.Items = source.Result[[]slicercli.Item]{Err: &clirun.ToolError{Code: "state", Message: msg, Command: "list"}}
	s.Runs = source.Result[[]slicercli.RelayRun]{Err: &clirun.ToolError{Code: "state", Message: msg, Command: "relay-run list"}}
	return s
}

// A project with no .slicer/ is shown once, dimmed in the sidebar as "no slicer".
// sidebarRow is the sidebar line whose text starts with prefix, with its
// styles, from the full screen.
func sidebarRow(m *Model, prefix string) string {
	for _, l := range strings.Split(m.screen(), "\n") {
		if strings.HasPrefix(sgr.ReplaceAllString(l, ""), prefix) {
			return l
		}
	}
	return ""
}

// faintSGR matches an SGR sequence that sets faint (dim), alone or with others.
var faintSGR = regexp.MustCompile(`\x1b\[(?:[0-9]+;)*2(?:;[0-9]+)*m`)

// dimmed reports whether the faint style is set before the text at name.
func dimmed(row, name string) bool {
	loc := faintSGR.FindStringIndex(row)
	return loc != nil && loc[0] < strings.Index(row, name)
}

func TestNoSlicerProjectIsDimmedInTheSidebar(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(noSlicer(four[2]))
	h.start()
	h.view()
	row := sidebarRow(h.m, "3 DataBall")
	if plain := sgr.ReplaceAllString(row, ""); !strings.Contains(compactSpaces(plain), "DataBall no slicer") {
		t.Fatalf("sidebar row is not \"DataBall no slicer\": %q", plain)
	}
	if !dimmed(row, "DataBall") {
		t.Fatalf("the sidebar row is not dimmed: %q", row)
	}
	// Either slicer call may show the state error; the items call may succeed.
	runsOnly := snap(four[2], nil, nil)
	runsOnly.Runs = noSlicer(four[2]).Runs
	runs := newHarness(t, four...)
	runs.src.set(runsOnly)
	runs.start()
	if v := runs.view(); !strings.Contains(compactSpaces(v), "DataBall no slicer") {
		t.Errorf("a runs error alone is not setup:\n%s", v)
	}
	// Only the state error with the "no .slicer/ found" message is setup. The
	// same code with another message, or the message with another code, is an
	// ordinary error row with a count-free badge.
	for _, c := range []struct {
		err  *clirun.ToolError
		want string
	}{
		{&clirun.ToolError{Code: "locked", Message: "no .slicer/ found", Command: "list"}, "items: locked: no .slicer/ found"},
		{&clirun.ToolError{Code: "state", Message: "no ledger", Command: "list"}, "items: state: no ledger"},
	} {
		other := newHarness(t, four...)
		s := snap(four[2], nil, nil)
		s.Items = source.Result[[]slicercli.Item]{Err: c.err}
		other.src.set(s)
		other.start()
		if v := other.view(); !strings.Contains(v, c.want) || strings.Contains(v, "no slicer") {
			t.Errorf("%q is not an error row:\n%s", c.want, v)
		}
	}
}

func TestNoSlicerProjectAppearsInNoOverviewSection(t *testing.T) {
	h := newHarness(t, four...)
	s := noSlicer(four[2])
	s.Open = source.Result[[]adjcli.Record]{Value: []adjcli.Record{openRec(5, "x.y", "a question for setup")}}
	h.src.set(s)
	h.start()
	h.view()
	h.send(press("s")) // the Overview alone, without the sidebar
	v := plainView(h.m)
	for _, gone := range []string{"DataBall", "a question for setup", "no .slicer/ found", "state:"} {
		if strings.Contains(v, gone) {
			t.Errorf("the Overview shows %q for a project with no .slicer/:\n%s", gone, v)
		}
	}
}

// A project with no .slicer/ adds nothing to the bell: its adj inbox rises, but
// it does not ring, and its badge stays "no slicer" (never a count).
func TestNoSlicerProjectAddsNothingToCountsOrBell(t *testing.T) {
	h := newHarness(t, four...)
	s := noSlicer(four[2])
	s.Open = source.Result[[]adjcli.Record]{Value: []adjcli.Record{openRec(5, "x.y", "q")}}
	h.src.set(s)
	h.start()
	s.Open = source.Result[[]adjcli.Record]{Value: []adjcli.Record{openRec(5, "x.y", "q"), openRec(6, "x.z", "r")}}
	h.src.set(s)
	refreshProject(h, "DataBall")
	s.Open = source.Result[[]adjcli.Record]{Value: []adjcli.Record{openRec(5, "x.y", "q"), openRec(6, "x.z", "r"), openRec(7, "x.w", "s")}}
	h.src.set(s)
	refreshProject(h, "DataBall")
	if h.bells != 0 {
		t.Fatalf("bells = %d: a project with no .slicer/ rang the bell", h.bells)
	}
	if v := h.view(); !strings.Contains(compactSpaces(v), "DataBall no slicer") || strings.Contains(compactSpaces(v), "DataBall 3") {
		t.Errorf("the badge is not \"no slicer\":\n%s", v)
	}
}

// Selecting the project shows its setup hint, the only place it is shown.
func TestSelectedNoSlicerProjectGivesTheInitHint(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(noSlicer(four[2]))
	h.start()
	if v := h.view(); strings.Contains(v, "slicer init") { // Needs you is selected at start
		t.Errorf("the init hint shows on Needs you:\n%s", v)
	}
	h.send(press("3")) // DataBall, third in the sidebar
	if v := h.view(); !strings.Contains(v, "no .slicer/") || !strings.Contains(v, "slicer init") {
		t.Fatalf("the selected project gives no init hint:\n%s", v)
	}
	if row := sidebarRow(h.m, "> DataBall"); !dimmed(row, "DataBall") {
		t.Errorf("the selected project's sidebar row is not dimmed: %q", row)
	}
}

// Errors merge into one row per project and code. The parts that failed are
// listed before the code, and the tool's message follows it. A merged row with
// an items or inbox part sits under Needs you. Another code stays its own row.
func TestErrorsMergePerCodeAndPart(t *testing.T) {
	h := newHarness(t, four...)
	s := snap(four[2], nil, nil)
	s.Items = source.Result[[]slicercli.Item]{Err: &clirun.ToolError{Code: "locked", Message: "Ledger is busy."}}
	s.Runs = source.Result[[]slicercli.RelayRun]{Err: &clirun.ToolError{Code: "locked", Message: "Ledger is busy."}}
	h.src.set(s)
	h.start()
	v := h.view()
	merged := strings.Index(v, "items, runs: locked: Ledger is busy.")
	if merged < 0 {
		t.Fatalf("the merged row is missing:\n%s", v)
	}
	if n := rowsWith(v, "!", "locked"); n != 1 {
		t.Errorf("%d rows for the code, want 1:\n%s", n, v)
	}
	if running := strings.Index(v, "│Running"); merged < strings.Index(v, "│Needs you") || merged > running {
		t.Errorf("the merged row is not under Needs you:\n%s", v)
	}
	other := newHarness(t, four...)
	s.Runs = source.Result[[]slicercli.RelayRun]{Err: &clirun.ToolError{Code: "timeout", Message: "Relay runs timed out."}}
	other.src.set(s)
	other.start()
	if v := other.view(); !strings.Contains(v, "items: locked: Ledger is busy.") || !strings.Contains(v, "runs: timeout: Relay runs timed out.") {
		t.Errorf("another code did not stay its own row:\n%s", v)
	}
}

// Messages that differ under one code share its row, joined in part order.
func TestMergedErrorsKeepEachMessage(t *testing.T) {
	h := newHarness(t, four...)
	s := snap(four[2], nil, nil)
	s.Items = source.Result[[]slicercli.Item]{Err: &clirun.ToolError{Code: "locked", Message: "Ledger is busy."}}
	s.Runs = source.Result[[]slicercli.RelayRun]{Err: &clirun.ToolError{Code: "locked", Message: "Runs are busy."}}
	h.src.set(s)
	h.start()
	h.send(press("s")) // the full width, so the row is not cut
	if v := h.view(); !strings.Contains(v, "items, runs: locked: Ledger is busy. / Runs are busy.") {
		t.Errorf("the messages are not both shown:\n%s", v)
	}
}

// refreshProject reads one project again, as the poller does when it is due.
func refreshProject(h *harness, name string) {
	h.advance(5 * time.Second)
	for _, msg := range exec(h.m.fetch(name, false)) {
		h.send(msg)
	}
}

func TestFailedAdjCallShowsAnErrorRow(t *testing.T) {
	h := newHarness(t, four...)
	s := snap(four[0], nil, nil)
	s.Open = source.Result[[]adjcli.Record]{Err: &clirun.ToolError{Code: "bad_root", Message: "Not an adj project."}}
	h.src.set(s)
	h.start()
	v := h.view()
	if !strings.Contains(v, "bad_root") || !strings.Contains(v, "Not an adj project.") {
		t.Fatalf("adj error not shown verbatim:\n%s", v)
	}
}

func TestNeedsYouListsDecisionsAndHeldItems(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0],
		[]adjcli.Record{openRec(11, "ui.slicer-tui", "Reuse slicer tui?")},
		[]slicercli.Item{item("S12", "Roadmap screen", "needs-owner"), item("S13", "Other", "later")}))
	h.start()
	v := h.view()
	for _, want := range []string{"Needs you", "Decisions", "#11", "ui.slicer-tui", "Reuse slicer tui?", "Held", "S12", "Roadmap screen"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "S13") {
		t.Errorf("an item without a hold flag is listed:\n%s", v)
	}
}

func TestBadgeCountsOpenRecordsPlusHeldItems(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0],
		[]adjcli.Record{openRec(1, "a.b", "q1"), openRec(2, "a.c", "q2")},
		[]slicercli.Item{item("S1", "t", "needs-owner"), item("S2", "t", "other-flag"), item("S3", "t")}))
	h.src.set(snap(four[1], nil, nil))
	h.start()
	first := h.view()
	if !strings.Contains(compactSpaces(first), "slicer 3") {
		t.Errorf("slicer tab should count 2 open + 1 held = 3: %q", first)
	}
	if strings.Contains(compactSpaces(first), "adjudicate 0") {
		t.Errorf("a zero badge is shown: %q", first)
	}
	if !strings.Contains(first, "Needs you") {
		t.Errorf("header total: %q", first)
	}
}

func TestRunningProjectIsMarkedWithAStar(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0], nil, nil, relay("run-1", "2026-10-19T16:00:00Z", "S01")))
	h.start()
	first := h.view()
	if !strings.Contains(compactSpaces(first), "slicer *") {
		t.Errorf("no running marker on the slicer tab: %q", first)
	}
	if strings.Contains(compactSpaces(first), "adjudicate*") {
		t.Errorf("marker on a project with no run: %q", first)
	}
}

func TestAFailedRunsCallKeepsTheRunningMarker(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0], nil, nil, relay("run-1", "2026-10-19T16:00:00Z", "S01")))
	h.start()
	failed := snap(four[0], nil, nil)
	failed.Runs = source.Result[[]slicercli.RelayRun]{Err: errors.New("slicer relay-run list: timed out")}
	h.src.set(failed)
	refreshSlicer(h)
	v := h.view()
	if first := v; !strings.Contains(compactSpaces(first), "slicer *") {
		t.Errorf("the running marker went out while the runs call failed: %q", first)
	}
	if !strings.Contains(v, "timed out") {
		t.Errorf("the runs error is not shown:\n%s", v)
	}
}

func withOpen(h *harness, n int) {
	var recs []adjcli.Record
	for i := 0; i < n; i++ {
		recs = append(recs, openRec(i+1, "k.k", "q"))
	}
	h.src.set(snap(four[0], recs, nil))
}

func refreshSlicer(h *harness) {
	h.advance(5 * time.Second)
	for _, msg := range exec(h.m.fetch("slicer", false)) {
		h.send(msg)
	}
}

func TestBellRingsOnceWhenTheCountRises(t *testing.T) {
	h := newHarness(t, four...)
	withOpen(h, 0)
	h.start()
	h.send(press("1"))
	if h.bells != 0 {
		t.Fatalf("bell at start: %d", h.bells)
	}
	withOpen(h, 2) // 0 -> 2
	refreshSlicer(h)
	if h.bells != 1 {
		t.Fatalf("bells after the count rose = %d, want 1", h.bells)
	}
	refreshSlicer(h) // 2 -> 2
	refreshSlicer(h)
	if h.bells != 1 {
		t.Fatalf("bells after unchanged refreshes = %d, want 1", h.bells)
	}
	withOpen(h, 1) // 2 -> 1: nothing new needs the owner
	refreshSlicer(h)
	if h.bells != 1 {
		t.Fatalf("bells after the count fell = %d, want 1", h.bells)
	}
	withOpen(h, 3) // 1 -> 3
	refreshSlicer(h)
	if h.bells != 2 {
		t.Fatalf("bells after a second rise = %d, want 2", h.bells)
	}
}

func TestNoBellForWhatWasAlreadyThereAtStart(t *testing.T) {
	h := newHarness(t, four...)
	withOpen(h, 3)
	h.start()
	if h.bells != 0 {
		t.Fatalf("bells = %d, want 0 for the first load", h.bells)
	}
}

func TestAFailedCallDoesNotChangeTheCountOrRingTheBell(t *testing.T) {
	h := newHarness(t, four...)
	withOpen(h, 2)
	h.start()
	h.send(press("1"))
	failed := snap(four[0], nil, nil)
	failed.Open = source.Result[[]adjcli.Record]{Err: errors.New("adj inbox: timed out")}
	h.src.set(failed)
	refreshSlicer(h)
	if first := h.view(); !strings.Contains(compactSpaces(first), "slicer 2") {
		t.Errorf("count dropped while adj failed: %q", first)
	}
	withOpen(h, 2) // adj answers again with the same two
	refreshSlicer(h)
	if h.bells != 0 {
		t.Fatalf("bells = %d: recovery from an error is not a new need", h.bells)
	}
}

func TestAFailedItemsCallKeepsTheHeldCount(t *testing.T) {
	h := newHarness(t, four...)
	held := []slicercli.Item{item("S1", "t", "needs-owner"), item("S2", "t", "needs-owner")}
	h.src.set(snap(four[0], nil, held))
	h.start()
	h.send(press("1"))
	failed := snap(four[0], nil, nil)
	failed.Items = source.Result[[]slicercli.Item]{Err: errors.New("slicer list: timed out")}
	h.src.set(failed)
	refreshSlicer(h)
	if first := h.view(); !strings.Contains(compactSpaces(first), "slicer 2") {
		t.Errorf("count dropped while slicer list failed: %q", first)
	}
	h.src.set(snap(four[0], nil, held))
	refreshSlicer(h)
	if h.bells != 0 {
		t.Fatalf("bells = %d: recovery from an error is not a new need", h.bells)
	}
}

func TestNoBellForNeedsThatWereThereWhenTheFirstReadingFailed(t *testing.T) {
	h := newHarness(t, four...)
	first := snap(four[0], nil, nil)
	first.Items = source.Result[[]slicercli.Item]{Err: errors.New("slicer list: timed out")}
	h.src.set(first)
	h.start()
	h.send(press("1"))
	h.src.set(snap(four[0], nil, []slicercli.Item{item("S1", "t", "needs-owner")})) // first good reading
	refreshSlicer(h)
	if h.bells != 0 {
		t.Fatalf("bells = %d: this need was there before ork could read it", h.bells)
	}
	h.src.set(snap(four[0], nil, []slicercli.Item{item("S1", "t", "needs-owner"), item("S2", "t", "needs-owner")}))
	refreshSlicer(h)
	if h.bells != 1 {
		t.Fatalf("bells = %d after a real rise, want 1", h.bells)
	}
	refreshSlicer(h)
	if h.bells != 1 {
		t.Fatalf("bells = %d after an unchanged refresh, want 1", h.bells)
	}
}

func TestNoBellForOpenRecordsThatWereThereWhenTheFirstReadingFailed(t *testing.T) {
	h := newHarness(t, four...)
	first := snap(four[0], nil, nil)
	first.Open = source.Result[[]adjcli.Record]{Err: errors.New("adj inbox: timed out")}
	h.src.set(first)
	h.start()
	h.send(press("1"))
	withOpen(h, 2) // first good reading of the inbox
	refreshSlicer(h)
	if h.bells != 0 {
		t.Fatalf("bells = %d", h.bells)
	}
	withOpen(h, 3)
	refreshSlicer(h)
	if h.bells != 1 {
		t.Fatalf("bells = %d after a real rise, want 1", h.bells)
	}
}

func TestARefreshInFlightMarksTheLoadedProject(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0], []adjcli.Record{openRec(11, "ui.slicer-tui", "A question")}, nil))
	h.start()
	h.send(press("1"))
	v := h.view()
	if strings.Contains(v, "~") {
		t.Fatalf("a mark with nothing in flight:\n%s", v)
	}
	held := h.sendHeld(press("r")) // slicer's refresh is out and has not returned
	v = h.view()
	if n := rowsWith(v, "A question"); n != 2 {
		t.Errorf("the project's row is not marked (%d):\n%s", n, v)
	}
	if first := v; !strings.Contains(compactSpaces(first), "slicer 1~") {
		t.Errorf("tab line: %q", first)
	}
	if !strings.Contains(v, "pending 1") {
		t.Errorf("status line lacks the word:\n%s", v)
	}
	for _, m := range held {
		h.send(m)
	}
	if v = h.view(); strings.Contains(v, "~") {
		t.Errorf("the mark stayed after the refresh returned:\n%s", v)
	}
}

func TestBellIsRungByACommand(t *testing.T) {
	h := newHarness(t, four...)
	withOpen(h, 0)
	h.start()
	h.send(press("1"))
	withOpen(h, 1)
	h.advance(5 * time.Second)
	_, cmd := h.m.Update(tickMsg{})
	for _, out := range exec(cmd) { // the fetch result arrives
		if s, ok := out.(snapshotMsg); ok {
			_, bell := h.m.Update(s)
			if h.bells != 0 {
				t.Fatal("Update rang the bell itself instead of returning a command")
			}
			exec(bell)
		}
	}
	if h.bells != 1 {
		t.Fatalf("bells = %d", h.bells)
	}
}

func TestExternalRunsOfferNoActions(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0], nil, nil, relay("run-20261019-004308-40d4", "2026-10-19T15:42:00Z", "S01", "S02")))
	h.start()
	v := h.view()
	for _, want := range []string{"Running", "run-20261019-004308-40d4", "S01", "S02", "active, 2 of 6 steps running", "slicer"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if id, el := strings.Index(v, "run-20261019-004308-40d4"), strings.Index(v, "1h00m"); id < 0 || el < id {
		t.Errorf("the run ID should come before the elapsed time:\n%s", v)
	}
	if !strings.Contains(v, "1h00m") {
		t.Errorf("elapsed time missing (begun 15:42, now 16:42):\n%s", v)
	}
	// ork never acts on them: no key does anything but move, refresh, help and quit.
	for _, k := range []string{"a", "s", "x", "d", "e", "l", "o", "enter", "space", "y", "n"} {
		h.src.take()
		if msgs := h.send(press(k)); len(msgs) != 0 || len(h.src.take()) != 0 {
			t.Errorf("key %q did something: %v", k, msgs)
		}
	}
}

// The row never holds the run ID, at any width (S145); the detail line holds
// the whole of it.
func TestRunIDIsInTheDetailNotTheRow(t *testing.T) {
	const id = "run-20261019-004308-40d4"
	for _, w := range []int{100, 80, 60} {
		h := newHarness(t, four...)
		h.src.set(snap(four[0], nil, nil, relay(id, "2026-10-19T15:42:00Z", "S01", "S02")))
		h.start()
		h.send(press("s"))
		h.send(tea.WindowSizeMsg{Width: w, Height: 30})
		sec := runningSection(plainView(h.m))
		if i := rowIndex(sec, "S01"); i < 0 || strings.Contains(sec[i], "run-") {
			t.Errorf("width %d: the run ID is in the row: %v", w, sec)
		}
		if got := detailText(h.m, w); !strings.Contains(got, id) {
			t.Errorf("width %d: the detail line lacks the whole run ID: %q", w, got)
		}
	}
}

func TestWrappedRowsHaveNoTrailingSpaces(t *testing.T) {
	h := newHarness(t, four...)
	// Many slices, so the row fills the pane and the detail line wraps.
	var items []string
	for i := 11; i < 41; i++ {
		items = append(items, fmt.Sprintf("S%d", i))
	}
	h.src.set(snap(four[0], nil, nil, relay("run-20261019-004308-40d4", "2026-10-19T15:42:00Z", items...)))
	h.start()
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	for i, l := range strings.Split(plainView(h.m), "\n") {
		if l != strings.TrimRight(l, " ") {
			t.Errorf("line %d ends in a space: %q", i, l)
		}
	}
}

func TestRunningSaysSoWhenNothingRuns(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	v := h.view()
	if !strings.Contains(v, "Running") || !strings.Contains(v, "nothing running") {
		t.Fatalf("empty Running section:\n%s", v)
	}
	if !strings.Contains(v, "nothing needs you") {
		t.Fatalf("empty Needs you section:\n%s", v)
	}
}

func TestReadyToLandPlaceholderIsGone(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	v := h.view()
	if !strings.Contains(v, "Ready to land") || strings.Contains(v, "comes with slice ") {
		t.Fatalf("Ready to land section:\n%s", v)
	}
}

func TestLayoutHasTabsViewBarBodyAndStatusLine(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	lines := strings.Split(plainView(h.m), "\n")
	if len(lines) != 30 {
		t.Fatalf("%d lines, want 30", len(lines))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > 100 {
			t.Errorf("line %d is %d wide: %q", i, w, l)
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "slicer") || !strings.Contains(lines[0], "?help") {
		t.Errorf("tab line: %q", lines[0])
	}
	for _, v := range []string{"Overview", "Questions", "Waves", "Roadmap", "Decisions"} {
		if !strings.Contains(lines[0], v) {
			t.Errorf("view bar lacks %q: %q", v, lines[1])
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "Needs you") {
		t.Errorf("tab line lacks the total: %q", lines[0])
	}
	status := lines[29]
	for _, want := range []string{"4 projects", "refreshed 16:42:00"} {
		if !strings.Contains(status, want) {
			t.Errorf("status line lacks %q: %q", want, status)
		}
	}
}

func TestViewFitsEverySize(t *testing.T) {
	for _, sz := range [][2]int{{100, 30}, {80, 24}, {60, 15}, {40, 10}, {20, 7}} {
		h := newHarness(t, four...)
		h.src.set(snap(four[0], []adjcli.Record{openRec(1, "a.b", "a rather long question title that cannot fit")}, nil,
			relay("run-20261019-004308-40d4", "2026-10-19T15:42:00Z", "S01")))
		h.start()
		h.send(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		for _, help := range []bool{false, true} {
			h.m.showHelp = help
			lines := strings.Split(plainView(h.m), "\n")
			if len(lines) != sz[1] {
				t.Errorf("%dx%d help=%v: %d lines", sz[0], sz[1], help, len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > sz[0] {
					t.Errorf("%dx%d help=%v: line %d is %d wide: %q", sz[0], sz[1], help, i, w, l)
				}
			}
		}
	}
}

func TestACutOverviewSaysHowMuchIsMissing(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(tea.WindowSizeMsg{Width: 80, Height: 10}) // 5 lines for the view
	v := plainView(h.m)
	if !strings.Contains(v, "more lines") {
		t.Fatalf("no cut marker:\n%s", v)
	}
	if strings.Contains(v, "Ready to land") {
		t.Errorf("the last section fit in 5 lines:\n%s", v)
	}
}

func TestStatusLineShowsTheLastRefreshTime(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(press("1"))
	h.advance(5 * time.Second)
	h.send(tickMsg{})
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	lines := strings.Split(plainView(h.m), "\n")
	if !strings.Contains(lines[29], "refreshed 16:42:05") {
		t.Errorf("status line: %q", lines[29])
	}
}

func TestUseMonoFollowsNOCOLOR(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if !UseMono(env(map[string]string{"NO_COLOR": "1"})) {
		t.Error("NO_COLOR=1 did not select monochrome")
	}
	if UseMono(env(map[string]string{})) {
		t.Error("monochrome without NO_COLOR")
	}
	if !UseMono(env(map[string]string{"NO_COLOR": "true"})) {
		t.Error("NO_COLOR=true did not select monochrome (any non-empty value does)")
	}
	if UseMono(env(map[string]string{"NO_COLOR": ""})) {
		t.Error("an empty NO_COLOR selected monochrome")
	}
}

// v1 set the alternate screen on the program (tea.WithAltScreen). v2 sets it
// on the view that each render returns.
func TestTheViewRunsOnTheAlternateScreen(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	if !h.m.View().AltScreen {
		t.Error("the view does not ask for the alternate screen")
	}
}

// The help view keeps whole key columns that fit the width and marks the rest
// with "...". The width comes from SetWidth, which v1 set as a field.
func TestHelpMarksTheColumnsThatDoNotFit(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.send(tea.WindowSizeMsg{Width: 40, Height: 30})
	h.send(press("?"))
	if v := plainView(h.m); !strings.Contains(v, "...") || !strings.Contains(v, "next project") {
		t.Errorf("a 40-column help view should keep the first column and mark the rest:\n%s", v)
	}
}

// S84 exercises layout independently of screen clipping and ANSI styles.
func layoutModel(t *testing.T) *Model {
	t.Helper()
	h := newHarness(t, proj("alpha", true), proj("beta", true), proj("unused-long-project", false))
	a := snap(h.m.projects[0].reg, []adjcli.Record{openRec(1, "short", "First title"), openRec(22, "longer.key", "Second title")}, []slicercli.Item{item("S9", "Held title", "needs-owner")})
	a.EngineRuns = []source.EngineRun{{ID: "stop-id", State: "stopped"}, {ID: "pause-id", State: "paused"}, {ID: "approve-id", State: "awaiting-approval"}, {ID: "removed-id", State: "stopped", Unavailable: true}}
	a.Items.Err = errors.New("items broke")
	h.src.set(a)
	h.src.set(snap(h.m.projects[1].reg, []adjcli.Record{openRec(3, "mid", "Third title")}, nil))
	h.start()
	// A first reply still pending belongs to Errors.
	h.m.projects[2].loaded = false
	return h.m
}

func TestNeedsYouGroupsByKind(t *testing.T) {
	m := layoutModel(t)
	body := sgr.ReplaceAllString(strings.Join(m.overview(100).body, "\n"), "")
	last := -1
	for _, heading := range []string{"Decisions 3", "Held 1", "Stopped 1", "Paused 1", "Approve 1", "Errors 3"} {
		at := strings.Index(body, heading)
		if at < 0 || at <= last {
			t.Errorf("missing or unordered group %q", heading)
		}
		last = at
	}
	needs, _ := m.rows()
	if len(needs) != 10 {
		t.Fatalf("got %d needs rows, want 10", len(needs))
	}
	want := []string{"?", "?", "?", "#", "x", "=", "+", "!", "!", "~"}
	for i, r := range needs {
		if i < len(want) && !strings.HasPrefix(r.kind, want[i]) {
			t.Errorf("cursor row %d kind %q, want %q", i, r.kind, want[i])
		}
	}
	for _, fragment := range []string{"?", "#", "x", "=", "+", "!", "~", "removed-id", "items broke", "waiting for the first reply"} {
		if !strings.Contains(body, fragment) {
			t.Errorf("missing %q", fragment)
		}
	}
	m.active = 1
	body = strings.Join(m.overview(100).body, "\n")
	if strings.Contains(body, "Held 0") || strings.Contains(body, "Errors") || strings.Contains(body, "Stopped") {
		t.Error("empty groups rendered")
	}
}

func TestSectionHeadingsCarryCounts(t *testing.T) {
	m := layoutModel(t)
	for i := 0; i < 5; i++ {
		m.projects[1].snap.Open.Value = append(m.projects[1].snap.Open.Value, openRec(30+i, "extra", "extra"))
	}
	body := strings.Join(m.overview(100).body, "\n")
	for _, want := range []string{"Needs you 15", "Running 1"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	m.projects[2].loaded = true
	if !strings.Contains(strings.Join(m.overview(100).body, "\n"), "Running 0") {
		t.Error("missing Running 0")
	}
}

func TestProjectColumnOnlyOnAllProjects(t *testing.T) {
	m := layoutModel(t)
	m.projects[2].loaded = true
	m.projects[2].snap.EngineRuns = []source.EngineRun{{ID: "running-id", Item: "SRun", State: "implement"}, {ID: "ready-id", Item: "SReady", State: "committed", Branch: "ready"}}
	body := sgr.ReplaceAllString(strings.Join(m.overview(140).body, "\n"), "")
	for _, id := range []string{"#1", "SRun", "SReady"} {
		wantName := "alpha"
		if id != "#1" {
			wantName = "unused-long-project"
		}
		if rowsWith(body, wantName, id) != 1 {
			t.Errorf("aggregate row %s lost project %s", id, wantName)
		}
	}
	if !strings.Contains(body, "alpha                #1") {
		t.Error("project width excludes projects with Running or Ready rows")
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.Contains(line, "SReady") && i+1 < len(lines) && strings.Index(lines[i+1], "slicer land") != strings.Index(line, "SReady") {
			t.Error("Ready command does not hang under text column")
		}
	}
	m.Update(press("r"))
	body = sgr.ReplaceAllString(strings.Join(m.overview(140).body, "\n"), "")
	if rowsWith(body, "~unused-long-project", "SRun") != 1 {
		t.Error("Running refresh mark missing")
	}
	for _, active := range []int{0, 2} {
		m.active = active
		body = sgr.ReplaceAllString(strings.Join(m.overview(140).body, "\n"), "")
		if strings.Contains(body, m.projects[active].reg.Name) {
			t.Error("selected project repeats in rows")
		}
	}
}

func TestDecisionTitlesAlign(t *testing.T) {
	m := layoutModel(t)
	body := sgr.ReplaceAllString(strings.Join(m.overview(100).body, "\n"), "")
	first, second := -1, -2
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "First title") {
			first = strings.Index(line, "First title")
		}
		if strings.Contains(line, "Second title") {
			second = strings.Index(line, "Second title")
		}
	}
	if first < 0 || first != second {
		t.Fatalf("title columns %d and %d differ", first, second)
	}
	m.active = 0
	m.projects[0].snap.Open.Value[0].Key = strings.Repeat("k", 80)
	found := false
	for _, line := range m.overview(60).body {
		if strings.Contains(line, "First title") {
			found = true
		}
		if strings.Contains(line, "First title") && (!strings.Contains(line, "...") || lipgloss.Width(line) > 60) {
			t.Error("long key was not capped and cut")
		}
	}
	if !found {
		t.Error("long key displaced the title")
	}
}

func TestHeldRowHasNoFlagSuffix(t *testing.T) {
	m := layoutModel(t)
	if strings.Contains(strings.Join(m.overview(100).body, "\n"), "(needs-owner)") {
		t.Error("redundant held flag suffix")
	}
}
func TestCursorBarFillsThePane(t *testing.T) {
	m := layoutModel(t)
	ov := m.overview(100)
	if got := lipgloss.Width(ov.body[ov.sel]); got != 100 {
		t.Fatalf("cursor width %d, want 100", got)
	}
}
func TestDecisionDetailShowsOptions(t *testing.T) {
	m := layoutModel(t)
	m.cfg.ASCII = true
	r := &m.projects[0].snap.Open.Value[0]
	r.Options = []adjcli.Option{{Label: "one"}, {Label: "two"}}
	r.Recommendation = &adjcli.Recommendation{Label: "two"}
	needs, _ := m.rows()
	for i, row := range needs {
		if strings.Contains(row.text, "First title") {
			m.cursor = i
			break
		}
	}
	detail := strings.Join(m.overview(70).detail, "\n")
	if !strings.Contains(detail, "#1 open | 2 options | recommends two") || !strings.Contains(detail, "First title") || strings.Contains(detail, "short") {
		t.Fatalf("decision detail lacks metadata: %s", detail)
	}
}
func TestSidebarHidesZeroCounts(t *testing.T) {
	m := layoutModel(t)
	m.projects[2].loaded = true
	lines := strings.Split(sgr.ReplaceAllString(strings.Join(m.sidebar(28, 24), "\n"), ""), "\n")
	needs, _ := m.rows()
	total := len(needs)
	for _, line := range lines {
		if strings.Contains(line, "unused-long-project") && strings.Contains(line, "0") {
			t.Error("zero badge shown")
		}
		if strings.Contains(line, "Needs you") && !strings.Contains(line, fmt.Sprint(total)) {
			t.Error("Needs you total missing")
		}
		if strings.Contains(line, "alpha") && strings.Index(line, fmt.Sprint(m.projects[0].needs())) != 25 {
			t.Error("badge not right aligned in digit column")
		}
	}
}
func TestRulesFallBackToASCII(t *testing.T) {
	m := layoutModel(t)
	for _, ascii := range []bool{false, true} {
		m.cfg.ASCII = ascii
		v := plainView(m)
		vertical, horizontal := "│", "─"
		if ascii {
			vertical, horizontal = "|", "-"
		}
		if !strings.Contains(v, vertical) || !strings.Contains(v, strings.Repeat(horizontal, 20)) {
			t.Errorf("ASCII=%v missing rule glyphs", ascii)
		}
		ov := m.overview(60)
		if len(ov.detail) > 0 {
			lines := strings.Split(v, "\n")
			if !strings.Contains(lines[len(lines)-len(ov.detail)-4], strings.Repeat(horizontal, 20)) {
				t.Errorf("ASCII=%v missing rule above detail", ascii)
			}
		}
	}
}
func TestStatusLineHidesPendingAtZero(t *testing.T) {
	m := layoutModel(t)
	if strings.Contains(m.statusLine(100), "pending") {
		t.Error("pending shown at zero")
	}
	m.Update(press("r"))
	if !strings.Contains(m.statusLine(100), "pending") {
		t.Error("nonzero pending hidden")
	}
}

func compactSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestLongDetailKeepsStatusLine(t *testing.T) {
	for _, kind := range []string{"decision", "pane", "overview-status", "roadmap-status", "questions-pane", "welcome-status"} {
		t.Run(kind, func(t *testing.T) {
			m := layoutModel(t)
			m.width, m.height = 80, 12
			long := strings.Repeat("long detail ", 200)
			switch kind {
			case "decision":
				m.projects[0].snap.Open.Value[0].Title = long
			case "pane", "questions-pane":
				h := paneHarness(t, "paused", "question")
				m = h.m
				m.width, m.height = 80, 12
				m.actionStatus = long
				if kind == "questions-pane" {
					m.view = questionsView
				}
			case "overview-status":
				m.actionStatus = long
			case "roadmap-status":
				m.active = 0
				m.view = roadmapView
				m.actionStatus = long
			case "welcome-status":
				m = newHarness(t).m
				m.width, m.height = 80, 12
				m.actionStatus = long
			}
			lines := strings.Split(plainView(m), "\n")
			if len(lines) != 12 || !strings.HasSuffix(lines[len(lines)-1], sgr.ReplaceAllString(m.statusLine(60), "")) {
				t.Errorf("last line is not status: %q", lines[len(lines)-1])
			}
		})
	}
}

func TestSidebarAggregateCountIncludesErrorsAndPending(t *testing.T) {
	m := layoutModel(t)
	needs, _ := m.rows()
	want := fmt.Sprint(len(needs))
	for _, active := range []int{-1, 0} {
		m.active = active
		line := sgr.ReplaceAllString(m.sidebar(28, 24)[2], "")
		if strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line[2:], "Needs you")), "│")) != want {
			t.Errorf("sidebar aggregate count differs from heading: %q, want %s", line, want)
		}
	}
}

func TestSidebarBadgeMarkerColumns(t *testing.T) {
	m := layoutModel(t)
	m.projects[2].loaded = true
	for _, refresh := range []bool{false, true} {
		m.projects[0].running = true
		if refresh {
			m.Update(press("r"))
		}
		lines := m.sidebar(28, 24)
		for _, line := range lines {
			plain := sgr.ReplaceAllString(line, "")
			if !strings.Contains(plain, "alpha") {
				continue
			}
			r := []rune(plain)
			mark := '*'
			if refresh {
				mark = '~'
			}
			if r[25] != []rune(fmt.Sprint(m.projects[0].needs()))[0] || r[26] != mark {
				t.Errorf("digits and marker not in reserved columns: %q", plain)
			}
		}
	}
}

func TestDecisionDetailOmitsEmptyOptionsAndUsesThemeSeparator(t *testing.T) {
	m := layoutModel(t)
	r := &m.projects[0].snap.Open.Value[0]
	r.Recommendation = &adjcli.Recommendation{Label: "one"}
	for _, ascii := range []bool{false, true} {
		m.cfg.ASCII = ascii
		detail := strings.Join(m.overview(100).detail, "\n")
		separator := "│"
		if ascii {
			separator = "|"
		}
		if strings.Contains(detail, "options") || !strings.Contains(detail, "#1 open "+separator+" recommends one") {
			t.Errorf("empty options or inconsistent separator: %q", detail)
		}
	}
}

func TestEmptyDetailHasNoBandBetweenRules(t *testing.T) {
	h := newHarness(t, proj("empty", false))
	h.start()
	h.m.width, h.m.height = 80, 12
	lines := strings.Split(plainView(h.m), "\n")
	rules := 0
	for _, line := range lines {
		if strings.Contains(line, strings.Repeat("─", 20)) {
			rules++
		}
	}
	if rules != 2 {
		t.Errorf("empty detail renders %d rules, want header and status only", rules)
	}
}

// numberedHarness is four with agents needing the owner and DataBall running,
// so the sidebar draws agents, DataBall, slicer, adjudicate: not registry order.
func numberedHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, four...)
	h.src.set(snap(four[3], []adjcli.Record{openRec(7, "a.q", "agents question")}, nil))
	h.src.set(snap(four[2], nil, nil, relay("run-1", "", "S1")))
	h.start()
	h.view()
	return h
}

// 1-9 select the first nine project rows in the order the sidebar draws them,
// 0 selects Needs you, and a number with no row does nothing (S85).
func TestNumberKeysFollowTheSidebar(t *testing.T) {
	h := numberedHarness(t)
	drawn := []string{"agents", "DataBall", "slicer", "adjudicate"}
	registry := map[string]int{"slicer": 0, "adjudicate": 1, "DataBall": 2, "agents": 3}
	v := h.view()
	top := strings.Join(strings.Split(v, "\n")[:8], "\n")
	for i, name := range drawn {
		line := fmt.Sprintf("%d %s", i+1, name)
		if sidebarLine(v, line) < 0 {
			t.Errorf("the sidebar has no row starting %q:\n%s", line, top)
		}
	}
	if sidebarLine(v, "  Running") < 0 || sidebarLine(v, "> Needs you") < 0 {
		t.Errorf("the Running entry must have no number, and Needs you is selected:\n%s", top)
	}
	for i, name := range drawn {
		h.send(press(string(rune('1' + i))))
		if got := h.m.active; got != registry[name] {
			t.Errorf("key %d selected registry project %d, want %s (%d)", i+1, got, name, registry[name])
		}
	}
	h.send(press("5"))
	if h.m.active != registry["adjudicate"] {
		t.Errorf("key 5 has no row and moved the selection to %d", h.m.active)
	}
	h.send(press("0"))
	if h.m.active != needsEntry {
		t.Errorf("key 0 selected %d, want Needs you", h.m.active)
	}
}

// The filter narrows the sidebar, and the numbers follow the rows that remain.
func TestNumberKeysFollowTheFilteredSidebar(t *testing.T) {
	h := numberedHarness(t)
	h.send(press("/"))
	h.send(press("a")) // keeps agents, DataBall and adjudicate; slicer has no "a"
	h.send(press("enter"))
	h.send(press("2"))
	if got := h.m.projects[h.m.active].reg.Name; got != "DataBall" {
		t.Errorf("key 2 selected %s, want DataBall", got)
	}
	h.send(press("3"))
	if got := h.m.projects[h.m.active].reg.Name; got != "adjudicate" {
		t.Errorf("key 3 selected %s, want adjudicate (slicer is filtered out)", got)
	}
}

// rowsHarness has two decisions and a ledger run on the first project: three
// rows on Needs you.
func rowsHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, four...)
	h.src.set(snap(four[0], []adjcli.Record{openRec(11, "a.one", "first question"), openRec(12, "a.two", "second question")}, nil,
		relay("run-1", "2026-10-19T16:00:00Z", "S01")))
	h.start()
	h.view()
	return h
}

// g and home go to the first row, G and end to the last, in each view that has
// a row cursor (S85).
func TestFirstAndLastRowKeys(t *testing.T) {
	home, end := tea.KeyPressMsg{Code: tea.KeyHome}, tea.KeyPressMsg{Code: tea.KeyEnd}
	t.Run("overview", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			first, last tea.KeyPressMsg
		}{{"letters", press("g"), press("G")}, {"home and end", home, end}} {
			h := rowsHarness(t)
			h.send(press("down"))
			h.send(tc.last)
			if got := detailText(h.m, 72); !strings.HasPrefix(got, "run-1  S01") || h.m.cursor != 2 {
				t.Errorf("%s: last row is %q, cursor %d", tc.name, got, h.m.cursor)
			}
			h.send(tc.last)
			if h.m.cursor != 2 {
				t.Errorf("%s: last twice moved the cursor to %d", tc.name, h.m.cursor)
			}
			h.send(tc.first)
			if got := detailText(h.m, 72); !strings.HasPrefix(got, "#11 open") || h.m.cursor != 0 {
				t.Errorf("%s: first row is %q, cursor %d", tc.name, got, h.m.cursor)
			}
			if h.m.active != needsEntry {
				t.Errorf("%s: the project changed to %d", tc.name, h.m.active)
			}
		}
	})
	t.Run("roadmap", func(t *testing.T) {
		h := roadmapHarness(t)
		h.send(press("G"))
		if h.m.roadmapCursor != 3 {
			t.Errorf("G put the roadmap cursor on %d, want 3", h.m.roadmapCursor)
		}
		h.send(press("g"))
		if h.m.roadmapCursor != 0 {
			t.Errorf("g put the roadmap cursor on %d, want 0", h.m.roadmapCursor)
		}
		h.send(end)
		h.send(home)
		if h.m.roadmapCursor != 0 {
			t.Errorf("end then home left the roadmap cursor on %d", h.m.roadmapCursor)
		}
	})
	t.Run("decisions", func(t *testing.T) {
		h := decisionsHarness(t)
		h.send(press("G"))
		if h.m.decisionsCursor != 1 {
			t.Errorf("G put the decisions cursor on %d, want 1", h.m.decisionsCursor)
		}
		h.send(press("g"))
		if h.m.decisionsCursor != 0 {
			t.Errorf("g put the decisions cursor on %d, want 0", h.m.decisionsCursor)
		}
	})
	t.Run("questions", func(t *testing.T) {
		h := questionsHarness(t)
		h.send(end)
		if want := len(h.m.selectableQuestions()) - 1; h.m.questionsCursor != want {
			t.Errorf("end put the questions cursor on %d, want %d", h.m.questionsCursor, want)
		}
		h.send(home)
		if h.m.questionsCursor != 0 {
			t.Errorf("home put the questions cursor on %d, want 0", h.m.questionsCursor)
		}
	})
	t.Run("waves", func(t *testing.T) {
		h, _ := wavesHarness(t)
		openWaves(t, h)
		h.send(press("G"))
		if want := len(h.m.waves.rows()) - 1; h.m.waves.cursor != want {
			t.Errorf("G put the waves cursor on %d, want %d", h.m.waves.cursor, want)
		}
		h.send(press("g"))
		if h.m.waves.cursor != 0 {
			t.Errorf("g put the waves cursor on %d, want 0", h.m.waves.cursor)
		}
	})
}

// manyRows is an 80x24 Overview of 40 decisions followed by one stopped run.
func manyRows(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, four...)
	var open []adjcli.Record
	for i := 1; i <= 40; i++ {
		open = append(open, openRec(i, fmt.Sprintf("a.k%02d", i), fmt.Sprintf("question %02d", i)))
	}
	s := snap(four[0], open, nil)
	s.EngineRuns = []source.EngineRun{{ID: "engine", Item: "S21", State: "stopped", Wait: "owner action"}}
	h.src.set(s)
	h.start()
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	return h
}

// With no pane open, pgdown and pgup move the cursor by the number of rows the
// body draws on screen, headings and the detail included, and stop at the first
// and last row. With a pane open they scroll the pane and leave the cursor.
func TestPageKeysMoveRowsWithoutAPane(t *testing.T) {
	pgdown, pgup := tea.KeyPressMsg{Code: tea.KeyPgDown}, tea.KeyPressMsg{Code: tea.KeyPgUp}
	h := manyRows(t)
	drawn := func() int { // the rows on screen: each names its project, the detail and headings do not
		n := 0
		for _, line := range strings.Split(plainView(h.m), "\n") {
			if _, main, ok := strings.Cut(line, "│"); ok && strings.Contains(main, "  slicer  ") {
				n++
			}
		}
		return n
	}
	first := drawn()
	if first <= 0 || first >= 24-6 {
		t.Fatalf("setup: %d rows drawn at 80x24; want fewer than the 18 body lines, the headings take some", first)
	}
	at := 0
	for len(h.m.cursorRows())-1 > at {
		step := drawn()
		h.send(pgdown)
		at = min(at+step, 40) // 41 rows: the last is index 40
		if h.m.cursor != at {
			t.Fatalf("pgdown with %d rows drawn put the cursor on %d, want %d", step, h.m.cursor, at)
		}
	}
	for at > 0 {
		step := drawn()
		h.send(pgup)
		at = max(at-step, 0)
		if h.m.cursor != at {
			t.Fatalf("pgup with %d rows drawn put the cursor on %d, want %d", step, h.m.cursor, at)
		}
	}
	h.send(pgup)
	if h.m.cursor != 0 || h.m.active != needsEntry {
		t.Errorf("pgup at the top: cursor %d, project %d; want row 0 and Needs you", h.m.cursor, h.m.active)
	}

	// On the last row (the stopped run) enter opens its pane, and the page keys scroll it.
	h.send(press("G"))
	h.send(press("enter"))
	if h.m.pane == nil {
		t.Fatal("enter did not open the stopped run's pane")
	}
	h.send(pgdown)
	if h.m.cursor != 40 || h.m.pane.offset != 1 {
		t.Errorf("pgdown with a pane: cursor %d, pane offset %d; want 40 and 1", h.m.cursor, h.m.pane.offset)
	}
	h.send(pgup)
	if h.m.cursor != 40 || h.m.pane.offset != 0 {
		t.Errorf("pgup with a pane: cursor %d, pane offset %d; want 40 and 0", h.m.cursor, h.m.pane.offset)
	}
	for _, k := range []string{"g", "G", "{", "}"} {
		h.send(press(k))
	}
	if h.m.cursor != 40 {
		t.Errorf("row keys moved the cursor to %d under an open pane", h.m.cursor)
	}

	// The Roadmap pages by the rows it draws: a short terminal draws fewer than
	// the four items, and a tall one pages to the last.
	road := roadmapHarness(t)
	road.send(tea.WindowSizeMsg{Width: 80, Height: 12})
	visible := rowsWith(plainView(road.m), "S0") + rowsWith(plainView(road.m), "S90")
	if visible <= 0 || visible >= 4 {
		t.Fatalf("setup: %d roadmap rows drawn at 80x12, want 1 to 3", visible)
	}
	road.send(pgdown)
	if road.m.roadmapCursor != visible {
		t.Errorf("pgdown put the roadmap cursor on %d with %d rows drawn", road.m.roadmapCursor, visible)
	}
	road.send(tea.WindowSizeMsg{Width: 80, Height: 40})
	road.send(pgdown)
	if road.m.roadmapCursor != 3 {
		t.Errorf("pgdown on a tall terminal put the roadmap cursor on %d, want the last row 3", road.m.roadmapCursor)
	}
}

// } moves to the first row of the next group and { to the first row of the
// current group, or of the previous one when the cursor is on a first row. The
// groups are the Needs you groups, then the Running, Recent and Ready sections.
func TestGroupJumpKeys(t *testing.T) {
	h := newHarness(t, four...)
	s := snap(four[0], []adjcli.Record{openRec(11, "a.one", "first question"), openRec(12, "a.two", "second question")},
		[]slicercli.Item{item("S05", "held thing", holdFlag)}, relay("run-1", "2026-10-19T16:00:00Z", "S01"))
	s.EngineRuns = []source.EngineRun{{ID: "engine", Item: "S21", State: "stopped", Wait: "owner action"}}
	h.src.set(s)
	h.start()
	h.view()
	// Rows: 0 #11, 1 #12 (Decisions), 2 S05 (Held), 3 engine (Stopped), 4 run-1 (Running).
	if n := len(h.m.cursorRows()); n != 5 {
		t.Fatalf("setup: %d rows, want 5", n)
	}
	for _, step := range []struct {
		key  string
		want int
	}{
		{"}", 2}, {"}", 3}, {"}", 4}, {"}", 4}, // the last group has no next
		{"{", 3}, {"{", 2}, {"{", 0}, {"{", 0}, // the first row has no previous group
	} {
		h.send(press(step.key))
		if h.m.cursor != step.want {
			t.Fatalf("%s put the cursor on %d, want %d", step.key, h.m.cursor, step.want)
		}
	}
	// From inside a group, { goes to that group's first row, then to the previous group's.
	h.send(press("down"))
	if h.m.cursor != 1 {
		t.Fatalf("down put the cursor on %d, want 1", h.m.cursor)
	}
	h.send(press("{"))
	if h.m.cursor != 0 {
		t.Errorf("{ inside Decisions put the cursor on %d, want its first row 0", h.m.cursor)
	}
	h.send(press("}"))
	h.send(press("}"))
	h.send(press("}"))
	h.send(press("}"))
	h.send(press("{")) // on Running's only row: its own first row, so the previous group
	if h.m.cursor != 3 {
		t.Errorf("{ on the first Running row put the cursor on %d, want 3", h.m.cursor)
	}

	// On the Running entry the groups are its live runs and Recent.
	t.Run("running entry", func(t *testing.T) {
		h := newHarness(t, four...)
		s := snap(four[0], nil, nil, relay("run-1", "2026-10-19T16:00:00Z", "S01"))
		s.BoardRuns.Value = []slicercli.RelayRun{{ID: "ended-1", Items: []string{"S97"}, State: "ended", Outcome: "pass",
			BeganAt: h.now.Add(-20 * time.Minute).Format(time.RFC3339), UpdatedAt: h.now.Add(-10 * time.Minute).Format(time.RFC3339)}}
		h.src.set(s)
		h.start()
		h.send(press("[")) // from Needs you to Running
		if h.m.active != runningEntry {
			t.Fatalf("[ selected %d, want the Running entry", h.m.active)
		}
		if n := len(h.m.cursorRows()); n != 2 {
			t.Fatalf("setup: %d rows, want one live run and one Recent row", n)
		}
		h.send(press("}"))
		if h.m.cursor != 1 {
			t.Errorf("} on the live run put the cursor on %d, want the first Recent row 1", h.m.cursor)
		}
		h.send(press("{"))
		if h.m.cursor != 0 {
			t.Errorf("{ on the first Recent row put the cursor on %d, want the live run 0", h.m.cursor)
		}
	})
}

// Keys that type stay with the field: g, G, 0, { and } typed into the filter or
// an owner's free answer go into the text and move nothing.
func TestNavigationKeysTypeIntoFields(t *testing.T) {
	typed := []string{"g", "G", "0", "{", "}", "1", "9"}
	h := rowsHarness(t)
	h.send(press("down"))
	h.send(press("/"))
	for _, k := range typed {
		h.send(press(k))
	}
	if h.m.filter != "gG0{}19" || h.m.cursor != 1 || h.m.active != needsEntry {
		t.Errorf("filter %q, cursor %d, active %d; want the text typed and nothing moved", h.m.filter, h.m.cursor, h.m.active)
	}

	p := paneHarness(t, "paused", "question")
	for _, k := range typed {
		p.deliver(press(k))
	}
	if got := p.m.pane.input.Value(); got != "gG0{}19" {
		t.Errorf("the answer field holds %q, want gG0{}19", got)
	}
}

// The help lists the number, first, last, page and group keys on four lines
// that fit the 44 column main area of a 64 column terminal uncut, and the page
// keys appear once: one entry covers paging rows and scrolling a pane.
func TestHelpListsTheRowKeys(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	help := strings.Split(strings.Join(h.m.helpLines(44, 40), "\n"), "\n")
	for _, want := range []string{"1-9 jump to project; 0 Needs you", "j/k down/up row; g/G home/end first/last", "pgdown/pgup page rows or scroll a pane", "}/{ next/previous group (Overview)"} {
		if !slices.Contains(help, want) {
			t.Errorf("the help has no line %q:\n%s", want, strings.Join(help, "\n"))
		}
	}
	for _, line := range help {
		if strings.Contains(line, "pgup") && strings.Contains(line, "pgdown") && !strings.HasPrefix(line, "pgdown/pgup page rows") {
			t.Errorf("a second page-key entry: %q", line)
		}
		if strings.Contains(line, "(rows)") {
			t.Errorf("the help still says (rows): %q", line)
		}
	}
}

// The launch list answers the number keys like every other screen: sidebar
// order, and 0 for Needs you (S85).
func TestLaunchListNumberKeysFollowTheSidebar(t *testing.T) {
	h, _ := launchHarness(t)
	// other needs the owner, so the sidebar draws it above ork: 1 other, 2 ork.
	h.m.apply(snapshotMsg{name: "other", snap: snap(proj("other", true), []adjcli.Record{openRec(1, "o.q", "other question")}, nil)})
	h.m.Update(press("l"))
	if h.m.launch == nil {
		t.Fatal("l did not open the launch list")
	}
	h.m.Update(press("1"))
	if h.m.active != 1 || h.m.launch != nil {
		t.Fatalf("key 1 in the launch list: active %d, launch open %v; want other (1) and the launch dropped", h.m.active, h.m.launch != nil)
	}
	h.m.Update(press("2"))
	h.m.Update(press("l"))
	h.m.Update(press("0"))
	if h.m.active != needsEntry {
		t.Fatalf("key 0 in the launch list selected %d, want Needs you", h.m.active)
	}
}

// A row key after a pending run jump (the snapshot of a launched run has not
// arrived yet) cancels the jump, as the other row keys do: the next snapshot
// leaves the cursor where the owner put it.
func TestRowKeysCancelAPendingRunJump(t *testing.T) {
	for _, k := range []string{"g", "G", "pgup", "pgdown", "}", "{"} {
		t.Run(k, func(t *testing.T) {
			h := newHarness(t, four...)
			s := snap(four[0], []adjcli.Record{openRec(11, "a.one", "first question"), openRec(12, "a.two", "second question")}, nil)
			s.EngineRuns = []source.EngineRun{
				{ID: "e1", Item: "S21", State: "stopped", Wait: "owner action"},
				{ID: "e2", Item: "S22", State: "paused", Wait: "owner action"},
			}
			h.src.set(s)
			h.start()
			h.view()
			if len(h.m.cursorRows()) != 4 {
				t.Fatalf("setup: %d rows, want 4", len(h.m.cursorRows()))
			}
			h.m.cursor = 2 // e1; every key below moves off it
			h.m.jumpRun = "e1"
			msg := press(k)
			switch k {
			case "pgup":
				msg = tea.KeyPressMsg{Code: tea.KeyPgUp}
			case "pgdown":
				msg = tea.KeyPressMsg{Code: tea.KeyPgDown}
			}
			h.send(msg)
			at := h.m.cursor
			if at == 2 {
				t.Fatalf("setup: %s did not move the cursor", k)
			}
			h.m.apply(snapshotMsg{name: "slicer", snap: s})
			if h.m.jumpRun != "" || h.m.cursor != at {
				t.Errorf("after %s the snapshot moved the cursor from %d to %d (jumpRun %q)", k, at, h.m.cursor, h.m.jumpRun)
			}
		})
	}
}
