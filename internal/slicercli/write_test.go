// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const wt = "/projects/ork/.worktrees/S04"

type memSink struct{ writes []runrec.Write }

func (m *memSink) AddWrite(w runrec.Write) error { m.writes = append(m.writes, w); return nil }

func writer() (runrec.Writer, *memSink) {
	sink := &memSink{}
	return runrec.Writer{
		Sink:    sink,
		Sleep:   func(ctx context.Context, d time.Duration) error { return ctx.Err() },
		Backoff: []time.Duration{time.Millisecond, time.Millisecond},
	}, sink
}

func rawRoute(args []string, file string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: args, File: recorded(file)}
}

func TestContractReadsTheJSONContractString(t *testing.T) {
	c, fake := client(t, route("relay-v11.json", "ai", "relay", "--json", "--root", root))
	got, err := c.Contract(context.Background(), root)
	if err != nil || got != "relay v11" {
		t.Fatalf("%q, %v", got, err)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"ai", "relay", "--json", "--root", root}) {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestSetRelayPinRunsConfigWithRoot(t *testing.T) {
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: []string{"config", "relay_contract", "11", "--root", root}, File: recorded("config-relay-contract-set.txt")})
	if err := c.SetRelayPin(context.Background(), root, 11); err != nil {
		t.Fatal(err)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"config", "relay_contract", "11", "--root", root}) {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestUnsetRelayPinRunsConfigUnsetWithRoot(t *testing.T) {
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: []string{"config", "relay_contract", "--unset", "--root", root}, File: recorded("config-relay-contract-unset.txt")})
	if err := c.UnsetRelayPin(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"config", "relay_contract", "--unset", "--root", root}) {
		t.Fatalf("calls: %+v", calls)
	}
}

// Without --json, slicer prints a refusal on stderr only, exit 2 (recorded).
func TestSetRelayPinReturnsSlicersMessage(t *testing.T) {
	msg, err := os.ReadFile(recorded("error-bad-config-value.txt"))
	if err != nil {
		t.Fatal(err)
	}
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"config", "relay_contract", "11", "--root", root}, Stderr: string(msg), Exit: 2})
	err = c.SetRelayPin(context.Background(), root, 11)
	var exit *clirun.ExitError
	if !errors.As(err, &exit) || exit.Result.ExitCode != 2 || !strings.Contains(err.Error(), "relay_contract: 'abc' is not a whole number") {
		t.Fatalf("%v", err)
	}
}

func TestShowRawReturnsStdoutAsPrinted(t *testing.T) {
	c, _ := client(t, route("show-S02.json", "show", "S02", "--json", "--lean", "--root", root))
	got, err := c.ShowRaw(context.Background(), root, "S02")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(recorded("show-S02.json"))
	if string(got) != string(want) {
		t.Fatalf("raw output differs: %d bytes, want %d", len(got), len(want))
	}
}

func TestConfigReturnsTheValue(t *testing.T) {
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"config", "implement_finish", "--json", "--root", root}, Stdout: `{"key":"implement_finish","value":"done"}`})
	got, err := c.Config(context.Background(), root, "implement_finish")
	if err != nil || got != "done" {
		t.Fatalf("%q, %v", got, err)
	}
}

func TestCheckGreenAndRed(t *testing.T) {
	args := []string{"check", "--json", "--root", root}
	c, _ := client(t, rawRoute(args, "check-ok.json"))
	res, err := c.Check(context.Background(), root)
	if err != nil || !res.OK || len(res.StaleRender) != 0 || res.Output == "" {
		t.Fatalf("green: %+v, %v", res, err)
	}

	red := clitest.Route{Tool: "slicer", Args: args, File: recorded("check-stale.json"), Exit: 1}
	c, _ = client(t, red)
	res, err = c.Check(context.Background(), root)
	if err != nil {
		t.Fatalf("a failed check is a result, not an error: %v", err)
	}
	if res.OK || !reflect.DeepEqual(res.StaleRender, []string{"ROADMAP.html", "ROADMAP.md"}) || !strings.Contains(res.Output, "stale_render") {
		t.Fatalf("red: %+v", res)
	}
}

