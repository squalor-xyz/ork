// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func engineSnapshot(t *testing.T, state, link string) source.Snapshot {
	t.Helper()
	s := snap(proj("ork", false), nil, nil, relay("ledger", "2026-10-19T16:40:00Z", "S22"))
	raw := fmt.Sprintf(`{"EngineRuns":[{"id":"engine","item":"S22","state":%q,"wait":"owner action","relay_run":%q,"created_at":"2026-10-19T16:40:00Z"}]}`, state, link)
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEngineRunRows(t *testing.T) {
	for _, state := range []string{"setup", "implement", "checks", "review", "gate", "awaiting-approval", "commit", "finish", "paused", "stopped", "committed"} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			h.start()
			h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, state, "ledger")})
			v := sgr.ReplaceAllString(strings.Join(h.m.overview(200).body, "\n"), "")
			sections := strings.Split(v, "Running")
			wantNeeds := state == "awaiting-approval" || state == "paused" || state == "stopped"
			if state == "committed" {
				if strings.Contains(strings.Split(v, "Ready to land")[0], "S22") || strings.Contains(v, "started elsewhere") {
					t.Fatalf("committed run still visible: %s", v)
				}
				return
			}
			row := "engine  S22  " + state
			if wantNeeds {
				row += "  owner action"
			}
			row += "  2 of 6 steps running"
			if state != "stopped" {
				row += "  2m"
			}
			if !wantNeeds {
				// A Running run names its slice, the state while no role works, and
				// the bar; the run id and the steps are in the pane (S145).
				row = "S22  " + state + "  [......]"
			}
			section := sections[1]
			if wantNeeds {
				section = sections[0]
			}
			if strings.Count(v, row) != 1 || !strings.Contains(section, row) || strings.Contains(v, "started elsewhere") {
				t.Fatalf("engine-led row missing or misplaced: %s", v)
			}
			badge := "ork *"
			if wantNeeds {
				badge = "ork 1"
			}
			if !strings.Contains(compactSpaces(h.view()), badge) {
				t.Fatalf("engine sidebar missing %q:\n%s", badge, h.view())
			}
		})
	}
}

func TestEngineJoinUsesRelayLink(t *testing.T) {
	for _, link := range []string{"ledger", "different"} {
		t.Run(link, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			h.start()
			h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, "implement", link)})
			v := strings.Join(h.m.overview(200).body, "\n")
			// The link joins the engine run and its ledger run into one row; a
			// ledger run under another id is a second row (S145 dropped its label).
			wantRows := 1
			if link != "ledger" {
				wantRows = 2
			}
			rows := 0
			for _, line := range strings.Split(sgr.ReplaceAllString(v, ""), "\n") {
				if strings.Contains(line, "S22  ") {
					rows++
				}
			}
			if rows != wantRows || !strings.Contains(v, "S22  implement") {
				t.Fatalf("relay-link join incorrect:\n%s", v)
			}
		})
	}
}

func TestGoldenEngineRuns(t *testing.T) {
	for _, state := range []string{"setup", "implement", "checks", "review", "gate", "awaiting-approval", "commit", "finish", "committed", "paused", "stopped"} {
		for _, size := range []struct{ w, h int }{{80, 24}, {100, 30}} {
			t.Run(fmt.Sprintf("%s-%dx%d", state, size.w, size.h), func(t *testing.T) {
				h := newHarness(t, proj("ork", false))
				h.start()
				s := engineSnapshot(t, state, "ledger")
				s.EngineRuns[0].ID = "ork-20261019-164000-abcd"
				s.EngineRuns[0].UpdatedAt = h.now.Add(-48 * time.Hour)
				if state == "committed" {
					s.EngineRuns[0].Branch = "s22"
				}
				h.m.apply(snapshotMsg{name: "ork", snap: s})
				h.m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
				checkGolden(t, fmt.Sprintf("engine-%s-%dx%d.txt", state, size.w, size.h), h.m.View().Content)
			})
		}
	}
}

