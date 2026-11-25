// SPDX-License-Identifier: GPL-3.0-or-later

package cache

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

var (
	alpha = registry.Project{Name: "alpha", Path: "/p/alpha", Adj: true}
	beta  = registry.Project{Name: "beta", Path: "/p/beta"}

	allParts = []source.Part{source.Items, source.Deps, source.Goals, source.Runs, source.Open, source.Decided, source.Head}
)

var epoch = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// fakeClock fires timers when the test advances it. Its Now is epoch plus the
// time advanced so far.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeTimer
}

type fakeTimer struct {
	c       *fakeClock
	at      time.Duration
	f       func()
	stopped bool
	fired   bool
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return epoch.Add(c.now)
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now + d, f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

// Pending returns the number of timers that can still fire.
func (c *fakeClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

// Advance moves time forward and runs the timers that are due, in time order.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now += d
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired && t.at <= c.now {
			t.fired = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	sort.SliceStable(due, func(i, j int) bool { return due[i].at < due[j].at })
	for _, t := range due {
		c.mu.Lock()
		skip := t.stopped
		c.mu.Unlock()
		if !skip {
			t.f()
		}
	}
}

type answerFunc func(p registry.Project, part source.Part) (any, error)

// fakeFetcher answers each call from a function and counts the calls by
// project and part. With gate set, a call waits until gate is closed or its
// context ends.
type fakeFetcher struct {
	mu        sync.Mutex
	counts    map[string]int
	inFlight  int
	maxFlight int
	gate      chan struct{}
	gateOnly  source.Part // with gate set, the part that waits; empty means every part
	answer    answerFunc
}

func newFetcher() *fakeFetcher {
	return &fakeFetcher{counts: map[string]int{}, answer: defaultAnswer}
}

func defaultAnswer(p registry.Project, part source.Part) (any, error) {
	switch part {
	case source.Items, source.Deps:
		return []slicercli.Item{{ID: "S1"}}, nil
	case source.Goals:
		return &slicercli.Goals{}, nil
	case source.Runs:
		return []slicercli.RelayRun{}, nil
	case source.Open, source.Decided:
		return []adjcli.Record{}, nil
	case source.Head:
		return "h1", nil
	}
	return nil, fmt.Errorf("no answer for %s", part)
}

func (f *fakeFetcher) Fetch(ctx context.Context, p registry.Project, part source.Part) (any, error) {
	f.mu.Lock()
	f.counts[p.Name+"/"+string(part)]++
	f.inFlight++
	if f.inFlight > f.maxFlight {
		f.maxFlight = f.inFlight
	}
	gate, gateOnly, answer := f.gate, f.gateOnly, f.answer
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	if gate != nil && (gateOnly == "" || part == gateOnly) {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return answer(p, part)
}

func (f *fakeFetcher) count(project string, part source.Part) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[project+"/"+string(part)]
}

func (f *fakeFetcher) setAnswer(a answerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answer = a
}

// setGate makes every call wait for g.
func (f *fakeFetcher) setGate(g chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = g
	f.gateOnly = ""
}

// setGateFor makes only the calls for part wait for g. The other parts answer
// at once, so a held call takes one of the four slots and no other part is
// left queued for a slot behind it.
func (f *fakeFetcher) setGateFor(part source.Part, g chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = g
	f.gateOnly = part
}

func (f *fakeFetcher) now() (inFlight, maxFlight int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inFlight, f.maxFlight
}

func newCache(t *testing.T, f *fakeFetcher, clock *fakeClock, onChange func(string, source.Part)) *Cache {
	t.Helper()
	c := New(Options{Clock: clock, Fetcher: f, OnChange: onChange})
	t.Cleanup(c.Close)
	return c
}

// readAll reads every part of p and waits for them.
func readAll(t *testing.T, c *Cache, p registry.Project) {
	t.Helper()
	if err := c.Refresh(context.Background(), p, allParts...); err != nil {
		t.Fatalf("refresh %s: %v", p.Name, err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not true within 5s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// settle gives calls that were started a moment to run. A call that should
// not have happened shows up as a changed count after it.
func settle() { time.Sleep(50 * time.Millisecond) }

// waitRead waits until the n-th read of part for project has ended: the
// fetcher has counted n calls, and the cache has no read of that part in
// flight. A test must wait so before it moves the fake clock on. A read still
// running when the clock moves makes the next due read pending, and that read
// is then skipped.
func waitRead(t *testing.T, c *Cache, f *fakeFetcher, project string, part source.Part, n int) {
	t.Helper()
	waitFor(t, fmt.Sprintf("read %d of %s/%s ended", n, project, part), func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		e := c.entries[key{project, part}]
		return f.count(project, part) == n && e != nil && e.flight == nil
	})
}

func TestConcurrentReadsOfAPartShareOneCall(t *testing.T) {
	f := newFetcher()
	gate := make(chan struct{})
	f.setGateFor(source.Items, gate)
	c := newCache(t, f, &fakeClock{}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Get(context.Background(), alpha, source.Items)
		}()
	}
	settle()
	close(gate)
	wg.Wait()
	if n := f.count("alpha", source.Items); n != 1 {
		t.Fatalf("items were read %d times for 10 concurrent reads, want 1", n)
	}
	if v := c.Get(context.Background(), alpha, source.Items); v.Err != nil || v.Value == nil {
		t.Fatalf("items after the read: %+v", v)
	}
}

// A read's change notice is sent before Get or Refresh returns for that read
// (S544). A client that reads a part and then subscribes must not get that
// read's notice. onChange is slow here, so a notice sent after the read's
// waiters were released would still be running when the call returns.
func TestChangeNoticeIsSentBeforeTheReadReturns(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func(c *Cache)
	}{
		{"Get", func(c *Cache) { c.Get(context.Background(), alpha, source.Items) }},
		{"Refresh", func(c *Cache) {
			if err := c.Refresh(context.Background(), alpha, source.Items); err != nil {
				t.Errorf("refresh: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent atomic.Bool
			c := newCache(t, newFetcher(), &fakeClock{}, func(_ string, part source.Part) {
				if part == source.Items {
					time.Sleep(50 * time.Millisecond)
					sent.Store(true)
				}
			})
			tc.read(c)
			if !sent.Load() {
				t.Fatal("the read returned before its change notice was sent")
			}
		})
	}
}

func TestNoMoreThanFourCallsRunAtOnce(t *testing.T) {
	f := newFetcher()
	gate := make(chan struct{})
	f.setGate(gate)
	c := newCache(t, f, &fakeClock{}, nil)
	gamma := registry.Project{Name: "gamma", Path: "/p/gamma", Adj: true}
	var wg sync.WaitGroup
	for _, p := range []registry.Project{alpha, beta, gamma} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Refresh(context.Background(), p, allParts...); err != nil {
				t.Errorf("refresh %s: %v", p.Name, err)
			}
		}()
	}
	waitFor(t, "calls in flight", func() bool { in, _ := f.now(); return in >= Limit })
	settle()
	if in, max := f.now(); in != Limit || max != Limit {
		t.Fatalf("calls in flight %d, highest %d; want %d and %d", in, max, Limit, Limit)
	}
	close(gate)
	wg.Wait()
	if _, max := f.now(); max > Limit {
		t.Fatalf("%d calls ran at once, limit %d", max, Limit)
	}
	total := 0
	for _, n := range f.counts {
		total += n
	}
	// alpha and gamma have seven parts; beta (no adj) has five.
	if total != 19 {
		t.Fatalf("calls made: %d, want 19 (one per part per project)", total)
	}
}

func TestFastPartsRefreshEvery5sWhenVisibleAnd30sOtherwise(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := newCache(t, f, clock, nil)
	readAll(t, c, alpha)
	readAll(t, c, beta)
	c.SetVisible([]string{"alpha"})
	waitFor(t, "alpha goals read again on tab open", func() bool { return f.count("alpha", source.Goals) == 2 })
	waitFor(t, "alpha decided read again on tab open", func() bool { return f.count("alpha", source.Decided) == 2 })

	clock.Advance(4 * time.Second)
	settle()
	if n := f.count("alpha", source.Items); n != 1 {
		t.Fatalf("visible items read at 4 s: %d reads, want 1", n)
	}
	clock.Advance(time.Second)
	waitFor(t, "visible items read at 5 s", func() bool { return f.count("alpha", source.Items) == 2 })
	if n := f.count("beta", source.Items); n != 1 {
		t.Fatalf("other project's items read at 5 s: %d reads, want 1", n)
	}
	for _, part := range []source.Part{source.Items, source.Runs, source.Head} {
		waitRead(t, c, f, "alpha", part, 2) // the reads due at 5 s have ended
	}
	for step := 1; step <= 5; step++ { // to 10 s .. 30 s, one 5 s step at a time
		clock.Advance(5 * time.Second)
		want := step + 2 // one cold read, one at 5 s, then one per step
		for _, part := range []source.Part{source.Items, source.Runs, source.Head} {
			waitRead(t, c, f, "alpha", part, want)
		}
	}
	waitFor(t, "other project's items read twice by 30 s", func() bool { return f.count("beta", source.Items) == 2 })
	settle()
	if n := f.count("alpha", source.Goals); n != 2 {
		t.Fatalf("goals read on a 5 s tick: %d reads, want 2", n)
	}
	if n := f.count("alpha", source.Items); n != 7 {
		t.Fatalf("visible items read %d times after 30 s, want 7", n)
	}
}

func TestGoalsAndDecidedRefreshEvery5MinutesAndOnTabOpen(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := newCache(t, f, clock, nil)
	readAll(t, c, alpha)
	readAll(t, c, beta)
	c.SetVisible([]string{"alpha"})
	waitFor(t, "alpha goals read on tab open", func() bool { return f.count("alpha", source.Goals) == 2 })
	waitFor(t, "alpha decided read on tab open", func() bool { return f.count("alpha", source.Decided) == 2 })

	clock.Advance(299 * time.Second)
	settle()
	if n := f.count("alpha", source.Goals); n != 2 {
		t.Fatalf("alpha goals read before 5 min: %d reads, want 2", n)
	}
	clock.Advance(time.Second)
	waitFor(t, "alpha goals read at 5 min", func() bool { return f.count("alpha", source.Goals) == 3 })
	waitFor(t, "alpha decided read at 5 min", func() bool { return f.count("alpha", source.Decided) == 3 })
	waitFor(t, "beta goals read at 5 min", func() bool { return f.count("beta", source.Goals) == 2 })
}

func TestGitHeadChangeRefreshesGoalsAndDecided(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := newCache(t, f, clock, nil)
	var mu sync.Mutex
	head := "h1"
	f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		if part == source.Head {
			mu.Lock()
			defer mu.Unlock()
			return head, nil
		}
		return defaultAnswer(p, part)
	})
	readAll(t, c, alpha)
	c.SetVisible([]string{"alpha"})
	settle()
	clock.Advance(5 * time.Second) // HEAD is read again and has not changed
	waitFor(t, "HEAD read at 5 s", func() bool { return f.count("alpha", source.Head) == 2 })
	settle()
	if n := f.count("alpha", source.Goals); n != 2 {
		t.Fatalf("goals read with HEAD unchanged: %d reads, want 2 (the tab open only)", n)
	}

	mu.Lock()
	head = "h2"
	mu.Unlock()
	clock.Advance(5 * time.Second)
	waitFor(t, "goals read after HEAD changed", func() bool { return f.count("alpha", source.Goals) == 3 })
	waitFor(t, "decided read after HEAD changed", func() bool { return f.count("alpha", source.Decided) == 3 })
	clock.Advance(5 * time.Second) // HEAD is h2 again: nothing new to read
	waitFor(t, "HEAD read again", func() bool { return f.count("alpha", source.Head) == 4 })
	settle()
	if n := f.count("alpha", source.Goals); n != 3 {
		t.Fatalf("goals read again with HEAD unchanged: %d reads, want 3", n)
	}
}

