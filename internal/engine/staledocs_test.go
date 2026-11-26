// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// staleDocsDiffRoute answers the role prompt diff read against the merge base
// "base" with stdout.
func staleDocsDiffRoute(W, stdout string) clitest.Route {
	return clitest.Route{Tool: "git", Args: []string{
		"-C", W, "diff", "--no-color", "--no-ext-diff", "--no-renames", "--unified=0", "base",
		"--", "internal/roles", ":(exclude)internal/roles/*_test.go",
	}, Stdout: stdout}
}

// staleDocsRemovedDiff is a diff that removes a line naming `adj find`.
const staleDocsRemovedDiff = "diff --git a/internal/roles/prompts/x.md b/internal/roles/prompts/x.md\n" +
	"index 1111111..2222222 100644\n" +
	"--- a/internal/roles/prompts/x.md\n" +
	"+++ b/internal/roles/prompts/x.md\n" +
	"@@ -76 +76 @@\n" +
	"-Never use `adj find`.\n" +
	"+Use `adj check TEXT`.\n"

// staleDocsDesign is a docs file whose line 4 names `adj find`.
const staleDocsDesign = "# Design\n\nThe engine.\nRun `adj find` first.\n"

// staleDocsHarness runs a round whose role prompt diff is diff and whose
// worktree holds files.
func staleDocsHarness(t *testing.T, diff string, files map[string]string) *harness {
	t.Helper()
	return ucsReviewerHarness(t, files, "", func(W string) []clitest.Route {
		return []clitest.Route{staleDocsDiffRoute(W, diff)}
	})
}

// staleDocsBody returns the body of the brief's Stale doc references section.
func staleDocsBody(brief string) (string, bool) {
	_, body, found := strings.Cut(brief, "## Stale doc references\n\n")
	if !found {
		return "", false
	}
	body, _, _ = strings.Cut(body, "\n## ")
	return body, true
}

// staleDocsRequest returns the first request the run made for role.
func staleDocsRequest(t *testing.T, h *harness, role string) Request {
	t.Helper()
	for _, req := range h.runner.requests() {
		if req.Role == role {
			return req
		}
	}
	t.Fatalf("no %s ran: %v", role, h.runner.roles())
	return Request{}
}

func TestStaleDocsCommandNames(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		want  []string
	}{
		{"plain command", []string{"Never use `adj find`."}, []string{"adj find"}},
		{"argument is dropped", []string{"`adj check TEXT`"}, []string{"adj check"}},
		{"flag after the arguments", []string{"`slicer show S05 --root DIR`"}, []string{"slicer show"}},
		{"two spans on one line", []string{"Run `adj find` or `slicer show S05`."}, []string{"adj find", "slicer show"}},
		{"sorted and de-duplicated", []string{"`slicer show`", "`adj find`", "`adj find X`"}, []string{"adj find", "slicer show"}},
		{"digits and hyphens", []string{"`relay-roles show`"}, []string{"relay-roles show"}},
		{"flag as second word", []string{"`go -C DIR test`"}, nil},
		{"uppercase second word", []string{"`adj KEY`"}, nil},
		{"one word", []string{"`ork`"}, nil},
		{"unpaired backtick", []string{"Never use `adj find"}, nil},
		{"no backticks", []string{"adj find"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := staleDocNames(tc.lines); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("staleDocNames(%q) = %q, want %q", tc.lines, got, tc.want)
			}
		})
	}
}

func TestStaleDocsRemovedLines(t *testing.T) {
	diff := "diff --git a/internal/roles/a.md b/internal/roles/a.md\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/internal/roles/a.md\n" +
		"+++ b/internal/roles/a.md\n" +
		"@@ -3,2 +3 @@\n" +
		"-first removed\n" +
		"--- starts with dashes\n" +
		"+added line\n" +
		"diff --git a/internal/roles/b.md b/internal/roles/b.md\n" +
		"--- a/internal/roles/b.md\n" +
		"+++ b/internal/roles/b.md\n" +
		"@@ -9 +9 @@\n" +
		"-second file removed\n" +
		"+++ starts with pluses\n"
	want := []string{"first removed", "-- starts with dashes", "second file removed"}
	if got := removedPromptLines(diff); !reflect.DeepEqual(got, want) {
		t.Errorf("removedPromptLines = %q, want %q", got, want)
	}
	if got := removedPromptLines(""); len(got) != 0 {
		t.Errorf("removedPromptLines of nothing = %q", got)
	}
}

