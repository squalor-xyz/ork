// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package daemon

import (
	"errors"
	"os"
	"syscall"
)

// lockPidfile opens the pidfile and takes an exclusive lock on it. The lock
// lives as long as the returned file is open, and the system drops it when
// the process dies, so a lock that cannot be taken means a live daemon, and a
// pid left in an unlocked file is stale. It returns ErrAlreadyRunning when
// another process holds the lock.
func lockPidfile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return f, nil
}
