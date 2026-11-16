// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
)

var rev1 = strings.Repeat("a", 40)
var rev2 = strings.Repeat("b", 40)

func staleApp(t *testing.T, projects []string, routes ...clitest.Route) (*App, *clitest.Fake) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var ps []registry.Project
	for i, p := range projects {
		ps = append(ps, registry.Project{Name: strings.Repeat("p", i+1), Path: p})
	}
	path, err := registry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if projects != nil {
		if err := registry.Save(path, &registry.File{Version: registry.Version, Projects: ps}); err != nil {
			t.Fatal(err)
		}
	}
	fake := clitest.Install(t, routes...)
	a := &App{BuildID: rev2 + "+modified=false", Tools: Tools{Git: fake.Bin("git")}}
	a.Call = func(_ context.Context, method string, _, result any) error {
		var v any = daemon.StatusResult{BuildID: rev1 + "+modified=false"}
		if method == daemon.MethodDaemonInfo {
			v = daemon.DaemonInfoResult{BuildID: rev1 + "+modified=false"}
		}
		b, _ := json.Marshal(v)
		return json.Unmarshal(b, result)
	}
	return a, fake
}

func staleRoutes(path string, diff int) []clitest.Route {
	return []clitest.Route{
		{Tool: "git", Args: []string{"-C", path, "cat-file", "-e"}, Prefix: true},
		{Tool: "git", Args: []string{"-C", path, "diff", "--quiet"}, Prefix: true, Exit: diff},
	}
}

func TestStalePathspecCountsOnlyWhatTheBinaryIsBuiltFrom(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	git("init")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.invalid")
	git("-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "base")
	for _, tc := range []struct {
		path string
		code bool
	}{
		{".slicer/x.json", false}, {"docs/a.md", false}, {"CHANGELOG.md", false}, {"a_test.go", false}, {"testdata/x", false}, {".github/y", false},
		{"internal/roles/prompts/a.md", true}, {"internal/cli/ai_instructions.md", true}, {"x.go", true}, {"go.mod", true}, {"cmd/ork/main.go", true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			p := filepath.Join(root, tc.path)
			if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("change"), 0600); err != nil {
				t.Fatal(err)
			}
			git("add", "--", tc.path)
			git("-c", "commit.gpgsign=false", "commit", "-m", "fixture")
			args := append([]string{"-C", root, "diff", "--quiet", "HEAD^", "HEAD", "--"}, engineChangePathspec...)
			err := exec.Command("git", args...).Run()
			exit := 0
			if err != nil {
				if e, ok := err.(*exec.ExitError); ok {
					exit = e.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			want := 0
			if tc.code {
				want = 1
			}
			if exit != want {
				t.Fatalf("%s: diff exit %d want %d", tc.path, exit, want)
			}
		})
	}
}

func TestStaleTrackingOnlyCommitIsNotStale(t *testing.T) {
	p := t.TempDir()
	a, _ := staleApp(t, []string{p}, staleRoutes(p, 0)...)
	for _, args := range [][]string{{"status"}, {"status", "--json"}} {
		code, _, err := run(t, a, args...)
		if code != 0 || err != "" {
			t.Errorf("tracking-only %v: %d stderr %q", args, code, err)
		}
	}
	wantStatusText(t, a, "state: current (the CLI build differs only in files the binary is not built from)\n")
}

func TestStaleCodeChangeIsStale(t *testing.T) {
	p := t.TempDir()
	a, _ := staleApp(t, []string{p}, staleRoutes(p, 1)...)
	code, _, err := run(t, a, "status", "--json")
	want := "ork: warning: stale daemon build " + rev1 + "+modified=false differs from CLI build " + rev2 + "+modified=false; run ork daemon restart\n"
	if code != 0 || err != want {
		t.Fatalf("code diff: %d stderr %q want %q", code, err, want)
	}
}

func TestStaleFailsSafe(t *testing.T) {
	for _, tc := range []struct {
		name, own, theirs string
		registry          bool
		cat, diff         int
	}{
		{"missing registry", rev2 + "+modified=false", rev1 + "+modified=false", false, 0, 0},
		{"commits missing", rev2 + "+modified=false", rev1 + "+modified=false", true, 1, 0},
		{"diff error", rev2 + "+modified=false", rev1 + "+modified=false", true, 0, 128},
		{"own dirty", rev2 + "+modified=true", rev1 + "+modified=false", true, 0, 0},
		{"daemon dirty", rev2 + "+modified=false", rev1 + "+modified=true", true, 0, 0},
		{"same revision dirty", rev1 + "+modified=false", rev1 + "+modified=true", true, 0, 0},
		{"old", rev2, "old", true, 0, 0}, {"unknown", rev2, "unknown", true, 0, 0}, {"empty", rev2, "", true, 0, 0}, {"dev", rev2, "dev", true, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := t.TempDir()
			paths := []string{p}
			if !tc.registry {
				paths = nil
			}
			routes := staleRoutes(p, tc.diff)
			routes[0].Exit = tc.cat
			a, _ := staleApp(t, paths, routes...)
			a.BuildID = tc.own
			if stale, _ := a.staleBuild(context.Background(), tc.theirs); !stale {
				t.Fatal("unsafe build treated as current")
			}
		})
	}
}

func TestStaleTakesTheFirstProjectWithBothCommits(t *testing.T) {
	first, second, third := t.TempDir(), t.TempDir(), t.TempDir()
	routes := []clitest.Route{{Tool: "git", Args: []string{"-C", first, "cat-file", "-e"}, Prefix: true, Exit: 1}}
	routes = append(routes, staleRoutes(second, 0)...)
	a, fake := staleApp(t, []string{first, second, third}, routes...)
	if stale, _ := a.staleBuild(context.Background(), rev1+"+modified=false"); stale {
		t.Fatal("second project not used")
	}
	calls := fake.CallsTo("git")
	wantDiff := append([]string{"-C", second, "diff", "--quiet", rev1, rev2, "--"}, engineChangePathspec...)
	want := []clitest.Call{
		{Tool: "git", Args: []string{"-C", first, "cat-file", "-e", rev1 + "^{commit}"}},
		{Tool: "git", Args: []string{"-C", second, "cat-file", "-e", rev1 + "^{commit}"}},
		{Tool: "git", Args: []string{"-C", second, "cat-file", "-e", rev2 + "^{commit}"}},
		{Tool: "git", Args: wantDiff},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("git calls: %#v", calls)
	}
}

func TestEqualBuildsAskGitNothing(t *testing.T) {
	a, fake := staleApp(t, nil)
	if stale, note := a.staleBuild(context.Background(), a.BuildID); stale || note != "" {
		t.Fatalf("equal: %v %q", stale, note)
	}
	if calls := fake.CallsTo("git"); len(calls) != 0 {
		t.Fatalf("git called: %#v", calls)
	}
}
