// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

var lineupRoles = [...]string{"implementer", "reviewer", "architect"}

type lineupEditor struct {
	item    string
	plan    *daemon.RunPlan
	chains  [len(lineupRoles)][]runrec.LineupEntry
	choice  [len(lineupRoles)]int
	budgets [2]textField
	row     int
	custom  bool
	fields  [3]textField
	focus   int
}

func (m *Model) lineupRefusal() (string, bool) {
	w := m.waves
	if m.view != wavesView || m.showHelp || w == nil || w.pending || w.starting {
		return "Wait for the plan before editing", true
	}
	if w.aggregate {
		return "Select a project to edit a lineup", true
	}
	rows := w.rows()
	if len(rows) == 0 {
		return "Tick the item before editing its lineup", true
	}
	p, ok := w.picked[rows[clampCursor(w.cursor, len(rows))].ID]
	if !ok {
		return "Tick the item before editing its lineup", true
	}
	if w.planning != "" || p.Expected == nil {
		return "Wait for the plan before editing", true
	}
	return "", false
}

func (m *Model) openLineup() tea.Cmd {
	if m.view != wavesView || m.showHelp {
		return nil
	}
	if status, refused := m.lineupRefusal(); refused {
		m.actionStatus = status
		return nil
	}
	w := m.waves
	rows := w.rows()
	p := w.picked[rows[clampCursor(w.cursor, len(rows))].ID]

	e := &lineupEditor{item: p.Item, plan: p.Expected}
	for i, role := range lineupRoles {
		e.chains[i] = append([]runrec.LineupEntry(nil), p.Expected.Lineup[role]...)
	}
	e.budgets[0].insert(strconv.Itoa(p.Expected.Budgets.Minutes))
	e.budgets[1].insert(strconv.Itoa(p.Expected.Budgets.Tokens))
	w.lineup = e
	m.actionStatus = ""
	return nil
}

func (m *Model) lineupKey(msg tea.KeyPressMsg) tea.Cmd {
	e := m.waves.lineup
	m.actionStatus = ""
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	if msg.String() == "esc" {
		m.waves.lineup = nil
		return nil
	}
	if e.custom {
		switch msg.String() {
		case "tab":
			e.focus = (e.focus + 1) % 3
		case "shift+tab":
			e.focus = (e.focus + 2) % 3
		case "enter":
			entry := runrec.LineupEntry{Vendor: e.fields[0].Value(), Model: e.fields[1].Value(), Effort: e.fields[2].Value()}
			if len(e.chains[e.row]) == 0 {
				e.chains[e.row] = []runrec.LineupEntry{entry}
			} else {
				e.chains[e.row][0] = entry
			}
			e.custom = false
		default:
			e.fields[e.focus].update(msg)
		}
		return nil
	}
	switch msg.String() {
	case "down", "tab":
		e.row = (e.row + 1) % (len(lineupRoles) + len(e.budgets))
	case "up", "shift+tab":
		e.row = (e.row + len(lineupRoles) + len(e.budgets) - 1) % (len(lineupRoles) + len(e.budgets))
	case "left", "right":
		if e.row >= len(lineupRoles) {
			e.budgets[e.row-len(lineupRoles)].update(msg)
			break
		}
		chain := e.plan.Lineup[lineupRoles[e.row]]
		if len(chain) == 0 {
			break
		}
		delta := 1
		if msg.String() == "left" {
			delta = -1
		}
		e.choice[e.row] = (e.choice[e.row] + delta + len(chain)) % len(chain)
		choice := e.choice[e.row]
		e.chains[e.row] = append([]runrec.LineupEntry{chain[choice]}, chain[:choice]...)
		e.chains[e.row] = append(e.chains[e.row], chain[choice+1:]...)
	case "d":
		if e.row < len(lineupRoles) && lineupRoles[e.row] == "architect" {
			e.chains[e.row] = nil
			e.choice[e.row] = 0
		}
	case "c":
		if e.row < len(lineupRoles) {
			entry := runrec.LineupEntry{}
			if len(e.chains[e.row]) > 0 {
				entry = e.chains[e.row][0]
			}
			e.fields = [3]textField{}
			for i, value := range []string{entry.Vendor, entry.Model, entry.Effort} {
				e.fields[i].insert(value)
			}
			e.custom, e.focus = true, 0
		}
	case "enter":
		return m.saveLineup()
	default:
		if e.row >= len(lineupRoles) {
			e.budgets[e.row-len(lineupRoles)].update(msg)
		}
	}
	return nil
}

