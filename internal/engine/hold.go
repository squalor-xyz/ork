// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"sync"
)

// HoldReason is the wait text of a run whose next role start is held while
// the daemon drains for a rebuild (S431, *proposed*; adj 464).
const HoldReason = "waiting for daemon rebuild"

// Hold stops new role starts while it is set (S431, *proposed*; adj 464,
// selfhost.auto-rebuild). One value is shared by every engine and spec check
// of a daemon, like Vendors. A role that already runs is never touched: the
// hold is read only at the point where a role would start, and Wait only
// blocks. A nil *Hold holds nothing, and every method is safe on it.
type Hold struct {
	mu          sync.Mutex
	held        bool
	open        chan struct{} // closed when the hold is released; replaced when it is set
	specWaiting int           // spec check roles that wait
	changed     func()        // called, outside mu, after a wait begins or ends
}

// NewHold returns a Hold that is not set. changed, which may be nil, is called
// whenever a role starts or stops waiting, so a drain can look again.
func NewHold(changed func()) *Hold {
	return &Hold{changed: changed}
}

// Set holds role starts (true) or releases every waiting start (false).
func (h *Hold) Set(on bool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	switch {
	case on && !h.held:
		h.held = true
		h.open = make(chan struct{})
	case !on && h.held:
		h.held = false
		close(h.open)
	}
	h.mu.Unlock()
}

// Held reports whether role starts are held now.
func (h *Hold) Held() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.held
}

// SpecWaiting is how many spec check roles wait for the hold to end.
func (h *Hold) SpecWaiting() int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.specWaiting
}

// wait blocks while the hold is set. onWait is called once, before blocking,
// and onDone once, after the wait ends, however it ends; both run outside the
// hold's lock. It returns ctx.Err() when ctx ends first, and nil otherwise. It
// returns at once when nothing is held.
func (h *Hold) wait(ctx context.Context, spec bool, onWait, onDone func()) error {
	if h == nil {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h.mu.Lock()
		if !h.held {
			h.mu.Unlock()
			return nil
		}
		open := h.open
		if spec {
			h.specWaiting++
		}
		h.mu.Unlock()
		if onWait != nil {
			onWait()
		}
		h.notify()
		select {
		case <-open:
		case <-ctx.Done():
		}
		h.mu.Lock()
		if spec {
			h.specWaiting--
		}
		h.mu.Unlock()
		if onDone != nil {
			onDone()
		}
		h.notify()
	}
}

func (h *Hold) notify() {
	if h.changed != nil {
		h.changed()
	}
}

// WaitSpec is the wait of a spec check role.
func (h *Hold) WaitSpec(ctx context.Context) error { return h.wait(ctx, true, nil, nil) }

// holdWaiting is the engine's memory-only record that a role start waits for
// the hold (S431). Its lock is its own, because Run holds the engine's.
type holdWaiting struct {
	mu sync.Mutex
	n  int
}

func (w *holdWaiting) add(n int) {
	w.mu.Lock()
	w.n += n
	w.mu.Unlock()
}

// waitForHold blocks a role start while the daemon holds role starts.
func (e *Engine) waitForHold(ctx context.Context) error {
	return e.d.Hold.wait(ctx, false, func() { e.holdWait.add(1) }, func() { e.holdWait.add(-1) })
}

// HeldForRebuild reports that a role start of this run waits for the hold, and
// that no role or check of the run runs. A run that does both (a split with one
// part held and another running) is not held: its running part ends by itself.
func (e *Engine) HeldForRebuild() bool {
	e.holdWait.mu.Lock()
	waiting := e.holdWait.n > 0
	e.holdWait.mu.Unlock()
	return waiting && len(e.Working()) == 0
}

// WaitsForHold reports that a role start of this run waits for the hold.
func (e *Engine) WaitsForHold() bool {
	e.holdWait.mu.Lock()
	defer e.holdWait.mu.Unlock()
	return e.holdWait.n > 0
}
