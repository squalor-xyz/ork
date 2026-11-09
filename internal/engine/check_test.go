// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// The check command runs in the project's own folder of the worktree (S293,
// *proposed*): a project that lives in a subfolder of its repository (the
// prism-ui case, libraries/prism-ui in the platforms repo) has its worktree
// rooted at the repository top, not at the project's folder.

const monorepoSub = "libraries/p"

// isolateGit keeps the test's git calls away from the developer's own git
// configuration.
func isolateGit(t *testing.T, root string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "Test")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "test@example.invalid")
	}
}

// realMonorepo makes a git repository R with the project at R/libraries/p and
// a worktree W of it.
func realMonorepo(t *testing.T) (R, P, W string) {
	t.Helper()
	root := t.TempDir()
	isolateGit(t, root)
	R = filepath.Join(root, "repo")
	P = filepath.Join(R, filepath.FromSlash(monorepoSub))
	W = filepath.Join(R, ".worktrees", item)
	writeFile(t, filepath.Join(P, "README.md"), "p\n")
	gitRun(t, R, "init", "-q", "-b", "main")
	gitRun(t, R, "add", "-A")
	gitRun(t, R, "commit", "-q", "-m", "init")
	gitRun(t, R, "worktree", "add", "-q", W, "-b", item)
	return R, P, W
}

func TestCheckDirIsTheProjectFolderInAMonorepo(t *testing.T) {
	_, P, W := realMonorepo(t)
	if got := gitRun(t, P, "rev-parse", "--show-prefix"); got != monorepoSub+"/" {
		t.Fatalf("git's prefix of the project is %q, want %q", got, monorepoSub+"/")
	}
	e := &Engine{}
	got, err := e.CheckDir(context.Background(), P, W)
	if err != nil {
		t.Fatalf("CheckDir: %v", err)
	}
	if want := filepath.Join(W, "libraries", "p"); got != want {
		t.Errorf("CheckDir = %q, want %q", got, want)
	}
}

func TestCheckDirIsTheWorktreeRootWhenTheProjectIsTheRepoRoot(t *testing.T) {
	R, _, W := realMonorepo(t)
	if got := gitRun(t, R, "rev-parse", "--show-prefix"); got != "" {
		t.Fatalf("git's prefix at the repository root is %q, want empty", got)
	}
	e := &Engine{}
	got, err := e.CheckDir(context.Background(), R, W)
	if err != nil {
		t.Fatalf("CheckDir: %v", err)
	}
	if got != W {
		t.Errorf("CheckDir = %q, want the worktree %q", got, W)
	}
}

// monorepoHarness is the fake-git harness for a project whose folder is
// libraries/p of its repository. The worktree is not on disk until the
// implementer's first round (the fake git creates nothing), so the runner
// makes the project's folder there, as a real checkout has it.
func monorepoHarness(t *testing.T, mkdir bool, opts ...func(*harnessOpts)) *harness {
	t.Helper()
	all := append([]func(*harnessOpts){func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{{Tool: "git", Args: []string{"-C", P, "rev-parse", "--show-prefix"}, Stdout: monorepoSub + "/\n"}}
		}
	}}, opts...)
	h := newHarness(t, all...)
	if mkdir {
		h.runner.script = func(req Request, n int) (Result, error) {
			if err := os.MkdirAll(filepath.Join(h.W, "libraries", "p"), 0o755); err != nil {
				return Result{}, err
			}
			return Result{}, nil
		}
	}
	return h
}

// pwdCheck is a check command that records its directory in log and is red
// until it has run twice, so that the S239 rerun runs it again.
func pwdCheck(log string) string {
	return fmt.Sprintf("pwd >> '%s'; test \"$(wc -l < '%s')\" -ge 2", log, log)
}

// requirePwdLog asserts that the command ran twice, both times in want.
func requirePwdLog(t *testing.T, log, want string) {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the check command did not run: %v", err)
	}
	// The worktree is gone after a landing, so the paths are compared as
	// written: the shell's pwd keeps the PWD that Go sets from the command's dir.
	for i, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line != want {
			t.Errorf("run %d was in %q, want %q", i+1, line, want)
		}
	}
	if n := strings.Count(string(b), "\n"); n != 2 {
		t.Errorf("the command ran %d times, want 2 (the run and the S239 rerun): %q", n, b)
	}
}

