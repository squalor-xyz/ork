// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/roles"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func TestMain(m *testing.M) {
	clitest.Main()
	os.Exit(m.Run())
}

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

// stubSource answers Fetch from a map by project name.
type stubSource map[string]source.Snapshot

func (s stubSource) Fetch(ctx context.Context, p registry.Project, part source.Part) (any, error) {
	snap := s[p.Name]
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

// world is a temp state directory with a registry and an App whose Start
// launches an in-process daemon.
type world struct {
	dir         string
	paths       daemon.Paths
	regPath     string
	src         source.Fetcher
	waveRunning func() bool
	starts      atomic.Int32
	daemonDone  <-chan struct{}
	app         *App
}

func newWorld(t *testing.T, src source.Fetcher, projects ...registry.Project) *world {
	t.Helper()
	dir := shortDir(t)
	w := &world{dir: dir, paths: daemon.PathsIn(filepath.Join(dir, "state")), regPath: filepath.Join(dir, "projects.json"), src: src}
	if projects == nil {
		projects = []registry.Project{}
	}
	if err := registry.Save(w.regPath, &registry.File{Version: registry.Version, Projects: projects}); err != nil {
		t.Fatal(err)
	}
	w.app = &App{Socket: w.paths.Socket, StartWait: 5 * time.Second, PollEvery: 5 * time.Millisecond, Start: w.startDaemon(t)}
	return w
}

// startDaemon returns a Start function that runs a daemon in this process.
func (w *world) startDaemon(t *testing.T) func(context.Context) error {
	return func(ctx context.Context) error {
		w.starts.Add(1)
		d := daemon.New(daemon.Config{Paths: w.paths, RegistryPath: w.regPath, Source: w.src, WaveRunning: w.waveRunning})
		dctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		w.daemonDone = done
		go func() { d.Run(dctx); close(done) }()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("daemon did not stop")
			}
		})
		return nil
	}
}

// run runs the command line and returns its exit code and output.
func run(t *testing.T, a *App, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code = a.Run(ctx, args, &out, &errb)
	return code, out.String(), errb.String()
}

func mustJSON(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, s)
	}
}

// noDaemon is an App that fails the test if it tries to start a daemon.
func noDaemon(t *testing.T) *App {
	return &App{
		Socket: filepath.Join(shortDir(t), "none.sock"),
		Start:  func(context.Context) error { t.Error("a daemon was started for a local command"); return nil },
	}
}

// ---- starting the daemon ----

func TestStartsTheDaemonOnceWhenNoneAnswers(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "a", Path: "/p/a"})
	code, stdout, stderr := run(t, w.app, "projects", "list")
	if code != 0 || !strings.Contains(stdout, "/p/a") {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if w.starts.Load() != 1 {
		t.Fatalf("Start was called %d times, want 1", w.starts.Load())
	}
	// The daemon now answers, so a second command does not start another.
	if code, _, stderr := run(t, w.app, "projects", "list"); code != 0 {
		t.Fatalf("second run: exit %d: %s", code, stderr)
	}
	if w.starts.Load() != 1 {
		t.Fatalf("Start was called again: %d", w.starts.Load())
	}
}

func TestStartRetriesUntilTheDaemonAnswers(t *testing.T) {
	w := newWorld(t, stubSource{})
	var starts atomic.Int32
	w.app.Start = func(ctx context.Context) error {
		starts.Add(1)
		go func() { // the daemon needs a moment to come up
			time.Sleep(150 * time.Millisecond)
			w.startDaemon(t)(ctx)
		}()
		return nil
	}
	code, _, stderr := run(t, w.app, "projects", "list")
	if code != 0 || starts.Load() != 1 {
		t.Fatalf("exit %d, %d starts: %s", code, starts.Load(), stderr)
	}
}

