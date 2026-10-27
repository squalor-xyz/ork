// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
)

// keyMap is every key the TUI answers. It also feeds the help view.
type keyMap struct {
	AllProjects key.Binding

	Add        key.Binding
	Cleanup    key.Binding
	EditLineup key.Binding
	Tick       key.Binding
	Launch     key.Binding
	Approve    key.Binding
	Stop       key.Binding
	Retry      key.Binding
	FileSlice  key.Binding
	Owner      key.Binding
	Fix        key.Binding
	MarkCheck  key.Binding
	Renders    key.Binding
	Next       key.Binding
	Prev       key.Binding
	TabNext    key.Binding
	TabPrev    key.Binding
	NextView   key.Binding
	PrevView   key.Binding
	Jump       key.Binding
	RowNext    key.Binding
	RowPrev    key.Binding
	First      key.Binding
	Last       key.Binding
	PageDown   key.Binding
	PageUp     key.Binding
	ScrollDown key.Binding
	ScrollUp   key.Binding
	NextGroup  key.Binding
	PrevGroup  key.Binding
	Select     key.Binding
	Answer     key.Binding
	PickOption key.Binding
	FreeAnswer key.Binding
	FileAdj    key.Binding
	Refresh    key.Binding
	Help       key.Binding
	Close      key.Binding
	Quit       key.Binding
	Yes        key.Binding
	No         key.Binding
	Filter     key.Binding
	Sidebar    key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		AllProjects: key.NewBinding(key.WithKeys("A", "shift+a"), key.WithHelp("A (Decisions)", "toggle all projects")),

		Add:        key.NewBinding(key.WithKeys("+"), key.WithHelp("+", "add project (Welcome)")),
		Cleanup:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "remove merged worktree and branch (Ready)")),
		EditLineup: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit ticked item lineup (Waves)")),
		Tick:       key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "tick item and ask inputs (Waves)")),
		Launch:     key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "launch a ready slice (Roadmap) or wave (Waves)")),
		Approve:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve")),
		Stop:       key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop")),
		Retry:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "retry")),
		Owner:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "answer owner finding")),
		FileSlice:  key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "file slice")),
		Fix:        key.NewBinding(key.WithKeys("F", "shift+f"), key.WithHelp("F", "fix round")),
		MarkCheck:  key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"), key.WithHelp("1-9", "mark check")),
		Renders:    key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "view renders")),
		Filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter projects; esc clears")),
		Sidebar:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "hide/show sidebar")),
		Next:       key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next project")),
		Prev:       key.NewBinding(key.WithKeys("["), key.WithHelp("[", "previous project")),
		TabNext:    key.NewBinding(key.WithKeys("right"), key.WithHelp("right", "next tab (Decisions)")),
		TabPrev:    key.NewBinding(key.WithKeys("left"), key.WithHelp("left", "previous tab (Decisions)")),
		NextView:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next view")),
		PrevView:   key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous view")),
		Jump:       key.NewBinding(key.WithKeys("0", "1", "2", "3", "4", "5", "6", "7", "8", "9"), key.WithHelp("1-9", "jump to project; 0 Needs you")),
		RowNext:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j/k", "down/up row; g/G home/end first/last")),
		RowPrev:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("up/k", "previous row")),
		First:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g/home", "first row")),
		Last:       key.NewBinding(key.WithKeys("G", "shift+g", "end"), key.WithHelp("G/end", "last row")),
		PageDown:   key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdown/pgup", "page rows or scroll a pane")),
		PageUp:     key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		ScrollDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d/u", "scroll a run pane half a page")),
		ScrollUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "scroll a pane back half a page")),
		NextGroup:  key.NewBinding(key.WithKeys("}"), key.WithHelp("}/{", "next/previous group (Overview)")),
		PrevGroup:  key.NewBinding(key.WithKeys("{"), key.WithHelp("{", "previous group")),
		Select:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open run, question, item or decision; other rows select project")),
		FileAdj:    key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a (answer)", "toggle filing the answer in adj")),
		PickOption: key.NewBinding(key.WithKeys("1", "2", "3", "4"), key.WithHelp("1-4 (question)", "pick an option; y sends")),
		FreeAnswer: key.NewBinding(key.WithKeys("t"), key.WithHelp("t (question)", "type a free answer")),
		Answer:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter (answer)", "submit free text")),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh now")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "show or hide this help")),
		Close:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close run, question, item, decision, help or message")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Yes:        key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y", "confirm")),
		No:         key.NewBinding(key.WithKeys("n", "N", "enter", "esc"), key.WithHelp("n", "cancel")),
	}
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding { return []key.Binding{k.Refresh, k.Help, k.Quit} }

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.NextView, k.PrevView, k.AllProjects}, {k.Next, k.Prev, k.Jump}, {k.RowNext, k.PageDown, k.ScrollDown, k.NextGroup, k.Select}, {k.Filter, k.Sidebar, k.Add}, {k.Tick, k.EditLineup, k.Launch}, {k.Approve, k.Fix, k.Stop, k.Retry, k.Renders, k.FileSlice, k.Owner, k.Answer, k.PickOption, k.FreeAnswer, k.FileAdj, k.Cleanup}, {k.Refresh, k.Help, k.Close, k.Quit}}
}

