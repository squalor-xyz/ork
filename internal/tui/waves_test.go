// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

type wavesLive struct {
	details map[string]*slicercli.Detail
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
	checks                                    [][]string // the items of each spec check
	check                                     func(items []string) (*daemon.SpecCheckResult, error)
	answers                                   []daemon.SpecAnswerParams
	answerErr                                 error
	defines                                   []daemon.WaveDefineParams // the saved-wave calls (S216)
	define                                    func(daemon.WaveDefineParams) (*daemon.SavedWave, error)
	lists                                     []string
	saved                                     []daemon.SavedWaveView
	listErr                                   error
}

func (l *wavesLive) WaveDefine(_ context.Context, p daemon.WaveDefineParams) (*daemon.SavedWave, error) {
	l.writes++
	l.defines = append(l.defines, p)
	if l.define != nil {
		return l.define(p)
	}
	return &daemon.SavedWave{Project: p.Project, Name: p.Name, Items: p.Items}, nil
}
func (l *wavesLive) WaveList(_ context.Context, project string) ([]daemon.SavedWaveView, error) {
	l.lists = append(l.lists, project)
	return l.saved, l.listErr
}

// SpecCheck answers that every item is ready unless the test sets check.
func (l *wavesLive) SpecCheck(_ context.Context, project string, items []string) (*daemon.SpecCheckResult, error) {
	l.checks = append(l.checks, append([]string{project}, items...))
	if l.check != nil {
		return l.check(items)
	}
	out := &daemon.SpecCheckResult{}
	for _, id := range items {
		out.Items = append(out.Items, daemon.SpecItemResult{Item: id, Ready: true})
	}
	return out, nil
}
func (l *wavesLive) SpecAnswer(_ context.Context, p daemon.SpecAnswerParams) (*daemon.SpecAnswerResult, error) {
	l.writes++
	l.answers = append(l.answers, p)
	if l.answerErr != nil {
		return nil, l.answerErr
	}
	return &daemon.SpecAnswerResult{Item: p.Item, Ready: true}, nil
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
	plan := &daemon.RunPlan{StopAfter: p.StopAfter, Item: p.Item, Lineup: map[string][]runrec.LineupEntry{
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
func tickWave(t *testing.T, h *harness) {
	t.Helper()
	h.deliver(press(" "))
	if strings.Contains(h.view(), "Well specified?") || strings.Contains(h.view(), "Split?") {
		t.Fatalf("tick must not ask inputs:\n%s", h.view())
	}
}
func twoWaveItems(t *testing.T, h *harness) {
	t.Helper()
	openWaves(t, h)
	tickWave(t, h)
	h.deliver(press("down"))
	tickWave(t, h)
}
func TestWavesListsSuggestedWaves(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	v := h.view()
	for _, s := range []string{"Wave 2", "A  Alpha", "B  Beta", "Wave 4", "C  Gamma", "Unplaced", "D  Delta", "no files listed"} {
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
	h.deliver(press("]"))
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
func TestWavesTickShowsThePlanAndUntickRemovesIt(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h)
	if len(l.plans) != 1 || l.plans[0].Item != "A" {
		t.Fatalf("plans: %+v", l.plans)
	}
	for _, s := range []string{"Lineup", "impl:  claude sonnet high", "rev:   claude opus medium", "gate:  claude opus medium", "[x] A"} {
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
				if p.Project != "ork" || p.Item != []string{"A", "B"}[i] || p.Expected == nil || p.Expected.Item != p.Item || !p.WellSpecified || p.Split {
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
	_, plan := h.m.Update(press(" "))
	if plan == nil {
		t.Fatal("missing plan command")
	}
	h.deliver(press("down"))
	h.deliver(press(" "))
	if !strings.Contains(h.view(), "A plan is pending") {
		t.Errorf("missing pending plan status:\n%s", h.view())
	}
	if len(h.m.waves.picked) != 1 || h.m.waves.planning != "A" || len(l.plans) != 0 {
		t.Fatal("second tick changed the pending selection")
	}
	h.deliver(plan())
	tickWave(t, h)
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
func TestWaveReasonExplainsTheEngineRule(t *testing.T) {
	if got, want := waveReason("engine: S129"), "one engine slice per wave; S129 goes first"; got != want {
		t.Fatalf("waveReason: %q, want %q", got, want)
	}
	// Other reasons keep their words.
	for reason, want := range map[string]string{"no_files": "no files listed", "after: S2": "after: S2", "engine:": "engine:", "engine: ": "engine: "} {
		if got := waveReason(reason); got != want {
			t.Errorf("waveReason(%q) = %q, want %q", reason, got, want)
		}
	}
}

func TestWavesShowsADeferredEngineSliceInItsOwnWave(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		name := fmt.Sprintf("waves-engine-%dx%d-mono.txt", size[0], size[1])
		t.Run(name, func(t *testing.T) {
			h, l := wavesHarness(t)
			h.m.cfg.Theme = NewTheme(true)
			var plan slicercli.ParallelPlan
			if err := json.Unmarshal([]byte(`{"waves":[{"wave":1,"items":[{"id":"A","title":"Alpha"},{"id":"B","title":"Beta"}]},{"wave":2,"items":[{"id":"C","title":"Gamma","reason":"engine: A"}]},{"wave":3,"items":[{"id":"D","title":"Delta","reason":"after: C"}]}]}`), &plan); err != nil {
				t.Fatal(err)
			}
			l.suggest = &plan
			h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			openWaves(t, h)
			h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			view := h.m.View().Content
			if !strings.Contains(view, "one engine slice per wave; A goes first") {
				t.Fatalf("the engine reason is not in plain words:\n%s", view)
			}
			if strings.Contains(view, "engine: A") {
				t.Fatalf("the raw reason shows:\n%s", view)
			}
			checkGolden(t, name, view)
		})
	}
}

func TestWavesGolden(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}, {120, 40}} {
		for _, mono := range []bool{false, true} {
			for _, picked := range []bool{false, true} {
				for _, ascii := range []bool{false, true} {
					theme, suffix := "color", ""
					if mono {
						theme = "mono"
					}
					if picked {
						suffix += "-lineup"
					}
					if ascii {
						suffix += "-ascii"
					}
					name := fmt.Sprintf("waves-%dx%d-%s%s.txt", size[0], size[1], theme, suffix)
					t.Run(name, func(t *testing.T) {
						h, l := wavesHarness(t)
						h.m.cfg.Theme = NewTheme(mono)
						h.m.cfg.ASCII = ascii
						h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
						l.suggest.Waves[1].Items[0].Reason = "shares: A internal/engine/shared.go"
						if picked {
							twoWaveItems(t, h)
						} else {
							openWaves(t, h)
						}
						h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
						checkGolden(t, name, h.m.View().Content)
					})
				}
			}
		}
	}
}

func TestWavesPlanFailureBlocksWholeSelection(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h)
	h.deliver(press("down"))
	l.planErr = &ipc.Error{Code: "plan_failed", Message: "Exact PLAN failure."}
	tickWave(t, h)
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
	h.deliver(press("]"))
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
			_, cmd := h.m.Update(press(" "))
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
				h.deliver(press("]"))
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
	tickWave(t, h)
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
	tickWave(t, h)
	h.deliver(press("l"))
	if h.m.confirm != nil || !strings.Contains(h.view(), "is pending") {
		t.Fatal("pending wave can launch twice")
	}
	h.deliver(cmd())
	if len(l.starts) != 1 || len(h.m.waveFlights) != 0 || !strings.Contains(h.view(), "done") {
		t.Fatalf("launch lost: %s", h.view())
	}
}
func TestWavesTickPlansAtOnceAndUntickCancels(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	_, plan := h.m.Update(press(" "))
	if plan == nil || h.m.waves.planning != "A" || !strings.Contains(h.view(), "[x] A") {
		t.Fatalf("tick did not start the plan:\n%s", h.view())
	}
	// Text pasted now reaches no field and plans nothing.
	h.deliver(tea.PasteMsg{Content: "Y\n"})
	if len(l.plans) != 0 {
		t.Fatal("paste planned")
	}
	h.deliver(press(" "))
	if strings.Contains(h.view(), "[x] A") || h.m.waves.planning != "" {
		t.Fatal("untick left the tick")
	}
	h.deliver(plan())
	if len(h.m.waves.picked) != 0 {
		t.Fatalf("a late plan reply ticked the row again: %+v", h.m.waves.picked)
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
	h.deliver(press("]"))
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
	tickWave(t, h)
	if len(l.plans) != 1 || l.plans[0].Item != "D" {
		t.Fatalf("unplaced: %+v", l.plans)
	}
}

func (l *wavesLive) AnswerAction(ctx context.Context, a daemon.RunAction) (string, error) {
	return "", l.Answer(ctx, a.Run, a.Text)
}

func (l *wavesLive) ItemShow(_ context.Context, project, id string) (*slicercli.Detail, error) {
	return l.details[id], nil
}

func TestWavesMarksEngineConflict(t *testing.T) {
	h, l := wavesHarness(t)
	l.details = map[string]*slicercli.Detail{
		"A": {Slice: &slicercli.Spec{Sections: []slicercli.Section{{Heading: "Files", Body: "- `internal/engine/a.go:12`"}}}},
		"B": {Slice: &slicercli.Spec{Sections: []slicercli.Section{{Heading: "Files", Body: "- `cmd/ork/main.go`"}}}},
	}
	twoWaveItems(t, h)
	if !strings.Contains(h.view(), "engine conflict: A and B") {
		t.Fatalf("conflict missing before launch:\n%s", h.view())
	}
	h.deliver(press("l"))
	if h.m.confirm != nil || l.writes != 0 {
		t.Fatal("conflicting selection could launch")
	}
	h.deliver(press(" "))
	if strings.Contains(h.view(), "engine conflict") {
		t.Fatal("conflict survived untick")
	}
	h.deliver(press("l"))
	h.deliver(press("y"))
	if len(l.starts) != 1 || len(l.starts[0].Items) != 1 {
		t.Fatal("single engine slice did not start")
	}
}

func TestSharesIsADimColumnWithThePredecessorIdOnly(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h, l := wavesHarness(t)
		h.m.cfg.Theme = NewTheme(mono)
		l.suggest.Waves[0].Items[1].Reason = "shares: S65 internal/engine/shared.go"
		openWaves(t, h)
		raw := strings.Join(h.m.wavesBody(76).body, "\n")
		plain := sgr.ReplaceAllString(raw, "")
		if !strings.Contains(plain, "Shares") || !strings.Contains(raw, h.m.cfg.Theme.Dim.Render("<S65")) || strings.Contains(plain, "shared.go") || strings.Contains(plain, "shares:") {
			t.Fatalf("shares column missing or contains path: %q", raw)
		}
	}
}

func TestEnterOnAWavesRowShowsTheSharedFiles(t *testing.T) {
	h, l := wavesHarness(t)
	l.suggest.Waves[0].Items[0].Reason = "shares: S65 internal/engine/shared.go"
	l.details = map[string]*slicercli.Detail{"A": {Title: "Alpha detail", Slice: &slicercli.Spec{Title: "Alpha detail", Sections: []slicercli.Section{{Heading: "Files", Body: "- `own.go`"}}}}}
	openWaves(t, h)
	h.deliver(press("enter"))
	for _, want := range []string{"Item A", "Alpha detail", "internal/engine/shared.go", "own.go", "esc"} {
		if !strings.Contains(h.view(), want) {
			t.Errorf("item pane missing %q:\n%s", want, h.view())
		}
	}
	for _, k := range []string{" ", "l", "e", "y", "a", "x", "t"} {
		h.deliver(press(k))
	}
	if l.writes != 0 || len(l.plans) != 0 || len(h.m.waves.picked) != 0 {
		t.Fatal("read-only pane wrote or ticked")
	}
	h.deliver(press("esc"))
	if !strings.Contains(h.view(), "Suggested") {
		t.Fatal("escape did not return to Waves")
	}
}

func TestWaveHeadersAreRulesWithCounts(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		h, _ := wavesHarness(t)
		h.m.cfg.ASCII = ascii
		openWaves(t, h)
		_, rule := h.m.cfg.Theme.rules(ascii)
		plain := sgr.ReplaceAllString(strings.Join(h.m.wavesBody(76).body, "\n"), "")
		for _, want := range []string{rule + rule + " Wave 2 ", "2 items", rule + rule + " Wave 4 ", "1 item", "Unplaced"} {
			if !strings.Contains(plain, want) {
				t.Errorf("missing %q:\n%s", want, plain)
			}
		}
	}
}

func TestLineupColumnAppearsOnlyAfterTicking(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	if strings.Contains(h.view(), "Lineup") {
		t.Fatal("empty Lineup column visible")
	}
	_, plan := h.m.Update(press(" "))
	if !strings.Contains(h.view(), "Lineup") {
		t.Fatal("ticking did not show column")
	}
	h.deliver(press(" "))
	if strings.Contains(h.view(), "Lineup") {
		t.Fatal("column remained after last tick cancelled")
	}
	h.deliver(plan())
	tickWave(t, h)
	if !strings.Contains(h.view(), "impl:") {
		t.Fatal("planned lineup missing")
	}
	h.deliver(press(" "))
	if strings.Contains(h.view(), "Lineup") {
		t.Fatal("column remained after last untick")
	}
}

func TestWavesRowsNeverClipTheKeyColumns(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		for _, picked := range []bool{false, true} {
			t.Run(fmt.Sprint(size, picked), func(t *testing.T) {
				h, l := wavesHarness(t)
				l.suggest.Waves[0].Items[0].Title = strings.Repeat("very long title ", 20)
				l.suggest.Waves[0].Items[0].Reason = "shares: S65 internal/engine/shared.go"
				h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				openWaves(t, h)
				if picked {
					tickWave(t, h)
				}
				h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				w, _, side := h.m.layoutSize()
				for _, line := range h.m.wavesBody(w - side).body {
					if lipgloss.Width(line) > w-side {
						t.Errorf("overwide row %d: %q", lipgloss.Width(line), line)
					}
				}
				v := plainView(h.m)
				if len(strings.Split(v, "\n")) != size[1] {
					t.Fatal("wrong height")
				}
				for _, line := range strings.Split(v, "\n") {
					if lipgloss.Width(line) > size[0] {
						t.Fatal("screen too wide")
					}
				}
				if rowsWith(v, "[", "A", "<S65") != 1 {
					t.Fatalf("key columns clipped:\n%s", v)
				}
			})
		}
	}
}

func TestWavesItemPaneIgnoresStaleRepliesAndScrolls(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	_, cmd := h.m.Update(press("enter"))
	old := exec(cmd)[0].(wavesMsg)
	if !strings.Contains(plainView(h.m), "pending: item.show") {
		t.Fatal("missing pending read")
	}
	h.deliver(press("esc"))
	h.deliver(press("down"))
	h.deliver(press("enter"))
	old.detail = &slicercli.Detail{Title: "STALE DETAIL"}
	h.deliver(old)
	if strings.Contains(plainView(h.m), "STALE DETAIL") {
		t.Fatal("stale detail accepted")
	}
	w := h.m.waves
	h.deliver(wavesMsg{kind: "detail", request: w.request, sequence: w.paneRequest, item: "B", err: &ipc.Error{Code: "read_failed", Message: "Exact item failure"}})
	if !strings.Contains(plainView(h.m), "read_failed: Exact item failure") {
		t.Fatal("detail error lost")
	}
	h.deliver(wavesMsg{kind: "detail", request: w.request, sequence: w.paneRequest, item: "B", detail: &slicercli.Detail{Slice: &slicercli.Spec{Title: "Beta", Sections: []slicercli.Section{{Heading: "Files", Body: strings.Repeat("more.go\n", 50) + "last-shared.go"}}}}})
	for range 100 {
		h.deliver(press("pgdown"))
	}
	if !strings.Contains(plainView(h.m), "last-shared.go") || !strings.Contains(plainView(h.m), "esc returns") {
		t.Fatal("cannot reach last file with close hint visible")
	}
	for range 100 {
		h.deliver(press("pgup"))
	}
	if w.paneOffset != 0 {
		t.Fatal("cannot return to first line")
	}
}

func TestWavesTableAlignsColumnsAndPreservesReasons(t *testing.T) {
	h, l := wavesHarness(t)
	l.suggest.Waves[0].Items[0].ID = "S1234"
	l.suggest.Waves[0].Items[0].Title = strings.Repeat("界", 60)
	l.suggest.Waves[0].Items[0].Reason = "shares: S65 path with spaces.go"
	l.suggest.Waves[0].Items[1].Reason = "after: S1234"
	openWaves(t, h)
	lines := h.m.wavesBody(60).body
	col := -1
	for _, raw := range lines {
		line := sgr.ReplaceAllString(raw, "")
		if !strings.Contains(line, "[ ]") {
			continue
		}
		if lipgloss.Width(line) != 60 {
			t.Errorf("row not full width: %q", raw)
		}
		if strings.Contains(line, "S1234") && strings.Contains(line, "<S65") {
			col = lipgloss.Width(strings.Split(line, "<S65")[0])
			if !reverseSGR.MatchString(raw) {
				t.Fatal("cursor not reversed")
			}
		}
	}
	if col != 54 {
		t.Fatalf("shares column at %d, want 54", col)
	}
	if !strings.Contains(sgr.ReplaceAllString(strings.Join(lines, "\n"), ""), "after: S1234") {
		t.Fatal("non-shares reason lost")
	}
	h.deliver(press("enter"))
	if !strings.Contains(plainView(h.m), "path with spaces.go") {
		t.Fatal("shared path with spaces lost")
	}
}

func TestWavesCursorBarCoversTheKeyColumns(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, picked := range []bool{false, true} {
			h, l := wavesHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			l.suggest.Waves[0].Items[0].Reason = "shares: S65 shared.go"
			openWaves(t, h)
			if picked {
				tickWave(t, h)
			}
			body := h.m.wavesBody(60)
			line := body.body[body.sel]
			reversed, last := false, 0
			for _, loc := range sgr.FindAllStringIndex(line, -1) {
				if loc[0] > last && !reversed {
					t.Fatalf("cursor bar gap: %q", line[last:loc[0]])
				}
				codes := strings.Split(strings.TrimSuffix(strings.TrimPrefix(line[loc[0]:loc[1]], "\x1b["), "m"), ";")
				for _, code := range codes {
					switch code {
					case "", "0", "27":
						reversed = false
					case "7":
						reversed = true
					}
				}
				last = loc[1]
			}
			if last < len(line) && !reversed {
				t.Fatalf("cursor bar ends early: %q", line)
			}
			if lipgloss.Width(line) != 60 {
				t.Fatal("cursor bar not full width")
			}
		}
	}
}

