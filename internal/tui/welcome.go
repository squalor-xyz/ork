// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/registry"
)

type welcomeState struct {
	editing bool
	field   textField
	confirm string
	pending bool
}

type projectAddedMsg struct {
	project registry.Project
	err     error
}

func (m *Model) welcomeKey(msg tea.KeyPressMsg) tea.Cmd {
	w := &m.welcome
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	if w.pending {
		return nil
	}
	if w.confirm != "" {
		switch msg.String() {
		case "y", "Y":
			path := w.confirm
			w.confirm = ""
			live := m.cfg.Live
			if live == nil {
				m.actionStatus = "No daemon connection; action unavailable"
				return nil
			}
			w.pending = true
			m.actionStatus = "Adding project..."
			return func() tea.Msg {
				p, err := live.ProjectsAdd(context.Background(), path)
				return projectAddedMsg{project: p, err: err}
			}
		case "enter", "n", "N", "esc":
			w.confirm = ""
			m.actionStatus = "Cancelled"
		}
		return nil
	}
	if w.editing {
		switch msg.String() {
		case "esc":
			w.editing = false
			m.actionStatus = ""
		case "enter":
			path := strings.TrimSpace(w.field.Value())
			if path == "" {
				var err error
				path, err = m.cfg.Getwd()
				if err != nil {
					m.actionStatus = actionError(err)
					return nil
				}
				if !hasProjectMarker(path) {
					m.actionStatus = path + " holds neither .slicer nor .adjudicate.json; type a path"
					return nil
				}
			}
			if path == "~" || strings.HasPrefix(path, "~/") {
				home, err := os.UserHomeDir()
				if err != nil {
					m.actionStatus = actionError(err)
					return nil
				}
				path = filepath.Join(home, strings.TrimPrefix(path, "~"))
			}
			w.confirm = path
			m.actionStatus = ""
		default:
			w.field.update(msg)
			m.actionStatus = ""
		}
		return nil
	}
	switch msg.String() {
	case "+":
		m.showHelp = false
		w.editing = true
		m.actionStatus = ""
	case "q":
		return tea.Quit
	case "?":
		m.showHelp = !m.showHelp
	case "esc":
		m.showHelp = false
		m.actionStatus = ""
	}
	return nil
}

func hasProjectMarker(path string) bool {
	if info, err := os.Stat(filepath.Join(path, ".slicer")); err == nil && info.IsDir() {
		return true
	}
	info, err := os.Stat(filepath.Join(path, ".adjudicate.json"))
	return err == nil && info.Mode().IsRegular()
}

func (m *Model) welcomeBody(width int) []string {
	th := m.cfg.Theme
	lines := []string{indent + th.Heading.Render("Welcome"), "", indent + "No projects yet."}
	if m.welcome.editing {
		value := m.welcome.field.view(max(width-len(indent)-14, 1), th)
		if m.welcome.confirm != "" || m.welcome.pending {
			value = th.Title.Render(m.welcome.field.Value())
		}
		lines = append(lines, "", indent+"Project path: "+value)
		status := m.actionStatus
		if m.welcome.confirm != "" {
			status = "ork projects add " + shellQuote(m.welcome.confirm) + "? [y/N] (Enter is No)"
		}
		if status != "" {
			for _, line := range wrapText(status, max(width-len(indent), 1)) {
				lines = append(lines, indent+line)
			}
		}
		for _, line := range wrapText("Type an absolute path, or leave it empty and press Enter to use the current directory (needs .slicer or .adjudicate.json).", max(width-len(indent), 1)) {
			lines = append(lines, indent+th.Dim.Render(line))
		}
	} else if m.actionStatus != "" {
		lines = append(lines, "")
		for _, line := range wrapText(m.actionStatus, max(width-len(indent), 1)) {
			lines = append(lines, indent+line)
		}
	}
	return lines
}

func (m *Model) applyProjectAdded(msg projectAddedMsg) tea.Cmd {
	m.welcome.pending = false
	if msg.err != nil {
		m.actionStatus = actionError(msg.err)
		return nil
	}
	m.registerProject(msg.project)
	p := m.byName[msg.project.Name]
	m.welcome = welcomeState{}
	m.actionStatus = fmt.Sprintf("Added %s %s", p.reg.Name, p.reg.Path)
	return tea.Batch(m.fetch(p.reg.Name, false), m.focus())
}

// projectReader uses the daemon's existing registry listing without requiring
// offline sources to supply one.
type projectReader interface {
	Projects(context.Context) ([]registry.Project, error)
}

type projectsDiscoveredMsg struct {
	projects []registry.Project
	err      error
}

func (m *Model) discoverProjects() tea.Cmd {
	reader, ok := m.cfg.Live.(projectReader)
	if !ok || m.projectsReading {
		return nil
	}
	m.projectsReading = true
	return func() tea.Msg {
		projects, err := reader.Projects(context.Background())
		return projectsDiscoveredMsg{projects: projects, err: err}
	}
}

func (m *Model) registerProject(project registry.Project) bool {
	old := m.byName[project.Name]
	if old != nil && !old.snap.Unavailable {
		return false
	}
	p := &projectState{reg: project}
	if old == nil {
		m.projects = append(m.projects, p)
	} else {
		for i, current := range m.projects {
			if current == old {
				m.projects[i] = p
				break
			}
		}
	}
	m.byName[project.Name] = p
	m.poller.Add(project.Name)
	return true
}

func (m *Model) applyProjectsDiscovered(msg projectsDiscoveredMsg) tea.Cmd {
	m.projectsReading = false
	if msg.err != nil {
		return nil
	}
	wasWelcome := m.registeredProjectCount() == 0
	var cmds []tea.Cmd
	var found string
	for _, project := range msg.projects {
		if m.registerProject(project) {
			if found == "" {
				found = fmt.Sprintf("Found %s %s", project.Name, project.Path)
			}
			cmds = append(cmds, m.fetch(project.Name, false))
		}
	}
	if len(cmds) > 0 {
		if wasWelcome {
			m.welcome = welcomeState{}
			m.showHelp = false
			m.actionStatus = found
		}
		cmds = append(cmds, m.focus())
	}
	return tea.Batch(cmds...)
}