// short is b with a short label and words for the hint line. It answers to the
// same keys as b, so the hint names no key the map does not bind.
func short(b key.Binding, label, desc string) key.Binding {
	h := key.NewBinding(key.WithKeys(b.Keys()...), key.WithHelp(label, desc))
	h.SetEnabled(b.Enabled())
	return h
}

// hintEntry is one hint of the hint line and how soon it drops when the line
// is too narrow: the highest drop goes first, and drop 0 never goes (the way
// out, esc in a pane and ? help elsewhere, which stand at the right end).
type hintEntry struct {
	b    key.Binding
	drop int
}

// The drop order of the hint line, first to go first: the row's rarer keys
// (renders, fix, the check digits, retry, stop), then the pane scroll keys'
// second form and the global tab and quit. Finding actions stay with row keys.
const (
	dropRender = 19
	dropFix    = 18
	dropMark   = 17
	dropRetry  = 16
	dropStop   = 15
	dropHalf   = 14
	dropTab    = 13
	dropQuit   = 12
	dropLine   = 11
	dropPage   = 10
	dropAppr   = 9
	dropClean  = 8
	dropRow    = 5
	dropPinned = 0
)

// hintList collects the entries a context offers; an entry whose binding is
// disabled (its check refused the key) is left out.
type hintList []hintEntry

func (l *hintList) add(drop int, b ...key.Binding) {
	for _, h := range b {
		if h.Enabled() {
			*l = append(*l, hintEntry{h, drop})
		}
	}
}

// offer is short with the key left out when ok is false.
func offer(ok bool, b key.Binding, label, desc string) key.Binding {
	h := short(b, label, desc)
	h.SetEnabled(ok && b.Enabled())
	return h
}

// confirmWord is what y does for a confirmation, in the confirmation's own
// verb (U4); the prompt on screen carries the rest of the sentence.
func confirmWord(verb string) string {
	switch verb {
	case "launch":
		return "run"
	case "wave":
		return "start"
	case "approve", "stop", "retry", "file":
		return verb
	case "cleanup":
		return "clean up"
	case "answer":
		return "send"
	}
	return "confirm"
}

// contextHelp is the hint line's ShortHelp (S149): the keys that work now,
// from the bindings, for the selected row, the open pane and the view.
func (m *Model) contextHelp() []key.Binding {
	var out []key.Binding
	for _, e := range m.contextHints() {
		out = append(out, e.b)
	}
	return out
}

