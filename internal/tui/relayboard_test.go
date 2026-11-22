// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func TestPipelineLineMarksTheRunningRole(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	for _, mono := range []bool{false, true} {
		h.m.cfg.Theme = NewTheme(mono)
		v := treePaneView(h.m, 120, 60)
		if !strings.Contains(v, "implementer > [reviewer] > gate") {
			t.Fatal(v)
		}
		steps := append([]runTreeStep{{Step: slicercli.Step{Role: "architect", Phase: "done"}}}, h.m.paneSteps()...)
		line := h.m.pipelineLine(steps, false, 120)
		if !strings.HasPrefix(sgr.ReplaceAllString(line, ""), "architect > implementer") {
			t.Fatal(line)
		}
	}
}

// roundSteps builds ledger steps from role:phase pairs, in order. A role with
// a third part, role:phase:task, carries that task line.
func roundSteps(spec ...string) []runTreeStep {
	var steps []slicercli.Step
	for _, s := range spec {
		parts := strings.SplitN(s, ":", 3)
		step := slicercli.Step{Role: parts[0], Phase: parts[1]}
		if len(parts) == 3 {
			step.Task = parts[2]
		}
		steps = append(steps, step)
	}
	return ledgerTreeSteps(steps)
}

// The round counts review rounds, not implementer steps: the finish step and
// a resumed step do not count (S145).
func TestRoundCountsReviewRounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps []string
		want  string
	}{
		{"first implementer", []string{"implementer:start"}, ""},
		{"first review", []string{"implementer:done", "reviewer:start"}, ""},
		{"one review, two fix rounds and a finish step",
			[]string{"orchestrator:start", "implementer:done", "reviewer:done", "implementer:done", "gate:fail", "implementer:done", "gate:done", "implementer:start"}, "fix 2"},
		{"the finish step alone", []string{"implementer:done", "reviewer:done", "gate:done", "implementer:start"}, ""},
		{"a gate step", []string{"implementer:done", "reviewer:done", "implementer:done", "gate:start"}, "gate"},
		{"the first gate", []string{"implementer:done", "reviewer:done", "gate:start"}, "gate"},
		{"a fix round running", []string{"implementer:done", "reviewer:done", "implementer:start"}, "fix 1"},
		{"a fix round after a failed gate", []string{"implementer:done", "reviewer:done", "implementer:done", "gate:fail", "implementer:start"}, "fix 2"},
		{"a resumed step is the same round", []string{"implementer:done", "reviewer:done", "implementer:done", "implementer:start"}, "fix 1"},
		{"a UI pass between review and fix", []string{"implementer:done", "reviewer:done", "ui:done", "implementer:start"}, "fix 1"},
		{"a fix round after a finish step", []string{"implementer:done", "reviewer:done", "implementer:done", "gate:done", "implementer:done:s1 implementer finish", "implementer:done:s1 implementer fix round 2", "reviewer:start"}, "fix 2"},
		// The engine ends a failing gate step done, and its fix implementer
		// task need not say fix. Without the engine's state this reads one low.
		{"a fix round after a gate FAIL recorded as done, ledger only", []string{"implementer:done", "reviewer:done", "implementer:done", "gate:done", "implementer:start:s1 implementer"}, "fix 1"},
		{"a fix round named by its task after a passed gate", []string{"implementer:done", "reviewer:done", "implementer:done", "gate:done", "implementer:start:s1 implementer fix round 2"}, "fix 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := roundSteps(tc.steps...)
			if got := roundWord(steps); got != tc.want {
				t.Fatalf("round word %q, want %q", got, tc.want)
			}
			bar := stageBar(steps, 0, true)
			if tc.want == "" && strings.Contains(bar, "] ") || tc.want != "" && !strings.HasSuffix(bar, "] "+tc.want) {
				t.Fatalf("the stage bar %q must end in %q", bar, tc.want)
			}
			if strings.Contains(bar, " r1") || strings.Contains(bar, " r2") || strings.Contains(bar, " r3") {
				t.Fatalf("the stage bar keeps an rN: %q", bar)
			}
		})
	}
}

