// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
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
			Seq: []clitest.Reply{good, good, good, {Stderr: "slicer: ledger busy", Exit: 1}}})
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
