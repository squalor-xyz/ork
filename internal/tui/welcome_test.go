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
	paths     []string
	err       error
	projects  []registry.Project
	listErr   error
	lists     int
	removed   []string
	removeErr error
}

func (l *welcomeLive) ProjectsAdd(_ context.Context, path string) (registry.Project, error) {
	l.paths = append(l.paths, path)
	return registry.Project{Name: "mine", Path: path}, l.err
}
func (l *welcomeLive) Projects(context.Context) ([]registry.Project, error) {
	l.lists++
	return l.projects, l.listErr
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
	if !strings.Contains(h.view(), "No projects yet.") {
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
	if h.m.active != -1 || h.m.view != 0 || strings.Contains(h.view(), "No projects yet") || h.m.actionStatus != "Added mine /work/mine" {
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
		for _, state := range []string{"empty", "confirm", "error", "cancel", "found"} {
			h, l := welcomeHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			if state != "empty" && state != "found" {
				welcomePath(h, "/work/mine")
			}
			if state == "error" {
				l.err = errors.New("path refused verbatim")
				h.deliver(press("y"))
			}
			if state == "cancel" {
				h.deliver(press("n"))
			}
			if state == "found" {
				l.projects = []registry.Project{proj("elsewhere", false)}
				h.deliver(tickMsg{})
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
	if strings.Contains(h.view(), "Project path:") || !strings.Contains(h.view(), "+ add project") {
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
	want := "Type an absolute path, or leave it empty and press Enter to use the current directory (needs .slicer or .adjudicate.json)."
	if !strings.Contains(got, want) {
		t.Fatalf("hint=%s", got)
	}
}

func TestWelcomeWithOnlyRemovedProjects(t *testing.T) {
	h, _ := welcomeHarness(t)
	group := source.Snapshot{Project: registry.Project{Name: "removed"}, Unavailable: true, EngineRuns: []source.EngineRun{{ID: "missing", State: "implement", Unavailable: true, Error: `project "removed" is not registered`}}}
	h.deliver(unavailableProjectsMsg{groups: []source.Snapshot{group}})
	if !strings.Contains(h.view(), "No projects yet.") {
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

func TestWelcomeLeavesWhenAProjectAppearsElsewhere(t *testing.T) {
	for _, state := range []string{"idle", "confirm", "help"} {
		t.Run(state, func(t *testing.T) {
			h, l := welcomeHarness(t)
			h.deliver(tickMsg{})
			if l.lists != 1 {
				t.Fatalf("project list calls=%d, want 1", l.lists)
			}
			if state == "confirm" {
				welcomePath(h, "/work/mine")
			}
			if state == "help" {
				h.deliver(press("?"))
			}
			l.projects = []registry.Project{proj("elsewhere", false)}
			h.deliver(tickMsg{})
			if h.m.byName["elsewhere"] == nil || strings.Contains(h.view(), "No projects yet.") || h.m.welcome.editing || h.m.welcome.confirm != "" {
				t.Fatalf("Welcome did not leave after external add: %s", h.view())
			}
			if h.m.showHelp {
				t.Error("leaving Welcome kept help open")
			}
			if h.m.actionStatus != "Found elsewhere /projects/elsewhere" || !strings.Contains(h.view(), "Found elsewhere /projects/elsewhere") {
				t.Errorf("discovery status=%q", h.m.actionStatus)
			}
			if len(h.src.calls) != 1 || !h.m.projects[0].loaded {
				t.Fatalf("new project not fetched: %v", h.src.calls)
			}
			h.deliver(tickMsg{})
			if len(h.m.projects) != 1 || len(h.src.calls) != 1 {
				t.Fatal("discovery duplicated project or re-fetched unchanged registration")
			}
		})
	}
}

func TestWelcomePathExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, path := range []string{"~", "~/mine", "~someone/mine", "relative", "/work/mine"} {
		t.Run(path, func(t *testing.T) {
			h, l := welcomeHarness(t)
			welcomePath(h, path)
			want := path
			if path == "~" {
				want = home
			}
			if path == "~/mine" {
				want = filepath.Join(home, "mine")
			}
			if h.m.welcome.confirm != want {
				t.Errorf("confirm=%q, want %q", h.m.welcome.confirm, want)
			}
			h.deliver(press("y"))
			if len(l.paths) != 1 || l.paths[0] != want {
				t.Fatalf("daemon paths=%v, want %q", l.paths, want)
			}
		})
	}
}

func TestStatusLineWithNoProjectsHasNoRefreshedNever(t *testing.T) {
	h, _ := welcomeHarness(t)
	got := sgr.ReplaceAllString(h.m.statusLine(100), "")
	if strings.TrimSpace(got) != "0 projects" {
		t.Fatalf("empty status=%q", got)
	}
}

func TestWelcomeMessagesSitNextToTheField(t *testing.T) {
	for _, kind := range []string{"error", "cancel", "confirm"} {
		t.Run(kind, func(t *testing.T) {
			h, l := welcomeHarness(t)
			welcomePath(h, "/work/mine")
			want := "ork projects add"
			switch kind {
			case "error":
				l.err = errors.New("path refused verbatim")
				h.deliver(press("y"))
				want = l.err.Error()
			case "cancel":
				h.deliver(press("n"))
				want = "Cancelled"
			}
			lines := strings.Split(h.view(), "\n")
			field, message := -1, -1
			for i, line := range lines {
				if strings.Contains(line, "Project path:") {
					field = i
				}
				if strings.Contains(line, want) {
					message = i
				}
			}
			if field < 0 || message != field+1 {
				t.Fatalf("field=%d message=%d: %s", field, message, h.view())
			}
		})
	}
}

func TestWelcomeSuccessStatusIsCapitalised(t *testing.T) {
	h, _ := welcomeHarness(t)
	welcomePath(h, "/work/mine")
	h.deliver(press("y"))
	if h.m.actionStatus != "Added mine /work/mine" {
		t.Fatal(h.m.actionStatus)
	}
}

func TestProjectDiscoveryRetriesAndPreservesRegisteredProjects(t *testing.T) {
	h, l := welcomeHarness(t)
	h.deliver(projectAddedMsg{project: proj("mine", false)})
	l.listErr = errors.New("daemon unavailable")
	h.deliver(tickMsg{})
	if l.lists != 1 {
		t.Fatalf("project list calls=%d, want 1", l.lists)
	}
	if h.m.byName["mine"] == nil {
		t.Fatal("failed discovery removed registered project")
	}
	l.listErr = nil
	l.projects = []registry.Project{proj("mine", false), proj("other", false)}
	h.deliver(tickMsg{})
	if len(h.m.projects) != 2 || h.m.byName["other"] == nil {
		t.Fatal("discovery failed to retry or duplicated mine")
	}
}

func TestProjectDiscoveryHasOnlyOneRequestInFlight(t *testing.T) {
	h, l := welcomeHarness(t)
	_, first := h.m.Update(tickMsg{})
	_, second := h.m.Update(tickMsg{})
	results := append(exec(first), exec(second)...)
	if l.lists != 1 {
		t.Fatalf("project list calls=%d, want 1", l.lists)
	}
	for _, result := range results {
		if _, tick := result.(tickMsg); !tick {
			h.deliver(result)
		}
	}
	h.deliver(tickMsg{})
	if l.lists != 2 {
		t.Fatalf("completed discovery was not rearmed: %d", l.lists)
	}
}

func TestProjectDiscoveryWhileWelcomeAddIsPending(t *testing.T) {
	for _, sameProject := range []bool{false, true} {
		t.Run(fmt.Sprint(sameProject), func(t *testing.T) {
			h, l := welcomeHarness(t)
			welcomePath(h, "/work/mine")
			_, add := h.m.Update(press("y"))
			if add == nil || !h.m.welcome.pending {
				t.Fatal("add not pending")
			}
			result := add()
			project := proj("elsewhere", false)
			wantCount := 2
			if sameProject {
				project = registry.Project{Name: "mine", Path: "/work/mine"}
				wantCount = 1
			}
			l.projects = []registry.Project{project}
			h.deliver(tickMsg{})
			wantStatus := "Found " + project.Name + " " + project.Path
			if h.m.welcome.pending || h.m.welcome.editing || h.m.welcome.confirm != "" || strings.Contains(h.view(), "No projects yet.") {
				t.Fatal("discovery did not close pending Welcome")
			}
			if h.m.actionStatus != wantStatus {
				t.Errorf("status=%q, want %q", h.m.actionStatus, wantStatus)
			}
			h.deliver(result)
			if len(h.m.projects) != wantCount || h.m.byName["mine"] == nil || len(l.paths) != 1 {
				t.Fatalf("pending add result lost or duplicated: projects=%d paths=%v", len(h.m.projects), l.paths)
			}
			if !h.m.byName[project.Name].loaded || !h.m.byName["mine"].loaded {
				t.Fatal("project not fetched")
			}
		})
	}
}

func TestProjectDiscoveryReplacesUnavailableGroup(t *testing.T) {
	h, l := welcomeHarness(t)
	group := source.Snapshot{Project: registry.Project{Name: "mine"}, Unavailable: true, EngineRuns: []source.EngineRun{{ID: "missing", State: "implement", Unavailable: true}}}
	h.deliver(unavailableProjectsMsg{groups: []source.Snapshot{group}})
	l.projects = []registry.Project{proj("mine", false)}
	h.deliver(tickMsg{})
	p := h.m.byName["mine"]
	if len(h.m.projects) != 1 || p == nil || h.m.projects[0] != p || p.snap.Unavailable || !p.loaded || p.reg.Path != "/projects/mine" {
		t.Fatalf("unavailable group was not replaced: %+v", p)
	}
	if strings.Contains(h.view(), "No projects yet.") || h.m.actionStatus != "Found mine /projects/mine" {
		t.Fatalf("status=%q view=%s", h.m.actionStatus, h.view())
	}
	needs, _ := h.m.rows()
	for _, row := range needs {
		if row.engineID == "missing" {
			t.Error("removed run survived discovery")
		}
	}
	h.deliver(tickMsg{})
	if len(h.src.calls) != 1 {
		t.Fatalf("replacement re-fetched: %v", h.src.calls)
	}
}

func TestAddKeyWorksWithProjectsRegistered(t *testing.T) {
	for view := range views {
		t.Run(fmt.Sprint(view), func(t *testing.T) {
			h := newHarness(t, proj("alpha", false))
			l := &welcomeLive{fakeLive: &fakeLive{}}
			h.m.cfg.Live = l
			h.m.active, h.m.view = 0, view
			h.m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
			if !strings.Contains(wideHint(h), "+ add project") || !strings.Contains(wideHint(h), "- remove project") {
				t.Errorf("project hints missing: %s", wideHint(h))
			}
			h.deliver(press("+"))
			if !h.m.welcome.editing || !strings.Contains(h.view(), "Project path:") || strings.Contains(h.view(), "No projects yet.") {
				t.Fatalf("add field did not open over registered view: %s", h.view())
			}
			h.deliver(press("esc"))
			if h.m.welcome.editing || h.m.active != 0 || h.m.view != view {
				t.Fatal("escape did not restore view")
			}
			welcomePath(h, "/work/mine")
			if !strings.Contains(h.view(), "ork projects add '/work/mine'? [y/N]") || len(l.paths) != 0 {
				t.Fatal("missing add confirmation")
			}
			h.deliver(press("y"))
			if len(l.paths) != 1 || l.paths[0] != "/work/mine" || h.m.byName["mine"] == nil {
				t.Fatalf("add calls %v", l.paths)
			}
			if h.m.active != 0 || h.m.view != view {
				t.Fatal("add changed original view")
			}
		})
	}
}

func (l *welcomeLive) ProjectsRemove(_ context.Context, name string) error {
	l.removed = append(l.removed, name)
	return l.removeErr
}

func TestRemoveKeyConfirmsAndRemoves(t *testing.T) {
	for _, answer := range []string{"enter", "n", "N", "esc", "y", "Y"} {
		for _, count := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/%d", answer, count), func(t *testing.T) {
				h, l := welcomeHarness(t)
				h.m.registerProject(proj("alpha", false))
				if count == 2 {
					h.m.registerProject(proj("beta", false))
				}
				h.m.active = 0
				h.deliver(press("-"))
				if !strings.Contains(h.view(), "ork projects rm 'alpha'? [y/N] (Enter is No)") || len(l.removed) != 0 {
					t.Fatalf("missing safe named confirmation: %s", h.view())
				}
				_, cmd := h.m.Update(press(answer))
				if len(l.removed) != 0 {
					t.Fatal("removal outside command")
				}
				if answer != "y" && answer != "Y" {
					if cmd != nil || h.m.registeredProjectCount() != count || h.m.removing.name != "" || h.m.actionStatus != "Cancelled" {
						t.Fatal("cancel removed project")
					}
					return
				}
				if cmd == nil {
					t.Fatal("no removal command")
				}
				for _, k := range []string{"y", "-", "+", "enter"} {
					_, again := h.m.Update(press(k))
					if again != nil {
						t.Fatal("pending removal repeated")
					}
				}
				for _, msg := range exec(cmd) {
					h.deliver(msg)
				}
				if len(l.removed) != 1 || l.removed[0] != "alpha" || h.m.registeredProjectCount() != count-1 {
					t.Fatalf("removed %v, count %d", l.removed, h.m.registeredProjectCount())
				}
				if count == 1 && !strings.Contains(h.view(), "No projects yet.") {
					t.Fatal("last removal did not return to Welcome")
				}
				if count == 2 && (h.m.active < 0 || h.m.projects[h.m.active].reg.Name != "beta") {
					t.Fatal("remaining project not selected")
				}
			})
		}
	}
}

func TestRemoveRefusedWhileRunActive(t *testing.T) {
	for _, state := range []string{"running", "paused"} {
		t.Run(state, func(t *testing.T) {
			h, l := welcomeHarness(t)
			h.m.registerProject(proj("alpha", false))
			h.m.active = 0
			l.removeErr = &ipc.Error{Code: "project_busy", Message: "project alpha has unfinished runs: " + state}
			h.deliver(press("-"))
			h.deliver(press("y"))
			if len(l.removed) != 1 || h.m.registeredProjectCount() != 1 || !strings.Contains(h.view(), actionError(l.removeErr)) {
				t.Fatalf("daemon refusal lost: calls %v, view %s", l.removed, h.view())
			}
		})
	}
}

func TestProjectKeysHelpAndOverlays(t *testing.T) {
	for view := range views {
		t.Run(fmt.Sprint(view), func(t *testing.T) {
			h, _ := welcomeHarness(t)
			h.m.registerProject(proj("alpha", false))
			h.m.active, h.m.view = 0, view
			h.m.Update(tea.WindowSizeMsg{Width: 160, Height: 60})
			h.deliver(press("?"))
			if !strings.Contains(h.view(), "+ add project") || !strings.Contains(h.view(), "- remove active project") {
				t.Fatal("help omits project keys")
			}
			h.deliver(press("+"))
			if h.m.showHelp || !strings.Contains(h.view(), "Project path:") {
				t.Fatal("help hid add field")
			}
			h.deliver(press("esc"))
			h.deliver(press("-"))
			if !strings.Contains(h.view(), "ork projects rm 'alpha'") {
				t.Fatal("remove not available in view")
			}
			h.deliver(press("esc"))
			if h.m.view != view || h.m.active != 0 {
				t.Fatal("cancel changed view")
			}
		})
	}
}

func TestRemovalIgnoresStaleReadsAndKeepsHistoryReadOnly(t *testing.T) {
	h, l := welcomeHarness(t)
	h.m.registerProject(proj("alpha", false))
	h.m.active = 0
	p := h.m.byName["alpha"]
	p.snap.EngineRuns = []source.EngineRun{{ID: "old", State: "stopped"}}
	p.open, p.held = 2, 3
	list := h.m.discoverProjects()
	h.deliver(press("-"))
	h.deliver(press("y"))
	l.projects = []registry.Project{proj("alpha", false)}
	h.deliver(list())
	h.deliver(snapshotMsg{name: "alpha", snap: source.Snapshot{}})
	if !p.snap.Unavailable || h.m.registeredProjectCount() != 0 {
		t.Fatal("stale reads restored registration")
	}
	if len(p.snap.EngineRuns) != 1 || !p.snap.EngineRuns[0].Unavailable || p.open != 0 || p.held != 0 {
		t.Fatal("removed history still offers live actions or counts")
	}
}

func TestGoldenProjectManagement(t *testing.T) {
	for _, mono := range []bool{true, false} {
		for _, state := range []string{"add", "add-confirm", "remove", "remove-refused", "removed"} {
			h, l := welcomeHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			h.m.registerProject(proj("alpha", false))
			h.m.active = 0
			switch state {
			case "add":
				h.deliver(press("+"))
			case "add-confirm":
				welcomePath(h, "/work/mine")
			default:
				h.deliver(press("-"))
				if state == "remove-refused" {
					l.removeErr = &ipc.Error{Code: "project_busy", Message: "project alpha has unfinished runs: run-1"}
				}
				if state != "remove" {
					h.deliver(press("y"))
				}
			}
			theme := "color"
			if mono {
				theme = "mono"
			}
			checkGolden(t, fmt.Sprintf("project-%s-100x30-%s.txt", state, theme), h.m.View().Content)
		}
	}
}

func TestProjectKeysPreserveDetailAndTextInput(t *testing.T) {
	h := paneHarness(t, "stopped", "")
	pane, active, view := h.m.pane, h.m.active, h.m.view
	h.deliver(press("+"))
	if !strings.Contains(h.view(), "Project path:") {
		t.Fatal("detail pane swallowed add key")
	}
	h.deliver(press("esc"))
	if h.m.pane != pane || h.m.active != active || h.m.view != view {
		t.Fatal("add cancel lost detail pane")
	}
	h.m.pane = nil
	h.m.filtering = true
	h.deliver(press("+"))
	h.deliver(press("-"))
	if h.m.filter != "+-" || h.m.welcome.editing {
		t.Fatal("project keys swallowed filter text")
	}
}

func TestRemoveProjectNeedsSelectionAndDaemon(t *testing.T) {
	h, l := welcomeHarness(t)
	h.m.registerProject(proj("alpha", false))
	h.deliver(press("-"))
	h.deliver(press("y"))
	if len(l.removed) != 0 || !strings.Contains(h.view(), "Select a registered project to remove") {
		t.Fatal("aggregate removed a project")
	}
	h.m.active = 0
	h.m.cfg.Live = nil
	h.deliver(press("-"))
	h.deliver(press("y"))
	if h.m.registeredProjectCount() != 1 || !strings.Contains(h.view(), "No daemon connection; action unavailable") {
		t.Fatal("missing no-daemon refusal")
	}
	h.deliver(press("esc"))
	if h.m.removing.name != "" {
		t.Fatal("cannot cancel without daemon")
	}
}

func TestRemovePromptExplainsFilesAndPastRuns(t *testing.T) {
	h, _ := welcomeHarness(t)
	h.m.registerProject(proj("alpha", false))
	h.m.active = 0
	h.deliver(press("-"))
	lines := h.m.projectRemovalBody(200)
	want := "Files on disk are untouched; past runs stay in a read-only removed group."
	if len(lines) < 4 || lines[3] != indent+h.m.cfg.Theme.Dim.Render(want) {
		t.Fatalf("missing dim explanation below confirmation: %q", lines)
	}
	var body []string
	for _, line := range strings.Split(h.view(), "\n") {
		if _, main, ok := strings.Cut(line, "│"); ok {
			body = append(body, main)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(strings.Join(body, " ")), " "), want) {
		t.Fatal("explanation absent from screen")
	}
}

func TestAddHintSurvivesRemoveHintAt80Columns(t *testing.T) {
	h, _ := welcomeHarness(t)
	h.m.registerProject(proj("alpha", false))
	h.m.byName["alpha"].loaded = true
	h.m.active, h.m.view = 0, roadmapView
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	addDrop, removeDrop := -1, -1
	for _, entry := range h.m.contextHints() {
		switch entry.b.Help().Key {
		case "+":
			addDrop = entry.drop
		case "-":
			removeDrop = entry.drop
		}
	}
	if addDrop < 0 || removeDrop <= addDrop {
		t.Fatalf("remove needs its own lower priority: add=%d remove=%d", addDrop, removeDrop)
	}
	hint := hintLine(plainView(h.m))
	// remove goes first. Add goes next, before m matrix (S168, owner 2026-10-29:
	// m outlasts add and remove and drops before tab views), so at 80 columns
	// the line holds m matrix and tab views and not add.
	if strings.Contains(hint, "- remove project") || strings.Contains(hint, "+ add project") ||
		!strings.Contains(hint, "m matrix") || !strings.Contains(hint, "tab views") {
		t.Fatalf("remove goes before add, add before m matrix, m matrix before tab views: %s", hint)
	}
	checkGolden(t, "project-hints-80x24-mono.txt", h.m.View().Content)
}