// An engine run's steps carry its state, which tells a fix round from the
// finish step after a gate the engine recorded as done (S145).
func TestEngineStateTellsAFixRoundFromTheFinishStep(t *testing.T) {
	afterGate := []string{"orchestrator:start", "implementer:done:s1 implementer", "reviewer:done:s1 reviewer", "implementer:done:s1 implementer fix round 1", "gate:done:s1 gate"}
	for _, tc := range []struct {
		state, task string
		want        string
	}{
		{"implement", "s1 implementer", "fix 2"},
		{"checks", "s1 implementer", "fix 2"},
		{"review", "s1 implementer", "fix 2"},
		{"finish", "s1 implementer finish step", "fix 1"},
		{"commit", "s1 implementer finish step", "fix 1"},
		{"awaiting-approval", "s1 implementer finish step", "fix 1"},
		{"committed", "s1 implementer finish step", "fix 1"},
		// The state outranks the task.
		{"commit", "s1 implementer fix round 9", "fix 1"},
		{"awaiting-approval", "s1 implementer fix round 9", "fix 1"},
		{"finish", "s1 implementer fix round 9", "fix 1"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			steps := withEngineState(roundSteps(append(append([]string(nil), afterGate...), "implementer:start:"+tc.task)...), tc.state)
			if got := roundWord(steps); got != tc.want {
				t.Fatalf("state %s: round word %q, want %q", tc.state, got, tc.want)
			}
		})
	}
}

// The pipeline line keeps "round N" for a run whose bar says no round; N is
// the fix rounds plus one.
func TestPipelineLineRoundFollowsTheFixRounds(t *testing.T) {
	h := newHarness(t)
	line := func(spec ...string) string {
		return sgr.ReplaceAllString(h.m.pipelineLine(roundSteps(spec...), false, 120), "")
	}
	if got := line("implementer:done", "reviewer:done", "gate:done", "implementer:start"); !strings.Contains(got, "round 1") {
		t.Fatalf("a finish step is not a second round: %q", got)
	}
	if got := line("implementer:done", "reviewer:done", "implementer:start"); !strings.Contains(got, "round 2") {
		t.Fatalf("a fix round is the second round: %q", got)
	}
}

func TestQueuedRolesShowAsQueued(t *testing.T) {
	h := treeHarness(t, []slicercli.RelayRun{{ID: "one", State: "active", Steps: []slicercli.Step{{Role: "implementer", Phase: "start"}}}}, "")
	v := treePaneView(h.m, 120, 60)
	for _, want := range []string{"reviewer    queued", "gate        queued"} {
		if !strings.Contains(v, want) {
			t.Fatal(v)
		}
	}
}

func TestRepeatedRolesShowTheirCountAndTheGateFailLine(t *testing.T) {
	h := treeHarness(t, []slicercli.RelayRun{{ID: "one", State: "active", Steps: []slicercli.Step{{Role: "implementer", Phase: "done"}, {Role: "gate", Phase: "fail", Task: "mutation stayed green"}, {Role: "implementer", Phase: "start"}}}}, "")
	v := treePaneView(h.m, 120, 60)
	for _, want := range []string{"x2", "✗ gate FAIL -> implementer round 2"} {
		if !strings.Contains(v, want) {
			t.Fatal(v)
		}
	}
}

