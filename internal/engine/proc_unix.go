// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package engine

import (
	"errors"
	"fmt"
	"syscall"
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

// Kill sends SIGKILL to the whole process group. It refuses group 0 and 1,
// negative ids and ork's own group.
func (p osProc) Kill(pgid int) error {
	if pgid <= 1 || pgid == selfPGID() {
		return fmt.Errorf("refusing to kill process group %d", pgid)
	}
	signal := p.signal
	if signal == nil {
		signal = syscall.Kill
	}
	err := signal(-pgid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func selfPGID() int { return syscall.Getpgrp() }
