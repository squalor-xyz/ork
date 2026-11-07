// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/registry"
)

// ShellWord quotes s for a shell only when it needs it.
func ShellWord(s string) string {
	plain := s != ""
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-/", r)) {
			plain = false
		}
	}
	if plain {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// NoCheckMessage says why ork refuses to start a run on a project with no
// check command, and how to fix it (adj 191, launch.no-check-command). It is
// also the first setup warning, so the plan, ork doctor and the refusal word it
// the same way.
func NoCheckMessage(name string) string {
	return fmt.Sprintf("no check command: ork refuses to start a run (fix: ork projects set %s --check CMD, or start with --allow-no-check)", ShellWord(name))
}

// NoCheckAllowedNote is the plan's warning for a start that carries
// --allow-no-check, where ork does not refuse: it says only what runs.
func NoCheckAllowedNote() string {
	return "no check command: only slicer check runs after landing"
}

// SetupWarnings lists what is missing in a project's setup (S265, *proposed*),
// in this order: the check command, the built-in Go allowlist on a project
// with no go.mod, a registry allowlist without Read, Edit or Write, and a
// registry allowlist with no pattern for the check command. has reports
// whether a file of that name is in the project directory.
func SetupWarnings(p registry.Project, has func(name string) bool) []string {
	if has == nil {
		has = func(string) bool { return false }
	}
	var out []string
	name := ShellWord(p.Name)
	if p.CheckCommand == "" {
		out = append(out, NoCheckMessage(p.Name))
	}
	entry := p.Permissions[RoleImplementer]
	if entry.Preset != "" {
		return out
	}
	if len(entry.Allow) == 0 {
		if !has("go.mod") {
			out = append(out, fmt.Sprintf("the implementer has the built-in Go allowlist, but %s has no go.mod (fix: %s)", p.Name, presetFix(p, has, name, false)))
		}
		return out
	}
	var missing []string
	for _, tool := range []string{"Read", "Edit", "Write"} {
		if !slices.Contains(entry.Allow, tool) {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		out = append(out, fmt.Sprintf("the implementer allowlist has no %s; it replaces the built-in list as a whole (fix: %s)", joinOr(missing), presetFix(p, has, name, true)))
	}
	if p.CheckCommand != "" && !allowsCommand(entry.Allow, p.CheckCommand) {
		out = append(out, fmt.Sprintf("the implementer allowlist has no pattern for the check command %q (fix: ork projects set %s --allow %s)",
			p.CheckCommand, name, ShellWord("implementer=Bash("+checkPatternPrefix(p.CheckCommand)+"*)")))
	}
	return out
}

// joinOr joins names as "a", "a or b" or "a, b or c".
func joinOr(names []string) string {
	if n := len(names); n > 1 {
		return strings.Join(names[:n-1], ", ") + " or " + names[n-1]
	}
	return strings.Join(names, "")
}

// presetFix is the fix text for a missing implementer list: the preset the
// project's files or check command point to, else a prompt to pick one.
func presetFix(p registry.Project, has func(string) bool, name string, goOK bool) string {
	word, _, _ := strings.Cut(strings.TrimSpace(p.CheckCommand), " ")
	switch {
	case goOK && (has("go.mod") || word == "go"):
		return "ork projects set " + name + " --preset go"
	case has("pyproject.toml") || strings.HasPrefix(word, "python"):
		return "ork projects set " + name + " --preset python"
	}
	return "ork projects set " + name + " --preset go|python, or --allow implementer=PATTERN"
}

// allowsCommand reports whether a pattern of the list covers the command:
// Bash(X*) when the command starts with X, or Bash(X) when it is X.
func allowsCommand(allow []string, command string) bool {
	for _, pat := range allow {
		body, ok := strings.CutPrefix(pat, "Bash(")
		if !ok {
			continue
		}
		body, ok = strings.CutSuffix(body, ")")
		if !ok {
			continue
		}
		if prefix, star := strings.CutSuffix(body, "*"); star {
			if strings.HasPrefix(command, prefix) {
				return true
			}
		} else if command == body {
			return true
		}
	}
	return false
}

// checkPatternPrefix is the first three words of the check command, so one
// pattern covers its variants (python3 -m unittest, go test ./...).
func checkPatternPrefix(command string) string {
	words := strings.Fields(command)
	if len(words) > 3 {
		words = words[:3]
	}
	return strings.Join(words, " ")
}
