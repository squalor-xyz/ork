// SPDX-License-Identifier: GPL-3.0-or-later

// Package engine runs one slice through ork's state machine: setup,
// implement, checks, review, gate, awaiting approval, commit and the finish
// step, with the slicer ledger and a run record (docs/design.md). It starts
// roles through the Runner interface and calls slicer and git through their
// CLIs, so it can be tested with no vendor and no real CLI.
package engine

import "context"

// Role names.
const (
	RoleImplementer = "implementer"
	RoleReviewer    = "reviewer"
	RoleGate        = "gate"
)

// ExitClass is how a vendor adapter classifies a role process's exit
// (docs/design.md, Vendors).
type ExitClass string

const (
	ExitOK          ExitClass = "ok"
	ExitRateLimited ExitClass = "rate_limited"
	ExitAuth        ExitClass = "auth"
	ExitError       ExitClass = "error"
)

// Request is one role round for a Runner. It names no vendor mechanics: the
// adapter turns Vendor, Model and Effort, Allowlist or ReadOnly, and Resume
// into its own command line.
type Request struct {
	Role       string
	Round      int
	Vendor     string
	Model      string
	Effort     string
	Brief      string   // the whole prompt for the role
	Worktree   string   // the role's working directory
	ReportPath string   // where the role writes its final report
	Allowlist  []string // tools the role may use; empty when ReadOnly
	ReadOnly   bool     // run in the vendor's read-only mode
	Resume     string   // a session id to resume; empty starts a fresh session
	// OnStart is called once, with the role process's group id, as soon as
	// the process runs. The engine records it so a restart can find the
	// process (engine.restart).
	OnStart func(pgid int)
}

// Result is what a role round produced.
type Result struct {
	Exit    ExitClass
	Session string   // the vendor session id, for a later resume
	Report  string   // the final result text; the report file takes precedence
	Tokens  int      // total tokens used
	CostUSD float64  // cost, when the vendor reports one
	Denials []string // permission denials the vendor reported
}

// Runner runs one role round to its end. A non-nil error means the round did
// not run to an exit; the engine treats it as an exit of class error.
type Runner interface {
	Run(ctx context.Context, req Request) (Result, error)
}