func TestStartFailureIsPrintedAndExitsOne(t *testing.T) {
	w := newWorld(t, stubSource{})
	w.app.Start = func(context.Context) error { return fmt.Errorf("fork failed: no such binary") }
	code, stdout, stderr := run(t, w.app, "status")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "fork failed: no such binary") {
		t.Fatalf("exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	code, stdout, stderr = run(t, w.app, "status", "--json")
	var e struct {
		Error struct{ Code, Message string }
	}
	mustJSON(t, stdout, &e)
	if code != 1 || e.Error.Code == "" || !strings.Contains(e.Error.Message, "fork failed") || stderr != "" {
		t.Fatalf("--json: exit %d\nstdout: %s\nstderr: %q", code, stdout, stderr)
	}
}

func TestDaemonThatNeverAnswersAfterStartExitsOne(t *testing.T) {
	w := newWorld(t, stubSource{})
	w.app.Start = func(context.Context) error { return nil } // starts nothing
	w.app.StartWait = 100 * time.Millisecond
	began := time.Now()
	code, _, stderr := run(t, w.app, "status")
	if time.Since(began) > 3*time.Second {
		t.Fatalf("gave up after %v, want about 100ms", time.Since(began))
	}
	if code != 1 || !strings.Contains(stderr, "did not answer") || !strings.Contains(stderr, w.paths.Socket) {
		t.Fatalf("exit %d: %q", code, stderr)
	}
}

func TestNoStartFunctionExitsOne(t *testing.T) {
	w := newWorld(t, stubSource{})
	w.app.Start = nil
	code, _, stderr := run(t, w.app, "status")
	if code != 1 || !strings.Contains(stderr, w.paths.Socket) {
		t.Fatalf("exit %d: %q", code, stderr)
	}
}

// ---- status ----

func TestStatusFromTheDaemonsSourceSnapshot(t *testing.T) {
	root := "/projects/ork"
	fixture := func(tool, name string) string { return filepath.Join("..", "..", "testdata", "cli", tool, name) }
	fake := clitest.Install(t,
		clitest.Route{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", root}, File: fixture("slicer", "list.json")},
		clitest.Route{Tool: "slicer", Args: []string{"goals", "--json", "--root", root}, File: fixture("slicer", "goals.json")},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "list", "--json", "--lean", "--active", "--root", root},
			Stdout: `{"runs":[{"id":"run-A","items":["S03"],"worktree":"s03","begun_at":"b","updated_at":"u","state":"active","counts":{"steps":5,"running":2}},{"id":"run-B","items":["S04","S05"],"state":"active","counts":{"steps":1,"running":1}}]}`},
		clitest.Route{Tool: "adj", Args: []string{"inbox", "--json", "--root", root}, File: fixture("adj", "inbox-nonempty.json")}, // 2 open records
		clitest.Route{Tool: "adj", Args: []string{"list", "--json", "--status", "decided", "--root", root}, File: fixture("adj", "list.json")},
	)
	src := source.NewDirect(slicercli.Client{Bin: fake.Bin("slicer")}, adjcli.Client{Bin: fake.Bin("adj")})
	src.Git = fake.Bin("git")
	w := newWorld(t, src,
		registry.Project{Name: "ork", Path: root, Adj: true},
		registry.Project{Name: "plain", Path: "/projects/plain"}, // no routes: its calls fail
	)

	code, stdout, stderr := run(t, w.app, "status", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var st daemon.StatusResult
	mustJSON(t, stdout, &st)
	if len(st.Projects) != 2 || st.Projects[0].Name != "ork" || st.Projects[1].Name != "plain" {
		t.Fatalf("projects: %s", stdout)
	}
	ork := st.Projects[0]
	if ork.NeedsYou != 2 || len(ork.ActiveRelays) != 2 || len(ork.Errors) != 0 || len(ork.Open) != 2 || ork.Open[0].ID != 11 || ork.Open[1].Key != "vendor.codex-ratelimit" {
		t.Fatalf("ork: %+v", ork)
	}
	if r := ork.ActiveRelays[0]; r.ID != "run-A" || r.State != "active" || r.Steps != 5 || r.Running != 2 || len(r.Items) != 1 || r.Items[0] != "S03" {
		t.Fatalf("first relay: %+v", r)
	}
	if ork.ActiveRelays[1].ID != "run-B" || len(ork.ActiveRelays[1].Items) != 2 {
		t.Fatalf("second relay: %+v", ork.ActiveRelays[1])
	}
	if plain := st.Projects[1]; plain.NeedsYou != 0 || len(plain.Errors) == 0 {
		t.Fatalf("plain should report its failed calls: %+v", plain)
	}
	if calls := fake.CallsTo("adj"); len(calls) != 2 {
		t.Fatalf("adj called %d times, want 2 (only the adj project): %+v", len(calls), calls)
	}

	// Text output: one line per project, then its relays and errors.
	code, stdout, _ = run(t, w.app, "status", "--project", "ork")
	want := "ork: needs you 2, active relays 2\n" +
		"  #11 ui.slicer-tui  Does ork reimplement slicer's roadmap editor or reuse slicer tui?\n" +
		"  #51 vendor.codex-ratelimit  Does codex count as rate-limit-capable in v1?\n" +
		"  run-A S03 active (5 steps, 2 running)\n" +
		"  run-B S04,S05 active (1 steps, 1 running)\n"
	if code != 0 || stdout != want {
		t.Fatalf("text status, exit %d:\n%s\nwant:\n%s", code, stdout, want)
	}
}

func TestStatusErrorsAreListedInText(t *testing.T) {
	w := newWorld(t, stubSource{"a": {Items: source.Result[[]slicercli.Item]{Err: fmt.Errorf("slicer is gone")}}},
		registry.Project{Name: "a", Path: "/p/a"}, registry.Project{Name: "b", Path: "/p/b"})
	_, stdout, _ := run(t, w.app, "status")
	want := "a: needs you 0, active relays 0\n  error items: slicer is gone (internal)\n" +
		"b: needs you 0, active relays 0\n"
	if stdout != want {
		t.Fatalf("status:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestStatusUnknownProject(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "a", Path: "/p/a"})
	code, _, stderr := run(t, w.app, "status", "--project", "nope")
	if code != 1 || !strings.Contains(stderr, `"nope"`) {
		t.Fatalf("exit %d: %q", code, stderr)
	}
	code, stdout, _ := run(t, w.app, "status", "--project", "nope", "--json")
	var e struct {
		Error struct{ Code, Message string }
	}
	mustJSON(t, stdout, &e)
	if code != 1 || e.Error.Code != daemon.CodeNotFound {
		t.Fatalf("--json: exit %d: %s", code, stdout)
	}
}

func TestStatusWithNoProjects(t *testing.T) {
	w := newWorld(t, stubSource{})
	code, stdout, _ := run(t, w.app, "status")
	if code != 0 || stdout != daemon.NoProjectsGuide+"\n" {
		t.Fatalf("exit %d: %q", code, stdout)
	}
	_, stdout, _ = run(t, w.app, "status", "--json")
	if strings.TrimSpace(stdout) != "{\n  \"build_id\": \"dev\",\n  \"roles_running\": false,\n  \"projects\": []\n}" {
		t.Fatalf("empty status JSON: %q", stdout)
	}
}

// ---- projects ----

func TestProjectsAddListRemove(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	withAdj := filepath.Join(w.dir, "with-adj")
	plain := filepath.Join(w.dir, "plain")
	for _, d := range []string{withAdj, plain} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(withAdj, ".adjudicate.json"), []byte("{}"), 0o644)

	code, stdout, stderr := run(t, w.app, "projects", "add", withAdj)
	if code != 0 || stdout != fmt.Sprintf("added with-adj %s (adj)\n", withAdj) {
		t.Fatalf("add: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	// --name may follow the path.
	code, stdout, stderr = run(t, w.app, "projects", "add", plain, "--name", "other", "--json")
	var added struct{ Project registry.Project }
	mustJSON(t, stdout, &added)
	if code != 0 || added.Project.Name != "other" || added.Project.Adj || added.Project.Path != plain {
		t.Fatalf("add --name: exit %d\n%s\n%s", code, stdout, stderr)
	}

	saved, err := registry.Load(w.regPath)
	if err != nil || saved.Version != 2 || len(saved.Projects) != 3 || saved.Projects[1].Name != "with-adj" || !saved.Projects[1].Adj || saved.Projects[2].Name != "other" {
		t.Fatalf("saved registry: %+v, %v", saved, err)
	}

	code, stdout, _ = run(t, w.app, "projects", "list")
	want := "keep      /p/keep\n" +
		fmt.Sprintf("with-adj  %s  adj\n", withAdj) +
		fmt.Sprintf("other     %s\n", plain)
	if code != 0 || stdout != want {
		t.Fatalf("list text, exit %d:\n%q\nwant:\n%q", code, stdout, want)
	}
	code, stdout, _ = run(t, w.app, "projects", "list", "--json")
	var list struct{ Projects []registry.Project }
	mustJSON(t, stdout, &list)
	if code != 0 || len(list.Projects) != 3 || list.Projects[2].Name != "other" {
		t.Fatalf("list --json: %s", stdout)
	}

	code, stdout, _ = run(t, w.app, "projects", "rm", "other")
	if code != 0 || stdout != "removed other\n" {
		t.Fatalf("rm: exit %d: %q", code, stdout)
	}
	code, stdout, _ = run(t, w.app, "projects", "rm", "with-adj", "--json")
	var rm struct{ Removed string }
	mustJSON(t, stdout, &rm)
	if code != 0 || rm.Removed != "with-adj" {
		t.Fatalf("rm --json: exit %d: %s", code, stdout)
	}
	after, _ := registry.Load(w.regPath)
	if len(after.Projects) != 1 || after.Projects[0].Name != "keep" {
		t.Fatalf("after rm: %+v", after.Projects)
	}
}

func TestProjectsRefusals(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	elsewhere := filepath.Join(w.dir, "elsewhere")
	os.MkdirAll(elsewhere, 0o755)

	// A duplicate name is a failure the daemon reports.
	code, _, stderr := run(t, w.app, "projects", "add", elsewhere, "--name", "keep")
	if code != 1 || !strings.Contains(stderr, `"keep"`) || !strings.Contains(stderr, "already registered") {
		t.Fatalf("duplicate: exit %d: %q", code, stderr)
	}
	code, stdout, _ := run(t, w.app, "projects", "add", elsewhere, "--name", "keep", "--json")
	var e struct {
		Error struct{ Code, Message string }
	}
	mustJSON(t, stdout, &e)
	if code != 1 || e.Error.Code != daemon.CodeDuplicateName {
		t.Fatalf("duplicate --json: exit %d: %s", code, stdout)
	}

	// A path that does not exist is a failure the daemon reports.
	missing := filepath.Join(w.dir, "missing")
	code, _, stderr = run(t, w.app, "projects", "add", missing)
	if code != 1 || !strings.Contains(stderr, missing) || !strings.Contains(stderr, "does not exist") {
		t.Fatalf("missing: exit %d: %q", code, stderr)
	}

	// A relative path is a usage error, caught before any daemon starts.
	starts := w.starts.Load()
	code, _, stderr = run(t, w.app, "projects", "add", "relative/dir")
	if code != 2 || !strings.Contains(stderr, "relative/dir") {
		t.Fatalf("relative: exit %d: %q", code, stderr)
	}
	if w.starts.Load() != starts {
		t.Fatal("a usage error started the daemon")
	}

	code, _, stderr = run(t, w.app, "projects", "rm", "nope")
	if code != 1 || !strings.Contains(stderr, `"nope"`) {
		t.Fatalf("rm unknown: exit %d: %q", code, stderr)
	}
	if saved, _ := registry.Load(w.regPath); len(saved.Projects) != 1 {
		t.Fatalf("a refusal changed the registry: %+v", saved.Projects)
	}

	// Start before seeding, so recovery cannot launch the unfinished run.
	if code, _, stderr := run(t, w.app, "projects", "list"); code != 0 {
		t.Fatalf("start daemon: exit %d: %s", code, stderr)
	}
	store := &runrec.Store{Dir: filepath.Join(w.paths.Dir, "runs")}
	if _, err := store.Create(runrec.Record{ID: "busy-run", Project: "keep", State: runrec.StatePaused}); err != nil {
		t.Fatal(err)
	}
	want := "project keep has unfinished runs: busy-run"
	code, _, stderr = run(t, w.app, "projects", "rm", "keep")
	if code != 1 || !strings.Contains(stderr, want) {
		t.Errorf("rm busy: exit %d: %q", code, stderr)
	}
	code, stdout, _ = run(t, w.app, "projects", "rm", "keep", "--json")
	mustJSON(t, stdout, &e)
	if code != 1 || e.Error.Code != "project_busy" || e.Error.Message != want {
		t.Errorf("rm busy --json: exit %d: %s", code, stdout)
	}
	if saved, err := registry.Load(w.regPath); err != nil || len(saved.Projects) != 1 || saved.Projects[0].Name != "keep" {
		t.Fatalf("busy refusal changed registry: %+v, %v", saved, err)
	}
}

// ---- roles ----

func TestRolesPrintAllFour(t *testing.T) {
	code, stdout, _ := run(t, noDaemon(t), "roles", "print")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var want strings.Builder
	for i, name := range roles.Names() {
		p, err := roles.Prompt(name)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			want.WriteString("\n")
		}
		fmt.Fprintf(&want, "==== %s ====\n%s", name, p)
		if !strings.HasSuffix(p, "\n") {
			want.WriteString("\n")
		}
	}
	if stdout != want.String() {
		t.Fatalf("roles print differs from the embedded prompts:\n%s", stdout)
	}
	for _, name := range []string{"implementer", "reviewer", "gate", "architect", "ui", "spec-check"} {
		if !strings.Contains(stdout, "==== "+name+" ====\n") {
			t.Errorf("no section for %s", name)
		}
	}
}

func TestRolesPrintOne(t *testing.T) {
	code, stdout, _ := run(t, noDaemon(t), "roles", "print", "gate")
	gate, _ := roles.Prompt("gate")
	if code != 0 || stdout != gate {
		t.Fatalf("roles print gate: exit %d\n%s", code, stdout)
	}
	code, stdout, _ = run(t, noDaemon(t), "roles", "print", "gate", "--json")
	var out struct {
		Roles []struct{ Name, Prompt string }
	}
	mustJSON(t, stdout, &out)
	if code != 0 || len(out.Roles) != 1 || out.Roles[0].Name != "gate" || out.Roles[0].Prompt != gate {
		t.Fatalf("--json: exit %d\n%s", code, stdout)
	}
	_, stdout, _ = run(t, noDaemon(t), "roles", "print", "--json")
	mustJSON(t, stdout, &out)
	if len(out.Roles) != 6 || out.Roles[0].Name != "implementer" || out.Roles[5].Name != "spec-check" {
		t.Fatalf("--json for all: %s", stdout)
	}
}

func TestRolesPrintUnknownRoleIsUsage(t *testing.T) {
	code, stdout, stderr := run(t, noDaemon(t), "roles", "print", "janitor")
	if code != 2 || stdout != "" || !strings.Contains(stderr, `unknown role "janitor"`) {
		t.Fatalf("exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	code, stdout, _ = run(t, noDaemon(t), "roles", "print", "janitor", "--json")
	var e struct {
		Error struct{ Code, Message string }
	}
	mustJSON(t, stdout, &e)
	if code != 2 || e.Error.Code != "usage" || !strings.Contains(e.Error.Message, "janitor") {
		t.Fatalf("--json: exit %d\n%s", code, stdout)
	}
}

// ---- usage ----

func TestUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"frobnicate"},
		{"status", "extra"},
		{"status", "--nope"},
		{"status", "--project"},
		{"projects"},
		{"projects", "frobnicate"},
		{"projects", "add", "/a", "/b"},
		{"projects", "rm"},
		{"projects", "list", "extra"},
		{"roles"},
		{"roles", "print", "gate", "reviewer"},
		{"doctor", "extra"},
	} {
		code, stdout, stderr := run(t, noDaemon(t), args...)
		if code != 2 || stdout != "" || stderr == "" {
			t.Errorf("%q: exit %d\nstdout: %q\nstderr: %q", args, code, stdout, stderr)
		}
	}
}

func TestHelp(t *testing.T) {
	for _, flag := range []string{"-h", "--help", "help"} {
		code, stdout, _ := run(t, noDaemon(t), flag)
		if code != 0 || !strings.Contains(stdout, "ork status") || !strings.Contains(stdout, "ork doctor") {
			t.Errorf("%s: exit %d: %q", flag, code, stdout)
		}
	}
}

// ---- doctor ----

// fakeVendor writes an executable that logs its arguments and answers
// --version, and returns its path and the log.
func fakeVendor(t *testing.T, dir, name, version string) (bin, log string) {
	t.Helper()
	bin = filepath.Join(dir, name)
	log = filepath.Join(dir, name+".log")
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\nif [ \"$1\" = \"--version\" ]; then echo %q; exit 0; fi\necho \"unexpected call\" >&2\nexit 97\n", log, version)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func readLog(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the fake was never called: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

type doctorOut struct {
	OK    bool `json:"ok"`
	Tools []struct {
		Name     string `json:"name"`
		Required bool   `json:"required"`
		Present  bool   `json:"present"`
		Path     string `json:"path"`
		Version  string `json:"version"`
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
	} `json:"tools"`
	Contract struct {
		Accepted []int  `json:"accepted"`
		Found    string `json:"found"`
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
	} `json:"contract"`
	Projects []struct {
		Name     string   `json:"name"`
		Path     string   `json:"path"`
		Findings []string `json:"findings"`
	} `json:"projects"`
	Binary string `json:"binary"`
}

func doctorApp(t *testing.T, relayFirstLine string) (app *App, fake *clitest.Fake, logs map[string]string) {
	t.Helper()
	return doctorAppWith(t, relayFirstLine)
}

// doctorAppWith is doctorApp with extra fake routes (for example git), which
// go after the slicer and adj routes.
func doctorAppWith(t *testing.T, relayFirstLine string, extra ...clitest.Route) (app *App, fake *clitest.Fake, logs map[string]string) {
	t.Helper()
	return doctorAppLead(t, nil, relayFirstLine, extra...)
}

// doctorAppLead is doctorAppWith with routes that go first, so a route for one
// project root beats the generic `ai relay --json` route.
func doctorAppLead(t *testing.T, lead []clitest.Route, relayFirstLine string, extra ...clitest.Route) (app *App, fake *clitest.Fake, logs map[string]string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// The recorded contract of the version in the line: v11 and newer lines use
	// the v11 file, every other line the v10 file.
	file, recordedAs := "relay-v10.json", `"contract": "relay v10"`
	if m := regexp.MustCompile(`relay v(\d+)$`).FindStringSubmatch(relayFirstLine); m != nil {
		if n, _ := strconv.Atoi(m[1]); n >= 11 {
			file, recordedAs = "relay-v11.json", `"contract": "relay v11"`
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cli", "slicer", file))
	if err != nil {
		t.Fatal(err)
	}
	contractJSON := strings.Replace(string(raw), recordedAs, `"contract": "`+strings.TrimPrefix(relayFirstLine, "Contract: ")+`"`, 1)
	routes := append(append([]clitest.Route{}, lead...),
		clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--json"}, Prefix: true, Stdout: contractJSON},
		clitest.Route{Tool: "slicer", Args: []string{"--version"}, Stdout: "slicer 1.15.0\n"},
		clitest.Route{Tool: "adj", Args: []string{"--version"}, Stdout: "v0.1.0\n"},
	)
	fake = clitest.Install(t, append(routes, extra...)...)
	dir := t.TempDir()
	logs = map[string]string{}
	var bins = map[string]string{}
	for name, ver := range map[string]string{"claude": "2.1.303 (Claude Code)", "codex": "codex-cli 0.163.0", "grok": "grok 1.0.62 (fake)"} {
		bins[name], logs[name] = fakeVendor(t, dir, name, ver)
	}
	app = noDaemon(t)
	app.Tools = Tools{Slicer: fake.Bin("slicer"), Adj: fake.Bin("adj"), Git: fake.Bin("git"), Claude: bins["claude"], Codex: bins["codex"], Grok: bins["grok"]}
	return app, fake, logs
}

// doctorProjects writes only a temporary registry and uses fake tool binaries.
func doctorProjects(t *testing.T, projects ...registry.Project) (*App, string) {
	t.Helper()
	app, _, _ := doctorApp(t, "Contract: relay v10")
	path, err := registry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if projects != nil {
		if err := registry.Save(path, &registry.File{Version: registry.Version, Projects: projects}); err != nil {
			t.Fatal(err)
		}
	}
	return app, path
}

func projectReport(t *testing.T, app *App) doctorOut {
	t.Helper()
	code, out, stderr := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, out, &d)
	if code != 0 || !d.OK {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
	if len(d.Projects) != 1 {
		t.Fatalf("want one project: %s", out)
	}
	return d
}

func TestDoctorChecksEachProjectCheckCommand(t *testing.T) {
	for _, tc := range []struct{ name, command, finding string }{
		{"path", "check-tool --never-run", ""},
		{"assignments", "MODE=test OTHER='two words' check-tool", ""},
		{"missing", "MODE=test missing-check-tool test", `check_command: "missing-check-tool" not found on PATH`},
		{"dot-relative", "./local-check-tool", ""},
		{"relative", "bin/check-tool", ""},
		{"quoted", "'./check tool'", ""},
		{"double-quoted", `"./check tool"`, ""},
		{"escaped", `./check\ tool`, ""},
		{"operator", "check-tool&& missing-second-command", ""},
		{"absolute", "ABSOLUTE", ""},
		{"missing-relative", "./missing-check-tool", `check_command: "./missing-check-tool" not found in project directory`},
		{"not-executable", "./plain-file", `check_command: "./plain-file" not executable`},
		{"directory", "./bin", `check_command: "./bin" not executable`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			pathDir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "bin"), 0o755); err != nil {
				t.Fatal(err)
			}
			// Executing this file would leave a marker. Doctor must only parse and resolve.
			marker := filepath.Join(dir, "executed")
			for _, name := range []string{"local-check-tool", "bin/check-tool", "check tool"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "plain-file"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(pathDir, "check-tool"), []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			command := tc.command
			if tc.name == "path" {
				command += " $(touch '" + marker + "') > '" + marker + "'"
			}
			if command == "ABSOLUTE" {
				command = filepath.Join(dir, "local-check-tool")
			}
			app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: dir, CheckCommand: command})
			t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			d := projectReport(t, app)
			findings := strings.Join(d.Projects[0].Findings, "\n")
			if tc.finding == "" && findings != "" || tc.finding != "" && findings != tc.finding {
				t.Fatalf("findings %q, want %q", findings, tc.finding)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("check command was executed: %v", err)
			}
		})
	}
}

func TestDoctorCheckCommandSyntaxError(t *testing.T) {
	app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: t.TempDir(), CheckCommand: "go test ("})
	d := projectReport(t, app)
	if got := strings.Join(d.Projects[0].Findings, " "); !strings.Contains(got, "syntax") {
		t.Fatalf("want syntax finding: %q", got)
	}
}

