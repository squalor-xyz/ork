// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

const wavesView = 2

type waveInputs struct {
	item   string
	fields [2]textField
	focus  int
}

type wavesState struct {
	engineItems    map[string]bool
	project        string
	request        uint64
	ctx            context.Context
	cancel         context.CancelFunc
	pending        bool
	suggest        *slicercli.ParallelPlan
	err            error
	cursor         int
	picked         map[string]daemon.RunParams
	lineup         *lineupEditor
	inputs         *waveInputs
	planning       string
	planRequest    uint64
	overlap        *slicercli.Overlap
	overlapErr     error
	overlapRequest uint64
	starting       bool
}

type wavesMsg struct {
	engineItem *bool
	request    uint64
	kind       string
	sequence   uint64
	item       string
	suggest    *slicercli.ParallelPlan
	plan       *daemon.RunPlan
	params     *daemon.RunParams
	overlap    *slicercli.Overlap
	result     *daemon.WaveResult
	action     runAction
	err        error
}

func (m *Model) dropWaves() {
	if m.waves != nil {
		m.waves.cancel()
		m.waves = nil
	}
}

func (m *Model) openWaves() tea.Cmd {
	m.dropWaves()
	if m.view != wavesView || m.active < 0 || m.projects[m.active].snap.Unavailable {
		return nil
	}
	m.wavesRequest++
	ctx, cancel := context.WithCancel(context.Background())
	w := &wavesState{project: m.projects[m.active].reg.Name, request: m.wavesRequest, ctx: ctx, cancel: cancel, pending: true, picked: map[string]daemon.RunParams{}}
	m.waves = w
	if m.cfg.Live == nil {
		w.pending = false
		return nil
	}
	live := m.cfg.Live
	return func() tea.Msg {
		if ctx.Err() != nil {
			return nil
		}
		plan, err := live.WaveSuggest(ctx, w.project)
		return wavesMsg{request: w.request, kind: "suggest", suggest: plan, err: err}
	}
}

// rows keeps slicer's wave and item order. Unplaced items can also be chosen.
func (w *wavesState) rows() []slicercli.ParallelItem {
	if w.suggest == nil {
		return nil
	}
	var rows []slicercli.ParallelItem
	for _, wave := range w.suggest.Waves {
		rows = append(rows, wave.Items...)
	}
	return append(rows, w.suggest.Unplaced...)
}

func (w *wavesState) items() []daemon.RunParams {
	var items []daemon.RunParams
	for _, row := range w.rows() {
		if p, ok := w.picked[row.ID]; ok {
			items = append(items, p)
		}
	}
	return items
}

func (m *Model) waveOverlap() tea.Cmd {
	w := m.waves
	w.overlapRequest++
	w.overlap, w.overlapErr = nil, nil
	var ids []string
	for _, p := range w.items() {
		ids = append(ids, p.Item)
	}
	if len(ids) < 2 {
		return nil
	}
	live, ctx, request, seq, project := m.cfg.Live, w.ctx, w.request, w.overlapRequest, w.project
	return func() tea.Msg {
		if ctx.Err() != nil {
			return nil
		}
		overlap, err := live.WaveOverlap(ctx, project, ids)
		return wavesMsg{request: request, kind: "overlap", sequence: seq, overlap: overlap, err: err}
	}
}

func (m *Model) tickWave() tea.Cmd {
	w := m.waves
	if m.view != wavesView || m.showHelp || w == nil || w.pending || w.err != nil || w.starting {
		return nil
	}
	if m.cfg.Live == nil {
		m.actionStatus = "No daemon connection; action unavailable"
		return nil
	}
	rows := w.rows()
	if len(rows) == 0 {
		return nil
	}
	w.cursor = clampCursor(w.cursor, len(rows))
	id := rows[w.cursor].ID
	if _, ok := w.picked[id]; ok {
		delete(w.picked, id)
		if w.planning == id {
			w.planRequest++
			w.planning = ""
		}
	} else {
		if w.planning != "" {
			m.actionStatus = "A plan is pending; wait before ticking another item"
			return nil
		}
		w.picked[id] = daemon.RunParams{Project: w.project, Item: id}
		w.inputs = &waveInputs{item: id}
	}
	m.actionStatus = ""
	return m.waveOverlap()
}

