// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
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
		Allow:        DefaultImplementerAllowlist(h.proj.CheckCommand),
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

func TestUIBriefSkillsOnlyForClaude(t *testing.T) {
	for _, tc := range []struct {
		name, role, vendor string
		skills             []string
		want               bool
	}{
		{"claude", RoleUI, "claude", []string{"tui-design", "frontend-design"}, true},
		{"unset", RoleUI, "claude", nil, false},
		{"empty", RoleUI, "claude", []string{}, false},
		{"codex", RoleUI, "codex", []string{"tui-design", "frontend-design"}, false},
		{"case-sensitive", RoleUI, "Claude", []string{"tui-design"}, false},
		{"implementer", RoleImplementer, "claude", []string{"tui-design"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := baseBrief()
			in.Role = tc.role
			in.Vendor = tc.vendor
			in.UIPassSkills = tc.skills
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			line := "## Skills\n\nLoad these skills for the UI pass: tui-design, frontend-design. If one is not available, say so in your report and go on.\n\n## UI design doc"
			if strings.Contains(got, line) != tc.want {
				t.Fatalf("skills line present=%v, want %v", strings.Contains(got, line), tc.want)
			}
			if !tc.want && (strings.Contains(got, "## Skills") || strings.Contains(got, "Load these skills") || strings.Contains(got, "tui-design") || strings.Contains(got, "frontend-design")) {
				t.Fatal("skills leaked to brief")
			}
		})
	}
}

func TestBriefFencesCheckCommandWithBackticks(t *testing.T) {
	for _, command := range []string{"echo `pwd`\ngo test ./...", "echo `pwd`", "go test ./...\nslicer check"} {
		assertCheckFence(t, command, "```")
	}
}

func TestBriefKeepsAPlainCheckCommandInline(t *testing.T) {
	in := baseBrief()
	in.CheckCommand = "go test -race ./..."
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "ork runs `go test -race ./...` and `slicer check` after the round, outside your sandbox.") {
		t.Fatal("plain check instructions changed")
	}
}

func TestBriefFenceGrowsWithTheCommand(t *testing.T) {
	assertCheckFence(t, "echo ```code```", "````")
}

func assertCheckFence(t *testing.T, command, fence string) {
	t.Helper()
	in := baseBrief()
	in.CheckCommand = command
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	want := "ork runs this command, then `slicer check`, after the round, outside your sandbox:\n\n" + fence + "\n" + command + "\n" + fence + "\n"
	if !strings.Contains(got, want) {
		t.Fatalf("brief lacks fenced instructions %q", want)
	}
	if strings.Contains(got, "ork runs `"+command+"`") {
		t.Fatal("command still wrapped inline")
	}
}

func yourFilesSection(t *testing.T, brief string) string {
	t.Helper()
	_, rest, ok := strings.Cut(brief, "## Your files\n\n")
	if !ok {
		t.Fatalf("no Your files section:\n%s", brief)
	}
	section, _, _ := strings.Cut(rest, "\n## ")
	return section
}

func TestReadOnlyBriefSaysTheReportIsTheFinalMessage(t *testing.T) {
	const pathLine = "Your report path: /state/runs/r1/implementer-1.md (ork saves your final message there; you cannot write it)\n"
	const finalLine = "Give your whole report as your final message, as plain text. Do not wrap it or any part of it in a code fence.\n"
	for _, role := range []string{RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck} {
		t.Run(role, func(t *testing.T) {
			in := baseBrief()
			in.Role = role
			in.ReadOnly = true
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			section := yourFilesSection(t, got)
			if !strings.Contains(section, pathLine) || !strings.Contains(section, finalLine) {
				t.Errorf("Your files section:\n%s", section)
			}
		})
	}
	got, err := BuildBrief(baseBrief())
	if err != nil {
		t.Fatal(err)
	}
	if section := yourFilesSection(t, got); strings.Contains(section, "you cannot write it") || strings.Contains(section, "code fence") || !strings.Contains(section, "Your report path: /state/runs/r1/implementer-1.md\n") {
		t.Errorf("an implementer brief changed:\n%s", section)
	}
}