func TestDoctorReportsProjectWithMissingPath(t *testing.T) {
	for _, file := range []bool{false, true} {
		t.Run(fmt.Sprint(file), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "project")
			if file {
				if err := os.WriteFile(path, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: path})
			d := projectReport(t, app)
			want := "path: does not exist"
			if file {
				want = "path: not a directory"
			}
			if got := strings.Join(d.Projects[0].Findings, " "); got != want {
				t.Fatalf("want path finding: %q", got)
			}
		})
	}
}

func TestDoctorPatternWithoutIdIsReported(t *testing.T) {
	for _, tc := range []struct{ worktree, branch, finding string }{
		{"", "", ""}, {".worktrees/{id}", "slice/{id}", ""},
		{".worktrees/fixed", "", "worktree_pattern"}, {"", "fixed", "branch_pattern"},
		{"{id_lower}", "", ""}, {"", "{id_lower}", ""},
		{"Fixed/{id}/{id_lower}", "feat/{id_lower}/{id}", ""},
	} {
		t.Run(tc.worktree+"/"+tc.branch, func(t *testing.T) {
			app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: t.TempDir(), WorktreePattern: tc.worktree, BranchPattern: tc.branch})
			d := projectReport(t, app)
			got := strings.Join(d.Projects[0].Findings, " ")
			if tc.finding == "" && got != "" || tc.finding != "" && (!strings.Contains(got, tc.finding) || !strings.Contains(got, "has no {id} or {id_lower}") || !strings.Contains(got, fmt.Sprintf("%q", tc.worktree+tc.branch))) {
				t.Fatalf("findings %q, want %q", got, tc.finding)
			}
			if hint := "(fix: ork projects set demo --" + strings.ReplaceAll(tc.finding, "_", "-") + " PATTERN)"; tc.finding != "" && !strings.Contains(got, hint) {
				t.Fatalf("findings %q lack the hint %q", got, hint)
			}
		})
	}
}

