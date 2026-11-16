// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
)

const statusBuild = "0123456789abcdef0123456789abcdef01234567+modified=false"

func statusApp(t *testing.T, build string, status daemon.StatusResult, statusErr error) *App {
	t.Helper()
	a := &App{Socket: filepath.Join(shortDir(t), "s.sock"), Pid: filepath.Join(t.TempDir(), "pid"), BuildID: statusBuild,
		Start: func(context.Context) error { t.Error("read started daemon"); return fmt.Errorf("unexpected start") }}
	if err := os.WriteFile(a.Pid, []byte("4242\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.Call = func(_ context.Context, method string, _, result any) error {
		var value any
		switch method {
		case daemon.MethodDaemonInfo:
			value = daemon.DaemonInfoResult{BuildID: build}
		case daemon.MethodStatus:
			value = status
			if statusErr != nil {
				return statusErr
			}
		default:
			return fmt.Errorf("unexpected method %s", method)
		}
		b, _ := json.Marshal(value)
		return json.Unmarshal(b, result)
	}
	return a
}

func wantStatusText(t *testing.T, a *App, wants ...string) {
	t.Helper()
	code, out, err := run(t, a, "daemon", "status")
	if code != 0 || err != "" {
		t.Fatalf("status exit %d, stderr %q", code, err)
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func wantStatusJSON(t *testing.T, a *App, want map[string]any) {
	t.Helper()
	code, out, err := run(t, a, "daemon", "status", "--json")
	var got map[string]any
	mustJSON(t, out, &got)
	if code != 0 || err != "" || !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON exit %d stderr %q: got %s want %#v", code, err, out, want)
	}
}

func TestDaemonStatusReportsARunningDaemon(t *testing.T) {
	a := statusApp(t, statusBuild, daemon.StatusResult{}, nil)
	wantStatusText(t, a, "daemon: running\n", "pid: 4242\n", "socket: "+a.Socket+"\n", "build: 0123456789ab\n", "state: current\n")
	wantStatusJSON(t, a, map[string]any{"running": true, "pid": float64(4242), "socket": a.Socket, "build_id": statusBuild, "cli_build_id": statusBuild, "stale": false, "roles_running": false, "behind_main": false})
}

func TestDaemonStatusDoesNotStartADaemon(t *testing.T) {
	a := &App{Socket: filepath.Join(shortDir(t), "missing"), BuildID: statusBuild, Start: func(context.Context) error { t.Error("started daemon"); return nil }}
	wantStatusText(t, a, "daemon: not running\n")
	wantStatusJSON(t, a, map[string]any{"running": false, "socket": a.Socket, "cli_build_id": statusBuild})
}

func TestDaemonStatusSaysStale(t *testing.T) {
	a := statusApp(t, "old", daemon.StatusResult{}, nil)
	wantStatusText(t, a, "state: stale (the CLI build differs; run ork daemon restart)\n")
	code, out, err := run(t, a, "daemon", "status", "--json")
	if code != 0 || err != "" || !strings.Contains(out, `"stale": true`) {
		t.Fatalf("stale JSON: %d %s %s", code, out, err)
	}
}

func TestDaemonStatusSaysRolesRunAndRestartIsRefused(t *testing.T) {
	a := statusApp(t, statusBuild, daemon.StatusResult{RolesRunning: true}, nil)
	wantStatusText(t, a, "roles: running (ork daemon restart is refused; --force cancels them)\n")
}

func TestDaemonStatusSaysBehindMainEvenWhileRolesRun(t *testing.T) {
	a := statusApp(t, statusBuild, daemon.StatusResult{RolesRunning: true, Projects: []daemon.ProjectStatus{{SelfHost: true, BehindMain: true}}}, nil)
	wantStatusText(t, a, "main: behind (restart waits for the roles, or use --force)\n")
	_, out, _ := run(t, a, "daemon", "status", "--json")
	if !strings.Contains(out, `"behind_main": true`) {
		t.Fatalf("behind JSON: %s", out)
	}
}

func TestDaemonStatusSurvivesAFailedStatusRead(t *testing.T) {
	a := statusApp(t, statusBuild, daemon.StatusResult{}, fmt.Errorf("status unavailable"))
	wantStatusText(t, a, "build: 0123456789ab\n", "pid: 4242\n")
	_, out, _ := run(t, a, "daemon", "status")
	if strings.Contains(out, "roles:") || strings.Contains(out, "main:") {
		t.Fatalf("failed status: %s", out)
	}
	wantStatusJSON(t, a, map[string]any{"running": true, "pid": float64(4242), "socket": a.Socket, "build_id": statusBuild, "cli_build_id": statusBuild, "stale": false})
}

func TestDaemonUnknownSubcommandSaysSo(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"daemon", "bogus"}, `ork: unknown daemon command "bogus"; use status, stop, restart or run`},
		{[]string{"daemon"}, "ork: daemon needs status, stop, restart or run"},
		{[]string{"daemon", "status", "extra"}, "daemon status takes no arguments"},
	} {
		code, _, err := run(t, &App{}, tc.args...)
		if code != 2 || !strings.Contains(err, tc.want) {
			t.Errorf("%v: %d %q", tc.args, code, err)
		}
	}
}

