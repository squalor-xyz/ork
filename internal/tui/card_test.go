// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

// cardLive is a daemon that takes human-check marks and fix notes: it keeps
// the calls and changes the snapshot the way the daemon's next read would.
type cardLive struct {
	actionLive
	approves int
	marks    []string
	fixes    []string
	markErr  error
	fixErr   error
}

func (l *cardLive) Approve(ctx context.Context, run string) error {
	l.approves++
	return l.actionLive.Approve(ctx, run)
}

func (l *cardLive) MarkCheck(_ context.Context, run string, n int, state string) error {
	l.marks = append(l.marks, fmt.Sprintf("%s#%d=%s", run, n, state))
	if l.markErr != nil {
		return l.markErr
	}
	s := l.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
	card := *s.EngineRuns[0].Card
	card.HumanChecks = append([]runrec.HumanCheck(nil), card.HumanChecks...)
	card.HumanChecks[n-1].State = state
	s.EngineRuns[0].Card = &card
	l.src.set(s)
	return nil
}

func (l *cardLive) FileLater(_ context.Context, run string, n int) (string, error) {
	return "filed as S130", nil
}

func (l *cardLive) Fix(_ context.Context, run, note string) error {
	l.fixes = append(l.fixes, run+": "+note)
	return l.fixErr
}

const (
	humanOne   = "Open the pane on a running relay and see it move on."
	humanTwo   = "Resize to 80x24 and read the card."
	humanThree = "Press F, type a note and send it."
)

func approvalCard(checks ...runrec.HumanCheck) *runrec.ApprovalCard {
	return &runrec.ApprovalCard{
		Round: 1, Verdict: "PASS", Passed: 12, Failed: 0,
		DiffStat:  " internal/engine/engine.go | 12 ++++++++----\n internal/tui/pane.go       | 40 ++++++++++++++++++++++++++++++++++++++++\n 2 files changed, 48 insertions(+), 4 deletions(-)",
		Untracked: []string{"internal/tui/card.go"}, HumanChecks: checks,
	}
}

func openChecksThree() []runrec.HumanCheck {
	return []runrec.HumanCheck{{Text: humanOne}, {Text: humanTwo}, {Text: humanThree}}
}

// cardHarness opens the pane on an awaiting-approval run with an approval card.
func cardHarness(t *testing.T, card *runrec.ApprovalCard, fs ...source.EngineFinding) *harness {
	t.Helper()
	h := actionHarness(t, "awaiting-approval", "")
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
	s.EngineRuns[0].Findings, s.EngineRuns[0].Card = fs, card
	h.src.set(s)
	h.m.byName["ork"].snap.EngineRuns[0].Findings = fs
	h.m.byName["ork"].snap.EngineRuns[0].Card = card
	h.m.cfg.Live = &cardLive{actionLive: actionLive{src: h.src}}
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "Run engine") {
		t.Fatalf("Enter must open the pane:\n%s", v)
	}
	return h
}

func (h *harness) live() *cardLive { return h.m.cfg.Live.(*cardLive) }

func TestApprovalCardShowsVerdictTestsDiffstat(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	v := h.view()
	for _, want := range []string{
		"Gate PASS (round 1)", "Tests: 12 passed, 0 failed",
		"Diff: 2 files, +48 -4, 1 new file",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("the card lacks %q:\n%s", want, v)
		}
	}
	// The per-file lines fill the room above the footer on a tall terminal.
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if v := plainView(h.m); !strings.Contains(v, "internal/tui/pane.go") || !strings.Contains(v, "internal/tui/card.go  (new)") {
		t.Errorf("the per-file diff stat is missing at 120x40:\n%s", v)
	}
}