func TestWavesSelectedSharesUseUniformStyle(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h, l := wavesHarness(t)
		h.m.cfg.Theme = NewTheme(mono)
		l.suggest.Waves[0].Items[0].Reason = "shares: S65 shared.go"
		openWaves(t, h)
		body := h.m.wavesBody(60)
		line := body.body[body.sel]
		if !strings.Contains(line, h.m.cfg.Theme.TabActive.Render("<S65  ")) {
			t.Errorf("shares differ from cursor style: %q", line)
		}
		if strings.HasSuffix(line, h.m.cfg.Theme.TabActive.Render("")) && h.m.cfg.Theme.TabActive.Render("") != "" {
			t.Errorf("empty trailing style: %q", line)
		}
	}
}

func TestWavesHasOneHintBar(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	body, footer := h.m.wavesParts(60)
	if len(footer) != 0 || strings.Contains(strings.Join(body.body, "\n"), "[enter]") {
		t.Fatal("duplicate Waves footer")
	}
	if got := wideHint(h); !strings.Contains(got, "enter item") || !strings.Contains(got, "space tick;") {
		t.Fatalf("hint: %q", got)
	}
}

func TestWavesItemPaneUsesRoadmapHeadingsAndOneTitle(t *testing.T) {
	h, l := wavesHarness(t)
	title := strings.Repeat("Long item title ", 8)
	l.suggest.Waves[0].Items[0].Title = title
	l.details = map[string]*slicercli.Detail{"A": {Slice: &slicercli.Spec{Title: title, Sections: []slicercli.Section{{Heading: "Files", Body: "own.go"}}}}}
	openWaves(t, h)
	h.deliver(press("enter"))
	body := h.m.waveItemBody(60).body
	if body[0] != h.m.cfg.Theme.Heading.Render("Item A  (esc returns)") {
		t.Errorf("wrong header: %q", body[0])
	}
	titleLines := wrapText(title, 60)
	for i, want := range titleLines {
		if body[i+1] != h.m.cfg.Theme.Heading.Render(want) {
			t.Errorf("title heading mismatch at %d: %q", i, body[i+1])
		}
	}
	raw := strings.Join(body, "\n")
	if !strings.Contains(raw, h.m.cfg.Theme.Heading.Render("Files")) {
		t.Error("section heading not styled like Roadmap")
	}
	if strings.Count(sgr.ReplaceAllString(raw, ""), "Long") != 8 {
		t.Error("duplicate title")
	}
}

