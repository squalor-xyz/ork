// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/roles"
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
	return specHarnessHead(t, nil, status...)
}

func specHarnessHead(t *testing.T, heads []clitest.Reply, status ...string) *harness {
	var reads []string
	for i := 0; i < len(status); i += 2 {
		after := status[i]
		if i+1 < len(status) {
			after = status[i+1]
		}
		reads = append(reads, status[i], status[i], after)
	}
	return specHarnessStatusReads(t, heads, reads...)
}

func specHarnessStatusReads(t *testing.T, heads []clitest.Reply, status ...string) *harness {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			if len(heads) == 0 {
				heads = []clitest.Reply{{Stdout: "abc\n"}}
			}
			var seq []clitest.Reply
			for _, s := range status {
				seq = append(seq, clitest.Reply{Stdout: s})
			}
			if len(seq) == 0 {
				seq = []clitest.Reply{{}}
			}
			return []clitest.Route{
				{Tool: "git", Args: []string{"-C", P, "rev-parse", "HEAD"}, Seq: heads},
				{Tool: "slicer", Args: []string{"show", "S10", "--json", "--lean", "--root", P}, Stdout: specSliceJSON("S10", "ten")},
				{Tool: "slicer", Args: []string{"show", "S11", "--json", "--lean", "--root", P}, Stdout: specSliceJSON("S11", "eleven")},
				{Tool: "git", Args: []string{"-C", P, "status", "--porcelain"}, Prefix: true, Seq: seq},
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

func TestSpecCheckFingerprintIsScopedToTheProject(t *testing.T) {
	h := specHarness(t)
	if _, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t)); err != nil {
		t.Fatal(err)
	}
	want := []string{"-C", h.P, "status", "--porcelain", "--", "."}
	for _, args := range h.calls("git", "-C", h.P, "status") {
		if !reflect.DeepEqual(args, want) {
			t.Errorf("fingerprint args %q, want %q", args, want)
		}
	}
}

// Use real git for the fingerprint and HEAD, but keep slicer and the role fake.
// Reports and fake CLI state live outside the repository being fingerprinted.
func specRepoHarness(t *testing.T, project string) *harness {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			root := filepath.Join(P, project)
			return []clitest.Route{
				{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", root}, File: filepath.Join("..", "..", "testdata", "cli", "slicer", "relay-v11.json")},
				{Tool: "slicer", Args: []string{"show", "S10", "--json", "--lean", "--root", root}, Stdout: specSliceJSON("S10", "ten")},
				{Tool: "slicer", Args: []string{"show", "S11", "--json", "--lean", "--root", root}, Stdout: specSliceJSON("S11", "eleven")},
			}
		}
	})
	gitRun(t, h.P, "init")
	for _, p := range []string{"a", "b"} {
		writeFile(t, filepath.Join(h.P, p, ".slicer", "log.jsonl"), "{}\n")
	}
	gitRun(t, h.P, "add", ".")
	gitRun(t, h.P, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	h.proj.Path = filepath.Join(h.P, project)
	h.deps.GitBin = "git"
	return h
}

func TestSpecCheckIgnoresASiblingProjectsWrite(t *testing.T) {
	for _, path := range []string{"a/.slicer/log.jsonl", "a/.slicer/slices/X.json"} {
		t.Run(path, func(t *testing.T) {
			h := specRepoHarness(t, "b")
			h.runner.script = func(req Request, n int) (Result, error) {
				writeFile(t, filepath.Join(h.P, path), "{}\n{}\n")
				return Result{Exit: ExitOK, Report: specReport}, nil
			}
			_, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			if err != nil {
				t.Fatalf("sibling write failed spec check: %v", err)
			}
		})
	}
}

func TestSpecCheckStillCatchesAWriteInItsOwnProject(t *testing.T) {
	for _, path := range []string{"b/notes.txt", "b/.slicer/log.jsonl"} {
		t.Run(path, func(t *testing.T) {
			h := specRepoHarness(t, "b")
			h.runner.script = func(req Request, n int) (Result, error) {
				writeFile(t, filepath.Join(h.P, path), "{}\n{}\n")
				return Result{Exit: ExitOK, Report: specReport}, nil
			}
			_, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			if err == nil || !strings.Contains(err.Error(), "changed the tree") || !strings.Contains(err.Error(), "read-only") {
				t.Fatalf("own write error: %v", err)
			}
		})
	}
}

