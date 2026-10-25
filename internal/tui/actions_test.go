// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// actionLive changes the fake source just as a successful daemon call would.
// A wrong run ID fails instead of accidentally acting on the slice or ledger ID.
type actionLive struct {
	answerRequest daemon.RunAction
	answerNotice  string
	fakeLive
	src *fakeSource
	err error
}

func (l *actionLive) action(run, state string) error {
	if l.err != nil {
		return l.err
	}
	s := l.src.Snapshot(context.Background(), proj("ork", false))
	// The daemon and the TUI do not share memory across the wire.
	s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
	for i := range s.EngineRuns {
		if s.EngineRuns[i].ID == run {
			s.EngineRuns[i].State = state
			s.EngineRuns[i].Wait = ""
			l.src.set(s)
			return nil
		}
	}
	return fmt.Errorf("wrong engine run: %s", run)
}
func (l *actionLive) Approve(_ context.Context, run string) error { return l.action(run, "commit") }
func (l *actionLive) Stop(_ context.Context, run string) error    { return l.action(run, "stopped") }
func (l *actionLive) Retry(_ context.Context, run string) error   { return l.action(run, "implement") }

func actionHarness(t *testing.T, state, pending string) *harness {
	t.Helper()
	h := newHarness(t, proj("ork", false))
	s := engineSnapshot(t, state, "ledger")
	// Use JSON so the pre-implementation test compiles before pending exists.
	raw := fmt.Sprintf(`{"id":"engine","item":"S22","state":%q,"pending":%q,"relay_run":"ledger","wait":"owner action"}`, state, pending)
	var r source.EngineRun
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	s.EngineRuns = []source.EngineRun{r}
	h.src.set(s)
	h.start()
	h.m.cfg.Live = &actionLive{src: h.src}
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return h
}

func TestActionApproveImmediateAndRefresh(t *testing.T) {
	h := actionHarness(t, "awaiting-approval", "")
	h.deliver(press("a"))
	if v := h.view(); !strings.Contains(v, "S22  commit") || !strings.Contains(v, "ork approve engine") || strings.Contains(v, "[y/N]") {
		t.Fatalf("approve must run immediately and refresh the engine row:\n%s", v)
	}
}

func TestActionApproveWrongState(t *testing.T) {
	for _, state := range []string{"setup", "implement", "checks", "review", "gate", "commit", "finish", "paused", "stopped"} {
		t.Run(state, func(t *testing.T) {
			h := actionHarness(t, state, "")
			_, cmd := h.m.Update(press("a"))
			if v := h.view(); cmd != nil || !strings.Contains(v, "approve needs awaiting-approval; run is "+state) {
				t.Fatalf("invalid approve must explain its state without a call:\n%s", v)
			}
		})
	}
}

func TestActionConfirmCommand(t *testing.T) {
	for _, tc := range []struct{ key, verb, state, pending string }{{"x", "stop", "implement", ""}, {"t", "retry", "paused", "shape"}} {
		t.Run(tc.verb, func(t *testing.T) {
			h := actionHarness(t, tc.state, tc.pending)
			_, cmd := h.m.Update(press(tc.key))
			if v := h.view(); cmd != nil || !strings.Contains(v, "ork "+tc.verb+" engine? [y/N] (Enter is No)") {
				t.Fatalf("missing exact command and default-no confirmation:\n%s", v)
			}
		})
	}
}

func TestActionConfirmYesAndRefresh(t *testing.T) {
	for _, tc := range []struct{ key, verb, state, pending, want string }{{"x", "stop", "implement", "", "stopped"}, {"t", "retry", "paused", "shape", "implement"}} {
		t.Run(tc.verb, func(t *testing.T) {
			h := actionHarness(t, tc.state, tc.pending)
			h.deliver(press(tc.key))
			h.deliver(press("y"))
			if v := h.view(); !strings.Contains(v, "S22  "+tc.want) || strings.Contains(v, "[y/N]") {
				t.Fatalf("confirmed action must refresh its row:\n%s", v)
			}
		})
	}
}