func TestCheckReadsOrphanRenderAndProblems(t *testing.T) {
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"check", "--json", "--root", root}, Exit: 1,
		Stdout: `{"ok":false,"stale_render":[],"orphan_render":["S99.md"],"stale_sync":[],"problems":["s443 / Why: unknown flag --stdin in x"],"warnings":[]}`})
	res, err := c.Check(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !reflect.DeepEqual(res.OrphanRender, []string{"S99.md"}) || len(res.Problems) != 1 || string(res.Problems[0]) != `"s443 / Why: unknown flag --stdin in x"` {
		t.Fatalf("red: %+v", res)
	}
}

func TestCheckToolErrorIsAnError(t *testing.T) {
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"check", "--json", "--root", root}, Stdout: `{"error":{"code":"state","message":"no .slicer/","command":"check"}}`, Exit: 3})
	res, err := c.Check(context.Background(), root)
	var te *clirun.ToolError
	if res != nil || !errors.As(err, &te) || te.Code != "state" {
		t.Fatalf("%+v, %v", res, err)
	}
}

func TestRecommendRoles(t *testing.T) {
	c, _ := client(t, rawRoute([]string{"relay-roles", "recommend", "S04", "--well-specified", "yes", "--split", "no", "--json", "--root", root}, "relay-roles-recommend.json"))
	rec, err := c.RecommendRoles(context.Background(), root, "S04", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID != "S01" || !rec.WellSpecified || rec.Split || !reflect.DeepEqual(rec.Unmatched, []string{"architect"}) {
		t.Fatalf("recommendation: %+v", rec)
	}
	byRole := map[string]RoleChoice{}
	for _, r := range rec.Roles {
		byRole[r.Role] = r
	}
	impl := byRole["implementer"]
	if impl.Row == nil || impl.Row.ID != "r3" || impl.Row.Vendor != "Claude" || impl.Row.Model != "sonnet" || impl.Row.Reasoning != "high" || impl.SelectedBy != "matrix_order" {
		t.Fatalf("implementer: %+v", impl)
	}
	if len(impl.Alternatives) != 1 || impl.Alternatives[0].ID != "r9" {
		t.Fatalf("alternatives: %+v", impl.Alternatives)
	}
	if arch := byRole["architect"]; arch.Row != nil || arch.SelectedBy != "unmatched" {
		t.Fatalf("architect: %+v", arch)
	}
}

func TestRecommendRolesPassesNoAndYes(t *testing.T) {
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend", "S04"}, Prefix: true, Stdout: `{"id":"S04","roles":[]}`})
	if _, err := c.RecommendRoles(context.Background(), root, "S04", false, true); err != nil {
		t.Fatal(err)
	}
	want := []string{"relay-roles", "recommend", "S04", "--well-specified", "no", "--split", "yes", "--json", "--root", root}
	if got := fake.CallsTo("slicer")[0].Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("args %v, want %v", got, want)
	}
}

func TestRelayBeginRecordsTheWriteAndReturnsTheRun(t *testing.T) {
	c, _ := client(t, rawRoute([]string{"relay-run", "begin", "S04", "--worktree", wt, "--json", "--root", wt}, "relay-run-begin.json"))
	w, sink := writer()
	run, err := c.RelayBegin(context.Background(), w, wt, wt, "S04")
	if err != nil || run.ID != "run-20261019-132634-4142" || run.State != "active" {
		t.Fatalf("%+v, %v", run, err)
	}
	want := []string{"relay-run", "begin", "S04", "--worktree", wt, "--json"}
	if len(sink.writes) != 1 || !reflect.DeepEqual(sink.writes[0].Argv, want) || sink.writes[0].Tool != "slicer" || sink.writes[0].Root != wt {
		t.Fatalf("writes: %+v", sink.writes)
	}
}

