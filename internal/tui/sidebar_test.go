// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/registry"
)

func TestSidebarRowsAndSelection(t *testing.T) {
	h := newHarness(t, proj("alpha", true), proj("beta", true))
	h.start()
	h.m.apply(snapshotMsg{name: "beta", snap: snap(proj("beta", true), []adjcli.Record{openRec(1, "b.q", "beta question")}, nil, relay("run", "", "S1"))})
	v := h.view()
	if !strings.Contains(v, "0>Needs you") {
		t.Fatalf("missing selected Needs you sidebar row:\n%s", v)
	}
	if !strings.Contains(compactSpaces(v), "beta 1*") {
		t.Fatalf("missing project badge and running marker:\n%s", v)
	}
	if !strings.Contains(h.m.screen(), "\x1b[7") && !strings.Contains(h.m.screen(), ";7m") {
		t.Fatal("selection has no reverse style")
	}
	h.send(press("2")) // alpha: beta needs the owner, so the sidebar draws it first
	if strings.Contains(h.view(), "beta question") {
		t.Fatal("project selection shows another project's rows")
	}
	h.send(press("[")) // beta is before alpha after priority sorting
	h.send(press("[")) // Needs you
	if !strings.Contains(h.view(), "beta question") {
		t.Fatal("Needs you lost all-project Overview")
	}
}

func TestSidebarNavigationFilterAndHide(t *testing.T) {
	h := newHarness(t, proj("alpha", false), proj("beta", false))
	h.start()
	for _, k := range []string{"]", "]"} {
		h.send(press(k))
	}
	if !strings.Contains(h.view(), "2>beta") {
		t.Fatal("right did not move selection to beta")
	}
	h.send(press("["))
	if !strings.Contains(h.view(), "1>alpha") {
		t.Fatal("left did not move selection")
	}
	h.send(press("[")) // Needs you: no active project to retain through the filter
	h.send(press("/"))
	h.send(press("b"))
	v := h.view()
	if strings.Contains(v, "alpha") || !strings.Contains(v, "beta") {
		t.Fatalf("filter did not restrict sidebar: %s", v)
	}
	h.send(press("esc"))
	if !strings.Contains(h.view(), "alpha") {
		t.Fatal("esc did not clear filter")
	}
	h.send(press("s"))
	if strings.Contains(h.view(), "0>Needs you") {
		t.Fatal("s did not hide sidebar")
	}
	h.send(press("s"))
	if !strings.Contains(h.view(), "0>Needs you") {
		t.Fatal("s did not show sidebar")
	}
}

func TestSidebarOrderFollowsProject(t *testing.T) {
	h := newHarness(t, proj("alpha", false), proj("beta", true), proj("gamma", false), proj("aardvark", false))
	h.start()
	h.send(press("1"))
	h.m.apply(snapshotMsg{name: "gamma", snap: snap(proj("gamma", false), nil, nil, relay("run", "", "S1"))})
	h.m.apply(snapshotMsg{name: "beta", snap: snap(proj("beta", true), []adjcli.Record{openRec(1, "b.q", "question")}, nil)})
	v := h.view()
	b, g, a := strings.Index(compactSpaces(v), "beta 1"), strings.Index(compactSpaces(v), "gamma *"), strings.Index(compactSpaces(v), "3>alpha")
	if b < 0 || g < b || a < g || strings.Index(compactSpaces(v), "aardvark") < a {
		t.Fatalf("wrong sidebar group order or selection:\n%s", v)
	}
}

func TestSidebarScrollAndCompactNames(t *testing.T) {
	var projects []registry.Project
	for i := 1; i <= 15; i++ {
		projects = append(projects, proj(fmt.Sprintf("%02d-long-project-name", i), false))
	}
	h := newHarness(t, projects...)
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	for i := 0; i < 15; i++ {
		h.send(press("]"))
	}
	v := plainView(h.m)
	if !strings.Contains(v, "> 15-long-proje...") || !strings.Contains(v, "+8 more") {
		t.Fatalf("selection/scroll hint/compact name missing:\n%s", v)
	}
}

func TestNeedsYouRefreshesAll(t *testing.T) {
	h := newHarness(t, four...)
	h.start()
	h.src.take()
	h.send(press("r"))
	if got := names(h.src.take()); strings.Join(got, ",") != "DataBall,adjudicate,agents,slicer" {
		t.Fatalf("Needs you refresh got %v", got)
	}
}

func TestNoticeDuringFetchReadsAgain(t *testing.T) {
	h := newHarness(t, proj("alpha", true))
	h.start()
	held := h.sendHeld(press("r"))
	h.m.cfg.Live = &fakeLive{}
	h.m.Update(noticeMsg{project: "alpha"})
	h.src.set(snap(proj("alpha", true), []adjcli.Record{openRec(1, "a.q", "new notice value")}, nil))
	h.send(held[0])
	if !strings.Contains(h.view(), "new notice value") {
		t.Fatal("notice during fetch was dropped")
	}
}

func TestNoticeFailureRetriesOnTick(t *testing.T) {
	h := newHarness(t, proj("alpha", false))
	h.start()
	live := &fakeLive{notices: []string{"alpha"}}
	h.m.cfg.Live = live
	h.m.Update(noticeMsg{err: errors.New("disconnected")})
	h.src.set(snap(proj("alpha", true), []adjcli.Record{openRec(1, "a.q", "recovered notice value")}, nil))
	for _, msg := range h.send(tickMsg{}) {
		if _, ok := msg.(focusMsg); ok {
			h.deliver(msg)
		}
	}
	if !strings.Contains(h.view(), "recovered notice value") {
		t.Fatal("tick did not recover notice output after feed failure")
	}
}

