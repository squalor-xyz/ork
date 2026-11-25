// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
)

// settingsLive answers projects.show from a canned result and project.setup
// from a canned command, and logs every call. Every other call is noteLive's.
type settingsLive struct {
	noteLive
	result  *daemon.ProjectsShowResult
	reads   []string
	setups  []daemon.ProjectSetupParams
	refusal error // the error of a run (not of a preview)
}

func (l *settingsLive) ProjectsShow(_ context.Context, project string) (*daemon.ProjectsShowResult, error) {
	l.reads = append(l.reads, project)
	return l.result, nil
}

func (l *settingsLive) ProjectSetup(_ context.Context, p daemon.ProjectSetupParams) (*daemon.ProjectSetupResult, error) {
	l.setups = append(l.setups, p)
	if !p.Preview && l.refusal != nil {
		return nil, l.refusal
	}
	return &daemon.ProjectSetupResult{Command: "adj init", Done: !p.Preview}, nil
}

func settingsFixture() *daemon.ProjectsShowResult {
	return &daemon.ProjectsShowResult{
		Project: registry.Project{
			Name: "ork", Path: "/projects/ork",
			VendorEnv: map[string]map[string]string{"codex": {"VISIBLE_KEY": "PRIVATE_VALUE"}},
		},
		Global: daemon.SettingsResult{Owner: "Jon", EffectiveGlobalCap: 4},
		Slicer: daemon.ShowSlicer{
			Config: json.RawMessage(`{"relay_contract":"v13","check_command":"go test ./...","claim_owner":"Jon","implement_finish":"handoff","hidden_config":"excluded"}`),
			Matrix: &daemon.ShowMatrix{Rows: matrixFixture().Rows[:1]},
		},
	}
}

// settingsHarness presses S on the Roadmap and delivers the read.
func settingsHarness(t *testing.T) (*harness, *settingsLive) {
	t.Helper()
	h := roadmapHarness(t)
	l := &settingsLive{result: settingsFixture()}
	h.m.cfg.Live = l
	_, cmd := h.m.Update(press("S"))
	if cmd == nil {
		t.Fatal("S returned no projects.show command")
	}
	if len(l.reads) != 0 {
		t.Fatal("the read ran inside Update")
	}
	h.m.Update(cmd())
	return h, l
}

// settingsChoose opens the chooser, picks its first step and delivers the preview.
func settingsChoose(t *testing.T, h *harness) {
	t.Helper()
	h.m.Update(press("i"))
	_, cmd := h.m.Update(press("enter"))
	if cmd == nil {
		t.Fatal("the chooser returned no preview command")
	}
	h.m.Update(cmd())
}

// settingsAll is the view at a size where every line of the body fits.
func settingsAll(h *harness) string {
	h.m.Update(tea.WindowSizeMsg{Width: 160, Height: 100})
	return plainView(h.m)
}

func TestSettingsKeyOpensThePaneForTheActiveProject(t *testing.T) {
	h, l := settingsHarness(t)
	if len(l.reads) != 1 || l.reads[0] != "ork" || !strings.Contains(h.view(), "Project settings") {
		t.Fatalf("reads=%v\n%s", l.reads, h.view())
	}
	for _, state := range []string{"project", "daemon", "interface"} {
		t.Run(state, func(t *testing.T) {
			h := roadmapHarness(t)
			h.m.cfg.Live = &roadmapLive{}
			switch state {
			case "project":
				h.m.active = -1
			case "daemon":
				h.m.cfg.Live = nil
			}
			_, cmd := h.m.Update(press("S"))
			if cmd != nil || h.m.actionStatus == "" || h.m.settings != nil {
				t.Fatalf("no refusal for %s: cmd %v, status %q", state, cmd != nil, h.m.actionStatus)
			}
		})
	}
}

