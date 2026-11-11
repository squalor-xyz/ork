// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const (
	checkOne   = "The owner opens a pane on a running relay and sees it move on."
	checkTwo   = "The owner resizes the terminal to 80x24 and reads the card."
	checkThree = "The owner stops the daemon and starts it again."
)

// humanSlice is the slice JSON with a Check section that holds the checks as
// [HUMAN CHECK] bullets among ordinary ones.
func humanSlice(checks ...string) string {
	body := "- go test ./...\\n- slicer check"
	for _, c := range checks {
		body += "\\n- [HUMAN CHECK] " + c
	}
	return `{"id":"S04","title":"Engine","status":"started","slice":{"id":"S04","title":"Engine","size":"M","sections":[` +
		`{"heading":"Why","body":"Because."},{"heading":"Check","body":"` + body + `"},` +
		`{"heading":"Git","body":"One commit: \"` + gitTitle + `\"."}]}}`
}

func withHumanChecks(checks ...string) func(*harnessOpts) {
	return func(o *harnessOpts) {
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"show", item}, Prefix: true, Stdout: humanSlice(checks...)})
	}
}

// sliceWith is the slice JSON with the given Why and Check section bodies and
// Check heading. The bodies are already escaped for a JSON string.
func sliceWith(checkHeading, why, check string) func(*harnessOpts) {
	return func(o *harnessOpts) {
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"show", item}, Prefix: true, Stdout: `{"id":"S04","title":"Engine","status":"started","slice":{"id":"S04","title":"Engine","size":"M","sections":[` +
			`{"heading":"Why","body":"` + why + `"},{"heading":"` + checkHeading + `","body":"` + check + `"},` +
			`{"heading":"Git","body":"One commit: \"` + gitTitle + `\"."}]}}`})
	}
}

const (
	deferredOne = "deferred to land: Run the workflow once."
	deferredTwo = "deferred to land: slicer check --root ../slicer"
)

// afterLandCheck holds an ordinary line, a [HUMAN CHECK] line that is also
// marked [AFTER LAND], and a plain [AFTER LAND] line (S204, *proposed*).
const afterLandCheck = "- go test ./...\\n- [HUMAN CHECK] [AFTER LAND] Run the workflow once.\\n- [AFTER LAND] slicer check --root ../slicer"

func TestCardListsAfterLandCheckLinesAsDeferred(t *testing.T) {
	h := newHarness(t, sliceWith("Check", "Because.", afterLandCheck))
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	want := []runrec.HumanCheck{{Text: deferredOne, State: runrec.CheckLater}, {Text: deferredTwo, State: runrec.CheckLater}}
	if rec.Card == nil || !reflect.DeepEqual(rec.Card.HumanChecks, want) {
		t.Fatalf("human checks %+v, want %+v", rec.Card, want)
	}
	if err := e.Approve(context.Background()); err != nil {
		t.Fatalf("Approve with no marks made: %v", err)
	}
	requireState(t, e, runrec.StateCommitted)
}

// The card keeps the order of the lines, and reads a numbered line and a
// lower-case heading and marker too.
func TestCardKeepsTheOrderOfDeferredAndHumanChecks(t *testing.T) {
	check := "- [HUMAN CHECK] " + checkOne + "\\n1. [after land] Run it on the scratch project.\\n- [HUMAN CHECK] " + checkTwo
	h := newHarness(t, sliceWith("check", "Because.", check))
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	want := []runrec.HumanCheck{
		{Text: checkOne},
		{Text: "deferred to land: Run it on the scratch project.", State: runrec.CheckLater},
		{Text: checkTwo},
	}
	if rec.Card == nil || !reflect.DeepEqual(rec.Card.HumanChecks, want) {
		t.Fatalf("human checks %+v, want %+v", rec.Card, want)
	}
}

func TestAfterLandLineOutsideTheCheckSectionIsIgnored(t *testing.T) {
	h := newHarness(t, sliceWith("Check", "- [AFTER LAND] slicer check --root ../slicer", "- go test ./..."))
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if rec.Card == nil || len(rec.Card.HumanChecks) != 0 {
		t.Fatalf("a Why bullet added checks: %+v", rec.Card)
	}
}

func TestAfterLandCheckKeepsTheOwnersMark(t *testing.T) {
	h := newHarness(t, sliceWith("Check", "Because.", "- [AFTER LAND] slicer check --root ../slicer"))
	e := h.start()
	ctx := context.Background()
	state := func() string {
		t.Helper()
		rec := requireState(t, e, runrec.StateAwaitingApproval)
		if rec.Card == nil || len(rec.Card.HumanChecks) != 1 || rec.Card.HumanChecks[0].Text != deferredTwo {
			t.Fatalf("card %+v", rec.Card)
		}
		return rec.Card.HumanChecks[0].State
	}
	if got := state(); got != runrec.CheckLater {
		t.Fatalf("first state %q, want later", got)
	}
	if err := e.MarkCheck(1, runrec.CheckTicked); err != nil {
		t.Fatal(err)
	}
	if err := e.Fix(ctx, "tidy"); err != nil {
		t.Fatal(err)
	}
	if got := state(); got != runrec.CheckTicked {
		t.Errorf("after a fix round the owner's tick is %q, want ticked", got)
	}
	// A check the owner reopened is open, not preset to later again.
	if err := e.MarkCheck(1, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.Fix(ctx, "tidy again"); err != nil {
		t.Fatal(err)
	}
	if got := state(); got != "" {
		t.Errorf("after a fix round a reopened check is %q, want open", got)
	}
}

func TestApprovalNoteNamesDeferredLines(t *testing.T) {
	h := newHarness(t, sliceWith("Check", "Because.", afterLandCheck))
	e := h.start()
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	notes := h.calls("slicer", "note")
	if len(notes) != 1 {
		t.Fatalf("%d notes: %v", len(notes), notes)
	}
	for _, want := range []string{"- later: " + deferredOne, "- later: " + deferredTwo} {
		if !strings.Contains(notes[0][3], want) {
			t.Errorf("note text lacks %q:\n%s", want, notes[0][3])
		}
	}
}

const stat = " internal/engine/engine.go | 12 ++++++++----\n internal/tui/pane.go       | 40 ++++++++++++++++++++++++++++++++++++++++\n 2 files changed, 48 insertions(+), 4 deletions(-)\n"

// withDiff answers the git calls that build the card's diff stat.
func withDiff(o *harnessOpts) {
	prev := o.routesFor
	o.routesFor = func(P, W string) []clitest.Route {
		var routes []clitest.Route
		if prev != nil {
			routes = prev(P, W)
		}
		return append(routes,
			clitest.Route{Tool: "git", Args: []string{"-C", W, "diff", "--stat", "HEAD"}, Stdout: stat},
			clitest.Route{Tool: "git", Args: []string{"-C", W, "ls-files", "--others", "--exclude-standard"}, Stdout: "internal/tui/card.go\n"},
		)
	}
}

func TestGatePassBuildsTheApprovalCard(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne, checkTwo), withDiff)
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	c := rec.Card
	if c == nil {
		t.Fatal("no approval card")
	}
	if c.Verdict != "PASS" || c.Round != 1 || c.Passed != 12 || c.Failed != 0 {
		t.Errorf("verdict and counts: %+v", c)
	}
	if strings.TrimSpace(c.DiffStat) != strings.TrimSpace(stat) || !reflect.DeepEqual(c.Untracked, []string{"internal/tui/card.go"}) {
		t.Errorf("diff stat %q, untracked %q", c.DiffStat, c.Untracked)
	}
	want := []runrec.HumanCheck{{Text: checkOne}, {Text: checkTwo}}
	if !reflect.DeepEqual(c.HumanChecks, want) {
		t.Errorf("human checks %+v, want %+v", c.HumanChecks, want)
	}
}

