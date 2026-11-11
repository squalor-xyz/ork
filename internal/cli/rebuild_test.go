// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
)

// rebuildDaemon serves real IPC, with an on-disk ordering trace shared with
// the build process. A stop unlinks the old socket; Start binds its successor.
func rebuildDaemon(t *testing.T, status string) (*App, string) {
	t.Helper()
	dir := shortDir(t)
	trace := filepath.Join(dir, "trace")
	socket := filepath.Join(dir, "daemon.sock")
	status = strings.ReplaceAll(status, "PROJECT_PATH", dir)
	status = strings.ReplaceAll(status, "TRACE_PATH", trace)
	serve := func() error {
		l, err := ipc.Listen(socket)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		s := &ipc.Server{Handler: func(_ context.Context, method string, _ json.RawMessage) (any, error) {
			switch method {
			case daemon.MethodDaemonInfo:
				return daemon.DaemonInfoResult{BuildID: "dev"}, nil
			case daemon.MethodStatus:
				var st daemon.StatusResult
				err := json.Unmarshal([]byte(status), &st)
				return st, err
			case daemon.MethodDaemonStop:
				f, err := os.OpenFile(trace, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
				if err != nil {
					return nil, err
				}
				_, err = f.WriteString("stop\n")
				f.Close()
				if err != nil {
					return nil, err
				}
				if err := os.Remove(socket); err != nil {
					return nil, err
				}
				return daemon.StopResult{Stopped: true}, nil
			}
			return nil, fmt.Errorf("unexpected method %s", method)
		}}
		go func() { defer close(done); s.Serve(ctx, l) }()
		t.Cleanup(func() { cancel(); l.Close(); <-done })
		return nil
	}
	if err := serve(); err != nil {
		t.Fatal(err)
	}
	return &App{Socket: socket, Start: func(context.Context) error {
		f, err := os.OpenFile(trace, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		_, err = f.WriteString("start\n")
		f.Close()
		if err != nil {
			return err
		}
		return serve()
	}}, trace
}

const rebuildStatus = `{"projects":[{"name":"ork","path":"PROJECT_PATH","self_host":true,"behind_main":true,"build_command":"printf 'build\\n' >> TRACE_PATH"}]}`

func TestRebuildBeforeRestart(t *testing.T) {
	a, trace := rebuildDaemon(t, rebuildStatus)
	code, out, stderr := run(t, a, "daemon", "restart", "--rebuild")
	b, _ := os.ReadFile(trace)
	if got := fmt.Sprintf("%d|%s|%s|%s", code, out, stderr, b); got != "0|daemon restarted\n||build\nstop\nstart\n" {
		t.Fatalf("restart order/output: %q", got)
	}
}

func TestRebuildFailureKeepsDaemon(t *testing.T) {
	st := strings.Replace(rebuildStatus, "printf 'build\\\\n' >> TRACE_PATH", "printf 'build failed last line\\\\n'; exit 1", 1)
	a, trace := rebuildDaemon(t, st)
	code, _, stderr := run(t, a, "daemon", "restart", "--rebuild")
	if code == 0 || !strings.Contains(stderr, "build failed last line") || !strings.Contains(stderr, "daemon not restarted") {
		t.Errorf("build failure: %d %q", code, stderr)
	}
	b, _ := os.ReadFile(trace)
	c, err := daemon.Dial(context.Background(), a.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var info daemon.DaemonInfoResult
	err = c.Call(context.Background(), daemon.MethodDaemonInfo, nil, &info)
	if len(b) != 0 || err != nil {
		t.Fatalf("failed build stopped daemon: trace %q probe %v", b, err)
	}
}

func TestRebuildMissingExecutableKeepsDaemon(t *testing.T) {
	a, trace := rebuildDaemon(t, rebuildStatus)
	a.PrepareRestart = func(context.Context) (func(context.Context) error, error) {
		return nil, &os.PathError{Op: "stat", Path: "missing-ork", Err: os.ErrNotExist}
	}
	code, _, stderr := run(t, a, "daemon", "restart", "--rebuild")
	if code == 0 || !strings.Contains(stderr, "missing-ork") || !strings.Contains(stderr, "daemon not restarted") {
		t.Errorf("missing executable refusal: %d %s", code, stderr)
	}
	b, _ := os.ReadFile(trace)
	if string(b) != "build\n" {
		t.Fatalf("missing executable sent stop: %q", b)
	}
}

func TestRebuildUsesPreparedStarter(t *testing.T) {
	a, trace := rebuildDaemon(t, rebuildStatus)
	prepared := a.Start
	a.Start = func(context.Context) error { return fmt.Errorf("unprepared starter used") }
	a.PrepareRestart = func(context.Context) (func(context.Context) error, error) {
		f, err := os.OpenFile(trace, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		_, err = f.WriteString("prepare\n")
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		return prepared, err
	}
	code, out, stderr := run(t, a, "daemon", "restart", "--rebuild")
	b, _ := os.ReadFile(trace)
	if got := fmt.Sprintf("%d|%s|%s|%s", code, out, stderr, b); got != "0|daemon restarted\n||build\nprepare\nstop\nstart\n" {
		t.Fatalf("prepared restart order/output: %q", got)
	}
}

func TestRebuildRefusals(t *testing.T) {
	for _, tc := range []struct{ name, change, replacement, want string }{
		{"relay", `"behind_main":true`, `"behind_main":true,"active_relays":[{"id":"relay-busy","running":1}]`, "relay-busy"},
		{"engine", `"behind_main":true`, `"behind_main":true,"engine_runs":[{"id":"engine-busy","state":"running"}]`, "engine-busy"},
		{"missing", `"build_command":"printf 'build\\n' >> TRACE_PATH"`, `"build_command":""`, "ork projects set ork --build"},
		{"other", `"self_host":true`, `"self_host":false`, "not self-host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, trace := rebuildDaemon(t, strings.Replace(rebuildStatus, tc.change, tc.replacement, 1))
			code, _, stderr := run(t, a, "daemon", "restart", "--rebuild")
			b, _ := os.ReadFile(trace)
			if code == 0 || !strings.Contains(stderr, tc.want) || len(b) != 0 {
				t.Fatalf("refusal: %d %q trace %q", code, stderr, b)
			}
		})
	}
}

func TestRebuildProjectSetting(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "ork", Path: "/ork"})
	for _, cmd := range []string{"./build-only-in-home-dir.sh", ""} {
		code, out, stderr := run(t, w.app, "projects", "set", "ork", "--build", cmd)
		f, err := registry.Load(w.regPath)
		b, _ := json.Marshal(f)
		want := `"build_command":"` + cmd + `"`
		if code != 0 || err != nil || (cmd != "" && !strings.Contains(string(b), want)) || (cmd == "" && strings.Contains(string(b), "build_command")) {
			t.Fatalf("set build: %d %s %s saved %s err %v", code, out, stderr, b, err)
		}
	}
}

func TestDoctorBuildExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	var p registry.Project
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"name":"ork","path":%q,"build_command":"./plain"}`, dir)), &p); err != nil {
		t.Fatal(err)
	}
	got := checkProject(context.Background(), p)
	if !strings.Contains(strings.Join(got.Findings, "\n"), `build_command: "./plain" not executable`) {
		t.Fatalf("build doctor findings: %v", got.Findings)
	}
}

func TestRebuildNotices(t *testing.T) {
	for _, command := range []string{"", "./build-only-in-home-dir.sh"} {
		t.Run(command, func(t *testing.T) {
			st := fmt.Sprintf(`{"projects":[{"name":"ork","self_host":true,"behind_main":true,"build_command":%q}]}`, command)
			want := daemon.BehindNotice
			if command != "" {
				want += " --rebuild"
			}
			a, _, _ := doctorApp(t, "Contract: relay v11")
			a.Call = func(_ context.Context, method string, _, result any) error {
				if method == daemon.MethodStatus {
					return json.Unmarshal([]byte(st), result)
				}
				return json.Unmarshal([]byte(`{"build_id":"dev"}`), result)
			}
			for _, cmd := range []string{"status", "doctor"} {
				t.Run(cmd, func(t *testing.T) {
					code, out, stderr := run(t, a, cmd)
					if code != 0 || !strings.Contains(out+stderr, want) || (command == "" && strings.Contains(out+stderr, "--rebuild")) {
						t.Fatalf("%s notice: %d %s %s", cmd, code, out, stderr)
					}
				})
			}
		})
	}
}

// Exercise build execution without a socket as well, so process behavior can
// be checked in sandboxes that prohibit creating Unix listeners.
func TestRebuildExecution(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "built")
	p := daemon.ProjectStatus{Name: "ork", Path: dir, SelfHost: true, BehindMain: true, BuildCommand: "printf built > built"}
	st := daemon.StatusResult{Projects: []daemon.ProjectStatus{p}}
	if err := rebuildProject(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(trace)
	if err != nil || string(b) != "built" {
		t.Fatalf("build artifact: %q %v", b, err)
	}
}

func TestRebuildExecutionFailure(t *testing.T) {
	st := daemon.StatusResult{Projects: []daemon.ProjectStatus{{Name: "ork", Path: t.TempDir(), SelfHost: true, BehindMain: true,
		BuildCommand: `printf 'old-line\n'; i=0; while [ "$i" -lt 20 ]; do printf 'last-line\n' >&2; i=$((i+1)); done; exit 1`}}}
	err := rebuildProject(context.Background(), st)
	if err == nil || !strings.Contains(err.Error(), "last-line") || !strings.Contains(err.Error(), "daemon not restarted") || strings.Contains(err.Error(), "old-line") {
		t.Fatalf("build failure tail: %v", err)
	}
}

func TestRebuildExecutionStartFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-directory")
	st := daemon.StatusResult{Projects: []daemon.ProjectStatus{{Name: "ork", Path: missing, SelfHost: true, BehindMain: true, BuildCommand: "printf built"}}}
	err := rebuildProject(context.Background(), st)
	if err == nil || !strings.Contains(err.Error(), "sh -c printf built:") || !strings.Contains(err.Error(), "no such file or directory") || !strings.Contains(err.Error(), "daemon not restarted") {
		t.Fatalf("missing spawn diagnostic: %v", err)
	}
}

func TestRebuildExecutionRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update func(*daemon.ProjectStatus)
		want   string
	}{
		{"relay", func(p *daemon.ProjectStatus) { p.ActiveRelays = []daemon.Relay{{ID: "relay-busy", Running: 1}} }, "relay-busy"},
		{"engine", func(p *daemon.ProjectStatus) {
			p.EngineRuns = []daemon.EngineRun{{ID: "engine-busy", State: "running"}}
		}, "engine-busy"},
		{"missing", func(p *daemon.ProjectStatus) { p.BuildCommand = "" }, "ork projects set ork --build CMD"},
		{"other", func(p *daemon.ProjectStatus) { p.SelfHost = false }, "not self-host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := daemon.ProjectStatus{Name: "ork", Path: dir, SelfHost: true, BehindMain: true, BuildCommand: "printf built > built"}
			tc.update(&p)
			err := rebuildProject(context.Background(), daemon.StatusResult{Projects: []daemon.ProjectStatus{p}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal: %v", err)
			}
			if b, err := os.ReadFile(filepath.Join(dir, "built")); !os.IsNotExist(err) {
				t.Errorf("refused build ran: %q %v", b, err)
			}
		})
	}
}
