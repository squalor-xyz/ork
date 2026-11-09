// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// A run queued behind the cap has not started and has not gone quiet: its heard
// cell says how long it has waited, dim, and carries no stall cue (S267).
func TestQueuedRunHasNoStaleCue(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	th := NewTheme(false)
	h.m.cfg.Theme = th
	run := source.EngineRun{ID: "q", Item: "S1", State: "queued", CreatedAt: h.now.Add(-32 * time.Minute)}
	// The linked ledger run was last updated 32 minutes ago too, as the ledger of
	// a queued run is: nothing refreshes it.
	ledger := engineLedger(slicercli.RelayRun{ID: "l", State: "active", UpdatedAt: h.now.Add(-32 * time.Minute).Format(time.RFC3339Nano)}, run)
	text, style := h.m.heardCell(ledger)
	if text != "queued 32m" {
		t.Fatalf("heard cell = %q, want %q", text, "queued 32m")
	}
	if strings.Contains(text, "!") || strings.Contains(text, "heard") {
		t.Errorf("a queued run carries no cue and no heard: %q", text)
	}
	if style.Render(text) != th.Dim.Render(text) {
		t.Errorf("the queued cell is not dim: %q", style.Render(text))
	}
	if last, _ := h.m.lastHeard(ledger); last != text {
		t.Errorf("lastHeard = %q, want %q", last, text)
	}
}

// The age of a queued run is its engine run's CreatedAt, the moment it was
// queued, not the ledger's updated_at (adj 207, ui.queued-age).
func TestQueuedRunAgeUsesCreatedAt(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	run := source.EngineRun{ID: "q", Item: "S1", State: "queued", CreatedAt: h.now.Add(-19 * time.Minute)}
	text, _ := h.m.heardCell(engineLedger(slicercli.RelayRun{}, run))
	if text != "queued 19m" {
		t.Fatalf("heard cell = %q, want %q", text, "queued 19m")
	}
	// A run with no creation time says only that it is queued.
	if text, _ := h.m.heardCell(engineLedger(slicercli.RelayRun{}, source.EngineRun{ID: "q", State: "queued"})); text != "queued" {
		t.Errorf("heard cell without a CreatedAt = %q, want %q", text, "queued")
	}
	// A run that is running keeps its quiet-time rule.
	running := source.EngineRun{ID: "r", State: "implement", CreatedAt: h.now.Add(-19 * time.Minute)}
	ledger := engineLedger(slicercli.RelayRun{UpdatedAt: h.now.Add(-19 * time.Minute).Format(time.RFC3339Nano)}, running)
	if text, _ := h.m.heardCell(ledger); text != "! heard 19m" {
		t.Errorf("a running run keeps its cue: %q", text)
	}
}

const queuedEngine = `{"id":"engine","item":"S55","state":"queued","created_at":"2026-10-19T16:30:00Z","lineup":{` +
	`"implementer":[{"vendor":"claude","model":"sonnet","effort":"high"}],` +
	`"reviewer":[{"vendor":"codex","model":"sol","effort":"medium"},{"vendor":"claude","model":"opus","effort":"high"}]}}`

