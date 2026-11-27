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
	"github.com/squalor-xyz/ork/internal/runrec"
)

const draftReport = `## Section Why
Why text, first line.

Second paragraph.

## Section Files
- ` + "`internal/a/a.go`" + `

## Section Failing tests
- TestA: a asserts b.

## Section Implement
1. Do it.

### Detail
A level-three heading stays inside the section.

## Section Check
unchanged

## Owner needed
none
`

func draftNames(secs []SpecDraftSection) []string {
	var out []string
	for _, s := range secs {
		out = append(out, s.Name)
	}
	return out
}

func TestSpecDraftReportParsesOneBlockPerSection(t *testing.T) {
	d, err := ParseSpecDraft(draftReport)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Why", "Files", "Failing tests", "Implement", "Check"}; !reflect.DeepEqual(draftNames(d.Sections), want) {
		t.Fatalf("sections %v, want %v", draftNames(d.Sections), want)
	}
	if got := d.Sections[0].Text; got != "Why text, first line.\n\nSecond paragraph." {
		t.Errorf("Why text %q", got)
	}
	if got := d.Sections[3].Text; !strings.Contains(got, "### Detail") || !strings.HasPrefix(got, "1. Do it.") {
		t.Errorf("Implement text %q", got)
	}
	if !d.Sections[4].Unchanged || d.Sections[4].Text != "" {
		t.Errorf("Check should be unchanged: %+v", d.Sections[4])
	}
	if d.Sections[0].Unchanged {
		t.Errorf("Why is not unchanged: %+v", d.Sections[0])
	}
	if d.OwnerNeeded != "" {
		t.Errorf("owner needed %q, want none", d.OwnerNeeded)
	}

	// The section name is read without regard to case, and the Owner needed
	// text of the role comes back for the owner.
	withOwner := strings.Replace(draftReport, "## Section Why", "## Section why", 1)
	withOwner = strings.Replace(withOwner, "## Owner needed\nnone", "## Owner needed\nQuestion: Which?\nOptions:\n1. a: A\n2. b: B\nRecommend: 1 - because", 1)
	d, err = ParseSpecDraft(withOwner)
	if err != nil || d.Sections[0].Name != "Why" || !strings.HasPrefix(d.OwnerNeeded, "Question: Which?") {
		t.Fatalf("case and owner text: %+v, %v", d, err)
	}

	for _, tc := range []struct {
		name, text, want string
	}{
		{"unknown section", strings.Replace(draftReport, "## Section Check", "## Section Checks", 1), "Checks"},
		{"missing block", strings.Replace(draftReport, "## Section Check\nunchanged\n\n", "", 1), "no block for Check"},
		{"two blocks", draftReport + "\n## Section Why\nagain\n", "two blocks for Why"},
		{"no owner needed", strings.Replace(draftReport, "## Owner needed\nnone\n", "", 1), "Owner needed"},
		{"empty block", strings.Replace(draftReport, "- TestA: a asserts b.\n", "", 1), "Failing tests: empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSpecDraft(tc.text)
			var shape *ShapeError
			if !errors.As(err, &shape) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want a shape error naming %q, got %v", tc.want, err)
			}
		})
	}
}

// S341: the draft's read-only error names the paths that changed, and still
// is errDraftTreeChanged.
func TestSpecDraftTreeChangeNamesThePaths(t *testing.T) {
	h := specHarnessStatusReads(t, nil, "", "", "?? src/a.py\n")
	h.runner.script = func(Request, int) (Result, error) {
		return Result{Exit: ExitOK, Report: draftReport}, nil
	}
	cfg := SpecDraftConfig{Project: h.proj, Item: "S10", Dir: t.TempDir(), Lineup: []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}}}
	_, err := RunSpecDraft(context.Background(), h.deps, cfg)
	if !errors.Is(err, errDraftTreeChanged) || !strings.HasSuffix(err.Error(), ": changed src/a.py") {
		t.Fatalf("error %v", err)
	}
}

