// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"strings"
	"testing"
)

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
	if first, _, _ := strings.Cut(got, "\n"); first != "Role: implementer. Contract: relay v7" {
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
