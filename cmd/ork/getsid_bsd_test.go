// SPDX-License-Identifier: GPL-3.0-or-later

//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import "syscall"

func getsid(pid int) (int, error) {
	return syscall.Getsid(pid)
}
