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
	h.deliver(press(" "))
	if !strings.Contains(h.view(), "Lineup") {
		t.Fatal("ticking did not show column")
	}
	h.deliver(press("esc"))
	if strings.Contains(h.view(), "Lineup") {
		t.Fatal("column remained after last tick cancelled")
	}
	tickWave(t, h, "y", "n")
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
					tickWave(t, h, "y", "n")
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
				tickWave(t, h, "y", "n")
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

func TestWavesFooterWrapsBetweenHints(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	body := strings.Join(h.m.wavesBody(60).body, "\n")
	for _, hint := range []string{"[enter] item", "[space] tick item", "[e] edit lineup", "[l] launch wave"} {
		if !strings.Contains(body, hint) {
			t.Errorf("split hint %q:\n%s", hint, body)
		}
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
		if h.m.waves.paneOffset != size[1]-5 {
			t.Errorf("page offset %d, want %d", h.m.waves.paneOffset, size[1]-5)
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
	h.deliver(press("a"))
	body = sgr.ReplaceAllString(strings.Join(h.m.wavesBody(72).body, "\n"), "")
	if !strings.Contains(body, "Wave 14") || len(h.m.waves.rows()) != 14 {
		t.Fatalf("show all failed:\n%s", body)
	}
}

func TestTickedItemStaysInViewWithInputs(t *testing.T) {
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
		if !strings.Contains(v, "1 ticked, not launched; [l] launches") {
			t.Fatalf("tick status offscreen:\n%s", v)
		}
		if !strings.Contains(v, "[x] "+id) || !strings.Contains(v, "Well specified? [y/n]:") || !strings.Contains(v, "Split? [y/n]:") {
			t.Fatalf("item and fields not visible:\n%s", v)
		}
		body := sgr.ReplaceAllString(strings.Join(h.m.wavesBody(72).body, "\n"), "")
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			if strings.Contains(line, "[x] "+id+" ") && (i+3 >= len(lines) || !strings.Contains(lines[i+1], "launch inputs") || !strings.Contains(lines[i+2], "Well specified?") || !strings.Contains(lines[i+3], "Split?")) {
				t.Fatalf("inputs not directly below item:\n%s", body)
			}
		}
	}
}

func TestOnlyActiveInputShowsCursor(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	h.deliver(press(" "))
	for focus := 0; focus < 2; focus++ {
		body := h.m.wavesBody(72).body
		for _, line := range body {
			if strings.Contains(line, "[y/n]:") {
				active := strings.Contains(line, []string{"Well specified?", "Split?"}[focus])
				if reverseSGR.MatchString(line) != active {
					t.Fatalf("field cursor focus %d: %q", focus, line)
				}
			}
		}
		h.deliver(press("tab"))
	}
}

func TestWavesCompactLineupAndContinuation(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h, "y", "n")
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
	if !strings.Contains(plainView(h.m), "1 ticked, not launched; [l] launches") {
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

func TestWavesLaunchRunNoticeAndJump(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h, "y", "n")
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}}
	snap := h.m.projects[0].snap
	snap.EngineRuns = []source.EngineRun{{ID: "ork-new", Item: "A", State: "queued"}}
	h.src.set(snap)
	h.deliver(press("l"))
	h.deliver(press("y"))
	if h.m.actionStatus != "launched A as ork-new; enter opens it" {
		t.Fatalf("launch notice: %s", h.m.actionStatus)
	}
	if !strings.Contains(plainView(h.m), "running ork-new") {
		t.Fatal(plainView(h.m))
	}
	h.deliver(press(" "))
	if h.m.waves.inputs != nil {
		t.Fatal("launched item can be ticked again")
	}
	h.deliver(press("enter"))
	if h.m.view != 0 {
		t.Fatal("enter did not open Overview")
	}
	r, ok := h.m.selectedRow()
	if !ok || r.engineID != "ork-new" {
		t.Fatalf("jump selected %+v", r)
	}
}

func TestWavesReasonAndHintSpacing(t *testing.T) {
	h, l := wavesHarness(t)
	l.suggest.Unplaced[0].Title = strings.Repeat("Long title ", 10)
	openWaves(t, h)
	tickWave(t, h, "y", "n")
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
	tickWave(t, h, "y", "n")
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
	tickWave(t, h, "y", "n")
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
	tickWave(t, h, "y", "n")
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}}
	h.deliver(press("l"))
	h.deliver(press("y"))
	h.deliver(press("enter"))
	snap := h.m.projects[0].snap
	snap.EngineRuns = []source.EngineRun{{ID: "other", Item: "B", State: "queued"}, {ID: "ork-new", Item: "A", State: "queued"}}
	h.deliver(snapshotMsg{name: "ork", snap: snap})
	r, ok := h.m.selectedRow()
	if !ok || r.engineID != "ork-new" {
		t.Fatalf("delayed jump selected %+v", r)
	}
}

