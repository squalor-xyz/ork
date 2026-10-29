// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"encoding/json"
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

func TestUsageNamesRolesMatrix(t *testing.T) {
	_, stdout, _ := run(t, noDaemon(t), "help")
	if !strings.Contains(stdout, "ork roles matrix P") {
		t.Fatalf("usage does not name roles matrix:\n%s", stdout)
	}
}
