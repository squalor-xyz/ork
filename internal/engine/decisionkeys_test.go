// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func decisionDiff(path, lines string) string {
	return "diff --git a/" + path + " b/" + path + "\n--- a/" + path + "\n+++ b/" + path + "\n@@ -1 +1 @@\n" + lines
}

func TestDiffCitedKeys(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       []string
	}{
		{"phrases", "+owner 2026-11-27 `z.last`\n+adj 12 [a.first]\n+adj (b.middle)\n+adj a.first\n", []string{"a.first", "b.middle", "z.last"}},
		{"decorations", "+(adj 12, `a.first`)\n+[adj b.middle]\n+owner 2026-11-27: (z.last)\n", []string{"a.first", "b.middle", "z.last"}},
		{"not citations", "+plain `a.first`\n-adj b.middle\n adj c.context\n+adjective d.nope\n+adjacent.key\n", nil},
		{"headers", "", nil},
		{"uppercase filename", "+adj README.md\n", nil},
		{"uppercase key", "+Adj Foo.Bar\n", nil},
		{"uppercase phrase", "+Adj a.key\n", []string{"a.key"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := "docs/x.md"
			if tc.name == "headers" {
				path = "docs/adj a.header"
			}
			if got := diffCitedKeys(decisionDiff(path, tc.text)); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("keys = %v, want %v", got, tc.want)
			}
		})
	}
	for _, path := range []string{".slicer/x.md", "decisions/x.md", "landed-changed/S537.md"} {
		t.Run(path, func(t *testing.T) {
			if got := diffCitedKeys(decisionDiff(path, "+adj a.first\n")); len(got) != 0 {
				t.Fatalf("excluded path keys = %v", got)
			}
		})
	}
}

func TestSpecOpenLines(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"`a.key` is open", []string{"a.key"}}, {"[a.key] is undecided", []string{"a.key"}},
		{"not decided: `a.key`", []string{"a.key"}}, {"`a.key` is OPEN", []string{"a.key"}},
		{"`a.key` is reopened", nil}, {"`a.key` decided", nil}, {"open without key", nil},
		{"`a.key`\nopen", nil},
		{"`a.spec-open-key` is decided", nil},
		{"[a.undecided-key] is decided", nil},
		{"Apply `review.spec-open-key` and `review.decision-key-citation`.", nil},
		{"`a.spec-open-key` is open", []string{"a.spec-open-key"}},
	} {
		t.Run(tc.line, func(t *testing.T) {
			if got := specOpenLines(tc.line); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("open keys = %v, want %v", got, tc.want)
			}
		})
	}
}

func decisionKeysHarness(t *testing.T, diff, spec, paths, failure string, adj bool, files map[string]string) *harness {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) {
		o.project = func(p *registry.Project) { p.Adj = adj }
		o.routesFor = func(P, W string) []clitest.Route {
			full := clitest.Route{Tool: "git", Args: []string{"-C", W, "diff", "--no-color", "--no-ext-diff", "--no-renames", "--unified=0", "base"}, Stdout: diff}
			if failure == "diff" {
				full.Exit = 1
			}
			routes := baseOverlayRoutes(W, paths, "", full)
			if failure == "branch" {
				routes[0].Exit = 1
			}
			if spec != "" {
				routes = append(routes, clitest.Route{Tool: "slicer", Args: []string{"show", item, "--json", "--lean", "--root", W}, Stdout: strings.Replace(sliceJSON(), "Because.", spec, 1)})
			}
			routes = append(routes, clitest.Route{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: `{"contract":"adj-json v1","version":1}`})
			for _, state := range []string{"missing", "decided", "open", "closed", "error"} {
				out := "{}"
				if state != "missing" && state != "error" {
					out = fmt.Sprintf(`{"%s":[{"id":42,"key":"test.%s"}]}`, state, state)
				}
				route := clitest.Route{Tool: "adj", Args: []string{"check", "test." + state, "--json", "--root", P}, Stdout: out}
				if state == "error" {
					route.Exit = 1
				}
				routes = append(routes, route)
			}
			return append(routes, ucsWorktree(P, W)...)
		}
	})
	h.deps.Adj = adjcli.Client{Bin: h.fake.Bin("adj")}
	ucsWrite(t, h, files)
	return h
}

func decisionSection(t *testing.T, h *harness, role, title string) string {
	t.Helper()
	brief := staleDocsRequest(t, h, role).Brief
	_, body, ok := strings.Cut(brief, "## "+title+"\n\n")
	if !ok {
		t.Fatalf("%s lacks %s", role, title)
	}
	body, _, _ = strings.Cut(body, "\n## ")
	return body
}

