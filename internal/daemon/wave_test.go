// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// Keep requests as wire JSON so these tests also cover method dispatch and decoding.
func waveRecords(t *testing.T, d *Daemon, raw string) (string, []runrec.Record) {
	t.Helper()
	var result struct {
		Wave  string `json:"wave"`
		Items []struct {
			Run   *runrec.Record  `json:"run"`
			Error json.RawMessage `json:"error"`
		} `json:"items"`
	}
	if err := json.Unmarshal(request(t, d, "wave.start", raw), &result); err != nil {
		t.Fatal(err)
	}
	var runs []runrec.Record
	for _, item := range result.Items {
		if item.Run == nil {
			t.Fatalf("item failed: %s", item.Error)
		}
		runs = append(runs, *item.Run)
	}
	return result.Wave, runs
}

func TestWaveStartStampsOneWaveIDOnEachRun(t *testing.T) {
	w := newWaveRig(t, 4, 0)
	wave, runs := waveRecords(t, w.d, `{"items":[{"project":"p","item":"S1"},{"project":"p","item":"S2"}]}`)
	if len(runs) != 2 || !regexp.MustCompile(`^wave-\d{8}-\d{6}-[0-9a-f]{4}$`).MatchString(wave) {
		t.Fatalf("wave %q runs %+v", wave, runs)
	}
	for _, run := range runs {
		disk, err := w.store.Open(run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Wave != wave || disk.Snapshot().Wave != wave {
			t.Fatalf("wave %q run %+v disk %+v", wave, run, disk.Snapshot())
		}
	}
	w.expectStarts(t, "S1", "S2")
}

func TestWaveStartRefusesTwoProjects(t *testing.T) {
	w := newWaveRig(t, 4, 0)
	_, err := w.d.handle(context.Background(), "wave.start", json.RawMessage(`{"items":[{"project":"p","item":"S1"},{"project":"other","item":"S2"}]}`))
	if err == nil || !strings.Contains(err.Error(), "one project") {
		t.Fatalf("error: %v", err)
	}
	runs, err := w.store.List()
	if err != nil || len(runs) != 0 {
		t.Fatalf("refusal created runs: %v %v", runs, err)
	}
}

func TestWaveStartContinuesAfterOneFails(t *testing.T) {
	w := newWaveRig(t, 1, 0)
	// A launch error must be reported per item and must not hide later starts.
	var result struct {
		Items []struct {
			Run   *runrec.Record  `json:"run"`
			Error json.RawMessage `json:"error"`
		} `json:"items"`
	}
	b := request(t, w.d, "wave.start", `{"items":[{"project":"p","item":"bad","lineup":{"implementer":[{"vendor":"codex"}]}},{"project":"p","item":"S1"},{"project":"p","item":"S2"}]}`)
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 3 || len(result.Items[0].Error) == 0 || result.Items[1].Run == nil || result.Items[2].Run == nil {
		t.Fatalf("result: %s", b)
	}
	w.expectStart(t, "S1")
	w.free <- struct{}{} // The first role fails. The queued peer must still run.
	w.expectStart(t, "S2")
	waitState(t, w.store, result.Items[1].Run.ID, runrec.StateStopped)
}

func TestWaveStartUsesQueuedCaps(t *testing.T) {
	for _, caps := range [][2]int{{1, 0}, {4, 1}} {
		w := newWaveRig(t, caps[0], caps[1])
		_, runs := waveRecords(t, w.d, `{"items":[{"project":"p","item":"S1"},{"project":"p","item":"S2"}]}`)
		if len(runs) != 2 || runs[0].State == runrec.StateQueued || runs[1].State != runrec.StateQueued || len(runs[1].Writes) != 0 {
			t.Fatalf("runs: %+v", runs)
		}
		w.expectStart(t, "S1")
		w.expectNoStart(t)
		w.d.closeRuns()
	}
}

func TestStopWaveStopsEachRunOnce(t *testing.T) {
	w := newWaveRig(t, 1, 0)
	wave, runs := waveRecords(t, w.d, `{"items":[{"project":"p","item":"S1"},{"project":"p","item":"S2"},{"project":"p","item":"S3"}]}`)
	w.expectStart(t, "S1")
	other := w.start(t, "S4")
	// A final member is skipped, including its on-disk timestamp.
	final, err := w.store.Create(runrec.Record{ID: "final", Project: "p", Wave: wave, State: runrec.StateStopped})
	if err != nil {
		t.Fatal(err)
	}
	finalPath := filepath.Join(w.store.Dir, final.Snapshot().ID, "run.json")
	before, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		b := request(t, w.d, "wave.stop", `{"wave":"`+wave+`"}`)
		var result struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		want := 3
		if attempt == 1 {
			want = 0
		}
		if len(result.Items) != want {
			t.Fatalf("stop attempt %d: %s", attempt, b)
		}
		for _, item := range result.Items {
			var stopped struct {
				Error json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(item, &stopped); err != nil || len(stopped.Error) != 0 {
				t.Fatalf("stop failed: %s (%v)", item, err)
			}
		}
	}
	for i, run := range runs {
		rec := waitState(t, w.store, run.ID, runrec.StateStopped)
		if i > 0 && (len(rec.Writes) != 0 || len(rec.Roles) != 0) {
			t.Fatalf("queued member ran: %+v", rec)
		}
	}
	after, err := os.ReadFile(finalPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("final member changed: %v", err)
	}
	w.expectStart(t, "S4")
	r, err := w.store.Open(other.ID)
	if err != nil || r.Snapshot().State == runrec.StateStopped {
		t.Fatalf("unrelated wave stopped: %v %v", r, err)
	}
}

func TestStopWaveSkipsMemberThatFinishesDuringStop(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	store.Now = func() time.Time { return epoch }
	const wave = "wave-finishes-during-stop"
	var members []*managedRun
	for _, id := range []string{"first", "second"} {
		run, err := store.Create(runrec.Record{ID: id, Project: p.Name, Item: id, Wave: wave, State: runrec.StatePaused})
		if err != nil {
			t.Fatal(err)
		}
		r := &managedRun{engine: engine.Load(d.runDeps(p), p, run)}
		d.runs.runs[id] = r
		members = append(members, r)
	}
	// The member list is captured before the first job is cancelled. Finish
	// the second member during that cancellation, before its owner stop.
	d.runs.mu.Lock()
	d.launchRun(members[0], func(ctx context.Context) error {
		<-ctx.Done()
		return members[1].engine.Stop(context.Background(), "finished during first stop")
	})
	d.runs.mu.Unlock()
	result, err := d.stopWave(context.Background(), WaveAction{Wave: wave})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Item != "first" || result.Items[0].Error != nil {
		t.Fatalf("finished member was not skipped: %+v", result.Items)
	}
	if rec := members[1].engine.Snapshot(); rec.State != runrec.StateStopped || rec.Reason != "stopped by the owner: finished during first stop" {
		t.Fatalf("finished member changed: %+v", rec)
	}
}

func TestWaveStartChecksOverridesAndExpectedPerItem(t *testing.T) {
	w := newWaveRig(t, 4, 0)
	input := `{"project":"p","item":"S1","lineup":{"reviewer":[{"vendor":"claude","model":"custom","effort":"high"}]},"budgets":{"minutes":30,"tokens":9000}}`
	plan := launchPlan(t, w.d, input)
	var p RunParams
	if err := json.Unmarshal([]byte(input), &p); err != nil {
		t.Fatal(err)
	}
	p.Expected = &plan
	// A stale confirmed plan is one failed item, with no admission or writes.
	stale := p
	stale.Item = "S2"
	raw, err := json.Marshal(WaveStartParams{Items: []RunParams{stale, p}})
	if err != nil {
		t.Fatal(err)
	}
	var result WaveResult
	if err := json.Unmarshal(request(t, w.d, "wave.start", string(raw)), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].Error == nil || !strings.Contains(result.Items[0].Error.Message, "plan changed") || result.Items[1].Run == nil {
		t.Fatalf("result: %+v", result)
	}
	rec := result.Items[1].Run
	if rec.Budgets.Minutes != 30 || rec.Budgets.Tokens != 9000 || rec.Lineup["reviewer"][0].Model != "custom" || rec.Lineup["gate"][0].Model != "custom" {
		t.Fatalf("overrides: %+v", rec)
	}
	runs, err := w.store.List()
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs: %v %v", runs, err)
	}
	w.expectStart(t, "S1")
}

