// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

// Claim says who started an item.
type Claim struct {
	Owner string `json:"owner"`
	At    string `json:"at"`
}

// Fields are the scored fields of an item. A field the project does not use
// is the zero value. Flags holds the item's flags, among them any hold flag
// such as "needs-owner".
type Fields struct {
	Flags      []string `json:"flags"`
	Size       string   `json:"size"`
	Key        string   `json:"key"`
	Pass       string   `json:"pass"`
	Importance int      `json:"importance"`
	Urgency    int      `json:"urgency"`
	Effort     int      `json:"effort"`
	Attempts   int      `json:"attempts"`
}

// Item is one row of `slicer list --json --lean`.
type Item struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	ShortTitle string   `json:"short_title"`
	Status     string   `json:"status"`
	HasSlice   bool     `json:"has_slice"`
	DependsOn  []string `json:"depends_on"`
	Claim      *Claim   `json:"claim"`
	Fields     Fields   `json:"fields"`
	Notes      []string `json:"notes"`
}

// Section is one headed part of a slice spec.
type Section struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

// Spec is the slice spec inside `slicer show ID --json --lean`.
type Spec struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Lead        []string  `json:"lead"`
	Size        string    `json:"size"`
	TreesPlural bool      `json:"trees_plural"`
	Boundary    string    `json:"boundary"`
	Sections    []Section `json:"sections"`
}

// Detail is the output of `slicer show ID --json --lean`. Spec is nil for an
// item without a slice.
type Detail struct {
	Item
	Slice *Spec `json:"slice"`
}

// GoalRecord is one goal or non-goal.
type GoalRecord struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	Successor string `json:"successor"`
}

// Goals is the output of `slicer goals --json`.
type Goals struct {
	Goals    string `json:"goals"`
	NonGoals string `json:"non_goals"`
	Records  struct {
		Goal    []GoalRecord `json:"goal"`
		NonGoal []GoalRecord `json:"non_goal"`
	} `json:"records"`
}

// Step is one role or subagent step of a relay run.
type Step struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Agent     string `json:"agent"`
	Vendor    string `json:"vendor"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning"`
	Phase     string `json:"phase"`
	Task      string `json:"task"`
	Parent    string `json:"parent"`
	BeganAt   string `json:"begun_at"`
	UpdatedAt string `json:"updated_at"`
}

// Counts summarizes a run's steps.
type Counts struct {
	Steps     int            `json:"steps"`
	Running   int            `json:"running"`
	Subagents int            `json:"subagents"`
	Roles     map[string]int `json:"roles"`
}

// RelayRun is one run of `slicer relay-run list --json --lean`. Times are
// the RFC 3339 strings slicer prints.
type RelayRun struct {
	ID        string   `json:"id"`
	Items     []string `json:"items"`
	Worktree  string   `json:"worktree"`
	BeganAt   string   `json:"begun_at"`
	UpdatedAt string   `json:"updated_at"`
	State     string   `json:"state"`
	Outcome   string   `json:"outcome"`
	Counts    Counts   `json:"counts"`
	Steps     []Step   `json:"steps"`
}
