// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// The spec check (adj 164, S116, S184) runs before Waves and Roadmap
// launch confirmations. Open questions share one sitting. Waves confirms
// its ready items; a Roadmap launch closes if its item is still held.

// specState is the check and owner sitting shared by Waves and Roadmap launch.
type specState struct {
	checkProject                         string
	checkItems                           []string
	permission                           *daemon.SpecPermissionResult
	permissionPolling, permissionSending bool
	permissionEpoch                      uint64
	checking                             bool
	checkStarted                         time.Time // when the running check began (S264)
	ownerNeeded                          string
	specs                                map[string]daemon.SpecItemResult
	sitting                              *specSitting
	answerProject                        string
	unsaved                              map[string]bool // items whose answers the daemon failed to write in this sitting (S190)
	// lineups are the lineup overrides of the items under check, by item. The
	// check and the answers send them: a local implementer's item is carved into
	// slivers once it is Ready (S359, *proposed*; owner decision adj 401).
	lineups map[string]map[string][]runrec.LineupEntry
}

// lineupsOf maps each item that has lineup overrides to them.
func lineupsOf(items []daemon.RunParams) map[string]map[string][]runrec.LineupEntry {
	var out map[string]map[string][]runrec.LineupEntry
	for _, p := range items {
		if len(p.Lineup) == 0 {
			continue
		}
		if out == nil {
			out = map[string]map[string][]runrec.LineupEntry{}
		}
		out[p.Item] = p.Lineup
	}
	return out
}

type specPermissionMsg struct {
	state    *specState
	epoch    uint64
	result   *daemon.SpecPermissionResult
	answered bool
	err      error
}

// specPermissionLive is optional so older clients and test sources retain
// their existing spec-check behavior. The daemon-backed TUI implements it.
type specPermissionLive interface {
	SpecPermission(context.Context, daemon.SpecPermissionParams) (*daemon.SpecPermissionResult, error)
}

func (m *Model) checkingSpec() (*specState, context.Context) {
	if m.view == roadmapView && m.launch != nil && m.launch.checking && m.launch.ctx != nil && m.launch.ctx.Err() == nil {
		return &m.launch.specState, m.launch.ctx
	}
	if m.view == wavesView && m.waves != nil && m.waves.checking && m.waves.ctx != nil && m.waves.ctx.Err() == nil {
		return &m.waves.specState, m.waves.ctx
	}
	return nil, nil
}

func (m *Model) pollSpecPermission() tea.Cmd {
	w, ctx := m.checkingSpec()
	live, ok := m.cfg.Live.(specPermissionLive)
	if w == nil || !ok || w.permissionPolling || w.permissionSending {
		return nil
	}
	w.permissionPolling = true
	epoch := w.permissionEpoch
	params := daemon.SpecPermissionParams{Project: w.checkProject, Items: append([]string(nil), w.checkItems...)}
	return func() tea.Msg {
		out, err := live.SpecPermission(ctx, params)
		return specPermissionMsg{state: w, epoch: epoch, result: out, err: err}
	}
}

func (m *Model) applySpecPermission(msg specPermissionMsg) tea.Cmd {
	w, _ := m.checkingSpec()
	if w == nil || w != msg.state || w.permissionEpoch != msg.epoch {
		return nil
	}
	w.permissionPolling = false
	if msg.err != nil {
		w.permissionSending = false
		if w.sitting != nil {
			w.sitting.recording = 0
		}
		m.actionStatus = actionError(msg.err)
		return nil
	}
	if msg.answered {
		w.permissionSending = false
		w.permission = nil
		w.sitting = nil
		m.actionStatus = ""
		return m.pollSpecPermission()
	}
	if msg.result == nil || msg.result.Question == nil {
		if w.permission != nil {
			w.permission = nil
			w.sitting = nil
		}
		return nil
	}
	if w.permission == nil || w.permission.ID != msg.result.ID {
		w.permission = msg.result
		w.sitting = &specSitting{asks: []specAsk{{item: strings.Join(w.checkItems, ", "), n: 1, of: 1, q: engine.SpecQuestion{Ask: msg.result.Question}}}}
	}
	return nil
}

