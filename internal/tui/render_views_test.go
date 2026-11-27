// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"flag"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// The TUI render flags mirror the CLI's (adj 644, adj 649, adj 651): a UI pass
// renders a TUI view with one plain command and no test file of its own.

var renders = flag.String("renders", "", "write plain-text TUI views to this directory")

// renderFixture picks the fixtures: a comma-separated list of names. With none,
// the five default fixtures are used.
var renderFixture = flag.String("render-fixture", "", "write or print only these comma-separated TUI fixtures")

// renderPrint prints each view's text to stdout, under a header line.
var renderPrint = flag.Bool("render-print", false, "print each TUI view's text to stdout")

// renderSize adds sizes beside the default ones.
var renderSize = flag.String("render-size", "", "also render each fixture at these comma-separated WxH sizes, as NAME-WxH.txt")

// renderView is one fixture: its name and a builder of the model it draws.
type renderView struct {
	name    string
	general bool // one of the fixtures -renders writes with no -render-fixture
	build   func(t *testing.T) *Model
}

// renderViews lists every fixture.
func renderViews() []renderView {
	return []renderView{
		{name: "overview", general: true, build: func(t *testing.T) *Model {
			return goldenModel(plainTheme(), 80, 24)
		}},
		{name: "roadmap", general: true, build: func(t *testing.T) *Model {
			return roadmapHarness(t).m
		}},
		{name: "launch-confirm", general: true, build: func(t *testing.T) *Model {
			h, _ := launchHarness(t)
			launchConfirmed(t, h)
			return h.m
		}},
		{name: "approval-card", general: true, build: func(t *testing.T) *Model {
			return cardHarness(t, approvalCard(openChecksThree()...)).m
		}},
		{name: "approval-render", general: true, build: func(t *testing.T) *Model {
			h := rendersHarness(t)
			h.deliver(press("v"))
			return h.m
		}},
	}
}

// renderDims is a view's width and height.
type renderDims struct{ w, h int }

// renderViewTexts draws the model at 80x24 and 120x40, at each extra size, and
// last in ASCII mode at 80x24. Keys are NAME-80x24, NAME-120x40, NAME-WxH and
// NAME-ascii.
func renderViewTexts(name string, m *Model, extra ...renderDims) map[string]string {
	files := map[string]string{}
	for _, size := range append([]renderDims{{80, 24}, {120, 40}}, extra...) {
		m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		files[fmt.Sprintf("%s-%dx%d", name, size.w, size.h)] = plainView(m)
	}
	m.cfg.Theme, m.cfg.ASCII = NewTheme(true), true
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	files[name+"-ascii"] = plainView(m)
	return files
}

// renderFileNames lists the keys renderViewTexts gives, in print order: the
// three default renders, then each extra size.
func renderFileNames(name string, extra []renderDims) []string {
	names := []string{name + "-80x24", name + "-120x40", name + "-ascii"}
	for _, size := range extra {
		names = append(names, fmt.Sprintf("%s-%dx%d", name, size.w, size.h))
	}
	return names
}

// parseRenderSizes reads -render-size: WxH[,WxH...].
func parseRenderSizes(spec string) ([]renderDims, error) {
	var sizes []renderDims
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		w, h, ok := strings.Cut(part, "x")
		width, werr := strconv.Atoi(w)
		height, herr := strconv.Atoi(h)
		if !ok || werr != nil || herr != nil || width < 1 || height < 1 {
			return nil, fmt.Errorf("render size %q: want WxH, for example 100x30", part)
		}
		sizes = append(sizes, renderDims{width, height})
	}
	return sizes, nil
}

// renderViewByName returns the named fixture, or fails with the fixture list.
func renderViewByName(t *testing.T, name string) renderView {
	t.Helper()
	var names []string
	for _, view := range renderViews() {
		if view.name == name {
			return view
		}
		names = append(names, view.name)
	}
	t.Fatalf("no render fixture %q; the fixtures are %s", name, strings.Join(names, ", "))
	return renderView{}
}

// renderSelected returns the fixtures one render run uses: the ones named by
// -render-fixture, or the default fixtures when it is empty.
func renderSelected(spec string) []string {
	if strings.TrimSpace(spec) == "" {
		var names []string
		for _, view := range renderViews() {
			if view.general {
				names = append(names, view.name)
			}
		}
		return names
	}
	var names []string
	for _, name := range strings.Split(spec, ",") {
		names = append(names, strings.TrimSpace(name))
	}
	return names
}