func TestRefreshReadsEveryPartAndWaits(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := newCache(t, f, clock, nil)
	readAll(t, c, alpha)
	clock.Advance(7 * time.Second)
	if err := c.Refresh(context.Background(), alpha, allParts...); err != nil {
		t.Fatal(err)
	}
	// Refresh returns only once every part is read, so the counts are final now.
	for _, part := range []source.Part{source.Items, source.Deps, source.Goals, source.Runs, source.Open, source.Decided, source.Head} {
		if n := f.count("alpha", part); n != 2 {
			t.Fatalf("%s read %d times after an explicit refresh, want 2", part, n)
		}
	}
	if v := c.Get(context.Background(), alpha, source.Items); !v.ReadAt.Equal(epoch.Add(7 * time.Second)) {
		t.Fatalf("items read at %v, want the refresh time %v", v.ReadAt, epoch.Add(7*time.Second))
	}
}

func TestFailedRefreshKeepsTheLastGoodValueAndItsAge(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := newCache(t, f, clock, nil)
	readAll(t, c, alpha)
	clock.Advance(7 * time.Second)
	f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		if part == source.Items {
			return nil, &clirun.ToolError{Code: "io", Message: "disk gone"}
		}
		return defaultAnswer(p, part)
	})
	if err := c.Refresh(context.Background(), alpha, source.Items); err != nil {
		t.Fatal(err)
	}
	v := c.Get(context.Background(), alpha, source.Items)
	items, ok := v.Value.([]slicercli.Item)
	if !ok || len(items) != 1 || items[0].ID != "S1" {
		t.Fatalf("the last good value was lost: %#v", v.Value)
	}
	if !v.ReadAt.Equal(epoch) {
		t.Fatalf("ReadAt %v, want the time of the good read %v", v.ReadAt, epoch)
	}
	var te *clirun.ToolError
	if !errors.As(v.Err, &te) || te.Code != "io" {
		t.Fatalf("error %v, want the tool's io code", v.Err)
	}
	if age := clock.Now().Sub(v.ReadAt); age != 7*time.Second {
		t.Fatalf("age of the stale value %v, want 7s", age)
	}
}

