// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package clitest

import (
	"errors"
	"os"
	"syscall"
)

// lock serializes fake calls. Closing the file or killing its process releases
// the lock, including when a heartbeat call is cancelled during its reply.
// Keep the file in place so every caller locks the same inode.
func lock(path string) func() {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		f.Close()
		return func() {}
	}
	return func() { f.Close() }
}
