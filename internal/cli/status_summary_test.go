// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

func summaryFixture() daemon.ProjectStatus {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	return daemon.ProjectStatus{Name: "p", Path: "/projects/p", Open: []daemon.OpenRecord{{ID: 12, Key: "design.x", Title: "Choose X"}}, Held: []daemon.HeldItem{{ID: "S9", Title: "Edit launcher"}}, SpecQuestions: []daemon.SpecQuestions{{Item: "S2", Keys: []string{"q1"}, List: "ork wave start p S2 --questions"}}, EngineRuns: []source.EngineRun{
		{ID: "r1", Item: "S1", State: "implementing", Working: []source.EngineWork{{What: "implementer round 2", Since: now.Add(-5 * time.Minute)}}, Roles: []source.EngineRole{{Role: "implementer", Vendor: "codex", Round: 2, StartedAt: now.Add(-5 * time.Minute)}}},
		{ID: "r2", Item: "S3", State: "awaiting-approval", Card: &runrec.ApprovalCard{HumanChecks: []runrec.HumanCheck{{Text: "Inspect screen", State: runrec.CheckLater}}}},
		{ID: "r3", Item: "S4", State: "paused"},
	}}
}

func summaryApp(t *testing.T, p daemon.ProjectStatus, status, deps string) *App {
	t.Helper()
	fake := clitest.Install(t,
		clitest.Route{Tool: "slicer", Args: []string{"status", "--json", "--lean", "--root", p.Path}, Stdout: status},
		clitest.Route{Tool: "slicer", Args: []string{"deps", "--json", "--lean", "--root", p.Path}, Stdout: deps})
	return &App{Tools: Tools{Slicer: fake.Bin("slicer")}, Getwd: func() (string, error) { return p.Path + "/subdir", nil }, Now: func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) }, Call: func(_ context.Context, method string, params, result any) error {
		if method != daemon.MethodStatus {
			return fmt.Errorf("unexpected method %s", method)
		}
		b, _ := json.Marshal(daemon.StatusResult{BuildID: "dev", Projects: []daemon.ProjectStatus{p, {Name: "other", Path: "/other"}}})
		return json.Unmarshal(b, result)
	}}
}

func TestStatusSummaryPopulated(t *testing.T) {
	app := summaryApp(t, summaryFixture(), `{"next":{"id":"S7","title":"First"},"blocked":[{"id":"S8","waiting_on":["S6","S5"]}]}`, `[{"id":"S2","title":"Second"},{"id":"S7","title":"First"}]`)
	code, out, errs := runCLI(app, "status", "--summary")
	want := "p\nRunning\n  r1 S1 implementing; codex; working: implementer round 2, 5m\nNeeds you\n  adj: 1 open question (adj inbox --root /projects/p)\n  S2: 1 open spec question (ork wave start p S2 --questions)\n  r2 S3 awaiting-approval (ork approve r2)\n  r2 S3: human check 1 later: Inspect screen (ork check r2 1)\n  r3 S4 paused (ork show r3)\nBlocked\n  S8 waits on S6, S5\n  S9 Edit launcher (needs-owner)\nNext\n  S7 First\n  S2 Second (waits on 1 spec question)\n"
	if code != ExitOK || out != want || errs != "" {
		t.Fatalf("summary exit %d stderr %q\ngot:\n%swant:\n%s", code, errs, out, want)
	}
}

func TestStatusSummaryEmpty(t *testing.T) {
	app := summaryApp(t, daemon.ProjectStatus{Name: "p", Path: "/projects/p"}, `{"next":null,"blocked":[]}`, `[]`)
	app.Getwd = func() (string, error) { return "/other", nil }
	code, out, errs := runCLI(app, "status", "--summary", "--project", "p")
	want := "p\nRunning\n  none\nNeeds you\n  none\nBlocked\n  none\nNext\n  none\n"
	if code != 0 || out != want || errs != "" {
		t.Fatalf("empty summary: %d %q %q", code, out, errs)
	}
}

