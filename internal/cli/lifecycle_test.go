// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/roles"
	"github.com/squalor-xyz/ork/internal/source"
)

type forbiddenBuildFetch struct{ t *testing.T }

func (s forbiddenBuildFetch) Fetch(context.Context, registry.Project, source.Part) (any, error) {
	s.t.Error("stale check fetched project data")
	return nil, nil
}

func TestStaleCheckDoesNotReadProjects(t *testing.T) {
	for _, args := range [][]string{{"projects", "list"}, {"daemon", "stop"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			w := newWorld(t, forbiddenBuildFetch{t}, registry.Project{Name: "p", Path: "/p", Adj: true})
			w.app.BuildID = "current"
			c, err := w.app.Connect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			c.Close()
			code, out, stderr := run(t, w.app, args...)
			want := "/p"
			if args[0] == "daemon" {
				want = "daemon stopped"
			}
			if code != 0 || !strings.Contains(out, want) || !strings.Contains(stderr, "ork daemon restart") {
				t.Fatalf("command: %d, stdout %q, stderr %q", code, out, stderr)
			}
		})
	}
}

func TestOldDaemonUnknownBuildMethod(t *testing.T) {
	socket := filepath.Join(shortDir(t), "old.sock")
	l, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s := &ipc.Server{Handler: func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method == "status" {
			t.Error("stale check read status on an old daemon")
		}
		if method == "projects.list" {
			return daemon.ProjectsResult{Projects: []registry.Project{{Name: "old-project", Path: "/old-project"}}}, nil
		}
		return nil, &ipc.Error{Code: daemon.CodeUnknownMethod, Message: "unknown method " + method}
	}}
	go func() { defer close(done); s.Serve(ctx, l) }()
	t.Cleanup(func() { cancel(); l.Close(); <-done })
	code, out, stderr := run(t, &App{Socket: socket, BuildID: "current"}, "projects", "list")
	if code != 0 || !strings.Contains(out, "/old-project") || !strings.Contains(stderr, "stale daemon build unknown") || !strings.Contains(stderr, "ork daemon restart") {
		t.Fatalf("old daemon: %d, stdout %q, stderr %q", code, out, stderr)
	}
}

func TestStatusReadsProjectsOnce(t *testing.T) {
	socket := filepath.Join(shortDir(t), "status.sock")
	l, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	read := false
	s := &ipc.Server{Handler: func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method == "daemon.info" {
			return map[string]string{"build_id": "current"}, nil
		}
		if method != daemon.MethodStatus {
			return nil, fmt.Errorf("unexpected method %s", method)
		}
		if read {
			return nil, fmt.Errorf("projects read twice")
		}
		read = true
		return daemon.StatusResult{BuildID: "current", Projects: []daemon.ProjectStatus{{Name: "read-once", Path: "/read-once"}}}, nil
	}}
	go func() { defer close(done); s.Serve(ctx, l) }()
	t.Cleanup(func() { cancel(); l.Close(); <-done })
	code, out, stderr := run(t, &App{Socket: socket, BuildID: "current"}, "status", "--json")
	var status daemon.StatusResult
	mustJSON(t, out, &status)
	if code != 0 || len(status.Projects) != 1 || status.Projects[0].Name != "read-once" || stderr != "" {
		t.Fatalf("single status read: %d, stdout %s, stderr %q", code, out, stderr)
	}
}

