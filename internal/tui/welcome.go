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
	lines := []string{indent + th.Heading.Render("Welcome"), "", indent + "No projects yet. Press + to add one"}
	if m.welcome.editing {
		value := m.welcome.field.view(max(width-len(indent)-14, 1), th)
		if m.welcome.confirm != "" || m.welcome.pending {
			value = th.Title.Render(m.welcome.field.Value())
		}
		lines = append(lines, "", indent+"Project path: "+value)
		for _, line := range wrapText("Type an absolute path, or leave it empty and press Enter to use the current directory (needs .slicer or .adjudicate.json). Esc cancels.", max(width-len(indent), 1)) {
			lines = append(lines, indent+th.Dim.Render(line))
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
	p := &projectState{reg: msg.project}
	replaced := false
	for i, old := range m.projects {
		if old.reg.Name == p.reg.Name && old.snap.Unavailable {
			m.projects[i] = p
			replaced = true
			break
		}
	}
	if !replaced {
		m.projects = append(m.projects, p)
	}
	m.byName[p.reg.Name] = p
	m.poller.Add(p.reg.Name)
	m.welcome = welcomeState{}
	m.actionStatus = fmt.Sprintf("added %s %s", p.reg.Name, p.reg.Path)
	return tea.Batch(m.fetch(p.reg.Name, false), m.focus())
}
