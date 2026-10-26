// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/source"
)

type welcomeLive struct {
	*fakeLive
	paths []string
	err   error
}

func (l *welcomeLive) ProjectsAdd(_ context.Context, path string) (registry.Project, error) {
	l.paths = append(l.paths, path)
	return registry.Project{Name: "mine", Path: path}, l.err
}
func welcomeHarness(t *testing.T) (*harness, *welcomeLive) {
	h := newHarness(t)
	l := &welcomeLive{fakeLive: &fakeLive{}}
	h.m.cfg.Live = l
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return h, l
}
func welcomePath(h *harness, path string) {
	h.deliver(press("+"))
	h.deliver(tea.PasteMsg{Content: path})
	h.deliver(press("enter"))
}
func TestTUIOpensOnTheWelcomeViewWithNoProjects(t *testing.T) {
	h, _ := welcomeHarness(t)
	for _, k := range []string{"tab", "shift+tab", "left", "right", "[", "]", "1", "9", "/", "s", "l", "x", "r"} {
		h.deliver(press(k))
		if h.m.view != 0 || h.m.active != -1 || h.m.filtering || h.m.sidebarHidden {
			t.Fatalf("%s changed welcome state", k)
		}
	}
	if !strings.Contains(h.view(), "No projects yet. Press + to add one") {
		t.Fatal(h.view())
	}
	h.deliver(press("?"))
	if !h.m.showHelp {
		t.Fatal("no help")
	}
	h.deliver(press("?"))
	if h.m.showHelp {
		t.Fatal("help not closed")
	}
	if len(exec(func() tea.Msg { _, c := h.m.Update(press("q")); return c() })) != 1 {
		t.Fatal("quit missing")
	}
}
func TestWelcomeAddAsksTheExactCommandThenAdds(t *testing.T) {
	for _, cancel := range []string{"enter", "n", "N", "esc"} {
		t.Run(cancel, func(t *testing.T) {
			h, l := welcomeHarness(t)
			welcomePath(h, " /work/my project's ")
			if !strings.Contains(h.view(), "ork projects add "+shellQuote("/work/my project's")+"? [y/N] (Enter is No)") || !strings.Contains(h.view(), "[y/N] (Enter is No)") {
				t.Fatal(h.view())
			}
			h.deliver(press(cancel))
			if len(l.paths) != 0 || h.m.actionStatus != "Cancelled" {
				t.Fatalf("calls %v, status %q", l.paths, h.m.actionStatus)
			}
		})
	}
	h, l := welcomeHarness(t)
	welcomePath(h, "/work/mine")
	_, cmd := h.m.Update(press("y"))
	if len(l.paths) != 0 || cmd == nil {
		t.Fatal("add must run in command")
	}

	value := h.m.welcome.field.Value()
	result := cmd() // the add remains pending until the returned message is applied
	for _, key := range []string{"y", "enter", "y"} {
		_, again := h.m.Update(press(key))
		if again != nil {
			t.Errorf("pending %s returned another command", key)
		}
	}
	if len(l.paths) != 1 || l.paths[0] != "/work/mine" {
		t.Errorf("pending add calls=%v, want exactly /work/mine", l.paths)
	}
	if got := h.m.welcome.field.Value(); got != value {
		t.Errorf("pending keys changed field: got %q want %q", got, value)
	}
	h.deliver(result)
	if len(l.paths) != 1 || l.paths[0] != "/work/mine" || len(h.m.projects) != 1 || h.m.byName["mine"] == nil {
		t.Fatalf("calls %v projects %+v", l.paths, h.m.projects)
	}
	if h.m.active != -1 || h.m.view != 0 || strings.Contains(h.view(), "No projects yet") || h.m.actionStatus != "added mine /work/mine" {
		t.Fatal(h.view())
	}
	if len(h.src.calls) != 1 {
		t.Fatalf("snapshot calls %v", h.src.calls)
	}
	if !h.m.poller.Start("mine") {
		t.Fatal("added project missing from poller")
	}
}
func TestWelcomeBareEnterOffersTheCurrentDirectory(t *testing.T) {
	for _, marker := range []string{"", ".slicer", ".adjudicate.json"} {
		t.Run(marker, func(t *testing.T) {
			h, l := welcomeHarness(t)
			dir := t.TempDir()
			if marker == ".slicer" {
				if err := os.Mkdir(filepath.Join(dir, marker), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if marker == ".adjudicate.json" {
				if err := os.WriteFile(filepath.Join(dir, marker), []byte("{}"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			h.m.cfg.Getwd = func() (string, error) { return dir, nil }
			welcomePath(h, "")
			if marker == "" {
				if h.m.actionStatus != dir+" holds neither .slicer nor .adjudicate.json; type a path" {
					t.Fatal(h.view())
				}
				h.deliver(press("y"))
				if len(l.paths) != 0 {
					t.Fatal(l.paths)
				}
			} else {
				if !strings.Contains(h.view(), "ork projects add") {
					t.Fatal(h.view())
				}
				h.deliver(press("Y"))
				if len(l.paths) != 1 || l.paths[0] != dir {
					t.Fatal(l.paths)
				}
			}
		})
	}
	h, _ := welcomeHarness(t)
	h.m.cfg.Getwd = func() (string, error) { return "", errors.New("cwd unavailable") }
	welcomePath(h, "")
	if h.m.actionStatus != "cwd unavailable" {
		t.Fatal(h.m.actionStatus)
	}
}
func TestWelcomeAddErrorShownVerbatim(t *testing.T) {
	for _, err := range []error{errors.New("path refused verbatim"), &ipc.Error{Code: "invalid_path", Message: "relative path refused"}} {
		h, l := welcomeHarness(t)
		l.err = err
		welcomePath(h, "relative path")
		h.deliver(press("y"))
		if h.m.actionStatus != actionError(err) || !strings.Contains(h.view(), actionError(err)) {
			t.Fatal(h.view())
		}
		l.err = nil
		h.deliver(press("enter"))
		h.deliver(press("y"))
		if len(l.paths) != 2 || l.paths[1] != "relative path" {
			t.Fatalf("field lost: %v", l.paths)
		}
	}
}
func TestWelcomeWithoutDaemon(t *testing.T) {
	h, _ := welcomeHarness(t)
	h.m.cfg.Live = nil
	welcomePath(h, "/work/mine")
	h.deliver(press("y"))
	if h.m.actionStatus != "No daemon connection; action unavailable" {
		t.Fatal(h.view())
	}
}
func TestGoldenWelcome(t *testing.T) {
	for _, mono := range []bool{false, true} {
		theme := "color"
		if mono {
			theme = "mono"
		}
		for _, adding := range []bool{false, true} {
			h, _ := welcomeHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			state := "empty"
			if adding {
				state = "confirm"
				welcomePath(h, "/work/mine")
			}
			checkGolden(t, fmt.Sprintf("welcome-%s-100x30-%s.txt", state, theme), h.m.View().Content)
		}
	}
}

func TestWelcomeAddClosesHelpAndEscReturns(t *testing.T) {
	h, _ := welcomeHarness(t)
	h.deliver(press("?"))
	h.deliver(press("+"))
	if h.m.showHelp || !strings.Contains(h.view(), "Project path:") {
		t.Fatal(h.view())
	}
	h.deliver(tea.PasteMsg{Content: "/work/mine"})
	h.deliver(press("esc"))
	if strings.Contains(h.view(), "Project path:") || !strings.Contains(h.view(), "Press + to add one") {
		t.Fatal(h.view())
	}
}

func TestWelcomeShowsIdleViewsAndNoSidebarSelection(t *testing.T) {
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprint(mono), func(t *testing.T) {
			h, _ := welcomeHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			want := " " + h.m.cfg.Theme.ViewIdle.Render("Overview │ Questions │ Waves │ Roadmap │ Decisions")
			if got := h.m.viewBar(); got != want {
				t.Errorf("view bar should be idle: got %q want %q", got, want)
			}
			for _, line := range h.m.sidebar(28, 30) {
				if strings.Contains(line, "Needs you") {
					t.Fatal("empty sidebar still offers Needs you")
				}
			}
			for _, name := range []string{"Overview", "Questions", "Waves", "Roadmap", "Decisions"} {
				if !strings.Contains(h.view(), name) {
					t.Errorf("view name hidden: %s", name)
				}
			}
		})
	}
}
func TestWelcomeFieldHasNoCursorDuringConfirmOrPending(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprint(pending), func(t *testing.T) {
			h, _ := welcomeHarness(t)
			welcomePath(h, "/work/mine")
			if pending {
				_, cmd := h.m.Update(press("y"))
				if cmd == nil {
					t.Fatal("no add command")
				}
			}
			want := indent + "Project path: " + h.m.cfg.Theme.Title.Render("/work/mine")
			found := false
			for _, line := range h.m.welcomeBody(72) {
				if strings.HasPrefix(line, indent+"Project path:") {
					found = true
					if line != want {
						t.Errorf("field still draws a cursor: got %q want %q", line, want)
					}
				}
			}
			if !found {
				t.Fatal("path field missing")
			}
		})
	}
}
func TestWelcomeHintNamesAbsolutePaths(t *testing.T) {
	h, _ := welcomeHarness(t)
	h.deliver(press("+"))
	got := sgr.ReplaceAllString(strings.Join(h.m.welcomeBody(300), "\n"), "")
	want := "Type an absolute path, or leave it empty and press Enter to use the current directory (needs .slicer or .adjudicate.json). Esc cancels."
	if !strings.Contains(got, want) {
		t.Fatalf("hint=%s", got)
	}
}

func TestWelcomeWithOnlyRemovedProjects(t *testing.T) {
	h, _ := welcomeHarness(t)
	group := source.Snapshot{Project: registry.Project{Name: "removed"}, Unavailable: true, EngineRuns: []source.EngineRun{{ID: "missing", State: "implement", Unavailable: true, Error: `project "removed" is not registered`}}}
	h.deliver(unavailableProjectsMsg{groups: []source.Snapshot{group}})
	if !strings.Contains(h.view(), "No projects yet. Press + to add one") {
		t.Error("removed-only groups hid Welcome")
	}
	h.deliver(press("1"))
	if h.m.active != 0 || !strings.Contains(h.view(), "missing") {
		t.Error("removed group unreachable from Welcome")
	}
	h.deliver(press("["))
	welcomePath(h, "/work/mine")
	if h.m.welcome.confirm != "/work/mine" {
		t.Errorf("removed-only Welcome cannot add project: %+v", h.m.welcome)
	}
	h.deliver(press("y"))
	if len(h.m.projects) != 2 || h.m.byName["mine"] == nil {
		t.Error("Welcome did not add registered project")
	}
}

func TestProjectAddedReplacesRemovedGroup(t *testing.T) {
	h := newHarness(t, proj("other", false))
	h.start()
	group := source.Snapshot{Project: registry.Project{Name: "mine"}, Unavailable: true, EngineRuns: []source.EngineRun{{ID: "missing", State: "implement", Unavailable: true}}}
	h.deliver(unavailableProjectsMsg{groups: []source.Snapshot{group}})
	h.deliver(projectAddedMsg{project: proj("mine", false)})
	count := 0
	for _, p := range h.m.projects {
		if p.reg.Name == "mine" {
			count++
			if p.snap.Unavailable {
				t.Error("registered project left removed group behind")
			}
		}
	}
	if count != 1 || len(h.m.projects) != 2 {
		t.Errorf("duplicate project after re-add: count=%d total=%d", count, len(h.m.projects))
	}
	if h.m.byName["mine"].snap.Unavailable {
		t.Error("byName still points to removed group")
	}
	needs, _ := h.m.rows()
	for _, r := range needs {
		if r.engineID == "missing" {
			t.Error("stale removed run remains in Needs you after re-add")
		}
	}
}
