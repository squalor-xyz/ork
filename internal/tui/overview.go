// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/adjcli"
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
	group             string
	decision          *adjcli.Record
	keyW, decisionIDW int
	dim               bool
	cells             []string // proposed: whole board summary cells
	ended             time.Time
	kind              string
	style             lipgloss.Style
	kindW             int
	noLabel           bool           // a Running engine row has no label column
	project           int            // registry index; Enter selects non-run rows
	name              string         // the project's name
	text              string         // the row's text after the name, in full
	id                string         // a run's ID, kept whole in the row; "" for other rows
	rest              string         // a run's base text after its ID, cut first
	stepParts         []relayRowPart // external role details, dropped before cutting base text
	ledgerID          string         // set only for started-elsewhere rows
	unavailable       bool
	failure           string
	engineID          string // empty for ledger and non-engine rows
	state             string
	pending           string
	commands          []string // plain copyable lines beneath a landing row
	note              string   // a line beneath a landing row that is not a command (why a cleanup failed)
	detail            string   // the selected landing row's detail, when it is not its commands
	ready             bool
	merged            bool
	mergeKnown        bool
	branch, worktree  string
	live              *liveRun // a live run's board cells, for the Running entry
	detailCells       []string // a live run's detail line, wrapped between cells
}

// liveRun is what the Running entry shows of one run beyond its Overview row:
// the stage bar, pipeline, round, last heard and activity (S110).
type liveRun struct {
	item     string
	ledger   slicercli.RelayRun
	steps    []runTreeStep
	activity []int
	alert    string // "? question" or "x stopped" when the run waits on the owner
}

// relayRowPart keeps a role's label and identity whole when its task is cut.
type relayRowPart struct {
	header string
	text   string
}

