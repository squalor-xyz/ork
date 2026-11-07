// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func adjAnswerRun(t *testing.T, check string, decideError bool, keys ...string) (*Daemon, *runrec.Store, string) {
	t.Helper()
	return adjAnswerRunGated(t, nil, check, decideError, keys...)
}

// adjAnswerRunGated is adjAnswerRun with a gate that holds the role round
// that follows the answer (the brief then carries the answer) until the test
// opens it.
func adjAnswerRunGated(t *testing.T, gate *roleGate, check string, decideError bool, keys ...string) (*Daemon, *runrec.Store, string) {
	t.Helper()
	routes := []clitest.Route{
		{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: check},
		{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "42\n"},
		{Tool: "adj", Args: []string{"decide"}, Prefix: true, Stdout: "42\n"},
	}
	if decideError {
		routes[2].Seq = []clitest.Reply{{Stderr: `{"error":{"code":"io","message":"disk full verbatim"}}`, Exit: 3}, {Stdout: "42\n"}}
	}
	runner := roundRunner(func(ctx context.Context, r engine.Request) (engine.Result, error) {
		if gate != nil && strings.Contains(r.Brief, "use stdlib") {
			gate.started <- struct{}{}
			select {
			case <-gate.release:
			case <-ctx.Done():
				return engine.Result{}, ctx.Err()
			}
		}
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

// An early reply (no_wait) comes after the adj filing, not before it, and
// carries the filing message.
func TestRunAnswerNoWaitFilesInAdjBeforeReturning(t *testing.T) {
	// The resumed role is held, so the reply can only be the early one.
	gate := &roleGate{started: make(chan struct{}, 8), release: make(chan struct{})}
	d, store, id := adjAnswerRunGated(t, gate, `{"result":"not found"}`, false)
	t.Cleanup(gate.open)
	b, _ := json.Marshal(RunAction{Run: id, Text: "use stdlib", Adj: true, By: "owner", NoWait: true})
	rep, err := callWithin(t, d, MethodRunAnswer, string(b), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rep.State != runrec.StateImplement {
		t.Errorf("early reply state %q, want implement", rep.State)
	}
	if !strings.Contains(rep.Message, "decided in adj as #42") {
		t.Errorf("early reply lacks the filing message: %q", rep.Message)
	}
	if calls := lastFake.CallsTo("adj"); len(calls) != 3 || calls[2].Args[0] != "decide" {
		t.Errorf("adj calls at the reply: %+v", calls)
	}
	if !busyNow(d, id) {
		t.Error("the run is not busy while its role is held")
	}
	gate.open()
	waitState(t, store, id, runrec.StateAwaitingApproval)
	waitIdle(t, d, id)
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

func TestAnswerWithAdjAndNoKeyProceedsWithoutDecidedBy(t *testing.T) {
	d, store, id := adjAnswerRun(t, `{}`, false, "")
	if _, err := answerAdj(t, d, id, true, ""); err != nil {
		t.Fatal(err)
	}
	rec := waitState(t, store, id, runrec.StateAwaitingApproval)
	waitIdle(t, d, id)
	if rec.Questions[0].Answer != "use stdlib" {
		t.Fatalf("answer: %q", rec.Questions[0].Answer)
	}
	if calls := lastFake.CallsTo("adj"); len(calls) != 0 {
		t.Fatalf("calls: %+v", calls)
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

// fullProjectRig is an adj-answer run (paused on a keyed question) in a project
// whose one slot another run holds, so an answer waits for it (S274). free
// ends the holder's job and frees the slot.
func fullProjectRig(t *testing.T) (d *Daemon, store *runrec.Store, id string, free func()) {
	t.Helper()
	d, store, id = adjAnswerRun(t, `{"result":"not found"}`, false)
	f, err := d.loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	p, _ := find(f, "p")
	p.Cap = 1
	if err := registry.Save(d.registryPath(), &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	// Another run of the project holds its one slot.
	other, err := store.Create(runrec.Record{ID: "other", Project: "p", Item: "S9", State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	d.runs.mu.Lock()
	holder := &managedRun{engine: engine.Load(d.runDeps(p), p, other)}
	d.runs.runs["other"] = holder
	d.launchRun(holder, func(context.Context) error { <-release; return nil })
	d.runs.mu.Unlock()
	var once sync.Once
	free = func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	return d, store, id, free
}

// gateAdj puts a wrapper in front of the fake adj that blocks every call until
// open is called, so a test can hold FileAnswer in the middle of its adj
// calls. started returns once the first call has begun.
func gateAdj(t *testing.T, d *Daemon, store *runrec.Store, id string) (started func(), open func()) {
	t.Helper()
	dir := t.TempDir()
	startedFile, openFile := filepath.Join(dir, "started"), filepath.Join(dir, "open")
	script := fmt.Sprintf("#!/bin/sh\n: > %q\nwhile [ ! -e %q ]; do sleep 0.01; done\nexec %q \"$@\"\n", startedFile, openFile, lastFake.Bin("adj"))
	wrapper := filepath.Join(dir, "adj")
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	d.runs.deps.Adj = adjcli.Client{Bin: wrapper}
	// The run's engine took its deps when it started: load the paused run again.
	f, err := d.loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	p, _ := find(f, "p")
	run, err := store.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	d.runs.mu.Lock()
	d.runs.runs[id].engine = engine.Load(d.runDeps(p), p, run)
	d.runs.mu.Unlock()
	var once sync.Once
	open = func() {
		once.Do(func() {
			if err := os.WriteFile(openFile, nil, 0o644); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(open)
	return func() {
		t.Helper()
		waitFor(t, "the adj filing to begin", func() bool { _, err := os.Stat(startedFile); return err == nil })
	}, open
}

// answerAsync sends the adj answer of run id the way the CLI does and returns
// the channel of its reply.
func answerAsync(d *Daemon, id string) <-chan callResult {
	b, _ := json.Marshal(RunAction{Run: id, Text: "use stdlib", Adj: true, By: "owner", NoWait: true})
	return startCall(d, MethodRunAnswer, string(b))
}

func waitReply(t *testing.T, ch <-chan callResult, what string) callResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return", what)
	}
	return callResult{}
}

func stopAsync(d *Daemon, id string) <-chan callResult {
	return startCall(d, MethodRunStop, fmt.Sprintf(`{"run":%q}`, id))
}

// An answer that waits for a slot is filed in adj at once; only the engine
// resume waits. The start does not file it a second time (S274, adj 177).
func TestAnswerThatWaitsFilesInAdjAtOnce(t *testing.T) {
	d, store, id, free := fullProjectRig(t)
	b, _ := json.Marshal(RunAction{Run: id, Text: "use stdlib", Adj: true, By: "owner", NoWait: true})
	rep, err := callWithin(t, d, MethodRunAnswer, string(b), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rep.State != runrec.StatePaused || rep.Resume == nil {
		t.Fatalf("reply: state %q resume %+v, want paused with a resume waiting", rep.State, rep.Resume)
	}
	if rep.Resume.Adj {
		t.Errorf("the stored resume would file in adj again: %+v", rep.Resume)
	}
	if !strings.Contains(rep.Message, "decided in adj as #42") || !strings.Contains(rep.Message, ResumeWaitNote) {
		t.Errorf("reply message %q lacks the filing notice or the wait note", rep.Message)
	}
	if calls := lastFake.CallsTo("adj"); len(calls) != 3 || calls[2].Args[0] != "decide" {
		t.Fatalf("adj calls at the reply: %+v", calls)
	}
	free() // the slot frees; the waiting resume starts
	waitState(t, store, id, runrec.StateAwaitingApproval)
	waitIdle(t, d, id)
	if calls := lastFake.CallsTo("adj"); len(calls) != 3 {
		t.Errorf("the start filed in adj again: %+v", calls)
	}
}

// While the answer is in adj, no other owner action starts on the run. The
// flag is set by the real path: the test holds FileAnswer open (S274).
func TestNoOwnerActionStartsWhileAnAnswerIsBeingFiled(t *testing.T) {
	d, store, id, _ := fullProjectRig(t)
	started, open := gateAdj(t, d, store, id)
	first := answerAsync(d, id)
	started()
	for name, ch := range map[string]<-chan callResult{
		"second answer": answerAsync(d, id),
		"retry":         startCall(d, MethodRunRetry, fmt.Sprintf(`{"run":%q,"no_wait":true}`, id)),
	} {
		if res := waitReply(t, ch, name); res.err == nil || !strings.Contains(res.err.Error(), "is busy") {
			t.Errorf("%s while filing: %v", name, res.err)
		}
	}
	open()
	if res := waitReply(t, first, "the first answer"); res.err != nil || res.rep.Resume == nil {
		t.Fatalf("first answer: %+v %v", res.rep, res.err)
	}
	if calls := lastFake.CallsTo("adj"); len(calls) != 3 {
		t.Errorf("adj calls: %+v", calls)
	}
}

// A stop that arrives while the answer is in adj waits for the filing, then
// stops the run: no second job starts, the waiting resume is not stored after
// the stop, and the stop's channel is closed once (S274).
func TestStopDuringAdjFilingWaitsThenStopsTheRun(t *testing.T) {
	d, store, id, free := fullProjectRig(t)
	started, open := gateAdj(t, d, store, id)
	first := answerAsync(d, id)
	started()
	stop := stopAsync(d, id)
	select {
	case res := <-stop:
		t.Fatalf("the stop returned during the filing: %+v %v", res.rep, res.err)
	case <-time.After(200 * time.Millisecond):
	}
	if busyNow(d, id) {
		t.Fatal("a job started on the run while its answer was being filed")
	}
	open()
	waitReply(t, first, "the answer")
	res := waitReply(t, stop, "the stop")
	if res.err != nil || res.rep.State != runrec.StateStopped || res.rep.Resume != nil {
		t.Fatalf("stop: state %q resume %+v err %v", res.rep.State, res.rep.Resume, res.err)
	}
	if got, _ := store.Open(id); got.Snapshot().State != runrec.StateStopped || got.Snapshot().Resume != nil {
		t.Fatalf("stored record: %+v", got.Snapshot())
	}
	// A freed slot must not start the stopped run. The holder's job end starts
	// queued runs under the lock that clears its busy flag, so once it is idle
	// that start has happened.
	free()
	waitIdle(t, d, "other")
	waitIdle(t, d, id)
	if got, _ := store.Open(id); got.Snapshot().State != runrec.StateStopped {
		t.Fatalf("the run left stopped: %q", got.Snapshot().State)
	}
	if calls := lastFake.CallsTo("adj"); len(calls) != 3 {
		t.Errorf("adj calls: %+v", calls)
	}
}

// A slot that frees during the filing starts the answer at once. A stop that
// waited for the filing then cancels that job and stops the run: one job at a
// time, one close of its channel. Which of the two the answer's reply shows
// depends on whether the job applied the answer before the stop cancelled it,
// so the stop case accepts both and asserts what holds either way (S274).
func TestSlotFreedDuringAdjFilingStartsTheAnswerOnce(t *testing.T) {
	for _, withStop := range []bool{false, true} {
		t.Run(fmt.Sprintf("stop-%v", withStop), func(t *testing.T) {
			d, store, id, free := fullProjectRig(t)
			started, open := gateAdj(t, d, store, id)
			first := answerAsync(d, id)
			started()
			free()
			waitIdle(t, d, "other")
			var stop <-chan callResult
			if withStop {
				stop = stopAsync(d, id)
			}
			open()
			res := waitReply(t, first, "the answer")
			if res.rep.Resume != nil {
				t.Fatalf("a resume was stored although a slot was free: %+v", res.rep.Resume)
			}
			if withStop {
				// The stop cancels the job that the filer launched, so the answer
				// returns its reply or the cancel; nothing else is acceptable.
				if res.err != nil && !errors.Is(res.err, context.Canceled) && !strings.Contains(res.err.Error(), "context canceled") {
					t.Fatalf("answer with a stop behind it: %v", res.err)
				}
				if st := waitReply(t, stop, "the stop"); st.err != nil || st.rep.State != runrec.StateStopped || st.rep.Resume != nil {
					t.Fatalf("stop: state %q resume %+v err %v", st.rep.State, st.rep.Resume, st.err)
				}
				waitIdle(t, d, id)
				if got, _ := store.Open(id); got.Snapshot().State != runrec.StateStopped || got.Snapshot().Resume != nil {
					t.Fatalf("stored record: state %q resume %+v", got.Snapshot().State, got.Snapshot().Resume)
				}
				if calls := lastFake.CallsTo("adj"); len(calls) != 3 {
					t.Errorf("adj calls: %+v", calls)
				}
				return
			}
			if res.err != nil {
				t.Fatalf("answer with a free slot: %v", res.err)
			}
			waitState(t, store, id, runrec.StateAwaitingApproval)
			waitIdle(t, d, id)
			if calls := lastFake.CallsTo("adj"); len(calls) != 3 {
				t.Errorf("adj calls: %+v", calls)
			}
		})
	}
}

// The filer treats anything that touched the run while it was in adj as a
// change: it neither stores the resume nor starts a job (S274).
func TestFilerSkipsTheResumeAndTheStartWhenTheRunChanged(t *testing.T) {
	for name, touch := range map[string]func(r *managedRun){
		"stopping": func(r *managedRun) { r.stopping = true },
		"busy":     func(r *managedRun) { r.busy = true },
		"stop":     func(r *managedRun) { r.stop = &stopCall{started: make(chan struct{})} },
	} {
		t.Run(name, func(t *testing.T) {
			d, store, id, _ := fullProjectRig(t)
			started, open := gateAdj(t, d, store, id)
			first := answerAsync(d, id)
			started()
			d.runs.mu.Lock()
			r := d.runs.runs[id]
			touch(r)
			d.runs.mu.Unlock()
			open()
			res := waitReply(t, first, "the answer")
			d.runs.mu.Lock()
			r.stopping, r.busy, r.stop = false, false, nil
			d.runs.mu.Unlock()
			if res.err == nil || !strings.Contains(res.err.Error(), "changed while its answer was filed in adj; check ork status") {
				t.Fatalf("answer after a change: %+v %v", res.rep, res.err)
			}
			if got, _ := store.Open(id); got.Snapshot().Resume != nil || got.Snapshot().State != runrec.StatePaused {
				t.Fatalf("the filer changed the record: state %q resume %+v", got.Snapshot().State, got.Snapshot().Resume)
			}
		})
	}
}