func TestRelayStepAddsOrUpdatesAStep(t *testing.T) {
	// A new step: only the fields that are set are passed.
	orch := []string{"relay-run", "step", "run-1", "--role", "orchestrator", "--agent", "ork", "--phase", "start", "--task", "S04 orchestrator", "--json"}
	full := []string{"relay-run", "step", "run-1", "--role", "implementer", "--agent", "claude", "--vendor", "claude", "--model", "sonnet", "--reasoning", "high", "--parent", "1", "--phase", "start", "--task", "S04 implementer", "--json"}
	update := []string{"relay-run", "step", "run-1", "--step", "2", "--phase", "done", "--json"}
	verdict := []string{"relay-run", "step", "run-1", "--step", "2", "--phase", "done", "--verdict", "fail", "--json"}
	c, fake := client(t,
		rawRoute(append(append([]string{}, orch...), "--root", wt), "relay-run-step.json"),
		rawRoute(append(append([]string{}, full...), "--root", wt), "relay-run-step.json"),
		rawRoute(append(append([]string{}, update...), "--root", wt), "relay-run-step.json"),
		rawRoute(append(append([]string{}, verdict...), "--root", wt), "relay-run-step.json"),
	)
	w, sink := writer()
	ctx := context.Background()
	if _, err := c.RelayStep(ctx, w, wt, "run-1", StepSpec{Role: "orchestrator", Agent: "ork", Phase: "start", Task: "S04 orchestrator"}); err != nil {
		t.Fatalf("orchestrator step: %v", err)
	}
	step, err := c.RelayStep(ctx, w, wt, "run-1", StepSpec{Role: "implementer", Agent: "claude", Vendor: "claude", Model: "sonnet", Reasoning: "high", Parent: "1", Phase: "start", Task: "S04 implementer"})
	if err != nil || step.ID != "2" || step.Parent != "1" || step.Role != "implementer" {
		t.Fatalf("%+v, %v", step, err)
	}
	if _, err := c.RelayStep(ctx, w, wt, "run-1", StepSpec{Step: "2", Phase: "done"}); err != nil {
		t.Fatalf("update step: %v", err)
	}
	if _, err := c.RelayStep(ctx, w, wt, "run-1", StepSpec{Step: "2", Phase: "done", Verdict: "fail"}); err != nil {
		t.Fatalf("verdict step: %v", err)
	}
	if len(fake.CallsTo("slicer")) != 4 || len(sink.writes) != 4 {
		t.Fatalf("calls %d, writes %d", len(fake.CallsTo("slicer")), len(sink.writes))
	}
	for i, argv := range [][]string{orch, full, update, verdict} {
		if !reflect.DeepEqual(sink.writes[i].Argv, argv) {
			t.Errorf("write %d argv %v, want %v", i, sink.writes[i].Argv, argv)
		}
	}
}

func TestRelayEnd(t *testing.T) {
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: []string{"relay-run", "end"}, Prefix: true, File: recorded("relay-run-end.json")})
	w, sink := writer()
	ctx := context.Background()
	if err := c.RelayEnd(ctx, w, wt, "run-1", "pass", "done"); err != nil {
		t.Fatal(err)
	}
	if err := c.RelayEnd(ctx, w, wt, "run-1", "stopped", ""); err != nil {
		t.Fatal(err)
	}
	calls := fake.CallsTo("slicer")
	if want := []string{"relay-run", "end", "run-1", "--outcome", "pass", "--close-steps", "done", "--json", "--root", wt}; !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("first call %v, want %v", calls[0].Args, want)
	}
	if want := []string{"relay-run", "end", "run-1", "--outcome", "stopped", "--json", "--root", wt}; !reflect.DeepEqual(calls[1].Args, want) {
		t.Errorf("second call %v, want %v", calls[1].Args, want)
	}
	if len(sink.writes) != 2 {
		t.Fatalf("writes %+v", sink.writes)
	}
}

func TestNoteWritesTheReviewFailText(t *testing.T) {
	text := "Review FAIL: gate.go:10 handle nil"
	args := []string{"note", "S04", "--text", text, "--render", "--json", "--root", wt}
	c, _ := client(t, rawRoute(args, "note.json"))
	w, sink := writer()
	if err := c.Note(context.Background(), w, wt, "S04", text); err != nil {
		t.Fatal(err)
	}
	if want := []string{"note", "S04", "--text", text, "--render", "--json"}; len(sink.writes) != 1 || !reflect.DeepEqual(sink.writes[0].Argv, want) {
		t.Fatalf("writes %+v", sink.writes)
	}
}

