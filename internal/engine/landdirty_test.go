// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestLaunchTrackingListedSlices(t *testing.T) {
	for _, tracked := range []string{"", ".slicer/slices/S901.json", ".slicer/render/slices/S901.md"} {
		t.Run("tracked="+tracked, func(t *testing.T) {
			h := launchTrackingHarness(t, false)
			if tracked != "" {
				writeFile(t, filepath.Join(h.P, tracked), "old\n")
				gitRun(t, h.P, "add", "--", tracked)
				gitRun(t, h.P, "commit", "-qm", "tracked slice")
			}
			paths := []string{".slicer/index.json", ".slicer/render/slices/S900.md", ".slicer/render/slices/S901.md", ".slicer/slices/S900.json", ".slicer/slices/S901.json"}
			var wantCommit, wantDirty []string
			for _, p := range paths {
				writeFile(t, filepath.Join(h.P, p), "new\n")
				if p == tracked {
					wantDirty = append(wantDirty, " M "+p)
				} else {
					wantCommit = append(wantCommit, p)
				}
			}
			// Unlisted S902 and has_slice:false S903 must stay with the owner,
			// along with staged config, matrix and ordinary project files.
			for _, p := range []string{".slicer/config.json", ".slicer/relay-roles.json", ".slicer/slices/S902.json", "notes.txt"} {
				writeFile(t, filepath.Join(h.P, p), "owner\n")
				gitRun(t, h.P, "add", "--", p)
				wantDirty = append(wantDirty, "A  "+p)
			}
			writeFile(t, filepath.Join(h.P, ".slicer/slices/S903.json"), "owner\n")
			wantDirty = append(wantDirty, "?? .slicer/slices/S903.json")
			fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", h.P}, Stdout: `[{"id":"S900","has_slice":true},{"id":"S901","has_slice":true},{"id":"S903","has_slice":false}]`})
			e := &Engine{proj: h.proj, d: h.deps}
			e.d.Slicer = slicercli.Client{Bin: fake.Bin("slicer")}
			reason := e.commitLaunchTracking(context.Background(), "S900")
			committed := gitRun(t, h.P, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
			dirty := strings.Split(gitRun(t, h.P, "status", "--porcelain"), "\n")
			// gitRun trims the first status column's leading space.
			for i := range dirty {
				dirty[i] = strings.TrimSpace(dirty[i])
			}
			for i := range wantDirty {
				wantDirty[i] = strings.TrimSpace(wantDirty[i])
			}
			sort.Strings(dirty)
			sort.Strings(wantDirty)
			if reason != "" || committed != strings.Join(wantCommit, "\n") || strings.Join(dirty, "\n") != strings.Join(wantDirty, "\n") {
				t.Errorf("launch reason %q; committed paths %q, want %q; owner status %q, want %q", reason, committed, wantCommit, dirty, wantDirty)
			}
		})
	}
}

// S254: the landing looks at the main checkout before it merges, and again
// before the fast-forward. These tests run real git in temp repos (newLandEnv).

// dirtyStop is the reason a landing pauses with when the main checkout has
// uncommitted changes to files the slice changes.
func dirtyStop(P, names string) string {
	return "landing stopped: uncommitted changes in " + P + " to files the slice changes: " + names + "; commit or stash them"
}

func appendLine(t *testing.T, path, text string) {
	t.Helper()
	b, _ := os.ReadFile(path)
	writeFile(t, path, string(b)+text)
}

