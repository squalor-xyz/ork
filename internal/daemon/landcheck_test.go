// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestRunLandCheckOnlyForAwaitingApprovalWithLandStop(t *testing.T) {
	for _, tc := range []struct {
		state, stop string
		active      bool
	}{
		{runrec.StateImplement, "", false},
		{runrec.StateAwaitingApproval, runrec.StopAfterCommit, false},
		{runrec.StateAwaitingApproval, "", true},
		{runrec.StateAwaitingApproval, runrec.StopAfterLand, true},
	} {
		t.Run(tc.state+"-"+tc.stop, func(t *testing.T) {
			d, store, p := runDaemon(t, nil)
			if _, err := store.Create(runrec.Record{ID: "check", Project: p.Name, State: tc.state, StopAfter: tc.stop, Branch: "S1", Worktree: filepath.Join(p.Path, ".worktrees/S1")}); err != nil {
				t.Fatal(err)
			}
			raw := request(t, d, MethodRunLandCheck, `{"run":"check"}`)
			var got engine.LandCheck
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			want := engine.LandCheck{}
			if tc.active {
				want.Base = "main"
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("landing check = %+v; want %+v", got, want)
			}
			if !tc.active && len(lastFake.CallsTo("git")) != 0 {
				t.Fatal("inactive landing check ran git")
			}
		})
	}
}

func TestRunLandCheckUnknownRunIsNotFound(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	_, err := d.handle(context.Background(), MethodRunLandCheck, json.RawMessage(`{"run":"missing"}`))
	var wire *ipc.Error
	if !errors.As(err, &wire) || wire.Code != CodeNotFound {
		t.Fatalf("unknown run error = %v; want not_found", err)
	}
}

func TestRunLandCheckDoesNotWaitForTheRun(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	if _, err := store.Create(runrec.Record{ID: "check", Project: p.Name, State: runrec.StateAwaitingApproval, Branch: "S1", Worktree: filepath.Join(p.Path, ".worktrees/S1")}); err != nil {
		t.Fatal(err)
	}
	// A manager lock held for another run must not delay this stored read.
	d.runs.mu.Lock()
	d.runs.runs["busy"] = &managedRun{busy: true}
	type reply struct {
		value any
		err   error
	}
	done := make(chan reply, 1)
	go func() {
		v, err := d.handle(context.Background(), MethodRunLandCheck, json.RawMessage(`{"run":"check"}`))
		done <- reply{v, err}
	}()
	var got reply
	timedOut := false
	select {
	case got = <-done:
	case <-time.After(time.Second):
		timedOut = true
	}
	delete(d.runs.runs, "busy")
	d.runs.mu.Unlock()
	if timedOut {
		<-done
		t.Fatal("landing check waited for the run-manager lock")
	}
	if got.err != nil || !reflect.DeepEqual(got.value, engine.LandCheck{Base: "main"}) {
		t.Fatalf("stored read = %+v, %v", got.value, got.err)
	}
}

func TestRunLandCheckReturnsFileOverlap(t *testing.T) {
	root := t.TempDir()
	pth := filepath.Join(root, "p")
	worktree := filepath.Join(pth, ".worktrees/S1")
	d, store, p := runDaemonIn(t, root, nil,
		clitest.Route{Tool: "git", Args: []string{"-C", worktree, "diff"}, Prefix: true, Stdout: "a.go\x00"},
		clitest.Route{Tool: "git", Args: []string{"-C", pth, "status"}, Prefix: true, Stdout: " M a.go\x00"},
	)
	if _, err := store.Create(runrec.Record{ID: "check", Project: p.Name, State: runrec.StateAwaitingApproval, Branch: "S1", Worktree: worktree}); err != nil {
		t.Fatal(err)
	}
	var got engine.LandCheck
	raw := request(t, d, MethodRunLandCheck, `{"run":"check"}`)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if want := (engine.LandCheck{Base: "main", Blockers: []string{"a.go"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("landing check = %+v; want %+v", got, want)
	}
}
