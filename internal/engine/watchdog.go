// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// The watchdog for a hung role (S182, *proposed*). A role that is alive but
// has sent no stream event and no assistant text for longer than the limit is
// killed by ending the context its round runs under: the vendor adapters kill
// the whole process group when that context ends. The run then pauses as
// lost, and the owner retries it. ork kills only the process group it
// recorded for the role (OnStart).

// DefaultHungAfter is how long a role may stay silent before the watchdog
// kills it. It matches the stale cutoff of the dashboard's Running row
// (docs/dashboard.md). The owner fixed it on 2026-10-29: it is not a setting.
const DefaultHungAfter = 30 * time.Minute

// LocalRoleAfter is the absolute runtime limit for a local launcher (adj 315).
// Output does not extend it. A timeout falls back through the role's lineup.
const LocalRoleAfter = 90 * time.Minute

// maxHungCheck is the longest wait between two looks at a silent role.
const maxHungCheck = 30 * time.Second

// hungLimit is the silence that kills a role, and whether the watchdog is on.
// Zero means DefaultHungAfter; a negative value turns it off.
func (d Deps) hungLimit() (time.Duration, bool) {
	switch {
	case d.HungAfter < 0:
		return 0, false
	case d.HungAfter == 0:
		return DefaultHungAfter, true
	}
	return d.HungAfter, true
}

// roleWatch keeps the time of the last sign of life from one role round.
type roleWatch struct {
	now         func() time.Time
	wake        *Wake // the sleep detector (S235); nil counts no sleep
	mu          sync.Mutex
	last        time.Time     // the last sign of life, or the start of the round
	output      time.Time     // the last sign of life; zero before the first
	sleptAtLast time.Duration // wake.Slept() when last was stored
	over        bool          // the round ended or the watchdog fired; nothing more is recorded
	hung        bool
	absolute    bool // local roles use elapsed time instead of silence
}

// touch records a sign of life. A nil watch is the watchdog turned off.
func (w *roleWatch) touch() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.over {
		// Slept first: a jump between the two reads then leaves the silence
		// short, never long, so it cannot cause a kill.
		if !w.absolute {
			w.sleptAtLast = w.wake.Slept()
		}
		at := w.now()
		if !w.absolute {
			w.last = at
		}
		w.output = at
	}
}

// silence is how long the role has been silent (for a local role, how long it
// has run), less the time the machine slept since its last sign of life, or
// since its start for a local role (S235). Call it with w.mu held. Slept
// samples the clock itself, after now is read, so the total never lacks a
// jump that now has already crossed.
func (w *roleWatch) silence() time.Duration {
	now := w.now() // before Slept: its sample is later, so it holds any jump now holds
	// A local role's elapsed time also leaves out the machine's sleep: neither
	// last nor sleptAtLast moves on output, so the limit is runtime (S301).
	return now.Sub(w.last) - (w.wake.Slept() - w.sleptAtLast)
}

// lastOutput is the time of the last sign of life, zero before the first. A
// nil watch (the watchdog off) has none.
func (w *roleWatch) lastOutput() time.Time {
	if w == nil {
		return time.Time{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output
}

// watchRole starts the watchdog for one role round, or returns a nil watch
// and no-op functions when it is off. kill ends the round's context. Call it
// just before Runner.Run, so that no wait for a slot counts as silence. stop
// ends the check and waits for it; after stop, hung is final.
func (e *Engine) watchRole(ctx context.Context, kill func(), vendor string) (w *roleWatch, stop func(), hung func() bool) {
	limit, on := e.d.hungLimit()
	every := maxHungCheck
	if on {
		every = max(min(limit/4, maxHungCheck), time.Millisecond)
	}
	local := IsLocalVendor(vendor)
	if local {
		limit, on = LocalRoleAfter, true
	}
	if !on {
		return nil, func() {}, func() bool { return false }
	}
	w = &roleWatch{now: e.d.now, wake: e.d.Wake, absolute: local}
	w.sleptAtLast = w.wake.Slept()
	w.last = w.now()
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.mu.Lock()
				if w.over {
					w.mu.Unlock()
					return
				}
				if w.silence() <= limit {
					w.mu.Unlock()
					continue
				}
				w.over, w.hung = true, true
				w.mu.Unlock()
				kill()
				return
			}
		}
	}()
	stop = func() {
		w.mu.Lock()
		w.over = true
		w.mu.Unlock()
		cancel()
		<-done
	}
	hung = func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.hung
	}
	return w, stop, hung
}

// hungReason says which role was killed, for how long it was silent and which
// process group went. The group clause is left out when the role never
// reported one.
func hungReason(r round, limit time.Duration, pgid int) string {
	reason := fmt.Sprintf("%s round %d hung: no output for %s", r.role, r.n, formatLimit(limit))
	if pgid > 0 {
		reason += fmt.Sprintf("; its process group %d was killed", pgid)
	}
	return reason
}

// formatLimit writes whole minutes as 30m and anything else as a duration.
func formatLimit(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}

// endHungRound records a role the watchdog killed and pauses the run as lost.
// The role's record ends with exit hung, its ledger step ends fail, and the
// heartbeat stops first. It is not a fallback exit: the next attempt runs the
// same row, with the "Earlier process lost" brief section.
func (e *Engine) endHungRound(ctx context.Context, rec runrec.Record, r round, idx int, stepID string, res Result, limit time.Duration, stopBeat func()) (outcome, error) {
	var pgid int
	err := e.run.Update(func(rr *runrec.Record) {
		role := &rr.Roles[idx]
		pgid = role.PGID
		role.EndedAt = e.d.now()
		role.Exit = "hung"
		role.Session = res.Session
		role.Tokens = res.Tokens
		role.CostUSD = res.CostUSD
	})
	if err != nil {
		return outcome{}, err
	}
	reason := hungReason(r, limit, pgid)
	stopBeat()
	e.stepFail(ctx, rec, stepID, "hung", reason, nil)
	return outcome{}, e.pause(runrec.Pending{Kind: runrec.PendingLost, State: r.state, Role: r.role, Round: r.n, Reason: reason})
}
