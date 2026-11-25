// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/brand"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// sweepMarker matches the scroll marker lines a clipped view draws. A marker
// is not content: the reference never holds one and a frame is not asked to.
var sweepMarker = regexp.MustCompile(`^\+\d+ (lines above|more lines)$`)

// sweepContent trims each line and drops blank lines and marker lines.
func sweepContent(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !sweepMarker.MatchString(line) {
			out = append(out, line)
		}
	}
	return out
}

// sweepMissing returns the want lines that no frame holds, in want order. It
// checks distinct lines, not each copy: a line that appears twice in want is
// shown once any frame holds it. A view whose reference repeats a line, such as
// the identical group headers of a row list, hides a skip of one copy, so the
// fixtures for such views keep their lines unique (sweepCase.unique).
func sweepMissing(want, frames []string) []string {
	seen := map[string]bool{}
	for _, frame := range frames {
		for _, line := range sweepContent(frame) {
			seen[line] = true
		}
	}
	var missing []string
	for _, line := range sweepContent(strings.Join(want, "\n")) {
		if !seen[line] && !slices.Contains(missing, line) {
			missing = append(missing, line)
		}
	}
	return missing
}

// The sweep must be able to fail: frames that skip a line name that line, and
// frames that hold every line, scroll markers included, name nothing.
func TestSweepMissingNamesSkippedLines(t *testing.T) {
	want := []string{"alpha", "", "beta", "gamma", "delta"}
	skipped := []string{"alpha\nbeta", "beta\ndelta"}
	if got := sweepMissing(want, skipped); !slices.Equal(got, []string{"gamma"}) {
		t.Errorf("skipped frames: missing %q, want [gamma]", got)
	}
	whole := []string{
		"alpha\nbeta\n+2 more lines",
		"+1 lines above\nbeta\ngamma\n+1 more lines",
		"+3 lines above\ndelta",
	}
	if got := sweepMissing(want, whole); len(got) != 0 {
		t.Errorf("whole frames: missing %q, want none", got)
	}
	// A marker line in the reference is not content a frame must show.
	if got := sweepMissing([]string{"alpha", "+4 more lines"}, []string{"alpha"}); len(got) != 0 {
		t.Errorf("marker in want: missing %q, want none", got)
	}
}

// sweepLines is the main column of the current frame: the lines between the
// tab bar and the footer, as helpBody cuts them, with the sidebar cut at the
// first vertical bar. The footer hint changes with the view's scroll state and
// the brand mark shows only when the whole body fits, so neither is content.
func sweepLines(h *harness) []string {
	lines := strings.Split(plainView(h.m), "\n")
	var body []string
	for _, line := range lines[2 : len(lines)-3] {
		if _, main, ok := strings.Cut(line, "│"); ok {
			line = main
		}
		body = append(body, line)
	}
	return sweepNoMark(h, body)
}

// sweepNoMark trims the lines and drops those of the brand mark.
func sweepNoMark(h *harness, lines []string) []string {
	mark := map[string]bool{}
	for _, line := range brand.Mark(false, h.m.cfg.ASCII) {
		mark[strings.TrimSpace(sgr.ReplaceAllString(line, ""))] = true
	}
	var out []string
	for _, line := range lines {
		if line = strings.TrimSpace(sgr.ReplaceAllString(line, "")); !mark[line] {
			out = append(out, line)
		}
	}
	return out
}

// sweepPass sends key until the frame stops changing and returns every frame it
// saw, the first included. It fails the test when the end is not reached within
// limit frames.
func sweepPass(t *testing.T, h *harness, key tea.KeyPressMsg, limit int) []string {
	t.Helper()
	frames := []string{strings.Join(sweepLines(h), "\n")}
	for {
		h.deliver(key)
		frame := strings.Join(sweepLines(h), "\n")
		if frame == frames[len(frames)-1] {
			return frames
		}
		frames = append(frames, frame)
		if len(frames) > limit {
			t.Fatalf("paging did not reach its end within %d frames", limit)
		}
	}
}

// sweepHelpReference is the whole help body at the window's current size, from
// helpLines as TestHelpPagesCoverEveryLineAtShortHeight takes it. The help
// wraps its key lines by body height, so a render at another height holds
// other lines; this is the content the window at its own height has to show.
func sweepHelpReference(h *harness) []string {
	w, height, sideW := h.m.layoutSize()
	return sweepNoMark(h, h.m.helpLines(max(w-sideW, 1), max(height-4, 1)))
}

// sweepCase is one scrollable view of the sweep.
type sweepCase struct {
	name    string
	heights []int
	setup   func(*testing.T) *harness // a fresh harness for every height
	// reference is the content the view must show. Nil takes the view's own
	// render at height 200, which holds the whole body with no marker.
	reference func(*harness) []string
	// unique fails the case when its reference holds a line twice, because
	// sweepMissing would let a skipped copy pass.
	unique           bool
	pageDown, pageUp tea.KeyPressMsg
}

