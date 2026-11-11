// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// promptRun is the result of one `ork run p S1` with scripted stdin.
type promptRun struct {
	code    int
	out     string
	errs    string
	plan    string
	started bool
}

// runPrompted runs `ork run p S1` with stdin input and records the run.plan
// params and whether run.start was called.
func runPrompted(t *testing.T, input string, args ...string) promptRun {
	t.Helper()
	var out, errs bytes.Buffer
	var r promptRun
	app := &App{Input: strings.NewReader(input), Call: func(_ context.Context, method string, params, result any) error {
		switch method {
		case "run.plan":
			raw, _ := json.Marshal(params)
			r.plan = string(raw)
			return json.Unmarshal([]byte(`{"project":{"name":"p"},"item":"S1","check_command":"go test ./..."}`), result)
		case "spec.check":
			return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true}]}`), result)
		case "run.start":
			r.started = true
			return json.Unmarshal([]byte(`{"id":"r1","state":"setup"}`), result)
		}
		t.Fatalf("method %s", method)
		return nil
	}}
	r.code = app.Run(context.Background(), append([]string{"run", "p", "S1"}, args...), &out, &errs)
	r.out, r.errs = out.String(), errs.String()
	return r
}

func TestRunUsageSaysYAndN(t *testing.T) {
	r := runPrompted(t, "", "--yes", "--well-specified", "maybe", "--split", "n")
	if r.code != ExitUsage || !strings.Contains(r.errs, "run: --well-specified y/n and --split y/n each take y or n") || strings.Contains(r.errs, "yes|no") {
		t.Fatalf("usage error: code %d: %s", r.code, r.errs)
	}
	var out, errs bytes.Buffer
	if code := (&App{}).Run(context.Background(), []string{"help"}, &out, &errs); code != ExitOK {
		t.Fatalf("help: code %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "ork run P ID [--yes] [--well-specified y/n] [--split y/n]") || strings.Contains(out.String(), "yes|no") {
		t.Fatalf("usage text: %s", out.String())
	}
}

func TestWaveStartUsageSaysYAndN(t *testing.T) {
	var out, errs bytes.Buffer
	code := (&App{}).Run(context.Background(), []string{"wave", "start", "p", "S1", "--yes", "--well-specified", "maybe", "--split", "n"}, &out, &errs)
	if code != ExitUsage || !strings.Contains(errs.String(), "wave start: --well-specified y/n and --split y/n each take y or n") || strings.Contains(errs.String(), "yes|no") {
		t.Fatalf("usage error: code %d: %s", code, errs.String())
	}
	// parse discards the flag package's help output, so no run of the command
	// shows the flag help strings; check the source that defines them.
	for _, file := range []string{"run.go", "wave.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "yes or no") {
			t.Errorf("%s: flag help still says yes or no", file)
		}
	}
	wave, err := os.ReadFile("wave.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(wave), `"y/n for every item"`); n != 2 {
		t.Errorf("wave.go: %d flag help strings say y/n for every item, want 2", n)
	}
}

func TestRunConfirmTakesYAndN(t *testing.T) {
	for _, tc := range []struct {
		answer string
		start  bool
	}{{"y", true}, {"Y", true}, {"yes", true}, {"YES", true}, {"n", false}, {"N", false}, {"no", false}, {"No", false}} {
		t.Run(tc.answer, func(t *testing.T) {
			r := runPrompted(t, tc.answer+"\n")
			if r.code != ExitOK || r.started != tc.start {
				t.Fatalf("code %d started %t: %s %s", r.code, r.started, r.out, r.errs)
			}
		})
	}
}

func TestRunConfirmEnterTakesNo(t *testing.T) {
	r := runPrompted(t, "\n")
	if r.code != ExitOK || r.started || !strings.Contains(r.out, "cancelled") {
		t.Fatalf("code %d started %t: %s %s", r.code, r.started, r.out, r.errs)
	}
}

func TestRunConfirmPromptShowsDefaultNo(t *testing.T) {
	r := runPrompted(t, "n\n")
	if !strings.Contains(r.out, "Start this slice? [y/N]: ") {
		t.Fatalf("confirm hint: %s", r.out)
	}
}

func TestRunFlagsAcceptYAndN(t *testing.T) {
	for _, tc := range []struct {
		well, split         string
		wantWell, wantSplit bool
	}{{"Y", "N", true, false}, {"no", "YES", false, true}, {"yes", "n", true, false}, {"N", "y", false, true}} {
		t.Run(tc.well+"/"+tc.split, func(t *testing.T) {
			r := runPrompted(t, "", "--yes", "--well-specified", tc.well, "--split", tc.split)
			want := fmt.Sprintf(`{"project":"p","item":"S1","well_specified":%t,"split":%t}`, tc.wantWell, tc.wantSplit)
			if r.code != ExitOK || r.plan != want || !r.started {
				t.Fatalf("code %d plan %s started %t: %s", r.code, r.plan, r.started, r.errs)
			}
		})
	}
}

func TestRunRejectsOtherAnswers(t *testing.T) {
	r := runPrompted(t, "sure\n")
	if r.code != ExitUsage || r.started {
		t.Fatalf("confirm: code %d started %t: %s", r.code, r.started, r.errs)
	}
}

func TestRunConfirmClosedInputIsNotEnter(t *testing.T) {
	r := runPrompted(t, "")
	if r.code != ExitUsage || r.started || strings.Contains(r.out, "cancelled") {
		t.Fatalf("code %d started %t: %s %s", r.code, r.started, r.out, r.errs)
	}
}

func TestRunShowsPlanBeforeConfirmation(t *testing.T) {
	for _, answer := range []string{"no", "yes"} {
		t.Run(answer, func(t *testing.T) {
			var out, errs bytes.Buffer
			app := &App{Input: strings.NewReader(answer + "\n")}
			app.Call = func(_ context.Context, method string, params, result any) error {
				switch method {
				case "run.plan":
					raw, _ := json.Marshal(params)
					if string(raw) != `{"project":"p","item":"S1","well_specified":true,"split":false}` {
						t.Fatalf("inputs %s", raw)
					}
					b := []byte(`{"project":{"name":"p"},"item":"S1","worktree":"/p/.worktrees/S1","branch":"S1","check_command":"go test ./...","lineup":{"implementer":[{"vendor":"claude","model":"sonnet","effort":"high"}]},"permissions":{"implementer":{"ReadOnly":false,"Allow":["Read","Write"]}}}`)
					return json.Unmarshal(b, result)
				case "spec.check":
					return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true}]}`), result)
				case "run.start":
					if !strings.Contains(out.String(), "/p/.worktrees/S1") || !strings.Contains(out.String(), "go test ./...") || !strings.Contains(out.String(), "sonnet") || !strings.Contains(out.String(), "Write") {
						t.Fatalf("plan missing before start: %s", &out)
					}
					return json.Unmarshal([]byte(`{"id":"r1","state":"setup"}`), result)
				default:
					t.Fatalf("method %s", method)
				}
				return nil
			}
			code := app.Run(context.Background(), []string{"run", "p", "S1"}, &out, &errs)
			if code != 0 {
				t.Fatalf("code %d: %s", code, &errs)
			}
			if strings.Contains(out.String(), "r1") != (answer == "yes") {
				t.Fatalf("confirmation %s: %s", answer, &out)
			}
		})
	}
}

