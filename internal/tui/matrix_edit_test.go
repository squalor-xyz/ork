// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// matrixEditLive is a matrixLive that also edits the matrix, as the optional
// matrixEditor interface says, and records every matrix.edit call. It also
// files slices and notes, so the letters keep their meanings off the pane.
type matrixEditLive struct {
	matrixLive
	calls   []daemon.MatrixEditParams
	command string // the preview's command
	warning string // the preview's warning
	newID   string // the id a run of add answers
	perr    error  // the preview's error
	rerr    error  // the run's error
	after   *daemon.RolesMatrixResult
}

func (l *matrixEditLive) MatrixEdit(_ context.Context, p daemon.MatrixEditParams) (*daemon.MatrixEditResult, error) {
	l.calls = append(l.calls, p)
	if p.Preview {
		if l.perr != nil {
			return nil, l.perr
		}
		return &daemon.MatrixEditResult{Command: l.command, Warning: l.warning}, nil
	}
	if l.rerr != nil {
		return nil, l.rerr
	}
	if l.after != nil {
		l.result = l.after
	}
	return &daemon.MatrixEditResult{Command: l.command, ID: l.newID, Done: true}, nil
}

func (l *matrixEditLive) ItemNew(context.Context, daemon.ItemNewParams) (daemon.ItemNewResult, error) {
	return daemon.ItemNewResult{}, nil
}

func (l *matrixEditLive) ItemNote(context.Context, string, string, string) error { return nil }

// previews and runs split the recorded calls.
func (l *matrixEditLive) previews() (n int) {
	for _, c := range l.calls {
		if c.Preview {
			n++
		}
	}
	return n
}

// editMatrix is a matrix with the rows r5, r2 and r7 and the rules u3 and u10;
// pad lines of slicer's text follow, so that the pane scrolls.
func editMatrix(pad int, marker string) *daemon.RolesMatrixResult {
	lines := []string{"# Relay roles", "", marker}
	for i := 0; i < pad; i++ {
		lines = append(lines, fmt.Sprintf("pad line %02d", i))
	}
	return &daemon.RolesMatrixResult{
		Markdown: strings.Join(lines, "\n") + "\n",
		Rows: []daemon.MatrixRow{
			{ID: "r5", Role: "implementer", Agent: "Codex", Vendor: "OpenAI", Model: "gpt-6.1-sol", Reasoning: "medium", Note: "default implementer", Use: "used as codex"},
			{ID: "r2", Role: "reviewer", Agent: "Claude Code", Vendor: "Claude", Model: "opus", Reasoning: "high", Use: "used as claude"},
			{ID: "r7", Role: "gate", Agent: "Claude Code", Use: "not used: the gate runs the reviewer's lineup"},
		},
		Rules: []slicercli.Rule{
			{ID: "u3", Text: "A reviewer never wrote the code it reviews."},
			{ID: "u10", Text: "The UI pass row runs on every size."},
		},
	}
}

func newMatrixEditLive(pad int) *matrixEditLive {
	return &matrixEditLive{
		matrixLive: matrixLive{result: editMatrix(pad, "marker before")},
		command:    "slicer relay-roles set r5 --model=opus --render",
	}
}

// matrixEditHarness opens the matrix pane of the Roadmap with l and loads its reply.
func matrixEditHarness(t *testing.T, l *matrixEditLive) *harness {
	t.Helper()
	h := roadmapHarness(t)
	h.m.cfg.Live = l
	_, cmd := h.m.Update(press("m"))
	if cmd == nil {
		t.Fatal("m returned no command")
	}
	h.m.Update(cmd())
	return h
}

var ctrlS = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}

// preview sends ctrl+s, checks that Update made no call, and runs the command.
func preview(t *testing.T, h *harness, l *matrixEditLive) {
	t.Helper()
	before := len(l.calls)
	_, cmd := h.m.Update(ctrlS)
	if cmd == nil || len(l.calls) != before {
		t.Fatalf("the call must run in the command, not in Update: cmd %v, calls %d, were %d", cmd != nil, len(l.calls), before)
	}
	h.m.Update(cmd())
}

