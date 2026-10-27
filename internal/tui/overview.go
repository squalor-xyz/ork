// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// Words and symbols for each state. A state never relies on colour alone.
const (
	kindDecision = "? decision"
	kindHeld     = "# held"
	kindError    = "! error"
	kindPending  = "~ pending"

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
	worktreeGone      bool // the worktree path no longer exists (S153)
	branch, worktree  string
	compact           bool       // a run row on a narrow terminal drops its run ID and vendor/model
	live              *liveRun   // a live run's board cells, for the Running entry
	detailCells       []string   // a live run's detail line, wrapped between cells
	run               *runRow    // a Running run's two-line form (S145)
	layout            *runLayout // the Running section's shared line-1 layout (S145)
}

// runRow is a Running run drawn on two lines (S145). Line 1 holds the slice id,
// its short title, the role working now with its model, the stage bar with its
// round word and last heard. Line 2 is that role's latest output line. The run
// id and "started elsewhere" are not in the row; the pane names them.
type runRow struct {
	id, title   string
	role, model string // the working role, or the state word when none works
	stateOnly   bool   // role is a state word, not a role: drawn dim
	bar         string
	heard       string
	heardStyle  lipgloss.Style
	tokens      string // an engine run's "tokens N", the first cell to drop
	second      string // line 2; "" when the role has said nothing
	secondWarn  bool   // line 2 is a denial, which the owner should not miss
}

// line2 is the text of a run's second line: its title when the layout moved
// the titles there, then a colon and the role's latest line. It is "" when the
// run has neither.
func (rr *runRow) line2(l *runLayout) string {
	if l == nil || !l.titleBelow || rr.title == "" {
		return rr.second
	}
	if rr.second == "" {
		return rr.title
	}
	return rr.title + ": " + rr.second
}

// Cells of line 1, left to right.
const (
	cellID = iota
	cellTitle
	cellRole
	cellModel
	cellBar
	cellHeard
	cellTokens
)

type runCell struct {
	text string
	kind int
	pad  int // columns of padding after the cell, when another cell follows
}

// titleMin is the fewest columns a shortened title keeps, "..." included. A
// title that would get less goes (S145).
const titleMin = 12

// runLayout is the cells and column widths of line 1, chosen once for every
// run in the Running section, so the tightest row decides which cells all rows
// keep and the bars, round words and last heard stand in one column (S145).
type runLayout struct {
	title, model, heard, role, bar, tokens   bool
	idW, titleW, roleW, barW, heardW, tokenW int
	// titleBelow: the title does not fit on line 1, so line 1 is status only
	// and every run's line 2 starts with its title.
	titleBelow bool
}

