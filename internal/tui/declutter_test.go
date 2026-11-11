// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/poll"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// S333 (adj 282, 283, 284): the TUI starts on Running, an open Running row adds
// only facts its row lacks, Recent lists real runs, other tools' stale relay
// ledgers sit behind one line, and the run detail quiets an expected fallback.

// TestStartsOnRunning: a new model with a registry and no input has the Running
// entry selected, and its first view shows the Running heading.
func TestStartsOnRunning(t *testing.T) {
	m := New(Config{
		Source:     &fakeSource{snaps: map[string]source.Snapshot{}},
		Projects:   expandProjects(2),
		Now:        func() time.Time { return t0 },
		Schedule:   poll.DefaultSchedule,
		TickEvery:  time.Second,
		FrameAfter: func(time.Duration) tea.Cmd { return nil },
		After:      func(time.Duration) tea.Cmd { return nil },
		Bell:       func() {},
		Theme:      plainTheme(),
	})
	if m.active != runningEntry {
		t.Fatalf("a new model has entry %d selected, want the Running entry (%d)", m.active, runningEntry)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	v := plainView(m)
	if colLine(mainCols(v), "Running 0") < 0 {
		t.Errorf("the first view has no Running heading:\n%s", v)
	}
	// The sidebar order is unchanged: Running, then Needs you.
	if nav := m.navigation(); len(nav) < 2 || nav[0] != runningEntry || nav[1] != needsEntry {
		t.Errorf("navigation %v, want Running then Needs you first", nav)
	}
}

// engineWithOutput is expandRun's engine run: the reviewer runs on claude opus
// high with 12.4k tokens and two lines of output.
func engineWithOutput(i int) source.EngineRun {
	run := liveEngine(fmt.Sprintf("run-%d", i), fmt.Sprintf("S1%d", i), "review", testActivity)
	run.Roles[1].Model, run.Roles[1].Effort = "opus", "high"
	run.Roles[1].StartedAt = t0.Add(-3 * time.Minute)
	run.Tokens, run.UsageKnown = 12400, true
	run.Tail = "reading the diff\nchecking the golden views\n"
	return run
}

func outputHarness(t *testing.T, n, w, ht int) *harness {
	t.Helper()
	projects := expandProjects(n)
	h := newHarness(t, projects...)
	for i, p := range projects {
		h.src.set(engineSnap(p, engineWithOutput(i+1)))
	}
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: w, Height: ht})
	h.send(press("["))
	return h
}

// repeated are the cells of a row an open row must not say again.
func repeated(t *testing.T, i int, line string) {
	t.Helper()
	for _, bad := range []string{fmt.Sprintf("run-%d", i), fmt.Sprintf("S1%d", i), "[#", "[~", "heard", "implementer>", "implementer >", "activity", "steps"} {
		if strings.Contains(line, bad) {
			t.Errorf("an open run repeats %q from its row: %q", bad, line)
		}
	}
}

// An open live row adds one line per active role, and its last output line.
func TestExpandedRowAddsOnlyTheActiveRole(t *testing.T) {
	h := outputHarness(t, 2, 100, 40)
	h.send(space())
	lines := mainCols(plainView(h.m))
	row := colLine(lines, "p1", "S11")
	next := colLine(lines, "p2", "S12")
	if row < 0 || next < row {
		t.Fatalf("rows not found (%d, %d):\n%s", row, next, strings.Join(lines, "\n"))
	}
	block := lines[row+1 : next]
	if len(block) != 2 {
		t.Fatalf("an open run adds %d lines, want one role line and one output line:\n%s", len(block), strings.Join(block, "\n"))
	}
	for _, want := range []string{"reviewer", "claude", "opus", "high", elapsed(3 * time.Minute), "tokens " + compactTokens(12400), "$-"} {
		if !strings.Contains(block[0], want) {
			t.Errorf("the role line lacks %q: %q", want, block[0])
		}
	}
	if !strings.Contains(block[1], "checking the golden views") || strings.Contains(block[1], "reading the diff") {
		t.Errorf("the output line is not the role's last line: %q", block[1])
	}
	for _, line := range block {
		repeated(t, 1, line)
	}
}

