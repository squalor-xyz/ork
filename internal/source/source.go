// SPDX-License-Identifier: GPL-3.0-or-later

// Package source is the one interface the TUI, and later the daemon, read
// slicer and adj state through. Direct implements it over the CLI adapters;
// S03 moves the same interface behind the daemon socket.
package source

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// Part names one part of a project's state. Every part is one CLI call,
// except Head, which is one git call.
type Part string

// The parts of a project. Head is the commit HEAD points at, read with
// `git -C PATH rev-parse HEAD` (boundary.git-head).
const (
	Items   Part = "items"
	Deps    Part = "deps"
	Goals   Part = "goals"
	Runs    Part = "runs"
	Open    Part = "open"
	Decided Part = "decided"
	Head    Part = "head"
)

// Fetcher makes the one call a part needs and returns the part's value, typed
// as the matching Snapshot field (Head: a string). A failed call returns an
// error and no value.
type Fetcher interface {
	Fetch(ctx context.Context, p registry.Project, part Part) (any, error)
}

// Result is one call's value and its own error. A failed call leaves Value at
// its zero value. ReadAt is when Value was read: a failed call after a good
// read keeps that value and its ReadAt, which makes the result stale.
type Result[T any] struct {
	Value  T
	ReadAt time.Time // zero when no value was ever read
	Err    error
}

// Stale reports a failed call that keeps an earlier good value.
func (r Result[T]) Stale() bool { return r.Err != nil && !r.ReadAt.IsZero() }

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
	EngineRuns []EngineRun
	Project    registry.Project
	Items      Result[[]slicercli.Item]
	Deps       Result[[]slicercli.Item]
	Goals      Result[*slicercli.Goals]
	Runs       Result[[]slicercli.RelayRun] // active runs only
	Open       Result[[]adjcli.Record]
	Decided    Result[[]adjcli.Record]
}

// Source reads the state of registered projects.
type Source interface {
	Snapshot(ctx context.Context, p registry.Project) Snapshot
}

// Direct implements Source by calling the adapters.
type Direct struct {
	Slicer slicercli.Client
	Adj    adjcli.Client
	Git    string // git binary; empty means "git" found on PATH
}

// NewDirect returns a Source that calls the given clients.
func NewDirect(s slicercli.Client, a adjcli.Client) *Direct {
	return &Direct{Slicer: s, Adj: a}
}

// Fetch makes the one call part needs and returns its value.
func (d *Direct) Fetch(ctx context.Context, p registry.Project, part Part) (any, error) {
	switch part {
	case Items:
		return d.Slicer.List(ctx, p.Path)
	case Deps:
		return d.Slicer.Deps(ctx, p.Path)
	case Goals:
		return d.Slicer.Goals(ctx, p.Path)
	case Runs:
		return d.Slicer.RelayRuns(ctx, p.Path, true)
	case Open:
		return d.Adj.Inbox(ctx, p.Path)
	case Decided:
		return d.Adj.List(ctx, p.Path, "decided")
	case Head:
		return d.head(ctx, p.Path)
	}
	return nil, fmt.Errorf("unknown part %q", part)
}

// head returns the commit HEAD points at. It is the only git call a refresh
// makes (boundary.git-head).
func (d *Direct) head(ctx context.Context, path string) (string, error) {
	bin := d.Git
	if bin == "" {
		bin = "git"
	}
	res, err := clirun.Run(ctx, clirun.Call{Bin: bin, Args: []string{"-C", path, "rev-parse", "HEAD"}})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// Snapshot makes the calls at the same time, so a slow call does not delay
// the others, and never fails as a whole. Each call writes only its own field.
func (d *Direct) Snapshot(ctx context.Context, p registry.Project) Snapshot {
	s := Snapshot{Project: p}
	calls := []func(){
		func() { s.Items = result(d.Slicer.List(ctx, p.Path)) },
		func() { s.Deps = result(d.Slicer.Deps(ctx, p.Path)) },
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

// EngineRole is the display subset of a role record. Wire fields are proposed.
type EngineRole struct {
	Role      string    `json:"role"`
	Round     int       `json:"round"`
	Vendor    string    `json:"vendor"`
	Model     string    `json:"model,omitempty"`
	Effort    string    `json:"effort,omitempty"`
	Step      string    `json:"step,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
	Exit      string    `json:"exit,omitempty"`
}

// EngineRun is the daemon's display of one engine run. Wire fields are proposed.
type EngineRun struct {
	Branch    string       `json:"branch,omitempty"`
	Merged    *bool        `json:"merged,omitempty"` // nil: unknown; S63 supplies merge detection
	Roles     []EngineRole `json:"roles,omitempty"`
	ID        string       `json:"id"`
	Item      string       `json:"item"`
	State     string       `json:"state"`
	Wait      string       `json:"wait,omitempty"`
	Pending   string       `json:"pending,omitempty"` // paused runrec pending kind (ui.pending-kind)
	Denials   []string     `json:"denials,omitempty"` // one text per denial: "Tool" or "Tool: command"
	RelayRun  string       `json:"relay_run,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
}

// Superseded reports a stopped run replaced by a newer engine run of its slice.
// The caller supplies runs for a single project. Ledger runs do not participate.
func (r EngineRun) Superseded(runs []EngineRun) bool {
	if r.State != "stopped" {
		return false
	}
	for _, newer := range runs {
		if newer.Item == r.Item && newer.CreatedAt.After(r.CreatedAt) {
			return true
		}
	}
	return false
}

// NeedsYou uses the same engine states for status and the Overview.
func (r EngineRun) NeedsYou(runs []EngineRun) bool {
	return r.State == "awaiting-approval" || r.State == "paused" || (r.State == "stopped" && !r.Superseded(runs))
}
