// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func lineupHarness(t *testing.T) (*harness, *wavesLive) {
	t.Helper()
	h, l := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h, "y", "n")
	p := h.m.waves.picked["A"]
	p.Expected.Budgets = registry.Budgets{Minutes: 45, Tokens: 9000}
	h.m.waves.picked["A"] = p
	h.deliver(press("e"))
	if !strings.Contains(h.view(), "Edit lineup: A") {
		t.Fatal("lineup editor did not open")
	}
	return h, l
}

func replaceLineupField(h *harness, value string) {
	h.deliver(press("ctrl+u"))
	h.deliver(tea.PasteMsg{Content: value})
}

func changeImplementer(h *harness) {
	h.deliver(press("c"))
	h.deliver(press("tab"))
	replaceLineupField(h, "custom-model")
	h.deliver(press("enter"))
}

func TestLineupEditorStartsFromPlan(t *testing.T) {
	h, _ := lineupHarness(t)
	for _, text := range []string{"implementer", "claude sonnet high", "reviewer", "claude opus medium", "45", "9000"} {
		if !strings.Contains(h.view(), text) {
			t.Errorf("missing %q:\n%s", text, h.view())
		}
	}
}

func TestLineupKeyIgnoredOutsideWaves(t *testing.T) {
	for _, tc := range []struct {
		name string
		view int
		help bool
	}{
		{"Overview", 0, false},
		{"Questions", questionsView, false},
		{"Roadmap", roadmapView, false},
		{"Decisions", decisionsView, false},
		{"help", wavesView, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, ready := range []bool{false, true} {
				h, l := wavesHarness(t)
				if ready {
					openWaves(t, h)
					tickWave(t, h, "y", "n")
				}
				h.m.view = tc.view
				h.m.showHelp = tc.help
				plans := len(l.plans)
				h.deliver(press("e"))
				if h.m.actionStatus != "" {
					t.Errorf("ready=%t: e set status %q", ready, h.m.actionStatus)
				}
				if h.m.waves != nil && h.m.waves.lineup != nil {
					t.Errorf("ready=%t: e opened the lineup", ready)
				}
				if len(l.plans) != plans || l.writes != 0 || h.m.view != tc.view || h.m.showHelp != tc.help {
					t.Errorf("ready=%t: e changed the view or called the live service", ready)
				}
			}
		})
	}
}

func TestLineupBudgetZeroShowsDefault(t *testing.T) {
	for _, tc := range []struct {
		name      string
		override  registry.Budgets
		effective registry.Budgets
		want      string
	}{
		{"minutes", registry.Budgets{Minutes: 0, Tokens: 1234}, registry.Budgets{Minutes: 45, Tokens: 1234}, "budgets*: 45 min, 1234 tokens"},
		{"tokens", registry.Budgets{Minutes: 12, Tokens: 0}, registry.Budgets{Minutes: 12, Tokens: 9000}, "budgets*: 12 min, 9000 tokens"},
		{"both", registry.Budgets{}, registry.Budgets{Minutes: 45, Tokens: 9000}, "budgets*: 45 min, 9000 tokens"},
		{"zero default", registry.Budgets{}, registry.Budgets{}, "budgets*: 0 min, 0 tokens"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := lineupHarness(t)
			h.deliver(press("esc"))
			p := h.m.waves.picked["A"]
			// Recorded plan values already include the daemon's project defaults.
			p.Budgets = &tc.override
			p.Expected.Budgets = tc.effective
			h.m.waves.picked["A"] = p
			if !strings.Contains(h.view(), tc.want) {
				t.Errorf("missing %q:\n%s", tc.want, h.view())
			}
			h.deliver(press("e"))
			got := h.m.waves.lineup.budgets
			if got[0].Value() != strconv.Itoa(tc.effective.Minutes) || got[1].Value() != strconv.Itoa(tc.effective.Tokens) {
				t.Errorf("editor budgets = %q, %q; want %+v", got[0].Value(), got[1].Value(), tc.effective)
			}
			if *h.m.waves.picked["A"].Budgets != tc.override {
				t.Fatal("drawing the effective budget changed the override")
			}
		})
	}
}

