// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// The landing tests run real git in temp repos. The slicer is a shell fake that
// logs its calls, and the git binary the engine gets is a wrapper that logs
// every call, runs the real git, and never pushes: a push is logged and
// answered with success (or failure, when the test asks).

const fakeSlicerScript = `#!/bin/sh
d=$(dirname "$0")
echo "$@" >> "$d/slicer.log"
root=""; prev=""
for a in "$@"; do [ "$prev" = "--root" ] && root="$a"; prev="$a"; done
case "$1" in
config) echo "{\"key\":\"implement_finish\",\"value\":\"$(cat "$d/finish" 2>/dev/null || echo done)\"}";;
show) cat "$d/show.json";;
render)
	if [ -f "$d/render-fails" ]; then rm "$d/render-fails"; echo "render broke" >&2; exit 1; fi
	echo render >> "$root/rendered.txt";;
check) echo '{"ok":true,"stale_render":[],"stale_sync":[],"problems":[],"warnings":[]}';;
land)
	if [ -f "$d/land-writes" ]; then mkdir -p "$root/.slicer"; echo done >> "$root/.slicer/index.json"; fi
	echo '{}';;
relay-run) if [ "$2" = step ]; then echo '{"step":{"id":"9"}}'; else echo '{"run":{"id":"run-1"}}'; fi;;
*) echo '{}';;
esac
`

const fakeGitScript = `#!/bin/sh
d=$(dirname "$0")
echo "$@" >> "$d/git.log"
if [ "$3" = merge ] && [ "$4" = -m ] && [ -f "$d/merge-fails" ]; then echo "merge broke" >&2; exit 1; fi
if [ "$3" = push ]; then
	if [ -f "$d/push-fails" ]; then echo "push refused" >&2; exit 1; fi
	exit 0
fi
exec git "$@"
`

type landEnv struct {
	t       *testing.T
	P, W    string
	fakes   string
	store   *runrec.Store
	runner  *fakeRunner
	checks  *fakeChecks
	deps    Deps
	proj    registry.Project
	e       *Engine
	run     *runrec.Run
	seen    []checkSeen
	baseTip string
	// beforeCheck, when set, runs as the re-check starts (it may block).
	beforeCheck func()
}

