// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func treeFixture(t *testing.T, file string) []slicercli.RelayRun {
	t.Helper()
	b, err := os.ReadFile("../../testdata/cli/slicer/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Runs []slicercli.RelayRun `json:"runs"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v.Runs
}

func treeHarness(t *testing.T, runs []slicercli.RelayRun, engine string) *harness {
	t.Helper()
	h := newHarness(t, proj("ork", false))
	s := snap(proj("ork", false), nil, nil, runs...)
	if engine != "" {
		var r source.EngineRun
		if err := json.Unmarshal([]byte(engine), &r); err != nil {
			t.Fatal(err)
		}
		s.EngineRuns = []source.EngineRun{r}
	}
	h.src.set(s)
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.deliver(press("enter"))
	if h.m.pane == nil {
		t.Fatal("Enter did not open run tree pane")
	}
	return h
}

func TestRunTreeLedger(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	v := treePaneView(h.m, 100, 40)
	for _, want := range []string{"  Run tree", "  | orchestrator start  2m  codex sol high", "    + implementer  done   1m  claude sonnet", "      ! explorer     fail   30s  codex sol", "    | reviewer     start  1m  claude opus", "  . gate         queued  claude opus"} {
		if !strings.Contains("\n"+v, "\n"+want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	// The original CLI sample also retains the orchestrator and its roles.
	sample := treeFixture(t, "relay-run-list.json")[:1]
	sample[0].State = "active"
	h = treeHarness(t, sample, "")
	v = treePaneView(h.m, 120, 40)
	if !strings.Contains(v, "\n  + orchestrator") || !strings.Contains(v, "\n    + gate") {
		t.Fatal(v)
	}
}

func TestRunTreeReadOnly(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	if h.m.pane == nil {
		t.Fatal("ledger Enter did not open pane")
	}
	for _, k := range []string{"a", "x", "t", "enter", "hello"} {
		if _, cmd := h.m.Update(press(k)); cmd != nil {
			t.Fatalf("read-only key %q returned action", k)
		}
	}
	h.m.Update(tea.PasteMsg{Content: "answer"})
	v := h.view()
	for _, bad := range []string{"Answer:", "x stop", "t retry", "a approve", "ork answer"} {
		if strings.Contains(v, bad) {
			t.Fatalf("read-only pane offers %q:\n%s", bad, v)
		}
	}
	if h.m.pane.input.Value() != "" {
		t.Fatal("read-only pane accepted input")
	}
	h.deliver(press("esc"))
	if h.m.pane != nil {
		t.Fatal("esc did not close ledger pane")
	}
}

const treeEngine = `{"id":"engine","item":"S55","state":"implement","roles":[{"role":"implementer","round":1,"vendor":"claude","model":"sonnet","effort":"high","step":"2","started_at":"2026-10-19T16:40:00Z","ended_at":"2026-10-19T16:41:00Z","exit":"ok"},{"role":"reviewer","round":1,"vendor":"claude","model":"opus","started_at":"2026-10-19T16:41:00Z"},{"role":"gate","round":1,"vendor":"codex","model":"sol","started_at":"2026-10-19T16:40:00Z","ended_at":"2026-10-19T16:40:30Z","exit":"error"}]}`

func TestRunTreeEngine(t *testing.T) {
	for _, linked := range []string{"", "missing", "tree"} {
		t.Run(linked, func(t *testing.T) {
			raw := strings.TrimSuffix(treeEngine, "}") + fmt.Sprintf(`,"relay_run":%q}`, linked)
			var runs []slicercli.RelayRun
			if linked == "tree" {
				runs = treeFixture(t, "relay-run-tree.json")
			}
			h := treeHarness(t, runs, raw)
			v := treePaneView(h.m, 100, 40)
			if linked == "tree" {
				tree := sgr.ReplaceAllString(strings.Join(h.m.runTreeLines(h.m.paneSteps(), 100, 0), "\n"), "")
				if !strings.Contains(tree, "orchestrator") || strings.Contains(tree, "round 1") || strings.Count(tree, "implementer") != 1 {
					t.Fatal(v)
				}
			} else {
				for _, want := range []string{"+ implementer done  1m  claude sonnet high", "| reviewer    start 1m  claude opus", "! gate        fail  30s  codex sol", "round 1"} {
					if !strings.Contains(v, want) {
						t.Errorf("missing %q:\n%s", want, v)
					}
				}
			}
		})
	}
}

func TestRunTreeSpinnerAndClock(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	if h.m.pane == nil {
		t.Fatal("missing pane")
	}
	for i, glyph := range []string{"|", "/", "-"} {
		if i > 0 {
			h.now = h.now.Add(time.Minute)
			h.m.Update(frameMsg{})
		}
		v := treePaneView(h.m, 100, 40)
		duration := elapsed(h.now.Sub(time.Date(2026, 10, 19, 16, 40, 0, 0, time.UTC)))
		if !strings.Contains(v, glyph+" orchestrator start  "+duration+"  codex sol high") || !strings.Contains(v, "+ implementer  done   1m  claude sonnet") {
			t.Fatal(v)
		}
	}
}

func TestRunTreeCycleAndWidth(t *testing.T) {
	runs := treeFixture(t, "relay-run-tree.json")
	runs[0].Steps[0].Parent = "child" // cycle with no root
	runs[0].Steps[1].Task = strings.Repeat("Very long task ", 50)
	h := treeHarness(t, runs, treeEngine)
	// Exercise the linked ledger through an engine pane.
	h.m.byName["ork"].snap.EngineRuns[0].RelayRun = "tree"
	for _, width := range []int{80, 32, 12, 3} {
		lines := h.m.runTreeLines(h.m.paneSteps(), width, 0)
		v := strings.Join(lines, "\n")
		for _, role := range []string{"orchestrator", "implementer", "explorer", "reviewer", "gate"} {
			if width == 80 && strings.Count(v, role) != 1 {
				t.Errorf("lost/duplicated %s:\n%s", role, v)
			}
		}
		for _, line := range lines {
			if lipgloss.Width(line) > width {
				t.Errorf("width %d overflow: %q", width, line)
			}
		}
		if width == 80 && !strings.Contains(v, "...") {
			t.Error("long task not clipped")
		}
	}
}

func TestRunTreeGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, size := range [][2]int{{80, 24}, {100, 30}} {
			for _, engine := range []bool{false, true} {
				t.Run(fmt.Sprintf("%t/%v/%t", mono, size, engine), func(t *testing.T) {
					runs := treeFixture(t, "relay-run-tree.json")
					raw := ""
					if engine {
						raw = treeEngine
						runs = nil
					}
					h := treeHarness(t, runs, raw)
					h.m.cfg.Theme = NewTheme(mono)
					h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					checkGolden(t, fmt.Sprintf("runtree-%dx%d-mono-%t-engine-%t.txt", size[0], size[1], mono, engine), h.m.View().Content)
				})
			}
		}
	}
}

