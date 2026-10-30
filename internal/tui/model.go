// SPDX-License-Identifier: GPL-3.0-or-later

// Package tui is ork's terminal UI: project sidebar, the Overview and the status
// line. It reads state through source.Source and sends owner actions through Live.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/poll"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// holdFlag is the flag slicer's default hold_flags holds an item on: the item
// waits for the owner.
const holdFlag = "needs-owner"

// Config is what the Model needs from outside. Everything but Source and
// Projects has a default, and tests replace the clock, the timer and the bell
// so no test sleeps.
type Config struct {
	Version     string                 // header version
	BuildID     string                 // this TUI build
	DaemonBuild string                 // daemon build reported by daemon.info
	Getwd       func() (string, error) // empty Welcome path offer; default os.Getwd
	Source      source.Source
	Projects    []registry.Project
	Now         func() time.Time              // default time.Now
	Schedule    poll.Schedule                 // default poll.DefaultSchedule
	TickEvery   time.Duration                 // how often the schedule is checked; default 1 s
	After       func(d time.Duration) tea.Cmd // a command that sends tickMsg after d; default tea.Tick
	FrameAfter  func(d time.Duration) tea.Cmd // sends frameMsg; default 250 ms tea.Tick
	ASCII       bool                          // use ASCII sparkline glyphs
	Bell        func()                        // rings the terminal bell; default writes BEL to stderr
	Theme       Theme
	Live        Live // the daemon's refresh, focus and notices; nil when there is no daemon
}

// tickMsg asks the Model to check which projects are due.
type tickMsg struct{}

// frameMsg advances only visible live animation, independently of polling.
type frameMsg struct{}

// snapshotMsg is one project's refresh coming back.
type snapshotMsg struct {
	name string
	snap source.Snapshot
}

// projectState is what the Model knows about one project.
type projectState struct {
	reg     registry.Project
	loaded  bool
	snap    source.Snapshot
	open    int  // open adj records, from the last Open call that worked
	held    int  // items held needs-owner, from the last Items call that worked
	hasOpen bool // Open has had a good reading
	hasHeld bool // Items has had a good reading
	running bool
	dirty   bool
}

func (p *projectState) needs() int {
	if p.setup() {
		return 0
	}
	n := p.open + p.held
	linked := map[string]bool{}
	for _, r := range p.snap.EngineRuns {
		if r.RelayRun != "" {
			linked[r.RelayRun] = true
		}
		n += r.FindingNeeds(p.snap.EngineRuns...)
		if r.Section(p.snap.EngineRuns) == source.SectionNeeds {
			n++
		}
	}
	for _, r := range boardLedgers(p.snap) {
		if !linked[r.ID] && ledgerOwnerQuestion(r) != nil {
			n++
		}
	}
	return n
}

// setup reports whether the project is loaded and has no .slicer/ folder.
func (p *projectState) setup() bool { return p.loaded && !p.snap.Unavailable && isSetup(p.snap) }

// isSetup reports whether slicer says the project has no .slicer/ folder: its
// items or runs call failed with that error.
func isSetup(s source.Snapshot) bool { return noSlicerErr(s.Items.Err) || noSlicerErr(s.Runs.Err) }

// noSlicerErr reports whether err is slicer's "no .slicer/ folder" error. slicer
// has no code of its own for it: it is the state code with that message.
func noSlicerErr(err error) bool {
	var te *clirun.ToolError
	return errors.As(err, &te) && te.Code == "state" && strings.HasPrefix(te.Message, "no .slicer/ found")
}

// The two aggregate sidebar entries, above the registry indices. Every index
// below zero shows all projects.
const (
	runningEntry = -2 // the live runs of every project (S110, adj 169)
	needsEntry   = -1
)

// Model is the Bubble Tea model of the whole TUI.
type Model struct {
	removing projectRemoval
	welcome  welcomeState
	cfg      Config
	keys     keyMap
	help     help.Model
	projects []*projectState
	byName   map[string]*projectState
	poller   *poll.Poller
	active   int // runningEntry, needsEntry, or a registry index
	view     int // the view in the bar: an index into views; 0 is the Overview

	questionsCursor int
	allProjects     bool
	decisionsTab    int
	decisionsCursor int
	decisionPane    *decisionRow
	decisionInput   *textField
	decisionScroll  int

	roadmapCursor int
	itemPane      *itemPane
	itemRequest   uint64
	cursor        int // the Overview row under the cursor; drawn clamped to the rows there are
	sidebarHidden bool
	filtering     bool
	filter        string
	focusing      bool
	showHelp      bool
	pageTop       pagerTop
	expanded      map[string]bool // the Running runs the owner opened, by project and run id (S164)
	width         int
	height        int
	treeFrame     int
	frameArmed    bool
	pane          *runPane
	confirm       *runAction
	jumpRun       string
	waves         *wavesState
	wavesRequest  uint64
	waveFlights   map[string]runAction
	launch        *launchState
	launchRequest uint64
	launchFlights map[string]launchFlight
	actionFlights map[string][]runAction
	renderSeq     int // the number of the last run.render ask; never restarts, so a closed pane's answer cannot match a new ask (S147)
	actionStatus  string
	lastRefresh   time.Time
	drawn         drawnRows // the body window of the last screen, for the page keys
	listening     bool      // the daemon's notice feed is being listened to (S13)

	compose        *composeState
	composeRequest uint64

	projectsReading  bool
	projectsRevision uint64
}

