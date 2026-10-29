// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

type launchStage int

const (
	launchList launchStage = iota
	launchWell
	launchSplit
	launchPlanPending
	launchConfirm
	launchEdit
	launchStarting
)

type launchState struct {
	lineup       *lineupEditor
	prev         *daemon.RunPlan
	prevParams   daemon.RunParams
	replanCancel context.CancelFunc
	stage        launchStage
	cursor       int
	input        textField
	params       daemon.RunParams
	plan         *daemon.RunPlan
	request      uint64
	ctx          context.Context
	cancel       context.CancelFunc
}

type launchMsg struct {
	request  uint64
	action   runAction
	plan     *daemon.RunPlan
	starting bool
	err      error
}

type launchFlight struct {
	action  runAction
	request uint64
}

func (m *Model) launchPrompt() bool {
	return m.launch != nil && (m.launch.stage == launchWell || m.launch.stage == launchSplit)
}

func (m *Model) dropLaunch() {
	if m.launch != nil {
		m.launch.cancel()
		m.launch = nil
	}
	if m.confirm != nil && m.confirm.verb == "launch" {
		m.confirm = nil
	}
}

// launchRefusal is why l does not open the launch flow on the Roadmap: the
// status line text, or "" for a silent refusal. It reports refused false when l
// opens the flow. The key refuses by it and the hint line lists l only when it
// does not.
func (m *Model) launchRefusal() (status string, refused bool) {
	if m.view != roadmapView || m.active < 0 || m.itemPane != nil || m.showHelp || m.projects[m.active].snap.Unavailable {
		return "", true
	}
	if m.cfg.Live == nil {
		return "No daemon connection; action unavailable", true
	}
	if flight, ok := m.launchFlights[m.projects[m.active].reg.Name]; ok {
		return flight.action.command() + " is pending", true
	}
	return "", false
}

func (m *Model) openLaunch() tea.Cmd {
	if status, refused := m.launchRefusal(); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	project := m.projects[m.active].reg.Name
	m.launchRequest++
	ctx, cancel := context.WithCancel(context.Background())
	l := &launchState{params: daemon.RunParams{Project: project}, request: m.launchRequest, ctx: ctx, cancel: cancel}
	m.launch = l
	m.actionStatus = ""
	return nil
}

func (m *Model) readyLaunchItems() []slicercli.Item {
	if m.active < 0 {
		return nil
	}
	s := m.projects[m.active].snap
	var items []slicercli.Item
	for _, it := range m.roadmapItems() {
		if readySlice(s, it) {
			items = append(items, it)
		}
	}
	return items
}

func (m *Model) launchKey(msg tea.KeyPressMsg) tea.Cmd {
	l := m.launch
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		if l.stage == launchPlanPending && l.prev != nil {
			l.replanCancel()
			l.replanCancel = nil
			l.plan, l.params = l.prev, l.prevParams
			l.prev = nil
			m.actionStatus = ""
			m.confirmLaunch()
			return nil
		}
		if l.stage != launchStarting {
			m.actionStatus = "Cancelled"
		}
		m.dropLaunch()
		return nil
	}
	if m.launchPrompt() {
		if msg.String() != "enter" {
			l.input.update(msg)
			m.actionStatus = ""
			return nil
		}
		yes, valid := launchAnswerValue(l.input.Value())
		if !valid {
			label := "Well specified?"
			if l.stage == launchSplit {
				label = "Split?"
			}
			m.actionStatus = label + " requires y or n"
			return nil
		}
		l.input = textField{}
		m.actionStatus = ""
		if l.stage == launchWell {
			l.params.WellSpecified = yes
			l.stage = launchSplit
			return nil
		}
		l.params.Split = yes
		l.stage = launchPlanPending
		live, params, request, ctx := m.cfg.Live, l.params, l.request, l.ctx
		return func() tea.Msg {
			if ctx.Err() != nil {
				return nil
			}
			plan, err := live.Plan(ctx, params)
			return launchMsg{request: request, plan: plan, err: err}
		}
	}
	items := m.readyLaunchItems()
	l.cursor = clampCursor(l.cursor, len(items))
	// Project and view navigation drop the launch, including a pending plan.
	switch {
	case key.Matches(msg, m.keys.Next):
		return m.move(1)
	case key.Matches(msg, m.keys.Prev):
		return m.move(-1)
	case key.Matches(msg, m.keys.NextView):
		return m.moveView(1)
	case key.Matches(msg, m.keys.PrevView):
		return m.moveView(-1)
	case key.Matches(msg, m.keys.Jump):
		return m.jump(int(msg.String()[0] - '0'))
	case key.Matches(msg, m.keys.RowNext):
		if l.stage == launchList {
			l.cursor = clampCursor(l.cursor+1, len(items))
		}
	case key.Matches(msg, m.keys.RowPrev):
		if l.stage == launchList {
			l.cursor = clampCursor(l.cursor-1, len(items))
		}
	case key.Matches(msg, m.keys.Select):
		if l.stage == launchList && len(items) > 0 {
			l.params.Item = items[l.cursor].ID
			l.stage = launchWell
		}
	}
	return nil
}