func TestDoctorPatternHintQuotesAnAwkwardName(t *testing.T) {
	app, _ := doctorProjects(t, registry.Project{Name: "my proj", Path: t.TempDir(), BranchPattern: "fixed"})
	d := projectReport(t, app)
	got := strings.Join(d.Projects[0].Findings, " ")
	if !strings.Contains(got, "(fix: ork projects set 'my proj' --branch-pattern PATTERN)") {
		t.Fatalf("findings %q", got)
	}
}

func TestDoctorProjectWithoutCheckCommandIsNotAnError(t *testing.T) {
	app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: t.TempDir()})
	d := projectReport(t, app)
	if d.Projects[0].Findings == nil || len(d.Projects[0].Findings) != 0 {
		t.Fatalf("findings: %+v", d.Projects[0])
	}
}

func TestDoctorProjectFindingsKeepExitZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: path, BranchPattern: "fixed"})
	d := projectReport(t, app)
	if len(d.Projects[0].Findings) != 2 {
		t.Fatalf("findings: %+v", d.Projects[0])
	}
	code, out, _ := run(t, app, "doctor")
	if code != 0 || !strings.Contains(out, "[WARN] project demo  "+path+"\n  ") || !strings.Contains(out, "\n  branch_pattern") {
		t.Fatalf("text: exit %d: %s", code, out)
	}
}

func TestDoctorJSONListsEachProject(t *testing.T) {
	projects := []registry.Project{{Name: "one", Path: t.TempDir()}, {Name: "two", Path: t.TempDir(), BranchPattern: "fixed"}}
	app, path := doctorProjects(t, projects...)
	// Compact, deliberately reordered JSON detects a doctor-side Save.
	before := []byte(fmt.Sprintf(`{"projects":[{"path":%q,"name":"one","adj":false},{"path":%q,"name":"two","adj":false,"branch_pattern":"fixed"}],"version":2}`, projects[0].Path, projects[1].Path))
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, out, &d)
	if !strings.HasPrefix(out, "{\n  \"ok\":") || strings.LastIndex(out, `"projects":`) < strings.Index(out, `"binary":`) {
		t.Fatalf("field order: %s", out)
	}
	if strings.Contains(out, `"registry_error"`) {
		t.Fatalf("unexpected registry_error: %s", out)
	}
	if code != 0 || len(d.Projects) != 2 {
		t.Fatalf("exit %d: %s", code, out)
	}
	for i, p := range projects {
		if d.Projects[i].Name != p.Name || d.Projects[i].Path != p.Path || d.Projects[i].Findings == nil {
			t.Fatalf("project %d: %+v", i, d.Projects[i])
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("doctor rewrote registry")
	}
	code, out, _ = run(t, app, "doctor")
	if code != 0 || !strings.Contains(out, "[ok] project one  "+projects[0].Path+"\n") {
		t.Fatalf("healthy project text: %s", out)
	}
}

func TestDoctorWithoutRegistry(t *testing.T) {
	app, path := doctorProjects(t)
	code, out, _ := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, out, &d)
	if code != 0 || !d.OK || d.Projects == nil || len(d.Projects) != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("registry directory created: %v", err)
	}
}

func TestDoctorRegistryReadFailureIsAdvisory(t *testing.T) {
	app, path := doctorProjects(t, registry.Project{Name: "demo", Path: t.TempDir()})
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, app, "doctor")
	if code != 0 || !strings.Contains(out, "[WARN] registry  ") || strings.Index(out, "[WARN] registry") < strings.Index(out, "[ok] contract") {
		t.Fatalf("exit %d: %s", code, out)
	}
	code, out, _ = run(t, app, "doctor", "--json")
	var d struct {
		OK            bool   `json:"ok"`
		Warning       string `json:"warning"`
		RegistryError string `json:"registry_error"`
	}
	mustJSON(t, out, &d)
	if code != 0 || !d.OK || d.Warning != "" || !strings.Contains(d.RegistryError, "unexpected end of JSON input") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestDoctorFindingsAreSingleLine(t *testing.T) {
	for _, command := range []string{`go test "`, ""} {
		t.Run(command, func(t *testing.T) {
			path := t.TempDir()
			if command == "" {
				file := filepath.Join(path, "file\nline")
				if err := os.WriteFile(file, nil, 0o644); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(file, "child")
			}
			app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: path, CheckCommand: command})
			d := projectReport(t, app)
			if len(d.Projects[0].Findings) != 1 {
				t.Fatalf("findings: %+v", d.Projects[0].Findings)
			}
			for _, finding := range d.Projects[0].Findings {
				if strings.ContainsAny(finding, "\r\n") {
					t.Fatalf("multiline finding: %q", finding)
				}
			}
			_, out, _ := run(t, app, "doctor")
			_, findings, ok := strings.Cut(out, "[WARN] project demo  "+path+"\n")
			if !ok {
				t.Fatalf("project line missing: %s", out)
			}
			for _, line := range strings.Split(strings.TrimSuffix(findings, "\n"), "\n") {
				if !strings.HasPrefix(line, "  ") {
					t.Fatalf("unindented finding: %q", line)
				}
			}
		})
	}
}

