// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// The approval card (S118, *proposed*): what the owner needs to approve a
// slice, on the pane of a run that awaits approval. The daemon sends it as
// the run's card. The keys: a approves, e opens a note for a fix round, x
// stops, and a digit marks the human check of that number.

// cardActor is the part of the daemon connection that acts on the approval
// card: it sends the owner's fix note and marks human checks. The live
// connection has it; a test double may not.
type cardActor interface {
	Fix(ctx context.Context, run, note string) error
	MarkCheck(ctx context.Context, run string, n int, state string) error
}

var _ cardActor = (*Daemon)(nil)

// Fix sends the owner's note to a fix round of the run that awaits approval.
// The call returns when the run rests again.
func (d *Daemon) Fix(ctx context.Context, run, note string) error {
	c, err := d.Connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Fix(ctx, run, note)
}

// MarkCheck ticks the run's n-th human check, marks it "later", or reopens it
// with an empty state.
func (d *Daemon) MarkCheck(ctx context.Context, run string, n int, state string) error {
	c, err := d.Connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.MarkCheck(ctx, run, n, state)
}

// paneCard is the approval card of the pane's run, or nil when the run does
// not await approval, has no card (a record from before S118), or is gone.
func (m *Model) paneCard() *runrec.ApprovalCard {
	run := m.paneRun()
	if run == nil || run.Unavailable || run.State != runrec.StateAwaitingApproval {
		return nil
	}
	return run.Card
}

func openCount(card *runrec.ApprovalCard) int {
	n := 0
	for _, c := range card.HumanChecks {
		if !c.Resolved() {
			n++
		}
	}
	return n
}

// approvalBlock says why a run cannot be approved yet, or "" when it can: a
// human check that is neither ticked nor marked later. The engine refuses the
// same approval; this saves the round trip. The pane names the open checks by
// number, since a digit marks one; the Overview row has no digits for them, so
// it says how many are open and where to mark them.
func (m *Model) approvalBlock(project, run string, inPane bool) string {
	p := m.byName[project]
	if p == nil {
		return ""
	}
	for _, r := range p.snap.EngineRuns {
		if r.ID != run || r.Card == nil {
			continue
		}
		var open []string
		for i, c := range r.Card.HumanChecks {
			if !c.Resolved() {
				open = append(open, strconv.Itoa(i+1))
			}
		}
		switch {
		case len(open) == 0:
		case inPane:
			return "Approve needs every human check marked; open: " + strings.Join(open, ", ") + " (press the number)"
		default:
			return fmt.Sprintf("Approve needs every human check marked; %d open; enter opens the card", len(open))
		}
	}
	return ""
}

// cardSummary is the card's first lines: the gate verdict with its test
// counts, and the diff stat in one line.
func (m *Model) cardSummary(card *runrec.ApprovalCard, width int) []string {
	th := m.cfg.Theme
	var lines []string
	for _, l := range wrapText(fmt.Sprintf("Gate %s (round %d)  Tests: %d passed, %d failed", card.Verdict, card.Round, card.Passed, card.Failed), width) {
		lines = append(lines, th.Heading.Render(l))
	}
	diff := diffLine(card.DiffStat)
	if n := len(card.Untracked); n == 1 {
		diff += ", 1 new file"
	} else if n > 1 {
		diff += fmt.Sprintf(", %d new files", n)
	}
	lines = append(lines, wrapText("Diff: "+diff, width)...)
	for _, deviation := range card.Deviations {
		lines = append(lines, wrapText(deviation, width)...)
	}
	return append(lines, m.rendersLine(card, width)...)
}

