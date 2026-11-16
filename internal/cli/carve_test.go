// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// S359 (*proposed*): the spec check carves slivers for a local implementer, so
// the plan the owner saw before it lists none. ork run shows the plan that does
// list them and starts with that plan, so the owner approves the chain once.
func TestRunShowsTheChainOfTheSliversTheSpecCheckFiledAndStartsWithThatPlan(t *testing.T) {
	const noSlivers = `{"project":{"name":"p"},"item":"S1","check_command":"go test ./...","carve":true}`
	const chain = `{"project":{"name":"p"},"item":"S1a","parent":"S1","check_command":"go test ./...","slivers":[{"item":"S1a","index":1,"count":2},{"item":"S1b","index":2,"count":2}]}`
	var plans, calls []string
	var started daemon.RunParams
	app := &App{Input: strings.NewReader("yes\n"), Call: func(_ context.Context, method string, params, result any) error {
		calls = append(calls, method)
		switch method {
		case daemon.MethodItemShow:
			return json.Unmarshal([]byte(`{"detail":{"slice":{"sections":[]}}}`), result)
		case daemon.MethodProjectsList:
			return json.Unmarshal([]byte(`{"projects":[]}`), result)
		case daemon.MethodRunPlan:
			plan := noSlivers
			if len(plans) > 0 {
				plan = chain
			}
			plans = append(plans, plan)
			return json.Unmarshal([]byte(plan), result)
		case daemon.MethodSpecCheck:
			return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true,"carved":["S1a","S1b"]}]}`), result)
		case daemon.MethodRunStart:
			started = params.(daemon.RunParams)
			return json.Unmarshal([]byte(`{"id":"r1","state":"setup"}`), result)
		}
		t.Fatalf("method %s", method)
		return nil
	}}
	var stdout, stderr bytes.Buffer
	if code := app.Run(context.Background(), []string{"run", "p", "S1"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("code %d, out %q, err %q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"carved S1 into 2 slivers: S1a, S1b", "slivers: 2, one landing of S1", "sliver 1 of 2: S1a", "sliver 2 of 2: S1b"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "carved S1") > strings.Index(out, "Start this slice?") {
		t.Errorf("the chain must be shown before the one approval:\n%s", out)
	}
	if len(plans) != 2 || started.Expected == nil || len(started.Expected.Slivers) != 2 {
		t.Fatalf("the start compares the plan with the slivers: plans %d, expected %+v", len(plans), started.Expected)
	}
	order := strings.Join(calls, " ")
	if !strings.Contains(order, "spec.check run.plan") || !strings.HasSuffix(order, "run.start") {
		t.Errorf("the plan is made again after the spec check, then the start: %s", order)
	}
}

// Answers that make the item Ready carve it: the answer call carries the
// lineup, and the chain it reports is the plan the owner confirms.
func TestRunShowsTheChainOfSliversCarvedByTheOwnersAnswers(t *testing.T) {
	const chain = `{"project":{"name":"p"},"item":"S1a","parent":"S1","check_command":"go test ./...","slivers":[{"item":"S1a","index":1,"count":2},{"item":"S1b","index":2,"count":2}]}`
	var answers []daemon.SpecAnswerParams
	var started daemon.RunParams
	plans := 0
	app := &App{Input: strings.NewReader("2\ncustom flag\ny\n"), Call: func(_ context.Context, method string, params, result any) error {
		switch method {
		case daemon.MethodItemShow:
			return json.Unmarshal([]byte(`{"detail":{"slice":{"sections":[]}}}`), result)
		case daemon.MethodProjectsList:
			return json.Unmarshal([]byte(`{"projects":[]}`), result)
		case daemon.MethodRunPlan:
			plans++
			if plans > 1 {
				return json.Unmarshal([]byte(chain), result)
			}
			return json.Unmarshal([]byte(`{"project":{"name":"p"},"item":"S1","check_command":"go test ./...","carve":true}`), result)
		case daemon.MethodSpecCheck:
			return json.Unmarshal([]byte(specCheckTwoQuestions), result)
		case daemon.MethodSpecAnswer:
			answers = append(answers, params.(daemon.SpecAnswerParams))
			return json.Unmarshal([]byte(`{"item":"S1","ready":true,"carved":["S1a","S1b"]}`), result)
		case daemon.MethodRunStart:
			started = params.(daemon.RunParams)
			return json.Unmarshal([]byte(`{"id":"r1","state":"setup"}`), result)
		}
		t.Fatalf("method %s", method)
		return nil
	}}
	var stdout, stderr bytes.Buffer
	if code := app.Run(context.Background(), []string{"run", "p", "S1", "--implementer", "claude-local:qwen3.8-27b:medium"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("code %d, out %q, err %q", code, stdout.String(), stderr.String())
	}
	if len(answers) != 1 || len(answers[0].Lineup["implementer"]) != 1 || answers[0].Lineup["implementer"][0].Vendor != "claude-local" {
		t.Fatalf("the answer carries the lineup: %+v", answers)
	}
	if !strings.Contains(stdout.String(), "sliver 2 of 2: S1b") || plans != 2 || started.Expected == nil || len(started.Expected.Slivers) != 2 {
		t.Fatalf("plans %d, expected %+v:\n%s", plans, started.Expected, stdout.String())
	}
}

// --questions starts nothing, but the check it runs still files the slivers of a
// local slice, so it says so, for a run and for a wave.
func TestQuestionsListingSaysWhichSliversTheCheckFiled(t *testing.T) {
	for _, args := range [][]string{{"run", "p", "S1", "--questions"}, {"wave", "start", "p", "S1", "--questions"}} {
		t.Run(args[0], func(t *testing.T) {
			var calls []string
			app := &App{Call: func(_ context.Context, method string, _, result any) error {
				calls = append(calls, method)
				if method != daemon.MethodSpecCheck {
					t.Fatalf("method %s", method)
				}
				return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true,"carved":["S1a","S1b"]}]}`), result)
			}}
			var stdout, stderr bytes.Buffer
			if code := app.Run(context.Background(), args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("code %d, out %q, err %q", code, stdout.String(), stderr.String())
			}
			if out := stdout.String(); !strings.Contains(out, "carved S1 into 2 slivers: S1a, S1b") || !strings.Contains(out, "no open spec questions: S1") {
				t.Fatalf("the listing does not say what the check filed:\n%s", out)
			}
			if len(calls) != 1 {
				t.Fatalf("calls %v: a listing starts and plans nothing", calls)
			}
		})
	}
	// With --json the line goes to stderr and stdout stays one JSON value.
	app := &App{Call: func(_ context.Context, _ string, _, result any) error {
		return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true,"carved":["S1a","S1b"]}]}`), result)
	}}
	var stdout, stderr bytes.Buffer
	if code := app.Run(context.Background(), []string{"run", "p", "S1", "--questions", "--json"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("code %d, err %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "carved S1 into 2 slivers: S1a, S1b") || strings.Contains(stdout.String(), "carved S1") {
		t.Fatalf("json: out %q err %q", stdout.String(), stderr.String())
	}
}

// A check that carved nothing leaves the plan the owner already saw as it is.
func TestRunWithNothingCarvedPlansOnce(t *testing.T) {
	plans := 0
	var started daemon.RunParams
	app := &App{Input: strings.NewReader("yes\n"), Call: func(_ context.Context, method string, params, result any) error {
		switch method {
		case daemon.MethodItemShow:
			return json.Unmarshal([]byte(`{"detail":{"slice":{"sections":[]}}}`), result)
		case daemon.MethodProjectsList:
			return json.Unmarshal([]byte(`{"projects":[]}`), result)
		case daemon.MethodRunPlan:
			plans++
			return json.Unmarshal([]byte(`{"project":{"name":"p"},"item":"S1","check_command":"go test ./..."}`), result)
		case daemon.MethodSpecCheck:
			return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true}]}`), result)
		case daemon.MethodRunStart:
			started = params.(daemon.RunParams)
			return json.Unmarshal([]byte(`{"id":"r1","state":"setup"}`), result)
		}
		t.Fatalf("method %s", method)
		return nil
	}}
	var stdout, stderr bytes.Buffer
	if code := app.Run(context.Background(), []string{"run", "p", "S1"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("code %d, out %q, err %q", code, stdout.String(), stderr.String())
	}
	if plans != 1 || started.Expected == nil || started.Expected.Item != "S1" {
		t.Fatalf("plans %d, expected %+v", plans, started.Expected)
	}
}
