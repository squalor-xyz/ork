// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func claudeRow(model, effort string) string {
	return `{"agent":"Claude Code","model":"` + model + `","reasoning":"` + effort + `"}`
}

// splitRoutes answers `slicer relay-roles recommend` by its --split flag: the
// lead's rows (split no) and the parts' rows (split yes) differ.
func splitRoutes(lead, parts string) []clitest.Route {
	recommend := func(split, roles string) clitest.Route {
		return clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend", "S1", "--well-specified", "no", "--split", split}, Prefix: true, Stdout: roles}
	}
	return []clitest.Route{recommend("yes", parts), recommend("no", lead), rolesRoute(lead)}
}

func recommendation(implementer, reviewer, architect string) string {
	return `{"roles":[{"role":"implementer","row":` + implementer + `},{"role":"reviewer","row":` + reviewer + `},{"role":"architect","row":` + architect + `,"alternatives":[]}]}`
}

// With --split (S61, *proposed*) the plan asks the matrix twice: split no for the
// lead, which has the implementer's slot, and split yes for the parts.
func TestSplitPlanAsksTheMatrixForTheLeadAndForTheParts(t *testing.T) {
	lead := recommendation(claudeRow("sonnet", "high"), claudeRow("opus", "medium"), "null")
	parts := recommendation(claudeRow("haiku", "medium"), claudeRow("opus", "medium"), "null")
	d, _, _ := runDaemon(t, nil, splitRoutes(lead, parts)...)
	plan := launchPlan(t, d, `{"project":"p","item":"S1","split":true}`)
	if !plan.Split {
		t.Fatal("the plan of a split run is not marked split")
	}
	if got := plan.Lineup[engine.RoleImplementer]; len(got) != 1 || got[0].Model != "sonnet" || got[0].Effort != "high" {
		t.Errorf("lead (the implementer's chain) %+v, want the split no row", got)
	}
	if got := plan.Lineup[engine.RolePart]; len(got) != 1 || got[0].Model != "haiku" || got[0].Effort != "medium" {
		t.Errorf("part chain %+v, want the split yes row", got)
	}
	perm, ok := plan.Permissions[engine.RolePart]
	if !ok || perm.ReadOnly || !slices.Contains(perm.Allow, "Edit") {
		t.Fatalf("part permission %+v", perm)
	}
	if slices.Contains(perm.Allow, "Bash(slicer *)") {
		t.Errorf("a part may run any slicer command: %v", perm.Allow)
	}
	if !slices.Contains(plan.Permissions[engine.RoleImplementer].Allow, "Bash(slicer *)") {
		t.Errorf("the lead lost its slicer commands: %v", plan.Permissions[engine.RoleImplementer].Allow)
	}
}

func TestPlanWithoutSplitHasNoPartRow(t *testing.T) {
	lead := recommendation(claudeRow("sonnet", "high"), claudeRow("opus", "medium"), "null")
	parts := recommendation(claudeRow("haiku", "medium"), claudeRow("opus", "medium"), "null")
	d, _, _ := runDaemon(t, nil, splitRoutes(lead, parts)...)
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	if _, ok := plan.Lineup[engine.RolePart]; ok || plan.Split {
		t.Fatalf("a run with no split has a part row: split %v, lineup %+v", plan.Split, plan.Lineup)
	}
	if _, ok := plan.Permissions[engine.RolePart]; ok {
		t.Fatal("a run with no split has a part permission")
	}
}

// The matrix gives the architect to size L only, and the lead's call (split no)
// carries it: a parts row's architect choice is not read.
func TestSplitPlanArchitectFollowsTheLeadsMatrixRow(t *testing.T) {
	architect := `{"agent":"Claude Code","model":"opus","reasoning":"high"}`
	for _, tc := range []struct {
		name            string
		lead, parts     string
		wantArchitect   bool
		wantArchitectIs string
	}{
		{"size M: the lead's row is unmatched", "null", architect, false, ""},
		{"size L: the lead's row is an architect", architect, "null", true, "opus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lead := recommendation(claudeRow("sonnet", "high"), claudeRow("opus", "medium"), tc.lead)
			parts := recommendation(claudeRow("haiku", "medium"), claudeRow("opus", "medium"), tc.parts)
			d, _, _ := runDaemon(t, nil, splitRoutes(lead, parts)...)
			plan := launchPlan(t, d, `{"project":"p","item":"S1","split":true}`)
			got, present := plan.Lineup[engine.RoleArchitect]
			if present != tc.wantArchitect || (present && got[0].Model != tc.wantArchitectIs) {
				t.Fatalf("architect %+v (present %v), want present %v", got, present, tc.wantArchitect)
			}
		})
	}
}

