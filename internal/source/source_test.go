// SPDX-License-Identifier: GPL-3.0-or-later

package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/squalor-xyz/ork/internal/runrec"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestMain(m *testing.M) {
	clitest.Main()
	os.Exit(m.Run())
}

const root = "/projects/ork"

func rec(tool, name string) string {
	return filepath.Join("..", "..", "testdata", "cli", tool, name)
}

func slicerRoutes() []clitest.Route {
	return []clitest.Route{
		{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", root}, File: rec("slicer", "list.json")},
		{Tool: "slicer", Args: []string{"goals", "--json", "--root", root}, File: rec("slicer", "goals.json")},
		{Tool: "slicer", Args: []string{"relay-run", "list", "--json", "--lean", "--active", "--root", root}, File: rec("slicer", "relay-run-list.json")},
		{Tool: "slicer", Args: []string{"deps", "--json", "--lean", "--root", root}, Stdout: `[{"id":"S90"}]`},
	}
}

func adjRoutes() []clitest.Route {
	return []clitest.Route{
		{Tool: "adj", Args: []string{"inbox", "--json", "--root", root}, File: rec("adj", "inbox-nonempty.json")},
		{Tool: "adj", Args: []string{"list", "--json", "--status", "decided", "--root", root}, File: rec("adj", "list.json")},
	}
}

func direct(fake *clitest.Fake) *Direct {
	return NewDirect(slicercli.Client{Bin: fake.Bin("slicer")}, adjcli.Client{Bin: fake.Bin("adj")})
}

func TestProjectWithoutAdjNeverCallsAdj(t *testing.T) {
	fake := clitest.Install(t, slicerRoutes()...) // no adj route: a call to adj would fail and be logged
	snap := direct(fake).Snapshot(context.Background(), registry.Project{Name: "DataBall", Path: root, Adj: false})

	if calls := fake.CallsTo("adj"); len(calls) != 0 {
		t.Fatalf("adj was called: %+v", calls)
	}
	if snap.Open.Err != nil || snap.Decided.Err != nil || len(snap.Open.Value) != 0 || len(snap.Decided.Value) != 0 {
		t.Fatalf("adj parts for an adj:false project: %+v %+v", snap.Open, snap.Decided)
	}
	if snap.Items.Err != nil || len(snap.Items.Value) != 11 {
		t.Fatalf("items: %v, %d", snap.Items.Err, len(snap.Items.Value))
	}
	if snap.Goals.Err != nil || snap.Goals.Value == nil || len(snap.Goals.Value.Records.Goal) == 0 {
		t.Fatalf("goals: %+v", snap.Goals)
	}
	if snap.Runs.Err != nil || len(snap.Runs.Value) != 3 {
		t.Fatalf("runs: %v, %d", snap.Runs.Err, len(snap.Runs.Value))
	}
	if snap.Project.Name != "DataBall" {
		t.Fatalf("project: %+v", snap.Project)
	}
}

func TestProjectWithAdjReadsOpenAndDecided(t *testing.T) {
	fake := clitest.Install(t, append(slicerRoutes(), adjRoutes()...)...)
	snap := direct(fake).Snapshot(context.Background(), registry.Project{Name: "ork", Path: root, Adj: true})

	if snap.Open.Err != nil || len(snap.Open.Value) != 2 || snap.Open.Value[0].Key != "ui.slicer-tui" {
		t.Fatalf("open: %v, %+v", snap.Open.Err, snap.Open.Value)
	}
	if snap.Decided.Err != nil || len(snap.Decided.Value) != 55 {
		t.Fatalf("decided: %v, %d", snap.Decided.Err, len(snap.Decided.Value))
	}
	if n := len(fake.CallsTo("adj")); n != 2 {
		t.Fatalf("adj calls: %d", n)
	}
}

func TestOneFailedCallDoesNotBlankTheOthers(t *testing.T) {
	routes := append(slicerRoutes(), adjRoutes()...)
	routes[1] = clitest.Route{Tool: "slicer", Args: []string{"goals", "--json", "--root", root},
		File: rec("slicer", "error-no-such-item.json"), Exit: 2} // goals fails
	routes[5] = clitest.Route{Tool: "adj", Args: []string{"list", "--json", "--status", "decided", "--root", root},
		Stderr: "adj list: boom\n", Exit: 1} // decided fails
	fake := clitest.Install(t, routes...)
	snap := direct(fake).Snapshot(context.Background(), registry.Project{Name: "ork", Path: root, Adj: true})

	var te *clirun.ToolError
	if !errors.As(snap.Goals.Err, &te) || te.Code != "no_such_item" || snap.Goals.Value != nil {
		t.Fatalf("goals: %+v", snap.Goals)
	}
	var ee *clirun.ExitError
	if !errors.As(snap.Decided.Err, &ee) || ee.Result.ExitCode != 1 {
		t.Fatalf("decided: %+v", snap.Decided)
	}
	if snap.Items.Err != nil || len(snap.Items.Value) != 11 {
		t.Fatalf("items blanked: %v, %d", snap.Items.Err, len(snap.Items.Value))
	}
	if snap.Runs.Err != nil || len(snap.Runs.Value) != 3 {
		t.Fatalf("runs blanked: %v, %d", snap.Runs.Err, len(snap.Runs.Value))
	}
	if snap.Open.Err != nil || len(snap.Open.Value) != 2 {
		t.Fatalf("open blanked: %v, %d", snap.Open.Err, len(snap.Open.Value))
	}
}

func TestDirectIsASource(t *testing.T) {
	var _ Source = (*Direct)(nil)
}

// barrierScript is a fake CLI that exits only once five calls are running at
// the same time, so a Snapshot that makes its calls one after another fails.
const barrierScript = `#!/bin/sh
d="$ORK_BARRIER"
touch "$d/$$"
i=0
while [ "$(ls "$d" | wc -l)" -lt 5 ]; do
  i=$((i+1))
  if [ $i -gt 60 ]; then echo "barrier timeout" >&2; exit 3; fi
  sleep 0.05
done
%s
`

func TestSnapshotCallsRunConcurrently(t *testing.T) {
	dir := t.TempDir()
	barrier := filepath.Join(dir, "barrier")
	if err := os.Mkdir(barrier, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORK_BARRIER", barrier)
	write := func(name, reply string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(fmt.Sprintf(barrierScript, reply)), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	slicer := write("slicer", `case "$1" in goals) echo '{}';; relay-run) echo '{"runs":[]}';; *) echo '[]';; esac`)
	adj := write("adj", `echo '{"records":[]}'`)
	d := NewDirect(slicercli.Client{Bin: slicer}, adjcli.Client{Bin: adj})

	snap := d.Snapshot(context.Background(), registry.Project{Name: "ork", Path: root, Adj: true})
	for name, err := range map[string]error{"items": snap.Items.Err, "goals": snap.Goals.Err,
		"runs": snap.Runs.Err, "open": snap.Open.Err, "decided": snap.Decided.Err} {
		if err != nil {
			t.Errorf("%s: %v (the calls did not overlap)", name, err)
		}
	}
}

func TestFetchMakesOneCallPerPart(t *testing.T) {
	for _, tc := range []struct {
		part Part
		tool string
		verb string
	}{
		{Items, "slicer", "list"},
		{Deps, "slicer", "deps"},
		{Goals, "slicer", "goals"},
		{Runs, "slicer", "relay-run"},
		{Open, "adj", "inbox"},
		{Decided, "adj", "list"},
	} {
		t.Run(string(tc.part), func(t *testing.T) {
			fake := clitest.Install(t, append(slicerRoutes(), adjRoutes()...)...)
			p := registry.Project{Name: "ork", Path: root, Adj: true}
			if _, err := direct(fake).Fetch(context.Background(), p, tc.part); err != nil {
				t.Fatalf("fetch %s: %v", tc.part, err)
			}
			calls := fake.Calls()
			if len(calls) != 1 || calls[0].Tool != tc.tool || calls[0].Args[0] != tc.verb {
				t.Fatalf("calls for %s: %+v", tc.part, calls)
			}
		})
	}
}

func TestFetchValueHasTheSnapshotFieldsType(t *testing.T) {
	fake := clitest.Install(t, slicerRoutes()...)
	v, err := direct(fake).Fetch(context.Background(), registry.Project{Name: "DataBall", Path: root}, Items)
	items, ok := v.([]slicercli.Item)
	if err != nil || !ok || len(items) != 11 {
		t.Fatalf("items: %T %v, %v", v, len(items), err)
	}
}

func TestFetchUnknownPartIsAnError(t *testing.T) {
	fake := clitest.Install(t)
	if _, err := direct(fake).Fetch(context.Background(), registry.Project{Name: "ork", Path: root}, Part("bogus")); err == nil {
		t.Fatal("an unknown part fetched without an error")
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("an unknown part made calls: %+v", fake.Calls())
	}
}

func TestDirectIsABatcher(t *testing.T) {
	var _ Batcher = (*Direct)(nil)
}

// snapshotArgs is the one call a batch makes for the given snapshot parts, which
// slicer reads in the order the batch names them: items, goals, runs.
func snapshotArgs(parts string, roots ...string) []string {
	args := []string{"snapshot", "--json", "--lean", "--parts", parts}
	for _, r := range roots {
		args = append(args, "--root", r)
	}
	return args
}

func TestBatchGivesTheTypedValuesOfThePerPartCalls(t *testing.T) {
	p := registry.Project{Name: "ork", Path: root}
	snapFake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: snapshotArgs("items,goals,runs", root), File: rec("slicer", "snapshot.json")})
	res, err := direct(snapFake).FetchBatch(context.Background(), []BatchRequest{{Project: p, Parts: []Part{Items, Goals, Runs}}})
	if err != nil || len(res) != 1 || res[0].Err != nil {
		t.Fatalf("batch: %+v, %v", res, err)
	}
	if calls := snapFake.CallsTo("slicer"); len(calls) != 1 {
		t.Fatalf("a batch of three parts made %d slicer calls: %+v", len(calls), calls)
	}

	partFake := clitest.Install(t, slicerRoutes()...)
	d := direct(partFake)
	for _, part := range []Part{Items, Goals, Runs} {
		want, err := d.Fetch(context.Background(), p, part)
		if err != nil {
			t.Fatalf("fetch %s: %v", part, err)
		}
		got := res[0].Parts[part]
		if got.Err != nil || got.Unchanged || got.Revision == "" {
			t.Fatalf("%s: %+v", part, got)
		}
		if !reflect.DeepEqual(got.Value, want) {
			t.Fatalf("%s: the snapshot value differs from the per-part call\n got %#v\nwant %#v", part, got.Value, want)
		}
	}
}

func TestBatchOfSeveralProjectsIsOneCallInRequestOrder(t *testing.T) {
	reply := `{"snapshot":2,"slicer":"1.22.1","contract":12,"projects":[` +
		`{"root":"/a","revision":"r","parts":{"items":{"revision":"i1","fingerprint":null,"value":[{"id":"A1"}]},` +
		`"goals":{"revision":"g1","fingerprint":null,"value":{"goals":"ga","non_goals":"","records":{"goal":[],"non_goal":[]}}},` +
		`"runs":{"revision":"u1","fingerprint":null,"value":{"runs":[]}}},"error":null},` +
		`{"root":"/b","revision":"r","parts":{"items":{"revision":"i2","fingerprint":null,"value":[{"id":"B1"},{"id":"B2"}]},` +
		`"goals":{"revision":"g2","fingerprint":null,"value":{"goals":"gb","non_goals":"","records":{"goal":[],"non_goal":[]}}},` +
		`"runs":{"revision":"u2","fingerprint":null,"value":{"runs":[{"id":"run-b"}]}}},"error":null}]}`
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: snapshotArgs("items,goals,runs", "/a", "/b"), Stdout: reply})
	a, b := registry.Project{Name: "a", Path: "/a"}, registry.Project{Name: "b", Path: "/b"}
	res, err := direct(fake).FetchBatch(context.Background(), []BatchRequest{
		{Project: a, Parts: []Part{Items}}, {Project: b, Parts: []Part{Runs, Goals, Items}}})
	if err != nil || len(res) != 2 {
		t.Fatalf("batch: %+v, %v", res, err)
	}
	if n := len(fake.CallsTo("slicer")); n != 1 {
		t.Fatalf("slicer calls: %d", n)
	}
	if items, _ := res[0].Parts[Items].Value.([]slicercli.Item); len(items) != 1 || items[0].ID != "A1" {
		t.Fatalf("a items: %+v", res[0].Parts[Items])
	}
	if _, asked := res[0].Parts[Runs]; asked {
		t.Fatalf("a part that was not asked for came back: %+v", res[0].Parts)
	}
	if runs, _ := res[1].Parts[Runs].Value.([]slicercli.RelayRun); len(runs) != 1 || runs[0].ID != "run-b" {
		t.Fatalf("b runs: %+v", res[1].Parts[Runs])
	}
	if items, _ := res[1].Parts[Items].Value.([]slicercli.Item); len(items) != 2 || res[1].Parts[Items].Revision != "i2" {
		t.Fatalf("b items: %+v", res[1].Parts[Items])
	}
}