// A queued role's row says what will run before it starts: the planned vendor,
// model and effort, dim (adj 208, ui.queued-plan-source).
func TestQueuedRowShowsThePlannedLineup(t *testing.T) {
	h := treeHarness(t, nil, queuedEngine)
	th := NewTheme(false)
	h.m.cfg.Theme = th
	raw := strings.Join(h.m.paneLines(100, 40, ""), "\n")
	for _, want := range []string{
		". implementer queued  claude sonnet high",
		". reviewer    queued  codex sol medium",
		// The gate is the resumed reviewer: it follows the reviewer's row.
		". gate        queued  codex sol medium",
	} {
		if !strings.Contains(raw, th.Dim.Render(want)) {
			t.Errorf("missing the dim row %q:\n%s", want, treePaneView(h.m, 100, 40))
		}
	}
	// The pane's progress line reads "queued 12m", dim, not "last heard".
	if v := treePaneView(h.m, 100, 40); !strings.Contains(v, "[......]  queued 12m\n") || strings.Contains(v, "heard") || strings.Contains(v, " ago") {
		t.Errorf("the pane's heard cell must read queued 12m and no more:\n%s", v)
	}
	// A run with no recorded plan keeps the bare word.
	bare := treeHarness(t, nil, `{"id":"engine","item":"S55","state":"queued","created_at":"2026-10-19T16:30:00Z"}`)
	if v := treePaneView(bare.m, 100, 40); !strings.Contains(v, ". reviewer    queued\n") {
		t.Errorf("a queued row without a plan reads only queued:\n%s", v)
	}

	// Line 2 of the Running row names the planned first role, dim.
	run := h.m.byName["ork"].snap.EngineRuns[0]
	ledger, steps := engineBoard(run)
	row := h.m.engineRunRow(h.m.byName["ork"].snap, run, engineLedger(ledger, run), steps)
	if row.second != "plan: implementer claude sonnet high" {
		t.Fatalf("line 2 = %q", row.second)
	}
	line := h.m.runSecondLine(ovRow{run: row}, "", row.second, "", 100, false)
	if !strings.Contains(line, th.Dim.Render(row.second)) {
		t.Errorf("line 2 is not dim: %q", line)
	}
}

// The queued role rows of a run that is running show the plan too.
func TestQueuedRowsOfARunningRunShowThePlan(t *testing.T) {
	engine := `{"id":"engine","item":"S55","state":"implement","created_at":"2026-10-19T16:30:00Z","lineup":{` +
		`"implementer":[{"vendor":"claude","model":"sonnet","effort":"high"}],` +
		`"reviewer":[{"vendor":"codex","model":"sol","effort":"medium"}]},` +
		`"roles":[{"role":"implementer","round":1,"vendor":"claude","model":"sonnet","effort":"high","started_at":"2026-10-19T16:31:00Z"}]}`
	h := treeHarness(t, nil, engine)
	v := treePaneView(h.m, 100, 40)
	if !strings.Contains(v, ". reviewer    queued  codex sol medium") || strings.Contains(v, ". implementer queued") {
		t.Errorf("the reviewer's row shows its plan and the running implementer shows none:\n%s", v)
	}
}

// ork's own orchestrator step is dim, labelled "ork engine", and has no clock:
// its age is the relay's, not a role's (adj owner decision ui.orchestrator-label-scope).
func TestOrchestratorRowIsLabelledAndHasNoClock(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	th := NewTheme(false)
	h.m.cfg.Theme = th
	at := func(ago time.Duration) string { return h.now.Add(-ago).Format(time.RFC3339Nano) }
	steps := ledgerTreeSteps([]slicercli.Step{
		{ID: "o", Role: "orchestrator", Agent: "ork", Vendor: "codex", Model: "sol", Reasoning: "high", Phase: "start", BeganAt: at(32 * time.Minute)},
		{ID: "i", Parent: "o", Role: "implementer", Phase: "done", Vendor: "claude", Model: "sonnet", BeganAt: at(30 * time.Minute), UpdatedAt: at(20 * time.Minute)},
		{ID: "r", Parent: "o", Role: "reviewer", Phase: "done", Vendor: "claude", Model: "opus", BeganAt: at(20 * time.Minute), UpdatedAt: at(15 * time.Minute)},
		{ID: "g", Parent: "o", Role: "gate", Phase: "start", Vendor: "claude", Model: "opus", BeganAt: at(2 * time.Minute)},
	})
	lines := h.m.runTreeLines(steps, 100, 0)
	if want := th.Dim.Render(". ork engine"); lines[0] != want {
		t.Fatalf("orchestrator node = %q, want %q", lines[0], want)
	}
	plain := sgr.ReplaceAllString(lines[0], "")
	for _, not := range []string{"32m", "codex", "sol", "high", "start", "orchestrator"} {
		if strings.Contains(plain, not) {
			t.Errorf("the orchestrator node keeps %q: %q", not, plain)
		}
	}
	for i, want := range map[int]string{1: "10m", 2: "5m", 3: "2m"} {
		if !strings.Contains(sgr.ReplaceAllString(lines[i], ""), want) {
			t.Errorf("row %d lost its clock %s: %q", i, want, lines[i])
		}
	}

	// Any other orchestrator step keeps its vendor, model and clock.
	steps[0].Agent = "claude"
	other := sgr.ReplaceAllString(h.m.runTreeLines(steps, 100, 0)[0], "")
	for _, want := range []string{"orchestrator", "start", "32m", "codex sol high"} {
		if !strings.Contains(other, want) {
			t.Errorf("an orchestrator that is not ork's lost %q: %q", want, other)
		}
	}
	if strings.Contains(other, "ork engine") {
		t.Errorf("an orchestrator that is not ork's is labelled ork engine: %q", other)
	}
}

