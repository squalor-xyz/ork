// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// vendorClock is a clock the test moves by hand.
type vendorClock struct {
	mu sync.Mutex
	t  time.Time
}

func newVendorClock() *vendorClock {
	return &vendorClock{t: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
}

func (c *vendorClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *vendorClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// acquireAsync starts an Acquire in a goroutine and reports its outcome.
type acquired struct {
	release func()
	waited  bool
	err     error
}

func acquireAsync(ctx context.Context, v *Vendors, vendor string) <-chan acquired {
	out := make(chan acquired, 1)
	go func() {
		release, waited, err := v.Acquire(ctx, vendor)
		out <- acquired{release, waited, err}
	}()
	return out
}

func mustNotReturn(t *testing.T, ch <-chan acquired, what string) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("%s returned early: %+v", what, got)
	case <-time.After(60 * time.Millisecond):
	}
}

func mustReturn(t *testing.T, ch <-chan acquired, what string) acquired {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(3 * time.Second):
		t.Fatalf("%s did not return", what)
		return acquired{}
	}
}

// waitForWaiters polls until the vendor has n waiters.
func waitForWaiters(t *testing.T, v *Vendors, vendor string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for v.waiters(vendor) != n {
		if time.Now().After(deadline) {
			t.Fatalf("waiters(%s) = %d, want %d", vendor, v.waiters(vendor), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestVendorSlotsLimitHoldersPerVendorAndLeaveOthersAlone(t *testing.T) {
	v := NewVendors(map[string]int{"codex": 1})
	ctx := context.Background()
	first, waited, err := v.Acquire(ctx, "codex")
	if err != nil || waited {
		t.Fatalf("first codex slot: waited %v, err %v", waited, err)
	}
	second := acquireAsync(ctx, v, "codex")
	mustNotReturn(t, second, "the second codex slot")

	// Claude has no cap: any number of holders at once.
	boundedCtx, stop := context.WithTimeout(ctx, 2*time.Second) // a capped claude fails the test instead of hanging it
	defer stop()
	for i := range 5 {
		release, waited, err := v.Acquire(boundedCtx, "claude")
		if err != nil || waited {
			t.Fatalf("claude slot %d: waited %v, err %v", i, waited, err)
		}
		defer release()
	}
	mustNotReturn(t, second, "the second codex slot while claude holds slots")

	first()
	got := mustReturn(t, second, "the second codex slot after the release")
	if got.err != nil || !got.waited {
		t.Fatalf("second codex slot: waited %v, err %v", got.waited, got.err)
	}
	got.release()

	// Releasing twice frees one slot, not two.
	a, _, _ := v.Acquire(ctx, "codex")
	a()
	a()
	b, _, _ := v.Acquire(ctx, "codex")
	third := acquireAsync(ctx, v, "codex")
	mustNotReturn(t, third, "a codex slot after a double release")
	b()
	mustReturn(t, third, "a codex slot after the release").release()

	// A nil Vendors never blocks and remembers nothing.
	var none *Vendors
	for range 3 {
		release, waited, err := none.Acquire(ctx, "codex")
		if err != nil || waited {
			t.Fatalf("nil Vendors: waited %v, err %v", waited, err)
		}
		release()
	}
	none.MarkOut("codex", "x", time.Now())
	none.Back("codex")
	if none.IsOut("codex") || none.Out() != nil {
		t.Fatal("a nil Vendors remembers a vendor as out")
	}
}

func TestVendorSlotWaitersGoInArrivalOrderAndStopOnCancel(t *testing.T) {
	v := NewVendors(map[string]int{"codex": 1})
	ctx := context.Background()
	holder, _, _ := v.Acquire(ctx, "codex")

	cancelCtx, cancel := context.WithCancel(ctx)
	var order []acquired
	chans := make([]<-chan acquired, 3)
	// Waiter 0 and 2 stay; waiter 1 is cancelled while it waits.
	chans[0] = acquireAsync(ctx, v, "codex")
	waitForWaiters(t, v, "codex", 1)
	chans[1] = acquireAsync(cancelCtx, v, "codex")
	waitForWaiters(t, v, "codex", 2)
	chans[2] = acquireAsync(ctx, v, "codex")
	waitForWaiters(t, v, "codex", 3)

	cancel()
	cancelled := mustReturn(t, chans[1], "the cancelled waiter")
	if cancelled.err != context.Canceled || cancelled.release != nil {
		t.Fatalf("cancelled waiter: err %v, release set %v", cancelled.err, cancelled.release != nil)
	}
	waitForWaiters(t, v, "codex", 2)

	holder()
	first := mustReturn(t, chans[0], "the oldest waiter")
	order = append(order, first)
	mustNotReturn(t, chans[2], "the newest waiter while the oldest holds the slot")
	first.release()
	order = append(order, mustReturn(t, chans[2], "the newest waiter after the release"))
	order[1].release()
	for i, got := range order {
		if got.err != nil || !got.waited {
			t.Fatalf("waiter %d: waited %v, err %v", i, got.waited, got.err)
		}
	}
	// The cancelled waiter's place is free: both slots came back.
	last, waited, err := v.Acquire(ctx, "codex")
	if err != nil || waited {
		t.Fatalf("slot after the waiters: waited %v, err %v", waited, err)
	}
	last()
	if v.waiters("codex") != 0 {
		t.Fatalf("waiters left: %d", v.waiters("codex"))
	}
}

func TestVendorOutExpiresAfterSixtyMinutesAndBackClearsIt(t *testing.T) {
	clock := newVendorClock()
	v := NewVendors(nil).WithClock(clock.Now)
	at := clock.Now()
	v.MarkOut("codex", "You've hit your usage limit", at)
	v.MarkOut("claude", "limit", at.Add(time.Minute))
	for _, name := range []string{"zed", "alpha", "mid"} {
		v.MarkOut(name, "x", at)
	}
	if !v.IsOut("codex") || !v.IsOut("claude") || v.IsOut("other") {
		t.Fatal("marked vendors must be out and others not")
	}
	out := v.Out()
	var names []string
	for _, entry := range out {
		names = append(names, entry.Vendor)
	}
	if strings.Join(names, ",") != "alpha,claude,codex,mid,zed" {
		t.Fatalf("Out() must be sorted by vendor: %v", names)
	}
	for _, name := range []string{"zed", "alpha", "mid"} {
		v.Back(name)
	}
	if codex := out[2]; codex.Reason != "You've hit your usage limit" || !codex.Since.Equal(at) || !codex.Until.Equal(at.Add(VendorOutFor)) {
		t.Fatalf("codex entry: %+v", codex)
	}

	clock.Advance(59 * time.Minute)
	if !v.IsOut("codex") {
		t.Fatal("codex must be out at 59 minutes")
	}
	clock.Advance(time.Minute)
	if v.IsOut("codex") {
		t.Fatal("codex must be back at 60 minutes")
	}
	if !v.IsOut("claude") {
		t.Fatal("claude was marked a minute later and is still out")
	}
	if got := v.Out(); len(got) != 1 || got[0].Vendor != "claude" {
		t.Fatalf("an expired vendor must leave Out(): %+v", got)
	}

	v.Back("claude")
	if v.IsOut("claude") || len(v.Out()) != 0 {
		t.Fatal("Back must clear the mark")
	}

	// A second mark restarts the 60 minutes.
	first := clock.Now()
	v.MarkOut("codex", "again", first)
	clock.Advance(30 * time.Minute)
	v.MarkOut("codex", "once more", clock.Now())
	clock.Advance(45 * time.Minute)
	if !v.IsOut("codex") {
		t.Fatal("a repeated mark must restart the expiry")
	}
	if got := v.Out(); got[0].Reason != "once more" || !got[0].Since.Equal(first) || !got[0].Until.Equal(first.Add(30*time.Minute+VendorOutFor)) {
		t.Fatalf("the newest reason and expiry stand, and Since stays: %+v", got)
	}
}

// holders is how many slots of the vendor are held (for tests).
func (v *Vendors) holders(vendor string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.held[vendor]
}

func TestOwnerMarkHasNoExpiryAndBackReportsWhetherItWasOut(t *testing.T) {
	clock := newVendorClock()
	v := NewVendors(nil).WithClock(clock.Now)
	at := clock.Now()
	v.MarkOutOwner("codex", "marked out by the owner", at)
	out := v.Out()
	if len(out) != 1 || !out[0].Until.IsZero() || !out[0].Since.Equal(at) {
		t.Fatalf("an owner mark has no Until: %+v", out)
	}
	clock.Advance(100 * time.Hour)
	if !v.IsOut("codex") {
		t.Fatal("an owner mark must hold until back")
	}
	// A rate-limit exit does not turn it into an expiring mark, nor change it.
	v.MarkOut("codex", "usage limit", clock.Now())
	if out := v.Out(); len(out) != 1 || !out[0].Until.IsZero() || out[0].Reason != "marked out by the owner" || !out[0].Since.Equal(at) {
		t.Fatalf("an exit mark changed an owner mark: %+v", out)
	}
	clock.Advance(100 * time.Hour)
	if !v.IsOut("codex") {
		t.Fatal("the owner mark expired after an exit mark")
	}
	if !v.Back("codex") {
		t.Fatal("Back must say the vendor was out")
	}
	if v.Back("codex") || v.Back("claude") {
		t.Fatal("Back must say a vendor that is not out was not out")
	}

	// An owner mark replaces an exit mark: it holds until back, with its own reason and time.
	v.MarkOut("codex", "usage limit", clock.Now())
	clock.Advance(10 * time.Minute)
	v.MarkOutOwner("codex", "credits tomorrow", clock.Now())
	clock.Advance(100 * time.Hour)
	if out := v.Out(); len(out) != 1 || !out[0].Until.IsZero() || out[0].Reason != "credits tomorrow" {
		t.Fatalf("owner mark over an exit mark: %+v", out)
	}

	// An exit mark that ran out is not "was out".
	w := NewVendors(nil).WithClock(clock.Now)
	w.MarkOut("codex", "x", clock.Now())
	clock.Advance(VendorOutFor)
	if w.Back("codex") {
		t.Fatal("Back on an expired mark must say the vendor was not out")
	}
}