func TestWaveBadParamsAndUnknownID(t *testing.T) {
	d, store, _ := runDaemon(t, nil)
	for _, tc := range []struct{ method, raw, code string }{
		{"wave.start", `{}`, CodeBadParams},
		{"wave.start", `{"items":[{"project":"p"}]}`, CodeBadParams},
		{"wave.start", `{"items":[{"project":"p","item":"S1","wave":"wave-chosen"}]}`, CodeBadParams},
		{"wave.stop", `{}`, CodeBadParams},
		{"wave.stop", `{"wave":"ork-1"}`, CodeBadParams},
		{"wave.stop", `{"wave":"wave-missing"}`, CodeNotFound},
	} {
		_, err := d.handle(context.Background(), tc.method, json.RawMessage(tc.raw))
		if err == nil || ipc.ErrorOf(err).Code != tc.code {
			t.Fatalf("%s %s: %v", tc.method, tc.raw, err)
		}
	}
	if runs, err := store.List(); err != nil || len(runs) != 0 {
		t.Fatalf("runs: %v %v", runs, err)
	}
}

func TestWaveStartRefusesTwoEngineSlices(t *testing.T) {
	for _, pair := range [][2]string{{"internal/engine/a.go:12", "internal/daemon/b.go"}, {"cmd/ork/main.go", "internal/engine/c.go"}} {
		t.Run(pair[0], func(t *testing.T) {
			routes := []clitest.Route{}
			for i, id := range []string{"S1", "S2"} {
				body, _ := json.Marshal(map[string]any{"slice": map[string]any{"sections": []map[string]string{{"heading": "Files", "body": "- `" + pair[i] + "`"}}}})
				routes = append(routes, clitest.Route{Tool: "slicer", Args: []string{"show", id}, Prefix: true, Stdout: string(body)})
			}
			d, store, _ := runDaemon(t, nil, routes...)
			_, err := d.startWave(context.Background(), WaveStartParams{Items: []RunParams{{Project: "p", Item: "S1"}, {Project: "p", Item: "S2"}}})
			if err == nil || !strings.Contains(err.Error(), "S1") || !strings.Contains(err.Error(), "S2") {
				t.Fatalf("expected conflict naming S1 and S2, got %v", err)
			}
			runs, listErr := store.List()
			if listErr != nil || len(runs) != 0 {
				t.Fatalf("refusal wrote runs: %v %v", runs, listErr)
			}
		})
	}
	// One engine slice must pass through normal admission, with a non-engine peer.
	d, _, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"show", "S1"}, Prefix: true, Stdout: "{\"slice\":{\"sections\":[{\"heading\":\"Files\",\"body\":\"- `internal/engine/a.go`\"}]}}"})
	_, runs := waveRecords(t, d, `{"items":[{"project":"p","item":"S1"},{"project":"p","item":"S2"}]}`)
	if len(runs) != 2 {
		t.Fatalf("one engine slice was refused: %v", runs)
	}
}

