// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func laterRunner() roundRunner {
	return func(_ context.Context, r engine.Request) (engine.Result, error) {
		report := "Tests: 1 passed, 0 failed\n\n## Owner needed\nnone\n"
		switch r.Role {
		case engine.RoleReviewer:
			report = "(A): nothing\n(B): nothing\n(C)\n- [later] Retire the old cache\n- [later] Add a --json flag to ork status\n(D): nothing\n\n## Owner needed\nnone\n"
		case engine.RoleGate:
			report = "VERDICT: PASS\n" + report
		}
		return engine.Result{Exit: engine.ExitOK, Session: "session", Report: report}, nil
	}
}

func TestRunFileFilesALaterFinding(t *testing.T) {
	d, store, p := runDaemon(t, laterRunner(),
		clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"id":"S130","title":"x","existing":false}`},
		clitest.Route{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{"id":"S130"}`},
	)
	b := request(t, d, "run.start", `{"project":"p","item":"S1","well_specified":true,"split":false}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, rec.ID, runrec.StateAwaitingApproval)
	waitIdle(t, d, rec.ID)

	rows := d.engineRuns(p.Name)
	if len(rows) != 1 || len(rows[0].Findings) != 2 {
		t.Fatalf("wire rows %+v", rows)
	}
	if f := rows[0].Findings[1]; f.N != 2 || f.Kind != runrec.FindingLater || f.Title != "Add a --json flag to ork status" || f.Filed != "" {
		t.Errorf("wire finding %+v", f)
	}
	for _, c := range lastFake.CallsTo("slicer") {
		if c.Args[0] == "add" {
			t.Fatal("slicer add ran before the owner confirmed")
		}
	}

	out := request(t, d, "run.file", fmt.Sprintf(`{"run":%q,"finding":2}`, rec.ID))
	if !strings.Contains(string(out), "filed as S130") {
		t.Errorf("reply %s", out)
	}
	var adds [][]string
	for _, c := range lastFake.CallsTo("slicer") {
		if c.Args[0] == "add" {
			adds = append(adds, c.Args)
		}
	}
	if len(adds) != 1 || adds[0][1] != "Add a --json flag to ork status" || adds[0][2] != "--discovered-from" || adds[0][3] != "S1" || adds[0][len(adds[0])-1] != p.Path {
		t.Errorf("slicer add calls %q", adds)
	}
	if rows := d.engineRuns(p.Name); rows[0].Findings[1].Filed != "S130" || rows[0].Findings[0].Filed != "" {
		t.Errorf("wire finding after filing %+v", rows[0].Findings)
	}
	if _, err := d.handle(context.Background(), "run.file", json.RawMessage(fmt.Sprintf(`{"run":%q,"finding":2}`, rec.ID))); err == nil || !strings.Contains(err.Error(), "S130") {
		t.Errorf("filing twice: %v", err)
	}
	if _, err := d.handle(context.Background(), "run.file", json.RawMessage(`{"run":"nope","finding":1}`)); err == nil {
		t.Error("filing for an unknown run")
	}
}

// A stopped or committed run is not rebuilt at a daemon start, but its later
// findings stay fileable (docs/dashboard.md).
func TestRunFileFilesFromAFinishedRunTheDaemonDidNotRebuild(t *testing.T) {
	d, store, p := runDaemon(t, nil,
		clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"id":"S131","existing":false}`},
		clitest.Route{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{"id":"S131"}`},
	)
	for _, state := range []string{runrec.StateStopped, runrec.StateCommitted} {
		id := "old-" + state
		_, err := store.Create(runrec.Record{ID: id, Project: p.Name, Item: "S1", State: state,
			Findings: []runrec.Finding{{Kind: runrec.FindingLater, Role: engine.RoleReviewer, Round: 1, Title: "Retire the old cache", Text: "Retire the old cache"}}})
		if err != nil {
			t.Fatal(err)
		}
		out := request(t, d, "run.file", fmt.Sprintf(`{"run":%q,"finding":1}`, id))
		if !strings.Contains(string(out), "filed as S131") {
			t.Errorf("%s: reply %s", state, out)
		}
		r, err := store.Open(id)
		if err != nil || r.Snapshot().Findings[0].Filed != "S131" {
			t.Errorf("%s: stored finding %+v, %v", state, r.Snapshot().Findings, err)
		}
	}
	// A run that is not finished and has no engine cannot be filed from.
	_, err := store.Create(runrec.Record{ID: "old-live", Project: p.Name, Item: "S1", State: runrec.StatePaused,
		Findings: []runrec.Finding{{Kind: runrec.FindingLater, Role: engine.RoleReviewer, Round: 1, Title: "Retire the old cache", Text: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.handle(context.Background(), "run.file", json.RawMessage(`{"run":"old-live","finding":1}`)); err == nil {
		t.Error("filing from a live run that the daemon did not rebuild")
	}
	if _, err := d.handle(context.Background(), "run.file", json.RawMessage(`{"run":"nope","finding":1}`)); err == nil {
		t.Error("filing for a run the store does not have")
	}
}
