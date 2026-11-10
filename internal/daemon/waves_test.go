// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/wavestore"
)

// listAllP is what the fake slicer lists for project p with --all.
const listAllP = `[{"id":"S1","title":"One","status":"open"},{"id":"S2","title":"Two","status":"done"},` +
	`{"id":"S3","title":"Three","status":"retired"},{"id":"S4","title":"Four","status":"started"}]`

func listAllRoute(root, stdout string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"list", "--all", "--json", "--lean", "--root", root}, Stdout: stdout}
}

// listAllCalls is the slicer list calls the fake saw, as their root argument.
func listAllCalls(f *clitest.Fake) []string {
	var roots []string
	for _, c := range f.CallsTo("slicer") {
		if len(c.Args) > 0 && c.Args[0] == "list" {
			if c.Args[1] != "--all" {
				roots = append(roots, "NOT --all: "+strings.Join(c.Args, " "))
				continue
			}
			roots = append(roots, c.Args[len(c.Args)-1])
		}
	}
	return roots
}

func wavesPath(d *Daemon) string {
	return filepath.Join(filepath.Dir(d.cfg.RegistryPath), "waves.json")
}

func loadWaves(t *testing.T, d *Daemon) *wavestore.File {
	t.Helper()
	f, err := wavestore.Load(wavesPath(d))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func saveWaves(t *testing.T, d *Daemon, waves ...wavestore.Wave) {
	t.Helper()
	if err := wavestore.Save(wavesPath(d), &wavestore.File{Version: wavestore.Version, Waves: waves}); err != nil {
		t.Fatal(err)
	}
}

func savedWave(project, name string, items ...string) wavestore.Wave {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	return wavestore.Wave{Project: project, Name: name, Items: items, CreatedAt: at, UpdatedAt: at}
}

func callErr(t *testing.T, d *Daemon, method, raw string) *ipc.Error {
	t.Helper()
	v, err := d.handle(context.Background(), method, json.RawMessage(raw))
	if err == nil {
		t.Fatalf("%s %s succeeded: %+v", method, raw, v)
	}
	return ipc.ErrorOf(err)
}

// addProjectQ registers q beside p, in the registry runDaemonIn made.
func addProjectQ(t *testing.T, d *Daemon, root string) registry.Project {
	t.Helper()
	f, err := registry.Load(d.cfg.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	q := registry.Project{Name: "q", Path: filepath.Join(root, "q"), CheckCommand: "go test ./..."}
	f.Projects = append(f.Projects, q)
	if err := registry.Save(d.cfg.RegistryPath, f); err != nil {
		t.Fatal(err)
	}
	return q
}

func TestWaveDefineStoresNameAndItemsInOrder(t *testing.T) {
	root := t.TempDir()
	d, _, p := runDaemonIn(t, root, nil, listAllRoute(filepath.Join(root, "p"), listAllP))
	before := time.Now().UTC().Add(-time.Second)
	var got SavedWave
	if err := json.Unmarshal(request(t, d, "wave.define", `{"project":"p","name":"pair-a","items":["S4","S1"]}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Project != "p" || got.Name != "pair-a" || !reflect.DeepEqual(got.Items, []string{"S4", "S1"}) {
		t.Fatalf("result: %+v", got)
	}
	if got.CreatedAt.Before(before) || got.CreatedAt.After(time.Now().Add(time.Second)) || !got.UpdatedAt.Equal(got.CreatedAt) {
		t.Fatalf("times: %+v", got)
	}
	// The file sits beside the registry, with the owner's mode (NW-Q5).
	path := filepath.Join(filepath.Dir(d.cfg.RegistryPath), "waves.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("%s: %v, %v", path, info, err)
	}
	f := loadWaves(t, d)
	if len(f.Waves) != 1 || !reflect.DeepEqual(f.Waves[0].Items, []string{"S4", "S1"}) || f.Waves[0].Project != p.Name {
		t.Fatalf("file: %+v", f)
	}
	// The registry is untouched.
	if reg, err := registry.Load(d.cfg.RegistryPath); err != nil || len(reg.Projects) != 1 {
		t.Fatalf("registry: %+v, %v", reg, err)
	}
}

func TestWaveDefineDuplicateNameFollowsNWQ3(t *testing.T) {
	root := t.TempDir()
	d, _, _ := runDaemonIn(t, root, nil, listAllRoute(filepath.Join(root, "p"), listAllP))
	var first SavedWave
	if err := json.Unmarshal(request(t, d, "wave.define", `{"project":"p","name":"pair-a","items":["S1","S4"]}`), &first); err != nil {
		t.Fatal(err)
	}
	request(t, d, "wave.define", `{"project":"p","name":"other","items":["S1"]}`)
	before, err := os.ReadFile(wavesPath(d))
	if err != nil {
		t.Fatal(err)
	}
	e := callErr(t, d, "wave.define", `{"project":"p","name":"pair-a","items":["S2"]}`)
	if e.Code != CodeDuplicateName || !strings.Contains(e.Message, "pair-a") || !strings.Contains(e.Message, "--replace") {
		t.Fatalf("refusal: %+v", e)
	}
	if after, err := os.ReadFile(wavesPath(d)); err != nil || string(after) != string(before) {
		t.Fatalf("refused define changed the file: %v", err)
	}
	// replace:false is the same as no replace.
	if e := callErr(t, d, "wave.define", `{"project":"p","name":"pair-a","items":["S2"],"replace":false}`); e.Code != CodeDuplicateName {
		t.Fatalf("replace false: %+v", e)
	}
	var replaced SavedWave
	if err := json.Unmarshal(request(t, d, "wave.define", `{"project":"p","name":"pair-a","items":["S2","S3"],"replace":true}`), &replaced); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replaced.Items, []string{"S2", "S3"}) || !replaced.CreatedAt.Equal(first.CreatedAt) || replaced.UpdatedAt.Before(first.UpdatedAt) {
		t.Fatalf("replaced: %+v (first %+v)", replaced, first)
	}
	f := loadWaves(t, d)
	if len(f.Waves) != 2 || f.Waves[0].Name != "pair-a" || !reflect.DeepEqual(f.Waves[0].Items, []string{"S2", "S3"}) || f.Waves[1].Name != "other" {
		t.Fatalf("file after replace: %+v", f.Waves)
	}
}

func TestWaveDefineItemRulesFollowNWQ4(t *testing.T) {
	root := t.TempDir()
	d, _, _ := runDaemonIn(t, root, nil, listAllRoute(filepath.Join(root, "p"), listAllP))
	over := make([]string, wavestore.MaxItems+1)
	for i := range over {
		over[i] = fmt.Sprintf("S%d", i+1)
	}
	overJSON, _ := json.Marshal(over)
	cases := []struct{ name, raw, want string }{
		{"unknown item", `{"project":"p","name":"a","items":["S1","S99"]}`, "S99"},
		{"repeated item", `{"project":"p","name":"a","items":["S1","S4","S1"]}`, "S1"},
		{"no items", `{"project":"p","name":"a","items":[]}`, "item"},
		{"missing items", `{"project":"p","name":"a"}`, "item"},
		{"too many items", `{"project":"p","name":"a","items":` + string(overJSON) + `}`, "20"},
		{"bad name", `{"project":"p","name":"Pair A","items":["S1"]}`, "Pair A"},
		{"wave prefix", `{"project":"p","name":"wave-1","items":["S1"]}`, "wave-"},
		{"empty name", `{"project":"p","name":"","items":["S1"]}`, "name"},
		{"unknown key", `{"project":"p","name":"a","items":["S1"],"status":"x"}`, "status"},
	}
	for _, c := range cases {
		e := callErr(t, d, "wave.define", c.raw)
		if e.Code != CodeBadParams || !strings.Contains(e.Message, c.want) {
			t.Errorf("%s: %+v, want bad_params naming %q", c.name, e, c.want)
		}
	}
	if _, err := os.Stat(wavesPath(d)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused define wrote the file: %v", err)
	}
	// A done and a retired item is known to slicer, so it can be saved (NW-Q4).
	var got SavedWave
	if err := json.Unmarshal(request(t, d, "wave.define", `{"project":"p","name":"ok","items":["S2","S3"]}`), &got); err != nil || len(got.Items) != 2 {
		t.Fatalf("done and retired items: %+v, %v", got, err)
	}
	// Every refusal above that reads slicer used --all: the unknown item and
	// this define each made one list call.
	for _, call := range listAllCalls(lastFake) {
		if strings.HasPrefix(call, "NOT --all") {
			t.Fatalf("a define listed without --all: %s", call)
		}
	}
}

func TestWaveDefineUnknownProjectIsNotFound(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	e := callErr(t, d, "wave.define", `{"project":"nope","name":"a","items":["S1"]}`)
	if e.Code != CodeNotFound || e.Message != `no project named "nope"` {
		t.Fatalf("unknown project: %+v", e)
	}
	if calls := listAllCalls(lastFake); len(calls) != 0 {
		t.Fatalf("slicer was asked about an unregistered project: %v", calls)
	}
	// With no project registered, the message is the registry guide.
	if err := registry.Save(d.cfg.RegistryPath, &registry.File{Version: registry.Version, Projects: []registry.Project{}}); err != nil {
		t.Fatal(err)
	}
	e = callErr(t, d, "wave.define", `{"project":"p","name":"a","items":["S1"]}`)
	if e.Code != CodeNotFound || e.Message != NoProjectsGuide {
		t.Fatalf("empty registry: %+v", e)
	}
	if _, err := os.Stat(wavesPath(d)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused define wrote the file: %v", err)
	}
}

func TestWaveListReadsStatusFromSlicerWithOneListPerProject(t *testing.T) {
	root := t.TempDir()
	d, _, _ := runDaemonIn(t, root, nil,
		listAllRoute(filepath.Join(root, "p"), listAllP),
		listAllRoute(filepath.Join(root, "q"), `[{"id":"Q1","title":"Quick","status":"open"}]`))
	addProjectQ(t, d, root)
	// S9 is not in slicer's list: it was removed after the wave was saved.
	saveWaves(t, d,
		savedWave("p", "pair-a", "S1", "S2", "S9"),
		savedWave("q", "pair-a", "Q1"),
		savedWave("p", "pair-b", "S4", "S3", "S1"))
	var out WaveListResult
	if err := json.Unmarshal(request(t, d, "wave.list", `{}`), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Waves) != 3 {
		t.Fatalf("waves: %+v", out.Waves)
	}
	want := []SavedWaveView{
		{Project: "p", Name: "pair-a", Items: []WaveMember{{"S1", "open", "One"}, {"S2", "done", "Two"}, {"S9", "missing", ""}}},
		{Project: "q", Name: "pair-a", Items: []WaveMember{{"Q1", "open", "Quick"}}},
		{Project: "p", Name: "pair-b", Items: []WaveMember{{"S4", "started", "Four"}, {"S3", "retired", "Three"}, {"S1", "open", "One"}}},
	}
	for i, w := range out.Waves {
		if w.Project != want[i].Project || w.Name != want[i].Name || !reflect.DeepEqual(w.Items, want[i].Items) || w.Error != nil {
			t.Errorf("wave %d: %+v, want %+v", i, w, want[i])
		}
		if w.CreatedAt.IsZero() || w.UpdatedAt.IsZero() {
			t.Errorf("wave %d: no times: %+v", i, w)
		}
	}
	// Seven members, two projects: one list call each.
	calls := listAllCalls(lastFake)
	if len(calls) != 2 || calls[0] == calls[1] {
		t.Fatalf("list calls: %v", calls)
	}
	// Nothing was written back to the file.
	if f := loadWaves(t, d); !reflect.DeepEqual(f.Waves[0].Items, []string{"S1", "S2", "S9"}) {
		t.Fatalf("file: %+v", f.Waves)
	}
}

func TestWaveListReportsASlicerErrorOnThatProjectsWavesOnly(t *testing.T) {
	root := t.TempDir()
	d, _, _ := runDaemonIn(t, root, nil,
		listAllRoute(filepath.Join(root, "p"), listAllP),
		clitest.Route{Tool: "slicer", Args: []string{"list", "--all", "--json", "--lean", "--root", filepath.Join(root, "q")},
			Stdout: `{"error":{"code":"no_root","message":"no slicer root here"}}`, Exit: 2})
	addProjectQ(t, d, root)
	saveWaves(t, d, savedWave("q", "broken", "Q1"), savedWave("p", "fine", "S1"))
	var out WaveListResult
	if err := json.Unmarshal(request(t, d, "wave.list", `{}`), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Waves) != 2 {
		t.Fatalf("waves: %+v", out.Waves)
	}
	broken, fine := out.Waves[0], out.Waves[1]
	if broken.Error == nil || broken.Error.Code != "no_root" || !strings.Contains(broken.Error.Message, "no slicer root") {
		t.Fatalf("broken: %+v", broken)
	}
	if len(broken.Items) != 1 || broken.Items[0].Item != "Q1" || broken.Items[0].Status != "" {
		t.Fatalf("broken members: %+v", broken.Items)
	}
	if fine.Error != nil || len(fine.Items) != 1 || fine.Items[0].Status != "open" {
		t.Fatalf("fine: %+v", fine)
	}
}

func TestWaveListFiltersByProject(t *testing.T) {
	root := t.TempDir()
	d, _, _ := runDaemonIn(t, root, nil,
		listAllRoute(filepath.Join(root, "p"), listAllP),
		listAllRoute(filepath.Join(root, "q"), `[{"id":"Q1","title":"Quick","status":"open"}]`))
	addProjectQ(t, d, root)
	saveWaves(t, d, savedWave("p", "a", "S1"), savedWave("q", "b", "Q1"), savedWave("p", "c", "S4"))
	var out WaveListResult
	if err := json.Unmarshal(request(t, d, "wave.list", `{"project":"q"}`), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Waves) != 1 || out.Waves[0].Name != "b" || out.Waves[0].Project != "q" {
		t.Fatalf("q: %+v", out.Waves)
	}
	if calls := listAllCalls(lastFake); len(calls) != 1 || !strings.HasSuffix(calls[0], "q") {
		t.Fatalf("a filtered list asked slicer about other projects: %v", calls)
	}
	// A project with no saved wave lists none, and is not an error.
	f, err := registry.Load(d.cfg.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	f.Projects = append(f.Projects, registry.Project{Name: "r", Path: filepath.Join(root, "r")})
	if err := registry.Save(d.cfg.RegistryPath, f); err != nil {
		t.Fatal(err)
	}
	out = WaveListResult{}
	if err := json.Unmarshal(request(t, d, "wave.list", `{"project":"r"}`), &out); err != nil || out.Waves == nil || len(out.Waves) != 0 {
		t.Fatalf("r: %+v, %v", out, err)
	}
	if e := callErr(t, d, "wave.list", `{"project":"nope"}`); e.Code != CodeNotFound {
		t.Fatalf("unknown project: %+v", e)
	}
	// With no file at all, the list is empty and creates nothing.
	empty, _, _ := runDaemon(t, nil)
	out = WaveListResult{}
	if err := json.Unmarshal(request(t, empty, "wave.list", `{}`), &out); err != nil || out.Waves == nil || len(out.Waves) != 0 {
		t.Fatalf("empty: %+v, %v", out, err)
	}
	if _, err := os.Stat(wavesPath(empty)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wave.list created the file: %v", err)
	}
}

func TestWaveRmRemovesOnlyThatProjectsWave(t *testing.T) {
	root := t.TempDir()
	d, _, _ := runDaemonIn(t, root, nil)
	addProjectQ(t, d, root)
	// The other project's wave of the same name comes first in the file.
	saveWaves(t, d, savedWave("q", "a", "Q1"), savedWave("p", "a", "S1"), savedWave("p", "b", "S4"))
	var removed SavedWave
	if err := json.Unmarshal(request(t, d, "wave.rm", `{"project":"p","name":"a"}`), &removed); err != nil {
		t.Fatal(err)
	}
	if removed.Project != "p" || removed.Name != "a" || !reflect.DeepEqual(removed.Items, []string{"S1"}) {
		t.Fatalf("removed: %+v", removed)
	}
	f := loadWaves(t, d)
	if len(f.Waves) != 2 || f.Waves[0].Project != "q" || f.Waves[0].Name != "a" || f.Waves[1].Name != "b" {
		t.Fatalf("file: %+v", f.Waves)
	}
	info, err := os.Stat(wavesPath(d))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v, %v", info, err)
	}
}

func TestWaveRmUnknownIsNotFound(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	e := callErr(t, d, "wave.rm", `{"project":"p","name":"a"}`)
	if e.Code != CodeNotFound || !strings.Contains(e.Message, `"a"`) || !strings.Contains(e.Message, "p") {
		t.Fatalf("no file: %+v", e)
	}
	if _, err := os.Stat(wavesPath(d)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused rm created the file: %v", err)
	}
	saveWaves(t, d, savedWave("q", "a", "Q1"))
	before, err := os.ReadFile(wavesPath(d))
	if err != nil {
		t.Fatal(err)
	}
	// The name exists, in another project.
	if e := callErr(t, d, "wave.rm", `{"project":"p","name":"a"}`); e.Code != CodeNotFound {
		t.Fatalf("other project's name: %+v", e)
	}
	if after, err := os.ReadFile(wavesPath(d)); err != nil || string(after) != string(before) {
		t.Fatalf("a refused rm changed the file: %v", err)
	}
	if e := callErr(t, d, "wave.rm", `{"project":"p"}`); e.Code != CodeBadParams {
		t.Fatalf("no name: %+v", e)
	}
}

func TestSavedWaveStartableSkipsDoneRetiredAndMissing(t *testing.T) {
	w := SavedWaveView{Project: "p", Name: "mix", Items: []WaveMember{
		{Item: "S5", Status: "open"},
		{Item: "S2", Status: "done"},
		{Item: "S4", Status: "started"},
		{Item: "S3", Status: "retired"},
		{Item: "S9", Status: "missing"},
		{Item: "S1", Status: "parked"},
	}}
	ids, skipped := w.Startable()
	if want := []string{"S5", "S4", "S1"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("startable %v, want %v", ids, want)
	}
	wantSkipped := []SkippedItem{{"S2", "done"}, {"S3", "retired"}, {"S9", "missing"}}
	if !reflect.DeepEqual(skipped, wantSkipped) {
		t.Fatalf("skipped %v, want %v", skipped, wantSkipped)
	}
	// Nothing to start: both lists are non-nil, so JSON shows [] and not null.
	ids, skipped = SavedWaveView{Items: []WaveMember{{Item: "S2", Status: "done"}}}.Startable()
	if ids == nil || len(ids) != 0 || len(skipped) != 1 {
		t.Fatalf("all done: %v %v", ids, skipped)
	}
	ids, skipped = SavedWaveView{}.Startable()
	if ids == nil || skipped == nil || len(ids) != 0 || len(skipped) != 0 {
		t.Fatalf("empty: %#v %#v", ids, skipped)
	}
	// A wave whose statuses could not be read has none to trust: nothing starts.
	broken := SavedWaveView{Error: &ipc.Error{Code: "no_root", Message: "x"}, Items: []WaveMember{{Item: "S1"}, {Item: "S2"}}}
	ids, skipped = broken.Startable()
	if len(ids) != 0 || len(skipped) != 2 || skipped[0] != (SkippedItem{"S1", "unknown"}) {
		t.Fatalf("broken: %v %v", ids, skipped)
	}
}

func TestWaveStartWithNameStampsWaveNameOnEveryRun(t *testing.T) {
	w := newWaveRig(t, 4, 0)
	saveWaves(t, w.d, savedWave("p", "pair-a", "S1", "S2", "S3"))
	var result struct {
		Wave  string `json:"wave"`
		Name  string `json:"name"`
		Items []struct {
			Run *runrec.Record `json:"run"`
		} `json:"items"`
	}
	b := request(t, w.d, "wave.start", `{"name":"pair-a","items":[{"project":"p","item":"S1"},{"project":"p","item":"S2"}]}`)
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "pair-a" || !strings.HasPrefix(result.Wave, "wave-") || len(result.Items) != 2 {
		t.Fatalf("result: %s", b)
	}
	for _, item := range result.Items {
		if item.Run == nil || item.Run.WaveName != "pair-a" || item.Run.Wave != result.Wave {
			t.Fatalf("run: %+v", item.Run)
		}
		disk, err := w.store.Open(item.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if rec := disk.Snapshot(); rec.WaveName != "pair-a" || rec.Wave != result.Wave {
			t.Fatalf("disk: %+v", rec)
		}
	}
	w.expectStarts(t, "S1", "S2")
	// The name is a label: a second start of the same name is a new wave id.
	var again struct {
		Wave string `json:"wave"`
	}
	if err := json.Unmarshal(request(t, w.d, "wave.start", `{"name":"pair-a","items":[{"project":"p","item":"S3"}]}`), &again); err != nil {
		t.Fatal(err)
	}
	if again.Wave == "" || again.Wave == result.Wave {
		t.Fatalf("wave ids: %q then %q", result.Wave, again.Wave)
	}
	w.expectStart(t, "S3")
}

func TestWaveStartWithoutNameLeavesWaveNameEmpty(t *testing.T) {
	w := newWaveRig(t, 4, 0)
	saveWaves(t, w.d, savedWave("p", "pair-a", "S1", "S2"))
	b := request(t, w.d, "wave.start", `{"items":[{"project":"p","item":"S1"},{"project":"p","item":"S2"}]}`)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, has := raw["name"]; has {
		t.Fatalf("result names a wave that was not named: %s", b)
	}
	var result struct {
		Items []struct {
			Run *runrec.Record `json:"run"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &result); err != nil || len(result.Items) != 2 {
		t.Fatalf("result: %s %v", b, err)
	}
	for _, item := range result.Items {
		disk, err := w.store.Open(item.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(disk.Dir(), "run.json"))
		if err != nil || item.Run.WaveName != "" || disk.Snapshot().WaveName != "" || strings.Contains(string(data), "wave_name") {
			t.Fatalf("run: %+v disk %s (%v)", item.Run, data, err)
		}
	}
	w.expectStarts(t, "S1", "S2")
}

func TestWaveStartNameMustBeSavedAndItemsMustBeMembers(t *testing.T) {
	w := newWaveRig(t, 4, 0)
	saveWaves(t, w.d, savedWave("p", "pair-a", "S1", "S2"), savedWave("q", "elsewhere", "S1"))
	cases := []struct{ name, raw, code string }{
		{"unsaved name", `{"name":"nope","items":[{"project":"p","item":"S1"}]}`, CodeNotFound},
		{"name of another project", `{"name":"elsewhere","items":[{"project":"p","item":"S1"}]}`, CodeNotFound},
		{"item that is not a member", `{"name":"pair-a","items":[{"project":"p","item":"S1"},{"project":"p","item":"S3"}]}`, CodeBadParams},
	}
	for _, c := range cases {
		e := callErr(t, w.d, "wave.start", c.raw)
		if e.Code != c.code || !strings.Contains(e.Message, "pair-a") && !strings.Contains(e.Message, "nope") && !strings.Contains(e.Message, "elsewhere") {
			t.Errorf("%s: %+v, want %s", c.name, e, c.code)
		}
		if c.code == CodeBadParams && !strings.Contains(e.Message, "S3") {
			t.Errorf("%s: the refusal does not name the item: %+v", c.name, e)
		}
	}
	runs, err := w.store.List()
	if err != nil || len(runs) != 0 {
		t.Fatalf("a refusal admitted runs: %v, %v", runs, err)
	}
	w.expectNoStart(t)
	// The checks come before anything is read from slicer.
	for _, call := range w.fake.CallsTo("slicer") {
		if len(call.Args) > 0 && (call.Args[0] == "deps" || call.Args[0] == "show") {
			t.Fatalf("a refused start read slicer: %v", call.Args)
		}
	}
	// A subset of the members is fine: done members are left out by the caller.
	b := request(t, w.d, "wave.start", `{"name":"pair-a","items":[{"project":"p","item":"S2"}]}`)
	if !strings.Contains(string(b), `"name":"pair-a"`) {
		t.Fatalf("subset start: %s", b)
	}
	w.expectStart(t, "S2")
}

func TestRemoveProjectKeepsOrDropsSavedWavesPerNWQ6(t *testing.T) {
	// NW-Q6, owner 2026-11-07: projects rm drops the project's saved waves.
	root := t.TempDir()
	d, store, _ := runDaemonIn(t, root, nil)
	addProjectQ(t, d, root)
	saveWaves(t, d, savedWave("p", "a", "S1"), savedWave("q", "a", "Q1"), savedWave("p", "b", "S4"))

	// A refused removal keeps them.
	if _, err := store.Create(runrec.Record{ID: "busy", Project: "p", Item: "S1", State: runrec.StateImplement}); err != nil {
		t.Fatal(err)
	}
	if e := callErr(t, d, MethodProjectsRm, `{"name":"p"}`); e.Code != CodeProjectBusy {
		t.Fatalf("busy: %+v", e)
	}
	if f := loadWaves(t, d); len(f.Waves) != 3 {
		t.Fatalf("a refused projects rm dropped waves: %+v", f.Waves)
	}
	run, err := store.Open("busy")
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Update(func(r *runrec.Record) { r.State = runrec.StateStopped }); err != nil {
		t.Fatal(err)
	}

	request(t, d, MethodProjectsRm, `{"name":"p"}`)
	f := loadWaves(t, d)
	if len(f.Waves) != 1 || f.Waves[0].Project != "q" || f.Waves[0].Name != "a" {
		t.Fatalf("after projects rm: %+v", f.Waves)
	}
	// Registering the project again does not bring them back.
	if err := os.MkdirAll(filepath.Join(root, "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	request(t, d, MethodProjectsAdd, `{"path":"`+filepath.Join(root, "p")+`","name":"p"}`)
	if f := loadWaves(t, d); len(f.Waves) != 1 {
		t.Fatalf("after projects add: %+v", f.Waves)
	}

	// A project with no saved wave removes without creating the file.
	d2, _, _ := runDaemon(t, nil)
	request(t, d2, MethodProjectsRm, `{"name":"p"}`)
	if _, err := os.Stat(wavesPath(d2)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("projects rm created waves.json: %v", err)
	}
}

func TestWaveMethodsRefuseConcurrentWritersCleanly(t *testing.T) {
	root := t.TempDir()
	d, _, _ := runDaemonIn(t, root, nil, listAllRoute(filepath.Join(root, "p"), listAllP))
	// Hold every writer between its read and its write, so that writers that
	// share no lock would all read the same file.
	orig := waveWriteHook
	t.Cleanup(func() { waveWriteHook = orig })
	waveWriteHook = func() { time.Sleep(15 * time.Millisecond) }
	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers*2)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw := fmt.Sprintf(`{"project":"p","name":"w%d","items":["S1","S4"]}`, i)
			if _, err := d.handle(context.Background(), "wave.define", json.RawMessage(raw)); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("define: %v", err)
	}
	f := loadWaves(t, d)
	if len(f.Waves) != writers {
		t.Fatalf("%d waves survived of %d: %+v", len(f.Waves), writers, f.Waves)
	}
	// A define and a rm at once also both land.
	var wg2 sync.WaitGroup
	for _, call := range []struct{ method, raw string }{
		{"wave.rm", `{"project":"p","name":"w0"}`},
		{"wave.define", `{"project":"p","name":"extra","items":["S1"]}`},
		{"wave.rm", `{"project":"p","name":"w1"}`},
	} {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			if _, err := d.handle(context.Background(), call.method, json.RawMessage(call.raw)); err != nil {
				t.Errorf("%s: %v", call.method, err)
			}
		}()
	}
	wg2.Wait()
	f = loadWaves(t, d)
	names := map[string]bool{}
	for _, w := range f.Waves {
		names[w.Name] = true
	}
	if len(f.Waves) != writers-2+1 || names["w0"] || names["w1"] || !names["extra"] {
		t.Fatalf("after define and rm: %+v", f.Waves)
	}
	entries, err := os.ReadDir(filepath.Dir(wavesPath(d)))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temporary file left behind: %s", e.Name())
		}
	}
}
