// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

// noticeFor is how long a switch notice stays: it fades on the first snapshot
// that arrives this long after the switch (proposed). The ledger refresh sends
// a snapshot every second, so the next one would be too soon to read it.
const noticeFor = 5 * time.Second

// gapPriority is the drop priority of the blank lines between the card's
// blocks: above every other footer line, so they go first (S141).
const gapPriority = 9

// minBodyRoom is the room the pane's scrolling body keeps before the card's
// blank lines may take any (S141).
const minBodyRoom = 8

// runPane keeps its run target while snapshots reorder Overview rows.
type runPane struct {
	finding                   int // one-based non-blocking owner finding being answered
	run, project              string
	answered, answerDone      bool
	answerState, answerNotice string
	answerStatus              string // the status text answer tracking wrote last (S134)
	answerWait, answerKey     string // the run's wait text and decision key when the answer was sent
	freeText                  bool
	fixing                    bool                   // the fix note field of the approval card is open
	number                    string                 // digits of a check number typed on a card of 10 or more checks, not yet sent (S141)
	render                    int                    // the card's render on show, one-based; 0 shows the card (S121, proposed)
	renderRound               int                    // the card round that render was opened on
	fetched                   map[string]renderFetch // run.render answers, by card round and name
	adj                       bool
	ledger                    bool
	input                     textField
	offset                    int
	room                      int       // the lines of the scrolling part the last draw had
	notice                    string    // proposed: why the pane moved to this run
	noticeAt                  time.Time // when it moved
}

func (m *Model) openPane(r ovRow) tea.Cmd {
	m.pane = &runPane{run: r.engineID, project: r.name}
	if r.ledgerID != "" {
		m.pane.run, m.pane.ledger = r.ledgerID, true
	}
	m.actionStatus = ""
	return nil
}

// dropNextCommand removes the daemon's closing "; next: ork ..." from a wait
// reason. The pane names the key in its hint line, so the CLI form adds noise
// (S134).
func dropNextCommand(wait string) string {
	if i := strings.LastIndex(wait, "; next: ork "); i >= 0 && !strings.Contains(wait[i:], "\n") {
		return wait[:i]
	}
	return wait
}

func (m *Model) paneRow() (ovRow, string, bool) {
	p := m.byName[m.pane.project]
	if p != nil {
		if m.pane.ledger {
			for _, r := range boardLedgers(p.snap) {
				if r.ID == m.pane.run {
					return ovRow{ledgerID: r.ID, name: p.reg.Name, id: strings.Join(r.Items, ", "), state: r.State}, "", true
				}
			}
			return ovRow{}, "", false
		}
		for _, r := range p.snap.EngineRuns {
			if r.ID == m.pane.run {
				reason := dropNextCommand(r.Wait)
				if !showsWait(r) {
					reason = ""
				}
				return ovRow{unavailable: r.Unavailable, failure: r.Error, engineID: r.ID, name: p.reg.Name, id: r.Item, state: r.State, pending: r.Pending}, reason, true
			}
		}
	}
	return ovRow{}, "", false
}

// paneEnded describes the pane's run when it is terminal: an engine run that
// is stopped (or superseded) or committed, or a ledger run that has ended.
// An engine run missing from the snapshot, or a stale ledger run, is not.
func (m *Model) paneEnded() (string, bool) {
	p := m.byName[m.pane.project]
	if p == nil {
		return "", false
	}
	if m.pane.ledger {
		for _, r := range boardLedgers(p.snap) {
			if r.ID == m.pane.run && r.State == "ended" {
				// The words of the Recent rows: pass, fail, and stopped for the rest.
				word := "stopped"
				switch r.Outcome {
				case "pass":
					word = "passed"
				case "fail":
					word = "failed"
				}
				return strings.Join(r.Items, ", ") + " " + word, true
			}
		}
		return "", false
	}
	for _, r := range p.snap.EngineRuns {
		if r.ID != m.pane.run || r.Unavailable {
			continue
		}
		switch {
		case r.Superseded(p.snap.EngineRuns):
			return r.Item + " superseded", true
		case r.State == runrec.StateStopped || r.State == runrec.StateCommitted:
			return r.Item + " " + r.State, true
		}
	}
	return "", false
}

