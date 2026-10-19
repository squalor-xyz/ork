// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package daemon

import (
	"errors"
	"os"
)

// lockPidfile is not supported here: the daemon needs a Unix socket and file
// locks.
func lockPidfile(path string) (*os.File, error) {
	return nil, errors.New("the ork daemon is supported on Unix systems only")
}
