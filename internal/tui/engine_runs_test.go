// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
				if strings.Contains(v, "S22") || strings.Contains(v, kindExternal) {
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
				h.m.apply(snapshotMsg{name: "ork", snap: engineSnapshot(t, state, "ledger")})
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
