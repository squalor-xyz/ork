// SPDX-License-Identifier: GPL-3.0-or-later

// Package engine runs one slice through ork's state machine: setup,
// implement, checks, review, architect, gate, awaiting approval, commit and
// the finish step, with the slicer ledger and a run record (docs/design.md). It starts
// roles through the Runner interface and calls slicer and git through their
// CLIs, so it can be tested with no vendor and no real CLI.
package engine

import (
	"context"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// Role names.
const (
	RoleImplementer = "implementer"
	// RoleLead is the lead of a split run (S61, *proposed*). It is the
	// implementer's slot in the state machine (same lineup chain, records, fix
	// rounds and finish step) and differs in its prompt; the ledger records it as
	// an implementer, because slicer's contract has no lead role.
	RoleLead = "lead"
	// RolePart is one part of a split run (S61, *proposed*). Parts run in the
	// slice worktree, change only their own files and run no slicer write.
	RolePart      = "part"
	RoleReviewer  = "reviewer"
	RoleArchitect = "architect"
	RoleGate      = "gate"
	RoleUI        = "ui"         // the UI pass, beside the reviewer on a slice that touches the UI
	RoleSpecCheck = "spec-check" // the batch readiness check before a wave (adj 164)
)

// ExitClass is how a vendor adapter classifies a role process's exit
// (docs/design.md, Vendors).
type ExitClass string

const (
	ExitOK           ExitClass = "ok"
	ExitOutOfCredits ExitClass = "out_of_credits"
	ExitRateLimited  ExitClass = "rate_limited"
	ExitAuth         ExitClass = "auth"
	ExitError        ExitClass = "error"
	ExitConnection   ExitClass = "connection"
	ExitUnclassified ExitClass = "unclassified"
)

// Request is one role round for a Runner. It names no vendor mechanics: the
// adapter turns Vendor, Model and Effort, Allowlist or ReadOnly, and Resume
// into its own command line.
type Request struct {
	SpecProject  string    // active spec-check project, before an ork run exists
	SpecItems    []string  // items in this spec-check sitting
	RunID        string    // ork run identity for live local permission questions
	ProjectRoot  string    // main checkout, for spec-check path permissions
	ProgressFile string    // sole progress-note write exception for a local role
	ReadLog      string    // file the approval point appends the role's Read and Grep calls to (S371); empty for a cloud role
	OnContext    func(int) // current context input tokens, including cached input
	Role         string
	Part         string // the split part's id when Role is RolePart (S61, *proposed*)
	Round        int
	Vendor       string
	Model        string
	Effort       string
	Brief        string   // the whole prompt for the role
	TempDir      string   // the role's own temp directory
	Worktree     string   // the role's working directory
	ReportPath   string   // where the role writes its final report
	Allowlist    []string // tools the role may use; empty when ReadOnly
	ReadOnly     bool     // run in the vendor's read-only mode
	Resume       string   // a session id to resume; empty starts a fresh session
	// OnStart is called once, with the role process's group id, as soon as
	// the process runs. The engine records it so a restart can find the
	// process (engine.restart).
	OnStart func(pgid int)
	// OnEvent counts one vendor stream event without exposing vendor payloads.
	OnEvent func()
	// OnLive carries assistant text and token increments for this role attempt.
	OnLive func(Live)
}

// Live is transient role output. Tokens is an increment, not a cumulative count.
type Live struct {
	UsageKnown    bool // a usage event arrived, including a reported zero
	TailTruncated bool // older text exceeded the memory cap
	Text          string
	Tokens        int
}

// Result is what a role round produced.
type Result struct {
	Reason          string // classified vendor failure message, when available
	Exit            ExitClass
	Session         string                 // the vendor session id, for a later resume
	Report          string                 // the final result text; the report file takes precedence
	Tokens          int                    // total tokens used
	CostUSD         float64                // cost, when the vendor reports one
	Denials         []runrec.DeniedCommand // permission denials, one per denied call
	ApprovalDenials int                    // denials from ork's approval point, included in Denials; not persisted
}

// Runner runs one role round to its end. A non-nil error means the round did
// not run to an exit; the engine treats it as an exit of class error.
type Runner interface {
	Run(ctx context.Context, req Request) (Result, error)
}
