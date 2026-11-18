// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package clitest

import (
	"errors"
	"io/fs"
	"os"
	"time"
)

// lock retains the existing exclusive-file fallback on non-Unix platforms.
func lock(path string) func() {
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(path) }
		}
		if !errors.Is(err, fs.ErrExist) {
			return func() {}
		}
		time.Sleep(time.Millisecond)
	}
}
