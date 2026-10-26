// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// The Running entry is the first sidebar row, above Needs you (S110, adj 169
// ui.sidebar-running). It shows the aggregate Overview's Running section
// full-screen, with an alert on a row that needs the owner.

// roleRun is an engine role that began ago before the harness clock. A zero
// ended means the role is still running.
func roleRun(role string, began, ended time.Duration) source.EngineRole {
	r := source.EngineRole{Role: role, Round: 1, Vendor: "claude", StartedAt: t0.Add(-began), Exit: "ok"}
	if ended > 0 {
		r.EndedAt = t0.Add(-ended)
	}
	return r
}

// liveEngine is an engine run with the implementer done and the reviewer running.
func liveEngine(id, item, state string, activity []int, roles ...source.EngineRole) source.EngineRun {
	if len(roles) == 0 {
		roles = []source.EngineRole{roleRun("implementer", 9*time.Minute, 4*time.Minute), roleRun("reviewer", 3*time.Minute, 0)}
	}
	return source.EngineRun{ID: id, Item: item, State: state, Roles: roles, Activity: activity, CreatedAt: t0.Add(-10 * time.Minute)}
}

var testActivity = []int{0, 1, 2, 4, 8, 3, 0, 2, 5, 1}

func engineSnap(p registry.Project, runs ...source.EngineRun) source.Snapshot {
	s := snap(p, nil, nil)
	s.EngineRuns = runs
	return s
}

// externalRun is a relay started elsewhere: the implementer is done and the
// reviewer is running, last heard a minute ago.
func externalRun(id, item string) slicercli.RelayRun {
	r := relayWithSteps(
		slicercli.Step{ID: "s1", Role: "implementer", Phase: "done"},
		slicercli.Step{ID: "s2", Role: "reviewer", Phase: "start"})
	r.ID, r.Items = id, []string{item}
	r.BeganAt = t0.Add(-8 * time.Minute).Format(time.RFC3339)
	r.UpdatedAt = t0.Add(-time.Minute).Format(time.RFC3339)
	return r
}

var runningProjects = []registry.Project{proj("alpha", false), proj("beta", false), proj("gamma", false)}

// runningHarness has alpha running an engine run, beta a relay started
// elsewhere and gamma nothing, with the Running entry selected.
func runningHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, runningProjects...)
	h.src.set(engineSnap(runningProjects[0], liveEngine("run-a", "S30", "review", testActivity)))
	h.src.set(snap(runningProjects[1], nil, nil, externalRun("ext-b", "S41")))
	h.src.set(snap(runningProjects[2], nil, nil))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	h.send(press("["))
	if h.m.active != runningEntry {
		t.Fatalf("left from Needs you selected %d, want the Running entry", h.m.active)
	}
	return h
}

// wide is the view at 160 columns, where no cell of a row is dropped.
func (h *harness) wide() string {
	h.m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	return plainView(h.m)
}

// mainRow is the first line of view, without the sidebar, that holds all words.
func mainRow(view string, words ...string) string {
next:
	for _, line := range strings.Split(view, "\n") {
		_, main, _ := strings.Cut(line, "│")
		for _, w := range words {
			if !strings.Contains(main, w) {
				continue next
			}
		}
		return main
	}
	return ""
}

func sidebarLine(view, prefix string) int {
	for i, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(line, prefix) {
			return i
		}
	}
	return -1
}