// A finished role is not active: only the running one gets a line, and a role
// with no output has no output line.
func TestExpandedRowSkipsFinishedRolesAndEmptyOutput(t *testing.T) {
	h := outputHarness(t, 1, 100, 40)
	run := engineWithOutput(1)
	run.Tail = ""
	h.src.set(engineSnap(expandProjects(1)[0], run))
	h.send(press("r"))
	h.send(space())
	lines := mainCols(plainView(h.m))
	row := colLine(lines, "p1", "S11")
	var block []string
	for _, l := range lines[row+1:] {
		if isRule(l) || strings.TrimSpace(l) == "" {
			break
		}
		block = append(block, l)
	}
	if len(block) != 1 || !strings.Contains(block[0], "reviewer") || strings.Contains(strings.Join(block, " "), "implementer") {
		t.Errorf("want one line for the running reviewer only, got:\n%s", strings.Join(block, "\n"))
	}
}

// E opens every run and adds no line that repeats a row.
func TestExpandAllAddsNoLineThatRepeatsARow(t *testing.T) {
	h := outputHarness(t, 2, 100, 40)
	// Without the hint and status lines at the bottom, which change with the key.
	body := func() []string { l := mainCols(plainView(h.m)); return l[:len(l)-2] }
	before := body()
	h.send(press("E"))
	after := body()
	seen := map[string]int{}
	for _, l := range before {
		seen[strings.TrimSpace(l)]++
	}
	var added []string
	for _, l := range after {
		if k := strings.TrimSpace(l); seen[k] > 0 {
			seen[k]--
		} else if k != "" {
			added = append(added, l)
		}
	}
	if len(added) != 4 {
		t.Fatalf("E on two runs adds %d lines, want a role line and an output line each:\n%s", len(added), strings.Join(added, "\n"))
	}
	for i, l := range added {
		repeated(t, i/2+1, l)
		repeated(t, 3-i/2-1, l)
	}
}

// The collapsed Running view has no bottom strip that says the selected row
// again: the row's heard cell and pipeline show once, and the run id, which the
// row does not carry, is not added back.
func TestCollapsedRunningViewHasNoStripRepeatingTheRow(t *testing.T) {
	h := outputHarness(t, 1, 100, 30)
	v := strings.Join(mainCols(plainView(h.m)), "\n")
	if n := strings.Count(v, "heard"); n > 1 {
		t.Errorf("heard shows %d times, a strip repeats the row:\n%s", n, v)
	}
	if n := strings.Count(v, "[reviewer]"); n > 1 {
		t.Errorf("the pipeline shows %d times:\n%s", n, v)
	}
	if strings.Contains(v, "activity") || strings.Contains(v, "run-1") {
		t.Errorf("a strip under the rows repeats the selected row:\n%s", v)
	}
}

// openLines is the lines an open run adds under its row: from the line after the
// row to the next row or rule. A row starts in column 1 to 3; the lines an open
// run adds are indented four columns.
func openLines(lines []string, row int) []string {
	var out []string
	for _, l := range lines[row+1:] {
		if isRule(l) || !strings.HasPrefix(l, "    ") {
			break
		}
		out = append(out, l)
	}
	return out
}

// An open run with no role working (a question, a pause or a stop) adds one dim
// line of the facts its row lacks, so its question or stop reason shows without
// selecting the row (owner 2026-11-11, ui.running-expand-idle).
func TestOpenIdleRunShowsItsFactsLine(t *testing.T) {
	projects := expandProjects(3)
	h := newHarness(t, projects...)
	question := liveEngine("run-1", "S11", "paused", nil, roleRun("implementer", 9*time.Minute, 4*time.Minute))
	question.Pending, question.Wait = "question", "Which store?"
	stopped := liveEngine("run-2", "S12", "stopped", nil, roleRun("implementer", 9*time.Minute, 4*time.Minute))
	stopped.Wait = "owner action"
	h.src.set(engineSnap(projects[0], question))
	h.src.set(engineSnap(projects[1], stopped))
	h.src.set(expandSnap(projects[2], 3))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	h.send(press("["))
	h.send(press("E"))
	lines := mainCols(plainView(h.m))
	for _, c := range []struct{ project, item, want string }{{"p1", "S11", "Which store?"}, {"p2", "S12", "owner action"}} {
		row := colLine(lines, c.project, c.item)
		block := openLines(lines, row)
		if len(block) != 1 || !strings.Contains(block[0], c.want) {
			t.Errorf("%s: an open idle run adds %d lines, want one with %q:\n%s", c.project, len(block), c.want, strings.Join(lines, "\n"))
			continue
		}
		for _, bad := range []string{c.item, "run-", "[#", "[~", "[.", "heard", "implementer>", "activity"} {
			if strings.Contains(block[0], bad) {
				t.Errorf("%s: the facts line repeats %q from the row: %q", c.project, bad, block[0])
			}
		}
	}
}

