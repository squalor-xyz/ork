// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// reviewerSays makes the reviewer give report on its first round and a clean
// review afterwards.
func reviewerSays(h *harness, report string) {
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleReviewer && req.Round == 1 && !strings.Contains(req.Brief, "Owner answer") {
			return Result{Exit: ExitOK, Session: "sess-reviewer", Report: report}, nil
		}
		return Result{}, nil
	}
}

func TestFindingKindsRouted(t *testing.T) {
	h := newHarness(t)
	reviewerSays(h, review(fixFinding+"\n"+ownerFinding+"\n"+laterFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)

	// fix: the fix round gets it, and only it.
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "implementer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	fix := h.runner.requests()[2].Brief
	if !strings.Contains(fix, "engine.go:42 drops the error.") {
		t.Errorf("the fix round lacks the fix finding:\n%s", fix)
	}
	for _, not := range []string{"Should the cache be shared", "--json flag", "isolation is simpler"} {
		if strings.Contains(fix, not) {
			t.Errorf("the fix round carries a non-fix finding %q:\n%s", not, fix)
		}
	}

	// owner: a structured question, recorded and not blocking.
	// later: an offer.
	if len(rec.Findings) != 2 {
		t.Fatalf("findings %+v, want one owner and one later", rec.Findings)
	}
	owner, later := rec.Findings[0], rec.Findings[1]
	if owner.Kind != runrec.FindingOwner || owner.Role != RoleReviewer || owner.Round != 1 || owner.Title != "Should the cache be shared across projects?" {
		t.Errorf("owner finding %+v", owner)
	}
	if owner.Ask == nil || len(owner.Ask.Options) != 2 || owner.Ask.Recommend != 2 || owner.Ask.Options[0].Label != "shared" {
		t.Errorf("owner finding question %+v", owner.Ask)
	}
	if later.Kind != runrec.FindingLater || later.Title != "Add a --json flag to ork status" || later.Filed != "" {
		t.Errorf("later finding %+v", later)
	}
	if len(rec.Questions) != 0 || rec.Pending != nil {
		t.Errorf("a non-blocking owner finding must not ask or pause: questions %+v, pending %+v", rec.Questions, rec.Pending)
	}
	if n := len(h.calls("slicer", "add")); n != 0 {
		t.Errorf("a later finding is only offered; slicer add ran %d times", n)
	}
}

func TestFindingKindsWithoutKindsAreAllFix(t *testing.T) {
	h := newHarness(t)
	reviewerSays(h, findingsReview("1. engine.go:42 drops the error."))
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "implementer-2", "gate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	if len(rec.Findings) != 0 {
		t.Errorf("findings %+v", rec.Findings)
	}
}

func TestOnlyNonFixFindingsStartNoFixRound(t *testing.T) {
	h := newHarness(t)
	reviewerSays(h, review(laterFinding+"\n"+ownerFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "gate-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role rounds %v, want %v", got, want)
	}
	if len(rec.Findings) != 2 {
		t.Errorf("findings %+v", rec.Findings)
	}
}

