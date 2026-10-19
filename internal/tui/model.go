// SPDX-License-Identifier: GPL-3.0-or-later

// Package tui is ork's terminal UI: project sidebar, the Overview and the status
// line. It reads state only through source.Source and never writes: no key
// here runs a slicer, adj or git write command.
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

func (p *projectState) needs() int { return p.open + p.held }

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
	cursor        int // the Overview row under the cursor; drawn clamped to the rows there are
	sidebarHidden bool
	filtering     bool
	filter        string
	focusing      bool
	showHelp      bool
	width         int
	height        int
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
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
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
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
	case key.Matches(msg, m.keys.Sidebar):
		m.sidebarHidden = !m.sidebarHidden
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
	case key.Matches(msg, m.keys.Close):
		m.showHelp = false
		m.filter = ""
	case key.Matches(msg, m.keys.Next):
		return m.move(1)
	case key.Matches(msg, m.keys.Prev):
		return m.move(-1)
	case key.Matches(msg, m.keys.RowNext):
		m.moveRow(1)
	case key.Matches(msg, m.keys.RowPrev):
		m.moveRow(-1)
	case key.Matches(msg, m.keys.Select):
		if r, ok := m.selectedRow(); ok {
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
	m.active = i
	m.cursor = 0
	if i >= 0 {
		m.poller.SetVisible(m.projects[i].reg.Name)
	} else {
		m.poller.SetVisible("")
	}
	return m.focus()
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
		if msg.snap.Runs.Err == nil {
			ps.running = len(msg.snap.Runs.Value) > 0
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