func TestWaveStartAcceptsAnArchitectOverride(t *testing.T) {
	w := newWaveRig(t, 4, 0)
	_, runs := waveRecords(t, w.d, `{"items":[{"project":"p","item":"S1","lineup":{"architect":[{"vendor":"codex","model":"sol","effort":"high"}]}},{"project":"p","item":"S2","lineup":{"architect":[]}}]}`)
	if len(runs) != 2 || len(runs[0].Lineup["architect"]) != 1 || runs[0].Lineup["architect"][0].Model != "sol" || len(runs[1].Lineup["architect"]) != 0 {
		t.Fatalf("architect overrides: %+v", runs)
	}
	w.expectStarts(t, "S1", "S2")
}

func TestWaveStartRefusesABlockedItem(t *testing.T) {
	testBlockedWave(t, []string{"S1", "S2"}, []clitest.Route{{Tool: "slicer", Args: []string{"deps", "S2"}, Prefix: true, Stdout: `{"id":"S2","blocked_by":["S9"]}`}}, `S2 waits on S9 "Title" (not done); land S9 first`, CodeRefused)
}
func TestWaveStartNamesABlockerInTheSameWave(t *testing.T) {
	testBlockedWave(t, []string{"S1", "S2"}, []clitest.Route{{Tool: "slicer", Args: []string{"deps", "S2"}, Prefix: true, Stdout: `{"id":"S2","blocked_by":["S1"]}`}}, `S2 waits on S1 "Title" (in this wave, not done); untick S2 or land S1 first`, CodeRefused)
}
func TestWaveStartRefusesWhenTheDepsReadFails(t *testing.T) {
	testBlockedWave(t, []string{"S1"}, []clitest.Route{{Tool: "slicer", Args: []string{"deps", "S1"}, Prefix: true, Exit: 2, Stdout: `{"error":{"code":"locked","message":"deps locked"}}`}}, "deps locked", "locked")
}
func testBlockedWave(t *testing.T, ids []string, routes []clitest.Route, want, code string) {
	t.Helper()
	d, store, _ := runDaemon(t, func(context.Context, engine.Request) (engine.Result, error) {
		t.Error("role ran before dependency refusal")
		return engine.Result{}, nil
	}, routes...)
	params := WaveStartParams{}
	for _, id := range ids {
		params.Items = append(params.Items, RunParams{Project: "p", Item: id})
	}
	result, err := d.startWave(context.Background(), params)
	if err == nil || ipc.ErrorOf(err).Code != code || ipc.ErrorOf(err).Message != want {
		t.Errorf("result %+v error %v, want %s: %s", result, err, code, want)
	}
	if runs, err := store.List(); err != nil || len(runs) != 0 {
		t.Errorf("refusal wrote runs: %v %v", runs, err)
	}
	for _, call := range lastFake.CallsTo("slicer") {
		if len(call.Args) > 0 && call.Args[0] != "deps" && call.Args[0] != "show" {
			t.Errorf("unexpected call before refusal: %+v", call)
		}
	}
}
func TestWaveStartAllowsItemsWithNoBlockers(t *testing.T) {
	w := newWaveRig(t, 4, 0)
	_, runs := waveRecords(t, w.d, `{"items":[{"project":"p","item":"S1"},{"project":"p","item":"S2"}]}`)
	if len(runs) != 2 {
		t.Fatalf("runs: %+v", runs)
	}
	w.expectStarts(t, "S1", "S2")
}

