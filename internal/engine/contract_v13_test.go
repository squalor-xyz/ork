// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestEngineStartWithV13CreatesARun(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) { o.contract = 13 })
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.Snapshot(); rec.Contract != 13 || rec.State != runrec.StateSetup {
		t.Fatalf("contract/state = %d/%s, want 13/setup", rec.Contract, rec.State)
	}
}

func v13(o *harnessOpts) { o.contract = 13 }

// withOutcome is an implementer report that carries the Outcome line a plain
// v13 run needs (S384).
func withOutcome(text string) string { return "Outcome: " + text + "\n" + implReport }

// outcomeRunner answers each non-finish implementer round with the Outcome of
// its round number (rounds past the list repeat the last).
func outcomeRunner(h *harness, outcomes ...string) {
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleImplementer && !strings.Contains(req.Brief, "## Finish step") {
			r := defaultResult(req)
			r.Report = withOutcome(outcomes[min(req.Round, len(outcomes))-1])
			return r, nil
		}
		return Result{}, nil
	}
}

// finishOrder lists, in call order, the note and done or handoff calls of
// slicer and the commit of git.
func finishOrder(h *harness) []string {
	var order []string
	for _, c := range h.fake.Calls() {
		switch {
		case c.Tool == "git" && len(c.Args) > 3 && c.Args[2] == "commit":
			order = append(order, "commit")
		case c.Tool == "slicer" && len(c.Args) > 0 && (c.Args[0] == "note" || c.Args[0] == "done" || c.Args[0] == "handoff"):
			order = append(order, c.Args[0])
		}
	}
	return order
}

func TestFinishRunsSlicerWithoutARole(t *testing.T) {
	h := newHarness(t, v13)
	outcomeRunner(h, "Verified engine behavior.")
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	before := h.runner.roles()
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateCommitted)
	if rec.Commit == nil {
		t.Fatal("no commit")
	}
	if got := h.runner.roles(); strings.Join(got, ",") != strings.Join(before, ",") || before[len(before)-1] != "gate-1" {
		t.Fatalf("roles after approval %v, before %v: the role list must end at the gate", got, before)
	}
	if got := strings.Join(finishOrder(h), ","); got != "note,done,commit" {
		t.Fatalf("order %s, want note,done,commit", got)
	}
	if done := h.calls("slicer", "done"); len(done) != 1 || join(done[0]) != "done S04 --note=Verified engine behavior. --render --check --json --root "+h.W {
		t.Errorf("done call: %v", done)
	}
	if end := h.calls("slicer", "relay-run", "end"); len(end) != 1 || !strings.Contains(join(end[0]), "--outcome pass --close-steps done") {
		t.Errorf("relay-run end: %v", end)
	}
}

func TestFinishUsesTheOutcomeLine(t *testing.T) {
	h := newHarness(t, v13)
	h.runner.script = func(req Request, n int) (Result, error) {
		switch {
		case req.Role == RoleReviewer:
			return Result{Exit: ExitOK, Session: "sess-reviewer", Report: findingsReview("1. engine.go:42 drops the error.")}, nil
		case req.Role == RoleImplementer && strings.Contains(req.Brief, "This is fix round"):
			r := defaultResult(req)
			r.Report = withOutcome("Fixed outcome.")
			return r, nil
		case req.Role == RoleImplementer:
			r := defaultResult(req)
			r.Report = withOutcome("First outcome.")
			return r, nil
		}
		return Result{}, nil
	}
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	if got := strings.Join(h.runner.roles(), ","); got != "implementer-1,reviewer-1,implementer-2,gate-2" {
		t.Fatalf("rounds %s", got)
	}
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateCommitted)
	note := h.calls("slicer", "note")
	if len(note) != 1 || note[0][2] != "--text=Fixed outcome." {
		t.Fatalf("note calls %v, want the Outcome of the last accepted implementer report", note)
	}
	if done := h.calls("slicer", "done"); len(done) != 1 || done[0][2] != "--note=Fixed outcome." {
		t.Fatalf("done calls %v", done)
	}
}

