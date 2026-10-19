// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"errors"
	"reflect"
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
		Source:    h.src,
		Projects:  projects,
		Now:       func() time.Time { return h.now },
		Schedule:  poll.DefaultSchedule,
		TickEvery: time.Second,
		After:     func(time.Duration) tea.Cmd { return func() tea.Msg { return tickMsg{} } },
		Bell:      func() { h.bells++ },
		Theme:     plainTheme(),
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
		{"tab", "slicer"},
		{"right", "adjudicate"},
		{"shift+tab", "slicer"},
		{"left", "Needs"},
		{"left", "agents"},
		{"tab", "Needs"},
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
	for _, want := range []string{"next project", "jump to project", "refresh now", "quit"} {
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
	h.send(press("tab"))
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

func TestFailedCallShowsItsCodeAndMessageVerbatim(t *testing.T) {
	h := newHarness(t, four...)
	bad := snap(four[2], nil, nil)
	bad.Items = source.Result[[]slicercli.Item]{Err: &clirun.ToolError{Code: "no_such_item", Message: "No such item: NOPE.", Command: "slicer show NOPE"}}
	bad.Runs = source.Result[[]slicercli.RelayRun]{Err: errors.New("slicer relay-run list: exit 3: weird  spacing, 100% \"quoted\"")}
	h.src.set(bad)
	h.start()
	v := h.view()
	for _, want := range []string{"no_such_item", "No such item: NOPE.", "weird  spacing, 100% \"quoted\"", "DataBall"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "no_such_item") && !strings.Contains(line, "DataBall") {
			t.Errorf("error is not on the project's row: %q", line)
		}
	}
}

func TestALongErrorIsWrappedNotCut(t *testing.T) {
	h := newHarness(t, four...)
	bad := snap(four[2], nil, nil)
	msg := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa quebec romeo"
	bad.Items = source.Result[[]slicercli.Item]{Err: &clirun.ToolError{Code: "too_long", Message: msg}}
	h.src.set(bad)
	h.start()
	h.send(press("s"))
	h.send(tea.WindowSizeMsg{Width: 60, Height: 30})
	lines := strings.Split(plainView(h.m), "\n")
	var rows []string
	for _, l := range lines {
		if strings.Contains(l, "too_long") || strings.Contains(l, "papa") || strings.Contains(l, "romeo") {
			rows = append(rows, strings.TrimSpace(l))
		}
	}
	if len(rows) < 2 {
		t.Fatalf("the error did not wrap:\n%s", strings.Join(lines, "\n"))
	}
	got := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	if !strings.Contains(got, "too_long: "+msg) {
		t.Fatalf("the error text was cut:\n%s", strings.Join(lines, "\n"))
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
		if v := plainView(h.m); !strings.Contains(v, id) {
			t.Errorf("width %d: the run ID is cut:\n%s", w, v)
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

func TestReadyToLandSaysItWaitsForS09(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	v := h.view()
	if !strings.Contains(v, "Ready to land") || !strings.Contains(v, "S09") {
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
	for _, v := range []string{"Overview", "Waves", "Roadmap", "Decisions"} {
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
