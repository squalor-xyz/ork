// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// Words and symbols for each state. A state never relies on colour alone.
const (
	kindDecision = "? decision"
	kindHeld     = "# held"
	kindError    = "! error"
	kindPending  = "~ pending"
	kindExternal = "> started elsewhere"

	kindWidth = len(kindDecision)
	indent    = "  "
)

// refreshing reports whether a project that has data is being read again.
func (m *Model) refreshing(name string) bool {
	ps := m.byName[name]
	return ps != nil && ps.loaded && m.poller.InFlight(name)
}

// ovRow is one row of the Overview. The row shows its text cut to the pane. The
// selected row shows the whole text on the detail line (ui.overview-cursor).
type ovRow struct {
	kind             string
	style            lipgloss.Style
	kindW            int
	project          int            // registry index; Enter selects non-run rows
	name             string         // the project's name
	text             string         // the row's text after the name, in full
	id               string         // a run's ID, kept whole in the row; "" for other rows
	rest             string         // a run's base text after its ID, cut first
	stepParts        []relayRowPart // external role details, dropped before cutting base text
	ledgerID         string         // set only for started-elsewhere rows
	engineID         string         // empty for ledger and non-engine rows
	state            string
	pending          string
	commands         []string // plain copyable lines beneath a landing row
	ready            bool
	merged           bool
	mergeKnown       bool
	branch, worktree string
}

// relayRowPart keeps a role's label and identity whole when its task is cut.
type relayRowPart struct {
	header string
	text   string
}

// rows lists Needs you and Running. readyRows follows them in the Overview
// cursor order. The lists do not depend on the width.
func (m *Model) rows() (needs, running []ovRow) {
	th := m.cfg.Theme
	for i, p := range m.projects {
		if m.active >= 0 && i != m.active {
			continue
		}
		name := p.reg.Name
		if !p.loaded {
			pending := ovRow{kind: kindPending, style: th.Pending, kindW: kindWidth, project: i, name: name, text: "waiting for the first reply"}
			needs = append(needs, pending)
			running = append(running, pending)
			continue
		}
		if p.setup() {
			// Shown once: dimmed in the sidebar, and as a hint when selected. It has
			// no rows, so it counts for nothing.
			continue
		}
		s := p.snap
		for _, e := range mergeErrors(s) {
			r := ovRow{kind: kindError, style: th.Error, kindW: kindWidth, project: i, name: name, text: e.text()}
			if e.needs {
				needs = append(needs, r)
			} else {
				running = append(running, r)
			}
		}
		for _, r := range s.Open.Value {
			needs = append(needs, ovRow{kind: kindDecision, style: th.Decision, kindW: kindWidth, project: i, name: name,
				text: fmt.Sprintf("#%d %s  %s", r.ID, r.Key, r.Title)})
		}
		for _, it := range s.Items.Value {
			if isHeld(it) {
				needs = append(needs, ovRow{kind: kindHeld, style: th.Held, kindW: kindWidth, project: i, name: name,
					text: fmt.Sprintf("%s  %s  (%s)", it.ID, it.Title, holdFlag)})
			}
		}

		joined := map[string]bool{}
		for _, r := range s.EngineRuns {
			if r.RelayRun != "" {
				joined[r.RelayRun] = true
			}
			if r.Section(s.EngineRuns) == source.SectionHidden {
				continue
			}
			parts := []string{r.State}
			if r.Wait != "" {
				parts = append(parts, r.Wait)
			}
			if len(r.Denials) > 0 {
				parts = append(parts, "denied: "+strings.Join(r.Denials, ", "))
			}
			began := r.CreatedAt
			for _, ledger := range s.Runs.Value {
				if r.RelayRun == "" || ledger.ID != r.RelayRun {
					continue
				}
				if ledger.Counts.Steps > 0 {
					parts = append(parts, fmt.Sprintf("%d of %d steps running", ledger.Counts.Running, ledger.Counts.Steps))
				}
				if at, err := time.Parse(time.RFC3339, ledger.BeganAt); err == nil {
					began = at
				}
				break
			}
			if !began.IsZero() {
				parts = append(parts, elapsed(m.cfg.Now().Sub(began)))
			}
			rest := strings.Join(parts, "  ")
			row := ovRow{engineID: r.ID, state: r.State, pending: r.Pending, kind: "", style: th.Title, project: i, name: name, id: r.Item, rest: rest, text: r.Item + "  " + rest}
			if r.Section(s.EngineRuns) == source.SectionNeeds {
				row.kindW = kindWidth
				needs = append(needs, row)
			} else {
				running = append(running, row)
			}
		}
		for _, r := range s.Runs.Value {
			if joined[r.ID] {
				continue
			}
			id, rest, steps := runParts(r, m.cfg.Now())
			text := id
			if rest != "" {
				text = id + "  " + rest
			}
			for _, step := range steps {
				text += "  " + step.text
			}
			running = append(running, ovRow{ledgerID: r.ID, kind: kindExternal, style: th.External, kindW: len(kindExternal), project: i, name: name,
				text: text, id: id, rest: rest, stepParts: steps})
		}
	}
	return needs, running
}

