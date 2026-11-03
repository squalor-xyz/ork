// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"sort"
	"sync"
	"time"
)

// Working is one step of a run that is running now (S235, *proposed*).
type Working struct {
	What       string        // "implementer round 1", "checks"
	Since      time.Time     // when the step started
	LastOutput time.Time     // the last sign of life from a role; zero before the first, and for a check
	Slept      time.Duration // how long the machine slept since Since
}

// workEntry is a registered step. watch is nil for a check and when the
// watchdog is off.
type workEntry struct {
	what        string
	since       time.Time
	watch       *roleWatch
	sleptAtBase time.Duration // Wake.Slept() when the step started
}

// workings is the set of steps the engine runs now. It has its own lock,
// because Run holds Engine.mu while a step runs.
type workings struct {
	mu      sync.Mutex
	entries map[*workEntry]struct{}
}

// beginWork registers a step and returns the function that removes it. Call the
// function on every path, with defer.
func (e *Engine) beginWork(what string, watch *roleWatch) (end func()) {
	ent := &workEntry{what: what, since: e.d.now(), watch: watch, sleptAtBase: e.d.Wake.Slept()}
	e.work.mu.Lock()
	if e.work.entries == nil {
		e.work.entries = map[*workEntry]struct{}{}
	}
	e.work.entries[ent] = struct{}{}
	e.work.mu.Unlock()
	return func() {
		e.work.mu.Lock()
		delete(e.work.entries, ent)
		e.work.mu.Unlock()
	}
}

// Working lists what the engine runs now, by name. It may be called at any
// moment; it is empty when nothing runs.
func (e *Engine) Working() []Working {
	e.work.mu.Lock()
	ents := make([]*workEntry, 0, len(e.work.entries))
	for ent := range e.work.entries {
		ents = append(ents, ent)
	}
	e.work.mu.Unlock()
	if len(ents) == 0 {
		return nil
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].what < ents[j].what })
	slept := e.d.Wake.Slept()
	out := make([]Working, len(ents))
	for i, ent := range ents {
		out[i] = Working{What: ent.what, Since: ent.since, LastOutput: ent.watch.lastOutput(), Slept: slept - ent.sleptAtBase}
	}
	return out
}
