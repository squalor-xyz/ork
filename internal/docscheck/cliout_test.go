// SPDX-License-Identifier: GPL-3.0-or-later

package docscheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var digitRun = regexp.MustCompile(`[0-9]+`)

// collapseSpace replaces each whitespace run with one space and trims the ends.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// quotedSpans returns the backtick spans of text outside fenced blocks, one
// paragraph at a time, with each whitespace run collapsed to one space.
func quotedSpans(text string) []string {
	var spans []string
	text = fencedBlock.ReplaceAllString(text, "")
	for _, paragraph := range strings.Split(text, "\n\n") {
		for _, span := range backtickSpan.FindAllStringSubmatch(paragraph, -1) {
			spans = append(spans, collapseSpace(span[1]))
		}
	}
	return spans
}

// testStringLiterals returns every Go string literal in src as its unquoted
// value with whitespace collapsed, one entry per literal, in source order.
func testStringLiterals(src string) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "src.go", src, 0)
	if err != nil {
		return nil, err
	}
	var literals []string
	var walkErr error
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			walkErr = err
			return false
		}
		literals = append(literals, collapseSpace(value))
		return true
	})
	return literals, walkErr
}

// checkCLIOutputQuotes flags each doc span that pairs with a test literal by
// its digit-masked text but is not contained in any paired literal exactly.
// Only spans of at least three words with a digit take part.
func checkCLIOutputQuotes(spans, literals []string) []error {
	var errs []error
	masked := make([]string, len(literals))
	for i, lit := range literals {
		masked[i] = digitRun.ReplaceAllString(lit, "#")
	}
	for _, span := range spans {
		if len(strings.Fields(span)) < 3 || !digitRun.MatchString(span) {
			continue
		}
		maskedSpan := digitRun.ReplaceAllString(span, "#")
		first := -1
		agrees := false
		for i, lit := range literals {
			if !strings.Contains(masked[i], maskedSpan) {
				continue
			}
			if first < 0 {
				first = i
			}
			if strings.Contains(lit, span) {
				agrees = true
				break
			}
		}
		if first >= 0 && !agrees {
			errs = append(errs, fmt.Errorf("docs quote %q but internal/cli/cli_test.go expects %q", span, literals[first]))
		}
	}
	return errs
}

func TestQuotedSpansJoinWrappedLines(t *testing.T) {
	text := "Intro `permissions=implementer:3 patterns;\n  reviewer:read-only` end.\n\n" +
		"```text\n`fenced span here`\n```\n\n" +
		"Unmatched ` in this paragraph.\n\n" +
		"Next `second span ok`."
	want := []string{"permissions=implementer:3 patterns; reviewer:read-only", "second span ok"}
	if got := quotedSpans(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("spans = %#v; want %#v", got, want)
	}
}

func TestTestStringLiteralsUnquotesGoStrings(t *testing.T) {
	src := "package p\n" +
		"var a = \"x\\ny \\\"q\\\"\"\n" +
		"var b = `raw  text\n\tmore`\n" +
		"var c = \"a\" + x + \"b\"\n"
	want := []string{"x y \"q\"", "raw text more", "a", "b"}
	got, err := testStringLiterals(src)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("literals = %#v; want %#v", got, want)
	}
}

func TestCLIOutputQuoteDisagreementIsFlagged(t *testing.T) {
	span := "implementer:18 patterns (preset python)"
	literal := "updated slicer: permissions=implementer:25 patterns (preset python)\n"
	errs := checkCLIOutputQuotes([]string{span}, []string{strings.Join(strings.Fields(literal), " ")})
	if len(errs) != 1 {
		t.Fatalf("errors = %v; want one", errs)
	}
	for _, part := range []string{span, "implementer:25 patterns (preset python)"} {
		if !strings.Contains(errs[0].Error(), part) {
			t.Errorf("error %q does not name %q", errs[0], part)
		}
	}
}

func TestCLIOutputQuoteAgreementPasses(t *testing.T) {
	literals := []string{"updated slicer: permissions=implementer:26 patterns (preset python)", "usage error, exit 3"}
	for _, tt := range []struct {
		name, span string
	}{
		{"fragment", "implementer:26 patterns (preset python)"},
		{"noCounterpart", "wholly different words 7 here"},
		{"shortSpan", "exit 2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if errs := checkCLIOutputQuotes([]string{tt.span}, literals); len(errs) != 0 {
				t.Fatalf("flagged: %v", errs)
			}
		})
	}
}

func TestDesignCLIOutputQuotesMatchCLITests(t *testing.T) {
	root := filepath.Join("..", "..")
	doc, err := os.ReadFile(filepath.Join(root, "docs", "design.md"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "internal", "cli", "cli_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	literals, err := testStringLiterals(string(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(literals) == 0 {
		t.Fatal("no string literals found in internal/cli/cli_test.go")
	}
	spans := quotedSpans(string(doc))
	if len(spans) == 0 {
		t.Fatal("no quoted spans found in docs/design.md")
	}
	for _, err := range checkCLIOutputQuotes(spans, literals) {
		t.Error(err)
	}
}
