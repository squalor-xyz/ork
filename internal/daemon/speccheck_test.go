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

const overrideCheck = `{"project":"p","items":["S1"],"lineups":{"S1":{"implementer":[{"vendor":"claude-local","model":"qwen3.8-27b","effort":"low"}]}}}`

func overrideSpecRig(t *testing.T) *specRig {
	t.Helper()
	rig := newSpecRig(t, rulesJSON(u11))
	rig.report = "## Slice S1\nReady\n\n## Owner needed\nnone\n\n## Orientation S1\nquoted source and test\n"
	return rig
}

func TestSpecCheckUsesRunLineupOverride(t *testing.T) {
	rig := overrideSpecRig(t)
	request(t, rig.d, MethodSpecCheck, overrideCheck)
	if !strings.Contains(rig.requests()[0].Brief, "Write `## Orientation S1`") {
		t.Error("local override did not request Orientation S1")
	}
	pack, err := os.ReadFile(filepath.Join(rig.d.specDir("p", "S1"), "orientation.md"))
	if err != nil || string(pack) != "quoted source and test" {
		t.Errorf("override spec pack: %q, %v", pack, err)
	}
}

func TestPollerSpecCheckUsesMatrixLineup(t *testing.T) {
	rig := overrideSpecRig(t)
	rig.check(t, "S1")
	if strings.Contains(rig.requests()[0].Brief, "Write `## Orientation S1`") {
		t.Error("cloud matrix requested Orientation S1")
	}
	pack, err := os.ReadFile(filepath.Join(rig.d.specDir("p", "S1"), "orientation.md"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cloud matrix saved pack: %q, %v", pack, err)
	}
}

func TestLocalRunCopiesPackFromOverrideCheck(t *testing.T) {
	rig := overrideSpecRig(t)
	request(t, rig.d, MethodSpecCheck, overrideCheck)
	rec, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1", Lineup: map[string][]runrec.LineupEntry{engine.RoleImplementer: {{Vendor: "claude-local", Model: "qwen3.8-27b", Effort: "low"}}}})
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, rig.d, rec.ID)
	pack, err := os.ReadFile(filepath.Join(rig.store.RunDir(rec.ID), "orientation.md"))
	if err != nil || string(pack) != "quoted source and test" {
		t.Errorf("override run pack: %q, %v", pack, err)
	}
}

func TestLocalOverrideRechecksCachedVerdictWithoutPack(t *testing.T) {
	rig := overrideSpecRig(t)
	rig.check(t, "S1")
	request(t, rig.d, MethodSpecCheck, overrideCheck)
	pack, err := os.ReadFile(filepath.Join(rig.d.specDir("p", "S1"), "orientation.md"))
	if err != nil || string(pack) != "quoted source and test" {
		t.Errorf("cached cloud verdict did not regenerate local pack: %q, %v", pack, err)
	}
}

func TestLocalRunWithoutPackAfterSkippedOrFailedCheck(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			rig := newSpecRig(t, rulesJSON(u11))
			if failed {
				dir := rig.d.specDir("p", "S1")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "orientation.md"), []byte("stale pack"), 0600); err != nil {
					t.Fatal(err)
				}
				rig.report = "malformed"
				_, err := rig.d.specCheck(context.Background(), SpecCheckParams{Project: "p", Items: []string{"S1"}})
				if err == nil {
					t.Fatal("malformed fake spec check unexpectedly succeeded")
				}
			}
			rec, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1", Lineup: map[string][]runrec.LineupEntry{engine.RoleImplementer: {{Vendor: "claude-local", Model: "qwen3.8-27b", Effort: "medium"}}}})
			if err != nil {
				t.Fatal(err)
			}
			waitIdle(t, rig.d, rec.ID)
			for _, req := range rig.roleRequests() {
				if strings.Contains(req.Brief, "## Orientation") {
					t.Error("skipped or failed spec check supplied an orientation")
				}
			}
		})
	}
}

