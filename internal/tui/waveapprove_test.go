// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/source"
)

// S222 (*proposed*): the A key approves every waiting run of the selected
// run's wave, in start order, after one y/N.

// waveLive is a daemon connection that answers a wave approval with result.
type waveLive struct {
	actionLive
	calls  []string
	result daemon.WaveApproveResult
	err    error
}

func (l *waveLive) WaveApprove(_ context.Context, wave string) (daemon.WaveApproveResult, error) {
	l.calls = append(l.calls, wave)
	return l.result, l.err
}

// waveRun is the JSON of one engine run of wave-1. open says its one human
// check is still open.
func waveRun(id, item, state, created string, open bool) string {
	checkState := "ticked"
	if open {
		checkState = ""
	}
	card := fmt.Sprintf(`,"card":{"verdict":"PASS","human_checks":[{"text":"Read the card.","state":%q}]}`, checkState)
	if state != "awaiting-approval" {
		card = ""
	}
	return fmt.Sprintf(`{"id":%q,"item":%q,"state":%q,"wave":"wave-1","wait":"owner action","created_at":%q%s}`, id, item, state, created, card)
}

// waveHarness shows the runs of wave-1 on Needs you. S23 started before S22.
func waveHarness(t *testing.T, runs ...string) (*harness, *waveLive) {
	t.Helper()
	h := newHarness(t, proj("ork", false))
	s := engineSnapshot(t, "awaiting-approval", "ledger")
	var rows []source.EngineRun
	if err := json.Unmarshal([]byte("["+strings.Join(runs, ",")+"]"), &rows); err != nil {
		t.Fatal(err)
	}
	s.EngineRuns = rows
	h.src.set(s)
	h.start()
	live := &waveLive{actionLive: actionLive{src: h.src}}
	live.result = daemon.WaveApproveResult{Wave: "wave-1"}
	h.m.cfg.Live = live
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return h, live
}

func twoWaiting(t *testing.T) (*harness, *waveLive) {
	t.Helper()
	return waveHarness(t,
		waveRun("r-a", "S22", "awaiting-approval", "2026-10-19T16:40:00Z", false),
		waveRun("r-b", "S23", "awaiting-approval", "2026-10-19T16:30:00Z", false),
		waveRun("r-c", "S24", "implement", "2026-10-19T16:50:00Z", false))
}

const wavePrompt = "Approve and land S23, then S22? [y/N] (Enter is No)"

func TestWaveApproveKeyAsksFirstAndListsTheOrder(t *testing.T) {
	for _, where := range []string{"overview", "pane"} {
		t.Run(where, func(t *testing.T) {
			h, live := twoWaiting(t)
			if where == "pane" {
				h.deliver(press("enter"))
				if h.m.pane == nil {
					t.Fatal("setup: no pane")
				}
			}
			if v := h.view(); !strings.Contains(v, "A approve wave") {
				t.Errorf("the hint does not offer the key:\n%s", v)
			}
			h.deliver(press("A"))
			c := h.m.confirm
			if c == nil || !strings.HasPrefix(c.prompt(), "ork approve wave-1\n") || !strings.HasSuffix(c.prompt(), "\n"+wavePrompt) {
				t.Fatalf("A must ask first and name the items in start order: %+v", c)
			}
			if !strings.Contains(c.prompt(), "removed") {
				t.Errorf("the prompt does not say each landing cleans up: %q", c.prompt())
			}
			if v := h.view(); !strings.Contains(v, wavePrompt) {
				t.Errorf("the confirm is not on screen:\n%s", v)
			}
			if len(live.calls) != 0 {
				t.Fatalf("called before y: %q", live.calls)
			}
			// Enter is No.
			h.deliver(press("enter"))
			if h.m.confirm != nil || len(live.calls) != 0 || h.m.actionStatus != "Cancelled" {
				t.Fatalf("enter must cancel: confirm %v, calls %q, status %q", h.m.confirm, live.calls, h.m.actionStatus)
			}
			h.deliver(press("A"))
			h.deliver(press("y"))
			if len(live.calls) != 1 || live.calls[0] != "wave-1" {
				t.Fatalf("calls after y: %q", live.calls)
			}
		})
	}
}

