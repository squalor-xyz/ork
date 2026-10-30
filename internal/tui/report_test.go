// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/source"
)

type reportLive struct {
	*cardLive
	calls []string
	err   error
}

func (l *reportLive) Report(_ context.Context, run, name string) (string, error) {
	l.calls = append(l.calls, run+"#"+name)
	return "\x1b[31mReport body for " + name + "\x1b[0m\nTests: 12 passed, 0 failed\n## Owner needed\nNone.", l.err
}
func reportHarness(t *testing.T) (*harness, *reportLive) {
	t.Helper()
	h := rendersHarness(t)
	h.m.byName["ork"].snap.EngineRuns[0].Roles = []source.EngineRole{
		{Role: "implementer", Round: 1, Report: "implementer-1.md"},
		{Role: "reviewer", Round: 1, Report: "reviewer-1.md"},
		{Role: "gate", Round: 1, Report: "gate-1.md"},
		{Role: "implementer", Round: 2},
	}
	l := &reportLive{cardLive: h.live()}
	h.m.cfg.Live = l
	return h, l
}

func TestPaneReportKeyOpensTheNewestReport(t *testing.T) {
	h, l := reportHarness(t)
	if !strings.Contains(h.at(160, 40), "p report") {
		t.Fatal("missing report hint")
	}
	_, cmd := h.m.Update(press("p"))
	v := h.at(80, 24)
	if !strings.Contains(v, "Report 1 of 3: gate-1.md") || !strings.Contains(v, "Loading gate-1.md") || len(l.calls) != 0 {
		t.Fatalf("before fetch: calls %v\n%s", l.calls, v)
	}
	for _, msg := range exec(cmd) {
		h.deliver(msg)
	}
	if !strings.Contains(h.at(80, 24), "Report body for gate-1.md") || !reflect.DeepEqual(l.calls, []string{"engine#gate-1.md"}) {
		t.Fatalf("after fetch: calls %v\n%s", l.calls, h.view())
	}
	for _, ledger := range []bool{false, true} {
		h, _ := reportHarness(t)
		h.m.pane.ledger = ledger
		if !ledger {
			h.m.byName["ork"].snap.EngineRuns[0].Roles = nil
		}
		if strings.Contains(h.at(160, 40), "p report") {
			t.Fatalf("report hint without reports (ledger %t)", ledger)
		}
		h.deliver(press("p"))
		if h.m.pane.reportName != "" {
			t.Fatal("opened without reports")
		}
	}
}

func TestPaneReportKeyCyclesAndReturnsToTheRun(t *testing.T) {
	h, l := reportHarness(t)
	for i, name := range []string{"gate-1.md", "reviewer-1.md", "implementer-1.md"} {
		h.deliver(press("p"))
		if v := h.at(120, 40); !strings.Contains(v, fmt.Sprintf("Report %d of 3: %s", i+1, name)) || strings.Contains(v, "Human checks") {
			t.Fatalf("report %d:\n%s", i, v)
		}
	}
	for _, k := range []string{"a", "x", "F", "v", "1", "t", "o", "f", "q", "tab", "+", "-", "?"} {
		h.deliver(press(k))
	}
	if len(l.marks) != 0 || l.approves != 0 || h.m.pane.reportName != "implementer-1.md" || h.m.welcome.editing || h.m.removing.name != "" || h.m.confirm != nil || h.m.showHelp {
		t.Fatal("another key acted")
	}
	h.deliver(press("p"))
	if h.m.pane.reportName != "" || !strings.Contains(h.at(120, 40), "Human checks") {
		t.Fatal("did not return to run")
	}
	h.deliver(press("p"))
	h.deliver(press("esc"))
	if h.m.pane == nil || h.m.pane.reportName != "" {
		t.Fatal("esc did not return to run")
	}
}

func TestPaneReportKeyIsTextInAnAnswerField(t *testing.T) {
	for _, fixing := range []bool{false, true} {
		h, _ := reportHarness(t)
		if fixing {
			h.deliver(press("F"))
		} else {
			h.m.byName["ork"].snap.EngineRuns[0].State = "paused"
			h.m.byName["ork"].snap.EngineRuns[0].Pending = "question"
			h.m.byName["ork"].snap.EngineRuns[0].Wait = "Answer please"
		}
		h.deliver(press("p"))
		if h.m.pane.reportName != "" || h.m.pane.input.Value() != "p" {
			t.Fatalf("fixing %t: field %q report %q", fixing, h.m.pane.input.Value(), h.m.pane.reportName)
		}
	}
}