// clampCursor keeps a cursor index on one of n rows. It returns -1 when there
// are no rows.
func clampCursor(i, n int) int {
	if n == 0 {
		return -1
	}
	return min(max(i, 0), n-1)
}

// moveRow moves the row cursor by delta rows, stopping at the first and last row.
func (m *Model) moveRow(delta int) {
	needs, running := m.rows()
	n := len(needs) + len(running) + len(m.readyRows())
	if i := clampCursor(m.cursor, n); i >= 0 {
		next := clampCursor(i+delta, n)
		if next != i {
			m.actionStatus = ""
		}
		m.cursor = next
	}
}

// selectedRow is the row under the cursor, and false when there is none.
func (m *Model) selectedRow() (ovRow, bool) {
	needs, running := m.rows()
	all := append(append(append([]ovRow(nil), needs...), running...), m.readyRows()...)
	i := clampCursor(m.cursor, len(all))
	if i < 0 {
		return ovRow{}, false
	}
	return all[i], true
}

// overviewView is the Overview at one width: its body lines, the body line of
// the selected row (-1 when there is none), and the selected row's detail lines.
type overviewView struct {
	body   []string
	sel    int
	detail []string
}

// overview is the Overview screen for all projects, or for the selected one:
// what needs the owner, what is running, and what is ready to land.
func (m *Model) overview(w int) overviewView {
	th := m.cfg.Theme
	nameW := 0
	for _, p := range m.projects {
		nameW = max(nameW, len(p.reg.Name))
	}
	needs, running := m.rows()
	all := append(append(append([]ovRow(nil), needs...), running...), m.readyRows()...)
	cursor := clampCursor(m.cursor, len(all))
	hang := strings.Repeat(" ", len(indent)+1+1+nameW+2)

	var lines []string
	sel := -1
	section := func(rows []ovRow, first int, empty string) {
		if len(rows) == 0 {
			lines = append(lines, indent+th.Dim.Render(empty))
			return
		}
		for i, r := range rows {
			selected := first+i == cursor
			if selected {
				sel = len(lines)
			}
			lines = append(lines, m.rowLine(r, selected, nameW, w))
			for _, command := range r.commands {
				prefix := hang
				if lipgloss.Width(hang+command) > w {
					prefix = indent + indent + indent
				}
				lines = append(lines, prefix+clip(command, w-len(prefix)))
			}
		}
	}
	lines = append(lines, th.Heading.Render("Needs you"))
	section(needs, 0, "nothing needs you")
	lines = append(lines, "", th.Heading.Render("Running"))
	section(running, len(needs), "nothing running")
	lines = append(lines, "", th.Heading.Render("Ready to land"))
	ready := m.readyRows()
	section(ready, len(needs)+len(running), "none")

	ov := overviewView{body: lines, sel: sel}
	switch {
	case m.active >= 0 && m.projects[m.active].setup():
		// The hint is the detail line of a selected project with no rows.
		ov.detail = []string{indent + th.Dim.Render(clip(setupHint, w-len(indent)))}
	case cursor >= 0:
		ov.detail = detailLines(all[cursor], w)
	}
	return ov
}

