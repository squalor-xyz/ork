// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/daemon"
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
			plans, started := 0, false
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