func TestStoppedEngineRows(t *testing.T) {
	for _, tc := range []struct {
		name, item, at string
		want           bool
	}{
		{"newer", "S22", "2026-10-19T16:41:00Z", false},
		{"older", "S22", "2026-10-19T16:39:00Z", true},
		{"equal", "S22", "2026-10-19T16:40:00Z", true},
		{"other-slice", "S23", "2026-10-19T16:41:00Z", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			h.start()
			s := engineSnapshot(t, "stopped", "ledger")
			raw := fmt.Sprintf(`{"EngineRuns":[{"id":"old","item":"S22","state":"stopped","created_at":"2026-10-19T16:40:00Z","relay_run":"ledger"},{"id":"new","item":%q,"state":"implement","created_at":%q}]}`, tc.item, tc.at)
			if err := json.Unmarshal([]byte(raw), &s); err != nil {
				t.Fatal(err)
			}
			h.m.apply(snapshotMsg{name: "ork", snap: s})
			v := strings.Join(h.m.overview(200).body, "\n")
			if strings.Contains(v, "S22  stopped") != tc.want {
				t.Fatalf("stopped row visibility incorrect:\n%s", v)
			}
			badge := "ork *"
			if tc.want {
				badge = "ork 1*"
			}
			if !strings.Contains(compactSpaces(h.view()), badge) {
				t.Fatalf("stopped badge missing %q:\n%s", badge, h.view())
			}
		})
	}
}

func TestCommittedEngineHasNoRunningBadge(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, "committed", "ledger")})
	if row := sidebarRow(h.m, "1 ork"); row == "" || strings.Contains(row, "*") {
		t.Fatalf("committed engine retains running badge:\n%s", h.view())
	}
}

func TestEngineSidebarRunningBadge(t *testing.T) {
	for _, state := range []string{"setup", "implement", "checks", "review", "gate", "commit", "finish", "awaiting-approval", "paused", "stopped"} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			h.start()
			h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, state, "ledger")})
			want := "ork *"
			if state == "awaiting-approval" || state == "paused" || state == "stopped" {
				want = "ork 1"
			}
			if v := h.view(); !strings.Contains(compactSpaces(v), want) {
				t.Fatalf("sidebar missing %q:\n%s", want, v)
			}
		})
	}
}

func TestEngineNeedsYouProjectColumn(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "paused", "ledger")
	s.Items.Value = append(s.Items.Value, item("S23", "Owner question", holdFlag))
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	var engineLine, heldLine string
	for _, line := range h.m.overview(200).body {
		line = sgr.ReplaceAllString(line, "")
		if strings.Contains(line, "S22  paused") {
			engineLine = line
		}
		if strings.Contains(line, "S23  Owner question") {
			heldLine = line
		}
	}
	engineCol, heldCol := strings.Index(engineLine, "ork"), strings.Index(heldLine, "ork")
	if engineCol < 0 || heldCol < 0 || engineCol != heldCol {
		t.Fatalf("project columns differ: engine=%d held=%d\n%s\n%s", engineCol, heldCol, engineLine, heldLine)
	}
}

func TestEngineElapsedWithoutLedger(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "setup", "")
	s.Runs.Value = nil
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	_, rows := h.m.rows()
	if v := strings.Join(detailLines(rows[0], 200, "|"), "\n"); !strings.Contains(v, "S22  setup  2m") {
		t.Fatalf("engine elapsed from CreatedAt missing from the detail line:\n%s", v)
	}
}

func TestEngineRunDenialShowsEachCommandInline(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "implement", "ledger")
	if err := json.Unmarshal([]byte(`{"EngineRuns":[{"id":"engine","item":"S45","state":"implement","denials":["Bash: git push origin main","Write: /x.txt","Glob"]}]}`), &s); err != nil {
		t.Fatal(err)
	}
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	v := sgr.ReplaceAllString(strings.Join(h.m.overview(200).body, "\n"), "")
	if !strings.Contains(v, "denied: Bash: git push origin main, Write: /x.txt, Glob") {
		t.Fatalf("denials lack their commands:\n%s", v)
	}
}

func TestEngineRunDenials(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "implement", "ledger")
	if err := json.Unmarshal([]byte(`{"EngineRuns":[{"id":"engine","item":"S22","state":"implement","denials":["Write","Bash"]}]}`), &s); err != nil {
		t.Fatal(err)
	}
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	v := sgr.ReplaceAllString(strings.Join(h.m.overview(200).body, "\n"), "")
	if !strings.Contains(v, "denied: Write, Bash") {
		t.Fatalf("missing denials: %s", v)
	}
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	checkGolden(t, "engine-denials-100x30.txt", h.m.View().Content)
}

