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

	"github.com/squalor-xyz/ork/internal/clitest"
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

// S398: an Unreleased section that is the last section of the file.
func TestMergeChangelogEntriesUnreleasedLast(t *testing.T) {
	const p = "# Changelog\n\n## Unreleased\n\n"
	tests := []struct {
		name, base, ours, theirs, want string
	}{
		{"with an old entry", p + "- old\n", p + "- S1\n\n- old\n", p + "- S2\n\n- old\n", p + "- S1\n\n- S2\n\n- old\n"},
		{"empty section", p, p + "- S1\n", p + "- S2\n", p + "- S1\n\n- S2\n"},
		{"no final newline", p + "- old", p + "- S1\n\n- old", p + "- S2\n\n- old", p + "- S1\n\n- S2\n\n- old"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mergeChangelogEntries(tt.base, tt.ours, tt.theirs)
			if !ok || got != tt.want {
				t.Fatalf("got (%q, %v), want (%q, true)", got, ok, tt.want)
			}
		})
	}
}

// S398: a CRLF file is not rewritten; the conflict goes to the owner.
func TestMergeChangelogEntriesRefusesCRLF(t *testing.T) {
	const p = "# Changelog\n\n## Unreleased\n\n"
	const suffix = "## v1.0.0\n\n- released\n"
	const branch = "- S1: branch thing.\n  Docs updated: `docs/design.md`."
	const main = "- S2: base thing.\n  Docs updated: `docs/design.md`."
	text := func(entries ...string) string {
		if len(entries) == 0 {
			return p + suffix
		}
		return p + strings.Join(entries, "\n\n") + "\n\n" + suffix
	}
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }
	tests := []struct{ name, base, ours, theirs string }{
		{"unreleased last", p + "- old\n", p + "- S1\n\n- old\n", p + "- S2\n\n- old\n"},
		{"reviewer example", text(), text(branch), text(main)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mergeChangelogEntries(crlf(tt.base), crlf(tt.ours), crlf(tt.theirs))
			if ok || got != "" {
				t.Fatalf("got (%q, %v), want (\"\", false)", got, ok)
			}
		})
	}
}

