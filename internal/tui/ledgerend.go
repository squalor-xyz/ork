// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"errors"
)

// ledgerEnder is the part of the daemon connection that ends a stale ledger
// run (run.end-ledger, S175, *proposed*). A test double may not have it.
type ledgerEnder interface {
	EndLedger(ctx context.Context, project, run string) error
}

var _ ledgerEnder = (*Daemon)(nil)

// ledgerEndCommand is the slicer command the end runs, as the prompt shows it.
// It breaks before the flags, so a flag never parts from its value.
func ledgerEndCommand(run string) string {
	return "slicer relay-run end " + run + "\n  --outcome stopped --close-steps fail"
}

// paneLedgerStale reports whether the open pane is a ledger run that slicer
// calls stale: no event for 30 minutes and not waiting on the owner. Only
// that pane ends its run; an active, waiting or ended run stays read-only.
func (m *Model) paneLedgerStale() bool {
	return m.pane != nil && m.pane.ledger && m.paneBoardRun().State == "stale"
}

// ledgerEndPending reports whether an end request for the pane's run is in
// progress, which hides the x hint as it hides x stop.
func (m *Model) ledgerEndPending() bool {
	for _, f := range m.actionFlights[m.pane.run] {
		if f.verb == "end" {
			return true
		}
	}
	return false
}

// requestLedgerEnd asks before ending the stale ledger run of the open pane.
// The daemon reads the ledger again and refuses a run that is no longer stale.
func (m *Model) requestLedgerEnd() {
	if !m.paneLedgerStale() {
		return
	}
	if m.cfg.Live == nil {
		m.actionStatus = "No daemon connection; action unavailable"
		return
	}
	a := runAction{verb: "end", run: m.pane.run, project: m.pane.project}
	for _, f := range m.actionFlights[a.run] {
		if f.verb == "end" {
			m.actionStatus = f.pendingText()
			return
		}
	}
	m.actionStatus = ""
	m.confirm = &a
}

// endLedger is the daemon call of the end verb.
func endLedger(live Live, a runAction) error {
	ender, ok := live.(ledgerEnder)
	if !ok {
		return errors.New("this daemon connection cannot end ledger runs")
	}
	return ender.EndLedger(context.Background(), a.project, a.run)
}