func TestMatrixEditKeysOpenOnlyOnALoadedMatrix(t *testing.T) {
	titles := map[string]string{"e": "Edit row", "n": "Add row", "d": "Remove row", "m": "Move row", "r": "Edit rule"}
	for k, title := range titles {
		l := newMatrixEditLive(0)
		h := matrixEditHarness(t, l)
		h.deliver(press(k))
		if v := h.view(); !strings.Contains(v, title) {
			t.Errorf("%s did not open %q:\n%s", k, title, v)
		}
		if len(l.calls) != 0 {
			t.Errorf("%s made a call", k)
		}
		h.deliver(press("esc"))
		if h.m.itemPane == nil || strings.Contains(h.view(), title) || !strings.Contains(h.view(), "Role matrix") {
			t.Errorf("%s: the first esc must cancel the edit and keep the pane:\n%s", k, h.view())
		}
		h.deliver(press("esc"))
		if h.m.itemPane != nil {
			t.Errorf("%s: the second esc did not close the pane", k)
		}
	}

	refused := func(name, status string, setup func(*harness, *matrixEditLive)) {
		t.Run(name, func(t *testing.T) {
			l := newMatrixEditLive(0)
			h := matrixEditHarness(t, l)
			setup(h, l)
			for k := range titles {
				h.m.actionStatus = ""
				_, cmd := h.m.Update(press(k))
				v := h.view()
				if cmd != nil || len(l.calls) != 0 || h.m.itemPane == nil {
					t.Errorf("%s: cmd %v, calls %d", k, cmd != nil, len(l.calls))
				}
				for _, title := range titles {
					if strings.Contains(v, title) {
						t.Errorf("%s opened %q:\n%s", k, title, v)
					}
				}
				if h.m.actionStatus != status {
					t.Errorf("%s: status %q, want %q", k, h.m.actionStatus, status)
				}
			}
		})
	}
	refused("reply pending", "Matrix not loaded; editing unavailable", func(h *harness, l *matrixEditLive) {
		h.deliver(press("esc"))
		h.m.Update(press("m")) // the reply is not delivered
	})
	refused("failed read", "Matrix not loaded; editing unavailable", func(h *harness, l *matrixEditLive) {
		h.deliver(press("esc"))
		l.merr = &ipc.Error{Code: "usage", Message: "matrix unreadable"}
		_, cmd := h.m.Update(press("m"))
		h.m.Update(cmd())
	})
	refused("no daemon", "No daemon connection; action unavailable", func(h *harness, l *matrixEditLive) { h.m.cfg.Live = nil })
	refused("no editor", "Matrix editing unavailable", func(h *harness, l *matrixEditLive) { h.m.cfg.Live = &l.matrixLive })

	// Off the matrix pane the letters keep their meanings.
	l := newMatrixEditLive(0)
	h := roadmapHarness(t)
	h.m.cfg.Live = l
	h.deliver(press("n"))
	if v := h.view(); !strings.Contains(v, "New slice:") || strings.Contains(v, "Add row") {
		t.Errorf("n on the Roadmap did not open the new slice form:\n%s", v)
	}
	h.deliver(press("esc"))
	h.deliver(press("N"))
	if v := h.view(); !strings.Contains(v, "Note for") {
		t.Errorf("N on the Roadmap did not open the note form:\n%s", v)
	}
}

func TestMatrixEditAsksFieldsOneLineEachAndSendsOnlyFilledOnes(t *testing.T) {
	l := newMatrixEditLive(0)
	h := matrixEditHarness(t, l)
	h.deliver(press("e"))
	v := h.view()
	for _, want := range []string{"Edit row", "Row id", "Agent", "Vendor", "Model", "Reasoning", "Note"} {
		if !strings.Contains(v, want) {
			t.Errorf("the form lacks %q:\n%s", want, v)
		}
	}
	// A pasted newline never submits; the id line takes the pasted text.
	h.deliver(tea.PasteMsg{Content: "r\n5"})
	if len(l.calls) != 0 || !strings.Contains(h.view(), "r5") {
		t.Fatalf("paste: calls %d:\n%s", len(l.calls), h.view())
	}
	// The current values of the row show dim beside their lines.
	for _, want := range []string{"now: Codex", "now: OpenAI", "now: gpt-6.1-sol", "now: medium", "now: default implementer"} {
		if !strings.Contains(h.view(), want) {
			t.Errorf("the form lacks %q:\n%s", want, h.view())
		}
	}
	h.deliver(press("enter")) // on a line that is not the last: next line
	if len(l.calls) != 0 {
		t.Fatalf("enter on the first line made a call")
	}
	h.deliver(press("tab")) // vendor
	h.deliver(press("tab")) // model
	typeText(h, "opus")
	preview(t, h, l)
	want := daemon.MatrixEditParams{Project: "ork", Op: "set", ID: "r5", Fields: slicercli.RowFields{"model": "opus"}, Preview: true}
	if len(l.calls) != 1 || !reflect.DeepEqual(l.calls[0], want) {
		t.Fatalf("calls %+v, want one %+v", l.calls, want)
	}
	h.deliver(press("n"))

	// A second run: agent and model, sent from enter on the last line; shift+tab goes back.
	h.deliver(press("e"))
	typeText(h, "r5")
	h.deliver(press("tab"))
	typeText(h, "Codex")
	h.deliver(press("tab"))
	h.deliver(press("tab"))
	typeText(h, "opus")
	h.deliver(press("tab")) // reasoning
	h.deliver(press("tab")) // note
	h.deliver(press("shift+tab"))
	h.deliver(press("tab"))
	before := len(l.calls)
	_, cmd := h.m.Update(press("enter"))
	if cmd == nil || len(l.calls) != before {
		t.Fatalf("enter on the last line: cmd %v, calls %d", cmd != nil, len(l.calls))
	}
	h.m.Update(cmd())
	want = daemon.MatrixEditParams{Project: "ork", Op: "set", ID: "r5", Fields: slicercli.RowFields{"agent": "Codex", "model": "opus"}, Preview: true}
	if len(l.calls) != 2 || !reflect.DeepEqual(l.calls[1], want) {
		t.Fatalf("calls %+v, want the second %+v", l.calls, want)
	}
}