// New returns a Model that has not fetched anything. Init starts the fetches.
func New(cfg Config) *Model {
	if cfg.Getwd == nil {
		cfg.Getwd = os.Getwd
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Schedule == (poll.Schedule{}) {
		cfg.Schedule = poll.DefaultSchedule
	}
	if cfg.TickEvery <= 0 {
		cfg.TickEvery = time.Second
	}
	if cfg.After == nil {
		cfg.After = func(d time.Duration) tea.Cmd {
			return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
		}
	}
	if cfg.FrameAfter == nil {
		cfg.FrameAfter = func(d time.Duration) tea.Cmd { return tea.Tick(d, func(time.Time) tea.Msg { return frameMsg{} }) }
	}
	if cfg.Bell == nil {
		cfg.Bell = func() { fmt.Fprint(os.Stderr, "\a") }
	}
	m := &Model{active: needsEntry, cfg: cfg, keys: newKeyMap(), byName: map[string]*projectState{}}
	m.help = help.Model{
		ShortSeparator: "  ",
		FullSeparator:  "    ",
		Ellipsis:       "...",
		Styles: help.Styles{
			Ellipsis: cfg.Theme.Dim, ShortKey: cfg.Theme.Title, ShortDesc: cfg.Theme.Dim, ShortSeparator: cfg.Theme.Dim,
			FullKey: cfg.Theme.Title, FullDesc: cfg.Theme.Dim, FullSeparator: cfg.Theme.Dim,
		},
		ShowAll: true,
	}
	var names []string
	for _, p := range cfg.Projects {
		ps := &projectState{reg: p}
		m.projects = append(m.projects, ps)
		m.byName[p.Name] = ps
		names = append(names, p.Name)
	}
	m.poller = poll.New(cfg.Schedule, names...)

	return m
}

// Init fetches every project, starts the schedule and tells the daemon which
// project is in view.
func (m *Model) Init() tea.Cmd {
	var discover tea.Cmd
	if reader, ok := m.cfg.Source.(source.UnavailableReader); ok {
		discover = func() tea.Msg {
			groups, err := reader.UnavailableProjects(context.Background())
			return unavailableProjectsMsg{groups: groups, err: err}
		}
	}
	return tea.Batch(m.tick(m.cfg.Now()), discover)
}

type unavailableProjectsMsg struct {
	groups []source.Snapshot
	err    error
}

// focus tells the daemon which project the owner sees, so it reads that one
// every 5 s. It returns nil without a daemon.
func (m *Model) focus() tea.Cmd {
	live := m.cfg.Live
	if live == nil || len(m.projects) == 0 {
		return nil
	}
	names := []string{}
	if m.active >= 0 {
		if !m.projects[m.active].snap.Unavailable {
			names = append(names, m.projects[m.active].reg.Name)
		}
	} else {
		for _, p := range m.projects {
			if p.snap.Unavailable {
				continue
			}
			names = append(names, p.reg.Name)
		}
	}
	m.focusing = true
	return func() tea.Msg {
		return focusMsg{err: live.Focus(context.Background(), names)}
	}
}

// listen waits for the next change notice. It returns noticeMsg when a notice
// comes or the feed fails.
func (m *Model) listen() tea.Cmd {
	live := m.cfg.Live
	return func() tea.Msg {
		name, err := live.Next(context.Background())
		return noticeMsg{project: name, err: err}
	}
}

// tick starts a fetch for every project that is due and schedules the next
// check.
func (m *Model) tick(now time.Time) tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, m.discoverProjects())
	if m.cfg.Live != nil && !m.listening && !m.focusing {
		cmds = append(cmds, m.focus())
	}
	for _, name := range m.poller.Due(now) {
		if c := m.fetch(name, false); c != nil {
			cmds = append(cmds, c)
		}
	}
	cmds = append(cmds, m.cfg.After(m.cfg.TickEvery))
	return tea.Batch(cmds...)
}