func TestWavesItemPanePageKeysUseVisibleHeight(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		h, l := wavesHarness(t)
		l.details = map[string]*slicercli.Detail{"A": {Slice: &slicercli.Spec{Title: "Alpha", Sections: []slicercli.Section{{Heading: "Files", Body: strings.Repeat("file.go\n", 100)}}}}}
		openWaves(t, h)
		h.deliver(press("enter"))
		h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		_ = h.m.View()
		h.deliver(press("pgdown"))
		if h.m.waves.paneOffset != size[1]-6 { // the hint line takes a row (S149)
			t.Errorf("page offset %d, want %d", h.m.waves.paneOffset, size[1]-6)
		}
		h.deliver(press("down"))
		h.deliver(press("pgup"))
		if h.m.waves.paneOffset != 1 {
			t.Errorf("page up offset %d, want 1", h.m.waves.paneOffset)
		}
	}
}

func TestWavesItemPanePreservesTheWholeSharedPath(t *testing.T) {
	h, l := wavesHarness(t)
	path := "internal/tui/path with spaces.go"
	l.suggest.Waves[0].Items[0].Reason = "shares: S65 " + path
	openWaves(t, h)
	h.deliver(press("enter"))
	body := sgr.ReplaceAllString(strings.Join(h.m.waveItemBody(72).body, "\n"), "")
	if !strings.Contains(body, "  Shares files with S65\n    "+path+"\n") {
		t.Fatalf("shared path not rendered as one indented path:\n%s", body)
	}
	if strings.Contains(body, "shares:") {
		t.Fatal("raw shares reason still visible")
	}
}

func TestWavesItemPaneGolden(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		for _, mono := range []bool{false, true} {
			theme := "color"
			if mono {
				theme = "mono"
			}
			name := fmt.Sprintf("waves-item-%dx%d-%s.txt", size[0], size[1], theme)
			t.Run(name, func(t *testing.T) {
				h, l := wavesHarness(t)
				h.m.cfg.Theme = NewTheme(mono)
				l.suggest.Waves[0].Items[0].Title = "Shared engine file"
				l.suggest.Waves[0].Items[0].Reason = "shares: S65 internal/engine/fixtures/a long shared path with spaces/round-details.go"
				l.details = map[string]*slicercli.Detail{"A": {Slice: &slicercli.Spec{Title: "Shared engine file", Size: "M", Lead: []string{"Keep the engine's shared file visible in the item detail."}, Sections: []slicercli.Section{{Heading: "Files", Body: "- `internal/engine/round.go`"}, {Heading: "Check", Body: "go test ./internal/engine"}}}}}
				openWaves(t, h)
				h.deliver(press("enter"))
				h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				checkGolden(t, name, h.m.View().Content)
			})
		}
	}
}

func TestWavesShowsFirstWavesOnly(t *testing.T) {
	h, l := wavesHarness(t)
	for i := 0; i < 5; i++ {
		l.suggest.Waves = append(l.suggest.Waves, l.suggest.Waves[0])
		l.suggest.Waves[len(l.suggest.Waves)-1].Wave = 10 + i
	}
	openWaves(t, h)
	body := sgr.ReplaceAllString(strings.Join(h.m.wavesBody(72).body, "\n"), "")
	if strings.Contains(body, "Wave 11") || !strings.Contains(body, "4 more waves") {
		t.Fatalf("wave limit missing:\n%s", body)
	}
	if len(h.m.waves.rows()) != 6 {
		t.Fatalf("hidden waves selectable: %d rows", len(h.m.waves.rows()))
	}
	h.deliver(press("A"))
	body = sgr.ReplaceAllString(strings.Join(h.m.wavesBody(72).body, "\n"), "")
	if !strings.Contains(body, "Wave 14") || len(h.m.waves.rows()) != 14 {
		t.Fatalf("show all failed:\n%s", body)
	}
}

func TestTickedItemStaysInView(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		h, l := wavesHarness(t)
		for i := 0; i < 40; i++ {
			l.suggest.Unplaced = append(l.suggest.Unplaced, slicercli.ParallelItem{ID: fmt.Sprintf("X%d", i), Title: "Extra"})
		}
		openWaves(t, h)
		h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for range 30 {
			h.deliver(press("down"))
		}
		id := h.m.waves.rows()[h.m.waves.cursor].ID
		h.deliver(press(" "))
		v := plainView(h.m)
		if !strings.Contains(v, "1 ticked, not launched") {
			t.Fatalf("tick status offscreen:\n%s", v)
		}
		if !strings.Contains(v, "[x] "+id) || strings.Contains(v, "Well specified?") || strings.Contains(v, "Split?") || strings.Contains(v, "launch inputs") {
			t.Fatalf("item not visible, or inputs shown:\n%s", v)
		}
	}
}

func TestWavesCompactLineupAndContinuation(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h)
	p := h.m.waves.picked["A"]
	p.Expected.Lineup["implementer"] = append(p.Expected.Lineup["implementer"], runrec.LineupEntry{Vendor: "hidden", Model: "fallback"}, runrec.LineupEntry{Vendor: "hidden", Model: "fallback2"})
	h.m.waves.picked["A"] = p
	body := h.m.wavesBody(72)
	raw := strings.Join(body.body, "\n")
	plain := sgr.ReplaceAllString(raw, "")
	for _, want := range []string{"impl:  claude sonnet high", "rev:   claude opus medium", "+2 fallbacks"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q:\n%s", want, plain)
		}
	}
	if !strings.Contains(plainView(h.m), "1 ticked, not launched") {
		t.Fatal("tick status missing")
	}
	if strings.Contains(plain, "hidden") || strings.Contains(plain, " -> ") {
		t.Error("fallback chain in table")
	}
	for _, line := range body.body {
		if strings.Contains(line, "rev:") || strings.Contains(line, "+2 fallbacks") {
			if !reverseSGR.MatchString(line) {
				t.Errorf("continuation lacks cursor: %q", line)
			}
		}
	}
}

// queuedRuns makes the project's next snapshot hold these runs, queued, in order.
func queuedRuns(h *harness, runs ...[2]string) {
	s := h.m.projects[0].snap
	s.EngineRuns = nil
	for _, r := range runs {
		s.EngineRuns = append(s.EngineRuns, source.EngineRun{ID: r[0], Item: r[1], State: "queued"})
	}
	h.src.set(s)
}

// launchedTwoRuns ticks A and B, launches both as ork-new and ork-other, and
// returns the harness after the reply and the snapshot that shows both runs
// (S262).
func launchedTwoRuns(t *testing.T) *harness {
	t.Helper()
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}, {Item: "B", Run: &runrec.Record{ID: "ork-other", Item: "B"}}}
	queuedRuns(h, [2]string{"ork-other", "B"}, [2]string{"ork-new", "A"})
	h.deliver(press("l"))
	h.deliver(press("y"))
	return h
}

// A launch the daemon accepts, with nothing held and no item failed, moves to the
// Running entry with the first launched run selected (S262, owner 2026-11-03).
func TestWaveLaunchJumpsToRunningWithTheFirstRunSelected(t *testing.T) {
	h := launchedTwoRuns(t)
	if h.m.active != runningEntry || h.m.view != 0 || h.m.waves != nil {
		t.Fatalf("active %d view %d waves %v, want the Running Overview with no composer", h.m.active, h.m.view, h.m.waves)
	}
	r, ok := h.m.selectedRow()
	if !ok || r.engineID != "ork-new" {
		t.Fatalf("the jump selected %q, want ork-new", r.engineID)
	}
	if want := "launched 2 items; enter on a running row opens its run"; h.m.actionStatus != want {
		t.Errorf("status %q, want %q", h.m.actionStatus, want)
	}
}

func TestWaveLaunchOfOneItemJumps(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h)
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}}
	queuedRuns(h, [2]string{"ork-other", "B"}, [2]string{"ork-new", "A"})
	h.deliver(press("l"))
	h.deliver(press("y"))
	if h.m.active != runningEntry || h.m.view != 0 {
		t.Fatalf("active %d view %d, want the Running Overview", h.m.active, h.m.view)
	}
	if r, ok := h.m.selectedRow(); !ok || r.engineID != "ork-new" {
		t.Fatalf("the jump selected %q, want ork-new", r.engineID)
	}
	if want := "launched A as ork-new; enter opens it"; h.m.actionStatus != want {
		t.Fatalf("launch notice %q, want %q", h.m.actionStatus, want)
	}
	h.deliver(press("enter"))
	if h.m.pane == nil || h.m.pane.run != "ork-new" {
		t.Fatalf("enter must open the pane of ork-new: %+v", h.m.pane)
	}
}

// The jump selects at once when a snapshot read before the reply already holds
// the run; it need not wait for the next one.
func TestWaveLaunchJumpSelectsARunThatIsAlreadyInTheSnapshot(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}, {Item: "B", Run: &runrec.Record{ID: "ork-other", Item: "B"}}}
	h.m.projects[0].snap.EngineRuns = []source.EngineRun{{ID: "ork-other", Item: "B", State: "queued"}, {ID: "ork-new", Item: "A", State: "queued"}}
	h.deliver(press("l"))
	reply := h.sendHeld(press("y"))
	if len(reply) != 1 {
		t.Fatalf("the launch made %d messages, want the reply", len(reply))
	}
	h.m.Update(reply[0]) // no snapshot is delivered after it
	if r, ok := h.m.selectedRow(); h.m.active != runningEntry || !ok || r.engineID != "ork-new" {
		t.Fatalf("active %d selected %q, want ork-new on Running", h.m.active, r.engineID)
	}
}