// rowLine is one row on one line. The kind, the project's name and the text are
// cut to the pane. The selected row is drawn reversed, which works under
// NO_COLOR too.
func (m *Model) rowLine(r ovRow, selected bool, nameW, w int) string {
	th := m.cfg.Theme
	kind := fmt.Sprintf("%-*s", r.kindW, r.kind)
	name := fmt.Sprintf("%-*s", nameW, r.name)
	if selected {
		mark := " "
		if m.refreshing(r.name) {
			mark = "~"
		}
		prefix := indent + kind + " " + mark + name + "  "
		return th.TabActive.Render(prefix + rowText(r, max(w-lipgloss.Width(prefix), 10)))
	}
	// A loaded project with a refresh in flight is marked "~" before its name;
	// the status line says "pending".
	mark := " "
	if m.refreshing(r.name) {
		mark = th.Pending.Render("~")
	}
	prefix := indent + r.style.Render(kind) + " " + mark + name + "  "
	return prefix + rowText(r, max(w-lipgloss.Width(prefix), 10))
}

// rowText is a row's text cut to room columns. A run's ID stays whole: the rest
// of its base text is cut first. External step parts never displace base text.
func rowText(r ovRow, room int) string {
	// Overview rows stay on one line; the pane retains the full Wait text.
	flatten := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")
	r.text, r.rest = flatten.Replace(r.text), flatten.Replace(r.rest)
	if r.id == "" {
		return clip(r.text, room)
	}
	avail := room - lipgloss.Width(r.id) - 2
	switch {
	case r.rest == "":
		return r.id
	case lipgloss.Width(r.rest) <= avail:
		text := r.id + "  " + r.rest
		for _, part := range r.stepParts {
			available := room - lipgloss.Width(text) - 2
			required := lipgloss.Width(part.header)
			if part.text != part.header {
				// Keep at least three task columns plus the ellipsis, or a whole
				// shorter task. The label, role and vendor/model are never cut.
				required += 2 + min(6, lipgloss.Width(part.text)-lipgloss.Width(part.header)-2)
			}
			if available < required {
				break
			}
			text += "  " + clip(part.text, available)
		}
		return text
	case avail < 4:
		return r.id
	}
	return r.id + "  " + clip(r.rest, avail)
}

// detailLines wraps a Ready row's full commands, or another row's full text,
// at the pane width. The row already shows the project's name.
func detailLines(r ovRow, w int) []string {
	text := r.text
	if r.ready && len(r.commands) > 0 {
		text = strings.Join(r.commands, "\n")
	}
	lines := wrapText(text, max(w-len(indent), 1))
	for i := range lines {
		lines[i] = indent + lines[i]
	}
	return lines
}

// runParts describes a relay run started outside ork. ork only shows it. The
// run ID stays whole. Items, state and elapsed retain their existing clipping;
// running and failed parts are dropped first, or only their tasks are cut.
func runParts(r slicercli.RelayRun, now time.Time) (id, rest string, steps []relayRowPart) {
	state := r.State
	if r.Counts.Steps > 0 {
		state = fmt.Sprintf("%s, %d of %d steps running", r.State, r.Counts.Running, r.Counts.Steps)
	}
	parts := []string{strings.Join(r.Items, ","), state}
	if t, err := time.Parse(time.RFC3339, r.BeganAt); err == nil {
		parts = append(parts, elapsed(now.Sub(t)))
	}

	// The last start wins so a subagent is shown ahead of its orchestrator.
	var running, failed *slicercli.Step
	for i := range r.Steps {
		step := &r.Steps[i]
		switch step.Phase {
		case "start":
			running = step
		case "fail":
			failed = step
		}
	}

	for _, selected := range []struct {
		label string
		step  *slicercli.Step
	}{{"running", running}, {"failed", failed}} {
		if selected.step == nil {
			continue
		}
		step := selected.step
		summary := daemon.RelayStep{Role: step.Role, Vendor: step.Vendor, Model: step.Model, Task: step.Task, Phase: step.Phase}
		text := daemon.FormatRelayStep(selected.label, summary)
		summary.Task = ""
		steps = append(steps, relayRowPart{header: daemon.FormatRelayStep(selected.label, summary), text: text})
	}
	return r.ID, strings.Join(parts, "  "), steps
}

func elapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// setupHint is the line a selected project with no .slicer/ folder shows.
const setupHint = "no .slicer/: run `slicer init` to track items and runs."

// errRow is the one row a project shows for one error code: the calls that
// failed with that code, and the tool's message (or messages) as printed.
type errRow struct {
	code  string
	parts []string // "items", "inbox" or "runs", in call order
	msgs  []string // distinct messages, in call order
	needs bool     // an items or inbox call failed: the row is under Needs you
}

