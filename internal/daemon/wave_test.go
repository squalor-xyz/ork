// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
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
	testBlockedWave(t, []string{"S1", "S2"}, []clitest.Route{{Tool: "slicer", Args: []string{"deps", "S2"}, Prefix: true, Stdout: `{"id":"S2","blocked_by":["S1"]}`}}, `S2 waits on S1 "Title" (in this wave, not done); untick S2, or land S1 first`, CodeRefused)
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
	const want = `S3 waits on S9 "Title" (not done), S2 "Title" (in this wave, not done); S2 waits on S1 "Title" (not done); untick S3 and S2, or land S9, S2 and S1 first`
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
	}, `S2 waits on S9 "First title" (not done), S8 "Second title" (not done); land S9 and S8 first`, CodeRefused)
}
func TestBlockedMessageFallsBackToTheIdWhenATitleReadFails(t *testing.T) {
	testBlockedWave(t, []string{"S2"}, []clitest.Route{
		{Tool: "slicer", Args: []string{"deps", "S2"}, Prefix: true, Stdout: `{"blocked_by":["S9","S8"]}`},
		{Tool: "slicer", Args: []string{"show", "S9"}, Prefix: true, Exit: 2, Stdout: `{"error":{"code":"not_found","message":"missing title"}}`},
		{Tool: "slicer", Args: []string{"show", "S8"}, Prefix: true, Stdout: `{"title":"Known title"}`},
	}, `S2 waits on S9 (not done), S8 "Known title" (not done); land S9 and S8 first`, CodeRefused)
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
	}, `S2 waits on S1 "Title" (in this wave, not done); S1 waits on S2 "Title" (in this wave, not done); untick S2 and S1, or land S1 and S2 first`, CodeRefused)
}

func TestBlockedMessageNamesASharedBlockerTitleOnce(t *testing.T) {
	deps := []slicercli.ItemDeps{{ID: "S2", BlockedBy: []string{"S9"}}, {ID: "S3", BlockedBy: []string{"S9"}}}
	const want = `S2 waits on S9 "Prior" (not done); S3 waits on S9 (not done); land S9 first`
	if got := BlockedMessage([]string{"S2", "S3"}, deps, map[string]string{"S9": "Prior"}); got != want {
		t.Errorf("message %q, want %q", got, want)
	}
}