// When the only start step is ork's orchestrator, the Running row names no live
// role: its role cell reads "ork engine", dim, with no model and no task line.
func TestRunningRowDoesNotShowTheOrchestratorAsLiveWork(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	th := NewTheme(false)
	h.m.cfg.Theme = th
	began := h.now.Add(-32 * time.Minute).Format(time.RFC3339Nano)
	run := slicercli.RelayRun{ID: "l", Items: []string{"S1"}, State: "active", BeganAt: began, UpdatedAt: began,
		Steps: []slicercli.Step{{ID: "o", Role: "orchestrator", Agent: "ork", Phase: "start", Task: "S1 orchestrator", Model: "sol", BeganAt: began}}}
	row := h.m.ledgerRunRow(source.Snapshot{}, run)
	if row.role != "ork engine" || row.model != "" || row.second != "" || !row.stateOnly {
		t.Fatalf("role %q model %q second %q stateOnly %v, want ork engine, none, none, true", row.role, row.model, row.second, row.stateOnly)
	}
	line := h.m.runLine(ovRow{run: row}, 120)
	if !strings.Contains(line, th.Dim.Render("ork engine")) {
		t.Errorf("the role cell is not dim: %q", line)
	}
	if plain := sgr.ReplaceAllString(line, ""); strings.Contains(plain, "orchestrator") {
		t.Errorf("the row still names the orchestrator: %q", plain)
	}

	// A role that starts after the orchestrator is the live work, as before.
	run.Steps = append(run.Steps, slicercli.Step{ID: "i", Parent: "o", Role: "implementer", Phase: "start", Model: "sonnet", Task: "S1 implementer", BeganAt: began})
	if row := h.m.ledgerRunRow(source.Snapshot{}, run); row.role != "implementer" || row.stateOnly {
		t.Errorf("a role after the orchestrator must show: role %q stateOnly %v", row.role, row.stateOnly)
	}

	// An orchestrator that is not ork's stays live work.
	run.Steps = run.Steps[:1]
	run.Steps[0].Agent = "claude"
	if row := h.m.ledgerRunRow(source.Snapshot{}, run); row.role != "orchestrator" || row.stateOnly {
		t.Errorf("an orchestrator that is not ork's keeps its row: role %q stateOnly %v", row.role, row.stateOnly)
	}
}

