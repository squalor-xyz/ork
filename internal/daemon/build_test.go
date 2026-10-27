// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
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
		t.Fatal("worktree HEAD used instead of main")
	}
	git("branch", "-D", "main")
	if buildProject(t, root, base)["behind_main"] {
		t.Fatal("missing main reported behind")
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