func TestSidebarHasARunningEntryAboveNeedsYouAndShowsItDimWhenNothingRuns(t *testing.T) {
	h := newHarness(t, runningProjects...)
	h.start()
	v := h.view()
	running, needs := sidebarLine(v, "  Running"), sidebarLine(v, "> Needs you")
	if running < 0 || needs < 0 || running+1 != needs {
		t.Fatalf("the Running entry must sit directly above Needs you (rows %d and %d):\n%s", running, needs, v)
	}
	row := sidebarRow(h.m, "  Running")
	if !dimmed(row, "Running") || !strings.Contains(compactSpaces(sgr.ReplaceAllString(row, "")), "Running 0") {
		t.Fatalf("an empty Running entry must be dim and say 0: %q", row)
	}
	h.send(press("["))
	v = h.view()
	if !strings.Contains(v, "> Running") || !strings.Contains(v, "nothing running") || !strings.Contains(v, "Running 0") {
		t.Fatalf("the empty Running view must say nothing running:\n%s", v)
	}
	if strings.Contains(v, "Needs you") && strings.Contains(v, "nothing needs you") {
		t.Fatalf("the Running view must not show the Needs you section:\n%s", v)
	}

	// A live run lights the entry up and counts it; the rows below do not move.
	h.src.set(engineSnap(runningProjects[0], liveEngine("run-a", "S30", "review", testActivity)))
	h.send(press("r"))
	row = sidebarRow(h.m, "> Running")
	if plain := compactSpaces(sgr.ReplaceAllString(row, "")); !strings.Contains(plain, "> Running 1") {
		t.Fatalf("one live run must show as Running 1: %q", plain)
	}
	if after := h.view(); sidebarLine(after, "  Needs you") != needs {
		t.Fatalf("Needs you moved when a run began:\n%s", after)
	}
	h.send(press("]"))
	if row := sidebarRow(h.m, "  Running"); dimmed(row, "Running") {
		t.Fatalf("a Running entry with a live run must not be dim: %q", row)
	}
}

func TestRunningEntryIsReachedWithTheSidebarKeysNotADigit(t *testing.T) {
	h := newHarness(t, runningProjects...)
	h.start()
	h.send(press("["))
	if h.m.active != runningEntry {
		t.Fatalf("left from Needs you: active %d, want Running", h.m.active)
	}
	h.send(press("["))
	if h.m.active != 2 {
		t.Fatalf("left from Running must wrap to the last project, got %d", h.m.active)
	}
	h.send(press("]"))
	h.send(press("]"))
	if h.m.active != needsEntry {
		t.Fatalf("right from Running: active %d, want Needs you", h.m.active)
	}
	h.send(press("1"))
	if h.m.active == runningEntry || h.m.active != 0 {
		t.Fatalf("digit 1 selected %d, want the first project", h.m.active)
	}
	// The filter hides projects, never the two aggregate entries.
	h.send(press("["))
	h.send(press("/"))
	h.send(press("z"))
	v := h.view()
	if sidebarLine(v, "  Running") < 0 || sidebarLine(v, "> Needs you") < 0 {
		t.Fatalf("the filter hid an aggregate entry:\n%s", v)
	}
}

func TestRunningEntryFocusesEveryProjectForTheLiveRefresh(t *testing.T) {
	h := newHarness(t, runningProjects...)
	live := &fakeLive{}
	h.m.cfg.Live = live
	h.start()
	h.send(press("1"))
	h.send(press("[")) // Needs you
	live.mu.Lock()
	live.focused = nil
	live.mu.Unlock()
	for _, msg := range h.send(press("[")) {
		h.deliver(msg)
	}
	calls := live.focusedCalls()
	if len(calls) == 0 || strings.Join(calls[len(calls)-1], ",") != "alpha,beta,gamma" {
		t.Fatalf("the Running entry must focus every project, got %v", calls)
	}
}

func TestSummaryListsEveryProjectWithALiveRun(t *testing.T) {
	h := runningHarness(t)
	v := h.wide()
	if !strings.Contains(v, "Running 2") {
		t.Fatalf("heading must count two live runs:\n%s", v)
	}
	alpha := mainRow(v, "alpha", "S30")
	for _, want := range []string{"[##~~..]", "implementer>[reviewer]>gate", "heard 3m", sparkline(testActivity, false)} {
		if !strings.Contains(alpha, want) {
			t.Errorf("alpha's row lacks %q: %q", want, alpha)
		}
	}
	beta := mainRow(v, "beta", "S41")
	for _, want := range []string{"[##~~..]", "implementer>[reviewer]>gate", "heard 1m"} {
		if !strings.Contains(beta, want) {
			t.Errorf("beta's row lacks %q: %q", want, beta)
		}
	}
	// The item sits before the bar, last heard follows the bar (S106; the round is
	// in the bar from the second), then the pipeline and the activity.
	last := -1
	for _, cell := range []string{"S30", "[##~~..]", "heard 3m", "implementer>[reviewer]>gate", sparkline(testActivity, false)} {
		i := strings.Index(alpha, cell)
		if i <= last {
			t.Errorf("cell %q is out of order in %q", cell, alpha)
		}
		last = i
	}
	if row := mainRow(v, "gamma"); row != "" {
		t.Errorf("a project with no live run must not be listed: %q", row)
	}
}

