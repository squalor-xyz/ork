// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// Run from a copied test executable, then atomically replace that executable
// with a build-only fixture. The daemon starter must execute the new file.
func TestRebuildReplacedExecutable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux deleted-executable regression")
	}
	if os.Getenv("ORK_REBUILD_REPLACEMENT_HELPER") == "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		replacement := exe + ".next"
		script := []byte("#!/bin/sh\nprintf '%s' \"$*\" > \"$ORK_REBUILD_REPLACEMENT_MARKER\"\n")
		if err := os.WriteFile(replacement, script, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, exe); err != nil {
			t.Fatal(err)
		}
		if err := startDaemon(context.Background()); err != nil {
			t.Fatalf("replacement daemon start: %v", err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			b, err := os.ReadFile(os.Getenv("ORK_REBUILD_REPLACEMENT_MARKER"))
			if err == nil && string(b) == "daemon run" {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("replacement output: %q %v", b, err)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(dir, "ork")
	if err := os.WriteFile(copied, data, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, copied, "-test.run=^TestRebuildReplacedExecutable$")
	cmd.Env = append(os.Environ(), "ORK_REBUILD_REPLACEMENT_HELPER=1", "ORK_REBUILD_REPLACEMENT_MARKER="+filepath.Join(dir, "started"), "XDG_STATE_HOME="+filepath.Join(dir, "state"), "XDG_RUNTIME_DIR="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PASS") {
		t.Fatalf("replaced executable: %v\n%s", err, out)
	}
}

func TestPrepareDaemonRestartPath(t *testing.T) {
	for _, kind := range []string{"plain", "deleted", "missing"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "new-ork")
			marker := filepath.Join(dir, "started")
			if kind != "missing" {
				script := "#!/bin/sh\nprintf '%s' \"$*\" > " + "\"" + marker + "\"\n"
				if err := os.WriteFile(exe, []byte(script), 0755); err != nil {
					t.Fatal(err)
				}
			}
			path := exe
			if kind == "deleted" {
				path += " (deleted)"
			}
			start, err := prepareDaemonRestartExe(context.Background(), path, daemon.PathsIn(filepath.Join(dir, "state")))
			if kind == "missing" {
				if err == nil || start != nil || !strings.Contains(err.Error(), exe) {
					t.Fatalf("missing executable was prepared: start=%v err=%v", start != nil, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := start(context.Background()); err != nil {
				t.Fatalf("prepared executable start: %v", err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				b, err := os.ReadFile(marker)
				if err == nil && string(b) == "daemon run" {
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("prepared executable output: %q %v", b, err)
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}
}
