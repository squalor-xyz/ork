// SPDX-License-Identifier: GPL-3.0-or-later

package docscheck

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type lineRange struct{ first, last int }
type citation struct {
	path   string
	ranges []lineRange
}

func TestCitationParserHandlesRangesAndSuffixes(t *testing.T) {
	for _, tt := range []struct {
		name, text string
		want       []citation
	}{
		{"line", "`a.go:5`", []citation{{"a.go", []lineRange{{5, 5}}}}},
		{"range", "`a.go:5-9`", []citation{{"a.go", []lineRange{{5, 9}}}}},
		{"suffix", "`a.go:5-9,12`", []citation{{"a.go", []lineRange{{5, 9}, {12, 12}}}}},
		{"suffixRange", "`a.go:5,9-12`", []citation{{"a.go", []lineRange{{5, 5}, {9, 12}}}}},
		{"prose", "See (`a.go:5`, `b.md:9-12`), next.", []citation{{"a.go", []lineRange{{5, 5}}}, {"b.md", []lineRange{{9, 12}}}}},
		{"insideSpan", "See `a.go:5,` and `b.md:9)`.", []citation{{"a.go", []lineRange{{5, 5}}}, {"b.md", []lineRange{{9, 9}}}}},
		{"plainProse", "a.go:5 outside a backtick span", nil},
		{"multilineSpan", "Use `ork daemon\nrestart` (`a.go:5`).", []citation{{"a.go", []lineRange{{5, 5}}}}},
		{"fencedBlock", "```text\n`ignored.go:1`\n```\n`a.go:5`", []citation{{"a.go", []lineRange{{5, 5}}}}},
		{"paragraphs", "Unmatched ` in prose.\n\nSee `a.go:5`.", []citation{{"a.go", []lineRange{{5, 5}}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseCitations(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("citations = %#v; want %#v", got, tt.want)
			}
		})
	}
}

func TestCitationTestFailsOnAMissingFile(t *testing.T) {
	err := checkCitation(t.TempDir(), citation{"missing.go", []lineRange{{1, 1}}})
	if err == nil || !strings.Contains(err.Error(), "missing.go") {
		t.Fatalf("missing file error = %v", err)
	}
}

func TestCitationTestFailsOnAnOutOfRangeLine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("one\ntwo"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		ranges []lineRange
		bad    bool
	}{
		{"lastLine", []lineRange{{1, 2}}, false},
		{"pastEnd", []lineRange{{1, 3}}, true},
		{"zero", []lineRange{{0, 1}}, true},
		{"reversed", []lineRange{{2, 1}}, true},
		{"suffixPastEnd", []lineRange{{1, 1}, {3, 3}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := checkCitation(root, citation{"a.go", tt.ranges})
			if (err != nil) != tt.bad {
				t.Fatalf("range error = %v; want error %v", err, tt.bad)
			}
		})
	}
}

func TestGuideAndDesignCitationsResolve(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, doc := range []string{"docs/guide.md", "docs/design.md"} {
		data, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatal(err)
		}
		cites := parseCitations(string(data))
		if len(cites) == 0 {
			t.Fatalf("no citations found in %s", doc)
		}
		for _, cite := range cites {
			t.Run(doc+"/"+cite.path, func(t *testing.T) {
				base := root
				for _, sibling := range []string{"slicer", "adjudicate", "agents"} {
					if strings.HasPrefix(cite.path, sibling+"/") {
						base = filepath.Join(root, "..")
						if _, err := os.Stat(filepath.Join(base, sibling)); os.IsNotExist(err) {
							t.Skipf("sibling %s absent", sibling)
						} else if err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := checkCitation(base, cite); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

var fencedBlock = regexp.MustCompile("(?ms)^```[^\n]*\n.*?^```[ \t]*(?:\n|$)")
var backtickSpan = regexp.MustCompile("`([^`]+)`")
var citationPattern = regexp.MustCompile(`([A-Za-z0-9_./-]+\.(?:go|md|json|sh|yml|txt)):([0-9]+(?:-[0-9]+)?(?:,[0-9]+(?:-[0-9]+)?)*)`)

func parseCitations(text string) []citation {
	var cites []citation
	text = fencedBlock.ReplaceAllString(text, "")
	for _, paragraph := range strings.Split(text, "\n\n") {
		for _, span := range backtickSpan.FindAllStringSubmatch(paragraph, -1) {
			for _, match := range citationPattern.FindAllStringSubmatch(span[1], -1) {
				cite := citation{path: match[1]}
				for _, part := range strings.Split(match[2], ",") {
					ends := strings.SplitN(part, "-", 2)
					first, err := strconv.Atoi(ends[0])
					if err != nil {
						first = 0
					}
					last := first
					if len(ends) == 2 {
						last, err = strconv.Atoi(ends[1])
						if err != nil {
							last = 0
						}
					}
					cite.ranges = append(cite.ranges, lineRange{first, last})
				}
				cites = append(cites, cite)
			}
		}
	}
	return cites
}

func checkCitation(root string, cite citation) error {
	data, err := os.ReadFile(filepath.Join(root, cite.path))
	if err != nil {
		return err
	}
	lines := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}
	for _, span := range cite.ranges {
		if span.first < 1 || span.last < span.first || span.last > lines {
			return fmt.Errorf("%s:%d-%d outside 1..%d", cite.path, span.first, span.last, lines)
		}
	}
	return nil
}
