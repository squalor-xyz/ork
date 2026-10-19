// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
	"github.com/squalor-xyz/ork/internal/tui"
)

func TestMain(m *testing.M) {
	clitest.Main()
	os.Exit(m.Run())
}

// sandbox points the state, config and seed directories at a temporary
// directory, so no test touches the real ones. The socket path must fit
// macOS's limit, so the directory is a short one when it can be.
func sandbox(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ork")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if len(filepath.Join(dir, "state", "ork", "ork.sock")) > 100 {
		dir = t.TempDir()
		if len(filepath.Join(dir, "state", "ork", "ork.sock")) > 100 {
			t.Skipf("no temp directory short enough for a socket: %s", dir)
		}
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("ORK_SEED_ROOT", filepath.Join(dir, "seed"))
	if err := os.MkdirAll(filepath.Join(dir, "seed"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// registryPath is the sandbox's registry (registry.DefaultPath).
func registryPath(dir string) string { return filepath.Join(dir, "config", "ork", "projects.json") }

func writeRegistry(t *testing.T, dir string, projects ...registry.Project) {
	t.Helper()
	if projects == nil {
		projects = []registry.Project{}
	}
	if err := registry.Save(registryPath(dir), &registry.File{Version: registry.Version, Projects: projects}); err != nil {
		t.Fatal(err)
	}
}

// makeSeeds creates the named project directories in the sandbox's seed root.
func makeSeeds(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.Mkdir(filepath.Join(dir, "seed", n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// refused is a program whose every function fails the test when it runs.
func refused(t *testing.T) program {
	return program{
		start:  func(context.Context) error { t.Error("a daemon was started"); return nil },
		tui:    func(tui.Config) error { t.Error("the TUI started"); return nil },
		daemon: func(context.Context) error { t.Error("the daemon served"); return nil },
	}
}

// noTUI is refused with a daemon that may start, for a command that uses one.
func noTUI(t *testing.T, start func(context.Context) error) program {
	p := refused(t)
	p.start = start
	return p
}

// inProcessDaemon returns a start function that runs a daemon in this process,
// reading through src, on the socket the environment names. The daemon stops
// when the test ends.
func inProcessDaemon(t *testing.T, src source.Fetcher) func(context.Context) error {
	return func(context.Context) error {
		paths, err := daemon.DefaultPaths()
		if err != nil {
			return err
		}
		d := daemon.New(daemon.Config{Paths: paths, Source: src})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { d.Run(ctx); close(done) }()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("the daemon did not stop")
			}
		})
		return nil
	}
}

// inTest is a program whose daemon runs in this process, reading through src,
// and whose TUI is tuiFn.
func inTest(t *testing.T, src source.Fetcher, tuiFn func(tui.Config) error) program {
	p := refused(t)
	p.start = inProcessDaemon(t, src)
	p.tui = tuiFn
	return p
}

// emptySource answers every part with no value and no error.
type emptySource struct{}

func (emptySource) Fetch(context.Context, registry.Project, source.Part) (any, error) {
	return nil, nil
}

// dialUntilUp dials the daemon's socket until something answers, for up to 5 s.
func dialUntilUp(t *testing.T, socket string) *daemon.Client {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := daemon.Dial(context.Background(), socket)
		if err == nil {
			t.Cleanup(func() { c.Close() })
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("no daemon answered at %s: %v", socket, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestVersionPrintsAndStartsNoTUI(t *testing.T) {
	for _, flag := range []string{"--version", "-version"} {
		var out, errOut bytes.Buffer
		code := run(context.Background(), []string{flag}, &out, &errOut, refused(t))
		if code != 0 || !strings.HasPrefix(out.String(), "ork ") || strings.Count(out.String(), "\n") != 1 {
			t.Errorf("%s: code %d, stdout %q, stderr %q", flag, code, out.String(), errOut.String())
		}
	}
}

func TestUnknownFlagOrArgumentIsAUsageError(t *testing.T) {
	sandbox(t)
	for _, args := range [][]string{{"--nope"}, {"extra"}, {"daemon"}, {"daemon", "stop"}} {
		var out, errOut bytes.Buffer
		code := run(context.Background(), args, &out, &errOut, refused(t))
		if code != 2 || errOut.Len() == 0 {
			t.Errorf("%v: code %d, stderr %q", args, code, errOut.String())
		}
	}
}

// help is a command of internal/cli, and it needs no daemon.
func TestHelpReachesTheCommandLine(t *testing.T) {
	sandbox(t)
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"help"}, &out, &errOut, refused(t))
	if code != 0 || !strings.HasPrefix(out.String(), "usage: ork") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}

// status reaches internal/cli, which reads through the daemon this binary
// started.
func TestStatusReachesTheCommandLineThroughTheDaemon(t *testing.T) {
	dir := sandbox(t)
	writeRegistry(t, dir, registry.Project{Name: "mine", Path: filepath.Join(dir, "mine")})
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"status"}, &out, &errOut, inTest(t, emptySource{}, func(tui.Config) error {
		t.Error("the TUI started for status")
		return nil
	}))
	if want := "mine: needs you 0, active relays 0\n"; code != 0 || out.String() != want {
		t.Fatalf("code %d, stdout %q (want %q), stderr %q", code, out.String(), want, errOut.String())
	}
}

// No arguments open the TUI, and its Source and its daemon feed are the
// daemon's.
func TestNoArgumentsOpenTheTUIOnTheDaemon(t *testing.T) {
	dir := sandbox(t)
	writeRegistry(t, dir, registry.Project{Name: "mine", Path: filepath.Join(dir, "mine")})
	var got tui.Config
	var out, errOut bytes.Buffer
	code := run(context.Background(), nil, &out, &errOut, inTest(t, emptySource{}, func(c tui.Config) error {
		got = c
		return nil
	}))
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	if got.Source == nil || got.Live == nil || len(got.Projects) != 1 {
		t.Fatalf("config: source %v, live %v, projects %+v", got.Source, got.Live, got.Projects)
	}
	if snap := got.Source.Snapshot(context.Background(), got.Projects[0]); snap.Project.Name != "mine" {
		t.Errorf("snapshot of %+v", snap.Project)
	}
}

// ork daemon run serves the socket until its context ends, and exits 0.
func TestDaemonRunServesTheSocketUntilItsContextEnds(t *testing.T) {
	sandbox(t)
	p := refused(t)
	p.daemon = serveDaemon
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exited := make(chan int, 1)
	var errOut bytes.Buffer
	go func() { exited <- run(ctx, []string{"daemon", "run"}, io.Discard, &errOut, p) }()

	paths, err := daemon.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	c := dialUntilUp(t, paths.Socket)
	projects, err := c.Projects(context.Background())
	if err != nil || len(projects) != 0 {
		t.Fatalf("projects.list over the socket: %v, %+v", err, projects)
	}
	c.Close()

	cancel()
	select {
	case code := <-exited:
		if code != 0 {
			t.Fatalf("daemon run exited %d, stderr %q", code, errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon run did not stop when its context ended")
	}
}

// The TUI's Source reads over the socket what source.Direct reads from the
// same fake CLIs: the same values and the same tool errors, part by part.
func TestTheTUISourceReadsWhatSourceDirectReads(t *testing.T) {
	dir := sandbox(t)
	alpha, beta := filepath.Join(dir, "alpha"), filepath.Join(dir, "beta")
	writeRegistry(t, dir,
		registry.Project{Name: "alpha", Path: alpha, Adj: true},
		registry.Project{Name: "beta", Path: beta},
	)
	routes := append(fixtureRoutes(t, alpha, true), fixtureRoutes(t, beta, false)...)
	fake := clitest.Install(t, routes...)
	direct := source.NewDirect(slicercli.Client{Bin: fake.Bin("slicer")}, adjcli.Client{Bin: fake.Bin("adj")})
	direct.Git = fake.Bin("git")

	var compared int
	p := inTest(t, direct, func(cfg tui.Config) error {
		ctx := context.Background()
		for _, proj := range cfg.Projects {
			got := cfg.Source.Snapshot(ctx, proj)
			want := direct.Snapshot(ctx, proj)
			compareSnapshots(t, proj.Name, got, want)
			compared++
		}
		return nil
	})
	var out, errOut bytes.Buffer
	if code := run(context.Background(), nil, &out, &errOut, p); code != 0 || compared != 2 {
		t.Fatalf("code %d, compared %d, stderr %q", code, compared, errOut.String())
	}
}

// fixtureRoutes answers the calls ork makes for one project from the testdata
// files. A project with adj also gets the adj calls.
func fixtureRoutes(t *testing.T, root string, adj bool) []clitest.Route {
	t.Helper()
	td := func(p string) string {
		abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "cli", p))
		if err != nil {
			t.Fatal(err)
		}
		return abs
	}
	routes := []clitest.Route{
		{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", root}, File: td("slicer/list.json")},
		{Tool: "slicer", Args: []string{"goals", "--json", "--root", root}, File: td("slicer/goals.json")},
		{Tool: "slicer", Args: []string{"relay-run", "list", "--json", "--lean", "--active", "--root", root}, File: td("slicer/relay-run-list.json")},
		{Tool: "git", Args: []string{"-C", root, "rev-parse", "HEAD"}, Stdout: "h1\n"},
	}
	if adj {
		routes = append(routes,
			clitest.Route{Tool: "adj", Args: []string{"inbox", "--json", "--root", root}, File: td("adj/inbox-nonempty.json")},
			clitest.Route{Tool: "adj", Args: []string{"list", "--json", "--status", "decided", "--root", root}, File: td("adj/list.json")},
		)
	}
	return routes
}

// compareSnapshots fails the test for each part whose value or tool error
// differs between the two snapshots. ReadAt is not compared: the socket
// reports when the daemon read a part, and Direct reads nothing.
func compareSnapshots(t *testing.T, name string, got, want source.Snapshot) {
	t.Helper()
	if name == "alpha" && len(want.Items.Value) == 0 {
		t.Fatal("the fixture has no items, so the comparison proves nothing")
	}
	check := func(part string, gotValue, wantValue any, gotErr, wantErr error) {
		t.Helper()
		if !reflect.DeepEqual(gotValue, wantValue) {
			t.Errorf("%s %s value over the socket %+v, want %+v", name, part, gotValue, wantValue)
		}
		if !sameErr(gotErr, wantErr) {
			t.Errorf("%s %s error over the socket %v, want %v", name, part, gotErr, wantErr)
		}
	}
	check("items", got.Items.Value, want.Items.Value, got.Items.Err, want.Items.Err)
	check("goals", got.Goals.Value, want.Goals.Value, got.Goals.Err, want.Goals.Err)
	check("runs", got.Runs.Value, want.Runs.Value, got.Runs.Err, want.Runs.Err)
	check("open", got.Open.Value, want.Open.Value, got.Open.Err, want.Open.Err)
	check("decided", got.Decided.Value, want.Decided.Value, got.Decided.Err, want.Decided.Err)
}

// sameErr reports whether two errors have the same code and message once
// converted to their wire form, and whether both are nil.
func sameErr(a, b error) bool {
	ea, eb := ipc.ErrorOf(a), ipc.ErrorOf(b)
	if ea == nil || eb == nil {
		return ea == eb
	}
	return ea.Code == eb.Code && ea.Message == eb.Message
}

func TestStartSeedsAMissingRegistryAndSaysSo(t *testing.T) {
	dir := sandbox(t)
	makeSeeds(t, dir, "slicer", "agents")
	path := registryPath(dir)
	var out, errOut bytes.Buffer
	var got tui.Config
	code := run(context.Background(), nil, &out, &errOut, inTest(t, emptySource{}, func(c tui.Config) error {
		got = c
		return nil
	}))
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	if len(got.Projects) != 2 || got.Projects[0].Name != "slicer" || got.Projects[1].Name != "agents" {
		t.Fatalf("projects: %+v", got.Projects)
	}
	if got.Source == nil {
		t.Error("no Source")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("registry not written: %v", err)
	}
	if msg := errOut.String(); !strings.Contains(msg, path) || !strings.Contains(msg, "2 project") {
		t.Errorf("seeding not announced: %q", msg)
	}
}

func TestStartReadsAnExistingRegistryWithoutSeeding(t *testing.T) {
	dir := sandbox(t)
	makeSeeds(t, dir, "slicer")
	path := registryPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"version":1,"projects":[{"name":"mine","path":"/work/mine","adj":false}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	var got tui.Config
	code := run(context.Background(), nil, &out, &errOut, inTest(t, emptySource{}, func(c tui.Config) error {
		got = c
		return nil
	}))
	if code != 0 || len(got.Projects) != 1 || got.Projects[0].Name != "mine" {
		t.Fatalf("code %d, projects %+v, stderr %q", code, got.Projects, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr: %q", errOut.String())
	}
}

// With no projects the TUI does not open and no daemon starts.
func TestStartWithNoProjectsFailsWithoutTheTUI(t *testing.T) {
	sandbox(t) // no project directories exist, so the seed holds nothing
	var out, errOut bytes.Buffer
	code := run(context.Background(), nil, &out, &errOut, refused(t))
	if code != 1 || !strings.Contains(errOut.String(), "no projects") {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
}

func TestABrokenRegistryFailsWithItsError(t *testing.T) {
	dir := sandbox(t)
	path := registryPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":9,"projects":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run(context.Background(), nil, &out, &errOut, refused(t))
	if code != 1 || !strings.Contains(errOut.String(), "unknown registry version 9") {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
}

func TestATUIErrorExitsNonZero(t *testing.T) {
	dir := sandbox(t)
	makeSeeds(t, dir, "slicer")
	var out, errOut bytes.Buffer
	code := run(context.Background(), nil, &out, &errOut, inTest(t, emptySource{}, func(tui.Config) error {
		return errors.New("no tty")
	}))
	if code != 1 || !strings.Contains(errOut.String(), "no tty") {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
}

// When no daemon can be started the TUI does not open, and the command says why.
func TestNoDaemonMeansNoTUI(t *testing.T) {
	dir := sandbox(t)
	makeSeeds(t, dir, "slicer")
	var out, errOut bytes.Buffer
	p := noTUI(t, func(context.Context) error { return errors.New("exec refused") })
	p.tui = func(tui.Config) error { t.Error("the TUI started without a daemon"); return nil }
	code := run(context.Background(), nil, &out, &errOut, p)
	if code != 1 || !strings.Contains(errOut.String(), "start the daemon") {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
}
