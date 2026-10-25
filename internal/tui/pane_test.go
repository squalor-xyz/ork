// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func (l *actionLive) Answer(_ context.Context, run, text string) error {
	if text != "use local storage" && text != "qa xtrsnpy?1/" {
		return fmt.Errorf("wrong answer: %q", text)
	}
	return l.action(run, "implement")
}

func paneHarness(t *testing.T, state, pending string) *harness {
	t.Helper()
	h := actionHarness(t, state, pending)
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "Run engine") {
		t.Fatalf("Enter must open engine detail pane:\n%s", v)
	}
	return h
}

func typeAnswer(h *harness, text string) {
	for _, r := range text {
		h.deliver(press(string(r)))
	}
}

func TestPaneField(t *testing.T) {
	for _, tc := range []struct {
		name, initial, want string
		edits               []tea.Msg
	}{
		{"typed", "", "hello", []tea.Msg{tea.KeyPressMsg{Code: 'h', Text: "hello"}}},
		{"unicode", "", "café", []tea.Msg{tea.KeyPressMsg{Code: 'c', Text: "café"}}},
		{"backspace", "café", "caf", []tea.Msg{tea.KeyPressMsg{Code: tea.KeyBackspace}}},
		{"backspace-start", "a", "a", []tea.Msg{press("left"), tea.KeyPressMsg{Code: tea.KeyBackspace}}},
		{"ctrl-u", "old", "new", []tea.Msg{tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, press("new")}},
		{"left", "ac", "abc", []tea.Msg{press("left"), press("b")}},
		{"right", "abc", "abxc", []tea.Msg{press("left"), press("left"), press("right"), press("x")}},
		{"left-boundary", "a", "ba", []tea.Msg{press("left"), press("left"), press("b")}},
		{"right-boundary", "a", "ab", []tea.Msg{press("right"), press("b")}},
		{"paste", "", "pasted answer", []tea.Msg{tea.PasteMsg{Content: "pasted answer"}}},
		{"paste-at-cursor", "ac", "abc", []tea.Msg{press("left"), tea.PasteMsg{Content: "b"}}},
		{"paste-newlines", "", "firstsecondthird", []tea.Msg{tea.PasteMsg{Content: "first\r\nsecond\nthird\r"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := paneHarness(t, "paused", "question")
			typeAnswer(h, tc.initial)
			for _, msg := range tc.edits {
				h.deliver(msg)
			}
			if v := h.view(); !strings.Contains(v, "ork answer engine "+tc.want+"\n") {
				t.Fatalf("field %s: want live command with %q:\n%s", tc.name, tc.want, v)
			}
		})
	}
}

func TestPaneStates(t *testing.T) {
	for _, tc := range []struct{ state, pending, want string }{
		{"paused", "question", "Answer:"},
		{"paused", "shape", "t retry"},
		{"paused", "lost", "t retry"},
		{"paused", "interrupted", "t retry"},
		{"paused", "error", "t retry"},
		{"paused", "", "t retry"},
		{"stopped", "question", "Stopped; relaunch, or clear with: ork dismiss engine"},
		{"awaiting-approval", "", "a approve"},
		{"implement", "", "x stop"},
	} {
		t.Run(tc.state+"/"+tc.pending, func(t *testing.T) {
			h := paneHarness(t, tc.state, tc.pending)
			if v := h.view(); !strings.Contains(v, tc.want) {
				t.Fatalf("pane state action missing %q:\n%s", tc.want, v)
			}
		})
	}
}

func TestPaneReasonAndPendingAreIndependent(t *testing.T) {
	for _, pending := range []string{"question", "shape"} {
		t.Run(pending, func(t *testing.T) {
			h := actionHarness(t, "paused", pending)
			s := h.src.Snapshot(context.Background(), proj("ork", false))
			s.EngineRuns[0].Wait = "question: Keep EVERY word.\nIncluding the last line."
			h.src.set(s)
			h.m.apply(snapshotMsg{name: "ork", snap: s})
			h.deliver(press("enter"))
			v := h.view()
			if !strings.Contains(v, "Run engine") || !strings.Contains(v, "question: Keep EVERY word.") || !strings.Contains(v, "Including the last line.") {
				t.Fatalf("pane lost full Wait reason:\n%s", v)
			}
			if strings.Contains(v, "Answer:") != (pending == "question") {
				t.Fatalf("pane inferred kind from Wait instead of pending:\n%s", v)
			}
		})
	}
}

func TestPaneEscape(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	h.deliver(press("esc"))
	if v := h.view(); strings.Contains(v, "Run engine") || !strings.Contains(v, "> Needs you") {
		t.Fatalf("Escape must close pane without switching project:\n%s", v)
	}
}

func TestPaneNonEngineEnter(t *testing.T) {
	for _, kind := range []string{"decision", "question"} {
		t.Run(kind, func(t *testing.T) {
			h := paneHarness(t, "paused", "question")
			h.deliver(press("esc"))
			s := snap(proj("ork", false), nil, nil)
			switch kind {
			case "decision":
				s.Open.Value = []adjcli.Record{openRec(1, "owner.choice", "Choose storage")}
			case "question":
				s.Items.Value = []slicercli.Item{item("S1", "Choose storage", "needs-owner")}
			}
			h.m.apply(snapshotMsg{name: "ork", snap: s})
			h.deliver(press("enter"))
			if v := h.view(); strings.Contains(v, "Run ") || !strings.Contains(v, "> ork") {
				t.Fatalf("non-engine Enter must switch project without pane:\n%s", v)
			}
		})
	}
}

func TestPaneAnswerLiveCommand(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	typeAnswer(h, "use local storage")
	if v := h.view(); !strings.Contains(v, "ork answer engine use local storage") || strings.Contains(v, "[y/N]") {
		t.Fatalf("answer command must follow field without confirmation:\n%s", v)
	}
}
func TestPaneAnswerSubmitRefresh(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	typeAnswer(h, "use local storage")
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "S22  implement") || !strings.Contains(v, "ork answer engine use local storage: done") || strings.Contains(v, "Answer:") {
		t.Fatalf("answer must submit immediately and refresh pane and row:\n%s", v)
	}
}
func TestPaneEmptyAnswer(t *testing.T) {
	for _, text := range []string{"", "   "} {
		t.Run(fmt.Sprintf("%q", text), func(t *testing.T) {
			h := paneHarness(t, "paused", "question")
			typeAnswer(h, text)
			_, cmd := h.m.Update(press("enter"))
			if v := h.view(); cmd != nil || !strings.Contains(v, "Answer must not be empty") {
				t.Fatalf("empty answer must be refused:\n%s", v)
			}
		})
	}
}
func TestPaneInputOwnsKeys(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	for _, r := range "qa xtrsnpy?1/" {
		for _, msg := range h.send(press(string(r))) {
			if _, ok := msg.(tea.QuitMsg); ok {
				t.Fatal("typing q quit the TUI")
			}
		}
	}
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "ork answer engine qa xtrsnpy?1/: done") || !strings.Contains(v, "S22  implement") {
		t.Fatalf("field leaked a global key or changed submitted text:\n%s", v)
	}
}

