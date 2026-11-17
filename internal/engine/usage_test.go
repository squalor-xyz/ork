// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"math"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// usageRounds runs implementer rounds 1..n through runRound on one engine.
// Round i answers with results[i-1]; a result with no exit is a good round.
// It returns the engine so a test can read the roles and the budget check.
func usageRounds(t *testing.T, lineup []runrec.LineupEntry, budgetTokens int, results ...Result) *Engine {
	t.Helper()
	h := newHarness(t, withDiff)
	h.cfg.Lineup[RoleImplementer] = lineup
	h.runner.script = func(req Request, n int) (Result, error) {
		res := results[min(n, len(results)-1)]
		if res.Exit == "" {
			res.Exit = ExitOK
		}
		if res.Exit == ExitOK && res.Report == "" {
			res.Report = implReport
		}
		return res, nil
	}
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.run.Update(func(r *runrec.Record) {
		r.State = runrec.StateImplement
		r.Budgets.Tokens = budgetTokens
	}); err != nil {
		t.Fatal(err)
	}
	for i := range results {
		if _, err := e.runRound(context.Background(), round{role: RoleImplementer, n: i + 1, state: runrec.StateImplement, resumeFrom: []string{RoleImplementer}}, nil); err != nil {
			t.Fatalf("round %d: %v", i+1, err)
		}
	}
	return e
}

func oneRow(vendor string) []runrec.LineupEntry {
	return []runrec.LineupEntry{{Vendor: vendor, Model: "m", Effort: "high"}}
}

func costs(e *Engine) []float64 {
	var out []float64
	for _, r := range e.Snapshot().Roles {
		out = append(out, r.CostUSD)
	}
	return out
}