// rendersLine says what the UI pass left to see (S121, *proposed*): how many
// renders, why they could not be read (S147), or that it wrote none. It is
// empty when no UI pass ran.
func (m *Model) rendersLine(card *runrec.ApprovalCard, width int) []string {
	var text string
	switch {
	case len(card.Renders) > 0:
		text = fmt.Sprintf("Renders: %d from the UI pass (round %d)", len(card.Renders), card.UIRound)
		if card.RendersDropped > 0 {
			text += fmt.Sprintf(", %d more not kept", card.RendersDropped)
		}
		if card.RendersError != "" {
			text += ", some could not be read: " + card.RendersError
		}
	case card.RendersError != "":
		text = fmt.Sprintf("Renders: the UI pass (round %d) could not be read: %s", card.UIRound, card.RendersError)
	case card.UIRound > 0:
		text = fmt.Sprintf("Renders: the UI pass (round %d) wrote none", card.UIRound)
	default:
		return nil
	}
	lines := wrapText(text, width)
	for i := range lines {
		lines[i] = m.cfg.Theme.Dim.Render(lines[i])
	}
	return lines
}

// renderOn is the one-based render on show, or 0 for the card. A render is
// on show only while the card still holds it: when the card has gone, lost the
// render, or was built again for a new round (a fix round started from another
// TUI), the mode ends here. It only reads: the draw path calls it, so it never
// writes the pane (S147). The stale field is overwritten by nextRender and
// cleared by esc.
func (m *Model) renderOn() int {
	if m.pane == nil || m.pane.render == 0 {
		return 0
	}
	card := m.paneCard()
	if card == nil || m.pane.render > len(card.Renders) || m.pane.renderRound != card.Round {
		return 0
	}
	return m.pane.render
}

// settleRender ends a render mode whose render the card no longer holds, so a
// card that comes back later does not bring the old render with it. It is the
// write that renderOn does not make, and it runs where the pane changes (a
// snapshot, a key), never in the draw path (S147).
func (m *Model) settleRender() {
	if m.pane != nil && m.pane.render != 0 && m.renderOn() == 0 {
		m.pane.render = 0
	}
}

// renderTitle names the render on show.
func (m *Model) renderTitle(card *runrec.ApprovalCard) string {
	idx := m.renderOn()
	return m.cfg.Theme.Heading.Render(fmt.Sprintf("Render %d of %d: %s", idx, len(card.Renders), card.Renders[idx-1].Name))
}

// renderFetch is the state of one render's text: asked for, read, or failed.
type renderFetch struct {
	done bool
	text string
	err  string
	seq  int // the ask this state belongs to (Model.renderSeq); 0 for a report (S147)
}

// renderMsg is the daemon's answer to a run.render call.
type renderMsg struct {
	run, name string
	round     int // the card round the call was made for
	seq       int // the ask it answers; only the latest ask for a render is kept (S147)
	text      string
	err       error
}

// renderReader is the part of the daemon connection that reads a render's
// text (run.render, S121, *proposed*). The live connection has it; a test
// double may not.
type renderReader interface {
	Render(ctx context.Context, run, name string) (string, error)
}

var _ renderReader = (*Daemon)(nil)

// Render reads the text of one render of the run, as its card lists it.
func (d *Daemon) Render(ctx context.Context, run, name string) (string, error) {
	c, err := d.Connect(ctx)
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.Render(ctx, run, name)
}

func renderKeyFor(round int, name string) string { return fmt.Sprintf("%d/%s", round, name) }

// fetchRender asks the daemon for the text of the render on show. The pane
// shows "Loading" until the answer comes back as a renderMsg.
func (m *Model) fetchRender(card *runrec.ApprovalCard) tea.Cmd {
	idx := m.renderOn()
	if idx == 0 {
		return nil
	}
	name, run, round := card.Renders[idx-1].Name, m.pane.run, card.Round
	k := renderKeyFor(round, name)
	if m.pane.fetched == nil {
		m.pane.fetched = map[string]renderFetch{}
	}
	reader, ok := m.cfg.Live.(renderReader)
	if !ok {
		m.pane.fetched[k] = renderFetch{done: true, err: "this daemon connection cannot read renders"}
		return nil
	}
	m.renderSeq++
	seq := m.renderSeq
	m.pane.fetched[k] = renderFetch{seq: seq}
	return func() tea.Msg {
		text, err := reader.Render(context.Background(), run, name)
		return renderMsg{run: run, name: name, round: round, seq: seq, text: text, err: err}
	}
}