func TestSpecDraftShapeFailureKeepsTheReport(t *testing.T) {
	h := specHarness(t)
	malformed := strings.ReplaceAll(draftReport, "## Section ", "## ")
	h.runner.script = func(req Request, _ int) (Result, error) {
		if err := os.WriteFile(req.ReportPath, []byte(malformed), 0o600); err != nil {
			t.Fatal(err)
		}
		return Result{Exit: ExitOK, Session: "sess"}, nil
	}
	dir := t.TempDir()
	_, err := RunSpecDraft(context.Background(), h.deps, SpecDraftConfig{Project: h.proj, Item: "S10", Dir: dir, Lineup: []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}}})
	var shape *ShapeError
	if !errors.As(err, &shape) {
		t.Errorf("want ShapeError, got %v", err)
	}
	if err == nil || !strings.HasSuffix(err.Error(), "[report kept in "+filepath.Join(dir, "spec-draft-failed-2.md")+"]") {
		t.Errorf("error does not name second kept report: %v", err)
	}
	for _, name := range []string{"spec-draft-failed-1.md", "spec-draft-failed-2.md"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != malformed {
			t.Errorf("%s: report %q, error %v; want %q", name, got, err, malformed)
		}
	}
}

func TestSpecDraftShapeSuccessKeepsNoFailedReport(t *testing.T) {
	h := specHarness(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spec-draft-failed-1.md"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.runner.script = func(Request, int) (Result, error) {
		return Result{Exit: ExitOK, Report: draftReport}, nil
	}
	_, err := RunSpecDraft(context.Background(), h.deps, SpecDraftConfig{Project: h.proj, Item: "S10", Dir: dir, Lineup: []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}}})
	if err != nil {
		t.Fatal(err)
	}
	if files, err := filepath.Glob(filepath.Join(dir, "spec-draft-failed-*.md")); err != nil || len(files) != 0 {
		t.Fatalf("failed reports %v, error %v; want none", files, err)
	}
}

func TestSpecDraftShapeRetryThatPassesKeepsNoFailedReport(t *testing.T) {
	h := specHarness(t)
	dir := t.TempDir()
	h.runner.script = func(req Request, _ int) (Result, error) {
		report := strings.ReplaceAll(draftReport, "## Section ", "## ")
		if req.Resume != "" {
			report = draftReport
		}
		return Result{Exit: ExitOK, Session: "sess", Report: report}, nil
	}
	_, err := RunSpecDraft(context.Background(), h.deps, SpecDraftConfig{Project: h.proj, Item: "S10", Dir: dir, Lineup: []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}}})
	if err != nil {
		t.Fatal(err)
	}
	if files, err := filepath.Glob(filepath.Join(dir, "spec-draft-failed-*.md")); err != nil || len(files) != 0 {
		t.Fatalf("failed reports %v, error %v; want none", files, err)
	}
}

func TestSpecDraftDenialResumeAsksForTheSectionBlocks(t *testing.T) {
	h := specHarness(t)
	h.runner.script = func(_ Request, n int) (Result, error) {
		res := Result{Exit: ExitOK, Session: "sess", Report: draftReport}
		if n == 0 {
			res.Denials = []runrec.DeniedCommand{{Tool: "Bash", Command: "rg spec | head"}}
		}
		return res, nil
	}
	_, err := RunSpecDraft(context.Background(), h.deps, SpecDraftConfig{Project: h.proj, Item: "S10", Dir: t.TempDir(), Lineup: []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}}})
	if err != nil {
		t.Fatal(err)
	}
	reqs := h.runner.requests()
	if len(reqs) != 2 || reqs[1].Resume != "sess" {
		t.Fatalf("want two requests resuming sess: %+v", reqs)
	}
	brief := reqs[1].Brief
	for _, want := range []string{"Bash: rg spec | head", "## Section NAME", "Why, Files, Failing tests, Implement, Check", "## Owner needed"} {
		if !strings.Contains(brief, want) {
			t.Errorf("draft denial nudge lacks %q:\n%s", want, brief)
		}
	}
	for _, unwanted := range []string{"## Slice", "Ready", "Question:"} {
		if strings.Contains(brief, unwanted) {
			t.Errorf("draft denial nudge contains spec-check form %q:\n%s", unwanted, brief)
		}
	}
}

