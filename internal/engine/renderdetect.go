// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"os"
	"path/filepath"
	"strings"
)

// renderTestsIn recognizes the render tests without following linked directories
// or files. Missing or unreadable paths do not advertise a render command.
func renderTestsIn(worktree string) (cli, tui bool) {
	has := func(pkg, name string) bool {
		dir := worktree
		for _, part := range []string{"internal", pkg} {
			dir = filepath.Join(dir, part)
			info, err := os.Lstat(dir)
			if err != nil || !info.IsDir() {
				return false
			}
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err == nil && strings.Contains(string(body), "func "+name+"(") {
				return true
			}
		}
		return false
	}
	return has("cli", "TestRenderCLIViews"), has("tui", "TestRenderTUIViews")
}
