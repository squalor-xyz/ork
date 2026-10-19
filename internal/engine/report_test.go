// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ownerNone = "\n## Owner needed\n\nnone\n"

func TestReportFileTakesPrecedenceOverVendorResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(path, []byte("file report"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, _ := (&Engine{}).readReport(path, "report.md", "vendor result")
	if text != "file report" {
		t.Fatalf("report text %q, want file report", text)
	}
}

func TestVendorResultIsSavedWhenReportFileIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	(&Engine{}).readReport(path, "report.md", "vendor result")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "vendor result" {
		t.Fatalf("saved report %q, %v", data, err)
	}
}

func shapeProblems(t *testing.T, err error) []string {
	t.Helper()
	var se *ShapeError
	if !errors.As(err, &se) {
		t.Fatalf("want a *ShapeError, got %v", err)
	}
	return se.Problems
}

func mustMention(t *testing.T, problems []string, want string) {
	t.Helper()
	for _, p := range problems {
		if strings.Contains(p, want) {
			return
		}
	}
	t.Errorf("no problem mentions %q: %q", want, problems)
}

func TestCheckReportAcceptsAGoodGateReport(t *testing.T) {
	for verdict, want := range map[string]string{"VERDICT: PASS": "PASS", "VERDICT: FAIL": "FAIL"} {
		r, err := CheckReport(RoleGate, false, verdict+"\n\n- tests green\nTests: 87 passed, 2 failed\n"+ownerNone)
		if err != nil {
			t.Fatalf("%s: %v", verdict, err)
		}
		if r.Verdict != want || !r.HasTests || r.Passed != 87 || r.Failed != 2 || r.Question != "" {
			t.Errorf("%s: %+v", verdict, r)
		}
	}
}

func TestCheckReportGateFirstLineMustBeExactlyTheVerdict(t *testing.T) {
	tail := "\nTests: 1 passed, 0 failed\n" + ownerNone
	for name, text := range map[string]string{
		"lowercase":           "verdict: pass" + tail,
		"trailing period":     "VERDICT: PASS." + tail,
		"leading blank line":  "\nVERDICT: PASS" + tail,
		"leading space":       " VERDICT: PASS" + tail,
		"text after":          "VERDICT: PASS (with nits)" + tail,
		"role line first":     "Role: gate\nVERDICT: PASS" + tail,
		"unknown verdict":     "VERDICT: MAYBE" + tail,
		"no verdict":          "All good." + tail,
		"verdict on 2nd line": "Report\nVERDICT: PASS" + tail,
		"empty":               "",
	} {
		_, err := CheckReport(RoleGate, false, text)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		mustMention(t, shapeProblems(t, err), "VERDICT")
	}
	if r, err := CheckReport(RoleGate, false, "VERDICT: PASS\r\n"+tail); err != nil || r.Verdict != "PASS" {
		t.Errorf("a CRLF first line is the same line: %+v, %v", r, err)
	}
}

func TestCheckReportOnlyTheGateNeedsAVerdict(t *testing.T) {
	r, err := CheckReport(RoleImplementer, false, "Diff stat.\nTests: 3 passed, 0 failed\n"+ownerNone)
	if err != nil || r.Verdict != "" {
		t.Fatalf("%+v, %v", r, err)
	}
}

