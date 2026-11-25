// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestSpecNoteNamesACheckFinding(t *testing.T) {
	var result daemon.SpecItemResult
	const finding = "Check entry q quit is not a runnable command"
	if err := json.Unmarshal([]byte(`{"item":"S1","ready":false,"findings":["`+finding+`"]}`), &result); err != nil {
		t.Fatal(err)
	}
	if got := specNote(result); got != finding {
		t.Fatalf("note %q, want %q", got, finding)
	}
}

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

func TestWavesOpenAdjHold(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	l.check = func([]string) (*daemon.SpecCheckResult, error) {
		var result daemon.SpecCheckResult
		if err := json.Unmarshal([]byte(`{"items":[{"item":"A","ready":true},{"item":"B","ready":false,"waits_on":[42]}]}`), &result); err != nil {
			t.Fatal(err)
		}
		return &result, nil
	}
	h.deliver(press("l"))
	if h.m.waves.sitting != nil || !strings.Contains(h.view(), "ork wave start ork A? [y/N] (Enter is No)  B waits on adj 42") {
		t.Fatalf("held item entered sitting or launch: %s", h.view())
	}
	h.deliver(press("n"))
	if v := h.view(); !strings.Contains(v, "waits on adj 42") {
		t.Fatalf("missing adj hold: %s", v)
	}
	checkGolden(t, "waves-open-adj.txt", h.m.View().Content)
}

func TestWavesAdjHoldAfterLaunch(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	l.check = func([]string) (*daemon.SpecCheckResult, error) {
		return &daemon.SpecCheckResult{Items: []daemon.SpecItemResult{{Item: "A", Ready: true}, {Item: "B", WaitsOn: []int{42}}}}, nil
	}
	l.result.Items = []daemon.WaveItemResult{{Item: "A", Run: &runrec.Record{ID: "ork-new", Item: "A"}}}
	h.deliver(press("l"))
	h.deliver(press("y"))
	if v := h.view(); !strings.Contains(v, "launched A as ork-new; enter opens it; B waits on adj 42") {
		t.Fatalf("wrong launch status: %s", v)
	}
}

func TestWavesAdjHoldWithRemainingQuestion(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	l.check = func([]string) (*daemon.SpecCheckResult, error) {
		return &daemon.SpecCheckResult{Items: []daemon.SpecItemResult{{Item: "A", Ready: true}, {Item: "B", WaitsOn: []int{42}, Questions: twoQuestions()[1:]}}}, nil
	}
	h.deliver(press("l"))
	h.deliver(press("n"))
	if hint := hintLine(h.view()); !strings.Contains(hint, "l launch") || strings.Contains(hint, "answer spec questions") {
		t.Fatalf("unanswerable adj hold hint: %s", hint)
	}
}

func TestWavesOnlyAdjHold(t *testing.T) {
	h, l := wavesHarness(t)
	openWaves(t, h)
	h.deliver(press("down"))
	tickWave(t, h)
	l.check = func([]string) (*daemon.SpecCheckResult, error) {
		return &daemon.SpecCheckResult{Items: []daemon.SpecItemResult{{Item: "B", WaitsOn: []int{42}}}}, nil
	}
	h.deliver(press("l"))
	if v := h.view(); !strings.Contains(v, "No item is ready: B waits on adj 42") {
		t.Fatalf("wrong all-held status: %s", v)
	}
}

func TestWavesAllLookupNotices(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	l.check = func([]string) (*daemon.SpecCheckResult, error) {
		return &daemon.SpecCheckResult{Items: []daemon.SpecItemResult{
			{Item: "A", Questions: twoQuestions(), Notices: []string{"adj check key.a: lookup A failed"}},
			{Item: "B", Questions: twoQuestions(), Notices: []string{"adj check key.b: lookup B failed"}},
		}}, nil
	}
	h.deliver(press("l"))
	if v := h.view(); !strings.Contains(v, "adj check key.a: lookup A failed; adj check key.b: lookup B failed") {
		t.Fatalf("lookup notices lost: %s", v)
	}
}

