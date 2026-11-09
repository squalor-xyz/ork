// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package clirun

// KillTree does nothing here: there is no process group or process table to
// walk.
func KillTree(pid int) error { return nil }

// Descendants finds nothing here.
func Descendants(pid int) []int { return nil }
