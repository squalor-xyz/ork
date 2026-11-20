// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var renders = flag.String("renders", "", "write plain-text CLI views to this directory")

// TestRenderCLIViews runs the real command printers with existing CLI fixtures.
// With no flag it does no work and writes nothing.
func TestRenderCLIViews(t *testing.T) {
	if *renders == "" {
		return
	}
	status := statusRebuildFixture()
	show := showDouble(pausedShow(), map[string]string{"reviewer-3-attempt-3.md": "PENDING REPORT BODY\nline two"})
	for _, view := range []struct {
		name string
		app  func() *App
		args []string
	}{
		{"status", func() *App { return &status }, []string{"status"}},
		{"status-summary", func() *App {
			return summaryApp(t, summaryFixture(), `{"next":{"id":"S7","title":"First"},"blocked":[{"id":"S8","waiting_on":["S6","S5"]}]}`, `[{"id":"S2","title":"Second"},{"id":"S7","title":"First"}]`)
		}, []string{"status", "--summary"}},
		{"status-summary-long", func() *App {
			return summaryApp(t, summaryLongFixture(), `{"next":{"id":"S7","title":"Review the complete summary display and its detailed information before starting the next item in the current project"}}`, `[]`)
		}, []string{"status", "--summary"}},
		{"show", func() *App { return &App{Call: show.call} }, []string{"show", "r1"}},
		{"help", func() *App { return &App{} }, []string{"help"}},
	} {
		code, out, errs := run(t, view.app(), view.args...)
		if code != ExitOK || errs != "" {
			t.Fatalf("%s: exit %d, stderr %q", view.name, code, errs)
		}
		if err := os.WriteFile(filepath.Join(*renders, view.name+".txt"), []byte(out), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderCLIViewsWritesFiles(t *testing.T) {
	if flag.Lookup("renders") == nil {
		t.Fatal("missing -renders test flag")
	}
	for _, enabled := range []bool{false, true} {
		name := "unset"
		if enabled {
			name = "set"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			args := []string{"-test.run=^TestRenderCLIViews$"}
			if enabled {
				args = append(args, "-renders", dir)
			}
			cmd := exec.CommandContext(ctx, os.Args[0], args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("render command: %v\n%s", err, out)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			want := []string{}
			if enabled {
				want = []string{"help.txt", "show.txt", "status-summary-long.txt", "status-summary.txt", "status.txt"}
			}
			if !reflect.DeepEqual(names, want) {
				t.Fatalf("render files %q, want %q", names, want)
			}
			for _, name := range names {
				text, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if strings.TrimSpace(string(text)) == "" {
					t.Errorf("%s is empty", name)
				}
				if strings.ContainsRune(string(text), '\x1b') {
					t.Errorf("%s contains ANSI escapes", name)
				}
				if name == "status-summary.txt" && !strings.Contains(string(text), "  S7 First\n") {
					t.Errorf("short summary used another fixture: %s", text)
				}
				if name == "status-summary-long.txt" && !strings.Contains(string(text), "  S7 Review the complete summary") {
					t.Errorf("long summary used another fixture: %s", text)
				}
			}
		})
	}
}