func TestRecentSectionListsRunsEndedWithinTheHour(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	s := snap(proj("ork", false), nil, nil)
	for _, minutes := range []int{20, 61, 5} {
		s.BoardRuns.Value = append(s.BoardRuns.Value, slicercli.RelayRun{ID: elapsed(time.Duration(minutes) * time.Minute), Items: []string{"S97"}, State: "ended", Outcome: "pass", BeganAt: h.now.Add(-time.Duration(minutes+10) * time.Minute).Format(time.RFC3339), UpdatedAt: h.now.Add(-time.Duration(minutes) * time.Minute).Format(time.RFC3339), Steps: []slicercli.Step{{Role: "implementer"}, {Role: "reviewer"}, {Role: "gate"}}})
	}
	h.src.set(s)
	h.start()
	rows := h.m.recentRows()
	if len(rows) != 2 || rows[0].ledgerID != "5m" {
		t.Fatalf("%+v", rows)
	}
	if !strings.Contains(rows[0].text, "✓ pass  S97  10m  ended 5m ago  impl 1  review 1  gate 1") {
		t.Fatal(rows[0].text)
	}
	h.m.openPane(rows[0])
	if _, _, ok := h.m.paneRow(); !ok || len(h.m.paneSteps()) != 3 {
		t.Fatal("ended ledger pane missing")
	}
	h.now = h.now.Add(time.Hour)
	if len(h.m.recentRows()) != 0 {
		t.Fatal("render clock not applied")
	}
}

// recentOutcomeHarness holds four ended runs within the hour: pass, fail,
// stopped, and a pass that ork landed (S390, adj 418).
func recentOutcomeHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, proj("ork", false))
	s := snap(proj("ork", false), nil, nil)
	for i, run := range []struct{ id, outcome string }{{"pass", "pass"}, {"fail", "fail"}, {"stopped", "stopped"}, {"landed", "pass"}} {
		minutes := time.Duration(i+1) * time.Minute
		s.BoardRuns.Value = append(s.BoardRuns.Value, slicercli.RelayRun{ID: run.id, Items: []string{"S97"}, State: "ended", Outcome: run.outcome, BeganAt: h.now.Add(-minutes - 10*time.Minute).Format(time.RFC3339), UpdatedAt: h.now.Add(-minutes).Format(time.RFC3339), Steps: []slicercli.Step{{Role: "implementer"}}})
	}
	s.EngineRuns = []source.EngineRun{{ID: "engine-1", Item: "S97", State: "landed", Landed: true, RelayRun: "landed"}}
	h.src.set(s)
	h.start()
	return h
}

func TestRecentOutcomeGlyphsFollowTheASCIIOption(t *testing.T) {
	for _, ascii := range []bool{true, false} {
		want := []string{"✓ pass", "✗ fail", "~ stopped", "✓ landed"}
		if ascii {
			want = []string{"+ pass", "x fail", "~ stopped", "+ landed"}
		}
		t.Run(fmt.Sprintf("ascii-%t", ascii), func(t *testing.T) {
			h := recentOutcomeHarness(t)
			h.m.cfg.ASCII = ascii
			rows := h.m.recentRows()
			if len(rows) != len(want) {
				t.Fatalf("%d rows, want %d: %+v", len(rows), len(want), rows)
			}
			for i, r := range rows {
				if r.cells[0] != want[i] {
					t.Errorf("row %d outcome %q, want %q", i, r.cells[0], want[i])
				}
				if ascii {
					for j := 0; j < len(r.text); j++ {
						if r.text[j] >= 128 {
							t.Errorf("row %d holds a non-ASCII byte: %q", i, r.text)
							break
						}
					}
				}
			}
		})
	}
}

func TestRecentASCIIOutcomeKeepsItsColour(t *testing.T) {
	h := recentOutcomeHarness(t)
	h.m.cfg.ASCII = true
	th := NewTheme(false)
	h.m.cfg.Theme = th
	rows := h.m.recentRows()
	if len(rows) != 4 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, c := range []struct {
		row   ovRow
		want  string
		style func() string
	}{
		{rows[0], "+ pass", func() string { return th.Passed.Render("+ pass") }},
		{rows[1], "x fail", func() string { return th.Error.Render("x fail") }},
		{rows[3], "+ landed", func() string { return th.Passed.Render("+ landed") }},
	} {
		line := h.m.rowLine(c.row, false, 3, 80)
		if got := c.style(); !strings.Contains(line, got) || got == c.want {
			t.Errorf("row %q is not drawn in its colour %q:\n%q", c.want, got, line)
		}
	}
	if line := h.m.rowLine(rows[2], false, 3, 80); strings.Contains(line, th.Passed.Render("~ stopped")) || strings.Contains(line, th.Error.Render("~ stopped")) {
		t.Errorf("a stopped run takes a pass or error colour: %q", line)
	}
}