func TestActionConfirmNo(t *testing.T) {
	for _, key := range []string{"x", "t"} {
		for _, answer := range []string{"enter", "n", "esc"} {
			t.Run(key+"/"+answer, func(t *testing.T) {
				h := actionHarness(t, "paused", "shape")
				h.deliver(press(key))
				h.deliver(press(answer))
				h.deliver(press("y")) // must not retain a confirmation after declining
				h.deliver(press("r"))
				if v := h.view(); !strings.Contains(v, "S22  paused") || strings.Contains(v, "[y/N]") || !strings.Contains(v, "Cancelled") {
					t.Fatalf("declined action must leave the run unchanged:\n%s", v)
				}
			})
		}
	}
}

func TestActionRetryQuestion(t *testing.T) {
	h := actionHarness(t, "paused", "question")
	_, cmd := h.m.Update(press("t"))
	if v := h.view(); cmd != nil || !strings.Contains(v, "answer with: ork answer engine TEXT") || strings.Contains(v, "[y/N]") {
		t.Fatalf("question must direct the owner to answer without retry:\n%s", v)
	}
}

func TestActionRetryOtherPendingKinds(t *testing.T) {
	for _, pending := range []string{"", "shape", "lost", "interrupted", "error"} {
		t.Run(pending, func(t *testing.T) {
			h := actionHarness(t, "paused", pending)
			h.deliver(press("t"))
			h.deliver(press("y"))
			if v := h.view(); !strings.Contains(v, "S22  implement") {
				t.Fatalf("non-question retry failed:\n%s", v)
			}
		})
	}
}

func TestActionRetryWrongState(t *testing.T) {
	h := actionHarness(t, "implement", "shape")
	_, cmd := h.m.Update(press("t"))
	if v := h.view(); cmd != nil || !strings.Contains(v, "retry needs paused; run is implement") {
		t.Fatalf("invalid retry must explain its state:\n%s", v)
	}
}

func TestActionErrorVerbatim(t *testing.T) {
	h := actionHarness(t, "awaiting-approval", "")
	h.m.cfg.Live.(*actionLive).err = &ipc.Error{Code: "run_busy", Message: "Wait: Owner's step failed."}
	h.deliver(press("a"))
	if v := h.view(); !strings.Contains(v, "run_busy: Wait: Owner's step failed.") || !strings.Contains(v, "S22  awaiting-approval") {
		t.Fatalf("failed action must preserve its code, message and row:\n%s", v)
	}
}

func TestActionExternalRows(t *testing.T) {
	for _, k := range []string{"a", "x", "t"} {
		t.Run(k, func(t *testing.T) {
			h := actionHarness(t, "implement", "")
			s := h.src.Snapshot(context.Background(), proj("ork", false))
			s.EngineRuns = nil
			s.Runs.Value = []slicercli.RelayRun{relayWithSteps(slicercli.Step{Role: "implementer", Phase: "start", Task: "Implement S30"})}
			h.m.apply(snapshotMsg{name: "ork", snap: s})
			_, cmd := h.m.Update(press(k))
			if v := h.view(); cmd != nil || !strings.Contains(v, "No engine run on this row; no actions") {
				t.Fatalf("external run offered an action:\n%s", v)
			}
		})
	}
}

func TestActionCursorTarget(t *testing.T) {
	h := actionHarness(t, "awaiting-approval", "")
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun{{ID: "first", Item: "S1", State: "awaiting-approval"}}, s.EngineRuns...)
	h.src.set(s)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.deliver(press("n"))
	h.deliver(press("a"))
	if v := h.view(); !strings.Contains(v, "S1  awaiting-approval") || !strings.Contains(v, "S22  commit") {
		t.Fatalf("action did not target cursor engine run:\n%s", v)
	}
}

