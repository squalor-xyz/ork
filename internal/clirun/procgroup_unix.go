// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package clirun

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// ownProcessGroup puts the child in its own process group and makes the
// context's cancel kill the whole group, grandchildren included.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

// killGroup kills what is left of the child's process group after the child
// exited, for example a grandchild still holding the output pipes.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