func TestSettingsPaneGroupsByOwner(t *testing.T) {
	h, _ := settingsHarness(t)
	v := settingsAll(h)
	last := -1
	for _, want := range []string{"ork (projects.json)", "ork (global)", "slicer config", "slicer relay roles", "adj"} {
		at := strings.Index(v[last+1:], want)
		if at < 0 {
			t.Fatalf("missing heading %q in order:\n%s", want, v)
		}
		last += at + 1
	}
	for _, want := range []string{"VISIBLE_KEY", "relay_contract", "v13", "claim_owner", "Jon", "r9 implementer Codex", "used as codex", "config_file"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	for _, secret := range []string{"PRIVATE_VALUE", "hidden_config", "excluded"} {
		if strings.Contains(v, secret) {
			t.Errorf("unexpected %q:\n%s", secret, v)
		}
	}
}

func TestSettingsPaneShowsPartErrors(t *testing.T) {
	h, l := settingsHarness(t)
	h.m.Update(press("esc"))
	l.result.Slicer.Matrix = nil
	l.result.Slicer.Errors = []daemon.PartError{{Part: "matrix", Code: "no_relay_roles", Message: "no relay roles file"}}
	_, cmd := h.m.Update(press("S"))
	h.m.Update(cmd())
	v := settingsAll(h)
	for _, want := range []string{"no_relay_roles", "no relay roles file", "ork (global)", "claim_owner", "config_file"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
}

func TestSettingsSetupOffersOnlyStepsThatApply(t *testing.T) {
	h, l := settingsHarness(t)
	h.m.Update(press("esc"))
	l.result.Adj.Enabled = true
	_, cmd := h.m.Update(press("S"))
	h.m.Update(cmd())
	h.m.Update(press("i"))
	v := h.view()
	if !strings.Contains(v, "adj-off") || strings.Contains(v, "adj-init") || strings.Contains(v, "relay-roles-init") || strings.Contains(v, "slicer-init") {
		t.Fatalf("wrong chooser with adj on and a matrix:\n%s", v)
	}
	// With adj off, no matrix and no slicer project, the three init steps stand.
	h.m.Update(press("esc"))
	h.m.Update(press("esc"))
	l.result.Adj.Enabled = false
	l.result.Slicer.Config, l.result.Slicer.Matrix = nil, nil
	l.result.Slicer.Errors = []daemon.PartError{
		{Part: "config", Code: "state", Message: "no .slicer/ found in /projects/ork"},
		{Part: "matrix", Code: "no_relay_roles", Message: "no relay roles file"},
	}
	_, cmd = h.m.Update(press("S"))
	h.m.Update(cmd())
	h.m.Update(press("i"))
	v = h.view()
	if !strings.Contains(v, "slicer-init") || !strings.Contains(v, "relay-roles-init") || !strings.Contains(v, "adj-init") || strings.Contains(v, "adj-off") {
		t.Fatalf("wrong chooser with nothing set up:\n%s", v)
	}
}

func TestSettingsSetupConfirmsAndNeverRunsOnNoOrEnter(t *testing.T) {
	for _, cancel := range []string{"n", "enter", "esc"} {
		t.Run(cancel, func(t *testing.T) {
			h, l := settingsHarness(t)
			settingsChoose(t, h)
			v := h.view()
			if !strings.Contains(v, "Run: adj init") || !strings.Contains(v, "[y/N] (Enter is No)") {
				t.Fatalf("missing confirmation:\n%s", v)
			}
			if len(l.setups) != 1 || !l.setups[0].Preview || l.setups[0].Project != "ork" || l.setups[0].Step != "adj-init" {
				t.Fatalf("preview call: %v", l.setups)
			}
			_, cmd := h.m.Update(press(cancel))
			if cmd != nil {
				h.m.Update(cmd())
			}
			if len(l.setups) != 1 || strings.Contains(h.view(), "[y/N]") {
				t.Fatalf("%s ran or kept the confirmation: %v\n%s", cancel, l.setups, h.view())
			}
		})
	}
}

func TestSettingsSetupYRunsOnceAndReloads(t *testing.T) {
	h, l := settingsHarness(t)
	settingsChoose(t, h)
	_, run := h.m.Update(press("y"))
	if run == nil {
		t.Fatal("y returned no run command")
	}
	if _, again := h.m.Update(press("y")); again != nil {
		t.Fatal("a second y started a second run")
	}
	l.result.Project.Path = "/setup-complete"
	_, reload := h.m.Update(run())
	if reload == nil {
		t.Fatal("a successful run returned no reload")
	}
	h.m.Update(reload())
	if len(l.setups) != 2 || l.setups[1].Preview || len(l.reads) != 2 || !strings.Contains(h.view(), "/setup-complete") {
		t.Fatalf("setups=%v reads=%v\n%s", l.setups, l.reads, h.view())
	}
}

func TestSettingsSetupShowsTheRefusalVerbatim(t *testing.T) {
	h, l := settingsHarness(t)
	l.refusal = &ipc.Error{Code: "state", Message: "slicer refuses this exact operation"}
	settingsChoose(t, h)
	_, run := h.m.Update(press("y"))
	if run == nil {
		t.Fatal("y returned no run command")
	}
	_, reload := h.m.Update(run())
	v := h.view()
	if reload != nil || len(l.reads) != 1 || !strings.Contains(v, "state: slicer refuses this exact operation") {
		t.Fatalf("refusal missing or the pane reloaded (reads %v):\n%s", l.reads, v)
	}
}

func TestSettingsPaneIgnoresStaleReplies(t *testing.T) {
	h, l := settingsHarness(t)
	h.m.Update(press("esc"))
	_, old := h.m.Update(press("S"))
	stale := old()
	h.m.Update(press("esc"))
	l.result = &daemon.ProjectsShowResult{Project: registry.Project{Path: "/latest"}}
	_, next := h.m.Update(press("S"))
	h.m.Update(next())
	h.m.Update(stale)
	if v := h.view(); !strings.Contains(v, "/latest") || strings.Contains(v, "/projects/ork") {
		t.Fatalf("a stale read replaced the latest:\n%s", v)
	}
	// A run whose pane was closed and opened again answers into the new pane.
	settingsChoose(t, h)
	_, run := h.m.Update(press("y"))
	h.m.Update(press("esc"))
	_, next = h.m.Update(press("S"))
	h.m.Update(next())
	before := h.view()
	_, reload := h.m.Update(run())
	if reload != nil || before != h.view() {
		t.Fatalf("a stale setup reply changed the pane:\n%s\n--- after\n%s", before, h.view())
	}
}

func TestSettingsPaneMOpensTheMatrixPane(t *testing.T) {
	h, _ := settingsHarness(t)
	_, cmd := h.m.Update(press("m"))
	if cmd == nil {
		t.Fatal("m returned no matrix command")
	}
	h.m.Update(cmd())
	if v := h.view(); !strings.Contains(v, "Role matrix  (esc returns)") || strings.Contains(v, "Project settings") {
		t.Fatalf("the matrix pane did not open:\n%s", v)
	}
}

// m from another view first shows the Roadmap, so the matrix pane's own
// refusal (it opens only on the Roadmap) does not stop it.
func TestSettingsPaneMFromAnotherViewShowsTheRoadmapMatrix(t *testing.T) {
	h := roadmapHarness(t)
	h.m.view = 0
	l := &settingsLive{result: settingsFixture()}
	h.m.cfg.Live = l
	_, cmd := h.m.Update(press("S"))
	h.m.Update(cmd())
	if h.m.settings == nil || h.m.view != 0 {
		t.Fatalf("the pane did not open on the Overview: view %d", h.m.view)
	}
	_, cmd = h.m.Update(press("m"))
	if cmd == nil {
		t.Fatal("m returned no command")
	}
	for _, msg := range exec(cmd) {
		h.m.Update(msg)
	}
	if h.m.view != roadmapView || !h.m.matrixPaneOpen() || h.m.settings != nil {
		t.Fatalf("view %d, matrix pane open %v, settings open %v", h.m.view, h.m.matrixPaneOpen(), h.m.settings != nil)
	}
	if v := h.view(); !strings.Contains(v, "Role matrix  (esc returns)") {
		t.Fatalf("the matrix pane is not on screen:\n%s", v)
	}
}

// S settings stays on the hint line after M mouse goes (owner 2026-11-24,
// ui.settings-pane-hint): at 120 columns it is on the Overview and the Roadmap
// and M mouse is the one that drops.
func TestSettingsHintOutlastsMouse(t *testing.T) {
	for _, view := range []int{0, roadmapView} {
		h := roadmapHarness(t)
		h.m.cfg.Live = &settingsLive{result: settingsFixture()}
		h.m.view = view
		h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		lines := strings.Split(plainView(h.m), "\n")
		hint := lines[len(lines)-2]
		// The Overview line has room for both; the Roadmap's has room for one.
		if !strings.Contains(hint, "S settings") || (view == roadmapView && strings.Contains(hint, "M mouse")) {
			t.Errorf("view %d at 120 columns: want S settings, and on the Roadmap no M mouse:\n%s", view, hint)
		}
	}
	// At 200 columns both stand, and S settings drops at a lower number than M mouse.
	h := roadmapHarness(t)
	h.m.cfg.Live = &settingsLive{result: settingsFixture()}
	h.m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	lines := strings.Split(plainView(h.m), "\n")
	if hint := lines[len(lines)-2]; !strings.Contains(hint, "S settings") || !strings.Contains(hint, "M mouse") {
		t.Errorf("at 200 columns want both hints:\n%s", hint)
	}
	var settings, mouse int
	for _, e := range h.m.contextHints() {
		switch e.b.Help().Key {
		case "S":
			settings = e.drop
		case "M":
			mouse = e.drop
		}
	}
	if settings == 0 || mouse == 0 || settings >= mouse {
		t.Errorf("S settings drops at %d, M mouse at %d: want S to outlast M", settings, mouse)
	}
}

func TestSettingsKeyTypesTextInAnInput(t *testing.T) {
	h, l := settingsHarness(t)
	h.m.Update(press("esc"))
	h.m.Update(press("N"))
	h.m.Update(press("S"))
	if h.m.settings != nil || len(l.reads) != 1 || h.m.compose == nil || h.m.compose.field().Value() != "S" {
		t.Fatalf("S did not type into the note:\n%s", h.view())
	}
}

func TestSettingsPaneScrollsByLineAndPage(t *testing.T) {
	h, _ := settingsHarness(t)
	top := h.view()
	if strings.Contains(top, "lines above") || !strings.Contains(top, "more lines") {
		t.Fatalf("the pane does not start at its top:\n%s", top)
	}
	h.m.Update(press("j"))
	if v := h.view(); !strings.Contains(v, "+1 lines above") {
		t.Fatalf("j did not scroll a line:\n%s", v)
	}
	h.m.Update(press("k"))
	if v := h.view(); v != top {
		t.Fatalf("k did not scroll back:\n%s", v)
	}
	h.m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	paged := h.view()
	if strings.Contains(paged, "+1 lines above") || !strings.Contains(paged, "lines above") {
		t.Fatalf("pgdown did not scroll a page:\n%s", paged)
	}
	h.m.Update(press("G"))
	if v := h.view(); !strings.Contains(v, "config_file") || strings.Contains(v, "more lines") {
		t.Fatalf("G did not reach the end:\n%s", v)
	}
	h.m.Update(press("g"))
	if v := h.view(); v != top {
		t.Fatalf("g did not return to the top:\n%s", v)
	}
}

// settingsFullMatrix opens the pane for the fixture with every row of the
// matrix fixture, and the model and reasoning of some of them. A last model
// that is not empty is the model of the last row.
func settingsFullMatrix(t *testing.T, lastModel string) *harness {
	t.Helper()
	h := roadmapHarness(t)
	l := &settingsLive{result: settingsFixture()}
	rows := matrixFixture().Rows
	rows[0].Model, rows[0].Reasoning = "gpt-6.1-sol", "medium"
	rows[1].Model, rows[1].Reasoning = "sonnet", "high"
	rows[2].Model = "tiny"
	rows[len(rows)-1].Model = lastModel
	l.result.Slicer.Matrix = &daemon.ShowMatrix{Rows: rows}
	h.m.cfg.Live = l
	_, cmd := h.m.Update(press("S"))
	if cmd == nil {
		t.Fatal("S returned no projects.show command")
	}
	h.m.Update(cmd())
	return h
}

// lineIndex is the index of the first line of the view that has every word of
// words in order, as fields, or -1.
func lineIndex(view string, words ...string) int {
	for i, line := range strings.Split(view, "\n") {
		fields := strings.Fields(line)
		for at := 0; at+len(words) <= len(fields); at++ {
			if slices.Equal(fields[at:at+len(words)], words) {
				return i
			}
		}
	}
	return -1
}

// headingIndex is the index of the first line of the view that is exactly text,
// less its indent, or -1.
func headingIndex(view, text string) int {
	return slices.IndexFunc(strings.Split(view, "\n"), func(line string) bool {
		_, pane, _ := strings.Cut(line, "│")
		return strings.TrimSpace(pane) == text
	})
}

func TestSettingsPaneKeepsTheBlockHeadingWhenScrolled(t *testing.T) {
	h := settingsFullMatrix(t, "")
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.m.Update(press("G"))
	v := plainView(h.m)
	heading, row := headingIndex(v, "slicer config"), lineIndex(v, "check_command", "go", "test", "./...")
	if row < 0 || heading < 0 || heading > row {
		t.Fatalf("the slicer config row at line %d is not under its heading at line %d:\n%s", row, heading, v)
	}

	// At every offset, a row of slicer config, of the relay roles and of adj
	// has its block heading above it.
	h.m.Update(press("g"))
	for off := 0; off < 80; off++ {
		v := plainView(h.m)
		for _, c := range []struct {
			heading string
			row     []string
		}{
			{"slicer config", []string{"claim_owner", "Jon"}},
			{"slicer relay roles", []string{"r3", "implementer"}},
			{"adj", []string{"config_file", "absent"}},
		} {
			row := lineIndex(v, c.row...)
			if row < 0 {
				continue
			}
			if heading := headingIndex(v, c.heading); heading < 0 || heading > row {
				t.Fatalf("offset %d: the row %v has no heading %v above it:\n%s", off, c.row, c.heading, v)
			}
		}
		h.m.Update(press("j"))
	}
}

func TestSettingsPanePinsTheHeadingBelowTheMarker(t *testing.T) {
	h := settingsFullMatrix(t, "")
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.m.Update(press("G"))
	lines := strings.Split(plainView(h.m), "\n")
	marker := -1
	for i, line := range lines {
		if strings.Contains(line, "lines above") {
			marker = i
		}
	}
	if marker < 0 || marker+1 >= len(lines) {
		t.Fatalf("no marker of the lines above:\n%s", strings.Join(lines, "\n"))
	}
	_, next, _ := strings.Cut(lines[marker+1], "│")
	if next = strings.TrimSpace(next); next != "adj" && next != "slicer config" && next != "slicer relay roles" && next != "ork (global)" && next != "ork (projects.json)" {
		t.Fatalf("the line under the marker is not a block heading: %q", next)
	}
}

func TestSettingsPaneEndsFlushAtEveryHeight(t *testing.T) {
	for height := 8; height <= 60; height++ {
		h := settingsFullMatrix(t, "")
		h.m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
		h.m.Update(press("G"))
		v := plainView(h.m)
		if !strings.Contains(v, "config_file") || strings.Contains(v, "more lines") {
			t.Fatalf("height %d: G did not end flush:\n%s", height, v)
		}
		// With room for a pin, the first body line under the marker is a block
		// heading or the blank line between blocks, never a row without one.
		if h.m.settings.room >= 4 {
			lines := strings.Split(v, "\n")
			marker := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "lines above") })
			if marker < 0 && h.m.settings.offset > 0 {
				t.Fatalf("height %d: no marker of the lines above:\n%s", height, v)
			}
			if marker < 0 {
				continue // the whole body fits
			}
			if _, pane, _ := strings.Cut(lines[marker+1], "│"); strings.HasPrefix(pane, indent) {
				t.Fatalf("height %d: a row under the marker has no block heading:\n%s", height, v)
			}
		}
		// One more j keeps the end in view.
		h.m.Update(press("j"))
		if after := plainView(h.m); after != v {
			t.Fatalf("height %d: j moved past the end:\n%s", height, after)
		}
	}
}