func (m *Model) waveInputKey(msg tea.KeyPressMsg) tea.Cmd {
	w := m.waves
	in := w.inputs
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		delete(w.picked, in.item)
		w.inputs = nil
		m.actionStatus = "Cancelled"
		return m.waveOverlap()
	case "tab", "shift+tab":
		in.focus = 1 - in.focus
		return nil
	case "enter":
		well, valid := launchAnswerValue(in.fields[0].Value())
		if !valid {
			in.focus = 0
			m.actionStatus = "Well specified? requires y or n"
			return nil
		}
		split, valid := launchAnswerValue(in.fields[1].Value())
		if !valid {
			in.focus = 1
			m.actionStatus = "Split? requires y or n"
			return nil
		}
		p := w.picked[in.item]
		p.WellSpecified, p.Split = well, split
		w.picked[in.item] = p
		w.planning = in.item
		w.inputs = nil
		w.planRequest++
		m.actionStatus = ""
		live, ctx, request, seq := m.cfg.Live, w.ctx, w.request, w.planRequest
		return func() tea.Msg {
			if ctx.Err() != nil {
				return nil
			}
			plan, err := live.Plan(ctx, p)
			var engineItem *bool
			if err == nil {
				detail, detailErr := live.ItemShow(ctx, p.Project, p.Item)
				err = detailErr
				value := daemon.EngineSlice(detail)
				engineItem = &value
			}
			return wavesMsg{engineItem: engineItem, request: request, kind: "plan", sequence: seq, item: p.Item, plan: plan, err: err}
		}
	default:
		in.fields[in.focus].update(msg)
		m.actionStatus = ""
	}
	return nil
}

func (m *Model) confirmWave() tea.Cmd {
	w := m.waves
	if m.view != wavesView || m.showHelp || w == nil {
		return nil
	}
	if a, ok := m.waveFlights[w.project]; ok {
		m.actionStatus = a.command() + " is pending"
		return nil
	}
	if w.planning != "" {
		m.actionStatus = "A plan is pending; wait before launching"
		return nil
	}
	if conflict := w.engineConflict(); conflict != "" {
		m.actionStatus = conflict
		return nil
	}
	items := w.items()
	if w.starting || len(items) == 0 {
		return nil
	}
	for _, p := range items {
		if p.Expected == nil {
			m.actionStatus = "Every ticked item needs a plan"
			return nil
		}
	}
	m.actionStatus = ""
	m.confirm = &runAction{verb: "wave", project: w.project, items: items}
	return nil
}

func (m *Model) startWave(a runAction) tea.Cmd {
	w := m.waves
	if w == nil || w.starting || w.project != a.project {
		return nil
	}
	if _, ok := m.waveFlights[a.project]; ok {
		return nil
	}
	if m.waveFlights == nil {
		m.waveFlights = map[string]runAction{}
	}
	m.waveFlights[a.project] = a
	w.starting = true
	m.actionStatus = a.command() + ": pending"
	live, request := m.cfg.Live, w.request
	// Confirmation authorizes this call even when the owner leaves the view.
	return func() tea.Msg {
		result, err := live.WaveStart(context.Background(), daemon.WaveStartParams{Items: a.items})
		return wavesMsg{request: request, kind: "start", action: a, result: result, err: err}
	}
}

func (m *Model) applyWaves(msg wavesMsg) tea.Cmd {
	w := m.waves
	if msg.kind == "start" {
		if _, ok := m.waveFlights[msg.action.project]; !ok {
			return nil
		}
		delete(m.waveFlights, msg.action.project)
		var itemErrors []string
		if msg.result != nil {
			for _, item := range msg.result.Items {
				if item.Error != nil {
					itemErrors = append(itemErrors, item.Item+": "+actionError(item.Error))
				}
			}
		}
		status := msg.action.command() + ": done"
		if len(itemErrors) > 0 {
			status = msg.action.command() + ": errors"
		}
		if msg.err != nil {
			status = msg.action.command() + ": " + actionError(msg.err)
		}
		for _, itemError := range itemErrors {
			status += "; " + itemError
		}
		if w != nil && w.request == msg.request {
			w.starting = false
			if msg.err == nil {
				w.picked = map[string]daemon.RunParams{}
				w.overlap = nil
				w.overlapErr = nil
				w.overlapRequest++
			}
		}
		m.actionStatus = status
		if m.poller.InFlight(msg.action.project) {
			m.byName[msg.action.project].dirty = true
		}
		return m.fetch(msg.action.project, true)
	}
	if w == nil || w.request != msg.request || m.view != wavesView {
		return nil
	}
	switch msg.kind {
	case "suggest":
		w.pending = false
		w.suggest, w.err = msg.suggest, msg.err
	case "overlap":
		if w.overlapRequest == msg.sequence {
			w.overlap, w.overlapErr = msg.overlap, msg.err
		}
	case "plan":
		if w.planRequest != msg.sequence || w.planning != msg.item {
			return nil
		}
		w.planning = ""
		if msg.err != nil {
			m.actionStatus = actionError(msg.err)
			return nil
		}
		if msg.plan == nil {
			m.actionStatus = "No run plan returned"
			return nil
		}
		p, ok := w.picked[msg.item]
		if !ok {
			return nil
		}
		if msg.params != nil {
			p = *msg.params
		}
		if w.engineItems == nil {
			w.engineItems = map[string]bool{}
		}
		if msg.engineItem != nil {
			w.engineItems[msg.item] = *msg.engineItem
		}
		p.Expected = msg.plan
		w.picked[msg.item] = p
	}
	return nil
}

