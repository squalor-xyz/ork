// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestLocalMatrixRowsKeepTheirVendorIdentity(t *testing.T) {
	for _, vendor := range []string{"claude-local", "codex-local", "grok-local"} {
		t.Run(vendor, func(t *testing.T) {
			d, _, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true, Stdout: `{"roles":[{"role":"implementer","row":{"agent":"Claude Code","model":"sonnet","reasoning":"high"}},{"role":"reviewer","row":{"agent":"` + vendor + `","model":"latest","reasoning":"medium"}}]}`})
			localLaunchers(t)
			plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
			want := []runrec.LineupEntry{{Vendor: vendor, Model: "latest", Effort: "medium"}}
			if !reflect.DeepEqual(plan.Lineup[engine.RoleReviewer], want) {
				t.Fatalf("local lineup: %+v; want %+v", plan.Lineup[engine.RoleReviewer], want)
			}
		})
	}
}

func TestLocalMatrixUsePrintsTheLauncherName(t *testing.T) {
	for _, vendor := range []string{"claude-local", "codex-local", "grok-local"} {
		t.Run(vendor, func(t *testing.T) {
			localLaunchers(t)
			use := matrixUse(slicercli.Row{Role: engine.RoleReviewer, Agent: vendor}, "", nil, false)
			lines := FormatMatrix(&RolesMatrixResult{Rows: []MatrixRow{{ID: "r33", Role: engine.RoleReviewer, Use: use}}})
			if !strings.Contains(strings.Join(lines, "\n"), "used as "+vendor) {
				t.Fatalf("matrix text: %v", lines)
			}
		})
	}
}

func TestLocalMatrixSkipsMissingLauncherWithAReason(t *testing.T) {
	d, _, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true, Stdout: `{"roles":[{"role":"implementer","row":{"agent":"claude-local","model":"qwen3.8-27b","reasoning":"medium"},"alternatives":[{"agent":"Claude Code","model":"sonnet","reasoning":"high"}]},{"role":"reviewer","row":{"agent":"Claude Code","model":"opus","reasoning":"medium"}}]}`})
	t.Setenv("PATH", t.TempDir())
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	if len(plan.Lineup[engine.RoleImplementer]) != 1 || plan.Lineup[engine.RoleImplementer][0].Vendor != "claude" || !strings.Contains(plan.Note, "claude-local") || !strings.Contains(plan.Note, "PATH") {
		t.Fatalf("missing launcher plan: %+v", plan)
	}
}

