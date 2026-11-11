// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// matrixEditor is optional for Live test doubles, as itemFiler is (S226,
// *proposed*): the role matrix editor of the Roadmap's matrix pane.
type matrixEditor interface {
	MatrixEdit(context.Context, daemon.MatrixEditParams) (*daemon.MatrixEditResult, error)
}

var _ matrixEditor = (*Daemon)(nil)

// The ops of matrix.edit and the line names of the form. A line named for a row
// field (agent, vendor, model, reasoning, note, role) sends that flag.
const (
	editSet      = "set"
	editAdd      = "add"
	editRemove   = "remove"
	editMove     = "move"
	editRuleEdit = "rule-edit"

	lineID     = "id"     // the row id or the rule id
	lineBefore = "before" // the row a moved row goes before
	lineText   = "text"   // the new text of a rule
)

// editField is one line of the form: its name, its label and its input.
type editField struct {
	name, label string
	input       textField
}

// matrixEditState is the form of one edit of the role matrix (S226,
// *proposed*): the lines of its op, the line the cursor is on, and, after the
// form is sent, the daemon's preview of the command to confirm.
type matrixEditState struct {
	op, project string
	lines       []editField
	focus       int
	request     uint64
	pending     bool // a preview or a run is out
	running     bool // the call that is out is the run, not the preview
	confirming  bool // the preview is back: the command waits for y
	params      daemon.MatrixEditParams
	command     string
	warning     string
	textEdited  bool // the owner typed in the rule's text line
}

// closedMatrixRun is a run whose form was closed with esc while the run was
// out: what its reply needs to show the edit that slicer applied.
type closedMatrixRun struct{ project, op, id string }

// matrixEditMsg is the daemon's reply to a preview or a run of matrix.edit.
type matrixEditMsg struct {
	project string
	request uint64
	run     bool
	result  *daemon.MatrixEditResult
	err     error
}

// rowFieldLines are the lines that follow the row id of an edit and the role of
// an add: the fields the form offers. The row's conditions and clearing a field
// stay in `slicer relay-roles`.
var rowFieldLines = []editField{
	{name: "agent", label: "Agent"},
	{name: "vendor", label: "Vendor"},
	{name: "model", label: "Model"},
	{name: "reasoning", label: "Reasoning"},
	{name: "note", label: "Note"},
}

// matrixEditOp is the op a key starts in the matrix pane, or "".
func (m *Model) matrixEditOp(msg tea.KeyPressMsg) string {
	k := m.keys
	switch {
	case key.Matches(msg, k.MatrixEdit):
		return editSet
	case key.Matches(msg, k.MatrixAdd):
		return editAdd
	case key.Matches(msg, k.MatrixRemove):
		return editRemove
	case key.Matches(msg, k.MatrixMove):
		return editMove
	case key.Matches(msg, k.MatrixRule):
		return editRuleEdit
	}
	return ""
}

// matrixEditRefusal is why the edit keys do not open the form: the status line
// text, or "" for a silent refusal. The keys refuse by it and the hint line
// lists them only when it does not.
func (m *Model) matrixEditRefusal() (status string, refused bool) {
	switch {
	case !m.matrixPaneOpen() || m.showHelp || m.matrixEdit != nil:
		return "", true
	case m.cfg.Live == nil:
		return "No daemon connection; action unavailable", true
	}
	if _, ok := m.cfg.Live.(matrixEditor); !ok {
		return "Matrix editing unavailable", true
	}
	if m.itemPane.reloading {
		return "Matrix reloading; editing unavailable", true
	}
	// A pane that waits for its first reply has no matrix yet.
	if pane := m.itemPane; pane.err != nil || pane.matrix == nil {
		return "Matrix not loaded; editing unavailable", true
	}
	return "", false
}