func TestDoctorSyntaxFindingOmitsShellCall(t *testing.T) {
	app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: t.TempDir(), CheckCommand: "go test ("})
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "sh: -c: line 1: syntax error near unexpected token '('" "sh: -c: line 1: go test (" >&2
exit 2
`
	if err := os.WriteFile(filepath.Join(dir, "sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	d := projectReport(t, app)
	want := "check_command: syntax error near unexpected token '('"
	if got := strings.Join(d.Projects[0].Findings, " "); got != want {
		t.Fatalf("finding %q, want %q", got, want)
	}
}

func TestDoctorSkipsShellWords(t *testing.T) {
	words := strings.Fields("! { } case do done elif else esac fi for if in then until while . : break continue eval exec exit export readonly return set shift times trap unset cd command time")
	words = append(words, "$TOOL", "${TOOL}", "$(printf tool)", "`printf tool`", "~/bin/check", "~someone/bin/check")
	for _, word := range words {
		t.Run(word, func(t *testing.T) {
			if got := checkCommandWord("MODE=test " + word); got != "" {
				t.Fatalf("shell word %q must skip lookup, got %q", word, got)
			}
		})
	}
}

func TestDoctorShellCommandsHaveNoLookupFinding(t *testing.T) {
	for _, command := range []string{"if :; then :; fi", "{ :; }", ". ./missing-script", "cd /missing-dir", "command missing-check-tool", `"$TOOL"`, "`printf tool`", "~/bin/check"} {
		t.Run(command, func(t *testing.T) {
			app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: t.TempDir(), CheckCommand: command})
			d := projectReport(t, app)
			if len(d.Projects[0].Findings) != 0 {
				t.Fatalf("findings: %+v", d.Projects[0].Findings)
			}
		})
	}
}

func TestDoctorReportsEachToolAndTheAcceptedContracts(t *testing.T) {
	app, fake, logs := doctorApp(t, fmt.Sprintf("Contract: relay v%d", roles.ContractVersion))
	code, stdout, stderr := run(t, app, "doctor", "--json")
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	var d doctorOut
	mustJSON(t, stdout, &d)
	if !d.OK || len(d.Tools) != 5 {
		t.Fatalf("report: %s", stdout)
	}
	versions := map[string]string{"slicer": "slicer 1.15.0", "adj": "v0.1.0", "claude": "2.1.303 (Claude Code)", "codex": "codex-cli 0.163.0", "grok": "grok 1.0.62 (fake)"}
	for i, name := range []string{"slicer", "adj", "claude", "codex", "grok"} {
		tool := d.Tools[i]
		if tool.Name != name || !tool.Present || !tool.OK || tool.Version != versions[name] || tool.Path == "" {
			t.Errorf("%s: %+v", name, tool)
		}
		if tool.Required != (name == "slicer" || name == "adj") {
			t.Errorf("%s required = %v", name, tool.Required)
		}
	}
	if !reflect.DeepEqual(d.Contract.Accepted, roles.AcceptedContracts()) || d.Contract.Found != fmt.Sprintf("Contract: relay v%d", roles.ContractVersion) || !d.Contract.OK {
		t.Fatalf("contract: %+v", d.Contract)
	}

	// No vendor model call: each vendor was asked for its version and nothing else.
	for name, log := range logs {
		if got := readLog(t, log); len(got) != 1 || got[0] != "--version" {
			t.Errorf("%s was called with %q", name, got)
		}
	}
	var slicerCalls [][]string
	for _, c := range fake.CallsTo("slicer") {
		slicerCalls = append(slicerCalls, c.Args)
	}
	cwd, _ := os.Getwd()
	if fmt.Sprint(slicerCalls) != "[[--version] [ai relay --json --root "+cwd+"]]" {
		t.Fatalf("slicer calls: %v", slicerCalls)
	}
	if got := fake.CallsTo("adj"); len(got) != 1 || fmt.Sprint(got[0].Args) != "[--version]" {
		t.Fatalf("adj calls: %v", got)
	}
}

func TestDoctorTextOutput(t *testing.T) {
	app, _, _ := doctorApp(t, "Contract: relay v11")
	app.Tools.Grok = filepath.Join(t.TempDir(), "no-such-grok")
	code, stdout, _ := run(t, app, "doctor")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	for _, want := range []string{
		"[ok] slicer  slicer 1.15.0\n",
		"[ok] adj  v0.1.0\n",
		"[ok] claude  2.1.303 (Claude Code)\n",
		"[ok] codex  codex-cli 0.163.0\n",
		"[--] grok  not found (optional)\n",
		"[ok] contract  accepts v10, v11, slicer prints \"Contract: relay v11\"\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
}

// doctor reports the path of the ork binary that runs it: in the JSON report
// and as the first text line. The expected path is the running test binary's.
func TestDoctorReportsWhereTheBinaryIs(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	app, _, _ := doctorApp(t, "Contract: relay v10")
	code, stdout, stderr := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, stdout, &d)
	if code != 0 || d.Binary != want {
		t.Fatalf("exit %d, binary %q, want %q\nstdout: %s\nstderr: %s", code, d.Binary, want, stdout, stderr)
	}
	code, stdout, _ = run(t, app, "doctor")
	if line := "[ok] ork  " + want + "\n"; code != 0 || !strings.Contains(stdout, line) {
		t.Fatalf("text: exit %d, output lacks %q:\n%s", code, line, stdout)
	}
}

func TestDoctorAcceptsAV10Slicer(t *testing.T) {
	app, _, _ := doctorApp(t, "Contract: relay v10")
	code, stdout, _ := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, stdout, &d)
	if code != 0 || !d.OK || !d.Contract.OK || d.Contract.Found != "Contract: relay v10" || !reflect.DeepEqual(d.Contract.Accepted, []int{10, 11}) || d.Contract.Error != "" {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}

func TestDoctorContractMismatchFails(t *testing.T) {
	app, _, _ := doctorApp(t, "Contract: relay v8")
	code, stdout, _ := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, stdout, &d)
	if code != 1 || d.OK || d.Contract.OK || d.Contract.Found != "Contract: relay v8" || d.Contract.Error == "" {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	for _, tool := range d.Tools {
		if !tool.OK {
			t.Errorf("%s should still be ok: %+v", tool.Name, tool)
		}
	}
	code, stdout, _ = run(t, app, "doctor")
	if code != 1 || !strings.Contains(stdout, "[FAIL] contract") {
		t.Fatalf("text: exit %d\n%s", code, stdout)
	}
}

// The accepted set replaces the exact pin (boundary.contract): an older
// version is refused as well as a newer one, and the error names both sides.
func TestDoctorRefusesEveryVersionOutsideTheAcceptedSet(t *testing.T) {
	for _, v := range []int{6, 7, 8, 9, 12, 70} {
		app, _, _ := doctorApp(t, fmt.Sprintf("Contract: relay v%d", v))
		code, stdout, _ := run(t, app, "doctor", "--json")
		var d doctorOut
		mustJSON(t, stdout, &d)
		if code != 1 || d.OK || d.Contract.OK || !strings.Contains(d.Contract.Error, fmt.Sprintf("v%d", v)) || !strings.Contains(d.Contract.Error, "ork accepts relay v10, v11") {
			t.Errorf("v%d: exit %d\n%s", v, code, stdout)
		}
	}
}

func TestDoctorUnrecognizedContractLine(t *testing.T) {
	app, _, _ := doctorApp(t, "something else entirely")
	code, stdout, _ := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, stdout, &d)
	if code != 1 || d.Contract.OK || d.Contract.Found != "Contract: something else entirely" {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}

func TestDoctorMissingRequiredToolFails(t *testing.T) {
	for _, tool := range []string{"slicer", "adj"} {
		t.Run(tool, func(t *testing.T) {
			app, _, _ := doctorApp(t, "Contract: relay v10")
			missing := filepath.Join(t.TempDir(), "gone")
			if tool == "slicer" {
				app.Tools.Slicer = missing
			} else {
				app.Tools.Adj = missing
			}
			code, stdout, _ := run(t, app, "doctor", "--json")
			var d doctorOut
			mustJSON(t, stdout, &d)
			if code != 1 || d.OK {
				t.Fatalf("exit %d\n%s", code, stdout)
			}
			for _, got := range d.Tools {
				if got.Name == tool && (got.Present || got.OK || got.Error == "") {
					t.Errorf("%s: %+v", tool, got)
				}
			}
			if tool == "slicer" && (d.Contract.OK || d.Contract.Error == "") {
				t.Errorf("without slicer the contract cannot be checked: %+v", d.Contract)
			}
		})
	}
}

func TestDoctorMissingVendorIsNotAFailure(t *testing.T) {
	app, _, _ := doctorApp(t, "Contract: relay v10")
	app.Tools.Codex = filepath.Join(t.TempDir(), "gone")
	code, stdout, _ := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, stdout, &d)
	if code != 0 || !d.OK {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	if codex := d.Tools[3]; codex.Name != "codex" || codex.Present || codex.OK || codex.Required {
		t.Fatalf("codex: %+v", codex)
	}
}

func TestDoctorToolThatFailsItsVersionCall(t *testing.T) {
	app, _, _ := doctorApp(t, "Contract: relay v10")
	bad := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(bad, []byte("#!/bin/sh\necho 'boom' >&2\nexit 3\n"), 0o755)
	app.Tools.Claude = bad
	code, stdout, _ := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, stdout, &d)
	claude := d.Tools[2]
	if claude.Name != "claude" || !claude.Present || claude.OK || !strings.Contains(claude.Error, "exit 3") {
		t.Fatalf("claude: %+v", claude)
	}
	if code != 0 { // optional tool: reported, not fatal
		t.Fatalf("exit %d", code)
	}
}

func TestStatusRelaySteps(t *testing.T) {
	steps := []slicercli.Step{
		{Role: "orchestrator", Vendor: "claude", Model: "sonnet", Phase: "start", Task: "Run relay"},
		{Role: "implementer", Vendor: "codex", Model: "gpt-6.1", Phase: "start", Task: "Implement S30"},
		{Role: "implementer", Phase: "fail", Task: "Earlier failure"},
		{Role: "reviewer", Vendor: "claude", Model: "opus", Phase: "fail", Task: "Review S30"},
		{Role: "gate", Phase: "done", Task: "Finished gate"},
	}
	w := newWorld(t, stubSource{"ork": {Runs: source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{
		{ID: "ledger", Items: []string{"S30"}, State: "active", Counts: slicercli.Counts{Steps: 5, Running: 2}, Steps: steps},
	}}}}, registry.Project{Name: "ork", Path: "/projects/ork"})
	code, stdout, stderr := run(t, w.app, "status")
	want := "ork: needs you 0, active relays 1\n  ledger S30 active (5 steps, 2 running)\n    running: implementer codex/gpt-6.1: Implement S30\n    failed: reviewer: Review S30\n"
	if code != 0 || stdout != want {
		t.Fatalf("step status exit %d: %s\ngot:\n%swant:\n%s", code, stderr, stdout, want)
	}
	code, stdout, stderr = run(t, w.app, "status", "--json")
	for _, field := range []string{
		`"running_step":{"role":"implementer","vendor":"codex","model":"gpt-6.1","task":"Implement S30","phase":"start"}`,
		`"failed_step":{"role":"reviewer","vendor":"claude","model":"opus","task":"Review S30","phase":"fail"}`,
	} {
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(stdout)); err != nil {
			t.Fatal(err)
		}
		if code != 0 || !strings.Contains(compact.String(), field) {
			t.Fatalf("step JSON exit %d: %s\n%s", code, stderr, stdout)
		}
	}
}

// ork status says what the Running row says: the leading "<id> <role>" label is
// cut from the running step's task (S150). The failed step and --json keep the
// task as it was recorded.
func TestStatusRunningTextCutsTheTaskLabel(t *testing.T) {
	for _, tc := range []struct{ task, running string }{
		{"s30 implementer", "running: implementer codex/gpt-6.1"},
		{"S30/Implementer", "running: implementer codex/gpt-6.1"},
		{"s30 implementer fix round 2", "running: implementer codex/gpt-6.1: fix round 2"},
		{"s30 implementer - fix round 2: x", "running: implementer codex/gpt-6.1: fix round 2: x"},
		{"Implement S30", "running: implementer codex/gpt-6.1: Implement S30"},
	} {
		t.Run(tc.task, func(t *testing.T) {
			w := newWorld(t, stubSource{"ork": {Runs: source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{
				{ID: "ledger", Items: []string{"S30"}, State: "active", Steps: []slicercli.Step{
					{Role: "implementer", Vendor: "codex", Model: "gpt-6.1", Task: tc.task, Phase: "start"},
					{Role: "reviewer", Task: "s30 reviewer", Phase: "fail"},
				}},
			}}}}, registry.Project{Name: "ork", Path: "/projects/ork"})
			code, stdout, stderr := run(t, w.app, "status")
			want := "ork: needs you 0, active relays 1\n  ledger S30 active (0 steps, 0 running)\n    " + tc.running + "\n    failed: reviewer: s30 reviewer\n"
			if code != 0 || stdout != want {
				t.Fatalf("exit %d: %s\ngot %q\nwant %q", code, stderr, stdout, want)
			}
			code, stdout, _ = run(t, w.app, "status", "--json")
			if code != 0 || !strings.Contains(stdout, `"task": "`+tc.task+`"`) && !strings.Contains(stdout, `"task":"`+tc.task+`"`) {
				t.Fatalf("--json must keep the recorded task %q:\n%s", tc.task, stdout)
			}
		})
	}
}

func TestStatusRelayOptionalFields(t *testing.T) {
	for _, tc := range []struct {
		name, vendor, model, task, running, failed string
	}{
		{"empty", "", "", "", "running: implementer", "failed: reviewer"},
		{"vendor", "codex", "", "", "running: implementer codex", "failed: reviewer"},
		{"model", "", "sol", "", "running: implementer sol", "failed: reviewer"},
		{"task", "", "", "Work", "running: implementer: Work", "failed: reviewer: Work"},
		{"multiline", "codex", "sol", "One\r\ntwo\nthree\rfour", "running: implementer codex/sol: One two three four", "failed: reviewer: One two three four"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, stubSource{"ork": {Runs: source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{
				{ID: "ledger", Items: []string{"S30"}, State: "active", Steps: []slicercli.Step{
					{Role: "implementer", Vendor: tc.vendor, Model: tc.model, Task: tc.task, Phase: "start"},
					{Role: "reviewer", Vendor: tc.vendor, Model: tc.model, Task: tc.task, Phase: "fail"},
				}},
			}}}}, registry.Project{Name: "ork", Path: "/projects/ork"})
			code, stdout, stderr := run(t, w.app, "status")
			want := "ork: needs you 0, active relays 1\n  ledger S30 active (0 steps, 0 running)\n    " + tc.running + "\n    " + tc.failed + "\n"
			if code != 0 || stdout != want {
				t.Fatalf("optional fields exit %d: %s\ngot %q\nwant %q", code, stderr, stdout, want)
			}
		})
	}
}

func TestStatusBoundsDenials(t *testing.T) {
	app := &App{Call: func(_ context.Context, _ string, _, result any) error {
		return json.Unmarshal([]byte(`{"projects":[{"name":"p","engine_runs":[{"id":"r1","item":"S43","state":"implement","denials":["A","B","C","D","E"]}]}]}`), result)
	}}
	code, stdout, stderr := run(t, app, "status")
	if code != 0 || !strings.Contains(stdout, "(denied: A, B, C and 2 more)\n") {
		t.Fatalf("bounded denials missing: code %d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

// ---- projects set (S161) ----

func TestProjectsSetUpdatesOneField(t *testing.T) {
	w := newWorld(t, stubSource{},
		registry.Project{Name: "keep", Path: "/p/keep", Cap: 2, UIDoc: "a.md"},
		registry.Project{Name: "other", Path: "/p/other", CheckCommand: "make"})
	code, stdout, stderr := run(t, w.app, "projects", "set", "other", "--push", "on")
	if code != 0 || stdout != "updated other: push=on\n" {
		t.Fatalf("exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	saved, err := registry.Load(w.regPath)
	if err != nil {
		t.Fatal(err)
	}
	wantKeep := registry.Project{Name: "keep", Path: "/p/keep", Cap: 2, UIDoc: "a.md"}
	wantOther := registry.Project{Name: "other", Path: "/p/other", CheckCommand: "make", Push: true}
	if !reflect.DeepEqual(saved.Projects, []registry.Project{wantKeep, wantOther}) {
		t.Fatalf("saved: %+v", saved.Projects)
	}

	// Several flags at once, in a fixed order; --json prints the project.
	code, stdout, stderr = run(t, w.app, "projects", "set", "other", "--json",
		"--budget-tokens", "900", "--budget-minutes", "30", "--check", "go test ./...", "--ui-doc", "docs/ui.md",
		"--worktree-pattern", ".wt/{id_lower}", "--branch-pattern", "{id_lower}", "--cap", "3", "--check-minutes", "5", "--push", "off")
	var res struct{ Project registry.Project }
	mustJSON(t, stdout, &res)
	want := registry.Project{Name: "other", Path: "/p/other", CheckCommand: "go test ./...", CheckMinutes: 5, Cap: 3, UIDoc: "docs/ui.md",
		WorktreePattern: ".wt/{id_lower}", BranchPattern: "{id_lower}", Budgets: registry.Budgets{Minutes: 30, Tokens: 900}}
	if code != 0 || !reflect.DeepEqual(res.Project, want) {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	saved, _ = registry.Load(w.regPath)
	if !reflect.DeepEqual(saved.Projects[1], want) || !reflect.DeepEqual(saved.Projects[0], wantKeep) {
		t.Fatalf("saved: %+v", saved.Projects)
	}

	// An empty value clears a text field, and the text output says so.
	code, stdout, _ = run(t, w.app, "projects", "set", "other", "--ui-doc", "", "--check-minutes", "0")
	if code != 0 || stdout != "updated other: ui_doc=unset, check_minutes=unset\n" {
		t.Fatalf("clear: exit %d: %q", code, stdout)
	}
}

// The text says what the registry now holds, not what was typed (S161).
func TestProjectsSetPrintsTheStoredValues(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	code, stdout, stderr := run(t, w.app, "projects", "set", "keep", "--ui-doc", "./docs/../docs/ui.md")
	if code != 0 || stdout != "updated keep: ui_doc=\"docs/ui.md\"\n" {
		t.Fatalf("exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
}

func TestProjectsSetRefusesUnknownField(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	starts := w.starts.Load()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"projects", "set", "keep", "--nope", "x"}, "flag provided but not defined: -nope"},
		{[]string{"projects", "set", "keep", "--adj", "on"}, "flag provided but not defined: -adj"},
		{[]string{"projects", "set", "keep"}, "needs at least one field flag"},
		{[]string{"projects", "set", "--push", "on"}, "takes one NAME, got 0"},
		{[]string{"projects", "set", "keep", "other", "--push", "on"}, "takes one NAME, got 2"},
		{[]string{"projects", "set", "keep", "--push", "maybe"}, `--push takes on or off, got "maybe"`},
		{[]string{"projects", "set", "keep", "--cap", "many"}, `invalid value "many" for flag -cap`},
	} {
		code, _, stderr := run(t, w.app, tc.args...)
		if code != 2 || !strings.Contains(stderr, "projects set") || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: exit %d: %q, want %q", tc.args, code, stderr, tc.want)
		}
	}
	if w.starts.Load() != starts {
		t.Fatal("a usage error started the daemon")
	}
	if saved, _ := registry.Load(w.regPath); !reflect.DeepEqual(saved.Projects, []registry.Project{{Name: "keep", Path: "/p/keep"}}) {
		t.Fatalf("a refusal changed the registry: %+v", saved.Projects)
	}
}

func TestProjectsSetValidatesUIDoc(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep", UIDoc: "old.md"})
	for _, bad := range []string{"/abs/ui.md", "../ui.md", "a/../../ui.md"} {
		code, _, stderr := run(t, w.app, "projects", "set", "keep", "--ui-doc", bad)
		if code != 1 || !strings.Contains(stderr, "ui_doc") || !strings.Contains(stderr, bad) {
			t.Errorf("%q: exit %d: %q", bad, code, stderr)
		}
	}
	code, stdout, _ := run(t, w.app, "projects", "set", "keep", "--ui-doc", "../x.md", "--json")
	var e struct {
		Error struct{ Code, Message string }
	}
	mustJSON(t, stdout, &e)
	if code != 1 || e.Error.Code != daemon.CodeBadParams {
		t.Fatalf("--json: exit %d: %s", code, stdout)
	}
	if saved, _ := registry.Load(w.regPath); saved.Projects[0].UIDoc != "old.md" {
		t.Fatalf("a refusal changed the registry: %+v", saved.Projects)
	}
}

func TestProjectsSetRefusesAnUnfinishedRun(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	if code, _, stderr := run(t, w.app, "projects", "list"); code != 0 {
		t.Fatalf("start daemon: exit %d: %s", code, stderr)
	}
	store := &runrec.Store{Dir: filepath.Join(w.paths.Dir, "runs")}
	if _, err := store.Create(runrec.Record{ID: "busy-run", Project: "keep", State: runrec.StatePaused}); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(t, w.app, "projects", "set", "keep", "--push", "on")
	if code != 1 || !strings.Contains(stderr, "project keep has unfinished runs: busy-run") {
		t.Fatalf("exit %d: %q", code, stderr)
	}
	if saved, _ := registry.Load(w.regPath); saved.Projects[0].Push {
		t.Fatal("a busy refusal changed the registry")
	}
	code, _, stderr = run(t, w.app, "projects", "set", "nope", "--push", "on")
	if code != 1 || !strings.Contains(stderr, `"nope"`) {
		t.Fatalf("unknown project: exit %d: %q", code, stderr)
	}
}

func TestDoctorRegistryErrorDoesNotRepeatTheWord(t *testing.T) {
	app, path := doctorProjects(t, registry.Project{Name: "demo", Path: t.TempDir()})
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, _ := run(t, app, "doctor")
	if strings.Contains(out, "registry  registry") {
		t.Fatalf("registry word repeated:\n%s", out)
	}
	want := "[WARN] registry  " + path + ": unexpected end of JSON input\n"
	if !strings.Contains(out, want) {
		t.Fatalf("output lacks %q:\n%s", want, out)
	}
	// The JSON field keeps the full error text.
	_, out, _ = run(t, app, "doctor", "--json")
	var d struct {
		RegistryError string `json:"registry_error"`
	}
	mustJSON(t, out, &d)
	if want := "registry " + path + ": unexpected end of JSON input"; d.RegistryError != want {
		t.Fatalf("registry_error %q, want %q", d.RegistryError, want)
	}
}

func TestDoctorShellErrorPrefixStopsAtTheFirstMarker(t *testing.T) {
	app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: t.TempDir(), CheckCommand: "go test ("})
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "sh: -c: line 1: unexpected word sh: 2: tail" >&2
exit 2
`
	if err := os.WriteFile(filepath.Join(dir, "sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	d := projectReport(t, app)
	want := "check_command: unexpected word sh: 2: tail"
	if got := strings.Join(d.Projects[0].Findings, " "); got != want {
		t.Fatalf("finding %q, want %q", got, want)
	}
}

func TestDoctorTimeIsOnTheShellWordSkipList(t *testing.T) {
	// PATH holds only sh, so a lookup of time would warn on any host.
	pathDir := t.TempDir()
	if err := os.Symlink("/bin/sh", filepath.Join(pathDir, "sh")); err != nil {
		t.Fatal(err)
	}
	app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: t.TempDir(), CheckCommand: "time go test"})
	t.Setenv("PATH", pathDir)
	d := projectReport(t, app)
	if len(d.Projects[0].Findings) != 0 {
		t.Fatalf("findings: %+v", d.Projects[0].Findings)
	}
}

