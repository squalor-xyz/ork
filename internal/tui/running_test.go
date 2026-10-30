// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/adjcli"
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
	running, needs := sidebarLine(v, "  Running"), sidebarLine(v, "0>Needs you")
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
	if after := h.view(); sidebarLine(after, "0 Needs you") != needs {
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
	if sidebarLine(v, "  Running") < 0 || sidebarLine(v, "0>Needs you") < 0 {
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
	if head := mainRow(v, "Running 3"); !strings.Contains(head, "? owner questions 1") || !strings.Contains(head, "x stopped 1") {
		t.Errorf("the heading must count the question and the stop: %q", head)
	}
	// The alert is one glyph in the mark slot before the project name, as the
	// kind symbol is on Needs you, and a space follows it. No row is wider or
	// moved by it.
	q := mainRow(v, "alpha", "S30")
	if !strings.HasPrefix(q, " ? alpha") || !strings.Contains(q, "[reviewer]") {
		t.Errorf("the paused run's row lacks its ? glyph before the name, or its pipeline: %q", q)
	}
	s := mainRow(v, "beta", "S31")
	if !strings.HasPrefix(s, " x beta") {
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
	if row := mainRow(v, "alpha", "S30"); !strings.HasPrefix(row, " ? alpha") {
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
func glanceHarness(t *testing.T) *harness { return glanceHarnessIn(t, "ork") }

// glanceHarnessIn is glanceHarness for a project of that name; a long name
// leaves line 1 too little room for the titles.
func glanceHarnessIn(t *testing.T, name string) *harness {
	t.Helper()
	p := proj(name, false)
	h := newHarness(t, p)
	impl, review := roleRun("implementer", 9*time.Minute, 4*time.Minute), roleRun("reviewer", 3*time.Minute, 0)
	impl.Model, review.Model = "sonnet", "opus"
	engine := liveEngine("run-eng", "S30", "review", testActivity, impl, review)
	engine.Tail = "reading the diff\nall four new tests fail for the stated reason\n"
	ext41 := externalRun("ext-41", "S41")
	ext41.Steps[1].Vendor, ext41.Steps[1].Model, ext41.Steps[1].Task = "claude", "haiku", "s41 reviewer round 2: rechecking the golden views"
	ext52 := externalRun("ext-52", "S52")
	ext52.Steps[1].Vendor, ext52.Steps[1].Model, ext52.Steps[1].Task = "codex", "gpt-5", "s52 reviewer"
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
	if i < 0 || i+1 >= len(sec) || !strings.Contains(sec[i+1], "round 2: rechecking the golden views") || strings.Contains(sec[i+1], "s41 reviewer") {
		t.Fatalf("line 2 of a ledger-only run must be the running step's task:\n%s", strings.Join(sec, "\n"))
	}
	// A task that is only "<id> <role>" says nothing line 1 does not: no line 2.
	// No task and no tail: no line 2 either.
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
		case !strings.Contains(text, "Forty") && strings.Contains(text, "haiku"):
			// The title moved to line 2, so the model comes back if it fits.
			return 3
		case !strings.Contains(text, "Forty"):
			return 4
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
		if p <= 4 && !strings.Contains(text, "reviewer") {
			t.Fatalf("room %d: the role must outlast the model and the title: %q", room, text)
		}
	}
	for p := 0; p <= 4; p++ {
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
		slicercli.Step{ID: "s3", Role: "gate", Phase: "start", Vendor: "claude", Model: "opus", Task: "s41 gate round 2: rerunning the checks"})
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
		{"glance-120x40-mono.txt", 120, 40, true},
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
	if i := rowIndex(sec, "S42"); i < 0 || !strings.Contains(sec[i], "ui") || !strings.Contains(sec[i], "[####~~] gate") {
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

// A relay started elsewhere sets its step task to a label, "<id> <role>". When
// that is all the task says, line 2 would repeat line 1, so it is hidden; a task
// that says more is shown (S145, owner decision).
func TestLedgerTaskLabelIsHiddenWhenItOnlyRepeatsIdAndRole(t *testing.T) {
	for _, tc := range []struct {
		items []string
		task  string
		want  string // "" when line 2 is hidden
	}{
		{[]string{"S52"}, "s52 implementer", ""},
		{[]string{"S52"}, "S52 Implementer", ""},
		{[]string{"S52"}, "  s52   implementer  ", ""},
		{[]string{"S52"}, "s52-implementer", ""},
		{[]string{"S52"}, "s52_implementer", ""},
		{[]string{"S52"}, "implementer", ""},
		{[]string{"S52"}, "s52", ""},
		{[]string{"S52", "S53"}, "s52,s53 implementer", ""},
		{[]string{"S52", "S53"}, "s52 s53 implementer", ""},
		{[]string{"S52"}, "s52 implementer:", ""},
		// The leading label is cut: it is on line 1 already.
		{[]string{"S52"}, "s52 implementer fix round 1", "fix round 1"},
		{[]string{"S52"}, "s52 implementer finish", "finish"},
		{[]string{"S52"}, "S52-Implementer: finish", "finish"},
		{[]string{"S52"}, "s52 implementer - fix round 2: x", "fix round 2: x"},
		{[]string{"S52", "S53"}, "s52,s53 implementer fix round 1", "fix round 1"},
		// Anything else is shown whole.
		{[]string{"S52"}, "s53 implementer", "s53 implementer"},
		{[]string{"S52"}, "s52 implementerx", "s52 implementerx"},
		{[]string{"S52"}, "s52 reviewer", "s52 reviewer"},
		{[]string{"S52"}, "Implement S52 in the worktree", "Implement S52 in the worktree"},
	} {
		t.Run(fmt.Sprintf("%v/%s", tc.items, tc.task), func(t *testing.T) {
			p := proj("ork", false)
			h := newHarness(t, p)
			r := relayWithSteps(slicercli.Step{ID: "s1", Role: "implementer", Phase: "start", Task: tc.task})
			r.ID, r.Items = "ext", tc.items
			h.src.set(snap(p, nil, nil, r))
			h.start()
			_, rows := h.m.rows()
			if got := rows[0].run.second; got != tc.want {
				t.Fatalf("line 2 = %q, want %q", got, tc.want)
			}
		})
	}
}

// One column fit for the whole Running section (S145): the tightest row decides
// which cells every row keeps, and the cells line up, so the bars, round words
// and last heard stand in one column.
func TestRunningRowsShareOneColumnFit(t *testing.T) {
	h := glanceHarness(t)
	for w := 50; w <= 160; w++ {
		sec := runningSection(h.at(w, 30))
		var rows []string
		for _, id := range []string{"S30", "S41", "S52"} {
			if i := rowIndex(sec, id); i >= 0 {
				rows = append(rows, sec[i])
			}
		}
		if len(rows) != 3 {
			t.Fatalf("width %d: %d rows", w, len(rows))
		}
		for _, cell := range []string{"[", "heard", "reviewer", "opus", "haiku", "gpt-5"} {
			at := strings.Index(rows[0], cell)
			for _, row := range rows[1:] {
				// A model belongs to one row; every other cell is in all or none.
				if cell == "opus" || cell == "haiku" || cell == "gpt-5" {
					continue
				}
				if (strings.Index(row, cell) >= 0) != (at >= 0) {
					t.Fatalf("width %d: only some rows show %q:\n%s", w, cell, strings.Join(rows, "\n"))
				}
			}
		}
		bar, role := strings.Index(rows[0], "[##"), strings.Index(rows[0], "reviewer")
		for _, row := range rows[1:] {
			if strings.Index(row, "[##") != bar || strings.Index(row, "reviewer") != role {
				t.Fatalf("width %d: the columns do not line up:\n%s", w, strings.Join(rows, "\n"))
			}
		}
		if heard := strings.Index(rows[0], "heard"); heard >= 0 {
			for _, row := range rows[1:] {
				if strings.Index(row, "heard") != heard && strings.Index(row, "! heard") != heard-2 {
					t.Fatalf("width %d: last heard does not line up:\n%s", w, strings.Join(rows, "\n"))
				}
			}
		}
	}
}

// A shortened title keeps at least 12 columns; below that the title goes (S145).
func TestRunningTitleIsDroppedBelowTwelveColumns(t *testing.T) {
	h := glanceHarness(t)
	dropped, clipped := false, false
	for w := 50; w <= 160; w++ {
		sec := runningSection(h.at(w, 30))
		i := rowIndex(sec, "S52")
		if i < 0 {
			t.Fatalf("width %d: no row", w)
		}
		_, after, _ := strings.Cut(sec[i], "S52  ")
		cell, _, _ := strings.Cut(after, "  ")
		switch {
		case strings.Contains(cell, "..."):
			clipped = true
			if lipgloss.Width(cell) < 12 {
				t.Fatalf("width %d: a title shortened to %d columns: %q", w, lipgloss.Width(cell), cell)
			}
		case !strings.HasPrefix(cell, "Fifty"):
			dropped = true
		}
	}
	if !dropped || !clipped {
		t.Fatalf("the sweep must show a shortened title (%v) and a dropped one (%v)", clipped, dropped)
	}
}

// The selected run is drawn reversed on both lines, and line 2 is not dimmed
// (S145). Mono keeps the reverse attribute, so it stays legible without colour.
func TestSelectedRunDrawsBothLinesReversed(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h := glanceHarness(t)
		h.m.cfg.Theme = NewTheme(mono)
		th := h.m.cfg.Theme
		h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		body := h.m.overview(73).body
		var i int
		for i = range body {
			if strings.Contains(sgr.ReplaceAllString(body[i], ""), "S30") {
				break
			}
		}
		line2 := sgr.ReplaceAllString(body[i+1], "")
		if !strings.Contains(line2, "all four new tests fail") {
			t.Fatalf("mono %v: no second line under the selected row: %q", mono, line2)
		}
		if want := th.TabActive.Bold(false).Render(line2); body[i+1] != want {
			t.Errorf("mono %v: the selected run's second line must be reversed whole:\n got %q\nwant %q", mono, body[i+1], want)
		}
		if body[i+1] == th.Dim.Render(strings.TrimSpace(line2)) || strings.Contains(body[i+1], th.Dim.Render("all four")) {
			t.Errorf("mono %v: the selected run's second line is dimmed: %q", mono, body[i+1])
		}
		if lipgloss.Width(body[i+1]) != lipgloss.Width(body[i]) {
			t.Errorf("mono %v: the two lines differ in width", mono)
		}
		// An unselected run's second line stays dim.
		j := i + 2
		for ; j < len(body); j++ {
			if strings.Contains(sgr.ReplaceAllString(body[j], ""), "S41") {
				break
			}
		}
		if !strings.Contains(body[j+1], th.Dim.Render("round 2: rechecking the golden views")) {
			t.Errorf("an unselected second line must stay dim: %q", body[j+1])
		}
	}
}

// A row with no model still lines up with rows that have one: the role and model
// share one padded column.
func TestRunningRowsAlignWithAndWithoutAModel(t *testing.T) {
	p := proj("ork", false)
	h := newHarness(t, p)
	a, b := externalRun("ext-a", "S41"), externalRun("ext-b", "S42")
	a.Steps[1].Model = "sonnet"
	h.src.set(snap(p, nil, nil, a, b))
	h.start()
	for w := 70; w <= 140; w += 10 {
		sec := runningSection(h.at(w, 30))
		i, j := rowIndex(sec, "S41"), rowIndex(sec, "S42")
		if i < 0 || j < 0 || strings.Index(sec[i], "[##") != strings.Index(sec[j], "[##") || strings.Index(sec[i], "[##") < 0 {
			t.Fatalf("width %d: the bars do not line up:\n%s", w, strings.Join(sec, "\n"))
		}
	}
}

// Where the title does not fit on line 1, the whole section moves it to line 2:
// line 1 is status only (the model comes back if it fits), and line 2 is the
// title, then two spaces and the role's latest line. A run with no latest line gets
// the title alone, so it has a line 2 even when its ledger label is bare
// (S145, owner decision).
func TestTitleMovesToLineTwoForTheWholeSection(t *testing.T) {
	h := glanceHarness(t)
	type sample struct {
		below, model bool
	}
	seen := map[sample]bool{}
	for w := 50; w <= 160; w++ {
		sec := runningSection(h.at(w, 30))
		var line1, line2 []string
		for _, id := range []string{"S30", "S41", "S52"} {
			i := rowIndex(sec, id)
			if i < 0 || i+1 >= len(sec) {
				t.Fatalf("width %d: no row for %s", w, id)
			}
			line1, line2 = append(line1, sec[i]), append(line2, sec[i+1])
		}
		titles := []string{"Thirty short", "Forty-one untitled", "Fifty-two short"}
		on1 := 0
		for i, t1 := range titles {
			if strings.Contains(line1[i], t1) || strings.Contains(line1[i], t1[:8]) {
				on1++
			}
		}
		if on1 != 0 && on1 != 3 {
			t.Fatalf("width %d: only some titles are on line 1:\n%s", w, strings.Join(line1, "\n"))
		}
		if on1 == 3 {
			seen[sample{false, true}] = true
			if strings.Contains(line2[2], "Fifty") {
				t.Fatalf("width %d: a title on line 1 is also on line 2: %q", w, line2[2])
			}
			continue
		}
		// Moved: line 2 starts with the title, then the latest line.
		// Each part is whole or a cut of itself; a title that crowds its output
		// is cut (S150).
		cutOf := func(got, want string) bool {
			return got == want || (strings.HasSuffix(got, "...") && strings.HasPrefix(want, strings.TrimSuffix(got, "...")))
		}
		for i, want := range [][2]string{{"Thirty short", "all four new tests fail for the stated reason"}, {"Forty-one untitled", "round 2: rechecking the golden views"}, {"Fifty-two short", ""}} {
			title, output, _ := strings.Cut(strings.TrimSpace(line2[i]), "  ")
			if !cutOf(title, want[0]) || (want[1] == "") != (output == "") || (output != "" && !cutOf(strings.TrimSpace(output), want[1])) {
				t.Fatalf("width %d: line 2 of row %d is %q, want %q, two spaces, %q (or cuts of them)", w, i, line2[i], want[0], want[1])
			}
		}
		model := strings.Contains(line1[0], "reviewer opus")
		seen[sample{true, model}] = true
		if model != strings.Contains(line1[1], "reviewer haiku") {
			t.Fatalf("width %d: the model is on only some rows:\n%s", w, strings.Join(line1, "\n"))
		}
	}
	if !seen[sample{false, true}] || !seen[sample{true, true}] || !seen[sample{true, false}] {
		t.Fatalf("the sweep must show the title on line 1, on line 2 with the model back, and on line 2 without it: %v", seen)
	}
}

// The selected run's title-only second line scrolls into view and is reversed.
func TestSelectedTitleLineScrollsIntoView(t *testing.T) {
	p := proj("ork", false)
	h := newHarness(t, p)
	var runs []slicercli.RelayRun
	var items []slicercli.Item
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("S%d", 40+i)
		r := externalRun(fmt.Sprintf("ext-%d", i), id)
		r.Steps[1].Task = strings.ToLower(id) + " reviewer"
		runs = append(runs, r)
		items = append(items, slicercli.Item{ID: id, Title: fmt.Sprintf("Slice number %d has a long title", i), ShortTitle: fmt.Sprintf("Long short title %d", i)})
	}
	s := snap(p, nil, items, runs...)
	h.src.set(s)
	h.start()
	// The hint line takes a row, so 13 was the old 12 (S149). The detail line now
	// wraps between cells and takes a line more at this width (S146), so the
	// sweep starts at 14.
	for height := 14; height <= 20; height++ {
		h.m.Update(tea.WindowSizeMsg{Width: 70, Height: height})
		for i := 0; i < 7; i++ {
			h.send(press("down"))
		}
		v := plainView(h.m)
		lines := mainLines(v)
		if i := lineWith(lines, 0, "S47", "reviewer"); i < 0 || i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) != "Long short title 7" {
			t.Fatalf("height %d: the title line must sit under the last row on screen:\n%s", height, v)
		}
		for i := 0; i < 7; i++ {
			h.send(press("up"))
		}
	}
}

// The inline pane keeps the headings above its row when the row's second line
// is only the title.
func TestPaneUnderATitleOnlyRowKeepsTheHeadings(t *testing.T) {
	h := glanceHarnessIn(t, "a-long-project-name")
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(press("down"))
	h.send(press("down")) // S52: its second line is the title alone
	h.send(press("enter"))
	v := plainView(h.m)
	for _, want := range []string{"Needs you 0", "Running 3", "Fifty-two short", "esc close pane"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	lines := mainLines(v)
	if i := lineWith(lines, 0, "S52"); i < 0 || !strings.Contains(lines[i+1], "Fifty-two short") || !strings.Contains(lines[i+2], "Run ext-52") {
		t.Fatalf("the pane must sit directly under the title line:\n%s", v)
	}
}

// A long pane under a run whose second line is only its title still leaves the
// headings above it on screen: the pane gives that line back (S145).
func TestLongPaneUnderATitleOnlyEngineRowKeepsTheHeadings(t *testing.T) {
	p := proj("a-long-project-name", false)
	h := newHarness(t, p)
	var roles []source.EngineRole
	for i := 0; i < 12; i++ {
		role := "implementer"
		if i%2 == 1 {
			role = "reviewer"
		}
		roles = append(roles, roleRun(role, time.Duration(30-2*i)*time.Minute, time.Duration(29-2*i)*time.Minute))
	}
	roles = append(roles, roleRun("implementer", time.Minute, 0))
	s := engineSnap(p, liveEngine("run-eng", "S30", "implement", nil, roles...))
	s.Items.Value = glanceItems()
	h.src.set(s)
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(press("enter"))
	v := plainView(h.m)
	for _, want := range []string{"Needs you 0", "Running 1", "Thirty short", "esc close pane"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
}

// body is the Overview body at that terminal size, with its styles.
func (h *harness) body(w, ht int) []string {
	h.m.Update(tea.WindowSizeMsg{Width: w, Height: ht})
	lw, _, sideW := h.m.layoutSize()
	return h.m.overview(max(lw-sideW, 1)).body
}

// bodyLine is the first body line whose plain text holds all words.
func bodyLine(t *testing.T, body []string, words ...string) int {
	t.Helper()
	i := rowIndex(plainLines(body), words...)
	if i < 0 {
		t.Fatalf("no body line with %q:\n%s", words, strings.Join(plainLines(body), "\n"))
	}
	return i
}

func plainLines(body []string) []string {
	out := make([]string, len(body))
	for i, l := range body {
		out[i] = sgr.ReplaceAllString(l, "")
	}
	return out
}

// clipHarness is one relay started elsewhere with a long title and a long task
// line, on a terminal where the title moves to line 2.
func clipHarness(t *testing.T, title string) *harness {
	t.Helper()
	p := proj("a-long-project-name", false)
	h := newHarness(t, p)
	r := externalRun("ext-60", "S60")
	r.Steps[1].Task = "s60 reviewer rechecking all four golden views carefully"
	s := snap(p, nil, []slicercli.Item{{ID: "S60", Title: title, ShortTitle: title}}, r)
	h.src.set(s)
	h.start()
	return h
}

// A title that would crowd the output off line 2 is cut so the output keeps 20
// columns: to the room left (never under 12 columns) once the title and the
// output do not fit side by side (S150, owner decision 2026-10-27).
func TestLongTitleOnLineTwoIsClippedToLeaveTheOutputRoom(t *testing.T) {
	long := "Make the very long slice title clip nicely"
	for _, size := range []struct{ w, ht int }{{70, 24}, {80, 24}, {90, 24}} {
		h := clipHarness(t, long)
		body := h.body(size.w, size.ht)
		plain := plainLines(body)
		i := bodyLine(t, plain, "S60")
		// The selected row's line 2 is padded to the pane: its width less the
		// indent is the room line 2 has.
		prefix := len(plain[i+1]) - len(strings.TrimLeft(plain[i+1], " "))
		avail := lipgloss.Width(body[i+1]) - prefix
		line2 := strings.TrimSpace(plain[i+1])
		title, rest, ok := strings.Cut(line2, "...  ")
		want := max(avail-2-20, 12)
		if !ok || lipgloss.Width(title+"...") != want || !strings.HasPrefix(long, title) {
			t.Fatalf("%dx%d: the title must be cut to %d columns, then two spaces: %q", size.w, size.ht, want, line2)
		}
		// The output keeps 20 columns, or what the 12 column floor leaves.
		if rest = strings.TrimSpace(rest); lipgloss.Width(rest) < min(20, avail-2-12) || !strings.HasPrefix("rechecking all four golden views carefully", strings.TrimSuffix(rest, "...")) {
			t.Fatalf("%dx%d: the output must keep at least 20 columns: %q", size.w, size.ht, line2)
		}
	}
}

// A title that fits beside its output is left whole, whatever its length; one
// that does not is cut for a short output too, and never under 12 columns.
func TestTitleIsWholeWhenItFitsBesideTheOutput(t *testing.T) {
	for _, tc := range []struct{ title, task, want string }{
		{"Twenty-four column title", "s60 reviewer all fine", "Twenty-four column title  all fine"},
		{"Thirty character slice title.", "s60 reviewer done", "Thirty character slice title.  done"},
	} {
		h := clipHarness(t, tc.title)
		h.m.byName["a-long-project-name"].snap.Runs.Value[0].Steps[1].Task = tc.task
		body := plainLines(h.body(90, 24))
		i := bodyLine(t, body, "S60")
		if line2 := strings.TrimSpace(body[i+1]); !strings.HasPrefix(line2, tc.want) {
			t.Errorf("title %q task %q: line 2 = %q, want it to start %q", tc.title, tc.task, line2, tc.want)
		}
	}
	// 41 + 2 + 4 columns do not fit in 44: the title gives way, the output stays.
	h := clipHarness(t, "Make the very long slice title clip nicely")
	h.m.byName["a-long-project-name"].snap.Runs.Value[0].Steps[1].Task = "s60 reviewer done"
	body := plainLines(h.body(90, 24))
	i := bodyLine(t, body, "S60")
	if line2 := strings.TrimSpace(body[i+1]); !strings.HasSuffix(line2, "...  done") || strings.Contains(line2, "title clip") {
		t.Errorf("a title that does not fit beside the output is cut: %q", line2)
	}
}

// open is the escape sequence a style starts with.
func open(st lipgloss.Style) string {
	r := st.Render("x")
	return r[:strings.Index(r, "x")]
}

// Line 2 draws the title in the default foreground and only the output dim, two
// spaces apart. A denial colours only its own text (S150).
func TestLineTwoStylesOnlyTheOutput(t *testing.T) {
	h := glanceHarnessIn(t, "a-long-project-name")
	h.m.cfg.Theme = NewTheme(false)
	th := h.m.cfg.Theme
	if open(th.Warn) == "" || open(th.Dim) == "" {
		t.Fatal("the colour theme draws no styles in this test")
	}
	h.m.byName["a-long-project-name"].snap.EngineRuns[0].Denials = []string{"Bash: git push origin main"}
	h.send(press("down")) // S41 is selected; S30 is not
	body := h.body(80, 24)
	plain := plainLines(body)
	for _, tc := range []struct{ id, title, output string }{
		{"S30", "Thirty short", "denied:"},
		{"S41", "Forty-one...", "round 2:"}, // the title is cut to leave the output its room
	} {
		i := bodyLine(t, plain, tc.id, "reviewer")
		if p := plain[i+1]; !strings.Contains(p, tc.title+"  "+tc.output) {
			t.Fatalf("%s: title, two spaces, output: %q", tc.id, p)
		}
	}
	// S30, unselected, with a denial: the title is plain, the denial amber.
	raw := strings.TrimLeft(body[bodyLine(t, plain, "S30", "reviewer")+1], " ")
	if !strings.HasPrefix(raw, "Thirty short  "+open(th.Warn)+"denied:") || strings.Contains(raw, open(th.Dim)) {
		t.Errorf("a denial colours only itself: %q", raw)
	}
	// S41, unselected... is selected here; S52 has only a title: plain, not dim.
	raw = strings.TrimLeft(body[bodyLine(t, plain, "S52", "reviewer")+1], " ")
	if !strings.HasPrefix(raw, "Fifty-two short") || strings.Contains(raw, open(th.Dim)) {
		t.Errorf("a title alone is in the default foreground: %q", raw)
	}
	h.send(press("up")) // back to S30
	h.send(press("down"))
	h.send(press("down")) // S52, so S41 is not selected
	raw = strings.TrimLeft(h.body(80, 24)[bodyLine(t, plain, "S41", "reviewer")+1], " ")
	if !strings.HasPrefix(raw, "Forty-one...  "+open(th.Dim)+"round 2:") {
		t.Errorf("only the output is dim: %q", raw)
	}
}

// The selected row keeps a denial's amber cue and line 2 is reversed but not
// bold (S150).
func TestSelectedDenialKeepsItsCueAndLineTwoIsNotBold(t *testing.T) {
	h := glanceHarnessIn(t, "a-long-project-name")
	h.m.cfg.Theme = NewTheme(false)
	th := h.m.cfg.Theme
	if open(th.Warn) == "" {
		t.Fatal("the colour theme draws no styles in this test")
	}
	h.m.byName["a-long-project-name"].snap.EngineRuns[0].Denials = []string{"Bash: git push origin main"}
	body := h.body(80, 24) // S30 is selected
	i := bodyLine(t, plainLines(body), "S30", "reviewer")
	line2 := plainLines(body)[i+1]
	if !strings.Contains(line2, "denied:") {
		t.Fatalf("no denial under the selected row: %q", line2)
	}
	// Both colours are set, so the line reverses to black on amber in any
	// terminal theme, not the terminal's own background on amber.
	want := th.Warn.Reverse(true).Background(lipgloss.Color("0")).Render(line2)
	if body[i+1] != want {
		t.Errorf("a selected denial must be reversed black on amber, not bold:\n got %q\nwant %q", body[i+1], want)
	}
	if !strings.Contains(body[i+1], "33") || !strings.Contains(body[i+1], "40") {
		t.Errorf("the denial line sets amber and black: %q", body[i+1])
	}
	// Monochrome has no colour to set: it stays reversed, not bold.
	h.m.cfg.Theme = NewTheme(true)
	body = h.body(80, 24)
	if got, want := body[i+1], h.m.cfg.Theme.Warn.Reverse(true).Render(line2); got != want {
		t.Errorf("mono: a selected denial is reversed only:\n got %q\nwant %q", got, want)
	}
}

// At the smallest body heights the scroll bump for a Running run's second line
// must not push the run's own first line out of the window (S150).
func TestScrollBumpNeverHidesLineOne(t *testing.T) {
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
	for height := 5; height <= 12; height++ {
		h.m.Update(tea.WindowSizeMsg{Width: 100, Height: height})
		for i := 0; i < 7; i++ {
			h.send(press("down"))
		}
		v := plainView(h.m)
		if i := lineWith(mainLines(v), 0, "S47", "reviewer"); i < 0 {
			t.Fatalf("height %d: the last run's first line is not on screen:\n%s", height, v)
		}
		for i := 0; i < 7; i++ {
			h.send(press("up"))
		}
	}
}

// Running view follow-ups (S140).

// An engine run with an error stays a live Running row, and its row carries the
// error marker in the mark slot, as the detail line carries the words.
func TestRunningRowKeepsErrorMarker(t *testing.T) {
	h := newHarness(t, runningProjects[0], runningProjects[1])
	bad := liveEngine("run-a", "S30", "review", testActivity)
	bad.Error = "vendor exited 2"
	h.src.set(engineSnap(runningProjects[0], bad))
	h.src.set(engineSnap(runningProjects[1], liveEngine("run-b", "S31", "review", testActivity)))
	h.start()
	h.send(press("["))
	v := h.wide()
	a, b := mainRow(v, "alpha", "S30"), mainRow(v, "beta", "S31")
	if !strings.HasPrefix(a, " ! alpha") {
		t.Errorf("the errored run's row lacks its ! marker before the name: %q", a)
	}
	if !strings.HasPrefix(b, "   beta") {
		t.Errorf("a healthy run carries a marker: %q", b)
	}
	if got := detailText(h.m, 150); !strings.Contains(got, "error: vendor exited 2") {
		t.Errorf("the detail line must keep the error in words: %q", got)
	}
	if strings.Index(a, "[##") != strings.Index(b, "[##") {
		t.Errorf("the marker moved the columns: %q against %q", a, b)
	}
}

// The count of the badge and the heading is the runs that are live. A relay
// ledger no run has touched for a while is stale and is listed but not counted;
// an engine run linked to a stale ledger follows the engine's state (S138).
func TestLiveRunCountIgnoresStaleRelayRuns(t *testing.T) {
	h := newHarness(t, runningProjects[0], runningProjects[1])
	linked := externalRun("ext-a", "S30")
	linked.State = "stale"
	engine := liveEngine("run-a", "S30", "review", testActivity)
	engine.RelayRun = "ext-a"
	s := engineSnap(runningProjects[0], engine)
	s.Runs.Value = []slicercli.RelayRun{linked}
	h.src.set(s)
	old := externalRun("ext-old", "S41")
	old.State = "stale"
	old.UpdatedAt = t0.Add(-35 * time.Minute).Format(time.RFC3339)
	h.src.set(snap(runningProjects[1], nil, nil, old))
	h.start()
	h.send(press("["))
	v := h.wide()
	if !strings.Contains(v, "Running 1") || strings.Contains(v, "Running 2") {
		t.Errorf("the heading must count the engine run and not the stale relay:\n%s", v)
	}
	if row := sidebarRow(h.m, "> Running"); !strings.Contains(compactSpaces(sgr.ReplaceAllString(row, "")), "> Running 1") {
		t.Errorf("the sidebar badge must count one live run: %q", row)
	}
	if mainRow(v, "beta", "S41") == "" {
		t.Errorf("the stale relay stays listed:\n%s", v)
	}
	if got := h.m.liveRunCount(); got != 1 {
		t.Errorf("liveRunCount = %d, want 1", got)
	}
}

// A run after a fix round reads the round on the bar before last heard, on the
// row and on the detail line.
func TestRunningDetailOrdersRoundBeforeHeard(t *testing.T) {
	h := newHarness(t, runningProjects[0])
	fix := roleRun("reviewer", 6*time.Minute, 3*time.Minute)
	fix.Exit = "fail"
	h.src.set(engineSnap(runningProjects[0], liveEngine("run-a", "S30", "implement", nil,
		roleRun("implementer", 9*time.Minute, 6*time.Minute), fix, roleRun("implementer", 3*time.Minute, 0))))
	h.start()
	h.send(press("["))
	row := mainRow(h.wide(), "alpha", "S30")
	if r, hd := strings.Index(row, "fix 1"), strings.Index(row, "heard"); r < 0 || hd < 0 || r > hd {
		t.Errorf("the row must show the round before last heard: %q", row)
	}
	got := detailText(h.m, 150)
	if r, hd := strings.Index(got, "fix 1"), strings.Index(got, "heard"); r < 0 || hd < 0 || r > hd {
		t.Errorf("the detail line must show the round before last heard: %q", got)
	}
}

// With the Running entry selected the cursor walks the Running rows, so a jump
// to a run lands on the index in those rows, not in the Overview's.
func TestSelectJumpRunUsesCursorRows(t *testing.T) {
	alpha, beta := proj("alpha", true), proj("beta", false)
	h := newHarness(t, alpha, beta)
	a := snap(alpha, []adjcli.Record{openRec(7, "ui.x", "A question")}, nil)
	h.src.set(a)
	h.src.set(engineSnap(beta, liveEngine("run-b", "S31", "review", testActivity)))
	h.start()
	h.send(press("["))
	if h.m.active != runningEntry {
		t.Fatalf("active %d, want the Running entry", h.m.active)
	}
	h.m.cursor, h.m.jumpRun = 3, "run-b"
	h.m.selectJumpRun()
	rows := h.m.cursorRows()
	if h.m.jumpRun != "" || h.m.cursor < 0 || h.m.cursor >= len(rows) || rows[h.m.cursor].engineID != "run-b" {
		t.Errorf("cursor %d (jumpRun %q) is not on run-b among %d Running rows", h.m.cursor, h.m.jumpRun, len(rows))
	}
}

// A stopped run's last role has ended; its row still names the role it stopped
// on, in the role cell and in the pipeline.
func TestStoppedRowShowsItsRole(t *testing.T) {
	h := newHarness(t, runningProjects[0])
	stopped := liveEngine("run-s", "S07", "stopped", nil,
		roleRun("implementer", 29*time.Minute, 20*time.Minute), roleRun("reviewer", 19*time.Minute, 15*time.Minute))
	stopped.Wait = "owner action"
	h.src.set(engineSnap(runningProjects[0], stopped))
	h.start()
	h.send(press("["))
	if row := mainRow(h.at(100, 30), "alpha", "S07"); !strings.Contains(row, "[reviewer]") {
		t.Errorf("at 100 columns the stopped row lacks its role: %q", row)
	}
	if row := mainRow(h.at(160, 30), "alpha", "S07"); !strings.Contains(row, "implementer>[reviewer]>gate") {
		t.Errorf("at 160 columns the stopped row's pipeline lacks its role: %q", row)
	}
	if row := mainRow(h.at(100, 30), "alpha", "S07"); !strings.Contains(row, "[####..]") {
		t.Errorf("the role must not change the stage bar: %q", row)
	}
}

func runningEmptyGoldenModel(theme Theme, w, h int) *Model {
	m := newGoldenModel(theme)
	for _, p := range four {
		s := snap(p, nil, nil)
		m.Update(snapshotMsg{name: p.Name, snap: s})
	}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.Update(press("["))
	return m
}

// The Running entry selected with nothing to run or to list under Recent.
func TestRunningSelectedEmptyGolden(t *testing.T) {
	for _, c := range []struct {
		name string
		mono bool
	}{{"running-empty-100x30-color.txt", false}, {"running-empty-100x30-mono.txt", true}} {
		t.Run(c.name, func(t *testing.T) {
			m := runningEmptyGoldenModel(NewTheme(c.mono), 100, 30)
			if m.active != runningEntry {
				t.Fatalf("active %d, want the Running entry", m.active)
			}
			checkGolden(t, c.name, m.View().Content)
		})
	}
}

// byProjectLive answers each project's wave.suggest with its own plan or error.
type byProjectLive struct {
	wavesLive
	plans map[string]*slicercli.ParallelPlan
	errs  map[string]error
}

func (l *byProjectLive) WaveSuggest(_ context.Context, p string) (*slicercli.ParallelPlan, error) {
	l.suggests = append(l.suggests, p)
	return l.plans[p], l.errs[p]
}

func parsePlan(t *testing.T, doc string) *slicercli.ParallelPlan {
	t.Helper()
	var plan slicercli.ParallelPlan
	if err := json.Unmarshal([]byte(doc), &plan); err != nil {
		t.Fatal(err)
	}
	return &plan
}

// combinedHarness has two adj projects with a decision and an item each, on the
// Running entry, with a wave plan per project.
func combinedHarness(t *testing.T, live func(plans map[string]*slicercli.ParallelPlan) *byProjectLive) (*harness, *byProjectLive) {
	t.Helper()
	alpha, beta := proj("alpha", true), proj("beta", true)
	h := newHarness(t, alpha, beta)
	h.src.set(snap(alpha, []adjcli.Record{openRec(7, "ui.alpha-key", "Alpha question")}, []slicercli.Item{item("S30", "Alpha item")}))
	h.src.set(snap(beta, []adjcli.Record{openRec(8, "ui.beta-key", "Beta question")}, []slicercli.Item{item("S41", "Beta item")}))
	h.start()
	l := live(map[string]*slicercli.ParallelPlan{
		"alpha": parsePlan(t, `{"waves":[{"wave":1,"items":[{"id":"A","title":"Alpha first"}]},{"wave":2,"items":[{"id":"B","title":"Alpha second"}]}],"unplaced":[{"id":"E","title":"Alpha loose"}]}`),
		"beta":  parsePlan(t, `{"waves":[{"wave":1,"items":[{"id":"C","title":"Beta first"}]}],"unplaced":[{"id":"D","title":"Beta loose","reason":"no_files"}]}`),
	})
	h.m.cfg.Live = l
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	h.send(press("["))
	if h.m.active != runningEntry {
		t.Fatalf("active %d, want the Running entry", h.m.active)
	}
	return h, l
}

func plainLive(plans map[string]*slicercli.ParallelPlan) *byProjectLive {
	return &byProjectLive{plans: plans}
}

func (h *harness) tabTo(view int) {
	h.t.Helper()
	for i := 0; h.m.view != view; i++ {
		if i > 10 {
			h.t.Fatalf("tab never reached view %d", view)
		}
		h.deliver(press("tab"))
	}
}

// Questions, Waves, Roadmap and Decisions show every project together while the
// Running entry is selected (S140, owner 2026-10-27). Questions, Decisions and
// Roadmap lead each row with its project; Waves has one block per project.
func TestRunningEntryTabsShowAllProjects(t *testing.T) {
	h, l := combinedHarness(t, plainLive)
	for _, tc := range []struct {
		view  int
		name  string
		lines [][]string // words each expected line holds
	}{
		{questionsView, "Questions", [][]string{{"alpha", "#7", "Alpha question"}, {"beta", "#8", "Beta question"}}},
		{roadmapView, "Roadmap", [][]string{{"alpha", "S30", "Alpha item"}, {"beta", "S41", "Beta item"}}},
		{decisionsView, "Decisions", [][]string{{"alpha", "#7", "Alpha question"}, {"beta", "#8", "Beta question"}}},
	} {
		h.tabTo(tc.view)
		v := plainView(h.m)
		for _, words := range tc.lines {
			if mainRow(v, words...) == "" {
				t.Errorf("%s: no row holds %q while Running is selected:\n%s", tc.name, words, v)
			}
		}
	}
	if !reflect.DeepEqual(l.suggests, []string{"alpha", "beta"}) {
		t.Errorf("Waves must ask each project once: %v", l.suggests)
	}
	// Nothing on the combined Waves or Roadmap opens or launches a project.
	h.tabTo(wavesView)
	h.deliver(press(" "))
	if w := h.m.waves; w == nil || len(w.picked) != 0 || w.inputs != nil {
		t.Fatalf("space must not tick a row of the combined Waves: %+v", w)
	}
	h.deliver(press("enter"))
	if w := h.m.waves; w == nil || w.pane != nil {
		t.Fatalf("enter must not open an item of the combined Waves: %+v", w)
	}
	h.tabTo(roadmapView)
	h.deliver(press("enter"))
	if h.m.itemPane != nil {
		t.Errorf("Enter on the combined Roadmap must not open an item")
	}
}

// The combined Waves tab is one block per project: the project's name, then its
// own waves and unplaced items, in project order. Nothing is ticked there.
func TestRunningWavesGroupByProject(t *testing.T) {
	h, _ := combinedHarness(t, plainLive)
	h.tabTo(wavesView)
	lines := mainLines(plainView(h.m))
	at := -1
	for _, step := range [][]string{{"alpha"}, {"Wave 1"}, {"A", "Alpha first"}, {"Wave 2"}, {"B", "Alpha second"},
		{"Unplaced"}, {"E", "Alpha loose"}, {"beta"}, {"Wave 1"}, {"C", "Beta first"}, {"Unplaced"}, {"D", "Beta loose"}} {
		i := lineWith(lines, at+1, step...)
		if i < 0 {
			t.Fatalf("no line %q after line %d:\n%s", step, at, strings.Join(lines, "\n"))
		}
		at = i
	}
	for _, line := range lines {
		if strings.Contains(line, "[ ]") || strings.Contains(line, "[x]") {
			t.Errorf("the read-only Waves tab must not draw tick boxes: %q", line)
		}
	}
	if got := len(h.m.waves.rows()); got != 5 {
		t.Errorf("the cursor walks %d rows, want the 5 items of both projects", got)
	}
	// Each wave and each Unplaced list of every project starts a group.
	if got := h.m.groupStarts(); !reflect.DeepEqual(got, []int{0, 1, 2, 3, 4}) {
		t.Errorf("group starts = %v, want [0 1 2 3 4]", got)
	}
	h.deliver(press("G"))
	if h.m.waves.cursor != 4 {
		t.Errorf("G must reach the last item: cursor %d", h.m.waves.cursor)
	}
}

// A project whose suggestion fails is named with its error; the others still
// list. When every project fails each one is named.
func TestRunningWavesNameFailedProjects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		errs   map[string]error
		listed []string
		failed []string
	}{
		{"one", map[string]error{"beta": errors.New("slicer plan-parallel exploded")}, []string{"Alpha first"}, []string{"beta"}},
		{"all", map[string]error{"alpha": errors.New("alpha broke"), "beta": errors.New("beta broke")}, nil, []string{"alpha", "beta"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := combinedHarness(t, func(plans map[string]*slicercli.ParallelPlan) *byProjectLive {
				return &byProjectLive{plans: plans, errs: tc.errs}
			})
			h.tabTo(wavesView)
			lines := mainLines(plainView(h.m))
			for _, title := range tc.listed {
				if lineWith(lines, 0, title) < 0 {
					t.Errorf("%q is missing:\n%s", title, strings.Join(lines, "\n"))
				}
			}
			if len(tc.listed) == 0 && lineWith(lines, 0, "No suggested items") >= 0 {
				t.Errorf("\"No suggested items\" under the error lines:\n%s", strings.Join(lines, "\n"))
			}
			for _, project := range tc.failed {
				if lineWith(lines, 0, project) < 0 || lineWith(lines, 0, "!", tc.errs[project].Error()) < 0 {
					t.Errorf("failed project %s is not named with its error:\n%s", project, strings.Join(lines, "\n"))
				}
			}
		})
	}
}

// Enter opens nothing on the combined Roadmap and Waves, so the hint line does
// not offer it; `e` explains that it needs a project.
func TestCombinedTabsDoNotOfferWhatTheyCannotDo(t *testing.T) {
	h, _ := combinedHarness(t, plainLive)
	for _, view := range []int{roadmapView, wavesView} {
		h.tabTo(view)
		hint := sgr.ReplaceAllString(h.m.hintLine(200), "")
		if strings.Contains(hint, "enter") {
			t.Errorf("view %d: the hint offers enter: %q", view, hint)
		}
	}
	h.deliver(press("e"))
	if got := h.m.actionStatus; !strings.Contains(got, "Select a project") {
		t.Errorf("e on the combined Waves must explain, got %q", got)
	}
}

// The alert glyph is amber and bold, with a space before the project name; the
// monochrome theme keeps the glyph and the space and shows it bold.
func TestAlertGlyphHasAGapAndWarnStyle(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h := newHarness(t, runningProjects...)
		question := liveEngine("q1", "S30", "paused", nil)
		question.Pending, question.Wait = "question", "Which store?"
		stopped := liveEngine("s1", "S31", "stopped", nil)
		stopped.Wait = "owner action"
		h.src.set(engineSnap(runningProjects[0], question))
		h.src.set(engineSnap(runningProjects[1], stopped))
		h.src.set(engineSnap(runningProjects[2], liveEngine("run-c", "S32", "review", testActivity)))
		h.m.cfg.Theme = NewTheme(mono)
		h.start()
		h.m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
		h.send(press("["))
		h.send(press("down"))
		h.send(press("down")) // gamma is selected, so alpha and beta are drawn plain
		th := h.m.cfg.Theme
		body := h.body(160, 30)
		plain := plainLines(body)
		glyph := th.Warn.Bold(true)
		for _, tc := range []struct{ item, mark, name string }{{"S30", "?", "alpha"}, {"S31", "x", "beta"}} {
			raw := body[bodyLine(t, plain, tc.item)]
			want := " " + glyph.Render(tc.mark) + " " + tc.name
			if !strings.Contains(raw, want) {
				t.Errorf("mono=%v: %s row lacks the amber bold glyph, a space, then the name %q: %q", mono, tc.item, want, raw)
			}
			if p := plain[bodyLine(t, plain, tc.item)]; !strings.HasPrefix(p, " "+tc.mark+" "+tc.name) {
				t.Errorf("mono=%v: %s row text: %q", mono, tc.item, p)
			}
		}
	}
}

// As the pane narrows the activity goes first, then the pipeline: a row never
// shows the sparkline without its pipeline (S110; owner 2026-10-26).
func TestSparklineDropsBeforeThePipeline(t *testing.T) {
	h := runningHarness(t)
	spark := sparkline(testActivity, false)
	for w := 60; w <= 170; w++ {
		row := mainRow(h.at(w, 30), "alpha", "S30")
		if strings.Contains(row, spark) && !strings.Contains(row, "implementer>[reviewer]>gate") {
			t.Errorf("width %d: the sparkline outlasts the pipeline: %q", w, row)
		}
	}
	if row := mainRow(h.at(100, 30), "alpha", "S30"); strings.Contains(row, spark) {
		t.Errorf("at 100x30 the sparkline must be dropped: %q", row)
	}
}

// Only the runs listed as live make "nothing running" untrue: a stale relay is
// listed, but the view still has no run (S140).
func TestNothingRunningOnlyWhenNoRowIsListed(t *testing.T) {
	h := newHarness(t, runningProjects[0])
	old := externalRun("ext-old", "S41")
	old.State = "stale"
	old.UpdatedAt = t0.Add(-35 * time.Minute).Format(time.RFC3339)
	h.src.set(snap(runningProjects[0], nil, nil, old))
	h.start()
	h.send(press("["))
	v := h.wide()
	if !strings.Contains(v, "Running 0") || mainRow(v, "alpha", "S41") == "" {
		t.Fatalf("the stale relay is listed under Running 0:\n%s", v)
	}
	if strings.Contains(v, "nothing running") {
		t.Errorf("\"nothing running\" above a listed row:\n%s", v)
	}
}

// A stopped run's detail line names the role it stopped on in brackets, as its
// row does; a last step that is not a pipeline role is bracketed nowhere.
func TestStoppedDetailBracketsItsRole(t *testing.T) {
	h := newHarness(t, runningProjects[0])
	stopped := liveEngine("run-s", "S07", "stopped", nil,
		roleRun("implementer", 29*time.Minute, 20*time.Minute), roleRun("reviewer", 19*time.Minute, 15*time.Minute))
	stopped.Wait = "owner action"
	h.src.set(engineSnap(runningProjects[0], stopped))
	h.start()
	h.send(press("["))
	h.wide()
	if got := detailText(h.m, 150); !strings.Contains(got, "implementer>[reviewer]>gate") {
		t.Errorf("the detail line must bracket the stopped role: %q", got)
	}

	h = newHarness(t, runningProjects[0])
	odd := liveEngine("run-o", "S08", "stopped", nil,
		roleRun("implementer", 29*time.Minute, 20*time.Minute), roleRun("reviewer", 19*time.Minute, 15*time.Minute), roleRun("scribe", 14*time.Minute, 12*time.Minute))
	odd.Wait = "owner action"
	h.src.set(engineSnap(runningProjects[0], odd))
	h.start()
	h.send(press("["))
	row := mainRow(h.at(100, 30), "alpha", "S08")
	if strings.Contains(row, "[implementer]") || strings.Contains(row, "[reviewer]") || strings.Contains(row, "[scribe]") {
		t.Errorf("a stopped run whose last step is not a pipeline role has no bracketed role: %q", row)
	}
	if got := detailText(h.m, 150); strings.Contains(got, "[implementer]") || strings.Contains(got, "[reviewer]") || strings.Contains(got, "[gate]") {
		t.Errorf("the detail line brackets a role: %q", got)
	}
}

// The heading's question count wears the row glyph, and the question and stop
// counts are amber and bold like the glyphs.
func TestRunningHeadingMatchesTheRowGlyphs(t *testing.T) {
	h := newHarness(t, runningProjects...)
	question := liveEngine("q1", "S30", "paused", nil)
	question.Pending, question.Wait = "question", "Which store?"
	stopped := liveEngine("s1", "S31", "stopped", nil)
	stopped.Wait = "owner action"
	h.src.set(engineSnap(runningProjects[0], question))
	h.src.set(engineSnap(runningProjects[1], stopped))
	h.m.cfg.Theme = NewTheme(false)
	h.start()
	h.send(press("["))
	v := h.wide()
	if head := mainRow(v, "Running 2"); !strings.Contains(head, "? owner questions 1") || strings.Contains(head, "!") {
		t.Errorf("the heading must read ? owner questions 1: %q", head)
	}
	raw := h.m.screen()
	warn := h.m.cfg.Theme.Warn.Bold(true)
	for _, text := range []string{"? owner questions 1", "x stopped 1"} {
		if !strings.Contains(raw, open(warn)+text) {
			t.Errorf("%q is not amber and bold in the heading", text)
		}
	}
}

// The Overview's "Running N" counts what its Running section lists as live: a
// run that is neither stale nor waiting on the owner (S140, owner 2026-10-28).
// The paused question and the stopped run sit under Needs you, so the Overview
// does not count them; the Running entry and the badge do.
func TestOverviewRunningCountMatchesItsRunningSection(t *testing.T) {
	h := newHarness(t, runningProjects...)
	question := liveEngine("q1", "S31", "paused", nil)
	question.Pending, question.Wait = "question", "Which store?"
	stopped := liveEngine("s1", "S32", "stopped", nil)
	stopped.Wait = "owner action"
	s := engineSnap(runningProjects[0], liveEngine("run-a", "S30", "review", testActivity), question, stopped)
	h.src.set(s)
	old := externalRun("ext-old", "S41")
	old.State = "stale"
	old.UpdatedAt = t0.Add(-35 * time.Minute).Format(time.RFC3339)
	h.src.set(snap(runningProjects[1], nil, nil, old))
	h.src.set(snap(runningProjects[2], nil, nil))
	h.start()
	if h.m.active != needsEntry {
		t.Fatalf("active %d, want Needs you", h.m.active)
	}
	v := h.wide()
	if !strings.Contains(v, "Running 1") || strings.Contains(v, "Running 2") || strings.Contains(v, "Running 3") {
		t.Errorf("the Overview must count its one live run:\n%s", v)
	}
	if strings.Contains(v, "nothing running") {
		t.Errorf("one run is listed:\n%s", v)
	}
	if n := h.m.liveRunCount(); n != 3 {
		t.Errorf("the entry still counts the question and the stop: liveRunCount = %d, want 3", n)
	}
}

// With a stale relay as the only Running row the Overview says Running 0 and
// does not claim "nothing running" over a listed row; a number that matches the
// rows is all the heading promises.
func TestOverviewRunningCountZeroOverAStaleRow(t *testing.T) {
	h := newHarness(t, runningProjects[0])
	old := externalRun("ext-old", "S41")
	old.State = "stale"
	old.UpdatedAt = t0.Add(-35 * time.Minute).Format(time.RFC3339)
	h.src.set(snap(runningProjects[0], nil, nil, old))
	h.start()
	v := h.wide()
	if !strings.Contains(v, "Running 0") || mainRow(v, "alpha", "S41") == "" || strings.Contains(v, "Running 1") {
		t.Errorf("the stale relay is listed under Running 0:\n%s", v)
	}
}

// The combined Roadmap's failed-project line starts its name where the item
// rows do, past the two-column ready slot.
func TestCombinedRoadmapErrorLineLinesUpWithItemRows(t *testing.T) {
	h, _ := combinedHarness(t, plainLive)
	bad := snap(proj("beta", true), nil, nil)
	bad.Items.Err = errors.New("items broke")
	h.src.set(bad)
	h.send(press("r"))
	h.tabTo(roadmapView)
	lines := mainLines(plainView(h.m))
	item, fail := lineWith(lines, 0, "S30"), lineWith(lines, 0, "beta", "items broke")
	if item < 0 || fail < 0 {
		t.Fatalf("rows missing:\n%s", strings.Join(lines, "\n"))
	}
	if a, b := strings.Index(lines[item], "alpha"), strings.Index(lines[fail], "beta"); a != b {
		t.Errorf("the failed project's name starts at column %d, item rows at %d:\n%s", b, a, strings.Join(lines, "\n"))
	}
}

// Both Overview headings word the owner questions as the Running view does.
func TestOverviewHeadingReadsOwnerQuestionsLikeRunning(t *testing.T) {
	h := newHarness(t, runningProjects[0])
	question := liveEngine("q1", "S31", "paused", nil)
	question.Pending, question.Wait = "question", "Which store?"
	h.src.set(engineSnap(runningProjects[0], question))
	h.m.cfg.Theme = NewTheme(false)
	h.start()
	v := h.wide()
	if head := mainRow(v, "Running"); !strings.Contains(head, "? owner questions 1") || strings.Contains(head, "!") {
		t.Errorf("the Overview heading must read ? owner questions 1: %q", head)
	}
	if !strings.Contains(h.m.screen(), open(h.m.cfg.Theme.Warn.Bold(true))+"? owner questions 1") {
		t.Errorf("the Overview heading's count is not amber and bold")
	}
}

// A run that waits for a vendor's slot has not gone quiet: on the Running entry
// its heard cell is dim and carries no cue, as on the Overview (S169, S146).
func TestRunningEntryHeardCellOfAVendorWaitCarriesNoCue(t *testing.T) {
	for _, wait := range []string{"", "codex"} {
		p := proj("ork", false)
		h := newHarness(t, p)
		h.m.cfg.Theme = NewTheme(false)
		th := h.m.cfg.Theme
		// The first run is the selected row; the second, unselected, shows its colours.
		first := liveEngine("run-a", "S29", "implement", nil)
		second := liveEngine("run-b", "S30", "implement", nil, roleRun("implementer", 31*time.Minute, 0))
		second.VendorWait, second.CreatedAt = wait, t0.Add(-31*time.Minute)
		h.src.set(engineSnap(p, first, second))
		h.start()
		h.send(press("["))
		row := mainRow(h.wide(), "ork", "S30")
		raw := h.m.View().Content
		if wait == "" {
			if !strings.Contains(row, "!! heard 31m") || !strings.Contains(raw, th.Error.Render("!! heard 31m")) {
				t.Errorf("a quiet run keeps its cue: %q", row)
			}
			continue
		}
		if !strings.Contains(row, "heard 31m") || strings.Contains(row, "!") {
			t.Errorf("a vendor wait has a heard cell without a cue: %q", row)
		}
		if !strings.Contains(raw, th.Dim.Render("heard 31m")) {
			t.Errorf("the heard cell of a vendor wait is dim:\n%s", raw)
		}
	}
}