// requireLandPause checks the run is paused in the landing with this reason.
func requireLandPause(t *testing.T, env *landEnv, reason string) {
	t.Helper()
	rec := requireState(t, env.e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingError || rec.Pending.State != runrec.StateLand {
		t.Errorf("pending %+v, want an error pause in the landing", rec.Pending)
	}
	if rec.Reason != reason {
		t.Errorf("reason %q, want %q", rec.Reason, reason)
	}
}

// requireNothingLanded checks no merge ran, no re-check ran and slicer land
// did not run.
func requireNothingLanded(t *testing.T, env *landEnv, mainTip string) {
	t.Helper()
	for _, l := range env.gitCalls() {
		if strings.HasPrefix(l, "W: merge -m ") || strings.HasPrefix(l, "P: merge --ff-only") {
			t.Errorf("git call %q although the landing stopped early", l)
		}
	}
	if len(env.seen) != 0 {
		t.Errorf("%d re-checks ran, want 0", len(env.seen))
	}
	if indexOf(env.log("slicer.log"), "land ") >= 0 {
		t.Errorf("slicer land ran")
	}
	if got := gitRun(t, env.P, "rev-parse", "main"); got != mainTip {
		t.Errorf("main moved: %s, was %s", got, mainTip)
	}
}

func TestLandStopsBeforeTheMergeWhenMainHasChangesTheSliceTouches(t *testing.T) {
	env := newLandEnv(t)
	moved := env.moveBase("base.txt", "from main\n")
	writeFile(t, filepath.Join(env.W, "shared.txt"), "slice\n")
	writeFile(t, filepath.Join(env.P, "shared.txt"), "owner\n")
	env.approve()
	requireLandPause(t, env, dirtyStop(env.P, "shared.txt"))
	requireNothingLanded(t, env, moved)
	if b, _ := os.ReadFile(filepath.Join(env.P, "shared.txt")); string(b) != "owner\n" {
		t.Errorf("the owner's file changed: %q", b)
	}
}

func TestLandDirtyStopNamesAtMostFiveFiles(t *testing.T) {
	env := newLandEnv(t)
	// git lists the tracked change first, then the untracked files: the names
	// are sorted by the landing, not by git.
	for _, n := range []string{"shared", "f", "a", "e", "c", "b", "d"} {
		writeFile(t, filepath.Join(env.W, n+".txt"), "slice\n")
		writeFile(t, filepath.Join(env.P, n+".txt"), "owner\n")
	}
	env.approve()
	requireLandPause(t, env, dirtyStop(env.P, "a.txt, b.txt, c.txt, d.txt, e.txt +2 more"))
}

func TestLandIgnoresUncommittedFilesTheSliceDoesNotTouch(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.P, "shared.txt"), "owner\n")
	writeFile(t, filepath.Join(env.P, "scratch.txt"), "scratch\n")
	writeFile(t, filepath.Join(env.P, "notes.txt"), "notes\n")
	gitRun(t, env.P, "add", "notes.txt")
	// shared.txt is dirty but the slice does not change it.
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	if got := gitRun(t, env.P, "log", "-1", "--format=%s", "main"); got != gitTitle {
		t.Errorf("main tip %q, want the slice's commit", got)
	}
	got := strings.Split(gitRun(t, env.P, "status", "--porcelain"), "\n")
	sort.Strings(got)
	if want := []string{" M shared.txt", "?? scratch.txt", "A  notes.txt"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("status %q, want %q", got, want)
	}
}

func TestLandChecksMainAgainBeforeTheFastForward(t *testing.T) {
	env := newLandEnv(t)
	moved := env.moveBase("base.txt", "from main\n")
	writeFile(t, filepath.Join(env.W, "shared.txt"), "slice\n")
	env.beforeCheck = func() { writeFile(t, filepath.Join(env.P, "shared.txt"), "owner\n") }
	env.approve()
	rec := requireState(t, env.e, runrec.StatePaused)
	if want := dirtyStop(env.P, "shared.txt"); rec.Reason != want {
		t.Errorf("reason %q, want %q", rec.Reason, want)
	}
	if len(env.seen) != 1 {
		t.Errorf("%d re-checks, want 1", len(env.seen))
	}
	if rec.LandRecheck {
		t.Errorf("the re-check marker stays set after a green re-check")
	}
	if got := gitRun(t, env.P, "rev-parse", "main"); got != moved {
		t.Errorf("main moved to %s, was %s", got, moved)
	}
	if i := indexOf(env.gitCalls(), "P: merge --ff-only"); i >= 0 {
		t.Errorf("the fast-forward ran: %q", env.gitCalls())
	}
	gitRun(t, env.P, "checkout", "--", "shared.txt")
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, env.e, runrec.StateCommitted)
	if len(env.seen) != 1 {
		t.Errorf("%d re-checks after the retry, want no second one", len(env.seen))
	}
}

