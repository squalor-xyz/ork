// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
)

func waveApp(t *testing.T, failure bool) (*App, *clitest.Fake) {
	t.Helper()
	file, exit := "plan-parallel.json", 0
	if failure {
		file, exit = "plan-parallel-error.json", 2
	}
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"plan-parallel", "--json", "--lean", "--root", "/project/p"}, File: filepath.Join("..", "..", "testdata", "cli", "slicer", file), Exit: exit})
	app := &App{Tools: Tools{Slicer: fake.Bin("slicer")}, Call: func(_ context.Context, method string, params, result any) error {
		if method != "projects.list" {
			t.Fatalf("unexpected method %s", method)
		}
		return json.Unmarshal([]byte(`{"projects":[{"name":"p","path":"/project/p"}]}`), result)
	}}
	return app, fake
}

func TestWaveSuggestPrintsPlanParallelWaves(t *testing.T) {
	app, fake := waveApp(t, false)
	var out, errs bytes.Buffer
	code := app.Run(context.Background(), []string{"wave", "suggest", "p"}, &out, &errs)
	if code != ExitOK {
		t.Fatalf("code %d: %s %s", code, &out, &errs)
	}
	last := -1
	for _, want := range []string{"Wave 1", "S55 Run tree", "S38 projects rm", "Wave 2", "S60 ork wave", "shares: S55 docs/dashboard.md", "Wave 14", "S28 TUI answers"} {
		idx := strings.Index(out.String(), want)
		if idx <= last {
			t.Fatalf("missing or out of order %q: %s", want, &out)
		}
		last = idx
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestWaveSuggestShowsFilesSectionErrorVerbatim(t *testing.T) {
	app, _ := waveApp(t, true)
	var out, errs bytes.Buffer
	code := app.Run(context.Background(), []string{"wave", "suggest", "p", "--json"}, &out, &errs)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cli", "slicer", "plan-parallel-error.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want, got struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if code != ExitFailure || got != want {
		t.Fatalf("code %d: %s %s; want %s", code, &out, &errs, fixture)
	}
}

func TestWaveSuggestJSONAndUnknownProject(t *testing.T) {
	app, fake := waveApp(t, false)
	var out, errs bytes.Buffer
	if code := app.Run(context.Background(), []string{"wave", "suggest", "missing", "--json"}, &out, &errs); code != ExitFailure || !strings.Contains(out.String(), `"not_found"`) || len(fake.Calls()) != 0 {
		t.Fatalf("unknown project: %d %s %s", code, &out, &errs)
	}
	out.Reset()
	if code := app.Run(context.Background(), []string{"wave", "suggest", "p", "--json"}, &out, &errs); code != ExitOK {
		t.Fatalf("code %d: %s %s", code, &out, &errs)
	}
	var plan struct {
		Width int
		Waves []json.RawMessage
		Held  []json.RawMessage
	}
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil || plan.Width != 4 || len(plan.Waves) != 14 || len(plan.Held) != 5 {
		t.Fatalf("JSON: %s (%v)", &out, err)
	}
}

func TestWaveStartPlansChosenItemsBeforeConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		flags       []string
		start       bool
		code        int
	}{
		{"confirm", "y\nn\nn\ny\ny\n", nil, true, ExitOK},
		{"decline", "y\nn\nn\ny\n\n", nil, false, ExitOK},
		{"closed", "y\nn\nn\ny\n", nil, false, ExitUsage},
		{"yes", "", []string{"--yes", "--well-specified", "yes", "--split", "no"}, true, ExitOK},
		{"missing inputs", "", []string{"--yes"}, false, ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errs bytes.Buffer
			plans, started, checked := 0, false, false
			app := &App{Input: strings.NewReader(tc.input)}
			app.Call = func(_ context.Context, method string, params, result any) error {
				switch method {
				case "run.plan":
					p := params.(daemon.RunParams)
					want := []string{"S2", "S1"}[plans]
					if p.Project != "p" || p.Item != want || p.Expected != nil {
						t.Fatalf("plan inputs: %+v", p)
					}
					well := plans == 0 || tc.name == "yes"
					split := plans == 1 && tc.name != "yes"
					if p.WellSpecified != well || p.Split != split {
						t.Fatalf("answers: %+v", p)
					}
					plans++
					*(result.(*daemon.RunPlan)) = daemon.RunPlan{Item: p.Item, Worktree: "/p/" + p.Item}
				case "spec.check":
					p := params.(daemon.SpecCheckParams)
					if plans != 2 || p.Project != "p" || !reflect.DeepEqual(p.Items, []string{"S2", "S1"}) {
						t.Fatalf("check before both plans were shown: %+v", p)
					}
					checked = true
					return json.Unmarshal([]byte(`{"items":[{"item":"S2","ready":true},{"item":"S1","ready":true}]}`), result)
				case "wave.start":
					p := params.(daemon.WaveStartParams)
					if plans != 2 || len(p.Items) != 2 || !strings.Contains(errs.String(), "/p/S2") || !strings.Contains(errs.String(), "/p/S1") {
						t.Fatalf("start before plans shown: %+v %s", p, &errs)
					}
					for _, item := range p.Items {
						if item.Expected == nil || item.Expected.Item != item.Item {
							t.Fatalf("expected plan: %+v", item)
						}
					}
					started = true
					return json.Unmarshal([]byte(`{"wave":"wave-123","items":[{"item":"S2","run":{"id":"r2","state":"setup"}},{"item":"S1","run":{"id":"r1","state":"queued"}}]}`), result)
				default:
					t.Fatalf("method: %s", method)
				}
				return nil
			}
			args := append([]string{"wave", "start", "p", "S2", "S1", "--json"}, tc.flags...)
			code := app.Run(context.Background(), args, &out, &errs)
			if checked != (tc.name != "missing inputs") {
				t.Fatalf("spec check ran: %t", checked)
			}
			if code != tc.code || started != tc.start || !json.Valid(out.Bytes()) {
				t.Fatalf("code %d started %t: %s %s", code, started, &out, &errs)
			}
		})
	}
}

