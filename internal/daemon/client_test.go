// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestClientPlanAndStart(t *testing.T) {
	for _, method := range []string{MethodRunPlan, MethodRunStart} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/error-%t", method, fail), func(t *testing.T) {
				plan := RunPlan{Project: registry.Project{Name: "ork"}, Item: "S52", Worktree: "/ork/.worktrees/S52", Branch: "S52", CheckCommand: "go test ./...", Lineup: map[string][]runrec.LineupEntry{"implementer": {{Vendor: "Codex", Model: "gpt", Effort: "high"}}}}
				params := RunParams{Project: "ork", Item: "S52", WellSpecified: true, Split: false}
				if method == MethodRunStart {
					params.Expected = &plan
				}
				socket := filepath.Join(shortDir(t), "s")
				listener, err := ipc.Listen(socket)
				if err != nil {
					t.Fatal(err)
				}
				server := &ipc.Server{Handler: func(_ context.Context, gotMethod string, raw json.RawMessage) (any, error) {
					var got RunParams
					if err := json.Unmarshal(raw, &got); err != nil {
						return nil, err
					}
					if gotMethod != method || !reflect.DeepEqual(got, params) {
						return nil, fmt.Errorf("wrong request: %s %s", gotMethod, raw)
					}
					if fail {
						return nil, &ipc.Error{Code: "launch_failed", Message: "Exact launch error."}
					}
					if method == MethodRunPlan {
						return plan, nil
					}
					return runrec.Record{ID: "engine", Item: "S52", State: "setup"}, nil
				}}
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan error, 1)
				go func() { done <- server.Serve(ctx, listener) }()
				t.Cleanup(func() {
					cancel()
					select {
					case <-done:
					case <-time.After(2 * time.Second):
						t.Error("server did not stop")
					}
				})
				c, err := Dial(ctx, socket)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if method == MethodRunPlan {
					got, callErr := c.Plan(ctx, params)
					err = callErr
					if !fail && (err != nil || !reflect.DeepEqual(got, &plan)) {
						t.Fatalf("plan: %+v %v", got, err)
					}
				} else {
					got, callErr := c.Start(ctx, params)
					err = callErr
					if !fail && (err != nil || got.ID != "engine" || got.Item != "S52" || got.State != "setup") {
						t.Fatalf("start: %+v %v", got, err)
					}
				}
				if fail {
					var e *ipc.Error
					if !errors.As(err, &e) || e.Code != "launch_failed" || e.Message != "Exact launch error." {
						t.Fatalf("changed error: %v", err)
					}
				}
			})
		}
	}
}

func TestClientSpecCheckAndAnswer(t *testing.T) {
	socket := filepath.Join(shortDir(t), "s")
	listener, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &ipc.Server{Handler: func(_ context.Context, method string, raw json.RawMessage) (any, error) {
		switch method {
		case MethodSpecCheck:
			var got SpecCheckParams
			if err := json.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(got, SpecCheckParams{Project: "ork", Items: []string{"S1", "S2"}}) {
				return nil, fmt.Errorf("wrong check request: %s", raw)
			}
			return SpecCheckResult{Items: []SpecItemResult{{Item: "S1", Ready: true}}}, nil
		case MethodSpecAnswer:
			var got SpecAnswerParams
			if err := json.Unmarshal(raw, &got); err != nil || got.Item != "S2" || !got.Adj || got.By != "jon" || !reflect.DeepEqual(got.Answers, []string{"a"}) {
				return nil, fmt.Errorf("wrong answer request: %s", raw)
			}
			return SpecAnswerResult{Item: "S2", Ready: true, Notices: []string{"n"}}, nil
		}
		return nil, fmt.Errorf("unexpected %s", method)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server did not stop")
		}
	})
	c, err := Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	res, err := c.SpecCheck(ctx, "ork", []string{"S1", "S2"})
	if err != nil || len(res.Items) != 1 || !res.Items[0].Ready {
		t.Fatalf("check: %+v %v", res, err)
	}
	ans, err := c.SpecAnswer(ctx, SpecAnswerParams{Project: "ork", Item: "S2", Answers: []string{"a"}, Adj: true, By: "jon"})
	if err != nil || !ans.Ready || !reflect.DeepEqual(ans.Notices, []string{"n"}) {
		t.Fatalf("answer: %+v %v", ans, err)
	}
}

// ApproveRecord returns the run record the approve reply carries, with the
// fields that tell a landing from a landing fix round (S148); Approve keeps
// its error-only result.
func TestClientApproveRecordCarriesTheLandingFields(t *testing.T) {
	landedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	socket := filepath.Join(shortDir(t), "s")
	listener, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &ipc.Server{Handler: func(_ context.Context, method string, raw json.RawMessage) (any, error) {
		var got RunAction
		if err := json.Unmarshal(raw, &got); err != nil || method != MethodRunApprove {
			return nil, fmt.Errorf("wrong request: %s %s", method, raw)
		}
		switch got.Run {
		case "landed":
			return RunReply{Record: runrec.Record{ID: "landed", State: runrec.StateCommitted, LandedAt: landedAt}}, nil
		case "fix":
			return RunReply{Record: runrec.Record{ID: "fix", State: runrec.StateAwaitingApproval, LandFixes: 1}}, nil
		}
		return nil, &ipc.Error{Code: "bad_params", Message: "no such run"}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server did not stop")
		}
	})
	c, err := Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	rec, err := c.ApproveRecord(ctx, "landed")
	if err != nil || rec.State != runrec.StateCommitted || !rec.LandedAt.Equal(landedAt) {
		t.Fatalf("landed: %+v %v", rec, err)
	}
	rec, err = c.ApproveRecord(ctx, "fix")
	if err != nil || rec.State != runrec.StateAwaitingApproval || rec.LandFixes != 1 {
		t.Fatalf("fix round: %+v %v", rec, err)
	}
	if err := c.Approve(ctx, "landed"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := c.ApproveRecord(ctx, "missing"); err == nil {
		t.Fatal("an error reply was swallowed")
	}
}
