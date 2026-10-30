// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

// The approval card shows the renders the UI pass wrote (S121, *proposed*;
// adj 165). ork holds no render code: the card shows the role's text.

// rendersText is what run.render answers, by render name. The last render
// holds terminal escapes on purpose: the daemon strips them, and the pane must
// not trust that.
func rendersText() map[string]string {
	var long []string
	for i := 1; i <= 40; i++ {
		long = append(long, fmt.Sprintf("row %02d of the 120x40 view", i))
	}
	long[1] = "wide " + strings.Repeat("W", 150)
	return map[string]string{
		"launch-80x24.txt":  "LAUNCH-80 first line\nsecond line",
		"launch-120x40.txt": strings.Join(long, "\n"),
		"launch-ascii.txt":  "+--ascii--+\n\x1b[31mred\x1b[0m\x1b]0;PWNED\x07 ok",
	}
}

// rendersCard lists the renders by name and size; the card holds no text.
func rendersCard() *runrec.ApprovalCard {
	card := approvalCard(openChecksThree()...)
	card.UIRound = 1
	text := rendersText()
	for _, name := range []string{"launch-80x24.txt", "launch-120x40.txt", "launch-ascii.txt"} {
		card.Renders = append(card.Renders, runrec.Render{Name: name, Size: len(text[name])})
	}
	return card
}

// rendersHarness opens the pane of a run whose card lists three renders and
// whose daemon answers run.render.
func rendersHarness(t *testing.T) *harness {
	t.Helper()
	h := cardHarness(t, rendersCard())
	h.live().renders = rendersText()
	return h
}

