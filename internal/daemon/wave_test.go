// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

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
