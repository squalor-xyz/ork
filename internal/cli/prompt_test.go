// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReadLinePrintsANewlineWhenTheInputEnds(t *testing.T) {
	for _, input := range []string{"n", "n\n", ""} {
		var out bytes.Buffer
		line, err := readLine(bufio.NewReader(strings.NewReader(input)), &out)
		wantErr, wantOut := io.EOF, "\n"
		if strings.HasSuffix(input, "\n") {
			wantErr, wantOut = nil, ""
		}
		if line != input || err != wantErr || out.String() != wantOut {
			t.Errorf("input %q: line %q err %v output %q", input, line, err, &out)
		}
	}
}

func TestWrapLinesBreaksAtWordsAndHangs(t *testing.T) {
	var words []string
	for i := range 45 {
		words = append(words, fmt.Sprintf("word%02d", i))
	}
	for _, text := range []string{"  " + strings.Join(words, " "), "  " + strings.Repeat("界", 200) + " tail words", strings.Repeat("écho ", 45)} {
		lines := wrapLines(text, 80, "  ")
		lead := text[:len(text)-len(strings.TrimLeft(text, " "))]
		if !strings.HasPrefix(lines[0], lead) {
			t.Errorf("first line lost indentation: %q", lines[0])
		}
		for i, line := range lines {
			if utf8.RuneCountInString(line) > 80 {
				t.Errorf("line too long: %q", line)
			}
			if i > 0 && !strings.HasPrefix(line, lead+"  ") {
				t.Errorf("missing hang: %q", line)
			}
		}
		if strings.Contains(text, strings.Repeat("界", 200)) {
			if strings.Join(strings.Fields(strings.Join(lines, " ")), "") != strings.Join(strings.Fields(text), "") {
				t.Fatal("lost long word")
			}
		} else if strings.Join(strings.Fields(strings.Join(lines, " ")), " ") != strings.Join(strings.Fields(text), " ") {
			t.Fatal("split or reordered words")
		}
	}
}

func TestPlainLineDropsControlCharacters(t *testing.T) {
	for input, want := range map[string]string{"a\x1b[31mb\x07\nc\u0085d": "ab cd", "a\xffb": "a?b"} {
		if got := plainLine(input); got != want {
			t.Errorf("plainLine(%q)=%q want %q", input, got, want)
		}
	}
}

func TestWrapLinesKeepsAllWordsWhenIndentationFillsTheWidth(t *testing.T) {
	for _, tc := range []struct {
		text  string
		width int
		hang  string
	}{
		{"  alpha beta gamma delta", 4, "  "},
		{"  alpha beta gamma delta", 3, "  "},
		{"    alpha beta gamma delta", 4, "  "},
	} {
		lines := wrapLines(tc.text, tc.width, tc.hang)
		if got, want := strings.Join(strings.Fields(strings.Join(lines, " ")), ""), strings.Join(strings.Fields(tc.text), ""); got != want {
			t.Errorf("width %d hang %q: lost text: %q, want %q", tc.width, tc.hang, got, want)
		}
	}
}
