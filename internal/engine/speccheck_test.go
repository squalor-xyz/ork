// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// specReport is a spec check report: S10 is ready, S11 has two questions, and
// only the first holds a Decision key.
const specReport = `## Slice S10
Ready

## Slice S11
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
3. none: no flag
Recommend: 2 - flags are spelled out

## Owner needed

none
`

func specSliceJSON(id, why string) string {
	return `{"id":"` + id + `","title":"T` + id + `","status":"ready","slice":{"id":"` + id + `","title":"T` + id + `","size":"M","sections":[{"heading":"Why","body":"` + why + `"}]}}`
}

// specHarness answers `slicer show` for S10 and S11 in the project checkout
// and git status in it with the given replies (one per call).
func specHarness(t *testing.T, status ...string) *harness {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			var seq []clitest.Reply
			for _, s := range status {
				seq = append(seq, clitest.Reply{Stdout: s})
			}
			if len(seq) == 0 {
				seq = []clitest.Reply{{}}
			}
			return []clitest.Route{
				{Tool: "slicer", Args: []string{"show", "S10", "--json", "--lean", "--root", P}, Stdout: specSliceJSON("S10", "ten")},
				{Tool: "slicer", Args: []string{"show", "S11", "--json", "--lean", "--root", P}, Stdout: specSliceJSON("S11", "eleven")},
				{Tool: "git", Args: []string{"-C", P, "status", "--porcelain"}, Seq: seq},
			}
		}
	})
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role != RoleSpecCheck {
			return Result{}, nil
		}
		return Result{Exit: ExitOK, Session: "sess", Report: specReport}, nil
	}
	return h
}

func specConfig(h *harness, t *testing.T) SpecCheckConfig {
	return SpecCheckConfig{
		Project: h.proj, Items: []string{"S10", "S11"}, Dir: t.TempDir(),
		Lineup: []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "medium"}},
	}
}

func TestSpecCheckMarksReadyAndQuestions(t *testing.T) {
	h := specHarness(t)
	res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Verdicts) != 2 {
		t.Fatalf("verdicts: %+v", res.Verdicts)
	}
	ready, asked := res.Verdicts[0], res.Verdicts[1]
	if ready.Item != "S10" || !ready.Ready || len(ready.Questions) != 0 {
		t.Errorf("S10: %+v", ready)
	}
	if asked.Item != "S11" || asked.Ready || len(asked.Questions) != 2 {
		t.Fatalf("S11: %+v", asked)
	}
	first, second := asked.Questions[0], asked.Questions[1]
	if first.Ask == nil || first.Ask.Title != "Which store holds the index?" || len(first.Ask.Options) != 2 || first.Ask.Recommend != 2 || first.Key != "cache.index-store" {
		t.Errorf("first question: %+v", first)
	}
	if second.Ask == nil || second.Ask.Title != "What names the flag?" || len(second.Ask.Options) != 3 || second.Key != "" {
		t.Errorf("second question: %+v", second)
	}
	for _, q := range asked.Questions {
		if !strings.HasPrefix(q.Text, "Question: ") {
			t.Errorf("raw block %q", q.Text)
		}
	}
	// One role round for the whole batch, read-only, in the project checkout.
	reqs := h.runner.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d role rounds, want 1", len(reqs))
	}
	req := reqs[0]
	if req.Role != RoleSpecCheck || !req.ReadOnly || len(req.Allowlist) != 0 || req.Worktree != h.P {
		t.Errorf("request: %+v", req)
	}
	for _, want := range []string{"Role: spec-check", "S10, S11", `"body":"eleven"`, `"id":"S11"`} {
		if !strings.Contains(req.Brief, want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	// The fingerprints identify the specs that were checked.
	if res.Specs["S10"] == "" || res.Specs["S10"] == res.Specs["S11"] {
		t.Errorf("fingerprints: %+v", res.Specs)
	}
}

func TestSpecCheckIsReadOnly(t *testing.T) {
	for name, status := range map[string][]string{
		"edits a file": {"", " M internal/engine/engine.go\n"},
		"adds a file":  {"", "?? notes.txt\n"},
		"changes dirt": {" M a.go\n", " M a.go\n M b.go\n"},
	} {
		t.Run(name, func(t *testing.T) {
			h := specHarness(t, status...)
			res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			if err == nil || !strings.Contains(err.Error(), "changed the tree") || !strings.Contains(err.Error(), "read-only") {
				t.Fatalf("error %v", err)
			}
			if res != nil {
				t.Errorf("a stopped check returned verdicts: %+v", res)
			}
		})
	}
}

func TestSpecCheckOnADirtyButUnchangedTreeIsFine(t *testing.T) {
	h := specHarness(t, " M a.go\n?? b.txt\n")
	if _, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t)); err != nil {
		t.Fatal(err)
	}
}

