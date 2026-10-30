// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/source"
)

type projectRemoval struct {
	name    string
	pending bool
}

type projectRemovedMsg struct {
	name string
	err  error
}

// Project keys remain text while an existing field owns keyboard input.
func (m *Model) projectTextEntry() bool {
	return m.filtering || m.compose != nil || m.launch != nil ||
		(m.waves != nil && (m.waves.sitting != nil || m.waves.lineup != nil || m.waves.inputs != nil)) ||
		(m.pane != nil && (m.pane.fixing || (m.paneQuestion() && (m.paneAsk() == nil || m.pane.freeText))))
}

func (m *Model) requestProjectRemoval() tea.Cmd {
	if m.active < 0 || m.projects[m.active].snap.Unavailable {
		m.actionStatus = "Select a registered project to remove"
		return nil
	}
	m.showHelp = false
	m.removing = projectRemoval{name: m.projects[m.active].reg.Name}
	m.actionStatus = ""
	return nil
}

func (m *Model) removeProjectKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	if m.removing.pending {
		return nil
	}
	switch msg.String() {
	case "y", "Y":
		live := m.cfg.Live
		if live == nil {
			m.actionStatus = "No daemon connection; action unavailable"
			return nil
		}
		name := m.removing.name
		m.removing.pending = true
		m.actionStatus = "Removing project..."
		return func() tea.Msg {
			return projectRemovedMsg{name: name, err: live.ProjectsRemove(context.Background(), name)}
		}
	case "enter", "n", "N", "esc":
		m.removing = projectRemoval{}
		m.actionStatus = "Cancelled"
	}
	return nil
}

func (m *Model) projectRemovalBody(width int) []string {
	lines := []string{indent + m.cfg.Theme.Heading.Render("Remove project"), ""}
	status := "ork projects rm " + shellQuote(m.removing.name) + "? [y/N] (Enter is No)"
	if m.removing.pending {
		status = m.actionStatus
	}
	for _, line := range wrapText(status, max(width-len(indent), 1)) {
		lines = append(lines, indent+line)
	}
	for _, line := range wrapText("Files on disk are untouched; past runs stay in a read-only removed group.", max(width-len(indent), 1)) {
		lines = append(lines, indent+m.cfg.Theme.Dim.Render(line))
	}
	if !m.removing.pending && m.actionStatus != "" {
		for _, line := range wrapText(m.actionStatus, max(width-len(indent), 1)) {
			lines = append(lines, indent+line)
		}
	}
	return lines
}

func (m *Model) applyProjectRemoved(msg projectRemovedMsg) tea.Cmd {
	m.removing = projectRemoval{}
	if msg.err != nil {
		m.actionStatus = actionError(msg.err)
		return nil
	}
	// Keep historical runs in the existing read-only removed group. Ignore
	// snapshots already in flight so they cannot restore the registration.
	if p := m.byName[msg.name]; p != nil {
		runs := p.snap.EngineRuns
		for i := range runs {
			runs[i].Unavailable = true
		}
		p.snap = source.Snapshot{Project: p.reg, Unavailable: true, EngineRuns: runs}
		p.open, p.held, p.running, p.dirty = 0, 0, false, false
		p.loaded = true
		m.poller.Finish(msg.name, m.cfg.Now())
	}
	m.projectsRevision++
	m.active = needsEntry
	for i, p := range m.projects {
		if !p.snap.Unavailable {
			m.active = i
			break
		}
	}
	m.pane, m.itemPane, m.decisionPane = nil, nil, nil
	m.dropLaunch()
	m.waves = nil
	m.cursor, m.roadmapCursor, m.questionsCursor, m.decisionsCursor = 0, 0, 0, 0
	if m.registeredProjectCount() == 0 {
		m.view = 0
	}
	m.actionStatus = "Removed " + msg.name
	return m.focus()
}
