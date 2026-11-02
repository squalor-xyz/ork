// SPDX-License-Identifier: GPL-3.0-or-later

package release

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// blockStart matches a line that opens a block scalar.
var blockStart = regexp.MustCompile(`:[ \t]+[|>][+-]?$`)

var (
	tagsFlow  = regexp.MustCompile(`^tags:[ \t]*\[[ \t]*(?:"v\*"|'v\*')[ \t]*\]$`)
	tagsBlock = regexp.MustCompile(`^- (?:"v\*"|'v\*')$`)
)

type normLine struct {
	depth int
	text  string
	raw   bool // block scalar content: text is the whole line
}

// normalize gives the workflow text one canonical layout so the guards do
// not depend on formatting: LF line ends, no trailing blanks, two spaces per
// level, no blank or comment-only lines outside block scalars, list items
// one level under their key, and `tags: ['v*']`. Block scalar content is
// kept line for line, moved as a whole so its first line sits two spaces
// under its key; trailing blank lines of a block are dropped. Flow mappings
// and anchors are not read; the guards then report the text as missing.
func normalize(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	type frame struct {
		indent int
		text   string
	}
	var stack []frame
	var out []normLine
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		body := strings.TrimLeft(line, " ")
		if body == "" || strings.HasPrefix(body, "#") {
			continue
		}
		orig := len(line) - len(body)
		indent := orig
		for len(stack) > 0 && stack[len(stack)-1].indent > indent {
			stack = stack[:len(stack)-1]
		}
		top := len(stack) - 1
		switch {
		case top >= 0 && stack[top].indent == indent && strings.HasPrefix(body, "- ") && strings.HasSuffix(stack[top].text, ":"):
			// A list written at the indent of its key is one level deeper.
			indent++
			stack = append(stack, frame{indent, body})
		case top >= 0 && stack[top].indent == indent:
			stack[top].text = body
		default:
			stack = append(stack, frame{indent, body})
		}
		depth := len(stack) - 1
		out = append(out, normLine{depth: depth, text: body})
		if !blockStart.MatchString(body) {
			continue
		}
		keyOrig, keyCol := orig, 2*depth
		if strings.HasPrefix(body, "- ") {
			keyOrig, keyCol = orig+2, keyCol+2
		}
		end := i + 1
		for end < len(lines) && (lines[end] == "" || indentOf(lines[end]) > keyOrig) {
			end++
		}
		for end > i+1 && lines[end-1] == "" {
			end--
		}
		shift, shiftSet := 0, false
		for _, l := range lines[i+1 : end] {
			if l == "" {
				out = append(out, normLine{raw: true})
				continue
			}
			if !shiftSet {
				shift, shiftSet = indentOf(l)-(keyCol+2), true
			}
			out = append(out, normLine{raw: true, text: strings.Repeat(" ", max(0, indentOf(l)-shift)) + strings.TrimLeft(l, " ")})
		}
		i = end - 1
	}

	var b strings.Builder
	for i := 0; i < len(out); i++ {
		l := out[i]
		if l.raw {
			b.WriteString(l.text + "\n")
			continue
		}
		pad := strings.Repeat("  ", l.depth)
		switch {
		case tagsFlow.MatchString(l.text):
			b.WriteString(pad + "tags: ['v*']\n")
		case l.text == "tags:" && i+1 < len(out) && !out[i+1].raw && tagsBlock.MatchString(out[i+1].text) &&
			!(i+2 < len(out) && !out[i+2].raw && strings.HasPrefix(out[i+2].text, "- ") && out[i+2].depth == out[i+1].depth):
			b.WriteString(pad + "tags: ['v*']\n")
			i++
		default:
			b.WriteString(pad + l.text + "\n")
		}
	}
	return b.String()
}