func treePaneView(m *Model, width, height int) string {
	return sgr.ReplaceAllString(strings.Join(m.paneLines(width, height, ""), "\n"), "")
}

func TestRunTreeNeutralAndMalformedTimes(t *testing.T) {
	h := treeHarness(t, nil, treeEngine)
	steps := []slicercli.Step{
		{ID: "a", Parent: "a", Role: "self", Phase: "", BeganAt: "invalid"},
		{ID: "b", Role: "neutral", Phase: "custom-phase", BeganAt: "2026-10-19T16:40:00Z", UpdatedAt: "invalid"},
		{ID: "c", Role: "done", Phase: "done", BeganAt: "invalid", UpdatedAt: "invalid"},
		{ID: "d", Role: "unicode", Phase: "fail", Task: strings.Repeat("界", 100) + "\nsecond line\tend"},
	}
	for _, mono := range []bool{false, true} {
		h.m.cfg.Theme = NewTheme(mono)
		lines := h.m.runTreeLines(ledgerTreeSteps(steps), 32, 0)
		v := sgr.ReplaceAllString(strings.Join(lines, "\n"), "")
		for _, want := range []string{". self", ". neutral custom-phase", "+ done    done", "! unicode fail", "..."} {
			if !strings.Contains(v, want) {
				t.Errorf("missing %q: %s", want, v)
			}
		}
		if strings.Contains(v, "waiting") || strings.Contains(v, "0s") {
			t.Fatal(v)
		}
		for _, line := range lines {
			if lipgloss.Width(line) > 32 || strings.ContainsAny(line, "\n\t") {
				t.Fatalf("bad line: %q", line)
			}
		}
	}
}

