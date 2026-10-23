// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// shortDir returns a directory whose socket path fits macOS's 104 byte limit.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ork")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if len(filepath.Join(dir, "state", "ork.sock")) > 100 {
		dir = t.TempDir()
		if len(filepath.Join(dir, "state", "ork.sock")) > 100 {
			t.Skipf("no temp directory short enough for a socket: %s", dir)
		}
	}
	return dir
}

// fakeClock fires timers when the test advances it.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeTimer

	// stopFails makes Stop return false without stopping, like a timer that
	// fired just before Stop was called.
	stopFails bool
}

type fakeTimer struct {
	c       *fakeClock
	at      time.Duration
	f       func()
	stopped bool
	fired   bool
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
	if t.c.stopFails {
		return false
	}
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

// Advance moves time forward and runs the timers that are due, in order.
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
	for _, t := range due {
		t.f()
	}
}

// stubSource answers Fetch from a map by project name, and records each call.
type stubSource struct {
	mu    sync.Mutex
	snaps map[string]source.Snapshot
	calls []string // "part project", one per call
}

func (s *stubSource) Fetch(ctx context.Context, p registry.Project, part source.Part) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, string(part)+" "+p.Name)
	snap := s.snaps[p.Name]
	switch part {
	case source.Items:
		return snap.Items.Value, snap.Items.Err
	case source.Deps:
		return snap.Deps.Value, snap.Deps.Err
	case source.Goals:
		return snap.Goals.Value, snap.Goals.Err
	case source.Runs:
		return snap.Runs.Value, snap.Runs.Err
	case source.Open:
		return snap.Open.Value, snap.Open.Err
	case source.Decided:
		return snap.Decided.Value, snap.Decided.Err
	case source.Head:
		return "h1", nil
	}
	return nil, fmt.Errorf("unknown part %s", part)
}

// put replaces a project's snapshot as the calls after it see it.
func (s *stubSource) put(name string, snap source.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snaps[name] = snap
}

func (s *stubSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

type env struct {
	dir     string
	paths   Paths
	regPath string
	clock   *fakeClock
	src     *stubSource
	fetch   source.Fetcher // when set, the daemon reads through it instead of src
	deps    *engine.Deps   // when set, the daemon's slicer calls go through it
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := shortDir(t)
	return &env{
		dir:     dir,
		paths:   PathsIn(filepath.Join(dir, "state")),
		regPath: filepath.Join(dir, "projects.json"),
		clock:   &fakeClock{},
		src:     &stubSource{snaps: map[string]source.Snapshot{}},
	}
}

func (e *env) config() Config {
	var f source.Fetcher = e.src
	if e.fetch != nil {
		f = e.fetch
	}
	return Config{Paths: e.paths, RegistryPath: e.regPath, Source: f, Clock: e.clock, Engine: e.deps}
}

func (e *env) writeRegistry(t *testing.T, idleMinutes int, projects ...registry.Project) {
	t.Helper()
	if projects == nil {
		projects = []registry.Project{}
	}
	f := &registry.File{Version: registry.Version, DaemonIdleMinutes: idleMinutes, Projects: projects}
	if err := registry.Save(e.regPath, f); err != nil {
		t.Fatal(err)
	}
}

// run is a daemon running in the background.
type run struct {
	d      *Daemon
	exited chan struct{} // closed when Run returned
	err    error         // Run's result, valid once exited is closed
	stop   context.CancelFunc
}

func (e *env) start(t *testing.T) *run {
	t.Helper()
	d := New(e.config())
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{d: d, exited: make(chan struct{}), stop: cancel}
	go func() {
		r.err = d.Run(ctx)
		close(r.exited)
	}()
	select {
	case <-d.Ready():
	case <-r.exited:
		t.Fatalf("daemon stopped before it was ready: %v", r.err)
	case <-time.After(5 * time.Second):
		t.Fatal("daemon not ready within 5s")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.exited:
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop at the end of the test")
		}
	})
	return r
}

func (r *run) waitExit(t *testing.T) error {
	t.Helper()
	select {
	case <-r.exited:
		return r.err
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not exit")
		return nil
	}
}

// stays fails the test if the daemon exits within a short wait. A daemon that
// should keep running is given time to show that it does not.
func (r *run) stays(t *testing.T, msg string) {
	t.Helper()
	select {
	case <-r.exited:
		t.Fatalf("%s (Run returned %v)", msg, r.err)
	case <-time.After(60 * time.Millisecond):
	}
}

func (e *env) dial(t *testing.T) *Client {
	t.Helper()
	c, err := Dial(context.Background(), e.paths.Socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not true within 5s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPathsFollowXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/x/state")
	p, err := DefaultPaths()
	if err != nil || p.Dir != "/x/state/ork" || p.Socket != "/x/state/ork/ork.sock" || p.Pid != "/x/state/ork/ork.pid" {
		t.Fatalf("paths: %+v, %v", p, err)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")
	p, err = DefaultPaths()
	if err != nil || p.Socket != "/home/u/.local/state/ork/ork.sock" || p.Pid != "/home/u/.local/state/ork/ork.pid" {
		t.Fatalf("paths: %+v, %v", p, err)
	}
}

func TestLiveDaemonRefusesASecondOne(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0)
	a := e.start(t)

	b := New(e.config())
	err := b.Run(context.Background())
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second daemon: %v", err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) {
		t.Fatalf("the refusal does not name the running pid: %v", err)
	}
	// The first daemon is untouched: its socket still answers.
	if _, err := e.dial(t).Projects(context.Background()); err != nil {
		t.Fatalf("first daemon stopped answering: %v", err)
	}
	a.stays(t, "first daemon exited")
}

