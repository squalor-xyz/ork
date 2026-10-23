// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
)

const emptyGuide = "no projects: run ork projects add PATH (or run ork to add them in the TUI)"

func TestStatusWithNoProjectsGivesTheOneLineGuide(t *testing.T) {
	w := newWorld(t, stubSource{})
	code, out, errOut := run(t, w.app, "status")
	if code != 0 || out != emptyGuide+"\n" || errOut != "" {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	for _, args := range [][]string{{"status", "--project", "missing"}, {"run", "missing", "S1", "--yes", "--well-specified", "yes", "--split", "no"}, {"wave", "suggest", "missing"}, {"wave", "start", "missing", "S1", "--yes", "--well-specified", "yes", "--split", "no"}} {
		code, out, errOut = run(t, w.app, args...)
		if code != 1 || out != "" || errOut != "ork: "+emptyGuide+"\n" {
			t.Errorf("%v: %d %q %q", args, code, out, errOut)
		}
		code, out, errOut = run(t, w.app, append(args, "--json")...)
		if code != 1 || !strings.Contains(out, emptyGuide) || errOut != "" {
			t.Errorf("json %v: %d %q %q", args, code, out, errOut)
		}
	}
	code, out, errOut = run(t, w.app, "status", "--json")
	if code != 0 || !strings.Contains(out, `"projects": []`) || errOut != "" {
		t.Fatalf("json status %d %q %q", code, out, errOut)
	}
}
func TestProjectsAddWithoutPathOffersTheCurrentDirectory(t *testing.T) {
	for _, answer := range []string{"y\n", "YES\n", "\n", "", "no\n", "invalid\n"} {
		t.Run(strings.ReplaceAll(answer, "\n", "enter"), func(t *testing.T) {
			w := newWorld(t, stubSource{})
			dir := filepath.Join(w.dir, "mine")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			w.app.Getwd = func() (string, error) { return dir, nil }
			w.app.Input = strings.NewReader(answer)
			code, out, errOut := run(t, w.app, "projects", "add")
			if !strings.HasPrefix(out, "Add "+dir+" as project mine? [y/N]: ") {
				t.Fatal(out)
			}
			f, err := registry.Load(w.regPath)
			if err != nil {
				t.Fatal(err)
			}
			if yesNo(strings.TrimSpace(answer)) == "yes" {
				if code != 0 || len(f.Projects) != 1 || f.Projects[0].Path != dir {
					t.Fatalf("%d %q %q %+v", code, out, errOut, f)
				}
			} else if code != 1 || len(f.Projects) != 0 || errOut != "ork: cancelled\n" {
				t.Fatalf("%d %q %q %+v", code, out, errOut, f)
			}
		})
	}
	a := noDaemon(t)
	a.Getwd = func() (string, error) { t.Fatal("json requested cwd"); return "", nil }
	code, out, _ := run(t, a, "projects", "add", "--json")
	if code != 2 || strings.Contains(out, "[y/N]") {
		t.Fatalf("%d %q", code, out)
	}
}
func TestFirstAddCreatesTheRegistry(t *testing.T) {
	w := newWorld(t, stubSource{})
	w.regPath = filepath.Join(w.dir, "missing", "config", "projects.json")
	dir := filepath.Join(w.dir, "mine")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, w.app, "status")
	if code != 0 || out != emptyGuide+"\n" || errOut != "" {
		t.Fatalf("empty status: %d %q %q", code, out, errOut)
	}
	for _, path := range []string{w.regPath, filepath.Dir(w.regPath)} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("status created registry path %s: %v", path, err)
		}
	}
	code, out, errOut = run(t, w.app, "projects", "add", dir)
	f, err := registry.Load(w.regPath)
	if code != 0 || err != nil || len(f.Projects) != 1 || f.Projects[0].Path != dir || f.Projects[0].Name != "mine" {
		t.Fatalf("%d %q %q %+v %v", code, out, errOut, f, err)
	}
	if _, err := os.Stat(w.regPath); err != nil {
		t.Fatal(err)
	}
}
