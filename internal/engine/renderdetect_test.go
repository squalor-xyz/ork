// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderTestsIn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cli, tui bool
	}{
		{"cli", true, false}, {"tui", false, true}, {"both", true, true}, {"empty", false, false}, {"missing internal", false, false}, {"linked directory", false, false}, {"ignored files", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write := func(path, body string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.cli {
				write(filepath.Join(root, "internal/cli/x_test.go"), "func TestRenderCLIViews(")
			}
			if tc.tui {
				write(filepath.Join(root, "internal/tui/y_test.go"), "func TestRenderTUIViews(")
			}
			if tc.name == "empty" {
				if err := os.MkdirAll(filepath.Join(root, "internal/cli"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "linked directory" {
				target := filepath.Join(root, "target")
				write(filepath.Join(target, "cli/x_test.go"), "func TestRenderCLIViews(")
				write(filepath.Join(target, "tui/y_test.go"), "func TestRenderTUIViews(")
				if err := os.Symlink(target, filepath.Join(root, "internal")); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "ignored files" {
				for _, pkg := range []string{"cli", "tui"} {
					body := "func TestRenderCLIViews( func TestRenderTUIViews("
					write(filepath.Join(root, "internal", pkg, "x.go"), body)
					write(filepath.Join(root, "internal", pkg, "x_test.go"), "func TestOther(")
					if err := os.Symlink("x.go", filepath.Join(root, "internal", pkg, "link_test.go")); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(root, "internal", pkg, "dir_test.go"), 0755); err != nil {
						t.Fatal(err)
					}
				}
			}
			cli, tui := renderTestsIn(root)
			if cli != tc.cli || tui != tc.tui {
				t.Errorf("renderTestsIn = (%t,%t), want (%t,%t)", cli, tui, tc.cli, tc.tui)
			}
		})
	}
}