func tokens(e *Engine) []int {
	var out []int
	for _, r := range e.Snapshot().Roles {
		out = append(out, r.Tokens)
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func sumCost(e *Engine) float64 {
	total := 0.0
	for _, c := range costs(e) {
		total += c
	}
	return total
}

// S380, *proposed*: Claude's total_cost_usd is the running cost of the
// session (testdata/vendors/claude/03-resume.stdout), so a resumed step records
// the part it added.
func TestResumedClaudeStepRecordsItsOwnCost(t *testing.T) {
	e := usageRounds(t, oneRow("claude"), 0,
		Result{Session: "s", CostUSD: 1.00, Tokens: 40},
		Result{Session: "s", CostUSD: 1.50, Tokens: 25})
	got := costs(e)
	if len(got) != 2 || !near(got[0], 1.00) || !near(got[1], 0.50) {
		t.Fatalf("step costs = %v, want [1 0.5]", got)
	}
	roles := e.Snapshot().Roles
	if !near(roles[0].SessionCostUSD, 1.00) || !near(roles[1].SessionCostUSD, 1.50) {
		t.Fatalf("session cost totals = %v, %v, want 1 and 1.5", roles[0].SessionCostUSD, roles[1].SessionCostUSD)
	}
}

// Claude's tokens are per step: the result's usage block does not grow with the
// session, so only the cost is a total.
func TestResumedClaudeTokensStayPerStep(t *testing.T) {
	e := usageRounds(t, oneRow("claude"), 0,
		Result{Session: "s", CostUSD: 1.00, Tokens: 40},
		Result{Session: "s", CostUSD: 1.50, Tokens: 25})
	if got := tokens(e); len(got) != 2 || got[0] != 40 || got[1] != 25 {
		t.Fatalf("step tokens = %v, want [40 25]", got)
	}
	if r := e.Snapshot().Roles[1]; r.SessionTokens != 0 {
		t.Fatalf("a per-step vendor stored a session token total: %d", r.SessionTokens)
	}
}

// Codex's turn.completed usage is the running total of the thread
// (testdata/vendors/codex/02-resume-by-id.stdout), so a resumed step records
// what it added.
func TestResumedCodexStepRecordsItsOwnTokens(t *testing.T) {
	e := usageRounds(t, oneRow("codex"), 0,
		Result{Session: "t", Tokens: 100},
		Result{Session: "t", Tokens: 250})
	got := tokens(e)
	if len(got) != 2 || got[0] != 100 || got[1] != 150 {
		t.Fatalf("step tokens = %v, want [100 150]", got)
	}
	roles := e.Snapshot().Roles
	if roles[0].SessionTokens != 100 || roles[1].SessionTokens != 250 {
		t.Fatalf("session token totals = %d, %d, want 100 and 250", roles[0].SessionTokens, roles[1].SessionTokens)
	}
}

// A new session id starts from zero, and a total below the last one is taken
// whole. A fallback to another row returns a new session id.
func TestFreshSessionAfterFallbackStartsFromZero(t *testing.T) {
	t.Run("fallback row", func(t *testing.T) {
		lineup := []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}, {Vendor: "claude", Model: "opus", Effort: "high"}}
		e := usageRounds(t, lineup, 0,
			Result{Session: "a", CostUSD: 1.00},
			Result{Exit: ExitRateLimited, Session: "a", CostUSD: 1.50},
			Result{Session: "b", CostUSD: 1.80})
		got := costs(e)
		if len(got) != 3 || !near(got[0], 1.00) || !near(got[1], 0.50) || !near(got[2], 1.80) {
			t.Fatalf("step costs = %v, want [1 0.5 1.8]", got)
		}
	})
	t.Run("new session id", func(t *testing.T) {
		e := usageRounds(t, oneRow("codex"), 0,
			Result{Session: "a", Tokens: 100},
			Result{Session: "a", Tokens: 250},
			Result{Session: "b", Tokens: 300})
		if got := tokens(e); len(got) != 3 || got[0] != 100 || got[1] != 150 || got[2] != 300 {
			t.Fatalf("step tokens = %v, want [100 150 300]", got)
		}
	})
	t.Run("total below the last", func(t *testing.T) {
		e := usageRounds(t, oneRow("claude"), 0,
			Result{Session: "s", CostUSD: 1.00},
			Result{Session: "s", CostUSD: 1.50},
			Result{Session: "s", CostUSD: 0.40})
		if got := costs(e); len(got) != 3 || !near(got[1], 0.50) || !near(got[2], 0.40) {
			t.Fatalf("step costs = %v, want [1 0.5 0.4]", got)
		}
	})
}

// The run total is the sum of the steps: 1.00 + 0.50, not 1.00 + 1.50.
func TestRunCostSumsStepCosts(t *testing.T) {
	e := usageRounds(t, oneRow("claude"), 0,
		Result{Session: "s", CostUSD: 1.00},
		Result{Session: "s", CostUSD: 1.50})
	if got := sumCost(e); !near(got, 1.50) {
		t.Fatalf("run cost = %v, want 1.5", got)
	}
}

// Session totals 100 then 250 are 250 tokens used, so a budget of 300 holds.
func TestBudgetCountsStepTokens(t *testing.T) {
	e := usageRounds(t, oneRow("codex"), 300,
		Result{Session: "t", Tokens: 100},
		Result{Session: "t", Tokens: 250},
		Result{Session: "t", Tokens: 250})
	roles := e.Snapshot().Roles
	if len(roles) != 3 {
		t.Fatalf("a round was refused for the budget: %d roles, state %s, pending %+v", len(roles), e.Snapshot().State, e.Snapshot().Pending)
	}
	if reason, over := e.overBudget(e.Snapshot()); over {
		t.Fatalf("over budget at 250 tokens of 300: %s", reason)
	}
	// 100 + 150 + 0: the third turn added nothing to the session.
	if got := tokens(e); got[0] != 100 || got[1] != 150 || got[2] != 0 {
		t.Fatalf("step tokens = %v, want [100 150 0]", got)
	}
}