func TestActionHelp(t *testing.T) {
	h := actionHarness(t, "implement", "")
	h.deliver(press("?"))
	for _, want := range []string{"a approve", "x stop", "t retry", "tab next view", "shift+tab previous view", "right next project", "left previous project", "down/j/n next row", "up/k/p previous row"} {
		if v := h.view(); !strings.Contains(v, want) {
			t.Errorf("missing help %q:\n%s", want, v)
		}
	}
}

func TestActionGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, tc := range []struct{ name, key, state, pending string }{{"x", "x", "implement", ""}, {"t", "t", "paused", "shape"}, {"help", "?", "implement", ""}} {
			name := fmt.Sprintf("action-%s-mono-%t.txt", tc.name, mono)
			t.Run(name, func(t *testing.T) {
				h := actionHarness(t, tc.state, tc.pending)
				h.m.cfg.Theme = NewTheme(mono)
				h.m.cfg.Version = "dev"
				h.deliver(press(tc.key))
				want := "ork stop engine?"
				if tc.key == "t" {
					want = "ork retry engine?"
				}
				if tc.key == "?" {
					want = "a approve"
				}
				if v := h.view(); !strings.Contains(v, want) {
					t.Fatalf("action view missing %q:\n%s", want, v)
				}
				checkGolden(t, name, h.m.View().Content)
			})
		}
	}
}

func TestActionDocs(t *testing.T) {
	b, err := os.ReadFile("../../docs/dashboard.md")
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); strings.Contains(s, "keys are not implemented in S22") || !strings.Contains(s, "`a` approves") || !strings.Contains(s, "`x` stops") || !strings.Contains(s, "`t` retries") {
		t.Fatal("dashboard still lacks implemented action keys")
	}
}

func TestActionDismissMessage(t *testing.T) {
	h := actionHarness(t, "implement", "")
	h.deliver(press("a"))
	h.deliver(press("esc"))
	v := h.view()
	lines := strings.Split(v, "\n")
	if !strings.Contains(strings.Join(lines[len(lines)-4:len(lines)-2], "\n"), "S22  implement  owner action  2 of 6 steps running  2m") {
		t.Fatalf("Escape must restore the cursor row detail:\n%s", v)
	}
}

// blockingActionLive holds the call open while the owner keeps using the UI.
type blockingActionLive struct {
	*actionLive
	started chan struct{}
	release chan struct{}
}

func (l *blockingActionLive) Retry(ctx context.Context, run string) error {
	close(l.started)
	<-l.release
	return l.actionLive.Retry(ctx, run)
}

