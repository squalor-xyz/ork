// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// The outcomes of one member in a wave.approve result (S222, *proposed*).
const (
	WaveApproveLanded       = "landed"        // approved, and the slice landed on the base branch
	WaveApproveCommitted    = "committed"     // approved, and the run ended committed without a landing (stop_after commit)
	WaveApproveFailed       = "failed"        // approved, and the run did not end committed; the loop stopped here
	WaveApproveNotAttempted = "not-attempted" // waits for approval; the loop stopped before it
	WaveApproveNotWaiting   = "not-waiting"   // not awaiting approval, left alone
	WaveApproveDone         = "done"          // already committed
)

// WaveApproveItem is one run of the wave in a wave.approve result.
type WaveApproveItem struct {
	Item    string     `json:"item"`
	Run     string     `json:"run"`
	Outcome string     `json:"outcome"`
	Error   *ipc.Error `json:"error,omitempty"`
	State   string     `json:"state"`
}

// WaveApproveResult is the proposed result of wave.approve (S222): every run of
// the wave in start order, with what the call did to it.
type WaveApproveResult struct {
	Wave  string            `json:"wave"`
	Items []WaveApproveItem `json:"items"`
}

// approveWave approves each run of the wave that awaits approval, one after the
// other in start order (created_at, then run id), through the same path as
// run.approve, which returns once the run rests again. It stops at the first
// run that does not end committed; the runs after it are reported
// not-attempted. It never marks a human check: when any waiting run has an open
// check, the whole call is refused before anything is approved (S222,
// *proposed*; owner decision 2026-11-07, AW-Q1 to AW-Q4).
func (d *Daemon) approveWave(ctx context.Context, p WaveAction) (*WaveApproveResult, error) {
	if !strings.HasPrefix(p.Wave, "wave-") {
		return nil, badParams(errors.New("wave id needs wave- prefix"))
	}
	members, err := d.waveMembers(p.Wave)
	if err != nil {
		return nil, err
	}
	result := &WaveApproveResult{Wave: p.Wave, Items: make([]WaveApproveItem, len(members))}
	var waiting []int // indexes into members
	var open []string
	for i, rec := range members {
		item := WaveApproveItem{Item: rec.Item, Run: rec.ID, State: rec.State, Outcome: WaveApproveNotWaiting}
		switch {
		case rec.State == runrec.StateAwaitingApproval:
			waiting = append(waiting, i)
			item.Outcome = WaveApproveNotAttempted
			if checks := openHumanChecks(rec.Card); len(checks) > 0 {
				open = append(open, fmt.Sprintf("run %s (%s): %s", rec.ID, rec.Item, strings.Join(checks, "; ")))
			}
		case rec.State == runrec.StateCommitted:
			item.Outcome = WaveApproveDone
		}
		result.Items[i] = item
	}
	if len(waiting) == 0 {
		return nil, &ipc.Error{Code: CodeRefused, Message: "no run of " + p.Wave + " awaits approval"}
	}
	if len(open) > 0 {
		return nil, &ipc.Error{Code: CodeRefused, Message: "human checks are open, so nothing was approved; tick each or mark it later: " + strings.Join(open, " | ")}
	}
	for _, i := range waiting {
		if ctx.Err() != nil {
			break
		}
		item := &result.Items[i]
		reply, err := d.ownerRunReply(ctx, MethodRunApprove, RunAction{Run: item.Run})
		item.State = reply.State
		switch {
		case err != nil:
			// A refusal or a failed step: the run's own state says where it rests.
			item.Outcome = WaveApproveFailed
			item.Error = ipc.ErrorOf(err)
			if rec, ok := d.waveMember(item.Run); ok {
				item.State = rec.State
			}
		case reply.State != runrec.StateCommitted:
			item.Outcome = WaveApproveFailed
		case reply.LandedAt.IsZero():
			item.Outcome = WaveApproveCommitted
		default:
			item.Outcome = WaveApproveLanded
		}
		if item.Outcome == WaveApproveFailed {
			break
		}
	}
	return result, nil
}

// waveMembers lists the runs of a wave in start order, from the live engines
// where there are some and the run store otherwise, as stopWave reads them.
func (d *Daemon) waveMembers(wave string) ([]runrec.Record, error) {
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		return nil, err
	}
	var members []runrec.Record
	for _, run := range stored {
		rec := run.Snapshot()
		if r := d.runs.runs[rec.ID]; r != nil && r.engine != nil {
			rec = r.engine.Snapshot()
		}
		if rec.Wave == wave {
			members = append(members, rec)
		}
	}
	if len(members) == 0 {
		return nil, &ipc.Error{Code: CodeNotFound, Message: "no wave " + wave}
	}
	slices.SortStableFunc(members, func(a, b runrec.Record) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return members, nil
}

// waveMember is the current record of one run.
func (d *Daemon) waveMember(id string) (runrec.Record, bool) {
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	if r := d.runs.runs[id]; r != nil && r.engine != nil {
		return r.engine.Snapshot(), true
	}
	run, err := d.runs.deps.Store.Open(id)
	if err != nil {
		return runrec.Record{}, false
	}
	return run.Snapshot(), true
}

// openHumanChecks lists the human checks the owner has neither ticked nor
// marked later, as "N. text" (the engine's approval refuses on the same list).
func openHumanChecks(card *runrec.ApprovalCard) []string {
	if card == nil {
		return nil
	}
	var open []string
	for i, c := range card.HumanChecks {
		if !c.Resolved() {
			open = append(open, fmt.Sprintf("%d. %s", i+1, c.Text))
		}
	}
	return open
}
