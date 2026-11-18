// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
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

// S431 (*proposed*; adj 464): auto_rebuild is allowed only with a build command,
// on a self-host project, and a registered value is kept.
func TestProjectsSetAutoRebuildRules(t *testing.T) {
	d, _, _, fake := rebuildDaemon(t, false, nil)
	fake.fail = &BuildError{Reason: "exit 1", Lines: []string{"stop here"}} // a drain that starts ends at once
	set := func(raw string) error {
		_, err := d.handle(context.Background(), MethodProjectsSet, json.RawMessage(raw))
		return err
	}
	saved := func() registry.Project {
		f, err := registry.Load(d.cfg.RegistryPath)
		if err != nil {
			t.Fatal(err)
		}
		return f.Projects[0]
	}
	// No build command: refused, in the same call that clears it as well.
	if err := set(`{"name":"p","build_command":"","auto_rebuild":true}`); err == nil || !strings.Contains(err.Error(), "auto_rebuild needs a build_command") {
		t.Fatalf("auto_rebuild with no build command: %v", err)
	}
	if got := saved(); got.AutoRebuild || got.BuildCommand != "make build" {
		t.Fatalf("a refusal changed the registry: %+v", got)
	}
	// Not self-host (the daemon's build is not a commit of the project): refused.
	d.cfg.BuildID = "dev"
	if err := set(`{"name":"p","auto_rebuild":true}`); err == nil || !strings.Contains(err.Error(), "only for a self-host project") {
		t.Fatalf("auto_rebuild on a project that is not self-host: %v", err)
	}
	d.cfg.BuildID = rebuildOld + "+modified=false"
	if err := set(`{"name":"p","auto_rebuild":true}`); err != nil {
		t.Fatal(err)
	}
	if !saved().AutoRebuild {
		t.Fatal("auto_rebuild on was not saved")
	}
	// Clearing the build command under it is refused too.
	if err := set(`{"name":"p","build_command":""}`); err == nil || !strings.Contains(err.Error(), "auto_rebuild needs a build_command") {
		t.Fatalf("clearing the build command under auto_rebuild: %v", err)
	}
	if err := set(`{"name":"p","auto_rebuild":false}`); err != nil {
		t.Fatal(err)
	}
	if saved().AutoRebuild {
		t.Fatal("auto_rebuild off was not saved")
	}
	if (SetProjectParams{Name: "p", AutoRebuild: new(bool)}).SetsNothing() {
		t.Fatal("auto_rebuild alone counts as no setting")
	}
}

// The notice of a drain speaks while roles run, where the behind notice is
// silent; with nothing pending it says nothing and the manual notice stays.
func TestRebuildNotice(t *testing.T) {
	pending := &StatusResult{RolesRunning: true, Projects: []ProjectStatus{{SelfHost: true, BehindMain: true, BuildCommand: "make", Rebuild: &RebuildStatus{State: RebuildDraining, Message: "rebuild pending: waiting for 2 roles"}}}}
	if got := RebuildNotice(pending); got != "rebuild pending: waiting for 2 roles" {
		t.Fatalf("notice of a drain: %q", got)
	}
	manual := &StatusResult{Projects: []ProjectStatus{{SelfHost: true, BehindMain: true, BuildCommand: "make"}}}
	if RebuildNotice(manual) != "" || BehindWarning(manual) != BehindNotice+" --rebuild" {
		t.Fatalf("manual: %q, %q", RebuildNotice(manual), BehindWarning(manual))
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