func TestUsageListsDaemonStatus(t *testing.T) {
	if !strings.Contains(usageText, "ork daemon status") {
		t.Error("usage missing ork daemon status")
	}
	for _, line := range strings.Split(usageText, "\n") {
		if len(line) > 80 {
			t.Errorf("usage line exceeds 80: %s", line)
		}
	}
}

func doctorStatus(t *testing.T, running, behind bool) *App {
	t.Helper()
	a, _, _ := doctorApp(t, "Contract: relay v12")
	a.BuildID = statusBuild
	a.Pid = filepath.Join(t.TempDir(), "pid")
	if err := os.WriteFile(a.Pid, []byte("4242\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if running {
		a.Call = func(_ context.Context, method string, _, result any) error {
			var value any = daemon.DaemonInfoResult{BuildID: statusBuild}
			if method == daemon.MethodStatus {
				value = daemon.StatusResult{RolesRunning: behind, Projects: []daemon.ProjectStatus{{SelfHost: true, BehindMain: behind}}}
			}
			b, _ := json.Marshal(value)
			return json.Unmarshal(b, result)
		}
	}
	return a
}

func TestDoctorPrintsADaemonLineWhenCurrent(t *testing.T) {
	a := doctorStatus(t, true, false)
	code, out, err := run(t, a, "doctor")
	if code != 0 || !strings.Contains(out, "[ok] daemon  running, build 0123456789ab (pid 4242)") || strings.Contains(out, "[WARN] daemon") {
		t.Fatalf("doctor %d %s %s", code, out, err)
	}
	_, out, _ = run(t, a, "doctor", "--json")
	var got map[string]any
	mustJSON(t, out, &got)
	if got["daemon_running"] != true || got["daemon_pid"] != float64(4242) {
		t.Fatalf("doctor JSON %s", out)
	}
}

func TestDoctorPrintsADaemonLineWhenNoneRuns(t *testing.T) {
	a := doctorStatus(t, false, false)
	code, out, err := run(t, a, "doctor")
	if code != 0 || !strings.Contains(out, "[--] daemon  not running; the next command starts it") {
		t.Fatalf("doctor %d %s %s", code, out, err)
	}
	_, out, _ = run(t, a, "doctor", "--json")
	var got map[string]any
	mustJSON(t, out, &got)
	if got["daemon_running"] != false {
		t.Fatalf("doctor JSON %s", out)
	}
}

func TestDoctorBehindMainShowsWhileRolesRun(t *testing.T) {
	a := doctorStatus(t, true, true)
	_, out, _ := run(t, a, "doctor")
	if !strings.Contains(out, "[WARN] daemon behind main:") || !strings.Contains(out, "roles are running; restart waits") {
		t.Fatalf("doctor behind: %s", out)
	}
}

func TestDoctorBehindMainLinesFitEightyColumns(t *testing.T) {
	a := doctorStatus(t, true, true)
	_, out, _ := run(t, a, "doctor")
	lines := strings.Split(out, "\n")
	found := false
	for i, line := range lines {
		if !strings.HasPrefix(line, "[WARN] daemon behind main:") {
			continue
		}
		found = true
		if i+1 >= len(lines) || lines[i+1] != "       roles are running; restart waits" {
			t.Fatalf("roles line does not follow: %q", lines[i:])
		}
		for _, l := range lines[i : i+2] {
			if n := len([]rune(l)); n > 80 {
				t.Errorf("%d columns: %q", n, l)
			}
		}
	}
	if !found {
		t.Fatalf("no behind line: %s", out)
	}
}
