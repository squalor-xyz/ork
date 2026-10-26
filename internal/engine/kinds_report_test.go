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
		"owner without options":    "- [owner] Which one?",
		"owner with one option":    "- [owner] Which one?\n  Options:\n  1. only: one\n  Recommend: 1 - only one",
		"owner without recommend":  "- [owner] Which one?\n  Options:\n  1. a: one\n  2. b: two",
		"owner with extra context": "- [owner] Which one?\n  Because of reasons.\n  Options:\n  1. a: one\n  2. b: two\n  Recommend: 1 - a",
		"later without title":      "- [later]",
		"owner without title":      "- [owner]\n  Options:\n  1. a: one\n  2. b: two\n  Recommend: 1 - a",
	} {
		_, err := Triage(review(finding, "nothing", "nothing", "nothing"))
		var se *ShapeError
		if !errors.As(err, &se) {
			t.Errorf("%s: err %v, want a *ShapeError", name, err)
		}
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
