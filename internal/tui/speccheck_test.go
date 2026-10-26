// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
)

// twoQuestions are the open questions of item B: the first has a decision key.
func twoQuestions() []engine.SpecQuestion {
	return []engine.SpecQuestion{
		{Ask: &engine.OwnerQuestion{Title: "Which store holds the index?", Options: []engine.Option{{Label: "file", Summary: "one JSON file"}, {Label: "memory", Summary: "rebuilt at each start"}}, Recommend: 2, Reason: "nothing reads it across restarts"}, Key: "cache.index-store", Text: "Question: Which store holds the index?"},
		{Ask: &engine.OwnerQuestion{Title: "What names the flag?", Options: []engine.Option{{Label: "short", Summary: "-x"}, {Label: "long", Summary: "--extra"}, {Label: "none", Summary: "no flag"}}, Recommend: 2, Reason: "flags are spelled out"}, Text: "Question: What names the flag?"},
	}
}

// questionsForB makes the spec check answer: A ready, B with two questions.
func questionsForB(l *wavesLive) {
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		out := &daemon.SpecCheckResult{}
		for _, id := range items {
			r := daemon.SpecItemResult{Item: id, Ready: id != "B"}
			if id == "B" {
				r.Questions = twoQuestions()
			}
			out.Items = append(out.Items, r)
		}
		return out, nil
	}
}

func TestWavesLaunchRunsTheSpecCheckBeforeTheConfirmation(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	h.deliver(press("l"))
	if want := [][]string{{"ork", "A", "B"}}; !reflect.DeepEqual(l.checks, want) {
		t.Fatalf("checks %v, want %v", l.checks, want)
	}
	if !strings.Contains(h.view(), "ork wave start ork A B? [y/N] (Enter is No)") {
		t.Fatal(h.view())
	}
	if l.writes != 0 || len(l.starts) != 0 {
		t.Fatal("the check wrote or started something")
	}
	if !strings.Contains(hintLine(h.view()), "y start; n cancel") || strings.Contains(h.view(), "answer spec questions") {
		t.Errorf("hint with nothing held:\n%s", h.view())
	}
	// Every ticked item goes to the check again: the daemon asks the role only
	// about a spec that changed, so a ready item is not trusted here.
	h.deliver(press("n"))
	h.deliver(press("l"))
	if want := [][]string{{"ork", "A", "B"}, {"ork", "A", "B"}}; !reflect.DeepEqual(l.checks, want) {
		t.Errorf("checks %v, want %v", l.checks, want)
	}
}

func TestWavesAReadyItemWhoseSpecChangedIsAskedAgain(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	h.deliver(press("l"))
	h.deliver(press("n"))
	if !h.m.waves.specReady("A") {
		t.Fatal("A should be ready after the first check")
	}
	// Meanwhile A's spec changed, and the daemon's check finds a question.
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		out := &daemon.SpecCheckResult{}
		for _, id := range items {
			r := daemon.SpecItemResult{Item: id, Ready: id != "A"}
			if id == "A" {
				r.Questions = twoQuestions()
			}
			out.Items = append(out.Items, r)
		}
		return out, nil
	}
	h.deliver(press("l"))
	if h.m.waves.sitting == nil || h.m.waves.sitting.asks[0].item != "A" {
		t.Fatalf("A was not asked again:\n%s", h.view())
	}
}

func TestWavesSpecCheckShowsPendingAndBlocksTickAndLaunch(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	_, cmd := h.m.Update(press("l"))
	if cmd == nil || !strings.Contains(h.view(), "pending: spec.check") {
		t.Fatalf("no pending check:\n%s", h.view())
	}
	h.deliver(press("l"))
	h.deliver(press(" "))
	if len(l.checks) != 0 || h.m.confirm != nil || len(h.m.waves.picked) != 2 {
		t.Fatalf("checks %v confirm %v picked %v", l.checks, h.m.confirm, h.m.waves.picked)
	}
	h.deliver(cmd())
	if h.m.confirm == nil {
		t.Fatal("no confirmation after the check")
	}
}

func TestWavesSpecCheckErrorIsShownVerbatim(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	l.check = func([]string) (*daemon.SpecCheckResult, error) {
		return nil, &ipc.Error{Code: "internal", Message: "Exact CHECK failure."}
	}
	h.deliver(press("l"))
	if h.m.confirm != nil || !strings.Contains(h.view(), "internal: Exact CHECK failure.") {
		t.Fatal(h.view())
	}
	l.check = nil
	h.deliver(press("l"))
	if h.m.confirm == nil {
		t.Error("a failed check must not stick")
	}
}

