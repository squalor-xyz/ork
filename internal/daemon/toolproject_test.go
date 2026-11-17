// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"path/filepath"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
)

// A run's engine resolves the project a tool feedback entry names from the
// registry (S387, *proposed*; adj 412): by the slug of the project's name, so
// case does not matter, and nothing resolves for an unregistered tool.
func TestRunDepsResolveARegisteredToolProject(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	other := registry.Project{Name: "Slicer", Path: filepath.Join(filepath.Dir(p.Path), "slicer")}
	if err := registry.Save(d.cfg.RegistryPath, &registry.File{Version: registry.Version, Projects: []registry.Project{p, other}}); err != nil {
		t.Fatal(err)
	}
	lookup := d.runDeps(p).Project
	if lookup == nil {
		t.Fatal("runDeps leaves Deps.Project unset")
	}
	if got, ok := lookup("slicer"); !ok || got.Path != other.Path {
		t.Errorf("slicer resolves to %+v, %v", got, ok)
	}
	if got, ok := lookup("some-cli"); ok {
		t.Errorf("an unregistered tool resolves to %+v", got)
	}
}
