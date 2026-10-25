// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"context"
	"encoding/json"
	"errors"
	"github.com/squalor-xyz/ork/internal/daemon"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func roadmapHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, proj("ork", false), proj("other", false))
	// JSON keeps these behavioral tests runnable before Deps is implemented.
	var s source.Snapshot
	if err := json.Unmarshal([]byte(`{
 "Items":{"Value":[
 {"id":"S90","title":"Full ninety","short_title":"Ninety","status":"open","has_slice":true,"fields":{"size":"M","effort":2},"claim":{"owner":"SECRET-CLAIM"}},
 {"id":"S02","title":"Two","status":"open","has_slice":true,"fields":{"size":"S","effort":1}},
 {"id":"S03","title":"Started","status":"started","has_slice":true},
 {"id":"S04","title":"Unspecified","status":"open","has_slice":false}]},
 "Deps":{"Value":[{"id":"S90"},{"id":"S03"},{"id":"S04"}]},
 "Goals":{"Value":{"goals":"Keep the owner informed","records":{"goal":[{"id":"C01","title":"Visible progress","body":"Across projects"}]}}}
 }`), &s); err != nil {
		t.Fatal(err)
	}
	s.Project = proj("ork", false)
	h.src.set(s)
	h.start()
	h.m.switchTo(0)
	h.m.view = 3
	return h
}

func TestRoadmapListsItemsInSlicerOrder(t *testing.T) {
	h := roadmapHarness(t)
	v := h.view()
	if rowsWith(v, "·", "S90", "Ninety", "M2") != 1 ||
		rowsWith(v, "S02", "Two", "S1") != 1 ||
		strings.Index(v, "S90") >= strings.Index(v, "S02") {
		t.Fatalf("missing ordered roadmap rows and fields:\n%s", v)
	}
	if strings.Contains(v, "Full ninety") {
		t.Fatal("short title was not used")
	}
}

func TestRoadmapReadinessFromDeps(t *testing.T) {
	h := roadmapHarness(t)
	v := h.view()
	for _, id := range []string{"S02", "S03", "S04"} {
		if rowsWith(v, id) != 1 || rowsWith(v, id, "·") != 0 {
			t.Errorf("%s must not be ready:\n%s", id, v)
		}
	}
	if rowsWith(v, "S90", "·") != 1 || strings.Contains(v, "SECRET-CLAIM") || strings.Contains(v, "elsewhere") {
		t.Fatalf("readiness/claim:\n%s", v)
	}
	// A failed deps call must not use its retained value as readiness.
	s := h.m.projects[0].snap
	s.Deps.Err = errors.New("deps broke")
	h.m.projects[0].snap = s
	v = h.view()
	if rowsWith(v, "S90", "?") != 1 || !strings.Contains(v, "deps broke") || rowsWith(v, "S90", "·") != 0 {
		t.Fatalf("unknown deps:\n%s", v)
	}
}

func TestRoadmapGoalsHeadTheScreen(t *testing.T) {
	h := roadmapHarness(t)
	v := h.view()
	if !strings.Contains(v, "Keep the owner informed") || !strings.Contains(v, "Visible progress") ||
		strings.Index(v, "Keep the owner informed") > strings.Index(v, "S90") {
		t.Fatalf("goals must head rows:\n%s", v)
	}
	h.m.projects[0].snap.Goals.Err = errors.New("goals: precise failure")
	v = h.view()
	if !strings.Contains(v, "goals: precise failure") || !strings.Contains(v, "S90") {
		t.Fatalf("goals error blanked items:\n%s", v)
	}
}

type roadmapLive struct {
	fakeLive
	calls  []string
	writes int
	detail *slicercli.Detail
	err    error
}

func (l *roadmapLive) ItemShow(_ context.Context, p, id string) (*slicercli.Detail, error) {
	l.calls = append(l.calls, p+" "+id)
	return l.detail, l.err
}
func (l *roadmapLive) Approve(context.Context, string) error        { l.writes++; return nil }
func (l *roadmapLive) Stop(context.Context, string) error           { l.writes++; return nil }
func (l *roadmapLive) Retry(context.Context, string) error          { l.writes++; return nil }
func (l *roadmapLive) Answer(context.Context, string, string) error { l.writes++; return nil }