// A held item keeps the owner on the Waves screen: its questions are answered
// there, and leaving drops the composer with its ticks.
func TestWaveLaunchWithAHeldItemStaysOnWaves(t *testing.T) {
	h, l := wavesHarness(t)
	questionsForB(l)
	twoWaveItems(t, h)
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}}
	h.deliver(press("l"))
	h.deliver(press("s"))
	h.deliver(press("y"))
	if h.m.active != 0 || h.m.view != wavesView || h.m.waves == nil {
		t.Fatalf("active %d view %d: a held launch must stay on Waves of ork", h.m.active, h.m.view)
	}
	v := h.view()
	if !strings.Contains(v, "[>] A") || !strings.Contains(v, "running ork-new") || !strings.Contains(v, "[x] B") || !strings.Contains(v, "waits on 2 spec questions") {
		t.Errorf("rows after a held launch:\n%s", v)
	}
	if !strings.HasSuffix(h.m.actionStatus, "B waits on spec questions") {
		t.Errorf("status %q", h.m.actionStatus)
	}
	h.m.waves.cursor = 0
	h.deliver(press(" "))
	if _, ticked := h.m.waves.picked["A"]; ticked {
		t.Fatal("a launched item can be ticked again")
	}
	if hint := wideHint(h); !strings.Contains(hint, "enter open run") || strings.Contains(hint, "enter item") {
		t.Errorf("the launched row's hint: %q", hint)
	}
	h.m.waves.cursor = 1
	if hint := wideHint(h); !strings.Contains(hint, "enter item") {
		t.Errorf("the held row's hint: %q", hint)
	}
}

// An item the daemon refused keeps the owner on Waves, where the error can be
// read and the item ticked again.
func TestWaveLaunchWithAnItemErrorStaysOnWaves(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}, {Item: "B", Error: &ipc.Error{Code: "changed_plan", Message: "Exact B failure."}}}
	queuedRuns(h, [2]string{"ork-new", "A"})
	h.deliver(press("l"))
	h.deliver(press("y"))
	if h.m.active != 0 || h.m.view != wavesView {
		t.Fatalf("active %d view %d: an item error must stay on Waves", h.m.active, h.m.view)
	}
	if want := "launched A as ork-new; enter opens it; B: changed_plan: Exact B failure."; h.m.actionStatus != want {
		t.Errorf("status %q, want %q", h.m.actionStatus, want)
	}
}

// A failed call changes nothing: the view, the ticks and the status stay.
func TestWaveLaunchErrorStaysOnWaves(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	l.result = nil
	l.startErr = &ipc.Error{Code: "changed_plan", Message: "Exact START failure."}
	h.deliver(press("l"))
	h.deliver(press("y"))
	if h.m.active != 0 || h.m.view != wavesView || h.m.waves == nil || len(h.m.waves.picked) != 2 {
		t.Fatalf("active %d view %d: a failed launch must keep the composer and its ticks", h.m.active, h.m.view)
	}
	if want := "ork wave start ork A B: changed_plan: Exact START failure."; h.m.actionStatus != want {
		t.Errorf("status %q, want %q", h.m.actionStatus, want)
	}
}

// The owner left the Waves screen before the reply: the launch text shows, and
// nothing moves them again.
func TestWaveLaunchReplyAfterLeavingWavesDoesNotJump(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h)
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}}
	queuedRuns(h, [2]string{"ork-new", "A"})
	h.deliver(press("l"))
	reply := h.sendHeld(press("y"))
	h.deliver(press("tab")) // Roadmap
	if h.m.view != roadmapView {
		t.Fatalf("tab reached view %d, want the Roadmap", h.m.view)
	}
	for _, msg := range reply {
		h.deliver(msg)
	}
	if h.m.active != 0 || h.m.view != roadmapView {
		t.Errorf("active %d view %d: the reply moved the owner", h.m.active, h.m.view)
	}
	if want := "launched A as ork-new; enter opens it"; h.m.actionStatus != want {
		t.Errorf("status %q, want %q", h.m.actionStatus, want)
	}
}

// Running right after a wave launch: the new run selected, the launch on the
// status line (S262).
func TestRunningAfterLaunchGolden(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		name := fmt.Sprintf("running-launched-%dx%d-mono.txt", size[0], size[1])
		t.Run(name, func(t *testing.T) {
			h := launchedTwoRuns(t)
			h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			if got := len(strings.Split(h.m.View().Content, "\n")); got != size[1] {
				t.Fatalf("golden height %d, want %d", got, size[1])
			}
			checkGolden(t, name, h.m.View().Content)
		})
	}
}

func TestWavesReasonAndHintSpacing(t *testing.T) {
	h, l := wavesHarness(t)
	l.suggest.Unplaced[0].Title = strings.Repeat("Long title ", 10)
	openWaves(t, h)
	tickWave(t, h)
	raw := h.m.wavesBody(60).body
	plain := sgr.ReplaceAllString(strings.Join(raw, "\n"), "")
	if !strings.Contains(plain, "no files listed") || strings.Contains(plain, "no_files") {
		t.Fatalf("reason clipped or raw:\n%s", plain)
	}
	for i, line := range raw {
		if strings.Contains(line, "[enter] item") && (i == 0 || raw[i-1] != "") {
			t.Fatal("no blank before hints")
		}
	}
	h.m.waves.cursor = 3
	h.deliver(press("enter"))
	if !strings.Contains(plainView(h.m), "no files listed") {
		t.Fatal("pane reason missing")
	}
	h.deliver(press("esc"))
	h.deliver(press("?"))
	if !strings.Contains(plainView(h.m), "<S65") {
		t.Fatal("shares marker not explained")
	}
}

func TestWavesLineupWidthFitsLongestSummary(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h)
	body := h.m.wavesBody(120).body
	line := sgr.ReplaceAllString(body[bodyIndex(body, "[x] A")], "")
	if got := strings.Index(line, "impl:"); got != 120-len("gate:  claude opus medium") {
		t.Fatalf("lineup starts at %d: %q", got, line)
	}
}

func bodyIndex(lines []string, text string) int {
	for i, line := range lines {
		if strings.Contains(line, text) {
			return i
		}
	}
	return 0
}

func TestWavesLineupEditorKeepsFullFallbackChain(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h)
	p := h.m.waves.picked["A"]
	p.Expected.Lineup["implementer"] = append(p.Expected.Lineup["implementer"], runrec.LineupEntry{Vendor: "codex", Model: "fallback", Effort: "high"})
	h.m.waves.picked["A"] = p
	h.deliver(press("e"))
	if !strings.Contains(plainView(h.m), "codex fallback high") {
		t.Fatal("full fallback chain missing on e")
	}
}

func TestWavesJumpWaitsForSnapshot(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h)
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}}
	h.deliver(press("l"))
	h.deliver(press("y"))
	// The first snapshot after the reply does not hold the run yet.
	if h.m.active != runningEntry || h.m.jumpRun != "ork-new" || h.m.cursor != 0 {
		t.Fatalf("active %d jumpRun %q cursor %d: the jump waits for the run", h.m.active, h.m.jumpRun, h.m.cursor)
	}
	snap := h.m.projects[0].snap
	snap.EngineRuns = []source.EngineRun{{ID: "other", Item: "B", State: "queued"}, {ID: "ork-new", Item: "A", State: "queued"}}
	h.deliver(snapshotMsg{name: "ork", snap: snap})
	r, ok := h.m.selectedRow()
	if !ok || r.engineID != "ork-new" || h.m.jumpRun != "" {
		t.Fatalf("delayed jump selected %q", r.engineID)
	}
}

func TestWavesComposerGolden(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		for _, state := range []string{"ticked", "limited", "fallbacks", "launched"} {
			for _, mono := range []bool{false, true} {
				theme := "color"
				if mono {
					theme = "mono"
				}
				name := fmt.Sprintf("waves-%s-%dx%d-%s.txt", state, size[0], size[1], theme)
				t.Run(name, func(t *testing.T) {
					h, l := wavesHarness(t)
					h.m.cfg.Theme = NewTheme(mono)
					h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					l.suggest.Waves[1].Items[0].Reason = "shares: A internal/engine/shared.go"
					if state == "limited" {
						for i := 0; i < 5; i++ {
							l.suggest.Waves = append(l.suggest.Waves, l.suggest.Waves[1])
							l.suggest.Waves[len(l.suggest.Waves)-1].Wave = 5 + i
						}
					}
					openWaves(t, h)
					switch state {
					case "ticked":
						// The tick plans at once: the lineup column is filled, no inputs are asked.
						tickWave(t, h)
					case "fallbacks":
						tickWave(t, h)
						p := h.m.waves.picked["A"]
						p.Expected.Lineup["implementer"] = append(p.Expected.Lineup["implementer"], runrec.LineupEntry{Vendor: "claude", Model: "haiku", Effort: "high"}, runrec.LineupEntry{Vendor: "claude", Model: "sonnet", Effort: "medium"})
						h.m.waves.picked["A"] = p
					case "launched":
						// B has spec questions and is skipped, so only A launches and the
						// screen stays on Waves (S262).
						questionsForB(l)
						tickWave(t, h)
						h.deliver(press("down"))
						tickWave(t, h)
						h.deliver(press("up")) // the cursor on the launched row, as before
						l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-20261025-123000-abcd", Item: "A"}}}
						h.deliver(press("l"))
						h.deliver(press("s"))
						h.deliver(press("y"))
					}
					h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					if got := len(strings.Split(h.m.View().Content, "\n")); got != size[1] {
						t.Fatalf("golden height %d, want %d", got, size[1])
					}
					checkGolden(t, name, h.m.View().Content)
				})
			}
		}
	}
}

func TestWavesExpandPreservesUnplacedCursor(t *testing.T) {
	h, l := wavesHarness(t)
	for range 3 {
		l.suggest.Waves = append(l.suggest.Waves, l.suggest.Waves[1])
	}
	openWaves(t, h)
	h.m.waves.cursor = len(h.m.waves.rows()) - 1
	h.deliver(press("A"))
	if got := h.m.waves.rows()[h.m.waves.cursor].ID; got != "D" {
		t.Fatalf("expansion moved cursor to %s", got)
	}
}

func TestWavesDelayedJumpYieldsToNavigation(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h)
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}}
	h.deliver(press("l"))
	h.deliver(press("y"))
	h.deliver(press("down"))
	snap := h.m.projects[0].snap
	snap.EngineRuns = []source.EngineRun{{ID: "other", Item: "B", State: "queued"}, {ID: "ork-new", Item: "A", State: "queued"}}
	h.deliver(snapshotMsg{name: "ork", snap: snap})
	if h.m.jumpRun != "" || h.m.cursor != 0 {
		t.Fatal("delayed jump stole cursor after navigation")
	}
}

// dimAt checks the effective style at text, across all cell reset sequences.
func dimAt(t *testing.T, line, text string, mono bool) {
	t.Helper()
	at := strings.Index(line, text)
	if at < 0 {
		t.Fatalf("missing %q in %q", text, line)
	}
	dim := false
	for _, s := range sgr.FindAllString(line[:at], -1) {
		for _, code := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(s, "\x1b["), "m"), ";") {
			if code == "" || code == "0" {
				dim = false
			}
			if mono {
				if code == "2" {
					dim = true
				}
				if code == "22" {
					dim = false
				}
			} else {
				if code == "90" {
					dim = true
				}
				if code == "39" || code == "97" {
					dim = false
				}
			}
		}
	}
	if !dim {
		t.Errorf("%q is not dim: %q", text, line)
	}
}

