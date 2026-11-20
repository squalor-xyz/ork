// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// Permission is how a role may act: in the vendor's read-only mode, or with
// an allowlist of tools.
type Permission struct {
	ReadOnly bool
	Allow    []string
}

// LocalReadOnlyAllowlist is the fixed tool list shared by read-only cloud
// Claude (adj 489) and local launchers (adj 307). The original name is kept
// for callers. It avoids plan mode's model-backed classifier. Each call
// returns its own slice.
func LocalReadOnlyAllowlist() []string {
	return []string{
		"Read", "Grep", "Glob",
		"Bash(git diff*)", "Bash(git log*)", "Bash(git status*)", "Bash(git show*)",
		"Bash(git -C * diff*)", "Bash(git -C * log*)", "Bash(git -C * status*)", "Bash(git -C * show*)",
		"Bash(slicer show*)", "Bash(slicer goals*)", "Bash(slicer list*)", "Bash(slicer deps*)", "Bash(slicer find*)", "Bash(slicer check*)",
		"Bash(adj show*)", "Bash(adj check*)", "Bash(adj lint*)",
		"Bash(go test*)", "Bash(go vet*)", "Bash(go -C * test*)", "Bash(go -C * vet*)",
		"Bash(gofmt -l*)",
	}
}

// presetReadOnly is the one registry preset (docs/state.md).
const presetReadOnly = "read-only"

// PresetNames are the implementer allowlist presets (S265, *proposed*).
func PresetNames() []string { return []string{"go", "python"} }

// ImplementerPreset is the full implementer allowlist of a language (S265,
// *proposed*): the base list, the language's entries, then the check
// command's entry when the project has a check command. The go preset is the
// built-in default (launch.default-allowlist). Each call returns its own slice.
func ImplementerPreset(name, checkCommand string) ([]string, error) {
	head := []string{
		"Read", "Grep", "Glob", "Edit", "Write",
		"Bash(slicer *)", "Bash(git status*)", "Bash(git diff*)", "Bash(git log*)",
		"Bash(git ls-files*)", "Bash(git mv*)", "Bash(git rm *)", "Bash(rm *)",
	}
	tail := []string{
		"Bash(git -C * status*)", "Bash(git -C * diff*)", "Bash(git -C * log*)",
		"Bash(git -C * ls-files*)", "Bash(git -C * mv*)", "Bash(git -C * rm *)",
		"Bash(adj check*)", "Bash(adj lint*)", "Bash(adj show*)", "Bash(adj inbox*)",
		"Bash(ork show*)",
	}
	var list []string
	switch name {
	case "go":
		list = append(head, "Bash(go -C * test*)", "Bash(go -C * vet*)", "Bash(go test*)", "Bash(go vet*)", "Bash(gofmt *)")
		list = append(list, tail...)
	case "python":
		list = append(head, tail...)
		list = append(list, "Bash(python3 -m unittest*)")
	default:
		return nil, fmt.Errorf("unknown preset %q (presets: %s)", name, strings.Join(PresetNames(), ", "))
	}
	if checkCommand != "" {
		list = append(list, "Bash("+checkCommand+"*)")
	}
	return list, nil
}

// DefaultImplementerAllowlist is the built-in allowlist of an implementer
// whose project has no permissions.implementer entry (launch.default-allowlist):
// the go preset. The check command's entry is left out when the project has no
// check command.
func DefaultImplementerAllowlist(checkCommand string) []string {
	list, _ := ImplementerPreset("go", checkCommand)
	return list
}

// partSlicerReads are the slicer commands that only read. A part keeps these
// in place of the implementer's `Bash(slicer *)`: it runs no slicer command
// that writes (slicer ai.py, Roles; S61, *proposed*).
var partSlicerReads = []string{"Bash(slicer show*)", "Bash(slicer goals*)", "Bash(slicer list*)", "Bash(slicer deps*)", "Bash(slicer find*)"}

// partAllowlist is the implementer's allowlist with the slicer entry that
// writes replaced by the entries that only read. Any other slicer entry stays
// only when it is one of those reads.
func partAllowlist(implementer []string) []string {
	var out []string
	for _, entry := range implementer {
		switch {
		case entry == "Bash(slicer *)":
			out = append(out, partSlicerReads...)
		case strings.HasPrefix(entry, "Bash(slicer ") && !slices.Contains(partSlicerReads, entry):
			// a slicer entry that may write
		default:
			out = append(out, entry)
		}
	}
	return out
}

// rolePermission is ResolvePermission for a role of this run (S346,
// *proposed*; adj 479): the implementer, and a lead or part that derives from
// it, also get the patterns the owner granted the run with ork retry --allow.
// A part keeps no slicer write entry, a grant included. A read-only role, and
// an implementer the project made read-only, keep their permission. The
// project in the registry is never changed.
func (e *Engine) rolePermission(rec runrec.Record, role string) (Permission, error) {
	perm, err := ResolvePermission(e.proj, role)
	if err != nil || perm.ReadOnly || len(rec.Grants) == 0 || (role != RoleImplementer && role != RolePart) {
		return perm, err
	}
	grants := rec.Grants
	if role == RolePart {
		grants = partAllowlist(grants)
	}
	perm.Allow = mergeGrants(perm.Allow, grants)
	return perm, nil
}

func readOnlyRole(role string) bool {
	return role == RoleReviewer || role == RoleGate || role == RoleArchitect || role == RoleUI || role == RoleSpecCheck || role == RoleSpecDraft
}

// ResolvePermission returns the permission of a role in a project. The
// implementer gets the registry's permissions.implementer, else the built-in
// default (an empty list counts as no entry). The reviewer, the gate, the
// architect, the UI pass, the spec check and the spec draft are read-only; a registry entry that gives one of them an
// allowlist is an error.
func ResolvePermission(p registry.Project, role string) (Permission, error) {
	if role == RolePart {
		// A part has the implementer's permission, less the slicer writes.
		perm, err := ResolvePermission(p, RoleImplementer)
		if err != nil {
			return Permission{}, err
		}
		perm.Allow = partAllowlist(perm.Allow)
		return perm, nil
	}
	entry, has := p.Permissions[role]
	switch {
	case readOnlyRole(role):
		if has && len(entry.Allow) > 0 {
			return Permission{}, fmt.Errorf("role %s is read-only, but the registry gives it an allowlist", role)
		}
		if has && entry.Preset != "" && entry.Preset != presetReadOnly {
			return Permission{}, fmt.Errorf("role %s: unknown permission preset %q", role, entry.Preset)
		}
		return Permission{ReadOnly: true}, nil
	case role == RoleImplementer:
		if has && entry.Preset != "" {
			if entry.Preset != presetReadOnly {
				return Permission{}, fmt.Errorf("role %s: unknown permission preset %q", role, entry.Preset)
			}
			return Permission{ReadOnly: true}, nil
		}
		if has && len(entry.Allow) > 0 {
			return Permission{Allow: append([]string(nil), entry.Allow...)}, nil
		}
		return Permission{Allow: DefaultImplementerAllowlist(p.CheckCommand)}, nil
	}
	return Permission{}, fmt.Errorf("unknown role %q", role)
}
