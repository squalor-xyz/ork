// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
)

func TestTrackingCommitCLI(t *testing.T) {
	var sent []string
	a := App{Call: func(_ context.Context, method string, params, result any) error {
		b, _ := json.Marshal(params)
		sent = append(sent, method+" "+string(b))
		return json.Unmarshal([]byte(`{"hash":"3b1c2d4","subject":"Tracking edits on main: S9; adj 7","files":3}`), result)
	}}
	var out, errout bytes.Buffer
	if code := a.Run(context.Background(), []string{"tracking", "commit", "p", "--leave", ".slicer/slices/S8.json"}, &out, &errout); code != 0 {
		t.Fatalf("code %d out %s err %s", code, &out, &errout)
	}
	if len(sent) != 1 || sent[0] != `tracking.commit {"project":"p","leave":[".slicer/slices/S8.json"]}` {
		t.Errorf("sent %q", sent)
	}
	if got := out.String(); got != "committed 3b1c2d4 Tracking edits on main: S9; adj 7 (3 files)\n" {
		t.Errorf("output %q", got)
	}
}

func TestTrackingCommitCLISendsTheBaseAndEveryLeave(t *testing.T) {
	var sent string
	a := App{Call: func(_ context.Context, _ string, params, result any) error {
		b, _ := json.Marshal(params)
		sent = string(b)
		return json.Unmarshal([]byte(`{"hash":"3b1c2d4","subject":"Tracking edits on dev","files":1}`), result)
	}}
	var out, errout bytes.Buffer
	args := []string{"tracking", "commit", "--base", "dev", "p", "--leave", "a", "--leave", "b"}
	if code := a.Run(context.Background(), args, &out, &errout); code != 0 {
		t.Fatalf("code %d err %s", code, &errout)
	}
	if sent != `{"project":"p","base":"dev","leave":["a","b"]}` {
		t.Errorf("sent %s", sent)
	}
	if !strings.HasSuffix(out.String(), "(1 file)\n") {
		t.Errorf("output %q, want the singular", out.String())
	}
}

func TestTrackingCommitCLIJSON(t *testing.T) {
	a := App{Call: func(_ context.Context, _ string, _, result any) error {
		return json.Unmarshal([]byte(`{"hash":"3b1c2d4","subject":"Tracking edits on main","files":2}`), result)
	}}
	var out, errout bytes.Buffer
	if code := a.Run(context.Background(), []string{"tracking", "commit", "p", "--json"}, &out, &errout); code != 0 {
		t.Fatalf("code %d err %s", code, &errout)
	}
	var got daemon.TrackingCommitResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Hash != "3b1c2d4" || got.Files != 2 {
		t.Errorf("output %s (%v)", &out, err)
	}
}

func TestTrackingCommitCLIFailures(t *testing.T) {
	a := App{Call: func(_ context.Context, _ string, _, _ any) error {
		return &ipc.Error{Code: daemon.CodeNothingToCommit, Message: "no uncommitted tracking files"}
	}}
	var out, errout bytes.Buffer
	if code := a.Run(context.Background(), []string{"tracking", "commit", "p"}, &out, &errout); code != 1 {
		t.Errorf("code %d, want 1", code)
	}
	if errout.String() != "ork: no uncommitted tracking files\n" || out.Len() != 0 {
		t.Errorf("out %q err %q", &out, &errout)
	}
	for _, args := range [][]string{{"tracking"}, {"tracking", "push"}, {"tracking", "commit"}, {"tracking", "commit", "p", "q"}} {
		calls := 0
		b := App{Call: func(context.Context, string, any, any) error { calls++; return nil }}
		out.Reset()
		errout.Reset()
		if code := b.Run(context.Background(), args, &out, &errout); code != 2 || calls != 0 || !strings.Contains(errout.String(), "tracking") {
			t.Errorf("%v: code %d calls %d err %q", args, code, calls, &errout)
		}
	}
}

func TestTrackingCommitHelp(t *testing.T) {
	var out, errout bytes.Buffer
	a := App{}
	if code := a.Run(context.Background(), []string{"tracking", "commit", "--help"}, &out, &errout); code != 0 || !strings.Contains(out.String(), "ork tracking commit P") {
		t.Errorf("code %d out %q err %q", code, &out, &errout)
	}
}