func TestStatusSummaryDeferredCheckAfterApproval(t *testing.T) {
	p := daemon.ProjectStatus{Name: "p", Path: "/projects/p", EngineRuns: []source.EngineRun{{ID: "r1", Item: "S1", State: runrec.StateCommitted, HumanChecks: []runrec.HumanCheck{{Text: "Done", State: runrec.CheckTicked}, {Text: "Inspect screen", State: runrec.CheckLater}}}}}
	app := summaryApp(t, p, `{}`, `[]`)
	code, out, errs := runCLI(app, "status", "--summary")
	want := "p\nRunning\n  none\nNeeds you\n  r1 S1: human check 2 later: Inspect screen (ork show r1)\nBlocked\n  none\nNext\n  none\n"
	if code != 0 || out != want || errs != "" {
		t.Fatalf("deferred after approval: %d %q %q", code, out, errs)
	}
}

func TestStatusSummaryMultilineRows(t *testing.T) {
	p := daemon.ProjectStatus{Name: "p", Path: "/projects/p", Held: []daemon.HeldItem{{ID: "S9", Title: "Edit\nlauncher"}}}
	app := summaryApp(t, p, `{}`, `[]`)
	code, out, errs := runCLI(app, "status", "--summary")
	if code != 0 || !strings.Contains(out, "  S9 Edit launcher (needs-owner)\n") {
		t.Fatalf("multiline row: %d %q %q", code, out, errs)
	}
}

func TestStatusSummaryCapsEverySectionAndJSON(t *testing.T) {
	p := daemon.ProjectStatus{Name: "p", Path: "/projects/p"}
	blocked, deps := []string{}, []string{}
	for i := 1; i <= 7; i++ {
		p.EngineRuns = append(p.EngineRuns, source.EngineRun{ID: fmt.Sprintf("r%d", i), Item: fmt.Sprintf("S%d", i), State: "implementing"})
		p.SpecQuestions = append(p.SpecQuestions, daemon.SpecQuestions{Item: fmt.Sprintf("S%d", i), Keys: []string{"q"}, List: "answer"})
		blocked = append(blocked, fmt.Sprintf(`{"id":"B%d","waiting_on":["X"]}`, i))
		deps = append(deps, fmt.Sprintf(`{"id":"N%d","title":%q}`, i, strings.TrimSpace(strings.Repeat("Next item description ", 6))))
	}
	app := summaryApp(t, p, `{"blocked":[`+strings.Join(blocked, ",")+`]}`, `[`+strings.Join(deps, ",")+`]`)
	code, out, errs := runCLI(app, "status", "--summary")
	if code != 0 || strings.Count(out, "  +2 more\n") != 4 || strings.Contains(out, "S6") || strings.Contains(out, "B6") || strings.Contains(out, "N6") {
		t.Fatalf("cap: %d %s %s", code, out, errs)
	}
	code, raw, errs := runCLI(app, "status", "--summary", "--json")
	// Check keys independently of Go field-name matching.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || code != 0 {
		t.Fatalf("JSON: %d %s %s", code, raw, errs)
	}
	if len(obj) != 5 || string(obj["project"]) != `"p"` {
		t.Fatalf("JSON keys: %s", raw)
	}
	for _, key := range []string{"running", "needs_you", "blocked", "next"} {
		var section struct {
			Rows []string `json:"rows"`
			More int      `json:"more"`
		}
		if err := json.Unmarshal(obj[key], &section); err != nil || len(section.Rows) != 5 || section.More != 2 {
			t.Errorf("section %s: %s", key, raw)
		}
		for _, row := range section.Rows {
			if !strings.Contains(unfoldSummaryText(out), "  "+row+"\n") {
				t.Errorf("JSON row absent from text: %q", row)
			}
		}
	}
}

func unfoldSummaryText(s string) string {
	return strings.ReplaceAll(s, "\n    ", " ")
}

