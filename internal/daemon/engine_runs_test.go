// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func TestEngineSnapshotMatchesStatus(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	at := time.Date(2026, 10, 19, 16, 40, 0, 0, time.UTC)
	store.Now = func() time.Time { return at }
	if _, err := store.Create(runrec.Record{ID: "engine", Project: p.Name, Item: "S22", State: runrec.StatePaused, RelayRun: "ledger", CreatedAt: at, Pending: &runrec.Pending{Reason: "reason", Question: "Which library?"}}); err != nil {
		t.Fatal(err)
	}
	status := request(t, d, "status", `{"project":"p"}`)
	snapshot := request(t, d, "snapshot", `{"project":"p"}`)
	var out struct {
		EngineRuns json.RawMessage `json:"engine_runs"`
	}
	json.Unmarshal(snapshot, &out)
	for _, want := range []string{`"relay_run":"ledger"`, `"created_at":"2026-10-19T16:40:00Z"`, `"wait":"Which library?"`} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(string(out.EngineRuns), want) || !strings.Contains(string(status), want) {
				t.Fatalf("shared engine snapshot lacks %s: snapshot=%s status=%s", want, snapshot, status)
			}
		})
	}
}

func TestStoppedNeedsNewerEngineRun(t *testing.T) {
	for _, tc := range []struct {
		name, item, project string
		delta               time.Duration
		want                int
	}{
		{"newer", "S22", "p", time.Second, 0}, {"older", "S22", "p", -time.Second, 1}, {"equal", "S22", "p", 0, 1}, {"other-slice", "S23", "p", time.Second, 1}, {"other-project", "S22", "other", time.Second, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, store, p := runDaemon(t, nil)
			at := time.Date(2026, 10, 19, 16, 40, 0, 0, time.UTC)
			for _, rec := range []runrec.Record{{ID: "stopped", Project: p.Name, Item: "S22", State: runrec.StateStopped, CreatedAt: at}, {ID: "second", Project: tc.project, Item: tc.item, State: runrec.StateImplement, CreatedAt: at.Add(tc.delta)}} {
				store.Now = func() time.Time { return rec.CreatedAt }
				if _, err := store.Create(rec); err != nil {
					t.Fatal(err)
				}
			}
			b := request(t, d, "status", `{"project":"p"}`)
			var out StatusResult
			json.Unmarshal(b, &out)
			if out.Projects[0].NeedsYou != tc.want {
				t.Fatalf("needs_you=%d want %d: %s", out.Projects[0].NeedsYou, tc.want, b)
			}
		})
	}
}

func TestStatusCountsHeldItems(t *testing.T) {
	s := source.Snapshot{Items: source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S22", Fields: slicercli.Fields{Flags: []string{"needs-owner"}}}}}}
	ps := projectStatus(registry.Project{Name: "p"}, s, time.Now())
	b, _ := json.Marshal(ps)
	if !strings.Contains(string(b), `"needs_you":1`) {
		t.Fatalf("held item absent from needs_you: %s", b)
	}
}
