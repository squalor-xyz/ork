// SPDX-License-Identifier: GPL-3.0-or-later

package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// stateExample returns the projects.json example from docs/state.md, so the
// test follows the document.
func stateExample(t *testing.T) []byte {
	t.Helper()
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "state.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	_, rest, ok := strings.Cut(text, "## projects.json")
	if !ok {
		t.Fatal("docs/state.md has no projects.json section")
	}
	_, rest, ok = strings.Cut(rest, "```json\n")
	if !ok {
		t.Fatal("projects.json section has no json block")
	}
	block, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatal("projects.json json block is not closed")
	}
	return []byte(block)
}

func asAny(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("not json: %v\n%s", err, data)
	}
	return v
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestRoundTripKeepsEveryField(t *testing.T) {
	example := stateExample(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in", "projects.json")
	out := filepath.Join(dir, "out", "projects.json")
	writeFile(t, in, example)

	f, err := Load(in)
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != 2 || f.GlobalCap != 4 || f.DaemonIdleMinutes != 10 || len(f.Projects) != 1 {
		t.Fatalf("loaded: %+v", f)
	}
	p := f.Projects[0]
	if p.Name != "DataBall" || p.Adj || p.Cap != 2 || p.CheckMinutes != 10 ||
		p.CheckCommand != "dotnet test" || p.WorktreePattern != ".worktrees/{id}" ||
		p.BranchPattern != "{id}" || len(p.EnvironmentLines) != 2 ||
		p.Budgets.Minutes != 120 || p.Budgets.Tokens != 2000000 {
		t.Fatalf("project fields: %+v", p)
	}
	if got := p.Permissions["reviewer"]; got.Preset != "read-only" {
		t.Fatalf("reviewer permission: %+v", got)
	}
	if got := p.Permissions["implementer"]; len(got.Allow) != 4 || got.Allow[2] != "Bash(dotnet test*)" {
		t.Fatalf("implementer permission: %+v", got)
	}
	if p.VendorEnv["codex"]["VENDOR_CODEX_NETWORK"] != "1" {
		t.Fatalf("vendor_env: %+v", p.VendorEnv)
	}
	if p.UIDoc != "docs/ui.md" {
		t.Fatalf("ui_doc: the docs example sets docs/ui.md, the project has %q", p.UIDoc)
	}
	if !p.Push {
		t.Fatalf("push: the docs example sets it, the project has %v", p.Push)
	}

	if err := Save(out, f); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(asAny(t, saved), asAny(t, example)) {
		t.Fatalf("saved file differs from the docs/state.md example\nsaved:\n%s", saved)
	}
	if !bytes.HasSuffix(saved, []byte("\n")) {
		t.Fatal("saved file does not end in a newline")
	}
}

// The push setting is off unless the file turns it on, and ork writes it
// only for a project that has it on (S119, adj 162).
func TestPushIsOffByDefaultAndOnlyWrittenWhenOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	f := &File{Version: Version, Projects: []Project{{Name: "a", Path: "/p/a"}, {Name: "b", Path: "/p/b", Push: true}}}
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	if strings.Count(string(saved), `"push"`) != 1 || !strings.Contains(string(saved), `"push": true`) {
		t.Fatalf("push is written for a project that has it off, or not for one that has it on:\n%s", saved)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Projects[0].Push || !back.Projects[1].Push {
		t.Fatalf("loaded push: %v %v", back.Projects[0].Push, back.Projects[1].Push)
	}
}

func TestSaveKeepsAdjFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	f := &File{Version: Version, Projects: []Project{{Name: "a", Path: "/p/a", Adj: false}}}
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	if !strings.Contains(string(saved), `"adj": false`) {
		t.Fatalf("adj false is not written:\n%s", saved)
	}
}

func TestUnknownVersionRefused(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		got              int
	}{
		{"newer", `{"version": 3, "projects": [], "future_field": true}`, "version 3", 3},
		{"missing", `{"projects": []}`, "version 0", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "projects.json")
			writeFile(t, path, []byte(tc.body))
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load accepted an unknown version")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error does not name the version: %v", err)
			}
			var ve *VersionError
			if !errors.As(err, &ve) || ve.Got != tc.got {
				t.Fatalf("not a *VersionError with Got=%d: %v", tc.got, err)
			}
		})
	}
}