func TestExternalRelayStepRows(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps []slicercli.Step
		want  string
	}{
		{"orchestrator", []slicercli.Step{{Role: "orchestrator", Vendor: "claude", Model: "sonnet", Phase: "start", Task: "Run relay"}}, "running: orchestrator claude/sonnet: Run relay"},
		{"last-running", []slicercli.Step{
			{Role: "orchestrator", Vendor: "claude", Model: "sonnet", Phase: "start", Task: "Run relay"},
			{Role: "implementer", Vendor: "codex", Model: "gpt-6.1", Phase: "start", Task: "Implement S30"},
			{Role: "reviewer", Phase: "done", Task: "Finished review"},
		}, "running: implementer codex/gpt-6.1: Implement S30"},
		{"last-failed", []slicercli.Step{
			{Role: "implementer", Phase: "fail", Task: "Earlier failure"},
			{Role: "reviewer", Phase: "fail", Task: "Review S30"},
			{Role: "gate", Phase: "done", Task: "Finished gate"},
		}, "failed: reviewer: Review S30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			h.src.set(snap(proj("ork", false), nil, nil, relayWithSteps(tc.steps...)))
			h.start()
			_, rows := h.m.rows()
			v := strings.Join(detailLines(rows[0], 1000, "|"), "\n")
			if !strings.Contains(v, "ledger  S30  active, 2 of 6 steps running  2m  "+stageBar(ledgerTreeSteps(tc.steps), 0, true)+"  "+tc.want) {
				t.Fatalf("external step detail missing %q:\n%s", tc.want, v)
			}
			if strings.Contains(v, "Earlier failure") || (tc.name == "last-running" && strings.Contains(v, "running: orchestrator")) {
				t.Fatalf("earlier step selected:\n%s", v)
			}
		})
	}
}

func TestExternalRelayTaskClipped(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	task := strings.Repeat("implement ", 40)
	h.src.set(snap(proj("ork", false), nil, nil, relayWithSteps(slicercli.Step{
		Role: "implementer", Vendor: "codex", Model: "gpt-6.1", Phase: "start", Task: task,
	})))
	h.start()
	_, rows := h.m.rows()
	// Line 2 is the task, cut to the pane with an ellipsis.
	body := h.m.overview(60).body
	i := rowIndex(body, "S30")
	if i < 0 || i+1 >= len(body) {
		t.Fatalf("no row:\n%s", strings.Join(body, "\n"))
	}
	if second := sgr.ReplaceAllString(body[i+1], ""); lipgloss.Width(second) > 60 || !strings.HasSuffix(second, "...") || !strings.Contains(second, "implement implement") {
		t.Fatalf("task not clipped with ellipsis: %q", second)
	}
	if !strings.Contains(strings.Join(detailLines(rows[0], 1000, "|"), "\n"), task) {
		t.Fatal("detail lost full task")
	}
}

func TestExternalRelayKeepsItsStepsInTheDetail(t *testing.T) {
	for _, phase := range []string{"start", "fail"} {
		t.Run(phase, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			r := relayWithSteps(slicercli.Step{Role: "implementer", Vendor: "codex", Model: "gpt-6.1", Phase: phase, Task: "Implement S30"})
			h.src.set(snap(proj("ork", false), nil, nil, r))
			h.start()
			_, rows := h.m.rows()
			label := "running: implementer codex/gpt-6.1"
			if phase == "fail" {
				label = "failed: implementer"
			}
			// The row shows the role and its model, and the task on line 2; the
			// running and failed step texts are in the detail line only.
			line1 := rowText(rows[0], 300)
			if strings.Contains(line1, "running:") || strings.Contains(line1, "failed:") {
				t.Fatalf("line 1 still carries a step text: %q", line1)
			}
			if phase == "start" && !strings.Contains(line1, "implementer gpt-6.1") {
				t.Fatalf("line 1 lacks the role and model: %q", line1)
			}
			if !strings.Contains(strings.Join(detailLines(rows[0], 300, "|"), "\n"), label+": Implement S30") {
				t.Fatal("detail lost the step")
			}
		})
	}
}