// renderArrived keeps the answer for the ask that made it. Every ask takes a
// number from the model's counter, which never restarts, and the pane keeps the
// number of its latest ask for each render. An answer that is not for that ask
// (an older ask for the same render, or an ask of a pane that was closed) is
// dropped (S147).
func (m *Model) renderArrived(msg renderMsg) {
	if m.pane == nil || m.pane.run != msg.run {
		return
	}
	k := renderKeyFor(msg.round, msg.name)
	if f, asked := m.pane.fetched[k]; !asked || f.seq != msg.seq {
		return
	}
	f := renderFetch{done: true, text: msg.text, seq: msg.seq}
	if msg.err != nil {
		f.err = actionError(msg.err)
	}
	m.pane.fetched[k] = f
}

// renderLines is the body of the viewer: the render's text, one pane line per
// line of it, or what is going on instead of it. A line wider than the pane is
// cut with "..." and the pane scrolls the rest. The daemon sends plain text;
// this pane drops control characters again before it draws.
func (m *Model) renderLines(card *runrec.ApprovalCard, idx, width int) []string {
	name := card.Renders[idx-1].Name
	f := m.pane.fetched[renderKeyFor(card.Round, name)]
	switch {
	case f.err != "":
		return wrapText("Could not read "+name+": "+f.err, width)
	case !f.done:
		return []string{m.cfg.Theme.Dim.Render("Loading " + name + " ...")}
	}
	var lines []string
	for _, l := range strings.Split(runrec.PlainText(f.text), "\n") {
		lines = append(lines, clip(l, width))
	}
	return lines
}

// renderKey is the keys of a render on show: v moves to the next render, and
// after the last one back to the card; s hides or shows the sidebar. esc and
// pgup/pgdown are the pane's. No other key acts, so the owner cannot mark or
// approve what the footer hides.
func (m *Model) renderKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Renders):
		return m.nextRender()
	case key.Matches(msg, m.keys.Sidebar):
		m.sidebarHidden = !m.sidebarHidden // a render is cut at the pane edge; the sidebar takes columns
	}
	return nil
}

// nextRender shows the next render, or the card after the last, and returns
// the call that reads the text of the render it shows. Every move reads the
// file again, so the owner never sees an answer older than the move.
func (m *Model) nextRender() tea.Cmd {
	card := m.paneCard()
	m.pane.offset = 0
	if card == nil || m.renderOn() >= len(card.Renders) {
		m.pane.render = 0
		return nil
	}
	m.pane.render = m.renderOn() + 1
	m.pane.renderRound = card.Round
	return m.fetchRender(card)
}

// diffSummary is the last line of a git diff --stat: "N files changed, ...".
func diffSummary(stat string) string {
	lines := strings.Split(strings.TrimSpace(stat), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); strings.Contains(last, "changed") {
		return last
	}
	return ""
}

var diffCounts = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// diffLine is the summary line of a git diff --stat in a few words: "2 files,
// +48 -4". It is "none" when the stat has no summary.
func diffLine(stat string) string {
	m := diffCounts.FindStringSubmatch(diffSummary(stat))
	if m == nil {
		return "none"
	}
	noun := "files"
	if m[1] == "1" {
		noun = "file"
	}
	return fmt.Sprintf("%s %s, +%s -%s", m[1], noun, cmp.Or(m[2], "0"), cmp.Or(m[3], "0"))
}

