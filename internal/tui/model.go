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
	Source    source.Source
	Projects  []registry.Project
	Now       func() time.Time              // default time.Now
	Schedule  poll.Schedule                 // default poll.DefaultSchedule
	TickEvery time.Duration                 // how often the schedule is checked; default 1 s
	After     func(d time.Duration) tea.Cmd // a command that sends tickMsg after d; default tea.Tick
	Bell      func()                        // rings the terminal bell; default writes BEL to stderr
	Theme     Theme
	Live      Live // the daemon's refresh, focus and notices; nil when there is no daemon
}

// tickMsg asks the Model to check which projects are due.
type tickMsg struct{}

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
	n := p.open + p.held
	for _, r := range p.snap.EngineRuns {
		if r.NeedsYou(p.snap.EngineRuns) {
			n++
		}
	}
	return n
}

// setup reports whether the project is loaded and has no .slicer/ folder.
func (p *projectState) setup() bool { return p.loaded && isSetup(p.snap) }

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
	cfg           Config
	keys          keyMap
	help          help.Model
	projects      []*projectState
	byName        map[string]*projectState
	poller        *poll.Poller
	active        int // -1 is Needs you; otherwise registry index
	view          int // the view in the bar: an index into views; 0 is the Overview
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
	pane          *runPane
	confirm       *runAction
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
	return m.tick(m.cfg.Now())
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
		names = append(names, m.projects[m.active].reg.Name)
	} else {
		for _, p := range m.projects {
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
	if ps == nil || !m.poller.Start(name) {
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
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		return m, m.tick(m.cfg.Now())
	case snapshotMsg:
		return m, m.apply(msg)
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
			if msg.err != nil {
				m.actionStatus = msg.action.command() + ": " + actionError(msg.err)
			} else {
				m.actionStatus = msg.action.command() + ": done"
				if msg.action.verb == "answer" && m.pane != nil {
					m.pane.input = textField{}
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
		if m.pane != nil && m.paneQuestion() {
			m.pane.input.update(msg)
			m.actionStatus = ""
		}
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
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

	if m.waves != nil && m.waves.inputs != nil {
		return m.waveInputKey(msg)
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
	case key.Matches(msg, m.keys.Tick):
		return m.tickWave()
	case key.Matches(msg, m.keys.Launch):
		if m.view == wavesView {
			return m.confirmWave()
		}
		return m.openLaunch()
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
			return nil
		}
		if m.view == roadmapView {
			return m.openItem()
		}
		if r, ok := m.selectedRow(); ok {
			if r.engineID != "" && !m.showHelp {
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
			if r.State != "committed" && !r.Superseded(msg.snap.EngineRuns) && !r.NeedsYou(msg.snap.EngineRuns) {
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
			m.itemPane = nil
			return m.openWaves()
		}
	}
	return nil
}

// runAction retains the exact target while a confirmation is on screen.
type runAction struct {
	verb, run, project, text string
	items                    []daemon.RunParams
	plan                     *daemon.RunPlan
	wellSpecified, split     bool
}

func (a runAction) command() string {
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
	cmd := "ork " + a.verb + " " + a.run
	if a.verb == "answer" {
		cmd += " " + a.text
	}
	return cmd
}

type actionMsg struct {
	action runAction
	err    error
}

func (m *Model) requestAction(verb string) tea.Cmd {
	if m.showHelp || m.view == roadmapView || m.view == wavesView {
		return nil
	}
	r, ok := m.actionRow()
	if !ok || r.engineID == "" {
		m.actionStatus = "No engine run on this row; no actions"
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
		m.actionStatus = "run is already stopped"
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
	if verb == "answer" {
		a.text = m.pane.input.Value()
	}
	if verb == "approve" || verb == "answer" {
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
	return func() tea.Msg {
		var err error
		switch a.verb {
		case "approve":
			err = live.Approve(context.Background(), a.run)
		case "stop":
			err = live.Stop(context.Background(), a.run)
		case "retry":
			err = live.Retry(context.Background(), a.run)
		case "answer":
			err = live.Answer(context.Background(), a.run, a.text)
		}
		return actionMsg{action: a, err: err}
	}
}

func actionError(err error) string {
	var wire *ipc.Error
	if errors.As(err, &wire) {
		return wire.Code + ": " + wire.Message
	}
	return err.Error()
}
