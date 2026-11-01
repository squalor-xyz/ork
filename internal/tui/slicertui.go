// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"errors"
	osexec "os/exec"

	tea "charm.land/bubbletea/v2"
)

type slicerTuiDoneMsg struct {
	project   string
	err       error
	selection *slicerTuiSelection
}

// slicerTuiSelection retains the cursor across slicer edits until a snapshot arrives.
type slicerTuiSelection struct {
	project, item string
	row           int
}

// slicerTuiRefusal is shared by the action and its hint. Availability comes
// from the snapshot; ork never opens slicer's files.
func (m *Model) slicerTuiRefusal() (string, bool) {
	switch {
	case m.view != roadmapView:
		return "slicer tui opens from the Roadmap", true
	case m.active < 0:
		return "Select a project to open slicer tui", true
	case m.showHelp:
		return "Close help to open slicer tui", true
	case !m.projects[m.active].loaded || m.projects[m.active].snap.Unavailable || m.projects[m.active].setup():
		return "Project unavailable; cannot open slicer tui", true
	case m.matrixPaneOpen():
		return "", true
	case m.itemPane != nil:
		return "Close the pane to open slicer tui", true
	}
	return "", false
}

func (m *Model) openSlicerTui() tea.Cmd {
	if status, refused := m.slicerTuiRefusal(); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	bin := m.cfg.SlicerBin
	if _, err := osexec.LookPath(bin); err != nil {
		m.actionStatus = "slicer tui: " + err.Error()
		if errors.Is(err, osexec.ErrNotFound) {
			m.actionStatus = "slicer tui: " + bin + " not found on PATH"
		}
		return nil
	}
	project := m.projects[m.active].reg
	selection := &slicerTuiSelection{project: project.Name, row: m.roadmapCursor}
	items := m.roadmapItems()
	if i := clampCursor(m.roadmapCursor, len(items)); i >= 0 {
		selection.item = items[i].ID
	}
	cmd := osexec.Command(bin, "tui", "--root", project.Path)
	return m.cfg.Exec(cmd, func(err error) tea.Msg {
		return slicerTuiDoneMsg{project: project.Name, err: err, selection: selection}
	})
}

// restoreSlicerTuiCursor runs only after apply has stored the new snapshot.
// A coalesced fetch leaves the restore pending for the in-flight read's reply.
func (m *Model) restoreSlicerTuiCursor(project string) {
	selection := m.slicerTuiRestore
	if selection == nil || selection.project != project || m.byName[project].snap.Items.Err != nil {
		return
	}
	m.slicerTuiRestore = nil
	if m.active < 0 || m.projects[m.active].reg.Name != project || m.view != roadmapView {
		return
	}
	items := m.roadmapItems()
	m.roadmapCursor = clampCursor(selection.row, len(items))
	for i, item := range items {
		if item.ID == selection.item {
			m.roadmapCursor = i
			break
		}
	}
}