func TestSettingsPaneAlignsRelayRoleRows(t *testing.T) {
	h := settingsFullMatrix(t, "")
	// Each row by its id and the start of its use text, which a narrow pane clips.
	rows := [][2]string{{"r9", "used as codex"}, {"r3", "used as claude"}, {"r21", "skipped:"}, {"r14", "UI pass row"}, {"r6", "not used:"}, {"r10", "not run by"}}
	for _, width := range []int{160, 100} {
		h.m.Update(tea.WindowSizeMsg{Width: width, Height: 100})
		lines := strings.Split(plainView(h.m), "\n")
		col := -1
		for _, row := range rows {
			at := slices.IndexFunc(lines, func(line string) bool {
				_, pane, _ := strings.Cut(line, "│")
				return strings.HasPrefix(strings.TrimSpace(pane), row[0]+" ") && strings.Contains(pane, row[1])
			})
			if at < 0 {
				t.Fatalf("%d columns: no line of %s with %q:\n%s", width, row[0], row[1], strings.Join(lines, "\n"))
			}
			_, pane, _ := strings.Cut(lines[at], "│")
			if got := strings.Index(pane, row[1]); col < 0 {
				col = got
			} else if got != col {
				t.Errorf("%d columns: the use text of %s starts at column %d, not %d:\n%s", width, row[0], got, col, lines[at])
			}
		}
	}
}