// rows lists Needs you and Running. recentRows and then readyRows follow
// them in the Overview cursor order. The lists do not depend on the width.
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
				decision: &r, text: fmt.Sprintf("#%d %s  %s", r.ID, r.Key, r.Title)})
		}
		for _, it := range s.Items.Value {
			if isHeld(it) {
				needs = append(needs, ovRow{kind: kindHeld, style: th.Held, kindW: kindWidth, project: i, name: name,
					text: fmt.Sprintf("%s  %s", it.ID, it.Title)})
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
			displayState := r.State
			var linkedStale *slicercli.RelayRun
			for _, ledger := range boardLedgers(s) {
				if r.RelayRun != "" && ledger.ID == r.RelayRun && ledger.State == "stale" {
					staleLedger := ledger
					linkedStale = &staleLedger
					displayState = "stale"
					break
				}
			}
			parts := []string{displayState}
			if r.Wait != "" && showsWait(r) {
				parts = append(parts, r.Wait)
			}
			if len(r.Denials) > 0 {
				parts = append(parts, "denied: "+source.DenialsText(r.Denials))
			}
			if r.Error != "" {
				parts = append(parts, "error: "+r.Error)
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
			if r.State == "stopped" {
				if !r.UpdatedAt.IsZero() {
					parts = append(parts, age(m.cfg.Now().Sub(r.UpdatedAt)))
				}
			} else if !began.IsZero() {
				end := m.cfg.Now()
				if linkedStale != nil {
					end = runClock(*linkedStale, end)
					if at, e := time.Parse(time.RFC3339Nano, linkedStale.BeganAt); e == nil {
						began = at
					}
				}
				parts = append(parts, elapsed(end.Sub(began)))
			}
			rest := strings.Join(parts, "  ")
			row := ovRow{unavailable: r.Unavailable, failure: r.Error, engineID: r.ID, state: r.State, pending: r.Pending, kind: "", style: th.Title, project: i, name: name, id: r.ID, rest: r.Item + "  " + rest, text: r.ID + "  " + r.Item + "  " + rest, dim: linkedStale != nil}
			ledger, steps := engineBoard(r)
			for _, linked := range boardLedgers(s) {
				if r.RelayRun != "" && linked.ID == r.RelayRun {
					ledger = linked
					steps = ledgerTreeSteps(linked.Steps)
					break
				}
			}
			if !r.Unavailable {
				row.live = &liveRun{item: r.Item, ledger: ledger, steps: steps, activity: r.Activity}
			}
			if r.Section(s.EngineRuns) == source.SectionRunning {
				row.noLabel = r.Error == ""
				if r.Error != "" {
					row.kindW = len(kindExternal)
					row.kind, row.style = kindError, th.Error
				}
				row.stepParts = m.boardRowParts(ledger, steps)
				if r.UsageKnown || r.Tokens != 0 {
					tokenText := fmt.Sprintf("tokens %d", r.Tokens)
					// Trailing cells drop first: tokens, heard, pipeline, then bar.
					row.stepParts = append(row.stepParts, relayRowPart{header: tokenText, text: tokenText})
				}
				for _, part := range row.stepParts {
					row.text += "  " + part.text
				}
			}
			if r.Section(s.EngineRuns) == source.SectionNeeds {
				row.kindW = kindWidth
				switch {
				case r.Unavailable:
					row.kind, row.style = "! removed", th.Error
				case r.State == "stopped":
					row.kind, row.style = "x stopped", th.Error
				case r.State == "paused" && r.Pending == "question":
					row.kind, row.style = "? question", th.Warn.Bold(th.Mono)
				case r.State == "paused":
					row.kind, row.style = "= paused", th.Held
				case r.State == "awaiting-approval":
					row.kind, row.style = "+ approve", th.Decision
				}
				if row.live != nil && (row.kind == "? question" || row.kind == "x stopped") {
					row.live.alert = row.kind
				}
				needs = append(needs, row)
			} else {
				running = append(running, row)
			}
		}
		for _, r := range boardLedgers(s) {
			if r.State == "ended" {
				continue
			}
			if joined[r.ID] {
				continue
			}
			id, rest, steps := runParts(r, m.cfg.Now())
			text := id
			if rest != "" {
				text = id + "  " + rest
			}
			boardParts := m.boardRowParts(r, ledgerTreeSteps(r.Steps))
			steps = append(append([]relayRowPart{boardParts[0]}, steps...), boardParts[1:]...)
			for _, step := range steps {
				text += "  " + step.text
			}
			style := th.External
			if r.State == "stale" {
				style = th.Dim
			}
			running = append(running, ovRow{ledgerID: r.ID, state: r.State, kind: kindExternal, style: style, kindW: len(kindExternal), project: i, name: name,
				text: text, id: id, rest: rest, stepParts: steps, dim: r.State == "stale",
				live: &liveRun{item: strings.Join(r.Items, ","), ledger: r, steps: ledgerTreeSteps(r.Steps)}})
		}
	}
	// Engine rows drop the empty label only while no other Running row has one.
	if slices.ContainsFunc(running, func(r ovRow) bool { return r.kind != "" }) {
		for i := range running {
			if running[i].noLabel {
				running[i].noLabel, running[i].kindW = false, len(kindExternal)
			}
		}
	}
	for i := range needs {
		switch needs[i].kind {
		case kindDecision:
			needs[i].group = "Decisions"
		case kindHeld:
			needs[i].group = "Held"
		case "x stopped":
			needs[i].group = "Stopped"
		case "= paused", "? question":
			needs[i].group = "Paused"
		case "+ approve":
			needs[i].group = "Approve"
		default:
			needs[i].group = "Errors"
		}
	}
	slices.SortStableFunc(needs, func(a, b ovRow) int { return slices.Index(needsGroups, a.group) - slices.Index(needsGroups, b.group) })
	return needs, running
}

var needsGroups = []string{"Decisions", "Held", "Stopped", "Paused", "Approve", "Errors"}

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
	n := len(m.cursorRows())
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
	if m.view == decisionsView || m.view == questionsView {
		return ovRow{}, false
	}
	all := m.cursorRows()
	i := clampCursor(m.cursor, len(all))
	if i < 0 {
		return ovRow{}, false
	}
	return all[i], true
}

