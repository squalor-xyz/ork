// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// VendorOutFor is how long a vendor stays out after a rate-limit or
// out-of-credits exit (S169, *proposed*). The owner fixed it on 2026-10-29:
// the classifier cannot tell a 429, a usage window and out of credits apart,
// so that memory expires on its own. `ork vendor back` ends it earlier, and a
// daemon restart clears it. A mark the owner sets with `ork vendor out` has no
// expiry.
const VendorOutFor = 60 * time.Minute

// VendorOut is a vendor that the daemon remembers as out.
type VendorOut struct {
	Vendor string
	Reason string    // the exit text of the round that marked it, or the owner's
	Since  time.Time // when it was first marked, in this stretch
	Until  time.Time // when the memory expires; zero for a mark the owner set, which holds until back
}

// Vendors is the per-vendor slot cap and the vendor-out memory (S169,
// *proposed*). One value is shared by every engine of a daemon, like
// Landings. It lives in memory only. A nil *Vendors has no cap and no memory,
// and every method is safe on it.
type Vendors struct {
	mu    sync.Mutex
	now   func() time.Time
	caps  map[string]int
	held  map[string]int
	queue map[string][]*vendorWaiter // oldest first
	out   map[string]VendorOut
}

// vendorWaiter is one round that waits for a vendor slot. granted is closed
// by the release that hands it the slot.
type vendorWaiter struct {
	granted chan struct{}
}

// NewVendors returns a Vendors with the given slot caps by vendor. A vendor
// with a cap of 0, or none, is unlimited.
func NewVendors(caps map[string]int) *Vendors {
	return &Vendors{
		caps:  maps.Clone(caps),
		held:  map[string]int{},
		queue: map[string][]*vendorWaiter{},
		out:   map[string]VendorOut{},
	}
}

// WithClock sets the clock that expires a vendor-out mark, and returns v. The
// default is the real clock. Call it before the value is shared.
func (v *Vendors) WithClock(now func() time.Time) *Vendors {
	if v != nil {
		v.now = now
	}
	return v
}

func (v *Vendors) clock() time.Time {
	if v.now != nil {
		return v.now().UTC()
	}
	return time.Now().UTC()
}

// Acquire waits for a slot of the vendor and returns the function that frees
// it. waited says the caller had to wait. Waiters are served oldest first. It
// gives up when ctx ends and returns ctx.Err() and no release.
func (v *Vendors) Acquire(ctx context.Context, vendor string) (release func(), waited bool, err error) {
	return v.AcquireWait(ctx, vendor, nil)
}

// AcquireWait is Acquire with a callback: onWait is called once, from the
// caller's goroutine, when the caller has taken its place in the queue and is
// about to block.
func (v *Vendors) AcquireWait(ctx context.Context, vendor string, onWait func()) (release func(), waited bool, err error) {
	if v == nil {
		return func() {}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	v.mu.Lock()
	limit := v.caps[vendor]
	if limit <= 0 || v.held[vendor] < limit {
		v.held[vendor]++
		v.mu.Unlock()
		return v.releaser(vendor), false, nil
	}
	w := &vendorWaiter{granted: make(chan struct{})}
	v.queue[vendor] = append(v.queue[vendor], w)
	v.mu.Unlock()
	if onWait != nil {
		onWait()
	}
	select {
	case <-w.granted:
		return v.releaser(vendor), true, nil
	case <-ctx.Done():
		v.mu.Lock()
		i := slices.Index(v.queue[vendor], w)
		if i >= 0 {
			v.queue[vendor] = slices.Delete(v.queue[vendor], i, i+1)
			v.mu.Unlock()
			return nil, true, ctx.Err()
		}
		v.mu.Unlock()
		// The slot was handed over while the wait ended: give it on.
		v.releaser(vendor)()
		return nil, true, ctx.Err()
	}
}

// releaser frees one slot of the vendor, once, however often it is called. The
// slot goes to the oldest waiter, if there is one.
func (v *Vendors) releaser(vendor string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			if q := v.queue[vendor]; len(q) > 0 {
				v.queue[vendor] = q[1:]
				close(q[0].granted) // the slot stays held, now by the waiter
				return
			}
			v.held[vendor]--
		})
	}
}