func TestStatusSummaryHeldDepsExcluded(t *testing.T) {
	p := daemon.ProjectStatus{Name: "p", Path: "/projects/p", Held: []daemon.HeldItem{{ID: "S9", Title: "Edit launcher"}}}
	want := "p\nRunning\n  none\nNeeds you\n  none\nBlocked\n  S9 Edit launcher (needs-owner)\nNext\n  S7 First\n"
	for _, deps := range []string{`[{"id":"S7","title":"First"}]`, `[{"id":"S9","title":"Edit launcher"},{"id":"S7","title":"First"}]`} {
		app := summaryApp(t, p, `{"next":{"id":"S7","title":"First"}}`, deps)
		code, out, errs := runCLI(app, "status", "--summary")
		if code != 0 || out != want || errs != "" {
			t.Errorf("held deps: exit %d, output %q, stderr %q", code, out, errs)
		}
	}
}

func TestStatusSummaryNeedsRunCommands(t *testing.T) {
	for _, tc := range []struct{ name, state, pending string }{
		{"paused", "paused", ""}, {"check-error", "paused", "error"}, {"shape", "paused", "shape"}, {"stopped", "stopped", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := daemon.ProjectStatus{Name: "p", Path: "/projects/p", EngineRuns: []source.EngineRun{{ID: "r1", Item: "S1", State: tc.state, Pending: tc.pending}}}
			code, out, errs := runCLI(summaryApp(t, p, `{}`, `[]`), "status", "--summary")
			want := "  r1 S1 " + tc.state + " (ork show r1)\n"
			if code != 0 || !strings.Contains(out, want) || errs != "" {
				t.Fatalf("needs command: exit %d, output %q, stderr %q", code, out, errs)
			}
		})
	}
}

func TestStatusSummaryIgnoresUnusedSourceErrors(t *testing.T) {
	for _, part := range []string{"goals", "runs", "decided"} {
		t.Run(part, func(t *testing.T) {
			p := daemon.ProjectStatus{Name: "p", Path: "/projects/p", Errors: []daemon.PartError{{Part: part, Code: "source", Message: "failed " + part}}}
			code, out, errs := runCLI(summaryApp(t, p, `{}`, `[]`), "status", "--summary")
			want := "p\nRunning\n  none\nNeeds you\n  none\nBlocked\n  none\nNext\n  none\n"
			if code != 0 || out != want || errs != "" {
				t.Fatalf("unused source: exit %d, output %q, stderr %q", code, out, errs)
			}
		})
	}
}

func TestStatusSummaryRequiredSourceErrors(t *testing.T) {
	for _, part := range []string{"items", "open"} {
		t.Run(part, func(t *testing.T) {
			p := daemon.ProjectStatus{Name: "p", Path: "/projects/p", Errors: []daemon.PartError{{Part: "goals", Code: "source", Message: "failed goals"}, {Part: part, Code: "source", Message: "failed " + part}}}
			code, out, errs := runCLI(summaryApp(t, p, `{}`, `[]`), "status", "--summary")
			if code != ExitFailure || out != "" || errs != "ork: failed "+part+"\n" {
				t.Fatalf("required source: exit %d, output %q, stderr %q", code, out, errs)
			}
		})
	}
}

func TestStatusSummaryQuestionNouns(t *testing.T) {
	for _, tc := range []struct {
		kind string
		n    int
		want string
	}{
		{"adj", 1, "adj: 1 open question ("},
		{"adj", 3, "adj: 3 open questions ("},
		{"spec", 1, "S2: 1 open spec question ("},
		{"spec", 3, "S2: 3 open spec questions ("},
		{"next", 1, "S2 Second (waits on 1 spec question)"},
		{"next", 3, "S2 Second (waits on 3 spec questions)"},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.kind, tc.n), func(t *testing.T) {
			p := daemon.ProjectStatus{Name: "p", Path: "/projects/p"}
			if tc.kind == "adj" {
				p.Open = make([]daemon.OpenRecord, tc.n)
			} else {
				p.SpecQuestions = []daemon.SpecQuestions{{Item: "S2", Keys: make([]string, tc.n), List: "answer"}}
			}
			code, out, errs := runCLI(summaryApp(t, p, `{"next":{"id":"S2","title":"Second"}}`, `[]`), "status", "--summary")
			if code != 0 || errs != "" {
				t.Fatalf("question counts: exit %d, stderr %q", code, errs)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("missing question wording %q in %q", tc.want, out)
			}
		})
	}
}