func TestWaveApproveKeyIsRefusedWhileAnyCheckIsOpen(t *testing.T) {
	h, live := waveHarness(t,
		waveRun("r-a", "S22", "awaiting-approval", "2026-10-19T16:40:00Z", true),
		waveRun("r-b", "S23", "awaiting-approval", "2026-10-19T16:30:00Z", false))
	h.deliver(press("A"))
	if h.m.confirm != nil || len(live.calls) != 0 {
		t.Fatalf("confirm %v, calls %q", h.m.confirm, live.calls)
	}
	for _, want := range []string{"S22", "r-a", "open 1"} {
		if !strings.Contains(h.m.actionStatus, want) {
			t.Errorf("status %q does not name %q", h.m.actionStatus, want)
		}
	}
	if strings.Contains(h.m.actionStatus, "r-b") {
		t.Errorf("status names a run whose checks are marked: %q", h.m.actionStatus)
	}
	// Enter opens the selected row's card, which may be a run with no open check:
	// the text says whose card, and never promises the selected one.
	if strings.Contains(h.m.actionStatus, "(enter opens the card)") || !strings.HasSuffix(h.m.actionStatus, "(enter on a run's row opens its card)") {
		t.Errorf("status %q does not say which card enter opens", h.m.actionStatus)
	}
	if v := h.view(); strings.Contains(v, "A approve wave") {
		t.Errorf("the hint offers a key the row refuses:\n%s", v)
	}
}

func TestWaveApproveKeyIsHiddenWhenTheWaveHasOneWaitingRun(t *testing.T) {
	h, live := waveHarness(t,
		waveRun("r-a", "S22", "awaiting-approval", "2026-10-19T16:40:00Z", false),
		waveRun("r-c", "S24", "implement", "2026-10-19T16:50:00Z", false))
	h.deliver(press("A"))
	if h.m.confirm != nil || len(live.calls) != 0 || h.m.actionStatus != "" {
		t.Fatalf("A acted: confirm %v, calls %q, status %q", h.m.confirm, live.calls, h.m.actionStatus)
	}
	if v := h.view(); strings.Contains(v, "approve wave") || !strings.Contains(v, "a approve") {
		t.Errorf("the hint must offer a, not A:\n%s", v)
	}
	// The plain key still approves the one run.
	h.deliver(press("a"))
	if h.m.confirm == nil || h.m.confirm.verb != "approve" {
		t.Errorf("a did not ask to approve the run: %+v", h.m.confirm)
	}
}

// A run with no wave, or a wave of other projects' runs, offers no key.
func TestWaveApproveKeyNeedsTwoRunsOfTheSameWave(t *testing.T) {
	h, live := waveHarness(t,
		`{"id":"r-a","item":"S22","state":"awaiting-approval","created_at":"2026-10-19T16:40:00Z"}`,
		`{"id":"r-b","item":"S23","state":"awaiting-approval","created_at":"2026-10-19T16:30:00Z"}`)
	h.deliver(press("A"))
	if h.m.confirm != nil || len(live.calls) != 0 {
		t.Fatalf("two runs with no wave: confirm %v, calls %q", h.m.confirm, live.calls)
	}
}

