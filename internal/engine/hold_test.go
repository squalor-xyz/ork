// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"testing"
	"time"
)

// S431 (*proposed*; adj 464): a Hold blocks a role start while it is set, ends
// the wait when it is released or the context ends, and never touches a role
// that already runs.
func TestHoldBlocksUntilReleased(t *testing.T) {
	h := NewHold(nil)
	h.Set(true)
	done := make(chan error, 1)
	go func() { done <- h.WaitSpec(context.Background()) }()
	deadline := time.Now().Add(5 * time.Second)
	for h.SpecWaiting() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the start never waited")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("a held start went on: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	h.Set(false)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("released wait: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the start did not go on after the release")
	}
	if h.SpecWaiting() != 0 {
		t.Fatalf("spec waiting after release: %d", h.SpecWaiting())
	}
}

func TestHoldWaitEndsWithTheContext(t *testing.T) {
	h := NewHold(nil)
	h.Set(true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.WaitSpec(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("wait ended with %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the wait ignored its context")
	}
}

func TestNilHoldHoldsNothing(t *testing.T) {
	var h *Hold
	h.Set(true)
	if h.Held() || h.SpecWaiting() != 0 || h.WaitSpec(context.Background()) != nil {
		t.Fatal("a nil hold held something")
	}
}

// A spec check role start waits for the hold before it calls the runner.
func TestSpecRoleWaitsForTheHold(t *testing.T) {
	h := NewHold(nil)
	h.Set(true)
	called := make(chan struct{}, 1)
	d := Deps{Hold: h, Runner: contextRunner{run: func(ctx context.Context, r Request) (Result, error) {
		called <- struct{}{}
		return Result{}, nil
	}}}
	done := make(chan error, 1)
	go func() {
		_, err := d.runSpecRole(context.Background(), Request{Vendor: "claude"})
		done <- err
	}()
	select {
	case <-called:
		t.Fatal("the spec check role started under the hold")
	case <-time.After(50 * time.Millisecond):
	}
	h.Set(false)
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("the spec check role did not start after the release")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
