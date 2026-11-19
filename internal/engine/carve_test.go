// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const carveReport = "## Slivers\n\n" +
	"### 1. Add the Parent field\n\nFiles:\n- `internal/slicercli/write.go`\n- `internal/slicercli/add_test.go:89`\n\nFailing tests:\n- TestAddItemArguments names --parent\n\nCheck:\n- `go test ./internal/slicercli`\n\n" +
	"### 2) Wire the carve step\n\nFiles:\n- `internal/daemon/runs.go`\n\nFailing tests:\n- a local run files its slivers\n- a cloud run is not slivered\n\nCheck:\n- `go test ./internal/daemon`\n- `gofmt -l .`\n\n" +
	"## Owner needed\nnone\n"

func TestParseCarveReadsEachSliversTitleFilesTestsAndChecks(t *testing.T) {
	got, err := ParseCarve(carveReport)
	if err != nil {
		t.Fatal(err)
	}
	want := []CarvedSliver{
		{Title: "Add the Parent field", Files: []string{"internal/slicercli/write.go", "internal/slicercli/add_test.go:89"}, Tests: []string{"TestAddItemArguments names --parent"}, Checks: []string{"`go test ./internal/slicercli`"}},
		{Title: "Wire the carve step", Files: []string{"internal/daemon/runs.go"}, Tests: []string{"a local run files its slivers", "a cloud run is not slivered"}, Checks: []string{"`go test ./internal/daemon`", "`gofmt -l .`"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// A Files bullet is the first backticked path, with an anchor that follows the
// closing backtick; text around it is not part of the path.
func TestParseCarveTakesThePathFromAFilesBullet(t *testing.T) {
	text := "## Slivers\n\n### 1. Paths\n\nFiles:\n- `internal/a.go` (add the flag)\n- `internal/b.go`:12-14\n\nFailing tests:\n- t\n\nCheck:\n- c\n\n### 2. Bare\n\nFiles:\n- internal/c.go\n\nFailing tests:\n- t\n\nCheck:\n- c\n\n## Owner needed\nnone\n"
	got, err := ParseCarve(text)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v, %v", got, err)
	}
	if want := []string{"internal/a.go", "internal/b.go:12-14"}; !reflect.DeepEqual(got[0].Files, want) {
		t.Errorf("files %q, want %q", got[0].Files, want)
	}
	if want := []string{"internal/c.go"}; !reflect.DeepEqual(got[1].Files, want) {
		t.Errorf("files %q, want %q", got[1].Files, want)
	}
}

func TestParseCarveRefusesAReportThatBreaksTheShape(t *testing.T) {
	one := "### 1. Only\n\nFiles:\n- `a.go`\n\nFailing tests:\n- t\n\nCheck:\n- c\n\n"
	for _, tc := range []struct{ name, text, want string }{
		{"no section", "## Owner needed\nnone\n", "## Slivers"},
		{"no slivers", "## Slivers\n\nnothing\n\n## Owner needed\nnone\n", "no sliver"},
		{"no owner section", "## Slivers\n\n" + one, "## Owner needed"},
		{"owner question", "## Slivers\n\n" + one + "## Owner needed\nQuestion: which file?\n", "which file?"},
		{"three files", "## Slivers\n\n### 1. Big\n\nFiles:\n- `a.go`\n- `b.go`\n- `c.go`\n\nFailing tests:\n- t\n\nCheck:\n- c\n\n## Owner needed\nnone\n", "one or two files"},
		{"no files", "## Slivers\n\n### 1. Bare\n\nFailing tests:\n- t\n\nCheck:\n- c\n\n## Owner needed\nnone\n", "Files"},
		{"no tests", "## Slivers\n\n### 1. Bare\n\nFiles:\n- `a.go`\n\nCheck:\n- c\n\n## Owner needed\nnone\n", "Failing tests"},
		{"no check", "## Slivers\n\n### 1. Bare\n\nFiles:\n- `a.go`\n\nFailing tests:\n- t\n\n## Owner needed\nnone\n", "Check"},
		{"no title", "## Slivers\n\n### 1.\n\nFiles:\n- `a.go`\n\nFailing tests:\n- t\n\nCheck:\n- c\n\n## Owner needed\nnone\n", "title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCarve(tc.text)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %+v, %v; want an error naming %q", got, err, tc.want)
			}
		})
	}
}

func TestParseCarveIgnoresToolFeedbackLines(t *testing.T) {
	text := strings.Replace(carveReport, "## Owner needed\nnone\n", "## Owner needed\nnone\n\ntool feedback: 1. spec-check | tool: slicer | kind: friction | proposal: x | evidence: y\n", 1)
	if got, err := ParseCarve(text); err != nil || len(got) != 2 {
		t.Fatalf("%+v, %v", got, err)
	}
}