func TestRetryOnceOnCorruptLockedOrTimeout(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		failTimes int  // how many attempts fail before one works
		wantCalls int  // calls made in all
		wantErr   bool // the part ends with the error
	}{
		{"corrupt is retried", &clirun.ToolError{Code: "corrupt", Message: "half written"}, 1, 2, false},
		{"locked is retried", &clirun.ToolError{Code: "locked", Message: "busy"}, 1, 2, false},
		{"a timeout is retried", &clirun.TimeoutError{Call: clirun.Call{Bin: "slicer"}}, 1, 2, false},
		{"a second failure is not retried again", &clirun.ToolError{Code: "corrupt", Message: "half written"}, 2, 2, true},
		{"no_such_item is not retried", &clirun.ToolError{Code: "no_such_item", Message: "no such item"}, 1, 1, true},
		{"io is not retried", &clirun.ToolError{Code: "io", Message: "disk"}, 1, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFetcher()
			c := newCache(t, f, &fakeClock{}, nil)
			var attempts int
			var mu sync.Mutex
			f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
				if part != source.Items {
					return defaultAnswer(p, part)
				}
				mu.Lock()
				defer mu.Unlock()
				attempts++
				if attempts <= tc.failTimes {
					return nil, tc.err
				}
				return defaultAnswer(p, part)
			})
			if err := c.Refresh(context.Background(), alpha, source.Items); err != nil {
				t.Fatal(err)
			}
			if n := f.count("alpha", source.Items); n != tc.wantCalls {
				t.Fatalf("calls for items: %d, want %d", n, tc.wantCalls)
			}
			v := c.Get(context.Background(), alpha, source.Items)
			if tc.wantErr != (v.Err != nil) {
				t.Fatalf("error %v, want error %v", v.Err, tc.wantErr)
			}
		})
	}
}

func TestNoticeOnlyWhenTheDataChanges(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	var mu sync.Mutex
	var notes []string
	c := newCache(t, f, clock, func(project string, part source.Part) {
		mu.Lock()
		defer mu.Unlock()
		notes = append(notes, project+"/"+string(part))
	})
	take := func() []string {
		settle()
		mu.Lock()
		defer mu.Unlock()
		out := notes
		notes = nil
		return out
	}
	readAll(t, c, alpha)
	awaitNotes := func(n int) {
		waitFor(t, "change notices", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(notes) >= n
		})
	}
	awaitNotes(6)
	first := take()
	if len(first) != 6 { // every data part of alpha, once; HEAD is not a data part
		t.Fatalf("first reads gave notices %v, want the six data parts", first)
	}
	c.SetVisible([]string{"alpha"})
	if got := take(); len(got) != 0 {
		t.Fatalf("tab open with unchanged goals and decided gave notices %v", got)
	}

	clock.Advance(5 * time.Second) // items read again with the same data
	waitRead(t, c, f, "alpha", source.Items, 2)
	if got := take(); len(got) != 0 {
		t.Fatalf("unchanged items gave notices %v", got)
	}

	f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		if part == source.Items {
			return []slicercli.Item{{ID: "S1"}, {ID: "S2"}}, nil
		}
		return defaultAnswer(p, part)
	})
	clock.Advance(5 * time.Second)
	waitRead(t, c, f, "alpha", source.Items, 3)
	awaitNotes(1)
	if got := take(); len(got) != 1 || got[0] != "alpha/items" {
		t.Fatalf("a changed item gave notices %v, want [alpha/items]", got)
	}

	f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		if part == source.Items {
			return nil, &clirun.ToolError{Code: "io", Message: "disk"}
		}
		return defaultAnswer(p, part)
	})
	clock.Advance(5 * time.Second)
	waitRead(t, c, f, "alpha", source.Items, 4)
	awaitNotes(1)
	if got := take(); len(got) != 1 || got[0] != "alpha/items" {
		t.Fatalf("a part that started to fail gave notices %v, want [alpha/items]", got)
	}
	clock.Advance(5 * time.Second)
	waitRead(t, c, f, "alpha", source.Items, 5)
	if got := take(); len(got) != 0 {
		t.Fatalf("the same error again gave notices %v", got)
	}
}

func TestNonAdjProjectHasNoOpenOrDecided(t *testing.T) {
	f := newFetcher()
	c := newCache(t, f, &fakeClock{}, nil)
	readAll(t, c, beta)
	if n := f.count("beta", source.Items); n != 1 {
		t.Fatalf("beta items read %d times, want 1", n)
	}
	if f.count("beta", source.Open) != 0 || f.count("beta", source.Decided) != 0 {
		t.Fatal("a project without adj had its open or decided records read")
	}
	if v := c.Get(context.Background(), beta, source.Decided); v.Value != nil || v.Err != nil || !v.ReadAt.IsZero() {
		t.Fatalf("decided for a project without adj: %+v", v)
	}
}