func TestLocalOverrideSkipsMissingLauncherWithAReason(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	t.Setenv("PATH", t.TempDir())
	plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"reviewer":[{"vendor":"claude-local","model":"qwen3.8-27b","effort":"medium"},{"vendor":"claude","model":"opus","effort":"medium"}]}}`)
	if len(plan.Lineup[engine.RoleReviewer]) != 1 || plan.Lineup[engine.RoleReviewer][0].Vendor != "claude" || !strings.Contains(plan.Note, "claude-local") || !strings.Contains(plan.Note, "PATH") {
		t.Fatalf("missing explicit launcher plan: %+v", plan)
	}
}

func TestLocalMissingLauncherNotesAreStableAcrossPlans(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	t.Setenv("PATH", t.TempDir())
	const raw = `{"project":"p","item":"S1","lineup":{"implementer":[{"vendor":"claude-local","model":"qwen3.8-27b","effort":"medium"},{"vendor":"claude","model":"sonnet","effort":"high"}],"reviewer":[{"vendor":"grok-local","model":"latest","effort":"medium"},{"vendor":"claude","model":"opus","effort":"medium"}]}}`
	want := launchPlan(t, d, raw).Note
	for range 64 {
		if got := launchPlan(t, d, raw).Note; got != want {
			t.Fatalf("unchanged missing-launcher plan changed note: %q; want %q", got, want)
		}
	}
}

const matrixText = "# Relay roles\n\n| ID | Role | Agent |\n| --- | --- | --- |\n| r1 | Implementer | Claude Code |\n"

// matrixNote is the note of row r2 in matrixShow; the other rows have none.
const matrixNote = "second opinion"

// shownRow is the MatrixRow that a matrixShow row becomes: its vendor, model
// and reasoning are the same for every row.
func shownRow(id, role, agent, use string) MatrixRow {
	return MatrixRow{ID: id, Role: role, Agent: agent, Vendor: "V", Model: "m", Reasoning: "high", Use: use}
}

// matrixShow is the output of `slicer relay-roles show --json` for a matrix
// with one row of each kind ork reads differently.
func matrixShow(t *testing.T) string {
	t.Helper()
	row := func(id, role, agent string) map[string]any {
		note := ""
		if id == "r2" {
			note = matrixNote
		}
		return map[string]any{"id": id, "role": role, "agent": agent, "vendor": "V", "model": "m", "reasoning": "high", "note": note}
	}
	b, err := json.Marshal(map[string]any{
		"relay_roles": matrixText,
		"rows": []map[string]any{
			row("r1", "implementer", "Claude Code"),
			row("r2", "implementer", "Codex"),
			row("r3", "implementer", "Acme Agent"),
			row("r5", "reviewer", "Claude Code"),
			row("r14", "reviewer", "Claude Code"),
			row("r16", "reviewer", "codex"),
			row("r6", "gate", "Claude Code"),
			row("r7", "architect", "Codex"),
			row("r10", "orchestrator", "Claude Code"),
		},
		"rules": []map[string]string{
			{"id": "u10", "text": "A slice that touches the UI also runs the UI pass row (r14) as its own role."},
			{"id": "u11", "text": "The spec check before a wave runs the Spec check row (r16): one read-only pass."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func matrixRoute(stdout string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "show", "--json", "--root", waveRoot}, Stdout: stdout}
}

func TestRolesMatrixMarksEachRow(t *testing.T) {
	d := readDaemon(t, matrixRoute(matrixShow(t)))
	var got RolesMatrixResult
	if err := json.Unmarshal(request(t, d, "roles.matrix", `{"project":"p"}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Markdown != matrixText {
		t.Errorf("markdown %q, want slicer's text %q", got.Markdown, matrixText)
	}
	r2 := shownRow("r2", "implementer", "Codex", "used as codex")
	r2.Note = matrixNote
	want := []MatrixRow{
		shownRow("r1", "implementer", "Claude Code", "used as claude"),
		r2,
		shownRow("r3", "implementer", "Acme Agent", "skipped: no adapter for Acme Agent"),
		shownRow("r5", "reviewer", "Claude Code", "used as claude"),
		shownRow("r14", "reviewer", "Claude Code", "UI pass row, used as claude"),
		shownRow("r16", "reviewer", "codex", "Spec check row, used as codex"),
		shownRow("r6", "gate", "Claude Code", "not used: the gate runs the reviewer's lineup"),
		shownRow("r7", "architect", "Codex", "used as codex"),
		shownRow("r10", "orchestrator", "Claude Code", "not run by ork"),
	}
	if !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows\n got %+v\nwant %+v", got.Rows, want)
	}
	if len(got.Rules) != 2 || got.Rules[0].ID != "u10" || got.Rules[1].ID != "u11" {
		t.Errorf("rules %+v", got.Rules)
	}
}

