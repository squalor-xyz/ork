// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestFirstRowBriefIsUnchanged(t *testing.T) {
	h := fallbackHarness(t, RoleImplementer)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := h.runner.requests()[0]
	want, err := BuildBrief(BriefInput{
		Role: req.Role, SliceID: item, Worktree: req.Worktree,
		TempDir: req.TempDir, ReportPath: req.ReportPath,
		SliceJSON: sliceJSON(), EnvLines: h.proj.EnvironmentLines,
		CheckCommand: h.proj.CheckCommand,
		Sections:     []BriefSection{{Title: "Round", Body: "This is the first round. The slice is not started: run `slicer start " + item + " --render --strict` first."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Brief != want || strings.Contains(req.Brief, "## Earlier lineup rows") {
		t.Fatal("first row brief changed")
	}
}

func baseBrief() BriefInput {
	return BriefInput{
		Role: RoleImplementer, SliceID: "S04", Worktree: "/p/.worktrees/S04",
		TempDir: "/state/runs/r1/tmp/implementer-1", ReportPath: "/state/runs/r1/implementer-1.md",
		SliceJSON: `{"id":"S04","title":"Engine"}`,
		EnvLines:  []string{"Shell: zsh on darwin. `timeout` is not installed.", "Go 1.27.2, no cd."},
	}
}

func TestBuildBriefFillsThePromptPlaceholders(t *testing.T) {
	in := baseBrief()
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "{{") {
		i := strings.Index(got, "{{")
		t.Fatalf("a placeholder is left: %q", got[i:min(len(got), i+30)])
	}
	if first, _, _ := strings.Cut(got, "\n"); first != "Role: implementer. Contract: relay v11" {
		t.Errorf("first line %q", first)
	}
	for _, want := range []string{
		"you implement exactly that slice, test-first, in the worktree /p/.worktrees/S04",
		"Put scratch files in /state/runs/r1/tmp/implementer-1",
		"write your final report to /state/runs/r1/implementer-1.md",
		"`slicer show S04`",
	} {
		if !strings.Contains(strings.Join(strings.Fields(got), " "), want) {
			t.Errorf("brief lacks %q", want)
		}
	}
}

func TestBuildBriefCarriesTheBriefParts(t *testing.T) {
	in := baseBrief()
	in.Sections = []BriefSection{{Title: "Checks failed", Body: "FAIL TestX: wrong"}, {Title: "Round", Body: "fix round 1"}}
	in.Earlier = []EarlierReport{{Name: "implementer-1", Text: "Tests: 3 passed, 0 failed"}}
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	order := []string{
		"Role: implementer.",
		"# Brief",
		`{"id":"S04","title":"Engine"}`,
		"Do not open, search, parse, or edit tracking files",
		"stay inside the workspace",
		"Shell: zsh on darwin. `timeout` is not installed.\n",
		"Go 1.27.2, no cd.",
		"/state/runs/r1/tmp/implementer-1",
		"/state/runs/r1/implementer-1.md",
		"## Checks failed\n\nFAIL TestX: wrong",
		"## Round\n\nfix round 1",
		"## Earlier reports",
		"### implementer-1\n\nTests: 3 passed, 0 failed",
	}
	pos := 0
	for _, want := range order {
		i := strings.Index(got[pos:], want)
		if i < 0 {
			t.Fatalf("%q is missing or out of order after offset %d", want, pos)
		}
		pos += i + len(want)
	}
}

func TestBuildBriefWithoutEarlierReportsOrSections(t *testing.T) {
	got, err := BuildBrief(baseBrief())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Earlier reports") {
		t.Error("an Earlier reports heading with nothing under it")
	}
}

func TestBuildBriefEveryRoleAndItsErrors(t *testing.T) {
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate, "architect"} {
		in := baseBrief()
		in.Role = role
		got, err := BuildBrief(in)
		if err != nil || !strings.HasPrefix(got, "Role: "+role+". Contract: relay v") {
			t.Errorf("%s: %v", role, err)
		}
	}
	in := baseBrief()
	in.Role = "janitor"
	if _, err := BuildBrief(in); err == nil {
		t.Error("unknown role accepted")
	}
	for field, edit := range map[string]func(*BriefInput){
		"SliceID":    func(b *BriefInput) { b.SliceID = "" },
		"Worktree":   func(b *BriefInput) { b.Worktree = "" },
		"TempDir":    func(b *BriefInput) { b.TempDir = "" },
		"ReportPath": func(b *BriefInput) { b.ReportPath = "" },
		"SliceJSON":  func(b *BriefInput) { b.SliceJSON = "" },
	} {
		in := baseBrief()
		edit(&in)
		if _, err := BuildBrief(in); err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("empty %s: %v", field, err)
		}
	}
}

func TestBriefNamesCheckCommand(t *testing.T) {
	checkBriefInstructionsOnlyForImplementer(t)
	for _, tc := range []struct{ name, command, want string }{
		{"configured", "go test -race ./...", "ork runs `go test -race ./...` and `slicer check` after the round, outside your sandbox."},
		{"empty", "", "ork runs `slicer check` after the round, outside your sandbox."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) { o.project = func(p *registry.Project) { p.CheckCommand = tc.command } })
			e := h.start()
			for _, req := range h.runner.requests() {
				_, brief, _ := strings.Cut(req.Brief, "\n# Brief\n")
				if req.Role == RoleImplementer {
					if !strings.Contains(brief, "## Checks ork runs") || !strings.Contains(brief, tc.want) {
						t.Errorf("implementer brief lacks %q", tc.want)
					}
				} else if strings.Contains(brief, "## Checks ork runs") {
					t.Errorf("%s brief includes implementer check instructions", req.Role)
				}
			}
			if err := e.Approve(context.Background()); err != nil {
				t.Fatal(err)
			}
			reqs := h.runner.requests()
			finish := reqs[len(reqs)-1]
			if finish.Role != RoleImplementer || !strings.Contains(finish.Brief, "## Finish step") {
				t.Fatal("last request must be the implementer finish step")
			}
			if strings.Contains(finish.Brief, "## Checks ork runs") {
				t.Error("finish brief includes checks that do not run after the finish step")
			}
		})
	}
}

func checkBriefInstructionsOnlyForImplementer(t *testing.T) {
	for _, role := range []string{RoleReviewer, RoleGate, "architect"} {
		t.Run(role, func(t *testing.T) {
			in := baseBrief()
			in.Role = role
			in.CheckCommand = "go test -race ./..."
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(got, "## Checks ork runs") {
				t.Error("non-implementer brief includes check instructions")
			}
		})
	}
}

// The UI pass brief names the project's design doc from the registry field
// ui_doc, or tells the pass to find the project's own UI docs (S136).
func TestUIBriefNamesTheProjectDesignDoc(t *testing.T) {
	in := baseBrief()
	in.Role = RoleUI
	in.UIDoc = "docs/ui.md"
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "## UI design doc\n\nThe project's UI design doc is `docs/ui.md`.") {
		t.Errorf("brief does not name the doc:\n%s", got)
	}
	in.UIDoc = ""
	got, err = BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "## UI design doc\n\nThe registry names no UI design doc for this project. Find the project's own UI docs") || strings.Contains(got, "docs/ui.md") {
		t.Errorf("brief does not tell the pass to look:\n%s", got)
	}
	// Other roles get no such section.
	in = baseBrief()
	in.UIDoc = "docs/ui.md"
	got, err = BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "UI design doc") {
		t.Errorf("implementer brief names the UI doc:\n%s", got)
	}
}
