// SPDX-License-Identifier: GPL-3.0-or-later

package cache

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
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