func TestDaemonLeftoverSocket(t *testing.T) {
	for _, sub := range []string{"stop", "restart"} {
		t.Run(sub, func(t *testing.T) {
			w := newWorld(t, stubSource{})
			if err := os.MkdirAll(w.paths.Dir, 0o700); err != nil {
				t.Fatal(err)
			}
			l, err := net.ListenUnix("unix", &net.UnixAddr{Name: w.paths.Socket, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			l.SetUnlinkOnClose(false)
			if err := l.Close(); err != nil {
				t.Fatal(err)
			}
			if sub == "stop" {
				w.app.Start = func(context.Context) error { t.Error("stop started a daemon"); return nil }
			}
			code, out, stderr := run(t, w.app, "daemon", sub)
			want := "daemon stopped\n"
			if sub == "restart" {
				want = "daemon restarted\n"
			}
			if code != 0 || out != want {
				t.Fatalf("leftover socket %s: %d, stdout %q, stderr %q", sub, code, out, stderr)
			}
		})
	}
}

func TestDoctorBuildProbe(t *testing.T) {
	for _, old := range []bool{false, true} {
		t.Run(fmt.Sprint(old), func(t *testing.T) {
			a, _, _ := doctorApp(t, fmt.Sprintf("Contract: relay v%d", roles.ContractVersion))
			a.BuildID = "current"
			a.Call = func(_ context.Context, method string, _, result any) error {
				if method == daemon.MethodStatus {
					// The separate self-host probe may fail without breaking doctor.
					return fmt.Errorf("project reads unavailable")
				}
				if method != "daemon.info" {
					t.Errorf("build probe called %s, want daemon.info", method)
					return fmt.Errorf("project reads unavailable")
				}
				if old {
					return &ipc.Error{Code: daemon.CodeUnknownMethod, Message: "unknown method daemon.info"}
				}
				return json.Unmarshal([]byte(`{"build_id":"old-build"}`), result)
			}
			code, out, stderr := run(t, a, "doctor", "--json")
			want := "old-build"
			if old {
				want = "unknown"
			}
			var rep DoctorReport
			mustJSON(t, out, &rep)
			if code != 0 || rep.DaemonBuildID != want || !strings.Contains(rep.Warning, "stale daemon build "+want) || !strings.Contains(rep.Warning, "ork daemon restart") {
				t.Fatalf("doctor build probe: exit %d, build %q, warning %q, stderr %q", code, rep.DaemonBuildID, rep.Warning, stderr)
			}
		})
	}
}

func TestDaemonStopClosesSocket(t *testing.T) {
	w := newWorld(t, stubSource{})
	if code, _, err := run(t, w.app, "status"); code != 0 {
		t.Fatal(err)
	}
	code, out, err := run(t, w.app, "daemon", "stop")
	if code != 0 {
		t.Fatalf("stop: code %d: %s", code, err)
	}
	if !strings.Contains(out, "stopped") {
		t.Fatalf("stop output: %s", out)
	}
	if c, err := daemon.Dial(context.Background(), w.paths.Socket); err == nil {
		c.Close()
		t.Fatal("daemon still answers after stop")
	}
}

func TestDaemonRestartServesAgain(t *testing.T) {
	w := newWorld(t, stubSource{})
	start := w.app.Start
	w.app.Start = func(ctx context.Context) error {
		if done := w.daemonDone; done != nil {
			// Production's serveWithRetry handles the interval between socket
			// removal and lock release. This in-process starter calls Run once,
			// so wait for the old Run to finish while its client stays open.
			select {
			case <-done:
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return fmt.Errorf("old daemon did not finish with a client connected")
			}
		}
		return start(ctx)
	}
	if code, _, err := run(t, w.app, "status"); code != 0 {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	old, err2 := daemon.Dial(ctx, w.paths.Socket)
	if err2 != nil {
		t.Fatal(err2)
	}
	defer old.Close()
	if _, err := old.Status(ctx, ""); err != nil {
		t.Fatalf("old daemon did not answer before restart: %v", err)
	}
	code, out, err := run(t, w.app, "daemon", "restart")
	if code != 0 {
		t.Fatalf("restart: code %d: %s", code, err)
	}
	if !strings.Contains(out, "restarted") {
		t.Fatalf("restart output: %s", out)
	}
	if _, err := old.Status(ctx, ""); err == nil {
		t.Fatal("old daemon connection survived restart")
	}
	c, err2 := daemon.Dial(ctx, w.paths.Socket)
	if err2 != nil {
		t.Fatal(err2)
	}
	defer c.Close()
	if _, err := c.Status(ctx, ""); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonStopBusyAndForce(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprint(force), func(t *testing.T) {
			w := newWorld(t, stubSource{})
			w.waveRunning = func() bool { return true }
			if code, _, err := run(t, w.app, "status"); code != 0 {
				t.Fatal(err)
			}
			args := []string{"daemon", "stop"}
			if force {
				args = append(args, "--force")
			}
			code, out, err := run(t, w.app, args...)
			if !force {
				if code != 1 || !strings.Contains(err, "--force") {
					t.Fatalf("busy stop: %d: %s", code, err)
				}
				c, err := daemon.Dial(context.Background(), w.paths.Socket)
				if err != nil {
					t.Fatalf("refused stop closed daemon: %v", err)
				}
				c.Close()
			} else {
				if code != 0 || !strings.Contains(out, "stopped") {
					t.Fatalf("forced stop: %d: %s %s", code, out, err)
				}
				if c, err := daemon.Dial(context.Background(), w.paths.Socket); err == nil {
					c.Close()
					t.Fatal("forced stop left daemon serving")
				}
			}
		})
	}
}

func TestStaleDaemonSocketCommandWarning(t *testing.T) {
	for _, build := range []string{"dev", "current"} {
		t.Run(build, func(t *testing.T) {
			w := newWorld(t, stubSource{})
			w.app.BuildID = build
			code, _, err := run(t, w.app, "status", "--json")
			if code != 0 {
				t.Fatal(err)
			}
			if build == "current" && !strings.Contains(err, "ork daemon restart") {
				t.Fatalf("missing socket stale warning: %q", err)
			}
			if build == "dev" && err != "" {
				t.Fatalf("matching socket daemon warned: %q", err)
			}
		})
	}
}

func TestStaleDaemonCommandWarning(t *testing.T) {
	for _, build := range []string{"old", "current", ""} {
		t.Run(build, func(t *testing.T) {
			a := &App{BuildID: "current", Call: func(_ context.Context, method string, _, result any) error {
				if method != daemon.MethodStatus {
					return fmt.Errorf("unexpected method %s", method)
				}
				return json.Unmarshal([]byte(fmt.Sprintf(`{"build_id":%q,"projects":[]}`, build)), result)
			}}
			code, _, err := run(t, a, "status", "--json")
			if code != 0 {
				t.Fatalf("status: %d: %s", code, err)
			}
			if build != "current" && !strings.Contains(err, "ork daemon restart") {
				t.Fatalf("missing stale daemon warning: %q", err)
			}
			if build == "current" && err != "" {
				t.Fatalf("matching build warning: %q", err)
			}
		})
	}
}

func TestDoctorStaleDaemonWarning(t *testing.T) {
	for _, build := range []string{"old", "current", ""} {
		t.Run(build, func(t *testing.T) {
			for _, jsonOutput := range []bool{false, true} {
				t.Run(fmt.Sprint(jsonOutput), func(t *testing.T) {
					a, _, _ := doctorApp(t, fmt.Sprintf("Contract: relay v%d", roles.ContractVersion))
					a.BuildID = "current"
					a.Call = func(_ context.Context, method string, _, result any) error {
						if method != daemon.MethodDaemonInfo {
							return fmt.Errorf("unexpected method %s", method)
						}
						return json.Unmarshal([]byte(fmt.Sprintf(`{"build_id":%q,"projects":[]}`, build)), result)
					}
					args := []string{"doctor"}
					if jsonOutput {
						args = append(args, "--json")
					}
					code, out, err := run(t, a, args...)
					if code != 0 {
						t.Fatalf("doctor: %d: %s", code, err)
					}
					if build != "current" && !strings.Contains(out, "ork daemon restart") {
						t.Fatalf("missing doctor stale warning: %s", out)
					}
					if build == "current" && strings.Contains(out, "ork daemon restart") {
						t.Fatalf("matching build warned: %s", out)
					}
				})
			}
		})
	}
}

// predatingDaemon serves a daemon that lacks daemon.stop. status answers
// the status method: a nil result with a nil error fails the call.
func predatingDaemon(t *testing.T, socket string, status func() (any, error)) {
	t.Helper()
	l, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s := &ipc.Server{Handler: func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method == daemon.MethodStatus {
			return status()
		}
		return nil, &ipc.Error{Code: daemon.CodeUnknownMethod, Message: "unknown method " + method}
	}}
	go func() { defer close(done); s.Serve(ctx, l) }()
	t.Cleanup(func() { cancel(); l.Close(); <-done })
}

func startChild(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd
}

func TestDaemonStopPredatingDaemon(t *testing.T) {
	running := daemon.StatusResult{Projects: []daemon.ProjectStatus{{Name: "p", ActiveRelays: []daemon.Relay{{ID: "r", Running: 1}}}}}
	engine := daemon.StatusResult{Projects: []daemon.ProjectStatus{{Name: "p", EngineRuns: []daemon.EngineRun{{ID: "e", State: "running"}}}}}
	idle := daemon.StatusResult{Projects: []daemon.ProjectStatus{{Name: "p",
		ActiveRelays: []daemon.Relay{{ID: "r", Running: 0}},
		EngineRuns:   []daemon.EngineRun{{State: "committed"}, {State: "stopped"}, {State: "paused"}, {State: "awaiting-approval"}}}}}
	cases := []struct {
		name    string
		status  func() (any, error)
		warning string // "" means no warning about a running role
	}{
		{"idle", func() (any, error) { return idle, nil }, ""},
		{"relay", func() (any, error) { return running, nil }, "a role is running"},
		{"engine", func() (any, error) { return engine, nil }, "a role is running"},
		{"status fails", func() (any, error) {
			return nil, &ipc.Error{Code: daemon.CodeUnknownMethod, Message: "unknown method status"}
		}, "a role may be running"},
	}
	for _, sub := range []string{"stop", "restart"} {
		for _, tc := range cases {
			t.Run(sub+"-"+tc.name, func(t *testing.T) {
				socket := filepath.Join(shortDir(t), "old.sock")
				predatingDaemon(t, socket, tc.status)
				child := startChild(t)
				pid := strconv.Itoa(child.Process.Pid)
				pidFile := filepath.Join(t.TempDir(), "ork.pid")
				if err := os.WriteFile(pidFile, []byte(pid+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				started := false
				app := &App{Socket: socket, Pid: pidFile, BuildID: "current", Start: func(context.Context) error { started = true; return nil }}
				code, out, stderr := run(t, app, "daemon", sub)
				if code != ExitFailure || out != "" {
					t.Fatalf("exit %d, stdout %q, stderr %q", code, out, stderr)
				}
				for _, want := range []string{"predates daemon.stop", "pid " + pid, "kill -TERM " + pid} {
					if !strings.Contains(stderr, want) {
						t.Errorf("stderr lacks %q: %q", want, stderr)
					}
				}
				if strings.Contains(stderr, "unknown method") {
					t.Errorf("raw error printed: %q", stderr)
				}
				if tc.warning == "" && strings.Contains(stderr, "role") {
					t.Errorf("unexpected role warning: %q", stderr)
				}
				if tc.warning != "" && !strings.Contains(stderr, tc.warning) {
					t.Errorf("stderr lacks %q: %q", tc.warning, stderr)
				}
				if started {
					t.Error("restart started a daemon")
				}
				if err := syscall.Kill(child.Process.Pid, 0); err != nil {
					t.Errorf("child no longer alive: %v", err)
				}
			})
		}
	}
}

func TestDaemonStopPredatingStatusTimeout(t *testing.T) {
	for _, sub := range []string{"stop", "restart"} {
		t.Run(sub, func(t *testing.T) {
			t.Parallel()
			socket := filepath.Join(shortDir(t), "old.sock")
			release := make(chan struct{})
			predatingDaemon(t, socket, func() (any, error) {
				<-release // Accept status but never answer while the command runs.
				return daemon.StatusResult{}, nil
			})
			t.Cleanup(func() { close(release) })
			pidFile := filepath.Join(t.TempDir(), "ork.pid")
			if err := os.WriteFile(pidFile, []byte("4242\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			started := false
			app := &App{Socket: socket, Pid: pidFile, StartWait: time.Millisecond,
				Start: func(context.Context) error { started = true; return nil }}
			ctx, cancel := context.WithTimeout(context.Background(), defaultStartWait+2*time.Second)
			defer cancel()
			var stdout, stderr bytes.Buffer
			begin := time.Now()
			code := app.Run(ctx, []string{"daemon", sub}, &stdout, &stderr)
			elapsed := time.Since(begin)
			if ctx.Err() != nil {
				t.Fatalf("command exceeded status timeout: %v (elapsed %s)", ctx.Err(), elapsed)
			}
			if elapsed < defaultStartWait {
				t.Errorf("status wait %s is shorter than %s", elapsed, defaultStartWait)
			}
			if code != ExitFailure || stdout.Len() != 0 {
				t.Errorf("exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
			}
			for _, want := range []string{"predates daemon.stop", "pid 4242", "kill -TERM 4242", "warning: a role may be running; stopping the daemon ends it"} {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr lacks %q: %q", want, stderr.String())
				}
			}
			if started {
				t.Error("restart started a daemon")
			}
		})
	}
}

func TestDaemonStopPredatingUnreadablePid(t *testing.T) {
	socket := filepath.Join(shortDir(t), "old.sock")
	predatingDaemon(t, socket, func() (any, error) { return daemon.StatusResult{}, nil })
	pidFile := filepath.Join(t.TempDir(), "missing.pid")
	code, _, stderr := run(t, &App{Socket: socket, Pid: pidFile}, "daemon", "stop")
	if code != ExitFailure || !strings.Contains(stderr, pidFile) || !strings.Contains(stderr, "cannot read") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestDaemonRestartAfterPredatingDaemonExits(t *testing.T) {
	socket := filepath.Join(shortDir(t), "old.sock")
	l, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	l.Close() // the old daemon is gone; a closed unix listener unlinks its socket
	os.Remove(socket)
	started := false
	app := &App{Socket: socket, Start: func(context.Context) error {
		started = true
		l2, err := ipc.Listen(socket)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		s := &ipc.Server{Handler: func(context.Context, string, json.RawMessage) (any, error) {
			return map[string]string{"build_id": "x"}, nil
		}}
		go func() { defer close(done); s.Serve(ctx, l2) }()
		t.Cleanup(func() { cancel(); l2.Close(); <-done })
		return nil
	}}
	code, out, stderr := run(t, app, "daemon", "restart")
	if code != 0 || !started || !strings.Contains(out, "restarted") {
		t.Fatalf("exit %d, started %v, stdout %q, stderr %q", code, started, out, stderr)
	}
}

func TestDaemonStopPredatingInvalidPid(t *testing.T) {
	for name, content := range map[string]string{"whitespace": " \n", "garbage": "abc\n", "zero": "0\n", "negative": "-5\n"} {
		t.Run(name, func(t *testing.T) {
			socket := filepath.Join(shortDir(t), "old.sock")
			predatingDaemon(t, socket, func() (any, error) { return daemon.StatusResult{}, nil })
			pidFile := filepath.Join(t.TempDir(), "ork.pid")
			if err := os.WriteFile(pidFile, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			code, _, stderr := run(t, &App{Socket: socket, Pid: pidFile}, "daemon", "stop")
			if code != ExitFailure || !strings.Contains(stderr, "cannot read") || !strings.Contains(stderr, pidFile) || strings.Contains(stderr, "kill -TERM "+strings.TrimSpace(content)) {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
		})
	}
}

func TestDaemonStopPredatingJSON(t *testing.T) {
	socket := filepath.Join(shortDir(t), "old.sock")
	predatingDaemon(t, socket, func() (any, error) { return daemon.StatusResult{}, nil })
	pidFile := filepath.Join(t.TempDir(), "ork.pid")
	if err := os.WriteFile(pidFile, []byte("4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, &App{Socket: socket, Pid: pidFile}, "daemon", "stop", "--json")
	var got struct {
		Error ipc.Error `json:"error"`
	}
	mustJSON(t, out, &got)
	if code != ExitFailure || got.Error.Code != daemon.CodeUnknownMethod || !strings.Contains(got.Error.Message, "kill -TERM 4242") {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
}