// A role fact that holds only the role name says nothing its row lacks: a relay
// started elsewhere whose step has no vendor, model or start time adds no role
// line; the facts line stands in.
func TestOpenRelayWithBareStepAddsNoRoleOnlyLine(t *testing.T) {
	projects := expandProjects(2)
	h := newHarness(t, projects...)
	h.src.set(snap(projects[0], nil, nil, externalRun("ext-1", "S97")))
	h.src.set(expandSnap(projects[1], 2))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	h.send(press("["))
	h.send(press("E"))
	lines := mainCols(plainView(h.m))
	row := colLine(lines, "p1", "S97")
	block := openLines(lines, row)
	if len(block) != 1 || strings.TrimSpace(block[0]) == "reviewer" || !strings.Contains(block[0], "steps") {
		t.Errorf("a bare step adds %v, want the facts line and no role-only line:\n%s", block, strings.Join(lines, "\n"))
	}
}

// A spec-check result is not a Recent row; landed, stopped and failed runs are.
func TestRecentLeavesOutSpecCheckResults(t *testing.T) {
	p := proj("p1", false)
	h := newHarness(t, p)
	ended := func(id, item, outcome, role string) slicercli.RelayRun {
		return slicercli.RelayRun{ID: id, Items: []string{item}, State: "ended", Outcome: outcome,
			BeganAt: t0.Add(-10 * time.Minute).Format(time.RFC3339), UpdatedAt: t0.Add(-5 * time.Minute).Format(time.RFC3339),
			Steps: []slicercli.Step{{ID: "s1", Role: role, Phase: "done"}}}
	}
	mixed := ended("led-mix", "S74", "pass", "spec-check")
	mixed.Steps = append(mixed.Steps, slicercli.Step{ID: "s2", Role: "implementer", Phase: "done"})
	s := snap(p, nil, nil)
	s.BoardRuns = source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{
		mixed,
		ended("led-spec", "S331", "pass", "spec-check"),
		ended("led-land", "S71", "pass", "implementer"),
		ended("led-stop", "S72", "stopped", "implementer"),
		ended("led-fail", "S73", "fail", "reviewer"),
	}}
	s.EngineRuns = []source.EngineRun{{ID: "run-land", Item: "S71", State: "committed", Landed: true, RelayRun: "led-land", CreatedAt: t0.Add(-10 * time.Minute)}}
	h.src.set(s)
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.send(press("["))
	v := strings.Join(mainCols(plainView(h.m)), "\n")
	if strings.Contains(v, "S331") {
		t.Errorf("a spec-check result is a Recent row:\n%s", v)
	}
	for _, want := range []string{"✓ landed", "~ stopped", "✗ fail"} {
		if !strings.Contains(v, want) {
			t.Errorf("Recent lacks a %q row:\n%s", want, v)
		}
	}
	// Mixed roles are a real run: a spec-check step beside others does not hide it.
	if !strings.Contains(v, "S74") {
		t.Errorf("a run with a spec-check step and an implementer step is not in Recent:\n%s", v)
	}
	if rows := h.m.recentRows(); len(rows) != 4 {
		t.Errorf("recentRows = %d, want 4", len(rows))
	}
}

func staleLedger(id, item string) slicercli.RelayRun {
	r := externalRun(id, item)
	r.State = "stale"
	r.BeganAt = t0.Add(-11 * time.Hour).Format(time.RFC3339)
	r.UpdatedAt = t0.Add(-10 * time.Hour).Format(time.RFC3339)
	return r
}

func staleHarness(t *testing.T, w, ht int) *harness {
	t.Helper()
	projects := expandProjects(3)
	h := newHarness(t, projects...)
	h.src.set(expandSnap(projects[0], 1))
	h.src.set(snap(projects[1], nil, nil, staleLedger("ext-a", "S97")))
	h.src.set(snap(projects[2], nil, nil, staleLedger("ext-b", "S98")))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: w, Height: ht})
	h.send(press("["))
	return h
}

