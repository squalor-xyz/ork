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

func TestAnExitNotClassifiedOkStopsTheSliceWithTheReason(t *testing.T) {
	for _, class := range []ExitClass{ExitAuth, ExitUnclassified} {
		t.Run(string(class), func(t *testing.T) {
			h := newHarness(t)
			h.runner.script = func(req Request, n int) (Result, error) {
				if req.Role == RoleReviewer {
					return Result{Exit: class, Session: "s", Tokens: 7, Report: cleanReview}, nil
				}
				return Result{}, nil
			}
			e := h.start()
			rec := requireState(t, e, runrec.StateStopped)
			if !strings.Contains(rec.Reason, "reviewer") || !strings.Contains(rec.Reason, string(class)) || !strings.Contains(rec.Reason, "1") {
				t.Errorf("reason %q", rec.Reason)
			}
			if got := h.runner.roles(); len(got) != 2 {
				t.Errorf("nothing runs after a stop: %v", got)
			}
			role := rec.Roles[1]
			if string(role.Exit) != string(class) || role.Running() || role.Tokens != 7 {
				t.Errorf("role record: %+v", role)
			}
			st := h.steps()
			if st[len(st)-1].Phase != "fail" || st[len(st)-1].Step != "4" {
				t.Errorf("the failed role's step must end fail: %+v", st)
			}
			end := h.calls("slicer", "relay-run", "end")
			if len(end) != 1 || !strings.Contains(join(end[0]), "--outcome stopped --close-steps fail") {
				t.Errorf("end: %v", end)
			}
			if rec.Pending != nil {
				t.Errorf("a stopped slice has no pending: %+v", rec.Pending)
			}
		})
	}
}

func TestAnUnsuccessfulPermissionDenialStopsTheSlice(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role != RoleImplementer {
			return Result{}, nil
		}
		return Result{Exit: ExitError, Session: "s", Report: implReport, Denials: []runrec.DeniedCommand{{Tool: "Bash", Command: "rm -rf /"}, {Tool: "Write", Command: "/x.txt"}}}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateStopped)
	if !strings.Contains(rec.Reason, "permission") || !strings.Contains(rec.Reason, "Bash: rm -rf /, Write: /x.txt") {
		t.Errorf("reason %q", rec.Reason)
	}
	if got := rec.Roles[0].Denials; !reflect.DeepEqual(got, []string{"Bash", "Write"}) {
		t.Errorf("denial names in the record: %v", got)
	}
	want := []runrec.DeniedCommand{{Tool: "Bash", Command: "rm -rf /"}, {Tool: "Write", Command: "/x.txt"}}
	if got := rec.Roles[0].DeniedCommands; !reflect.DeepEqual(got, want) {
		t.Errorf("denied commands in the record: %+v, want %+v", got, want)
	}
	if len(h.checks.calls) != 0 {
		t.Errorf("checks ran after a denial")
	}
}

func statusRoute(replies ...string) func(o *harnessOpts) {
	return func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			var seq []clitest.Reply
			for _, r := range replies {
				seq = append(seq, clitest.Reply{Stdout: r})
			}
			return []clitest.Route{{Tool: "git", Args: []string{"-C", W, "status", "--porcelain"}, Seq: seq}}
		}
	}
}

func TestAReadOnlyRoleThatChangedTheTreeStopsTheSlice(t *testing.T) {
	// git status is read before and after each read-only role: reviewer (calls
	// 0 and 1), gate (2 and 3).
	for name, tc := range map[string]struct {
		replies []string
		role    string
	}{
		"reviewer edits a file": {[]string{"", " M internal/engine/engine.go\n"}, "reviewer"},
		"reviewer adds a file":  {[]string{"", "?? notes.txt\n"}, "reviewer"},
		"gate edits a file":     {[]string{"", "", "", " M a.go\n"}, "gate"},
		"gate changes dirt":     {[]string{" M a.go\n", " M a.go\n", " M a.go\n", " M a.go\n M b.go\n"}, "gate"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, statusRoute(tc.replies...))
			e := h.start()
			rec := requireState(t, e, runrec.StateStopped)
			if !strings.Contains(rec.Reason, tc.role) || !strings.Contains(rec.Reason, "changed the tree") {
				t.Errorf("reason %q", rec.Reason)
			}
			if rec.Roles[len(rec.Roles)-1].Role != tc.role {
				t.Errorf("roles: %+v", rec.Roles)
			}
		})
	}
}

