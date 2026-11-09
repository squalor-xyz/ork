// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