// checkSeen is what the check command saw when it ran in the worktree.
type checkSeen struct {
	dir       string
	mainTip   string // the base branch's tip at that moment
	hasBase   bool   // the worktree's HEAD already contains the base tip
	treeClean bool
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newLandEnv(t *testing.T, opts ...func(*registry.Project)) *landEnv {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "Test")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "test@example.invalid")
	}
	P := filepath.Join(root, "ork")
	W := filepath.Join(P, ".worktrees", item)
	if err := os.MkdirAll(P, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, P, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(P, ".git", "info", "exclude"), ".worktrees/\n")
	writeFile(t, filepath.Join(P, "shared.txt"), "one\n")
	gitRun(t, P, "add", "-A")
	gitRun(t, P, "commit", "-q", "-m", "init")
	gitRun(t, P, "worktree", "add", "-q", W, "-b", item)
	writeFile(t, filepath.Join(W, "feature.txt"), "the slice\n")

	fakes := filepath.Join(root, "fakes")
	for name, body := range map[string]string{"slicer": fakeSlicerScript, "gitw": fakeGitScript} {
		writeFile(t, filepath.Join(fakes, name), body)
		if err := os.Chmod(filepath.Join(fakes, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(fakes, "show.json"), sliceJSON())

	proj := registry.Project{Name: "ork", Path: P, CheckCommand: "go test ./...", CheckMinutes: 10}
	for _, o := range opts {
		o(&proj)
	}
	clock := &fakeClock{t: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	env := &landEnv{t: t, P: P, W: W, fakes: fakes, proj: proj, runner: &fakeRunner{}, checks: &fakeChecks{}, baseTip: gitRun(t, P, "rev-parse", "main"),
		store: &runrec.Store{Dir: filepath.Join(root, "state", "runs"), Now: clock.Now}}
	env.deps = Deps{
		Runner: env.runner, Slicer: slicercli.Client{Bin: filepath.Join(fakes, "slicer")}, GitBin: filepath.Join(fakes, "gitw"),
		Store: env.store, Proc: &fakeProc{alive: map[int]bool{}}, Now: clock.Now, Sleep: (&fakeSleep{}).Sleep,
		Backoff: []time.Duration{time.Millisecond}, RandHex: func() string { return "a1b2" }, Landings: &Landings{},
		Check: func(ctx context.Context, dir, command string, timeout time.Duration) (CheckOutput, error) {
			if env.beforeCheck != nil {
				env.beforeCheck()
			}
			env.seen = append(env.seen, checkSeen{
				dir:       dir,
				mainTip:   gitRun(t, P, "rev-parse", "main"),
				hasBase:   exec.Command("git", "-C", dir, "merge-base", "--is-ancestor", "main", "HEAD").Run() == nil,
				treeClean: gitRun(t, dir, "status", "--porcelain") == "",
			})
			return env.checks.Run(ctx, dir, command, timeout)
		},
	}
	run, err := env.store.Create(runrec.Record{
		ID: "ork-20261026-120000-a1b2", Project: "ork", Item: item, Worktree: W, Branch: item, RelayRun: "run-1",
		Contract: 7, State: runrec.StateAwaitingApproval, Round: 1,
		Lineup: map[string][]runrec.LineupEntry{
			RoleImplementer: {{Vendor: "claude", Model: "sonnet", Effort: "high"}},
			RoleReviewer:    {{Vendor: "claude", Model: "opus", Effort: "medium"}},
		},
		Card: &runrec.ApprovalCard{Round: 1, Verdict: "PASS", Passed: 12},
	})
	if err != nil {
		t.Fatal(err)
	}
	env.run = run
	env.e = Load(env.deps, proj, run)
	return env
}

// secondRun adds a run of another slice (S05) in the same project and main
// checkout: its own worktree and branch, its own engine, the same landing lock.
func (env *landEnv) secondRun() *landEnv {
	t := env.t
	t.Helper()
	W2 := filepath.Join(env.P, ".worktrees", "S05")
	gitRun(t, env.P, "worktree", "add", "-q", W2, "-b", "S05")
	writeFile(t, filepath.Join(W2, "feature2.txt"), "the second slice\n")
	two := &landEnv{t: t, P: env.P, W: W2, fakes: env.fakes, store: env.store, runner: &fakeRunner{}, checks: &fakeChecks{}, proj: env.proj, deps: env.deps}
	two.deps.Runner = two.runner
	two.deps.Check = func(ctx context.Context, dir, command string, timeout time.Duration) (CheckOutput, error) {
		two.seen = append(two.seen, checkSeen{
			dir:       dir,
			mainTip:   gitRun(t, env.P, "rev-parse", "main"),
			hasBase:   exec.Command("git", "-C", dir, "merge-base", "--is-ancestor", "main", "HEAD").Run() == nil,
			treeClean: gitRun(t, dir, "status", "--porcelain") == "",
		})
		return two.checks.Run(ctx, dir, command, timeout)
	}
	run, err := env.store.Create(runrec.Record{
		ID: "ork-20261026-120001-c3d4", Project: "ork", Item: "S05", Worktree: W2, Branch: "S05", RelayRun: "run-2",
		Contract: 7, State: runrec.StateAwaitingApproval, Round: 1,
		Lineup: map[string][]runrec.LineupEntry{
			RoleImplementer: {{Vendor: "claude", Model: "sonnet", Effort: "high"}},
			RoleReviewer:    {{Vendor: "claude", Model: "opus", Effort: "medium"}},
		},
		Card: &runrec.ApprovalCard{Round: 1, Verdict: "PASS", Passed: 12},
	})
	if err != nil {
		t.Fatal(err)
	}
	two.run = run
	two.e = Load(two.deps, env.proj, run)
	return two
}

// moveBase commits a new file on the base branch in the main checkout.
func (env *landEnv) moveBase(name, text string) string {
	env.t.Helper()
	writeFile(env.t, filepath.Join(env.P, name), text)
	gitRun(env.t, env.P, "add", "-A")
	gitRun(env.t, env.P, "commit", "-q", "-m", "base moves: "+name)
	return gitRun(env.t, env.P, "rev-parse", "main")
}

func (env *landEnv) approve() {
	env.t.Helper()
	if err := env.e.Approve(context.Background()); err != nil {
		env.t.Fatalf("Approve: %v", err)
	}
}

// log reads a fake's call log, one call per entry.
func (env *landEnv) log(name string) []string {
	b, _ := os.ReadFile(filepath.Join(env.fakes, name))
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// gitCalls are the calls the engine made to git, with the directories named P
// (the main checkout) and W (the worktree).
func (env *landEnv) gitCalls() []string {
	var out []string
	for _, l := range env.log("git.log") {
		l = strings.Replace(l, "-C "+env.W+" ", "W: ", 1)
		l = strings.Replace(l, "-C "+env.P+" ", "P: ", 1)
		out = append(out, l)
	}
	return out
}

func indexOf(list []string, prefix string) int {
	for i, l := range list {
		if strings.HasPrefix(l, prefix) {
			return i
		}
	}
	return -1
}

// relayEnds counts the calls that ended the ledger run.
func (env *landEnv) relayEnds() int {
	n := 0
	for _, l := range env.log("slicer.log") {
		if strings.HasPrefix(l, "relay-run end ") {
			n++
		}
	}
	return n
}

func (env *landEnv) setCheckOutputs(o ...CheckOutput) { env.checks.outputs = o }

func (env *landEnv) refOK(ref string) bool {
	return exec.Command("git", "-C", env.P, "rev-parse", "--verify", "-q", ref).Run() == nil
}

func TestLandMergesBaseWhenMoved(t *testing.T) {
	env := newLandEnv(t)
	moved := env.moveBase("base.txt", "from main\n")
	env.approve()
	rec := requireState(t, env.e, runrec.StateCommitted)

	// The base branch took the slice by a fast-forward over the merge.
	if got := gitRun(t, env.P, "rev-parse", "main"); got == moved {
		t.Fatalf("main did not move: %s", got)
	}
	if got := gitRun(t, env.P, "rev-list", "--merges", "--count", "main"); got != "1" {
		t.Errorf("%s merge commits on main, want the one that took the base branch in", got)
	}
	if exec.Command("git", "-C", env.P, "merge-base", "--is-ancestor", moved, "main").Run() != nil {
		t.Errorf("main lacks the base branch's commit %s", moved)
	}
	if got := gitRun(t, env.P, "show", "main:base.txt"); got != "from main" {
		t.Errorf("base.txt on main: %q", got)
	}
	if got := gitRun(t, env.P, "show", "main:feature.txt"); got != "the slice" {
		t.Errorf("feature.txt on main: %q", got)
	}
	// slicer render ran in the worktree after the merge and its output was
	// committed before the fast-forward.
	if got := gitRun(t, env.P, "show", "main:rendered.txt"); got != "render" {
		t.Errorf("rendered.txt on main: %q", got)
	}
	if msg := gitRun(t, env.P, "log", "-1", "--format=%s", "main"); msg != "Render after merging main into "+item {
		t.Errorf("tip commit: %q", msg)
	}
	// The re-check ran once, in the worktree, on the merged and rendered tree,
	// while the base branch had not moved yet.
	if len(env.seen) != 1 {
		t.Fatalf("%d checks, want 1: %+v", len(env.seen), env.seen)
	}
	if s := env.seen[0]; s.dir != env.W || !s.hasBase || !s.treeClean || s.mainTip != moved {
		t.Errorf("check saw %+v (base tip %s)", s, moved)
	}
	slicer := env.log("slicer.log")
	render := indexOf(slicer, "render --json --root "+env.W)
	check := indexOf(slicer[max(render, 0):], "check --json --root "+env.W)
	land := indexOf(slicer, "land "+item)
	if render < 0 || check < 0 || land < render+check {
		t.Errorf("slicer calls out of order: %q", slicer)
	}
	if want := "land " + item + " --branch " + item + " --note Landed " + item + " --json --root " + env.P; land < 0 || slicer[land] != want {
		t.Errorf("land call %q, want %q", slicer[max(land, 0):], want)
	}
	calls := env.gitCalls()
	ff := indexOf(calls, "P: merge --ff-only "+item)
	if mergeIn := indexOf(calls, "W: merge -m "); mergeIn < 0 || ff < mergeIn {
		t.Errorf("git calls out of order: %q", calls)
	}
	if rec.Commit == nil || rec.Commit.Title != gitTitle {
		t.Errorf("commit: %+v", rec.Commit)
	}
}

func TestLandSkipsTheMergeAndRecheckWhenTheBaseDidNotMove(t *testing.T) {
	env := newLandEnv(t)
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	if len(env.seen) != 0 {
		t.Errorf("the check command ran %d times after approval, want 0", len(env.seen))
	}
	for _, l := range env.gitCalls() {
		if strings.HasPrefix(l, "W: merge -m ") {
			t.Errorf("merge call %q although the base did not move", l)
		}
	}
	if indexOf(env.log("slicer.log"), "render ") >= 0 {
		t.Errorf("slicer render ran although the base did not move")
	}
	if gitRun(t, env.P, "log", "-1", "--format=%s", "main") != gitTitle {
		t.Errorf("main tip is not the slice's commit")
	}
}

func TestLandStopsOnRedRecheck(t *testing.T) {
	env := newLandEnv(t)
	env.moveBase("base.txt", "from main\n")
	env.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && strings.Contains(req.Brief, "## Landing blocked") {
			env.moveBase("later.txt", "main moves again\n") // so the second landing merges again
		}
		return Result{}, nil
	}
	// call 0: the re-check after the merge is red; call 1: the checks after
	// the fix round are green; call 2: the second landing's re-check is red.
	red := CheckOutput{Output: "FAIL TestGolden sidebar\n"}
	env.setCheckOutputs(red, CheckOutput{OK: true, Output: "ok\n"}, red)

	env.approve()
	// A red re-check is treated like a conflict: one implementer fix round, the
	// check, the gate, and back to awaiting approval. Nothing reached main.
	rec := requireState(t, env.e, runrec.StateAwaitingApproval)
	if rec.LandFixes != 1 {
		t.Errorf("land fixes %d, want 1", rec.LandFixes)
	}
	if rec.LandRecheck {
		t.Errorf("the re-check marker stays set although the landing went to the fix round")
	}
	if env.refOK("main") && exec.Command("git", "-C", env.P, "merge-base", "--is-ancestor", item, "main").Run() == nil {
		t.Errorf("the slice branch reached main although the re-check was red")
	}
	if i := indexOf(env.gitCalls(), "P: merge --ff-only"); i >= 0 {
		t.Errorf("ff-merge before the fix round: %q", env.gitCalls())
	}
	var fix *Request
	for _, r := range env.runner.requests() {
		if strings.Contains(r.Brief, "## Landing blocked") {
			fix = &r
		}
	}
	if fix == nil || fix.Role != RoleImplementer {
		t.Fatalf("no implementer fix round with the landing brief: %v", env.runner.roles())
	}
	if !strings.Contains(fix.Brief, "FAIL TestGolden sidebar") {
		t.Errorf("the fix brief lacks the red output:\n%s", fix.Brief)
	}

	// The second landing is red again: stopped in Needs you, worktree kept.
	env.approve()
	rec = requireState(t, env.e, runrec.StateStopped)
	if rec.Reason != "land blocked: checks red after merging main" {
		t.Errorf("reason %q", rec.Reason)
	}
	if indexOf(env.gitCalls(), "P: merge --ff-only") >= 0 {
		t.Errorf("ff-merge although the re-check stayed red: %q", env.gitCalls())
	}
	if exec.Command("git", "-C", env.P, "merge-base", "--is-ancestor", item, "main").Run() == nil {
		t.Errorf("the slice branch reached main")
	}
	if _, err := os.Stat(env.W); err != nil {
		t.Errorf("the worktree is gone: %v", err)
	}
	if !env.refOK(item) {
		t.Errorf("the branch is gone")
	}
	if indexOf(env.log("slicer.log"), "land ") >= 0 {
		t.Errorf("slicer land ran although the landing was blocked")
	}
	// The finish step ended the ledger run once; the fix round and the stop
	// that follow do not end it again.
	if n := env.relayEnds(); n != 1 {
		t.Errorf("%d relay-run end calls, want 1", n)
	}
	// The re-check has its own log, apart from the checks of the rounds.
	if _, err := os.Stat(filepath.Join(env.store.RunDir(rec.ID), "land-checks-1.log")); err != nil {
		t.Errorf("no log of the re-check: %v", err)
	}
}

func TestLandPushesOnlyWhenSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		push bool
	}{{"off by default", false}, {"on", true}} {
		t.Run(tc.name, func(t *testing.T) {
			env := newLandEnv(t, func(p *registry.Project) { p.Push = tc.push })
			env.approve()
			requireState(t, env.e, runrec.StateCommitted)
			calls := env.gitCalls()
			push := indexOf(calls, "P: push")
			if !tc.push {
				if push >= 0 {
					t.Errorf("pushed although the setting is off: %q", calls)
				}
				return
			}
			// One push, in the main checkout, after the fast-forward and before the cleanup.
			if push < 0 {
				t.Fatalf("no push: %q", calls)
			}
			if calls[push] != "P: push" {
				t.Errorf("push call %q", calls[push])
			}
			ff := indexOf(calls, "P: merge --ff-only "+item)
			rm := indexOf(calls, "P: worktree remove")
			if !(ff >= 0 && ff < push && push < rm) {
				t.Errorf("order: ff %d, push %d, remove %d in %q", ff, push, rm, calls)
			}
		})
	}
}

