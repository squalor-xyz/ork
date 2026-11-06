// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRetryCallsDaemon(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			var out, errs bytes.Buffer
			app := &App{Call: func(_ context.Context, method string, params, result any) error {
				b, err := json.Marshal(params)
				if err != nil || method != "run.retry" || string(b) != `{"run":"r1","no_wait":true}` {
					t.Fatalf("retry request: %s %s (%v)", method, b, err)
				}
				return json.Unmarshal([]byte(`{"id":"r1","state":"awaiting-approval"}`), result)
			}}
			args := []string{"retry", "r1"}
			want := "r1 awaiting-approval\n"
			if format == "json" {
				args = append(args, "--json")
				want = `"state": "awaiting-approval"`
			}
			if code := app.Run(context.Background(), args, &out, &errs); code != ExitOK || !strings.Contains(out.String(), want) {
				t.Fatalf("retry result: code %d, stdout %q, stderr %q", code, &out, &errs)
			}
		})
	}
}

func TestRetrySendsRoundGrant(t *testing.T) {
	calls, code, _, stderr := ownerCalls(t, "retry", "r1", "--rounds", "4")
	if code != ExitOK || len(calls) != 1 || calls[0] != `run.retry {"run":"r1","rounds":4,"no_wait":true}` {
		t.Fatalf("code %d calls %v stderr %s", code, calls, stderr)
	}
	for _, n := range []string{"0", "-1", "many"} {
		calls, code, _, _ := ownerCalls(t, "retry", "r1", "--rounds", n)
		if code != ExitUsage || len(calls) != 0 {
			t.Errorf("accepted rounds %q", n)
		}
	}
}

// ownerCalls runs the command against a fake daemon that answers every call
// with a run in state implement, and returns the calls it saw as
// "method params" and the CLI result.
func ownerCalls(t *testing.T, args ...string) (calls []string, code int, stdout, stderr string) {
	t.Helper()
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, method+" "+string(b))
		return json.Unmarshal([]byte(`{"id":"r1","state":"implement"}`), result)
	}}
	code, stdout, stderr = run(t, app, args...)
	return calls, code, stdout, stderr
}

func TestAnswerAndRetrySendNoWaitByDefault(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"answer", "r1", "text"}, `run.answer {"run":"r1","text":"text","no_wait":true}`},
		{[]string{"retry", "r1"}, `run.retry {"run":"r1","no_wait":true}`},
	} {
		calls, code, stdout, stderr := ownerCalls(t, tc.args...)
		if code != ExitOK || stdout != "r1 implement\n" || len(calls) != 1 || calls[0] != tc.want {
			t.Errorf("%v: code %d, stdout %q, stderr %q, calls %q, want %q", tc.args, code, stdout, stderr, calls, tc.want)
		}
	}
}

func TestAnswerAndRetryWaitFlagOmitsNoWait(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"answer", "r1", "text", "--wait"}, `run.answer {"run":"r1","text":"text"}`},
		{[]string{"answer", "--wait", "r1", "text"}, `run.answer {"run":"r1","text":"text"}`},
		{[]string{"retry", "r1", "--wait"}, `run.retry {"run":"r1"}`},
		{[]string{"retry", "--wait", "r1"}, `run.retry {"run":"r1"}`},
	} {
		calls, code, _, stderr := ownerCalls(t, tc.args...)
		if code != ExitOK || len(calls) != 1 || calls[0] != tc.want {
			t.Errorf("%v: code %d, stderr %q, calls %q, want %q", tc.args, code, stderr, calls, tc.want)
		}
	}
}

