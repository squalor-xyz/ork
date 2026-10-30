// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

// reportReader reads one role report on demand (run.report, proposed, S176).
type reportReader interface {
	Report(ctx context.Context, run, name string) (string, error)
}

var _ reportReader = (*Daemon)(nil)

type reportMsg struct {
	run, name, text string
	err             error
}

// Report reads the text of a report the run record lists.
func (d *Daemon) Report(ctx context.Context, run, name string) (string, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.Report(ctx, run, name)
}

// reportList reverses the run's role history, so the newest report is first.
func (m *Model) reportList() []source.EngineRole {
	run := m.paneRun()
	if run == nil {
		return nil
	}
	var out []source.EngineRole
	for i := len(run.Roles) - 1; i >= 0; i-- {
		if run.Roles[i].Report != "" {
			out = append(out, run.Roles[i])
		}
	}
	return out
}

// reportOn is the one-based report on show; a removed report ends the viewer.
func (m *Model) reportOn() int {
	if m.pane == nil || m.pane.reportName == "" {
		return 0
	}
	for i, role := range m.reportList() {
		if role.Report == m.pane.reportName {
			return i + 1
		}
	}
	m.pane.reportName = ""
	return 0
}

func (m *Model) fetchReport() tea.Cmd {
	run, name := m.pane.run, m.pane.reportName
	m.pane.reportLayout = nil
	if m.pane.reports == nil {
		m.pane.reports = map[string]renderFetch{}
	}
	reader, ok := m.cfg.Live.(reportReader)
	if !ok {
		m.pane.reports[name] = renderFetch{done: true, err: "this daemon connection cannot read reports"}
		return nil
	}
	m.pane.reports[name] = renderFetch{}
	return func() tea.Msg {
		text, err := reader.Report(context.Background(), run, name)
		return reportMsg{run: run, name: name, text: text, err: err}
	}
}

func (m *Model) reportArrived(msg reportMsg) {
	if m.pane == nil || m.pane.run != msg.run || m.pane.reportName == "" || m.pane.reportName != msg.name {
		return
	}
	if m.pane.reports == nil {
		m.pane.reports = map[string]renderFetch{}
	}
	f := renderFetch{done: true, text: msg.text}
	if msg.err != nil {
		f.err = actionError(msg.err)
	}
	m.pane.reports[msg.name] = f
}

// reportKey opens the newest report, moves one older, then returns to the run.
// Other action keys cannot act while a report covers the run's details.
func (m *Model) reportKey(msg tea.KeyPressMsg) tea.Cmd {
	if key.Matches(msg, m.keys.Sidebar) {
		m.sidebarHidden = !m.sidebarHidden
		return nil
	}
	if !key.Matches(msg, m.keys.Report) {
		return nil
	}
	reports, idx := m.reportList(), m.reportOn()
	m.pane.offset = 0
	if idx >= len(reports) {
		m.pane.reportName = ""
		return nil
	}
	m.pane.reportName = reports[idx].Report
	m.pane.number = ""
	return m.fetchReport()
}

func (m *Model) reportTitle() string {
	idx := m.reportOn()
	reports := m.reportList()
	role := reports[idx-1]
	return m.cfg.Theme.Heading.Render(fmt.Sprintf("Report %d of %d: %s  %s round %d", idx, len(reports), role.Report, role.Role, role.Round))
}

// reportLayout holds only the current layout. Its text is unstyled and is
// copied before the pane indents it. The source text also identifies a new
// reply for the same report name.
type reportLayout struct {
	name, text    string
	width         int
	sidebarHidden bool
	lines         []string
}

func (m *Model) reportLines(width int) []string {
	name := m.pane.reportName
	f := m.pane.reports[name]
	switch {
	case f.err != "":
		return wrapText("Could not read "+name+": "+f.err, width)
	case !f.done:
		return []string{m.cfg.Theme.Dim.Render("Loading " + name + " ...")}
	}
	width = max(width, 1)
	cached := m.pane.reportLayout
	if cached != nil && cached.name == name && cached.text == f.text && cached.width == width && cached.sidebarHidden == m.sidebarHidden {
		return append([]string(nil), cached.lines...)
	}
	var lines []string
	for _, line := range strings.Split(runrec.PlainText(f.text), "\n") {
		if strings.HasPrefix(line, "|") {
			lines = append(lines, clip(line, width))
		} else {
			lines = append(lines, wrapText(breakReportLine(line, width), width)...)
		}
	}
	m.pane.reportLayout = &reportLayout{name: name, text: f.text, width: width, sidebarHidden: m.sidebarHidden, lines: lines}
	return append([]string(nil), lines...)
}

// breakReportLine bounds the shared wrapper's inputs in one forward pass.
// Prefer a space within the current width; hard-cut a word when none fits.
// Measuring each rune avoids repeatedly measuring the report-sized suffix.
func breakReportLine(line string, width int) string {
	var out strings.Builder
	out.Grow(len(line))
	start, columns := 0, 0       // byte start and display width of the current piece
	space, spaceColumns := -1, 0 // last breakable space and width before it
	trimSpaces := false
	for i, r := range line {
		if trimSpaces && r == ' ' {
			start = i + 1
			continue
		}
		trimSpaces = false
		if r == ' ' && i > start && line[i-1] != ' ' {
			space, spaceColumns = i, columns
		}
		runeWidth := lipgloss.Width(string(r))
		if columns+runeWidth > width {
			if space > start {
				out.WriteString(line[start:space])
				out.WriteByte('\n')
				start = space + 1
				for start <= i && line[start] == ' ' {
					start++
				}
				columns -= spaceColumns + start - space
				space = -1
				if start > i {
					columns, trimSpaces = 0, true
					continue
				}
			}
			if columns+runeWidth > width && i > start {
				out.WriteString(line[start:i])
				out.WriteByte('\n')
				start, columns, space = i, 0, -1
			}
		}
		columns += runeWidth
	}
	out.WriteString(line[start:])
	return out.String()
}