func TestUnfoundDecisionKeysSection(t *testing.T) {
	h := decisionKeysHarness(t, decisionDiff("docs/x.md", "+adj test.missing\n+adj test.decided\n+adj test.open\n+adj test.closed\n"), "", "", "", true, nil)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	for _, role := range []string{RoleReviewer, RoleGate} {
		body := decisionSection(t, h, role, "Decision keys not found")
		if !strings.Contains(body, "- `test.missing`") {
			t.Errorf("missing key absent: %s", body)
		}
		for _, key := range []string{"test.decided", "test.open", "test.closed"} {
			if strings.Contains(body, key) {
				t.Errorf("nonmissing key %s listed: %s", key, body)
			}
		}
	}
}

func TestUnfoundDecisionKeysUntrackedFile(t *testing.T) {
	h := decisionKeysHarness(t, "", "", "docs/new.md\x00", "", true, map[string]string{"docs/new.md": "adj test.missing\n"})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	for _, role := range []string{RoleReviewer, RoleGate} {
		if body := decisionSection(t, h, role, "Decision keys not found"); !strings.Contains(body, "- `test.missing`") {
			t.Fatalf("untracked key absent: %s", body)
		}
	}
}

func TestUnfoundDecisionKeysAdvisory(t *testing.T) {
	for _, failure := range []string{"adj", "diff", "branch", "file", "disabled"} {
		t.Run(failure, func(t *testing.T) {
			path := ""
			if failure == "file" {
				path = "docs/missing.md\x00"
			}
			h := decisionKeysHarness(t, decisionDiff("docs/x.md", "+adj test.error\n"), "", path, failure, failure != "disabled", nil)
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			for _, role := range []string{RoleReviewer, RoleGate} {
				if failure == "disabled" {
					if strings.Contains(staleDocsRequest(t, h, role).Brief, "## Decision keys not found\n") {
						t.Fatal("section without adj")
					}
					continue
				}
				body := decisionSection(t, h, role, "Decision keys not found")
				want := "Check incomplete"
				if failure == "adj" {
					want = "Check incomplete: adj check test.error could not be read"
				}
				if failure == "file" {
					want = "docs/missing.md"
				}
				if !strings.Contains(body, want) {
					t.Fatalf("missing %q: %s", want, body)
				}
			}
		})
	}
}

func TestUnfoundDecisionKeysOneCheckPerKey(t *testing.T) {
	h := decisionKeysHarness(t, decisionDiff("docs/x.md", "+adj test.missing\n+adj test.missing\n+adj test.decided\n"), "`test.decided` is open", "", "", true, nil)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	for _, role := range []string{RoleReviewer, RoleGate} {
		decisionSection(t, h, role, "Decision keys not found")
		decisionSection(t, h, role, "Spec text calls a decided key open")
	}
	if got := len(h.calls("adj", "check", "test.missing")); got != 2 {
		t.Fatalf("duplicate citation checks = %d, want 2", got)
	}
	// decisionPathsSection keeps its independent reviewer check; the two new
	// sections share one additional check in each role round.
	if got := len(h.calls("adj", "check", "test.decided")); got != 3 {
		t.Fatalf("shared key checks = %d, want 3", got)
	}
}

func TestSpecCallsDecidedKeyOpen(t *testing.T) {
	for _, state := range []string{"decided", "open", "closed", "missing"} {
		t.Run(state, func(t *testing.T) {
			line := "`test." + state + "` is open"
			h := decisionKeysHarness(t, "", line, "", "", true, nil)
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			for _, role := range []string{RoleReviewer, RoleGate} {
				if state != "decided" {
					if strings.Contains(staleDocsRequest(t, h, role).Brief, "## Spec text calls a decided key open\n") {
						t.Fatal("flagged undecided key")
					}
					continue
				}
				body := decisionSection(t, h, role, "Spec text calls a decided key open")
				for _, want := range []string{"Why", line, "42"} {
					if !strings.Contains(body, want) {
						t.Errorf("missing %q: %s", want, body)
					}
				}
			}
		})
	}
}

func TestSpecCallsDecidedKeyOpenAdvisory(t *testing.T) {
	for _, adj := range []bool{true, false} {
		t.Run(fmt.Sprint(adj), func(t *testing.T) {
			h := decisionKeysHarness(t, "", "`test.error` is open", "", "", adj, nil)
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			for _, role := range []string{RoleReviewer, RoleGate} {
				if !adj {
					if strings.Contains(staleDocsRequest(t, h, role).Brief, "## Spec text calls a decided key open\n") {
						t.Fatal("section without adj")
					}
					continue
				}
				if body := decisionSection(t, h, role, "Spec text calls a decided key open"); !strings.Contains(body, "Check incomplete: adj check test.error could not be read") {
					t.Fatalf("missing incomplete notice: %s", body)
				}
			}
		})
	}
}
