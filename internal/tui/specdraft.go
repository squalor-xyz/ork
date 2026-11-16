// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// specDrafter is optional for Live test doubles, as matrixEditor is (S179,
// *proposed*): the spec draft of the Roadmap item pane. The draft writes
// nothing; an approval replaces the sections the owner approves.
type specDrafter interface {
	SpecDraft(ctx context.Context, project, item string) (*daemon.SpecDraftResult, error)
	SpecDraftApply(context.Context, daemon.SpecDraftApplyParams) (*daemon.SpecDraftApplyResult, error)
}

var _ specDrafter = (*Daemon)(nil)

// specDraftState is the draft view that D opens in the item pane of a Roadmap
// item (owner decision 2026-11-16, ui.spec-draft-key): the spec as it is, the
// role's proposal for each section, and the one section the cursor is on. y asks
// with the command that approving the section runs; a second y runs it.
type specDraftState struct {
	project, item string
	request       uint64
	pending       bool // the draft call is out
	applying      bool // an approval is out
	confirming    bool // y asked: the command waits for y
	result        *daemon.SpecDraftResult
	err           error
	cursor        int
	applied       map[string]bool
	pane          *itemPane // the scroll state of the body (offset and room)
	heads         []int     // the body line of each section's heading, from the last draw
	follow        bool      // the cursor moved: the next draw scrolls its section to the top
}

// specDraftMsg is the daemon's reply to spec.draft.
type specDraftMsg struct {
	project, item string
	request       uint64
	result        *daemon.SpecDraftResult
	err           error
}

// specApplyMsg is the daemon's reply to spec.draft-apply for one section.
type specApplyMsg struct {
	project, item, section string
	request                uint64
	err                    error
}

// specDraftRefusal is why D does not open the draft view: the status line text,
// or "" for a silent refusal. The key refuses by it and the item pane's hint
// line lists D only when it does not.
func (m *Model) specDraftRefusal() (status string, refused bool) {
	pane := m.itemPane
	if m.view != roadmapView || pane == nil || pane.id == matrixPaneID || pane.pending || m.showHelp || m.specDraft != nil || m.active < 0 {
		return "", true
	}
	if m.cfg.Live == nil {
		return "No daemon connection; action unavailable", true
	}
	if _, ok := m.cfg.Live.(specDrafter); !ok {
		return "Spec drafting unavailable", true
	}
	return "", false
}

// openSpecDraft opens the draft view of the item pane's item and starts the
// draft in a command, as the item pane's read runs.
func (m *Model) openSpecDraft() tea.Cmd {
	if status, refused := m.specDraftRefusal(); refused {
		if status != "" {
			m.actionStatus = status
		}
		return nil
	}
	m.specDraftRequest++
	st := &specDraftState{project: m.itemPane.project, item: m.itemPane.id, request: m.specDraftRequest, pending: true, applied: map[string]bool{}, pane: &itemPane{}}
	m.specDraft = st
	m.actionStatus = ""
	drafter := m.cfg.Live.(specDrafter)
	return func() tea.Msg {
		result, err := drafter.SpecDraft(context.Background(), st.project, st.item)
		return specDraftMsg{project: st.project, item: st.item, request: st.request, result: result, err: err}
	}
}

// applySpecDraft takes the draft's reply. A reply to a view that is gone, or to
// an older one, changes nothing.
func (m *Model) applySpecDraft(msg specDraftMsg) {
	st := m.specDraft
	if st == nil || st.request != msg.request || st.project != msg.project || st.item != msg.item || !st.pending {
		return
	}
	st.pending = false
	st.result, st.err = msg.result, msg.err
	if st.err == nil && st.result == nil {
		st.err = fmt.Errorf("spec.draft returned no draft")
	}
	if st.err == nil {
		for i := range st.result.Sections {
			if st.approvable(i) == "" {
				st.cursor = i
				st.follow = true
				break
			}
		}
	}
}