func TestResumedBriefAsksToReReadTheTree(t *testing.T) {
	const body = "ork resumed your earlier session. The worktree may have changed since your last report. Before you judge or change anything, run git status and git diff again and read again every file you cite. Do not repeat an earlier finding, verdict or answer without checking it against the tree as it is now."
	in := baseBrief()
	in.Resumed = true
	in.Tree = " M a.go\n?? b.go\n"
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	want := "## Your files\n\nYour temp directory: /state/runs/r1/tmp/implementer-1\nYour report path: /state/runs/r1/implementer-1.md\n\n" +
		"## Resumed session\n\n" + body + "\n\ngit status --porcelain when this round started:\n\n```\n M a.go\n?? b.go\n```\n"
	if !strings.Contains(got, want) {
		t.Errorf("resumed section with a tree:\n%s", got)
	}

	in.Tree = ""
	got, err = BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	want = "## Resumed session\n\n" + body + "\n\nThe tree had no uncommitted changes when this round started.\n"
	if !strings.Contains(got, want) || strings.Contains(got, "git status --porcelain when this round started") {
		t.Errorf("resumed section with a clean tree:\n%s", got)
	}

	// A listing that holds a backtick run gets a longer fence.
	in.Tree = "?? a```b.go\n"
	got, err = BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "\n````\n?? a```b.go\n````\n") {
		t.Errorf("fence is not longer than the listing's backticks:\n%s", got)
	}

	// The section sits after Your files and before the round's own sections.
	in.Sections = []BriefSection{{Title: "Round", Body: "fix round 1"}}
	got, err = BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if files, resumed, round := strings.Index(got, "## Your files"), strings.Index(got, "## Resumed session"), strings.Index(got, "## Round"); files < 0 || files > resumed || resumed > round {
		t.Errorf("section order:\n%s", got)
	}
}

func TestFreshBriefHasNoResumedSection(t *testing.T) {
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck} {
		in := baseBrief()
		in.Role = role
		in.Tree = " M a.go\n" // a tree alone does not make a resumed brief
		got, err := BuildBrief(in)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "## Resumed session") || strings.Contains(got, "git status --porcelain when") {
			t.Errorf("%s: a fresh brief has a resumed section", role)
		}
	}
}

// Inspect only the generated checklist, never matching text elsewhere in the prompt.
func checklist(t *testing.T, in BriefInput) string {
	t.Helper()
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(got, "\n## Done when\n\n")
	if !ok {
		t.Fatal("missing Done when section")
	}
	section, _, _ := strings.Cut(rest, "\n\n## ")
	return section
}

func requireChecklistText(t *testing.T, in BriefInput, wants ...string) {
	t.Helper()
	got := checklist(t, in)
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("checklist lacks %q", want)
		}
	}
}

func TestEveryRoleBriefHasADoneWhenSection(t *testing.T) {
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck} {
		t.Run(role, func(t *testing.T) {
			in := baseBrief()
			in.Role = role
			got := checklist(t, in)
			if strings.Count(got, "- [ ] ") < 3 {
				t.Fatal("fewer than three checklist items")
			}
			if len(strings.Split(got, "\n")) >= 40 {
				t.Fatal("checklist reaches 40 lines")
			}
		})
	}
}

func TestImplementerDoneWhenNamesTheExactReportLines(t *testing.T) {
	requireChecklistText(t, baseBrief(), "Tests: N passed, M failed", "## Owner needed", "nothing may follow the counts", "skipped tests go under item 6", "report parts in the prompt", "docs")
}

func TestImplementerDoneWhenListsTheSlicesFailingTestsAndChecks(t *testing.T) {
	in := baseBrief()
	in.SliceJSON = `{"slice":{"sections":[{"heading":"Failing tests","body":"- TestA\n- TestB"},{"heading":"Check","body":"- go test ./alpha\n- go test ./beta\n- [HUMAN CHECK] look"}]}}`
	requireChecklistText(t, in, "TestA: red before the code, green after", "TestB: red before the code, green after", "go test ./alpha", "go test ./beta")
	if strings.Contains(checklist(t, in), "HUMAN CHECK") {
		t.Fatal("human check required in sandbox")
	}
	requireChecklistText(t, baseBrief(), "Failing tests: the slice lists none", "Check: the slice lists none")
}

func TestImplementerDoneWhenShowsTheAllowedCommands(t *testing.T) {
	in := baseBrief()
	in.Allow = DefaultImplementerAllowlist("python3 -m unittest")
	requireChecklistText(t, in, append(in.Allow, "any other command is denied", "no pipes, no cd, no env prefix unless an entry allows it")...)
	in.Allow = []string{"Bash(go  test*)"}
	requireChecklistText(t, in, "Bash(go  test*)")
	in.ReadOnly = true
	requireChecklistText(t, in, "may only read")
}