func TestCloseStopsRefreshesAndWaitsForCalls(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := New(Options{Clock: clock, Fetcher: f})
	readAll(t, c, alpha)
	c.SetVisible([]string{"alpha"})
	clock.Advance(5 * time.Second)
	waitFor(t, "items read at 5 s", func() bool { return f.count("alpha", source.Items) == 2 })

	gate := make(chan struct{})
	f.setGateFor(source.Items, gate)
	clock.Advance(5 * time.Second) // this read is in flight when Close runs
	waitFor(t, "items read in flight", func() bool { return f.count("alpha", source.Items) == 3 })

	done := make(chan struct{})
	go func() {
		c.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not wait for the call in flight, or did not cancel it")
	}
	if in, _ := f.now(); in != 0 {
		t.Fatalf("%d calls still running after Close", in)
	}
	if n := clock.Pending(); n != 0 {
		t.Fatalf("%d timers still pending after Close", n)
	}
	before := f.count("alpha", source.Items) + f.count("beta", source.Items)
	clock.Advance(time.Minute)
	settle()
	if after := f.count("alpha", source.Items) + f.count("beta", source.Items); after != before {
		t.Fatalf("reads after Close: %d, want none", after-before)
	}
	c.Close() // a second Close is harmless
}

// releaseOnce closes gate once: now, when the test calls the result, or when
// the test ends. A failing test then does not leave a call blocked, which
// would hold up the cache's Close in the test's cleanup.
func releaseOnce(t *testing.T, gate chan struct{}) func() {
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return release
}

func TestGetOfAForgottenProjectReturnsAnError(t *testing.T) {
	f := newFetcher()
	gate := make(chan struct{})
	f.setGateFor(source.Items, gate)
	c := newCache(t, f, &fakeClock{}, nil)
	release := releaseOnce(t, gate)
	got := make(chan Value, 1)
	go func() { got <- c.Get(context.Background(), alpha, source.Items) }()
	waitFor(t, "items read in flight", func() bool { return f.count("alpha", source.Items) == 1 })
	c.Forget("alpha")
	release()
	select {
	case v := <-got:
		if !errors.Is(v.Err, ErrForgotten) || v.Value != nil {
			t.Fatalf("a read of a forgotten project returned %+v, want ErrForgotten and no value", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read of a forgotten project did not return")
	}
}

func TestForgetDuringAReadThenReAddKeepsOneCall(t *testing.T) {
	f := newFetcher()
	oldGate, newGate := make(chan struct{}), make(chan struct{})
	f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		if part != source.Items {
			return defaultAnswer(p, part)
		}
		gate, id := newGate, "NEW"
		if p.Path == "/p/old" {
			gate, id = oldGate, "OLD"
		}
		<-gate
		return []slicercli.Item{{ID: id}}, nil
	})
	var mu sync.Mutex
	var itemChanges int
	c := newCache(t, f, &fakeClock{}, func(project string, part source.Part) {
		if part == source.Items {
			mu.Lock()
			defer mu.Unlock()
			itemChanges++
		}
	})
	releaseOld := releaseOnce(t, oldGate)
	releaseNew := releaseOnce(t, newGate)
	oldP := registry.Project{Name: "alpha", Path: "/p/old", Adj: true}
	newP := registry.Project{Name: "alpha", Path: "/p/new", Adj: true}

	oldGot := make(chan Value, 1)
	go func() { oldGot <- c.Get(context.Background(), oldP, source.Items) }()
	waitFor(t, "old items read in flight", func() bool { return f.count("alpha", source.Items) == 1 })
	c.Forget("alpha")
	newGot := make(chan Value, 1)
	go func() { newGot <- c.Get(context.Background(), newP, source.Items) }()
	waitFor(t, "new items read in flight", func() bool { return f.count("alpha", source.Items) == 2 })

	releaseOld() // the forgotten read ends while the new project's read is in flight
	settle()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	early := c.Get(ctx, newP, source.Items) // must wait for the new read, not take the old value
	cancel()
	if early.Value != nil {
		t.Fatalf("the new project's items came from the forgotten read: %+v", early.Value)
	}
	mu.Lock()
	if itemChanges != 0 {
		t.Fatalf("the forgotten read reported %d item changes, want none", itemChanges)
	}
	mu.Unlock()

	refreshed := make(chan error, 1)
	go func() { refreshed <- c.Refresh(context.Background(), newP, source.Items) }()
	settle()
	if n := f.count("alpha", source.Items); n != 2 {
		t.Fatalf("items read %d times with the new project's read in flight, want 2 (a refresh joins it)", n)
	}
	releaseNew()
	select {
	case v := <-newGot:
		items, _ := v.Value.([]slicercli.Item)
		if len(items) != 1 || items[0].ID != "NEW" {
			t.Fatalf("the new project's items: %+v, want NEW", v.Value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the new project's read did not return")
	}
	if err := <-refreshed; err != nil {
		t.Fatal(err)
	}
	if n := f.count("alpha", source.Items); n != 2 {
		t.Fatalf("items read %d times in all, want 2", n)
	}
	select {
	case v := <-oldGot:
		if !errors.Is(v.Err, ErrForgotten) || v.Value != nil {
			t.Fatalf("the forgotten project's read returned %+v, want ErrForgotten", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the forgotten project's read did not return")
	}
}

func TestHeadChangeDuringAGoalsReadReadsGoalsAgainAfterIt(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := newCache(t, f, clock, nil)
	var mu sync.Mutex
	head := "h1"
	holding := false
	gate := make(chan struct{})
	release := releaseOnce(t, gate)
	var events []string // "start N" and "end N" for each goals read
	f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		switch part {
		case source.Head:
			mu.Lock()
			defer mu.Unlock()
			return head, nil
		case source.Goals:
			mu.Lock()
			n := 1
			for _, ev := range events {
				if ev[:5] == "start" {
					n++
				}
			}
			events = append(events, fmt.Sprintf("start %d", n))
			hold := holding
			mu.Unlock()
			if hold {
				<-gate
			}
			mu.Lock()
			events = append(events, fmt.Sprintf("end %d", n))
			mu.Unlock()
			return &slicercli.Goals{}, nil
		}
		return defaultAnswer(p, part)
	})
	c.SetVisible([]string{"alpha"}) // HEAD is read every 5 s for a visible project
	readAll(t, c, alpha)

	mu.Lock()
	holding = true
	mu.Unlock()
	go func() {
		if err := c.Refresh(context.Background(), alpha, source.Goals); err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "goals read 2 held in flight", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(events) > 0 && events[len(events)-1] == "start 2"
	})

	mu.Lock()
	head = "h2" // the commit lands while goals read 2 is in flight
	mu.Unlock()
	clock.Advance(5 * time.Second)
	waitFor(t, "HEAD read after the change", func() bool { return f.count("alpha", source.Head) == 2 })
	settle()

	mu.Lock()
	holding = false
	mu.Unlock()
	release()
	// Goals read 2 may have read the records from before the commit. A third
	// read must follow it, once read 2 has ended.
	waitFor(t, "goals read again after the HEAD change", func() bool { return f.count("alpha", source.Goals) == 3 })
	waitFor(t, "goals read 3 ended", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(events) == 6 && events[5] == "end 3"
	})
	mu.Lock()
	defer mu.Unlock()
	want := []string{"start 1", "end 1", "start 2", "end 2", "start 3", "end 3"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("goals reads ran as %v, want %v (one at a time, the third after the second)", events, want)
	}
}

