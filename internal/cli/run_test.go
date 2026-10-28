// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
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
			return json.Unmarshal([]byte(`{"project":{"name":"p"},"item":"S1"}`), result)
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

func TestRunLineupPromptsTakeYAndN(t *testing.T) {
	for _, tc := range []struct {
		answer string
		yes    bool
	}{{"y", true}, {"Y", true}, {"yes", true}, {"YES", true}, {"n", false}, {"N", false}, {"no", false}, {"No", false}} {
		t.Run(tc.answer, func(t *testing.T) {
			r := runPrompted(t, tc.answer+"\n"+tc.answer+"\nyes\n")
			want := fmt.Sprintf(`{"project":"p","item":"S1","well_specified":%t,"split":%t}`, tc.yes, tc.yes)
			if r.code != ExitOK || r.plan != want || !r.started {
				t.Fatalf("code %d plan %s started %t: %s %s", r.code, r.plan, r.started, r.out, r.errs)
			}
		})
	}
}

func TestRunConfirmTakesYAndN(t *testing.T) {
	for _, tc := range []struct {
		answer string
		start  bool
	}{{"y", true}, {"Y", true}, {"yes", true}, {"YES", true}, {"n", false}, {"N", false}, {"no", false}, {"No", false}} {
		t.Run(tc.answer, func(t *testing.T) {
			r := runPrompted(t, "yes\nno\n"+tc.answer+"\n")
			if r.code != ExitOK || r.started != tc.start {
				t.Fatalf("code %d started %t: %s %s", r.code, r.started, r.out, r.errs)
			}
		})
	}
}

func TestRunConfirmEnterTakesNo(t *testing.T) {
	r := runPrompted(t, "yes\nno\n\n")
	if r.code != ExitOK || r.started || !strings.Contains(r.out, "cancelled") {
		t.Fatalf("code %d started %t: %s %s", r.code, r.started, r.out, r.errs)
	}
}

func TestRunConfirmPromptShowsDefaultNo(t *testing.T) {
	r := runPrompted(t, "yes\nno\nn\n")
	if !strings.Contains(r.out, "Start this slice? [y/N]: ") {
		t.Fatalf("confirm hint: %s", r.out)
	}
}

func TestRunWellSpecifiedPromptShowsYN(t *testing.T) {
	r := runPrompted(t, "\n")
	if !strings.Contains(r.out, "Well specified? [y/n]: ") {
		t.Fatalf("well-specified hint: %s", r.out)
	}
}

func TestRunSplitPromptShowsYN(t *testing.T) {
	r := runPrompted(t, "yes\n\n")
	if !strings.Contains(r.out, "Split? [y/n]: ") {
		t.Fatalf("split hint: %s", r.out)
	}
}