func (m *Model) answerSpecPermission(w *specState, text string) tea.Cmd {
	live, ok := m.cfg.Live.(specPermissionLive)
	active, ctx := m.checkingSpec()
	if !ok || active != w || w.permission == nil || w.permissionSending {
		return nil
	}
	params := daemon.SpecPermissionParams{Project: w.checkProject, Items: append([]string(nil), w.checkItems...), ID: w.permission.ID, Text: text}
	w.permissionEpoch++ // discard polls started before this answer
	epoch := w.permissionEpoch
	w.permissionSending = true
	w.permissionPolling = false
	w.sitting.recording = 1
	return func() tea.Msg {
		out, err := live.SpecPermission(ctx, params)
		return specPermissionMsg{state: w, epoch: epoch, result: out, answered: true, err: err}
	}
}

func (w *specState) clearPermission() {
	w.permissionEpoch++
	if w.permission != nil {
		w.sitting = nil
	}
	w.permission = nil
	w.permissionPolling = false
	w.permissionSending = false
}

type specAnswerMsg struct {
	state  *specState
	item   string
	answer *daemon.SpecAnswerResult
	err    error
}

// activeSpec routes input and answer replies to the sitting that owns them.
func (m *Model) activeSpec() (*specState, func() tea.Cmd) {
	if m.launch != nil && m.launch.sitting != nil {
		return &m.launch.specState, m.finishLaunchSpec
	}
	if m.waves != nil && m.waves.sitting != nil {
		return &m.waves.specState, m.askLaunch
	}
	return nil, nil
}

// specAsk is one question of the sitting: the n-th of the item's of questions.
type specAsk struct {
	item  string
	n, of int
	q     engine.SpecQuestion
}

type specSitting struct {
	asks      []specAsk
	pos       int
	free      bool
	input     textField
	answers   map[string][]string
	recording int  // answers being written into specs
	ended     bool // every question answered, or the rest skipped
}

// current is the question on screen. It is nil when none is left.
func (s *specSitting) current() *specAsk {
	if s.pos >= len(s.asks) {
		return nil
	}
	return &s.asks[s.pos]
}

// freeOnly reports whether the question has no options to pick from.
func (a specAsk) freeOnly() bool { return a.q.Ask == nil || len(a.q.Ask.Options) == 0 }

func (w *specState) specReady(id string) bool {
	r, ok := w.specs[id]
	return ok && r.Ready
}

// checkClock is the time a spec check has run: seconds, then minutes and
// seconds, then hours and minutes. The owner sees it move each poll tick.
func checkClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// specWaitText is the line shown while the spec check runs (S264, *proposed*):
// what is checked, how long it has run, and that the confirmation comes next.
func specWaitText(items []string, d time.Duration) string {
	what := fmt.Sprintf("%d items", len(items))
	if len(items) == 1 {
		what = items[0]
	}
	return "spec check: " + what + ", " + checkClock(d) + "; wait for the confirmation"
}

// startSpecCheck asks the daemon about every ticked item. The daemon runs the
// role only for the specs that have no check yet or changed since it, so a
// ready item here is asked again for free, and one whose spec changed is not
// trusted.
func (m *Model) startSpecCheck(items []daemon.RunParams) tea.Cmd {
	w := m.waves
	var need []string
	for _, p := range items {
		need = append(need, p.Item)
	}
	w.checking = true
	w.checkStarted = m.cfg.Now()
	w.checkProject = w.project
	w.checkItems = append([]string(nil), need...)
	w.lineups = lineupsOf(items)
	live, ctx, request, project, lineups := m.cfg.Live, w.ctx, w.request, w.project, w.lineups
	return func() tea.Msg {
		if ctx.Err() != nil {
			return nil
		}
		res, err := live.SpecCheck(ctx, project, need, lineups)
		return wavesMsg{request: request, kind: "check", check: res, err: err}
	}
}