// fetch returns the command that reads one project, or nil when a call for it
// is already in flight (dash.refresh: never started twice). With refresh the
// daemon reads the project now before the snapshot is taken.
func (m *Model) fetch(name string, refresh bool) tea.Cmd {
	ps := m.byName[name]
	if ps == nil || ps.snap.Unavailable || !m.poller.Start(name) {
		return nil
	}
	src, reg, live := m.cfg.Source, ps.reg, m.cfg.Live
	return func() tea.Msg {
		ctx := context.Background()
		if refresh && live != nil {
			// A failed refresh shows in the snapshot's parts, so its error is not kept.
			_ = live.Refresh(ctx, reg.Name)
		}
		return snapshotMsg{name: name, snap: src.Snapshot(ctx, reg)}
	}
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (model tea.Model, cmd tea.Cmd) {
	defer func() {
		if frame := m.armFrame(); frame != nil {
			cmd = tea.Batch(cmd, frame)
		}
	}()
	switch msg := msg.(type) {
	case unavailableProjectsMsg:
		if msg.err != nil {
			m.actionStatus = "Read unavailable runs: " + actionError(msg.err)
			return m, nil
		}
		for _, group := range msg.groups {
			if !group.Unavailable || m.byName[group.Project.Name] != nil {
				continue
			}
			ps := &projectState{reg: group.Project, loaded: true, snap: group}
			m.projects = append(m.projects, ps)
			m.byName[ps.reg.Name] = ps
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case frameMsg:
		m.frameArmed = false
		m.treeFrame = (m.treeFrame + 1) % 4
		return m, nil
	case tickMsg:
		return m, m.tick(m.cfg.Now())
	case snapshotMsg:
		cmd := m.apply(msg)
		m.settleRender()
		return m, cmd
	case projectsDiscoveredMsg:
		return m, m.applyProjectsDiscovered(msg)
	case projectRemovedMsg:
		return m, m.applyProjectRemoved(msg)
	case projectAddedMsg:
		return m, m.applyProjectAdded(msg)
	case newSliceMsg:
		return m, m.applyNew(msg)
	case noteMsg:
		return m, m.applyNote(msg)
	case itemShowMsg:
		if pane := m.itemPane; pane != nil && m.view == roadmapView && m.active >= 0 &&
			pane.project == msg.project && pane.id == msg.id && pane.request == msg.request &&
			m.projects[m.active].reg.Name == msg.project {
			pane.pending = false
			pane.detail, pane.err = msg.detail, msg.err
		}
	case matrixMsg:
		if pane := m.itemPane; pane != nil && m.matrixPaneOpen() && m.active >= 0 &&
			pane.project == msg.project && pane.request == msg.request &&
			m.projects[m.active].reg.Name == msg.project {
			pane.pending = false
			pane.matrix, pane.err = msg.result, msg.err
		}
	case wavesMsg:
		return m, m.applyWaves(msg)
	case launchSpecMsg:
		return m, m.applyLaunchSpec(msg)
	case specAnswerMsg:
		if state, done := m.activeSpec(); state != nil && state == msg.state {
			return m, m.applyAnswer(state, done, msg)
		}
		return m, nil
	case launchMsg:
		return m, m.applyLaunch(msg)
	case reportMsg:
		m.reportArrived(msg)
		return m, nil
	case renderMsg:
		m.renderArrived(msg)
		return m, nil
	case actionMsg:
		flights := m.actionFlights[msg.action.flightKey()]
		for i, a := range flights {
			if a.verb == msg.action.verb {
				m.actionFlights[msg.action.flightKey()] = append(flights[:i], flights[i+1:]...)
				break
			}
		}
		if len(m.actionFlights[msg.action.flightKey()]) == 0 {
			delete(m.actionFlights, msg.action.flightKey())
		}
		if msg.action.verb == "decide" {
			if msg.err != nil {
				m.actionStatus = actionError(msg.err)
			} else {
				m.actionStatus = msg.action.doneText()
				if p := m.decisionPane; p != nil && p.project == msg.action.project && p.record.ID == msg.action.adjID {
					m.decisionPane, m.decisionInput = nil, nil
				}
			}
		}
		if r, ok := m.actionRow(); ok && msg.action.run != "" && (r.engineID == msg.action.run || (msg.action.verb == "end" && r.ledgerID == msg.action.run)) && r.name == msg.action.project {
			switch {
			case msg.err != nil && msg.action.verb == "end" && isRefused(msg.err):
				m.actionStatus = actionError(msg.err) // the refusal names the run and its state
			case msg.err != nil:
				m.actionStatus = msg.action.named() + ": " + actionError(msg.err)
				if msg.action.verb == "answer" && m.pane != nil {
					m.pane.answered = false
				}
				if msg.action.verb == "fix" && m.pane != nil && m.pane.run == msg.action.run {
					// A failed fix keeps the note for another attempt.
					m.pane.fixing, m.pane.input = true, textField{}
					m.pane.input.insert(msg.action.text)
				}
			case msg.action.verb == "answer":
				m.actionStatus = msg.message
				if m.pane == nil {
					// The owner closed the pane while the call ran.
					m.actionStatus = "answered"
					if msg.message != "" {
						m.actionStatus += "; " + msg.message
					}
					break
				}
				m.pane.input = textField{}
				if msg.action.finding > 0 {
					m.pane.finding = 0
					m.pane.answered = false
					if m.actionStatus == "" {
						m.actionStatus = "answer recorded"
					}
					break
				}
				// A tracking that the run's next state already dropped stays dropped.
				m.pane.answerDone, m.pane.answerNotice = true, msg.message
				m.pane.answerStatus = m.actionStatus // the result replaced the status; tracking writes over it
				m.refreshAnswerStatus()
			default:
				m.actionStatus = msg.action.doneText()
				if msg.message != "" {
					m.actionStatus += "; " + msg.message
				}
			}
		}
		if m.poller.InFlight(msg.action.project) {
			m.byName[msg.action.project].dirty = true
		}
		return m, m.fetch(msg.action.project, true)
	case focusMsg:
		m.focusing = false
		// The feed starts once the daemon has the first focus, and a notice
		// that failed it stopped the feed: a focus that works starts it again.
		if msg.err == nil && !m.listening && m.cfg.Live != nil {
			m.listening = true
			return m, m.listen()
		}
	case noticeMsg:
		if msg.err != nil {
			m.listening = false
			return m, nil
		}
		if ps := m.byName[msg.project]; ps != nil && m.poller.InFlight(msg.project) {
			ps.dirty = true
		}
		return m, tea.Batch(m.fetch(msg.project, false), m.listen())
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.PasteMsg:
		if m.removing.name != "" {
			return m, nil
		}
		if m.welcome.editing || m.registeredProjectCount() == 0 {
			if m.welcome.editing && m.welcome.confirm == "" && !m.welcome.pending {
				m.welcome.field.update(msg)
				m.actionStatus = ""
			}
			return m, nil
		}
		if state, _ := m.activeSpec(); state != nil {
			if s := state.sitting; s.free {
				s.input.update(msg)
			}
			return m, nil
		}
		if e := m.activeLineup(); e != nil {
			if e.custom {
				e.fields[e.focus].update(msg)
			} else if e.row > len(lineupRoles) && e.row <= len(lineupRoles)+len(e.budgets) {
				e.budgets[e.row-len(lineupRoles)-1].update(msg)
			}
			m.actionStatus = ""
			return m, nil
		}
		if m.waves != nil && m.waves.inputs != nil {
			in := m.waves.inputs
			in.fields[in.focus].update(msg)
			m.actionStatus = ""
			return m, nil
		}
		if m.decisionInput != nil && m.confirm == nil {
			m.decisionInput.update(msg)
			return m, nil
		}
		if m.compose != nil {
			if !m.compose.pending {
				m.compose.field().update(msg)
				m.actionStatus = ""
			}
			return m, nil
		}
		if m.launchPrompt() {
			m.launch.input.update(msg)
			m.actionStatus = ""
			return m, nil
		}
		if m.confirm == nil && m.pane != nil && m.paneQuestion() && (m.paneAsk() == nil || m.pane.freeText) {
			m.pane.input.update(msg)
			m.actionStatus = ""
		}
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	if m.reportOn() > 0 {
		return m.paneKey(msg)
	}
	if m.welcome.editing || m.welcome.pending || m.welcome.confirm != "" {
		return m.welcomeKey(msg)
	}
	if m.removing.name != "" {
		return m.removeProjectKey(msg)
	}
	if !m.projectTextEntry() && m.confirm == nil {
		switch msg.String() {
		case "+":
			return m.welcomeKey(msg)
		case "-":
			return m.requestProjectRemoval()
		}
	}
	if key.Matches(msg, m.keys.RowNext, m.keys.RowPrev, m.keys.Next, m.keys.Prev, m.keys.NextView, m.keys.PrevView, m.keys.Jump, m.keys.First, m.keys.Last, m.keys.PageDown, m.keys.PageUp, m.keys.NextGroup, m.keys.PrevGroup, m.keys.Filter, m.keys.Select, m.keys.Close) {
		m.jumpRun = ""
	}
	if m.registeredProjectCount() == 0 && m.pane == nil {
		editing := m.welcome.editing || m.welcome.pending || m.welcome.confirm != ""
		navigation := key.Matches(msg, m.keys.Next, m.keys.Prev, m.keys.Jump, m.keys.Sidebar)
		if len(m.projects) == 0 || editing || (m.active < 0 && !navigation) {
			return m.welcomeKey(msg)
		}
	}
	if m.confirm != nil {
		switch msg.String() {
		case "e":
			if m.confirm.verb == "launch" {
				return m.openLaunchLineup()
			}
		case "ctrl+c":
			return tea.Quit
		case "y", "Y":
			a := *m.confirm
			m.confirm = nil
			return m.callAction(a)
		case "n", "N", "enter", "esc":
			if m.confirm.verb == "launch" {
				m.dropLaunch()
			}
			m.confirm = nil
			m.actionStatus = "Cancelled"
		}
		return nil
	}

	if state, done := m.activeSpec(); state != nil {
		return m.sittingKey(state, done, msg)
	}
	if m.waves != nil && m.waves.lineup != nil {
		return m.lineupKey(msg)
	}
	if m.waves != nil && m.waves.inputs != nil {
		return m.waveInputKey(msg)
	}
	if m.view == wavesView && m.waves != nil && m.waves.pane != nil {
		return m.waveItemKey(msg)
	}
	if m.decisionInput != nil {
		return m.decisionKey(msg)
	}
	if m.decisionPane != nil && !m.showHelp && m.decisionAnswerable() {
		switch msg.String() {
		case "1", "2", "3", "4", "t":
			return m.decisionKey(msg)
		}
	}
	if m.compose != nil {
		return m.composeKey(msg)
	}
	if m.launch != nil {
		if m.launch.stage == launchEdit {
			return m.launchLineupKey(msg)
		}
		return m.launchKey(msg)
	}

	if m.pane != nil {
		return m.paneKey(msg)
	}

	if m.filtering {
		switch msg.String() {
		case "ctrl+c":
			return tea.Quit
		case "esc":
			m.filtering = false
			m.filter = ""
		case "enter":
			m.filtering = false
		case "backspace":
			r := []rune(m.filter)
			if len(r) > 0 {
				m.filter = string(r[:len(r)-1])
			}
		default:
			if msg.Text != "" {
				m.filter += msg.Text
			}
		}
		return nil
	}
	switch {
	case m.view == wavesView && !m.showHelp && key.Matches(msg, m.keys.AllProjects):
		if w := m.waves; w != nil {
			rows := w.rows()
			id := ""
			if i := clampCursor(w.cursor, len(rows)); i >= 0 {
				id = rows[i].ID
			}
			w.showAll = true
			for i, row := range w.rows() {
				if row.ID == id {
					w.cursor = i
					break
				}
			}
		}
		return nil
	case m.view != decisionsView && !m.showHelp && key.Matches(msg, m.keys.TabNext, m.keys.TabPrev):
		// Left and right do nothing here: they belong to the Decisions tabs,
		// the lineup and the text fields. The status says what switches projects.
		m.actionStatus = "[ and ] switch projects"
		return nil
	case m.showHelp && key.Matches(msg, m.keys.RowNext, m.keys.RowPrev):
		// The cursor is hidden under the help: the line keys leave it where it is.
		return nil
	case m.matrixPaneOpen() && !m.showHelp && key.Matches(msg, m.keys.RowNext, m.keys.RowPrev, m.keys.PageDown, m.keys.PageUp):
		m.scrollMatrix(msg)
	case key.Matches(msg, m.keys.ScrollDown, m.keys.ScrollUp):
		m.scrollHalf(key.Matches(msg, m.keys.ScrollUp))
	case key.Matches(msg, m.keys.First, m.keys.Last, m.keys.PageDown, m.keys.PageUp, m.keys.NextGroup, m.keys.PrevGroup):
		m.moveRows(msg)
	case m.view == questionsView && key.Matches(msg, m.keys.RowNext, m.keys.RowPrev):
		delta := 1
		if key.Matches(msg, m.keys.RowPrev) {
			delta = -1
		}
		if m.decisionPane != nil {
			m.scrollDecision(delta, false)
		} else {
			n := len(m.selectableQuestions())
			m.questionsCursor = clampCursor(clampCursor(m.questionsCursor, n)+delta, n)
		}
	case m.view == questionsView && key.Matches(msg, m.keys.Select):
		return m.openQuestion()
	case m.view == decisionsView && key.Matches(msg, m.keys.AllProjects):
		m.allProjects = !m.allProjects
		m.decisionsCursor = 0
		m.decisionPane = nil
	case m.view == decisionsView && key.Matches(msg, m.keys.TabNext, m.keys.TabPrev):
		m.decisionsTab = 1 - m.decisionsTab
		m.decisionsCursor = 0
		m.decisionPane = nil
	case m.view == decisionsView && key.Matches(msg, m.keys.RowNext, m.keys.RowPrev):
		delta := 1
		if key.Matches(msg, m.keys.RowPrev) {
			delta = -1
		}
		if m.decisionPane != nil {
			m.scrollDecision(delta, false)
		} else {
			n := len(m.decisionRecords())
			m.decisionsCursor = clampCursor(clampCursor(m.decisionsCursor, n)+delta, n)
		}
	case m.view == decisionsView && key.Matches(msg, m.keys.Select):
		m.openDecision()
	case m.view == 0 && m.active == runningEntry && !m.showHelp && key.Matches(msg, m.keys.Expand):
		m.toggleExpanded()
	case m.view == 0 && m.active == runningEntry && !m.showHelp && key.Matches(msg, m.keys.ExpandAll):
		m.toggleAllExpanded()
	case key.Matches(msg, m.keys.EditLineup):
		return m.openLineup()
	case key.Matches(msg, m.keys.Tick):
		return m.tickWave()
	case key.Matches(msg, m.keys.New):
		return m.openNew()
	case key.Matches(msg, m.keys.Note):
		return m.openNote()
	case key.Matches(msg, m.keys.Launch):
		if m.view == wavesView {
			return m.confirmWave()
		}
		return m.openLaunch()
	case key.Matches(msg, m.keys.Matrix):
		return m.openMatrix()
	case key.Matches(msg, m.keys.Cleanup):
		return m.requestAction("cleanup")
	case key.Matches(msg, m.keys.Approve):
		return m.requestAction("approve")
	case key.Matches(msg, m.keys.Stop):
		return m.requestAction("stop")
	case key.Matches(msg, m.keys.Retry):
		return m.requestAction("retry")
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
	case key.Matches(msg, m.keys.Sidebar):
		m.sidebarHidden = !m.sidebarHidden
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
	case key.Matches(msg, m.keys.Close):
		m.actionStatus = ""
		m.itemPane = nil
		m.decisionPane = nil
		m.showHelp = false
		m.filter = ""
	case key.Matches(msg, m.keys.Next):
		return m.move(1)
	case key.Matches(msg, m.keys.Prev):
		return m.move(-1)
	case key.Matches(msg, m.keys.NextView):
		return m.moveView(1)
	case key.Matches(msg, m.keys.PrevView):
		return m.moveView(-1)
	case key.Matches(msg, m.keys.RowNext):
		if m.view == wavesView {
			if m.waves != nil {
				m.waves.cursor = clampCursor(m.waves.cursor+1, len(m.waves.rows()))
			}
			return nil
		}
		if m.view == roadmapView {
			if m.itemPane == nil {
				n := len(m.roadmapItems())
				m.roadmapCursor = clampCursor(clampCursor(m.roadmapCursor, n)+1, n)
			}
		} else {
			m.moveRow(1)
		}
	case key.Matches(msg, m.keys.RowPrev):
		if m.view == wavesView {
			if m.waves != nil {
				m.waves.cursor = clampCursor(m.waves.cursor-1, len(m.waves.rows()))
			}
			return nil
		}
		if m.view == roadmapView {
			if m.itemPane == nil {
				n := len(m.roadmapItems())
				m.roadmapCursor = clampCursor(clampCursor(m.roadmapCursor, n)-1, n)
			}
		} else {
			m.moveRow(-1)
		}
	case key.Matches(msg, m.keys.Select):
		if m.view == wavesView {
			return m.openWaveItem()
		}
		if m.view == roadmapView {
			return m.openItem()
		}
		if r, ok := m.selectedRow(); ok {
			if (r.engineID != "" || r.ledgerID != "") && !m.showHelp {
				return m.openPane(r)
			}
			return m.switchTo(r.project)
		}
	case key.Matches(msg, m.keys.Jump):
		return m.jump(int(msg.String()[0] - '0'))
	case key.Matches(msg, m.keys.Refresh):
		if m.active >= 0 {
			return m.fetch(m.projects[m.active].reg.Name, true)
		}
		var cmds []tea.Cmd
		for _, p := range m.projects {
			cmds = append(cmds, m.fetch(p.reg.Name, true))
		}
		return tea.Batch(cmds...)
	}
	return nil
}

// rowCursor is the cursor the row keys move in the current view and the number
// of rows it walks. It is nil when an open pane holds the view.
func (m *Model) rowCursor() (*int, int) {
	switch m.view {
	case questionsView:
		return &m.questionsCursor, len(m.selectableQuestions())
	case decisionsView:
		return &m.decisionsCursor, len(m.decisionRecords())
	case wavesView:
		if m.waves == nil {
			return nil, 0
		}
		return &m.waves.cursor, len(m.waves.rows())
	case roadmapView:
		if m.itemPane != nil {
			return nil, 0
		}
		return &m.roadmapCursor, len(m.roadmapItems())
	}
	return &m.cursor, len(m.cursorRows())
}

// groupStarts lists the cursor rows that start a group: on the Overview each
// Needs you group, then the Running, Recent and Ready sections; on the
// Roadmap each run of items with one pass, the first row under its heading; on Waves each wave, then Unplaced.
func (m *Model) groupStarts() []int {
	switch m.view {
	case roadmapView:
		var starts []int
		for _, run := range passRuns(m.roadmapItems()) {
			starts = append(starts, run.start)
		}
		return starts
	case wavesView:
		if m.waves == nil {
			return nil
		}
		return m.waves.groupStarts()
	}
	if m.view != 0 {
		return nil
	}
	var starts []int
	next := 0
	section := func(n int) {
		if n > 0 {
			starts = append(starts, next)
		}
		next += n
	}
	if m.active == runningEntry {
		section(len(m.runningRows()))
		section(len(m.recentRows()))
		return starts
	}
	needs, running := m.rows()
	for i, r := range needs {
		if i == 0 || r.group != needs[i-1].group {
			starts = append(starts, next)
		}
		next++
	}
	section(len(running))
	section(len(m.recentRows()))
	section(len(m.readyRows()))
	return starts
}

// scrollHalf is ctrl+d (ctrl+u when back) in the decision pane of Questions
// and Decisions and in the Roadmap item pane: half of what the pane shows
// (S152). It does nothing elsewhere, with the help open included.
func (m *Model) scrollHalf(back bool) {
	if m.showHelp {
		return
	}
	switch {
	case m.decisionPane != nil && (m.view == questionsView || m.view == decisionsView):
		_, visible := m.decisionPage()
		step := halfPage(visible)
		if back {
			step = -step
		}
		m.scrollDecision(step, false)
	case m.itemPane != nil && m.view == roadmapView:
		step := halfPage(m.itemPane.room)
		if back {
			step = -step
		}
		m.itemPane.offset = max(m.itemPane.offset+step, 0)
	}
}

// moveRows handles the first, last, page and group keys. They move the row
// cursor of the view. With a decision open on Questions or Decisions the page
// keys scroll it, and the others do nothing. With the help open they do
// nothing: the cursor is hidden under it.
func (m *Model) moveRows(msg tea.KeyPressMsg) {
	if m.showHelp {
		return
	}
	if m.decisionPane != nil && (m.view == questionsView || m.view == decisionsView) {
		switch {
		case key.Matches(msg, m.keys.PageDown):
			m.scrollDecision(1, true)
		case key.Matches(msg, m.keys.PageUp):
			m.scrollDecision(-1, true)
		}
		return
	}
	cur, n := m.rowCursor()
	if cur == nil {
		return
	}
	at := clampCursor(*cur, n)
	if at < 0 {
		return
	}
	next := at
	switch {
	case key.Matches(msg, m.keys.First):
		next = 0
	case key.Matches(msg, m.keys.Last):
		next = n - 1
	case key.Matches(msg, m.keys.PageDown):
		next = m.pageDown(at, n)
	case key.Matches(msg, m.keys.PageUp):
		next = m.pageUp(at)
	case key.Matches(msg, m.keys.NextGroup):
		for _, start := range m.groupStarts() {
			if start > at {
				next = start
				break
			}
		}
	case key.Matches(msg, m.keys.PrevGroup):
		for _, start := range m.groupStarts() {
			if start < at {
				next = start
			}
		}
	}
	if cur == &m.cursor && next != at {
		m.actionStatus = ""
	}
	*cur = next
}

// pagerTop holds the window of a page down: the body line it starts at, kept
// while the cursor stays inside it (S151). A key that moves the cursor out of
// the window drops it, and the window follows the cursor again.
type pagerTop struct {
	view, active, line int
	on                 bool
}

// pageDown is the row a pgdown puts the cursor on. The page keys scroll like
// less ctrl-f: the window starts at the first row below the one on screen, and
// the cursor goes to that row. It draws the screen once, for the rows it shows.
func (m *Model) pageDown(at, n int) int {
	m.screen()
	d := m.drawn
	m.pageTop = pagerTop{}
	if len(d.rows) != n {
		return min(at+1, n-1)
	}
	for row, line := range d.rows {
		if line >= d.hi {
			row = max(row, at)
			m.pageTop = pagerTop{view: m.view, active: m.active, line: d.rows[row], on: true}
			return row
		}
	}
	return n - 1 // every row is on screen: the end
}

// pageUp is the mirror: the cursor goes to the last row above the window, which
// then ends the window.
func (m *Model) pageUp(at int) int {
	m.screen()
	d := m.drawn
	m.pageTop = pagerTop{}
	for row := len(d.rows) - 1; row >= 0; row-- {
		if d.rows[row] < d.lo {
			return min(row, at)
		}
	}
	return 0
}

// switchTo selects registry project i, or an aggregate entry below zero, and
// updates polling.
// It returns the command that tells the daemon which projects are visible.
func (m *Model) switchTo(i int) tea.Cmd {
	n := len(m.projects)
	if n == 0 {
		return nil
	}
	if m.launch == nil || m.launch.stage != launchStarting {
		m.actionStatus = ""
	}
	m.dropLaunch()
	m.compose = nil
	m.pane = nil
	m.active = i
	m.roadmapCursor = 0
	m.decisionsCursor = 0
	m.questionsCursor = 0
	m.decisionPane = nil
	m.itemPane = nil
	m.cursor = 0
	if i >= 0 {
		m.poller.SetVisible(m.projects[i].reg.Name)
	} else {
		m.poller.SetVisible("")
	}
	return tea.Batch(m.focus(), m.openWaves())
}

// apply stores a returned snapshot. A part whose call failed keeps its last
// good count, so an error neither lowers the badge nor later looks like a
// new need. The bell rings when a part's count rises past an earlier good
// reading of that same part; a part's first good reading is its baseline, so
// needs that were already there before ork could read them do not ring.
func (m *Model) apply(msg snapshotMsg) tea.Cmd {
	ps := m.byName[msg.name]
	if ps == nil || ps.snap.Unavailable {
		return nil
	}
	now := m.cfg.Now()
	m.poller.Finish(msg.name, now)
	m.lastRefresh = now

	rose := false
	if isSetup(msg.snap) {
		// No .slicer/ folder: the project counts for nothing and rings nothing.
		// Its counts reset, so the first reading after a setup is a baseline.
		ps.open, ps.hasOpen, ps.held, ps.hasHeld, ps.running = 0, false, 0, false, false
	} else {
		if msg.snap.Open.Err == nil {
			n := len(msg.snap.Open.Value)
			rose = rose || (ps.hasOpen && n > ps.open)
			ps.open, ps.hasOpen = n, true
		}
		if msg.snap.Items.Err == nil {
			n := countHeld(msg.snap.Items.Value)
			rose = rose || (ps.hasHeld && n > ps.held)
			ps.held, ps.hasHeld = n, true
		}
		linked := map[string]bool{}
		engineActive := false
		for _, r := range msg.snap.EngineRuns {
			if r.RelayRun != "" {
				linked[r.RelayRun] = true
			}
			if r.Section(msg.snap.EngineRuns) == source.SectionRunning {
				engineActive = true
			}
		}
		if msg.snap.Runs.Err == nil {
			ps.running = engineActive
			for _, r := range msg.snap.Runs.Value {
				if !linked[r.ID] && ledgerOwnerQuestion(r) == nil {
					ps.running = true
				}
			}
		} else if engineActive {
			ps.running = true
		}

	}
	ps.loaded = true
	ps.snap = msg.snap
	if m.pane != nil && m.pane.project == msg.name {
		m.refreshAnswerStatus()
		m.followNewestRun(msg.name)
	}
	m.selectJumpRun()
	m.pruneExpanded()
	var cmds []tea.Cmd
	if ps.dirty {
		ps.dirty = false
		cmds = append(cmds, m.fetch(msg.name, false))
	}
	if rose {
		bell := m.cfg.Bell
		cmds = append(cmds, func() tea.Msg { bell(); return nil })
	}
	return tea.Batch(cmds...)
}

func countHeld(items []slicercli.Item) int {
	n := 0
	for _, it := range items {
		if isHeld(it) {
			n++
		}
	}
	return n
}

func isHeld(it slicercli.Item) bool {
	for _, f := range it.Fields.Flags {
		if f == holdFlag {
			return true
		}
	}
	return false
}

// navigation lists registry indices in stable priority order, with the
// Running entry and Needs you first.
func (m *Model) navigation() []int {
	out := []int{runningEntry, needsEntry}
	for group := 0; group < 3; group++ {
		for i, p := range m.projects {
			rank := 2
			if p.needs() > 0 {
				rank = 0
			} else if p.running {
				rank = 1
			}
			if rank == group && (i == m.active || strings.Contains(strings.ToLower(p.reg.Name), strings.ToLower(m.filter))) {
				out = append(out, i)
			}
		}
	}
	return out
}

// numberedProjects lists the registry indices of the first nine project rows in
// the order the sidebar draws them. The sidebar numbers them 1 to 9 and the
// number keys select them (S85).
func (m *Model) numberedProjects() []int {
	var out []int
	for _, idx := range m.navigation() {
		if idx >= 0 && len(out) < 9 {
			out = append(out, idx)
		}
	}
	return out
}

// jump handles a number key: 0 selects Needs you, 1-9 the project the sidebar
// shows with that number. A number with no project does nothing.
func (m *Model) jump(n int) tea.Cmd {
	if n == 0 {
		return m.switchTo(needsEntry)
	}
	if projects := m.numberedProjects(); n <= len(projects) {
		return m.switchTo(projects[n-1])
	}
	return nil
}

func (m *Model) move(delta int) tea.Cmd {
	nav := m.navigation()
	pos := 0
	for i, n := range nav {
		if n == m.active {
			pos = i
			break
		}
	}
	return m.switchTo(nav[(pos+delta+len(nav))%len(nav)])
}

// moveView steps to the next built view in direction delta and wraps around.
// With no other view built it does nothing (ui.view-list).
func (m *Model) moveView(delta int) tea.Cmd {
	n := len(views)
	for step := 1; step < n; step++ {
		i := ((m.view+delta*step)%n + n) % n
		if views[i].built {
			if m.launch != nil && m.launch.stage != launchStarting {
				m.actionStatus = ""
			}
			m.dropLaunch()
			m.compose = nil
			m.view = i
			m.roadmapCursor = 0
			m.decisionsCursor = 0
			m.questionsCursor = 0
			m.pane = nil
			m.decisionPane = nil
			m.itemPane = nil
			return m.openWaves()
		}
	}
	return nil
}

// runAction retains the exact target while a confirmation is on screen.
type runAction struct {
	adjID                    int
	option, adjTitle         string
	adjRecommended           bool
	stopAfter                string
	findingTitle             string
	scores                   *runrec.FindingScores
	verb, run, project, text string
	item                     string // file: the slice the finding came from
	finding                  int    // file: the one-based finding
	check                    int    // check: the one-based human check
	state                    string // check: ticked, later or "" for open
	adj                      bool
	by                       string
	cleanupCommand           string
	worktreeGone             bool // cleanup: the daemon skips git worktree remove, so the confirm omits it (S153)
	items                    []daemon.RunParams
	held                     []string // wave: ticked items left out because they wait on spec questions
	lineup                   map[string][]runrec.LineupEntry
	budgets                  *registry.Budgets
	plan                     *daemon.RunPlan
	wellSpecified, split     bool
	push                     bool   // approve: the project's push setting is on
	outcome                  string // approve: "committed", "landed", "fix round", or ""
}

// cleanupCommand is the git commands of the cleanup key that will run, one per
// line (git.cleanup-key, adj 152). The daemon skips git worktree remove when
// the worktree is gone, so the command then leaves it out (S153).
func cleanupCommand(project, worktree, branch string, worktreeGone bool) string {
	path := shellQuote(project)
	branchStep := "git -C " + path + " branch -d " + shellQuote(branch)
	if worktreeGone {
		return branchStep
	}
	return "git -C " + path + " worktree remove " + shellQuote(worktree) + "\n" + branchStep
}

// cleanupQuestion asks about the steps that will run.
func (a runAction) cleanupQuestion() string {
	if a.worktreeGone {
		return "Remove branch? [y/N] (Enter is No)"
	}
	return "Remove worktree and branch? [y/N] (Enter is No)"
}

// named is how the status line calls the action. The cleanup is named by its
// item (S63, not the run id): its git commands are the confirm's, and the
// status must not echo steps that may not have run (S153).
func (a runAction) named() string {
	if a.verb == "cleanup" {
		return "cleanup " + a.cleanupItem()
	}
	if a.verb == "end" {
		return "end ledger run " + a.run
	}
	return a.command()
}

// cleanupItem is the slice a cleanup acts on; the run id stands in when the
// row carried none.
func (a runAction) cleanupItem() string {
	if a.item != "" {
		return a.item
	}
	return a.run
}

func (a runAction) command() string {
	if a.verb == "decide" {
		flag, value := "--option", a.option
		if value == "" {
			flag, value = "--text", a.text
		}
		return fmt.Sprintf("ork adj answer %s %d %s %s", a.project, a.adjID, flag, strconv.Quote(value))
	}
	if a.verb == "cleanup" {
		return a.cleanupCommand
	}
	if a.verb == "wave" {
		cmd := "ork wave start " + a.project
		for _, p := range a.items {
			cmd += " " + p.Item
		}
		return cmd
	}
	if a.verb == "launch" {
		return "ork run " + a.project + " " + a.run
	}
	if a.verb == "end" {
		return ledgerEndCommand(a.run)
	}
	if a.verb == "answer" && a.finding > 0 {
		return "Record answer to " + strconv.Quote(a.findingTitle) + ": " + a.text
	}
	if a.verb == "file" {
		return "slicer add " + shellQuote(a.text) + " --discovered-from " + a.item
	}
	// The approval card's actions have no ork command (S118): the status line
	// names them in words (card.go).
	if a.verb == "fix" {
		return "fix note"
	}
	if a.verb == "check" {
		return fmt.Sprintf("mark of check %d", a.check)
	}
	cmd := "ork " + a.verb + " " + a.run
	if a.verb == "answer" {
		cmd += " " + shellQuote(a.text)
	}
	return cmd
}

// prompt keeps the cleanup and approve questions separate from the command.
func (a runAction) prompt() string {
	if a.verb == "cleanup" {
		return a.command() + "\n" + a.cleanupQuestion()
	}
	if a.verb == "approve" || a.verb == "file" || a.verb == "end" || a.verb == "decide" {
		return a.promptWidth(0)
	}
	prompt := a.command() + "? [y/N] (Enter is No)"
	if len(a.held) > 0 {
		verb := "waits"
		if len(a.held) > 1 {
			verb = "wait"
		}
		// Outside the command: ork does not run it with these items.
		prompt += "  " + strings.Join(a.held, ", ") + " " + verb + " on spec questions"
	}
	return prompt
}

// promptWidth is the prompt for a pane w columns wide (0: no limit). The
// approve prompt reads like the cleanup one: the command, then the question
// naming the landing, the push when the project pushes, and the cleanup the
// approval confirms (S119, adj 162, adj 152). The keys `[y/N] (Enter is No)`
// stay whole: they go on their own line when question and keys do not fit one.
func (a runAction) promptWidth(w int) string {
	if a.verb == "decide" {
		label := a.option
		if label == "" {
			label = a.text
		}
		picked := strconv.Quote(label)
		if a.adjRecommended {
			picked += " (recommended)"
		}
		question := fmt.Sprintf("Decide #%d (%s) as %s in adj as %s?", a.adjID, a.adjTitle, picked, a.by)
		const keys = "[y/N] (Enter is No)"
		if w > 0 && lipgloss.Width(question+" "+keys) > w {
			return question + "\n" + keys
		}
		return question + " " + keys
	}

	if a.verb == "file" {
		note := "ork adds a Why section from the finding."
		if a.scores != nil {
			note += " Then slicer set applies proposed importance, urgency and effort."
		}
		question := "File this finding as a slice?"
		const keys = "[y/N] (Enter is No)"
		if w > 0 && lipgloss.Width(question+" "+keys) > w {
			return a.command() + "\n" + note + "\n" + question + "\n" + keys
		}
		return a.command() + "\n" + note + "\n" + question + " " + keys
	}
	if a.verb == "end" {
		const question = "End this run as stopped and fail its unfinished steps?"
		const keys = "[y/N] (Enter is No)"
		if w > 0 && lipgloss.Width(question+" "+keys) > w {
			return a.command() + "\n" + question + "\n" + keys
		}
		return a.command() + "\n" + question + " " + keys
	}
	if a.verb != "approve" {
		return a.prompt()
	}
	question := "Land " + a.item + ", then remove its worktree and branch?"
	if a.push {
		question = "Land and push " + a.item + ", then remove its worktree and branch?"
	}
	if a.stopAfter == runrec.StopAfterCommit {
		question = "Commit " + a.item + " on its branch?"
	}
	if a.item == "" {
		question = "Approve run " + a.run + "?"
	}
	const keys = "[y/N] (Enter is No)"
	if w > 0 && lipgloss.Width(question+" "+keys) > w {
		return a.command() + "\n" + question + "\n" + keys
	}
	return a.command() + "\n" + question + " " + keys
}

// cleanupConfirmLines caps long commands separately so both command starts
// and the default-no question remain visible while the body keeps three lines.
func (a runAction) cleanupConfirmLines(width, limit int) []string {
	limit = max(limit, 3)
	commands := strings.Split(a.command(), "\n")
	var lines []string
	for i, command := range commands {
		wrapped := wrapText(command, width)
		budget := (limit - 1) / 2
		if i == 0 {
			budget += (limit - 1) % 2
		}
		if len(wrapped) > budget {
			wrapped = wrapped[:budget]
			wrapped[budget-1] = clip(wrapped[budget-1]+"...", width)
		}
		lines = append(lines, wrapped...)
	}
	return append(lines, a.cleanupQuestion())
}

type actionMsg struct {
	message string
	action  runAction
	err     error
}

// actionRefusal is why the row refuses verb: the status line text, or "" when
// the refusal is silent. It reports refused false when the row takes the key. The
// keys refuse by it and the hint line (hintBindings) lists a key only when it
// does not, so the two cannot disagree.
func (m *Model) actionRefusal(verb string, r ovRow, ok bool) (status string, refused bool) {
	if !ok || r.engineID == "" {
		return "No engine run on this row; no actions", true
	}
	if r.unavailable {
		return source.RemovedProjectHelp(r.name), true
	}
	if r.ready && verb != "cleanup" {
		return "Ready rows have no run actions; c cleans up a merged row", true
	}
	if r.ready && verb == "cleanup" {
		switch {
		case r.branch == "":
			return "cleanup needs a merged branch; branch unknown", true
		case !r.mergeKnown:
			return "cleanup needs a merged branch; merge state unknown", true
		case !r.merged:
			return "cleanup needs a merged branch; this one is unmerged", true
		case r.worktree == "":
			return "cleanup needs a worktree; worktree unknown", true
		}
	}
	if verb == "cleanup" && (!r.ready || !r.merged || r.branch == "" || r.worktree == "") {
		return "", true
	}
	for _, a := range m.actionFlights[r.engineID] {
		// Stop can interrupt an approve or retry, but a second stop waits.
		if verb != "stop" || a.verb == "stop" {
			if a.verb == "cleanup" {
				// One wording for the pending state, first press or second.
				return a.pendingText(), true
			}
			return a.command() + " is pending", true
		}
	}
	if verb == "stop" && r.state == "stopped" {
		return "run is already stopped; clear with: ork dismiss " + r.engineID, true
	}
	if verb == "approve" && r.state != "awaiting-approval" {
		return "approve needs " + stateWord("awaiting-approval") + "; run is " + stateWord(r.state), true
	}
	if verb == "approve" {
		if why := m.approvalBlock(r.name, r.engineID, m.pane != nil); why != "" {
			return why, true
		}
	}
	if verb == "retry" {
		if r.state != "paused" {
			return "retry needs paused; run is " + r.state, true
		}
		if r.pending == "question" {
			return "answer with: ork answer " + r.engineID + " TEXT", true
		}
	}
	if verb == "answer" && (m.pane == nil || m.pane.finding == 0) && (r.state != "paused" || r.pending != "question") {
		return "", true
	}
	return "", false
}

func (m *Model) requestAction(verb string) tea.Cmd {
	if m.showHelp || m.view == roadmapView || m.view == wavesView || m.view == decisionsView || (m.view == questionsView && m.pane == nil) {
		return nil
	}
	r, ok := m.actionRow()
	if status, refused := m.actionRefusal(verb, r, ok); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	if verb == "answer" && strings.TrimSpace(m.pane.input.Value()) == "" {
		m.actionStatus = "Answer must not be empty"
		return nil
	}
	if m.cfg.Live == nil {
		m.actionStatus = "No daemon connection; action unavailable"
		return nil
	}
	a := runAction{verb: verb, run: r.engineID, project: r.name}
	if verb == "cleanup" {
		a.worktreeGone = r.worktreeGone
		a.item = r.id
		a.cleanupCommand = cleanupCommand(m.projects[r.project].reg.Path, r.worktree, r.branch, r.worktreeGone)
	}
	if verb == "answer" {
		a.text = m.pane.input.Value()
		a.finding = m.pane.finding
		if f := m.paneOwner(); a.finding > 0 && f != nil {
			a.findingTitle = f.Title
		}
		_, reason := m.paneAdjKey()
		a.adj = m.pane.adj && reason == ""
		if a.adj || a.finding > 0 {
			a.by = os.Getenv("ADJ_DECIDED_BY")
		}
	}
	if verb == "approve" {
		// The prompt and the status line name the slice the approval lands.
		a.push = m.projects[r.project].reg.Push
		for _, run := range m.projects[r.project].snap.EngineRuns {
			if run.ID == r.engineID {
				a.item, a.stopAfter = run.Item, run.StopAfter
			}
		}
	}
	if verb == "approve" && m.pane == nil {
		// On an Overview row approval asks first: it lands the slice and
		// cleans up (S119, adj 162). The approval card's own key stays one key.
		m.actionStatus = ""
		m.confirm = &a
		return nil
	}
	if verb == "approve" || (verb == "answer" && (m.paneAsk() == nil || m.pane.freeText)) {
		return m.callAction(a)
	}
	m.actionStatus = ""
	m.confirm = &a
	return nil
}

func (m *Model) callAction(a runAction) tea.Cmd {
	if a.verb == "wave" {
		return m.startWave(a)
	}
	if a.verb == "launch" {
		return m.startLaunch(a)
	}
	live := m.cfg.Live
	if m.actionFlights == nil {
		m.actionFlights = make(map[string][]runAction)
	}
	m.actionFlights[a.flightKey()] = append(m.actionFlights[a.flightKey()], a)
	m.actionStatus = a.pendingText()
	if a.verb == "fix" && m.pane != nil {
		m.pane.fixing, m.pane.input = false, textField{}
	}
	if a.verb == "answer" && a.finding == 0 && m.pane != nil {
		// The call blocks until the run rests, so follow the snapshots meanwhile.
		m.pane.answered, m.pane.answerDone = true, false
		m.pane.answerState, m.pane.answerNotice = "", ""
		m.pane.answerStatus = m.actionStatus
		m.pane.answerWait, m.pane.answerKey = m.paneRunQuestion()
		m.refreshAnswerStatus()
	}
	return func() tea.Msg {
		var err error
		var message string
		switch a.verb {
		case "decide":
			if answerer, ok := live.(adjAnswerer); ok {
				message, err = answerer.AdjAnswer(context.Background(), daemon.AdjAnswerParams{Project: a.project, ID: a.adjID, Option: a.option, Text: a.text, By: a.by, Via: "tui"})
			} else {
				err = errors.New("this daemon connection cannot answer in adj")
			}
		case "cleanup":
			err = live.Cleanup(context.Background(), a.run)
		case "approve":
			if ap, ok := live.(approver); ok {
				var rec runrec.Record
				rec, err = ap.ApproveRecord(context.Background(), a.run)
				a.outcome = approveOutcome(rec)
			} else {
				err = live.Approve(context.Background(), a.run)
			}
		case "end":
			err = endLedger(live, a)
		case "stop":
			err = live.Stop(context.Background(), a.run)
		case "retry":
			err = live.Retry(context.Background(), a.run)
		case "fix", "check":
			actor, ok := live.(cardActor)
			switch {
			case !ok:
				err = errors.New("this daemon connection cannot act on the approval card")
			case a.verb == "fix":
				err = actor.Fix(context.Background(), a.run, a.text)
			default:
				err = actor.MarkCheck(context.Background(), a.run, a.check, a.state)
			}
		case "file":
			if filer, ok := live.(laterFiler); ok {
				message, err = filer.FileLater(context.Background(), a.run, a.finding)
			} else {
				err = errors.New("this daemon connection cannot file slices")
			}
		case "answer":
			message, err = live.AnswerAction(context.Background(), daemon.RunAction{Run: a.run, Text: a.text, Adj: a.adj, By: a.by, Finding: a.finding})
		}
		return actionMsg{action: a, err: err, message: message}
	}
}

// isRefused reports whether the daemon refused the action with its own reason.
func isRefused(err error) bool {
	var wire *ipc.Error
	return errors.As(err, &wire) && wire.Code == daemon.CodeRefused
}

func actionError(err error) string {
	var wire *ipc.Error
	if errors.As(err, &wire) {
		if wire.Code == daemon.CodeRefused {
			return wire.Message // the refusal is the text; the status line names the verb
		}
		// Some messages lead with the code in words ("bad params: ..."): say it once.
		return wire.Code + ": " + strings.TrimPrefix(wire.Message, strings.ReplaceAll(wire.Code, "_", " ")+": ")
	}
	return err.Error()
}

// registeredProjectCount excludes display-only removed-project groups.
func (m *Model) registeredProjectCount() int {
	count := 0
	for _, p := range m.projects {
		if !p.snap.Unavailable {
			count++
		}
	}
	return count
}
