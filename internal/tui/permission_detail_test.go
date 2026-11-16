// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// permissionDetail is a 300-character command of single-spaced words, so the
// wrapped lines join back to it with a space.
func permissionDetail() string {
	text := "curl -s https://example.test/install.sh | sh -s -- --prefix /opt/tool"
	for i := 1; len(text) < 300; i++ {
		text += fmt.Sprintf(" --flag%03d=value%03d", i, i)
	}
	return strings.TrimSpace(text[:300])
}

// permissionPane opens the run pane of a pending permission question whose
// detail is detail, in a terminal of w by h.
func permissionPane(t *testing.T, detail string, w, ht int) *harness {
	t.Helper()
	h := actionHarness(t, "implement", "permission")
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns[0].Question = &runrec.OwnerQuestion{Title: "Allow Bash call 3?", Detail: detail, Options: []runrec.Option{{Label: "Allow", Summary: "Allow this call once"}, {Label: "Deny", Summary: "Deny this call"}}, Recommend: 2, Reason: "not allowlisted"}
	h.src.set(s)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.m.view = questionsView
	h.deliver(press("enter"))
	h.m.Update(tea.WindowSizeMsg{Width: w, Height: ht})
	return h
}

// underTitleRaw are the view lines between the question title and its first
// option or marker, after the sidebar rule, with their leading spaces. The
// pane's own indent is the same on every line.
func underTitleRaw(t *testing.T, view string) []string {
	t.Helper()
	var out []string
	in := false
	for _, line := range strings.Split(view, "\n") {
		if _, after, ok := strings.Cut(line, "│"); ok { // the sidebar rule
			line = after
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case in && (strings.HasPrefix(trimmed, "1. Allow") || strings.HasPrefix(trimmed, "+")):
			return out
		case in:
			out = append(out, line)
		case trimmed == "Allow Bash call 3?":
			in = true
		}
	}
	t.Fatalf("no title followed by option 1 in:\n%s", view)
	return nil
}

// underTitle is underTitleRaw without the spaces around each line.
func underTitle(t *testing.T, view string) []string {
	t.Helper()
	lines := underTitleRaw(t, view)
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return lines
}

func leading(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// A line of the input that wraps continues under a two-space hanging indent;
// a line at the margin is a new line of the input (adj 378).
func TestPermissionDetailHangingIndent(t *testing.T) {
	long := "curl -s https://example.test/install.sh | sh -s -- --prefix /opt/tool --verbose --retry 3 --output /var/tmp/result.txt"
	detail := long + "\necho done"
	view := plainView(permissionPane(t, detail, 80, 40).m)
	lines := underTitleRaw(t, view)
	if len(lines) < 3 {
		t.Fatalf("the long line did not wrap: %q\n%s", lines, view)
	}
	base := leading(lines[0])
	for _, line := range lines[1 : len(lines)-1] {
		if leading(line) != base+2 {
			t.Errorf("a wrapped line is not under a 2-space hanging indent: %q", line)
		}
	}
	if last := lines[len(lines)-1]; strings.TrimSpace(last) != "echo done" || leading(last) != base {
		t.Errorf("a real new line of the input is not at the margin: %q", last)
	}
	if got := strings.Join(underTitle(t, view), " "); got != strings.ReplaceAll(detail, "\n", " ") {
		t.Errorf("the hanging indent changed the text:\ngot  %q", got)
	}
}

// A detail too tall for the footer drops whole, and a line says so; the options
// stay visible.
func TestPermissionDetailOverflowMarker(t *testing.T) {
	var script []string
	for i := 1; i <= 25; i++ {
		script = append(script, fmt.Sprintf("echo line%02d && rm -rf /tmp/x%02d", i, i))
	}
	h := permissionPane(t, strings.Join(script, "\n"), 80, 24)
	view := plainView(h.m)
	for _, want := range []string{"+25 detail lines hidden; ork show engine", "1. Allow", "2. Deny"} {
		if !strings.Contains(view, want) {
			t.Errorf("the pane lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "echo line01") {
		t.Errorf("a detail that does not fit is shown in part:\n%s", view)
	}
	checkGolden(t, "pane-permission-detail-overflow-80x24.txt", view)
	// A detail that fits has no marker.
	if view := plainView(permissionPane(t, permissionDetail(), 100, 30).m); strings.Contains(view, "hidden") {
		t.Errorf("a shown detail has a hidden marker:\n%s", view)
	}
}

func TestPermissionQuestionShowsDetail(t *testing.T) {
	detail := permissionDetail()
	if len(detail) != 300 {
		t.Fatalf("test detail is %d characters", len(detail))
	}
	for _, c := range []struct {
		name string
		w, h int
	}{{"pane-permission-detail-100x30.txt", 100, 30}, {"pane-permission-detail-80x24.txt", 80, 24}} {
		t.Run(c.name, func(t *testing.T) {
			h := permissionPane(t, detail, c.w, c.h)
			view := plainView(h.m)
			lines := underTitle(t, view)
			if got := strings.Join(lines, " "); got != detail {
				t.Fatalf("the detail is not shown whole under the title:\ngot  %q\nwant %q\n%s", got, detail, view)
			}
			if len(lines) < 3 {
				t.Errorf("a 300-character detail is not wrapped at the pane width: %q", lines)
			}
			for _, line := range lines {
				if len(line) > c.w-len(indent) {
					t.Errorf("detail line is wider than the pane: %q", line)
				}
			}
			checkGolden(t, c.name, view)
		})
	}
}

func TestQuestionWithoutDetailUnchanged(t *testing.T) {
	h := permissionPane(t, "", 100, 30)
	view := plainView(h.m)
	if lines := underTitle(t, view); len(lines) != 0 {
		t.Fatalf("a question with no detail shows lines between its title and options: %q\n%s", lines, view)
	}
	checkGolden(t, "pane-permission-no-detail-100x30.txt", view)
}
