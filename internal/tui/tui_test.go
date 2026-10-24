// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
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
		{"right", "slicer"},
		{"right", "adjudicate"},
		{"left", "slicer"},
		{"left", "Needs"},
		{"left", "agents"},
		{"right", "Needs"},
		{"3", "DataBall"},
		{"9", "DataBall"}, // no such tab
		{"0", "DataBall"},
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
			h.send(press("n"))
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
			if bar := strings.Split(v, "\n")[0]; !strings.Contains(bar, "Overview | Questions | Waves | Roadmap | Decisions") {
				t.Errorf("after %s the view bar is %q", k, bar)
			}
			want := "Waves"
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
	for _, want := range []string{"tab next view", "shift+tab previous view", "right next project", "left previous project", "jump to project", "down/j/n next row", "up/k/p previous row", "refresh now", "quit"} {
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
	h.send(press("right"))
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
	if n := rowsWith(v, "~ pending", "slicer"); n != 2 { // one in Needs you, one in Running
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
	if rowsWith(v, "~ pending", "slicer") != 0 || !strings.Contains(v, "A question") {
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
		if strings.Contains(line, "! error") && !strings.Contains(line, "DataBall") {
			t.Errorf("error is not on the project's row: %q", line)
		}
	}
	if got := detailText(h.m, 72); got != "items: no_such_item: No such item: NOPE." {
		t.Errorf("the items error is not on the detail line: %q", got)
	}
	h.send(press("n"))
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
		if strings.Contains(l, "! error") {
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
	if got := detailText(h.m, 72); got != "#11 a.one  first question" {
		t.Fatalf("the first row is not selected: %q", got)
	}
	if !reversed(h.m, "#11 a.one") || reversed(h.m, "#12 a.two") {
		t.Errorf("the cursor row is not the reversed one")
	}
	h.send(press("n"))
	if got := detailText(h.m, 72); got != "#12 a.two  second question" {
		t.Fatalf("n did not move to the second row: %q", got)
	}
	if reversed(h.m, "#11 a.one") || !reversed(h.m, "#12 a.two") {
		t.Errorf("the reverse does not follow the cursor")
	}
	h.send(press("n"))
	if got := detailText(h.m, 72); !strings.HasPrefix(got, "run-1  S01") {
		t.Fatalf("n did not move to the run row: %q", got)
	}
	h.send(press("n"))
	if h.m.cursor != 2 {
		t.Errorf("the cursor went past the last row: stored %d, want 2", h.m.cursor)
	}
	if got := detailText(h.m, 72); !strings.HasPrefix(got, "run-1  S01") {
		t.Errorf("the cursor left the last row: %q", got)
	}
	h.send(press("p"))
	if got := detailText(h.m, 72); !strings.HasPrefix(got, "#12 a.two") {
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
	h.send(press("n"))
	if h.m.cursor != 1 {
		t.Fatalf("cursor = %d after n, want 1", h.m.cursor)
	}
	h.send(press("right")) // to slicer
	if h.m.active != 0 || h.m.cursor != 0 {
		t.Errorf("right: active %d, cursor %d; want slicer's first row", h.m.active, h.m.cursor)
	}
	h.send(press("left")) // back to Needs you
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
				{tc.next, "#12 a.two"}, {tc.next, "run-1  S01"}, {tc.next, "run-1  S01"},
				{tc.prev, "#12 a.two"}, {tc.prev, "#11 a.one"}, {tc.prev, "#11 a.one"},
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
		{"up", "Needs", "#11 a.one"},
		{"k", "Needs", "#11 a.one"},
		{"n", "Needs", "run-1  S01"},
		{"p", "Needs", "#11 a.one"},
		{"tab", "Needs", "#12 a.two"},
		{"shift+tab", "Needs", "#12 a.two"},
		{"right", "slicer", "#11 a.one"},
		{"2", "adjudicate", ""},
		{"left", "agents", ""},
		{"9", "Needs", "#12 a.two"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			h := newHarness(t, four...)
			h.src.set(snap(four[0], []adjcli.Record{openRec(11, "a.one", "first question"), openRec(12, "a.two", "second question")}, nil,
				relay("run-1", "2026-10-19T16:00:00Z", "S01")))
			h.start()
			h.m.cursor = 1 // setup without a row key, so each key is tested alone
			if got := detailText(h.m, 72); !strings.HasPrefix(got, "#12 a.two") {
				t.Fatalf("setup: the cursor row is %q, want #12 a.two", got)
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
	if got := detailText(h.m, 72); !strings.HasPrefix(got, "#3 owner.choice") {
		t.Fatalf("the first row is %q, want the decision", got)
	}
	h.send(press("down"))
	if got := detailText(h.m, 72); !strings.HasPrefix(got, "engine  S21  stopped") {
		t.Fatalf("down did not move the cursor to the stopped run: %q", got)
	}
	h.send(press("enter"))
	if v := h.view(); !strings.Contains(v, "Run engine  S21  stopped") || !strings.Contains(v, "Stopped; relaunch, or clear with: ork dismiss engine") {
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
		"Outside Decisions, keys `right`/`left` move selection and wrap through \"Needs you\" and the sorted projects.",
		"`1`-`9` select projects in registry order.",
		"`tab` and `shift+tab` switch views",
		"`space` ticks or unticks the selected item",
		"`e` edits the selected ticked item",
		"`l` launches the ticked items with one y/n confirmation",
		"`down`/`j` and `up`/`k` move it to the next and previous row, and so do `n` and `p`",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("dashboard.md lacks %q", want)
		}
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
	if got := detailText(h.m, 72); got != "#3 output.shape  Which JSON shape?" {
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
	if got := detailText(h.m, 60); got != "#11 ui.slicer-tui  "+long {
		t.Errorf("the detail line is not the full text:\n got %q", got)
	}
	v := plainView(h.m)
	if rowsWith(v, "? decision", "#11") != 1 {
		t.Errorf("the row is not one line with its kind:\n%s", v)
	}
}

// A run's ID stays whole when its row is cut: the rest of the row goes first.
func TestRunRowCutsItsRestBeforeItsID(t *testing.T) {
	const id = "run-20261019-004308-40d4"
	h := newHarness(t, four...)
	h.src.set(snap(four[0], nil, nil, relay(id, "2026-10-19T15:42:00Z", "S01", "S02")))
	h.start()
	h.send(press("s"))
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	v := plainView(h.m)
	row := ""
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "started elsewhere") {
			row = strings.TrimRight(l, " ")
		}
	}
	if !strings.Contains(row, id) {
		t.Errorf("the run ID is cut from its row: %q", row)
	}
	if !strings.HasSuffix(row, "...") {
		t.Errorf("the rest of the run row is not cut with an ellipsis: %q", row)
	}
	if strings.Contains(row, "1h00m") {
		t.Errorf("the elapsed time, cut last, is on the row: %q", row)
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
		h.send(press("n"))
	}
	v := plainView(h.m)
	if rowsWith(v, "? decision", "question 15") != 1 {
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
	row := sidebarRow(h.m, "  DataBall")
	if plain := sgr.ReplaceAllString(row, ""); !strings.Contains(plain, "DataBall no slicer") {
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
	if v := runs.view(); !strings.Contains(v, "DataBall no slicer") {
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
	if v := h.view(); !strings.Contains(v, "DataBall no slicer") || strings.Contains(v, "DataBall 3") {
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
	h.send(press("3")) // DataBall, in registry order
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
	if n := rowsWith(v, "! error", "locked"); n != 1 {
		t.Errorf("%d rows for the code, want 1:\n%s", n, v)
	}
	if running := strings.Index(v, "Running"); merged < strings.Index(v, "Needs you") || merged > running {
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
	for _, want := range []string{"Needs you", "decision", "#11", "ui.slicer-tui", "Reuse slicer tui?", "held", "S12", "Roadmap screen", "needs-owner"} {
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
	if !strings.Contains(first, "slicer 3") {
		t.Errorf("slicer tab should count 2 open + 1 held = 3: %q", first)
	}
	if !strings.Contains(first, "adjudicate 0") {
		t.Errorf("a zero badge is missing: %q", first)
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
	if !strings.Contains(first, "slicer 0*") && !strings.Contains(first, "slicer 0*") {
		t.Errorf("no running marker on the slicer tab: %q", first)
	}
	if strings.Contains(first, "adjudicate*") {
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
	if first := v; !strings.Contains(first, "slicer 0*") {
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
	if first := h.view(); !strings.Contains(first, "slicer 2") {
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
	if first := h.view(); !strings.Contains(first, "slicer 2") {
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
	if n := rowsWith(v, "A question", "~slicer"); n != 1 {
		t.Errorf("the project's row is not marked (%d):\n%s", n, v)
	}
	if first := v; !strings.Contains(first, "slicer 1~") {
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

func TestExternalRunsShowStartedElsewhereAndOfferNoActions(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0], nil, nil, relay("run-20261019-004308-40d4", "2026-10-19T15:42:00Z", "S01", "S02")))
	h.start()
	v := h.view()
	for _, want := range []string{"Running", "run-20261019-004308-40d4", "S01", "S02", "started elsewhere", "active, 2 of 6 steps running", "slicer"} {
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

func TestRunIDIsNeverClipped(t *testing.T) {
	const id = "run-20261019-004308-40d4"
	for _, w := range []int{100, 80, 60} {
		h := newHarness(t, four...)
		h.src.set(snap(four[0], nil, nil, relay(id, "2026-10-19T15:42:00Z", "S01", "S02")))
		h.start()
		h.send(press("s"))
		h.send(tea.WindowSizeMsg{Width: w, Height: 30})
		// The row itself must hold the whole ID. The detail line holds it too,
		// so the row is found by its kind, not by the ID.
		v := plainView(h.m)
		row := ""
		for _, l := range strings.Split(v, "\n") {
			if strings.Contains(l, "started elsewhere") {
				row = l
			}
		}
		if !strings.Contains(row, id) {
			t.Errorf("width %d: the run ID is cut from its row: %q\n%s", w, row, v)
		}
	}
}

func TestWrappedRowsHaveNoTrailingSpaces(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0], nil, nil, relay("run-20261019-004308-40d4", "2026-10-19T15:42:00Z", "S11")))
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
	if !strings.Contains(strings.Join(lines, "\n"), "slicer 0") || !strings.Contains(lines[0], "?help") {
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
	for _, want := range []string{"4 projects", "refreshed 16:42:00", "pending 0"} {
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