func (m *Model) startLaunch(a runAction) tea.Cmd {
	l := m.launch
	if l == nil || l.stage != launchConfirm || l.plan == nil {
		return nil
	}
	if _, ok := m.launchFlights[a.project]; ok {
		return nil
	}
	if m.launchFlights == nil {
		m.launchFlights = map[string]launchFlight{}
	}
	m.launchFlights[a.project] = launchFlight{action: a, request: l.request}
	l.stage = launchStarting
	m.actionStatus = a.command() + ": pending"
	params := daemon.RunParams{Project: a.project, Item: a.run, WellSpecified: a.wellSpecified, Split: a.split, Expected: a.plan, StopAfter: a.stopAfter, Lineup: a.lineup, Budgets: a.budgets}
	live, request, ctx := m.cfg.Live, l.request, l.ctx
	return func() tea.Msg {
		if ctx.Err() != nil {
			return launchMsg{request: request, action: a, starting: true, err: ctx.Err()}
		}
		_, err := live.Start(context.Background(), params)
		return launchMsg{request: request, action: a, starting: true, err: err}
	}
}

func (m *Model) applyLaunch(msg launchMsg) tea.Cmd {
	if msg.starting {
		flight, ok := m.launchFlights[msg.action.project]
		if !ok || flight.request != msg.request {
			return nil
		}
		delete(m.launchFlights, msg.action.project)
		if m.launch != nil && m.launch.request == msg.request {
			m.dropLaunch()
		}
		if msg.err != nil {
			m.actionStatus = msg.action.command() + ": " + actionError(msg.err)
			return nil
		}
		m.actionStatus = msg.action.command() + ": done"
		if m.poller.InFlight(msg.action.project) {
			m.byName[msg.action.project].dirty = true
		}
		return m.fetch(msg.action.project, true)
	}
	l := m.launch
	if l == nil || l.request != msg.request || m.view != roadmapView || m.active < 0 || m.projects[m.active].reg.Name != l.params.Project {
		return nil
	}
	if l.stage != launchPlanPending {
		return nil
	}
	if l.replanCancel != nil {
		l.replanCancel()
		l.replanCancel = nil
	}
	if msg.err != nil {
		if l.prev != nil {
			l.plan, l.params = l.prev, l.prevParams
			l.prev = nil
			m.confirmLaunch()
		} else {
			m.dropLaunch()
		}
		m.actionStatus = actionError(msg.err)
		return nil
	}
	if msg.plan == nil {
		m.dropLaunch()
		m.actionStatus = "No run plan returned"
		return nil
	}
	l.plan = msg.plan
	l.prev = nil
	m.actionStatus = ""
	m.confirmLaunch()
	return nil
}

func (m *Model) confirmLaunch() {
	l := m.launch
	l.stage = launchConfirm
	m.confirm = &runAction{verb: "launch", project: l.params.Project, run: l.params.Item, plan: l.plan, wellSpecified: l.params.WellSpecified, split: l.params.Split, stopAfter: l.params.StopAfter, lineup: l.params.Lineup, budgets: l.params.Budgets}
}

func (m *Model) openLaunchLineup() tea.Cmd {
	l := m.launch
	if l == nil || l.stage != launchConfirm || l.plan == nil {
		return nil
	}
	m.confirm = nil
	l.lineup = newLineupEditor(l.params.Item, l.plan)
	l.stage = launchEdit
	m.actionStatus = ""
	return nil
}

