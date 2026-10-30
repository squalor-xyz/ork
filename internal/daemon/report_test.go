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
