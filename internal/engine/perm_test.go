// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// allowlistMatches reports whether a Bash(...) entry matches the command, with
// `*` as a wildcard for any text (Claude Code's prefix-pattern rule).
func allowlistMatches(entry, command string) bool {
	pat, ok := strings.CutSuffix(strings.TrimPrefix(entry, "Bash("), ")")
	if !ok || !strings.HasPrefix(entry, "Bash(") {
		return false
	}
	parts := strings.Split(pat, "*")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$").MatchString(command)
}

// The slicer entries must be plain read-only: no form that writes tracking
// may match (S505 fix round 2, adj 519).
func TestReadOnlyAllowlistMatchesNoTrackingWrite(t *testing.T) {
	// The fixed list of known tracking-write commands (S509, adj 543): slicer
	// note, done, set, ai relay --init-roles and changelog add, each bare and
	// with a trailing --root, plus the --help look-alikes of S505.
	writes := []string{
		`slicer note S505 --text "x"`,
		`slicer note S505 --text "x" --root /w`,
		`slicer note S505 --text " --help"`,
		`slicer note S505 --text "x --help"`,
		`slicer done S505 --note "x"`,
		`slicer done S505 --note "x" --render --check`,
		`slicer done S505 --note "x" --root /w`,
		`slicer set S505 --field status=done`,
		`slicer set S505 --field status=done --root /w`,
		"slicer ai relay --init-roles",
		"slicer ai relay --init-roles --force",
		"slicer ai relay --init-roles --force --root /w",
		`slicer changelog add S505 --text "x"`,
		`slicer changelog add S505 --text "x" --replace`,
		`slicer changelog add S505 --text "x" --root /w`,
	}
	lists := map[string][]string{
		"local":   LocalReadOnlyAllowlist(),
		"cloudui": CloudUIAllowlist(),
	}
	for name, list := range lists {
		for _, command := range writes {
			t.Run(name+"/"+command, func(t *testing.T) {
				for _, entry := range list {
					if allowlistMatches(entry, command) {
						t.Errorf("entry %s matches %q", entry, command)
					}
				}
			})
		}
		for _, command := range []string{"slicer ai relay", "slicer ai relay --help", "slicer show --help"} {
			t.Run(name+"/allows "+command, func(t *testing.T) {
				for _, entry := range list {
					if allowlistMatches(entry, command) {
						return
					}
				}
				t.Errorf("no entry matches %q", command)
			})
		}
	}
}

// The model must match as the real rule does: a non-Bash entry and a malformed
// entry match no command, and `*` spans any text.
func TestAllowlistMatchesModel(t *testing.T) {
	for _, tc := range []struct {
		entry, command string
		want           bool
	}{
		{"Bash(slicer note*)", `slicer note S1 --text "x"`, true},
		{"Bash(slicer * --help)", "slicer note S1 --help", true},
		{"Bash(slicer * --help)", `slicer note S1 --text "x --help"`, false},
		{"Bash(slicer ai relay)", "slicer ai relay --init-roles", false},
		{"Skill", "Skill", false},
		{"Bash(slicer note*", "slicer note S1", false},
	} {
		if got := allowlistMatches(tc.entry, tc.command); got != tc.want {
			t.Errorf("allowlistMatches(%q, %q) = %v, want %v", tc.entry, tc.command, got, tc.want)
		}
	}
}

func TestReadOnlyAllowlistBatch(t *testing.T) {
	list := LocalReadOnlyAllowlist()
	for _, entry := range []string{"Bash(slicer * --help)", "Bash(slicer ai relay)", "Bash(ls*)", "Bash(git ls-files*)", "Bash(git -C * ls-files*)"} {
		t.Run(entry, func(t *testing.T) {
			if !slices.Contains(list, entry) {
				t.Errorf("read-only allowlist missing %s", entry)
			}
		})
	}
	for _, forbidden := range []string{"rg", "$", ";", "&&", "="} {
		t.Run("excludes_"+forbidden, func(t *testing.T) {
			for _, entry := range list {
				if strings.Contains(entry, forbidden) {
					t.Errorf("read-only allowlist contains forbidden %q in %s", forbidden, entry)
				}
			}
		})
	}
}

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
		"Bash(ork show*)",
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
		if !slices.Contains(got, "Bash(ork show*)") {
			t.Error("go preset missing Bash(ork show*)")
		}
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
		"Bash(ork show*)",
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

func TestImplementerPresetsAllowOrkShow(t *testing.T) {
	for _, preset := range []string{"go", "python"} {
		t.Run(preset, func(t *testing.T) {
			list, err := ImplementerPreset(preset, "")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(list, "Bash(ork show*)") {
				t.Error("preset missing Bash(ork show*)")
			}
			for _, entry := range list {
				if strings.HasPrefix(entry, "Bash(ork") && entry != "Bash(ork show*)" {
					t.Errorf("unexpected ork permission: %s", entry)
				}
			}
		})
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
