// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const carveTwoSlivers = "## Slivers\n\n" +
	"### 1. Add the flag\n\nFiles:\n- `internal/a.go`\n\nFailing tests:\n- TestA\n\nCheck:\n- `go test ./internal/a`\n\n" +
	"### 2. Wire the flag\n\nFiles:\n- `internal/b.go`\n- `internal/b_test.go`\n\nFailing tests:\n- TestB\n\nCheck:\n- `go test ./internal/b`\n\n" +
	"## Owner needed\nnone\n"

var localImplementer = map[string][]runrec.LineupEntry{engine.RoleImplementer: {{Vendor: "claude-local", Model: "qwen3.8-27b", Effort: "medium"}}}

// carveRig is a specRig whose S1 has the given size and effort. Each of lists
// is the children `slicer list --parent S1` answers, call by call, and the
// last answer repeats: with {nil, S1a S1b}, the start finds none and the
// chain is planned from the two the carve filed. Filing S1a and S1b answers
// the two `slicer add` calls in order.
func carveRig(t *testing.T, size string, effort int, lists ...[]string) *specRig {
	t.Helper()
	return carveRigAdds(t, size, effort, []clitest.Reply{{Stdout: `{"id":"S1a"}`}, {Stdout: `{"id":"S1b"}`}}, lists...)
}

