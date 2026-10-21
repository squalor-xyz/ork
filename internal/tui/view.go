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

	sideW := 0
	if !m.sidebarHidden {
		sideW = 28
		if w < 100 {
			sideW = 20
		}
		sideW = min(sideW, max(w/3, 1))
	}
	mainW := max(w-sideW, 1)
	th := m.cfg.Theme
	var body, detail []string
	bodyH, start := max(h-4, 1), 0
	if m.showHelp {
		body = m.helpLines(mainW)
	} else if m.view == roadmapView {
		rv := m.roadmap(mainW)
		body = rv.body
		start = windowStart(rv.sel, len(body), bodyH)
	} else {
		// The detail line holds the selected row's full text. It takes lines
		// from the body, at most h-5, so the body keeps one line.
		ov := m.overview(mainW)
		body, detail = ov.body, ov.detail
		status := m.actionStatus
		if m.confirm != nil {
			status = m.confirm.command() + "? [y/N] (Enter is No)"
		}
		if m.pane != nil {
			detail = m.paneLines(mainW, max(h-5, 1), status)
		} else if status != "" {
			detail = wrapText(status, max(mainW-len(indent), 1))
			for i := range detail {
				detail[i] = indent + detail[i]
			}
		}
		if len(detail) == 0 {
			detail = []string{""}
		}
		detail = detail[:min(len(detail), max(h-5, 1))]
		bodyH = max(h-4-len(detail), 1)
		start = windowStart(ov.sel, len(body), bodyH)
	}
	body = window(body, start, bodyH, th)
	rule := m.cfg.Theme.Dim.Render(strings.Repeat("-", mainW))
	main := []string{m.viewBar(), rule}
	main = append(main, body...)
	main = append(main, detail...)
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
		setup := false
		if idx >= 0 {
			p := m.projects[idx]
			label = p.reg.Name
			setup = p.setup()
			if setup {
				badge = " no slicer"
			} else {
				badge = fmt.Sprintf(" %d", p.needs())
				if p.running {
					badge += "*"
				}
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
		switch {
		case idx == m.active && setup:
			label = m.cfg.Theme.TabActive.Faint(true).Render(label)
		case idx == m.active:
			label = m.cfg.Theme.TabActive.Render(label)
		case setup:
			label = m.cfg.Theme.Dim.Render(label)
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

// views is the view bar, in order (ui.view-list). A view that is not built yet is
// drawn dim, and tab and shift+tab skip it. Each later slice sets built when its
// view lands.
var views = []struct {
	name  string
	built bool
}{
	{"Overview", true},
	{"Questions", false},
	{"Waves", false},
	{"Roadmap", true},
	{"Decisions", false},
}

// viewBar lists the views. The active view uses ViewActive. A view that is not
// built yet is drawn dim with the separators before it; a built view that is not
// active is plain.
func (m *Model) viewBar() string {
	th := m.cfg.Theme
	var b strings.Builder
	var idle string // separators and unbuilt names not yet drawn, drawn as one dim run
	flush := func() {
		if idle != "" {
			b.WriteString(th.ViewIdle.Render(idle))
			idle = ""
		}
	}
	b.WriteString(" ")
	for i, v := range views {
		if i > 0 {
			idle += " | "
		}
		switch {
		case i == m.view:
			flush()
			b.WriteString(th.ViewActive.Render(v.name))
		case v.built:
			flush()
			b.WriteString(v.name)
		default:
			idle += v.name
		}
	}
	flush()
	return b.String()
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
	lines = append(lines, "", clip("Overview and Roadmap are available. Questions, Waves and Decisions come in later slices.", w))
	return lines
}

// span is the part of a body of total lines that a window of n lines shows
// from start: the lines start to hi, and whether a marker line is needed above
// and below. A marker takes the place of a line of the window.
func span(total, start, n int) (hi int, above, below bool) {
	if n < 3 {
		// Too short for markers: the window is just the lines.
		return min(start+n, total), false, false
	}
	avail := n
	above = start > 0
	if above {
		avail--
	}
	below = start+avail < total
	if below {
		avail--
	}
	return min(start+avail, total), above, below
}

// windowStart is the first body line a window of n lines starts at so that
// line sel is shown. It is the first line the window can start at with sel on
// it. sel -1 means no line, and the window starts at the top.
func windowStart(sel, total, n int) int {
	for start := 0; start < sel; start++ {
		if hi, _, _ := span(total, start, n); sel < hi {
			return start
		}
	}
	return max(sel, 0)
}

// window is n lines of body from start. Lines hidden above or below are said
// so on a marker line, and the window is padded to n lines.
func window(lines []string, start, n int, th Theme) []string {
	n = max(n, 1)
	start = min(max(start, 0), max(len(lines)-1, 0))
	hi, above, below := span(len(lines), start, n)
	var out []string
	if above {
		out = append(out, th.Dim.Render(fmt.Sprintf("%s+%d lines above", indent, start)))
	}
	out = append(out, lines[start:hi]...)
	if below {
		out = append(out, th.Dim.Render(fmt.Sprintf("%s+%d more lines", indent, len(lines)-hi)))
	}
	for len(out) < n {
		out = append(out, "")
	}
	return out
}
