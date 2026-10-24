// SPDX-License-Identifier: GPL-3.0-or-later

// Package brand draws ork's terminal text mark.
package brand

import (
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

var pixels = [12]string{
	".....TT.....", "....T..T....", "....T..T....", "....TTTT....",
	".....TT.....", ".....TT.....", ".....TT.....", ".TTTTTTTTTT.",
	".T...TT...T.", ".BB..VV..OO.", ".BB..VV..OO.", ".BB..VV..OO.",
}
var colours = map[byte]string{'T': "#6ee7d2", 'B': "#3b82f6", 'V': "#8b5cf6", 'O': "#ea580c"}

// Mark returns six rows of twelve terminal cells. ASCII uses the coarser,
// uncoloured fallback; Unicode cells preserve both pixel colours.
func Mark(colour, ascii bool) []string {
	if ascii {
		return []string{"    .--.    ", "    |  |    ", "    '--'    ", "     ||     ", " +---||---+ ", " ##  ##  ## "}
	}
	rows := make([]string, 6)
	for r := range rows {
		for c := 0; c < 12; c++ {
			top, bottom := pixels[2*r][c], pixels[2*r+1][c]
			glyph := " "
			fg, bg := top, byte('.')
			switch {
			case top == '.' && bottom == '.':
			case top == '.':
				glyph = "▄"
				fg = bottom
			case bottom == '.':
				glyph = "▀"
			case top == bottom:
				glyph = "█"
			default:
				glyph = "▀"
				bg = bottom
			}
			if colour && fg != '.' {
				style := lipgloss.NewStyle().Foreground(lipgloss.Color(colours[fg]))
				if bg != '.' {
					style = style.Background(lipgloss.Color(colours[bg]))
				}
				glyph = style.Render(glyph)
			}
			rows[r] += glyph
		}
	}
	return rows
}

// ASCIILocale follows locale precedence for character encoding.
func ASCIILocale(getenv func(string) string) bool {
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if value := getenv(key); value != "" {
			value = strings.ToLower(value)
			return !strings.Contains(value, "utf-8") && !strings.Contains(value, "utf8")
		}
	}
	return true
}

// ColourWanted enables colour only on character-device stdout without NO_COLOR.
func ColourWanted(w io.Writer, getenv func(string) string) bool {
	if getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