func TestApprovalCardShowsRenders(t *testing.T) {
	h := rendersHarness(t)
	v := h.at(80, 24)
	for _, want := range []string{"Renders: 3 from the UI pass (round 1)", "esc close pane"} { // the key line wraps
		if !strings.Contains(v, want) {
			t.Errorf("the card lacks %q at 80x24:\n%s", want, v)
		}
	}
	if strings.Contains(v, "LAUNCH-80") {
		t.Errorf("a render shows before the owner asks:\n%s", v)
	}

	// v opens the first render and takes the room of the card.
	h.deliver(press("v"))
	v = h.at(80, 24)
	for _, want := range []string{"Render 1 of 3: launch-80x24.txt", "LAUNCH-80 first line", "second line", "v next render; s sidebar", "esc back to card"} {
		if !strings.Contains(v, want) {
			t.Errorf("render 1 lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "Human checks") {
		t.Errorf("the card's checks stay under a render:\n%s", v)
	}

	// The next render is wider than the pane: it is cut with a cue, and the
	// rows below scroll.
	h.deliver(press("v"))
	v = h.at(80, 24)
	if !strings.Contains(v, "Render 2 of 3: launch-120x40.txt") || !strings.Contains(v, "row 01 of the 120x40 view") {
		t.Errorf("render 2:\n%s", v)
	}
	if !strings.Contains(v, "WWW...") || strings.Contains(v, strings.Repeat("W", 100)) {
		t.Errorf("a wide row is not cut with a cue:\n%s", v)
	}
	for _, line := range strings.Split(v, "\n") {
		if w := utf8.RuneCountInString(line); w > 80 {
			t.Errorf("a line is %d columns at 80:\n%s", w, line)
		}
	}
	if !strings.Contains(v, "more lines") {
		t.Errorf("hidden rows are not said:\n%s", v)
	}
	h.deliver(press("pgdown"))
	if v = h.at(80, 24); !strings.Contains(v, "lines above") {
		t.Errorf("pgdown does not scroll the render:\n%s", v)
	}

	// s hides the sidebar, so a render gets the whole width.
	narrow := strings.Count(v, "W")
	h.deliver(press("s"))
	if v = h.at(80, 24); strings.Contains(v, "Needs you      1") || strings.Count(v, "W") <= narrow {
		t.Errorf("s must hide the sidebar and widen the render (%d W before):\n%s", narrow, v)
	}
	h.deliver(press("s"))

	// A terminal escape in a render never reaches the screen.
	h.deliver(press("v"))
	v = h.at(80, 24)
	if !strings.Contains(v, "Render 3 of 3: launch-ascii.txt") || !strings.Contains(v, "+--ascii--+") {
		t.Errorf("render 3:\n%s", v)
	}
	if raw := h.m.View().Content; strings.Contains(raw, "\x1b]0;") || strings.Contains(raw, "\x07") {
		t.Errorf("an escape sequence reaches the screen:\n%q", raw)
	}
	for _, left := range []string{"[31m", "[0m", "PWNED", "]0;"} {
		if strings.Contains(v, left) {
			t.Errorf("the rest of an escape sequence (%q) shows:\n%s", left, v)
		}
	}
	if !strings.Contains(v, "red ok") {
		t.Errorf("the text around the sequences is lost:\n%s", v)
	}
	// On the last render the key line says where v goes.
	if !strings.Contains(hintLine(v), "v back to card; s sidebar") || strings.Contains(hintLine(v), "v next render") {
		t.Errorf("the last render's key line:\n%s", v)
	}

	// While a render shows, the card's keys do nothing: the owner cannot mark
	// a check or approve what the footer does not show.
	h.deliver(press("1"))
	h.deliver(press("a"))
	if l := h.live(); len(l.marks) != 0 || l.approves != 0 {
		t.Errorf("a card key acted under a render: marks %v, approves %d", l.marks, l.approves)
	}

	// After the last render, v goes back to the card; esc goes back at once.
	h.deliver(press("v"))
	v = h.at(80, 24)
	if strings.Contains(v, "Render 3 of 3") || !strings.Contains(v, "Human checks") {
		t.Errorf("v after the last render must show the card:\n%s", v)
	}
	h.deliver(press("1")) // the card's keys act again
	if l := h.live(); len(l.marks) != 1 {
		t.Errorf("a digit on the card after the renders marked %v", l.marks)
	}
	h.deliver(press("v"))
	h.deliver(press("esc"))
	v = h.at(80, 24)
	if !strings.Contains(v, "Human checks") || !strings.Contains(v, "Run engine") {
		t.Errorf("esc must go back to the card, not close the pane:\n%s", v)
	}
	h.deliver(press("esc"))
	if strings.Contains(h.at(80, 24), "Gate PASS") {
		t.Errorf("a second esc must close the pane:\n%s", h.at(80, 24))
	}
}

func TestApprovalCardRendersAtWidth120(t *testing.T) {
	h := rendersHarness(t)
	h.deliver(press("v"))
	h.deliver(press("v"))
	v := h.at(120, 40)
	// The pane has the whole body, but a 40-row render and its footer still
	// exceed 40 terminal rows; it scrolls and says how many rows are hidden.
	if !strings.Contains(v, "row 01 of the 120x40 view") || !strings.Contains(v, "more lines") {
		t.Errorf("the 120x40 render must start at its top and say what is hidden:\n%s", v)
	}
	if !strings.Contains(v, "WWW...") || strings.Contains(v, strings.Repeat("W", 120)) {
		t.Errorf("a wide row is cut at the pane edge:\n%s", v)
	}
	for range 20 { // one line a press
		h.deliver(press("pgdown"))
		h.at(120, 40)
	}
	if v = h.at(120, 40); !strings.Contains(v, "row 40 of the 120x40 view") {
		t.Errorf("pgdown must reach the last row:\n%s", v)
	}
}

func TestApprovalCardRendersStates(t *testing.T) {
	// The UI pass ran and wrote none: the card says so, and v does nothing.
	card := approvalCard(openChecksThree()...)
	card.UIRound = 1
	h := cardHarness(t, card)
	v := h.view()
	if !strings.Contains(v, "Renders: the UI pass (round 1) wrote none") {
		t.Errorf("the missing renders are not said:\n%s", v)
	}
	if strings.Contains(v, "v view") {
		t.Errorf("the key line offers v with nothing to show:\n%s", v)
	}
	h.deliver(press("v"))
	if v = h.view(); strings.Contains(v, "Render 1") || !strings.Contains(v, "Human checks") {
		t.Errorf("v with no renders must leave the card:\n%s", v)
	}

	// No UI pass: the card does not mention renders.
	h = cardHarness(t, approvalCard(openChecksThree()...))
	if v = h.view(); strings.Contains(v, "Renders") || strings.Contains(v, "v view") {
		t.Errorf("a card without a UI pass mentions renders:\n%s", v)
	}

	// Dropped files are counted.
	card = rendersCard()
	card.RendersDropped = 2
	h = cardHarness(t, card)
	if v = h.view(); !strings.Contains(v, "2 more not kept") {
		t.Errorf("dropped renders are not said:\n%s", v)
	}
}

// The card can change under an open render (a fix round started from another
// TUI): a render that is no longer there is never drawn or kept as a mode.
func TestApprovalCardRendersSurviveACardSwap(t *testing.T) {
	for name, swap := range map[string]func(*runrec.ApprovalCard) *runrec.ApprovalCard{
		"fewer renders": func(c *runrec.ApprovalCard) *runrec.ApprovalCard {
			n := *c
			n.Renders = c.Renders[:1]
			return &n
		},
		"no renders": func(c *runrec.ApprovalCard) *runrec.ApprovalCard {
			n := *c
			n.Renders, n.UIRound = nil, 0
			return &n
		},
		"no card": func(*runrec.ApprovalCard) *runrec.ApprovalCard { return nil },
		"a new round": func(c *runrec.ApprovalCard) *runrec.ApprovalCard {
			n := *c
			n.Round++
			return &n
		},
	} {
		t.Run(name, func(t *testing.T) {
			card := rendersCard()
			h := cardHarness(t, card)
			h.live().renders = rendersText()
			h.deliver(press("v"))
			h.deliver(press("v"))
			h.deliver(press("v")) // render 3 of 3
			if v := h.at(80, 24); !strings.Contains(v, "Render 3 of 3") {
				t.Fatalf("render 3:\n%s", v)
			}
			swapped := swap(card)
			h.m.byName["ork"].snap.EngineRuns[0].Card = swapped
			v := h.at(80, 24)
			if swapped == nil {
				if strings.Contains(v, "Render ") || strings.Contains(v, "Human checks") {
					t.Errorf("a run without a card shows card or render:\n%s", v)
				}
				return
			}
			if strings.Contains(v, "Render ") || !strings.Contains(v, "Human checks") {
				t.Errorf("the card must show again after the swap:\n%s", v)
			}
			h.deliver(press("1")) // the card's keys act
			if l := h.live(); len(l.marks) != 1 {
				t.Errorf("a digit marked %v after the swap", l.marks)
			}
			if strings.Contains(h.at(80, 24), "Render ") {
				t.Error("a render shows after the card's key")
			}
		})
	}
}

// The text comes from run.render, once for each render the owner moves to; the
// card in the snapshot carries none. Opening the viewer again asks again.
func TestApprovalCardFetchesRendersOnDemand(t *testing.T) {
	h := rendersHarness(t)
	if l := h.live(); len(l.renderCalls) != 0 {
		t.Fatalf("renders fetched before the owner asked: %v", l.renderCalls)
	}
	// The call is made by the command v returns. Before it returns the pane
	// says it is loading, and the footer is already the viewer's.
	_, cmd := h.m.Update(press("v"))
	if v := h.at(80, 24); !strings.Contains(v, "Loading launch-80x24.txt") || !strings.Contains(v, "Render 1 of 3: launch-80x24.txt") || strings.Contains(v, "LAUNCH-80 first line") {
		t.Errorf("loading state:\n%s", v)
	}
	for _, out := range exec(cmd) {
		h.deliver(out)
	}
	if v := h.at(80, 24); !strings.Contains(v, "LAUNCH-80 first line") || strings.Contains(v, "Loading") {
		t.Errorf("loaded state:\n%s", v)
	}
	h.deliver(press("v"))
	h.deliver(press("v"))
	h.deliver(press("v")) // back to the card
	h.deliver(press("v"))
	want := []string{"engine#launch-80x24.txt", "engine#launch-120x40.txt", "engine#launch-ascii.txt", "engine#launch-80x24.txt"}
	if got := h.live().renderCalls; !reflect.DeepEqual(got, want) {
		t.Errorf("run.render calls %v, want %v", got, want)
	}
}

func TestApprovalCardRendersFailedState(t *testing.T) {
	h := rendersHarness(t)
	h.live().renderErr = errors.New("not_found: no render launch-80x24.txt for run engine")
	h.deliver(press("v"))
	v := h.at(80, 24)
	if !strings.Contains(v, "Could not read launch-80x24.txt: not_found: no render") || !strings.Contains(v, "for run engine") || strings.Contains(v, "Loading") {
		t.Errorf("failed state:\n%s", v)
	}
	// The owner can go on to the next render, and back out.
	h.live().renderErr = nil
	h.deliver(press("v"))
	if v = h.at(80, 24); !strings.Contains(v, "row 01 of the 120x40 view") {
		t.Errorf("the next render after a failed one:\n%s", v)
	}
	h.deliver(press("esc"))
	if v = h.at(80, 24); !strings.Contains(v, "Human checks") {
		t.Errorf("esc after a failed render:\n%s", v)
	}

	// A daemon connection that cannot read renders says so.
	h = rendersHarness(t)
	h.m.cfg.Live = &actionLive{src: h.src}
	h.deliver(press("v"))
	if v = h.at(80, 24); !strings.Contains(v, "Could not read launch-80x24.txt: this daemon connection") || !strings.Contains(v, "cannot read renders") {
		t.Errorf("no render reader:\n%s", v)
	}
}

// While a render shows, the Overview rows above the pane are gone and the
// render gets the whole body (docs/dashboard.md).
func TestApprovalCardRenderCollapsesTheOverview(t *testing.T) {
	h := rendersHarness(t)
	card := h.at(80, 24)
	if !strings.Contains(card, "Needs you 1") || !strings.Contains(card, "Approve 1") {
		t.Fatalf("the Overview rows are missing on the card:\n%s", card)
	}
	h.deliver(press("v"))
	v := h.at(80, 24)
	for _, gone := range []string{"Needs you 1", "Approve 1", "Running 0", "nothing running"} {
		if strings.Contains(v, gone) {
			t.Errorf("Overview row %q stays above a render:\n%s", gone, v)
		}
	}
	if len(strings.Split(v, "\n")) != 24 {
		t.Errorf("the screen is %d rows, want 24:\n%s", len(strings.Split(v, "\n")), v)
	}
	h.deliver(press("v")) // the 40-row render fills more of the screen than it did
	long := h.at(80, 24)
	if len(strings.Split(long, "\n")) != 24 {
		t.Errorf("the screen is %d rows with the long render, want 24:\n%s", len(strings.Split(long, "\n")), long)
	}
	if rows := strings.Count(long, "row "); rows != 13 { // the rule under the run line takes one of the 13 rows (the hint line takes one, S149)
		t.Errorf("the render shows %d rows at 80x24:\n%s", rows, long)
	}
	// Back on the card the rows return.
	h.deliver(press("esc"))
	if back := h.at(80, 24); !strings.Contains(back, "Needs you 1") {
		t.Errorf("the Overview rows must return:\n%s", back)
	}
}

func TestApprovalCardRenderGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprintf("mono-%t", mono), func(t *testing.T) {
			h := rendersHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			h.deliver(press("v"))
			h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			checkGolden(t, fmt.Sprintf("approval-render-80x24-mono-%t.txt", mono), h.m.View().Content)
		})
	}
}

