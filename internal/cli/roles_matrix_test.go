// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

var matrixResult = daemon.RolesMatrixResult{
	Markdown: "# Relay roles\n\n| ID | Role |\n| r1 | Implementer |\n",
	Rows: []daemon.MatrixRow{
		{ID: "r1", Role: "implementer", Agent: "Claude Code", Use: "used as claude"},
		{ID: "r6", Role: "gate", Agent: "Claude Code", Use: "not used: the gate runs the reviewer's lineup"},
	},
	Rules: []slicercli.Rule{{ID: "u1", Text: "A reviewer never wrote code."}},
}

// matrixApp answers roles.matrix for project ork and fails the test on any
// other call.
func matrixApp(t *testing.T, calls *[]any) *App {
	t.Helper()
	return &App{Call: func(_ context.Context, method string, params, result any) error {
		if method != daemon.MethodRolesMatrix {
			t.Errorf("unexpected method %s", method)
		}
		if calls != nil {
			*calls = append(*calls, params)
		}
		if p, ok := params.(daemon.RolesMatrixParams); !ok || p.Project != "ork" {
			return &ipc.Error{Code: daemon.CodeNotFound, Message: `no project named "nope"`}
		}
		b, err := json.Marshal(matrixResult)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, result)
	}}
}

func TestRolesMatrixCommandPrintsSlicersTextThenTheOrkSection(t *testing.T) {
	var calls []any
	code, stdout, stderr := run(t, matrixApp(t, &calls), "roles", "matrix", "ork")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	if !reflect.DeepEqual(calls, []any{daemon.RolesMatrixParams{Project: "ork"}}) {
		t.Fatalf("calls %+v", calls)
	}
	if !strings.HasPrefix(stdout, matrixResult.Markdown+"\n## ork\n") {
		t.Fatalf("slicer's text is not first, as slicer prints it:\n%s", stdout)
	}
	want := "- r1 implementer: used as claude\n- r6 gate: not used: the gate runs the reviewer's lineup\n"
	if !strings.HasSuffix(stdout, "\n## ork\n"+want) {
		t.Fatalf("the ork section does not end the output:\n%s", stdout)
	}
}

func TestRolesMatrixCommandJSON(t *testing.T) {
	code, stdout, stderr := run(t, matrixApp(t, nil), "roles", "matrix", "ork", "--json")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	var got daemon.RolesMatrixResult
	dec := json.NewDecoder(strings.NewReader(stdout))
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout)
	}
	if dec.More() {
		t.Fatalf("more than one value:\n%s", stdout)
	}
	if !reflect.DeepEqual(got, matrixResult) {
		t.Fatalf("got %+v, want the daemon's result %+v", got, matrixResult)
	}
}

func TestRolesMatrixCommandUsage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"no project", []string{"roles", "matrix"}, ExitUsage, "PROJECT"},
		{"two projects", []string{"roles", "matrix", "ork", "other"}, ExitUsage, "PROJECT"},
		{"unknown project", []string{"roles", "matrix", "nope"}, ExitFailure, `no project named "nope"`},
		{"no subcommand", []string{"roles"}, ExitUsage, "matrix"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := run(t, matrixApp(t, nil), tc.args...)
			if code != tc.code || stdout != "" || !strings.Contains(stderr, tc.want) {
				t.Fatalf("exit %d, want %d\nstdout: %q\nstderr: %q", code, tc.code, stdout, stderr)
			}
		})
	}
	code, stdout, _ := run(t, matrixApp(t, nil), "roles", "matrix", "nope", "--json")
	var e struct {
		Error struct{ Code, Message string }
	}
	mustJSON(t, stdout, &e)
	if code != ExitFailure || e.Error.Code != daemon.CodeNotFound {
		t.Fatalf("--json: exit %d\n%s", code, stdout)
	}
}

func TestRolesPrintStillNeedsNoDaemon(t *testing.T) {
	app := &App{Call: func(_ context.Context, method string, _, _ any) error {
		t.Errorf("roles print called the daemon: %s", method)
		return nil
	}, Start: func(context.Context) error { t.Error("a daemon was started for roles print"); return nil }}
	for _, args := range [][]string{{"roles", "print"}, {"roles", "print", "gate"}, {"roles", "print", "--json"}} {
		if code, stdout, stderr := run(t, app, args...); code != ExitOK || stdout == "" {
			t.Errorf("%v: exit %d\nstdout: %q\nstderr: %q", args, code, stdout, stderr)
		}
	}
}

// ---- roles matrix copy (S261) ----

