// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// S305 (*proposed*; adj 373): grok runs a write role in dontAsk mode with allow
// rules, so a plan keeps a grok row of the implementer and accepts a launch
// override that names one. S268's skip is gone.

func grokRoles(implementer string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true,
		Stdout: `{"roles":[` + implementer + `,{"role":"reviewer","row":{"id":"r5","agent":"Claude Code","model":"opus","reasoning":"medium"}}]}`}
}

func TestPlanKeepsAGrokWriteRow(t *testing.T) {
	for _, agent := range []string{"Grok", "grok-local"} {
		t.Run(agent, func(t *testing.T) {
			d, _, _ := runDaemon(t, nil, grokRoles(`{"role":"implementer","row":{"id":"r22","agent":"`+agent+`","model":"grok-4.5","reasoning":"high"},"alternatives":[{"id":"r1","agent":"Claude Code","model":"sonnet","reasoning":"high"}]}`))
			localLaunchers(t)
			plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
			vendor := strings.ToLower(agent)
			want := []runrec.LineupEntry{{Vendor: vendor, Model: "grok-4.5", Effort: "high"}, {Vendor: "claude", Model: "sonnet", Effort: "high"}}
			if !reflect.DeepEqual(plan.Lineup[engine.RoleImplementer], want) || plan.Permissions[engine.RoleImplementer].ReadOnly {
				t.Fatalf("implementer lineup %+v, permission %+v; want %+v and a write permission", plan.Lineup[engine.RoleImplementer], plan.Permissions[engine.RoleImplementer], want)
			}
			if strings.Contains(plan.Note, "write") || strings.Contains(plan.Note, "r22") {
				t.Errorf("plan note %q skips the grok row", plan.Note)
			}
		})
	}
}

func TestPlanAllowsAGrokWriteOnlyImplementerRow(t *testing.T) {
	d, _, _ := runDaemon(t, nil, grokRoles(`{"role":"implementer","row":{"id":"r22","agent":"Grok","model":"grok-4.5","reasoning":"high"}}`))
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	want := []runrec.LineupEntry{{Vendor: "grok", Model: "grok-4.5", Effort: "high"}}
	if !reflect.DeepEqual(plan.Lineup[engine.RoleImplementer], want) {
		t.Fatalf("implementer lineup %+v; want %+v", plan.Lineup[engine.RoleImplementer], want)
	}
}

func TestPlanAllowsAGrokWriteOverride(t *testing.T) {
	for _, vendor := range []string{"grok", "grok-local"} {
		for name, entries := range map[string]string{
			"alone":  `{"vendor":"` + vendor + `","model":"grok-4.5","effort":"high"}`,
			"second": `{"vendor":"claude","model":"sonnet","effort":"high"},{"vendor":"` + vendor + `","model":"grok-4.5","effort":"high"}`,
		} {
			t.Run(vendor+"/"+name, func(t *testing.T) {
				d, _, _ := runDaemon(t, nil)
				localLaunchers(t)
				plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"implementer":[`+entries+`]}}`)
				var vendors []string
				for _, e := range plan.Lineup[engine.RoleImplementer] {
					vendors = append(vendors, e.Vendor)
				}
				if !strings.Contains(strings.Join(vendors, ","), vendor) || plan.Permissions[engine.RoleImplementer].ReadOnly {
					t.Fatalf("implementer lineup %+v; want %s kept", plan.Lineup[engine.RoleImplementer], vendor)
				}
			})
		}
	}
}

func TestPlanKeepsAGrokReadOnlyRow(t *testing.T) {
	d, _, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true,
		Stdout: `{"roles":[{"role":"implementer","row":{"id":"r1","agent":"Claude Code","model":"sonnet","reasoning":"high"}},{"role":"reviewer","row":{"id":"r22","agent":"Grok","model":"grok-4.5","reasoning":"high"}}]}`})
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	want := []runrec.LineupEntry{{Vendor: "grok", Model: "grok-4.5", Effort: "high"}}
	if !reflect.DeepEqual(plan.Lineup[engine.RoleReviewer], want) || strings.Contains(plan.Note, "write") {
		t.Fatalf("reviewer lineup %+v, note %q; want %+v and no write note", plan.Lineup[engine.RoleReviewer], plan.Note, want)
	}
}

func TestPlanAllowsAGrokReadOnlyOverride(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"reviewer":[{"vendor":"grok","model":"grok-4.5","effort":"high"}],"architect":[{"vendor":"grok","model":"grok-4.5","effort":"high"}]}}`)
	want := []runrec.LineupEntry{{Vendor: "grok", Model: "grok-4.5", Effort: "high"}}
	if !reflect.DeepEqual(plan.Lineup[engine.RoleReviewer], want) || !reflect.DeepEqual(plan.Lineup[engine.RoleArchitect], want) {
		t.Fatalf("lineup %+v; want grok reviewer and architect", plan.Lineup)
	}
}

// `ork roles matrix` no longer marks a grok write row as skipped.
func TestMatrixUsesAGrokWriteRow(t *testing.T) {
	row := func(id, role, agent string) map[string]any {
		return map[string]any{"id": id, "role": role, "agent": agent, "vendor": "xAI", "model": "latest", "reasoning": "high", "note": ""}
	}
	body, err := json.Marshal(map[string]any{
		"relay_roles": "x",
		"rows": []map[string]any{
			row("r21", "implementer", "Grok"),
			row("r22", "implementer", "grok-local"),
			row("r23", "reviewer", "Grok"),
			row("r24", "architect", "Grok"),
			row("r25", "implementer", "Claude Code"),
		},
		"rules": []map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := readDaemon(t, matrixRoute(string(body)))
	localLaunchers(t)
	var got RolesMatrixResult
	if err := json.Unmarshal(request(t, d, "roles.matrix", `{"project":"p"}`), &got); err != nil {
		t.Fatal(err)
	}
	uses := map[string]string{}
	for _, r := range got.Rows {
		uses[r.ID] = r.Use
	}
	want := map[string]string{
		"r21": "used as grok",
		"r22": "used as grok-local",
		"r23": "used as grok",
		"r24": "used as grok",
		"r25": "used as claude",
	}
	if !reflect.DeepEqual(uses, want) {
		t.Fatalf("uses\n got %v\nwant %v", uses, want)
	}
}