func (r errRow) text() string {
	code := ""
	if r.code != "" {
		code = r.code + ": "
	}
	return strings.Join(r.parts, ", ") + ": " + code + strings.Join(r.msgs, " / ")
}

// mergeErrors makes one row per error code from a snapshot's failed calls, in
// call order: items, inbox, then runs.
func mergeErrors(s source.Snapshot) []errRow {
	calls := []struct {
		part  string
		needs bool
		err   error
	}{
		{"items", true, s.Items.Err},
		{"inbox", true, s.Open.Err},
		{"runs", false, s.Runs.Err},
	}
	var rows []errRow
	for _, c := range calls {
		if c.err == nil {
			continue
		}
		code, msg := errParts(c.err)
		i := slices.IndexFunc(rows, func(r errRow) bool { return r.code == code })
		if i < 0 {
			rows = append(rows, errRow{code: code})
			i = len(rows) - 1
		}
		r := &rows[i]
		r.parts = append(r.parts, c.part)
		r.needs = r.needs || c.needs
		if !slices.Contains(r.msgs, msg) {
			r.msgs = append(r.msgs, msg)
		}
	}
	return rows
}

// errParts is a failed call as the tool printed it: the error code and message
// of a tool error. A call that failed without one has no code, and its error
// text is the message. Line breaks become spaces; nothing is reworded.
func errParts(err error) (code, msg string) {
	var te *clirun.ToolError
	if errors.As(err, &te) {
		code, msg = te.Code, te.Message
	} else {
		msg = err.Error()
	}
	return code, strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(msg)
}

// clip cuts s to n columns, marking the cut with "...".
func clip(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+3 > n {
		r = r[:len(r)-1]
	}
	return string(r) + "..."
}

// wrapText preserves explicit line breaks and wraps each line to n columns.
// An added break drops a single space, or splits a word longer than n.
func wrapText(s string, n int) []string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s)
	if strings.Contains(s, "\n") {
		var out []string
		for _, line := range strings.Split(s, "\n") {
			out = append(out, wrapText(line, n)...)
		}
		return out
	}
	var out []string
	for lipgloss.Width(s) > n {
		if i := breakAt(s, n); i > 0 {
			out, s = append(out, s[:i]), s[i+1:]
			continue
		}
		head, rest := splitAt(s, n)
		out, s = append(out, head), rest
	}
	return append(out, s)
}

// breakAt is the index of the space to break s at: the last single space that
// leaves at most n columns before it, or -1 when there is none.
func breakAt(s string, n int) int {
	best := -1
	for i := 1; i < len(s)-1; i++ {
		if s[i] != ' ' || s[i-1] == ' ' {
			continue
		}
		if lipgloss.Width(s[:i]) > n {
			break
		}
		best = i
	}
	return best
}

// splitAt cuts s after the longest prefix that is at most n columns wide.
func splitAt(s string, n int) (head, rest string) {
	r := []rune(s)
	i := len(r)
	for i > 1 && lipgloss.Width(string(r[:i])) > n {
		i--
	}
	return string(r[:i]), string(r[i:])
}

// readyRows follows Running in the cursor order and retains the landing layout.
func (m *Model) readyRows() []ovRow {
	var ready []ovRow
	for i, p := range m.projects {
		if (m.active >= 0 && i != m.active) || !p.loaded || p.setup() {
			continue
		}
		for _, run := range p.snap.EngineRuns {
			if run.State != "committed" {
				continue
			}
			rest := run.Branch
			var commands []string
			switch {
			case run.Branch == "":
				rest = "branch unknown"
			case run.Merged != nil && !*run.Merged:
				rest += "  merge first"
			default:
				commands = append(commands, fmt.Sprintf("slicer land %s --branch %s --note %s", shellQuote(run.Item), shellQuote(run.Branch), shellQuote("Landed "+run.Branch)))
				if run.Merged == nil {
					rest += "  merge state unknown"
				} else {
					rest += "  merged"
					commands = append(commands, "slicer render")
				}
			}
			ready = append(ready, ovRow{mergeKnown: run.Merged != nil, ready: true, engineID: run.ID, state: run.State, merged: run.Merged != nil && *run.Merged, branch: run.Branch, worktree: run.Worktree, project: i, name: p.reg.Name, style: m.cfg.Theme.Title,
				id: run.Item, rest: rest, text: run.Item + "  " + rest, commands: commands})
		}
	}
	return ready
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }
