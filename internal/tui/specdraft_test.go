// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// specDraftLive is a roadmapLive that also drafts a spec, as the optional
// specDrafter interface says, and records every call.
type specDraftLive struct {
	roadmapLive
	result  *daemon.SpecDraftResult
	derr    error
	aerr    error
	drafts  []string
	applies []daemon.SpecDraftApplyParams
}

func (l *specDraftLive) SpecDraft(_ context.Context, project, item string) (*daemon.SpecDraftResult, error) {
	l.drafts = append(l.drafts, project+" "+item)
	return l.result, l.derr
}

func (l *specDraftLive) SpecDraftApply(_ context.Context, p daemon.SpecDraftApplyParams) (*daemon.SpecDraftApplyResult, error) {
	l.applies = append(l.applies, p)
	if l.aerr != nil {
		return nil, l.aerr
	}
	return &daemon.SpecDraftApplyResult{Item: p.Item, Applied: p.Sections}, nil
}

func newSpecDraftLive() *specDraftLive {
	return &specDraftLive{
		roadmapLive: roadmapLive{detail: &slicercli.Detail{Item: slicercli.Item{ID: "S90", Title: "Full ninety"}, Slice: &slicercli.Spec{Title: "Spec title", Size: "M", Sections: []slicercli.Section{{Heading: "Why", Body: "The old why."}}}}},
		result: &daemon.SpecDraftResult{
			Item: "S90",
			Sections: []daemon.SpecDraftSectionView{
				{Name: "Why", Current: "The old why.", Text: "A new why.", Command: "slicer edit S90 --section Why --text 'A new why.' --render"},
				{Name: "Files", Current: "- `a.go`", Text: "- `gone.go`", Command: "slicer edit S90 --section Files --text '- `gone.go`' --render", Problems: []string{"`gone.go` is not in the checkout; mark a new file `(new)`"}},
				{Name: "Failing tests", Unchanged: true, Current: "- TestA: a."},
				{Name: "Implement", Current: "1. Old.", Text: "1. New.", Command: "slicer edit S90 --section Implement --text '1. New.' --render"},
				{Name: "Check", Unchanged: true},
			},
			OwnerNeeded: "Question: Which store?",
		},
	}
}

// specDraftHarness opens the item pane of S90 on the Roadmap, then the draft
// view with D, and loads the draft's reply.
func specDraftHarness(t *testing.T, l *specDraftLive) *harness {
	t.Helper()
	h := roadmapHarness(t)
	h.m.cfg.Live = l
	h.deliver(press("enter"))
	if h.m.itemPane == nil || h.m.itemPane.detail == nil {
		t.Fatalf("the item pane did not open:\n%s", h.view())
	}
	before := len(l.drafts)
	_, cmd := h.m.Update(press("D"))
	if cmd == nil || len(l.drafts) != before {
		t.Fatalf("D must start the draft in a command, not in Update: cmd %v, calls %d", cmd != nil, len(l.drafts))
	}
	if v := h.view(); !strings.Contains(v, "pending: spec.draft") {
		t.Fatalf("the draft shows pending:\n%s", v)
	}
	h.m.Update(cmd())
	return h
}

