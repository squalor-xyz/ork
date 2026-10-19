// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clirun"
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

// stubSource answers Snapshot from a map by project name.
type stubSource struct {
	mu    sync.Mutex
	snaps map[string]source.Snapshot
	calls []string
}

func (s *stubSource) Snapshot(ctx context.Context, p registry.Project) source.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, p.Name)
	snap := s.snaps[p.Name]
	snap.Project = p
	return snap
}

type env struct {
	dir     string
	paths   Paths
	regPath string
	clock   *fakeClock
	src     *stubSource
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
	return Config{Paths: e.paths, RegistryPath: e.regPath, SeedRoot: filepath.Join(e.dir, "seed"), Source: e.src, Clock: e.clock}
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
	e.src.mu.Lock()
	calls := len(e.src.calls)
	e.src.mu.Unlock()
	if calls != 3 { // two for the first status, one for the second
		t.Fatalf("Source was called %d times, want 3", calls)
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
			if _, err := c.AddProject(context.Background(), fmt.Sprintf("/p/proj%d", i), ""); err != nil {
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

func TestMissingRegistryIsSeeded(t *testing.T) {
	e := newEnv(t) // no projects.json; the seed root holds a "slicer" directory
	os.MkdirAll(filepath.Join(e.dir, "seed", "slicer"), 0o755)
	e.start(t)
	list, err := e.dial(t).Projects(context.Background())
	if err != nil || len(list) != 1 || list[0].Name != "slicer" {
		t.Fatalf("seeded list: %+v, %v", list, err)
	}
	if _, err := os.Stat(e.regPath); err != nil {
		t.Fatalf("the seeded registry was not saved: %v", err)
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
	if err := raw.Call(context.Background(), "wave.start", nil, nil); !errors.As(err, &re) || re.Code != CodeUnknownMethod || !strings.Contains(re.Message, "wave.start") {
		t.Fatalf("unknown method: %v", err)
	}
	if err := raw.Call(context.Background(), MethodProjectsAdd, []int{1}, nil); !errors.As(err, &re) || re.Code != CodeBadParams {
		t.Fatalf("bad params: %v", err)
	}
	if err := raw.Call(context.Background(), MethodProjectsAdd, map[string]any{"path": "/p/x", "nme": "typo"}, nil); !errors.As(err, &re) || re.Code != CodeBadParams || !strings.Contains(re.Message, "nme") {
		t.Fatalf("unknown param key: %v", err)
	}
}
