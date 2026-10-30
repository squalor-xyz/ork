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

func TestContractReturnsTheFirstLine(t *testing.T) {
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--root", root}, Stdout: "Contract: relay v7\n\n# Slice relay\nmore\n"})
	got, err := c.Contract(context.Background(), root)
	if err != nil || got != "Contract: relay v7" {
		t.Fatalf("%q, %v", got, err)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"ai", "relay", "--root", root}) {
		t.Fatalf("calls: %+v", calls)
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
			args = append(args, "--option", spec.Option)
		} else {
			args = append(args, "--text", spec.Text)
		}
		if spec.By != "" {
			args = append(args, "--by", spec.By)
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
