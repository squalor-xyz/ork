// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	pane           *itemPane
	paneItem       slicercli.ParallelItem
	paneOffset     int
	paneRequest    uint64
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
	detail     *slicercli.Detail
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
	case "detail":
		if w.pane != nil && w.paneRequest == msg.sequence && w.pane.id == msg.item {
			w.pane.pending = false
			w.pane.detail, w.pane.err = msg.detail, msg.err
		}
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
	if w != nil && w.pane != nil {
		return m.waveItemBody(width)
	}
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
	// Reserve the key columns before giving the remaining width to titles.
	rows := w.rows()
	idWidth, sharesWidth := 1, len("Shares")
	for _, it := range rows {
		idWidth = max(idWidth, lipgloss.Width(it.ID))
		shares, _ := waveShares(it.Reason)
		sharesWidth = max(sharesWidth, lipgloss.Width(shares))
	}
	showLineup := len(w.picked) > 0
	lineupWidth := 0
	if showLineup {
		lineupWidth = max((width-2)/2, 1)
	}
	titleWidth := max(width-2-4-idWidth-2-2-sharesWidth, 1)
	if showLineup {
		titleWidth = max(titleWidth-lineupWidth-2, 1)
	}
	pad := func(text string, n int) string {
		return text + strings.Repeat(" ", max(n-lipgloss.Width(text), 0))
	}
	columns := func(left, title, shares, lineup string, dim, selected bool) string {
		before := indent + pad(left, 4+idWidth+2) + pad(clip(title, titleWidth), titleWidth) + "  "
		shareCell := pad(shares, sharesWidth)
		after := ""
		if showLineup {
			after = "  " + pad(clip(lineup, lineupWidth), lineupWidth)
		}
		if selected {
			before = th.TabActive.Render(before)
			if after != "" {
				after = th.TabActive.Render(after)
			}
			shareCell = th.TabActive.Render(shareCell)
		} else if dim && shares != "" {
			shareCell = th.Dim.Render(shares) + strings.Repeat(" ", max(sharesWidth-lipgloss.Width(shares), 0))
		}
		return before + shareCell + after
	}
	header := columns("", "Items", "Shares", "Lineup (* edited)", false, false)
	out.body = append(out.body, th.Heading.Render(header))
	rule := func(label string, count int) {
		_, h := th.rules(m.cfg.ASCII)
		noun := "items"
		if count == 1 {
			noun = "item"
		}
		prefix := h + h + " " + label + " "
		suffix := fmt.Sprintf(" %d %s", count, noun)
		out.body = append(out.body, th.Group.Render(prefix+strings.Repeat(h, max(width-lipgloss.Width(prefix)-len(suffix), 1))+suffix))
	}
	row := 0
	draw := func(it slicercli.ParallelItem) {
		tick := "[ ]"
		p, ok := w.picked[it.ID]
		if ok {
			tick = "[x]"
		}
		shares, _ := waveShares(it.Reason)
		title := it.Title
		if it.Reason != "" && shares == "" {
			title += " (" + it.Reason + ")"
		}
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
		first := ""
		if len(lineup) > 0 {
			first = wrapText(lineup[0], max(lineupWidth, 1))[0]
		}
		selected := row == clampCursor(w.cursor, len(rows))
		line := columns(tick+" "+it.ID, title, shares, first, true, selected)
		if selected {
			out.sel = len(out.body)
		}
		out.body = append(out.body, line)
		// Wrap each role chain within its column, preserving all fallbacks.
		for i, text := range lineup {
			parts := wrapText(text, max(lineupWidth, 1))
			if i == 0 {
				parts = parts[1:]
			}
			for _, part := range parts {
				out.body = append(out.body, indent+strings.Repeat(" ", 4+idWidth+2+titleWidth+2+sharesWidth+2)+pad(part, lineupWidth))
			}
		}
		row++
	}
	if w.suggest != nil {
		for _, wave := range w.suggest.Waves {
			rule(fmt.Sprintf("Wave %d", wave.Wave), len(wave.Items))
			for _, it := range wave.Items {
				draw(it)
			}
		}
		if len(w.suggest.Unplaced) > 0 {
			rule("Unplaced", len(w.suggest.Unplaced))
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
	hints := ""
	for _, hint := range []string{"[enter] item", "[space] tick item", "[e] edit lineup", "[l] launch wave"} {
		if hints != "" && lipgloss.Width(hints)+3+len(hint) > width-len(indent) {
			add(hints)
			hints = ""
		}
		if hints != "" {
			hints += "   "
		}
		hints += hint
	}
	add(hints)
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

// waveShares decodes the reason shape emitted by slicer plan-parallel.
// Keep the original reason in the pane, including paths with spaces.
func waveShares(reason string) (marker, paths string) {
	rest, ok := strings.CutPrefix(reason, "shares: ")
	if !ok {
		return "", ""
	}
	id, paths, ok := strings.Cut(rest, " ")
	if !ok || id == "" || strings.TrimSpace(paths) == "" {
		return "", ""
	}
	return "<" + id, paths
}

func (m *Model) openWaveItem() tea.Cmd {
	w := m.waves
	if m.showHelp || w == nil || w.pending || w.err != nil || w.starting || w.pane != nil {
		return nil
	}
	rows := w.rows()
	if len(rows) == 0 {
		return nil
	}
	it := rows[clampCursor(w.cursor, len(rows))]
	w.paneRequest++
	w.paneItem, w.paneOffset = it, 0
	w.pane = &itemPane{project: w.project, id: it.ID, pending: true}
	m.actionStatus = ""
	if m.cfg.Live == nil {
		w.pane.pending = false
		w.pane.err = fmt.Errorf("No daemon connection; detail unavailable")
		return nil
	}
	live, ctx, project, request, seq := m.cfg.Live, w.ctx, w.project, w.request, w.paneRequest
	return func() tea.Msg {
		if ctx.Err() != nil {
			return nil
		}
		detail, err := live.ItemShow(ctx, project, it.ID)
		return wavesMsg{kind: "detail", request: request, sequence: seq, item: it.ID, detail: detail, err: err}
	}
}

func (m *Model) waveItemKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.waves.pane = nil
	case "pgdown", "pgup":
		width, height, side := m.layoutSize()
		headerLines := len(wrapText("Item "+m.waves.pane.id+"  (esc returns)", max(width-side, 1)))
		step := max(height-4-headerLines, 1)
		if msg.String() == "pgup" {
			step = -step
		}
		m.waves.paneOffset = max(m.waves.paneOffset+step, 0)
	case "down", "j":
		m.waves.paneOffset++
	case "up", "k":
		m.waves.paneOffset = max(m.waves.paneOffset-1, 0)
	}
	return nil
}

