// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// matrixLive answers roles.matrix from a canned result and logs the project of
// each call. Every other call is roadmapLive's.
type matrixLive struct {
	roadmapLive
	result *daemon.RolesMatrixResult
	merr   error
	asked  []string
}

func (l *matrixLive) RolesMatrix(_ context.Context, project string) (*daemon.RolesMatrixResult, error) {
	l.asked = append(l.asked, project)
	return l.result, l.merr
}

// matrixFixture is a matrix like slicer prints it, with a row of every kind
// ork reads differently.
func matrixFixture() *daemon.RolesMatrixResult {
	return &daemon.RolesMatrixResult{
		Markdown: "# Relay roles\n\n## Matrix\n\n| ID | Role | When | Agent | Vendor | Model | Reasoning | Note |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n" +
			"| r9 | Implementer | whole slice, effort 3, not a sliver | Codex | OpenAI | gpt-6.1-sol | medium | default implementer for a whole slice (owner lineup 2026-10-23: medium); Codex sandbox cannot bind Unix sockets, so rerun socket-test packages outside it |\n" +
			"| r3 | Implementer | whole slice | Claude Code | Claude | sonnet | high | - |\n| r21 | Implementer | whole slice | Acme Agent | Acme | tiny | low | - |\n" +
			"| r14 | Reviewer | every slice | Claude Code | Claude | opus | medium | UI pass |\n| r6 | Gate | every slice | Claude Code | Claude | opus | medium | - |\n| r10 | Orchestrator | every slice | Claude Code | Claude | sonnet | high | - |\n\n" +
			"## Rules\n\n- u10: A slice that touches the UI also runs the UI pass row (r14) as its own role, in parallel with the reviewer, not instead of it, on every size.\n- u1: A reviewer never wrote code in the slice it reviews.\n",
		Rows: []daemon.MatrixRow{
			{ID: "r9", Role: "implementer", Agent: "Codex", Use: "used as codex"},
			{ID: "r3", Role: "implementer", Agent: "Claude Code", Use: "used as claude"},
			{ID: "r21", Role: "implementer", Agent: "Acme Agent", Use: "skipped: no adapter for Acme Agent"},
			{ID: "r14", Role: "reviewer", Agent: "Claude Code", Use: "UI pass row, used as claude"},
			{ID: "r6", Role: "gate", Agent: "Claude Code", Use: "not used: the gate runs the reviewer's lineup"},
			{ID: "r10", Role: "orchestrator", Agent: "Claude Code", Use: "not run by ork"},
		},
	}
}

func longMatrix() *daemon.RolesMatrixResult {
	var lines []string
	for i := 0; i < 120; i++ {
		lines = append(lines, fmt.Sprintf("long line %03d", i))
	}
	return &daemon.RolesMatrixResult{Markdown: strings.Join(lines, "\n") + "\n", Rows: matrixFixture().Rows}
}

// openMatrixPane presses m on the Roadmap of the harness and delivers the reply.
func openMatrixPane(t *testing.T, h *harness, l *matrixLive) {
	t.Helper()
	h.m.cfg.Live = l
	_, cmd := h.m.Update(press("m"))
	if cmd == nil {
		t.Fatal("m returned no command")
	}
	h.m.Update(cmd())
}

func TestRoadmapMKeyOpensTheMatrixPane(t *testing.T) {
	h := roadmapHarness(t)
	l := &matrixLive{result: matrixFixture()}
	h.m.cfg.Live = l
	_, cmd := h.m.Update(press("m"))
	if cmd == nil || len(l.asked) != 0 {
		t.Fatalf("the read must run in the command, not in Update: cmd %v, asked %v", cmd != nil, l.asked)
	}
	v := h.view()
	if !strings.Contains(v, "pending: roles.matrix") || !strings.Contains(v, "Role matrix  (esc returns)") || strings.Contains(v, "## ork") {
		t.Fatalf("the pending pane:\n%s", v)
	}
	h.m.Update(cmd())
	if len(l.asked) != 1 || l.asked[0] != "ork" {
		t.Fatalf("asked %v, want the active project once", l.asked)
	}
	v = h.view()
	for _, want := range []string{
		"Role matrix  (esc returns)",
		"# Relay roles", "| r3 | Implementer | whole slice | Claude Code |",
		"## ork",
		"- r9 implementer: used as codex",
		"- r21 implementer: skipped: no adapter for Acme Agent",
		"- r14 reviewer: UI pass row, used as claude",
		"- r6 gate: not used: the gate runs the reviewer's lineup",
		"- r10 orchestrator: not run by ork",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("the pane lacks %q:\n%s", want, v)
		}
	}
	if strings.Index(v, "## ork") > strings.Index(v, "# Relay roles") {
		t.Errorf("ork's section must come before slicer's text:\n%s", v)
	}
	cell := func(line string) string { _, c, _ := strings.Cut(line, "│"); return strings.TrimSpace(c) }
	lines := strings.Split(v, "\n")
	for i, line := range lines {
		if strings.Contains(line, "- r10 orchestrator") {
			if cell(lines[i+1]) != "" || !strings.Contains(lines[i+2], "# Relay roles") {
				t.Errorf("a blank line must separate the ork section from slicer's text:\n%s", v)
			}
		}
	}
	if strings.Contains(v, "Ninety") {
		t.Errorf("the roadmap rows show under the pane:\n%s", v)
	}
	h.deliver(press("esc"))
	if h.m.itemPane != nil || !strings.Contains(h.view(), "Ninety") || strings.Contains(h.view(), "Role matrix") {
		t.Fatalf("esc did not return to the roadmap:\n%s", h.view())
	}
}