func TestLandPausesWhenThePushFails(t *testing.T) {
	env := newLandEnv(t, func(p *registry.Project) { p.Push = true })
	writeFile(t, filepath.Join(env.fakes, "push-fails"), "")
	env.approve()
	rec := requireState(t, env.e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.State != runrec.StateLand || !strings.Contains(rec.Reason, "git push") {
		t.Errorf("pending %+v, reason %q", rec.Pending, rec.Reason)
	}
	// The branch is on main already; the worktree stays until the push is done.
	if gitRun(t, env.P, "rev-parse", "main") != gitRun(t, env.P, "rev-parse", item) {
		t.Errorf("main is not the slice branch")
	}
	if _, err := os.Stat(env.W); err != nil {
		t.Errorf("worktree removed before the push worked: %v", err)
	}
	if err := os.Remove(filepath.Join(env.fakes, "push-fails")); err != nil {
		t.Fatal(err)
	}
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, env.e, runrec.StateCommitted)
	if _, err := os.Stat(env.W); !os.IsNotExist(err) {
		t.Errorf("worktree kept after a landing that pushed: %v", err)
	}
}

func TestLandCleansUpAfterConfirm(t *testing.T) {
	env := newLandEnv(t)
	// Before the approval nothing is removed and nothing is merged.
	if _, err := os.Stat(env.W); err != nil {
		t.Fatal(err)
	}
	if len(env.log("git.log")) != 0 {
		t.Fatalf("git calls before the approval: %q", env.log("git.log"))
	}
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)

	calls := env.gitCalls()
	rm := indexOf(calls, "P: worktree remove "+env.W)
	del := indexOf(calls, "P: branch -d "+item)
	ff := indexOf(calls, "P: merge --ff-only "+item)
	if !(ff >= 0 && ff < rm && rm < del) {
		t.Fatalf("want ff-merge, worktree remove, branch -d in order: %q", calls)
	}
	for _, c := range calls {
		fields := strings.Fields(c)
		for _, f := range fields {
			if f == "-D" || f == "--force" || f == "-f" || f == "--delete" {
				t.Errorf("forced or unconfirmed removal %q", c)
			}
		}
	}
	if _, err := os.Stat(env.W); !os.IsNotExist(err) {
		t.Errorf("worktree still there: %v", err)
	}
	if env.refOK("refs/heads/" + item) {
		t.Errorf("branch %s still there", item)
	}
	// The cleanup is a git write ork made: it is in writes[].
	var seen []string
	for _, w := range env.e.Snapshot().Writes {
		if w.Tool == "git" {
			seen = append(seen, strings.Join(w.Argv, " "))
		}
	}
	for _, want := range []string{"worktree remove " + env.W, "branch -d " + item, "merge --ff-only " + item} {
		found := false
		for _, s := range seen {
			found = found || s == want
		}
		if !found {
			t.Errorf("writes[] lacks git %q: %q", want, seen)
		}
	}
}