func TestExternalRelayOptionalFields(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.src.set(snap(proj("ork", false), nil, nil, relayWithSteps(
		slicercli.Step{Role: "implementer", Phase: "start"},
		slicercli.Step{Role: "reviewer", Phase: "fail"},
	)))
	h.start()
	_, rows := h.m.rows()
	want := "ledger  S30  active, 2 of 6 steps running  2m  [~~!!..]  running: implementer  failed: reviewer  [implementer]>reviewer>gate"
	if got := strings.Join(detailLines(rows[0], 300, "|"), "\n"); !strings.Contains(got, want) {
		t.Fatalf("empty fields: got %q, want %q", got, want)
	}
	// With no model and no task the row shows the bare role and no second line.
	if got := rowText(rows[0], 300); !strings.Contains(got, "implementer  [~~!!..]") || rows[0].run.second != "" {
		t.Fatalf("empty fields on the row: %q", got)
	}
}

func TestOverviewSectionsAgreeWithNeedsCount(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "paused", "")
	s.Runs.Value = nil
	s.Open.Value = []adjcli.Record{{ID: 125, Key: "ui.status-list", Title: "Status list"}}
	s.Items.Value = append(s.Items.Value, item("S32", "Owner answer", holdFlag))
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	s.EngineRuns = []source.EngineRun{
		{ID: "old", Item: "S1", State: "stopped", CreatedAt: at},
		{ID: "current", Item: "S1", State: "implement", CreatedAt: at.Add(time.Second)},
		{ID: "done", Item: "S2", State: "committed"},
		{ID: "approval", Item: "S3", State: "awaiting-approval"},
		{ID: "paused", Item: "S4", State: "paused"},
		{ID: "stopped", Item: "S5", State: "stopped"},
	}
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	needs, running := h.m.rows()
	if len(needs) != 5 || h.m.projects[0].needs() != len(needs) || len(running) != 1 || running[0].engineID != "current" {
		t.Fatalf("needs=%+v count=%d running=%+v", needs, h.m.projects[0].needs(), running)
	}
	for _, rows := range [][]ovRow{needs, running} {
		for _, r := range rows {
			if r.engineID == "old" || r.engineID == "done" {
				t.Fatalf("hidden run visible: %+v", r)
			}
		}
	}
}

func TestEngineRowLeadsWithRunID(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "implement", "ledger")
	s.EngineRuns[0].ID = "ork-20261019-164000-abcd"
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	_, rows := h.m.rows()
	r := rows[0]
	if r.id != s.EngineRuns[0].ID || !strings.HasPrefix(r.text, r.id+"  S22  implement") {
		t.Fatalf("engine ID must lead the row's full text: id=%q text=%q", r.id, r.text)
	}
	// The row names the slice; the run id is in the pane and the detail line.
	if got := rowText(r, 200); !strings.HasPrefix(got, "S22  implement") || strings.Contains(got, r.id) {
		t.Fatalf("the row must lead with the slice and not carry the run id: %q", got)
	}
	if len(r.stepParts) == 0 {
		t.Fatal("running row lost step parts")
	}
}

func TestEngineRowDetailShowsRunID(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, "stopped", "ledger")})
	if got := detailText(h.m, 200); !strings.HasPrefix(got, "engine  S22  stopped") {
		t.Fatalf("detail lacks engine ID: %q", got)
	}
}

func TestLedgerRowShowsNoEngineID(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, "implement", "different")})
	_, rows := h.m.rows()
	r := rows[len(rows)-1]
	if r.id != "ledger" || r.engineID != "" || !strings.HasPrefix(r.text, "ledger  S22") || !strings.HasPrefix(rowText(r, 200), "S22") || strings.Contains(r.text, "engine") {
		t.Fatalf("ledger row changed identity: %+v", r)
	}
}

func TestRunningEngineRowsOmitEmptyLabel(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, "implement", "ledger")})
	_, rows := h.m.rows()
	a := sgr.ReplaceAllString(h.m.rowLine(rows[0], false, 3, 300), "")
	if len(rows) != 1 || rows[0].kindW != 0 || !strings.HasPrefix(a, indent+" ork  S22  implement") {
		t.Fatalf("a Running list of engine rows only must omit the empty label: %d rows, %q", len(rows), a)
	}
}

