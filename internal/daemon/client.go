// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
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
			Project:   p,
			Items:     source.Result[[]slicercli.Item]{Err: err},
			Deps:      source.Result[[]slicercli.Item]{Err: err},
			Goals:     source.Result[*slicercli.Goals]{Err: err},
			Runs:      source.Result[[]slicercli.RelayRun]{Err: err},
			BoardRuns: source.Result[[]slicercli.RelayRun]{Err: err},
			Open:      source.Result[[]adjcli.Record]{Err: err},
			Decided:   source.Result[[]adjcli.Record]{Err: err},
		}
	}
	return source.Snapshot{
		SelfHost:     out.SelfHost,
		BehindMain:   out.BehindMain,
		RolesRunning: out.RolesRunning,
		Unavailable:  out.Unavailable,
		Project:      out.Project,
		EngineRuns:   out.EngineRuns,
		Items:        out.Items.result(),
		Deps:         out.Deps.result(),
		Goals:        out.Goals.result(),
		Runs:         out.Runs.result(),
		BoardRuns:    out.BoardRuns.result(),
		Open:         out.Open.result(),
		Decided:      out.Decided.result(),
	}
}

// Subscribe makes focus the projects this connection shows. The daemon then
// sends this connection a notice for each change; read them with Next. Send
// requests on another connection.
func (c *Client) Subscribe(ctx context.Context, focus []string) error {
	return c.c.Call(ctx, MethodSubscribe, SubscribeParams{Focus: focus}, nil)
}

// Refresh reads every part of a project now and returns once they are read.
func (c *Client) Refresh(ctx context.Context, project string) error {
	return c.c.Call(ctx, MethodRefresh, RefreshParams{Project: project}, nil)
}

// Next waits for the next change notice on a subscribed connection.
func (c *Client) Next(ctx context.Context) (SnapshotChanged, error) {
	n, err := c.c.Next(ctx)
	if err != nil {
		return SnapshotChanged{}, err
	}
	var out SnapshotChanged
	if n.Method != MethodSnapshotChanged {
		return out, &ipc.Error{Code: "unexpected_notice", Message: "notice " + n.Method}
	}
	if err := json.Unmarshal(n.Params, &out); err != nil {
		return out, err
	}
	return out, nil
}

// Call sends an engine command over the daemon connection.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	return c.c.Call(ctx, method, params, result)
}

// Approve approves the run's checked result without a confirmation round trip.
func (c *Client) Approve(ctx context.Context, run string) error {
	return c.Call(ctx, MethodRunApprove, RunAction{Run: run}, nil)
}

// Stop stops an engine run after the caller has confirmed it.
func (c *Client) Stop(ctx context.Context, run string) error {
	return c.Call(ctx, MethodRunStop, RunAction{Run: run}, nil)
}

// Retry retries a paused run after the caller has confirmed it.
func (c *Client) Retry(ctx context.Context, run string) error {
	return c.Call(ctx, MethodRunRetry, RunAction{Run: run}, nil)
}

// Answer resumes a paused question with the owner's text.
func (c *Client) Answer(ctx context.Context, run, text string) error {
	_, err := c.AnswerAction(ctx, RunAction{Run: run, Text: text})
	return err
}

// WaveSuggest reads slicer's plan-parallel waves for a registered project.
func (c *Client) WaveSuggest(ctx context.Context, project string) (*slicercli.ParallelPlan, error) {
	var out slicercli.ParallelPlan
	if err := c.Call(ctx, MethodWaveSuggest, WaveSuggestParams{Project: project}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WaveOverlap reads the paths that the items share. Items needs at least two.
func (c *Client) WaveOverlap(ctx context.Context, project string, items []string) (*slicercli.Overlap, error) {
	var out slicercli.Overlap
	if err := c.Call(ctx, MethodWaveOverlap, WaveOverlapParams{Project: project, Items: items}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ItemShow reads the registered project's item detail.
func (c *Client) ItemShow(ctx context.Context, project, id string) (*slicercli.Detail, error) {
	var out ItemShowResult
	if err := c.Call(ctx, MethodItemShow, ItemShowParams{Project: project, ID: id}, &out); err != nil {
		return nil, err
	}
	return out.Detail, nil
}

// Plan resolves the slice's launch plan without starting it.
func (c *Client) Plan(ctx context.Context, params RunParams) (*RunPlan, error) {
	var out RunPlan
	if err := c.Call(ctx, MethodRunPlan, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Start starts the slice with its confirmed plan.
func (c *Client) Start(ctx context.Context, params RunParams) (runrec.Record, error) {
	var out runrec.Record
	err := c.Call(ctx, MethodRunStart, params, &out)
	return out, err
}

// WaveStart starts the items with their confirmed plans.
func (c *Client) WaveStart(ctx context.Context, params WaveStartParams) (*WaveResult, error) {
	var out WaveResult
	if err := c.Call(ctx, MethodWaveStart, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Cleanup removes a merged run's worktree and branch after owner confirmation.
func (c *Client) Cleanup(ctx context.Context, run string) error {
	return c.Call(ctx, MethodRunCleanup, RunAction{Run: run}, nil)
}

// UnavailableProjects returns stored run groups without registering them.
func (c *Client) UnavailableProjects(ctx context.Context) ([]source.Snapshot, error) {
	var out ProjectsResult
	if err := c.c.Call(ctx, MethodProjectsList, nil, &out); err != nil {
		return nil, err
	}
	var groups []source.Snapshot
	for _, p := range out.Unavailable {
		groups = append(groups, source.Snapshot{Project: registry.Project{Name: p.Name}, Unavailable: true, EngineRuns: p.EngineRuns})
	}
	return groups, nil
}

// AnswerAction passes the TUI's optional adj filing request and returns its notice.
func (c *Client) AnswerAction(ctx context.Context, action RunAction) (string, error) {
	var reply RunReply
	err := c.Call(ctx, MethodRunAnswer, action, &reply)
	return reply.Message, err
}

// FileFinding files the run's later finding n (one-based) as a slice after the
// owner confirmed it, and returns the notice that names the new slice.
func (c *Client) FileFinding(ctx context.Context, run string, n int) (string, error) {
	var reply RunReply
	err := c.Call(ctx, MethodRunFile, RunAction{Run: run, Finding: n}, &reply)
	return reply.Message, err
}