func TestGatePassCardWithoutHumanChecksNeedsNoMarks(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if rec.Card == nil || len(rec.Card.HumanChecks) != 0 {
		t.Fatalf("card %+v", rec.Card)
	}
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateCommitted)
	if n := len(h.calls("slicer", "note")); n != 0 {
		t.Errorf("a slice without human checks recorded %d notes", n)
	}
}

func TestFixActionStartsFixRoundWithNote(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne))
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	note := "Rename the flag to --json and keep the old name as an alias."
	if err := e.Fix(context.Background(), note); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)

	// A fix round, the checks and the gate: no second review.
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "gate-1", "implementer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	if len(h.checks.calls) != 2 {
		t.Errorf("the checks ran %d times, want 2", len(h.checks.calls))
	}
	reqs := h.runner.requests()
	fix := reqs[3]
	if fix.Resume != "sess-implementer" || fix.ReadOnly {
		t.Errorf("fix request: %+v", fix)
	}
	for _, want := range []string{"This is fix round 1.", "owner", note} {
		if !strings.Contains(fix.Brief, want) {
			t.Errorf("the fix brief lacks %q:\n%s", want, fix.Brief)
		}
	}
	if gate := reqs[4].Brief; !strings.Contains(gate, note) {
		t.Errorf("the gate does not see the owner's note:\n%s", gate)
	}
	if len(rec.Fixes) != 1 || rec.Fixes[0].Note != note || rec.Fixes[0].Round != 2 || rec.Fixes[0].At.IsZero() {
		t.Errorf("fixes: %+v", rec.Fixes)
	}
	if rec.Card == nil || rec.Card.Round != 2 || rec.Card.Verdict != "PASS" {
		t.Errorf("the card is not rebuilt for the new round: %+v", rec.Card)
	}
	if rec.Feedback != nil || rec.Round != 2 {
		t.Errorf("round %d, feedback %+v", rec.Round, rec.Feedback)
	}
	if n := len(h.calls("slicer", "note")); n != 0 {
		t.Errorf("only a gate FAIL writes a Review FAIL note; %d notes written", n)
	}
	if len(h.calls("git", "-C", h.W, "commit")) != 0 {
		t.Error("a fix must not commit")
	}
}

