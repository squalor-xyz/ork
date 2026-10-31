// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestMain(m *testing.M) {
	clitest.Main()
	os.Exit(m.Run())
}

const (
	item     = "S04"
	gitTitle = "S04: engine core for one slice"
)

// Reports a role writes. Every one ends with the Owner needed section.
const (
	implReport   = "Diff stat.\n\nTests: 12 passed, 0 failed\n\n## Owner needed\n\nnone\n"
	gatePass     = "VERDICT: PASS\n\n- earlier blockers resolved\nTests: 12 passed, 0 failed\n\n## Owner needed\n\nnone\n"
	gateFail     = "VERDICT: FAIL\n\n- engine.go:10 missing nil check\nTests: 12 passed, 0 failed\n\n## Owner needed\n\nnone\n"
	finishReport = "Closed S04.\n\n## Owner needed\n\nnone\n"
)

var cleanReview = review("nothing", "nothing", "nothing", "nothing")

func findingsReview(a string) string { return review(a, "nothing", "nothing", "nothing") }

// fakeClock moves one second on every call.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Second)
	return c.t
}

type fakeSleep struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (s *fakeSleep) Sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delays = append(s.delays, d)
	return ctx.Err()
}

// fakeRunner answers role rounds from a script. The default script returns a
// good report for each role, with a session id per role.
type fakeRunner struct {
	mu     sync.Mutex
	reqs   []Request
	script func(req Request, n int) (Result, error)
	exits  map[string]ExitClass // scripted exit by adapter row
}

func (f *fakeRunner) Run(ctx context.Context, req Request) (Result, error) {
	f.mu.Lock()
	n := len(f.reqs)
	f.reqs = append(f.reqs, req)
	script := f.script
	f.mu.Unlock()
	if req.OnStart != nil {
		req.OnStart(4000 + n)
	}
	if script != nil {
		if res, err := script(req, n); res.Exit != "" || err != nil {
			return res, err
		}
	}
	if class := f.exits[req.Vendor]; class != "" {
		return Result{Exit: class, Session: "failed-" + req.Vendor}, nil
	}
	return defaultResult(req), nil
}

func (f *fakeRunner) requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.reqs...)
}

func (f *fakeRunner) roles() []string {
	var out []string
	for _, r := range f.requests() {
		out = append(out, fmt.Sprintf("%s-%d", r.Role, r.Round))
	}
	return out
}

func defaultResult(req Request) Result {
	res := Result{Exit: ExitOK, Session: "sess-" + req.Role, Tokens: 1000, CostUSD: 0.5}
	switch req.Role {
	case RoleImplementer:
		res.Report = implReport
		if strings.Contains(req.Brief, "## Finish step") {
			res.Report = finishReport
		}
	case RoleReviewer, RoleUI:
		res.Report = cleanReview
	case RoleGate:
		res.Report = gatePass
	}
	return res
}

type fakeChecks struct {
	mu      sync.Mutex
	calls   []string // "dir|command"
	outputs []CheckOutput
	err     error
}

func (f *fakeChecks) Run(ctx context.Context, dir, command string, timeout time.Duration) (CheckOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.calls)
	f.calls = append(f.calls, dir+"|"+command)
	if f.err != nil {
		return CheckOutput{}, f.err
	}
	if len(f.outputs) == 0 {
		return CheckOutput{OK: true, Output: "ok  \tall packages\n"}, nil
	}
	return f.outputs[min(n, len(f.outputs)-1)], nil
}

type fakeProc struct {
	alive  map[int]bool
	killed []int
}

func (f *fakeProc) Alive(pgid int) bool { return f.alive[pgid] }
func (f *fakeProc) Kill(pgid int) error {
	f.killed = append(f.killed, pgid)
	delete(f.alive, pgid)
	return nil
}

type harnessOpts struct {
	routes    []clitest.Route                   // answered before the defaults
	routesFor func(P, W string) []clitest.Route // the same, for routes that name the paths
	project   func(*registry.Project)
	exclude   *string // contents of .git/info/exclude; nil means the file does not exist
	contract  int     // relay contract version the fake slicer prints: 10 (default) or 11
}

