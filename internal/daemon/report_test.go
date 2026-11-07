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

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func reportRun(t *testing.T) (*Daemon, *runrec.Store) {
	t.Helper()
	d, store, p := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "r1", Project: p.Name, Item: "S1", State: runrec.StatePaused, Roles: []runrec.RoleRecord{{Role: "gate", Round: 1, Report: "gate-1.md"}, {Role: "implementer", Round: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"gate-1.md": "\x1b[31mREPORT BODY\x1b[0m", "findings-1.md": "unlisted"} {
		if err := os.WriteFile(filepath.Join(store.RunDir("r1"), name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return d, store
}

func TestRunReportReturnsTheTextOfOneReport(t *testing.T) {
	d, _ := reportRun(t)
	var got ReportReply
	if err := json.Unmarshal(request(t, d, MethodRunReport, `{"run":"r1","name":"gate-1.md"}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "gate-1.md" || got.Text != "REPORT BODY" || MethodRunReport != "run.report" {
		t.Fatalf("reply %+v", got)
	}
}

func TestRunReportRefusesWhatTheRecordDoesNotList(t *testing.T) {
	d, _ := reportRun(t)
	for _, tc := range []RunAction{{Run: "r1", Name: "findings-1.md"}, {Run: "r1", Name: "run.json"}, {Run: "unknown", Name: "gate-1.md"}, {Run: "r1"}, {Run: "../r1", Name: "gate-1.md"}} {
		_, err := d.handle(context.Background(), MethodRunReport, renderParams(tc.Run, tc.Name))
		var wire *ipc.Error
		if !errors.As(err, &wire) || wire.Code != CodeNotFound {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}

func TestRunRowsCarryReportNamesNotText(t *testing.T) {
	d, _ := reportRun(t)
	rows := d.engineRuns("p")
	if len(rows) != 1 || len(rows[0].Roles) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	wire, _ := json.Marshal(rows[0].Roles)
	if strings.Contains(string(wire), "REPORT BODY") || strings.Count(string(wire), `"report":`) != 1 || !strings.Contains(string(wire), `"report":"gate-1.md"`) {
		t.Fatalf("wire %s", wire)
	}
}

// checkLogRun is a run whose checks[] list checks-1.log; checks-9.log is on
// disk but not listed.
func checkLogRun(t *testing.T, log string) (*Daemon, *runrec.Store) {
	t.Helper()
	d, store, p := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "r1", Project: p.Name, Item: "S1", State: runrec.StatePaused,
		Roles:  []runrec.RoleRecord{{Role: "gate", Round: 1, Report: "gate-1.md"}},
		Checks: []runrec.CheckRecord{{Round: 1, Command: "go test", Log: "checks-1.log"}, {Round: 1, Command: "go test", Log: "land-checks-1.log"}}})
	if err != nil {
		t.Fatal(err)
	}
	dir := store.RunDir("r1")
	for name, text := range map[string]string{"gate-1.md": "g", "checks-1.log": log, "checks-9.log": "unlisted", "land-checks-1.log": "LAND CHECK LOG", "land-checks-9.log": "unlisted"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "secret.txt"), filepath.Join(dir, "checks-link.log")); err != nil {
		t.Fatal(err)
	}
	return d, store
}

func TestRunReportServesAListedCheckLog(t *testing.T) {
	d, store := checkLogRun(t, "\x1b[31mFAIL\x1b[0m TestX\n")
	var got ReportReply
	if err := json.Unmarshal(request(t, d, MethodRunReport, `{"run":"r1","name":"checks-1.log"}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "checks-1.log" || got.Text != "FAIL TestX" {
		t.Fatalf("reply %+v", got)
	}
	// A link is refused even when the record lists it.
	r, err := store.Open("r1")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Update(func(rr *runrec.Record) {
		rr.Checks = append(rr.Checks, runrec.CheckRecord{Round: 2, Log: "checks-link.log"}, runrec.CheckRecord{Round: 3, Log: "../secret.txt"}, runrec.CheckRecord{Round: 4, Log: "run.json"})
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"checks-9.log", "run.json", "../r1/checks-1.log", filepath.Join(store.RunDir("r1"), "checks-1.log"), "checks-link.log", "../secret.txt", "secret.txt", "checks-1.txt", ""} {
		_, err := d.handle(context.Background(), MethodRunReport, renderParams("r1", name))
		var wire *ipc.Error
		if !errors.As(err, &wire) || wire.Code != CodeNotFound {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestRunReportServesAListedLandCheckLog(t *testing.T) {
	d, _ := checkLogRun(t, "x")
	var got ReportReply
	if err := json.Unmarshal(request(t, d, MethodRunReport, `{"run":"r1","name":"land-checks-1.log"}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "land-checks-1.log" || got.Text != "LAND CHECK LOG" {
		t.Fatalf("reply %+v", got)
	}
	_, err := d.handle(context.Background(), MethodRunReport, renderParams("r1", "land-checks-9.log"))
	var wire *ipc.Error
	if !errors.As(err, &wire) || wire.Code != CodeNotFound {
		t.Fatalf("an unlisted land log: %v", err)
	}
}

func TestRunReportCutsALongLogAtALineEnd(t *testing.T) {
	d, _ := checkLogRun(t, strings.Repeat("line\n", (64<<10)/5)+strings.Repeat("X", 20))
	var got ReportReply
	if err := json.Unmarshal(request(t, d, MethodRunReport, `{"run":"r1","name":"checks-1.log"}`), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got.Text, "line\n(cut: over 64 KiB)") || strings.Contains(got.Text, "X") || len(got.Text) > 64<<10+32 {
		t.Fatalf("log of %d bytes, ends %q", len(got.Text), got.Text[max(0, len(got.Text)-40):])
	}
}