// The answer field takes j, k and the arrows as text, and tab and shift+tab type
// nothing. The cursor and the project stay where they are (ui.row-keys, ui.views).
func TestAnswerFieldTakesRowKeysAsText(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	for _, k := range []string{"down", "j", "up", "k", "tab", "shift+tab"} {
		h.deliver(press(k))
	}
	if v := h.view(); !strings.Contains(v, "Run engine") || !strings.Contains(v, "ork answer engine jk\n") {
		t.Fatalf("j and k did not reach the answer field:\n%s", v)
	}
	if h.m.active != -1 || h.m.cursor != 0 {
		t.Fatalf("the keys moved the project to %d or the cursor to %d", h.m.active, h.m.cursor)
	}
}

func TestPaneAnswerFailure(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	h.m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	typeAnswer(h, "use local storage")
	live := h.m.cfg.Live.(*actionLive)
	live.err = &ipc.Error{Code: "answer_failed", Message: "Wait: Owner's exact failure."}
	h.deliver(press("enter"))
	if v := plainView(h.m); !strings.Contains(v, "answer_failed: Wait: Owner's exact failure.") {
		t.Fatalf("answer error changed:\n%s", v)
	}
}

func TestPaneFailedAnswerClearsFlight(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	typeAnswer(h, "use local storage")
	live := h.m.cfg.Live.(*actionLive)
	live.err = &ipc.Error{Code: "answer_failed", Message: "Exact failure."}
	h.deliver(press("enter"))
	live.err = nil
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "S22  implement") {
		t.Fatalf("failed answer retained in-flight entry:\n%s", v)
	}
}
func TestPaneAnswerPending(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	typeAnswer(h, "use local storage")
	h.m.Update(press("enter")) // hold the returned command
	_, cmd := h.m.Update(press("enter"))
	if v := h.view(); cmd != nil || !strings.Contains(v, "ork answer engine use local storage is pending") {
		t.Fatalf("duplicate answer was not blocked:\n%s", v)
	}
}
func TestPaneAnswerOtherRun(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	typeAnswer(h, "use local storage")
	h.m.Update(press("enter")) // this run's call remains in flight
	h.deliver(press("esc"))
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append(s.EngineRuns, source.EngineRun{ID: "other", Item: "S23", State: "awaiting-approval"})
	h.src.set(s)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.deliver(press("n"))
	h.deliver(press("a"))
	if v := h.view(); !strings.Contains(v, "S23  commit") {
		t.Fatalf("pending answer blocked a different run:\n%s", v)
	}
}
func TestPaneRetryConfirm(t *testing.T) {
	h := paneHarness(t, "paused", "shape")
	h.deliver(press("t"))
	if v := h.view(); !strings.Contains(v, "ork retry engine? [y/N] (Enter is No)") {
		t.Fatalf("pane retry lost confirmation:\n%s", v)
	}
	h.deliver(press("y"))
	if v := h.view(); !strings.Contains(v, "S22  implement") {
		t.Fatalf("pane retry failed to refresh:\n%s", v)
	}
}
func TestPaneStoppedReadOnly(t *testing.T) {
	h := paneHarness(t, "stopped", "question")
	for _, k := range []string{"a", "x", "t", "enter"} {
		h.deliver(press(k))
	}
	if v := h.view(); !strings.Contains(v, "Stopped; relaunch, or clear with: ork dismiss engine") || strings.Contains(v, "ork stop") || strings.Contains(v, "ork retry") || strings.Contains(v, "ork answer") || strings.Contains(v, "ork approve") {
		t.Fatalf("stopped pane offered an action:\n%s", v)
	}
}
func TestPaneKeepsRunAfterReorder(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	typeAnswer(h, "use local storage")
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun{{ID: "other", Item: "S23", State: "paused", Pending: "question", Wait: "Other question"}}, s.EngineRuns...)
	h.src.set(s)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "S22  implement") || !strings.Contains(v, "S23  paused") {
		t.Fatalf("pane answer changed target after reorder:\n%s", v)
	}
}
func TestPaneMessageCursorMove(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	h.deliver(press("esc"))
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append(s.EngineRuns, source.EngineRun{ID: "other", Item: "S23", State: "paused"})
	h.src.set(s)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.deliver(press("a"))
	h.deliver(press("n"))
	if v := h.view(); strings.Contains(v, "approve needs") {
		t.Fatalf("previous row message lingered after cursor move:\n%s", v)
	}
}
func TestPaneLateResult(t *testing.T) {
	for _, tc := range []struct{ verb, key, state, pending string }{
		{"answer", "enter", "paused", "question"},
		{"approve", "a", "awaiting-approval", ""},
		{"retry", "t", "paused", "shape"},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			h := paneHarness(t, tc.state, tc.pending)
			if tc.verb == "answer" {
				typeAnswer(h, "use local storage")
			} else {
				h.deliver(press("esc"))
			}
			// Keep row order unchanged on completion, so a later snapshot cannot
			// hide an incorrectly displayed result by moving the selected row.
			h.m.cfg.Live.(*actionLive).err = &ipc.Error{Code: "action_failed", Message: "Exact failure."}
			_, cmd := h.m.Update(press(tc.key))
			if tc.verb == "retry" {
				_, cmd = h.m.Update(press("y"))
			}
			if tc.verb == "answer" {
				h.deliver(press("esc"))
			}
			s := h.src.Snapshot(context.Background(), proj("ork", false))
			s.EngineRuns = append(s.EngineRuns, source.EngineRun{ID: "other", Item: "S23", State: tc.state})
			h.src.set(s)
			h.m.apply(snapshotMsg{name: "ork", snap: s})
			h.deliver(press("n"))
			h.deliver(cmd())
			if v := h.view(); strings.Contains(v, "ork "+tc.verb+" engine") {
				t.Fatalf("late result appeared on another row:\n%s", v)
			}
		})
	}
}