func TestRenderAndLandAreLoggedWrites(t *testing.T) {
	c, _ := client(t,
		rawRoute([]string{"render", "--json", "--root", wt}, "note.json"),
		rawRoute([]string{"land", "S04", "--branch", "S04", "--note", "Landed S04", "--json", "--root", wt}, "note.json"),
	)
	w, sink := writer()
	if err := c.Render(context.Background(), w, wt); err != nil {
		t.Fatal(err)
	}
	if err := c.Land(context.Background(), w, wt, "S04", "S04", "Landed S04"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"render", "--json"}, {"land", "S04", "--branch", "S04", "--note", "Landed S04", "--json"}}
	if len(sink.writes) != 2 || !reflect.DeepEqual(sink.writes[0].Argv, want[0]) || !reflect.DeepEqual(sink.writes[1].Argv, want[1]) {
		t.Fatalf("writes %+v", sink.writes)
	}
}

func TestWritesRetryOnLockedAndLogEachAttempt(t *testing.T) {
	locked := clitest.Reply{Stdout: `{"error":{"code":"locked","message":"busy","command":"note"}}`, Exit: 3}
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: []string{"note", "S04", "--text", "x", "--render", "--json", "--root", wt},
		Seq: []clitest.Reply{locked, locked, {Stdout: `{"id":"S04","added":true}`}}})
	w, sink := writer()
	if err := c.Note(context.Background(), w, wt, "S04", "x"); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.CallsTo("slicer")); n != 3 {
		t.Fatalf("%d calls", n)
	}
	var got []int
	for _, e := range sink.writes {
		got = append(got, e.Exit*10+e.Retry)
	}
	if !reflect.DeepEqual(got, []int{30, 31, 2}) {
		t.Fatalf("exit/retry per attempt %v: %+v", got, sink.writes)
	}
	if sink.writes[0].Code != "locked" || sink.writes[2].Code != "" {
		t.Fatalf("codes: %+v", sink.writes)
	}
}

func TestAFailedWriteIsLoggedAndReturnsTheToolError(t *testing.T) {
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"relay-run", "end", "run-1", "--outcome", "pass", "--json", "--root", wt},
		Stdout: `{"error":{"code":"run_ended","message":"already ended","command":"relay-run"}}`, Exit: 2})
	w, sink := writer()
	err := c.RelayEnd(context.Background(), w, wt, "run-1", "pass", "")
	var te *clirun.ToolError
	if !errors.As(err, &te) || te.Code != "run_ended" {
		t.Fatalf("err %v", err)
	}
	if len(sink.writes) != 1 || sink.writes[0].Exit != 2 || sink.writes[0].Code != "run_ended" {
		t.Fatalf("writes %+v", sink.writes)
	}
}

func TestReadsAreNotWrites(t *testing.T) {
	c, _ := client(t,
		rawRoute([]string{"check", "--json", "--root", wt}, "check-ok.json"),
		rawRoute([]string{"relay-roles", "recommend", "S04", "--well-specified", "yes", "--split", "no", "--json", "--root", wt}, "relay-roles-recommend.json"),
	)
	// Reads take no Writer, so nothing can log them as a write.
	if _, err := c.Check(context.Background(), wt); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RecommendRoles(context.Background(), wt, "S04", true, false); err != nil {
		t.Fatal(err)
	}
}

func TestNoteRefusesEmptyText(t *testing.T) {
	for _, text := range []string{"", " \t\n"} {
		c, fake := client(t)
		w, _ := writer()
		if err := c.Note(context.Background(), w, wt, "S04", text); err == nil {
			t.Error("blank note accepted")
		}
		if calls := fake.CallsTo("slicer"); len(calls) != 0 {
			t.Fatalf("blank note called slicer: %v", calls)
		}
	}
}