func TestRunTreeLedgerPaneKeepsTarget(t *testing.T) {
	runs := treeFixture(t, "relay-run-tree.json")
	h := treeHarness(t, runs, "")
	s := h.m.byName["ork"].snap
	s.Runs.Value = append([]slicercli.RelayRun{relay("other", "2026-10-19T16:40:00Z", "S56")}, s.Runs.Value...)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	if v := treePaneView(h.m, 100, 40); !strings.Contains(v, "Run tree") || !strings.Contains(v, "orchestrator") {
		t.Fatal(v)
	}
	s.Runs.Value = s.Runs.Value[:1]
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	if v := treePaneView(h.m, 100, 40); !strings.Contains(v, "Run tree: no longer in snapshot") {
		t.Fatal(v)
	}
}

func TestRunTreeScroll(t *testing.T) {
	runs := treeFixture(t, "relay-run-tree.json")
	for i := 0; i < 30; i++ {
		runs[0].Steps = append(runs[0].Steps, slicercli.Step{ID: fmt.Sprint(i), Role: fmt.Sprintf("child-%02d", i), Parent: "root", Phase: "done"})
	}
	h := treeHarness(t, runs, "")
	if v := h.view(); strings.Contains(v, "child-29") {
		t.Fatal("tree did not scroll")
	}
	for i := 0; i < 60; i++ {
		h.m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if v := h.view(); !strings.Contains(v, "child-29") || !strings.Contains(v, "esc close pane") {
		t.Fatal(v)
	}
	for i := 0; i < 60; i++ {
		h.m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	}
	if v := h.view(); !strings.Contains(v, "orchestrator") {
		t.Fatal(v)
	}
}

func TestRunTreePausedQuestionBeforeLongTree(t *testing.T) {
	h := treeHarness(t, nil, treeEngine)
	r := &h.m.byName["ork"].snap.EngineRuns[0]
	r.State, r.Pending = "paused", "question"
	r.Wait = "question: Which storage engine should we use? Please keep the answer local."
	role := r.Roles[0]
	for round := 2; round <= 30; round++ {
		role.Round = round
		r.Roles = append(r.Roles, role)
	}
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := h.view()
	if !strings.Contains(v, "question: Which storage engine") || !strings.Contains(v, "Answer:") {
		t.Fatalf("long tree hid paused question:\n%s", v)
	}
	pane := treePaneView(h.m, 80, 24)
	if strings.Index(pane, "question:") < 0 || strings.Index(pane, "question:") > strings.Index(pane, "+ implementer") {
		t.Fatalf("wait reason must precede tree:\n%s", pane)
	}
}

func TestRunTreeEndedRunUnfinishedRole(t *testing.T) {
	for _, state := range []string{"stopped", "paused", "committed"} {
		t.Run(state, func(t *testing.T) {
			h := treeHarness(t, nil, treeEngine)
			h.m.byName["ork"].snap.EngineRuns[0].State = state
			steps := h.m.paneSteps()
			if steps[1].Phase != state {
				t.Fatalf("unfinished role phase = %q, want neutral %q", steps[1].Phase, state)
			}
			v := sgr.ReplaceAllString(strings.Join(h.m.runTreeLines(steps, 100, 0), "\n"), "")
			for _, line := range strings.Split(v, "\n") {
				if strings.Contains(line, "reviewer") && (!strings.HasPrefix(strings.TrimSpace(line), ". reviewer") || strings.Contains(line, " start") || strings.Contains(line, "1m")) {
					t.Fatalf("inactive run shows running role: %s", line)
				}
			}
		})
	}
}

func TestRunTreeReadOnlyGuard(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	for _, k := range []string{"a", "x", "t"} {
		t.Run(k, func(t *testing.T) {
			h.m.actionStatus = "unchanged"
			cmd := h.m.paneKey(press(k))
			// requestAction sets actionStatus even for an empty engineID. This
			// assertion isolates paneKey's ledger guard from that later check.
			if cmd != nil || h.m.confirm != nil || len(h.m.actionFlights) != 0 || h.m.actionStatus != "unchanged" {
				t.Fatalf("read-only key changed action state: status=%q confirm=%v", h.m.actionStatus, h.m.confirm)
			}
		})
	}
}

func TestRunTreeTaskIndent(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	lines := h.m.runTreeLines(h.m.paneSteps(), 100, 0)
	for i, line := range lines {
		plain := sgr.ReplaceAllString(line, "")
		if strings.Contains(plain, "Build the tree") {
			child := sgr.ReplaceAllString(lines[i+1], "")
			if strings.Index(plain, "Build") != 6 || strings.Index(child, "!") != 4 {
				t.Fatalf("task and child columns are ambiguous: task=%q child=%q", plain, child)
			}
			if line != "  "+h.m.cfg.Theme.Dim.Render("    Build the tree") {
				t.Fatalf("task style is not Dim: %q", line)
			}
			return
		}
	}
	t.Fatal("task missing")
}

func TestRunTreeFieldsAndStyles(t *testing.T) {
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprint(mono), func(t *testing.T) {
			h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
			h.m.cfg.Theme = NewTheme(mono)
			lines := h.m.runTreeLines(h.m.paneSteps(), 100, 0)
			for _, tc := range []struct {
				index  int
				text   string
				style  lipgloss.Style
				prefix string
			}{
				{0, "| orchestrator start  2m  codex sol high", h.m.cfg.Theme.Heading, ""},
				{1, "+ implementer  done   1m  claude sonnet", h.m.cfg.Theme.Dim, "  "},
				{3, "! explorer     fail   30s  codex sol", h.m.cfg.Theme.Error, "    "},
				{5, ". gate         queued  claude opus", lipgloss.NewStyle(), ""},
			} {
				if want := tc.prefix + tc.style.Render(tc.text); lines[tc.index] != want {
					t.Errorf("wrong fields/style: got %q want %q", lines[tc.index], want)
				}
			}
			// Compare columns before depth indent, including a double-width role.
			steps := h.m.paneSteps()[:2]
			steps[0].Role, steps[1].Role = "界界界", "x"
			steps[1].Parent = ""
			aligned := h.m.runTreeLines(steps, 60, 0)
			for i, phase := range []string{"start", "done"} {
				plain := sgr.ReplaceAllString(aligned[i], "")
				if col := lipgloss.Width(strings.Split(plain, phase)[0]); col != 9 {
					t.Errorf("phase column = %d, want 9: %q", col, plain)
				}
				duration := []string{"2m", "1m"}[i]
				if col := lipgloss.Width(strings.Split(plain, duration)[0]); col != 15 {
					t.Errorf("elapsed column = %d, want 15: %q", col, plain)
				}
			}
		})
	}
}

