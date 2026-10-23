// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	if !strings.Contains(h.view(), "implementer*") || strings.Contains(h.view(), "reviewer*") {
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
	if p.Expected.Lineup["gate"][0] != want || !strings.Contains(h.view(), "reviewer*") {
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
	for range 3 {
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
	for _, want := range []string{"Edit lineup: A  Alpha", "  > implementer: claude sonnet high (no alternatives; c for custom)", "[up/down/tab] row   [left/right] cycle   [c] custom"} {
		if !strings.Contains(body(), want) {
			t.Errorf("missing %q", want)
		}
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
	if !strings.Contains(body(), "[up/down/tab] row   type a whole number") || strings.Contains(body(), "[left/right]") {
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
