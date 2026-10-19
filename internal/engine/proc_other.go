// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package engine

import "errors"

// osProc has no process groups to look at on this platform.
type osProc struct{}

func (osProc) Alive(pgid int) bool { return false }
func (osProc) Kill(pgid int) error {
	return errors.New("process groups are not supported on this platform")
}

func selfPGID() int { return 0 }
