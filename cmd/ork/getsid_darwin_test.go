// SPDX-License-Identifier: GPL-3.0-or-later

//go:build darwin

package main

import "syscall"

func getsid(pid int) (int, error) {
	return syscall.Getsid(pid)
}