func TestNormalizeWorkflow(t *testing.T) {
	real := readWorkflow(t)
	var noBlank []string
	for _, line := range strings.Split(real, "\n") {
		if line != "" {
			noBlank = append(noBlank, line)
		}
	}
	// The real file has no comment-only line and no blank line inside a
	// block scalar, so it normalises to itself less its blank lines.
	t.Run("real_file", func(t *testing.T) {
		got := normalize(real)
		if want := strings.Join(noBlank, "\n") + "\n"; got != want {
			t.Errorf("real file changed:\n%s", got)
		}
		if again := normalize(got); again != got {
			t.Errorf("normalize is not idempotent:\n%s", again)
		}
	})

	tests := []struct{ name, in, want string }{
		{"four_space_copy",
			"on:\n    push:\n        tags: ['v*']\njobs:\n    t:\n        steps:\n            - run: |\n                  echo a\n                    echo b\n            - uses: x\n              with:\n                  k: v\n",
			"on:\n  push:\n    tags: ['v*']\njobs:\n  t:\n    steps:\n      - run: |\n          echo a\n            echo b\n      - uses: x\n        with:\n          k: v\n"},
		{"crlf_spaces_blank_comments",
			"a:  \r\n\r\n  # a comment\r\n  b: 1   \r\n\r\n  # another\r\n  c:\r\n    - uses: x # v4.2.2\r\n",
			"a:\n  b: 1\n  c:\n    - uses: x # v4.2.2\n"},
		{"block_scalar_kept",
			"s:\n    - run: |\n        # keep\n          indented\n\n        last\n    # dropped\n\n    - name: n\n",
			"s:\n  - run: |\n      # keep\n        indented\n\n      last\n  - name: n\n"},
		{"block_scalar_plain_key",
			"jobs:\n      run: |-\n          a: 'b'\n           c\n      next: 1\n",
			"jobs:\n  run: |-\n    a: 'b'\n     c\n  next: 1\n"},
		{"sequence_at_parent_indent",
			"steps:\n- uses: a\n  with:\n    k: v\n- uses: b\nnext: 1\n",
			"steps:\n  - uses: a\n    with:\n      k: v\n  - uses: b\nnext: 1\n"},
		{"tags_flow_double", "on:\n  push:\n    tags: [\"v*\"]\n", "on:\n  push:\n    tags: ['v*']\n"},
		{"tags_flow_spaced", "on:\n  push:\n    tags: [ 'v*' ]\n", "on:\n  push:\n    tags: ['v*']\n"},
		{"tags_block_single_quote", "on:\n  push:\n    tags:\n      - 'v*'\n  workflow_dispatch:\n", "on:\n  push:\n    tags: ['v*']\n  workflow_dispatch:\n"},
		{"tags_block_double_quote_flush", "on:\n  push:\n    tags:\n    - \"v*\"\n  workflow_dispatch:\n", "on:\n  push:\n    tags: ['v*']\n  workflow_dispatch:\n"},
		{"tags_block_two_items_kept", "on:\n  push:\n    tags:\n      - 'v*'\n      - 'x*'\n", "on:\n  push:\n    tags:\n      - 'v*'\n      - 'x*'\n"},
		{"double_quote_in_run_kept", "x:\n  run: echo \"v*\" \"$A\"\n  tags: echo [\"v*\"]\n", "x:\n  run: echo \"v*\" \"$A\"\n  tags: echo [\"v*\"]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalize(tt.in); got != tt.want {
				t.Errorf("normalize(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGuardsSurviveReformatting(t *testing.T) {
	real := readWorkflow(t)
	want := normalize(real)
	variants := []struct{ name, text string }{
		{"four_space_indent", doubleIndent(real)},
		{"quotes_comments_blanks_crlf", noisy(real)},
		{"dash_at_parent_indent", dashAtParentIndent(real)},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			if v.text == real {
				t.Fatal("variant did not change the workflow")
			}
			got := normalize(v.text)
			if got != want {
				t.Errorf("variant normalises differently from the real file:\n%s", got)
			}
			for _, c := range workflowChecks {
				if failures := runCheck(c, got); len(failures) > 0 {
					t.Errorf("guard %s fails on the reformatted copy: %v", c.name, failures)
				}
			}
		})
	}
}

func TestGuardsStillFailOnRealChanges(t *testing.T) {
	base := doubleIndent(readWorkflow(t))
	for _, c := range workflowChecks {
		if failures := runCheck(c, normalize(base)); len(failures) > 0 {
			t.Fatalf("guard %s fails on the unchanged 4-space copy: %v", c.name, failures)
		}
	}
	checkout := "actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2"
	tests := []struct {
		guard string
		edit  func(string) string
	}{
		{"pinned_actions", func(s string) string { return strings.Replace(s, checkout, "actions/checkout@v4", 1) }},
		{"permissions", func(s string) string {
			return insertAfter(s, "needs: test", "        permissions:\n            contents: write")
		}},
		{"version_and_vcs", func(s string) string {
			return strings.Replace(s, "go build -trimpath", "go build -buildvcs=false -trimpath", 1)
		}},
		{"dispatch_dry_run", func(s string) string {
			return insertBefore(s, "- name: Publish release", "            - run: gh release create v0 dist/x.tar.gz")
		}},
		{"persist_credentials", func(s string) string { return removeLine(s, "persist-credentials: false") }},
		{"setup_go_cache", func(s string) string { return removeLine(s, "cache: false") }},
		{"changelog_section", func(s string) string { return removeLine(s, "END { exit !found }") }},
		{"tag_signature", func(s string) string { return removeLine(s, "%(contents:signature)") }},
	}
	for _, tt := range tests {
		t.Run(tt.guard, func(t *testing.T) {
			changed := tt.edit(base)
			if changed == base {
				t.Fatal("edit changed nothing")
			}
			for _, c := range workflowChecks {
				if c.name != tt.guard {
					continue
				}
				if failures := runCheck(c, normalize(changed)); len(failures) == 0 {
					t.Errorf("guard %s still passes after the edit", tt.guard)
				}
				return
			}
			t.Fatalf("no guard named %s", tt.guard)
		})
	}
}

// recorder collects the failures of one guard run.
type recorder struct{ failures []string }

type stopCheck struct{}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

// Fatalf ends the check, as testing.T does, with a panic runCheck recovers.
func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	panic(stopCheck{})
}