type harness struct {
	t      *testing.T
	fake   *clitest.Fake
	proj   registry.Project
	P, W   string
	store  *runrec.Store
	runner *fakeRunner
	checks *fakeChecks
	proc   *fakeProc
	sleep  *fakeSleep
	deps   Deps
	cfg    Config
}

func sliceJSON() string {
	return `{"id":"S04","title":"Engine","status":"started","slice":{"id":"S04","title":"Engine","size":"M","sections":[` +
		`{"heading":"Why","body":"Because."},{"heading":"Git","body":"One commit: \"` + gitTitle + `\"."}]}}`
}

func stepReplies(n int) []clitest.Reply {
	var out []clitest.Reply
	for i := 1; i <= n; i++ {
		out = append(out, clitest.Reply{Stdout: fmt.Sprintf(`{"step":{"id":"%d"}}`, i)})
	}
	return out
}

func newHarness(t *testing.T, opts ...func(*harnessOpts)) *harness {
	t.Helper()
	var o harnessOpts
	for _, f := range opts {
		f(&o)
	}
	root := t.TempDir()
	P := filepath.Join(root, "ork")
	if err := os.MkdirAll(filepath.Join(P, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if o.exclude != nil {
		if err := os.WriteFile(filepath.Join(P, ".git", "info", "exclude"), []byte(*o.exclude), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	proj := registry.Project{
		Name: "ork", Path: P, CheckCommand: "go test ./...", CheckMinutes: 10,
		EnvironmentLines: []string{"Shell: zsh on darwin.", "Go 1.27.2."},
	}
	if o.project != nil {
		o.project(&proj)
	}
	wtPattern, brPattern := ".worktrees/{id}", "{id}"
	if proj.WorktreePattern != "" {
		wtPattern = proj.WorktreePattern
	}
	if proj.BranchPattern != "" {
		brPattern = proj.BranchPattern
	}
	W := filepath.Join(P, strings.ReplaceAll(wtPattern, "{id}", item))
	branch := strings.ReplaceAll(brPattern, "{id}", item)
	s := func(args ...string) clitest.Route { return clitest.Route{Tool: "slicer", Args: args} }
	g := func(args ...string) clitest.Route { return clitest.Route{Tool: "git", Args: args} }
	with := func(r clitest.Route, stdout string) clitest.Route { r.Stdout = stdout; return r }
	routes := append([]clitest.Route{}, o.routes...)
	if o.routesFor != nil {
		routes = append(routes, o.routesFor(P, W)...)
	}
	version := 10
	if o.contract != 0 {
		version = o.contract
	}
	routes = append(routes,
		clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, File: filepath.Join("..", "..", "testdata", "cli", "slicer", fmt.Sprintf("relay-v%d.json", version))},
		with(clitest.Route{Tool: "slicer", Args: []string{"relay-run", "begin"}, Prefix: true}, `{"run":{"id":"run-1"}}`),
		with(clitest.Route{Tool: "slicer", Args: []string{"relay-run", "ask"}, Prefix: true}, `{"question":{"id":"q1"}}`),
		with(clitest.Route{Tool: "slicer", Args: []string{"relay-run", "answer"}, Prefix: true}, `{}`),
		with(s("show", item, "--json", "--lean", "--root", W), sliceJSON()),
		with(s("config", "implement_finish", "--json", "--root", W), `{"key":"implement_finish","value":"done"}`),
		with(s("check", "--json", "--root", W), `{"ok":true,"stale_render":[],"stale_sync":[],"problems":[],"warnings":[]}`),
		with(s("relay-run", "begin", item, "--worktree", W, "--json", "--root", W), `{"run":{"id":"run-1","items":["S04"],"state":"active"}}`),
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "step", "run-1"}, Prefix: true, Seq: stepReplies(80)},
		with(clitest.Route{Tool: "slicer", Args: []string{"relay-run", "end", "run-1"}, Prefix: true}, `{"run":{"id":"run-1","state":"ended"}}`),
		with(clitest.Route{Tool: "slicer", Args: []string{"note", item}, Prefix: true}, `{"id":"S04","added":true}`),
		with(g("-C", P, "rev-parse", "--git-path", "info/exclude"), ".git/info/exclude\n"),
		g("-C", P, "worktree", "add", W, "-b", branch),
		g("-C", W, "status", "--porcelain"),
		g("-C", W, "add", "-A"),
		g("-C", W, "commit", "-m", gitTitle),
		with(g("-C", W, "rev-parse", "HEAD"), "0123abc\n"),
		// The landing (S119) on a base branch that did not move.
		with(g("-C", P, "symbolic-ref", "--short", "HEAD"), "main\n"),
		clitest.Route{Tool: "git", Args: []string{"-C", P, "merge-base", "--is-ancestor", branch, "main"}, Exit: 1},
		g("-C", W, "merge-base", "--is-ancestor", "main", "HEAD"),
		g("-C", P, "merge", "--ff-only", branch),
		with(clitest.Route{Tool: "slicer", Args: []string{"land", item}, Prefix: true}, `{"item":{"id":"S04"}}`),
		g("-C", P, "status", "--porcelain", "--", ".slicer"),
		g("-C", P, "push"),
		g("-C", P, "worktree", "remove", W),
		g("-C", P, "branch", "-d", branch),
	)
	fake := clitest.Install(t, routes...)
	h := &harness{t: t, fake: fake, proj: proj, P: P, W: W,
		store:  &runrec.Store{Dir: filepath.Join(root, "state", "runs"), Now: (&fakeClock{t: time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)}).Now},
		runner: &fakeRunner{}, checks: &fakeChecks{}, proc: &fakeProc{alive: map[int]bool{}}, sleep: &fakeSleep{}}
	clock := &fakeClock{t: time.Date(2026, 10, 19, 16, 12, 0, 0, time.UTC)}
	h.deps = Deps{
		Runner: h.runner, Slicer: slicercli.Client{Bin: fake.Bin("slicer")}, GitBin: fake.Bin("git"),
		Store: h.store, Check: h.checks.Run, Proc: h.proc, Now: clock.Now, Sleep: h.sleep.Sleep,
		Backoff: []time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond},
		RandHex: func() string { return "a1b2" },
	}
	h.cfg = Config{Project: proj, Item: item, Lineup: map[string][]runrec.LineupEntry{
		RoleImplementer: {{Vendor: "claude", Model: "sonnet", Effort: "high"}},
		RoleReviewer:    {{Vendor: "claude", Model: "opus", Effort: "medium"}},
	}}
	return h
}

