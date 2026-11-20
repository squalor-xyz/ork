// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package clirun

import (
	"fmt"
	"io"
	"os"
)

// ReadRegularFile reads a file that a role wrote and refuses anything that is
// not a regular file. This form has no non-blocking open.
func ReadRegularFile(path string) ([]byte, error) {
	f, err := os.Open(path)
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

// OpenNoBlockWrite creates or truncates the file at path for writing and
// refuses anything that is not a regular file.
func OpenNoBlockWrite(path string, perm os.FileMode) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, perm)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%s is not a regular file", path)
	}
	if err == nil {
		err = f.Truncate(0)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