func TestLineupEditorChangesOneRole(t *testing.T) {
	h, l := lineupHarness(t)
	changeImplementer(h)
	h.deliver(press("enter"))
	if len(l.plans) != 2 {
		t.Fatalf("plans=%d, want 2", len(l.plans))
	}
	p := h.m.waves.picked["A"]
	want := runrec.LineupEntry{Vendor: "claude", Model: "custom-model", Effort: "high"}
	if !reflect.DeepEqual(p.Lineup["implementer"], []runrec.LineupEntry{want}) || len(p.Lineup["reviewer"]) != 0 || len(p.Lineup["gate"]) != 0 {
		t.Fatalf("overrides: %+v", p.Lineup)
	}
	if !strings.Contains(h.view(), "impl*") || strings.Contains(h.view(), "rev*") {
		t.Fatal(h.view())
	}
}

func TestLineupEditorHasNoGateField(t *testing.T) {
	h, _ := lineupHarness(t)
	if strings.Contains(h.view(), "gate:") {
		t.Fatal("gate must follow reviewer, without its own field")
	}
}

func TestLineupEditRePlans(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "error"}[fail], func(t *testing.T) {
			h, l := lineupHarness(t)
			old := h.m.waves.picked["A"]
			if fail {
				l.planErr = &ipc.Error{Code: "bad_params", Message: "Exact override failure."}
			}
			changeImplementer(h)
			h.deliver(press("enter"))
			if len(l.plans) != 2 || l.plans[1].Lineup["implementer"][0].Model != "custom-model" {
				t.Fatalf("replan params: %+v", l.plans)
			}
			got := h.m.waves.picked["A"]
			if fail {
				if !strings.Contains(h.view(), "bad_params: Exact override failure.") || !reflect.DeepEqual(old, got) {
					t.Fatal("failed replan changed prior selection or hid error")
				}
			} else if got.Expected == old.Expected || got.Expected == nil || got.Expected.Lineup["implementer"][0].Model != "custom-model" {
				t.Fatal("returned plan not installed as Expected")
			}
		})
	}
}

func TestLineupEditorEscDiscards(t *testing.T) {
	h, l := lineupHarness(t)
	old := h.m.waves.picked["A"]
	changeImplementer(h)
	h.deliver(press("esc"))
	if !reflect.DeepEqual(old, h.m.waves.picked["A"]) || len(l.plans) != 1 {
		t.Fatal("esc changed lineup or planned")
	}
}

func TestLineupBudgetsMustBeNumbers(t *testing.T) {
	for _, value := range []string{"-1", "1.5", "oops", "999999999999999999999999999999"} {
		t.Run(value, func(t *testing.T) {
			h, l := lineupHarness(t)
			h.deliver(press("down"))
			h.deliver(press("down"))
			h.deliver(press("down"))
			replaceLineupField(h, value)
			h.deliver(press("enter"))
			if len(l.plans) != 1 || !strings.Contains(h.view(), "whole number") {
				t.Fatal("invalid budget accepted or missing error")
			}
		})
	}
}

func TestWaveStartCarriesOverrides(t *testing.T) {
	h, l := lineupHarness(t)
	changeImplementer(h)
	h.deliver(press("down"))
	h.deliver(press("down"))
	h.deliver(press("down"))
	replaceLineupField(h, "0")
	h.deliver(press("down"))
	replaceLineupField(h, "1234")
	h.deliver(press("enter"))
	h.deliver(press("down"))
	tickWave(t, h, "n", "y")
	h.deliver(press("l"))
	h.deliver(press("y"))
	if len(l.starts) != 1 {
		t.Fatal("wave did not launch")
	}
	a, b := l.starts[0].Items[0], l.starts[0].Items[1]
	if a.Lineup["implementer"][0].Model != "custom-model" || a.Budgets == nil || *a.Budgets != (registry.Budgets{Minutes: 0, Tokens: 1234}) {
		t.Fatalf("edited item: %+v", a)
	}
	if b.Lineup != nil || b.Budgets != nil {
		t.Fatalf("unedited item has overrides: %+v", b)
	}
}

func TestLineupGolden(t *testing.T) {
	for _, tc := range []struct {
		name string
		mono bool
	}{{"lineup-100x30-color.txt", false}, {"lineup-100x30-mono.txt", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := lineupHarness(t)
			h.m.cfg.Theme = NewTheme(tc.mono)
			checkGolden(t, tc.name, h.m.View().Content)
		})
	}
}