// A plan the owner confirmed is refused when it changed since (S161).
func TestSplitPlanChangedSinceTheConfirmIsRefused(t *testing.T) {
	lead := recommendation(claudeRow("sonnet", "high"), claudeRow("opus", "medium"), "null")
	parts := recommendation(claudeRow("haiku", "medium"), claudeRow("opus", "medium"), "null")
	d, _, _ := runDaemon(t, nil, splitRoutes(lead, parts)...)
	plan := launchPlan(t, d, `{"project":"p","item":"S1","split":true}`)
	for name, change := range map[string]func(*RunPlan){
		"the part row": func(p *RunPlan) {
			if len(p.Lineup[engine.RolePart]) == 0 {
				t.Fatal("the plan of a split run has no part row")
			}
			p.Lineup[engine.RolePart][0].Model = "sonnet"
		},
		"the split bit": func(p *RunPlan) { p.Split = false },
	} {
		t.Run(name, func(t *testing.T) {
			shown := plan
			shown.Lineup = map[string][]runrec.LineupEntry{}
			for role, entries := range plan.Lineup {
				shown.Lineup[role] = slices.Clone(entries)
			}
			change(&shown)
			raw, err := json.Marshal(RunParams{Project: "p", Item: "S1", Split: true, Expected: &shown})
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.handle(context.Background(), MethodRunStart, raw)
			if err == nil || !strings.Contains(err.Error(), "plan changed") {
				t.Fatalf("a changed plan was not refused: %v", err)
			}
		})
	}
}

// A confirmed split start makes a split run: the record says so and keeps the
// part chain the plan showed.
func TestSplitStartRecordsTheSplitAndThePartChain(t *testing.T) {
	lead := recommendation(claudeRow("sonnet", "high"), claudeRow("opus", "medium"), "null")
	parts := recommendation(claudeRow("haiku", "medium"), claudeRow("opus", "medium"), "null")
	d, store, _ := runDaemon(t, func(ctx context.Context, _ engine.Request) (engine.Result, error) {
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	}, splitRoutes(lead, parts)...)
	plan := launchPlan(t, d, `{"project":"p","item":"S1","split":true}`)
	shown, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var rec runrec.Record
	if err := json.Unmarshal(request(t, d, MethodRunStart, `{"project":"p","item":"S1","split":true,"expected":`+string(shown)+`}`), &rec); err != nil {
		t.Fatal(err)
	}
	run, err := store.Open(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := run.Snapshot()
	if !got.Split || len(got.Lineup[engine.RolePart]) != 1 || got.Lineup[engine.RolePart][0].Model != "haiku" {
		t.Fatalf("the run record is not the split plan: split %v, lineup %+v", got.Split, got.Lineup)
	}
}

// A part override needs a split run.
func TestPartLineupOverrideNeedsASplitRun(t *testing.T) {
	lead := recommendation(claudeRow("sonnet", "high"), claudeRow("opus", "medium"), "null")
	parts := recommendation(claudeRow("haiku", "medium"), claudeRow("opus", "medium"), "null")
	d, _, _ := runDaemon(t, nil, splitRoutes(lead, parts)...)
	raw := `"lineup":{"part":[{"vendor":"claude","model":"custom","effort":"low"}]}`
	if _, err := d.handle(context.Background(), MethodRunPlan, json.RawMessage(`{"project":"p","item":"S1",`+raw+`}`)); err == nil || !strings.Contains(err.Error(), `cannot override lineup role "part"`) {
		t.Fatalf("a part override without split: %v", err)
	}
	plan := launchPlan(t, d, `{"project":"p","item":"S1","split":true,`+raw+`}`)
	if got := plan.Lineup[engine.RolePart]; len(got) != 1 || got[0].Model != "custom" {
		t.Fatalf("part chain %+v", got)
	}
}