// cursorRows lists the rows the row cursor walks, in order: Needs you,
// Running, Recent and Ready, or on the Running entry its own rows and Recent.
func (m *Model) cursorRows() []ovRow {
	if m.active == runningEntry {
		return append(m.runningRows(), m.recentRows()...)
	}
	needs, running := m.rows()
	return append(append(append(append([]ovRow(nil), needs...), running...), m.recentRows()...), m.readyRows()...)
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
	if m.active == runningEntry {
		return m.runningOverview(w)
	}
	th := m.cfg.Theme
	nameW := 0
	needs, running := m.rows()
	keyW, idW := 0, 0
	for _, r := range needs {
		if r.decision != nil {
			keyW = max(keyW, lipgloss.Width(r.decision.Key))
			idW = max(idW, len(fmt.Sprint(r.decision.ID))+1)
		}
	}
	keyW = min(keyW, w/3)
	for i := range needs {
		needs[i].keyW = keyW
		needs[i].decisionIDW = idW
	}
	all := append(append(append(append([]ovRow(nil), needs...), running...), m.recentRows()...), m.readyRows()...)
	cursor := clampCursor(m.cursor, len(all))
	if m.active < 0 {
		for _, r := range all {
			nameW = max(nameW, lipgloss.Width(r.name))
		}
	}

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
			textColumn := len(indent) + r.kindW + 1
			if r.noLabel {
				textColumn = len(indent)
			}
			if r.group != "" {
				textColumn = len(indent) + 2
			}
			if nameW > 0 {
				textColumn += 1 + nameW + 2
			}
			hang := strings.Repeat(" ", textColumn)
			for _, command := range r.commands {
				prefix := hang
				if lipgloss.Width(hang+command) > w {
					prefix = indent + indent + indent
				}
				lines = append(lines, prefix+clip(command, w-len(prefix)))
			}
			if r.note != "" {
				lines = append(lines, hang+th.Dim.Render(clip(r.note, max(w-len(hang), 1))))
			}
		}
	}
	lines = append(lines, th.Heading.Render(fmt.Sprintf("Needs you %d", len(needs))))
	if len(needs) == 0 {
		section(nil, 0, "nothing needs you")
	}
	first := 0
	for _, group := range needsGroups {
		var rows []ovRow
		for _, r := range needs {
			if r.group == group {
				rows = append(rows, r)
			}
		}
		if len(rows) == 0 {
			continue
		}
		lines = append(lines, indent+th.Group.Render(fmt.Sprintf("%s %d", group, len(rows))))
		section(rows, first, "")
		first += len(rows)
	}
	summary := th.Heading.Render(fmt.Sprintf("Running %d", len(running)))
	questions := 0
	for _, r := range needs {
		if r.pending == "question" {
			questions++
		}
	}
	if questions > 0 {
		summary += "  " + th.Warn.Bold(th.Mono).Render(fmt.Sprintf("! owner questions %d", questions))
	}
	lines = append(lines, "", summary)
	section(running, len(needs), "nothing running")
	lines = append(lines, "", th.Heading.Render("Recent"))
	recent := m.recentRows()
	section(recent, len(needs)+len(running), "none")
	lines = append(lines, "", th.Heading.Render("Ready to land"))
	ready := m.readyRows()
	section(ready, len(needs)+len(running)+len(recent), "none")

	ov := overviewView{body: lines, sel: sel}
	switch {
	case m.active >= 0 && m.projects[m.active].setup():
		// The hint is the detail line of a selected project with no rows.
		ov.detail = []string{indent + th.Dim.Render(clip(setupHint, w-len(indent)))}
	case cursor >= 0:
		vertical, _ := th.rules(m.cfg.ASCII)
		ov.detail = detailLines(all[cursor], w, vertical)
	}
	return ov
}

