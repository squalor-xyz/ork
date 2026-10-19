// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package main

import "os/exec"

// detach does nothing here: the daemon needs Unix sockets and file locks, and
// its start fails on these systems anyway.
func detach(cmd *exec.Cmd) {}