// sweep pages the view to its end and back at each of its heights, width 80,
// and fails when either pass skips a line of the reference.
func (c sweepCase) sweep(t *testing.T) {
	for _, height := range c.heights {
		t.Run(fmt.Sprintf("%s/h%d", c.name, height), func(t *testing.T) {
			h := c.setup(t)
			var reference []string
			if c.reference == nil {
				h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 200})
				reference = sweepLines(h)
				for _, line := range reference {
					if sweepMarker.MatchString(line) {
						t.Fatalf("the reference at height 200 is clipped (%q)", line)
					}
				}
			}
			h.m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
			if c.reference != nil {
				reference = c.reference(h)
			}
			if c.unique {
				seen := map[string]bool{}
				for _, line := range sweepContent(strings.Join(reference, "\n")) {
					if seen[line] {
						t.Fatalf("%s: the reference holds %q twice, so a skipped copy would pass", c.name, line)
					}
					seen[line] = true
				}
			}
			limit := 3*len(reference) + 5
			// A row list may initially follow its cursor below the Goals.
			// Keep the frames reaching the true top: pgdown ends that lift
			// and resumes paging from the cursor (S580, adj 582).
			top := sweepPass(t, h, c.pageUp, limit)
			down := append(top, sweepPass(t, h, c.pageDown, limit)...)
			up := sweepPass(t, h, c.pageUp, limit)
			for _, pass := range []struct {
				direction string
				frames    []string
			}{{"down", down}, {"up", up}} {
				if missing := sweepMissing(reference, pass.frames); len(missing) > 0 {
					t.Errorf("%s at height %d, paging %s, never showed %q", c.name, height, pass.direction, missing)
				}
			}
		})
	}
}

var (
	sweepPgDown = tea.KeyPressMsg{Code: tea.KeyPgDown}
	sweepPgUp   = tea.KeyPressMsg{Code: tea.KeyPgUp}
	sweepAll    = []int{10, 16, 24, 40}
)

// TestScrollSweep pages every scrollable view reachable from launchHarness to
// its end and back at heights 10, 16, 24 and 40, and fails when a line of the
// view never appears (S531, adj 570). A page-up that skips a line at 80x16
// passed the 80x24 tests until the UI pass found it by hand (S401).
//
// Left out, as the owner decided (adj 570, adj 575): the sweep found a real
// paging defect in each, so the view or height stays out until its follow-up
// slice lands and then joins this table.
//   - Roadmap item pane (openLongItemPane, pgdown and pgup), all heights: a
//     page moves the offset by the whole room, but the draw spends one line of
//     it on the "+N more lines" marker, so each page skips the line under the
//     marker (roadmap.go scrollRoadmapPane). Follow-up: S579.
func TestScrollSweep(t *testing.T) {
	cases := []sweepCase{
		{name: "help-running", heights: sweepAll, setup: sweepHelp(0), reference: sweepHelpReference, pageDown: sweepPgDown, pageUp: sweepPgUp},
		{name: "help-waves", heights: sweepAll, setup: sweepHelp(wavesView), reference: sweepHelpReference, pageDown: sweepPgDown, pageUp: sweepPgUp},
		{name: "help-roadmap", heights: sweepAll, setup: sweepHelp(roadmapView), reference: sweepHelpReference, pageDown: sweepPgDown, pageUp: sweepPgUp},
		{name: "help-decisions", heights: sweepAll, setup: sweepHelp(decisionsView), reference: sweepHelpReference, pageDown: sweepPgDown, pageUp: sweepPgUp},
		{name: "help-questions", heights: sweepAll, setup: sweepHelp(questionsView), reference: sweepHelpReference, pageDown: sweepPgDown, pageUp: sweepPgUp},
		{name: "roadmap-list", heights: sweepAll, unique: true, setup: func(t *testing.T) *harness {
			h, _ := launchHarness(t)
			h.m.view = roadmapView
			// The four fixture rows fit the window from height 16, so add
			// enough rows for the page keys to have pages to move over. All
			// of them share one pass, so each group header is one line only.
			s := h.m.projects[0].snap
			for i := range 20 {
				s.Items.Value = append(s.Items.Value, slicercli.Item{
					ID: fmt.Sprintf("S%d", 100+i), Title: fmt.Sprintf("Extra row %d", i), Status: "open", HasSlice: true,
					Fields: slicercli.Fields{Pass: "v1.2", Size: "S", Effort: 1},
				})
			}
			h.m.projects[0].snap = s
			return h
		}, pageDown: sweepPgDown, pageUp: sweepPgUp},
		{name: "launch-confirm", heights: sweepAll, setup: func(t *testing.T) *harness {
			h, l := launchHarness(t)
			for i := 1; i <= 8; i++ {
				l.plan.Warnings = append(l.plan.Warnings, fmt.Sprintf("warning number %d", i))
			}
			launchConfirmed(t, h)
			return h
		}, pageDown: press("down"), pageUp: press("up")},
	}
	for _, c := range cases {
		c.sweep(t)
	}
}

// sweepHelp opens the help overlay on a tab view.
func sweepHelp(view int) func(*testing.T) *harness {
	return func(t *testing.T) *harness {
		h, _ := launchHarness(t)
		h.m.view = view
		h.deliver(press("?"))
		return h
	}
}