// A stale ledger row leaves the Running list for one dim line `stale: N`; the
// count of the heading is unchanged.
func TestStaleLedgersLeaveTheRunningList(t *testing.T) {
	h := staleHarness(t, 100, 30)
	th := NewTheme(false)
	h.m.cfg.Theme = th
	v := strings.Join(mainCols(plainView(h.m)), "\n")
	for _, gone := range []string{"S97", "S98", "ext-a", "ext-b"} {
		if strings.Contains(v, gone) {
			t.Errorf("a stale ledger (%s) stays in the Running list:\n%s", gone, v)
		}
	}
	if !strings.Contains(v, "S11") || !strings.Contains(v, "Running 1") {
		t.Errorf("the live run or the count is wrong:\n%s", v)
	}
	lines := mainCols(plainView(h.m))
	at := colLine(lines, "stale: 2")
	rec := colLine(lines, "Recent")
	if at < 0 || at > rec || strings.Count(v, "stale:") != 1 {
		t.Fatalf("the Running list does not end with one stale: 2 line (line %d, Recent %d):\n%s", at, rec, v)
	}
	if got := h.m.liveRunCount(); got != 1 {
		t.Errorf("liveRunCount = %d, want 1", got)
	}
	var raw string
	for _, l := range strings.Split(h.m.View().Content, "\n") {
		if strings.Contains(sgr.ReplaceAllString(l, ""), "stale: 2") {
			raw = l
		}
	}
	if !strings.Contains(raw, th.Dim.Render("stale: 2")) {
		t.Errorf("the stale line is not dim: %q", raw)
	}
	// No stale ledger: no line.
	h2 := newHarness(t, expandProjects(1)...)
	h2.src.set(expandSnap(expandProjects(1)[0], 1))
	h2.start()
	h2.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h2.send(press("["))
	if v := plainView(h2.m); strings.Contains(v, "stale:") {
		t.Errorf("a stale line with no stale ledger:\n%s", v)
	}
}

// The stale line is a cursor row (owner 2026-11-11, ui.stale-ledger-access):
// Enter or space shows the stale rows, each of which opens its pane as before,
// so `x end run` stays reachable.
func TestStaleLineIsSelectableAndShowsTheStaleRows(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{press("enter"), space()} {
		h := staleHarness(t, 100, 30)
		h.send(press("j"))
		r, ok := h.m.selectedRow()
		if !ok || r.staleN != 2 {
			t.Fatalf("j does not land on the stale line: %+v", r)
		}
		if hint := wideHint(h); !strings.Contains(hint, "show stale") {
			t.Errorf("the hint on the stale line is %q, want show stale", hint)
		}
		h.send(key)
		h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		v := strings.Join(mainCols(plainView(h.m)), "\n")
		if !strings.Contains(v, "S97") || !strings.Contains(v, "S98") || !strings.Contains(v, "Running 1") || h.m.pane != nil {
			t.Fatalf("%v does not show the stale rows (pane %v):\n%s", key, h.m.pane != nil, v)
		}
		if hint := wideHint(h); !strings.Contains(hint, "hide stale") {
			t.Errorf("with the rows shown the hint is %q, want hide stale", hint)
		}
		if got := h.m.liveRunCount(); got != 1 {
			t.Errorf("liveRunCount = %d with the stale rows shown, want 1", got)
		}
		h.send(press("j"))
		h.send(press("enter"))
		if h.m.pane == nil || !h.m.pane.ledger || h.m.pane.run != "ext-a" {
			t.Fatalf("a stale row does not open its pane: %+v", h.m.pane)
		}
		h.send(press("esc"))
		h.send(press("k"))
		h.send(key)
		if v := strings.Join(mainCols(plainView(h.m)), "\n"); strings.Contains(v, "S97") {
			t.Errorf("%v again leaves the stale rows shown:\n%s", key, v)
		}
	}
}

// --- run detail ---

