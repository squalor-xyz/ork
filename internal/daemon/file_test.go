// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
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

func TestRunFileSerialized(t *testing.T) {
	d, store, p := runDaemon(t, nil,
		clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"id":"S131","existing":false}`},
		clitest.Route{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{"id":"S131"}`})
	_, err := store.Create(runrec.Record{ID: "finished", Project: p.Name, Item: "S1", State: runrec.StateCommitted,
		Findings: []runrec.Finding{{Kind: runrec.FindingLater, Title: "First", Text: "First"}, {Kind: runrec.FindingLater, Title: "Second", Text: "Second"}}})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for n := 1; n <= 2; n++ {
		go func(n int) {
			<-start
			_, err := d.fileFinding(context.Background(), RunAction{Run: "finished", Finding: n})
			results <- err
		}(n)
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	r, err := store.Open("finished")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Snapshot().Findings {
		if f.Filed == "" {
			t.Fatalf("concurrent filing lost finding: %+v", r.Snapshot().Findings)
		}
	}
}

func TestNeedsYouCountsFindings(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "findings", Project: p.Name, Item: "S1", State: runrec.StateCommitted,
		Findings: []runrec.Finding{{Kind: runrec.FindingLater, Title: "Later"}, {Kind: runrec.FindingOwner, Title: "Owner"}, {Kind: runrec.FindingLater, Title: "Filed", Filed: "S2"}}})
	if err != nil {
		t.Fatal(err)
	}
	rows := d.engineRuns(p.Name)
	ps := projectStatus(p, source.Snapshot{EngineRuns: rows}, time.Now())
	if ps.NeedsYou != 2 {
		t.Fatalf("needs you %d, want 2 findings", ps.NeedsYou)
	}
}

func TestNonBlockingOwnerFindingAnsweredWithoutPausing(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "owner-finding", Project: p.Name, Item: "S1", State: runrec.StateCommitted,
		Findings: []runrec.Finding{{Kind: runrec.FindingOwner, Title: "Which?", Ask: &runrec.OwnerQuestion{Title: "Which?", Options: []runrec.Option{{Label: "one", Summary: "first"}, {Label: "two", Summary: "second"}}, Recommend: 1, Reason: "simple"}}}})
	if err != nil {
		t.Fatal(err)
	}
	b := request(t, d, "run.answer", `{"run":"owner-finding","finding":1,"text":"one: first"}`)
	if !strings.Contains(string(b), "one: first") {
		t.Fatalf("answer missing: %s", b)
	}
	r, err := store.Open("owner-finding")
	if err != nil {
		t.Fatal(err)
	}
	if r.Snapshot().State != runrec.StateCommitted || r.Snapshot().Pending != nil {
		t.Fatal("answer changed run state")
	}
}