// The plan says a latest model is the latest and what it resolves to (S245,
// *proposed*); a pinned model prints its name.
func TestRunPlanTextShowsLatestResolution(t *testing.T) {
	var out, errs bytes.Buffer
	app := &App{Input: strings.NewReader("no\n")}
	app.Call = func(_ context.Context, method string, params, result any) error {
		switch method {
		case "run.plan":
			b := []byte(`{"project":{"name":"p"},"item":"S1","check_command":"go test ./...","lineup":{"implementer":[{"vendor":"codex","model":"gpt-6.2-sol","effort":"medium","requested":"gpt-*-sol@latest"}],"reviewer":[{"vendor":"claude","model":"opus","effort":"high"}]}}`)
			return json.Unmarshal(b, result)
		case "spec.check":
			return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true}]}`), result)
		}
		t.Fatalf("method %s", method)
		return nil
	}
	if code := app.Run(context.Background(), []string{"run", "p", "S1"}, &out, &errs); code != 0 {
		t.Fatalf("code %d: %s", code, &errs)
	}
	for _, want := range []string{"implementer: codex latest (resolves to gpt-6.2-sol) medium\n", "reviewer: claude opus high\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan lacks %q:\n%s", want, &out)
		}
	}
}

func TestRunYesStartsAndOwnerCommands(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		method string
	}{
		{[]string{"run", "p", "S1", "--yes", "--well-specified", "no", "--split", "yes", "--json"}, "run.start"},
		{[]string{"approve", "r1", "--json"}, "run.approve"},
		{[]string{"stop", "r1", "--json"}, "run.stop"},
		{[]string{"dismiss", "r1", "--json"}, "run.dismiss"},
		{[]string{"answer", "r1", "use stdlib", "--json"}, "run.answer"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			var out, errs bytes.Buffer
			app := &App{Call: func(_ context.Context, method string, params, result any) error {
				if method == "run.plan" {
					return json.Unmarshal([]byte(`{"project":{"name":"p"},"item":"S1","check_command":"go test ./..."}`), result)
				}
				if method == daemon.MethodSpecCheck {
					return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true}]}`), result)
				}
				if method != tc.method {
					t.Fatalf("method %s", method)
				}
				if method == "run.dismiss" {
					b, _ := json.Marshal(params)
					if string(b) != `{"run":"r1"}` {
						t.Fatalf("dismiss params %s", b)
					}
				}
				if method == "run.answer" {
					b, _ := json.Marshal(params)
					if !strings.Contains(string(b), `"text":"use stdlib"`) {
						t.Fatalf("answer %s", b)
					}
				}
				return json.Unmarshal([]byte(`{"id":"r1","state":"stopped"}`), result)
			}}
			if code := app.Run(context.Background(), tc.args, &out, &errs); code != 0 {
				t.Fatalf("code %d: %s", code, &errs)
			}
			var rec runrec.Record
			if err := json.Unmarshal(out.Bytes(), &rec); err != nil || rec.ID != "r1" {
				t.Fatalf("result %s (%v)", &out, err)
			}
		})
	}
}

func TestStatusPrintsEngineStateAndWait(t *testing.T) {
	var out, errs bytes.Buffer
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		return json.Unmarshal([]byte(`{"projects":[{"name":"p","needs_you":1,"engine_runs":[{"id":"r1","item":"S1","state":"paused","wait":"Which library?","denials":["Write","Bash"],"error":"background broke"}]}]}`), result)
	}}
	code := app.Run(context.Background(), []string{"status"}, &out, &errs)
	if code != 0 || !strings.Contains(out.String(), "r1 S1 paused (wait: Which library?) (denied: 2 commands, first: Write)\n    error: background broke\n") {
		t.Fatalf("status code %d: %s %s", code, &out, &errs)
	}
}

// A running role or the checks step shows how long it has run, how long ago
// the role sent output and how long the machine slept meanwhile (S235).
func workingApp(raw string, now time.Time) *App {
	return &App{Now: func() time.Time { return now }, Call: func(_ context.Context, method string, params, result any) error {
		return json.Unmarshal([]byte(raw), result)
	}}
}

func TestStatusPrintsWorkingTimes(t *testing.T) {
	now := time.Date(2026, 11, 2, 22, 0, 0, 0, time.UTC)
	app := workingApp(`{"projects":[{"name":"p","engine_runs":[{"id":"r1","item":"S1","state":"implement","error":"broke","working":[{"what":"implementer round 2","since":"2026-11-02T21:46:00Z","last_output":"2026-11-02T21:58:00Z","slept_seconds":3780}]}]}]}`, now)
	code, stdout, stderr := run(t, app, "status")
	want := "  r1 S1 implement\n    working: implementer round 2, 14m (last output 2m ago, slept 1h03m)\n    error: broke\n"
	if code != 0 || !strings.Contains(stdout, want) {
		t.Fatalf("status exit %d: %s\ngot:\n%swant:\n%s", code, stderr, stdout, want)
	}
}

func TestStatusPrintsEachWorkingEntry(t *testing.T) {
	now := time.Date(2026, 11, 2, 22, 0, 0, 0, time.UTC)
	app := workingApp(`{"projects":[{"name":"p","engine_runs":[{"id":"r1","item":"S1","state":"review","working":[{"what":"reviewer round 1","since":"2026-11-02T21:59:15Z","last_output":"2026-11-02T21:59:50Z"},{"what":"ui round 1","since":"2026-11-02T21:30:00Z"}]}]}]}`, now)
	_, stdout, _ := run(t, app, "status")
	want := "    working: reviewer round 1, 45s (last output 10s ago)\n    working: ui round 1, 30m\n"
	if !strings.Contains(stdout, want) {
		t.Fatalf("got:\n%swant:\n%s", stdout, want)
	}
}

func TestStatusPrintsChecksWorkingWithoutLastOutput(t *testing.T) {
	now := time.Date(2026, 11, 2, 22, 0, 0, 0, time.UTC)
	app := workingApp(`{"projects":[{"name":"p","engine_runs":[{"id":"r1","item":"S1","state":"checks","working":[{"what":"checks","since":"2026-11-02T21:48:00Z"}]}]}]}`, now)
	_, stdout, _ := run(t, app, "status")
	if want := "    working: checks, 12m\n"; !strings.Contains(stdout, want) {
		t.Fatalf("got:\n%swant:\n%s", stdout, want)
	}
}