func TestRolesMatrixSkipsAUIRowWithoutAnAdapter(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"relay_roles": "x",
		"rows":        []map[string]string{{"id": "r14", "role": "reviewer", "agent": "Acme Agent"}},
		"rules":       []map[string]string{{"id": "u10", "text": "The UI pass row (r14) runs beside the reviewer."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := readDaemon(t, matrixRoute(string(body)))
	var got RolesMatrixResult
	if err := json.Unmarshal(request(t, d, "roles.matrix", `{"project":"p"}`), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 1 || got.Rows[0].Use != "UI pass row, skipped: no adapter for Acme Agent" {
		t.Fatalf("rows %+v", got.Rows)
	}
}

func TestMatrixAdaptersMapTheGrokAgent(t *testing.T) {
	row := func(id, role, agent string) map[string]any {
		return map[string]any{"id": id, "role": role, "agent": agent, "vendor": "xAI", "model": "latest", "reasoning": "high", "note": ""}
	}
	body, err := json.Marshal(map[string]any{
		"relay_roles": "x",
		"rows": []map[string]any{
			row("r21", "orchestrator", "Grok"),
			row("r22", "reviewer", "Grok"),
			row("r23", "reviewer", "grok"),
			row("r24", "architect", "GROK"),
		},
		"rules": []map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if matrixAdapters["grok"] != "grok" {
		t.Fatalf("matrixAdapters %v", matrixAdapters)
	}
	d := readDaemon(t, matrixRoute(string(body)))
	var got RolesMatrixResult
	if err := json.Unmarshal(request(t, d, "roles.matrix", `{"project":"p"}`), &got); err != nil {
		t.Fatal(err)
	}
	want := []MatrixRow{
		{ID: "r21", Role: "orchestrator", Agent: "Grok", Vendor: "xAI", Model: "latest", Reasoning: "high", Use: "not run by ork"},
		{ID: "r22", Role: "reviewer", Agent: "Grok", Vendor: "xAI", Model: "latest", Reasoning: "high", Use: "used as grok"},
		{ID: "r23", Role: "reviewer", Agent: "grok", Vendor: "xAI", Model: "latest", Reasoning: "high", Use: "used as grok"},
		{ID: "r24", Role: "architect", Agent: "GROK", Vendor: "xAI", Model: "latest", Reasoning: "high", Use: "used as grok"},
	}
	if !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows\n got %+v\nwant %+v", got.Rows, want)
	}
}

func TestRolesMatrixUnknownProject(t *testing.T) {
	d := readDaemon(t, matrixRoute(matrixShow(t)))
	_, err := d.handle(context.Background(), "roles.matrix", json.RawMessage(`{"project":"nope"}`))
	if e := ipc.ErrorOf(err); e == nil || e.Code != CodeNotFound {
		t.Fatalf("error %v, want not_found", err)
	}
	if calls := lastFake.CallsTo("slicer"); len(calls) != 0 {
		t.Fatalf("slicer was called for an unregistered project: %+v", calls)
	}
}

func TestRolesMatrixNeedsAProject(t *testing.T) {
	d := readDaemon(t, matrixRoute(matrixShow(t)))
	_, err := d.handle(context.Background(), "roles.matrix", json.RawMessage(`{}`))
	if e := ipc.ErrorOf(err); e == nil || e.Code != CodeBadParams {
		t.Fatalf("error %v, want bad_params", err)
	}
}

func TestRolesMatrixSlicerErrorKeepsCodeAndMessage(t *testing.T) {
	d := readDaemon(t, clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "show", "--json", "--root", waveRoot}, Exit: 2,
		Stdout: `{"error":{"code":"usage","message":"matrix unreadable: no relay roles file"}}`})
	got, err := d.handle(context.Background(), "roles.matrix", json.RawMessage(`{"project":"p"}`))
	e := ipc.ErrorOf(err)
	if e == nil || e.Code != "usage" || e.Message != "matrix unreadable: no relay roles file" {
		t.Fatalf("error %+v, want slicer's code and message", e)
	}
	if res, _ := got.(*RolesMatrixResult); res != nil {
		t.Fatalf("partial result %+v", res)
	}
}

func TestRolesMatrixWritesNothing(t *testing.T) {
	d := readDaemon(t, matrixRoute(matrixShow(t)))
	request(t, d, "roles.matrix", `{"project":"p"}`)
	calls := lastFake.Calls()
	if len(calls) != 1 || calls[0].Tool != "slicer" || !reflect.DeepEqual(calls[0].Args, []string{"relay-roles", "show", "--json", "--root", waveRoot}) {
		t.Fatalf("calls %+v, want one relay-roles show", calls)
	}
}

// The client method over the socket: the result and slicer's error come back
// as the wire carries them.
func TestRolesMatrixOverTheSocket(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0, registry.Project{Name: "p", Path: waveRoot}, registry.Project{Name: "q", Path: "/p/q"})
	fake := clitest.Install(t,
		matrixRoute(matrixShow(t)),
		clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "show", "--json", "--root", "/p/q"}, Exit: 2, Stdout: `{"error":{"code":"usage","message":"no relay roles"}}`},
	)
	e.deps = &engine.Deps{Slicer: slicercli.Client{Bin: fake.Bin("slicer")}}
	e.start(t)
	c := e.dial(t)
	got, err := c.RolesMatrix(context.Background(), "p")
	if err != nil || got.Markdown != matrixText || len(got.Rows) != 9 || got.Rows[2].Use != "skipped: no adapter for Acme Agent" {
		t.Fatalf("matrix %+v, %v", got, err)
	}
	var re *ipc.Error
	if _, err := c.RolesMatrix(context.Background(), "q"); !errors.As(err, &re) || re.Code != "usage" || re.Message != "no relay roles" {
		t.Fatalf("slicer error over the socket: %v", err)
	}
	if _, err := c.RolesMatrix(context.Background(), "nope"); !errors.As(err, &re) || re.Code != CodeNotFound {
		t.Fatalf("unknown project: %v", err)
	}
}