func TestImplementerReportNeedsOutcomeOnV13(t *testing.T) {
	h := newHarness(t, v13)
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape || !strings.Contains(rec.Pending.Reason, "Outcome:") {
		t.Fatalf("missing Outcome did not pause for shape: %+v", rec.Pending)
	}
	if got := h.runner.roles(); strings.Join(got, ",") != "implementer-1,implementer-1" {
		t.Fatalf("shape retry rounds = %v", got)
	}
	if brief := h.runner.requests()[1].Brief; !strings.Contains(brief, "ork checked your last report and refused it: report shape: need exactly one non-empty Outcome: line") {
		t.Fatalf("retry brief lacks Outcome: error: %s", brief)
	}
}

func TestFinishHandoffMode(t *testing.T) {
	h := newHarness(t, v13, func(o *harnessOpts) {
		o.routes = []clitest.Route{{Tool: "slicer", Args: []string{"config", "implement_finish", "--json", "--root"}, Prefix: true,
			Stdout: `{"key":"implement_finish","value":"handoff"}`}}
	})
	outcomeRunner(h, "Verified engine behavior.")
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	before := len(h.runner.requests())
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateCommitted)
	if len(h.runner.requests()) != before {
		t.Errorf("a role ran in the finish step: %v", h.runner.roles())
	}
	if got := strings.Join(finishOrder(h), ","); got != "note,handoff,commit" {
		t.Fatalf("order %s, want note,handoff,commit", got)
	}
	if hand := h.calls("slicer", "handoff"); len(hand) != 1 || join(hand[0]) != "handoff S04 --render --check --json --root "+h.W {
		t.Errorf("handoff call: %v", hand)
	}
	if len(h.calls("slicer", "done")) != 0 {
		t.Errorf("slicer done ran in handoff mode")
	}
}

func TestFinishSlicerFailurePauses(t *testing.T) {
	h := newHarness(t, v13, func(o *harnessOpts) {
		o.routes = []clitest.Route{{Tool: "slicer", Args: []string{"done", item}, Prefix: true,
			Seq: []clitest.Reply{{Stderr: "slicer check failed: stale render\n", Exit: 1}, {Stdout: `{"id":"S04"}`}}}}
	})
	outcomeRunner(h, "Verified engine behavior.")
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingError || rec.Pending.State != runrec.StateFinish || !strings.Contains(rec.Pending.Reason, "stale render") {
		t.Fatalf("pending after a failed done: %+v", rec.Pending)
	}
	if len(h.calls("git", "-C", h.W, "commit")) != 0 {
		t.Fatal("committed before the finish step passed")
	}
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateCommitted)
	if n := len(h.calls("slicer", "done")); n != 2 {
		t.Errorf("done ran %d times, want the failed call and the retry", n)
	}
}

// showClaimed is `slicer show` of the test slice, claimed by Jon, with the given
// status and notes.
func showClaimed(status, notes string) string {
	return `{"id":"S04","title":"Engine","status":"` + status + `","claim":{"owner":"Jon","at":"2026-11-19T13:55:57Z"},"notes":[` + notes + `],` +
		`"slice":{"id":"S04","title":"Engine","size":"M","sections":[{"heading":"Git","body":"One commit: \"` + gitTitle + `\"."}]}}`
}

// A retry after a failed done does not write the closing note again, and both
// calls name the item's claim owner.
func TestFinishRetryKeepsOneNote(t *testing.T) {
	// Probe how many times the run reads the slice before its finish step.
	probe := newHarness(t, v13)
	outcomeRunner(probe, "Verified engine behavior.")
	probe.start()
	shows := len(probe.calls("slicer", "show"))

	seq := []clitest.Reply{}
	for range shows + 1 { // the reads before the first attempt, and the first attempt's own
		seq = append(seq, clitest.Reply{Stdout: showClaimed("started", "")})
	}
	seq = append(seq, clitest.Reply{Stdout: showClaimed("started", `"**2026-11-19** — Verified engine behavior."`)})
	h := newHarness(t, v13, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{
				{Tool: "slicer", Args: []string{"done", item}, Prefix: true, Seq: []clitest.Reply{{Stderr: "slicer check failed\n", Exit: 1}, {Stdout: `{"id":"S04"}`}}},
				{Tool: "slicer", Args: []string{"show", item, "--json", "--lean", "--root", W}, Seq: seq},
			}
		}
	})
	outcomeRunner(h, "Verified engine behavior.")
	e := h.start()
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StatePaused)
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateCommitted)
	if got := strings.Join(finishOrder(h), ","); got != "note,done,done,commit" {
		t.Fatalf("order %s, want one note, the failed done, the retried done, the commit", got)
	}
	for _, c := range append(h.calls("slicer", "note"), h.calls("slicer", "done")...) {
		if !slices.Contains(c, "--owner=Jon") {
			t.Errorf("%v does not name the claim owner", c)
		}
	}
}

