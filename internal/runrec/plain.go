// SPDX-License-Identifier: GPL-3.0-or-later

package runrec

import (
	"regexp"
	"strings"
)

// Terminal control sequences: CSI (colors, cursor moves) and OSC (window title,
// links). They go whole, so no stray "[31m" is left in the text.
var (
	csiSeq = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	oscSeq = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
)

// PlainText is s as text that is safe to draw in a terminal (S121,
// *proposed*): line ends become "\n", a tab becomes four spaces, invalid
// UTF-8 becomes "?", and every other control character is dropped, so an
// escape sequence (CSI or OSC) in a role's file goes whole. Trailing
// blank lines and line-end spaces are trimmed.
func PlainText(s string) string {
	s = strings.ToValidUTF8(s, "?")
	s = oscSeq.ReplaceAllString(csiSeq.ReplaceAllString(s, ""), "")
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", "    ").Replace(s)
	s = strings.Map(func(r rune) rune {
		if r == '\n' || (r >= 0x20 && r != 0x7f && (r < 0x80 || r > 0x9f)) {
			return r
		}
		return -1
	}, s)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