// contextHints is contextHelp with each hint's drop order. A key is listed
// only when the check that refuses it (actionRefusal, fileRefusal,
// launchRefusal, waveLaunchRefusal, tickRefusal) lets it through, so the hint
// never offers a key the row refuses.
func (m *Model) contextHints() []hintEntry {
	k := m.keys
	var l hintList
	// Elsewhere than in a pane the way out is ? help, at the right end.
	global := func() {
		l.add(dropTab, short(k.NextView, "tab", "views"))
		l.add(dropQuit, short(k.Quit, "q", "quit"))
		l.add(dropPinned, short(k.Help, "?", "help"))
	}
	switch {
	case m.registeredProjectCount() == 0 && m.pane == nil:
		switch {
		case m.welcome.pending:
			l.add(dropPinned, short(k.Quit, "ctrl+c", "quit"))
		case m.welcome.confirm != "":
			l.add(dropRow, offer(m.cfg.Live != nil, k.Yes, "y", "add project"))
			l.add(dropPinned, short(k.No, "n", "cancel"))
		case m.welcome.editing:
			l.add(dropRow, short(k.Select, "enter", "review path"))
			l.add(dropPinned, short(k.Close, "esc", "cancel"))
		case m.showHelp:
			l.add(dropPinned, short(k.Help, "? or esc", "close help"), short(k.Quit, "q", "quit"))
		default:
			l.add(dropRow, short(k.Add, "+", "add project"))
			l.add(dropPinned, short(k.Quit, "q", "quit"))
		}
	case m.confirm != nil:
		l.add(dropRow, short(k.Yes, "y", confirmWord(m.confirm.verb)), short(k.No, "n", "cancel"))
	case m.waves != nil && m.waves.sitting != nil:
		sitting := m.waves.sitting
		if ask := sitting.current(); ask != nil {
			if sitting.free {
				l.add(dropRow, offer(strings.TrimSpace(sitting.input.Value()) != "", k.Answer, "enter", "submit"))
				desc := "skip the rest"
				if !ask.freeOnly() {
					desc = "back to options"
				}
				l.add(dropPinned, short(k.Close, "esc", desc))
			} else {
				l.add(dropRow, short(k.PickOption, fmt.Sprintf("1-%d", len(ask.q.Ask.Options)), "pick"), short(k.FreeAnswer, "t", "free text"))
				l.add(dropPinned, key.NewBinding(key.WithKeys("s", "esc"), key.WithHelp("s/esc", "skip the rest")))
			}
		}
	case m.waves != nil && m.waves.lineup != nil:
		e := m.waves.lineup
		if e.custom {
			l.add(dropRow, short(k.NextView, "tab/shift+tab", "field"), short(k.Select, "enter", "use entry"))
		} else {
			l.add(dropRow, short(k.NextView, "up/down/tab", "row"))
			if e.row < len(lineupRoles) {
				l.add(dropRow, offer(len(e.plan.Lineup[lineupRoles[e.row]]) > 1, k.TabNext, "left/right", "cycle"), key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "custom")))
			}
			if e.row < len(lineupRoles) && lineupRoles[e.row] == "architect" && len(e.chains[e.row]) > 0 {
				l.add(dropRow+1, key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "drop architect")))
			}
			l.add(dropRow-1, short(k.Select, "enter", "save and replan"))
		}
		l.add(dropPinned, short(k.Close, "esc", "discard"))
	case m.waves != nil && m.waves.inputs != nil:
		l.add(dropRow, short(k.NextView, "tab", "next field"), short(k.Select, "enter", "plan"))
		l.add(dropPinned, short(k.Close, "esc", "cancel"))
	case m.launch != nil:
		switch m.launch.stage {
		case launchList:
			l.add(dropRow, offer(len(m.readyLaunchItems()) > 0, k.Select, "enter", "select slice"), short(k.RowNext, "up/down", "row"))
		case launchWell:
			l.add(dropRow, short(k.Select, "enter", "next field"))
		case launchSplit:
			l.add(dropRow, short(k.Select, "enter", "plan"))
		}
		desc := "cancel"
		if m.launch.stage == launchStarting {
			desc = "close"
		}
		l.add(dropPinned, short(k.Close, "esc", desc))
	case m.view == wavesView && m.waves != nil && m.waves.pane != nil:
		l.add(dropPage, short(k.PageDown, "pgup/pgdown", "page"))
		l.add(dropLine, short(k.RowNext, "j/k", "line"))
		l.add(dropPinned, short(k.Close, "esc", "close pane"))
	case m.pane != nil:
		return m.paneHints()
	case m.filtering:
		l.add(dropRow, short(k.Select, "enter", "keep filter"))
		l.add(dropPinned, short(k.Close, "esc", "clear filter"))
	case m.showHelp:
		l.add(dropPinned, short(k.Help, "? or esc", "close help"))
		l.add(dropQuit, short(k.Quit, "q", "quit"))
	case m.view == questionsView || m.view == decisionsView:
		if m.decisionPane != nil {
			l.add(dropPage, short(k.PageDown, "pgup/pgdown", "page"))
			l.add(dropLine, short(k.RowNext, "j/k", "line"))
			l.add(dropTab, short(k.NextView, "tab", "views"))
			l.add(dropQuit, short(k.Quit, "q", "quit"))
			l.add(dropPinned, short(k.Close, "esc", "close"), short(k.Help, "?", "help"))
			break
		}
		if m.view == questionsView {
			l.add(dropRow, offer(len(m.selectableQuestions()) > 0, k.Select, "enter", "open"))
		} else {
			l.add(dropRow, offer(len(m.decisionRecords()) > 0, k.Select, "enter", "open"),
				short(k.TabNext, "left/right", "switch tab"))
			if m.active >= 0 {
				scope := "all projects"
				if m.allProjects {
					scope = m.projects[m.active].reg.Name + " only"
				}
				l.add(dropRow, short(k.AllProjects, "A", scope))
			}
		}
		global()
	case m.view == roadmapView:
		if m.itemPane != nil {
			l.add(dropTab, short(k.NextView, "tab", "views"))
			l.add(dropQuit, short(k.Quit, "q", "quit"))
			l.add(dropPinned, short(k.Close, "esc", "close"), short(k.Help, "?", "help"))
			break
		}
		_, launchRefused := m.launchRefusal()
		l.add(dropRow, offer(len(m.roadmapItems()) > 0, k.Select, "enter", "open"), offer(!launchRefused, k.Launch, "l", "launch"))
		global()
	case m.view == wavesView:
		if w := m.waves; w != nil {
			rows := w.rows()
			open := !w.pending && w.err == nil && !w.starting && len(rows) > 0
			enter := "item"
			if len(rows) > 0 && w.launched[rows[clampCursor(w.cursor, len(rows))].ID] != "" {
				enter = "open run"
			}
			_, tickRefused := m.tickRefusal()
			_, launchRefused := m.waveLaunchRefusal()
			launch := "launch wave"
			if w.heldWithQuestions() {
				launch = "answer spec questions"
			}
			l.add(dropRow, offer(open, k.Select, "enter", enter), offer(!tickRefused, k.Tick, "space", "tick item"), offer(!launchRefused, k.Launch, "l", launch))
			_, editRefused := m.lineupRefusal()
			l.add(dropRow, offer(!editRefused, k.EditLineup, "e", "edit lineup"))
		}
		global()
	default:
		if r, ok := m.selectedRow(); ok {
			switch {
			case r.ready:
				l.add(dropRow, short(k.Select, "enter", "open"))
			case r.engineID != "" || r.ledgerID != "":
				l.add(dropRow, short(k.Select, "enter", "open"))
			default:
				l.add(dropRow, short(k.Select, "enter", "go to project"))
			}
			l.rowActions(m, r, ok, true)
		}
		global()
	}
	return l
}