// openMatrixEdit opens the form of op on the pane's loaded matrix.
func (m *Model) openMatrixEdit(op string) tea.Cmd {
	if status, refused := m.matrixEditRefusal(); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	var lines []editField
	switch op {
	case editSet:
		lines = append([]editField{{name: lineID, label: "Row id"}}, rowFieldLines...)
	case editAdd:
		lines = append([]editField{{name: "role", label: "Role"}}, rowFieldLines...)
	case editRemove:
		lines = []editField{{name: lineID, label: "Row id"}}
	case editMove:
		lines = []editField{{name: lineID, label: "Row id"}, {name: lineBefore, label: "Before"}}
	case editRuleEdit:
		lines = []editField{{name: lineID, label: "Rule id"}, {name: lineText, label: "Text"}}
	}
	m.matrixEditRequest++
	m.matrixEdit = &matrixEditState{op: op, project: m.itemPane.project, lines: lines, request: m.matrixEditRequest}
	m.actionStatus = ""
	return nil
}

// matrixEditKey takes every key while the form is open, as composeKey does.
// While a preview or a run is out only esc acts.
func (m *Model) matrixEditKey(msg tea.KeyPressMsg) tea.Cmd {
	e := m.matrixEdit
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.matrixEdit = nil
		m.actionStatus = "Cancelled"
		if e.pending {
			m.actionStatus = "Closed"
		}
		if e.running {
			// Slicer may apply the edit: the run's reply is still taken (applyMatrixEdit).
			if m.closedRuns == nil {
				m.closedRuns = map[uint64]closedMatrixRun{}
			}
			m.closedRuns[e.request] = closedMatrixRun{project: e.project, op: e.op, id: e.params.ID}
			m.actionStatus = "Closed; the matrix reloads when the edit ends"
		}
		return nil
	}
	if e.pending {
		return nil
	}
	if e.confirming {
		switch msg.String() {
		case "y", "Y":
			e.pending, e.running = true, true
			m.actionStatus = ""
			return m.callMatrixEdit(e, e.params, true) // the params of the preview, without Preview
		case "n", "N", "enter":
			m.matrixEdit = nil
			m.actionStatus = "Cancelled"
		}
		return nil
	}
	m.actionStatus = ""
	switch msg.String() {
	case "tab":
		m.moveEditFocus(1)
	case "shift+tab":
		m.moveEditFocus(-1)
	case "enter":
		if e.focus < len(e.lines)-1 {
			m.moveEditFocus(1)
			return nil
		}
		return m.previewMatrixEdit()
	case "ctrl+s":
		return m.previewMatrixEdit()
	default:
		e.update(msg)
	}
	return nil
}

// update gives a key or a paste to the line under the cursor.
func (e *matrixEditState) update(msg tea.Msg) {
	if e.pending || e.confirming {
		return
	}
	if e.lines[e.focus].name == lineText {
		e.textEdited = true
	}
	e.lines[e.focus].input.update(msg)
}

// moveEditFocus moves the cursor by delta lines, round the ends. A rule's text
// line the owner has not typed in takes the text of the rule whose id is typed.
func (m *Model) moveEditFocus(delta int) {
	e := m.matrixEdit
	e.focus = (e.focus + delta + len(e.lines)) % len(e.lines)
	if e.op != editRuleEdit || e.lines[e.focus].name != lineText || e.textEdited {
		return
	}
	if rule, ok := m.matrixRule(strings.TrimSpace(e.lines[0].input.Value())); ok {
		r := []rune(rule.Text)
		e.lines[e.focus].input = textField{text: r, cursor: len(r)}
	}
}

func (m *Model) matrixRule(id string) (slicercli.Rule, bool) {
	if pane := m.itemPane; pane != nil && pane.matrix != nil {
		for _, rule := range pane.matrix.Rules {
			if rule.ID == id {
				return rule, true
			}
		}
	}
	return slicercli.Rule{}, false
}

func (m *Model) matrixRow(id string) (daemon.MatrixRow, bool) {
	if pane := m.itemPane; pane != nil && pane.matrix != nil {
		for _, row := range pane.matrix.Rows {
			if row.ID == id {
				return row, true
			}
		}
	}
	return daemon.MatrixRow{}, false
}

