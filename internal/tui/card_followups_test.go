// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// manyChecks is a card of n open human checks, each a short line.
func manyChecks(n int) []runrec.HumanCheck {
	var checks []runrec.HumanCheck
	for i := 1; i <= n; i++ {
		checks = append(checks, runrec.HumanCheck{Text: fmt.Sprintf("Check number %d.", i)})
	}
	return checks
}

// Checks 10 and above are marked by typing the number. A digit that could
// start a longer number waits for the next digit or for enter; any other digit
// marks at once, so 1-9 keep their meaning on a card of up to nine checks.
func TestChecksTenAndAboveCanBeMarked(t *testing.T) {
	h := cardHarness(t, approvalCard(manyChecks(12)...))
	live := h.live()
	press2 := func(keys ...string) {
		for _, k := range keys {
			h.deliver(press(k))
		}
	}
	press2("1", "2")
	press2("1", "0")
	press2("1", "enter")
	press2("3")
	if got, want := strings.Join(live.marks, " "), "engine#12=ticked engine#10=ticked engine#1=ticked engine#3=ticked"; got != want {
		t.Fatalf("marks %q, want %q", got, want)
	}
	// A pending number says so, and a number past the last check is refused.
	h.deliver(press("1"))
	if v := h.view(); !strings.Contains(v, "check 1_") {
		t.Errorf("a pending number is not shown:\n%s", v)
	}
	h.deliver(press("3"))
	if v := h.view(); !strings.Contains(v, "no human check 13") || len(live.marks) != 4 {
		t.Errorf("check 13 of 12 (marks %q):\n%s", live.marks, v)
	}
	// esc drops a pending number before it closes the pane.
	h.deliver(press("1"))
	h.deliver(press("esc"))
	if h.m.pane == nil || len(live.marks) != 4 {
		t.Fatalf("esc closed the pane or marked (marks %q)", live.marks)
	}
	h.deliver(press("enter"))
	if len(live.marks) != 4 {
		t.Errorf("a dropped number was marked: %q", live.marks)
	}
	// Any other key drops it too, and then acts.
	h.deliver(press("1"))
	h.deliver(press("a"))
	h.deliver(press("2"))
	if got := live.marks[len(live.marks)-1]; got != "engine#2=ticked" {
		t.Errorf("last mark %q, want engine#2=ticked", got)
	}
	// The hint names the whole range.
	if v := h.view(); !strings.Contains(v, "1-12") {
		t.Errorf("the hint does not name 1-12:\n%s", v)
	}
}

// Nine checks or fewer keep the single digit: no waiting.
func TestChecksUpToNineMarkOnTheFirstDigit(t *testing.T) {
	h := cardHarness(t, approvalCard(manyChecks(9)...))
	h.deliver(press("1"))
	h.deliver(press("9"))
	if got, want := strings.Join(h.live().marks, " "), "engine#1=ticked engine#9=ticked"; got != want {
		t.Errorf("marks %q, want %q", got, want)
	}
}

// The new files line up with the diff stat's column: the bar of every line is
// in the same place.
func TestCardAlignsNewFilesWithTheDiffStat(t *testing.T) {
	h := cardHarness(t, approvalCard())
	card := approvalCard()
	card.Untracked = []string{"internal/tui/card.go", "docs/a-very-long-new-file-name.md"}
	var cols []int
	var lines []string
	for _, l := range h.m.cardBody(card, 100) {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines = append(lines, l)
		cols = append(cols, strings.Index(l, "|"))
	}
	if len(lines) != 4 {
		t.Fatalf("lines %q", lines)
	}
	for i, c := range cols {
		if c < 0 || c != cols[0] {
			t.Errorf("line %d has its bar at %d, line 0 at %d: %q", i, c, cols[0], lines)
		}
	}
	for _, l := range lines[2:] {
		if !strings.HasSuffix(l, "| (new)") {
			t.Errorf("a new file line should end in the bar and (new): %q", l)
		}
	}
}

