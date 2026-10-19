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

// chrome is the number of lines around the active view: the tab line, the
// view bar, two rules and the status line.
const chrome = 5

// View implements tea.Model. The view runs on the alternate screen.
func (m *Model) View() tea.View {
	v := tea.NewView(m.screen())
	v.AltScreen = true
	return v
}

// screen is the text of the view. It has exactly height lines, none wider than
// width, laid out as in docs/dashboard.md: tabs, view bar, active view, status
// line.
func (m *Model) screen() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = defaultWidth
	}
	if h <= 0 {
		h = defaultHeight
	}
	bodyH := h - chrome
	if bodyH < 1 {
		bodyH = 1
	}
	rule := m.cfg.Theme.Dim.Render(strings.Repeat("-", w))

	var body []string
	if m.showHelp {
		body = m.helpLines(w)
	} else {
		body = m.overview(w)
	}
	body = fitHeight(body, bodyH, m.cfg.Theme)

	lines := []string{m.tabLine(w), m.viewBar(), rule}
	lines = append(lines, body...)
	lines = append(lines, rule, m.statusLine())
	clip := lipgloss.NewStyle().MaxWidth(w)
	for i, l := range lines {
		lines[i] = clip.Render(l)
	}
	return strings.Join(lines, "\n")
}

// tabLine is the project tabs on the left and the total on the right.
func (m *Model) tabLine(w int) string {
	th := m.cfg.Theme
	var tabs []string
	total := 0
	for i, p := range m.projects {
		n, star := p.needs(), ""
		if p.running {
			star = "*"
		}
		if m.refreshing(p.reg.Name) {
			star += "~"
		}
		if i == m.active {
			label := p.reg.Name
			if n > 0 {
				label += fmt.Sprint(" ", n)
			}
			tabs = append(tabs, th.TabActive.Render("["+label+star+"]"))
		} else {
			label := " " + p.reg.Name
			if n > 0 {
				label += " " + th.Badge.Render(fmt.Sprint(n))
			}
			tabs = append(tabs, th.TabIdle.Render(label+star+" "))
		}
		total += n
	}
	left := th.Title.Render("ork") + " " + strings.Join(tabs, " ")
	right := fmt.Sprintf("needs you: %d  ?help", total)
	if total > 0 {
		right = th.Badge.Render(fmt.Sprintf("needs you: %d", total)) + "  ?help"
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = lipgloss.NewStyle().MaxWidth(max(w-lipgloss.Width(right)-1, 1)).Render(left)
		gap = max(w-lipgloss.Width(left)-lipgloss.Width(right), 1)
	}
	return left + strings.Repeat(" ", gap) + right
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
	m.help.SetWidth(w)
	lines := []string{th.Heading.Render("Keys"), ""}
	lines = append(lines, strings.Split(m.help.View(m.keys), "\n")...)
	lines = append(lines, "", th.Dim.Render("Only the Overview exists so far. Waves, Roadmap and Decisions come in later slices."))
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
