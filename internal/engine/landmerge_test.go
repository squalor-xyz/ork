// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestLandMergeDriverPause(t *testing.T) {
	// Each subtest's fixture starts beneath a path containing spaces.
	temp := filepath.Join(t.TempDir(), "space dir")
	if err := os.MkdirAll(temp, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", temp)
	for _, tc := range []struct {
		name       string
		path       string
		configured bool
		clause     bool
	}{
		{"slicer missing", ".slicer/render/ROADMAP.md", false, true},
		{"source missing", "internal/x.go", false, false},
		{"slicer configured", ".slicer/render/ROADMAP.md", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := changelogConflict(t, tc.path, true)
			writeFile(t, filepath.Join(env.W, ".slicer", ".gitattributes"), "render/* merge=slicer-generated\nlog.jsonl merge=union\n")
			if tc.configured {
				// A real text merge driver still conflicts, isolating configuration
				// from the conflicted path and file contents.
				gitRun(t, env.W, "config", "merge.slicer-generated.driver", "git merge-file %A %O %B")
			}
			env.approve()
			rec := requireConflictPause(t, env)
			want := conflictReason("main", rec.Branch, env.W, rec.ID, []string{tc.path})
			if tc.clause {
				want += "; slicer merge drivers are not configured in this clone; run slicer setup-git and the lines it prints, then git -C " + ShellWord(env.W) + " merge --abort and ork retry " + rec.ID
			}
			if rec.Reason != want {
				t.Errorf("pause reason = %q; want %q", rec.Reason, want)
			}
		})
	}
}

// mixedConflict is a landing where both CHANGELOG.md (an entry on each side)
// and shared.txt (the same line on each side) conflict.
func mixedConflict(t *testing.T) *landEnv {
	t.Helper()
	env := changelogConflict(t, "CHANGELOG.md", true)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "base\n")
	return env
}