func TestLandFastForwardFailureIsOneLine(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.fakes, "ff-fails"), "")
	env.approve()
	rec := requireState(t, env.e, runrec.StatePaused)
	if strings.Contains(rec.Reason, "\n") || !strings.HasPrefix(rec.Reason, "git merge --ff-only "+item+": ") || !strings.Contains(rec.Reason, "fatal: one fatal: two") {
		t.Errorf("reason %q", rec.Reason)
	}
}

func TestLandDirtyCheckErrorPauses(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.fakes, "status-fails"), env.P)
	env.approve()
	rec := requireState(t, env.e, runrec.StatePaused)
	if !strings.HasPrefix(rec.Reason, "git status: ") || strings.Contains(rec.Reason, "\n") {
		t.Errorf("reason %q", rec.Reason)
	}
	requireNothingLanded(t, env, env.baseTip)
}

// trackingEnv sets up the Q1 (b) case: main holds .slicer/log.jsonl with a
// union merge driver, the worktree has it and appends a line of its own, and
// the main checkout holds an uncommitted line of the owner's and a staged file.
func trackingEnv(t *testing.T, opts ...func(*registry.Project)) *landEnv {
	t.Helper()
	env := newLandEnv(t, opts...)
	trackingBase(t, env, ".slicer")
	return env
}

// trackingBase commits dir/log.jsonl and a union driver for it on main, takes
// them into the worktree, appends "branch" there and "owner" in the main
// checkout (uncommitted).
func trackingBase(t *testing.T, env *landEnv, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(env.P, dir, "log.jsonl"), "a\n")
	writeFile(t, filepath.Join(env.P, dir, ".gitattributes"), "log.jsonl merge=union\n")
	gitRun(t, env.P, "add", "-A")
	gitRun(t, env.P, "commit", "-q", "-m", "tracking base")
	gitRun(t, env.W, "merge", "--ff-only", "main")
	appendLine(t, filepath.Join(env.W, dir, "log.jsonl"), "branch\n")
	appendLine(t, filepath.Join(env.P, dir, "log.jsonl"), "owner\n")
}

const trackingSubject = "Tracking edits on main before landing " + item

func trackingCommits(t *testing.T, env *landEnv) []string {
	t.Helper()
	out := gitRun(t, env.P, "log", "--format=%H", "--grep=^"+trackingSubject+"$", "main")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func TestLandCommitsUncommittedTrackingEditsFirst(t *testing.T) {
	env := trackingEnv(t)
	writeFile(t, filepath.Join(env.P, "notes.txt"), "notes\n")
	gitRun(t, env.P, "add", "notes.txt")
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	commits := trackingCommits(t, env)
	if len(commits) != 1 {
		t.Fatalf("%d tracking commits on main, want 1", len(commits))
	}
	if got := gitRun(t, env.P, "show", "--name-only", "--format=", commits[0]); got != ".slicer/log.jsonl" {
		t.Errorf("the tracking commit holds %q, want only .slicer/log.jsonl", got)
	}
	log := gitRun(t, env.P, "show", "main:.slicer/log.jsonl")
	if strings.Count(log, "owner") != 1 || strings.Count(log, "branch") != 1 {
		t.Errorf("log.jsonl on main: %q, want owner and branch once each", log)
	}
	if got := gitRun(t, env.P, "status", "--porcelain"); got != "A  notes.txt" {
		t.Errorf("status %q, want only the staged notes.txt", got)
	}
	if len(env.seen) != 1 {
		t.Fatalf("%d re-checks, want 1", len(env.seen))
	}
	if s := env.seen[0]; s.mainTip != commits[0] || !s.hasBase {
		t.Errorf("the re-check saw %+v, want main at the tracking commit %s", s, commits[0])
	}
	// The tracking commit is a git write of the run: by name, never a bare dot.
	calls := env.gitCalls()
	if indexOf(calls, "P: add -A -- :(literal).slicer/log.jsonl") < 0 || indexOf(calls, "P: commit -m "+trackingSubject+" -- :(literal).slicer/log.jsonl") < 0 {
		t.Errorf("git calls %q", calls)
	}
}

func TestLandTrackingCommitSkipsWhenTheSliceDoesNotTouchThem(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.P, ".slicer", "slices", "S99.json"), "{}\n")
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	if n := len(trackingCommits(t, env)); n != 0 {
		t.Errorf("%d tracking commits, want none", n)
	}
	// Nothing was committed before the landing: the base did not move, so
	// there was no merge-in and no re-check. (The ID done commit after
	// slicer land sweeps up whatever is dirty under .slicer; that is S119's,
	// not this slice's.)
	if len(env.seen) != 0 {
		t.Errorf("%d re-checks ran, want 0 (the base must not have moved)", len(env.seen))
	}
	for _, l := range env.gitCalls() {
		if strings.HasPrefix(l, "W: merge -m ") {
			t.Errorf("merge-in %q although nothing was committed on main", l)
		}
	}
}