func TestSummaryRowsUpdateOnTheNextRefresh(t *testing.T) {
	h := runningHarness(t)
	h.m.cfg.Live = &fakeLive{}
	before := h.wide()
	if row := mainRow(before, "alpha", "S30"); !strings.Contains(row, "[##~~..]") || !strings.Contains(row, "[reviewer]") {
		t.Fatalf("setup: %q", row)
	}

	// The reviewer finishes and the gate starts; a notice reads the project.
	h.src.set(engineSnap(runningProjects[0], liveEngine("run-a", "S30", "gate", testActivity,
		roleRun("implementer", 9*time.Minute, 4*time.Minute), roleRun("reviewer", 3*time.Minute, 1*time.Minute), roleRun("gate", 1*time.Minute, 0))))
	h.send(noticeMsg{project: "alpha"})
	row := mainRow(h.wide(), "alpha", "S30")
	if !strings.Contains(row, "[######]") && !strings.Contains(row, "[####~~]") {
		t.Fatalf("the bar did not advance without a key: %q", row)
	}
	if !strings.Contains(row, "implementer>reviewer>[gate]") {
		t.Fatalf("the running role did not move to the gate: %q", row)
	}

	// The run ends: it leaves Running and shows under Recent, still without a key.
	ended := engineSnap(runningProjects[0], liveEngine("run-a", "S30", "committed", nil))
	ended.BoardRuns = source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{{
		ID: "ledger-a", Items: []string{"S30"}, State: "ended", Outcome: "pass",
		BeganAt: t0.Add(-10 * time.Minute).Format(time.RFC3339), UpdatedAt: t0.Add(-time.Minute).Format(time.RFC3339),
	}}}
	h.src.set(ended)
	h.send(noticeMsg{project: "alpha"})
	v := h.wide()
	if !strings.Contains(v, "Running 1") {
		t.Fatalf("the ended run still counts as running:\n%s", v)
	}
	if row := mainRow(v, "alpha", "[reviewer]"); row != "" {
		t.Fatalf("the ended run is still in Running: %q", row)
	}
	recent := v[strings.Index(v, "Recent"):]
	if row := mainRow(recent, "alpha", "S30", "✓ pass"); row == "" {
		t.Fatalf("the ended run did not drop to Recent:\n%s", v)
	}
	// The cursor walks on into Recent, and enter opens the ended run.
	h.send(press("down"))
	h.send(press("enter"))
	if h.m.pane == nil || !h.m.pane.ledger || h.m.pane.run != "ledger-a" {
		t.Fatalf("enter on the Recent row must open the ended run: %+v", h.m.pane)
	}
}

