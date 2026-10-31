// SPDX-License-Identifier: GPL-3.0-or-later

//go:build linux

package main

import "syscall"

func getsid(pid int) (int, error) {
	sid, _, errno := syscall.Syscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(sid), nil
}