func TestLandMixedDirtyStopsAndCommitsNothing(t *testing.T) {
	env := trackingEnv(t)
	tip := gitRun(t, env.P, "rev-parse", "main")
	writeFile(t, filepath.Join(env.W, "shared.txt"), "slice\n")
	writeFile(t, filepath.Join(env.P, "shared.txt"), "owner\n")
	env.approve()
	requireLandPause(t, env, dirtyStop(env.P, "shared.txt"))
	requireNothingLanded(t, env, tip)
	if n := len(trackingCommits(t, env)); n != 0 {
		t.Errorf("%d tracking commits, want none", n)
	}
	if got := gitRun(t, env.P, "status", "--porcelain"); !strings.Contains(got, "M .slicer/log.jsonl") {
		t.Errorf("status %q, want log.jsonl still modified", got)
	}
}

func TestLandDoesNotCommitConfigOrMatrixEdits(t *testing.T) {
	for _, name := range []string{"relay-roles.json", "config.json"} {
		t.Run(name, func(t *testing.T) {
			env := newLandEnv(t)
			writeFile(t, filepath.Join(env.W, ".slicer", name), "{\"slice\":1}\n")
			writeFile(t, filepath.Join(env.P, ".slicer", name), "{\"owner\":1}\n")
			env.approve()
			requireLandPause(t, env, dirtyStop(env.P, ".slicer/"+name))
			requireNothingLanded(t, env, env.baseTip)
			if n := len(trackingCommits(t, env)); n != 0 {
				t.Errorf("%d tracking commits, want none", n)
			}
		})
	}
}

// A failed commit after a good add leaves the files staged: the reason says so.
func TestLandTrackingCommitFailurePauses(t *testing.T) {
	env := trackingEnv(t)
	tip := gitRun(t, env.P, "rev-parse", "main")
	// Arm the one-shot commit failure when the tracking edits are first staged
	// in P, so the commit in the worktree at approval is not the one that fails.
	script := strings.Replace(fakeGitScript, "exec git", "if [ \"$2\" = \""+env.P+"\" ] && [ \"$3\" = add ] && [ ! -e \"$d/armed\" ]; then touch \"$d/armed\" \"$d/commit-fails\"; fi\nexec git", 1)
	replaceFake(t, filepath.Join(env.fakes, "gitw"), script)
	env.approve()
	rec := requireState(t, env.e, runrec.StatePaused)
	if !strings.HasPrefix(rec.Reason, "git commit: ") || strings.Contains(rec.Reason, "\n") || !strings.Contains(rec.Reason, "commit broke") || !strings.HasSuffix(rec.Reason, "; ork staged .slicer/log.jsonl") {
		t.Errorf("reason %q", rec.Reason)
	}
	requireNothingLanded(t, env, tip)
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, env.e, runrec.StateCommitted)
	if n := len(trackingCommits(t, env)); n != 1 {
		t.Errorf("%d tracking commits after the retry, want 1", n)
	}
}