func TestQuestionOrStopShowsAnAlertOnTheSummaryRowAndEnterOpensItsPane(t *testing.T) {
	h := newHarness(t, runningProjects...)
	question := liveEngine("q1", "S30", "paused", nil)
	question.Pending, question.Wait = "question", "Which store should hold the cache?"
	stopped := liveEngine("s1", "S31", "stopped", nil)
	stopped.Wait = "owner action"
	h.src.set(engineSnap(runningProjects[0], question))
	h.src.set(engineSnap(runningProjects[1], stopped))
	h.src.set(engineSnap(runningProjects[2], liveEngine("run-c", "S32", "review", testActivity)))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	h.send(press("["))
	v := h.wide()
	if !strings.Contains(v, "Running 3") {
		t.Fatalf("a run that waits on the owner is still a live run:\n%s", v)
	}
	if head := mainRow(v, "Running 3"); !strings.Contains(head, "! owner questions 1") || !strings.Contains(head, "x stopped 1") {
		t.Errorf("the heading must count the question and the stop: %q", head)
	}
	// The alert is one glyph in the mark slot before the project name, as the
	// kind symbol is on Needs you. No row is wider or moved by it.
	q := mainRow(v, "alpha", "S30")
	if !strings.HasPrefix(q, "  ?alpha") || !strings.Contains(q, "[reviewer]") {
		t.Errorf("the paused run's row lacks its ? glyph before the name, or its pipeline: %q", q)
	}
	s := mainRow(v, "beta", "S31")
	if !strings.HasPrefix(s, "  xbeta") {
		t.Errorf("the stopped run's row lacks its x glyph before the name: %q", s)
	}
	c := mainRow(v, "gamma", "S32")
	if !strings.HasPrefix(c, "   gamma") {
		t.Errorf("a healthy run carries an alert glyph: %q", c)
	}
	for _, row := range []string{q, s, c} {
		if strings.Contains(row, "? question") || strings.Contains(row, "x stopped") {
			t.Errorf("the alert words belong to the heading and the detail line, not the row: %q", row)
		}
		if strings.Index(row, "[##") != strings.Index(c, "[##") {
			t.Errorf("an alert moved the columns of a row: %q against %q", row, c)
		}
	}
	if got := detailText(h.m, 150); !strings.Contains(got, "paused") || !strings.Contains(got, "Which store should hold the cache?") {
		t.Errorf("the detail line must keep the question in words: %q", got)
	}

	h.send(press("enter"))
	if h.m.pane == nil || h.m.pane.run != "q1" || h.m.active != runningEntry {
		t.Fatalf("enter must open the question run's pane on the Running entry: pane %+v, active %d", h.m.pane, h.m.active)
	}
	if v := h.view(); !strings.Contains(v, "Run q1") {
		t.Fatalf("the pane is not the question run's:\n%s", v)
	}
	h.send(press("esc"))
	h.send(press("down"))
	h.send(press("enter"))
	if h.m.pane == nil || h.m.pane.run != "s1" {
		t.Fatalf("enter on the stopped row must open its pane: %+v", h.m.pane)
	}
}

func TestEnterOnALiveSummaryRowOpensThatRunsPane(t *testing.T) {
	h := runningHarness(t)
	h.send(press("down"))
	h.send(press("enter"))
	if h.m.pane == nil || !h.m.pane.ledger || h.m.pane.run != "ext-b" || h.m.pane.project != "beta" {
		t.Fatalf("enter on beta's row must open its ledger run: %+v", h.m.pane)
	}
}

func TestSummaryAtEightyByTwentyFour(t *testing.T) {
	h := newHarness(t, runningProjects...)
	question := liveEngine("q1", "S30", "paused", nil)
	question.Pending, question.Wait = "question", "Which store?"
	h.src.set(engineSnap(runningProjects[0], question))
	h.src.set(snap(runningProjects[1], nil, nil, externalRun("ext-b", "S41")))
	h.src.set(engineSnap(runningProjects[2], liveEngine("run-c", "S32", "review", testActivity)))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(press("["))
	v := plainView(h.m)
	lines := strings.Split(v, "\n")
	if len(lines) != 24 {
		t.Fatalf("the screen has %d lines, want 24", len(lines))
	}
	for _, line := range lines {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line wider than 80: %q", line)
		}
	}
	for _, c := range []struct{ name, item, bar, role string }{
		{"alpha", "S30", "[##~~..]", "[reviewer]"},
		{"beta", "S41", "[##~~..]", "[reviewer]"},
		{"gamma", "S32", "[##~~..]", "[reviewer]"},
	} {
		row := mainRow(v, c.name, c.item)
		if !strings.Contains(row, c.bar) || !strings.Contains(row, c.role) {
			t.Errorf("%s's row lost its stage bar %q or role %q at 80x24: %q", c.name, c.bar, c.role, row)
		}
	}
	if row := mainRow(v, "alpha", "S30"); !strings.HasPrefix(row, "  ?alpha") {
		t.Errorf("the alert glyph was lost at 80x24: %q", row)
	}
}

func TestSummarySelectedRowShowsEverythingOnTheDetailLine(t *testing.T) {
	h := runningHarness(t)
	h.wide()
	got := detailText(h.m, 150)
	for _, want := range []string{"run-a", "S30", "review", "[##~~..]", "implementer>[reviewer]>gate", "heard 3m", sparkline(testActivity, false)} {
		if !strings.Contains(got, want) {
			t.Errorf("the detail line lacks %q: %q", want, got)
		}
	}
}

func TestGoldenRunningSummary(t *testing.T) {
	for _, c := range []struct {
		name string
		w, h int
		mono bool
	}{
		{"running-100x30-color.txt", 100, 30, false},
		{"running-100x30-mono.txt", 100, 30, true},
		{"running-80x24-color.txt", 80, 24, false},
		{"running-80x24-mono.txt", 80, 24, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := runningGoldenModel(NewTheme(c.mono), c.w, c.h)
			checkGolden(t, c.name, m.View().Content)
		})
	}
}

