// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

func changelogConflict(t *testing.T, name string, ancestor bool) *landEnv {
	t.Helper()
	env := newLandEnv(t)
	if ancestor {
		env.moveBase(name, "## Unreleased\n\n- old\n\n## v1.0.0\n\n- released\n")
		gitRun(t, env.W, "merge", "--no-edit", "main")
	}
	writeFile(t, filepath.Join(env.W, name), "## Unreleased\n\n- S1: branch thing.\n  Docs updated: `docs/design.md`.\n\n- old\n\n## v1.0.0\n\n- released\n")
	env.moveBase(name, "## Unreleased\n\n- S2: base thing.\n  Docs updated: `docs/design.md`.\n\n- old\n\n## v1.0.0\n\n- released\n")
	return env
}

func TestLandResolvesAChangelogOnlyConflict(t *testing.T) {
	env := changelogConflict(t, "CHANGELOG.md", true)
	env.beforeCheck = func() {
		if !env.run.Snapshot().LandRecheck {
			t.Error("LandRecheck cleared before re-check")
		}
	}
	env.approve()
	rec := env.run.Snapshot()
	if rec.LandFixes != 0 {
		t.Fatalf("LandFixes = %d, want 0", rec.LandFixes)
	}
	requireState(t, env.e, runrec.StateCommitted)
	if got := gitRun(t, env.P, "show", "main:CHANGELOG.md"); got != "## Unreleased\n\n- S1: branch thing.\n  Docs updated: `docs/design.md`.\n\n- S2: base thing.\n  Docs updated: `docs/design.md`.\n\n- old\n\n## v1.0.0\n\n- released" {
		t.Errorf("merged content: %q", got)
	}
	if rec.LandRecheck {
		t.Error("LandRecheck still set after green check")
	}
	if indexOf(env.gitCalls(), "P: merge --ff-only") < 0 || indexOf(env.log("slicer.log"), "land ") < 0 {
		t.Error("landing did not finish")
	}
	for _, req := range env.runner.requests() {
		if strings.Contains(req.Brief, "## Landing blocked") {
			t.Error("ran a landing fix round")
		}
	}
}

func TestLandChangelogResolutionCommitsTheMerge(t *testing.T) {
	env := changelogConflict(t, "CHANGELOG.md", true)
	env.approve()
	var calls []string
	for _, w := range env.run.Snapshot().Writes {
		if w.Tool == "git" {
			calls = append(calls, strings.Join(w.Argv, " "))
		}
	}
	for _, want := range []string{"add -- CHANGELOG.md", "commit -m Merge main into " + item} {
		if indexOf(calls, want) < 0 {
			t.Errorf("writes[] lacks %q: %v", want, calls)
		}
	}
	if t.Failed() {
		return
	}
	merge := gitRun(t, env.P, "log", "--merges", "-1", "--format=%H", "main")
	if got := gitRun(t, env.P, "show", "-s", "--format=%s", merge); got != "Merge main into "+item {
		t.Errorf("message: %q", got)
	}
	if got := strings.Fields(gitRun(t, env.P, "show", "-s", "--format=%P", merge)); len(got) != 2 {
		t.Errorf("parents: %v", got)
	}
}