func TestWaveApproveStatusReportsEachOutcome(t *testing.T) {
	item := func(name, outcome, state string, err *ipc.Error) daemon.WaveApproveItem {
		return daemon.WaveApproveItem{Item: name, Run: "r-" + name, Outcome: outcome, State: state, Error: err}
	}
	for _, tc := range []struct {
		name  string
		items []daemon.WaveApproveItem
		err   error
		want  string
	}{
		{"all landed", []daemon.WaveApproveItem{item("S23", "landed", "committed", nil), item("S22", "landed", "committed", nil)}, nil, "landed S23, S22"},
		{"a fix round stops the rest", []daemon.WaveApproveItem{
			item("A", "landed", "committed", nil), item("B", "failed", "awaiting-approval", nil), item("C", "not-attempted", "awaiting-approval", nil)},
			nil, "landed A; B needs a fix round; C not approved"},
		{"an error", []daemon.WaveApproveItem{
			item("A", "landed", "committed", nil), item("B", "failed", "awaiting-approval", &ipc.Error{Code: "internal", Message: "cannot write the note"}), item("C", "not-attempted", "awaiting-approval", nil)},
			nil, "landed A; B failed: internal: cannot write the note; C not approved"},
		{"a pause", []daemon.WaveApproveItem{item("A", "failed", "paused", nil)}, nil, "A is paused"},
		{"commit only and runs left alone", []daemon.WaveApproveItem{
			item("A", "committed", "committed", nil), item("B", "not-waiting", "implement", nil), item("C", "done", "committed", nil)},
			nil, "committed A"},
		{"two not approved", []daemon.WaveApproveItem{item("A", "failed", "awaiting-approval", nil), item("B", "not-attempted", "awaiting-approval", nil), item("C", "not-attempted", "awaiting-approval", nil)},
			nil, "A needs a fix round; B, C not approved"},
		{"refused", nil, &ipc.Error{Code: daemon.CodeRefused, Message: "human checks are open"}, "ork approve wave-1: human checks are open"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, live := twoWaiting(t)
			live.result.Items, live.err = tc.items, tc.err
			h.deliver(press("A"))
			h.deliver(press("y"))
			if len(live.calls) != 1 {
				t.Fatalf("calls %q", live.calls)
			}
			if h.m.actionStatus != tc.want {
				t.Errorf("status %q, want %q", h.m.actionStatus, tc.want)
			}
			if v := h.view(); !strings.Contains(v, tc.want) {
				t.Errorf("the status is not on screen:\n%s", v)
			}
		})
	}
}

// While the call runs the status says what is happening, and a second press
// is refused as pending.
func TestWaveApproveIsPendingWhileTheCallRuns(t *testing.T) {
	h, live := twoWaiting(t)
	h.deliver(press("A"))
	held := h.sendHeld(press("y"))
	if len(live.calls) != 1 || h.m.actionStatus != "approving wave-1: S23, S22: pending" {
		t.Fatalf("calls %q, status %q", live.calls, h.m.actionStatus)
	}
	h.deliver(press("A"))
	if h.m.confirm != nil || !strings.Contains(h.m.actionStatus, "ork approve wave-1 is pending") {
		t.Errorf("a second A: confirm %v, status %q", h.m.confirm, h.m.actionStatus)
	}
	// The flight is under the selected run; the wave's other run refuses too.
	h.m.actionStatus = ""
	h.deliver(press("down"))
	h.deliver(press("A"))
	if h.m.confirm != nil || !strings.Contains(h.m.actionStatus, "ork approve wave-1 is pending") {
		t.Errorf("A on the wave's other run: confirm %v, status %q", h.m.confirm, h.m.actionStatus)
	}
	// The plain key on the wave's other run would approve a run the wave call is
	// about to approve: it is refused too, and the hint does not offer it.
	h.m.actionStatus = ""
	h.deliver(press("a"))
	if h.m.confirm != nil || !strings.Contains(h.m.actionStatus, "ork approve wave-1 is pending") {
		t.Errorf("a on the wave's other run: confirm %v, status %q", h.m.confirm, h.m.actionStatus)
	}
	if v := h.view(); strings.Contains(v, "a approve") {
		t.Errorf("the hint offers a while the wave call runs:\n%s", v)
	}
	_ = held
}