func TestFixActionRefusals(t *testing.T) {
	h := newHarness(t)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Fix(context.Background(), "x"); err == nil {
		t.Error("Fix before the run awaited approval was accepted")
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, note := range []string{"", "  \n"} {
		if err := e.Fix(context.Background(), note); err == nil {
			t.Errorf("Fix with the note %q was accepted", note)
		}
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if rec.Round != 1 || len(rec.Fixes) != 0 || len(h.runner.requests()) != 3 {
		t.Errorf("a refused fix changed the run: round %d, fixes %+v", rec.Round, rec.Fixes)
	}
	// The refusals left the run able to take a real note.
	if err := e.Fix(context.Background(), "  tidy up  "); err != nil {
		t.Fatal(err)
	}
	if rec := requireState(t, e, runrec.StateAwaitingApproval); len(rec.Fixes) != 1 || rec.Fixes[0].Note != "tidy up" {
		t.Errorf("the note is stored trimmed: %+v", rec.Fixes)
	}
}

func TestFixActionAtTheRoundLimitRefusesWithoutStopping(t *testing.T) {
	h := newHarness(t)
	h.deps.MaxRounds = 1
	e := h.start()
	err := e.Fix(context.Background(), "again")
	if err == nil || !strings.Contains(err.Error(), "round limit") {
		t.Fatalf("err %v", err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
}

func TestFixWithinTheCurrentLimitRunsAtOnce(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	if err := e.Fix(context.Background(), "round two"); err != nil {
		t.Fatal(err)
	}
	if err := e.Fix(context.Background(), "round three"); err != nil {
		t.Fatal(err)
	}
	r := requireState(t, e, runrec.StateAwaitingApproval)
	if r.Round != 3 {
		t.Fatalf("round %d", r.Round)
	}
}

func TestFixPastTheCurrentLimitRefuses(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	for i := 0; i < 2; i++ {
		if err := e.Fix(context.Background(), "again"); err != nil {
			t.Fatal(err)
		}
	}
	err := e.Fix(context.Background(), "past limit")
	if err == nil || !strings.Contains(err.Error(), "round limit") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("refusal %v", err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	// A grant made earlier in the run also permits the approval card's Fix.
	h = redLoop(t, true, true)
	e = h.start()
	requireRoundsPause(t, e, 3)
	h.checks.outputs = append(h.checks.outputs[:6], CheckOutput{OK: true, Output: "green\n"})
	if err := e.RetryNote(context.Background(), "", 2); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	saved, err := h.store.Open(e.Snapshot().ID)
	if err != nil {
		t.Fatal(err)
	}
	e = Load(h.deps, h.proj, saved)
	if err := e.Fix(context.Background(), "within grant"); err != nil {
		t.Fatal(err)
	}
	if r := requireState(t, e, runrec.StateAwaitingApproval); r.Round != 5 {
		t.Fatalf("round %d", r.Round)
	}
}

func TestApprovalNeedsEveryHumanCheckTickedOrLater(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne, checkTwo, checkThree))
	e := h.start()
	ctx := context.Background()
	requireState(t, e, runrec.StateAwaitingApproval)
	before := len(h.runner.requests())

	err := e.Approve(ctx)
	if err == nil || !strings.Contains(err.Error(), "3 human checks") {
		t.Fatalf("Approve with no marks: %v", err)
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if len(rec.Approvals) != 0 || len(h.runner.requests()) != before {
		t.Fatalf("a refused approval changed the run: %+v", rec.Approvals)
	}

	if err := e.MarkCheck(1, runrec.CheckTicked); err != nil {
		t.Fatal(err)
	}
	if err := e.MarkCheck(2, runrec.CheckLater); err != nil {
		t.Fatal(err)
	}
	err = e.Approve(ctx)
	if err == nil || !strings.Contains(err.Error(), "1 human check ") || !strings.Contains(err.Error(), checkThree) {
		t.Fatalf("Approve with one check open: %v", err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)

	// A mark can be taken back.
	if err := e.MarkCheck(1, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.MarkCheck(3, runrec.CheckTicked); err != nil {
		t.Fatal(err)
	}
	if err := e.Approve(ctx); err == nil {
		t.Fatal("Approve with check 1 reopened")
	}
	if err := e.MarkCheck(1, runrec.CheckTicked); err != nil {
		t.Fatal(err)
	}
	card := e.Snapshot().Card
	if card == nil || len(card.HumanChecks) != 3 {
		t.Fatalf("card %+v", card)
	}
	marks := card.HumanChecks
	if marks[0].State != runrec.CheckTicked || marks[1].State != runrec.CheckLater || marks[2].State != runrec.CheckTicked {
		t.Fatalf("marks %+v", marks)
	}
	if err := e.Approve(ctx); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateCommitted)
}

func TestMarkCheckRefusals(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne))
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.MarkCheck(1, runrec.CheckTicked); err == nil {
		t.Error("a mark before the run awaited approval was accepted")
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		n     int
		state string
	}{{0, runrec.CheckTicked}, {2, runrec.CheckTicked}, {-1, runrec.CheckLater}, {1, "done"}} {
		if err := e.MarkCheck(c.n, c.state); err == nil {
			t.Errorf("MarkCheck(%d, %q) was accepted", c.n, c.state)
		}
	}
	if err := e.MarkCheck(1, runrec.CheckLater); err != nil {
		t.Fatal(err)
	}
	card := e.Snapshot().Card
	if card == nil || len(card.HumanChecks) != 1 || card.HumanChecks[0].State != runrec.CheckLater {
		t.Errorf("the mark is not stored, or a refused one changed the card: %+v", card)
	}
}

func TestApprovalRecordsHumanChecksAsSlicerNote(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne, checkTwo))
	e := h.start()
	ctx := context.Background()
	if err := e.MarkCheck(1, runrec.CheckTicked); err != nil {
		t.Fatal(err)
	}
	if err := e.MarkCheck(2, runrec.CheckLater); err != nil {
		t.Fatal(err)
	}
	if err := e.Approve(ctx); err != nil {
		t.Fatal(err)
	}
	notes := h.calls("slicer", "note")
	if len(notes) != 1 {
		t.Fatalf("%d notes: %v", len(notes), notes)
	}
	n := notes[0]
	if n[1] != item || n[2] != "--text" || n[len(n)-2] != "--root" || n[len(n)-1] != h.W {
		t.Fatalf("note call: %v", n)
	}
	for _, want := range []string{"Human checks at approval", "ticked: " + checkOne, "later: " + checkTwo} {
		if !strings.Contains(n[3], want) {
			t.Errorf("note text lacks %q:\n%s", want, n[3])
		}
	}
	// The note comes before the finish step, which closes the slice.
	var order []string
	for _, c := range h.fake.Calls() {
		switch {
		case c.Tool == "slicer" && len(c.Args) > 0 && c.Args[0] == "note":
			order = append(order, "note")
		case c.Tool == "slicer" && len(c.Args) > 0 && c.Args[0] == "config":
			order = append(order, "config")
		}
	}
	if strings.Join(order, ",") != "note,config" {
		t.Errorf("order %v", order)
	}
	if rec := e.Snapshot(); rec.Card == nil || len(rec.Card.HumanChecks) != 2 || rec.Card.HumanChecks[1].State != runrec.CheckLater {
		t.Errorf("the marks stay in the record: %+v", rec.Card)
	}
}

func TestFixKeepsTheOwnersHumanCheckMarks(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne, checkTwo))
	e := h.start()
	if err := e.MarkCheck(2, runrec.CheckLater); err != nil {
		t.Fatal(err)
	}
	if err := e.Fix(context.Background(), "tidy the card"); err != nil {
		t.Fatal(err)
	}
	card := requireState(t, e, runrec.StateAwaitingApproval).Card
	if card == nil {
		t.Fatal("no card")
	}
	got := card.HumanChecks
	want := []runrec.HumanCheck{{Text: checkOne}, {Text: checkTwo, State: runrec.CheckLater}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("marks after the fix round: %+v, want %+v", got, want)
	}
}

// A fix round that pauses leaves the card in the record; the owner cannot mark
// checks of a slice that is not awaiting approval.
func TestMarkCheckIsRefusedOutsideAwaitingApproval(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne))
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && req.Round == 2 {
			return Result{Exit: ExitOK, Session: "sess-implementer", Report: "no tests line\n"}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	if err := e.Fix(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Card == nil {
		t.Fatal("the card is kept while the fix round is paused")
	}
	if err := e.MarkCheck(1, runrec.CheckTicked); err == nil || !strings.Contains(err.Error(), "not awaiting approval") {
		t.Errorf("MarkCheck on a paused slice: %v", err)
	}
}

// The slice cannot be read when the card is built: the card says so, and keeps
// the verdict and the diff stat.
func TestCardNamesHumanChecksItCouldNotRead(t *testing.T) {
	h := newHarness(t, withDiff, func(o *harnessOpts) {
		good := clitest.Reply{Stdout: humanSlice(checkOne)}
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"show", item}, Prefix: true,
			Seq: []clitest.Reply{good, good, good, good, {Stderr: "slicer: ledger busy", Exit: 1}}})
	})
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	c := rec.Card
	if c == nil || c.Verdict != "PASS" || strings.TrimSpace(c.DiffStat) != strings.TrimSpace(stat) {
		t.Fatalf("card %+v", c)
	}
	if len(c.HumanChecks) != 0 || c.ChecksError == "" {
		t.Errorf("checks %+v, error %q", c.HumanChecks, c.ChecksError)
	}
}

