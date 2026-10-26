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
	StateArchitect        = "architect"
	StateGate             = "gate"
	StateAwaitingApproval = "awaiting-approval"
	StateCommit           = "commit" // the finish step is done; ork commits next
	StateFinish           = "finish" // approved; the implementer runs the finish step
	StateLand             = "land"   // committed; ork merges the base branch in, re-checks, merges, pushes if set and cleans up (S119)
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

// OwnerQuestion is the structured form of an Owner needed block.
type OwnerQuestion struct {
	Title     string   `json:"title"`
	Options   []Option `json:"options"`
	Recommend int      `json:"recommend"` // one-based option number
	Reason    string   `json:"reason"`
}

// Option is one answer the owner can pick.
type Option struct {
	Label   string `json:"label"`
	Summary string `json:"summary"`
}

// Question is an owner question taken from a role report, and its answer.
type Question struct {
	Ask        *OwnerQuestion `json:"ask,omitempty"`
	Key        string         `json:"key,omitempty"`
	Role       string         `json:"role"`
	Round      int            `json:"round"`
	Text       string         `json:"text"`
	At         time.Time      `json:"at"`
	Answer     string         `json:"answer,omitempty"`
	AnsweredAt time.Time      `json:"answered_at,omitzero"`
}

// Commit is the commit ork made on the slice branch.
type Commit struct {
	SHA   string    `json:"sha"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

// Pending kinds: why a slice is paused.
const (
	PendingBudget      = "budget"      // a run limit reached before the next role round
	PendingQuestion    = "question"    // a report's Owner needed section
	PendingShape       = "shape"       // a report failed the shape checks
	PendingLost        = "lost"        // the role's process is gone (engine.restart)
	PendingInterrupted = "interrupted" // ork stopped between roles
	PendingError       = "error"       // a tool call failed and the engine cannot go on
)

// Pending says why a slice is paused and what resumes it. State is the state
// to re-enter; Role and Round name the role to run again.
type Pending struct {
	Ask      *OwnerQuestion `json:"ask,omitempty"`
	Key      string         `json:"key,omitempty"`
	Kind     string         `json:"kind"`
	State    string         `json:"state"`
	Role     string         `json:"role,omitempty"`
	Round    int            `json:"round,omitempty"`
	Reason   string         `json:"reason"`
	Question string         `json:"question,omitempty"`
	Answer   string         `json:"answer,omitempty"`
}

// Feedback names the file that holds what the next implementer round must
// fix: failing check output, reviewer or architect findings, a gate FAIL
// report, or the owner's note from the approval card. The file is in the run
// directory.
type Feedback struct {
	Kind string `json:"kind"` // "checks", "findings", "architect", "fail", "owner" or "land"
	File string `json:"file"`
}

// Human check states on the approval card. A check starts open (empty).
const (
	CheckTicked = "ticked" // the owner did the check
	CheckLater  = "later"  // the owner marked it to do later
)

// HumanCheck is one [HUMAN CHECK] item of the slice, with the owner's mark.
type HumanCheck struct {
	Text  string `json:"text"`
	State string `json:"state,omitempty"` // "", CheckTicked or CheckLater
}

// Resolved reports whether the owner ticked the check or marked it later.
func (c HumanCheck) Resolved() bool { return c.State == CheckTicked || c.State == CheckLater }

// ApprovalCard is what the owner needs to approve a slice on one screen
// (S118, *proposed*): the gate verdict, its test counts, the diff stat and the
// slice's human checks. The engine builds it when the gate passes. Owner
// findings are in Record.Findings.
type ApprovalCard struct {
	Round       int          `json:"round"`
	Verdict     string       `json:"verdict"`
	Passed      int          `json:"passed"`
	Failed      int          `json:"failed"`
	DiffStat    string       `json:"diff_stat,omitempty"` // git diff --stat against HEAD
	Untracked   []string     `json:"untracked,omitempty"` // new files the diff stat does not count
	HumanChecks []HumanCheck `json:"human_checks,omitempty"`
	ChecksError string       `json:"checks_error,omitempty"` // why the slice's human checks could not be read

	// The UI pass's renders (S121, *proposed*; adj 165): the changed views as
	// plain text files, listed by name and size. UIRound is the round of the UI pass they come from, or 0
	// when no UI pass ran for the slice. RendersDropped counts files the card
	// did not keep.
	UIRound        int      `json:"ui_round,omitempty"`
	Renders        []Render `json:"renders,omitempty"`
	RendersDropped int      `json:"renders_dropped,omitempty"`
}

// Render is one view the UI pass wrote as a text file, for example
// "launch-80x24.txt": its name and the size in bytes of its plain text. The
// text stays in the file and is read on demand (the daemon's run.render), so
// no record or snapshot carries it. ork does not make renders.
type Render struct {
	Name string `json:"name"`
	Size int    `json:"size"`
}

// OwnerFix is a fix the owner asked for from the approval card, with the note
// that went to the fix round.
type OwnerFix struct {
	Round int       `json:"round"` // the implementer round that acts on the note
	Note  string    `json:"note"`
	At    time.Time `json:"at"`
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
	Decisions        string                   `json:"decisions,omitempty"` // the owner's spec check answers, for every brief (S116)
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
	LandFixes        int                      `json:"land_fixes,omitempty"`   // fix rounds that a blocked landing has had (S119)
	LandedAt         time.Time                `json:"landed_at,omitzero"`     // when ork landed the slice on the base branch (S119)
	LandRecheck      bool                     `json:"land_recheck,omitempty"` // the base branch is merged into the slice branch and no green re-check has followed (S119)
	CleanedAt        time.Time                `json:"cleaned_at,omitzero"`    // when the worktree and branch were removed (S95)
	Reason           string                   `json:"reason,omitempty"`
	Pending          *Pending                 `json:"pending,omitempty"`
	Feedback         *Feedback                `json:"feedback,omitempty"`
	Lineup           map[string][]LineupEntry `json:"lineup,omitempty"`
	Roles            []RoleRecord             `json:"roles"`
	Checks           []CheckRecord            `json:"checks"`
	Budgets          registry.Budgets         `json:"budgets,omitzero"`
	Approvals        []Approval               `json:"approvals"`
	Questions        []Question               `json:"questions"`
	Findings         []Finding                `json:"findings,omitempty"`
	Card             *ApprovalCard            `json:"card,omitempty"`
	Fixes            []OwnerFix               `json:"fixes,omitempty"`
	Writes           []Write                  `json:"writes"`
	Commit           *Commit                  `json:"commit"`
	DismissedAt      time.Time                `json:"dismissed_at,omitzero"`
	CreatedAt        time.Time                `json:"created_at"`
	WaitStartedAt    time.Time                `json:"wait_started_at,omitzero"` // entry into awaiting-approval or paused (S138)
	UpdatedAt        time.Time                `json:"updated_at"`
}

// Cleaned reports whether the run has no worktree or branch left to clean up
// (S95). The cleanup key sets CleanedAt, also for a run that was landed with
// `slicer land`, which leaves LandedAt zero. A run that ork landed is cleaned
// when its own cleanup left no reason behind.
func (r Record) Cleaned() bool {
	return !r.CleanedAt.IsZero() || (!r.LandedAt.IsZero() && r.State == StateCommitted && r.Reason == "")
}

// MarkCleaned sets the cleanup time and drops the reason a landed run kept for
// a failed cleanup.
func MarkCleaned(r *Record, at time.Time) {
	r.CleanedAt = at
	if !r.LandedAt.IsZero() && r.State == StateCommitted {
		r.Reason = ""
	}
}

// Finding kinds that ork keeps in the record (adj 163, review.finding-kinds).
// A fix finding goes to the fix round and is not kept.
const (
	FindingOwner = "owner" // an owner question that does not block
	FindingLater = "later" // later work offered to the owner as a new slice
)

// Finding is a review finding that is not a fix. An owner finding holds its
// structured question in Ask; a later finding waits for the owner to file it
// as a slice, and Filed holds the new slice's id once ork has.
type Finding struct {
	Kind    string         `json:"kind"`
	Role    string         `json:"role"`
	Round   int            `json:"round"`
	Title   string         `json:"title"`
	Text    string         `json:"text"`
	Ask     *OwnerQuestion `json:"ask,omitempty"`
	Key     string         `json:"key,omitempty"`
	At      time.Time      `json:"at"`
	Filed   string         `json:"filed,omitempty"`
	FiledAt time.Time      `json:"filed_at,omitzero"`
}