func TestRunTreeEngineRoundOnNode(t *testing.T) {
	h := treeHarness(t, nil, treeEngine)
	lines := h.m.runTreeLines(h.m.paneSteps(), 100, 0)
	if len(lines) != 5 {
		t.Fatalf("engine roles need nodes, usage and gate result, got %d: %v", len(lines), lines)
	}
	for _, i := range []int{0, 1, 3} {
		line := lines[i]
		if !strings.Contains(line, "round 1") {
			t.Errorf("node missing round: %q", line)
		}
	}
}

func TestRunTreeReadOnlyLabel(t *testing.T) {
	h := treeHarness(t, treeFixture(t, "relay-run-tree.json"), "")
	v := treePaneView(h.m, 100, 40)
	if !strings.Contains(v, "Run tree  S55  active  started elsewhere") || strings.Contains(v, "read-only") {
		t.Fatalf("the pane names the run and leaves the read-only note to the hint line:\n%s", v)
	}
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if hint := hintLine(plainView(h.m)); !strings.Contains(hint, "started elsewhere, read-only; esc close pane") {
		t.Fatalf("the hint line must explain the read-only state: %q", hint)
	}
}

// The engine records a UI pass round as a reviewer step in the ledger (slicer
// has no ui role), with the task "S55 ui". The tree of a linked run reads the
// role and the round from the engine's role record, so the UI pass is not a
// second reviewer (S136).
func TestRunTreeShowsUIPassRound(t *testing.T) {
	runs := []slicercli.RelayRun{{ID: "tree", Items: []string{"S55"}, State: "active", BeganAt: "2026-10-19T16:40:00Z", Steps: []slicercli.Step{
		{ID: "root", Role: "orchestrator", Vendor: "codex", Model: "sol", Phase: "start", BeganAt: "2026-10-19T16:40:00Z"},
		{ID: "rev", Parent: "root", Role: "reviewer", Vendor: "claude", Model: "opus", Phase: "done", Task: "S55 reviewer", BeganAt: "2026-10-19T16:41:00Z", UpdatedAt: "2026-10-19T16:42:00Z"},
		{ID: "ui", Parent: "root", Role: "reviewer", Vendor: "claude", Model: "sonnet", Phase: "done", Task: "S55 ui", BeganAt: "2026-10-19T16:41:00Z", UpdatedAt: "2026-10-19T16:43:00Z"},
	}}}
	engine := `{"id":"engine","item":"S55","state":"review","relay_run":"tree","roles":[` +
		`{"role":"reviewer","round":2,"vendor":"claude","model":"opus","step":"rev","started_at":"2026-10-19T16:41:00Z","ended_at":"2026-10-19T16:42:00Z","exit":"ok"},` +
		`{"role":"ui","round":2,"vendor":"claude","model":"sonnet","step":"ui","started_at":"2026-10-19T16:41:00Z","ended_at":"2026-10-19T16:43:00Z","exit":"ok"}]}`
	h := treeHarness(t, runs, engine)
	tree := sgr.ReplaceAllString(strings.Join(h.m.runTreeLines(h.m.paneSteps(), 100, 0), "\n"), "")
	if !strings.Contains(tree, "+ ui ") || !strings.Contains(tree, "claude sonnet round 2") {
		t.Fatalf("the UI pass round is not named:\n%s", tree)
	}
	if strings.Contains(tree, "x2") {
		t.Fatalf("the UI pass counts as a second reviewer:\n%s", tree)
	}
}