func TestFormatMatrixPrintsSlicersTextThenTheOrkSection(t *testing.T) {
	got := FormatMatrix(&RolesMatrixResult{
		Markdown: "# Relay roles\n\nrow text\n",
		Rows: []MatrixRow{
			{ID: "r1", Role: "implementer", Agent: "Claude Code", Use: "used as claude"},
			{ID: "r6", Role: "gate", Agent: "Claude Code", Use: "not used: the gate runs the reviewer's lineup"},
		},
	})
	want := []string{
		"# Relay roles",
		"",
		"row text",
		"",
		"## ork",
		"- r1 implementer: used as claude",
		"- r6 gate: not used: the gate runs the reviewer's lineup",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines\n got %q\nwant %q", got, want)
	}
}

// The TUI shows ork's section above slicer's text; the CLI keeps slicer's text
// first. Both are the same lines in the other order.
func TestFormatMatrixOrkFirstPutsTheOrkSectionAboveSlicersText(t *testing.T) {
	r := &RolesMatrixResult{
		Markdown: "# Relay roles\n\nrow text\n",
		Rows:     []MatrixRow{{ID: "r1", Role: "implementer", Agent: "Claude Code", Use: "used as claude"}},
	}
	want := []string{
		"## ork",
		"- r1 implementer: used as claude",
		"",
		"# Relay roles",
		"",
		"row text",
	}
	if got := FormatMatrixOrkFirst(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("lines\n got %q\nwant %q", got, want)
	}
	if got := FormatMatrixOrkFirst(&RolesMatrixResult{Rows: r.Rows}); !reflect.DeepEqual(got, want[:2]) {
		t.Fatalf("with no slicer text: %q", got)
	}
	if got := FormatMatrix(r); got[0] != "# Relay roles" {
		t.Fatalf("the CLI order changed: %q", got)
	}
}

