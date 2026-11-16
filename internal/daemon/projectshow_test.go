// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// showConfig is what `slicer config --json` prints for the tests: compact, so
// that the wire carries it byte for byte.
const showConfig = `{"check_command":"go test ./...","implement_finish":"done","relay_contract":11}`

func configRoute(root, stdout string, exit int) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"config", "--json", "--root", root}, Stdout: stdout, Exit: exit}
}

func matrixRouteAt(root, stdout string, exit int) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "show", "--json", "--root", root}, Stdout: stdout, Exit: exit}
}

// showDaemon is a daemon whose registry holds the project p and the file-level
// settings that set gives. It returns the daemon and the registry's path. The
// fake slicer is lastFake.
func showDaemon(t *testing.T, p registry.Project, set func(*registry.File), routes ...clitest.Route) (*Daemon, string) {
	t.Helper()
	dir := t.TempDir()
	reg := filepath.Join(dir, "projects.json")
	f := &registry.File{Version: registry.Version, Projects: []registry.Project{p}}
	if set != nil {
		set(f)
	}
	if err := registry.Save(reg, f); err != nil {
		t.Fatal(err)
	}
	fake := clitest.Install(t, routes...)
	lastFake = fake
	deps := engine.Deps{Slicer: slicercli.Client{Bin: fake.Bin("slicer")}}
	d := New(Config{Paths: PathsIn(filepath.Join(dir, "state")), RegistryPath: reg, Source: &stubSource{}, Engine: &deps})
	t.Cleanup(d.cache.Close)
	t.Cleanup(d.closeRuns)
	return d, reg
}

// showRaw calls projects.show and returns the JSON the wire carries.
func showRaw(t *testing.T, d *Daemon, name string) []byte {
	t.Helper()
	return request(t, d, "projects.show", `{"name":"`+name+`"}`)
}

func decodeShow(t *testing.T, raw []byte) ProjectsShowResult {
	t.Helper()
	var got ProjectsShowResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return got
}

func TestProjectsShowGathersEveryPart(t *testing.T) {
	proj := registry.Project{
		Name: "p", Path: waveRoot, Adj: true, CheckCommand: "go test ./...",
		VendorEnv: map[string]map[string]string{"claude": {"K": "secret-value"}},
	}
	d, _ := showDaemon(t, proj, func(f *registry.File) { f.GlobalCap, f.Owner = 3, "jon" },
		configRoute(waveRoot, showConfig, 0), matrixRouteAt(waveRoot, matrixShow(t), 0))
	raw := showRaw(t, d, "p")
	got := decodeShow(t, raw)

	if !reflect.DeepEqual(got.Project, proj) {
		t.Errorf("project %+v, want the registry entry %+v", got.Project, proj)
	}
	if got.Global.GlobalCap != 3 || got.Global.EffectiveGlobalCap != 3 || got.Global.Owner != "jon" || !got.Global.FileAnswers {
		t.Errorf("global %+v, want settings.get's result", got.Global)
	}
	if string(got.Slicer.Config) != showConfig {
		t.Errorf("config %s, want slicer's object %s", got.Slicer.Config, showConfig)
	}
	if got.Slicer.Matrix == nil || len(got.Slicer.Matrix.Rows) != 9 || len(got.Slicer.Matrix.Rules) != 2 {
		t.Fatalf("matrix %+v", got.Slicer.Matrix)
	}
	rows := got.Slicer.Matrix.Rows
	if want := shownRow("r1", "implementer", "Claude Code", "used as claude"); !reflect.DeepEqual(rows[0], want) {
		t.Errorf("row r1 %+v, want %+v", rows[0], want)
	}
	if want := shownRow("r14", "reviewer", "Claude Code", "UI pass row, used as claude"); !reflect.DeepEqual(rows[4], want) {
		t.Errorf("row r14 %+v, want %+v", rows[4], want)
	}
	if want := shownRow("r10", "orchestrator", "Claude Code", "not run by ork"); !reflect.DeepEqual(rows[8], want) {
		t.Errorf("row r10 %+v, want %+v", rows[8], want)
	}
	if got.Slicer.Errors == nil || len(got.Slicer.Errors) != 0 {
		t.Errorf("errors %+v, want an empty list", got.Slicer.Errors)
	}
	if !got.Adj.Enabled || got.Adj.ConfigFile {
		t.Errorf("adj %+v, want enabled from the registry and no config file", got.Adj)
	}
	// --json prints vendor_env as projects list --json does (owner, 2026-11-16).
	if got.Project.VendorEnv["claude"]["K"] != "secret-value" {
		t.Errorf("vendor_env %+v", got.Project.VendorEnv)
	}
	// Keys come in the order of the slice's result shape.
	last := -1
	for _, key := range []string{`"project":{`, `"global":{`, `"slicer":{`, `"adj":{`} {
		i := strings.Index(string(raw), key)
		if i <= last {
			t.Errorf("key %s at %d is out of order in %s", key, i, raw)
		}
		last = i
	}
	if !strings.Contains(string(raw), `"errors":[]`) {
		t.Errorf("the wire carries no empty errors list: %s", raw)
	}
}