func TestAReadOnlyRoleOnADirtyButUnchangedTreeIsFine(t *testing.T) {
	h := newHarness(t, statusRoute(" M a.go\n?? b.txt\n"))
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	var status int
	for _, c := range h.fake.CallsTo("git") {
		if len(c.Args) > 2 && c.Args[2] == "status" {
			status++
		}
	}
	if status != 4 {
		t.Errorf("%d git status calls, want 2 per read-only role and none for the implementer", status)
	}
}

func TestASuccessfulPermissionDenialProceeds(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		report := implReport
		if req.Role != RoleImplementer {
			report = cleanReview
		}
		if req.Role == RoleGate {
			report = gatePass
		}
		return Result{Exit: ExitOK, Session: "s", Report: report, Denials: []runrec.DeniedCommand{{Tool: "Write", Command: "/x.txt"}}}, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	for _, role := range rec.Roles {
		if len(role.Denials) != 1 || role.Denials[0] != "Write" {
			t.Fatalf("role lost denials: %+v", role)
		}
		if want := []runrec.DeniedCommand{{Tool: "Write", Command: "/x.txt"}}; !reflect.DeepEqual(role.DeniedCommands, want) {
			t.Fatalf("role lost the denied command: %+v", role.DeniedCommands)
		}
	}
}

func TestStopQueuedRunMakesNoWrites(t *testing.T) {
	h := newHarness(t)
	h.cfg.Queued = true
	h.cfg.Wave = "wave-20261020-100000-ab12"
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateQueued)
	before := len(h.fake.Calls())
	if err := e.Run(context.Background()); err != nil {
		t.Fatalf("a queued run rests: %v", err)
	}
	if err := e.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateQueued)
	if err := e.Stop(context.Background(), "CLI stop"); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateStopped)
	if len(rec.Writes) != 0 {
		t.Errorf("writes: %+v", rec.Writes)
	}
	if rec.Wave != h.cfg.Wave {
		t.Errorf("wave %q", rec.Wave)
	}
	if got := len(h.fake.Calls()); got != before {
		t.Errorf("a queued stop called tools: %v", h.fake.Calls()[before:])
	}
}

func TestDequeueMovesAQueuedRunToSetup(t *testing.T) {
	h := newHarness(t)
	h.cfg.Queued = true
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Dequeue(); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateSetup)
	if err := e.Dequeue(); err == nil {
		t.Error("dequeue of a run that is not queued must fail")
	}
}

func TestStopQueuedRecordsTheReasonAndRefusesOtherStates(t *testing.T) {
	for _, state := range []string{runrec.StateQueued, runrec.StateSetup, runrec.StateStopped} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t)
			h.cfg.Queued = true
			e, err := New(context.Background(), h.deps, h.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.run.Update(func(r *runrec.Record) { r.State = state }); err != nil {
				t.Fatal(err)
			}
			before := e.Snapshot()
			calls := len(h.fake.Calls())
			err = e.StopQueued(context.Background(), "project was removed")
			if state != runrec.StateQueued {
				if err == nil {
					t.Fatal("stop of a run that is not queued must fail")
				}
				if !reflect.DeepEqual(e.Snapshot(), before) {
					t.Fatal("refused stop changed the record")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				rec := requireState(t, e, runrec.StateStopped)
				if rec.Reason != "project was removed" || len(rec.Writes) != 0 {
					t.Fatalf("reason %q, writes %+v", rec.Reason, rec.Writes)
				}
				stored, err := h.store.Open(rec.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got := stored.Snapshot(); got.State != runrec.StateStopped || got.Reason != rec.Reason {
					t.Fatalf("stored state %q, reason %q", got.State, got.Reason)
				}
			}
			if got := len(h.fake.Calls()); got != calls {
				t.Errorf("stop queued called tools: %v", h.fake.Calls()[calls:])
			}
		})
	}
}