func TestWavesFix1DimCellsAndAlignment(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h, l := wavesHarness(t)
		h.m.cfg.Theme = NewTheme(mono)
		openWaves(t, h)
		tickWave(t, h)
		p := h.m.waves.picked["A"]
		p.Lineup = map[string][]runrec.LineupEntry{"implementer": p.Expected.Lineup["implementer"]}
		p.Expected.Lineup["implementer"] = append(p.Expected.Lineup["implementer"], runrec.LineupEntry{Vendor: "extra"})
		h.m.waves.picked["A"] = p
		body := h.m.wavesBody(92).body
		valueCol := -1
		for _, raw := range body {
			plain := sgr.ReplaceAllString(raw, "")
			if strings.Contains(plain, "claude") {
				col := strings.Index(plain, "claude")
				if valueCol < 0 {
					valueCol = col
				}
				if col != valueCol {
					t.Errorf("values not aligned: %q", plain)
				}
			}
			if strings.Contains(plain, "+1 fallbacks") {
				if strings.Index(plain, "+1 fallbacks") != valueCol {
					t.Errorf("fallback not aligned: %q", plain)
				}
				dimAt(t, raw, "+1 fallbacks", mono)
			}
		}
		// A selected and an unselected launched row both retain dim text after cell resets.
		l.suggest.Waves[0].Items[0].Reason = "shares: S65 shared.go"
		h.m.waves.picked = map[string]daemon.RunParams{}
		h.m.waves.launched = map[string]string{"A": "ork-new", "B": "ork-other"}
		for _, raw := range h.m.wavesBody(92).body {
			if strings.Contains(raw, "Alpha") {
				dimAt(t, raw, "Alpha", mono)
				dimAt(t, raw, "<S65", mono)
			}
			if strings.Contains(raw, "Beta") {
				dimAt(t, raw, "Beta", mono)
			}
		}
	}
}

func TestWavesFix1SecondaryLines(t *testing.T) {
	h, l := wavesHarness(t)
	h.m.cfg.Theme = NewTheme(false)
	openWaves(t, h)
	tickWave(t, h)
	l.suggest.Unplaced[0].Title = strings.Repeat("long title ", 10)
	h.m.waves.launched = map[string]string{"B": "ork-" + strings.Repeat("r", 36)}
	for _, raw := range h.m.wavesBody(60).body {
		plain := sgr.ReplaceAllString(raw, "")
		for _, text := range []string{"no files listed", "running ork-"} {
			if strings.Contains(plain, text) {
				if !strings.HasPrefix(plain, strings.Repeat(" ", 9)+text) {
					t.Errorf("secondary line not at title column: %q", plain)
				}
				dimAt(t, raw, text, false)
			}
		}
	}
	h.m.waves.launched = nil
	// Ticking again shows no input rows and no input hints (adj 189).
	h.deliver(press(" "))
	h.deliver(press(" "))
	plain := sgr.ReplaceAllString(strings.Join(h.m.wavesBody(60).body, "\n"), "")
	if strings.Contains(plain, "launch inputs") || strings.Contains(plain, "[y/n]:") {
		t.Errorf("input rows still drawn:\n%s", plain)
	}
	for _, hint := range []string{"tab next field", "enter plan", "esc cancel"} {
		if strings.Contains(wideHint(h), hint) {
			t.Errorf("input hint %q still listed", hint)
		}
	}
	if strings.Contains(plain, "[space] tick") || strings.Contains(plain, "[l] launch") {
		t.Error("composer hints shown in the body")
	}
}

func TestWavesFix1StatusNotInLineupEditor(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h)
	if !strings.Contains(plainView(h.m), "1 ticked, not launched") {
		t.Fatal("tick status missing")
	}
	h.deliver(press("e"))
	if strings.Contains(plainView(h.m), "ticked, not launched") {
		t.Fatal("tick status in lineup editor")
	}
}

func TestWavesFix1NoticesHintsAndHelp(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	// The help text of the Waves view names the A key and the shares marker.
	help := sgr.ReplaceAllString(strings.Join(h.m.helpLines(100, 40), "\n"), "")
	if !strings.Contains(help, "Waves: A shows all waves. A again shows first 3. <S65: shares files with S65, an earlier item.") {
		t.Fatal(help)
	}
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new"}}, {Item: "B", Run: &runrec.Record{ID: "ork-other"}}}
	h.deliver(press("l"))
	h.deliver(press("y"))
	// Both launched, so the screen moved to Running; the status stays.
	if h.m.actionStatus != "launched 2 items; enter on a running row opens its run" {
		t.Fatal(h.m.actionStatus)
	}
}

func TestWavesFix1OneMoreWave(t *testing.T) {
	h, l := wavesHarness(t)
	for range 2 {
		l.suggest.Waves = append(l.suggest.Waves, l.suggest.Waves[1])
	}
	openWaves(t, h)
	body := sgr.ReplaceAllString(strings.Join(h.m.wavesBody(72).body, "\n"), "")
	if !strings.Contains(body, "1 more wave;") || strings.Contains(body, "1 more waves") {
		t.Fatal(body)
	}
}

func TestWavesHintsStayPinnedBelowBody(t *testing.T) {
	h, l := wavesHarness(t)
	for i := 0; i < 40; i++ {
		l.suggest.Unplaced = append(l.suggest.Unplaced, slicercli.ParallelItem{ID: fmt.Sprintf("X%d", i), Title: "Extra"})
	}
	openWaves(t, h)
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for range 30 {
		h.deliver(press("down"))
	}
	h.deliver(press(" "))
	lines := strings.Split(plainView(h.m), "\n")
	at := func(text string) int {
		for i, line := range lines {
			if strings.Contains(line, text) {
				return i
			}
		}
		return -1
	}
	// With an item ticked at 80x24 `enter item` has gone from the line (adj 270).
	if at("space tick;") != len(lines)-2 || at("[x] ") < 0 || at("[x] ") > at("space tick;") || at("next field") >= 0 {
		t.Fatalf("composer hints not pinned under the body:\n%s", strings.Join(lines, "\n"))
	}
	if got := plainView(h.m); !strings.Contains(hintLine(got), "space tick;") || !strings.Contains(hintLine(got), "l launch") {
		t.Fatalf("composer hints scrolled away:\n%s", got)
	}
}

func TestWavesLaunchedRowMarker(t *testing.T) {
	h, _ := wavesHarness(t)
	h.m.cfg.Theme = NewTheme(false)
	openWaves(t, h)
	h.m.waves.launched = map[string]string{"A": "ork-new"}
	h.m.waves.cursor = 1
	for _, raw := range h.m.wavesBody(80).body {
		plain := sgr.ReplaceAllString(raw, "")
		if strings.Contains(plain, " A ") {
			if !strings.Contains(plain, "[>] A") {
				t.Errorf("launched marker missing: %q", plain)
			}
			dimAt(t, raw, "[>]", false)
		}
		if strings.Contains(plain, " B ") && !strings.Contains(plain, "[ ] B") {
			t.Errorf("plain row changed: %q", plain)
		}
	}
}

func TestWavesArchitectLineup(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h, _ := wavesHarness(t)
		h.m.cfg.Theme = NewTheme(mono)
		openWaves(t, h)
		tickWave(t, h)
		p := h.m.waves.picked["A"]
		if strings.Contains(strings.Join(waveLineup(p), "\n"), "arch:") {
			t.Fatal("absent architect shown")
		}
		p.Expected.Lineup["architect"] = []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}}
		if !strings.Contains(h.view(), "arch:  codex sol high") {
			t.Fatal(h.view())
		}
		theme := "color"
		if mono {
			theme = "mono"
		}
		checkGolden(t, "waves-architect-100x30-"+theme+".txt", h.m.View().Content)
	}
}

// The Waves lineup column shows the UI pass row when the plan has one (S136).
func TestWavesLineupShowsUIPass(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h)
	p := h.m.waves.picked["A"]
	if strings.Contains(strings.Join(waveLineup(p), "\n"), "ui:") {
		t.Fatal("absent UI pass shown")
	}
	p.Expected.Lineup["ui"] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}, {Vendor: "codex", Model: "sol", Effort: "high"}}
	lines := waveLineup(p)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "ui:    claude opus medium") || !strings.Contains(joined, "+1 fallbacks") {
		t.Fatalf("lineup lines: %q", lines)
	}
	if !strings.Contains(h.view(), "ui:    claude opus medium") {
		t.Fatal(h.view())
	}
}

func TestWavesLaunchShowsTheBlockedRefusal(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	const text = `B waits on A "Alpha" (in this wave, not done); untick B or land A first`
	l.check = func([]string) (*daemon.SpecCheckResult, error) {
		return nil, &ipc.Error{Code: daemon.CodeRefused, Message: text}
	}
	h.deliver(press("l"))
	if h.m.actionStatus != text || !strings.Contains(strings.Join(strings.Fields(strings.ReplaceAll(h.view(), "│", " ")), " "), text) || h.m.view != wavesView || len(l.starts) != 0 || l.writes != 0 || len(h.m.waves.picked) != 2 || h.m.confirm != nil {
		t.Fatalf("status %q starts %+v picked %+v\n%s", h.m.actionStatus, l.starts, h.m.waves.picked, h.view())
	}
}

