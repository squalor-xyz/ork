// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func adjAnswerRun(t *testing.T, check string, decideError bool, keys ...string) (*Daemon, *runrec.Store, string) {
	t.Helper()
	routes := []clitest.Route{
		{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: check},
		{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "42\n"},
		{Tool: "adj", Args: []string{"decide"}, Prefix: true, Stdout: "42\n"},
	}
	if decideError {
		routes[2].Seq = []clitest.Reply{{Stderr: `{"error":{"code":"io","message":"disk full verbatim"}}`, Exit: 3}, {Stdout: "42\n"}}
	}
	runner := roundRunner(func(_ context.Context, r engine.Request) (engine.Result, error) {
		report := "Tests: 1 passed, 0 failed\n## Owner needed\nnone\n"
		if r.Role == engine.RoleImplementer && !strings.Contains(r.Brief, "use stdlib") {
			report = "Tests: 1 passed, 0 failed\n## Owner needed\nDecision key: storage.engine\n- Which library?\nOptions here.\n"
		}
		if len(keys) > 0 && keys[0] == "structured" && strings.Contains(report, "Options here.") {
			report = "Tests: 1 passed, 0 failed\n## Owner needed\nQuestion: Which library?\nOptions:\n1. local: use stdlib\n2. external: add a library\nRecommend: 1 - no dependency\nDecision key: storage.engine\n"
		}
		if len(keys) > 0 && keys[0] == "" {
			report = strings.ReplaceAll(report, "Decision key: storage.engine\n", "")
		}
		if r.Role == engine.RoleReviewer {
			report = "(A): nothing\n(B): nothing\n(D): nothing\n## Owner needed\nnone\n"
		}
		if r.Role == engine.RoleGate {
			report = "VERDICT: PASS\n" + report
		}
		return engine.Result{Exit: engine.ExitOK, Session: "s", Report: report}, nil
	})
	d, store, _ := runDaemon(t, runner, routes...)
	d.runs.deps.Adj = adjcli.Client{Bin: lastFake.Bin("adj")}
	d.runs.deps.Now = func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }
	b := request(t, d, MethodRunStart, `{"project":"p","item":"S1"}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, rec.ID, runrec.StatePaused)
	waitIdle(t, d, rec.ID)
	return d, store, rec.ID
}

func answerAdj(t *testing.T, d *Daemon, id string, on bool, by string) (any, error) {
	t.Helper()
	b, _ := json.Marshal(RunAction{Run: id, Text: "use stdlib", Adj: on, By: by})
	return d.handle(context.Background(), MethodRunAnswer, b)
}

func TestAnswerFilesAdjAskAndDecideWhenToggled(t *testing.T) {
	for _, check := range []string{`{"result":"not found"}`, `{"closed":[{"id":9,"key":"storage.engine","status":"rejected"}]}`, `{"decided":[{"id":9,"key":"storage.engine-extra"}]}`} {
		t.Run(check, func(t *testing.T) {
			d, store, id := adjAnswerRun(t, check, false)
			rec := waitState(t, store, id, runrec.StatePaused)
			if rec.Pending.Key != "storage.engine" || rec.Questions[0].Key != "storage.engine" {
				t.Fatalf("key lost: %+v", rec)
			}
			runs := d.engineRuns("p")
			if runs[0].Key != "storage.engine" {
				t.Fatalf("snapshot key: %+v", runs)
			}
			if reply, err := answerAdj(t, d, id, true, "owner"); err != nil {
				t.Fatal(err)
			} else if b, _ := json.Marshal(reply); !strings.Contains(string(b), "decided in adj as #42") {
				t.Fatalf("notice: %s", b)
			}
			waitState(t, store, id, runrec.StateAwaitingApproval)
			waitIdle(t, d, id)
			calls := lastFake.CallsTo("adj")
			if len(calls) != 3 {
				t.Fatalf("calls: %+v", calls)
			}
			want := [][]string{
				{"check", "storage.engine", "--json", "--root", d.runs.runs[id].engine.AdjRoot()},
				{"ask", "Which library?", "--key", "storage.engine", "--ref", "S1", "--context", "Decision key: storage.engine\n- Which library?\nOptions here.", "--by", "owner", "--root", d.runs.runs[id].engine.AdjRoot()},
				{"decide", "42", "--text", "use stdlib", "--why", "Answered in ork on 2026-10-25: Which library?", "--by", "owner", "--root", d.runs.runs[id].engine.AdjRoot()},
			}
			for i, c := range calls {
				if fmt.Sprint(c.Args) != fmt.Sprint(want[i]) {
					t.Errorf("call %d: %q, want %q", i, c.Args, want[i])
				}
			}
		})
	}
}

func TestAnswerSkipsAskWhenKeyIsOpenOrDecided(t *testing.T) {
	d, store, id := adjAnswerRun(t, `{"decided":[{"id":99,"key":"storage.engine-extra"}],"open":[{"id":21,"key":"storage.engine","status":"open"}]}`, false)
	if _, err := answerAdj(t, d, id, true, "owner"); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, id, runrec.StateAwaitingApproval)
	waitIdle(t, d, id)
	calls := lastFake.CallsTo("adj")
	if len(calls) != 2 || calls[1].Args[0] != "decide" || calls[1].Args[1] != "21" {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestAnswerAlreadyDecidedResumesWithNotice(t *testing.T) {
	d, store, id := adjAnswerRun(t, `{"decided":[{"id":21,"key":"storage.engine","status":"decided"}]}`, false)
	v, err := answerAdj(t, d, id, true, "owner")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), "storage.engine already decided as #21; not changed") {
		t.Fatalf("reply: %s", b)
	}
	waitState(t, store, id, runrec.StateAwaitingApproval)
	waitIdle(t, d, id)
	if calls := lastFake.CallsTo("adj"); len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestAnswerWithToggleOffRecordsNothingInAdj(t *testing.T) {
	d, store, id := adjAnswerRun(t, `{}`, false)
	if _, err := answerAdj(t, d, id, false, ""); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, id, runrec.StateAwaitingApproval)
	waitIdle(t, d, id)
	if calls := lastFake.CallsTo("adj"); len(calls) != 0 {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestAnswerWithoutAdjDecidedByIsRefused(t *testing.T) {
	d, store, id := adjAnswerRun(t, `{}`, false)
	if _, err := answerAdj(t, d, id, true, ""); err == nil || !strings.Contains(err.Error(), "ADJ_DECIDED_BY") {
		t.Fatalf("error: %v", err)
	}
	rec := waitState(t, store, id, runrec.StatePaused)
	waitIdle(t, d, id)
	if rec.Questions[0].Answer != "" || len(lastFake.CallsTo("adj")) != 0 {
		t.Fatal("refused answer filed or resumed")
	}
	if rows := d.engineRuns("p"); len(rows) != 1 || rows[0].Error != "" {
		t.Fatalf("plain refusal marked run failed: %+v", rows)
	}
}

func TestAnswerAdjErrorKeepsTextAndDoesNotResume(t *testing.T) {
	d, store, id := adjAnswerRun(t, `{"open":[{"id":42,"key":"storage.engine"}]}`, true)
	_, err := answerAdj(t, d, id, true, "owner")
	if e := ipc.ErrorOf(err); e == nil || e.Code != "io" || e.Message != "disk full verbatim" {
		t.Fatalf("error: %v", err)
	}
	waitIdle(t, d, id)
	rec := waitState(t, store, id, runrec.StatePaused)
	if rec.Questions[0].Answer != "" {
		t.Fatal("resumed on adj failure")
	}
	if _, err := answerAdj(t, d, id, true, "owner"); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, id, runrec.StateAwaitingApproval)
	waitIdle(t, d, id)
}

func TestAdjCallsLandInWrites(t *testing.T) {
	d, store, id := adjAnswerRun(t, `{}`, false)
	if _, err := answerAdj(t, d, id, true, "owner"); err != nil {
		t.Fatal(err)
	}
	rec := waitState(t, store, id, runrec.StateAwaitingApproval)
	waitIdle(t, d, id)
	var verbs []string
	for _, w := range rec.Writes {
		if w.Tool == "adj" {
			if w.Root != d.runs.runs[id].engine.AdjRoot() || w.Exit != 0 {
				t.Fatalf("write: %+v", w)
			}
			verbs = append(verbs, w.Argv[0])
		}
	}
	if fmt.Sprint(verbs) != "[check ask decide]" {
		t.Fatalf("writes: %v", verbs)
	}
}

func TestAnswerWithoutKeyNeverFiles(t *testing.T) {
	d, store, id := adjAnswerRun(t, `{}`, false, "")
	if _, err := answerAdj(t, d, id, true, "owner"); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, id, runrec.StateAwaitingApproval)
	waitIdle(t, d, id)
	if calls := lastFake.CallsTo("adj"); len(calls) != 0 {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestAnswerRetryReusesAskAfterDecideFailure(t *testing.T) {
	d, store, id := adjAnswerRun(t, `{}`, true)
	// The first check sees no record. The second sees the ask left open.
	fake := clitest.Install(t,
		clitest.Route{Tool: "adj", Args: []string{"check"}, Prefix: true, Seq: []clitest.Reply{{Stdout: `{}`}, {Stdout: `{"open":[{"id":42,"key":"storage.engine"}]}`}}},
		clitest.Route{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "42\n"},
		clitest.Route{Tool: "adj", Args: []string{"decide"}, Prefix: true, Seq: []clitest.Reply{{Stderr: `{"error":{"code":"io","message":"disk full verbatim"}}`, Exit: 3}, {Stdout: "42\n"}}},
		clitest.Route{Tool: "slicer", Prefix: true, Stdout: `{"step":{"id":"1"},"ok":true}`},
		clitest.Route{Tool: "git", Prefix: true})
	// The engine already owns a copy of dependencies; use the same fake binary path.
	// All clitest binaries consult the current routes environment.
	if _, err := answerAdj(t, d, id, true, "owner"); err == nil {
		t.Fatal("expected decide error")
	}
	waitIdle(t, d, id)
	rec := waitState(t, store, id, runrec.StatePaused)
	if rec.Questions[0].Answer != "" {
		t.Fatal("resumed after failed decide")
	}
	if _, err := answerAdj(t, d, id, true, "owner"); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, id, runrec.StateAwaitingApproval)
	waitIdle(t, d, id)
	var verbs []string
	for _, c := range fake.CallsTo("adj") {
		verbs = append(verbs, c.Args[0])
	}
	if fmt.Sprint(verbs) != "[check ask decide check decide]" {
		t.Fatalf("calls: %v", verbs)
	}
}

func TestAnswerCheckAndAskErrorsDoNotResume(t *testing.T) {
	for _, verb := range []string{"check", "ask"} {
		t.Run(verb, func(t *testing.T) {
			d, store, id := adjAnswerRun(t, `{}`, false)
			fake := clitest.Install(t,
				clitest.Route{Tool: "adj", Args: []string{verb}, Prefix: true, Stderr: `{"error":{"code":"io","message":"exact failure"}}`, Exit: 3},
				clitest.Route{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: `{}`})
			if _, err := answerAdj(t, d, id, true, "owner"); ipc.ErrorOf(err) == nil || ipc.ErrorOf(err).Code != "io" || ipc.ErrorOf(err).Message != "exact failure" {
				t.Fatalf("error: %v", err)
			}
			waitIdle(t, d, id)
			rec := waitState(t, store, id, runrec.StatePaused)
			if rec.Questions[0].Answer != "" {
				t.Fatal("resumed on adj error")
			}
			calls := fake.CallsTo("adj")
			if calls[len(calls)-1].Args[0] != verb {
				t.Fatalf("calls: %+v", calls)
			}
			last := rec.Writes[len(rec.Writes)-1]
			if last.Tool != "adj" || last.Code != "io" || last.Exit != 3 {
				t.Fatalf("write: %+v", last)
			}
		})
	}
}

func TestAnswerPlainAdjErrorsDoNotLeakOrResume(t *testing.T) {
	for _, verb := range []string{"ask", "decide"} {
		t.Run(verb, func(t *testing.T) {
			d, store, id := adjAnswerRun(t, `{}`, false)
			stderr := "adj " + verb + ": record 5 is not open"
			fake := clitest.Install(t,
				clitest.Route{Tool: "adj", Args: []string{verb}, Prefix: true, Stderr: stderr + "\n", Exit: 1},
				clitest.Route{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: `{}`},
				clitest.Route{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "5\n"})
			_, err := answerAdj(t, d, id, true, "owner")
			wire := ipc.ErrorOf(err)
			if wire == nil || wire.Code != "internal" || wire.Message != "adj: exit 1: "+stderr {
				t.Fatalf("wire error: %+v", wire)
			}
			waitIdle(t, d, id)
			rec := waitState(t, store, id, runrec.StatePaused)
			if rec.Questions[0].Answer != "" {
				t.Fatal("resumed after plain adj error")
			}
			for _, s := range []string{wire.Message, d.engineRuns("p")[0].Error} {
				if strings.Contains(s, "--context") || strings.Contains(s, "--text") || strings.Contains(s, "use stdlib") || strings.Contains(s, "Options here") {
					t.Fatalf("argv leaked: %s", s)
				}
			}
			calls := fake.CallsTo("adj")
			if calls[len(calls)-1].Args[0] != verb {
				t.Fatalf("calls: %+v", calls)
			}
		})
	}
}

func TestAnswerWithoutAdjDecidedByIsRefusedWithoutKey(t *testing.T) {
	d, store, id := adjAnswerRun(t, `{}`, false, "")
	if _, err := answerAdj(t, d, id, true, ""); err == nil || !strings.Contains(err.Error(), "ADJ_DECIDED_BY") {
		t.Fatalf("error: %v", err)
	}
	waitIdle(t, d, id)
	rec := waitState(t, store, id, runrec.StatePaused)
	if rec.Questions[0].Answer != "" || d.engineRuns("p")[0].Error != "" {
		t.Fatal("refused answer changed run")
	}
}

func TestStructuredOptionFilesAdjTitleAndDecisionText(t *testing.T) {
	d, store, id := adjAnswerRun(t, `{"result":"not found"}`, false, "structured")
	b, _ := json.Marshal(RunAction{Run: id, Text: "local: use stdlib", Adj: true, By: "owner"})
	if _, err := d.handle(context.Background(), MethodRunAnswer, b); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, id, runrec.StateAwaitingApproval)
	waitIdle(t, d, id)
	calls := lastFake.CallsTo("adj")
	if len(calls) != 3 || calls[1].Args[1] != "Which library?" || calls[2].Args[3] != "local: use stdlib" {
		t.Fatalf("calls: %+v", calls)
	}
}
