// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

type wavesLive struct {
	fakeLive
	suggest                                   *slicercli.ParallelPlan
	overlap                                   *slicercli.Overlap
	suggests                                  []string
	overlaps                                  [][]string
	plans                                     []daemon.RunParams
	starts                                    []daemon.WaveStartParams
	writes                                    int
	suggestErr, overlapErr, planErr, startErr error
	result                                    *daemon.WaveResult
}

func (l *wavesLive) WaveSuggest(_ context.Context, p string) (*slicercli.ParallelPlan, error) {
	l.suggests = append(l.suggests, p)
	return l.suggest, l.suggestErr
}
func (l *wavesLive) WaveOverlap(_ context.Context, p string, ids []string) (*slicercli.Overlap, error) {
	l.overlaps = append(l.overlaps, append([]string{p}, ids...))
	return l.overlap, l.overlapErr
}
func (l *wavesLive) Plan(_ context.Context, p daemon.RunParams) (*daemon.RunPlan, error) {
	l.plans = append(l.plans, p)
	plan := &daemon.RunPlan{Item: p.Item, Lineup: map[string][]runrec.LineupEntry{
		"implementer": {{Vendor: "claude", Model: "sonnet", Effort: "high"}},
		"reviewer":    {{Vendor: "claude", Model: "opus", Effort: "medium"}},
		"gate":        {{Vendor: "claude", Model: "opus", Effort: "medium"}},
	}}
	for role, chain := range p.Lineup {
		plan.Lineup[role] = append([]runrec.LineupEntry(nil), chain...)
	}
	plan.Lineup["gate"] = append([]runrec.LineupEntry(nil), plan.Lineup["reviewer"]...)
	if p.Budgets != nil {
		plan.Budgets = *p.Budgets
	}
	return plan, l.planErr
}
func (l *wavesLive) WaveStart(_ context.Context, p daemon.WaveStartParams) (*daemon.WaveResult, error) {
	l.writes++
	l.starts = append(l.starts, p)
	return l.result, l.startErr
}
func (l *wavesLive) Start(context.Context, daemon.RunParams) (runrec.Record, error) {
	l.writes++
	return runrec.Record{}, nil
}
func (l *wavesLive) Approve(context.Context, string) error        { l.writes++; return nil }
func (l *wavesLive) Stop(context.Context, string) error           { l.writes++; return nil }
func (l *wavesLive) Retry(context.Context, string) error          { l.writes++; return nil }
func (l *wavesLive) Answer(context.Context, string, string) error { l.writes++; return nil }
func wavesHarness(t *testing.T) (*harness, *wavesLive) {
	t.Helper()
	h := newHarness(t, proj("ork", false), proj("other", false))
	h.start()
	var plan slicercli.ParallelPlan
	if err := json.Unmarshal([]byte(`{"waves":[{"wave":2,"items":[{"id":"A","title":"Alpha"},{"id":"B","title":"Beta"}]},{"wave":4,"items":[{"id":"C","title":"Gamma"}]}],"unplaced":[{"id":"D","title":"Delta","reason":"no_files"}]}`), &plan); err != nil {
		t.Fatal(err)
	}
	l := &wavesLive{suggest: &plan, overlap: &slicercli.Overlap{Overlap: []slicercli.OverlapPair{{A: "A", B: "B", Shared: []string{"shared.go", "second.go"}}}}, result: &daemon.WaveResult{Wave: "wave-test"}}
	h.m.cfg.Live = l
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.m.switchTo(0)
	return h, l
}
func openWaves(t *testing.T, h *harness) {
	t.Helper()
	h.deliver(press("tab")) // Questions
	h.deliver(press("tab"))
	if h.m.view != 2 {
		t.Fatalf("tab reached %d, want Waves", h.m.view)
	}
}
func tickWave(t *testing.T, h *harness, well, split string) {
	t.Helper()
	h.deliver(press(" "))
	if !strings.Contains(h.view(), "Well specified? [y/n]:") || !strings.Contains(h.view(), "Split? [y/n]:") {
		t.Fatalf("both inputs must be visible:\n%s", h.view())
	}
	h.deliver(tea.PasteMsg{Content: well})
	h.deliver(press("tab"))
	h.deliver(tea.PasteMsg{Content: split})
	h.deliver(press("enter"))
}
func twoWaveItems(t *testing.T, h *harness) {
	t.Helper()
	openWaves(t, h)
	tickWave(t, h, "YeS", "nO")
	h.deliver(press("down"))
	tickWave(t, h, "n", "y")
}
func TestWavesListsSuggestedWaves(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	v := h.view()
	for _, s := range []string{"Wave 2", "A  Alpha", "B  Beta", "Wave 4", "C  Gamma", "Unplaced", "D  Delta", "no_files"} {
		if !strings.Contains(v, s) {
			t.Errorf("missing %q:\n%s", s, v)
		}
	}
	if strings.Index(v, "Alpha") > strings.Index(v, "Beta") || strings.Index(v, "Beta") > strings.Index(v, "Gamma") {
		t.Fatal("wave order lost")
	}
	if !reflect.DeepEqual(l.suggests, []string{"ork"}) {
		t.Fatalf("suggests: %v", l.suggests)
	}
	h.deliver(press("right"))
	if !reflect.DeepEqual(l.suggests, []string{"ork", "other"}) {
		t.Fatalf("project change: %v", l.suggests)
	}
}
func TestWavesSuggestErrorVerbatim(t *testing.T) {
	h, l := wavesHarness(t)
	l.suggestErr = &ipc.Error{Code: "locked", Message: "Exact SUGGEST failure."}
	openWaves(t, h)
	if !strings.Contains(h.view(), "locked: Exact SUGGEST failure.") || h.m.view != 2 {
		t.Fatal(h.view())
	}
}
func TestWavesTickAsksInputsThenPlans(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	h.deliver(press(" "))
	for _, k := range []string{"enter", "tab", "enter"} {
		h.deliver(press(k))
	}
	if len(l.plans) != 0 || !strings.Contains(h.view(), "requires y or n") {
		t.Fatal("empty inputs must not plan")
	}
	h.deliver(press("tab"))
	h.deliver(tea.PasteMsg{Content: "nO"})
	h.deliver(press("shift+tab"))
	h.deliver(tea.PasteMsg{Content: "YeS"})
	h.deliver(press("enter"))
	if len(l.plans) != 1 || !l.plans[0].WellSpecified || l.plans[0].Split || l.plans[0].Item != "A" {
		t.Fatalf("plans: %+v", l.plans)
	}
	for _, s := range []string{"Lineup", "implementer: claude sonnet high", "reviewer: claude opus medium", "gate: claude opus medium", "[x] A"} {
		if !strings.Contains(h.view(), s) {
			t.Errorf("missing %s:\n%s", s, h.view())
		}
	}
	h.deliver(press(" "))
	if strings.Contains(h.view(), "[x] A") {
		t.Fatal("space did not untick")
	}
}
func TestWavesOverlapWarning(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	if !reflect.DeepEqual(l.overlaps, [][]string{{"ork", "A", "B"}}) {
		t.Fatalf("overlap calls: %v", l.overlaps)
	}
	for _, p := range []string{"shared.go", "second.go"} {
		if !strings.Contains(h.view(), "! A and B both touch "+p) {
			t.Fatal(h.view())
		}
	}
	h.deliver(press("l"))
	if h.m.confirm == nil {
		t.Fatal("warning blocked launch")
	}
	h.deliver(press("n"))
	h.deliver(press(" "))
	if strings.Contains(h.view(), "both touch") {
		t.Fatal("stale warning after untick")
	}
}
func TestWavesLaunchConfirmsOnce(t *testing.T) {
	for _, answer := range []string{"enter", "n", "esc", "y"} {
		t.Run(answer, func(t *testing.T) {
			h, l := wavesHarness(t)
			twoWaveItems(t, h)
			h.deliver(press("l"))
			if !strings.Contains(h.view(), "ork wave start ork A B? [y/N] (Enter is No)") {
				t.Fatal(h.view())
			}
			if l.writes != 0 {
				t.Fatal("write before confirm")
			}
			h.deliver(press(answer))
			if answer != "y" {
				if len(l.starts) != 0 {
					t.Fatal("cancel launched")
				}
				return
			}
			if len(l.starts) != 1 || len(l.starts[0].Items) != 2 || h.m.confirm != nil {
				t.Fatalf("starts: %+v", l.starts)
			}
			for i, p := range l.starts[0].Items {
				if p.Project != "ork" || p.Item != []string{"A", "B"}[i] || p.Expected == nil || p.Expected.Item != p.Item || p.WellSpecified != (i == 0) || p.Split != (i == 1) {
					t.Fatalf("wrong launch: %+v", p)
				}
			}
			h.deliver(press("y"))
			if len(l.starts) != 1 {
				t.Fatal("duplicate launch")
			}
		})
	}
}
func TestWavesWritesNothingBeforeConfirm(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	for _, k := range []string{"a", "x", "t", "enter", "l", "enter", "l", "n", "l", "esc"} {
		h.deliver(press(k))
	}
	if l.writes != 0 {
		t.Fatalf("%d writes before y", l.writes)
	}
}
func TestWavesErrorVerbatim(t *testing.T) {
	for _, kind := range []string{"call", "item"} {
		t.Run(kind, func(t *testing.T) {
			h, l := wavesHarness(t)
			twoWaveItems(t, h)
			e := &ipc.Error{Code: "changed_plan", Message: "Exact START failure."}
			if kind == "call" {
				l.startErr = e
			} else {
				l.result.Items = []daemon.WaveItemResult{{Item: "A", Error: e}}
			}
			h.deliver(press("l"))
			h.deliver(press("y"))
			if !strings.Contains(h.view(), "changed_plan: Exact START failure.") {
				t.Fatal(h.view())
			}
		})
	}
}
func TestWavesLaunchStatusReflectsItemErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		errors []string
	}{
		{name: "success"},
		{name: "partial failure", errors: []string{"B"}},
		{name: "all failed", errors: []string{"A", "B"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, l := wavesHarness(t)
			twoWaveItems(t, h)
			l.result.Items = []daemon.WaveItemResult{{Item: "A"}, {Item: "B"}}
			for i := range l.result.Items {
				for _, id := range tc.errors {
					if l.result.Items[i].Item == id {
						l.result.Items[i].Error = &ipc.Error{Code: "changed_plan", Message: "Exact " + id + " failure."}
					}
				}
			}
			h.deliver(press("l"))
			h.deliver(press("y"))
			status := h.m.actionStatus
			if len(tc.errors) == 0 {
				if status != "ork wave start ork A B: done" {
					t.Fatalf("successful launch status: %q", status)
				}
			} else if strings.Contains(status, ": done") {
				t.Errorf("item errors must not report success: %q", status)
			}
			for _, id := range tc.errors {
				if !strings.Contains(status, id+": changed_plan: Exact "+id+" failure.") {
					t.Errorf("missing verbatim error for %s: %q", id, status)
				}
			}
		})
	}
}
func TestWavesTickWhilePlanPendingShowsStatus(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	h.deliver(press(" "))
	h.deliver(tea.PasteMsg{Content: "y"})
	h.deliver(press("tab"))
	h.deliver(tea.PasteMsg{Content: "n"})
	_, plan := h.m.Update(press("enter"))
	if plan == nil {
		t.Fatal("missing plan command")
	}
	h.deliver(press("down"))
	h.deliver(press(" "))
	if !strings.Contains(h.view(), "A plan is pending") {
		t.Errorf("missing pending plan status:\n%s", h.view())
	}
	if h.m.waves.inputs != nil || len(h.m.waves.picked) != 1 || h.m.waves.planning != "A" || len(l.plans) != 0 {
		t.Fatal("second tick changed the pending selection")
	}
	h.deliver(plan())
	tickWave(t, h, "n", "y")
	if len(l.plans) != 2 || l.plans[0].Item != "A" || l.plans[1].Item != "B" {
		t.Fatalf("planning after pending call finished: %+v", l.plans)
	}
}
func TestTabReachesTheWavesView(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	h.deliver(press("tab"))
	if h.m.view != 3 {
		t.Fatal("Waves must precede Roadmap")
	}
	h.deliver(press("shift+tab"))
	if h.m.view != 2 {
		t.Fatal("reverse navigation skipped Waves")
	}
}
func TestWavesGolden(t *testing.T) {
	for _, tc := range []struct {
		name string
		mono bool
	}{{"waves-100x30-color.txt", false}, {"waves-100x30-mono.txt", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := wavesHarness(t)
			h.m.cfg.Theme = NewTheme(tc.mono)
			twoWaveItems(t, h)
			checkGolden(t, tc.name, h.m.View().Content)
		})
	}
}