func TestSpecDraftViewShowsOldAndNewText(t *testing.T) {
	l := newSpecDraftLive()
	h := specDraftHarness(t, l)
	v := h.view()
	for _, want := range []string{"Spec draft S90", "Why", "The old why.", "A new why.", "Files", "gone.go", "not in the checkout", "unchanged"} {
		if !strings.Contains(v, want) {
			t.Errorf("the draft view lacks %q:\n%s", want, v)
		}
	}
	if oldAt, newAt := strings.Index(v, "The old why."), strings.Index(v, "A new why."); oldAt < 0 || newAt < oldAt {
		t.Errorf("the current text comes before the proposal:\n%s", v)
	}
	if len(l.drafts) != 1 || l.drafts[0] != "ork S90" {
		t.Fatalf("draft calls: %v", l.drafts)
	}

	// y on the section asks with the exact command, and calls nothing yet.
	h.deliver(press("y"))
	if v := h.view(); !strings.Contains(v, "Run: slicer edit S90 --section Why --text 'A new why.' --render") || !strings.Contains(v, "[y/N]") {
		t.Fatalf("y did not ask with the command:\n%s", v)
	}
	if len(l.applies) != 0 {
		t.Fatalf("asking called spec.draft-apply: %v", l.applies)
	}
	// n cancels the question and writes nothing.
	h.deliver(press("n"))
	if len(l.applies) != 0 || strings.Contains(h.view(), "[y/N]") {
		t.Fatalf("n did not cancel:\n%s", h.view())
	}

	// esc at the question backs out of the question, as n does: the draft stays.
	h.deliver(press("y"))
	h.deliver(press("esc"))
	if h.m.specDraft == nil || strings.Contains(h.view(), "[y/N]") || h.m.actionStatus != "Cancelled" || len(l.applies) != 0 {
		t.Fatalf("esc at the question must cancel it and keep the draft (status %q):\n%s", h.m.actionStatus, h.view())
	}

	// A second y confirms: the call runs in the command, not in Update.
	h.deliver(press("y"))
	_, cmd := h.m.Update(press("y"))
	if cmd == nil || len(l.applies) != 0 {
		t.Fatalf("the approval must run in the command: cmd %v, calls %d", cmd != nil, len(l.applies))
	}
	h.m.Update(cmd())
	if len(l.applies) != 1 || l.applies[0].Item != "S90" || l.applies[0].Project != "ork" || strings.Join(l.applies[0].Sections, ",") != "Why" {
		t.Fatalf("applies: %+v", l.applies)
	}
	if v := h.view(); !strings.Contains(v, "Applied Why") {
		t.Errorf("the status says what was applied:\n%s", v)
	}

	// Sections that cannot be approved never ask: one with a problem, one the
	// draft leaves unchanged, and one that is already applied.
	for _, move := range []string{"right", "right"} { // Files, then Failing tests
		h.deliver(press(move))
		h.deliver(press("y"))
		if strings.Contains(h.view(), "[y/N]") {
			t.Errorf("a section that cannot be approved asked (after %s):\n%s", move, h.view())
		}
	}
	h.deliver(press("left"))
	h.deliver(press("left"))
	h.deliver(press("y"))
	if strings.Contains(h.view(), "[y/N]") {
		t.Errorf("an applied section asked again:\n%s", h.view())
	}
	if len(l.applies) != 1 {
		t.Errorf("applies: %+v", l.applies)
	}

	// esc leaves the draft view for the item pane.
	h.deliver(press("esc"))
	if v := h.view(); strings.Contains(v, "Spec draft S90") || !strings.Contains(v, "Item S90") {
		t.Errorf("esc did not return to the item pane:\n%s", v)
	}
}

func TestSpecDraftShowsTheRolesError(t *testing.T) {
	l := newSpecDraftLive()
	l.derr = errors.New("spec-draft changed the tree, and it is a read-only role")
	h := specDraftHarness(t, l)
	if v := h.view(); !strings.Contains(v, "changed the tree") {
		t.Errorf("the draft's error:\n%s", v)
	}
	h.deliver(press("y"))
	if len(l.applies) != 0 {
		t.Errorf("an approval with no draft: %+v", l.applies)
	}
}

func TestSpecDraftSurvivesNoDaemonConnection(t *testing.T) {
	h := roadmapHarness(t)
	h.m.cfg.Live = nil
	h.deliver(press("enter")) // the item pane opens with its own no-daemon text
	if h.m.itemPane == nil {
		t.Fatal("the item pane did not open")
	}
	h.deliver(press("D"))
	if got := h.m.actionStatus; got != "No daemon connection; action unavailable" {
		t.Fatalf("status %q", got)
	}
	if v := h.view(); !strings.Contains(v, "No daemon connection; action unavailable") || strings.Contains(v, "Spec draft") {
		t.Errorf("view:\n%s", v)
	}
}

// lines is n numbered lines of text.
func lines(prefix string, n int) string {
	var out []string
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("%s line %02d", prefix, i))
	}
	return strings.Join(out, "\n")
}