func TestStaleSocketWithDeadPidIsReplaced(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0)
	if err := os.MkdirAll(e.paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A pid that is dead: a process that ran and was reaped.
	dead := exec.Command(os.Args[0], "-test.run=NONE")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.paths.Pid, []byte(strconv.Itoa(dead.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A stale socket: bound, then closed without being unlinked.
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: e.paths.Socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	l.Close()
	if info, err := os.Lstat(e.paths.Socket); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("no stale socket to start from: %v", err)
	}
	if c, err := net.Dial("unix", e.paths.Socket); err == nil {
		c.Close()
		t.Fatal("the stale socket answers")
	}

	r := e.start(t)
	if _, err := e.dial(t).Projects(context.Background()); err != nil {
		t.Fatalf("the replacement daemon does not answer: %v", err)
	}
	pid, err := os.ReadFile(e.paths.Pid)
	if err != nil || strings.TrimSpace(string(pid)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("pidfile holds %q, %v", pid, err)
	}
	r.stop()
	r.waitExit(t)
	if _, err := os.Lstat(e.paths.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the socket is left behind after exit: %v", err)
	}
	// A new daemon can start once the old one is gone.
	e.start(t)
}

func TestLiveSocketIsNotReplacedWhenThePidfileIsGone(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0)
	a := e.start(t)
	if err := os.Remove(e.paths.Pid); err != nil { // the lock went with the file
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := New(e.config()).Run(ctx)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second daemon with a live socket: %v", err)
	}
	if _, err := e.dial(t).Projects(context.Background()); err != nil {
		t.Fatalf("first daemon lost its socket: %v", err)
	}
	a.stays(t, "first daemon exited")
}

func TestRegularFileAtSocketPathIsNotRemoved(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0)
	os.MkdirAll(e.paths.Dir, 0o700)
	if err := os.WriteFile(e.paths.Socket, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := New(e.config()).Run(ctx)
	if err == nil {
		t.Fatal("Run replaced a regular file")
	}
	if b, _ := os.ReadFile(e.paths.Socket); string(b) != "precious" {
		t.Fatalf("the file was changed: %q", b)
	}
}

func TestSocketAndDirectoryModes(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0)
	e.start(t)
	d, err := os.Stat(e.paths.Dir)
	if err != nil || d.Mode().Perm() != 0o700 {
		t.Fatalf("state directory: %v, %v", d, err)
	}
	s, err := os.Stat(e.paths.Socket)
	if err != nil || s.Mode().Perm() != 0o600 {
		t.Fatalf("socket: %v, %v", s, err)
	}
}

func TestIdleExit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		minutes int // the registry's daemon_idle_minutes
		want    time.Duration
	}{
		{"configured", 3, 3 * time.Minute},
		{"zero means the default of 10", 0, 10 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.writeRegistry(t, tc.minutes)
			r := e.start(t)

			e.clock.Advance(tc.want - time.Second)
			r.stays(t, fmt.Sprintf("daemon exited before %v of idle time", tc.want))
			e.clock.Advance(time.Second)
			if err := r.waitExit(t); err != nil {
				t.Fatalf("idle exit returned %v", err)
			}
		})
	}
}

func TestMissingRegistryUsesTheDefaultIdle(t *testing.T) {
	e := newEnv(t) // no projects.json
	r := e.start(t)
	e.clock.Advance(10*time.Minute - time.Second)
	r.stays(t, "daemon exited early")
	e.clock.Advance(time.Second)
	r.waitExit(t)
}

func TestConnectedClientHoldsOffIdleAndResetsTheTimer(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 1)
	r := e.start(t)

	e.clock.Advance(50 * time.Second) // 50 s of the first minute are used up
	c := e.dial(t)
	if _, err := c.Projects(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "idle timer stopped while connected", func() bool { return e.clock.Pending() == 0 })

	e.clock.Advance(time.Hour)
	r.stays(t, "daemon exited with a client connected")
	if _, err := c.Projects(context.Background()); err != nil {
		t.Fatalf("connected client lost its daemon: %v", err)
	}

	c.Close()
	waitFor(t, "idle timer restarted on disconnect", func() bool { return e.clock.Pending() == 1 })
	e.clock.Advance(59 * time.Second) // the 50 s before the client do not count
	r.stays(t, "daemon exited before a full idle period after the last client left")
	e.clock.Advance(time.Second)
	r.waitExit(t)
}

func TestAStaleTimerDoesNotEndTheIdlePeriodEarly(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 1)
	e.clock.stopFails = true // the first timer cannot be stopped and still fires at 60 s
	r := e.start(t)

	e.clock.Advance(30 * time.Second)
	c := e.dial(t)
	if _, err := c.Projects(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Close()
	waitFor(t, "second idle timer armed", func() bool { return e.clock.Pending() == 2 })

	e.clock.Advance(30 * time.Second) // 60 s: the stale timer fires, 30 s after the client left
	time.Sleep(100 * time.Millisecond)
	r.stays(t, "a stale timer ended the idle period early")
	e.clock.Advance(30 * time.Second) // 90 s: a full minute after the client left
	r.waitExit(t)
}