func TestLandTrackingEditDuringTheRecheckGoesBackToTheMergeOnce(t *testing.T) {
	env := trackingEnv(t)
	n := 0
	env.beforeCheck = func() {
		n++
		if n == 1 {
			appendLine(t, filepath.Join(env.P, ".slicer", "log.jsonl"), "during\n")
		}
	}
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	if got := len(trackingCommits(t, env)); got != 2 {
		t.Errorf("%d tracking commits, want 2", got)
	}
	if len(env.seen) != 2 {
		t.Fatalf("%d re-checks, want 2", len(env.seen))
	}
	log := gitRun(t, env.P, "show", "main:.slicer/log.jsonl")
	for _, want := range []string{"owner", "branch", "during"} {
		if strings.Count(log, want) != 1 {
			t.Errorf("log.jsonl on main: %q, want %q once", log, want)
		}
	}
	if s := env.seen[1]; !s.hasBase || !s.treeClean {
		t.Errorf("the second re-check saw %+v", s)
	}
}

func TestLandTrackingEditOnEveryRecheckPausesAfterTheSecond(t *testing.T) {
	env := trackingEnv(t)
	env.beforeCheck = func() { appendLine(t, filepath.Join(env.P, ".slicer", "log.jsonl"), "again\n") }
	env.approve()
	requireLandPause(t, env, dirtyStop(env.P, ".slicer/log.jsonl"))
	if len(env.seen) != 2 {
		t.Errorf("%d re-checks, want 2", len(env.seen))
	}
	if got := len(trackingCommits(t, env)); got != 2 {
		t.Errorf("%d tracking commits, want 2", got)
	}
	if indexOf(env.gitCalls(), "P: merge --ff-only") >= 0 {
		t.Errorf("the fast-forward ran")
	}
}

// The project is a subdirectory of the repository (S254, the prism-ui case):
// tracking paths are matched under the project's folder, and the commit names
// them from there.
func subProject(sub string) func(*registry.Project) {
	return func(p *registry.Project) { p.Path = filepath.Join(p.Path, sub) }
}

func TestLandCommitsTrackingEditsOfASubdirectoryProject(t *testing.T) {
	env := newLandEnv(t, subProject("libs/ui"))
	trackingBase(t, env, "libs/ui/.slicer")
	// A sibling project's dirty tracking file that the slice does not touch is
	// not this landing's to commit.
	writeFile(t, filepath.Join(env.P, "libs", "other", ".slicer", "log.jsonl"), "sibling\n")
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	commits := trackingCommits(t, env)
	if len(commits) != 1 {
		t.Fatalf("%d tracking commits, want 1", len(commits))
	}
	if got := gitRun(t, env.P, "show", "--name-only", "--format=", commits[0]); got != "libs/ui/.slicer/log.jsonl" {
		t.Errorf("the tracking commit holds %q", got)
	}
	log := gitRun(t, env.P, "show", "main:libs/ui/.slicer/log.jsonl")
	if strings.Count(log, "owner") != 1 || strings.Count(log, "branch") != 1 {
		t.Errorf("log.jsonl on main: %q", log)
	}
	if got := gitRun(t, env.P, "status", "--porcelain", "--untracked-files=all"); got != "?? libs/other/.slicer/log.jsonl" {
		t.Errorf("status %q, want only the sibling's file", got)
	}
}

func TestLandStopsOnASiblingProjectsTrackingFile(t *testing.T) {
	env := newLandEnv(t, subProject("libs/ui"))
	trackingBase(t, env, "libs/ui/.slicer")
	writeFile(t, filepath.Join(env.W, "libs", "other", ".slicer", "log.jsonl"), "slice\n")
	writeFile(t, filepath.Join(env.P, "libs", "other", ".slicer", "log.jsonl"), "sibling\n")
	env.approve()
	requireLandPause(t, env, dirtyStop(filepath.Join(env.P, "libs", "ui"), "libs/other/.slicer/log.jsonl"))
	if n := len(trackingCommits(t, env)); n != 0 {
		t.Errorf("%d tracking commits, want none", n)
	}
}

