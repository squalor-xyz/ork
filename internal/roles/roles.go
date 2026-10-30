// SPDX-License-Identifier: GPL-3.0-or-later

// Package roles holds ork's role prompts. They are embedded in the binary and
// canonical (roles.prompts), seeded from slicer's relay contract and written
// for a role that ork launches headless, with no orchestrator agent.
//
// Placeholders. A prompt holds {{name}} tokens that the engine fills when it
// builds a role's brief (S04). This package returns the prompt text with the
// tokens unfilled. The set is closed:
//
//	{{slice_id}}    the slice id, for example S10; the spec check gets the ids of
//	                its batch, comma-separated
//	{{worktree}}    the absolute path of the slice worktree
//	{{temp_dir}}    the role's own temp directory
//	{{report_path}} the file where the role writes its final report
//
// Every prompt names {{report_path}} and {{temp_dir}}.
package roles

import (
	"embed"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

//go:embed prompts/*.md
var promptFS embed.FS

// ContractVersion is the version of slicer's relay contract that the prompts
// are written against. It matches each prompt's first line. ork accepts the
// versions in AcceptedContracts, which include this one.
const ContractVersion = 11

var names = []string{"implementer", "reviewer", "gate", "architect", "ui", "spec-check"}

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

// acceptedContracts is the owner-decided supported set: v10 and v11, v9 dropped
// (adj 175, boundary.contract-set, decided 2026-10-30; it follows S142 and
// boundary.contract).
var acceptedContracts = []int{10, 11}

// AcceptedContracts returns the relay contract versions ork supports
// (boundary.contract).
func AcceptedContracts() []int { return slices.Clone(acceptedContracts) }

// ContractAccepted reports whether v is in the accepted set.
func ContractAccepted(v int) bool { return slices.Contains(acceptedContracts, v) }

// contractLine matches the first line of `slicer ai relay`.
var contractLine = regexp.MustCompile(`^Contract: relay v(\d+)\s*$`)

// CheckContractLine parses the first line of `slicer ai relay` and checks the
// version against the accepted set. It returns the version found, even when
// that version is refused, and 0 when the line is not a contract line.
func CheckContractLine(line string) (int, error) {
	m := contractLine.FindStringSubmatch(line)
	if m == nil {
		return 0, errors.New("the first line of `slicer ai relay` is not a contract line")
	}
	v, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, errors.New("the first line of `slicer ai relay` is not a contract line")
	}
	if !ContractAccepted(v) {
		return v, fmt.Errorf("ork accepts relay %s and slicer prints v%d", versionList(), v)
	}
	return v, nil
}

func versionList() string {
	parts := make([]string, len(acceptedContracts))
	for i, v := range acceptedContracts {
		parts[i] = "v" + strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}
