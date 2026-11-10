// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
)

// Saved waves on the Waves screen (S216, *proposed*): the name input that saves
// the ticked set, the saved-wave list that takes the table's place, and the load
// that ticks a saved wave's members in the table. Removing a saved wave stays
// `ork wave rm`.

// nameInput is the one-line input that names the ticked set.
type nameInput struct {
	field   textField
	ids     []string // the ticked ids in table order, fixed when the input opened
	name    string   // the name last sent
	pending bool     // wave.define is in flight
	replace bool     // the daemon refused a duplicate: asking "Replace NAME?"
}

// savedList is the saved-wave list that takes the table's place.
type savedList struct {
	pending bool
	err     error
	waves   []daemon.SavedWaveView
	cursor  int
}

// loadedWave is the wave the last load ticked from: its name and its startable
// members, whether or not the table holds them.
type loadedWave struct {
	name string
	ids  []string
}

// savedWavesRefusal is why w and L do not act now: the status line text, or ""
// for a silent refusal. verb completes "wait before ...". The hint line lists
// each key only when its check lets it through.
func (m *Model) savedWavesRefusal(verb string) (status string, refused bool) {
	w := m.waves
	if m.view != wavesView || m.showHelp || w == nil || m.active < 0 {
		return "", true
	}
	if m.cfg.Live == nil {
		return "No daemon connection; action unavailable", true
	}
	if w.pending || w.err != nil || w.checking || w.sitting != nil || w.lineup != nil || w.pane != nil || w.naming != nil || w.list != nil {
		return "", true
	}
	if a, ok := m.waveFlights[w.project]; ok {
		return a.command() + " is pending", true
	}
	if w.starting {
		return "", true
	}
	if w.planning != "" {
		return "A plan is pending; wait before " + verb, true
	}
	return "", false
}

// saveWaveRefusal is why w does not open the name input.
func (m *Model) saveWaveRefusal() (status string, refused bool) {
	if status, refused = m.savedWavesRefusal("saving a wave"); refused {
		return status, true
	}
	items := m.waves.items()
	if len(items) == 0 {
		return "Tick at least one item to save a wave", true
	}
	for _, p := range items {
		if p.Expected == nil {
			return "Every ticked item needs a plan", true
		}
	}
	return "", false
}

// savedListRefusal is why L does not open the saved list.
func (m *Model) savedListRefusal() (status string, refused bool) {
	return m.savedWavesRefusal("listing saved waves")
}

func (m *Model) openSaveWave() tea.Cmd {
	if status, refused := m.saveWaveRefusal(); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	w := m.waves
	n := &nameInput{}
	for _, p := range w.items() {
		n.ids = append(n.ids, p.Item)
	}
	w.naming = n
	m.actionStatus = ""
	return nil
}

// nameKey takes every key while the name input is open.
func (m *Model) nameKey(msg tea.KeyPressMsg) tea.Cmd {
	w := m.waves
	n := w.naming
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	if n.pending {
		return nil
	}
	if n.replace {
		switch msg.String() {
		case "y", "Y":
			return m.defineWave(true)
		case "n", "N", "enter", "esc":
			n.replace = false
			m.actionStatus = ""
		}
		return nil
	}
	switch msg.String() {
	case "esc":
		w.naming = nil
		m.actionStatus = "Cancelled"
	case "enter":
		if strings.TrimSpace(n.field.Value()) == "" {
			m.actionStatus = "Name must not be empty"
			return nil
		}
		return m.defineWave(false)
	default:
		n.field.update(msg)
	}
	return nil
}

// defineWave sends wave.define for the typed name. The owner's enter authorizes
// the call even if the screen closes meanwhile, as a launch's does.
func (m *Model) defineWave(replace bool) tea.Cmd {
	w := m.waves
	n := w.naming
	n.name = strings.TrimSpace(n.field.Value())
	n.pending, n.replace = true, false
	m.actionStatus = "pending: wave.define"
	live, request, name := m.cfg.Live, w.request, n.name
	params := daemon.WaveDefineParams{Project: w.project, Name: name, Items: slices.Clone(n.ids), Replace: replace}
	return func() tea.Msg {
		saved, err := live.WaveDefine(context.Background(), params)
		return wavesMsg{request: request, kind: "define", name: name, saved: saved, err: err}
	}
}

// applyDefine takes the reply of wave.define.
func (m *Model) applyDefine(msg wavesMsg) tea.Cmd {
	w := m.waves
	n := w.naming
	if n == nil || !n.pending || n.name != msg.name {
		return nil
	}
	n.pending = false
	var wire *ipc.Error
	switch {
	case errors.As(msg.err, &wire) && wire.Code == daemon.CodeDuplicateName:
		// NW-Q3: the daemon refuses a duplicate unless replace is set; ask first.
		n.replace = true
		m.actionStatus = ""
	case msg.err != nil:
		m.actionStatus = actionError(msg.err)
	default:
		items := n.ids
		if msg.saved != nil {
			items = msg.saved.Items
		}
		w.naming = nil
		m.actionStatus = "saved " + msg.name + ": " + strings.Join(items, ", ")
	}
	return nil
}

