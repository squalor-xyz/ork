// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func requireBudgetPause(t *testing.T, e *Engine, role, state string, count int) runrec.Record {
	t.Helper()
	rec := e.Snapshot()
	if rec.State != runrec.StatePaused || rec.Pending == nil || rec.Pending.Kind != "budget" || rec.Pending.Role != role || rec.Pending.State != state || rec.Pending.Round != 1 || len(rec.Roles) != count {
		t.Fatalf("want budget pause before %s, %d completed roles; state=%s pending=%+v roles=%+v", role, count, rec.State, rec.Pending, rec.Roles)
	}
	return rec
}

func TestBudgetTokensPauseBeforeTheNextRole(t *testing.T) {
	for _, limit := range []int{999, 1000, 1500} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			h := newHarness(t)
			h.cfg.Budgets.Tokens = limit
			e := h.start()
			count, role, state := 1, RoleReviewer, runrec.StateReview
			if limit == 1500 {
				count, role, state = 2, RoleGate, runrec.StateGate
			}
			rec := requireBudgetPause(t, e, role, state, count)
			if !strings.Contains(rec.Reason, "token budget") || !strings.Contains(rec.Reason, "reached") {
				t.Fatalf("reason: %s", rec.Reason)
			}
			if len(h.runner.requests()) != count {
				t.Fatal("next role started")
			}
			if _, err := os.Stat(filepath.Join(e.run.Dir(), role+"-1.md")); !os.IsNotExist(err) {
				t.Fatalf("next report: %v", err)
			}
			starts := 0
			for _, call := range h.calls("slicer", "relay-run", "step") {
				if contains(call, "start") {
					starts++
				}
			}
			if starts != count+1 {
				t.Fatalf("ledger starts=%d, want %d including orchestrator", starts, count+1)
			}
		})
	}
}

// budgetClock advances only inside the fake role runner, so checks and pauses
// have no effect on the recorded role durations.
type budgetClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *budgetClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *budgetClock) Advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.at = c.at.Add(d) }

func TestBudgetMinutesPauseBeforeTheNextRole(t *testing.T) {
	for _, duration := range []time.Duration{time.Minute, 61 * time.Second, 30 * time.Second} {
		t.Run(duration.String(), func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) { o.project = func(p *registry.Project) { p.Budgets.Minutes = 1 } })
			c := &budgetClock{at: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}
			h.deps.Now = c.Now
			h.runner.script = func(req Request, n int) (Result, error) { c.Advance(duration); return defaultResult(req), nil }
			count, role, state := 1, RoleReviewer, runrec.StateReview
			if duration == 30*time.Second {
				count, role, state = 2, RoleGate, runrec.StateGate
			}
			rec := requireBudgetPause(t, h.start(), role, state, count)
			if !strings.Contains(rec.Reason, "time budget 1m reached") {
				t.Fatalf("reason: %s", rec.Reason)
			}
		})
	}
}

func TestBudgetMinutesCountRoleTimeNotPausedTime(t *testing.T) {
	h := newHarness(t)
	h.cfg.Budgets.Minutes = 1
	c := &budgetClock{at: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}
	h.deps.Now = c.Now
	h.runner.script = func(req Request, n int) (Result, error) {
		c.Advance(10 * time.Second)
		res := defaultResult(req)
		if n == 0 {
			res.Report = "bad shape"
		}
		return res, nil
	}
	e := h.start()
	if rec := e.Snapshot(); rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape {
		t.Fatalf("expected shape pause: %+v", rec.Pending)
	}
	c.Advance(24 * time.Hour)
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rec := e.Snapshot(); rec.State != runrec.StateAwaitingApproval || len(rec.Roles) != 4 {
		t.Fatalf("paused time counted: %+v", rec)
	}
}

func TestRetryAfterBudgetPauseGrantsOneRound(t *testing.T) {
	h := newHarness(t)
	h.cfg.Budgets.Tokens = 1000
	e := h.start()
	requireBudgetPause(t, e, RoleReviewer, runrec.StateReview, 1)
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireBudgetPause(t, e, RoleGate, runrec.StateGate, 2)
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rec := e.Snapshot(); rec.State != runrec.StateAwaitingApproval || len(rec.Roles) != 3 || rec.Pending != nil {
		t.Fatalf("second grant: %+v", rec)
	}
}

func TestBudgetLetsTheRunningRoleFinish(t *testing.T) {
	h := newHarness(t)
	h.cfg.Budgets.Tokens = 9000
	h.runner.script = func(req Request, n int) (Result, error) {
		res := defaultResult(req)
		res.Tokens = 9100
		return res, nil
	}
	e := h.start()
	rec := requireBudgetPause(t, e, RoleReviewer, runrec.StateReview, 1)
	role := rec.Roles[0]
	if role.Running() || role.Tokens != 9100 || role.Exit != "ok" || role.Report == "" {
		t.Fatalf("unfinished role: %+v", role)
	}
	if rec.Reason != "token budget 9000 reached (9100 used)" {
		t.Fatalf("reason: %s", rec.Reason)
	}
	reopened, err := h.store.Open(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Snapshot(); got.Roles[0].Tokens != 9100 || got.Pending == nil || got.Pending.Kind != "budget" {
		t.Fatalf("not persisted: %+v", got)
	}
}

func TestZeroBudgetNeverPauses(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		res := defaultResult(req)
		res.Tokens = 1000000
		return res, nil
	}
	e := h.start()
	if rec := e.Snapshot(); rec.State != runrec.StateAwaitingApproval || len(rec.Roles) != 3 {
		t.Fatalf("zero budget: %+v", rec)
	}
}

func TestBudgetMinutesIncludesRunningRole(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	e := &Engine{d: Deps{Now: func() time.Time { return now }}}
	for _, tc := range []struct {
		name string
		end  time.Time
		want bool
	}{
		{"running", time.Time{}, true},
		{"ended below", now.Add(-time.Second), false},
		{"ended at limit", now, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := runrec.Record{Budgets: registry.Budgets{Minutes: 1}, Roles: []runrec.RoleRecord{
				{StartedAt: now.Add(-time.Hour), EndedAt: now.Add(-time.Hour).Add(30 * time.Second)},
				{StartedAt: now.Add(-30 * time.Second), EndedAt: tc.end},
			}}
			if why, got := e.overBudget(rec); got != tc.want {
				t.Fatalf("over=%v reason=%q, want %v", got, why, tc.want)
			}
		})
	}
}

func TestBudgetGrantSpentOnShapePause(t *testing.T) {
	h := newHarness(t)
	h.cfg.Budgets.Tokens = 1000
	h.runner.script = func(req Request, n int) (Result, error) {
		res := defaultResult(req)
		if n == 1 {
			res.Report = "bad shape"
		}
		return res, nil
	}
	e := h.start()
	requireBudgetPause(t, e, RoleReviewer, runrec.StateReview, 1)
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rec := e.Snapshot(); rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape {
		t.Fatalf("expected shape pause: %+v", rec.Pending)
	}
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireBudgetPause(t, e, RoleReviewer, runrec.StateReview, 2)
	if len(h.runner.requests()) != 2 {
		t.Fatal("spent grant ran another role")
	}
}
