// SPDX-License-Identifier: GPL-3.0-or-later

// Package slicercli makes typed calls over the slicer CLI. Every call passes
// --root, and none reads slicer's files.
package slicercli

import (
	"context"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
)

// Client calls the slicer CLI.
type Client struct {
	Bin     string        // binary name or path; empty means "slicer" found on PATH
	Timeout time.Duration // per-call deadline; zero means clirun.DefaultTimeout
}

func (c Client) call(args ...string) clirun.Call {
	bin := c.Bin
	if bin == "" {
		bin = "slicer"
	}
	return clirun.Call{Bin: bin, Args: args, Timeout: c.Timeout}
}

// PlanParallel runs `slicer plan-parallel --json --lean --root ROOT`.
func (c Client) PlanParallel(ctx context.Context, root string) (*ParallelPlan, error) {
	var plan ParallelPlan
	if err := clirun.RunJSON(ctx, c.call("plan-parallel", "--json", "--lean", "--root", root), &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// DepsOverlap runs `slicer deps --overlap ID... --json --lean --root ROOT`,
// which lists the paths each pair of the given items shares.
func (c Client) DepsOverlap(ctx context.Context, root string, ids ...string) (*Overlap, error) {
	args := append([]string{"deps", "--overlap"}, ids...)
	args = append(args, "--json", "--lean", "--root", root)
	var out Overlap
	if err := clirun.RunJSON(ctx, c.call(args...), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// List runs `slicer list --json --lean --root ROOT`.
func (c Client) List(ctx context.Context, root string) ([]Item, error) {
	var items []Item
	if err := clirun.RunJSON(ctx, c.call("list", "--json", "--lean", "--root", root), &items); err != nil {
		return nil, err
	}
	return items, nil
}

// Deps runs `slicer deps --json --lean --root ROOT` for unblocked items.
func (c Client) Deps(ctx context.Context, root string) ([]Item, error) {
	var items []Item
	if err := clirun.RunJSON(ctx, c.call("deps", "--json", "--lean", "--root", root), &items); err != nil {
		return nil, err
	}
	return items, nil
}

// Show runs `slicer show ID --json --lean --root ROOT`.
func (c Client) Show(ctx context.Context, root, id string) (*Detail, error) {
	var d Detail
	if err := clirun.RunJSON(ctx, c.call("show", id, "--json", "--lean", "--root", root), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Goals runs `slicer goals --json --root ROOT`.
func (c Client) Goals(ctx context.Context, root string) (*Goals, error) {
	var g Goals
	if err := clirun.RunJSON(ctx, c.call("goals", "--json", "--root", root), &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// RelayRuns runs `slicer relay-run list --json --lean [--active] --root ROOT`.
// With activeOnly, slicer lists only runs that have not ended and have had an
// event in the last 30 minutes.
func (c Client) RelayRuns(ctx context.Context, root string, activeOnly bool) ([]RelayRun, error) {
	args := []string{"relay-run", "list", "--json", "--lean"}
	if activeOnly {
		args = append(args, "--active")
	}
	args = append(args, "--root", root)
	var out struct {
		Runs []RelayRun `json:"runs"`
	}
	if err := clirun.RunJSON(ctx, c.call(args...), &out); err != nil {
		return nil, err
	}
	return out.Runs, nil
}