func TestWavesQuestionsAreAnsweredInOneSittingThenEveryItemLaunches(t *testing.T) {
	h, l := wavesHarness(t)
	questionsForB(l)
	t.Setenv("ADJ_DECIDED_BY", "jon")
	h.m.byName["ork"].reg.Adj = true
	twoWaveItems(t, h)
	h.deliver(press("l"))
	v := h.view()
	for _, want := range []string{"Spec check", "B  Beta", "[B 1/2] Which store holds the index?", "1. file: one JSON file", "2. memory: rebuilt at each start (recommended)", "Why 2: nothing reads it across restarts", "Decision key: cache.index-store"} {
		if !strings.Contains(v, want) {
			t.Errorf("sitting lacks %q:\n%s", want, v)
		}
	}
	if h.m.confirm != nil || len(l.starts) != 0 {
		t.Fatal("the wave must wait for the sitting")
	}
	h.deliver(press("2"))
	if !strings.Contains(h.view(), "[B 2/2] What names the flag?") || len(l.answers) != 0 {
		t.Fatalf("second question:\n%s", h.view())
	}
	h.deliver(press("t"))
	h.deliver(tea.PasteMsg{Content: "call it --widen"})
	h.deliver(press("enter"))
	if len(l.answers) != 1 {
		t.Fatalf("answers: %+v", l.answers)
	}
	want := daemon.SpecAnswerParams{Project: "ork", Item: "B", Answers: []string{"memory: rebuilt at each start", "call it --widen"}, Adj: true, By: "jon"}
	if !reflect.DeepEqual(l.answers[0], want) {
		t.Errorf("answer %+v, want %+v", l.answers[0], want)
	}
	if h.m.confirm == nil || !strings.Contains(h.view(), "ork wave start ork A B? [y/N]") {
		t.Fatalf("no confirmation for both items:\n%s", h.view())
	}
	h.deliver(press("y"))
	if len(l.starts) != 1 || len(l.starts[0].Items) != 2 {
		t.Fatalf("starts: %+v", l.starts)
	}
	if len(l.checks) != 1 {
		t.Errorf("checks %v", l.checks)
	}
}

func TestWavesSkippingTheSittingLaunchesOnlyTheReadyItems(t *testing.T) {
	for _, skip := range []string{"esc", "s"} {
		t.Run(skip, func(t *testing.T) {
			h, l := wavesHarness(t)
			questionsForB(l)
			twoWaveItems(t, h)
			h.deliver(press("l"))
			h.deliver(press("1")) // one of two questions answered is not enough to record
			h.deliver(press(skip))
			if len(l.answers) != 0 {
				t.Fatalf("a half-answered item was recorded: %+v", l.answers)
			}
			if v := h.view(); !strings.Contains(v, "ork wave start ork A? [y/N] (Enter is No)") || !strings.Contains(v, "B waits on spec questions") {
				t.Fatalf("only A is ready, and the prompt says B is held:\n%s", v)
			}
			h.deliver(press("y"))
			if len(l.starts) != 1 || len(l.starts[0].Items) != 1 || l.starts[0].Items[0].Item != "A" {
				t.Fatalf("starts: %+v", l.starts)
			}
			// B stays ticked, with its questions on the row, and the status says so.
			if _, ok := h.m.waves.picked["B"]; !ok {
				t.Error("B was unticked")
			}
			v := h.view()
			if !strings.Contains(v, "waits on 2 spec questions") || !strings.Contains(h.m.actionStatus, "B waits on spec questions") {
				t.Errorf("status %q:\n%s", h.m.actionStatus, v)
			}
			// The hint says what l does now: answer the held questions.
			if !strings.Contains(v, "l answer spec questions") || strings.Contains(v, "l launch wave") {
				t.Errorf("hint with B held:\n%s", v)
			}
			// A launched, so only B is still ticked. The daemon remembers B's questions.
			h.deliver(press("l"))
			if got := l.checks[len(l.checks)-1]; !reflect.DeepEqual(got, []string{"ork", "B"}) {
				t.Errorf("second check %v", got)
			}
			if !strings.Contains(h.view(), "[B 1/2] Which store holds the index?") {
				t.Errorf("no sitting:\n%s", h.view())
			}
		})
	}
}

func TestWavesNothingReadyAfterTheSittingStartsNothing(t *testing.T) {
	h, l := wavesHarness(t)
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		out := &daemon.SpecCheckResult{}
		for _, id := range items {
			out.Items = append(out.Items, daemon.SpecItemResult{Item: id, Questions: twoQuestions()})
		}
		return out, nil
	}
	twoWaveItems(t, h)
	h.deliver(press("l"))
	if !strings.Contains(h.view(), "Spec check: 4 questions for A, B") {
		t.Fatalf("heading:\n%s", h.view())
	}
	h.deliver(press("esc"))
	if h.m.confirm != nil || len(l.starts) != 0 || !strings.Contains(h.m.actionStatus, "No item is ready") {
		t.Fatalf("confirm %v status %q", h.m.confirm, h.m.actionStatus)
	}
}