func TestBatchSkipsTheDecodeOfAPartWhoseRevisionIsUnchanged(t *testing.T) {
	// The items value is not valid items JSON: a part that is decoded fails, so a
	// part that is skipped is the one with no error.
	reply := `{"snapshot":2,"projects":[{"root":"/a","revision":"r","parts":{` +
		`"items":{"revision":"i1","fingerprint":null,"value":"not a list of items"},` +
		`"goals":{"revision":"g2","fingerprint":null,"value":{"goals":"new","non_goals":"","records":{"goal":[],"non_goal":[]}}}},"error":null}]}`
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: snapshotArgs("items,goals", "/a"), Stdout: reply})
	res, err := direct(fake).FetchBatch(context.Background(), []BatchRequest{{
		Project: registry.Project{Name: "a", Path: "/a"}, Parts: []Part{Items, Goals},
		Have: map[Part]string{Items: "i1", Goals: "g1"},
	}})
	if err != nil || len(res) != 1 {
		t.Fatalf("batch: %+v, %v", res, err)
	}
	items := res[0].Parts[Items]
	if items.Err != nil || !items.Unchanged || items.Value != nil || items.Revision != "i1" {
		t.Fatalf("an unchanged items part: %+v", items)
	}
	goals := res[0].Parts[Goals]
	if g, _ := goals.Value.(*slicercli.Goals); goals.Err != nil || goals.Unchanged || g == nil || g.Goals != "new" || goals.Revision != "g2" {
		t.Fatalf("a changed goals part: %+v", goals)
	}
}