// landedRetry runs a plain v13 run whose finish call (done or handoff) exits 1
// after it landed, as slicer does when only the tracking check fails: the
// first attempt pauses, and the retry finds the item already finished. After
// the first attempt, `slicer check` answers checkOK.
func landedRetry(t *testing.T, mode, status string, checkOK bool) (*harness, *Engine, int) {
	t.Helper()
	modeRoute := func(o *harnessOpts) {
		o.routes = []clitest.Route{{Tool: "slicer", Args: []string{"config", "implement_finish", "--json", "--root"}, Prefix: true,
			Stdout: `{"key":"implement_finish","value":"` + mode + `"}`}}
	}
	// Probe how often the run reads the slice and runs slicer check before its finish step.
	probe := newHarness(t, v13, modeRoute)
	outcomeRunner(probe, "Verified engine behavior.")
	probe.start()
	shows := len(probe.calls("slicer", "show"))
	checks := len(probe.calls("slicer", "check"))

	var showSeq, checkSeq []clitest.Reply
	for range shows + 1 { // the reads before the first attempt, and the first attempt's own
		showSeq = append(showSeq, clitest.Reply{Stdout: showClaimed("started", "")})
	}
	showSeq = append(showSeq, clitest.Reply{Stdout: showClaimed(status, `"**2026-11-19** — Verified engine behavior."`)})
	const green = `{"ok":true,"stale_render":[],"stale_sync":[],"problems":[],"warnings":[]}`
	for range checks {
		checkSeq = append(checkSeq, clitest.Reply{Stdout: green})
	}
	if checkOK {
		checkSeq = append(checkSeq, clitest.Reply{Stdout: green})
	} else {
		checkSeq = append(checkSeq, clitest.Reply{Stdout: `{"ok":false,"stale_render":[],"stale_sync":[],"problems":["stale render"],"warnings":[]}`, Exit: 1})
	}
	h := newHarness(t, v13, modeRoute, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{
				{Tool: "slicer", Args: []string{mode, item}, Prefix: true, Stderr: "tracking check failed\n", Exit: 1},
				{Tool: "slicer", Args: []string{"show", item, "--json", "--lean", "--root", W}, Seq: showSeq},
				{Tool: "slicer", Args: []string{"check", "--json", "--root", W}, Seq: checkSeq},
			}
		}
	})
	outcomeRunner(h, "Verified engine behavior.")
	e := h.start()
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StatePaused)
	if n := len(h.calls("slicer", mode)); n != 1 {
		t.Fatalf("%s ran %d times in the first attempt", mode, n)
	}
	return h, e, len(h.calls("slicer", "check"))
}

// slicer done exits 1 when it landed and only the tracking check failed. The
// retry must not run done again on the finished item: it runs slicer check.
func TestFinishRetryAfterAFinishThatLandedRunsCheckNotTheFinishAgain(t *testing.T) {
	for _, tc := range []struct{ mode, status string }{{"done", "done"}, {"handoff", "review"}} {
		t.Run(tc.mode, func(t *testing.T) {
			h, e, checks := landedRetry(t, tc.mode, tc.status, true)
			if err := e.Retry(context.Background()); err != nil {
				t.Fatal(err)
			}
			requireState(t, e, runrec.StateCommitted)
			if n := len(h.calls("slicer", tc.mode)); n != 1 {
				t.Errorf("%s ran %d times, want only the first attempt", tc.mode, n)
			}
			if n := len(h.calls("slicer", "check")); n != checks+1 {
				t.Errorf("slicer check ran %d times, want one more than %d", n, checks)
			}
			if got := strings.Join(finishOrder(h), ","); got != "note,"+tc.mode+",commit" {
				t.Errorf("order %s", got)
			}
		})
	}
}