func runCheck(c workflowCheck, s string) (failures []string) {
	r := &recorder{}
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(stopCheck); !ok {
				panic(p)
			}
		}
		failures = r.failures
	}()
	c.check(r, s)
	return nil
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// mapLines applies f to every line of s with whether the line is block
// scalar content, using the same rule as the workflow files: a block runs
// while lines are blank or indented deeper than the key line.
func mapLines(s string, f func(line string, inBlock bool, keyIndent int) string) string {
	lines := strings.Split(s, "\n")
	keyIndent := -1
	for i, line := range lines {
		inBlock := keyIndent >= 0 && (line == "" || indentOf(line) > keyIndent)
		if !inBlock {
			keyIndent = -1
		}
		lines[i] = f(line, inBlock, keyIndent)
		if !inBlock && blockStart.MatchString(line) {
			keyIndent = indentOf(line)
			if strings.HasPrefix(strings.TrimLeft(line, " "), "- ") {
				keyIndent += 2
			}
		}
	}
	return strings.Join(lines, "\n")
}

// doubleIndent turns the 2-space layout into a 4-space layout. Block scalar
// content moves with its key and keeps its relative indent.
func doubleIndent(s string) string {
	pad := 0
	return mapLines(s, func(line string, inBlock bool, keyIndent int) string {
		if line == "" {
			return line
		}
		if inBlock {
			return strings.Repeat(" ", pad) + line
		}
		pad = indentOf(line)
		return strings.Repeat(" ", pad) + line
	})
}

// noisy adds the reformatting noise that is not a real change.
func noisy(s string) string {
	s = strings.Replace(s, `tags: ['v*']`, `tags: ["v*"]`, 1)
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		if strings.HasPrefix(line, "      - ") {
			out = append(out, "", "      # next step")
		}
		if line != "" {
			line += "  "
		}
		out = append(out, line)
	}
	return strings.Join(out, "\r\n")
}

// dashAtParentIndent writes every "- " item at the indent of the key that
// owns the list, as some formatters do.
func dashAtParentIndent(s string) string {
	lines := strings.Split(s, "\n")
	var regions []int // minimum indent of each open list region
	for i, line := range lines {
		if line == "" {
			continue
		}
		ind := indentOf(line)
		for len(regions) > 0 && ind < regions[len(regions)-1] {
			regions = regions[:len(regions)-1]
		}
		shifted := line[2*len(regions):]
		if strings.HasSuffix(line, ":") && i+1 < len(lines) &&
			strings.HasPrefix(strings.TrimLeft(lines[i+1], " "), "- ") && indentOf(lines[i+1]) == ind+2 {
			regions = append(regions, ind+2)
		}
		lines[i] = shifted
	}
	return strings.Join(lines, "\n")
}

func removeLine(s, part string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.Contains(line, part) {
			return strings.Join(append(lines[:i:i], lines[i+1:]...), "\n")
		}
	}
	return s
}

func insertAfter(s, part, add string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.Contains(line, part) {
			return strings.Join(append(lines[:i+1:i+1], append([]string{add}, lines[i+1:]...)...), "\n")
		}
	}
	return s
}

func insertBefore(s, part, add string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.Contains(line, part) {
			return strings.Join(append(lines[:i:i], append([]string{add}, lines[i:]...)...), "\n")
		}
	}
	return s
}