// matrixEditParams checks the form against the loaded matrix and returns the
// params of the call. When a line is wrong it returns the status that says
// why and moves the cursor to that line.
func (m *Model) matrixEditParams(e *matrixEditState) (p daemon.MatrixEditParams, status string) {
	value := func(i int) string { return strings.TrimSpace(e.lines[i].input.Value()) }
	fail := func(line int, text string) (daemon.MatrixEditParams, string) {
		e.focus = line
		return p, text
	}
	p = daemon.MatrixEditParams{Project: e.project, Op: e.op}
	if e.op != editAdd {
		id := value(0)
		switch {
		case id == "" && e.op == editRuleEdit:
			return fail(0, "Rule id must not be empty")
		case id == "":
			return fail(0, "Row id must not be empty")
		case e.op == editRuleEdit:
			if _, ok := m.matrixRule(id); !ok {
				return fail(0, "no rule "+id)
			}
		default:
			if _, ok := m.matrixRow(id); !ok {
				return fail(0, "no row "+id)
			}
		}
		p.ID = id
	}
	switch e.op {
	case editAdd, editSet:
		p.Fields = slicercli.RowFields{}
		for i, line := range e.lines {
			if v := value(i); line.name != lineID && v != "" {
				p.Fields[line.name] = v
			}
		}
		if e.op == editAdd && p.Fields["role"] == "" {
			return fail(0, "Role must not be empty")
		}
		if e.op == editSet && len(p.Fields) == 0 {
			return fail(1, "Nothing to change: type a new value on a line")
		}
	case editMove:
		if before := value(1); before == "" {
			p.Last = true
		} else if _, ok := m.matrixRow(before); !ok {
			return fail(1, "no row "+before)
		} else {
			p.Before = before
		}
	case editRuleEdit:
		if p.Text = value(1); p.Text == "" {
			return fail(1, "Rule text must not be empty")
		}
	}
	return p, ""
}

// previewMatrixEdit checks the form and asks the daemon for the command. The
// call runs in the command, never in Update.
func (m *Model) previewMatrixEdit() tea.Cmd {
	e := m.matrixEdit
	p, status := m.matrixEditParams(e)
	if status != "" {
		m.actionStatus = status
		return nil
	}
	e.params = p
	p.Preview = true
	e.pending = true
	return m.callMatrixEdit(e, p, false)
}

// callMatrixEdit is the command of one matrix.edit call; its reply carries the
// form's request id, so that a reply to a form that is gone changes nothing.
func (m *Model) callMatrixEdit(e *matrixEditState, p daemon.MatrixEditParams, run bool) tea.Cmd {
	editor, ok := m.cfg.Live.(matrixEditor)
	if !ok {
		e.pending = false
		m.actionStatus = "Matrix editing unavailable"
		return nil
	}
	project, request := e.project, e.request
	return func() tea.Msg {
		result, err := editor.MatrixEdit(context.Background(), p)
		return matrixEditMsg{project: project, request: request, run: run, result: result, err: err}
	}
}

// applyMatrixEdit takes the reply of a preview (the command waits for y) or of
// a run (the matrix is read again). Slicer's refusal shows as it came and the
// form stays for a change.
func (m *Model) applyMatrixEdit(msg matrixEditMsg) tea.Cmd {
	if closed, ok := m.closedRuns[msg.request]; ok && msg.run && closed.project == msg.project {
		// The owner pressed esc while this run was out: slicer may have applied it.
		delete(m.closedRuns, msg.request)
		if msg.err != nil {
			m.actionStatus = actionError(msg.err)
			return nil
		}
		m.actionStatus = editedStatus(closed.op, closed.id, msg.result)
		if pane := m.itemPane; pane != nil && pane.project == msg.project {
			return m.reloadMatrix()
		}
		return nil
	}
	e := m.matrixEdit
	if e == nil || e.request != msg.request || e.project != msg.project || !e.pending {
		return nil
	}
	e.pending, e.running = false, false
	switch {
	case msg.err != nil:
		e.confirming = false
		m.actionStatus = actionError(msg.err)
	case !msg.run:
		if msg.result == nil || msg.result.Command == "" {
			m.actionStatus = "matrix.edit returned no command"
			return nil
		}
		e.confirming, e.command, e.warning = true, msg.result.Command, msg.result.Warning
		m.actionStatus = ""
	default:
		m.matrixEdit = nil
		m.actionStatus = editedStatus(e.op, e.params.ID, msg.result)
		return m.reloadMatrix()
	}
	return nil
}