// requireConflictPause checks the run paused at the landing with an error and
// returns the record. It also checks that no role was asked and nothing was
// undone.
func requireConflictPause(t *testing.T, env *landEnv) runrec.Record {
	t.Helper()
	rec := requireState(t, env.e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingError || rec.Pending.State != runrec.StateLand {
		t.Fatalf("pending %+v", rec.Pending)
	}
	if rec.LandFixes != 0 {
		t.Errorf("LandFixes = %d, want 0", rec.LandFixes)
	}
	for _, req := range env.runner.requests() {
		if strings.Contains(req.Brief, "## Landing blocked") {
			t.Error("ran a landing fix round")
		}
	}
	for _, call := range env.gitCalls() {
		if strings.Contains(call, "--abort") || strings.Contains(call, "reset") {
			t.Errorf("undid the merge: %s", call)
		}
	}
	return rec
}

func unmerged(t *testing.T, env *landEnv) string {
	t.Helper()
	return gitRun(t, env.W, "diff", "--name-only", "--diff-filter=U")
}

func countCalls(env *landEnv, prefix string) int {
	n := 0
	for _, c := range env.gitCalls() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestLandMixedConflictResolvesTheChangelogAndPausesOnTheRest(t *testing.T) {
	env := mixedConflict(t)
	env.approve()
	rec := requireConflictPause(t, env)
	if got := unmerged(t, env); got != "shared.txt" {
		t.Errorf("unmerged = %q, want only shared.txt", got)
	}
	staged := gitRun(t, env.W, "show", ":CHANGELOG.md")
	for _, want := range []string{"- S1: branch thing.", "- S2: base thing."} {
		if !strings.Contains(staged, want) {
			t.Errorf("staged CHANGELOG.md lacks %q:\n%s", want, staged)
		}
	}
	if !strings.Contains(rec.Reason, "shared.txt") || strings.Contains(rec.Reason, "CHANGELOG.md") {
		t.Errorf("reason %q", rec.Reason)
	}
	if indexOf(env.gitCalls(), "P: merge --ff-only") >= 0 {
		t.Error("landed with a conflict open")
	}
}

// compactChangelogConflict is a CHANGELOG.md-only conflict where old holds the
// Unreleased entries of the merge base, in CHANGELOG.md's layout: compact
// entries with two-space continuation lines.
func compactChangelogConflict(t *testing.T, old string) *landEnv {
	t.Helper()
	env := newLandEnv(t)
	const head = "# Changelog\n\n## Unreleased\n\n"
	const tail = "\n## v1.0.0\n\n- released\n"
	env.moveBase("CHANGELOG.md", head+old+tail)
	gitRun(t, env.W, "merge", "--no-edit", "main")
	writeFile(t, filepath.Join(env.W, "CHANGELOG.md"), head+"- S1: branch thing.\n  Docs updated: `docs/design.md`.\n"+old+tail)
	env.moveBase("CHANGELOG.md", head+"- S2: base thing.\n  Docs updated: `docs/design.md`.\n"+old+tail)
	return env
}

func TestLandResolvesACompactChangelogConflict(t *testing.T) {
	const old = "- old one\n  second line.\n- old two.\n"
	env := compactChangelogConflict(t, old)
	env.approve()
	rec := env.run.Snapshot()
	if rec.LandFixes != 0 {
		t.Fatalf("LandFixes = %d, want 0", rec.LandFixes)
	}
	requireState(t, env.e, runrec.StateCommitted)
	want := "# Changelog\n\n## Unreleased\n\n- S1: branch thing.\n  Docs updated: `docs/design.md`.\n- S2: base thing.\n  Docs updated: `docs/design.md`.\n" + old + "\n## v1.0.0\n\n- released"
	if got := gitRun(t, env.P, "show", "main:CHANGELOG.md"); got != want {
		t.Errorf("merged content: %q, want %q", got, want)
	}
	if got := strings.Fields(gitRun(t, env.P, "show", "-s", "--format=%P", gitRun(t, env.P, "log", "--merges", "-1", "--format=%H", "main"))); len(got) != 2 {
		t.Errorf("merge parents: %v", got)
	}
	if indexOf(env.gitCalls(), "P: merge --ff-only") < 0 {
		t.Error("landing did not finish")
	}
}

func TestLandResolvesAMixedLayoutChangelogConflict(t *testing.T) {
	const old = "- old one\n  second line.\n- old two.\n\n- old three.\n"
	env := compactChangelogConflict(t, old)
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	want := "# Changelog\n\n## Unreleased\n\n- S1: branch thing.\n  Docs updated: `docs/design.md`.\n- S2: base thing.\n  Docs updated: `docs/design.md`.\n" + old + "\n## v1.0.0\n\n- released"
	if got := gitRun(t, env.P, "show", "main:CHANGELOG.md"); got != want {
		t.Errorf("merged content: %q, want %q", got, want)
	}
}

func TestLandLeavesOtherConflictsToThePause(t *testing.T) {
	env := mixedConflict(t)
	env.approve()
	rec := requireConflictPause(t, env)
	if !strings.Contains(rec.Reason, "shared.txt") {
		t.Errorf("reason does not name the file: %q", rec.Reason)
	}
	if indexOf(env.gitCalls(), "W: add -- shared.txt") >= 0 {
		t.Error("staged a file that still conflicts")
	}
	if b, err := os.ReadFile(filepath.Join(env.W, "shared.txt")); err != nil || !strings.Contains(string(b), "<<<<<<<") {
		t.Errorf("the conflict markers were lost: %q %v", b, err)
	}
}

func TestLandConflictPauseIsOneLineAndNamesFilesWorktreeAndRetry(t *testing.T) {
	env := newLandEnv(t)
	names := []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt", "f.txt", "g.txt"}
	for _, n := range names {
		writeFile(t, filepath.Join(env.W, n), "branch\n")
		writeFile(t, filepath.Join(env.P, n), "base\n")
	}
	gitRun(t, env.P, "add", "-A")
	gitRun(t, env.P, "commit", "-q", "-m", "base adds seven files")
	env.approve()
	rec := requireConflictPause(t, env)
	if strings.Contains(rec.Reason, "\n") {
		t.Errorf("reason has a newline: %q", rec.Reason)
	}
	for _, n := range names[:5] {
		if !strings.Contains(rec.Reason, n) {
			t.Errorf("reason lacks %s: %q", n, rec.Reason)
		}
	}
	for _, n := range names[5:] {
		if strings.Contains(rec.Reason, n) {
			t.Errorf("reason names %s beyond the fifth: %q", n, rec.Reason)
		}
	}
	for _, want := range []string{"+2 more", env.W, "ork retry " + rec.ID, "no need to git add"} {
		if !strings.Contains(rec.Reason, want) {
			t.Errorf("reason lacks %q: %q", want, rec.Reason)
		}
	}
	if brief, _ := filepath.Glob(filepath.Join(env.store.RunDir(rec.ID), "land-*.md")); len(brief) != 0 {
		t.Errorf("a brief file was written: %v", brief)
	}
}

func TestConflictReasonNamesEveryFileUpToFive(t *testing.T) {
	got := conflictReason("main", "S04", "/w", "run-1", []string{"a", "b", "c", "d", "e"})
	if strings.Contains(got, "more") || strings.Contains(got, "\n") {
		t.Errorf("five files: %q", got)
	}
	got = conflictReason("main", "S04", "/w", "run-1", []string{"a", "b", "c", "d", "e", "f"})
	if !strings.Contains(got, "+1 more") || strings.Contains(got, "f,") {
		t.Errorf("six files: %q", got)
	}
}

func TestLandRetryCommitsAMergeTheOwnerResolved(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "base\n")
	env.approve()
	requireConflictPause(t, env)
	requests := len(env.runner.requests())
	// The owner edits the file and does not git add it.
	writeFile(t, filepath.Join(env.W, "shared.txt"), "resolved\n")
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, env.e, runrec.StateCommitted)
	if rec.LandFixes != 0 {
		t.Errorf("LandFixes = %d, want 0", rec.LandFixes)
	}
	if got := len(env.runner.requests()); got != requests {
		t.Errorf("%d role requests after the retry, want none", got-requests)
	}
	if got := gitRun(t, env.P, "show", "main:shared.txt"); got != "resolved" {
		t.Errorf("shared.txt on main: %q", got)
	}
	merge := gitRun(t, env.P, "log", "--merges", "-1", "--format=%s", "main")
	if merge != "Merge main into "+item {
		t.Errorf("merge commit message %q", merge)
	}
	if got := gitRun(t, env.P, "show", "main:rendered.txt"); got != "render" {
		t.Errorf("rendered.txt on main: %q", got)
	}
	if len(env.seen) != 1 || !env.seen[0].treeClean || !env.seen[0].hasBase {
		t.Errorf("re-check: %+v", env.seen)
	}
	if rec.LandRecheck {
		t.Error("LandRecheck still set")
	}
}