func TestLandKeepsTheRunCommittedWhenTheCleanupFails(t *testing.T) {
	env := newLandEnv(t)
	gitRun(t, env.P, "worktree", "lock", env.W) // git worktree remove refuses a locked worktree
	env.approve()
	rec := requireState(t, env.e, runrec.StateCommitted)
	if !strings.Contains(rec.Reason, "cleanup") {
		t.Errorf("reason %q does not name the cleanup", rec.Reason)
	}
	if gitRun(t, env.P, "rev-parse", "main") != gitRun(t, env.P, "rev-parse", item) {
		t.Errorf("main is not the slice branch")
	}
	if !env.refOK("refs/heads/" + item) {
		t.Errorf("branch deleted although the worktree stayed")
	}
}

func TestLandConflictRunsOneFixRoundThenReturnsToApproval(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "main\n")
	var brief string
	env.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && strings.Contains(req.Brief, "## Landing blocked") {
			brief = req.Brief
			// the conflict is still in the tree: ork aborted nothing
			if _, err := os.Stat(filepath.Join(req.Worktree, ".git")); err != nil {
				t.Error(err)
			}
			if got := gitRun(t, req.Worktree, "diff", "--name-only", "--diff-filter=U"); got != "shared.txt" {
				t.Errorf("unmerged files at the start of the fix round: %q", got)
			}
			writeFile(t, filepath.Join(req.Worktree, "shared.txt"), "resolved\n")
			gitRun(t, req.Worktree, "add", "-A")
			gitRun(t, req.Worktree, "commit", "-q", "--no-edit")
		}
		return Result{}, nil
	}
	env.approve()
	rec := requireState(t, env.e, runrec.StateAwaitingApproval)
	if rec.LandFixes != 1 {
		t.Errorf("land fixes %d, want 1", rec.LandFixes)
	}
	for _, want := range []string{"## Landing blocked", "merge conflict", "shared.txt"} {
		if !strings.Contains(brief, want) {
			t.Errorf("fix brief lacks %q:\n%s", want, brief)
		}
	}
	for _, l := range env.gitCalls() {
		if strings.Contains(l, "--abort") || strings.Contains(l, "reset") {
			t.Errorf("ork undid the merge by hand: %q", l)
		}
	}
	if exec.Command("git", "-C", env.P, "merge-base", "--is-ancestor", item, "main").Run() == nil {
		t.Fatalf("the slice reached main before the second approval")
	}
	// The check ran after the fix round (call 0: the checks state). The
	// approval lands: the base did not move again, so no merge is repeated.
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	if got := gitRun(t, env.P, "show", "main:shared.txt"); got != "resolved" {
		t.Errorf("shared.txt on main: %q", got)
	}
	if got := gitRun(t, env.P, "show", "main:feature.txt"); got != "the slice" {
		t.Errorf("feature.txt on main: %q", got)
	}
	if n := env.relayEnds(); n != 1 {
		t.Errorf("%d relay-run end calls over two finish steps, want 1", n)
	}
	if got := len(env.runner.requests()); got < 3 {
		t.Errorf("%d role requests", got)
	}
}