func TestReadLongerThanItsIntervalStartsAgainWhenItEnds(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := newCache(t, f, clock, nil)
	var mu sync.Mutex
	holding := false
	gate := make(chan struct{})
	release := releaseOnce(t, gate)
	inFlight, maxInFlight := 0, 0
	f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		if part != source.Items {
			return defaultAnswer(p, part)
		}
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		hold := holding
		mu.Unlock()
		if hold {
			<-gate
		}
		mu.Lock()
		inFlight--
		mu.Unlock()
		return defaultAnswer(p, part)
	})
	c.SetVisible([]string{"alpha"})
	readAll(t, c, alpha) // items read 1 at 0 s

	mu.Lock()
	holding = true
	mu.Unlock()
	clock.Advance(5 * time.Second) // 5 s: read 2 is due and starts; it is held
	waitFor(t, "items read 2 in flight", func() bool { return f.count("alpha", source.Items) == 2 })
	clock.Advance(5 * time.Second) // 10 s: read 3 is due while read 2 still runs
	settle()
	if n := f.count("alpha", source.Items); n != 2 {
		t.Fatalf("items read %d times while read 2 was still running at 10 s, want 2 (it waits for read 2)", n)
	}

	release() // read 2 ends, and the due read starts then
	waitFor(t, "items read 3 once read 2 ended", func() bool { return f.count("alpha", source.Items) == 3 })
	settle()
	mu.Lock()
	defer mu.Unlock()
	if maxInFlight != 1 {
		t.Fatalf("items reads overlapped: %d ran at once, want 1", maxInFlight)
	}
}

func TestDepsRefreshesAtTheItemsInterval(t *testing.T) {
	f := newFetcher()
	f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		if part == source.Part("deps") {
			return []slicercli.Item{{ID: "ready"}}, nil
		}
		return defaultAnswer(p, part)
	})
	clock := &fakeClock{}
	c := newCache(t, f, clock, nil)
	c.SetVisible([]string{"alpha"})
	v := c.Get(context.Background(), alpha, source.Part("deps"))
	if items, ok := v.Value.([]slicercli.Item); !ok || len(items) != 1 || items[0].ID != "ready" {
		t.Fatalf("deps not cached: %+v", v)
	}
	clock.Advance(5 * time.Second)
	waitRead(t, c, f, "alpha", source.Part("deps"), 2)
}

func TestLedgerRefreshIsTwoSecondsForTheVisibleProjectWhileARunIsLive(t *testing.T) {
	f := newFetcher()
	var mu sync.Mutex
	live := true
	f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		if part == source.Runs && live {
			return []slicercli.RelayRun{{ID: "run-1", State: "running"}}, nil
		}
		return defaultAnswer(p, part)
	})
	setLive := func(v bool) {
		mu.Lock()
		defer mu.Unlock()
		live = v
	}
	clock := &fakeClock{}
	c := newCache(t, f, clock, nil)
	readAll(t, c, alpha)
	readAll(t, c, beta)
	c.SetVisible([]string{"alpha"})
	waitFor(t, "alpha goals read again on tab open", func() bool { return f.count("alpha", source.Goals) == 2 })

	// A visible project with a live run: the Runs part is read every 2 s (S336).
	clock.Advance(time.Second)
	settle()
	if n := f.count("alpha", source.Runs); n != 1 {
		t.Fatalf("runs read %d times at 1 s with a live run, want 1 (2 s)", n)
	}
	clock.Advance(time.Second)
	waitRead(t, c, f, "alpha", source.Runs, 2)
	clock.Advance(2 * time.Second)
	waitRead(t, c, f, "alpha", source.Runs, 3)
	if n := f.count("alpha", source.Items); n != 1 {
		t.Fatalf("items read at 4 s: %d reads, want 1 (only the Runs part speeds up)", n)
	}
	if n := f.count("beta", source.Runs); n != 1 {
		t.Fatalf("hidden project's runs read at 4 s: %d reads, want 1", n)
	}

	// The run ends: that read still came at 2 s, and the next waits 5 s.
	setLive(false)
	clock.Advance(2 * time.Second)
	waitRead(t, c, f, "alpha", source.Runs, 4)
	clock.Advance(4 * time.Second)
	settle()
	if n := f.count("alpha", source.Runs); n != 4 {
		t.Fatalf("runs read %d times 4 s after the last run ended, want 4 (back to 5 s)", n)
	}
	clock.Advance(time.Second)
	waitRead(t, c, f, "alpha", source.Runs, 5)

	// A run begins: the read that sees it moves the next one to 2 s.
	setLive(true)
	clock.Advance(5 * time.Second)
	waitRead(t, c, f, "alpha", source.Runs, 6)
	clock.Advance(2 * time.Second)
	waitRead(t, c, f, "alpha", source.Runs, 7)

	// The owner leaves the project: back to 30 s.
	c.SetVisible(nil)
	clock.Advance(20 * time.Second)
	settle()
	if n := f.count("alpha", source.Runs); n != 7 {
		t.Fatalf("hidden project's runs read %d times at 20 s, want 7 (30 s)", n)
	}
	clock.Advance(10 * time.Second)
	waitRead(t, c, f, "alpha", source.Runs, 8)
}

func newIdleCache(t *testing.T, f *fakeFetcher, clock *fakeClock) *Cache {
	t.Helper()
	c := New(Options{Clock: clock, Fetcher: f, Idle: true})
	t.Cleanup(c.Close)
	return c
}

// With no client attached nothing is read on a timer, however long it is
// (S336, adj 301). A read that someone asks for still happens.
func TestIdleCacheReadsNothingOnATimer(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := newIdleCache(t, f, clock)
	readAll(t, c, alpha)
	readAll(t, c, beta)
	clock.Advance(10 * time.Minute)
	settle()
	for _, p := range []registry.Project{alpha, beta} {
		for _, part := range partsOf(p) {
			if n := f.count(p.Name, part); n != 1 {
				t.Errorf("%s/%s read %d times in 10 idle minutes, want 1", p.Name, part, n)
			}
		}
	}
}

