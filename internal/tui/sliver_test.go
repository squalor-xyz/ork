// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/source"
)

// The three slivers of one chain: the first is committed, the second runs and
// the third waits.
const (
	sliverA = `{"id":"run-a","item":"S1a","state":"committed","branch":"S1","parent":"S1","chain":"chain-1","sliver_index":1,"sliver_count":3,"created_at":"2026-10-19T16:30:00Z","roles":[{"role":"implementer","round":1,"vendor":"claude","model":"haiku","started_at":"2026-10-19T16:30:00Z","ended_at":"2026-10-19T16:35:00Z","exit":"ok"}]}`
	sliverB = `{"id":"run-b","item":"S1b","state":"implement","branch":"S1","parent":"S1","chain":"chain-1","sliver_index":2,"sliver_count":3,"created_at":"2026-10-19T16:40:00Z","roles":[{"role":"implementer","round":1,"vendor":"claude","model":"haiku","started_at":"2026-10-19T16:40:00Z"}]}`
	sliverC = `{"id":"run-c","item":"S1c","state":"queued","branch":"S1","parent":"S1","chain":"chain-1","sliver_index":3,"sliver_count":3,"created_at":"2026-10-19T16:40:00Z"}`
)

// sliverHarness shows the engine runs and opens the pane of the run named.
func sliverHarness(t *testing.T, open string, runs ...string) *harness {
	t.Helper()
	h := newHarness(t, proj("ork", false))
	s := snap(proj("ork", false), nil, nil)
	for _, raw := range runs {
		var r source.EngineRun
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatal(err)
		}
		s.EngineRuns = append(s.EngineRuns, r)
	}
	h.src.set(s)
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if open != "" {
		h.m.pane = &runPane{run: open, project: "ork"}
	}
	return h
}

func sliverLine(text, want string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	return ""
}

// The Running row itself names the parent and where this sliver sits in the
// chain, not only the detail strip under it.
func TestSliverRowShowsTheParentAndPosition(t *testing.T) {
	h := sliverHarness(t, "", sliverB)
	h.m.active = runningEntry
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	row := sliverLine(plainView(h.m), "S1b")
	if !strings.Contains(row, "parent S1") || !strings.Contains(row, "2 of 3") {
		t.Fatalf("the S1b row lacks the parent and the position: %q", row)
	}
}

// A mid sliver of a chain is committed, but the parent's branch lands once,
// after the last sliver: only the last one is ready to land.
func TestReadyToLandSkipsAMidSliver(t *testing.T) {
	h := landingHarness(t, `[`+sliverA+`]`)
	if rows := h.m.readyRows(); len(rows) != 0 {
		t.Fatalf("a mid sliver is ready to land: %+v", rows)
	}
	last := strings.Replace(strings.Replace(sliverA, `"sliver_index":1`, `"sliver_index":3`, 1), `"item":"S1a"`, `"item":"S1c"`, 1)
	h = landingHarness(t, `[`+last+`]`)
	rows := h.m.readyRows()
	if len(rows) != 1 {
		t.Fatalf("the last sliver is not ready to land: %+v", rows)
	}
	// The last sliver lands the parent, as the engine does, not the sliver.
	if len(rows[0].commands) == 0 || rows[0].commands[0] != `slicer land 'S1' --branch 'S1' --note 'Landed S1'` {
		t.Fatalf("the last sliver's land command: %q", rows[0].commands)
	}
}

// The run tree puts a sliver's roles under the sliver, under the parent, and
// says what is true of each node.
func TestRunTreeNestsSliversUnderTheParent(t *testing.T) {
	h := sliverHarness(t, "run-b", sliverA, sliverB, sliverC)
	steps := h.m.paneSteps()
	tree := sgr.ReplaceAllString(strings.Join(h.m.runTreeLines(h.m.paneTreeSteps(steps), 100, 0), "\n"), "")
	indent := func(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }
	parent, current, role := sliverLine(tree, "chain"), sliverLine(tree, "S1b 2 of 3"), sliverLine(tree, "implementer")
	if parent == "" || current == "" || role == "" {
		t.Fatalf("the tree lacks the parent, the sliver or its role:\n%s", tree)
	}
	if !(indent(parent) < indent(current) && indent(current) < indent(role)) {
		t.Fatalf("sliver roles are not nested under the sliver under the parent:\n%s", tree)
	}
	// The parent is not done while the chain runs.
	if strings.Contains(parent, "+") || strings.Contains(parent, "done") {
		t.Fatalf("the parent reads as done while its chain runs: %q", parent)
	}
	// The committed sliver is done; the queued one is queued, not waiting on the owner.
	if done := sliverLine(tree, "S1a 1 of 3"); !strings.Contains(done, "+") {
		t.Fatalf("the committed sliver is not done: %q", done)
	}
	if queued := sliverLine(tree, "S1c 3 of 3"); strings.Contains(queued, "?") || !strings.Contains(queued, "queued") {
		t.Fatalf("a queued sliver does not read as queued: %q", queued)
	}
}

// The pane of a queued sliver shows it queued, not started, with its roles
// that have not begun nested under it.
func TestQueuedSliverPaneReadsQueued(t *testing.T) {
	h := sliverHarness(t, "run-c", sliverA, sliverB, sliverC)
	view := treePaneView(h.m, 100, 40)
	if node := sliverLine(view, "S1c 3 of 3"); !strings.Contains(node, "queued") || strings.Contains(node, "start") {
		t.Fatalf("the open sliver node does not read as queued: %q\n%s", node, view)
	}
	node, tail := sliverLine(view, "S1c 3 of 3"), sliverLine(view, ". implementer")
	indent := func(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }
	if tail == "" || indent(tail) <= indent(node) {
		t.Fatalf("the queued roles are not nested under the sliver: %q\n%s", tail, view)
	}
}

// The pane's stage bar is the run's own, as its row reads, not one made from
// the parent and sliver nodes.
func TestSliverPaneStageBarMatchesTheRow(t *testing.T) {
	h := sliverHarness(t, "run-b", sliverA, sliverB, sliverC)
	view := treePaneView(h.m, 100, 40)
	if !strings.Contains(view, "[~~....]") && !strings.Contains(view, "[~=....]") && !strings.Contains(view, "[==....]") && !strings.Contains(view, "[=~....]") {
		t.Fatalf("the pane's stage bar shows no implementer stage running:\n%s", view)
	}
}