// pauseOnConflict approves and requires the landing to pause on exactly the
// unmerged files in files, with no role round. markers says CHANGELOG.md must
// still hold its conflict markers. It returns the pause reason.
func pauseOnConflict(t *testing.T, env *landEnv, files string, markers bool) string {
	t.Helper()
	env.approve()
	rec := requireConflictPause(t, env)
	if got := unmerged(t, env); got != files {
		t.Errorf("unmerged = %q, want %q", got, files)
	}
	for _, name := range strings.Fields(files) {
		if !strings.Contains(rec.Reason, name) {
			t.Errorf("reason lacks %s: %q", name, rec.Reason)
		}
	}
	if markers {
		b, err := os.ReadFile(filepath.Join(env.W, "CHANGELOG.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "<<<<<<<") {
			t.Errorf("conflict markers lost: %s", b)
		}
	}
	return rec.Reason
}

func TestLandChangelogResolverIgnoresOtherChangelogNames(t *testing.T) {
	env := changelogConflict(t, "docs/CHANGELOG.md", true)
	pauseOnConflict(t, env, "docs/CHANGELOG.md", false)
	if indexOf(env.gitCalls(), "W: add -- CHANGELOG.md") >= 0 {
		t.Error("attempted resolution of nested changelog")
	}
}

func TestLandChangelogResolverFailureFallsBack(t *testing.T) {
	env := changelogConflict(t, "CHANGELOG.md", true)
	script := strings.Replace(fakeGitScript, "exec git", "if [ \"$3\" = merge ] && [ \"$4\" = -m ]; then touch \"$d/add-fails\"; fi\nexec git", 1)
	replaceFake(t, filepath.Join(env.fakes, "gitw"), script)
	reason := pauseOnConflict(t, env, "CHANGELOG.md", true)
	if !strings.Contains(reason, "ork could not merge CHANGELOG.md: git add") {
		t.Errorf("reason lacks the resolver failure: %q", reason)
	}
	if indexOf(env.gitCalls(), "W: checkout --merge -- CHANGELOG.md") < 0 {
		t.Error("markers were not restored")
	}
}

func TestLandChangelogMergeCommitFailurePausesWithGitsMessage(t *testing.T) {
	env := changelogConflict(t, "CHANGELOG.md", true)
	script := strings.Replace(fakeGitScript, "exec git", "if [ \"$3\" = merge ] && [ \"$4\" = -m ]; then touch \"$d/commit-fails\"; fi\nexec git", 1)
	replaceFake(t, filepath.Join(env.fakes, "gitw"), script)
	reason := pauseOnConflict(t, env, "", false)
	if !strings.Contains(reason, "git commit") || !strings.Contains(reason, "commit broke") {
		t.Errorf("reason lacks git's message: %q", reason)
	}
}

func TestLandChangelogResolverNeedsAllThreeStages(t *testing.T) {
	env := changelogConflict(t, "CHANGELOG.md", false)
	pauseOnConflict(t, env, "CHANGELOG.md", true)
	if indexOf(env.gitCalls(), "W: add -- CHANGELOG.md") >= 0 {
		t.Error("attempted resolution without an ancestor stage")
	}
}

func TestMergeChangelogEntries(t *testing.T) {
	const prefix = "# Changelog\n\n## Unreleased\n\n"
	const suffix = "## v1.0.0\n\n- released\n"
	const branch = "- S1: branch thing.\n  Docs updated: `docs/design.md`."
	const main = "- S2: base thing.\n  Docs updated: `docs/design.md`."
	text := func(entries ...string) string {
		if len(entries) == 0 {
			return prefix + suffix
		}
		return prefix + strings.Join(entries, "\n\n") + "\n\n" + suffix
	}
	tests := []struct {
		name, base, ours, theirs, want string
		ok                             bool
	}{
		{"reviewer example", text(), text(branch), text(main), text(branch, main), true},
		{"multiline with base", text("- old"), text(branch, "- old"), text(main, "- old"), text(branch, main, "- old"), true},
		{"single line", text("- old"), text("- branch", "- old"), text("- main", "- old"), text("- branch", "- main", "- old"), true},
		{"top and bottom", text("- old"), text(branch, "- old"), text("- old", main), text(branch, main, "- old"), true},
		{"identical added", text("- old"), text(branch, "- old"), text(branch, "- old"), text(branch, "- old"), true},
		{"edited base", text("- old"), text("- edited"), text("- old"), "", false},
		{"removed base", text("- old"), text(), text("- old"), "", false},
		{"reordered base", text("- one", "- two"), text("- two", "- one"), text("- one", "- two"), "", false},
		{"prefix changed", text(), "changed\n" + text(branch), text(main), "", false},
		{"suffix changed", text(), text(branch), text(main) + "changed\n", "", false},
		{"no heading", text(), "- branch\n", text(main), "", false},
		{"base no heading", "- old\n", text(branch), text(main), "", false},
		{"stray paragraph", text(), text("paragraph"), text(main), "", false},
		{"stray after entry", text(), text(branch, "paragraph"), text(main), "", false},
		{"unseparated entries", text(), text("- one\n- two"), text(main), "", false},
		{"duplicate added", text(), text(branch, branch), text(main), "", false},
		{"duplicate base", text("- old", "- old"), text("- old", "- old"), text("- old", "- old"), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mergeChangelogEntries(tt.base, tt.ours, tt.theirs)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestChangelogWriteErrorRestoresMarkers(t *testing.T) {
	env := changelogConflict(t, "CHANGELOG.md", true)
	gitRun(t, env.W, "add", "-A")
	gitRun(t, env.W, "commit", "-m", "branch entry")
	if err := exec.Command("git", "-C", env.W, "merge", "--no-edit", "main").Run(); err == nil {
		t.Fatal("expected conflict")
	}
	called := false
	err := env.e.writeChangelog(context.Background(), env.run.Snapshot(), "partial", func(path string, b []byte, mode os.FileMode) error {
		called = true
		if err := os.WriteFile(path, b, mode); err != nil {
			t.Fatal(err)
		}
		return errors.New("injected partial write failure")
	})
	if err == nil || !strings.Contains(err.Error(), "injected partial write failure") || !called {
		t.Fatalf("write error = %v, called = %v", err, called)
	}
	b, err := os.ReadFile(filepath.Join(env.W, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "<<<<<<<") {
		t.Errorf("markers not restored: %s", b)
	}
	if got := gitRun(t, env.W, "diff", "--name-only", "--diff-filter=U"); got != "CHANGELOG.md" {
		t.Errorf("unmerged = %q", got)
	}
	if indexOf(env.gitCalls(), "W: checkout --merge -- CHANGELOG.md") < 0 {
		t.Error("no marker restoration")
	}
}
