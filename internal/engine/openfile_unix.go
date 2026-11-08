// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package engine

import (
	"os"
	"syscall"
)

func openNoBlock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
