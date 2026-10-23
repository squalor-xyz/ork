// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
				if strings.Contains(strings.Split(v, "Ready to land")[0], "S22") || strings.Contains(v, kindExternal) {
					t.Fatalf("committed run still visible: %s", v)
				}
				return
			}
			row := "S22  " + state + "  owner action  2 of 6 steps running  2m"
			section := sections[1]
			if wantNeeds {
				section = sections[0]
			}
			if strings.Count(v, row) != 1 || !strings.Contains(section, row) || strings.Contains(v, kindExternal) {
				t.Fatalf("engine-led row missing or misplaced: %s", v)
			}
			badge := "ork 0*"
			if wantNeeds {
				badge = "ork 1"
			}
			if !strings.Contains(h.view(), badge) {
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
			wantExternal := link != "ledger"
			if strings.Contains(v, kindExternal) != wantExternal || !strings.Contains(v, "S22  implement  owner action") {
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
			badge := "ork 0*"
			if tc.want {
				badge = "ork 1*"
			}
			if !strings.Contains(h.view(), badge) {
				t.Fatalf("stopped badge missing %q:\n%s", badge, h.view())
			}
		})
	}
}

func TestCommittedEngineHasNoRunningBadge(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, "committed", "ledger")})
	if !strings.Contains(h.view(), "ork 0 ") {
		t.Fatalf("committed engine retains running badge:\n%s", h.view())
	}
}

func TestEngineSidebarRunningBadge(t *testing.T) {
	for _, state := range []string{"setup", "implement", "checks", "review", "gate", "commit", "finish", "awaiting-approval", "paused", "stopped"} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			h.start()
			h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, state, "ledger")})
			want := "ork 0*"
			if state == "awaiting-approval" || state == "paused" || state == "stopped" {
				want = "ork 1 "
			}
			if v := h.view(); !strings.Contains(v, want) {
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
	v := sgr.ReplaceAllString(strings.Join(h.m.overview(200).body, "\n"), "")
	if !strings.Contains(v, "S22  setup  owner action  2m") {
		t.Fatalf("engine elapsed from CreatedAt missing:\n%s", v)
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
			v := sgr.ReplaceAllString(strings.Join(h.m.overview(300).body, "\n"), "")
			if !strings.Contains(v, "ledger  S30  active, 2 of 6 steps running  2m  "+tc.want) {
				t.Fatalf("external step row missing %q:\n%s", tc.want, v)
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
	prefix := "ledger  S30  active, 2 of 6 steps running  2m  running: implementer codex/gpt-6.1: "
	got := rowText(rows[0], len(prefix)+20)
	if got != prefix+task[:17]+"..." {
		t.Fatalf("task not clipped with ellipsis: %q", got)
	}
	if !strings.Contains(strings.Join(detailLines(rows[0], 1000), "\n"), task) {
		t.Fatal("detail lost full task")
	}
}

func TestExternalRelayWholePartClipping(t *testing.T) {
	for _, phase := range []string{"start", "fail"} {
		t.Run(phase, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			r := relayWithSteps(slicercli.Step{Role: "implementer", Vendor: "codex", Model: "gpt-6.1", Phase: phase, Task: "Implement S30"})
			h.src.set(snap(proj("ork", false), nil, nil, r))
			h.start()
			_, rows := h.m.rows()
			base := "ledger  S30  active, 2 of 6 steps running  2m"
			label := "running: implementer codex/gpt-6.1"
			if phase == "fail" {
				label = "failed: implementer"
			}
			for _, extra := range []int{0, 5, len(label) + 2, len(label) + 7} {
				if got := rowText(rows[0], len(base)+extra); got != base {
					t.Errorf("partial step must be dropped, room %d: got %q, want %q", len(base)+extra, got, base)
				}
			}
			noSteps := r
			noSteps.Steps = nil
			h.m.apply(snapshotMsg{name: "ork", snap: snap(proj("ork", false), nil, nil, noSteps)})
			_, oldRows := h.m.rows()
			for room := 10; room <= len(base); room++ {
				if got, want := rowText(rows[0], room), rowText(oldRows[0], room); got != want {
					t.Fatalf("step changed base clipping at %d: got %q, want %q", room, got, want)
				}
			}
			if !strings.Contains(strings.Join(detailLines(rows[0], 300), "\n"), label+": Implement S30") {
				t.Fatal("detail lost dropped step")
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
	want := "ledger  S30  active, 2 of 6 steps running  2m  running: implementer  failed: reviewer  [implementer]>reviewer>gate"
	if got := rowText(rows[0], 300); got != want {
		t.Fatalf("empty fields: got %q, want %q", got, want)
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