// A status names the command of the run's newest check row, so the round
// command and the full command can be told apart (S285).
func TestStatusNamesTheCheckCommandThatRan(t *testing.T) {
	now := time.Date(2026, 11, 2, 22, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ name, check, want string }{
		{"round", `{"round":2,"ok":true,"command":"fast","log":"checks-2.log"}`, "  r1 S1 review\n    last check: fast (ok, round 2)\n"},
		{"full", `{"round":2,"ok":false,"command":"full\u001b[31m","log":"checks-full-2.log"}`, "  r1 S1 review\n    last check: full (red, round 2)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := workingApp(`{"projects":[{"name":"p","engine_runs":[{"id":"r1","item":"S1","state":"review","last_check":`+tc.check+`}]}]}`, now)
			code, stdout, stderr := run(t, app, "status")
			if code != 0 || !strings.Contains(stdout, tc.want) {
				t.Fatalf("status exit %d: %s\ngot:\n%swant:\n%s", code, stderr, stdout, tc.want)
			}
			if strings.Contains(stdout, "\x1b") {
				t.Errorf("a terminal control reached the status: %q", stdout)
			}
		})
	}
	// A run with no check row has no such line.
	app := workingApp(`{"projects":[{"name":"p","engine_runs":[{"id":"r1","item":"S1","state":"implement"}]}]}`, now)
	if _, stdout, _ := run(t, app, "status"); strings.Contains(stdout, "last check") {
		t.Errorf("status names a check that did not run:\n%s", stdout)
	}
}

func TestStatusWorkingJSONKeepsTheTimes(t *testing.T) {
	now := time.Date(2026, 11, 2, 22, 0, 0, 0, time.UTC)
	app := workingApp(`{"projects":[{"name":"p","engine_runs":[{"id":"r1","item":"S1","state":"implement","working":[{"what":"implementer round 2","since":"2026-11-02T21:46:00Z","last_output":"2026-11-02T21:58:00Z","slept_seconds":3780}]}]}]}`, now)
	_, stdout, _ := run(t, app, "status", "--json")
	for _, want := range []string{`"since": "2026-11-02T21:46:00Z"`, `"last_output": "2026-11-02T21:58:00Z"`, `"slept_seconds": 3780`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("json lacks %s:\n%s", want, stdout)
		}
	}
}

// After a red re-check and its fix round the owner approves a second time;
// status says so (S238).
func TestStatusSaysApprovalIsRepeatedAfterABlockedLanding(t *testing.T) {
	var out, errs bytes.Buffer
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		return json.Unmarshal([]byte(`{"projects":[{"name":"p","needs_you":1,"engine_runs":[{"id":"r1","item":"S1","state":"awaiting-approval","wait":"owner approval","land_fixes":1}]}]}`), result)
	}}
	code := app.Run(context.Background(), []string{"status"}, &out, &errs)
	want := "  r1 S1 awaiting-approval (wait: owner approval, again: the landing was blocked and its fix round is done)\n"
	if code != 0 || !strings.Contains(out.String(), want) {
		t.Fatalf("status code %d: %s %s", code, &out, &errs)
	}
}

func TestStatusFirstApprovalIsUnchanged(t *testing.T) {
	var out, errs bytes.Buffer
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		return json.Unmarshal([]byte(`{"projects":[{"name":"p","needs_you":1,"engine_runs":[{"id":"r1","item":"S1","state":"awaiting-approval","wait":"owner approval","land_fixes":0}]}]}`), result)
	}}
	code := app.Run(context.Background(), []string{"status"}, &out, &errs)
	want := "  r1 S1 awaiting-approval (wait: owner approval)\n"
	if code != 0 || !strings.Contains(out.String(), want) || strings.Contains(out.String(), "again") {
		t.Fatalf("status code %d: %s %s", code, &out, &errs)
	}
}

func TestStatusSectionsAndNeedsRows(t *testing.T) {
	const reply = `{"projects":[{"name":"p","needs_you":5,"open":[{"id":125,"key":"ui.status-list","title":"Status list"}],"held":[{"id":"S32","title":"Owner answer"}],"active_relays":[{"id":"ledger","items":["S1"],"state":"active"}],"engine_runs":[{"id":"old","item":"S1","state":"stopped","created_at":"2026-10-23T12:00:00Z"},{"id":"current","item":"S1","state":"implement","created_at":"2026-10-23T12:01:00Z"},{"id":"done","item":"S2","state":"committed"},{"id":"approval","item":"S3","state":"awaiting-approval"},{"id":"paused","item":"S4","state":"paused","wait":"Question?","denials":["Write"]},{"id":"stopped","item":"S5","state":"stopped","updated_at":"2026-10-23T12:00:00Z"}]}]}`
	app := &App{Call: func(_ context.Context, _ string, _, result any) error { return json.Unmarshal([]byte(reply), result) }}
	code, stdout, stderr := run(t, app, "status")
	want := "p: needs you 5, active relays 1\n  #125 ui.status-list  Status list\n  S32  Owner answer  (needs-owner)\n  ledger S1 active (0 steps, 0 running)\n  approval S3 awaiting-approval\n  paused S4 paused (wait: Question?) (denied: 1 command, first: Write)\n  stopped S5 stopped\n  current S1 implement\n"
	if code != 0 || stdout != want {
		t.Fatalf("status exit %d: %s\ngot:\n%swant:\n%s", code, stderr, stdout, want)
	}
	code, stdout, stderr = run(t, app, "status", "--json")
	var got struct {
		Projects []struct {
			EngineRuns []source.EngineRun `json:"engine_runs"`
			Open       []struct {
				ID         int
				Key, Title string
			}
			Held []struct{ ID, Title string }
		}
	}
	mustJSON(t, stdout, &got)
	if code != 0 || len(got.Projects) != 1 {
		t.Fatalf("json exit %d: %s %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"updated_at": "2026-10-23T12:00:00Z"`) {
		t.Fatalf("status JSON lost updated_at: %s", stdout)
	}
	p := got.Projects[0]
	if len(p.EngineRuns) != 6 || p.EngineRuns[0].ID != "old" || p.EngineRuns[2].ID != "done" || len(p.Open) != 1 || p.Open[0].Key != "ui.status-list" || len(p.Held) != 1 || p.Held[0].Title != "Owner answer" {
		t.Fatalf("json lost status details: %s", stdout)
	}
}

func TestStatusPrintsUnavailableGroup(t *testing.T) {
	var out, errs bytes.Buffer
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		return json.Unmarshal([]byte(`{"projects":[{"name":"removed","unavailable":true,"needs_you":1,"engine_runs":[{"id":"missing","item":"S35","state":"implement","unavailable":true,"error":"project \"removed\" is not registered"}]}]}`), result)
	}}
	code := app.Run(context.Background(), []string{"status"}, &out, &errs)
	if code != 0 || !strings.Contains(out.String(), "removed (removed): needs you 1") || !strings.Contains(out.String(), "missing S35 implement\n    error: project \"removed\" is not registered") {
		t.Fatalf("status %d: %s %s", code, &out, &errs)
	}
}

