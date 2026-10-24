// SPDX-License-Identifier: GPL-3.0-or-later
package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/brand"
)

func TestHeaderShowsVersion(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, v := range []string{"dev", "123456789012"} {
			m := goldenModel(NewTheme(mono), 80, 24)
			m.cfg.Version = v
			got := stripANSI(m.sidebar(20, 24)[0])
			want := "ork " + v[:min(len(v), 8)] + "  ?help"
			if !strings.HasPrefix(got, want) || lipgloss.Width(got) != 20 {
				t.Fatalf("header %q want %q", got, want)
			}
		}
	}
}
func TestStaleDaemonWarningInStatusLine(t *testing.T) {
	for _, tc := range []struct {
		own, theirs string
		warn        bool
	}{
		{"a", "a", false}, {"a", "b", true}, {"a", "unknown", true}, {"a", "", true}, {"", "", false},
	} {
		m := goldenModel(NewTheme(true), 80, 24)
		m.cfg.BuildID = tc.own
		m.cfg.DaemonBuild = tc.theirs
		line := stripANSI(m.statusLine(60))
		if strings.Contains(line, "daemon build differs: ork daemon restart") != tc.warn {
			t.Fatalf("%+v: %q", tc, line)
		}
		if tc.warn && !strings.HasPrefix(line, " daemon build differs: ork daemon restart") {
			t.Fatal(line)
		}
		if lipgloss.Width(line) > 60 {
			t.Fatal("status too wide")
		}
	}
}
func TestHeaderVersionMovesToStatusWhenSidebarHidden(t *testing.T) {
	m := goldenModel(NewTheme(true), 100, 30)
	m.cfg.Version = "v1"
	if strings.Contains(stripANSI(m.statusLine(72)), "v1") {
		t.Fatal("version with sidebar")
	}
	m.sidebarHidden = true
	if s := stripANSI(m.statusLine(100)); !strings.HasSuffix(s, "v1") || lipgloss.Width(s) != 100 {
		t.Fatalf("%q", s)
	}
	m.cfg.Version = strings.Repeat("x", 100)
	if strings.Contains(stripANSI(m.statusLine(100)), "xxx") {
		t.Fatal("crowded version not dropped")
	}
}
func TestHelpStartsWithMark(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, ascii := range []bool{false, true} {
			m := goldenModel(NewTheme(mono), 100, 30)
			m.cfg.ASCII = ascii
			base := m.helpLines(72, 0)
			h := len(base) + 7
			rows := m.helpLines(72, h)
			want := brand.Mark(!mono, ascii)
			if strings.Join(rows[:6], "\n") != strings.Join(want, "\n") || len(rows) != h {
				t.Fatalf("help mark: %q", rows)
			}
			if len(m.helpLines(72, h-1)) != len(base) || len(m.helpLines(11, h)) != len(base) {
				t.Fatal("mark without room")
			}
		}
	}
}
func TestHelpKeepsQuitVisible(t *testing.T) {
	m := goldenModel(NewTheme(true), 100, 30)
	m.showHelp = true
	if !strings.Contains(stripANSI(m.screen()), "q quit") {
		t.Fatal(m.screen())
	}
	if strings.Contains(m.screen(), "▄") {
		t.Fatal("mark at 100x30")
	}
}

func stripANSI(s string) string { return sgr.ReplaceAllString(s, "") }

func TestHeaderDropsVersionBeforeHelp(t *testing.T) {
	m := goldenModel(NewTheme(true), 80, 24)
	m.cfg.Version = "abcdefghijkl"
	if got := stripANSI(m.sidebar(12, 24)[0]); got != "ork  ?help |" {
		t.Fatalf("%q", got)
	}
}

func TestGoldenVersionAndHelp(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, mode := range []string{"help-tall", "help-100x30", "stale", "hidden"} {
			t.Run(fmt.Sprintf("%s-mono-%t", mode, mono), func(t *testing.T) {
				m := goldenModel(NewTheme(mono), 100, 30)
				switch mode {
				case "help-tall":
					m.showHelp = true
					m.height = len(m.helpLines(72, 0)) + 7 + 4
				case "help-100x30":
					m.showHelp = true
				case "stale":
					m.width = 80
					m.height = 24
					m.cfg.BuildID = "new"
					m.cfg.DaemonBuild = "old"
				case "hidden":
					m.sidebarHidden = true
				}
				checkGolden(t, fmt.Sprintf("version-%s-mono-%t.txt", mode, mono), m.screen())
			})
		}
	}
}

func TestStaleStatusDropsWholeFieldsAt80x24(t *testing.T) {
	for _, mono := range []bool{false, true} {
		m := goldenModel(NewTheme(mono), 80, 24)
		m.cfg.BuildID = "new"
		m.cfg.DaemonBuild = "old"
		got := stripANSI(m.statusLine(60))
		want := " daemon build differs: ork daemon restart | 4 projects"
		if got != want {
			t.Fatalf("status = %q, want %q", got, want)
		}
	}
}

func TestStatusLineUsesSuppliedWidth(t *testing.T) {
	m := goldenModel(NewTheme(true), 100, 30)
	for _, tc := range []struct {
		width int
		want  string
	}{
		{30, " 4 projects"},
		{15, " 4 projects"},
	} {
		if got := stripANSI(m.statusLine(tc.width)); got != tc.want {
			t.Fatalf("width %d: %q, want %q", tc.width, got, tc.want)
		}
	}
	m.cfg.BuildID = "new"
	m.cfg.DaemonBuild = "old"
	if got := stripANSI(m.statusLine(20)); got != " daemon build differ" {
		t.Fatalf("clipped warning: %q", got)
	}
	m.sidebarHidden = true
	m.cfg.Version = "v1"
	if got := stripANSI(m.statusLine(60)); !strings.HasSuffix(got, "v1") || lipgloss.Width(got) != 60 {
		t.Fatalf("aligned version: %q", got)
	}
}
