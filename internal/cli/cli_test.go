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
	for _, name := range []string{"implementer", "reviewer", "gate", "architect"} {
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
	if len(out.Roles) != 4 || out.Roles[0].Name != "implementer" {
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
	Binary string `json:"binary"`
}

func doctorApp(t *testing.T, relayFirstLine string) (app *App, fake *clitest.Fake, logs map[string]string) {
	t.Helper()
	fake = clitest.Install(t,
		clitest.Route{Tool: "slicer", Args: []string{"--version"}, Stdout: "slicer 1.15.0\n"},
		clitest.Route{Tool: "slicer", Args: []string{"ai", "relay"}, Stdout: relayFirstLine + "\n\n# Slice relay\nmore text\n"},
		clitest.Route{Tool: "adj", Args: []string{"--version"}, Stdout: "v0.1.0\n"},
	)
	dir := t.TempDir()
	logs = map[string]string{}
	var bins = map[string]string{}
	for name, ver := range map[string]string{"claude": "2.1.303 (Claude Code)", "codex": "codex-cli 0.163.0", "grok": "grok 1.0.62 (fake)"} {
		bins[name], logs[name] = fakeVendor(t, dir, name, ver)
	}
	app = noDaemon(t)
	app.Tools = Tools{Slicer: fake.Bin("slicer"), Adj: fake.Bin("adj"), Claude: bins["claude"], Codex: bins["codex"], Grok: bins["grok"]}
	return app, fake, logs
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
	if fmt.Sprint(slicerCalls) != "[[--version] [ai relay]]" {
		t.Fatalf("slicer calls: %v", slicerCalls)
	}
	if got := fake.CallsTo("adj"); len(got) != 1 || fmt.Sprint(got[0].Args) != "[--version]" {
		t.Fatalf("adj calls: %v", got)
	}
}

func TestDoctorTextOutput(t *testing.T) {
	app, _, _ := doctorApp(t, "Contract: relay v7")
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
		"[ok] contract  accepts v7, v8, slicer prints \"Contract: relay v7\"\n",
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
	app, _, _ := doctorApp(t, "Contract: relay v7")
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

func TestDoctorAcceptsAV8Slicer(t *testing.T) {
	app, _, _ := doctorApp(t, "Contract: relay v8")
	code, stdout, _ := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, stdout, &d)
	if code != 0 || !d.OK || !d.Contract.OK || d.Contract.Found != "Contract: relay v8" || !reflect.DeepEqual(d.Contract.Accepted, []int{7, 8}) || d.Contract.Error != "" {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}

func TestDoctorContractMismatchFails(t *testing.T) {
	app, _, _ := doctorApp(t, "Contract: relay v9")
	code, stdout, _ := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, stdout, &d)
	if code != 1 || d.OK || d.Contract.OK || d.Contract.Found != "Contract: relay v9" || d.Contract.Error == "" {
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
	for _, v := range []int{6, 9, 70} {
		app, _, _ := doctorApp(t, fmt.Sprintf("Contract: relay v%d", v))
		code, stdout, _ := run(t, app, "doctor", "--json")
		var d doctorOut
		mustJSON(t, stdout, &d)
		if code != 1 || d.OK || d.Contract.OK || !strings.Contains(d.Contract.Error, fmt.Sprintf("v%d", v)) || !strings.Contains(d.Contract.Error, "ork accepts relay v7, v8") {
			t.Errorf("v%d: exit %d\n%s", v, code, stdout)
		}
	}
}

func TestDoctorUnrecognizedContractLine(t *testing.T) {
	app, _, _ := doctorApp(t, "something else entirely")
	code, stdout, _ := run(t, app, "doctor", "--json")
	var d doctorOut
	mustJSON(t, stdout, &d)
	if code != 1 || d.Contract.OK || d.Contract.Found != "something else entirely" {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}

func TestDoctorMissingRequiredToolFails(t *testing.T) {
	for _, tool := range []string{"slicer", "adj"} {
		t.Run(tool, func(t *testing.T) {
			app, _, _ := doctorApp(t, "Contract: relay v7")
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
	app, _, _ := doctorApp(t, "Contract: relay v7")
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
	app, _, _ := doctorApp(t, "Contract: relay v7")
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