func TestOrientationSpecToRun(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprint(local), func(t *testing.T) {
			agent := "Claude Code"
			if local {
				agent = "claude-local"
			}
			recommend := fmt.Sprintf(`{"roles":[{"role":"implementer","row":{"agent":%q,"model":"qwen3.8-27b","reasoning":"medium"}},{"role":"reviewer","row":{"id":"r5","agent":"Claude Code","model":"opus","reasoning":"medium"},"alternatives":[{"id":"r16","agent":"Claude Code","model":"sonnet","reasoning":"medium"}]}]}`, agent)
			rig := newSpecRig(t, rulesJSON(u11), clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true, Stdout: recommend})
			rig.report = "## Slice S1\nReady\n\n## Owner needed\nnone\n\n## Orientation S1\nquoted source and test\n"
			rig.check(t, "S1")
			if strings.Contains(rig.requests()[0].Brief, "Write `## Orientation S1`") != local {
				t.Error("spec prompt ignored planned implementer")
			}
			pack, err := os.ReadFile(filepath.Join(rig.d.specDir("p", "S1"), "orientation.md"))
			if local {
				if err != nil || string(pack) != "quoted source and test" {
					t.Errorf("saved spec pack: %q, %v", pack, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("cloud spec pack: %q, %v", pack, err)
			}
			rec, err := rig.d.startRun(context.Background(), RunParams{Project: "p", Item: "S1"})
			if err != nil {
				t.Fatal(err)
			}
			waitIdle(t, rig.d, rec.ID)
			pack, err = os.ReadFile(filepath.Join(rig.store.RunDir(rec.ID), "orientation.md"))
			if local {
				if err != nil || string(pack) != "quoted source and test" {
					t.Errorf("saved run pack: %q, %v", pack, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("cloud run pack: %q, %v", pack, err)
			}
		})
	}
}

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

func TestSpecCheckReusesAdjDecision(t *testing.T) {
	for _, tc := range []struct {
		name, output, notice string
		exit                 int
		ready, waiting       bool
	}{
		{"decided", `{"decided":[{"id":42,"key":"cache.index-store","decision":"memory","options":[{"label":"memory","summary":"rebuilt at each start"}],"decided_at":"2026-11-09"}]}`, "", 0, true, false},
		{"open", `{"open":[{"id":42,"key":"cache.index-store"}]}`, "", 0, false, true},
		{"not found", `{"result":"not found"}`, "", 0, false, false},
		{"near key", `{"decided":[{"id":42,"key":"cache.index-store-other","decision":"memory","options":[{"label":"memory","summary":"rebuilt at each start"}]}]}`, "", 0, false, false},
		{"stale", `{"decided":[{"id":42,"key":"cache.index-store","decision":"disk","options":[{"label":"disk","summary":"old store"}],"decided_at":"2026-11-09"}]}`, "decided as disk: old store on 2026-11-09", 0, false, false},
		{"failure", "", "lookup unavailable", 1, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			route := clitest.Route{Tool: "adj", Args: []string{"check", "cache.index-store", "--json"}, Prefix: true, Stdout: tc.output, Stderr: "lookup unavailable", Exit: tc.exit}
			if tc.waiting {
				route.Seq = []clitest.Reply{{Stdout: tc.output}, {Stdout: `{"decided":[{"id":42,"key":"cache.index-store","decision":"memory","options":[{"label":"memory","summary":"rebuilt at each start"}]}]}`}}
			}
			rig := newSpecRig(t, rulesJSON(u11),
				clitest.Route{Tool: "adj", Args: []string{"contract", "--json"}, Prefix: true, Stdout: `{"contract":"adj-json v1","version":1,"pinned":null}`},
				route)
			reg, err := registry.Load(rig.d.cfg.RegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			reg.Projects[0].Adj = true
			if err := registry.Save(rig.d.cfg.RegistryPath, reg); err != nil {
				t.Fatal(err)
			}
			rig.d.runs.deps.Adj = adjcli.Client{Bin: rig.fake.Bin("adj")}
			first, _, _ := strings.Cut(specQuestionsReport, "\n\nQuestion: What")
			rig.report = first + "\n\n## Owner needed\nnone\n"
			result := rig.check(t, "S2")
			item := rig.item(result, "S2")
			if item.Ready != tc.ready {
				t.Errorf("ready = %v, want %v", item.Ready, tc.ready)
			}
			wantQuestions := 1
			if tc.ready || tc.waiting {
				wantQuestions = 0
			}
			if len(item.Questions) != wantQuestions {
				t.Errorf("questions = %d, want %d", len(item.Questions), wantQuestions)
			}
			raw, _ := json.Marshal(item)
			if tc.waiting && !strings.Contains(string(raw), `"waits_on":[42]`) {
				t.Errorf("missing open hold: %s", raw)
			}
			if tc.notice != "" && !strings.Contains(string(raw), tc.notice) {
				t.Errorf("missing notice %q: %s", tc.notice, raw)
			}
			if tc.name == "failure" && strings.Count(string(raw), "lookup unavailable") != 1 {
				t.Errorf("lookup notice must appear once: %s", raw)
			}
			calls := rig.fake.CallsTo("adj")
			if len(calls) != 1 || calls[0].Args[0] != "check" {
				t.Errorf("expected read-only exact lookup: %+v", calls)
			}
			if tc.ready {
				var paragraphs []string
				for _, call := range rig.fake.CallsTo("slicer") {
					if len(call.Args) > 1 && call.Args[0] == "edit" {
						paragraphs = append(paragraphs, strings.Join(call.Args, " "))
					}
				}
				if len(paragraphs) != 1 || !strings.Contains(paragraphs[0], "-> memory: rebuilt at each start [cache.index-store, adj 42]") {
					t.Errorf("missing reused decision: %v", paragraphs)
				}
			}
			if tc.ready || tc.waiting {
				err := rig.d.specGate(context.Background(), "p", "S2")
				if (err != nil) != tc.waiting {
					t.Errorf("launch guard: %v", err)
				}
				if tc.waiting && (err == nil || !strings.Contains(err.Error(), "waits on adj 42")) {
					t.Errorf("hold reason: %v", err)
				}
			}
			if tc.ready {
				b := request(t, rig.d, MethodWaveStart, `{"items":[{"project":"p","item":"S2"}]}`)
				if !strings.Contains(string(b), `"run"`) {
					t.Fatalf("reused decision did not permit launch: %s", b)
				}
				deadline := time.Now().Add(5 * time.Second)
				for len(rig.roleRequests()) == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				reqs := rig.roleRequests()
				if len(reqs) == 0 || !strings.Contains(reqs[0].Brief, "-> memory: rebuilt at each start [cache.index-store, adj 42]") {
					t.Fatalf("reused decision missing from implementer brief: %+v", reqs)
				}
			}
			if tc.waiting {
				after := rig.check(t, "S2")
				if !after.Items[0].Ready || len(after.Items[0].Questions) > 0 || len(rig.requests()) != 1 {
					t.Fatalf("decided pending record did not release cached hold: %+v", after)
				}
				body, err := os.ReadFile(filepath.Join(rig.d.specDir("p", "S2"), "answers.json"))
				var stored storedSpecAnswers
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(body, &stored); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(stored.Decisions, "-> memory: rebuilt at each start [cache.index-store, adj 42]") {
					t.Fatalf("resolved hold lost saved answer: %s", body)
				}
			}
		})
	}
}

// specRig is a daemon whose matrix has a spec check row and whose spec check
// role answers with the report in rig.report. S1 and S2 are the project's
// slices; their `slicer show` output is in files the test can rewrite.
type specRig struct {
	d      *Daemon
	store  *runrec.Store
	fake   *clitest.Fake
	mu     sync.Mutex
	report string
	carve  string // when set, the answer to a carve round (S359); the other rounds answer report
	// onCarve runs when a carve round starts, before it answers (S359): a test
	// uses it to change what `slicer list` finds afterwards.
	onCarve func()
	reqs    []engine.Request
	others  []engine.Request // the rounds of the runs that start afterwards
	gate    chan struct{}    // when set, the role waits for it
	root    string
	draft   string           // the report of the spec-draft role (S179)
	drafts  []engine.Request // the rounds the spec-draft role was asked for
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
	recommend := `{"roles":[{"role":"implementer","row":` + row("r3", "sonnet", "high") + `},{"role":"reviewer","row":` + row("r5", "opus", "medium") + `,"alternatives":[` + row("r16", "sonnet-spec", "low") + `,` + row("r24", "grok-spec", "medium") + `,{"id":"r30","agent":"Acme Agent","vendor":"Acme","model":"acme-spec","reasoning":"low"}]},{"role":"architect","row":null,"alternatives":[],"selected_by":"unmatched"}]}`
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
		clitest.Route{Tool: "slicer", Args: []string{"edit", "S1"}, Prefix: true, Stdout: `{"id":"S1"}`},
	)
	rig.d, rig.store, _ = runDaemon(t, func(ctx context.Context, r engine.Request) (engine.Result, error) {
		if r.Role == engine.RoleSpecDraft {
			rig.mu.Lock()
			rig.drafts = append(rig.drafts, r)
			report := rig.draft
			rig.mu.Unlock()
			return engine.Result{Exit: engine.ExitOK, Report: report}, nil
		}
		if r.Role != engine.RoleSpecCheck {
			rig.mu.Lock()
			rig.others = append(rig.others, r)
			rig.mu.Unlock()
			return engine.Result{Exit: engine.ExitAuth}, nil
		}
		rig.mu.Lock()
		rig.reqs = append(rig.reqs, r)
		report, gate := rig.report, rig.gate
		onCarve := rig.onCarve
		if rig.carve != "" && strings.Contains(r.Brief, "Role: carve.") {
			report = rig.carve
		} else {
			onCarve = nil
		}
		rig.mu.Unlock()
		if onCarve != nil {
			onCarve()
		}
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

func TestSpecCheckReportsAMissingSpecPerItem(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.mu.Lock()
	rig.report = "## Slice S1\nReady\n\n## Owner needed\n\nnone\n"
	rig.mu.Unlock()
	if err := os.WriteFile(filepath.Join(rig.root, "S2.json"), []byte(`{"id":"S2","title":"T","slice":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res := rig.check(t, "S1", "S2")
	if len(res.Items) != 2 || res.Items[0].Item != "S1" || res.Items[1].Item != "S2" {
		t.Fatalf("want one row for each item, in order: %+v", res)
	}
	if s1 := res.Items[0]; !s1.Ready || s1.NoSpec {
		t.Errorf("S1: %+v", s1)
	}
	if s2 := res.Items[1]; !s2.NoSpec || s2.Ready || len(s2.Questions) != 0 {
		t.Errorf("S2: %+v", s2)
	}
	reqs := rig.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d role rounds, want one for S1", len(reqs))
	}
	if strings.Contains(reqs[0].Brief, `"id":"S2"`) {
		t.Errorf("the role was asked about the item with no spec: %q", reqs[0].Brief[:min(len(reqs[0].Brief), 200)])
	}
	// Every item lacks a spec: no role runs and every row says so.
	if err := os.WriteFile(filepath.Join(rig.root, "S1.json"), []byte(`{"id":"S1","slice":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res = rig.check(t, "S1", "S2")
	if len(res.Items) != 2 || !res.Items[0].NoSpec || !res.Items[1].NoSpec || len(rig.requests()) != 1 {
		t.Errorf("all without a spec: %+v, %d role rounds", res, len(rig.requests()))
	}
}

func TestSpecCheckStillFailsOnOtherShowErrors(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11), clitest.Route{Tool: "slicer", Args: []string{"show", "S2"}, Prefix: true, Stderr: "boom", Exit: 2})
	_, err := rig.d.handle(context.Background(), MethodSpecCheck, json.RawMessage(`{"project":"p","items":["S1","S2"]}`))
	if err == nil {
		t.Fatal("a failed slicer show must fail the whole call")
	}
	if len(rig.requests()) != 0 {
		t.Error("a role ran after a failed read")
	}
	// A show that is not JSON is not a missing spec either.
	rig2 := newSpecRig(t, rulesJSON(u11))
	rig2.mu.Lock()
	rig2.report = "## Slice S1\nReady\n\n## Owner needed\n\nnone\n"
	rig2.mu.Unlock()
	if werr := os.WriteFile(filepath.Join(rig2.root, "S2.json"), []byte(`nope`), 0o644); werr != nil {
		t.Fatal(werr)
	}
	_, err = rig2.d.handle(context.Background(), MethodSpecCheck, json.RawMessage(`{"project":"p","items":["S1","S2"]}`))
	if err == nil || errors.Is(err, engine.ErrNoSpec) || !strings.Contains(err.Error(), "S2: slicer show") {
		t.Fatalf("malformed show output: %v", err)
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
			b := request(t, rig.d, MethodSpecAnswer, `{"project":"p","item":"S2","answers":["memory: rebuilt at each start","long: --extra"],"adj":true,"by":"jon"}`)
			var ans SpecAnswerResult
			if err := json.Unmarshal(b, &ans); err != nil || !ans.Ready || len(ans.Notices) != 1 {
				t.Fatalf("answer: %s %v", b, err)
			}
			calls := rig.fake.CallsTo("adj")
			if hasAdj {
				if len(calls) != 4 || ans.Notices[0] != "filed in adj: cache.index-store (adj 42)" {
					t.Errorf("adj calls %+v, notices %v", calls, ans.Notices)
				}
			} else if len(calls) != 0 || !strings.Contains(ans.Notices[0], "no adj in this project") {
				t.Errorf("adj calls %+v, notices %v", calls, ans.Notices)
			}
		})
	}
}

// ownerRig is a spec rig whose project has adj and whose registry holds the
// owner settings. The adj client is the rig's fake: no test reaches a real
// adj store.
func ownerRig(t *testing.T, owner string, fileAnswers *bool) *specRig {
	t.Helper()
	rig := newSpecRig(t, rulesJSON(u11),
		clitest.Route{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: `{"result":"not found"}`},
		clitest.Route{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "42\n"},
		clitest.Route{Tool: "adj", Args: []string{"decide"}, Prefix: true, Stdout: "42\n"})
	f, err := registry.Load(rig.d.cfg.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	f.Projects[0].Adj = true
	f.Owner, f.FileAnswers = owner, fileAnswers
	if err := registry.Save(rig.d.cfg.RegistryPath, f); err != nil {
		t.Fatal(err)
	}
	rig.d.runs.deps.Adj = adjcli.Client{Bin: rig.fake.Bin("adj")}
	rig.report = specQuestionsReport + "\n## Owner needed\n\nnone\n"
	rig.check(t, "S2")
	return rig
}

func ownerAnswer(t *testing.T, rig *specRig, by string) SpecAnswerResult {
	t.Helper()
	params := `{"project":"p","item":"S2","answers":["memory: rebuilt at each start","long: --extra"],"adj":true`
	if by != "" {
		params += `,"by":"` + by + `"`
	}
	b := request(t, rig.d, MethodSpecAnswer, params+`}`)
	var ans SpecAnswerResult
	if err := json.Unmarshal(b, &ans); err != nil || !ans.Ready {
		t.Fatalf("answer: %s %v", b, err)
	}
	return ans
}

func decideCall(t *testing.T, rig *specRig) string {
	t.Helper()
	for _, c := range rig.fake.CallsTo("adj") {
		if c.Args[0] == "decide" {
			return strings.Join(c.Args, " ")
		}
	}
	t.Fatalf("no adj decide call: %+v", rig.fake.CallsTo("adj"))
	return ""
}

// With the owner in ork's settings and no ADJ_DECIDED_BY in the caller, the
// daemon files the answer as the owner (S314, *proposed*).
func TestSpecAnswerFilesAsTheOwnerInTheSettingsWhenTheCallerHasNoName(t *testing.T) {
	rig := ownerRig(t, "Jon Setting", nil)
	ans := ownerAnswer(t, rig, "")
	if len(ans.Notices) != 1 || ans.Notices[0] != "filed in adj: cache.index-store (adj 42)" {
		t.Fatalf("notices: %q", ans.Notices)
	}
	decide := decideCall(t, rig)
	if !strings.Contains(decide, "--by Jon Setting") || !strings.Contains(decide, "--why Answered in ork on ") || !strings.Contains(decide, "(S2)") {
		t.Errorf("decide: %s", decide)
	}
}

func TestSpecAnswerPrefersTheCallersNameOverTheSetting(t *testing.T) {
	rig := ownerRig(t, "Jon Setting", nil)
	ownerAnswer(t, rig, "Jon Env")
	if decide := decideCall(t, rig); !strings.Contains(decide, "--by Jon Env") || strings.Contains(decide, "Jon Setting") {
		t.Errorf("decide: %s", decide)
	}
}

func TestSpecAnswerWithNoOwnerFilesNothingAndSaysHowToSetOne(t *testing.T) {
	rig := ownerRig(t, "", nil)
	ans := ownerAnswer(t, rig, "")
	if len(ans.Notices) != 1 || !strings.Contains(ans.Notices[0], "no owner set; run ork settings set --owner NAME") {
		t.Fatalf("notices: %q", ans.Notices)
	}
	if calls := rig.fake.CallsTo("adj"); len(calls) != 1 || calls[0].Args[0] != "check" {
		t.Errorf("expected only the spec lookup without an owner: %+v", calls)
	}
	// The answer is recorded in the slice all the same.
	if edits := rig.fake.CallsTo("slicer"); len(edits) == 0 {
		t.Error("no slicer call")
	}
	if got := ownerStored(t, rig); !strings.Contains(got, "memory: rebuilt at each start") {
		t.Errorf("the answers are not kept for the run: %q", got)
	}
}

func TestSpecAnswerFilesNothingWhenFileAnswersIsOff(t *testing.T) {
	off := false
	rig := ownerRig(t, "Jon Setting", &off)
	ans := ownerAnswer(t, rig, "Jon Env")
	if calls := rig.fake.CallsTo("adj"); len(calls) != 1 || calls[0].Args[0] != "check" {
		t.Errorf("expected only the spec lookup with file-answers off: %+v", calls)
	}
	if len(ans.Notices) != 0 {
		t.Errorf("notices: %q", ans.Notices)
	}
}

// ownerStored returns the decisions paragraph the daemon keeps for S2.
func ownerStored(t *testing.T, rig *specRig) string {
	t.Helper()
	rig.d.runs.mu.Lock()
	defer rig.d.runs.mu.Unlock()
	return rig.d.runs.specs[specKey("p", "S2")].decisions
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

// An answers.json written before the engine cleaned the Owner needed text still
// holds a none followed by a fence and prose; loading it shows no owner text.
func TestStoredOwnerTextIsCleanedOnLoad(t *testing.T) {
	rig := answeredSpecRig(t)
	path := filepath.Join(rig.d.specDir("p", "S2"), "answers.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored storedSpecAnswers
	if err := json.Unmarshal(b, &stored); err != nil {
		t.Fatal(err)
	}
	stored.OwnerNeeded = strings.Join([]string{"none", "```", "prose"}, "\n")
	if err := writeSpecJSON(path, stored); err != nil {
		t.Fatal(err)
	}
	restartSpecRig(t, rig)
	res := rig.check(t, "S2")
	if !rig.item(res, "S2").Ready || res.OwnerNeeded != "" || len(rig.requests()) != 1 {
		t.Fatalf("reply %+v after %d checks, want S2 ready with no owner text", res, len(rig.requests()))
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
	if wire == nil || wire.Code == ipc.CodeInternal || !strings.Contains(wire.Message, "retry") || !strings.Contains(wire.Message, "Check again") || !strings.Contains(wire.Message, "S1") || !strings.Contains(wire.Message, "safe") {
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

// setSpecCheckRows stores the project's spec_check_rows setting in the
// registry, as `ork projects set p --spec-check-rows` would.
func setSpecCheckRows(t *testing.T, d *Daemon, rows ...string) registry.Project {
	t.Helper()
	f, err := registry.Load(d.registryPath())
	if err != nil {
		t.Fatal(err)
	}
	f.Projects[0].SpecCheckRows = rows
	if err := registry.Save(d.registryPath(), f); err != nil {
		t.Fatal(err)
	}
	return f.Projects[0]
}

func specChainOf(t *testing.T, rig *specRig) ([]runrec.LineupEntry, error) {
	t.Helper()
	f, err := registry.Load(rig.d.registryPath())
	if err != nil {
		t.Fatal(err)
	}
	proj := f.Projects[0]
	return rig.d.specChain(context.Background(), rig.d.runDeps(proj), proj, "S1")
}

func TestSpecChainUsesTheProjectSetting(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	setSpecCheckRows(t, rig.d, "r24", "r30", "r16")
	chain, err := specChainOf(t, rig)
	if err != nil {
		t.Fatal(err)
	}
	// The setting's order, not the matrix's; r30 has no adapter and is left out.
	want := []runrec.LineupEntry{{Vendor: "claude", Model: "grok-spec", Effort: "medium"}, {Vendor: "claude", Model: "sonnet-spec", Effort: "low"}}
	if !reflect.DeepEqual(chain, want) {
		t.Fatalf("chain %+v, want %+v", chain, want)
	}
	rig.report = "## Slice S1\nReady\n\n## Owner needed\n\nnone\n"
	rig.check(t, "S1")
	if reqs := rig.requests(); len(reqs) != 1 || reqs[0].Model != "grok-spec" || reqs[0].Effort != "medium" {
		t.Fatalf("the spec check ran on %+v, want the first listed row", reqs)
	}
}

func TestSpecCheckLocalFirstFallsBackToCloud(t *testing.T) {
	recommend := `{"roles":[{"role":"reviewer","row":` + localCheckRow("r35") + `,"alternatives":[` + matrixRow("r16", "sonnet-spec", "low") + `]}]}`
	rig := newSpecRig(t, rulesJSON(u11, localCheckRule), clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true, Stdout: recommend})
	setSpecCheckRows(t, rig.d, "r35", "r16")
	localFailed := false
	rig.d.runs.deps.Runner = roundRunner(func(_ context.Context, req engine.Request) (engine.Result, error) {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		if req.Vendor == "claude-local" {
			localFailed = true
			return engine.Result{Exit: engine.ExitRateLimited}, nil
		}
		// The fallback report proves that the failed local row ran first.
		if !localFailed {
			return engine.Result{Exit: engine.ExitUnclassified}, nil
		}
		return engine.Result{Exit: engine.ExitOK, Report: "## Slice S1\nReady\n\n## Owner needed\n\nnone\n"}, nil
	})
	chain, err := specChainOf(t, rig)
	if err != nil {
		t.Fatal(err)
	}
	want := []runrec.LineupEntry{{Vendor: "claude-local", Model: "qwen3.8-27b", Effort: "medium"}, {Vendor: "claude", Model: "sonnet-spec", Effort: "low"}}
	if !reflect.DeepEqual(chain, want) {
		t.Errorf("chain %+v, want %+v", chain, want)
	}
	res := rig.check(t, "S1")
	if !rig.item(res, "S1").Ready {
		t.Errorf("fallback result %+v", res)
	}
}

func TestSpecChainFallsBackToTheRuleWhenUnset(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	chain, err := specChainOf(t, rig)
	if err != nil {
		t.Fatal(err)
	}
	if want := []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet-spec", Effort: "low"}}; !reflect.DeepEqual(chain, want) {
		t.Fatalf("chain %+v, want %+v", chain, want)
	}
}

// The setting works with no rule at all.
func TestSpecChainNeedsNoRuleWhenTheProjectSetsRows(t *testing.T) {
	rig := newSpecRig(t, rulesJSON("A reviewer never wrote code."))
	setSpecCheckRows(t, rig.d, "r24")
	chain, err := specChainOf(t, rig)
	if err != nil {
		t.Fatal(err)
	}
	if want := []runrec.LineupEntry{{Vendor: "claude", Model: "grok-spec", Effort: "medium"}}; !reflect.DeepEqual(chain, want) {
		t.Fatalf("chain %+v, want %+v", chain, want)
	}
}

// leavesChains is the phrase of the line that says a spec check row leaves the chains.
const leavesChains = "leaves every run's reviewer and gate chains"

func TestSpecChainWithoutARowIsAnOwnerError(t *testing.T) {
	rig := newSpecRig(t, rulesJSON("A reviewer never wrote code."))
	_, err := specChainOf(t, rig)
	var re *ipc.Error
	if !errors.As(err, &re) || re.Code != CodeNoSpecCheckRow {
		t.Fatalf("error %v, want code %s", err, CodeNoSpecCheckRow)
	}
	// One line each: the problem, the fix with a real row, the rule, the rows,
	// the warning (owner decision 2026-11-07).
	lines := strings.Split(re.Message, "\n")
	if len(lines) != 5 || !strings.HasPrefix(lines[0], "no Spec check row is set") ||
		lines[1] != "fix: ork projects set p --spec-check-rows r5" ||
		lines[2] != `or:  slicer relay-roles rule-add ... "Spec check row (rNN)"` ||
		lines[3] != "reviewer rows with an adapter: r5, r16, r24" || !strings.Contains(lines[4], leavesChains) {
		t.Errorf("message:\n%s", re.Message)
	}
	for _, line := range lines {
		if len(line) > 80 {
			t.Errorf("line of %d columns: %q", len(line), line)
		}
	}
	// The same through the wire call: the owner sees the code, not internal.
	_, err = rig.d.handle(context.Background(), MethodSpecCheck, json.RawMessage(`{"project":"p","items":["S1"]}`))
	if got := ipc.ErrorOf(err); got.Code != CodeNoSpecCheckRow {
		t.Fatalf("spec.check error code %q", got.Code)
	}
	if len(rig.requests()) != 0 {
		t.Error("a role ran without a row")
	}
}

func TestSpecChainRowNotRecommendedIsAnOwnerError(t *testing.T) {
	const ruleHint = `or:  slicer relay-roles rule-add ... "Spec check row (rNN)"`
	const backToRule = `or:  ork projects set p --spec-check-rows ""`
	for _, tc := range []struct {
		name    string
		rule    bool
		set     []string
		want    []string
		missing []string
	}{
		{"rule", true, nil,
			[]string{"the spec check row (r99) is not among the reviewer rows", "\nfix: ork projects set p --spec-check-rows r5\n", "\n" + ruleHint + "\n", "\nreviewer rows with an adapter: r5, r16, r24\n", leavesChains},
			[]string{"change it", "change them", backToRule}},
		{"several set, rule names one", true, []string{"r98", "r99"},
			[]string{"the spec check rows (r98, r99) are not among the reviewer rows", "\nfix: change them with ork projects set p --spec-check-rows r5\n", "\n" + backToRule + "\n", leavesChains},
			[]string{"rule-add", " is not among"}},
		{"one set, no rule", false, []string{"r98"},
			[]string{"the spec check row (r98) is not among the reviewer rows", "\nfix: change it with ork projects set p --spec-check-rows r5\n", leavesChains},
			[]string{"rule-add", backToRule, " are not among"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules := rulesJSON("A reviewer never wrote code.")
			if tc.rule {
				rules = rulesJSON(strings.ReplaceAll(u11, "r16", "r99"))
			}
			rig := newSpecRig(t, rules)
			if tc.set != nil {
				setSpecCheckRows(t, rig.d, tc.set...)
			}
			_, err := specChainOf(t, rig)
			var re *ipc.Error
			if !errors.As(err, &re) || re.Code != CodeNoSpecCheckRow {
				t.Fatalf("error %v, want code %s", err, CodeNoSpecCheckRow)
			}
			for _, want := range tc.want {
				if !strings.Contains(re.Message, want) {
					t.Errorf("message lacks %q:\n%s", want, re.Message)
				}
			}
			for _, bad := range tc.missing {
				if strings.Contains(re.Message, bad) {
					t.Errorf("message has %q:\n%s", bad, re.Message)
				}
			}
		})
	}
}

// With no setting, every rule-named row is a spec check row, in rule order
// (owner decision 2026-11-07): the first runs and the next is its fallback.
func TestSpecChainWithoutASettingUsesEveryRuleRowInOrder(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11, "The Spec check row (r24) is the fallback."))
	chain, err := specChainOf(t, rig)
	if err != nil {
		t.Fatal(err)
	}
	want := []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet-spec", Effort: "low"}, {Vendor: "claude", Model: "grok-spec", Effort: "medium"}}
	if !reflect.DeepEqual(chain, want) {
		t.Fatalf("chain %+v, want %+v", chain, want)
	}
}

// grokFirstRig is a project whose spec check rows are r24 (grok) then r16
// (claude), as in the S379 finding.
func grokFirstRig(t *testing.T) *specRig {
	t.Helper()
	grok := `{"id":"r24","agent":"Grok","vendor":"xAI","model":"grok-4.7","reasoning":"medium"}`
	recommend := `{"roles":[{"role":"reviewer","row":` + grok + `,"alternatives":[` + matrixRow("r16", "sonnet-spec", "low") + `]}]}`
	rig := newSpecRig(t, rulesJSON(u11), clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true, Stdout: recommend})
	setSpecCheckRows(t, rig.d, "r24", "r16")
	rig.report = "## Slice S1\nReady\n\n## Owner needed\n\nnone\n"
	return rig
}

// A vendor marked out is skipped by the spec check, as run admission skips it
// (S379, *proposed*).
func TestSpecChainSkipsAVendorMarkedOut(t *testing.T) {
	rig := grokFirstRig(t)
	if err := rig.d.runs.deps.Vendors.MarkOutOwnerUntil("grok", "marked out by the owner", time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	chain, err := specChainOf(t, rig)
	if err != nil {
		t.Fatal(err)
	}
	if want := []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet-spec", Effort: "low"}}; !reflect.DeepEqual(chain, want) {
		t.Fatalf("chain %+v, want %+v", chain, want)
	}
	if res := rig.check(t, "S1"); !rig.item(res, "S1").Ready {
		t.Fatalf("result %+v", res)
	}
	if reqs := rig.requests(); len(reqs) != 1 || reqs[0].Vendor != "claude" || reqs[0].Model != "sonnet-spec" {
		t.Fatalf("the spec check ran %+v, want one round on r16", reqs)
	}
}

// A spec-check role that exits with the out class marks its vendor out and the
// check moves to the next row (S379, *proposed*).
func TestSpecCheckFallsBackOnAnOutExit(t *testing.T) {
	for _, class := range []engine.ExitClass{engine.ExitOutOfCredits, engine.ExitRateLimited} {
		t.Run(string(class), func(t *testing.T) {
			rig := grokFirstRig(t)
			var mu sync.Mutex
			var vendors []string
			rig.d.runs.deps.Runner = roundRunner(func(_ context.Context, req engine.Request) (engine.Result, error) {
				mu.Lock()
				defer mu.Unlock()
				vendors = append(vendors, req.Vendor)
				if req.Vendor == "grok" {
					return engine.Result{Exit: class, Reason: "grok is out"}, nil
				}
				return engine.Result{Exit: engine.ExitOK, Report: rig.report}, nil
			})
			res := rig.check(t, "S1")
			if !rig.item(res, "S1").Ready {
				t.Fatalf("result %+v", res)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(vendors, []string{"grok", "claude"}) {
				t.Errorf("rounds on %v, want grok then claude", vendors)
			}
			if !rig.d.runs.deps.Vendors.IsOut("grok") {
				t.Error("grok was not marked out")
			}
			if rig.d.runs.deps.Vendors.IsOut("claude") {
				t.Error("claude was marked out")
			}
		})
	}
}

// After a rate limit the check skips the vendor's other rows too: grok is out
// now, so r16 runs and not a second grok row (S379, *proposed*).
func TestSpecCheckSkipsTheOutVendorsOtherRowsAfterARateLimit(t *testing.T) {
	grok := func(id string) string {
		return `{"id":"` + id + `","agent":"Grok","vendor":"xAI","model":"grok-` + id + `","reasoning":"medium"}`
	}
	recommend := `{"roles":[{"role":"reviewer","row":` + grok("r24") + `,"alternatives":[` + grok("r25") + `,` + matrixRow("r16", "sonnet-spec", "low") + `]}]}`
	rig := newSpecRig(t, rulesJSON(u11), clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true, Stdout: recommend})
	setSpecCheckRows(t, rig.d, "r24", "r25", "r16")
	var mu sync.Mutex
	var models []string
	rig.d.runs.deps.Runner = roundRunner(func(_ context.Context, req engine.Request) (engine.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		models = append(models, req.Model)
		if req.Vendor == "grok" {
			return engine.Result{Exit: engine.ExitRateLimited}, nil
		}
		return engine.Result{Exit: engine.ExitOK, Report: "## Slice S1\nReady\n\n## Owner needed\n\nnone\n"}, nil
	})
	if res := rig.check(t, "S1"); !rig.item(res, "S1").Ready {
		t.Fatalf("result %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"grok-r24", "sonnet-spec"}; !reflect.DeepEqual(models, want) {
		t.Errorf("rounds on %v, want %v", models, want)
	}
}

// When every spec check row is on a vendor that is out, the check is refused
// and the message names the vendors and when each one ends (S379, *proposed*).
func TestSpecCheckAllRowsOutRefuses(t *testing.T) {
	rig := grokFirstRig(t)
	now := time.Now()
	grokUntil, claudeUntil := now.Add(time.Hour), now.Add(2*time.Hour)
	vendors := rig.d.runs.deps.Vendors
	if err := vendors.MarkOutOwnerUntil("grok", "marked out by the owner", now, grokUntil); err != nil {
		t.Fatal(err)
	}
	if err := vendors.MarkOutOwnerUntil("claude", "marked out by the owner", now, claudeUntil); err != nil {
		t.Fatal(err)
	}
	_, err := rig.d.handle(context.Background(), MethodSpecCheck, json.RawMessage(`{"project":"p","items":["S1"]}`))
	if err == nil {
		t.Fatal("the check ran with every row out")
	}
	got := ipc.ErrorOf(err)
	if got.Code != CodeRefused {
		t.Fatalf("error %v, want code %s", err, CodeRefused)
	}
	for _, want := range []string{"grok is out until " + grokUntil.Local().Format("15:04"), "claude is out until " + claudeUntil.Local().Format("15:04")} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message lacks %q:\n%s", want, got.Message)
		}
	}
	if len(rig.requests()) != 0 {
		t.Error("a role ran with every row out")
	}
}

func TestPlanKeepsEveryRuleNamedSpecCheckRowOutOfEveryChain(t *testing.T) {
	plan := uiPlan(t,
		[]string{matrixRow("r3", "sonnet", "high")},
		[]string{matrixRow("r5", "opus", "medium"), matrixRow("r16", "sonnet-spec", "low"), matrixRow("r24", "grok-spec", "medium"), matrixRow("r20", "haiku", "high")},
		u11, "The Spec check row (r24) is the fallback.")
	want := []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}, {Vendor: "claude", Model: "haiku", Effort: "high"}}
	for _, role := range []string{engine.RoleReviewer, engine.RoleGate} {
		if !reflect.DeepEqual(plan.Lineup[role], want) {
			t.Errorf("%s chain %+v, want %+v", role, plan.Lineup[role], want)
		}
	}
}

