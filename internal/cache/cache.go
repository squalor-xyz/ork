// SPDX-License-Identifier: GPL-3.0-or-later

// Package cache is the daemon's in-memory snapshot cache: one value per
// project and part, shared by every client (dash.cache). Each part is read on
// its schedule (dash.refresh) by one CLI call. A read that is already running
// is shared, so a part is never read twice at once, and at most Limit CLI
// processes run at once across all projects. A failed read keeps the last good
// value, and a read that fails with corrupt, locked or a timeout is tried once
// more at once (R3). A change of a project's git HEAD reads its goals and
// decided records again (boundary.git-head). The cache keeps no files and
// watches none.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// Limit is the most CLI processes that run at once, across all projects.
const Limit = 4

// Refresh intervals (dash.refresh). Items, deps, runs, open records and git HEAD
// follow FastInterval for a visible project and SlowInterval for the others.
// Goals and decided records follow GoalInterval.
const (
	FastInterval = 5 * time.Second
	SlowInterval = 30 * time.Second
	GoalInterval = 5 * time.Minute
	// LiveInterval is the Runs part's interval for a visible project while
	// the last good read holds a live run (proposed).
	LiveInterval = time.Second
)

// ErrClosed is returned once Close has been called.
var ErrClosed = errors.New("cache closed")

// ErrForgotten is returned to a read that was waiting when its project was
// forgotten.
var ErrForgotten = errors.New("project forgotten during the read")

// Timer is a pending call of a Clock.
type Timer interface{ Stop() bool }

// Clock is the source of time. Tests replace it.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Options configures a Cache.
type Options struct {
	Clock    Clock
	Fetcher  source.Fetcher
	OnChange func(project string, part source.Part) // a data part changed; may be nil
}

// Value is one part as the cache holds it: the last good value and when it
// was read, and the error of the last read, if that read failed.
type Value struct {
	Value  any
	ReadAt time.Time // zero when no value was ever read
	Err    error
}

// Cache holds one value per project and part.
type Cache struct {
	clock    Clock
	fetch    source.Fetcher
	onChange func(project string, part source.Part)
	slots    chan struct{} // one token per CLI process that may run
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup // reads in flight

	mu      sync.Mutex
	closed  bool
	visible map[string]bool
	entries map[key]*entry
}

type key struct {
	project string
	part    source.Part
}

// entry is one part of one project.
type entry struct {
	reg     registry.Project  // as last registered; reads use it
	value   any               // the last good value
	readAt  time.Time         // when value was read; zero if never
	sum     [sha256.Size]byte // hash of the value's JSON, to tell a change
	err     error             // the last read's error; nil if it worked
	done    bool              // a read has finished
	started time.Time         // when the last read started
	flight  *flight           // the read in progress, if any
	pending bool              // a read came due during one in flight; it starts when that ends
	timer   Timer             // the next scheduled read
}

// flight is one read in progress. done closes when it has finished.
type flight struct{ done chan struct{} }

// New returns an empty cache.
func New(opt Options) *Cache {
	ctx, cancel := context.WithCancel(context.Background())
	return &Cache{
		clock:    opt.Clock,
		fetch:    opt.Fetcher,
		onChange: opt.OnChange,
		slots:    make(chan struct{}, Limit),
		ctx:      ctx,
		cancel:   cancel,
		visible:  map[string]bool{},
		entries:  map[key]*entry{},
	}
}

// partsOf lists the parts a project has. Open and decided records need adj.
func partsOf(p registry.Project) []source.Part {
	if p.Adj {
		return []source.Part{source.Items, source.Deps, source.Goals, source.Runs, source.Open, source.Decided, source.Head}
	}
	return []source.Part{source.Items, source.Deps, source.Goals, source.Runs, source.Head}
}

// entryLocked returns the entry of a part the project has, or nil.
func (c *Cache) entryLocked(p registry.Project, part source.Part) *entry {
	for _, q := range partsOf(p) {
		if q == part {
			return c.entries[key{p.Name, part}]
		}
	}
	return nil
}

// ensureLocked registers p, and starts a first read of each of its parts that
// the cache has not seen before.
func (c *Cache) ensureLocked(p registry.Project) {
	for _, part := range partsOf(p) {
		k := key{p.Name, part}
		e, ok := c.entries[k]
		if !ok {
			e = &entry{}
			c.entries[k] = e
		}
		e.reg = p
		if !ok {
			c.startLocked(k)
		}
	}
}

