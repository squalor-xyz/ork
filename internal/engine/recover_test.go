// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func runningRole(role string, pgid int, step string) runrec.RoleRecord {
	return runrec.RoleRecord{Role: role, Round: 1, Vendor: "claude", PGID: pgid, Step: step, StartedAt: time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)}
}

func (h *harness) seed(id, state string, roles ...runrec.RoleRecord) {
	h.t.Helper()
	_, err := h.store.Create(runrec.Record{
		ID: id, Project: "ork", Item: item, Worktree: h.W, Branch: item, RelayRun: "run-1", OrchestratorStep: "1",
		Contract: 7, State: state, Round: 1, Roles: roles,
		Lineup: h.cfg.Lineup,
	})
	if err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) recover() map[string]*Engine {
	h.t.Helper()
	engines, err := Recover(context.Background(), h.deps, func(name string) (registry.Project, bool) {
		return h.proj, name == "ork"
	})
	if err != nil {
		h.t.Fatalf("Recover: %v", err)
	}
	out := map[string]*Engine{}
	for _, e := range engines {
		out[e.Snapshot().ID] = e
	}
	return out
}

func TestRestartKillsALiveOrphanMarksItLostAndPausesTheSlice(t *testing.T) {
	h := newHarness(t)
	h.seed("run-a", runrec.StateImplement, runningRole(RoleImplementer, 111, "2"))
	h.proc.alive[111] = true
	engines := h.recover()
	if len(h.proc.killed) != 1 || h.proc.killed[0] != 111 {
		t.Fatalf("killed %v, want [111]", h.proc.killed)
	}
	rec := requireState(t, engines["run-a"], runrec.StatePaused)
	role := rec.Roles[0]
	if role.Exit != "lost" || role.Running() {
		t.Errorf("role: %+v", role)
	}
	p := rec.Pending
	if p == nil || p.Kind != runrec.PendingLost || p.Role != RoleImplementer || p.Round != 1 || p.State != runrec.StateImplement || !strings.Contains(p.Reason, "lost") || !strings.Contains(p.Reason, "killed") {
		t.Errorf("pending: %+v", p)
	}
	st := h.steps()
	if len(st) != 1 || st[0].Phase != "fail" || st[0].Step != "2" {
		t.Errorf("the lost role's ledger step must be marked fail: %+v", st)
	}
}

func TestRestartMarksAGoneProcessLostWithoutKilling(t *testing.T) {
	h := newHarness(t)
	h.seed("run-a", runrec.StateReview, runningRole(RoleReviewer, 222, "4"))
	engines := h.recover()
	if len(h.proc.killed) != 0 {
		t.Fatalf("killed %v although the group was gone", h.proc.killed)
	}
	rec := requireState(t, engines["run-a"], runrec.StatePaused)
	if rec.Pending.Kind != runrec.PendingLost || rec.Pending.State != runrec.StateReview || strings.Contains(rec.Pending.Reason, "killed") {
		t.Errorf("pending: %+v", rec.Pending)
	}
}

func TestRestartKillsOnlyProcessGroupsOrkOwns(t *testing.T) {
	h := newHarness(t)
	h.seed("run-own", runrec.StateImplement, runningRole(RoleImplementer, 111, "2"))
	h.seed("run-final", runrec.StateCommitted, runningRole(RoleImplementer, 333, "2")) // finished: leave alone
	h.seed("run-stopped", runrec.StateStopped, runningRole(RoleImplementer, 334, "2"))
	h.seed("run-ended-role", runrec.StateImplement, func() runrec.RoleRecord {
		r := runningRole(RoleImplementer, 555, "2")
		r.EndedAt = time.Date(2026, 10, 19, 16, 5, 0, 0, time.UTC)
		r.Exit = "ok"
		return r
	}())
	h.seed("run-nopgid", runrec.StateImplement, runningRole(RoleImplementer, 0, "2"))
	h.seed("run-init", runrec.StateImplement, runningRole(RoleImplementer, 1, "2"))
	h.seed("run-neg", runrec.StateImplement, runningRole(RoleImplementer, -5, "2"))
	h.seed("run-self", runrec.StateImplement, runningRole(RoleImplementer, selfPGID(), "2"))
	for _, pg := range []int{111, 333, 334, 555, 1, -5, selfPGID()} {
		h.proc.alive[pg] = true
	}
	engines := h.recover()
	if len(h.proc.killed) != 1 || h.proc.killed[0] != 111 {
		t.Fatalf("killed %v, want only [111]", h.proc.killed)
	}
	if _, ok := engines["run-final"]; ok {
		t.Errorf("a finished run is not recovered")
	}
	if _, ok := engines["run-stopped"]; ok {
		t.Errorf("a stopped run is not recovered")
	}
	for _, id := range []string{"run-nopgid", "run-init", "run-neg", "run-self"} {
		rec := engines[id].Snapshot()
		if rec.State != runrec.StatePaused || rec.Roles[0].Exit != "lost" {
			t.Errorf("%s: state %q role %+v", id, rec.State, rec.Roles[0])
		}
	}
	// The finished runs' records are untouched.
	run, _ := h.store.Open("run-final")
	if rec := run.Snapshot(); !rec.Roles[0].Running() || rec.State != runrec.StateCommitted {
		t.Errorf("run-final was changed: %+v", rec)
	}
}

