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
	"time"

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
	// Wave suggest also reads each placed item with show for the engine rule
	// (S218); this fake answers no show, so those reads fail open.
	if n := planParallelCalls(fake); n != 1 {
		t.Fatalf("plan-parallel calls: %d in %+v", n, fake.CallsTo("slicer"))
	}
}

// planParallelCalls counts the calls that read slicer's plan.
func planParallelCalls(fake *clitest.Fake) int {
	n := 0
	for _, c := range fake.CallsTo("slicer") {
		if len(c.Args) > 0 && c.Args[0] == "plan-parallel" {
			n++
		}
	}
	return n
}

// engineWaveApp is waveApp with a plan whose first wave holds three engine
// slices and one plain slice; show answers each item's Files section.
func engineWaveApp(t *testing.T, withShow bool) (*App, *clitest.Fake) {
	t.Helper()
	plan := `{"width":4,"waves":[{"wave":1,"items":[{"id":"E1","title":"first engine"},{"id":"E2","title":"second engine","reason":"shares: E1 internal/engine/a.go"},{"id":"N1","title":"plain"},{"id":"E3","title":"third engine"}]},{"wave":2,"items":[{"id":"N2","title":"after the engine","reason":"after: E2"}]}],"in_flight":[],"unplaced":[],"blocked":[],"held":[],"unspecified":[]}`
	routes := []clitest.Route{{Tool: "slicer", Args: []string{"plan-parallel", "--json", "--lean", "--root", "/project/p"}, Stdout: plan}}
	if withShow {
		for id, path := range map[string]string{"E1": "internal/engine/a.go", "E2": "internal/daemon/b.go", "E3": "cmd/ork/main.go", "N1": "docs/c.md", "N2": "docs/d.md"} {
			body := fmt.Sprintf(`{"id":%q,"title":"t","slice":{"id":%q,"title":"t","sections":[{"heading":"Files","body":"- `+"`%s`"+`"}]}}`, id, id, path)
			routes = append(routes, clitest.Route{Tool: "slicer", Args: []string{"show", id, "--json", "--lean", "--root", "/project/p"}, Stdout: body})
		}
	}
	fake := clitest.Install(t, routes...)
	app := &App{Tools: Tools{Slicer: fake.Bin("slicer")}, Call: func(_ context.Context, method string, params, result any) error {
		return json.Unmarshal([]byte(`{"projects":[{"name":"p","path":"/project/p"}]}`), result)
	}}
	return app, fake
}

func TestWaveSuggestDefersASecondEngineSlice(t *testing.T) {
	app, _ := engineWaveApp(t, true)
	var out, errs bytes.Buffer
	if code := app.Run(context.Background(), []string{"wave", "suggest", "p"}, &out, &errs); code != ExitOK {
		t.Fatalf("code %d: %s %s", code, &out, &errs)
	}
	want := "Width: 4\nWave 1\n  E1 first engine\n  N1 plain\nWave 2\n  E2 second engine (engine: E1)\nWave 3\n  E3 third engine (engine: E1)\nWave 4\n  N2 after the engine (after: E2)\n"
	if out.String() != want {
		t.Fatalf("text:\n%s\nwant:\n%s", &out, want)
	}
	out.Reset()
	if code := app.Run(context.Background(), []string{"wave", "suggest", "p", "--json"}, &out, &errs); code != ExitOK {
		t.Fatalf("json code %d: %s %s", code, &out, &errs)
	}
	var plan struct {
		Waves []struct {
			Wave  int
			Items []struct{ ID, Reason string }
		}
	}
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Waves) != 4 || plan.Waves[1].Wave != 2 || plan.Waves[1].Items[0].ID != "E2" || plan.Waves[1].Items[0].Reason != "engine: E1" || plan.Waves[2].Items[0].Reason != "engine: E1" {
		t.Fatalf("JSON: %s", &out)
	}
}