func TestRoadmapDetailLoadsOnEnter(t *testing.T) {
	h := roadmapHarness(t)
	l := &roadmapLive{detail: &slicercli.Detail{Item: slicercli.Item{ID: "S90", Title: "Detail title"}, Slice: &slicercli.Spec{Title: "Spec title", Size: "M", Lead: []string{"The lead"}, Sections: []slicercli.Section{{Heading: "Why", Body: "A wrapped body"}}}}}
	h.m.cfg.Live = l
	_, cmd := h.m.Update(press("enter"))
	if cmd == nil || len(l.calls) != 0 || !strings.Contains(h.view(), "pending: item.show") || strings.Contains(h.view(), "A wrapped body") {
		t.Fatalf("detail must load in command and show pending:\n%s", h.view())
	}
	h.m.Update(cmd())
	v := h.view()
	for _, word := range []string{"Spec title", "The lead", "M", "Why", "A wrapped body"} {
		if !strings.Contains(v, word) {
			t.Errorf("detail missing %q:\n%s", word, v)
		}
	}
	if len(l.calls) != 1 || l.calls[0] != "ork S90" {
		t.Fatalf("calls: %v", l.calls)
	}
	h.m.Update(press("esc"))
	if v := h.view(); !strings.Contains(v, "Ninety") || strings.Contains(v, "A wrapped body") {
		t.Fatalf("Esc did not return:\n%s", v)
	}
}

func TestRoadmapDetailErrorsAndStaleResults(t *testing.T) {
	h := roadmapHarness(t)
	l := &roadmapLive{err: errors.New("detail failed")}
	h.m.cfg.Live = l
	_, cmd := h.m.Update(press("enter"))
	if cmd == nil {
		t.Fatal("Enter returned no item.show command")
	}
	msg := cmd()
	h.m.Update(press("esc"))
	h.m.Update(press("down"))
	h.m.Update(msg)
	if strings.Contains(h.view(), "detail failed") {
		t.Fatal("closed result reopened detail")
	}
	_, cmd = h.m.Update(press("enter"))
	h.m.Update(cmd())
	if !strings.Contains(h.view(), "detail failed") {
		t.Fatal("detail error lost")
	}
	h.m.Update(press("esc"))
	_, cmd = h.m.Update(press("enter"))
	msg = cmd()
	h.m.switchTo(1)
	h.m.Update(msg)
	if strings.Contains(h.view(), "detail failed") {
		t.Fatal("other project's result shown")
	}
}

func TestRoadmapWritesNothing(t *testing.T) {
	for _, state := range []string{"awaiting-approval", "paused", "implement"} {
		for _, detail := range []bool{false, true} {
			h := roadmapHarness(t)
			l := &roadmapLive{}
			h.m.cfg.Live = l
			h.m.projects[0].snap.EngineRuns = []source.EngineRun{{ID: "engine", Item: "S90", State: state, Pending: "question"}}
			if detail {
				_, cmd := h.m.Update(press("enter"))
				if cmd != nil {
					h.m.Update(cmd())
				}
			}
			for _, k := range []string{"a", "x", "t", "y", "enter", "hello", "esc", "down", "up", "j", "k", "n", "p", "?", "?", "s", "s", "/", "esc", "r"} {
				// Refresh from r is a read and is allowed.
				_, cmd := h.m.Update(press(k))
				for _, msg := range exec(cmd) {
					if _, ok := msg.(noticeMsg); !ok {
						h.m.Update(msg)
					}
				}
			}
			if l.writes != 0 {
				t.Fatalf("%s detail=%v made %d writes", state, detail, l.writes)
			}
		}
	}
}

func TestTabReachesTheRoadmapView(t *testing.T) {
	h := roadmapHarness(t)
	h.m.view = 0
	h.m.Update(press("tab")) // Questions
	h.m.Update(press("tab")) // Waves
	h.m.Update(press("tab"))
	if h.m.view != 3 {
		t.Fatalf("tab reached %d, want Roadmap", h.m.view)
	}
	h.m.Update(press("shift+tab")) // Waves
	h.m.Update(press("shift+tab")) // Questions
	h.m.Update(press("shift+tab"))
	if h.m.view != 0 {
		t.Fatalf("shift+tab reached %d", h.m.view)
	}
}

func TestRoadmapNeedsAProject(t *testing.T) {
	h := roadmapHarness(t)
	h.m.switchTo(-1)
	if v := h.view(); !strings.Contains(strings.ToLower(v), "select a project") || strings.Contains(v, "S90") {
		t.Fatalf("Needs you:\n%s", v)
	}
}