// newestRunning is the newest run the project is running: an engine run in
// the Running section, or an active ledger run that no engine run links to
// (a relay started elsewhere; the logic of ps.running). A stale ledger run is
// not running.
func newestRunning(s source.Snapshot) (ovRow, string, bool) {
	var best ovRow
	var item string
	var bestAt time.Time
	found := false
	consider := func(row ovRow, label string, at time.Time) {
		if !found || at.After(bestAt) {
			best, item, bestAt, found = row, label, at, true
		}
	}
	linked := map[string]bool{}
	for _, r := range s.EngineRuns {
		if r.RelayRun != "" {
			linked[r.RelayRun] = true
		}
	}
	for _, r := range s.EngineRuns {
		if r.Section(s.EngineRuns) == source.SectionRunning {
			consider(ovRow{engineID: r.ID, name: s.Project.Name}, r.Item, r.CreatedAt)
		}
	}
	if s.Runs.Err == nil {
		for _, r := range s.Runs.Value {
			if linked[r.ID] || r.State == "stale" {
				continue
			}
			at, _ := time.Parse(time.RFC3339Nano, r.BeganAt)
			consider(ovRow{ledgerID: r.ID, name: s.Project.Name}, strings.Join(r.Items, ", "), at)
		}
	}
	return best, item, found
}

// followNewestRun points the pane at the newest running run of its project
// once the pane's run is over (proposed). It acts only on the Overview. It
// never moves a pane whose answer field has text or that has a confirmation
// open, and it stays on the ended run when nothing else is running.
func (m *Model) followNewestRun(project string) {
	pane := m.pane
	if pane == nil || pane.project != project || m.view != 0 {
		return // only the Overview pane follows
	}
	now := m.cfg.Now()
	if pane.notice != "" && !now.Before(pane.noticeAt.Add(noticeFor)) {
		pane.notice = ""
	}
	if pane.input.Value() != "" || m.confirm != nil {
		return
	}
	ended, ok := m.paneEnded()
	if !ok {
		return
	}
	next, item, ok := newestRunning(m.byName[project].snap)
	if !ok {
		return
	}
	m.openPane(next)
	m.pane.notice, m.pane.noticeAt = ended+"; now showing "+item, now
}

func (m *Model) actionRow() (ovRow, bool) {
	if m.pane != nil {
		r, _, ok := m.paneRow()
		return r, ok
	}
	return m.selectedRow()
}

func (m *Model) paneQuestion() bool {
	r, _, ok := m.paneRow()
	return ok && !r.unavailable && !m.pane.ledger && (m.pane.finding > 0 || (r.state == "paused" && r.pending == "question"))
}

// halfPage is the step of ctrl+d and ctrl+u in a pane that shows room lines
// of its scrolling part: the run pane's room, or what the decision, Roadmap
// item and Waves item panes last drew (S152).
func halfPage(room int) int { return max(room/2, 1) }

