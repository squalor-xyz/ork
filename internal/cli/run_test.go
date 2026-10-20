// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
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
		return json.Unmarshal([]byte(`{"projects":[{"name":"p","needs_you":1,"engine_runs":[{"id":"r1","item":"S1","state":"paused","wait":"Which library?"}]}]}`), result)
	}}
	code := app.Run(context.Background(), []string{"status"}, &out, &errs)
	if code != 0 || !strings.Contains(out.String(), "r1 S1 paused (wait: Which library?)") {
		t.Fatalf("status code %d: %s %s", code, &out, &errs)
	}
}