func TestWaveSuggestStillWorksWithoutAShowRoute(t *testing.T) {
	app, fake := engineWaveApp(t, false)
	var out, errs bytes.Buffer
	if code := app.Run(context.Background(), []string{"wave", "suggest", "p"}, &out, &errs); code != ExitOK {
		t.Fatalf("code %d: %s %s", code, &out, &errs)
	}
	want := "Width: 4\nWave 1\n  E1 first engine\n  E2 second engine (shares: E1 internal/engine/a.go)\n  N1 plain\n  E3 third engine\nWave 2\n  N2 after the engine (after: E2)\n"
	if out.String() != want {
		t.Fatalf("text:\n%s\nwant:\n%s", &out, want)
	}
	if n := planParallelCalls(fake); n != 1 || len(fake.CallsTo("slicer")) != 6 {
		t.Fatalf("calls: %+v", fake.CallsTo("slicer"))
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
		{"confirm", "y\n", nil, true, ExitOK},
		{"decline", "\n", nil, false, ExitOK},
		{"closed", "", nil, false, ExitUsage},
		{"yes", "", []string{"--yes"}, true, ExitOK},
		{"bad flag", "", []string{"--yes", "--split", "maybe"}, false, ExitUsage},
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
					if !p.WellSpecified || p.Split {
						t.Fatalf("answers: %+v", p)
					}
					plans++
					*(result.(*daemon.RunPlan)) = daemon.RunPlan{CheckCommand: "go test ./...", Item: p.Item, Worktree: "/p/" + p.Item}
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
			if checked != (tc.name != "bad flag") {
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

func TestWaveUsageListsDefineListRmStart(t *testing.T) {
	// One positional after "wave start" is now a saved wave's name, so it is no
	// longer a usage error; the forms below still are.
	for _, args := range [][]string{
		{"wave"}, {"wave", "stop"}, {"wave", "suggest"}, {"wave", "start"},
		{"wave", "start", "p", "S1", "--well-specified", "maybe"}, {"stop"},
		{"wave", "define"}, {"wave", "define", "--project", "p"}, {"wave", "define", "--project", "p", "pair-a"},
		{"wave", "define", "pair-a", "S1"}, {"wave", "rm"}, {"wave", "rm", "a", "b"}, {"wave", "list", "extra"},
		{"wave", "bogus"},
	} {
		var out, errs bytes.Buffer
		if code := (&App{}).Run(context.Background(), args, &out, &errs); code != ExitUsage {
			t.Fatalf("%v: %d %s %s", args, code, &out, &errs)
		}
	}
	// The command's own usage line and the help text name all four.
	var out, errs bytes.Buffer
	if code := (&App{}).Run(context.Background(), []string{"wave"}, &out, &errs); code != ExitUsage ||
		!strings.Contains(errs.String(), "define") || !strings.Contains(errs.String(), "list") ||
		!strings.Contains(errs.String(), "rm") || !strings.Contains(errs.String(), "start") {
		t.Fatalf("wave usage: %d %s", code, &errs)
	}
	out.Reset()
	errs.Reset()
	if code := (&App{}).Run(context.Background(), []string{"help"}, &out, &errs); code != ExitOK {
		t.Fatalf("help: %d", code)
	}
	for _, want := range []string{
		"ork wave define --project P NAME ITEM...", "ork wave list [--project P]", "ork wave rm [--project P] NAME",
		"ork wave start [--project P] NAME", "ork wave start P ITEM...",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help does not show %q:\n%s", want, &out)
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
	planErr map[string]error // the run.plan error of an item, by id
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
		if err := s.planErr[p.Item]; err != nil {
			return err
		}
		*(result.(*daemon.RunPlan)) = daemon.RunPlan{CheckCommand: "go test ./...", Item: p.Item, Worktree: "/p/" + p.Item}
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

// runWaveItems runs `ork wave start p ITEM...` with the default answers.
func runWaveItems(t *testing.T, stub *specStub, input string, items []string, extra ...string) (code int, out, errs string) {
	t.Helper()
	var o, e bytes.Buffer
	app := &App{Input: strings.NewReader(input), Call: stub.call}
	args := append(append([]string{"wave", "start", "p"}, items...), "--well-specified", "yes", "--split", "no")
	code = app.Run(context.Background(), append(args, extra...), &o, &e)
	return code, o.String(), e.String()
}

func (s *specStub) waveStarts() (n int) {
	for _, c := range s.calls {
		if c == "wave.start" {
			n++
		}
	}
	return n
}

const specCheckS2NoSpec = `{"items":[{"item":"S1","ready":true},{"item":"S2","ready":false,"no_spec":true}]}`

func TestWaveStartSaysToWaitForTheSpecCheck(t *testing.T) {
	stub := &specStub{t: t, check: specCheckTwoQuestions}
	var o, e bytes.Buffer
	var atCall string // what stdout held when the spec check was called
	app := &App{Input: strings.NewReader("2\ncustom flag\ny\n"), Call: func(ctx context.Context, method string, params, result any) error {
		if method == "spec.check" {
			atCall = o.String()
		}
		return stub.call(ctx, method, params, result)
	}}
	code := app.Run(context.Background(), []string{"wave", "start", "p", "S2", "S1", "--well-specified", "yes", "--split", "no"}, &o, &e)
	out, errs := o.String(), e.String()
	if !strings.HasSuffix(atCall, "; wait for it to finish\n") {
		t.Errorf("the wait line must come before the spec.check call; stdout then:\n%s", atCall)
	}
	if code != ExitOK {
		t.Fatalf("code %d: %s %s", code, out, errs)
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "spec check: ") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "S1") || !strings.Contains(lines[0], "S2") || !strings.HasSuffix(lines[0], "; wait for it to finish") {
		t.Fatalf("wait lines %q in:\n%s", lines, out)
	}
	if i, j := strings.Index(out, lines[0]), strings.Index(out, "[S1 1/2]"); i < 0 || j < 0 || i > j {
		t.Errorf("the wait line must come before the questions:\n%s", out)
	}
}

func TestWaveStartSkipsAnItemWithNoSpec(t *testing.T) {
	stub := &specStub{t: t, check: specCheckS2NoSpec}
	code, out, errs := runWaveItems(t, stub, "", []string{"S1", "S2"}, "--yes")
	if code != ExitOK {
		t.Fatalf("code %d: %s %s", code, out, errs)
	}
	if stub.waveStarts() != 1 || !reflect.DeepEqual(stub.started, []string{"S1"}) {
		t.Errorf("want one wave.start with S1 alone: calls %v started %v", stub.calls, stub.started)
	}
	if !strings.Contains(out, "S2: no slice spec; run slicer promote S2\n") {
		t.Errorf("the skipped item is not named with its fix:\n%s%s", out, errs)
	}
	if strings.Contains(out, "S1: no slice spec") || strings.Contains(out, "held:") {
		t.Errorf("only S2 is skipped, and not as held:\n%s", out)
	}
}

func TestWaveStartAllItemsWithoutASpecStartsNothing(t *testing.T) {
	stub := &specStub{t: t, check: `{"items":[{"item":"S1","ready":false,"no_spec":true},{"item":"S2","ready":false,"no_spec":true}]}`}
	code, out, errs := runWaveItems(t, stub, "", []string{"S1", "S2"}, "--yes")
	if code != ExitFailure || stub.waveStarts() != 0 || len(stub.started) != 0 {
		t.Fatalf("code %d calls %v: %s %s", code, stub.calls, out, errs)
	}
	for _, want := range []string{"S1: no slice spec; run slicer promote S1\n", "S2: no slice spec; run slicer promote S2\n", "no item is ready to start"} {
		if !strings.Contains(out+errs, want) {
			t.Errorf("missing %q:\n%s%s", want, out, errs)
		}
	}
}

func TestWaveStartJSONListsTheSkippedItems(t *testing.T) {
	stub := &specStub{t: t, check: specCheckS2NoSpec}
	code, out, errs := runWaveItems(t, stub, "", []string{"S1", "S2"}, "--json", "--yes")
	var result map[string]json.RawMessage
	if code != ExitOK || json.Unmarshal([]byte(out), &result) != nil {
		t.Fatalf("code %d: %s %s", code, out, errs)
	}
	var noSpec, held []string
	if json.Unmarshal(result["no_spec"], &noSpec) != nil || !reflect.DeepEqual(noSpec, []string{"S2"}) {
		t.Errorf("no_spec: %s", out)
	}
	if json.Unmarshal(result["held"], &held) != nil || held == nil || len(held) != 0 {
		t.Errorf("held must stay an empty array: %s", out)
	}
	if string(result["wave"]) != `"wave-1"` {
		t.Errorf("the wave result is kept: %s", out)
	}
	if strings.Contains(out, "slicer promote") || !strings.Contains(errs, "S2: no slice spec; run slicer promote S2") {
		t.Errorf("the hint goes to stderr with --json:\nstdout %s\nstderr %s", out, errs)
	}
	// Nothing starts: the same array shows beside started false, also when the
	// start is declined.
	for name, tc := range map[string]struct {
		check, input string
		flags        []string
		code         int
	}{
		"none ready": {`{"items":[{"item":"S1","ready":false,"no_spec":true},{"item":"S2","ready":false,"no_spec":true}]}`, "", []string{"--yes"}, ExitFailure},
		"declined":   {specCheckS2NoSpec, "n\n", nil, ExitOK},
	} {
		stub := &specStub{t: t, check: tc.check}
		code, out, errs := runWaveItems(t, stub, tc.input, []string{"S1", "S2"}, append([]string{"--json"}, tc.flags...)...)
		var got struct {
			Started bool     `json:"started"`
			Held    []string `json:"held"`
			NoSpec  []string `json:"no_spec"`
		}
		if code != tc.code || json.Unmarshal([]byte(out), &got) != nil || got.Started || got.Held == nil || len(got.NoSpec) == 0 || stub.waveStarts() != 0 {
			t.Errorf("%s: code %d: %s %s", name, code, out, errs)
		}
	}
}

func TestWaveStartOtherPlanFailuresStillAbort(t *testing.T) {
	stub := &specStub{t: t, check: specCheckS2NoSpec, planErr: map[string]error{"S2": &ipc.Error{Code: "not_found", Message: "S2 cannot be planned"}}}
	code, out, errs := runWaveItems(t, stub, "", []string{"S1", "S2"}, "--yes")
	if code != ExitFailure || !strings.Contains(out+errs, "S2 cannot be planned") {
		t.Fatalf("code %d: %s %s", code, out, errs)
	}
	for _, c := range stub.calls {
		if c == "spec.check" || c == "wave.start" {
			t.Errorf("%s ran after a plan failure: %v", c, stub.calls)
		}
	}
	if len(stub.started) != 0 {
		t.Errorf("started %v", stub.started)
	}
}

func TestWaveStartMissingSpecAfterAnOpenQuestion(t *testing.T) {
	stub := &specStub{t: t, check: strings.Replace(specCheckTwoQuestions, `{"item":"S2","ready":true}`, `{"item":"S2","ready":false,"no_spec":true}`, 1)}
	code, out, errs := runWaveStart(t, stub, "2\nthe --flag-name\ny\n")
	if code != ExitOK {
		t.Fatalf("code %d: %s %s", code, out, errs)
	}
	if len(stub.answers) != 1 || stub.answers[0].Item != "S1" || len(stub.answers[0].Answers) != 2 {
		t.Errorf("answers: %+v", stub.answers)
	}
	if !reflect.DeepEqual(stub.started, []string{"S1"}) || stub.waveStarts() != 1 {
		t.Errorf("started %v, calls %v", stub.started, stub.calls)
	}
	if !strings.Contains(out, "S2: no slice spec; run slicer promote S2") || strings.Contains(out, "S2 needs") {
		t.Errorf("S2 is skipped, never asked about:\n%s", out)
	}
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
					*(result.(*daemon.RunPlan)) = daemon.RunPlan{CheckCommand: "go test ./...", Item: "S2"}
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
			*result.(*daemon.RunPlan) = daemon.RunPlan{CheckCommand: "go test ./...", Item: p.Item, Lineup: p.Lineup}
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
			*result.(*daemon.RunPlan) = daemon.RunPlan{CheckCommand: "go test ./...", Item: p.Item, Lineup: p.Lineup}
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

func TestWaveStartHeldLineStartsOnItsOwnLineAtEOF(t *testing.T) {
	stub := &specStub{t: t, check: `{"items":[{"item":"S1","questions":[{"text":"Question?"}]},{"item":"S2","ready":true}]}`}
	code, out, errs := runWaveStart(t, stub, "", "--yes")
	if code != 0 || !strings.Contains(out, "): \nheld: S1 has 1 open spec question") {
		t.Fatalf("code %d out %q err %s", code, out, errs)
	}
}

func TestSpecCheckErrorHasAPrefixInTextMode(t *testing.T) {
	for _, wireError := range []ipc.Error{
		{Code: "spec_failed", Message: "MESSAGE"},
		{Code: daemon.CodeRefused, Message: `S2 waits on S1 "Title" (not done); land S1 first`},
	} {
		for _, wave := range []bool{false, true} {
			for _, jsonMode := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/wave=%v/json=%v", wireError.Code, wave, jsonMode), func(t *testing.T) {
					stub := &specStub{t: t}
					app := &App{Call: func(ctx context.Context, m string, p, r any) error {
						if m == daemon.MethodSpecCheck {
							return &wireError
						}
						return stub.call(ctx, m, p, r)
					}}
					args := []string{"run", "p", "S1", "--well-specified", "y", "--split", "n"}
					if wave {
						args = append([]string{"wave", "start"}, args[1:]...)
					}
					if jsonMode {
						args = append(args, "--json")
					}
					code, out, errs := run(t, app, args...)
					if code != 1 {
						t.Fatal(code, out, errs)
					}
					if jsonMode {
						var result struct{ Error ipc.Error }
						if err := json.Unmarshal([]byte(out), &result); err != nil || result.Error != wireError {
							t.Fatal(out, err)
						}
					} else {
						prefix := "spec check failed: "
						if wireError.Code == daemon.CodeRefused {
							prefix = ""
						}
						if want := "ork: " + prefix + wireError.Message + "\n"; errs != want {
							t.Errorf("stderr %q, want %q", errs, want)
						}
					}
				})
			}
		}
	}
}

func TestSpecQuestionsWrapAtEightyColumns(t *testing.T) {
	long := strings.Repeat("word ", 40)
	check := strings.ReplaceAll(specCheckTwoQuestions, "Which store?", long)
	check = strings.ReplaceAll(check, "one JSON file", long)
	check = strings.ReplaceAll(check, "nothing reads it", long)
	// Also exercise a text-only question.
	check = strings.ReplaceAll(check, `{"ask":{"title":"What flag?","options":[{"label":"short","summary":"-x"},{"label":"long","summary":"--extra"}],"recommend":1,"reason":"short"},"text":"Question: What flag?"}`, `{"text":"`+long+`"}`)
	for _, wave := range []bool{false, true} {
		var code int
		var out, errs string
		if wave {
			code, out, errs = runWaveStart(t, &specStub{t: t, check: check}, "1\nanswer\nn\n")
		} else {
			code, out, errs, _ = singleSpecRun(t, "1\nanswer\nn\n", check, nil)
		}
		if code != 0 {
			t.Fatal(code, out, errs)
		}
		// A terminal echoes the answer's newline. The string reader does not;
		// remove prompts before measuring the printed question lines.
		questions := strings.ReplaceAll(out, "Answer 1-2, or type text (empty holds S1 back): ", "\n")
		questions = strings.ReplaceAll(questions, "Answer in text (empty holds S1 back): ", "\n")
		for _, line := range strings.Split(questions, "\n") {
			if len([]rune(line)) > 80 {
				t.Errorf("wave=%v: %d columns: %s", wave, len([]rune(line)), line)
			}
			if strings.HasPrefix(line, "word") {
				t.Errorf("missing continuation indent: %q", line)
			}
		}
		if !strings.Contains(out, "\n    word") || !strings.Contains(out, "\n  word") {
			t.Errorf("missing hanging lines: %s", out)
		}
	}
}

// savedList is what wave.list answers in the saved wave tests: pair-a in two
// projects, and solo, whose members cannot start.
const savedList = `{"waves":[` +
	`{"project":"p","name":"pair-a","items":[{"item":"S1","status":"open","title":"One"},{"item":"S2","status":"done","title":"Two"},{"item":"S4","status":"started","title":"Four"}],"created_at":"2026-11-01T12:00:00Z","updated_at":"2026-11-01T12:00:00Z"},` +
	`{"project":"q","name":"pair-a","items":[{"item":"Q1","status":"open","title":"Quick"}],"created_at":"2026-11-01T12:00:00Z","updated_at":"2026-11-01T12:00:00Z"},` +
	`{"project":"p","name":"solo","items":[{"item":"S3","status":"retired","title":"Three"},{"item":"S9","status":"missing","title":""}],"created_at":"2026-11-01T12:00:00Z","updated_at":"2026-11-01T12:00:00Z"}]}`

// savedStub answers the saved wave methods and the calls of a wave start, and
// records every call.
type savedStub struct {
	t       *testing.T
	list    string // the wave.list result before the project filter
	calls   []string
	listed  []daemon.WaveListParams
	defined []daemon.WaveDefineParams
	removed []daemon.WaveRmParams
	planned []daemon.RunParams
	checked []daemon.SpecCheckParams
	started []daemon.WaveStartParams
}

func (s *savedStub) call(_ context.Context, method string, params, result any) error {
	s.calls = append(s.calls, method)
	switch method {
	case daemon.MethodWaveList:
		p := params.(daemon.WaveListParams)
		s.listed = append(s.listed, p)
		var all daemon.WaveListResult
		if err := json.Unmarshal([]byte(s.list), &all); err != nil {
			s.t.Fatal(err)
		}
		out := daemon.WaveListResult{Waves: []daemon.SavedWaveView{}}
		for _, w := range all.Waves {
			if p.Project == "" || w.Project == p.Project {
				out.Waves = append(out.Waves, w)
			}
		}
		*(result.(*daemon.WaveListResult)) = out
	case daemon.MethodWaveDefine:
		p := params.(daemon.WaveDefineParams)
		s.defined = append(s.defined, p)
		at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
		*(result.(*daemon.SavedWave)) = daemon.SavedWave{Project: p.Project, Name: p.Name, Items: p.Items, CreatedAt: at, UpdatedAt: at}
	case daemon.MethodWaveRm:
		p := params.(daemon.WaveRmParams)
		s.removed = append(s.removed, p)
		*(result.(*daemon.SavedWave)) = daemon.SavedWave{Project: p.Project, Name: p.Name, Items: []string{"S1"}}
	case daemon.MethodRunPlan:
		p := params.(daemon.RunParams)
		s.planned = append(s.planned, p)
		*(result.(*daemon.RunPlan)) = daemon.RunPlan{CheckCommand: "go test ./...", Item: p.Item, Worktree: "/p/" + p.Item, Lineup: p.Lineup}
	case daemon.MethodSpecCheck:
		p := params.(daemon.SpecCheckParams)
		s.checked = append(s.checked, p)
		ready := make([]string, 0, len(p.Items))
		for _, id := range p.Items {
			ready = append(ready, fmt.Sprintf(`{"item":%q,"ready":true}`, id))
		}
		return json.Unmarshal([]byte(`{"items":[`+strings.Join(ready, ",")+`]}`), result)
	case daemon.MethodWaveStart:
		p := params.(daemon.WaveStartParams)
		s.started = append(s.started, p)
		items := make([]string, 0, len(p.Items))
		for _, item := range p.Items {
			items = append(items, fmt.Sprintf(`{"item":%q,"run":{"id":"run-%s","state":"setup"}}`, item.Item, item.Item))
		}
		name := ""
		if p.Name != "" {
			name = fmt.Sprintf(`"name":%q,`, p.Name)
		}
		return json.Unmarshal([]byte(`{"wave":"wave-9",`+name+`"items":[`+strings.Join(items, ",")+`]}`), result)
	default:
		s.t.Fatalf("unexpected method %s", method)
	}
	return nil
}

func (s *savedStub) run(args ...string) (code int, out, errs string) {
	s.t.Helper()
	var o, e bytes.Buffer
	app := &App{Input: strings.NewReader(""), Call: s.call}
	code = app.Run(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

func (s *savedStub) has(method string) bool {
	for _, c := range s.calls {
		if c == method {
			return true
		}
	}
	return false
}

func TestWaveDefineListRm(t *testing.T) {
	// define prints the saved wave.
	stub := &savedStub{t: t, list: savedList}
	code, out, errs := stub.run("wave", "define", "--project", "p", "pair-a", "S4", "S1")
	if code != ExitOK || out != "pair-a  p\n  S4\n  S1\n" || errs != "" {
		t.Fatalf("define: %d %q %q", code, out, errs)
	}
	if want := []daemon.WaveDefineParams{{Project: "p", Name: "pair-a", Items: []string{"S4", "S1"}}}; !reflect.DeepEqual(stub.defined, want) {
		t.Fatalf("define params: %+v", stub.defined)
	}
	code, out, _ = stub.run("wave", "define", "--project", "p", "pair-a", "S2", "--replace", "--json")
	var saved daemon.SavedWave
	if code != ExitOK || json.Unmarshal([]byte(out), &saved) != nil || saved.Name != "pair-a" || !reflect.DeepEqual(saved.Items, []string{"S2"}) || !strings.Contains(out, `"created_at"`) {
		t.Fatalf("define --json: %d %s", code, out)
	}
	if got := stub.defined[1]; !got.Replace || got.Name != "pair-a" || !reflect.DeepEqual(got.Items, []string{"S2"}) {
		t.Fatalf("--replace: %+v", got)
	}

	// list prints a block per wave: name and project, then each member with its
	// status and title.
	code, out, errs = stub.run("wave", "list")
	want := "pair-a  p\n  S1  open     One\n  S2  done     Two\n  S4  started  Four\n\n" +
		"pair-a  q\n  Q1  open  Quick\n\n" +
		"solo  p\n  S3  retired  Three\n  S9  missing\n"
	if code != ExitOK || out != want || errs != "" {
		t.Fatalf("list: %d\n%q\nwant\n%q\n%q", code, out, want, errs)
	}
	code, out, _ = stub.run("wave", "list", "--json")
	var listed daemon.WaveListResult
	if code != ExitOK || json.Unmarshal([]byte(out), &listed) != nil || len(listed.Waves) != 3 || listed.Waves[1].Items[0].Item != "Q1" || listed.Waves[0].Items[1].Status != "done" {
		t.Fatalf("list --json: %d %s", code, out)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil || len(raw) != 1 || raw["waves"] == nil {
		t.Fatalf("list --json prints the daemon result: %s", out)
	}
	// A project that slicer could not be read for shows its error, and its
	// members without a status.
	broken := &savedStub{t: t, list: `{"waves":[{"project":"q","name":"b","items":[{"item":"Q1","status":"","title":""}],"error":{"code":"no_root","message":"no slicer root here"}}]}`}
	code, out, _ = broken.run("wave", "list")
	if code != ExitOK || out != "b  q\n  error: no slicer root here (no_root)\n  Q1\n" {
		t.Fatalf("list with an error: %d %q", code, out)
	}
	// No saved wave at all.
	empty := &savedStub{t: t, list: `{"waves":[]}`}
	code, out, _ = empty.run("wave", "list")
	if code != ExitOK || out != "no saved waves\n" {
		t.Fatalf("empty list: %d %q", code, out)
	}
	code, out, _ = empty.run("wave", "list", "--json")
	if code != ExitOK || strings.Contains(out, "null") || !strings.Contains(out, `"waves": []`) {
		t.Fatalf("empty list --json: %d %s", code, out)
	}

	// rm removes it.
	code, out, errs = stub.run("wave", "rm", "--project", "p", "pair-a")
	if code != ExitOK || out != "removed pair-a (p)\n" || errs != "" || !reflect.DeepEqual(stub.removed, []daemon.WaveRmParams{{Project: "p", Name: "pair-a"}}) {
		t.Fatalf("rm: %d %q %q %+v", code, out, errs, stub.removed)
	}
	code, out, _ = stub.run("wave", "rm", "--project", "p", "pair-a", "--json")
	saved = daemon.SavedWave{}
	if code != ExitOK || json.Unmarshal([]byte(out), &saved) != nil || saved.Name != "pair-a" {
		t.Fatalf("rm --json: %d %s", code, out)
	}
	// A daemon refusal is the command's failure, shown as for other commands.
	refuse := func(_ context.Context, method string, _, _ any) error {
		return &ipc.Error{Code: daemon.CodeDuplicateName, Message: `a saved wave "pair-a" already exists for project p`}
	}
	var o, e bytes.Buffer
	if code := (&App{Call: refuse}).Run(context.Background(), []string{"wave", "define", "--project", "p", "pair-a", "S1"}, &o, &e); code != ExitFailure || !strings.Contains(e.String(), "already exists") {
		t.Fatalf("refused define: %d %s %s", code, &o, &e)
	}
}

func TestWaveStartByNameSkipsDoneMembers(t *testing.T) {
	stub := &savedStub{t: t, list: savedList}
	code, out, errs := stub.run("wave", "start", "--project", "p", "pair-a", "--yes", "--well-specified", "y", "--split", "n")
	if code != ExitOK {
		t.Fatalf("code %d: %s %s", code, out, errs)
	}
	ids := func(items []daemon.RunParams) (got []string) {
		for _, p := range items {
			got = append(got, p.Item)
		}
		return got
	}
	if got := ids(stub.planned); !reflect.DeepEqual(got, []string{"S1", "S4"}) {
		t.Fatalf("planned %v", got)
	}
	if len(stub.checked) != 1 || stub.checked[0].Project != "p" || !reflect.DeepEqual(stub.checked[0].Items, []string{"S1", "S4"}) {
		t.Fatalf("spec check: %+v", stub.checked)
	}
	if len(stub.started) != 1 || stub.started[0].Name != "pair-a" || !reflect.DeepEqual(ids(stub.started[0].Items), []string{"S1", "S4"}) {
		t.Fatalf("wave.start: %+v", stub.started)
	}
	for _, item := range stub.started[0].Items {
		if item.Project != "p" || item.Expected == nil {
			t.Fatalf("item: %+v", item)
		}
	}
	if !strings.Contains(out, "skipped S2: done\n") || strings.Contains(out, "skipped S1") || !strings.Contains(out, "wave-9") {
		t.Fatalf("output:\n%s", out)
	}

	// With --json the skipped line goes to stderr, and stdout is JSON that keeps
	// the name and names the skipped member.
	stub = &savedStub{t: t, list: savedList}
	code, out, errs = stub.run("wave", "start", "--project", "p", "pair-a", "--yes", "--well-specified", "y", "--split", "n", "--json")
	var result struct {
		Wave    string                  `json:"wave"`
		Name    string                  `json:"name"`
		Skipped []daemon.SkippedItem    `json:"skipped"`
		Held    []string                `json:"held"`
		Items   []daemon.WaveItemResult `json:"items"`
	}
	if code != ExitOK || json.Unmarshal([]byte(out), &result) != nil {
		t.Fatalf("json: %d %s %s", code, out, errs)
	}
	if result.Name != "pair-a" || result.Wave != "wave-9" || len(result.Items) != 2 || result.Held == nil ||
		!reflect.DeepEqual(result.Skipped, []daemon.SkippedItem{{Item: "S2", Status: "done"}}) {
		t.Fatalf("json result: %s", out)
	}
	if !strings.Contains(errs, "skipped S2: done") || strings.Contains(out, "skipped S2: done") {
		t.Fatalf("the skipped line goes to stderr: %q %q", out, errs)
	}
	// A wave with nothing skipped still prints the key, as an empty array.
	stub = &savedStub{t: t, list: `{"waves":[{"project":"p","name":"all","items":[{"item":"S1","status":"open","title":"One"}]}]}`}
	_, out, _ = stub.run("wave", "start", "all", "--yes", "--well-specified", "y", "--split", "n", "--json")
	if !strings.Contains(out, `"skipped": []`) {
		t.Fatalf("skipped is not an empty array: %s", out)
	}
	// The typed form prints no such key.
	stub = &savedStub{t: t}
	_, out, _ = stub.run("wave", "start", "p", "S1", "--yes", "--well-specified", "y", "--split", "n", "--json")
	if strings.Contains(out, "skipped") || strings.Contains(out, `"name"`) {
		t.Fatalf("typed start output changed: %s", out)
	}
}

func TestWaveStartByNameWithNoStartableMemberDoesNotStart(t *testing.T) {
	stub := &savedStub{t: t, list: savedList}
	code, out, errs := stub.run("wave", "start", "solo", "--yes", "--well-specified", "y", "--split", "n")
	if code != ExitFailure {
		t.Fatalf("code %d: %s %s", code, out, errs)
	}
	for _, want := range []string{"skipped S3: retired", "skipped S9: missing", "no item is ready to start"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not say %q:\n%s%s", want, out, errs)
		}
	}
	for _, method := range []string{daemon.MethodRunPlan, daemon.MethodSpecCheck, daemon.MethodWaveStart} {
		if stub.has(method) {
			t.Errorf("%s was called for a wave with nothing to start", method)
		}
	}

	stub = &savedStub{t: t, list: savedList}
	code, out, errs = stub.run("wave", "start", "solo", "--yes", "--well-specified", "y", "--split", "n", "--json")
	var got struct {
		Started bool                 `json:"started"`
		Held    []string             `json:"held"`
		Skipped []daemon.SkippedItem `json:"skipped"`
	}
	if code != ExitFailure || json.Unmarshal([]byte(out), &got) != nil || got.Started || got.Held == nil || len(got.Held) != 0 ||
		!reflect.DeepEqual(got.Skipped, []daemon.SkippedItem{{Item: "S3", Status: "retired"}, {Item: "S9", Status: "missing"}}) {
		t.Fatalf("json: %d %s %s", code, out, errs)
	}
	if !strings.Contains(errs, "no item is ready to start") || stub.has(daemon.MethodWaveStart) || stub.has(daemon.MethodRunPlan) {
		t.Fatalf("json mode: %s calls %v", errs, stub.calls)
	}

	// A wave whose project slicer could not be read starts nothing either, and
	// says why.
	stub = &savedStub{t: t, list: `{"waves":[{"project":"q","name":"b","items":[{"item":"Q1","status":"","title":""}],"error":{"code":"no_root","message":"no slicer root here"}}]}`}
	code, out, errs = stub.run("wave", "start", "b", "--yes")
	if code != ExitFailure || !strings.Contains(out+errs, "no slicer root here") || stub.has(daemon.MethodWaveStart) || stub.has(daemon.MethodRunPlan) {
		t.Fatalf("unreadable project: %d %s %s calls %v", code, out, errs, stub.calls)
	}
}

func TestWaveStartByNameUnknownNameIsNotFound(t *testing.T) {
	stub := &savedStub{t: t, list: savedList}
	code, out, errs := stub.run("wave", "start", "nope", "--yes")
	if code != ExitFailure || !strings.Contains(errs, `no saved wave "nope"`) || out != "" {
		t.Fatalf("code %d: %q %q", code, out, errs)
	}
	code, out, errs = stub.run("wave", "start", "--project", "p", "nope", "--yes", "--json")
	var got struct {
		Error ipc.Error `json:"error"`
	}
	if code != ExitFailure || json.Unmarshal([]byte(out), &got) != nil || got.Error.Code != daemon.CodeNotFound || !strings.Contains(got.Error.Message, `"nope"`) || !strings.Contains(got.Error.Message, "p") {
		t.Fatalf("json: %d %s %s", code, out, errs)
	}
	// The name exists, but not in the project asked for.
	code, _, errs = stub.run("wave", "start", "--project", "q", "solo", "--yes")
	if code != ExitFailure || !strings.Contains(errs, `no saved wave "solo"`) {
		t.Fatalf("other project: %d %s", code, errs)
	}
	for _, method := range []string{daemon.MethodRunPlan, daemon.MethodSpecCheck, daemon.MethodWaveStart} {
		if stub.has(method) {
			t.Errorf("%s was called for an unknown wave", method)
		}
	}
}

func TestWaveStartByNameKeepsTheTypedFormWorking(t *testing.T) {
	stub := &savedStub{t: t, list: savedList}
	code, out, errs := stub.run("wave", "start", "p", "S2", "S1", "--yes", "--well-specified", "y", "--split", "n")
	if code != ExitOK || len(stub.started) != 1 {
		t.Fatalf("code %d: %s %s", code, out, errs)
	}
	if stub.has(daemon.MethodWaveList) {
		t.Fatal("the typed form read the saved waves")
	}
	start := stub.started[0]
	if start.Name != "" || len(start.Items) != 2 || start.Items[0].Item != "S2" || start.Items[1].Item != "S1" || start.Items[0].Project != "p" {
		t.Fatalf("wave.start: %+v", start)
	}
	if strings.Contains(out, "skipped") {
		t.Fatalf("typed start mentions skipped members: %s", out)
	}
	// A typed item that happens to be done is still planned, as before.
	stub = &savedStub{t: t, list: savedList}
	stub.run("wave", "start", "p", "S3", "S9", "--yes", "--well-specified", "y", "--split", "n")
	if len(stub.planned) != 2 {
		t.Fatalf("typed items planned: %+v", stub.planned)
	}
}

func TestWaveStartByNameAppliesFlagsToEveryMember(t *testing.T) {
	stub := &savedStub{t: t, list: savedList}
	code, out, errs := stub.run("wave", "start", "--project", "p", "pair-a", "--yes", "--well-specified", "n", "--split", "y", "--stop-after", "commit", "--allow-no-check",
		"--implementer", "codex:sol:medium", "--implementer", "claude:sonnet:high", "--architect", "off", "--budget-minutes", "20", "--budget-tokens", "0")
	if code != ExitOK || len(stub.planned) != 2 || len(stub.started) != 1 || len(stub.started[0].Items) != 2 {
		t.Fatalf("code %d planned %d: %s %s", code, len(stub.planned), out, errs)
	}
	for i, p := range stub.started[0].Items {
		if p.Expected == nil || p.Expected.Item != stub.planned[i].Item {
			t.Fatalf("item %d: Expected lost: %+v", i, p)
		}
		p.Expected = nil
		if !reflect.DeepEqual(p, stub.planned[i]) {
			t.Fatalf("item %d: started %+v, planned %+v", i, p, stub.planned[i])
		}
		if p.WellSpecified || !p.Split || p.StopAfter != "commit" || !p.AllowNoCheck || len(p.Lineup["implementer"]) != 2 ||
			p.Lineup["architect"] == nil || p.Budgets == nil || p.Budgets.Minutes != 20 || p.Budgets.Tokens != 0 {
			t.Fatalf("item %d: %+v", i, p)
		}
	}
	if stub.started[0].Name != "pair-a" {
		t.Fatalf("name %q", stub.started[0].Name)
	}
	// Without --yes, the saved wave asks to confirm like a typed list does.
	stub = &savedStub{t: t, list: savedList}
	var o, e bytes.Buffer
	app := &App{Input: strings.NewReader("n\n"), Call: stub.call}
	code = app.Run(context.Background(), []string{"wave", "start", "--project", "p", "pair-a", "--well-specified", "y", "--split", "n"}, &o, &e)
	if code != ExitOK || stub.has(daemon.MethodWaveStart) || !strings.Contains(o.String(), "Start this wave? [y/N]: ") || !strings.Contains(o.String(), "cancelled") {
		t.Fatalf("declined: %d %s %s", code, &o, &e)
	}
	// A bad flag value is a usage error before anything is asked of the daemon.
	stub = &savedStub{t: t, list: savedList}
	if code, _, _ := stub.run("wave", "start", "--project", "p", "pair-a", "--split", "maybe"); code != ExitUsage || len(stub.calls) != 0 {
		t.Fatalf("bad flag: %d calls %v", code, stub.calls)
	}
}

func TestWaveProjectArgumentFollowsNWQ1(t *testing.T) {
	// NW-Q1, owner 2026-11-07: --project P is required on define, optional on
	// list, and optional on rm and start when the name is saved for one project.
	t.Run("define needs --project", func(t *testing.T) {
		stub := &savedStub{t: t, list: savedList}
		code, _, errs := stub.run("wave", "define", "pair-a", "S1")
		if code != ExitUsage || !strings.Contains(errs, "--project") || len(stub.calls) != 0 {
			t.Fatalf("%d %s calls %v", code, errs, stub.calls)
		}
	})
	t.Run("list takes it or not", func(t *testing.T) {
		stub := &savedStub{t: t, list: savedList}
		if code, _, errs := stub.run("wave", "list"); code != ExitOK || !reflect.DeepEqual(stub.listed, []daemon.WaveListParams{{}}) {
			t.Fatalf("%d %s %+v", code, errs, stub.listed)
		}
		code, out, errs := stub.run("wave", "list", "--project", "q")
		if code != ExitOK || stub.listed[1] != (daemon.WaveListParams{Project: "q"}) || strings.Contains(out, "solo") || !strings.Contains(out, "Q1") {
			t.Fatalf("%d %s %s %+v", code, out, errs, stub.listed)
		}
	})
	t.Run("rm and start take the project from a unique name", func(t *testing.T) {
		stub := &savedStub{t: t, list: savedList}
		if code, _, errs := stub.run("wave", "rm", "solo"); code != ExitOK || !reflect.DeepEqual(stub.removed, []daemon.WaveRmParams{{Project: "p", Name: "solo"}}) || !reflect.DeepEqual(stub.listed, []daemon.WaveListParams{{}}) {
			t.Fatalf("rm: %d %s removed %+v listed %+v", code, errs, stub.removed, stub.listed)
		}
		stub = &savedStub{t: t, list: savedList}
		if code, out, errs := stub.run("wave", "start", "solo", "--yes"); code != ExitFailure || !reflect.DeepEqual(stub.listed, []daemon.WaveListParams{{}}) || !strings.Contains(out, "skipped S3: retired") {
			t.Fatalf("start: %d %s %s listed %+v", code, out, errs, stub.listed)
		}
		// The saved wave supplies the project of the started items.
		stub = &savedStub{t: t, list: `{"waves":[{"project":"q","name":"only-q","items":[{"item":"Q1","status":"open","title":"Quick"}]}]}`}
		if code, out, errs := stub.run("wave", "start", "only-q", "--yes", "--well-specified", "y", "--split", "n"); code != ExitOK || len(stub.started) != 1 || stub.started[0].Items[0].Project != "q" || stub.started[0].Name != "only-q" {
			t.Fatalf("start: %d %s %s %+v", code, out, errs, stub.started)
		}
	})
	t.Run("an ambiguous name needs --project", func(t *testing.T) {
		for _, args := range [][]string{{"wave", "rm", "pair-a"}, {"wave", "start", "pair-a", "--yes"}} {
			stub := &savedStub{t: t, list: savedList}
			code, _, errs := stub.run(args...)
			if code != ExitUsage || !strings.Contains(errs, "pair-a") || !strings.Contains(errs, "p, q") || !strings.Contains(errs, "--project") {
				t.Fatalf("%v: %d %s", args, code, errs)
			}
			if len(stub.removed) != 0 || stub.has(daemon.MethodWaveStart) || stub.has(daemon.MethodRunPlan) {
				t.Fatalf("%v: acted on an ambiguous name: %v", args, stub.calls)
			}
		}
		// With --project it is not ambiguous.
		stub := &savedStub{t: t, list: savedList}
		if code, _, errs := stub.run("wave", "rm", "--project", "q", "pair-a"); code != ExitOK || !reflect.DeepEqual(stub.removed, []daemon.WaveRmParams{{Project: "q", Name: "pair-a"}}) {
			t.Fatalf("rm --project: %d %s %+v", code, errs, stub.removed)
		}
		stub = &savedStub{t: t, list: savedList}
		if code, _, errs := stub.run("wave", "start", "--project", "q", "pair-a", "--yes", "--well-specified", "y", "--split", "n"); code != ExitOK || len(stub.started) != 1 || stub.started[0].Items[0].Item != "Q1" {
			t.Fatalf("start --project: %d %s %+v", code, errs, stub.started)
		}
	})
	t.Run("an unknown name is not found", func(t *testing.T) {
		stub := &savedStub{t: t, list: savedList}
		code, _, errs := stub.run("wave", "rm", "nope")
		if code != ExitFailure || !strings.Contains(errs, `no saved wave "nope"`) || len(stub.removed) != 0 {
			t.Fatalf("%d %s", code, errs)
		}
	})
	t.Run("the typed form takes no --project", func(t *testing.T) {
		stub := &savedStub{t: t, list: savedList}
		code, _, errs := stub.run("wave", "start", "--project", "p", "p", "S1", "--yes")
		if code != ExitUsage || !strings.Contains(errs, "--project") || len(stub.calls) != 0 {
			t.Fatalf("%d %s calls %v", code, errs, stub.calls)
		}
	})
}

func TestConfirmCancelledStartsOnANewLineWhenInputEndsWithoutNewline(t *testing.T) {
	for _, wave := range []bool{false, true} {
		check := `{"items":[{"item":"S1","ready":true},{"item":"S2","ready":true}]}`
		var code int
		var out, errs string
		label := "slice"
		if wave {
			label = "wave"
			code, out, errs = runWaveStart(t, &specStub{t: t, check: check}, "n")
		} else {
			code, out, errs, _ = singleSpecRun(t, "n", check, nil)
		}
		if code != 0 || !strings.Contains(out, "Start this "+label+"? [y/N]: \ncancelled") {
			t.Errorf("wave=%v code %d out %q err %s", wave, code, out, errs)
		}
	}
}