// A client that attaches gets one read of each part of the projects it shows.
// The projects it does not show wait their own interval from the attach.
func TestAttachingReadsEachPartOfTheShownProjectsOnce(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := newIdleCache(t, f, clock)
	readAll(t, c, alpha)
	readAll(t, c, beta)
	clock.Advance(10 * time.Minute)
	settle()

	c.SetIdle(false)
	c.SetVisible([]string{"alpha"})
	for _, part := range partsOf(alpha) {
		waitRead(t, c, f, "alpha", part, 2)
	}
	settle()
	for _, part := range partsOf(alpha) {
		if n := f.count("alpha", part); n != 2 {
			t.Errorf("alpha/%s read %d times on attach, want 2 (one cold, one on attach)", part, n)
		}
	}
	for _, part := range partsOf(beta) {
		if n := f.count("beta", part); n != 1 {
			t.Errorf("beta/%s read %d times on attach, want 1 (not shown)", part, n)
		}
	}
	clock.Advance(30 * time.Second)
	waitRead(t, c, f, "beta", source.Items, 2)
}

// The shown projects are read on attach whichever of SetIdle and SetVisible
// the caller makes first.
func TestAttachingReadsTheShownProjectsWhenTheyAreSetBeforeTheAttach(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := newIdleCache(t, f, clock)
	readAll(t, c, alpha)
	clock.Advance(10 * time.Minute)
	c.SetVisible([]string{"alpha"})
	c.SetIdle(false)
	for _, part := range []source.Part{source.Items, source.Deps, source.Runs, source.Open, source.Head} {
		waitRead(t, c, f, "alpha", part, 2)
	}
}

// The last client leaving makes the cache idle again.
func TestDetachingStopsTheTimers(t *testing.T) {
	f := newFetcher()
	clock := &fakeClock{}
	c := newIdleCache(t, f, clock)
	readAll(t, c, alpha)
	clock.Advance(time.Minute)
	c.SetIdle(false)
	c.SetVisible([]string{"alpha"})
	waitRead(t, c, f, "alpha", source.Items, 2)
	waitRead(t, c, f, "alpha", source.Goals, 2)
	c.SetVisible(nil)
	c.SetIdle(true)
	if n := clock.Pending(); n != 0 {
		t.Errorf("%d timers pending after the last client left, want 0", n)
	}
	clock.Advance(10 * time.Minute)
	settle()
	for _, part := range partsOf(alpha) {
		if n := f.count("alpha", part); n != 2 {
			t.Errorf("alpha/%s read %d times after the client left, want 2", part, n)
		}
	}
}

func TestLedgerRefreshIgnoresStaleRuns(t *testing.T) {
	f := newFetcher()
	f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		if part == source.Runs {
			return []slicercli.RelayRun{{ID: "run-1", State: "stale"}}, nil
		}
		return defaultAnswer(p, part)
	})
	clock := &fakeClock{}
	c := newCache(t, f, clock, nil)
	readAll(t, c, alpha)
	c.SetVisible([]string{"alpha"})
	clock.Advance(time.Second)
	settle()
	if n := f.count("alpha", source.Runs); n != 1 {
		t.Fatalf("runs read %d times at 1 s with only a stale run, want 1", n)
	}
}

// The Running entry shows every project at once (S110). Each visible project
// with a live run is read every 2 s, a visible project without one stays at
// 5 s, and the Limit still caps the processes however many are visible.
func TestLedgerRefreshIsTwoSecondsForEveryVisibleProjectWithALiveRun(t *testing.T) {
	f := newFetcher()
	live := func(p registry.Project) bool { return p.Name[len(p.Name)-1]%2 == 0 }
	f.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		if part == source.Runs && live(p) {
			return []slicercli.RelayRun{{ID: "run-" + p.Name, State: "running"}}, nil
		}
		return defaultAnswer(p, part)
	})
	clock := &fakeClock{}
	c := newCache(t, f, clock, nil)
	var projects []registry.Project
	var names []string
	for i := 10; i < 24; i++ { // 14 projects
		p := registry.Project{Name: fmt.Sprintf("p%d", i), Path: fmt.Sprintf("/p/p%d", i)}
		projects = append(projects, p)
		names = append(names, p.Name)
		readAll(t, c, p)
	}
	c.SetVisible(names)
	for _, p := range projects {
		waitFor(t, p.Name+" goals read on open", func() bool { return f.count(p.Name, source.Goals) == 2 })
	}
	clock.Advance(time.Second)
	settle()
	for _, p := range projects {
		if n := f.count(p.Name, source.Runs); n != 1 {
			t.Errorf("%s: runs read %d times after 1 s, want 1", p.Name, n)
		}
	}
	clock.Advance(time.Second)
	for _, p := range projects {
		if live(p) {
			waitRead(t, c, f, p.Name, source.Runs, 2)
		}
	}
	settle()
	for _, p := range projects {
		want := 1
		if live(p) {
			want = 2
		}
		if n := f.count(p.Name, source.Runs); n != want {
			t.Errorf("%s: runs read %d times after 2 s, want %d (live %v)", p.Name, n, want, live(p))
		}
	}
	if _, max := f.now(); max > Limit {
		t.Fatalf("%d calls ran at once with %d projects visible, limit %d", max, len(projects), Limit)
	}
}

// batchFetcher is a fakeFetcher that also reads Items, Goals and Runs in one
// call, as source.Direct does over `slicer snapshot` (poll.batch-shape). Its
// revision of a part is a hash of the part's value, and a part whose revision
// the request already holds comes back Unchanged with no value, as slicer's does.
type batchFetcher struct {
	*fakeFetcher
	bmu         sync.Mutex
	batches     [][]source.BatchRequest
	fail        error            // the whole batched call fails
	failProject map[string]error // a project that the snapshot reports an error for
}

func newBatchFetcher() *batchFetcher { return &batchFetcher{fakeFetcher: newFetcher()} }