// cardBody is the per-file diff stat, for the scrolling part of the pane.
func (m *Model) cardBody(card *runrec.ApprovalCard, width int) []string {
	var lines []string
	files := strings.Split(strings.TrimSpace(card.DiffStat), "\n")
	if diffSummary(card.DiffStat) != "" {
		files = files[:len(files)-1]
	}
	// The new files line up with the diff stat: names are padded to one column
	// and every line has its bar there, so "(new)" stands where the counts do.
	type entry struct{ name, rest string }
	var entries []entry
	// A name is cut to leave room for the bar and some counts, so one long name
	// does not push every line past the pane.
	nameCap := max(width-12, 12)
	nameW := 0
	add := func(e entry) {
		if len([]rune(e.name)) > nameCap {
			e.name = clip(e.name, nameCap)
		}
		nameW = max(nameW, len([]rune(e.name)))
		entries = append(entries, e)
	}
	for _, f := range files {
		// git right-aligns the counts: keep the padding after the bar.
		if f = strings.TrimRight(f, " \t"); strings.TrimSpace(f) == "" {
			continue
		}
		if i := strings.Index(f, " | "); i >= 0 {
			add(entry{strings.TrimSpace(f[:i]), f[i+3:]})
		} else {
			entries = append(entries, entry{name: strings.TrimSpace(f)})
		}
	}
	for _, f := range card.Untracked {
		add(entry{f, "(new)"})
	}
	for _, e := range entries {
		if e.rest == "" {
			lines = append(lines, clip(e.name, width)) // not a stat line: shown as git wrote it
			continue
		}
		lines = append(lines, clip(fmt.Sprintf("%-*s | %s", nameW, e.name, e.rest), width))
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	return lines
}

// cardChecks lists the slice's human checks with the owner's marks. A digit
// marks the check of that number: ticked, then later, then open again. The
// label is a heading in every case; a wrapped check hangs under its text.
func (m *Model) cardChecks(card *runrec.ApprovalCard, width int) []string {
	th := m.cfg.Theme
	label := th.Heading.Render("Human checks")
	switch {
	case card.ChecksError != "":
		lines := wrapText("Human checks: not read ("+card.ChecksError+")", width)
		lines[0] = label + strings.TrimPrefix(lines[0], "Human checks")
		return lines
	case len(card.HumanChecks) == 0:
		return []string{label + ": none"}
	}
	lines := []string{label + fmt.Sprintf(" (%d of %d open)", openCount(card), len(card.HumanChecks))}
	// A check's text starts at column 6; with ten or more checks the numbers are
	// padded to one width, so the brackets and the text still line up (S141).
	numW := len(strconv.Itoa(len(card.HumanChecks)))
	hang := numW + len(" [x] ")
	for i, c := range card.HumanChecks {
		mark, suffix := "[ ]", ""
		switch c.State {
		case runrec.CheckTicked:
			mark = "[x]"
		case runrec.CheckLater:
			mark, suffix = "[~]", " (later)"
		}
		for j, l := range wrapText(c.Text+suffix, max(width-hang, 1)) {
			if j == 0 {
				l = fmt.Sprintf("%*d %s %s", numW, i+1, mark, l)
			} else {
				l = strings.Repeat(" ", hang) + l
			}
			if c.State == runrec.CheckTicked {
				l = th.Dim.Render(l)
			}
			lines = append(lines, l)
		}
	}
	return lines
}

// cardLandNote says what approval does. The approval is the owner's confirm of
// the stop point: commit only, or the whole landing and cleanup (S119, S177).
// For landing, the line says it pushes when the project's setting is on.
func (m *Model) cardLandNote() string {
	if run := m.paneRun(); run != nil && run.StopAfter == runrec.StopAfterCommit {
		return "Approve commits it; its worktree and branch stay for you to land."
	}
	if m.cardPushes() {
		return "Approve lands and pushes it, then removes its worktree and branch."
	}
	return "Approve lands it, then removes its worktree and branch."
}

// cardPushes reports whether approval pushes: the project's registry setting.
func (m *Model) cardPushes() bool {
	p := m.byName[m.pane.project]
	return p != nil && p.reg.Push
}

// nextMark is the state a digit moves a check to: ticked, later, open.
func nextMark(state string) string {
	switch state {
	case "":
		return runrec.CheckTicked
	case runrec.CheckTicked:
		return runrec.CheckLater
	}
	return ""
}

// cardKey handles the keys of the card: F (shift-f) opens the fix note, a digit marks a
// human check. It reports whether the key was the card's.
func (m *Model) cardKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	card := m.paneCard()
	if card == nil {
		m.pane.number = ""
		return nil, false
	}
	if m.pane.number != "" {
		if msg.String() == "enter" {
			n, _ := strconv.Atoi(m.pane.number)
			m.pane.number = ""
			return m.markCheck(card, n), true
		}
		if !key.Matches(msg, m.keys.MarkCheck) {
			m.dropNumber() // any other key drops the number, then acts
		}
	}
	switch {
	case key.Matches(msg, m.keys.Fix):
		m.startFix()
		return nil, true
	case key.Matches(msg, m.keys.Renders):
		if len(card.Renders) > 0 {
			return m.nextRender(), true
		}
		return nil, true
	case key.Matches(msg, m.keys.MarkCheck):
		return m.markDigit(card, msg.String()), true
	}
	return nil, false
}