func TestNoteRefusesEmptyItem(t *testing.T) {
	for _, id := range []string{"", " \t"} {
		c, fake := client(t)
		w, _ := writer()
		if err := c.Note(context.Background(), w, wt, id, "text"); err == nil {
			t.Error("blank item accepted")
		}
		if calls := fake.CallsTo("slicer"); len(calls) != 0 {
			t.Fatalf("blank item called slicer: %v", calls)
		}
	}
}

func TestRelayAnswerPassesOptionOrTextAndBy(t *testing.T) {
	for _, spec := range []RelayAnswerSpec{{Option: "Database", By: "Jon R"}, {Text: "keep files", By: "Jon R"}, {Text: "engine answer"}} {
		args := []string{"relay-run", "answer", "run-1", "q1"}
		if spec.Option != "" {
			args = append(args, "--option="+spec.Option)
		} else {
			args = append(args, "--text="+spec.Text)
		}
		if spec.By != "" {
			args = append(args, "--by="+spec.By)
		}
		args = append(args, "--json", "--root", root)
		c, f := client(t, clitest.Route{Tool: "slicer", Args: args})
		if err := c.RelayAnswer(context.Background(), runrec.Writer{}, root, "run-1", "q1", spec); err != nil {
			t.Fatal(err)
		}
		if calls := f.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, args) {
			t.Fatalf("calls: %+v", calls)
		}
	}
}

func TestRelayAnswerKeepsADashLeadingValueAttached(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spec  RelayAnswerSpec
		flags []string
	}{
		{"option", RelayAnswerSpec{Option: "-fast"}, []string{"--option=-fast"}},
		{"text", RelayAnswerSpec{Text: "-x"}, []string{"--text=-x"}},
		{"by", RelayAnswerSpec{Text: "ok", By: "-bob"}, []string{"--text=ok", "--by=-bob"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := append([]string{"relay-run", "answer", "run-1", "q1"}, tc.flags...)
			want = append(want, "--json", "--root", root)
			c, f := client(t, clitest.Route{Tool: "slicer", Args: want})
			err := c.RelayAnswer(context.Background(), runrec.Writer{}, root, "run-1", "q1", tc.spec)
			if calls := f.CallsTo("slicer"); err != nil || len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
				t.Fatalf("answer: %v; calls %+v, want %v", err, calls, want)
			}
		})
	}
}

// rowCall runs fn against a fake whose only route is want and returns the calls
// it made. The route matches exact argv, so a different argv is a failure of
// the call itself.
func rowCall(t *testing.T, want []string, stdout string, fn func(c Client) error) []clitest.Call {
	t.Helper()
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: want, Stdout: stdout})
	if err := fn(c); err != nil {
		t.Fatalf("call for %v: %v", want, err)
	}
	return fake.CallsTo("slicer")
}

func TestRelayRowSetPassesOnlyTheGivenFields(t *testing.T) {
	want := []string{"relay-roles", "set", "r5", "--model=opus", "--render", "--json", "--root", root}
	calls := rowCall(t, want, `{"id":"r5"}`, func(c Client) error {
		return c.RelayRowSet(context.Background(), runrec.Writer{}, root, "r5", RowFields{"model": "opus"})
	})
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("calls %+v, want one %v", calls, want)
	}
}

func TestRelayRowFieldsAreAttachedAndSorted(t *testing.T) {
	want := []string{"relay-roles", "set", "r5", "--agent=Codex", "--model=", "--note=-x", "--render", "--json", "--root", root}
	calls := rowCall(t, want, `{"id":"r5"}`, func(c Client) error {
		return c.RelayRowSet(context.Background(), runrec.Writer{}, root, "r5", RowFields{"note": "-x", "model": "", "agent": "Codex"})
	})
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("calls %+v, want one %v", calls, want)
	}
}