func TestRoadmapCursorChoosesDetailAndRejectsStaleReplies(t *testing.T) {
	h := roadmapHarness(t)
	l := &roadmapLive{err: errors.New("obsolete")}
	h.m.cfg.Live = l
	_, cmd := h.m.Update(press("enter"))
	stale := cmd()
	h.m.Update(press("esc"))
	h.m.Update(press("down"))
	_, cmd = h.m.Update(press("enter"))
	if cmd == nil {
		t.Fatal("second row did not open")
	}
	h.m.Update(stale)
	if v := h.view(); strings.Contains(v, "obsolete") || !strings.Contains(v, "pending: item.show") {
		t.Fatalf("old item result accepted:\n%s", v)
	}
	l.err = nil
	l.detail = &slicercli.Detail{Item: slicercli.Item{Title: "Second item"}}
	h.m.Update(cmd())
	if len(l.calls) != 2 || l.calls[1] != "ork S02" || !strings.Contains(h.view(), "Second item") {
		t.Fatalf("cursor target: %v\n%s", l.calls, h.view())
	}
	h.m.Update(press("esc"))
	// An older request for the same item must also be ignored after reopening.
	l.detail = &slicercli.Detail{Item: slicercli.Item{Title: "Obsolete same-item detail"}}
	_, cmd = h.m.Update(press("enter"))
	older := cmd()
	h.m.Update(press("esc"))
	h.m.Update(press("enter"))
	h.m.Update(older)
	if v := h.view(); !strings.Contains(v, "pending: item.show") || strings.Contains(v, "Obsolete same-item detail") {
		t.Fatalf("old request replaced new request:\n%s", v)
	}
}

func TestRoadmapItemErrorKeepsGoalsAndDetailFits(t *testing.T) {
	h := roadmapHarness(t)
	h.m.projects[0].snap.Items.Err = errors.New("items exact failure")
	if v := h.view(); !strings.Contains(v, "items exact failure") || !strings.Contains(v, "Keep the owner informed") {
		t.Fatalf("items error:\n%s", v)
	}
	_, cmd := h.m.Update(press("enter"))
	if cmd != nil {
		t.Fatal("failed Items opened a detail")
	}
	h.m.projects[0].snap.Items.Err = nil
	h.m.cfg.Live = &roadmapLive{detail: &slicercli.Detail{Slice: &slicercli.Spec{
		Title: strings.Repeat("Long title ", 10), Lead: []string{strings.Repeat("Long lead ", 15)}, Sections: []slicercli.Section{{Heading: "Why", Body: strings.Repeat("Long body ", 80)}}}}}
	_, cmd = h.m.Update(press("enter"))
	h.m.Update(cmd())
	h.m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	v := plainView(h.m)
	if len(strings.Split(v, "\n")) != 12 {
		t.Fatalf("wrong height: %s", v)
	}
	for _, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("overwide: %q", line)
		}
	}
}

func TestRoadmapCursorMovesAfterListShrinks(t *testing.T) {
	for _, keys := range [][]string{{"up"}, {"down", "up"}} {
		t.Run(strings.Join(keys, "-"), func(t *testing.T) {
			h := roadmapHarness(t)
			for i := 0; i < 3; i++ {
				h.m.Update(press("down"))
			}
			if h.m.roadmapCursor != 3 {
				t.Fatalf("cursor = %d, want last row 3", h.m.roadmapCursor)
			}
			s := h.m.projects[0].snap
			s.Items.Value = s.Items.Value[:2]
			h.m.Update(snapshotMsg{name: "ork", snap: s})
			v := h.m.roadmap(100)
			if v.sel < 0 || !strings.Contains(v.body[v.sel], "S02") {
				t.Fatalf("shrunken list must select its last row: %+v", v)
			}
			for _, key := range keys {
				h.m.Update(press(key))
				want, cursor := "S90", 0
				if key == "down" {
					want, cursor = "S02", 1
				}
				v = h.m.roadmap(100)
				if h.m.roadmapCursor != cursor || v.sel < 0 || !strings.Contains(v.body[v.sel], want) {
					t.Fatalf("%s after shrink: cursor = %d, want %d (%s); view: %+v", key, h.m.roadmapCursor, cursor, want, v)
				}
			}
		})
	}
}