// The finish step ends the ledger run. A landing fix round starts after that:
// its rounds run without ledger steps (relay-run would refuse them with
// run_ended), and the engine run record keeps the rounds (S153).
func TestLandFixRoundRecordsNoLedgerStepsAfterTheRunEnded(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "main\n")
	env.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && strings.Contains(req.Brief, "## Landing blocked") {
			writeFile(t, filepath.Join(req.Worktree, "shared.txt"), "resolved\n")
			gitRun(t, req.Worktree, "add", "-A")
			gitRun(t, req.Worktree, "commit", "-q", "--no-edit")
		}
		return Result{}, nil
	}
	env.approve()
	requireState(t, env.e, runrec.StateAwaitingApproval)
	env.approve()
	rec := requireState(t, env.e, runrec.StateCommitted)
	calls := env.log("slicer.log")
	end := indexOf(calls, "relay-run end ")
	if end < 0 {
		t.Fatalf("the ledger run was never ended: %q", calls)
	}
	for _, l := range calls[end+1:] {
		if strings.HasPrefix(l, "relay-run step ") {
			t.Errorf("a ledger step after the run ended: %q", l)
		}
	}
	// The ledger steps before the end are still recorded.
	if indexOf(calls[:end], "relay-run step ") < 0 {
		t.Errorf("no ledger steps before the run ended: %q", calls[:end])
	}
	// The fix round and the second finish step ran and stay in the run record,
	// without a ledger step id.
	var withStep, withoutStep, fixRound int
	for _, r := range rec.Roles {
		if r.Step != "" {
			withStep++
		} else {
			withoutStep++
		}
		if r.Role == RoleImplementer && r.Round > 0 {
			fixRound++
		}
	}
	if withStep == 0 || withoutStep < 2 || fixRound < 3 {
		t.Errorf("roles in the record: %d with a step, %d without, %d implementer rounds: %+v", withStep, withoutStep, fixRound, rec.Roles)
	}
}

