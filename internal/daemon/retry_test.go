// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const retryReport = "Tests: 1 passed, 0 failed\n\n## Owner needed\nnone\n"

func retryDaemon(t *testing.T, state string, pending *runrec.Pending) (*Daemon, *runrec.Store) {
	t.Helper()
	d, store, p := runDaemon(t, func(_ context.Context, r engine.Request) (engine.Result, error) {
		report := retryReport
		if r.Role == engine.RoleReviewer {
			report = "(A): nothing\n(B): nothing\n(D): nothing\n\n## Owner needed\nnone\n"
		}
		if r.Role == engine.RoleGate {
			report = "VERDICT: PASS\n" + report
		}
		return engine.Result{Exit: engine.ExitOK, Report: report}, nil
	})
	lineup := map[string][]runrec.LineupEntry{}
	for _, role := range []string{engine.RoleImplementer, engine.RoleReviewer, engine.RoleGate} {
		lineup[role] = []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet"}}
	}
	r, err := store.Create(runrec.Record{
		ID: "r1", Project: p.Name, Item: "S1", Worktree: filepath.Join(p.Path, ".worktrees", "S1"),
		State: state, Round: 1, Lineup: lineup, Pending: pending,
	})
	if err != nil {
		t.Fatal(err)
	}
	d.runs.deps.Check = func(context.Context, string, string, time.Duration) (engine.CheckOutput, error) {
		return engine.CheckOutput{OK: true, Output: "retry checks\n"}, nil
	}
	d.runs.runs["r1"] = &managedRun{engine: engine.Load(d.runDeps(p), p, r)}
	return d, store
}

func TestRunRetryResumesPausedRole(t *testing.T) {
	for _, kind := range []string{runrec.PendingShape, runrec.PendingError, runrec.PendingLost, runrec.PendingInterrupted} {
		t.Run(kind, func(t *testing.T) {
			d, store := retryDaemon(t, runrec.StatePaused, &runrec.Pending{Kind: kind, State: runrec.StateImplement, Role: engine.RoleImplementer, Round: 1, Reason: "bad report"})
			b := request(t, d, "run.retry", `{"run":"r1"}`)
			var rec runrec.Record
			if err := json.Unmarshal(b, &rec); err != nil {
				t.Fatal(err)
			}
			if rec.State != runrec.StateAwaitingApproval {
				t.Fatalf("retry did not leave paused for approval: %s", b)
			}
			report, err := os.ReadFile(filepath.Join(store.RunDir("r1"), "implementer-1.md"))
			if err != nil || string(report) != retryReport {
				t.Fatalf("retried implementer report = %q, err = %v", report, err)
			}
		})
	}
}

func TestRunRetryResumesPausedStep(t *testing.T) {
	for _, kind := range []string{runrec.PendingError, runrec.PendingInterrupted} {
		t.Run(kind, func(t *testing.T) {
			d, store := retryDaemon(t, runrec.StatePaused, &runrec.Pending{Kind: kind, State: runrec.StateChecks, Reason: "check failed"})
			b := request(t, d, "run.retry", `{"run":"r1"}`)
			var rec runrec.Record
			if err := json.Unmarshal(b, &rec); err != nil {
				t.Fatal(err)
			}
			if rec.State != runrec.StateAwaitingApproval {
				t.Fatalf("step retry did not reach approval: %s", b)
			}
			log, err := os.ReadFile(filepath.Join(store.RunDir("r1"), "checks-1.log"))
			if err != nil || !strings.Contains(string(log), "retry checks") {
				t.Fatalf("retried check log = %q, err = %v", log, err)
			}
		})
	}
}

func TestRunRetryUnknownRun(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	_, err := d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"missing"}`))
	var wire *ipc.Error
	if !errors.As(err, &wire) || wire.Code != CodeNotFound || !strings.Contains(wire.Message, "missing") {
		t.Fatalf("retry unknown run: %v", err)
	}
}

func TestRunRetryWrongState(t *testing.T) {
	for _, state := range []string{runrec.StateImplement, runrec.StateAwaitingApproval} {
		t.Run(state, func(t *testing.T) {
			d, _ := retryDaemon(t, state, &runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateImplement})
			_, err := d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
			if err == nil || !strings.Contains(err.Error(), "not paused") || !strings.Contains(err.Error(), "ork stop r1") {
				t.Fatalf("retry wrong state needs stop guidance: %v", err)
			}
		})
	}
}

func TestRunRetryQuestionNeedsAnswer(t *testing.T) {
	d, _ := retryDaemon(t, runrec.StatePaused, &runrec.Pending{Kind: runrec.PendingQuestion, State: runrec.StateImplement, Question: "Which library?"})
	_, err := d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
	if err == nil || !strings.Contains(err.Error(), "ork answer r1 TEXT") || !strings.Contains(err.Error(), "ork stop r1") {
		t.Fatalf("retry question needs answer or stop guidance: %v", err)
	}
}

func TestRunRetryBusyNamesStop(t *testing.T) {
	d, _ := retryDaemon(t, runrec.StateImplement, nil)
	d.runs.runs["r1"].busy = true
	_, err := d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
	if err == nil || !strings.Contains(err.Error(), "busy") || !strings.Contains(err.Error(), "ork stop r1") {
		t.Fatalf("retry busy run needs stop guidance: %v", err)
	}
}

func TestRunAnswerWithoutQuestionNamesRetry(t *testing.T) {
	for _, pending := range []*runrec.Pending{nil, {Kind: runrec.PendingShape, State: runrec.StateImplement}} {
		name := "no-pending"
		if pending != nil {
			name = pending.Kind
		}
		t.Run(name, func(t *testing.T) {
			d, _ := retryDaemon(t, runrec.StatePaused, pending)
			_, err := d.handle(context.Background(), "run.answer", json.RawMessage(`{"run":"r1","text":"try again"}`))
			if err == nil || !strings.Contains(err.Error(), "no pending question") || !strings.Contains(err.Error(), "ork retry r1") {
				t.Fatalf("answer without question needs retry guidance: %v", err)
			}
		})
	}
}

func TestPausedStatusNamesNextCommand(t *testing.T) {
	for _, tc := range []struct{ kind, want string }{
		{runrec.PendingShape, "bad report; next: ork retry r1"},
		{runrec.PendingError, "bad report; next: ork retry r1"},
		{runrec.PendingLost, "bad report; next: ork retry r1"},
		{runrec.PendingInterrupted, "bad report; next: ork retry r1"},
		{runrec.PendingQuestion, "Which library?; next: ork answer r1 TEXT"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			pending := &runrec.Pending{Kind: tc.kind, State: runrec.StateImplement, Reason: "bad report", Question: "Which library?"}
			d, _ := retryDaemon(t, runrec.StatePaused, pending)
			b := request(t, d, "status", `{"project":"p"}`)
			if !strings.Contains(string(b), `"wait":"`+tc.want+`"`) {
				t.Fatalf("paused status lacks reason and next command %q: %s", tc.want, b)
			}
		})
	}
}