// The confirm says what the call does to each run: a landing run is landed,
// pushed when the project pushes, and cleaned up; a run set to stop after its
// commit is committed on its branch and keeps its worktree. The wave stops when
// a run does not end committed.
func TestWaveApproveConfirmNamesPushAndCommitOnlyRuns(t *testing.T) {
	commitOnly := func(run string) string {
		return strings.Replace(run, `"wave":"wave-1"`, `"wave":"wave-1","stop_after":"commit"`, 1)
	}
	a := waveRun("r-a", "S22", "awaiting-approval", "2026-10-19T16:40:00Z", false)
	b := waveRun("r-b", "S23", "awaiting-approval", "2026-10-19T16:30:00Z", false)
	const stops = "does not end committed"
	for _, tc := range []struct {
		name        string
		push        bool
		runs        []string
		question    string
		has, hasNot []string
	}{
		{"push", true, []string{a, b}, "Approve and land and push S23, then S22? [y/N] (Enter is No)",
			[]string{"pushed", "removed", stops}, []string{"worktree stays"}},
		{"commit only first", false, []string{a, commitOnly(b)}, "Approve and commit S23 on its branch, then land S22? [y/N] (Enter is No)",
			[]string{"worktree stays", "removed", stops}, []string{"(commit only)", "pushed"}},
		{"commit only and push", true, []string{a, commitOnly(b)}, "Approve and commit S23 on its branch, then land and push S22? [y/N] (Enter is No)",
			[]string{"pushed", "worktree stays", stops}, nil},
		{"all commit only", false, []string{commitOnly(a), commitOnly(b)}, "Approve and commit S23 on its branch, then commit S22 on its branch? [y/N] (Enter is No)",
			[]string{"worktree stays", stops}, []string{"removed", "pushed", "lands", "land "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, live := waveHarness(t, tc.runs...)
			h.m.byName["ork"].reg.Push = tc.push
			h.deliver(press("A"))
			c := h.m.confirm
			if c == nil {
				t.Fatal("no confirm")
			}
			p := c.prompt()
			if !strings.HasSuffix(p, "\n"+tc.question) {
				t.Errorf("question: prompt %q does not end with %q", p, tc.question)
			}
			note := strings.TrimPrefix(strings.TrimSuffix(p, "\n"+tc.question), "ork approve wave-1\n")
			for _, want := range tc.has {
				if !strings.Contains(note, want) {
					t.Errorf("note %q does not say %q", note, want)
				}
			}
			for _, not := range tc.hasNot {
				if strings.Contains(p, not) {
					t.Errorf("prompt %q must not say %q", p, not)
				}
			}
			if len(live.calls) != 0 {
				t.Errorf("called before y: %q", live.calls)
			}
		})
	}
}

// On an approval card the wave key outlasts p report and the scroll keys and
// drops just before x stop (adj 390).
func TestWaveApproveHintOnTheCardRanksJustAfterStop(t *testing.T) {
	h, _ := twoWaiting(t)
	h.deliver(press("enter"))
	var gotA, gotStop, gotApprove *hintEntry
	for _, e := range h.m.contextHints() {
		e := e
		switch e.b.Help().Key {
		case "A":
			gotA = &e
		case "x":
			gotStop = &e
		case "a":
			gotApprove = &e
		}
	}
	if gotA == nil || gotStop == nil || gotApprove == nil {
		t.Fatalf("hints a %v, A %v, x %v", gotApprove, gotA, gotStop)
	}
	if gotA.drop != dropStopCard || gotStop.drop != dropStopCard || gotApprove.drop != dropApprCard {
		t.Errorf("drops: a %d, A %d, x %d; want %d, %d, %d", gotApprove.drop, gotA.drop, gotStop.drop, dropApprCard, dropStopCard, dropStopCard)
	}
	// A is listed after x stop, so on a tie it drops first, and before p report
	// and the scroll keys (a higher drop value goes sooner).
	idx := func(key string) int {
		for i, e := range h.m.contextHints() {
			if e.b.Help().Key == key {
				return i
			}
		}
		return -1
	}
	if idx("A") < idx("x") {
		t.Errorf("A is listed before x: A %d, x %d", idx("A"), idx("x"))
	}
}

// The help names A on the Overview, where it approves a wave, and not on the
// views where A is all projects or all waves.
func TestWaveApproveHelpNamesTheKeyOnTheOverview(t *testing.T) {
	h, _ := twoWaiting(t)
	h.deliver(press("?"))
	if v := h.view(); !strings.Contains(v, "A approve wave") {
		t.Errorf("the Overview help does not name A:\n%s", v)
	}
	h.m.view = decisionsView
	if v := h.view(); strings.Contains(v, "A approve wave") {
		t.Errorf("the Decisions help names A as approve wave:\n%s", v)
	}
}

func TestWaveApproveConfirmGoldens(t *testing.T) {
	for _, size := range []struct{ w, h int }{{100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			h, _ := twoWaiting(t)
			h.m.cfg.Theme = NewTheme(true)
			h.send(tea.WindowSizeMsg{Width: size.w, Height: size.h})
			h.send(press("A"))
			checkGolden(t, fmt.Sprintf("approve-wave-confirm-%dx%d-mono.txt", size.w, size.h), h.m.View().Content)
		})
	}
}