func TestPlanKeepsEverySpecCheckRowOutOfEveryChain(t *testing.T) {
	plan := uiPlanWith(t, []string{"r24"},
		[]string{matrixRow("r3", "sonnet", "high")},
		[]string{matrixRow("r5", "opus", "medium"), matrixRow("r16", "sonnet-spec", "low"), matrixRow("r24", "grok-spec", "medium"), matrixRow("r20", "haiku", "high")},
		"A reviewer never wrote code.", u11)
	want := []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}, {Vendor: "claude", Model: "haiku", Effort: "high"}}
	for _, role := range []string{engine.RoleReviewer, engine.RoleGate} {
		if !reflect.DeepEqual(plan.Lineup[role], want) {
			t.Errorf("%s chain %+v, want %+v", role, plan.Lineup[role], want)
		}
	}
}

// A project with no check command refuses the start before the spec gate, so
// the owner is not sent to answer spec questions for a start that is refused.
func TestRunStartRefusesNoCheckCommandBeforeTheSpecGate(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	rig.check(t, "S1", "S2")
	editProject(t, rig.d, func(p *registry.Project) { p.CheckCommand = "" })
	_, err := rig.d.handle(context.Background(), MethodRunStart, json.RawMessage(`{"project":"p","item":"S2"}`))
	if re := ipc.ErrorOf(err); err == nil || re.Code != CodeRefused || !strings.Contains(re.Message, "no check command") {
		t.Fatalf("run.start error: %v, want the no-check refusal", err)
	}
}
