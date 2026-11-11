// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"strings"
	"testing"
)

// S316 (*proposed*; adj 233, 234): ork approve RUN --later-checks and ork check
// RUN all later defer every open human check in one call.

func deferredReply(state string) *callLog {
	return &callLog{reply: func(string, any) (string, error) {
		return `{"id":"r1","state":"` + state + `","deferred":["1. Read the card at 80x24.","2. Press F and send a note."]}`, nil
	}}
}

func TestApproveLaterChecksPrintsEachDeferredCheck(t *testing.T) {
	log := deferredReply("committed")
	code, out, errs := runCLI(&App{Call: log.call}, "approve", "r1", "--later-checks")
	want := "deferred: 1. Read the card at 80x24.\ndeferred: 2. Press F and send a note.\nr1 committed\n"
	if code != ExitOK || out != want || errs != "" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
	if len(log.calls) != 1 || log.calls[0] != `run.approve {"run":"r1","later_checks":true}` {
		t.Errorf("calls %q", log.calls)
	}
}

func TestApproveLaterChecksOnARunWithNoChecksPrintsTheRunOnly(t *testing.T) {
	log := &callLog{reply: func(string, any) (string, error) { return `{"id":"r1","state":"committed"}`, nil }}
	code, out, errs := runCLI(&App{Call: log.call}, "approve", "r1", "--later-checks")
	if code != ExitOK || out != "r1 committed\n" || errs != "" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
}

func TestApproveLaterChecksJSONCarriesTheDeferredChecks(t *testing.T) {
	log := deferredReply("committed")
	code, out, _ := runCLI(&App{Call: log.call}, "approve", "r1", "--later-checks", "--json")
	if code != ExitOK || !strings.Contains(out, `"state": "committed"`) || !strings.Contains(out, `"1. Read the card at 80x24."`) {
		t.Fatalf("code %d, stdout %q", code, out)
	}
}

// Without the flag the call is the one it was.
func TestApproveWithoutTheFlagSendsNoLaterChecks(t *testing.T) {
	log := &callLog{}
	if code, _, errs := runCLI(&App{Call: log.call}, "approve", "r1"); code != ExitOK {
		t.Fatalf("code %d, stderr %q", code, errs)
	}
	if len(log.calls) != 1 || log.calls[0] != `run.approve {"run":"r1"}` {
		t.Errorf("calls %q", log.calls)
	}
}

func TestOnlyApproveTakesLaterChecks(t *testing.T) {
	for _, cmd := range []string{"stop", "retry", "dismiss", "check", "fix"} {
		log := &callLog{}
		code, _, _ := runCLI(&App{Call: log.call}, cmd, "r1", "--later-checks")
		if code != ExitUsage || len(log.calls) != 0 {
			t.Errorf("%s --later-checks: code %d, calls %q", cmd, code, log.calls)
		}
	}
}

func TestCheckAllLaterDefersEveryOpenCheckAndDoesNotApprove(t *testing.T) {
	log := deferredReply("awaiting-approval")
	code, out, errs := runCLI(&App{Call: log.call}, "check", "r1", "all", "later")
	want := "deferred: 1. Read the card at 80x24.\ndeferred: 2. Press F and send a note.\nr1 awaiting-approval\n"
	if code != ExitOK || out != want || errs != "" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
	if len(log.calls) != 1 || log.calls[0] != `run.check {"state":"later","run":"r1","all":true}` {
		t.Errorf("calls %q", log.calls)
	}
}

// Ticking says the owner looked, so all takes later only (adj 234).
func TestCheckAllTakesLaterOnly(t *testing.T) {
	for _, args := range [][]string{{"check", "r1", "all"}, {"check", "r1", "all", "ticked"}, {"check", "r1", "all", "open"}, {"check", "r1", "all", "later", "extra"}} {
		log := &callLog{}
		code, _, errs := runCLI(&App{Call: log.call}, args...)
		if code != ExitUsage || !strings.Contains(errs, "check RUN all later") || len(log.calls) != 0 {
			t.Errorf("%q: code %d, stderr %q, calls %q", args, code, errs, log.calls)
		}
	}
}

func TestUsageNamesLaterChecksAndAll(t *testing.T) {
	_, out, _ := runCLI(&App{}, "help")
	for _, want := range []string{"ork approve RUN [--later-checks]", "ork check RUN all later"} {
		if !strings.Contains(out, want) {
			t.Errorf("help omits %q:\n%s", want, out)
		}
	}
}