// start creates the engine and runs it to its first rest.
func (h *harness) start() *Engine {
	h.t.Helper()
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		h.t.Fatalf("New: %v", err)
	}
	if err := e.Run(context.Background()); err != nil {
		h.t.Fatalf("Run: %v", err)
	}
	return e
}

// calls returns the arguments of every call made to tool whose arguments start
// with prefix.
func (h *harness) calls(tool string, prefix ...string) [][]string {
	var out [][]string
	for _, c := range h.fake.CallsTo(tool) {
		if len(c.Args) >= len(prefix) && strings.Join(c.Args[:len(prefix)], "\x00") == strings.Join(prefix, "\x00") {
			out = append(out, c.Args)
		}
	}
	return out
}

// stepCall is one `slicer relay-run step` call, parsed.
type stepCall struct {
	Role, Agent, Vendor, Model, Reasoning, Phase, Task, Parent, Step string
}

func (h *harness) steps() []stepCall {
	var out []stepCall
	for _, a := range h.calls("slicer", "relay-run", "step") {
		var s stepCall
		for i := 3; i < len(a)-1; i++ {
			v := a[i+1]
			switch a[i] {
			case "--role":
				s.Role = v
			case "--agent":
				s.Agent = v
			case "--vendor":
				s.Vendor = v
			case "--model":
				s.Model = v
			case "--reasoning":
				s.Reasoning = v
			case "--phase":
				s.Phase = v
			case "--task":
				s.Task = v
			case "--parent":
				s.Parent = v
			case "--step":
				s.Step = v
			}
		}
		out = append(out, s)
	}
	return out
}

func join(args []string) string { return strings.Join(args, " ") }

func (h *harness) readReport(rec runrec.Record, name string) string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.store.RunDir(rec.ID), name))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(b)
}

func requireState(t *testing.T, e *Engine, want string) runrec.Record {
	t.Helper()
	rec := e.Snapshot()
	if rec.State != want {
		t.Fatalf("state %q (reason %q, pending %+v), want %q", rec.State, rec.Reason, rec.Pending, want)
	}
	return rec
}