func TestPaneHelp(t *testing.T) {
	h := paneHarness(t, "paused", "shape")
	h.deliver(press("esc"))
	h.deliver(press("?"))
	for _, want := range []string{"enter open run, question, item or decision; other rows select project", "esc close run, question, item, decision, help or message", "enter (answer) submit; no confirmation", "pgup/pgdown scroll run tree and reason"} {
		if v := h.view(); !strings.Contains(v, want) {
			t.Errorf("missing pane help %q:\n%s", want, v)
		}
	}
}
func TestPaneGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, pending := range []string{"question", "shape", "stopped"} {
			t.Run(fmt.Sprintf("%s/%t", pending, mono), func(t *testing.T) {
				state := "paused"
				if pending == "stopped" {
					state = "stopped"
				}
				h := paneHarness(t, state, pending)
				h.m.cfg.Theme = NewTheme(mono)
				if pending == "question" {
					typeAnswer(h, "use local storage")
				}
				checkGolden(t, fmt.Sprintf("pane-%s-mono-%t.txt", pending, mono), h.m.View().Content)
			})
		}
	}
}

type answerCall interface {
	Answer(context.Context, string, string) error
}

func TestPaneTypedAnswerConnectFailure(t *testing.T) {
	d := &Daemon{Connect: func(context.Context) (*daemon.Client, error) { return nil, errors.New("exact connect failure") }}
	a, ok := any(d).(answerCall)
	if !ok {
		t.Fatal("daemon adapter lacks typed Answer")
	}
	if err := a.Answer(context.Background(), "engine", "use local storage"); err == nil || err.Error() != "exact connect failure" {
		t.Fatalf("connect error changed: %v", err)
	}
}
func TestPaneTypedAnswerWire(t *testing.T) {
	if _, ok := any(&Daemon{}).(answerCall); !ok {
		t.Fatal("daemon adapter lacks typed Answer")
	}
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			d, _, _, _ := requestServer(t, func(_ context.Context, method string, params json.RawMessage) (any, error) {
				if method != "run.answer" || string(params) != `{"run":"engine","text":"use local storage"}` {
					return nil, fmt.Errorf("wrong request: %s %s", method, params)
				}
				if fail {
					return nil, &ipc.Error{Code: "answer_failed", Message: "Exact failure."}
				}
				return map[string]string{"id": "engine"}, nil
			})
			err := any(d).(answerCall).Answer(context.Background(), "engine", "use local storage")
			if fail {
				var e *ipc.Error
				if !errors.As(err, &e) || e.Code != "answer_failed" || e.Message != "Exact failure." {
					t.Fatalf("wire error changed: %v", err)
				}
			} else if err != nil {
				t.Fatalf("typed answer failed: %v", err)
			}
		})
	}
}