func TestLineupCyclesPlanChainAndPreservesFallbacks(t *testing.T) {
	h, l := lineupHarness(t)
	h.deliver(press("esc"))
	p := h.m.waves.picked["A"]
	original := []runrec.LineupEntry{
		{Vendor: "claude", Model: "sonnet", Effort: "high"},
		{Vendor: "claude", Model: "alternative", Effort: "low"},
		{Vendor: "claude", Model: "last", Effort: "medium"},
	}
	p.Expected.Lineup["implementer"] = append([]runrec.LineupEntry(nil), original...)
	h.deliver(press("e"))
	h.deliver(press("left"))
	if !strings.Contains(h.view(), "claude last medium") {
		t.Fatal(h.view())
	}
	h.deliver(press("right"))
	if !reflect.DeepEqual(h.m.waves.lineup.chains[0], original) {
		t.Fatalf("cycle back: %+v", h.m.waves.lineup.chains[0])
	}
	h.deliver(press("right"))
	h.deliver(press("enter"))
	want := []runrec.LineupEntry{original[1], original[0], original[2]}
	if len(l.plans) != 2 || !reflect.DeepEqual(l.plans[1].Lineup["implementer"], want) {
		t.Fatalf("cycled chain: %+v", l.plans)
	}
	if !reflect.DeepEqual(p.Expected.Lineup["implementer"], original) {
		t.Fatal("mutated old plan")
	}
	h.deliver(press("e"))
	changeImplementer(h)
	h.deliver(press("enter"))
	want[0].Model = "custom-model"
	if !reflect.DeepEqual(l.plans[2].Lineup["implementer"], want) {
		t.Fatalf("custom chain: %+v", l.plans[2].Lineup)
	}
}

func TestLineupUnchangedDoesNotPlan(t *testing.T) {
	h, l := lineupHarness(t)
	h.deliver(press("enter"))
	if len(l.plans) != 1 || h.m.waves.picked["A"].Lineup != nil || h.m.waves.picked["A"].Budgets != nil {
		t.Fatal("unchanged editor created overrides")
	}
}

func TestLineupPendingReplanBlocksLaunch(t *testing.T) {
	h, l := lineupHarness(t)
	changeImplementer(h)
	_, cmd := h.m.Update(press("enter"))
	if cmd == nil {
		t.Fatal("missing replan")
	}
	h.deliver(press("l"))
	h.deliver(press("y"))
	if len(l.starts) != 0 || h.m.confirm != nil {
		t.Fatal("launched with old plan while editing")
	}
	h.deliver(cmd())
	h.deliver(press("l"))
	h.deliver(press("y"))
	if len(l.starts) != 1 {
		t.Fatal("replan did not unblock launch")
	}
}

func TestLineupReviewerAndExistingOverrides(t *testing.T) {
	h, l := lineupHarness(t)
	changeImplementer(h)
	h.deliver(press("enter"))
	h.deliver(press("e"))
	h.deliver(press("down"))
	h.deliver(press("c"))
	replaceLineupField(h, "custom-vendor")
	h.deliver(press("tab"))
	replaceLineupField(h, "review-model")
	h.deliver(press("tab"))
	replaceLineupField(h, "review-effort")
	h.deliver(press("enter"))
	h.deliver(press("enter"))
	p := h.m.waves.picked["A"]
	want := runrec.LineupEntry{Vendor: "custom-vendor", Model: "review-model", Effort: "review-effort"}
	if len(l.plans) != 3 || p.Lineup["implementer"][0].Model != "custom-model" || p.Lineup["reviewer"][0] != want || len(p.Lineup["gate"]) != 0 {
		t.Fatalf("overrides: %+v", p.Lineup)
	}
	if p.Expected.Lineup["gate"][0] != want || !strings.Contains(h.view(), "rev*") {
		t.Fatal(h.view())
	}
	h.deliver(press("e"))
	h.deliver(press("enter"))
	if len(l.plans) != 3 {
		t.Fatal("unchanged reopen replanned")
	}
}

func TestLineupOnlyOpensTickedReadyItem(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	h.deliver(press("e"))
	if strings.Contains(h.view(), "Edit lineup:") || len(l.plans) != 0 {
		t.Fatal("edited unticked item")
	}
	tickWave(t, h, "y", "n")
	h.deliver(press("down"))
	h.deliver(press("e"))
	if strings.Contains(h.view(), "Edit lineup:") {
		t.Fatal("edited different item")
	}
}

