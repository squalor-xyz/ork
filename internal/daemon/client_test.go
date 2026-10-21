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