// The marks are the owner's work: a note that cannot be written refuses the
// approval and leaves the slice awaiting it.
func TestApprovalRefusedWhenTheChecksNoteFails(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne, checkTwo), func(o *harnessOpts) {
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"note", item}, Prefix: true, Stderr: "slicer: cannot write the note\n", Exit: 1})
	})
	e := h.start()
	ctx := context.Background()
	before := len(h.runner.requests())
	if err := e.MarkCheck(1, runrec.CheckTicked); err != nil {
		t.Fatal(err)
	}
	if err := e.MarkCheck(2, runrec.CheckLater); err != nil {
		t.Fatal(err)
	}
	err := e.Approve(ctx)
	if err == nil || !strings.Contains(err.Error(), "human checks") {
		t.Fatalf("Approve with a failing note: %v", err)
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if len(rec.Approvals) != 0 || len(h.runner.requests()) != before || len(h.calls("git", "-C", h.W, "commit")) != 0 {
		t.Errorf("a refused approval changed the run: approvals %+v", rec.Approvals)
	}
	if rec.Card == nil || rec.Card.HumanChecks[0].State != runrec.CheckTicked || rec.Card.HumanChecks[1].State != runrec.CheckLater {
		t.Errorf("the marks are kept for the next try: %+v", rec.Card)
	}
	if n := len(h.calls("slicer", "note")); n != 1 {
		t.Errorf("%d note calls", n)
	}
}

// Human checks that cannot be read at approval do not stop the approval: ork
// approves and writes a slicer note that says they could not be read (S141).
func TestApprovalNotesWhenChecksCannotBeRead(t *testing.T) {
	h := newHarness(t, withDiff, func(o *harnessOpts) {
		good := clitest.Reply{Stdout: humanSlice(checkOne)}
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"show", item}, Prefix: true,
			Seq: []clitest.Reply{good, good, good, good, {Stderr: "slicer: ledger busy", Exit: 1}, good}})
	})
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if rec.Card == nil || rec.Card.ChecksError == "" || len(rec.Card.HumanChecks) != 0 {
		t.Fatalf("card %+v", rec.Card)
	}
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateCommitted)
	notes := h.calls("slicer", "note")
	if len(notes) != 1 {
		t.Fatalf("%d notes: %v", len(notes), notes)
	}
	text := notes[0][3]
	for _, want := range []string{"Human checks at approval (" + item + ")", "could not be read", "ledger busy"} {
		if !strings.Contains(text, want) {
			t.Errorf("note lacks %q:\n%s", want, text)
		}
	}
}

// The note about unreadable checks is the owner's record like the marks: a
// note that cannot be written refuses the approval, as it does for the marks.
func TestApprovalRefusedWhenTheUnreadableChecksNoteFails(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		good := clitest.Reply{Stdout: humanSlice(checkOne)}
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"show", item}, Prefix: true,
			Seq: []clitest.Reply{good, good, good, good, {Stderr: "slicer: ledger busy", Exit: 1}, good}},
			clitest.Route{Tool: "slicer", Args: []string{"note", item}, Prefix: true, Stderr: "slicer: cannot write the note\n", Exit: 1})
	})
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	if err := e.Approve(context.Background()); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("Approve with a failing note: %v", err)
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if len(rec.Approvals) != 0 {
		t.Errorf("a refused approval changed the run: %+v", rec.Approvals)
	}
}

// The run record cannot be saved after the checks note was written: the owner
// approves again and the note is not written twice (S141).
func TestApprovalNoteNotDuplicatedWhenTheUpdateFails(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne))
	e := h.start()
	ctx := context.Background()
	if err := e.MarkCheck(1, runrec.CheckTicked); err != nil {
		t.Fatal(err)
	}
	// A directory in place of run.json makes the atomic rename fail.
	path := filepath.Join(e.run.Dir(), "run.json")
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := e.Approve(ctx); err == nil {
		t.Fatal("Approve succeeded although the record could not be saved")
	}
	if rec := e.Snapshot(); rec.State != runrec.StateAwaitingApproval || len(rec.Approvals) != 0 {
		t.Fatalf("the failed save changed the run: %s %+v", rec.State, rec.Approvals)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, saved, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.Approve(ctx); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateCommitted)
	if n := len(h.calls("slicer", "note")); n != 1 {
		t.Errorf("%d notes after the approval was retried, want 1", n)
	}
}

// The owner's fix note goes to the gate that checks the fix round. A red check
// or a gate FAIL sends the slice to a later round; the note still goes with it
// until a gate passes the change (S141).
func TestFixNoteCarriedIntoLaterGateBriefs(t *testing.T) {
	const note = "Rename the flag to --json and keep the old name as an alias."
	for _, tc := range []struct {
		name  string
		setup func(h *harness)
		roles []string
	}{
		{"red check", func(h *harness) {
			h.checks.outputs = []CheckOutput{{OK: true}, {OK: false, Output: "--- FAIL: TestFlag\nFAIL\n"}, {OK: false, Output: "--- FAIL: TestFlag\nFAIL\n"}, {OK: true}}
		}, []string{"implementer-1", "reviewer-1", "gate-1", "implementer-2", "implementer-3", "gate-3"}},
		{"gate FAIL", func(h *harness) {
			h.runner.script = func(req Request, n int) (Result, error) {
				if req.Role == RoleGate && req.Round == 2 {
					return Result{Exit: ExitOK, Session: "sess-gate", Report: gateFail}, nil
				}
				return Result{}, nil
			}
		}, []string{"implementer-1", "reviewer-1", "gate-1", "implementer-2", "gate-2", "implementer-3", "gate-3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			e := h.start()
			requireState(t, e, runrec.StateAwaitingApproval)
			tc.setup(h)
			if err := e.Fix(context.Background(), note); err != nil {
				t.Fatal(err)
			}
			requireState(t, e, runrec.StateAwaitingApproval)
			if got := h.runner.roles(); !reflect.DeepEqual(got, tc.roles) {
				t.Fatalf("role rounds %v, want %v", got, tc.roles)
			}
			reqs := h.runner.requests()
			gate := reqs[len(reqs)-1]
			if gate.Role != RoleGate || gate.Round != 3 {
				t.Fatalf("last request %s round %d", gate.Role, gate.Round)
			}
			if !strings.Contains(gate.Brief, note) {
				t.Errorf("the round-3 gate brief lacks the owner's note:\n%s", gate.Brief)
			}
		})
	}
}