func TestApprovalCardIsOnlyForARunAwaitingApprovalWithACard(t *testing.T) {
	for name, tc := range map[string]struct {
		state string
		card  *runrec.ApprovalCard
	}{
		"no card":   {"awaiting-approval", nil},
		"implement": {"implement", approvalCard(openChecksThree()...)},
		"paused":    {"paused", approvalCard(openChecksThree()...)},
		"stopped":   {"stopped", approvalCard(openChecksThree()...)},
	} {
		t.Run(name, func(t *testing.T) {
			h := actionHarness(t, tc.state, "")
			s := h.src.Snapshot(context.Background(), proj("ork", false))
			s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
			s.EngineRuns[0].Card = tc.card
			h.src.set(s)
			h.m.byName["ork"].snap.EngineRuns[0].Card = tc.card
			h.deliver(press("enter"))
			v := h.view()
			for _, not := range []string{"Gate PASS", "Human checks", "F fix"} {
				if strings.Contains(v, not) {
					t.Errorf("shows %q:\n%s", not, v)
				}
			}
		})
	}
}

func TestApprovalCardListsOwnerFindingsAndHumanChecks(t *testing.T) {
	checks := openChecksThree()
	checks[0].State = runrec.CheckTicked
	checks[1].State = runrec.CheckLater
	h := cardHarness(t, approvalCard(checks...), laterOne, ownerNote)
	v := h.view()
	for _, want := range []string{
		"Owner question, not blocking: Should the cache be shared? Recommended:", "per-project.",
		"Human checks (1 of 3 open)",
		"1 [x] " + humanOne,
		"2 [~] " + humanTwo + " (later)",
		"3 [ ] " + humanThree,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("the card lacks %q:\n%s", want, v)
		}
	}
	// The owner finding comes before the human checks, and both after the verdict.
	at := func(s string) int { return strings.Index(v, s) }
	if !(at("Gate PASS") < at("Owner question") && at("Owner question") < at("Human checks")) {
		t.Errorf("order of verdict, owner findings, human checks:\n%s", v)
	}
}

func TestApprovalCardWithoutHumanChecks(t *testing.T) {
	h := cardHarness(t, approvalCard())
	v := h.view()
	if !strings.Contains(v, "Human checks: none") || !strings.Contains(v, "a approve") {
		t.Errorf("a card without checks:\n%s", v)
	}
	h.deliver(press("a"))
	if h.live().approves != 1 {
		t.Error("a card without human checks approves at once")
	}
}

func TestApprovalCardNamesChecksItCouldNotRead(t *testing.T) {
	card := approvalCard()
	card.ChecksError = "slicer show: locked"
	h := cardHarness(t, card)
	if v := h.view(); !strings.Contains(v, "Human checks: not read (slicer show: locked)") {
		t.Errorf("unreadable checks:\n%s", v)
	}
}

func TestApprovalCardDigitsMarkHumanChecks(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	live := h.live()
	// Each press moves the check on: ticked, later, open again.
	h.deliver(press("2"))
	if v := h.view(); !strings.Contains(v, "2 [x] "+humanTwo) || !strings.Contains(v, "Human checks (2 of 3 open)") {
		t.Fatalf("first press:\n%s", v)
	}
	h.deliver(press("2"))
	if v := h.view(); !strings.Contains(v, "2 [~] "+humanTwo+" (later)") {
		t.Fatalf("second press:\n%s", v)
	}
	h.deliver(press("2"))
	if v := h.view(); !strings.Contains(v, "2 [ ] "+humanTwo) {
		t.Fatalf("third press:\n%s", v)
	}
	if got := strings.Join(live.marks, " "); got != "engine#2=ticked engine#2=later engine#2=" {
		t.Errorf("calls %q", got)
	}
	// A digit beyond the last check does nothing.
	h.deliver(press("4"))
	h.deliver(press("9"))
	if len(live.marks) != 3 {
		t.Errorf("calls %q", live.marks)
	}
}

func TestApprovalCardMarkErrorIsShown(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	h.live().markErr = fmt.Errorf("the slice is gate, not awaiting approval")
	h.deliver(press("1"))
	if v := h.view(); !strings.Contains(v, "not awaiting approval") || !strings.Contains(v, "1 [ ] "+humanOne) {
		t.Errorf("mark error:\n%s", v)
	}
}