// A role that fell back shows the row it is on and, dim, the last entry it
// skipped (adj 209, ui.fell-back-text). A role that did not fall back shows no
// suffix.
func TestFailedOverRowShowsFellBackFrom(t *testing.T) {
	engine := `{"id":"engine","item":"S55","state":"review","created_at":"2026-10-19T16:30:00Z","roles":[` +
		`{"role":"implementer","round":1,"vendor":"codex","model":"sol","effort":"high","started_at":"2026-10-19T16:31:00Z","ended_at":"2026-10-19T16:35:00Z","exit":"rate_limited"},` +
		`{"role":"implementer","round":1,"vendor":"claude","model":"sonnet","started_at":"2026-10-19T16:35:00Z","ended_at":"2026-10-19T16:38:00Z","exit":"ok"},` +
		`{"role":"reviewer","round":1,"vendor":"claude","model":"opus","started_at":"2026-10-19T16:38:00Z","ended_at":"2026-10-19T16:40:00Z","exit":"ok"}]}`
	h := treeHarness(t, nil, engine)
	th := NewTheme(false)
	h.m.cfg.Theme = th
	lines := h.m.runTreeLines(h.m.paneSteps(), 120, 0)
	var codex, claude, reviewer string
	for _, line := range lines {
		// The suffix names codex too, so the row is told by what comes before it.
		plain, _, _ := strings.Cut(sgr.ReplaceAllString(line, ""), "fell back")
		switch {
		case strings.Contains(plain, "codex sol high"):
			codex = line
		case strings.Contains(plain, "claude sonnet"):
			claude = line
		case strings.Contains(plain, "claude opus"):
			reviewer = line
		}
	}
	if want := th.Dim.Render("  fell back from codex sol high"); !strings.HasSuffix(claude, want) {
		t.Errorf("the fallback row lacks the dim suffix %q: %q", want, claude)
	}
	if plain := sgr.ReplaceAllString(claude, ""); !strings.Contains(plain, "claude sonnet") {
		t.Errorf("the fallback row lost its entry: %q", plain)
	}
	for name, line := range map[string]string{"the skipped row": codex, "a role that did not fall back": reviewer} {
		if line == "" || strings.Contains(line, "fell back") {
			t.Errorf("%s must show no suffix: %q", name, line)
		}
	}

	// Only a failure that ends on another entry is a fallback: a retry on the
	// same entry is not, and a role that finished ok did not fall back.
	for name, roles := range map[string]string{
		"a retry on the same entry": `{"role":"implementer","round":1,"vendor":"claude","model":"sonnet","started_at":"2026-10-19T16:31:00Z","ended_at":"2026-10-19T16:35:00Z","exit":"fail"},` +
			`{"role":"implementer","round":1,"vendor":"claude","model":"sonnet","started_at":"2026-10-19T16:35:00Z"}`,
		"a role that finished ok": `{"role":"implementer","round":1,"vendor":"codex","model":"sol","started_at":"2026-10-19T16:31:00Z","ended_at":"2026-10-19T16:35:00Z","exit":"ok"},` +
			`{"role":"implementer","round":2,"vendor":"claude","model":"sonnet","started_at":"2026-10-19T16:35:00Z"}`,
	} {
		h := treeHarness(t, nil, `{"id":"engine","item":"S55","state":"implement","created_at":"2026-10-19T16:30:00Z","roles":[`+roles+`]}`)
		if v := treePaneView(h.m, 120, 40); strings.Contains(v, "fell back") {
			t.Errorf("%s shows a fallback:\n%s", name, v)
		}
	}
}

// The Running entry with a queued run (dim queued age, planned line 2) beside a
// relay whose only live step is ork's orchestrator (S267).
func TestQueuedCuesGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		name := "queued-cues-100x30-color.txt"
		if mono {
			name = "queued-cues-100x30-mono.txt"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			h.m.cfg.Theme = NewTheme(mono)
			began := h.now.Add(-32 * time.Minute).Format(time.RFC3339)
			ledger := slicercli.RelayRun{ID: "s443", Items: []string{"S443"}, State: "active", BeganAt: began, UpdatedAt: began,
				Steps: []slicercli.Step{{ID: "o", Role: "orchestrator", Agent: "ork", Phase: "start", Task: "S443 orchestrator", BeganAt: began}}}
			s := snap(proj("ork", false), nil, nil, ledger)
			var queued source.EngineRun
			if err := json.Unmarshal([]byte(queuedEngine), &queued); err != nil {
				t.Fatal(err)
			}
			s.EngineRuns = []source.EngineRun{queued}
			h.src.set(s)
			h.start()
			h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			checkGolden(t, name, h.m.View().Content)
		})
	}
}