func TestApproveFixCheckAndStopAreUnchanged(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"approve", "r1"}, `run.approve {"run":"r1"}`},
		{[]string{"stop", "r1"}, `run.stop {"run":"r1"}`},
		{[]string{"fix", "r1", "tidy"}, `run.fix {"run":"r1","text":"tidy"}`},
		{[]string{"check", "r1", "2"}, `run.check {"check":2,"state":"ticked","run":"r1"}`},
	} {
		calls, code, _, stderr := ownerCalls(t, tc.args...)
		if code != ExitOK || len(calls) != 1 || calls[0] != tc.want {
			t.Errorf("%v: code %d, stderr %q, calls %q, want %q", tc.args, code, stderr, calls, tc.want)
		}
	}
	// --wait belongs to answer and retry only.
	for _, cmd := range []string{"approve", "stop"} {
		calls, code, _, _ := ownerCalls(t, cmd, "r1", "--wait")
		if code != ExitUsage || len(calls) != 0 {
			t.Errorf("%s --wait: code %d, calls %q, want a usage error", cmd, code, calls)
		}
	}
}

func TestUsageNamesTheWaitFlag(t *testing.T) {
	var out, errs bytes.Buffer
	(&App{}).Run(context.Background(), []string{"help"}, &out, &errs)
	for _, want := range []string{"ork answer RUN TEXT [--wait]", "ork retry RUN [TEXT] [--wait]"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help omits %q:\n%s", want, &out)
		}
	}
}

func TestAnswerEarlyReplyJSON(t *testing.T) {
	_, code, stdout, stderr := ownerCalls(t, "answer", "r1", "text", "--json")
	var rec struct{ ID, State string }
	if err := json.Unmarshal([]byte(stdout), &rec); code != ExitOK || err != nil || rec.ID != "r1" || rec.State != "implement" {
		t.Fatalf("code %d, stdout %q, stderr %q, err %v", code, stdout, stderr, err)
	}
}

func TestRetryTakesOneRun(t *testing.T) {
	for _, args := range [][]string{{"retry"}, {"retry", "r1", "a", "b"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			var out, errs bytes.Buffer
			code := (&App{}).Run(context.Background(), args, &out, &errs)
			if code != ExitUsage || !strings.Contains(errs.String(), "retry takes RUN [TEXT]") {
				t.Fatalf("retry usage: code %d, stderr %q", code, &errs)
			}
		})
	}
}

func TestRetryTakesAnOptionalNote(t *testing.T) {
	calls, code, _, stderr := ownerCalls(t, "retry", "r1", "re-read testdata")
	if want := `run.retry {"run":"r1","text":"re-read testdata","no_wait":true}`; code != ExitOK || len(calls) != 1 || calls[0] != want {
		t.Errorf("retry with a note: code %d, stderr %q, calls %q, want %q", code, stderr, calls, want)
	}
	calls, code, _, stderr = ownerCalls(t, "retry", "r1", "re-read testdata", "--wait")
	if want := `run.retry {"run":"r1","text":"re-read testdata"}`; code != ExitOK || len(calls) != 1 || calls[0] != want {
		t.Errorf("retry with a note and --wait: code %d, stderr %q, calls %q, want %q", code, stderr, calls, want)
	}
	calls, code, _, stderr = ownerCalls(t, "retry", "r1")
	if want := `run.retry {"run":"r1","no_wait":true}`; code != ExitOK || len(calls) != 1 || calls[0] != want {
		t.Errorf("retry without a note: code %d, stderr %q, calls %q, want %q", code, stderr, calls, want)
	}
	calls, code, _, stderr = ownerCalls(t, "retry", "r1", "  ")
	if code != ExitUsage || len(calls) != 0 || !strings.Contains(stderr, "retry TEXT is empty") {
		t.Errorf("retry with a blank note: code %d, stderr %q, calls %q", code, stderr, calls)
	}
}

func TestUsageNamesRetry(t *testing.T) {
	var out, errs bytes.Buffer
	(&App{}).Run(context.Background(), []string{"help"}, &out, &errs)
	if !strings.Contains(out.String(), "ork retry RUN [TEXT]") {
		t.Fatalf("help omits retry: %s", &out)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "ork retry RUN") && len(line) > 80 {
			t.Errorf("usage line over 80 columns: %q", line)
		}
	}
}