func TestBatchProjectErrorMarksOnlyThatProject(t *testing.T) {
	reply := `{"snapshot":2,"projects":[` +
		`{"root":"/a","revision":null,"parts":{},"error":{"code":"corrupt","message":"index unreadable","command":"snapshot"}},` +
		`{"root":"/b","revision":"r","parts":{"items":{"revision":"i2","fingerprint":null,"value":[{"id":"B1"}]}},"error":null}]}`
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: snapshotArgs("items", "/a", "/b"), Stdout: reply, Exit: 3})
	res, err := direct(fake).FetchBatch(context.Background(), []BatchRequest{
		{Project: registry.Project{Name: "a", Path: "/a"}, Parts: []Part{Items}},
		{Project: registry.Project{Name: "b", Path: "/b"}, Parts: []Part{Items}}})
	if err != nil || len(res) != 2 {
		t.Fatalf("batch: %+v, %v", res, err)
	}
	var te *clirun.ToolError
	if !errors.As(res[0].Err, &te) || te.Code != "corrupt" || len(res[0].Parts) != 0 {
		t.Fatalf("a: %+v", res[0])
	}
	if items, _ := res[1].Parts[Items].Value.([]slicercli.Item); res[1].Err != nil || len(items) != 1 {
		t.Fatalf("b: %+v", res[1])
	}
}

