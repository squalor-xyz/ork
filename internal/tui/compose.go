// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type composeState struct {
	kind, project, item string
	input               textField
	request             uint64
	pending             bool
}

type noteMsg struct {
	project, item string
	request       uint64
	err           error
}

func (m *Model) noteRefusal() (string, bool) {
	switch {
	case m.view != roadmapView:
		return "Notes are available on the Roadmap", true
	case m.active < 0:
		return "Select a project to add a note", true
	case m.showHelp:
		return "Close help to add a note", true
	case !m.projects[m.active].loaded || m.projects[m.active].snap.Unavailable:
		return "Project unavailable; cannot add a note", true
	case m.matrixPaneOpen():
		return "", true
	case m.cfg.Live == nil:
		return "No daemon connection; action unavailable", true
	}
	if _, ok := m.cfg.Live.(itemNoter); !ok {
		return "Note action unavailable", true
	}
	if m.itemPane == nil && len(m.roadmapItems()) == 0 {
		return "Select a Roadmap item to add a note", true
	}
	return "", false
}

func (m *Model) openNote() tea.Cmd {
	if status, refused := m.noteRefusal(); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	project := m.projects[m.active].reg.Name
	var item string
	if m.itemPane != nil {
		item = m.itemPane.id
	} else {
		items := m.roadmapItems()
		item = items[clampCursor(m.roadmapCursor, len(items))].ID
	}
	m.composeRequest++
	m.compose = &composeState{kind: "note", project: project, item: item, request: m.composeRequest}
	m.actionStatus = ""
	return nil
}

func (m *Model) composeKey(msg tea.KeyPressMsg) tea.Cmd {
	c := m.compose
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.compose = nil
		m.actionStatus = "Cancelled"
		if c.pending {
			m.actionStatus = "Closed"
		}
		return nil
	}
	if c.pending {
		return nil
	}
	m.actionStatus = ""
	if msg.String() != "enter" {
		c.input.update(msg)
		return nil
	}
	text := c.input.Value()
	if strings.TrimSpace(text) == "" {
		m.actionStatus = "Note must not be empty"
		return nil
	}
	c.pending = true
	noter := m.cfg.Live.(itemNoter)
	project, item, request := c.project, c.item, c.request
	return func() tea.Msg {
		err := noter.ItemNote(context.Background(), project, item, text)
		return noteMsg{project: project, item: item, request: request, err: err}
	}
}

func (m *Model) applyNote(msg noteMsg) tea.Cmd {
	if msg.err != nil {
		m.actionStatus = actionError(msg.err)
	} else {
		m.actionStatus = "Noted " + msg.item
	}
	c := m.compose
	if c == nil || c.request != msg.request {
		return nil
	}
	if msg.err != nil {
		c.pending = false
		return nil
	}
	m.compose = nil
	cmds := []tea.Cmd{m.fetch(msg.project, true)}
	if pane := m.itemPane; pane != nil && pane.project == msg.project && pane.id == msg.item && m.view == roadmapView && m.active >= 0 && m.projects[m.active].reg.Name == msg.project {
		m.itemRequest++
		pane.request = m.itemRequest
		pane.pending = true
		live, request := m.cfg.Live, pane.request
		cmds = append(cmds, func() tea.Msg {
			detail, err := live.ItemShow(context.Background(), msg.project, msg.item)
			return itemShowMsg{project: msg.project, id: msg.item, request: request, detail: detail, err: err}
		})
	}
	return tea.Batch(cmds...)
}