func staleDocsWrite(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, src := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStaleDocsHits(t *testing.T) {
	root := t.TempDir()
	staleDocsWrite(t, root, map[string]string{
		"docs/a.md": "# A\n\nuse adj find here\nadj finder and badj find\nadj find and adj check\n" +
			"adj-find, xadj check, adj check_x\nends with adj check\nadj find\nbadj find, then adj find\n",
		"docs/sub/b.md": "1\n2\n3\n4\n5\n6\nadj check",
		"docs/none.md":  "nothing here\n",
		"README.md":     "adj find outside docs\n",
	})
	names := []string{"adj check", "adj find"}
	hits, unread := staleDocHits(root, names)
	want := []staleDocHit{
		{Path: "docs/a.md", Line: 3, Names: []string{"adj find"}},
		{Path: "docs/a.md", Line: 5, Names: []string{"adj check", "adj find"}},
		{Path: "docs/a.md", Line: 7, Names: []string{"adj check"}},
		{Path: "docs/a.md", Line: 8, Names: []string{"adj find"}},
		{Path: "docs/a.md", Line: 9, Names: []string{"adj find"}},
		{Path: "docs/sub/b.md", Line: 7, Names: []string{"adj check"}},
	}
	if !reflect.DeepEqual(hits, want) {
		t.Errorf("hits = %+v, want %+v", hits, want)
	}
	if len(unread) != 0 {
		t.Errorf("unread = %q, want none", unread)
	}

	hits, unread = staleDocHits(t.TempDir(), names)
	if len(hits) != 0 || len(unread) != 0 {
		t.Errorf("missing docs/: hits = %+v, unread = %q, want none", hits, unread)
	}
}

func TestStaleDocsHitLimit(t *testing.T) {
	var big strings.Builder
	for i := 1; i <= 25; i++ {
		fmt.Fprintf(&big, "line %d runs `adj find`\n", i)
	}
	h := staleDocsHarness(t, staleDocsRemovedDiff, map[string]string{"docs/big.md": big.String()})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	body, found := staleDocsBody(staleDocsRequest(t, h, RoleReviewer).Brief)
	if !found {
		t.Fatalf("reviewer brief lacks the Stale doc references section")
	}
	var got []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "- `") {
			got = append(got, line)
		}
	}
	if len(got) != 20 {
		t.Fatalf("%d entries, want 20:\n%s", len(got), body)
	}
	for i, line := range got {
		if want := fmt.Sprintf("- `docs/big.md:%d`: `adj find`", i+1); line != want {
			t.Errorf("entry %d = %q, want %q", i, line, want)
		}
	}
	if !strings.Contains(body, "5 more hits were left out (ork lists at most 20)") {
		t.Errorf("section does not say 5 hits were left out:\n%s", body)
	}
}

func TestStaleDocsReviewerAndGateBriefs(t *testing.T) {
	h := architectHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return append(baseOverlayRoutes(W, "", "", staleDocsDiffRoute(W, staleDocsRemovedDiff)), ucsWorktree(P, W)...)
		}
	})
	ucsWrite(t, h, map[string]string{"docs/design.md": staleDocsDesign})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	seen := map[string]bool{}
	for _, req := range h.runner.requests() {
		seen[req.Role] = true
		body, has := staleDocsBody(req.Brief)
		if want := req.Role == RoleReviewer || req.Role == RoleGate; has != want {
			t.Errorf("%s brief holds a Stale doc references section = %v, want %v", req.Role, has, want)
			continue
		}
		if !has {
			continue
		}
		if entry := "- `docs/design.md:4`: `adj find`"; !strings.Contains(body, entry) {
			t.Errorf("%s section lacks %q:\n%s", req.Role, entry, body)
		}
		if !strings.Contains(body, "advisory") {
			t.Errorf("%s section does not say it is advisory:\n%s", req.Role, body)
		}
		if strings.Contains(body, "Check incomplete") {
			t.Errorf("%s section reports an incomplete check:\n%s", req.Role, body)
		}
	}
	for _, role := range []string{RoleReviewer, RoleArchitect, RoleGate} {
		if !seen[role] {
			t.Fatalf("role %s did not run: %v", role, h.runner.roles())
		}
	}
}