func (m *Model) paneKey(msg tea.KeyPressMsg) tea.Cmd {
	m.pane.notice = "" // the first key read the switch notice
	if m.pane.number != "" && msg.String() != "esc" && msg.String() != "enter" && !key.Matches(msg, m.keys.MarkCheck) {
		m.dropNumber() // any key but a digit, enter or esc drops a waiting number (S141)
	}
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		if m.pane.number != "" {
			m.dropNumber()
			return nil
		}
		if m.renderOn() > 0 {
			m.pane.render, m.pane.offset = 0, 0
			return nil
		}
		if m.pane.fixing {
			m.pane.fixing, m.pane.input = false, textField{}
			m.actionStatus = ""
			return nil
		}
		if m.paneAsk() != nil && m.pane.freeText {
			m.pane.freeText = false
			m.actionStatus = ""
			return nil
		}
		if m.pane.finding > 0 {
			m.pane.finding = 0
			m.pane.freeText = false
			m.pane.input = textField{}
			m.actionStatus = ""
			return nil
		}
		m.pane = nil
		m.actionStatus = ""
		return nil
	case "pgup":
		m.pane.offset = max(m.pane.offset-1, 0)
		return nil
	case "pgdown":
		m.pane.offset++
		return nil
	case "ctrl+d":
		// Half a page of the scrolling part (S149). Nothing in a field uses ctrl+d.
		m.pane.offset += halfPage(m.pane.room)
		return nil
	case "ctrl+u":
		// ctrl+u in an answer field or the fix note still clears the field.
		if !m.paneField() {
			m.pane.offset = max(m.pane.offset-halfPage(m.pane.room), 0)
			return nil
		}
	}
	if m.renderOn() > 0 {
		return m.renderKey(msg)
	}
	if m.paneQuestion() {
		if msg.String() == "ctrl+a" && m.pane.finding == 0 {
			if _, reason := m.paneAdjKey(); reason == "" {
				m.pane.adj = !m.pane.adj
			}
			return nil
		}
		if ask := m.paneAsk(); ask != nil && !m.pane.freeText {
			if msg.String() == "t" {
				m.pane.freeText = true
				m.pane.input = textField{}
				m.actionStatus = ""
				return nil
			}
			if n, err := strconv.Atoi(msg.String()); err == nil && n >= 1 && n <= len(ask.Options) {
				option := ask.Options[n-1]
				m.pane.input = textField{}
				m.pane.input.insert(option.Label + ": " + option.Summary)
				return m.requestAction("answer")
			}
			return nil
		}
		if msg.String() == "enter" {
			return m.requestAction("answer")
		}
		m.pane.input.update(msg)
		m.actionStatus = ""
		return nil
	}
	if m.pane.fixing {
		if m.paneCard() != nil {
			return m.fixKey(msg)
		}
		m.pane.fixing = false // the run left the card; the field has nothing to send
	}
	if cmd, ok := m.cardKey(msg); ok {
		return cmd
	}
	if key.Matches(msg, m.keys.Owner) {
		if f := m.paneOwner(); f != nil && m.cfg.Live != nil {
			m.pane.finding = f.N
			m.pane.adj = f.Key != ""
			m.pane.input = textField{}
			m.pane.freeText = false
		}
		return nil
	}
	if key.Matches(msg, m.keys.FileSlice) {
		m.requestFile() // a finished run keeps its later findings
		return nil
	}
	r, _, ok := m.paneRow()
	if !ok || m.pane.ledger || r.state == "stopped" || r.state == "committed" {
		return nil
	}
	switch {
	case key.Matches(msg, m.keys.Approve):
		return m.requestAction("approve")
	case key.Matches(msg, m.keys.Stop):
		return m.requestAction("stop")
	case key.Matches(msg, m.keys.Retry):
		return m.requestAction("retry")
	}
	return nil
}

// paneLines expands the selected run below the Overview. The field, command
// and status stay visible while Page Up/Down scroll the tree and Wait reason.
func (m *Model) paneLines(w, h int, status string) []string {
	return m.paneLinesAs(w, h, status, false)
}