// rowLine is one row on one line. The kind, the project's name and the text are
// cut to the pane. The selected row is drawn reversed, which works under
// NO_COLOR too.
func (m *Model) rowLine(r ovRow, selected bool, nameW, w int) string {
	th := m.cfg.Theme
	kind := fmt.Sprintf("%-*s", r.kindW, r.kind)
	if r.group != "" {
		kind = r.kind[:1]
	}
	name := ""
	if nameW > 0 {
		name = fmt.Sprintf("%-*s", nameW, r.name) + "  "
	}
	if r.decision != nil {
		d := r.decision
		id := fmt.Sprintf("#%d", d.ID)
		r.text = fmt.Sprintf("%-*s  %-*s  %s", r.decisionIDW, id, r.keyW, clip(d.Key, r.keyW), d.Title)
	}
	if selected {
		mark := " "
		if nameW > 0 && m.refreshing(r.name) {
			mark = "~"
		}
		prefix := indent + kind + " "
		if r.noLabel {
			prefix = indent
		}
		if nameW > 0 {
			prefix += mark + name
		}
		line := prefix + rowText(r, max(w-lipgloss.Width(prefix), 1))
		line += strings.Repeat(" ", max(w-lipgloss.Width(line), 0))
		return th.TabActive.Render(line)
	}
	// A loaded project with a refresh in flight is marked "~" before its name;
	// the status line says "pending".
	mark := " "
	if nameW > 0 && m.refreshing(r.name) {
		mark = th.Pending.Render("~")
	}
	styledKind := kind
	if kind != "" {
		styledKind = r.style.Render(kind)
	}
	prefix := indent + styledKind + " "
	if r.noLabel {
		prefix = indent
	}
	if nameW > 0 {
		prefix += mark + name
	}
	text := rowText(r, max(w-lipgloss.Width(prefix), 1))
	heard := ""
	heardStyle := th.Warn
	for _, part := range r.stepParts {
		cell := part.text
		if strings.Contains(text, cell) && (strings.HasPrefix(cell, "! heard ") || strings.HasPrefix(cell, "!! heard ")) {
			heard = cell
			if strings.HasPrefix(cell, "!! heard ") {
				heardStyle = th.Error
			}
			break
		}
	}
	if heard != "" {
		before, after, _ := strings.Cut(text, heard)
		if r.dim {
			before = th.Dim.Render(before)
			if after != "" {
				after = th.Dim.Render(after)
			}
		}
		text = before + heardStyle.Render(heard) + after
	} else if r.dim {
		text = th.Dim.Render(text)
	}
	if len(r.cells) > 0 {
		outcome := r.cells[0]
		style := th.Dim
		if strings.HasPrefix(outcome, "✓") {
			style = th.Passed
		}
		if strings.HasPrefix(outcome, "✗") {
			style = th.Error
		}
		text = style.Render(outcome) + th.Dim.Render(strings.TrimPrefix(text, outcome))
	}
	return prefix + text
}

