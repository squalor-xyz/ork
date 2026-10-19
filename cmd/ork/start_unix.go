// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach puts the child in its own session, so it has no controlling terminal
// and a closed terminal does not reach it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