// paneLinesAs is paneLines; inline is the pane under its Overview row (S106),
// whose title is the run ID alone because the row above names the item and the
// stage.
func (m *Model) paneLinesAs(w, h int, status string, inline bool) []string {
	r, reason, ok := m.paneRow()
	if !ok {
		return []string{indent + "Run " + m.pane.run + ": no longer in snapshot"}
	}
	width := max(w-len(indent), 1)
	run := r.engineID
	if m.pane.ledger {
		run = r.ledgerID
	}
	displayState := stateWord(r.state)
	stale := m.paneBoardRun().State == "stale"
	if stale {
		displayState = "stale"
	}
	title := "Run " + run + "  " + r.id + "  " + displayState
	if m.pane.ledger {
		title += "  started elsewhere"
	}
	if inline {
		title = "Run " + run
	}
	header := wrapText(title, width)
	for i := range header {
		if stale {
			header[i] = m.cfg.Theme.Dim.Render(header[i])
		} else {
			header[i] = m.cfg.Theme.Title.Render(header[i])
		}
	}
	if m.pane.notice != "" {
		for _, line := range wrapText(m.pane.notice, width) {
			header = append(header, m.cfg.Theme.Dim.Render(line))
		}
	}
	footer := []string{}
	var priorities, groups []int
	// A group is a set of lines that stay or drop together (0: a line alone).
	nextGroup := 0
	addGroup := func(lines []string, priority int, grouped bool) {
		footer = append(footer, lines...)
		if grouped {
			nextGroup++
		}
		for range lines {
			priorities = append(priorities, priority)
			if grouped {
				groups = append(groups, nextGroup)
			} else {
				groups = append(groups, 0)
			}
		}
	}
	addFooter := func(lines []string, priority int) { addGroup(lines, priority, false) }
	card := m.paneCard()
	if card == nil {
		m.pane.number = "" // a number waits only on a card (S141)
	}
	// A render on show takes the room of the card's summary and checks.
	idx := m.renderOn()
	viewing := idx > 0
	if viewing {
		// A dim rule sets the render apart from the run line above it.
		_, horizontal := m.cfg.Theme.rules(m.cfg.ASCII)
		header = append(header, m.cfg.Theme.Dim.Render(strings.Repeat(horizontal, width)))
	}
	// A blank line sets the card's blocks apart when the terminal is taller than
	// 24 rows. It is the first line to go when the pane is short (S141).
	gap := func() {
		if card != nil && !viewing && !m.pane.fixing && m.height > 24 && len(footer) > 0 {
			addFooter([]string{""}, gapPriority)
		}
	}
	if card != nil && !viewing {
		priority := 0
		if m.pane.fixing {
			priority = 2
		}
		addFooter(m.cardSummary(card, width), priority)
	}
	if !viewing {
		// On a short pane the later findings drop first, then the owner findings.
		laterLines, ownerLines := m.findingLines(width)
		if len(laterLines)+len(ownerLines) > 0 {
			gap()
		}
		addFooter(laterLines, 4)
		for _, finding := range ownerLines {
			addGroup(finding, 2, true) // an owner finding drops whole
		}
	}
	switch {
	case viewing:
		addFooter(wrapText(m.renderTitle(card), width), 0)
	case r.unavailable:
		addFooter(wrapText(source.RemovedProjectHelp(r.name), width), 2)
	case m.pane.ledger:
		if q := ledgerOwnerQuestion(m.paneBoardRun()); q != nil {
			title := wrapText(q.Title, width)
			for i := range title {
				title[i] = m.cfg.Theme.Heading.Render(title[i])
			}
			addFooter(title, 0)
			for i, o := range q.Options {
				text := fmt.Sprintf("%d. %s: %s", i+1, o.Label, o.Summary)
				if o.Label == q.Recommend {
					text += " (recommended)"
				}
				addFooter(wrapText(text, width), 0)
			}
		}
		// The note "started elsewhere, read-only; esc close pane" is on the hint
		// line (S149).
	case m.paneQuestion():
		if ask := m.paneAsk(); ask != nil && !m.pane.freeText {
			title := wrapText(ask.Title, width)
			for i := range title {
				title[i] = m.cfg.Theme.Heading.Render(title[i])
			}
			addFooter(title, 0)
			for i, option := range ask.Options {
				text := fmt.Sprintf("%d. %s: %s", i+1, option.Label, option.Summary)
				if i+1 == ask.Recommend {
					text += " (recommended)"
				}
				addFooter(wrapText(text, width), 0)
			}
			addFooter(wrapText("Why "+strconv.Itoa(ask.Recommend)+": "+ask.Reason, width), 2)
		} else {
			addFooter([]string{"Answer: " + m.pane.input.view(max(width-len("Answer: "), 1), m.cfg.Theme)}, 0)
			// The preview stays out while the call runs: the command is sent.
			if !m.answerInFlight() {
				command := "ork answer " + r.engineID + " " + shellQuote(m.pane.input.Value())
				if m.pane.finding > 0 {
					if f := m.paneOwner(); f != nil {
						command = "Record answer to " + strconv.Quote(f.Title) + ": " + m.pane.input.Value()
					}
				}
				addFooter(wrapText(command, width), 5)
			}
		}
		decisionKey, disabledReason := m.paneAdjKey()
		on := m.pane.adj && disabledReason == ""
		toggle := "[ ] file answer in adj as " + decisionKey
		if on {
			toggle = "[x] file answer in adj as " + decisionKey
		}
		if disabledReason != "" {
			toggle = "adj filing off: " + disabledReason
		}
		if m.pane.finding > 0 {
			toggle = ""
			if f := m.paneOwner(); f != nil && f.Key != "" && m.byName[m.pane.project].reg.Adj {
				toggle = "Answer also recorded in adj as " + f.Key
				on = true
			} else {
				on = false
				disabledReason = ""
			}
		}
		warningAt := len(toggle)
		if on && strings.TrimSpace(os.Getenv("ADJ_DECIDED_BY")) == "" {
			toggle += "  ADJ_DECIDED_BY unset"
			warningAt += 2
		}
		// Wrap plain text first so each physical line has its own style.
		toggleLines := wrapText(toggle, width)
		offset := 0
		for i, line := range toggleLines {
			offset += strings.Index(toggle[offset:], line)
			switch {
			case disabledReason != "":
				toggleLines[i] = m.cfg.Theme.Dim.Render(line)
			case on:
				split := min(max(warningAt-offset, 0), len(line))
				toggleLines[i] = ""
				if split > 0 {
					toggleLines[i] = m.cfg.Theme.Heading.Render(line[:split])
				}
				if split < len(line) {
					toggleLines[i] += m.cfg.Theme.Warn.Render(line[split:])
				}
			}
			offset += len(line)
		}
		priority := 4
		if on {
			priority = 2
		}
		addFooter(toggleLines, priority)
	case r.state == "stopped":
		addFooter(wrapText("Stopped; relaunch, or clear with: ork dismiss "+r.engineID, width), 2)
	case card != nil && m.pane.fixing:
		addFooter([]string{"Fix note: " + m.pane.input.view(max(width-len("Fix note: "), 1), m.cfg.Theme)}, 0)
		addFooter(wrapText("enter sends the note to a fix round; esc back to card", width), 0)
	case card != nil:
		gap()
		addFooter(m.cardChecks(card, width), 0)
		gap()
		// The line that says approval lands drops before an owner finding; the one
		// that says it also pushes ranks above it (S141).
		landPriority := 3
		if m.cardPushes() {
			landPriority = 1
		}
		addFooter(wrapText(m.cardLandNote(), width), landPriority)
	}
	if status != "" {
		priority := 1
		if m.confirm != nil && m.confirm.verb == "answer" {
			priority = -1
		}
		addFooter(wrapText(status, width), priority)
	}
	// Drop the lowest priority lines first: preview, off/disabled toggle,
	// on toggle, status, then input. An option confirmation
	// takes priority over the question. Preserve earlier lines
	// within a priority when the terminal cannot fit the whole group.
	// A card leaves a line for the header that names the run.
	limit := h
	if card != nil {
		limit = max(h-1, 0)
	}
	for len(footer) > limit && len(footer) > 0 {
		drop := 0
		for i, priority := range priorities {
			if priority >= priorities[drop] {
				drop = i
			}
		}
		group := groups[drop]
		for i := len(footer) - 1; i >= 0; i-- {
			if i == drop || (group != 0 && groups[i] == group) {
				footer = append(footer[:i], footer[i+1:]...)
				priorities = append(priorities[:i], priorities[i+1:]...)
				groups = append(groups[:i], groups[i+1:]...)
			}
		}
	}
	// The action area takes priority if the header no longer fits.
	fullHeader := header
	header = header[:min(len(header), max(h-len(footer), 0))]
	var lines []string
	if m.paneAsk() != nil {
		context := []string{}
		for _, line := range strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(reason), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "Question:") {
				break
			}
			context = append(context, line)
		}
		reason = strings.TrimSpace(strings.Join(context, "\n"))
	}
	sepAt := -1 // the row that sets the reason off from the progress line
	if viewing {
		lines = m.renderLines(card, idx, width)
	} else {
		if reason != "" {
			lines = append(lines, wrapText(reason, width)...)
		}
		if card != nil {
			lines = append(lines, m.cardBody(card, width)...)
		}
		if reason != "" && len(lines) > 0 && lines[len(lines)-1] != "" {
			sepAt = len(lines)
			lines = append(lines, "") // the progress line starts after a blank line
		}
		steps := m.paneSteps()
		lines = append(lines, m.boardLines(steps, width)...)
		roleWidth := treeRoleWidth(steps)
		if boardLive(m.paneBoardRun().State) {
			for _, role := range pipelineRoles(steps) {
				roleWidth = max(roleWidth, len(role))
			}
		}
		lines = append(lines, m.runTreeLinesWithRoleWidth(steps, width, m.treeFrame, roleWidth)...)
		lines = append(lines, m.boardTail(steps, width, roleWidth)...)
	}
	if len(lines) == 0 {
		lines = wrapText(reason, width)
	}
	room := max(h-len(header)-len(footer)-1, 0)
	if room < min(len(lines), minBodyRoom) {
		// The blank lines between the card's blocks never take room the body
		// needs: without enough of it they go (S141).
		kept := 0
		for i, p := range priorities {
			if p != gapPriority {
				footer[kept], priorities[kept] = footer[i], p
				kept++
			}
		}
		if kept < len(footer) {
			footer, priorities = footer[:kept], priorities[:kept]
			header = fullHeader[:min(len(fullHeader), max(h-len(footer), 0))]
			room = max(h-len(header)-len(footer)-1, 0)
		}
	}
	if sepAt >= 0 && room < sepAt+2 {
		// The reason, the blank and one progress line must fit; else the blank goes.
		lines = append(lines[:sepAt], lines[sepAt+1:]...)
		sepAt = -1
	}
	last := max(len(lines)-room, 0)
	if len(lines) > room && room >= 3 {
		last++ // the marker above uses one line at the bottom of the content
	}
	m.pane.offset = min(m.pane.offset, last)
	m.pane.room = room
	// Indent content before window adds styled markers at the same column.
	for _, section := range [][]string{header, lines, footer} {
		for i := range section {
			section[i] = indent + section[i]
		}
	}
	if sepAt >= 0 {
		lines[sepAt] = "" // the separator row stays bare
	}
	for viewing && len(lines) < room {
		lines = append(lines, "") // the footer stays at the bottom of the pane
	}
	if room == 0 {
		lines = nil
	} else if len(lines) > room {
		lines = window(lines, m.pane.offset, room, m.cfg.Theme)
	}
	out := append(header, lines...)
	if len(out)+len(footer) < h {
		out = append(out, "")
	}
	out = append(out, footer...)
	for i := range out {
		out[i] = lipgloss.NewStyle().MaxWidth(max(w, 1)).Render(out[i])
	}
	return out
}