// copyApp answers matrix.copy with the result for the call: dry for a dry run,
// written otherwise. It records every call's params and fails the test on any
// other method.
func copyApp(t *testing.T, calls *[]daemon.MatrixCopyParams, dry, written daemon.MatrixCopyResult, input string) *App {
	t.Helper()
	return &App{Input: strings.NewReader(input), Call: func(_ context.Context, method string, params, result any) error {
		if method != daemon.MethodMatrixCopy {
			t.Errorf("unexpected method %s", method)
		}
		p, ok := params.(daemon.MatrixCopyParams)
		if !ok {
			t.Fatalf("params %T", params)
		}
		*calls = append(*calls, p)
		res := written
		if p.DryRun {
			res = dry
		}
		b, err := json.Marshal(res)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, result)
	}}
}

var (
	copyStepsRemove = []daemon.MatrixCopyStep{{Op: "remove", ID: "r9", Changed: true}, {Op: "add", ID: "r21", Changed: true}, {Op: "rule-add", ID: "u10", Changed: true}}
	noAdapterWarn   = `agent "Acme" has no ork adapter: a plan skips this row`
)

func wouldChange(project string) daemon.MatrixCopyProject {
	return daemon.MatrixCopyProject{Project: project, Status: daemon.CopyWouldChange, Steps: copyStepsRemove, Digest: digestOf(project)}
}

// digestOf is the digest the preview of a project carries in these tests.
func digestOf(project string) string { return "digest-" + project }

func TestRolesMatrixCopyDryRunPrintsEachProject(t *testing.T) {
	var calls []daemon.MatrixCopyParams
	p := wouldChange("adjudicate")
	p.Warnings = []string{noAdapterWarn}
	dry := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{p}}
	app := copyApp(t, &calls, dry, daemon.MatrixCopyResult{}, "")
	code, stdout, stderr := run(t, app, "roles", "matrix", "copy", "ork", "adjudicate", "--dry-run")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	if want := []daemon.MatrixCopyParams{{From: "ork", To: []string{"adjudicate"}, DryRun: true}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls %+v, want only a dry run: %+v", calls, want)
	}
	want := "adjudicate: would change (3 steps)\n  remove r9\n  add r21\n  rule-add u10\n  warning: " + noAdapterWarn + "\n"
	if stdout != want {
		t.Fatalf("stdout\n got %q\nwant %q", stdout, want)
	}
}

func TestRolesMatrixCopyPrintsEveryStatusOnOneLine(t *testing.T) {
	var calls []daemon.MatrixCopyParams
	dry := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{
		{Project: "a", Status: daemon.CopyUnchanged},
		{Project: "b", Status: daemon.CopyWouldChange, Steps: []daemon.MatrixCopyStep{{Op: "set", ID: "r1", Changed: true}, {Op: "move", ID: "r2"}}},
		{Project: "c", Status: daemon.CopySkipped, Error: &ipc.Error{Code: "no_relay_roles", Message: "no matrix; run ork projects setup c relay-roles-init"}},
	}}
	app := copyApp(t, &calls, dry, daemon.MatrixCopyResult{}, "")
	code, stdout, _ := run(t, app, "roles", "matrix", "copy", "ork", "--all", "--dry-run")
	want := "a: unchanged\nb: would change (2 steps)\n  set r1\n  move r2: unchanged\nc: skipped: no matrix; run ork projects setup c relay-roles-init\n"
	if code != ExitOK || stdout != want {
		t.Fatalf("exit %d\n got %q\nwant %q", code, stdout, want)
	}
	if want := []daemon.MatrixCopyParams{{From: "ork", All: true, DryRun: true}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls %+v", calls)
	}
}