// Enter sends the form only from the last line; on any other line it moves on,
// even when the form is complete.
func TestMatrixEditEnterSendsOnlyFromTheLastLine(t *testing.T) {
	l := newMatrixEditLive(0)
	h := matrixEditHarness(t, l)
	h.deliver(press("e"))
	typeText(h, "r5")
	h.deliver(press("tab"))
	typeText(h, "Codex")
	h.deliver(press("shift+tab"))
	_, cmd := h.m.Update(press("enter"))
	if v := h.view(); cmd != nil || len(l.calls) != 0 || !strings.Contains(v, "> Agent:") {
		t.Fatalf("enter on the row id line: cmd %v, calls %d:\n%s", cmd != nil, len(l.calls), v)
	}
}

func TestMatrixEditNeedsAFieldToChange(t *testing.T) {
	l := newMatrixEditLive(0)
	h := matrixEditHarness(t, l)
	h.deliver(press("e"))
	typeText(h, "r5")
	_, cmd := h.m.Update(ctrlS)
	if cmd != nil || len(l.calls) != 0 || !strings.Contains(h.view(), "Nothing to change") {
		t.Fatalf("cmd %v, calls %d:\n%s", cmd != nil, len(l.calls), h.view())
	}
}

func TestMatrixEditRowMustExist(t *testing.T) {
	l := newMatrixEditLive(0)
	h := matrixEditHarness(t, l)
	h.deliver(press("e"))
	typeText(h, "r99")
	h.deliver(press("tab"))
	typeText(h, "Codex")
	_, cmd := h.m.Update(ctrlS)
	v := h.view()
	if cmd != nil || len(l.calls) != 0 || !strings.Contains(v, "no row r99") || !strings.Contains(v, "Edit row") {
		t.Fatalf("cmd %v, calls %d:\n%s", cmd != nil, len(l.calls), v)
	}
	// The cursor went back to the row id: typing there changes it.
	h.deliver(press("backspace"))
	h.deliver(press("backspace"))
	typeText(h, "5")
	preview(t, h, l)
	if len(l.calls) != 1 || l.calls[0].ID != "r5" {
		t.Fatalf("calls %+v", l.calls)
	}

	// Remove and move check the id too, and move checks the row it goes before.
	for _, c := range []struct{ key, typed, want string }{
		{"d", "r99", "no row r99"},
		{"m", "r99", "no row r99"},
		{"d", "", "Row id must not be empty"},
	} {
		l := newMatrixEditLive(0)
		h := matrixEditHarness(t, l)
		h.deliver(press(c.key))
		typeText(h, c.typed)
		h.m.Update(ctrlS)
		if v := h.view(); len(l.calls) != 0 || !strings.Contains(v, c.want) {
			t.Errorf("%s %q: calls %d:\n%s", c.key, c.typed, len(l.calls), v)
		}
	}
	h = matrixEditHarness(t, newMatrixEditLive(0))
	h.deliver(press("m"))
	typeText(h, "r5")
	h.deliver(press("tab"))
	typeText(h, "r99")
	h.m.Update(ctrlS)
	if v := h.view(); !strings.Contains(v, "no row r99") {
		t.Errorf("move before an unknown row:\n%s", v)
	}
}

func TestMatrixEditAddNeedsARole(t *testing.T) {
	l := newMatrixEditLive(0)
	h := matrixEditHarness(t, l)
	h.deliver(press("n"))
	_, cmd := h.m.Update(ctrlS)
	if v := h.view(); cmd != nil || len(l.calls) != 0 || !strings.Contains(v, "Role must not be empty") {
		t.Fatalf("cmd %v, calls %d:\n%s", cmd != nil, len(l.calls), v)
	}
	typeText(h, "reviewer")
	h.deliver(press("tab"))
	typeText(h, "Codex")
	preview(t, h, l)
	want := daemon.MatrixEditParams{Project: "ork", Op: "add", Fields: slicercli.RowFields{"role": "reviewer", "agent": "Codex"}, Preview: true}
	if len(l.calls) != 1 || !reflect.DeepEqual(l.calls[0], want) {
		t.Fatalf("calls %+v, want one %+v", l.calls, want)
	}
}

