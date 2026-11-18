// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

// S431 (*proposed*; adj 464): while the daemon drains for its own rebuild the
// status line says so, also while roles run (the behind notice is silent then),
// and a failed build is said too. A narrow line keeps the head of the notice.
func TestStatusLineShowsTheRebuild(t *testing.T) {
	m := goldenModel(NewTheme(true), 160, 30)
	m.projects[0].snap.SelfHost, m.projects[0].snap.BehindMain = true, true
	m.projects[0].snap.RolesRunning = true
	m.projects[0].snap.Rebuild = &source.Rebuild{State: "draining", Waiting: 2, Message: "rebuild pending: waiting for 2 roles"}
	if got := stripANSI(m.statusLine(150)); !strings.Contains(got, " rebuild pending: waiting for 2 roles") {
		t.Fatalf("status line of a drain: %q", got)
	}
	if got := stripANSI(m.statusLine(30)); !strings.HasPrefix(got, " rebuild pending") || lipgloss.Width(got) > 30 {
		t.Fatalf("narrow status line of a drain: %q", got)
	}
	m.projects[0].snap.RolesRunning = false
	m.projects[0].snap.Rebuild = &source.Rebuild{State: "failed", Message: "daemon not rebuilt: build failed (exit 1)"}
	if got := stripANSI(m.statusLine(150)); !strings.Contains(got, "daemon not rebuilt: build failed (exit 1)") || strings.Contains(got, "behind main") {
		t.Fatalf("status line of a failed build: %q", got)
	}
	m.projects[0].snap.Rebuild = nil
	if got := stripANSI(m.statusLine(150)); !strings.Contains(got, "daemon behind main") {
		t.Fatalf("no rebuild must leave the manual notice: %q", got)
	}
}

// S431 (*proposed*; adj 464): a run whose next role start waits for the daemon's
// rebuild says so on line 2 of its Running row, instead of its output tail, and
// in its pane. Another run keeps its tail.
func TestHeldRunShowsTheRebuildWait(t *testing.T) {
	for _, state := range []string{"implement", "review", "checks"} {
		t.Run(state, func(t *testing.T) {
			m := goldenModel(plainTheme(), 100, 30)
			s := m.projects[0].snap
			s.EngineRuns = []source.EngineRun{
				{ID: "held", Item: "S12", State: state, Wait: engine.HoldReason, Tail: "last tail line here", CreatedAt: time.Date(2026, 10, 19, 16, 30, 0, 0, time.UTC)},
				{ID: "busy", Item: "S13", State: state, Tail: "other tail line", CreatedAt: time.Date(2026, 10, 19, 16, 30, 0, 0, time.UTC)},
			}
			m.Update(snapshotMsg{name: s.Project.Name, snap: s})
			body := stripANSI(strings.Join(m.overview(120).body, "\n"))
			if !strings.Contains(body, engine.HoldReason) || strings.Contains(body, "last tail line here") || !strings.Contains(body, "other tail line") {
				t.Fatalf("Running rows:\n%s", body)
			}
			m.pane = &runPane{run: "held", project: s.Project.Name}
			if _, reason, ok := m.paneRow(); !ok || reason != engine.HoldReason {
				t.Fatalf("pane reason = %q (found %v), want %q", reason, ok, engine.HoldReason)
			}
			m.pane = &runPane{run: "busy", project: s.Project.Name}
			if _, reason, ok := m.paneRow(); !ok || reason != "" {
				t.Fatalf("pane reason of an ordinary run = %q (found %v)", reason, ok)
			}
		})
	}
}

func TestLandedRebuildNotice(t *testing.T) {
	for _, tc := range []struct {
		name                string
		self, behind, build bool
		auto                bool // auto_rebuild is on (S431; adj 469): no manual restart hint
	}{
		{"configured", true, true, true, false}, {"not-self", false, true, true, false}, {"current", true, false, true, false}, {"unset", true, true, false, false},
		{"auto-rebuild", true, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := actionHarness(t, "awaiting-approval", "")
			ps := h.m.byName["ork"]
			ps.snap.SelfHost, ps.snap.BehindMain = tc.self, tc.behind
			if tc.build {
				p := ps.reg
				json.Unmarshal([]byte(`{"build_command":"./build","auto_rebuild":`+fmt.Sprint(tc.auto)+`}`), &p)
				ps.reg = p
				ps.snap.Project = p
			}
			h.m.cfg.Live.(*actionLive).src.set(ps.snap)
			// Main moves at landing: the model's cached build can still be current.
			ps.snap.BehindMain = false
			h.m.cfg.Live.(*actionLive).approved = runrec.Record{State: runrec.StateCommitted, LandedAt: time.Now()}
			h.deliver(press("a"))
			_, cmd := h.m.Update(press("y"))
			if cmd == nil {
				t.Fatal("no approval command")
			}
			h.deliver(cmd())
			got := h.view()
			want := "landed S22"
			hint := tc.self && tc.behind && tc.build && !tc.auto
			if hint {
				want += "; ork daemon restart --rebuild"
			}
			// Only the action line is judged: the status line may show the behind
			// notice from the snapshot until the daemon starts its drain.
			landed := ""
			for _, line := range strings.Split(got, "\n") {
				if strings.Contains(line, "landed S22") {
					landed = line
				}
			}
			if !strings.Contains(got, want) || (!hint && strings.Contains(landed, "--rebuild")) {
				t.Fatalf("landed line lacks %q:\n%s", want, got)
			}
		})
	}
}