// An answer is kept only for the pane, the card round and the render that
// asked: a late answer for another run or an older round never shows.
func TestApprovalCardIgnoresLateRenderAnswers(t *testing.T) {
	h := rendersHarness(t)
	_, cmd := h.m.Update(press("v")) // the call is not delivered yet
	for _, late := range []renderMsg{
		{run: "another-run", name: "launch-80x24.txt", round: 1, text: "WRONG RUN"},
		{run: "engine", name: "launch-80x24.txt", round: 99, text: "STALE ROUND"},
		{run: "engine", name: "launch-120x40.txt", round: 1, text: "OTHER RENDER"},
	} {
		h.m.Update(late)
	}
	v := h.at(80, 24)
	for _, bad := range []string{"WRONG RUN", "STALE ROUND", "OTHER RENDER"} {
		if strings.Contains(v, bad) {
			t.Errorf("a late answer shows (%s):\n%s", bad, v)
		}
	}
	if !strings.Contains(v, "Loading launch-80x24.txt") {
		t.Errorf("the render must still load:\n%s", v)
	}
	for _, out := range exec(cmd) {
		h.deliver(out)
	}
	if v = h.at(80, 24); !strings.Contains(v, "LAUNCH-80 first line") {
		t.Errorf("the real answer:\n%s", v)
	}
	// Opening the viewer again must not show the earlier answer while loading.
	h.deliver(press("v"))
	h.deliver(press("v"))
	h.deliver(press("v"))
	_, cmd = h.m.Update(press("v"))
	if v = h.at(80, 24); strings.Contains(v, "LAUNCH-80 first line") || !strings.Contains(v, "Loading") {
		t.Errorf("an old answer shows while the new one loads:\n%s", v)
	}
	_ = cmd
}