func TestRolesMatrixCopyAsksPerProjectAndEnterIsNo(t *testing.T) {
	dry := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{wouldChange("a"), wouldChange("b")}}
	written := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{{Project: "b", Status: daemon.CopyChanged, Steps: copyStepsRemove}}}
	for _, tc := range []struct {
		name, input string
		writes      []string // the targets of the write call, or nil for no write
	}{
		{"enter and n", "\nn\n", nil},
		{"closed input", "", nil},
		{"y then Enter", "y\n\n", []string{"a"}},
		{"n then y", "n\ny\n", []string{"b"}},
		{"upper case", "Y\nYES\n", []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []daemon.MatrixCopyParams
			app := copyApp(t, &calls, dry, written, tc.input)
			code, stdout, stderr := run(t, app, "roles", "matrix", "copy", "ork", "a", "b")
			if code != ExitOK {
				t.Fatalf("exit %d\nstderr: %s", code, stderr)
			}
			for _, q := range []string{"Copy ork's matrix to a? [y/N] ", "Copy ork's matrix to b? [y/N] "} {
				if !strings.Contains(stdout, q) {
					t.Errorf("no question %q in\n%s", q, stdout)
				}
			}
			if strings.Index(stdout, "Copy ork's matrix to a?") > strings.Index(stdout, "b: would change") {
				t.Fatalf("a's question must precede b's preview:\n%s", stdout)
			}
			wantCalls := []daemon.MatrixCopyParams{{From: "ork", To: []string{"a", "b"}, DryRun: true}}
			if tc.writes == nil && !strings.HasSuffix(stdout, "nothing copied\n") {
				t.Errorf("no word that nothing was copied:\n%s", stdout)
			}
			if tc.writes != nil {
				expect := map[string][]daemon.MatrixCopyStep{}
				rows := map[string]string{}
				for _, name := range tc.writes {
					expect[name] = copyStepsRemove
					rows[name] = digestOf(name)
				}
				wantCalls = append(wantCalls, daemon.MatrixCopyParams{From: "ork", To: tc.writes, ExpectSteps: expect, ExpectRows: rows})
			}
			if !reflect.DeepEqual(calls, wantCalls) {
				t.Fatalf("calls\n got %+v\nwant %+v", calls, wantCalls)
			}
		})
	}
}

func TestRolesMatrixCopyYesSkipsTheQuestion(t *testing.T) {
	var calls []daemon.MatrixCopyParams
	dry := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{wouldChange("adjudicate")}}
	written := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{{Project: "adjudicate", Status: daemon.CopyChanged, Steps: copyStepsRemove}}}
	app := copyApp(t, &calls, dry, written, "")
	app.Input = failReader{t}
	code, stdout, stderr := run(t, app, "roles", "matrix", "copy", "ork", "adjudicate", "--yes")
	if code != ExitOK || stderr != "" || strings.Contains(stdout, "[y/N]") {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	want := []daemon.MatrixCopyParams{
		{From: "ork", To: []string{"adjudicate"}, DryRun: true},
		{From: "ork", To: []string{"adjudicate"}, ExpectSteps: map[string][]daemon.MatrixCopyStep{"adjudicate": copyStepsRemove},
			ExpectRows: map[string]string{"adjudicate": digestOf("adjudicate")}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls\n got %+v\nwant %+v", calls, want)
	}
	if !strings.HasSuffix(stdout, "written:\nadjudicate: changed (3 steps)\n") || strings.Count(stdout, "  remove r9\n") != 1 {
		t.Fatalf("the written result is not the last output:\n%s", stdout)
	}
}

// The write binds each confirmed project to the steps its own preview showed,
// and only the confirmed ones (S548).
func TestRolesMatrixCopyWriteExpectsEachConfirmedProjectsPreviewedSteps(t *testing.T) {
	stepsB := []daemon.MatrixCopyStep{{Op: "add", ID: "r30", Changed: true}}
	b := daemon.MatrixCopyProject{Project: "b", Status: daemon.CopyWouldChange, Steps: stepsB}
	dry := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{wouldChange("a"), b, wouldChange("c")}}
	written := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{{Project: "b", Status: daemon.CopyChanged, Steps: stepsB}}}
	var calls []daemon.MatrixCopyParams
	app := copyApp(t, &calls, dry, written, "n\ny\nn\n")
	code, _, stderr := run(t, app, "roles", "matrix", "copy", "ork", "--all")
	if code != ExitOK || len(calls) != 2 {
		t.Fatalf("exit %d, calls %+v\nstderr: %s", code, calls, stderr)
	}
	got := calls[1]
	if !reflect.DeepEqual(got.ExpectSteps, map[string][]daemon.MatrixCopyStep{"b": stepsB}) || !reflect.DeepEqual(got.To, []string{"b"}) || got.DryRun {
		t.Fatalf("write call %+v, want b bound to its own steps", got)
	}
}

// The write sends back the digest of the rows each confirmed project's preview
// carried, and only the confirmed ones' (S558).
func TestRolesMatrixCopyWriteSendsEachConfirmedProjectsPreviewDigest(t *testing.T) {
	b := wouldChange("b")
	b.Digest = "sha256:b-rows"
	dry := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{wouldChange("a"), b, wouldChange("c")}}
	written := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{{Project: "b", Status: daemon.CopyChanged, Steps: copyStepsRemove}}}
	var calls []daemon.MatrixCopyParams
	app := copyApp(t, &calls, dry, written, "n\ny\nn\n")
	code, _, stderr := run(t, app, "roles", "matrix", "copy", "ork", "--all")
	if code != ExitOK || len(calls) != 2 {
		t.Fatalf("exit %d, calls %+v\nstderr: %s", code, calls, stderr)
	}
	if want := map[string]string{"b": "sha256:b-rows"}; !reflect.DeepEqual(calls[1].ExpectRows, want) {
		t.Fatalf("ExpectRows %v, want %v", calls[1].ExpectRows, want)
	}
}