// Above 24 rows a blank line sets the card's blocks apart; at 24 rows there is
// no room for them.
func TestCardBlankLineBetweenBlocksAbove24Rows(t *testing.T) {
	blockBefore := func(plain, start string) (string, bool) {
		lines := strings.Split(plain, "\n")
		for i, l := range lines {
			if strings.Contains(l, start) && i > 0 {
				_, text, _ := strings.Cut(lines[i-1], "│")
				return strings.TrimSpace(text), true
			}
		}
		return "", false
	}
	for _, size := range []struct{ w, h int }{{120, 40}} {
		h := cardHarness(t, approvalCard(manyChecks(2)...), laterOne, ownerNote)
		h.m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		plain := plainView(h.m)
		for _, start := range []string{"1 later finding", "Human checks", "Approve lands it"} {
			if prev, ok := blockBefore(plain, start); !ok || prev != "" {
				t.Errorf("%dx%d: the line above %q is %q, want a blank:\n%s", size.w, size.h, start, prev, plain)
			}
		}
	}
	h := cardHarness(t, approvalCard(manyChecks(2)...), laterOne, ownerNote)
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	plain := plainView(h.m)
	for _, start := range []string{"1 later finding", "Human checks", "Approve lands it"} {
		if prev, ok := blockBefore(plain, start); !ok || prev == "" {
			t.Errorf("80x24: a blank line above %q:\n%s", start, plain)
		}
	}
}

// At 80x24 a check that wraps takes more lines; the owner finding with its
// recommendation stays, and the boilerplate lines go first.
func TestWrappedCheckLineKeepsTheRecommendation(t *testing.T) {
	checks := openChecksThree()
	checks[0].Text += " " + humanOne
	checks[2].Text += " " + humanThree
	h := cardHarness(t, approvalCard(checks...), laterOne, ownerNote)
	h.deliver(press("2")) // the status line tells the mark
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	plain := plainView(h.m)
	if !strings.Contains(plain, "check 2 marked done") {
		t.Fatalf("no status line:\n%s", plain)
	}
	if !strings.Contains(plain, "Recommended: per-project.") || !strings.Contains(plain, "Owner question, not blocking") {
		t.Errorf("a wrapped check pushed the owner recommendation out:\n%s", plain)
	}
	for _, want := range []string{"Human checks", "Run engine", "Gate PASS"} {
		if !strings.Contains(plain, want) {
			t.Errorf("lacks %q:\n%s", want, plain)
		}
	}
	if lines := strings.Split(plain, "\n"); len(lines) > 24 {
		t.Errorf("%d lines at 80x24", len(lines))
	}
}

// A refused action shows its text after the verb, without the wire code.
func TestRefusedActionStatusIsTheVerbAndTheText(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	h.live().markErr = &ipc.Error{Code: daemon.CodeRefused, Message: "the slice is gate, not awaiting approval"}
	h.deliver(press("1"))
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	plain := plainView(h.m)
	if !strings.Contains(plain, "mark: the slice is gate, not awaiting approval") && !strings.Contains(plain, ": the slice is gate, not awaiting approval") {
		t.Fatalf("no refusal text:\n%s", plain)
	}
	if strings.Contains(plain, "refused:") || strings.Contains(plain, "internal:") {
		t.Errorf("the wire code is shown:\n%s", plain)
	}
	// Another code keeps its code.
	if got := actionError(&ipc.Error{Code: "not_found", Message: "no run x"}); got != "not_found: no run x" {
		t.Errorf("actionError = %q", got)
	}
	if got := actionError(&ipc.Error{Code: daemon.CodeRefused, Message: "nope"}); got != "nope" {
		t.Errorf("actionError of a refusal = %q", got)
	}
}

