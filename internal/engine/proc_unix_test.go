// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package engine

import (
	"fmt"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

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