func TestWavesPlanFailureBlocksWholeSelection(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h, "yes", "no")
	h.deliver(press("down"))
	l.planErr = &ipc.Error{Code: "plan_failed", Message: "Exact PLAN failure."}
	tickWave(t, h, "no", "yes")
	if !strings.Contains(h.view(), "plan_failed: Exact PLAN failure.") {
		t.Fatal(h.view())
	}
	h.deliver(press("l"))
	h.deliver(press("y"))
	if h.m.confirm != nil || l.writes != 0 {
		t.Fatal("failed plan must block the chosen set")
	}
	h.deliver(press(" "))
	h.deliver(press("l"))
	h.deliver(press("y"))
	if len(l.starts) != 1 || len(l.starts[0].Items) != 1 || l.starts[0].Items[0].Item != "A" {
		t.Fatalf("remaining set: %+v", l.starts)
	}
}
func TestWavesOverlapErrorVerbatimDoesNotBlock(t *testing.T) {
	h, l := wavesHarness(t)
	l.overlapErr = &ipc.Error{Code: "tool_failed", Message: "Exact OVERLAP failure."}
	twoWaveItems(t, h)
	if !strings.Contains(h.view(), "tool_failed: Exact OVERLAP failure.") {
		t.Fatal(h.view())
	}
	h.deliver(press("l"))
	h.deliver(press("y"))
	if len(l.starts) != 1 {
		t.Fatal("overlap error blocked launch")
	}
}
func TestWavesStaleSuggestAfterProjectChange(t *testing.T) {
	h, l := wavesHarness(t)
	h.m.Update(press("tab")) // Questions
	_, cmd := h.m.Update(press("tab"))
	if cmd == nil {
		t.Fatal("missing suggest command")
	}
	reply := cmd()
	h.deliver(press("right"))
	l.suggest = &slicercli.ParallelPlan{} // the old result must not replace the new state
	h.m.waves.suggest = l.suggest
	h.deliver(reply)
	if strings.Contains(h.view(), "Alpha") {
		t.Fatal("old project's suggestion applied")
	}
}
func TestWavesStalePlanAfterUntickOrNavigation(t *testing.T) {
	for _, change := range []string{"untick", "view", "project"} {
		t.Run(change, func(t *testing.T) {
			h, _ := wavesHarness(t)
			openWaves(t, h)
			h.deliver(press(" "))
			h.deliver(tea.PasteMsg{Content: "y"})
			h.deliver(press("tab"))
			h.deliver(tea.PasteMsg{Content: "n"})
			_, cmd := h.m.Update(press("enter"))
			if cmd == nil {
				t.Fatal("missing plan command")
			}
			reply := cmd()
			switch change {
			case "untick":
				h.deliver(press(" "))
			case "view":
				h.deliver(press("tab"))
			case "project":
				h.deliver(press("right"))
			}
			h.deliver(reply)
			if strings.Contains(h.view(), "sonnet") {
				t.Fatal("stale plan applied")
			}
		})
	}
}
func TestWavesStaleOverlapAfterUntick(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h, "y", "n")
	h.deliver(press("down"))
	_, cmd := h.m.Update(press(" "))
	if cmd == nil {
		t.Fatal("missing overlap command")
	}
	reply := cmd()
	h.deliver(press("esc"))
	h.deliver(reply)
	if strings.Contains(h.view(), "both touch") {
		t.Fatal("stale overlap applied")
	}
}
func TestWavesPendingLaunchSurvivesNavigationAndDeduplicates(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	h.deliver(press("l"))
	_, cmd := h.m.Update(press("y"))
	if cmd == nil {
		t.Fatal("missing start command")
	}
	h.deliver(press("tab"))
	h.deliver(press("shift+tab"))
	tickWave(t, h, "y", "n")
	h.deliver(press("l"))
	if h.m.confirm != nil || !strings.Contains(h.view(), "is pending") {
		t.Fatal("pending wave can launch twice")
	}
	h.deliver(cmd())
	if len(l.starts) != 1 || len(h.m.waveFlights) != 0 || !strings.Contains(h.view(), "done") {
		t.Fatalf("launch lost: %s", h.view())
	}
}
func TestWavesInputsCancelPasteAndNoDefaults(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	for _, answer := range []string{"Y", "YES", "n", "No"} {
		h.deliver(press(" "))
		h.deliver(tea.PasteMsg{Content: answer + "\n"})
		if len(l.plans) != 0 {
			t.Fatal("paste submitted")
		}
		h.deliver(press("enter"))
		if len(l.plans) != 0 || !strings.Contains(h.view(), "Split? requires y or n") {
			t.Fatal("split defaulted")
		}
		h.deliver(press("esc"))
		if strings.Contains(h.view(), "[x] A") {
			t.Fatal("cancel left tick")
		}
	}
	h.deliver(press("l"))
	if h.m.confirm != nil {
		t.Fatal("empty selection launches")
	}
}
func TestWavesNeedsProjectAndDaemon(t *testing.T) {
	h, l := wavesHarness(t)
	h.m.switchTo(-1)
	openWaves(t, h)
	if !strings.Contains(h.view(), "Select a project") || len(l.suggests) != 0 {
		t.Fatal(h.view())
	}
	h.m.cfg.Live = nil
	h.deliver(press("right"))
	h.deliver(press(" "))
	h.deliver(press("l"))
	if h.m.confirm != nil || !strings.Contains(h.view(), "No daemon connection") {
		t.Fatal(h.view())
	}
}
func TestWavesUnplacedCanBePlanned(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	for range 3 {
		h.deliver(press("down"))
	}
	tickWave(t, h, "n", "n")
	if len(l.plans) != 1 || l.plans[0].Item != "D" {
		t.Fatalf("unplaced: %+v", l.plans)
	}
}

func TestWavesInputsStayVisibleForLongLists(t *testing.T) {
	h, l := wavesHarness(t)
	for range 40 {
		l.suggest.Unplaced = append(l.suggest.Unplaced, slicercli.ParallelItem{ID: "extra", Title: "Extra row"})
	}
	openWaves(t, h)
	h.deliver(press(" "))
	if !strings.Contains(h.view(), "Well specified? [y/n]:") || !strings.Contains(h.view(), "Split? [y/n]:") {
		t.Fatal("launch fields scrolled out of view")
	}
}

func (l *wavesLive) AnswerAction(ctx context.Context, a daemon.RunAction) (string, error) {
	return "", l.Answer(ctx, a.Run, a.Text)
}