// wideView is the view at 120x40, where the status line does not wrap.
func wideView(h *harness) string {
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return plainView(h.m)
}

func TestApproveNeedsEveryHumanCheckTickedOrLater(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	live := h.live()
	h.deliver(press("a"))
	if v := wideView(h); live.approves != 0 || !strings.Contains(v, "Approve needs every human check marked; open: 1, 2, 3 (press the number)") {
		t.Fatalf("approve with all open (calls %d):\n%s", live.approves, v)
	}
	h.deliver(press("1"))
	h.deliver(press("2"))
	h.deliver(press("2"))
	h.deliver(press("a"))
	if v := wideView(h); live.approves != 0 || !strings.Contains(v, "Approve needs every human check marked; open: 3 (press the number)") {
		t.Fatalf("approve with one open (calls %d):\n%s", live.approves, v)
	}
	h.deliver(press("3"))
	h.deliver(press("a"))
	if live.approves != 1 {
		t.Fatalf("approve with every check resolved made %d calls", live.approves)
	}
}

func TestApproveFromTheOverviewAlsoNeedsTheChecks(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	h.deliver(press("esc"))
	h.deliver(press("a"))
	if v := wideView(h); h.live().approves != 0 || !strings.Contains(v, "Approve needs every human check marked; 3 open; enter opens the card") {
		t.Fatalf("approve on the row (calls %d):\n%s", h.live().approves, v)
	}
}

func TestFixKeyOpensANoteFieldAndSendsIt(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	live := h.live()
	h.deliver(press("F"))
	v := h.view()
	if !strings.Contains(v, "Fix note: ") || !strings.Contains(v, "enter sends the note to a fix round; esc back to card") {
		t.Fatalf("F must open the note field:\n%s", v)
	}
	// The field owns the keys, the action letters and digits included.
	typeAnswer(h, "tidy 2 and a")
	if h.m.pane.input.Value() != "tidy 2 and a" || len(live.marks) != 0 || live.approves != 0 || len(live.fixes) != 0 {
		t.Fatalf("typing reached the card: field %q, marks %v, approves %d", h.m.pane.input.Value(), live.marks, live.approves)
	}
	if v := h.view(); !strings.Contains(v, "Fix note: tidy 2 and a") {
		t.Errorf("the field shows the text:\n%s", v)
	}
	h.deliver(press("enter"))
	if len(live.fixes) != 1 || live.fixes[0] != "engine: tidy 2 and a" {
		t.Fatalf("fixes %q", live.fixes)
	}
	if h.m.pane == nil || h.m.pane.fixing || h.m.pane.input.Value() != "" {
		t.Errorf("the field must close once the note is sent: %+v", h.m.pane)
	}
}

func TestFixNoteMustNotBeEmptyAndEscGoesBack(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	live := h.live()
	h.deliver(press("F"))
	h.deliver(press("enter"))
	if v := h.view(); len(live.fixes) != 0 || !strings.Contains(v, "Fix note must not be empty") || !strings.Contains(v, "Fix note: ") {
		t.Fatalf("empty note:\n%s", v)
	}
	typeAnswer(h, "x")
	h.deliver(press("esc"))
	if h.m.pane == nil || h.m.pane.fixing || h.m.pane.input.Value() != "" {
		t.Fatalf("esc must close the field and keep the pane: %+v", h.m.pane)
	}
	if v := h.view(); !strings.Contains(v, "Gate PASS") || strings.Contains(v, "Fix note:") {
		t.Errorf("back on the card:\n%s", v)
	}
	h.deliver(press("esc"))
	if h.m.pane != nil {
		t.Error("a second esc closes the pane")
	}
}

