// SPDX-License-Identifier: GPL-3.0-or-later

// Package tui is ork's terminal UI: project sidebar, the Overview and the status
// line. It reads state through source.Source and sends owner actions through Live.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/poll"
	"github.com/squalor-xyz/ork/internal/registry"
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
	for _, r := range p.snap.EngineRuns {
		if r.Section(p.snap.EngineRuns) == source.SectionNeeds {
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

// Model is the Bubble Tea model of the whole TUI.
type Model struct {
	welcome  welcomeState
	cfg      Config
	keys     keyMap
	help     help.Model
	projects []*projectState
	byName   map[string]*projectState
	poller   *poll.Poller
	active   int // -1 is Needs you; otherwise registry index
	view     int // the view in the bar: an index into views; 0 is the Overview

	questionsCursor int
	allProjects     bool
	decisionsTab    int
	decisionsCursor int
	decisionPane    *decisionRow
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
	actionStatus  string
	lastRefresh   time.Time
	listening     bool // the daemon's notice feed is being listened to (S13)
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
	m := &Model{active: -1, cfg: cfg, keys: newKeyMap(), byName: map[string]*projectState{}}
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
			m.actionStatus = "Read unavailable runs: " + msg.err.Error()
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
		return m, m.apply(msg)
	case projectAddedMsg:
		return m, m.applyProjectAdded(msg)
	case itemShowMsg:
		if pane := m.itemPane; pane != nil && m.view == roadmapView && m.active >= 0 &&
			pane.project == msg.project && pane.id == msg.id && pane.request == msg.request &&
			m.projects[m.active].reg.Name == msg.project {
			pane.pending = false
			pane.detail, pane.err = msg.detail, msg.err
		}
	case wavesMsg:
		return m, m.applyWaves(msg)
	case launchMsg:
		return m, m.applyLaunch(msg)
	case actionMsg:
		flights := m.actionFlights[msg.action.run]
		for i, a := range flights {
			if a.verb == msg.action.verb {
				m.actionFlights[msg.action.run] = append(flights[:i], flights[i+1:]...)
				break
			}
		}
		if len(m.actionFlights[msg.action.run]) == 0 {
			delete(m.actionFlights, msg.action.run)
		}
		if r, ok := m.actionRow(); ok && r.engineID == msg.action.run && r.name == msg.action.project {
			switch {
			case msg.err != nil:
				m.actionStatus = msg.action.command() + ": " + actionError(msg.err)
				if msg.action.verb == "answer" && m.pane != nil {
					m.pane.answered = false
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
				// A tracking that the run's next state already dropped stays dropped.
				m.pane.answerDone, m.pane.answerNotice = true, msg.message
				m.refreshAnswerStatus()
			default:
				m.actionStatus = msg.action.command() + ": done"
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
		if m.registeredProjectCount() == 0 {
			if m.welcome.editing && m.welcome.confirm == "" && !m.welcome.pending {
				m.welcome.field.update(msg)
				m.actionStatus = ""
			}
			return m, nil
		}
		if m.waves != nil && m.waves.lineup != nil {
			e := m.waves.lineup
			if e.custom {
				e.fields[e.focus].update(msg)
			} else if e.row >= 2 {
				e.budgets[e.row-2].update(msg)
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
	if key.Matches(msg, m.keys.RowNext, m.keys.RowPrev, m.keys.Next, m.keys.Prev, m.keys.NextView, m.keys.PrevView, m.keys.Jump, m.keys.Filter, m.keys.Select, m.keys.Close) {
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

	if m.waves != nil && m.waves.lineup != nil {
		return m.lineupKey(msg)
	}
	if m.waves != nil && m.waves.inputs != nil {
		return m.waveInputKey(msg)
	}
	if m.view == wavesView && m.waves != nil && m.waves.pane != nil {
		return m.waveItemKey(msg)
	}
	if m.launch != nil {
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
	case m.view == wavesView && !m.showHelp && msg.String() == "a":
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
	case m.view == questionsView && key.Matches(msg, m.keys.RowNext, m.keys.RowPrev, m.keys.PaneScroll):
		delta := 1
		if key.Matches(msg, m.keys.RowPrev) || msg.String() == "pgup" {
			delta = -1
		}
		if m.decisionPane != nil {
			m.scrollDecision(delta, key.Matches(msg, m.keys.PaneScroll))
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
	case m.view == decisionsView && key.Matches(msg, m.keys.Next, m.keys.Prev):
		m.decisionsTab = 1 - m.decisionsTab
		m.decisionsCursor = 0
		m.decisionPane = nil
	case m.view == decisionsView && key.Matches(msg, m.keys.RowNext, m.keys.RowPrev, m.keys.PaneScroll):
		delta := 1
		if key.Matches(msg, m.keys.RowPrev) || msg.String() == "pgup" {
			delta = -1
		}
		if m.decisionPane != nil {
			m.scrollDecision(delta, key.Matches(msg, m.keys.PaneScroll))
		} else {
			n := len(m.decisionRecords())
			m.decisionsCursor = clampCursor(clampCursor(m.decisionsCursor, n)+delta, n)
		}
	case m.view == decisionsView && key.Matches(msg, m.keys.Select):
		m.openDecision()
	case key.Matches(msg, m.keys.EditLineup):
		return m.openLineup()
	case key.Matches(msg, m.keys.Tick):
		return m.tickWave()
	case key.Matches(msg, m.keys.Launch):
		if m.view == wavesView {
			return m.confirmWave()
		}
		return m.openLaunch()
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
			if r.ready {
				return nil
			}
			if (r.engineID != "" || r.ledgerID != "") && !m.showHelp {
				return m.openPane(r)
			}
			return m.switchTo(r.project)
		}
	case key.Matches(msg, m.keys.Jump):
		if n := int(msg.String()[0] - '1'); n < len(m.projects) {
			return m.switchTo(n)
		}
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

// switchTo selects registry project i, or Needs you at -1, and updates polling.
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
	if ps == nil {
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
				if !linked[r.ID] {
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
	}
	m.selectJumpRun()
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

// navigation lists registry indices in stable priority order, with Needs you first.
func (m *Model) navigation() []int {
	out := []int{-1}
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
	verb, run, project, text string
	item                     string // file: the slice the finding came from
	finding                  int    // file: the one-based finding
	adj                      bool
	by                       string
	cleanupCommand           string
	items                    []daemon.RunParams
	plan                     *daemon.RunPlan
	wellSpecified, split     bool
}

func (a runAction) command() string {
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
	if a.verb == "file" {
		return "slicer add " + shellQuote(a.text) + " --discovered-from " + a.item
	}
	cmd := "ork " + a.verb + " " + a.run
	if a.verb == "answer" {
		cmd += " " + shellQuote(a.text)
	}
	return cmd
}

// prompt keeps the cleanup question separate from the shell-quoted commands.
func (a runAction) prompt() string {
	if a.verb == "cleanup" {
		return a.command() + "\nRemove worktree and branch? [y/N] (Enter is No)"
	}
	return a.command() + "? [y/N] (Enter is No)"
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
	return append(lines, "Remove worktree and branch? [y/N] (Enter is No)")
}

type actionMsg struct {
	message string
	action  runAction
	err     error
}

func (m *Model) requestAction(verb string) tea.Cmd {
	if m.showHelp || m.view == roadmapView || m.view == wavesView || m.view == decisionsView || (m.view == questionsView && m.pane == nil) {
		return nil
	}
	r, ok := m.actionRow()
	if !ok || r.engineID == "" {
		m.actionStatus = "No engine run on this row; no actions"
		return nil
	}
	if r.unavailable {
		m.actionStatus = source.RemovedProjectHelp(r.name)
		return nil
	}
	if r.ready && verb != "cleanup" {
		m.actionStatus = "Ready rows have no run actions; c cleans up a merged row"
		return nil
	}
	if r.ready && verb == "cleanup" {
		switch {
		case r.branch == "":
			m.actionStatus = "cleanup needs a merged branch; branch unknown"
			return nil
		case !r.mergeKnown:
			m.actionStatus = "cleanup needs a merged branch; merge state unknown"
			return nil
		case !r.merged:
			m.actionStatus = "cleanup needs a merged branch; this one is unmerged"
			return nil
		case r.worktree == "":
			m.actionStatus = "cleanup needs a worktree; worktree unknown"
			return nil
		}
	}
	if verb == "cleanup" && (!r.ready || !r.merged || r.branch == "" || r.worktree == "") {
		return nil
	}
	for _, a := range m.actionFlights[r.engineID] {
		// Stop can interrupt an approve or retry, but a second stop waits.
		if verb != "stop" || a.verb == "stop" {
			m.actionStatus = a.command() + " is pending"
			return nil
		}
	}
	if verb == "stop" && r.state == "stopped" {
		m.actionStatus = "run is already stopped; clear with: ork dismiss " + r.engineID
		return nil
	}
	if verb == "approve" && r.state != "awaiting-approval" {
		m.actionStatus = "approve needs awaiting-approval; run is " + r.state
		return nil
	}
	if verb == "retry" {
		if r.state != "paused" {
			m.actionStatus = "retry needs paused; run is " + r.state
			return nil
		}
		if r.pending == "question" {
			m.actionStatus = "answer with: ork answer " + r.engineID + " TEXT"
			return nil
		}
	}
	if verb == "answer" {
		if r.state != "paused" || r.pending != "question" {
			return nil
		}
		if strings.TrimSpace(m.pane.input.Value()) == "" {
			m.actionStatus = "Answer must not be empty"
			return nil
		}
	}
	if m.cfg.Live == nil {
		m.actionStatus = "No daemon connection; action unavailable"
		return nil
	}
	a := runAction{verb: verb, run: r.engineID, project: r.name}
	if verb == "cleanup" {
		path := shellQuote(m.projects[r.project].reg.Path)
		a.cleanupCommand = "git -C " + path + " worktree remove " + shellQuote(r.worktree) + "\ngit -C " + path + " branch -d " + shellQuote(r.branch)
	}
	if verb == "answer" {
		a.text = m.pane.input.Value()
		_, reason := m.paneAdjKey()
		a.adj = m.pane.adj && reason == ""
		if a.adj {
			a.by = os.Getenv("ADJ_DECIDED_BY")
		}
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
	m.actionFlights[a.run] = append(m.actionFlights[a.run], a)
	m.actionStatus = a.command() + ": pending"
	if a.verb == "answer" && m.pane != nil {
		// The call blocks until the run rests, so follow the snapshots meanwhile.
		m.pane.answered, m.pane.answerDone = true, false
		m.pane.answerState, m.pane.answerNotice = "", ""
		m.refreshAnswerStatus()
	}
	return func() tea.Msg {
		var err error
		var message string
		switch a.verb {
		case "cleanup":
			err = live.Cleanup(context.Background(), a.run)
		case "approve":
			err = live.Approve(context.Background(), a.run)
		case "stop":
			err = live.Stop(context.Background(), a.run)
		case "retry":
			err = live.Retry(context.Background(), a.run)
		case "file":
			if filer, ok := live.(laterFiler); ok {
				message, err = filer.FileLater(context.Background(), a.run, a.finding)
			} else {
				err = errors.New("this daemon connection cannot file slices")
			}
		case "answer":
			message, err = live.AnswerAction(context.Background(), daemon.RunAction{Run: a.run, Text: a.text, Adj: a.adj, By: a.by})
		}
		return actionMsg{action: a, err: err, message: message}
	}
}

func actionError(err error) string {
	var wire *ipc.Error
	if errors.As(err, &wire) {
		return wire.Code + ": " + wire.Message
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
