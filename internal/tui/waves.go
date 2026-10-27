// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"slices"
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
	showAll        bool
	launched       map[string]string
	starting       bool
	checking       bool                             // a spec check is in flight
	checkRequest   uint64                           // the check whose reply counts
	specs          map[string]daemon.SpecItemResult // the spec check's latest answer for each item
	sitting        *specSitting                     // the spec questions on screen
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
	check      *daemon.SpecCheckResult
	answer     *daemon.SpecAnswerResult
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
	for _, wave := range w.suggest.Waves[:w.visibleWaveCount()] {
		rows = append(rows, wave.Items...)
	}
	return append(rows, w.suggest.Unplaced...)
}

func (w *wavesState) visibleWaveCount() int {
	if w.suggest == nil {
		return 0
	}
	if w.showAll {
		return len(w.suggest.Waves)
	}
	return min(3, len(w.suggest.Waves))
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

// tickRefusal is why space does not tick or untick the row under the cursor:
// the status line text, or "" for a silent refusal. It reports refused false
// when space acts. The key refuses by it and the hint line lists space only
// when it does not.
func (m *Model) tickRefusal() (status string, refused bool) {
	w := m.waves
	if !m.wavesTickable() {
		return "", true
	}
	if m.cfg.Live == nil {
		return "No daemon connection; action unavailable", true
	}
	rows := w.rows()
	if len(rows) == 0 {
		return "", true
	}
	id := rows[clampCursor(w.cursor, len(rows))].ID
	if w.launched[id] != "" {
		return "", true
	}
	if _, ok := w.picked[id]; !ok && w.planning != "" {
		return "A plan is pending; wait before ticking another item", true
	}
	return "", false
}

func (m *Model) tickWave() tea.Cmd {
	w := m.waves
	if status, refused := m.tickRefusal(); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	rows := w.rows()
	w.cursor = clampCursor(w.cursor, len(rows))
	id := rows[w.cursor].ID
	if _, ok := w.picked[id]; ok {
		delete(w.picked, id)
		if w.planning == id {
			w.planRequest++
			w.planning = ""
		}
	} else {
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

// waveLaunchRefusal is why l does not launch the ticked items: the status line
// text, or "" for a silent refusal. It reports refused false when l asks to
// launch. The key refuses by it and the hint line lists l only when it does not.
func (m *Model) waveLaunchRefusal() (status string, refused bool) {
	w := m.waves
	if m.view != wavesView || m.showHelp || w == nil {
		return "", true
	}
	if a, ok := m.waveFlights[w.project]; ok {
		return a.command() + " is pending", true
	}
	if w.planning != "" {
		return "A plan is pending; wait before launching", true
	}
	if w.checking {
		return "", true
	}
	if conflict := w.engineConflict(); conflict != "" {
		return conflict, true
	}
	items := w.items()
	if w.starting || len(items) == 0 {
		return "", true
	}
	for _, p := range items {
		if p.Expected == nil {
			return "Every ticked item needs a plan", true
		}
	}
	return "", false
}

func (m *Model) confirmWave() tea.Cmd {
	if status, refused := m.waveLaunchRefusal(); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	m.actionStatus = ""
	return m.startSpecCheck(m.waves.items())
}

// wavesTickable reports whether space may tick the row under the cursor now:
// the plan has loaded and nothing is starting or being checked.
func (m *Model) wavesTickable() bool {
	w := m.waves
	return m.view == wavesView && !m.showHelp && w != nil && !w.pending && w.err == nil && !w.starting && !w.checking
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
		var itemErrors, launchedItems, launchedRuns []string
		if msg.result != nil {
			for _, item := range msg.result.Items {
				if item.Error == nil && item.Run != nil && item.Run.ID != "" {
					launchedItems, launchedRuns = append(launchedItems, item.Item), append(launchedRuns, item.Run.ID)
					if w != nil && w.request == msg.request {
						if w.launched == nil {
							w.launched = map[string]string{}
						}
						w.launched[item.Item] = item.Run.ID
						delete(w.picked, item.Item)
					}
				}
				if item.Error != nil {
					itemErrors = append(itemErrors, item.Item+": "+actionError(item.Error))
				}
			}
		}
		status := msg.action.command() + ": done"
		switch len(launchedItems) {
		case 0:
		case 1:
			status = "launched " + launchedItems[0] + " as " + launchedRuns[0] + "; enter opens it"
		default:
			status = fmt.Sprintf("launched %d items; enter on a running row opens its run", len(launchedItems))
		}
		if len(itemErrors) > 0 && len(launchedItems) == 0 {
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
				// Ticked items that were not in the call wait on spec questions.
				var waiting []string
				for _, p := range w.items() {
					if !slices.ContainsFunc(msg.action.items, func(a daemon.RunParams) bool { return a.Item == p.Item }) {
						waiting = append(waiting, p.Item)
					}
				}
				if len(waiting) > 0 {
					verb := "waits"
					if len(waiting) > 1 {
						verb = "wait"
					}
					status += "; " + strings.Join(waiting, ", ") + " " + verb + " on spec questions"
				}
				kept := map[string]daemon.RunParams{}
				for _, id := range waiting {
					kept[id] = w.picked[id]
				}
				w.picked = kept
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
	case "check":
		return m.applyCheck(msg)
	case "answer":
		return m.applyAnswer(msg)
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

// wavesBody is the Waves body.
func (m *Model) wavesBody(width int) overviewView {
	out, footer := m.wavesParts(width)
	out.body = append(out.body, footer...)
	return out
}

// wavesParts returns the scrolling body. Keys live in the common hint line.
func (m *Model) wavesParts(width int) (overviewView, []string) {
	out := overviewView{sel: -1}
	th := m.cfg.Theme
	w := m.waves
	if w != nil && w.pane != nil {
		return m.waveItemBody(width), nil
	}
	if w != nil && w.sitting != nil {
		return m.sittingParts(width)
	}
	add := func(text string) {
		for _, line := range wrapText(text, max(width-2, 1)) {
			out.body = append(out.body, indent+line)
		}
	}
	out.body = append(out.body, th.Heading.Render("Suggested (slicer plan-parallel)"))
	if m.active < 0 {
		add("Select a project for Waves")
		return out, nil
	}
	if m.cfg.Live == nil {
		add("No daemon connection; action unavailable")
		return out, nil
	}
	if w == nil || w.pending {
		add("pending: wave.suggest")
		return out, nil
	}
	if w.err != nil {
		add(actionError(w.err))
		return out, nil
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
		lineupWidth = len("Lineup (* edited)")
		for _, p := range w.picked {
			for _, line := range waveLineup(p) {
				lineupWidth = max(lineupWidth, lipgloss.Width(line))
			}
		}
		if w.planning != "" {
			lineupWidth = max(lineupWidth, len("pending: run.plan"))
		}
		lineupWidth = min(lineupWidth, max((width-2)/2, 1))
	}
	titleWidth := max(width-2-4-idWidth-2-2-sharesWidth, 1)
	if showLineup {
		titleWidth = max(titleWidth-lineupWidth-2, 1)
	}
	titleCol := len(indent) + 4 + idWidth + 2
	// cell pads text to n columns. A dim cell is dimmed on its text alone, so
	// the style survives the resets that end every other cell.
	cell := func(text string, n int, dim, selected bool) string {
		gap := strings.Repeat(" ", max(n-lipgloss.Width(text), 0))
		switch {
		case text == "":
			if selected {
				return th.TabActive.Render(gap)
			}
			return gap
		case selected && dim:
			return th.TabActive.Render(th.Dim.Render(text)) + th.TabActive.Render(gap)
		case selected:
			return th.TabActive.Render(text + gap)
		case dim:
			return th.Dim.Render(text) + gap
		}
		return text + gap
	}
	columns := func(left, title, shares, lineup string, dimShares, dimAll, selected bool) string {
		line := cell(indent+left, titleCol, dimAll, selected) + cell(clip(title, titleWidth), titleWidth, dimAll, selected) + cell("", 2, false, selected)
		line += cell(shares, sharesWidth, dimAll || (dimShares && !selected), selected)
		if showLineup {
			line += cell("", 2, false, selected) + cell(clip(lineup, lineupWidth), lineupWidth, dimAll, selected)
		}
		return line
	}
	// addAt puts a secondary line under the title column.
	addAt := func(text string, dim bool) {
		for _, line := range wrapText(text, max(width-titleCol, 1)) {
			if dim {
				line = th.Dim.Render(line)
			}
			out.body = append(out.body, strings.Repeat(" ", titleCol)+line)
		}
	}
	header := columns("", "Items", "Shares", "Lineup (* edited)", false, false, false)
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
		run := w.launched[it.ID]
		if run != "" {
			tick = "[>]"
		}
		note := ""
		if run != "" {
			note = "running " + run
		} else if r, checked := w.specs[it.ID]; checked && ok {
			note = specNote(r)
		}
		shares, _ := waveShares(it.Reason)
		title := it.Title
		reasonBelow := it.Reason != "" && shares == "" && lipgloss.Width(title+" ("+waveReason(it.Reason)+")") > titleWidth
		if it.Reason != "" && shares == "" && !reasonBelow {
			title += " (" + waveReason(it.Reason) + ")"
		}
		lineup := waveLineup(p)
		if w.planning == it.ID {
			lineup = []string{"pending: run.plan"}
		}
		first := ""
		if len(lineup) > 0 {
			first = wrapText(lineup[0], max(lineupWidth, 1))[0]
		}
		runBelow := false
		if note != "" {
			runBelow = lipgloss.Width(title+"  "+note) > titleWidth
			if !runBelow {
				title += "  " + note
			}
		}
		selected := row == clampCursor(w.cursor, len(rows))
		out.rows = append(out.rows, len(out.body))
		if selected {
			out.sel = len(out.body)
		}
		out.body = append(out.body, columns(tick+" "+it.ID, title, shares, first, true, run != "", selected))
		if reasonBelow {
			addAt(waveReason(it.Reason), true)
		}
		if runBelow {
			addAt(note, true)
		}
		// Wrap the compact role summaries within their column.
		for i, text := range lineup {
			parts := wrapText(text, max(lineupWidth, 1))
			if i == 0 {
				parts = parts[1:]
			}
			for _, part := range parts {
				fallback := strings.HasPrefix(strings.TrimSpace(part), "+")
				out.body = append(out.body, columns("", "", "", part, false, fallback, selected))
			}
		}
		if w.inputs != nil && w.inputs.item == it.ID {
			addAt("launch inputs", true)
			for i, label := range []string{"Well specified? [y/n]:", "Split? [y/n]:"} {
				room := max(width-titleCol-len(label)-1, 1)
				value := clip(w.inputs.fields[i].Value(), room)
				if w.inputs.focus == i {
					value = w.inputs.fields[i].view(room, th)
				}
				out.body = append(out.body, strings.Repeat(" ", titleCol)+label+" "+value)
			}
			out.sel = len(out.body) - 1
		}
		row++
	}
	if w.suggest != nil {
		for _, wave := range w.suggest.Waves[:w.visibleWaveCount()] {
			rule(fmt.Sprintf("Wave %d", wave.Wave), len(wave.Items))
			for _, it := range wave.Items {
				draw(it)
			}
		}
		if n := len(w.suggest.Waves) - w.visibleWaveCount(); n > 0 {
			noun := "waves"
			if n == 1 {
				noun = "wave"
			}
			add(th.Dim.Render(fmt.Sprintf("%d more %s; [A] show all", n, noun)))
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
	return out, nil
}

func waveReason(reason string) string {
	if reason == "no_files" {
		return "no files listed"
	}
	return reason
}

func waveLineup(p daemon.RunParams) []string {
	var lines []string
	if p.Expected == nil {
		return nil
	}
	for _, role := range []struct{ key, label string }{{"implementer", "impl"}, {"reviewer", "rev"}, {"ui", "ui"}, {"gate", "gate"}, {"architect", "arch"}} {
		chain := p.Expected.Lineup[role.key]
		if override, edited := p.Lineup[role.key]; role.key == "architect" && edited && len(override) == 0 {
			lines = append(lines, "arch*: off")
			continue
		}
		if len(chain) == 0 {
			continue
		}
		label := role.label
		if len(p.Lineup[role.key]) > 0 {
			label += "*"
		}
		e := chain[0]
		lines = append(lines, fmt.Sprintf("%-6s %s %s %s", label+":", e.Vendor, e.Model, e.Effort))
		if len(chain) > 1 {
			// Fallbacks sit under the values, past the label column.
			lines = append(lines, fmt.Sprintf("%6s +%d fallbacks", "", len(chain)-1))
		}
	}
	if p.Budgets != nil {
		lines = append(lines, fmt.Sprintf("budgets*: %d min, %d tokens", p.Expected.Budgets.Minutes, p.Expected.Budgets.Tokens))
	}
	return lines
}

// selectJumpRun also runs after refresh, when admission preceded the snapshot.
func (m *Model) selectJumpRun() {
	if m.jumpRun == "" || m.view != 0 {
		return
	}
	needs, running := m.rows()
	all := append(append(append(needs, running...), m.recentRows()...), m.readyRows()...)
	for i, r := range all {
		if r.engineID == m.jumpRun {
			m.cursor = i
			m.jumpRun = ""
			return
		}
	}
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
	if run := w.launched[it.ID]; run != "" {
		m.view = 0
		m.jumpRun = run
		m.selectJumpRun()
		return m.fetch(w.project, true)
	}
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
		add(waveReason(w.paneItem.Reason))
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