func TestCheckReportTestsLine(t *testing.T) {
	good := "VERDICT: PASS\n"
	for _, role := range []string{RoleImplementer, RoleGate} {
		text := "Done.\n" + ownerNone
		if role == RoleGate {
			text = good + ownerNone
		}
		_, err := CheckReport(role, false, text)
		if err == nil {
			t.Errorf("%s without a Tests line was accepted", role)
			continue
		}
		mustMention(t, shapeProblems(t, err), "Tests:")
	}
	for name, line := range map[string]string{
		"no failed count": "Tests: 10 passed",
		"wrong words":     "Tests: ten passed, 0 failed",
		"indented":        "  Tests: 1 passed, 0 failed",
		"inline":          "see Tests: 1 passed, 0 failed",
		"trailing text":   "Tests: 1 passed, 0 failed (race)",
	} {
		if _, err := CheckReport(RoleImplementer, false, line+"\n"+ownerNone); err == nil {
			t.Errorf("%s: %q was accepted", name, line)
		}
	}
	// The reviewer runs no suite, and the implementer's finish step is not a
	// report of a run of the suite.
	if _, err := CheckReport(RoleReviewer, false, "(A) nothing\n"+ownerNone); err != nil {
		t.Errorf("reviewer: %v", err)
	}
	if _, err := CheckReport(RoleImplementer, true, "Closed.\n"+ownerNone); err != nil {
		t.Errorf("finish step: %v", err)
	}
}

func TestCheckReportNeedsAnOwnerNeededSection(t *testing.T) {
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate} {
		text := "x\nTests: 1 passed, 0 failed\n"
		if role == RoleGate {
			text = "VERDICT: PASS\n" + text
		}
		_, err := CheckReport(role, false, text)
		if err == nil {
			t.Errorf("%s without ## Owner needed was accepted", role)
			continue
		}
		mustMention(t, shapeProblems(t, err), "## Owner needed")
	}
	for name, heading := range map[string]string{
		"wrong level": "### Owner needed",
		"wrong case":  "## Owner Needed",
		"indented":    " ## Owner needed",
		"prefixed":    "x ## Owner needed",
	} {
		if _, err := CheckReport(RoleReviewer, false, "x\n"+heading+"\nsomething\n"); err == nil {
			t.Errorf("%s: %q was accepted", name, heading)
		}
	}
}

func TestCheckReportListsEveryProblem(t *testing.T) {
	_, err := CheckReport(RoleGate, false, "nothing useful")
	problems := shapeProblems(t, err)
	mustMention(t, problems, "VERDICT")
	mustMention(t, problems, "Tests:")
	mustMention(t, problems, "## Owner needed")
	if len(problems) != 3 {
		t.Errorf("problems: %q", problems)
	}
	if msg := err.Error(); !strings.Contains(msg, "VERDICT") || !strings.Contains(msg, "Owner needed") {
		t.Errorf("the error text must show the owner every problem: %q", msg)
	}
}

func TestCheckReportOwnerNeeded(t *testing.T) {
	for _, none := range []string{"none", "None", "none.", "  NONE  ", "", "\n\n", "None.\n"} {
		r, err := CheckReport(RoleReviewer, false, "x\n## Owner needed\n\n"+none+"\n")
		if err != nil || r.Question != "" {
			t.Errorf("%q: %+v, %v", none, r, err)
		}
	}
	r, err := CheckReport(RoleReviewer, false, "x\n## Owner needed\n\nShould the cap be 4 or 8?\nOptions: 4, 8.\nI recommend 4.\n\n## Later\nignored\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Should the cap be 4 or 8?\nOptions: 4, 8.\nI recommend 4."; r.Question != want {
		t.Errorf("question %q, want %q", r.Question, want)
	}
	// "none" with more after it is a question.
	if r, _ := CheckReport(RoleReviewer, false, "## Owner needed\nnone of the options fit\n"); r.Question == "" {
		t.Errorf("a sentence that starts with none is a question")
	}
}

// ---- Findings ----

func review(a, b, c, d string) string {
	return "Review of S04.\n\n## (A) Correctness\n" + a + "\n\n## (B) Test gaps\n" + b + "\n\n## (C) Direction\n" + c + "\n\n## (D) Doc accuracy\n" + d + "\n\n## Owner needed\n\nnone\n"
}

func TestFindingsNothingMeansNoFindings(t *testing.T) {
	for _, nothing := range []string{"nothing", "Nothing.", "none", "  nothing  "} {
		got, err := Findings(review(nothing, nothing, nothing, nothing))
		if err != nil || got != "" {
			t.Errorf("%q: %q, %v", nothing, got, err)
		}
	}
}

func TestFindingsAreSectionsABAndD(t *testing.T) {
	cases := map[string]string{
		"A": review("1. run.go:10 crashes on nil.", "nothing", "nothing", "nothing"),
		"B": review("nothing", "TestGate does not reach the stop.", "nothing", "nothing"),
		"D": review("nothing", "nothing", "nothing", "docs/state.md names the wrong field."),
	}
	for sec, text := range cases {
		got, err := Findings(text)
		if err != nil || got == "" {
			t.Errorf("(%s): %q, %v", sec, got, err)
		}
	}
	want := map[string]string{"A": "run.go:10", "B": "TestGate", "D": "docs/state.md"}
	for sec, text := range cases {
		got, _ := Findings(text)
		if !strings.Contains(got, want[sec]) {
			t.Errorf("(%s): findings %q lack %q", sec, got, want[sec])
		}
	}
	// (C) Direction holds notes for later, which do not start a fix round.
	if got, err := Findings(review("nothing", "nothing", "Note for later: S06 must undo this.", "nothing")); err != nil || got != "" {
		t.Errorf("(C) only: %q, %v", got, err)
	}
}

func TestFindingsDoNotIncludeTheOwnerNeededSection(t *testing.T) {
	text := review("nothing", "nothing", "nothing", "docs/state.md is wrong.") + "\nShould we?\n"
	got, err := Findings(text)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Should we?") || strings.Contains(got, "Owner needed") {
		t.Errorf("findings include the Owner needed section: %q", got)
	}
}