// A narrow help pane wraps the action keys rather than clipping one away.
func TestHelpWrapsActionKeysInANarrowPane(t *testing.T) {
	m := goldenModel(NewTheme(true), 80, 24)
	got := strings.Join(m.helpLines(40, 0), "\n")
	for _, want := range []string{"v view renders", "f file slice"} {
		if !strings.Contains(got, want) {
			t.Errorf("help at 40 columns lacks %q:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if utf8.RuneCountInString(line) > 40 {
			t.Errorf("help line over 40 columns: %q", line)
		}
	}
}

// The action keys of the help wrap between keys, never inside one, and each
// line takes as many keys as fit.
func TestHelpPacksActionKeysWhole(t *testing.T) {
	m := goldenModel(NewTheme(true), 80, 24)
	hints := []string{"a approve", "F fix round", "x stop", "t retry", "v view renders", "f file slice"}
	isHint := map[string]bool{}
	for _, h := range hints {
		isHint[h] = true
	}
	for w := 24; w <= 80; w++ {
		var block [][]string // the lines made of action keys, each as its keys
		for _, line := range m.helpLines(w, 0) {
			if utf8.RuneCountInString(line) > w {
				t.Errorf("width %d: line over the width: %q", w, line)
			}
			keys := strings.Split(line, "; ")
			whole := true
			for _, k := range keys {
				whole = whole && isHint[k]
			}
			if whole {
				block = append(block, keys)
			}
		}
		var got []string
		for i, keys := range block {
			got = append(got, keys...)
			if i+1 < len(block) {
				used := utf8.RuneCountInString(strings.Join(keys, "; "))
				if next := block[i+1][0]; used+2+utf8.RuneCountInString(next) <= w {
					t.Errorf("width %d: %q would have fitted after %q", w, next, keys)
				}
			}
		}
		if !reflect.DeepEqual(got, hints) {
			t.Fatalf("width %d: action keys %v, want %v (a key is split or lost)", w, got, hints)
		}
	}
}

// A dim rule sits between the run line and the render, in the theme's
// horizontal device (ASCII with the ASCII option), so the render does not
// read as part of the header.
func TestApprovalCardRenderHasARuleUnderTheRunLine(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		t.Run(fmt.Sprintf("ascii-%t", ascii), func(t *testing.T) {
			h := rendersHarness(t)
			h.m.cfg.ASCII = ascii
			h.deliver(press("v"))
			rows := strings.Split(h.at(80, 24), "\n")
			head := -1
			for i, row := range rows {
				if strings.Contains(row, "Run engine  S22  awaiting approval") {
					head = i
				}
			}
			if head < 0 || head+2 >= len(rows) {
				t.Fatalf("no run line:\n%s", strings.Join(rows, "\n"))
			}
			_, horizontal := h.m.cfg.Theme.rules(ascii)
			// The pane's text starts after the sidebar's rule: "│  ".
			after := func(row string) string { _, text, _ := strings.Cut(row, "|"); return text }
			if !ascii {
				after = func(row string) string { _, text, _ := strings.Cut(row, "│"); return text }
			}
			rule, first := strings.TrimSpace(after(rows[head+1])), after(rows[head+2])
			if want := strings.Repeat(horizontal, 20); !strings.Contains(rule, want) || strings.Trim(rule, horizontal) != "" {
				t.Errorf("the row under the run line is %q, want a rule of %q", rule, horizontal)
			}
			if !strings.Contains(first, "LAUNCH-80 first line") {
				t.Errorf("the render's first row follows the rule, got %q", first)
			}
			if ascii && strings.Contains(strings.Join(rows[head:head+3], ""), "─") {
				t.Errorf("a box-drawing rule in ASCII mode")
			}
		})
	}
}

