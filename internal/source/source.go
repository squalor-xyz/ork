// SPDX-License-Identifier: GPL-3.0-or-later

// Package source is the one interface the TUI, and later the daemon, read
// slicer and adj state through. Direct implements it over the CLI adapters;
// S03 moves the same interface behind the daemon socket.
package source

import (
	"context"
	"sync"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// Result is one call's value and its own error. A failed call leaves Value at
// its zero value.
type Result[T any] struct {
	Value T
	Err   error
}

func result[T any](v T, err error) Result[T] {
	if err != nil {
		var zero T
		return Result[T]{Value: zero, Err: err}
	}
	return Result[T]{Value: v}
}

// Snapshot is what ork shows for one project. Each part has its own error, so
// one failed call does not blank the others. Open and Decided stay empty, with
// no error, for a project whose registry entry has adj false.
type Snapshot struct {
	Project registry.Project
	Items   Result[[]slicercli.Item]
	Goals   Result[*slicercli.Goals]
	Runs    Result[[]slicercli.RelayRun] // active runs only
	Open    Result[[]adjcli.Record]
	Decided Result[[]adjcli.Record]
}

// Source reads the state of registered projects.
type Source interface {
	Snapshot(ctx context.Context, p registry.Project) Snapshot
}

// Direct implements Source by calling the adapters.
type Direct struct {
	Slicer slicercli.Client
	Adj    adjcli.Client
}

// NewDirect returns a Source that calls the given clients.
func NewDirect(s slicercli.Client, a adjcli.Client) *Direct {
	return &Direct{Slicer: s, Adj: a}
}

// Snapshot makes the calls at the same time, so a slow call does not delay
// the others, and never fails as a whole. Each call writes only its own field.
func (d *Direct) Snapshot(ctx context.Context, p registry.Project) Snapshot {
	s := Snapshot{Project: p}
	calls := []func(){
		func() { s.Items = result(d.Slicer.List(ctx, p.Path)) },
		func() { s.Goals = result(d.Slicer.Goals(ctx, p.Path)) },
		func() { s.Runs = result(d.Slicer.RelayRuns(ctx, p.Path, true)) },
	}
	if p.Adj {
		calls = append(calls,
			func() { s.Open = result(d.Adj.Inbox(ctx, p.Path)) },
			func() { s.Decided = result(d.Adj.List(ctx, p.Path, "decided")) },
		)
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
