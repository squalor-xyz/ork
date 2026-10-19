// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/poll"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// Run `go test ./internal/tui -update` to rewrite testdata/golden/.
var update = flag.Bool("update", false, "rewrite the golden files")

// TestStylesEmit24BitColour checks that a style writes a hex colour as 24-bit
// escape codes. lipgloss v2 has no renderer and no colour profile: a style
// writes the colours it was given; the output writer downsamples them for
// the terminal. So the colour goldens do not depend on the terminal or the
// environment the tests run in.
func TestStylesEmit24BitColour(t *testing.T) {
	got := lipgloss.NewStyle().Foreground(lipgloss.Color("#123456")).Render("x")
	if !strings.Contains(got, "38;2;18;52;86") {
		t.Fatalf("the style does not write 24-bit colour: %q", got)
	}
}

// newGoldenModel is the Model the golden views start from: four projects, a
// fixed clock, and every project in flight.
func newGoldenModel(theme Theme) *Model {
	now := time.Date(2026, 10, 19, 16, 42, 7, 0, time.UTC)
	m := New(Config{
		Source:    &fakeSource{snaps: map[string]source.Snapshot{}},
		Projects:  four,
		Now:       func() time.Time { return now },
		Schedule:  poll.DefaultSchedule,
		TickEvery: time.Second,
		After:     func(time.Duration) tea.Cmd { return nil },
		Bell:      func() {},
		Theme:     theme,
	})
	m.Init()
	return m
}

// goldenModel is the Overview with every kind of row: decisions, a held item,
// a failed call, a pending project, and a relay started elsewhere.
func goldenModel(theme Theme, w, h int) *Model {
	m := newGoldenModel(theme) // every project is now in flight; the three below return
	slicer := snap(four[0],
		[]adjcli.Record{
			openRec(11, "ui.slicer-tui", "Does ork reimplement slicer's roadmap editor or reuse slicer tui?"),
			openRec(51, "vendor.codex-ratelimit", "Does codex count as rate-limit-capable in v1?"),
		},
		[]slicercli.Item{item("S12", "Roadmap and Decisions screens, read-only", "needs-owner")},
		relay("run-20261019-004308-40d4", "2026-10-19T15:42:00Z", "S11"))
	adj := snap(four[1], []adjcli.Record{openRec(3, "output.shape", "Which JSON shape does inbox print?")}, nil)
	databall := snap(four[2], nil, nil)
	databall.Items = source.Result[[]slicercli.Item]{Err: &clirun.ToolError{Code: "no_such_item", Message: "No such item: S99."}}
	for _, s := range []source.Snapshot{slicer, adj, databall} {
		m.Update(snapshotMsg{name: s.Project.Name, snap: s})
	}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// setupModel is the Overview with a project that has no .slicer/ folder and a
// project whose two slicer calls failed with one code. Its rows are short, so
// the golden does not depend on how long a row is cut.
func setupModel(theme Theme, w, h int) *Model {
	m := newGoldenModel(theme)
	slicer := snap(four[0],
		[]adjcli.Record{openRec(11, "ui.slicer-tui", "Reuse slicer tui?")},
		[]slicercli.Item{item("S12", "Roadmap screen", "needs-owner")})
	agents := snap(four[3], nil, nil)
	agents.Items = source.Result[[]slicercli.Item]{Err: &clirun.ToolError{Code: "locked", Message: "Ledger is busy."}}
	agents.Runs = source.Result[[]slicercli.RelayRun]{Err: &clirun.ToolError{Code: "locked", Message: "Ledger is busy."}}
	for _, s := range []source.Snapshot{slicer, snap(four[1], nil, nil), noSlicer(four[2]), agents} {
		m.Update(snapshotMsg{name: s.Project.Name, snap: s})
	}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// TestGoldenSetupAndErrorRows covers both cases: a project with no .slicer/
// (no rows, dimmed, and its hint when selected) and merged errors.
func TestGoldenSetupAndErrorRows(t *testing.T) {
	for _, c := range []struct {
		name string
		w, h int
		mono bool
		pick string // the key that selects the project, or ""
	}{
		{"setup-100x30-mono.txt", 100, 30, true, ""},
		{"setup-80x24-color.txt", 80, 24, false, "3"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := setupModel(NewTheme(c.mono), c.w, c.h)
			if c.pick != "" {
				m.Update(press(c.pick))
			}
			checkGolden(t, c.name, m.View().Content)
		})
	}
}

// TestGoldenCutRowsAndSelectedDetail covers long rows cut to one line, and the
// detail line of the selected row: a decision wrapped over two lines, and a run
// whose ID stays whole. The down count moves the row cursor from the first row.
func TestGoldenCutRowsAndSelectedDetail(t *testing.T) {
	for _, c := range []struct {
		name string
		w, h int
		mono bool
		down int
	}{
		{"cut-100x30-mono.txt", 100, 30, true, 1},
		{"cut-80x24-color.txt", 80, 24, false, 6},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := goldenModel(NewTheme(c.mono), c.w, c.h)
			for i := 0; i < c.down; i++ {
				m.Update(press("n"))
			}
			checkGolden(t, c.name, m.View().Content)
		})
	}
}

