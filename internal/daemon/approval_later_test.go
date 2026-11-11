// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// laterRun starts a run with two open human checks and waits for its card.
func laterRun(t *testing.T) (*Daemon, *runrec.Store, string, string) {
	t.Helper()
	d, store, p := runDaemon(t, passRunner(),
		clitest.Route{Tool: "slicer", Args: []string{"show"}, Prefix: true, Stdout: twoHumanChecks},
		clitest.Route{Tool: "slicer", Args: []string{"note"}, Prefix: true, Stdout: `{"id":"S1","added":true}`},
	)
	if err := os.MkdirAll(p.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	b := request(t, d, "run.start", `{"project":"p","item":"S1","well_specified":true,"split":false}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, rec.ID, runrec.StateAwaitingApproval)
	waitIdle(t, d, rec.ID)
	return d, store, rec.ID, p.Name
}

func wantDeferred(t *testing.T, reply []byte) {
	t.Helper()
	var got RunReply
	if err := json.Unmarshal(reply, &got); err != nil {
		t.Fatal(err)
	}
	want := []string{"1. Read the card at 80x24.", "2. Press F and send a note."}
	if !reflect.DeepEqual(got.Deferred, want) {
		t.Errorf("deferred %q, want %q (reply %s)", got.Deferred, want, reply)
	}
}

func TestApproveWithLaterChecksDefersTheOpenChecks(t *testing.T) {
	d, store, id, _ := laterRun(t)
	// The plain approval still stops on the open checks and names the way out.
	_, err := d.handle(context.Background(), "run.approve", json.RawMessage(fmt.Sprintf(`{"run":%q}`, id)))
	if err == nil || !strings.Contains(err.Error(), "2 human checks") || !strings.Contains(err.Error(), "--later-checks") {
		t.Fatalf("approve with open checks: %v", err)
	}
	if wire := ipc.ErrorOf(err); wire.Code != CodeRefused {
		t.Errorf("wire code %q", wire.Code)
	}
	reply := request(t, d, "run.approve", fmt.Sprintf(`{"run":%q,"later_checks":true}`, id))
	wantDeferred(t, reply)
	waitState(t, store, id, runrec.StateCommitted)
	waitIdle(t, d, id)
}

func TestCheckAllLaterDefersTheOpenChecksAndDoesNotApprove(t *testing.T) {
	d, store, id, project := laterRun(t)
	request(t, d, "run.check", fmt.Sprintf(`{"run":%q,"check":1,"state":"ticked"}`, id))
	reply := request(t, d, "run.check", fmt.Sprintf(`{"run":%q,"all":true,"state":"later"}`, id))
	var got RunReply
	if err := json.Unmarshal(reply, &got); err != nil {
		t.Fatal(err)
	}
	if want := []string{"2. Press F and send a note."}; !reflect.DeepEqual(got.Deferred, want) {
		t.Errorf("deferred %q, want %q", got.Deferred, want)
	}
	if got.State != runrec.StateAwaitingApproval {
		t.Fatalf("state %q: marking approved the run", got.State)
	}
	marks := d.engineRuns(project)[0].Card.HumanChecks
	if marks[0].State != runrec.CheckTicked || marks[1].State != runrec.CheckLater {
		t.Errorf("marks %+v", marks)
	}
	rec, err := store.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Snapshot().State != runrec.StateAwaitingApproval {
		t.Errorf("stored state %q", rec.Snapshot().State)
	}
}

// All is for later only: ticking says the owner looked (adj 234).
func TestCheckAllIsRefusedForAnythingButLater(t *testing.T) {
	d, _, id, project := laterRun(t)
	for _, state := range []string{`"ticked"`, `""`} {
		call := fmt.Sprintf(`{"run":%q,"all":true,"state":%s}`, id, state)
		_, err := d.handle(context.Background(), "run.check", json.RawMessage(call))
		if err == nil || ipc.ErrorOf(err).Code != CodeRefused {
			t.Errorf("run.check %s: %v", call, err)
		}
	}
	for _, c := range d.engineRuns(project)[0].Card.HumanChecks {
		if c.State != "" {
			t.Errorf("a refused call changed the card: %+v", c)
		}
	}
}