// With the project's push on, the line that says approval pushes ranks above an
// owner finding; with push off the landing line still drops before it (S141).
func TestPushLineOutranksOwnerFindingsWhenPushIsOn(t *testing.T) {
	checks := openChecksThree()
	checks[0].Text += " " + humanOne
	checks[2].Text += " " + humanThree
	for _, push := range []bool{true, false} {
		h := cardHarness(t, approvalCard(checks...), laterOne, ownerNote)
		h.m.byName["ork"].reg.Push = push
		h.deliver(press("2"))
		h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		plain := plainView(h.m)
		hasPush := strings.Contains(plain, "Approve lands and pushes it") || strings.Contains(plain, "and pushes it, then")
		hasOwner := strings.Contains(plain, "Recommended: per-project.")
		if push && !hasPush {
			t.Errorf("push on: the push line was dropped (owner %t):\n%s", hasOwner, plain)
		}
		if !push && (hasPush || !hasOwner || strings.Contains(plain, "Approve lands it")) {
			t.Errorf("push off: the owner finding must outlast the landing line (owner %t):\n%s", hasOwner, plain)
		}
	}
}

// The blank lines between the card's blocks never take room the body needs: at
// 100x30 with wrapped checks the diff stat stays in view (S141).
func TestCardGapsDoNotCrowdTheBodyAt100x30(t *testing.T) {
	checks := openChecksThree()
	checks[0].Text += " " + humanOne
	checks[2].Text += " " + humanThree
	h := cardHarness(t, approvalCard(checks...), laterOne, ownerNote)
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	plain := plainView(h.m)
	for _, want := range []string{"internal/engine/engine.go", "internal/tui/card.go"} {
		if !strings.Contains(plain, want) {
			t.Errorf("lacks %q:\n%s", want, plain)
		}
	}
	// The body's own marker, besides the Overview's.
	if n := strings.Count(plain, "more lines"); n < 2 {
		t.Errorf("the body lost its marker (%d):\n%s", n, plain)
	}
}

// With ten or more checks the numbers are padded to one width, so the brackets
// and the text line up, and a wrapped check hangs under its text (S141).
func TestHumanCheckNumbersAlignPastNine(t *testing.T) {
	checks := manyChecks(12)
	checks[0].Text = "A first check that is long enough to wrap over two lines."
	checks[11].Text = "A last check that is long enough to wrap over two lines too."
	h := cardHarness(t, approvalCard(checks...))
	lines := h.m.cardChecks(approvalCard(checks...), 40)[1:]
	var plain []string
	for _, l := range lines {
		plain = append(plain, sgr.ReplaceAllString(l, ""))
	}
	text := func(p string) int { return strings.Index(p, "[") }
	if !strings.HasPrefix(plain[0], " 1 [ ] A first") || text(plain[0]) != 3 {
		t.Fatalf("first line %q", plain[0])
	}
	for i, p := range plain {
		if strings.Contains(p, "[") && text(p) != 3 {
			t.Errorf("line %d: bracket at %d: %q", i, text(p), p)
		}
		if len(p) > 40 {
			t.Errorf("line %d is %d wide: %q", i, len(p), p)
		}
	}
	// "12 [ ] " and " 1 [ ] " put the bracket in the same column, and a wrapped
	// line starts its text under the first line's text (column 7).
	var wrapped, ten, one string
	for _, p := range plain {
		switch {
		case strings.HasPrefix(p, "12 [ ] "):
			ten = p
		case strings.HasPrefix(p, " 1 [ ] "):
			one = p
		case strings.TrimSpace(p) == "too." || strings.HasPrefix(p, "       ") && strings.Contains(p, "lines"):
			wrapped = p
		}
	}
	if ten == "" || one == "" || strings.Index(ten, "[") != strings.Index(one, "[") {
		t.Errorf("brackets do not line up:\n%s", strings.Join(plain, "\n"))
	}
	if wrapped == "" || !strings.HasPrefix(wrapped, "       ") || strings.HasPrefix(wrapped, "        ") {
		t.Errorf("a wrapped check does not hang at column 7:\n%s", strings.Join(plain, "\n"))
	}
}