func TestNarrowRowsDropCellsFromTheRightWhole(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	if line := sgr.ReplaceAllString(h.m.pipelineLine(h.m.paneSteps(), false, 35), ""); strings.Contains(line, "round") || strings.Contains(line, "...") {
		t.Fatal(line)
	}
	h.m.byName["ork"].snap.Runs.Value[0].UpdatedAt = h.now.Format(time.RFC3339)
	_, rows := h.m.rows()
	r := rows[0]
	full := rowText(r, 200)
	if !strings.Contains(full, "heard") {
		t.Fatal(full)
	}
	for width := 30; width < 80; width++ {
		v := rowText(r, width)
		if strings.Contains(v, "hea") && !strings.Contains(v, "heard 0s") {
			t.Fatalf("partial heard at %d: %s", width, v)
		}
	}
}

func TestStaleRunFreezesAtLastEvent(t *testing.T) {
	r := slicercli.RelayRun{ID: "stale", State: "stale", Items: []string{"S97"}, BeganAt: t0.Add(-time.Hour).Format(time.RFC3339), UpdatedAt: t0.Add(-30 * time.Minute).Format(time.RFC3339), Steps: []slicercli.Step{{Role: "implementer", Phase: "start", BeganAt: t0.Add(-time.Hour).Format(time.RFC3339)}}}
	h := newHarness(t, proj("ork", false))
	s := snap(proj("ork", false), nil, nil)
	s.BoardRuns.Value = []slicercli.RelayRun{r}
	h.src.set(s)
	h.start()
	_, rows := h.m.rows()
	if len(rows) != 1 || !strings.Contains(rows[0].text, "stale  30m") {
		t.Fatalf("%+v", rows)
	}
	h.m.openPane(rows[0])
	v := treePaneView(h.m, 120, 60)
	if !strings.Contains(v, "~ implementer stale 30m") || !strings.Contains(v, "!! last heard 30m ago") {
		t.Fatal(v)
	}
	h.now = h.now.Add(time.Hour)
	v = treePaneView(h.m, 120, 60)
	if !strings.Contains(v, "~ implementer stale 30m") || !strings.Contains(v, "!! last heard 1h30m ago") {
		t.Fatal(v)
	}
}

// An engine run with no ledger run counts its quiet time from the role's last
// output, not from the role's start (S394, adj 427). With no output yet it
// counts from the start.
func TestEngineRunHeardFollowsTheLastOutput(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	th := NewTheme(false)
	h.m.cfg.Theme = th
	for _, tc := range []struct {
		name       string
		lastOutput time.Time
		want       string
		dim        bool
	}{
		{"wrote 2m ago", t0.Add(-2 * time.Minute), "heard 2m", true},
		{"no output yet", time.Time{}, "!! heard 45m", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := source.EngineRun{ID: "e", Item: "S1", State: "implementing", CreatedAt: t0.Add(-46 * time.Minute),
				Roles:   []source.EngineRole{roleRun("implementer", 45*time.Minute, 0)},
				Working: []source.EngineWork{{What: "implementer round 1", Since: t0.Add(-45 * time.Minute), LastOutput: tc.lastOutput}}}
			ledger, _ := engineBoard(r)
			text, style := h.m.lastHeard(engineLedger(ledger, r))
			if text != tc.want {
				t.Fatalf("heard = %q, want %q", text, tc.want)
			}
			if tc.dim && style.Render(text) != th.Dim.Render(text) {
				t.Errorf("the cell is not dim: %q", style.Render(text))
			}
		})
	}
}

func TestRelayBoardGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, size := range [][2]int{{100, 30}, {80, 24}, {120, 40}} {
			h := newHarness(t, proj("ork", false))
			h.m.cfg.Theme = NewTheme(mono)
			active := slicercli.RelayRun{ID: "s97", Items: []string{"S97"}, State: "active", BeganAt: h.now.Add(-20 * time.Minute).Format(time.RFC3339), UpdatedAt: h.now.Add(-16 * time.Minute).Format(time.RFC3339), Steps: []slicercli.Step{{Role: "implementer", Phase: "done"}, {Role: "reviewer", Phase: "start"}}}
			s := snap(proj("ork", false), nil, nil, active)
			for i, outcome := range []string{"pass", "fail", "stopped"} {
				r := active
				r.ID = outcome
				r.State = "ended"
				r.Outcome = outcome
				r.Items = []string{fmt.Sprintf("S%d", 93+i)}
				r.UpdatedAt = h.now.Add(-time.Duration([]int{20, 5, 35}[i]) * time.Minute).Format(time.RFC3339)
				r.BeganAt = h.now.Add(-time.Duration([]int{30, 15, 45}[i]) * time.Minute).Format(time.RFC3339)
				began, _ := time.Parse(time.RFC3339, r.BeganAt)
				r.Steps = []slicercli.Step{{Role: "implementer", Phase: "done", BeganAt: r.BeganAt, UpdatedAt: began.Add(5 * time.Minute).Format(time.RFC3339)}, {Role: "reviewer", Phase: "start"}}
				s.BoardRuns.Value = append(s.BoardRuns.Value, r)
			}
			if size[0] == 120 {
				h.m.sidebarHidden = true
				h.m.cursor = 2
				stale := active
				stale.ID = "stale"
				stale.State = "stale"
				stale.BeganAt = h.now.Add(-50 * time.Minute).Format(time.RFC3339)
				stale.UpdatedAt = h.now.Add(-30 * time.Minute).Format(time.RFC3339)
				s.BoardRuns.Value = append(s.BoardRuns.Value, stale)
			}
			h.src.set(s)
			h.start()
			h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			checkGolden(t, fmt.Sprintf("board-%dx%d-mono-%t.txt", size[0], size[1], mono), h.m.View().Content)
			h.m.openPane(ovRow{ledgerID: "s97", name: "ork"})
			if size[0] >= 100 && !strings.Contains(h.m.View().Content, "10m (active 5m)") {
				t.Fatal("Recent active duration missing from board pane")
			}
			checkGolden(t, fmt.Sprintf("board-pane-%dx%d-mono-%t.txt", size[0], size[1], mono), h.m.View().Content)
		}
	}
}

func TestPipelineRoleStyles(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h := newHarness(t, proj("ork", false))
		h.m.cfg.Theme = NewTheme(mono)
		th := h.m.cfg.Theme
		steps := ledgerTreeSteps([]slicercli.Step{{Role: "architect", Phase: "done"}, {Role: "implementer", Phase: "start"}, {Role: "reviewer", Phase: "fail"}})
		want := th.Passed.Render("architect") + " > " + th.Running.Render("[implementer]") + " > " + th.Error.Render("reviewer") + " > " + th.Dim.Render("gate")
		if got := h.m.pipeline(steps, false, false); got != want {
			t.Fatalf("%q != %q", got, want)
		}
		want = th.Dim.Render("architect") + " > " + th.Dim.Render("implementer") + " > " + th.Dim.Render("reviewer") + " > " + th.Dim.Render("gate")
		if got := h.m.pipeline(steps, true, false); got != want {
			t.Fatalf("stale: %q != %q", got, want)
		}
	}
}
