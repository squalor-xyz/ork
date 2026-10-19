// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"sync"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// allParts is every part of a project, as a refresh reads them.
var allParts = []source.Part{source.Items, source.Goals, source.Runs, source.Open, source.Decided, source.Head}

// snapshotOf reads a project's parts from the cache (dash.cache). A part with
// no value yet is read now, one call for each part however many clients ask;
// the others are answered from the cache. Parts the project does not have are
// empty, with no error.
func (d *Daemon) snapshotOf(ctx context.Context, p registry.Project) source.Snapshot {
	s := source.Snapshot{Project: p}
	calls := []func(){
		func() { s.Items = readAs[[]slicercli.Item](ctx, d, p, source.Items) },
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
	return s
}

// readAs reads one part from the cache and types its value.
func readAs[T any](ctx context.Context, d *Daemon, p registry.Project, part source.Part) source.Result[T] {
	v := d.cache.Get(ctx, p, part)
	t, _ := v.Value.(T)
	return source.Result[T]{Value: t, ReadAt: v.ReadAt, Err: v.Err}
}