// Every row that the project's setting names, and the row of the rule, is a
// spec check row; a row nothing names is not.
func TestMatrixLabelsEverySpecCheckRow(t *testing.T) {
	row := func(id, role, agent string) map[string]any {
		return map[string]any{"id": id, "role": role, "agent": agent, "vendor": "V", "model": "m", "reasoning": "high", "note": ""}
	}
	body, err := json.Marshal(map[string]any{
		"relay_roles": "x",
		"rows": []map[string]any{
			row("r5", "reviewer", "Claude Code"),
			row("r16", "reviewer", "Claude Code"),
			row("r24", "reviewer", "Grok"),
		},
		"rules": []map[string]string{{"id": "u11", "text": "The spec check before a wave runs the Spec check row (r16): one read-only pass."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := readDaemon(t, matrixRoute(string(body)))
	setSpecCheckRows(t, d, "r24")
	var got RolesMatrixResult
	if err := json.Unmarshal(request(t, d, "roles.matrix", `{"project":"p"}`), &got); err != nil {
		t.Fatal(err)
	}
	want := []MatrixRow{
		shownRow("r5", "reviewer", "Claude Code", "used as claude"),
		shownRow("r16", "reviewer", "Claude Code", "Spec check row, used as claude"),
		shownRow("r24", "reviewer", "Grok", "Spec check row, used as grok"),
	}
	if !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows\n got %+v\nwant %+v", got.Rows, want)
	}
}

// roles.matrix rows carry the fields the editor prefills; a note shows only
// when the row has one.
func TestRolesMatrixRowsCarryVendorModelReasoningAndNote(t *testing.T) {
	d := readDaemon(t, matrixRoute(matrixShow(t)))
	var raw struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(request(t, d, "roles.matrix", `{"project":"p"}`), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Rows) != 9 {
		t.Fatalf("rows %+v", raw.Rows)
	}
	for _, row := range raw.Rows {
		for key, want := range map[string]string{"vendor": "V", "model": "m", "reasoning": "high"} {
			if row[key] != want {
				t.Errorf("row %v: %s is %v, want %q", row["id"], key, row[key], want)
			}
		}
		note, has := row["note"]
		if row["id"] == "r2" && note != matrixNote {
			t.Errorf("row r2 note %v", note)
		}
		if row["id"] != "r2" && has {
			t.Errorf("row %v has a note key %v; an empty note is omitted", row["id"], note)
		}
	}
}

// editRoute answers any relay-roles call with stdout.
func editRoute(stdout string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"relay-roles"}, Prefix: true, Stdout: stdout}
}

func editResult(t *testing.T, d *Daemon, raw string) MatrixEditResult {
	t.Helper()
	var got MatrixEditResult
	if err := json.Unmarshal(request(t, d, "matrix.edit", raw), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestMatrixEditRunsTheSlicerCommand(t *testing.T) {
	d, store, p := runDaemon(t, nil, editRoute(`{"id":"r5"}`))
	got := editResult(t, d, `{"project":"p","op":"set","id":"r5","fields":{"model":"opus"}}`)
	want := MatrixEditResult{Command: "slicer relay-roles set r5 --model=opus --render", Done: true}
	if got != want {
		t.Fatalf("result %+v, want %+v", got, want)
	}
	calls := lastFake.CallsTo("slicer")
	argv := []string{"relay-roles", "set", "r5", "--model=opus", "--render", "--json", "--root", p.Path}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, argv) {
		t.Fatalf("calls %+v, want one %v", calls, argv)
	}
	if runs, err := store.List(); err != nil || len(runs) != 0 {
		t.Fatalf("run records %v, %v", runs, err)
	}
}

func TestMatrixEditPreviewRunsNothing(t *testing.T) {
	d := readDaemon(t, editRoute(`{"id":"r5"}`))
	got := editResult(t, d, `{"project":"p","op":"set","id":"r5","fields":{"model":"opus"},"preview":true}`)
	want := MatrixEditResult{Command: "slicer relay-roles set r5 --model=opus --render"}
	if got != want {
		t.Fatalf("result %+v, want %+v", got, want)
	}
	if calls := lastFake.CallsTo("slicer"); len(calls) != 0 {
		t.Fatalf("a preview called slicer: %+v", calls)
	}
}

func TestMatrixEditPassesSlicersRefusalThrough(t *testing.T) {
	d := readDaemon(t, clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "set"}, Prefix: true, Exit: 2,
		Stdout: `{"error":{"code":"unknown_role_row","message":"no such relay role row: r99"}}`})
	got, err := d.handle(context.Background(), "matrix.edit", json.RawMessage(`{"project":"p","op":"set","id":"r99","fields":{"model":"opus"}}`))
	e := ipc.ErrorOf(err)
	if e == nil || e.Code != "unknown_role_row" || e.Message != "no such relay role row: r99" {
		t.Fatalf("error %+v, want slicer's code and message", e)
	}
	if res, _ := got.(*MatrixEditResult); res != nil {
		t.Fatalf("partial result %+v", res)
	}
}

