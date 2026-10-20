// SPDX-License-Identifier: GPL-3.0-or-later

package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	routes[4] = clitest.Route{Tool: "adj", Args: []string{"list", "--json", "--status", "decided", "--root", root},
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