func TestFindingsHeadingVariants(t *testing.T) {
	variants := map[string]string{
		"bold":        "**(A) Correctness**\nrun.go:1 bug\n\n**(B) Test gaps**\nnothing\n\n**(D) Doc accuracy**\nnothing\n",
		"bulleted":    "- (A) Correctness, most severe first.\n  run.go:1 bug\n- (B) Test gaps. nothing\n- (D) Doc accuracy: nothing\n",
		"inline none": "(A) Correctness: run.go:1 bug\n(B) Test gaps: nothing\n(D) Doc accuracy: nothing\n",
	}
	for name, text := range variants {
		got, err := Findings(text + "\n## Owner needed\nnone\n")
		if err != nil || !strings.Contains(got, "run.go:1") {
			t.Errorf("%s: %q, %v", name, got, err)
		}
	}
	clean := map[string]string{
		"bold":        "**(A) Correctness**\nnothing\n\n**(B) Test gaps**\nnothing\n\n**(D) Doc accuracy**\nnothing\n",
		"inline none": "(A) Correctness: nothing\n(B) Test gaps: nothing\n(D) Doc accuracy: nothing.\n",
		"bulleted":    "- (A) Correctness, most severe first: nothing\n- (B) Test gaps: nothing\n- (D) Doc accuracy: nothing\n",
	}
	for name, text := range clean {
		got, err := Findings(text + "\n## Owner needed\nnone\n")
		if err != nil || got != "" {
			t.Errorf("%s clean: %q, %v", name, got, err)
		}
	}
}

func TestFindingsErrorsOnAReportWithoutTheSections(t *testing.T) {
	if _, err := Findings("## (A) Correctness\nnothing\n## (B) Test gaps\nnothing\n## (D) Doc accuracy\nnothing\n"); err != nil {
		t.Fatalf("a report with the sections is not an error: %v", err)
	}
	for name, text := range map[string]string{
		"no sections": "Looks fine to me.\n## Owner needed\nnone\n",
		"no (B)":      "## (A) Correctness\nnothing\n## (D) Doc accuracy\nnothing\n",
		"blank (A)":   "## (A) Correctness\n\n## (B) Test gaps\nnothing\n## (D) Doc accuracy\nnothing\n",
	} {
		if got, err := Findings(text); err == nil {
			t.Errorf("%s: accepted, findings %q", name, got)
		}
	}
}
