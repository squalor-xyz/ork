// SPDX-License-Identifier: GPL-3.0-or-later

package docscheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type insertion struct {
	path        string
	first, last int
}

// parseInsertions keeps only Markdown hunks with no deleted lines.
func parseInsertions(diff string) []insertion {
	header := regexp.MustCompile(`^@@ -[0-9]+(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)
	var result []insertion
	var path string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++ ") {
			path = strings.TrimPrefix(line, "+++ b/")
			if !strings.HasPrefix(line, "+++ b/") || !strings.HasSuffix(path, ".md") {
				path = ""
			}
			continue
		}
		match := header.FindStringSubmatch(line)
		if path == "" || match == nil || match[1] != "0" {
			continue
		}
		first, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		count := 1
		if match[3] != "" {
			count, err = strconv.Atoi(match[3])
			if err != nil {
				continue
			}
		}
		if count > 0 {
			result = append(result, insertion{path, first, first + count - 1})
		}
	}
	return result
}

// checkInsertion checks the two boundaries of a pure insertion, not its interior.
func checkInsertion(lines []string, ins insertion) error {
	blank := func(n int) bool { return strings.TrimSpace(lines[n-1]) == "" }
	touched := 0
	if ins.first > 1 && !blank(ins.first) && !blank(ins.first-1) {
		touched = ins.first - 1
	} else if ins.last < len(lines) && !blank(ins.last) && !blank(ins.last+1) {
		touched = ins.last + 1
	}
	if touched != 0 {
		return fmt.Errorf("%s:%d-%d: inserted lines touch unchanged line %d; put a blank line between paragraphs", ins.path, ins.first, ins.last, touched)
	}
	return nil
}

// docInsertionErrors compares docs with the branch point, including local edits.
// Missing git or main makes this optional repository check skip.
func docInsertionErrors(root string) (errs []error, skip string) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, "git not found"
	}
	git := func(args ...string) ([]byte, error) {
		return exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	}
	if _, err := git("rev-parse", "--verify", "--quiet", "main"); err != nil {
		return nil, "no main branch"
	}
	base, err := git("merge-base", "main", "HEAD")
	if err != nil {
		return nil, "no merge base with main"
	}
	diff, err := git("diff", "--unified=0", "--no-color", "--no-ext-diff", "--no-renames", "--src-prefix=a/", "--dst-prefix=b/", strings.TrimSpace(string(base)), "--", "docs")
	if err != nil {
		return []error{fmt.Errorf("git diff: %w: %s", err, diff)}, ""
	}
	for _, ins := range parseInsertions(string(diff)) {
		data, err := os.ReadFile(filepath.Join(root, ins.path))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ins.path, err))
			continue
		}
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		if err := checkInsertion(lines, ins); err != nil {
			errs = append(errs, err)
		}
	}
	return errs, ""
}

func TestParseInsertionsKeepsPureInsertionHunks(t *testing.T) {
	diff := "+++ b/docs/a.md\n@@ -5,0 +6,2 @@\n+a\n+b\n@@ -8,0 +9 @@\n+c\n@@ -12 +12 @@\n-old\n+new\n@@ -20,2 +19,0 @@\n-a\n-b\n+++ /dev/null\n@@ -0,0 +1 @@\n+ignored\n+++ b/docs/a.txt\n@@ -0,0 +1 @@\n+ignored\n"
	got := parseInsertions(diff)
	want := []insertion{{"docs/a.md", 6, 7}, {"docs/a.md", 9, 9}}
	if len(got) != len(want) {
		t.Fatalf("insertions = %#v; want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("insertion %d = %#v; want %#v", i, got[i], want[i])
		}
	}
}

func TestInsertionRunningIntoNextParagraphIsFlagged(t *testing.T) {
	err := checkInsertion([]string{"intro", "", "new a", "new b", "old next"}, insertion{"docs/a.md", 3, 4})
	want := "docs/a.md:3-4: inserted lines touch unchanged line 5; put a blank line between paragraphs"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v; want %q", err, want)
	}
}

func TestInsertionRunningOntoPreviousParagraphIsFlagged(t *testing.T) {
	err := checkInsertion([]string{"old prev", "new a", "", "old next"}, insertion{"docs/a.md", 2, 2})
	want := "docs/a.md:2-2: inserted lines touch unchanged line 1; put a blank line between paragraphs"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v; want %q", err, want)
	}
}

func TestInsertionKeepingBlankLinesPasses(t *testing.T) {
	for _, tt := range []struct {
		name        string
		lines       []string
		first, last int
	}{
		{"between", []string{"old", " \t", "new", "", "next"}, 3, 3},
		{"start", []string{"new", "", "old"}, 1, 1},
		{"end", []string{"old", "", "new"}, 3, 3},
		{"whole", []string{"new a", "new b"}, 1, 2},
		{"firstBlank", []string{"old", " \t", "new", ""}, 2, 3},
		{"lastBlank", []string{"", "new", " \t", "old"}, 2, 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkInsertion(tt.lines, insertion{"docs/a.md", tt.first, tt.last}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func blanklineGit(t *testing.T, root string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	base := []string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}
	if out, err := exec.Command("git", append(base, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDocInsertionCheckFlagsAJoinedParagraphInAGitRepo(t *testing.T) {
	root := t.TempDir()
	blanklineGit(t, root, "init", "-b", "main")
	if err := os.Mkdir(filepath.Join(root, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "docs", "a.md")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("one\n\ntwo\n")
	blanklineGit(t, root, "add", "docs/a.md")
	blanklineGit(t, root, "commit", "-m", "base")
	for _, tt := range []struct{ name, text, want string }{
		{"joined", "one\n\nnew\ntwo\n", "docs/a.md:3-3: inserted lines touch unchanged line 4; put a blank line between paragraphs"},
		{"separated", "one\n\nnew\n\ntwo\n", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			write(tt.text)
			errs, skip := docInsertionErrors(root)
			if skip != "" {
				t.Fatalf("unexpected skip: %s", skip)
			}
			if tt.want == "" {
				if len(errs) != 0 {
					t.Fatalf("errors = %v; want none", errs)
				}
			} else if len(errs) != 1 || errs[0].Error() != tt.want {
				t.Fatalf("errors = %v; want %q", errs, tt.want)
			}
		})
	}
}

func TestDocInsertionCheckSkipsWithoutMain(t *testing.T) {
	for _, name := range []string{"notRepo", "otherBranch"} {
		t.Run(name, func(t *testing.T) {
			if _, err := exec.LookPath("git"); err != nil {
				t.Skip("git not found")
			}
			root := t.TempDir()
			if name == "notRepo" {
				// Reproduce TMPDIR pointing inside a checkout with main.
				blanklineGit(t, root, "init", "-b", "main")
				blanklineGit(t, root, "commit", "--allow-empty", "-m", "base")
				root = filepath.Join(root, "plain")
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
			} else {
				blanklineGit(t, root, "init", "-b", "other")
				blanklineGit(t, root, "commit", "--allow-empty", "-m", "base")
			}
			errs, skip := docInsertionErrors(root)
			if len(errs) != 0 || skip == "" {
				t.Fatalf("errors = %v, skip = %q; want skip only", errs, skip)
			}
		})
	}
}

func TestDocsInsertionsKeepParagraphsApart(t *testing.T) {
	errs, skip := docInsertionErrors(filepath.Join("..", ".."))
	if skip != "" {
		t.Skip(skip)
	}
	for _, err := range errs {
		t.Error(err)
	}
}