func (b *batchFetcher) FetchBatch(ctx context.Context, reqs []source.BatchRequest) ([]source.BatchResult, error) {
	b.bmu.Lock()
	b.batches = append(b.batches, append([]source.BatchRequest(nil), reqs...))
	fail, failProject := b.fail, b.failProject
	b.bmu.Unlock()
	if fail != nil {
		return nil, fail
	}
	b.fakeFetcher.mu.Lock()
	answer := b.fakeFetcher.answer
	b.fakeFetcher.mu.Unlock()
	out := make([]source.BatchResult, len(reqs))
	for i, r := range reqs {
		if err := failProject[r.Project.Name]; err != nil {
			out[i] = source.BatchResult{Err: err}
			continue
		}
		res := source.BatchResult{Parts: map[source.Part]source.BatchPart{}}
		for _, part := range r.Parts {
			v, err := answer(r.Project, part)
			if err != nil {
				res.Parts[part] = source.BatchPart{Err: err}
				continue
			}
			j, _ := json.Marshal(v)
			rev := fmt.Sprintf("%x", sha256.Sum256(j))[:12]
			if r.Have[part] == rev {
				res.Parts[part] = source.BatchPart{Revision: rev, Unchanged: true}
			} else {
				res.Parts[part] = source.BatchPart{Revision: rev, Value: v}
			}
		}
		out[i] = res
	}
	return out, nil
}

func (b *batchFetcher) batchCount() int {
	b.bmu.Lock()
	defer b.bmu.Unlock()
	return len(b.batches)
}

func (b *batchFetcher) lastBatch() []source.BatchRequest {
	b.bmu.Lock()
	defer b.bmu.Unlock()
	if len(b.batches) == 0 {
		return nil
	}
	return b.batches[len(b.batches)-1]
}

func (b *batchFetcher) setBatchFailure(err error, projects map[string]error) {
	b.bmu.Lock()
	defer b.bmu.Unlock()
	b.fail, b.failProject = err, projects
}

func newBatchCache(t *testing.T, b *batchFetcher, clock *fakeClock, onChange func(string, source.Part)) *Cache {
	t.Helper()
	c := New(Options{Clock: clock, Fetcher: b, OnChange: onChange})
	t.Cleanup(c.Close)
	return c
}

// waitBatches waits until the fetcher has seen n batched calls and no read is
// in flight or queued. Like waitRead, it keeps a test from moving the fake clock
// on while a read still runs.
func waitBatches(t *testing.T, c *Cache, b *batchFetcher, n int) {
	t.Helper()
	waitFor(t, fmt.Sprintf("batch %d ended", n), func() bool {
		if b.batchCount() != n {
			return false
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.queue) != 0 {
			return false
		}
		for _, e := range c.entries {
			if e.flight != nil {
				return false
			}
		}
		return true
	})
}

func partNames(r source.BatchRequest) []string {
	var out []string
	for _, p := range r.Parts {
		out = append(out, string(p))
	}
	sort.Strings(out)
	return out
}

// noteLog collects the change notices of a cache.
type noteLog struct {
	mu    sync.Mutex
	notes []string
}

func (n *noteLog) add(project string, part source.Part) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notes = append(n.notes, project+"/"+string(part))
}

func (n *noteLog) take() []string {
	settle()
	n.mu.Lock()
	defer n.mu.Unlock()
	out := n.notes
	n.notes = nil
	return out
}

func TestDueItemsGoalsAndRunsOfThreeProjectsAreOneBatchedCall(t *testing.T) {
	bf := newBatchFetcher()
	clock := &fakeClock{}
	c := newBatchCache(t, bf, clock, nil)
	gamma := registry.Project{Name: "gamma", Path: "/p/gamma"}
	projects := []registry.Project{alpha, beta, gamma}
	for _, p := range projects {
		readAll(t, c, p)
	}
	before := bf.batchCount()

	clock.Advance(GoalInterval) // every part of every project is due at once
	waitBatches(t, c, bf, before+1)
	settle()
	if n := bf.batchCount(); n != before+1 {
		t.Fatalf("%d batched calls for three due projects, want 1", n-before)
	}
	last := bf.lastBatch()
	if len(last) != 3 {
		t.Fatalf("the batch has %d projects, want 3: %+v", len(last), last)
	}
	for _, r := range last {
		if got := fmt.Sprint(partNames(r)); got != "[goals items runs]" {
			t.Errorf("%s: batch parts %s, want [goals items runs]", r.Project.Name, got)
		}
	}
	for _, p := range projects {
		for _, part := range []source.Part{source.Items, source.Goals, source.Runs} {
			if n := bf.count(p.Name, part); n != 0 {
				t.Errorf("%s/%s: %d part calls, want 0 (batched)", p.Name, part, n)
			}
		}
		if n := bf.count(p.Name, source.Deps); n != 2 {
			t.Errorf("%s/deps: %d calls, want 2 (deps stays per project)", p.Name, n)
		}
	}
}

func TestAPartWhoseRevisionIsUnchangedKeepsItsValueAndGivesNoNotice(t *testing.T) {
	bf := newBatchFetcher()
	clock := &fakeClock{}
	var log noteLog
	c := newBatchCache(t, bf, clock, log.add)
	readAll(t, c, alpha)
	waitBatches(t, c, bf, 1)
	log.take()
	first := c.Get(context.Background(), alpha, source.Items)
	if first.Err != nil || first.ReadAt != epoch {
		t.Fatalf("first read: %+v", first)
	}

	clock.Advance(SlowInterval) // items and runs read again, with the same data
	waitBatches(t, c, bf, 2)
	for _, r := range bf.lastBatch() {
		if r.Have[source.Items] == "" || r.Have[source.Runs] == "" {
			t.Fatalf("the request does not hold the last revisions: %+v", r.Have)
		}
	}
	if got := log.take(); len(got) != 0 {
		t.Fatalf("an unchanged revision gave notices %v", got)
	}
	again := c.Get(context.Background(), alpha, source.Items)
	if again.Err != nil || !reflect.DeepEqual(again.Value, first.Value) || again.Value == nil {
		t.Fatalf("an unchanged part lost its value: %+v", again)
	}
	if want := epoch.Add(SlowInterval); !again.ReadAt.Equal(want) {
		t.Fatalf("ReadAt %v after an unchanged read, want %v", again.ReadAt, want)
	}

	bf.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		if part == source.Items {
			return []slicercli.Item{{ID: "S1"}, {ID: "S2"}}, nil
		}
		return defaultAnswer(p, part)
	})
	clock.Advance(SlowInterval)
	waitBatches(t, c, bf, 3)
	if got := log.take(); len(got) != 1 || got[0] != "alpha/items" {
		t.Fatalf("a changed revision gave notices %v, want [alpha/items]", got)
	}
	changed := c.Get(context.Background(), alpha, source.Items)
	if items, _ := changed.Value.([]slicercli.Item); len(items) != 2 {
		t.Fatalf("a changed part kept its old value: %+v", changed)
	}
}