func TestRunningRowsPadToTheLabelOfAnErrorRow(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "implement", "different")
	s.EngineRuns = append(s.EngineRuns, source.EngineRun{ID: "bad", Item: "S23", State: "implement", Error: "boom", CreatedAt: s.EngineRuns[0].CreatedAt})
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	_, rows := h.m.rows()
	if len(rows) != 3 {
		t.Fatalf("%d running rows", len(rows))
	}
	var lines []string
	for _, r := range rows {
		lines = append(lines, sgr.ReplaceAllString(h.m.rowLine(r, false, 3, 300), ""))
	}
	for i, r := range rows {
		if r.kindW != kindWidth || strings.Index(lines[i], "ork") != strings.Index(lines[0], "ork") {
			t.Fatalf("mixed Running list must keep the label column: %q", lines)
		}
	}
}

func TestGoldenRunningMixedLabels(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "implement", "different")
	s.EngineRuns[0].ID = "ork-20261019-164000-abcd"
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	checkGolden(t, "running-mixed-100x30.txt", h.m.View().Content)
}

func TestLinkedLedgerRunHiddenByDesign(t *testing.T) {
	for _, superseded := range []bool{false, true} {
		t.Run(fmt.Sprint(superseded), func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			h.start()
			s := engineSnapshot(t, "committed", "ledger")
			if superseded {
				s.EngineRuns[0].State = "stopped"
				newer := s.EngineRuns[0]
				newer.ID = "new"
				newer.State = "implement"
				newer.CreatedAt = newer.CreatedAt.Add(time.Second)
				s.EngineRuns = append(s.EngineRuns, newer)
			}
			h.m.apply(snapshotMsg{name: "ork", snap: s})
			needs, running := h.m.rows()
			for _, r := range append(needs, running...) {
				if r.ledgerID == "ledger" || r.engineID == "engine" {
					t.Fatalf("hidden run leaked: %+v", r)
				}
			}
		})
	}
	b, err := os.ReadFile("../../docs/dashboard.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "A ledger run linked to a committed or superseded engine run is not shown") {
		t.Fatal("dashboard must document hidden linked ledger runs")
	}
}

func TestEngineRowShowsStateSymbol(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, tc := range []struct{ state, kind string }{{"stopped", "x stopped"}, {"paused", "= paused"}, {"awaiting-approval", "+ approve"}} {
			t.Run(fmt.Sprintf("%s/mono=%v", tc.state, mono), func(t *testing.T) {
				h := newHarness(t, proj("ork", false))
				h.start()
				h.m.cfg.Theme = NewTheme(mono)
				s := engineSnapshot(t, tc.state, "ledger")
				s.Open.Value = []adjcli.Record{openRec(3, "owner.choice", "Choose storage")}
				h.m.apply(snapshotMsg{name: "ork", snap: s})
				h.m.cursor = 0 // Select the decision so the engine kind keeps its own style.
				rows, _ := h.m.rows()
				r := rows[1]
				th := h.m.cfg.Theme
				want := th.Error
				if tc.state == "paused" {
					want = th.Held
				}
				if tc.state == "awaiting-approval" {
					want = th.Decision
				}
				if r.kind != tc.kind || r.kindW != kindWidth || r.style.Render("probe") != want.Render("probe") {
					t.Fatalf("state kind/style: got %q width %d, want %q", r.kind, r.kindW, tc.kind)
				}
				styledKind := want.Render(tc.kind[:1])
				if view := strings.Join(h.m.overview(200).body, "\n"); !strings.Contains(view, styledKind) {
					t.Fatalf("unselected kind lacks expected style %q in %q", styledKind, view)
				}
				if !strings.Contains(sgr.ReplaceAllString(h.m.rowLine(r, false, 3, 200), ""), tc.kind[:1]) {
					t.Fatal("state word missing")
				}
			})
		}
	}
}