func TestProjectsShowKeepsSlicerErrorsPerPart(t *testing.T) {
	proj := registry.Project{Name: "p", Path: waveRoot}
	t.Run("no matrix", func(t *testing.T) {
		d, _ := showDaemon(t, proj, nil, configRoute(waveRoot, showConfig, 0),
			matrixRouteAt(waveRoot, `{"error":{"code":"no_relay_roles","message":"no relay roles file"}}`, 2))
		raw := showRaw(t, d, "p")
		got := decodeShow(t, raw)
		want := []PartError{{Part: "matrix", Code: "no_relay_roles", Message: "no relay roles file"}}
		if got.Slicer.Matrix != nil || !reflect.DeepEqual(got.Slicer.Errors, want) {
			t.Errorf("matrix %+v, errors %+v; want null and %+v", got.Slicer.Matrix, got.Slicer.Errors, want)
		}
		if string(got.Slicer.Config) != showConfig {
			t.Errorf("config %s, want it filled", got.Slicer.Config)
		}
		if !strings.Contains(string(raw), `"matrix":null`) {
			t.Errorf("the wire carries no matrix null: %s", raw)
		}
	})
	t.Run("no .slicer", func(t *testing.T) {
		d, _ := showDaemon(t, proj, nil,
			configRoute(waveRoot, `{"error":{"code":"no_slicer","message":"no .slicer directory"}}`, 2),
			matrixRouteAt(waveRoot, `{"error":{"code":"no_slicer","message":"no .slicer directory"}}`, 2))
		raw := showRaw(t, d, "p")
		got := decodeShow(t, raw)
		want := []PartError{
			{Part: "config", Code: "no_slicer", Message: "no .slicer directory"},
			{Part: "matrix", Code: "no_slicer", Message: "no .slicer directory"},
		}
		if string(got.Slicer.Config) != "null" || got.Slicer.Matrix != nil || !reflect.DeepEqual(got.Slicer.Errors, want) {
			t.Errorf("slicer %+v; want both parts null and %+v", got.Slicer, want)
		}
		if got.Project.Name != "p" {
			t.Errorf("project %+v, want the registry entry", got.Project)
		}
		if !strings.Contains(string(raw), `"config":null`) {
			t.Errorf("the wire carries no config null: %s", raw)
		}
	})
}

