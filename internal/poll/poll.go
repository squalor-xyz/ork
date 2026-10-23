// SPDX-License-Identifier: GPL-3.0-or-later

// Package poll is the refresh schedule of the dashboard (dash.refresh): the
// visible project every 5 s, the others every 30 s, an explicit refresh now,
// and never a second call for a key while one is in flight. It holds no
// clock and starts no goroutine; the caller passes the time in.
package poll

import "time"

// Schedule is how often each kind of project is refreshed.
type Schedule struct {
	Visible time.Duration
	Other   time.Duration
}

// DefaultSchedule is what dash.refresh decided.
var DefaultSchedule = Schedule{Visible: 5 * time.Second, Other: 30 * time.Second}

// Poller tracks when each key was last refreshed and which are in flight.
type Poller struct {
	sched   Schedule
	keys    []string
	jobs    map[string]*job
	visible string
}

type job struct {
	inFlight bool
	ever     bool      // a call has returned
	last     time.Time // when the last call returned
}

// New returns a Poller for the keys, in the order Due reports them. No key
// has been refreshed yet, so every key is due.
func New(s Schedule, keys ...string) *Poller {
	p := &Poller{sched: s, keys: keys, jobs: make(map[string]*job, len(keys))}
	for _, k := range keys {
		p.jobs[k] = &job{}
	}
	return p
}

// SetVisible says which key the owner is looking at.
func (p *Poller) SetVisible(key string) { p.visible = key }

func (p *Poller) interval(key string) time.Duration {
	if key == p.visible {
		return p.sched.Visible
	}
	return p.sched.Other
}

// Due returns the keys whose next refresh time has come, in key order, leaving
// out any key in flight. The interval runs from when the last call returned.
func (p *Poller) Due(now time.Time) []string {
	var due []string
	for _, k := range p.keys {
		j := p.jobs[k]
		if j.inFlight {
			continue
		}
		if !j.ever || !now.Before(j.last.Add(p.interval(k))) {
			due = append(due, k)
		}
	}
	return due
}

// Start marks a call for key as in flight. It reports false, and changes
// nothing, when key is unknown or a call for it is already in flight.
func (p *Poller) Start(key string) bool {
	j, ok := p.jobs[key]
	if !ok || j.inFlight {
		return false
	}
	j.inFlight = true
	return true
}

// Finish marks the call for key as returned at now.
func (p *Poller) Finish(key string, now time.Time) {
	if j, ok := p.jobs[key]; ok {
		j.inFlight = false
		j.ever = true
		j.last = now
	}
}

// InFlight reports whether a call for key has started and not finished.
func (p *Poller) InFlight(key string) bool {
	j, ok := p.jobs[key]
	return ok && j.inFlight
}

// Pending is the number of keys in flight.
func (p *Poller) Pending() int {
	n := 0
	for _, j := range p.jobs {
		if j.inFlight {
			n++
		}
	}
	return n
}

// Add registers a key due immediately. An existing key is left untouched.
func (p *Poller) Add(key string) {
	if _, ok := p.jobs[key]; ok {
		return
	}
	p.keys = append(p.keys, key)
	p.jobs[key] = &job{}
}