func TestDoneWhenSaysWhatToWriteWhenACheckCannotRun(t *testing.T) {
	requireChecklistText(t, baseBrief(), "denied or impossible check", "item 6", "verbatim command and denial text", "counts of tests actually run", "never a made-up zero", "ork re-runs the checks outside the sandbox")
}

func TestFixRoundDoneWhenListsEachFinding(t *testing.T) {
	for _, title := range []string{"Review findings", "Gate FAIL", "Architect findings"} {
		t.Run(title, func(t *testing.T) {
			in := baseBrief()
			in.Sections = []BriefSection{{Title: title, Body: "- [fix] alpha.go:1 loses error\n  Fix: return it\n- [later] polish\n- [fix] beta.go:2 races"}}
			requireChecklistText(t, in, "alpha.go:1 loses error: proving test or a stated reason it needs none", "beta.go:2 races: proving test or a stated reason it needs none")
			if strings.Contains(checklist(t, in), "polish") {
				t.Fatal("later finding included")
			}
		})
	}
}

func TestDoneWhenSeparatesBulletsAcrossProse(t *testing.T) {
	for _, tc := range []struct{ heading, first, second string }{
		{"Failing tests", "- [ ] First item continued: red before the code, green after", "- [ ] Second item: red before the code, green after"},
		{"Check", "- [ ] Run or report blocked: First item continued\n", "- [ ] Run or report blocked: Second item"},
	} {
		t.Run(tc.heading, func(t *testing.T) {
			in := baseBrief()
			raw, err := json.Marshal(map[string]any{"slice": map[string]any{"sections": []map[string]string{{"heading": tc.heading, "body": "- First item\n  continued\nIn other_test.go:\n  Detached prose\n- Second item"}}}})
			if err != nil {
				t.Fatal(err)
			}
			in.SliceJSON = string(raw)
			got := checklist(t, in)
			if !strings.Contains(got, tc.first) {
				t.Errorf("first bullet does not retain only its indented continuation: %s", got)
			}
			if !strings.Contains(got, tc.second) {
				t.Errorf("second bullet missing: %s", got)
			}
			for _, unwanted := range []string{"In other_test.go:", "Detached prose"} {
				if strings.Contains(got, unwanted) {
					t.Errorf("prose leaked into checklist: %q", unwanted)
				}
			}
		})
	}
}

func TestResumedBriefRepeatsDoneWhen(t *testing.T) {
	in := baseBrief()
	in.Resumed = true
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(got, "\n## Done when\n") <= strings.Index(got, "\n## Resumed session\n") {
		t.Fatal("Done when missing after Resumed session")
	}
	requireChecklistText(t, in, "First re-run git status and git diff")
}

func TestFinishStepDoneWhen(t *testing.T) {
	in := baseBrief()
	in.Finish = true
	in.Sections = []BriefSection{{Title: "Finish step", Body: "1. `slicer note S04 --text \"outcome\" --render`\n2. `slicer handoff S04 --render --check`"}}
	got := checklist(t, in)
	requireChecklistText(t, in, "slicer note S04", "slicer handoff S04", "in order", "denied", "## Owner needed", "orchestrator", "engine.denials", "not retried")
	if strings.Index(got, "slicer note S04") > strings.Index(got, "slicer handoff S04") {
		t.Fatal("finish commands out of order")
	}
	if strings.Contains(got, "Tests:") {
		t.Fatal("finish checklist requires Tests line")
	}
}

func TestReviewerAndGateDoneWhen(t *testing.T) {
	for _, role := range []string{RoleReviewer, RoleGate} {
		t.Run(role, func(t *testing.T) {
			in := baseBrief()
			in.Role = role
			in.ReadOnly = true
			requireChecklistText(t, in, "file:line", "reproduction or a test", "Deviation", "never a [fix]", "Importance", "Urgency", "Effort", "## Owner needed", "may only read")
			if role == RoleReviewer {
				requireChecklistText(t, in, "untagged line under section (C)")
			} else {
				requireChecklistText(t, in, "first line exactly VERDICT: PASS or VERDICT: FAIL", "one evidence line per criterion", "Tests: N passed, M failed", "Deviation for the orchestrator", "no [fix]", "no blocker", "tests green", "docs match")
			}
		})
	}
}