func TestSpecCheckStopsOnPermissionDenials(t *testing.T) {
	h := specHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		return Result{Exit: ExitError, Denials: []runrec.DeniedCommand{{Tool: "Bash", Command: "rm -rf /"}}}, nil
	}
	cfg := specConfig(h, t)
	cfg.Lineup = append(cfg.Lineup, runrec.LineupEntry{Vendor: "codex", Model: "gpt", Effort: "high"})
	_, err := RunSpecCheck(context.Background(), h.deps, cfg)
	if err == nil || !strings.Contains(err.Error(), "Bash: rm -rf /") {
		t.Fatalf("error %v", err)
	}
	if n := len(h.runner.requests()); n != 1 {
		t.Errorf("%d rounds: a denial must not fall back", n)
	}
}

func TestSpecCheckFallsBackOnRateLimit(t *testing.T) {
	h := specHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Vendor == "claude" {
			return Result{Exit: ExitRateLimited}, nil
		}
		return Result{Exit: ExitOK, Report: specReport}, nil
	}
	cfg := specConfig(h, t)
	cfg.Lineup = append(cfg.Lineup, runrec.LineupEntry{Vendor: "codex", Model: "gpt", Effort: "high"})
	res, err := RunSpecCheck(context.Background(), h.deps, cfg)
	if err != nil || len(res.Verdicts) != 2 {
		t.Fatalf("%v %+v", err, res)
	}
	reqs := h.runner.requests()
	if len(reqs) != 2 || reqs[1].Vendor != "codex" {
		t.Errorf("rounds: %+v", reqs)
	}
}

func TestSpecCheckRefusesAnEmptyLineupAndNoItems(t *testing.T) {
	h := specHarness(t)
	cfg := specConfig(h, t)
	cfg.Lineup = nil
	if _, err := RunSpecCheck(context.Background(), h.deps, cfg); err == nil || !strings.Contains(err.Error(), "lineup") {
		t.Errorf("empty lineup: %v", err)
	}
	cfg = specConfig(h, t)
	cfg.Items = nil
	if _, err := RunSpecCheck(context.Background(), h.deps, cfg); err == nil {
		t.Error("no items")
	}
	if n := len(h.runner.requests()); n != 0 {
		t.Errorf("%d rounds ran", n)
	}
}

func TestSpecCheckShapeErrorsAreReported(t *testing.T) {
	for name, report := range map[string]string{
		"missing slice":  "## Slice S10\nReady\n\n## Owner needed\n\nnone\n",
		"no owner":       "## Slice S10\nReady\n\n## Slice S11\nReady\n",
		"neither":        "## Slice S10\nReady\n\n## Slice S11\nLooks fine to me.\n\n## Owner needed\n\nnone\n",
		"bad block":      "## Slice S10\nReady\n\n## Slice S11\nQuestion: T\nOptions:\n1. a: A\nRecommend: 1 - x\n\n## Owner needed\n\nnone\n",
		"unknown slice":  "## Slice S10\nReady\n\n## Slice S11\nReady\n\n## Slice S99\nReady\n\n## Owner needed\n\nnone\n",
		"duplicate":      "## Slice S10\nReady\n\n## Slice S10\nReady\n\n## Slice S11\nReady\n\n## Owner needed\n\nnone\n",
		"ready and ask":  "## Slice S10\nReady\n\n## Slice S11\nReady\nQuestion: T\nOptions:\n1. a: A\n2. b: B\nRecommend: 1 - x\n\n## Owner needed\n\nnone\n",
		"empty report":   "",
		"free text only": "All good.\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := specHarness(t)
			h.runner.script = func(req Request, n int) (Result, error) {
				return Result{Exit: ExitOK, Report: report}, nil
			}
			res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			var se *ShapeError
			if !errors.As(err, &se) || res != nil {
				t.Fatalf("error %v, result %+v", err, res)
			}
		})
	}
}

func TestParseOwnerQuestionsReadsEveryBlock(t *testing.T) {
	body := `Some context.

Question: One?
Options:
1. a: A
2. b: B
Recommend: 1 - because
Decision key: area.one

Question: Two?
Options:
1. c: C
2. d: D
Recommend: 2 - since
`
	blocks, err := ParseOwnerQuestions(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks: %+v", blocks)
	}
	if blocks[0].Key != "area.one" || blocks[0].Ask.Title != "One?" || blocks[1].Key != "" || blocks[1].Ask.Title != "Two?" || blocks[1].Ask.Recommend != 2 {
		t.Errorf("blocks: %+v", blocks)
	}
	if strings.Contains(blocks[0].Text, "Two?") || strings.Contains(blocks[0].Text, "Some context") || !strings.Contains(blocks[0].Text, "Decision key: area.one") {
		t.Errorf("raw text of block 0: %q", blocks[0].Text)
	}
}

