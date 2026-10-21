// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/clitest"
)

func TestMain(m *testing.M) {
	clitest.Main()
	os.Exit(m.Run())
}

const root = "/projects/ork"

func recorded(name string) string {
	return filepath.Join("..", "..", "testdata", "cli", "slicer", name)
}

func client(t *testing.T, routes ...clitest.Route) (Client, *clitest.Fake) {
	t.Helper()
	fake := clitest.Install(t, routes...)
	return Client{Bin: fake.Bin("slicer")}, fake
}

func route(file string, args ...string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: args, File: recorded(file)}
}

func TestList(t *testing.T) {
	c, fake := client(t, route("list.json", "list", "--json", "--lean", "--root", root))
	items, err := c.List(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 11 {
		t.Fatalf("got %d items", len(items))
	}
	s02 := items[0]
	if s02.ID != "S02" || s02.Title != "Core: registry and CLI adapters" || s02.ShortTitle != "Core and adapters" ||
		s02.Status != "started" || !s02.HasSlice {
		t.Fatalf("S02: %+v", s02)
	}
	if s02.Claim == nil || s02.Claim.Owner != "Jon R" || s02.Claim.At != "2026-10-19T02:42:36Z" {
		t.Fatalf("S02 claim: %+v", s02.Claim)
	}
	want := Fields{Size: "M", Key: "v1.core", Pass: "v1", Importance: 3, Urgency: 3, Effort: 2, Attempts: 1}
	if !reflect.DeepEqual(s02.Fields, want) {
		t.Fatalf("S02 fields: %+v", s02.Fields)
	}
	if s10 := items[1]; s10.ID != "S10" || !reflect.DeepEqual(s10.DependsOn, []string{"S01"}) || s10.Claim != nil {
		t.Fatalf("S10: %+v", s10)
	}
	withNotes := 0
	for _, it := range items {
		if len(it.Notes) > 0 {
			withNotes++
		}
	}
	if withNotes == 0 {
		t.Fatal("no item kept its notes")
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestShow(t *testing.T) {
	c, _ := client(t, route("show-S02.json", "show", "S02", "--json", "--lean", "--root", root))
	d, err := c.Show(context.Background(), root, "S02")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "S02" || d.Status != "started" || d.Fields.Effort != 2 {
		t.Fatalf("detail: %+v", d.Item)
	}
	if d.Slice == nil {
		t.Fatal("no slice spec")
	}
	if d.Slice.Size != "M" || !strings.HasPrefix(d.Slice.Boundary, "**Not in this slice:**") || len(d.Slice.Lead) != 1 {
		t.Fatalf("spec: %+v", d.Slice)
	}
	var headings []string
	for _, s := range d.Slice.Sections {
		headings = append(headings, s.Heading)
	}
	if !reflect.DeepEqual(headings, []string{"Why", "Files", "Failing tests", "Implement", "Check", "Git"}) {
		t.Fatalf("headings: %v", headings)
	}
	if !strings.Contains(d.Slice.Sections[0].Body, "the\nregistry") {
		t.Fatalf("Why body: %q", d.Slice.Sections[0].Body)
	}
}

func TestGoals(t *testing.T) {
	c, _ := client(t, route("goals.json", "goals", "--json", "--root", root))
	g, err := c.Goals(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Records.Goal) == 0 || len(g.Records.NonGoal) == 0 {
		t.Fatalf("goal records: %d goals, %d non-goals", len(g.Records.Goal), len(g.Records.NonGoal))
	}
	first := g.Records.Goal[0]
	if first.ID != "C01" || first.Kind != "goal" || first.Status != "active" ||
		first.Title != "One clear owner for each relay step; no step lives in two tools" {
		t.Fatalf("first goal: %+v", first)
	}
	if ng := g.Records.NonGoal[0]; ng.ID != "C05" || ng.Kind != "non_goal" {
		t.Fatalf("non-goal kind: %+v", ng)
	}
}

func TestRelayRuns(t *testing.T) {
	c, _ := client(t, route("relay-run-list.json", "relay-run", "list", "--json", "--lean", "--root", root))
	runs, err := c.RelayRuns(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("got %d runs", len(runs))
	}
	ended, active := runs[0], runs[1]
	if ended.ID != "run-20261019-004308-40d4" || ended.State != "ended" || ended.Outcome != "pass" ||
		!reflect.DeepEqual(ended.Items, []string{"S01"}) || ended.Worktree != "s01" ||
		ended.BeganAt != "2026-10-19T00:43:08Z" || len(ended.Steps) != 6 {
		t.Fatalf("ended run: %+v", ended)
	}
	if st := ended.Steps[1]; st.Role != "implementer" || st.Vendor != "claude" || st.Model != "haiku" ||
		st.Reasoning != "xhigh" || st.Phase != "done" || st.Parent != "1" {
		t.Fatalf("ended step 2: %+v", st)
	}
	if active.State != "active" || active.Counts.Running != 2 || active.Counts.Subagents != 1 ||
		active.Counts.Roles["orchestrator"] != 1 {
		t.Fatalf("active run: %+v", active)
	}
}

func TestRelayRunsActiveOnlyPassesFlag(t *testing.T) {
	c, _ := client(t, route("relay-run-list.json", "relay-run", "list", "--json", "--lean", "--active", "--root", root))
	if _, err := c.RelayRuns(context.Background(), root, true); err != nil {
		t.Fatal(err)
	}
}

func TestListReadsFlags(t *testing.T) {
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", root},
		Stdout: `[{"id":"X1","title":"t","status":"open","has_slice":false,"fields":{"flags":["needs-owner","other"]}},` +
			`{"id":"X2","title":"u","status":"open","has_slice":false,"fields":{"size":"S"}}]`})
	items, err := c.List(context.Background(), root)
	if err != nil || len(items) != 2 {
		t.Fatalf("List: %+v, %v", items, err)
	}
	if !reflect.DeepEqual(items[0].Fields.Flags, []string{"needs-owner", "other"}) || len(items[1].Fields.Flags) != 0 {
		t.Fatalf("flags: %+v, %+v", items[0].Fields.Flags, items[1].Fields.Flags)
	}
}

func TestUnknownFieldsAreIgnored(t *testing.T) {
	c, _ := client(t,
		clitest.Route{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", root},
			Stdout: `[{"id":"X1","title":"t","status":"open","has_slice":false,"zzz":{"a":1},"fields":{"size":"S","brand_new":"q"},"claim":{"owner":"o","at":"a","extra":1}}]`},
		clitest.Route{Tool: "slicer", Args: []string{"goals", "--json", "--root", root},
			Stdout: `{"goals":"g","non_goals":"","later":[1],"records":{"goal":[{"id":"C1","new":true}],"other":[]}}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "list", "--json", "--lean", "--root", root},
			Stdout: `{"runs":[{"id":"r1","novel":1,"steps":[{"id":"1","role":"x","novel":2}]}],"page":2}`},
		clitest.Route{Tool: "slicer", Args: []string{"show", "X1", "--json", "--lean", "--root", root},
			Stdout: `{"id":"X1","title":"t","status":"open","has_slice":true,"zzz":[1],"fields":{"size":"S","brand_new":"q"},"slice":{"id":"X1","size":"S","novel":{"a":1},"sections":[{"heading":"Why","body":"b","extra":true}]},"also":"new"}`},
	)
	items, err := c.List(context.Background(), root)
	if err != nil || len(items) != 1 || items[0].ID != "X1" || items[0].Fields.Size != "S" {
		t.Fatalf("List: %+v, %v", items, err)
	}
	d, err := c.Show(context.Background(), root, "X1")
	if err != nil || d.ID != "X1" || d.Fields.Size != "S" || d.Slice == nil || d.Slice.Size != "S" ||
		len(d.Slice.Sections) != 1 || d.Slice.Sections[0].Heading != "Why" {
		t.Fatalf("Show: %+v, %v", d, err)
	}
	g, err := c.Goals(context.Background(), root)
	if err != nil || g.Goals != "g" || len(g.Records.Goal) != 1 {
		t.Fatalf("Goals: %+v, %v", g, err)
	}
	runs, err := c.RelayRuns(context.Background(), root, false)
	if err != nil || len(runs) != 1 || runs[0].Steps[0].Role != "x" {
		t.Fatalf("RelayRuns: %+v, %v", runs, err)
	}
}

func TestToolErrorKeepsItsCode(t *testing.T) {
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"show", "NOPE", "--json", "--lean", "--root", root},
		File: recorded("error-no-such-item.json"), Exit: 2})
	_, err := c.Show(context.Background(), root, "NOPE")
	var te *clirun.ToolError
	if !errors.As(err, &te) || te.Code != "no_such_item" || te.Command != "show" {
		t.Fatalf("error: %v", err)
	}
}

func TestMalformedOutputIsAnError(t *testing.T) {
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", root}, Stdout: "{"})
	_, err := c.List(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("error: %v", err)
	}
}

func TestDeps(t *testing.T) {
	c, fake := client(t, route("deps.json", "deps", "--json", "--lean", "--root", root))
	got, err := c.Deps(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var want []Item
	raw, err := os.ReadFile(recorded("deps.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(got) != 26 || got[0].ID != "S55" || got[25].ID != "S39" || !reflect.DeepEqual(got, want) {
		t.Fatalf("deps: %+v, want %+v", got, want)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestPlanParallel(t *testing.T) {
	c, fake := client(t, route("plan-parallel.json", "plan-parallel", "--json", "--lean", "--root", root))
	plan, err := c.PlanParallel(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Width != 4 || len(plan.Waves) != 14 || plan.Waves[0].Wave != 1 || plan.Waves[0].Items[0].ID != "S55" || plan.Waves[1].Items[0].Reason != "shares: S55 docs/dashboard.md" || len(plan.InFlight) != 0 || plan.Unplaced[0].Reason != "no_files" || plan.Blocked[0].WaitingOn[0] != "S48" || plan.Held[0].Flags[0] != "needs-owner" || plan.Unspecified[0].Missing[0] != "Implement" {
		t.Fatalf("plan: %+v", plan)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestPlanParallelToolError(t *testing.T) {
	r := route("plan-parallel-error.json", "plan-parallel", "--json", "--lean", "--root", root)
	r.Exit = 2
	c, _ := client(t, r)
	_, err := c.PlanParallel(context.Background(), root)
	var te *clirun.ToolError
	if !errors.As(err, &te) || te.Code != "usage" || te.Command != "plan-parallel" || !strings.Contains(te.Message, "files_section") {
		t.Fatalf("error: %v", err)
	}
}