func TestAnUnchangedRevisionAfterAFailedReadClearsTheError(t *testing.T) {
	bf := newBatchFetcher()
	clock := &fakeClock{}
	var log noteLog
	c := newBatchCache(t, bf, clock, log.add)
	readAll(t, c, alpha)
	waitBatches(t, c, bf, 1)
	log.take()

	bf.setBatchFailure(nil, map[string]error{"alpha": &clirun.ToolError{Code: "io", Message: "disk"}})
	clock.Advance(SlowInterval)
	waitBatches(t, c, bf, 2)
	if got := c.Get(context.Background(), alpha, source.Items); got.Err == nil || got.Value == nil {
		t.Fatalf("after a failed read: %+v", got)
	}
	log.take()

	bf.setBatchFailure(nil, nil)
	clock.Advance(SlowInterval)
	waitBatches(t, c, bf, 3)
	got := c.Get(context.Background(), alpha, source.Items)
	if got.Err != nil || got.Value == nil {
		t.Fatalf("after the recovery: %+v", got)
	}
	if notes := log.take(); !reflect.DeepEqual(notes, []string{"alpha/items", "alpha/runs"}) && !reflect.DeepEqual(notes, []string{"alpha/runs", "alpha/items"}) {
		t.Fatalf("a part that recovered gave notices %v, want items and runs", notes)
	}
}

func TestAFailedBatchedCallFallsBackToPartCallsForThatTick(t *testing.T) {
	bf := newBatchFetcher()
	clock := &fakeClock{}
	c := newBatchCache(t, bf, clock, nil)
	readAll(t, c, alpha)
	readAll(t, c, beta)
	waitBatches(t, c, bf, 2)

	bf.setBatchFailure(errors.New("slicer snapshot: exit 2"), nil)
	clock.Advance(SlowInterval)
	waitFor(t, "part calls for items and runs", func() bool {
		return bf.count("alpha", source.Items) == 1 && bf.count("alpha", source.Runs) == 1 &&
			bf.count("beta", source.Items) == 1 && bf.count("beta", source.Runs) == 1
	})
	waitBatches(t, c, bf, 3)
	if n := bf.count("alpha", source.Goals); n != 0 {
		t.Fatalf("goals was not due and was read %d times", n)
	}
	for _, p := range []registry.Project{alpha, beta} {
		for _, part := range []source.Part{source.Items, source.Runs} {
			if got := c.Get(context.Background(), p, part); got.Err != nil || !got.ReadAt.Equal(epoch.Add(SlowInterval)) {
				t.Fatalf("%s/%s after the fallback: %+v", p.Name, part, got)
			}
		}
	}

	bf.setBatchFailure(nil, nil)
	clock.Advance(SlowInterval)
	waitBatches(t, c, bf, 4)
	if n := bf.count("alpha", source.Items); n != 1 {
		t.Fatalf("the next tick made part calls again: %d", n)
	}
}

func TestASnapshotProjectErrorMarksOnlyThatProjectsPartsFailed(t *testing.T) {
	bf := newBatchFetcher()
	clock := &fakeClock{}
	var log noteLog
	c := newBatchCache(t, bf, clock, log.add)
	readAll(t, c, alpha)
	readAll(t, c, beta)
	waitBatches(t, c, bf, 2)
	log.take()
	prior := c.Get(context.Background(), beta, source.Items)

	boom := &clirun.ToolError{Code: "io", Message: "cannot read", Command: "snapshot"}
	bf.setBatchFailure(nil, map[string]error{"beta": boom})
	clock.Advance(SlowInterval)
	waitBatches(t, c, bf, 3)

	for _, part := range []source.Part{source.Items, source.Runs} {
		b := c.Get(context.Background(), beta, part)
		var te *clirun.ToolError
		if !errors.As(b.Err, &te) || te.Code != "io" || b.Value == nil || !b.ReadAt.Equal(epoch) {
			t.Fatalf("beta/%s: %+v, want its old value, its old age and the project's error", part, b)
		}
		a := c.Get(context.Background(), alpha, part)
		if a.Err != nil || a.Value == nil || !a.ReadAt.Equal(epoch.Add(SlowInterval)) {
			t.Fatalf("alpha/%s: %+v, want a good read", part, a)
		}
	}
	if got := c.Get(context.Background(), beta, source.Items); !reflect.DeepEqual(got.Value, prior.Value) {
		t.Fatalf("beta items changed: %+v", got)
	}
	if got := log.take(); len(got) != 2 {
		t.Fatalf("notices %v, want beta items and runs", got)
	}
	for _, p := range []string{"alpha", "beta"} {
		if n := bf.count(p, source.Items) + bf.count(p, source.Runs); n != 0 {
			t.Fatalf("%s: %d part calls after a project error, want 0 (no fallback)", p, n)
		}
	}
}

func TestLiveRunStateChangeIsDeliveredWithinOneBatchedTick(t *testing.T) {
	bf := newBatchFetcher()
	var mu sync.Mutex
	state := "running"
	bf.setAnswer(func(p registry.Project, part source.Part) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		if part == source.Runs {
			return []slicercli.RelayRun{{ID: "run-1", State: state}}, nil
		}
		return defaultAnswer(p, part)
	})
	clock := &fakeClock{}
	var log noteLog
	c := newBatchCache(t, bf, clock, log.add)
	readAll(t, c, alpha)
	readAll(t, c, beta)
	c.SetVisible([]string{"alpha"})
	waitFor(t, "goals read again on tab open", func() bool { return bf.batchCount() == 3 })
	waitBatches(t, c, bf, 3)
	log.take()

	mu.Lock()
	state = "waiting"
	mu.Unlock()
	clock.Advance(LiveInterval)
	waitBatches(t, c, bf, 4)
	last := bf.lastBatch()
	if len(last) != 1 || last[0].Project.Name != "alpha" || fmt.Sprint(partNames(last[0])) != "[runs]" {
		t.Fatalf("the live tick batch: %+v, want alpha's runs only", last)
	}
	if got := log.take(); len(got) != 1 || got[0] != "alpha/runs" {
		t.Fatalf("notices %v, want [alpha/runs]", got)
	}
	runs, _ := c.Get(context.Background(), alpha, source.Runs).Value.([]slicercli.RelayRun)
	if len(runs) != 1 || runs[0].State != "waiting" {
		t.Fatalf("runs after one live tick: %+v", runs)
	}
	if n := bf.count("alpha", source.Runs); n != 0 {
		t.Fatalf("runs read by a part call: %d", n)
	}
}