func TestFixFailureKeepsTheNote(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	h.live().fixErr = fmt.Errorf("the round limit of 8 implementer rounds was reached")
	h.deliver(press("F"))
	typeAnswer(h, "one more")
	h.deliver(press("enter"))
	v := h.view()
	if !strings.Contains(v, "round limit") || !strings.Contains(v, "Fix note: one more") {
		t.Errorf("a failed fix keeps the field and its text:\n%s", v)
	}
}

func TestFixKeyNeedsADaemonThatTakesFixes(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	h.m.cfg.Live = &actionLive{src: h.src}
	h.deliver(press("F"))
	if v := h.view(); !strings.Contains(v, "cannot send fixes") || strings.Contains(v, "Fix note:") {
		t.Errorf("a connection without fixes:\n%s", v)
	}
}

func TestFixKeyDoesNotTakeOverFileSlice(t *testing.T) {
	h := cardHarness(t, approvalCard(), laterOne)
	v := h.view()
	if !strings.Contains(v, "a approve; F fix round; x stop; f file slice; esc close pane") {
		t.Errorf("the key line:\n%s", v)
	}
	h.deliver(press("f"))
	if v := h.view(); !strings.Contains(v, "slicer add 'Add a --json flag to ork status'") || h.m.pane.fixing {
		t.Errorf("f still files a later finding:\n%s", v)
	}
}

func TestCardMarksWaitForAnyPendingCheck(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	// Hold the first call: it has started and not returned. The daemon takes
	// one mark at a time (the run is busy while it applies one).
	_, first := h.m.Update(press("1"))
	if first == nil {
		t.Fatal("the first mark made no call")
	}
	for _, key := range []string{"1", "2"} {
		if _, again := h.m.Update(press(key)); again != nil {
			t.Errorf("a mark of check %s went out while the first is pending", key)
		}
		if v := h.view(); !strings.Contains(v, "mark of check 1 is pending") {
			t.Errorf("the status names the pending mark after %s:\n%s", key, v)
		}
	}
	// Once the first call returns, the next mark goes out.
	h.deliver(first())
	if _, next := h.m.Update(press("2")); next == nil {
		t.Error("a mark waited after the first returned")
	}
}

func TestApprovalCardGoldens(t *testing.T) {
	checks := openChecksThree()
	checks[0].State = runrec.CheckTicked
	checks[1].State = runrec.CheckLater
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}} {
		for _, mono := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d-mono-%t", size.w, size.h, mono), func(t *testing.T) {
				h := cardHarness(t, approvalCard(checks...), laterOne, ownerNote)
				h.m.cfg.Theme = NewTheme(mono)
				h.m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
				got := h.m.View().Content
				checkGolden(t, fmt.Sprintf("pane-approval-card-%dx%d-mono-%t.txt", size.w, size.h, mono), got)
				// The card's whole footer fits: verdict, owner finding, checks, keys.
				plain := sgr.ReplaceAllString(got, "")
				for _, want := range []string{"Run engine  S22  awaiting-approval", "Gate PASS", "Owner question, not blocking", "Human checks (1 of 3 open)", "[~] " + humanTwo, "a approve; F fix round", "esc close pane"} {
					if !strings.Contains(plain, want) {
						t.Errorf("%dx%d lacks %q:\n%s", size.w, size.h, want, plain)
					}
				}
			})
		}
	}
}

// The live connection sends fixes and marks through the daemon's run.fix and
// run.check.
func TestDaemonFixAndCheckWire(t *testing.T) {
	var _ cardActor = (*Daemon)(nil)
	var seen []daemon.RunAction
	var methods []string
	d, _, _, _ := requestServer(t, func(_ context.Context, method string, params json.RawMessage) (any, error) {
		var a daemon.RunAction
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		methods, seen = append(methods, method), append(seen, a)
		return daemon.RunReply{}, nil
	})
	if err := d.Fix(context.Background(), "engine", "Rename it."); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkCheck(context.Background(), "engine", 2, "later"); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkCheck(context.Background(), "engine", 2, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, " ") != "run.fix run.check run.check" ||
		seen[0] != (daemon.RunAction{Run: "engine", Text: "Rename it."}) ||
		seen[1] != (daemon.RunAction{Run: "engine", Check: 2, State: "later"}) ||
		seen[2] != (daemon.RunAction{Run: "engine", Check: 2}) {
		t.Errorf("requests %v %+v", methods, seen)
	}
}