func TestPaneLongReasonScroll(t *testing.T) {
	h := actionHarness(t, "paused", "question")
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns[0].Wait = strings.Repeat("Keep this reason line.\n", 60) + "FINAL reason line."
	h.src.set(s)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "Run engine") {
		t.Fatalf("long reason did not open pane:\n%s", v)
	}
	for i := 0; i < 70; i++ {
		h.deliver(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if v := h.view(); !strings.Contains(v, "FINAL reason line.") || !strings.Contains(v, "Answer:") {
		t.Fatalf("full reason must remain reachable with answer field visible:\n%s", v)
	}
	for i := 0; i < 70; i++ {
		h.deliver(tea.KeyPressMsg{Code: tea.KeyPgUp})
	}
	if v := h.view(); !strings.Contains(v, "Keep this reason line.") || strings.Contains(v, "FINAL reason line.") {
		t.Fatalf("page up must restore reason start:\n%s", v)
	}
}

func TestActionResultSurvivesRowRefresh(t *testing.T) {
	for _, tc := range []struct{ verb, key, state string }{
		{"approve", "a", "awaiting-approval"},
		{"retry", "t", "paused"},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			h := actionHarness(t, tc.state, "shape")
			s := h.src.Snapshot(context.Background(), proj("ork", false))
			s.EngineRuns = append(s.EngineRuns, source.EngineRun{ID: "other", Item: "S23", State: tc.state})
			h.src.set(s)
			h.m.apply(snapshotMsg{name: "ork", snap: s})
			h.deliver(press(tc.key))
			if tc.verb == "retry" {
				h.deliver(press("y"))
			}
			if v := h.view(); !strings.Contains(v, "ork "+tc.verb+" engine: done") {
				t.Fatalf("refresh lost completed action message:\n%s", v)
			}
		})
	}
}