func TestBatchOfAFailedCallIsAnErrorAndAPartMissingFromTheReplyFailsItself(t *testing.T) {
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: snapshotArgs("items", "/a"),
		Stdout: `{"error":{"code":"usage","message":"nope","command":"snapshot"}}`, Exit: 2})
	p := registry.Project{Name: "a", Path: "/a"}
	if res, err := direct(fake).FetchBatch(context.Background(), []BatchRequest{{Project: p, Parts: []Part{Items}}}); err == nil {
		t.Fatalf("a failed call gave results: %+v", res)
	}

	fake = clitest.Install(t, clitest.Route{Tool: "slicer", Args: snapshotArgs("items", "/a"),
		Stdout: `{"snapshot":2,"projects":[{"root":"/a","revision":"r","parts":{},"error":null}]}`})
	res, err := direct(fake).FetchBatch(context.Background(), []BatchRequest{{Project: p, Parts: []Part{Items}}})
	if err != nil || len(res) != 1 || res[0].Err != nil || res[0].Parts[Items].Err == nil {
		t.Fatalf("a part the reply lacks: %+v, %v", res, err)
	}
}

func TestBatchOfAPartItCannotReadIsAnError(t *testing.T) {
	fake := clitest.Install(t)
	_, err := direct(fake).FetchBatch(context.Background(), []BatchRequest{{Project: registry.Project{Name: "a", Path: "/a"}, Parts: []Part{Deps}}})
	if err == nil || len(fake.Calls()) != 0 {
		t.Fatalf("deps in a batch: %v, calls %+v", err, fake.Calls())
	}
}

