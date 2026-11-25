// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/source"
)

// localZone sets time.Local to a fixed zone two hours east of UTC for the test,
// so a time in the output proves the conversion from UTC.
func localZone(t *testing.T) {
	t.Helper()
	old := time.Local
	time.Local = time.FixedZone("T", 2*3600)
	t.Cleanup(func() { time.Local = old })
}

// statusOut runs ork status against one raw daemon reply with a fixed clock.
func statusOut(t *testing.T, raw string, now time.Time, args ...string) string {
	t.Helper()
	code, stdout, stderr := run(t, workingApp(raw, now), append([]string{"status"}, args...)...)
	if code != 0 {
		t.Fatalf("status exit %d: %s", code, stderr)
	}
	return stdout
}

func TestAgeText(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{-time.Minute, "0s"},
		{0, "0s"},
		{45 * time.Second, "45s"},
		{59*time.Second + 999*time.Millisecond, "59s"},
		{time.Minute, "1m"},
		{4*time.Minute + 30*time.Second, "4m"},
		{59 * time.Minute, "59m"},
		{time.Hour, "1h00m"},
		{2*time.Hour + 10*time.Minute, "2h10m"},
		{23*time.Hour + 59*time.Minute, "23h59m"},
		{24 * time.Hour, "1d00h"},
		{27 * time.Hour, "1d03h"},
		{50*time.Hour + 30*time.Minute, "2d02h"},
	} {
		if got := ageText(tc.d); got != tc.want {
			t.Errorf("ageText(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestClockText(t *testing.T) {
	localZone(t) // UTC+2: now is 05:05 on the 20th, local
	now := time.Date(2026, 11, 20, 3, 5, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		t    time.Time
		want string
	}{
		{"same local day", time.Date(2026, 11, 20, 0, 55, 0, 0, time.UTC), "02:55"},
		{"same UTC day, earlier local day", time.Date(2026, 11, 19, 21, 59, 0, 0, time.UTC), "11-19 23:59"},
		{"local midnight", time.Date(2026, 11, 19, 22, 0, 0, 0, time.UTC), "00:00"},
		{"other day", time.Date(2026, 11, 3, 8, 9, 0, 0, time.UTC), "11-03 10:09"},
		{"other year", time.Date(2025, 12, 31, 10, 0, 0, 0, time.UTC), "12-31 12:00"},
	} {
		if got := clockText(tc.t, now); got != tc.want {
			t.Errorf("%s: clockText = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSplitWait(t *testing.T) {
	for _, tc := range []struct {
		name, wait string
		lines      []string
		next       string
	}{
		{"empty", "", nil, ""},
		{"one line", "Which?", []string{"Which?"}, ""},
		{"next", "Which?; next: ork retry r1", []string{"Which?"}, "ork retry r1"},
		{"next only", "; next: ork stop r1", nil, "ork stop r1"},
		{"empty next is text", "Which?; next: ", []string{"Which?; next:"}, ""},
		{"next in the middle is text", "a; next: b\nc", []string{"a; next: b", "c"}, ""},
		{"lines", "A\r\n\nB\n  \nC; next: go", []string{"A", "B", "C"}, "go"},
		{"controls", "\x1b[31mA\x07", []string{"A"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines, next := splitWait(tc.wait)
			if !reflect.DeepEqual(lines, tc.lines) || next != tc.next {
				t.Errorf("splitWait(%q) = %q, %q; want %q, %q", tc.wait, lines, next, tc.lines, tc.next)
			}
		})
	}
}

func TestStatusHeaderCountsRunsInProgress(t *testing.T) {
	now := time.Date(2026, 11, 20, 3, 5, 0, 0, time.UTC)
	const raw = `{"projects":[{"name":"p","needs_you":1,"active_relays":[{"id":"run-A","items":["S1"],"state":"active"},{"id":"run-B","items":["S2"],"state":"active"}],"engine_runs":[
{"id":"r1","item":"S1","state":"implement"},
{"id":"r2","item":"S2","state":"checks"},
{"id":"r3","item":"S3","state":"queued"},
{"id":"r4","item":"S4","state":"awaiting-approval"},
{"id":"r5","item":"S5","state":"committed"}]}]}`
	out := statusOut(t, raw, now)
	if want := "p: needs you 1, in progress 3, active relays 2\n"; !strings.HasPrefix(out, want) {
		t.Fatalf("header:\n%s\nwant prefix %q", out, want)
	}
}

func TestStatusRowShowsWhenTheStateBegan(t *testing.T) {
	localZone(t)
	now := time.Date(2026, 11, 20, 3, 5, 0, 0, time.UTC) // 05:05 local
	for _, tc := range []struct{ name, run, want string }{
		{"paused waiting", `"state":"paused","wait_started_at":"2026-11-20T00:55:00Z"`, "r1 S1 paused, waiting since 02:55 (2h10m)"},
		{"approval waiting", `"state":"awaiting-approval","wait_started_at":"2026-11-20T00:55:00Z"`, "r1 S1 awaiting-approval, waiting since 02:55 (2h10m)"},
		{"checks updated", `"state":"checks","updated_at":"2026-11-20T03:01:00Z"`, "r1 S1 checks, updated 05:01 (4m ago)"},
		{"implement seconds", `"state":"implement","updated_at":"2026-11-20T03:04:15Z"`, "r1 S1 implement, updated 05:04 (45s ago)"},
		{"implement other day", `"state":"implement","updated_at":"2026-11-19T00:05:00Z"`, "r1 S1 implement, updated 11-19 02:05 (1d03h ago)"},
		{"zero time", `"state":"implement"`, "r1 S1 implement"},
		{"zero wait time", `"state":"paused"`, "r1 S1 paused"},
		{"paused ignores updated_at", `"state":"paused","updated_at":"2026-11-20T03:01:00Z"`, "r1 S1 paused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := statusOut(t, `{"projects":[{"name":"p","engine_runs":[{"id":"r1","item":"S1",`+tc.run+`}]}]}`, now)
			if want := "\n  " + tc.want + "\n"; !strings.Contains(out, want) {
				t.Fatalf("got:\n%swant a row %q", out, tc.want)
			}
		})
	}
}

func TestStatusPrintsAMultiLineQuestionOnSeparateLines(t *testing.T) {
	now := time.Date(2026, 11, 20, 3, 5, 0, 0, time.UTC)
	const question = `Which scope?\n1. update the assertion: edit the test\n2. leave it: ask later`
	for _, tc := range []struct{ name, wait, want string }{
		{"with next", question + `; next: ork answer ork-1 TEXT`,
			"  ork-1 S207 paused (wait: Which scope?)\n      1. update the assertion: edit the test\n      2. leave it: ask later\n    next: ork answer ork-1 TEXT\n"},
		{"without next", question,
			"  ork-1 S207 paused (wait: Which scope?)\n      1. update the assertion: edit the test\n      2. leave it: ask later\n"},
		{"one line with next", `Which library?; next: ork retry ork-1`,
			"  ork-1 S207 paused (wait: Which library?)\n    next: ork retry ork-1\n"},
		{"empty lines dropped", `A\n\n\nB`,
			"  ork-1 S207 paused (wait: A)\n      B\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := statusOut(t, `{"projects":[{"name":"p","engine_runs":[{"id":"ork-1","item":"S207","state":"paused","wait":"`+tc.wait+`"}]}]}`, now)
			if want := "p: needs you 0, in progress 0, active relays 0\n" + tc.want; out != want {
				t.Fatalf("got:\n%swant:\n%s", out, want)
			}
		})
	}
}

func TestStatusRemovesControlCharactersFromAWait(t *testing.T) {
	now := time.Date(2026, 11, 20, 3, 5, 0, 0, time.UTC)
	out := statusOut(t, `{"projects":[{"name":"p","engine_runs":[{"id":"ork-1","item":"S1","state":"paused","wait":"Red \u001b[31mwords\u0007\nsecond \u001b[0mline; next: ork retry ork-1\u0007"}]}]}`, now)
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Fatalf("a control character reached the status: %q", out)
	}
	if want := "(wait: Red words)\n      second line\n    next: ork retry ork-1\n"; !strings.Contains(out, want) {
		t.Fatalf("got:\n%swant %q", out, want)
	}
}

func TestStatusLabelsRelayRows(t *testing.T) {
	now := time.Date(2026, 11, 20, 3, 5, 0, 0, time.UTC)
	const raw = `{"projects":[{"name":"p","active_relays":[{"id":"run-9","items":["S100"],"state":"active","steps":3,"running_step":{"role":"implementer","vendor":"codex","model":"gpt-6.1","task":"Implement S100"},"failed_step":{"role":"reviewer","task":"Review S100"}}]}]}`
	out := statusOut(t, raw, now)
	want := "  relay run-9 S100 active (3 steps, 0 running)\n    running: implementer codex/gpt-6.1: Implement S100\n    failed: reviewer: Review S100\n"
	if !strings.Contains(out, want) {
		t.Fatalf("got:\n%swant:\n%s", out, want)
	}
}

func TestStatusListsEveryFindingTheCountIncludes(t *testing.T) {
	now := time.Date(2026, 11, 20, 3, 5, 0, 0, time.UTC)
	const raw = `{"projects":[{"name":"p","needs_you":4,"engine_runs":[
{"id":"ork-a","item":"S1","state":"awaiting-approval","wait":"owner approval","findings":[{"n":1,"kind":"owner","title":"Pick \u001b[31mone"},{"n":2,"kind":"owner","title":"Answered","answer":"yes"}]},
{"id":"ork-b","item":"S2","state":"committed","findings":[{"n":1,"kind":"later","title":"First"},{"n":2,"kind":"later","title":"Second"},{"n":3,"kind":"later","title":"Filed","filed":"S900"}]},
{"id":"ork-c","item":"S3","state":"committed","landed":true,"findings":[{"n":1,"kind":"later","title":"Landed"},{"n":2,"kind":"owner","title":"Landed owner"}]},
{"id":"ork-d","item":"S4","state":"stopped","dismissed":true,"findings":[{"n":1,"kind":"later","title":"Dismissed"}]}]}]}`
	out := statusOut(t, raw, now)
	want := "p: needs you 4, in progress 0, active relays 0\n" +
		"  ork-a S1 awaiting-approval (wait: owner approval)\n" +
		"  finding ork-a 1 owner: Pick one (answer it in the TUI run pane)\n" +
		"  finding ork-b 1 later: First\n" +
		"    file it: ork file ork-b 1\n" +
		"  finding ork-b 2 later: Second\n" +
		"    file it: ork file ork-b 2\n"
	if out != want {
		t.Fatalf("got:\n%swant:\n%s", out, want)
	}
	// needs_you is the count these lines add up to: the run row and the finding
	// lines; the "file it" line belongs to its finding.
	counted := 0
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n")[1:] {
		if !strings.HasPrefix(line, "    ") {
			counted++
		}
	}
	if counted != 4 {
		t.Fatalf("%d counted lines, want 4", counted)
	}
	// A real run id (24 columns) keeps the command whole on its own line, inside
	// 80 columns.
	long := "ork-20261120-120000-7a8b"
	fl := findingLines([]source.EngineRun{{ID: long, Item: "S1", State: "committed", Findings: []source.EngineFinding{{N: 2, Kind: "later", Title: "T"}}}})
	if len(fl) != 2 || len("  "+fl[1]) > 80 || !strings.HasSuffix(fl[1], "file it: ork file "+long+" 2") {
		t.Fatalf("long id lines: %q", fl)
	}
}

func TestStatusShowsTheWaveOfARow(t *testing.T) {
	now := time.Date(2026, 11, 20, 3, 5, 0, 0, time.UTC)
	const raw = `{"projects":[{"name":"p","engine_runs":[
{"id":"ork-1","item":"S1","state":"paused","wave":"wave-20261120-ab","wait":"Which scope?\n1. a\n2. b; next: ork answer ork-1 TEXT"},
{"id":"ork-2","item":"S2","state":"implement"}]}]}`
	out := statusOut(t, raw, now)
	// The wave comes last of the wait block: after the options and the next: line.
	want := "  ork-1 S1 paused (wait: Which scope?)\n      1. a\n      2. b\n    next: ork answer ork-1 TEXT\n    wave: wave-20261120-ab\n  ork-2 S2 implement\n"
	if !strings.Contains(out, want) {
		t.Fatalf("got:\n%swant:\n%s", out, want)
	}
	if strings.Count(out, "wave:") != 1 {
		t.Fatalf("a row without a wave printed one:\n%s", out)
	}
}

// The JSON of status is the daemon's result, re-encoded, with no key of this
// slice added.
func TestStatusJSONIsUnchanged(t *testing.T) {
	const raw = `{"build_id":"dev","roles_running":false,"projects":[{"name":"p","needs_you":2,"active_relays":[{"id":"run-9","items":["S1"],"state":"active","steps":3}],"engine_runs":[{"id":"ork-1","item":"S1","state":"paused","wait":"Q?\nnext","wave":"wave-1","created_at":"2026-11-20T00:00:00Z","wait_started_at":"2026-11-20T00:55:00Z","findings":[{"n":1,"kind":"later","title":"T"}]}]}]}`
	var res daemon.StatusResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out := statusOut(t, raw, time.Date(2026, 11, 20, 3, 5, 0, 0, time.UTC), "--json")
	if strings.TrimSpace(out) != string(want) {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestOwnerCommandHintsWhenGivenARelayID(t *testing.T) {
	const relay = "run-20261102-aa"
	const hinted = "no run " + relay + " (" + relay + " is a slicer relay run; use the ork- id that starts the run's row in ork status)"
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"approve", []string{"approve", "ID"}},
		{"stop", []string{"stop", "ID"}},
		{"answer", []string{"answer", "ID", "text"}},
		{"retry", []string{"retry", "ID"}},
		{"dismiss", []string{"dismiss", "ID"}},
		{"fix", []string{"fix", "ID", "text"}},
		{"check", []string{"check", "ID", "1"}},
		{"file", []string{"file", "ID", "1"}},
	} {
		run := func(id string, extra ...string) (int, string, string) {
			args := append([]string(nil), tc.args...)
			for i := range args {
				if args[i] == "ID" {
					args[i] = id
				}
			}
			var out, errs bytes.Buffer
			app := &App{Call: func(context.Context, string, any, any) error {
				return &ipc.Error{Code: daemon.CodeNotFound, Message: "no run " + id}
			}}
			code := app.Run(context.Background(), append(args, extra...), &out, &errs)
			return code, out.String(), errs.String()
		}
		t.Run(tc.name+" text", func(t *testing.T) {
			code, out, errs := run(relay)
			if code != ExitFailure || out != "" || errs != "ork: "+hinted+"\n" {
				t.Fatalf("exit %d out %q errs %q", code, out, errs)
			}
		})
		t.Run(tc.name+" json", func(t *testing.T) {
			code, out, errs := run(relay, "--json")
			var got struct{ Error ipc.Error }
			mustJSON(t, out, &got)
			if code != ExitFailure || errs != "" || got.Error.Code != daemon.CodeNotFound || got.Error.Message != hinted {
				t.Fatalf("exit %d out %s errs %q", code, out, errs)
			}
		})
		t.Run(tc.name+" other id", func(t *testing.T) {
			code, _, errs := run("ork-1")
			if code != ExitFailure || errs != "ork: no run ork-1\n" {
				t.Fatalf("exit %d errs %q", code, errs)
			}
		})
	}
}

// The hint is for a run that is not found; any other error keeps its text.
func TestOwnerCommandHintsOnlyANotFoundError(t *testing.T) {
	var out, errs bytes.Buffer
	app := &App{Call: func(context.Context, string, any, any) error {
		return &ipc.Error{Code: daemon.CodeRefused, Message: "run run-1 is not awaiting approval"}
	}}
	code := app.Run(context.Background(), []string{"approve", "run-1"}, &out, &errs)
	if code != ExitFailure || strings.Contains(errs.String(), "slicer relay run") {
		t.Fatalf("exit %d errs %q", code, errs.String())
	}
}

func TestUsageSaysWhichIDRunIs(t *testing.T) {
	var out, errs bytes.Buffer
	if code := (&App{}).Run(context.Background(), []string{"help"}, &out, &errs); code != ExitOK {
		t.Fatalf("help: code %d: %s", code, errs.String())
	}
	flat := strings.Join(strings.Fields(out.String()), " ")
	if want := "RUN is the ork- id that starts a run's row in ork status"; !strings.Contains(flat, want) {
		t.Fatalf("help lacks %q:\n%s", want, out.String())
	}
	if want := "projects, needs-you counts, runs in progress and active relays"; !strings.Contains(flat, want) {
		t.Fatalf("help lacks the status line %q:\n%s", want, out.String())
	}
}
