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

// S316 (*proposed*; adj 233, 234): the owner defers every open human check in
// one step, with MarkOpenLater or with ApproveLater, which then approves.

func checkStates(card *runrec.ApprovalCard) []string {
	var got []string
	for _, c := range card.HumanChecks {
		got = append(got, c.State)
	}
	return got
}

func TestMarkOpenLaterChangesOnlyTheOpenChecks(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne, checkTwo, checkThree))
	e := h.start()
	if err := e.MarkCheck(1, runrec.CheckTicked); err != nil {
		t.Fatal(err)
	}
	if err := e.MarkCheck(2, runrec.CheckLater); err != nil {
		t.Fatal(err)
	}
	deferred, err := e.MarkOpenLater()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"3. " + checkThree}; !reflect.DeepEqual(deferred, want) {
		t.Errorf("deferred %q, want %q", deferred, want)
	}
	want := []string{runrec.CheckTicked, runrec.CheckLater, runrec.CheckLater}
	if got := checkStates(e.Snapshot().Card); !reflect.DeepEqual(got, want) {
		t.Errorf("states %q, want %q", got, want)
	}
	// Nothing is open now: a second call changes nothing and names nothing.
	if deferred, err := e.MarkOpenLater(); err != nil || len(deferred) != 0 {
		t.Errorf("second call: %q, %v", deferred, err)
	}
	if got := checkStates(e.Snapshot().Card); !reflect.DeepEqual(got, want) {
		t.Errorf("states after the second call %q, want %q", got, want)
	}
	// The slice is still awaiting approval: marking does not approve.
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if len(rec.Approvals) != 0 {
		t.Errorf("approvals %+v", rec.Approvals)
	}
}

// The state it writes is the state ork check RUN N later writes.
func TestMarkOpenLaterWritesWhatMarkCheckLaterWrites(t *testing.T) {
	one := newHarness(t, withHumanChecks(checkOne, checkTwo))
	a := one.start()
	if _, err := a.MarkOpenLater(); err != nil {
		t.Fatal(err)
	}
	two := newHarness(t, withHumanChecks(checkOne, checkTwo))
	b := two.start()
	for n := 1; n <= 2; n++ {
		if err := b.MarkCheck(n, runrec.CheckLater); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := a.Snapshot().Card.HumanChecks, b.Snapshot().Card.HumanChecks; !reflect.DeepEqual(got, want) {
		t.Errorf("card checks %+v, want %+v", got, want)
	}
	// The follow-up list is built from the card, so it is the same too.
	if got, want := laterChecks(a.Snapshot().Card), laterChecks(b.Snapshot().Card); len(got) != 2 || !reflect.DeepEqual(got, want) {
		t.Errorf("follow-up list %q, want %q", got, want)
	}
}

func TestMarkOpenLaterNamesTheFirst80CharactersOfAText(t *testing.T) {
	long := strings.Repeat("abcdefghij", 10)
	h := newHarness(t, withHumanChecks(long, checkOne))
	deferred, err := h.start().MarkOpenLater()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1. " + long[:80] + "...", "2. " + checkOne}
	if !reflect.DeepEqual(deferred, want) {
		t.Errorf("deferred %q, want %q", deferred, want)
	}
}

func TestMarkOpenLaterIsRefusedOutsideAwaitingApproval(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne))
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.MarkOpenLater(); err == nil || !strings.Contains(err.Error(), "not awaiting approval") {
		t.Errorf("before the run: %v", err)
	}
}

func TestApproveLaterDefersOpenChecksAndApproves(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne, checkTwo))
	e := h.start()
	deferred, err := e.ApproveLater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1. " + checkOne, "2. " + checkTwo}; !reflect.DeepEqual(deferred, want) {
		t.Errorf("deferred %q, want %q", deferred, want)
	}
	rec := requireState(t, e, runrec.StateCommitted)
	if got, want := checkStates(rec.Card), []string{runrec.CheckLater, runrec.CheckLater}; !reflect.DeepEqual(got, want) {
		t.Errorf("states %q, want %q", got, want)
	}
	// The slicer note records them as later, as a per-check mark does.
	notes := h.calls("slicer", "note")
	if len(notes) != 1 || !strings.Contains(notes[0][3], "later: "+checkOne) || !strings.Contains(notes[0][3], "later: "+checkTwo) {
		t.Errorf("notes %v", notes)
	}
}

func TestApproveLaterKeepsTickedAndLaterChecks(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne, checkTwo, checkThree))
	e := h.start()
	if err := e.MarkCheck(1, runrec.CheckTicked); err != nil {
		t.Fatal(err)
	}
	if err := e.MarkCheck(2, runrec.CheckLater); err != nil {
		t.Fatal(err)
	}
	deferred, err := e.ApproveLater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"3. " + checkThree}; !reflect.DeepEqual(deferred, want) {
		t.Errorf("deferred %q, want %q", deferred, want)
	}
	rec := requireState(t, e, runrec.StateCommitted)
	if got, want := checkStates(rec.Card), []string{runrec.CheckTicked, runrec.CheckLater, runrec.CheckLater}; !reflect.DeepEqual(got, want) {
		t.Errorf("states %q, want %q", got, want)
	}
}

func TestApproveLaterWithNoChecksApprovesAsBefore(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	deferred, err := e.ApproveLater(context.Background())
	if err != nil || len(deferred) != 0 {
		t.Fatalf("deferred %q, err %v", deferred, err)
	}
	rec := requireState(t, e, runrec.StateCommitted)
	if len(rec.Approvals) != 1 {
		t.Errorf("approvals %+v", rec.Approvals)
	}
}

// A refused plain approval names the way out.
func TestApprovalRefusalMentionsLaterChecks(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne, checkTwo))
	err := h.start().Approve(context.Background())
	if err == nil || !strings.Contains(err.Error(), "2 human checks still open") || !strings.Contains(err.Error(), "--later-checks") ||
		!strings.Contains(err.Error(), "1. "+checkOne) || !strings.Contains(err.Error(), "2. "+checkTwo) {
		t.Fatalf("refusal: %v", err)
	}
}

// An approval that fails after the checks were deferred puts them back, so the
// owner who never saw the list is not left with checks marked later.
func TestApproveLaterRestoresTheChecksWhenTheApprovalFails(t *testing.T) {
	h := newHarness(t, withHumanChecks(checkOne, checkTwo), func(o *harnessOpts) {
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"note", item}, Prefix: true, Stderr: "slicer: cannot write the note\n", Exit: 1})
	})
	e := h.start()
	if err := e.MarkCheck(1, runrec.CheckTicked); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ApproveLater(context.Background()); err == nil {
		t.Fatal("approval with a failing note succeeded")
	}
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if got, want := checkStates(rec.Card), []string{runrec.CheckTicked, ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("states %q, want %q", got, want)
	}
}