func TestStopWavePrefixAndPartialFailure(t *testing.T) {
	for _, id := range []string{"wave-123", "ork-123"} {
		var out, errs bytes.Buffer
		app := &App{Call: func(_ context.Context, method string, params, result any) error {
			if id == "wave-123" {
				if method != "wave.stop" || params.(daemon.WaveAction).Wave != id {
					t.Fatalf("method %s params %+v", method, params)
				}
				return json.Unmarshal([]byte(`{"wave":"wave-123","items":[{"item":"S1","run":{"id":"r1","state":"stopped"}},{"item":"S2","error":{"code":"io","message":"cannot save"}}]}`), result)
			}
			if method != "run.stop" || params.(daemon.RunAction).Run != id {
				t.Fatalf("method %s params %+v", method, params)
			}
			return json.Unmarshal([]byte(`{"id":"ork-123","state":"stopped"}`), result)
		}}
		code := app.Run(context.Background(), []string{"stop", id, "--json"}, &out, &errs)
		want := ExitOK
		if strings.HasPrefix(id, "wave-") {
			want = ExitFailure
		}
		if code != want || !json.Valid(out.Bytes()) {
			t.Fatalf("code %d: %s %s", code, &out, &errs)
		}
	}
}

func TestWaveUsage(t *testing.T) {
	for _, args := range [][]string{{"wave"}, {"wave", "stop"}, {"wave", "suggest"}, {"wave", "start", "p"}, {"wave", "start", "p", "S1", "--well-specified", "maybe"}, {"stop"}} {
		var out, errs bytes.Buffer
		if code := (&App{}).Run(context.Background(), args, &out, &errs); code != ExitUsage {
			t.Fatalf("%v: %d %s %s", args, code, &out, &errs)
		}
	}
}

// specStub answers a wave start with a spec check: S2 is ready and S1 has two
// questions. It records every call.
type specStub struct {
	t       *testing.T
	calls   []string
	answers []daemon.SpecAnswerParams
	adj     bool
	started []string
	check   string
}

const specCheckTwoQuestions = `{"items":[{"item":"S2","ready":true},{"item":"S1","ready":false,"questions":[` +
	`{"ask":{"title":"Which store?","options":[{"label":"file","summary":"one JSON file"},{"label":"memory","summary":"rebuilt at each start"}],"recommend":2,"reason":"nothing reads it"},"key":"cache.store","text":"Question: Which store?"},` +
	`{"ask":{"title":"What flag?","options":[{"label":"short","summary":"-x"},{"label":"long","summary":"--extra"}],"recommend":1,"reason":"short"},"text":"Question: What flag?"}]}]}`