func TestWaveStartPreservesDependencyOrderAndReadsEachItemOnce(t *testing.T) {
	routes := []clitest.Route{
		{Tool: "slicer", Args: []string{"deps", "S3"}, Prefix: true, Stdout: `{"id":"S3","blocked_by":["S9","S2"]}`},
		{Tool: "slicer", Args: []string{"deps", "S2"}, Prefix: true, Stdout: `{"id":"S2","blocked_by":["S1"]}`},
	}
	d, _, _ := runDaemon(t, nil, routes...)
	_, err := d.startWave(context.Background(), WaveStartParams{Items: []RunParams{{Project: "p", Item: "S3"}, {Project: "p", Item: "S2"}, {Project: "p", Item: "S3"}}})
	const want = `S3 waits on S9 "Title" (not done), S2 "Title" (in this wave, not done); S2 waits on S1 "Title" (not done); untick S3, S2 or land S9, S2, S1 first`
	if err == nil || ipc.ErrorOf(err).Message != want {
		t.Fatalf("error: %v", err)
	}
	calls := lastFake.CallsTo("slicer")
	var depsIDs []string
	for _, call := range calls {
		if call.Args[0] == "deps" {
			depsIDs = append(depsIDs, call.Args[1])
		}
	}
	if strings.Join(depsIDs, ",") != "S3,S2" {
		t.Fatalf("deps calls: %+v", calls)
	}
}