func TestLandConflictTwiceStopsAndKeepsTheWorktree(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.W, "shared.txt"), "branch\n")
	env.moveBase("shared.txt", "main\n")
	env.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && strings.Contains(req.Brief, "## Landing blocked") {
			writeFile(t, filepath.Join(req.Worktree, "shared.txt"), "resolved\n")
			gitRun(t, req.Worktree, "add", "-A")
			gitRun(t, req.Worktree, "commit", "-q", "--no-edit")
			env.moveBase("shared.txt", "main again\n") // main changes the same line again
		}
		return Result{}, nil
	}
	env.approve()
	requireState(t, env.e, runrec.StateAwaitingApproval)
	env.approve()
	rec := requireState(t, env.e, runrec.StateStopped)
	if rec.Reason != "land blocked: merge conflict" {
		t.Errorf("reason %q", rec.Reason)
	}
	if _, err := os.Stat(env.W); err != nil {
		t.Errorf("the worktree is gone: %v", err)
	}
	if !env.refOK("refs/heads/" + item) {
		t.Errorf("branch gone")
	}
	if indexOf(env.gitCalls(), "P: merge --ff-only") >= 0 || indexOf(env.log("slicer.log"), "land ") >= 0 {
		t.Errorf("landed although the conflict stayed")
	}
	if rec.LandFixes != 1 {
		t.Errorf("land fixes %d, want 1", rec.LandFixes)
	}
	if n := env.relayEnds(); n != 1 {
		t.Errorf("%d relay-run end calls, want 1", n)
	}
}

func TestLandRunsSlicerLandAndCommitsTheTrackingOnTheBase(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.fakes, "finish"), "handoff")
	writeFile(t, filepath.Join(env.fakes, "land-writes"), "")
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	// slicer land marked the item done in the main checkout; ork commits that.
	if got := gitRun(t, env.P, "status", "--porcelain"); got != "" {
		t.Errorf("main checkout left dirty: %q", got)
	}
	if got := gitRun(t, env.P, "log", "-1", "--format=%s", "main"); got != item+" done" {
		t.Errorf("main tip %q, want %q", got, item+" done")
	}
	if got := gitRun(t, env.P, "show", "main:.slicer/index.json"); got != "done" {
		t.Errorf("tracking on main: %q", got)
	}
}

func TestLandPausesWhenTheFastForwardFails(t *testing.T) {
	env := newLandEnv(t)
	// An untracked file in the main checkout that the slice also adds: git
	// refuses to overwrite it.
	writeFile(t, filepath.Join(env.P, "feature.txt"), "mine\n")
	env.approve()
	rec := requireState(t, env.e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.State != runrec.StateLand || !strings.Contains(rec.Reason, "git merge --ff-only") {
		t.Errorf("pending %+v, reason %q", rec.Pending, rec.Reason)
	}
	if _, err := os.Stat(env.W); err != nil {
		t.Errorf("worktree removed: %v", err)
	}
	if indexOf(env.log("slicer.log"), "land ") >= 0 {
		t.Errorf("slicer land ran before the merge")
	}
	if err := os.Remove(filepath.Join(env.P, "feature.txt")); err != nil {
		t.Fatal(err)
	}
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, env.e, runrec.StateCommitted)
}

// A landing that stopped between the merge of the base branch and a green
// re-check must not fast-forward an unchecked tree when it is retried (the
// merge commit is in the branch, so "the base is in" no longer proves a check).
func TestLandRetryAfterAPauseStillRechecksTheMergedTree(t *testing.T) {
	env := newLandEnv(t)
	moved := env.moveBase("base.txt", "from main\n")
	writeFile(t, filepath.Join(env.fakes, "render-fails"), "")
	env.approve()
	rec := requireState(t, env.e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.State != runrec.StateLand || !strings.Contains(rec.Reason, "slicer render") {
		t.Fatalf("pending %+v, reason %q", rec.Pending, rec.Reason)
	}
	if indexOf(env.gitCalls(), "P: merge --ff-only") >= 0 || len(env.seen) != 0 {
		t.Fatalf("landed or checked before the render worked: %q", env.gitCalls())
	}
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, env.e, runrec.StateCommitted)
	if len(env.seen) != 1 {
		t.Fatalf("%d checks after the retry, want 1: %+v", len(env.seen), env.seen)
	}
	if s := env.seen[0]; s.dir != env.W || !s.hasBase || !s.treeClean || s.mainTip != moved {
		t.Errorf("the check saw %+v before the fast-forward (base tip %s)", s, moved)
	}
	if got := gitRun(t, env.P, "show", "main:rendered.txt"); got != "render" {
		t.Errorf("rendered.txt on main: %q", got)
	}
}

// A red re-check is not left pending: after the fix round the next landing,
// with the base unmoved, does not check again, and the marker is gone.
func TestLandRecheckMarkerClearsAfterAGreenCheck(t *testing.T) {
	env := newLandEnv(t)
	env.moveBase("base.txt", "from main\n")
	env.approve()
	rec := requireState(t, env.e, runrec.StateCommitted)
	if rec.LandRecheck {
		t.Errorf("the re-check marker is still set after a landing")
	}
}

