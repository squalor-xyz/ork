// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