func TestRunLineupPromptsHaveNoDefault(t *testing.T) {
	for _, input := range []string{"\nyes\n", "yes\n\n"} {
		r := runPrompted(t, input)
		if r.code != ExitUsage || r.plan != "" || r.started {
			t.Fatalf("input %q: code %d plan %s: %s", input, r.code, r.plan, r.errs)
		}
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
	r := runPrompted(t, "maybe\nyes\n")
	if r.code != ExitUsage || r.plan != "" {
		t.Fatalf("lineup: code %d plan %s: %s", r.code, r.plan, r.errs)
	}
	r = runPrompted(t, "yes\nno\nsure\n")
	if r.code != ExitUsage || r.started {
		t.Fatalf("confirm: code %d started %t: %s", r.code, r.started, r.errs)
	}
}

func TestRunConfirmClosedInputIsNotEnter(t *testing.T) {
	r := runPrompted(t, "yes\nno\n")
	if r.code != ExitUsage || r.started || strings.Contains(r.out, "cancelled") {
		t.Fatalf("code %d started %t: %s %s", r.code, r.started, r.out, r.errs)
	}
}

func TestRunYesRequiresExplicitLineupInputs(t *testing.T) {
	for _, flags := range [][]string{nil, {"--well-specified", "yes"}, {"--split", "no"}, {"--well-specified", "maybe", "--split", "no"}} {
		var out, errs bytes.Buffer
		code := (&App{}).Run(context.Background(), append([]string{"run", "p", "S1", "--yes"}, flags...), &out, &errs)
		if code != ExitUsage || !strings.Contains(errs.String(), "well-specified") || strings.Contains(errs.String(), "unknown command") {
			t.Fatalf("code %d: %s", code, &errs)
		}
	}
}

func TestRunShowsPlanBeforeConfirmation(t *testing.T) {
	for _, answer := range []string{"no", "yes"} {
		t.Run(answer, func(t *testing.T) {
			var out, errs bytes.Buffer
			app := &App{Input: strings.NewReader("yes\nno\n" + answer + "\n")}
			app.Call = func(_ context.Context, method string, params, result any) error {
				switch method {
				case "run.plan":
					raw, _ := json.Marshal(params)
					if string(raw) != `{"project":"p","item":"S1","well_specified":true,"split":false}` {
						t.Fatalf("inputs %s", raw)
					}
					b := []byte(`{"project":{"name":"p"},"item":"S1","worktree":"/p/.worktrees/S1","branch":"S1","check_command":"go test ./...","lineup":{"implementer":[{"vendor":"claude","model":"sonnet","effort":"high"}]},"permissions":{"implementer":{"ReadOnly":false,"Allow":["Read","Write"]}}}`)
					return json.Unmarshal(b, result)
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
					return json.Unmarshal([]byte(`{"project":{"name":"p"},"item":"S1"}`), result)
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
	if code != 0 || !strings.Contains(out.String(), "r1 S1 paused (wait: Which library?) (denied: Write, Bash)\n    error: background broke\n") {
		t.Fatalf("status code %d: %s %s", code, &out, &errs)
	}
}

func TestStatusSectionsAndNeedsRows(t *testing.T) {
	const reply = `{"projects":[{"name":"p","needs_you":5,"open":[{"id":125,"key":"ui.status-list","title":"Status list"}],"held":[{"id":"S32","title":"Owner answer"}],"active_relays":[{"id":"ledger","items":["S1"],"state":"active"}],"engine_runs":[{"id":"old","item":"S1","state":"stopped","created_at":"2026-10-23T12:00:00Z"},{"id":"current","item":"S1","state":"implement","created_at":"2026-10-23T12:01:00Z"},{"id":"done","item":"S2","state":"committed"},{"id":"approval","item":"S3","state":"awaiting-approval"},{"id":"paused","item":"S4","state":"paused","wait":"Question?","denials":["Write"]},{"id":"stopped","item":"S5","state":"stopped","updated_at":"2026-10-23T12:00:00Z"}]}]}`
	app := &App{Call: func(_ context.Context, _ string, _, result any) error { return json.Unmarshal([]byte(reply), result) }}
	code, stdout, stderr := run(t, app, "status")
	want := "p: needs you 5, active relays 1\n  #125 ui.status-list  Status list\n  S32  Owner answer  (needs-owner)\n  ledger S1 active (0 steps, 0 running)\n  approval S3 awaiting-approval\n  paused S4 paused (wait: Question?) (denied: Write)\n  stopped S5 stopped\n  current S1 implement\n"
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
				app, _, _ := doctorApp(t, "Contract: relay v7")
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
			app, _, _ := doctorApp(t, "Contract: relay v7")
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
		printRunPlan(&out, plan)
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
		printRunPlan(&out, plan)
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
		printRunPlan(&out, plan)
		got := out.String()
		if strings.Contains(got, "note:") != present {
			t.Fatalf("present=%v: %s", present, got)
		}
		if present && !strings.HasSuffix(got, "\nnote: "+note+"\n") {
			t.Errorf("the note does not follow the plan lines: %q", got)
		}
	}
}