func TestLineupTokenValidationAndPasteDoesNotSubmit(t *testing.T) {
	h, l := lineupHarness(t)
	for range 4 {
		h.deliver(press("tab"))
	}
	replaceLineupField(h, "oops\r\n")
	if len(l.plans) != 1 {
		t.Fatal("paste submitted")
	}
	h.deliver(press("enter"))
	if len(l.plans) != 1 || !strings.Contains(h.view(), "whole number") {
		t.Fatal("invalid token budget accepted")
	}
}

func TestLineupCustomEscDiscardsWholeEditor(t *testing.T) {
	h, l := lineupHarness(t)
	old := h.m.waves.picked["A"]
	changeImplementer(h)
	h.deliver(press("down"))
	h.deliver(press("down"))
	h.deliver(press("down"))
	replaceLineupField(h, "12")
	h.deliver(press("up"))
	h.deliver(press("c"))
	replaceLineupField(h, "discard-vendor")
	h.deliver(press("esc"))
	if h.m.waves.lineup != nil || !reflect.DeepEqual(old, h.m.waves.picked["A"]) || len(l.plans) != 1 {
		t.Fatal("custom esc did not discard the whole editor")
	}
}

func TestLineupUnavailableStatus(t *testing.T) {
	for _, state := range []string{"unticked", "pending", "missing"} {
		t.Run(state, func(t *testing.T) {
			h, _ := lineupHarness(t)
			h.deliver(press("esc"))
			want := "Wait for the plan before editing"
			switch state {
			case "unticked":
				delete(h.m.waves.picked, "A")
				want = "Tick the item before editing its lineup"
			case "pending":
				h.m.waves.planning = "A"
			case "missing":
				p := h.m.waves.picked["A"]
				p.Expected = nil
				h.m.waves.picked["A"] = p
			}
			h.deliver(press("e"))
			if h.m.waves.lineup != nil || !strings.Contains(h.view(), want) {
				t.Fatalf("missing status %q: %s", want, h.view())
			}
		})
	}
}

func TestLineupSavedBudgetVisible(t *testing.T) {
	h, _ := lineupHarness(t)
	h.deliver(press("down"))
	h.deliver(press("down"))
	h.deliver(press("down"))
	replaceLineupField(h, "0")
	h.deliver(press("down"))
	replaceLineupField(h, "1234")
	h.deliver(press("enter"))
	if !strings.Contains(h.view(), "budgets*: 0 min, 1234 tokens") {
		t.Fatal("saved budgets missing from Waves lineup")
	}
}

func TestLineupEditorAffordances(t *testing.T) {
	h, _ := lineupHarness(t)
	body := func() string { return strings.Join(h.m.lineupBody(100).body, "\n") }
	for _, want := range []string{"Edit lineup: A  Alpha", "  > implementer: claude sonnet high (no alternatives)"} {
		if !strings.Contains(body(), want) {
			t.Errorf("missing %q", want)
		}
	}
	if hint := wideHint(h); !strings.Contains(hint, "c custom") || !strings.Contains(hint, "enter save and replan") {
		t.Errorf("lineup hint: %q", hint)
	}
	e := h.m.waves.lineup
	e.plan.Lineup["implementer"] = append(e.plan.Lineup["implementer"], runrec.LineupEntry{Vendor: "other", Model: "model", Effort: "low"})
	e.chains[0] = append([]runrec.LineupEntry(nil), e.plan.Lineup["implementer"]...)
	if !strings.Contains(body(), "> implementer: < claude sonnet high >  1/2") {
		t.Error("missing cycle arrows and position")
	}
	h.deliver(press("right"))
	if !strings.Contains(body(), "implementer*: < other model low >  2/2") {
		t.Error("missing edited role or cycle position")
	}
	h.deliver(press("c"))
	if strings.Count(body(), "> ") != 1 || !strings.Contains(body(), "(editing)") {
		t.Error("custom mode must focus only the field and label the editing role")
	}
	h.deliver(press("enter"))
	h.deliver(press("down"))
	h.deliver(press("down"))
	h.deliver(press("down"))
	if hint := wideHint(h); !strings.Contains(hint, "up/down/tab row") || strings.Contains(hint, "left/right cycle") {
		t.Error("budget row hints are incorrect")
	}
}