func TestLandRetryAfterTheOwnerStagedTheResolution(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "base\n")
	env.approve()
	requireConflictPause(t, env)
	requests := len(env.runner.requests())
	writeFile(t, filepath.Join(env.W, "shared.txt"), "resolved\n")
	gitRun(t, env.W, "add", "shared.txt")
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, env.e, runrec.StateCommitted)
	if rec.LandFixes != 0 || len(env.runner.requests()) != requests {
		t.Errorf("LandFixes %d, %d new role requests", rec.LandFixes, len(env.runner.requests())-requests)
	}
	if got := gitRun(t, env.P, "show", "main:shared.txt"); got != "resolved" {
		t.Errorf("shared.txt on main: %q", got)
	}
	if got := gitRun(t, env.P, "log", "--merges", "-1", "--format=%s", "main"); got != "Merge main into "+item {
		t.Errorf("merge commit message %q", got)
	}
}

func TestLandRetryAfterTheOwnerCommittedTheMerge(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "base\n")
	env.approve()
	requireConflictPause(t, env)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "resolved\n")
	gitRun(t, env.W, "add", "shared.txt")
	gitRun(t, env.W, "commit", "-q", "--no-edit")
	merges := countCalls(env, "W: merge -m ")
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, env.e, runrec.StateCommitted)
	if got := countCalls(env, "W: merge -m "); got != merges {
		t.Errorf("git merge ran again (%d calls, was %d)", got, merges)
	}
	if indexOf(env.log("slicer.log"), "render --json --root "+env.W) < 0 {
		t.Error("render did not run")
	}
	if len(env.seen) != 1 {
		t.Errorf("%d re-checks, want 1", len(env.seen))
	}
	if i, j := indexOf(env.gitCalls(), "W: merge -m "), indexOf(env.gitCalls(), "P: merge --ff-only"); i < 0 || j < i {
		t.Errorf("git calls: %q", env.gitCalls())
	}
}