// The refusal wraps on the status lines at 80x24 with no word cut and no line
// over the width (S190, S186 note). Whitespace is not squashed: the lines of
// the status block, trimmed and joined by one space, are the refusal.
func TestBlockedWaveRefusalWrapsAt80Columns(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	const text = `B waits on A "Alpha" (in this wave, not done); untick B or land A first`
	l.check = func([]string) (*daemon.SpecCheckResult, error) {
		return nil, &ipc.Error{Code: daemon.CodeRefused, Message: text}
	}
	h.deliver(press("l"))
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	lines := strings.Split(plainView(h.m), "\n")
	var rules []int
	for i, line := range lines {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("line %d is %d columns wide: %q", i, w, line)
		}
		if isRule(line) {
			rules = append(rules, i)
		}
	}
	if len(rules) < 2 {
		t.Fatalf("no status block:\n%s", strings.Join(lines, "\n"))
	}
	// The status block sits between the last two rules, above the hint line.
	var status []string
	for _, line := range lines[rules[len(rules)-2]+1 : rules[len(rules)-1]] {
		_, rest, _ := strings.Cut(line, "│")
		status = append(status, strings.TrimSpace(rest))
	}
	if len(status) < 2 || strings.Join(status, " ") != text {
		t.Fatalf("the refusal is not wrapped whole on the status lines %q:\n%s", status, strings.Join(lines, "\n"))
	}
}
func TestWavesStartRefusalKeepsTheTicks(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	const text = `B waits on A "Alpha" (in this wave, not done); untick B or land A first`
	l.result = nil
	l.startErr = &ipc.Error{Code: daemon.CodeRefused, Message: text}
	h.deliver(press("l"))
	h.deliver(press("y"))
	if !strings.Contains(h.m.actionStatus, text) || !strings.Contains(strings.Join(strings.Fields(strings.ReplaceAll(h.view(), "│", " ")), " "), text) || h.m.view != wavesView || len(l.starts) != 1 || len(h.m.waves.picked) != 2 || h.m.waves.starting || len(h.m.waves.launched) != 0 {
		t.Fatalf("status %q picked %+v\n%s", h.m.actionStatus, h.m.waves.picked, h.view())
	}
}

func TestWavesConflictMarkerHangIndents(t *testing.T) {
	h, l := wavesHarness(t)
	l.details = map[string]*slicercli.Detail{"A": {Slice: &slicercli.Spec{Sections: []slicercli.Section{{Heading: "Files", Body: "- `internal/engine/a.go`"}}}}, "B": {Slice: &slicercli.Spec{Sections: []slicercli.Section{{Heading: "Files", Body: "- `cmd/ork/main.go`"}}}}}
	twoWaveItems(t, h)
	h.m.waves.overlap = nil
	lines := h.m.wavesBody(38).body
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(stripANSI(line), "  ! engine conflict") {
			start = i
			break
		}
	}
	if start < 0 || start+1 >= len(lines) {
		t.Fatalf("no wrapped marker: %v", lines)
	}
	for _, line := range lines[start+1:] {
		if !strings.HasPrefix(stripANSI(line), "    ") {
			t.Errorf("continuation must hang-indent: %q", stripANSI(line))
		}
	}
}

func TestWavesAKeyTogglesBackToThreeWaves(t *testing.T) {
	for _, unplaced := range []bool{false, true} {
		for _, selected := range []string{"S2", "S3b", "S4", "S5", "D", "E"} {
			if !unplaced && (selected == "D" || selected == "E") {
				continue
			}
			t.Run(fmt.Sprintf("unplaced-%t/%s", unplaced, selected), func(t *testing.T) {
				h, l := wavesHarness(t)
				l.suggest.Waves = nil
				l.suggest.Unplaced = nil
				if unplaced {
					l.suggest.Unplaced = []slicercli.ParallelItem{{ID: "D", Title: "Unplaced D"}, {ID: "E", Title: "Unplaced E"}}
				}
				for i := 1; i <= 5; i++ {
					l.suggest.Waves = append(l.suggest.Waves, struct {
						Wave  int                      `json:"wave"`
						Items []slicercli.ParallelItem `json:"items"`
					}{Wave: i, Items: []slicercli.ParallelItem{{ID: fmt.Sprintf("S%d", i), Title: "Feature"}}})
				}
				l.suggest.Waves[2].Items = append(l.suggest.Waves[2].Items, slicercli.ParallelItem{ID: "S3b", Title: "Last visible wave item"})
				openWaves(t, h)
				h.deliver(press("A"))
				extra := 0
				if unplaced {
					extra = 2
				}
				if len(h.m.waves.rows()) != 6+extra {
					t.Fatal("expand failed")
				}
				if !strings.Contains(strings.Join(h.m.wavesBody(80).body, "\n"), "[A] show first 3") {
					t.Error("missing collapse hint")
				}
				found := false
				for i, row := range h.m.waves.rows() {
					if row.ID == selected {
						h.m.waves.cursor = i
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("missing selected fixture item %s", selected)
				}
				h.deliver(press("A"))
				rows := h.m.waves.rows()
				if len(rows) != 4+extra || h.m.waves.showAll {
					t.Fatalf("collapse failed: %d rows", len(rows))
				}
				want := selected
				if selected == "S4" || selected == "S5" {
					want = "S3b"
				}
				if h.m.waves.cursor < 0 || h.m.waves.cursor >= len(rows) || rows[h.m.waves.cursor].ID != want {
					t.Fatalf("cursor=%d, want %s", h.m.waves.cursor, want)
				}
				if !strings.Contains(strings.Join(h.m.wavesBody(80).body, "\n"), "2 more waves; [A] show all") {
					t.Error("missing expand hint")
				}
			})
		}
	}
}

// Saved waves on the Waves screen (S216, *proposed*).

func typeText(h *harness, s string) {
	for _, r := range s {
		h.deliver(press(string(r)))
	}
}

// tickBThenA ticks B and then A, so that table order and tick order differ.
func tickBThenA(t *testing.T, h *harness) {
	t.Helper()
	openWaves(t, h)
	h.deliver(press("down"))
	tickWave(t, h)
	h.deliver(press("up"))
	tickWave(t, h)
}

func wm(id, status, title string) daemon.WaveMember {
	return daemon.WaveMember{Item: id, Status: status, Title: title}
}

func savedWave(name string, members ...daemon.WaveMember) daemon.SavedWaveView {
	return daemon.SavedWaveView{Project: "ork", Name: name, Items: members}
}

// planWith replaces the suggestion with the harness plan plus the given JSON
// members of blocked, held and unspecified.
func planWith(t *testing.T, l *wavesLive, extra string) {
	t.Helper()
	var plan slicercli.ParallelPlan
	body := `{"waves":[{"wave":2,"items":[{"id":"A","title":"Alpha"},{"id":"B","title":"Beta"}]},{"wave":4,"items":[{"id":"C","title":"Gamma"}]}],"unplaced":[{"id":"D","title":"Delta","reason":"no_files"}]` + extra + `}`
	if err := json.Unmarshal([]byte(body), &plan); err != nil {
		t.Fatal(err)
	}
	l.suggest = &plan
}

// hintOf is the hint line at the given width; 80 columns is the 80x24 terminal.
func hintOf(h *harness, width int) string {
	height := 30
	if width == 80 {
		height = 24
	}
	return hintLine(h.at(width, height))
}

func TestWavesSaveNeedsATickedItem(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	h.deliver(press("w"))
	if want := "Tick at least one item to save a wave"; h.m.actionStatus != want {
		t.Fatalf("status %q, want %q", h.m.actionStatus, want)
	}
	if h.m.waves.naming != nil || len(l.defines) != 0 {
		t.Fatalf("the save key opened an input or called the daemon: %+v %v", h.m.waves.naming, l.defines)
	}
}

func TestWavesSaveAsksForAName(t *testing.T) {
	h, l := wavesHarness(t)
	tickBThenA(t, h)
	h.deliver(press("w"))
	if v := h.view(); !strings.Contains(v, "Save wave A, B as:") {
		t.Fatalf("no name input:\n%s", v)
	}
	typeText(h, "pair-a")
	h.deliver(press("esc"))
	if h.m.waves.naming != nil || len(l.defines) != 0 || strings.Contains(h.view(), "Save wave") {
		t.Fatalf("esc did not cancel without a call: %v", l.defines)
	}
	if len(h.m.waves.picked) != 2 {
		t.Fatal("esc dropped the ticks")
	}
	h.deliver(press("w"))
	typeText(h, "pair-a")
	h.deliver(press("enter"))
	want := []daemon.WaveDefineParams{{Project: "ork", Name: "pair-a", Items: []string{"A", "B"}}}
	if !reflect.DeepEqual(l.defines, want) {
		t.Fatalf("define calls %+v, want %+v", l.defines, want)
	}
	if h.m.waves.naming != nil {
		t.Fatal("the input stayed open after the save")
	}
	if got, want := h.m.actionStatus, "saved pair-a: A, B"; got != want {
		t.Fatalf("status %q, want %q", got, want)
	}
	if len(h.m.waves.picked) != 2 {
		t.Fatal("the save dropped the ticks")
	}
}

func TestWavesSaveShowsTheDaemonsRefusal(t *testing.T) {
	h, l := wavesHarness(t)
	l.define = func(daemon.WaveDefineParams) (*daemon.SavedWave, error) {
		return nil, &ipc.Error{Code: "bad_params", Message: "wave name must be 1 to 32 characters"}
	}
	tickBThenA(t, h)
	h.deliver(press("w"))
	typeText(h, "Bad Name")
	h.deliver(press("enter"))
	if want := "bad_params: wave name must be 1 to 32 characters"; h.m.actionStatus != want {
		t.Fatalf("status %q, want %q", h.m.actionStatus, want)
	}
	n := h.m.waves.naming
	if n == nil || n.pending || n.field.Value() != "Bad Name" {
		t.Fatalf("the input did not stay open with the name: %+v", n)
	}
	if v := h.view(); !strings.Contains(v, "bad_params: wave name must be 1 to 32 characters") || !strings.Contains(v, "Save wave A, B as:") {
		t.Fatalf("refusal or input not on screen:\n%s", v)
	}
	h.deliver(press("enter"))
	if len(l.defines) != 2 {
		t.Fatalf("the input did not take a second try: %d calls", len(l.defines))
	}
}

// NW-Q3 (owner 2026-11-07): the daemon refuses a duplicate name unless replace
// is set, and the screen asks before it sets it.
func TestWavesSaveAsksBeforeReplacingWhenNWQ3AllowsReplace(t *testing.T) {
	h, l := wavesHarness(t)
	l.define = func(p daemon.WaveDefineParams) (*daemon.SavedWave, error) {
		if !p.Replace {
			return nil, &ipc.Error{Code: daemon.CodeDuplicateName, Message: "a saved wave \"pair-a\" already exists"}
		}
		return &daemon.SavedWave{Project: p.Project, Name: p.Name, Items: p.Items}, nil
	}
	tickBThenA(t, h)
	h.deliver(press("w"))
	typeText(h, "pair-a")
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "Replace pair-a? [y/N] (Enter is No)") {
		t.Fatalf("no replace question:\n%s", v)
	}
	if len(l.defines) != 1 || l.defines[0].Replace {
		t.Fatalf("calls %+v, want one without replace", l.defines)
	}
	h.deliver(press("enter")) // Enter is No: back to the name, nothing sent
	if len(l.defines) != 1 || h.m.waves.naming == nil || h.m.waves.naming.field.Value() != "pair-a" || h.m.waves.naming.replace {
		t.Fatalf("No did not return to the name input: %+v %v", h.m.waves.naming, l.defines)
	}
	h.deliver(press("enter"))
	h.deliver(press("y"))
	if len(l.defines) != 3 || !l.defines[2].Replace || l.defines[2].Name != "pair-a" {
		t.Fatalf("y did not replace: %+v", l.defines)
	}
	if got, want := h.m.actionStatus, "saved pair-a: A, B"; got != want || h.m.waves.naming != nil {
		t.Fatalf("status %q, want %q", got, want)
	}
}

func TestWavesSavedListShowsNamesMembersAndStatus(t *testing.T) {
	h, l := wavesHarness(t)
	l.saved = []daemon.SavedWaveView{
		savedWave("pair-a", wm("A", "open", "Alpha"), wm("B", "done", "Beta"), wm("X", "missing", "")),
		savedWave("solo", wm("C", "open", "Gamma")),
	}
	openWaves(t, h)
	h.deliver(press("L"))
	if !reflect.DeepEqual(l.lists, []string{"ork"}) {
		t.Fatalf("list calls %v, want [ork]", l.lists)
	}
	v := h.view()
	for _, words := range [][]string{{"pair-a", "3 items"}, {"A", "open", "Alpha"}, {"B", "done", "Beta"}, {"X", "missing"}, {"solo", "1 item"}, {"C", "open", "Gamma"}} {
		if rowsWith(v, words...) != 1 {
			t.Errorf("want one line with %v:\n%s", words, v)
		}
	}
	if strings.Contains(v, "Wave 2") {
		t.Fatalf("the table still shows:\n%s", v)
	}
	h.deliver(press("esc"))
	if v := h.view(); !strings.Contains(v, "Wave 2") || strings.Contains(v, "pair-a") {
		t.Fatalf("esc did not return to the table:\n%s", v)
	}
}

func TestWavesSavedListShowsEmptyAndErrors(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	h.deliver(press("L"))
	if v := h.view(); !strings.Contains(v, "No saved waves for ork") {
		t.Fatalf("no empty text:\n%s", v)
	}
	h.deliver(press("esc"))
	l.listErr = &ipc.Error{Code: "locked", Message: "Exact LIST failure."}
	h.deliver(press("L"))
	if v := h.view(); !strings.Contains(v, "locked: Exact LIST failure.") {
		t.Fatalf("no list error:\n%s", v)
	}
	h.deliver(press("enter"))
	if len(h.m.waves.picked) != 0 {
		t.Fatal("enter on a failed list loaded something")
	}
}

func TestWavesLoadTicksTheStartableMembersPresentInTheTable(t *testing.T) {
	h, l := wavesHarness(t)
	l.saved = []daemon.SavedWaveView{savedWave("pair-a", wm("D", "open", "Delta"), wm("C", "open", "Gamma"), wm("B", "done", "Beta"), wm("A", "open", "Alpha"))}
	openWaves(t, h)
	h.deliver(press("L"))
	h.deliver(press("enter"))
	if h.m.waves.list != nil {
		t.Fatal("the list stayed open after the load")
	}
	var planned []string
	for _, p := range l.plans {
		planned = append(planned, p.Item)
	}
	if !reflect.DeepEqual(planned, []string{"A", "C", "D"}) {
		t.Fatalf("plans %v, want A, C, D in table order", planned)
	}
	for _, id := range []string{"A", "C", "D"} {
		if p, ok := h.m.waves.picked[id]; !ok || p.Expected == nil || !p.WellSpecified {
			t.Errorf("%s: %+v", id, p)
		}
	}
	if _, ok := h.m.waves.picked["B"]; ok {
		t.Fatal("the done member was ticked")
	}
	if !reflect.DeepEqual(l.overlaps, [][]string{{"ork", "A", "C", "D"}}) {
		t.Fatalf("overlap calls %v", l.overlaps)
	}
	if got, want := h.m.actionStatus, "loaded pair-a: ticked A, C, D; skipped B (done)"; got != want {
		t.Fatalf("status %q, want %q", got, want)
	}
	v := h.view()
	for _, s := range []string{"[x] A", "[x] C", "[x] D", "[ ] B"} {
		if !strings.Contains(v, s) {
			t.Errorf("missing %q:\n%s", s, v)
		}
	}
	if strings.Contains(v, "Well specified?") || strings.Contains(v, "Split?") {
		t.Fatalf("a load must not ask inputs:\n%s", v)
	}
	if l.writes != 0 {
		t.Fatal("a load wrote")
	}
}

func TestWavesSavedListEnterLoadsTheWaveUnderTheCursor(t *testing.T) {
	h, l := wavesHarness(t)
	l.saved = []daemon.SavedWaveView{savedWave("pair-a", wm("A", "open", "Alpha")), savedWave("solo", wm("C", "open", "Gamma")), savedWave("last", wm("D", "open", "Delta"))}
	openWaves(t, h)
	h.deliver(press("L"))
	h.deliver(press("down"))
	h.deliver(press("down"))
	h.deliver(press("up"))
	h.deliver(press("enter"))
	if got, want := h.m.actionStatus, "loaded solo: ticked C"; got != want {
		t.Fatalf("status %q, want %q", got, want)
	}
	if _, ok := h.m.waves.picked["C"]; !ok || len(h.m.waves.picked) != 1 {
		t.Fatalf("picked %v, want only C", h.m.waves.picked)
	}
}

func TestWavesLoadNamesMembersNotInTheSuggestion(t *testing.T) {
	h, l := wavesHarness(t)
	planWith(t, l, `,"blocked":[{"id":"E","waiting_on":["C"]}],"held":[{"id":"F","title":"Phi","flags":["needs-owner"]}],"unspecified":[{"id":"G","missing":["files"]}]`)
	l.saved = []daemon.SavedWaveView{savedWave("mixed", wm("A", "open", "Alpha"), wm("E", "open", "Epsilon"), wm("F", "open", "Phi"), wm("G", "open", "Gee"), wm("H", "open", "Aitch"))}
	openWaves(t, h)
	h.deliver(press("L"))
	h.deliver(press("enter"))
	if len(h.m.waves.picked) != 1 || h.m.waves.picked["A"].Item != "A" {
		t.Fatalf("picked %v, want only A", h.m.waves.picked)
	}
	want := "loaded mixed: ticked A; not ticked: E (blocked, waiting on C), F (held: needs-owner), G (unspecified: missing files), H (not in slicer's suggestion)"
	if h.m.actionStatus != want {
		t.Fatalf("status\n got %q\nwant %q", h.m.actionStatus, want)
	}
}

func TestWavesLoadShowsAMemberInAHiddenWave(t *testing.T) {
	h, l := wavesHarness(t)
	for i := 0; i < 5; i++ {
		l.suggest.Waves = append(l.suggest.Waves, l.suggest.Waves[0])
		l.suggest.Waves[len(l.suggest.Waves)-1].Items = []slicercli.ParallelItem{{ID: fmt.Sprintf("W%d", i), Title: "Late"}}
	}
	l.saved = []daemon.SavedWaveView{savedWave("late", wm("W4", "open", "Late"))}
	openWaves(t, h)
	if h.m.waves.showAll {
		t.Fatal("fixture shows all waves already")
	}
	h.deliver(press("L"))
	h.deliver(press("enter"))
	if _, ok := h.m.waves.picked["W4"]; !ok || !h.m.waves.showAll {
		t.Fatalf("W4 in a hidden wave was not ticked: %v showAll %v", h.m.waves.picked, h.m.waves.showAll)
	}
}

func TestWavesLaunchAfterLoadSendsTheWaveName(t *testing.T) {
	load := func(t *testing.T, saved daemon.SavedWaveView) (*harness, *wavesLive) {
		h, l := wavesHarness(t)
		planWith(t, l, `,"blocked":[{"id":"E","waiting_on":["C"]}]`)
		l.saved = []daemon.SavedWaveView{saved}
		openWaves(t, h)
		h.deliver(press("L"))
		h.deliver(press("enter"))
		return h, l
	}
	t.Run("the startable set", func(t *testing.T) {
		h, l := load(t, savedWave("pair-a", wm("A", "open", "Alpha"), wm("B", "open", "Beta"), wm("C", "done", "Gamma")))
		h.deliver(press("l"))
		if !strings.Contains(h.view(), "ork wave start ork A B? [y/N] (Enter is No)") {
			t.Fatal(h.view())
		}
		h.deliver(press("y"))
		if len(l.starts) != 1 || l.starts[0].Name != "pair-a" || len(l.starts[0].Items) != 2 {
			t.Fatalf("starts %+v, want one named pair-a", l.starts)
		}
	})
	t.Run("one unticked", func(t *testing.T) {
		h, l := load(t, savedWave("pair-a", wm("A", "open", "Alpha"), wm("B", "open", "Beta")))
		h.m.waves.cursor = 0
		h.deliver(press(" ")) // untick A
		h.deliver(press("l"))
		h.deliver(press("y"))
		if len(l.starts) != 1 || l.starts[0].Name != "" || len(l.starts[0].Items) != 1 {
			t.Fatalf("starts %+v, want B alone with no name", l.starts)
		}
	})
	t.Run("a member the table lacks", func(t *testing.T) {
		h, l := load(t, savedWave("pair-a", wm("A", "open", "Alpha"), wm("E", "open", "Epsilon")))
		h.deliver(press("l"))
		h.deliver(press("y"))
		if len(l.starts) != 1 || l.starts[0].Name != "" || len(l.starts[0].Items) != 1 {
			t.Fatalf("starts %+v, want A alone with no name", l.starts)
		}
	})
	t.Run("an item added after the load", func(t *testing.T) {
		h, l := load(t, savedWave("pair-a", wm("A", "open", "Alpha")))
		h.deliver(press("down"))
		tickWave(t, h)
		h.deliver(press("l"))
		h.deliver(press("y"))
		if len(l.starts) != 1 || l.starts[0].Name != "" || len(l.starts[0].Items) != 2 {
			t.Fatalf("starts %+v, want A and B with no name", l.starts)
		}
	})
	t.Run("a hand-ticked set", func(t *testing.T) {
		h, l := wavesHarness(t)
		twoWaveItems(t, h)
		h.deliver(press("l"))
		h.deliver(press("y"))
		if len(l.starts) != 1 || l.starts[0].Name != "" {
			t.Fatalf("starts %+v, want no name", l.starts)
		}
	})
}

