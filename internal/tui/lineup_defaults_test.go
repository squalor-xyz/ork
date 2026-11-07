// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// Adj 189 (launch.well-specified-default, S260): the TUI asks neither well
// specified nor split. A tick on Waves and the single launch plan at once with
// well specified = yes and split = no.

func TestTickPlansAtOnceWithTheDefaults(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	_, cmd := h.m.Update(press(" "))
	if cmd == nil {
		t.Fatal("space gave no plan command")
	}
	if len(l.plans) != 0 || h.m.waves.planning != "A" {
		t.Fatalf("the plan must run in the command: plans %+v planning %q", l.plans, h.m.waves.planning)
	}
	view := h.view()
	for _, text := range []string{"launch inputs", "Well specified?", "Split?"} {
		if strings.Contains(view, text) {
			t.Errorf("tick still shows %q:\n%s", text, view)
		}
	}
	for _, msg := range exec(cmd) {
		h.deliver(msg)
	}
	want := daemon.RunParams{Project: "ork", Item: "A", WellSpecified: true, Split: false}
	if len(l.plans) != 1 || l.plans[0].Project != want.Project || l.plans[0].Item != want.Item || !l.plans[0].WellSpecified || l.plans[0].Split {
		t.Fatalf("plans: %+v", l.plans)
	}
	if p := h.m.waves.picked["A"]; p.Expected == nil || !p.WellSpecified || p.Split {
		t.Fatalf("picked: %+v", p)
	}
	// A second tick plans the next row at once, with no typing in between.
	h.deliver(press("down"))
	h.deliver(press(" "))
	if len(l.plans) != 2 || l.plans[1].Item != "B" || !l.plans[1].WellSpecified || l.plans[1].Split {
		t.Fatalf("second plan: %+v", l.plans)
	}
}

func TestLaunchGoesStraightToThePlan(t *testing.T) {
	h, l := launchHarness(t)
	h.m.Update(press("l"))
	_, cmd := h.m.Update(press("enter"))
	if cmd == nil {
		t.Fatalf("enter on a ready slice gave no plan command:\n%s", h.view())
	}
	view := h.view()
	for _, text := range []string{"Well specified?", "Split?"} {
		if strings.Contains(view, text) {
			t.Errorf("launch still asks %q:\n%s", text, view)
		}
	}
	if !strings.Contains(view, "pending: run.plan") || len(l.plans) != 0 {
		t.Fatalf("the plan must run in the command:\n%s", view)
	}
	h.deliver(cmd())
	want := daemon.RunParams{Project: "ork", Item: "S90", WellSpecified: true, Split: false}
	if len(l.plans) != 1 || l.plans[0].Project != want.Project || l.plans[0].Item != want.Item || !l.plans[0].WellSpecified || l.plans[0].Split {
		t.Fatalf("plans: %+v", l.plans)
	}
}
