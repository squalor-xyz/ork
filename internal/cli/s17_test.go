// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// S222 (*proposed*): ork approve WAVE-ID approves the runs of a wave in order.

const waveApproveFailedReply = `{"wave":"wave-1","items":[` +
	`{"item":"S1","run":"r1","outcome":"landed","state":"committed"},` +
	`{"item":"S2","run":"r2","outcome":"failed","state":"awaiting-approval"},` +
	`{"item":"S3","run":"r3","outcome":"not-attempted","state":"awaiting-approval"}]}`

const waveApproveLandedReply = `{"wave":"wave-1","items":[` +
	`{"item":"S1","run":"r1","outcome":"landed","state":"committed"},` +
	`{"item":"S2","run":"r2","outcome":"committed","state":"committed"},` +
	`{"item":"S3","run":"r3","outcome":"not-waiting","state":"implement"},` +
	`{"item":"S4","run":"r4","outcome":"done","state":"committed"}]}`

func TestApproveWaveIDApprovesTheWave(t *testing.T) {
	log := &callLog{reply: func(string, any) (string, error) { return waveApproveLandedReply, nil }}
	code, out, errs := runCLI(&App{Call: log.call}, "approve", "wave-1")
	want := "S1 r1 landed\nS2 r2 committed\nS3 r3 not-waiting\nS4 r4 done\n"
	if code != ExitOK || out != want || errs != "" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
	if len(log.calls) != 1 || log.calls[0] != `wave.approve {"wave":"wave-1"}` {
		t.Errorf("calls %q", log.calls)
	}

	// A run that did not land, or that was not tried, makes the exit 1.
	log = &callLog{reply: func(string, any) (string, error) { return waveApproveFailedReply, nil }}
	code, out, errs = runCLI(&App{Call: log.call}, "approve", "wave-1")
	want = "S1 r1 landed\nS2 r2 failed\nS3 r3 not-attempted\n"
	if code != ExitFailure || out != want || !strings.Contains(errs, "S2: the run is awaiting-approval") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}

	// A failed approval names its error.
	log = &callLog{reply: func(string, any) (string, error) {
		return `{"wave":"wave-1","items":[{"item":"S2","run":"r2","outcome":"failed","state":"awaiting-approval","error":{"code":"internal","message":"cannot write the note"}}]}`, nil
	}}
	if code, _, errs = runCLI(&App{Call: log.call}, "approve", "wave-1"); code != ExitFailure || !strings.Contains(errs, "S2: cannot write the note (internal)") {
		t.Fatalf("code %d, stderr %q", code, errs)
	}

	// Only the not-attempted run alone also fails the exit.
	log = &callLog{reply: func(string, any) (string, error) {
		return `{"wave":"wave-1","items":[{"item":"S3","run":"r3","outcome":"not-attempted","state":"awaiting-approval"}]}`, nil
	}}
	if code, _, _ = runCLI(&App{Call: log.call}, "approve", "wave-1"); code != ExitFailure {
		t.Fatalf("not-attempted: code %d", code)
	}

	// --json prints the result, and the exit follows it.
	log = &callLog{reply: func(string, any) (string, error) { return waveApproveFailedReply, nil }}
	code, out, _ = runCLI(&App{Call: log.call}, "approve", "wave-1", "--json")
	if code != ExitFailure || !strings.Contains(out, `"outcome": "not-attempted"`) || !strings.Contains(out, `"wave": "wave-1"`) {
		t.Fatalf("json: code %d, stdout %q", code, out)
	}
}

// A mark that skips a check is not for a wave: --later-checks stays with one run.
func TestApproveWaveIDRefusesLaterChecks(t *testing.T) {
	log := &callLog{}
	code, _, errs := runCLI(&App{Call: log.call}, "approve", "wave-1", "--later-checks")
	if code != ExitUsage || !strings.Contains(errs, "--later-checks") || len(log.calls) != 0 {
		t.Fatalf("code %d, stderr %q, calls %q", code, errs, log.calls)
	}
}

func TestApproveRunIDStillApprovesOneRun(t *testing.T) {
	log := &callLog{}
	code, out, errs := runCLI(&App{Call: log.call}, "approve", "r1")
	if code != ExitOK || out != "r1 awaiting-approval\n" || errs != "" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
	if len(log.calls) != 1 || log.calls[0] != `run.approve {"run":"r1"}` {
		t.Errorf("calls %q", log.calls)
	}
}

func TestUsageNamesApproveWave(t *testing.T) {
	_, out, _ := runCLI(&App{}, "help")
	if !strings.Contains(out, "ork approve WAVE-ID") {
		t.Errorf("help omits ork approve WAVE-ID:\n%s", out)
	}
}

func TestS17UsageNamesDaemonRun(t *testing.T) {
	var out, errs bytes.Buffer
	(&App{}).Run(context.Background(), []string{"help"}, &out, &errs)
	if !strings.Contains(out.String(), "ork daemon run") {
		t.Fatal("usage omits ork daemon run")
	}
}