func TestPaneReportFailedReadShowsTheError(t *testing.T) {
	h, l := reportHarness(t)
	l.err = &ipc.Error{Code: daemon.CodeNotFound, Message: "no report gate-1.md"}
	h.deliver(press("p"))
	if v := h.at(120, 40); !strings.Contains(v, "Could not read gate-1.md: not_found: no report gate-1.md") {
		t.Fatal(v)
	}
	l.err = nil
	h.deliver(press("p"))
	if !strings.Contains(h.at(120, 40), "Report body for reviewer-1.md") {
		t.Fatal("cannot advance after failure")
	}
	h.deliver(press("esc"))
	if h.m.pane.reportName != "" {
		t.Fatal("cannot escape")
	}
}

func TestPaneIgnoresALateReportAnswer(t *testing.T) {
	h, _ := reportHarness(t)
	_, cmd := h.m.Update(press("p"))
	for _, msg := range []reportMsg{{run: "other", name: "gate-1.md", text: "WRONG"}, {run: "engine", name: "reviewer-1.md", text: "WRONG"}} {
		before := map[string]renderFetch{}
		for name, value := range h.m.pane.reports {
			before[name] = value
		}
		h.m.Update(msg)
		if !reflect.DeepEqual(before, h.m.pane.reports) {
			t.Fatalf("late answer changed cache: %+v", msg)
		}
	}
	if v := h.at(120, 40); strings.Contains(v, "WRONG") || !strings.Contains(v, "Loading gate-1.md") {
		t.Fatal(v)
	}
	for _, msg := range exec(cmd) {
		h.deliver(msg)
	}
	h.deliver(press("esc"))
	before := h.at(120, 40)
	h.m.Update(reportMsg{run: "engine", name: "gate-1.md", text: "WRONG"})
	if after := h.at(120, 40); after != before {
		t.Fatal("closed viewer changed")
	}
	h.deliver(press("esc"))
	h.m.Update(reportMsg{run: "engine", name: "gate-1.md", text: "WRONG"})
}

func TestDaemonReportWire(t *testing.T) {
	var seen []daemon.RunAction
	var methods []string
	d, _, _, _ := requestServer(t, func(_ context.Context, method string, params json.RawMessage) (any, error) {
		var a daemon.RunAction
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		methods = append(methods, method)
		seen = append(seen, a)
		if a.Name == "gone.md" {
			return nil, &ipc.Error{Code: daemon.CodeNotFound, Message: "no report"}
		}
		return daemon.ReportReply{Name: a.Name, Text: "report"}, nil
	})
	if text, err := d.Report(context.Background(), "engine", "gate-1.md"); err != nil || text != "report" {
		t.Fatalf("%q %v", text, err)
	}
	_, err := d.Report(context.Background(), "engine", "gone.md")
	var wire *ipc.Error
	if !errors.As(err, &wire) || actionError(err) != "not_found: no report" {
		t.Fatalf("%v", err)
	}
	if strings.Join(methods, " ") != "run.report run.report" || !reflect.DeepEqual(seen, []daemon.RunAction{{Run: "engine", Name: "gate-1.md"}, {Run: "engine", Name: "gone.md"}}) {
		t.Fatalf("%v %+v", methods, seen)
	}
}

func TestPaneReportGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprint(mono), func(t *testing.T) {
			h, _ := reportHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			h.deliver(press("p"))
			h.m.pane.reports[h.m.pane.reportName] = renderFetch{done: true, text: "Report body for gate-1.md\nThe gate report explains the remaining changes so the owner can review the result without opening files outside the run pane.\n\n| internal/tui/report.go | prose wraps; table rows keep their columns and are clipped at the pane edge |\nTests: 12 passed, 0 failed\n## Owner needed\nNone."}
			h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			checkGolden(t, fmt.Sprintf("report-80x24-mono-%t.txt", mono), h.m.View().Content)
		})
	}
}