// The slice renames a file the main checkout has changed: the fast-forward
// writes the old path too, so the old path counts.
func TestLandStopsWhenTheSliceRenamesAFileMainHasChanged(t *testing.T) {
	env := newLandEnv(t)
	gitRun(t, env.W, "mv", "shared.txt", "moved.txt")
	writeFile(t, filepath.Join(env.P, "shared.txt"), "owner\n")
	env.approve()
	requireLandPause(t, env, dirtyStop(env.P, "shared.txt"))
	requireNothingLanded(t, env, env.baseTip)
}

func TestTrackingPath(t *testing.T) {
	for rel, want := range map[string]bool{
		".slicer/log.jsonl":          true,
		".slicer/slices/S04.json":    true,
		".slicer/render/ROADMAP.md":  true,
		".slicer/.gitattributes":     true,
		".slicer/config.json":        false,
		".slicer/relay-roles.json":   false,
		".slicer/sub/config.json":    true,
		".slicer":                    false,
		".slicerx/log.jsonl":         false,
		"decisions/1.md":             false,
		"x/.slicer/log.jsonl":        false,
		"shared.txt":                 false,
		".slicer/relay-roles.json.x": true,
	} {
		if got := trackingPath(rel); got != want {
			t.Errorf("trackingPath(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestSplitTrackingMatchesUnderTheProjectPrefix(t *testing.T) {
	files := []string{"libs/other/.slicer/log.jsonl", "libs/ui/.slicer/config.json", "libs/ui/.slicer/log.jsonl", "libs/ui/src/a.go", "libs/uix/.slicer/log.jsonl"}
	tracking, other := splitTracking("libs/ui/", files)
	if got := strings.Join(tracking, " "); got != ".slicer/log.jsonl" {
		t.Errorf("tracking %q, want the project's log only, relative to the project", got)
	}
	if got := strings.Join(other, " "); got != "libs/other/.slicer/log.jsonl libs/ui/.slicer/config.json libs/ui/src/a.go libs/uix/.slicer/log.jsonl" {
		t.Errorf("other %q", got)
	}
	tracking, other = splitTracking("", []string{".slicer/log.jsonl", "libs/ui/.slicer/log.jsonl"})
	if len(tracking) != 1 || tracking[0] != ".slicer/log.jsonl" || len(other) != 1 || other[0] != "libs/ui/.slicer/log.jsonl" {
		t.Errorf("at the repository root: tracking %q, other %q", tracking, other)
	}
}

func TestDirtyReasonFoldsToOneLine(t *testing.T) {
	got := dirtyReason("/p", []string{"a b.txt", "x\ny.txt", "\x1b[31mz\x00.txt"})
	if want := "landing stopped: uncommitted changes in /p to files the slice changes: a b.txt, x y.txt, z.txt; commit or stash them"; got != want {
		t.Errorf("reason %q, want %q", got, want)
	}
}

// A failing read while the main checkout is looked at pauses the landing with
// one line that starts with the failing command, and nothing is merged.
func TestLandDirtyCheckReadErrorsPause(t *testing.T) {
	for _, tc := range []struct{ op, want string }{{"diff", "git diff: "}, {"rev-parse", "git rev-parse: "}} {
		t.Run(tc.op, func(t *testing.T) {
			env := newLandEnv(t)
			if tc.op == "rev-parse" {
				writeFile(t, filepath.Join(env.W, ".slicer", "log.jsonl"), "branch\n")
				writeFile(t, filepath.Join(env.P, ".slicer", "log.jsonl"), "owner\n")
			} else {
				writeFile(t, filepath.Join(env.W, "shared.txt"), "slice\n")
				writeFile(t, filepath.Join(env.P, "shared.txt"), "owner\n")
			}
			script := strings.Replace(fakeGitScript, "exec git", "if [ \"$2\" = \""+env.P+"\" ] && [ \"$3\" = "+tc.op+" ]; then echo \"read broke\" >&2; exit 1; fi\nexec git", 1)
			replaceFake(t, filepath.Join(env.fakes, "gitw"), script)
			env.approve()
			rec := requireState(t, env.e, runrec.StatePaused)
			if !strings.HasPrefix(rec.Reason, tc.want) || strings.Contains(rec.Reason, "\n") || !strings.Contains(rec.Reason, "read broke") {
				t.Errorf("reason %q, want it to start %q", rec.Reason, tc.want)
			}
			requireNothingLanded(t, env, env.baseTip)
		})
	}
}

func TestLandTrackingAddFailurePauses(t *testing.T) {
	env := trackingEnv(t)
	tip := gitRun(t, env.P, "rev-parse", "main")
	script := strings.Replace(fakeGitScript, "for op in add commit; do", "if [ \"$2\" = \""+env.P+"\" ] && [ \"$3\" = add ] && [ ! -e \"$d/added\" ]; then touch \"$d/added\"; echo \"add broke\" >&2; exit 1; fi\nfor op in add commit; do", 1)
	replaceFake(t, filepath.Join(env.fakes, "gitw"), script)
	env.approve()
	rec := requireState(t, env.e, runrec.StatePaused)
	if !strings.HasPrefix(rec.Reason, "git add: ") || strings.Contains(rec.Reason, "\n") || !strings.Contains(rec.Reason, "add broke") {
		t.Errorf("reason %q", rec.Reason)
	}
	requireNothingLanded(t, env, tip)
	if n := len(trackingCommits(t, env)); n != 0 {
		t.Errorf("%d tracking commits after a failed add, want none", n)
	}
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, env.e, runrec.StateCommitted)
}

// A tracking file the slice deletes and the owner deleted with git rm: the path
// is in neither the index nor the tree, so git add cannot name it. ork adds only
// what exists and the commit still records the deletion.
func TestLandCommitsTrackingDeletionsStagedInMain(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.P, ".slicer", "x.json"), "x\n")
	gitRun(t, env.P, "add", "-A")
	gitRun(t, env.P, "commit", "-q", "-m", "x base")
	gitRun(t, env.W, "merge", "--ff-only", "main")
	if err := os.Remove(filepath.Join(env.W, ".slicer", "x.json")); err != nil {
		t.Fatal(err)
	}
	gitRun(t, env.P, "rm", "-q", ".slicer/x.json")
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	commits := trackingCommits(t, env)
	if len(commits) != 1 {
		t.Fatalf("%d tracking commits, want 1", len(commits))
	}
	if got := gitRun(t, env.P, "show", "--name-status", "--format=", commits[0]); got != "D\t.slicer/x.json" {
		t.Errorf("the tracking commit holds %q, want the deletion", got)
	}
	if _, err := os.Stat(filepath.Join(env.P, ".slicer", "x.json")); err == nil {
		t.Errorf("x.json is back in the main checkout")
	}
}