// TestMergeChangelogEntriesLayouts covers bodies whose entries are not all
// separated by one blank line, as in CHANGELOG.md: compact at the top, blank
// separated below.
func TestMergeChangelogEntriesLayouts(t *testing.T) {
	const prefix = "# Changelog\n\n## Unreleased\n\n"
	const suffix = "## v1.0.0\n\n- released\n"
	const branch = "- S1: branch thing.\n  Docs updated: `docs/design.md`."
	const main = "- S2: base thing.\n  Docs updated: `docs/design.md`."
	const old1 = "- old one\n  second line."
	const old2 = "- old two."
	const old3 = "- old three."
	body := func(b string) string { return prefix + b + suffix }
	tests := []struct {
		name, base, ours, theirs, want string
		ok                             bool
	}{
		{
			"compact",
			body(old1 + "\n" + old2 + "\n\n"),
			body(branch + "\n" + old1 + "\n" + old2 + "\n\n"),
			body(main + "\n" + old1 + "\n" + old2 + "\n\n"),
			body(branch + "\n" + main + "\n" + old1 + "\n" + old2 + "\n\n"),
			true,
		},
		{
			"compact, top and bottom additions",
			body(old1 + "\n" + old2 + "\n\n"),
			body(branch + "\n" + old1 + "\n" + old2 + "\n\n"),
			body(old1 + "\n" + old2 + "\n" + main + "\n\n"),
			body(branch + "\n" + main + "\n" + old1 + "\n" + old2 + "\n\n"),
			true,
		},
		{
			"mixed base with a compact top takes the top separator",
			body(old1 + "\n" + old2 + "\n\n" + old3 + "\n\n"),
			body(branch + "\n" + old1 + "\n" + old2 + "\n\n" + old3 + "\n\n"),
			body(main + "\n" + old1 + "\n" + old2 + "\n\n" + old3 + "\n\n"),
			body(branch + "\n" + main + "\n" + old1 + "\n" + old2 + "\n\n" + old3 + "\n\n"),
			true,
		},
		{
			"mixed base with a blank top takes the top separator",
			body(old1 + "\n\n" + old2 + "\n" + old3 + "\n\n"),
			body(branch + "\n\n" + old1 + "\n\n" + old2 + "\n" + old3 + "\n\n"),
			body(main + "\n\n" + old1 + "\n\n" + old2 + "\n" + old3 + "\n\n"),
			body(branch + "\n\n" + main + "\n\n" + old1 + "\n\n" + old2 + "\n" + old3 + "\n\n"),
			true,
		},
		{
			"a side that restyles the separators does not change the base's",
			body(old1 + "\n\n" + old2 + "\n\n"),
			body(branch + "\n" + old1 + "\n" + old2 + "\n\n"),
			body(main + "\n\n" + old1 + "\n\n" + old2 + "\n\n"),
			body(branch + "\n\n" + main + "\n\n" + old1 + "\n\n" + old2 + "\n\n"),
			true,
		},
		{
			"compact additions into an empty base keep their style",
			body(""),
			body(branch + "\n" + main + "\n\n"),
			body(old3 + "\n\n"),
			body(branch + "\n" + main + "\n" + old3 + "\n\n"),
			true,
		},
		{
			"blank lines before the first entry stay put",
			body("\n" + old1 + "\n" + old2 + "\n\n"),
			body("\n" + branch + "\n" + old1 + "\n" + old2 + "\n\n"),
			body("\n" + main + "\n" + old1 + "\n" + old2 + "\n\n"),
			body("\n" + branch + "\n" + main + "\n" + old1 + "\n" + old2 + "\n\n"),
			true,
		},
		{
			"empty base, sides end differently",
			body(""),
			body(branch + "\n\n"),
			body(main + "\n"),
			"", false,
		},
		{
			"empty base, a side with a stray blank line",
			body(""),
			body(branch + "\n\n"),
			body("\n"),
			"", false,
		},
		{
			"compact edited base",
			body(old1 + "\n" + old2 + "\n\n"),
			body(old1 + "\n- edited\n\n"),
			body(old1 + "\n" + old2 + "\n\n"),
			"", false,
		},
		{
			"compact reordered base",
			body(old1 + "\n" + old2 + "\n\n"),
			body(old2 + "\n" + old1 + "\n\n"),
			body(old1 + "\n" + old2 + "\n\n"),
			"", false,
		},
		{
			"compact duplicate added",
			body(old1 + "\n\n"),
			body(branch + "\n" + branch + "\n" + old1 + "\n\n"),
			body(old1 + "\n\n"),
			"", false,
		},
		{
			"compact stray paragraph",
			body(old1 + "\n\n"),
			body(old1 + "\nparagraph\n\n"),
			body(old1 + "\n\n"),
			"", false,
		},
		{
			"ending changed",
			body(old1 + "\n\n"),
			body(branch + "\n" + old1 + "\n"),
			body(old1 + "\n\n"),
			"", false,
		},
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

// S332 (adj 404): the gate fails a slice with no landed-changed/ITEM.md when the
// project uses the folder. ork checks it in Go, before the gate role runs.

const entryFolderOn = `{"changelog_path":"CHANGELOG.md","changelog_entries_dir":"landed-changed"}`

// existingWorktreeRoutes answer the checks setup makes on a worktree folder that
// a test created before the run started (here, to hold an entry file).
func existingWorktreeRoutes(P, W string) []clitest.Route {
	return []clitest.Route{
		{Tool: "git", Args: []string{"-C", W, "rev-parse", "--show-toplevel"}, Stdout: W + "\n"},
		{Tool: "git", Args: []string{"-C", W, "symbolic-ref", "--short", "HEAD"}, Stdout: item + "\n"},
		{Tool: "git", Args: []string{"-C", W, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
		{Tool: "git", Args: []string{"-C", P, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
	}
}

// entryHarness is the engine harness with a `slicer config --json` route that
// answers before the default one, with the reply's stdout, stderr and exit.
func entryHarness(t *testing.T, reply clitest.Reply) *harness {
	t.Helper()
	return newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return append(existingWorktreeRoutes(P, W), clitest.Route{Tool: "slicer", Args: []string{"config", "--json", "--root", W}, Stdout: reply.Stdout, Stderr: reply.Stderr, Exit: reply.Exit})
		}
	})
}

func changelogEntryFiles(t *testing.T, h *harness, rec runrec.Record) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(h.store.RunDir(rec.ID), "changelog-entry-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func hasGate(roles []string) bool {
	for _, r := range roles {
		if strings.HasPrefix(r, "gate-") {
			return true
		}
	}
	return false
}

func TestGateFailsWithoutTheChangelogEntryWhenTheProjectUsesTheFolder(t *testing.T) {
	h := entryHarness(t, clitest.Reply{Stdout: entryFolderOn})
	e := h.start()
	rec := e.run.Snapshot()
	if hasGate(h.runner.roles()) {
		t.Fatalf("the gate role ran: %v", h.runner.roles())
	}
	want := "VERDICT: FAIL\n\nThe changelog entry landed-changed/S04.md is missing. This project keeps one changelog entry per slice in landed-changed/ (adj 404). " +
		"Write it with `slicer changelog add S04 --text \"...\"`, adding `--category NAME` when `slicer config changelog_categories` lists any. Do not edit CHANGELOG.md.\n"
	if got := h.readReport(rec, "changelog-entry-1.md"); got != want {
		t.Errorf("changelog-entry-1.md:\n%q\nwant\n%q", got, want)
	}
	var fix string
	for _, req := range h.runner.requests() {
		if req.Role == RoleImplementer && req.Round == 2 {
			fix = req.Brief
		}
	}
	_, section, ok := strings.Cut(fix, "## Gate FAIL")
	if !ok || !strings.Contains(section, "landed-changed/S04.md") {
		t.Errorf("round-2 implementer brief lacks a Gate FAIL section naming the file:\n%s", fix)
	}
	if rec.Fails != 0 || len(h.calls("slicer", "note")) != 0 {
		t.Errorf("ork found it, not the gate: fails %d, notes %v", rec.Fails, h.calls("slicer", "note"))
	}
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingRounds || !strings.Contains(rec.Pending.Reason, "gate failed (changelog entry landed-changed/S04.md is missing)") {
		t.Errorf("pending %+v", rec.Pending)
	}
}

func TestGateRunsWhenTheChangelogEntryExists(t *testing.T) {
	h := entryHarness(t, clitest.Reply{Stdout: entryFolderOn})
	writeFile(t, filepath.Join(h.W, "landed-changed", "S04.md"), "entry text\n")
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if !hasGate(h.runner.roles()) {
		t.Errorf("the gate role did not run: %v", h.runner.roles())
	}
	if got := changelogEntryFiles(t, h, rec); len(got) != 0 {
		t.Errorf("feedback files %v", got)
	}
	path, on, err := e.changelogEntry(context.Background(), rec)
	if err != nil || !on || path != "landed-changed/S04.md" {
		t.Errorf("changelogEntry = %q, %v, %v", path, on, err)
	}
}

func TestGateSkipsTheChangelogCheckWhenTheFolderIsOff(t *testing.T) {
	h := entryHarness(t, clitest.Reply{Stdout: `{"changelog_path":"","changelog_entries_dir":"landed-changed"}`})
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if !hasGate(h.runner.roles()) {
		t.Errorf("the gate role did not run: %v", h.runner.roles())
	}
	if got := changelogEntryFiles(t, h, rec); len(got) != 0 {
		t.Errorf("feedback files %v", got)
	}
	if _, on, err := e.changelogEntry(context.Background(), rec); err != nil || on {
		t.Errorf("changelogEntry on = %v, err = %v", on, err)
	}
}

func TestChangelogEntryReadsTheConfig(t *testing.T) {
	for _, tc := range []struct {
		name, config, path string
		on                 bool
	}{
		{"default dir", `{"changelog_path":"CHANGELOG.md"}`, "landed-changed/S04.md", true},
		{"own dir", `{"changelog_path":"CHANGELOG.md","changelog_entries_dir":"notes/entries"}`, "notes/entries/S04.md", true},
		{"older slicer", `{}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := entryHarness(t, clitest.Reply{Stdout: tc.config})
			e := h.start()
			path, on, err := e.changelogEntry(context.Background(), e.run.Snapshot())
			if err != nil || on != tc.on || (on && path != tc.path) {
				t.Errorf("changelogEntry = %q, %v, %v; want %q, %v", path, on, err, tc.path, tc.on)
			}
		})
	}
}

func TestGateChangelogConfigReadFailurePauses(t *testing.T) {
	h := entryHarness(t, clitest.Reply{Exit: 1, Stderr: "config unavailable"})
	e := h.start()
	rec := e.run.Snapshot()
	if hasGate(h.runner.roles()) {
		t.Errorf("the gate role ran: %v", h.runner.roles())
	}
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingError || rec.Pending.State != runrec.StateGate || !strings.HasPrefix(rec.Pending.Reason, "changelog entry check: ") {
		t.Errorf("state %s, pending %+v", rec.State, rec.Pending)
	}
}