// applyCheck stores the check's answers and opens the sitting for the ticked
// items with questions, or goes on to the confirmation.
func (m *Model) applyCheck(msg wavesMsg) tea.Cmd {
	w := m.waves
	if !w.checking {
		return nil
	}
	w.checking = false
	w.clearPermission()
	m.actionStatus = ""
	if msg.err != nil {
		m.actionStatus = actionError(msg.err)
		return nil
	}
	if msg.check == nil {
		m.actionStatus = "No spec check returned"
		return nil
	}
	w.answerProject = w.project
	w.ownerNeeded = msg.check.OwnerNeeded
	if w.specs == nil {
		w.specs = map[string]daemon.SpecItemResult{}
	}
	var notices []string
	for _, r := range msg.check.Items {
		w.specs[r.Item] = r
		notices = append(notices, r.Notices...)
	}
	m.actionStatus = strings.Join(notices, "; ")
	var asks []specAsk
	for _, p := range w.items() {
		r := w.specs[p.Item]
		if r.Ready || len(r.WaitsOn) > 0 {
			continue
		}
		for i, q := range r.Questions {
			asks = append(asks, specAsk{item: p.Item, n: i + 1, of: len(r.Questions), q: q})
		}
	}
	if len(asks) == 0 {
		return m.askLaunch()
	}
	w.sitting = &specSitting{asks: asks, answers: map[string][]string{}}
	w.sitting.free = asks[0].freeOnly()
	return nil
}

