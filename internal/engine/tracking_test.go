// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gitIdentity gives the test its own git configuration and identity.
func gitIdentity(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(key, "Test")
	}
	for _, key := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(key, "test@example.invalid")
	}
}

// trackingRepo is a project with slicer files, an adj store and one commit on
// main. Every file the tests change is tracked, except those a test adds.
func trackingRepo(t *testing.T) string {
	t.Helper()
	gitIdentity(t)
	P := t.TempDir()
	gitRun(t, P, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(P, ".adjudicate.json"), "{\"store\":\"decisions\"}\n")
	writeFile(t, filepath.Join(P, ".slicer", "index.json"), "{}\n")
	writeFile(t, filepath.Join(P, ".slicer", "config.json"), "{}\n")
	writeFile(t, filepath.Join(P, ".slicer", "relay-roles.json"), "{}\n")
	writeFile(t, filepath.Join(P, ".slicer", "slices", "S8.json"), "{}\n")
	writeFile(t, filepath.Join(P, ".slicer", "slices", "S9.json"), "{}\n")
	writeFile(t, filepath.Join(P, "decisions", "0001-a.md"), "a\n")
	writeFile(t, filepath.Join(P, "README.md"), "one\n")
	gitRun(t, P, "add", "-A")
	gitRun(t, P, "commit", "-q", "-m", "init")
	return P
}

func commitTrackingFiles(P, base string, leave ...string) (TrackingCommit, error) {
	return CommitTrackingFiles(context.Background(), PlainGit(Deps{}), P, base, leave)
}

// commitFiles is the sorted file list of the commit at HEAD.
func commitFiles(t *testing.T, P string) string {
	t.Helper()
	return strings.ReplaceAll(gitRun(t, P, "show", "--name-only", "--pretty=format:", "HEAD"), "\n", " ")
}

func requireTrackingError(t *testing.T, err error, code, message string) {
	t.Helper()
	var te *TrackingError
	if !errors.As(err, &te) {
		t.Fatalf("error %v, want a *TrackingError", err)
	}
	if te.Code != code || te.Message != message {
		t.Fatalf("error %q %q, want %q %q", te.Code, te.Message, code, message)
	}
}