func (s *specStub) call(_ context.Context, method string, params, result any) error {
	s.calls = append(s.calls, method)
	switch method {
	case "projects.list":
		return json.Unmarshal([]byte(fmt.Sprintf(`{"projects":[{"name":"p","adj":%t}]}`, s.adj)), result)
	case "run.plan":
		p := params.(daemon.RunParams)
		*(result.(*daemon.RunPlan)) = daemon.RunPlan{Item: p.Item, Worktree: "/p/" + p.Item}
	case "spec.check":
		return json.Unmarshal([]byte(s.check), result)
	case "spec.answer":
		p := params.(daemon.SpecAnswerParams)
		s.answers = append(s.answers, p)
		*(result.(*daemon.SpecAnswerResult)) = daemon.SpecAnswerResult{Item: p.Item, Ready: true}
	case "wave.start":
		for _, item := range params.(daemon.WaveStartParams).Items {
			s.started = append(s.started, item.Item)
		}
		return json.Unmarshal([]byte(`{"wave":"wave-1","items":[]}`), result)
	default:
		s.t.Fatalf("method %s", method)
	}
	return nil
}

func runWaveStart(t *testing.T, stub *specStub, input string, extra ...string) (code int, out, errs string) {
	t.Helper()
	var o, e bytes.Buffer
	app := &App{Input: strings.NewReader(input), Call: stub.call}
	args := append([]string{"wave", "start", "p", "S2", "S1", "--well-specified", "yes", "--split", "no"}, extra...)
	code = app.Run(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

func TestWaveStartAsksTheSpecQuestionsInOneSittingThenStartsEveryItem(t *testing.T) {
	stub := &specStub{t: t, check: specCheckTwoQuestions, adj: true}
	t.Setenv("ADJ_DECIDED_BY", "jon")
	// Option 2 for the first question, free text for the second, then the start.
	code, out, errs := runWaveStart(t, stub, "2\nthe --flag-name\ny\n")
	if code != ExitOK {
		t.Fatalf("code %d: %s %s", code, out, errs)
	}
	for _, want := range []string{"S1 needs 2 answers", "[S1 1/2] Which store?", "2. memory: rebuilt at each start (recommended)", "Decision key: cache.store", "[S1 2/2] What flag?"} {
		if !strings.Contains(out, want) {
			t.Errorf("sitting lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "(empty holds S1 back)") {
		t.Errorf("the prompt must say what an empty line does:\n%s", out)
	}
	if len(stub.answers) != 1 {
		t.Fatalf("answers: %+v", stub.answers)
	}
	a := stub.answers[0]
	if a.Project != "p" || a.Item != "S1" || !reflect.DeepEqual(a.Answers, []string{"memory: rebuilt at each start", "the --flag-name"}) || !a.Adj || a.By != "jon" {
		t.Errorf("answer: %+v", a)
	}
	if !reflect.DeepEqual(stub.started, []string{"S2", "S1"}) {
		t.Errorf("started %v", stub.started)
	}
	want := []string{"run.plan", "run.plan", "spec.check", "projects.list", "spec.answer", "wave.start"}
	if !reflect.DeepEqual(stub.calls, want) {
		t.Errorf("calls %v, want %v", stub.calls, want)
	}
}

func TestWaveStartLeavesOutAnItemWhoseQuestionsStayOpen(t *testing.T) {
	stub := &specStub{t: t, check: specCheckTwoQuestions}
	// An empty line skips S1; the wave starts with the ready S2.
	code, out, errs := runWaveStart(t, stub, "\ny\n")
	if code != ExitOK || !reflect.DeepEqual(stub.started, []string{"S2"}) || len(stub.answers) != 0 {
		t.Fatalf("code %d started %v answers %v: %s %s", code, stub.started, stub.answers, out, errs)
	}
	if !strings.Contains(out, "held: S1 has 2 open spec questions") {
		t.Errorf("no held notice:\n%s", out)
	}
}

func TestWaveStartWithNoReadyItemDoesNotStart(t *testing.T) {
	stub := &specStub{t: t, check: strings.Replace(specCheckTwoQuestions, `{"item":"S2","ready":true},`, `{"item":"S2","ready":false,"questions":[{"ask":{"title":"T","options":[{"label":"a","summary":"A"},{"label":"b","summary":"B"}],"recommend":1,"reason":"r"},"text":"Question: T"}]},`, 1)}
	code, out, errs := runWaveStart(t, stub, "", "--yes")
	if code != ExitFailure || len(stub.started) != 0 || strings.Contains(strings.Join(stub.calls, " "), "wave.start") {
		t.Fatalf("code %d calls %v: %s %s", code, stub.calls, out, errs)
	}
	if !strings.Contains(out+errs, "held: S2") || !strings.Contains(out+errs, "held: S1") {
		t.Errorf("output:\n%s%s", out, errs)
	}
}

func TestWaveStartJSONKeepsTheSittingOffStdout(t *testing.T) {
	stub := &specStub{t: t, check: specCheckTwoQuestions}
	code, out, errs := runWaveStart(t, stub, "1\n1\ny\n", "--json")
	if code != ExitOK || !json.Valid([]byte(out)) {
		t.Fatalf("code %d: %q %s", code, out, errs)
	}
	if !strings.Contains(errs, "[S1 1/2] Which store?") {
		t.Errorf("the questions go to stderr: %s", errs)
	}
}

func TestWaveStartAsksAgainForANumberOutOfRange(t *testing.T) {
	for _, bad := range []string{"9", "0", "-1"} {
		t.Run(bad, func(t *testing.T) {
			stub := &specStub{t: t, check: specCheckTwoQuestions}
			code, out, errs := runWaveStart(t, stub, bad+"\n1\n2\ny\n")
			if code != ExitOK || len(stub.answers) != 1 {
				t.Fatalf("code %d answers %+v: %s %s", code, stub.answers, out, errs)
			}
			if !strings.Contains(out, "Pick 1-2, or type text") {
				t.Errorf("no reprompt:\n%s", out)
			}
			if got := stub.answers[0].Answers; !reflect.DeepEqual(got, []string{"file: one JSON file", "long: --extra"}) {
				t.Errorf("the bad number %q became an answer: %v", bad, got)
			}
		})
	}
	// The end of the input while asking again gives up on the item.
	stub := &specStub{t: t, check: specCheckTwoQuestions}
	code, out, _ := runWaveStart(t, stub, "9\n")
	if len(stub.answers) != 0 || !strings.Contains(out, "held: S1") {
		t.Errorf("code %d answers %v:\n%s", code, stub.answers, out)
	}
}

func TestWaveStartJSONIncludesHeldItems(t *testing.T) {
	stub := &specStub{t: t, check: specCheckTwoQuestions}
	code, out, errs := runWaveStart(t, stub, "\n", "--json", "--yes")
	var result struct {
		Wave string   `json:"wave"`
		Held []string `json:"held"`
	}
	if code != ExitOK || json.Unmarshal([]byte(out), &result) != nil || result.Wave != "wave-1" || !reflect.DeepEqual(result.Held, []string{"S1"}) {
		t.Fatalf("code %d, stdout %s, stderr %s", code, out, errs)
	}
}

func TestCLIAdjFlagMatchesRegistryForKeyedQuestions(t *testing.T) {
	for _, adj := range []bool{false, true} {
		t.Run(fmt.Sprint(adj), func(t *testing.T) {
			stub := &specStub{t: t, check: specCheckTwoQuestions, adj: adj}
			code, out, errs := runWaveStart(t, stub, "2\nlong\ny\n")
			if code != ExitOK || len(stub.answers) != 1 || stub.answers[0].Adj != adj {
				t.Fatalf("answers %+v code %d: %s %s", stub.answers, code, out, errs)
			}
		})
	}
}

func TestWaveSittingKeepsOwnerNeededText(t *testing.T) {
	stub := &specStub{t: t, check: strings.Replace(specCheckTwoQuestions, `{"items":`, `{"owner_needed":"Confirm the rollout window.","items":`, 1)}
	code, out, errs := runWaveStart(t, stub, "\n", "--json", "--yes")
	if code != ExitOK || !strings.Contains(errs, "Owner needed: Confirm the rollout window.") || strings.Contains(out, "Confirm the rollout") {
		t.Fatalf("code %d: %s %s", code, out, errs)
	}
}

func TestWaveStartJSONAlwaysIncludesHeldArray(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		check       string
		want        string
	}{
		{"start empty", "2\nlong\n", specCheckTwoQuestions, `[]`},
		{"cancel held", "\nn\n", specCheckTwoQuestions, `["S1"]`},
		{"cancel empty", "2\nlong\nn\n", specCheckTwoQuestions, `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &specStub{t: t, check: tc.check}
			args := []string{"--json"}
			if tc.name == "start empty" {
				args = append(args, "--yes")
			}
			code, out, errs := runWaveStart(t, stub, tc.input, args...)
			var result map[string]json.RawMessage
			if code != ExitOK || json.Unmarshal([]byte(out), &result) != nil {
				t.Fatalf("code %d: %s %s", code, out, errs)
			}
			var got, want []string
			if json.Unmarshal(result["held"], &got) != nil || got == nil {
				t.Fatalf("held is missing or null: %s", out)
			}
			json.Unmarshal([]byte(tc.want), &want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("held %v, want %v", got, want)
			}
		})
	}
}

func TestWaveStartPrintsTheBlockedRefusal(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		t.Run(fmt.Sprint(jsonMode), func(t *testing.T) {
			const text = `S2 waits on S1 "Title" (in this wave, not done); untick S2 or land S1 first`
			app := &App{Input: strings.NewReader("y\n"), Call: func(_ context.Context, method string, params, result any) error {
				switch method {
				case "run.plan":
					*(result.(*daemon.RunPlan)) = daemon.RunPlan{Item: "S2"}
					return nil
				case "spec.check":
					return &ipc.Error{Code: daemon.CodeRefused, Message: text}
				default:
					t.Fatalf("unexpected call %s", method)
					return nil
				}
			}}
			var out, errs bytes.Buffer
			args := []string{"wave", "start", "p", "S2", "--well-specified", "yes", "--split", "no"}
			if jsonMode {
				args = append(args, "--json")
			}
			code := app.Run(context.Background(), args, &out, &errs)
			combined := out.String() + errs.String()
			wantText := text
			if jsonMode {
				raw, _ := json.Marshal(text)
				wantText = string(raw)
			}
			if code != ExitFailure || strings.Count(combined, wantText) != 1 || strings.Contains(combined, "Start this wave?") {
				t.Fatalf("code %d output %s", code, combined)
			}
			if jsonMode && strings.Count(out.String(), `"code": "refused"`) != 1 {
				t.Fatalf("wire code: %s", &out)
			}
			if !jsonMode && strings.Contains(combined, "refused:") {
				t.Fatalf("human refusal should show text alone: %s", combined)
			}
		})
	}
}

func TestWaveStartLineupFlagsApplyToEveryItem(t *testing.T) {
	var plans []daemon.RunParams
	var started daemon.WaveStartParams
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		switch method {
		case daemon.MethodRunPlan:
			p := params.(daemon.RunParams)
			plans = append(plans, p)
			*result.(*daemon.RunPlan) = daemon.RunPlan{Item: p.Item, Lineup: p.Lineup}
		case "spec.check":
			return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true},{"item":"S2","ready":true}]}`), result)
		case daemon.MethodWaveStart:
			started = params.(daemon.WaveStartParams)
		default:
			t.Fatalf("unexpected %s", method)
		}
		return nil
	}}
	code, out, errs := run(t, app, "wave", "start", "p", "S1", "S2", "--yes", "--well-specified", "y", "--split", "n", "--implementer", "codex:sol:medium", "--implementer", "claude:sonnet:high", "--architect", "off", "--budget-minutes", "20", "--budget-tokens", "0")
	if code != 0 || len(plans) != 2 || len(started.Items) != 2 {
		t.Fatalf("exit %d plans %d items %d: %s %s", code, len(plans), len(started.Items), out, errs)
	}
	for i, p := range started.Items {
		if p.Expected == nil || p.Expected.Item != plans[i].Item {
			t.Fatal("Expected lost")
		}
		p.Expected = nil
		if !reflect.DeepEqual(p, plans[i]) || len(p.Lineup["implementer"]) != 2 || p.Lineup["architect"] == nil || p.Budgets == nil || p.Budgets.Minutes != 20 || p.Budgets.Tokens != 0 {
			t.Fatalf("item %d: %+v", i, p)
		}
	}
}