func (m *Model) paneAdjKey() (string, string) {
	if m.pane.finding > 0 {
		if f := m.paneOwner(); f != nil && m.byName[m.pane.project].reg.Adj && f.Key != "" {
			return f.Key, ""
		}
		return "", "question has no available adj decision key"
	}
	p := m.byName[m.pane.project]
	if p == nil {
		return "", "no adj in this project"
	}
	for _, r := range p.snap.EngineRuns {
		if r.ID == m.pane.run {
			if !p.reg.Adj {
				return r.Key, "no adj in this project"
			}
			if r.Key == "" {
				return "", "question has no decision key"
			}
			return r.Key, ""
		}
	}
	return "", "question has no decision key"
}

func (m *Model) paneAsk() *runrec.OwnerQuestion {
	if m.pane == nil || !m.paneQuestion() {
		return nil
	}
	if m.pane.finding > 0 {
		if f := m.paneOwner(); f != nil {
			return f.Ask
		}
		return nil
	}
	p := m.byName[m.pane.project]
	for _, r := range p.snap.EngineRuns {
		if r.ID == m.pane.run {
			return r.Question
		}
	}
	return nil
}

// paneRunQuestion is the wait text and the decision key of the pane's engine
// run: together they tell one question from the next.
func (m *Model) paneRunQuestion() (wait, key string) {
	if p := m.byName[m.pane.project]; p != nil {
		for _, r := range p.snap.EngineRuns {
			if r.ID == m.pane.run {
				return r.Wait, r.Key
			}
		}
	}
	return "", ""
}

