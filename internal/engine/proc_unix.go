// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package engine

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/squalor-xyz/ork/internal/clirun"
)

// osProc is the operating system's process table.
type osProc struct {
	signal func(int, syscall.Signal) error // nil uses syscall.Kill
}

// Alive reports whether the process group exists. A group id of 1 or less is
// never a role's group.
func (osProc) Alive(pgid int) bool {
	if pgid <= 1 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Kill sends SIGKILL to the whole process group. When the group's leader is
// still alive, it also kills the processes below the leader that left the
// group (*proposed*; found from a process table snapshot taken before the
// kill, so a process already reparented to pid 1 is not found). It refuses
// group 0 and 1, negative ids and ork's own group.
func (p osProc) Kill(pgid int) error {
	if pgid <= 1 || pgid == selfPGID() {
		return fmt.Errorf("refusing to kill process group %d", pgid)
	}
	signal := p.signal
	if signal == nil {
		signal = syscall.Kill
	}
	var below []int
	if leaderAlive(pgid) {
		below = clirun.Descendants(pgid)
	}
	err := signal(-pgid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		err = nil
	}
	for _, pid := range below {
		if derr := signal(pid, syscall.SIGKILL); derr != nil && !errors.Is(derr, syscall.ESRCH) && err == nil {
			err = derr
		}
	}
	return err
}

// leaderAlive reports whether pgid is the pid of a live process that leads its
// own group. The check keeps a recycled pid from being taken for the leader.
func leaderAlive(pgid int) bool {
	g, err := syscall.Getpgid(pgid)
	return err == nil && g == pgid
}

func selfPGID() int { return syscall.Getpgrp() }