func TestDoctorShowsNoneRegisteredForAnEmptyRegistry(t *testing.T) {
	const want = "[--] projects  none registered: run ork projects add PATH\n"
	t.Run("missing", func(t *testing.T) {
		app, _ := doctorProjects(t)
		_, out, _ := run(t, app, "doctor")
		if !strings.Contains(out, want) || strings.Index(out, want) < strings.Index(out, "[ok] contract") {
			t.Fatalf("output lacks %q after the contract line:\n%s", want, out)
		}
	})
	t.Run("empty", func(t *testing.T) {
		app, path := doctorProjects(t)
		if err := registry.Save(path, &registry.File{Version: registry.Version, Projects: []registry.Project{}}); err != nil {
			t.Fatal(err)
		}
		_, out, _ := run(t, app, "doctor")
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	})
	t.Run("projects", func(t *testing.T) {
		app, _ := doctorProjects(t, registry.Project{Name: "demo", Path: t.TempDir()})
		_, out, _ := run(t, app, "doctor")
		if strings.Contains(out, "none registered") {
			t.Fatalf("unexpected line:\n%s", out)
		}
	})
	t.Run("unreadable", func(t *testing.T) {
		app, path := doctorProjects(t, registry.Project{Name: "demo", Path: t.TempDir()})
		if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, out, _ := run(t, app, "doctor")
		if strings.Contains(out, "none registered") {
			t.Fatalf("unexpected line:\n%s", out)
		}
	})
}