func TestVersion1FileLoadsAndSavesAsVersion2(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "projects.json")
	writeFile(t, in, []byte(`{"version": 1, "global_cap": 3, "projects": [{"name": "a", "path": "/p/a", "adj": true, "cap": 2}]}`))
	f, err := Load(in)
	if err != nil {
		t.Fatalf("a version 1 file must load: %v", err)
	}
	if f.Version != 2 || f.GlobalCap != 3 || f.DaemonIdleMinutes != 0 || len(f.Projects) != 1 || f.Projects[0].Cap != 2 {
		t.Fatalf("loaded: %+v", f)
	}
	if err := Save(in, f); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(in)
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(saved, &head); err != nil || head.Version != 2 {
		t.Fatalf("saved version %d, %v:\n%s", head.Version, err, saved)
	}
	if strings.Contains(string(saved), "daemon_idle_minutes") {
		t.Fatalf("an unset daemon_idle_minutes is written:\n%s", saved)
	}
}

func TestVersion1FileRefusesVersion2Field(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	writeFile(t, path, []byte(`{"version": 1, "daemon_idle_minutes": 5, "projects": []}`))
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "daemon_idle_minutes") {
		t.Fatalf("a version 1 file with a version 2 key must fail naming it: %v", err)
	}
}

func TestVersion2UnknownKeyRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	writeFile(t, path, []byte(`{"version": 2, "daemon_idle_minute": 5, "projects": []}`))
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "daemon_idle_minute") {
		t.Fatalf("an unknown key must fail naming it: %v", err)
	}
}

func TestDaemonIdle(t *testing.T) {
	for _, tc := range []struct {
		minutes int
		want    time.Duration
	}{
		{0, 10 * time.Minute}, // 0 equals absent (registry.zero-values)
		{1, time.Minute},
		{25, 25 * time.Minute},
	} {
		if got := (&File{DaemonIdleMinutes: tc.minutes}).DaemonIdle(); got != tc.want {
			t.Errorf("DaemonIdle for %d minutes = %v, want %v", tc.minutes, got, tc.want)
		}
	}
	f := &File{Version: Version, DaemonIdleMinutes: -1, Projects: []Project{}}
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "daemon_idle_minutes") {
		t.Fatalf("a negative daemon_idle_minutes must fail validation: %v", err)
	}
}

func TestSaveRefusesUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	err := Save(path, &File{Version: 3})
	if err == nil || !strings.Contains(err.Error(), "version 3") {
		t.Fatalf("Save of version 3: %v", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("Save wrote a file with an unknown version")
	}
}

func TestLoadMissingRegistryIsEmptyAndWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	f, err := Load(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != Version || f.Projects == nil || len(f.Projects) != 0 {
		t.Fatalf("registry: %+v", f)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("directory created: %v", err)
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"empty name", `{"version":1,"projects":[{"name":"","path":"/p"}]}`, "name"},
		{"duplicate name", `{"version":1,"projects":[{"name":"a","path":"/p"},{"name":"a","path":"/q"}]}`, "duplicate"},
		{"relative path", `{"version":1,"projects":[{"name":"a","path":"p"}]}`, "absolute"},
		{"negative cap", `{"version":1,"global_cap":-1,"projects":[]}`, "global_cap"},
		{"unknown key", `{"version":1,"projects":[{"name":"a","path":"/p","bogus":1}]}`, "bogus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "projects.json")
			writeFile(t, path, []byte(tc.body))
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestSaveWritesTempInSameDirAndRenames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ork", "projects.json")
	var tempName string
	orig := writeTemp
	t.Cleanup(func() { writeTemp = orig })
	writeTemp = func(f *os.File, data []byte) error {
		tempName = f.Name()
		if filepath.Dir(tempName) != filepath.Dir(path) {
			t.Errorf("temp file %s is not in %s", tempName, filepath.Dir(path))
		}
		if tempName == path {
			t.Errorf("temp file is the target")
		}
		if _, err := os.Stat(path); err == nil {
			t.Errorf("target exists before the rename")
		}
		return orig(f, data)
	}
	f := &File{Version: Version, Projects: []Project{{Name: "a", Path: "/p/a"}}}
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	if tempName == "" {
		t.Fatal("no temp file was written")
	}
	if _, err := os.Stat(tempName); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp file survived the rename: %v", err)
	}
	if names := dirNames(t, filepath.Dir(path)); !reflect.DeepEqual(names, []string{"projects.json"}) {
		t.Fatalf("directory holds %v", names)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
}

func TestSaveFailureKeepsOldFileAndLeavesNoTemp(t *testing.T) {
	old := []byte(`{"version": 1, "projects": []}` + "\n")
	f := &File{Version: Version, Projects: []Project{{Name: "a", Path: "/p/a"}}}

	t.Run("write fails", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "projects.json")
		writeFile(t, path, old)
		orig := writeTemp
		t.Cleanup(func() { writeTemp = orig })
		writeTemp = func(f *os.File, data []byte) error {
			f.Write(data[:len(data)/2]) // a partial write, then the failure
			return errors.New("disk full")
		}
		err := Save(path, f)
		if err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("Save error: %v", err)
		}
		got, _ := os.ReadFile(path)
		if !bytes.Equal(got, old) {
			t.Fatalf("old file changed: %q", got)
		}
		if names := dirNames(t, filepath.Dir(path)); !reflect.DeepEqual(names, []string{"projects.json"}) {
			t.Fatalf("directory holds %v", names)
		}
	})

	t.Run("rename fails", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "projects.json")
		if err := os.Mkdir(path, 0o755); err != nil { // a rename onto a directory fails
			t.Fatal(err)
		}
		if err := Save(path, f); err == nil {
			t.Fatal("Save succeeded over a directory")
		}
		if names := dirNames(t, dir); !reflect.DeepEqual(names, []string{"projects.json"}) {
			t.Fatalf("directory holds %v", names)
		}
	})
}