// A number that waits shows in the hint line, which is always in view, even at
// 80x24 where the card's footer is cut; the hint says how to go on (S141).
func TestWaitingNumberShowsInTheHintLineAt80x24(t *testing.T) {
	h := cardHarness(t, approvalCard(manyChecks(12)...), laterOne, ownerNote)
	h.deliver(press("1"))
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	lines := strings.Split(plainView(h.m), "\n")
	hint := ""
	for _, l := range lines {
		if strings.Contains(l, "check 1_") {
			hint = l
		}
	}
	for _, want := range []string{"check 1_", "enter mark", "0-2 longer", "esc drop"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint line lacks %q: %q", want, hint)
		}
	}
	if strings.Contains(hint, "esc close pane") {
		t.Errorf("the hint still says esc closes the pane: %q", hint)
	}
	if len(lines) > 24 {
		t.Errorf("%d lines at 80x24", len(lines))
	}
	// Once dropped, the hint is the card's again.
	h.deliver(press("esc"))
	if v := plainView(h.m); strings.Contains(v, "check 1_") || !strings.Contains(v, "esc close pane") {
		t.Errorf("hint after esc:\n%s", v)
	}
}

// The scroll keys drop a waiting number like any other key.
func TestScrollKeysDropAWaitingNumber(t *testing.T) {
	for name, k := range map[string]tea.KeyPressMsg{
		"pgup": {Code: tea.KeyPgUp}, "pgdown": {Code: tea.KeyPgDown},
		"ctrl+d": {Code: 'd', Mod: tea.ModCtrl}, "ctrl+u": {Code: 'u', Mod: tea.ModCtrl},
	} {
		t.Run(name, func(t *testing.T) {
			h := cardHarness(t, approvalCard(manyChecks(12)...))
			h.deliver(press("1"))
			h.deliver(k)
			h.deliver(press("2"))
			if got := strings.Join(h.live().marks, " "); got != "engine#2=ticked" {
				t.Errorf("marks %q, want only engine#2=ticked (the 1 was dropped)", got)
			}
		})
	}
}

// git right-aligns the change counts; the card keeps that padding, so the
// counts end in the same column (S141).
func TestCardKeepsGitsCountPadding(t *testing.T) {
	h := cardHarness(t, approvalCard())
	card := approvalCard()
	card.DiffStat = " a/one.go   |  12 ++++++++++++\n a/two.go   |   3 +++\n a/three.go | 100 ----\n 3 files changed, 15 insertions(+), 100 deletions(-)"
	card.Untracked = []string{"a/new.go"}
	lines := h.m.cardBody(card, 80)
	for i, want := range []string{"|  12 +", "|   3 +", "| 100 -", "| (new)"} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d lacks %q (the counts lost git's padding): %q", i, want, lines)
		}
	}
}

// One long new file name does not widen every line past the pane: it is cut,
// and the stat lines keep their counts (S141).
func TestOneLongNewFileNameDoesNotClipTheDiffStat(t *testing.T) {
	h := cardHarness(t, approvalCard())
	card := approvalCard()
	card.Untracked = []string{"internal/tui/testdata/golden/a-really-long-generated-new-file-name-for-the-card-view-80x24.txt"}
	lines := h.m.cardBody(card, 60)
	for _, l := range lines {
		if len([]rune(l)) > 60 {
			t.Errorf("line is %d wide: %q", len([]rune(l)), l)
		}
	}
	if !strings.Contains(lines[0], "| 12 +") || !strings.Contains(lines[1], "| 40 +") {
		t.Errorf("the stat lines lost their counts: %q", lines)
	}
	if last := lines[2]; !strings.Contains(last, "...") || !strings.Contains(last, "| (new)") {
		t.Errorf("the long name is cut with ... before the bar: %q", last)
	}
}

// A waiting number does not survive the run leaving the card.
func TestWaitingNumberEndsWhenTheRunLeavesTheCard(t *testing.T) {
	h := cardHarness(t, approvalCard(manyChecks(12)...))
	h.deliver(press("1"))
	run := &h.m.byName["ork"].snap.EngineRuns[0]
	run.State = "implement"
	h.view() // a draw while the run has no card
	run.State = "awaiting-approval"
	h.deliver(press("2"))
	if got := strings.Join(h.live().marks, " "); got != "engine#2=ticked" {
		t.Errorf("marks %q, want only engine#2=ticked (the waiting 1 ended with the card)", got)
	}
}