// fitRunLayout chooses the layout for room columns from the widest content of
// each column. Tokens, which only an engine run has, go first. Then the title
// shortens (to titleMin columns) and the model goes. A title that would still
// get less moves to line 2 for every run (titleBelow), and the model comes back
// if it then fits. Last heard drops next unless a row has a cue, then the role,
// then the stage bar, so a cue outlasts the bar as it does elsewhere (S106). The
// id stays.
func fitRunLayout(rows []*runRow, room int) runLayout {
	var full, roleOnly, roleModel runLayout
	cue := false
	for _, rr := range rows {
		full.idW = max(full.idW, lipgloss.Width(rr.id))
		full.titleW = max(full.titleW, lipgloss.Width(rr.title))
		roleOnly.roleW = max(roleOnly.roleW, lipgloss.Width(rr.role))
		withModel := lipgloss.Width(rr.role)
		if rr.model != "" {
			withModel += 1 + lipgloss.Width(rr.model)
		}
		roleModel.roleW = max(roleModel.roleW, withModel)
		full.barW = max(full.barW, lipgloss.Width(rr.bar))
		full.heardW = max(full.heardW, lipgloss.Width(rr.heard))
		full.tokenW = max(full.tokenW, lipgloss.Width(rr.tokens))
		cue = cue || strings.HasPrefix(rr.heard, "!")
	}
	type stage struct{ title, model, heard, role, bar, tokens bool }
	stages := []stage{
		{true, true, true, true, true, true},
		{true, true, true, true, true, false},
		{true, false, true, true, true, false},
		{false, true, true, true, true, false},
		{false, false, true, true, true, false},
		{false, false, cue, true, true, false},
		{false, false, cue, false, true, false},
		{false, false, cue, false, false, false},
	}
	for i, s := range stages {
		l := full
		l.title, l.model, l.heard, l.role, l.bar, l.tokens = s.title && full.titleW > 0, s.model, s.heard && full.heardW > 0, s.role, s.bar && full.barW > 0, s.tokens && full.tokenW > 0
		l.roleW = roleOnly.roleW
		if s.model {
			l.roleW = roleModel.roleW
		}
		if !l.title {
			l.titleW = 0
			l.titleBelow = full.titleW > 0
		}
		total := func(l runLayout) int {
			n := l.idW
			for _, w := range []struct {
				on bool
				w  int
			}{{l.title, l.titleW}, {l.role, l.roleW}, {l.bar, l.barW}, {l.heard, l.heardW}, {l.tokens, l.tokenW}} {
				if w.on {
					n += 2 + w.w
				}
			}
			return n
		}
		if !l.role {
			l.roleW = 0
		}
		if !l.bar {
			l.barW = 0
		}
		if !l.heard {
			l.heardW = 0
		}
		if !l.tokens {
			l.tokenW = 0
		}
		switch {
		case total(l) <= room:
			return l
		case i == 0 || !l.title:
			continue
		}
		// The title shortens to what the other cells leave, if that is enough.
		other := l
		other.title, other.titleW = false, 0
		if avail := room - total(other) - 2; avail >= titleMin {
			l.titleW = avail
			return l
		}
	}
	return runLayout{idW: min(full.idW, room), titleBelow: full.titleW > 0}
}

// cells are line 1 of rr in the layout. Each cell carries the padding that
// brings its column to the shared width; a trailing empty cell is dropped.
func (l runLayout) cells(rr *runRow) []runCell {
	out := []runCell{{rr.id, cellID, l.idW - lipgloss.Width(rr.id)}}
	if out[0].pad < 0 {
		out[0] = runCell{clip(rr.id, l.idW), cellID, 0}
	}
	if l.title {
		t := rr.title
		if lipgloss.Width(t) > l.titleW {
			t = clip(t, l.titleW)
		}
		out = append(out, runCell{t, cellTitle, l.titleW - lipgloss.Width(t)})
	}
	if l.role {
		used := lipgloss.Width(rr.role)
		if l.model && rr.model != "" {
			out = append(out, runCell{rr.role, cellRole, 0})
			used += 1 + lipgloss.Width(rr.model)
			out = append(out, runCell{rr.model, cellModel, l.roleW - used})
		} else {
			out = append(out, runCell{rr.role, cellRole, l.roleW - used})
		}
	}
	if l.bar {
		out = append(out, runCell{rr.bar, cellBar, l.barW - lipgloss.Width(rr.bar)})
	}
	if l.heard {
		out = append(out, runCell{rr.heard, cellHeard, l.heardW - lipgloss.Width(rr.heard)})
	}
	if l.tokens && rr.tokens != "" {
		out = append(out, runCell{rr.tokens, cellTokens, 0})
	}
	for len(out) > 1 && out[len(out)-1].text == "" {
		out = out[:len(out)-1]
	}
	return out
}

// runGap is the space before a cell: one between a role and its model, two
// between the others.
func runGap(c runCell) int {
	if c.kind == cellModel {
		return 1
	}
	return 2
}

// joinRun is the plain text of a row's cells.
func joinRun(cells []runCell) string {
	var b strings.Builder
	for i, c := range cells {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", runGap(c)))
		}
		b.WriteString(c.text)
		if i < len(cells)-1 {
			b.WriteString(strings.Repeat(" ", max(c.pad, 0)))
		}
	}
	return b.String()
}