func TestWavesComposerGolden(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		for _, state := range []string{"inputs", "limited", "fallbacks", "launched"} {
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
					case "inputs":
						h.deliver(press(" "))
					case "fallbacks":
						tickWave(t, h, "y", "n")
						p := h.m.waves.picked["A"]
						p.Expected.Lineup["implementer"] = append(p.Expected.Lineup["implementer"], runrec.LineupEntry{Vendor: "claude", Model: "haiku", Effort: "high"}, runrec.LineupEntry{Vendor: "claude", Model: "sonnet", Effort: "medium"})
						h.m.waves.picked["A"] = p
					case "launched":
						tickWave(t, h, "y", "n")
						l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-20261025-123000-abcd", Item: "A"}}}
						h.deliver(press("l"))
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
	h.deliver(press("a"))
	if got := h.m.waves.rows()[h.m.waves.cursor].ID; got != "D" {
		t.Fatalf("expansion moved cursor to %s", got)
	}
}

func TestWavesDelayedJumpYieldsToNavigation(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h, "y", "n")
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}}
	h.deliver(press("l"))
	h.deliver(press("y"))
	h.deliver(press("enter"))
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
		tickWave(t, h, "y", "n")
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

func TestWavesFix1SecondaryLinesAndInputs(t *testing.T) {
	h, l := wavesHarness(t)
	h.m.cfg.Theme = NewTheme(false)
	openWaves(t, h)
	tickWave(t, h, "y", "n")
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
	h.deliver(press(" "))
	h.deliver(press(" "))
	body := h.m.wavesBody(60).body
	plain := sgr.ReplaceAllString(strings.Join(body, "\n"), "")
	for _, raw := range body {
		line := sgr.ReplaceAllString(raw, "")
		if strings.Contains(line, "launch inputs") {
			if line != "         launch inputs" {
				t.Errorf("input header: %q", line)
			}
			dimAt(t, raw, "launch inputs", false)
		}
		if strings.Contains(line, "[y/n]:") && (!strings.HasPrefix(line, "         ") || strings.Contains(line, ">")) {
			t.Errorf("input indentation/marker: %q", line)
		}
	}
	for _, hint := range []string{"[tab] next field", "[enter] plan", "[esc] cancel"} {
		if !strings.Contains(plain, hint) {
			t.Errorf("missing input hint %q", hint)
		}
	}
	if strings.Contains(plain, "[space] tick") || strings.Contains(plain, "[l] launch") {
		t.Error("composer hints still shown while entering inputs")
	}
}

func TestWavesFix1StatusNotInLineupEditor(t *testing.T) {
	h, _ := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h, "y", "n")
	if !strings.Contains(plainView(h.m), "1 ticked, not launched; [l] launches") {
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
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new"}}, {Item: "B", Run: &runrec.Record{ID: "ork-other"}}}
	h.deliver(press("l"))
	h.deliver(press("y"))
	if h.m.actionStatus != "launched 2 items; enter on a running row opens its run" {
		t.Fatal(h.m.actionStatus)
	}
	body := sgr.ReplaceAllString(strings.Join(h.m.wavesBody(60).body, "\n"), "")
	if !strings.Contains(body, "[enter] open run") || strings.Contains(body, "[enter] item") {
		t.Fatal("launched row hint missing")
	}
	h.m.waves.cursor = 2
	body = sgr.ReplaceAllString(strings.Join(h.m.wavesBody(60).body, "\n"), "")
	if !strings.Contains(body, "[enter] item") {
		t.Fatal("plain row hint changed")
	}
	help := sgr.ReplaceAllString(strings.Join(h.m.helpLines(100, 40), "\n"), "")
	if !strings.Contains(help, "Waves: a shows all waves. <S65: shares files with S65, an earlier item.") {
		t.Fatal(help)
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
	if at("[tab] next field") < 0 || at("[tab] next field") > at("1 ticked, not launched") || at("Split? [y/n]:") > at("[tab] next field") {
		t.Fatalf("input hints not pinned under the body:\n%s", strings.Join(lines, "\n"))
	}
	h.deliver(press("esc"))
	tickWave(t, h, "y", "n")
	if got := plainView(h.m); !strings.Contains(got, "[enter] item") || !strings.Contains(got, "[l] launch wave") {
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