func TestLineupCyclesMatrixAlternativesBothDirections(t *testing.T) {
	h, _ := lineupHarness(t)
	h.deliver(press("esc"))
	p := h.m.waves.picked["A"]
	original := p.Expected.Lineup["implementer"][0]
	alternative := runrec.LineupEntry{Vendor: "codex", Model: "sol", Effort: "high"}
	p.Expected.Lineup["implementer"] = append(p.Expected.Lineup["implementer"], alternative)
	h.m.waves.picked["A"] = p
	h.deliver(press("e"))
	for _, step := range []struct {
		key  string
		want runrec.LineupEntry
	}{{"right", alternative}, {"left", original}, {"left", alternative}, {"right", original}} {
		h.deliver(press(step.key))
		e := h.m.waves.lineup
		if got := e.chains[0][0]; got != step.want {
			t.Fatalf("%s selected %+v, want %+v", step.key, got, step.want)
		}
	}
}

// The UI pass row shows in the editor, for information: it follows the
// matrix, runs only on a slice that touches the UI, and has no row to move to.
func TestLineupEditorShowsTheUIPassRow(t *testing.T) {
	h, _ := lineupHarness(t)
	body := func() string { return strings.Join(h.m.lineupBody(100).body, "\n") }
	if strings.Contains(body(), "ui:") {
		t.Fatalf("a plan without a ui role shows a ui row:\n%s", body())
	}
	h.m.waves.lineup.plan.Lineup["ui"] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}, {Vendor: "codex", Model: "sol", Effort: "high"}}
	want := "  ui: claude opus medium -> codex sol high (UI slices only; from the matrix)"
	if !strings.Contains(body(), want) {
		t.Fatalf("missing %q:\n%s", want, body())
	}
	// It lines up with the editable roles: four spaces, as the reviewer's.
	if !strings.Contains(body(), "\n    reviewer:") || !strings.Contains(body(), "\n    ui: claude") {
		t.Fatalf("ui row is not indented like the reviewer row:\n%s", body())
	}
	// The row is not one the cursor visits: five editable rows.
	for i := 0; i < 5; i++ {
		h.deliver(press("down"))
	}
	if h.m.waves.lineup.row != 0 || strings.Contains(strings.Join(h.m.lineupBody(100).body, "\n"), "> ui") {
		t.Fatalf("cursor row %d", h.m.waves.lineup.row)
	}
	// Narrow: the line wraps and stays within the pane width.
	for _, line := range h.m.lineupBody(40).body {
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("line of %d cells at width 40: %q", w, line)
		}
	}
}

func TestLineupEditorCanAddAndDropTheArchitect(t *testing.T) {
	h, l := lineupHarness(t)
	h.deliver(press("down"))
	h.deliver(press("down"))
	if !strings.Contains(h.view(), "> architect: off") {
		t.Fatal("architect row missing: " + h.view())
	}
	h.deliver(press("c"))
	replaceLineupField(h, "codex")
	h.deliver(press("tab"))
	replaceLineupField(h, "sol")
	h.deliver(press("tab"))
	replaceLineupField(h, "high")
	h.deliver(press("enter"))
	h.deliver(press("enter"))
	p := h.m.waves.picked["A"]
	want := []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}}
	if len(l.plans) != 2 || !reflect.DeepEqual(p.Lineup["architect"], want) || !reflect.DeepEqual(p.Expected.Lineup["architect"], want) {
		t.Fatalf("add: %+v plans=%d", p, len(l.plans))
	}
	if !strings.Contains(h.view(), "arch*:") {
		t.Fatal(h.view())
	}
	h.deliver(press("e"))
	h.deliver(press("down"))
	h.deliver(press("down"))
	h.deliver(press("d"))
	h.deliver(press("enter"))
	p = h.m.waves.picked["A"]
	chain, overridden := p.Lineup["architect"]
	if len(l.plans) != 3 || !overridden || len(chain) != 0 || len(p.Expected.Lineup["architect"]) != 0 {
		t.Fatalf("drop: %+v plans=%d", p, len(l.plans))
	}
	if !strings.Contains(h.view(), "arch*: off") {
		t.Fatal(h.view())
	}
	h.deliver(press("e"))
	h.deliver(press("enter"))
	if len(l.plans) != 3 {
		t.Fatal("unchanged off architect replanned")
	}
}

