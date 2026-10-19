// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package clirun

import "os/exec"

// OwnProcessGroup does nothing here: a cancel kills only the child.
func OwnProcessGroup(cmd *exec.Cmd) {}

// KillGroup does nothing here: there is no process group to kill.
func KillGroup(cmd *exec.Cmd) {}
