// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// roadmapEditor is optional for Live doubles.
type roadmapEditor interface {
	ItemPark(context.Context, string, string) error
	ItemUnpark(context.Context, string, string) error
	ItemMove(context.Context, string, string, string, string) (bool, error)
}

var _ roadmapEditor = (*Daemon)(nil)

type roadmapEditMsg struct {
	action     runAction
	navigation uint64
	index      int
	changed    bool
	err        error
}

// roadmapEditRefusal shares the list-only, silent refusals of matrixRefusal.
func (m *Model) roadmapEditRefusal(verb string) (string, bool) {
	if status, refused := m.matrixRefusal(); refused {
		return status, true
	}
	if n := len(m.roadmapItems()); n == 0 || (n < 2 && (verb == "move" || verb == "down" || verb == "up")) {
		return "", true
	}
	if _, ok := m.cfg.Live.(roadmapEditor); !ok {
		return "Roadmap edit unavailable", true
	}
	if verb == "park" {
		items := m.roadmapItems()
		it := items[clampCursor(m.roadmapCursor, len(items))]
		if it.Status != "parked" {
			for _, r := range m.projects[m.active].snap.EngineRuns {
				if r.Item == it.ID && !runrec.IsFinal(r.State) {
					return "Cannot park " + it.ID + ": run " + r.ID + " is unfinished", true
				}
			}
		}
	}
	return "", false
}

func (m *Model) requestRoadmapEdit(verb string) tea.Cmd {
	if status, refused := m.roadmapEditRefusal(verb); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	items := m.roadmapItems()
	i := clampCursor(m.roadmapCursor, len(items))
	a := runAction{verb: verb, project: m.projects[m.active].reg.Name, item: items[i].ID}
	switch verb {
	case "park":
		if items[i].Status == "parked" {
			a.verb = "unpark"
		}
	case "down":
		if i == len(items)-1 {
			return nil
		}
		a.verb, a.position, a.other = "move", "after", items[i+1].ID
	case "up":
		if i == 0 {
			return nil
		}
		a.verb, a.position, a.other = "move", "before", items[i-1].ID
	}
	m.confirm = &a
	return nil
}

func (m *Model) startRoadmapEdit(a runAction) tea.Cmd {
	editor, ok := m.cfg.Live.(roadmapEditor)
	if !ok {
		m.actionStatus = "Roadmap edit unavailable"
		return nil
	}
	m.actionStatus = "pending: " + a.command()
	navigation := m.roadmapNavigation
	index := -1
	if ps := m.byName[a.project]; ps != nil {
		for i, it := range ps.snap.Items.Value {
			if it.ID == a.item {
				index = i
				break
			}
		}
	}
	return func() tea.Msg {
		msg := roadmapEditMsg{action: a, navigation: navigation, index: index}
		switch a.verb {
		case "park":
			msg.err = editor.ItemPark(context.Background(), a.project, a.item)
		case "unpark":
			msg.err = editor.ItemUnpark(context.Background(), a.project, a.item)
		case "move":
			msg.changed, msg.err = editor.ItemMove(context.Background(), a.project, a.item, a.other, a.position)
		}
		return msg
	}
}

func (m *Model) applyRoadmapEdit(msg roadmapEditMsg) tea.Cmd {
	if msg.err != nil {
		m.actionStatus = actionError(msg.err)
		return nil
	}
	a := msg.action
	switch a.verb {
	case "park":
		m.actionStatus = "Parked " + a.item
	case "unpark":
		m.actionStatus = "Unparked " + a.item
	case "move":
		m.actionStatus = "Moved " + a.item + " " + a.position + " " + a.other
		if !msg.changed {
			m.actionStatus += " in queue; list order unchanged"
		} else if msg.navigation == m.roadmapNavigation && m.view == roadmapView && m.active >= 0 && m.projects[m.active].reg.Name == a.project {
			m.roadmapFollow = &a
			// A poll can deliver the new order before the write reply.
			if ps := m.byName[a.project]; ps != nil && ps.snap.Items.Err == nil && msg.index >= 0 {
				for i, it := range ps.snap.Items.Value {
					if it.ID == a.item {
						if i != msg.index {
							m.roadmapCursor = i
							m.roadmapFollow = nil
						}
						break
					}
				}
			}
		}
	}
	return m.fetch(a.project, true)
}

func sameRoadmapOrder(a, b []slicercli.Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}