func TestRunningWaveHoldsOffIdle(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 1)
	var mu sync.Mutex
	wave := true
	cfg := e.config()
	cfg.WaveRunning = func() bool { mu.Lock(); defer mu.Unlock(); return wave }
	d := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case <-d.Ready():
	case err := <-done:
		t.Fatalf("daemon stopped before it was ready: %v", err)
	}

	e.clock.Advance(time.Minute) // the timer fires, finds a wave, and waits again
	select {
	case err := <-done:
		t.Fatalf("daemon exited while a wave runs: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	waitFor(t, "idle timer re-armed", func() bool { return e.clock.Pending() == 1 })
	mu.Lock()
	wave = false
	mu.Unlock()
	e.clock.Advance(time.Minute)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not exit once the wave ended")
	}
}

func TestContextCancelStopsTheDaemon(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0)
	r := e.start(t)
	r.stop()
	if err := r.waitExit(t); err != nil {
		t.Fatalf("Run on cancel returned %v", err)
	}
}

// ---- handlers ----

func twoProjects(t *testing.T, e *env) {
	t.Helper()
	e.writeRegistry(t, 0,
		registry.Project{Name: "alpha", Path: "/p/alpha", Adj: true},
		registry.Project{Name: "beta", Path: "/p/beta"},
	)
}

func TestStatusListsProjectsWithNeedsYouAndActiveRelays(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.src.snaps["alpha"] = source.Snapshot{
		Open: source.Result[[]adjcli.Record]{Value: make([]adjcli.Record, 3)},
		Runs: source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{
			{ID: "run-1", Items: []string{"S03"}, State: "running", Worktree: "s03", BeganAt: "b", UpdatedAt: "u",
				Counts: slicercli.Counts{Steps: 4, Running: 1}},
		}},
	}
	e.src.snaps["beta"] = source.Snapshot{}
	e.start(t)
	c := e.dial(t)

	all, err := c.Status(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Projects) != 2 || all.Projects[0].Name != "alpha" || all.Projects[1].Name != "beta" {
		t.Fatalf("projects: %+v", all.Projects)
	}
	a, b := all.Projects[0], all.Projects[1]
	if a.NeedsYou != 3 || !a.Adj || a.Path != "/p/alpha" {
		t.Fatalf("alpha: %+v", a)
	}
	want := Relay{ID: "run-1", Items: []string{"S03"}, State: "running", Worktree: "s03", BeganAt: "b", UpdatedAt: "u", Steps: 4, Running: 1}
	if len(a.ActiveRelays) != 1 || fmt.Sprint(a.ActiveRelays[0]) != fmt.Sprint(want) {
		t.Fatalf("alpha relays: %+v", a.ActiveRelays)
	}
	if b.NeedsYou != 0 || b.ActiveRelays == nil || len(b.ActiveRelays) != 0 {
		t.Fatalf("beta: %+v", b)
	}

	one, err := c.Status(context.Background(), "beta")
	if err != nil || len(one.Projects) != 1 || one.Projects[0].Name != "beta" {
		t.Fatalf("status --project beta: %+v, %v", one, err)
	}
	// The parts were read by the first status; the next status reads the cache.
	settle()
	before := e.src.callCount()
	if _, err := c.Status(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	settle()
	if after := e.src.callCount(); after != before {
		t.Fatalf("a status made %d CLI calls with the parts cached, want none", after-before)
	}

	var re *ipc.Error
	if _, err := c.Status(context.Background(), "nope"); !errors.As(err, &re) || re.Code != CodeNotFound || !strings.Contains(re.Message, "nope") {
		t.Fatalf("unknown project: %v", err)
	}
}