func TestWavesAnswerFailureLeavesTheItemHeld(t *testing.T) {
	h, l := wavesHarness(t)
	questionsForB(l)
	l.answerErr = &ipc.Error{Code: "internal", Message: "Exact ANSWER failure."}
	twoWaveItems(t, h)
	h.deliver(press("l"))
	h.deliver(press("1"))
	h.deliver(press("1"))
	if !strings.Contains(h.m.actionStatus, "internal: Exact ANSWER failure.") {
		t.Errorf("status %q", h.m.actionStatus)
	}
	if h.m.confirm == nil || !strings.Contains(h.view(), "ork wave start ork A? [y/N]") {
		t.Errorf("only A is ready after a failed record:\n%s", h.view())
	}
}

func TestWavesSittingFreeTextNeedsText(t *testing.T) {
	h, l := wavesHarness(t)
	questionsForB(l)
	twoWaveItems(t, h)
	h.deliver(press("l"))
	h.deliver(press("t"))
	h.deliver(press("enter")) // empty: stays on the question
	if !strings.Contains(h.view(), "[B 1/2]") || !strings.Contains(h.view(), "Answer:") {
		t.Fatalf("empty free text moved on:\n%s", h.view())
	}
	h.deliver(press("esc")) // back to the options, not out of the sitting
	if !strings.Contains(h.view(), "1. file") || h.m.waves.sitting == nil {
		t.Fatalf("esc left the sitting:\n%s", h.view())
	}
	h.deliver(press("9")) // out of range: ignored
	h.deliver(press("x"))
	if !strings.Contains(h.view(), "[B 1/2]") {
		t.Fatalf("a stray key moved on:\n%s", h.view())
	}
}

func TestWavesStaleSpecCheckAfterNavigation(t *testing.T) {
	h, l := wavesHarness(t)
	questionsForB(l)
	twoWaveItems(t, h)
	_, cmd := h.m.Update(press("l"))
	stale := cmd()        // the daemon has answered, and the reply is on its way
	h.deliver(press("]")) // another project: the waves state is dropped
	h.deliver(press("[")) // and a fresh one for the first project
	tickWave(t, h, "y", "n")
	h.deliver(press("down"))
	tickWave(t, h, "y", "n")
	if len(h.m.waves.picked) != 2 {
		t.Fatalf("picked %v", h.m.waves.picked)
	}
	h.m.waves.checking = true // the new state is waiting for a check of its own
	h.deliver(stale)
	if h.m.confirm != nil || h.m.waves.sitting != nil || len(h.m.waves.specs) != 0 {
		t.Fatalf("a stale check reply was applied: confirm %v sitting %v specs %v", h.m.confirm, h.m.waves.sitting, h.m.waves.specs)
	}
}

func TestWavesSittingSeparatesTheProgressLine(t *testing.T) {
	h, l := wavesHarness(t)
	questionsForB(l)
	twoWaveItems(t, h)
	h.deliver(press("l"))
	lines := strings.Split(plainView(h.m), "\n")
	for i, line := range lines {
		if strings.Contains(line, "0 of 2 answered") {
			prev := strings.TrimSpace(strings.SplitN(lines[i-1], "│", 2)[1])
			if prev != "" {
				t.Errorf("no blank line before the progress line: %q", lines[i-1])
			}
			return
		}
	}
	t.Fatalf("no progress line:\n%s", h.view())
}

func TestWavesSittingGolden(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		for _, mono := range []bool{false, true} {
			for _, state := range []string{"options", "free", "held", "confirm"} {
				theme := "color"
				if mono {
					theme = "mono"
				}
				name := fmt.Sprintf("waves-sitting-%s-%dx%d-%s.txt", state, size[0], size[1], theme)
				t.Run(name, func(t *testing.T) {
					h, l := wavesHarness(t)
					h.m.cfg.Theme = NewTheme(mono)
					questionsForB(l)
					h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					twoWaveItems(t, h)
					h.deliver(press("l"))
					switch state {
					case "free":
						h.deliver(press("t"))
						h.deliver(tea.PasteMsg{Content: "call it --widen"})
					case "confirm":
						h.deliver(press("esc")) // skip: the confirmation names B as held
					case "held":
						h.deliver(press("esc")) // skip: A is ready, B waits
						h.deliver(press("n"))   // do not launch yet
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