func TestPaneShortTerminalKeepsError(t *testing.T) {
	for _, height := range []int{9, 10} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			h := paneHarness(t, "paused", "question")
			h.m.Update(tea.WindowSizeMsg{Width: 140, Height: height})
			typeAnswer(h, "use local storage")
			h.m.cfg.Live.(*actionLive).err = &ipc.Error{Code: "answer_failed", Message: "boom"}
			h.deliver(press("enter"))
			if v := plainView(h.m); !strings.Contains(v, "answer_failed: boom") {
				t.Fatalf("short terminal hid answer error:\n%s", v)
			}
		})
	}
}

func TestPaneSuccessfulAnswerClearsField(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	typeAnswer(h, "use local storage")
	h.deliver(press("enter"))
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
	s.EngineRuns[0].State = "paused"
	s.EngineRuns[0].Pending = "question"
	s.EngineRuns[0].Wait = "Next question"
	h.src.set(s)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "Answer must not be empty") {
		t.Fatalf("new question reused the successful answer:\n%s", v)
	}
}

func TestPaneReasonCarriageReturns(t *testing.T) {
	for _, separator := range []string{"\r\n", "\r"} {
		t.Run(fmt.Sprintf("%q", separator), func(t *testing.T) {
			h := paneHarness(t, "paused", "question")
			s := h.src.Snapshot(context.Background(), proj("ork", false))
			s.EngineRuns[0].Wait = "first" + separator + "second"
			h.m.apply(snapshotMsg{name: "ork", snap: s})
			lines := strings.Join(h.m.paneLines(72, 20, ""), "\n")
			if !strings.Contains(lines, "  first\n  second") {
				t.Fatalf("reason did not normalize line breaks: %q", lines)
			}
		})
	}
}

func TestPaneScrollMarkerIndent(t *testing.T) {
	for _, mono := range []bool{false, true} {
		for _, offset := range []int{0, 1} {
			t.Run(fmt.Sprintf("%t/%d", mono, offset), func(t *testing.T) {
				h := paneHarness(t, "paused", "question")
				h.m.cfg.Theme = NewTheme(mono)
				s := h.src.Snapshot(context.Background(), proj("ork", false))
				s.EngineRuns[0].Wait = strings.Repeat("reason\n", 60)
				h.m.apply(snapshotMsg{name: "ork", snap: s})
				h.m.pane.offset = offset
				lines := sgr.ReplaceAllString(strings.Join(h.m.paneLines(72, 12, ""), "\n"), "")
				marker := "  +"
				if !strings.Contains("\n"+lines, "\n"+marker) || strings.Contains(lines, "    +") {
					t.Fatalf("scroll marker indent differs from reason: %q", lines)
				}
			})
		}
	}
}

func TestPaneFieldWideCursor(t *testing.T) {
	var f textField
	f.insert("abc界")
	f.update(press("left"))
	if v := sgr.ReplaceAllString(f.view(4, NewTheme(true)), ""); v != "bc界" {
		t.Fatalf("wide cursor was clipped: %q", v)
	}
}