// creditsLedger is the ledger of a run whose codex implementer ran out of
// credits at once and fell back to claude: every step carries the engine's own
// "S298 role" task label.
func creditsFixture(exit string, fallback bool) ([]slicercli.RelayRun, string) {
	steps := []slicercli.Step{
		{ID: "o", Role: "orchestrator", Agent: "ork", Phase: "start", Task: "S298 orchestrator", BeganAt: "2026-10-19T16:30:00Z"},
		{ID: "i1", Parent: "o", Role: "implementer", Vendor: "codex", Model: "gpt-6.1-sol", Reasoning: "medium", Phase: "fail", Task: "S298 implementer", BeganAt: "2026-10-19T16:31:00Z", UpdatedAt: "2026-10-19T16:31:03Z"},
		{ID: "r1", Parent: "o", Role: "reviewer", Vendor: "claude", Model: "opus", Phase: "start", Task: "S298 reviewer", BeganAt: "2026-10-19T16:39:00Z"},
	}
	roles := `{"role":"implementer","round":1,"vendor":"codex","model":"gpt-6.1-sol","effort":"medium","step":"i1","started_at":"2026-10-19T16:31:00Z","ended_at":"2026-10-19T16:31:03Z","exit":"` + exit + `"},`
	if fallback {
		steps = append(steps[:2], append([]slicercli.Step{{ID: "i2", Parent: "o", Role: "implementer", Vendor: "claude", Model: "sonnet", Reasoning: "high", Phase: "done", Task: "S298 implementer", BeganAt: "2026-10-19T16:31:03Z", UpdatedAt: "2026-10-19T16:38:00Z"}}, steps[2:]...)...)
		roles += `{"role":"implementer","round":1,"vendor":"claude","model":"sonnet","effort":"high","step":"i2","started_at":"2026-10-19T16:31:03Z","ended_at":"2026-10-19T16:38:00Z","exit":"ok"},`
	}
	roles += `{"role":"reviewer","round":1,"vendor":"claude","model":"opus","step":"r1","started_at":"2026-10-19T16:39:00Z"}`
	ledger := []slicercli.RelayRun{{ID: "tree", Items: []string{"S298"}, State: "active", BeganAt: "2026-10-19T16:30:00Z", Steps: steps}}
	return ledger, `{"id":"engine","item":"S298","state":"review","relay_run":"tree","created_at":"2026-10-19T16:30:00Z","roles":[` + roles + `]}`
}

const creditsNote = "codex out of credits, fell back to claude sonnet high"

func TestRunDetailShowsOutOfCreditsFallbackAsADimNote(t *testing.T) {
	runs, engine := creditsFixture("out_of_credits", true)
	h := treeHarness(t, runs, engine)
	th := NewTheme(false)
	h.m.cfg.Theme = th
	lines := h.m.paneLines(100, 40, "")
	plain := sgr.ReplaceAllString(strings.Join(lines, "\n"), "")
	at := -1
	for i, l := range lines {
		if strings.Contains(sgr.ReplaceAllString(l, ""), creditsNote) {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no fallback note %q:\n%s", creditsNote, plain)
	}
	// Its mark is one no step state uses: `.` is a queued step, `+` done, `~`
	// stale, `|/-\` running, `?` waiting, `!` failed.
	if !strings.Contains(lines[at], th.Dim.Render("* "+creditsNote)) {
		t.Errorf("the note is not dim, or its mark is not *: %q", lines[at])
	}
	if strings.Contains(plain, " fail ") || strings.Contains(plain, "! implementer") {
		t.Errorf("the out-of-credits attempt still shows as a failure:\n%s", plain)
	}
	if strings.Contains(plain, "fell back from") {
		t.Errorf("the fallback is said twice:\n%s", plain)
	}
	red := strings.Split(th.Error.Render("x"), "x")[0]
	for _, l := range lines {
		if strings.Contains(l, red) {
			t.Errorf("a red line in a run with an expected fallback: %q", l)
		}
	}
}

// A real failure stays red, whether or not another attempt follows; an
// out-of-credits attempt with nothing after it is a failure too.
func TestRunDetailKeepsARealFailureRed(t *testing.T) {
	for _, c := range []struct {
		name     string
		exit     string
		fallback bool
	}{{"error then fallback", "error", true}, {"unclassified then fallback", "", true}, {"out of credits, nothing after", "out_of_credits", false}} {
		t.Run(c.name, func(t *testing.T) {
			runs, engine := creditsFixture(c.exit, c.fallback)
			h := treeHarness(t, runs, engine)
			th := NewTheme(false)
			h.m.cfg.Theme = th
			red := strings.Split(th.Error.Render("x"), "x")[0]
			var failLine string
			for _, l := range h.m.paneLines(100, 40, "") {
				if strings.Contains(sgr.ReplaceAllString(l, ""), "! implementer") {
					failLine = l
				}
			}
			if failLine == "" || !strings.Contains(failLine, red) {
				t.Errorf("the failure is not a red fail line: %q", failLine)
			}
			if plain := treePaneView(h.m, 100, 40); strings.Contains(plain, "out of credits, fell back") {
				t.Errorf("a failure that is not an out-of-credits fallback has the note:\n%s", plain)
			}
		})
	}
}

// The tree names each role once: no `S298 implementer` sub-line. A task that
// says more than the id and the role stays.
func TestRunDetailHasNoIDRoleSubLine(t *testing.T) {
	// A real failure keeps its node, and the node of the running reviewer too;
	// each carries the engine's `S298 role` task.
	runs, engine := creditsFixture("error", true)
	h := treeHarness(t, runs, engine)
	v := treePaneView(h.m, 100, 40)
	for _, want := range []string{"! implementer", "| reviewer"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the fixture lacks the node %q:\n%s", want, v)
		}
	}
	for _, bad := range []string{"S298 implementer", "S298 reviewer", "S298 ui", "S298 orchestrator"} {
		if strings.Contains(v, bad) {
			t.Errorf("the tree has an ID role sub-line %q:\n%s", bad, v)
		}
	}
	// A task that says more than the label stays.
	runs[0].Steps[len(runs[0].Steps)-1].Task = "S298 reviewer rechecking the golden views"
	h = treeHarness(t, runs, engine)
	if v := treePaneView(h.m, 100, 40); !strings.Contains(v, "S298 reviewer rechecking the golden views") {
		t.Errorf("a real task line was dropped:\n%s", v)
	}
}

