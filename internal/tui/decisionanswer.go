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
)

// adjAnswerer is optional for Live test doubles.
type adjAnswerer interface {
	AdjAnswer(context.Context, daemon.AdjAnswerParams) (string, error)
}

var _ adjAnswerer = (*Daemon)(nil)

// decisionAnswerable is shared by keys, numbering and hints. The owner's name
// is checked on the key so an unset name can explain its refusal.
func (m *Model) decisionAnswerable() bool {
	pane := m.decisionPane
	if pane == nil || pane.record == nil || pane.record.Status != "open" || m.cfg.Live == nil {
		return false
	}
	p := m.byName[pane.project]
	if p == nil || !p.reg.Adj {
		return false
	}
	_, ok := m.cfg.Live.(adjAnswerer)
	return ok
}

func (m *Model) decisionAnswerPending() bool {
	pane := m.decisionPane
	if pane == nil || pane.record == nil {
		return false
	}
	key := (runAction{verb: "decide", project: pane.project, adjID: pane.record.ID}).flightKey()
	return len(m.actionFlights[key]) > 0
}

func (m *Model) decisionKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.decisionInput != nil {
		switch msg.String() {
		case "ctrl+c":
			return tea.Quit
		case "esc":
			m.decisionInput = nil
			m.actionStatus = "Cancelled"
		case "enter":
			text := m.decisionInput.Value()
			if strings.TrimSpace(text) == "" {
				m.actionStatus = "Answer must not be empty"
				return nil
			}
			m.requestDecision("", text)
		default:
			m.decisionInput.update(msg)
		}
		return nil
	}
	k := msg.String()
	if k != "t" && k != "1" && k != "2" && k != "3" && k != "4" {
		return nil
	}
	if !m.decisionAnswerable() {
		return nil
	}
	if strings.TrimSpace(os.Getenv("ADJ_DECIDED_BY")) == "" {
		m.actionStatus = "ADJ_DECIDED_BY is unset; set it before answering in adj"
		return nil
	}
	pane := m.decisionPane
	if m.decisionAnswerPending() {
		m.actionStatus = "Answer in adj is pending"
		return nil
	}
	if k == "t" {
		m.decisionInput = &textField{}
		m.actionStatus = ""
		return nil
	}
	n, _ := strconv.Atoi(k)
	if n <= len(pane.record.Options) {
		m.requestDecision(pane.record.Options[n-1].Label, "")
	}
	return nil
}

func (m *Model) requestDecision(option, text string) {
	by := os.Getenv("ADJ_DECIDED_BY")
	if strings.TrimSpace(by) == "" {
		m.actionStatus = "ADJ_DECIDED_BY is unset; set it before answering in adj"
		return
	}
	pane := m.decisionPane
	a := runAction{verb: "decide", project: pane.project, adjID: pane.record.ID, option: option, text: text, by: by, adjTitle: pane.record.Title}
	if rec := pane.record.Recommendation; rec != nil && option != "" && rec.Label == option {
		a.adjRecommended = true
	}
	m.decisionInput = nil
	m.confirm = &a
	m.actionStatus = ""
}

func (a runAction) flightKey() string {
	if a.verb == "decide" {
		return fmt.Sprintf("adj:%s#%d", a.project, a.adjID)
	}
	return a.run
}