func TestStoppedRowShowsAge(t *testing.T) {
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{{45 * time.Second, "45s ago"}, {12 * time.Minute, "12m ago"}, {3 * time.Hour, "3h ago"}, {48 * time.Hour, "2d ago"}, {0, ""}} {
		t.Run(fmt.Sprint(tc.age), func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			h.start()
			s := engineSnapshot(t, "stopped", "ledger")
			if tc.age != 0 {
				raw := fmt.Sprintf(`{"updated_at":%q}`, h.now.Add(-tc.age).Format(time.RFC3339))
				if err := json.Unmarshal([]byte(raw), &s.EngineRuns[0]); err != nil {
					t.Fatal(err)
				}
			}
			h.m.apply(snapshotMsg{name: "ork", snap: s})
			rows, _ := h.m.rows()
			got := rows[0].text
			want := "engine  S22  stopped  owner action  2 of 6 steps running"
			if tc.want != "" {
				want += "  " + tc.want
			}
			if got != want {
				t.Fatalf("stopped age: got %q, want %q", got, want)
			}
		})
	}
}

func TestSetupProjectNeedsCountsNothing(t *testing.T) {
	p := projectState{loaded: true, snap: noSlicer(proj("ork", false)), open: 2, held: 3}
	p.snap.EngineRuns = []source.EngineRun{{ID: "engine", State: "paused"}}
	if got := p.needs(); got != 0 {
		t.Fatalf("setup needs=%d, want 0", got)
	}
}

func TestEngineRunError(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "implement", "ledger")
	s.EngineRuns[0].ID = "ork-20261019-164000-abcd"
	s.EngineRuns[0].Error = "background broke"
	s.EngineRuns[0].Denials = []string{"Write"}
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	_, rows := h.m.rows()
	if len(rows) == 0 || !strings.Contains(rows[0].text, "denied: Write  error: background broke") {
		t.Fatalf("missing error after denials: %+v", rows)
	}
	if rows[0].kind != kindError {
		t.Error("background error row has no visible error marker")
	}
	if len(rows[0].stepParts) == 0 {
		t.Error("error row lost step parts")
	}
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(sgr.ReplaceAllString(h.m.View().Content, ""), s.EngineRuns[0].ID) {
		t.Error("error row clipped run ID")
	}
	checkGolden(t, "engine-error-80x24.txt", h.m.View().Content)
}

type unavailableTestSource struct {
	*fakeSource
	groups []source.Snapshot
}

func (s unavailableTestSource) UnavailableProjects(context.Context) ([]source.Snapshot, error) {
	return s.groups, nil
}

func TestUnavailableProjectGroup(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	var group source.Snapshot
	if err := json.Unmarshal([]byte(`{"Project":{"name":"removed"},"Unavailable":true,"EngineRuns":[{"id":"ork-20261019-164000-abcd","item":"S35","state":"implement","unavailable":true,"error":"project \"removed\" is not registered"}]}`), &group); err != nil {
		t.Fatal(err)
	}
	group.EngineRuns[0].Wait = source.RemovedProjectHelp("removed")
	h.m.cfg.Source = unavailableTestSource{h.src, []source.Snapshot{group}}
	live := &fakeLive{}
	h.m.cfg.Live = live
	for _, msg := range exec(h.m.Init()) {
		if _, tick := msg.(tickMsg); !tick {
			h.deliver(msg)
		}
	}
	ps := h.m.byName["removed"]
	if ps == nil {
		t.Fatal("unavailable group was not discovered")
	}
	if ps.setup() || ps.needs() != 1 {
		t.Fatalf("unavailable group treated as setup or lost need: %+v", ps)
	}
	if !strings.Contains(h.m.statusLine(60), "1 project |") {
		t.Errorf("pseudo-project counted as registered: %s", h.m.statusLine(60))
	}
	h.m.active = -1
	h.m.cursor = 0
	for _, verb := range []string{"retry", "stop", "approve", "answer"} {
		if cmd := h.m.requestAction(verb); cmd != nil || h.m.confirm != nil || !strings.Contains(h.m.actionStatus, `project "removed" is not registered`) {
			t.Errorf("unavailable %s accepted or lost error: %s", verb, h.m.actionStatus)
		}
	}
	if cmd := h.m.fetch("removed", true); cmd != nil {
		t.Error("unavailable group can refresh")
	}
	for _, msg := range exec(h.m.focus()) {
		h.deliver(msg)
	}
	for _, names := range live.focusedCalls() {
		for _, name := range names {
			if name == "removed" {
				t.Error("unavailable group sent to focus")
			}
		}
	}
	h.m.active = 1
	h.m.view = roadmapView
	if cmd := h.m.openLaunch(); cmd != nil || h.m.launch != nil {
		t.Error("unavailable group can launch")
	}
	h.m.view = wavesView
	if cmd := h.m.openWaves(); cmd != nil || h.m.waves != nil {
		t.Error("unavailable group can open wave launch")
	}
	h.m.view = 0
	h.m.active = -1
	h.m.actionStatus = ""
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	view := sgr.ReplaceAllString(h.m.View().Content, "")
	if !strings.Contains(view, "Errors 1") || !strings.Contains(strings.Join(h.m.overview(300).body, "\n"), `error: project "removed" is not registered`) {
		t.Errorf("group or error missing: %s", view)
	}
	checkGolden(t, "engine-unavailable-100x30.txt", h.m.View().Content)
}