// intervalLocked is how long after a read starts the next one is due.
func (c *Cache) intervalLocked(k key) time.Duration {
	switch k.part {
	case source.Goals, source.Decided:
		return GoalInterval
	}
	if c.visible[k.project] {
		if k.part == source.Runs && c.liveRunLocked(k) {
			return LiveInterval
		}
		return FastInterval
	}
	return SlowInterval
}

// liveRunLocked reports whether the last good read of the Runs part holds a
// run that is not stale. A stale run does not change by the second.
func (c *Cache) liveRunLocked(k key) bool {
	e := c.entries[k]
	if e == nil {
		return false
	}
	runs, _ := e.value.([]slicercli.RelayRun)
	for _, r := range runs {
		if r.State != "stale" {
			return true
		}
	}
	return false
}

// startLocked starts a read of k unless one is in flight, and schedules the
// next one an interval after this start. It returns nil once the cache is
// closed.
func (c *Cache) startLocked(k key) *flight {
	e := c.entries[k]
	if e == nil {
		return nil
	}
	if e.flight != nil {
		return e.flight
	}
	if c.closed {
		return nil
	}
	f := &flight{done: make(chan struct{})}
	e.flight = f
	e.started = c.clock.Now()
	e.pending = false
	c.wg.Add(1)
	go c.run(k, e.reg, f)
	c.scheduleLocked(k)
	return f
}

// scheduleLocked sets the timer for k's next read, an interval after the last
// one started. A read that is due while another runs waits for it to end.
func (c *Cache) scheduleLocked(k key) {
	e := c.entries[k]
	if e == nil {
		return
	}
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	if c.closed {
		return
	}
	now := c.clock.Now()
	due := e.started.Add(c.intervalLocked(k))
	if due.After(now) {
		e.timer = c.clock.AfterFunc(due.Sub(now), func() { c.tick(k) })
		return
	}
	if e.flight != nil {
		e.pending = true
		return
	}
	c.startLocked(k)
}

// tick runs when a part's timer fires.
func (c *Cache) tick(k key) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[k]; ok {
		c.scheduleLocked(k)
	}
}

// run makes the read and records it. A read that fails with corrupt, locked
// or a timeout is tried once more at once.
func (c *Cache) run(k key, p registry.Project, f *flight) {
	defer c.wg.Done()
	v, err := c.call(p, k.part)
	if err != nil && retryable(err) && c.ctx.Err() == nil {
		v, err = c.call(p, k.part)
	}
	c.finish(k, f, v, err)
}

