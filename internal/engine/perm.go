// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"

	"github.com/squalor-xyz/ork/internal/registry"
)

// Permission is how a role may act: in the vendor's read-only mode, or with
// an allowlist of tools.
type Permission struct {
	ReadOnly bool
	Allow    []string
}

// presetReadOnly is the one registry preset (docs/state.md).
const presetReadOnly = "read-only"

// DefaultImplementerAllowlist is the built-in allowlist of an implementer
// whose project has no permissions.implementer entry (launch.default-allowlist).
// The check command's entry is left out when the project has no check command.
func DefaultImplementerAllowlist(checkCommand string) []string {
	list := []string{
		"Read", "Grep", "Glob", "Edit", "Write",
		"Bash(slicer *)", "Bash(git status*)", "Bash(git diff*)", "Bash(git log*)",
		"Bash(go -C * test*)", "Bash(go -C * vet*)", "Bash(go test*)", "Bash(go vet*)", "Bash(gofmt *)",
		"Bash(git -C * status*)", "Bash(git -C * diff*)", "Bash(git -C * log*)",
		"Bash(adj check*)", "Bash(adj lint*)", "Bash(adj show*)", "Bash(adj inbox*)",
	}
	if checkCommand != "" {
		list = append(list, "Bash("+checkCommand+"*)")
	}
	return list
}

func readOnlyRole(role string) bool {
	return role == RoleReviewer || role == RoleGate || role == RoleArchitect
}

// ResolvePermission returns the permission of a role in a project. The
// implementer gets the registry's permissions.implementer, else the built-in
// default (an empty list counts as no entry). The reviewer, the gate and the
// architect are read-only; a registry entry that gives one of them an
// allowlist is an error.
func ResolvePermission(p registry.Project, role string) (Permission, error) {
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