func TestSpecCheckAtTheRepoRootIsUnchanged(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		t.Run(fmt.Sprint(dirty), func(t *testing.T) {
			h := specRepoHarness(t, ".")
			h.runner.script = func(req Request, n int) (Result, error) {
				if dirty {
					writeFile(t, filepath.Join(h.P, "notes.txt"), "notes\n")
				}
				return Result{Exit: ExitOK, Report: specReport}, nil
			}
			_, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			if dirty {
				if err == nil || !strings.Contains(err.Error(), "changed the tree") || !strings.Contains(err.Error(), "read-only") {
					t.Fatalf("root write error: %v", err)
				}
			} else if err != nil {
				t.Fatalf("unchanged root: %v", err)
			}
		})
	}
}

func TestProjectPrefix(t *testing.T) {
	h := specRepoHarness(t, "b")
	for _, tc := range []struct{ dir, want string }{
		{h.P, ""},
		{h.proj.Path, "b/"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			got, err := projectPrefix(context.Background(), tc.dir)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("project prefix %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("not a repository", func(t *testing.T) {
		if _, err := projectPrefix(context.Background(), t.TempDir()); err == nil {
			t.Fatal("project prefix outside a repository succeeded")
		}
	})
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
	for _, want := range []string{"Role: spec-check", "S10, S11", `"body":"eleven"`, `"id":"S11"`, "(ork saves your final message there; you cannot write it)"} {
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

func TestSpecCheckBriefNamesTheRowsVendorLimits(t *testing.T) {
	h := specHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Vendor == "grok" {
			return Result{Exit: ExitError}, nil
		}
		return Result{Exit: ExitOK, Report: specReport}, nil
	}
	cfg := specConfig(h, t)
	cfg.Lineup = append([]runrec.LineupEntry{{Vendor: "grok", Model: "grok-4.7", Effort: "medium"}}, cfg.Lineup...)
	if _, err := RunSpecCheck(context.Background(), h.deps, cfg); err != nil {
		t.Fatal(err)
	}
	reqs := h.runner.requests()
	if len(reqs) != 2 {
		t.Fatalf("got %d requests", len(reqs))
	}
	for _, form := range []string{"git status --short", "git diff -- PATH", "git diff --check -- PATH"} {
		if !strings.Contains(reqs[0].Brief, form) {
			t.Errorf("Grok brief lacks %q", form)
		}
		if strings.Contains(reqs[1].Brief, form) {
			t.Errorf("Claude brief got Grok limit %q", form)
		}
	}
	if !strings.Contains(reqs[1].Brief, "You may only read the tree; no git or tracking writes.") {
		t.Error("Claude lost read-only instruction")
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

func TestParseSpecCheckMatchesAnIdIgnoringCase(t *testing.T) {
	report := "## Slice S374\nReady\n\n## Slice s375\nReady\n\n## Owner needed\n\nnone\n"
	verdicts, err := ParseSpecCheck(report, []string{"s374", "S375"})
	if err != nil {
		t.Fatalf("error %v", err)
	}
	if len(verdicts) != 2 || verdicts[0].Item != "s374" || verdicts[1].Item != "S375" {
		t.Errorf("verdicts %+v", verdicts)
	}
	dup := "## Slice S374\nReady\n\n## Slice s374\nReady\n\n## Owner needed\n\nnone\n"
	if _, err := ParseSpecCheck(dup, []string{"s374"}); err == nil {
		t.Error("two sections for one id in different case were accepted")
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

func TestSpecCheckCriteriaExampleBlockParses(t *testing.T) {
	p, err := roles.Prompt("spec-check")
	if err != nil {
		t.Fatal(err)
	}
	_, report, _ := strings.Cut(p, "\n## Report\n")
	var example string
	for _, part := range strings.Split(report, "```text\n")[1:] {
		block, _, _ := strings.Cut(part, "```")
		if strings.Contains(block, "1. tighten:") {
			example = block
			break
		}
	}
	if example == "" {
		t.Fatal("spec-check report has no worked vague-Check question block")
	}
	blocks, err := ParseOwnerQuestions(example)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d questions, want one", len(blocks))
	}
	q := blocks[0]
	if q.Key != "" {
		t.Errorf("slice criteria question has decision key %q", q.Key)
	}
	want := &OwnerQuestion{
		Title: "How should this slice's vague Check say when it is done?",
		Options: []Option{
			{Label: "tighten", Summary: "replace \"works\" with `go test ./...`; done when it exits 0 with all tests passing"},
			{Label: "human check", Summary: "mark the item [HUMAN CHECK] and state what the owner looks at and what they should see"},
			{Label: "as written", Summary: "accept the Check as written"},
		},
		Recommend: 1,
		Reason:    "the proposed command and expected result make done verifiable",
	}
	if !reflect.DeepEqual(q.Ask, want) {
		t.Errorf("criteria question = %+v, want %+v", q.Ask, want)
	}
}

// S288 (owner decision 2026-11-07): spec-check question blocks get the same
// leniency as an owner finding.
func TestParseOwnerQuestionsKeepsContextAndMarkers(t *testing.T) {
	body := `Question: One?
The first reason.
- Options:
  - 1. a: A
  - 2. b: B
- Recommend: 1 - because
A closing remark.
Decision key: area.one

Question: Two?
Options:
1. c: C
2. d: D
Recommend: 2 - since
`
	blocks, err := ParseOwnerQuestions(body)
	if err != nil || len(blocks) != 2 {
		t.Fatalf("blocks: %+v, %v", blocks, err)
	}
	q := blocks[0]
	if q.Key != "area.one" || q.Ask.Title != "One?" || len(q.Ask.Options) != 2 || q.Ask.Options[1].Label != "b" || q.Ask.Reason != "because" {
		t.Errorf("block 0: %+v", q)
	}
	for _, want := range []string{"The first reason.", "A closing remark."} {
		if !strings.Contains(q.Text, want) {
			t.Errorf("block 0 text lacks %q: %q", want, q.Text)
		}
	}
	if strings.Contains(q.Text, "Two?") || blocks[1].Ask.Recommend != 2 {
		t.Errorf("blocks bled into each other: %+v", blocks)
	}
	if _, err := ParseOwnerQuestions("Question: One?\nOptions:\n1. a: A\nIn between.\n2. b: B\nRecommend: 1 - x\n"); err == nil {
		t.Error("context between options accepted")
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

func TestSpecFingerprintNoSpecIsASentinel(t *testing.T) {
	for _, raw := range []string{`{"id":"S10","slice":null}`, `{"id":"S10"}`} {
		if _, err := SpecFingerprint([]byte(raw)); !errors.Is(err, ErrNoSpec) {
			t.Errorf("%s: error %v, want ErrNoSpec", raw, err)
		}
	}
	for _, raw := range []string{`nope`, `{"slice":`} {
		_, err := SpecFingerprint([]byte(raw))
		if err == nil || errors.Is(err, ErrNoSpec) {
			t.Errorf("%s: error %v, want a malformed JSON error that is not ErrNoSpec", raw, err)
		}
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

func TestCheckRerunsOnceWhenMainMovesDuringTheCheck(t *testing.T) {
	for _, keepsMoving := range []bool{false, true} {
		t.Run(fmt.Sprint(keepsMoving), func(t *testing.T) {
			heads := []clitest.Reply{{Stdout: "a"}, {Stdout: "b"}, {Stdout: "b"}, {Stdout: "b"}}
			if keepsMoving {
				heads[3].Stdout = "c"
			}
			h := specHarnessHead(t, heads, "", " M a.go\n", "", "")
			res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			if keepsMoving {
				if err == nil || !strings.Contains(err.Error(), "main moved") || res != nil {
					t.Fatalf("result %+v, error %v", res, err)
				}
			} else if err != nil || res == nil {
				t.Fatalf("retry failed: %v", err)
			}
			if n := len(h.runner.requests()); n != 2 {
				t.Fatalf("%d attempts, want 2", n)
			}
		})
	}
}

func TestCheckRerunsOnceOnAShapeError(t *testing.T) {
	for _, alwaysBad := range []bool{false, true} {
		t.Run(fmt.Sprint(alwaysBad), func(t *testing.T) {
			h := specHarness(t)
			h.runner.script = func(req Request, n int) (Result, error) {
				report := specReport
				if n == 0 || alwaysBad {
					report = "Malformed report"
				}
				return Result{Exit: ExitOK, Report: report}, nil
			}
			res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			if alwaysBad {
				var se *ShapeError
				if !errors.As(err, &se) || res != nil {
					t.Fatalf("result %+v, error %v", res, err)
				}
			} else if err != nil || res == nil {
				t.Fatalf("retry failed: %v", err)
			}
			if n := len(h.runner.requests()); n != 2 {
				t.Fatalf("%d attempts, want 2", n)
			}
		})
	}
}

func TestOwnerNeededTextIsKept(t *testing.T) {
	h := specHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		return Result{Exit: ExitOK, Report: strings.Replace(specReport, "\nnone\n", "\nConfirm the rollout window with the owner.\n", 1)}, nil
	}
	res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(res)
	if err != nil || !strings.Contains(string(b), "Confirm the rollout window with the owner.") {
		t.Fatalf("owner text dropped: %s %v", b, err)
	}
}

func TestMovingMainDoesNotRetryAPermissionDenial(t *testing.T) {
	h := specHarnessHead(t, []clitest.Reply{{Stdout: "a"}, {Stdout: "b"}})
	h.runner.script = func(req Request, n int) (Result, error) {
		return Result{Exit: ExitError, Denials: []runrec.DeniedCommand{{Tool: "Bash", Command: "blocked command"}}}, nil
	}
	_, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
	if err == nil || !strings.Contains(err.Error(), "permission denied") || len(h.runner.requests()) != 1 {
		t.Fatalf("denial retried: %v (%d attempts)", err, len(h.runner.requests()))
	}
}

func TestSpecCheckRetriesStatusChangesOutsideTheRole(t *testing.T) {
	for _, again := range []bool{false, true} {
		t.Run(fmt.Sprint(again), func(t *testing.T) {
			statuses := []string{"", " M outside.go\n", " M outside.go\n", " M outside.go\n", " M outside.go\n"}
			if again {
				statuses[3] = " M another.go\n"
			}
			h := specHarnessStatusReads(t, nil, statuses...)
			res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			if again {
				if err == nil || !strings.Contains(err.Error(), "retry") || !strings.Contains(err.Error(), "Check again") || !strings.Contains(err.Error(), "the main checkout changed outside the check of S10, S11") {
					t.Fatalf("second change: %v", err)
				}
			} else if err != nil || res == nil {
				t.Fatalf("external status change failed: %v", err)
			}
		})
	}
}

func TestSpecCheckRetryFailureExplainsRecovery(t *testing.T) {
	for _, shape := range []bool{false, true} {
		t.Run(fmt.Sprint(shape), func(t *testing.T) {
			h := specHarnessHead(t, []clitest.Reply{{Stdout: "a"}, {Stdout: "b"}, {Stdout: "b"}, {Stdout: "c"}})
			if shape {
				h = specHarness(t)
				h.runner.script = func(Request, int) (Result, error) { return Result{Exit: ExitOK, Report: "bad shape"}, nil }
			}
			_, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			if err == nil || !strings.Contains(err.Error(), "retry") || !strings.Contains(err.Error(), "Check again") {
				t.Fatalf("unhelpful retry failure: %v", err)
			}
		})
	}
}

// S266: a report-shape failure or a denied read command resumes the spec
// check's own session once, with the error and the expected forms, before the
// check gives up.

// resumeScript answers the first round with first and every later round with
// the good report; it records nothing itself (the harness keeps the requests).
func resumeScript(first Result) func(Request, int) (Result, error) {
	return func(req Request, n int) (Result, error) {
		if n == 0 {
			return first, nil
		}
		return Result{Exit: ExitOK, Session: "sess-2", Report: specReport}, nil
	}
}

func TestSpecCheckResumesTheSameSessionOnAShapeError(t *testing.T) {
	h := specHarness(t)
	h.runner.script = resumeScript(Result{Exit: ExitOK, Session: "sess-1", Report: "## Slice S10\nReady\n\n## Slice S11\nLooks fine to me.\n\n## Owner needed\n\nnone\n"})
	res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
	if err != nil || res == nil || len(res.Verdicts) != 2 {
		t.Fatalf("result %+v, error %v", res, err)
	}
	reqs := h.runner.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d rounds, want the first and one resume", len(reqs))
	}
	first, second := reqs[0], reqs[1]
	if first.Resume != "" {
		t.Errorf("the first round resumed %q", first.Resume)
	}
	if second.Resume != "sess-1" {
		t.Errorf("the second round resumed %q, want sess-1", second.Resume)
	}
	if second.Role != RoleSpecCheck || !second.ReadOnly || len(second.Allowlist) != 0 || second.Worktree != h.P {
		t.Errorf("resumed request: %+v", second)
	}
	if second.Vendor != first.Vendor || second.Model != first.Model || second.Effort != first.Effort {
		t.Errorf("the resume changed the row: %+v vs %+v", second, first)
	}
	for _, want := range []string{"S11: write Ready, or question blocks that start with Question:", "Ready", "Question:", "## Owner needed", "final message"} {
		if !strings.Contains(second.Brief, want) {
			t.Errorf("nudge lacks %q:\n%s", want, second.Brief)
		}
	}
	if second.ReportPath != first.ReportPath {
		t.Errorf("report paths differ: %q vs %q", second.ReportPath, first.ReportPath)
	}
}

func TestSpecCheckShapeErrorAfterTheResumeFailsWithoutAStartOver(t *testing.T) {
	h := specHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if n >= 6 {
			return Result{Exit: ExitError}, nil // a runaway resume loop ends here
		}
		return Result{Exit: ExitOK, Session: fmt.Sprintf("sess-%d", n), Report: "Malformed report"}, nil
	}
	res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
	var se *ShapeError
	var retried *SpecCheckRetryError
	if res != nil || !errors.As(err, &se) || !errors.As(err, &retried) {
		t.Fatalf("result %+v, error %v", res, err)
	}
	if n := len(h.runner.requests()); n != 2 {
		t.Fatalf("%d rounds, want the first and one resume (no start over)", n)
	}
	msg := err.Error()
	for _, want := range []string{"S10, S11 failed again after ork gave the role its error once: report shape: ", "Check again; the check changes nothing, so a rerun is safe."} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q: %s", want, msg)
		}
	}
	for _, not := range []string{"spec check", "session", "resumed"} {
		if strings.Contains(msg, not) {
			t.Errorf("message has %q (the CLI prefix already says spec check): %s", not, msg)
		}
	}
}

func TestSpecCheckResumesTheSameSessionOnADeniedCommand(t *testing.T) {
	for _, exit := range []ExitClass{ExitOK, ExitError} {
		t.Run(string(exit), func(t *testing.T) {
			h := specHarness(t)
			h.runner.script = resumeScript(Result{Exit: exit, Session: "sess-1", Denials: []runrec.DeniedCommand{{Tool: "Bash", Command: "rg -n spec | head -20"}}})
			res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			if err != nil || res == nil || len(res.Verdicts) != 2 {
				t.Fatalf("result %+v, error %v", res, err)
			}
			reqs := h.runner.requests()
			if len(reqs) != 2 || reqs[1].Resume != "sess-1" || !reqs[1].ReadOnly {
				t.Fatalf("rounds: %+v", reqs)
			}
			for _, want := range []string{"Bash: rg -n spec | head -20", "denied", "one command", "pipe", "final message"} {
				if !strings.Contains(reqs[1].Brief, want) {
					t.Errorf("nudge lacks %q:\n%s", want, reqs[1].Brief)
				}
			}
		})
	}
}

func TestSpecCheckDeniedAgainAfterTheResumeFails(t *testing.T) {
	h := specHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if n >= 6 {
			return Result{Exit: ExitError}, nil // a runaway resume loop ends here
		}
		return Result{Exit: ExitOK, Session: fmt.Sprintf("sess-%d", n), Report: specReport, Denials: []runrec.DeniedCommand{{Tool: "Bash", Command: fmt.Sprintf("denied %d", n)}}}, nil
	}
	res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
	var retried *SpecCheckRetryError
	if res != nil || !errors.As(err, &retried) {
		t.Fatalf("result %+v, error %v", res, err)
	}
	if n := len(h.runner.requests()); n != 2 {
		t.Fatalf("%d rounds, want the first and one resume", n)
	}
	for _, want := range []string{"S10, S11 failed again after ork gave the role its error once: permission denied: Bash: denied 1", "kept using a denied command, such as a pipe", "Check again; the check changes nothing, so a rerun is safe."} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "exited") || strings.Contains(err.Error(), "spec check") {
		t.Errorf("message keeps jargon: %v", err)
	}
}

func TestSpecCheckResumesOnlyOncePerCheck(t *testing.T) {
	// Round 1 is malformed and is resumed. Round 2 is good, but main moved, so
	// the check starts over. Round 3 is malformed again: the one resume is
	// spent, so the check fails there with no fourth round.
	h := specHarnessHead(t, []clitest.Reply{{Stdout: "a"}, {Stdout: "a"}, {Stdout: "b"}, {Stdout: "b"}, {Stdout: "b"}})
	h.runner.script = func(req Request, n int) (Result, error) {
		report := "Malformed report"
		if n == 1 {
			report = specReport
		}
		return Result{Exit: ExitOK, Session: fmt.Sprintf("sess-%d", n), Report: report}, nil
	}
	res, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
	var se *ShapeError
	if res != nil || !errors.As(err, &se) {
		t.Fatalf("result %+v, error %v", res, err)
	}
	reqs := h.runner.requests()
	if len(reqs) != 3 {
		t.Fatalf("%d rounds, want 3", len(reqs))
	}
	if reqs[0].Resume != "" || reqs[1].Resume != "sess-0" || reqs[2].Resume != "" {
		t.Errorf("resumes: %q %q %q", reqs[0].Resume, reqs[1].Resume, reqs[2].Resume)
	}
}

// Without a session id there is nothing to resume: a shape error starts the
// check over as before, and a denial fails at once.
func TestSpecCheckWithoutASessionCannotResume(t *testing.T) {
	h := specHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		return Result{Exit: ExitOK, Report: "Malformed report"}, nil
	}
	if _, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t)); err == nil {
		t.Fatal("no error")
	}
	for _, r := range h.runner.requests() {
		if r.Resume != "" || !strings.Contains(r.Brief, "Role: spec-check") {
			t.Errorf("round resumed %q with no session, or sent a nudge in place of the brief", r.Resume)
		}
	}
	if n := len(h.runner.requests()); n != 2 {
		t.Errorf("%d rounds, want the start-over pair", n)
	}
}

