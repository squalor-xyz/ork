// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// The sleep detector (S235, *proposed*). A wall-clock gap between two samples
// that is far longer than the sampling step is a sleep of the machine. These
// tests move a fake wall clock by hand.

// wallClock is a settable wall clock.
type wallClock struct {
	mu sync.Mutex
	t  time.Time
}

func newWallClock() *wallClock {
	return &wallClock{t: time.Date(2026, 11, 2, 20, 0, 0, 0, time.UTC)}
}

func (c *wallClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *wallClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// startedWake returns a Wake whose Start has run. The tests sample by hand; the
// background sampler's first tick is a real 5s away, after the test is over.
func startedWake(t *testing.T, clock *wallClock) *Wake {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w := NewWake(clock.Now)
	w.Start(ctx)
	return w
}

// A Wake counts a sleep only after Start (S259). A Wake nobody started has no
// sampler, so a gap it sees may be a late caller, not a sleep.
func TestWakeCountsASleepOnlyAfterStart(t *testing.T) {
	clock := newWallClock()
	w := NewWake(clock.Now)
	if w.Started() {
		t.Error("a new Wake reports started")
	}
	clock.advance(2 * time.Hour)
	if got := w.Slept(); got != 0 {
		t.Errorf("an unstarted Wake counted %s of sleep", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	if !w.Started() {
		t.Error("Start did not make the Wake report started")
	}
	if got := w.Slept(); got != 0 {
		t.Errorf("Start counted the jump from before it: %s", got)
	}
	clock.advance(2 * time.Hour)
	if got, want := w.Slept(), 2*time.Hour-wakeSampleEvery; got != want {
		t.Errorf("Slept %s after Start, want %s", got, want)
	}
}

// The jump comes before Start and nothing samples in between, so only Start
// itself can keep the Wake from counting it.
func TestWakeStartIgnoresAJumpFromBeforeIt(t *testing.T) {
	clock := newWallClock()
	w := NewWake(clock.Now)
	clock.advance(2 * time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	if got := w.Slept(); got != 0 {
		t.Errorf("Slept %s for a jump from before Start", got)
	}
}

func TestWakeStartedIsNilSafe(t *testing.T) {
	var w *Wake
	if w.Started() {
		t.Error("a nil Wake reports started")
	}
}

func TestWakeCountsAWallClockJumpAsSleep(t *testing.T) {
	clock := newWallClock()
	w := startedWake(t, clock)
	clock.advance(wakeSampleEvery)
	w.sample()
	clock.advance(2 * time.Hour)
	w.sample()
	if got, want := w.Slept(), 2*time.Hour-wakeSampleEvery; got != want {
		t.Errorf("Slept %s, want %s", got, want)
	}
	// Steady steps after the jump add nothing.
	clock.advance(wakeSampleEvery)
	w.sample()
	if got, want := w.Slept(), 2*time.Hour-wakeSampleEvery; got != want {
		t.Errorf("Slept %s after a steady step, want %s", got, want)
	}
}

func TestWakeIgnoresASmallStall(t *testing.T) {
	clock := newWallClock()
	w := startedWake(t, clock)
	clock.advance(10 * time.Second)
	w.sample()
	if got := w.Slept(); got != 0 {
		t.Errorf("a stall of 10s counted as %s of sleep", got)
	}
}

func TestWakeSampleIsSynchronous(t *testing.T) {
	clock := newWallClock()
	w := startedWake(t, clock)
	clock.advance(2 * time.Hour) // no tick has run
	if got, want := w.Slept(), 2*time.Hour-wakeSampleEvery; got != want {
		t.Errorf("Slept %s right after a jump, want %s", got, want)
	}
}

func TestWakeNilCountsNothing(t *testing.T) {
	var w *Wake
	if got := w.Slept(); got != 0 {
		t.Errorf("nil Wake slept %s", got)
	}
	w.Start(context.Background()) // must not panic
}

func TestWakeStartSamplesInTheBackground(t *testing.T) {
	clock := newWallClock()
	w := newWake(clock.Now, 5*time.Millisecond, 20*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	clock.advance(time.Hour)
	deadline := time.Now().Add(2 * time.Second)
	for {
		w.mu.Lock()
		got := w.slept // read without Slept, which would sample by itself
		w.mu.Unlock()
		if got >= time.Hour-time.Second {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the background sampler never saw the jump (slept %s)", got)
		}
		time.Sleep(time.Millisecond)
	}
}

// blockUntilReleased is a role that waits for release, or for its context.
func blockUntilReleased(started chan<- struct{}, release <-chan struct{}) func(context.Context, Request) (Result, error) {
	var once sync.Once
	return func(ctx context.Context, req Request) (Result, error) {
		once.Do(func() { close(started) })
		select {
		case <-release:
			return defaultResult(req), nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(safetyStop):
			return Result{}, context.DeadlineExceeded
		}
	}
}

// clockWithOffset is the real clock plus an offset that a test moves to fake a
// sleep.
type clockWithOffset struct{ off atomic.Int64 }

func (c *clockWithOffset) Now() time.Time {
	return time.Now().Add(time.Duration(c.off.Load()))
}

func (c *clockWithOffset) jump(d time.Duration) { c.off.Add(int64(d)) }

// testWakeEvery and testWakeGap are the sampling step and the sleep threshold
// of a Wake in the tests that use real timers: the silence a sleep leaves
// behind is one step, and it must stay under the limit of a few hundred
// milliseconds.
const (
	testWakeEvery = 10 * time.Millisecond
	testWakeGap   = 100 * time.Millisecond
)

func TestWorkingNamesARunningRoleWithItsLastOutputAndSleep(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var req Request
	var reqMu sync.Mutex
	hr := &hangRunner{silent: onlyRole(RoleImplementer)}
	inner := blockUntilReleased(started, release)
	hr.script = func(ctx context.Context, r Request) (Result, error) {
		reqMu.Lock()
		req = r
		reqMu.Unlock()
		return inner(ctx, r)
	}
	h := hungHarness(t, time.Hour, hr)
	clock := &clockWithOffset{}
	h.deps.Now = clock.Now
	wctx, wcancel := context.WithCancel(context.Background())
	defer wcancel()
	h.deps.Wake = newWake(clock.Now, testWakeEvery, testWakeGap)
	h.deps.Wake.Start(wctx)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	clock.jump(time.Hour) // a sleep before the round began is not the round's
	done := make(chan error, 1)
	go func() { done <- e.Run(context.Background()) }()
	<-started
	got := e.Working()
	if len(got) != 1 || got[0].What != "implementer round 1" {
		t.Fatalf("working: %+v", got)
	}
	since := got[0].Since
	if since.IsZero() || time.Since(since) > 5*time.Second {
		t.Errorf("since %s", since)
	}
	if !got[0].LastOutput.IsZero() || got[0].Slept != 0 {
		t.Errorf("before any event or jump: %+v", got[0])
	}
	reqMu.Lock()
	onEvent := req.OnEvent
	reqMu.Unlock()
	time.Sleep(5 * time.Millisecond)
	before := clock.Now()
	onEvent()
	after := clock.Now()
	clock.jump(90 * time.Minute)
	got = e.Working()
	if len(got) != 1 {
		t.Fatalf("working: %+v", got)
	}
	if got[0].Since != since {
		t.Errorf("since moved from %s to %s", since, got[0].Since)
	}
	if lo, hi := before.UTC(), after.UTC(); got[0].LastOutput.Before(lo) || got[0].LastOutput.After(hi) {
		t.Errorf("last output %s, want between %s and %s", got[0].LastOutput, lo, hi)
	}
	if want := 90*time.Minute - testWakeEvery; got[0].Slept < want || got[0].Slept > 90*time.Minute+time.Second {
		t.Errorf("slept %s, want about %s", got[0].Slept, want)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWorkingNamesTheChecksStep(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := newHarness(t)
	h.deps.Check = func(ctx context.Context, dir, command string, timeout time.Duration) (CheckOutput, error) {
		close(started)
		<-release
		return CheckOutput{OK: true, Output: "ok"}, nil
	}
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.Run(context.Background()) }()
	<-started
	got := e.Working()
	if len(got) != 1 || got[0].What != "checks" || got[0].Since.IsZero() || !got[0].LastOutput.IsZero() {
		t.Errorf("working: %+v", got)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWorkingIsEmptyWhenNothingRuns(t *testing.T) {
	t.Run("after a normal run", func(t *testing.T) {
		h := newHarness(t)
		e := h.start()
		requireState(t, e, runrec.StateAwaitingApproval)
		if got := e.Working(); len(got) != 0 {
			t.Errorf("working after the run: %+v", got)
		}
	})
	t.Run("after a role error", func(t *testing.T) {
		hr := &hangRunner{silent: onlyRole(RoleImplementer), script: func(ctx context.Context, req Request) (Result, error) {
			return Result{}, context.DeadlineExceeded
		}}
		h := hungHarness(t, time.Hour, hr)
		e := h.start()
		if got := e.Working(); len(got) != 0 {
			t.Errorf("working after a failed role: %+v", got)
		}
	})
	t.Run("after a cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		started := make(chan struct{})
		hr := &hangRunner{silent: onlyRole(RoleImplementer), script: func(rctx context.Context, req Request) (Result, error) {
			close(started)
			<-rctx.Done()
			return Result{}, rctx.Err()
		}}
		h := hungHarness(t, time.Hour, hr)
		e, err := New(context.Background(), h.deps, h.cfg)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { e.Run(ctx); close(done) }()
		<-started
		if got := e.Working(); len(got) != 1 {
			t.Fatalf("working while running: %+v", got)
		}
		cancel()
		<-done
		if got := e.Working(); len(got) != 0 {
			t.Errorf("working after a cancel: %+v", got)
		}
	})
	t.Run("after a failed check", func(t *testing.T) {
		h := newHarness(t)
		h.deps.Check = func(ctx context.Context, dir, command string, timeout time.Duration) (CheckOutput, error) {
			return CheckOutput{}, context.DeadlineExceeded
		}
		e := h.start()
		if got := e.Working(); len(got) != 0 {
			t.Errorf("working after a failed check: %+v", got)
		}
	})
}

func TestWorkingListsTheReviewerAndTheUIPassTogether(t *testing.T) {
	var mu sync.Mutex
	running := 0
	both := make(chan struct{})
	release := make(chan struct{})
	hr := &hangRunner{silent: onlyRole(RoleReviewer, RoleUI), script: func(ctx context.Context, req Request) (Result, error) {
		mu.Lock()
		running++
		if running == 2 {
			close(both)
		}
		mu.Unlock()
		select {
		case <-release:
			return defaultResult(req), nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}}
	h := uiHarness(t, tuiFiles)
	h.deps.Now = time.Now
	h.deps.Runner = hr
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.Run(context.Background()) }()
	select {
	case <-both:
	case <-time.After(safetyStop):
		t.Fatal("the two review roles never ran together")
	}
	got := e.Working()
	if len(got) != 2 || got[0].What != "reviewer round 1" || got[1].What != "ui round 1" {
		t.Errorf("working: %+v", got)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
