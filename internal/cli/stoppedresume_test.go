// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// S296 (*proposed*): ork run and ork wave start say what they did with the
// item's stopped run; ork retry and ork show name the resume.

func startReply(t *testing.T, reply string, args ...string) (code int, out, errs string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := &App{Input: strings.NewReader("yes\n"), Call: func(_ context.Context, method string, params, result any) error {
		switch method {
		case daemon.MethodRunPlan:
			return json.Unmarshal([]byte(`{"project":{"name":"p"},"item":"S1","check_command":"go test ./..."}`), result)
		case daemon.MethodSpecCheck:
			return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true}]}`), result)
		case daemon.MethodRunStart, daemon.MethodWaveStart:
			return json.Unmarshal([]byte(reply), result)
		}
		t.Fatalf("method %s", method)
		return nil
	}}
	code = app.Run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRunPrintsWhatTheStartDidWithAStoppedRun(t *testing.T) {
	const message = "resumed stopped run r1: its worktree is unchanged since implementer round 3; granted 2 more rounds"
	reply := `{"id":"r1","state":"checks","message":"` + message + `"}`
	code, out, errs := startReply(t, reply, "run", "p", "S1", "--yes")
	if code != ExitOK || !strings.Contains(out, "r1 checks\n"+message+"\n") {
		t.Fatalf("code %d, out %q, err %q", code, out, errs)
	}
	// A start with nothing to say prints the one line it always did.
	code, out, _ = startReply(t, `{"id":"r2","state":"setup"}`, "run", "p", "S1", "--yes")
	if code != ExitOK || !strings.HasSuffix(out, "r2 setup\n") {
		t.Fatalf("code %d, out %q", code, out)
	}
	// JSON carries the message as a field of the one result value.
	code, out, _ = startReply(t, reply, "run", "p", "S1", "--yes", "--json")
	if code != ExitOK {
		t.Fatalf("code %d, json out %q", code, out)
	}
	if !strings.Contains(out, `"message": "`+message+`"`) && !strings.Contains(out, `"message":"`+message+`"`) {
		t.Fatalf("json out lacks the message: %q", out)
	}
}

func TestWaveStartPrintsWhatTheStartDidWithAStoppedRun(t *testing.T) {
	const message = "started a new run: stopped run r1 was not resumed (its worktree changed since implementer round 3); the new run starts at implementer round 1 in the same worktree"
	reply := `{"wave":"wave-1","items":[{"item":"S1","run":{"id":"r9","state":"setup"},"message":"` + message + `"}]}`
	code, out, errs := startReply(t, reply, "wave", "start", "p", "S1", "--yes")
	if code != ExitOK || !strings.Contains(out, "r9 setup\nS1: "+message+"\n") {
		t.Fatalf("code %d, out %q, err %q", code, out, errs)
	}
}

func TestRetryUsageNamesAStoppedRun(t *testing.T) {
	var out, errs bytes.Buffer
	if code := (&App{}).Run(context.Background(), []string{"help"}, &out, &errs); code != ExitOK {
		t.Fatalf("help: %d %s", code, errs.String())
	}
	help := out.String()
	i := strings.Index(help, "ork retry RUN")
	flat := strings.Join(strings.Fields(help[max(i, 0):]), " ")
	if i < 0 || !strings.Contains(flat, "a stopped run resumes from its worktree with N more implementer rounds (default 2) and takes no TEXT") {
		t.Fatalf("the retry usage does not name a stopped run:\n%s", help[max(i, 0):])
	}
}

func TestRetryOfAStoppedRunSendsTheGrant(t *testing.T) {
	var got daemon.RunAction
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		if method != "run.retry" {
			t.Fatalf("method %s", method)
		}
		got = params.(daemon.RunAction)
		return json.Unmarshal([]byte(`{"id":"r1","state":"checks"}`), result)
	}}
	var out, errs bytes.Buffer
	if code := app.Run(context.Background(), []string{"retry", "r1", "--rounds", "3"}, &out, &errs); code != ExitOK || got.Run != "r1" || got.Rounds != 3 || !got.NoWait {
		t.Fatalf("code %d, params %+v, err %s", code, got, errs.String())
	}
}

func TestShowNamesTheResumeOfAStoppedRun(t *testing.T) {
	log := showDouble(daemon.ShowReply{ID: "r1", Item: "S9", State: "stopped", Reason: "stopped by the owner: fix the check", Next: "ork retry r1 --rounds N"}, nil)
	code, out, errs := runCLI(&App{Call: log.call}, "show", "r1")
	if code != ExitOK || errs != "" || !strings.Contains(out, "next: ork retry r1 --rounds N") {
		t.Fatalf("code %d, out %q, err %q", code, out, errs)
	}
}