func TestMatrixPaneShowsAFailureAndIgnoresStaleReplies(t *testing.T) {
	h := roadmapHarness(t)
	l := &matrixLive{merr: &ipc.Error{Code: "usage", Message: "matrix unreadable"}}
	openMatrixPane(t, h, l)
	if v := h.view(); !strings.Contains(v, "usage: matrix unreadable") || strings.Contains(v, "pending") {
		t.Fatalf("the failure is not shown as the pane's text:\n%s", v)
	}
	// A reply that arrives after esc must not reopen the pane.
	l.merr, l.result = nil, matrixFixture()
	h.deliver(press("esc"))
	_, cmd := h.m.Update(press("m"))
	late := cmd()
	h.deliver(press("esc"))
	h.m.Update(late)
	if h.m.itemPane != nil || strings.Contains(h.view(), "Role matrix") {
		t.Fatalf("a stale reply reopened the pane:\n%s", h.view())
	}
}

// A reply to an earlier read does not fill the pane of a later one, and a
// matrix reply does not fill an item pane.
func TestMatrixReplyFillsOnlyItsOwnPane(t *testing.T) {
	h := roadmapHarness(t)
	l := &matrixLive{result: matrixFixture()}
	h.m.cfg.Live = l
	_, first := h.m.Update(press("m"))
	h.deliver(press("esc"))
	_, second := h.m.Update(press("m"))
	h.m.Update(first())
	if v := h.view(); !strings.Contains(v, "pending: roles.matrix") || strings.Contains(v, "## ork") {
		t.Fatalf("the reply of the first read filled the second pane:\n%s", v)
	}
	h.m.Update(second())
	if v := h.view(); strings.Contains(v, "pending: roles.matrix") || !strings.Contains(v, "## ork") {
		t.Fatalf("the reply of the second read did not fill its pane:\n%s", v)
	}

	h = roadmapHarness(t)
	h.m.cfg.Live = &roadmapLive{detail: &slicercli.Detail{Item: slicercli.Item{ID: "S90"}, Slice: &slicercli.Spec{Title: "Spec title"}}}
	h.m.Update(press("enter")) // the item pane, its reply not yet delivered
	pane := h.m.itemPane
	h.m.Update(matrixMsg{project: "ork", request: pane.request, result: matrixFixture()})
	if pane.matrix != nil || !pane.pending {
		t.Fatalf("a matrix reply filled the item pane: %+v", pane)
	}
}

func TestMatrixPaneScrollsLikeTheItemPane(t *testing.T) {
	h := roadmapHarness(t)
	l := &matrixLive{result: longMatrix()}
	openMatrixPane(t, h, l)
	if v := h.view(); !strings.Contains(v, "long line 000") || strings.Contains(v, "long line 119") {
		t.Fatalf("the pane does not start at the top:\n%s", v)
	}
	pane := h.m.itemPane
	room := pane.room
	if room < 5 {
		t.Fatalf("the pane drew %d lines of room", room)
	}
	offset := func(want int, what string) {
		t.Helper()
		if pane.offset != want {
			t.Fatalf("%s: offset %d, want %d", what, pane.offset, want)
		}
		h.view()
	}
	h.deliver(press("j"))
	offset(1, "j")
	h.deliver(press("down"))
	offset(2, "down")
	h.deliver(press("k"))
	offset(1, "k")
	h.deliver(press("k"))
	h.deliver(press("k"))
	offset(0, "k at the top")
	h.deliver(press("pgdown"))
	offset(room, "pgdown")
	h.deliver(press("pgup"))
	offset(0, "pgup")
	h.deliver(press("ctrl+d"))
	offset(max(room/2, 1), "ctrl+d")
	h.deliver(press("ctrl+u"))
	offset(0, "ctrl+u")
	for range 40 {
		h.deliver(press("ctrl+d"))
		h.view()
	}
	if v := h.view(); !strings.Contains(v, "long line 119") || !strings.Contains(v, "Role matrix  (esc returns)") {
		t.Fatalf("scrolling does not reach the end under the heading:\n%s", v)
	}
	// The cursor of the Roadmap under the pane does not move.
	if h.m.roadmapCursor != 0 {
		t.Errorf("the roadmap cursor moved to %d", h.m.roadmapCursor)
	}
	// Nothing in the pane takes an edit key.
	for _, k := range []string{"a", "x", "t", "f", "F", "l", "e", "c", "o", "space", "enter", "y", "n"} {
		h.deliver(press(k))
		if h.m.itemPane == nil || h.m.confirm != nil || h.m.launch != nil || l.writes != 0 || len(l.asked) != 1 {
			t.Fatalf("%q acted in the matrix pane: confirm %v, launch %v, writes %d, asked %v", k, h.m.confirm, h.m.launch != nil, l.writes, l.asked)
		}
	}
	// m inside the pane does not read the matrix again.
	h.deliver(press("m"))
	if len(l.asked) != 1 {
		t.Errorf("m in the pane read the matrix again: %v", l.asked)
	}
}

