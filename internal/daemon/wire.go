// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// The methods a client can call.
const (
	MethodStatus       = "status"        // params StatusParams, result StatusResult
	MethodSnapshot     = "snapshot"      // params SnapshotParams, result SnapshotResult
	MethodProjectsList = "projects.list" // no params, result ProjectsResult
	MethodProjectsAdd  = "projects.add"  // params AddProjectParams, result registry.Project
	MethodProjectsRm   = "projects.rm"   // params RemoveProjectParams, no result
)

// Error codes the handlers add to the protocol's own (ipc.CodeBadRequest,
// ipc.CodeInternal).
const (
	CodeUnknownMethod = "unknown_method"
	CodeBadParams     = "bad_params"
	CodeNotFound      = "not_found"
	CodeDuplicateName = "duplicate_name"
	CodeInvalidPath   = "invalid_path"
	CodeRegistry      = "registry"
)

// StatusParams selects the projects of a status call. An empty Project means
// every registered project.
type StatusParams struct {
	Project string `json:"project,omitempty"`
}

// StatusResult is what `ork status --json` prints.
type StatusResult struct {
	Projects []ProjectStatus `json:"projects"`
}

// ProjectStatus is one project in a status. NeedsYou counts the project's
// open adj questions; approvals, stops and pauses join it with the engine
// (S04).
type ProjectStatus struct {
	Name         string      `json:"name"`
	Path         string      `json:"path"`
	Adj          bool        `json:"adj"`
	NeedsYou     int         `json:"needs_you"`
	ActiveRelays []Relay     `json:"active_relays"`
	Errors       []PartError `json:"errors,omitempty"`
}

// Relay is one active slicer relay run.
type Relay struct {
	ID        string   `json:"id"`
	Items     []string `json:"items"`
	State     string   `json:"state"`
	Worktree  string   `json:"worktree"`
	BeganAt   string   `json:"begun_at"`
	UpdatedAt string   `json:"updated_at"`
	Steps     int      `json:"steps"`
	Running   int      `json:"running"`
}

// PartError is a failed part of a project's snapshot: Part is one of "items",
// "goals", "runs", "open" and "decided". Code and Message are the tool
// error's own when slicer or adj printed one.
type PartError struct {
	Part    string `json:"part"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SnapshotParams names the project of a snapshot call.
type SnapshotParams struct {
	Project string `json:"project"`
}

// Part is one part of a snapshot over the wire: a value, or an error as
// {code,message}. A slicer or adj tool error keeps its code.
type Part[T any] struct {
	Value T          `json:"value"`
	Error *ipc.Error `json:"error,omitempty"`
}

func partOf[T any](r source.Result[T]) Part[T] {
	if r.Err != nil {
		return Part[T]{Error: ipc.ErrorOf(r.Err)}
	}
	return Part[T]{Value: r.Value}
}

func (p Part[T]) result() source.Result[T] {
	if p.Error != nil {
		return source.Result[T]{Err: p.Error}
	}
	return source.Result[T]{Value: p.Value}
}

// SnapshotResult is source.Snapshot over the wire.
type SnapshotResult struct {
	Project registry.Project           `json:"project"`
	Items   Part[[]slicercli.Item]     `json:"items"`
	Goals   Part[*slicercli.Goals]     `json:"goals"`
	Runs    Part[[]slicercli.RelayRun] `json:"runs"`
	Open    Part[[]adjcli.Record]      `json:"open"`
	Decided Part[[]adjcli.Record]      `json:"decided"`
}

// AddProjectParams is a projects.add call. An empty Name means the base name
// of Path.
type AddProjectParams struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

// RemoveProjectParams is a projects.rm call.
type RemoveProjectParams struct {
	Name string `json:"name"`
}

// ProjectsResult is the registered projects, in registry order.
type ProjectsResult struct {
	Projects []registry.Project `json:"projects"`
}