func TestOverviewRowFlattensMultilineWait(t *testing.T) {
	for _, separator := range []string{"\n", "\r\n", "\r"} {
		t.Run(fmt.Sprintf("%q", separator), func(t *testing.T) {
			h := actionHarness(t, "paused", "question")
			h.m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
			s := h.src.Snapshot(context.Background(), proj("ork", false))
			s.EngineRuns[0].Wait = "alpha" + separator + "beta"
			h.m.apply(snapshotMsg{name: "ork", snap: s})
			var row string
			for _, line := range strings.Split(plainView(h.m), "\n") {
				if strings.Contains(line, "S22") {
					row = line
					break
				}
			}
			if !strings.Contains(row, "alpha beta") {
				t.Fatalf("Overview row must hold the Wait text on one line, got row %q", row)
			}
		})
	}
}

func TestPaneFieldViewFitsWidth(t *testing.T) {
	var f textField
	f.insert("abcdefghij")
	for range 8 {
		f.update(press("left"))
	}
	v := sgr.ReplaceAllString(f.view(5, NewTheme(true)), "")
	if v != "abcde" {
		t.Fatalf("text after the cursor must be cut to the field width: %q", v)
	}
}

func TestPaneAdjToggle(t *testing.T) {
	for _, tc := range []struct {
		key string
		adj bool
	}{{"storage.engine", true}, {"", true}, {"storage.engine", false}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			h := paneHarness(t, "paused", "question")
			h.m.byName["ork"].reg.Adj = tc.adj
			h.m.byName["ork"].snap.EngineRuns[0].Key = tc.key
			typeAnswer(h, "a")
			ctrlA := tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
			h.deliver(ctrlA)
			if h.m.pane.adj != (tc.adj && tc.key != "") || h.m.pane.input.Value() != "a" {
				t.Fatalf("pane: %+v", h.m.pane)
			}
			h.deliver(ctrlA)
			if h.m.pane.adj || h.m.pane.input.Value() != "a" {
				t.Fatalf("pane: %+v", h.m.pane)
			}
			h.deliver(ctrlA)
			h.deliver(press("esc"))
			h.deliver(press("enter"))
			if h.m.pane.adj {
				t.Fatal("toggle persisted")
			}
		})
	}
}

func (l *actionLive) AnswerAction(ctx context.Context, a daemon.RunAction) (string, error) {
	l.answerRequest = a
	return l.answerNotice, l.Answer(ctx, a.Run, a.Text)
}

