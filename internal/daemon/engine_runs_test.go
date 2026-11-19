// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func TestEngineSnapshotMatchesStatus(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	at := time.Date(2026, 10, 19, 16, 40, 0, 0, time.UTC)
	store.Now = func() time.Time { return at }
	r, err := store.Create(runrec.Record{ID: "engine", Project: p.Name, Item: "S22", Branch: "s22", State: runrec.StatePaused, RelayRun: "ledger", CreatedAt: at, Roles: []runrec.RoleRecord{{Denials: []string{"Write"}}, {Denials: []string{"Bash"}}}, Pending: &runrec.Pending{Kind: runrec.PendingQuestion, Reason: "reason", Question: "Which library?"}})
	if err != nil {
		t.Fatal(err)
	}
	store.Now = func() time.Time { return at.Add(time.Hour) }
	if err := r.Update(func(*runrec.Record) {}); err != nil {
		t.Fatal(err)
	}
	status := request(t, d, "status", `{"project":"p"}`)
	snapshot := request(t, d, "snapshot", `{"project":"p"}`)
	var out struct {
		EngineRuns json.RawMessage `json:"engine_runs"`
	}
	json.Unmarshal(snapshot, &out)
	for _, want := range []string{`"wait_started_at":"2026-10-19T16:40:00Z"`, `"updated_at":"2026-10-19T17:40:00Z"`, `"branch":"s22"`, `"denials":["Write","Bash"]`, `"relay_run":"ledger"`, `"created_at":"2026-10-19T16:40:00Z"`, `"wait":"Which library?; next: ork answer engine TEXT"`} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(string(out.EngineRuns), want) || !strings.Contains(string(status), want) {
				t.Fatalf("shared engine snapshot lacks %s: snapshot=%s status=%s", want, snapshot, status)
			}
		})
	}
}

func TestEngineSnapshotAndStatusShowEachDenialsCommand(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return at }
	roles := []runrec.RoleRecord{{
		Role: "implementer", Round: 1, Vendor: "claude",
		Denials:        []string{"Bash", "Write", "Glob"},
		DeniedCommands: []runrec.DeniedCommand{{Tool: "Bash", Command: "git push origin main"}, {Tool: "Write", Command: "/x.txt"}, {Tool: "Glob"}},
	}, {
		Role: "reviewer", Round: 1, Vendor: "claude",
		DeniedCommands: []runrec.DeniedCommand{{Tool: "Write", Command: "/x.txt"}, {Tool: "Write", Command: "/y.txt"}},
	}}
	if _, err := store.Create(runrec.Record{ID: "denied", Project: p.Name, Item: "S45", State: runrec.StatePaused, CreatedAt: at, Roles: roles, Pending: &runrec.Pending{Kind: runrec.PendingError, State: "implement", Role: "implementer", Reason: "reason"}}); err != nil {
		t.Fatal(err)
	}
	want := `"denials":["Bash: git push origin main","Write: /x.txt x2","Glob","Write: /y.txt"]`
	status := request(t, d, "status", `{"project":"p"}`)
	snapshot := request(t, d, "snapshot", `{"project":"p"}`)
	if !strings.Contains(string(snapshot), want) || !strings.Contains(string(status), want) {
		t.Fatalf("denials lack the command text: want %s\nsnapshot=%s\nstatus=%s", want, snapshot, status)
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
			for _, rec := range []runrec.Record{{ID: "stopped", Project: p.Name, Item: "S22", State: runrec.StateStopped, CreatedAt: at}, {ID: "second", Project: tc.project, Item: tc.item, State: runrec.StateImplement, CreatedAt: at.Add(tc.delta)},
				{ID: "paused", Project: p.Name, Item: "S90", State: runrec.StatePaused, CreatedAt: at},
				{ID: "approval", Project: p.Name, Item: "S91", State: runrec.StateAwaitingApproval, CreatedAt: at},
				{ID: "committed", Project: p.Name, Item: "S92", State: runrec.StateCommitted, CreatedAt: at}} {
				store.Now = func() time.Time { return rec.CreatedAt }
				if _, err := store.Create(rec); err != nil {
					t.Fatal(err)
				}
			}
			b := request(t, d, "status", `{"project":"p"}`)
			var out StatusResult
			json.Unmarshal(b, &out)
			wantRuns := 5
			if tc.project != p.Name {
				wantRuns--
			}
			if len(out.Projects[0].EngineRuns) != wantRuns {
				t.Fatalf("status lost engine runs: %s", b)
			}
			if out.Projects[0].NeedsYou != tc.want+2 {
				t.Fatalf("needs_you=%d want %d: %s", out.Projects[0].NeedsYou, tc.want+2, b)
			}
		})
	}
}