// runCells are the cells of a row in room columns: in its section's layout, or
// in a layout of its own when it has none.
func runCells(r ovRow, room int) []runCell {
	if r.layout != nil {
		return r.layout.cells(r.run)
	}
	l := fitRunLayout([]*runRow{r.run}, room)
	return l.cells(r.run)
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
	// The part without its vendor/model column, which a compact row shows
	// instead; empty when the part has none.
	shortHeader, shortText string
}

// in is the part's header and text in a row that is compact or not.
func (p relayRowPart) in(compact bool) (header, text string) {
	if compact && p.shortText != "" {
		return p.shortHeader, p.shortText
	}
	return p.header, p.text
}

// compactWidth is the terminal width below which a run row drops its run ID and
// the vendor/model column (S106). It is the width that already narrows the
// sidebar. The pane title and the selected row's detail keep both.
const compactWidth = 100

// compact reports whether the terminal is narrower than compactWidth. Before the
// terminal reports its size the rows stay whole.
func (m *Model) compact() bool {
	return m.width > 0 && m.width < compactWidth
}

// rows lists Needs you and Running. recentRows and then readyRows follow
// them in the Overview cursor order. The lists do not depend on the width.
func (m *Model) rows() (needs, running []ovRow) {
	th := m.cfg.Theme
	compact := m.compact()
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
			if r.FindingNeeds(s.EngineRuns...) > 0 {
				for _, f := range r.Findings {
					if (f.Kind == runrec.FindingLater && f.Filed == "") || (f.Kind == runrec.FindingOwner && f.Answer == "") {
						text := r.Item + "  " + f.Title
						needs = append(needs, ovRow{engineID: r.ID, name: name, project: i, id: r.Item, state: r.State, group: "Findings", kind: "? " + f.Kind, style: th.Decision, kindW: kindWidth, rest: f.Title, text: text})
					}
				}
			}
			if r.RelayRun != "" {
				joined[r.RelayRun] = true
			}
			if r.Section(s.EngineRuns) == source.SectionHidden {
				continue
			}
			displayState := stateWord(r.State)
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
			if r.State == "awaiting-approval" || r.State == "paused" {
				if !r.WaitStartedAt.IsZero() {
					began = r.WaitStartedAt
				} else if !r.UpdatedAt.IsZero() {
					// Older records have no durable wait-start timestamp.
					began = r.UpdatedAt
				}
			}
			if r.State == "stopped" {
				if !r.UpdatedAt.IsZero() {
					parts = append(parts, age(m.cfg.Now().Sub(r.UpdatedAt)))
				}
			} else if !began.IsZero() {
				end := m.cfg.Now()
				duration := elapsed(end.Sub(began))
				if r.State == "awaiting-approval" || r.State == "paused" {
					parts = append([]string{parts[0], duration}, parts[1:]...)
				} else {
					parts = append(parts, duration)
				}
			}
			rest := strings.Join(parts, "  ")
			row := ovRow{unavailable: r.Unavailable, failure: r.Error, engineID: r.ID, state: r.State, pending: r.Pending, kind: "", style: th.Title, project: i, name: name, id: r.ID, rest: r.Item + "  " + rest, text: r.ID + "  " + r.Item + "  " + rest, compact: compact}
			ledger, steps := engineBoard(r)
			for _, linked := range boardLedgers(s) {
				if r.RelayRun != "" && linked.ID == r.RelayRun {
					ledger = linked
					steps = ledgerTreeSteps(linked.Steps)
					break
				}
			}
			// The engine owns the state; a linked ledger supplies only its steps.
			ledger.State = r.State
			// The engine's state tells a fix round from the finish step (S145).
			steps = withEngineState(steps, r.State)
			if !r.Unavailable {
				row.live = &liveRun{item: r.Item, ledger: ledger, steps: steps, activity: r.Activity}
			}
			if r.Section(s.EngineRuns) == source.SectionRunning {
				row.noLabel = r.Error == ""
				if r.Error != "" {
					row.kindW = kindWidth
					row.kind, row.style = kindError, th.Error
				}
				row.stepParts = m.boardRowParts(ledger, steps)
				if r.Error == "" {
					row.run = m.engineRunRow(s, r, ledger, steps)
				}
				if r.UsageKnown || r.Tokens != 0 {
					tokenText := fmt.Sprintf("tokens %d", r.Tokens)
					// Trailing cells drop first: tokens, pipeline, heard, then bar. A heard
					// cue (! or !!) outlasts the bar (rowText).
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
			// The bar and last heard lead; the pipeline follows the running step.
			boardParts := m.boardRowParts(r, ledgerTreeSteps(r.Steps))
			last := len(boardParts) - 1
			steps = append(append(append([]relayRowPart(nil), boardParts[:last]...), steps...), boardParts[last:]...)
			for _, step := range steps {
				text += "  " + step.text
			}
			style := th.External
			if r.State == "stale" {
				style = th.Dim
			}
			// A relay started elsewhere has no label: the pane says so (S145).
			running = append(running, ovRow{ledgerID: r.ID, state: r.State, style: style, noLabel: true, project: i, name: name,
				text: text, id: id, rest: rest, stepParts: steps, dim: r.State == "stale", compact: compact,
				run:  m.ledgerRunRow(s, r),
				live: &liveRun{item: strings.Join(r.Items, ","), ledger: r, steps: ledgerTreeSteps(r.Steps)}})
		}
	}
	// Run rows drop the empty label only while no other Running row has one;
	// then they pad to the widest label so the rows line up (S33).
	if slices.ContainsFunc(running, func(r ovRow) bool { return r.kind != "" }) {
		labelW := 0
		for _, r := range running {
			if r.kind != "" {
				labelW = max(labelW, r.kindW)
			}
		}
		for i := range running {
			if running[i].noLabel {
				running[i].noLabel, running[i].kindW = false, labelW
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
		case "? owner", "? later":
			needs[i].group = "Findings"
		default:
			needs[i].group = "Errors"
		}
	}
	slices.SortStableFunc(needs, func(a, b ovRow) int { return slices.Index(needsGroups, a.group) - slices.Index(needsGroups, b.group) })
	return needs, running
}

var needsGroups = []string{"Decisions", "Held", "Stopped", "Paused", "Approve", "Findings", "Errors"}

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
	paneAt int // the body line the open run pane goes under (S106); -1 when none
	// paneExtra is the lines the pane's row takes beyond one: a Running run's
	// second line (S145).
	paneExtra int
	// selExtra is 1 when the selected row has a second line (S145).
	selExtra int
	// rows is the body line each cursor row starts on, in cursor order. The
	// page keys read it to find the first row below or above the window (S151).
	rows []int
}

