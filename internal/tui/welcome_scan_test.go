// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/projscan"
	"github.com/squalor-xyz/ork/internal/registry"
)

// S302 (*proposed*): + with a folder that holds several projects opens a
// checklist of them.

const scanRoot = "/work/code"

func scanCands() []projscan.Candidate {
	return []projscan.Candidate{
		{Path: scanRoot + "/alpha", Name: "alpha", Slicer: true, Adj: true, Git: true},
		{Path: scanRoot + "/tools/beta", Name: "beta", Adj: true},
		{Path: scanRoot + "/gamma", Name: "gamma", Git: true},
	}
}

// scanHarness is a harness whose folder scan is a fixed list, so the views do
// not hold a temp path. It starts on Welcome when it has no projects.
func scanHarness(t *testing.T, cands []projscan.Candidate, projects ...registry.Project) (*harness, *welcomeLive) {
	t.Helper()
	h := newHarness(t, projects...)
	l := &welcomeLive{fakeLive: &fakeLive{}, byBase: true}
	h.m.cfg.Live = l
	h.m.cfg.Scan = func(root string) (projscan.Result, error) {
		if root != scanRoot {
			return projscan.Result{}, errors.New("not scanned: " + root)
		}
		return projscan.Result{Candidates: cands}, nil
	}
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return h, l
}

// row is the line of the plain view that names the project.
func row(view, name string) string {
	re := regexp.MustCompile(`\] ` + regexp.QuoteMeta(name) + `(\s|$)`)
	for _, line := range strings.Split(view, "\n") {
		if re.MatchString(line) {
			return line
		}
	}
	return ""
}

func TestScanOfAFolderOfProjectsOpensTheChecklist(t *testing.T) {
	h, l := scanHarness(t, scanCands())
	welcomePath(h, scanRoot)
	view := h.view()
	if h.m.welcome.scan == nil || h.m.welcome.confirm != "" {
		t.Fatalf("no checklist:\n%s", view)
	}
	for _, want := range []string{"alpha", "beta", "gamma", "slicer+adj", "adj ", "git", "tools/beta", scanRoot} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "ork projects add") || len(l.paths) != 0 {
		t.Errorf("single-add confirmation or a call: %v\n%s", l.paths, view)
	}
}

func TestChecklistStartsWithTrackedRowsTicked(t *testing.T) {
	h, _ := scanHarness(t, scanCands())
	welcomePath(h, scanRoot)
	view := h.view()
	for name, want := range map[string]string{"alpha": "[x]", "beta": "[x]", "gamma": "[ ]"} {
		if line := row(view, name); !strings.Contains(line, want) {
			t.Errorf("%s row %q, want %s", name, line, want)
		}
	}
}

func TestChecklistKeysTickAllNoneAndMoveTheCursor(t *testing.T) {
	h, l := scanHarness(t, scanCands())
	welcomePath(h, scanRoot)
	ticks := func() string {
		v := h.view()
		return fmt.Sprintf("%s%s%s", mark(row(v, "alpha")), mark(row(v, "beta")), mark(row(v, "gamma")))
	}
	if got := ticks(); got != "xx " {
		t.Fatalf("start %q", got)
	}
	h.deliver(press("n"))
	if got := ticks(); got != "   " {
		t.Fatalf("n: %q", got)
	}
	h.deliver(press("a"))
	if got := ticks(); got != "xxx" {
		t.Fatalf("a: %q", got)
	}
	h.deliver(press(" ")) // the cursor starts on the first row
	if got := ticks(); got != " xx" {
		t.Fatalf("space on row 1: %q", got)
	}
	h.deliver(press("j"))
	h.deliver(press(" "))
	if got := ticks(); got != "  x" {
		t.Fatalf("j then space: %q", got)
	}
	h.deliver(press("down"))
	h.deliver(press("down")) // stops at the last row
	h.deliver(press(" "))
	if got := ticks(); got != "   " {
		t.Fatalf("down down space: %q", got)
	}
	h.deliver(press("k"))
	h.deliver(press("up"))
	h.deliver(press("up")) // stops at the first row
	h.deliver(press(" "))
	if got := ticks(); got != "x  " {
		t.Fatalf("k up up space: %q", got)
	}
	_, alpha, _ := strings.Cut(row(h.view(), "alpha"), "│")
	if !strings.HasPrefix(strings.TrimLeft(alpha, " "), ">") {
		t.Errorf("cursor not on the first row:\n%s", h.view())
	}
	if len(l.paths) != 0 {
		t.Fatalf("keys added projects: %v", l.paths)
	}
}