// The UI pass has no stage of its own: while it runs and the reviewer is done,
// the stage bar and the pipeline line both show the reviewer stage running,
// for an engine-only run and for one linked to a ledger run (S136).
func TestUIPassCountsInTheReviewerStage(t *testing.T) {
	roles := `{"role":"implementer","round":1,"vendor":"claude","model":"sonnet","step":"impl","started_at":"2026-10-19T16:40:00Z","ended_at":"2026-10-19T16:41:00Z","exit":"ok"},` +
		`{"role":"reviewer","round":1,"vendor":"claude","model":"opus","step":"rev","started_at":"2026-10-19T16:41:00Z","ended_at":"2026-10-19T16:42:00Z","exit":"ok"},` +
		`{"role":"ui","round":1,"vendor":"claude","model":"sonnet","step":"ui","started_at":"2026-10-19T16:41:00Z"}`
	ledger := []slicercli.RelayRun{{ID: "tree", Items: []string{"S55"}, State: "active", BeganAt: "2026-10-19T16:40:00Z", Steps: []slicercli.Step{
		{ID: "root", Role: "orchestrator", Phase: "start", BeganAt: "2026-10-19T16:40:00Z"},
		{ID: "impl", Parent: "root", Role: "implementer", Phase: "done", BeganAt: "2026-10-19T16:40:00Z", UpdatedAt: "2026-10-19T16:41:00Z"},
		{ID: "rev", Parent: "root", Role: "reviewer", Phase: "done", Task: "S55 reviewer", BeganAt: "2026-10-19T16:41:00Z", UpdatedAt: "2026-10-19T16:42:00Z"},
		{ID: "ui", Parent: "root", Role: "reviewer", Phase: "start", Task: "S55 ui", BeganAt: "2026-10-19T16:41:00Z"},
	}}}
	for _, linked := range []bool{false, true} {
		engine := `{"id":"engine","item":"S55","state":"review","roles":[` + roles + `]}`
		var runs []slicercli.RelayRun
		if linked {
			engine = `{"id":"engine","item":"S55","state":"review","relay_run":"tree","roles":[` + roles + `]}`
			runs = ledger
		}
		h := treeHarness(t, runs, engine)
		steps := h.m.paneSteps()
		if bar := stageBar(steps, 0, true); bar != "[##~~..]" {
			t.Errorf("linked=%v: stage bar %q, want [##~~..]", linked, bar)
		}
		line := sgr.ReplaceAllString(h.m.pipelineLine(steps, false, 120), "")
		if !strings.Contains(line, "implementer > [reviewer] > gate") {
			t.Errorf("linked=%v: pipeline line %q", linked, line)
		}
		if got := runningRole(steps); got != "[reviewer]" {
			t.Errorf("linked=%v: running role %q", linked, got)
		}
	}
}

