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

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

var setupCommands = map[string]string{"slicer-init": "slicer init", "relay-roles-init": "slicer relay-roles init --render", "adj-init": "adj init", "adj-off": "registry: adj off"}

func setupRequest(t *testing.T, d *Daemon, step string, preview bool) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"project": "p", "step": step, "preview": preview})
	var out map[string]any
	if err := json.Unmarshal(request(t, d, "project.setup", string(raw)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func setupRegistry(t *testing.T, d *Daemon) []byte {
	t.Helper()
	b, err := os.ReadFile(d.registryPath())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestProjectSetupPreviewRunsNothing(t *testing.T) {
	for step, command := range setupCommands {
		t.Run(step, func(t *testing.T) {
			d := readDaemon(t)
			before := setupRegistry(t, d)
			out := setupRequest(t, d, step, true)
			want := map[string]any{"command": command, "done": false}
			if step == "adj-init" || step == "adj-off" {
				want["adj"] = false
			}
			if !reflect.DeepEqual(out, want) {
				t.Fatalf("preview: %v, want %v", out, want)
			}
			if string(setupRegistry(t, d)) != string(before) {
				t.Fatal("preview changed registry")
			}
			if calls := lastFake.Calls(); len(calls) != 0 {
				t.Fatalf("preview called tools: %v", calls)
			}
		})
	}
}

func TestProjectSetupSlicerInitRuns(t *testing.T) {
	args := []string{"init", "--json", "--root", waveRoot}
	d := readDaemon(t, clitest.Route{Tool: "slicer", Args: args, Stdout: `{}`})
	out := setupRequest(t, d, "slicer-init", false)
	if !reflect.DeepEqual(out, map[string]any{"command": "slicer init", "done": true}) {
		t.Fatalf("result: %v", out)
	}
	if calls := lastFake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, args) {
		t.Fatalf("calls: %v", calls)
	}
}

func TestProjectSetupRelayRolesInitPassesSlicersRefusal(t *testing.T) {
	d := readDaemon(t, clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "init", "--render", "--json", "--root", waveRoot}, Exit: 2, Stdout: `{"error":{"code":"state","message":"matrix already exists"}}`})
	_, err := d.handle(context.Background(), "project.setup", json.RawMessage(`{"project":"p","step":"relay-roles-init"}`))
	if e := ipc.ErrorOf(err); err == nil || e.Code != "state" || e.Message != "matrix already exists" {
		t.Fatalf("refusal: %v", err)
	}
}

func TestProjectSetupAdjInitTurnsTheFlagOn(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			d := readDaemon(t)
			route := clitest.Route{Tool: "adj", Args: []string{"init", "--root", waveRoot}, Stdout: "initialized\n"}
			if fail {
				route.Exit = 1
				route.Stderr = "init failed\n"
			}
			fake := clitest.Install(t, route)
			d.runs.deps.Adj = adjcli.Client{Bin: fake.Bin("adj")}
			before := setupRegistry(t, d)
			v, err := d.handle(context.Background(), "project.setup", json.RawMessage(`{"project":"p","step":"adj-init"}`))
			if fail {
				if err == nil || !strings.Contains(err.Error(), "init failed") {
					t.Fatalf("error: %v", err)
				}
				if string(setupRegistry(t, d)) != string(before) {
					t.Fatal("failed init changed registry")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				b, _ := json.Marshal(v)
				if string(b) != `{"command":"adj init","done":true,"adj":true}` {
					t.Fatalf("result: %s", b)
				}
				p, err := d.registered("p")
				if err != nil || !p.Adj {
					t.Fatalf("registry: %+v %v", p, err)
				}
			}
			if calls := fake.CallsTo("adj"); len(calls) != 1 {
				t.Fatalf("calls: %v", calls)
			}
		})
	}
}

func TestProjectSetupAdjOffMakesNoAdjCall(t *testing.T) {
	d := readDaemon(t)
	f, _ := d.loadRegistry()
	f.Projects[0].Adj = true
	if err := registry.Save(d.registryPath(), f); err != nil {
		t.Fatal(err)
	}
	out := setupRequest(t, d, "adj-off", false)
	if !reflect.DeepEqual(out, map[string]any{"command": "registry: adj off", "done": true, "adj": false}) {
		t.Fatalf("result: %v", out)
	}
	p, err := d.registered("p")
	if err != nil || p.Adj {
		t.Fatalf("registry: %+v %v", p, err)
	}
	if calls := lastFake.Calls(); len(calls) != 0 {
		t.Fatalf("tools: %v", calls)
	}
}