func TestLandRetryAfterTheOwnerAbortedTheMerge(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "base\n")
	env.approve()
	first := requireConflictPause(t, env)
	gitRun(t, env.W, "merge", "--abort")
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := requireConflictPause(t, env)
	if second.Reason != first.Reason {
		t.Errorf("reason changed:\n%q\n%q", first.Reason, second.Reason)
	}
	if got := countCalls(env, "W: merge -m "); got != 2 {
		t.Errorf("%d git merge calls, want 2", got)
	}
}

func TestLandRetryPausesAgainWhileMarkersRemain(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "base\n")
	env.approve()
	requireConflictPause(t, env)
	adds, commits := countCalls(env, "W: add"), countCalls(env, "W: commit")
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireConflictPause(t, env)
	if !strings.Contains(rec.Reason, "shared.txt") {
		t.Errorf("reason %q", rec.Reason)
	}
	if countCalls(env, "W: add") != adds || countCalls(env, "W: commit") != commits {
		t.Errorf("staged or committed with markers left: %q", env.gitCalls())
	}
}

func TestLandKeepsAnUnmergedFileThatIsMissing(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "base\n")
	env.approve()
	requireConflictPause(t, env)
	if err := os.Remove(filepath.Join(env.W, "shared.txt")); err != nil {
		t.Fatal(err)
	}
	adds := countCalls(env, "W: add")
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireConflictPause(t, env)
	if !strings.Contains(rec.Reason, "shared.txt") {
		t.Errorf("reason %q", rec.Reason)
	}
	if countCalls(env, "W: add") != adds {
		t.Errorf("staged a missing file: %q", env.gitCalls())
	}
	if got := unmerged(t, env); got != "shared.txt" {
		t.Errorf("unmerged = %q", got)
	}
}

func TestLandStagesOnlyTheUnmergedFilesItResolved(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "base\n")
	env.approve()
	requireConflictPause(t, env)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "resolved\n")
	writeFile(t, filepath.Join(env.W, "stray.txt"), "not part of the merge\n")
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The render commit that follows takes the tree as it is (unchanged
	// behavior); the merge commit itself holds only what the merge staged.
	merge := gitRun(t, env.P, "log", "--merges", "-1", "--format=%H", "main")
	if files := gitRun(t, env.P, "ls-tree", "-r", "--name-only", merge); strings.Contains(files, "stray.txt") {
		t.Errorf("the merge commit holds the stray file: %q", files)
	}
	if got := gitRun(t, env.P, "show", merge+":shared.txt"); got != "resolved" {
		t.Errorf("shared.txt in the merge commit: %q", got)
	}
}

// A conflict git leaves without markers (a binary file) is unseen by anyone
// right after `git merge`; ork must not stage it as resolved then.
func TestLandDoesNotStageAFreshConflictWithoutMarkers(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.P, "logo.bin"), "\x00base\n")
	gitRun(t, env.P, "add", "-A")
	gitRun(t, env.P, "commit", "-q", "-m", "binary")
	gitRun(t, env.W, "merge", "-q", "main")
	writeFile(t, filepath.Join(env.W, "logo.bin"), "\x00branch\n")
	env.moveBase("logo.bin", "\x00main\n")
	env.approve()
	rec := requireConflictPause(t, env)
	if got := unmerged(t, env); got != "logo.bin" {
		t.Errorf("unmerged = %q, want logo.bin", got)
	}
	if !strings.Contains(rec.Reason, "logo.bin") {
		t.Errorf("reason %q", rec.Reason)
	}
	if got := countCalls(env, "W: add"); got != 1 { // the approval's own add -A
		t.Errorf("%d git add calls, want only the commit step's", got)
	}
}