func TestPaneReportSidebarToggle(t *testing.T) {
	h, l := reportHarness(t)
	h.deliver(press("p"))
	h.at(120, 40)
	h.m.pane.offset = 2
	beforeName := h.m.pane.reportName
	h.deliver(press("s"))
	if !h.m.sidebarHidden || h.m.pane.reportName != beforeName || h.m.pane.offset != 2 || len(l.calls) != 1 {
		t.Fatalf("sidebar %t, report %q, offset %d, calls %v", h.m.sidebarHidden, h.m.pane.reportName, h.m.pane.offset, l.calls)
	}
	if hint := hintLine(h.at(120, 40)); !strings.Contains(hint, "s sidebar") {
		t.Fatalf("sidebar hint missing: %s", hint)
	}
	h.deliver(press("s"))
	if h.m.sidebarHidden {
		t.Fatal("s did not restore the sidebar")
	}
}

func TestPaneReportLastHintNamesTheReturn(t *testing.T) {
	h, _ := reportHarness(t)
	for i := 1; i <= 3; i++ {
		h.deliver(press("p"))
		want := "p next report"
		if i == 3 {
			want = "p back to run"
		}
		if hint := hintLine(h.at(120, 40)); !strings.Contains(hint, want) || !strings.Contains(hint, "esc back to run") {
			t.Fatalf("report %d hint %q, want %q and back key", i, hint, want)
		}
	}
	h.deliver(press("p"))
	if h.m.pane.reportName != "" {
		t.Fatal("last p did not return to run")
	}
}

func TestPaneReportHintSurvivesAt80x24(t *testing.T) {
	for _, tc := range []struct{ name, state, pending string }{{"running", "implement", ""}, {"failed", "stopped", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := reportHarness(t)
			run := &h.m.byName["ork"].snap.EngineRuns[0]
			run.Card = nil
			run.State = tc.state
			run.Pending = tc.pending
			hint := hintLine(h.at(80, 24))
			previous := -1
			for _, want := range []string{"p report", "pgup/pgdown line", "esc close pane"} {
				at := strings.Index(hint, want)
				if at <= previous {
					t.Fatalf("hint %q lacks %q in order", hint, want)
				}
				previous = at
			}
			if strings.Contains(hint, "half page") {
				t.Fatalf("half-page hint outlasted report: %q", hint)
			}
			if tc.name == "failed" && hint != "p report; pgup/pgdown line; esc close pane" {
				t.Fatalf("failed hint %q", hint)
			}
		})
	}
}

func TestPaneReportHintOnApprovalAt100x30(t *testing.T) {
	h, _ := reportHarness(t)
	v := h.at(100, 30)
	hint := hintLine(v)
	for _, want := range []string{"p report", "1-3 tick/later", "F fix round", "esc close pane"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("approval hint %q lacks %q", hint, want)
		}
	}
	checkGolden(t, "report-approval-100x30.txt", h.m.View().Content)
}

func TestPaneReportWrapsProseAndClipsTables(t *testing.T) {
	h, _ := reportHarness(t)
	h.deliver(press("p"))
	h.m.pane.reports[h.m.pane.reportName] = renderFetch{done: true, text: "\x1b[31mOne two three four five six seven eight nine ten\x1b[0m\n\n| column one | column two | tail |\nDone."}
	want := []string{"One two three four", "five six seven eight", "nine ten", "", "| column one | co...", "Done."}
	if got := h.m.reportLines(20); !reflect.DeepEqual(got, want) {
		t.Fatalf("lines %#v, want %#v", got, want)
	}
}

func reportPerformanceText(prose bool) string {
	if prose {
		return strings.Repeat("normal prose keeps every word visible ", 2048)[:64<<10]
	}
	return strings.Repeat("x", 64<<10)
}

func TestPaneReportWraps64KiBPromptly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		prose bool
	}{{"no-spaces", false}, {"prose", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := reportHarness(t)
			h.deliver(press("p"))
			text := reportPerformanceText(tc.prose)
			h.m.pane.reports[h.m.pane.reportName] = renderFetch{done: true, text: text}
			start := time.Now()
			lines := h.m.reportLines(60)
			elapsed := time.Since(start)
			t.Logf("64 KiB %s at width 60: %s", tc.name, elapsed)
			if elapsed >= time.Second {
				t.Fatalf("wrapping took %s, want under 1s", elapsed)
			}
			for i, line := range lines {
				if width := lipgloss.Width(line); width > 60 {
					t.Fatalf("line %d: %d columns", i, width)
				}
			}
			if strings.ReplaceAll(strings.Join(lines, ""), " ", "") != strings.ReplaceAll(text, " ", "") {
				t.Fatal("wrapping lost report text")
			}
		})
	}
}