// rowText is a row's text cut to room columns. A run's ID stays whole: the rest
// of its base text is cut first. External step parts never displace base text.
func rowText(r ovRow, room int) string {
	if len(r.cells) > 0 {
		cells := r.cells
		for len(cells) > 1 && lipgloss.Width(strings.Join(cells, "  ")) > room {
			cells = cells[:len(cells)-1]
		}
		return strings.Join(cells, "  ")
	}
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
func detailLines(r ovRow, w int, separator string) []string {
	if len(r.detailCells) > 0 {
		lines := packCells(r.detailCells, max(w-len(indent), 1))
		for i := range lines {
			lines[i] = indent + lines[i]
		}
		return lines
	}
	text := r.text
	if d := r.decision; d != nil {
		text = fmt.Sprintf("#%d %s", d.ID, d.Status)
		if len(d.Options) > 0 {
			text += fmt.Sprintf(" %s %d options", separator, len(d.Options))
		}
		if d.Recommendation != nil && d.Recommendation.Label != "" {
			text += " " + separator + " recommends " + d.Recommendation.Label
		}
		text += "\n" + d.Title
	}
	if r.ready && len(r.commands) > 0 {
		text = strings.Join(r.commands, "\n")
	}
	if r.ready && r.detail != "" {
		text = r.detail
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
		parts = append(parts, elapsed(runClock(r, now).Sub(t)))
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

// age shows the largest whole unit since a stopped run's last update.
func age(d time.Duration) string {
	d = max(d, 0)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d >= time.Minute:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	default:
		return fmt.Sprintf("%ds ago", int(d/time.Second))
	}
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
			if run.State != "committed" || (run.Landed && run.CleanupFailed == "") {
				continue // ork landed it (S119): nothing is left to land
			}
			rest := run.Branch
			var commands []string
			var note, detail string
			switch {
			case run.Landed:
				// Landed, but the cleanup failed: no landing command. The reason is a
				// note under the row, not a copyable line; the detail names the
				// cleanup key (c).
				rest += "  landed  cleanup failed"
				note = strings.TrimPrefix(strings.TrimPrefix(run.CleanupFailed, "landed; "), "cleanup failed at ")
				detail = "c finishes the cleanup: " + strings.ReplaceAll(cleanupCommand(m.projects[i].reg.Path, run.Worktree, run.Branch), "\n", "; ")
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
				id: run.Item, rest: rest, text: run.Item + "  " + rest, commands: commands, note: note, detail: detail})
		}
	}
	return ready
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }

// showsWait reports whether the run's wait text still applies. A run that
// resumed after an answer keeps no question in its row or pane; the removed
// project help always applies.
func showsWait(r source.EngineRun) bool {
	return r.Unavailable || r.State == "paused" || r.State == "stopped" || r.State == "awaiting-approval"
}

// runningRows lists the Running entry's rows (S110, adj 169): the Running
// section of the aggregate Overview, plus every run that waits on the owner
// with a question or a stop, which Needs you holds. They stay in project
// order, so a run that starts waiting does not change the order of the rows.
func (m *Model) runningRows() []ovRow {
	needs, running := m.rows()
	rows := append([]ovRow(nil), running...)
	for _, r := range needs {
		if r.live != nil && r.live.alert != "" {
			rows = append(rows, r)
		}
	}
	slices.SortStableFunc(rows, func(a, b ovRow) int { return a.project - b.project })
	for i := range rows {
		if rows[i].live != nil {
			rows[i].detailCells = m.liveDetail(rows[i])
			rows[i].text = strings.Join(rows[i].detailCells, "  ")
		}
	}
	return rows
}

// liveRunCount is the number of runs the Running entry lists. An error or a
// pending project is a row there too, but it is not a run.
func (m *Model) liveRunCount() int {
	n := 0
	for _, r := range m.runningRows() {
		if r.live != nil {
			n++
		}
	}
	return n
}

// liveDetail is the whole text of a Running row, for the detail line, as the
// cells it is wrapped between: the Overview text, the board cells the row
// lacks and the activity.
func (m *Model) liveDetail(r ovRow) []string {
	cells := strings.Split(r.text, "  ")
	if r.live.alert != "" {
		// The Needs you row carries no board cells.
		for _, part := range m.boardRowParts(r.live.ledger, r.live.steps) {
			cells = append(cells, part.text)
		}
	}
	if len(r.live.activity) > 0 {
		cells = append(cells, "activity "+sparkline(r.live.activity, m.cfg.ASCII))
	}
	return cells
}

// packCells wraps cells into lines of at most n columns, two spaces apart. A
// line breaks between cells only; a cell wider than n is wrapped alone.
func packCells(cells []string, n int) []string {
	var lines []string
	line := ""
	for _, c := range cells {
		if c == "" {
			continue
		}
		switch {
		case line == "":
			line = c
		case lipgloss.Width(line)+2+lipgloss.Width(c) <= n:
			line += "  " + c
		default:
			lines = append(lines, wrapText(line, n)...)
			line = c
		}
	}
	if line != "" {
		lines = append(lines, wrapText(line, n)...)
	}
	return lines
}

