// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/brand"
)

// The install command in the README puts ork in GOBIN, a user directory, with
// no sudo. The test runs that command with GOBIN set to a temporary directory
// and runs the binary it writes.
func TestTheDocumentedInstallPutsOrkInGOBIN(t *testing.T) {
	const install = "go install ./cmd/ork"
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), install) {
		t.Fatalf("README.md does not document %q", install)
	}

	bin := t.TempDir()
	argv := strings.Fields(install)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "GOBIN="+bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", install, err, out)
	}
	out, err := exec.Command(filepath.Join(bin, "ork"), "--version").Output()
	if err != nil || string(out) != strings.Join(brand.Mark(false, asciiLocale(os.Getenv)), "\n")+"\nork dev\n" {
		t.Fatalf("installed ork --version: %v, stdout %q", err, out)
	}
}