func TestBlockedMessageJoinsNamesWithAnd(t *testing.T) {
	for name, tc := range map[string]struct {
		ids  []string
		deps []slicercli.ItemDeps
		want string
	}{
		"several items, blockers in and outside the wave": {
			ids:  []string{"S182", "S183"},
			deps: []slicercli.ItemDeps{{ID: "S182", BlockedBy: []string{"S181"}}, {ID: "S183", BlockedBy: []string{"S182", "S175"}}},
			want: "S182 waits on S181 (not done); S183 waits on S182 (in this wave, not done), S175 (not done); untick S182 and S183, or land S181, S182 and S175 first",
		},
		"one blocker in the wave": {
			ids:  []string{"S1", "S2"},
			deps: []slicercli.ItemDeps{{ID: "S2", BlockedBy: []string{"S1"}}},
			want: "S2 waits on S1 (in this wave, not done); untick S2, or land S1 first",
		},
		"two blockers outside the wave": {
			ids:  []string{"S2"},
			deps: []slicercli.ItemDeps{{ID: "S2", BlockedBy: []string{"S9", "S8"}}},
			want: "S2 waits on S9 (not done), S8 (not done); land S9 and S8 first",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := BlockedMessage(tc.ids, tc.deps, nil); got != tc.want {
				t.Errorf("message %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBlockedMessageShowsATitleWithAQuoteAsIs(t *testing.T) {
	deps := []slicercli.ItemDeps{{ID: "S2", BlockedBy: []string{"S9"}}}
	for title, want := range map[string]string{
		`The "big" one`: `S2 waits on S9 "The "big" one" (not done); land S9 first`,
		"a\x1b[31mb\nc": `S2 waits on S9 "ab c" (not done); land S9 first`,
	} {
		if got := BlockedMessage([]string{"S2"}, deps, map[string]string{"S9": title}); got != want {
			t.Errorf("title %q gave %q, want %q", title, got, want)
		}
	}
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

func TestWaveStartRefusesAMemberWithANonCommandCheckEntry(t *testing.T) {
	w := newWaveRigRoutes(t, 4, 0, clitest.Route{Tool: "slicer", Args: []string{"show", "S1"}, Prefix: true, Stdout: `{"id":"S1","slice":{"id":"S1","sections":[{"heading":"Check","body":"- ` + "`q quit`" + `"}]}}`})
	var out WaveResult
	if err := json.Unmarshal(request(t, w.d, MethodWaveStart, `{"items":[{"project":"p","item":"S1"},{"project":"p","item":"S2"}]}`), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("items %+v", out.Items)
	}
	if it := out.Items[0]; it.Error == nil || it.Error.Code != CodeRefused || !strings.Contains(it.Error.Message, "q quit") || !strings.Contains(it.Error.Message, "S1") || it.Run != nil {
		t.Errorf("bad member: %+v", it)
	}
	if it := out.Items[1]; it.Error != nil || it.Run == nil || it.Run.Item != "S2" {
		t.Fatalf("good member: %+v", it)
	}
	if runs, err := w.store.List(); err != nil || len(runs) != 1 || runs[0].Snapshot().Item != "S2" {
		t.Errorf("stored runs: %+v %v", runs, err)
	}
}

func TestWaveStartRefusesAProjectWithoutACheckCommand(t *testing.T) {
	w := newWaveRig(t, 4, 0)
	editProject(t, w.d, func(p *registry.Project) { p.CheckCommand = "" })
	want := "project p: no check command: ork refuses to start a run (fix: slicer config check_command CMD, ork projects set p --check CMD, or start with --allow-no-check)"
	// The whole wave is refused, even when one item carries the flag.
	for _, raw := range []string{
		`{"items":[{"project":"p","item":"S1"},{"project":"p","item":"S2"}]}`,
		`{"items":[{"project":"p","item":"S1","allow_no_check":true},{"project":"p","item":"S2"}]}`,
	} {
		_, err := w.d.handle(context.Background(), MethodWaveStart, json.RawMessage(raw))
		if re := ipc.ErrorOf(err); err == nil || re.Code != CodeRefused || re.Message != want {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if runs, err := w.store.List(); err != nil || len(runs) != 0 {
		t.Fatalf("a refusal created runs: %v %v", runs, err)
	}
	_, runs := waveRecords(t, w.d, `{"items":[{"project":"p","item":"S1","allow_no_check":true},{"project":"p","item":"S2","allow_no_check":true}]}`)
	if len(runs) != 2 {
		t.Fatalf("allow_no_check wave: %+v", runs)
	}
}

// overlapRoute makes `slicer deps --overlap ...` answer with stdout.
func overlapRoute(stdout string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"deps", "--overlap"}, Prefix: true, Stdout: stdout}
}

// The pairs the slice names: s455 shares snapshot.py with s456 and with s451.
const overlapTwoPairs = `{"overlap":[{"a":"s455","b":"s456","shared":["snapshot.py"]},{"a":"s455","b":"s451","shared":["snapshot.py"]}]}`

const overlapWaveItems = `{"items":[{"project":"p","item":"s455"},{"project":"p","item":"s450"},{"project":"p","item":"s456"},{"project":"p","item":"s451"}]}`

func TestWaveStartHoldsAnOverlappingMemberBehindTheEarlierOne(t *testing.T) {
	// Three slots: after s455 and s450 start, one is free for a held member.
	w := newWaveRigRoutes(t, 3, 0, overlapRoute(overlapTwoPairs))
	_, runs := waveRecords(t, w.d, overlapWaveItems)
	if len(runs) != 4 {
		t.Fatalf("runs: %+v", runs)
	}
	w.expectStarts(t, "s455", "s450")
	for i, wantAfter := range [][]string{nil, nil, {"s455"}, {"s455"}} {
		disk := w.record(t, runs[i].ID)
		if !slices.Equal(runs[i].After, wantAfter) || !slices.Equal(disk.After, wantAfter) {
			t.Errorf("%s after: reply %q disk %q, want %q", runs[i].Item, runs[i].After, disk.After, wantAfter)
		}
		if held := wantAfter != nil; held != (disk.State == runrec.StateQueued) {
			t.Errorf("%s state %s, held %t", runs[i].Item, disk.State, held)
		}
	}
	w.expectNoStart(t) // a slot is free, and the held members do not take it
	w.free <- struct{}{}
	w.expectNoStart(t) // a role ended; the held members still wait
	for _, i := range []int{2, 3} {
		if state := w.record(t, runs[i].ID).State; state != runrec.StateQueued {
			t.Errorf("%s state %s, want queued", runs[i].Item, state)
		}
	}
}

func TestWaveHeldMemberStartsAfterTheEarlierLands(t *testing.T) {
	root := t.TempDir()
	d, store, p := runDaemonIn(t, root, passRunner(), overlapRoute(overlapTwoPairs),
		clitest.Route{Tool: "slicer", Args: []string{"note"}, Prefix: true, Stdout: `{"id":"s455","added":true}`})
	if err := os.MkdirAll(p.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := registry.Save(d.cfg.RegistryPath, &registry.File{Version: registry.Version, GlobalCap: 8, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	items := `{"items":[{"project":"p","item":"s455","well_specified":true},{"project":"p","item":"s450","well_specified":true},{"project":"p","item":"s456","well_specified":true},{"project":"p","item":"s451","well_specified":true}]}`
	_, runs := waveRecords(t, d, items)
	for _, i := range []int{0, 1} {
		waitState(t, store, runs[i].ID, runrec.StateAwaitingApproval)
		waitIdle(t, d, runs[i].ID)
	}
	// Both held members wait with every slot free: s455 has not landed.
	time.Sleep(100 * time.Millisecond)
	for _, i := range []int{2, 3} {
		run, err := store.Open(runs[i].ID)
		if err != nil {
			t.Fatal(err)
		}
		if rec := run.Snapshot(); rec.State != runrec.StateQueued || len(rec.Roles) != 0 {
			t.Fatalf("%s state %s with %d roles before s455 landed", runs[i].Item, rec.State, len(rec.Roles))
		}
	}
	request(t, d, "run.approve", fmt.Sprintf(`{"run":%q}`, runs[0].ID))
	landed := waitState(t, store, runs[0].ID, runrec.StateCommitted)
	if landed.LandedAt.IsZero() {
		t.Fatalf("s455 committed without landing: %+v", landed)
	}
	// s451 overlaps s455 only, so it starts with s456 once s455 has landed.
	for _, i := range []int{2, 3} {
		rec := waitState(t, store, runs[i].ID, runrec.StateAwaitingApproval)
		if len(rec.Roles) == 0 || rec.Roles[0].StartedAt.Before(landed.LandedAt) {
			t.Errorf("%s started before s455 landed at %v: %+v", runs[i].Item, landed.LandedAt, rec.Roles)
		}
	}
}

func TestWaveHeldMemberStaysWhenTheEarlierStops(t *testing.T) {
	w := newWaveRigRoutes(t, 4, 0, overlapRoute(`{"overlap":[{"a":"s455","b":"s456","shared":["snapshot.py"]}]}`))
	_, runs := waveRecords(t, w.d, `{"items":[{"project":"p","item":"s455"},{"project":"p","item":"s456"}]}`)
	w.expectStart(t, "s455")
	request(t, w.d, "run.stop", fmt.Sprintf(`{"run":%q}`, runs[0].ID))
	waitState(t, w.store, runs[0].ID, runrec.StateStopped)
	waitIdle(t, w.d, runs[0].ID)
	w.expectNoStart(t)
	if state := w.record(t, runs[1].ID).State; state != runrec.StateQueued {
		t.Fatalf("s456 state %s, want queued", state)
	}
	var wait string
	for _, row := range w.d.engineRuns("p") {
		if row.Item == "s456" {
			wait = row.Wait
		}
	}
	if !strings.Contains(wait, "waits on s455 (stopped)") {
		t.Errorf("s456 wait %q", wait)
	}
}

func TestWaveStartSaysWhyAMemberIsHeld(t *testing.T) {
	w := newWaveRigRoutes(t, 4, 0, overlapRoute(overlapTwoPairs))
	var params WaveStartParams
	if err := json.Unmarshal([]byte(overlapWaveItems), &params); err != nil {
		t.Fatal(err)
	}
	result, err := w.d.startWave(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range result.Items {
		got = append(got, item.Item+": "+item.Message)
	}
	want := []string{"s455: ", "s450: ", "s456: held until s455 (snapshot.py) lands", "s451: held until s455 (snapshot.py) lands"}
	if !slices.Equal(got, want) {
		t.Errorf("messages %q, want %q", got, want)
	}
}

// An earlier member that was not admitted leaves no run to wait on.
func TestWaveStartDoesNotHoldBehindAMemberThatDidNotStart(t *testing.T) {
	w := newWaveRigRoutes(t, 4, 0, overlapRoute(`{"overlap":[{"a":"s455","b":"s456","shared":["snapshot.py"]}]}`))
	var params WaveStartParams
	if err := json.Unmarshal([]byte(`{"items":[{"project":"p","item":"s455"},{"project":"p","item":"s456"}]}`), &params); err != nil {
		t.Fatal(err)
	}
	params.Items[0].Expected = &RunPlan{} // a stale confirmed plan: s455 is refused
	result, err := w.d.startWave(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Items[0].Error == nil || result.Items[1].Run == nil || len(result.Items[1].Run.After) != 0 || result.Items[1].Message != "" {
		t.Fatalf("result: %+v", result)
	}
	w.expectStart(t, "s456")
}

func TestWaveOrderHoldsALaterMemberBehindEveryEarlierOverlap(t *testing.T) {
	ids := []string{"s455", "s450", "s456", "s451"}
	pairs := []slicercli.OverlapPair{
		{A: "s451", B: "s456", Shared: []string{"b.py"}}, // slicer may name a pair in either order
		{A: "s455", B: "s456", Shared: []string{"snapshot.py"}},
		{A: "s455", B: "s451", Shared: []string{"snapshot.py", "a.py"}},
		{A: "s450", B: "s451"},                           // shares nothing
		{A: "s455", B: "s999", Shared: []string{"x.py"}}, // not in the wave
	}
	got := OrderLine(WaveOrder(ids, pairs))
	want := "order: s456 after s455 (snapshot.py); s451 after s455 (snapshot.py, a.py); s451 after s456 (b.py)"
	if got != want {
		t.Errorf("order line %q, want %q", got, want)
	}
	if line := OrderLine(WaveOrder(ids, nil)); line != "" {
		t.Errorf("order line without overlap: %q", line)
	}
}

func TestWaitingOnNamesEachItemWhoseNewestRunHasNotLanded(t *testing.T) {
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	rec := func(id, item, state string, age time.Duration, mod func(*runrec.Record)) runrec.Record {
		r := runrec.Record{ID: id, Project: "p", Item: item, State: state, CreatedAt: at.Add(age)}
		if mod != nil {
			mod(&r)
		}
		return r
	}
	landed := func(r *runrec.Record) { r.LandedAt = at }
	cleaned := func(r *runrec.Record) { r.CleanedAt = at }
	notMerged := func(r *runrec.Record) { r.CleanedAt, r.CleanedReason = at, "branch gone, not merged" }
	all := []runrec.Record{
		rec("a", "s1", runrec.StateCommitted, 0, landed),
		rec("b", "s2", runrec.StateCommitted, 0, nil), // committed at stop_after commit, not landed
		rec("c", "s3", runrec.StateCommitted, 0, cleaned),
		rec("d", "s4", runrec.StateCommitted, 0, notMerged),
		rec("e", "s5", runrec.StateCommitted, 0, landed),
		rec("f", "s5", runrec.StateStopped, time.Hour, nil), // a newer run of s5 decides
		rec("g", "s6", runrec.StateStopped, 0, nil),
		rec("h", "s7", runrec.StateImplement, 0, nil),
		{ID: "i", Project: "other", Item: "s9", State: runrec.StateCommitted, LandedAt: at, CreatedAt: at}, // another project's s9
	}
	held := runrec.Record{Project: "p", After: []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9"}}
	got := strings.Join(waitingOn(held, all), ", ")
	want := "s2 (committed), s4 (committed), s5 (stopped), s6 (stopped), s7 (implement), s8 (no run), s9 (no run)"
	if got != want {
		t.Errorf("waiting on %q, want %q", got, want)
	}
}

// An earlier member that runs as a sliver chain has runs named for its slivers,
// with the member as their parent. The chain lands the parent through its last
// sliver, so only that run says whether the member has landed.
func TestWaitingOnCountsAChainOfSliversAsItsParent(t *testing.T) {
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	sliver := func(id string, index int, state string, mod func(*runrec.Record)) runrec.Record {
		r := runrec.Record{ID: id, Project: "p", Item: "s10-" + id, Parent: "s10", Chain: "chain-1", SliverIndex: index, SliverCount: 2, State: state, CreatedAt: at.Add(time.Duration(index) * time.Second)}
		if mod != nil {
			mod(&r)
		}
		return r
	}
	landed := func(r *runrec.Record) { r.LandedAt = at }
	held := runrec.Record{Project: "p", After: []string{"s10"}}
	for name, tc := range map[string]struct {
		all  []runrec.Record
		want string
	}{
		"the last sliver has not run":                                    {[]runrec.Record{sliver("a", 1, runrec.StateCommitted, nil), sliver("b", 2, runrec.StateQueued, nil)}, "s10 (queued)"},
		"the last sliver is in progress":                                 {[]runrec.Record{sliver("a", 1, runrec.StateCommitted, nil), sliver("b", 2, runrec.StateImplement, nil)}, "s10 (implement)"},
		"the last sliver landed":                                         {[]runrec.Record{sliver("a", 1, runrec.StateCommitted, nil), sliver("b", 2, runrec.StateCommitted, landed)}, ""},
		"a sliver committed without a landing is not the parent landing": {[]runrec.Record{sliver("a", 1, runrec.StateCommitted, landed), sliver("b", 2, runrec.StateStopped, nil)}, "s10 (stopped)"},
		// The slivers are created together; the last one decides, whatever the ids sort to.
		"slivers made at one instant": {[]runrec.Record{
			sliver("z", 1, runrec.StateCommitted, func(r *runrec.Record) { r.LandedAt, r.CreatedAt = at, at }),
			sliver("a", 2, runrec.StateStopped, func(r *runrec.Record) { r.CreatedAt = at }),
		}, "s10 (stopped)"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := strings.Join(waitingOn(held, tc.all), ", "); got != tc.want {
				t.Errorf("waiting on %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWaveStartWithoutOverlapIsUnchanged(t *testing.T) {
	// slicer lists a pair that shares nothing without a shared array.
	w := newWaveRigRoutes(t, 4, 0, overlapRoute(`{"overlap":[{"a":"s455","b":"s456"}]}`))
	_, runs := waveRecords(t, w.d, `{"items":[{"project":"p","item":"s455"},{"project":"p","item":"s456"}]}`)
	w.expectStarts(t, "s455", "s456")
	for _, run := range runs {
		if len(run.After) != 0 || run.State == runrec.StateQueued {
			t.Errorf("%s was held: %+v", run.Item, run)
		}
	}
}