// waiters is how many rounds wait for a slot of the vendor (for tests).
func (v *Vendors) waiters(vendor string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.queue[vendor])
}

// MarkOut remembers the vendor as out for VendorOutFor from at, because a
// round exited with a rate limit or out of credits. A vendor that is out
// already keeps its Since; its reason and expiry are renewed. A mark the owner
// set (MarkOutOwner) is not touched: it holds until back.
func (v *Vendors) MarkOut(vendor, reason string, at time.Time) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.expireLocked()
	entry := VendorOut{Vendor: vendor, Reason: reason, Since: at, Until: at.Add(VendorOutFor)}
	if old, ok := v.out[vendor]; ok {
		if old.Until.IsZero() {
			return
		}
		entry.Since = old.Since
	}
	v.out[vendor] = entry
}

// MarkOutOwner remembers the vendor as out because the owner said so. The
// mark has no expiry (Until is zero): it holds until Back or a daemon restart
// (owner decision 2026-10-29). It replaces any earlier mark.
func (v *Vendors) MarkOutOwner(vendor, reason string, at time.Time) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.out[vendor] = VendorOut{Vendor: vendor, Reason: reason, Since: at}
}

// Back forgets that the vendor is out. It says whether the vendor was out.
func (v *Vendors) Back(vendor string) bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.expireLocked()
	_, was := v.out[vendor]
	delete(v.out, vendor)
	return was
}

// IsOut reports whether the vendor is out now.
func (v *Vendors) IsOut(vendor string) bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.expireLocked()
	_, ok := v.out[vendor]
	return ok
}

// Out lists the vendors that are out now, sorted by vendor.
func (v *Vendors) Out() []VendorOut {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.expireLocked()
	var list []VendorOut
	for _, entry := range v.out {
		list = append(list, entry)
	}
	slices.SortFunc(list, func(a, b VendorOut) int { return strings.Compare(a.Vendor, b.Vendor) })
	return list
}

// expireLocked drops the marks whose time is up. The caller holds v.mu.
func (v *Vendors) expireLocked() {
	now := v.clock()
	for vendor, entry := range v.out {
		if !entry.Until.IsZero() && !now.Before(entry.Until) {
			delete(v.out, vendor)
		}
	}
}

// vendorWaits is the memory-only record of the vendors the rounds of one run
// wait for a slot of.
type vendorWaits struct {
	mu sync.Mutex
	by map[string]int
}

func (w *vendorWaits) add(vendor string, n int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.by == nil {
		w.by = map[string]int{}
	}
	w.by[vendor] += n
	if w.by[vendor] <= 0 {
		delete(w.by, vendor)
	}
}

// VendorWait names the vendor a round of this run waits for a slot of, or "".
// It is memory only, like Live: a restarted daemon has no waits. With two
// rounds waiting on different vendors it names the first by name.
func (e *Engine) VendorWait() string {
	e.vendorWait.mu.Lock()
	defer e.vendorWait.mu.Unlock()
	var names []string
	for vendor := range e.vendorWait.by {
		names = append(names, vendor)
	}
	slices.Sort(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// skipVendorsOut returns the row a new round starts on: the first row at or
// after i whose vendor is not out. When every row from i on is on a vendor
// that is out, the round tries row i anyway (S169). The sticky rule of
// currentEntry is unchanged: this skips rows before a round, and the role's
// records then match the later row going forward.
func (e *Engine) skipVendorsOut(entries []runrec.LineupEntry, i int) int {
	if i >= len(entries) || !e.d.Vendors.IsOut(entries[i].Vendor) {
		return i
	}
	for j := i + 1; j < len(entries); j++ {
		if !e.d.Vendors.IsOut(entries[j].Vendor) {
			return j
		}
	}
	return i
}