func (m *Model) wavesBody(width int) overviewView {
	out := overviewView{sel: -1}
	th := m.cfg.Theme
	w := m.waves
	add := func(text string) {
		for _, line := range wrapText(text, max(width-2, 1)) {
			out.body = append(out.body, indent+line)
		}
	}
	out.body = append(out.body, th.Heading.Render("Suggested (slicer plan-parallel)"))
	if m.active < 0 {
		add("Select a project for Waves")
		return out
	}
	if m.cfg.Live == nil {
		add("No daemon connection; action unavailable")
		return out
	}
	if w == nil || w.pending {
		add("pending: wave.suggest")
		return out
	}
	if w.err != nil {
		add(actionError(w.err))
		return out
	}
	out.body = append(out.body, th.Heading.Render("Items                   Lineup (* edited for this run)"))
	row := 0
	draw := func(it slicercli.ParallelItem) {
		tick := "[ ]"
		p, ok := w.picked[it.ID]
		if ok {
			tick = "[x]"
		}
		label := tick + " " + it.ID + "  " + it.Title
		if it.Reason != "" {
			label += " (" + it.Reason + ")"
		}
		col := min(24, max(width/2, 1))
		var lineup []string
		if p.Expected != nil {
			for _, role := range []string{"implementer", "reviewer", "gate"} {
				var chain []string
				for _, e := range p.Expected.Lineup[role] {
					chain = append(chain, e.Vendor+" "+e.Model+" "+e.Effort)
				}
				if len(chain) > 0 {
					label := role
					if len(p.Lineup[role]) > 0 {
						label += "*"
					}
					lineup = append(lineup, label+": "+strings.Join(chain, " -> "))
				}
			}
		} else if w.planning == it.ID {
			lineup = []string{"pending: run.plan"}
		}
		if p.Budgets != nil {
			lineup = append(lineup, fmt.Sprintf("budgets*: %d min, %d tokens", p.Budgets.Minutes, p.Budgets.Tokens))
		}
		line := indent + label
		if len(lineup) > 0 {
			line = indent + fmt.Sprintf("%-*s", col, clip(label, col-1)) + lineup[0]
		}
		if row == clampCursor(w.cursor, len(w.rows())) {
			out.sel = len(out.body)
			line = th.TabActive.Render(line)
		}
		out.body = append(out.body, line)
		for i, text := range lineup {
			if i == 0 {
				continue
			}
			out.body = append(out.body, indent+strings.Repeat(" ", col)+text)
		}
		row++
	}
	if w.suggest != nil {
		for _, wave := range w.suggest.Waves {
			add(fmt.Sprintf("Wave %d", wave.Wave))
			for _, it := range wave.Items {
				draw(it)
			}
		}
		if len(w.suggest.Unplaced) > 0 {
			add("Unplaced")
			for _, it := range w.suggest.Unplaced {
				draw(it)
			}
		}
	}
	if row == 0 {
		add("No suggested items")
	}
	if conflict := w.engineConflict(); conflict != "" {
		add("! " + conflict)
	}
	if w.overlapErr != nil {
		add(actionError(w.overlapErr))
	}
	if w.overlap != nil {
		for _, pair := range w.overlap.Overlap {
			for _, path := range pair.Shared {
				add("! " + pair.A + " and " + pair.B + " both touch " + path)
			}
		}
	}
	if w.inputs != nil {
		add(w.inputs.item + " launch inputs (tab switches fields; enter plans; esc cancels)")
		for i, label := range []string{"Well specified? [y/n]:", "Split? [y/n]:"} {
			prefix := "  "
			if w.inputs.focus == i {
				prefix = "> "
			}
			out.body = append(out.body, prefix+label+" "+w.inputs.fields[i].view(max(width-len(label)-3, 1), th))
		}
		out.sel = len(out.body) - 1
	}
	add("[space] tick item   [e] edit lineup   [l] launch wave")
	return out
}

func (w *wavesState) engineConflict() string {
	first := ""
	for _, item := range w.items() {
		if !w.engineItems[item.Item] {
			continue
		}
		if first != "" {
			return daemon.EngineConflict(first, item.Item)
		}
		first = item.Item
	}
	return ""
}
