// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

const u11 = "The spec check before a wave runs the Spec check row (r16): one read-only pass."

const specQuestionsReport = `## Slice S2
Question: Which store holds the index?
Options:
1. file: one JSON file
2. memory: rebuilt at each start
Recommend: 2 - nothing reads it across restarts
Decision key: cache.index-store

Question: What names the flag?
Options:
1. short: -x
2. long: --extra
Recommend: 2 - flags are spelled out
`

// specRig is a daemon whose matrix has a spec check row and whose spec check
// role answers with the report in rig.report. S1 and S2 are the project's
// slices; their `slicer show` output is in files the test can rewrite.
type specRig struct {
	d      *Daemon
	store  *runrec.Store
	fake   *clitest.Fake
	mu     sync.Mutex
	report string
	reqs   []engine.Request
	others []engine.Request // the rounds of the runs that start afterwards
	gate   chan struct{}    // when set, the role waits for it
	root   string
}

func (r *specRig) requests() []engine.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]engine.Request(nil), r.reqs...)
}

func (r *specRig) roleRequests() []engine.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]engine.Request(nil), r.others...)
}

func (r *specRig) setSpec(t *testing.T, id, why string) {
	t.Helper()
	body := fmt.Sprintf(`{"id":%q,"title":"T","slice":{"id":%q,"title":"T","sections":[{"heading":"Why","body":%q}]}}`, id, id, why)
	if err := os.WriteFile(filepath.Join(r.root, id+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newSpecRig(t *testing.T, rules string, extra ...clitest.Route) *specRig {
	t.Helper()
	rig := &specRig{root: t.TempDir(), report: "## Slice S1\nReady\n\n" + specQuestionsReport + "\n## Owner needed\n\nnone\n"}
	row := func(id, model, effort string) string {
		return fmt.Sprintf(`{"id":%q,"agent":"Claude Code","vendor":"Claude","model":%q,"reasoning":%q}`, id, model, effort)
	}
	recommend := `{"roles":[{"role":"implementer","row":` + row("r3", "sonnet", "high") + `},{"role":"reviewer","row":` + row("r5", "opus", "medium") + `,"alternatives":[` + row("r16", "sonnet-spec", "low") + `]},{"role":"architect","row":null,"alternatives":[],"selected_by":"unmatched"}]}`
	for _, id := range []string{"S1", "S2"} {
		rig.setSpec(t, id, "why "+id)
	}
	routes := append([]clitest.Route{}, extra...)
	routes = append(routes,
		clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "show"}, Prefix: true, Stdout: `{"rows":[],"rules":` + rules + `}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true, Stdout: recommend},
		clitest.Route{Tool: "slicer", Args: []string{"show", "S1"}, Prefix: true, File: filepath.Join(rig.root, "S1.json")},
		clitest.Route{Tool: "slicer", Args: []string{"show", "S2"}, Prefix: true, File: filepath.Join(rig.root, "S2.json")},
		clitest.Route{Tool: "slicer", Args: []string{"edit", "S2"}, Prefix: true, Stdout: `{"id":"S2"}`},
	)
	rig.d, rig.store, _ = runDaemon(t, func(ctx context.Context, r engine.Request) (engine.Result, error) {
		if r.Role != engine.RoleSpecCheck {
			rig.mu.Lock()
			rig.others = append(rig.others, r)
			rig.mu.Unlock()
			return engine.Result{Exit: engine.ExitAuth}, nil
		}
		rig.mu.Lock()
		rig.reqs = append(rig.reqs, r)
		report, gate := rig.report, rig.gate
		rig.mu.Unlock()
		if gate != nil {
			select {
			case <-gate:
			case <-ctx.Done():
				return engine.Result{}, ctx.Err()
			}
		}
		return engine.Result{Exit: engine.ExitOK, Report: report}, nil
	}, routes...)
	rig.fake = lastFake
	return rig
}

func rulesJSON(texts ...string) string {
	var rs []string
	for i, text := range texts {
		rs = append(rs, fmt.Sprintf(`{"id":"u%d","text":%q}`, i+1, text))
	}
	return "[" + strings.Join(rs, ",") + "]"
}

func (r *specRig) check(t *testing.T, items ...string) SpecCheckResult {
	t.Helper()
	raw, _ := json.Marshal(SpecCheckParams{Project: "p", Items: items})
	var out SpecCheckResult
	if err := json.Unmarshal(request(t, r.d, MethodSpecCheck, string(raw)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (r *specRig) item(res SpecCheckResult, id string) SpecItemResult {
	for _, it := range res.Items {
		if it.Item == id {
			return it
		}
	}
	return SpecItemResult{}
}

func TestSpecCheckRunsTheSpecCheckRowReadOnly(t *testing.T) {
	rig := newSpecRig(t, rulesJSON("A reviewer never wrote code.", u11))
	res := rig.check(t, "S1", "S2")
	if len(res.Items) != 2 || !rig.item(res, "S1").Ready || rig.item(res, "S2").Ready || len(rig.item(res, "S2").Questions) != 2 {
		t.Fatalf("result: %+v", res)
	}
	reqs := rig.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d role rounds, want one for the batch", len(reqs))
	}
	r := reqs[0]
	if r.Vendor != "claude" || r.Model != "sonnet-spec" || r.Effort != "low" || !r.ReadOnly || len(r.Allowlist) != 0 {
		t.Errorf("request: %+v", r)
	}
	if !strings.Contains(r.Brief, "S1, S2") {
		t.Errorf("brief names the batch: %q", r.Brief[:min(len(r.Brief), 200)])
	}
	if rs, err := rig.store.List(); err != nil || len(rs) != 0 {
		t.Errorf("a spec check is not a run: %v %v", rs, err)
	}
}

func TestSpecCheckAsksAgainOnlyForAChangedSpec(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.check(t, "S1", "S2")
	res := rig.check(t, "S1", "S2")
	if n := len(rig.requests()); n != 1 {
		t.Fatalf("%d role rounds after an unchanged spec", n)
	}
	if !rig.item(res, "S1").Ready || len(rig.item(res, "S2").Questions) != 2 {
		t.Errorf("cached result: %+v", res)
	}
	rig.setSpec(t, "S2", "why S2, changed")
	rig.mu.Lock()
	rig.report = "## Slice S2\nReady\n\n## Owner needed\n\nnone\n"
	rig.mu.Unlock()
	res = rig.check(t, "S1", "S2")
	reqs := rig.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d role rounds, want the changed S2 checked again", len(reqs))
	}
	if strings.Contains(reqs[1].Brief, `"id":"S1"`) || !strings.Contains(reqs[1].Brief, `"id":"S2"`) {
		t.Errorf("second brief should hold S2 only: %q", reqs[1].Brief)
	}
	if !rig.item(res, "S1").Ready || !rig.item(res, "S2").Ready {
		t.Errorf("result: %+v", res)
	}
}

func TestWaveApproveRefusesUncheckedItem(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.check(t, "S1", "S2")
	var out struct {
		Items []struct {
			Item  string                          `json:"item"`
			Run   *runrec.Record                  `json:"run"`
			Error *struct{ Code, Message string } `json:"error"`
		} `json:"items"`
	}
	b := request(t, rig.d, MethodWaveStart, `{"items":[{"project":"p","item":"S1"},{"project":"p","item":"S2"}]}`)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 || out.Items[0].Run == nil || out.Items[1].Run != nil || out.Items[1].Error == nil {
		t.Fatalf("result: %s", b)
	}
	if e := out.Items[1].Error; e.Code != CodeSpecQuestions || !strings.Contains(e.Message, "S2") || !strings.Contains(e.Message, "2 open spec questions") {
		t.Errorf("error: %+v", e)
	}
	runs, err := rig.store.List()
	if err != nil || len(runs) != 1 || runs[0].Snapshot().Item != "S1" {
		t.Fatalf("runs: %v %v", runs, err)
	}
}

// An item the check has not seen is not refused by the daemon: the clients
// run the check before they start a wave. A changed spec is unchecked too.
func TestWaveStartDoesNotRefuseAnItemWhoseSpecChangedSinceTheCheck(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.check(t, "S1", "S2")
	rig.setSpec(t, "S2", "why S2, rewritten by the owner")
	b := request(t, rig.d, MethodWaveStart, `{"items":[{"project":"p","item":"S2"}]}`)
	if !strings.Contains(string(b), `"run"`) || strings.Contains(string(b), CodeSpecQuestions) {
		t.Fatalf("result: %s", b)
	}
}

func TestSpecAnswerRecordsTheAnswersAndReleasesTheItem(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.check(t, "S1", "S2")
	b := request(t, rig.d, MethodSpecAnswer, `{"project":"p","item":"S2","answers":["memory: rebuilt at each start","long: --extra"]}`)
	var ans SpecAnswerResult
	if err := json.Unmarshal(b, &ans); err != nil || !ans.Ready || ans.Item != "S2" {
		t.Fatalf("answer: %s %v", b, err)
	}
	var edits [][]string
	for _, c := range rig.fake.CallsTo("slicer") {
		if c.Args[0] == "edit" {
			edits = append(edits, c.Args)
		}
	}
	if len(edits) != 1 || edits[0][3] != "Implement" || !strings.Contains(strings.Join(edits[0], " "), "Which store holds the index? -> memory: rebuilt at each start [cache.index-store]. What names the flag? -> long: --extra.") {
		t.Fatalf("edits: %v", edits)
	}
	res := rig.check(t, "S2")
	if !rig.item(res, "S2").Ready || len(rig.item(res, "S2").Questions) != 0 || len(rig.requests()) != 1 {
		t.Errorf("after the answer: %+v, %d rounds", res, len(rig.requests()))
	}
	b = request(t, rig.d, MethodWaveStart, `{"items":[{"project":"p","item":"S2"}]}`)
	if !strings.Contains(string(b), `"run"`) || strings.Contains(string(b), CodeSpecQuestions) {
		t.Fatalf("an answered item must start: %s", b)
	}
}

func TestSpecAnswerRefusals(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	call := func(raw string) error {
		_, err := rig.d.handle(context.Background(), MethodSpecAnswer, json.RawMessage(raw))
		return err
	}
	if err := call(`{"project":"p","item":"S2","answers":["x","y"]}`); err == nil || !strings.Contains(err.Error(), "no open") {
		t.Errorf("unchecked item: %v", err)
	}
	rig.check(t, "S1", "S2")
	if err := call(`{"project":"p","item":"S1","answers":["x"]}`); err == nil || !strings.Contains(err.Error(), "no open") {
		t.Errorf("ready item: %v", err)
	}
	if err := call(`{"project":"p","item":"S2","answers":["x"]}`); err == nil {
		t.Error("one answer for two questions")
	}
	rig.setSpec(t, "S2", "changed under the questions")
	if err := call(`{"project":"p","item":"S2","answers":["x","y"]}`); err == nil || !strings.Contains(err.Error(), "spec changed") {
		t.Errorf("changed spec: %v", err)
	}
	for _, c := range rig.fake.CallsTo("slicer") {
		if c.Args[0] == "edit" {
			t.Errorf("a refused answer edited the slice: %v", c.Args)
		}
	}
}

func TestSpecCheckNeedsTheRuleAndItsRow(t *testing.T) {
	for name, rules := range map[string]string{
		"no rule":        rulesJSON("A reviewer never wrote code."),
		"row not listed": rulesJSON(strings.ReplaceAll(u11, "r16", "r99")),
	} {
		t.Run(name, func(t *testing.T) {
			rig := newSpecRig(t, rules)
			_, err := rig.d.handle(context.Background(), MethodSpecCheck, json.RawMessage(`{"project":"p","items":["S1"]}`))
			if err == nil || !strings.Contains(err.Error(), "Spec check row") {
				t.Fatalf("error %v", err)
			}
			if len(rig.requests()) != 0 {
				t.Error("a role ran without a row")
			}
		})
	}
}

func TestSpecCheckBadParams(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	for name, raw := range map[string]string{
		"no items":        `{"project":"p","items":[]}`,
		"no project":      `{"items":["S1"]}`,
		"unknown project": `{"project":"nope","items":["S1"]}`,
		"empty item":      `{"project":"p","items":[""]}`,
	} {
		if _, err := rig.d.handle(context.Background(), MethodSpecCheck, json.RawMessage(raw)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestSpecCheckStopsWhenTheRoleChangedTheTree(t *testing.T) {
	// Script the status reads; HEAD stays unchanged.
	rig := newSpecRig(t, rulesJSON(u11), clitest.Route{Tool: "git", Args: []string{"-C"}, Prefix: true, Seq: []clitest.Reply{{}, {}, {}, {Stdout: " M a.go\n"}, {}, {}, {}, {}, {}, {}}})
	_, err := rig.d.handle(context.Background(), MethodSpecCheck, json.RawMessage(`{"project":"p","items":["S1","S2"]}`))
	if err == nil || !strings.Contains(err.Error(), "changed the tree") {
		t.Fatalf("error %v", err)
	}
	// Nothing the stopped check said is kept.
	rig.mu.Lock()
	rig.report = "## Slice S1\nReady\n\n## Slice S2\nReady\n\n## Owner needed\n\nnone\n"
	rig.mu.Unlock()
	if res := rig.check(t, "S1", "S2"); !rig.item(res, "S2").Ready {
		t.Errorf("the second check: %+v", res)
	}
	if n := len(rig.requests()); n != 2 {
		t.Errorf("%d role rounds, want the stopped check run again", n)
	}
}

func TestAnActiveSpecCheckKeepsTheDaemonBusy(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.gate = make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		rig.check(t, "S1", "S2")
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(rig.requests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !rig.d.activeRuns() {
		close(rig.gate)
		<-done
		t.Fatal("a running spec check is not active work")
	}
	if _, err := rig.d.stopDaemon(StopParams{}); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Errorf("stop without force: %v", err)
	}
	close(rig.gate)
	<-done
	if rig.d.activeRuns() {
		t.Error("the finished check still holds the daemon")
	}
}

func TestPlanKeepsTheSpecCheckRowOutOfEveryChain(t *testing.T) {
	plan := uiPlan(t,
		[]string{matrixRow("r3", "sonnet", "high")},
		[]string{matrixRow("r5", "opus", "medium"), matrixRow("r16", "sonnet-spec", "low"), matrixRow("r20", "haiku", "high")},
		"A reviewer never wrote code.", u11)
	want := []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}, {Vendor: "claude", Model: "haiku", Effort: "high"}}
	for _, role := range []string{engine.RoleReviewer, engine.RoleGate} {
		if !reflect.DeepEqual(plan.Lineup[role], want) {
			t.Errorf("%s chain %+v, want %+v", role, plan.Lineup[role], want)
		}
	}
	if _, ok := plan.Lineup[engine.RoleSpecCheck]; ok {
		t.Errorf("the plan of a run holds a spec-check chain: %+v", plan.Lineup)
	}
}

func TestSpecAnswerFilesKeyedQuestionsInAdjOnlyWhenTheProjectHasAdj(t *testing.T) {
	for _, hasAdj := range []bool{false, true} {
		t.Run(fmt.Sprint(hasAdj), func(t *testing.T) {
			rig := newSpecRig(t, rulesJSON(u11),
				clitest.Route{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: `{"result":"not found"}`},
				clitest.Route{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "42\n"},
				clitest.Route{Tool: "adj", Args: []string{"decide"}, Prefix: true, Stdout: "42\n"})
			f, err := registry.Load(rig.d.cfg.RegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			f.Projects[0].Adj = hasAdj
			if err := registry.Save(rig.d.cfg.RegistryPath, f); err != nil {
				t.Fatal(err)
			}
			rig.d.runs.deps.Adj = adjcli.Client{Bin: rig.fake.Bin("adj")}
			rig.report = specQuestionsReport + "\n## Owner needed\n\nnone\n"
			rig.check(t, "S2")
			b := request(t, rig.d, MethodSpecAnswer, `{"project":"p","item":"S2","answers":["memory: rebuilt","long: --extra"],"adj":true,"by":"jon"}`)
			var ans SpecAnswerResult
			if err := json.Unmarshal(b, &ans); err != nil || !ans.Ready || len(ans.Notices) != 1 {
				t.Fatalf("answer: %s %v", b, err)
			}
			calls := rig.fake.CallsTo("adj")
			if hasAdj {
				if len(calls) != 3 || !strings.Contains(ans.Notices[0], "decided in adj as #42") {
					t.Errorf("adj calls %+v, notices %v", calls, ans.Notices)
				}
			} else if len(calls) != 0 || !strings.Contains(ans.Notices[0], "no adj in this project") {
				t.Errorf("adj calls %+v, notices %v", calls, ans.Notices)
			}
		})
	}
}

// An answered question reaches the implementer: the run's worktree has its own
// copy of the spec (here the show output never gains the paragraph), so ork
// carries the paragraph into the run (S116).
func TestAnsweredSpecQuestionsReachTheImplementerBrief(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.check(t, "S1", "S2")
	request(t, rig.d, MethodSpecAnswer, `{"project":"p","item":"S2","answers":["memory: rebuilt at each start","long: --extra"]}`)
	b := request(t, rig.d, MethodWaveStart, `{"items":[{"project":"p","item":"S1"},{"project":"p","item":"S2"}]}`)
	var out struct {
		Items []struct {
			Item string         `json:"item"`
			Run  *runrec.Record `json:"run"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &out); err != nil || len(out.Items) != 2 || out.Items[0].Run == nil || out.Items[1].Run == nil {
		t.Fatalf("wave: %s %v", b, err)
	}
	const para = "Owner decisions ("
	deadline := time.Now().Add(5 * time.Second)
	var s2, s1 []engine.Request
	for time.Now().Before(deadline) {
		s2, s1 = nil, nil
		for _, r := range rig.roleRequests() {
			if strings.HasSuffix(r.Worktree, "S2") {
				s2 = append(s2, r)
			} else {
				s1 = append(s1, r)
			}
		}
		if len(s1) > 0 && len(s2) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(s2) == 0 || len(s1) == 0 {
		t.Fatalf("roles did not start: %d for S2, %d for S1", len(s2), len(s1))
	}
	want := "Which store holds the index? -> memory: rebuilt at each start [cache.index-store]. What names the flag? -> long: --extra."
	if r := s2[0]; r.Role != engine.RoleImplementer || !strings.Contains(r.Brief, "## Owner decisions\n\n"+para) || !strings.Contains(r.Brief, want) {
		t.Errorf("S2 implementer brief lacks the decisions:\n%s", r.Brief)
	}
	if strings.Contains(s1[0].Brief, "Owner decisions") {
		t.Error("S1 had no questions, yet its brief has decisions")
	}
	if rec, err := rig.store.Open(out.Items[1].Run.ID); err != nil || !strings.Contains(rec.Snapshot().Decisions, want) {
		t.Errorf("run record: %v %+v", err, rec)
	}
}

// A forced stop must not wait for a spec check that is still running: it
// cancels the job, and the check returns the cancellation.
func TestForcedStopCancelsARunningSpecCheck(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.gate = make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-rig.gate:
		default:
			close(rig.gate)
		}
	})
	errc := make(chan error, 1)
	go func() {
		_, err := rig.d.handle(context.Background(), MethodSpecCheck, json.RawMessage(`{"project":"p","items":["S1","S2"]}`))
		errc <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(rig.requests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stopped := make(chan error, 1)
	go func() { _, err := rig.d.stopDaemon(StopParams{Force: true}); stopped <- err }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("forced stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("forced stop hangs on the running spec check")
	}
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("check returned %v, want a cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the check did not return")
	}
}

func answeredSpecRig(t *testing.T) *specRig {
	t.Helper()
	rig := newSpecRig(t, rulesJSON(u11))
	rig.check(t, "S1", "S2")
	request(t, rig.d, MethodSpecAnswer, `{"project":"p","item":"S2","answers":["memory","long"]}`)
	return rig
}

func restartSpecRig(t *testing.T, rig *specRig) {
	t.Helper()
	old := rig.d
	old.closeRuns()
	old.cache.Close()
	rig.d = New(old.cfg)
	t.Cleanup(rig.d.closeRuns)
	t.Cleanup(rig.d.cache.Close)
}

func TestOwnerAnswersSurviveARestart(t *testing.T) {
	for _, recheck := range []bool{false, true} {
		t.Run(fmt.Sprint(recheck), func(t *testing.T) {
			rig := answeredSpecRig(t)
			rig.report = specQuestionsReport + "\n## Owner needed\nnone\n"
			restartSpecRig(t, rig)
			if recheck {
				res := rig.check(t, "S2")
				if !rig.item(res, "S2").Ready || len(rig.requests()) != 1 {
					t.Fatalf("answers lost after restart: %+v (%d checks)", res, len(rig.requests()))
				}
			}
			b := request(t, rig.d, MethodWaveStart, `{"items":[{"project":"p","item":"S2"}]}`)
			var out WaveResult
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatal(err)
			}
			if len(out.Items) != 1 || out.Items[0].Run == nil || !strings.Contains(out.Items[0].Run.Decisions, "Which store holds the index? -> memory") {
				t.Fatalf("run lost decisions: %s", b)
			}
			if _, err := os.Stat(filepath.Join(rig.d.specDir("p", "S2"), "answers.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("answers not consumed at run start: %v", err)
			}
		})
	}
}

func TestSpecChangeDropsStoredAnswers(t *testing.T) {
	rig := answeredSpecRig(t)
	paths, err := filepath.Glob(filepath.Join(rig.d.cfg.Paths.Dir, "spec-checks", "*", "*", "answers.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("answers not stored: %v %v", paths, err)
	}
	rig.setSpec(t, "S2", "new spec")
	rig.mu.Lock()
	rig.report = "## Slice S2\nReady\n\n## Owner needed\nnone\n"
	rig.mu.Unlock()
	rig.check(t, "S2")
	restartSpecRig(t, rig)
	// A reverted spec must not resurrect the old answers.
	rig.setSpec(t, "S2", "why S2")
	rig.check(t, "S2")
	rig.d.runs.mu.Lock()
	decisions := rig.d.runs.specs[specKey("p", "S2")].decisions
	rig.d.runs.mu.Unlock()
	if decisions != "" {
		t.Fatalf("stale decisions: %q", decisions)
	}
	if len(rig.requests()) != 3 {
		t.Fatalf("stored answers survived a spec change: %d checks", len(rig.requests()))
	}
}

func TestSittingDoesNotFreezeWhileAnAnswerWaitsOnSpecMu(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.check(t, "S1", "S2")
	socket := filepath.Join(shortDir(t), "s")
	ln, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	server := &ipc.Server{Handler: func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method == MethodSpecAnswer {
			close(entered)
		}
		return rig.d.handle(ctx, method, raw)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, ln) }()
	defer func() { cancel(); <-done }()
	client, err := Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	rig.d.runs.specMu.Lock()
	answers := make(chan error, 1)
	go func() {
		_, err := client.SpecAnswer(ctx, SpecAnswerParams{Project: "p", Item: "S2", Answers: []string{"memory", "long"}})
		answers <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		rig.d.runs.specMu.Unlock()
		t.Fatal("answer did not arrive")
	}
	projects := make(chan error, 1)
	go func() { _, err := client.Projects(ctx); projects <- err }()
	frozen := false
	select {
	case err := <-projects:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(250 * time.Millisecond):
		frozen = true
	}
	rig.d.runs.specMu.Unlock()
	if err := <-answers; err != nil {
		t.Error(err)
	}
	if frozen {
		<-projects
		t.Fatal("sitting connection froze behind the answer waiting on specMu")
	}
}

func TestSpecCheckDirRemovedWhenTheRunLands(t *testing.T) {
	d, store, _ := runDaemon(t, func(ctx context.Context, r engine.Request) (engine.Result, error) {
		report := "Tests: 1 passed, 0 failed\n\n## Owner needed\nnone\n"
		if r.Role == engine.RoleReviewer {
			report = "(A): nothing\n(B): nothing\n(D): nothing\n\n## Owner needed\nnone\n"
		}
		if r.Role == engine.RoleGate {
			report = "VERDICT: PASS\n" + report
		}
		return engine.Result{Exit: engine.ExitOK, Report: report}, nil
	})
	dir := d.specDir("p", "S1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec-check.md"), []byte("report"), 0600); err != nil {
		t.Fatal(err)
	}
	b := request(t, d, MethodRunStart, `{"project":"p","item":"S1","well_specified":true}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, rec.ID, runrec.StateAwaitingApproval)
	waitIdle(t, d, rec.ID)
	assertSpecMuDoesNotBlock(t, d, func() error {
		_, err := d.ownerRun(context.Background(), MethodRunApprove, RunAction{Run: rec.ID})
		return err
	})
	waitIdle(t, d, rec.ID)
	r, err := store.Open(rec.ID)
	if err != nil || r.Snapshot().LandedAt.IsZero() {
		t.Fatalf("run did not land: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("landed check directory remains: %v", err)
	}
}

func TestSpecCheckDirsSweptAtDaemonStart(t *testing.T) {
	for _, listFails := range []bool{false, true} {
		t.Run(fmt.Sprint(listFails), func(t *testing.T) {
			exit := 0
			if listFails {
				exit = 1
			}
			rig := newSpecRig(t, rulesJSON(u11), clitest.Route{Tool: "slicer", Args: []string{"list"}, Prefix: true, Stdout: `[{"id":"S1"}]`, Exit: exit})
			old := time.Now().Add(-15 * 24 * time.Hour)
			for _, item := range []string{"S1", "S2", "S3"} {
				dir := rig.d.specDir("p", item)
				if err := writeSpecJSON(filepath.Join(dir, "check.json"), map[string]string{"project": "p", "item": item}); err != nil {
					t.Fatal(err)
				}
				if item != "S3" {
					if err := os.Chtimes(dir, old, old); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := rig.d.recoverRuns(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, item := range []string{"S1", "S2", "S3"} {
				_, err := os.Stat(rig.d.specDir("p", item))
				removed := errors.Is(err, os.ErrNotExist)
				if removed != (item == "S2" && !listFails) {
					t.Errorf("%s removed=%v, error %v", item, removed, err)
				}
			}
		})
	}
}

func TestSpecCheckDirsRemovedWithProject(t *testing.T) {
	rig := answeredSpecRig(t)
	paths, _ := filepath.Glob(filepath.Join(rig.d.cfg.Paths.Dir, "spec-checks", "*", "*", "answers.json"))
	if len(paths) != 1 {
		t.Fatalf("missing stored answers: %v", paths)
	}
	if err := rig.d.removeProject("p"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(paths[0])); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed project's answers remain: %v", err)
	}
}

func TestSpecCheckKeepsOwnerNeededTextInCachedReplies(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.report = strings.Replace(rig.report, "\nnone\n", "\nConfirm the rollout window.\n", 1)
	for i := 0; i < 2; i++ {
		b, _ := json.Marshal(rig.check(t, "S1", "S2"))
		if !strings.Contains(string(b), `"owner_needed":"Confirm the rollout window."`) {
			t.Fatalf("owner text lost: %s", b)
		}
	}
	if n := len(rig.requests()); n != 1 {
		t.Fatalf("%d checks", n)
	}
}

func assertSpecMuDoesNotBlock(t *testing.T, d *Daemon, fn func() error) {
	t.Helper()
	d.runs.specMu.Lock()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	timedOut := false
	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		timedOut = true
	}
	d.runs.specMu.Unlock()
	if timedOut {
		err = <-done
	}
	if err != nil {
		t.Fatal(err)
	}
	if timedOut {
		t.Fatal("operation waited for the role's specMu")
	}
}

func TestRunStartDoesNotWaitForSpecMu(t *testing.T) {
	rig := answeredSpecRig(t)
	assertSpecMuDoesNotBlock(t, rig.d, func() error {
		_, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S2"})
		return err
	})
}

func TestSpecLifecycleDoesNotWaitForSpecMu(t *testing.T) {
	t.Run("recover landed", func(t *testing.T) {
		d, store, _ := runDaemon(t, nil)
		if _, err := store.Create(runrec.Record{ID: "landed", Project: "p", Item: "S1", State: runrec.StateCommitted, LandedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		assertSpecMuDoesNotBlock(t, d, func() error { return d.recoverRuns(context.Background()) })
	})
	t.Run("remove project", func(t *testing.T) {
		rig := answeredSpecRig(t)
		assertSpecMuDoesNotBlock(t, rig.d, func() error { return rig.d.removeProject("p") })
	})
}

func TestInvalidStoredSpecAnswersAreDiscardedAndLogged(t *testing.T) {
	for _, body := range []string{"broken JSON", "", "{}", "null"} {
		t.Run(fmt.Sprintf("body-%q", body), func(t *testing.T) {
			rig := answeredSpecRig(t)
			rig.report = specQuestionsReport + "\n## Owner needed\nnone\n"
			restartSpecRig(t, rig)
			path := filepath.Join(rig.d.specDir("p", "S2"), "answers.json")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			log, err := os.CreateTemp(t.TempDir(), "daemon-log")
			if err != nil {
				t.Fatal(err)
			}
			old := os.Stderr
			os.Stderr = log
			defer func() { os.Stderr = old; log.Close() }()
			res, checkErr := rig.d.specCheck(context.Background(), SpecCheckParams{Project: "p", Items: []string{"S2"}})
			os.Stderr = old
			b, _ := os.ReadFile(log.Name())
			if checkErr != nil {
				t.Fatalf("invalid answer blocked re-check: %v", checkErr)
			}
			if res.Items[0].Ready || len(res.Items[0].Questions) != 2 {
				t.Fatalf("questions not asked again: %+v", res)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("invalid answer remains: %v", err)
			}
			if !strings.Contains(string(b), path) {
				t.Errorf("log lacks full path %s: %s", path, b)
			}
		})
	}
}

func TestSpecSweepRemovesDoneRetiredUnregisteredAndLegacy(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11), clitest.Route{Tool: "slicer", Args: []string{"list"}, Prefix: true, Stdout: `[{"id":"S1","status":"open"},{"id":"S2","status":"done"},{"id":"S3","status":"retired"}]`})
	old := time.Now().Add(-15 * 24 * time.Hour)
	var dirs []string
	for _, pair := range [][2]string{{"p", "S1"}, {"p", "S2"}, {"p", "S3"}, {"removed", "S1"}} {
		dir := rig.d.specDir(pair[0], pair[1])
		if err := writeSpecJSON(filepath.Join(dir, "check.json"), specCheckIdentity{Project: pair[0], Item: pair[1]}); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, dir)
	}
	for _, name := range []string{"20261001-120000-abcd", "20261027-120000-1234", "unidentified"} {
		dir := filepath.Join(rig.d.cfg.Paths.Dir, "spec-checks", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, dir)
	}
	for i, dir := range dirs {
		if i != 5 {
			if err := os.Chtimes(dir, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := rig.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i, dir := range dirs {
		_, err := os.Stat(dir)
		wantRemoved := i == 1 || i == 2 || i == 3 || i == 4
		if errors.Is(err, os.ErrNotExist) != wantRemoved {
			t.Errorf("%s removal=%v, want %v", dir, err, wantRemoved)
		}
	}
	for _, call := range rig.fake.CallsTo("slicer") {
		for _, arg := range call.Args {
			if arg == "--all" {
				t.Fatal("sweep used --all")
			}
		}
	}
}

func TestSpecRetryFailureHasAnActionableWireCode(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.report = "bad shape"
	_, err := rig.d.specCheck(context.Background(), SpecCheckParams{Project: "p", Items: []string{"S1"}})
	wire := ipc.ErrorOf(err)
	if wire == nil || wire.Code == ipc.CodeInternal || !strings.Contains(wire.Message, "retry") || !strings.Contains(wire.Message, "check again") {
		t.Fatalf("retry failure wire: %+v", wire)
	}
}

func TestSpecCheckRefusesABlockedItemBeforeAnyRole(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11), clitest.Route{Tool: "slicer", Args: []string{"deps", "S2"}, Prefix: true, Stdout: `{"id":"S2","blocked_by":["S9"]}`})
	rig.d.runs.deps.Runner = roundRunner(func(context.Context, engine.Request) (engine.Result, error) {
		t.Error("blocked spec check ran a role")
		return engine.Result{}, nil
	})
	_, err := rig.d.handle(context.Background(), MethodSpecCheck, json.RawMessage(`{"project":"p","items":["S2"]}`))
	if err == nil || ipc.ErrorOf(err).Code != CodeRefused || ipc.ErrorOf(err).Message != `S2 waits on S9 "Title" (not done); land S9 first` {
		t.Errorf("error: %v", err)
	}
	if runs, err := rig.store.List(); err != nil || len(runs) != 0 {
		t.Errorf("runs: %v %v", runs, err)
	}
	for _, call := range rig.fake.CallsTo("slicer") {
		if call.Args[0] != "deps" && call.Args[0] != "show" {
			t.Errorf("call before dependency refusal: %+v", call)
		}
	}
}

func TestRunStartRefusesAnItemWithOpenSpecQuestions(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.check(t, "S1", "S2")
	_, err := rig.d.handle(context.Background(), MethodRunStart, json.RawMessage(`{"project":"p","item":"S2"}`))
	var wire *ipc.Error
	if !errors.As(err, &wire) || wire.Code != CodeSpecQuestions {
		t.Fatalf("run.start error: %v, want spec_questions", err)
	}
	if runs, err := rig.store.List(); err != nil || len(runs) != 0 {
		t.Fatalf("refused start wrote runs: %v %v", runs, err)
	}
	if len(rig.roleRequests()) != 0 {
		t.Fatal("refused start ran a role")
	}
}

func TestRunStartDoesNotRefuseAnItemNeverChecked(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			rig := newSpecRig(t, rulesJSON(u11))
			if changed {
				rig.check(t, "S1", "S2")
				rig.setSpec(t, "S2", "changed spec")
			}
			b := request(t, rig.d, MethodRunStart, `{"project":"p","item":"S2"}`)
			var rec runrec.Record
			if err := json.Unmarshal(b, &rec); err != nil || rec.ID == "" {
				t.Fatalf("start: %s %v", b, err)
			}
		})
	}
}

func TestSingleRunCarriesSpecAnswersIntoTheBrief(t *testing.T) {
	rig := answeredSpecRig(t)
	b := request(t, rig.d, MethodRunStart, `{"project":"p","item":"S2"}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	want := "Which store holds the index? -> memory [cache.index-store]. What names the flag? -> long."
	deadline := time.Now().Add(5 * time.Second)
	for len(rig.roleRequests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	reqs := rig.roleRequests()
	if len(reqs) == 0 || reqs[0].Role != engine.RoleImplementer || !strings.Contains(reqs[0].Brief, "## Owner decisions") || !strings.Contains(reqs[0].Brief, want) {
		t.Fatalf("implementer requests lack answers: %+v", reqs)
	}
	stored, err := rig.store.Open(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Decisions, want) || !strings.Contains(stored.Snapshot().Decisions, want) {
		t.Fatalf("decisions missing: %+v", rec)
	}
}
