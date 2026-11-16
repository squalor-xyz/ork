// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
)

func notifyProject(t *testing.T) (registry.Project, string) {
	t.Helper()
	dir := t.TempDir()
	command := filepath.Join(dir, "notify")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"name": "demo", "path": dir, "notify_command": command})
	if err != nil {
		t.Fatal(err)
	}
	var p registry.Project
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p, command
}

func TestNotifyDoctorMissingExecutable(t *testing.T) {
	p, command := notifyProject(t)
	if err := os.Remove(command); err != nil {
		t.Fatal(err)
	}
	got := checkProject(context.Background(), p)
	if !strings.Contains(strings.Join(got.Findings, "\n"), "notify_command: executable not found") {
		t.Fatalf("doctor did not warn about missing notify executable: %v", got.Findings)
	}
}

func TestNotifySetStoredCommand(t *testing.T) {
	p, command := notifyProject(t)
	w := newWorld(t, stubSource{}, registry.Project{Name: p.Name, Path: p.Path})
	code, out, stderr := run(t, w.app, "projects", "set", "demo", "--notify", command)
	if code != 0 {
		t.Fatalf("set notify: exit %d, %s %s", code, out, stderr)
	}
	raw, err := os.ReadFile(w.regPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Projects []struct {
			Notify string `json:"notify_command"`
		}
	}
	mustJSON(t, string(raw), &saved)
	if saved.Projects[0].Notify != command {
		t.Fatalf("stored notify_command = %q, want %q", saved.Projects[0].Notify, command)
	}
}

func TestNotifyClearStoredCommand(t *testing.T) {
	p, _ := notifyProject(t)
	w := newWorld(t, stubSource{}, p)
	code, out, stderr := run(t, w.app, "projects", "set", "demo", "--notify", "")
	if code != 0 {
		t.Fatalf("clear notify: exit %d, %s %s", code, out, stderr)
	}
	raw, err := os.ReadFile(w.regPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "notify_command") {
		t.Fatalf("cleared notify_command remains in registry: %s", raw)
	}
}

func TestNotifyProjectsDisplay(t *testing.T) {
	p, command := notifyProject(t)
	w := newWorld(t, stubSource{}, p)
	code, out, stderr := run(t, w.app, "projects", "list")
	if code != 0 {
		t.Fatalf("projects list: exit %d, %s %s", code, out, stderr)
	}
	if !strings.Contains(out, "notify_command="+command) {
		t.Fatalf("projects omits notify_command: %q", out)
	}
}

func TestNotifyDoctorDisplay(t *testing.T) {
	p, command := notifyProject(t)
	app, _ := doctorProjects(t, p)
	_, out, _ := run(t, app, "doctor")
	if !strings.Contains(out, "notify_command="+command) {
		t.Fatalf("doctor omits notify_command: %q", out)
	}
}

func TestNotifyHelp(t *testing.T) {
	_, out, _ := run(t, noDaemon(t), "help")
	if !strings.Contains(out, "[--notify CMD]") {
		t.Fatal("help omits [--notify CMD]")
	}
}

func TestNotifyDoctorFindingClean(t *testing.T) {
	p, _ := notifyProject(t)
	p.NotifyCommand = filepath.Join(p.Path, "missing\x1b[2J\nnext\x07")
	var findings []string
	for _, finding := range checkProject(context.Background(), p).Findings {
		if strings.HasPrefix(finding, "notify_command:") {
			findings = append(findings, finding)
		}
	}
	want := "notify_command: executable not found: " + filepath.Join(p.Path, "missing next")
	if got := strings.Join(findings, "\n"); got != want {
		t.Fatalf("unsafe notify finding: %q, want %q", got, want)
	}
}

func TestNotifyProjectsColumns(t *testing.T) {
	var out bytes.Buffer
	printProjects(&out, []registry.Project{
		{Name: "a", Path: "/p", Adj: true, NotifyCommand: "first"},
		{Name: "b", Path: "/p", NotifyCommand: "second"},
		{Name: "c", Path: "/p"},
	})
	want := "a  /p  adj  notify_command=first\nb  /p  -    notify_command=second\nc  /p  -\n"
	if got := out.String(); got != want {
		t.Fatalf("inconsistent project columns: %q, want %q", got, want)
	}
}

func TestNotifyProjectsSingleLine(t *testing.T) {
	var out bytes.Buffer
	printProjects(&out, []registry.Project{{Name: "a", Path: "/p", Adj: true, NotifyCommand: "missing\x1b[2J\nnext\x07"}})
	want := "a  /p  adj  notify_command=missing next\n"
	if got := out.String(); got != want {
		t.Fatalf("unsafe project row: %q, want %q", got, want)
	}
}
