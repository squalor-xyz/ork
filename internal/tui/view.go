// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/brand"
	"github.com/squalor-xyz/ork/internal/daemon"
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

// layoutSize shares the terminal and sidebar dimensions with detail scrolling.
func (m *Model) layoutSize() (w, h, sideW int) {
	w, h = m.width, m.height
	if w <= 0 {
		w = defaultWidth
	}
	if h <= 0 {
		h = defaultHeight
	}

	sideW = 0
	if !m.sidebarHidden {
		sideW = 28
		if w < 100 {
			sideW = 20
		}
		sideW = min(sideW, max(w/3, 1))
	}
	return w, h, sideW
}

// screen is exactly height lines, none wider than width: sidebar, view bar,
// active view and status line, as in docs/dashboard.md.
func (m *Model) screen() string {
	w, h, sideW := m.layoutSize()
	mainW := max(w-sideW, 1)
	th := m.cfg.Theme
	var body, detail, footer []string
	bodyH, start := max(h-4, 1), 0
	m.drawn = drawnRows{sel: -1}
	selectedLine, selExtra := -1, 0
	overviewBody := false
	collapse := false // a render on show takes the whole body (S121, *proposed*)
	if m.showHelp {
		body = m.helpLines(mainW, bodyH)
	} else if m.registeredProjectCount() == 0 && m.pane == nil && (m.active < 0 || m.welcome.editing || m.welcome.pending || m.welcome.confirm != "") {
		body = m.welcomeBody(mainW)
		status := m.actionStatus
		if m.welcome.confirm != "" {
			status = "ork projects add " + shellQuote(m.welcome.confirm) + "? [y/N] (Enter is No)"
		}
		if status != "" {
			detail = wrapText(status, max(mainW-len(indent), 1))
			for i := range detail {
				detail[i] = indent + detail[i]
			}
		}
	} else if m.view == roadmapView || m.view == wavesView || m.view == decisionsView || m.view == questionsView {
		rv := overviewView{}
		if m.view == questionsView {
			rv = m.questions(mainW)
		} else if m.view == decisionsView {
			rv = m.decisions(mainW)
		} else if m.view == wavesView {
			if m.waves != nil && m.waves.lineup != nil {
				rv = m.lineupBody(mainW)
			} else {
				rv, footer = m.wavesParts(mainW)
			}
		} else if m.launch != nil {
			rv = m.launchView(mainW)
		} else {
			rv = m.roadmap(mainW)
		}
		status := m.actionStatus
		if status == "" && m.view == wavesView && m.waves != nil && m.waves.lineup == nil && m.waves.sitting == nil && len(m.waves.picked) > 0 {
			status = fmt.Sprintf("%d ticked, not launched; [l] launches", len(m.waves.picked))
		}
		if m.confirm != nil {
			status = m.confirm.prompt()
		}
		if m.view == questionsView && m.pane != nil {
			detail = m.paneLines(mainW, max(h-6, 1), status)
		} else if status != "" {
			detail = wrapText(status, max(mainW-len(indent), 1))
			for i := range detail {
				detail[i] = indent + detail[i]
			}
		}
		body = rv.body
		selectedLine = rv.sel
	} else {
		// Detail shares the height with its rule, the body and screen chrome.
		overviewBody = true
		ov := m.overview(mainW)
		body, detail = ov.body, ov.detail
		status := m.actionStatus
		if m.confirm != nil {
			status = m.confirm.promptWidth(max(mainW-len(indent), 1))
		}
		if m.pane != nil {
			// The pane opens inline under its row (S106), so it takes no
			// detail lines. At standard terminal heights retain six Overview
			// rows; tiny terminals still give the pane's input and errors their
			// priority, and leave its row and the scroll markers a line each.
			paneHeight := max(h-7, 1)
			if h >= 24 {
				paneHeight = max(h-10, 1)
				// A Running run's second line is one more row line (S145).
				paneHeight = max(paneHeight-ov.paneExtra, 1)
			}
			// A render on show takes the whole body: the Overview rows above
			// the pane give way, so the pane leaves its row for the detail.
			if collapse = m.renderOn() > 0; collapse {
				detail = m.paneLines(mainW, max(h-4, 1), status)
			} else {
				// Four columns in, the pane sits inside its row (S106).
				pane := m.paneLinesAs(max(mainW-len(indent), 1), paneHeight, status, true)
				for i := range pane {
					if pane[i] != "" {
						pane[i] = indent + pane[i]
					}
				}
				at := ov.paneAt
				if at < 0 {
					at = ov.sel // the pane's run left the rows: keep it under the cursor
				}
				at = min(max(at, -1), len(body)-1)
				body = append(append(append([]string(nil), body[:at+1]...), pane...), body[at+1:]...)
				detail = nil
				// Scroll to the pane's last line; its row is a few lines above.
				ov.sel = at + len(pane)
			}
		} else if status != "" {
			detail = wrapText(status, max(mainW-len(indent), 1))
			if m.confirm != nil && m.confirm.verb == "cleanup" {
				detail = m.confirm.cleanupConfirmLines(max(mainW-len(indent), 1), max(h-7, 1))
			}
			for i := range detail {
				detail[i] = indent + detail[i]
			}
		}
		selectedLine = ov.sel
		selExtra = ov.selExtra
	}
	hasDetail := false
	for _, line := range detail {
		if strings.TrimSpace(line) != "" {
			hasDetail = true
			break
		}
	}
	bodyH = max(h-4, 1)
	if collapse && hasDetail {
		// No body: the bar, a rule, the pane, a rule and the status line.
		detail = detail[:min(len(detail), max(h-4, 1))]
		body, bodyH, footer = nil, 0, nil
	} else if hasDetail {
		detail = detail[:min(len(detail), max(h-6, 1))]
		bodyH = max(h-5-len(detail), 1)
	} else {
		detail = nil
	}
	if len(footer) > 0 {
		// The hints stay below the scrolling body; they never take more than half of it.
		if len(footer) > bodyH/2 && footer[0] == "" {
			footer = footer[1:]
		}
		footer = footer[:min(len(footer), max(bodyH/2, 1))]
		bodyH = max(bodyH-len(footer), 1)
	}
	rowLine := selectedLine // the selected row's own line, before the ready row's commands
	if overviewBody {
		if r, ok := m.selectedRow(); ok && r.ready && m.pane == nil {
			// The row's own lines: its commands, and the note under a
			// cleanup-failed row.
			below := len(r.commands)
			if r.note != "" {
				below++
			}
			if bodyH >= below+2 {
				selectedLine += below
			}
		}
		// A Running run's second line scrolls into view with its row (S145).
		if selExtra > 0 && m.pane == nil && bodyH >= 3 {
			selectedLine += selExtra
		}
	}

	if selectedLine >= 0 {
		start = windowStart(selectedLine, len(body), bodyH)
	}
	if bodyH > 0 {
		hi, _, _ := span(len(body), start, bodyH)
		m.drawn = drawnRows{lo: start, hi: hi, sel: rowLine}
		body = window(body, start, bodyH, th)
	}
	_, horizontal := th.rules(m.cfg.ASCII)
	rule := th.Dim.Render(strings.Repeat(horizontal, mainW))
	bar := m.viewBar()
	if lipgloss.Width(bar) > mainW && strings.HasPrefix(bar, " ") {
		bar = bar[1:] // the longer first tab still fits an 80 column screen
	}
	main := []string{bar, rule}
	main = append(main, body...)
	main = append(main, footer...)
	if len(detail) > 0 && bodyH > 0 {
		main = append(main, rule)
	}
	main = append(main, detail...)
	main = append(main, rule, m.statusLine(mainW))
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

// drawnRows is the window of body lines the last screen drew, lines lo up to
// hi, and the body line of the row under the cursor (-1 when none is drawn).
type drawnRows struct{ lo, hi, sel int }

func (m *Model) sidebar(w, h int) []string {
	header := m.cfg.Theme.Title.Render("ork")
	version := ""
	if w > 12 {
		version = lipgloss.NewStyle().MaxWidth(w - 12).Render(m.cfg.Version)
	}
	if version != "" {
		header += " " + m.cfg.Theme.Dim.Render(version)
	}
	lines := []string{header + "  ?help"}
	nav := m.navigation()
	if len(m.projects) == 0 {
		nav = nil
	}
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
	numbers := map[int]int{} // registry index to the number the number key takes
	for i, idx := range m.numberedProjects() {
		numbers[idx] = i + 1
	}
	for _, idx := range nav[start:end] {
		label := "Needs you"
		badge := ""
		marker := " "
		dim := false
		switch idx {
		case runningEntry:
			label = "Running"
			aggregate := *m
			aggregate.active = runningEntry
			n := aggregate.liveRunCount()
			badge = fmt.Sprintf(" %d", n)
			// With nothing running the entry stays, dim, so no row moves.
			dim = n == 0
		case needsEntry:
			aggregate := *m
			aggregate.active = needsEntry
			needs, _ := aggregate.rows()
			badge = fmt.Sprintf(" %d", len(needs))
		}
		if idx >= 0 {
			p := m.projects[idx]
			label = p.reg.Name
			setup := p.setup() || p.snap.Unavailable
			dim = setup
			if p.snap.Unavailable {
				badge = " removed"
			} else if setup {
				badge = " no slicer"
			} else {
				if p.needs() > 0 {
					badge = fmt.Sprintf(" %d", p.needs())
				}
				if p.running {
					marker = "*"
				}
			}
			if m.refreshing(p.reg.Name) {
				marker = "~"
			}
		}
		label = clip(label, max(w-4-lipgloss.Width(badge), 1))
		label += strings.Repeat(" ", max(w-4-lipgloss.Width(label)-lipgloss.Width(badge), 0)) + badge + marker
		// A number takes the selection arrow's place on a project row, and 0
		// on Needs you. A dim row is dimmed whole, so its number is not styled
		// twice.
		prefix := "  "
		number := -1 // the Running entry has none
		if idx >= 0 && numbers[idx] > 0 {
			number = numbers[idx]
		} else if idx == needsEntry {
			number = 0
		}
		if number >= 0 {
			prefix = strconv.Itoa(number) + " "
			if !dim {
				prefix = m.cfg.Theme.Dim.Render(strconv.Itoa(number)) + " "
			}
		}
		if idx == m.active {
			prefix = "> "
		}
		label = prefix + label
		switch {
		case idx == m.active && dim:
			label = m.cfg.Theme.TabActive.Faint(true).Render(label)
		case idx == m.active:
			label = m.cfg.Theme.TabActive.Render(label)
		case dim:
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
	vertical, _ := m.cfg.Theme.rules(m.cfg.ASCII)
	for i, l := range lines {
		l = lipgloss.NewStyle().MaxWidth(max(w-1, 1)).Render(l)
		lines[i] = l + strings.Repeat(" ", max(w-lipgloss.Width(l)-1, 0)) + vertical
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
	{"Questions", true},
	{"Waves", true},
	{"Roadmap", true},
	{"Decisions", true},
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
		if i == 0 && m.active == runningEntry {
			v.name = "Overview (Running)" // the Running entry is the Overview, narrowed
		}
		if i > 0 {
			vertical, _ := th.rules(m.cfg.ASCII)
			idle += " " + vertical + " "
		}
		switch {
		case len(m.projects) == 0:
			idle += v.name
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
func (m *Model) statusLine(w int) string {
	count := m.registeredProjectCount()
	noun := "projects"
	if count == 1 {
		noun = "project"
	}
	refreshed := "never"
	if !m.lastRefresh.IsZero() {
		refreshed = m.lastRefresh.Format("15:04:05")
	}
	fields := []string{
		fmt.Sprintf("%d %s", count, noun),
		"refreshed " + refreshed,
	}
	if pending := m.poller.Pending(); pending > 0 {
		fields = append(fields, fmt.Sprintf("pending %d", pending))
	}
	if r, ok := m.selectedRow(); ok && r.ready && r.merged {
		fields = append(fields, "c clean up")
	}
	// Every daemon snapshot carries global activity, including other projects.
	status := &daemon.StatusResult{}
	for _, project := range m.projects {
		status.RolesRunning = status.RolesRunning || project.snap.RolesRunning
		status.Projects = append(status.Projects, daemon.ProjectStatus{
			SelfHost: project.snap.SelfHost, BehindMain: project.snap.BehindMain,
		})
	}
	if notice := daemon.BehindWarning(status); notice != "" {
		fields = append([]string{m.cfg.Theme.Warn.Render(notice)}, fields...)
	} else if (m.cfg.BuildID != "" || m.cfg.DaemonBuild != "") && daemon.StaleBuild(m.cfg.BuildID, m.cfg.DaemonBuild) {
		// Empty builds are the seam for runs with no daemon info (tests).
		// The behind notice replaces the stale marker while it is shown.
		fields = append([]string{m.cfg.Theme.Warn.Render("daemon build differs: ork daemon restart")}, fields...)
	}
	line := " " + strings.Join(fields, " | ")
	for len(fields) > 1 && lipgloss.Width(line) > w {
		fields = fields[:len(fields)-1]
		line = " " + strings.Join(fields, " | ")
	}
	if m.sidebarHidden && m.cfg.Version != "" && lipgloss.Width(line)+1+lipgloss.Width(m.cfg.Version) <= w {
		line += strings.Repeat(" ", w-lipgloss.Width(line)-lipgloss.Width(m.cfg.Version)) + m.cfg.Theme.Dim.Render(m.cfg.Version)
	}
	return lipgloss.NewStyle().MaxWidth(max(w, 1)).Render(line)
}

// packHints joins keys with "; " into lines of at most w columns, as many to a
// line as fit. A line breaks only between keys, so no key splits from its words.
func packHints(hints []string, w int) []string {
	var lines []string
	line := ""
	for _, h := range hints {
		switch {
		case line == "":
			line = h
		case lipgloss.Width(line)+2+lipgloss.Width(h) <= w:
			line += "; " + h
		default:
			lines = append(lines, clip(line, w))
			line = h
		}
	}
	if line != "" {
		lines = append(lines, clip(line, w))
	}
	return lines
}

// helpLines is the key list shown in place of the Overview.
func (m *Model) helpLines(w, bodyH int) []string {
	th := m.cfg.Theme
	lines := []string{th.Heading.Render("Keys")}
	if m.view == wavesView {
		lines = append(lines, wrapText("Waves: a shows all waves. <S65: shares files with S65, an earlier item.", max(w, 1))...)
	}
	var actionHints []string
	for _, group := range m.keys.FullHelp() {
		for _, binding := range group {
			h := binding.Help()
			// Share a line so the added answer toggle keeps help and quit
			// visible at the standard terminal height.
			if h == m.keys.Quit.Help() {
				continue
			}
			if h == m.keys.Help.Help() {
				q := m.keys.Quit.Help()
				lines = append(lines, clip(q.Key+" "+q.Desc+"; "+h.Key+" "+h.Desc, w))
				continue
			}
			if h.Key == m.keys.AllProjects.Help().Key && m.view != decisionsView {
				continue
			}
			if (h == m.keys.Add.Help() || h == m.keys.Approve.Help()) && (m.view == decisionsView || m.view == questionsView) {
				continue
			}
			if m.view == questionsView && m.pane == nil &&
				(h == m.keys.Stop.Help() || h == m.keys.Retry.Help() || h == m.keys.FileSlice.Help() || h == m.keys.Fix.Help() || h == m.keys.Renders.Help() || h == m.keys.Cleanup.Help()) {
				continue
			}
			// Keep the added question keys and quit visible at standard height.
			if h == m.keys.Approve.Help() || h == m.keys.Stop.Help() || h == m.keys.Retry.Help() || h == m.keys.FileSlice.Help() || h == m.keys.Fix.Help() || h == m.keys.Renders.Help() {
				actionHints = append(actionHints, h.Key+" "+h.Desc)
				continue
			}
			if h == m.keys.Answer.Help() && len(actionHints) > 0 {
				if w < 24 {
					lines = append(lines, clip(strings.Join(actionHints, "; "), w)) // too narrow to pack
				} else {
					lines = append(lines, packHints(actionHints, w)...)
				}
			}
			lines = append(lines, clip(h.Key+" "+h.Desc, w))
		}
	}
	if m.view == decisionsView {
		lines = append(lines, wrapText("On Decisions, left/right switch tabs; [ and ] switch projects.", max(w, 1))...)
	}
	lines = append(lines, "", clip("Overview, Questions, Waves, Roadmap and Decisions are available.", w))
	mark := brand.Mark(!th.Mono, m.cfg.ASCII)
	if w >= 12 && bodyH >= len(lines)+len(mark)+1 {
		lines = append(append(mark, ""), lines...)
	}
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
