// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// The methods a client can call.
const (
	MethodWaveStart    = "wave.start"    // params WaveStartParams (run.launch-wire), proposed result shape WaveResult
	MethodWaveStop     = "wave.stop"     // proposed wire; params WaveAction, result WaveResult
	MethodWaveSuggest  = "wave.suggest"  // proposed wire; params WaveSuggestParams, result slicercli.ParallelPlan
	MethodWaveOverlap  = "wave.overlap"  // proposed wire; params WaveOverlapParams, result slicercli.Overlap
	MethodRolesMatrix  = "roles.matrix"  // proposed (S168); params RolesMatrixParams, result RolesMatrixResult
	MethodDaemonInfo   = "daemon.info"   // no params, result DaemonInfoResult
	MethodDaemonStop   = "daemon.stop"   // params StopParams, result StopResult
	MethodStatus       = "status"        // params StatusParams, result StatusResult
	MethodItemNote     = "item.note"     // proposed; params ItemNoteParams, result ItemNoteResult
	MethodItemShow     = "item.show"     // proposed; params ItemShowParams, result ItemShowResult
	MethodSnapshot     = "snapshot"      // params SnapshotParams, result SnapshotResult
	MethodProjectsList = "projects.list" // no params, result ProjectsResult
	MethodProjectsAdd  = "projects.add"  // params AddProjectParams, result registry.Project
	MethodProjectsRm   = "projects.rm"   // params RemoveProjectParams, no result
	MethodProjectsSet  = "projects.set"  // proposed (S161); params SetProjectParams, result registry.Project
	MethodSettingsGet  = "settings.get"  // proposed (S174); no params, result SettingsResult
	MethodSettingsSet  = "settings.set"  // proposed (S174); params SetSettingsParams, result SettingsResult
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
	CodeProjectBusy   = "project_busy" // proposed
	CodeRefused       = "refused"      // proposed: an owner action the engine refused before it changed anything (S141)
)

// StatusParams selects the projects of a status call. An empty Project means
// every registered project.
type StatusParams struct {
	Project string `json:"project,omitempty"`
}

// StatusResult is what `ork status --json` prints.
type StatusResult struct {
	BuildID      string          `json:"build_id"`
	RolesRunning bool            `json:"roles_running"`
	Projects     []ProjectStatus `json:"projects"`
}

// DaemonInfoResult identifies the daemon without reading registry or project data.
type DaemonInfoResult struct {
	BuildID string `json:"build_id"`
}

// StopParams requests daemon shutdown. Force permits cancelling active work.
type StopParams struct {
	Force bool `json:"force,omitempty"`
}

type StopResult struct {
	Stopped bool `json:"stopped"`
}

// ProjectStatus is one project in a status. NeedsYou counts the project's
// open adj questions, needs-owner items and Needs-section engine runs.
// Parts holds each part's age and state.
type ProjectStatus struct {
	SelfHost     bool                 `json:"self_host"`             // proposed
	BehindMain   bool                 `json:"behind_main"`           // proposed
	Unavailable  bool                 `json:"unavailable,omitempty"` // proposed: display only, not registered
	Open         []OpenRecord         `json:"open,omitempty"`        // proposed
	Held         []HeldItem           `json:"held,omitempty"`        // proposed
	EngineRuns   []EngineRun          `json:"engine_runs"`
	Name         string               `json:"name"`
	Path         string               `json:"path"`
	Adj          bool                 `json:"adj"`
	NeedsYou     int                  `json:"needs_you"`
	ActiveRelays []Relay              `json:"active_relays"`
	Parts        map[string]PartState `json:"parts,omitempty"`
	Errors       []PartError          `json:"errors,omitempty"`
}

// OpenRecord is a proposed status wire summary of an open adj record.
type OpenRecord struct {
	ID    int    `json:"id"`
	Key   string `json:"key"`
	Title string `json:"title"`
}

// HeldItem is a proposed status wire summary of a needs-owner item.
type HeldItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
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
	// Proposed role details for the last start and fail steps in ledger order.
	RunningStep *RelayStep `json:"running_step,omitempty"`
	FailedStep  *RelayStep `json:"failed_step,omitempty"`
}

// RelayStep is a proposed summary of a relay's running or failed step.
type RelayStep struct {
	Role   string `json:"role"`
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
	Task   string `json:"task"`
	Phase  string `json:"phase"`
}

// FormatRelayStep renders the proposed running or failed wording on one line.
// Empty vendor, model and task fields leave no separators behind.
func FormatRelayStep(label string, step RelayStep) string {
	text := label + ": " + step.Role
	if label == "running" {
		model := step.Vendor
		if step.Model != "" {
			if model != "" {
				model += "/"
			}
			model += step.Model
		}
		if model != "" {
			text += " " + model
		}
	}
	if step.Task != "" {
		task := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(step.Task)
		text += ": " + task
	}
	return text
}