func TestStatusFlattensRunError(t *testing.T) {
	var out, errs bytes.Buffer
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		return json.Unmarshal([]byte(`{"projects":[{"name":"p","engine_runs":[{"id":"r","item":"S35","state":"implement","error":"first\nsecond\r\nthird"}]}]}`), result)
	}}
	if code := app.Run(context.Background(), []string{"status"}, &out, &errs); code != 0 || !strings.Contains(out.String(), "\n    error: first second third\n") {
		t.Errorf("run error not indented and flattened: %s", &out)
	}
}

func TestBehindNoticeOnlyWhenIdle(t *testing.T) {
	for _, command := range []string{"status", "doctor"} {
		for _, active := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/active=%v", command, active), func(t *testing.T) {
				app, _, _ := doctorApp(t, "Contract: relay v10")
				app.Call = func(_ context.Context, method string, params, result any) error {
					if method == daemon.MethodStatus {
						return json.Unmarshal([]byte(fmt.Sprintf(`{"build_id":"dev","roles_running":%v,"projects":[{"name":"ork","self_host":true,"behind_main":true}]}`, active)), result)
					}
					if method == daemon.MethodDaemonInfo {
						return json.Unmarshal([]byte(`{"build_id":"dev"}`), result)
					}
					return fmt.Errorf("unexpected %s", method)
				}
				code, out, errout := run(t, app, command)
				if code != 0 {
					t.Fatalf("code %d: %s %s", code, out, errout)
				}
				notice := "daemon behind main: restart with ork daemon restart"
				if strings.Contains(out+errout, notice) != !active {
					t.Fatalf("active=%v: %s %s", active, out, errout)
				}
				if !active {
					want, stream := "ork: warning: "+notice, errout
					if command == "doctor" {
						want, stream = "[WARN] "+notice, out
					}
					if !strings.Contains("\n"+stream, "\n"+want+"\n") {
						t.Fatalf("missing exact prefixed notice %q: stdout=%q stderr=%q", want, out, errout)
					}
					other := out
					if command == "doctor" {
						other = errout
					}
					if strings.Contains(other, notice) {
						t.Fatalf("notice on wrong stream: stdout=%q stderr=%q", out, errout)
					}
				}
			})
		}
	}
}

func TestBehindNoticeOverDaemonConnection(t *testing.T) {
	for _, command := range []string{"status", "doctor"} {
		t.Run(command, func(t *testing.T) {
			app, _, _ := doctorApp(t, "Contract: relay v10")
			predatingDaemon(t, app.Socket, func() (any, error) {
				return daemon.StatusResult{BuildID: "dev", Projects: []daemon.ProjectStatus{{Name: "ork", SelfHost: true, BehindMain: true}}}, nil
			})
			code, out, stderr := run(t, app, command)
			if code != 0 || !strings.Contains(out+stderr, daemon.BehindNotice) {
				t.Fatalf("exit %d: %s %s", code, out, stderr)
			}
		})
	}
}

func TestPlanTextShowsTheArchitectRow(t *testing.T) {
	for _, present := range []bool{false, true} {
		plan := daemon.RunPlan{Lineup: map[string][]runrec.LineupEntry{}}
		if present {
			plan.Lineup["architect"] = []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}}
		}
		var out bytes.Buffer
		printRunPlan(&out, plan, false)
		if strings.Contains(out.String(), "architect: codex sol high") != present {
			t.Fatalf("present=%v: %s", present, out.String())
		}
	}
}

// The UI pass row is part of the plan the owner confirms (S136): its
// vendor, model and effort, and that it is read-only.
func TestPlanShowsUIPassRow(t *testing.T) {
	for _, present := range []bool{false, true} {
		plan := daemon.RunPlan{Lineup: map[string][]runrec.LineupEntry{}, Permissions: map[string]engine.Permission{}}
		if present {
			plan.Lineup["ui"] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}}
			plan.Permissions["ui"] = engine.Permission{ReadOnly: true}
		}
		var out bytes.Buffer
		printRunPlan(&out, plan, false)
		if strings.Contains(out.String(), "ui: claude opus medium\n  permissions: read-only") != present {
			t.Fatalf("present=%v: %s", present, out.String())
		}
	}
}

// A plan made without the UI or spec-check rules says so (S159): the note was
// in the RPC and the JSON only. It follows the plan lines.
func TestPrintRunPlanShowsTheNote(t *testing.T) {
	const note = "slicer relay-roles show failed; planning without UI or spec-check rules: exit 2"
	for _, present := range []bool{false, true} {
		plan := daemon.RunPlan{Lineup: map[string][]runrec.LineupEntry{"implementer": {{Vendor: "codex", Model: "sol", Effort: "high"}}}, Permissions: map[string]engine.Permission{}}
		if present {
			plan.Note = note
		}
		var out bytes.Buffer
		printRunPlan(&out, plan, false)
		got := out.String()
		if strings.Contains(got, "note:") != present {
			t.Fatalf("present=%v: %s", present, got)
		}
		if present && !strings.HasSuffix(got, "\nnote: "+note+"\n") {
			t.Errorf("the note does not follow the plan lines: %q", got)
		}
	}
}

