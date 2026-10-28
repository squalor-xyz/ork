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

// callLog is a daemon double that keeps the method and params of each call and
// answers with a run record.
type callLog struct {
	calls []string
	reply func(method string, params any) (string, error)
}

func (l *callLog) call(_ context.Context, method string, params, result any) error {
	b, _ := json.Marshal(params)
	l.calls = append(l.calls, method+" "+string(b))
	record := `{"id":"r1","state":"awaiting-approval"}`
	if l.reply != nil {
		var err error
		if record, err = l.reply(method, params); err != nil {
			return err
		}
	}
	return json.Unmarshal([]byte(record), result)
}

func runCLI(app *App, args ...string) (code int, out, errs string) {
	var o, e bytes.Buffer
	code = app.Run(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

func TestOrkCheckMarksAHumanCheck(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"check", "r1", "2"}, `run.check {"check":2,"state":"ticked","run":"r1"}`},
		{[]string{"check", "r1", "2", "ticked"}, `run.check {"check":2,"state":"ticked","run":"r1"}`},
		{[]string{"check", "r1", "12", "later"}, `run.check {"check":12,"state":"later","run":"r1"}`},
		{[]string{"check", "r1", "3", "open"}, `run.check {"check":3,"run":"r1"}`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			log := &callLog{}
			code, out, errs := runCLI(&App{Call: log.call}, tc.args...)
			if code != ExitOK || len(log.calls) != 1 || log.calls[0] != tc.want || out != "r1 awaiting-approval\n" {
				t.Fatalf("code %d, calls %q, stdout %q, stderr %q", code, log.calls, out, errs)
			}
		})
	}
	t.Run("json", func(t *testing.T) {
		log := &callLog{}
		code, out, _ := runCLI(&App{Call: log.call}, "check", "r1", "1", "--json")
		if code != ExitOK || !strings.Contains(out, `"state": "awaiting-approval"`) {
			t.Fatalf("code %d, stdout %q", code, out)
		}
	})
}

func TestOrkCheckTakesRunNumberAndState(t *testing.T) {
	for _, args := range [][]string{{"check"}, {"check", "r1"}, {"check", "r1", "x"}, {"check", "r1", "0"}, {"check", "r1", "2", "done"}, {"check", "r1", "2", "later", "extra"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			log := &callLog{}
			code, _, errs := runCLI(&App{Call: log.call}, args...)
			if code != ExitUsage || !strings.Contains(errs, "check takes RUN N") || len(log.calls) != 0 {
				t.Fatalf("code %d, stderr %q, calls %q", code, errs, log.calls)
			}
		})
	}
}

func TestOrkFixFilesTheOwnerFix(t *testing.T) {
	log := &callLog{}
	code, out, errs := runCLI(&App{Call: log.call}, "fix", "r1", "Rename the flag to --json.")
	if code != ExitOK || len(log.calls) != 1 || log.calls[0] != `run.fix {"run":"r1","text":"Rename the flag to --json."}` || out != "r1 awaiting-approval\n" {
		t.Fatalf("code %d, calls %q, stdout %q, stderr %q", code, log.calls, out, errs)
	}
	for _, args := range [][]string{{"fix"}, {"fix", "r1"}, {"fix", "r1", "   "}, {"fix", "r1", "a", "b"}} {
		log := &callLog{}
		code, _, errs := runCLI(&App{Call: log.call}, args...)
		if code != ExitUsage || !strings.Contains(errs, "fix takes RUN TEXT") || len(log.calls) != 0 {
			t.Errorf("%q: code %d, stderr %q, calls %q", args, code, errs, log.calls)
		}
	}
}

// A run with human checks is approved from the shell: ork approve names the
// open ones, ork check marks them, and ork approve then goes through.
func TestOrkApproveFromTheShellWithChecks(t *testing.T) {
	open := map[int]bool{1: true, 2: true}
	log := &callLog{reply: func(method string, params any) (string, error) {
		a := params.(daemon.RunAction)
		switch method {
		case "run.check":
			delete(open, a.Check)
		case "run.approve":
			if len(open) > 0 {
				return "", &ipc.Error{Code: "internal", Message: "2 human checks still open; tick each or mark it later: 1. One; 2. Two"}
			}
			return `{"id":"r1","state":"committed"}`, nil
		}
		return `{"id":"r1","state":"awaiting-approval"}`, nil
	}}
	app := &App{Call: log.call}
	code, out, errs := runCLI(app, "approve", "r1")
	if code != ExitFailure || out != "" || !strings.Contains(errs, "2 human checks still open") || !strings.Contains(errs, "1. One; 2. Two") {
		t.Fatalf("approve with open checks: code %d, stdout %q, stderr %q", code, out, errs)
	}
	for _, args := range [][]string{{"check", "r1", "1"}, {"check", "r1", "2", "later"}} {
		if code, _, errs := runCLI(app, args...); code != ExitOK {
			t.Fatalf("%q: code %d, stderr %q", args, code, errs)
		}
	}
	code, out, errs = runCLI(app, "approve", "r1")
	if code != ExitOK || out != "r1 committed\n" {
		t.Fatalf("approve after the marks: code %d, stdout %q, stderr %q", code, out, errs)
	}
	// In JSON the refusal keeps the daemon's error object.
	open[1] = true
	code, out, errs = runCLI(app, "approve", "r1", "--json")
	var obj struct {
		Error struct{ Code, Message string }
	}
	if code != ExitFailure || json.Unmarshal([]byte(out+errs), &obj) != nil || obj.Error.Code == "" || !strings.Contains(obj.Error.Message, "human checks still open") {
		t.Fatalf("json refusal: code %d, stdout %q, stderr %q", code, out, errs)
	}
}

func TestUsageNamesCheckAndFix(t *testing.T) {
	_, out, _ := runCLI(&App{}, "help")
	for _, want := range []string{"ork check RUN N", "ork fix RUN TEXT"} {
		if !strings.Contains(out, want) {
			t.Errorf("help omits %q:\n%s", want, out)
		}
	}
}

// A refusal keeps the daemon's code in --json and prints its text as it is.
func TestOrkRefusalPrintsTheTextAndKeepsTheCode(t *testing.T) {
	log := &callLog{reply: func(string, any) (string, error) {
		return "", &ipc.Error{Code: daemon.CodeRefused, Message: "the slice is stopped, not awaiting approval"}
	}}
	code, out, errs := runCLI(&App{Call: log.call}, "approve", "r1")
	if code != ExitFailure || out != "" || errs != "ork: the slice is stopped, not awaiting approval\n" {
		t.Fatalf("text: code %d, stdout %q, stderr %q", code, out, errs)
	}
	code, out, _ = runCLI(&App{Call: log.call}, "approve", "r1", "--json")
	var obj struct {
		Error struct{ Code, Message string }
	}
	if code != ExitFailure || json.Unmarshal([]byte(out), &obj) != nil || obj.Error.Code != "refused" || obj.Error.Message != "the slice is stopped, not awaiting approval" {
		t.Fatalf("json: code %d, stdout %q", code, out)
	}
}
