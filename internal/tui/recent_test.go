// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestRecentShowsDashForUnrecordedReviewAndGate(t *testing.T) {
	for _, tt := range []struct {
		name  string
		steps []slicercli.Step
		want  string
	}{
		{"external implementer only", []slicercli.Step{{ID: "1", Role: "orchestrator"}, {ID: "2", Parent: "1", Role: "implementer"}}, "impl 1  review -  gate -"},
		{"no steps", nil, "impl 0  review -  gate -"},
		{"review recorded", []slicercli.Step{{Role: "reviewer"}}, "impl 0  review 1  gate -"},
		{"gate recorded", []slicercli.Step{{Role: "gate"}}, "impl 0  review -  gate 1"},
		{"nested rounds recorded", []slicercli.Step{{ID: "1", Role: "implementer"}, {ID: "2", Parent: "1", Role: "reviewer"}, {ID: "3", Parent: "1", Role: "reviewer"}, {ID: "4", Parent: "2", Role: "gate"}}, "impl 1  review 2  gate 1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			s := snap(proj("ork", false), nil, nil)
			s.BoardRuns.Value = []slicercli.RelayRun{{ID: "external", State: "ended", Outcome: "pass", BeganAt: h.now.Add(-time.Hour).Format(time.RFC3339), UpdatedAt: h.now.Format(time.RFC3339), Steps: tt.steps}}
			h.src.set(s)
			h.start()
			rows := h.m.recentRows()
			if len(rows) != 1 || !strings.Contains(rows[0].text, tt.want) {
				t.Fatalf("Recent rows = %+v, want %q", rows, tt.want)
			}
		})
	}
}

func TestRecentDurationMatchesTheLedger(t *testing.T) {
	// Wall time includes overnight waits; active time uses completed workers.
	h := newHarness(t, proj("ork", false))
	s := snap(proj("ork", false), nil, nil)
	ended := h.now.Add(-3 * time.Minute)
	s.BoardRuns.Value = []slicercli.RelayRun{{
		ID: "overnight", State: "ended", Outcome: "pass",
		BeganAt:   ended.Add(-13*time.Hour - 27*time.Minute).Format(time.RFC3339),
		UpdatedAt: ended.Format(time.RFC3339),
		Steps:     []slicercli.Step{{Role: "implementer", Phase: "done", BeganAt: ended.Add(-100 * time.Minute).Format(time.RFC3339), UpdatedAt: ended.Add(-time.Minute).Format(time.RFC3339)}},
	}}
	h.src.set(s)
	h.start()
	for _, later := range []time.Duration{0, 10 * time.Minute} {
		h.now = h.now.Add(later)
		rows := h.m.recentRows()
		if len(rows) != 1 || rows[0].cells[2] != "13h27m (active 1h39m)" || rows[0].cells[3] != "ended "+elapsed(h.now.Sub(ended))+" ago" {
			t.Fatalf("Recent rows = %+v, want frozen wall time 13h27m, active 1h39m and current ended age", rows)
		}
	}
}

func TestRecentShowsActiveTimeAsTheUnionOfWorkerSteps(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	s := snap(proj("ork", false), nil, nil)
	ended := h.now.Add(-3 * time.Minute)
	stamp := func(minutes int) string {
		return ended.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339Nano)
	}
	// Deliberately unordered: duplicate and nested intervals must not add time.
	s.BoardRuns.Value = []slicercli.RelayRun{{
		ID: "union", State: "ended", BeganAt: stamp(-807), UpdatedAt: stamp(0),
		Steps: []slicercli.Step{
			{Role: "gate", Phase: "fail", BeganAt: stamp(-40), UpdatedAt: stamp(-20)},
			{Role: "implementer", Phase: "done", BeganAt: stamp(-160), UpdatedAt: stamp(-100)},
			{Role: "orchestrator", Phase: "done", BeganAt: stamp(-807), UpdatedAt: stamp(0)},
			{Role: "reviewer", Phase: "done", Parent: "impl", BeganAt: stamp(-150), UpdatedAt: stamp(-140)},
			{Role: "implementer", Phase: "done", BeganAt: stamp(-160), UpdatedAt: stamp(-100)},
			{Role: "reviewer", Phase: "done", BeganAt: stamp(-110), UpdatedAt: stamp(-90)},
			{Role: "architect", Phase: "done", BeganAt: stamp(-180), UpdatedAt: stamp(-170)},
			{Role: "implementer", Phase: "start", BeganAt: stamp(-90), UpdatedAt: stamp(0)},
			{Role: "gate", Phase: "done", BeganAt: "bad", UpdatedAt: stamp(0)},
			{Role: "reviewer", Phase: "fail", BeganAt: stamp(-90), UpdatedAt: "bad"},
			{Role: "gate", Phase: "done", BeganAt: stamp(-10), UpdatedAt: stamp(-15)},
			{Role: "other", Phase: "done", BeganAt: stamp(-807), UpdatedAt: stamp(0)},
		},
	}}
	h.src.set(s)
	h.start()
	rows := h.m.recentRows()
	if len(rows) != 1 || rows[0].cells[2] != "13h27m (active 1h40m)" {
		t.Fatalf("Recent rows = %+v, want wall 13h27m and union active 1h40m", rows)
	}
}

func TestRecentDurationWithoutUsableWorkerTimes(t *testing.T) {
	for _, tt := range []struct {
		name  string
		steps []slicercli.Step
	}{
		{"no steps", nil},
		{"no times", []slicercli.Step{{Role: "implementer", Phase: "done"}}},
		{"bad times", []slicercli.Step{{Role: "reviewer", Phase: "fail", BeganAt: "bad", UpdatedAt: "bad"}}},
		{"running", []slicercli.Step{{Role: "gate", Phase: "start"}}},
		{"orchestrator", []slicercli.Step{{Role: "orchestrator", Phase: "done"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, proj("ork", false))
			s := snap(proj("ork", false), nil, nil)
			began, ended := h.now.Add(-time.Hour).Format(time.RFC3339), h.now.Format(time.RFC3339)
			for i := range tt.steps {
				if tt.steps[i].Role == "orchestrator" || tt.steps[i].Phase == "start" {
					tt.steps[i].BeganAt, tt.steps[i].UpdatedAt = began, ended
				}
			}
			s.BoardRuns.Value = []slicercli.RelayRun{{ID: "no-times", State: "ended", BeganAt: began, UpdatedAt: ended, Steps: tt.steps}}
			h.src.set(s)
			h.start()
			rows := h.m.recentRows()
			if len(rows) != 1 || rows[0].cells[2] != "1h00m" {
				t.Fatalf("Recent rows = %+v, want wall time only", rows)
			}
		})
	}
}
