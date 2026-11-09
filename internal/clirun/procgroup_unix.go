// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package clirun

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// OwnProcessGroup puts the child in its own process group and makes the
// context's cancel kill the whole group, grandchildren included, and the
// processes below the child that left the group (see KillTree).
func OwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := killTree(cmd.Process.Pid)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

// KillGroup kills what is left of the child's process group after the child
// exited, for example a grandchild still holding the output pipes. It kills
// the group only: once the child is gone, a process that left the group has
// been reparented to pid 1 and cannot be found from the child.
func KillGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