func TestStaleDocsSearchesNameAddedBackElsewhere(t *testing.T) {
	diff := staleDocsRemovedDiff + "+Also say `adj find` here.\n"
	h := staleDocsHarness(t, diff, map[string]string{"docs/design.md": staleDocsDesign})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	body, found := staleDocsBody(staleDocsRequest(t, h, RoleReviewer).Brief)
	if !found || !strings.Contains(body, "- `docs/design.md:4`: `adj find`") {
		t.Errorf("section does not list the docs hit for the name added back (found %v):\n%s", found, body)
	}
}

func TestStaleDocsNoSectionWithoutHits(t *testing.T) {
	for _, tc := range []struct {
		name string
		diff string
		docs map[string]string
	}{
		{"empty diff", "", map[string]string{"docs/design.md": staleDocsDesign}},
		{"no match in docs", staleDocsRemovedDiff, map[string]string{"docs/design.md": "# Design\n\nNothing here.\n"}},
		{"no docs directory", staleDocsRemovedDiff, map[string]string{"README.md": staleDocsDesign}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := staleDocsHarness(t, tc.diff, tc.docs)
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			for _, role := range []string{RoleReviewer, RoleGate} {
				if body, found := staleDocsBody(staleDocsRequest(t, h, role).Brief); found {
					t.Errorf("%s brief holds a Stale doc references section:\n%s", role, body)
				}
			}
		})
	}
}

func TestStaleDocsAdvisory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  map[string]string
		mutate func(routes []clitest.Route) // baseOverlayRoutes order: merge-base, diff, ls-files, extra
		chmod  string
	}{
		{name: "diff read fails", files: map[string]string{"docs/design.md": staleDocsDesign}, mutate: func(r []clitest.Route) {
			r[len(r)-1].Exit = 1
		}},
		{name: "merge base read fails", files: map[string]string{"docs/design.md": staleDocsDesign}, mutate: func(r []clitest.Route) {
			r[0].Exit = 1
		}},
		{name: "doc file unreadable", files: map[string]string{"docs/design.md": staleDocsDesign, "docs/locked.md": "adj find\n"}, chmod: "docs/locked.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.routesFor = func(P, W string) []clitest.Route {
					routes := baseOverlayRoutes(W, "", "", staleDocsDiffRoute(W, staleDocsRemovedDiff))
					if tc.mutate != nil {
						tc.mutate(routes)
					}
					return append(routes, ucsWorktree(P, W)...)
				}
			})
			ucsWrite(t, h, tc.files)
			if tc.chmod != "" {
				locked := filepath.Join(h.W, filepath.FromSlash(tc.chmod))
				if err := os.Chmod(locked, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(locked, 0o644) })
				if _, err := os.ReadFile(locked); err == nil {
					t.Skip("the file stays readable with mode 0 (running as root)")
				}
			}
			rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
			if rec.Pending != nil {
				t.Errorf("the run paused: %+v", rec.Pending)
			}
			for _, role := range []string{RoleReviewer, RoleGate} {
				body, found := staleDocsBody(staleDocsRequest(t, h, role).Brief)
				if !found {
					t.Errorf("%s brief lacks the Stale doc references section", role)
					continue
				}
				for _, want := range []string{"Check incomplete", "does not block the run"} {
					if !strings.Contains(body, want) {
						t.Errorf("%s section lacks %q:\n%s", role, want, body)
					}
				}
			}
		})
	}
}