// TestRenderTUIViews renders the TUI fixtures. With no flag it does no work and
// writes and prints nothing.
func TestRenderTUIViews(t *testing.T) {
	if *renders == "" && !*renderPrint {
		return
	}
	sizes, err := parseRenderSizes(*renderSize)
	if err != nil {
		t.Fatal(err)
	}
	names := renderSelected(*renderFixture)
	views := make([]renderView, len(names))
	for i, name := range names {
		views[i] = renderViewByName(t, name) // an unknown name fails before any file is written
	}
	for _, view := range views {
		files := renderViewTexts(view.name, view.build(t), sizes...)
		for _, key := range renderFileNames(view.name, sizes) {
			if *renders != "" {
				if err := os.WriteFile(filepath.Join(*renders, key+".txt"), []byte(files[key]), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if *renderPrint {
				fmt.Fprintf(os.Stdout, "## %s\n%s\n", key, files[key])
			}
		}
	}
}

// renderRun runs this test binary on TestRenderTUIViews with the flags. A
// "-renders" flag takes dir as its value.
func renderRun(t *testing.T, dir string, flags ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"-test.run=^TestRenderTUIViews$"}
	for _, f := range flags {
		args = append(args, f)
		if f == "-renders" {
			args = append(args, dir)
		}
	}
	cmd := osexec.CommandContext(ctx, os.Args[0], args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// renderHead cuts a long output for a failure message.
func renderHead(out string) string {
	if len(out) > 400 {
		return out[:400] + "\n..."
	}
	return out
}

// renderFiles lists the names in dir, sorted.
func renderFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// renderFixtureFiles are the three default files of each named fixture.
func renderFixtureFiles(names ...string) []string {
	files := []string{}
	for _, name := range names {
		files = append(files, name+"-80x24.txt", name+"-120x40.txt", name+"-ascii.txt")
	}
	sort.Strings(files)
	return files
}

func allRenderFixtures() []string {
	var names []string
	for _, view := range renderViews() {
		names = append(names, view.name)
	}
	return names
}

func TestRenderTUIViewsWritesFiles(t *testing.T) {
	for _, name := range []string{"renders", "render-fixture", "render-print", "render-size"} {
		if flag.Lookup(name) == nil {
			t.Fatalf("missing -%s test flag", name)
		}
	}
	type renderCase struct {
		name    string
		flags   []string
		want    []string
		failure string // text the failing run must print
	}
	sized := append(renderFixtureFiles("overview"), "overview-100x30.txt", "overview-60x20.txt")
	sort.Strings(sized)
	cases := []renderCase{
		{name: "unset", want: []string{}},
		{name: "defaults", flags: []string{"-renders"}, want: renderFixtureFiles(renderSelected("")...)},
		{name: "two", flags: []string{"-renders", "-render-fixture", "roadmap,overview"}, want: renderFixtureFiles("roadmap", "overview")},
		{name: "unknown", flags: []string{"-renders", "-render-fixture", "nope"}, failure: `no render fixture "nope"`},
		{name: "fixture without renders", flags: []string{"-render-fixture", "roadmap"}, want: []string{}},
		{name: "sizes", flags: []string{"-renders", "-render-fixture", "overview", "-render-size", "100x30,60x20"}, want: sized},
		{name: "size without renders", flags: []string{"-render-size", "100x30"}, want: []string{}},
		{name: "bad size", flags: []string{"-renders", "-render-fixture", "overview", "-render-size", "wide"}, failure: `render size "wide"`},
	}
	for _, name := range allRenderFixtures() {
		cases = append(cases, renderCase{name: name, flags: []string{"-renders", "-render-fixture", name}, want: renderFixtureFiles(name)})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out, err := renderRun(t, dir, tc.flags...)
			if tc.failure != "" {
				if err == nil || !strings.Contains(out, tc.failure) {
					t.Fatalf("render command: %v, want it to fail with %q\n%s", err, tc.failure, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("render command: %v\n%s", err, out)
			}
			names := renderFiles(t, dir)
			if strings.Join(names, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("render files %q, want %q", names, tc.want)
			}
			for _, name := range names {
				text, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if strings.TrimSpace(string(text)) == "" {
					t.Errorf("%s is empty", name)
				}
				if strings.ContainsRune(string(text), '\x1b') {
					t.Errorf("%s contains ANSI escapes", name)
				}
				if name == "overview-60x20.txt" {
					if rows := len(strings.Split(strings.TrimRight(string(text), "\n"), "\n")); rows > 20 {
						t.Errorf("%s has %d rows, want at most 20", name, rows)
					}
				}
			}
		})
	}
}

func TestRenderTUIViewsFitTheirSize(t *testing.T) {
	for _, name := range allRenderFixtures() {
		t.Run(name, func(t *testing.T) {
			files := renderViewTexts(name, renderViewByName(t, name).build(t))
			for _, size := range []renderDims{{80, 24}, {120, 40}} {
				key := fmt.Sprintf("%s-%dx%d", name, size.w, size.h)
				text, ok := files[key]
				if !ok {
					t.Fatalf("no %s render", key)
				}
				rows := strings.Split(strings.TrimRight(text, "\n"), "\n")
				if len(rows) > size.h {
					t.Errorf("%s has %d rows, want at most %d", key, len(rows), size.h)
				}
				for i, row := range rows {
					if w := utf8.RuneCountInString(row); w > size.w {
						t.Errorf("%s row %d is %d runes wide, want at most %d: %q", key, i+1, w, size.w, row)
					}
				}
			}
			ascii, ok := files[name+"-ascii"]
			if !ok {
				t.Fatalf("no %s-ascii render", name)
			}
			if strings.ContainsAny(ascii, "│─") {
				t.Errorf("%s-ascii has box-drawing runes:\n%s", name, ascii)
			}
		})
	}
}

func TestRenderTUIViewsPrintsText(t *testing.T) {
	const name = "overview"
	want := renderViewTexts(name, renderViewByName(t, name).build(t), renderDims{100, 30})
	t.Run("print", func(t *testing.T) {
		dir := t.TempDir()
		out, err := renderRun(t, dir, "-test.v", "-render-print", "-render-fixture", name, "-render-size", "100x30")
		if err != nil {
			t.Fatalf("render command: %v\n%s", err, out)
		}
		for _, key := range renderFileNames(name, []renderDims{{100, 30}}) {
			text, ok := want[key]
			if !ok {
				t.Fatalf("no %s render", key)
			}
			if !strings.Contains(out, "## "+key+"\n"+text+"\n") {
				t.Errorf("output lacks the header and text of %s:\n%s", key, renderHead(out))
			}
		}
		if files := renderFiles(t, dir); len(files) != 0 {
			t.Errorf("-render-print without -renders wrote %q", files)
		}
	})
	t.Run("print and write", func(t *testing.T) {
		dir := t.TempDir()
		out, err := renderRun(t, dir, "-test.v", "-render-print", "-renders", "-render-fixture", name)
		if err != nil {
			t.Fatalf("render command: %v\n%s", err, out)
		}
		if !strings.Contains(out, "## overview-80x24\n") {
			t.Errorf("no printed text:\n%s", renderHead(out))
		}
		if got := renderFiles(t, dir); strings.Join(got, " ") != strings.Join(renderFixtureFiles(name), " ") {
			t.Errorf("files %q", got)
		}
	})
	t.Run("write without print", func(t *testing.T) {
		out, err := renderRun(t, t.TempDir(), "-test.v", "-renders", "-render-fixture", name)
		if err != nil {
			t.Fatalf("render command: %v\n%s", err, out)
		}
		if strings.Contains(out, "## ") {
			t.Errorf("printed with no -render-print:\n%s", renderHead(out))
		}
	})
	t.Run("neither flag", func(t *testing.T) {
		out, err := renderRun(t, t.TempDir(), "-test.v", "-render-fixture", name)
		if err != nil {
			t.Fatalf("render command: %v\n%s", err, out)
		}
		if strings.Contains(out, "## ") {
			t.Errorf("printed with no -render-print:\n%s", renderHead(out))
		}
	})
}

func TestRenderTUIFixtureDocsListEveryFixture(t *testing.T) {
	const marker = "go test ./internal/tui -run TestRenderTUIViews -renders DIR"
	for _, doc := range []string{"guide.md", "design.md"} {
		t.Run(doc, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
			if err != nil {
				t.Fatal(err)
			}
			_, rest, ok := strings.Cut(string(data), marker)
			if !ok {
				t.Fatalf("docs/%s lacks the TUI render command %q", doc, marker)
			}
			list, _, ok := strings.Cut(rest, "No file is written without `-renders`.")
			if !ok {
				t.Fatalf("docs/%s TUI render list lacks its ending", doc)
			}
			for _, name := range allRenderFixtures() {
				t.Run(name, func(t *testing.T) {
					if !strings.Contains(list, "`"+name+"`") {
						t.Errorf("docs/%s TUI render list lacks exact name %q", doc, name)
					}
				})
			}
			for _, flagName := range []string{"`-render-fixture NAME[,NAME...]`", "`-render-print`", "`-render-size WxH[,WxH...]`"} {
				if !strings.Contains(list, flagName) {
					t.Errorf("docs/%s TUI render list lacks %s", doc, flagName)
				}
			}
		})
	}
}
