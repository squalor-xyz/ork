// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"sync"
	"time"
)

// activityRing belongs only to the current role attempt. It never enters run.json.
// Its lock is separate from Engine.mu, which Run holds while the runner streams.
type activityRing struct {
	mu         sync.Mutex
	generation uint64
	active     bool
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
