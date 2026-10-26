// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"sync"
)

// Landings lets one landing per project run at a time (S119): the engines of
// a project share it, and the next landing waits for the one in progress. The
// zero value is ready.
type Landings struct {
	mu    sync.Mutex
	slots map[string]chan struct{}
	wait  map[string]int
}

// defaultLandings is what an engine uses when its Deps carry none, so all
// engines of one process serialize their landings.
var defaultLandings = &Landings{}

func (d Deps) landings() *Landings {
	if d.Landings != nil {
		return d.Landings
	}
	return defaultLandings
}

// Acquire waits for the project's landing slot and returns the function that
// frees it. It gives up when ctx ends.
func (l *Landings) Acquire(ctx context.Context, key string) (release func(), err error) {
	l.mu.Lock()
	if l.slots == nil {
		l.slots, l.wait = map[string]chan struct{}{}, map[string]int{}
	}
	slot := l.slots[key]
	if slot == nil {
		slot = make(chan struct{}, 1)
		l.slots[key] = slot
	}
	select {
	case slot <- struct{}{}:
		l.mu.Unlock()
		return func() { <-slot }, nil
	default:
	}
	l.wait[key]++
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.wait[key]--
		l.mu.Unlock()
	}()
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// waiters is how many landings of the project wait for the slot (for tests).
func (l *Landings) waiters(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.wait[key]
}