// offered reports whether the hint line offers a key with these words.
func offered(m *Model, keyName, desc string) bool {
	for _, e := range m.contextHints() {
		if h := e.b.Help(); h.Key == keyName && h.Desc == desc {
			return true
		}
	}
	return false
}

func TestMatrixKeyIsOfferedOnlyOnTheRoadmap(t *testing.T) {
	h := roadmapHarness(t)
	h.m.cfg.Live = &matrixLive{result: matrixFixture()}
	help := func() string { return strings.Join(h.m.helpLines(120, 60), "\n") }
	if !offered(h.m, "m", "matrix") {
		t.Fatalf("the Roadmap hints do not list m matrix: %+v", h.m.contextHints())
	}
	// The hint line drops it before the tab key when the pane is narrow.
	h.m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	if v := plainView(h.m); !strings.Contains(v, "m matrix") {
		t.Fatalf("a wide hint line does not show m matrix:\n%s", v)
	}
	h.m.showHelp = true
	if !strings.Contains(help(), "m matrix") {
		t.Errorf("the help on the Roadmap does not list m matrix:\n%s", help())
	}
	h.m.showHelp = false
	for view, name := range map[int]string{0: "Overview", 1: "Questions", 2: "Waves", 4: "Decisions"} {
		h.m.view = view
		if offered(h.m, "m", "matrix") {
			t.Errorf("%s hints list m matrix", name)
		}
		if strings.Contains(help(), "m matrix") {
			t.Errorf("%s help lists m matrix", name)
		}
	}
	h.m.view = roadmapView
	h.deliver(press("m"))
	if offered(h.m, "m", "matrix") {
		t.Errorf("the open pane's hints list m matrix")
	}
	if !offered(h.m, "esc", "close") {
		t.Errorf("the open pane's hints lack esc")
	}
}

func TestMatrixKeyElsewhereDoesNothing(t *testing.T) {
	for _, view := range []int{0, 1, 2, 4} {
		h := roadmapHarness(t)
		l := &matrixLive{result: matrixFixture()}
		h.m.cfg.Live = l
		h.m.view = view
		before := h.m.itemPane
		_, cmd := h.m.Update(press("m"))
		if cmd != nil || h.m.itemPane != before || len(l.asked) != 0 {
			t.Errorf("view %d: m opened something (cmd %v, asked %v)", view, cmd != nil, l.asked)
		}
	}
	// The Running entry has no project to ask about.
	h := roadmapHarness(t)
	l := &matrixLive{result: matrixFixture()}
	h.m.cfg.Live = l
	h.m.active = runningEntry
	if _, cmd := h.m.Update(press("m")); cmd != nil || h.m.itemPane != nil || offered(h.m, "m", "matrix") {
		t.Errorf("m opened the matrix with no project")
	}
}

// A project that is unavailable or has not answered yet has no matrix to ask for.
func TestMatrixKeyNeedsALoadedAvailableProject(t *testing.T) {
	for name, set := range map[string]func(*Model){
		"unavailable": func(m *Model) { m.projects[0].snap.Unavailable = true },
		"not loaded":  func(m *Model) { m.projects[0].loaded = false },
	} {
		t.Run(name, func(t *testing.T) {
			h := roadmapHarness(t)
			l := &matrixLive{result: matrixFixture()}
			h.m.cfg.Live = l
			set(h.m)
			if _, cmd := h.m.Update(press("m")); cmd != nil || h.m.itemPane != nil || len(l.asked) != 0 || offered(h.m, "m", "matrix") {
				t.Errorf("m acted on a project that cannot answer")
			}
		})
	}
}