func TestMatrixEditRemoveSendsTheRow(t *testing.T) {
	l := newMatrixEditLive(0)
	h := matrixEditHarness(t, l)
	h.deliver(press("d"))
	typeText(h, "r5")
	_, cmd := h.m.Update(press("enter")) // the only line is the last
	if cmd == nil || len(l.calls) != 0 {
		t.Fatalf("cmd %v, calls %d", cmd != nil, len(l.calls))
	}
	h.m.Update(cmd())
	want := daemon.MatrixEditParams{Project: "ork", Op: "remove", ID: "r5", Preview: true}
	if len(l.calls) != 1 || !reflect.DeepEqual(l.calls[0], want) {
		t.Fatalf("calls %+v, want one %+v", l.calls, want)
	}
}

func TestMatrixEditMoveSendsBeforeOrLast(t *testing.T) {
	l := newMatrixEditLive(0)
	h := matrixEditHarness(t, l)
	h.deliver(press("m"))
	typeText(h, "r5")
	h.deliver(press("tab"))
	typeText(h, "r2")
	preview(t, h, l)
	want := daemon.MatrixEditParams{Project: "ork", Op: "move", ID: "r5", Before: "r2", Preview: true}
	if len(l.calls) != 1 || !reflect.DeepEqual(l.calls[0], want) {
		t.Fatalf("calls %+v, want one %+v", l.calls, want)
	}
	h.deliver(press("n"))
	h.deliver(press("m"))
	typeText(h, "r5")
	preview(t, h, l)
	want = daemon.MatrixEditParams{Project: "ork", Op: "move", ID: "r5", Last: true, Preview: true}
	if len(l.calls) != 2 || !reflect.DeepEqual(l.calls[1], want) {
		t.Fatalf("calls %+v, want the second %+v", l.calls, want)
	}
}

func TestMatrixEditRuleSendsTheText(t *testing.T) {
	l := newMatrixEditLive(0)
	h := matrixEditHarness(t, l)
	h.deliver(press("r"))
	typeText(h, "u99")
	h.m.Update(ctrlS)
	if v := h.view(); len(l.calls) != 0 || !strings.Contains(v, "no rule u99") {
		t.Fatalf("calls %d:\n%s", len(l.calls), v)
	}
	for range 3 {
		h.deliver(press("backspace"))
	}
	typeText(h, "u3")
	h.deliver(press("tab"))
	text := "A reviewer never wrote the code it reviews."
	if v := h.view(); !strings.Contains(v, "> Text:") || !strings.Contains(v, text) {
		t.Fatalf("the text line is not prefilled with the rule's text:\n%s", v)
	}
	typeText(h, " Ever.")
	preview(t, h, l)
	want := daemon.MatrixEditParams{Project: "ork", Op: "rule-edit", ID: "u3", Text: text + " Ever.", Preview: true}
	if len(l.calls) != 1 || !reflect.DeepEqual(l.calls[0], want) {
		t.Fatalf("calls %+v, want one %+v", l.calls, want)
	}
	// An empty text is refused before the call.
	h.deliver(press("n"))
	h.deliver(press("r"))
	typeText(h, "u3")
	h.deliver(press("tab"))
	h.deliver(press("ctrl+u"))
	h.m.Update(ctrlS)
	if v := h.view(); len(l.calls) != 1 || !strings.Contains(v, "Rule text must not be empty") {
		t.Fatalf("calls %d:\n%s", len(l.calls), v)
	}
}

func TestMatrixEditConfirmShowsTheExactCommandAndNeverRunsOnNoOrEnter(t *testing.T) {
	for _, answer := range []string{"n", "enter", "esc"} {
		t.Run(answer, func(t *testing.T) {
			l := newMatrixEditLive(0)
			h := matrixEditHarness(t, l)
			h.deliver(press("e"))
			typeText(h, "r5")
			h.deliver(press("tab"))
			h.deliver(press("tab"))
			h.deliver(press("tab"))
			typeText(h, "opus")
			h.deliver(ctrlS)
			v := h.view()
			if !strings.Contains(v, "Run: slicer relay-roles set r5 --model=opus --render") || !strings.Contains(v, "[y/N] (Enter is No)") {
				t.Fatalf("the confirm lacks the command or the question:\n%s", v)
			}
			if strings.Contains(v, "Reasoning") {
				t.Errorf("the form is still on screen with the confirm:\n%s", v)
			}
			h.deliver(press(answer))
			if len(l.calls) != 1 || !l.calls[0].Preview {
				t.Fatalf("%s: calls %+v, want the preview only", answer, l.calls)
			}
			if v := h.view(); strings.Contains(v, "[y/N]") || strings.Contains(v, "Edit row") || h.m.itemPane == nil {
				t.Errorf("%s did not end the edit and keep the pane:\n%s", answer, v)
			}
			if h.m.actionStatus != "Cancelled" {
				t.Errorf("%s: status %q", answer, h.m.actionStatus)
			}
		})
	}
}