// markCheck moves the n-th human check on: ticked, later, open again.
func (m *Model) markCheck(card *runrec.ApprovalCard, n int) tea.Cmd {
	if n < 1 || n > len(card.HumanChecks) {
		return nil
	}
	return m.requestCheck(n, nextMark(card.HumanChecks[n-1].State))
}

// dropNumber forgets a number typed but not sent.
func (m *Model) dropNumber() {
	m.pane.number = ""
}

// markDigit takes a digit of a check number. On a card of up to nine checks a
// digit marks at once. With ten or more, the digits accumulate: a number that
// no digit can make longer (10 x 10 > checks) is sent at once, and one that
// could be the start of another (1 of 12) waits for the next digit or enter.
// esc, or any other key, drops it (S141).
func (m *Model) markDigit(card *runrec.ApprovalCard, digit string) tea.Cmd {
	count := len(card.HumanChecks)
	if count < 10 {
		n, _ := strconv.Atoi(digit)
		return m.markCheck(card, n)
	}
	if m.pane.number == "" && digit == "0" {
		return nil // no check 0, and no number starts with 0
	}
	typed := m.pane.number + digit
	n, _ := strconv.Atoi(typed)
	switch {
	case n > count:
		m.pane.number = ""
		m.actionStatus = fmt.Sprintf("the slice has no human check %d", n)
		return nil
	case n*10 > count:
		m.pane.number = ""
		return m.markCheck(card, n)
	}
	m.pane.number = typed // the hint line tells it
	return nil
}

// startFix opens the note field.
func (m *Model) startFix() {
	if m.cfg.Live == nil {
		m.actionStatus = "No daemon connection; action unavailable"
		return
	}
	if _, ok := m.cfg.Live.(cardActor); !ok {
		m.actionStatus = "This daemon connection cannot send fixes"
		return
	}
	m.pane.fixing, m.pane.input = true, textField{}
	m.actionStatus = ""
}

// fixKey is the note field: it owns every key until enter sends the note or
// esc goes back to the card.
func (m *Model) fixKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "enter" {
		return m.requestFix()
	}
	m.pane.input.update(msg)
	m.actionStatus = ""
	return nil
}

// requestFix sends the note. It has no y/n: enter is the confirmation.
func (m *Model) requestFix() tea.Cmd {
	r, ok := m.actionRow()
	if !ok || r.engineID == "" || m.paneCard() == nil {
		return nil
	}
	note := strings.TrimSpace(m.pane.input.Value())
	if note == "" {
		m.actionStatus = "Fix note must not be empty"
		return nil
	}
	for _, a := range m.actionFlights[r.engineID] {
		m.actionStatus = a.command() + " is pending"
		return nil
	}
	return m.callAction(runAction{verb: "fix", run: r.engineID, project: r.name, text: note})
}

