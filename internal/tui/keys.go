// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import "charm.land/bubbles/v2/key"

// keyMap is every key the TUI answers. It also feeds the help view.
type keyMap struct {
	AllProjects key.Binding

	Add        key.Binding
	Cleanup    key.Binding
	EditLineup key.Binding
	Tick       key.Binding
	Launch     key.Binding
	Approve    key.Binding
	Stop       key.Binding
	Retry      key.Binding
	FileSlice  key.Binding
	Fix        key.Binding
	MarkCheck  key.Binding
	Renders    key.Binding
	Next       key.Binding
	Prev       key.Binding
	TabNext    key.Binding
	TabPrev    key.Binding
	NextView   key.Binding
	PrevView   key.Binding
	Jump       key.Binding
	RowNext    key.Binding
	RowPrev    key.Binding
	Select     key.Binding
	Answer     key.Binding
	PickOption key.Binding
	FreeAnswer key.Binding
	FileAdj    key.Binding
	PaneScroll key.Binding
	Refresh    key.Binding
	Help       key.Binding
	Close      key.Binding
	Quit       key.Binding
	Filter     key.Binding
	Sidebar    key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		AllProjects: key.NewBinding(key.WithKeys("A", "shift+a"), key.WithHelp("A (Decisions)", "toggle all projects")),

		Add:        key.NewBinding(key.WithKeys("+"), key.WithHelp("+", "add project (Welcome)")),
		Cleanup:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "remove merged worktree and branch (Ready)")),
		EditLineup: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit ticked item lineup (Waves)")),
		Tick:       key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "tick item and ask inputs (Waves)")),
		Launch:     key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "launch a ready slice (Roadmap) or wave (Waves)")),
		Approve:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve")),
		Stop:       key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop")),
		Retry:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "retry")),
		FileSlice:  key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "file slice")),
		Fix:        key.NewBinding(key.WithKeys("F", "shift+f"), key.WithHelp("F", "fix round")),
		MarkCheck:  key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"), key.WithHelp("1-9", "mark check")),
		Renders:    key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "view renders")),
		Filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter projects; esc clears")),
		Sidebar:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "hide/show sidebar")),
		Next:       key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next project")),
		Prev:       key.NewBinding(key.WithKeys("["), key.WithHelp("[", "previous project")),
		TabNext:    key.NewBinding(key.WithKeys("right"), key.WithHelp("right", "next tab (Decisions)")),
		TabPrev:    key.NewBinding(key.WithKeys("left"), key.WithHelp("left", "previous tab (Decisions)")),
		NextView:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next view")),
		PrevView:   key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous view")),
		Jump:       key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"), key.WithHelp("1-9", "jump to project")),
		RowNext:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("down/j", "next row")),
		RowPrev:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("up/k", "previous row")),
		Select:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open run, question, item or decision; other rows select project")),
		FileAdj:    key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a (answer)", "toggle filing the answer in adj")),
		PickOption: key.NewBinding(key.WithKeys("1", "2", "3", "4"), key.WithHelp("1-4 (question)", "pick an option; y sends")),
		FreeAnswer: key.NewBinding(key.WithKeys("t"), key.WithHelp("t (question)", "type a free answer")),
		Answer:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter (answer)", "submit free text")),
		PaneScroll: key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdown", "scroll run tree and reason or decision")),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh now")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "show or hide this help")),
		Close:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close run, question, item, decision, help or message")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding { return []key.Binding{k.Refresh, k.Help, k.Quit} }

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.NextView, k.PrevView, k.AllProjects}, {k.Next, k.Prev, k.Jump}, {k.RowNext, k.RowPrev, k.Select}, {k.Filter, k.Sidebar, k.Add}, {k.Tick, k.EditLineup, k.Launch}, {k.Approve, k.Fix, k.Stop, k.Retry, k.Renders, k.FileSlice, k.Answer, k.PickOption, k.FreeAnswer, k.FileAdj, k.PaneScroll, k.Cleanup}, {k.Refresh, k.Help, k.Close, k.Quit}}
}
