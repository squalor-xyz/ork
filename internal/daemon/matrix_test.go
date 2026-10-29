// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

const matrixText = "# Relay roles\n\n| ID | Role | Agent |\n| --- | --- | --- |\n| r1 | Implementer | Claude Code |\n"

// matrixShow is the output of `slicer relay-roles show --json` for a matrix
// with one row of each kind ork reads differently.
func matrixShow(t *testing.T) string {
	t.Helper()
	row := func(id, role, agent string) map[string]any {
		return map[string]any{"id": id, "role": role, "agent": agent, "vendor": "V", "model": "m", "reasoning": "high", "note": ""}
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
	want := []MatrixRow{
		{"r1", "implementer", "Claude Code", "used as claude"},
		{"r2", "implementer", "Codex", "used as codex"},
		{"r3", "implementer", "Acme Agent", "skipped: no adapter for Acme Agent"},
		{"r5", "reviewer", "Claude Code", "used as claude"},
		{"r14", "reviewer", "Claude Code", "UI pass row, used as claude"},
		{"r16", "reviewer", "codex", "Spec check row, used as codex"},
		{"r6", "gate", "Claude Code", "not used: the gate runs the reviewer's lineup"},
		{"r7", "architect", "Codex", "used as codex"},
		{"r10", "orchestrator", "Claude Code", "not run by ork"},
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
			{"r1", "implementer", "Claude Code", "used as claude"},
			{"r6", "gate", "Claude Code", "not used: the gate runs the reviewer's lineup"},
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
		Rows:     []MatrixRow{{"r1", "implementer", "Claude Code", "used as claude"}},
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