func blockedRetry(t *testing.T) (*harness, func() tea.Msg) {
	t.Helper()
	h := actionHarness(t, "paused", "shape")
	l := &blockingActionLive{actionLive: h.m.cfg.Live.(*actionLive), started: make(chan struct{}), release: make(chan struct{})}
	h.m.cfg.Live = l
	h.deliver(press("t"))
	_, cmd := h.m.Update(press("y"))
	if cmd == nil {
		t.Fatal("confirmed retry returned no command")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	var once sync.Once
	var msg tea.Msg
	finish := func() tea.Msg {
		once.Do(func() { close(l.release); msg = <-result })
		return msg
	}
	t.Cleanup(func() { finish() })
	select {
	case <-l.started:
	case <-time.After(5 * time.Second):
		t.Fatal("retry did not start")
	}
	return h, finish
}

func TestActionBlockedRetry(t *testing.T) {
	t.Run("stop", func(t *testing.T) {
		h, _ := blockedRetry(t)
		h.deliver(press("x"))
		if v := h.view(); !strings.Contains(v, "ork stop engine? [y/N]") {
			t.Fatalf("stop must remain available during retry:\n%s", v)
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		h, _ := blockedRetry(t)
		h.deliver(press("t"))
		if v := h.view(); !strings.Contains(v, "ork retry engine is pending") {
			t.Fatalf("duplicate retry must explain the pending call:\n%s", v)
		}
	})
	t.Run("escape", func(t *testing.T) {
		h, _ := blockedRetry(t)
		h.deliver(press("esc"))
		if v := h.view(); strings.Contains(v, "ork retry engine") {
			t.Fatalf("Escape must hide the pending message:\n%s", v)
		}
	})
}

func TestActionOtherRunWhileRetryPending(t *testing.T) {
	for _, tc := range []struct{ key, state, want string }{
		{"a", "awaiting-approval", "S23  commit"},
		{"t", "paused", "ork retry other? [y/N]"},
		{"x", "implement", "ork stop other? [y/N]"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			h, _ := blockedRetry(t)
			s := h.src.Snapshot(context.Background(), proj("ork", false))
			s.EngineRuns = append([]source.EngineRun{{ID: "other", Item: "S23", State: tc.state}}, s.EngineRuns...)
			h.src.set(s)
			h.m.apply(snapshotMsg{name: "ork", snap: s})
			if tc.state == "implement" {
				h.deliver(press("n"))
			}
			h.deliver(press(tc.key))
			if v := h.view(); !strings.Contains(v, tc.want) {
				t.Fatalf("other run action must remain available:\n%s", v)
			}
		})
	}
}

func TestActionStopStopped(t *testing.T) {
	h := actionHarness(t, "stopped", "")
	_, cmd := h.m.Update(press("x"))
	if v := h.view(); cmd != nil || !strings.Contains(v, "run is already stopped; clear with: ork dismiss engine") || strings.Contains(v, "[y/N]") {
		t.Fatalf("stopped run must refuse stop with a reason:\n%s", v)
	}
}

func TestActionResultsKeepConcurrentStopPending(t *testing.T) {
	h, finishRetry := blockedRetry(t)
	h.deliver(press("x"))
	_, stop := h.m.Update(press("y"))
	if stop == nil {
		t.Fatal("stop must start while retry is pending")
	}
	h.deliver(finishRetry())
	if v := h.view(); !strings.Contains(v, "ork retry engine: done") {
		t.Fatalf("retry result must name the completed command:\n%s", v)
	}
	h.deliver(press("x"))
	if v := h.view(); !strings.Contains(v, "ork stop engine is pending") {
		t.Fatalf("retry result must keep concurrent stop pending:\n%s", v)
	}
	h.deliver(stop())
	if v := h.view(); !strings.Contains(v, "S22  stopped") || !strings.Contains(v, "ork stop engine: done") {
		t.Fatalf("stop result must refresh its row and name its command:\n%s", v)
	}
	h.deliver(press("x"))
	if v := h.view(); !strings.Contains(v, "run is already stopped; clear with: ork dismiss engine") {
		t.Fatalf("completed stop must clear only its pending call:\n%s", v)
	}
}

func TestActionErrorRefresh(t *testing.T) {
	h := actionHarness(t, "awaiting-approval", "")
	h.m.cfg.Live.(*actionLive).err = &ipc.Error{Code: "run_busy", Message: "Already advanced"}
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
	s.EngineRuns[0].State = "implement"
	h.src.set(s) // The daemon advanced before refusing this stale action.
	h.deliver(press("a"))
	if v := h.view(); !strings.Contains(v, "S22  implement") {
		t.Fatalf("failed action must refresh the row:\n%s", v)
	}
}

func TestActionConfirmKeepsOriginalRun(t *testing.T) {
	h := actionHarness(t, "implement", "")
	h.deliver(press("x"))
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun{{ID: "other", Item: "S23", State: "implement"}}, s.EngineRuns...)
	h.src.set(s)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.deliver(press("y"))
	if v := h.view(); !strings.Contains(v, "S22  stopped") || !strings.Contains(v, "S23  implement") {
		t.Fatalf("confirmation must stop its original run after rows change:\n%s", v)
	}
}