func TestProjectSetupAdjStepsRefuseAnUnfinishedRun(t *testing.T) {
	for _, step := range []string{"adj-init", "adj-off", "slicer-init"} {
		t.Run(step, func(t *testing.T) {
			d, store, p := runDaemon(t, nil)
			if _, err := store.Create(runrec.Record{ID: "busy", Project: p.Name, State: runrec.StatePaused}); err != nil {
				t.Fatal(err)
			}
			fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"init", "--json", "--root", p.Path}, Stdout: `{}`})
			d.runs.deps.Slicer = slicercli.Client{Bin: fake.Bin("slicer")}
			d.runs.deps.Adj = adjcli.Client{Bin: fake.Bin("adj")}
			before := setupRegistry(t, d)
			raw, _ := json.Marshal(map[string]string{"project": "p", "step": step})
			_, err := d.handle(context.Background(), "project.setup", raw)
			if step == "slicer-init" {
				if err != nil {
					t.Fatal(err)
				}
			} else if e := ipc.ErrorOf(err); err == nil || e.Code != CodeProjectBusy {
				t.Fatalf("busy: %v", err)
			}
			if string(setupRegistry(t, d)) != string(before) {
				t.Fatal("changed registry")
			}
			if calls := fake.CallsTo("adj"); len(calls) != 0 {
				t.Fatalf("called adj: %v", calls)
			}
		})
	}
}

func TestProjectSetupRefusesBadRequests(t *testing.T) {
	for _, tc := range []struct{ raw, code string }{{`{"step":"slicer-init"}`, CodeBadParams}, {`{"project":"missing","step":"slicer-init"}`, CodeNotFound}, {`{"project":"p","step":"unknown"}`, CodeBadParams}, {`{"project":"p"}`, CodeBadParams}} {
		t.Run(tc.raw, func(t *testing.T) {
			d := readDaemon(t)
			_, err := d.handle(context.Background(), "project.setup", json.RawMessage(tc.raw))
			e := ipc.ErrorOf(err)
			if err == nil || e.Code != tc.code {
				t.Fatalf("bad request: %v", err)
			}
			if strings.Contains(tc.raw, `"unknown"`) {
				for step := range setupCommands {
					if !strings.Contains(e.Message, step) {
						t.Fatalf("missing step %s: %v", step, err)
					}
				}
			}
			if calls := lastFake.Calls(); len(calls) != 0 {
				t.Fatalf("called tools: %v", calls)
			}
		})
	}
}

func TestProjectSetupOverTheSocket(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0, registry.Project{Name: "p", Path: waveRoot})
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"init", "--json", "--root", waveRoot}, Stdout: `{}`})
	e.deps = &engine.Deps{Slicer: slicercli.Client{Bin: fake.Bin("slicer")}}
	e.start(t)
	c := e.dial(t)
	out, err := c.ProjectSetup(context.Background(), ProjectSetupParams{Project: "p", Step: "slicer-init"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Command != "slicer init" || !out.Done || out.Adj != nil {
		t.Fatalf("socket result: %v", out)
	}
}

func TestProjectSetupRefreshesCache(t *testing.T) {
	for _, step := range []string{"slicer-init", "relay-roles-init", "adj-init"} {
		t.Run(step, func(t *testing.T) {
			d := readDaemon(t)
			fake := clitest.Install(t, clitest.Route{Tool: "slicer", Prefix: true, Stdout: `{}`}, clitest.Route{Tool: "adj", Prefix: true, Stdout: "initialized\n"})
			d.runs.deps.Slicer = slicercli.Client{Bin: fake.Bin("slicer")}
			d.runs.deps.Adj = adjcli.Client{Bin: fake.Bin("adj")}
			proj, _ := d.registered("p")
			// Warm the cache with missing setup. Replacing the source alone must
			// not repair the cached error; the setup write must refresh it.
			src := d.cfg.Source.(*stubSource)
			src.snaps = map[string]source.Snapshot{"p": {Items: source.Result[[]slicercli.Item]{Err: fmt.Errorf("no slicer")}}}
			d.cache.Get(context.Background(), proj, source.Items)
			src.put("p", source.Snapshot{Items: source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S1", Title: "ready"}}}})
			setupRequest(t, d, step, false)
			got := d.cache.Get(context.Background(), proj, source.Items)
			if got.Err != nil || !reflect.DeepEqual(got.Value, []slicercli.Item{{ID: "S1", Title: "ready"}}) {
				t.Fatalf("stale setup cache: %+v", got)
			}
		})
	}
}
