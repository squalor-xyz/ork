// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"sync"
	"time"
)

// The sleep detector (S235, *proposed*). When the machine sleeps, the wall
// clock jumps and the hung-role watchdog would read the whole sleep as
// silence. A Wake samples the wall clock about every wakeSampleEvery. A gap
// between two samples of more than wakeGap is a sleep, and the gap less one
// step is added to the time slept. The watchdog subtracts the time slept from
// a role's silence (watchdog.go). It works whether or not the platform's
// monotonic clock counts sleep, and needs only the standard library.

const (
	// wakeSampleEvery is the step between two samples.
	wakeSampleEvery = 5 * time.Second
	// wakeGap is the gap between two samples that counts as a sleep. It is
	// well over a step, so a busy machine that is late by a few seconds is not
	// read as asleep.
	wakeGap = 30 * time.Second
)

// Wake counts the time the machine slept. One Wake is shared by every run of
// the daemon. A nil *Wake counts nothing.
type Wake struct {
	now   func() time.Time
	every time.Duration // the sampling step
	gap   time.Duration // the smallest gap between samples that is a sleep

	mu    sync.Mutex
	last  time.Time     // wall time of the last sample
	slept time.Duration // total over the life of the Wake

	startOnce sync.Once
}

// NewWake returns a detector that reads the wall clock from now. Call Start
// to sample in the background.
func NewWake(now func() time.Time) *Wake {
	return newWake(now, wakeSampleEvery, wakeGap)
}

func newWake(now func() time.Time, every, gap time.Duration) *Wake {
	w := &Wake{now: now, every: every, gap: gap}
	w.last = w.wall()
	return w
}

// wall reads the clock without its monotonic reading. On some platforms the
// monotonic clock stops during sleep, so a difference of two readings that
// carry it would hide the sleep.
func (w *Wake) wall() time.Time { return w.now().Round(0) }

// Start samples the wall clock every step until ctx ends. It does nothing for
// a nil Wake, and a second call does nothing.
func (w *Wake) Start(ctx context.Context) {
	if w == nil {
		return
	}
	w.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(w.every)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					w.sample()
				}
			}
		}()
	})
}

// sample reads the clock and adds the last gap to the time slept when it is
// over the threshold.
func (w *Wake) sample() {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.wall()
	if gap := now.Sub(w.last); gap > w.gap {
		w.slept += gap - w.every
	}
	w.last = now
}

// Slept returns the total time the machine slept. It samples first, so that a
// caller never reads a total that lacks a jump it has already crossed.
func (w *Wake) Slept() time.Duration {
	if w == nil {
		return 0
	}
	w.sample()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.slept
}
