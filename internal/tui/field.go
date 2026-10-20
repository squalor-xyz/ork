// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// textField is a single line, edited at a rune cursor. Paste never submits.
type textField struct {
	text   []rune
	cursor int
}

func (f *textField) update(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		f.insert(msg.Content)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "backspace":
			if f.cursor > 0 {
				f.text = append(f.text[:f.cursor-1], f.text[f.cursor:]...)
				f.cursor--
			}
		case "ctrl+u":
			f.text = nil
			f.cursor = 0
		case "left":
			f.cursor = max(f.cursor-1, 0)
		case "right":
			f.cursor = min(f.cursor+1, len(f.text))
		default:
			f.insert(msg.Text)
		}
	}
}

func (f *textField) insert(text string) {
	// A pasted CR/LF belongs to the same answer, never a second input line.
	text = strings.NewReplacer("\r", "", "\n", "").Replace(text)
	r := []rune(text)
	tail := append([]rune(nil), f.text[f.cursor:]...)
	f.text = append(append(f.text[:f.cursor], r...), tail...)
	f.cursor += len(r)
}

func (f textField) Value() string { return string(f.text) }

func (f textField) view(width int, th Theme) string {
	// Keep the cursor visible as a reversed character, including at the end.
	cursor, end := " ", f.cursor
	if end < len(f.text) {
		cursor = string(f.text[end])
		end++
	}
	start := 0
	for start < f.cursor && lipgloss.Width(string(f.text[start:f.cursor]))+lipgloss.Width(cursor) > width {
		start++
	}
	text := th.Title.Render(string(f.text[start:f.cursor])) + th.TabActive.Render(cursor) + th.Title.Render(string(f.text[end:]))
	return lipgloss.NewStyle().MaxWidth(width).Render(text)
}
