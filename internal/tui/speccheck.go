// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
)

// The spec check (adj 164, S116, S184) runs before Waves and Roadmap
// launch confirmations. Open questions share one sitting. Waves confirms
// its ready items; a Roadmap launch closes if its item is still held.

// specState is the check and owner sitting shared by Waves and Roadmap launch.
type specState struct {
	checking      bool
	ownerNeeded   string
	specs         map[string]daemon.SpecItemResult
	sitting       *specSitting
	answerProject string
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
	m.actionStatus = "pending: spec.check"
	live, ctx, request, project := m.cfg.Live, w.ctx, w.request, w.project
	return func() tea.Msg {
		if ctx.Err() != nil {
			return nil
		}
		res, err := live.SpecCheck(ctx, project, need)
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
	for _, r := range msg.check.Items {
		w.specs[r.Item] = r
	}
	var asks []specAsk
	for _, p := range w.items() {
		r := w.specs[p.Item]
		if r.Ready {
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

// askLaunch confirms the wave for the ticked items whose spec is ready.
func (m *Model) askLaunch() tea.Cmd {
	w := m.waves
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
			m.actionStatus = "No item is ready: " + strings.Join(held, ", ") + " wait on spec questions"
		}
		return nil
	}
	m.confirm = &runAction{verb: "wave", project: w.project, items: ready, held: held}
	return nil
}

func (m *Model) sittingKey(state *specState, done func() tea.Cmd, msg tea.KeyPressMsg) tea.Cmd {
	s := state.sitting
	if msg.String() == "ctrl+c" {
		return tea.Quit
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
		params := daemon.SpecAnswerParams{Project: project, Item: ask.item, Answers: answers, By: os.Getenv("ADJ_DECIDED_BY")}
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
	} else {
		if w.specs == nil {
			w.specs = map[string]daemon.SpecItemResult{}
		}
		w.specs[msg.item] = daemon.SpecItemResult{Item: msg.item, Ready: true}
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
	noun := "spec questions"
	if len(r.Questions) == 1 {
		noun = "spec question"
	}
	return fmt.Sprintf("waits on %d %s", len(r.Questions), noun)
}

// heldWithQuestions reports whether a ticked item waits on spec questions.
func (w *wavesState) heldWithQuestions() bool {
	for _, p := range w.items() {
		if r, ok := w.specs[p.Item]; ok && !r.Ready && len(r.Questions) > 0 {
			return true
		}
	}
	return false
}

// sittingParts draws the sitting in place of the table.
func (m *Model) sittingParts(w *specState, width int) (overviewView, []string) {
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
	out.body = append(out.body, th.Dim.Render(fmt.Sprintf("Spec check: %d %s for %s", len(s.asks), noun, strings.Join(items, ", "))))
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
	add(dim, fmt.Sprintf("%d of %d answered", s.pos, len(s.asks)), "")
	if w.ownerNeeded != "" {
		out.body = append(out.body, "")
		add(dim, "Owner needed: "+w.ownerNeeded, "")
	}
	return out, nil
}

func askTitle(q engine.SpecQuestion) string {
	if q.Ask != nil {
		return q.Ask.Title
	}
	return "Question"
}
