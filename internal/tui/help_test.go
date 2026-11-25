// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func helpHarness(t *testing.T) *harness {
	t.Helper()
	h, _ := slicerTuiHarness(t)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.deliver(press("?"))
	return h
}

func helpBody(h *harness) string {
	lines := strings.Split(plainView(h.m), "\n")
	var body []string
	for _, line := range lines[2 : len(lines)-3] {
		_, main, _ := strings.Cut(line, "│")
		body = append(body, strings.TrimSpace(main))
	}
	return strings.Join(body, "\n")
}

func TestWelcomeHelpScrollsAndReopens(t *testing.T) {
	for _, close := range []string{"?", "esc"} {
		t.Run(close, func(t *testing.T) {
			h, _ := welcomeHarness(t)
			h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
			h.deliver(press("?"))
			if got := hintLine(plainView(h.m)); got != "up/down pgup/pgdown scroll; ? or esc close help; q quit" {
				t.Errorf("Welcome scroll hint: %q", got)
			}
			for _, k := range []string{"down", "j"} {
				h.deliver(press(k))
				if body := helpBody(h); !strings.HasPrefix(body, "+1 lines above\n") {
					t.Fatalf("Welcome %s did not scroll: %s", k, body)
				}
				up := "up"
				if k == "j" {
					up = "k"
				}
				h.deliver(press(up))
				if body := helpBody(h); !strings.HasPrefix(body, "Keys\n") {
					t.Fatalf("Welcome %s did not return to top: %s", up, body)
				}
			}
			h.deliver(tea.KeyPressMsg{Code: tea.KeyPgDown})
			if body := helpBody(h); strings.Contains(body, "more lines") || !strings.Contains(body, "lines above") {
				t.Fatalf("Welcome pgdown did not reach end: %s", body)
			}
			h.deliver(tea.KeyPressMsg{Code: tea.KeyPgUp})
			if body := helpBody(h); !strings.HasPrefix(body, "Keys\n") {
				t.Fatalf("Welcome pgup did not return to top: %s", body)
			}
			h.deliver(press("down"))
			h.deliver(press(close))
			h.deliver(press("?"))
			if body := helpBody(h); !strings.HasPrefix(body, "Keys\n") {
				t.Fatalf("Welcome reopened below top: %s", body)
			}
		})
	}
}

func TestHelpPagesCoverEveryLineAtShortHeight(t *testing.T) {
	h := helpHarness(t)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 16})
	want := h.m.helpLines(60, 11)
	for _, direction := range []struct {
		name   string
		code   rune
		marker string
	}{
		{"down", tea.KeyPgDown, "more lines"}, {"up", tea.KeyPgUp, "lines above"},
	} {
		seen := map[string]bool{}
		for frame := 0; ; frame++ {
			body := helpBody(h)
			t.Logf("%s frame %d:\n%s", direction.name, frame, body)
			for _, line := range strings.Split(body, "\n") {
				seen[line] = true
			}
			if !strings.Contains(body, direction.marker) {
				break
			}
			if frame == 10 {
				t.Fatal("paging did not reach endpoint")
			}
			h.deliver(tea.KeyPressMsg{Code: direction.code})
		}
		for _, line := range want {
			line = strings.TrimSpace(sgr.ReplaceAllString(line, ""))
			if line != "" && !seen[line] {
				t.Errorf("page %s skipped help line %q", direction.name, line)
			}
		}
	}
}

func TestHelpWrapsBetweenHintsFirst(t *testing.T) {
	h := helpHarness(t)
	h.m.view = 0
	lines := h.m.helpLines(72, 25)
	if !strings.Contains(strings.Join(lines, "\n"), "E expand or collapse all") {
		t.Error("E key split from its label at 100x30")
	}
	h.m.view = roadmapView
	if !strings.Contains(strings.Join(h.m.helpLines(60, 19), "\n"), "n new slice") {
		t.Error("n key split from its label at 80x24")
	}
}

func TestOverviewHelpFitsAt100x30(t *testing.T) {
	h := helpHarness(t)
	h.m.view = 0
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	body := helpBody(h)
	if strings.Contains(body, "more lines") || !strings.HasSuffix(body, "Overview, Questions, Waves, Roadmap and Decisions are available.") {
		t.Fatalf("Overview help does not fit at 100x30: %s", body)
	}
}