func TestUnavailableRunActions(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "paused", "")
	s.EngineRuns[0].Unavailable = true
	s.EngineRuns[0].Error = `project "ork" is not registered`
	s.EngineRuns[0].Pending = "question"
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.m.pane = &runPane{run: s.EngineRuns[0].ID, project: "ork"}
	h.m.cfg.Live = &actionLive{src: h.src}
	pane := strings.Join(h.m.paneLines(300, 30, ""), "\n")
	if !strings.Contains(pane, "No actions:") || !strings.Contains(pane, "ork projects add PATH --name ork") || !strings.Contains(pane, "ork daemon restart") {
		t.Error("removed pane lacks actionable recovery guidance")
	}
	if strings.Contains(pane, "x stop") || strings.Contains(pane, "t retry") || strings.Contains(pane, "a approve") || strings.Contains(pane, "Answer:") || h.m.paneQuestion() {
		t.Errorf("unavailable pane offers actions: %s", pane)
	}
	for _, verb := range []string{"retry", "stop", "approve", "answer"} {
		h.m.actionStatus = ""
		if cmd := h.m.requestAction(verb); cmd != nil || h.m.confirm != nil || !strings.Contains(h.m.actionStatus, `No actions: project "ork" is not registered.`) || !strings.Contains(h.m.actionStatus, "ork daemon restart") {
			t.Errorf("unavailable action %s: %s", verb, h.m.actionStatus)
		}
	}
}

func TestDismissedRunHasNoRow(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "stopped", "")
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	if !strings.Contains(h.view(), "engine  S22") {
		t.Fatal("stopped row missing")
	}
	s.EngineRuns = nil
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	if strings.Contains(h.view(), "engine  S22") {
		t.Fatal("dismissed run still has a row")
	}
}

func TestEngineRunBoundsDenials(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "implement", "ledger")
	s.EngineRuns[0].Denials = []string{"A", "B", "C", "D", "E"}
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	v := sgr.ReplaceAllString(strings.Join(h.m.overview(200).body, "\n"), "")
	if !strings.Contains(v, "denied: A, B, C and 2 more") || strings.Contains(v, "C, D") {
		t.Fatalf("bounded denials missing:\n%s", v)
	}
}

func TestBudgetPausedRowAndPane(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	s := engineSnapshot(t, "paused", "ledger")
	s.EngineRuns[0].Pending = "budget"
	s.EngineRuns[0].Wait = "token budget 9000 reached (9100 used); next: ork retry " + s.EngineRuns[0].ID
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	v := sgr.ReplaceAllString(strings.Join(h.m.overview(200).body, "\n"), "")
	if !strings.Contains(strings.Split(v, "Running")[0], "S22  paused  "+s.EngineRuns[0].Wait) {
		t.Fatalf("budget row missing from Needs you: %s", v)
	}
	h.m.pane = &runPane{run: s.EngineRuns[0].ID, project: "ork"}
	h.m.cfg.Live = &actionLive{src: h.src}
	pane := sgr.ReplaceAllString(strings.Join(h.m.paneLines(200, 40, ""), "\n"), "")
	if !strings.Contains(pane, s.EngineRuns[0].Wait) || !strings.Contains(wideHint(h), "t retry") {
		t.Fatalf("budget pane missing reason or grant hint: %s", pane)
	}
	narrow := sgr.ReplaceAllString(strings.Join(h.m.paneLines(30, 40, ""), "\n"), "")
	if !strings.Contains(sgr.ReplaceAllString(h.m.hintLine(30), ""), "esc close pane") {
		t.Fatalf("narrow budget pane missing close hint: %s", narrow)
	}
}
