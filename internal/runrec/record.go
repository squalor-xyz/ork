// SPDX-License-Identifier: GPL-3.0-or-later

// Package runrec reads and writes ork's run records: one run.json per slice
// run under ~/.local/state/ork/runs/<run>/ (docs/state.md). Writes are atomic.
// The record also holds writes[], every slicer, adj and git write ork made
// for the run (state.writes). Field names beyond the example in
// docs/state.md are *proposed*.
package runrec

import (
	"time"

	"github.com/squalor-xyz/ork/internal/registry"
)

// Version is the run.json version this package writes and reads.
const Version = 1

// The slice states. paused and stopped are the two rests that need the owner;
// awaiting-approval is checkpoint 2.
const (
	StateSetup            = "setup"
	StateImplement        = "implement"
	StateChecks           = "checks"
	StateReview           = "review"
	StateGate             = "gate"
	StateAwaitingApproval = "awaiting-approval"
	StateCommit           = "commit" // approved; ork commits next
	StateFinish           = "finish" // committed; the implementer runs the finish step
	StateCommitted        = "committed"
	StatePaused           = "paused"
	StateStopped          = "stopped"
	StateQueued           = "queued" // waits for a slot under the caps; not final
)

// IsFinal reports whether a run in this state is finished.
func IsFinal(state string) bool { return state == StateCommitted || state == StateStopped }

// LineupEntry is one vendor, model and effort for a role. The first entry of
// a role's list is the primary; the rest are its fallback chain (S06).
type LineupEntry struct {
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// RoleRecord is ork's record of one role process it started.
type RoleRecord struct {
	Role      string    `json:"role"`
	Round     int       `json:"round"`
	Vendor    string    `json:"vendor"`
	Model     string    `json:"model,omitempty"`
	Effort    string    `json:"effort,omitempty"`
	Session   string    `json:"session,omitempty"`
	PGID      int       `json:"pgid,omitempty"`
	Step      string    `json:"step,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
	Exit      string    `json:"exit,omitempty"`
	Tokens    int       `json:"tokens,omitempty"`
	CostUSD   float64   `json:"cost_usd,omitempty"`
	Report    string    `json:"report,omitempty"`
	Denials   []string  `json:"denials,omitempty"`
	// DeniedCommands holds one entry per denial, in the same order as Denials.
	DeniedCommands []DeniedCommand `json:"denied_commands,omitempty"`
}

// Running reports whether the role was started and has no end recorded.
func (r RoleRecord) Running() bool { return r.EndedAt.IsZero() }

// DeniedCommand is one tool call the vendor refused. Command is the Bash
// command, or the file path of a file tool, capped at 200 characters
// (claude.Denial.Detail). It can hold secrets (docs/state.md).
type DeniedCommand struct {
	Tool    string `json:"tool"`
	Command string `json:"command,omitempty"`
}

// Text is the line status and the TUI show for a denial: "Tool: command", or
// the bare tool name when the call had no detail.
func (d DeniedCommand) Text() string {
	if d.Command == "" {
		return d.Tool
	}
	return d.Tool + ": " + d.Command
}

// CheckRecord is one run of the project's checks.
type CheckRecord struct {
	Round   int    `json:"round"`
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	Seconds int    `json:"seconds"`
	Log     string `json:"log,omitempty"`
}

// Approval is an owner approval.
type Approval struct {
	What string    `json:"what"`
	At   time.Time `json:"at"`
}

// Question is an owner question taken from a role report, and its answer.
type Question struct {
	Role       string    `json:"role"`
	Round      int       `json:"round"`
	Text       string    `json:"text"`
	At         time.Time `json:"at"`
	Answer     string    `json:"answer,omitempty"`
	AnsweredAt time.Time `json:"answered_at,omitzero"`
}

// Commit is the commit ork made on the slice branch.
type Commit struct {
	SHA   string    `json:"sha"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

// Pending kinds: why a slice is paused.
const (
	PendingQuestion    = "question"    // a report's Owner needed section
	PendingShape       = "shape"       // a report failed the shape checks
	PendingLost        = "lost"        // the role's process is gone (engine.restart)
	PendingInterrupted = "interrupted" // ork stopped between roles
	PendingError       = "error"       // a tool call failed and the engine cannot go on
)

// Pending says why a slice is paused and what resumes it. State is the state
// to re-enter; Role and Round name the role to run again.
type Pending struct {
	Kind     string `json:"kind"`
	State    string `json:"state"`
	Role     string `json:"role,omitempty"`
	Round    int    `json:"round,omitempty"`
	Reason   string `json:"reason"`
	Question string `json:"question,omitempty"`
	Answer   string `json:"answer,omitempty"`
}

// Feedback names the file that holds what the next implementer round must
// fix: failing check output, review findings or a gate FAIL report. The file
// is in the run directory.
type Feedback struct {
	Kind string `json:"kind"` // "checks", "findings" or "fail"
	File string `json:"file"`
}

// Write is one change ork made through a tool's CLI, or to git's info/exclude
// (state.writes). Retry is 0 for the first attempt and omitted; a locked
// attempt is retried with backoff and each retry is its own entry.
type Write struct {
	At    time.Time `json:"at"`
	Tool  string    `json:"tool"` // "slicer", "adj" or "git"
	Root  string    `json:"root"`
	Argv  []string  `json:"argv"`
	Exit  int       `json:"exit"`
	Code  string    `json:"code,omitempty"`  // the tool's error code, for example "locked"
	Error string    `json:"error,omitempty"` // a failure that is not an exit, for example a timeout
	Retry int       `json:"retry,omitempty"`
	MS    int64     `json:"ms"`
}

// Record is the content of run.json.
type Record struct {
	Version          int                      `json:"version"`
	ID               string                   `json:"id"`
	Wave             string                   `json:"wave,omitempty"`
	Project          string                   `json:"project"`
	Item             string                   `json:"item"`
	Worktree         string                   `json:"worktree"`
	Branch           string                   `json:"branch"`
	RelayRun         string                   `json:"relay_run,omitempty"`
	OrchestratorStep string                   `json:"orchestrator_step,omitempty"`
	Contract         int                      `json:"contract"`
	State            string                   `json:"state"`
	Round            int                      `json:"round"`
	Fails            int                      `json:"fails,omitempty"`
	Reason           string                   `json:"reason,omitempty"`
	Pending          *Pending                 `json:"pending,omitempty"`
	Feedback         *Feedback                `json:"feedback,omitempty"`
	Lineup           map[string][]LineupEntry `json:"lineup,omitempty"`
	Roles            []RoleRecord             `json:"roles"`
	Checks           []CheckRecord            `json:"checks"`
	Budgets          registry.Budgets         `json:"budgets,omitzero"`
	Approvals        []Approval               `json:"approvals"`
	Questions        []Question               `json:"questions"`
	Writes           []Write                  `json:"writes"`
	Commit           *Commit                  `json:"commit"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
}