// overrideRun records the wire params without starting a daemon or vendor.
func overrideRun(t *testing.T, flags ...string) ([]daemon.RunParams, string) {
	t.Helper()
	var calls []daemon.RunParams
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		if method == daemon.MethodSpecCheck {
			return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true}]}`), result)
		}
		p := params.(daemon.RunParams)
		calls = append(calls, p)
		if method == daemon.MethodRunPlan {
			*result.(*daemon.RunPlan) = daemon.RunPlan{CheckCommand: "go test ./...", StopAfter: p.StopAfter, Item: p.Item, Lineup: p.Lineup}
		} else if method != daemon.MethodRunStart {
			t.Fatalf("method %s", method)
		}
		return nil
	}}
	args := append([]string{"run", "p", "S1", "--yes", "--well-specified", "y", "--split", "n"}, flags...)
	code, out, errs := run(t, app, args...)
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, errs)
	}
	if len(calls) != 2 || calls[1].Expected == nil || calls[1].Expected.Item != "S1" {
		t.Fatalf("calls: %+v", calls)
	}
	return calls, out
}
func TestRunLineupFlagsSendLineupOnPlanAndStart(t *testing.T) {
	calls, out := overrideRun(t, "--implementer", "codex:gpt-6.1-sol:medium", "--implementer", "claude:sonnet:high", "--reviewer", "claude:model:with:colons:high")
	for _, p := range calls {
		chain := p.Lineup["implementer"]
		if len(chain) != 2 || chain[0].Model != "gpt-6.1-sol" || chain[1].Vendor != "claude" || p.Lineup["reviewer"][0].Model != "model:with:colons" {
			t.Fatalf("lineup: %+v", p.Lineup)
		}
	}
	if !strings.Contains(out, "implementer: codex gpt-6.1-sol medium") || !strings.Contains(out, "implementer: claude sonnet high") {
		t.Fatal(out)
	}
	if !reflect.DeepEqual(calls[1].Expected.Lineup, calls[0].Lineup) {
		t.Fatal("Expected lost printed lineup")
	}
}
func TestRunArchitectOffSendsEmptyArchitect(t *testing.T) {
	calls, _ := overrideRun(t, "--architect", "off")
	for _, p := range calls {
		raw, _ := json.Marshal(p)
		if !strings.Contains(string(raw), `"architect":[]`) {
			t.Fatalf("wire: %s", raw)
		}
	}
}
func TestRunBudgetFlagsSendBudgets(t *testing.T) {
	calls, _ := overrideRun(t, "--budget-minutes", "20", "--budget-tokens", "0")
	for _, p := range calls {
		if p.Budgets == nil || p.Budgets.Minutes != 20 || p.Budgets.Tokens != 0 {
			t.Fatalf("budgets: %+v", p.Budgets)
		}
	}
}
func TestRunWithoutLineupFlagsSendsNoOverrides(t *testing.T) {
	calls, _ := overrideRun(t)
	raw, _ := json.Marshal(calls[0])
	if strings.Contains(string(raw), `"lineup"`) || strings.Contains(string(raw), `"budgets"`) {
		t.Fatal(string(raw))
	}
}
func TestRunLineupFlagUsageErrors(t *testing.T) {
	for _, flags := range [][]string{{"--implementer", "codex:sol"}, {"--reviewer", "claude::high"}, {"--implementer", ":sol:high"}, {"--reviewer", "claude:opus:"}, {"--architect", "off", "--architect", "codex:sol:high"}, {"--architect", "codex:sol:high", "--architect", "off"}, {"--budget-minutes", "-1"}, {"--budget-tokens", "-1"}} {
		for _, base := range [][]string{{"run", "p", "S1"}, {"wave", "start", "p", "S1", "S2"}} {
			app := &App{Call: func(context.Context, string, any, any) error { t.Fatal("daemon called for invalid flags"); return nil }}
			args := append(append([]string{}, base...), flags...)
			code, _, errs := run(t, app, args...)
			if code != ExitUsage || errs == "" || strings.Contains(errs, "flag provided but not defined") {
				t.Fatalf("%v: exit %d %s", args, code, errs)
			}
		}
	}
}

func TestLineupVendorIsCaseInsensitive(t *testing.T) {
	for _, tc := range []struct{ typed, vendor, model, effort string }{
		{"Claude:sonnet:high", "claude", "sonnet", "high"},
		{"CLAUDE:sonnet:high", "claude", "sonnet", "high"},
		{"Codex:gpt-5:high", "codex", "gpt-5", "high"},
		{"GROK:latest:high", "grok", "latest", "high"},
	} {
		for _, role := range []string{"implementer", "reviewer", "architect"} {
			for _, wave := range []bool{false, true} {
				name := fmt.Sprint(tc.typed, "/", role, "/wave=", wave)
				t.Run(name, func(t *testing.T) {
					var sent []daemon.RunParams
					app := &App{Call: func(_ context.Context, method string, params, result any) error {
						switch method {
						case daemon.MethodSpecCheck:
							return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true},{"item":"S2","ready":true}]}`), result)
						case daemon.MethodRunPlan:
							p := params.(daemon.RunParams)
							sent = append(sent, p)
							*result.(*daemon.RunPlan) = daemon.RunPlan{CheckCommand: "go test ./...", Item: p.Item, Lineup: p.Lineup}
						case daemon.MethodRunStart:
							sent = append(sent, params.(daemon.RunParams))
						case daemon.MethodWaveStart:
							sent = append(sent, params.(daemon.WaveStartParams).Items...)
						default:
							t.Fatalf("unexpected %s", method)
						}
						return nil
					}}
					args := []string{"run", "p", "S1"}
					want := 2 // run.plan and run.start
					if wave {
						args, want = []string{"wave", "start", "p", "S1", "S2"}, 4 // a plan and an item of the start for each
					}
					args = append(args, "--yes", "--well-specified", "y", "--split", "n", "--"+role, tc.typed)
					if code, out, errs := run(t, app, args...); code != 0 || len(sent) != want {
						t.Fatalf("exit %d, %d calls, want %d: %s %s", code, len(sent), want, out, errs)
					}
					entry := runrec.LineupEntry{Vendor: tc.vendor, Model: tc.model, Effort: tc.effort}
					for _, p := range sent {
						if got := p.Lineup[role]; !reflect.DeepEqual(got, []runrec.LineupEntry{entry}) {
							t.Errorf("%s sent %+v, want %+v", role, got, entry)
						}
					}
				})
			}
		}
	}
}

func TestUnknownLineupVendorListsTheVendors(t *testing.T) {
	for _, base := range [][]string{{"run", "p", "S1"}, {"wave", "start", "p", "S1", "S2"}} {
		name := "run"
		if base[0] == "wave" {
			name = "wave start"
		}
		for _, role := range []string{"implementer", "reviewer", "architect"} {
			app := &App{Call: func(context.Context, string, any, any) error {
				t.Fatal("an unknown vendor called the daemon")
				return nil
			}}
			code, _, errs := run(t, app, append(append([]string{}, base...), "--"+role, "acme:m:high")...)
			want := "ork: " + name + ": --" + role + ": unsupported vendor \"acme\"; use " + strings.Join(daemon.Vendors(), ", ") + "\n"
			if code != ExitUsage || !strings.HasPrefix(errs, want) {
				t.Errorf("%v --%s: exit %d errors %q want %q", base, role, code, errs, want)
			}
		}
	}
}

func TestLaunchUsageFitsEightyColumns(t *testing.T) {
	for _, line := range strings.Split(usageText, "\n") {
		if len(line) > 80 {
			t.Errorf("usage line has %d columns: %s", len(line), line)
		}
	}
}

func TestLineupErrorsNameCommandAndFlags(t *testing.T) {
	for _, base := range [][]string{{"run", "p", "S1"}, {"wave", "start", "p", "S1", "S2"}} {
		name := "run"
		if base[0] == "wave" {
			name = "wave start"
		}
		for _, tc := range []struct {
			flags   []string
			message string
		}{
			{[]string{"--implementer", "bad"}, "--implementer requires VENDOR:MODEL:EFFORT"},
			{[]string{"--architect", "bad"}, "--architect requires VENDOR:MODEL:EFFORT or off"},
			{[]string{"--architect", "off", "--architect", "codex:sol:high"}, "--architect off cannot be combined with an entry"},
			{[]string{"--budget-minutes", "-1"}, "--budget-minutes and --budget-tokens must be zero or more"},
			{[]string{"--budget-tokens", "-1"}, "--budget-minutes and --budget-tokens must be zero or more"},
		} {
			app := &App{Call: func(context.Context, string, any, any) error { t.Fatal("invalid flags called daemon"); return nil }}
			code, _, errs := run(t, app, append(append([]string{}, base...), tc.flags...)...)
			want := "ork: " + name + ": " + tc.message + "\n"
			if code != ExitUsage || !strings.HasPrefix(errs, want) {
				t.Errorf("%v: exit %d errors %q want prefix %q", tc.flags, code, errs, want)
			}
		}
	}
}