func TestStatusReportsPartErrorsWithToolCodes(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	toolErr := &clirun.ExitError{Tool: &clirun.ToolError{Code: "no_such_item", Message: "no such item: NOPE", Command: "show"}}
	e.src.snaps["alpha"] = source.Snapshot{
		Items: source.Result[[]slicercli.Item]{Err: fmt.Errorf("list: %w", toolErr)},
		Open:  source.Result[[]adjcli.Record]{Err: errors.New("adj exploded")},
	}
	e.start(t)
	st, err := e.dial(t).Status(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	got := st.Projects[0].Errors
	want := []PartError{
		{Part: "items", Code: "no_such_item", Message: "no such item: NOPE"},
		{Part: "open", Code: "internal", Message: "adj exploded"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("errors: %+v, want %+v", got, want)
	}
}

func TestSnapshotOverTheSocketKeepsValuesAndToolErrorCodes(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	toolErr := &clirun.ExitError{Tool: &clirun.ToolError{Code: "no_such_item", Message: "no such item: NOPE", Command: "show"}}
	e.src.snaps["alpha"] = source.Snapshot{
		Items:   source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S03", Title: "Daemon"}}},
		Deps:    source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S03"}}},
		Goals:   source.Result[*slicercli.Goals]{Err: fmt.Errorf("goals: %w", toolErr)},
		Runs:    source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{{ID: "run-1"}}},
		Decided: source.Result[[]adjcli.Record]{Err: toolErr},
	}
	e.start(t)
	c := e.dial(t)

	snap := c.Snapshot(context.Background(), registry.Project{Name: "alpha", Path: "/ignored"})
	if snap.Project.Name != "alpha" || snap.Project.Path != "/p/alpha" {
		t.Fatalf("project: %+v", snap.Project)
	}
	if snap.Items.Err != nil || len(snap.Items.Value) != 1 || snap.Items.Value[0].ID != "S03" {
		t.Fatalf("items: %+v", snap.Items)
	}
	if len(snap.Deps.Value) != 1 || snap.Deps.Value[0].ID != "S03" || snap.Deps.Err != nil {
		t.Fatalf("deps: %+v", snap.Deps)
	}
	if len(snap.Runs.Value) != 1 || snap.Runs.Err != nil {
		t.Fatalf("runs: %+v", snap.Runs)
	}
	for name, err := range map[string]error{"goals": snap.Goals.Err, "decided": snap.Decided.Err} {
		var re *ipc.Error
		if !errors.As(err, &re) || re.Code != "no_such_item" || re.Message != "no such item: NOPE" {
			t.Errorf("%s error: %T %v", name, err, err)
		}
	}
	if snap.Goals.Value != nil || snap.Open.Err != nil {
		t.Fatalf("goals value %v, open error %v", snap.Goals.Value, snap.Open.Err)
	}

	missing := c.Snapshot(context.Background(), registry.Project{Name: "gone"})
	var re *ipc.Error
	if !errors.As(missing.Items.Err, &re) || re.Code != CodeNotFound || !errors.As(missing.Decided.Err, &re) {
		t.Fatalf("a snapshot of an unknown project: %+v", missing)
	}
}

func TestProjectsAddListRemoveSaveTheRegistry(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 7, registry.Project{Name: "keep", Path: "/p/keep"})
	withAdj := filepath.Join(e.dir, "with-adj")
	plain := filepath.Join(e.dir, "plain")
	for _, d := range []string{withAdj, plain} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(withAdj, ".adjudicate.json"), []byte("{}"), 0o644)
	e.start(t)
	c := e.dial(t)
	ctx := context.Background()

	p, err := c.AddProject(ctx, withAdj, "")
	if err != nil || p.Name != "with-adj" || p.Path != withAdj || !p.Adj {
		t.Fatalf("add with .adjudicate.json: %+v, %v", p, err)
	}
	p, err = c.AddProject(ctx, plain, "named")
	if err != nil || p.Name != "named" || p.Adj {
		t.Fatalf("add with --name: %+v, %v", p, err)
	}

	saved, err := registry.Load(e.regPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != 2 || saved.DaemonIdleMinutes != 7 || len(saved.Projects) != 3 || saved.Projects[1].Name != "with-adj" || saved.Projects[2].Name != "named" {
		t.Fatalf("saved registry: %+v", saved)
	}
	list, err := c.Projects(ctx)
	if err != nil || len(list) != 3 || list[0].Name != "keep" {
		t.Fatalf("list: %+v, %v", list, err)
	}

	var re *ipc.Error
	if _, err := c.AddProject(ctx, plain, "named"); !errors.As(err, &re) || re.Code != CodeDuplicateName || !strings.Contains(re.Message, "named") {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := c.AddProject(ctx, "relative/dir", ""); !errors.As(err, &re) || re.Code != CodeInvalidPath || !strings.Contains(re.Message, "relative/dir") {
		t.Fatalf("relative path: %v", err)
	}
	if _, err := c.AddProject(ctx, "/", ""); !errors.As(err, &re) || re.Code != CodeInvalidPath {
		t.Fatalf("path with no base name: %v", err)
	}
	missing := filepath.Join(e.dir, "missing")
	if _, err := c.AddProject(ctx, missing, ""); !errors.As(err, &re) || re.Code != CodeInvalidPath || !strings.Contains(re.Message, missing) {
		t.Fatalf("path that does not exist: %v", err)
	}
	file := filepath.Join(withAdj, ".adjudicate.json")
	if _, err := c.AddProject(ctx, file, ""); !errors.As(err, &re) || re.Code != CodeInvalidPath {
		t.Fatalf("path that is a file: %v", err)
	}
	if again, _ := registry.Load(e.regPath); len(again.Projects) != 3 {
		t.Fatalf("a refused add changed the registry: %+v", again.Projects)
	}

	if err := c.RemoveProject(ctx, "named"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveProject(ctx, "named"); !errors.As(err, &re) || re.Code != CodeNotFound || !strings.Contains(re.Message, "named") {
		t.Fatalf("remove twice: %v", err)
	}
	after, _ := registry.Load(e.regPath)
	if len(after.Projects) != 2 || after.Projects[1].Name != "with-adj" {
		t.Fatalf("after rm: %+v", after.Projects)
	}
}

func TestConcurrentAddsAreAllKept(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0)
	e.start(t)
	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := Dial(context.Background(), e.paths.Socket)
			if err != nil {
				t.Error(err)
				return
			}
			defer c.Close()
			dir := filepath.Join(e.dir, fmt.Sprintf("proj%d", i))
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Error(err)
				return
			}
			if _, err := c.AddProject(context.Background(), dir, ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	saved, err := registry.Load(e.regPath)
	if err != nil || len(saved.Projects) != n {
		t.Fatalf("saved %d projects, want %d: %v", len(saved.Projects), n, err)
	}
}

func TestMissingRegistryIsEmptyAndNeverWrittenByReads(t *testing.T) {
	e := newEnv(t)
	e.regPath = filepath.Join(e.dir, "missing", "projects.json")
	e.start(t)
	c := e.dial(t)
	list, err := c.Projects(context.Background())
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("projects: %+v, %v", list, err)
	}
	status, err := c.Status(context.Background(), "")
	if err != nil || len(status.Projects) != 0 {
		t.Fatalf("status: %+v, %v", status, err)
	}
	var snapshot SnapshotResult
	err = c.Call(context.Background(), MethodSnapshot, SnapshotParams{Project: "missing"}, &snapshot)
	if ipc.ErrorOf(err).Code != CodeNotFound {
		t.Fatalf("snapshot: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(e.regPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("registry directory created: %v", err)
	}
}

func TestBrokenRegistryIsAnErrorNotAnExit(t *testing.T) {
	e := newEnv(t)
	os.WriteFile(e.regPath, []byte(`{"version": 2, "projects": [], "bogus": 1}`), 0o644)
	r := e.start(t)
	var re *ipc.Error
	_, err := e.dial(t).Projects(context.Background())
	if !errors.As(err, &re) || re.Code != CodeRegistry || !strings.Contains(re.Message, "bogus") {
		t.Fatalf("broken registry: %v", err)
	}
	r.stays(t, "daemon exited on a broken registry")
}

func TestUnknownMethodAndBadParams(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0)
	e.start(t)
	raw, err := ipc.Dial(context.Background(), e.paths.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var re *ipc.Error
	if err := raw.Call(context.Background(), "wave.nope", nil, nil); !errors.As(err, &re) || re.Code != CodeUnknownMethod || !strings.Contains(re.Message, "wave.nope") {
		t.Fatalf("unknown method: %v", err)
	}
	if err := raw.Call(context.Background(), MethodProjectsAdd, []int{1}, nil); !errors.As(err, &re) || re.Code != CodeBadParams {
		t.Fatalf("bad params: %v", err)
	}
	if err := raw.Call(context.Background(), MethodProjectsAdd, map[string]any{"path": "/p/x", "nme": "typo"}, nil); !errors.As(err, &re) || re.Code != CodeBadParams || !strings.Contains(re.Message, "nme") {
		t.Fatalf("unknown param key: %v", err)
	}
}

// ---- the snapshot cache, subscriptions and notices (S15) ----

func TestMain(m *testing.M) {
	blockedGitMain()
	clitest.Main()
	os.Exit(m.Run())
}

var epoch = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// Now is the fake clock's time: epoch plus the time advanced so far.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return epoch.Add(c.now)
}

// settle gives calls that were started a moment to run.
func settle() { time.Sleep(50 * time.Millisecond) }

// notices reads a subscribed connection's notices into a channel until the
// test ends.
func notices(t *testing.T, c *Client) <-chan SnapshotChanged {
	t.Helper()
	ch := make(chan SnapshotChanged, 100)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		for {
			n, err := c.Next(ctx)
			if err != nil {
				return
			}
			ch <- n
		}
	}()
	return ch
}

// advanceFiveSeconds moves the fake clock on by 5 s and lets the calls it
// started run.
func advanceFiveSeconds(e *env) {
	e.clock.Advance(5 * time.Second)
	settle()
}

var alphaItems = source.Snapshot{
	Items:   source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S1", Title: "one"}}},
	Goals:   source.Result[*slicercli.Goals]{Value: &slicercli.Goals{}},
	Runs:    source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{}},
	Open:    source.Result[[]adjcli.Record]{Value: []adjcli.Record{}},
	Decided: source.Result[[]adjcli.Record]{Value: []adjcli.Record{}},
}

func TestTwoClientsReadingAtOnceMakeOneCallPerPart(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0, registry.Project{Name: "alpha", Path: "/p/alpha", Adj: true})
	const root = "/p/alpha"
	fake := clitest.Install(t,
		clitest.Route{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", root}, Stdout: "[]"},
		clitest.Route{Tool: "slicer", Args: []string{"deps", "--json", "--lean", "--root", root}, Stdout: "[]"},
		clitest.Route{Tool: "slicer", Args: []string{"goals", "--json", "--root", root}, Stdout: "{}"},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "list", "--json", "--lean", "--active", "--root", root}, Stdout: `{"runs":[]}`},
		clitest.Route{Tool: "adj", Args: []string{"inbox", "--json", "--root", root}, Stdout: `{"records":[]}`},
		clitest.Route{Tool: "adj", Args: []string{"list", "--json", "--status", "decided", "--root", root}, Stdout: `{"records":[]}`},
		clitest.Route{Tool: "git", Args: []string{"-C", root, "rev-parse", "HEAD"}, Stdout: "h1\n"},
	)
	direct := source.NewDirect(slicercli.Client{Bin: fake.Bin("slicer")}, adjcli.Client{Bin: fake.Bin("adj")})
	direct.Git = fake.Bin("git")
	e.fetch = direct
	e.start(t)

	c1, c2 := e.dial(t), e.dial(t)
	var wg sync.WaitGroup
	for _, c := range []*Client{c1, c2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Snapshot(context.Background(), registry.Project{Name: "alpha"})
		}()
	}
	wg.Wait()
	waitFor(t, "git HEAD read", func() bool { return len(fake.CallsTo("git")) == 1 })
	settle()
	for _, tc := range []struct {
		tool  string
		verbs int
	}{{"slicer", 4}, {"adj", 2}, {"git", 1}} {
		if n := len(fake.CallsTo(tc.tool)); n != tc.verbs {
			t.Errorf("%s calls for two snapshots: %d, want %d (one per part)", tc.tool, n, tc.verbs)
		}
	}
}