// failReader fails the test when anything reads it.
type failReader struct{ t *testing.T }

func (r failReader) Read([]byte) (int, error) {
	r.t.Error("the command read its input")
	return 0, io.EOF
}

func TestRolesMatrixCopyAllExitsOneOnAFailure(t *testing.T) {
	var calls []daemon.MatrixCopyParams
	dry := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{wouldChange("a"), wouldChange("b"), wouldChange("c")}}
	written := daemon.MatrixCopyResult{From: "ork", Partial: true, Projects: []daemon.MatrixCopyProject{
		{Project: "a", Status: daemon.CopyChanged, Steps: copyStepsRemove[:1]},
		{Project: "b", Status: daemon.CopyFailed, Error: &ipc.Error{Code: "stale_revision", Message: "the matrix changed"}},
		{Project: "c", Status: daemon.CopyChanged, Steps: copyStepsRemove[:2]},
	}}
	app := copyApp(t, &calls, dry, written, "")
	code, stdout, _ := run(t, app, "roles", "matrix", "copy", "ork", "--all", "--yes")
	if code != ExitFailure {
		t.Fatalf("exit %d, want %d\n%s", code, ExitFailure, stdout)
	}
	want := "written:\na: changed (1 step)\nb: failed: the matrix changed (stale_revision)\nc: changed (2 steps)\n"
	if !strings.HasSuffix(stdout, want) {
		t.Fatalf("stdout\n got %q\nwant it to end with %q", stdout, want)
	}
}

// A failed check stops the copy: nothing is asked and nothing is written.
func TestRolesMatrixCopyAFailedPreviewWritesNothing(t *testing.T) {
	var calls []daemon.MatrixCopyParams
	dry := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{
		wouldChange("a"),
		{Project: "b", Status: daemon.CopyFailed, Error: &ipc.Error{Code: "no_spec_check_row", Message: "b has no Spec check row rule"}},
	}}
	app := copyApp(t, &calls, dry, daemon.MatrixCopyResult{}, "y\ny\n")
	code, stdout, _ := run(t, app, "roles", "matrix", "copy", "ork", "--all", "--yes")
	if code != ExitFailure || len(calls) != 1 || !calls[0].DryRun || strings.Contains(stdout, "[y/N]") {
		t.Fatalf("exit %d, calls %+v\n%s", code, calls, stdout)
	}
	if !strings.Contains(stdout, "b: failed: b has no Spec check row rule (no_spec_check_row)\n") || !strings.Contains(stdout, "nothing written") {
		t.Fatalf("stdout:\n%s", stdout)
	}
}

func TestRolesMatrixCopyNothingToCopyAsksNothing(t *testing.T) {
	var calls []daemon.MatrixCopyParams
	dry := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{{Project: "a", Status: daemon.CopyUnchanged}}}
	app := copyApp(t, &calls, dry, daemon.MatrixCopyResult{}, "")
	app.Input = failReader{t}
	code, stdout, _ := run(t, app, "roles", "matrix", "copy", "ork", "a")
	if code != ExitOK || len(calls) != 1 || strings.Contains(stdout, "[y/N]") || !strings.HasPrefix(stdout, "a: unchanged\n") {
		t.Fatalf("exit %d, calls %+v\n%s", code, calls, stdout)
	}
}

func TestRolesMatrixCopyJSONIsTheWireResult(t *testing.T) {
	var calls []daemon.MatrixCopyParams
	p := wouldChange("a")
	p.Warnings = []string{noAdapterWarn}
	dry := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{p}}
	written := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{{Project: "a", Status: daemon.CopyChanged, Steps: copyStepsRemove}}}
	for _, tc := range []struct {
		name string
		args []string
		want daemon.MatrixCopyResult
	}{
		{"dry run", []string{"--dry-run"}, dry},
		{"yes", []string{"--yes"}, written},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls = nil
			app := copyApp(t, &calls, dry, written, "")
			app.Input = failReader{t}
			code, stdout, stderr := run(t, app, append([]string{"roles", "matrix", "copy", "ork", "a", "--json"}, tc.args...)...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit %d\nstderr: %s", code, stderr)
			}
			var got daemon.MatrixCopyResult
			dec := json.NewDecoder(strings.NewReader(stdout))
			if err := dec.Decode(&got); err != nil || dec.More() {
				t.Fatalf("output is not one JSON value: %v\n%s", err, stdout)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want the daemon's result %+v", got, tc.want)
			}
		})
	}
}