func TestMatrixEditYRunsOnceAndReloadsTheMatrix(t *testing.T) {
	l := newMatrixEditLive(60)
	l.after = editMatrix(60, "marker after")
	h := matrixEditHarness(t, l)
	for range 3 {
		h.deliver(press("j"))
	}
	h.view()
	if h.m.itemPane.offset != 3 {
		t.Fatalf("offset %d", h.m.itemPane.offset)
	}
	h.deliver(press("e"))
	typeText(h, "r5")
	h.deliver(press("tab"))
	h.deliver(press("tab"))
	h.deliver(press("tab"))
	typeText(h, "opus")
	h.deliver(ctrlS)
	if len(l.calls) != 1 || len(l.asked) != 1 {
		t.Fatalf("before y: calls %d, reads %d", len(l.calls), len(l.asked))
	}
	_, cmd := h.m.Update(press("y"))
	if cmd == nil || len(l.calls) != 1 {
		t.Fatalf("the run must be a command: cmd %v, calls %d", cmd != nil, len(l.calls))
	}
	h.deliver(cmd())
	want := daemon.MatrixEditParams{Project: "ork", Op: "set", ID: "r5", Fields: slicercli.RowFields{"model": "opus"}}
	if len(l.calls) != 2 || !reflect.DeepEqual(l.calls[1], want) {
		t.Fatalf("calls %+v, want the run %+v after the preview", l.calls, want)
	}
	if len(l.asked) != 2 {
		t.Fatalf("matrix reads %v, want the first and one reload", l.asked)
	}
	v := h.view()
	if h.m.itemPane.offset != 3 || !strings.Contains(v, "Role matrix") || strings.Contains(v, "pending") {
		t.Errorf("offset %d:\n%s", h.m.itemPane.offset, v)
	}
	if h.m.actionStatus != "Edited r5 (uncommitted)" || !strings.Contains(v, "Edited r5 (uncommitted)") {
		t.Errorf("status %q:\n%s", h.m.actionStatus, v)
	}
	if v := h.view(); !strings.Contains(v, "marker after") || strings.Contains(v, "marker before") {
		t.Errorf("the pane does not show the reloaded matrix:\n%s", v)
	}
	// The pane is not editable while the reload is out.
	h2 := matrixEditHarness(t, newMatrixEditLive(0))
	h2.deliver(press("e"))
	typeText(h2, "r5")
	h2.deliver(press("tab"))
	typeText(h2, "Codex")
	h2.deliver(ctrlS)
	_, run := h2.m.Update(press("y"))
	_, reload := h2.m.Update(run()) // the reload is out, its reply is not delivered
	h2.m.actionStatus = ""
	h2.m.Update(press("e"))
	if h2.m.actionStatus != "Matrix reloading; editing unavailable" {
		t.Errorf("e while the matrix reloads: status %q", h2.m.actionStatus)
	}
	h2.m.Update(reload())
	h2.m.Update(press("e"))
	if v := h2.view(); !strings.Contains(v, "Edit row") {
		t.Errorf("e after the reload:\n%s", v)
	}
}

func TestMatrixEditStatusAfterEachOp(t *testing.T) {
	for _, c := range []struct {
		keys []string // typed on the lines, tab between
		want string
		id   string
	}{
		{[]string{"d", "r5"}, "Removed r5 (uncommitted)", ""},
		{[]string{"m", "r5", "r2"}, "Moved r5 (uncommitted)", ""},
		{[]string{"r", "u3", "New text."}, "Edited u3 (uncommitted)", ""},
		{[]string{"n", "reviewer"}, "Added r11 (uncommitted)", "r11"},
	} {
		l := newMatrixEditLive(0)
		l.newID = c.id
		h := matrixEditHarness(t, l)
		h.deliver(press(c.keys[0]))
		for i, text := range c.keys[1:] {
			if i > 0 {
				h.deliver(press("tab"))
			}
			if c.keys[0] == "r" && i == 1 {
				h.deliver(press("ctrl+u"))
			}
			typeText(h, text)
		}
		h.deliver(ctrlS)
		h.deliver(press("y"))
		if h.m.actionStatus != c.want {
			t.Errorf("%v: status %q, want %q", c.keys, h.m.actionStatus, c.want)
		}
	}
}

