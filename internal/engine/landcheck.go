// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// LandCheck is an advisory, current read of what would stop a landing.
// Reason describes branch states; Error is an unavailable check, not a warning.
type LandCheck struct {
	Base     string   `json:"base,omitempty"`
	Blockers []string `json:"blockers,omitempty"`
	Error    string   `json:"error,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// CheckLanding never writes. Unlike dirtyOverlap, it includes the worktree's
// uncommitted changes, because approval precedes the slice commit.
// Only exact path overlap is detected; directory-versus-file clashes are not.
func CheckLanding(ctx context.Context, deps Deps, project registry.Project, rec runrec.Record) LandCheck {
	e := &Engine{d: deps}
	result := LandCheck{}
	fail := func(op string, err error) LandCheck {
		return LandCheck{Base: result.Base, Error: op + ": " + err.Error()}
	}
	out, err := e.gitRead(ctx, project.Path, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		// A detached HEAD is different from an unreadable repository. Quiet
		// symbolic-ref exits 1 only for the former; other failures stay hidden.
		_, probe := e.gitRead(ctx, project.Path, "symbolic-ref", "--quiet", "--short", "HEAD")
		var exit *clirun.ExitError
		if errors.As(probe, &exit) && exit.Result.ExitCode == 1 {
			return LandCheck{Reason: "the main checkout is not on a branch: " + errText(err)}
		}
		return fail("git symbolic-ref", err)
	}
	result.Base = strings.TrimSpace(out)
	if result.Base == "" {
		return LandCheck{Reason: "the main checkout is not on a branch: "}
	}
	if result.Base == rec.Branch {
		result.Reason = "the main checkout is on the slice branch " + rec.Branch
		return result
	}
	mergeBase, err := e.gitRead(ctx, rec.Worktree, "merge-base", result.Base, "HEAD")
	if err != nil {
		return fail("git merge-base", err)
	}
	changed, err := e.gitRead(ctx, rec.Worktree, "diff", "--name-only", "-z", "--no-renames", "--no-relative", strings.TrimSpace(mergeBase))
	if err != nil {
		return fail("git diff", err)
	}
	untracked, err := e.gitRead(ctx, rec.Worktree, "ls-files", "--others", "--exclude-standard", "-z", "--full-name")
	if err != nil {
		return fail("git ls-files", err)
	}
	paths := map[string]bool{}
	for _, p := range strings.Split(changed+untracked, "\x00") {
		if p != "" {
			paths[p] = true
		}
	}
	dirty, err := e.changedPaths(ctx, project.Path)
	if err != nil {
		return fail("git status", err)
	}
	prefix, err := e.gitRead(ctx, project.Path, "rev-parse", "--show-prefix")
	if err != nil {
		return fail("git rev-parse", err)
	}
	_, other := splitTracking(strings.TrimSuffix(prefix, "\n"), dirty)
	for _, p := range other {
		if paths[p] {
			result.Blockers = append(result.Blockers, p)
			delete(paths, p)
		}
	}
	sort.Strings(result.Blockers)
	return result
}