func TestWaveStartStopAfterFlag(t *testing.T) {
	var plans []daemon.RunParams
	var started daemon.WaveStartParams
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		switch method {
		case daemon.MethodRunPlan:
			p := params.(daemon.RunParams)
			plans = append(plans, p)
			*result.(*daemon.RunPlan) = daemon.RunPlan{Item: p.Item, Lineup: p.Lineup}
		case "spec.check":
			return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true},{"item":"S2","ready":true}]}`), result)
		case daemon.MethodWaveStart:
			started = params.(daemon.WaveStartParams)
		default:
			t.Fatalf("unexpected %s", method)
		}
		return nil
	}}
	code, out, errs := run(t, app, "wave", "start", "p", "S1", "S2", "--yes", "--well-specified", "y", "--split", "n", "--stop-after", "commit")
	if code != 0 || len(plans) != 2 || len(started.Items) != 2 {
		t.Fatalf("exit %d plans %d items %d: %s %s", code, len(plans), len(started.Items), out, errs)
	}
	for i, p := range started.Items {
		if p.Expected == nil || p.Expected.Item != plans[i].Item {
			t.Fatal("Expected lost")
		}
		p.Expected = nil
		if !reflect.DeepEqual(p, plans[i]) || p.StopAfter != "commit" {
			t.Fatalf("item %d: %+v", i, p)
		}
	}
}
