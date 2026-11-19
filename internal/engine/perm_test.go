// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// S346 (*proposed*): rolePermission adds a run's grants to the implementer and
// to a part (less its slicer writes), and to no other role.
func TestRolePermissionAddsTheRunsGrants(t *testing.T) {
	grants := []string{"Bash(make test*)", "Bash(slicer note*)"}
	e := &Engine{proj: registry.Project{Permissions: map[string]registry.Permission{RoleImplementer: {Allow: []string{"Read", "Edit"}}}}}
	rec := runrec.Record{Grants: grants}
	for _, tc := range []struct {
		role string
		want []string // nil: read-only
	}{
		{RoleImplementer, []string{"Read", "Edit", "Bash(make test*)", "Bash(slicer note*)"}},
		{RolePart, []string{"Read", "Edit", "Bash(make test*)"}},
		{RoleReviewer, nil}, {RoleGate, nil}, {RoleArchitect, nil}, {RoleUI, nil},
	} {
		got, err := e.rolePermission(rec, tc.role)
		if err != nil || got.ReadOnly != (tc.want == nil) || !reflect.DeepEqual(got.Allow, tc.want) {
			t.Errorf("%s: %+v, %v; want allow %v", tc.role, got, err, tc.want)
		}
	}
	// A run with no grants resolves as the registry says; a read-only
	// implementer stays read-only.
	if got, _ := e.rolePermission(runrec.Record{}, RoleImplementer); !reflect.DeepEqual(got.Allow, []string{"Read", "Edit"}) {
		t.Errorf("no grants: %v", got.Allow)
	}
	e.proj.Permissions = map[string]registry.Permission{RoleImplementer: {Preset: "read-only"}}
	if got, err := e.rolePermission(rec, RoleImplementer); err != nil || !got.ReadOnly || len(got.Allow) != 0 {
		t.Errorf("read-only implementer with grants: %+v, %v", got, err)
	}
}

func TestReadOnlyAllowlistIncludesAdjLint(t *testing.T) {
	if !slices.Contains(LocalReadOnlyAllowlist(), "Bash(adj lint*)") {
		t.Fatal("read-only allowlist missing Bash(adj lint*)")
	}
}

func TestReadOnlyAllowlistIncludesSlicerCheck(t *testing.T) {
	if !slices.Contains(LocalReadOnlyAllowlist(), "Bash(slicer check*)") {
		t.Fatal("read-only allowlist missing Bash(slicer check*)")
	}
}

func TestDefaultImplementerAllowlistIsExactlyTheDecidedOne(t *testing.T) {
	want := []string{
		"Read", "Grep", "Glob", "Edit", "Write",
		"Bash(slicer *)", "Bash(git status*)", "Bash(git diff*)", "Bash(git log*)",
		"Bash(git ls-files*)", "Bash(git mv*)", "Bash(git rm *)", "Bash(rm *)",
		"Bash(go -C * test*)", "Bash(go -C * vet*)", "Bash(go test*)", "Bash(go vet*)", "Bash(gofmt *)",
		"Bash(git -C * status*)", "Bash(git -C * diff*)", "Bash(git -C * log*)",
		"Bash(git -C * ls-files*)", "Bash(git -C * mv*)", "Bash(git -C * rm *)",
		"Bash(adj check*)", "Bash(adj lint*)", "Bash(adj show*)", "Bash(adj inbox*)",
		"Bash(go test ./...*)",
	}
	if got := DefaultImplementerAllowlist("go test ./..."); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if got := DefaultImplementerAllowlist(""); !reflect.DeepEqual(got, want[:len(want)-1]) {
		t.Fatalf("without a check command: %q", got)
	}
	// Each call returns a slice of its own.
	a := DefaultImplementerAllowlist("x")
	a[0] = "Changed"
	if b := DefaultImplementerAllowlist("x"); b[0] != "Read" {
		t.Fatalf("callers share the default: %q", b)
	}
}

func TestImplementerGetsTheRegistryAllowlistAsIs(t *testing.T) {
	p := registry.Project{CheckCommand: "go test ./...", Permissions: map[string]registry.Permission{
		"implementer": {Allow: []string{"Edit", "Bash(dotnet test*)"}},
	}}
	got, err := ResolvePermission(p, RoleImplementer)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReadOnly || !reflect.DeepEqual(got.Allow, []string{"Edit", "Bash(dotnet test*)"}) {
		t.Fatalf("the registry list must replace the default, not extend it: %+v", got)
	}
}