func TestSidebarBracketNavigation(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		want string
	}{{"]", "]", "1>alpha"}, {"[", "[", "> Running"}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, proj("alpha", false), proj("beta", false))
			h.start()
			h.send(press(tc.key))
			if !strings.Contains(h.view(), tc.want) {
				t.Fatalf("%s did not select %s", tc.name, tc.want)
			}
		})
	}
}

func TestFilterQuitKeys(t *testing.T) {
	for _, k := range []string{"ctrl+c", "q"} {
		t.Run(k, func(t *testing.T) {
			h := newHarness(t, proj("alpha", false))
			h.start()
			h.send(press("/"))
			msgs := h.send(press(k))
			quit := false
			for _, msg := range msgs {
				if _, ok := msg.(tea.QuitMsg); ok {
					quit = true
				}
			}
			if k == "ctrl+c" && !quit {
				t.Fatal("ctrl+c did not quit while filtering")
			}
			if k == "q" {
				if quit {
					t.Fatal("q quit while filtering")
				}
				if !strings.Contains(h.view(), "/q") {
					t.Fatalf("q missing from filter output: %s", h.view())
				}
			}
		})
	}
}

// While the filter box is open, j and k are typed into it, and the arrows, tab
// and shift+tab do nothing. The cursor stays on its row and the project stays
// put (ui.row-keys, ui.views).
func TestFilterBoxTakesRowKeysAsText(t *testing.T) {
	h := newHarness(t, four...)
	h.src.set(snap(four[0], []adjcli.Record{openRec(11, "a.one", "first question"), openRec(12, "a.two", "second question")}, nil))
	h.start()
	h.m.cursor = 1 // the second row
	h.send(press("/"))
	for _, k := range []string{"down", "j", "up", "k", "tab", "shift+tab"} {
		h.send(press(k))
	}
	if h.m.filter != "jk" || h.m.active != -1 {
		t.Fatalf("filter %q, project %d; want filter jk on Needs you", h.m.filter, h.m.active)
	}
	if got := detailText(h.m, 72); !strings.HasPrefix(got, "#12 open") {
		t.Fatalf("the cursor moved to %q", got)
	}
	if v := h.view(); !strings.Contains(v, "/jk") {
		t.Fatalf("j and k are not in the filter:\n%s", v)
	}
}

func TestFilterKeepsActiveProject(t *testing.T) {
	for _, action := range []string{"visible", "move"} {
		t.Run(action, func(t *testing.T) {
			h := newHarness(t, proj("alpha", false), proj("beta", false))
			h.start()
			h.send(press("1"))
			h.send(press("/"))
			h.send(press("b"))
			h.send(press("enter"))
			if action == "visible" {
				if !strings.Contains(h.view(), "1>alpha") {
					t.Fatalf("filtered active selection disappeared: %s", h.view())
				}
			} else {
				h.send(press("["))
				if !strings.Contains(h.view(), "0>Needs you") {
					t.Fatalf("movement lost active project position: %s", h.view())
				}
			}
		})
	}
}

// The sidebar numbers its first nine project rows in the gutter the selection
// arrow uses, dimmed, and shows a dimmed 0 on Needs you. The tenth project and
// the Running entry have none. The selected row keeps its number and shows the
// arrow after it, as 2>p02 (S85, S151).
func TestSidebarNumbersTheFirstNineProjectsDimmed(t *testing.T) {
	var projects []registry.Project
	for i := 1; i <= 10; i++ {
		projects = append(projects, proj(fmt.Sprintf("p%02d", i), false))
	}
	h := newHarness(t, projects...)
	h.start()
	h.send(press("2")) // p02 is selected
	h.view()
	dim := h.m.cfg.Theme.Dim
	for i := 1; i <= 10; i++ {
		name := fmt.Sprintf("p%02d", i)
		switch {
		case i == 2:
			if row := sidebarRow(h.m, "2>p02"); row == "" {
				t.Errorf("the selected row must show its number, then the arrow: %q", row)
			}
		case i <= 9:
			row := sidebarRow(h.m, fmt.Sprintf("%d %s", i, name))
			if row == "" || !strings.Contains(row, dim.Render(fmt.Sprint(i))) {
				t.Errorf("project %d has no dimmed number: %q", i, row)
			}
		default:
			if row := sidebarRow(h.m, "  "+name); row == "" {
				t.Errorf("the tenth project has no unnumbered row:\n%s", plainView(h.m))
			}
		}
	}
	v := plainView(h.m)
	if sidebarLine(v, "  Running") < 0 {
		t.Errorf("the Running entry must have no number:\n%s", v)
	}
	if row := sidebarRow(h.m, "0 Needs you"); row == "" || !strings.Contains(row, dim.Render("0")) {
		t.Errorf("Needs you has no dimmed 0 while another row is selected: %q", row)
	}
	h.send(press("0"))
	h.view()
	if row := sidebarRow(h.m, "0>Needs you"); row == "" {
		t.Errorf("the selected Needs you row must show 0, then the arrow: %q", row)
	}
}
