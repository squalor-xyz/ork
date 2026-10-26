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

func passRunner() roundRunner {
	return func(_ context.Context, r engine.Request) (engine.Result, error) {
		report := "Tests: 1 passed, 0 failed\n\n## Owner needed\nnone\n"
		switch r.Role {
		case engine.RoleReviewer:
			report = "(A): nothing\n(B): nothing\n(C): nothing\n(D): nothing\n\n## Owner needed\nnone\n"
		case engine.RoleGate:
			report = "VERDICT: PASS\n" + report
		}
		return engine.Result{Exit: engine.ExitOK, Session: "session", Report: report}, nil
	}
}

const twoHumanChecks = `{"slice":{"sections":[{"heading":"Check","body":"- go test ./...\n- [HUMAN CHECK] Read the card at 80x24.\n- [HUMAN CHECK] Press F and send a note."},{"heading":"Git","body":"One commit: \"S1: test\"."}]}}`

func TestRunStatusCarriesTheApprovalCardAndItsActions(t *testing.T) {
	d, store, p := runDaemon(t, passRunner(),
		clitest.Route{Tool: "slicer", Args: []string{"show"}, Prefix: true, Stdout: twoHumanChecks},
		clitest.Route{Tool: "slicer", Args: []string{"note"}, Prefix: true, Stdout: `{"id":"S1","added":true}`},
	)
	b := request(t, d, "run.start", `{"project":"p","item":"S1","well_specified":true,"split":false}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, rec.ID, runrec.StateAwaitingApproval)
	waitIdle(t, d, rec.ID)

	rows := d.engineRuns(p.Name)
	if len(rows) != 1 || rows[0].Card == nil {
		t.Fatalf("wire rows %+v", rows)
	}
	card := rows[0].Card
	if card.Verdict != "PASS" || card.Passed != 1 || card.Failed != 0 || len(card.HumanChecks) != 2 || card.HumanChecks[1].Text != "Press F and send a note." {
		t.Errorf("wire card %+v", card)
	}
	wire, _ := json.Marshal(rows[0])
	if !strings.Contains(string(wire), `"card":{`) || !strings.Contains(string(wire), `"human_checks"`) {
		t.Errorf("the card is not on the wire: %s", wire)
	}

	// Approval waits for the marks.
	if _, err := d.handle(context.Background(), "run.approve", json.RawMessage(fmt.Sprintf(`{"run":%q}`, rec.ID))); err == nil || !strings.Contains(err.Error(), "2 human checks") {
		t.Fatalf("approve with open checks: %v", err)
	}
	request(t, d, "run.check", fmt.Sprintf(`{"run":%q,"check":1,"state":"ticked"}`, rec.ID))
	request(t, d, "run.check", fmt.Sprintf(`{"run":%q,"check":2,"state":"later"}`, rec.ID))
	got := d.engineRuns(p.Name)[0].Card.HumanChecks
	if got[0].State != runrec.CheckTicked || got[1].State != runrec.CheckLater {
		t.Errorf("marks on the wire: %+v", got)
	}
	if _, err := d.handle(context.Background(), "run.check", json.RawMessage(fmt.Sprintf(`{"run":%q,"check":3,"state":"ticked"}`, rec.ID))); err == nil {
		t.Error("a mark on a check the slice does not have")
	}

	// A fix goes to a fix round and the run comes back to the card.
	out := request(t, d, "run.fix", fmt.Sprintf(`{"run":%q,"text":"Rename the flag."}`, rec.ID))
	if !strings.Contains(string(out), `"state":"awaiting-approval"`) {
		t.Errorf("fix reply %s", out)
	}
	after, _ := store.Open(rec.ID)
	if fixes := after.Snapshot().Fixes; len(fixes) != 1 || fixes[0].Note != "Rename the flag." {
		t.Errorf("stored fixes %+v", fixes)
	}
	if got := d.engineRuns(p.Name)[0].Card; got == nil || got.Round != 2 || got.HumanChecks[0].State != runrec.CheckTicked {
		t.Errorf("card after the fix %+v", got)
	}
	if _, err := d.handle(context.Background(), "run.fix", json.RawMessage(fmt.Sprintf(`{"run":%q,"text":"  "}`, rec.ID))); err == nil {
		t.Error("an empty fix note")
	}

	request(t, d, "run.approve", fmt.Sprintf(`{"run":%q}`, rec.ID))
	waitState(t, store, rec.ID, runrec.StateCommitted)
	waitIdle(t, d, rec.ID)
	if rows := d.engineRuns(p.Name); rows[0].Card != nil {
		t.Errorf("a committed run keeps no card on the wire: %+v", rows[0].Card)
	}
}
