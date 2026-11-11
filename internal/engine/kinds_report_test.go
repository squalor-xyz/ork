// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/roles"
)

const (
	fixFinding   = "- [fix] engine.go:42 drops the error."
	ownerFinding = "- [owner] Should the cache be shared across projects?\n" +
		"  Options:\n" +
		"  1. shared: one cache for all\n" +
		"  2. per-project: one cache each\n" +
		"  Recommend: 2 - isolation is simpler"
	blockerFinding = "- [owner blocker] Drop the old wire field now?\n" +
		"  Options:\n" +
		"  1. drop: remove it\n" +
		"  2. keep: keep one more release\n" +
		"  Recommend: 1 - nothing reads it\n" +
		"  Decision key: wire.old-field"
	laterFinding = "- [later] Add a --json flag to ork status"
)

func TestTriageWithoutKindsIsAllFix(t *testing.T) {
	text := review("1. run.go:10 crashes on nil.", "TestGate does not reach the stop.", "Note for later: S06 must undo this.", "nothing")
	got, err := Triage(text)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := Findings(text)
	if got.Tagged || got.Fix != want || !strings.Contains(got.Fix, "run.go:10") || !strings.Contains(got.Fix, "TestGate") {
		t.Errorf("untagged report: tagged %v, fix %q, want %q", got.Tagged, got.Fix, want)
	}
	if len(got.Owner) != 0 || len(got.Later) != 0 {
		t.Errorf("untagged report has owner %v or later %v findings", got.Owner, got.Later)
	}
}

func TestTriageSortsFindingsByKind(t *testing.T) {
	text := review(fixFinding+"\n"+ownerFinding+"\n"+laterFinding, "- [fix] TestGate does not reach the stop.", "- [later] Move the cache to its own package", "nothing")
	got, err := Triage(text)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Tagged {
		t.Error("a report with tags is Tagged")
	}
	for _, want := range []string{"engine.go:42 drops the error.", "TestGate does not reach the stop."} {
		if !strings.Contains(got.Fix, want) {
			t.Errorf("fix findings lack %q:\n%s", want, got.Fix)
		}
	}
	for _, not := range []string{"Should the cache", "--json flag", "own package", "[later]", "isolation is simpler"} {
		if strings.Contains(got.Fix, not) {
			t.Errorf("fix findings include %q:\n%s", not, got.Fix)
		}
	}
	if len(got.Owner) != 1 || got.Owner[0].Blocker || got.Owner[0].Title != "Should the cache be shared across projects?" {
		t.Fatalf("owner findings: %+v", got.Owner)
	}
	ask := got.Owner[0].Ask
	if ask == nil || len(ask.Options) != 2 || ask.Options[1].Label != "per-project" || ask.Recommend != 2 || ask.Reason != "isolation is simpler" {
		t.Errorf("owner question: %+v", ask)
	}
	if len(got.Later) != 2 || got.Later[0].Title != "Add a --json flag to ork status" || got.Later[1].Title != "Move the cache to its own package" {
		t.Errorf("later findings: %+v", got.Later)
	}
}

