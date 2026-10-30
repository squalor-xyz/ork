// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
)

const composeNew = "new"

type composeState struct {
	fields              [2]textField
	focus               int
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

func (m *Model) composeRefusal(kind string) (string, bool) {
	action := "add a note"
	if kind == composeNew {
		action = "file a slice"
	}
	switch {
	case m.view != roadmapView:
		if kind == composeNew {
			return "Slice filing is available on the Roadmap", true
		}
		return "Notes are available on the Roadmap", true
	case m.active < 0:
		return "Select a project to " + action, true
	case m.showHelp:
		return "Close help to " + action, true
	case !m.projects[m.active].loaded || m.projects[m.active].snap.Unavailable:
		return "Project unavailable; cannot " + action, true
	case m.matrixPaneOpen():
		return "", true
	case m.cfg.Live == nil:
		return "No daemon connection; action unavailable", true
	}
	if kind == composeNew {
		if _, ok := m.cfg.Live.(itemFiler); !ok {
			return "Slice filing unavailable", true
		}
		return "", false
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
	if status, refused := m.composeRefusal("note"); refused {
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
	if c.kind == composeNew {
		return m.newKey(msg)
	}
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

func (c *composeState) field() *textField {
	if c.kind == composeNew {
		return &c.fields[c.focus]
	}
	return &c.input
}

// itemFiler is optional for Live test doubles.
type itemFiler interface {
	ItemNew(context.Context, daemon.ItemNewParams) (daemon.ItemNewResult, error)
}

var _ itemFiler = (*Daemon)(nil)

type newSliceMsg struct {
	project string
	request uint64
	result  daemon.ItemNewResult
	err     error
}

func (m *Model) openNew() tea.Cmd {
	if status, refused := m.composeRefusal(composeNew); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	m.composeRequest++
	c := &composeState{kind: composeNew, project: m.projects[m.active].reg.Name, request: m.composeRequest}
	if m.itemPane != nil {
		c.item = m.itemPane.id
	}
	m.compose = c
	m.actionStatus = ""
	return nil
}

func (m *Model) newKey(msg tea.KeyPressMsg) tea.Cmd {
	c := m.compose
	switch msg.String() {
	case "tab", "shift+tab":
		c.focus = 1 - c.focus
		return nil
	case "enter":
		title := strings.TrimSpace(c.fields[0].Value())
		if title == "" {
			m.actionStatus = "Title must not be empty"
			return nil
		}
		p := daemon.ItemNewParams{Project: c.project, Title: title, Why: c.fields[1].Value(), From: c.item}
		filer, request := m.cfg.Live.(itemFiler), c.request
		c.pending = true
		return func() tea.Msg {
			result, err := filer.ItemNew(context.Background(), p)
			return newSliceMsg{project: p.Project, request: request, result: result, err: err}
		}
	default:
		c.field().update(msg)
		return nil
	}
}

func (m *Model) applyNew(msg newSliceMsg) tea.Cmd {
	if msg.err != nil {
		m.actionStatus = actionError(msg.err)
	} else if msg.result.Existing {
		m.actionStatus = msg.result.ID + " is already filed"
	} else {
		m.actionStatus = "Filed " + msg.result.ID
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
	return m.fetch(msg.project, true)
}
