// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

// carveReadyReport is the spec check's answer for S1 on a local implementer:
// Ready, with the orientation pack a local run takes (so a second check finds
// the first one's answer).
const carveReadyReport = "## Slice S1\nReady\n\n## Owner needed\nnone\n\n## Orientation S1\nthe pack\n"

var localImplementer = map[string][]runrec.LineupEntry{engine.RoleImplementer: {{Vendor: "claude-local", Model: "qwen3.8-27b", Effort: "medium"}}}

// carveRig is a specRig whose S1 has the given size and effort. Before is the
// children `slicer list --parent S1` finds until the carve round starts, and
// after is what it finds from then on (the slivers the carve filed). Filing
// S1a and S1b answers the two `slicer add` calls in order.
func carveRig(t *testing.T, size string, effort int, before, after []string) *specRig {
	t.Helper()
	return carveRigAdds(t, size, effort, []clitest.Reply{{Stdout: `{"id":"S1a"}`}, {Stdout: `{"id":"S1b"}`}}, before, after)
}

// carveRigAdds is carveRig with its own answers to the `slicer add` calls, in
// order, the last repeating.
func carveRigAdds(t *testing.T, size string, effort int, adds []clitest.Reply, before, after []string) *specRig {
	t.Helper()
	localLaunchers(t)
	children := filepath.Join(t.TempDir(), "children.json")
	setChildren := func(ids []string) {
		var items []string
		for _, id := range ids {
			items = append(items, fmt.Sprintf(`{"id":%q,"parent":"S1"}`, id))
		}
		if err := os.WriteFile(children, []byte("["+strings.Join(items, ",")+"]"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	setChildren(before)
	row := func(id, model, effort string) string {
		return fmt.Sprintf(`{"id":%q,"agent":"Claude Code","vendor":"Claude","model":%q,"reasoning":%q}`, id, model, effort)
	}
	sliverRoles := `{"sliver":true,"roles":[{"role":"implementer","row":` + row("r8", "haiku", "xhigh") + `},{"role":"reviewer","row":` + row("r5", "opus", "medium") + `,"alternatives":[` + row("r16", "sonnet-spec", "low") + `]},{"role":"architect","row":null,"alternatives":[]}]}`
	routes := []clitest.Route{
		{Tool: "slicer", Args: []string{"show", "S1", "--json"}, Prefix: true, Stdout: fmt.Sprintf(`{"id":"S1","title":"T","fields":{"size":%q,"effort":%d},"slice":{"id":"S1","title":"T","size":%q,"sections":[{"heading":"Why","body":"why S1"}]}}`, size, effort, size)},
		{Tool: "slicer", Args: []string{"list", "--parent", "S1"}, Prefix: true, File: children},
		{Tool: "slicer", Args: []string{"add"}, Prefix: true, Seq: adds},
		{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{}`},
		{Tool: "slicer", Args: []string{"edit", "S1"}, Prefix: true, Stdout: `{"id":"S1"}`},
	}
	for _, id := range []string{"S1a", "S1b"} {
		routes = append(routes,
			clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend", id}, Prefix: true, Stdout: sliverRoles},
			clitest.Route{Tool: "slicer", Args: []string{"show", id}, Prefix: true, Stdout: `{"id":"` + id + `","parent":"S1","title":"t","slice":{"sections":[{"heading":"Git","body":"One commit."}]}}`})
	}
	rig := newSpecRig(t, rulesJSON(u11), routes...)
	rig.report = carveReadyReport
	rig.carve = carveTwoSlivers
	rig.onCarve = func() { setChildren(after) }
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

// checkS1 runs the spec check of S1 for an implementer lineup (none: the
// matrix's own).
func (r *specRig) checkS1(t *testing.T, lineup map[string][]runrec.LineupEntry) SpecItemResult {
	t.Helper()
	p := SpecCheckParams{Project: "p", Items: []string{"S1"}}
	if lineup != nil {
		p.Lineups = map[string]map[string][]runrec.LineupEntry{"S1": lineup}
	}
	raw, _ := json.Marshal(p)
	var out SpecCheckResult
	if err := json.Unmarshal(request(t, r.d, MethodSpecCheck, string(raw)), &out); err != nil {
		t.Fatal(err)
	}
	return r.item(out, "S1")
}

// A local implementer on a size M slice has the spec check row carve it into
// slivers inside the spec check, which ork files with no owner confirmation
// (adj 337, adj 396); the plan then lists the slivers as the chain, and the
// owner's one approval of that plan runs the chain.
func TestLocalRunOfASizeMSliceCarvesAndFilesSliversThenRunsTheChain(t *testing.T) {
	rig := carveRig(t, "M", 1, nil, []string{"S1a", "S1b"})
	params := RunParams{Project: "p", Item: "S1", Lineup: localImplementer}
	// The plan the owner sees first is a checkpoint: it files nothing.
	before, err := rig.d.planRun(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Carve || len(before.Slivers) != 0 {
		t.Fatalf("plan before the carve: carve %v, %d slivers", before.Carve, len(before.Slivers))
	}
	if len(rig.requests()) != 0 || len(rig.slicerCalls("add")) != 0 {
		t.Fatalf("planning carved: %d rounds, adds %v", len(rig.requests()), rig.slicerCalls("add"))
	}
	item := rig.checkS1(t, localImplementer)
	if !item.Ready || strings.Join(item.Carved, ",") != "S1a,S1b" {
		t.Fatalf("the spec check carves and files the slivers: %+v", item)
	}
	reqs := rig.requests()
	if len(reqs) != 2 || strings.Contains(reqs[0].Brief, "Role: carve.") || !strings.Contains(reqs[1].Brief, "Role: carve.") {
		t.Fatalf("one spec check round, then one carve round: %d rounds", len(reqs))
	}
	if reqs[1].Role != engine.RoleSpecCheck || !reqs[1].ReadOnly || reqs[1].Model != "sonnet-spec" {
		t.Fatalf("the carve is a read-only pass of the spec check row: %+v", reqs[1])
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
	if len(rig.runsOf(t)) != 0 {
		t.Fatalf("the spec check started a run: %v", rig.runsOf(t))
	}
	// The plan now lists the filed slivers; the owner confirms it once.
	plan, err := rig.d.planRun(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Carve || len(plan.Slivers) != 2 || plan.Slivers[0].Item != "S1a" || plan.Slivers[1].Item != "S1b" {
		t.Fatalf("plan after the carve: carve %v, slivers %+v", plan.Carve, plan.Slivers)
	}
	params.Expected = plan
	rec, err := rig.d.startRun(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	runs := rig.runsOf(t)
	a, b := runs["S1a"], runs["S1b"]
	if rec.Item != "S1a" || a.ID == "" || b.ID == "" || runs["S1"].ID != "" {
		t.Fatalf("the run is the chain of S1a and S1b, not S1: reply %q, runs %v", rec.Item, runs)
	}
	if a.Chain == "" || a.Chain != b.Chain || a.SliverIndex != 1 || b.SliverIndex != 2 || a.SliverCount != 2 {
		t.Errorf("one chain: %+v %+v", a, b)
	}
	if n := len(rig.slicerCalls("add")); n != 2 {
		t.Errorf("the start filed more slivers: %d adds", n)
	}
}

// A confirmed set of slivers (filed earlier, by the carve or by hand) runs as
// one chain in the parent's worktree on the local implementer; the spec check
// does not carve again.
func TestLocalRunOfASlicedSliceRunsItsSliversAsOneChainInTheParentWorktree(t *testing.T) {
	rig := carveRig(t, "M", 1, []string{"S1a", "S1b"}, []string{"S1a", "S1b"})
	params := RunParams{Project: "p", Item: "S1", Lineup: localImplementer}
	if item := rig.checkS1(t, localImplementer); !item.Ready || len(item.Carved) != 0 {
		t.Fatalf("slivers that exist are not carved again: %+v", item)
	}
	if n := len(rig.requests()); n != 1 || len(rig.slicerCalls("add")) != 0 {
		t.Fatalf("slivers that exist are not carved again: %d rounds, adds %v", n, rig.slicerCalls("add"))
	}
	plan, err := rig.d.planRun(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	params.Expected = plan
	if _, err := rig.d.startRun(context.Background(), params); err != nil {
		t.Fatal(err)
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

// A cloud implementer on the same slice runs it whole: its spec check carves
// nothing and the start files nothing.
func TestCloudRunOfASizeMSliceIsNotSlivered(t *testing.T) {
	rig := carveRig(t, "M", 1, nil, []string{"S1a", "S1b"})
	if item := rig.checkS1(t, nil); !item.Ready || len(item.Carved) != 0 {
		t.Fatalf("a cloud run is not carved: %+v", item)
	}
	rec, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Item != "S1" || rec.Chain != "" {
		t.Fatalf("a cloud run is the whole slice: %+v", rec)
	}
	if n := len(rig.requests()); n != 1 || len(rig.slicerCalls("add")) != 0 || len(rig.slicerCalls("list")) != 0 {
		t.Fatalf("a cloud run is not carved: %d rounds, adds %v, lists %v", n, rig.slicerCalls("add"), rig.slicerCalls("list"))
	}
}

// A size S slice is not above size S: a local implementer takes it whole.
func TestLocalRunOfASizeSSliceIsNotSlivered(t *testing.T) {
	rig := carveRig(t, "S", 1, nil, nil)
	if item := rig.checkS1(t, localImplementer); !item.Ready || len(item.Carved) != 0 {
		t.Fatalf("a size S slice is not carved: %+v", item)
	}
	rec, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1", Lineup: localImplementer})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Item != "S1" || rec.Chain != "" || len(rig.requests()) != 1 || len(rig.slicerCalls("add")) != 0 {
		t.Fatalf("a size S slice is run whole: %+v, %d rounds", rec, len(rig.requests()))
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
			if item := rig.checkS1(t, localImplementer); len(item.Carved) != 0 {
				t.Fatalf("an effort 3 slice is not carved: %+v", item)
			}
			if len(rig.requests()) > 1 || len(rig.slicerCalls("add")) != 0 || len(rig.runsOf(t)) != 0 {
				t.Fatalf("a refused start carves and runs nothing")
			}
		})
	}
}

// The plan is a checkpoint, not an action: before the spec check it says
// slivers will be carved and files nothing.
func TestPlanOfALocalRunAboveSizeSSaysItWillCarveAndFilesNothing(t *testing.T) {
	for _, size := range []string{"M", "L"} {
		t.Run(size, func(t *testing.T) {
			rig := carveRig(t, size, 1, nil, []string{"S1a", "S1b"})
			raw, _ := json.Marshal(RunParams{Project: "p", Item: "S1", Lineup: localImplementer})
			var plan RunPlan
			if err := json.Unmarshal(request(t, rig.d, MethodRunPlan, string(raw)), &plan); err != nil {
				t.Fatal(err)
			}
			if !plan.Carve || !strings.Contains(plan.Note, "spec check") || len(plan.Slivers) != 0 {
				t.Fatalf("plan: carve %v note %q slivers %d", plan.Carve, plan.Note, len(plan.Slivers))
			}
			if len(rig.requests()) != 0 || len(rig.slicerCalls("add")) != 0 {
				t.Fatalf("planning carved: %d rounds, adds %v", len(rig.requests()), rig.slicerCalls("add"))
			}
		})
	}
}

// A start never carves: with no slivers filed, the owner has not seen the
// chain, so the start is refused and files and runs nothing.
func TestStartOfAnUncarvedLocalSliceIsRefused(t *testing.T) {
	rig := carveRig(t, "M", 1, nil, []string{"S1a", "S1b"})
	_, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1", Lineup: localImplementer})
	if err == nil || !strings.Contains(err.Error(), "no slivers filed") || !strings.Contains(err.Error(), "spec check") {
		t.Fatalf("start of an uncarved slice: %v", err)
	}
	if len(rig.requests()) != 0 || len(rig.slicerCalls("add")) != 0 || len(rig.runsOf(t)) != 0 {
		t.Fatalf("a refused start carves and runs nothing: %d rounds, adds %v", len(rig.requests()), rig.slicerCalls("add"))
	}
}

// An item with open spec questions is not carved: its spec may change when the
// owner answers. The answers that make it Ready carve it.
func TestSpecCheckCarvesAfterTheOwnerAnswersTheQuestions(t *testing.T) {
	rig := carveRig(t, "M", 1, nil, []string{"S1a", "S1b"})
	questions, _, _ := strings.Cut(strings.Replace(specQuestionsReport, "## Slice S2", "## Slice S1", 1), "\n\nQuestion: What")
	rig.report = questions + "\n\n## Owner needed\nnone\n\n## Orientation S1\nthe pack\n"
	if item := rig.checkS1(t, localImplementer); item.Ready || len(item.Questions) != 1 || len(item.Carved) != 0 {
		t.Fatalf("an item with open questions: %+v", item)
	}
	if len(rig.slicerCalls("add")) != 0 {
		t.Fatalf("carved an item that is not Ready: %v", rig.slicerCalls("add"))
	}
	raw, _ := json.Marshal(SpecAnswerParams{Project: "p", Item: "S1", Answers: []string{"memory: rebuilt at each start"}, Lineup: localImplementer})
	var answered SpecAnswerResult
	if err := json.Unmarshal(request(t, rig.d, MethodSpecAnswer, string(raw)), &answered); err != nil {
		t.Fatal(err)
	}
	if !answered.Ready || strings.Join(answered.Carved, ",") != "S1a,S1b" {
		t.Fatalf("the answers make the item Ready, which carves it: %+v", answered)
	}
	if n := len(rig.requests()); n != 2 || len(rig.slicerCalls("add")) != 2 {
		t.Errorf("%d rounds, adds %v: one spec check and one carve", n, rig.slicerCalls("add"))
	}
	if item := rig.checkS1(t, localImplementer); !item.Ready || len(item.Carved) != 0 || len(rig.slicerCalls("add")) != 2 {
		t.Fatalf("the check after the answers files nothing more: %+v", item)
	}
}

// A filing that fails part way must not leave a short chain that lands the
// parent with half its work: the next spec check files the rest from the saved
// carve report (slicer's add key returns the sliver that is already filed)
// without asking the role again, and the plan then lists the whole chain.
func TestCarveThatFailedPartWayIsFiledFromItsSavedReportByTheNextSpecCheck(t *testing.T) {
	adds := []clitest.Reply{
		{Stdout: `{"id":"S1a"}`},
		{Stderr: "slicer is busy", Exit: 1},
		{Stdout: `{"id":"S1a","existing":true,"has_slice":true}`},
		{Stdout: `{"id":"S1b"}`},
	}
	rig := carveRigAdds(t, "M", 1, adds, nil, []string{"S1a"})
	params := RunParams{Project: "p", Item: "S1", Lineup: localImplementer}
	raw, _ := json.Marshal(SpecCheckParams{Project: "p", Items: []string{"S1"}, Lineups: map[string]map[string][]runrec.LineupEntry{"S1": localImplementer}})
	_, err := rig.d.handle(context.Background(), MethodSpecCheck, json.RawMessage(raw))
	if err == nil || !strings.Contains(err.Error(), "Filed so far: S1a") || !strings.Contains(err.Error(), "not filed") {
		t.Fatalf("a filing that fails part way: %v", err)
	}
	if len(rig.runsOf(t)) != 0 {
		t.Fatalf("a failed filing started runs: %v", rig.runsOf(t))
	}
	if plan, err := rig.d.planRun(context.Background(), params); err != nil || !plan.Carve || !strings.Contains(plan.Note, "files the rest") {
		t.Fatalf("a short chain is not planned: %+v, %v", plan, err)
	}
	item := rig.checkS1(t, localImplementer)
	if strings.Join(item.Carved, ",") != "S1a,S1b" {
		t.Fatalf("the next check files the rest: %+v", item)
	}
	if n := len(rig.requests()); n != 2 {
		t.Errorf("%d rounds, want a spec check and one carve; the second check files from the saved report", n)
	}
	if n := len(rig.slicerCalls("add")); n != 4 {
		t.Errorf("%d slicer add calls, want 4 (S1a, the failure, S1a again, S1b)", n)
	}
}

// A saved report that has no more slivers than are filed is no reason to carve
// or file again: the filed slivers are the chain.
func TestSavedCarveReportDoesNotRefileAFiledChain(t *testing.T) {
	rig := carveRig(t, "M", 1, nil, []string{"S1a", "S1b"})
	params := RunParams{Project: "p", Item: "S1", Lineup: localImplementer}
	if item := rig.checkS1(t, localImplementer); len(item.Carved) != 2 {
		t.Fatalf("carve: %+v", item)
	}
	if item := rig.checkS1(t, localImplementer); len(item.Carved) != 0 {
		t.Fatalf("a second check filed again: %+v", item)
	}
	plan, err := rig.d.planRun(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Carve || len(plan.Slivers) != 2 || len(rig.requests()) != 2 || len(rig.slicerCalls("add")) != 2 {
		t.Fatalf("a fully filed chain: carve %v, %d slivers, %d rounds, %d adds", plan.Carve, len(plan.Slivers), len(rig.requests()), len(rig.slicerCalls("add")))
	}
}

// A carve that cannot be read files nothing and starts nothing.
func TestCarveWithAMalformedReportFilesAndRunsNothing(t *testing.T) {
	rig := carveRig(t, "M", 1, nil, nil)
	rig.carve = "## Slivers\n\n### 1. Big\n\nFiles:\n- `a.go`\n- `b.go`\n- `c.go`\n\nFailing tests:\n- t\n\nCheck:\n- c\n\n## Owner needed\nnone\n"
	raw, _ := json.Marshal(SpecCheckParams{Project: "p", Items: []string{"S1"}, Lineups: map[string]map[string][]runrec.LineupEntry{"S1": localImplementer}})
	_, err := rig.d.handle(context.Background(), MethodSpecCheck, json.RawMessage(raw))
	if err == nil || !strings.Contains(err.Error(), "one or two files") {
		t.Fatalf("spec check: %v", err)
	}
	if len(rig.slicerCalls("add")) != 0 || len(rig.runsOf(t)) != 0 {
		t.Fatalf("filed or started after a refused carve: adds %v", rig.slicerCalls("add"))
	}
}