func TestAFindingKeptOnceAcrossRounds(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleReviewer {
			return Result{Exit: ExitOK, Session: "sess-reviewer", Report: review(laterFinding, "nothing", "nothing", "nothing")}, nil
		}
		if req.Role == RoleGate && req.Round == 1 {
			return Result{Exit: ExitOK, Report: "VERDICT: FAIL\n\n- engine.go:10 missing nil check\n- [later] Add a --json flag to ork status\nTests: 1 passed, 0 failed\n\n## Owner needed\n\nnone\n"}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if len(rec.Findings) != 1 {
		t.Errorf("the same later finding from two reports is offered once: %+v", rec.Findings)
	}
	fix := h.runner.requests()[3].Brief
	if !strings.Contains(fix, "engine.go:10 missing nil check") || strings.Contains(fix, "--json flag") {
		t.Errorf("the fix round after a gate FAIL carries only the fix findings:\n%s", fix)
	}
	notes := h.calls("slicer", "note")
	if len(notes) != 1 || !strings.Contains(notes[0][3], "engine.go:10 missing nil check") || strings.Contains(notes[0][3], "--json flag") {
		t.Errorf("the Review FAIL note carries only the fix findings: %q", notes)
	}
}

func TestOwnerFindingBlocksOnlyWhenMarked(t *testing.T) {
	t.Run("unmarked does not block", func(t *testing.T) {
		h := newHarness(t)
		reviewerSays(h, review(ownerFinding, "nothing", "nothing", "nothing"))
		e := h.start()
		rec := requireState(t, e, runrec.StateAwaitingApproval)
		if len(rec.Questions) != 0 || rec.Pending != nil || len(rec.Findings) != 1 {
			t.Errorf("questions %+v, pending %+v, findings %+v", rec.Questions, rec.Pending, rec.Findings)
		}
	})

	t.Run("blocker pauses the reviewer round on a structured question", func(t *testing.T) {
		h := newHarness(t)
		reviewerSays(h, review(blockerFinding, "nothing", "nothing", "nothing"))
		e := h.start()
		rec := requireState(t, e, runrec.StatePaused)
		if rec.Pending == nil || rec.Pending.Kind != runrec.PendingQuestion || rec.Pending.State != runrec.StateReview || rec.Pending.Role != RoleReviewer {
			t.Fatalf("pending %+v", rec.Pending)
		}
		if ask := rec.Pending.Ask; ask == nil || ask.Title != "Drop the old wire field now?" || len(ask.Options) != 2 || ask.Recommend != 1 {
			t.Errorf("pending question %+v", rec.Pending.Ask)
		}
		if rec.Pending.Key != "wire.old-field" {
			t.Errorf("pending key %q", rec.Pending.Key)
		}
		if len(rec.Questions) != 1 || rec.Questions[0].Ask == nil || rec.Questions[0].Role != RoleReviewer {
			t.Errorf("questions %+v", rec.Questions)
		}
		if len(rec.Findings) != 0 {
			t.Errorf("a blocker is asked, not kept as a finding: %+v", rec.Findings)
		}
		if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("role rounds %v, want %v", got, want)
		}

		// The answer reaches the reviewer, who may now give a clean review.
		if err := e.Answer(context.Background(), "drop"); err != nil {
			t.Fatal(err)
		}
		requireState(t, e, runrec.StateAwaitingApproval)
		reqs := h.runner.requests()
		if again := reqs[2]; again.Role != RoleReviewer || again.Resume != "sess-reviewer" || !strings.Contains(again.Brief, "drop") || !strings.Contains(again.Brief, "Owner answer") {
			t.Errorf("the reviewer is asked again with the answer: %+v\n%s", again.Role, again.Brief)
		}
	})

	t.Run("blocker in a gate PASS blocks the gate", func(t *testing.T) {
		h := newHarness(t)
		h.runner.script = func(req Request, n int) (Result, error) {
			if req.Role == RoleGate && !strings.Contains(req.Brief, "Owner answer") {
				return Result{Exit: ExitOK, Session: "sess-gate", Report: "VERDICT: PASS\n\n" + blockerFinding + "\nTests: 12 passed, 0 failed\n\n## Owner needed\n\nnone\n"}, nil
			}
			return Result{}, nil
		}
		e := h.start()
		rec := requireState(t, e, runrec.StatePaused)
		if rec.Pending == nil || rec.Pending.Kind != runrec.PendingQuestion || rec.Pending.State != runrec.StateGate || rec.Pending.Role != RoleGate || rec.Pending.Ask == nil {
			t.Fatalf("pending %+v", rec.Pending)
		}
		if err := e.Answer(context.Background(), "keep"); err != nil {
			t.Fatal(err)
		}
		requireState(t, e, runrec.StateAwaitingApproval)
	})

	t.Run("unmarked in a gate PASS does not", func(t *testing.T) {
		h := newHarness(t)
		h.runner.script = func(req Request, n int) (Result, error) {
			if req.Role == RoleGate {
				return Result{Exit: ExitOK, Report: "VERDICT: PASS\n\n" + ownerFinding + "\n" + laterFinding + "\nTests: 12 passed, 0 failed\n\n## Owner needed\n\nnone\n"}, nil
			}
			return Result{}, nil
		}
		e := h.start()
		rec := requireState(t, e, runrec.StateAwaitingApproval)
		if len(rec.Findings) != 2 {
			t.Errorf("findings %+v", rec.Findings)
		}
	})
}

func TestMalformedOwnerFindingPausesOnAShapeError(t *testing.T) {
	h := newHarness(t)
	reviewerSays(h, review("- [owner] Which one?", "nothing", "nothing", "nothing"))
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape || !strings.Contains(rec.Pending.Reason, "Which one?") {
		t.Errorf("pending %+v", rec.Pending)
	}
}

// fileRoutes answer the two calls that file a later finding.
func fileRoutes(id string) func(*harnessOpts) {
	return func(o *harnessOpts) {
		o.routes = append(o.routes,
			clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"id":"` + id + `","title":"x","status":"open","existing":false}`},
			clitest.Route{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{"id":"` + id + `"}`},
		)
	}
}

