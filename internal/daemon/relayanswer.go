// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// relayAnswer rechecks the ledger before writing an explicitly confirmed answer.
func (d *Daemon) relayAnswer(ctx context.Context, p RelayAnswerParams) (*RelayAnswerResult, error) {
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	refuse := func(message string) (*RelayAnswerResult, error) {
		return nil, &ipc.Error{Code: CodeRefused, Message: message}
	}
	if strings.TrimSpace(p.By) == "" {
		return refuse("ADJ_DECIDED_BY is unset; set it before answering in the ledger")
	}
	if (p.Option == "") == (strings.TrimSpace(p.Text) == "") {
		return refuse("give exactly one of option and non-empty text")
	}
	if p.Via != "tui" && p.Via != "cli" {
		return refuse("via must be cli or tui")
	}
	runs, err := d.runs.deps.Slicer.RelayRuns(ctx, proj.Path, false)
	if err != nil {
		return nil, err
	}
	var run *slicercli.RelayRun
	for i := range runs {
		if runs[i].ID == p.Run {
			run = &runs[i]
			break
		}
	}
	if run == nil {
		return refuse("no ledger run " + p.Run)
	}
	if run.State == "ended" || run.State == "stale" {
		return refuse("ledger run " + p.Run + " is " + run.State)
	}
	d.runs.mu.Lock()
	stored, err := d.runs.deps.Store.List()
	d.runs.mu.Unlock()
	if err != nil {
		return nil, err
	}
	for _, r := range stored {
		rec := r.Snapshot()
		if rec.Project == p.Project && rec.RelayRun == p.Run {
			return refuse("ledger run " + p.Run + " belongs to engine run " + rec.ID + "; answer through that run")
		}
	}
	var q *slicercli.LedgerQuestion
	for _, s := range run.Steps {
		if s.Question != nil && s.Question.ID == p.Question {
			q = s.Question
			break
		}
	}
	if q == nil {
		return refuse("no question " + p.Question + " on " + p.Run)
	}
	if q.AnsweredAt != "" || len(q.Answer) > 0 {
		return refuse("question " + p.Question + " is already answered")
	}
	if p.Option != "" {
		found := false
		for _, o := range q.Options {
			if o.Label == p.Option {
				found = true
			}
		}
		if !found {
			return refuse("question " + p.Question + " does not offer " + p.Option)
		}
	}
	if err := d.runs.deps.Slicer.RelayAnswer(ctx, runrec.Writer{}, proj.Path, p.Run, p.Question, slicercli.RelayAnswerSpec{Option: p.Option, Text: p.Text, By: p.By}); err != nil {
		return nil, err
	}
	message := "answered #" + p.Question + " on " + p.Run + " in the ledger"
	// A secondary failure must preserve the successful ledger result.
	if q.Key != "" && proj.Adj {
		state, id, checkErr := d.runs.deps.Adj.Check(ctx, runrec.Writer{}, proj.Path, q.Key)
		switch {
		case checkErr != nil:
			message += "; adj skipped: " + checkErr.Error()
		case state != "open":
			if id > 0 {
				message += fmt.Sprintf("; adj skipped: adj #%d is %s", id, state)
			} else {
				message += "; adj skipped: " + q.Key + " is " + state
			}
		case p.Adj != id:
			message += fmt.Sprintf("; adj skipped: confirm did not name #%d", id)
		default:
			notice, answerErr := engine.AnswerRecord(ctx, d.runDeps(proj), engine.AnswerRecordConfig{Project: proj, ID: id, Option: p.Option, Text: p.Text, By: p.By, Via: "tui"})
			if answerErr != nil {
				message += fmt.Sprintf("; adj #%d failed: %s", id, answerErr)
			} else {
				message += "; " + notice
			}
		}
	}
	if err := d.cache.Refresh(ctx, proj, source.Runs, source.Open, source.Decided); err != nil {
		message += "; refresh failed: " + err.Error()
	}
	return &RelayAnswerResult{Message: message}, nil
}