func TestNonBlockingFindingKeyFilesAdjBeforeRecordingAnswer(t *testing.T) {
	d, store, p := runDaemon(t, nil,
		clitest.Route{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: `{"result":"not found"}`},
		clitest.Route{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "42\n"},
		clitest.Route{Tool: "adj", Args: []string{"decide"}, Prefix: true, Seq: []clitest.Reply{{Exit: 3, Stderr: "disk full"}, {Stdout: "42\n"}}})
	d.runs.deps.Adj = adjcli.Client{Bin: lastFake.Bin("adj")}
	p.Adj = true
	if err := registry.Save(d.registryPath(), &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	_, err := store.Create(runrec.Record{ID: "keyed", Project: p.Name, Item: "S1", State: runrec.StateCommitted, Findings: []runrec.Finding{{
		Kind: runrec.FindingOwner, Title: "Which?", Key: "cache.mode", Ask: &runrec.OwnerQuestion{Title: "Which?", Options: []runrec.Option{{Label: "one", Summary: "first"}, {Label: "two", Summary: "second"}}, Recommend: 1, Reason: "simple"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, by := range []string{"", "owner", "owner"} {
		b, _ := json.Marshal(RunAction{Run: "keyed", Finding: 1, Text: "one: first", By: by})
		_, err = d.handle(context.Background(), MethodRunAnswer, b)
		if by == "" && err == nil {
			t.Fatal("keyed answer accepted without owner identity")
		}
		r, _ := store.Open("keyed")
		if err != nil && r.Snapshot().Findings[0].Answer != "" {
			t.Fatal("failed adj write recorded answer")
		}
	}
	r, _ := store.Open("keyed")
	f := r.Snapshot().Findings[0]
	if err != nil || f.Answer != "one: first" || f.AnsweredAt.IsZero() {
		t.Fatalf("finding %+v, err %v", f, err)
	}
	calls := lastFake.CallsTo("adj")
	if len(calls) != 6 || calls[0].Args[0] != "check" || calls[1].Args[0] != "ask" || calls[2].Args[0] != "decide" {
		t.Fatalf("adj calls %+v", calls)
	}
	if !strings.Contains(strings.Join(calls[1].Args, " "), "Question: Which?") {
		t.Fatal("adj question lacks structured context")
	}
}

func TestRunFilePreservesFinishedRunMetadata(t *testing.T) {
	for _, field := range []string{"dismissed", "cleaned"} {
		t.Run(field, func(t *testing.T) {
			d, store, p := runDaemon(t, nil,
				clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"id":"S131"}`},
				clitest.Route{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{"id":"S131"}`})
			_, err := store.Create(runrec.Record{ID: "old", Project: p.Name, Item: "S1", State: runrec.StateStopped,
				Findings: []runrec.Finding{{Kind: runrec.FindingLater, Title: "First"}, {Kind: runrec.FindingLater, Title: "Second"}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.fileFinding(context.Background(), RunAction{Run: "old", Finding: 1}); err != nil {
				t.Fatal(err)
			}
			if field == "dismissed" {
				request(t, d, MethodRunDismiss, `{"run":"old"}`)
			} else if err := d.markCleaned("old"); err != nil {
				t.Fatal(err)
			}
			before, err := store.Open("old")
			if err != nil {
				t.Fatal(err)
			}
			saved := before.Snapshot()
			if _, err := d.fileFinding(context.Background(), RunAction{Run: "old", Finding: 2}); err != nil {
				t.Fatal(err)
			}
			after, err := store.Open("old")
			if err != nil {
				t.Fatal(err)
			}
			got := after.Snapshot()
			if field == "dismissed" && (saved.DismissedAt.IsZero() || got.DismissedAt != saved.DismissedAt) {
				t.Fatal("filing erased dismissed_at")
			}
			if field == "cleaned" && (saved.CleanedAt.IsZero() || got.CleanedAt != saved.CleanedAt) {
				t.Fatal("filing erased cleaned_at")
			}
		})
	}
}

func TestNonBlockingFindingKeyWithoutAdjRecordsAnswerOnly(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	p.Adj = false
	// Load directly so the project setting is explicit, independent of registry fixtures.
	run, err := store.Create(runrec.Record{ID: "no-adj", Project: p.Name, Item: "S1", State: runrec.StateStopped, Findings: []runrec.Finding{{
		Kind: runrec.FindingOwner, Title: "Which?", Key: "cache.mode", Ask: &runrec.OwnerQuestion{Title: "Which?"}}}})
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.Load(d.runDeps(p), p, run)
	if _, err := eng.AnswerFinding(context.Background(), 1, "one", ""); err != nil {
		t.Fatal(err)
	}
	if eng.Snapshot().Findings[0].Answer != "one" {
		t.Fatal("answer not recorded")
	}
	if calls := lastFake.CallsTo("adj"); len(calls) != 0 {
		t.Fatalf("no-adj project called adj: %+v", calls)
	}
}

func TestStatusSettledFindingsStopCounting(t *testing.T) {
	for _, mode := range []string{"landed", "dismissed", "superseded"} {
		t.Run(mode, func(t *testing.T) {
			d, store, p := runDaemon(t, nil)
			rec := runrec.Record{ID: "settled", Project: p.Name, Item: "S1", State: runrec.StateStopped,
				Findings: []runrec.Finding{{Kind: runrec.FindingLater, Title: "Later"}, {Kind: runrec.FindingOwner, Title: "Owner"}}}
			at := time.Now().UTC()
			if mode == "landed" {
				rec.State = runrec.StateCommitted
				rec.LandedAt = at
			}
			if mode == "dismissed" {
				rec.DismissedAt = at
			}
			old, err := store.Create(rec)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "superseded" {
				_, err = store.Create(runrec.Record{ID: "new", Project: p.Name, Item: "S1", State: runrec.StateImplement, CreatedAt: old.Snapshot().CreatedAt.Add(time.Second)})
				if err != nil {
					t.Fatal(err)
				}
			}
			rows := d.engineRuns(p.Name)
			if got := projectStatus(p, source.Snapshot{EngineRuns: rows}, at).NeedsYou; got != 0 {
				t.Fatalf("settled run counts %d needs you", got)
			}
			if len(rows) == 0 || len(rows[0].Findings) != 2 {
				t.Fatal("finding data lost from pane snapshot")
			}
		})
	}
}