// The panes of a queued run, a relay with ork's orchestrator, and a role that
// fell back (S267).
func TestQueuedCuePaneGoldens(t *testing.T) {
	began := t0.Add(-32 * time.Minute).Format(time.RFC3339)
	ledger := []slicercli.RelayRun{{ID: "s443", Items: []string{"S443"}, State: "active", BeganAt: began, UpdatedAt: began, Steps: []slicercli.Step{
		{ID: "o", Role: "orchestrator", Agent: "ork", Phase: "start", Task: "S443 orchestrator", BeganAt: began},
		{ID: "i", Parent: "o", Role: "implementer", Vendor: "claude", Model: "sonnet", Phase: "start", BeganAt: t0.Add(-3 * time.Minute).Format(time.RFC3339)}}}}
	fellBack := `{"id":"engine","item":"S55","state":"review","created_at":"2026-10-19T16:30:00Z","roles":[` +
		`{"role":"implementer","round":1,"vendor":"codex","model":"sol","effort":"high","started_at":"2026-10-19T16:31:00Z","ended_at":"2026-10-19T16:35:00Z","exit":"rate_limited"},` +
		`{"role":"implementer","round":1,"vendor":"claude","model":"sonnet","started_at":"2026-10-19T16:35:00Z","ended_at":"2026-10-19T16:38:00Z","exit":"ok"},` +
		`{"role":"reviewer","round":1,"vendor":"claude","model":"opus","started_at":"2026-10-19T16:38:00Z"}]}`
	for _, c := range []struct {
		name   string
		runs   []slicercli.RelayRun
		engine string
	}{
		{"queued-pane-100x30-mono.txt", nil, queuedEngine},
		{"orchestrator-pane-100x30-mono.txt", ledger, ""},
		{"fell-back-pane-100x30-mono.txt", nil, fellBack},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := treeHarness(t, c.runs, c.engine)
			h.m.cfg.Theme = NewTheme(true)
			checkGolden(t, c.name, strings.Join(h.m.paneLines(100, 30, ""), "\n")+"\n")
		})
	}
}

// A pane too narrow for the cue beside the node's text puts it on its own dim
// line under the node, so the skipped entry is still named (S267).
func TestFellBackCueKeepsItsOwnLineInANarrowPane(t *testing.T) {
	engine := `{"id":"engine","item":"S55","state":"review","created_at":"2026-10-19T16:30:00Z","roles":[` +
		`{"role":"implementer","round":1,"vendor":"codex","model":"sol","effort":"high","started_at":"2026-10-19T16:31:00Z","ended_at":"2026-10-19T16:35:00Z","exit":"rate_limited"},` +
		`{"role":"implementer","round":1,"vendor":"claude","model":"sonnet","started_at":"2026-10-19T16:35:00Z","ended_at":"2026-10-19T16:38:00Z","exit":"ok"}]}`
	h := treeHarness(t, nil, engine)
	th := NewTheme(false)
	h.m.cfg.Theme = th
	for _, width := range []int{80, 60} {
		lines := h.m.paneLines(width, 24, "")
		at := -1
		for i, line := range lines {
			if strings.Contains(sgr.ReplaceAllString(line, ""), "claude sonnet") {
				at = i
			}
		}
		if at < 0 || at+1 >= len(lines) {
			t.Fatalf("width %d: no fallback node:\n%s", width, strings.Join(lines, "\n"))
		}
		if strings.Contains(sgr.ReplaceAllString(lines[at], ""), "fell back") {
			t.Errorf("width %d: the cue does not fit beside the node: %q", width, lines[at])
		}
		if want := "  " + th.Dim.Render("    fell back from codex sol high"); lines[at+1] != want {
			t.Errorf("width %d: next line = %q, want the dim cue %q", width, lines[at+1], want)
		}
	}
}

// With ASCII and mono the text carries the meaning without colour.
func TestQueuedAndOrchestratorCuesInMono(t *testing.T) {
	h := treeHarness(t, nil, queuedEngine)
	h.m.cfg.Theme, h.m.cfg.ASCII = NewTheme(true), true
	run := h.m.byName["ork"].snap.EngineRuns[0]
	if text, _ := h.m.heardCell(engineLedger(slicercli.RelayRun{}, run)); text != "queued 12m" {
		t.Errorf("mono heard cell = %q, want queued 12m", text)
	}
	v := treePaneView(h.m, 100, 40)
	if !strings.Contains(v, "queued 12m") || !strings.Contains(v, ". implementer queued  claude sonnet high") {
		t.Errorf("mono pane lacks the queued cues:\n%s", v)
	}
	at := h.now.Add(-32 * time.Minute).Format(time.RFC3339Nano)
	steps := ledgerTreeSteps([]slicercli.Step{{ID: "o", Role: "orchestrator", Agent: "ork", Phase: "start", BeganAt: at}})
	if line := sgr.ReplaceAllString(h.m.runTreeLines(steps, 100, 0)[0], ""); line != ". ork engine" {
		t.Errorf("mono orchestrator node = %q, want %q", line, ". ork engine")
	}
}