func TestSettingsPaneDropsTheUseTextWhenTheColumnsFillTheWidth(t *testing.T) {
	// The last row has a 17-column model, so the padded head is 53 columns.
	// The pane at 80 columns is 60 wide: the indent, the head and the gap
	// leave the use text 3 columns. Unpadded, the longest head is 46 columns.
	h := settingsFullMatrix(t, strings.Repeat("m", 17))
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 100})
	v := plainView(h.m)
	lines := strings.Split(v, "\n")
	from, to := headingIndex(v, "slicer relay roles"), headingIndex(v, "adj")
	if from < 0 || to < from {
		t.Fatalf("no relay roles block:\n%s", v)
	}
	for _, line := range lines[from+1 : to] {
		for _, use := range []string{"used a", "not run", "skipped", "UI pass", "not used", "..."} {
			if strings.Contains(line, use) {
				t.Fatalf("the use text %q is drawn without 4 columns:\n%s", use, v)
			}
		}
	}
	if !strings.Contains(v, "r10 orchestrator Claude Code") {
		t.Fatalf("the head of a row is missing:\n%s", v)
	}
}

func TestSettingsPaneGoldens(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		for _, mono := range []bool{false, true} {
			// The top of the body, its end (G, where slicer and adj stand), and
			// the confirmation of a setup step.
			for _, shape := range []string{"", "end-", "confirm-"} {
				name := fmt.Sprintf("settings-%s%dx%d-mono-%t.txt", shape, size[0], size[1], mono)
				t.Run(name, func(t *testing.T) {
					h, _ := settingsHarness(t)
					h.m.cfg.Theme = NewTheme(mono)
					h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					switch shape {
					case "end-":
						h.m.Update(press("G"))
					case "confirm-":
						settingsChoose(t, h)
					}
					checkGolden(t, name, h.m.View().Content)
				})
			}
		}
	}
}
