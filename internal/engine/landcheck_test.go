// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func checkLand(t *testing.T, env *landEnv, want LandCheck) {
	t.Helper()
	got := CheckLanding(context.Background(), env.deps, env.proj, env.run.Snapshot())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("landing check = %+v; want %+v", got, want)
	}
}

func TestCheckLandingCleanMainHasNoBlockers(t *testing.T) {
	checkLand(t, newLandEnv(t), LandCheck{Base: "main"})
}

func TestCheckLandingSortsAndDeduplicates(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "a.go"), "slice")
	writeFile(t, filepath.Join(env.W, "z.go"), "slice")
	replaceFake(t, filepath.Join(env.fakes, "gitw"), "#!/bin/sh\nif [ \"$3\" = status ]; then printf '?? z.go\\0?? a.go\\0?? z.go\\0'; exit 0; fi\nexec git \"$@\"\n")
	checkLand(t, env, LandCheck{Base: "main", Blockers: []string{"a.go", "z.go"}})
}

func TestCheckLandingIgnoresDirtyFilesTheMergeDoesNotTouch(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.P, ".slicer/index.json"), "base")
	gitRun(t, env.P, "add", ".slicer/index.json")
	gitRun(t, env.P, "commit", "-qm", "tracking baseline")
	gitRun(t, env.W, "merge", "--ff-only", "main")
	writeFile(t, filepath.Join(env.W, "shared.txt"), "slice")
	writeFile(t, filepath.Join(env.P, ".slicer/index.json"), "dirty")
	writeFile(t, filepath.Join(env.P, "scratch.txt"), "scratch")
	checkLand(t, env, LandCheck{Base: "main"})
}

func TestCheckLandingNamesATrackedFileModifiedInMain(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "slice")
	gitRun(t, env.W, "add", "shared.txt")
	gitRun(t, env.W, "commit", "-qm", "slice")
	writeFile(t, filepath.Join(env.P, "shared.txt"), "owner")
	checkLand(t, env, LandCheck{Base: "main", Blockers: []string{"shared.txt"}})
}

func TestCheckLandingCountsUncommittedWorktreeChanges(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "slice")
	writeFile(t, filepath.Join(env.P, "shared.txt"), "owner")
	checkLand(t, env, LandCheck{Base: "main", Blockers: []string{"shared.txt"}})
}

func TestCheckLandingNamesAnUntrackedFileInTheWay(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.P, "feature.txt"), "owner")
	checkLand(t, env, LandCheck{Base: "main", Blockers: []string{"feature.txt"}})
}

func TestCheckLandingHandlesRenamesAndSpaces(t *testing.T) {
	env := newLandEnv(t)
	gitRun(t, env.P, "mv", "shared.txt", "new name.txt")
	writeFile(t, filepath.Join(env.W, "shared.txt"), "slice")
	writeFile(t, filepath.Join(env.W, "new name.txt"), "slice")
	checkLand(t, env, LandCheck{Base: "main", Blockers: []string{"new name.txt", "shared.txt"}})
}

func TestCheckLandingReportsAMainCheckoutThatIsNotOnABranch(t *testing.T) {
	env := newLandEnv(t)
	gitRun(t, env.P, "checkout", "--detach")
	got := CheckLanding(context.Background(), env.deps, env.proj, env.run.Snapshot())
	if !strings.HasPrefix(got.Reason, "the main checkout is not on a branch: ") || got.Error != "" || len(got.Blockers) != 0 {
		t.Fatalf("branch warning = %+v; want landing's detached-branch reason", got)
	}
}

func TestCheckLandingReportsMainOnTheSliceBranch(t *testing.T) {
	env := newLandEnv(t)
	rec := env.run.Snapshot()
	rec.Branch = "main"
	got := CheckLanding(context.Background(), env.deps, env.proj, rec)
	if !reflect.DeepEqual(got, LandCheck{Base: "main", Reason: "the main checkout is on the slice branch main"}) {
		t.Fatalf("slice-branch warning = %+v", got)
	}
}

func TestCheckLandingGitFailureIsAnErrorNotAWarning(t *testing.T) {
	for _, command := range []string{"symbolic-ref", "merge-base", "diff", "ls-files", "status", "rev-parse"} {
		t.Run(command, func(t *testing.T) {
			env := newLandEnv(t)
			writeFile(t, filepath.Join(env.P, "feature.txt"), "owner")
			replaceFake(t, filepath.Join(env.fakes, "gitw"), "#!/bin/sh\nif [ \"$3\" = "+command+" ]; then echo broken >&2; exit 2; fi\nexec git \"$@\"\n")
			got := CheckLanding(context.Background(), env.deps, env.proj, env.run.Snapshot())
			if got.Error == "" || got.Reason != "" || len(got.Blockers) != 0 {
				t.Fatalf("%s failure = %+v; want Error only", command, got)
			}
		})
	}
}

func TestCheckLandingNeverWrites(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.P, "feature.txt"), "owner")
	checkLand(t, env, LandCheck{Base: "main", Blockers: []string{"feature.txt"}})
	b, err := os.ReadFile(filepath.Join(env.fakes, "git.log"))
	if err != nil {
		t.Fatal(err)
	}
	var ops []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			t.Fatalf("invalid git call: %s", line)
		}
		ops = append(ops, fields[2])
	}
	if want := []string{"symbolic-ref", "merge-base", "diff", "ls-files", "status", "rev-parse"}; !reflect.DeepEqual(ops, want) {
		t.Fatalf("git operations = %v; want reads %v", ops, want)
	}
}

func TestCheckLandingIgnoresTrackingOnlyOverlap(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.P, ".slicer/index.json"), "owner")
	writeFile(t, filepath.Join(env.W, ".slicer/index.json"), "slice")
	checkLand(t, env, LandCheck{Base: "main"})
}

func TestCheckLandingIncludesTrackingConfigOverlap(t *testing.T) {
	env := newLandEnv(t)
	for _, p := range []string{".slicer/config.json", ".slicer/relay-roles.json", ".slicer/index.json"} {
		writeFile(t, filepath.Join(env.P, p), "owner")
		writeFile(t, filepath.Join(env.W, p), "slice")
	}
	checkLand(t, env, LandCheck{Base: "main", Blockers: []string{".slicer/config.json", ".slicer/relay-roles.json"}})
}

func TestCheckLandingSubdirectoryProject(t *testing.T) {
	env := newLandEnv(t)
	env.proj.Path = filepath.Join(env.P, "lib")
	if err := os.MkdirAll(env.proj.Path, 0755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"lib/.slicer/index.json", "other/.slicer/index.json", "lib/a b.go"} {
		writeFile(t, filepath.Join(env.P, p), "owner")
		writeFile(t, filepath.Join(env.W, p), "slice")
	}
	gitRun(t, env.W, "config", "diff.relative", "true")
	checkLand(t, env, LandCheck{Base: "main", Blockers: []string{"lib/a b.go", "other/.slicer/index.json"}})
}