func TestMatrixKeyWithoutADaemonSaysSo(t *testing.T) {
	h := roadmapHarness(t)
	h.m.cfg.Live = nil
	_, cmd := h.m.Update(press("m"))
	if cmd != nil || h.m.itemPane != nil || h.m.actionStatus != "No daemon connection; action unavailable" {
		t.Fatalf("cmd %v, pane %v, status %q", cmd != nil, h.m.itemPane != nil, h.m.actionStatus)
	}
	if !strings.Contains(h.view(), "No daemon connection; action unavailable") {
		t.Errorf("the status line is not on screen:\n%s", h.view())
	}
	if offered(h.m, "m", "matrix") {
		t.Errorf("the hint offers m with no daemon")
	}
}

func TestMatrixPaneGolden(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		for _, mono := range []bool{false, true} {
			name := fmt.Sprintf("matrix-pane-%dx%d-mono-%t.txt", size[0], size[1], mono)
			t.Run(name, func(t *testing.T) {
				h := roadmapHarness(t)
				h.m.cfg.Theme = NewTheme(mono)
				openMatrixPane(t, h, &matrixLive{result: matrixFixture()})
				h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				checkGolden(t, name, h.m.View().Content)
			})
		}
	}
}

func TestDaemonRolesMatrixAsksTheDaemon(t *testing.T) {
	d, _, _, _ := requestServer(t, func(_ context.Context, method string, raw json.RawMessage) (any, error) {
		if method != daemon.MethodRolesMatrix || string(raw) != `{"project":"ork"}` {
			return nil, fmt.Errorf("unexpected %s %s", method, raw)
		}
		return matrixFixture(), nil
	})
	got, err := d.RolesMatrix(context.Background(), "ork")
	if err != nil || got == nil || len(got.Rows) != 6 || got.Rows[2].Use != "skipped: no adapter for Acme Agent" {
		t.Fatalf("matrix %+v, %v", got, err)
	}
	failed := errors.New("connect failed")
	d = &Daemon{Connect: func(context.Context) (*daemon.Client, error) { return nil, failed }}
	if _, err := d.RolesMatrix(context.Background(), "ork"); err != failed {
		t.Fatalf("connect error changed: %v", err)
	}
}

// The hint line keeps m matrix at 100 columns (the owner's order, S168): it
// outlasts + add project and - remove project and drops before tab views.
func TestMatrixHintOutlastsAddAndRemoveAndDropsBeforeTabViews(t *testing.T) {
	h := roadmapHarness(t)
	h.m.cfg.Live = &matrixLive{result: matrixFixture()}
	drops := map[string]int{}
	for _, e := range h.m.contextHints() {
		drops[e.b.Help().Key] = e.drop
	}
	if !(drops["-"] > drops["m"] && drops["+"] > drops["m"] && drops["m"] > drops["tab"]) {
		t.Fatalf("drop order: remove %d, add %d must exceed m %d, which must exceed tab %d", drops["-"], drops["+"], drops["m"], drops["tab"])
	}
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	hint := hintLine(plainView(h.m))
	for _, want := range []string{"m matrix", "tab views", "l launch", "enter open"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint line at 100 columns lacks %q: %s", want, hint)
		}
	}
	if strings.Contains(hint, "+ add project") || strings.Contains(hint, "- remove project") {
		t.Errorf("add or remove outlasted m matrix at 100 columns: %s", hint)
	}
}

// Wrapped lines hang two spaces under their first line, so each table row and
// each rule bullet stands out (UI pass, S168).
func TestMatrixPaneWrappedLinesHang(t *testing.T) {
	h := roadmapHarness(t)
	openMatrixPane(t, h, &matrixLive{result: matrixFixture()})
	const w = 60
	body := h.m.roadmap(w).body
	var lines []string
	for _, l := range body {
		lines = append(lines, sgr.ReplaceAllString(l, ""))
	}
	hung := 0
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			t.Errorf("line %d is %d columns wide, over %d: %q", i, lipgloss.Width(l), w, l)
		}
		if strings.HasPrefix(l, "  | r9 |") || strings.HasPrefix(l, "  - u10:") {
			next := lines[i+1]
			if !strings.HasPrefix(next, "    ") || strings.HasPrefix(next, "     ") {
				t.Errorf("the continuation of %q is not hung two spaces: %q", l, next)
			}
			hung++
		}
	}
	if hung != 2 {
		t.Fatalf("found %d wrapped starts, want the r9 row and the u10 rule:\n%s", hung, strings.Join(lines, "\n"))
	}
}