// rowActions adds the run keys the row takes (cleanup only on the Overview, not
// in a pane), each only when the check that refuses the key lets it through.
// With no daemon connection every one refuses.
func (l *hintList) rowActions(m *Model, r ovRow, ok, cleanup bool) {
	k := m.keys
	if m.cfg.Live == nil {
		return
	}
	allowed := func(verb string) bool {
		_, refused := m.actionRefusal(verb, r, ok)
		return !refused
	}
	l.add(dropAppr, offer(allowed("approve"), k.Approve, "a", "approve"))
	l.add(dropStop, offer(allowed("stop"), k.Stop, "x", "stop"))
	l.add(dropRetry, offer(allowed("retry"), k.Retry, "t", "retry"))
	if cleanup {
		l.add(dropClean, offer(allowed("cleanup"), k.Cleanup, "c", "clean up"))
	}
}

// paneField reports whether the open pane holds a text field that owns the
// keys: the answer field, or the fix note. ctrl+u edits that field.
func (m *Model) paneField() bool {
	if m.pane == nil || m.renderOn() > 0 {
		return false
	}
	if m.paneQuestion() {
		return m.paneAsk() == nil || m.pane.freeText
	}
	return m.pane.fixing && m.paneCard() != nil
}

// paneHints is the hint of an open run pane: what the pane takes, then how to
// scroll it, then the way out at the right end.
func (m *Model) paneHints() []hintEntry {
	k := m.keys
	var l hintList
	card := m.paneCard()
	row, _, rowOK := m.paneRow()
	esc := func(desc string) { l.add(dropPinned, short(k.Close, "esc", desc)) }
	scroll := func() {
		// In a run pane pgup and pgdown move one line; ctrl+d/u half the pane.
		l.add(dropPage, short(k.PageDown, "pgup/pgdown", "line"))
		if m.paneField() {
			l.add(dropHalf, short(k.ScrollDown, "ctrl+d", "half page")) // ctrl+u edits the field
		} else {
			l.add(dropHalf, short(k.ScrollDown, "ctrl+d/u", "half page"))
		}
	}
	// The answer goes out only when the run takes it and a daemon is there.
	answers := func() bool {
		if m.cfg.Live == nil {
			return false
		}
		_, refused := m.actionRefusal("answer", row, rowOK)
		return !refused
	}
	switch {
	case m.renderOn() > 0:
		desc := "next render"
		if card != nil && m.renderOn() == len(card.Renders) {
			desc = "back to card"
		}
		l.add(dropRow, short(k.Renders, "v", desc), short(k.Sidebar, "s", "sidebar"))
		scroll()
		esc("back to card")
	case m.paneQuestion():
		_, noAdj := m.paneAdjKey()
		ask := m.paneAsk()
		switch {
		case ask != nil && !m.pane.freeText:
			l.add(dropRow, offer(answers(), k.PickOption, fmt.Sprintf("1-%d", len(ask.Options)), "pick"), short(k.FreeAnswer, "t", "free text"))
			l.add(dropRow+1, offer(noAdj == "" && m.pane.finding == 0, k.FileAdj, "ctrl+a", "file in adj"))
			scroll()
			if m.pane.finding > 0 {
				esc("back to run")
			} else {
				esc("close pane")
			}
		default:
			l.add(dropRow, offer(answers(), k.Answer, "enter", "submit"))
			l.add(dropRow+1, offer(noAdj == "" && m.pane.finding == 0, k.FileAdj, "ctrl+a", "file in adj"))
			scroll()
			if ask != nil && m.pane.freeText {
				esc("back to options")
			} else if m.pane.finding > 0 {
				esc("back to run")
			} else {
				esc("close pane")
			}
		}
	case m.pane.fixing && card != nil:
		l.add(dropRow, short(k.Select, "enter", "send note"))
		scroll()
		esc("back to card")
	case !rowOK:
		scroll()
		esc("close pane")
	case m.pane.ledger:
		// A relay started elsewhere has no actions (S149 moved this note here).
		scroll()
		l.add(dropPinned, short(k.Close, "", "started elsewhere, read-only"))
		esc("close pane")
	case row.unavailable:
		scroll()
		esc("close pane")
	default:
		if row.state != "stopped" && row.state != "committed" {
			l.rowActions(m, row, rowOK, false)
		}
		if card != nil {
			markDrop, fixDrop := dropMark, dropFix
			if openCount(card) > 0 {
				markDrop, fixDrop = dropRow, dropRow+1
			}
			if n := len(card.HumanChecks); n > 0 {
				label := "1"
				if n > 1 {
					label = fmt.Sprintf("1-%d", min(n, 9))
				}
				_, actor := m.cfg.Live.(cardActor)
				idle := len(m.actionFlights[row.engineID]) == 0
				l.add(markDrop, offer(actor && idle, k.MarkCheck, label, "tick/later"))
			}
			_, canFix := m.cfg.Live.(cardActor)
			l.add(fixDrop, offer(canFix, k.Fix, "F", "fix round"))
			l.add(dropRender, offer(len(card.Renders) > 0, k.Renders, "v", "renders"))
		}
		if m.paneOwner() != nil && m.cfg.Live != nil && !m.paneQuestion() {
			l.add(dropRow, short(k.Owner, "o", "answer"))
		}
		if _, refused := m.fileRefusal(); !refused && !m.paneQuestion() {
			l.add(dropRow, short(k.FileSlice, "f", "file slice"))
		}
		scroll()
		esc("close pane")
	}
	return l
}