// Once a gate has passed the fix, the note is not repeated to the next gate.
func TestFixNoteIsNotCarriedPastAGatePass(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	ctx := context.Background()
	requireState(t, e, runrec.StateAwaitingApproval)
	if err := e.Fix(ctx, "first change please"); err != nil {
		t.Fatal(err)
	}
	if err := e.Fix(ctx, "second change please"); err != nil {
		t.Fatal(err)
	}
	reqs := h.runner.requests()
	gate := reqs[len(reqs)-1]
	if gate.Role != RoleGate || !strings.Contains(gate.Brief, "second change please") || strings.Contains(gate.Brief, "first change please") {
		t.Errorf("the last gate brief:\n%s", gate.Brief)
	}
}

// An owner action the engine refuses changed nothing: its error is a refusal,
// which the daemon does not keep as a failure of the run (S141). A note that
// cannot be written is a failure, not a refusal.
func TestOwnerRefusalsAreMarked(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne), func(o *harnessOpts) {
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"note", item}, Prefix: true, Stderr: "slicer: cannot write the note\n", Exit: 1})
	})
	e := h.start()
	ctx := context.Background()
	for name, err := range map[string]error{
		"approve with an open check": e.Approve(ctx),
		"mark a check out of range":  e.MarkCheck(9, runrec.CheckTicked),
		"mark with a bad state":      e.MarkCheck(1, "bogus"),
		"fix with an empty note":     e.Fix(ctx, " "),
		"answer without a question":  e.Answer(ctx, "yes"),
		"retry when not paused":      e.Retry(ctx),
	} {
		if err == nil || !IsRefusal(err) {
			t.Errorf("%s: %v is not a refusal", name, err)
		}
	}
	if err := e.MarkCheck(1, runrec.CheckTicked); err != nil {
		t.Fatal(err)
	}
	if err := e.Approve(ctx); err == nil || IsRefusal(err) {
		t.Errorf("a failed note is a failure, not a refusal: %v", err)
	}
	if err := e.Reject(ctx, "no"); err != nil {
		t.Fatal(err)
	}
	if err := e.Stop(ctx, "again"); err == nil || !IsRefusal(err) {
		t.Errorf("stop of a stopped slice: %v", err)
	}
	if err := e.Approve(ctx); err == nil || !IsRefusal(err) {
		t.Errorf("approve of a stopped slice: %v", err)
	}
}

// S331 (*proposed*; adj 281): auto-land. A run whose gate passes, with no open
// owner finding and green checks, is approved by ork and lands with no approve
// call, unless the project opted out. The tests start a run at its gate in a
// landing environment (real git, fake slicer), so the whole path runs.

const autoLandCheckBody = "- go test ./...\\n- [HUMAN CHECK] " + checkOne + "\\n- [AFTER LAND] slicer check --root ../slicer"

func autoLandShow(checkBody string) string {
	return `{"id":"S04","title":"Engine","status":"started","slice":{"id":"S04","title":"Engine","size":"M","sections":[` +
		`{"heading":"Why","body":"Because."},{"heading":"Check","body":"` + checkBody + `"},` +
		`{"heading":"Git","body":"One commit: \"` + gitTitle + `\"."}]}}`
}

// newGateEnv is a landing environment whose run is at its gate, after a green
// check, with the slice's checks in show.json.
func newGateEnv(t *testing.T, checkBody string, opts ...func(*registry.Project)) *landEnv {
	t.Helper()
	env := newLandEnv(t, append([]func(*registry.Project){onAutoLand}, opts...)...)
	writeFile(t, filepath.Join(env.fakes, "show.json"), autoLandShow(checkBody))
	// The gate's brief reads the last check's log from the run directory.
	writeFile(t, filepath.Join(env.run.Dir(), "checks-1.log"), "$ go test ./...\nok  \tall packages\n")
	if err := env.run.Update(func(r *runrec.Record) {
		r.State = runrec.StateGate
		r.Card = nil
		r.Checks = []runrec.CheckRecord{{Round: 1, Command: "go test ./...", OK: true, Log: "checks-1.log"}}
	}); err != nil {
		t.Fatal(err)
	}
	return env
}

func (env *landEnv) runToRest() runrec.Record {
	env.t.Helper()
	if err := env.e.Run(context.Background()); err != nil {
		env.t.Fatalf("Run: %v", err)
	}
	return env.e.Snapshot()
}