func TestRunAndWavePlanShowOnlyOverriddenBudgets(t *testing.T) {
	for _, wave := range []bool{false, true} {
		for _, flags := range [][]string{nil, {"--budget-minutes", "20"}, {"--budget-minutes", "0", "--budget-tokens", "0"}} {
			t.Run(fmt.Sprint(wave, flags), func(t *testing.T) {
				effective := registry.Budgets{Minutes: 45, Tokens: 9000}
				if len(flags) == 2 {
					effective.Minutes = 20
				}
				var out, errs bytes.Buffer
				app := &App{Input: strings.NewReader("n\n"), Call: func(_ context.Context, method string, params, result any) error {
					switch method {
					case daemon.MethodRunPlan:
						*result.(*daemon.RunPlan) = daemon.RunPlan{CheckCommand: "go test ./...", Item: "S1", Budgets: effective}
					case "spec.check":
						return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true}]}`), result)
					default:
						t.Fatalf("unexpected %s", method)
					}
					return nil
				}}
				base := []string{"run", "p", "S1"}
				prompt := "Start this slice?"
				if wave {
					base = []string{"wave", "start", "p", "S1"}
					prompt = "Start this wave?"
				}
				code := app.Run(context.Background(), append(base, flags...), &out, &errs)
				text := out.String()
				row := fmt.Sprintf("budgets: %d min, %d tokens", effective.Minutes, effective.Tokens)
				if code != ExitOK {
					t.Fatal(code, errs.String())
				}
				if len(flags) == 0 {
					if strings.Contains(text, "budgets:") {
						t.Fatal(text)
					}
				} else {
					if !strings.Contains(text, row) || strings.Index(text, row) > strings.Index(text, prompt) {
						t.Fatalf("budget row absent or after confirmation: %s", text)
					}
				}
			})
		}
	}
}

// singleSpecRun reuses the wave sitting's recorded replies without a daemon.
func singleSpecRun(t *testing.T, input, check string, checkErr error, extra ...string) (int, string, string, *specStub) {
	t.Helper()
	stub := &specStub{t: t, check: check}
	var out, errs bytes.Buffer
	app := &App{Input: strings.NewReader(input), Call: func(ctx context.Context, method string, params, result any) error {
		if method == daemon.MethodSpecCheck {
			if !strings.Contains(out.String()+errs.String(), "/p/S1") || strings.Contains(out.String()+errs.String(), "Start this slice?") {
				t.Fatal("spec check must follow plan and precede confirmation")
			}
			if !strings.Contains(out.String()+errs.String(), "spec check: S1; wait for it to finish\n") {
				t.Error("the wait line must come before the spec.check call")
			}
			p := params.(daemon.SpecCheckParams)
			if p.Project != "p" || !reflect.DeepEqual(p.Items, []string{"S1"}) {
				t.Fatalf("check params: %+v", p)
			}
			if checkErr != nil {
				stub.calls = append(stub.calls, method)
				return checkErr
			}
		}
		if method == daemon.MethodRunStart {
			stub.calls = append(stub.calls, method)
			stub.started = append(stub.started, params.(daemon.RunParams).Item)
			if !strings.Contains(out.String()+errs.String(), "Start this slice?") && !containsYes(extra) {
				t.Fatal("start before confirmation")
			}
			return json.Unmarshal([]byte(`{"id":"r1","state":"setup"}`), result)
		}
		return stub.call(ctx, method, params, result)
	}}
	args := append([]string{"run", "p", "S1", "--well-specified", "y", "--split", "n"}, extra...)
	code := app.Run(context.Background(), args, &out, &errs)
	return code, out.String(), errs.String(), stub
}

func containsYes(args []string) bool {
	for _, arg := range args {
		if arg == "--yes" {
			return true
		}
	}
	return false
}

func TestRunCallsTheSpecCheckAfterThePlanAndBeforeConfirmation(t *testing.T) {
	for _, yes := range []bool{false, true} {
		flags := []string{}
		if yes {
			flags = append(flags, "--yes")
		}
		code, out, errs, stub := singleSpecRun(t, "y\n", `{"items":[{"item":"S1","ready":true}]}`, nil, flags...)
		if code != ExitOK || !reflect.DeepEqual(stub.calls, []string{"run.plan", "spec.check", "run.start"}) {
			t.Fatalf("exit %d calls %v: %s %s", code, stub.calls, out, errs)
		}
	}
}

func TestRunSaysToWaitForTheSpecCheck(t *testing.T) {
	const line = "spec check: S1; wait for it to finish"
	code, out, errs, _ := singleSpecRun(t, "y\n", `{"items":[{"item":"S1","ready":true}]}`, nil)
	if code != ExitOK || strings.Count(out, line+"\n") != 1 || strings.Index(out, line) > strings.Index(out, "Start this slice?") {
		t.Fatalf("exit %d, wait line missing or after the confirmation: %s %s", code, out, errs)
	}
	code, out, errs, _ = singleSpecRun(t, "", `{"items":[{"item":"S1","ready":true}]}`, nil, "--json", "--yes")
	var value map[string]any
	if err := json.Unmarshal([]byte(out), &value); err != nil || code != ExitOK {
		t.Fatalf("exit %d, stdout is not one JSON value: %s %s %v", code, out, errs, err)
	}
	if !strings.Contains(errs, line+"\n") || strings.Contains(out, "spec check:") {
		t.Fatalf("wait line must be on stderr with --json: %s %s", out, errs)
	}
}

func TestRunAsksSpecQuestionsOnStdinThenStarts(t *testing.T) {
	code, out, errs, stub := singleSpecRun(t, "2\ncustom flag\ny\n", specCheckTwoQuestions, nil)
	want := []string{"run.plan", "spec.check", "projects.list", "spec.answer", "run.start"}
	if code != ExitOK || !reflect.DeepEqual(stub.calls, want) || len(stub.answers) != 1 || !reflect.DeepEqual(stub.answers[0].Answers, []string{"memory: rebuilt at each start", "custom flag"}) {
		t.Fatalf("exit %d calls %v answers %+v: %s %s", code, stub.calls, stub.answers, out, errs)
	}
	if !strings.Contains(out, "[S1 1/2] Which store?") {
		t.Fatal(out)
	}
}

func TestRunHoldsAnItemWhoseQuestionsStayOpen(t *testing.T) {
	for _, input := range []string{"\n", ""} {
		code, out, errs, stub := singleSpecRun(t, input, specCheckTwoQuestions, nil, "--yes")
		if code != ExitFailure || len(stub.started) != 0 || !strings.Contains(out, "held: S1 has 2 open spec questions") {
			t.Fatalf("exit %d started %v: %s %s", code, stub.started, out, errs)
		}
	}
}

func TestRunSpecCheckErrorStopsBeforeStart(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		flags := []string{"--yes"}
		if jsonMode {
			flags = append(flags, "--json")
		}
		code, out, errs, stub := singleSpecRun(t, "", "", &ipc.Error{Code: "spec_failed", Message: "no Spec check row"}, flags...)
		if code != ExitFailure || len(stub.started) != 0 || !strings.Contains(out+errs, "no Spec check row") {
			t.Fatalf("exit %d calls %v: %s %s", code, stub.calls, out, errs)
		}
		if jsonMode {
			var result struct{ Error ipc.Error }
			if err := json.Unmarshal([]byte(out), &result); err != nil || result.Error.Code != "spec_failed" {
				t.Fatalf("error result: %s %v", out, err)
			}
		}
	}
}

func TestRunJSONKeepsTheSittingOffStdout(t *testing.T) {
	for _, held := range []bool{false, true} {
		input := "2\ncustom flag\n"
		if held {
			input = ""
		}
		code, out, errs, stub := singleSpecRun(t, input, specCheckTwoQuestions, nil, "--yes", "--json")
		var value map[string]any
		if err := json.Unmarshal([]byte(out), &value); err != nil {
			t.Fatalf("stdout is not one JSON value: %s %v", out, err)
		}
		if !strings.Contains(errs, "[S1 1/2] Which store?") || strings.Contains(out, "Which store?") {
			t.Fatalf("sitting: %s %s", out, errs)
		}
		if held {
			if code != ExitFailure || value["started"] != false || !reflect.DeepEqual(value["held"], []any{"S1"}) || len(stub.started) != 0 {
				t.Fatalf("held exit %d: %s", code, out)
			}
		} else if code != ExitOK || value["id"] != "r1" {
			t.Fatalf("start exit %d: %s", code, out)
		}
	}
}

// S283: the shared spec check leaves an item with no spec out of `ork run` too:
// the hint, no start, exit 1, and the JSON result is the held shape as before.
func TestRunLeavesOutAnItemWithNoSpec(t *testing.T) {
	const check = `{"items":[{"item":"S1","ready":false,"no_spec":true}]}`
	code, out, errs, stub := singleSpecRun(t, "", check, nil, "--yes")
	if code != ExitFailure || len(stub.started) != 0 || !strings.Contains(out+errs, "S1: no slice spec; run slicer promote S1\n") {
		t.Fatalf("text: exit %d: %s %s", code, out, errs)
	}
	code, out, errs, stub = singleSpecRun(t, "", check, nil, "--yes", "--json")
	var value map[string]any
	if json.Unmarshal([]byte(out), &value) != nil || code != ExitFailure || len(stub.started) != 0 || value["started"] != false || !reflect.DeepEqual(value["held"], []any{}) {
		t.Fatalf("json: exit %d: %s %s", code, out, errs)
	}
	if !strings.Contains(errs, "S1: no slice spec; run slicer promote S1") {
		t.Errorf("the hint goes to stderr with --json: %s", errs)
	}
}

func TestRunRefusesBlockedItemBeforeConfirmation(t *testing.T) {
	message := daemon.BlockedMessage([]string{"S2"}, []slicercli.ItemDeps{{ID: "S2", BlockedBy: []string{"S1"}}}, map[string]string{"S1": "Title"})
	const wantMessage = `S2 waits on S1 "Title" (not done); land S1 first`
	if message != wantMessage {
		t.Fatalf("blocked message: %q", message)
	}
	for _, jsonMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("json=%t", jsonMode), func(t *testing.T) {
			var out, errs bytes.Buffer
			var calls []string
			app := &App{Input: strings.NewReader("y\n"), Call: func(_ context.Context, method string, params, result any) error {
				calls = append(calls, method)
				switch method {
				case daemon.MethodRunPlan:
					*result.(*daemon.RunPlan) = daemon.RunPlan{CheckCommand: "go test ./...", Item: "S2"}
					return nil
				case daemon.MethodSpecCheck:
					p := params.(daemon.SpecCheckParams)
					if p.Project != "p" || !reflect.DeepEqual(p.Items, []string{"S2"}) {
						t.Fatalf("check params: %+v", p)
					}
					return &ipc.Error{Code: daemon.CodeRefused, Message: message}
				default:
					t.Fatalf("blocked item called %s", method)
					return nil
				}
			}}
			args := []string{"run", "p", "S2", "--well-specified", "y", "--split", "n"}
			if jsonMode {
				args = append(args, "--json")
			}
			code := app.Run(context.Background(), args, &out, &errs)
			if code != ExitFailure || !reflect.DeepEqual(calls, []string{daemon.MethodRunPlan, daemon.MethodSpecCheck}) {
				t.Fatalf("exit %d calls %v: %s %s", code, calls, &out, &errs)
			}
			if strings.Contains(out.String()+errs.String(), "Start this slice?") {
				t.Fatal("blocked item reached confirmation")
			}
			if jsonMode {
				var result struct{ Error ipc.Error }
				if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Error.Code != daemon.CodeRefused || result.Error.Message != wantMessage {
					t.Fatalf("refusal: %s (%v)", &out, err)
				}
			} else if errs.String() != "ork: "+wantMessage+"\n" {
				t.Fatalf("refusal: %q", errs.String())
			}
		})
	}
}

func TestRunStopAfterFlag(t *testing.T) {
	calls, printed := overrideRun(t, "--stop-after", "commit")
	if !strings.Contains(printed, "stop: after commit (you land it)") {
		t.Fatal(printed)
	}
	for _, p := range calls {
		if p.StopAfter != "commit" {
			t.Fatalf("params: %+v", p)
		}
	}
	var out bytes.Buffer
	printRunPlan(&out, daemon.RunPlan{StopAfter: "commit"}, false)
	if !strings.Contains(out.String(), "stop: after commit (you land it)") {
		t.Fatal(out.String())
	}
	r := runPrompted(t, "", "--stop-after", "merge")
	if r.code != ExitUsage || r.plan != "" {
		t.Fatalf("invalid flag: %+v", r)
	}
}

func TestStopAfterInRunAndWaveHelp(t *testing.T) {
	_, out, _ := run(t, &App{}, "help")
	if strings.Count(out, "[--stop-after land|commit]") != 2 {
		t.Fatal(out)
	}
}

func TestRunPlanSaysAfterLanding(t *testing.T) {
	var out bytes.Buffer
	printRunPlan(&out, daemon.RunPlan{StopAfter: "land"}, false)
	if !strings.Contains(out.String(), "stop: after landing\n") {
		t.Fatal(out.String())
	}
}

func TestRunDeclineJSONHasAnEmptyHeldArray(t *testing.T) {
	code, out, errs, stub := singleSpecRun(t, "n", `{"items":[{"item":"S1","ready":true}]}`, nil, "--json")
	var value map[string]any
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		t.Fatal(err)
	}
	if code != 0 || len(stub.started) != 0 || value["started"] != false || !reflect.DeepEqual(value["held"], []any{}) {
		t.Fatalf("code %d out %s err %s", code, out, errs)
	}
}

func TestRunHeldLineStartsOnItsOwnLineAtEOF(t *testing.T) {
	check := `{"items":[{"item":"S1","questions":[{"text":"Question?"}]}]}`
	code, out, errs, _ := singleSpecRun(t, "", check, nil, "--yes")
	if code != 1 || !strings.Contains(out, "): \nheld: S1 has 1 open spec question") {
		t.Fatalf("code %d out %q err %s", code, out, errs)
	}
}

func TestPrintRunPlanShowsWarnings(t *testing.T) {
	plan := daemon.RunPlan{Project: registry.Project{Name: "p"}, Item: "S1", Lineup: map[string][]runrec.LineupEntry{"implementer": {{Vendor: "codex", Model: "sol", Effort: "high"}}},
		Permissions: map[string]engine.Permission{"implementer": {Allow: []string{"Read"}}, "reviewer": {ReadOnly: true}, "gate": {ReadOnly: true}}, Note: "a note", Warnings: []string{"first warning", "second warning"}}
	var out bytes.Buffer
	printRunPlan(&out, plan, false)
	got := out.String()
	if !strings.Contains(got, "\ncheck: none\n") {
		t.Errorf("an empty check command is not shown as none: %q", got)
	}
	if !strings.HasSuffix(got, "implementer: codex sol high\n  allowlist: Read\n  permissions: read-only\n  permissions: read-only\nwarning: first warning\nwarning: second warning\nnote: a note\n") {
		t.Errorf("warnings do not follow the role rows and precede the note: %q", got)
	}
	plan.Warnings, plan.CheckCommand = nil, "go test ./..."
	out.Reset()
	printRunPlan(&out, plan, false)
	if got := out.String(); strings.Contains(got, "warning:") || !strings.Contains(got, "\ncheck: go test ./...\n") {
		t.Errorf("a plan without warnings: %q", got)
	}
}

// noCheckRun is `ork run p S1` or `ork wave start p S1` against a daemon whose
// project has no check command: it records which methods were called.
func noCheckRun(t *testing.T, args ...string) (code int, out, errs string, methods []string, starts []daemon.RunParams) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := &App{Input: strings.NewReader("yes\n"), Call: func(_ context.Context, method string, params, result any) error {
		methods = append(methods, method)
		switch method {
		case daemon.MethodRunPlan:
			return json.Unmarshal([]byte(`{"project":{"name":"p"},"item":"S1","warnings":["no check command: ork refuses to start a run (fix: ork projects set p --check CMD, or start with --allow-no-check)"]}`), result)
		case daemon.MethodSpecCheck:
			return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true}]}`), result)
		case daemon.MethodRunStart:
			starts = append(starts, params.(daemon.RunParams))
			return json.Unmarshal([]byte(`{"id":"r1","state":"setup"}`), result)
		case daemon.MethodWaveStart:
			starts = append(starts, params.(daemon.WaveStartParams).Items...)
			return json.Unmarshal([]byte(`{"wave":"wave-1","items":[{"item":"S1","run":{"id":"r1","state":"setup"}}]}`), result)
		}
		t.Fatalf("method %s", method)
		return nil
	}}
	code = app.Run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String(), methods, starts
}