// The commit of the tracking change on the base branch takes only .slicer: a
// file the owner staged in the main checkout stays staged and out of it.
func TestLandTrackingCommitLeavesTheOwnersStagedFilesAlone(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.fakes, "finish"), "handoff")
	writeFile(t, filepath.Join(env.fakes, "land-writes"), "")
	writeFile(t, filepath.Join(env.P, "notes.txt"), "owner work in progress\n")
	gitRun(t, env.P, "add", "notes.txt")
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	if got := gitRun(t, env.P, "log", "-1", "--format=%s", "main"); got != item+" done" {
		t.Fatalf("main tip %q", got)
	}
	if files := gitRun(t, env.P, "show", "--name-only", "--format=", "main"); files != ".slicer/index.json" {
		t.Errorf("the done commit holds %q, want only .slicer/index.json", files)
	}
	if got := gitRun(t, env.P, "diff", "--cached", "--name-only"); got != "notes.txt" {
		t.Errorf("staged after landing: %q, want the owner's notes.txt", got)
	}
}

// A crash between setting the re-check marker and the merge leaves the marker
// set with the base not merged. The retry must still merge, then render and
// re-check (the marker is set before the merge, so it never lags the merge).
func TestLandMarkerSetButMergeNotDoneStillMerges(t *testing.T) {
	env := newLandEnv(t)
	moved := env.moveBase("base.txt", "from main\n")
	if err := env.run.Update(func(r *runrec.Record) { r.LandRecheck = true }); err != nil {
		t.Fatal(err)
	}
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)
	if got := gitRun(t, env.P, "rev-list", "--merges", "--count", "main"); got != "1" {
		t.Errorf("%s merge commits on main, want the merge of the base branch", got)
	}
	if len(env.seen) != 1 {
		t.Fatalf("%d checks, want 1: %+v", len(env.seen), env.seen)
	}
	if s := env.seen[0]; !s.hasBase || !s.treeClean || s.mainTip != moved {
		t.Errorf("the check saw %+v (base tip %s)", s, moved)
	}
	if got := gitRun(t, env.P, "show", "main:base.txt"); got != "from main" {
		t.Errorf("base.txt on main: %q", got)
	}
}

// The marker is set before the merge: a merge that fails for a reason other
// than a conflict leaves it set, so the retry resumes in the same place and
// still merges.
func TestLandSetsTheMarkerBeforeTheMerge(t *testing.T) {
	env := newLandEnv(t)
	moved := env.moveBase("base.txt", "from main\n")
	writeFile(t, filepath.Join(env.fakes, "merge-fails"), "")
	env.approve()
	rec := requireState(t, env.e, runrec.StatePaused)
	if !rec.LandRecheck || !strings.Contains(rec.Reason, "git merge main") {
		t.Fatalf("marker %v, reason %q", rec.LandRecheck, rec.Reason)
	}
	if err := os.Remove(filepath.Join(env.fakes, "merge-fails")); err != nil {
		t.Fatal(err)
	}
	if err := env.e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec = requireState(t, env.e, runrec.StateCommitted)
	if rec.LandRecheck {
		t.Errorf("marker still set after the landing")
	}
	if len(env.seen) != 1 || !env.seen[0].hasBase || env.seen[0].mainTip != moved {
		t.Errorf("re-check after the retried merge: %+v", env.seen)
	}
	if got := gitRun(t, env.P, "rev-list", "--merges", "--count", "main"); got != "1" {
		t.Errorf("%s merge commits on main", got)
	}
}

