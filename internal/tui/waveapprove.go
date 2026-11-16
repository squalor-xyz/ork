// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

// S222 (*proposed*): A approves every run of the selected run's wave that
// waits for approval, one after the other in start order. The daemon stops at
// the first run that does not land and never marks a human check.

// waveApprover is the part of the daemon connection that approves a wave. A
// test double may not have it; the call then says the connection cannot.
type waveApprover interface {
	WaveApprove(ctx context.Context, wave string) (daemon.WaveApproveResult, error)
}

var _ waveApprover = (*Daemon)(nil)

// waveRuns are the engine runs of the project that share the wave of run, in
// start order (created_at, then id), and those of them that await approval. It
// returns nothing for a run that has no wave.
func (m *Model) waveRuns(project, run string) (members, waiting []source.EngineRun) {
	p := m.byName[project]
	if p == nil {
		return nil, nil
	}
	wave := ""
	for _, r := range p.snap.EngineRuns {
		if r.ID == run {
			wave = r.Wave
		}
	}
	if wave == "" {
		return nil, nil
	}
	for _, r := range p.snap.EngineRuns {
		if r.Wave == wave && !r.Dismissed {
			members = append(members, r)
		}
	}
	slices.SortStableFunc(members, func(a, b source.EngineRun) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	for _, r := range members {
		if r.State == runrec.StateAwaitingApproval && !r.Unavailable {
			waiting = append(waiting, r)
		}
	}
	return members, waiting
}

// waveApproveRefusal is why the A key does nothing on the row, or refused
// false when it takes the key. The key is for a wave with two or more runs
// waiting; with fewer it is silent, since a approves the one run. A wave with
// a human check open is refused whole, naming each run and the numbers of its
// open checks (the daemon refuses the same call and approves nothing).
func (m *Model) waveApproveRefusal(r ovRow) (status string, refused bool) {
	members, waiting := m.waveRuns(r.name, r.engineID)
	if len(waiting) < 2 {
		return "", true
	}
	for _, run := range members {
		for _, a := range m.actionFlights[run.ID] {
			if a.verb == "approve" || a.verb == "approve-wave" {
				return a.command() + " is pending", true
			}
		}
	}
	var open []string
	for _, run := range waiting {
		if run.Card == nil {
			continue
		}
		var numbers []string
		for i, c := range run.Card.HumanChecks {
			if !c.Resolved() {
				numbers = append(numbers, strconv.Itoa(i+1))
			}
		}
		if len(numbers) > 0 {
			open = append(open, fmt.Sprintf("%s (%s): open %s", waveItemName(run), run.ID, strings.Join(numbers, ", ")))
		}
	}
	if len(open) > 0 {
		return "Approve wave needs every human check marked; " + strings.Join(open, "; ") + " (enter on a run's row opens its card)", true
	}
	return "", false
}

// waveCallPending is the status that names a wave approval in flight on any run
// of the row's wave, or "" when none is. The flight is stored under the run that
// was selected when the owner pressed the key.
func (m *Model) waveCallPending(r ovRow) string {
	members, _ := m.waveRuns(r.name, r.engineID)
	for _, run := range members {
		for _, a := range m.actionFlights[run.ID] {
			if a.verb == "approve-wave" {
				return a.command() + " is pending"
			}
		}
	}
	return ""
}

// waveItemName is the run's item, or its id when the row has none.
func waveItemName(r source.EngineRun) string {
	if r.Item != "" {
		return r.Item
	}
	return r.ID
}

// fillWave names the wave and the waiting runs, in start order, on the confirm.
// A run set to stop after its commit is named so: its approval commits and
// does not land.
func (m *Model) fillWave(a *runAction, r ovRow) {
	members, waiting := m.waveRuns(r.name, r.engineID)
	if len(members) == 0 {
		return
	}
	a.wave = members[0].Wave
	a.push = m.projects[r.project].reg.Push
	for _, run := range waiting {
		a.waveItems = append(a.waveItems, waveItemName(run))
		a.waveCommit = append(a.waveCommit, run.StopAfter == runrec.StopAfterCommit)
	}
}

// waveQuestion is the confirm's question and note for the runs it names. A
// run lands (and is pushed when the project pushes) and then loses its worktree
// and branch, or, set to stop after its commit, is committed on its branch and
// keeps its worktree. The wave stops at the first run that does not end
// committed. The question reads `Approve and land A, then B?` when every run
// lands.
func (a runAction) waveQuestion() (note, question string) {
	land := "land"
	if a.push {
		land = "land and push"
	}
	var parts []string
	lands, commits := 0, 0
	afterLand := false
	for i, name := range a.waveItems {
		if i < len(a.waveCommit) && a.waveCommit[i] {
			commits++
			afterLand = false
			parts = append(parts, "commit "+name+" on its branch")
			continue
		}
		lands++
		if afterLand {
			parts = append(parts, name)
		} else {
			parts = append(parts, land+" "+name)
		}
		afterLand = true
	}
	switch {
	case lands > 0:
		note = "Each run lands in turn"
		if a.push {
			note += " and is pushed"
		}
		note += ", then its worktree and branch are removed."
		if commits > 0 {
			note += " A commit-only run is committed on its branch; its worktree stays."
		}
	default:
		note = "Each run is committed on its branch in turn; its worktree stays."
	}
	note += " The first run that does not end committed stops the rest."
	return note, "Approve and " + strings.Join(parts, ", then ") + "?"
}

// waveDoneText is the status line when the call returned: what landed, what
// needs a fix round or stopped the wave, and what was left unapproved.
func (a runAction) waveDoneText(err error) string {
	if err != nil {
		return a.command() + ": " + actionError(err)
	}
	if a.waveResult == nil {
		return a.command() + ": done"
	}
	var landed, committed, notApproved, failed []string
	for _, it := range a.waveResult.Items {
		switch it.Outcome {
		case daemon.WaveApproveLanded:
			landed = append(landed, it.Item)
		case daemon.WaveApproveCommitted:
			committed = append(committed, it.Item)
		case daemon.WaveApproveNotAttempted:
			notApproved = append(notApproved, it.Item)
		case daemon.WaveApproveFailed:
			switch {
			case it.Error != nil:
				failed = append(failed, it.Item+" failed: "+actionError(it.Error))
			case it.State == runrec.StateAwaitingApproval:
				failed = append(failed, it.Item+" needs a fix round")
			default:
				failed = append(failed, it.Item+" is "+stateWord(it.State))
			}
		}
	}
	var parts []string
	if len(landed) > 0 {
		parts = append(parts, "landed "+strings.Join(landed, ", "))
	}
	if len(committed) > 0 {
		parts = append(parts, "committed "+strings.Join(committed, ", "))
	}
	parts = append(parts, failed...)
	if len(notApproved) > 0 {
		parts = append(parts, strings.Join(notApproved, ", ")+" not approved")
	}
	if len(parts) == 0 {
		return a.command() + ": no run was approved"
	}
	return strings.Join(parts, "; ")
}
