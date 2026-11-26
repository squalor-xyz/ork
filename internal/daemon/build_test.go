// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestStaleBuild(t *testing.T) {
	for _, tc := range []struct {
		own, theirs string
		want        bool
	}{
		{"a", "a", false}, {"a", "b", true}, {"", "dev", false}, {"dev", "", true}, {"unknown", "unknown", true}, {"a", "unknown", true}, {"", "", true},
	} {
		if got := StaleBuild(tc.own, tc.theirs); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}

func TestSnapshotCarriesTheDaemonBuild(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	d.cfg.BuildID = "daemon-build"
	snap, err := d.snapshot(context.Background(), SnapshotParams{Project: p.Name})
	if err != nil {
		t.Fatal(err)
	}
	if snap.DaemonBuild != "daemon-build" {
		t.Fatalf("snapshot daemon build = %q, want daemon-build", snap.DaemonBuild)
	}
}

// selfHostRepo creates commits only in the test's disposable repository.
func selfHostRepo(t *testing.T) (string, string, func(...string) string) {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "main")
	git("commit", "--allow-empty", "-m", "base")
	return root, git("rev-parse", "HEAD"), git
}

func buildProject(t *testing.T, root, build string) map[string]bool {
	t.Helper()
	d, _, p := runDaemon(t, nil)
	// The registry path is test-owned; use its normal registration storage.
	p.Path = root
	if err := registry.Save(d.cfg.RegistryPath, &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	d.cfg.BuildID = build
	setGitBin(d, "git")
	out, err := d.status(context.Background(), StatusParams{})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := d.snapshot(context.Background(), SnapshotParams{Project: p.Name})
	if err != nil {
		t.Fatal(err)
	}
	if snap.SelfHost != out.Projects[0].SelfHost || snap.BehindMain != out.Projects[0].BehindMain {
		t.Fatalf("snapshot build state disagrees with status: %+v, %+v", snap, out.Projects[0])
	}
	for _, active := range []bool{false, true} {
		d.cfg.WaveRunning = func() bool { return active }
		snap, err = d.snapshot(context.Background(), SnapshotParams{Project: p.Name})
		if err != nil || snap.RolesRunning != active {
			t.Fatalf("snapshot activity=%v, want %v: %v", snap.RolesRunning, active, err)
		}
	}
	result := map[string]bool{"self_host": out.Projects[0].SelfHost, "behind_main": out.Projects[0].BehindMain}
	return result
}

func TestSelfHostDetectedByRevision(t *testing.T) {
	root, revision, git := selfHostRepo(t)
	git("tag", "dev") // symbolic revision names must never count as a build commit.
	for _, tc := range []struct {
		build string
		want  bool
	}{
		{revision + "+modified=false", true}, {revision + "+modified=true", true},
		{"dev", false}, {"unknown", false}, {"", false}, {strings.Repeat("f", 40), false}, {"main", false},
	} {
		t.Run(tc.build, func(t *testing.T) {
			if got := buildProject(t, root, tc.build)["self_host"]; got != tc.want {
				t.Fatalf("self_host=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestDaemonBehindBaseBranch(t *testing.T) {
	root, base, git := selfHostRepo(t)
	if buildProject(t, root, base)["behind_main"] {
		t.Fatal("equal revision reported behind")
	}
	git("commit", "--allow-empty", "-m", "landed")
	head := git("rev-parse", "HEAD")
	if !buildProject(t, root, base+"+modified=false")["behind_main"] {
		t.Fatal("ancestor build not reported behind main")
	}
	git("checkout", "-b", "slice", base)
	git("commit", "--allow-empty", "-m", "diverged")
	diverged := git("rev-parse", "HEAD")
	if buildProject(t, root, diverged)["behind_main"] {
		t.Fatal("diverged build reported behind main")
	}
	if buildProject(t, root, head)["behind_main"] {
		t.Fatal("diverged build reported behind checkout branch")
	}
	git("branch", "-D", "main")
	if !buildProject(t, root, base)["behind_main"] {
		t.Fatal("checkout branch ancestor not behind after deleting main")
	}
}

// countingGit records real git invocations, including their arguments.
func countingGit(t *testing.T, d *Daemon) func() []string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log, script := filepath.Join(dir, "calls"), filepath.Join(dir, "git")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> "+quote(log)+"\nexec "+quote(real)+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	setGitBin(d, script)
	return func() []string {
		t.Helper()
		b, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
}

func selfHostDaemon(t *testing.T, root, build string) (*Daemon, registry.Project) {
	t.Helper()
	d, _, p := runDaemon(t, nil)
	p.Path = root
	if err := registry.Save(d.cfg.RegistryPath, &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	d.cfg.BuildID = build
	setGitBin(d, "git")
	return d, p
}

func TestProjectBuildCachedUntilHeadMoves(t *testing.T) {
	root, base, git := selfHostRepo(t)
	d, p := selfHostDaemon(t, root, base)
	calls := countingGit(t, d)
	check := func(wantBehind, hit bool) {
		t.Helper()
		before := len(calls())
		self, behind := d.projectBuild(context.Background(), p)
		if !self || behind != wantBehind {
			t.Fatalf("build state = (%v, %v), want (true, %v)", self, behind, wantBehind)
		}
		got := calls()[before:]
		if hit && (len(got) != 1 || got[0] != "-C "+root+" rev-parse HEAD --symbolic-full-name HEAD") {
			t.Fatalf("cache hit git calls = %v, want only checkout rev-parse", got)
		}
		if !hit && len(got) <= 1 {
			t.Fatalf("cache miss git calls = %v, want full computation", got)
		}
	}
	check(false, false)
	check(false, true)
	git("commit", "--allow-empty", "-m", "moved")
	check(true, false)
	check(true, true)
}

func TestRunDepsBehindFollowsDaemonBuild(t *testing.T) {
	root, base, git := selfHostRepo(t)
	d, p := selfHostDaemon(t, root, base)
	if d.runs.deps.Behind == nil {
		t.Fatal("run deps have no Behind")
	}
	ctx := context.Background()
	if d.runs.deps.Behind(ctx, p) {
		t.Fatal("build at main's revision reported behind")
	}
	git("commit", "--allow-empty", "-m", "landed")
	if !d.runs.deps.Behind(ctx, p) {
		t.Fatal("build not reported behind after a commit landed on main")
	}
	d.cfg.BuildID = "dev"
	if d.runs.deps.Behind(ctx, p) {
		t.Fatal("a build ID that is not a commit reported behind")
	}
}

func TestBaseBranchIsTheMainCheckoutsCurrentBranch(t *testing.T) {
	root, base, git := selfHostRepo(t)
	d, p := selfHostDaemon(t, root, base)
	git("checkout", "-b", "trunk")
	git("commit", "--allow-empty", "-m", "trunk ahead")
	if self, behind := d.projectBuild(context.Background(), p); !self || !behind {
		t.Fatalf("trunk build state = (%v, %v), want (true, true)", self, behind)
	}
	git("checkout", "main")
	if self, behind := d.projectBuild(context.Background(), p); !self || behind {
		t.Fatalf("main build state = (%v, %v), want (true, false)", self, behind)
	}
}

func TestNoBaseBranchIsNotBehind(t *testing.T) {
	for _, name := range []string{"detached", "unborn", "deleted-main"} {
		t.Run(name, func(t *testing.T) {
			root, base, git := selfHostRepo(t)
			git("commit", "--allow-empty", "-m", "ahead")
			wantSelf, wantBehind := true, false
			switch name {
			case "detached":
				git("checkout", "--detach")
			case "unborn":
				root = t.TempDir()
				cmd := exec.Command("git", "-C", root, "init", "-b", "main")
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("init: %s: %v", out, err)
				}
				wantSelf = false
			case "deleted-main":
				git("checkout", "-b", "trunk")
				git("branch", "-D", "main")
				wantBehind = true
			}
			d, p := selfHostDaemon(t, root, base)
			if self, behind := d.projectBuild(context.Background(), p); self != wantSelf || behind != wantBehind {
				t.Fatalf("build state = (%v, %v), want (%v, %v)", self, behind, wantSelf, wantBehind)
			}
		})
	}
}

func TestSnapshotAndStatusAgreeAfterTheCacheHits(t *testing.T) {
	root, base, git := selfHostRepo(t)
	git("checkout", "-b", "trunk")
	git("commit", "--allow-empty", "-m", "ahead")
	d, p := selfHostDaemon(t, root, base)
	calls := countingGit(t, d)
	d.projectBuild(context.Background(), p)
	for range 2 {
		before := len(calls())
		status, err := d.status(context.Background(), StatusParams{})
		if err != nil {
			t.Fatal(err)
		}
		if got := status.Projects[0]; !got.SelfHost || !got.BehindMain {
			t.Fatalf("status build state = (%v, %v), want (true, true)", got.SelfHost, got.BehindMain)
		}
		if got := len(calls()) - before; got != 1 {
			t.Fatalf("warm status git calls = %d, want 1", got)
		}
		before = len(calls())
		snap, err := d.snapshot(context.Background(), SnapshotParams{Project: p.Name})
		if err != nil {
			t.Fatal(err)
		}
		if !snap.SelfHost || !snap.BehindMain {
			t.Fatalf("snapshot build state = (%v, %v), want (true, true)", snap.SelfHost, snap.BehindMain)
		}
		if got := len(calls()) - before; got != 1 {
			t.Fatalf("warm snapshot git calls = %d, want 1", got)
		}
	}
}

func TestEngineSliceFiles(t *testing.T) {
	for _, tc := range []struct {
		heading, body string
		want          bool
	}{
		{"Files", "- `internal/engine/x.go:2-9`", true},
		{"Files", "- `./internal/daemon/x.go`", true},
		{"Files", "- `cmd/ork/main.go`", true},
		{"Files", "- `internal/engineer/x.go`", false},
		{"Files", "- `docs/internal/daemon/x.md`", false},
		{"Implement", "Change `internal/daemon/x.go`", false},
		{"Files", "- `docs/design.md`", false},
	} {
		detail := &slicercli.Detail{Slice: &slicercli.Spec{Sections: []slicercli.Section{{Heading: tc.heading, Body: tc.body}}}}
		if got := EngineSlice(detail); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
	if EngineSlice(nil) || EngineSlice(&slicercli.Detail{}) {
		t.Fatal("missing spec matched")
	}
}

// RunBuild is the one build step of the CLI's restart --rebuild and the daemon's
// own rebuild: it keeps the last twenty lines of a failure and says why it failed.
func TestRunBuild(t *testing.T) {
	dir := t.TempDir()
	if err := RunBuild(context.Background(), dir, "true"); err != nil {
		t.Fatalf("a build that exits 0: %v", err)
	}
	err := RunBuild(context.Background(), dir, "i=1; while [ $i -le 30 ]; do echo line$i; i=$((i+1)); done; exit 3")
	if err == nil || err.Reason != "exit 3" || len(err.Lines) != BuildTailLines || err.Lines[0] != "line11" || err.Lines[BuildTailLines-1] != "line30" {
		t.Fatalf("a failed build: %+v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RunBuild(ctx, dir, "true"); err == nil || err.Reason != "cancelled" {
		t.Fatalf("a cancelled build: %+v", err)
	}
}

// S431 (*proposed*; adj 464, adj 465): the daemon of a self-host project that
// opted in drains, builds and restarts itself after a landing.

// rebuildFake stands for the build command and for the start of the successor.
type rebuildFake struct {
	mu       sync.Mutex
	builds   int
	prepares int
	starts   int
	fail     *BuildError
	built    string // the revision the built executable holds; rebuildNew unless a test sets it
}

func (f *rebuildFake) revision(context.Context) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.built, "/fake/bin/ork", nil
}

func (f *rebuildFake) build(context.Context, registry.Project) *BuildError {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.builds++
	return f.fail
}

func (f *rebuildFake) prepare(context.Context) (func(context.Context) error, error) {
	f.mu.Lock()
	f.prepares++
	f.mu.Unlock()
	return func(context.Context) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.starts++
		return nil
	}, nil
}

func (f *rebuildFake) counts() (builds, starts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.builds, f.starts
}

var (
	rebuildOld = strings.Repeat("a", 40) // the daemon's build
	rebuildNew = strings.Repeat("b", 40) // main after a landing
)

// rebuildDaemon is a daemon whose project p is self-host, has a build command
// and is behind main: a landing has moved main past the daemon's build.
func rebuildDaemon(t *testing.T, autoRebuild bool, runner roundRunner) (*Daemon, *runrec.Store, registry.Project, *rebuildFake) {
	t.Helper()
	root := t.TempDir()
	pp := filepath.Join(root, "p")
	d, store, p := runDaemonIn(t, root, runner, clitest.Route{Tool: "git", Args: []string{"-C", pp, "rev-parse", "HEAD", "--symbolic-full-name", "HEAD"}, Stdout: rebuildNew + " refs/heads/main\n"})
	f, err := registry.Load(d.cfg.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	f.Projects[0].AutoRebuild, f.Projects[0].BuildCommand = autoRebuild, "make build"
	if err := registry.Save(d.cfg.RegistryPath, f); err != nil {
		t.Fatal(err)
	}
	p = f.Projects[0]
	fake := &rebuildFake{built: rebuildNew}
	d.cfg.BuildID = rebuildOld + "+modified=false"
	d.cfg.Rebuild = RebuildConfig{Build: fake.build, Prepare: fake.prepare, Revision: fake.revision, Poll: 5 * time.Millisecond}
	d.runs.deps.Check = func(context.Context, string, string, time.Duration) (engine.CheckOutput, error) {
		return engine.CheckOutput{OK: true}, nil
	}
	return d, store, p, fake
}

// pausedRun stores a run that rests at an error pause in implement and loads its
// engine, as a recovered run is. run.retry then starts its implementer.
func pausedRun(t *testing.T, d *Daemon, store *runrec.Store, p registry.Project, id string) {
	t.Helper()
	lineup := map[string][]runrec.LineupEntry{}
	for _, role := range []string{engine.RoleImplementer, engine.RoleReviewer, engine.RoleGate} {
		lineup[role] = []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet"}}
	}
	r, err := store.Create(runrec.Record{
		ID: id, Project: p.Name, Item: "S1", Worktree: filepath.Join(p.Path, ".worktrees", "S1"),
		State: runrec.StatePaused, Round: 1, Lineup: lineup,
		Pending: &runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Role: engine.RoleImplementer, Round: 1, Reason: "bad report"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d.runs.mu.Lock()
	d.runs.runs[id] = &managedRun{engine: engine.Load(d.runDeps(p), p, r)}
	d.runs.mu.Unlock()
}

// captureExit stands for Run, whose socket path does not fit in a long temp
// directory: it records the request to end the daemon. endLikeRun then does what
// Run's defers do, in their order (TestAutoRebuildRunStartsTheSuccessor runs the
// real Run where the path fits).
func captureExit(d *Daemon) <-chan struct{} {
	exited := make(chan struct{})
	d.rebuild.mu.Lock()
	d.rebuild.stop = func() { close(exited) }
	d.rebuild.mu.Unlock()
	return exited
}

func endLikeRun(d *Daemon) {
	d.closeRuns()
	d.startSuccessor()
}

func neverExited(t *testing.T, exited <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-exited:
		t.Fatalf("the daemon was ended: %s", why)
	default:
	}
}

func rebuildOf(t *testing.T, d *Daemon) *RebuildStatus {
	t.Helper()
	out, err := d.status(context.Background(), StatusParams{})
	if err != nil {
		t.Fatal(err)
	}
	return out.Projects[0].Rebuild
}

func engineRunWait(d *Daemon, id string) string {
	for _, r := range d.engineRuns("p") {
		if r.ID == id {
			return r.Wait
		}
	}
	return ""
}

// blockingFirstRunner records each role start and blocks the first one until
// release is closed. entered is closed when the first starts; ctxs holds the
// context of each start.
type blockingFirstRunner struct {
	mu      sync.Mutex
	started []string
	ctxs    []context.Context
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// newBlockingFirstRunner's first role also ends when the test does, so a test
// that fails early does not hang in its cleanup.
func newBlockingFirstRunner(t *testing.T) *blockingFirstRunner {
	b := &blockingFirstRunner{entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(b.open)
	return b
}

// open ends the first role's wait.
func (b *blockingFirstRunner) open() { b.once.Do(func() { close(b.release) }) }

func (b *blockingFirstRunner) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-b.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first role never started")
	}
}

func (b *blockingFirstRunner) run(ctx context.Context, r engine.Request) (engine.Result, error) {
	b.mu.Lock()
	b.started = append(b.started, r.RunID+" "+r.Role)
	b.ctxs = append(b.ctxs, ctx)
	first := len(b.started) == 1
	b.mu.Unlock()
	if first {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
			return engine.Result{}, ctx.Err()
		}
	}
	return retryRunner(ctx, r)
}

func (b *blockingFirstRunner) starts() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.started...)
}

func TestAutoRebuildDrainsThenRestarts(t *testing.T) {
	roles := newBlockingFirstRunner(t)
	d, store, p, fake := rebuildDaemon(t, true, roles.run)
	exited := captureExit(d)
	pausedRun(t, d, store, p, "r1")
	pausedRun(t, d, store, p, "r2")
	request(t, d, "run.retry", `{"run":"r1","no_wait":true}`)
	roles.waitEntered(t)

	// A landing leaves the daemon behind main: it drains and waits for the role.
	d.checkRebuild()
	waitFor(t, "the drain names the running role", func() bool {
		r := rebuildOf(t, d)
		return r != nil && r.Message == "rebuild pending: waiting for 1 role"
	})
	// No new role starts meanwhile: r2's start waits, and says why.
	request(t, d, "run.retry", `{"run":"r2","no_wait":true}`)
	waitFor(t, "r2's role start is held", func() bool { return engineRunWait(d, "r2") == engine.HoldReason })
	if builds, starts := fake.counts(); builds != 0 || starts != 0 {
		t.Fatalf("while a role runs: %d builds and %d restarts, want none", builds, starts)
	}
	neverExited(t, exited, "under a running role")

	// The role finishes; the runs are held at their next role start; then the
	// daemon builds once and ends with its successor to start once.
	roles.open()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the daemon did not end for the rebuild")
	}
	d.runs.mu.Lock()
	closed := d.runs.closed
	d.runs.mu.Unlock()
	if !closed {
		t.Fatal("admission was left open as the daemon ended")
	}
	endLikeRun(d)
	if builds, starts := fake.counts(); builds != 1 || starts != 1 {
		t.Fatalf("after the drain: %d builds and %d restarts, want 1 and 1", builds, starts)
	}
	if got := roles.starts(); !reflect.DeepEqual(got, []string{"r1 implementer"}) {
		t.Fatalf("roles started: %v, want only r1's implementer, which was running", got)
	}
	r1, err := store.Open("r1")
	if err != nil {
		t.Fatal(err)
	}
	if first := r1.Snapshot().Roles[0]; first.Exit != string(engine.ExitOK) || first.EndedAt.IsZero() {
		t.Fatalf("the running role was cut short: %+v", first)
	}
}

// A role start that already waits for a vendor slot when the hold begins does
// not start when the slot comes: it gives the slot back and waits at the hold.
func TestAutoRebuildHoldsAStartQueuedForASlot(t *testing.T) {
	roles := newBlockingFirstRunner(t)
	d, store, p, fake := rebuildDaemon(t, true, roles.run)
	d.runs.deps.Vendors = engine.NewVendors(map[string]int{"claude": 1})
	exited := captureExit(d)
	pausedRun(t, d, store, p, "r1")
	pausedRun(t, d, store, p, "r2")
	request(t, d, "run.retry", `{"run":"r1","no_wait":true}`)
	roles.waitEntered(t)
	request(t, d, "run.retry", `{"run":"r2","no_wait":true}`) // waits for the one claude slot
	waitFor(t, "r2 waits for the slot", func() bool {
		for _, r := range d.engineRuns("p") {
			if r.ID == "r2" && r.VendorWait == "claude" {
				return true
			}
		}
		return false
	})
	d.checkRebuild()
	waitFor(t, "the drain is pending", func() bool { return rebuildOf(t, d) != nil })
	roles.open()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the daemon did not end for the rebuild")
	}
	if got := roles.starts(); !reflect.DeepEqual(got, []string{"r1 implementer"}) {
		t.Fatalf("roles started: %v, want only r1's implementer: r2 got the slot after the hold began", got)
	}
	endLikeRun(d)
	if builds, starts := fake.counts(); builds != 1 || starts != 1 {
		t.Fatalf("%d builds and %d restarts, want 1 and 1", builds, starts)
	}
}

func TestAutoRebuildFailedBuildReleasesHold(t *testing.T) {
	roles := newBlockingFirstRunner(t)
	d, store, p, fake := rebuildDaemon(t, true, roles.run)
	fake.fail = &BuildError{Reason: "exit 1", Lines: []string{"internal/engine/x.go:1: undefined: y", "make: *** [build] Error 1"}}
	exited := captureExit(d)
	pausedRun(t, d, store, p, "r1")
	pausedRun(t, d, store, p, "r2")
	request(t, d, "run.retry", `{"run":"r1","no_wait":true}`)
	roles.waitEntered(t)
	d.checkRebuild()
	waitFor(t, "the drain waits for the role", func() bool { return rebuildOf(t, d) != nil })
	request(t, d, "run.retry", `{"run":"r2","no_wait":true}`)
	waitFor(t, "r2's role start is held", func() bool { return engineRunWait(d, "r2") == engine.HoldReason })
	roles.open()

	waitFor(t, "the failed build is in status", func() bool {
		r := rebuildOf(t, d)
		return r != nil && r.State == RebuildFailed
	})
	r := rebuildOf(t, d)
	if !strings.Contains(r.Message, "daemon not rebuilt") || !strings.Contains(r.Message, "exit 1") || !reflect.DeepEqual(r.Output, fake.fail.Lines) {
		t.Fatalf("status of the failed build: %+v", r)
	}
	// The old daemon keeps running and the hold is released: both runs go on.
	waitState(t, store, "r1", runrec.StateAwaitingApproval)
	waitState(t, store, "r2", runrec.StateAwaitingApproval)
	if d.runs.deps.Hold.Held() {
		t.Fatal("the hold is still set after the build failed")
	}
	neverExited(t, exited, "after a failed build")
	if _, starts := fake.counts(); starts != 0 {
		t.Fatalf("a failed build started a successor %d times", starts)
	}
	// Not tried again until main moves: the same commit builds nothing.
	d.checkRebuild()
	if r := rebuildOf(t, d); r == nil || r.State != RebuildFailed {
		t.Fatalf("a second check of the same main left the status at %+v, want the failure kept", r)
	}
	time.Sleep(30 * time.Millisecond)
	if builds, _ := fake.counts(); builds != 1 {
		t.Fatalf("builds = %d after a second check of the same main, want 1", builds)
	}
}

// A build that leaves the daemon's own executable at its old revision (the build
// command wrote another file) would start the same build again, and the successor
// would rebuild again, with no end. The daemon keeps running and says why.
func TestAutoRebuildUnchangedBuildKeepsTheDaemon(t *testing.T) {
	d, _, _, fake := rebuildDaemon(t, true, nil)
	fake.built = rebuildOld // the executable still holds the daemon's revision
	exited := captureExit(d)
	d.checkRebuild()
	waitFor(t, "the unchanged build is in status", func() bool {
		r := rebuildOf(t, d)
		return r != nil && r.State == RebuildFailed
	})
	r := rebuildOf(t, d)
	if !strings.Contains(r.Message, "did not change the daemon executable /fake/bin/ork") || len(r.Output) == 0 {
		t.Fatalf("status of the unchanged build: %+v", r)
	}
	neverExited(t, exited, "after a build that changed nothing")
	if builds, starts := fake.counts(); builds != 1 || starts != 0 {
		t.Fatalf("%d builds and %d successors started, want 1 and 0", builds, starts)
	}
	if d.runs.deps.Hold.Held() {
		t.Fatal("the hold is still set after the failure")
	}
	d.runs.mu.Lock()
	closed := d.runs.closed
	d.runs.mu.Unlock()
	if closed {
		t.Fatal("admission was closed although the daemon stays")
	}
}

// The real revision reader finds the executable of the test process, which a Go
// build stamps or leaves without a revision; it never fails on it.
func TestExecutableRevisionReadsThisProcess(t *testing.T) {
	_, path, err := executableRevision(context.Background())
	if err != nil || path == "" {
		t.Fatalf("executableRevision: %q, %v", path, err)
	}
}

func TestAutoRebuildOffKeepsManual(t *testing.T) {
	roles := newBlockingFirstRunner(t)
	d, store, p, fake := rebuildDaemon(t, false, roles.run)
	pausedRun(t, d, store, p, "r1")
	pausedRun(t, d, store, p, "r2")
	request(t, d, "run.retry", `{"run":"r1","no_wait":true}`)
	roles.waitEntered(t)
	d.checkRebuild()
	// Nothing is held: r2's role starts although the daemon is behind main.
	request(t, d, "run.retry", `{"run":"r2","no_wait":true}`)
	waitFor(t, "r2's role starts", func() bool { return len(roles.starts()) >= 2 })
	if d.runs.deps.Hold.Held() || rebuildOf(t, d) != nil || engineRunWait(d, "r2") == engine.HoldReason {
		t.Fatal("a project with auto_rebuild off held a role start")
	}
	roles.open()
	waitState(t, store, "r1", runrec.StateAwaitingApproval)
	waitState(t, store, "r2", runrec.StateAwaitingApproval)
	waitIdle(t, d, "r1")
	waitIdle(t, d, "r2")
	if builds, starts := fake.counts(); builds != 0 || starts != 0 {
		t.Fatalf("auto_rebuild off built %d times and restarted %d", builds, starts)
	}
	// The behind notice is the manual one.
	out, err := d.status(context.Background(), StatusParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got := BehindWarning(out); got != BehindNotice+" --rebuild" {
		t.Fatalf("behind notice = %q, want the manual restart", got)
	}
}

// A daemon that starts behind main, for a project that opted in, builds, ends
// and starts its successor, through the real Run. It needs a socket path that
// fits, which a long temp directory does not give.
func TestAutoRebuildRunStartsTheSuccessor(t *testing.T) {
	d, _, _, fake := rebuildDaemon(t, true, nil)
	d.cfg.Paths = PathsIn(filepath.Join(shortDir(t), "state"))
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the daemon did not end for the rebuild")
	}
	if builds, starts := fake.counts(); builds != 1 || starts != 1 {
		t.Fatalf("%d builds and %d successors started, want 1 and 1", builds, starts)
	}
}

func TestAutoRebuildNeverKillsARole(t *testing.T) {
	roles := newBlockingFirstRunner(t)
	d, store, p, fake := rebuildDaemon(t, true, roles.run)
	pausedRun(t, d, store, p, "r1")
	request(t, d, "run.retry", `{"run":"r1","no_wait":true}`)
	roles.waitEntered(t)
	d.checkRebuild()
	waitFor(t, "the drain waits for the role", func() bool { return rebuildOf(t, d) != nil })
	// Many looks of the drain pass; the role's context stays alive throughout.
	time.Sleep(100 * time.Millisecond)
	roles.mu.Lock()
	err := roles.ctxs[0].Err()
	roles.mu.Unlock()
	if err != nil {
		t.Fatalf("the drain cancelled the running role: %v", err)
	}
	if builds, _ := fake.counts(); builds != 0 {
		t.Fatalf("built %d times under a running role", builds)
	}
	roles.open()
	waitFor(t, "the build after the role", func() bool { b, _ := fake.counts(); return b == 1 })
	r1, err := store.Open("r1")
	if err != nil {
		t.Fatal(err)
	}
	if first := r1.Snapshot().Roles[0]; first.Exit != string(engine.ExitOK) {
		t.Fatalf("the running role did not end on its own: %+v", first)
	}
}
