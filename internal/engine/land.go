// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// land runs after the commit (adj 162, landing.in-ork; S119): merge the base
// branch into the slice branch when it moved, render, re-check, fast-forward
// the base branch, run `slicer land`, push when the project says so, and
// remove the worktree and the branch. The owner's approval is the confirm of
// the cleanup (adj 152, git.cleanup-key). Each step reads git first, so a
// retry after a pause picks up where the landing stopped.
//
// A merge conflict, or a red re-check after the merge, gets one implementer
// fix round with the details as the brief, then the checks, the gate and a new
// approval. A second block stops the slice in Needs you with the worktree kept.
func (e *Engine) land(ctx context.Context) error {
	// One landing per project at a time: the next waits, then reads the base
	// branch the first one moved.
	release, err := e.d.landings().Acquire(ctx, e.proj.Path)
	if err != nil {
		return err
	}
	defer release()
	rec := e.run.Snapshot()
	P, W, B := e.proj.Path, rec.Worktree, rec.Branch
	pauseWith := func(reason string) error {
		return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateLand, Reason: reason})
	}
	if B == "" || W == "" || strings.HasPrefix(B, "-") || strings.HasPrefix(W, "-") {
		return pauseWith("the run has no usable branch and worktree to land")
	}
	out, err := e.gitRead(ctx, P, "symbolic-ref", "--short", "HEAD")
	base := strings.TrimSpace(out)
	if err != nil || base == "" {
		return pauseWith("the main checkout is not on a branch: " + errText(err))
	}
	if base == B {
		return pauseWith("the main checkout is on the slice branch " + B)
	}
	merged, err := e.isAncestor(ctx, P, B, base)
	if err != nil {
		return pauseWith("git merge-base: " + err.Error())
	}
	if !merged {
		proceed, err := e.mergeBaseIn(ctx, rec, base)
		if !proceed {
			return err
		}
		if _, err := e.gitWrite(ctx, P, "merge", "--ff-only", B); err != nil {
			return pauseWith("git merge --ff-only " + B + ": " + errText(err))
		}
	}
	if err := e.d.Slicer.Land(ctx, e.wr, P, rec.Item, B, "Landed "+B); err != nil {
		return pauseWith("slicer land: " + err.Error())
	}
	// In handoff mode slicer land marked the item done in the main checkout.
	// Commit that, as the commands slicer prints say to.
	status, err := e.gitRead(ctx, P, "status", "--porcelain", "--", ".slicer")
	if err != nil {
		return pauseWith("git status: " + err.Error())
	}
	if strings.TrimSpace(status) != "" {
		if _, err := e.gitWrite(ctx, P, "add", "-A", "--", ".slicer"); err != nil {
			return pauseWith("git add .slicer: " + err.Error())
		}
		if _, err := e.gitWrite(ctx, P, "commit", "-m", rec.Item+" done", "--", ".slicer"); err != nil {
			return pauseWith("git commit " + rec.Item + " done: " + err.Error())
		}
	}
	if e.proj.Push {
		if _, err := e.gitWrite(ctx, P, "push"); err != nil {
			return pauseWith("git push: " + err.Error())
		}
	}
	// The approval confirmed the cleanup: remove the worktree, then delete the
	// branch with -d, which refuses a branch that is not merged. Never forced.
	reason := ""
	for _, args := range [][]string{{"worktree", "remove", W}, {"branch", "-d", B}} {
		if _, err := e.gitWrite(ctx, P, args...); err != nil {
			reason = "landed; cleanup failed at git " + strings.Join(args, " ") + ": " + err.Error()
			break
		}
	}
	return e.run.Update(func(r *runrec.Record) {
		r.Pending = nil
		r.Reason = reason
		if r.LandedAt.IsZero() {
			r.LandedAt = e.d.now()
		}
		r.State = runrec.StateCommitted
	})
}

