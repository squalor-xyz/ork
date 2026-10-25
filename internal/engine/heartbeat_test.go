// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const testHeartbeatEvery = 50 * time.Millisecond

func heartbeatEngine(t *testing.T, opts ...func(*harnessOpts)) (*harness, *Engine) {
	t.Helper()
	h := newHarness(t, opts...)
	h.deps.HeartbeatEvery = testHeartbeatEvery
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.run.Update(func(r *runrec.Record) { r.RelayRun = "run-1" }); err != nil {
		t.Fatal(err)
	}
	return h, e
}

func runHeartbeatRound(t *testing.T, e *Engine) {
	t.Helper()
	out, err := e.runRound(context.Background(), round{role: RoleImplementer, n: 1, state: runrec.StateImplement, task: "S04 implementer"}, nil)
	if err != nil || !out.ok {
		t.Fatalf("round: ok=%v err=%v reason=%s", out.ok, err, e.Snapshot().Reason)
	}
}

func heartbeats(h *harness) []stepCall {
	var out []stepCall
	for _, s := range h.steps() {
		if s.Step != "" && s.Phase == "start" && s.Task == "" {
			out = append(out, s)
		}
	}
	return out
}

func waitHeartbeat(h *harness) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(heartbeats(h)) > 0 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestHeartbeatSendsStepStartWithoutTask(t *testing.T) {
	h, e := heartbeatEngine(t)
	var during bool
	h.runner.script = func(Request, int) (Result, error) { during = waitHeartbeat(h); return Result{}, nil }
	runHeartbeatRound(t, e)
	if !during {
		t.Fatal("no heartbeat while role was running")
	}
	for _, s := range heartbeats(h) {
		if s.Step != e.Snapshot().Roles[0].Step {
			t.Fatalf("heartbeat step %q, want role step", s.Step)
		}
	}
	for _, args := range h.calls("slicer", "relay-run", "step") {
		if strings.Contains(join(args), "--step") && strings.Contains(join(args), "--phase start") {
			for _, arg := range args {
				if arg == "--task" {
					t.Fatal("heartbeat includes --task")
				}
			}
		}
	}
}

// Hold the terminal call open so a missing explicit stop cannot hide behind
// the deferred stop when runRound returns.
type heartbeatEndSink struct{ runrec.Sink }

func (s heartbeatEndSink) AddWrite(w runrec.Write) error {
	if strings.Contains(join(w.Argv), "--phase done") || strings.Contains(join(w.Argv), "--phase fail") {
		time.Sleep(4 * testHeartbeatEvery)
	}
	return s.Sink.AddWrite(w)
}