// A run whose resume waits for a slot leaves needs_you (S274, adj 177).
func TestStatusDoesNotCountAWaitingResumeAsNeedsYou(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	for _, rec := range []runrec.Record{
		{ID: "asks", Project: p.Name, Item: "S1", State: runrec.StatePaused, Pending: &runrec.Pending{Kind: runrec.PendingQuestion, State: runrec.StateImplement, Question: "which?"}},
		{ID: "waits", Project: p.Name, Item: "S2", State: runrec.StatePaused, Pending: &runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement},
			Resume: &runrec.ResumeRequest{Method: runrec.ResumeRetry, At: time.Now()}},
	} {
		if _, err := store.Create(rec); err != nil {
			t.Fatal(err)
		}
	}
	var out StatusResult
	if err := json.Unmarshal(request(t, d, "status", `{"project":"p"}`), &out); err != nil {
		t.Fatal(err)
	}
	if got := out.Projects[0].NeedsYou; got != 1 {
		t.Fatalf("needs_you=%d, want 1 (only the question)", got)
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

// statusSpecEntry is a spec cache entry made against the spec the fake slicer
// shows for every item (S340).
func statusSpecEntry(t *testing.T, e specEntry) specEntry {
	t.Helper()
	fp, err := engine.SpecFingerprint([]byte(`{"title":"Title","slice":{"sections":[{"heading":"Git","body":"One commit: \"S1: test\"."}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if e.fingerprint == "" {
		e.fingerprint = fp
	}
	return e
}

func statusOf(t *testing.T, d *Daemon) ProjectStatus {
	t.Helper()
	var out StatusResult
	if err := json.Unmarshal(request(t, d, "status", `{"project":"p"}`), &out); err != nil {
		t.Fatal(err)
	}
	return out.Projects[0]
}

// Open spec questions are needs-you, listed by item (S340, adj 475).
func TestStatusCountsOpenSpecQuestions(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	before := statusOf(t, d).NeedsYou
	two := []engine.SpecQuestion{{Key: "a.b", Text: "one?"}, {Key: "c.d", Text: "two?"}}
	d.runs.mu.Lock()
	d.runs.specs[specKey(p.Name, "S1")] = statusSpecEntry(t, specEntry{questions: two})
	d.runs.specs[specKey(p.Name, "S2")] = statusSpecEntry(t, specEntry{ready: true})
	d.runs.specs[specKey(p.Name, "S3")] = statusSpecEntry(t, specEntry{fingerprint: "an older spec", questions: two})
	d.runs.specs[specKey(p.Name, "S4")] = statusSpecEntry(t, specEntry{})
	d.runs.specs[specKey("other", "S5")] = statusSpecEntry(t, specEntry{questions: two})
	d.runs.mu.Unlock()
	got := statusOf(t, d)
	if got.NeedsYou != before+2 {
		t.Fatalf("needs_you=%d, want %d", got.NeedsYou, before+2)
	}
	want := []SpecQuestions{{Item: "S1", Keys: []string{"a.b", "c.d"}, List: "ork wave start p S1 --questions"}}
	if !reflect.DeepEqual(got.SpecQuestions, want) {
		t.Fatalf("spec_questions=%+v, want %+v", got.SpecQuestions, want)
	}
	if b := request(t, d, "status", `{"project":"p"}`); !strings.Contains(string(b), `"spec_questions":[{"item":"S1","keys":["a.b","c.d"],"list":"ork wave start p S1 --questions"}]`) {
		t.Fatalf("status JSON lacks spec_questions: %s", b)
	}
}

// A question with no key is addressed by its place, as ork wave start does.
func TestStatusSpecQuestionsNumberKeylessQuestions(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	d.runs.mu.Lock()
	d.runs.specs[specKey(p.Name, "S1")] = statusSpecEntry(t, specEntry{questions: []engine.SpecQuestion{{Text: "one?"}, {Key: "c.d", Text: "two?"}}})
	d.runs.mu.Unlock()
	got := statusOf(t, d).SpecQuestions
	if len(got) != 1 || !reflect.DeepEqual(got[0].Keys, []string{"#1", "c.d"}) {
		t.Fatalf("spec_questions=%+v, want keys #1 and c.d", got)
	}
}

// An item held on an open adj record counts through that record (S340).
func TestStatusSpecQuestionsWaitingOnAdjAreNotCounted(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	before := statusOf(t, d).NeedsYou
	d.runs.mu.Lock()
	d.runs.specs[specKey(p.Name, "S1")] = statusSpecEntry(t, specEntry{waitsOn: []int{7}, questions: []engine.SpecQuestion{{Key: "a.b", Text: "one?"}}})
	// The control: an item that waits on nothing is counted, so the test cannot
	// pass for want of the field.
	d.runs.specs[specKey(p.Name, "S2")] = statusSpecEntry(t, specEntry{questions: []engine.SpecQuestion{{Key: "e.f", Text: "two?"}}})
	d.runs.mu.Unlock()
	got := statusOf(t, d)
	if got.NeedsYou != before+1 || len(got.SpecQuestions) != 1 || got.SpecQuestions[0].Item != "S2" {
		t.Fatalf("needs_you=%d (was %d), spec_questions=%+v; want only S2: an item that waits on adj is not counted twice", got.NeedsYou, before, got.SpecQuestions)
	}
}

func TestEnginePendingKind(t *testing.T) {
	for _, state := range []string{runrec.StatePaused, runrec.StateImplement} {
		for _, kind := range []string{runrec.PendingQuestion, runrec.PendingShape, runrec.PendingLost, runrec.PendingInterrupted, runrec.PendingError} {
			t.Run(state+"/"+kind, func(t *testing.T) {
				d, store, p := runDaemon(t, nil)
				if _, err := store.Create(runrec.Record{ID: "pending", Project: p.Name, Item: "S25", State: state, Pending: &runrec.Pending{Kind: kind, Reason: "reason", Question: "Question?"}}); err != nil {
					t.Fatal(err)
				}
				for _, method := range []string{"snapshot", "status"} {
					b := request(t, d, method, `{"project":"p"}`)
					want := `"pending":"` + kind + `"`
					if strings.Contains(string(b), want) != (state == runrec.StatePaused) {
						t.Errorf("%s pending kind wrong: %s", method, b)
					}
				}
			})
		}
	}
}

func TestStatusNeedsWire(t *testing.T) {
	s := source.Snapshot{
		Open: source.Result[[]adjcli.Record]{Value: []adjcli.Record{{ID: 125, Key: "ui.status-list", Title: "Status list"}}},
		Items: source.Result[[]slicercli.Item]{Value: []slicercli.Item{
			{ID: "S32", Title: "Owner answer", Fields: slicercli.Fields{Flags: []string{"needs-owner"}}},
			{ID: "S33", Title: "Ordinary item"},
		}},
		EngineRuns: []source.EngineRun{{ID: "paused", State: "paused"}, {ID: "committed", State: "committed"}, {ID: "running", State: "implement"}},
	}
	ps := projectStatus(registry.Project{Name: "p"}, s, time.Now())
	b, err := json.Marshal(ps)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"open":[{"id":125,"key":"ui.status-list","title":"Status list"}]`, `"held":[{"id":"S32","title":"Owner answer"}]`, `"needs_you":3`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s: %s", want, b)
		}
	}
	if len(ps.EngineRuns) != 3 {
		t.Fatalf("lost engine runs: %s", b)
	}
	empty, _ := json.Marshal(projectStatus(registry.Project{Name: "empty"}, source.Snapshot{}, time.Now()))
	if strings.Contains(string(empty), `"open"`) || strings.Contains(string(empty), `"held"`) {
		t.Fatalf("empty fields not omitted: %s", empty)
	}
}

func TestEngineRunUpdatedAtZeroWire(t *testing.T) {
	var r EngineRun
	if err := json.Unmarshal([]byte(`{"id":"old","state":"stopped"}`), &r); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"updated_at"`) {
		t.Fatalf("zero update must be omitted: %s", b)
	}
}

func TestDismissedStoppedRunLeavesNeedsYou(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	if _, err := store.Create(runrec.Record{ID: "dismiss", Project: p.Name, Item: "S34", State: runrec.StateStopped}); err != nil {
		t.Fatal(err)
	}
	var before StatusResult
	if err := json.Unmarshal(request(t, d, "status", `{"project":"p"}`), &before); err != nil {
		t.Fatal(err)
	}
	request(t, d, "run.dismiss", `{"run":"dismiss"}`)
	var after StatusResult
	if err := json.Unmarshal(request(t, d, "status", `{"project":"p"}`), &after); err != nil {
		t.Fatal(err)
	}
	if len(before.Projects[0].EngineRuns) != 1 || len(after.Projects[0].EngineRuns) != 1 || !after.Projects[0].EngineRuns[0].Dismissed || after.Projects[0].NeedsYou != before.Projects[0].NeedsYou-1 {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	var snapshot struct {
		EngineRuns []EngineRun `json:"engine_runs"`
	}
	if err := json.Unmarshal(request(t, d, "snapshot", `{"project":"p"}`), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.EngineRuns) != 1 || !snapshot.EngineRuns[0].Dismissed || snapshot.EngineRuns[0].Section(snapshot.EngineRuns) != source.SectionHidden {
		t.Fatalf("snapshot must retain dismissed run for its pane, hidden from lists: %+v", snapshot)
	}
	r, err := store.Open("dismiss")
	if err != nil || r.Snapshot().DismissedAt.IsZero() {
		t.Fatalf("record not retained: %v", err)
	}
}

func TestDismissedNewerRunStillSupersedesStoppedRun(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for i, row := range []struct{ id, item string }{{"older", "S22"}, {"other", "S23"}, {"newer", "S22"}} {
		store.Now = func() time.Time { return at.Add(time.Duration(i) * time.Minute) }
		if _, err := store.Create(runrec.Record{ID: row.id, Project: p.Name, Item: row.item, State: runrec.StateStopped}); err != nil {
			t.Fatal(err)
		}
	}
	var before StatusResult
	if err := json.Unmarshal(request(t, d, "status", `{"project":"p"}`), &before); err != nil {
		t.Fatal(err)
	}
	if before.Projects[0].NeedsYou != 2 {
		t.Fatalf("before needs_you = %d, want 2", before.Projects[0].NeedsYou)
	}
	request(t, d, "run.dismiss", `{"run":"newer"}`)
	var after StatusResult
	if err := json.Unmarshal(request(t, d, "status", `{"project":"p"}`), &after); err != nil {
		t.Fatal(err)
	}
	if after.Projects[0].NeedsYou != before.Projects[0].NeedsYou-1 {
		t.Fatalf("after needs_you = %d, want 1", after.Projects[0].NeedsYou)
	}
	for _, method := range []string{"status", "snapshot"} {
		raw := request(t, d, method, `{"project":"p"}`)
		var runs []EngineRun
		if method == "status" {
			runs = after.Projects[0].EngineRuns
		} else {
			var snapshot struct {
				EngineRuns []EngineRun `json:"engine_runs"`
			}
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				t.Fatal(err)
			}
			runs = snapshot.EngineRuns
		}
		var visible []EngineRun
		for _, r := range runs {
			if r.Section(runs) != source.SectionHidden {
				visible = append(visible, r)
			}
		}
		if len(visible) != 1 || visible[0].ID != "other" {
			t.Fatalf("%s visible runs = %+v, want only other slice", method, visible)
		}
	}
}