// Pathspecs are literal: a tracking file named like a glob must not stage the
// matrix file the glob would match.
func TestLandTrackingCommitPathspecsAreLiteral(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.P, ".slicer", "c*"), "a\n")
	writeFile(t, filepath.Join(env.P, ".slicer", "config.json"), "{}\n")
	gitRun(t, env.P, "add", "-A")
	gitRun(t, env.P, "commit", "-q", "-m", "glob base")
	gitRun(t, env.W, "merge", "--ff-only", "main")
	// The same line on both sides, so the merge-in has nothing to settle.
	appendLine(t, filepath.Join(env.W, ".slicer", "c*"), "same\n")
	appendLine(t, filepath.Join(env.P, ".slicer", "c*"), "same\n")
	writeFile(t, filepath.Join(env.P, ".slicer", "config.json"), "{\"owner\":1}\n")
	// During the re-check, the tracking commit is made: config.json must be as
	// the owner left it, modified and not staged.
	var during string
	env.beforeCheck = func() { during = gitRun(t, env.P, "status", "--porcelain") }
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	commits := trackingCommits(t, env)
	if len(commits) != 1 {
		t.Fatalf("%d tracking commits, want 1", len(commits))
	}
	if got := gitRun(t, env.P, "show", "--name-only", "--format=", commits[0]); got != ".slicer/c*" {
		t.Errorf("the tracking commit holds %q, want only .slicer/c*", got)
	}
	if during != "M .slicer/config.json" {
		t.Errorf("main checkout during the re-check: %q, want config.json modified and unstaged", during)
	}
}

