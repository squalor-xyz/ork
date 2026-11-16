// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// S359 (*proposed*; owner decision adj 401): the TUI's spec check and answer
// calls send the lineup, so a launch of a local run carves like the CLI does,
// and the confirmation is of the plan that lists the slivers.

var localLineup = map[string][]runrec.LineupEntry{"implementer": {{Vendor: "claude-local", Model: "qwen3.8-27b", Effort: "medium"}}}

func carvedChain(l *launchLive) daemon.RunPlan {
	chain := l.plan
	// The daemon's chain plan names the parent as its item and as its parent
	// (internal/daemon/runs.go, planSliverChain).
	chain.Item, chain.Parent, chain.Carve = "S90", "S90", false
	chain.Slivers = []daemon.SliverPlan{{Item: "S90a", Parent: "S90", Index: 1, Count: 2}, {Item: "S90b", Parent: "S90", Index: 2, Count: 2}}
	return chain
}

func TestLaunchOfALocalSliceSendsTheLineupAndConfirmsTheChainTheCheckCarved(t *testing.T) {
	h, l := launchHarness(t)
	l.plan.Carve = true
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		l.plan = carvedChain(l) // the plan made after the check lists the slivers
		return &daemon.SpecCheckResult{Items: []daemon.SpecItemResult{{Item: items[0], Ready: true, Carved: []string{"S90a", "S90b"}}}}, nil
	}
	h.m.Update(press("l"))
	h.m.launch.params.Lineup = localLineup
	_, cmd := h.m.Update(press("enter"))
	h.deliver(cmd())
	if len(l.lineups) != 1 || !reflect.DeepEqual(l.lineups[0], map[string]map[string][]runrec.LineupEntry{"S90": localLineup}) {
		t.Fatalf("the check sends the lineup: %+v", l.lineups)
	}
	if len(l.plans) != 2 || h.m.confirm == nil || h.m.confirm.plan == nil || len(h.m.confirm.plan.Slivers) != 2 {
		t.Fatalf("plans %d, the confirmation must hold the plan with the slivers: %+v", len(l.plans), h.m.confirm)
	}
	v := h.view()
	if want := "slivers: 2, one landing of S90: S90a, S90b"; !strings.Contains(v, want) {
		t.Errorf("the confirmation lacks %q:\n%s", want, v)
	}
	if strings.Contains(v, "parent:") {
		t.Errorf("a chain plan is the parent's own: no parent line:\n%s", v)
	}
	h.deliver(press("y"))
	if len(l.starts) != 1 || l.starts[0].Expected == nil || len(l.starts[0].Expected.Slivers) != 2 {
		t.Fatalf("the start must carry the plan with the slivers: %+v", l.starts)
	}
}

func TestLaunchAnswersThatCarveReplanAndConfirmTheChain(t *testing.T) {
	h, l := launchHarness(t)
	launchQuestions(l)
	l.plan.Carve = true
	l.answerCarved = []string{"S90a", "S90b"}
	h.m.Update(press("l"))
	h.m.launch.params.Lineup = localLineup
	_, cmd := h.m.Update(press("enter"))
	h.deliver(cmd())
	l.plan = carvedChain(l)
	h.deliver(press("2"))
	h.deliver(press("t"))
	h.deliver(tea.PasteMsg{Content: "call it --widen"})
	_, rec := h.m.Update(press("enter"))
	h.deliver(rec())
	if len(l.answers) != 1 || !reflect.DeepEqual(l.answers[0].Lineup, localLineup) {
		t.Fatalf("the answers send the lineup: %+v", l.answers)
	}
	if len(l.plans) != 2 || h.m.confirm == nil || len(h.m.confirm.plan.Slivers) != 2 {
		t.Fatalf("plans %d: the confirmation must hold the plan with the slivers after the answers", len(l.plans))
	}
}

// A long chain must not push the roles and the warnings off the confirmation:
// the slivers share one wrapped line, not one line each.
func TestLaunchConfirmationOfALongChainStillShowsTheGateAndTheNote(t *testing.T) {
	h, l := launchHarness(t)
	chain := carvedChain(l)
	chain.Slivers = nil
	for i, id := range []string{"S90a", "S90b", "S90c", "S90d", "S90e", "S90f"} {
		chain.Slivers = append(chain.Slivers, daemon.SliverPlan{Item: id, Parent: "S90", Index: i + 1, Count: 6})
	}
	chain.Note = "note text"
	l.plan = chain
	launchConfirmed(t, h)
	v := h.at(80, 24)
	for _, want := range []string{"slivers: 6, one landing of S90: S90a", "S90f", "gate: Codex gpt low", "note: note text"} {
		if !strings.Contains(v, want) {
			t.Errorf("80x24 lacks %q:\n%s", want, v)
		}
	}
}