// The reviewer stage reads running while the reviewer or the UI pass runs,
// failed if either failed, and otherwise done, whichever started first (S136).
func TestReviewerStageCombinesTheUIPass(t *testing.T) {
	step := func(role, phase string) runTreeStep {
		return runTreeStep{Step: slicercli.Step{Role: role, Phase: phase}}
	}
	for _, tc := range []struct{ reviewer, ui, want string }{
		{"done", "start", "start"},
		{"start", "done", "start"},
		{"done", "fail", "fail"},
		{"fail", "done", "fail"},
		{"done", "done", "done"},
		{"", "start", "start"},
		{"start", "", "start"},
	} {
		var steps []runTreeStep
		if tc.reviewer != "" {
			steps = append(steps, step("reviewer", tc.reviewer))
		}
		if tc.ui != "" {
			steps = append(steps, step("ui", tc.ui))
		}
		if got := pipelinePhases(steps)["reviewer"]; got != tc.want {
			t.Errorf("reviewer %q ui %q: stage %q, want %q", tc.reviewer, tc.ui, got, tc.want)
		}
	}
}

// A UI pass step that comes after the gate is not part of the review, so it
// does not reopen the reviewer stage (S136).
func TestUIPassAfterTheGateLeavesTheReviewerStage(t *testing.T) {
	steps := []runTreeStep{
		{Step: slicercli.Step{Role: "reviewer", Phase: "done"}},
		{Step: slicercli.Step{Role: "gate", Phase: "start"}},
		{Step: slicercli.Step{Role: "ui", Phase: "start"}},
	}
	if got := pipelinePhases(steps)["reviewer"]; got != "done" {
		t.Fatalf("reviewer stage %q, want done", got)
	}
}