// A role that changed the tree is not given another turn (guard: the tree
// check stays ahead of the resume).
func TestSpecCheckDoesNotResumeARoleThatChangedTheTree(t *testing.T) {
	for name, report := range map[string]string{"shape": "Malformed report", "denial": specReport} {
		t.Run(name, func(t *testing.T) {
			h := specHarness(t, "", " M edited.go\n")
			h.runner.script = func(req Request, n int) (Result, error) {
				res := Result{Exit: ExitOK, Session: "sess", Report: report}
				if name == "denial" {
					res.Denials = []runrec.DeniedCommand{{Tool: "Bash", Command: "touch x"}}
				}
				return res, nil
			}
			_, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			if err == nil {
				t.Fatal("no error")
			}
			if n := len(h.runner.requests()); n != 1 {
				t.Errorf("%d rounds, want 1: %v", n, err)
			}
		})
	}
}

func TestSpecCheckResumeRecordsItsOwnLedgerStep(t *testing.T) {
	h := specHarness(t)
	h.runner.script = resumeScript(Result{Exit: ExitOK, Session: "sess-1", Report: "Malformed report"})
	if _, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t)); err != nil {
		t.Fatal(err)
	}
	var tasks []string
	for _, s := range h.steps() {
		if s.Role == RoleSpecCheck && s.Phase == "start" {
			tasks = append(tasks, s.Task)
		}
	}
	want := []string{"S10, S11 spec-check", "S10, S11 spec-check resumed with error"}
	if !reflect.DeepEqual(tasks, want) {
		t.Errorf("spec-check steps %q, want %q", tasks, want)
	}
}

