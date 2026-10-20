// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// Daemon uses a fresh request connection per operation. A daemon restart cannot
// leave snapshots on a dead connection, and a refresh cannot block another
// project's snapshot. The notice connection stays open across focus changes.
type Daemon struct {
	Connect func(context.Context) (*daemon.Client, error)
	Feed    *daemon.Feed
}

func (d *Daemon) Snapshot(ctx context.Context, p registry.Project) source.Snapshot {
	c, err := d.Connect(ctx)
	if err != nil {
		return source.Snapshot{Project: p,
			Items:   source.Result[[]slicercli.Item]{Err: err},
			Goals:   source.Result[*slicercli.Goals]{Err: err},
			Runs:    source.Result[[]slicercli.RelayRun]{Err: err},
			Open:    source.Result[[]adjcli.Record]{Err: err},
			Decided: source.Result[[]adjcli.Record]{Err: err}}
	}
	defer c.Close()
	return c.Snapshot(ctx, p)
}
func (d *Daemon) Refresh(ctx context.Context, project string) error {
	c, err := d.Connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Refresh(ctx, project)
}
func (d *Daemon) Focus(ctx context.Context, projects []string) error {
	return d.Feed.Focus(ctx, projects)
}
func (d *Daemon) Next(ctx context.Context) (string, error) {
	n, err := d.Feed.Next(ctx)
	return n.Project, err
}

func (d *Daemon) Approve(ctx context.Context, run string) error {
	c, err := d.Connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Approve(ctx, run)
}
func (d *Daemon) Stop(ctx context.Context, run string) error {
	c, err := d.Connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Stop(ctx, run)
}
func (d *Daemon) Retry(ctx context.Context, run string) error {
	c, err := d.Connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Retry(ctx, run)
}

func (d *Daemon) Answer(ctx context.Context, run, text string) error {
	c, err := d.Connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Answer(ctx, run, text)
}
