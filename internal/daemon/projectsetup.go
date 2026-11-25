// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

type setupStep struct {
	name, command string
	run           func(context.Context, engine.Deps, string) error
	registry      bool
}

var setupSteps = []setupStep{
	{"slicer-init", "slicer init", func(ctx context.Context, d engine.Deps, root string) error {
		return d.Slicer.Init(ctx, runrec.Writer{}, root)
	}, false},
	{"relay-roles-init", "slicer relay-roles init --render", func(ctx context.Context, d engine.Deps, root string) error {
		return d.Slicer.RelayRolesInit(ctx, runrec.Writer{}, root)
	}, false},
	{"adj-init", "adj init", func(ctx context.Context, d engine.Deps, root string) error {
		return d.Adj.Init(ctx, runrec.Writer{}, root)
	}, true},
	{"adj-off", "registry: adj off", nil, true},
}

// projectSetup runs only owning-tool commands; ork writes only its registry.
func (d *Daemon) projectSetup(ctx context.Context, p ProjectSetupParams) (*ProjectSetupResult, error) {
	if strings.TrimSpace(p.Project) == "" {
		return nil, badParams(fmt.Errorf("project.setup needs a project"))
	}
	i := slices.IndexFunc(setupSteps, func(s setupStep) bool { return s.name == p.Step })
	if i < 0 {
		return nil, badParams(fmt.Errorf("step must be slicer-init, relay-roles-init, adj-init or adj-off"))
	}
	step := setupSteps[i]
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	out := &ProjectSetupResult{Command: step.command}
	if step.registry {
		out.Adj = &proj.Adj
	}
	if p.Preview {
		return out, nil
	}
	if step.registry {
		proj, err = d.setupAdj(ctx, p.Project, step)
		if err != nil {
			return nil, err
		}
		out.Adj = &proj.Adj
	} else if err = step.run(ctx, d.runDeps(proj), proj.Path); err != nil {
		return nil, err
	}
	out.Done = true
	// Refresh only after releasing registry/run locks: cache reads may invoke
	// tools and publish snapshots. A refresh failure does not undo the write.
	if step.run != nil {
		_ = d.cache.Refresh(ctx, proj, source.Items, source.Deps, source.Goals, source.Runs, source.Open, source.Decided)
	}
	return out, nil
}

func (d *Daemon) setupAdj(ctx context.Context, name string, step setupStep) (registry.Project, error) {
	d.regMu.Lock()
	defer d.regMu.Unlock()
	f, err := d.loadRegistry()
	if err != nil {
		return registry.Project{}, err
	}
	i := slices.IndexFunc(f.Projects, func(p registry.Project) bool { return p.Name == name })
	if i < 0 {
		return registry.Project{}, notFound(name, f)
	}
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	ids, err := d.unfinishedRunsLocked(name)
	if err != nil {
		return registry.Project{}, ipc.ErrorOf(err)
	}
	if len(ids) != 0 {
		return registry.Project{}, &ipc.Error{Code: CodeProjectBusy, Message: fmt.Sprintf("project %s has unfinished runs: %s", name, strings.Join(ids, ", "))}
	}
	proj := &f.Projects[i]
	if step.run != nil {
		if err := step.run(ctx, d.runDeps(*proj), proj.Path); err != nil {
			return registry.Project{}, err
		}
	}
	proj.Adj = step.name == "adj-init"
	if err := registry.Save(d.registryPath(), f); err != nil {
		return registry.Project{}, &ipc.Error{Code: CodeRegistry, Message: err.Error()}
	}
	return *proj, nil
}