// A vendor limit, an auth failure or an unclassified exit is not a malformed
// report or a denied command to talk the role out of: the session is not
// resumed (adj 23: an unclassified exit stops).
func TestSpecCheckDoesNotResumeAnExitThatIsNotAnAnswer(t *testing.T) {
	for _, exit := range []ExitClass{ExitRateLimited, ExitOutOfCredits, ExitAuth, ExitUnclassified} {
		t.Run(string(exit), func(t *testing.T) {
			h := specHarness(t)
			h.runner.script = func(req Request, n int) (Result, error) {
				if n >= 6 {
					return Result{Exit: ExitError}, nil
				}
				return Result{Exit: exit, Session: "sess", Denials: []runrec.DeniedCommand{{Tool: "Bash", Command: "rg x | head"}}}, nil
			}
			if _, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t)); err == nil {
				t.Fatal("no error")
			}
			for _, r := range h.runner.requests() {
				if r.Resume != "" {
					t.Errorf("resumed %q after exit %s", r.Resume, exit)
				}
			}
			if n := len(h.runner.requests()); n != 1 {
				t.Errorf("%d rounds, want 1", n)
			}
		})
	}
}

// The HEAD guard: a role whose round saw main move is not resumed against the
// stale specs. A malformed report starts the check over (and main moving again
// fails it); a denial still stops at once, as TestMovingMainDoesNotRetryAPermissionDenial
// pins for a role with no session.
func TestSpecCheckDoesNotResumeWhileMainMoves(t *testing.T) {
	for name, tc := range map[string]struct {
		res    Result
		err    string
		rounds int
	}{
		"shape":  {Result{Exit: ExitOK, Session: "sess", Report: "Malformed report"}, "main moved", 2},
		"denial": {Result{Exit: ExitOK, Session: "sess", Report: specReport, Denials: []runrec.DeniedCommand{{Tool: "Bash", Command: "rg x | head"}}}, "permission denied", 1},
	} {
		t.Run(name, func(t *testing.T) {
			h := specHarnessHead(t, []clitest.Reply{{Stdout: "a"}, {Stdout: "b"}, {Stdout: "c"}, {Stdout: "d"}})
			h.runner.script = func(req Request, n int) (Result, error) { return tc.res, nil }
			_, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t))
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("error %v, want %q", err, tc.err)
			}
			reqs := h.runner.requests()
			if len(reqs) != tc.rounds {
				t.Fatalf("%d rounds, want %d", len(reqs), tc.rounds)
			}
			for _, r := range reqs {
				if r.Resume != "" {
					t.Errorf("resumed %q while main moved", r.Resume)
				}
			}
		})
	}
}