// refreshAnswerStatus follows the resumed run until its next state change, or
// until it pauses again with other wait text, which is a new question. The
// tracking writes the status line only while the line still shows what it
// wrote last or is empty (typing in the field clears it), so another message
// stays until something replaces it (S134).
func (m *Model) refreshAnswerStatus() {
	p := m.pane
	if p == nil || !p.answered {
		return
	}
	write := func(text string) {
		if m.actionStatus == p.answerStatus || m.actionStatus == "" {
			m.actionStatus, p.answerStatus = text, text
		}
	}
	project := m.byName[p.project]
	for _, r := range project.snap.EngineRuns {
		if r.ID != p.run {
			continue
		}
		if p.answerState != "" && r.State != p.answerState || p.answerState == "" && r.State == "paused" && (r.Wait != p.answerWait || r.Key != p.answerKey) {
			p.answered = false
			write("")
			return
		}
		text := "sending answer"
		if p.answerDone {
			text = "answered"
		}
		if r.State != "paused" {
			p.answerState = r.State
			text = "answered"
			for i := len(r.Roles) - 1; i >= 0; i-- {
				role := r.Roles[i]
				if role.EndedAt.IsZero() && role.Round > 0 {
					text += fmt.Sprintf("; %s resumed (round %d)", role.Role, role.Round)
					break
				}
			}
		}
		if p.answerNotice != "" {
			text += "; " + p.answerNotice
		}
		write(text)
		return
	}
	p.answered = false
	write("")
}

// answerInFlight is true while an answer to the pane's run is being sent.
func (m *Model) answerInFlight() bool {
	for _, a := range m.actionFlights[m.pane.run] {
		if a.verb == "answer" {
			return true
		}
	}
	return false
}