func TestTriageOwnerBlockerAndKey(t *testing.T) {
	got, err := Triage(review(blockerFinding, "nothing", "nothing", "nothing"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Fix != "" || len(got.Owner) != 1 || !got.Owner[0].Blocker || got.Owner[0].Key != "wire.old-field" {
		t.Fatalf("blocker finding: fix %q, owner %+v", got.Fix, got.Owner)
	}
	if got.Owner[0].Ask == nil || got.Owner[0].Ask.Title != "Drop the old wire field now?" {
		t.Errorf("blocker question: %+v", got.Owner[0].Ask)
	}
}

func TestTriageOnlyNonFixFindingsLeaveNoFix(t *testing.T) {
	got, err := Triage(review(laterFinding, "nothing", ownerFinding, "nothing"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Fix != "" || len(got.Later) != 1 || len(got.Owner) != 1 {
		t.Errorf("fix %q, later %v, owner %v", got.Fix, got.Later, got.Owner)
	}
	// Findings, which feeds the fix round, sees none.
	if f, err := Findings(review(laterFinding, "nothing", "nothing", "nothing")); err != nil || f != "" {
		t.Errorf("Findings of a later-only report: %q, %v", f, err)
	}
}

func TestTriageAnUntaggedFindingBesideTagsIsFix(t *testing.T) {
	got, err := Triage(review("1. run.go:10 crashes on nil.\n"+laterFinding, "nothing", "nothing", "nothing"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Fix, "run.go:10") || strings.Contains(got.Fix, "--json flag") || len(got.Later) != 1 {
		t.Errorf("fix %q, later %v", got.Fix, got.Later)
	}
}

func TestTriageIndentedContinuationStaysInItsFinding(t *testing.T) {
	later := "- [later] Split the cache\n\n  Two projects share one file today.\n"
	got, err := Triage(review(later+"engine.go:9 real bug", "nothing", "nothing", "nothing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Later) != 1 || !strings.Contains(got.Later[0].Text, "Two projects share one file") {
		t.Errorf("later: %+v", got.Later)
	}
	if strings.Contains(got.Fix, "Two projects") || !strings.Contains(got.Fix, "engine.go:9 real bug") {
		t.Errorf("fix: %q", got.Fix)
	}
}

func TestTriageTagVariants(t *testing.T) {
	for name, line := range map[string]string{
		"bold":     "**[later]** Add a flag",
		"numbered": "1. [later] Add a flag",
		"plain":    "[later] Add a flag",
		"upper":    "- [LATER] Add a flag",
	} {
		got, err := Triage(review(line, "nothing", "nothing", "nothing"))
		if err != nil || len(got.Later) != 1 || got.Later[0].Title != "Add a flag" {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
}

func TestTriageRefusesMalformedNonFixFindings(t *testing.T) {
	for name, finding := range map[string]string{
		"owner without options":   "- [owner] Which one?",
		"owner with one option":   "- [owner] Which one?\n  Options:\n  1. only: one\n  Recommend: 1 - only one",
		"owner without recommend": "- [owner] Which one?\n  Options:\n  1. a: one\n  2. b: two",
		"later without title":     "- [later]",
		"owner without title":     "- [owner]\n  Options:\n  1. a: one\n  2. b: two\n  Recommend: 1 - a",
	} {
		_, err := Triage(review(finding, "nothing", "nothing", "nothing"))
		var se *ShapeError
		if !errors.As(err, &se) {
			t.Errorf("%s: err %v, want a *ShapeError", name, err)
		}
	}
}

// ownerProblem is the shape problem Triage gives for one owner finding body.
func ownerProblem(t *testing.T, body string) string {
	t.Helper()
	_, err := Triage(review("- [owner] Which one?\n"+body, "nothing", "nothing", "nothing"))
	var se *ShapeError
	if !errors.As(err, &se) || len(se.Problems) != 1 {
		t.Fatalf("err %v, want a *ShapeError with one problem", err)
	}
	return se.Problems[0]
}

func TestOwnerFindingErrorNamesTheRealDefect(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"no Options line", "  Needs a decision.", "has no Options: line"},
		{"Recommend without Options", "  1. a: one\n  Recommend: 1 - a", "has no Options: line"},
		{"one option", "  Options:\n  1. only: one\n  Recommend: 1 - only one", "needs 2 to 4 options, has 1"},
		{"five options", "  Options:\n  1. a: x\n  2. b: x\n  3. c: x\n  4. d: x\n  5. e: x\n  Recommend: 1 - a", `numbered line "5. e: x"`},
		{"misnumbered option", "  Options:\n  1. a: one\n  3. b: two\n  Recommend: 1 - a", "option 2 is numbered 3"},
		{"option without summary", "  Options:\n  1. a: one\n  2. beta\n  Recommend: 1 - a", `"2. beta" is not an option`},
		{"repeated label", "  Options:\n  1. a: one\n  2. a: two\n  Recommend: 1 - a", `option 2 repeats the label "a"`},
		{"no Recommend", "  Options:\n  1. a: one\n  2. b: two", "has no Recommend: line"},
		{"Recommend without a reason", "  Options:\n  1. a: one\n  2. b: two\n  Recommend: 1", `"Recommend: 1" is not "Recommend: N - reason"`},
		{"Recommend out of range", "  Options:\n  1. a: one\n  2. b: two\n  Recommend: 5 - x", "Recommend: 5 names no option; with 2 options use 1 to 2"},
		{"line between options", "  Options:\n  1. a: one\n  note\n  2. b: two\n  Recommend: 1 - a", `"2. b: two" comes after a line that is not an option`},
		{"numbered line after Recommend", "  Options:\n  1. a: one\n  2. b: two\n  Recommend: 1 - a\n  3. c: three", `"3. c: three" comes after Recommend:`},
		{"Options twice", "  Options:\n  1. a: one\n  Options:\n  2. b: two\n  Recommend: 1 - a", "Options: appears twice"},
		{"marked Decision key", "  Options:\n  1. a: one\n  2. b: two\n  Recommend: 1 - a\n  - Decision key: x.y", "Decision key: line must not have a list marker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ownerProblem(t, tc.body)
			for _, want := range []string{`"Which one?"`, tc.want} {
				if !strings.Contains(got, want) {
					t.Errorf("problem %q lacks %q", got, want)
				}
			}
		})
	}
}

func TestOwnerFindingErrorQuotesAtMost120Characters(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := ownerProblem(t, "  Options:\n  1. a: one\n  2. "+long+"\n  Recommend: 1 - a")
	if !strings.Contains(got, strings.Repeat("x", 100)) || strings.Contains(got, strings.Repeat("x", 121)) {
		t.Errorf("problem %q does not cut the quoted line to 120 characters", got)
	}
}

func TestOwnerFindingErrorIsNotFalse(t *testing.T) {
	// Each finding has Options: and Recommend:, so the message must not say that
	// they are missing.
	for name, body := range map[string]string{
		"misnumbered":                   "  Options:\n  1. a: one\n  3. b: two\n  Recommend: 1 - a",
		"one option":                    "  Options:\n  1. only: one\n  Recommend: 1 - a",
		"out of range":                  "  Options:\n  1. a: one\n  2. b: two\n  Recommend: 9 - a",
		"numbered line after Recommend": "  Options:\n  1. a: one\n  2. b: two\n  Recommend: 1 - a\n  3. c: three",
		"option without summary":        "  Options:\n  1. a: one\n  2. beta\n  Recommend: 1 - a",
	} {
		if got := ownerProblem(t, body); strings.Contains(got, "needs Options") || strings.Contains(got, "no Options") || strings.Contains(got, "no Recommend") {
			t.Errorf("%s: %q says Options or Recommend is missing", name, got)
		}
	}
}

func TestOwnerFindingWithADetailLineAfterRecommendIsAccepted(t *testing.T) {
	got, err := Triage(review(ownerFinding+"\n  This stays context.", "nothing", "nothing", "nothing"))
	if err != nil || len(got.Owner) != 1 || got.Owner[0].Ask == nil {
		t.Fatalf("owner %+v, err %v", got, err)
	}
}

func TestShapeErrorKeepsTheOffendingFindingLines(t *testing.T) {
	bad := "- [owner] Which one?\n  Options:\n  1. a: one\n  3. b: two\n  Recommend: 1 - a"
	_, err := Triage(review(bad, "nothing", "nothing", "nothing"))
	var se *ShapeError
	if !errors.As(err, &se) || len(se.Findings) != 1 {
		t.Fatalf("err %v, want one offending finding", err)
	}
	for _, want := range []string{"[owner] Which one?", "3. b: two", "Recommend: 1 - a"} {
		if !strings.Contains(se.Findings[0], want) {
			t.Errorf("finding text %q lacks %q", se.Findings[0], want)
		}
	}
	_, err = TriageGate("VERDICT: PASS\n\n" + bad + "\nTests: 1 passed, 0 failed\n\n## Owner needed\n\nnone\n")
	if !errors.As(err, &se) || len(se.Findings) != 1 || !strings.Contains(se.Findings[0], "3. b: two") {
		t.Fatalf("gate: err %v findings %v", err, se)
	}
}

func TestLenientTriageKeepsAnOwnerFindingWithoutOptions(t *testing.T) {
	text := review("- [owner] Which one?\n  Some detail.\n- [owner blocker] Drop it?", "nothing", "nothing", "nothing")
	got, err := triage(text, true)
	if err != nil || len(got.Owner) != 2 {
		t.Fatalf("lenient triage: %+v, %v", got, err)
	}
	if got.Owner[0].Ask != nil || !strings.Contains(got.Owner[0].Text, "Some detail.") || !got.Owner[1].Blocker || got.Owner[1].Ask != nil {
		t.Errorf("findings %+v", got.Owner)
	}
	// Other defects of a finding stay errors.
	if _, err := triage(review("- [owner]\n  Options:", "nothing", "nothing", "nothing"), true); err == nil {
		t.Error("lenient triage accepted an owner finding with no title")
	}
	if _, err := triage(review("- [owner] Q?\n  Decision key: Bad Key", "nothing", "nothing", "nothing"), true); err == nil {
		t.Error("lenient triage accepted a bad Decision key")
	}
	if _, err := Triage(text); err == nil {
		t.Error("strict triage accepted an owner finding without options")
	}
}

func TestTriageSectionCHoldsTaggedFindingsOnly(t *testing.T) {
	got, err := Triage(review("nothing", "nothing", "Note for later: S06 must undo this.\n- [later] Retire the old cache\n- [fix] undo the lock in main.go", "nothing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Later) != 1 || got.Later[0].Title != "Retire the old cache" {
		t.Errorf("later: %+v", got.Later)
	}
	if !strings.Contains(got.Fix, "undo the lock in main.go") || strings.Contains(got.Fix, "S06 must undo") {
		t.Errorf("fix: %q", got.Fix)
	}
}

const gateTail = "\nTests: 12 passed, 0 failed\n\n## Owner needed\n\nnone\n"

func TestTriageGateKeepsTheReportMinusNonFixFindings(t *testing.T) {
	text := "VERDICT: FAIL\n\n- engine.go:10 missing nil check\n" + fixFinding + "\n\nNon-blocking\n" + laterFinding + "\n" + ownerFinding + "\n" + gateTail
	got, err := TriageGate(text)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Tagged || len(got.Later) != 1 || len(got.Owner) != 1 {
		t.Fatalf("gate triage: %+v", got)
	}
	for _, want := range []string{"VERDICT: FAIL", "engine.go:10 missing nil check", "engine.go:42 drops the error.", "Tests: 12 passed, 0 failed"} {
		if !strings.Contains(got.Fix, want) {
			t.Errorf("gate fix text lacks %q:\n%s", want, got.Fix)
		}
	}
	for _, not := range []string{"--json flag", "Should the cache", "isolation is simpler"} {
		if strings.Contains(got.Fix, not) {
			t.Errorf("gate fix text includes %q:\n%s", not, got.Fix)
		}
	}
}

func TestTriageGateWithoutTagsChangesNothing(t *testing.T) {
	text := gateFail
	got, err := TriageGate(text)
	if err != nil || got.Tagged || got.Fix != text || len(got.Owner)+len(got.Later) != 0 {
		t.Errorf("untagged gate: %+v, %v", got, err)
	}
}

func TestTriageGateIgnoresTagsInTheOwnerNeededSection(t *testing.T) {
	text := "VERDICT: PASS\n\n- [later] a real finding\nTests: 1 passed, 0 failed\n\n## Owner needed\n\n[later] not a finding here\n"
	got, err := TriageGate(text)
	if err != nil || len(got.Later) != 1 || got.Later[0].Title != "a real finding" {
		t.Errorf("%+v, %v", got, err)
	}
}

// promptExample is the text block under the Finding kinds heading of a prompt.
func promptExample(t *testing.T, role string) string {
	t.Helper()
	p, err := roles.Prompt(role)
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(p, "## Finding kinds")
	if !ok {
		t.Fatalf("%s has no Finding kinds section", role)
	}
	_, block, ok := strings.Cut(after, "```text\n")
	if !ok {
		t.Fatalf("%s: Finding kinds has no text example", role)
	}
	block, _, _ = strings.Cut(block, "```")
	return block
}

// The examples in the prompts are what the parser reads.
func TestPromptExamplesParse(t *testing.T) {
	for _, role := range []string{"reviewer", "ui"} {
		got, err := Triage(review(promptExample(t, role), "nothing", "nothing", "nothing"))
		if err != nil || !got.Tagged || !strings.Contains(got.Fix, "engine.go:42") || len(got.Owner) != 1 || got.Owner[0].Ask == nil || got.Owner[0].Ask.Recommend != 2 || len(got.Later) != 1 {
			t.Errorf("%s example: %+v, %v", role, got, err)
		}
	}
	got, err := TriageGate("VERDICT: FAIL\n\n" + promptExample(t, "gate") + gateTail)
	if err != nil || !got.Tagged || !strings.Contains(got.Fix, "engine.go:42") || len(got.Owner) != 1 || len(got.Later) != 1 {
		t.Errorf("gate example: %+v, %v", got, err)
	}
}

func TestTriageGateAColumnZeroLineEndsABulletedFinding(t *testing.T) {
	text := "VERDICT: PASS\n\n- [later] Add a flag\n  Why: scripts parse it.\nTests: 12 passed, 0 failed\n\n## Owner needed\n\nnone\n"
	got, err := TriageGate(text)
	if err != nil || len(got.Later) != 1 || strings.Contains(got.Later[0].Text, "Tests:") {
		t.Fatalf("later: %+v, %v", got, err)
	}
	if !strings.Contains(got.Fix, "Tests: 12 passed, 0 failed") || strings.Contains(got.Fix, "scripts parse") {
		t.Errorf("fix text: %q", got.Fix)
	}
}

// S288: a body line that is not the block is context, not a shape error.

const ownerBlock = "  Options:\n  1. a: one\n  2. b: two\n  Recommend: 1 - a"

func TestOwnerFindingKeepsProseAsContext(t *testing.T) {
	text := "- [owner] Which one?\n  Because of reasons.\n  The second reason.\n" + ownerBlock
	got, err := Triage(review(text, "nothing", "nothing", "nothing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Owner) != 1 || got.Owner[0].Ask == nil || len(got.Owner[0].Ask.Options) != 2 || got.Owner[0].Ask.Recommend != 1 {
		t.Fatalf("owner findings: %+v", got.Owner)
	}
	for _, want := range []string{"Because of reasons.", "The second reason."} {
		if !strings.Contains(got.Owner[0].Text, want) {
			t.Errorf("Text %q lacks context %q", got.Owner[0].Text, want)
		}
	}
	if got.Owner[0].Context != "Because of reasons.\nThe second reason." {
		t.Errorf("Context %q", got.Owner[0].Context)
	}
	if strings.Contains(got.Fix, "Because of reasons.") {
		t.Errorf("context went to the fix round: %q", got.Fix)
	}
}

func TestOwnerFindingAcceptsListMarkers(t *testing.T) {
	for name, text := range map[string]string{
		"dash bullets":  "- [owner] Which one?\n  - Options:\n    1. a: one\n    2. b: two\n  - Recommend: 1 - a",
		"star bullets":  "- [owner] Which one?\n  * Options:\n  * 1. a: one\n  * 2. b: two\n  * Recommend: 1 - a",
		"plus bullets":  "- [owner] Which one?\n  + Options:\n  + 1. a: one\n  + 2. b: two\n  + Recommend: 1 - a",
		"nested bullet": "- [owner] Which one?\n  - Options:\n    - 1. a: one\n    - 2. b: two\n  - Recommend: 1 - a",
	} {
		got, err := Triage(review(text, "nothing", "nothing", "nothing"))
		if err != nil || len(got.Owner) != 1 || got.Owner[0].Ask == nil {
			t.Errorf("%s: %+v, %v", name, got, err)
			continue
		}
		ask := got.Owner[0].Ask
		if len(ask.Options) != 2 || ask.Options[0].Label != "a" || ask.Options[1].Summary != "two" || ask.Recommend != 1 || ask.Reason != "a" {
			t.Errorf("%s: ask %+v", name, ask)
		}
	}
}

func TestOwnerFindingKeepsTrailingProse(t *testing.T) {
	text := "- [owner blocker] Which one?\n" + ownerBlock + "\n  Decision key: cache.scope\n  - The first trailing point.\n  - The second trailing point."
	got, err := TriageGate("VERDICT: FAIL\n\n" + text + "\n" + gateTail)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Owner) != 1 || got.Blocker() == nil {
		t.Fatalf("owner findings: %+v", got.Owner)
	}
	f := got.Owner[0]
	if f.Key != "cache.scope" || f.Ask == nil || len(f.Ask.Options) != 2 || f.Ask.Reason != "a" {
		t.Errorf("finding: %+v", f)
	}
	if f.Context != "- The first trailing point.\n- The second trailing point." {
		t.Errorf("Context %q", f.Context)
	}
	for _, want := range []string{"The first trailing point.", "The second trailing point.", "Recommend: 1 - a"} {
		if !strings.Contains(f.Text, want) {
			t.Errorf("Text %q lacks %q", f.Text, want)
		}
	}
}

func TestOwnerFindingStillNeedsOptionsAndRecommend(t *testing.T) {
	for name, finding := range map[string]string{
		"no options":                    "- [owner] Which one?\n  Only prose here.",
		"one option":                    "- [owner] Which one?\n  Options:\n  1. only: one\n  Recommend: 1 - only one",
		"no recommend":                  "- [owner] Which one?\n  Prose.\n  Options:\n  1. a: one\n  2. b: two\n  More prose.",
		"no title":                      "- [owner]\n  Prose.\n  Options:\n  1. a: one\n  2. b: two\n  Recommend: 1 - a",
		"prose between options":         "- [owner] Which one?\n  Options:\n  1. a: one\n  Because.\n  2. b: two\n  Recommend: 1 - a",
		"malformed option":              "- [owner] Which one?\n  Options:\n  1. a: one\n  2. b: two\n  3. c\n  Recommend: 1 - a",
		"recommend before options":      "- [owner] Which one?\n  Recommend: 1 - a\n  Options:\n  1. a: one\n  2. b: two",
		"marked Decision key":           "- [owner] Which one?\n" + ownerBlock + "\n  - Decision key: cache.scope",
		"option after recommend":        "- [owner] Which one?\n" + ownerBlock + "\n  3. c: three",
		"numbered line after recommend": "- [owner] Which one?\n" + ownerBlock + "\n  3) c: three",
		"final option without space":    "- [owner] Which one?\n  Options:\n  1. a: one\n  2. b: two\n  3.c: three\n  Recommend: 1 - a",
		"final option with paren":       "- [owner] Which one?\n  Options:\n  1. a: one\n  2. b: two\n  3) c: three\n  Recommend: 1 - a",
		"two Recommend lines":           "- [owner] Which one?\n" + ownerBlock + "\n  Recommend: 2 - b",
		"recommend on both sides":       "- [owner] Which one?\n  Recommend: 2 - b\n" + ownerBlock,
		"options after recommend":       "- [owner] Which one?\n" + ownerBlock + "\n  Options:",
		"two Options lines":             "- [owner] Which one?\n  Options:\n  Options:\n  1. a: one\n  2. b: two\n  Recommend: 1 - a",
		"recommend without a reason":    "- [owner] Which one?\n  Options:\n  1. a: one\n  2. b: two\n  Recommend: 1",
		"recommend beyond the options":  "- [owner] Which one?\n  Options:\n  1. a: one\n  2. b: two\n  Recommend: 3 - c",
	} {
		_, err := Triage(review(finding, "nothing", "nothing", "nothing"))
		var se *ShapeError
		if !errors.As(err, &se) {
			t.Errorf("%s: err %v, want a *ShapeError", name, err)
		}
	}
}

// promptOwnerExamples returns the fenced blocks a prompt introduces with a
// line that starts with "Refused" or "Accepted", keyed by that word.
func promptOwnerExamples(prompt string) map[string][]string {
	out := map[string][]string{}
	var kind string
	var block []string
	inFence := false
	for _, line := range strings.Split(prompt, "\n") {
		switch {
		case inFence && strings.HasPrefix(line, "```"):
			if kind != "" {
				out[kind] = append(out[kind], strings.Join(block, "\n"))
			}
			inFence, kind, block = false, "", nil
		case inFence:
			block = append(block, line)
		case strings.HasPrefix(line, "```"):
			inFence = true
		case strings.HasPrefix(line, "Refused"):
			kind = "Refused"
		case strings.HasPrefix(line, "Accepted"):
			kind = "Accepted"
		}
	}
	return out
}

func TestOwnerFindingExamplesParse(t *testing.T) {
	for _, role := range []string{"reviewer", "gate", "ui"} {
		p, err := roles.Prompt(role)
		if err != nil {
			t.Fatal(err)
		}
		ex := promptOwnerExamples(p)
		if len(ex["Refused"]) != 1 || len(ex["Accepted"]) != 1 {
			t.Errorf("%s: %d refused and %d accepted owner examples, want one of each", role, len(ex["Refused"]), len(ex["Accepted"]))
			continue
		}
		got, err := Triage(review(ex["Accepted"][0], "nothing", "nothing", "nothing"))
		if err != nil || len(got.Owner) != 1 || got.Owner[0].Ask == nil {
			t.Errorf("%s: accepted example: %+v, %v", role, got, err)
		} else {
			// The parser keeps extra lines as context, so check that the
			// example itself holds the block and nothing else.
			_, body, _ := strings.Cut(got.Owner[0].Text, "\n")
			for _, line := range strings.Split(body, "\n") {
				line = strings.TrimSpace(line)
				if line != "Options:" && !optionLine.MatchString(line) && !recommendLine.MatchString(line) && !strings.HasPrefix(line, "Decision key:") {
					t.Errorf("%s: accepted example has a line outside the block: %q", role, line)
				}
			}
		}
		_, err = Triage(review(ex["Refused"][0], "nothing", "nothing", "nothing"))
		var se *ShapeError
		if !errors.As(err, &se) {
			t.Errorf("%s: refused example gave %v, want a *ShapeError", role, err)
		}
	}
}

// S288: one tail names every rule a refused owner block broke.
const shapeTail = "with no line between the options, no numbered line after Recommend:, and no list marker before Decision key:"

func TestOwnerFindingRefusalMessageNamesTheRules(t *testing.T) {
	for name, finding := range map[string]string{
		"marked Decision key":    "- [owner] Which one?\n" + ownerBlock + "\n  - Decision key: cache.scope",
		"option after recommend": "- [owner] Which one?\n" + ownerBlock + "\n  3. c: three",
		"prose between options":  "- [owner] Which one?\n  Options:\n  1. a: one\n  Because.\n  2. b: two\n  Recommend: 1 - a",
	} {
		_, err := Triage(review(finding, "nothing", "nothing", "nothing"))
		var se *ShapeError
		if !errors.As(err, &se) || !strings.Contains(strings.Join(se.Problems, "\n"), shapeTail) {
			t.Errorf("%s: %v lacks %q", name, err, shapeTail)
		}
	}
}