// approvable is "" when section i can be approved, else why not.
func (st *specDraftState) approvable(i int) string {
	v := st.result.Sections[i]
	switch {
	case st.applied[v.Name]:
		return v.Name + " is already applied"
	case v.Unchanged:
		return "the draft leaves " + v.Name + " unchanged"
	case len(v.Problems) > 0:
		return v.Name + " has a problem and cannot be approved"
	}
	return ""
}

// ready reports whether the draft is loaded and the view takes section keys.
func (st *specDraftState) ready() bool {
	return !st.pending && !st.applying && st.err == nil && st.result != nil && len(st.result.Sections) > 0
}

// specDraftKey takes every key while the draft view is open, as matrixEditKey
// does. While a call is out only esc acts.
func (m *Model) specDraftKey(msg tea.KeyPressMsg) tea.Cmd {
	st := m.specDraft
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		if st.confirming {
			// esc at the question backs out of the question, as n does; it does not
			// throw away a draft that took a role run to make.
			st.confirming = false
			m.actionStatus = "Cancelled"
			return nil
		}
		m.specDraft = nil
		m.actionStatus = ""
		switch {
		case st.applying:
			m.actionStatus = "Closed; the section is written when the call ends"
		case st.pending:
			m.actionStatus = "Closed"
		}
		return nil
	}
	if !st.ready() {
		return nil
	}
	if st.confirming {
		switch msg.String() {
		case "y", "Y":
			return m.applySpecSection(st)
		case "n", "N", "enter":
			st.confirming = false
			m.actionStatus = "Cancelled"
		}
		return nil
	}
	m.actionStatus = ""
	switch {
	case key.Matches(msg, m.keys.TabNext):
		st.cursor = min(st.cursor+1, len(st.result.Sections)-1)
		st.follow = true
	case key.Matches(msg, m.keys.TabPrev):
		st.cursor = max(st.cursor-1, 0)
		st.follow = true
	case key.Matches(msg, m.keys.Yes):
		if why := st.approvable(st.cursor); why != "" {
			m.actionStatus = why
			return nil
		}
		st.confirming = true
	case key.Matches(msg, m.keys.RowNext, m.keys.RowPrev, m.keys.PageDown, m.keys.PageUp, m.keys.First, m.keys.Last):
		m.scrollRoadmapPane(msg)
	}
	return nil
}

// applySpecSection sends the approval of the section under the cursor, in a
// command.
func (m *Model) applySpecSection(st *specDraftState) tea.Cmd {
	drafter, ok := m.cfg.Live.(specDrafter)
	if !ok {
		st.confirming = false
		m.actionStatus = "Spec drafting unavailable"
		return nil
	}
	name := st.result.Sections[st.cursor].Name
	st.confirming, st.applying = false, true
	m.actionStatus = ""
	p := daemon.SpecDraftApplyParams{Project: st.project, Item: st.item, Sections: []string{name}}
	request := st.request
	return func() tea.Msg {
		_, err := drafter.SpecDraftApply(context.Background(), p)
		return specApplyMsg{project: p.Project, item: p.Item, section: name, request: request, err: err}
	}
}

// applySpecApply takes the reply of an approval. The section is marked applied
// in the view when it is still open, and the item pane reads the item again so
// that its text is the new one. The status says what happened even when the
// view was closed while the call was out.
func (m *Model) applySpecApply(msg specApplyMsg) tea.Cmd {
	st := m.specDraft
	if st != nil && st.request == msg.request && st.project == msg.project && st.item == msg.item {
		st.applying = false
	} else {
		st = nil
	}
	if msg.err != nil {
		m.actionStatus = actionError(msg.err)
		return nil
	}
	if st != nil {
		st.applied[msg.section] = true
	}
	m.actionStatus = "Applied " + msg.section + " of " + msg.item + " (uncommitted)"
	return m.reloadItem(msg.project, msg.item)
}