// runningRole is the role that is running, in brackets as the pipeline draws
// it, or "" when none is.
func runningRole(steps []runTreeStep) string {
	phases := pipelinePhases(steps)
	for _, role := range pipelineRoles(laneSteps(steps)) {
		if phases[role] == "start" {
			return "[" + role + "]"
		}
	}
	return ""
}

// runningOverview is the Running entry's screen: the live runs of every
// project, each on one line, and the runs that ended in the last hour. It
// reuses the Overview's rows and its cursor; nothing else is drawn.
func (m *Model) runningOverview(w int) overviewView {
	th := m.cfg.Theme
	running, recent := m.runningRows(), m.recentRows()
	all := append(append([]ovRow(nil), running...), recent...)
	cursor := clampCursor(m.cursor, len(all))
	nameW := 0
	for _, r := range all {
		nameW = max(nameW, lipgloss.Width(r.name))
	}
	live, questions, stops := 0, 0, 0
	var liveRows []ovRow
	for _, r := range running {
		if r.live == nil {
			continue
		}
		live++
		liveRows = append(liveRows, r)
		switch r.live.alert {
		case "? question":
			questions++
		case "x stopped":
			stops++
		}
	}
	// One set of cells for every row, chosen from the widest.
	layout := m.liveLayout(liveRows, nameW, w)
	heading := th.Heading.Render(fmt.Sprintf("Running %d", live))
	if questions > 0 {
		heading += "  " + th.Warn.Bold(th.Mono).Render(fmt.Sprintf("! owner questions %d", questions))
	}
	if stops > 0 {
		heading += "  " + th.Error.Render(fmt.Sprintf("x stopped %d", stops))
	}
	lines := []string{heading}
	sel := -1
	if live == 0 {
		lines = append(lines, indent+th.Dim.Render("nothing running"))
	}
	for i, r := range running {
		if i == cursor {
			sel = len(lines)
		}
		if r.live != nil {
			lines = append(lines, m.runningLine(r, i == cursor, nameW, w, layout))
		} else {
			lines = append(lines, m.rowLine(r, i == cursor, nameW, w))
		}
	}
	lines = append(lines, "", th.Heading.Render("Recent"))
	if len(recent) == 0 {
		lines = append(lines, indent+th.Dim.Render("none"))
	}
	for i, r := range recent {
		if len(running)+i == cursor {
			sel = len(lines)
		}
		lines = append(lines, m.rowLine(r, len(running)+i == cursor, nameW, w))
	}
	ov := overviewView{body: lines, sel: sel}
	if cursor >= 0 {
		vertical, _ := th.rules(m.cfg.ASCII)
		ov.detail = detailLines(all[cursor], w, vertical)
	}
	return ov
}

// liveText is the text of the cells of one Running row.
type liveText struct {
	item, bar, role, pipeline, heard, spark string
	heardStyle                              lipgloss.Style
}

func (m *Model) liveText(r ovRow) liveText {
	lv := r.live
	stale := lv.ledger.State == "stale"
	t := liveText{item: lv.item, pipeline: m.pipeline(lv.steps, stale, true)}
	t.bar = stageBar(lv.steps, m.treeFrame, !stale && boardLive(lv.ledger.State))
	if !stale {
		t.role = runningRole(lv.steps)
	}
	t.heard, t.heardStyle = m.lastHeard(lv.ledger)
	if len(lv.activity) > 0 {
		t.spark = sparkline(lv.activity, m.cfg.ASCII)
	}
	return t
}

// liveLayout is the cells every Running row shows and the width of each
// column, chosen once from the widest row so the columns line up and no row
// shows a cell another lacks.
type liveLayout struct {
	itemW, barW, roleW, pipelineW, heardW, sparkW int
	pipeline, heard, spark                        bool
}