func TestVisibleProjectRefreshesEvery5sAndOthersEvery30s(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.src.put("alpha", alphaItems)
	e.src.put("beta", alphaItems)
	r := e.start(t)
	c := e.dial(t)
	ctx := context.Background()
	c.Snapshot(ctx, registry.Project{Name: "alpha"})
	c.Snapshot(ctx, registry.Project{Name: "beta"})

	sub := e.dial(t)
	if err := sub.Subscribe(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "alpha goals read on tab open", func() bool { return e.src.count("goals alpha") == 2 })

	for step := 1; step <= 6; step++ { // 5 s to 30 s
		e.clock.Advance(5 * time.Second)
		want := step + 1 // one cold read, then one per 5 s step
		waitFor(t, fmt.Sprintf("visible items read %d times", want), func() bool {
			return e.src.count("items alpha") == want
		})
		// The read of this tick must end before the clock moves on: a read still
		// running then makes the next due read pending, and that one is skipped.
		at := epoch.Add(time.Duration(5*step) * time.Second)
		waitFor(t, "items read at this tick ended", func() bool {
			return c.Snapshot(ctx, registry.Project{Name: "alpha"}).Items.ReadAt.Equal(at)
		})
	}
	waitFor(t, "other project's items read twice by 30 s", func() bool { return e.src.count("items beta") == 2 })

	// The subscriber leaves: alpha is no longer visible, so it goes to 30 s.
	sub.Close()
	waitFor(t, "the subscriber's projects dropped", func() bool {
		r.d.subsMu.Lock()
		defer r.d.subsMu.Unlock()
		return len(r.d.focus) == 0
	})
	for step := 0; step < 5; step++ { // 35 s to 55 s: nothing is read
		advanceFiveSeconds(e)
	}
	if n := e.src.count("items alpha"); n != 7 {
		t.Fatalf("items read 25 s after the subscriber left: %d reads, want 7", n)
	}
	e.clock.Advance(5 * time.Second) // 60 s
	waitFor(t, "items read 30 s after the subscriber left", func() bool {
		return e.src.count("items alpha") == 8
	})
}