func TestHeadIsOneGitCallForHEAD(t *testing.T) {
	fake := clitest.Install(t, clitest.Route{Tool: "git", Args: []string{"-C", root, "rev-parse", "HEAD"}, Stdout: "9f2c41e0\n"})
	d := direct(fake)
	d.Git = fake.Bin("git")
	v, err := d.Fetch(context.Background(), registry.Project{Name: "ork", Path: root}, Head)
	if err != nil || v != "9f2c41e0" {
		t.Fatalf("head: %v, %v", v, err)
	}
	calls := fake.Calls()
	want := clitest.Call{Tool: "git", Args: []string{"-C", root, "rev-parse", "HEAD"}}
	if len(calls) != 1 || calls[0].Tool != want.Tool || strings.Join(calls[0].Args, " ") != strings.Join(want.Args, " ") {
		t.Fatalf("calls for head: %+v", calls)
	}
}

func TestResultIsStaleOnlyAfterAGoodRead(t *testing.T) {
	read := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	failed := errors.New("slicer list: exit 3")
	if (Result[int]{Err: failed}).Stale() {
		t.Fatal("a failed call that never read a value is stale")
	}
	if !(Result[int]{Value: 7, ReadAt: read, Err: failed}).Stale() {
		t.Fatal("a failed call after a good read is not stale")
	}
	if (Result[int]{Value: 7, ReadAt: read}).Stale() {
		t.Fatal("a good read is stale")
	}
}

func TestEngineRunPendingWire(t *testing.T) {
	for _, kind := range []string{"question", "shape", "lost", "interrupted", "error"} {
		t.Run(kind, func(t *testing.T) {
			raw := `{"id":"r1","state":"paused","pending":"` + kind + `"}`
			var r EngineRun
			if err := json.Unmarshal([]byte(raw), &r); err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), `"pending":"`+kind+`"`) {
				t.Fatalf("pending kind lost across wire: %s", b)
			}
		})
	}
}

func TestEngineRunOldWire(t *testing.T) {
	var r EngineRun
	if err := json.Unmarshal([]byte(`{"id":"old","state":"paused"}`), &r); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"pending"`) || r.ID != "old" {
		t.Fatalf("old engine run changed: %s", b)
	}
}

func TestSnapshotAndFetchCarryDeps(t *testing.T) {
	fake := clitest.Install(t, slicerRoutes()...)
	d := direct(fake)
	p := registry.Project{Name: "ork", Path: root}
	s := d.Snapshot(context.Background(), p)
	if s.Deps.Err != nil || len(s.Deps.Value) != 1 || s.Deps.Value[0].ID != "S90" {
		t.Fatalf("snapshot deps: %+v", s.Deps)
	}
	v, err := d.Fetch(context.Background(), p, Deps)
	items, ok := v.([]slicercli.Item)
	if err != nil || !ok || len(items) != 1 || items[0].ID != "S90" {
		t.Fatalf("fetch deps: %+v, %v", v, err)
	}
}

func TestEngineRunLandingWire(t *testing.T) {
	for _, merged := range []string{"", `,"merged":true`, `,"merged":false`} {
		t.Run(merged, func(t *testing.T) {
			var r EngineRun
			if err := json.Unmarshal([]byte(`{"id":"r1","branch":"s09"`+merged+`}`), &r); err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), `"branch":"s09"`) || (merged != "" && !strings.Contains(string(b), merged[1:])) || (merged == "" && strings.Contains(string(b), `"merged"`)) {
				t.Fatalf("landing wire lost state: %s", b)
			}
		})
	}
	var old EngineRun
	if err := json.Unmarshal([]byte(`{"id":"old"}`), &old); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"branch"`) || strings.Contains(string(b), `"merged"`) {
		t.Fatalf("old wire gained landing state: %s", b)
	}
}