// isPaneRow reports whether r is the row of the open run pane.
func (m *Model) isPaneRow(r ovRow) bool {
	if m.pane == nil || r.name != m.pane.project {
		return false
	}
	if m.pane.ledger {
		return r.ledgerID == m.pane.run
	}
	return r.engineID == m.pane.run
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

	// One line-1 layout for every run in Running: the tightest row decides which
	// cells all rows keep, and the cells line up (S145).
	var runs []*runRow
	room := w
	for _, r := range running {
		if r.run != nil {
			runs = append(runs, r.run)
			room = min(room, w-textColumn(r, nameW))
		}
	}
	if len(runs) > 0 {
		layout := fitRunLayout(runs, max(room, 1))
		for i := range running {
			if running[i].run != nil {
				running[i].layout = &layout
			}
		}
	}

	var lines []string
	sel, paneAt, paneExtra, selExtra := -1, -1, 0, 0
	var rowAt []int
	section := func(rows []ovRow, first int, empty string) {
		if len(rows) == 0 {
			lines = append(lines, indent+th.Dim.Render(empty))
			return
		}
		for i, r := range rows {
			selected := first+i == cursor
			rowAt = append(rowAt, len(lines))
			if selected {
				sel = len(lines)
			}
			lines = append(lines, m.rowLine(r, selected, nameW, w))
			hang := strings.Repeat(" ", textColumn(r, nameW))
			second := ""
			if r.run != nil {
				second = r.run.line2(r.layout)
			}
			if second != "" {
				lines = append(lines, m.runSecondLine(r, second, hang, w, selected))
				if selected {
					selExtra = 1
				}
			}
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
			if m.isPaneRow(r) && (paneAt < 0 || selected) {
				paneAt = len(lines) - 1
				paneExtra = 0
				if second != "" {
					paneExtra = 1
				}
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

	ov := overviewView{body: lines, sel: sel, paneAt: paneAt, paneExtra: paneExtra, selExtra: selExtra, rows: rowAt}
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

// textColumn is the column a row's text starts at: after its indent, kind and
// project name.
func textColumn(r ovRow, nameW int) int {
	col := len(indent) + r.kindW + 1
	if r.noLabel {
		col = len(indent)
	}
	if r.group != "" {
		col = len(indent) + 2
	}
	if nameW > 0 {
		col += 1 + nameW + 2
	}
	return col
}

// followed is true for the row of the run the pane moved to, while the switch
// notice shows (S109 note, S134).
func (m *Model) followed(r ovRow) bool {
	p := m.pane
	if p == nil || p.notice == "" || r.name != p.project {
		return false
	}
	if p.ledger {
		return r.ledgerID == p.run
	}
	return r.engineID == p.run
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
		if nameW > 0 && m.followed(r) {
			mark = ">"
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
	if nameW > 0 && m.followed(r) {
		mark = th.Title.Render(">")
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
	if r.run != nil {
		return prefix + m.runLine(r, max(w-lipgloss.Width(prefix), 1))
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

// sliceTitle is the short title of an item, or its title when it has no short
// one, on one line. It is "" for an unknown item.
func sliceTitle(items []slicercli.Item, id string) string {
	for _, it := range items {
		if it.ID == id {
			if it.ShortTitle != "" {
				return treeText(it.ShortTitle)
			}
			return treeText(it.Title)
		}
	}
	return ""
}

// newRunRow is the part of a run's two lines that both kinds of run share.
func (m *Model) newRunRow(id, title string, ledger slicercli.RelayRun, steps []runTreeStep) *runRow {
	live := ledger.State != "stale" && boardLive(ledger.State)
	rr := &runRow{id: id, title: title, bar: stageBar(steps, m.treeFrame, live)}
	rr.heard, rr.heardStyle = m.lastHeard(ledger)
	return rr
}

// engineRunRow is the two-line form of an engine run. The role working now is
// the engine's own, with its model; between roles the engine's state stands in.
// Line 2 is a denial when there is one, otherwise the newest line of the S99
// output tail.
func (m *Model) engineRunRow(s source.Snapshot, r source.EngineRun, ledger slicercli.RelayRun, steps []runTreeStep) *runRow {
	rr := m.newRunRow(r.Item, sliceTitle(s.Items.Value, r.Item), ledger, steps)
	if r.UsageKnown || r.Tokens != 0 {
		rr.tokens = fmt.Sprintf("tokens %d", r.Tokens)
	}
	rr.role, rr.stateOnly = r.State, true
	for _, role := range r.Roles {
		if role.EndedAt.IsZero() {
			rr.role, rr.model, rr.stateOnly = role.Role, role.Model, false
		}
	}
	rr.finishRow(ledger)
	switch {
	case len(r.Denials) > 0:
		rr.second, rr.secondWarn = "denied: "+source.DenialsText(r.Denials), true
	default:
		rr.second = lastTailLine(r.Tail)
	}
	return rr
}

// ledgerRunRow is the two-line form of a relay started elsewhere. The ledger
// has no output stream, so line 2 is the task line of the step that is running
// (relay-run step --task), which the relay contract sets to a short label for
// the step. The last start wins, so a subagent shows ahead of its orchestrator.
func (m *Model) ledgerRunRow(s source.Snapshot, r slicercli.RelayRun) *runRow {
	title := ""
	if len(r.Items) == 1 {
		title = sliceTitle(s.Items.Value, r.Items[0])
	}
	rr := m.newRunRow(strings.Join(r.Items, ","), title, r, ledgerTreeSteps(r.Steps))
	rr.role, rr.stateOnly = r.State, true
	for _, step := range r.Steps {
		if step.Phase == "start" {
			rr.role, rr.model, rr.second, rr.stateOnly = step.Role, step.Model, treeText(step.Task), false
		}
	}
	rr.second = stripTaskLabel(rr.second, r.Items, rr.role)
	rr.finishRow(r)
	return rr
}

// finishRow settles line 1 once the role is known. The gate is named once: when
// the gate is the working role, the round word "gate" leaves the bar (the pane
// bar and the Running entry keep it). A stale run says stale before its role.
func (rr *runRow) finishRow(ledger slicercli.RelayRun) {
	if rr.role == "gate" && !rr.stateOnly {
		rr.bar = strings.TrimSuffix(rr.bar, " gate")
	}
	rr.markStale(ledger)
}

// markStale puts the word stale before the role of a stale run: it stays under
// Running, dimmed and labelled stale (docs/dashboard.md).
func (rr *runRow) markStale(ledger slicercli.RelayRun) {
	if ledger.State == "stale" && !strings.HasPrefix(rr.role, "stale") {
		rr.role = "stale " + rr.role
	}
}

// taskRepeatsLabel reports whether a step task is only the label the relay
// contract asks for, "<id> <role>" (or the id or the role alone, the ids of a
// run on several slices joined), which line 1 already says. Case, spacing and
// -, _ and , between the words do not matter.
func taskRepeatsLabel(task string, items []string, role string) bool {
	norm := func(s string) string {
		s = strings.NewReplacer("-", " ", "_", " ", ",", " ", "/", " ").Replace(strings.ToLower(s))
		return strings.Join(strings.Fields(s), " ")
	}
	task, role = norm(task), norm(role)
	if task == "" {
		return false
	}
	ids := append([]string{norm(strings.Join(items, " "))}, items...)
	if task == role {
		return true
	}
	for _, id := range ids {
		id = norm(id)
		if id != "" && (task == id || task == id+" "+role) {
			return true
		}
	}
	return false
}

// stripTaskLabel cuts the leading "<id> <role>" label off a step task, with the
// ":", "-" and spaces after it, as line 1 says it already; "round 2: x" is left
// of "s41 gate round 2: x". A task that is only the label (or the id or the
// role alone) is "". Anything else is returned as it is.
func stripTaskLabel(task string, items []string, role string) string {
	if taskRepeatsLabel(task, items, role) {
		return ""
	}
	ids := append([]string{strings.Join(items, " ")}, items...)
	sep := `[\s_,/-]+`
	for _, id := range ids {
		words := append(strings.Fields(strings.NewReplacer("-", " ", "_", " ", ",", " ", "/", " ").Replace(id)),
			strings.Fields(strings.NewReplacer("-", " ", "_", " ", ",", " ", "/", " ").Replace(role))...)
		if len(words) == 0 || id == "" {
			continue
		}
		for i, w := range words {
			words[i] = regexp.QuoteMeta(w)
		}
		re := regexp.MustCompile(`(?i)^\s*` + strings.Join(words, sep) + `(?:[\s:_,/-]+|$)`)
		if loc := re.FindStringIndex(task); loc != nil {
			rest := strings.TrimLeft(task[loc[1]:], " :-")
			return rest
		}
	}
	return task
}

// lastTailLine is the newest nonblank line of a role's output tail.
func lastTailLine(tail string) string {
	lines := strings.Split(tail, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := treeText(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// runLine is line 1 of a Running run in room columns, styled: the id in bold, the
// working role in the running colour with its model dim, last heard in its stall
// colour. A stale run is dim except for a heard cue.
func (m *Model) runLine(r ovRow, room int) string {
	th := m.cfg.Theme
	var b strings.Builder
	cells := runCells(r, room)
	for i, c := range cells {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", runGap(c)))
		}
		style := lipgloss.NewStyle()
		switch c.kind {
		case cellID:
			style = th.Title
		case cellRole:
			style = th.Running
			if r.run.stateOnly {
				style = th.Dim
			}
		case cellModel:
			style = th.Dim
		case cellHeard:
			style = r.run.heardStyle
		}
		if r.dim && !(c.kind == cellHeard && strings.HasPrefix(c.text, "!")) {
			style = th.Dim
		}
		if c.text != "" {
			b.WriteString(style.Render(c.text))
		}
		if i < len(cells)-1 {
			b.WriteString(strings.Repeat(" ", max(c.pad, 0)))
		}
	}
	return b.String()
}

// runSecondLine is line 2 of a Running run: the role's latest output line under
// the slice id, set in by one indent, dim (a denial is amber), cut to the pane.
// The selected run's line is reversed whole, as its first line is, and not
// dimmed, so it reads in monochrome too.
func (m *Model) runSecondLine(r ovRow, second, hang string, w int, selected bool) string {
	th := m.cfg.Theme
	prefix := hang + indent
	text := clip(second, max(w-lipgloss.Width(prefix), 1))
	if selected {
		line := prefix + text
		return th.TabActive.Render(line + strings.Repeat(" ", max(w-lipgloss.Width(line), 0)))
	}
	style := th.Dim
	if r.run.secondWarn {
		style = th.Warn
	}
	return prefix + style.Render(text)
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
	if r.run != nil {
		return joinRun(runCells(r, room))
	}
	// Overview rows stay on one line; the pane retains the full Wait text.
	flatten := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")
	r.text, r.rest = flatten.Replace(r.text), flatten.Replace(r.rest)
	if r.id == "" {
		return clip(r.text, room)
	}
	// A compact row starts at its text: the run ID is in the pane title and in
	// the detail line of the selected row.
	head, gap := r.id, "  "
	if r.compact && r.rest != "" {
		head, gap = "", ""
	}
	avail := room - lipgloss.Width(head) - len(gap)
	switch {
	case r.rest == "":
		return r.id
	case lipgloss.Width(r.rest) <= avail:
		text := head + gap + r.rest
		for _, part := range outlastBar(r.stepParts, room-lipgloss.Width(text)) {
			header, partText := part.in(r.compact)
			available := room - lipgloss.Width(text) - 2
			required := lipgloss.Width(header)
			if partText != header {
				// Keep at least three task columns plus the ellipsis, or a whole
				// shorter task. The label, role and vendor/model are never cut.
				required += 2 + min(6, lipgloss.Width(partText)-lipgloss.Width(header)-2)
			}
			if available < required {
				break
			}
			text += "  " + clip(partText, available)
		}
		return text
	case avail < 4:
		if head == "" {
			return clip(r.rest, room)
		}
		return r.id
	}
	return head + gap + clip(r.rest, avail)
}

// outlastBar keeps a heard cue (! or !!) on a row that has room for it but not
// for the bar and the cue together: the bar, the first part, goes instead. A
// heard cell without a cue drops with the trailing cells, before the bar.
func outlastBar(parts []relayRowPart, room int) []relayRowPart {
	if len(parts) < 2 {
		return parts
	}
	heard := parts[1].text
	if !strings.HasPrefix(heard, "! heard ") && !strings.HasPrefix(heard, "!! heard ") {
		return parts
	}
	if lipgloss.Width(parts[0].text)+2+lipgloss.Width(heard)+2 <= room {
		return parts
	}
	return parts[1:]
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
		part := relayRowPart{text: text}
		summary.Task = ""
		part.header = daemon.FormatRelayStep(selected.label, summary)
		// The compact variant drops only the vendor and model.
		summary.Vendor, summary.Model = "", ""
		part.shortHeader = daemon.FormatRelayStep(selected.label, summary)
		summary.Task = step.Task
		part.shortText = daemon.FormatRelayStep(selected.label, summary)
		steps = append(steps, part)
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
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd%02dh", int(d/(24*time.Hour)), int(d/time.Hour)%24)
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
// An added break drops the run of spaces it breaks at, or splits a word longer
// than n.
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
			out, s = append(out, s[:i]), strings.TrimLeft(s[i+1:], " ")
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
			if run.State != "committed" || run.Cleaned || (run.Landed && run.CleanupFailed == "") {
				continue // ork landed it (S119) or the cleanup removed it (S95): nothing is left to do
			}
			rest := run.Branch
			var commands []string
			var note, detail string
			switch {
			case run.Landed:
				// Landed, but the cleanup failed: no landing command. The reason is a
				// clipped note under the row, not a copyable line; the detail holds it
				// in full and, when the run is known to be merged (c refuses
				// otherwise), names the cleanup key (c).
				rest += "  landed  cleanup failed"
				note = strings.TrimPrefix(strings.TrimPrefix(run.CleanupFailed, "landed; "), "cleanup failed at ")
				detail = run.CleanupFailed
				if run.Merged != nil && *run.Merged {
					detail += "\nc finishes the cleanup: " + strings.ReplaceAll(cleanupCommand(m.projects[i].reg.Path, run.Worktree, run.Branch, run.WorktreeGone), "\n", "; ")
				}
			case run.Branch == "":
				rest = "branch unknown"
			case run.Merged != nil && !*run.Merged:
				rest += "  merge first"
			case slicerLanded(p.snap, run.Item):
				// Slicer no longer lists the item: `slicer land` ran outside ork, or
				// the item was retired (the default list hides both). The landing
				// and render lines have nothing left to do; c still removes the
				// branch and worktree (S153).
				rest += "  not listed"
			default:
				commands = append(commands, fmt.Sprintf("slicer land %s --branch %s --note %s", shellQuote(run.Item), shellQuote(run.Branch), shellQuote("Landed "+run.Branch)))
				if run.Merged == nil {
					rest += "  merge state unknown"
				} else {
					rest += "  merged"
					commands = append(commands, "slicer render")
				}
			}
			ready = append(ready, ovRow{mergeKnown: run.Merged != nil, worktreeGone: run.WorktreeGone, ready: true, engineID: run.ID, state: run.State, merged: run.Merged != nil && *run.Merged, branch: run.Branch, worktree: run.Worktree, project: i, name: p.reg.Name, style: m.cfg.Theme.Title,
				id: run.Item, rest: rest, text: run.Item + "  " + rest, commands: commands, note: note, detail: detail})
		}
	}
	return ready
}

// slicerLanded reports that slicer no longer lists the item: its default list
// omits done and retired items, so an item missing from a list that was read
// without error was landed (as `slicer land` leaves it) or retired (S153). A
// list that was never read or that failed says nothing.
func slicerLanded(s source.Snapshot, item string) bool {
	if s.Items.Err != nil || s.Items.ReadAt.IsZero() {
		return false
	}
	return !slices.ContainsFunc(s.Items.Value, func(it slicercli.Item) bool { return it.ID == item })
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
	sel, paneAt := -1, -1
	var rowAt []int
	if live == 0 {
		lines = append(lines, indent+th.Dim.Render("nothing running"))
	}
	for i, r := range running {
		rowAt = append(rowAt, len(lines))
		if i == cursor {
			sel = len(lines)
		}
		if r.live != nil {
			lines = append(lines, m.runningLine(r, i == cursor, nameW, w, layout))
		} else {
			lines = append(lines, m.rowLine(r, i == cursor, nameW, w))
		}
		if m.isPaneRow(r) {
			paneAt = len(lines) - 1
		}
	}
	lines = append(lines, "", th.Heading.Render("Recent"))
	if len(recent) == 0 {
		lines = append(lines, indent+th.Dim.Render("none"))
	}
	for i, r := range recent {
		rowAt = append(rowAt, len(lines))
		if len(running)+i == cursor {
			sel = len(lines)
		}
		lines = append(lines, m.rowLine(r, len(running)+i == cursor, nameW, w))
		if m.isPaneRow(r) {
			paneAt = len(lines) - 1
		}
	}
	ov := overviewView{body: lines, sel: sel, paneAt: paneAt, rows: rowAt}
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
	// Last heard follows the bar, which carries the round from its second.
	if l.heard {
		cols = append(cols, col{t.heard, l.heardW, t.heardStyle})
	}
	switch {
	case l.pipeline:
		cols = append(cols, col{t.pipeline, l.pipelineW, plain})
	case l.roleW > 0:
		cols = append(cols, col{t.role, l.roleW, plain})
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

// stateWord is the state spelling shown to the owner.
func stateWord(state string) string {
	if state == "awaiting-approval" {
		return "awaiting approval"
	}
	return state
}