// prompt is the status text while the name input is open.
func (n *nameInput) prompt(width int, th Theme, status string) string {
	if n.replace {
		return "Replace " + n.name + "? [y/N] (Enter is No)"
	}
	out := "Save wave " + strings.Join(n.ids, ", ") + " as:\n"
	if n.pending {
		out += n.field.Value()
	} else {
		out += n.field.view(width, th)
	}
	if status != "" {
		out += "\n" + status
	}
	return out
}

func (m *Model) openSavedWaves() tea.Cmd {
	if status, refused := m.savedListRefusal(); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	w := m.waves
	w.list = &savedList{pending: true}
	w.listRequest++
	m.actionStatus = ""
	live, ctx, project, request, seq := m.cfg.Live, w.ctx, w.project, w.request, w.listRequest
	return func() tea.Msg {
		if ctx.Err() != nil {
			return nil
		}
		waves, err := live.WaveList(ctx, project)
		return wavesMsg{request: request, kind: "saved", sequence: seq, waves: waves, err: err}
	}
}

func (m *Model) applySavedList(msg wavesMsg) {
	l := m.waves.list
	if l == nil || m.waves.listRequest != msg.sequence {
		return
	}
	l.pending, l.err, l.waves = false, msg.err, msg.waves
	l.cursor = clampCursor(l.cursor, len(l.waves))
}

// savedListKey takes every key while the saved list is open.
func (m *Model) savedListKey(msg tea.KeyPressMsg) tea.Cmd {
	w := m.waves
	l := w.list
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		w.list = nil
		w.listRequest++ // a reply that is still coming is stale
	case "down", "j":
		l.cursor = clampCursor(l.cursor+1, len(l.waves))
	case "up", "k":
		l.cursor = clampCursor(l.cursor-1, len(l.waves))
	case "enter":
		if l.pending || l.err != nil || len(l.waves) == 0 {
			return nil
		}
		return m.loadSaved(l.waves[clampCursor(l.cursor, len(l.waves))])
	}
	return nil
}

// loadSaved ticks the startable members of the saved wave that the table
// holds, in table order, and plans them one after another as space does (NW-Q8
// (a), owner 2026-11-10). Members the suggestion does not offer are named with
// the reason and not ticked; done, retired and missing members are named and
// skipped. A load never launches: l runs the spec check and the confirmation.
func (m *Model) loadSaved(view daemon.SavedWaveView) tea.Cmd {
	w := m.waves
	ids, skipped := view.Startable()
	if !w.showAll && slices.ContainsFunc(ids, w.inHiddenWave) {
		w.showAll = true // a member in a wave past the third is a row too
	}
	var ticked []string
	for _, row := range w.rows() {
		if slices.Contains(ids, row.ID) && w.launched[row.ID] == "" {
			ticked = append(ticked, row.ID)
		}
	}
	var absent []string
	for _, id := range ids {
		switch run := w.launched[id]; {
		case run != "":
			absent = append(absent, id+" (running "+run+")")
		case !slices.Contains(ticked, id):
			absent = append(absent, id+" ("+w.absentReason(id)+")")
		}
	}
	for _, id := range ticked {
		if _, ok := w.picked[id]; ok {
			continue // already ticked, with its plan or its plan on the way
		}
		w.picked[id] = daemon.RunParams{Project: w.project, Item: id, WellSpecified: true}
		w.planQueue = append(w.planQueue, id)
	}
	w.loaded = &loadedWave{name: view.Name, ids: ids}
	w.list = nil
	w.listRequest++
	for i, row := range w.rows() {
		if len(ticked) > 0 && row.ID == ticked[0] {
			w.cursor = i
			break
		}
	}
	parts := []string{"nothing to tick"}
	if len(ticked) > 0 {
		parts[0] = "ticked " + strings.Join(ticked, ", ")
	}
	if len(skipped) > 0 {
		var named []string
		for _, s := range skipped {
			named = append(named, s.Item+" ("+s.Status+")")
		}
		parts = append(parts, "skipped "+strings.Join(named, ", "))
	}
	if len(absent) > 0 {
		parts = append(parts, "not ticked: "+strings.Join(absent, ", "))
	}
	cmd := tea.Batch(m.waveOverlap(), m.planNext())
	m.actionStatus = "loaded " + view.Name + ": " + strings.Join(parts, "; ")
	return cmd
}

// planNext plans the next loaded item, one at a time: a plan reply asks for the
// next, so the one-plan-pending rule of the ticks holds.
func (m *Model) planNext() tea.Cmd {
	w := m.waves
	for len(w.planQueue) > 0 {
		id := w.planQueue[0]
		w.planQueue = w.planQueue[1:]
		p, ok := w.picked[id]
		if !ok {
			continue
		}
		w.planning = id
		w.planRequest++
		return m.planTicked(p)
	}
	return nil
}