func TestHeartbeatStopsAtStepEnd(t *testing.T) {
	for _, exit := range []ExitClass{ExitOK, ExitError, ExitRateLimited} {
		t.Run(string(exit), func(t *testing.T) {
			h, e := heartbeatEngine(t)
			if exit == ExitRateLimited {
				if err := e.run.Update(func(r *runrec.Record) {
					r.Lineup[RoleImplementer] = append(r.Lineup[RoleImplementer], runrec.LineupEntry{Vendor: "codex", Model: "test", Effort: "high"})
				}); err != nil {
					t.Fatal(err)
				}
			}
			e.wr.Sink = heartbeatEndSink{e.run}
			h.runner.script = func(Request, int) (Result, error) {
				time.Sleep(3 * testHeartbeatEvery)
				if exit != ExitOK {
					return Result{Exit: exit}, nil
				}
				return Result{}, nil
			}
			_, err := e.runRound(context.Background(), round{role: RoleImplementer, n: 1, state: runrec.StateImplement, task: "S04 implementer"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(2 * testHeartbeatEvery)
			ended := false
			for _, s := range h.steps() {
				if s.Phase == "done" || s.Phase == "fail" {
					ended = true
				}
				if ended && s.Phase == "start" && s.Step != "" {
					t.Fatal("heartbeat sent after stepEnd")
				}
			}
			if !ended {
				t.Fatal("no stepEnd observed")
			}
		})
	}
}

func TestShortRoundSendsNoHeartbeat(t *testing.T) {
	h, e := heartbeatEngine(t)
	e.heartbeatEvery = time.Hour
	h.runner.script = func(Request, int) (Result, error) { time.Sleep(testHeartbeatEvery); return Result{}, nil }
	runHeartbeatRound(t, e)
	if got := heartbeats(h); len(got) != 0 {
		t.Fatalf("short round sent %d heartbeats", len(got))
	}
}

func TestFailedHeartbeatDoesNotStopRole(t *testing.T) {
	h, e := heartbeatEngine(t, func(o *harnessOpts) {
		o.routes = []clitest.Route{{Tool: "slicer", Args: []string{"relay-run", "step", "run-1", "--step", "1", "--phase", "start"}, Prefix: true, Exit: 1, Stderr: `{"error":{"code":"io","message":"heartbeat failed"}}`}}
	})
	var during bool
	h.runner.script = func(Request, int) (Result, error) { during = waitHeartbeat(h); return Result{}, nil }
	runHeartbeatRound(t, e)
	if !during {
		t.Fatal("no failed heartbeat attempted while role was running")
	}
	failed := false
	for _, w := range e.Snapshot().Writes {
		if w.Exit != 0 && strings.Contains(join(w.Argv), "--phase start") {
			failed = true
		}
	}
	if !failed {
		t.Fatal("failed heartbeat was not recorded")
	}
	if e.Snapshot().Roles[0].Exit != string(ExitOK) {
		t.Fatal("failed heartbeat stopped role")
	}
}

func TestHeartbeatIntervalIsTenMinutes(t *testing.T) {
	if heartbeatInterval != 10*time.Minute || heartbeatInterval >= 30*time.Minute {
		t.Fatalf("heartbeat interval %s, want 10m below 30m stale limit", heartbeatInterval)
	}
}

func TestHeartbeatSkipsMissingStep(t *testing.T) {
	h, e := heartbeatEngine(t)
	stop := e.heartbeat(context.Background(), e.Snapshot(), "")
	defer stop()
	time.Sleep(3 * testHeartbeatEvery)
	if len(h.steps()) != 0 {
		t.Fatal("heartbeat sent without a step id")
	}
}

func TestHeartbeatStopCancelsLockedRetry(t *testing.T) {
	h, e := heartbeatEngine(t, func(o *harnessOpts) {
		o.routes = []clitest.Route{{Tool: "slicer", Args: []string{"relay-run", "step", "run-1", "--step", "1", "--phase", "start"}, Prefix: true, Exit: 3, Stderr: `{"error":{"code":"locked","message":"busy"}}`}}
	})
	sleeping := make(chan struct{})
	e.wr.Sleep = func(ctx context.Context, _ time.Duration) error {
		close(sleeping)
		<-ctx.Done()
		return ctx.Err()
	}
	stop := e.heartbeat(context.Background(), e.Snapshot(), "1")
	defer stop()
	select {
	case <-sleeping:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat never reached locked retry")
	}
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not cancel locked retry")
	}
	if got := len(heartbeats(h)); got != 1 {
		t.Fatalf("heartbeat attempts %d, want 1", got)
	}
}

func TestHeartbeatStopsOnRoundCancellation(t *testing.T) {
	h, e := heartbeatEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var during bool
	h.runner.script = func(Request, int) (Result, error) {
		during = waitHeartbeat(h)
		cancel()
		return Result{}, nil
	}
	_, err := e.runRound(ctx, round{role: RoleImplementer, n: 1, state: runrec.StateImplement, task: "S04 implementer"}, nil)
	if err != context.Canceled {
		t.Fatalf("round error %v, want context canceled", err)
	}
	if !during {
		t.Fatal("no heartbeat during role")
	}
	before := len(h.steps())
	time.Sleep(3 * testHeartbeatEvery)
	if len(h.steps()) != before {
		t.Fatal("heartbeat continued after canceled round returned")
	}
}