func TestRoadmapMoveViewResetsDetailAndCursor(t *testing.T) {
	h := roadmapHarness(t)
	h.m.cfg.Live = &roadmapLive{detail: &slicercli.Detail{Item: slicercli.Item{Title: "Detail before tab"}}}
	h.m.Update(press("down"))
	_, cmd := h.m.Update(press("enter"))
	if cmd == nil {
		t.Fatal("second row did not open")
	}
	h.m.Update(cmd())
	if !strings.Contains(h.view(), "Detail before tab") {
		t.Fatal("detail did not load")
	}
	h.m.Update(press("tab"))
	if h.m.view == roadmapView {
		t.Fatal("tab did not leave Roadmap")
	}
	h.m.Update(press("shift+tab"))
	if h.m.view != roadmapView {
		t.Fatal("shift+tab did not return to Roadmap")
	}
	if h.m.roadmapCursor != 0 {
		t.Errorf("cursor = %d, want 0", h.m.roadmapCursor)
	}
	if v := h.view(); !strings.Contains(v, "Ninety") || !strings.Contains(v, "Two") || strings.Contains(v, "Detail before tab") {
		t.Fatalf("view change did not return to the list:\n%s", v)
	}
}

func TestRoadmapUnusedSizeAndEffortShowDashes(t *testing.T) {
	h := roadmapHarness(t)
	v := h.view()
	for _, id := range []string{"S03", "S04"} {
		if rowsWith(v, id, "--") != 1 {
			t.Errorf("%s must show unused size and effort as dashes:\n%s", id, v)
		}
	}
	if rowsWith(v, "S90", "M2") != 1 || rowsWith(v, "S02", "S1") != 1 {
		t.Fatalf("used size and effort changed:\n%s", v)
	}
}

func (l *roadmapLive) AnswerAction(ctx context.Context, a daemon.RunAction) (string, error) {
	return "", l.Answer(ctx, a.Run, a.Text)
}

func TestRoadmapHeaderCarriesCounts(t *testing.T) {
	h := roadmapHarness(t)
	body := strings.Join(h.m.roadmap(80).body, "\n")
	if !strings.Contains(body, "Roadmap  1 ready of 4, 3 open, 1 other") {
		t.Fatal("mixed status counts missing")
	}
	h.m.projects[0].snap.Items.Value[2].Status = "open"
	if !strings.Contains(strings.Join(h.m.roadmap(80).body, "\n"), "Roadmap  2 ready of 4, all open") {
		t.Fatal("all-open counts missing")
	}
}
func TestRoadmapSizeEffortColumn(t *testing.T) {
	h := roadmapHarness(t)
	ov := h.m.roadmap(60)
	for _, line := range ov.body {
		plain := sgr.ReplaceAllString(line, "")
		if strings.Contains(plain, "S90") || strings.Contains(plain, "S02") {
			if lipgloss.Width(line) != 60 || !(strings.HasSuffix(plain, "M2") || strings.HasSuffix(plain, "S1")) {
				t.Errorf("compact right column missing: %q", plain)
			}
			if strings.Contains(plain, "open") || strings.Contains(plain, "effort") || strings.Contains(plain, "ready") {
				t.Errorf("repeated metadata: %q", plain)
			}
		}
	}
	h.m.roadmapCursor = 1
	if !strings.Contains(strings.Join(h.m.roadmap(60).body, "\n"), h.m.cfg.Theme.Dim.Render("·")) {
		t.Error("ready marker is not dim")
	}
	if !strings.Contains(sgr.ReplaceAllString(strings.Join(ov.body, "\n"), ""), "· S90") {
		t.Error("ready marker missing")
	}
}

func TestRoadmapASCIIAndUnbrokenCursor(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, mono := range []bool{false, true} {
			h := roadmapHarness(t)
			h.m.cfg.ASCII = ascii
			h.m.cfg.Theme = NewTheme(mono)
			ov := h.m.roadmap(60)
			if ascii {
				for _, r := range sgr.ReplaceAllString(strings.Join(ov.body, "\n"), "") {
					if r > 127 {
						t.Errorf("ASCII roadmap has non-ASCII glyph %q", r)
						break
					}
				}
			}
			line := ov.body[ov.sel]
			plain := sgr.ReplaceAllString(line, "")
			if line != h.m.cfg.Theme.TabActive.Render(plain) {
				t.Errorf("cursor bar interrupted by nested marker style: %q", line)
			}
		}
	}
}