func TestSpecDraftShapeResumeAsksForEverySection(t *testing.T) {
	h := specHarness(t)
	malformed := strings.ReplaceAll(draftReport, "## Section ", "## ")
	h.runner.script = func(_ Request, n int) (Result, error) {
		report := malformed
		if n > 0 {
			report = draftReport
		}
		return Result{Exit: ExitOK, Session: "sess", Report: report}, nil
	}
	_, err := RunSpecDraft(context.Background(), h.deps, SpecDraftConfig{Project: h.proj, Item: "S10", Dir: t.TempDir(), Lineup: []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}}})
	if err != nil {
		t.Fatal(err)
	}
	reqs := h.runner.requests()
	if len(reqs) != 2 {
		t.Fatalf("want two requests, got %d", len(reqs))
	}
	_, shapeErr := ParseSpecDraft(malformed)
	for _, want := range []string{shapeErr.Error(), "every section again", "even when the earlier report was right", "ork reads only the resumed reply", "new text, or `unchanged`", "Why, Files, Failing tests, Implement, Check", "owner questions in the same reply"} {
		if !strings.Contains(reqs[1].Brief, want) {
			t.Errorf("draft shape nudge lacks %q:\n%s", want, reqs[1].Brief)
		}
	}
}

func TestSpecDraftDenialResumeThatSendsTheFullReportPasses(t *testing.T) {
	h := specHarness(t)
	dir := t.TempDir()
	h.runner.script = func(_ Request, n int) (Result, error) {
		res := Result{Exit: ExitOK, Session: "sess", Report: draftReport}
		if n == 0 {
			res.Denials = []runrec.DeniedCommand{{Tool: "Bash", Command: "rg spec | head"}}
		}
		return res, nil
	}
	got, err := RunSpecDraft(context.Background(), h.deps, SpecDraftConfig{Project: h.proj, Item: "S10", Dir: dir, Lineup: []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}}})
	if err != nil {
		t.Fatal(err)
	}
	want, err := ParseSpecDraft(draftReport)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Draft, want) {
		t.Errorf("parsed draft %+v, want %+v", got.Draft, want)
	}
	if files, err := filepath.Glob(filepath.Join(dir, "spec-draft-failed-*.md")); err != nil || len(files) != 0 {
		t.Errorf("failed reports %v, error %v; want none", files, err)
	}
}

func TestSpecDraftShapeFailureDropsFailedReportsOfAnOldDraft(t *testing.T) {
	h := specHarness(t)
	dir := t.TempDir()
	stale := filepath.Join(dir, "spec-draft-failed-3.md")
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.runner.script = func(Request, int) (Result, error) {
		return Result{Exit: ExitOK, Session: "sess", Report: strings.ReplaceAll(draftReport, "## Section ", "## ")}, nil
	}
	_, err := RunSpecDraft(context.Background(), h.deps, SpecDraftConfig{Project: h.proj, Item: "S10", Dir: dir, Lineup: []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}}})
	if err == nil {
		t.Error("want shape failure")
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale report remains: %v", err)
	}
}

func TestCheckDraftCheckFlagsAMissingPathArgument(t *testing.T) {
	tree := t.TempDir()
	sections := []SpecDraftSection{{Name: "Check", Text: "- `gofmt -l ./roles`"}}
	want := []SpecDraftProblem{{Section: "Check", Message: `Check entry "gofmt -l ./roles": path argument "./roles" does not exist in the worktree`}}
	if got := CheckDraftCheck(tree, sections, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("problems %+v, want %+v", got, want)
	}
	sections = append(sections, SpecDraftSection{Name: "Files", Text: "- `roles/x.go` (new)"})
	if got := CheckDraftCheck(tree, sections, nil); len(got) != 0 {
		t.Fatalf("Files parent problems %+v", got)
	}
}

