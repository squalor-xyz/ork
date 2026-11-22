// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Run go test ./internal/cli -update to rewrite the CLI text fixtures.
var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "golden", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/cli -update)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (run go test ./internal/cli -update and review the diff)\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

func prepareDoctorGolden(t *testing.T, env *upEnv) {
	t.Helper()
	// Optional local launchers must not come from the host PATH. These vendor
	// fakes use only shell builtins so they also work with the isolated PATH.
	dir := t.TempDir()
	for _, v := range []struct {
		name, version string
		bin           *string
	}{
		{"claude", "2.1.303 (Claude Code)", &env.app.Tools.Claude},
		{"codex", "codex-cli 0.163.0", &env.app.Tools.Codex},
		{"grok", "grok 1.0.62 (fake)", &env.app.Tools.Grok},
	} {
		*v.bin = filepath.Join(dir, v.name)
		if err := os.WriteFile(*v.bin, []byte("#!/bin/sh\nprintf '%s\\n' '"+v.version+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	// Pin the mark locale and omit the host owner.
	env.app.Getenv = func(key string) string {
		if key == "LANG" {
			return "en_US.UTF-8"
		}
		return ""
	}
}

func normalizeDoctor(t *testing.T, env *upEnv, got string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	paths := []struct{ path, word string }{
		{env.slicerD, "SLICER_CHECKOUT"},
		{env.adjPath, "ADJ_BIN"},
		{bin, "ORK_BIN"},
	}
	// Replace nested paths before their parents; never replace an empty path.
	slices.SortFunc(paths, func(a, b struct{ path, word string }) int {
		return len(b.path) - len(a.path)
	})
	for _, p := range paths {
		if p.path != "" {
			got = strings.ReplaceAll(got, p.path, p.word)
		}
	}
	// ADJ_CHECKOUT and ORK_CHECKOUT are already placeholders in doctor output.
	return got
}