// The refusal at the narrowest standard size: the status line sits in the
// card's footer and the checks stay visible beside it.
func TestApprovalRefusedGolden(t *testing.T) {
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprint(mono), func(t *testing.T) {
			checks := openChecksThree()
			checks[0].State = runrec.CheckTicked
			h := cardHarness(t, approvalCard(checks...), ownerNote)
			h.m.cfg.Theme = NewTheme(mono)
			h.deliver(press("a"))
			h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			got := h.m.View().Content
			checkGolden(t, fmt.Sprintf("pane-approval-refused-80x24-mono-%t.txt", mono), got)
			if plain := sgr.ReplaceAllString(got, ""); !strings.Contains(plain, "Approve needs every human check marked; open: 2, 3") {
				t.Errorf("the refusal is not in the footer:\n%s", plain)
			}
		})
	}
}

func TestWrappedHumanCheckHangsUnderItsText(t *testing.T) {
	long := "Open the pane on a running relay, let it finish, and see the pane move on to the next running run within a couple of seconds."
	h := cardHarness(t, approvalCard())
	lines := h.m.cardChecks(approvalCard(runrec.HumanCheck{Text: long, State: runrec.CheckLater}), 40)
	lines = lines[1:] // the heading
	if len(lines) < 3 {
		t.Fatalf("the check should wrap over several lines: %q", lines)
	}
	for i, l := range lines {
		plain := sgr.ReplaceAllString(l, "")
		if len(plain) > 40 {
			t.Errorf("line %d is %d wide: %q", i, len(plain), plain)
		}
		if i == 0 && !strings.HasPrefix(plain, "1 [~] Open the pane") {
			t.Errorf("first line %q", plain)
		}
		if i > 0 && (!strings.HasPrefix(plain, "      ") || strings.HasPrefix(plain, "       ")) {
			t.Errorf("line %d does not start its text at column 6: %q", i, plain)
		}
	}
	if last := sgr.ReplaceAllString(lines[len(lines)-1], ""); !strings.HasSuffix(last, "(later)") {
		t.Errorf("the later mark ends the last line: %q", last)
	}
}

func TestHumanChecksLabelIsAHeadingWhateverTheCase(t *testing.T) {
	for name, card := range map[string]*runrec.ApprovalCard{
		"none":     approvalCard(),
		"not read": func() *runrec.ApprovalCard { c := approvalCard(); c.ChecksError = "slicer show: locked"; return c }(),
		"open":     approvalCard(openChecksThree()...),
	} {
		t.Run(name, func(t *testing.T) {
			h := cardHarness(t, card)
			h.m.cfg.Theme = NewTheme(false)
			want := NewTheme(false).Heading.Render("Human checks")
			if got := h.m.View().Content; !strings.Contains(got, want) {
				t.Errorf("the label is not a heading; want %q in the view", want)
			}
		})
	}
}

func TestFixKeyIsShiftFAndNothingElse(t *testing.T) {
	// A terminal that reports the text of the shifted key, and one that does not.
	for name, msg := range map[string]tea.KeyPressMsg{
		"text":   {Code: 'f', Mod: tea.ModShift, Text: "F"},
		"legacy": {Code: 'f', Mod: tea.ModShift},
		"plain":  press("F"),
	} {
		t.Run(name, func(t *testing.T) {
			h := cardHarness(t, approvalCard(openChecksThree()...))
			h.deliver(msg)
			if !h.m.pane.fixing {
				t.Errorf("shift-f did not open the note field:\n%s", h.view())
			}
		})
	}
	for _, k := range []string{"e", "f", "E"} {
		t.Run("not "+k, func(t *testing.T) {
			h := cardHarness(t, approvalCard(openChecksThree()...))
			h.deliver(press(k))
			if h.m.pane.fixing || h.m.confirm != nil {
				t.Errorf("%s opened a fix note or a confirmation", k)
			}
		})
	}
}