func TestBlockedMessageShowsTitlesAndTheNextStep(t *testing.T) {
	testBlockedWave(t, []string{"S2"}, []clitest.Route{
		{Tool: "slicer", Args: []string{"deps", "S2"}, Prefix: true, Stdout: `{"blocked_by":["S9","S8"]}`},
		{Tool: "slicer", Args: []string{"show", "S9"}, Prefix: true, Stdout: `{"title":"First title"}`},
		{Tool: "slicer", Args: []string{"show", "S8"}, Prefix: true, Stdout: `{"title":"Second title"}`},
	}, `S2 waits on S9 "First title" (not done), S8 "Second title" (not done); land S9, S8 first`, CodeRefused)
}
func TestBlockedMessageFallsBackToTheIdWhenATitleReadFails(t *testing.T) {
	testBlockedWave(t, []string{"S2"}, []clitest.Route{
		{Tool: "slicer", Args: []string{"deps", "S2"}, Prefix: true, Stdout: `{"blocked_by":["S9","S8"]}`},
		{Tool: "slicer", Args: []string{"show", "S9"}, Prefix: true, Exit: 2, Stdout: `{"error":{"code":"not_found","message":"missing title"}}`},
		{Tool: "slicer", Args: []string{"show", "S8"}, Prefix: true, Stdout: `{"title":"Known title"}`},
	}, `S2 waits on S9 (not done), S8 "Known title" (not done); land S9, S8 first`, CodeRefused)
}
func TestBlockerTitlesAreReadOncePerDistinctBlocker(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			title := clitest.Route{Tool: "slicer", Args: []string{"show", "S9"}, Prefix: true, Stdout: `{"title":"Title"}`}
			if fail {
				title.Exit = 2
				title.Stdout = `{"error":{"code":"not_found","message":"missing"}}`
			}
			d, _, p := runDaemon(t, nil, title, clitest.Route{Tool: "slicer", Args: []string{"deps"}, Prefix: true, Stdout: `{"blocked_by":["S9","S8"]}`})
			for attempt := 0; attempt < 2; attempt++ {
				err := d.blockedItems(context.Background(), p, []string{"S2", "S3", "S2"})
				if err == nil || ipc.ErrorOf(err).Code != CodeRefused {
					t.Fatalf("error: %v", err)
				}
				counts := map[string]int{}
				for _, call := range lastFake.CallsTo("slicer") {
					if call.Args[0] == "show" {
						counts[call.Args[1]]++
						want := []string{"show", call.Args[1], "--json", "--lean", "--root", p.Path}
						if strings.Join(call.Args, "|") != strings.Join(want, "|") {
							t.Fatalf("title read: %+v", call)
						}
					}
				}
				if counts["S9"] != attempt+1 || counts["S8"] != attempt+1 || len(counts) != 2 {
					t.Fatalf("title reads after call %d: %+v", attempt+1, counts)
				}
			}
		})
	}
}
func TestWaveStartRefusesACycleInsideOneWave(t *testing.T) {
	testBlockedWave(t, []string{"S2", "S1"}, []clitest.Route{
		{Tool: "slicer", Args: []string{"deps", "S2"}, Prefix: true, Stdout: `{"blocked_by":["S1"]}`},
		{Tool: "slicer", Args: []string{"deps", "S1"}, Prefix: true, Stdout: `{"blocked_by":["S2"]}`},
	}, `S2 waits on S1 "Title" (in this wave, not done); S1 waits on S2 "Title" (in this wave, not done); untick S2, S1 or land S1, S2 first`, CodeRefused)
}

func TestWaveStartCarriesStopAfterPerItem(t *testing.T) {
	w := newWaveRig(t, 4, 0)
	_, runs := waveRecords(t, w.d, `{"items":[{"project":"p","item":"S1","stop_after":"commit"},{"project":"p","item":"S2","stop_after":"land"}]}`)
	if len(runs) != 2 || runs[0].StopAfter != "commit" || runs[1].StopAfter != "land" {
		t.Fatalf("runs: %+v", runs)
	}
	for _, rec := range runs {
		disk, err := w.store.Open(rec.ID)
		if err != nil {
			t.Fatal(err)
		}
		if disk.Snapshot().StopAfter != rec.StopAfter {
			t.Fatal("stop point lost")
		}
	}
	w.expectStarts(t, "S1", "S2")
}