func TestProjectsShowStatsButNeverReadsTheAdjConfig(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(t *testing.T, file string)
		want  bool
	}{
		"none": {func(*testing.T, string) {}, false},
		"regular file, mode 0000": {func(t *testing.T, file string) {
			if err := os.WriteFile(file, []byte(`{"store":"decisions"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(file, 0); err != nil {
				t.Fatal(err)
			}
		}, true},
		"directory": {func(t *testing.T, file string) {
			if err := os.Mkdir(file, 0o700); err != nil {
				t.Fatal(err)
			}
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, filepath.Join(root, ".adjudicate.json"))
			// The registry flag says no adj, so enabled and config_file differ.
			d, _ := showDaemon(t, registry.Project{Name: "p", Path: root}, nil,
				configRoute(root, showConfig, 0), matrixRouteAt(root, matrixShow(t), 0))
			got := decodeShow(t, showRaw(t, d, "p"))
			if got.Adj.ConfigFile != tc.want || got.Adj.Enabled {
				t.Fatalf("adj %+v, want config_file %v and enabled from the registry (false)", got.Adj, tc.want)
			}
		})
	}
}

func TestProjectsShowCarriesTheMatrixRevisionWhenSlicerPrintsOne(t *testing.T) {
	show := func(extra string) string {
		return `{"relay_roles":"x","rows":[{"id":"r1","role":"implementer","agent":"Claude Code"}],"rules":[]` + extra + `}`
	}
	proj := registry.Project{Name: "p", Path: waveRoot}
	t.Run("printed", func(t *testing.T) {
		d, _ := showDaemon(t, proj, nil, configRoute(waveRoot, showConfig, 0), matrixRouteAt(waveRoot, show(`,"revision":"rev-7"`), 0))
		raw := showRaw(t, d, "p")
		if got := decodeShow(t, raw); got.Slicer.Matrix == nil || got.Slicer.Matrix.Revision != "rev-7" {
			t.Fatalf("matrix %+v, want revision rev-7", got.Slicer.Matrix)
		}
	})
	t.Run("absent", func(t *testing.T) {
		d, _ := showDaemon(t, proj, nil, configRoute(waveRoot, showConfig, 0), matrixRouteAt(waveRoot, show(``), 0))
		raw := showRaw(t, d, "p")
		if got := decodeShow(t, raw); got.Slicer.Matrix == nil || len(got.Slicer.Matrix.Rows) != 1 {
			t.Fatalf("matrix %+v", got.Slicer.Matrix)
		}
		if strings.Contains(string(raw), "revision") {
			t.Fatalf("a revision slicer did not print is on the wire: %s", raw)
		}
	})
}

func TestProjectsShowUnknownProject(t *testing.T) {
	d, _ := showDaemon(t, registry.Project{Name: "p", Path: waveRoot}, nil,
		configRoute(waveRoot, showConfig, 0), matrixRouteAt(waveRoot, matrixShow(t), 0))
	_, err := d.handle(context.Background(), "projects.show", json.RawMessage(`{"name":"nope"}`))
	if e := ipc.ErrorOf(err); e == nil || e.Code != CodeNotFound {
		t.Fatalf("error %v, want not_found", err)
	}
	if calls := lastFake.Calls(); len(calls) != 0 {
		t.Fatalf("a tool was called for an unregistered project: %+v", calls)
	}
}

func TestProjectsShowWritesNothing(t *testing.T) {
	root := t.TempDir()
	d, reg := showDaemon(t, registry.Project{Name: "p", Path: root}, nil,
		configRoute(root, showConfig, 0), matrixRouteAt(root, matrixShow(t), 0))
	before, err := os.ReadFile(reg)
	if err != nil {
		t.Fatal(err)
	}
	showRaw(t, d, "p")
	after, err := os.ReadFile(reg)
	if err != nil || string(after) != string(before) {
		t.Fatalf("registry changed (%v):\nbefore %s\nafter  %s", err, before, after)
	}
	calls := lastFake.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls %+v, want the two reads", calls)
	}
	var got [][]string
	for _, c := range calls {
		if c.Tool != "slicer" {
			t.Fatalf("call to %s: %+v", c.Tool, calls)
		}
		got = append(got, c.Args)
	}
	for _, want := range [][]string{{"config", "--json", "--root", root}, {"relay-roles", "show", "--json", "--root", root}} {
		found := false
		for _, args := range got {
			found = found || reflect.DeepEqual(args, want)
		}
		if !found {
			t.Errorf("no call %v in %v", want, got)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Errorf("the project folder changed: %v, %v", entries, err)
	}
}

// The client call over the socket returns what the handler returns, and the
// registry's error comes back with its code.
func TestProjectsShowOverTheSocket(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0, registry.Project{Name: "p", Path: waveRoot, Adj: true, CheckCommand: "make"})
	fake := clitest.Install(t, configRoute(waveRoot, showConfig, 0), matrixRouteAt(waveRoot, matrixShow(t), 0))
	e.deps = &engine.Deps{Slicer: slicercli.Client{Bin: fake.Bin("slicer")}}
	r := e.start(t)
	c := e.dial(t)

	got, err := c.ProjectsShow(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	direct, err := r.d.handle(context.Background(), "projects.show", json.RawMessage(`{"name":"p"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(direct)
	if err != nil {
		t.Fatal(err)
	}
	if want := decodeShow(t, b); !reflect.DeepEqual(*got, want) {
		t.Fatalf("over the socket\n got %+v\nwant %+v", *got, want)
	}
	if got.Project.CheckCommand != "make" || len(got.Slicer.Matrix.Rows) != 9 || string(got.Slicer.Config) != showConfig {
		t.Fatalf("result %+v", got)
	}
	var re *ipc.Error
	if _, err := c.ProjectsShow(context.Background(), "nope"); !errors.As(err, &re) || re.Code != CodeNotFound {
		t.Fatalf("unknown project: %v", err)
	}
}

// ProjectsShowParams refuses a key it does not name, as every call does.
func TestProjectsShowNeedsAName(t *testing.T) {
	d, _ := showDaemon(t, registry.Project{Name: "p", Path: waveRoot}, nil)
	for _, raw := range []string{`{}`, `{"name":""}`} {
		_, err := d.handle(context.Background(), "projects.show", json.RawMessage(raw))
		if e := ipc.ErrorOf(err); e == nil || e.Code != CodeBadParams {
			t.Errorf("%s: error %v, want bad_params", raw, err)
		}
	}
	if calls := lastFake.Calls(); len(calls) != 0 {
		t.Fatalf("a tool was called with no name: %+v", calls)
	}
}