// At 80 columns no detail line passes the width, with the fallback note and the
// fallback suffix of another failure.
func TestRunDetailFitsAt80Columns(t *testing.T) {
	for _, exit := range []string{"out_of_credits", "rate_limited", "error"} {
		runs, engine := creditsFixture(exit, true)
		h := treeHarness(t, runs, engine)
		for _, w := range []int{80, 60, 40} {
			for i, l := range h.m.paneLines(w, 40, "") {
				if lipgloss.Width(l) > w {
					t.Errorf("%s at %d: line %d is %d wide: %q", exit, w, i, lipgloss.Width(l), sgr.ReplaceAllString(l, ""))
				}
			}
			// The tree itself, before the pane draws it.
			for i, l := range h.m.runTreeLines(h.m.paneSteps(), w, 0) {
				if lipgloss.Width(l) > w {
					t.Errorf("%s at %d: tree line %d is %d wide: %q", exit, w, i, lipgloss.Width(l), sgr.ReplaceAllString(l, ""))
				}
			}
		}
		h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		for i, l := range strings.Split(plainView(h.m), "\n") {
			if lipgloss.Width(l) > 80 {
				t.Errorf("%s: view line %d passes 80 columns: %q", exit, i, l)
			}
		}
	}
}

// Goldens of the new views: the Running list with its stale line and an open
// run, in colour at 100x30, mono at 80x24 and ASCII at 100x30; and the run
// detail with the fallback note.
func TestGoldenRunningDeclutter(t *testing.T) {
	for _, c := range []struct {
		name  string
		w, h  int
		mono  bool
		ascii bool
		open  bool
	}{
		{"running-stale-100x30-color.txt", 100, 30, false, false, true},
		{"running-stale-80x24-mono.txt", 80, 24, true, false, true},
		{"running-stale-100x30-ascii.txt", 100, 30, true, true, true},
		{"running-stale-shown-100x30-mono.txt", 100, 30, true, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := staleHarness(t, c.w, c.h)
			h.m.cfg.Theme, h.m.cfg.ASCII = NewTheme(c.mono), c.ascii
			h.src.set(engineSnap(expandProjects(3)[0], engineWithOutput(1)))
			h.send(press("r"))
			if c.open {
				h.send(space())
			} else {
				h.send(press("j"))
				h.send(press("enter"))
			}
			checkGolden(t, c.name, h.m.View().Content)
		})
	}
}

func TestGoldenRunDetailFallbackNote(t *testing.T) {
	for _, c := range []struct {
		name  string
		w, h  int
		ascii bool
	}{{"pane-credits-fallback-100x30-mono.txt", 100, 30, false}, {"pane-credits-fallback-80x24-mono.txt", 80, 24, false}, {"pane-credits-fallback-100x30-ascii.txt", 100, 30, true}} {
		t.Run(c.name, func(t *testing.T) {
			runs, engine := creditsFixture("out_of_credits", true)
			h := treeHarness(t, runs, engine)
			h.m.cfg.Theme, h.m.cfg.ASCII = NewTheme(true), c.ascii
			checkGolden(t, c.name, strings.Join(h.m.paneLines(c.w, c.h, ""), "\n")+"\n")
		})
	}
}
