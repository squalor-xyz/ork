// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestScopeWarningsNamesPathsOutsideTheList(t *testing.T) {
	changed := []string{"internal/engine/check.go", "CHANGELOG.md", "docs/design.md", "docs/design.md.bak", "CHANGELOG.md"}
	want := []string{"CHANGELOG.md", "docs/design.md.bak"}
	for _, allowed := range [][]string{{"internal/engine", "docs/design.md"}, {"internal/engine/", "docs/design.md"}} {
		if got := ScopeWarnings(changed, allowed); !reflect.DeepEqual(got, want) {
			t.Fatalf("warnings %v, want %v", got, want)
		}
	}
}

func TestScopeWarningsNoneWhenEveryPathIsListed(t *testing.T) {
	if got := ScopeWarnings([]string{"internal/engine", "internal/engine/check.go", "docs/design.md"}, []string{"internal/engine", "docs/design.md"}); len(got) != 0 {
		t.Fatalf("warnings %v", got)
	}
}

func TestScopeWarningsTrackingFilesAreNeverOutOfScope(t *testing.T) {
	if got := ScopeWarnings([]string{".slicer/index.json", ".slicer/render/ROADMAP.md"}, []string{"internal/engine"}); len(got) != 0 {
		t.Fatalf("warnings %v", got)
	}
}

func TestScopeWarningsNeedsAList(t *testing.T) {
	if got := ScopeWarnings([]string{"CHANGELOG.md"}, nil); len(got) != 0 {
		t.Fatalf("warnings %v", got)
	}
}

func TestFilesScopeReadsOnlyChangedPaths(t *testing.T) {
	out := " M modified\x00A  added\x00 D deleted\x00?? untracked file\x00R  new name\x00old name\x00 C copy new\x00copy old\x00!! ignored\x00"
	want := []string{"modified", "added", "deleted", "untracked file", "new name", "old name", "copy new", "copy old"}
	if got := parsePorcelainZ(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("paths %q, want %q", got, want)
	}
}

func filesScopeHarness(t *testing.T, paths []string, status string, failure string) *harness {
	t.Helper()
	rawPaths, _ := json.Marshal(paths)
	raw := strings.TrimSuffix(sliceJSON(), "}") + ",\"paths\":" + string(rawPaths) + "}"
	return newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			show := clitest.Route{Tool: "slicer", Args: []string{"show", item, "--json", "--lean", "--root", W}, Stdout: raw}
			// Implementer and reviewer read the spec first; the next read is the
			// advisory scope check. The gate's own spec read must still succeed.
			if failure == "show" || failure == "decode" {
				bad := clitest.Reply{Exit: 1, Stderr: "show unavailable"}
				if failure == "decode" {
					bad = clitest.Reply{Stdout: "{"}
				}
				show.Seq = []clitest.Reply{{Stdout: raw}, {Stdout: raw}, bad, {Stdout: raw}}
			}
			git := clitest.Route{Tool: "git", Args: []string{"-C", W, "status", "--porcelain=v1", "-z", "--untracked-files=all"}, Stdout: status}
			if failure == "git" {
				git.Exit = 1
				git.Stderr = "status unavailable"
			}
			return []clitest.Route{show, git}
		}
	})
}

func scopeGateBrief(t *testing.T, h *harness) (string, runrec.Record) {
	t.Helper()
	e := h.start()
	rec := e.run.Snapshot()
	for _, req := range h.runner.requests() {
		if req.Role == RoleGate {
			return req.Brief, rec
		}
	}
	t.Fatalf("gate did not run: state %s, pending %+v", rec.State, rec.Pending)
	return "", rec
}

func TestGateBriefListsTheOutOfScopeFiles(t *testing.T) {
	h := filesScopeHarness(t, []string{"internal/engine"}, " M CHANGELOG.md\x00 M internal/engine/check.go\x00 M .slicer/index.json\x00", "")
	brief, _ := scopeGateBrief(t, h)
	_, body, ok := strings.Cut(brief, "## Out of scope files\n\n")
	if !ok {
		t.Fatal("no Out of scope files section")
	}
	for _, want := range []string{"CHANGELOG.md", "warning", "Files list", "adj 119", "gate", "does not fail the gate"} {
		if !strings.Contains(body, want) {
			t.Errorf("scope section lacks %q: %s", want, body)
		}
	}
	if strings.Contains(body, "internal/engine/check.go") || strings.Contains(body, ".slicer/index.json") {
		t.Fatal("scope section names covered or tracking files")
	}
}

func TestGateBriefHasNoScopeSectionWhenEveryFileIsListed(t *testing.T) {
	for _, paths := range [][]string{{"internal/engine", "CHANGELOG.md"}, nil} {
		t.Run(strings.Join(paths, ","), func(t *testing.T) {
			h := filesScopeHarness(t, paths, " M CHANGELOG.md\x00 M internal/engine/check.go\x00", "")
			brief, _ := scopeGateBrief(t, h)
			if strings.Contains(brief, "## Out of scope files") {
				t.Fatal("unexpected scope section")
			}
			if len(paths) == 0 && len(h.calls("git", "-C", h.W, "status", "--porcelain=v1")) != 0 {
				t.Fatal("status read without a Files list")
			}
		})
	}
}

func TestFilesScopeNeverStopsOrPausesARun(t *testing.T) {
	var baseline runrec.Record
	for _, tc := range []struct{ name, status, failure string }{
		{"listed", " M internal/engine/check.go\x00", ""},
		{"unlisted", " M CHANGELOG.md\x00", ""},
		{"show fails", " M CHANGELOG.md\x00", "show"},
		{"show malformed", " M CHANGELOG.md\x00", "decode"},
		{"git fails", " M CHANGELOG.md\x00", "git"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := filesScopeHarness(t, []string{"internal/engine"}, tc.status, tc.failure)
			brief, rec := scopeGateBrief(t, h)
			if tc.name == "listed" {
				baseline = rec
			}
			if rec.State != runrec.StateAwaitingApproval || rec.State != baseline.State || !reflect.DeepEqual(rec.Pending, baseline.Pending) {
				t.Fatalf("scope changed state or pending: %s %+v", rec.State, rec.Pending)
			}
			if tc.failure != "" && strings.Contains(brief, "## Out of scope files") {
				t.Fatal("failed read adds scope section")
			}
		})
	}
}