func TestRunChecksRunsTheCommandInTheProjectFolder(t *testing.T) {
	t.Run("round check and its rerun", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "pwd.log")
		h := monorepoHarness(t, true, func(o *harnessOpts) {
			o.project = func(p *registry.Project) { p.CheckCommand = pwdCheck(log) }
		})
		h.deps.Check = RunCheck
		e := h.start()
		rec := requireState(t, e, runrec.StateAwaitingApproval)
		if len(rec.Checks) != 1 || !rec.Checks[0].OK || rec.Checks[0].Attempts != 2 {
			t.Fatalf("checks: %+v, want one green check run twice", rec.Checks)
		}
		requirePwdLog(t, log, filepath.Join(h.W, "libraries", "p"))
	})

	t.Run("landing re-check", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "pwd.log")
		env := newLandEnv(t, subProject(monorepoSub), func(p *registry.Project) { p.CheckCommand = pwdCheck(log) })
		// The project's folder is in the base and so in the worktree.
		writeFile(t, filepath.Join(env.P, "libraries", "p", "README.md"), "p\n")
		gitRun(t, env.P, "add", "-A")
		gitRun(t, env.P, "commit", "-q", "-m", "the project")
		gitRun(t, env.W, "merge", "--ff-only", "main")
		env.moveBase("base.txt", "from main\n") // the base moves, so the landing merges and re-checks
		env.deps.Check = RunCheck
		env.e = Load(env.deps, env.proj, env.run)
		env.approve()
		rec := requireState(t, env.e, runrec.StateCommitted)
		requirePwdLog(t, log, filepath.Join(env.W, "libraries", "p"))
		if _, err := os.Stat(filepath.Join(env.store.RunDir(rec.ID), "land-checks-1.log")); err != nil {
			t.Errorf("no log of the re-check: %v", err)
		}
	})
}

// requireChecksPause checks the run paused in the checks with an error whose
// reason holds every part, and that the project command did not run.
func requireChecksPause(t *testing.T, h *harness, e *Engine, parts ...string) {
	t.Helper()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingError || rec.Pending.State != runrec.StateChecks {
		t.Errorf("pending %+v, want an error pause in the checks", rec.Pending)
	}
	for _, part := range parts {
		if !strings.Contains(rec.Reason, part) {
			t.Errorf("reason %q lacks %q", rec.Reason, part)
		}
	}
	if len(h.checks.calls) != 0 {
		t.Errorf("the check command ran: %q", h.checks.calls)
	}
}

func TestCheckDirMissingInTheWorktreePausesWithAReason(t *testing.T) {
	h := monorepoHarness(t, false)
	e := h.start()
	requireChecksPause(t, h, e, filepath.Join(h.W, "libraries", "p"))
}

func TestCheckDirReadErrorPausesWithAReason(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{{Tool: "git", Args: []string{"-C", P, "rev-parse", "--show-prefix"}, Exit: 128, Stderr: "fatal: not a git repository\n"}}
		}
	})
	e := h.start()
	requireChecksPause(t, h, e, "fatal: not a git repository")
}

func TestCheckLogNamesTheDirectory(t *testing.T) {
	const cmd = "go test ./..."
	t.Run("in a subfolder", func(t *testing.T) {
		h := monorepoHarness(t, true)
		h.checks.outputs = []CheckOutput{{Output: "FAIL one\n"}, {OK: true, Output: "ok\n"}}
		e := h.start()
		rec := requireState(t, e, runrec.StateAwaitingApproval)
		log := h.readReport(rec, "checks-1.log")
		if want := "$ " + cmd + "\n(in libraries/p)\nFAIL one\n"; !strings.HasPrefix(log, want) {
			t.Errorf("the log starts %q, want %q", log[:min(len(log), len(want))], want)
		}
		if want := "\n\n$ " + cmd + rerunMarker + "\n(in libraries/p)\nok\n"; !strings.Contains(log, want) {
			t.Errorf("the rerun heading is not followed by the directory:\n%s", log)
		}
	})
	t.Run("at the repository root", func(t *testing.T) {
		h := newHarness(t)
		e := h.start()
		rec := requireState(t, e, runrec.StateAwaitingApproval)
		log := h.readReport(rec, "checks-1.log")
		if want := "$ " + cmd + "\nok  \tall packages\n"; !strings.HasPrefix(log, want) {
			t.Errorf("the log starts %q, want %q", log[:min(len(log), len(want))], want)
		}
		if strings.Contains(log, "(in ") {
			t.Errorf("the log names a directory for a project at the repository root:\n%s", log)
		}
	})
}