func TestRolesMatrixCopyJSONExitsOneOnAFailure(t *testing.T) {
	var calls []daemon.MatrixCopyParams
	dry := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{
		{Project: "b", Status: daemon.CopyFailed, Error: &ipc.Error{Code: "bad_role_field", Message: "no"}},
	}}
	app := copyApp(t, &calls, dry, daemon.MatrixCopyResult{}, "")
	code, stdout, _ := run(t, app, "roles", "matrix", "copy", "ork", "b", "--dry-run", "--json")
	var got daemon.MatrixCopyResult
	mustJSON(t, stdout, &got)
	if code != ExitFailure || got.Projects[0].Status != daemon.CopyFailed {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}

func TestRolesMatrixCopyUsage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no from", []string{"roles", "matrix", "copy"}, "FROM"},
		{"only flags", []string{"roles", "matrix", "copy", "--all"}, "FROM"},
		{"neither to nor all", []string{"roles", "matrix", "copy", "ork"}, "TO"},
		{"both to and all", []string{"roles", "matrix", "copy", "ork", "a", "--all"}, "not both"},
		{"unknown flag", []string{"roles", "matrix", "copy", "ork", "a", "--force"}, "-force"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []daemon.MatrixCopyParams
			app := copyApp(t, &calls, daemon.MatrixCopyResult{}, daemon.MatrixCopyResult{}, "")
			code, stdout, stderr := run(t, app, tc.args...)
			if code != ExitUsage || stdout != "" || !strings.Contains(stderr, tc.want) || len(calls) != 0 {
				t.Fatalf("exit %d, calls %+v\nstdout: %q\nstderr: %q", code, calls, stdout, stderr)
			}
		})
	}
}

func TestRolesMatrixCopyShowsTheDaemonsError(t *testing.T) {
	app := &App{Call: func(_ context.Context, _ string, _, _ any) error {
		return &ipc.Error{Code: daemon.CodeNotFound, Message: `no project named "nope"`}
	}}
	code, stdout, stderr := run(t, app, "roles", "matrix", "copy", "nope", "a", "--dry-run")
	if code != ExitFailure || stdout != "" || !strings.Contains(stderr, `no project named "nope"`) {
		t.Fatalf("exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
}

func TestUsageNamesRolesMatrixCopy(t *testing.T) {
	_, stdout, _ := run(t, noDaemon(t), "help")
	if !strings.Contains(stdout, "ork roles matrix copy FROM (TO... | --all) [--dry-run] [--yes]") {
		t.Fatalf("usage does not name roles matrix copy:\n%s", stdout)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if len(line) > 80 {
			t.Errorf("usage line over 80 columns: %q", line)
		}
	}
}

func TestUsageNamesRolesMatrix(t *testing.T) {
	_, stdout, _ := run(t, noDaemon(t), "help")
	if !strings.Contains(stdout, "ork roles matrix P") {
		t.Fatalf("usage does not name roles matrix:\n%s", stdout)
	}
}

func TestMatrixCopyWrappedFailure(t *testing.T) {
	var out strings.Builder
	printMatrixCopy(&out, daemon.MatrixCopyResult{Projects: []daemon.MatrixCopyProject{{Project: "target", Status: daemon.CopyFailed, Error: &ipc.Error{Code: "stale_preview", Message: strings.Repeat("changed ", 20) + "preview again, then copy"}}}})
	for i, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
		if len(line) > 80 || i > 0 && !strings.HasPrefix(line, "  ") {
			t.Fatalf("failure must wrap at 80 with two-space hang: %q", line)
		}
	}
}

func TestMatrixCopyPrintsSetFieldsInOrder(t *testing.T) {
	var step daemon.MatrixCopyStep
	mustJSON(t, `{"op":"set","id":"r1","changed":true,"fields":{"note":"","reasoning":"high","model":"gpt-x","vendor":"openai","agent":"Codex","when":"[\"L\"]","role":"reviewer"}}`, &step)
	var out strings.Builder
	printMatrixCopy(&out, daemon.MatrixCopyResult{Projects: []daemon.MatrixCopyProject{{Project: "target", Status: daemon.CopyWouldChange, Steps: []daemon.MatrixCopyStep{step}}}})
	want := "target: would change (1 step)\n  set r1 role=reviewer when=[\"L\"] agent=Codex vendor=openai model=gpt-x reasoning=high note=\"\"\n"
	if out.String() != want {
		t.Fatalf("set fields output got %q, want %q", out.String(), want)
	}
}
