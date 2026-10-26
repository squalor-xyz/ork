// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func landingHarness(t *testing.T, runs string) *harness {
	t.Helper()
	h := newHarness(t, proj("ork", false), proj("other", false))
	h.start()
	s := snap(proj("ork", false), nil, nil)
	if err := json.Unmarshal([]byte(`{"EngineRuns":`+runs+`}`), &s); err != nil {
		t.Fatal(err)
	}
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	return h
}

func landingText(h *harness) string {
	return strings.Split(sgr.ReplaceAllString(strings.Join(h.m.overview(80).body, "\n"), ""), "Ready to land\n")[1]
}

func TestReadyToLandListsCommittedRuns(t *testing.T) {
	h := landingHarness(t, `[{"item":"S09","state":"committed","branch":"s09"},{"item":"STOP","state":"stopped"}]`)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := plainView(h.m)
	command := `slicer land 'S09' --branch 's09' --note 'Landed s09'`
	lines := strings.Split(v, "\n")
	found := false
	for i, line := range lines {
		if strings.Contains(line, "S09") && strings.Contains(line, "s09") && !strings.Contains(line, "slicer land") && i+1 < len(lines) {
			_, rowLine, _ := strings.Cut(line, "│")
			_, commandLine, _ := strings.Cut(lines[i+1], "│") // remove the rendered sidebar
			prefix := strings.Repeat(" ", strings.Index(rowLine, "S09"))
			if len(prefix)+len(command) > 60 {
				prefix = "      "
			}
			found = commandLine == prefix+command
			if found {
				break
			}
		}
	}
	if !found {
		t.Fatalf("missing committed run with full command on its indented second line at 80x24:\n%s", v)
	}
	for _, step := range []struct{ key, want string }{{"down", "S09"}, {"up", "STOP"}, {"down", "S09"}} {
		h.send(press(step.key))
		row, ok := h.m.selectedRow()
		ov := h.m.overview(60)
		if !ok || (row.ready && row.id != step.want) || (!row.ready && !strings.HasPrefix(row.rest, step.want+"  ")) || ov.sel < 0 || !strings.Contains(ov.body[ov.sel], step.want) {
			t.Fatalf("cursor must select %s, got %+v", step.want, row)
		}
	}
}

func TestReadyToLandShowsOnlyCommittedRuns(t *testing.T) {
	h := landingHarness(t, `[{"item":"S09","state":"committed","branch":"s09"},{"item":"STOP","state":"stopped"},{"item":"PAUSE","state":"paused"},{"item":"APPROVE","state":"awaiting-approval"},{"item":"RUN","state":"implement"}]`)
	v := landingText(h)
	for _, absent := range []string{"STOP", "PAUSE", "APPROVE", "RUN"} {
		if strings.Contains(v, absent) {
			t.Errorf("%s appears in landing:\n%s", absent, v)
		}
	}
	if !strings.Contains(v, "S09") {
		t.Fatalf("committed run missing:\n%s", v)
	}
}

func TestReadyToLandEmptyReadsNone(t *testing.T) {
	h := landingHarness(t, `[]`)
	if v := strings.TrimSpace(landingText(h)); v != "none" {
		t.Fatalf("empty landing = %q, want none", v)
	}
}

func TestReadyToLandShowsSliceRenderAfterAMerge(t *testing.T) {
	h := landingHarness(t, `[{"item":"S09","state":"committed","branch":"s09","merged":true}]`)
	v := landingText(h)
	for _, want := range []string{`slicer land 'S09' --branch 's09' --note 'Landed s09'`, "S09  s09  merged", "\n" + strings.Repeat(" ", strings.Index(v, "S09")) + "slicer render"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "merge state unknown") || strings.Contains(v, "merge first") {
		t.Fatalf("incorrect merge state:\n%s", v)
	}
}

func TestReadyToLandUnknownMergeState(t *testing.T) {
	h := landingHarness(t, `[{"item":"S09","state":"committed","branch":"s09"}]`)
	v := landingText(h)
	if !strings.Contains(v, "merge state unknown") || !strings.Contains(v, `slicer land 'S09' --branch 's09' --note 'Landed s09'`) || strings.Contains(v, "slicer render") {
		t.Fatalf("unknown merge state:\n%s", v)
	}
}

func TestReadyToLandUnmergedSaysMergeFirst(t *testing.T) {
	h := landingHarness(t, `[{"item":"S09","state":"committed","branch":"s09","merged":false}]`)
	v := landingText(h)
	if !strings.Contains(v, "S09") || !strings.Contains(v, "s09") || !strings.Contains(v, "merge first") || strings.Contains(v, "slicer land") || strings.Contains(v, "slicer render") {
		t.Fatalf("unmerged branch:\n%s", v)
	}
}

func TestReadyToLandUnknownBranch(t *testing.T) {
	h := landingHarness(t, `[{"item":"S09","state":"committed","merged":true}]`)
	v := landingText(h)
	if !strings.Contains(v, "S09") || !strings.Contains(v, "branch unknown") || strings.Contains(v, "slicer land") || strings.Contains(v, "slicer render") {
		t.Fatalf("unknown branch:\n%s", v)
	}
}

func TestReadyToLandScopesProjects(t *testing.T) {
	h := landingHarness(t, `[{"item":"S09","state":"committed","branch":"s09"}]`)
	s := snap(proj("other", false), nil, nil)
	s.EngineRuns = []source.EngineRun{{Item: "S10", State: "committed"}}
	h.m.apply(snapshotMsg{name: "other", snap: s})
	if v := landingText(h); !strings.Contains(v, "S09") || !strings.Contains(v, "S10") {
		t.Fatalf("all projects:\n%s", v)
	}
	h.m.active = 1
	if v := landingText(h); strings.Contains(v, "S09") || !strings.Contains(v, "S10") {
		t.Fatalf("selected project:\n%s", v)
	}
}

func TestReadyToLandMarksClippedCommands(t *testing.T) {
	for _, branch := range []string{"s0901", strings.Repeat("long-", 12)} {
		t.Run(branch, func(t *testing.T) {
			h := landingHarness(t, `[{"item":"STOP","state":"stopped"},{"item":"S09","state":"committed","branch":"`+branch+`"}]`)
			h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
			command := `slicer land 'S09' --branch '` + branch + `' --note 'Landed ` + branch + `'`
			found := false
			for _, line := range strings.Split(plainView(h.m), "\n") {
				if !strings.Contains(line, "slicer land") {
					continue
				}
				found = true
				_, body, _ := strings.Cut(line, "│")
				shown := strings.TrimSpace(body)
				prefix := strings.Repeat(" ", strings.Index(landingText(h), "S09"))
				if len(command)+len(prefix) > 60 {
					prefix = "      "
				}
				if !strings.HasPrefix(body, prefix+"slicer land") || (shown != command && !strings.HasSuffix(shown, "...")) {
					t.Fatalf("command is neither whole nor visibly clipped at fallback indent: %q", body)
				}
				if len(command)+6 <= 60 && shown != command {
					t.Fatalf("command that fits fallback indent was clipped: %q", body)
				}
			}
			if !found {
				t.Fatal("command missing from rendered 80x24 view")
			}
		})
	}
}

func TestReadyRowsAreSelectable(t *testing.T) {
	h := landingHarness(t, `[{"id":"need","item":"NEED","state":"stopped"},{"id":"running","item":"RUN","state":"implement"},{"id":"ready","item":"READY","state":"committed","branch":"s63","merged":true}]`)
	for _, want := range []string{"NEED", "RUN", "READY"} {
		r, ok := h.m.selectedRow()
		if !ok || (r.ready && r.id != want) || (!r.ready && !strings.HasPrefix(r.rest, want+"  ")) {
			t.Fatalf("selected=%+v want %s", r, want)
		}
		h.send(press("down"))
	}
	h.send(tea.WindowSizeMsg{Width: 80, Height: 10})
	if v := plainView(h.m); !strings.Contains(v, "READY  s63") {
		t.Fatalf("Ready row did not scroll into view:\n%s", v)
	}
}

type cleanupLive struct {
	fakeLive
	calls int
}

func (l *cleanupLive) Cleanup(_ context.Context, run string) error {
	if run != "ready" {
		return fmt.Errorf("wrong run %s", run)
	}
	l.calls++
	return nil
}
func cleanupHarness(t *testing.T, merged string) (*harness, *cleanupLive) {
	h := landingHarness(t, `[{"id":"ready","item":"S63","state":"committed","branch":"s63","worktree":"/p/wt","merged":`+merged+`}]`)
	l := &cleanupLive{}
	h.m.cfg.Live = l
	return h, l
}
func TestCleanupKeyConfirmsThenCalls(t *testing.T) {
	for _, answer := range []string{"enter", "n", "y"} {
		t.Run(answer, func(t *testing.T) {
			h, l := cleanupHarness(t, "true")
			h.send(press("c"))
			if h.m.confirm == nil {
				t.Fatal("cleanup key did not show confirmation")
			}
			command := h.m.confirm.command()
			for _, want := range []string{"worktree remove '/p/wt'", "branch -d 's63'"} {
				if !strings.Contains(command, want) {
					t.Fatalf("confirmation missing %q: %s", want, command)
				}
			}
			if l.calls != 0 {
				t.Fatal("cleanup before confirmation")
			}
			h.deliver(press(answer))
			h.deliver(press("y"))
			want := 0
			if answer == "y" {
				want = 1
			}
			if l.calls != want {
				t.Fatalf("cleanup calls=%d want %d", l.calls, want)
			}
		})
	}
}
func TestSelectingAReadyRowOffersNothingExceptCleanupOnAMergedOne(t *testing.T) {
	for _, merged := range []string{"true", "false", "null"} {
		for _, k := range []string{"enter", "a", "x", "t", "l", "c"} {
			t.Run(merged+"/"+k, func(t *testing.T) {
				h, l := cleanupHarness(t, merged)
				_, cmd := h.m.Update(press(k))
				if cmd != nil || h.m.pane != nil || h.m.launch != nil || l.calls != 0 {
					t.Fatal("Ready row acted as a run row")
				}
				if (h.m.confirm != nil) != (k == "c" && merged == "true") {
					t.Fatalf("unexpected confirmation: %+v", h.m.confirm)
				}
			})
		}
	}
}
func TestLandingCommandsQuoteShellMetacharacters(t *testing.T) {
	h := landingHarness(t, "[{\"item\":\"$item\",\"state\":\"committed\",\"branch\":\"s`id`'63\"}]")
	if v := landingText(h); !strings.Contains(v, `slicer land '$item' --branch 's`+"`id`"+`'"'"'63'`) {
		t.Fatalf("unsafe command: %s", v)
	}
}

func TestReadyDetailShowsFullCommands(t *testing.T) {
	h := landingHarness(t, `[{"id":"ready","item":"S63","state":"committed","branch":"a-very-long-branch-name-for-a-complete-command","merged":true}]`)
	row, _ := h.m.selectedRow()
	got := strings.Join(detailLines(row, 60, "|"), "\n")
	want := wrapText(strings.Join(row.commands, "\n"), 58)
	for i := range want {
		want[i] = indent + want[i]
	}
	if got != strings.Join(want, "\n") {
		t.Fatalf("detail omitted full commands: %s", got)
	}
}
func TestReadyCommandBlockScrollsTogether(t *testing.T) {
	var runs []source.EngineRun
	for i := 0; i < 25; i++ {
		runs = append(runs, source.EngineRun{ID: fmt.Sprint(i), Item: fmt.Sprintf("NEED%02d", i), State: "stopped"})
	}
	yes := true
	runs = append(runs, source.EngineRun{ID: "ready", Item: "S63", State: "committed", Branch: "s63", Merged: &yes})
	b, _ := json.Marshal(runs)
	h := landingHarness(t, string(b))
	h.m.cursor = 25
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := plainView(h.m)
	lines := strings.Split(v, "\n")
	found := false
	for i, line := range lines {
		if strings.Contains(line, "S63  s63  merged") && i+2 < len(lines) {
			found = strings.Contains(lines[i+1], "slicer land") && strings.Contains(lines[i+2], "slicer render")
			if found {
				break
			}
		}
	}
	if !found {
		t.Fatalf("selected Ready block was clipped:\n%s", v)
	}
}
func TestCleanupConfirmHasSeparateCommandsAndPrompt(t *testing.T) {
	h, _ := cleanupHarness(t, "true")
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(press("c"))
	command := h.m.confirm.command()
	if strings.Count(command, "\n") != 1 || strings.Contains(command, "&&") {
		t.Fatalf("commands not on separate lines: %s", command)
	}
	v := plainView(h.m)
	if !strings.Contains(v, "Remove worktree and branch? [y/N] (Enter is No)") || strings.Contains(v, "'s63'?") {
		t.Fatalf("missing separate prompt:\n%s", v)
	}
}
func TestCleanupConfirmKeepsThreeBodyLines(t *testing.T) {
	h, _ := cleanupHarness(t, "true")
	h.m.projects[0].snap.EngineRuns[0].Worktree = "/" + strings.Repeat("long-worktree/", 80)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(press("c"))
	v := plainView(h.m)
	lines := strings.Split(v, "\n")
	if strings.Contains(lines[2], "git -C") || strings.Contains(lines[3], "git -C") || strings.Contains(lines[4], "git -C") || !strings.Contains(v, "Remove worktree and branch?") {
		t.Fatalf("confirmation squeezed body or lost prompt:\n%s", v)
	}
}
func TestReadyActionExplainsUnavailableAction(t *testing.T) {
	for _, tc := range []struct{ merged, key, want string }{{"false", "c", "unmerged"}, {"null", "c", "unknown"}, {"true", "a", "Ready rows have no run actions"}, {"true", "x", "Ready rows have no run actions"}, {"true", "t", "Ready rows have no run actions"}} {
		t.Run(tc.merged+tc.key, func(t *testing.T) {
			h, _ := cleanupHarness(t, tc.merged)
			h.send(press(tc.key))
			if !strings.Contains(h.m.actionStatus, tc.want) {
				t.Fatalf("missing action explanation %q: %s", tc.want, h.m.actionStatus)
			}
		})
	}
	h, _ := cleanupHarness(t, "true")
	h.m.projects[0].snap.EngineRuns[0].Branch = ""
	h.send(press("c"))
	if !strings.Contains(h.m.actionStatus, "branch unknown") {
		t.Fatalf("missing branch explanation: %s", h.m.actionStatus)
	}
}
func TestCleanupConfirmGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprint(mono), func(t *testing.T) {
			h, _ := cleanupHarness(t, "true")
			h.m.cfg.Theme = NewTheme(mono)
			h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
			h.send(press("c"))
			checkGolden(t, fmt.Sprintf("cleanup-confirm-80x24-mono-%t.txt", mono), h.m.View().Content)
		})
	}
}

// A run that ork landed has nothing left to land: it leaves Ready to land. One
// whose cleanup failed stays, with the reason, so the cleanup key can finish it
// (S119, adj 162).
func TestLandedRunsLeaveReadyToLand(t *testing.T) {
	h := landingHarness(t, `[{"id":"r1","item":"S09","state":"committed","branch":"s09"},`+
		`{"id":"r2","item":"S10","state":"committed","branch":"s10","landed":true},`+
		`{"id":"r3","item":"S11","state":"committed","branch":"s11","merged":true,"worktree":"/p/.worktrees/s11","landed":true,"cleanup_failed":"landed; cleanup failed at git worktree remove /p/.worktrees/s11: locked"}]`)
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	text := landingText(h)
	if !strings.Contains(text, "S09") {
		t.Errorf("a committed run that ork did not land left the list:\n%s", text)
	}
	if strings.Contains(text, "S10") {
		t.Errorf("a landed run is still listed:\n%s", text)
	}
	if strings.Contains(text, "slicer land 'S11'") {
		t.Errorf("the landed run offers the landing command again:\n%s", text)
	}
	// The row says it once; the reason is the line under it, without the
	// "landed; cleanup failed at" lead the row already said.
	var rowLine, reasonLine string
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.Contains(l, "S11") {
			rowLine = l
			if i+1 < len(lines) {
				reasonLine = lines[i+1]
			}
		}
	}
	if !strings.Contains(rowLine, "S11  s11  landed  cleanup failed") || strings.Contains(rowLine, "merged") {
		t.Errorf("the cleanup-failed row: %q", rowLine)
	}
	if got := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(reasonLine), "│")); !strings.HasSuffix(got, "git worktree remove /p/.worktrees/s11: locked") || strings.Contains(reasonLine, "cleanup failed") {
		t.Errorf("the reason line: %q", reasonLine)
	}
	for _, row := range h.m.readyRows() {
		if row.engineID != "r3" {
			continue
		}
		if !row.merged || row.worktree == "" || row.branch == "" {
			t.Errorf("the cleanup-failed row cannot be cleaned up: %+v", row)
		}
		// What a copy of the row yanks holds no reason.
		if copied := strings.Join(row.commands, "\n"); copied != "" || strings.Contains(row.text, "locked") {
			t.Errorf("the reason is in the row's copy text: commands %q, text %q", copied, row.text)
		}
		if row.note != "git worktree remove /p/.worktrees/s11: locked" {
			t.Errorf("note %q", row.note)
		}
		detail := strings.Join(detailLines(row, 200, "|"), "\n")
		for _, want := range []string{"c finishes the cleanup: git -C ", "worktree remove '/p/.worktrees/s11'; git -C ", "branch -d 's11'"} {
			if !strings.Contains(detail, want) {
				t.Errorf("the detail lacks %q:\n%s", want, detail)
			}
		}
		if strings.Contains(detail, "locked") {
			t.Errorf("the detail repeats the reason:\n%s", detail)
		}
	}
}

// The window scrolls to the whole row of the selected cleanup-failed run, the
// note under it included.
func TestSelectedCleanupFailedRowKeepsItsReasonInView(t *testing.T) {
	var runs []string
	for i := 0; i < 30; i++ {
		runs = append(runs, fmt.Sprintf(`{"id":"r%d","item":"S%02d","state":"committed","branch":"b%d"}`, i, i, i))
	}
	runs = append(runs, `{"id":"last","item":"S99","state":"committed","branch":"s99","merged":true,"worktree":"/p/.worktrees/s99","landed":true,"cleanup_failed":"landed; cleanup failed at git worktree remove /p/.worktrees/s99: locked"}`)
	for i := 30; i < 40; i++ {
		runs = append(runs, fmt.Sprintf(`{"id":"r%d","item":"S%02d","state":"committed","branch":"b%d"}`, i, i, i))
	}
	h := landingHarness(t, "["+strings.Join(runs, ",")+"]")
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	needs, running := h.m.rows()
	h.m.cursor = len(needs) + len(running) + len(h.m.recentRows()) + 30
	if v := plainView(h.m); !strings.Contains(v, "S99") || !strings.Contains(v, "git worktree remove /p/.worktrees/s99: locked") {
		t.Fatalf("the selected cleanup-failed row lost its reason:\n%s", v)
	}
}

// The ledger run of a landed slice shows in Recent as landed, not as a bare pass.
func TestLandedRunShowsInRecent(t *testing.T) {
	h := landingHarness(t, `[{"id":"r2","item":"S10","state":"committed","branch":"s10","landed":true,"relay_run":"led-1"}]`)
	s := h.m.byName["ork"].snap
	for _, id := range []string{"led-1", "led-2"} {
		s.BoardRuns.Value = append(s.BoardRuns.Value, slicercli.RelayRun{ID: id, Items: []string{"S10"}, State: "ended", Outcome: "pass",
			BeganAt: h.now.Add(-20 * time.Minute).Format(time.RFC3339), UpdatedAt: h.now.Add(-5 * time.Minute).Format(time.RFC3339),
			Steps: []slicercli.Step{{Role: "implementer"}}})
	}
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	rows := h.m.recentRows()
	if len(rows) != 2 {
		t.Fatalf("%d recent rows: %+v", len(rows), rows)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.ledgerID] = r.text
	}
	if !strings.Contains(got["led-1"], "✓ landed") || strings.Contains(got["led-1"], "✓ pass") {
		t.Errorf("the landed slice's ledger run: %q", got["led-1"])
	}
	if !strings.Contains(got["led-2"], "✓ pass") {
		t.Errorf("an unrelated ledger run: %q", got["led-2"])
	}
}

// A cleaned run (S95) has nothing left to land or clean: it leaves Ready to
// land, with or without the landed flag, and its commands do not show.
func TestCleanedRunsLeaveReadyToLand(t *testing.T) {
	h := landingHarness(t, `[{"id":"r1","item":"S09","state":"committed","branch":"s09","merged":true},`+
		`{"id":"r2","item":"S10","state":"committed","branch":"s10","merged":true,"cleaned":true},`+
		`{"id":"r3","item":"S11","state":"committed","branch":"s11","cleaned":true,"landed":true}]`)
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	text := landingText(h)
	if !strings.Contains(text, "slicer land 'S09'") {
		t.Errorf("a merged run that is not cleaned left the list:\n%s", text)
	}
	for _, gone := range []string{"S10", "S11"} {
		if strings.Contains(text, gone) {
			t.Errorf("a cleaned run is still listed (%q):\n%s", gone, text)
		}
	}
	if n := strings.Count(text, "slicer render"); n != 1 {
		t.Errorf("slicer render shown %d times, want once (for S09 only):\n%s", n, text)
	}
	if rows := h.m.readyRows(); len(rows) != 1 || rows[0].engineID != "r1" {
		t.Errorf("ready rows: %+v", rows)
	}
}
