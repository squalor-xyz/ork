// SPDX-License-Identifier: GPL-3.0-or-later

// Package tui is ork's terminal UI: project tabs, the Overview and the status
// line. It reads state only through source.Source and never writes: no key
// here runs a slicer, adj or git write command.
package tui

import (
	"context"
	"fmt"
	"os"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

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
}

func (p *projectState) needs() int { return p.open + p.held }

// Model is the Bubble Tea model of the whole TUI.
type Model struct {
	cfg         Config
	keys        keyMap
	help        help.Model
	projects    []*projectState
	byName      map[string]*projectState
	poller      *poll.Poller
	active      int
	showHelp    bool
	width       int
	height      int
	lastRefresh time.Time
	listening   bool // the daemon's notice feed is being listened to (S13)
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
	m := &Model{cfg: cfg, keys: newKeyMap(), byName: map[string]*projectState{}}
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
	if len(names) > 0 {
		m.poller.SetVisible(names[0])
	}
	return m
}

// Init fetches every project, starts the schedule and tells the daemon which
// project is in view.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.tick(m.cfg.Now()), m.focus())
}

// focus tells the daemon which project the owner sees, so it reads that one
// every 5 s. It returns nil without a daemon.
func (m *Model) focus() tea.Cmd {
	live := m.cfg.Live
	if live == nil || len(m.projects) == 0 {
		return nil
	}
	name := m.projects[m.active].reg.Name
	return func() tea.Msg {
		return focusMsg{err: live.Focus(context.Background(), []string{name})}
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
		return m, tea.Batch(m.fetch(msg.project, false), m.listen())
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
	case key.Matches(msg, m.keys.Close):
		m.showHelp = false
	case key.Matches(msg, m.keys.Next):
		return m.switchTo(m.active + 1)
	case key.Matches(msg, m.keys.Prev):
		return m.switchTo(m.active - 1)
	case key.Matches(msg, m.keys.Jump):
		if n := int(msg.String()[0] - '1'); n < len(m.projects) {
			return m.switchTo(n)
		}
	case key.Matches(msg, m.keys.Refresh):
		if len(m.projects) > 0 {
			return m.fetch(m.projects[m.active].reg.Name, true)
		}
	}
	return nil
}

// switchTo shows project i (wrapping around), makes it the fast-polled one,
// and returns the command that tells the daemon which project that is.
func (m *Model) switchTo(i int) tea.Cmd {
	n := len(m.projects)
	if n == 0 {
		return nil
	}
	m.active = (i%n + n) % n
	m.poller.SetVisible(m.projects[m.active].reg.Name)
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
	ps.loaded = true
	ps.snap = msg.snap
	if msg.snap.Runs.Err == nil {
		ps.running = len(msg.snap.Runs.Value) > 0
	}
	if rose {
		bell := m.cfg.Bell
		return func() tea.Msg { bell(); return nil }
	}
	return nil
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
