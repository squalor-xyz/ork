// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package engine

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// lockMainFile waits for the repository's main writer. Closing the file
// releases the lock, including when the process exits.
func lockMainFile(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { f.Close() }, nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
