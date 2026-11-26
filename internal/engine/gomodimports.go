// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// goModuleImportsSection lists the module paths the worktree's go.mod requires,
// so a reviewer's probe test imports a library by the path the module uses and
// not by one from memory (S589, adj 590). ork reads the file, so the prompt
// carries no project-specific path. A missing go.mod means the role has no Go
// worktree and gives no section; any other read error gives an advisory
// section and never blocks the run. It returns nil when go.mod holds no
// require path.
func goModuleImportsSection(worktree string) *BriefSection {
	const title = "Go module imports"
	raw, err := os.ReadFile(filepath.Join(worktree, "go.mod"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return &BriefSection{Title: title, Body: "The require paths of the worktree's `go.mod` could not be read. Read `go.mod` yourself and import a library in a probe test by the exact path of its `require` line; this does not block the run."}
	}
	paths := goModRequirePaths(string(raw))
	if len(paths) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("These are the module paths the worktree's `go.mod` requires. A probe test imports a package from one of them by exactly this path, with any sub-package after it (for example `charm.land/bubbletea/v2`, not `github.com/charmbracelet/bubbletea`). Use these exact paths in a probe test's `import` lines.\n\n")
	for _, p := range paths {
		b.WriteString("- `" + p + "`\n")
	}
	return &BriefSection{Title: title, Body: strings.TrimRight(b.String(), "\n")}
}

// goModRequirePaths returns the paths of the require lines of a go.mod, in
// file order and without duplicates. It reads the first field of each line in a
// `require (` block and of a `require PATH VERSION` line, and ignores every
// other directive and block.
func goModRequirePaths(text string) []string {
	var paths []string
	inRequire, inOther := false, false
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch {
		case inRequire:
			if fields[0] == ")" {
				inRequire = false
			} else {
				paths = append(paths, fields[0])
			}
		case inOther:
			if fields[0] == ")" {
				inOther = false
			}
		case fields[0] == "require":
			switch {
			case len(fields) == 2 && fields[1] == "(":
				inRequire = true
			case len(fields) >= 2 && fields[1] != "(":
				paths = append(paths, fields[1])
			}
		case len(fields) == 2 && fields[1] == "(":
			inOther = true
		}
	}
	seen := map[string]bool{}
	return slices.DeleteFunc(paths, func(p string) bool {
		if seen[p] {
			return true
		}
		seen[p] = true
		return false
	})
}
