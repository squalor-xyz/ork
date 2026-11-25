// SPDX-License-Identifier: GPL-3.0-or-later

// Package cache is the daemon's in-memory snapshot cache: one value per
// project and part, shared by every client (dash.cache). Each part is read on
// its schedule (dash.refresh) by one CLI call; a Fetcher that is also a
// source.Batcher reads the items, goals and runs parts that come due together
// across projects with one call, and a part whose revision did not change is
// only marked read again. A read that is already running
// is shared, so a part is never read twice at once, and at most Limit CLI
// processes run at once across all projects. A failed read keeps the last good
// value, and a read that fails with corrupt, locked or a timeout is tried once
// more at once (R3). A change of a project's git HEAD reads its goals and
// decided records again (boundary.git-head). With no client attached (SetIdle)
// nothing is read on a timer. The cache keeps no files and watches none.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
	// the last good read holds a live run (proposed; 2 s since S336, adj 301).
	LiveInterval = 2 * time.Second
)

// BatchWindow is how long a read of Items, Goals or Runs waits for others to
// join it in one batched call (proposed, adj 306). Parts that come due within
// the window, such as every project's at one tick, share one slicer process.
// It is real time, not the Clock's: it joins reads that start together, and
// does not schedule them.
const BatchWindow = 10 * time.Millisecond

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
	Idle     bool                                   // start with no client attached: no read comes on a timer (see SetIdle)
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
	batcher  source.Batcher // fetch's optional batched read; nil when it has none
	onChange func(project string, part source.Part)
	slots    chan struct{} // one token per CLI process that may run
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup // reads in flight

	mu      sync.Mutex
	closed  bool
	idle    bool // no client is attached: nothing is read on a timer (S336)
	visible map[string]bool
	entries map[key]*entry
	queue   []queued // reads of batchable parts that wait for the batch window to end
}