// Answers can come back in any order, and the same render can be asked for
// more than once (S147). Only the answer to the latest ask for a render is
// kept; an older one never replaces it.
func TestOutOfOrderRenderAnswerKeepsTheLatest(t *testing.T) {
	h := rendersHarness(t)
	var cmds []tea.Cmd
	for range 3 {
		_, cmd := h.m.Update(press("v")) // renders 1, 2, 3
		cmds = append(cmds, cmd)
	}
	h.deliver(press("v")) // back to the card
	_, cmd := h.m.Update(press("v"))
	cmds = append(cmds, cmd) // render 1 again: call 4
	answer := func(cmd tea.Cmd, text string) {
		t.Helper()
		msgs := exec(cmd)
		if len(msgs) != 1 {
			t.Fatalf("a render call gave %d messages", len(msgs))
		}
		msg, ok := msgs[0].(renderMsg)
		if !ok {
			t.Fatalf("a render call gave %T", msgs[0])
		}
		msg.text, msg.err = text, nil
		h.deliver(msg)
	}
	answer(cmds[3], "SECOND")
	answer(cmds[0], "FIRST")
	v := h.at(80, 24)
	if !strings.Contains(v, "Render 1 of 3") || !strings.Contains(v, "SECOND") || strings.Contains(v, "FIRST") {
		t.Errorf("the older answer replaced the latest:\n%s", v)
	}
}