func TestTrackingCommitTakesSlicerAndAdjFiles(t *testing.T) {
	P := trackingRepo(t)
	gitRun(t, P, "rm", "-q", ".slicer/slices/S9.json") // S9 is a new file in this test
	gitRun(t, P, "commit", "-q", "-m", "drop S9")
	before := gitRun(t, P, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(P, ".slicer", "slices", "S9.json"), "{\"new\":1}\n")
	writeFile(t, filepath.Join(P, ".slicer", "index.json"), "{\"changed\":1}\n")
	writeFile(t, filepath.Join(P, "decisions", "0007-x.md"), "x\n")
	writeFile(t, filepath.Join(P, "README.md"), "two\n")

	got, err := commitTrackingFiles(P, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "Tracking edits on main: S9; adj 7" {
		t.Errorf("subject %q", got.Subject)
	}
	if subject := gitRun(t, P, "log", "-1", "--pretty=%s"); subject != got.Subject {
		t.Errorf("commit subject %q, want %q", subject, got.Subject)
	}
	if n := gitRun(t, P, "rev-list", "--count", before+"..HEAD"); n != "1" {
		t.Errorf("%s new commits, want 1", n)
	}
	if files := commitFiles(t, P); files != ".slicer/index.json .slicer/slices/S9.json decisions/0007-x.md" {
		t.Errorf("commit files %q", files)
	}
	if len(got.Files) != 3 {
		t.Errorf("result files %v, want 3", got.Files)
	}
	if want := gitRun(t, P, "rev-parse", "--short", "HEAD"); got.Hash != want {
		t.Errorf("hash %q, want %q", got.Hash, want)
	}
	if modified := gitRun(t, P, "diff", "--name-only"); modified != "README.md" {
		t.Errorf("modified %q, want README.md still modified and unstaged", modified)
	}
	if untracked := gitRun(t, P, "ls-files", "--others", "--exclude-standard"); untracked != "" {
		t.Errorf("untracked %q, want none", untracked)
	}
	if staged := gitRun(t, P, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("staged %q, want nothing", staged)
	}
}

func TestTrackingCommitLeavesConfigAndNamedPaths(t *testing.T) {
	P := trackingRepo(t)
	before := gitRun(t, P, "rev-parse", "HEAD")
	for _, rel := range []string{".slicer/config.json", ".slicer/relay-roles.json", ".slicer/slices/S9.json", ".slicer/slices/S8.json"} {
		writeFile(t, filepath.Join(P, filepath.FromSlash(rel)), "{\"edited\":1}\n")
	}
	got, err := commitTrackingFiles(P, "main", ".slicer/slices/S8.json")
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "Tracking edits on main: S9" {
		t.Errorf("subject %q", got.Subject)
	}
	if files := commitFiles(t, P); files != ".slicer/slices/S9.json" {
		t.Errorf("commit files %q, want only S9", files)
	}
	if n := gitRun(t, P, "rev-list", "--count", before+"..HEAD"); n != "1" {
		t.Errorf("%s new commits, want 1", n)
	}
	want := ".slicer/config.json\n.slicer/relay-roles.json\n.slicer/slices/S8.json"
	if modified := gitRun(t, P, "diff", "--name-only"); modified != want {
		t.Errorf("modified %q, want the config, matrix and left file still modified", modified)
	}
	if staged := gitRun(t, P, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("staged %q, want nothing", staged)
	}
}

func TestTrackingCommitRefusesOtherStagedPaths(t *testing.T) {
	P := trackingRepo(t)
	before := gitRun(t, P, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(P, "README.md"), "two\n")
	gitRun(t, P, "add", "README.md")
	writeFile(t, filepath.Join(P, ".slicer", "index.json"), "{\"changed\":1}\n")
	_, err := commitTrackingFiles(P, "main")
	requireTrackingError(t, err, "refused", "other paths are staged: README.md; commit or unstage them first")
	if after := gitRun(t, P, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD moved to %s", after)
	}
	if staged := gitRun(t, P, "diff", "--cached", "--name-only"); staged != "README.md" {
		t.Errorf("staged %q, want README.md only", staged)
	}
}

func TestTrackingCommitRefusesOffTheBaseBranch(t *testing.T) {
	P := trackingRepo(t)
	gitRun(t, P, "checkout", "-q", "-b", "x")
	before := gitRun(t, P, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(P, ".slicer", "index.json"), "{\"changed\":1}\n")
	_, err := commitTrackingFiles(P, "main")
	requireTrackingError(t, err, "refused", "the main checkout is on x, not the base branch main")
	if after := gitRun(t, P, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD moved to %s", after)
	}
}

func TestTrackingCommitNothingToCommit(t *testing.T) {
	P := trackingRepo(t)
	before := gitRun(t, P, "rev-parse", "HEAD")
	_, err := commitTrackingFiles(P, "main")
	requireTrackingError(t, err, "nothing_to_commit", "no uncommitted tracking files")
	if after := gitRun(t, P, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD moved to %s", after)
	}
}

func TestTrackingCommitOnlyConfigChangedIsNothingToCommit(t *testing.T) {
	P := trackingRepo(t)
	writeFile(t, filepath.Join(P, ".slicer", "config.json"), "{\"edited\":1}\n")
	_, err := commitTrackingFiles(P, "main")
	requireTrackingError(t, err, "nothing_to_commit", "no uncommitted tracking files")
}

func TestTrackingCommitLeavePathMustBeATrackingPath(t *testing.T) {
	P := trackingRepo(t)
	writeFile(t, filepath.Join(P, ".slicer", "index.json"), "{\"changed\":1}\n")
	for _, leave := range []string{"README.md", ".slicer/config.json", "../x"} {
		_, err := commitTrackingFiles(P, "main", leave)
		requireTrackingError(t, err, "bad_params", "--leave "+leave+" is not a tracking path")
	}
}

func TestTrackingCommitWithoutAdjFileTakesSlicerFilesOnly(t *testing.T) {
	P := trackingRepo(t)
	if err := os.Remove(filepath.Join(P, ".adjudicate.json")); err != nil {
		t.Fatal(err)
	}
	gitRun(t, P, "commit", "-q", "-a", "-m", "drop the adj file")
	writeFile(t, filepath.Join(P, "decisions", "0007-x.md"), "x\n")
	writeFile(t, filepath.Join(P, ".slicer", "index.json"), "{\"changed\":1}\n")
	got, err := commitTrackingFiles(P, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "Tracking edits on main" {
		t.Errorf("subject %q, want the fallback", got.Subject)
	}
	if files := commitFiles(t, P); files != ".slicer/index.json" {
		t.Errorf("commit files %q", files)
	}
}

func TestTrackingCommitTakesDeletions(t *testing.T) {
	P := trackingRepo(t)
	if err := os.Remove(filepath.Join(P, "decisions", "0001-a.md")); err != nil {
		t.Fatal(err)
	}
	got, err := commitTrackingFiles(P, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "Tracking edits on main: adj 1" {
		t.Errorf("subject %q", got.Subject)
	}
	if files := commitFiles(t, P); files != "decisions/0001-a.md" {
		t.Errorf("commit files %q", files)
	}
}

func TestTrackingCommitProjectInASubdirectory(t *testing.T) {
	gitIdentity(t)
	R := t.TempDir()
	P := filepath.Join(R, "libs", "ui")
	gitRun(t, R, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(P, ".slicer", "index.json"), "{}\n")
	writeFile(t, filepath.Join(R, "libs", "other", ".slicer", "index.json"), "{}\n")
	writeFile(t, filepath.Join(R, "top.txt"), "one\n")
	gitRun(t, R, "add", "-A")
	gitRun(t, R, "commit", "-q", "-m", "init")
	writeFile(t, filepath.Join(P, ".slicer", "index.json"), "{\"changed\":1}\n")
	writeFile(t, filepath.Join(R, "libs", "other", ".slicer", "index.json"), "{\"changed\":1}\n")
	if _, err := commitTrackingFiles(P, "main"); err != nil {
		t.Fatal(err)
	}
	if files := commitFiles(t, R); files != "libs/ui/.slicer/index.json" {
		t.Errorf("commit files %q, want the project's own index only", files)
	}
}

// numbered is one path for each number from first to last, by the format.
func numbered(format string, first, last int) []string {
	var paths []string
	for n := first; n <= last; n++ {
		paths = append(paths, fmt.Sprintf(format, n))
	}
	return paths
}

func TestTrackingMessage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paths []string
		want  string
	}{
		{"a run of two folds", []string{".slicer/slices/S10.json", ".slicer/slices/S9.json", "decisions/0012-b.md", "decisions/0007-a.md"}, "Tracking edits on main: S9-S10; adj 7, 12"},
		{"gaps stay apart", []string{".slicer/slices/S11.json", ".slicer/slices/S9.json", "decisions/0012-b.md", "decisions/0007-a.md"}, "Tracking edits on main: S9, S11; adj 7, 12"},
		{"a spec pass", append(append(numbered(".slicer/slices/S%d.json", 387, 389), numbered("decisions/%04d-x.md", 402, 405)...), numbered("decisions/%04d-x.md", 410, 446)...), "Tracking edits on main: S387-S389; adj 402-405, 410-446"},
		{"prefixes fold apart", []string{".slicer/slices/T3.json", ".slicer/slices/S4.json", ".slicer/slices/S3.json", ".slicer/slices/T4.json", ".slicer/slices/T5.json"}, "Tracking edits on main: S3-S4, T3-T5"},
		{"numbers of different prefixes never fold", []string{".slicer/slices/S3.json", ".slicer/slices/T4.json"}, "Tracking edits on main: S3, T4"},
		{"ids that are not plain numbers stay as they are", []string{".slicer/slices/S04.json", ".slicer/slices/S05.json", ".slicer/slices/draft.json", ".slicer/slices/S6.json", ".slicer/slices/S7.json"}, "Tracking edits on main: S6-S7, S04, S05, draft"},
		{"a record number appears once", []string{"decisions/0007-a.md", "decisions/0007-b.md", "decisions/0008-c.md"}, "Tracking edits on main: adj 7-8"},
		{"slices only", []string{".slicer/index.json", ".slicer/slices/S4.json"}, "Tracking edits on main: S4"},
		{"records only", []string{"decisions/0003-a.md"}, "Tracking edits on main: adj 3"},
		{"neither", []string{".slicer/index.json", ".slicer/log.jsonl"}, "Tracking edits on main"},
		{"render files name no slice", []string{".slicer/render/slices/S4.md"}, "Tracking edits on main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := trackingMessage("main", "decisions", tc.paths); got != tc.want {
				t.Errorf("message %q, want %q", got, tc.want)
			}
		})
	}
}
