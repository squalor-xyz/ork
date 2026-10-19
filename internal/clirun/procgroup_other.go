// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package clirun

import "os/exec"

// ownProcessGroup does nothing here: a cancel kills only the child.
func ownProcessGroup(cmd *exec.Cmd) {}

// killGroup does nothing here: there is no process group to kill.
func killGroup(cmd *exec.Cmd) {}
