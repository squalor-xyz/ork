// SPDX-License-Identifier: GPL-3.0-or-later

// Package roles holds ork's role prompts. They are embedded in the binary and
// canonical (roles.prompts), seeded from slicer's relay contract and written
// for a role that ork launches headless, with no orchestrator agent.
//
// Placeholders. A prompt holds {{name}} tokens that the engine fills when it
// builds a role's brief (S04). This package returns the prompt text with the
// tokens unfilled. The set is closed:
//
//	{{slice_id}}    the slice id, for example S10
//	{{worktree}}    the absolute path of the slice worktree
//	{{temp_dir}}    the role's own temp directory
//	{{report_path}} the file where the role writes its final report
//
// Every prompt names {{report_path}} and {{temp_dir}}.
package roles

import (
	"embed"
	"fmt"
	"slices"
)

//go:embed prompts/*.md
var promptFS embed.FS

// ContractVersion is the version of slicer's relay contract that the prompts
// are written against. It matches the first line of `slicer ai relay`
// ("Contract: relay v7") and each prompt's first line.
const ContractVersion = 7

var names = []string{"implementer", "reviewer", "gate", "architect"}

// Names returns the role names, in the order ork lists them.
func Names() []string { return slices.Clone(names) }

// Prompt returns the embedded prompt of the named role, with its placeholders
// unfilled. An unknown name is an error that names it.
func Prompt(name string) (string, error) {
	if !slices.Contains(names, name) {
		return "", fmt.Errorf("unknown role %q", name)
	}
	b, err := promptFS.ReadFile("prompts/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("role %q: %w", name, err)
	}
	return string(b), nil
}