// Landings of one project go one at a time; the next waits for the lock and
// merges the base branch the first landing moved.
func TestLandingsOfOneProjectGoOneAtATime(t *testing.T) {
	one := newLandEnv(t)
	moved := one.moveBase("base.txt", "from main\n")
	two := one.secondRun()

	entered, release := make(chan struct{}), make(chan struct{})
	one.beforeCheck = func() { close(entered); <-release }
	done := make(chan error, 2)
	go func() { done <- one.e.Approve(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("the first landing never reached its re-check")
	}
	go func() { done <- two.e.Approve(context.Background()) }()
	deadline := time.Now().Add(30 * time.Second)
	for one.deps.Landings.waiters(one.P) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the second landing is not waiting for the lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(two.seen) != 0 || exec.Command("git", "-C", one.P, "merge-base", "--is-ancestor", "S05", "main").Run() == nil {
		t.Fatalf("the second landing ran beside the first: %+v", two.seen)
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	requireState(t, one.e, runrec.StateCommitted)
	requireState(t, two.e, runrec.StateCommitted)
	for _, f := range []string{"feature.txt", "feature2.txt", "base.txt"} {
		if _, err := os.Stat(filepath.Join(one.P, f)); err != nil {
			t.Errorf("%s is not on main: %v", f, err)
		}
	}
	if len(two.seen) != 1 || !two.seen[0].hasBase || two.seen[0].mainTip == moved {
		t.Errorf("the second landing did not merge the base the first one moved: %+v", two.seen)
	}
	if got := gitRun(t, one.P, "status", "--porcelain"); got != "" {
		t.Errorf("main checkout dirty: %q", got)
	}
}

// A landing leaves the time it landed in the run record, apart from the
// outcome of its cleanup.
func TestLandRecordsWhenTheRunLanded(t *testing.T) {
	env := newLandEnv(t)
	env.approve()
	rec := requireState(t, env.e, runrec.StateCommitted)
	if rec.LandedAt.IsZero() || rec.Reason != "" {
		t.Errorf("landed at %v, reason %q", rec.LandedAt, rec.Reason)
	}
	// A landing whose cleanup failed is landed too, with the reason kept.
	bad := newLandEnv(t)
	gitRun(t, bad.P, "worktree", "lock", bad.W)
	bad.approve()
	rec = requireState(t, bad.e, runrec.StateCommitted)
	if rec.LandedAt.IsZero() || !strings.Contains(rec.Reason, "cleanup failed") {
		t.Errorf("landed at %v, reason %q", rec.LandedAt, rec.Reason)
	}
}

// A run that stopped before it landed has no landing time.
func TestAStoppedLandingIsNotLanded(t *testing.T) {
	env := newLandEnv(t)
	env.moveBase("base.txt", "from main\n")
	env.setCheckOutputs(CheckOutput{Output: "FAIL\n"})
	env.runner.script = func(req Request, n int) (Result, error) { return Result{}, nil }
	env.setCheckOutputs(CheckOutput{Output: "FAIL\n"}, CheckOutput{OK: true}, CheckOutput{Output: "FAIL\n"})
	env.approve()
	rec := env.e.Snapshot()
	if !rec.LandedAt.IsZero() {
		t.Errorf("landed at %v although the landing was blocked", rec.LandedAt)
	}
}

// A landing that waits for the project's lock gives up when its context ends:
// nobody waits any more, the slot goes to the next landing, and the cancelled
// run can land later from where it stopped.
func TestLandingWaitingForTheLockGivesUpOnCancel(t *testing.T) {
	one := newLandEnv(t)
	one.moveBase("base.txt", "from main\n")
	two := one.secondRun()
	entered, release := make(chan struct{}), make(chan struct{})
	one.beforeCheck = func() { close(entered); <-release }
	oneDone := make(chan error, 1)
	go func() { oneDone <- one.e.Approve(context.Background()) }()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	twoDone := make(chan error, 1)
	go func() { twoDone <- two.e.Approve(ctx) }()
	deadline := time.Now().Add(30 * time.Second)
	for one.deps.Landings.waiters(one.P) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the second landing is not waiting for the lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-twoDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("the waiting landing returned %v, want the cancellation", err)
	}
	if n := one.deps.Landings.waiters(one.P); n != 0 {
		t.Errorf("%d landings still wait after the cancel", n)
	}
	if rec := two.e.Snapshot(); rec.State != runrec.StateLand || len(two.seen) != 0 {
		t.Errorf("the cancelled landing is %q with %d checks", rec.State, len(two.seen))
	}

	close(release)
	if err := <-oneDone; err != nil {
		t.Fatal(err)
	}
	// The slot is free: the cancelled run lands now.
	if err := two.e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, one.e, runrec.StateCommitted)
	requireState(t, two.e, runrec.StateCommitted)
	if _, err := os.Stat(filepath.Join(one.P, "feature2.txt")); err != nil {
		t.Errorf("the second slice did not land: %v", err)
	}
}

// A landing fix round returns the slice to awaiting approval, and the second
// approval must not write the human-checks note again (S148).
func TestSecondApprovalAfterALandingFixRoundRecordsTheChecksOnce(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.fakes, "show.json"), humanSlice(checkOne))
	if err := env.run.Update(func(r *runrec.Record) {
		r.Card.HumanChecks = []runrec.HumanCheck{{Text: checkOne, State: runrec.CheckTicked}}
	}); err != nil {
		t.Fatal(err)
	}
	env.moveBase("base.txt", "from main\n")
	// call 0: the re-check after the merge is red; call 1: the checks after the
	// fix round are green.
	env.setCheckOutputs(CheckOutput{Output: "FAIL TestGolden sidebar\n"}, CheckOutput{OK: true, Output: "ok\n"})

	env.approve()
	rec := requireState(t, env.e, runrec.StateAwaitingApproval)
	if rec.LandFixes != 1 {
		t.Fatalf("land fixes %d, want 1", rec.LandFixes)
	}
	if rec.Card == nil || len(rec.Card.HumanChecks) != 1 || rec.Card.HumanChecks[0].State != runrec.CheckTicked {
		t.Fatalf("the mark did not carry over the fix round: %+v", rec.Card)
	}
	env.approve()
	requireState(t, env.e, runrec.StateCommitted)

	notes := 0
	for _, l := range env.log("slicer.log") {
		if strings.HasPrefix(l, "note ") {
			notes++
		}
	}
	if notes != 1 {
		t.Errorf("%d human-checks notes after two approvals, want 1:\n%s", notes, strings.Join(env.log("slicer.log"), "\n"))
	}
}
