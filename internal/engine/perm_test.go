// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
)

func TestDefaultImplementerAllowlistIsExactlyTheDecidedOne(t *testing.T) {
	want := []string{
		"Read", "Grep", "Glob", "Edit", "Write",
		"Bash(slicer *)", "Bash(git status*)", "Bash(git diff*)", "Bash(git log*)",
		"Bash(go test ./...*)",
	}
	if got := DefaultImplementerAllowlist("go test ./..."); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if got := DefaultImplementerAllowlist(""); !reflect.DeepEqual(got, want[:9]) {
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
