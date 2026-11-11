// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/source"
)

func TestRebuildStatusAndSetting(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "projects.json")
	if err := registry.Save(reg, &registry.File{Version: registry.Version, Projects: []registry.Project{{Name: "ork", Path: dir}}}); err != nil {
		t.Fatal(err)
	}
	d := New(Config{Paths: PathsIn(dir), RegistryPath: reg})
	for _, command := range []string{"sh ./build-only-in-home-dir.sh", ""} {
		p, err := d.setProject(context.Background(), SetProjectParams{Name: "ork", BuildCommand: &command})
		if err != nil {
			t.Fatal(err)
		}
		f, err := registry.Load(reg)
		if err != nil {
			t.Fatal(err)
		}
		if p.BuildCommand != command || f.Projects[0].BuildCommand != command {
			t.Fatalf("saved build command: reply %q disk %q want %q", p.BuildCommand, f.Projects[0].BuildCommand, command)
		}
		status := projectStatus(f.Projects[0], source.Snapshot{}, time.Now())
		status.SelfHost, status.BehindMain = true, true
		want := BehindNotice
		if command != "" {
			want += " --rebuild"
		}
		if got := BehindWarning(&StatusResult{Projects: []ProjectStatus{status}}); got != want {
			t.Fatalf("status rebuild notice: %q want %q", got, want)
		}
	}
}

func TestRebuildWire(t *testing.T) {
	command := "sh ./build-only-in-home-dir.sh"
	p := SetProjectParams{Name: "ork", BuildCommand: &command}
	if p.SetsNothing() {
		t.Fatal("build command treated as no setting")
	}
	if got := strings.Join(p.fieldsReadByARun(), ","); got != "" {
		t.Fatalf("build command read by a run: %s", got)
	}
}
