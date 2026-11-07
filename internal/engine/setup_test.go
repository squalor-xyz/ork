// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"reflect"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
)

func TestSetupWarnings(t *testing.T) {
	const pyCheck = "python3 -m unittest discover -s tests -t tests"
	const noCheck = "no check command: ork refuses to start a run (fix: ork projects set slicer --check CMD, or start with --allow-no-check)"
	const goDefault = "the implementer has the built-in Go allowlist, but slicer has no go.mod (fix: ork projects set slicer --preset python)"
	files := func(names ...string) func(string) bool {
		return func(name string) bool {
			for _, n := range names {
				if n == name {
					return true
				}
			}
			return false
		}
	}
	implementer := func(allow ...string) map[string]registry.Permission {
		return map[string]registry.Permission{"implementer": {Allow: allow}}
	}
	tests := []struct {
		name string
		p    registry.Project
		has  func(string) bool
		want []string
	}{
		{"go project", registry.Project{Name: "slicer", CheckCommand: "go test ./..."}, files("go.mod"), nil},
		{"no check command", registry.Project{Name: "slicer"}, files("go.mod"), []string{noCheck}},
		{"no check command, python project", registry.Project{Name: "slicer"}, files("pyproject.toml"), []string{noCheck, goDefault}},
		{"no marker file", registry.Project{Name: "slicer"}, files(), []string{noCheck,
			"the implementer has the built-in Go allowlist, but slicer has no go.mod (fix: ork projects set slicer --preset go|python, or --allow implementer=PATTERN)"}},
		{"a nil has reads as no files", registry.Project{Name: "slicer", CheckCommand: "make"}, nil, []string{
			"the implementer has the built-in Go allowlist, but slicer has no go.mod (fix: ork projects set slicer --preset go|python, or --allow implementer=PATTERN)"}},
		{"allowlist without Read, Edit or Write", registry.Project{Name: "slicer", CheckCommand: pyCheck, Permissions: implementer("Bash(python3 -m unittest*)")}, files("pyproject.toml"), []string{
			"the implementer allowlist has no Read, Edit or Write; it replaces the built-in list as a whole (fix: ork projects set slicer --preset python)"}},
		{"allowlist without Read or Write", registry.Project{Name: "slicer", CheckCommand: pyCheck, Permissions: implementer("Edit", "Bash(python3 *)")}, files("pyproject.toml"), []string{
			"the implementer allowlist has no Read or Write; it replaces the built-in list as a whole (fix: ork projects set slicer --preset python)"}},
		{"allowlist without Edit only", registry.Project{Name: "slicer", CheckCommand: pyCheck, Permissions: implementer("Read", "Write", "Bash(python3 *)")}, files("pyproject.toml"), []string{
			"the implementer allowlist has no Edit; it replaces the built-in list as a whole (fix: ork projects set slicer --preset python)"}},
		{"allowlist without a check pattern", registry.Project{Name: "slicer", CheckCommand: pyCheck, Permissions: implementer("Read", "Edit", "Write")}, files(), []string{
			`the implementer allowlist has no pattern for the check command "python3 -m unittest discover -s tests -t tests" (fix: ork projects set slicer --allow 'implementer=Bash(python3 -m unittest*)')`}},
		{"exact check pattern", registry.Project{Name: "slicer", CheckCommand: "make test", Permissions: implementer("Read", "Edit", "Write", "Bash(make test)")}, files(), nil},
		{"prefix check pattern", registry.Project{Name: "slicer", CheckCommand: pyCheck, Permissions: implementer("Read", "Edit", "Write", "Bash(python3 *)")}, files(), nil},
		{"an exact pattern is not a prefix", registry.Project{Name: "slicer", CheckCommand: "make test all", Permissions: implementer("Read", "Edit", "Write", "Bash(make test)")}, files(), []string{
			`the implementer allowlist has no pattern for the check command "make test all" (fix: ork projects set slicer --allow 'implementer=Bash(make test all*)')`}},
		{"read-only implementer", registry.Project{Name: "slicer", CheckCommand: pyCheck, Permissions: map[string]registry.Permission{"implementer": {Preset: "read-only"}}}, files(), nil},
		{"a name with a space", registry.Project{Name: "my proj"}, files("go.mod"), []string{
			"no check command: ork refuses to start a run (fix: ork projects set 'my proj' --check CMD, or start with --allow-no-check)"}},
	}
	for _, tt := range tests {
		if got := SetupWarnings(tt.p, tt.has); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, got, tt.want)
		}
	}
}

func TestShellWordQuotesOnlyWhenNeeded(t *testing.T) {
	for in, want := range map[string]string{"slicer": "slicer", "a/b-c_d.e": "a/b-c_d.e", "my proj": "'my proj'", "it's": `'it'"'"'s'`, "": "''"} {
		if got := ShellWord(in); got != want {
			t.Errorf("ShellWord(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNoCheckMessage(t *testing.T) {
	if got := NoCheckMessage("p"); got != "no check command: ork refuses to start a run (fix: ork projects set p --check CMD, or start with --allow-no-check)" {
		t.Fatal(got)
	}
}

func TestNoCheckAllowedNote(t *testing.T) {
	if got := NoCheckAllowedNote(); got != "no check command: only slicer check runs after landing" {
		t.Fatal(got)
	}
}
