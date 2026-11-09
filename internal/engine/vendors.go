// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
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
// expiry unless the owner gave an end (`--until`), is stored in the state
// directory (S299, *proposed*), and so survives a restart.
const VendorOutFor = 60 * time.Minute

// IsLocalVendor identifies the three launcher identities decided for S301.
func IsLocalVendor(vendor string) bool {
	return vendor == "claude-local" || vendor == "codex-local" || vendor == "grok-local"
}

const LocalSlotCap = 4
const localPoolName = "\x00local"

// IsLocalRun uses the admitted primary implementer, never the active fallback.
func IsLocalRun(lineup map[string][]runrec.LineupEntry) bool {
	entries := lineup[RoleImplementer]
	return len(entries) > 0 && IsLocalVendor(entries[0].Vendor)
}

func vendorPool(vendor string) string {
	if IsLocalVendor(vendor) {
		return localPoolName
	}
	return vendor
}

// VendorOut is a vendor that the daemon remembers as out.
type VendorOut struct {
	Vendor string
	Reason string    // the exit text of the round that marked it, or the owner's
	Since  time.Time // when it was first marked, in this stretch
	Until  time.Time // when the memory expires; zero for a mark the owner set without an end, which holds until back
	Owner  bool      // the owner set it with `ork vendor out`; only these are stored (S299)
}

// Vendors is the per-vendor slot cap and the vendor-out memory (S169,
// *proposed*). One value is shared by every engine of a daemon, like
// Landings. The marks live in memory, except the owner's (S299, *proposed*),
// which WithStore keeps in a file. A nil *Vendors has no cap and no memory, and
// every method is safe on it.
type Vendors struct {
	mu      sync.Mutex
	store   string // the file of the owner's marks; "" keeps them in memory
	now     func() time.Time
	caps    map[string]int
	held    map[string]int
	queue   map[string][]*vendorWaiter // oldest first
	out     map[string]VendorOut
	changed chan struct{} // one daemon admission watcher; notifications coalesce
}

// vendorWaiter is one round that waits for a vendor slot. granted is closed
// by the release that hands it the slot.
type vendorWaiter struct {
	granted chan struct{}
}