// A pane that was closed and opened again is a new ask: an answer to the old
// pane's call does not fill it.
func TestRenderAnswerFromAClosedPaneIsIgnored(t *testing.T) {
	h := rendersHarness(t)
	_, old := h.m.Update(press("v"))
	h.deliver(press("esc")) // back to the card
	h.deliver(press("esc")) // closes the pane
	if h.m.pane != nil {
		t.Fatal("the pane must be closed")
	}
	h.deliver(press("enter")) // opens it again
	h.m.Update(press("v"))    // asks again; the new call is not answered yet
	msgs := exec(old)
	msg := msgs[0].(renderMsg)
	msg.text = "OLD PANE ANSWER"
	h.deliver(msg)
	v := h.at(80, 24)
	if !strings.Contains(v, "Loading launch-80x24.txt") || strings.Contains(v, "OLD PANE ANSWER") {
		t.Errorf("an answer to the old pane's call filled the new pane:\n%s", v)
	}
}

// Drawing and asking which render is on show never change the pane (S147).
// A render the card no longer holds ends the mode for the owner, and the keys
// that leave it still work.
func TestRenderOnDoesNotWritePaneState(t *testing.T) {
	h := rendersHarness(t)
	for range 3 {
		h.deliver(press("v")) // render 3 of 3
	}
	card := *h.m.paneCard()
	card.Renders = card.Renders[:1] // the card lost render 3
	h.m.byName["ork"].snap.EngineRuns[0].Card = &card
	before := *h.m.pane
	if v := h.view(); strings.Contains(v, "Render ") || !strings.Contains(v, "Human checks") {
		t.Errorf("the card must show once the render is gone:\n%s", v)
	}
	if got := h.m.renderOn(); got != 0 {
		t.Errorf("renderOn = %d, want 0 for a render that is gone", got)
	}
	if !reflect.DeepEqual(*h.m.pane, before) {
		t.Errorf("drawing or renderOn changed the pane: render %d/round %d before, %d/%d after", before.render, before.renderRound, h.m.pane.render, h.m.pane.renderRound)
	}
	h.deliver(press("v")) // the first render of the new card
	if v := h.at(80, 24); !strings.Contains(v, "Render 1 of 1") {
		t.Errorf("v must open the first render:\n%s", v)
	}
	h.deliver(press("esc")) // back to the card
	if v := h.at(80, 24); strings.Contains(v, "Render ") || !strings.Contains(v, "Human checks") {
		t.Errorf("esc must return to the card:\n%s", v)
	}
	if h.m.pane == nil {
		t.Error("esc from a render must keep the pane open")
	}
}

