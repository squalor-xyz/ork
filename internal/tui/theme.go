// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import "github.com/charmbracelet/lipgloss"

// Theme is every style the TUI draws with. The colour theme uses the 16 ANSI
// colours, so the terminal's own palette decides the shades. The monochrome
// theme uses bold, underline and reverse only. In both, a state is also
// spelled out in a word or symbol (docs/dashboard.md, "Color is never the only
// signal"), so the two themes differ in style, never in text.
type Theme struct {
	Title      lipgloss.Style
	TabActive  lipgloss.Style
	TabIdle    lipgloss.Style
	Badge      lipgloss.Style
	ViewActive lipgloss.Style
	ViewIdle   lipgloss.Style
	Heading    lipgloss.Style
	Dim        lipgloss.Style
	Decision   lipgloss.Style
	Held       lipgloss.Style
	Error      lipgloss.Style
	Pending    lipgloss.Style
	External   lipgloss.Style
}

// NewTheme returns the colour theme, or the monochrome theme when mono is
// true, drawn through r.
func NewTheme(r *lipgloss.Renderer, mono bool) Theme {
	s := r.NewStyle
	if mono {
		return Theme{
			Title:      s().Bold(true),
			TabActive:  s().Bold(true).Reverse(true),
			TabIdle:    s(),
			Badge:      s().Bold(true),
			ViewActive: s().Bold(true),
			ViewIdle:   s().Faint(true),
			Heading:    s().Bold(true),
			Dim:        s().Faint(true),
			Decision:   s().Bold(true),
			Held:       s().Bold(true),
			Error:      s().Bold(true),
			Pending:    s().Faint(true),
			External:   s().Bold(true),
		}
	}
	return Theme{
		Title:      s().Bold(true),
		TabActive:  s().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("4")),
		TabIdle:    s(),
		Badge:      s().Bold(true).Foreground(lipgloss.Color("3")),
		ViewActive: s().Bold(true).Foreground(lipgloss.Color("6")),
		ViewIdle:   s().Foreground(lipgloss.Color("8")),
		Heading:    s().Bold(true).Foreground(lipgloss.Color("6")),
		Dim:        s().Foreground(lipgloss.Color("8")),
		Decision:   s().Bold(true).Foreground(lipgloss.Color("3")),
		Held:       s().Bold(true).Foreground(lipgloss.Color("5")),
		Error:      s().Bold(true).Foreground(lipgloss.Color("1")),
		Pending:    s().Foreground(lipgloss.Color("8")),
		External:   s().Foreground(lipgloss.Color("6")),
	}
}

// UseMono reports whether the environment asks for no colour: NO_COLOR set to
// a non-empty value (https://no-color.org).
func UseMono(getenv func(string) string) bool { return getenv("NO_COLOR") != "" }

// ThemeFor returns the monochrome theme when UseMono says so, else the colour
// theme.
func ThemeFor(getenv func(string) string, r *lipgloss.Renderer) Theme {
	return NewTheme(r, UseMono(getenv))
}
