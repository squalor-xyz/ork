// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Size used until the terminal reports its own.
const (
	defaultWidth  = 80
	defaultHeight = 24
)

// View implements tea.Model. The view runs on the alternate screen.
func (m *Model) View() tea.View {
	v := tea.NewView(m.screen())
	v.AltScreen = true
	return v
}

// screen is the text of the view. It has exactly height lines, none wider than
// width, laid out as in docs/dashboard.md: sidebar, view bar, active view, status
// line.
func (m *Model) screen() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = defaultWidth
	}
	if h <= 0 {
		h = defaultHeight
	}

	bodyH := max(h-4, 1)
	sideW := 0
	if !m.sidebarHidden {
		sideW = 28
		if w < 100 {
			sideW = 20
		}
		sideW = min(sideW, max(w/3, 1))
	}
	mainW := max(w-sideW, 1)
	var body []string
	if m.showHelp {
		body = m.helpLines(mainW)
	} else {
		body = m.overview(mainW)
	}
	body = fitHeight(body, bodyH, m.cfg.Theme)
	rule := m.cfg.Theme.Dim.Render(strings.Repeat("-", mainW))
	main := []string{m.viewBar(), rule}
	main = append(main, body...)
	main = append(main, rule, m.statusLine())
	if len(main) > h {
		main = main[:h]
	}
	side := m.sidebar(sideW, h)
	clipStyle := lipgloss.NewStyle().MaxWidth(w)
	for i := range main {
		line := main[i]
		if sideW > 0 {
			line = side[i] + line
		}
		main[i] = clipStyle.Render(line)
	}
	return strings.Join(main, "\n")
}

func (m *Model) sidebar(w, h int) []string {
	lines := []string{m.cfg.Theme.Title.Render("ork") + "  ?help"}
	nav := m.navigation()
	slots := max(h-3, 1)
	start := 0
	selected := 0
	for i, n := range nav {
		if n == m.active {
			selected = i
		}
	}
	if selected >= slots {
		start = selected - slots + 1
	}
	end := min(start+slots, len(nav))
	for _, idx := range nav[start:end] {
		label := "Needs you"
		badge := ""
		if idx >= 0 {
			p := m.projects[idx]
			label = p.reg.Name
			badge = fmt.Sprintf(" %d", p.needs())
			if p.running {
				badge += "*"
			}
			if m.refreshing(p.reg.Name) {
				badge += "~"
			}
		}
		label = clip(label, max(w-3-len(badge), 1)) + badge
		prefix := "  "
		if idx == m.active {
			prefix = "> "
		}
		label = prefix + label
		if idx == m.active {
			label = m.cfg.Theme.TabActive.Render(label)
		}
		lines = append(lines, label)
	}
	hidden := start + len(nav) - end
	if hidden > 0 {
		lines = append(lines, fmt.Sprintf("  +%d more", hidden))
	}
	if m.filtering || m.filter != "" {
		lines = append(lines, "/"+m.filter)
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	lines = lines[:h]
	for i, l := range lines {
		l = lipgloss.NewStyle().MaxWidth(max(w-1, 1)).Render(l)
		lines[i] = l + strings.Repeat(" ", max(w-lipgloss.Width(l)-1, 0)) + "|"
	}
	return lines
}

// viewBar lists the views. Only the Overview exists in this slice; the others
// are drawn dim and cannot be selected yet.
func (m *Model) viewBar() string {
	th := m.cfg.Theme
	return " " + th.ViewActive.Render("Overview") + th.ViewIdle.Render(" | Waves | Roadmap | Decisions")
}

// statusLine shows the project count, the last refresh and the number of
// projects still refreshing. The token sparkline waits for S07 and the daemon figures for S03.
func (m *Model) statusLine() string {
	noun := "projects"
	if len(m.projects) == 1 {
		noun = "project"
	}
	refreshed := "never"
	if !m.lastRefresh.IsZero() {
		refreshed = m.lastRefresh.Format("15:04:05")
	}
	return fmt.Sprintf(" %d %s | refreshed %s | pending %d", len(m.projects), noun, refreshed, m.poller.Pending())
}

// helpLines is the key list shown in place of the Overview.
func (m *Model) helpLines(w int) []string {
	th := m.cfg.Theme
	lines := []string{th.Heading.Render("Keys"), ""}
	for _, group := range m.keys.FullHelp() {
		for _, binding := range group {
			h := binding.Help()
			lines = append(lines, clip(h.Key+" "+h.Desc, w))
		}
	}
	lines = append(lines, "", clip("Only the Overview exists so far. Waves, Roadmap and Decisions come in later slices.", w))
	return lines
}

// fitHeight cuts lines to n, saying how many were cut, or pads them to n.
func fitHeight(lines []string, n int, th Theme) []string {
	if len(lines) > n {
		cut := len(lines) - (n - 1)
		lines = append(lines[:n-1:n-1], th.Dim.Render(fmt.Sprintf("  +%d more lines", cut)))
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}