func TestFinishRetryAfterAFinishThatLandedPausesWhileCheckFails(t *testing.T) {
	h, e, _ := landedRetry(t, "done", "done", false)
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.State != runrec.StateFinish || !strings.Contains(rec.Pending.Reason, "slicer check fails") || !strings.Contains(rec.Pending.Reason, "stale render") {
		t.Fatalf("pending %+v", rec.Pending)
	}
	if n := len(h.calls("slicer", "done")); n != 1 {
		t.Errorf("done ran %d times", n)
	}
	if len(h.calls("git", "-C", h.W, "commit")) != 0 {
		t.Error("committed while the tracking check fails")
	}
}

// A plain v13 run whose accepted implementer reports hold no Outcome line (it
// began on an ork build from before S384) keeps the implementer finish round.
func TestFinishWithNoOutcomeInTheRecordKeepsTheRolePath(t *testing.T) {
	h := newHarness(t, v13)
	outcomeRunner(h, "Verified engine behavior.")
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	if err := e.run.Update(func(r *runrec.Record) {
		for i := range r.Roles {
			if r.Roles[i].Role == RoleImplementer {
				r.Roles[i].Report = ""
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateCommitted)
	reqs := h.runner.requests()
	last := reqs[len(reqs)-1]
	if last.Role != RoleImplementer || !strings.Contains(last.Brief, "## Finish step") {
		t.Fatalf("no implementer finish round: %v", h.runner.roles())
	}
	if len(h.calls("slicer", "done")) != 0 {
		t.Error("ork ran slicer done with no Outcome to note")
	}
}

// A split lead, a chain sliver and a lone sliver keep the implementer finish
// round and their report shape on v13 (adj 500, engine.finish-scope).
func TestFinishOnV13KeepsTheRolePathForSplitAndSlivers(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) *harness
		stop  string
	}{
		{"split lead", func(t *testing.T) *harness {
			h, _ := splitHarness(t, "M", []string{partOne, partTwo}, v13)
			return h
		}, runrec.StateAwaitingApproval},
		{"lone sliver", func(t *testing.T) *harness {
			h := newHarness(t, v13)
			h.cfg.Parent = "S00"
			return h
		}, runrec.StateAwaitingApproval},
		{"last chain sliver", func(t *testing.T) *harness {
			h := newHarness(t, v13)
			h.cfg.Parent, h.cfg.Chain, h.cfg.SliverIndex, h.cfg.SliverCount = "S00", "chain-1", 2, 2
			return h
		}, runrec.StateAwaitingApproval},
		{"mid chain sliver", func(t *testing.T) *harness {
			h := newHarness(t, v13)
			h.cfg.Parent, h.cfg.Chain, h.cfg.SliverIndex, h.cfg.SliverCount = "S00", "chain-1", 1, 2
			return h
		}, runrec.StateCommitted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.setup(t)
			e := h.start()
			// The report carries no Outcome line, and the run still reaches its stop.
			rec := e.Snapshot()
			if rec.State != tc.stop && rec.State != runrec.StateAwaitingApproval {
				t.Fatalf("state %s (%s): a report without Outcome was refused", rec.State, rec.Reason)
			}
			if rec.State == runrec.StateAwaitingApproval {
				if err := e.Approve(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			var finish *Request
			for _, req := range h.runner.requests() {
				if strings.Contains(req.Brief, "## Finish step") {
					r := req
					finish = &r
				}
			}
			if finish == nil {
				t.Fatalf("no finish implementer round: %v", h.runner.roles())
			}
			if finish.Role != RoleImplementer || !strings.HasPrefix(finish.Resume, "sess-") {
				t.Errorf("finish role/session: %+v", finish)
			}
			section := finishSection(t, finish.Brief)
			for _, want := range []string{`slicer note S04 --text "Describe the verified outcome" --render`, `slicer done S04 --note "Describe the verified outcome" --render --check`} {
				if !strings.Contains(section, want) {
					t.Errorf("finish brief lacks %q: %s", want, section)
				}
			}
			if strings.Index(section, "slicer note S04") >= strings.Index(section, "slicer done S04") {
				t.Errorf("note must precede done: %s", section)
			}
			if got := strings.Join(finishOrder(h), ","); strings.Contains(got, "done") || strings.Contains(got, "handoff") {
				t.Errorf("ork ran the finish step itself: %s", got)
			}
		})
	}
}