func readTodo(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "human-checks-todo.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func offAutoLand(p *registry.Project) { off := false; p.AutoLand = &off }

// onAutoLand leaves the field absent, as a project that never set it is: on.
func onAutoLand(p *registry.Project) { p.AutoLand = nil }

const wantRunHeader = "## S04 (run ork-20261026-120000-a1b2, 2026-10-26)"

func TestAutoLandLandsAPassingRunWithNoApproveCall(t *testing.T) {
	env := newGateEnv(t, autoLandCheckBody)
	rec := env.runToRest()
	if rec.State != runrec.StateCommitted || rec.LandedAt.IsZero() {
		t.Fatalf("state %q (reason %q, pending %+v), landed at %v; want a landed run", rec.State, rec.Reason, rec.Pending, rec.LandedAt)
	}
	if len(rec.Approvals) != 1 || rec.Approvals[0].What != "slice" {
		t.Errorf("approvals %+v, want the one recorded by the automatic approval", rec.Approvals)
	}
	// The human checks end as later on the card, and none is open.
	want := []runrec.HumanCheck{
		{Text: checkOne, State: runrec.CheckLater},
		{Text: deferredTwo, State: runrec.CheckLater},
	}
	if rec.Card == nil || !reflect.DeepEqual(rec.Card.HumanChecks, want) {
		t.Errorf("card checks %+v, want %+v", rec.Card, want)
	}
	if _, err := os.Stat(env.W); !os.IsNotExist(err) {
		t.Errorf("worktree kept after an automatic landing: %v", err)
	}
	if env.refOK(item) {
		t.Errorf("branch %s kept after an automatic landing", item)
	}
	// The todo file holds the checks, with the deferred marker written out.
	wantTodo := todoHeader() + "\n" + wantRunHeader + "\n\n- [ ] " + checkOne + "\n- [ ] [AFTER LAND] slicer check --root ../slicer\n"
	if got := readTodo(t, env.P); got != wantTodo {
		t.Errorf("todo file:\n%q\nwant\n%q", got, wantTodo)
	}
	// It is in the landing's commit, and the tree is clean.
	if got := gitRun(t, env.P, "show", "main:human-checks-todo.md"); got+"\n" != wantTodo {
		t.Errorf("todo file on main:\n%q", got)
	}
	if got := gitRun(t, env.P, "status", "--porcelain"); got != "" {
		t.Errorf("main checkout left dirty: %q", got)
	}
	// The slicer note that records the marks is written, as an approval does.
	calls := strings.Join(env.log("slicer.log"), "\n")
	if n := strings.Count(calls, "note "+item+" --text Human checks at approval (S04):"); n != 1 || !strings.Contains(calls, "- later: "+checkOne) {
		t.Errorf("%d approval notes in:\n%s", n, calls)
	}
}

// The todo file is committed with the landing's tracking commit, by path.
func TestAutoLandCommitsTheTodoWithTheTrackingCommit(t *testing.T) {
	env := newGateEnv(t, autoLandCheckBody)
	writeFile(t, filepath.Join(env.fakes, "finish"), "handoff")
	writeFile(t, filepath.Join(env.fakes, "land-writes"), "")
	rec := env.runToRest()
	if rec.State != runrec.StateCommitted {
		t.Fatalf("state %q (reason %q)", rec.State, rec.Reason)
	}
	if got := gitRun(t, env.P, "log", "-1", "--format=%s", "main"); got != item+" done" {
		t.Errorf("main tip %q, want %q", got, item+" done")
	}
	files := strings.Fields(gitRun(t, env.P, "show", "--name-only", "--format=", "main"))
	if !reflect.DeepEqual(files, []string{".slicer/index.json", "human-checks-todo.md"}) {
		t.Errorf("files in the tracking commit: %q", files)
	}
	if got := gitRun(t, env.P, "status", "--porcelain"); got != "" {
		t.Errorf("main checkout left dirty: %q", got)
	}
}

// With nothing to commit under .slicer, the todo file is committed alone.
func TestAutoLandCommitsTheTodoAloneWhenTheTrackingIsClean(t *testing.T) {
	env := newGateEnv(t, autoLandCheckBody)
	env.runToRest()
	if got := gitRun(t, env.P, "log", "-1", "--format=%s", "main"); got != item+" human checks" {
		t.Errorf("main tip %q, want %q", got, item+" human checks")
	}
	files := strings.Fields(gitRun(t, env.P, "show", "--name-only", "--format=", "main"))
	if !reflect.DeepEqual(files, []string{"human-checks-todo.md"}) {
		t.Errorf("files in the commit: %q", files)
	}
}

// A run with no human check appends nothing and makes no todo commit.
func TestAutoLandWithoutHumanChecksTouchesNoTodoFile(t *testing.T) {
	env := newGateEnv(t, "- go test ./...")
	rec := env.runToRest()
	if rec.State != runrec.StateCommitted {
		t.Fatalf("state %q (reason %q)", rec.State, rec.Reason)
	}
	if _, err := os.Stat(filepath.Join(env.P, "human-checks-todo.md")); !os.IsNotExist(err) {
		t.Errorf("a todo file for a run with no checks: %v", err)
	}
	if got := gitRun(t, env.P, "log", "-1", "--format=%s", "main"); got != gitTitle {
		t.Errorf("main tip %q, want the slice's commit %q", got, gitTitle)
	}
}

// A todo file the repo already has keeps its text byte for byte; the run's
// section is appended after it.
func TestAutoLandAppendsToAnExistingTodoFile(t *testing.T) {
	env := newGateEnv(t, autoLandCheckBody)
	old := todoHeader() + "\n## S01 (run ork-20261101-000000-0000, 2026-11-01)\n\n- [ ] an older check\n"
	env.moveBase("human-checks-todo.md", old)
	rec := env.runToRest()
	if rec.State != runrec.StateCommitted {
		t.Fatalf("state %q (reason %q)", rec.State, rec.Reason)
	}
	got := readTodo(t, env.P)
	if !strings.HasPrefix(got, old) {
		t.Errorf("the old text changed:\n%q", got)
	}
	if rest := strings.TrimPrefix(got, old); rest != "\n"+wantRunHeader+"\n\n- [ ] "+checkOne+"\n- [ ] [AFTER LAND] slicer check --root ../slicer\n" {
		t.Errorf("appended text %q", rest)
	}
}

// What leaves a run at the approval card, or at its pause, as today.
func TestAutoLandLeavesARunThatDoesNotQualify(t *testing.T) {
	openOwner := runrec.Finding{Kind: runrec.FindingOwner, Role: RoleReviewer, Round: 1, Title: "Which store", Text: "Which store?", Ask: &runrec.OwnerQuestion{Title: "Which store"}}
	for _, tc := range []struct {
		name  string
		opts  []func(*registry.Project)
		setup func(env *landEnv)
		want  string
	}{
		{"auto_land off", []func(*registry.Project){offAutoLand}, func(*landEnv) {}, runrec.StateAwaitingApproval},
		{"an open owner finding", nil, func(env *landEnv) {
			env.run.Update(func(r *runrec.Record) { r.Findings = []runrec.Finding{openOwner} })
		}, runrec.StateAwaitingApproval},
		{"a red check", nil, func(env *landEnv) {
			env.run.Update(func(r *runrec.Record) {
				r.Checks = []runrec.CheckRecord{{Round: 1, OK: true, Log: "checks-1.log"}, {Round: 1, OK: false, Log: "checks-1.log"}}
			})
		}, runrec.StateAwaitingApproval},
		{"no check recorded", nil, func(env *landEnv) {
			env.run.Update(func(r *runrec.Record) { r.Checks = nil })
		}, runrec.StateAwaitingApproval},
		{"stop after commit", nil, func(env *landEnv) {
			env.run.Update(func(r *runrec.Record) { r.StopAfter = runrec.StopAfterCommit })
		}, runrec.StateAwaitingApproval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newGateEnv(t, autoLandCheckBody, tc.opts...)
			tc.setup(env)
			rec := env.runToRest()
			if rec.State != tc.want {
				t.Fatalf("state %q (reason %q, pending %+v), want %q", rec.State, rec.Reason, rec.Pending, tc.want)
			}
			if len(rec.Approvals) != 0 || !rec.LandedAt.IsZero() || rec.Commit != nil {
				t.Errorf("approved or committed: approvals %+v, commit %+v", rec.Approvals, rec.Commit)
			}
			// The card waits for the owner with its checks open (a deferred one is later).
			if rec.Card == nil || len(rec.Card.HumanChecks) != 2 || rec.Card.HumanChecks[0].State != "" || rec.Card.HumanChecks[1].State != runrec.CheckLater {
				t.Errorf("card %+v", rec.Card)
			}
			if _, err := os.Stat(filepath.Join(env.P, "human-checks-todo.md")); !os.IsNotExist(err) {
				t.Errorf("a todo file before any landing: %v", err)
			}
			if _, err := os.Stat(env.W); err != nil {
				t.Errorf("worktree removed: %v", err)
			}
		})
	}
}