func TestAnswerAdjErrorKeepsTextAndDoesNotResume(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	h.m.byName["ork"].reg.Adj = true
	h.m.byName["ork"].snap.EngineRuns[0].Key = "storage.engine"
	t.Setenv("ADJ_DECIDED_BY", "caller")
	typeAnswer(h, "use local storage")
	h.deliver(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	live := h.m.cfg.Live.(*actionLive)
	live.err = &ipc.Error{Code: "io", Message: "disk full verbatim"}
	h.deliver(press("enter"))
	if live.answerRequest != (daemon.RunAction{Run: "engine", Text: "use local storage", Adj: true, By: "caller"}) {
		t.Fatalf("request: %+v", live.answerRequest)
	}
	if h.m.pane.input.Value() != "use local storage" || !strings.Contains(h.m.actionStatus, "io: disk full verbatim") {
		t.Fatalf("text/status: %s %s", h.m.pane.input.Value(), h.m.actionStatus)
	}
	if h.m.byName["ork"].snap.EngineRuns[0].State != "paused" {
		t.Fatal("resumed")
	}
	live.err = nil
	live.answerNotice = "storage.engine already decided as #21; not changed"
	h.deliver(press("enter"))
	if !strings.Contains(h.m.actionStatus, "storage.engine already decided as #21; not changed") {
		t.Fatalf("notice: %s", h.m.actionStatus)
	}
}

func TestPaneAdjAnswerWire(t *testing.T) {
	d, _, _, _ := requestServer(t, func(_ context.Context, method string, params json.RawMessage) (any, error) {
		var a daemon.RunAction
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		if method != "run.answer" || a != (daemon.RunAction{Run: "engine", Text: "yes", Adj: true, By: "caller"}) {
			return nil, fmt.Errorf("wrong request: %s %s", method, params)
		}
		return daemon.RunReply{Message: "storage.engine already decided as #21; not changed"}, nil
	})
	message, err := d.AnswerAction(context.Background(), daemon.RunAction{Run: "engine", Text: "yes", Adj: true, By: "caller"})
	if err != nil || message != "storage.engine already decided as #21; not changed" {
		t.Fatalf("reply: %s %v", message, err)
	}
}

func TestPaneAdjGoldens(t *testing.T) {
	t.Setenv("ADJ_DECIDED_BY", "owner")
	for _, mode := range []string{"off", "on", "no-key", "no-adj"} {
		for _, mono := range []bool{true, false} {
			t.Run(fmt.Sprint(mode, mono), func(t *testing.T) {
				h := paneHarness(t, "paused", "question")
				h.m.cfg.Theme = NewTheme(mono)
				h.m.byName["ork"].reg.Adj = mode != "no-adj"
				if mode != "no-key" {
					h.m.byName["ork"].snap.EngineRuns[0].Key = "storage.engine"
				}
				if mode == "on" {
					h.deliver(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
				}
				typeAnswer(h, "use local storage")
				checkGolden(t, fmt.Sprintf("pane-question-adj-%s-mono-%t.txt", mode, mono), h.m.View().Content)
			})
		}
	}
}

func TestQuestionsEngineAdjToggle(t *testing.T) {
	h := actionHarness(t, "paused", "question")
	h.m.view = questionsView
	h.m.byName["ork"].reg.Adj = true
	h.m.byName["ork"].snap.EngineRuns[0].Key = "storage.engine"
	h.deliver(press("enter"))
	h.deliver(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if h.m.pane == nil || !h.m.pane.adj {
		t.Fatal("Questions did not use the adj answer pane")
	}
}

func TestPaneAdjFilingStates(t *testing.T) {
	for _, tc := range []struct {
		name, key, want string
		adj, on         bool
	}{
		{"off", "a.b", "[ ] file answer in adj as a.b  ctrl+a toggles", true, false},
		{"on", "a.b", "[x] file answer in adj as a.b  ctrl+a toggles", true, true},
		{"no-adj", "a.b", "adj filing off: no adj in this project", false, false},
		{"no-key", "", "adj filing off: question has no decision key", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := paneHarness(t, "paused", "question")
			t.Setenv("ADJ_DECIDED_BY", "owner")
			h.m.byName["ork"].reg.Adj = tc.adj
			h.m.byName["ork"].snap.EngineRuns[0].Key = tc.key
			h.m.pane.adj = tc.on
			lines := h.m.paneLines(100, 20, "")
			got := strings.Join(lines, "\n")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("filing line missing %q: %s", tc.want, got)
			}
			if tc.on && !strings.Contains(got, h.m.cfg.Theme.Heading.Render(tc.want)) {
				t.Fatalf("on toggle lacks heading style: %q", got)
			}
			if !tc.adj || tc.key == "" {
				if strings.Contains(got, "ctrl+a") || strings.Contains(got, "[ ]") {
					t.Fatalf("disabled toggle offers action: %s", got)
				}
			}
		})
	}
}

func TestPaneShortTerminalKeepsOnToggle(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	t.Setenv("ADJ_DECIDED_BY", "owner")
	h.m.byName["ork"].reg.Adj = true
	h.m.byName["ork"].snap.EngineRuns[0].Key = "a.b"
	h.m.pane.adj = true
	got := strings.Join(h.m.paneLines(100, 3, "result"), "\n")
	if !strings.Contains(got, "[x]") || !strings.Contains(got, "Answer:") || !strings.Contains(got, "result") || strings.Contains(got, "enter submit") {
		t.Fatalf("priority lost on toggle: %s", got)
	}
	h.m.pane.adj = false
	got = strings.Join(h.m.paneLines(100, 3, "result"), "\n")
	if strings.Contains(got, "[ ]") || !strings.Contains(got, "enter submit") {
		t.Fatalf("off toggle has wrong priority: %s", got)
	}
}

func TestPaneAdjMissingAttributionWarning(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	t.Setenv("ADJ_DECIDED_BY", "")
	h.m.cfg.Theme = NewTheme(false)
	h.m.byName["ork"].reg.Adj = true
	h.m.byName["ork"].snap.EngineRuns[0].Key = "a.b"
	h.m.pane.adj = true
	got := strings.Join(h.m.paneLines(120, 20, ""), "\n")
	if !strings.Contains(got, h.m.cfg.Theme.Warn.Render("ADJ_DECIDED_BY unset")) {
		t.Fatalf("missing styled warning: %q", got)
	}
}

func TestPaneAdjDimEveryWrappedLine(t *testing.T) {
	h := paneHarness(t, "paused", "question")
	h.m.cfg.Theme = NewTheme(false)
	got := strings.Join(h.m.paneLines(26, 30, ""), "\n")
	for _, line := range wrapText("adj filing off: no adj in this project", 24) {
		if !strings.Contains(got, indent+h.m.cfg.Theme.Dim.Render(line)) {
			t.Fatalf("wrapped line %q not dimmed: %q", line, got)
		}
	}
}

func TestPaneAdjNarrowGoldens(t *testing.T) {
	for _, mode := range []string{"no-adj", "no-key", "on", "on-unset"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("ADJ_DECIDED_BY", "owner")
			if mode == "on-unset" {
				t.Setenv("ADJ_DECIDED_BY", "")
			}
			h := paneHarness(t, "paused", "question")
			h.m.cfg.Theme = NewTheme(false)
			h.m.byName["ork"].reg.Adj = mode != "no-adj"
			if mode != "no-key" {
				h.m.byName["ork"].snap.EngineRuns[0].Key = "storage.engine"
			}
			h.m.pane.adj = strings.HasPrefix(mode, "on")
			lines := h.m.paneLines(40, 20, "")
			for _, line := range lines {
				if lipgloss.Width(line) > 40 {
					t.Fatalf("overwide line: %q", line)
				}
			}
			checkGolden(t, "pane-question-adj-narrow-"+mode+".txt", strings.Join(lines, "\n"))
		})
	}
}