func TestRestartLeavesRestingRunsAlone(t *testing.T) {
	h := newHarness(t)
	h.seed("run-wait", runrec.StateAwaitingApproval)
	h.seed("run-pause", runrec.StatePaused)
	engines := h.recover()
	if len(engines) != 2 {
		t.Fatalf("%d engines", len(engines))
	}
	requireState(t, engines["run-wait"], runrec.StateAwaitingApproval)
	requireState(t, engines["run-pause"], runrec.StatePaused)
	if len(h.steps()) != 0 || len(h.proc.killed) != 0 {
		t.Errorf("a resting run needs nothing")
	}
}

func TestRestartPausesARunInterruptedBetweenRoles(t *testing.T) {
	h := newHarness(t)
	h.seed("run-a", runrec.StateChecks)
	engines := h.recover()
	rec := requireState(t, engines["run-a"], runrec.StatePaused)
	if rec.Pending.Kind != runrec.PendingInterrupted || rec.Pending.State != runrec.StateChecks {
		t.Fatalf("pending: %+v", rec.Pending)
	}
	if err := engines["run-a"].Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, engines["run-a"], runrec.StateAwaitingApproval)
}

func TestRetryOfALostRoleRunsItAgainAndStopEndsTheRun(t *testing.T) {
	h := newHarness(t)
	h.seed("run-a", runrec.StateImplement, runningRole(RoleImplementer, 111, "2"))
	h.seed("run-b", runrec.StateImplement, runningRole(RoleImplementer, 112, "2"))
	engines := h.recover()

	if err := engines["run-a"].Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, engines["run-a"], runrec.StateAwaitingApproval)
	if len(rec.Roles) != 4 || rec.Roles[0].Exit != "lost" || rec.Roles[1].Role != RoleImplementer {
		t.Fatalf("roles: %+v", rec.Roles)
	}
	first := h.runner.requests()[0]
	if first.Role != RoleImplementer || first.Resume != "" || !strings.Contains(first.Brief, "lost") {
		t.Errorf("retry request: resume %q\n%.400s", first.Resume, first.Brief)
	}

	if err := engines["run-b"].Stop(context.Background(), "owner gave up"); err != nil {
		t.Fatal(err)
	}
	rec = requireState(t, engines["run-b"], runrec.StateStopped)
	if !strings.Contains(rec.Reason, "owner gave up") {
		t.Errorf("reason %q", rec.Reason)
	}
}

func TestRecoverReportsAProjectItCannotFindAndRecoversTheRest(t *testing.T) {
	h := newHarness(t)
	h.seed("run-a", runrec.StateAwaitingApproval)
	_, err := h.store.Create(runrec.Record{ID: "run-x", Project: "gone", Item: "S99", State: runrec.StatePaused})
	if err != nil {
		t.Fatal(err)
	}
	engines, err := Recover(context.Background(), h.deps, func(name string) (registry.Project, bool) { return h.proj, name == "ork" })
	if err == nil || !strings.Contains(err.Error(), "gone") || len(engines) != 1 {
		t.Fatalf("%d engines, err %v", len(engines), err)
	}
}

func TestRunCheck(t *testing.T) {
	dir := t.TempDir()
	out, err := RunCheck(context.Background(), dir, "echo hello; pwd", 5*time.Second)
	if err != nil || !out.OK || !strings.Contains(out.Output, "hello") || !strings.Contains(out.Output, strings.TrimPrefix(dir, "/private")) {
		t.Fatalf("%+v, %v", out, err)
	}
	out, err = RunCheck(context.Background(), dir, "echo out; echo err >&2; exit 3", 5*time.Second)
	if err != nil || out.OK || !strings.Contains(out.Output, "out") || !strings.Contains(out.Output, "err") || !strings.Contains(out.Output, "exit 3") {
		t.Fatalf("failing command: %+v, %v", out, err)
	}
	out, err = RunCheck(context.Background(), dir, "sleep 5", 100*time.Millisecond)
	if err != nil || out.OK || !strings.Contains(out.Output, "timed out") {
		t.Fatalf("timeout: %+v, %v", out, err)
	}
}