// runningGoldenModel has four projects: an engine run in review, a relay
// started elsewhere, a paused run with a question and a stopped run.
func runningGoldenModel(theme Theme, w, h int) *Model {
	m := newGoldenModel(theme)
	now := time.Date(2026, 10, 19, 16, 42, 7, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	role := func(name string, began, ended time.Duration) source.EngineRole {
		r := source.EngineRole{Role: name, Round: 1, Vendor: "claude", StartedAt: ago(began), Exit: "ok"}
		if ended > 0 {
			r.EndedAt = ago(ended)
		}
		return r
	}
	review := source.EngineRun{ID: "run-1", Item: "S110", State: "review", CreatedAt: ago(12 * time.Minute), Activity: testActivity,
		Roles: []source.EngineRole{role("implementer", 11*time.Minute, 5*time.Minute), role("reviewer", 4*time.Minute, 0)}}
	question := source.EngineRun{ID: "run-2", Item: "S42", State: "paused", Pending: "question", Wait: "Which store?", CreatedAt: ago(20 * time.Minute),
		Roles: []source.EngineRole{role("implementer", 19*time.Minute, 0)}}
	stopped := source.EngineRun{ID: "run-3", Item: "S07", State: "stopped", Wait: "owner action", CreatedAt: ago(30 * time.Minute),
		Roles: []source.EngineRole{role("implementer", 29*time.Minute, 20*time.Minute), role("reviewer", 19*time.Minute, 15*time.Minute)}}
	external := relayWithSteps(
		slicercli.Step{ID: "s1", Role: "implementer", Phase: "done"},
		slicercli.Step{ID: "s2", Role: "reviewer", Phase: "done"},
		slicercli.Step{ID: "s3", Role: "gate", Phase: "start"})
	external.ID, external.Items = "ext-1", []string{"S97"}
	external.BeganAt, external.UpdatedAt = ago(9*time.Minute).Format(time.RFC3339), ago(2*time.Minute).Format(time.RFC3339)
	for _, s := range []source.Snapshot{
		engineSnap(four[0], review),
		snap(four[1], nil, nil, external),
		engineSnap(four[2], question),
		engineSnap(four[3], stopped),
	} {
		m.Update(snapshotMsg{name: s.Project.Name, snap: s})
	}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.Update(press("["))
	return m
}

func TestSummaryGoldenNamesEveryProject(t *testing.T) {
	v := sgr.ReplaceAllString(runningGoldenModel(NewTheme(true), 100, 30).View().Content, "")
	for _, p := range four {
		if !strings.Contains(mainRowOnly(v), p.Name) {
			t.Errorf("%s is missing from the Running view:\n%s", p.Name, v)
		}
	}
}

func mainRowOnly(view string) string {
	var b strings.Builder
	for _, line := range strings.Split(view, "\n") {
		_, main, _ := strings.Cut(line, "│")
		b.WriteString(main + "\n")
	}
	return b.String()
}

func TestSummaryShowsTheRoundOnTheStageBarAfterAFixRound(t *testing.T) {
	h := newHarness(t, runningProjects...)
	fix := roleRun("reviewer", 6*time.Minute, 3*time.Minute)
	fix.Exit = "fail"
	h.src.set(engineSnap(runningProjects[0], liveEngine("run-a", "S30", "implement", nil,
		roleRun("implementer", 9*time.Minute, 6*time.Minute), fix, roleRun("implementer", 3*time.Minute, 0))))
	h.start()
	h.send(press("["))
	row := mainRow(h.wide(), "alpha", "S30")
	if !strings.Contains(row, "[~~....] r2") || strings.Contains(row, "round") {
		t.Fatalf("a second round must read r2 on the stage bar, with no cell of its own: %q", row)
	}
}

// twoRunHarness has an engine run with a short item and a relay with a longer
// item and another running role, so the rows differ in width.
func twoRunHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, runningProjects...)
	h.src.set(engineSnap(runningProjects[0], liveEngine("run-a", "S9", "review", testActivity)))
	ext := externalRun("ext-b", "S110")
	ext.Steps = append(ext.Steps[:1:1], slicercli.Step{ID: "s2", Role: "reviewer", Phase: "done"}, slicercli.Step{ID: "s3", Role: "gate", Phase: "start"})
	h.src.set(snap(runningProjects[1], nil, nil, ext))
	h.start()
	h.send(press("["))
	return h
}