// editedStatus is the status after slicer applied an edit; the change is left
// uncommitted in the project.
func editedStatus(op, id string, result *daemon.MatrixEditResult) string {
	if result != nil && result.ID != "" {
		id = result.ID
	}
	verb := map[string]string{editAdd: "Added", editRemove: "Removed", editMove: "Moved"}[op]
	if verb == "" {
		verb = "Edited"
	}
	return strings.TrimSpace(verb+" "+id) + " (uncommitted)"
}

// reloadMatrix reads roles.matrix again as openMatrix does. The pane keeps its
// old text and its offset until the reply replaces them.
func (m *Model) reloadMatrix() tea.Cmd {
	pane := m.itemPane
	if !m.matrixPaneOpen() || m.cfg.Live == nil {
		return nil
	}
	m.itemRequest++
	pane.request, pane.reloading = m.itemRequest, true
	live, project, request := m.cfg.Live, pane.project, pane.request
	return func() tea.Msg {
		result, err := live.RolesMatrix(context.Background(), project)
		return matrixMsg{project: project, request: request, result: result, err: err}
	}
}

func (e *matrixEditState) title() string {
	switch e.op {
	case editAdd:
		return "Add row:"
	case editRemove:
		return "Remove row:"
	case editMove:
		return "Move row:"
	case editRuleEdit:
		return "Edit rule:"
	}
	return "Edit row:"
}

// editHint is the dim text beside line i: the value the row has now, or what an
// empty line does.
func (m *Model) editHint(e *matrixEditState, i int) string {
	name := e.lines[i].name
	switch {
	case name == lineBefore:
		return "empty: last"
	case e.op != editSet || name == lineID:
		return ""
	}
	row, ok := m.matrixRow(strings.TrimSpace(e.lines[0].input.Value()))
	if !ok {
		return ""
	}
	var now string
	switch name {
	case "agent":
		now = row.Agent
	case "vendor":
		now = row.Vendor
	case "model":
		now = row.Model
	case "reasoning":
		now = row.Reasoning
	case "note":
		now = row.Note
	}
	if now == "" {
		return ""
	}
	return "now: " + now
}

// matrixEditView is the status area of the form: the title and one line for each
// field, or, once the preview is back, the command, the warning and the
// question. Every line fits width, so the status area does not wrap it.
func (m *Model) matrixEditView(width int) string {
	th := m.cfg.Theme
	e := m.matrixEdit
	width = max(width, 1)
	var out []string
	if e.confirming {
		out = append(out, wrapText("Run: "+e.command, width)...)
		if e.warning != "" {
			for _, line := range wrapText(e.warning, width) {
				out = append(out, th.Warn.Bold(th.Mono).Render(line))
			}
		}
		out = append(out, "[y/N] (Enter is No)")
	} else {
		out = append(out, th.Heading.Render(e.title()))
		labelW := 0
		for _, line := range e.lines {
			labelW = max(labelW, len(line.label)+1)
		}
		for i, line := range e.lines {
			marker := "  "
			if i == e.focus {
				marker = "> "
			}
			label := line.label + ":"
			head := marker + label + strings.Repeat(" ", labelW-len(label)) + " "
			room := max(width-lipgloss.Width(head), 1)
			now := m.editHint(e, i)
			valueW, nowW := room, 0
			if now != "" {
				valueW = max(min(24, room/2), 1)
				nowW = max(room-valueW-2, 0)
			}
			value := clip(line.input.Value(), valueW)
			if i == e.focus {
				value = line.input.view(valueW, th)
			}
			text := head + value
			if nowW > 0 {
				pad := strings.Repeat(" ", max(valueW-lipgloss.Width(value), 0))
				text += pad + "  " + th.Dim.Render(clip(now, nowW))
			}
			out = append(out, text)
		}
	}
	if e.pending {
		out = append(out, "pending: matrix.edit")
	}
	if m.actionStatus != "" {
		out = append(out, m.actionStatus)
	}
	return strings.Join(out, "\n")
}
