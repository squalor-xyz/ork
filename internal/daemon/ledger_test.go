// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// ledgerList routes `slicer relay-run list` to one ledger run in the state.
func ledgerList(state string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"relay-run", "list"}, Prefix: true,
		Stdout: `{"runs":[{"id":"run-old","state":"` + state + `","items":["S1"]}]}`}
}

func endCalls() [][]string {
	var out [][]string
	for _, c := range lastFake.CallsTo("slicer") {
		if len(c.Args) >= 2 && c.Args[0] == "relay-run" && c.Args[1] == "end" {
			out = append(out, c.Args)
		}
	}
	return out
}

func endLedger(d *Daemon, project, run string) (any, error) {
	return d.handle(context.Background(), MethodRunEndLedger, json.RawMessage(`{"project":"`+project+`","run":"`+run+`"}`))
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want code %q", code)
	}
	if got := ipc.ErrorOf(err).Code; got != code {
		t.Fatalf("error %v has code %q, want %q", err, got, code)
	}
}

func TestRunEndLedgerEndsAStaleRun(t *testing.T) {
	d, _, p := runDaemon(t, nil, ledgerList("stale"))
	v, err := endLedger(d, p.Name, "run-old")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	if string(b) != `{"run":"run-old"}` {
		t.Errorf("reply %s, want the run id", b)
	}
	want := []string{"relay-run", "end", "run-old", "--outcome", "stopped", "--close-steps", "fail", "--json", "--root", p.Path}
	if calls := endCalls(); len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("relay-run end calls %v, want once %v", calls, want)
	}
}

func TestRunEndLedgerRefusesAnActiveRun(t *testing.T) {
	d, _, p := runDaemon(t, nil, ledgerList("active"))
	_, err := endLedger(d, p.Name, "run-old")
	wantCode(t, err, CodeRefused)
	if !strings.Contains(err.Error(), "active") {
		t.Errorf("error %q does not name the state", err)
	}
	if calls := endCalls(); len(calls) != 0 {
		t.Fatalf("relay-run end called: %v", calls)
	}
}

func TestRunEndLedgerRefusesAnEndedRun(t *testing.T) {
	d, _, p := runDaemon(t, nil, ledgerList("ended"))
	_, err := endLedger(d, p.Name, "run-old")
	wantCode(t, err, CodeRefused)
	if !strings.Contains(err.Error(), "ended") {
		t.Errorf("error %q does not name the state", err)
	}
	if calls := endCalls(); len(calls) != 0 {
		t.Fatalf("relay-run end called: %v", calls)
	}
}

func TestRunEndLedgerRefusesTheLedgerRunOfAnUnfinishedEngineRun(t *testing.T) {
	d, store, p := runDaemon(t, nil, ledgerList("stale"))
	if _, err := store.Create(runrec.Record{ID: "eng-1", Project: p.Name, State: runrec.StateImplement, RelayRun: "run-old"}); err != nil {
		t.Fatal(err)
	}
	_, err := endLedger(d, p.Name, "run-old")
	wantCode(t, err, CodeRefused)
	if !strings.Contains(err.Error(), "eng-1") {
		t.Errorf("error %q does not name the engine run", err)
	}
	if calls := endCalls(); len(calls) != 0 {
		t.Fatalf("relay-run end called: %v", calls)
	}
}

func TestRunEndLedgerAllowsTheLedgerRunOfAStoppedEngineRun(t *testing.T) {
	d, store, p := runDaemon(t, nil, ledgerList("stale"))
	if _, err := store.Create(runrec.Record{ID: "eng-1", Project: p.Name, State: runrec.StateStopped, RelayRun: "run-old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := endLedger(d, p.Name, "run-old"); err != nil {
		t.Fatal(err)
	}
	if calls := endCalls(); len(calls) != 1 {
		t.Fatalf("relay-run end calls %v, want one", calls)
	}
}

func TestRunEndLedgerUnknownRunIsNotFound(t *testing.T) {
	d, _, p := runDaemon(t, nil, ledgerList("stale"))
	_, err := endLedger(d, p.Name, "run-none")
	wantCode(t, err, CodeNotFound)
	_, err = endLedger(d, "nope", "run-old")
	wantCode(t, err, CodeNotFound)
	if calls := endCalls(); len(calls) != 0 {
		t.Fatalf("relay-run end called: %v", calls)
	}
}

func TestRunEndLedgerReturnsSlicersFailure(t *testing.T) {
	d, _, p := runDaemon(t, nil, ledgerList("stale"),
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "end"}, Prefix: true, Stderr: "boom: ledger locked", Exit: 1})
	_, err := endLedger(d, p.Name, "run-old")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error %v, want slicer's failure", err)
	}
	for _, c := range lastFake.CallsTo("slicer") {
		if len(c.Args) >= 2 && c.Args[0] == "relay-run" && c.Args[1] == "end" {
			continue
		}
		if len(c.Args) >= 2 && c.Args[0] == "relay-run" && c.Args[1] == "list" {
			continue
		}
		t.Errorf("unexpected slicer call %v", c.Args)
	}
}