// A lineup edit that chooses a local implementer after the check makes a plan
// that still carves: the check runs again, once, to carve it.
func TestLaunchLineupEditToALocalImplementerRunsTheCheckAgainToCarve(t *testing.T) {
	h, l := launchHarness(t)
	launchConfirmed(t, h)
	if len(l.checks) != 1 {
		t.Fatalf("checks %d", len(l.checks))
	}
	l.plan.Carve = true // the plan the daemon makes for the edited lineup
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		l.plan = carvedChain(l)
		return &daemon.SpecCheckResult{Items: []daemon.SpecItemResult{{Item: items[0], Ready: true, Carved: []string{"S90a", "S90b"}}}}, nil
	}
	_, cmd := h.m.Update(press("s")) // any replan: the daemon's plan carves
	h.deliver(cmd())
	if len(l.checks) != 2 || len(l.plans) != 3 || h.m.confirm == nil || len(h.m.confirm.plan.Slivers) != 2 {
		t.Fatalf("checks %d plans %d confirm %+v", len(l.checks), len(l.plans), h.m.confirm)
	}
}

// The second check is not a loop: a plan that still carves after it goes to the
// confirmation, and the daemon refuses its start with the reason.
func TestLaunchChecksAgainOnlyOnceForAPlanThatStillCarves(t *testing.T) {
	h, l := launchHarness(t)
	launchConfirmed(t, h)
	l.plan.Carve = true
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		return &daemon.SpecCheckResult{Items: []daemon.SpecItemResult{{Item: items[0], Ready: true, Carved: []string{"S90a"}}}}, nil
	}
	_, cmd := h.m.Update(press("s"))
	h.deliver(cmd())
	if len(l.checks) != 2 || h.m.confirm == nil {
		t.Fatalf("checks %d, confirm %v", len(l.checks), h.m.confirm)
	}
}

// A plan that fails after a carve holds the item, and the status names it; the
// other ticked items still confirm.
func TestWavesHoldsAnItemWhoseCarvedPlanFailed(t *testing.T) {
	h, l := wavesHarness(t)
	l.chain = map[string][]daemon.SliverPlan{}
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		return &daemon.SpecCheckResult{Items: []daemon.SpecItemResult{{Item: "A", Ready: true, Carved: []string{"A1", "A2"}}, {Item: "B", Ready: true}}}, nil
	}
	twoWaveItems(t, h)
	l.planErr = errors.New("plan exploded")
	h.deliver(press("l"))
	if h.m.confirm == nil || len(h.m.confirm.items) != 1 || h.m.confirm.items[0].Item != "B" {
		t.Fatalf("the held item must stay out of the confirmation: %+v", h.m.confirm)
	}
	if st := h.m.actionStatus; !strings.Contains(st, "A: the plan with its carved slivers failed") || !strings.Contains(st, "plan exploded") {
		t.Fatalf("status %q", st)
	}
}

// An item unticked while its carved plan was pending is dropped without a
// failure status, whether or not the plan came back.
func TestWavesCarvedPlanOfAnUntickedItemIsDroppedQuietly(t *testing.T) {
	h, _ := wavesHarness(t)
	twoWaveItems(t, h)
	w := h.m.waves
	w.specs = map[string]daemon.SpecItemResult{"A": {Item: "A", Ready: true, Carved: []string{"A1"}}, "B": {Item: "B", Ready: true}}
	w.replanning = map[string]bool{"A": true}
	delete(w.picked, "A")
	h.m.actionStatus = ""
	h.m.applyCarvedPlan(wavesMsg{request: w.request, kind: "carved", item: "A", plan: &daemon.RunPlan{Item: "A"}})
	if h.m.actionStatus != "" || len(w.replanning) != 0 {
		t.Fatalf("status %q, replanning %v", h.m.actionStatus, w.replanning)
	}
	if _, ok := w.picked["A"]; ok {
		t.Fatal("the unticked item came back")
	}
}

func TestWavesCheckSendsTheLineupsAndPlansACarvedItemAgain(t *testing.T) {
	h, l := wavesHarness(t)
	l.chain = map[string][]daemon.SliverPlan{}
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		l.chain["A"] = []daemon.SliverPlan{{Item: "A1", Parent: "A", Index: 1, Count: 2}, {Item: "A2", Parent: "A", Index: 2, Count: 2}}
		out := &daemon.SpecCheckResult{}
		for _, id := range items {
			r := daemon.SpecItemResult{Item: id, Ready: true}
			if id == "A" {
				r.Carved = []string{"A1", "A2"}
			}
			out.Items = append(out.Items, r)
		}
		return out, nil
	}
	twoWaveItems(t, h)
	p := h.m.waves.picked["A"]
	p.Lineup = localLineup
	h.m.waves.picked["A"] = p
	h.deliver(press("l"))
	if len(l.lineups) != 1 || !reflect.DeepEqual(l.lineups[0], map[string]map[string][]runrec.LineupEntry{"A": localLineup}) {
		t.Fatalf("the check sends the lineups: %+v", l.lineups)
	}
	if h.m.confirm == nil {
		t.Fatalf("no confirmation:\n%s", h.view())
	}
	if !strings.Contains(h.view(), "slivers: 2") {
		t.Errorf("the table lacks the carved chain:\n%s", h.view())
	}
	h.deliver(press("y"))
	if len(l.starts) != 1 {
		t.Fatalf("starts: %+v", l.starts)
	}
	for _, item := range l.starts[0].Items {
		got := len(item.Expected.Slivers)
		if want := map[string]int{"A": 2, "B": 0}[item.Item]; got != want {
			t.Errorf("%s: Expected has %d slivers, want %d", item.Item, got, want)
		}
	}
}