func summaryLongFixture() daemon.ProjectStatus {
	p := summaryFixture()
	p.EngineRuns[0].Working[0].What = "implementer round 2 " + strings.TrimSpace(strings.Repeat("processing the current source files ", 4))
	p.EngineRuns[1].Card.HumanChecks[0].Text = strings.TrimSpace(strings.Repeat("Inspect the screen and review the interaction carefully. ", 3))
	p.SpecQuestions[0].List = "ork wave start p S2 --questions " + strings.TrimSpace(strings.Repeat("inspect question ", 5))
	p.Held[0].Title = strings.TrimSpace(strings.Repeat("Edit the local launcher configuration ", 4))
	return p
}

func TestStatusSummaryWrapsLongRows(t *testing.T) {
	app := summaryApp(t, summaryLongFixture(), `{"next":{"id":"S7","title":"Review the complete summary display and its detailed information before starting the next item in the current project"}}`, `[]`)
	code, out, errs := runCLI(app, "status", "--summary")
	if code != 0 || errs != "" {
		t.Fatalf("long summary: exit %d, stderr %q", code, errs)
	}
	for _, line := range strings.Split(out, "\n") {
		if utf8.RuneCountInString(line) > 80 {
			t.Errorf("summary line exceeds 80 columns: %q", line)
		}
		if strings.HasPrefix(line, " ") {
			indent := len(line) - len(strings.TrimLeft(line, " "))
			if indent != 2 && indent != 4 {
				t.Errorf("summary indent must be two or four spaces: %q", line)
			}
		}
	}
	if strings.Count(out, "\n    ") < 4 {
		t.Errorf("missing four-space continuation indents: %q", out)
	}
	code, raw, errs := runCLI(app, "status", "--summary", "--json")
	var data struct {
		Running, Blocked, Next struct {
			Rows []string `json:"rows"`
		}
		NeedsYou struct {
			Rows []string `json:"rows"`
		} `json:"needs_you"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil || code != 0 || errs != "" {
		t.Fatalf("long JSON: exit %d, output %q, stderr %q", code, raw, errs)
	}
	if len(data.Running.Rows) != 1 || len(data.NeedsYou.Rows) != 5 || len(data.Blocked.Rows) != 1 || len(data.Next.Rows) != 1 {
		t.Fatalf("long JSON lost logical rows: %s", raw)
	}
	for _, rows := range [][]string{data.Running.Rows, data.NeedsYou.Rows, data.Blocked.Rows, data.Next.Rows} {
		for _, row := range rows {
			if !strings.Contains(unfoldSummaryText(out), "  "+row+"\n") {
				t.Errorf("wrapped text lost JSON row %q", row)
			}
		}
	}
}

func TestStatusHelpNamesNeedsYouCounts(t *testing.T) {
	code, out, errs := runCLI(&App{}, "help")
	start := strings.Index(out, "  ork status [")
	end := strings.Index(out, "  ork show ")
	if code != 0 || errs != "" || start < 0 || end < start || !strings.Contains(out[start:end], "needs-you counts") {
		t.Fatalf("status help lost counts: exit %d, output %q, stderr %q", code, out, errs)
	}
}

func TestStatusSummaryDaemonErrorMatchesStatus(t *testing.T) {
	app := &App{Call: func(context.Context, string, any, any) error { return fmt.Errorf("daemon unavailable") }}
	plain, _, plainErr := runCLI(app, "status")
	code, _, errs := runCLI(app, "status", "--summary")
	if code != plain || errs != plainErr {
		t.Fatalf("summary: %d %q; status: %d %q", code, errs, plain, plainErr)
	}
}