// inHiddenWave reports whether the suggestion holds id in a wave that the table
// does not show yet.
func (w *wavesState) inHiddenWave(id string) bool {
	if w.suggest == nil {
		return false
	}
	for _, wave := range w.suggest.Waves[w.visibleWaves(w.suggest):] {
		for _, it := range wave.Items {
			if it.ID == id {
				return true
			}
		}
	}
	return false
}

// absentReason is why slicer's suggestion has no row for a startable item.
func (w *wavesState) absentReason(id string) string {
	s := w.suggest
	if s == nil {
		return "not in slicer's suggestion"
	}
	for _, b := range s.Blocked {
		if b.ID == id {
			if len(b.WaitingOn) == 0 {
				return "blocked"
			}
			return "blocked, waiting on " + strings.Join(b.WaitingOn, ", ")
		}
	}
	for _, h := range s.Held {
		if h.ID == id {
			if len(h.Flags) == 0 {
				return "held"
			}
			return "held: " + strings.Join(h.Flags, ", ")
		}
	}
	for _, u := range s.Unspecified {
		if u.ID == id {
			if len(u.Missing) == 0 {
				return "unspecified"
			}
			return "unspecified: missing " + strings.Join(u.Missing, ", ")
		}
	}
	return "not in slicer's suggestion"
}

// waveName is the saved name a launch of items carries: the loaded wave's, when
// the items are exactly its startable members, else none (NW-Q8).
func (w *wavesState) waveName(items []daemon.RunParams) string {
	if w.loaded == nil || len(items) != len(w.loaded.ids) {
		return ""
	}
	for _, p := range items {
		if !slices.Contains(w.loaded.ids, p.Item) {
			return ""
		}
	}
	return w.loaded.name
}

// savedListBody is the saved-wave list: one block per wave, its name on a rule
// and a row for each member with its id, status and title.
func (m *Model) savedListBody(width int) overviewView {
	w, th := m.waves, m.cfg.Theme
	l := w.list
	out := overviewView{sel: -1}
	out.body = append(out.body, th.Heading.Render(clip("Saved waves of "+w.project+"  (esc returns)", max(width, 1))))
	add := func(text string) {
		for _, line := range wrapText(text, max(width-2, 1)) {
			out.body = append(out.body, indent+line)
		}
	}
	switch {
	case l.pending:
		add("pending: wave.list")
		return out
	case l.err != nil:
		add(actionError(l.err))
		return out
	case len(l.waves) == 0:
		add("No saved waves for " + w.project + "; w saves the ticked items")
		return out
	}
	status := func(wave daemon.SavedWaveView, it daemon.WaveMember) string {
		if wave.Error != nil || it.Status == "" {
			return daemon.WaveStatusUnknown
		}
		return it.Status
	}
	idWidth, statusWidth := 1, 1
	for _, wave := range l.waves {
		for _, it := range wave.Items {
			idWidth = max(idWidth, lipgloss.Width(it.Item))
			statusWidth = max(statusWidth, lipgloss.Width(status(wave, it)))
		}
	}
	titleWidth := max(width-len(indent)-idWidth-2-statusWidth-2, 1)
	_, h := th.rules(m.cfg.ASCII)
	cursor := clampCursor(l.cursor, len(l.waves))
	for i, wave := range l.waves {
		noun := "items"
		if len(wave.Items) == 1 {
			noun = "item"
		}
		prefix := h + h + " " + wave.Name + " "
		suffix := fmt.Sprintf(" %d %s", len(wave.Items), noun)
		rule := prefix + strings.Repeat(h, max(width-lipgloss.Width(prefix)-lipgloss.Width(suffix), 1)) + suffix
		blockStart := len(out.body)
		out.rows = append(out.rows, blockStart)
		if i == cursor {
			out.sel = blockStart
			out.body = append(out.body, th.TabActive.Render(clip(rule, width)))
		} else {
			out.body = append(out.body, th.Group.Render(clip(rule, width)))
		}
		if wave.Error != nil {
			add(actionError(wave.Error))
		}
		for _, it := range wave.Items {
			st := status(wave, it)
			gap := strings.Repeat(" ", max(statusWidth-lipgloss.Width(st), 0))
			line := strings.TrimRight(indent+it.Item+strings.Repeat(" ", max(idWidth-lipgloss.Width(it.Item), 0))+"  "+st+gap+"  "+clip(it.Title, titleWidth), " ")
			switch st {
			case "done", "retired", daemon.WaveStatusMissing, daemon.WaveStatusUnknown:
				line = th.Dim.Render(line) // a member a start leaves out
			}
			out.body = append(out.body, line)
		}
		if _, height, _ := m.layoutSize(); i == cursor && len(out.body)-blockStart <= max(height-4-3, 1) {
			// Scroll to the block's last line, so the members that enter loads are
			// on screen with the rule; a block taller than the body keeps its rule.
			out.sel = len(out.body) - 1
		}
	}
	return out
}