func TestMatrixEditWarnsOnARowOrkCannotRun(t *testing.T) {
	warning := `agent "Acme" has no ork adapter: a plan skips this row`
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprintf("mono-%t", mono), func(t *testing.T) {
			l := newMatrixEditLive(0)
			l.warning = warning
			h := matrixEditHarness(t, l)
			h.m.cfg.Theme = NewTheme(mono)
			h.deliver(press("e"))
			typeText(h, "r5")
			h.deliver(press("tab"))
			typeText(h, "Acme")
			h.deliver(ctrlS)
			h.view()
			styled := h.m.cfg.Theme.Warn.Bold(mono).Render(warning)
			if v := h.m.View().Content; !strings.Contains(v, styled) {
				t.Fatalf("the warning is not drawn in the warning style %q:\n%s", styled, v)
			}
			v := plainView(h.m)
			if strings.Index(v, "Run: ") > strings.Index(v, warning) || strings.Index(v, warning) > strings.Index(v, "[y/N]") {
				t.Errorf("the warning must come between the command and the question:\n%s", v)
			}
			h.deliver(press("y"))
			if len(l.calls) != 2 || l.calls[1].Preview {
				t.Errorf("the edit could not be confirmed: %+v", l.calls)
			}
		})
	}
}

func TestMatrixEditShowsSlicersRefusalVerbatim(t *testing.T) {
	l := newMatrixEditLive(0)
	l.rerr = &ipc.Error{Code: "unknown_role_row", Message: "no row r5 in the matrix"}
	h := matrixEditHarness(t, l)
	h.deliver(press("e"))
	typeText(h, "r5")
	h.deliver(press("tab"))
	typeText(h, "Codex")
	h.deliver(ctrlS)
	h.deliver(press("y"))
	v := h.view()
	if !strings.Contains(v, "unknown_role_row") || !strings.Contains(v, "no row r5 in the matrix") {
		t.Fatalf("slicer's refusal is not shown:\n%s", v)
	}
	if len(l.asked) != 1 {
		t.Errorf("a refused edit reloaded the matrix: %v", l.asked)
	}
	if !strings.Contains(v, "Edit row") || strings.Contains(v, "Edited") {
		t.Errorf("the form must stay open for a change:\n%s", v)
	}
	// The change goes through after the owner fixes the line.
	l.rerr = nil
	h.deliver(press("shift+tab")) // the agent line back to the row id
	h.deliver(press("backspace"))
	h.deliver(press("backspace"))
	typeText(h, "r2")
	h.deliver(ctrlS)
	h.deliver(press("y"))
	if h.m.actionStatus != "Edited r2 (uncommitted)" || len(l.asked) != 2 {
		t.Errorf("status %q, reads %v", h.m.actionStatus, l.asked)
	}

	// A refused preview shows too, and the form stays.
	l = newMatrixEditLive(0)
	l.perr = &ipc.Error{Code: "bad_params", Message: "relay-roles set needs at least one field"}
	h = matrixEditHarness(t, l)
	h.deliver(press("e"))
	typeText(h, "r5")
	h.deliver(press("tab"))
	typeText(h, "x")
	h.deliver(ctrlS)
	if v := h.view(); !strings.Contains(v, "bad_params") || !strings.Contains(v, "needs at least one field") || !strings.Contains(v, "Edit row") {
		t.Errorf("a refused preview:\n%s", v)
	}
}

func TestMatrixEditIgnoresAStaleReply(t *testing.T) {
	l := newMatrixEditLive(0)
	h := matrixEditHarness(t, l)
	h.deliver(press("e"))
	typeText(h, "r5")
	h.deliver(press("tab"))
	typeText(h, "Codex")
	_, cmd := h.m.Update(ctrlS) // the preview is out
	stale := cmd()
	// While it is pending only esc acts.
	h.deliver(press("x"))
	h.deliver(press("tab"))
	if v := h.view(); !strings.Contains(v, "pending: matrix.edit") || strings.Contains(v, "Codexx") {
		t.Errorf("a key acted while the preview was pending:\n%s", v)
	}
	h.deliver(press("esc"))
	status := h.m.actionStatus
	h.m.Update(stale)
	if v := h.view(); strings.Contains(v, "[y/N]") || strings.Contains(v, "Edit row") || h.m.actionStatus != status || h.m.itemPane == nil {
		t.Fatalf("a stale reply changed the screen (status %q, was %q):\n%s", h.m.actionStatus, status, v)
	}

	// A reply for the earlier editor does not move a later one to its confirm.
	h.deliver(press("e"))
	typeText(h, "r2")
	h.m.Update(stale)
	if v := h.view(); strings.Contains(v, "[y/N]") || !strings.Contains(v, "Edit row") {
		t.Errorf("an earlier editor's reply reached the later editor:\n%s", v)
	}
	// Nor does it while the later editor waits for its own preview.
	h.deliver(press("tab"))
	typeText(h, "Codex")
	h.m.Update(ctrlS)
	h.m.Update(stale)
	if v := h.view(); strings.Contains(v, "[y/N]") || !strings.Contains(v, "pending: matrix.edit") {
		t.Errorf("an earlier editor's reply answered the later editor's preview:\n%s", v)
	}
}