// A long section makes a long command; the status area must still end with the
// question, and say that the command was cut.
func TestSpecDraftConfirmKeepsTheQuestionForALongSection(t *testing.T) {
	l := newSpecDraftLive()
	long := lines("new", 30)
	l.result.Sections[0].Text = long
	l.result.Sections[0].Command = "slicer edit S90 --section Why --text '" + long + "' --render"
	h := specDraftHarness(t, l)
	h.deliver(press("y"))
	v := h.at(80, 24)
	for _, want := range []string{"Spec draft S90", "Run: slicer edit S90 --section Why", "more lines of the command", "[y/N] (Enter is No)"} {
		if !strings.Contains(v, want) {
			t.Errorf("the long confirmation lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "new line 30") {
		t.Errorf("the whole command was shown:\n%s", v)
	}
}

// Moving the cursor brings the section it lands on into view.
func TestSpecDraftCursorScrollsItsSectionIntoView(t *testing.T) {
	l := newSpecDraftLive()
	for i := range l.result.Sections {
		s := &l.result.Sections[i]
		s.Unchanged, s.Problems = false, nil
		s.Current, s.Text = lines("old "+s.Name, 6), lines("new "+s.Name, 6)
		s.Command = "slicer edit S90 --section " + s.Name + " --text x --render"
	}
	h := specDraftHarness(t, l)
	if v := h.at(80, 24); strings.Contains(v, "> Implement") {
		t.Fatalf("Implement is below the fold at the start:\n%s", v)
	}
	for i := 0; i < 3; i++ {
		h.deliver(press("right"))
	}
	if v := h.at(80, 24); !strings.Contains(v, "> Implement") || !strings.Contains(v, "new Implement line 01") {
		t.Errorf("the section under the cursor is not on screen:\n%s", v)
	}
	for i := 0; i < 3; i++ {
		h.deliver(press("left"))
	}
	if v := h.at(80, 24); !strings.Contains(v, "> Why") {
		t.Errorf("moving back did not bring Why into view:\n%s", v)
	}
}

// The item pane's hint line offers D at 80 columns too.
func TestSpecDraftKeyIsOnTheItemPaneHintAt80Columns(t *testing.T) {
	h := roadmapHarness(t)
	h.m.cfg.Live = newSpecDraftLive()
	h.deliver(press("enter"))
	if v := h.at(80, 24); !strings.Contains(v, "D draft") {
		t.Errorf("no D draft hint at 80x24:\n%s", v)
	}
	// With no daemon the key cannot act, so the hint does not offer it.
	h.m.cfg.Live = nil
	if v := h.at(80, 24); strings.Contains(v, "D draft") {
		t.Errorf("D draft offered with no daemon:\n%s", v)
	}
}

// The golden views of the draft: the proposal beside the current text, and the
// command that waits for y. A mono and a colour theme, at both sizes.
func TestSpecDraftGoldens(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		for _, mono := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d-mono-%t", size[0], size[1], mono), func(t *testing.T) {
				h := specDraftHarness(t, newSpecDraftLive())
				h.m.cfg.Theme = NewTheme(mono)
				h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				checkGolden(t, fmt.Sprintf("spec-draft-%dx%d-mono-%t.txt", size[0], size[1], mono), h.m.View().Content)
				h.deliver(press("y"))
				checkGolden(t, fmt.Sprintf("spec-draft-confirm-%dx%d-mono-%t.txt", size[0], size[1], mono), h.m.View().Content)
			})
		}
	}
}

// D does nothing outside the item pane of a Roadmap item: not on the list, and
// not in the role matrix pane.
func TestSpecDraftKeyOpensOnlyInTheItemPane(t *testing.T) {
	l := newSpecDraftLive()
	h := roadmapHarness(t)
	h.m.cfg.Live = l
	h.deliver(press("D"))
	if len(l.drafts) != 0 || h.m.specDraft != nil {
		t.Fatalf("D on the list opened a draft: %v", l.drafts)
	}
	h.m.itemPane = &itemPane{project: "ork", id: matrixPaneID, matrix: matrixFixture()}
	h.deliver(press("D"))
	if len(l.drafts) != 0 || h.m.specDraft != nil {
		t.Fatalf("D in the matrix pane opened a draft: %v", l.drafts)
	}
}