func TestDiffLineIsOneShortLine(t *testing.T) {
	for stat, want := range map[string]string{
		" a.go | 2 +-\n 2 files changed, 48 insertions(+), 4 deletions(-)": "2 files, +48 -4",
		" 1 file changed, 1 insertion(+)":                                  "1 file, +1 -0",
		" 1 file changed, 3 deletions(-)":                                  "1 file, +0 -3",
		"":                                                                 "none",
		" something unexpected":                                            "none",
	} {
		if got := diffLine(stat); got != want {
			t.Errorf("diffLine(%q) = %q, want %q", stat, got, want)
		}
	}
}

// At 80x24 the card leaves a line for the header that names the run.
func TestApprovalCardKeepsTheRunHeaderAt80x24(t *testing.T) {
	checks := openChecksThree()
	checks[1].State = runrec.CheckLater
	for _, fs := range [][]source.EngineFinding{nil, {laterOne, ownerNote}} {
		h := cardHarness(t, approvalCard(checks...), fs...)
		h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		v := plainView(h.m)
		if lines := strings.Split(v, "\n"); len(lines) > 24 {
			t.Errorf("%d lines at 80x24", len(lines))
		}
		for _, want := range []string{"Run engine  S22  awaiting-approval", "Gate PASS", "Diff: 2 files, +48 -4, 1 new file", "Human checks", "esc close"} {
			if !strings.Contains(v, want) {
				t.Errorf("findings %d: 80x24 lacks %q:\n%s", len(fs), want, v)
			}
		}
		if rowsWith(v, "Diff:") != 1 {
			t.Errorf("the diff summary must be one line:\n%s", v)
		}
	}
}

// The card's calls are described in words, not as commands: there is no ork
// command for them.
func TestCardStatusWords(t *testing.T) {
	h := cardHarness(t, approvalCard(openChecksThree()...))
	for _, step := range []struct{ key, pending, done string }{
		{"2", "marking check 2 done", "check 2 marked done"},
		{"2", "marking check 2 for later", "check 2 marked for later"},
		{"2", "reopening check 2", "check 2 reopened"},
	} {
		_, cmd := h.m.Update(press(step.key))
		if v := h.view(); !strings.Contains(v, step.pending) {
			t.Fatalf("pending status %q:\n%s", step.pending, v)
		}
		h.deliver(cmd())
		if v := h.view(); !strings.Contains(v, step.done) || strings.Contains(v, "check engine") {
			t.Fatalf("done status %q:\n%s", step.done, v)
		}
	}
	h.deliver(press("F"))
	typeAnswer(h, "tidy")
	_, cmd := h.m.Update(press("enter"))
	if v := h.view(); !strings.Contains(v, "fix note sent; fix round running") || strings.Contains(v, "fix engine") {
		t.Fatalf("pending fix:\n%s", v)
	}
	h.deliver(cmd())
	if v := h.view(); !strings.Contains(v, "fix round finished") || strings.Contains(v, "fix engine") {
		t.Fatalf("done fix:\n%s", v)
	}
}

// Whatever the height, the footer of a card leaves a line for the run header.
func TestApprovalCardKeepsTheRunHeaderAtEveryHeight(t *testing.T) {
	checks := openChecksThree()
	checks[1].State = runrec.CheckLater
	for height := 16; height <= 60; height++ {
		h := cardHarness(t, approvalCard(checks...), laterOne, laterTwo, ownerNote)
		h.m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
		if v := plainView(h.m); !strings.Contains(v, "Run engine  S22  awaiting-approval") {
			t.Errorf("no run header at 80x%d:\n%s", height, v)
		}
	}
}