// A paused run whose answer or retry waits for a slot is not waiting for the
// owner any more: it shows under Running (S274, adj 177).
func TestResumeWaitingRunIsRunningNotNeeds(t *testing.T) {
	r := EngineRun{Item: "S1", State: "paused", Pending: "question"}
	if got := r.Section([]EngineRun{r}); got != SectionNeeds {
		t.Fatalf("paused section %v, want needs", got)
	}
	r.ResumeWaiting = true
	if got := r.Section([]EngineRun{r}); got != SectionRunning {
		t.Fatalf("resume-waiting section %v, want running", got)
	}
}

func TestEngineRunSection(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		state string
		want  Section
	}{
		{"committed", SectionHidden}, {"stopped", SectionNeeds}, {"paused", SectionNeeds},
		{"awaiting-approval", SectionNeeds}, {"setup", SectionRunning}, {"implement", SectionRunning},
		{"checks", SectionRunning}, {"review", SectionRunning}, {"gate", SectionRunning},
		{"commit", SectionRunning}, {"finish", SectionRunning}, {"future-state", SectionRunning},
	} {
		t.Run(tc.state, func(t *testing.T) {
			r := EngineRun{Item: "S32", State: tc.state, CreatedAt: at}
			for _, newer := range []bool{false, true} {
				runs := []EngineRun{r}
				if newer {
					runs = append(runs, EngineRun{Item: "S32", State: "implement", CreatedAt: at.Add(time.Second)})
				}
				want := tc.want
				if newer && tc.state == "stopped" {
					want = SectionHidden
				}
				if got := r.Section(runs); got != want {
					t.Errorf("newer=%v: section=%v want %v", newer, got, want)
				}
			}
		})
	}
}

func TestDenialsText(t *testing.T) {
	for _, tc := range []struct {
		name    string
		denials []string
		want    string
	}{
		{"five", []string{"A", "B", "C", "D", "E"}, "A, B, C and 2 more"},
		{"three", []string{"A", "B", "C"}, "A, B, C"},
		{"two", []string{"Write", "Bash"}, "Write, Bash"},
		{"empty", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DenialsText(tc.denials); got != tc.want {
				t.Fatalf("DenialsText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBudgetPauseIsNeedsYou(t *testing.T) {
	var r EngineRun
	if err := json.Unmarshal([]byte(`{"id":"budget","state":"paused","pending":"budget","wait":"token budget 9000 reached (9100 used)"}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Pending != "budget" || r.Section([]EngineRun{r}) != SectionNeeds || r.Wait != "token budget 9000 reached (9100 used)" {
		t.Fatalf("budget pause: %+v", r)
	}
}

func TestFindingNeedsExcludesSettledRuns(t *testing.T) {
	f := []EngineFinding{{Kind: runrec.FindingLater, Title: "Later"}, {Kind: runrec.FindingOwner, Title: "Owner"}}
	now := time.Now()
	for _, tc := range []struct {
		name  string
		run   EngineRun
		newer bool
		want  int
	}{
		{"unlanded committed", EngineRun{ID: "old", Item: "S1", State: runrec.StateCommitted, Findings: f}, false, 2},
		{"landed", EngineRun{ID: "old", Item: "S1", State: runrec.StateCommitted, Landed: true, Findings: f}, false, 0},
		{"dismissed", EngineRun{ID: "old", Item: "S1", State: runrec.StateStopped, Dismissed: true, Findings: f}, false, 0},
		{"superseded", EngineRun{ID: "old", Item: "S1", State: runrec.StateStopped, CreatedAt: now, Findings: f}, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runs := []EngineRun{tc.run}
			if tc.newer {
				runs = append(runs, EngineRun{ID: "new", Item: "S1", State: runrec.StateImplement, CreatedAt: now.Add(time.Second)})
			}
			// Production callers must supply sibling runs for supersession.
			if got := tc.run.FindingNeeds(runs...); got != tc.want {
				t.Errorf("count %d, want %d", got, tc.want)
			}
			if tc.newer && !tc.run.Superseded(runs) {
				t.Fatal("invalid supersession fixture")
			}
		})
	}
}