func TestRelayRowAddRemoveMoveRuleEditArgv(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		args []string
		run  func(c Client) error
	}{
		{"add", []string{"relay-roles", "add", "--role=reviewer", "--agent=Codex", "--render", "--json", "--root", root},
			func(c Client) error {
				_, err := c.RelayRowAdd(ctx, runrec.Writer{}, root, RowFields{"agent": "Codex", "role": "reviewer"})
				return err
			}},
		{"remove", []string{"relay-roles", "remove", "r5", "--render", "--json", "--root", root},
			func(c Client) error { return c.RelayRowRemove(ctx, runrec.Writer{}, root, "r5") }},
		{"move before", []string{"relay-roles", "move", "r5", "--before=r2", "--render", "--json", "--root", root},
			func(c Client) error { return c.RelayRowMove(ctx, runrec.Writer{}, root, "r5", "r2", false) }},
		{"move last", []string{"relay-roles", "move", "r5", "--last", "--render", "--json", "--root", root},
			func(c Client) error { return c.RelayRowMove(ctx, runrec.Writer{}, root, "r5", "", true) }},
		{"rule-edit", []string{"relay-roles", "rule-edit", "u3", "- keep two words", "--render", "--json", "--root", root},
			func(c Client) error { return c.RelayRuleEdit(ctx, runrec.Writer{}, root, "u3", "- keep two words") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := rowCall(t, tc.args, `{"id":"r17"}`, tc.run)
			if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, tc.args) {
				t.Fatalf("calls %+v, want one %v", calls, tc.args)
			}
		})
	}
}

func TestRelayRowAddReturnsTheNewID(t *testing.T) {
	args := []string{"relay-roles", "add", "--role=reviewer", "--render", "--json", "--root", root}
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: args, Stdout: `{"id":"r17","role":"reviewer"}`})
	id, err := c.RelayRowAdd(context.Background(), runrec.Writer{}, root, RowFields{"role": "reviewer"})
	if err != nil || id != "r17" {
		t.Fatalf("id %q, %v", id, err)
	}
	c, _ = client(t, clitest.Route{Tool: "slicer", Args: args, Stdout: `{"role":"reviewer"}`})
	if id, err := c.RelayRowAdd(context.Background(), runrec.Writer{}, root, RowFields{"role": "reviewer"}); err == nil {
		t.Fatalf("output without an id gave %q", id)
	}
}

func TestRelayRowRefusalsMakeNoCall(t *testing.T) {
	ctx := context.Background()
	w := runrec.Writer{}
	for _, tc := range []struct {
		name string
		run  func(c Client) error
	}{
		{"set empty id", func(c Client) error { return c.RelayRowSet(ctx, w, root, "", RowFields{"model": "x"}) }},
		{"set blank id", func(c Client) error { return c.RelayRowSet(ctx, w, root, " \t", RowFields{"model": "x"}) }},
		{"remove blank id", func(c Client) error { return c.RelayRowRemove(ctx, w, root, " ") }},
		{"move blank id", func(c Client) error { return c.RelayRowMove(ctx, w, root, "", "r2", false) }},
		{"rule-edit blank id", func(c Client) error { return c.RelayRuleEdit(ctx, w, root, " ", "text") }},
		{"add without role", func(c Client) error {
			_, err := c.RelayRowAdd(ctx, w, root, RowFields{"agent": "Codex"})
			return err
		}},
		{"add with blank role", func(c Client) error {
			_, err := c.RelayRowAdd(ctx, w, root, RowFields{"role": " "})
			return err
		}},
		{"set without fields", func(c Client) error { return c.RelayRowSet(ctx, w, root, "r5", RowFields{}) }},
		{"set with nil fields", func(c Client) error { return c.RelayRowSet(ctx, w, root, "r5", nil) }},
		{"move with both", func(c Client) error { return c.RelayRowMove(ctx, w, root, "r5", "r2", true) }},
		{"move with neither", func(c Client) error { return c.RelayRowMove(ctx, w, root, "r5", "", false) }},
		{"set unknown field", func(c Client) error { return c.RelayRowSet(ctx, w, root, "r5", RowFields{"colour": "red"}) }},
		{"add unknown field", func(c Client) error {
			_, err := c.RelayRowAdd(ctx, w, root, RowFields{"role": "reviewer", "id": "r9"})
			return err
		}},
		{"rule-edit blank text", func(c Client) error { return c.RelayRuleEdit(ctx, w, root, "u3", " \n") }},
		{"rule-edit dash text", func(c Client) error { return c.RelayRuleEdit(ctx, w, root, "u3", "-x") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, fake := client(t)
			if err := tc.run(c); err == nil {
				t.Error("refusal expected")
			}
			if calls := fake.CallsTo("slicer"); len(calls) != 0 {
				t.Fatalf("a refusal called slicer: %+v", calls)
			}
		})
	}
	// A dash then a space is a positional to slicer's parser, so it goes through.
	args := []string{"relay-roles", "rule-edit", "u3", "- x", "--render", "--json", "--root", root}
	if calls := rowCall(t, args, `{}`, func(c Client) error { return c.RelayRuleEdit(ctx, w, root, "u3", "- x") }); len(calls) != 1 {
		t.Fatalf("calls %+v", calls)
	}
}