func TestArchitectDropHintUsesDAndKeepsSaveAt80x24(t *testing.T) {
	h, _ := lineupHarness(t)
	e := h.m.waves.lineup
	e.plan.Lineup["architect"] = []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}}
	e.chains[2] = append([]runrec.LineupEntry(nil), e.plan.Lineup["architect"]...)
	e.row = 2
	found := false
	for _, hint := range h.m.contextHints() {
		if hint.b.Help().Desc == "drop architect" {
			found = true
			if !reflect.DeepEqual(hint.b.Keys(), []string{"d"}) {
				t.Errorf("drop hint keys = %q, want [d]", hint.b.Keys())
			}
		}
	}
	if !found {
		t.Fatal("missing drop hint")
	}
	hint := hintLine(h.at(80, 24))
	if !strings.Contains(hint, "enter save and replan") {
		t.Errorf("save lost at 80x24: %s", hint)
	}
}

func TestArchitectOffHasNoCycleDecoration(t *testing.T) {
	for _, chain := range [][]runrec.LineupEntry{nil, {{Vendor: "codex", Model: "sol", Effort: "high"}}, {{Vendor: "codex", Model: "sol", Effort: "high"}, {Vendor: "claude", Model: "opus", Effort: "medium"}}} {
		h, _ := lineupHarness(t)
		e := h.m.waves.lineup
		e.plan.Lineup["architect"] = chain
		e.chains[2] = append([]runrec.LineupEntry(nil), chain...)
		e.row = 2
		h.deliver(press("d"))
		want := "> architect: off"
		if len(chain) > 0 {
			want = "> architect*: off"
		}
		for _, line := range h.m.lineupBody(100).body {
			if strings.Contains(line, "architect") && strings.TrimSpace(sgr.ReplaceAllString(line, "")) != want {
				t.Errorf("off row = %q, want %q", sgr.ReplaceAllString(line, ""), want)
			}
		}
	}
}

func TestArchitectDroppedGolden(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h, _ := lineupHarness(t)
		h.m.cfg.Theme = NewTheme(mono)
		e := h.m.waves.lineup
		e.plan.Lineup["architect"] = []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}, {Vendor: "claude", Model: "opus", Effort: "medium"}}
		e.chains[2] = append([]runrec.LineupEntry(nil), e.plan.Lineup["architect"]...)
		e.row = 2
		theme := "color"
		if mono {
			theme = "mono"
		}
		h.at(80, 24)
		checkGolden(t, "lineup-architect-80x24-"+theme+".txt", h.m.View().Content)
		h.deliver(press("d"))
		checkGolden(t, "lineup-architect-off-80x24-"+theme+".txt", h.m.View().Content)
		h.deliver(press("enter"))
		checkGolden(t, "waves-architect-off-80x24-"+theme+".txt", h.m.View().Content)
	}
}

func TestWavesLineupStillWorksAfterEditorExtraction(t *testing.T) {
	h, l := lineupHarness(t)
	e := h.m.waves.lineup
	original := e.plan.Lineup["implementer"][0]
	fallback := runrec.LineupEntry{Vendor: "codex", Model: "sol", Effort: "medium"}
	e.plan.Lineup["implementer"] = append(e.plan.Lineup["implementer"], fallback)
	reviewer := append([]runrec.LineupEntry(nil), e.plan.Lineup["reviewer"]...)
	h.deliver(press("right"))
	if !reflect.DeepEqual(e.chains[0], []runrec.LineupEntry{fallback, original}) {
		t.Fatalf("cycled chain: %+v", e.chains[0])
	}
	changeImplementer(h)
	h.deliver(press("down"))
	h.deliver(press("down"))
	h.deliver(press("d"))
	h.deliver(press("down"))
	replaceLineupField(h, "20")
	h.deliver(press("down"))
	replaceLineupField(h, "0")
	h.deliver(press("enter"))
	if len(l.plans) != 2 {
		t.Fatalf("plans %d", len(l.plans))
	}
	p := l.plans[1]
	want := []runrec.LineupEntry{{Vendor: "codex", Model: "custom-model", Effort: "medium"}, original}
	if !reflect.DeepEqual(p.Lineup["implementer"], want) || len(p.Lineup["reviewer"]) != 0 || !reflect.DeepEqual(h.m.waves.picked["A"].Expected.Lineup["reviewer"], reviewer) || len(p.Lineup["architect"]) != 0 || p.Budgets == nil || *p.Budgets != (registry.Budgets{Minutes: 20}) {
		t.Fatalf("params: %+v", p)
	}
}