func TestParseOwnerQuestionsRefusals(t *testing.T) {
	good := "Question: One?\nOptions:\n1. a: A\n2. b: B\nRecommend: 1 - x\n"
	for name, body := range map[string]string{
		"none":              "none",
		"no block":          "just words",
		"dup key":           good + "Decision key: a.b\nDecision key: a.c\n",
		"bad key":           good + "Decision key: Not A Key\n",
		"one bad of two":    good + "\nQuestion: Two?\nOptions:\n1. a: A\nRecommend: 1 - x\n",
		"duplicate labels":  "Question: T\nOptions:\n1. a: A\n2. a: B\nRecommend: 1 - x\n",
		"recommend too big": "Question: T\nOptions:\n1. a: A\n2. b: B\nRecommend: 3 - x\n",
	} {
		if _, err := ParseOwnerQuestions(body); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if blocks, err := ParseOwnerQuestions(good); err != nil || len(blocks) != 1 {
		t.Errorf("good: %v %+v", err, blocks)
	}
}

func TestSpecFingerprintFollowsTheSpecOnly(t *testing.T) {
	a := specSliceJSON("S10", "ten")
	sameSpec := strings.Replace(a, `"status":"ready"`, `"status":"started","claim":{"owner":"x"}`, 1)
	otherSpec := specSliceJSON("S10", "ten, changed")
	fa, err := SpecFingerprint([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	fs, _ := SpecFingerprint([]byte(sameSpec))
	fo, _ := SpecFingerprint([]byte(otherSpec))
	if fa != fs {
		t.Error("a change of status or claim changed the fingerprint")
	}
	if fa == fo {
		t.Error("a change of a section did not change the fingerprint")
	}
	if _, err := SpecFingerprint([]byte(`{"id":"S10"}`)); err == nil {
		t.Error("an item without a slice has no spec to fingerprint")
	}
	if _, err := SpecFingerprint([]byte(`nope`)); err == nil {
		t.Error("bad JSON")
	}
}

func TestSpecCheckIsAReadOnlyRole(t *testing.T) {
	p, err := ResolvePermission(registry.Project{}, RoleSpecCheck)
	if err != nil || !p.ReadOnly || len(p.Allow) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	_, err = ResolvePermission(registry.Project{Permissions: map[string]registry.Permission{RoleSpecCheck: {Allow: []string{"Bash(rm *)"}}}}, RoleSpecCheck)
	if err == nil {
		t.Error("a registry allowlist for the spec check must be refused")
	}
}

// answerHarness answers the slicer edit and the adj calls of a recording.
func answerHarness(t *testing.T, editExit int) *harness {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			edit := clitest.Route{Tool: "slicer", Args: []string{"edit", "S11"}, Prefix: true, Stdout: `{"id":"S11"}`}
			if editExit != 0 {
				edit.Stdout, edit.Stderr, edit.Exit = "", `{"error":{"code":"usage","message":"no such section"}}`, editExit
			}
			return []clitest.Route{
				edit,
				{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: `{"result":"not found"}`},
				{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "42\n"},
				{Tool: "adj", Args: []string{"decide"}, Prefix: true, Stdout: "42\n"},
			}
		}
	})
	h.deps.Adj = adjcli.Client{Bin: h.fake.Bin("adj")}
	return h
}

func answerConfig(h *harness, adj bool, by string) SpecAnswerConfig {
	_, rest, _ := strings.Cut(specReport, "## Slice S11\n")
	body, _, _ := strings.Cut(rest, "## Owner needed")
	qs, err := ParseOwnerQuestions(body)
	if err != nil {
		panic(err)
	}
	return SpecAnswerConfig{Project: h.proj, Item: "S11", Questions: qs, Adj: adj, By: by,
		Answers: []string{"memory: rebuilt at each start", "  --extra\n"}}
}

func TestRecordSpecAnswersAppendsToTheImplementSection(t *testing.T) {
	h := answerHarness(t, 0)
	para, notices, err := RecordSpecAnswers(context.Background(), h.deps, answerConfig(h, false, ""))
	if err != nil || len(notices) != 0 {
		t.Fatalf("%v %v", notices, err)
	}
	wantPara := "Owner decisions (2026-10-19): Which store holds the index? -> memory: rebuilt at each start [cache.index-store]. What names the flag? -> --extra."
	if para != wantPara {
		t.Errorf("paragraph %q, want %q", para, wantPara)
	}
	edits := h.calls("slicer", "edit")
	if len(edits) != 1 {
		t.Fatalf("edits: %v", edits)
	}
	want := []string{"edit", "S11", "--section", "Implement", "--append", "--text",
		"Owner decisions (2026-10-19): Which store holds the index? -> memory: rebuilt at each start [cache.index-store]. What names the flag? -> --extra.",
		"--render", "--json", "--lean", "--root", h.P}
	if !reflect.DeepEqual(edits[0], want) {
		t.Errorf("edit args:\n%q\nwant\n%q", edits[0], want)
	}
	if n := len(h.fake.CallsTo("adj")); n != 0 {
		t.Errorf("%d adj calls with filing off", n)
	}
}

