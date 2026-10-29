// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// ledgerLive is the daemon's side of ending a ledger run. A successful end
// moves the run to ended in the fake source, as slicer would.
type ledgerLive struct {
	actionLive
	calls [][2]string // project, run
	err   error
}

func (l *ledgerLive) EndLedger(_ context.Context, project, run string) error {
	l.calls = append(l.calls, [2]string{project, run})
	if l.err != nil {
		return l.err
	}
	l.src.mu.Lock() // not Snapshot: the test counts the TUI's reads
	s := l.src.snaps["ork"]
	l.src.mu.Unlock()
	// Slicer lists an ended run with the board's runs, no longer with the active ones.
	var runs []slicercli.RelayRun
	for _, r := range s.Runs.Value {
		if r.ID != run {
			runs = append(runs, r)
			continue
		}
		r.State, r.Outcome = "ended", "stopped"
		s.BoardRuns.Value = append(s.BoardRuns.Value, r)
	}
	s.Runs.Value = runs
	l.src.set(s)
	return nil
}

// realRun is a ledger run id of the length slicer writes.
const realRun = "run-20261029-004308-40d4"

// ledgerEndHarness opens the pane of ledger run x1, which is in the given
// state, with a daemon that can end it.
func ledgerEndHarness(t *testing.T, state string) (*harness, *ledgerLive) {
	return ledgerEndHarnessFor(t, "x1", state)
}

// ledgerEndHarnessFor is ledgerEndHarness for a ledger run of the given id.
// The run last wrote 45 minutes ago, so a stale run is stale (slicer: no
// event for 30 minutes).
func ledgerEndHarnessFor(t *testing.T, id, state string) (*harness, *ledgerLive) {
	t.Helper()
	h := newHarness(t, proj("ork", false))
	r := followLedger(id, "S27", state, "", 2*time.Hour)
	r.UpdatedAt = t0.Add(-45 * time.Minute).Format(time.RFC3339)
	h.src.set(followSnap(nil, r))
	h.start()
	live := &ledgerLive{actionLive: actionLive{src: h.src}}
	h.m.cfg.Live = live
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.m.openPane(ovRow{ledgerID: id, name: "ork"})
	if !h.m.pane.ledger {
		t.Fatalf("setup: the pane is not the ledger run's: %+v", h.m.pane)
	}
	return h, live
}

func TestLedgerPaneOffersEndOnlyForAStaleRun(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {200, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			h, _ := ledgerEndHarness(t, "stale")
			h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			hint := hintLine(plainView(h.m))
			if !strings.Contains(hint, "x end run") || !strings.Contains(hint, "started elsewhere") || strings.Contains(hint, "read-only") {
				t.Errorf("a stale ledger pane's hint: %q, want x end run and the short note", hint)
			}
			for _, state := range []string{"active", "ended"} {
				h, live := ledgerEndHarness(t, state)
				h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				hint := hintLine(plainView(h.m))
				if !strings.Contains(hint, "started elsewhere, read-only") || strings.Contains(hint, "x ") {
					t.Errorf("%s pane hint %q: want the read-only note and no x", state, hint)
				}
				h.send(press("x"))
				if h.m.confirm != nil || h.m.actionStatus != "" || len(live.calls) != 0 {
					t.Errorf("%s pane: x did something: confirm %+v, status %q, calls %v", state, h.m.confirm, h.m.actionStatus, live.calls)
				}
			}
		})
	}
}

// The hint stops offering x while the end is in progress, as x stop does.
func TestLedgerPaneHidesEndWhileItRuns(t *testing.T) {
	h, _ := ledgerEndHarness(t, "stale")
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.send(press("x"))
	msgs := h.sendHeld(press("y"))
	if hint := hintLine(plainView(h.m)); strings.Contains(hint, "x end run") {
		t.Errorf("the hint offers x while the end runs: %q", hint)
	}
	for _, msg := range msgs {
		h.send(msg)
	}
}

func TestLedgerPaneEndAsksFirst(t *testing.T) {
	h, live := ledgerEndHarness(t, "stale")
	h.send(press("x"))
	want := "slicer relay-run end x1\n  --outcome stopped --close-steps fail\n" +
		"End this run as stopped and fail its unfinished steps? [y/N] (Enter is No)"
	if h.m.confirm == nil || h.m.confirm.prompt() != want {
		t.Fatalf("x must ask %q, got %+v", want, h.m.confirm)
	}
	if hint := hintLine(plainView(h.m)); !strings.Contains(hint, "y end run") || !strings.Contains(hint, "n cancel") {
		t.Errorf("the confirm hint: %q", hint)
	}
	for _, k := range []string{"n", "enter", "esc"} {
		h.send(press(k))
		if h.m.confirm != nil || h.m.actionStatus != "Cancelled" || len(live.calls) != 0 {
			t.Fatalf("%s: confirm %+v, status %q, calls %v", k, h.m.confirm, h.m.actionStatus, live.calls)
		}
		h.send(press("x"))
		if h.m.confirm == nil {
			t.Fatalf("%s: x did not ask again", k)
		}
	}
}