// replanCarved plans again each ticked item whose spec check or answers filed
// slivers: the plan the owner saw when ticking it lists none, and the start
// refuses a plan that changed (S359, *proposed*; owner decision adj 401). The
// plans arrive as wavesMsg "carved", and the last one goes on to the
// confirmation. It returns nil when no item needs one.
func (m *Model) replanCarved() tea.Cmd {
	w := m.waves
	var cmds []tea.Cmd
	live, ctx, request := m.cfg.Live, w.ctx, w.request
	for _, p := range w.items() {
		if r := w.specs[p.Item]; !r.Ready || len(r.Carved) == 0 || w.replanning[p.Item] {
			continue
		}
		if w.replanning == nil {
			w.replanning = map[string]bool{}
		}
		w.replanning[p.Item] = true
		p.Expected = nil
		cmds = append(cmds, func() tea.Msg {
			if ctx.Err() != nil {
				return nil
			}
			plan, err := live.Plan(ctx, p)
			return wavesMsg{request: request, kind: "carved", item: p.Item, plan: plan, err: err}
		})
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// applyCarvedPlan takes the plan that lists an item's carved slivers as the
// plan its start compares. An item whose new plan failed is held, because the
// start would refuse its old plan.
func (m *Model) applyCarvedPlan(msg wavesMsg) tea.Cmd {
	w := m.waves
	if !w.replanning[msg.item] {
		return nil
	}
	delete(w.replanning, msg.item)
	r := w.specs[msg.item]
	r.Carved = nil
	// An item the owner unticked meanwhile is dropped with no word: its plan
	// no longer matters, whether or not it came back.
	if p, ok := w.picked[msg.item]; ok {
		if msg.err == nil && msg.plan != nil {
			p.Expected = msg.plan
			w.picked[msg.item] = p
		} else {
			r.Ready = false
			m.actionStatus = msg.item + ": the plan with its carved slivers failed; " + errorText(msg.err)
		}
	}
	w.specs[msg.item] = r
	if len(w.replanning) > 0 {
		return nil
	}
	return m.askLaunch()
}

// errorText is err's owner-facing text, or a plain note for none.
func errorText(err error) string {
	if err == nil {
		return "no plan returned"
	}
	return actionError(err)
}

// askLaunch confirms the wave for the ticked items whose spec is ready.
func (m *Model) askLaunch() tea.Cmd {
	w := m.waves
	if cmd := m.replanCarved(); cmd != nil {
		return cmd
	}
	var ready []daemon.RunParams
	var held []string
	for _, p := range w.items() {
		if w.specReady(p.Item) {
			ready = append(ready, p)
		} else {
			held = append(held, p.Item)
		}
	}
	if len(ready) == 0 {
		if len(held) > 0 {
			m.actionStatus = "No item is ready: " + strings.Join(w.heldNotes(held), "; ")
		}
		return nil
	}
	m.confirm = &runAction{verb: "wave", project: w.project, items: ready, held: w.heldNotes(held)}
	return nil
}

// heldNotes gives each held item the same explanation as its spec row.
func (w *wavesState) heldNotes(items []string) []string {
	var notes []string
	for _, id := range items {
		notes = append(notes, id+" "+specNote(w.specs[id]))
	}
	return notes
}

func (m *Model) sittingKey(state *specState, done func() tea.Cmd, msg tea.KeyPressMsg) tea.Cmd {
	s := state.sitting
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	if state.permissionSending {
		return nil
	}
	ask := s.current()
	if ask == nil {
		return nil
	}
	if s.free {
		switch msg.String() {
		case "esc":
			if !ask.freeOnly() {
				s.free = false
				return nil
			}
			return m.endSitting(state, done)
		case "enter":
			if text := strings.TrimSpace(s.input.Value()); text != "" {
				return m.answerSitting(state, done, text)
			}
		default:
			s.input.update(msg)
		}
		return nil
	}
	switch k := msg.String(); k {
	case "esc", "s":
		return m.endSitting(state, done)
	case "t":
		if state.permission != nil {
			return nil
		}
		s.free = true
		s.input = textField{}
	default:
		if n, err := strconv.Atoi(k); err == nil && n >= 1 && n <= len(ask.q.Ask.Options) {
			o := ask.q.Ask.Options[n-1]
			return m.answerSitting(state, done, o.Label+": "+o.Summary)
		}
	}
	return nil
}

// answerSitting takes the answer to the current question. The last answer of
// an item records them all in the item's spec.
func (m *Model) answerSitting(w *specState, done func() tea.Cmd, text string) tea.Cmd {
	if w.permission != nil {
		return m.answerSpecPermission(w, text)
	}
	s := w.sitting
	ask := *s.current()
	s.answers[ask.item] = append(s.answers[ask.item], text)
	s.pos++
	s.input = textField{}
	s.free = false
	var record tea.Cmd
	if ask.n == ask.of {
		s.recording++
		answers := s.answers[ask.item]
		live, project := m.cfg.Live, w.answerProject
		params := daemon.SpecAnswerParams{Project: project, Item: ask.item, Answers: answers, By: os.Getenv("ADJ_DECIDED_BY"), Lineup: w.lineups[ask.item]}
		if ps := m.byName[project]; ps != nil {
			params.Adj = ps.reg.Adj
		}
		// The owner's answers are written even if the view changes meanwhile.
		record = func() tea.Msg {
			res, err := live.SpecAnswer(context.Background(), params)
			return specAnswerMsg{state: w, item: params.Item, answer: res, err: err}
		}
	}
	if next := s.current(); next != nil {
		s.free = next.freeOnly()
		return record
	}
	s.ended = true
	return tea.Batch(record, m.finishSitting(w, done))
}

// endSitting skips the questions that are left.
func (m *Model) endSitting(w *specState, done func() tea.Cmd) tea.Cmd {
	if w.permission != nil {
		return m.answerSpecPermission(w, "deny")
	}
	w.sitting.ended = true
	return m.finishSitting(w, done)
}

// finishSitting closes the sitting once it has ended and every answer is
// recorded, and goes on to the confirmation of the ready items.
func (m *Model) finishSitting(w *specState, done func() tea.Cmd) tea.Cmd {
	if s := w.sitting; s == nil || !s.ended || s.recording > 0 {
		return nil
	}
	w.sitting = nil
	return done()
}

// applyAnswer marks the item ready once its answers are in its spec.
func (m *Model) applyAnswer(w *specState, done func() tea.Cmd, msg specAnswerMsg) tea.Cmd {
	if s := w.sitting; s != nil {
		s.recording--
	}
	if msg.err != nil {
		m.actionStatus = msg.item + ": " + actionError(msg.err)
		if w.unsaved == nil {
			w.unsaved = map[string]bool{}
		}
		w.unsaved[msg.item] = true
	} else {
		if w.specs == nil {
			w.specs = map[string]daemon.SpecItemResult{}
		}
		ready := daemon.SpecItemResult{Item: msg.item, Ready: true}
		if msg.answer != nil {
			// The answers made the item Ready, which carved a local run (S359).
			ready.Carved = msg.answer.Carved
		}
		w.specs[msg.item] = ready
		delete(w.unsaved, msg.item)
		m.actionStatus = ""
		if msg.answer != nil && len(msg.answer.Notices) > 0 {
			m.actionStatus = strings.Join(msg.answer.Notices, "; ")
		}
	}
	return m.finishSitting(w, done)
}

// specNote is the row's note on its spec check: ready, or the open questions
// the item waits on.
func specNote(r daemon.SpecItemResult) string {
	if r.Ready {
		return "spec ready"
	}
	if len(r.WaitsOn) > 0 {
		var names []string
		for _, id := range r.WaitsOn {
			names = append(names, fmt.Sprintf("adj %d", id))
		}
		return "waits on " + strings.Join(names, ", ")
	}
	noun := "spec questions"
	if len(r.Questions) == 1 {
		noun = "spec question"
	}
	return fmt.Sprintf("waits on %d %s", len(r.Questions), noun)
}

// heldWithQuestions reports whether a ticked item waits on spec questions.
func (w *wavesState) heldWithQuestions() bool {
	for _, p := range w.items() {
		if r, ok := w.specs[p.Item]; ok && !r.Ready && len(r.WaitsOn) == 0 && len(r.Questions) > 0 {
			return true
		}
	}
	return false
}

// sittingParts draws the sitting in place of the table. lines is the body
// window the screen will show. Zero means the caller wants the whole body.
func (m *Model) sittingParts(w *specState, width, lines int) (overviewView, []string) {
	out := overviewView{sel: -1}
	th := m.cfg.Theme
	s := w.sitting
	add := func(style func(...string) string, text string, extra string) {
		for _, line := range wrapText(text, max(width-len(indent), 1)) {
			if style != nil {
				line = style(line)
			}
			out.body = append(out.body, indent+extra+line)
		}
	}
	dim := func(parts ...string) string { return th.Dim.Render(strings.Join(parts, "")) }
	var items []string
	seen := map[string]bool{}
	for _, a := range s.asks {
		if !seen[a.item] {
			seen[a.item] = true
			items = append(items, a.item)
		}
	}
	noun := "questions"
	if len(s.asks) == 1 {
		noun = "question"
	}
	heading := fmt.Sprintf("Spec check: %d %s for %s", len(s.asks), noun, strings.Join(items, ", "))
	if w.permission != nil {
		heading = "Spec check tool permission"
	}
	out.body = append(out.body, th.Dim.Render(heading))
	ask := s.current()
	if ask == nil {
		add(nil, "Recording answers", "")
		return out, nil
	}
	title := ""
	if m.view == wavesView && m.waves != nil {
		for _, row := range m.waves.rows() {
			if row.ID == ask.item {
				title = row.Title
			}
		}
	} else {
		for _, row := range m.readyLaunchItems() {
			if row.ID == ask.item {
				title = row.Title
			}
		}
	}
	out.body = append(out.body, "")
	add(dim, ask.item+"  "+title, "")
	add(th.Heading.Render, fmt.Sprintf("[%s %d/%d] ", ask.item, ask.n, ask.of)+askTitle(ask.q), "")
	detailAt := len(out.body)
	detailRows := 0
	if ask.q.Ask != nil && ask.q.Ask.Detail != "" {
		// The whole tool input, wrapped as the run pane wraps it (S373, adj 377).
		room := max(width-len(indent), 1)
		rows := permissionInputLines(ask.q.Ask.Detail, room)
		detailRows = len(rows)
		for _, line := range rows {
			out.body = append(out.body, indent+line)
		}
		// A blank line keeps a wrapped command from starting where the options do.
		out.body = append(out.body, "")
	}
	suffixAt := len(out.body)
	if ask.q.Previous != "" {
		add(dim, ask.q.Previous, "  ")
	}
	if a := ask.q.Ask; a != nil {
		for i, o := range a.Options {
			text := fmt.Sprintf("%d. %s: %s", i+1, o.Label, o.Summary)
			if i+1 == a.Recommend {
				text += " (recommended)"
			}
			add(nil, text, "  ")
		}
		add(dim, fmt.Sprintf("Why %d: %s", a.Recommend, a.Reason), "  ")
	} else {
		add(nil, ask.q.Text, "  ")
	}
	if ask.q.Key != "" {
		add(dim, "Decision key: "+ask.q.Key, "  ")
	}
	if s.free {
		room := max(width-len(indent)-len("Answer: "), 1)
		out.body = append(out.body, "", indent+"Answer: "+s.input.view(room, th))
	}
	out.body = append(out.body, "")
	if w.permission != nil {
		text := "The spec check is still running."
		if w.permissionSending {
			text = "Sending permission answer"
		}
		add(dim, text, "")
	} else {
		add(dim, fmt.Sprintf("%d of %d answered", s.pos, len(s.asks)), "")
	}
	if w.ownerNeeded != "" {
		out.body = append(out.body, "")
		add(dim, "Owner needed: "+w.ownerNeeded, "")
	}
	if lines > 0 && detailRows > 0 && len(out.body) > lines {
		// The wrapped command would push the options, the Why line or the
		// footer out of the window. Drop it whole. This screen is before a
		// run exists, so the marker does not name one.
		word := "lines"
		if detailRows == 1 {
			word = "line"
		}
		suffix := append([]string(nil), out.body[suffixAt:]...)
		out.body = out.body[:detailAt]
		add(dim, fmt.Sprintf("+%d detail %s hidden", detailRows, word), "")
		out.body = append(out.body, "")
		out.body = append(out.body, suffix...)
	}
	return out, nil
}

// specCheckBodyLines is the body window a spec-check sitting is drawn into.
// It matches screen: the terminal less the hint, the view bar and rules, and
// the status band while a Waves check is running.
func (m *Model) specCheckBodyLines(mainW int) int {
	_, h, _ := m.layoutSize()
	status := m.actionStatus
	if m.view == wavesView && m.waves != nil && m.waves.checking {
		var ids []string
		for _, p := range m.waves.items() {
			ids = append(ids, p.Item)
		}
		wait := specWaitText(ids, m.cfg.Now().Sub(m.waves.checkStarted))
		if status != "" {
			wait += "\n" + status
		}
		status = wait
	}
	bodyH := max(h-4, 1)
	if status == "" {
		return bodyH
	}
	detail := wrapText(status, max(mainW-len(indent), 1))
	detail = detail[:min(len(detail), max(h-6, 1))]
	return max(h-5-len(detail), 1)
}

func askTitle(q engine.SpecQuestion) string {
	if q.Ask != nil {
		return q.Ask.Title
	}
	return "Question"
}

// permissionInputLines is a permission question's whole tool input, wrapped
// the way the run pane wraps it (S373): every token, a tab as four spaces,
// and a line that wraps continued under a two-space hanging indent.
func permissionInputLines(detail string, width int) []string {
	detail = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", "    ").Replace(detail)
	var rows []string
	for _, line := range strings.Split(detail, "\n") {
		rows = append(rows, hangDetail(line, width)...)
	}
	return rows
}