func TestSpecDraftRefusesACheckEntryThatIsNotACommand(t *testing.T) {
	for _, tc := range []struct {
		name, check, files string
		want               []SpecDraftProblem
	}{
		{"bad entries", "- `go test ./...`\n- `q quit`\n- `TestFoo`", "unchanged", []SpecDraftProblem{
			{Section: "Check", Message: `Check entry "q quit": first word "q" is not a runnable command`},
			{Section: "Check", Message: `Check entry "TestFoo": first word "TestFoo" is not a runnable command`},
		}},
		{"acceptance", "Acceptance criteria:\n- `q quit`\nCommands:\n- `go test ./...`", "unchanged", nil},
		{"human", "- HUMAN CHECK: `q quit`", "unchanged", nil},
		{"after land", "- [AFTER LAND] `q quit`", "unchanged", nil},
		{"unchanged", "unchanged", "unchanged", nil},
		{"draft Files", "- `scripts/new-check.sh --x`", "- `scripts/new-check.sh` (new)", nil},
		{"current Files", "- `tools/current.sh`", "unchanged", nil},
		{"replaced Files", "- `tools/current.sh`", "- `scripts/new-check.sh` (new)", []SpecDraftProblem{{Section: "Check", Message: `Check entry "tools/current.sh": first word "tools/current.sh" is not a runnable command`}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"id":"S10","slice":{"id":"S10","sections":[{"heading":"Files","body":"- ` + "`tools/current.sh`" + `"},{"heading":"Check","body":"- ` + "`q quit`" + `"}]}}`
			h := specHarnessRoutes(t, []clitest.Route{{Tool: "slicer", Args: []string{"show", "S10"}, Prefix: true, Stdout: raw}}, nil, "", "", "")
			report := strings.Replace(draftReport, "- `internal/a/a.go`", tc.files, 1)
			report = strings.Replace(report, "## Section Check\nunchanged", "## Section Check\n"+tc.check, 1)
			h.runner.script = func(Request, int) (Result, error) { return Result{Exit: ExitOK, Report: report}, nil }
			got, err := RunSpecDraft(context.Background(), h.deps, SpecDraftConfig{Project: h.proj, Item: "S10", Dir: t.TempDir(), Lineup: []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}}})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Problems, tc.want) {
				t.Fatalf("problems %+v, want %+v", got.Problems, tc.want)
			}
		})
	}
}

func TestSpecDraftRefusesAFilesPathThatDoesNotExist(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "a", "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := strings.Join([]string{
		"- `internal/a/a.go`",
		"- `internal/a/a.go:3-9` (an anchor is not part of the path)",
		"- `internal/a/gone.go`",
		"- `internal/a/fresh.go` (new)",
		"- not a path line",
		"- `../outside.go`",
	}, "\n")
	secs := []SpecDraftSection{
		{Name: "Why", Text: "- `internal/a/missing.go` is a list line, but the Why section is not checked."},
		{Name: "Files", Text: files},
		{Name: "Check", Unchanged: true},
	}
	got := CheckDraftFiles(root, secs)
	want := []SpecDraftProblem{
		{Section: "Files", Message: "`internal/a/gone.go` is not in the checkout; mark a new file `(new)`"},
		{Section: "Files", Message: "`../outside.go` is outside the checkout"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems %+v, want %+v", got, want)
	}
	// An unchanged Files section is the slice's own text, so it is not checked.
	secs[1] = SpecDraftSection{Name: "Files", Unchanged: true, Text: "- `internal/a/gone.go`"}
	if got := CheckDraftFiles(root, secs); len(got) != 0 {
		t.Fatalf("unchanged Files: %+v", got)
	}
}