func TestPaneReportReusesLayoutAcross100Views(t *testing.T) {
	h, _ := reportHarness(t)
	h.deliver(press("p"))
	h.m.pane.reports[h.m.pane.reportName] = renderFetch{done: true, text: reportPerformanceText(true)}
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	before := h.m.View().Content // Warm the cache; do not count the first wrap.
	cached := h.m.pane.reportLayout
	if cached == nil {
		t.Fatal("View did not keep a report layout")
	}
	start := time.Now()
	for range 100 {
		if after := h.m.View().Content; after != before {
			t.Fatal("repeated View changed the report layout")
		}
		if h.m.pane.reportLayout != cached {
			t.Fatal("View rewrapped an unchanged report")
		}
	}
	elapsed := time.Since(start)
	t.Logf("100 cached Views: %s", elapsed)
	if elapsed >= 2*time.Second {
		t.Fatalf("100 Views took %s; layout was not reused", elapsed)
	}
}

func TestPaneReportLayoutFollowsContentNameAndWidth(t *testing.T) {
	h, _ := reportHarness(t)
	h.deliver(press("p"))
	text := "one two three four five six seven eight nine ten"
	h.m.pane.reports[h.m.pane.reportName] = renderFetch{done: true, text: text}
	first := h.m.reportLines(20)
	first[0] = "caller changed this line"
	if got := h.m.reportLines(20); got[0] != "one two three four" {
		t.Fatalf("caller mutated cached layout: %v", got)
	}
	if got := h.m.reportLines(10); len(got) <= len(first) {
		t.Fatalf("width change did not rewrap: %v", got)
	}
	h.m.reportLines(20)
	h.m.reportArrived(reportMsg{run: "engine", name: h.m.pane.reportName, text: "fresh report"})
	if got := h.m.reportLines(20); !reflect.DeepEqual(got, []string{"fresh report"}) {
		t.Fatalf("stale report content: %v", got)
	}
	h.deliver(press("p"))
	if got := h.m.reportLines(60); !strings.Contains(strings.Join(got, " "), "reviewer-1.md") {
		t.Fatalf("stale report name: %v", got)
	}
	h.m.reportLines(60)
	cached := h.m.pane.reportLayout
	h.deliver(press("s"))
	h.m.reportLines(60)
	if h.m.pane.reportLayout == cached {
		t.Fatal("sidebar change did not invalidate the layout")
	}
	h.m.pane.reports[h.m.pane.reportName] = renderFetch{done: true, text: text}
	wide := h.at(80, 24)
	h.deliver(press("s"))
	narrow := h.at(80, 24)
	if wide == narrow {
		t.Fatal("sidebar change did not update the view")
	}
}

func TestPaneReportWrapsLongUnicodeWords(t *testing.T) {
	h, _ := reportHarness(t)
	h.deliver(press("p"))
	text := strings.Repeat("界e\u0301", 100)
	h.m.pane.reports[h.m.pane.reportName] = renderFetch{done: true, text: text}
	lines := h.m.reportLines(60)
	for i, line := range lines {
		if width := lipgloss.Width(line); width > 60 {
			t.Fatalf("line %d: %d columns", i, width)
		}
	}
	if strings.Join(lines, "") != text {
		t.Fatal("wrapping lost Unicode text")
	}
}

func TestPaneReportBoundedWrappingMatchesShortProse(t *testing.T) {
	// Exhaust short words and runs of spaces, where the shared wrapper is cheap.
	var visit func(string, int)
	visit = func(text string, remaining int) {
		plain := strings.TrimRight(text, " ") // PlainText already trims report line ends.
		for width := 1; width <= 8; width++ {
			got := wrapText(breakReportLine(plain, width), width)
			want := wrapText(plain, width)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%q at width %d: got %#v, want %#v", text, width, got, want)
			}
		}
		if remaining > 0 {
			for _, r := range "ab " {
				visit(text+string(r), remaining-1)
			}
		}
	}
	visit("", 6)
	for _, text := range []string{"first " + strings.Repeat("x", 80) + " last", "界界 e\u0301e\u0301 and more words"} {
		for _, width := range []int{6, 10, 60} {
			if got, want := wrapText(breakReportLine(text, width), width), wrapText(text, width); !reflect.DeepEqual(got, want) {
				t.Fatalf("%q at %d: got %#v, want %#v", text, width, got, want)
			}
		}
	}
}