func TestWavesStaleAdjAnswerIsShown(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	l.check = func([]string) (*daemon.SpecCheckResult, error) {
		var result daemon.SpecCheckResult
		if err := json.Unmarshal([]byte(`{"items":[{"item":"A","ready":true},{"item":"B","ready":false,"questions":[{"ask":{"title":"Store?","options":[{"label":"file","summary":"new store"}]},"previous":"decided as disk: old store on 2026-11-09"}]}]}`), &result); err != nil {
			t.Fatal(err)
		}
		return &result, nil
	}
	h.deliver(press("l"))
	if !strings.Contains(h.view(), "decided as disk: old store on 2026-11-09") {
		t.Fatal(h.view())
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

func TestSpecWaitText(t *testing.T) {
	for _, tc := range []struct {
		items []string
		d     time.Duration
		want  string
	}{
		{[]string{"S90"}, 12 * time.Second, "spec check: S90, 12s; wait for the confirmation"},
		{[]string{"A", "B"}, 72 * time.Second, "spec check: 2 items, 1m12s; wait for the confirmation"},
		{[]string{"A", "B", "C"}, 3725 * time.Second, "spec check: 3 items, 1h02m; wait for the confirmation"},
	} {
		if got := specWaitText(tc.items, tc.d); got != tc.want {
			t.Errorf("specWaitText(%v, %v) = %q, want %q", tc.items, tc.d, got, tc.want)
		}
	}
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m00s"},
		{-time.Second, "0s"},
	} {
		if got := checkClock(tc.d); got != tc.want {
			t.Errorf("checkClock(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestWavesSpecCheckSaysToWaitWithItsElapsedTime(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	_, cmd := h.m.Update(press("l"))
	if cmd == nil {
		t.Fatal("no check command")
	}
	if v := h.view(); !strings.Contains(v, "spec check: 2 items, 0s; wait for the confirmation") || strings.Contains(v, "pending: spec."+"check") { // the old status text
		t.Fatalf("no wait text:\n%s", v)
	}
	h.now = h.now.Add(72 * time.Second)
	const want = "spec check: 2 items, 1m12s; wait for the confirmation"
	if v := h.view(); !strings.Contains(v, want) {
		t.Fatalf("clock did not move:\n%s", v)
	}
	h.deliver(press("l"))
	if len(l.checks) != 0 || h.m.confirm != nil || !strings.Contains(h.view(), want) {
		t.Fatalf("second l: checks %v confirm %v:\n%s", l.checks, h.m.confirm, h.view())
	}
	h.deliver(cmd())
	if v := h.view(); !strings.Contains(v, "ork wave start ork A B? [y/N] (Enter is No)") || strings.Contains(v, "wait for the confirmation") {
		t.Fatalf("no confirmation after the check:\n%s", v)
	}
}

// A refusal while the check runs shows below the wait line and does not
// replace it (S264 review): e on an unticked row is still offered meanwhile.
func TestWavesSpecCheckKeepsItsWaitLineUnderARefusal(t *testing.T) {
	h, _ := wavesHarness(t)
	twoWaveItems(t, h)
	_, cmd := h.m.Update(press("l"))
	if cmd == nil {
		t.Fatal("no check command")
	}
	h.m.Update(press("down")) // C is not ticked
	h.m.Update(press("e"))
	h.now = h.now.Add(72 * time.Second)
	v := h.view()
	wait, refusal := "spec check: 2 items, 1m12s; wait for the confirmation", "Tick the item before editing its lineup"
	i, j := strings.Index(v, wait), strings.Index(v, refusal)
	if i < 0 || j < 0 || i > j {
		t.Fatalf("want the wait line, then the refusal below it:\n%s", v)
	}
	h.deliver(cmd())
	if v := h.view(); strings.Contains(v, refusal) || !strings.Contains(v, "ork wave start ork A B? [y/N]") {
		t.Fatalf("the refusal outlived the check:\n%s", v)
	}
}

func TestWavesListShowsOneLineOfOwnerNeeded(t *testing.T) {
	h, l := wavesHarness(t)
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		out := &daemon.SpecCheckResult{OwnerNeeded: "Slice S2 could not be read.\nSecond line.\nThird line."}
		for _, id := range items {
			out.Items = append(out.Items, daemon.SpecItemResult{Item: id, Ready: true})
		}
		return out, nil
	}
	twoWaveItems(t, h)
	h.deliver(press("l"))
	h.deliver(press("n"))
	v := h.view()
	if !strings.Contains(v, "Owner needed: Slice S2 could not be read. (+2 more lines)") || strings.Contains(v, "Second line.") {
		t.Fatalf("owner text:\n%s", v)
	}
	// A long first line is clipped to the body width and never wraps.
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		out := &daemon.SpecCheckResult{OwnerNeeded: strings.Repeat("Confirm the rollout window with the owner. ", 6) + "\nSecond line."}
		for _, id := range items {
			out.Items = append(out.Items, daemon.SpecItemResult{Item: id, Ready: true})
		}
		return out, nil
	}
	h.deliver(press("l"))
	h.deliver(press("n"))
	narrow := h.at(80, 24)
	n := 0
	for _, line := range strings.Split(narrow, "\n") {
		if strings.Contains(line, "Owner needed") {
			n++
			if lipgloss.Width(line) > 80 || !strings.HasSuffix(strings.TrimRight(line, " "), "(+1 more lines)") {
				t.Errorf("owner line not clipped to one line: %q", line)
			}
		}
		if strings.Contains(line, "Second line.") {
			t.Errorf("second line shown: %q", line)
		}
	}
	if n != 1 {
		t.Fatalf("owner lines %d:\n%s", n, narrow)
	}
	// One line shows with no marker.
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		out := &daemon.SpecCheckResult{OwnerNeeded: "Slice S2 could not be read."}
		for _, id := range items {
			out.Items = append(out.Items, daemon.SpecItemResult{Item: id, Ready: true})
		}
		return out, nil
	}
	h.deliver(press("l"))
	h.deliver(press("n"))
	if v := h.view(); !strings.Contains(v, "Owner needed: Slice S2 could not be read.") || strings.Contains(v, "more lines") {
		t.Fatalf("one-line text:\n%s", v)
	}
}

func TestWavesCheckingGolden(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		for _, mono := range []bool{false, true} {
			theme := "color"
			if mono {
				theme = "mono"
			}
			name := fmt.Sprintf("waves-checking-%dx%d-%s.txt", size[0], size[1], theme)
			t.Run(name, func(t *testing.T) {
				h, _ := wavesHarness(t)
				h.m.cfg.Theme = NewTheme(mono)
				twoWaveItems(t, h)
				h.m.Update(press("l"))
				h.now = h.now.Add(72 * time.Second)
				if v := h.at(size[0], size[1]); !strings.Contains(v, "spec check: 2 items, 1m12s; wait for the confirmation") {
					t.Fatalf("no wait text:\n%s", v)
				}
				checkGolden(t, name, h.m.View().Content)
			})
		}
	}
}

