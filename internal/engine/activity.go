// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// activityRing belongs only to the current role attempt. It never enters run.json.
// Its lock is separate from Engine.mu, which Run holds while the runner streams.
type activityRing struct {
	mu         sync.Mutex
	generation uint64
	active     bool
	live       Live
	buckets    [10]activityBucket
}
type activityBucket struct {
	edge  int64
	count int
}

func (e *Engine) startActivity() (event func(), end func()) {
	a := &e.activity
	a.mu.Lock()
	a.generation++
	generation := a.generation
	a.active = true
	a.buckets = [10]activityBucket{}
	a.live = Live{}
	a.mu.Unlock()
	return func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			if !a.active || a.generation != generation {
				return
			}
			edge := e.d.now().Unix() / 30
			b := &a.buckets[(edge%10+10)%10]
			if b.edge != edge {
				*b = activityBucket{edge: edge}
			}
			b.count++
		}, func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.generation == generation {
				a.active = false
			}
		}
}

// Activity returns ten wall-clock 30-second buckets, oldest first, against the
// snapshot caller's clock. No active role means no series, including after load.
func (e *Engine) Activity(now time.Time) []int {
	a := &e.activity
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.active {
		return nil
	}
	out := make([]int, 10)
	edge := now.Unix() / 30
	for i := range out {
		want := edge - 9 + int64(i)
		b := a.buckets[(want%10+10)%10]
		if b.edge == want {
			out[i] = b.count
		}
	}
	return out
}

// liveHook captures the current attempt so late callbacks cannot contaminate a retry.
func (e *Engine) liveHook() func(Live) {
	a := &e.activity
	a.mu.Lock()
	generation := a.generation
	a.mu.Unlock()
	return func(l Live) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if !a.active || a.generation != generation {
			return
		}
		// Keep line breaks, but never terminal controls in the display buffer.
		a.live.Text += strings.Map(func(r rune) rune {
			if r != '\n' && unicode.IsControl(r) {
				return -1
			}
			return r
		}, l.Text)
		if len(a.live.Text) > 2048 {
			tail := a.live.Text[len(a.live.Text)-2048:]
			for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
				tail = tail[1:]
			}
			a.live.Text = string(append([]byte(nil), tail...))
			a.live.TailTruncated = true
		}
		a.live.Tokens += l.Tokens
		a.live.UsageKnown = a.live.UsageKnown || l.UsageKnown || l.Tokens != 0
	}
}

// Live returns a copy of the current attempt's transient output and token total.
func (e *Engine) Live() Live {
	a := &e.activity
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.active {
		return Live{}
	}
	return a.live
}
