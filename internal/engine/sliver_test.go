// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// A sliver that is not the last of its chain is accepted at its gate with no
// owner stop. It runs its finish step and the ledger close, is committed, and
// does not land: the next sliver then starts on a clean tree (S62).
func TestMidSliverIsFinishedAndCommittedAtItsGate(t *testing.T) {
	h := newHarness(t)
	h.cfg.Parent, h.cfg.Chain, h.cfg.SliverIndex, h.cfg.SliverCount = "S00", "chain-1", 1, 2
	e := h.start()
	rec := requireState(t, e, runrec.StateCommitted)
	if rec.Commit == nil || rec.Commit.Title != gitTitle || len(rec.Approvals) != 1 || !rec.LandedAt.IsZero() {
		t.Fatalf("a mid sliver was not finished and committed: commit %+v, approvals %+v, landed %v", rec.Commit, rec.Approvals, rec.LandedAt)
	}
	for _, c := range h.fake.CallsTo("slicer") {
		if len(c.Args) > 0 && c.Args[0] == "land" {
			t.Fatalf("a mid sliver landed: %v", c.Args)
		}
	}
	reqs := h.runner.requests()
	fin := reqs[len(reqs)-1]
	if fin.Role != RoleImplementer || !strings.Contains(finishSection(t, fin.Brief), "no owner stop") {
		t.Errorf("the finish step did not run for the mid sliver: %+v", fin)
	}
	if end := h.calls("slicer", "relay-run", "end"); len(end) != 1 {
		t.Errorf("the ledger run was not ended once: %v", end)
	}
	var order []string
	for _, c := range h.fake.Calls() {
		switch {
		case c.Tool == "git" && len(c.Args) > 3 && c.Args[2] == "commit":
			order = append(order, "commit")
		case c.Tool == "slicer" && len(c.Args) > 2 && c.Args[0] == "relay-run" && c.Args[1] == "end":
			order = append(order, "end")
		}
	}
	if strings.Join(order, ",") != "end,commit" {
		t.Errorf("order %v, want the finish and ledger close before the commit", order)
	}
}

// The last sliver of a chain waits for the owner, as a slice does.
func TestLastSliverWaitsForTheOwner(t *testing.T) {
	h := newHarness(t)
	h.cfg.Parent, h.cfg.Chain, h.cfg.SliverIndex, h.cfg.SliverCount = "S00", "chain-1", 2, 2
	requireState(t, h.start(), runrec.StateAwaitingApproval)
}

// Only the last sliver of a chain lands the parent. A lone sliver lands its
// own item on the parent's branch.
func TestOnlyTheLastSliverLandsTheParent(t *testing.T) {
	for _, tc := range []struct {
		name, chain string
		index       int
		want        string
	}{
		{"lone", "", 0, "land " + item + " --branch " + item},
		{"last", "chain-1", 2, "land S00 --branch " + item},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newLandEnv(t)
			count := 0
			if tc.chain != "" {
				count = 2
			}
			if err := env.run.Update(func(r *runrec.Record) {
				r.Parent, r.Chain, r.SliverIndex, r.SliverCount = "S00", tc.chain, tc.index, count
			}); err != nil {
				t.Fatal(err)
			}
			if err := env.e.Approve(context.Background()); err != nil {
				t.Fatal(err)
			}
			if rec := env.e.Snapshot(); rec.State != runrec.StateCommitted || rec.LandedAt.IsZero() {
				t.Fatalf("did not land: %+v", rec)
			}
			slicer := env.log("slicer.log")
			land := indexOf(slicer, "land ")
			if land < 0 || !strings.HasPrefix(slicer[land], tc.want+" ") {
				t.Fatalf("land call %q, want it to start with %q", slicer[max(land, 0):], tc.want)
			}
		})
	}
}
