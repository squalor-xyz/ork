// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"sync"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// allParts is every part of a project, as a refresh reads them.
var allParts = []source.Part{source.Items, source.Deps, source.Goals, source.Runs, source.Open, source.Decided, source.Head}

// snapshotOf reads a project's parts from the cache (dash.cache). A part with
// no value yet is read now, one call for each part however many clients ask;
// the others are answered from the cache. Parts the project does not have are
// empty, with no error.
func (d *Daemon) snapshotOf(ctx context.Context, p registry.Project) source.Snapshot {
	s := source.Snapshot{Project: p}
	calls := []func(){
		func() { s.Items = readAs[[]slicercli.Item](ctx, d, p, source.Items) },
		func() { s.Deps = readAs[[]slicercli.Item](ctx, d, p, source.Deps) },
		func() { s.Goals = readAs[*slicercli.Goals](ctx, d, p, source.Goals) },
		func() { s.Runs = readAs[[]slicercli.RelayRun](ctx, d, p, source.Runs) },
		func() { s.Open = readAs[[]adjcli.Record](ctx, d, p, source.Open) },
		func() { s.Decided = readAs[[]adjcli.Record](ctx, d, p, source.Decided) },
	}
	var wg sync.WaitGroup
	for _, call := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call()
		}()
	}
	wg.Wait()
	s.EngineRuns = d.engineRuns(p.Name)
	return s
}

// readAs reads one part from the cache and types its value.
func readAs[T any](ctx context.Context, d *Daemon, p registry.Project, part source.Part) source.Result[T] {
	v := d.cache.Get(ctx, p, part)
	t, _ := v.Value.(T)
	return source.Result[T]{Value: t, ReadAt: v.ReadAt, Err: v.Err}
}

// waveSuggest reads slicer's plan-parallel waves for a registered project.
// It calls slicer on each request; the plan is not cached.
func (d *Daemon) waveSuggest(ctx context.Context, p WaveSuggestParams) (*slicercli.ParallelPlan, error) {
	project, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	return d.runs.deps.Slicer.PlanParallel(ctx, project.Path)
}

// waveOverlap reads the paths that the given items share. Slicer computes the
// overlap; ork does not compare Files itself. It calls slicer on each request.
func (d *Daemon) waveOverlap(ctx context.Context, p WaveOverlapParams) (*slicercli.Overlap, error) {
	if len(p.Items) < 2 {
		return nil, badParams(errors.New("overlap needs at least two items"))
	}
	project, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	return d.runs.deps.Slicer.DepsOverlap(ctx, project.Path, p.Items...)
}

// registered returns the project registered under name, or a not_found error.
func (d *Daemon) registered(name string) (registry.Project, error) {
	f, err := d.loadRegistry()
	if err != nil {
		return registry.Project{}, err
	}
	project, ok := find(f, name)
	if !ok {
		return registry.Project{}, notFound(name)
	}
	return project, nil
}