// A run whose approval ork could not record waits for the owner's approve, with
// the reason on the record and the checks marked later on the card.
func TestAutoLandWaitsForTheOwnerWhenTheApprovalNoteFails(t *testing.T) {
	env := newGateEnv(t, autoLandCheckBody)
	replaceFake(t, filepath.Join(env.fakes, "slicer"), strings.Replace(fakeSlicerScript, "\nland)\n", "\nnote) echo \"note broke\" >&2; exit 1;;\nland)\n", 1))
	rec := env.runToRest()
	if rec.State != runrec.StateAwaitingApproval || !strings.Contains(rec.Reason, "automatic approval failed") || !strings.Contains(rec.Reason, "note broke") {
		t.Fatalf("state %q, reason %q", rec.State, rec.Reason)
	}
	if len(rec.Approvals) != 0 || rec.Commit != nil || !rec.LandedAt.IsZero() {
		t.Errorf("approved or landed: %+v", rec.Approvals)
	}
}

// A card that could not read the slice's human checks has none to add to the
// todo file, so the run waits for the owner's approve.
func TestAutoLandWaitsWhenTheCardCouldNotReadTheChecks(t *testing.T) {
	env := newGateEnv(t, autoLandCheckBody)
	replaceFake(t, filepath.Join(env.fakes, "slicer"), strings.Replace(fakeSlicerScript, "show) cat \"$d/show.json\";;", "show) if [ -f \"$d/show-fails\" ]; then echo \"show broke\" >&2; exit 1; fi; cat \"$d/show.json\";;", 1))
	env.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate {
			writeFile(t, filepath.Join(env.fakes, "show-fails"), "") // after the gate's brief read the slice
		}
		return Result{}, nil
	}
	rec := env.runToRest()
	if rec.State != runrec.StateAwaitingApproval || rec.Card == nil || !strings.Contains(rec.Card.ChecksError, "show broke") {
		t.Fatalf("state %q, card %+v", rec.State, rec.Card)
	}
	if len(rec.Approvals) != 0 || rec.Commit != nil {
		t.Errorf("approved: %+v", rec.Approvals)
	}
}

// A landing whose re-check is red goes to its one fix round as for a run
// approved by hand; the run then lands by itself, and its checks are added once.
func TestAutoLandAfterALandingFixRoundLandsAndAppendsOnce(t *testing.T) {
	env := newGateEnv(t, autoLandCheckBody)
	env.moveBase("base.txt", "from main\n")
	// calls 0 and 1: the re-check after the merge is red, and so is its rerun;
	// every later check is green.
	red := CheckOutput{Output: "FAIL TestGolden sidebar\n"}
	env.setCheckOutputs(red, red, CheckOutput{OK: true, Output: "ok\n"})
	rec := env.runToRest()
	if rec.State != runrec.StateCommitted || rec.LandFixes != 1 || len(rec.Approvals) != 2 {
		t.Fatalf("state %q (reason %q), land fixes %d, approvals %d", rec.State, rec.Reason, rec.LandFixes, len(rec.Approvals))
	}
	if n := strings.Count(readTodo(t, env.P), wantRunHeader); n != 1 {
		t.Errorf("%d sections for the run:\n%s", n, readTodo(t, env.P))
	}
	if n := strings.Count(strings.Join(env.log("slicer.log"), "\n"), "note "+item+" --text Human checks at approval"); n != 1 {
		t.Errorf("%d approval notes, want the one the first approval wrote", n)
	}
}

// The same run, with the owner finding answered, lands: the open finding is the
// one variable that held it back.
func TestAutoLandLandsWhenTheOwnerFindingIsAnswered(t *testing.T) {
	env := newGateEnv(t, autoLandCheckBody)
	env.run.Update(func(r *runrec.Record) {
		r.Findings = []runrec.Finding{{Kind: runrec.FindingOwner, Role: RoleReviewer, Round: 1, Title: "Which store", Ask: &runrec.OwnerQuestion{Title: "Which store"}, Answer: "the first"}}
	})
	if rec := env.runToRest(); rec.State != runrec.StateCommitted {
		t.Fatalf("state %q (reason %q)", rec.State, rec.Reason)
	}
}

// A later finding is not an owner finding and does not hold the run back.
func TestAutoLandIgnoresALaterFinding(t *testing.T) {
	env := newGateEnv(t, autoLandCheckBody)
	env.run.Update(func(r *runrec.Record) {
		r.Findings = []runrec.Finding{{Kind: runrec.FindingLater, Role: RoleReviewer, Round: 1, Title: "Tidy the helper"}}
	})
	if rec := env.runToRest(); rec.State != runrec.StateCommitted {
		t.Fatalf("state %q (reason %q)", rec.State, rec.Reason)
	}
}

// A gate FAIL goes back to the implementer as today, and nothing is appended.
func TestAutoLandDoesNotLandAGateFail(t *testing.T) {
	env := newGateEnv(t, autoLandCheckBody)
	env.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleGate {
			return Result{Exit: ExitOK, Session: "gate", Report: gateFail}, nil
		}
		return Result{}, nil
	}
	rec := env.runToRest()
	if rec.State == runrec.StateCommitted || rec.State == runrec.StateAwaitingApproval || rec.Fails == 0 {
		t.Fatalf("state %q, fails %d; want a run sent back after the FAIL", rec.State, rec.Fails)
	}
	if len(rec.Approvals) != 0 || !rec.LandedAt.IsZero() {
		t.Errorf("approved or landed: %+v", rec.Approvals)
	}
	if _, err := os.Stat(filepath.Join(env.P, "human-checks-todo.md")); !os.IsNotExist(err) {
		t.Errorf("a todo file after a FAIL: %v", err)
	}
}