// The plan names the round check command under the check command when the
// project has one (S285), and shows no line when it does not.
func TestPlanShowsTheRoundCheckCommand(t *testing.T) {
	for _, tc := range []struct {
		round string
		want  string
	}{{"make fast", "check: make test\nround check: make fast\nstop:"}, {"", "check: make test\nstop:"}} {
		var b bytes.Buffer
		printRunPlan(&b, daemon.RunPlan{Project: registry.Project{Name: "p"}, Item: "S1", CheckCommand: "make test", RoundCheckCommand: tc.round}, false)
		if !strings.Contains(b.String(), tc.want) {
			t.Errorf("round %q: plan\n%s\nwant it to contain %q", tc.round, b.String(), tc.want)
		}
	}
}

func TestRunRefusesAPlanWithNoCheckCommand(t *testing.T) {
	for name, args := range map[string][]string{
		"run":  {"run", "p", "S1", "--yes", "--well-specified", "y", "--split", "n"},
		"wave": {"wave", "start", "p", "S1", "--yes", "--well-specified", "y", "--split", "n"},
	} {
		t.Run(name, func(t *testing.T) {
			code, out, errs, methods, starts := noCheckRun(t, args...)
			if code != ExitFailure || len(starts) != 0 {
				t.Fatalf("exit %d, starts %+v: %s %s", code, starts, out, errs)
			}
			// The plan is shown first, then the refusal names the fix.
			if !strings.Contains(out, "check: none\n") || strings.Contains(out, "no check command") {
				t.Errorf("the plan must show check: none, and leave the refusal text to the error:\n%s", out)
			}
			if want := "ork: project p: no check command: ork refuses to start a run (fix: ork projects set p --check CMD, or start with --allow-no-check)\n"; errs != want {
				t.Errorf("stderr %q, want %q", errs, want)
			}
			if slices.Contains(methods, daemon.MethodSpecCheck) {
				t.Errorf("the refusal came after the spec check: %v", methods)
			}

			code, out, errs, _, starts = noCheckRun(t, append(args, "--allow-no-check")...)
			if code != ExitOK || len(starts) != 1 || !starts[len(starts)-1].AllowNoCheck {
				t.Fatalf("--allow-no-check: exit %d, starts %+v: %s", code, starts, errs)
			}
			// With the flag the plan warns in plain words that do not say ork refuses.
			if !strings.Contains(out, "\nwarning: no check command: only slicer check runs after landing\n") || strings.Contains(out, "refuses") {
				t.Errorf("--allow-no-check plan text:\n%s", out)
			}
			// The confirmed plan the daemon compares is the one it sent, unchanged.
			if exp := starts[len(starts)-1].Expected; exp == nil || len(exp.Warnings) != 1 || !strings.Contains(exp.Warnings[0], "refuses") {
				t.Errorf("expected plan was rewritten: %+v", exp)
			}
		})
	}
	// --json prints the refusal as the command's error value.
	code, out, _, _, _ := noCheckRun(t, "run", "p", "S1", "--json", "--yes", "--well-specified", "y", "--split", "n")
	var e struct {
		Error struct{ Code, Message string }
	}
	mustJSON(t, out, &e)
	if code != ExitFailure || e.Error.Code != daemon.CodeRefused || !strings.Contains(e.Error.Message, "--allow-no-check") {
		t.Fatalf("--json: exit %d: %s", code, out)
	}
}