// call makes one CLI call, holding one of the Limit slots while it runs.
func (c *Cache) call(p registry.Project, part source.Part) (any, error) {
	select {
	case c.slots <- struct{}{}:
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
	defer func() { <-c.slots }()
	return c.fetch.Fetch(c.ctx, p, part)
}

// retryable reports whether a failed read gets one more try: a corrupt or
// locked store is a write caught half way, and a timeout may be a slow start.
func retryable(err error) bool {
	var te *clirun.ToolError
	if errors.As(err, &te) {
		return te.Code == "corrupt" || te.Code == "locked"
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// requestLocked asks for a read of k that starts after the read in flight, if
// there is one. That read may have started before the change that asked, so
// it is not joined: the next read starts when it ends (pending).
func (c *Cache) requestLocked(k key) {
	e := c.entries[k]
	if e == nil {
		return
	}
	if e.flight != nil {
		e.pending = true
		return
	}
	c.startLocked(k)
}

// finish records a read's outcome. A good read that changes the data is a
// change; so is an error that differs from the last one. The change is
// reported after the lock is released.
func (c *Cache) finish(k key, f *flight, v any, err error) {
	c.mu.Lock()
	e := c.entries[k]
	if e == nil || e.flight != f {
		// The project was forgotten during the read, and may have been registered
		// again since: this read belongs to the old entry. Its value is dropped
		// and nobody is told.
		close(f.done)
		c.mu.Unlock()
		return
	}
	e.flight = nil
	e.done = true
	var sum [sha256.Size]byte
	if err == nil {
		b, merr := json.Marshal(v)
		if merr != nil {
			err = merr
		} else {
			sum = sha256.Sum256(b)
		}
	}
	changed, headMoved := false, false
	wasLive := k.part == source.Runs && c.liveRunLocked(k)
	if err == nil {
		changed = e.readAt.IsZero() || sum != e.sum || e.err != nil
		headMoved = k.part == source.Head && !e.readAt.IsZero() && sum != e.sum
		e.value, e.readAt, e.sum, e.err = v, c.clock.Now(), sum, nil
	} else {
		changed = e.err == nil || e.err.Error() != err.Error()
		e.err = err
	}
	if headMoved {
		c.requestLocked(key{k.project, source.Goals})
		c.requestLocked(key{k.project, source.Decided})
	}
	if e.pending {
		c.startLocked(k)
	} else if k.part == source.Runs && wasLive != c.liveRunLocked(k) {
		// A run began or ended: the next read is due by the new interval.
		c.scheduleLocked(k)
	}
	close(f.done)
	c.mu.Unlock()
	if changed && k.part != source.Head && c.onChange != nil {
		c.onChange(k.project, k.part)
	}
}

func (e *entry) valueLocked() Value {
	return Value{Value: e.value, ReadAt: e.readAt, Err: e.err}
}

// Get returns a part's value. Once a read has finished, Get returns the
// cached value and makes no call. Before that, it waits for the first read
// (ctx may end the wait). A part the project does not have is the zero Value.
func (c *Cache) Get(ctx context.Context, p registry.Project, part source.Part) Value {
	c.mu.Lock()
	c.ensureLocked(p)
	e := c.entryLocked(p, part)
	if e == nil {
		c.mu.Unlock()
		return Value{}
	}
	if e.done {
		v := e.valueLocked()
		c.mu.Unlock()
		return v
	}
	f := c.startLocked(key{p.Name, part})
	c.mu.Unlock()
	if f == nil {
		return Value{Err: ErrClosed}
	}
	select {
	case <-f.done:
	case <-ctx.Done():
		return Value{Err: ctx.Err()}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries[key{p.Name, part}] != e {
		return Value{Err: ErrForgotten} // the project was forgotten while this read ran
	}
	return e.valueLocked()
}

// Refresh reads the given parts of p now, and returns once each has been read.
// A read already in flight is shared, not repeated. Parts p does not have are
// skipped. It returns ctx's error if ctx ends first.
func (c *Cache) Refresh(ctx context.Context, p registry.Project, parts ...source.Part) error {
	c.mu.Lock()
	c.ensureLocked(p)
	var waits []*flight
	for _, part := range parts {
		if c.entryLocked(p, part) == nil {
			continue
		}
		f := c.startLocked(key{p.Name, part})
		if f == nil {
			c.mu.Unlock()
			return ErrClosed
		}
		waits = append(waits, f)
	}
	c.mu.Unlock()
	for _, f := range waits {
		select {
		case <-f.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// SetVisible sets the projects the clients show. Their items, deps, runs, open
// records and git HEAD move to FastInterval, and the others to SlowInterval.
// A project that is newly shown has its goals and decided records read now:
// the tab open.
func (c *Cache) SetVisible(names []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := make(map[string]bool, len(names))
	for _, n := range names {
		next[n] = true
	}
	var opened []string
	for n := range next {
		if !c.visible[n] {
			opened = append(opened, n)
		}
	}
	changed := map[string]bool{}
	for n := range next {
		changed[n] = true
	}
	for n := range c.visible {
		changed[n] = true
	}
	c.visible = next
	for k := range c.entries {
		if changed[k.project] {
			c.scheduleLocked(k)
		}
	}
	for _, n := range opened {
		for _, part := range []source.Part{source.Goals, source.Decided} {
			if c.entries[key{n, part}] != nil {
				c.startLocked(key{n, part})
			}
		}
	}
}

// Close stops every timer, cancels the reads in flight and waits for them to
// end. Reads after Close return ErrClosed. Close may be called again.
func (c *Cache) Close() {
	c.mu.Lock()
	c.closed = true
	for _, e := range c.entries {
		if e.timer != nil {
			e.timer.Stop()
			e.timer = nil
		}
	}
	c.mu.Unlock()
	c.cancel()
	c.wg.Wait()
}

// Forget drops a project that was removed from the registry: its timers stop,
// its values are dropped, and a read still in flight for it ends without a
// value. Its name is no longer visible.
func (c *Cache) Forget(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if k.project == name {
			if e.timer != nil {
				e.timer.Stop()
				e.timer = nil
			}
			delete(c.entries, k)
		}
	}
	delete(c.visible, name)
}