// diff.relative in the owner's git config must not turn the check off for a
// project in a subfolder.
func TestLandDiffRelativeConfigDoesNotSwitchTheCheckOff(t *testing.T) {
	env := newLandEnv(t, subProject("libs/ui"))
	gitRun(t, env.P, "config", "diff.relative", "true")
	trackingBase(t, env, "libs/ui/.slicer")
	writeFile(t, filepath.Join(env.W, "libs", "other", ".slicer", "log.jsonl"), "slice\n")
	writeFile(t, filepath.Join(env.P, "libs", "other", ".slicer", "log.jsonl"), "sibling\n")
	env.approve()
	requireLandPause(t, env, dirtyStop(filepath.Join(env.P, "libs", "ui"), "libs/other/.slicer/log.jsonl"))
	if n := len(trackingCommits(t, env)); n != 0 {
		t.Errorf("%d tracking commits, want none", n)
	}
}

func TestDiffRelativeConfigStillFindsTheTrackingOverlap(t *testing.T) {
	env := newLandEnv(t, subProject("libs/ui"))
	gitRun(t, env.P, "config", "diff.relative", "true")
	trackingBase(t, env, "libs/ui/.slicer")
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	if n := len(trackingCommits(t, env)); n != 1 {
		t.Errorf("%d tracking commits, want 1", n)
	}
}

// File names are the owner's bytes: an escape sequence or control character in
// one never reaches the pause reason, which the TUI and ork status draw.
func TestLandDirtyStopDropsControlBytesFromNames(t *testing.T) {
	env := newLandEnv(t)
	name := "a\x1b[31mb\x07.txt"
	writeFile(t, filepath.Join(env.W, name), "slice\n")
	writeFile(t, filepath.Join(env.P, name), "owner\n")
	env.approve()
	rec := requireState(t, env.e, runrec.StatePaused)
	for _, r := range rec.Reason {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("reason %q holds the control character %q", rec.Reason, r)
		}
	}
	if want := dirtyStop(env.P, "ab.txt"); rec.Reason != want {
		t.Errorf("reason %q, want %q", rec.Reason, want)
	}
}

func TestLandTrackingCommitFailureReasonDropsControlBytes(t *testing.T) {
	env := trackingEnv(t)
	name := ".slicer/x\x07y.json"
	writeFile(t, filepath.Join(env.W, name), "same\n")
	writeFile(t, filepath.Join(env.P, name), "same\n")
	script := strings.Replace(fakeGitScript, "exec git", "if [ \"$2\" = \""+env.P+"\" ] && [ \"$3\" = add ] && [ ! -e \"$d/armed\" ]; then touch \"$d/armed\" \"$d/commit-fails\"; fi\nexec git", 1)
	replaceFake(t, filepath.Join(env.fakes, "gitw"), script)
	env.approve()
	rec := requireState(t, env.e, runrec.StatePaused)
	if !strings.HasPrefix(rec.Reason, "git commit: ") || !strings.Contains(rec.Reason, "; ork staged ") || !strings.Contains(rec.Reason, "xy.json") {
		t.Errorf("reason %q", rec.Reason)
	}
	for _, r := range rec.Reason {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("reason %q holds the control character %q", rec.Reason, r)
		}
	}
}