// NewVendors returns a Vendors with the given cloud slot caps by vendor. A
// cloud vendor with no cap is unlimited. The three local launchers always
// share four slots (adj 187).
func NewVendors(caps map[string]int) *Vendors {
	return &Vendors{
		caps:    maps.Clone(caps),
		held:    map[string]int{},
		queue:   map[string][]*vendorWaiter{},
		out:     map[string]VendorOut{},
		changed: make(chan struct{}, 1),
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

func (v *Vendors) limitLocked(pool string) int {
	if pool == localPoolName {
		return LocalSlotCap
	}
	return v.caps[pool]
}

// HasSlot is a read-only admission check. TryAcquire must still claim the slot.
func (v *Vendors) HasSlot(vendor string) bool {
	if v == nil {
		return true
	}
	pool := vendorPool(vendor)
	v.mu.Lock()
	defer v.mu.Unlock()
	limit := v.limitLocked(pool)
	return limit <= 0 || v.held[pool] < limit && len(v.queue[pool]) == 0
}

// TryAcquire reserves a slot without waiting or overtaking an existing waiter.
func (v *Vendors) TryAcquire(vendor string) (release func(), ok bool) {
	if v == nil {
		return func() {}, true
	}
	pool := vendorPool(vendor)
	v.mu.Lock()
	limit := v.limitLocked(pool)
	if limit > 0 && (v.held[pool] >= limit || len(v.queue[pool]) > 0) {
		v.mu.Unlock()
		return nil, false
	}
	v.held[pool]++
	v.mu.Unlock()
	return v.releaser(pool), true
}

// SlotChanges wakes the daemon when a shared local slot is released. It is
// memory only, and has one consumer. A nil Vendors supplies no notifications.
func (v *Vendors) SlotChanges() <-chan struct{} {
	if v == nil {
		return nil
	}
	return v.changed
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
	vendor = vendorPool(vendor)
	v.mu.Lock()
	limit := v.limitLocked(vendor)
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
			defer func() {
				if vendor == localPoolName {
					select {
					case v.changed <- struct{}{}:
					default:
					}
				}
			}()
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
// set (MarkOutOwner) is not touched: it holds until back, or until its end.
// These marks are memory only.
func (v *Vendors) MarkOut(vendor, reason string, at time.Time) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.expireLocked()
	entry := VendorOut{Vendor: vendor, Reason: reason, Since: at, Until: at.Add(VendorOutFor)}
	if old, ok := v.out[vendor]; ok {
		if old.Owner {
			return
		}
		entry.Since = old.Since
	}
	v.out[vendor] = entry
}

// MarkOutOwner remembers the vendor as out because the owner said so. The
// mark has no expiry (Until is zero): it holds until Back (owner decision
// 2026-10-29), also across a daemon restart when the value has a store (S299,
// *proposed*). It replaces any earlier mark. The error says the mark could not
// be stored; the vendor is then not marked.
func (v *Vendors) MarkOutOwner(vendor, reason string, at time.Time) error {
	return v.MarkOutOwnerUntil(vendor, reason, at, time.Time{})
}

// MarkOutOwnerUntil is MarkOutOwner with an end: the mark lapses at until and
// holds until then. A zero until is a mark with no end.
func (v *Vendors) MarkOutOwnerUntil(vendor, reason string, at, until time.Time) error {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.expireLocked()
	old, had := v.out[vendor]
	v.out[vendor] = VendorOut{Vendor: vendor, Reason: reason, Since: at, Until: until, Owner: true}
	if err := v.saveLocked(); err != nil {
		if had {
			v.out[vendor] = old
		} else {
			delete(v.out, vendor)
		}
		return err
	}
	return nil
}

// Back forgets that the vendor is out. It says whether the vendor was out. The
// error says the end of an owner mark could not be stored; the vendor is then
// still out, and was is false.
func (v *Vendors) Back(vendor string) (was bool, err error) {
	if v == nil {
		return false, nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.expireLocked()
	old, was := v.out[vendor]
	delete(v.out, vendor)
	if was && old.Owner {
		if err := v.saveLocked(); err != nil {
			v.out[vendor] = old
			return false, err
		}
	}
	return was, nil
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

// vendorStoreVersion is the version of the file of the owner's marks.
const vendorStoreVersion = 1

// vendorStoreFile is the file of the owner's marks (S299, *proposed*).
type vendorStoreFile struct {
	Version int               `json:"version"`
	Marks   []vendorStoreMark `json:"marks"`
}

type vendorStoreMark struct {
	Vendor string    `json:"vendor"`
	Reason string    `json:"reason,omitempty"`
	Since  time.Time `json:"since"`
	Until  time.Time `json:"until,omitzero"` // absent for a mark with no end
}

// WithStore keeps the owner's marks in the file at path, and returns v: it
// loads the marks the file holds that have not ended, and writes the file again
// whenever the owner marks a vendor out or back. Exit marks are never stored. A
// missing file holds no marks. When the file cannot be read, is not valid or
// has a version this ork does not know, WithStore returns v with no marks
// loaded and the error, and the next owner mark replaces the file. A mark of a
// vendor that known rejects is not loaded, so it cannot stay out with no command
// that ends it (known nil accepts every name). Call it before the value is shared.
func (v *Vendors) WithStore(path string, known func(vendor string) bool) (*Vendors, error) {
	if v == nil {
		return nil, nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.store = path
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return v, fmt.Errorf("read the vendor marks: %w", err)
	}
	var file vendorStoreFile
	if err := json.Unmarshal(b, &file); err != nil {
		return v, fmt.Errorf("read the vendor marks in %s: %w", path, err)
	}
	if file.Version != vendorStoreVersion {
		return v, fmt.Errorf("read the vendor marks in %s: version %d is not one this ork knows", path, file.Version)
	}
	// A mark whose end has passed is dropped by the next read of the marks.
	for _, m := range file.Marks {
		if m.Vendor == "" || (known != nil && !known(m.Vendor)) {
			continue
		}
		v.out[m.Vendor] = VendorOut{Vendor: m.Vendor, Reason: m.Reason, Since: m.Since, Until: m.Until, Owner: true}
	}
	return v, nil
}

// saveLocked writes the owner's marks to the store, if there is one. The file
// is replaced whole, at mode 0600, so a reader never sees half of it. The caller
// holds v.mu.
func (v *Vendors) saveLocked() error {
	if v.store == "" {
		return nil
	}
	file := vendorStoreFile{Version: vendorStoreVersion, Marks: []vendorStoreMark{}}
	for _, entry := range v.out {
		if entry.Owner {
			file.Marks = append(file.Marks, vendorStoreMark{Vendor: entry.Vendor, Reason: entry.Reason, Since: entry.Since, Until: entry.Until})
		}
	}
	slices.SortFunc(file.Marks, func(a, b vendorStoreMark) int { return strings.Compare(a.Vendor, b.Vendor) })
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(v.store)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("store the vendor marks: %w", err)
	}
	f, err := os.CreateTemp(dir, ".vendors-out-*")
	if err != nil {
		return fmt.Errorf("store the vendor marks: %w", err)
	}
	defer os.Remove(f.Name()) // a no-op once the rename has moved it
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("store the vendor marks: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("store the vendor marks: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("store the vendor marks: %w", err)
	}
	if err := os.Rename(f.Name(), v.store); err != nil {
		return fmt.Errorf("store the vendor marks: %w", err)
	}
	// Make the rename itself durable. The mark is stored whether or not this works.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
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

// VendorWait names the vendor a role or local run waits for a slot of, or "".
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