// A landing pause is the same as for an approved run, and `ork retry`
// continues the automatic landing; the run's section is appended once.
func TestAutoLandPauseThenRetryLandsAndAppendsOnce(t *testing.T) {
	env := newGateEnv(t, autoLandCheckBody, func(p *registry.Project) { p.Push = true })
	writeFile(t, filepath.Join(env.fakes, "push-fails"), "")
	rec := env.runToRest()
	if rec.State != runrec.StatePaused || rec.Pending == nil || rec.Pending.State != runrec.StateLand || !strings.Contains(rec.Reason, "git push") {
		t.Fatalf("state %q, pending %+v, reason %q", rec.State, rec.Pending, rec.Reason)
	}
	if strings.Count(readTodo(t, env.P), wantRunHeader) != 1 {
		t.Fatalf("the paused landing did not append the section once:\n%s", readTodo(t, env.P))
	}
	if err := os.Remove(filepath.Join(env.fakes, "push-fails")); err != nil {
		t.Fatal(err)
	}
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, env.e, runrec.StateCommitted)
	if n := strings.Count(readTodo(t, env.P), wantRunHeader); n != 1 {
		t.Errorf("%d sections for the run after the retry:\n%s", n, readTodo(t, env.P))
	}
	if got := gitRun(t, env.P, "status", "--porcelain"); got != "" {
		t.Errorf("main checkout left dirty: %q", got)
	}
}

// An approval by hand lands too and adds the checks the owner marked later;
// a check the owner ticked is not added.
func TestManualApprovalAppendsOnlyTheChecksMarkedLater(t *testing.T) {
	env := newLandEnv(t, offAutoLand)
	if err := env.run.Update(func(r *runrec.Record) {
		r.Card.HumanChecks = []runrec.HumanCheck{
			{Text: checkOne, State: runrec.CheckTicked},
			{Text: checkTwo, State: runrec.CheckLater},
			{Text: deferredTwo, State: runrec.CheckLater},
		}
	}); err != nil {
		t.Fatal(err)
	}
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	want := todoHeader() + "\n" + wantRunHeader + "\n\n- [ ] " + checkTwo + "\n- [ ] [AFTER LAND] slicer check --root ../slicer\n"
	if got := readTodo(t, env.P); got != want {
		t.Errorf("todo file:\n%q\nwant\n%q", got, want)
	}
}

func TestLaterChecksReplaceTheDeferredPrefix(t *testing.T) {
	card := &runrec.ApprovalCard{HumanChecks: []runrec.HumanCheck{
		{Text: "open", State: ""},
		{Text: "ticked", State: runrec.CheckTicked},
		{Text: "plain", State: runrec.CheckLater},
		{Text: deferredPrefix + "after the rebuild", State: runrec.CheckLater},
	}}
	if got, want := laterChecks(card), []string{"plain", "[AFTER LAND] after the rebuild"}; !reflect.DeepEqual(got, want) {
		t.Errorf("laterChecks = %q, want %q", got, want)
	}
	if got := laterChecks(nil); len(got) != 0 {
		t.Errorf("laterChecks(nil) = %q", got)
	}
}

// The todo file (S331, *proposed*).

func todoHeader() string {
	return "# Human checks to do\n\n" +
		"Checks that a person runs by hand. They never block a landing: a slice that passes its gates and checks lands, and its human checks are added here (owner decision 2026-11-11, adj 281). The owner usually tests after a release, and anything found becomes a fix slice.\n\n" +
		"Tick a box when the check is done, or delete the line. Write `-> SNNN` after a check that found a problem, naming the fix slice.\n"
}

func TestTodoFileMissingIsCreatedWithTheHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "human-checks-todo.md")
	if err := appendHumanChecksTodo(path, "S09", "ork-20261111-010203-abcd", "2026-11-11", []string{"first", "second"}); err != nil {
		t.Fatal(err)
	}
	want := todoHeader() + "\n## S09 (run ork-20261111-010203-abcd, 2026-11-11)\n\n- [ ] first\n- [ ] second\n"
	if b, _ := os.ReadFile(path); string(b) != want {
		t.Errorf("file:\n%q\nwant\n%q", b, want)
	}
}

func TestTodoFileGetsOneSectionAndKeepsTheRestByteForByte(t *testing.T) {
	path := filepath.Join(t.TempDir(), "human-checks-todo.md")
	old := todoHeader() + "\n## S01 (run r1, 2026-11-01)\n\n- [x] done one\n- [ ] open  one \t\n\nfree text, no trailing blank\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendHumanChecksTodo(path, "S02", "r2", "2026-11-02", []string{"new check"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), old) {
		t.Fatalf("the old text changed:\n%q", b)
	}
	if rest := strings.TrimPrefix(string(b), old); rest != "\n## S02 (run r2, 2026-11-02)\n\n- [ ] new check\n" {
		t.Errorf("appended %q", rest)
	}
}

// A file whose last line has no newline keeps its text; a newline is added
// before the section.
func TestTodoFileWithoutAFinalNewlineKeepsItsText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "human-checks-todo.md")
	old := "# Human checks to do\n\n- [ ] last line"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendHumanChecksTodo(path, "S02", "r2", "2026-11-02", []string{"c"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if want := old + "\n\n## S02 (run r2, 2026-11-02)\n\n- [ ] c\n"; string(b) != want {
		t.Errorf("file %q, want %q", b, want)
	}
}

// The same run is never added twice, so a retried landing appends once.
func TestTodoFileSkipsARunAlreadyThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "human-checks-todo.md")
	for i := 0; i < 2; i++ {
		if err := appendHumanChecksTodo(path, "S02", "r2", "2026-11-02", []string{"c"}); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(path)
	if n := strings.Count(string(b), "## S02 (run r2,"); n != 1 {
		t.Errorf("%d sections:\n%s", n, b)
	}
	// Another run of the same item is its own section.
	if err := appendHumanChecksTodo(path, "S02", "r3", "2026-11-03", []string{"c"}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if n := strings.Count(string(b), "## S02 (run "); n != 2 {
		t.Errorf("%d sections after a second run:\n%s", n, b)
	}
}

func TestTodoFileWithNoChecksIsNotCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "human-checks-todo.md")
	if err := appendHumanChecksTodo(path, "S02", "r2", "2026-11-02", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a file for no checks: %v", err)
	}
}
