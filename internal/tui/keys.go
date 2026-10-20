// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import "charm.land/bubbles/v2/key"

// keyMap is every key the TUI answers. It also feeds the help view.
type keyMap struct {
	Approve    key.Binding
	Stop       key.Binding
	Retry      key.Binding
	Next       key.Binding
	Prev       key.Binding
	Jump       key.Binding
	RowNext    key.Binding
	RowPrev    key.Binding
	Select     key.Binding
	Answer     key.Binding
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
		Approve:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve")),
		Stop:       key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop")),
		Retry:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "retry")),
		Filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter projects; esc clears")),
		Sidebar:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "hide/show sidebar")),
		Next:       key.NewBinding(key.WithKeys("tab", "right"), key.WithHelp("tab/right", "next project")),
		Prev:       key.NewBinding(key.WithKeys("shift+tab", "left"), key.WithHelp("shift+tab/left", "previous project")),
		Jump:       key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"), key.WithHelp("1-9", "jump to project")),
		RowNext:    key.NewBinding(key.WithKeys("down", "j", "n"), key.WithHelp("down/j/n", "next row")),
		RowPrev:    key.NewBinding(key.WithKeys("up", "k", "p"), key.WithHelp("up/k/p", "previous row")),
		Select:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open engine pane; other rows select project")),
		Answer:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter (answer)", "submit; no confirmation")),
		PaneScroll: key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdown", "scroll pane reason")),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh now")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "show or hide this help")),
		Close:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close pane, help or message")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding { return []key.Binding{k.Refresh, k.Help, k.Quit} }

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Next, k.Prev, k.Jump}, {k.RowNext, k.RowPrev, k.Select}, {k.Filter, k.Sidebar}, {k.Approve, k.Stop, k.Retry, k.Answer, k.PaneScroll}, {k.Refresh, k.Help, k.Close, k.Quit}}
}