// ---- projects set: env lines, permissions and vendor env (S174) ----

func TestProjectsSetEnvLinePermissionAndAllowFlags(t *testing.T) {
	keep := registry.Project{Name: "keep", Path: "/p/keep", Cap: 2, EnvironmentLines: []string{"old"},
		Permissions: map[string]registry.Permission{"gate": {Preset: "read-only"}},
		VendorEnv:   map[string]map[string]string{"codex": {"K": "v"}}}
	other := registry.Project{Name: "other", Path: "/p/other", EnvironmentLines: []string{"other line"}}
	w := newWorld(t, stubSource{}, keep, other)
	saved := func() registry.Project {
		t.Helper()
		f, err := registry.Load(w.regPath)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.Projects[1], other) {
			t.Fatalf("the other project changed: %+v", f.Projects[1])
		}
		return f.Projects[0]
	}

	code, stdout, stderr := run(t, w.app, "projects", "set", "keep",
		"--env-line", "zsh on darwin", "--env-line", "Go 1.27.x",
		"--permission", "reviewer=read-only", "--allow", "implementer=Read", "--allow", "implementer=Bash(go test*)",
		"--vendor-env", "codex:A=1", "--vendor-env", "codex:URL=http://x?a=b")
	want := "updated keep: environment_lines=2 lines, permissions=implementer:2 patterns; reviewer:read-only, vendor_env=codex:A,K,URL\n"
	if code != 0 || stdout != want {
		t.Fatalf("exit %d\nstdout: %q\nwant:   %q\nstderr: %q", code, stdout, want, stderr)
	}
	wantKeep := keep
	wantKeep.EnvironmentLines = []string{"zsh on darwin", "Go 1.27.x"}
	wantKeep.Permissions = map[string]registry.Permission{"gate": {Preset: "read-only"}, "reviewer": {Preset: "read-only"},
		"implementer": {Allow: []string{"Read", "Bash(go test*)"}}}
	wantKeep.VendorEnv = map[string]map[string]string{"codex": {"K": "v", "A": "1", "URL": "http://x?a=b"}}
	if got := saved(); !reflect.DeepEqual(got, wantKeep) {
		t.Fatalf("saved:\n got %+v\nwant %+v", got, wantKeep)
	}

	// One empty --env-line clears the list.
	code, stdout, stderr = run(t, w.app, "projects", "set", "keep", "--env-line", "")
	if code != 0 || stdout != "updated keep: environment_lines=unset\n" {
		t.Fatalf("clear env lines: exit %d: %q %q", code, stdout, stderr)
	}
	wantKeep.EnvironmentLines = nil
	if got := saved(); !reflect.DeepEqual(got, wantKeep) {
		t.Fatalf("after clearing env lines:\n got %+v\nwant %+v", got, wantKeep)
	}

	// One line and one pattern read in the singular.
	code, stdout, stderr = run(t, w.app, "projects", "set", "keep", "--env-line", "just one", "--allow", "implementer=Edit")
	if code != 0 || stdout != "updated keep: environment_lines=1 line, permissions=implementer:1 pattern\n" {
		t.Fatalf("singular: exit %d: %q %q", code, stdout, stderr)
	}
	code, stdout, stderr = run(t, w.app, "projects", "set", "keep", "--env-line", "")
	if code != 0 || stdout != "updated keep: environment_lines=unset\n" {
		t.Fatalf("clear again: exit %d: %q %q", code, stdout, stderr)
	}
	wantKeep.Permissions["implementer"] = registry.Permission{Allow: []string{"Edit"}}

	// ROLE= removes the role's entry; the other roles stay.
	code, stdout, stderr = run(t, w.app, "projects", "set", "keep", "--permission", "reviewer=")
	if code != 0 || stdout != "updated keep: permissions=reviewer:default\n" {
		t.Fatalf("remove a role: exit %d: %q %q", code, stdout, stderr)
	}
	delete(wantKeep.Permissions, "reviewer")
	if got := saved(); !reflect.DeepEqual(got, wantKeep) {
		t.Fatalf("after removing a role:\n got %+v\nwant %+v", got, wantKeep)
	}

	// --unset-vendor-env removes one key, then the vendor once it is empty.
	code, stdout, stderr = run(t, w.app, "projects", "set", "keep", "--unset-vendor-env", "codex:K", "--unset-vendor-env", "codex:A")
	if code != 0 || stdout != "updated keep: vendor_env=codex:URL\n" {
		t.Fatalf("unset keys: exit %d: %q %q", code, stdout, stderr)
	}
	code, stdout, _ = run(t, w.app, "projects", "set", "keep", "--unset-vendor-env", "codex:URL", "--json")
	var res struct{ Project registry.Project }
	mustJSON(t, stdout, &res)
	wantKeep.VendorEnv = nil
	if code != 0 || !reflect.DeepEqual(res.Project, wantKeep) {
		t.Fatalf("--json after the last key: exit %d: %s", code, stdout)
	}
	if got := saved(); !reflect.DeepEqual(got, wantKeep) {
		t.Fatalf("after unsetting every key:\n got %+v\nwant %+v", got, wantKeep)
	}
	code, stdout, _ = run(t, w.app, "projects", "set", "keep", "--unset-vendor-env", "codex:URL")
	if code != 0 || stdout != "updated keep: vendor_env=codex:unset\n" {
		t.Fatalf("unset a missing key: exit %d: %q", code, stdout)
	}
}

