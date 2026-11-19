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
// Every prompt names {{report_path}} and {{temp_dir}}. The UI prompt also uses
// {{renders_dir}}, the stable run asset path independent of process TMPDIR.
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

// localSystem is ork's own system prompt for a claude-local role (S364,
// adj 351, *proposed*). It is not a role prompt: it replaces Claude Code's
// default system prompt, and the role prompt stays the user message. It holds
// no placeholders and is under 2,000 characters.
//
//go:embed prompts/local-system.md
var localSystem string

// LocalSystemPrompt returns ork's own system prompt for a claude-local role.
func LocalSystemPrompt() string { return localSystem }

// carve is the spec check row's task when a local implementer's slice above
// size S is cut into slivers (S359, *proposed*). It is not a role: ork briefs
// it as the spec-check role, and its placeholders are the closed set above.
//
//go:embed prompts/carve.md
var carve string

// CarvePrompt returns the carve prompt, with its placeholders unfilled.
func CarvePrompt() string { return carve }

// ContractVersion is the version of slicer's relay contract that the prompts
// are written against. It matches each prompt's first line. ork accepts the
// versions in AcceptedContracts, which include this one.
const ContractVersion = 13

// names lists the roles. lead and part are the roles of a split run (S61,
// *proposed*): the lead is the one implementer of the slice, and a part
// changes only its own files and runs no slicer command that writes.
//
// spec-draft is the read-only role that proposes a slice's spec sections for
// the owner to approve one by one (S179, scope.spec-drafting).
var names = []string{"implementer", "reviewer", "gate", "architect", "ui", "spec-check", "lead", "part", "spec-draft"}

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

// LocalImplementerPrompt replaces the cloud Work section for a small context
// window, sharing the rules, owner questions and report contract unchanged.
func LocalImplementerPrompt() (string, error) {
	prompt, err := Prompt("implementer")
	if err != nil {
		return "", err
	}
	before, work, _ := strings.Cut(prompt, "## Work\n")
	_, after, _ := strings.Cut(work, "## Later rounds")
	return before + `## Work

1. Write your progress note as instructed above before any reads.
2. Read the project's AGENTS.md and this slice end to end, including Not in this
   slice and the rest of the brief. The anchors were checked by the spec check;
   trust the symbol names. Read only the source ranges needed for the current test.
3. Start this slice unless the brief says it is already started. Touch no other item.
4. For one test at a time: write one listed test, see it fail for the stated reason
   and save its failure line, write the code, see it pass, update the note, then
   proceed to the next test. If it passes before the code, fix the test.
5. After the last test, mutation-check every new assertion once: break the product,
   see that assertion fail for the right reason, restore and verify the file.
   Then run the full test suite of each affected project once. Never ship a green
   mutation. Use one variable per differential test and assert on real output.
   If the premise is false, stop and report a refutation instead of inventing a fix.
6. Update docs with the code. Before reporting, re-read Not in this slice and
   check the diff contains only named files, or explain why. Verify cited symbols.

` + "## Later rounds" + after, nil
}

// acceptedContracts is the owner-decided supported set: v12 and v13.
// v11 is dropped (adj 470, boundary.contract-set, decided 2026-11-18;
// supersedes adj 326).
var acceptedContracts = []int{12, 13}

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
		return v, fmt.Errorf("ork accepts relay %s and slicer prints v%d", AcceptedContractsPhrase(), v)
	}
	return v, nil
}

// AcceptedContractsPhrase returns the accepted versions as a readable list.
func AcceptedContractsPhrase() string { return phrase(acceptedContracts) }

// acceptedAdjContracts is the adj JSON contract ork accepts (adj 97,
// boundary.contract-pin, decided ork-sets-checks).
var acceptedAdjContracts = []int{1}

// AcceptedAdjContracts returns the adj contract versions ork supports.
func AcceptedAdjContracts() []int { return slices.Clone(acceptedAdjContracts) }

// AdjContractAccepted reports whether v is in the accepted adj set.
func AdjContractAccepted(v int) bool { return slices.Contains(acceptedAdjContracts, v) }

// AcceptedAdjContractsPhrase returns the adj contract ork accepts.
func AcceptedAdjContractsPhrase() string { return "adj-json v1" }

// CheckAdjContract refuses a pin that is not the version adj speaks, a pin
// outside the accepted set, or a spoken version outside it (adj 97). An
// unpinned repo is checked against the accepted set alone. The error names
// the versions.
func CheckAdjContract(contract string, version int, pinned *int) error {
	spoken := contract
	if spoken == "" {
		spoken = fmt.Sprintf("adj-json v%d", version)
	}
	if pinned != nil && *pinned != version {
		return fmt.Errorf("this project pins adj-json v%d, but this adj speaks %s", *pinned, spoken)
	}
	if pinned != nil && !AdjContractAccepted(*pinned) {
		return fmt.Errorf("adj pin v%d is outside what ork accepts (%s)", *pinned, AcceptedAdjContractsPhrase())
	}
	if !AdjContractAccepted(version) {
		return fmt.Errorf("this adj speaks %s, which ork does not accept (%s)", spoken, AcceptedAdjContractsPhrase())
	}
	return nil
}

func phrase(versions []int) string {
	parts := make([]string, len(versions))
	for i, v := range versions {
		parts[i] = "v" + strconv.Itoa(v)
	}
	if len(parts) < 2 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1]
}