func (h *harness) at(w, ht int) string {
	h.m.Update(tea.WindowSizeMsg{Width: w, Height: ht})
	return plainView(h.m)
}

func TestSummaryColumnsLineUpAcrossRows(t *testing.T) {
	for _, size := range [][2]int{{160, 30}, {100, 30}, {80, 24}} {
		h := twoRunHarness(t)
		v := h.at(size[0], size[1])
		alpha, beta := mainRow(v, "alpha", "S9"), mainRow(v, "beta", "S110")
		for _, cell := range []string{"S", "[##", "heard"} {
			a, b := strings.Index(alpha, cell), strings.Index(beta, cell)
			if a < 0 || a != b {
				t.Errorf("%dx%d: column %q starts at %d and %d:\n%s\n%s", size[0], size[1], cell, a, b, alpha, beta)
			}
		}
		// The cell after the item starts in one column even though the items differ in width.
		if a, b := strings.Index(alpha, "[##"), strings.Index(beta, "[##"); a != b {
			t.Errorf("%dx%d: the stage bars do not line up", size[0], size[1])
		}
	}
}

// Every row shows the same cells, whatever the width: the set is chosen once,
// from the widest row, not per row (S110).
func TestSummaryRowsShowTheSameCellsAtEveryWidth(t *testing.T) {
	h := twoRunHarness(t)
	for w := 60; w <= 150; w++ {
		v := h.at(w, 30)
		alpha, beta := mainRow(v, "alpha", "S9"), mainRow(v, "beta", "S110")
		for _, cell := range []string{"heard", "implementer>"} {
			if strings.Contains(alpha, cell) != strings.Contains(beta, cell) {
				t.Fatalf("width %d: only one row shows %q:\n%s\n%s", w, cell, alpha, beta)
			}
		}
		if !strings.Contains(alpha, "[##") || !strings.Contains(beta, "[##") {
			t.Fatalf("width %d: a row lost its stage bar:\n%s\n%s", w, alpha, beta)
		}
	}
}

func TestOverviewTabIsLabelledRunningWhileTheRunningEntryIsSelected(t *testing.T) {
	h := runningHarness(t)
	for _, size := range [][2]int{{160, 30}, {80, 24}} {
		bar := strings.Split(h.at(size[0], size[1]), "\n")[0]
		_, main, _ := strings.Cut(bar, "│")
		if !strings.Contains(main, "Overview (Running)") || !strings.Contains(main, "Decisions") {
			t.Fatalf("%dx%d: the view bar must read Overview (Running) and keep every tab: %q", size[0], size[1], main)
		}
	}
	h.send(press("]"))
	if bar := strings.Split(h.at(100, 30), "\n")[0]; strings.Contains(bar, "(Running)") {
		t.Fatalf("the Needs you view must not carry the Running label: %q", bar)
	}
}

func TestSummaryDetailWrapsBetweenCellsOnly(t *testing.T) {
	h := runningHarness(t)
	for _, w := range []int{40, 50, 58, 72, 100} {
		h.at(80, 24)
		lines := h.m.overview(w).detail
		if len(lines) == 0 {
			t.Fatal("no detail line")
		}
		joined := strings.Join(lines, "\n")
		for _, whole := range []string{"heard 3m", "activity " + sparkline(testActivity, false), "implementer>[reviewer]>gate", "[##~~..]"} {
			if w >= 40 && !strings.Contains(joined, whole) {
				t.Errorf("width %d: %q is split across lines:\n%s", w, whole, joined)
			}
		}
		for i, line := range lines {
			if lipgloss.Width(line) > w {
				t.Errorf("width %d: line %d is %d wide: %q", w, i, lipgloss.Width(line), line)
			}
			if !strings.HasPrefix(line, indent) || strings.HasPrefix(line[len(indent):], " ") {
				t.Errorf("width %d: line %d starts with a stray space: %q", w, i, line)
			}
		}
	}
}