func TestHelpScrollsByLineAndPage(t *testing.T) {
	h := helpHarness(t)
	if body := helpBody(h); !strings.HasPrefix(body, "Keys\n") || !strings.Contains(body, "more lines") {
		t.Fatalf("initial help window: %s", body)
	}
	for _, k := range []string{"down", "j"} {
		h.deliver(press(k))
		if body := helpBody(h); !strings.HasPrefix(body, "+1 lines above\n") {
			t.Fatalf("%s did not scroll one line: %s", k, body)
		}
		up := "up"
		if k == "j" {
			up = "k"
		}
		h.deliver(press(up))
		if body := helpBody(h); !strings.HasPrefix(body, "Keys\n") {
			t.Fatalf("%s did not return to top: %s", up, body)
		}
	}
	h.deliver(tea.KeyPressMsg{Code: tea.KeyPgDown})
	lastStart := len(h.m.helpLines(60, 19)) - 18 // final page has one marker and 18 content lines
	if body := helpBody(h); !strings.HasPrefix(body, fmt.Sprintf("+%d lines above\n", min(18, lastStart))) {
		t.Fatalf("pgdown did not move by visible content height: %s", body)
	}
	for i := 0; i < 4; i++ {
		h.deliver(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	body := helpBody(h)
	last := h.m.helpLines(60, 19)
	if strings.Contains(body, "more lines") || !strings.HasSuffix(body, strings.TrimSpace(sgr.ReplaceAllString(last[len(last)-1], ""))) {
		t.Fatalf("last page does not end flush with final help line: %s", body)
	}
	h.deliver(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if got := helpBody(h); got != body {
		t.Fatalf("pgdown escaped end: %s", got)
	}
	for i := 0; i < 5; i++ {
		h.deliver(tea.KeyPressMsg{Code: tea.KeyPgUp})
	}
	if body := helpBody(h); !strings.HasPrefix(body, "Keys\n") || strings.Contains(body, "lines above") {
		t.Fatalf("pgup did not return to top: %s", body)
	}
	h.deliver(press("up"))
	if body := helpBody(h); !strings.HasPrefix(body, "Keys\n") {
		t.Fatalf("up escaped top: %s", body)
	}
}

func TestHelpReopensAtTheTop(t *testing.T) {
	for _, close := range []string{"?", "esc"} {
		t.Run(close, func(t *testing.T) {
			h := helpHarness(t)
			h.deliver(press("down"))
			if body := helpBody(h); !strings.HasPrefix(body, "+1 lines above\n") {
				t.Fatalf("scroll precondition failed: %s", body)
			}
			h.deliver(press(close))
			h.deliver(press("?"))
			if body := helpBody(h); !strings.HasPrefix(body, "Keys\n") {
				t.Fatalf("reopened below top: %s", body)
			}
		})
	}
}

func TestHelpLinesWrapInsteadOfClipping(t *testing.T) {
	h := helpHarness(t)
	lines := h.m.helpLines(60, 19)
	for i, line := range lines {
		if lipgloss.Width(line) > 60 {
			t.Errorf("line %d exceeds 60 columns: %q", i, line)
		}
		if strings.HasSuffix(line, "...") {
			t.Errorf("line %d clipped: %q", i, line)
		}
	}
	text := strings.Join(strings.Fields(sgr.ReplaceAllString(strings.Join(lines, " "), "")), " ")
	for _, want := range []string{"open run, question, item or decision; other rows select project", "l launch a ready slice (Roadmap)"} {
		if !strings.Contains(text, want) {
			t.Errorf("full help label missing: %q", want)
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "\n  other rows select project") {
		t.Error("wrapped enter label lacks two-space hang")
	}
}

func TestHelpScrollDoesNotMoveTheRowCursor(t *testing.T) {
	h := helpHarness(t)
	cursor := h.m.roadmapCursor
	for i := 0; i < 3; i++ {
		h.deliver(press("down"))
	}
	h.deliver(press("?"))
	if h.m.roadmapCursor != cursor {
		t.Fatalf("help scrolling moved row cursor: %d -> %d", cursor, h.m.roadmapCursor)
	}
}

func TestHelpHintNamesTheScrollKeys(t *testing.T) {
	h := helpHarness(t)
	if got := hintLine(plainView(h.m)); got != "up/down pgup/pgdown scroll; ? or esc close help; q quit" {
		t.Errorf("small help hint: %q", got)
	}
	h.send(tea.WindowSizeMsg{Width: 200, Height: 60})
	if got := hintLine(plainView(h.m)); got != "? or esc close help; q quit" {
		t.Errorf("fitting help hint: %q", got)
	}
}
