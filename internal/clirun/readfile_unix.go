// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package clirun

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// ReadRegularFile reads a file that a role wrote. It never blocks on a FIFO
// (the open is non-blocking) and it refuses a symlink, a FIFO, a device or any
// other file that is not a regular file. A missing file returns the error of
// os.Open.
func ReadRegularFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return io.ReadAll(f)
}

// OpenNoBlockWrite creates or truncates the file at path for writing. It never
// blocks on a FIFO and it refuses a symlink or any file that is not a regular
// file; a FIFO with no reader is refused by the open itself.
func OpenNoBlockWrite(path string, perm os.FileMode) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, perm)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%s is not a regular file", path)
	}
	// Truncate only after the check: O_TRUNC on a device or a FIFO is not ours.
	if err == nil {
		err = f.Truncate(0)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
