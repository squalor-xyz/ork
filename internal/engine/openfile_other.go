// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package engine

import "os"

func openNoBlock(path string) (*os.File, error) { return os.Open(path) }
