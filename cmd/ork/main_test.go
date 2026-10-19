// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/tui"
)

func TestVersionPrintsAndStartsNoTUI(t *testing.T) {
	for _, flag := range []string{"--version", "-version"} {
		var out, errOut bytes.Buffer
		called := false
		code := run([]string{flag}, &out, &errOut, func(tui.Config) error { called = true; return nil })
		if code != 0 || !strings.HasPrefix(out.String(), "ork ") || strings.Count(out.String(), "\n") != 1 {
			t.Errorf("%s: code %d, stdout %q, stderr %q", flag, code, out.String(), errOut.String())
		}
		if called {
			t.Errorf("%s started the TUI", flag)
		}
	}
}

func TestUnknownFlagOrArgumentIsAUsageError(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"extra"}} {
		var out, errOut bytes.Buffer
		called := false
		code := run(args, &out, &errOut, func(tui.Config) error { called = true; return nil })
		if code != 2 || called || errOut.Len() == 0 {
			t.Errorf("%v: code %d, called %v, stderr %q", args, code, called, errOut.String())
		}
	}
}

// seedEnv points the registry and the seed root at a temporary tree holding
// the named project directories.
func seedEnv(t *testing.T, dirs ...string) (registryPath string) {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("ORK_SEED_ROOT", root)
	return filepath.Join(cfg, "ork", "projects.json")
}

func TestStartSeedsAMissingRegistryAndSaysSo(t *testing.T) {
	path := seedEnv(t, "slicer", "agents")
	var out, errOut bytes.Buffer
	var got tui.Config
	code := run(nil, &out, &errOut, func(c tui.Config) error { got = c; return nil })
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	if len(got.Projects) != 2 || got.Projects[0].Name != "slicer" || got.Projects[1].Name != "agents" {
		t.Fatalf("projects: %+v", got.Projects)
	}
	if got.Source == nil {
		t.Error("no Source")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("registry not written: %v", err)
	}
	if msg := errOut.String(); !strings.Contains(msg, path) || !strings.Contains(msg, "2 project") {
		t.Errorf("seeding not announced: %q", msg)
	}
}

func TestStartReadsAnExistingRegistryWithoutSeeding(t *testing.T) {
	path := seedEnv(t, "slicer")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"version":1,"projects":[{"name":"mine","path":"/work/mine","adj":false}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	var got tui.Config
	code := run(nil, &out, &errOut, func(c tui.Config) error { got = c; return nil })
	if code != 0 || len(got.Projects) != 1 || got.Projects[0].Name != "mine" {
		t.Fatalf("code %d, projects %+v, stderr %q", code, got.Projects, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr: %q", errOut.String())
	}
}

func TestStartWithNoProjectsFailsWithoutTheTUI(t *testing.T) {
	seedEnv(t) // no project directories exist, so the seed holds nothing
	var out, errOut bytes.Buffer
	called := false
	code := run(nil, &out, &errOut, func(tui.Config) error { called = true; return nil })
	if code != 1 || called || !strings.Contains(errOut.String(), "no projects") {
		t.Fatalf("code %d, called %v, stderr %q", code, called, errOut.String())
	}
}

func TestABrokenRegistryFailsWithItsError(t *testing.T) {
	path := seedEnv(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":9,"projects":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run(nil, &out, &errOut, func(tui.Config) error { t.Fatal("TUI started"); return nil })
	if code != 1 || !strings.Contains(errOut.String(), "unknown registry version 9") {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
}

func TestATUIErrorExitsNonZero(t *testing.T) {
	seedEnv(t, "slicer")
	var out, errOut bytes.Buffer
	code := run(nil, &out, &errOut, func(tui.Config) error { return errors.New("no tty") })
	if code != 1 || !strings.Contains(errOut.String(), "no tty") {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
}
