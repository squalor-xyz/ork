// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
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
			Items:     source.Result[[]slicercli.Item]{Err: err},
			Deps:      source.Result[[]slicercli.Item]{Err: err},
			Goals:     source.Result[*slicercli.Goals]{Err: err},
			Runs:      source.Result[[]slicercli.RelayRun]{Err: err},
			BoardRuns: source.Result[[]slicercli.RelayRun]{Err: err},
			Open:      source.Result[[]adjcli.Record]{Err: err},
			Decided:   source.Result[[]adjcli.Record]{Err: err}}
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

func (d *Daemon) ItemShow(ctx context.Context, project, id string) (*slicercli.Detail, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.ItemShow(ctx, project, id)
}

func (d *Daemon) Plan(ctx context.Context, params daemon.RunParams) (*daemon.RunPlan, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.Plan(ctx, params)
}
func (d *Daemon) Start(ctx context.Context, params daemon.RunParams) (runrec.Record, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return runrec.Record{}, err
	}
	defer c.Close()
	return c.Start(ctx, params)
}

func (d *Daemon) WaveSuggest(ctx context.Context, project string) (*slicercli.ParallelPlan, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.WaveSuggest(ctx, project)
}
func (d *Daemon) WaveOverlap(ctx context.Context, project string, items []string) (*slicercli.Overlap, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.WaveOverlap(ctx, project, items)
}
func (d *Daemon) WaveStart(ctx context.Context, params daemon.WaveStartParams) (*daemon.WaveResult, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.WaveStart(ctx, params)
}

func (d *Daemon) SpecCheck(ctx context.Context, project string, items []string) (*daemon.SpecCheckResult, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.SpecCheck(ctx, project, items)
}
func (d *Daemon) SpecAnswer(ctx context.Context, params daemon.SpecAnswerParams) (*daemon.SpecAnswerResult, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.SpecAnswer(ctx, params)
}

func (d *Daemon) Cleanup(ctx context.Context, run string) error {
	c, err := d.Connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Cleanup(ctx, run)
}

// ProjectsAdd registers a path through a fresh daemon connection.
func (d *Daemon) ProjectsAdd(ctx context.Context, path string) (registry.Project, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return registry.Project{}, err
	}
	defer c.Close()
	return c.AddProject(ctx, path, "")
}

// UnavailableProjects discovers read-only groups alongside registered projects.
func (d *Daemon) UnavailableProjects(ctx context.Context) ([]source.Snapshot, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.UnavailableProjects(ctx)
}

// FileLater files the run's later finding n as a slice after the owner
// confirmed it, and returns the daemon's notice.
func (d *Daemon) FileLater(ctx context.Context, run string, n int) (string, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.FileFinding(ctx, run, n)
}

func (d *Daemon) AnswerAction(ctx context.Context, action daemon.RunAction) (string, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.AnswerAction(ctx, action)
}

// Projects reads registered projects through a fresh daemon connection.
func (d *Daemon) Projects(ctx context.Context) ([]registry.Project, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.Projects(ctx)
}

// ProjectsRemove unregisters a project through a fresh daemon connection.
func (d *Daemon) ProjectsRemove(ctx context.Context, name string) error {
	c, err := d.Connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.RemoveProject(ctx, name)
}