func (m *Model) saveLineup() tea.Cmd {
	w, e := m.waves, m.waves.lineup
	var values [2]int
	for i := range values {
		value, err := strconv.Atoi(e.budgets[i].Value())
		if err != nil || value < 0 {
			e.row = i + len(lineupRoles)
			m.actionStatus = "Budget must be a whole number of zero or more"
			return nil
		}
		values[i] = value
	}
	p := w.picked[e.item]
	p.Lineup = maps.Clone(p.Lineup)
	for i, role := range lineupRoles {
		if !reflect.DeepEqual(e.chains[i], e.plan.Lineup[role]) {
			if p.Lineup == nil {
				p.Lineup = map[string][]runrec.LineupEntry{}
			}
			p.Lineup[role] = append([]runrec.LineupEntry(nil), e.chains[i]...)
		}
	}
	budgets := registry.Budgets{Minutes: values[0], Tokens: values[1]}
	if budgets != e.plan.Budgets {
		p.Budgets = &budgets
	}
	w.lineup = nil
	if reflect.DeepEqual(p, w.picked[e.item]) {
		return nil
	}
	w.planning = e.item
	w.planRequest++
	live, ctx, request, seq := m.cfg.Live, w.ctx, w.request, w.planRequest
	return func() tea.Msg {
		if ctx.Err() != nil {
			return nil
		}
		plan, err := live.Plan(ctx, p)
		return wavesMsg{request: request, kind: "plan", sequence: seq, item: p.Item, plan: plan, params: &p, err: err}
	}
}

func (m *Model) lineupBody(width int) overviewView {
	e, th := m.waves.lineup, m.cfg.Theme
	out := overviewView{sel: -1}
	add := func(s string) {
		for _, line := range wrapText(s, max(width-2, 1)) {
			out.body = append(out.body, indent+line)
		}
	}
	title := ""
	for _, item := range m.waves.rows() {
		if item.ID == e.item {
			title = item.Title
			break
		}
	}
	out.body = append(out.body, th.Heading.Render("Edit lineup: "+e.item+"  "+title))
	add("Run only; gate follows reviewer.")
	for i, role := range lineupRoles {
		entry := runrec.LineupEntry{}
		if len(e.chains[i]) > 0 {
			entry = e.chains[i][0]
		}
		prefix := "  "
		focused := e.row == i && !e.custom
		if focused {
			prefix = "> "
			out.sel = len(out.body)
		}
		label := role
		if !reflect.DeepEqual(e.chains[i], e.plan.Lineup[role]) {
			label += "*"
		}
		value := entry.Vendor + " " + entry.Model + " " + entry.Effort
		if role == "architect" && len(e.chains[i]) == 0 {
			value = "off"
		}
		if e.custom && e.row == i {
			value += " (editing)"
		} else if focused && len(e.chains[i]) > 0 {
			if n := len(e.plan.Lineup[role]); n > 1 {
				value = fmt.Sprintf("< %s >  %d/%d", value, e.choice[i]+1, n)
			} else {
				value += " (no alternatives)"
			}
		}
		start := len(out.body)
		add(prefix + label + ": " + value)
		for _, fallback := range e.chains[i][min(1, len(e.chains[i])):] {
			add(th.Dim.Render("    fallback: " + fallback.Vendor + " " + fallback.Model + " " + fallback.Effort))
		}
		if focused {
			for j := start; j < len(out.body); j++ {
				out.body[j] = th.TabActive.Render(out.body[j])
			}
		}
	}
	if chain := e.plan.Lineup["ui"]; len(chain) > 0 {
		var rows []string
		for _, entry := range chain {
			rows = append(rows, entry.Vendor+" "+entry.Model+" "+entry.Effort)
		}
		add("  ui: " + strings.Join(rows, " -> ") + " (UI slices only; from the matrix)")
	}
	for i, label := range []string{"Budget minutes", "Budget tokens"} {
		prefix := "  "
		if e.row == i+len(lineupRoles) {
			prefix = "> "
			out.sel = len(out.body)
		}
		text := e.budgets[i].Value()
		if e.row == i+len(lineupRoles) {
			text = e.budgets[i].view(max(width-len(label)-6, 1), th)
		}
		add(prefix + label + ": " + text)
	}
	add("Zero budget uses the project default.")
	if e.custom {
		add("Custom " + lineupRoles[e.row] + ":")
		for i, label := range []string{"Vendor", "Model", "Effort"} {
			prefix, value := "  ", e.fields[i].Value()
			if e.focus == i {
				prefix = "> "
				value = e.fields[i].view(max(width-len(label)-6, 1), th)
				out.sel = len(out.body)
			}
			add(fmt.Sprintf("%s%s: %s", prefix, label, value))
		}
	}
	return out
}
