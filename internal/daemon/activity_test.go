// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/source"
)

func TestSnapshotActivityUsesDaemonClock(t *testing.T) {
	emitted := make(chan struct{})
	d, _, _ := runDaemon(t, func(ctx context.Context, req engine.Request) (engine.Result, error) {
		req.OnEvent()
		req.OnEvent()
		req.OnLive(engine.Live{Text: "live assistant text", Tokens: 42})
		close(emitted)
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	})
	clock := &fakeClock{}
	d.cfg.Clock = clock
	d.runs.deps.Now = clock.Now
	request(t, d, MethodRunStart, `{"project":"p","item":"S1"}`)
	select {
	case <-emitted:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not emit")
	}
	for _, age := range []time.Duration{0, 30 * time.Second, 270 * time.Second} {
		clock.Advance(age)
		runs := d.engineRuns("p")
		want := make([]int, 10)
		if age == 0 {
			want[9] = 2
		} else if age == 30*time.Second {
			want[8] = 2
		}
		if len(runs) != 1 || !reflect.DeepEqual(runs[0].Activity, want) {
			t.Fatalf("snapshot activity %v, want %v", runs, want)
		}
		raw, err := json.Marshal(runs)
		if err != nil {
			t.Fatal(err)
		}
		var received []source.EngineRun
		if err := json.Unmarshal(raw, &received); err != nil {
			t.Fatal(err)
		}
		if received[0].Tail != "live assistant text" || received[0].Tokens != 42 || received[0].CostUSD != 0 || !received[0].UsageKnown || !received[0].LiveUsageKnown {
			t.Fatalf("live snapshot lost: %+v", received[0])
		}
		if !reflect.DeepEqual(received[0].Activity, want) {
			t.Fatalf("wire lost activity: %s", raw)
		}
	}
	d.closeRuns()
	if got := d.engineRuns("p")[0].Activity; got != nil {
		t.Fatalf("ended role activity %v", got)
	}
	raw, err := json.Marshal(d.engineRuns("p"))
	if err != nil {
		t.Fatal(err)
	}
	var objects []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objects); err != nil {
		t.Fatal(err)
	}
	if _, present := objects[0]["tail"]; present {
		t.Fatalf("tail after stop: %s", raw)
	}
	if _, present := objects[0]["activity"]; present {
		t.Fatalf("activity key without running role: %s", raw)
	}

}

func TestSnapshotPreservesZeroUsageAndTailCut(t *testing.T) {
	emitted := make(chan struct{})
	d, _, _ := runDaemon(t, func(ctx context.Context, req engine.Request) (engine.Result, error) {
		req.OnLive(engine.Live{Text: strings.Repeat("x", 2100), UsageKnown: true})
		close(emitted)
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	})
	request(t, d, MethodRunStart, `{"project":"p","item":"S1"}`)
	select {
	case <-emitted:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not emit")
	}
	raw, err := json.Marshal(d.engineRuns("p"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []source.EngineRun
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].UsageKnown || !rows[0].LiveUsageKnown || !rows[0].TailTruncated || rows[0].Tokens != 0 {
		t.Fatalf("lost presence or truncation: %s", raw)
	}
}