// mergeBaseIn merges the base branch into the slice branch in the worktree
// when the base moved, renders and re-checks. proceed is true when the branch
// is ready to fast-forward onto the base; otherwise the slice has been moved
// to a fix round, a stop or a pause, and err is the result to return.
func (e *Engine) mergeBaseIn(ctx context.Context, rec runrec.Record, base string) (proceed bool, err error) {
	W, B := rec.Worktree, rec.Branch
	pauseWith := func(reason string) (bool, error) {
		return false, e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateLand, Reason: reason})
	}
	// The marker is set before the merge and cleared by a green re-check, so a
	// stop at any point in between (a failed render or check, a crash) is
	// resumed at the re-check and never fast-forwards an unchecked tree. A
	// retry with the marker set still merges when the base is not in yet.
	if !rec.LandRecheck {
		has, err := e.isAncestor(ctx, W, base, "HEAD")
		if err != nil {
			return pauseWith("git merge-base: " + err.Error())
		}
		if has {
			return true, nil
		}
		if err := e.run.Update(func(r *runrec.Record) { r.LandRecheck = true }); err != nil {
			return false, err
		}
	}
	has, err := e.isAncestor(ctx, W, base, "HEAD")
	if err != nil {
		return pauseWith("git merge-base: " + err.Error())
	}
	if !has {
		res, err := e.gitWrite(ctx, W, "merge", "-m", "Merge "+base+" into "+B, base)
		if err != nil {
			// ork aborts nothing: the conflict stays in the worktree for the fix round.
			files, ferr := e.gitRead(ctx, W, "diff", "--name-only", "--diff-filter=U")
			if ferr == nil && strings.TrimSpace(files) != "" {
				detail := fmt.Sprintf("`git merge %s` in the worktree stopped on conflicts in:\n\n%s\n\ngit said:\n\n```\n%s\n```", base, strings.TrimSpace(files), tail(outputText(res), 6000))
				return false, e.landBlocked(ctx, "merge conflict", detail)
			}
			return pauseWith("git merge " + base + ": " + errText(err))
		}
	}
	if err := e.d.Slicer.Render(ctx, e.wr, W); err != nil {
		return pauseWith("slicer render: " + err.Error())
	}
	status, err := e.treeStatus(ctx, W)
	if err != nil {
		return pauseWith(err.Error())
	}
	if strings.TrimSpace(status) != "" {
		if _, err := e.gitWrite(ctx, W, "add", "-A"); err != nil {
			return pauseWith("git add: " + err.Error())
		}
		if _, err := e.gitWrite(ctx, W, "commit", "-m", "Render after merging "+base+" into "+B); err != nil {
			return pauseWith("git commit: " + err.Error())
		}
	}
	run, paused, err := e.runChecks(ctx, e.run.Snapshot(), runrec.StateLand, "land-checks")
	if paused || err != nil {
		return false, err
	}
	if !run.ok {
		b, _ := os.ReadFile(filepath.Join(e.run.Dir(), run.log))
		detail := fmt.Sprintf("The checks are red after `git merge %s` and `slicer render`:\n\n```\n%s\n```", base, tail(string(b), 30000))
		return false, e.landBlocked(ctx, "checks red after merging "+base, detail)
	}
	if err := e.run.Update(func(r *runrec.Record) { r.LandRecheck = false }); err != nil {
		return false, err
	}
	return true, nil
}

// landBlocked sends a blocked landing to its one implementer fix round, or
// stops the slice when it already had one. The worktree and the branch stay.
func (e *Engine) landBlocked(ctx context.Context, what, detail string) error {
	rec := e.run.Snapshot()
	if rec.LandFixes >= 1 {
		return e.stop(ctx, "land blocked: "+what)
	}
	name := fmt.Sprintf("land-%d.md", rec.Round)
	text := "Landing blocked: " + what + "\n\n" + detail + "\n"
	if err := os.WriteFile(filepath.Join(e.run.Dir(), name), []byte(text), 0o600); err != nil {
		return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateLand, Reason: "land file: " + err.Error()})
	}
	if err := e.run.Update(func(r *runrec.Record) { r.LandFixes++; r.LandRecheck = false }); err != nil {
		return err
	}
	return e.bumpRound(ctx, runrec.Feedback{Kind: "land", File: name})
}

// isAncestor reports whether a is an ancestor of b in the repository at dir:
// exit 0 is yes, exit 1 is no, anything else is an error.
func (e *Engine) isAncestor(ctx context.Context, dir, a, b string) (bool, error) {
	res, err := clirun.Run(ctx, clirun.Call{Bin: e.d.gitBin(), Args: []string{"-C", dir, "merge-base", "--is-ancestor", a, b}, Timeout: 30 * time.Second})
	if err == nil {
		return true, nil
	}
	var exit *clirun.ExitError
	if errors.As(err, &exit) && res.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// outputText is what a failed git call printed, for a reason.
func outputText(res clirun.Result) string {
	s := strings.TrimSpace(string(res.Stdout) + string(res.Stderr))
	if s == "" {
		return ""
	}
	return "\n" + s
}

// MarkCleaned records that the owner's cleanup removed the worktree and the
// branch (S95), and drops the reason a landed run kept for a failed cleanup
// (S119).
func (e *Engine) MarkCleaned(at time.Time) error {
	return e.run.Update(func(r *runrec.Record) { runrec.MarkCleaned(r, at) })
}