func TestDefaultPaths(t *testing.T) {
	t.Run("XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		got, err := DefaultPath()
		if err != nil || got != "/xdg/ork/projects.json" {
			t.Fatalf("DefaultPath = %q, %v", got, err)
		}
	})
	t.Run("home fallback", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "/home/x")
		got, err := DefaultPath()
		if err != nil || got != "/home/x/.config/ork/projects.json" {
			t.Fatalf("DefaultPath = %q, %v", got, err)
		}
	})
}

func TestSaveStillReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	writeFile(t, path, []byte(`{"version": 1, "projects": []}`))
	f := &File{Version: Version, Projects: []Project{{Name: "a", Path: "/p/a"}}}
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || len(got.Projects) != 1 {
		t.Fatalf("Load after Save: %+v, %v", got, err)
	}
}

func TestValidateUIDocAcceptsOnlyPathsInsideTheProject(t *testing.T) {
	for _, ok := range []string{"", "docs/ui.md", "ui.md", "a/b/c.md", "docs/../ui.md"} {
		if err := ValidateUIDoc(ok); err != nil {
			t.Errorf("ValidateUIDoc(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"/etc/ui.md", "../ui.md", "docs/../../ui.md", "..", ".", "./", "docs/..", "a\x00b"} {
		if err := ValidateUIDoc(bad); err == nil {
			t.Errorf("ValidateUIDoc(%q) = nil, want an error", bad)
		}
	}
}

func TestValidateUIDocNamesTheProblem(t *testing.T) {
	if err := ValidateUIDoc("/etc/ui.md"); err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Errorf("absolute path: %v", err)
	}
	if err := ValidateUIDoc("../ui.md"); err == nil || !strings.Contains(err.Error(), "inside the project") {
		t.Errorf("escaping path: %v", err)
	}
}

func TestValidatePatternNeedsAnIDPlaceholder(t *testing.T) {
	for _, ok := range []string{"", "{id}", ".worktrees/{id}", "{id_lower}", "/abs/{id_lower}/x", "{id}-{id_lower}"} {
		if err := ValidatePattern("worktree_pattern", ok); err != nil {
			t.Errorf("ValidatePattern(%q) = %v, want nil", ok, err)
		}
	}
	err := ValidatePattern("branch_pattern", "feature")
	if err == nil || !strings.Contains(err.Error(), "branch_pattern") || !strings.Contains(err.Error(), "{id}") {
		t.Errorf("ValidatePattern without a placeholder = %v", err)
	}
	if err := ValidatePattern("branch_pattern", "{ID}"); err == nil {
		t.Error("ValidatePattern({ID}) = nil, want an error: placeholders are case sensitive")
	}
}