func TestLandOwnerEditedChangelogIsNotOverwritten(t *testing.T) {
	// No ancestor stage: the entry merge cannot run, so CHANGELOG.md stays
	// unmerged for the owner.
	env := changelogConflict(t, "CHANGELOG.md", false)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "base\n")
	env.approve()
	rec := requireConflictPause(t, env)
	if !strings.Contains(rec.Reason, "CHANGELOG.md") {
		t.Fatalf("reason %q", rec.Reason)
	}
	stageReads := countCalls(env, "W: show :")
	owner := "# Changelog\n\nOwner's own text.\n"
	writeFile(t, filepath.Join(env.W, "CHANGELOG.md"), owner)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "resolved\n")
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, env.e, runrec.StateCommitted)
	if got := gitRun(t, env.P, "show", "main:CHANGELOG.md"); got != strings.TrimSpace(owner) {
		t.Errorf("CHANGELOG.md on main: %q", got)
	}
	if got := countCalls(env, "W: show :"); got != stageReads {
		t.Errorf("the entry merge ran on an owner-edited file (%d stage reads, was %d)", got, stageReads)
	}
}

func TestLandMergeCommitFailurePausesWithGitsMessage(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "base\n")
	env.approve()
	requireConflictPause(t, env)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "resolved\n")
	writeFile(t, filepath.Join(env.fakes, "commit-fails"), "")
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireConflictPause(t, env)
	if !strings.Contains(rec.Reason, "git commit") || !strings.Contains(rec.Reason, "commit broke") || strings.Contains(rec.Reason, "\n") {
		t.Errorf("reason %q", rec.Reason)
	}
	// The switch is spent: the next retry commits.
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, env.e, runrec.StateCommitted)
}

func TestLandCleanMergeIsUnchanged(t *testing.T) {
	env := newLandEnv(t)
	env.moveBase("base.txt", "from main\n")
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	if got := countCalls(env, "W: commit -m Merge"); got != 0 {
		t.Errorf("ork committed a merge git had already committed: %d", got)
	}
}

func TestLandFixBriefDoesNotAskTheRoleToCommit(t *testing.T) {
	env := newLandEnv(t)
	section := env.e.feedbackSection(&runrec.Feedback{Kind: "land", File: "land-1.md"})
	body := section.Body
	for _, bad := range []string{"git add", "git commit", "--no-edit", "commit what it rewrote"} {
		if strings.Contains(body, bad) {
			t.Errorf("the land brief contains %q:\n%s", bad, body)
		}
	}
	if !strings.Contains(body, "do not commit") && !strings.Contains(body, "Do not commit") {
		t.Errorf("the land brief does not say the role does not commit:\n%s", body)
	}
}

func TestRoleToolsStillCannotCommit(t *testing.T) {
	commands := []string{"git add x", "git commit -m x", "git commit --no-edit", "git -C /w add x", "git -C /w commit -m x"}
	for _, entry := range DefaultImplementerAllowlist("") {
		if !strings.HasPrefix(entry, "Bash(") || !strings.HasSuffix(entry, ")") {
			continue
		}
		pat := strings.TrimSuffix(strings.TrimPrefix(entry, "Bash("), ")")
		// `*` matches any run of characters, slashes included.
		re := regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(pat), `\*`, ".*") + "$")
		for _, cmd := range commands {
			if re.MatchString(cmd) {
				t.Errorf("allowlist entry %q matches %q", entry, cmd)
			}
		}
	}
}

func TestHasConflictMarkers(t *testing.T) {
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		text string
		want bool
	}{
		"both":       {"a\n<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> main\n", true},
		"lone rule":  {"Title\n=======\ntext\n", false},
		"open only":  {"<<<<<<< HEAD\n", false},
		"close only": {">>>>>>> main\n", false},
		"none":       {"plain\n", false},
	} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
		writeFile(t, path, tc.text)
		if got := hasConflictMarkers(path); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
	if hasConflictMarkers(filepath.Join(dir, "missing")) {
		t.Error("a missing file has markers")
	}
}