func (m *Model) launchLineupKey(msg tea.KeyPressMsg) tea.Cmd {
	l := m.launch
	m.actionStatus = ""
	switch editLineup(l.lineup, msg) {
	case lineupQuit:
		return tea.Quit
	case lineupDiscard:
		l.lineup = nil
		m.confirmLaunch()
	case lineupSave:
		p, changed, ok := lineupOverrides(l.lineup, l.params)
		if !ok {
			m.actionStatus = "Budget must be a whole number of zero or more"
			return nil
		}
		l.lineup = nil
		if !changed {
			m.confirmLaunch()
			return nil
		}
		l.prev, l.prevParams = l.plan, l.params
		l.params, l.stage = p, launchPlanPending
		// Each replan gets a new request so cancelled replies cannot replace a newer one.
		m.launchRequest++
		l.request = m.launchRequest
		ctx, cancel := context.WithCancel(l.ctx)
		l.replanCancel = cancel
		live, request := m.cfg.Live, l.request
		return func() tea.Msg {
			if ctx.Err() != nil {
				return nil
			}
			plan, err := live.Plan(ctx, p)
			return launchMsg{request: request, plan: plan, err: err}
		}
	}
	return nil
}

func (m *Model) launchView(w int) overviewView {
	out := overviewView{sel: -1}
	th, l := m.cfg.Theme, m.launch
	if l.stage == launchEdit {
		title := ""
		for _, item := range m.readyLaunchItems() {
			if item.ID == l.params.Item {
				title = item.Title
				break
			}
		}
		return m.editorBody(l.lineup, title, w)
	}
	add := func(text string) {
		for _, line := range wrapText(text, max(w-2, 1)) {
			out.body = append(out.body, indent+line)
		}
	}
	out.body = append(out.body, th.Heading.Render("Ready slices"))
	switch l.stage {
	case launchList:
		items := m.readyLaunchItems()
		cursor := clampCursor(l.cursor, len(items))
		if len(items) == 0 {
			add("No ready slices")
		}
		for i, it := range items {
			title := it.ShortTitle
			if title == "" {
				title = it.Title
			}
			size, effort := it.Fields.Size, "-"
			if size == "" {
				size = "-"
			}
			if it.Fields.Effort != 0 {
				effort = fmt.Sprint(it.Fields.Effort)
			}
			prefix := it.ID + "  "
			fields := fmt.Sprintf("  %s  effort %s", size, effort)
			line := indent + prefix + clip(title, max(w-2-lipgloss.Width(prefix)-lipgloss.Width(fields), 1)) + fields
			if i == cursor {
				out.sel = len(out.body)
				line = th.TabActive.Render(line)
			}
			out.body = append(out.body, line)
		}
	case launchWell, launchSplit:
		label := "Well specified? [y/n]:"
		if l.stage == launchSplit {
			label = "Split? [y/n]:"
		}
		add(label)
		out.body = append(out.body, indent+l.input.view(max(w-2, 1), th))
	case launchPlanPending:
		add("pending: run.plan")
	case launchConfirm, launchStarting:
		p := l.plan
		add(p.Project.Name + " " + p.Item)
		add("worktree: " + p.Worktree)
		add("branch: " + p.Branch)
		add("check: " + p.CheckCommand)
		stop := "after landing"
		if p.StopAfter == runrec.StopAfterCommit {
			stop = "after commit (you land it)"
		}
		add("stop: " + stop)
		if l.params.Budgets != nil {
			add(fmt.Sprintf("budgets: %d min, %d tokens", p.Budgets.Minutes, p.Budgets.Tokens))
		}
		for _, role := range []string{"implementer", "reviewer", "ui", "architect", "gate"} {
			if (role == "architect" || role == "ui") && len(p.Lineup[role]) == 0 {
				continue
			}
			for _, e := range p.Lineup[role] {
				add(fmt.Sprintf("%s: %s %s %s", role, e.Vendor, e.Model, e.Effort))
			}
			perm := p.Permissions[role]
			if perm.ReadOnly {
				add("  permissions: read-only")
			} else {
				add("  allowlist: " + strings.Join(perm.Allow, ", "))
			}
		}
		if p.Note != "" {
			for _, line := range wrapText("note: "+p.Note, max(w-2, 1)) {
				out.body = append(out.body, indent+th.Warn.Render(line))
			}
		}
	}
	return out
}

func launchAnswerValue(text string) (yes, valid bool) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "y", "yes":
		return true, true
	case "n", "no":
		return false, true
	}
	return false, false
}
