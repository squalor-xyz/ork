// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package clitest

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestKilledFakeReleasesCallLock(t *testing.T) {
	f := Install(t, Route{Tool: "slicer", Args: []string{"next"}, Stdout: "next reply\n"})
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--clitest-hold-lock", filepath.Join(f.Dir, "lock"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	joined := false
	defer func() {
		if !joined {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatalf("lock holder readiness: %q, %v", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	joined = true
	// The next real fake call must return its routed output after the holder dies.
	nextCtx, nextCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer nextCancel()
	out, err := exec.CommandContext(nextCtx, f.Bin("slicer"), "next").CombinedOutput()
	if err != nil || string(out) != "next reply\n" {
		t.Fatalf("fake after killed lock holder: output %q, error %v", out, err)
	}
}
