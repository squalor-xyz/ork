// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// S268 (*proposed*): grok has no write mode, so a plan skips a grok row of a
// write role (the implementer) and refuses a launch override that names one.

func grokRoles(implementer string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true,
		Stdout: `{"roles":[` + implementer + `,{"role":"reviewer","row":{"id":"r5","agent":"Claude Code","model":"opus","reasoning":"medium"}}]}`}
}

func TestPlanSkipsAGrokWriteRow(t *testing.T) {
	for _, agent := range []string{"Grok", "grok-local"} {
		t.Run(agent, func(t *testing.T) {
			d, _, _ := runDaemon(t, nil, grokRoles(`{"role":"implementer","row":{"id":"r22","agent":"`+agent+`","model":"grok-4.5","reasoning":"high"},"alternatives":[{"id":"r1","agent":"Claude Code","model":"sonnet","reasoning":"high"}]}`))
			localLaunchers(t)
			plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
			want := []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}}
			if !reflect.DeepEqual(plan.Lineup[engine.RoleImplementer], want) {
				t.Fatalf("implementer lineup %+v; want %+v", plan.Lineup[engine.RoleImplementer], want)
			}
			for _, part := range []string{"r22", "implementer", "no write mode"} {
				if !strings.Contains(plan.Note, part) {
					t.Errorf("plan note %q lacks %q", plan.Note, part)
				}
			}
		})
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

// A project whose implementer is the read-only preset gives it a read-only
// permission, which grok can run.
func TestPlanKeepsAGrokImplementerRowWhenTheImplementerIsReadOnly(t *testing.T) {
	root := t.TempDir()
	d, _, p := runDaemonIn(t, root, nil, grokRoles(`{"role":"implementer","row":{"id":"r22","agent":"Grok","model":"grok-4.5","reasoning":"high"}}`))
	p.Permissions = map[string]registry.Permission{engine.RoleImplementer: {Preset: "read-only"}}
	if err := registry.Save(filepath.Join(root, "projects.json"), &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	want := []runrec.LineupEntry{{Vendor: "grok", Model: "grok-4.5", Effort: "high"}}
	if !reflect.DeepEqual(plan.Lineup[engine.RoleImplementer], want) || !plan.Permissions[engine.RoleImplementer].ReadOnly {
		t.Fatalf("implementer lineup %+v, permission %+v; want %+v read-only", plan.Lineup[engine.RoleImplementer], plan.Permissions[engine.RoleImplementer], want)
	}
}

func TestPlanRefusesWhenEveryImplementerRowIsAGrokWriteRow(t *testing.T) {
	d, _, _ := runDaemon(t, nil, grokRoles(`{"role":"implementer","row":{"id":"r22","agent":"Grok","model":"grok-4.5","reasoning":"high"}}`))
	_, err := d.handle(context.Background(), MethodRunPlan, json.RawMessage(`{"project":"p","item":"S1"}`))
	if err == nil || !strings.Contains(err.Error(), "no write mode") || !strings.Contains(err.Error(), "r22") {
		t.Fatalf("error %v; want the skip reason naming r22", err)
	}
}

func TestPlanRefusesAGrokWriteOverrideWithBadParams(t *testing.T) {
	for _, vendor := range []string{"grok", "grok-local"} {
		for name, entries := range map[string]string{
			"alone":  `{"vendor":"` + vendor + `","model":"grok-4.5","effort":"high"}`,
			"second": `{"vendor":"claude","model":"sonnet","effort":"high"},{"vendor":"` + vendor + `","model":"grok-4.5","effort":"high"}`,
		} {
			t.Run(vendor+"/"+name, func(t *testing.T) {
				d, _, _ := runDaemon(t, nil)
				localLaunchers(t)
				_, err := d.handle(context.Background(), MethodRunPlan, json.RawMessage(`{"project":"p","item":"S1","lineup":{"implementer":[`+entries+`]}}`))
				e := ipc.ErrorOf(err)
				if e == nil || e.Code != CodeBadParams || !strings.Contains(e.Message, "no write mode") {
					t.Fatalf("error %v; want bad_params naming the missing write mode", err)
				}
			})
		}
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

// A read-only implementer can run on grok, so the matrix does not skip its row.
func TestMatrixKeepsAGrokImplementerRowWhenTheImplementerIsReadOnly(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"relay_roles": "x",
		"rows":        []map[string]any{{"id": "r21", "role": "implementer", "agent": "Grok", "vendor": "xAI", "model": "latest", "reasoning": "high"}},
		"rules":       []map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := readDaemon(t, matrixRoute(string(body)))
	p := registry.Project{Name: "p", Path: waveRoot, Permissions: map[string]registry.Permission{engine.RoleImplementer: {Preset: "read-only"}}}
	if err := registry.Save(d.cfg.RegistryPath, &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	var got RolesMatrixResult
	if err := json.Unmarshal(request(t, d, "roles.matrix", `{"project":"p"}`), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 1 || got.Rows[0].Use != "used as grok" {
		t.Fatalf("rows %+v; want used as grok", got.Rows)
	}
}

func TestMatrixSkipsAGrokWriteRow(t *testing.T) {
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
	var got RolesMatrixResult
	if err := json.Unmarshal(request(t, d, "roles.matrix", `{"project":"p"}`), &got); err != nil {
		t.Fatal(err)
	}
	uses := map[string]string{}
	for _, r := range got.Rows {
		uses[r.ID] = r.Use
	}
	want := map[string]string{
		"r21": "skipped: grok has no write mode",
		"r22": "skipped: grok has no write mode",
		"r23": "used as grok",
		"r24": "used as grok",
		"r25": "used as claude",
	}
	if !reflect.DeepEqual(uses, want) {
		t.Fatalf("uses\n got %v\nwant %v", uses, want)
	}
}