func TestPanePlainAdjErrorsKeepText(t *testing.T) {
	for _, verb := range []string{"ask", "decide"} {
		t.Run(verb, func(t *testing.T) {
			h := paneHarness(t, "paused", "question")
			typeAnswer(h, "use local storage")
			want := "internal: adj: exit 1: adj " + verb + ": record 5 is not open"
			h.m.cfg.Live.(*actionLive).err = &ipc.Error{Code: "internal", Message: "adj: exit 1: adj " + verb + ": record 5 is not open"}
			h.deliver(press("enter"))
			if h.m.actionStatus != "ork answer engine use local storage: "+want || h.m.pane.input.Value() != "use local storage" {
				t.Fatalf("text/status: %q %q", h.m.pane.input.Value(), h.m.actionStatus)
			}
		})
	}
}

func TestPaneAdjOutcomeStatus(t *testing.T) {
	for _, notice := range []string{"decided in adj as #21", "a.b already decided as #21; not changed"} {
		t.Run(notice, func(t *testing.T) {
			h := paneHarness(t, "paused", "question")
			typeAnswer(h, "use local storage")
			h.m.cfg.Live.(*actionLive).answerNotice = notice
			h.deliver(press("enter"))
			if h.m.actionStatus != "ork answer engine use local storage: done; "+notice {
				t.Fatalf("status: %q", h.m.actionStatus)
			}
		})
	}
}

func TestPaneShortNonQuestionStatusPriority(t *testing.T) {
	for _, state := range []string{"paused", "stopped", "awaiting-approval", "implement", "committed"} {
		t.Run(state, func(t *testing.T) {
			h := actionHarness(t, state, "shape")
			h.m.pane = &runPane{run: "engine", project: "ork"}
			lines := h.m.paneLines(100, 2, "first status\nlast status")
			if len(lines) != 2 || !strings.Contains(lines[0], "first status") || !strings.Contains(lines[1], "last status") {
				t.Fatalf("status priority: %q", lines)
			}
		})
	}
}
