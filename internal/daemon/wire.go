// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"time"

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
	MethodSubscribe    = "subscribe"     // params SubscribeParams, no result
	MethodRefresh      = "refresh"       // params RefreshParams, no result
)

// MethodSnapshotChanged is the notice a subscribed connection gets when a part
// of a project changes. The daemon sends it without an id; params are
// SnapshotChanged.
const MethodSnapshotChanged = "snapshot.changed"

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
// (S04). Parts holds each part's age and state.
type ProjectStatus struct {
	EngineRuns   []EngineRun          `json:"engine_runs"`
	Name         string               `json:"name"`
	Path         string               `json:"path"`
	Adj          bool                 `json:"adj"`
	NeedsYou     int                  `json:"needs_you"`
	ActiveRelays []Relay              `json:"active_relays"`
	Parts        map[string]PartState `json:"parts,omitempty"`
	Errors       []PartError          `json:"errors,omitempty"`
}

// PartState is one part's age and state as the cache holds it. AgeMs is the
// value's age in ms; a part that was never read has AgeMs 0 and an Error.
type PartState struct {
	AgeMs int64      `json:"age_ms"`
	Stale bool       `json:"stale,omitempty"`
	Error *ipc.Error `json:"error,omitempty"`
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

// Part is one part of a snapshot over the wire. Value is the last good value,
// and ReadAt says when it was read. Error is the last call's error, if any; a
// part with an Error and a ReadAt is stale, and AgeMs is then the value's age.
// A slicer or adj tool error keeps its code.
type Part[T any] struct {
	Value  T          `json:"value"`
	ReadAt *time.Time `json:"read_at,omitempty"`
	AgeMs  int64      `json:"age_ms"`
	Stale  bool       `json:"stale,omitempty"`
	Error  *ipc.Error `json:"error,omitempty"`
}

func partOf[T any](r source.Result[T], now time.Time) Part[T] {
	p := Part[T]{Value: r.Value, Stale: r.Stale()}
	if !r.ReadAt.IsZero() {
		at := r.ReadAt
		p.ReadAt = &at
		p.AgeMs = now.Sub(at).Milliseconds()
	}
	if r.Err != nil {
		p.Error = ipc.ErrorOf(r.Err)
	}
	return p
}

func (p Part[T]) result() source.Result[T] {
	r := source.Result[T]{Value: p.Value}
	if p.ReadAt != nil {
		r.ReadAt = *p.ReadAt
	}
	if p.Error != nil {
		r.Err = p.Error
	}
	return r
}

// SnapshotResult is source.Snapshot over the wire.
type SnapshotResult struct {
	EngineRuns []EngineRun                `json:"engine_runs"`
	Project    registry.Project           `json:"project"`
	Items      Part[[]slicercli.Item]     `json:"items"`
	Goals      Part[*slicercli.Goals]     `json:"goals"`
	Runs       Part[[]slicercli.RelayRun] `json:"runs"`
	Open       Part[[]adjcli.Record]      `json:"open"`
	Decided    Part[[]adjcli.Record]      `json:"decided"`
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

// SubscribeParams names the projects this connection shows. Their items and
// runs refresh every 5 s, the others every 30 s. Opening a project reads its
// goals and decided records now. Focus replaces the connection's earlier list.
type SubscribeParams struct {
	Focus []string `json:"focus"`
}

// RefreshParams names the project to read again now, every part of it.
type RefreshParams struct {
	Project string `json:"project"`
}

// SnapshotChanged is the notice for the parts of a project whose value changed.
type SnapshotChanged struct {
	Project string    `json:"project"`
	Parts   []string  `json:"parts"`
	At      time.Time `json:"at"`
}