func (m *Model) waveItemBody(width int) overviewView {
	w, th := m.waves, m.cfg.Theme
	out := overviewView{sel: -1}
	var body []string
	heading := func(text string) {
		for _, line := range wrapText(text, max(width, 1)) {
			body = append(body, th.Heading.Render(line))
		}
	}
	heading("Item " + w.pane.id + "  (esc returns)")
	out.body, body = body, nil
	add := func(s string) {
		for _, line := range wrapText(s, max(width-2, 1)) {
			body = append(body, indent+line)
		}
	}
	title := w.paneItem.Title
	if d := w.pane.detail; d != nil && !w.pane.pending && w.pane.err == nil {
		title = d.Title
		if d.Slice != nil {
			title = d.Slice.Title
		}
	}
	heading(title)
	if marker, path := waveShares(w.paneItem.Reason); marker != "" {
		add("Shares files with " + strings.TrimPrefix(marker, "<"))
		for _, line := range wrapText(path, max(width-2*len(indent), 1)) {
			body = append(body, indent+indent+line)
		}
	} else if w.paneItem.Reason != "" {
		add(w.paneItem.Reason)
	}
	switch {
	case w.pane.pending:
		add("pending: item.show")
	case w.pane.err != nil:
		add(actionError(w.pane.err))
	case w.pane.detail != nil:
		d := w.pane.detail
		if d.Slice != nil {
			add("Size: " + d.Slice.Size)
			for _, lead := range d.Slice.Lead {
				add(lead)
			}
			for _, section := range d.Slice.Sections {
				heading(section.Heading)
				add(section.Body)
			}
		} else {
			add("Size: " + d.Fields.Size)
		}
	default:
		add("No item detail returned")
	}
	_, height, _ := m.layoutSize()
	visible := max(height-4-len(out.body), 1)
	w.paneOffset = min(w.paneOffset, max(len(body)-visible, 0))
	out.body = append(out.body, body[w.paneOffset:]...)
	return out
}