// queued is a read that waits for the next batched call.
type queued struct {
	k key
	f *flight
	p registry.Project
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
	rev     string            // slicer's revision of value, from a batched read; empty after a read of the part alone
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
	batcher, _ := opt.Fetcher.(source.Batcher)
	return &Cache{
		clock:    opt.Clock,
		fetch:    opt.Fetcher,
		batcher:  batcher,
		onChange: opt.OnChange,
		slots:    make(chan struct{}, Limit),
		ctx:      ctx,
		cancel:   cancel,
		idle:     opt.Idle,
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
	if c.batcher != nil && batchable(k.part) {
		c.enqueueLocked(queued{k, f, e.reg})
	} else {
		c.wg.Add(1)
		go c.run(k, e.reg, f)
	}
	c.scheduleLocked(k)
	return f
}

// batchable reports whether a part is read with the others in one batched call
// (poll.snapshot-parts, adj 299). Deps, open and decided records and git HEAD
// stay one call per project.
func batchable(part source.Part) bool {
	switch part {
	case source.Items, source.Goals, source.Runs:
		return true
	}
	return false
}

// enqueueLocked adds a read to the batch that the end of BatchWindow starts.
// The first read of a window starts the goroutine that waits for it.
func (c *Cache) enqueueLocked(q queued) {
	c.queue = append(c.queue, q)
	if len(c.queue) == 1 {
		c.wg.Add(1)
		go c.flush()
	}
}

// flush waits for the batch window to end, then reads every queued part of
// every project with one batched call. A call that fails as a whole is read
// part by part this once (poll.snapshot-errors, adj 300); a project that the
// reply marks as failed fails only that project's parts.
func (c *Cache) flush() {
	defer c.wg.Done()
	timer := time.NewTimer(BatchWindow)
	select {
	case <-timer.C:
	case <-c.ctx.Done():
		timer.Stop()
	}

	c.mu.Lock()
	queue := c.queue
	c.queue = nil
	var live, gone []queued
	var reqs []source.BatchRequest
	at := map[string]int{}
	for _, q := range queue {
		e := c.entries[q.k]
		if e == nil || e.flight != q.f {
			gone = append(gone, q) // forgotten during the window
			continue
		}
		i, ok := at[q.k.project]
		if !ok {
			i = len(reqs)
			at[q.k.project] = i
			reqs = append(reqs, source.BatchRequest{Project: q.p, Have: map[source.Part]string{}})
		}
		reqs[i].Parts = append(reqs[i].Parts, q.k.part)
		if e.rev != "" && !e.readAt.IsZero() {
			reqs[i].Have[q.k.part] = e.rev
		}
		live = append(live, q)
	}
	c.mu.Unlock()

	for _, q := range gone {
		c.finish(q.k, q.f, nil, ErrForgotten)
	}
	if len(live) == 0 {
		return
	}
	results, err := c.callBatch(reqs)
	if err == nil && len(results) != len(reqs) {
		err = errors.New("batched read answered the wrong number of projects")
	}
	if err != nil {
		for _, q := range live {
			if c.ctx.Err() != nil {
				c.finish(q.k, q.f, nil, c.ctx.Err())
				continue
			}
			c.wg.Add(1)
			go c.run(q.k, q.p, q.f)
		}
		return
	}
	for _, q := range live {
		res := results[at[q.k.project]]
		bp, ok := res.Parts[q.k.part]
		switch {
		case res.Err != nil:
			c.finishRead(q.k, q.f, readOut{err: res.Err})
		case !ok:
			c.finishRead(q.k, q.f, readOut{err: fmt.Errorf("batched read has no %s of %s", q.k.part, q.k.project)})
		case bp.Err != nil:
			c.finishRead(q.k, q.f, readOut{err: bp.Err})
		default:
			c.finishRead(q.k, q.f, readOut{v: bp.Value, rev: bp.Revision, unchanged: bp.Unchanged})
		}
	}
}

// callBatch makes the batched call, holding one of the Limit slots while it
// runs. As for a part, a call that fails with corrupt, locked or a timeout is
// tried once more at once.
func (c *Cache) callBatch(reqs []source.BatchRequest) ([]source.BatchResult, error) {
	once := func() ([]source.BatchResult, error) {
		select {
		case c.slots <- struct{}{}:
		case <-c.ctx.Done():
			return nil, c.ctx.Err()
		}
		defer func() { <-c.slots }()
		return c.batcher.FetchBatch(c.ctx, reqs)
	}
	res, err := once()
	if err != nil && retryable(err) && c.ctx.Err() == nil {
		res, err = once()
	}
	return res, err
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
	if c.closed || c.idle {
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
	c.finishRead(k, f, readOut{v: v, err: err})
}

// readOut is a read's outcome. rev is slicer's revision of v, from a batched
// read. unchanged says a batched read found rev equal to the one the cache holds
// for the part, so v is nil and the held value stays.
type readOut struct {
	v         any
	err       error
	rev       string
	unchanged bool
}

// finishRead is finish for a read that may be an unchanged revision. That read
// only brings the part's ReadAt up to date, with no notice, unless the part's last
// read failed: its recovery is a change.
func (c *Cache) finishRead(k key, f *flight, out readOut) {
	v, err := out.v, out.err
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
	if err == nil && out.unchanged && (e.readAt.IsZero() || e.rev == "" || e.rev != out.rev) {
		err = errors.New("batched read found a part unchanged that the cache holds no such revision of")
	}
	if err == nil && !out.unchanged {
		b, merr := json.Marshal(v)
		if merr != nil {
			err = merr
		} else {
			sum = sha256.Sum256(b)
		}
	}
	changed, headMoved := false, false
	wasLive := k.part == source.Runs && c.liveRunLocked(k)
	switch {
	case err != nil:
		changed = e.err == nil || e.err.Error() != err.Error()
		e.err = err
	case out.unchanged:
		changed = e.err != nil
		e.readAt, e.err = c.clock.Now(), nil
	default:
		changed = e.readAt.IsZero() || sum != e.sum || e.err != nil
		headMoved = k.part == source.Head && !e.readAt.IsZero() && sum != e.sum
		e.value, e.readAt, e.sum, e.rev, e.err = v, c.clock.Now(), sum, out.rev, nil
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
	c.mu.Unlock()
	// The notice goes out before the read's waiters are released, so a client
	// whose Get or Refresh has returned has already had it sent, and a
	// subscription made after the call does not get it (S544).
	if changed && k.part != source.Head && c.onChange != nil {
		c.onChange(k.project, k.part)
	}
	close(f.done)
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
// While the cache is idle (SetIdle) no timer runs for either.
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

// SetIdle says whether no client is attached. While idle, no read comes on a
// timer: the timers stop, and only a read that someone asks for (Get, Refresh, a
// HEAD change) happens. When a client attaches (idle false), each part of a
// visible project that is overdue is read at once (after a long idle, every
// part; a part read within its interval waits for it); a
// project that is not visible is read an interval from now (S336, adj 301).
func (c *Cache) SetIdle(idle bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idle == idle {
		return
	}
	c.idle = idle
	for k, e := range c.entries {
		if idle {
			if e.timer != nil {
				e.timer.Stop()
				e.timer = nil
			}
			continue
		}
		if c.visible[k.project] {
			c.scheduleLocked(k)
		} else if e.timer == nil && !c.closed {
			e.timer = c.clock.AfterFunc(c.intervalLocked(k), func() { c.tick(k) })
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