func TestWavesLoadNeverLaunches(t *testing.T) {
	h, l := wavesHarness(t)
	l.saved = []daemon.SavedWaveView{savedWave("pair-a", wm("A", "open", "Alpha"), wm("B", "open", "Beta"))}
	openWaves(t, h)
	h.deliver(press("L"))
	h.deliver(press("enter"))
	if len(l.starts) != 0 || len(l.checks) != 0 || h.m.confirm != nil {
		t.Fatalf("a load launched or checked: starts %v checks %v", l.starts, l.checks)
	}
}

func TestWavesSavedKeysDoNothingOnTheAggregateTab(t *testing.T) {
	h, l := wavesHarness(t)
	l.saved = []daemon.SavedWaveView{savedWave("pair-a", wm("A", "open", "Alpha"))}
	// The keys work on a project's Waves tab ...
	openWaves(t, h)
	h.deliver(press("L"))
	if h.m.waves.list == nil || len(l.lists) != 1 {
		t.Fatal("L does not open the saved list on a project's Waves tab")
	}
	h.deliver(press("esc"))
	// ... and do nothing on the combined Running entry, in any view.
	h.m.switchTo(runningEntry)
	for _, view := range []int{0, wavesView} {
		h.m.view = view
		for _, k := range []string{"w", "L", "w", "enter"} {
			h.deliver(press(k))
		}
	}
	if h.m.waves != nil || len(l.defines) != 0 || len(l.lists) != 1 || len(l.starts) != 0 {
		t.Fatalf("the aggregate tab acted: waves %v defines %v lists %v", h.m.waves, l.defines, l.lists)
	}
}

func TestWavesSavedKeysAreHiddenWithoutADaemon(t *testing.T) {
	h := newHarness(t, proj("ork", false))
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.m.switchTo(0)
	openWaves(t, h)
	for _, k := range []string{"w", "L"} {
		h.deliver(press(k))
		if want := "No daemon connection; action unavailable"; h.m.actionStatus != want {
			t.Fatalf("%s: status %q, want %q", k, h.m.actionStatus, want)
		}
		if h.m.waves.naming != nil || h.m.waves.list != nil {
			t.Fatalf("%s opened something without a daemon", k)
		}
	}
	if hint := hintOf(h, 100); strings.Contains(hint, "save") || strings.Contains(hint, "saved") {
		t.Fatalf("the hint offers the keys without a daemon: %q", hint)
	}
}

func TestWavesSavedKeysAreRefusedWhileAPlanOrStartIsInFlight(t *testing.T) {
	h, l := wavesHarness(t)
	l.saved = []daemon.SavedWaveView{savedWave("pair-a", wm("A", "open", "Alpha"))}
	openWaves(t, h)
	_, plan := h.m.Update(press(" ")) // the plan call is held
	h.deliver(press("w"))
	h.deliver(press("L"))
	if h.m.waves.naming != nil || h.m.waves.list != nil || len(l.lists) != 0 {
		t.Fatal("a key worked while a plan was pending")
	}
	if hint := hintOf(h, 100); strings.Contains(hint, "save") {
		t.Fatalf("the hint offers save while a plan is pending: %q", hint)
	}
	h.deliver(plan())
	h.m.waves.starting = true
	h.deliver(press("w"))
	h.deliver(press("L"))
	if h.m.waves.naming != nil || h.m.waves.list != nil {
		t.Fatal("a key worked while a start was pending")
	}
	h.m.waves.starting = false
	h.deliver(press("w"))
	if h.m.waves.naming == nil {
		t.Fatal("w does not open the input once the plan is back")
	}
	h.deliver(press("esc"))
	h.deliver(press("L"))
	if h.m.waves.list == nil {
		t.Fatal("L does not open the list once the plan is back")
	}
}

func TestWavesHintLinesShowSaveAndSavedAt100And80Columns(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	// Nothing ticked: the list is offered, save is not.
	for _, width := range []int{100, 80} {
		hint := hintOf(h, width)
		if !strings.Contains(hint, "L saved") || strings.Contains(hint, "w save") {
			t.Errorf("%d columns, nothing ticked: %q", width, hint)
		}
		if !strings.Contains(hint, "q quit; ? help") {
			t.Errorf("%d columns lost the way out: %q", width, hint)
		}
	}
	tickWave(t, h)
	h.deliver(press("down"))
	tickWave(t, h)
	// Two ticked (owner, adj 270, ui.waves-hint-priority): the tick hint is the
	// short `space tick`, and `enter item` goes first where the line is short, so
	// e edit lineup, w save and L saved all fit at 100x30. At 80 columns L saved
	// goes too; the way out stays.
	for _, tc := range []struct {
		width      int
		want, lack []string
	}{
		{120, []string{"enter item; space tick; l launch; e edit lineup; w save; L saved"}, nil},
		{100, []string{"space tick; l launch; e edit lineup; w save; L saved; q quit; ? help"}, []string{"enter item", "space tick item"}},
		{80, []string{"space tick; l launch; e edit lineup; w save; q quit; ? help"}, []string{"L saved", "space tick item"}},
	} {
		hint := hintOf(h, tc.width)
		for _, s := range tc.want {
			if !strings.Contains(hint, s) {
				t.Errorf("%d columns, two ticked: %q lacks %q", tc.width, hint, s)
			}
		}
		for _, s := range tc.lack {
			if strings.Contains(hint, s) {
				t.Errorf("%d columns, two ticked: %q holds %q", tc.width, hint, s)
			}
		}
	}
	h.deliver(press("w"))
	typeText(h, "pair-a")
	hint := hintOf(h, 100)
	if !strings.Contains(hint, "enter save") || !strings.Contains(hint, "esc cancel") || strings.Contains(hint, "space") {
		t.Errorf("name input hint: %q", hint)
	}
}

// The help names the saved-wave keys on the end of the Waves sentence, which
// costs the help no line: at 100x30 the Waves help already hid its last three
// lines before S216 (the marker read +3), and still hides exactly those.
func TestWavesHelpListsTheSavedWaveKeys(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	h.deliver(press("?"))
	v := h.at(100, 30)
	if !strings.Contains(v, "S65, an earlier item. w saves the ticked items; L lists saved waves.") {
		t.Errorf("the help lacks the saved-wave keys:\n%s", v)
	}
	if !strings.Contains(v, "+3 more lines") || !strings.Contains(v, "r refresh now; q quit; ? show or hide this help") {
		t.Errorf("the saved-wave keys cost the Waves help a line at 100x30:\n%s", v)
	}
}

// The saved list keeps the member rows of the wave under the cursor in view
// when there are more saved waves than the body holds, so enter loads what the
// owner sees.
func TestWavesSavedListShowsTheMembersOfTheWaveUnderTheCursor(t *testing.T) {
	h, l := wavesHarness(t)
	for i := 1; i <= 6; i++ {
		l.saved = append(l.saved, savedWave(fmt.Sprintf("wave-set-%d", i), wm(fmt.Sprintf("M%d1", i), "open", "First"), wm(fmt.Sprintf("M%d2", i), "open", "Second"), wm(fmt.Sprintf("M%d3", i), "done", "Third")))
	}
	openWaves(t, h)
	h.deliver(press("L"))
	for i := 0; i < 4; i++ {
		h.deliver(press("down"))
	}
	v := h.at(80, 24)
	for _, id := range []string{"wave-set-5", "M51", "M52", "M53"} {
		if !strings.Contains(v, id) {
			t.Errorf("a member of the wave under the cursor is hidden: no %s\n%s", id, v)
		}
	}
}

func TestDaemonAdapterSavesAndListsWaves(t *testing.T) {
	d, _, _, _ := requestServer(t, func(_ context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case daemon.MethodWaveDefine:
			var p daemon.WaveDefineParams
			if err := json.Unmarshal(params, &p); err != nil || p.Project != "ork" || p.Name != "pair-a" || !reflect.DeepEqual(p.Items, []string{"A", "B"}) || !p.Replace {
				return nil, fmt.Errorf("wrong define params: %s", params)
			}
			return daemon.SavedWave{Project: p.Project, Name: p.Name, Items: p.Items}, nil
		case daemon.MethodWaveList:
			var p daemon.WaveListParams
			if err := json.Unmarshal(params, &p); err != nil || p.Project != "ork" {
				return nil, fmt.Errorf("wrong list params: %s", params)
			}
			return daemon.WaveListResult{Waves: []daemon.SavedWaveView{savedWave("pair-a", wm("A", "open", "Alpha"))}}, nil
		}
		return nil, fmt.Errorf("unexpected %s", method)
	})
	var live Live = d
	saved, err := live.WaveDefine(context.Background(), daemon.WaveDefineParams{Project: "ork", Name: "pair-a", Items: []string{"A", "B"}, Replace: true})
	if err != nil || saved.Name != "pair-a" {
		t.Fatalf("define: %v %+v", err, saved)
	}
	waves, err := live.WaveList(context.Background(), "ork")
	if err != nil || len(waves) != 1 || waves[0].Name != "pair-a" || waves[0].Items[0].Title != "Alpha" {
		t.Fatalf("list: %v %+v", err, waves)
	}
}

func TestWavesSavedGolden(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		for _, mono := range []bool{false, true} {
			for _, ascii := range []bool{false, true} {
				theme, suffix := "color", ""
				if mono {
					theme = "mono"
				}
				if ascii {
					suffix = "-ascii"
				}
				name := fmt.Sprintf("waves-saved-list-%dx%d-%s%s.txt", size[0], size[1], theme, suffix)
				t.Run(name, func(t *testing.T) {
					h, l := wavesHarness(t)
					h.m.cfg.Theme = NewTheme(mono)
					h.m.cfg.ASCII = ascii
					l.saved = []daemon.SavedWaveView{
						savedWave("pair-a", wm("A", "open", "Alpha"), wm("B", "done", "Beta"), wm("X", "missing", "")),
						savedWave("solo", wm("C", "open", "Gamma, a title long enough that it has to be cut when the terminal is narrow")),
					}
					h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					openWaves(t, h)
					h.deliver(press("L"))
					h.deliver(press("down"))
					h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					checkGolden(t, name, h.m.View().Content)
				})
			}
		}
	}
}

func TestWavesNameInputGolden(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		name := fmt.Sprintf("waves-name-input-%dx%d-mono.txt", size[0], size[1])
		t.Run(name, func(t *testing.T) {
			h, _ := wavesHarness(t)
			h.m.cfg.Theme = NewTheme(true)
			h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			tickBThenA(t, h)
			h.deliver(press("w"))
			typeText(h, "pair-a")
			h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			checkGolden(t, name, h.m.View().Content)
		})
	}
}