func TestWavesSpecCheckShowsPendingAndBlocksTickAndLaunch(t *testing.T) {
	h, l := wavesHarness(t)
	twoWaveItems(t, h)
	_, cmd := h.m.Update(press("l"))
	if cmd == nil || !strings.Contains(h.view(), "spec check: 2 items, 0s; wait for the confirmation") {
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
			if v := h.view(); !strings.Contains(v, "ork wave start ork A? [y/N] (Enter is No)") || !strings.Contains(v, "B waits on 2 spec questions") {
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
			if !strings.Contains(v, "waits on 2 spec questions") || !strings.Contains(h.m.actionStatus, "B waits on 2 spec questions") {
				t.Errorf("status %q:\n%s", h.m.actionStatus, v)
			}
			// The hint says what l does now: answer the held questions.
			if !strings.Contains(v, "l answer spec questions") || strings.Contains(v, "l launch;") {
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
	tickWave(t, h)
	h.deliver(press("down"))
	tickWave(t, h)
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

func TestHeldLayoutAt80x24KeepsOverlapWarnings(t *testing.T) {
	h, l := wavesHarness(t)
	questionsForB(l)
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	twoWaveItems(t, h)
	h.deliver(press("l"))
	h.deliver(press("esc"))
	h.deliver(press("n"))
	h.m.waves.ownerNeeded = strings.Repeat("Confirm the rollout window with the owner. ", 4)
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := plainView(h.m)
	for _, path := range []string{"shared.go", "second.go"} {
		if !strings.Contains(v, "A and B both touch "+path) {
			t.Errorf("overlap %s hidden:\n%s", path, v)
		}
	}
	if !strings.Contains(v, "waits on 2 spec") || !strings.Contains(v, "questions") {
		t.Errorf("held note lost:\n%s", v)
	}
}

func TestSittingHeadingAndQuestionTitleStylesDiffer(t *testing.T) {
	h, l := wavesHarness(t)
	questionsForB(l)
	twoWaveItems(t, h)
	h.deliver(press("l"))
	// Mark the styles without depending on terminal color detection.
	h.m.cfg.Theme.Heading = lipgloss.NewStyle().SetString("HEADING ")
	h.m.cfg.Theme.Dim = lipgloss.NewStyle().SetString("DIM ")
	out, _ := h.m.sittingParts(&h.m.waves.specState, 80, 0)
	heading, title := "", ""
	for _, line := range out.body {
		if strings.Contains(line, "Spec check:") {
			heading = line
		}
		if strings.Contains(line, "[B 1/2]") {
			title = line
		}
	}
	if heading == "" || title == "" || strings.Contains(heading, "HEADING") == strings.Contains(title, "HEADING") {
		t.Fatalf("heading %q and title %q use the same style", heading, title)
	}
}

func TestSittingKeepsOwnerNeededText(t *testing.T) {
	h, l := wavesHarness(t)
	l.check = func(items []string) (*daemon.SpecCheckResult, error) {
		var res daemon.SpecCheckResult
		raw := `{"owner_needed":"Confirm the rollout window.","items":[{"item":"A","ready":true},{"item":"B","questions":[{"text":"Question: Choose a window"}]}]}`
		if err := json.Unmarshal([]byte(raw), &res); err != nil {
			return nil, err
		}
		return &res, nil
	}
	twoWaveItems(t, h)
	h.deliver(press("l"))
	if v := h.view(); !strings.Contains(v, "Owner needed: Confirm the rollout window.") {
		t.Fatalf("owner text lost:\n%s", v)
	}
}

func TestLongSpecOwnerTextFollowsTheQuestionAt80x24(t *testing.T) {
	h, l := wavesHarness(t)
	questionsForB(l)
	twoWaveItems(t, h)
	h.deliver(press("l"))
	h.deliver(press("t"))
	h.m.waves.ownerNeeded = strings.Repeat("Long owner context must not displace the question or answer. ", 20)
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := plainView(h.m)
	for _, text := range []string{"[B 1/2]", "1. file:", "Answer:", "0 of 2 answered"} {
		if !strings.Contains(v, text) {
			t.Errorf("%s hidden:\n%s", text, v)
		}
	}
	parts, _ := h.m.sittingParts(&h.m.waves.specState, 60, 0)
	body := sgr.ReplaceAllString(strings.Join(parts.body, "\n"), "")
	if strings.Index(body, "Owner needed:") < strings.Index(body, "0 of 2 answered") {
		t.Errorf("owner text precedes progress:\n%s", body)
	}
	if !strings.Contains(body, "0 of 2 answered\n\n") {
		t.Error("owner text has no blank separation")
	}
}

func TestSelectedSpecNoteUsesTheSelectedRowSGR(t *testing.T) {
	h, l := wavesHarness(t)
	questionsForB(l)
	twoWaveItems(t, h)
	h.deliver(press("l"))
	h.deliver(press("esc"))
	h.deliver(press("n"))
	h.m.cfg.Theme = NewTheme(false)
	parts, _ := h.m.wavesParts(60)
	found := false
	for _, line := range parts.body {
		if strings.Contains(line, "waits on 2 spec") {
			found = true
			expected := h.m.cfg.Theme.TabActive.Render("waits on 2 spec")
			if !strings.Contains(line, expected[:len(expected)-len("\x1b[m")]) {
				t.Errorf("selected note has a dim patch: %q", line)
			}
			if strings.Contains(line, h.m.cfg.Theme.Dim.Render("waits on 2 spec")) {
				t.Errorf("selected note is dim: %q", line)
			}
		}
	}
	if !found {
		t.Fatal("selected note not rendered")
	}
}

// A failed answer write whose launch is gone still tells the owner (S190, S193
// note): the answers were not saved, and nothing else would say so.
func TestFailedSpecAnswerAfterTheLaunchWasDroppedShowsAStatus(t *testing.T) {
	h, l := launchHarness(t)
	launchQuestions(l)
	l.answerErr = &ipc.Error{Code: "internal", Message: "Exact ANSWER failure."}
	h.deliver(launchPlanning(t, h)())
	h.deliver(press("1"))
	_, record := h.m.Update(press("1"))
	if record == nil {
		t.Fatal("no answer command")
	}
	// esc is held by the finished sitting while its write runs (sittingKey has no
	// question left); navigation is what drops a launch then.
	h.deliver(press("esc"))
	h.m.switchTo(1)
	h.m.switchTo(0)
	if h.m.launch != nil {
		t.Fatal("the launch is still open")
	}
	h.m.actionStatus = ""
	for _, msg := range exec(record) {
		h.deliver(msg)
	}
	if want := "S90: internal: Exact ANSWER failure."; h.m.actionStatus != want {
		t.Fatalf("status %q, want %q", h.m.actionStatus, want)
	}
	// A write that worked says nothing.
	h.m.actionStatus = ""
	h.deliver(specAnswerMsg{state: &specState{}, item: "S90"})
	if h.m.actionStatus != "" {
		t.Fatalf("a good write set the status %q", h.m.actionStatus)
	}
}

// specCheckPermissionScreen is the Waves spec-check sitting on a live
// permission question whose detail is detail, at w by ht.
func specCheckPermissionScreen(t *testing.T, detail string, w, ht int) *harness {
	t.Helper()
	h, live := wavesHarness(t)
	openWaves(t, h)
	tickWave(t, h) // A is the first row; the wait line names what is checked
	q := &runrec.OwnerQuestion{
		Title:     "Allow Bash call 3?",
		Detail:    detail,
		Options:   []runrec.Option{{Label: "Allow", Summary: "Allow this call once"}, {Label: "Deny", Summary: "Deny this call"}},
		Recommend: 2,
		Reason:    "not allowlisted",
	}
	h.m.cfg.Live = &fakeSpecPermissionLive{Live: live, project: "ork", item: "A", answer: "allow", question: q}
	h.m.startSpecCheck([]daemon.RunParams{{Project: "ork", Item: "A"}})
	h.deliver(tickMsg{})
	h.m.Update(tea.WindowSizeMsg{Width: w, Height: ht})
	return h
}

// specCheckUnderTitle is the view lines between the permission title and its
// first option, after the sidebar rule. raw keeps the indent; trimmed does not.
func specCheckUnderTitle(t *testing.T, view string) (raw, trimmed []string) {
	t.Helper()
	in := false
	for _, line := range strings.Split(view, "\n") {
		if _, after, ok := strings.Cut(line, "│"); ok {
			line = after
		}
		text := strings.TrimSpace(line)
		switch {
		case in && strings.HasPrefix(text, "1. Allow"):
			return raw, trimmed
		case in && text == "":
			// The blank line between the command and the options is not detail.
		case in:
			raw = append(raw, line)
			trimmed = append(trimmed, text)
		case strings.Contains(text, "Allow Bash call 3?"):
			in = true
		}
	}
	t.Fatalf("no title followed by option 1 in:\n%s", view)
	return nil, nil
}

// A spec-check permission question shows its whole detail, wrapped, under the
// title. The run pane already does this (S373); this screen did not.
func TestSpecCheckPermissionQuestionShowsDetail(t *testing.T) {
	detail := permissionDetail()
	if len(detail) != 300 {
		t.Fatalf("test detail is %d characters", len(detail))
	}
	for _, c := range []struct {
		name string
		w, h int
	}{
		{"speccheck-permission-detail-100x30.txt", 100, 30},
		{"speccheck-permission-detail-80x24.txt", 80, 24},
	} {
		t.Run(c.name, func(t *testing.T) {
			view := plainView(specCheckPermissionScreen(t, detail, c.w, c.h).m)
			raw, lines := specCheckUnderTitle(t, view)
			if got := strings.Join(lines, " "); got != detail {
				t.Fatalf("the detail is not shown whole under the title:\ngot  %q\nwant %q\n%s", got, detail, view)
			}
			if len(lines) < 3 {
				t.Errorf("a 300-character detail is not wrapped at the pane width: %q", lines)
			}
			base := leading(raw[0])
			for _, line := range raw[1:] {
				if leading(line) != base+2 {
					t.Errorf("a wrapped line is not under a 2-space hanging indent: %q", line)
				}
			}
			if prev := specCheckLineBefore(t, view, "1. Allow"); strings.TrimSpace(prev) != "" {
				t.Errorf("the command runs into the options with no blank line: %q\n%s", prev, view)
			}
			checkGolden(t, c.name, view)
		})
	}
}

// specCheckLineBefore is the view line before the first one whose trimmed
// text starts with prefix, after the sidebar rule.
func specCheckLineBefore(t *testing.T, view, prefix string) string {
	t.Helper()
	prev := ""
	for _, line := range strings.Split(view, "\n") {
		if _, after, ok := strings.Cut(line, "│"); ok {
			line = after
		}
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return prev
		}
		prev = line
	}
	t.Fatalf("no line starting %q in:\n%s", prefix, view)
	return ""
}

// A detail taller than the body is omitted. The options, and a line that says
// how many detail lines are hidden, stay on screen. A prefix of the command
// does not.
func TestSpecCheckPermissionDetailHiddenWhenItDoesNotFit(t *testing.T) {
	var script []string
	for i := 1; i <= 25; i++ {
		script = append(script, fmt.Sprintf("echo line%02d && rm -rf /tmp/x%02d", i, i))
	}
	h := specCheckPermissionScreen(t, strings.Join(script, "\n"), 80, 24)
	view := plainView(h.m)
	if strings.Contains(view, "echo line01") {
		t.Errorf("a detail that does not fit is shown in part:\n%s", view)
	}
	if !strings.Contains(view, "+25 detail lines hidden") {
		t.Errorf("the hidden line is missing:\n%s", view)
	}
	for _, want := range []string{"1. Allow", "2. Deny"} {
		if !strings.Contains(view, want) {
			t.Errorf("the option %q is off screen:\n%s", want, view)
		}
	}
	marker := "  " + h.m.cfg.Theme.Dim.Render("+25 detail lines hidden")
	if !strings.Contains(h.m.View().Content, marker) {
		t.Errorf("the hidden line is not dim")
	}
}

// tallPermissionDetail is a 2,000-character command of single-spaced words.
func tallPermissionDetail() string {
	text := "curl -s https://example.test/tall-detail.sh | sh -s -- --prefix /opt/tool"
	for i := 1; len(text) < 2000; i++ {
		text += fmt.Sprintf(" --flag%03d=value%03d", i, i)
	}
	return text[:2000]
}

// A 2,000-character detail is taller than 80x24. The screen drops it whole
// behind the overflow marker, and the rows around it stay (golden).
func TestSpecCheckTallPermissionDetailOverflows(t *testing.T) {
	detail := tallPermissionDetail()
	if len(detail) != 2000 {
		t.Fatalf("test detail is %d characters", len(detail))
	}
	h := specCheckPermissionScreen(t, detail, 80, 24)
	view := plainView(h.m)
	_, under := specCheckUnderTitle(t, view)
	if len(under) != 1 || !strings.HasPrefix(under[0], "+") || !strings.HasSuffix(under[0], " detail lines hidden") {
		t.Fatalf("a 2,000-character detail is not replaced by the overflow marker: %q\n%s", under, view)
	}
	if strings.Contains(view, "tall-detail.sh") || strings.Contains(view, "flag001") {
		t.Fatalf("the detail is shown beside the marker:\n%s", view)
	}
	for _, want := range []string{
		"Spec check tool permission",
		"[A 1/1] Allow Bash call 3?",
		"1. Allow: Allow this call once",
		"2. Deny: Deny this call (recommended)",
		"Why 2: not allowlisted",
		"The spec check is still running.",
		"spec check: A, 0s; wait for the confirmation",
		"1-2 pick; s/esc deny call",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the screen lost %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "more lines") || strings.Contains(view, "lines above") {
		t.Errorf("a tall detail pushed rows off the screen:\n%s", view)
	}
	if n := len(strings.Split(view, "\n")); n != 24 {
		t.Errorf("the screen is %d lines at 80x24", n)
	}
	dim := "  " + h.m.cfg.Theme.Dim.Render(under[0])
	if !strings.Contains(h.m.View().Content, dim) {
		t.Errorf("the overflow marker is not dim: %q", under[0])
	}
	checkGolden(t, "speccheck-permission-detail-overflow-80x24.txt", view)
}