// carveRigAdds is carveRig with its own answers to the `slicer add` calls, in
// order, the last repeating.
func carveRigAdds(t *testing.T, size string, effort int, adds []clitest.Reply, lists ...[]string) *specRig {
	t.Helper()
	localLaunchers(t)
	var answers []clitest.Reply
	for _, ids := range lists {
		var items []string
		for _, id := range ids {
			items = append(items, fmt.Sprintf(`{"id":%q,"parent":"S1"}`, id))
		}
		answers = append(answers, clitest.Reply{Stdout: "[" + strings.Join(items, ",") + "]"})
	}
	row := func(id, model, effort string) string {
		return fmt.Sprintf(`{"id":%q,"agent":"Claude Code","vendor":"Claude","model":%q,"reasoning":%q}`, id, model, effort)
	}
	sliverRoles := `{"sliver":true,"roles":[{"role":"implementer","row":` + row("r8", "haiku", "xhigh") + `},{"role":"reviewer","row":` + row("r5", "opus", "medium") + `,"alternatives":[` + row("r16", "sonnet-spec", "low") + `]},{"role":"architect","row":null,"alternatives":[]}]}`
	routes := []clitest.Route{
		{Tool: "slicer", Args: []string{"show", "S1", "--json"}, Prefix: true, Stdout: fmt.Sprintf(`{"id":"S1","title":"T","fields":{"size":%q,"effort":%d},"slice":{"id":"S1","title":"T","size":%q,"sections":[{"heading":"Why","body":"why S1"}]}}`, size, effort, size)},
		{Tool: "slicer", Args: []string{"list", "--parent", "S1"}, Prefix: true, Seq: answers},
		{Tool: "slicer", Args: []string{"add"}, Prefix: true, Seq: adds},
		{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{}`},
	}
	for _, id := range []string{"S1a", "S1b"} {
		routes = append(routes,
			clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend", id}, Prefix: true, Stdout: sliverRoles},
			clitest.Route{Tool: "slicer", Args: []string{"show", id}, Prefix: true, Stdout: `{"id":"` + id + `","parent":"S1","title":"t","slice":{"sections":[{"heading":"Git","body":"One commit."}]}}`})
	}
	rig := newSpecRig(t, rulesJSON(u11), routes...)
	rig.report = carveTwoSlivers
	return rig
}

func (r *specRig) slicerCalls(verb string) [][]string {
	var out [][]string
	for _, c := range r.fake.CallsTo("slicer") {
		if len(c.Args) > 0 && c.Args[0] == verb {
			out = append(out, c.Args)
		}
	}
	return out
}

func (r *specRig) projectPath() string {
	proj, err := r.d.registered("p")
	if err != nil {
		panic(err)
	}
	return proj.Path
}

func (r *specRig) runsOf(t *testing.T) map[string]runrec.Record {
	t.Helper()
	all, err := r.store.List()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]runrec.Record{}
	for _, run := range all {
		rec := run.Snapshot()
		out[rec.Item] = rec
	}
	return out
}

// A local implementer on a size M slice has the spec check row carve it into
// slivers, which ork files with no owner confirmation (adj 337); the run is
// then the chain of those slivers.
func TestLocalRunOfASizeMSliceCarvesAndFilesSliversThenRunsTheChain(t *testing.T) {
	rig := carveRig(t, "M", 1, nil, []string{"S1a", "S1b"})
	rec, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1", Lineup: localImplementer})
	if err != nil {
		t.Fatal(err)
	}
	reqs := rig.requests()
	if len(reqs) != 1 || reqs[0].Role != engine.RoleSpecCheck || !reqs[0].ReadOnly || reqs[0].Model != "sonnet-spec" || !strings.Contains(reqs[0].Brief, "Role: carve.") {
		t.Fatalf("the carve is one read-only pass of the spec check row: %+v", reqs)
	}
	adds := rig.slicerCalls("add")
	if len(adds) != 2 {
		t.Fatalf("slicer add calls: %v", adds)
	}
	for i, title := range []string{"Add the flag", "Wire the flag"} {
		line := strings.Join(adds[i], " ")
		if adds[i][1] != title || !strings.Contains(line, "--parent S1 ") || !strings.Contains(line, "--size S") || !strings.Contains(line, "--effort 1") {
			t.Errorf("add %d: %s", i, line)
		}
	}
	if len(rig.slicerCalls("promote")) != 2 {
		t.Errorf("each sliver gets its sections: %v", rig.slicerCalls("promote"))
	}
	runs := rig.runsOf(t)
	a, b := runs["S1a"], runs["S1b"]
	if rec.Item != "S1a" || a.ID == "" || b.ID == "" || runs["S1"].ID != "" {
		t.Fatalf("the run is the chain of S1a and S1b, not S1: reply %q, runs %v", rec.Item, runs)
	}
	if a.Chain == "" || a.Chain != b.Chain || a.SliverIndex != 1 || b.SliverIndex != 2 || a.SliverCount != 2 {
		t.Errorf("one chain: %+v %+v", a, b)
	}
}

// A confirmed set of slivers (filed earlier, by the carve or by hand) runs as
// one chain in the parent's worktree on the local implementer; it is not carved
// again.
func TestLocalRunOfASlicedSliceRunsItsSliversAsOneChainInTheParentWorktree(t *testing.T) {
	rig := carveRig(t, "M", 1, []string{"S1a", "S1b"}, []string{"S1a", "S1b"})
	if _, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1", Lineup: localImplementer}); err != nil {
		t.Fatal(err)
	}
	if len(rig.requests()) != 0 || len(rig.slicerCalls("add")) != 0 {
		t.Fatalf("slivers that exist are not carved again: %d carve rounds, adds %v", len(rig.requests()), rig.slicerCalls("add"))
	}
	runs := rig.runsOf(t)
	want := filepath.Join(rig.projectPath(), ".worktrees", "S1")
	for _, id := range []string{"S1a", "S1b"} {
		rec := runs[id]
		if rec.Worktree != want || rec.Branch != "S1" || rec.Chain == "" || rec.Chain != runs["S1a"].Chain {
			t.Errorf("%s: worktree %q branch %q chain %q, want worktree %q branch S1 in one chain", id, rec.Worktree, rec.Branch, rec.Chain, want)
		}
		if got := rec.Lineup[engine.RoleImplementer]; len(got) == 0 || got[0].Vendor != "claude-local" {
			t.Errorf("%s implementer is not local: %+v", id, got)
		}
	}
}

// A cloud implementer on the same slice runs it whole: no carve, no slivers.
func TestCloudRunOfASizeMSliceIsNotSlivered(t *testing.T) {
	rig := carveRig(t, "M", 1, nil, []string{"S1a", "S1b"})
	rec, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Item != "S1" || rec.Chain != "" {
		t.Fatalf("a cloud run is the whole slice: %+v", rec)
	}
	if len(rig.requests()) != 0 || len(rig.slicerCalls("add")) != 0 || len(rig.slicerCalls("list")) != 0 {
		t.Fatalf("a cloud run is not carved: %d carve rounds, adds %v, lists %v", len(rig.requests()), rig.slicerCalls("add"), rig.slicerCalls("list"))
	}
}

// A size S slice is not above size S: a local implementer takes it whole.
func TestLocalRunOfASizeSSliceIsNotSlivered(t *testing.T) {
	rig := carveRig(t, "S", 1, nil, nil)
	rec, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1", Lineup: localImplementer})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Item != "S1" || rec.Chain != "" || len(rig.requests()) != 0 || len(rig.slicerCalls("add")) != 0 {
		t.Fatalf("a size S slice is run whole: %+v, %d carve rounds", rec, len(rig.requests()))
	}
}

// Rule u6 never slivers an effort 3 slice, so ork does not run one on a local
// implementer, whatever its size (owner decision 2026-11-16).
func TestLocalRunOfAnEffortThreeSliceIsRefused(t *testing.T) {
	for _, size := range []string{"S", "M", "L"} {
		t.Run(size, func(t *testing.T) {
			rig := carveRig(t, size, 3, nil, nil)
			_, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1", Lineup: localImplementer})
			if err == nil || !strings.Contains(err.Error(), "cloud implementer") || !strings.Contains(err.Error(), "effort 3") {
				t.Fatalf("an effort 3 slice on a local implementer: %v", err)
			}
			if len(rig.requests()) != 0 || len(rig.slicerCalls("add")) != 0 || len(rig.runsOf(t)) != 0 {
				t.Fatalf("a refused start carves and runs nothing")
			}
		})
	}
}

// The plan is a checkpoint, not an action: it shows that slivers will be
// carved and files nothing.
func TestPlanOfALocalRunAboveSizeSSaysItWillCarveAndFilesNothing(t *testing.T) {
	for _, size := range []string{"M", "L"} {
		t.Run(size, func(t *testing.T) {
			rig := carveRig(t, size, 1, nil, []string{"S1a", "S1b"})
			raw, _ := json.Marshal(RunParams{Project: "p", Item: "S1", Lineup: localImplementer})
			var plan RunPlan
			if err := json.Unmarshal(request(t, rig.d, MethodRunPlan, string(raw)), &plan); err != nil {
				t.Fatal(err)
			}
			if !plan.Carve || !strings.Contains(plan.Note, "sliver") || len(plan.Slivers) != 0 {
				t.Fatalf("plan: carve %v note %q slivers %d", plan.Carve, plan.Note, len(plan.Slivers))
			}
			if len(rig.requests()) != 0 || len(rig.slicerCalls("add")) != 0 {
				t.Fatalf("planning carved: %d rounds, adds %v", len(rig.requests()), rig.slicerCalls("add"))
			}
		})
	}
}

// The owner confirms the plan that says it will carve; the start carves and
// runs the chain instead of refusing the plan as changed.
func TestStartOfAConfirmedCarvePlanCarvesAndRunsTheChain(t *testing.T) {
	// The plan below and the start's own planning each find no sliver.
	rig := carveRig(t, "M", 1, nil, nil, []string{"S1a", "S1b"})
	params := RunParams{Project: "p", Item: "S1", Lineup: localImplementer}
	plan, err := rig.d.planRun(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	params.Expected = plan
	rec, err := rig.d.startRun(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Item != "S1a" || rec.Chain == "" || len(rig.slicerCalls("add")) != 2 {
		t.Fatalf("a confirmed carve plan: %+v, adds %v", rec, rig.slicerCalls("add"))
	}
}

// A filing that fails part way must not leave a short chain that lands the
// parent with half its work: the next start files the rest from the saved
// carve report (slicer's add key returns the sliver that is already filed),
// and runs the whole chain without asking the role again.
func TestCarveThatFailedPartWayIsFiledFromItsSavedReportOnTheNextStart(t *testing.T) {
	adds := []clitest.Reply{
		{Stdout: `{"id":"S1a"}`},
		{Stderr: "slicer is busy", Exit: 1},
		{Stdout: `{"id":"S1a","existing":true,"has_slice":true}`},
		{Stdout: `{"id":"S1b"}`},
	}
	rig := carveRigAdds(t, "M", 1, adds, nil, []string{"S1a"}, []string{"S1a", "S1b"})
	params := RunParams{Project: "p", Item: "S1", Lineup: localImplementer}
	_, err := rig.d.startRun(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "Filed so far: S1a") || !strings.Contains(err.Error(), "not filed") {
		t.Fatalf("a filing that fails part way: %v", err)
	}
	if len(rig.runsOf(t)) != 0 {
		t.Fatalf("a failed filing started runs: %v", rig.runsOf(t))
	}
	rec, err := rig.d.startRun(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(rig.requests()); n != 1 {
		t.Errorf("%d carve rounds; the second start files from the saved report", n)
	}
	if n := len(rig.slicerCalls("add")); n != 4 {
		t.Errorf("%d slicer add calls, want 4 (S1a, the failure, S1a again, S1b)", n)
	}
	runs := rig.runsOf(t)
	a, b := runs["S1a"], runs["S1b"]
	if rec.Item != "S1a" || a.ID == "" || b.ID == "" || a.Chain == "" || a.Chain != b.Chain || b.SliverCount != 2 {
		t.Fatalf("the whole chain runs, not the slivers filed before the failure: %+v", runs)
	}
}

// A saved report that has no more slivers than are filed is no reason to carve
// or file again: the filed slivers are the chain.
func TestSavedCarveReportDoesNotRefileAFiledChain(t *testing.T) {
	rig := carveRig(t, "M", 1, nil, []string{"S1a", "S1b"}, []string{"S1a", "S1b"})
	params := RunParams{Project: "p", Item: "S1", Lineup: localImplementer}
	if _, err := rig.d.startRun(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	plan, err := rig.d.planRun(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Carve || len(plan.Slivers) != 2 || len(rig.requests()) != 1 {
		t.Fatalf("a fully filed chain: carve %v, %d slivers, %d carve rounds", plan.Carve, len(plan.Slivers), len(rig.requests()))
	}
}

// A carve that cannot be read files nothing and starts nothing.
func TestCarveWithAMalformedReportFilesAndRunsNothing(t *testing.T) {
	rig := carveRig(t, "M", 1, nil, nil)
	rig.report = "## Slivers\n\n### 1. Big\n\nFiles:\n- `a.go`\n- `b.go`\n- `c.go`\n\nFailing tests:\n- t\n\nCheck:\n- c\n\n## Owner needed\nnone\n"
	_, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1", Lineup: localImplementer})
	if err == nil || !strings.Contains(err.Error(), "one or two files") {
		t.Fatalf("start: %v", err)
	}
	if len(rig.slicerCalls("add")) != 0 || len(rig.runsOf(t)) != 0 {
		t.Fatalf("filed or started after a refused carve: adds %v", rig.slicerCalls("add"))
	}
}