// PartError is a failed part of a project's status: Part is one of "items",
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
	SelfHost     bool                       `json:"self_host"`             // proposed
	BehindMain   bool                       `json:"behind_main"`           // proposed
	RolesRunning bool                       `json:"roles_running"`         // global activity, as in status
	Unavailable  bool                       `json:"unavailable,omitempty"` // proposed: stored runs only
	BoardRuns    Part[[]slicercli.RelayRun] `json:"board_runs"`            // proposed: stale and recently ended runs
	EngineRuns   []EngineRun                `json:"engine_runs"`
	Project      registry.Project           `json:"project"`
	Items        Part[[]slicercli.Item]     `json:"items"`
	Deps         Part[[]slicercli.Item]     `json:"deps"`
	Goals        Part[*slicercli.Goals]     `json:"goals"`
	Runs         Part[[]slicercli.RelayRun] `json:"runs"`
	Open         Part[[]adjcli.Record]      `json:"open"`
	Decided      Part[[]adjcli.Record]      `json:"decided"`
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

// SetProjectParams is a projects.set call (S161, *proposed*). Name picks the
// project. Every other field is a pointer or a map: nil leaves the registry
// value as it is, and a pointer to "" or 0 clears it (a zero value means
// unset). The daemon refuses a call that sets no field.
//
// S174 (*proposed*) adds three groups. EnvironmentLines replaces the list; a
// pointer to an empty list clears it. Permissions changes one role at a time:
// a non-nil entry sets the role's permission, and a nil entry (JSON null)
// removes it, so the default applies. VendorEnv changes one key at a time: a
// non-nil value sets the key, and a nil value removes it; a vendor with no
// keys left is removed.
type SetProjectParams struct {
	Name             string                          `json:"name"`
	Push             *bool                           `json:"push,omitempty"`
	UIDoc            *string                         `json:"ui_doc,omitempty"`
	WorktreePattern  *string                         `json:"worktree_pattern,omitempty"`
	BranchPattern    *string                         `json:"branch_pattern,omitempty"`
	CheckCommand     *string                         `json:"check_command,omitempty"`
	CheckMinutes     *int                            `json:"check_minutes,omitempty"`
	Cap              *int                            `json:"cap,omitempty"`
	BudgetMinutes    *int                            `json:"budget_minutes,omitempty"`
	BudgetTokens     *int                            `json:"budget_tokens,omitempty"`
	EnvironmentLines *[]string                       `json:"environment_lines,omitempty"`
	Permissions      map[string]*registry.Permission `json:"permissions,omitempty"`
	VendorEnv        map[string]map[string]*string   `json:"vendor_env,omitempty"`
}

// SetsNothing reports whether the call sets no field. The struct holds maps,
// so it cannot be compared with ==. The daemon and the command line both use it.
func (p SetProjectParams) SetsNothing() bool {
	return p.Push == nil && p.UIDoc == nil && p.WorktreePattern == nil && p.BranchPattern == nil &&
		p.CheckCommand == nil && p.CheckMinutes == nil && p.Cap == nil && p.BudgetMinutes == nil &&
		p.BudgetTokens == nil && p.EnvironmentLines == nil && len(p.Permissions) == 0 && len(p.VendorEnv) == 0
}

// SetSettingsParams is a settings.set call (S174, *proposed*). Each field is a
// pointer: nil leaves the registry value as it is, and a pointer to 0 clears
// it (zero means unset). The daemon refuses a call that sets no field and a
// negative number.
type SetSettingsParams struct {
	GlobalCap         *int `json:"global_cap,omitempty"`
	DaemonIdleMinutes *int `json:"daemon_idle_minutes,omitempty"`
}

// SettingsResult is the registry's file-level settings (S174, *proposed*):
// the stored values (0 means unset) and the values the daemon uses.
type SettingsResult struct {
	GlobalCap                  int `json:"global_cap"`
	DaemonIdleMinutes          int `json:"daemon_idle_minutes"`
	EffectiveGlobalCap         int `json:"effective_global_cap"`
	EffectiveDaemonIdleMinutes int `json:"effective_daemon_idle_minutes"`
}

// ProjectsResult is the registered projects, in registry order.
type ProjectsResult struct {
	Unavailable []ProjectStatus    `json:"unavailable,omitempty"` // proposed: display-only groups
	Projects    []registry.Project `json:"projects"`
}

// SubscribeParams names the projects this connection shows. Their items, deps,
// runs and open records refresh every 5 s, the others every 30 s. Opening a project reads its
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

// ItemShowParams selects one item's detail. Proposed wire fields.
type ItemShowParams struct {
	Project string `json:"project"`
	ID      string `json:"id"`
}

// ItemShowResult wraps slicer's detail. Proposed wire field.
type ItemShowResult struct {
	Detail *slicercli.Detail `json:"detail"`
}

// ItemNoteParams adds owner text to a registered project's item. Proposed wire fields.
type ItemNoteParams struct {
	Project string `json:"project"`
	Item    string `json:"item"`
	Text    string `json:"text"`
}

// ItemNoteResult names the item noted. Proposed wire field.
type ItemNoteResult struct {
	Item string `json:"item"`
}

// WaveSuggestParams names the project whose plan-parallel waves are read.
// Proposed wire field.
type WaveSuggestParams struct {
	Project string `json:"project"`
}

// WaveOverlapParams names the project and the items whose shared paths are
// read. Items needs at least two. Proposed wire fields.
type WaveOverlapParams struct {
	Project string   `json:"project"`
	Items   []string `json:"items"`
}