// RowArgs is the argv builder under the client calls and the daemon's command
// line: it refuses an unknown op, and fields on a verb that takes none.
func TestRowArgsRefusesAnUnknownOpAndFieldsOnOtherVerbs(t *testing.T) {
	f := RowFields{"model": "x"}
	for name, run := range map[string]func() ([]string, error){
		"unknown op":       func() ([]string, error) { return RowArgs("rule-add", "u1", nil, "", false, "x") },
		"fields on remove": func() ([]string, error) { return RowArgs("remove", "r5", f, "", false, "") },
		"fields on move":   func() ([]string, error) { return RowArgs("move", "r5", f, "", true, "") },
		"fields on rule":   func() ([]string, error) { return RowArgs("rule-edit", "u3", f, "", false, "x") },
	} {
		if args, err := run(); err == nil {
			t.Errorf("%s: got %v", name, args)
		}
	}
	got, err := RowArgs("set", "r5", f, "", false, "")
	if want := []string{"relay-roles", "set", "r5", "--model=x"}; err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("set argv %v, %v; want %v", got, err, want)
	}
}

func TestParkAndUnparkRunTheSlicerCommand(t *testing.T) {
	for _, verb := range []string{"park", "unpark"} {
		t.Run(verb, func(t *testing.T) {
			args := []string{verb, "S1", "--render", "--json", "--root", root}
			c, fake := client(t, clitest.Route{Tool: "slicer", Args: args, Stdout: `{}`})
			w, _ := writer()
			var err error
			if verb == "park" {
				err = c.Park(context.Background(), w, root, "S1")
			} else {
				err = c.Unpark(context.Background(), w, root, "S1")
			}
			if err != nil {
				t.Fatal(err)
			}
			if calls := fake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, args) {
				t.Fatalf("calls %v", calls)
			}
		})
	}
}
func TestMoveRunsBeforeAndAfter(t *testing.T) {
	for _, pos := range []string{"before", "after"} {
		t.Run(pos, func(t *testing.T) {
			args := []string{"move", "S1", "--" + pos, "S2", "--render", "--json", "--root", root}
			c, fake := client(t, clitest.Route{Tool: "slicer", Args: args, Stdout: `{}`})
			w, _ := writer()
			if err := c.Move(context.Background(), w, root, "S1", "S2", pos); err != nil {
				t.Fatal(err)
			}
			if calls := fake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, args) {
				t.Fatalf("calls %v", calls)
			}
		})
	}
}
func TestEditWrappersRefuseEmptyArguments(t *testing.T) {
	c, fake := client(t)
	w, _ := writer()
	for _, id := range []string{"", " "} {
		if err := c.Park(context.Background(), w, root, id); err == nil {
			t.Error("park accepted blank item")
		}
		if err := c.Unpark(context.Background(), w, root, id); err == nil {
			t.Error("unpark accepted blank item")
		}
	}
	for _, tc := range []struct{ id, other, pos string }{{"", "S2", "before"}, {" ", "S2", "after"}, {"S1", "", "before"}, {"S1", " ", "after"}, {"S1", "S2", "to"}} {
		if err := c.Move(context.Background(), w, root, tc.id, tc.other, tc.pos); err == nil {
			t.Errorf("accepted %+v", tc)
		}
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 0 {
		t.Fatalf("invalid edits called slicer: %v", calls)
	}
}
