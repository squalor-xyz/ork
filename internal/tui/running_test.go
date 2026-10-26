// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
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

func TestSummaryShowsTheFixRoundOnTheStageBar(t *testing.T) {
	h := newHarness(t, runningProjects...)
	fix := roleRun("reviewer", 6*time.Minute, 3*time.Minute)
	fix.Exit = "fail"
	h.src.set(engineSnap(runningProjects[0], liveEngine("run-a", "S30", "implement", nil,
		roleRun("implementer", 9*time.Minute, 6*time.Minute), fix, roleRun("implementer", 3*time.Minute, 0))))
	h.start()
	h.send(press("["))
	row := mainRow(h.wide(), "alpha", "S30")
	if !strings.Contains(row, "[~~....] fix 1") || strings.Contains(row, "round") || strings.Contains(row, " r2") {
		t.Fatalf("a fix round must read fix 1 on the stage bar, with no cell of its own: %q", row)
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

// Running rows at a glance (S145): two lines per run on the Overview. Line 1
// names the slice, the role working now with its model, the stage bar with its
// round and last heard; line 2 is that role's latest output line. The run id
// and "started elsewhere" are in the pane.

// glanceItems are the slices the three running runs work on. S41 has no short
// title, so its row falls back to the title.
func glanceItems() []slicercli.Item {
	return []slicercli.Item{
		{ID: "S30", Title: "Thirty is the full title of the slice", ShortTitle: "Thirty short", Status: "started"},
		{ID: "S41", Title: "Forty-one untitled", Status: "started"},
		{ID: "S52", Title: "Fifty-two is long", ShortTitle: "Fifty-two short", Status: "started"},
	}
}

// glanceHarness is one project with three running runs: an engine run in
// review whose reviewer has a tail, a relay started elsewhere whose running
// step has a task line, and one whose step has none.
func glanceHarness(t *testing.T) *harness {
	t.Helper()
	p := proj("ork", false)
	h := newHarness(t, p)
	impl, review := roleRun("implementer", 9*time.Minute, 4*time.Minute), roleRun("reviewer", 3*time.Minute, 0)
	impl.Model, review.Model = "sonnet", "opus"
	engine := liveEngine("run-eng", "S30", "review", testActivity, impl, review)
	engine.Tail = "reading the diff\nall four new tests fail for the stated reason\n"
	ext41 := externalRun("ext-41", "S41")
	ext41.Steps[1].Vendor, ext41.Steps[1].Model, ext41.Steps[1].Task = "claude", "haiku", "s41 reviewer"
	ext52 := externalRun("ext-52", "S52")
	ext52.Steps[1].Vendor, ext52.Steps[1].Model = "codex", "gpt-5"
	s := engineSnap(p, engine)
	s.Items.Value = glanceItems()
	s.Runs.Value = []slicercli.RelayRun{ext41, ext52}
	h.src.set(s)
	h.start()
	return h
}

// runningSection is the main column's lines under the Running heading, up to
// the next heading.
func runningSection(view string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(view, "\n") {
		main := line
		if _, after, ok := strings.Cut(line, "│"); ok {
			main = after
		}
		switch {
		case strings.HasPrefix(main, "Running"):
			in = true
		case in && strings.HasPrefix(main, "Recent"):
			return out
		case in:
			out = append(out, main)
		}
	}
	return out
}

func rowIndex(lines []string, words ...string) int {
next:
	for i, line := range lines {
		for _, w := range words {
			if !strings.Contains(line, w) {
				continue next
			}
		}
		return i
	}
	return -1
}

func TestRunningRowNamesSliceRoleAndModel(t *testing.T) {
	h := glanceHarness(t)
	sec := runningSection(h.at(100, 30))
	for _, tc := range []struct{ id, title, role string }{
		{"S30", "Thirty short", "reviewer opus"},
		{"S41", "Forty-one untitled", "reviewer haiku"},
		{"S52", "Fifty-two short", "reviewer gpt-5"},
	} {
		i := rowIndex(sec, tc.id)
		if i < 0 {
			t.Fatalf("no row for %s:\n%s", tc.id, strings.Join(sec, "\n"))
		}
		row := sec[i]
		for _, want := range []string{tc.title, tc.role, "[##~~..]", "heard "} {
			if !strings.Contains(row, want) {
				t.Errorf("%s: line 1 lacks %q: %q", tc.id, want, row)
			}
		}
		for _, gone := range []string{"started elsewhere", "run-eng", "ext-41", "ext-52", "steps running", "active"} {
			if strings.Contains(row, gone) {
				t.Errorf("%s: line 1 still shows %q: %q", tc.id, gone, row)
			}
		}
	}
	if row := sec[rowIndex(sec, "S30")]; strings.Contains(row, "Thirty is the full title") {
		t.Errorf("line 1 must show the short title, not the title: %q", row)
	}
}

func TestRunningRowShowsLatestOutputLine(t *testing.T) {
	h := glanceHarness(t)
	sec := runningSection(h.at(100, 30))
	// An engine run: the newest line of the S99 output tail, not the older one.
	i := rowIndex(sec, "S30")
	if i < 0 || i+1 >= len(sec) || !strings.Contains(sec[i+1], "all four new tests fail for the stated reason") {
		t.Fatalf("line 2 of the engine run must be the latest output line:\n%s", strings.Join(sec, "\n"))
	}
	if strings.Contains(strings.Join(sec, "\n"), "reading the diff") {
		t.Fatalf("only the latest output line is shown:\n%s", strings.Join(sec, "\n"))
	}
	// A relay started elsewhere has no output stream: the running step's task.
	i = rowIndex(sec, "S41")
	if i < 0 || i+1 >= len(sec) || !strings.Contains(sec[i+1], "s41 reviewer") {
		t.Fatalf("line 2 of a ledger-only run must be the running step's task:\n%s", strings.Join(sec, "\n"))
	}
	// No task and no tail: no line 2.
	i = rowIndex(sec, "S52")
	if i < 0 || (i+1 < len(sec) && strings.TrimSpace(sec[i+1]) != "") {
		t.Fatalf("a run with nothing to say has no second line:\n%s", strings.Join(sec, "\n"))
	}
	// An engine run with no tail yet has none either.
	h.m.byName["ork"].snap.EngineRuns[0].Tail = ""
	sec = runningSection(h.at(100, 30))
	if i = rowIndex(sec, "S30"); i < 0 || strings.Contains(sec[i+1], "all four") || !strings.Contains(sec[i+1], "S41") {
		t.Fatalf("an engine run without a tail has no second line:\n%s", strings.Join(sec, "\n"))
	}
}

func TestRunIdStaysInPane(t *testing.T) {
	h := glanceHarness(t)
	sec := strings.Join(runningSection(h.at(100, 30)), "\n")
	if strings.Contains(sec, "run-eng") || strings.Contains(sec, "ext-41") || strings.Contains(sec, "started elsewhere") {
		t.Fatalf("the rows must not carry the run id or started elsewhere:\n%s", sec)
	}
	h.send(press("enter"))
	if v := plainView(h.m); !strings.Contains(v, "Run run-eng") {
		t.Fatalf("the engine run's pane must name the run id:\n%s", v)
	}
	h.send(press("esc"))
	h.send(press("down"))
	h.send(press("enter"))
	v := plainView(h.m)
	if !strings.Contains(v, "Run ext-41") || !strings.Contains(v, "started elsewhere") {
		t.Fatalf("the relay's pane must name the run id and say started elsewhere:\n%s", v)
	}
}

// The title shortens first, then the model goes; the id, the role, the bar and
// last heard stay while there is room, and line 2 does not depend on the
// width (S145).
func TestRunningRowDropsTitleThenModel(t *testing.T) {
	h := glanceHarness(t)
	h.at(100, 30)
	_, running := h.m.rows()
	var row ovRow
	for _, r := range running {
		if r.ledgerID == "ext-41" {
			row = r
		}
	}
	if row.run == nil {
		t.Fatal("no run row for ext-41")
	}
	phase := func(text string) int {
		switch {
		case strings.Contains(text, "Forty-one untitled") && strings.Contains(text, "haiku"):
			return 0
		case strings.Contains(text, "...") && strings.Contains(text, "haiku"):
			return 1
		case strings.Contains(text, "Forty") && !strings.Contains(text, "haiku"):
			return 2
		case !strings.Contains(text, "Forty"):
			return 3
		}
		return -1
	}
	seen, last := map[int]bool{}, 0
	for room := 120; room >= 24; room-- {
		text := rowText(row, room)
		p := phase(text)
		if p < 0 || p < last {
			t.Fatalf("room %d: the cells went out of order (phase %d after %d): %q", room, p, last, text)
		}
		seen[p], last = true, p
		if !strings.Contains(text, "S41") || lipgloss.Width(text) > room {
			t.Fatalf("room %d: the id must stay and the row must fit: %q", room, text)
		}
		if p <= 2 && !strings.Contains(text, "reviewer") {
			t.Fatalf("room %d: the role must outlast the model and the title: %q", room, text)
		}
	}
	for p := 0; p <= 3; p++ {
		if !seen[p] {
			t.Errorf("no width showed phase %d", p)
		}
	}
	// At 80x24 line 2 stays.
	sec := runningSection(h.at(80, 24))
	if i := rowIndex(sec, "S30"); i < 0 || !strings.Contains(sec[i+1], "all four new tests fail") {
		t.Fatalf("line 2 must stay at 80x24:\n%s", strings.Join(sec, "\n"))
	}
}

func glanceGoldenModel(theme Theme, w, h int) *Model {
	m := newGoldenModel(theme)
	now := time.Date(2026, 10, 19, 16, 42, 7, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	role := func(name, model string, began, ended time.Duration) source.EngineRole {
		r := source.EngineRole{Role: name, Round: 1, Vendor: "claude", Model: model, StartedAt: ago(began), Exit: "ok"}
		if ended > 0 {
			r.EndedAt = ago(ended)
		}
		return r
	}
	fix := role("implementer", "sonnet", 3*time.Minute, 0)
	fix.Round = 2
	engine := source.EngineRun{ID: "run-1", Item: "S30", State: "implement", CreatedAt: ago(20 * time.Minute), Activity: testActivity,
		Tail:  "reading the diff\nupdating the golden views for the two-line rows\n",
		Roles: []source.EngineRole{role("implementer", "sonnet", 15*time.Minute, 8*time.Minute), role("reviewer", "opus", 8*time.Minute, 4*time.Minute), fix}}
	one := relayWithSteps(
		slicercli.Step{ID: "s1", Role: "implementer", Phase: "done"},
		slicercli.Step{ID: "s2", Role: "reviewer", Phase: "done"},
		slicercli.Step{ID: "s3", Role: "gate", Phase: "start", Vendor: "claude", Model: "opus", Task: "s41 gate"})
	one.ID, one.Items = "ext-1", []string{"S41"}
	one.BeganAt, one.UpdatedAt = ago(18*time.Minute).Format(time.RFC3339), ago(2*time.Minute).Format(time.RFC3339)
	two := relayWithSteps(
		slicercli.Step{ID: "s1", Role: "implementer", Phase: "start", Vendor: "codex", Model: "gpt-5", Task: "s52 implementer"})
	two.ID, two.Items = "ext-2", []string{"S52"}
	two.BeganAt, two.UpdatedAt = ago(40*time.Minute).Format(time.RFC3339), ago(14*time.Minute).Format(time.RFC3339)
	ork := engineSnap(four[0], engine)
	ork.Items.Value = glanceItems()
	other := snap(four[1], nil, glanceItems(), one, two)
	for _, s := range []source.Snapshot{ork, other, snap(four[2], nil, nil), snap(four[3], nil, nil)} {
		m.Update(snapshotMsg{name: s.Project.Name, snap: s})
	}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func TestGoldenRunningRowsAtAGlance(t *testing.T) {
	for _, c := range []struct {
		name string
		w, h int
		mono bool
	}{
		{"glance-100x30-color.txt", 100, 30, false},
		{"glance-100x30-mono.txt", 100, 30, true},
		{"glance-80x24-color.txt", 80, 24, false},
		{"glance-80x24-mono.txt", 80, 24, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := glanceGoldenModel(NewTheme(c.mono), c.w, c.h)
			checkGolden(t, c.name, m.View().Content)
		})
	}
}

// A stale run stays under Running, dimmed and labelled stale (S145: the label
// is a word before the role, since the state text left the row).
func TestStaleRunningRowSaysStale(t *testing.T) {
	p := proj("ork", false)
	h := newHarness(t, p)
	r := externalRun("ext-old", "S41")
	r.State = "stale"
	r.UpdatedAt = t0.Add(-35 * time.Minute).Format(time.RFC3339)
	h.src.set(snap(p, nil, nil, r))
	h.start()
	sec := runningSection(h.at(100, 30))
	i := rowIndex(sec, "S41")
	if i < 0 || !strings.Contains(sec[i], "stale reviewer") || !strings.Contains(sec[i], "!! heard 35m") {
		t.Fatalf("a stale run must say stale before its role and keep the red cue:\n%s", strings.Join(sec, "\n"))
	}
}

// The cursor on the last of many runs keeps that run's second line on screen.
func TestSelectedRunSecondLineScrollsIntoView(t *testing.T) {
	p := proj("ork", false)
	h := newHarness(t, p)
	var runs []slicercli.RelayRun
	for i := 0; i < 8; i++ {
		r := externalRun(fmt.Sprintf("ext-%d", i), fmt.Sprintf("S%d", 40+i))
		r.Steps[1].Task = fmt.Sprintf("task line %d", i)
		runs = append(runs, r)
	}
	h.src.set(snap(p, nil, nil, runs...))
	h.start()
	for height := 12; height <= 20; height++ {
		h.m.Update(tea.WindowSizeMsg{Width: 100, Height: height})
		for i := 0; i < 7; i++ {
			h.send(press("down"))
		}
		// The detail line repeats the task, so look at the line under the row.
		v := plainView(h.m)
		lines := mainLines(v)
		if i := lineWith(lines, 0, "S47", "reviewer"); i < 0 || i+1 >= len(lines) || !strings.Contains(lines[i+1], "task line 7") {
			t.Fatalf("height %d: the last run's second line must sit under its row on screen:\n%s", height, v)
		}
		for i := 0; i < 7; i++ {
			h.send(press("up"))
		}
	}
}

// An engine run's fix round after a gate FAIL: the engine ends the gate step
// done, so the row, the pane and the Running entry read the round from the
// engine's state, implement, and not from the gate's phase (S145).
func TestEngineRowCountsTheFixRoundAfterAGateFail(t *testing.T) {
	roles := func() []source.EngineRole {
		fix := roleRun("implementer", 6*time.Minute, 4*time.Minute)
		fix.Round = 2
		third := roleRun("implementer", 2*time.Minute, 0)
		third.Round = 3
		return []source.EngineRole{
			roleRun("implementer", 12*time.Minute, 9*time.Minute),
			roleRun("reviewer", 9*time.Minute, 7*time.Minute),
			fix,
			roleRun("gate", 4*time.Minute, 2*time.Minute),
			third,
		}
	}
	for _, tc := range []struct{ state, want, not string }{
		{"implement", "fix 2", ""},
		{"finish", "fix 1", "fix 2"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			p := proj("ork", false)
			h := newHarness(t, p)
			run := liveEngine("run-eng", "S30", tc.state, nil, roles()...)
			h.src.set(engineSnap(p, run))
			h.start()
			row := mainRow(h.at(100, 30), "S30", "implementer")
			if !strings.Contains(row, "] "+tc.want) || (tc.not != "" && strings.Contains(row, tc.not)) {
				t.Fatalf("the row must read %q in state %s: %q", tc.want, tc.state, row)
			}
			h.send(press("enter"))
			if v := plainView(h.m); !strings.Contains(v, "] "+tc.want+"  last heard") {
				t.Fatalf("the pane bar must read %q in state %s:\n%s", tc.want, tc.state, v)
			}
			h.send(press("esc"))
			h.send(press("["))
			if row := mainRow(h.at(160, 30), "ork", "S30"); !strings.Contains(row, "] "+tc.want) {
				t.Fatalf("the Running entry must read %q in state %s: %q", tc.want, tc.state, row)
			}
		})
	}
}

// The gate is named once on line 1: by the role cell, not by the round word
// too. The pane bar and the Running entry keep the word.
func TestGateRowNamesTheGateOnce(t *testing.T) {
	p := proj("ork", false)
	h := newHarness(t, p)
	r := relayWithSteps(
		slicercli.Step{ID: "s1", Role: "implementer", Phase: "done"},
		slicercli.Step{ID: "s2", Role: "reviewer", Phase: "done"},
		slicercli.Step{ID: "s3", Role: "gate", Phase: "start", Model: "opus"})
	r.ID, r.Items = "ext-g", []string{"S41"}
	r.UpdatedAt = t0.Add(-time.Minute).Format(time.RFC3339)
	h.src.set(snap(p, nil, nil, r))
	h.start()
	sec := runningSection(h.at(100, 30))
	i := rowIndex(sec, "S41")
	if i < 0 || !strings.Contains(sec[i], "gate opus  [####~~]") || strings.Count(sec[i], "gate") != 1 {
		t.Fatalf("line 1 must name the gate once:\n%s", strings.Join(sec, "\n"))
	}
	// The word stays when the gate is not the working role: a UI pass started after it.
	other := r
	other.ID = "ext-u"
	other.Items = []string{"S42"}
	other.Steps = append(append([]slicercli.Step(nil), r.Steps...), slicercli.Step{ID: "s4", Role: "ui", Phase: "start"})
	h.src.set(snap(p, nil, nil, r, other))
	h.send(press("r"))
	sec = runningSection(h.at(100, 30))
	if i := rowIndex(sec, "S42"); i < 0 || !strings.Contains(sec[i], "ui  [####~~] gate") {
		t.Fatalf("the gate word stays while another role works:\n%s", strings.Join(sec, "\n"))
	}
	h.send(press("enter"))
	if v := plainView(h.m); !strings.Contains(v, "[####~~] gate  last heard") {
		t.Fatalf("the pane bar keeps the gate word:\n%s", v)
	}
	h.send(press("esc"))
	h.send(press("["))
	if row := mainRow(h.at(160, 30), "ork", "S41"); !strings.Contains(row, "[####~~] gate") {
		t.Fatalf("the Running entry keeps the gate word: %q", row)
	}
}

// With no role working, the state word in the role cell is not drawn as a role.
func TestStateWordIsDimNotTheRunningColour(t *testing.T) {
	p := proj("ork", false)
	h := newHarness(t, p)
	h.m.cfg.Theme = NewTheme(false)
	th := h.m.cfg.Theme
	done := roleRun("implementer", 9*time.Minute, 4*time.Minute)
	h.src.set(engineSnap(p, liveEngine("run-eng", "S30", "checks", nil, done)))
	h.start()
	_, rows := h.m.rows()
	line := h.m.rowLine(rows[0], false, 3, 200)
	if !strings.Contains(line, th.Dim.Render("checks")) || strings.Contains(line, th.Running.Render("checks")) {
		t.Fatalf("the state word must be dim: %q", line)
	}
	// A working role keeps the running colour.
	h.src.set(engineSnap(p, liveEngine("run-eng", "S30", "review", nil)))
	h.send(press("r"))
	_, rows = h.m.rows()
	if line := h.m.rowLine(rows[0], false, 3, 200); !strings.Contains(line, th.Running.Render("reviewer")) {
		t.Fatalf("a working role must be in the running colour: %q", line)
	}
}