func TestImplementerWithoutARegistryEntryGetsTheDefault(t *testing.T) {
	for name, p := range map[string]registry.Project{
		"no permissions":  {CheckCommand: "go test ./..."},
		"other role only": {CheckCommand: "go test ./...", Permissions: map[string]registry.Permission{"reviewer": {Preset: "read-only"}}},
		"empty list":      {CheckCommand: "go test ./...", Permissions: map[string]registry.Permission{"implementer": {Allow: []string{}}}},
	} {
		got, err := ResolvePermission(p, RoleImplementer)
		if err != nil || got.ReadOnly || !reflect.DeepEqual(got.Allow, DefaultImplementerAllowlist("go test ./...")) {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
}

func TestImplementerPresetReadOnlyAndUnknownPreset(t *testing.T) {
	p := registry.Project{Permissions: map[string]registry.Permission{"implementer": {Preset: "read-only"}}}
	if got, err := ResolvePermission(p, RoleImplementer); err != nil || !got.ReadOnly || len(got.Allow) != 0 {
		t.Fatalf("%+v, %v", got, err)
	}
	p.Permissions["implementer"] = registry.Permission{Preset: "yolo"}
	if _, err := ResolvePermission(p, RoleImplementer); err == nil || !strings.Contains(err.Error(), "yolo") {
		t.Fatalf("unknown preset: %v", err)
	}
}

func TestReadOnlyRolesGetReadOnlyMode(t *testing.T) {
	for _, role := range []string{RoleReviewer, RoleGate, "architect"} {
		for name, p := range map[string]registry.Project{
			"no entry": {},
			"preset":   {Permissions: map[string]registry.Permission{role: {Preset: "read-only"}}},
		} {
			got, err := ResolvePermission(p, role)
			if err != nil || !got.ReadOnly || len(got.Allow) != 0 {
				t.Errorf("%s %s: %+v, %v", role, name, got, err)
			}
		}
		p := registry.Project{Permissions: map[string]registry.Permission{role: {Allow: []string{"Edit"}}}}
		if _, err := ResolvePermission(p, role); err == nil || !strings.Contains(err.Error(), role) {
			t.Errorf("%s with an allowlist must be an error naming the role: %v", role, err)
		}
	}
	if _, err := ResolvePermission(registry.Project{}, "janitor"); err == nil {
		t.Error("an unknown role was accepted")
	}
}

func TestImplementerPresetGoIsTheBuiltInDefault(t *testing.T) {
	for _, check := range []string{"go test ./...", ""} {
		got, err := ImplementerPreset("go", check)
		if err != nil || !reflect.DeepEqual(got, DefaultImplementerAllowlist(check)) {
			t.Errorf("check %q: %q, %v", check, got, err)
		}
	}
}

func TestImplementerPresetPython(t *testing.T) {
	check := "python3 -m unittest discover -s tests -t tests"
	want := []string{
		"Read", "Grep", "Glob", "Edit", "Write",
		"Bash(slicer *)", "Bash(git status*)", "Bash(git diff*)", "Bash(git log*)",
		"Bash(git ls-files*)", "Bash(git mv*)", "Bash(git rm *)", "Bash(rm *)",
		"Bash(git -C * status*)", "Bash(git -C * diff*)", "Bash(git -C * log*)",
		"Bash(git -C * ls-files*)", "Bash(git -C * mv*)", "Bash(git -C * rm *)",
		"Bash(adj check*)", "Bash(adj lint*)", "Bash(adj show*)", "Bash(adj inbox*)",
		"Bash(python3 -m unittest*)", "Bash(python3 -m unittest discover -s tests -t tests*)",
	}
	got, err := ImplementerPreset("python", check)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q, %v\nwant %q", got, err, want)
	}
	// Without a check command the list ends with the unittest entry.
	got, err = ImplementerPreset("python", "")
	if err != nil || !reflect.DeepEqual(got, want[:len(want)-1]) {
		t.Fatalf("without a check command: %q, %v", got, err)
	}
	// Each call returns a slice of its own.
	got[0] = "Changed"
	if again, _ := ImplementerPreset("python", ""); again[0] != "Read" {
		t.Fatalf("callers share the preset: %q", again)
	}
}

func TestImplementerPresetsAllowGitLsFilesAndMv(t *testing.T) {
	for _, preset := range PresetNames() {
		for _, entry := range []string{"Bash(git ls-files*)", "Bash(git -C * ls-files*)", "Bash(git mv*)", "Bash(git -C * mv*)"} {
			t.Run(preset+"/"+entry, func(t *testing.T) {
				list, err := ImplementerPreset(preset, "")
				if err != nil || !slices.Contains(list, entry) {
					t.Fatalf("%s missing %s: %v", preset, entry, err)
				}
			})
		}
	}
}

func TestImplementerPresetsAllowFileRemoval(t *testing.T) {
	for _, preset := range PresetNames() {
		for _, entry := range []string{"Bash(git rm *)", "Bash(git -C * rm *)", "Bash(rm *)"} {
			t.Run(preset+"/"+entry, func(t *testing.T) {
				list, err := ImplementerPreset(preset, "")
				if err != nil || !slices.Contains(list, entry) {
					t.Fatalf("%s missing %s: %v", preset, entry, err)
				}
			})
		}
	}
}

func TestImplementerPresetUnknownName(t *testing.T) {
	got, err := ImplementerPreset("rust", "")
	if err == nil || err.Error() != `unknown preset "rust" (presets: go, python)` || got != nil {
		t.Fatalf("got %q, %v", got, err)
	}
	if !reflect.DeepEqual(PresetNames(), []string{"go", "python"}) {
		t.Fatalf("PresetNames: %q", PresetNames())
	}
}