// checkGolden compares got with testdata/golden/name, or rewrites the file
// under -update.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "golden", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/tui -update)", err)
	}
	if got != string(want) {
		t.Errorf("view differs from %s (run go test ./internal/tui -update and review the diff)\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

func TestGoldenViews(t *testing.T) {
	sizes := []struct{ w, h int }{{100, 30}, {80, 24}, {120, 40}}
	themes := []struct {
		name string
		mono bool
	}{{"color", false}, {"mono", true}}
	for _, sz := range sizes {
		for _, th := range themes {
			name := fmt.Sprintf("overview-%dx%d-%s.txt", sz.w, sz.h, th.name)
			t.Run(name, func(t *testing.T) {
				got := goldenModel(NewTheme(th.mono), sz.w, sz.h).View().Content
				path := filepath.Join("..", "..", "testdata", "golden", name)
				if *update {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("%v (run go test ./internal/tui -update)", err)
				}
				if got != string(want) {
					t.Errorf("view differs from %s (run go test ./internal/tui -update and review the diff)\n--- got\n%s\n--- want\n%s", path, got, want)
				}
			})
		}
	}
}

var sgr = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// colourCodes returns the SGR parameter lists in s that set a colour.
func colourCodes(s string) []string {
	var out []string
	for _, m := range sgr.FindAllStringSubmatch(s, -1) {
		for _, p := range strings.Split(m[1], ";") {
			switch {
			case p == "38" || p == "48" || p == "58":
				out = append(out, m[1])
			case len(p) == 2 && (p[0] == '3' || p[0] == '4' || p[0] == '9') && p[1] >= '0' && p[1] <= '9' && p != "39" && p != "49":
				out = append(out, m[1])
			case strings.HasPrefix(p, "10") && len(p) == 3:
				out = append(out, m[1])
			}
		}
	}
	return out
}

func TestColourThemeUsesColourAndMonoThemeDoesNot(t *testing.T) {
	colour := goldenModel(NewTheme(false), 100, 30).View().Content
	mono := goldenModel(NewTheme(true), 100, 30).View().Content
	if len(colourCodes(colour)) == 0 {
		t.Error("the colour theme set no colour")
	}
	if got := colourCodes(mono); len(got) != 0 {
		t.Errorf("the monochrome theme set colours: %q", got)
	}
	if strings.TrimSpace(sgr.ReplaceAllString(colour, "")) != strings.TrimSpace(sgr.ReplaceAllString(mono, "")) {
		t.Error("colour and monochrome differ in their words and symbols")
	}
}

func TestNoColorSelectsTheMonochromeTheme(t *testing.T) {
	withNoColor := goldenModel(ThemeFor(func(k string) string {
		if k == "NO_COLOR" {
			return "1"
		}
		return ""
	}), 100, 30).View().Content
	without := goldenModel(ThemeFor(func(string) string { return "" }), 100, 30).View().Content
	if got := colourCodes(withNoColor); len(got) != 0 {
		t.Errorf("NO_COLOR=1 still set colours: %q", got)
	}
	if len(colourCodes(without)) == 0 {
		t.Error("no NO_COLOR but no colour either")
	}
	if withNoColor != goldenModel(NewTheme(true), 100, 30).View().Content {
		t.Error("NO_COLOR view is not the monochrome view")
	}
}

func TestEveryColouredStateHasAWord(t *testing.T) {
	v := sgr.ReplaceAllString(goldenModel(NewTheme(false), 100, 30).View().Content, "")
	for _, word := range []string{"decision", "held", "error", "pending", "started elsewhere"} {
		if !strings.Contains(v, word) {
			t.Errorf("no word %q for its state:\n%s", word, v)
		}
	}
}

func TestGoldenFifteenProjects(t *testing.T) {
	var projects []registry.Project
	for i := 1; i <= 15; i++ {
		projects = append(projects, proj(fmt.Sprintf("project-%02d", i), false))
	}
	h := newHarness(t, projects...)
	h.start()
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := h.m.View().Content
	path := filepath.Join("..", "..", "testdata", "golden", "overview-15-projects.txt")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatal("15-project golden differs")
	}
}