// The command breaks before its flags, never between a flag and its value, and
// the question and the keys stay whole, at the sizes the pane is used at.
func TestLedgerPaneEndPromptBreaksBetweenParts(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			h, _ := ledgerEndHarnessFor(t, realRun, "stale")
			h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			h.send(press("x"))
			var lines []string
			for _, l := range strings.Split(plainView(h.m), "\n") {
				if _, rest, ok := strings.Cut(l, "│"); ok {
					l = rest
				}
				lines = append(lines, strings.TrimSpace(l))
			}
			at := func(want string) int {
				for i, l := range lines {
					if strings.HasPrefix(l, want) {
						return i
					}
				}
				t.Fatalf("no line starts with %q:\n%s", want, strings.Join(lines, "\n"))
				return -1
			}
			cmd := at("slicer relay-run end " + realRun)
			if lines[cmd] != "slicer relay-run end "+realRun || lines[cmd+1] != "--outcome stopped --close-steps fail" {
				t.Errorf("command lines %q, %q", lines[cmd], lines[cmd+1])
			}
			q := at("End this run as stopped and fail its unfinished steps?")
			if q != cmd+2 {
				t.Errorf("the question is on line %d, want %d", q, cmd+2)
			}
			keys := "[y/N] (Enter is No)"
			if lines[q] != "End this run as stopped and fail its unfinished steps?" && lines[q] != "End this run as stopped and fail its unfinished steps? "+keys {
				t.Errorf("question line %q", lines[q])
			}
			if lines[q] != "End this run as stopped and fail its unfinished steps? "+keys && lines[q+1] != keys {
				t.Errorf("the keys are cut: %q, %q", lines[q], lines[q+1])
			}
		})
	}
}

func TestLedgerPaneEndCallsTheDaemonOnce(t *testing.T) {
	h, live := ledgerEndHarness(t, "stale")
	h.send(press("x"))
	msgs := h.sendHeld(press("y"))
	if want := [][2]string{{"ork", "x1"}}; !reflect.DeepEqual(live.calls, want) {
		t.Fatalf("calls %v, want %v", live.calls, want)
	}
	if !strings.Contains(h.m.actionStatus, "pending") {
		t.Errorf("status while the call runs: %q", h.m.actionStatus)
	}
	h.send(press("x"))
	if h.m.confirm != nil || !strings.Contains(h.m.actionStatus, "pending") {
		t.Errorf("a second x while the call runs: confirm %+v, status %q", h.m.confirm, h.m.actionStatus)
	}
	for _, msg := range msgs {
		h.send(msg)
	}
	if len(live.calls) != 1 {
		t.Fatalf("calls %v, want one", live.calls)
	}
}

func TestLedgerPaneEndStatusAndRefresh(t *testing.T) {
	h, live := ledgerEndHarness(t, "stale")
	h.src.take()
	h.send(press("x"))
	h.deliver(press("y"))
	if h.m.actionStatus != "ended ledger run x1" {
		t.Errorf("status %q, want ended ledger run x1", h.m.actionStatus)
	}
	if got := h.src.take(); !reflect.DeepEqual(got, []string{"ork"}) {
		t.Errorf("snapshot reads after the end: %v, want the project once", got)
	}
	var state string
	for _, r := range h.m.byName["ork"].snap.BoardRuns.Value {
		if r.ID == "x1" {
			state = r.State
		}
	}
	if state != "ended" {
		t.Errorf("the refreshed snapshot shows the run %q, want ended", state)
	}
	if len(live.calls) != 1 {
		t.Errorf("calls %v", live.calls)
	}
}

// A refusal also reads the project again, so a pane that is no longer stale
// stops offering x.
func TestLedgerPaneRefusalRefreshesThePane(t *testing.T) {
	h, live := ledgerEndHarness(t, "stale")
	live.err = &ipc.Error{Code: daemon.CodeRefused, Message: "ledger run x1 is active, not stale"}
	// A heartbeat arrived after the owner opened the pane.
	h.src.mu.Lock()
	s := h.src.snaps["ork"]
	h.src.mu.Unlock()
	runs := append([]slicercli.RelayRun(nil), s.Runs.Value...)
	runs[0].State = "active"
	s.Runs.Value = runs
	h.src.set(s)
	h.src.take()
	h.send(press("x"))
	h.deliver(press("y"))
	if h.m.actionStatus != "ledger run x1 is active, not stale" {
		t.Errorf("a refusal shows its text alone, got %q", h.m.actionStatus)
	}
	if got := h.src.take(); !reflect.DeepEqual(got, []string{"ork"}) {
		t.Errorf("snapshot reads after the refusal: %v, want the project once", got)
	}
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	if hint := hintLine(plainView(h.m)); strings.Contains(hint, "x end run") || !strings.Contains(hint, "read-only") {
		t.Errorf("the pane still offers x after the refusal: %q", hint)
	}
}

func TestDaemonEndLedgerWire(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			d, _, _, _ := requestServer(t, func(_ context.Context, method string, params json.RawMessage) (any, error) {
				if method != daemon.MethodRunEndLedger || string(params) != `{"project":"ork","run":"x1"}` {
					return nil, fmt.Errorf("wrong request: %s %s", method, params)
				}
				if fail {
					return nil, &ipc.Error{Code: daemon.CodeRefused, Message: "ledger run x1 is active, not stale"}
				}
				return daemon.LedgerEndReply{Run: "x1"}, nil
			})
			ender, ok := any(d).(ledgerEnder)
			if !ok {
				t.Fatal("the daemon connection lacks EndLedger")
			}
			err := ender.EndLedger(context.Background(), "ork", "x1")
			if !fail {
				if err != nil {
					t.Fatalf("EndLedger: %v", err)
				}
				return
			}
			if err == nil || actionError(err) != "ledger run x1 is active, not stale" {
				t.Fatalf("refusal came back as %v (%q)", err, actionError(err))
			}
		})
	}
}

func TestLedgerPaneStaleGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprint(mono), func(t *testing.T) {
			h, _ := ledgerEndHarnessFor(t, realRun, "stale")
			h.m.cfg.Theme = NewTheme(mono)
			h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
			h.send(press("x"))
			checkGolden(t, fmt.Sprintf("ledger-end-confirm-80x24-mono-%t.txt", mono), h.m.View().Content)
		})
	}
}