// mark is the box of a checklist row: x when ticked, a space when not.
func mark(line string) string {
	_, rest, ok := strings.Cut(line, "[")
	if !ok || len(rest) < 2 {
		return "?"
	}
	return rest[:1]
}

func TestChecklistAddsTheTickedRowsOneByOneAndNamesEachOutcome(t *testing.T) {
	h, l := scanHarness(t, scanCands())
	// The first ticked row is refused; the second still goes in.
	l.failPaths = map[string]error{scanRoot + "/alpha": &ipc.Error{Code: "duplicate_name", Message: `a project named "alpha" is already registered`}}
	welcomePath(h, scanRoot)
	h.deliver(press("y"))
	want := []string{scanRoot + "/alpha", scanRoot + "/tools/beta"}
	if fmt.Sprint(l.paths) != fmt.Sprint(want) {
		t.Fatalf("added %v, want %v", l.paths, want)
	}
	view := h.view()
	for _, w := range []string{"added   beta  " + scanRoot + "/tools/beta", "failed  alpha: duplicate_name: a project named \"alpha\" is"} {
		if !strings.Contains(view, w) {
			t.Errorf("results lack %q:\n%s", w, view)
		}
	}
	if strings.Contains(view, "gamma") {
		t.Errorf("an unticked row in the results:\n%s", view)
	}
	// The project that was added is registered and polled; the failed one is not.
	if h.m.byName["beta"] == nil || h.m.byName["alpha"] != nil || !h.m.poller.Start("beta") {
		t.Errorf("registered: %v", h.m.byName)
	}
	h.deliver(press("enter"))
	if h.m.welcome.scan != nil || h.m.welcome.editing || strings.Contains(h.view(), "failed") {
		t.Fatalf("results not closed:\n%s", h.view())
	}
}

// Enter adds nothing in the checklist (owner, 2026-11-07): only y adds.
func TestChecklistEnterDoesNothing(t *testing.T) {
	h, l := scanHarness(t, scanCands())
	welcomePath(h, scanRoot)
	before := h.view()
	if _, c := h.m.Update(press("enter")); c != nil {
		t.Fatal("enter started a command")
	}
	if len(l.paths) != 0 || h.m.welcome.pending || h.m.welcome.scan == nil || h.view() != before {
		t.Fatalf("enter changed the checklist: %v\n%s", l.paths, h.view())
	}
	if offered(h.m, "enter", "add ticked") {
		t.Error("the hint offers enter")
	}
}

func TestChecklistWithNothingTickedAddsNothing(t *testing.T) {
	h, l := scanHarness(t, scanCands())
	welcomePath(h, scanRoot)
	h.deliver(press("n"))
	h.deliver(press("y"))
	if len(l.paths) != 0 || h.m.welcome.scan == nil || !strings.Contains(h.view(), "Nothing ticked") {
		t.Fatalf("paths %v\n%s", l.paths, h.view())
	}
	if offered(h.m, "y", "add ticked") || strings.Contains(hintLine(h.view()), "add ticked") {
		t.Errorf("y is offered with nothing ticked: %q", hintLine(h.view()))
	}
	h.deliver(press(" "))
	if !strings.Contains(hintLine(h.view()), "y add ticked") {
		t.Errorf("y is not offered with a row ticked: %q", hintLine(h.view()))
	}
}

func TestChecklistWithoutADaemonAddsNothing(t *testing.T) {
	h, l := scanHarness(t, scanCands())
	welcomePath(h, scanRoot)
	h.m.cfg.Live = nil
	h.deliver(press("y"))
	if len(l.paths) != 0 || h.m.welcome.scan == nil || !strings.Contains(h.view(), "No daemon connection") {
		t.Fatalf("%v\n%s", l.paths, h.view())
	}
	if offered(h.m, "y", "add ticked") {
		t.Error("y is offered with no daemon")
	}
}

func TestChecklistEscReturnsToThePathField(t *testing.T) {
	h, l := scanHarness(t, scanCands())
	welcomePath(h, scanRoot)
	if h.m.welcome.scan == nil {
		t.Fatal("no checklist")
	}
	h.deliver(press("esc"))
	if h.m.welcome.scan != nil || !h.m.welcome.editing || h.m.welcome.field.Value() != scanRoot || len(l.paths) != 0 {
		t.Fatalf("%+v\n%s", h.m.welcome, h.view())
	}
	if !strings.Contains(h.view(), "Project path:") {
		t.Fatal(h.view())
	}
}

func TestChecklistGreysRegisteredRowsAndBlocksANameClash(t *testing.T) {
	cands := append(scanCands(),
		projscan.Candidate{Path: scanRoot + "/delta", Name: "delta", Slicer: true},
		projscan.Candidate{Path: scanRoot + "/other/epsilon", Name: "epsilon", Slicer: true},
	)
	h, l := scanHarness(t, cands,
		registry.Project{Name: "delta", Path: scanRoot + "/delta"},
		registry.Project{Name: "epsilon", Path: "/elsewhere/epsilon"},
	)
	h.deliver(press("+"))
	h.deliver(tea.PasteMsg{Content: scanRoot})
	h.deliver(press("enter"))
	view := h.view()
	if h.m.welcome.scan == nil {
		t.Fatalf("no checklist:\n%s", view)
	}
	delta, epsilon := row(view, "delta"), row(view, "epsilon")
	if mark(delta) == "x" || mark(epsilon) == "x" || !strings.Contains(view, "already registered as delta") || !strings.Contains(view, `name "epsilon" is registered for /elsewhere/epsilon`) {
		t.Fatalf("blocked rows:\n%s", view)
	}
	// Neither blocked row can be ticked, one at a time or with a.
	h.deliver(press("a"))
	for i := 0; i < 3; i++ {
		h.deliver(press("j"))
	}
	h.deliver(press(" ")) // on delta
	h.deliver(press("j"))
	h.deliver(press(" ")) // on epsilon
	view = h.view()
	if !strings.Contains(view, "3 of 3 ticked") {
		t.Fatalf("count of ticked rows:\n%s", view)
	}
	if mark(row(view, "delta")) == "x" || mark(row(view, "epsilon")) == "x" {
		t.Fatalf("a blocked row was ticked:\n%s", view)
	}
	h.deliver(press("y"))
	for _, p := range l.paths {
		if strings.HasSuffix(p, "/delta") || strings.HasSuffix(p, "/epsilon") {
			t.Fatalf("blocked row added: %v", l.paths)
		}
	}
	if len(l.paths) != 3 {
		t.Fatalf("a ticks the three free rows: %v", l.paths)
	}
}

// A greyed row is drawn faint, the way the theme draws other dimmed text.
func TestChecklistDrawsBlockedRowsDim(t *testing.T) {
	cands := append(scanCands(), projscan.Candidate{Path: scanRoot + "/delta", Name: "delta", Slicer: true})
	h, _ := scanHarness(t, cands, registry.Project{Name: "delta", Path: scanRoot + "/delta"})
	h.deliver(press("+"))
	h.deliver(tea.PasteMsg{Content: scanRoot})
	h.deliver(press("enter"))
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	var delta, beta string
	for _, line := range strings.Split(h.m.View().Content, "\n") {
		if strings.Contains(line, "[-] delta") {
			delta = line
		}
		if strings.Contains(line, "[x] beta") {
			beta = line
		}
	}
	const faint = "\x1b[2m"
	if i := strings.Index(delta, "delta"); i < 0 || !strings.Contains(delta[:i], faint) {
		t.Errorf("registered row not faint: %q", delta)
	}
	if i := strings.Index(beta, "beta"); i < 0 || strings.Contains(beta[:i], faint) {
		t.Errorf("free row faint: %q", beta)
	}
}

func TestFolderThatIsItselfAProjectKeepsTheSingleAdd(t *testing.T) {
	h, l := scanHarness(t, []projscan.Candidate{{Path: scanRoot, Name: "code", Slicer: true}})
	welcomePath(h, scanRoot)
	if h.m.welcome.scan != nil || h.m.welcome.confirm != scanRoot || !strings.Contains(h.view(), "ork projects add '"+scanRoot+"'? [y/N]") {
		t.Fatalf("%+v\n%s", h.m.welcome, h.view())
	}
	h.deliver(press("y"))
	if len(l.paths) != 1 || l.paths[0] != scanRoot {
		t.Fatal(l.paths)
	}
}

func TestFolderWithNoProjectKeepsTheSingleAdd(t *testing.T) {
	h, _ := scanHarness(t, nil)
	welcomePath(h, scanRoot)
	if h.m.welcome.scan != nil || h.m.welcome.confirm != scanRoot {
		t.Fatalf("%+v\n%s", h.m.welcome, h.view())
	}
}

func TestFolderThatCannotBeScannedKeepsTheSingleAdd(t *testing.T) {
	h, _ := scanHarness(t, scanCands())
	// A scan that fails keeps whatever it found out of the checklist.
	h.m.cfg.Scan = func(string) (projscan.Result, error) {
		return projscan.Result{Candidates: scanCands()}, errors.New("permission denied")
	}
	welcomePath(h, "/work/missing")
	if h.m.welcome.scan != nil || h.m.welcome.confirm != "/work/missing" {
		t.Fatalf("%+v\n%s", h.m.welcome, h.view())
	}
}

func TestBareEnterScansTheCurrentDirectoryWhenItIsNotAProject(t *testing.T) {
	h, _ := scanHarness(t, scanCands())
	h.m.cfg.Getwd = func() (string, error) { return scanRoot, nil }
	welcomePath(h, "")
	if h.m.welcome.scan == nil {
		t.Fatalf("no checklist:\n%s", h.view())
	}
}

func TestChecklistFromTheRealScan(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a/.slicer", "group/b/.git", "plain/x"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h, l := welcomeHarness(t) // no Scan set: the default walks the folder
	l.byBase = true
	welcomePath(h, root)
	view := h.view()
	if h.m.welcome.scan == nil || row(view, "a") == "" || row(view, "b") == "" || strings.Contains(view, "plain") {
		t.Fatalf("%s", view)
	}
	h.deliver(press("a"))
	h.deliver(press("y"))
	if fmt.Sprint(l.paths) != fmt.Sprint([]string{filepath.Join(root, "a"), filepath.Join(root, "group", "b")}) {
		t.Fatalf("added %v", l.paths)
	}
}

func TestChecklistHints(t *testing.T) {
	h, _ := scanHarness(t, scanCands())
	welcomePath(h, scanRoot)
	for _, w := range [][2]string{{"space", "tick"}, {"a", "all"}, {"n", "none"}, {"y", "add ticked"}, {"esc", "back"}} {
		if !offered(h.m, w[0], w[1]) {
			t.Errorf("hint %s %s missing: %+v", w[0], w[1], h.m.contextHints())
		}
	}
	h.deliver(press("y"))
	if !offered(h.m, "enter", "close") || offered(h.m, "y", "add ticked") {
		t.Errorf("results hints: %+v", h.m.contextHints())
	}
}

func TestChecklistPendingIgnoresKeysExceptQuit(t *testing.T) {
	h, l := scanHarness(t, scanCands())
	welcomePath(h, scanRoot)
	_, add := h.m.Update(press("y"))
	if add == nil || h.m.welcome.scan == nil || !strings.Contains(h.view(), "Adding") {
		t.Fatalf("not pending:\n%s", h.view())
	}
	for _, k := range []string{"y", "n", "a", " ", "enter", "esc", "j"} {
		if _, c := h.m.Update(press(k)); c != nil {
			t.Errorf("%q started a command while pending", k)
		}
	}
	if len(l.paths) != 0 {
		t.Fatalf("keys added during pending: %v", l.paths)
	}
	for _, out := range exec(add) {
		h.deliver(out)
	}
	if len(l.paths) != 2 {
		t.Fatalf("added %v", l.paths)
	}
	if len(exec(func() tea.Msg { _, c := h.m.Update(press("ctrl+c")); return c() })) != 1 {
		t.Fatal("quit missing")
	}
}

func TestChecklistListLongerThanTheBodyKeepsTheCursorVisible(t *testing.T) {
	var cands []projscan.Candidate
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("p%02d", i)
		cands = append(cands, projscan.Candidate{Path: scanRoot + "/" + name, Name: name, Slicer: true})
	}
	h, _ := scanHarness(t, cands)
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	welcomePath(h, scanRoot)
	for i := 0; i < 20; i++ {
		h.deliver(press("j"))
	}
	view := plainView(h.m)
	if !strings.Contains(view, "p20") || strings.Contains(view, "p00 ") || len(strings.Split(view, "\n")) != 24 {
		t.Fatalf("%s", view)
	}
	h.deliver(press("a"))
	if !strings.Contains(plainView(h.m), "30 of 30 ticked") {
		t.Fatalf("%s", plainView(h.m))
	}
}

func TestGoldenChecklist(t *testing.T) {
	cands := append(scanCands(),
		projscan.Candidate{Path: scanRoot + "/delta", Name: "delta", Slicer: true},
		projscan.Candidate{Path: scanRoot + "/other/epsilon", Name: "epsilon", Slicer: true},
	)
	projects := []registry.Project{
		{Name: "delta", Path: scanRoot + "/delta"},
		{Name: "epsilon", Path: "/elsewhere/epsilon"},
	}
	for _, size := range [][2]int{{100, 30}, {80, 24}, {140, 40}} {
		for _, mono := range []bool{false, true} {
			theme := map[bool]string{false: "color", true: "mono"}[mono]
			for _, state := range []string{"list", "results"} {
				h, l := scanHarness(t, cands, projects...)
				h.m.cfg.Theme = NewTheme(mono)
				l.failPaths = map[string]error{scanRoot + "/tools/beta": &ipc.Error{Code: "duplicate_name", Message: `a project named "beta" is already registered`}}
				h.deliver(press("+"))
				h.deliver(tea.PasteMsg{Content: scanRoot})
				h.deliver(press("enter"))
				if state == "results" {
					h.deliver(press("y"))
				}
				h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				checkGolden(t, fmt.Sprintf("checklist-%s-%dx%d-%s.txt", state, size[0], size[1], theme), h.m.View().Content)
			}
		}
	}
}

func TestChecklistIgnoresPasteAndTextKeys(t *testing.T) {
	h, l := scanHarness(t, scanCands())
	welcomePath(h, scanRoot)
	before := h.view()
	h.deliver(tea.PasteMsg{Content: "/other/path"})
	for _, k := range []string{"q", "?", "x", "-", "+", "tab", "1"} {
		if _, c := h.m.Update(press(k)); c != nil {
			t.Errorf("%q started a command", k)
		}
	}
	if h.m.welcome.field.Value() != scanRoot || h.m.welcome.scan == nil || h.view() != before || len(l.paths) != 0 {
		t.Fatalf("state changed:\n%s", h.view())
	}
	if len(exec(func() tea.Msg { _, c := h.m.Update(press("ctrl+c")); return c() })) != 1 {
		t.Fatal("ctrl+c does not quit")
	}
}

// With projects registered, the checklist opens from the main views and leaves
// the view and selection as they were when it closes.
func TestChecklistFromAMainViewReturnsToIt(t *testing.T) {
	h, _ := scanHarness(t, scanCands(), proj("slicer", false))
	h.start()
	h.deliver(press("1"))
	view, active := h.m.view, h.m.active
	h.deliver(press("+"))
	h.deliver(tea.PasteMsg{Content: scanRoot})
	h.deliver(press("enter"))
	if h.m.welcome.scan == nil || !strings.Contains(h.view(), "Add projects") {
		t.Fatalf("no checklist:\n%s", h.view())
	}
	h.deliver(press("y"))
	h.deliver(press("enter"))
	if h.m.welcome.scan != nil || h.m.view != view || h.m.active != active || strings.Contains(h.view(), "Add projects") {
		t.Fatalf("view %d active %d, want %d %d\n%s", h.m.view, h.m.active, view, active, h.view())
	}
	if h.m.byName["alpha"] == nil || h.m.byName["beta"] == nil || h.m.byName["gamma"] != nil {
		t.Fatalf("registered %v", h.m.byName)
	}
}

// A relative path is never scanned: the daemon refuses it with its own message.
func TestChecklistNeverScansARelativePath(t *testing.T) {
	h, _ := scanHarness(t, scanCands())
	h.m.cfg.Scan = func(root string) (projscan.Result, error) {
		t.Errorf("scanned %q", root)
		return projscan.Result{Candidates: scanCands()}, nil
	}
	welcomePath(h, "relative/dir")
	if h.m.welcome.scan != nil || h.m.welcome.confirm != "relative/dir" {
		t.Fatalf("%+v", h.m.welcome)
	}
}

// Of two same-named rows the later is blocked, with the reason, and a does not
// tick it and y does not add it.
func TestChecklistBlocksTheLaterOfTwoSameNamedRows(t *testing.T) {
	cands := []projscan.Candidate{
		{Path: scanRoot + "/one/app", Name: "app", Slicer: true},
		{Path: scanRoot + "/two/app", Name: "app", Slicer: true},
		{Path: scanRoot + "/zed", Name: "zed", Slicer: true},
	}
	h, l := scanHarness(t, cands)
	welcomePath(h, scanRoot)
	view := h.view()
	if !strings.Contains(view, "2 of 2 ticked") || !strings.Contains(view, `name "app" is also found at `+scanRoot+`/one/app; add it with ork`) {
		t.Fatalf("%s", view)
	}
	if got := mark(row(view, "app")); got != "x" {
		t.Errorf("first app row: %q", got)
	}
	if !strings.Contains(view, "[-] app") {
		t.Errorf("no blocked app row:\n%s", view)
	}
	h.deliver(press("a"))
	h.deliver(press("y"))
	if fmt.Sprint(l.paths) != fmt.Sprint([]string{scanRoot + "/one/app", scanRoot + "/zed"}) {
		t.Fatalf("added %v", l.paths)
	}
}

func TestChecklistShowsAStoppedScan(t *testing.T) {
	h, _ := scanHarness(t, scanCands())
	inner := h.m.cfg.Scan
	h.m.cfg.Scan = func(root string) (projscan.Result, error) {
		r, err := inner(root)
		r.Stopped, r.Folders = true, 500
		return r, err
	}
	welcomePath(h, scanRoot)
	if view := strings.Join(strings.Fields(mainPane(h.view())), " "); !strings.Contains(view, "Stopped after 500 folders and 3 projects; scan a narrower folder to see more.") {
		t.Fatalf("%s", view)
	}
	h2, _ := scanHarness(t, scanCands())
	welcomePath(h2, scanRoot)
	if strings.Contains(h2.view(), "Stopped after") {
		t.Fatal("a complete scan says it stopped")
	}
}

// Names, paths and errors are shown as one plain line each (S302 review): an
// escape sequence or a line break in a folder name cannot move the screen.
func TestChecklistShowsHostileNamesAsPlainText(t *testing.T) {
	var cands []projscan.Candidate
	for i := 0; i < 12; i++ {
		cands = append(cands, projscan.Candidate{Path: fmt.Sprintf("%s/p%02d", scanRoot, i), Name: fmt.Sprintf("p%02d", i), Slicer: true})
	}
	evil := scanRoot + "/ev\x1b[31mil\nname\x1b]0;title\x07"
	cands = append(cands, projscan.Candidate{Path: evil, Name: filepath.Base(evil), Slicer: true})
	// A second folder with that name is blocked, and its reason holds the name and the first path.
	dup := scanRoot + "/zdup/" + filepath.Base(evil)
	cands = append(cands, projscan.Candidate{Path: dup, Name: filepath.Base(evil), Slicer: true})
	// A third is added fine, and its name shows in the results.
	green := scanRoot + "/v\x1b[32mx"
	cands = append(cands, projscan.Candidate{Path: green, Name: filepath.Base(green), Slicer: true})
	h, l := scanHarness(t, cands)
	l.failPaths = map[string]error{evil: errors.New("refused\nsecond line\x1b[2J")}
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	welcomePath(h, scanRoot)
	for i := 0; i < 13; i++ {
		h.deliver(press("j"))
	}
	raw := h.m.View().Content
	plain := plainView(h.m)
	if strings.Contains(raw, "\x1b[31m") || strings.Contains(raw, "\x1b]") || strings.Contains(raw, "\x07") || strings.Contains(raw, "\x1b[2J") || strings.Contains(raw, "\x1b[32m") {
		t.Fatalf("an escape sequence got through: %q", raw)
	}
	if !strings.Contains(plain, "evil name") || len(strings.Split(plain, "\n")) != 24 {
		t.Fatalf("%d lines\n%s", len(strings.Split(plain, "\n")), plain)
	}
	if !strings.Contains(plain, "is also") || !strings.Contains(plain, "found at /work/code/evil name") {
		t.Fatalf("no reason shown:\n%s", plain)
	}
	h.deliver(press("y"))
	plain = plainView(h.m)
	// The sidebar lists a registered project under its name as the daemon holds it; this
	// slice leaves that as it was, so only the pane beside it is checked.
	raw = mainPane(h.m.View().Content)
	if strings.ContainsAny(raw, "\x07") || strings.Contains(raw, "\x1b[2J") || strings.Contains(raw, "\x1b[32m") || strings.Contains(raw, "\x1b[31m") ||
		!strings.Contains(plain, "refused second line") || !strings.Contains(plain, "added   vx  ") || len(strings.Split(plain, "\n")) != 24 {
		t.Fatalf("results: %q", raw)
	}
}

// mainPane is the view without its sidebar column.
func mainPane(view string) string {
	var out []string
	for _, line := range strings.Split(view, "\n") {
		_, rest, _ := strings.Cut(line, "│")
		out = append(out, rest)
	}
	return strings.Join(out, "\n")
}

func TestChecklistScrollsBackUp(t *testing.T) {
	h, _ := checklistOf(t, 30)
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 0; i < 25; i++ {
		h.deliver(press("j"))
	}
	down := plainView(h.m)
	if !strings.Contains(down, "p25") || strings.Contains(down, "p00 ") || !strings.Contains(down, "(rows ") {
		t.Fatalf("%s", down)
	}
	for i := 0; i < 25; i++ {
		h.deliver(press("k"))
	}
	up := plainView(h.m)
	if !strings.Contains(up, "p00 ") || strings.Contains(up, "p25") || !strings.Contains(up, "(rows 1-") {
		t.Fatalf("%s", up)
	}
}

// checklistOf opens a checklist of n tracked rows p00, p01 and so on.
func checklistOf(t *testing.T, n int) (*harness, *welcomeLive) {
	t.Helper()
	var cands []projscan.Candidate
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("p%02d", i)
		cands = append(cands, projscan.Candidate{Path: scanRoot + "/" + name, Name: name, Slicer: true})
	}
	h, l := scanHarness(t, cands)
	welcomePath(h, scanRoot)
	return h, l
}

// The cursor row is a full-width reverse bar, as on Waves.
func TestChecklistCursorIsAFullWidthBar(t *testing.T) {
	h, _ := scanHarness(t, scanCands())
	welcomePath(h, scanRoot)
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	bar := strings.TrimSuffix(NewTheme(true).TabActive.Render("x"), "x\x1b[m")
	if bar == "" {
		t.Fatal("no bar style")
	}
	for _, line := range strings.Split(h.m.View().Content, "\n") {
		hasBar := strings.Contains(line, bar+"> [x] alpha") || strings.Contains(line, bar+"> ")
		switch {
		case strings.Contains(line, "alpha") && strings.Contains(line, "["):
			if !hasBar {
				t.Errorf("cursor row has no bar: %q", line)
			}
		case (strings.Contains(line, "beta") || strings.Contains(line, "gamma")) && strings.Contains(line, "["):
			if strings.Contains(line, bar) {
				t.Errorf("another row has the bar: %q", line)
			}
		}
	}
}

func TestResultsIndentAFailedRowsContinuationToTheNameColumn(t *testing.T) {
	h, l := scanHarness(t, scanCands())
	l.failPaths = map[string]error{scanRoot + "/alpha": errors.New(strings.Repeat("long refusal ", 12))}
	welcomePath(h, scanRoot)
	h.deliver(press("y"))
	lines := strings.Split(h.view(), "\n")
	for i, line := range lines {
		if strings.Contains(line, "failed  alpha: long refusal") {
			_, rest, _ := strings.Cut(lines[i+1], "│")
			if !strings.HasPrefix(rest, "  "+strings.Repeat(" ", 8)+"long") && !strings.HasPrefix(rest, "  "+strings.Repeat(" ", 8)+"refusal") {
				t.Fatalf("continuation %q", rest)
			}
			return
		}
	}
	t.Fatalf("no failed row:\n%s", h.view())
}

func TestPathGuidanceTakesTwoLinesAt100Columns(t *testing.T) {
	h, _ := welcomeHarness(t)
	h.deliver(press("+"))
	lines := strings.Split(h.view(), "\n")
	var guide []string
	for i, line := range lines {
		if strings.Contains(line, "Project path:") {
			for _, next := range lines[i+1:] {
				_, rest, _ := strings.Cut(next, "│")
				if strings.TrimSpace(rest) == "" {
					break
				}
				guide = append(guide, strings.TrimSpace(rest))
			}
		}
	}
	const want = "Type an absolute path to a project, or to a folder to pick projects from it. Leave it empty and press Enter to use the current directory."
	if len(guide) != 2 || strings.Join(guide, " ") != want {
		t.Fatalf("guidance %q", guide)
	}
}

func TestGoldenChecklistScrolledAndSingle(t *testing.T) {
	for _, mono := range []bool{false, true} {
		theme := map[bool]string{false: "color", true: "mono"}[mono]
		h, _ := checklistOf(t, 30)
		h.m.cfg.Theme = NewTheme(mono)
		h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		for i := 0; i < 20; i++ {
			h.deliver(press("j"))
		}
		checkGolden(t, "checklist-scrolled-80x24-"+theme+".txt", h.m.View().Content)

		one, _ := scanHarness(t, []projscan.Candidate{{Path: scanRoot + "/only/app", Name: "app", Slicer: true, Git: true}})
		one.m.cfg.Theme = NewTheme(mono)
		welcomePath(one, scanRoot)
		one.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		checkGolden(t, "checklist-one-80x24-"+theme+".txt", one.m.View().Content)
	}
}

func TestChecklistShowsAHostileFolderNameAsPlainText(t *testing.T) {
	h, _ := scanHarness(t, scanCands())
	h.m.cfg.Scan = func(root string) (projscan.Result, error) {
		return projscan.Result{Candidates: scanCands()}, nil
	}
	h.deliver(press("+"))
	h.deliver(tea.PasteMsg{Content: "/work/co\x1b[31mde\nx"})
	h.deliver(press("enter"))
	if h.m.welcome.scan == nil {
		t.Fatalf("no checklist:\n%s", h.view())
	}
	if raw := h.m.View().Content; strings.Contains(raw, "\x1b[31m") || !strings.Contains(plainView(h.m), "under /work/co de x:") && !strings.Contains(plainView(h.m), "/work/co") {
		t.Fatalf("%q", raw)
	}
}

// On a blocked row the bar keeps the dim mark of the row.
func TestChecklistCursorOnABlockedRowKeepsTheBarAndDim(t *testing.T) {
	cands := append(scanCands(), projscan.Candidate{Path: scanRoot + "/delta", Name: "delta", Slicer: true})
	h, _ := scanHarness(t, cands, registry.Project{Name: "delta", Path: scanRoot + "/delta"})
	h.deliver(press("+"))
	h.deliver(tea.PasteMsg{Content: scanRoot})
	h.deliver(press("enter"))
	for i := 0; i < 3; i++ {
		h.deliver(press("j"))
	}
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	bar := strings.TrimSuffix(NewTheme(true).TabActive.Render("x"), "x\x1b[m")
	for _, line := range strings.Split(h.m.View().Content, "\n") {
		if strings.Contains(line, "delta") && strings.Contains(line, "[-]") {
			if !strings.Contains(line, bar) || !strings.Contains(line, "\x1b[2m") {
				t.Fatalf("blocked cursor row: %q", line)
			}
			return
		}
	}
	t.Fatal("no delta row")
}

func TestGoldenChecklistStopped(t *testing.T) {
	for _, mono := range []bool{false, true} {
		theme := map[bool]string{false: "color", true: "mono"}[mono]
		h, _ := scanHarness(t, scanCands())
		h.m.cfg.Theme = NewTheme(mono)
		inner := h.m.cfg.Scan
		h.m.cfg.Scan = func(root string) (projscan.Result, error) {
			r, err := inner(root)
			r.Stopped, r.Folders = true, 500
			return r, err
		}
		welcomePath(h, scanRoot)
		h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		checkGolden(t, "checklist-stopped-80x24-"+theme+".txt", h.m.View().Content)
	}
}