// liveLayout keeps the item, the stage bar and the running role. As the pane
// narrows the cells go in this order: activity, the pipeline (down to the
// running role), last heard.
func (m *Model) liveLayout(rows []ovRow, nameW, w int) liveLayout {
	var l liveLayout
	for _, r := range rows {
		t := m.liveText(r)
		l.itemW = max(l.itemW, lipgloss.Width(t.item))
		l.barW = max(l.barW, lipgloss.Width(t.bar))
		l.roleW = max(l.roleW, lipgloss.Width(t.role))
		l.pipelineW = max(l.pipelineW, lipgloss.Width(t.pipeline))
		l.heardW = max(l.heardW, lipgloss.Width(t.heard))
		l.sparkW = max(l.sparkW, lipgloss.Width(t.spark))
	}
	room := w - len(indent) - 1 - nameW - 2
	width := func() int {
		n := l.itemW + 2 + l.barW
		if l.pipeline {
			n += 2 + l.pipelineW
		} else if l.roleW > 0 {
			n += 2 + l.roleW
		}
		if l.heard {
			n += 2 + l.heardW
		}
		if l.spark {
			n += 2 + l.sparkW
		}
		return n
	}
	if l.heard = l.heardW > 0; l.heard && width() > room {
		l.heard = false
	}
	if l.pipeline = true; width() > room {
		l.pipeline = false
	}
	if l.spark = l.sparkW > 0; l.spark && width() > room {
		l.spark = false
	}
	return l
}

// runningLine is one live run on one line: the alert glyph and the project,
// then the item, the stage bar (with its round), the pipeline or the running
// role, last heard and activity, in the columns and cells of layout.
func (m *Model) runningLine(r ovRow, selected bool, nameW, w int, l liveLayout) string {
	th := m.cfg.Theme
	t := m.liveText(r)
	// The mark slot holds the alert glyph, as the kind symbol does on Needs you.
	mark, markStyle := " ", lipgloss.NewStyle()
	switch {
	case r.live.alert != "":
		mark, markStyle = r.live.alert[:1], r.style
	case m.refreshing(r.name):
		mark, markStyle = "~", th.Pending
	}
	padded := fmt.Sprintf("%-*s", nameW, r.name)

	type col struct {
		text  string
		width int
		style lipgloss.Style
	}
	plain := lipgloss.NewStyle()
	cols := []col{{t.item, l.itemW, th.Title}, {t.bar, l.barW, plain}}
	switch {
	case l.pipeline:
		cols = append(cols, col{t.pipeline, l.pipelineW, plain})
	case l.roleW > 0:
		cols = append(cols, col{t.role, l.roleW, plain})
	}
	if l.heard {
		cols = append(cols, col{t.heard, l.heardW, t.heardStyle})
	}
	if l.spark {
		cols = append(cols, col{t.spark, l.sparkW, th.Dim})
	}
	var out []string
	var raw []string
	for _, c := range cols {
		pad := strings.Repeat(" ", max(c.width-lipgloss.Width(c.text), 0))
		style := c.style
		if r.dim && !strings.HasPrefix(c.text, "!") {
			style = th.Dim
		}
		raw = append(raw, c.text+pad)
		if c.text == "" {
			out = append(out, pad)
			continue
		}
		out = append(out, style.Render(c.text)+pad)
	}
	if selected {
		line := indent + mark + padded + "  " + strings.Join(raw, "  ")
		line = strings.TrimRight(line, " ")
		if lipgloss.Width(line) > w {
			line = clip(line, w)
		}
		return th.TabActive.Render(line + strings.Repeat(" ", max(w-lipgloss.Width(line), 0)))
	}
	if mark != " " {
		mark = markStyle.Render(mark)
	}
	return strings.TrimRight(indent+mark+padded+"  "+strings.Join(out, "  "), " ")
}
