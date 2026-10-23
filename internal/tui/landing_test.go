// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	command := `slicer land S09 --branch s09 --note "Landed s09"`
	lines := strings.Split(v, "\n")
	found := false
	for i, line := range lines {
		if strings.Contains(line, "S09") && strings.Contains(line, "s09") && !strings.Contains(line, "slicer land") && i+1 < len(lines) {
			_, rowLine, _ := strings.Cut(line, "|")
			_, commandLine, _ := strings.Cut(lines[i+1], "|") // remove the rendered sidebar
			found = commandLine == strings.Repeat(" ", strings.Index(rowLine, "S09"))+command
			if found {
				break
			}
		}
	}
	if !found {
		t.Fatalf("missing committed run with full command on its indented second line at 80x24:\n%s", v)
	}
	for _, key := range []string{"down", "up", "down"} {
		h.send(press(key))
		selected, ok := h.m.selectedRow()
		ov := h.m.overview(60)
		if !ok || selected.id != "STOP" || ov.sel < 0 || !strings.Contains(ov.body[ov.sel], "STOP") {
			t.Fatal("cursor landed on a Ready row instead of the Needs you row")
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
	for _, want := range []string{`slicer land S09 --branch s09 --note "Landed s09"`, "S09  s09  merged", "\n" + strings.Repeat(" ", 11) + "slicer render"} {
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
	if !strings.Contains(v, "merge state unknown") || !strings.Contains(v, `slicer land S09 --branch s09 --note "Landed s09"`) || strings.Contains(v, "slicer render") {
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
			h := landingHarness(t, `[{"item":"S09","state":"committed","branch":"`+branch+`"}]`)
			h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
			command := `slicer land S09 --branch ` + branch + ` --note "Landed ` + branch + `"`
			found := false
			for _, line := range strings.Split(plainView(h.m), "\n") {
				if !strings.Contains(line, "slicer land") {
					continue
				}
				found = true
				_, body, _ := strings.Cut(line, "|")
				shown := strings.TrimSpace(body)
				if !strings.HasPrefix(body, "      slicer land") || (shown != command && !strings.HasSuffix(shown, "...")) {
					t.Fatalf("command is neither whole nor visibly clipped at fallback indent: %q", body)
				}
				if branch == "s0901" && shown != command {
					t.Fatalf("command that fits fallback indent was clipped: %q", body)
				}
			}
			if !found {
				t.Fatal("command missing from rendered 80x24 view")
			}
		})
	}
}