func carveConfig(h *harness, t *testing.T) CarveConfig {
	return CarveConfig{
		Project: h.proj, Item: "S10", Dir: t.TempDir(),
		Lineup: []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}, {Vendor: "codex", Model: "terra", Effort: "high"}},
	}
}

func carveScript(report string) func(Request, int) (Result, error) {
	return func(req Request, _ int) (Result, error) { return Result{Exit: ExitOK, Report: report}, nil }
}

func TestRunCarveIsOneReadOnlyPassOfTheSpecCheckRow(t *testing.T) {
	h := specHarnessStatusReads(t, nil, "")
	h.runner.script = carveScript(carveReport)
	cfg := carveConfig(h, t)
	got, err := RunCarve(context.Background(), h.deps, cfg)
	if err != nil || len(got) != 2 || got[0].Title != "Add the Parent field" {
		t.Fatalf("%+v, %v", got, err)
	}
	reqs := h.runner.requests()
	if len(reqs) != 1 || reqs[0].Role != RoleSpecCheck || !reqs[0].ReadOnly || reqs[0].Model != "sonnet" || reqs[0].Worktree != h.proj.Path {
		t.Fatalf("requests: %+v", reqs)
	}
	for _, want := range []string{"Role: carve.", "## Slivers", "one or two files", "Give a ## Slivers section"} {
		if !strings.Contains(reqs[0].Brief, want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	for _, bad := range []string{"Role: spec-check", "## Slice ID", "Check command classification"} {
		if strings.Contains(reqs[0].Brief, bad) {
			t.Errorf("the carve brief holds the spec check's %q", bad)
		}
	}
	if b, err := os.ReadFile(filepath.Join(cfg.Dir, "carve.md")); err != nil || string(b) != carveReport {
		t.Errorf("saved report: %q, %v", b, err)
	}
}

func TestRunCarveRefusesARoleThatChangedTheTree(t *testing.T) {
	h := specHarnessStatusReads(t, nil, "", " M edited.go\n")
	h.runner.script = carveScript(carveReport)
	if got, err := RunCarve(context.Background(), h.deps, carveConfig(h, t)); !errors.Is(err, errCarveStatusChanged) || !strings.HasSuffix(err.Error(), ": changed edited.go") {
		t.Fatalf("a carve that changed the checkout: %+v, %v", got, err)
	}
}

func TestRunCarveRefusesAMovingHead(t *testing.T) {
	h := specHarnessHead(t, []clitest.Reply{{Stdout: "abc\n"}, {Stdout: "def\n"}})
	h.runner.script = carveScript(carveReport)
	if got, err := RunCarve(context.Background(), h.deps, carveConfig(h, t)); !errors.Is(err, errSpecMainMoved) {
		t.Fatalf("a carve while main moved: %+v, %v", got, err)
	}
}

func TestRunCarveFallsBackToTheNextRowWhenCreditsRunOut(t *testing.T) {
	h := specHarnessStatusReads(t, nil, "")
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Vendor == "claude" {
			return Result{Exit: ExitOutOfCredits}, nil
		}
		return Result{Exit: ExitOK, Report: carveReport}, nil
	}
	got, err := RunCarve(context.Background(), h.deps, carveConfig(h, t))
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v, %v", got, err)
	}
	if reqs := h.runner.requests(); len(reqs) != 2 || reqs[1].Vendor != "codex" {
		t.Fatalf("requests: %+v", reqs)
	}
}

// FileSliver files one carved sliver under its parent at size S and effort 1
// (rule u6), then gives it the Files, Failing tests and Check sections.
func TestFileSliverAddsItUnderTheParentAtSizeSEffortOne(t *testing.T) {
	d, c, p := newSliceCapture(t)
	sl := CarvedSliver{Title: "Add the Parent field", Files: []string{"internal/a.go"}, Tests: []string{"a test"}, Checks: []string{"`go test ./...`"}}
	id, err := FileSliver(context.Background(), d, registry.Project{Name: "p", Path: c.Project.Path}, "S9", 1, sl, c.TempDir)
	if err != nil || id != "S184" {
		t.Fatalf("%q, %v", id, err)
	}
	calls, _ := os.ReadFile(p + ".calls")
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "add Add the Parent field --parent S9 ") || !strings.Contains(lines[0], "--size S") || !strings.Contains(lines[0], "--effort 1") || !strings.HasPrefix(lines[1], "promote S184 --file ") {
		t.Fatalf("calls: %q", lines)
	}
	b, _ := os.ReadFile(p)
	for _, want := range []string{"## Add the Parent field\n", "### Why\n\nA sliver of S9", "### Files\n\n- `internal/a.go`\n", "### Failing tests\n\n- a test\n", "### Check\n\n- `go test ./...`\n"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("outline lacks %q:\n%s", want, b)
		}
	}
}
