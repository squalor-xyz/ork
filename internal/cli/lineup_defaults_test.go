// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// Adj 189 (launch.well-specified-default, S260): run and wave start plan every
// item as well specified and not split, and ask neither. The flags override.

func TestRunPlansWithoutAskingWellSpecifiedOrSplit(t *testing.T) {
	// "n" answers the one confirmation; it is the only prompt left.
	r := runPrompted(t, "n\n")
	want := `{"project":"p","item":"S1","well_specified":true,"split":false}`
	if r.code != ExitOK || r.plan != want {
		t.Fatalf("code %d plan %s: %s %s", r.code, r.plan, r.out, r.errs)
	}
	for _, prompt := range []string{"Well specified?", "Split?"} {
		if strings.Contains(r.out+r.errs, prompt) {
			t.Errorf("output still asks %q: %s", prompt, r.out)
		}
	}
	if !strings.Contains(r.out, "Start this slice? [y/N]: ") {
		t.Errorf("the confirmation is gone: %s", r.out)
	}
}

func TestRunYesNeedsNoLineupInputs(t *testing.T) {
	r := runPrompted(t, "", "--yes")
	want := `{"project":"p","item":"S1","well_specified":true,"split":false}`
	if r.code != ExitOK || r.plan != want || !r.started {
		t.Fatalf("code %d plan %s started %t: %s %s", r.code, r.plan, r.started, r.out, r.errs)
	}
	// A bad value is still refused with --yes, naming the flags.
	for _, flags := range [][]string{{"--well-specified", "maybe"}, {"--split", "sure"}} {
		r := runPrompted(t, "", append([]string{"--yes"}, flags...)...)
		if r.code != ExitUsage || r.plan != "" || r.started || !strings.Contains(r.errs, "--well-specified y/n and --split y/n") {
			t.Errorf("%v: code %d plan %q: %s", flags, r.code, r.plan, r.errs)
		}
	}
}

func TestRunFlagsStillOverride(t *testing.T) {
	for _, tc := range []struct {
		flags []string
		want  string
	}{
		{[]string{"--well-specified", "n", "--split", "y"}, `{"project":"p","item":"S1","well_specified":false,"split":true}`},
		{[]string{"--well-specified", "N"}, `{"project":"p","item":"S1","well_specified":false,"split":false}`},
		{[]string{"--split", "YES"}, `{"project":"p","item":"S1","well_specified":true,"split":true}`},
	} {
		// Once with the confirmation prompt, once with --yes.
		for _, extra := range []struct {
			input string
			flags []string
		}{{"y\n", nil}, {"", []string{"--yes"}}} {
			r := runPrompted(t, extra.input, append(append([]string{}, tc.flags...), extra.flags...)...)
			if r.code != ExitOK || r.plan != tc.want || !r.started {
				t.Errorf("%v %v: code %d plan %s started %t: %s", tc.flags, extra.flags, r.code, r.plan, r.started, r.errs)
			}
		}
	}
}

func TestRunYesStillRefusesNoCheckWithoutAskingAnything(t *testing.T) {
	// Adj 191: the default answers do not weaken the no-check refusal.
	code, out, errs, methods, starts := noCheckRun(t, "run", "p", "S1", "--yes")
	if code != ExitFailure || len(starts) != 0 || strings.Contains(out+errs, "Well specified?") || strings.Contains(out+errs, "Split?") {
		t.Fatalf("code %d starts %v: %s %s", code, starts, out, errs)
	}
	if !reflect.DeepEqual(methods, []string{daemon.MethodRunPlan}) {
		t.Fatalf("methods %v", methods)
	}
}

func TestWaveStartPlansEveryItemWithTheDefaults(t *testing.T) {
	var plans []daemon.RunParams
	var started daemon.WaveStartParams
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		switch method {
		case daemon.MethodRunPlan:
			p := params.(daemon.RunParams)
			plans = append(plans, p)
			*result.(*daemon.RunPlan) = daemon.RunPlan{CheckCommand: "go test ./...", Item: p.Item, Worktree: "/p/" + p.Item}
		case daemon.MethodSpecCheck:
			// S1 is not Ready and has no answer coming: the spec check holds it.
			return json.Unmarshal([]byte(`{"items":[{"item":"S2","ready":true},{"item":"S1","ready":false,"questions":[{"text":"Question?"}]}]}`), result)
		case daemon.MethodWaveStart:
			started = params.(daemon.WaveStartParams)
			return json.Unmarshal([]byte(`{"wave":"wave-1","items":[]}`), result)
		default:
			t.Fatalf("unexpected %s", method)
		}
		return nil
	}}
	code, out, errs := run(t, app, "wave", "start", "p", "S2", "S1", "--yes")
	if code != ExitOK {
		t.Fatalf("code %d: %s %s", code, out, errs)
	}
	if len(plans) != 2 {
		t.Fatalf("plans: %+v", plans)
	}
	for _, p := range plans {
		if !p.WellSpecified || p.Split {
			t.Errorf("%s planned with well %t split %t", p.Item, p.WellSpecified, p.Split)
		}
	}
	for _, prompt := range []string{"Well specified?", "Split?"} {
		if strings.Contains(out+errs, prompt) {
			t.Errorf("output still asks %q: %s", prompt, out)
		}
	}
	if len(started.Items) != 1 || started.Items[0].Item != "S2" || !strings.Contains(out, "held: S1 has 1 open spec question") {
		t.Fatalf("the not-Ready item is not held: %+v: %s", started, out)
	}
}

func TestWaveStartFlagsStillOverride(t *testing.T) {
	var plans []daemon.RunParams
	app := &App{Call: func(_ context.Context, method string, params, result any) error {
		switch method {
		case daemon.MethodRunPlan:
			p := params.(daemon.RunParams)
			plans = append(plans, p)
			*result.(*daemon.RunPlan) = daemon.RunPlan{CheckCommand: "go test ./...", Item: p.Item}
		case daemon.MethodSpecCheck:
			return json.Unmarshal([]byte(`{"items":[{"item":"S1","ready":true},{"item":"S2","ready":true}]}`), result)
		case daemon.MethodWaveStart:
			return json.Unmarshal([]byte(`{"wave":"wave-1","items":[]}`), result)
		default:
			t.Fatalf("unexpected %s", method)
		}
		return nil
	}}
	code, out, errs := run(t, app, "wave", "start", "p", "S1", "S2", "--yes", "--well-specified", "n", "--split", "y")
	if code != ExitOK || len(plans) != 2 {
		t.Fatalf("code %d plans %+v: %s %s", code, plans, out, errs)
	}
	for _, p := range plans {
		if p.WellSpecified || !p.Split {
			t.Errorf("%s planned with well %t split %t", p.Item, p.WellSpecified, p.Split)
		}
	}
}