func TestLaterFindingFiledOnConfirm(t *testing.T) {
	h := newHarness(t, fileRoutes("S130"))
	reviewerSays(h, review(laterFinding+"\n  Status prints text only today.", "nothing", "nothing", "nothing"))
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if len(rec.Findings) != 1 {
		t.Fatalf("findings %+v", rec.Findings)
	}
	if n := len(h.calls("slicer", "add")) + len(h.calls("slicer", "promote")); n != 0 {
		t.Fatalf("nothing is filed before the owner confirms; %d filing calls", n)
	}

	id, err := e.FileLater(context.Background(), 1)
	if err != nil || id != "S130" {
		t.Fatalf("FileLater: %q, %v", id, err)
	}
	adds := h.calls("slicer", "add")
	if len(adds) != 1 {
		t.Fatalf("slicer add calls %q", adds)
	}
	key := rec.ID + "-later-1"
	wantAdd := []string{"add", "Add a --json flag to ork status", "--discovered-from", item, "--key", key, "--render", "--json", "--lean", "--root", h.P}
	if !reflect.DeepEqual(adds[0], wantAdd) {
		t.Errorf("slicer add\n got %q\nwant %q", adds[0], wantAdd)
	}
	promotes := h.calls("slicer", "promote")
	if len(promotes) != 1 || len(promotes[0]) != 9 || promotes[0][1] != "S130" || promotes[0][2] != "--file" {
		t.Fatalf("slicer promote calls %q", promotes)
	}
	outline, err := os.ReadFile(promotes[0][3])
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(promotes[0][3]) != h.store.RunDir(rec.ID) {
		t.Errorf("the outline is written in the run directory, not %s", promotes[0][3])
	}
	for _, want := range []string{"## Add a --json flag to ork status", "### Why", "Status prints text only today.", item, "reviewer"} {
		if !strings.Contains(string(outline), want) {
			t.Errorf("outline lacks %q:\n%s", want, outline)
		}
	}
	if want := []string{"--render", "--json", "--lean", "--root", h.P}; !reflect.DeepEqual(promotes[0][4:], want) {
		t.Errorf("promote flags %q, want %q", promotes[0][4:], want)
	}

	after := e.Snapshot()
	if after.Findings[0].Filed != "S130" || after.Findings[0].FiledAt.IsZero() {
		t.Errorf("finding after filing %+v", after.Findings[0])
	}
	var logged int
	for _, w := range after.Writes {
		if w.Tool == "slicer" && (w.Argv[0] == "add" || w.Argv[0] == "promote") && w.Root == h.P {
			logged++
		}
	}
	if logged != 2 {
		t.Errorf("writes[] holds %d filing writes in the project root, want 2: %+v", logged, after.Writes)
	}

	// A second confirm files nothing more.
	if _, err := e.FileLater(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "S130") {
		t.Errorf("filing twice: %v", err)
	}
	if n := len(h.calls("slicer", "add")); n != 1 {
		t.Errorf("slicer add ran %d times", n)
	}
}