func TestSubscriberGetsANoticeOnlyWhenAPartChanges(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.src.put("alpha", alphaItems)
	e.start(t)
	e.dial(t).Snapshot(context.Background(), registry.Project{Name: "alpha"})

	sub := e.dial(t)
	if err := sub.Subscribe(context.Background(), []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	ch := notices(t, sub)
	settle()
	select {
	case n := <-ch:
		t.Fatalf("a notice for a subscription with nothing changed: %+v", n)
	default:
	}

	changed := alphaItems
	changed.Items = source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S1"}, {ID: "S2"}}}
	e.src.put("alpha", changed)
	advanceFiveSeconds(e)
	select {
	case n := <-ch:
		if n.Project != "alpha" || len(n.Parts) != 1 || n.Parts[0] != "items" {
			t.Fatalf("notice: %+v, want alpha's items", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no notice when the items changed")
	}

	advanceFiveSeconds(e) // the same items again
	select {
	case n := <-ch:
		t.Fatalf("a notice for unchanged items: %+v", n)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStaleValueKeepsTheLastGoodValueWithItsErrorAndAge(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.src.put("alpha", alphaItems)
	e.start(t)
	c := e.dial(t)
	ctx := context.Background()
	c.Snapshot(ctx, registry.Project{Name: "alpha"})
	if err := e.dial(t).Subscribe(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}

	failing := alphaItems
	failing.Items = source.Result[[]slicercli.Item]{Err: &clirun.ToolError{Code: "io", Message: "disk gone"}}
	e.src.put("alpha", failing)
	e.clock.Advance(5 * time.Second) // the items are read again at 5 s, and fail
	waitFor(t, "items failed at 5 s", func() bool {
		st, err := c.Status(ctx, "alpha")
		return err == nil && st.Projects[0].Parts["items"].Stale
	})

	snap := c.Snapshot(ctx, registry.Project{Name: "alpha"})
	if len(snap.Items.Value) != 1 || snap.Items.Value[0].ID != "S1" {
		t.Fatalf("the last good items were lost: %+v", snap.Items)
	}
	var re *ipc.Error
	if !errors.As(snap.Items.Err, &re) || re.Code != "io" {
		t.Fatalf("items error: %v", snap.Items.Err)
	}
	if !snap.Items.Stale() || !snap.Items.ReadAt.Equal(epoch) {
		t.Fatalf("items stale %v, read at %v; want stale, read at %v", snap.Items.Stale(), snap.Items.ReadAt, epoch)
	}

	st, err := c.Status(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	items := st.Projects[0].Parts["items"]
	if !items.Stale || items.AgeMs != 5000 || items.Error == nil || items.Error.Code != "io" {
		t.Fatalf("status items: %+v, want stale, 5000 ms old, code io", items)
	}
}

func TestRefreshReadsEveryPartNowAndUnknownProjectsAreRefused(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.src.put("alpha", alphaItems)
	e.start(t)
	c := e.dial(t)
	ctx := context.Background()
	c.Snapshot(ctx, registry.Project{Name: "alpha"})

	e.src.put("alpha", source.Snapshot{Items: source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S2"}}}})
	if err := c.Refresh(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	if n := e.src.count("items alpha"); n != 2 {
		t.Fatalf("items read %d times after an explicit refresh, want 2", n)
	}
	snap := c.Snapshot(ctx, registry.Project{Name: "alpha"})
	if len(snap.Items.Value) != 1 || snap.Items.Value[0].ID != "S2" {
		t.Fatalf("items after refresh: %+v", snap.Items.Value)
	}

	var re *ipc.Error
	if err := c.Refresh(ctx, "gone"); !errors.As(err, &re) || re.Code != CodeNotFound {
		t.Fatalf("refresh of an unknown project: %v", err)
	}
	if err := e.dial(t).Subscribe(ctx, []string{"gone"}); !errors.As(err, &re) || re.Code != CodeNotFound {
		t.Fatalf("subscribe to an unknown project: %v", err)
	}
}

func TestShutdownStopsRefreshesAndCalls(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.src.put("alpha", alphaItems)
	r := e.start(t)
	sub := e.dial(t)
	if err := sub.Subscribe(context.Background(), []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	settle()
	r.stop()
	r.waitExit(t)
	if n := e.clock.Pending(); n != 0 {
		t.Fatalf("%d timers still pending after the daemon exited", n)
	}
	before := e.src.callCount()
	e.clock.Advance(time.Minute)
	settle()
	if after := e.src.callCount(); after != before {
		t.Fatalf("%d calls after the daemon exited, want none", after-before)
	}
}

// count returns how many calls had the text "part project".
func (s *stubSource) count(call string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		if c == call {
			n++
		}
	}
	return n
}

func TestRemovedProjectIsNoLongerRead(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.src.put("beta", alphaItems)
	e.start(t)
	c := e.dial(t)
	ctx := context.Background()
	c.Snapshot(ctx, registry.Project{Name: "beta"})
	if err := c.RemoveProject(ctx, "beta"); err != nil {
		t.Fatal(err)
	}
	settle()
	before := e.src.count("items beta")
	e.clock.Advance(time.Minute)
	settle()
	if after := e.src.count("items beta"); after != before {
		t.Fatalf("a removed project was read %d more times, want none", after-before)
	}
}

func TestAgeOfAPartReadJustNowIsOnTheWire(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.src.put("alpha", alphaItems)
	e.start(t)
	c := e.dial(t)
	ctx := context.Background()
	c.Snapshot(ctx, registry.Project{Name: "alpha"})
	st, err := c.Status(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	// A value 0 ms old is still read: its age must be on the wire, not omitted
	// the way a part that was never read is.
	b, err := json.Marshal(st.Projects[0].Parts["items"])
	if err != nil || !strings.Contains(string(b), `"age_ms":0`) {
		t.Fatalf("a part read at this instant has no age_ms on the wire: %s, %v", b, err)
	}
}

// ---- wave reads for the Waves screen (S81) ----

// waveRoot is the path the wave read tests register project "p" at.
const waveRoot = "/p/waves"

// readDaemon is runDaemon for the wave reads: project "p" at waveRoot, and a
// fake slicer that answers the given routes. The fake is lastFake.
func readDaemon(t *testing.T, routes ...clitest.Route) *Daemon {
	t.Helper()
	dir := t.TempDir()
	reg := filepath.Join(dir, "projects.json")
	if err := registry.Save(reg, &registry.File{Version: registry.Version, Projects: []registry.Project{{Name: "p", Path: waveRoot}}}); err != nil {
		t.Fatal(err)
	}
	fake := clitest.Install(t, routes...)
	lastFake = fake
	deps := engine.Deps{Slicer: slicercli.Client{Bin: fake.Bin("slicer")}}
	d := New(Config{Paths: PathsIn(filepath.Join(dir, "state")), RegistryPath: reg, Source: &stubSource{}, Engine: &deps})
	t.Cleanup(d.cache.Close)
	t.Cleanup(d.closeRuns)
	return d
}

// recordedSlicer is the path of a recorded slicer answer.
func recordedSlicer(name string) string {
	return filepath.Join("..", "..", "testdata", "cli", "slicer", name)
}

func TestWaveSuggestReturnsPlanParallel(t *testing.T) {
	d := readDaemon(t, clitest.Route{Tool: "slicer", Args: []string{"plan-parallel", "--json", "--lean", "--root", waveRoot}, File: recordedSlicer("plan-parallel.json")})
	var got, want slicercli.ParallelPlan
	if err := json.Unmarshal(request(t, d, "wave.suggest", `{"project":"p"}`), &got); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(recordedSlicer("plan-parallel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || got.Width != 4 || len(got.Waves) != 14 {
		t.Fatalf("suggest: %+v, want %+v", got, want)
	}
	if calls := lastFake.CallsTo("slicer"); len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestWaveSuggestKeepsSlicerErrorCode(t *testing.T) {
	d := readDaemon(t, clitest.Route{Tool: "slicer", Args: []string{"plan-parallel", "--json", "--lean", "--root", waveRoot}, Exit: 2, File: recordedSlicer("plan-parallel-error.json")})
	_, err := d.handle(context.Background(), "wave.suggest", json.RawMessage(`{"project":"p"}`))
	raw, rerr := os.ReadFile(recordedSlicer("plan-parallel-error.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	var recorded struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatal(err)
	}
	got := ipc.ErrorOf(err)
	if got == nil || got.Code != "usage" || got.Code != recorded.Error.Code || got.Message != recorded.Error.Message {
		t.Fatalf("error: %+v, want code %q and message %q", got, recorded.Error.Code, recorded.Error.Message)
	}
}

func TestWaveOverlapReturnsSharedPaths(t *testing.T) {
	d := readDaemon(t, clitest.Route{Tool: "slicer", Args: []string{"deps", "--overlap", "S60", "S52", "--json", "--lean", "--root", waveRoot}, File: recordedSlicer("deps-overlap.json")})
	var got struct {
		Overlap []struct {
			A      string   `json:"a"`
			B      string   `json:"b"`
			Shared []string `json:"shared"`
		} `json:"overlap"`
	}
	if err := json.Unmarshal(request(t, d, "wave.overlap", `{"project":"p","items":["S60","S52"]}`), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Overlap) != 1 || got.Overlap[0].A != "S60" || got.Overlap[0].B != "S52" || !reflect.DeepEqual(got.Overlap[0].Shared, []string{"internal/cli/run.go", "docs/dashboard.md"}) {
		t.Fatalf("overlap: %+v", got)
	}
	if calls := lastFake.CallsTo("slicer"); len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestWaveOverlapNeedsTwoItems(t *testing.T) {
	d := readDaemon(t)
	for _, raw := range []string{`{"project":"p"}`, `{"project":"p","items":[]}`, `{"project":"p","items":["S60"]}`} {
		_, err := d.handle(context.Background(), "wave.overlap", json.RawMessage(raw))
		var re *ipc.Error
		if !errors.As(err, &re) || re.Code != CodeBadParams {
			t.Fatalf("%s: error %v, want bad_params", raw, err)
		}
	}
	if calls := lastFake.CallsTo("slicer"); len(calls) != 0 {
		t.Fatalf("slicer was called: %+v", calls)
	}
}

// The client methods over the socket: the codes and the tool's message come
// back as the wire carries them, and each request runs slicer again.
func TestWaveReadsOverTheSocket(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0, registry.Project{Name: "p", Path: waveRoot}, registry.Project{Name: "q", Path: "/p/q"})
	fake := clitest.Install(t,
		clitest.Route{Tool: "slicer", Args: []string{"plan-parallel", "--json", "--lean", "--root", waveRoot}, File: recordedSlicer("plan-parallel.json")},
		clitest.Route{Tool: "slicer", Args: []string{"plan-parallel", "--json", "--lean", "--root", "/p/q"}, Exit: 2, File: recordedSlicer("plan-parallel-error.json")},
		clitest.Route{Tool: "slicer", Args: []string{"deps", "--overlap", "S60", "S52", "--json", "--lean", "--root", waveRoot}, File: recordedSlicer("deps-overlap.json")},
	)
	e.deps = &engine.Deps{Slicer: slicercli.Client{Bin: fake.Bin("slicer")}}
	e.start(t)
	c := e.dial(t)
	ctx := context.Background()

	plan, err := c.WaveSuggest(ctx, "p")
	if err != nil || plan.Width != 4 || len(plan.Waves) != 14 {
		t.Fatalf("suggest: %+v, %v", plan, err)
	}
	if _, err := c.WaveSuggest(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 2 {
		t.Fatalf("two suggest requests made %d slicer calls, want 2: %+v", len(calls), calls)
	}
	ov, err := c.WaveOverlap(ctx, "p", []string{"S60", "S52"})
	if err != nil || len(ov.Overlap) != 1 || !reflect.DeepEqual(ov.Overlap[0].Shared, []string{"internal/cli/run.go", "docs/dashboard.md"}) {
		t.Fatalf("overlap: %+v, %v", ov, err)
	}

	var re *ipc.Error
	if _, err := c.WaveSuggest(ctx, "q"); !errors.As(err, &re) || re.Code != "usage" ||
		re.Message != "plan-parallel needs files_section: name the section that lists an item's files with `slicer config files_section NAME`" {
		t.Fatalf("tool error over the socket: %v", err)
	}
	if _, err := c.WaveSuggest(ctx, "nope"); !errors.As(err, &re) || re.Code != CodeNotFound {
		t.Fatalf("unknown project: %v", err)
	}
	if _, err := c.WaveOverlap(ctx, "p", []string{"S60"}); !errors.As(err, &re) || re.Code != CodeBadParams {
		t.Fatalf("one item: %v", err)
	}
}

func TestWaveReadsUnknownProject(t *testing.T) {
	d := readDaemon(t)
	for _, tc := range []struct{ method, raw string }{
		{"wave.suggest", `{"project":"nope"}`},
		{"wave.overlap", `{"project":"nope","items":["S60","S52"]}`},
	} {
		_, err := d.handle(context.Background(), tc.method, json.RawMessage(tc.raw))
		var re *ipc.Error
		if !errors.As(err, &re) || re.Code != CodeNotFound || !strings.Contains(re.Message, "nope") {
			t.Fatalf("%s: error %v, want not_found", tc.method, err)
		}
	}
	if calls := lastFake.CallsTo("slicer"); len(calls) != 0 {
		t.Fatalf("slicer was called: %+v", calls)
	}
}

func TestProjectStatusRelaySteps(t *testing.T) {
	steps := []slicercli.Step{
		{Role: "orchestrator", Vendor: "claude", Model: "sonnet", Phase: "start", Task: "Run relay"},
		{Role: "implementer", Vendor: "codex", Model: "gpt-6.1", Phase: "start", Task: "Implement S30"},
		{Role: "implementer", Phase: "fail", Task: "Earlier failure"},
		{Role: "reviewer", Vendor: "claude", Model: "opus", Phase: "fail", Task: "Review S30"},
		{Role: "gate", Phase: "done", Task: "Finished gate"},
	}
	ps := projectStatus(registry.Project{Name: "ork"}, source.Snapshot{
		Runs: source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{{ID: "ledger", Steps: steps}}},
	}, time.Now())
	raw, err := json.Marshal(ps.ActiveRelays)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"running_step":{"role":"implementer","vendor":"codex","model":"gpt-6.1","task":"Implement S30","phase":"start"}`,
		`"failed_step":{"role":"reviewer","vendor":"claude","model":"opus","task":"Review S30","phase":"fail"}`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("relay missing %s: %s", want, raw)
		}
	}
}
