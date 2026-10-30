// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

type relayAnswerer interface {
	RelayAnswer(context.Context, daemon.RelayAnswerParams) (string, error)
}

var _ relayAnswerer = (*Daemon)(nil)

// externalQuestion keeps linked engine runs on their existing answer path.
func (m *Model) externalQuestion() *slicercli.LedgerQuestion {
	if m.pane == nil || !m.pane.ledger {
		return nil
	}
	p := m.byName[m.pane.project]
	if p == nil || p.snap.Unavailable {
		return nil
	}
	for _, r := range p.snap.EngineRuns {
		if r.RelayRun == m.pane.run {
			return nil
		}
	}
	return ledgerExternalQuestion(m.paneBoardRun())
}
func (m *Model) relayAnswerable() bool {
	_, ok := m.cfg.Live.(relayAnswerer)
	return ok && m.externalQuestion() != nil
}
func (m *Model) relayAnswerPending() bool {
	q := m.externalQuestion()
	if q == nil {
		return false
	}
	return len(m.actionFlights[(runAction{verb: "relay-answer", project: m.pane.project, run: m.pane.run, question: q.ID}).flightKey()]) > 0
}
func (m *Model) relayKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.relayInput != nil {
		switch msg.String() {
		case "ctrl+c":
			return tea.Quit
		case "esc":
			m.relayInput = nil
			m.actionStatus = "Cancelled"
		case "enter":
			text := m.relayInput.Value()
			if strings.TrimSpace(text) == "" {
				m.actionStatus = "Answer must not be empty"
				return nil
			}
			m.requestRelayAnswer("", text)
		default:
			m.relayInput.update(msg)
		}
		return nil
	}
	if !m.relayAnswerable() {
		return nil
	}
	if strings.TrimSpace(os.Getenv("ADJ_DECIDED_BY")) == "" {
		m.actionStatus = "ADJ_DECIDED_BY is unset; set it before answering in the ledger"
		return nil
	}
	if m.relayAnswerPending() {
		m.actionStatus = "Answer in the ledger is pending"
		return nil
	}
	if msg.String() == "t" {
		m.relayInput = &textField{}
		m.actionStatus = ""
		return nil
	}
	n, _ := strconv.Atoi(msg.String())
	q := m.externalQuestion()
	if n > 0 && n <= min(len(q.Options), 4) {
		m.requestRelayAnswer(q.Options[n-1].Label, "")
	}
	return nil
}
func (m *Model) requestRelayAnswer(option, text string) {
	q := m.externalQuestion()
	if q == nil {
		return
	}
	by := os.Getenv("ADJ_DECIDED_BY")
	if strings.TrimSpace(by) == "" {
		m.actionStatus = "ADJ_DECIDED_BY is unset; set it before answering in the ledger"
		return
	}
	a := runAction{verb: "relay-answer", project: m.pane.project, run: m.pane.run, question: q.ID, adjTitle: q.Title, option: option, text: text, by: by, adjRecommended: option != "" && option == q.Recommend}
	if p := m.byName[m.pane.project]; p.reg.Adj && q.Key != "" && p.snap.Open.Err == nil {
		for _, r := range p.snap.Open.Value {
			if r.Key == q.Key && r.Status == "open" {
				a.adjID = r.ID
				break
			}
		}
	}
	m.relayInput = nil
	m.confirm = &a
	m.actionStatus = ""
}
func relayFlightKey(a runAction) string {
	return fmt.Sprintf("relay:%s:%s:%s", a.project, a.run, a.question)
}
