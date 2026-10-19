// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap is every key the TUI answers. It also feeds the help view. None of
// them writes anything (slice S11 has no write action).
type keyMap struct {
	Next    key.Binding
	Prev    key.Binding
	Jump    key.Binding
	Refresh key.Binding
	Help    key.Binding
	Close   key.Binding
	Quit    key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Next:    key.NewBinding(key.WithKeys("tab", "right"), key.WithHelp("tab/right", "next project")),
		Prev:    key.NewBinding(key.WithKeys("shift+tab", "left"), key.WithHelp("shift+tab/left", "previous project")),
		Jump:    key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"), key.WithHelp("1-9", "jump to project")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh now")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "show or hide this help")),
		Close:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close help")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding { return []key.Binding{k.Refresh, k.Help, k.Quit} }

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Next, k.Prev, k.Jump}, {k.Refresh, k.Help, k.Close, k.Quit}}
}