func TestRecordSpecAnswersFilesOnlyKeyedQuestionsInAdj(t *testing.T) {
	h := answerHarness(t, 0)
	_, notices, err := RecordSpecAnswers(context.Background(), h.deps, answerConfig(h, true, "jon"))
	if err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "cache.index-store") || !strings.Contains(notices[0], "decided in adj as #42") {
		t.Errorf("notices: %v", notices)
	}
	var verbs []string
	for _, c := range h.fake.CallsTo("adj") {
		verbs = append(verbs, c.Args[0])
	}
	if !reflect.DeepEqual(verbs, []string{"check", "ask", "decide"}) {
		t.Errorf("adj calls: %v", verbs)
	}
	decide := h.calls("adj", "decide")[0]
	if !strings.Contains(join(decide), "--text memory: rebuilt at each start") || !strings.Contains(join(decide), "--by jon") {
		t.Errorf("decide: %v", decide)
	}
	ask := h.calls("adj", "ask")[0]
	if !strings.Contains(join(ask), "--ref S11") || !strings.Contains(join(ask), "--key cache.index-store") {
		t.Errorf("ask: %v", ask)
	}
	// The slice is edited before anything goes to adj.
	all := h.fake.Calls()
	if all[len(all)-4].Tool != "slicer" {
		t.Errorf("order: %+v", all)
	}
}

func TestRecordSpecAnswersWithoutAnAuthorKeepsTheSliceEditAndSaysWhy(t *testing.T) {
	h := answerHarness(t, 0)
	_, notices, err := RecordSpecAnswers(context.Background(), h.deps, answerConfig(h, true, " "))
	if err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "ADJ_DECIDED_BY is unset") || len(h.fake.CallsTo("adj")) != 0 || len(h.calls("slicer", "edit")) != 1 {
		t.Errorf("notices %v, adj calls %v", notices, h.fake.CallsTo("adj"))
	}
}

func TestRecordSpecAnswersStopsAtASliceEditFailure(t *testing.T) {
	h := answerHarness(t, 2)
	_, _, err := RecordSpecAnswers(context.Background(), h.deps, answerConfig(h, true, "jon"))
	if err == nil || !strings.Contains(err.Error(), "no such section") {
		t.Fatalf("error %v", err)
	}
	if n := len(h.fake.CallsTo("adj")); n != 0 {
		t.Errorf("%d adj calls after a failed edit", n)
	}
}

func TestRecordSpecAnswersNeedsOneAnswerForEachQuestion(t *testing.T) {
	h := answerHarness(t, 0)
	for name, answers := range map[string][]string{"short": {"x"}, "long": {"x", "y", "z"}, "blank": {"x", "  "}} {
		cfg := answerConfig(h, false, "")
		cfg.Answers = answers
		if _, _, err := RecordSpecAnswers(context.Background(), h.deps, cfg); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if n := len(h.fake.Calls()); n != 0 {
		t.Errorf("%d calls for refused answers", n)
	}
}

// The owner's decisions reach every role of a run: the worktree's own copy of
// the spec lacks the paragraph that the main checkout got (S116).
func TestOwnerDecisionsReachEveryRoleBrief(t *testing.T) {
	h := newHarness(t)
	h.cfg.Decisions = "Owner decisions (2026-10-26): Which store? -> memory: rebuilt [cache.index-store]."
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if rec.Decisions != h.cfg.Decisions {
		t.Errorf("record decisions %q", rec.Decisions)
	}
	reqs := h.runner.requests()
	if len(reqs) < 3 {
		t.Fatalf("%d role rounds", len(reqs))
	}
	for _, r := range reqs {
		if !strings.Contains(r.Brief, "## Owner decisions\n\n"+h.cfg.Decisions) {
			t.Errorf("%s brief lacks the decisions section", r.Role)
		}
	}
}

func TestARunWithoutDecisionsHasNoDecisionsSection(t *testing.T) {
	h := newHarness(t)
	h.start()
	for _, r := range h.runner.requests() {
		if strings.Contains(r.Brief, "Owner decisions") {
			t.Errorf("%s brief has a decisions section", r.Role)
		}
	}
}