func TestMatrixEditRefusesAnUnregisteredProject(t *testing.T) {
	d := readDaemon(t, editRoute(`{"id":"r5"}`))
	_, err := d.handle(context.Background(), "matrix.edit", json.RawMessage(`{"project":"nope","op":"remove","id":"r5"}`))
	if e := ipc.ErrorOf(err); e == nil || e.Code != CodeNotFound {
		t.Fatalf("error %v, want not_found", err)
	}
	if calls := lastFake.CallsTo("slicer"); len(calls) != 0 {
		t.Fatalf("slicer was called for an unregistered project: %+v", calls)
	}
}

func TestMatrixEditRefusesBadRequests(t *testing.T) {
	for name, raw := range map[string]string{
		"missing project":       `{"op":"remove","id":"r5"}`,
		"unknown op":            `{"project":"p","op":"rule-add","id":"u1","text":"x"}`,
		"no op":                 `{"project":"p","id":"r5"}`,
		"set without id":        `{"project":"p","op":"set","fields":{"model":"x"}}`,
		"remove without id":     `{"project":"p","op":"remove"}`,
		"move without id":       `{"project":"p","op":"move","last":true}`,
		"rule-edit without id":  `{"project":"p","op":"rule-edit","text":"x"}`,
		"add without role":      `{"project":"p","op":"add","fields":{"agent":"Codex"}}`,
		"add without fields":    `{"project":"p","op":"add"}`,
		"set without fields":    `{"project":"p","op":"set","id":"r5"}`,
		"move with both":        `{"project":"p","op":"move","id":"r5","before":"r2","last":true}`,
		"move with neither":     `{"project":"p","op":"move","id":"r5"}`,
		"field outside list":    `{"project":"p","op":"set","id":"r5","fields":{"colour":"red"}}`,
		"fields on remove":      `{"project":"p","op":"remove","id":"r5","fields":{"model":"x"}}`,
		"blank text":            `{"project":"p","op":"rule-edit","id":"u3","text":"  "}`,
		"dash text without gap": `{"project":"p","op":"rule-edit","id":"u3","text":"-x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			d := readDaemon(t, editRoute(`{}`))
			_, err := d.handle(context.Background(), "matrix.edit", json.RawMessage(raw))
			if e := ipc.ErrorOf(err); e == nil || e.Code != CodeBadParams {
				t.Fatalf("error %v, want bad_params", err)
			}
			if calls := lastFake.CallsTo("slicer"); len(calls) != 0 {
				t.Fatalf("a bad request called slicer: %+v", calls)
			}
		})
	}
}

func TestMatrixEditWarnsOnAnAgentWithoutAnAdapter(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		warn string
	}{
		{`{"project":"p","op":"set","id":"r5","fields":{"agent":"Acme Agent"}}`, `agent "Acme Agent" has no ork adapter: a plan skips this row`},
		{`{"project":"p","op":"add","fields":{"role":"reviewer","agent":"Acme Agent"}}`, `agent "Acme Agent" has no ork adapter: a plan skips this row`},
		{`{"project":"p","op":"set","id":"r5","fields":{"agent":"Claude Code"}}`, ""},
		{`{"project":"p","op":"set","id":"r5","fields":{"agent":"codex"}}`, ""},
		{`{"project":"p","op":"set","id":"r5","fields":{"agent":"Codex"}}`, ""},
		{`{"project":"p","op":"set","id":"r5","fields":{"model":"opus"}}`, ""},
		{`{"project":"p","op":"remove","id":"r5"}`, ""},
	} {
		d := readDaemon(t, editRoute(`{"id":"r5"}`))
		got := editResult(t, d, tc.raw)
		if got.Warning != tc.warn || !got.Done {
			t.Errorf("%s: warning %q done %v, want warning %q and done", tc.raw, got.Warning, got.Done, tc.warn)
		}
		if calls := lastFake.CallsTo("slicer"); len(calls) != 1 {
			t.Errorf("%s: the edit did not run once: %+v", tc.raw, calls)
		}
	}
}

func TestMatrixEditAddReturnsTheNewRowID(t *testing.T) {
	d := readDaemon(t, editRoute(`{"id":"r17"}`))
	got := editResult(t, d, `{"project":"p","op":"add","fields":{"role":"reviewer","agent":"Codex"}}`)
	want := MatrixEditResult{Command: "slicer relay-roles add --role=reviewer --agent=Codex --render", ID: "r17", Done: true}
	if got != want {
		t.Fatalf("result %+v, want %+v", got, want)
	}
	// A preview has no row yet.
	if got := editResult(t, d, `{"project":"p","op":"add","fields":{"role":"reviewer"},"preview":true}`); got.ID != "" || got.Done {
		t.Fatalf("preview result %+v", got)
	}
}

func TestMatrixEditQuotesValuesInTheCommand(t *testing.T) {
	for _, tc := range []struct{ raw, command, arg string }{
		{`{"project":"p","op":"set","id":"r5","fields":{"note":"two words"}}`, "slicer relay-roles set r5 --note='two words' --render", "--note=two words"},
		{`{"project":"p","op":"set","id":"r5","fields":{"note":"it's"}}`, `slicer relay-roles set r5 --note='it'\''s' --render`, "--note=it's"},
		{`{"project":"p","op":"set","id":"r5","fields":{"note":"-x","model":""}}`, "slicer relay-roles set r5 --model= --note=-x --render", "--note=-x"},
		{`{"project":"p","op":"rule-edit","id":"u3","text":"- two words"}`, "slicer relay-roles rule-edit u3 '- two words' --render", "- two words"},
	} {
		d := readDaemon(t, editRoute(`{}`))
		got := editResult(t, d, tc.raw)
		if got.Command != tc.command {
			t.Errorf("%s: command %q, want %q", tc.raw, got.Command, tc.command)
		}
		calls := lastFake.CallsTo("slicer")
		if len(calls) != 1 || !slices.Contains(calls[0].Args, tc.arg) {
			t.Errorf("%s: argv %+v lacks the raw %q", tc.raw, calls, tc.arg)
		}
	}
}

