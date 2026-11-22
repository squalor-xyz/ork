// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A text field whose start has scrolled off begins with "..." (S394, adj 444).
// Below 6 columns the mark has no room and the field shows the plain tail.
func TestTextFieldMarksAScrolledOffStart(t *testing.T) {
	for _, tc := range []struct {
		text                   string
		cursor, width          int
		before, cursor1, after string
	}{
		{"abcdefghij", 10, 6, "...ij", " ", ""},
		{"abcdefghij", 8, 6, "...gh", "i", "j"},
		{"abcdefghij", 10, 11, "abcdefghij", " ", ""},
		{"abcdefghij", 10, 5, "ghij", " ", ""},
	} {
		f := textField{text: []rune(tc.text), cursor: tc.cursor}
		before, cursor, after := f.parts(tc.width)
		if before != tc.before || cursor != tc.cursor1 || after != tc.after {
			t.Errorf("parts(%q, cursor %d, width %d) = (%q, %q, %q), want (%q, %q, %q)",
				tc.text, tc.cursor, tc.width, before, cursor, after, tc.before, tc.cursor1, tc.after)
		}
	}
}

// Every one-line field draws through textField.parts, so each shows the cue
// once its start has scrolled off (S394, adj 444).
func TestEveryTextFieldShowsTheStartCue(t *testing.T) {
	long := strings.Repeat("x", 120)
	t.Run("note", func(t *testing.T) {
		h := roadmapHarness(t)
		h.m.cfg.Live = &noteLive{}
		h.m.Update(press("N"))
		h.m.Update(tea.PasteMsg{Content: long})
		h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		v := plainView(h.m)
		if !strings.Contains(v, "Note for S90:") || !strings.Contains(v, "...x") {
			t.Fatalf("the note field has no start cue:\n%s", v)
		}
	})
	t.Run("answer", func(t *testing.T) {
		h := paneHarness(t, "paused", "question")
		h.m.Update(tea.PasteMsg{Content: long})
		h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		v := plainView(h.m)
		found := false
		for _, line := range strings.Split(v, "\n") {
			found = found || (strings.Contains(line, "Answer: ") && strings.Contains(line, "...x"))
		}
		if !found {
			t.Fatalf("the answer field has no start cue:\n%s", v)
		}
	})
}