// A renders directory the daemon could not read is told apart from a UI pass
// that wrote none (S147).
func TestApprovalCardSaysWhyRendersCouldNotBeRead(t *testing.T) {
	card := approvalCard(openChecksThree()...)
	card.UIRound, card.RendersError = 1, "permission denied"
	h := cardHarness(t, card)
	v := h.at(120, 40)
	if !strings.Contains(v, "Renders: the UI pass (round 1) could not be read: permission denied") || strings.Contains(v, "wrote none") {
		t.Errorf("the card must say why:\n%s", v)
	}
	// Some renders listed, one not readable: the count stays and the reason is added.
	card = rendersCard()
	card.RendersError = "b.txt: permission denied"
	h = cardHarness(t, card)
	v = h.at(120, 40)
	if !strings.Contains(v, "Renders: 3 from the UI pass (round 1)") || !strings.Contains(v, "could not be read: b.txt: permission denied") {
		t.Errorf("the card must count the renders and say why some are missing:\n%s", v)
	}
}

// The draw path no longer ends the render mode, so the snapshot that takes the
// render away does: a card that comes back later does not bring the render
// back with it (S147).
func TestRenderModeEndsWhenASnapshotDropsTheRender(t *testing.T) {
	h := rendersHarness(t)
	for range 3 {
		h.deliver(press("v")) // render 3 of 3
	}
	full := h.m.byName["ork"].snap
	lost := full
	lost.EngineRuns = append([]source.EngineRun(nil), full.EngineRuns...)
	card := *full.EngineRuns[0].Card
	card.Renders = card.Renders[:1]
	lost.EngineRuns[0].Card = &card
	h.deliver(snapshotMsg{name: "ork", snap: lost})
	h.deliver(snapshotMsg{name: "ork", snap: full}) // the three renders are back
	if v := h.at(80, 24); strings.Contains(v, "Render ") || !strings.Contains(v, "Human checks") {
		t.Errorf("the render came back with the card:\n%s", v)
	}
}