// reloadItem reads the item pane's item again after its spec changed. The pane
// keeps its old text and offset until the reply replaces them.
func (m *Model) reloadItem(project, id string) tea.Cmd {
	pane := m.itemPane
	if pane == nil || pane.project != project || pane.id != id || m.cfg.Live == nil {
		return nil
	}
	m.itemRequest++
	pane.request = m.itemRequest
	live, request := m.cfg.Live, pane.request
	return func() tea.Msg {
		detail, err := live.ItemShow(context.Background(), project, id)
		return itemShowMsg{project: project, id: id, request: request, detail: detail, err: err}
	}
}

// specDraftLines draws the draft: for each section its name and state, the
// text the slice has now, and the proposal. A section with a problem shows it.
func (m *Model) specDraftLines(out *overviewView, st *specDraftState, w int) {
	th := m.cfg.Theme
	add := func(prefix, text string, render func(string) string) {
		first := true
		for _, para := range strings.Split(text, "\n") {
			for _, line := range wrapText(para, max(w-lenIndent()-len(prefix), 1)) {
				lead := prefix
				if !first {
					lead = strings.Repeat(" ", len(prefix))
				}
				first = false
				out.body = append(out.body, indent+lead+render(line))
			}
		}
	}
	plain := func(s string) string { return s }
	dim := func(s string) string { return th.Dim.Render(s) }
	warn := func(s string) string { return th.Warn.Render(s) }
	base := len(out.body)
	st.heads = st.heads[:0]
	for i, v := range st.result.Sections {
		st.heads = append(st.heads, len(out.body)-base)
		marker := "  "
		if i == st.cursor {
			marker = "> "
		}
		state := ""
		switch {
		case st.applied[v.Name]:
			state = "  applied"
		case v.Unchanged:
			state = "  unchanged"
		case len(v.Problems) > 0:
			state = "  problem"
		}
		head := marker + v.Name
		if i == st.cursor {
			head = th.Heading.Render(head)
		}
		if state != "" {
			head += th.Dim.Render(state)
		}
		out.body = append(out.body, head)
		if v.Unchanged {
			continue
		}
		current := v.Current
		if current == "" {
			current = "(empty)"
		}
		add("Current:  ", current, dim)
		add("Proposed: ", v.Text, plain)
		for _, problem := range v.Problems {
			add("Problem:  ", problem, warn)
		}
	}
	if text := strings.TrimSpace(st.result.OwnerNeeded); text != "" {
		out.body = append(out.body, th.Heading.Render("Owner needed"))
		add("", text, plain)
	}
	// A cursor that moved brings its section to the top of the scrolling part;
	// the draw clamps an offset past the end (scrollItemPane).
	if st.follow && st.cursor < len(st.heads) {
		st.pane.offset = st.heads[st.cursor]
		st.follow = false
	}
}

// specDraftCommandLines is how many lines of the command the question shows.
const specDraftCommandLines = 4

// lenIndent is the width of the body's indent.
func lenIndent() int { return len(indent) }

// specDraftView is the status area of the draft view: the command that waits
// for y, what is pending, and the status line.
func (m *Model) specDraftView(width int) string {
	st := m.specDraft
	width = max(width, 1)
	var out []string
	if st.confirming && st.ready() {
		// A long section makes a long command. The status area is cut to the
		// screen, so the command is capped and the question always shows; the
		// whole text is the Proposed text above.
		cmd := wrapText("Run: "+st.result.Sections[st.cursor].Command, width)
		if len(cmd) > specDraftCommandLines {
			more := len(cmd) - (specDraftCommandLines - 1)
			cmd = append(cmd[:specDraftCommandLines-1:specDraftCommandLines-1], m.cfg.Theme.Dim.Render(fmt.Sprintf("+%d more lines of the command", more)))
		}
		out = append(out, cmd...)
		out = append(out, "[y/N] (Enter is No)")
	}
	if st.applying {
		out = append(out, "pending: spec.draft-apply")
	}
	if m.actionStatus != "" {
		out = append(out, m.actionStatus)
	}
	return strings.Join(out, "\n")
}
