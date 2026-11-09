// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package engine

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A test binary started with ORK_ENGINE_PROC_HELPER=leader is a role that
// starts a child in a session of its own, prints the child's pid and blocks.
// init runs before TestMain, so the helper never runs a test.
func init() {
	if os.Getenv("ORK_ENGINE_PROC_HELPER") != "leader" {
		return
	}
	child := exec.Command("sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(98)
	}
	fmt.Fprintf(os.Stdout, "%d\n", child.Process.Pid)
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

func procAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func waitGone(t *testing.T, pid int, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for procAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("%s %d survived Kill", what, pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOSProcKillAlsoKillsDescendantsOutsideTheGroup(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "ORK_ENGINE_PROC_HELPER=leader")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start the helper: %v", err)
	}
	leader := cmd.Process.Pid
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	line, rerr := bufio.NewReader(out).ReadString('\n')
	child, perr := strconv.Atoi(strings.TrimSpace(line))
	if perr == nil {
		defer syscall.Kill(child, syscall.SIGKILL)
	}
	defer func() { syscall.Kill(-leader, syscall.SIGKILL); <-done }()
	if perr != nil {
		t.Fatalf("child pid not captured: %q, %v", line, rerr)
	}
	if pg, err := syscall.Getpgid(child); err != nil || pg == leader {
		t.Fatalf("the child must be outside the leader's group: pgid %d, %v", pg, err)
	}

	if err := (osProc{}).Kill(leader); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the leader survived Kill")
	}
	waitGone(t, child, "the descendant outside the group")
}

func TestOSProcKillOfADeadLeaderStillKillsTheGroup(t *testing.T) {
	// The leader starts a member of its group and exits at once.
	cmd := exec.Command("sh", "-c", "sleep 30 >/dev/null 2>&1 & echo $!")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sh: %v", err)
	}
	pgid := cmd.Process.Pid
	data, _ := io.ReadAll(out)
	cmd.Wait()
	member, perr := strconv.Atoi(strings.TrimSpace(string(data)))
	if perr != nil {
		syscall.Kill(-pgid, syscall.SIGKILL)
		t.Fatalf("member pid not captured: %q", data)
	}
	defer syscall.Kill(member, syscall.SIGKILL)

	p := osProc{}
	if !p.Alive(pgid) {
		t.Fatal("a group with a member is alive")
	}
	if err := p.Kill(pgid); err != nil {
		t.Fatal(err)
	}
	waitGone(t, member, "the group member")
}

func TestOSProcRejectsUnsafeGroupsBeforeSignalling(t *testing.T) {
	for _, pgid := range []int{0, 1, -1, selfPGID()} {
		t.Run(fmt.Sprint(pgid), func(t *testing.T) {
			p := osProc{signal: func(pid int, sig syscall.Signal) error {
				// Never send an unsafe signal, even with a broken guard.
				t.Fatalf("unsafe signal: pid %d signal %v", pid, sig)
				return nil
			}}
			if err := p.Kill(pgid); err == nil {
				t.Fatal("unsafe process group was accepted")
			}
		})
	}
}

func TestOSProcKillsAProcessGroupAndRefusesToKillOthers(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	pgid := cmd.Process.Pid
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	defer func() { syscall.Kill(-pgid, syscall.SIGKILL); <-done }()

	p := osProc{}
	if !p.Alive(pgid) {
		t.Fatal("a running group is alive")
	}
	if err := p.Kill(pgid); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the process survived Kill")
	}
	if p.Alive(pgid) {
		t.Error("a killed and reaped group is still alive")
	}
	for _, bad := range []int{0, 1, -1, selfPGID()} {
		if err := p.Kill(bad); err == nil {
			t.Errorf("Kill(%d) was allowed", bad)
		}
		if bad <= 1 && p.Alive(bad) {
			t.Errorf("Alive(%d)", bad)
		}
	}
}
