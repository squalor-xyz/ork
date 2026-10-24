// SPDX-License-Identifier: GPL-3.0-or-later

package brand

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestMarkRowsAreTwelveCells(t *testing.T) {
	for _, colour := range []bool{false, true} {
		for _, ascii := range []bool{false, true} {
			rows := Mark(colour, ascii)
			if len(rows) != 6 {
				t.Fatalf("rows = %d", len(rows))
			}
			for _, row := range rows {
				if lipgloss.Width(row) != 12 {
					t.Fatalf("width: %q", row)
				}
			}
		}
	}
}
func TestMarkMonoHasNoEscapeCodes(t *testing.T) {
	if strings.Contains(strings.Join(Mark(false, false), ""), "\x1b") {
		t.Fatal("mono has ESC")
	}
}
func TestMarkColourUsesSliceColours(t *testing.T) {
	s := strings.Join(Mark(true, false), "")
	for _, code := range []string{"38;2;110;231;210", "38;2;59;130;246", "38;2;139;92;246", "38;2;234;88;12", "48;2;59;130;246", "48;2;139;92;246", "48;2;234;88;12"} {
		if !strings.Contains(s, code) {
			t.Errorf("missing %s", code)
		}
	}
}
func TestMarkMatchesPinnedGlyph(t *testing.T) {
	want := []string{"    ▄▀▀▄    ", "    █▄▄█    ", "     ██     ", " ▄▄▄▄██▄▄▄▄ ", " ▀▄  ▀▀  ▄▀ ", " ██  ██  ██ "}
	if strings.Join(Mark(false, false), "\n") != strings.Join(want, "\n") {
		t.Fatalf("glyph: %q", Mark(false, false))
	}
}
func TestMarkASCIIIsASCII(t *testing.T) {
	want := []string{"    .--.    ", "    |  |    ", "    '--'    ", "     ||     ", " +---||---+ ", " ##  ##  ## "}
	for _, colour := range []bool{false, true} {
		rows := Mark(colour, true)
		if strings.Join(rows, "\n") != strings.Join(want, "\n") {
			t.Fatalf("ascii: %q", rows)
		}
		for _, row := range rows {
			for _, b := range []byte(row) {
				if b >= 128 {
					t.Fatal("non ASCII")
				}
			}
		}
	}
}
func TestMarkASCIIWhenLocaleIsNotUTF8(t *testing.T) {
	for _, tc := range []struct {
		all, ctype, lang string
		want             bool
	}{
		{"", "", "", true}, {"C", "", "en_US.UTF-8", true}, {"", "C", "en_US.UTF-8", true},
		{"", "", "en_US.UTF-8", false}, {"", "en_US.utf8", "C", false}, {"en_US.UTF-8", "C", "C", false},
	} {
		env := map[string]string{"LC_ALL": tc.all, "LC_CTYPE": tc.ctype, "LANG": tc.lang}
		if got := ASCIILocale(func(k string) string { return env[k] }); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}
func TestColourWanted(t *testing.T) {
	env := func(string) string { return "" }
	if ColourWanted(&bytes.Buffer{}, env) {
		t.Fatal("buffer colour")
	}
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if ColourWanted(f, env) {
		t.Fatal("regular file colour")
	}
	if ColourWanted(os.Stdout, func(string) string { return "1" }) {
		t.Fatal("NO_COLOR ignored")
	}
}