func TestDaemonMatrixEditAsksTheDaemon(t *testing.T) {
	d, _, _, _ := requestServer(t, func(_ context.Context, method string, raw json.RawMessage) (any, error) {
		if method != daemon.MethodMatrixEdit || string(raw) != `{"project":"ork","op":"set","id":"r5","fields":{"model":"opus"},"preview":true}` {
			return nil, fmt.Errorf("unexpected %s %s", method, raw)
		}
		return &daemon.MatrixEditResult{Command: "slicer relay-roles set r5 --model=opus --render"}, nil
	})
	got, err := d.MatrixEdit(context.Background(), daemon.MatrixEditParams{Project: "ork", Op: "set", ID: "r5", Fields: slicercli.RowFields{"model": "opus"}, Preview: true})
	if err != nil || got == nil || got.Command != "slicer relay-roles set r5 --model=opus --render" {
		t.Fatalf("result %+v, %v", got, err)
	}
	failed := errors.New("connect failed")
	d = &Daemon{Connect: func(context.Context) (*daemon.Client, error) { return nil, failed }}
	if _, err := d.MatrixEdit(context.Background(), daemon.MatrixEditParams{}); err != failed {
		t.Fatalf("connect error changed: %v", err)
	}
}

// The project keys + and - are text in a field: a model name has dashes.
func TestMatrixEditTypesPlusAndMinus(t *testing.T) {
	l := newMatrixEditLive(0)
	h := matrixEditHarness(t, l)
	h.deliver(press("e"))
	typeText(h, "r5")
	h.deliver(press("tab"))
	h.deliver(press("tab"))
	h.deliver(press("tab"))
	typeText(h, "gpt-6.1-sol+x")
	if h.m.removing.name != "" || h.m.welcome.editing {
		t.Fatalf("a project key acted while typing: removing %q, welcome %v", h.m.removing.name, h.m.welcome.editing)
	}
	preview(t, h, l)
	if len(l.calls) != 1 || l.calls[0].Fields["model"] != "gpt-6.1-sol+x" {
		t.Fatalf("calls %+v", l.calls)
	}
}

func TestMatrixEditCtrlCQuits(t *testing.T) {
	h := matrixEditHarness(t, newMatrixEditLive(0))
	h.deliver(press("e"))
	_, cmd := h.m.Update(press("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c in the editor returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c in the editor did not quit")
	}
}

func TestMatrixEditEndsWhenTheProjectChanges(t *testing.T) {
	h := matrixEditHarness(t, newMatrixEditLive(0))
	h.deliver(press("e"))
	h.m.switchTo(1)
	if strings.Contains(h.view(), "Edit row") || h.m.itemPane != nil {
		t.Errorf("the editor outlived its project:\n%s", h.view())
	}
}

func TestMatrixEditHintLine(t *testing.T) {
	h := matrixEditHarness(t, newMatrixEditLive(0))
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	hint := hintLine(plainView(h.m))
	for _, want := range []string{"e/n/d/m/r edit", "pgup/pgdown page", "j/k line", "q quit", "esc close", "? help"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint line at 100 columns lacks %q: %s", want, hint)
		}
	}
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	hint = hintLine(plainView(h.m))
	if strings.Contains(hint, "e/n/d/m/r") {
		t.Errorf("the edit hint did not drop at 80 columns: %s", hint)
	}
	for _, want := range []string{"pgup/pgdown page", "q quit", "esc close", "? help"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint line at 80 columns lacks %q: %s", want, hint)
		}
	}
	// It drops before the page field: the entry's drop is not below that field's.
	drops := map[string]int{}
	for _, e := range h.m.contextHints() {
		drops[e.b.Help().Key] = e.drop
	}
	if drops["e/n/d/m/r"] < drops["pgup/pgdown"] || drops["e/n/d/m/r"] == 0 {
		t.Errorf("drop order: edit %d must not be below page %d", drops["e/n/d/m/r"], drops["pgup/pgdown"])
	}
	// A Live without the editor, and the item pane, are not offered it.
	h = matrixEditHarness(t, newMatrixEditLive(0))
	h.m.cfg.Live = &matrixLive{result: editMatrix(0, "")}
	if offered(h.m, "e/n/d/m/r", "edit") {
		t.Errorf("the hint offers the edit keys to a Live that cannot edit")
	}
	// The editor's own keys.
	h = matrixEditHarness(t, newMatrixEditLive(0))
	h.deliver(press("e"))
	hint = hintLine(plainView(h.m))
	for _, want := range []string{"tab", "enter next", "ctrl+s", "esc cancel"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the editor's hint line lacks %q: %s", want, hint)
		}
	}
	// On the last line, and in a form of one line, enter sends the form.
	h.deliver(press("shift+tab"))
	if hint = hintLine(plainView(h.m)); !strings.Contains(hint, "enter review") {
		t.Errorf("the hint line on the last line lacks enter review: %s", hint)
	}
	h.deliver(press("esc"))
	h.deliver(press("d"))
	if hint = hintLine(plainView(h.m)); !strings.Contains(hint, "enter review") || strings.Contains(hint, "tab/shift+tab") {
		t.Errorf("the hint line of the one-line form: %s", hint)
	}
	// The help lists the five keys with the pane open, and r does not refresh there.
	h.deliver(press("esc"))
	help := func() string { return strings.Join(h.m.helpLines(100, 40), "\n") }
	h.m.showHelp = true
	if !strings.Contains(help(), "e/n/d/m/r edit") || strings.Contains(help(), "r refresh now") {
		t.Errorf("the help of the open matrix pane:\n%s", help())
	}
	h.m.showHelp = false
	h.deliver(press("esc")) // closes the pane
	h.m.showHelp = true
	if !strings.Contains(help(), "r refresh now") {
		t.Errorf("the Roadmap help lost r refresh now:\n%s", help())
	}
}

