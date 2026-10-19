// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// Client is a connection to the daemon. Snapshot makes it a source.Source,
// which S13 hands to the TUI.
type Client struct{ c *ipc.Client }

var _ source.Source = (*Client)(nil)

// Dial connects to the daemon's socket.
func Dial(ctx context.Context, socket string) (*Client, error) {
	c, err := ipc.Dial(ctx, socket)
	if err != nil {
		return nil, err
	}
	return &Client{c: c}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.c.Close() }

// Status asks for the status of one project, or of all when project is empty.
func (c *Client) Status(ctx context.Context, project string) (*StatusResult, error) {
	var out StatusResult
	if err := c.c.Call(ctx, MethodStatus, StatusParams{Project: project}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Projects lists the registered projects.
func (c *Client) Projects(ctx context.Context) ([]registry.Project, error) {
	var out ProjectsResult
	if err := c.c.Call(ctx, MethodProjectsList, nil, &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

// AddProject registers a project. An empty name means the base name of path.
func (c *Client) AddProject(ctx context.Context, path, name string) (registry.Project, error) {
	var out registry.Project
	err := c.c.Call(ctx, MethodProjectsAdd, AddProjectParams{Path: path, Name: name}, &out)
	return out, err
}

// RemoveProject unregisters a project.
func (c *Client) RemoveProject(ctx context.Context, name string) error {
	return c.c.Call(ctx, MethodProjectsRm, RemoveProjectParams{Name: name}, nil)
}

// Snapshot implements source.Source over the socket. The daemon looks the
// project up by p.Name. A failed call gives a snapshot whose every part holds
// the error, as an *ipc.Error with the tool's code when there was one.
func (c *Client) Snapshot(ctx context.Context, p registry.Project) source.Snapshot {
	var out SnapshotResult
	if err := c.c.Call(ctx, MethodSnapshot, SnapshotParams{Project: p.Name}, &out); err != nil {
		return source.Snapshot{
			Project: p,
			Items:   source.Result[[]slicercli.Item]{Err: err},
			Goals:   source.Result[*slicercli.Goals]{Err: err},
			Runs:    source.Result[[]slicercli.RelayRun]{Err: err},
			Open:    source.Result[[]adjcli.Record]{Err: err},
			Decided: source.Result[[]adjcli.Record]{Err: err},
		}
	}
	return source.Snapshot{
		Project: out.Project,
		Items:   out.Items.result(),
		Goals:   out.Goals.result(),
		Runs:    out.Runs.result(),
		Open:    out.Open.result(),
		Decided: out.Decided.result(),
	}
}
