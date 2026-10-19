// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
)

// buildORK builds this package into a temporary directory and returns the
// binary's path.
func buildORK(t *testing.T) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command to build ork with")
	}
	bin := filepath.Join(t.TempDir(), "ork")
	out, err := exec.Command(goBin, "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// stopDaemon stops the daemon whose pid is in pidFile, if a daemon answers on
// its socket. A daemon idles for ten minutes, so a test stops its own.
func stopDaemon(t *testing.T, pidFile, socket string) {
	t.Helper()
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return
	}
	c, err := daemon.Dial(context.Background(), socket)
	if err != nil {
		return // nothing answers, so the pid may belong to a process that is gone
	}
	c.Close()
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		t.Errorf("stop daemon %d: %v", pid, err)
		return
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("daemon %d did not stop after SIGTERM", pid)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Two commands start the daemon at once with none running. Both get their
// answer, the daemon they started leads its own session, and it keeps
// running after both commands exit. Output returning at all shows that the
// daemon does not hold the commands' stdout.
func TestTwoCommandsStartTheDaemonAtOnceAndItOutlivesThem(t *testing.T) {
	bin := buildORK(t)
	dir := sandbox(t)
	alpha := filepath.Join(dir, "alpha")
	writeRegistry(t, dir, registry.Project{Name: "alpha", Path: alpha, Adj: true})
	fake := clitest.Install(t, fixtureRoutes(t, alpha, true)...)

	paths, err := daemon.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopDaemon(t, paths.Pid, paths.Socket) })

	env := append(os.Environ(), "PATH="+fake.Dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	type result struct {
		out string
		err error
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, "status")
			cmd.Env = env
			// A daemon that held the command's stdout would keep Output
			// waiting; WaitDelay turns that into a failure, not a hang.
			cmd.WaitDelay = 10 * time.Second
			out, err := cmd.Output()
			results[i] = result{string(out), err}
		}()
	}
	wg.Wait()

	// The fixtures give alpha two open records and three relay runs.
	const head = "alpha: needs you 2, active relays 3\n"
	for i, r := range results {
		if r.err != nil || !strings.HasPrefix(r.out, head) {
			t.Errorf("command %d: stdout %q, err %v; want it to start with %q", i, r.out, r.err, head)
		}
	}
	if results[0].out != results[1].out {
		t.Errorf("the two commands printed different status:\n%q\n%q", results[0].out, results[1].out)
	}
	if t.Failed() {
		return
	}

	b, err := os.ReadFile(paths.Pid)
	if err != nil {
		t.Fatalf("no pidfile after the commands: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("pidfile %q: %v", b, err)
	}
	if sid, err := syscall.Getsid(pid); err != nil || sid != pid {
		t.Errorf("daemon %d has session %d (err %v): it must lead its own session, detached from the command", pid, sid, err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("daemon %d is not running after both commands exited: %v", pid, err)
	}

	c := dialUntilUp(t, paths.Socket)
	res, err := c.Status(context.Background(), "")
	if err != nil || len(res.Projects) != 1 || res.Projects[0].Name != "alpha" {
		t.Fatalf("the daemon does not answer after the commands exited: %v, %+v", err, res)
	}
}