// esc while a run is out closes the form, but slicer may still apply the edit:
// its reply shows the edit and reloads the matrix, or shows slicer's refusal.
func TestMatrixEditEscWhileARunIsOutStillShowsTheEdit(t *testing.T) {
	run := func(t *testing.T, l *matrixEditLive) (*harness, tea.Msg) {
		h := matrixEditHarness(t, l)
		h.deliver(press("e"))
		typeText(h, "r5")
		h.deliver(press("tab"))
		typeText(h, "Codex")
		h.deliver(ctrlS)
		_, cmd := h.m.Update(press("y"))
		h.deliver(press("esc")) // the run is out
		if v := h.view(); strings.Contains(v, "[y/N]") || strings.Contains(v, "Edit row") || !strings.Contains(h.m.actionStatus, "reloads") {
			t.Fatalf("esc did not close the form (status %q):\n%s", h.m.actionStatus, v)
		}
		return h, cmd()
	}
	l := newMatrixEditLive(0)
	l.after = editMatrix(0, "marker after")
	h, reply := run(t, l)
	h.deliver(reply)
	if h.m.actionStatus != "Edited r5 (uncommitted)" || len(l.asked) != 2 {
		t.Errorf("status %q, reads %v", h.m.actionStatus, l.asked)
	}
	if v := h.view(); !strings.Contains(v, "marker after") {
		t.Errorf("the pane does not show the reloaded matrix:\n%s", v)
	}

	l = newMatrixEditLive(0)
	l.rerr = &ipc.Error{Code: "unknown_role_row", Message: "no row r5 in the matrix"}
	h, reply = run(t, l)
	h.deliver(reply)
	if !strings.Contains(h.m.actionStatus, "unknown_role_row") || !strings.Contains(h.m.actionStatus, "no row r5 in the matrix") || len(l.asked) != 1 {
		t.Errorf("status %q, reads %v", h.m.actionStatus, l.asked)
	}

	// The same reply twice shows and reloads once.
	l = newMatrixEditLive(0)
	h, reply = run(t, l)
	h.deliver(reply)
	h.deliver(reply)
	if len(l.asked) != 2 {
		t.Errorf("a repeated reply reloaded again: %v", l.asked)
	}
}

func TestMatrixEditorGoldens(t *testing.T) {
	warning := `agent "Acme" has no ork adapter: a plan skips this row`
	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		for _, mono := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d-mono-%t", size[0], size[1], mono), func(t *testing.T) {
				l := newMatrixEditLive(0)
				l.warning = warning
				h := matrixEditHarness(t, l)
				h.m.cfg.Theme = NewTheme(mono)
				h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				h.deliver(press("e"))
				typeText(h, "r5")
				h.deliver(press("tab"))
				h.deliver(press("tab"))
				h.deliver(press("tab"))
				typeText(h, "opus")
				checkGolden(t, fmt.Sprintf("matrix-edit-%dx%d-mono-%t.txt", size[0], size[1], mono), h.m.View().Content)
				h.deliver(ctrlS)
				checkGolden(t, fmt.Sprintf("matrix-edit-confirm-%dx%d-mono-%t.txt", size[0], size[1], mono), h.m.View().Content)
			})
		}
	}
}
