// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func settingsCall(t *testing.T, d *Daemon, method, raw string) SettingsResult {
	t.Helper()
	var params json.RawMessage
	if raw != "" {
		params = json.RawMessage(raw)
	}
	got, err := d.handle(context.Background(), method, params)
	if err != nil {
		t.Fatalf("%s %s: %v", method, raw, err)
	}
	res, ok := got.(SettingsResult)
	if !ok {
		t.Fatalf("%s result: %#v", method, got)
	}
	return res
}

func TestSettingsSetSavesGlobalCapAndIdleMinutes(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	seedSetFixture(t, d)
	before, _ := registry.Load(d.registryPath())
	want := *before
	want.Projects = slices.Clone(before.Projects)

	res := settingsCall(t, d, MethodSettingsSet, `{"global_cap":6,"daemon_idle_minutes":15}`)
	want.GlobalCap, want.DaemonIdleMinutes = 6, 15
	after, err := registry.Load(d.registryPath())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*after, want) {
		t.Fatalf("both:\n got %+v\nwant %+v", *after, want)
	}
	if res != (SettingsResult{GlobalCap: 6, DaemonIdleMinutes: 15, EffectiveGlobalCap: 6, EffectiveDaemonIdleMinutes: 15}) {
		t.Fatalf("result %+v", res)
	}

	// One field alone leaves the other.
	settingsCall(t, d, MethodSettingsSet, `{"global_cap":3}`)
	want.GlobalCap = 3
	if after, _ = registry.Load(d.registryPath()); !reflect.DeepEqual(*after, want) {
		t.Fatalf("one field:\n got %+v\nwant %+v", *after, want)
	}

	// An unfinished run does not block a settings change.
	if _, err := store.Create(runrec.Record{ID: "a-run", Project: p.Name, State: runrec.StatePaused}); err != nil {
		t.Fatal(err)
	}
	settingsCall(t, d, MethodSettingsSet, `{"daemon_idle_minutes":20}`)
	want.DaemonIdleMinutes = 20
	if after, _ = registry.Load(d.registryPath()); !reflect.DeepEqual(*after, want) {
		t.Fatalf("with a run:\n got %+v\nwant %+v", *after, want)
	}

	// 0 clears.
	res = settingsCall(t, d, MethodSettingsSet, `{"global_cap":0,"daemon_idle_minutes":0}`)
	want.GlobalCap, want.DaemonIdleMinutes = 0, 0
	if after, _ = registry.Load(d.registryPath()); !reflect.DeepEqual(*after, want) {
		t.Fatalf("clear:\n got %+v\nwant %+v", *after, want)
	}
	if res != (SettingsResult{EffectiveGlobalCap: 4, EffectiveDaemonIdleMinutes: 10}) {
		t.Fatalf("cleared result %+v", res)
	}
	raw, _ := os.ReadFile(d.registryPath())
	if strings.Contains(string(raw), "global_cap") || strings.Contains(string(raw), "daemon_idle_minutes") {
		t.Fatalf("a cleared value was written:\n%s", raw)
	}
}

func TestSettingsSetRefusesBadCalls(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	seedSetFixture(t, d)
	before, _ := os.ReadFile(d.registryPath())
	for raw, want := range map[string]string{
		`{}`:                         "sets no field",
		`{"global_cap":-1}`:          "global_cap",
		`{"daemon_idle_minutes":-2}`: "daemon_idle_minutes",
		`{"global_cap":2,"daemon_idle_minutes":-2}`: "daemon_idle_minutes",
		`{"cap":2}`: "unknown field",
	} {
		_, err := d.handle(context.Background(), MethodSettingsSet, json.RawMessage(raw))
		var re *ipc.Error
		if !errors.As(err, &re) || re.Code != CodeBadParams || !strings.Contains(re.Message, want) {
			t.Errorf("%s: %v, want bad_params naming %q", raw, err, want)
		}
	}
	after, _ := os.ReadFile(d.registryPath())
	if string(after) != string(before) {
		t.Fatalf("a refusal changed the registry:\n%s", after)
	}
}

func TestSettingsGetReportsEffectiveValues(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	if got := settingsCall(t, d, MethodSettingsGet, ""); got != (SettingsResult{EffectiveGlobalCap: 4, EffectiveDaemonIdleMinutes: 10}) {
		t.Fatalf("unset: %+v", got)
	}
	f, _ := registry.Load(d.registryPath())
	f.GlobalCap, f.DaemonIdleMinutes = 6, 25
	if err := registry.Save(d.registryPath(), f); err != nil {
		t.Fatal(err)
	}
	if got := settingsCall(t, d, MethodSettingsGet, ""); got != (SettingsResult{GlobalCap: 6, DaemonIdleMinutes: 25, EffectiveGlobalCap: 6, EffectiveDaemonIdleMinutes: 25}) {
		t.Fatalf("set: %+v", got)
	}
	// The wire names are the registry's keys.
	b, _ := json.Marshal(settingsCall(t, d, MethodSettingsGet, ""))
	if string(b) != `{"global_cap":6,"daemon_idle_minutes":25,"effective_global_cap":6,"effective_daemon_idle_minutes":25}` {
		t.Fatalf("wire: %s", b)
	}
}

func TestSettingsOverTheSocket(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 7, registry.Project{Name: "keep", Path: "/p/keep", Cap: 2})
	e.start(t)
	c := e.dial(t)
	cap6 := 6
	got, err := c.SetSettings(context.Background(), SetSettingsParams{GlobalCap: &cap6})
	if err != nil || got != (SettingsResult{GlobalCap: 6, DaemonIdleMinutes: 7, EffectiveGlobalCap: 6, EffectiveDaemonIdleMinutes: 7}) {
		t.Fatalf("set: %+v, %v", got, err)
	}
	saved, _ := registry.Load(e.regPath)
	if saved.GlobalCap != 6 || saved.DaemonIdleMinutes != 7 || saved.Projects[0].Cap != 2 {
		t.Fatalf("saved: %+v", saved)
	}
	if got, err = c.Settings(context.Background()); err != nil || got.GlobalCap != 6 || got.EffectiveDaemonIdleMinutes != 7 {
		t.Fatalf("get: %+v, %v", got, err)
	}
}

func TestRaisingTheGlobalCapStartsQueuedRuns(t *testing.T) {
	w := newWaveRig(t, 1, 0)
	first := w.start(t, "S1")
	second := w.start(t, "S2")
	w.expectStart(t, "S1")
	if second.State != runrec.StateQueued {
		t.Fatalf("second run state %q, want queued", second.State)
	}
	w.expectNoStart(t)

	// Raising the cap starts the queued run without another event.
	settingsCall(t, w.d, MethodSettingsSet, `{"global_cap":2}`)
	w.expectStart(t, "S2")

	// Lowering the cap stops nothing, and the lower cap holds a new run.
	settingsCall(t, w.d, MethodSettingsSet, `{"global_cap":1}`)
	third := w.start(t, "S3")
	w.expectNoStart(t)
	if third.State != runrec.StateQueued {
		t.Fatalf("third run state %q, want queued under the lowered cap", third.State)
	}
	for _, id := range []string{first.ID, second.ID} {
		r, err := w.store.Open(id)
		if err != nil {
			t.Fatal(err)
		}
		if st := r.Snapshot().State; runrec.IsFinal(st) || st == runrec.StateQueued {
			t.Errorf("run %s state %q after the cap was lowered, want it still running", id, st)
		}
	}
}