// requestCheck marks the n-th human check. A mark is quick; the owner presses
// the next digit once the status says it is done.
func (m *Model) requestCheck(n int, state string) tea.Cmd {
	r, ok := m.actionRow()
	if !ok || r.engineID == "" {
		return nil
	}
	if m.cfg.Live == nil {
		m.actionStatus = "No daemon connection; action unavailable"
		return nil
	}
	if _, ok := m.cfg.Live.(cardActor); !ok {
		m.actionStatus = "This daemon connection cannot mark checks"
		return nil
	}
	// The daemon applies one mark at a time: the run is busy while it does, and
	// a second mark would fail with "run is busy". So any pending call waits.
	for _, a := range m.actionFlights[r.engineID] {
		m.actionStatus = a.command() + " is pending"
		return nil
	}
	return m.callAction(runAction{verb: "check", run: r.engineID, project: r.name, check: n, state: state})
}

// The card's calls are told in words: they have no ork command to quote.

// markWord is the state a check is moving to, as the status line says it.
func markWord(state string) string {
	switch state {
	case runrec.CheckTicked:
		return "done"
	case runrec.CheckLater:
		return "for later"
	}
	return "open"
}

// pendingText is the status line while the call runs.
func (a runAction) pendingText() string {
	if a.verb == "relay-answer" {
		return "answering #" + a.question + " on " + a.run + " in the ledger: pending"
	}
	if a.verb == "decide" {
		return fmt.Sprintf("deciding #%d in adj", a.adjID)
	}
	switch a.verb {
	case "fix":
		return "fix note sent; fix round running"
	case "check":
		if a.state == "" {
			return fmt.Sprintf("reopening check %d", a.check)
		}
		return fmt.Sprintf("marking check %d %s", a.check, markWord(a.state))
	}
	if a.verb == "approve" && a.item != "" {
		if a.stopAfter == runrec.StopAfterCommit {
			return "committing " + a.item + ": pending"
		}
		return "landing " + a.item + ": pending"
	}
	if a.verb == "end" {
		return "ending ledger run " + a.run + ": pending"
	}
	if a.verb == "cleanup" {
		return "cleaning up " + a.cleanupItem() + ": pending"
	}
	return a.command() + ": pending"
}

// approver is the part of the daemon connection that approves and returns the
// run record the reply carries. A test double may not have it.
type approver interface {
	ApproveRecord(ctx context.Context, run string) (runrec.Record, error)
}

var _ approver = (*Daemon)(nil)

// approveOutcome reads what an approval did from the run record the reply
// carries: the slice committed, landed, or a blocked landing went to its fix round and
// the run awaits approval again. Any other state (a pause, a stop) is "".
func approveOutcome(rec runrec.Record) string {
	switch {
	case rec.State == runrec.StateCommitted && !rec.LandedAt.IsZero():
		return "landed"
	case rec.State == runrec.StateCommitted:
		return "committed"
	case rec.State == runrec.StateAwaitingApproval && rec.LandFixes > 0:
		return "fix round"
	}
	return ""
}

// doneText is the status line when the call has returned.
func (a runAction) doneText() string {
	if a.verb == "decide" {
		return fmt.Sprintf("decided #%d in adj", a.adjID)
	}
	if a.verb == "approve" && a.item != "" {
		switch a.outcome {
		case "committed":
			return "committed " + a.item + "; ready to land"
		case "landed":
			return "landed " + a.item
		case "fix round":
			return "landing " + a.item + " blocked; fix round done, awaiting approval"
		}
	}
	switch a.verb {
	case "fix":
		return "fix round finished"
	case "check":
		if a.state == "" {
			return fmt.Sprintf("check %d reopened", a.check)
		}
		return fmt.Sprintf("check %d marked %s", a.check, markWord(a.state))
	}
	if a.verb == "cleanup" {
		return "cleaned up " + a.cleanupItem()
	}
	if a.verb == "end" {
		return "ended ledger run " + a.run
	}
	return a.command() + ": done"
}