// A hand-edited role key that ork does not know can be removed with ROLE=;
// setting an unknown role is refused (S174, owner decision 2026-10-29).
func TestProjectsSetRemovesAHandEditedRoleKey(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep",
		Permissions: map[string]registry.Permission{"reviwer": {Preset: "read-only"}, "gate": {Preset: "read-only"}}})
	code, stdout, stderr := run(t, w.app, "projects", "set", "keep", "--permission", "bogus=read-only")
	if code != 1 || !strings.Contains(stderr, "unknown role") {
		t.Fatalf("set an unknown role: exit %d: %q %q", code, stdout, stderr)
	}
	code, stdout, stderr = run(t, w.app, "projects", "set", "keep", "--permission", "reviwer=")
	if code != 0 || stdout != "updated keep: permissions=reviwer:default\n" {
		t.Fatalf("remove the typo: exit %d: %q %q", code, stdout, stderr)
	}
	saved, _ := registry.Load(w.regPath)
	if want := map[string]registry.Permission{"gate": {Preset: "read-only"}}; !reflect.DeepEqual(saved.Projects[0].Permissions, want) {
		t.Fatalf("saved: %+v", saved.Projects[0].Permissions)
	}
}

func TestProjectsSetNeverPrintsVendorEnvValues(t *testing.T) {
	const secret = "s3cr3t-value"
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	code, stdout, stderr := run(t, w.app, "projects", "set", "keep", "--vendor-env", "codex:MYKEY="+secret)
	if code != 0 || !strings.Contains(stdout, "MYKEY") || strings.Contains(stdout+stderr, secret) {
		t.Fatalf("set: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	// A call the daemon refuses names the key and not the value.
	code, stdout, stderr = run(t, w.app, "projects", "set", "keep", "--vendor-env", "codex:1BAD="+secret)
	if code != 1 || !strings.Contains(stderr, "1BAD") || strings.Contains(stdout+stderr, secret) {
		t.Fatalf("daemon refusal: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	code, stdout, stderr = run(t, w.app, "projects", "set", "keep", "--vendor-env", "Codex:MYKEY="+secret)
	if code != 1 || strings.Contains(stdout+stderr, secret) {
		t.Fatalf("daemon refusal of the vendor: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	// A usage error quotes no value, even when the flag is malformed.
	for _, arg := range []string{secret, "MYKEY=" + secret, "codex-" + secret, "A=" + secret + ":B=c"} {
		code, stdout, stderr = run(t, w.app, "projects", "set", "keep", "--vendor-env", arg)
		if code != 2 || strings.Contains(stdout+stderr, secret) {
			t.Errorf("--vendor-env %q: exit %d\nstdout: %q\nstderr: %q", arg, code, stdout, stderr)
		}
	}
	// A refusal for an unfinished run names no value either.
	if code, _, stderr := run(t, w.app, "projects", "list"); code != 0 {
		t.Fatalf("start daemon: %s", stderr)
	}
	store := &runrec.Store{Dir: filepath.Join(w.paths.Dir, "runs")}
	if _, err := store.Create(runrec.Record{ID: "busy-run", Project: "keep", State: runrec.StatePaused}); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = run(t, w.app, "projects", "set", "keep", "--vendor-env", "codex:MYKEY="+secret)
	if code != 1 || !strings.Contains(stderr, "unfinished runs") || strings.Contains(stdout+stderr, secret) {
		t.Fatalf("busy: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
}

func TestProjectsSetNewFlagUsageErrors(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	starts := w.starts.Load()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--allow", "implementer"}, "--allow wants ROLE=PATTERN"},
		{[]string{"--allow", "=Read"}, "--allow wants ROLE=PATTERN"},
		{[]string{"--permission", "reviewer"}, "--permission wants ROLE=read-only or ROLE="},
		{[]string{"--permission", "=read-only"}, "--permission wants ROLE=read-only or ROLE="},
		{[]string{"--vendor-env", "codex"}, "--vendor-env wants VENDOR:KEY=VALUE"},
		{[]string{"--vendor-env", "codex:A"}, "--vendor-env wants VENDOR:KEY=VALUE"},
		{[]string{"--vendor-env", "A=1"}, "--vendor-env wants VENDOR:KEY=VALUE"},
		{[]string{"--vendor-env", ":A=1"}, "--vendor-env wants VENDOR:KEY=VALUE"},
		{[]string{"--vendor-env", "A=1:B=2"}, "--vendor-env wants VENDOR:KEY=VALUE"},
		{[]string{"--unset-vendor-env", "A"}, "--unset-vendor-env wants VENDOR:KEY"},
		{[]string{"--unset-vendor-env", "codex:"}, "--unset-vendor-env wants VENDOR:KEY"},
		{[]string{"--permission", "reviewer=read-only", "--allow", "reviewer=Read"}, "--permission and --allow both set reviewer"},
		{[]string{"--permission", "reviewer=", "--allow", "reviewer=Read"}, "--permission and --allow both set reviewer"},
		{[]string{"--permission", "reviewer=read-only", "--permission", "reviewer="}, "--permission names reviewer twice"},
		{[]string{"--vendor-env", "codex:A=1", "--unset-vendor-env", "codex:A"}, "--vendor-env and --unset-vendor-env both name codex:A"},
		{[]string{"--vendor-env", "codex:A=1", "--vendor-env", "codex:A=2"}, "--vendor-env names codex:A twice"},
	} {
		args := append([]string{"projects", "set", "keep"}, tc.args...)
		code, _, stderr := run(t, w.app, args...)
		if code != 2 || !strings.Contains(stderr, "projects set") || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: exit %d: %q, want %q", tc.args, code, stderr, tc.want)
		}
	}
	if w.starts.Load() != starts {
		t.Fatal("a usage error started the daemon")
	}
	if saved, _ := registry.Load(w.regPath); !reflect.DeepEqual(saved.Projects, []registry.Project{{Name: "keep", Path: "/p/keep"}}) {
		t.Fatalf("a refusal changed the registry: %+v", saved.Projects)
	}
}

func TestProjectsSetUIPassSkills(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep", UIDoc: "keep.md"})
	for _, tc := range []struct {
		value, output string
		want          []string
	}{
		{"tui-design,frontend-design", "updated keep: ui_pass_skills=\"tui-design,frontend-design\"\n", []string{"tui-design", "frontend-design"}},
		{"", "updated keep: ui_pass_skills=unset\n", nil},
	} {
		code, stdout, stderr := run(t, w.app, "projects", "set", "keep", "--ui-pass-skills", tc.value)
		if code != 0 || stdout != tc.output {
			t.Fatalf("exit %d stdout %q stderr %s", code, stdout, stderr)
		}
		data, err := os.ReadFile(w.regPath)
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			Projects []struct {
				UIDoc  string   `json:"ui_doc"`
				Skills []string `json:"ui_pass_skills"`
			}
		}
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(saved.Projects[0].Skills, tc.want) || saved.Projects[0].UIDoc != "keep.md" {
			t.Fatalf("saved: %+v", saved)
		}
	}
	for _, bad := range []string{"a,", ",b", " ", "a\nb", "a\x00b"} {
		code, _, stderr := run(t, w.app, "projects", "set", "keep", "--ui-pass-skills", bad)
		if code != 1 || !strings.Contains(stderr, "ui_pass_skills") {
			t.Errorf("%q: exit %d: %s", bad, code, stderr)
		}
	}
}

func TestProjectsSetUIPassSkillsTrimsNames(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	code, stdout, stderr := run(t, w.app, "projects", "set", "keep", "--ui-pass-skills", " a, b ")
	if code != 0 || stdout != "updated keep: ui_pass_skills=\"a,b\"\n" {
		t.Fatalf("exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	saved, err := registry.Load(w.regPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Projects[0].UIPassSkills, []string{"a", "b"}) {
		t.Fatalf("saved names: %q", saved.Projects[0].UIPassSkills)
	}
}

func TestProjectsSetUIPassSkillsBlankPosition(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	for _, bad := range []string{"a,,b", "a,"} {
		t.Run(bad, func(t *testing.T) {
			code, _, stderr := run(t, w.app, "projects", "set", "keep", "--ui-pass-skills", bad)
			if code != 1 || !strings.Contains(stderr, `ui_pass_skills: item 2 name "" must not be blank`) {
				t.Fatalf("%q exit %d: %s", bad, code, stderr)
			}
		})
	}
}

func TestProjectsSetUIPassSkillsRejectsControlCharacters(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	for _, bad := range []string{"a\r", "a\n", "a\x00"} {
		t.Run(fmt.Sprintf("%q", bad), func(t *testing.T) {
			code, _, stderr := run(t, w.app, "projects", "set", "keep", "--ui-pass-skills", bad)
			if code != 1 || !strings.Contains(stderr, fmt.Sprintf("name %q must not contain NUL, CR or LF", bad)) {
				t.Fatalf("%q exit %d: %s", bad, code, stderr)
			}
		})
	}
}