func TestFileLaterRefusesWhatIsNotALaterFinding(t *testing.T) {
	h := newHarness(t, fileRoutes("S130"))
	reviewerSays(h, review(ownerFinding+"\n"+laterFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	for _, n := range []int{0, 3, -1, 1} {
		if _, err := e.FileLater(context.Background(), n); err == nil {
			t.Errorf("FileLater(%d) accepted", n)
		}
	}
	if len(h.calls("slicer", "add")) != 0 {
		t.Error("slicer add ran")
	}
	if id, err := e.FileLater(context.Background(), 2); err != nil || id != "S130" {
		t.Errorf("FileLater of the later finding: %q, %v", id, err)
	}
	adds := h.calls("slicer", "add")
	if len(adds) != 1 || !strings.HasSuffix(adds[0][5], "-later-2") {
		t.Errorf("the filing key names the finding: %q", adds)
	}
}

func TestFileLaterKeepsTheFindingOpenWhenSlicerFails(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Exit: 2, Stderr: "slicer: no such item"})
	})
	reviewerSays(h, review(laterFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	if _, err := e.FileLater(context.Background(), 1); err == nil {
		t.Fatal("a failed slicer add is an error")
	}
	found := e.Snapshot().Findings
	if len(found) != 1 || found[0].Filed != "" {
		t.Errorf("finding marked filed after a failure: %+v", found)
	}
}

func TestFileLaterRetriesAfterAFailedWhySection(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routes = append(o.routes,
			clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"id":"S130","existing":false}`},
			clitest.Route{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Seq: []clitest.Reply{{Exit: 2, Stderr: "slicer: boom"}, {Stdout: `{"id":"S130"}`}}},
		)
	})
	reviewerSays(h, review(laterFinding, "nothing", "nothing", "nothing"))
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)

	_, err := e.FileLater(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "S130") || !strings.Contains(err.Error(), "Why") {
		t.Fatalf("a failed promote names the filed slice: %v", err)
	}
	if f := e.Snapshot().Findings[0]; f.Filed != "" {
		t.Fatalf("a finding without its Why section is not filed: %+v", f)
	}
	id, err := e.FileLater(context.Background(), 1)
	if err != nil || id != "S130" {
		t.Fatalf("retry: %q, %v", id, err)
	}
	adds := h.calls("slicer", "add")
	if len(adds) != 2 || !reflect.DeepEqual(adds[0], adds[1]) {
		t.Errorf("the retry uses the same filing key: %q", adds)
	}
	if f := e.Snapshot().Findings[0]; f.Filed != "S130" {
		t.Errorf("finding after the retry %+v", f)
	}
}

// A blocker raised in the same round as another role's pause is asked once
// that pause is answered: the queued blocker is shown before roles rerun.
func TestBlockerBesideAnotherRolesPauseIsStillAsked(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	uiDone := make(chan struct{})
	var reviewerCalls int
	h.runner.script = func(req Request, _ int) (Result, error) {
		switch req.Role {
		case RoleUI:
			if strings.Contains(req.Brief, "Owner answer") {
				return Result{}, nil
			}
			defer close(uiDone)
			return Result{Exit: ExitOK, Session: "sess-ui", Report: strings.Replace(cleanReview, "none", "Which library?", 1)}, nil
		case RoleReviewer:
			reviewerCalls++
			if reviewerCalls == 1 {
				<-uiDone
				time.Sleep(200 * time.Millisecond) // the UI pass has paused the slice by now
				return Result{Exit: ExitOK, Session: "sess-reviewer", Report: review(blockerFinding, "nothing", "nothing", "nothing")}, nil
			}
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Role != RoleUI {
		t.Fatalf("the UI pass paused first: %+v", rec.Pending)
	}
	if err := e.Answer(context.Background(), "stdlib"); err != nil {
		t.Fatal(err)
	}
	var reviewerRounds int
	for _, r := range h.runner.roles() {
		if strings.HasPrefix(r, "reviewer") {
			reviewerRounds++
		}
	}
	if reviewerRounds != 1 {
		t.Errorf("reviewer reran before its queued question was answered: %v", h.runner.roles())
	}
	rec = requireState(t, e, runrec.StatePaused)
	if rec.Pending.Role != RoleReviewer || rec.Pending.Ask == nil || rec.Pending.Ask.Title != "Drop the old wire field now?" {
		t.Fatalf("queued blocker not shown: %+v", rec.Pending)
	}
	if err := e.Answer(context.Background(), "keep"); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
}

func TestUIPassFindingsAreSortedByKindToo(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleUI && req.Round == 1 {
			return Result{Exit: ExitOK, Session: "sess-ui", Report: review("- [fix] model.go:10 the key hint clips at 80 columns\n"+laterFinding, "nothing", "nothing", "nothing")}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	fix := ""
	for _, req := range h.runner.requests() {
		if req.Role == RoleImplementer && req.Round == 2 {
			fix = req.Brief
		}
	}
	if !strings.Contains(fix, "model.go:10 the key hint clips at 80 columns") || !strings.Contains(fix, "### UI pass report (ui-1)") || strings.Contains(fix, "--json flag") {
		t.Errorf("fix round brief:\n%s", fix)
	}
	if len(rec.Findings) != 1 || rec.Findings[0].Role != RoleUI || rec.Findings[0].Kind != runrec.FindingLater {
		t.Errorf("findings %+v", rec.Findings)
	}
}

// A retry after the Why section was written: slicer add finds the item by its
// key, already specified, and promote would refuse it. Only that combination
// skips promote.
func TestFileLaterSkipsPromoteWhenTheSliceAlreadyHasItsSpec(t *testing.T) {
	for name, tc := range map[string]struct {
		add      string
		promotes int
	}{
		"existing with spec":    {`{"id":"S130","existing":true,"has_slice":true}`, 0},
		"existing without spec": {`{"id":"S130","existing":true,"has_slice":false}`, 1},
		"new":                   {`{"id":"S130","existing":false,"has_slice":false}`, 1},
		"new with a spec":       {`{"id":"S130","existing":false,"has_slice":true}`, 1},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.routes = append(o.routes,
					clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: tc.add},
					clitest.Route{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{"id":"S130"}`},
				)
			})
			reviewerSays(h, review(laterFinding, "nothing", "nothing", "nothing"))
			e := h.start()
			requireState(t, e, runrec.StateAwaitingApproval)
			id, err := e.FileLater(context.Background(), 1)
			if err != nil || id != "S130" {
				t.Fatalf("FileLater: %q, %v", id, err)
			}
			if n := len(h.calls("slicer", "promote")); n != tc.promotes {
				t.Errorf("promote ran %d times, want %d", n, tc.promotes)
			}
			if f := e.Snapshot().Findings[0]; f.Filed != "S130" {
				t.Errorf("finding %+v", f)
			}
		})
	}
}