// A long list of denied commands is cut in the prompt.
func TestSpecDenialNudgeCapsTheList(t *testing.T) {
	var denials []runrec.DeniedCommand
	for i := 0; i < 13; i++ {
		denials = append(denials, runrec.DeniedCommand{Tool: "Bash", Command: fmt.Sprintf("cmd%02d | head", i)})
	}
	nudge := specDenialNudge(denials, []string{"S10"})
	if !strings.Contains(nudge, "cmd09 | head") || strings.Contains(nudge, "cmd10 | head") || !strings.Contains(nudge, "and 3 more") {
		t.Errorf("nudge:\n%s", nudge)
	}
}

// The refused first round is a failed ledger step with a short reason, so the
// run tree explains the second spec-check row.
func TestSpecCheckRecordsTheRefusedRoundAsAFailedStep(t *testing.T) {
	for name, tc := range map[string]struct {
		first Result
		want  string
	}{
		"shape":                     {Result{Exit: ExitOK, Session: "sess-1", Report: "Malformed report"}, "report shape"},
		"denial":                    {Result{Exit: ExitOK, Session: "sess-1", Report: specReport, Denials: []runrec.DeniedCommand{{Tool: "Bash", Command: "rg x | head"}}}, "denied: Bash: rg x | head"},
		"denial with an error exit": {Result{Exit: ExitError, Session: "sess-1", Denials: []runrec.DeniedCommand{{Tool: "Bash", Command: "rg x | head"}}}, "denied: Bash: rg x | head"},
		"long denial":               {Result{Exit: ExitOK, Session: "sess-1", Report: specReport, Denials: []runrec.DeniedCommand{{Tool: "Bash", Command: strings.Repeat("a", 200)}}}, "denied: Bash: " + strings.Repeat("a", 63) + "..."},
	} {
		t.Run(name, func(t *testing.T) {
			h := specHarness(t)
			h.runner.script = resumeScript(tc.first)
			if _, err := RunSpecCheck(context.Background(), h.deps, specConfig(h, t)); err != nil {
				t.Fatal(err)
			}
			var ends []string
			for _, a := range h.calls("slicer", "relay-run", "step") {
				if j := join(a); strings.Contains(j, "--phase fail") || strings.Contains(j, "--phase done") {
					ends = append(ends, j)
				}
			}
			if len(ends) != 2 || !strings.Contains(ends[0], "--phase fail") || !strings.Contains(ends[0]+" ", "--reason "+tc.want+" ") || !strings.Contains(ends[1], "--phase done") {
				t.Errorf("step ends: %q", ends)
			}
		})
	}
}