// The client call over the socket: the result and slicer's error come back as
// the wire carries them.
func TestMatrixEditOverTheSocket(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0, registry.Project{Name: "p", Path: waveRoot}, registry.Project{Name: "q", Path: "/p/q"})
	fake := clitest.Install(t,
		clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "set", "r5", "--model=opus", "--render", "--json", "--root", waveRoot}, Stdout: `{"id":"r5"}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "set", "r99", "--model=opus", "--render", "--json", "--root", "/p/q"}, Exit: 2,
			Stdout: `{"error":{"code":"unknown_role_row","message":"no such relay role row: r99"}}`},
	)
	e.deps = &engine.Deps{Slicer: slicercli.Client{Bin: fake.Bin("slicer")}}
	e.start(t)
	c := e.dial(t)
	got, err := c.MatrixEdit(context.Background(), MatrixEditParams{Project: "p", Op: "set", ID: "r5", Fields: slicercli.RowFields{"model": "opus"}})
	if err != nil || got.Command != "slicer relay-roles set r5 --model=opus --render" || !got.Done {
		t.Fatalf("edit %+v, %v", got, err)
	}
	var re *ipc.Error
	if _, err := c.MatrixEdit(context.Background(), MatrixEditParams{Project: "q", Op: "set", ID: "r99", Fields: slicercli.RowFields{"model": "opus"}}); !errors.As(err, &re) || re.Code != "unknown_role_row" || re.Message != "no such relay role row: r99" {
		t.Fatalf("slicer error over the socket: %v", err)
	}
	if _, err := c.MatrixEdit(context.Background(), MatrixEditParams{Project: "nope", Op: "remove", ID: "r5"}); !errors.As(err, &re) || re.Code != CodeNotFound {
		t.Fatalf("unknown project: %v", err)
	}
	if _, err := c.MatrixEdit(context.Background(), MatrixEditParams{Project: "p", Op: "remove"}); !errors.As(err, &re) || re.Code != CodeBadParams {
		t.Fatalf("bad request: %v", err)
	}
}
