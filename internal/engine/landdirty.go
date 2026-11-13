// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// The check on the main checkout before a landing merges (*proposed*, S254).
// git refuses a fast-forward only when an uncommitted path in the main
// checkout is one the fast-forward writes, so that is the set the landing
// looks at: the paths that are dirty there and that the slice branch changes.
// dirtyOverlap computes it for committed branches. S217's approval-time check
// reuses the status parser and tracking rules, including uncommitted slice work.

// dirtyOverlap returns the sorted, unique paths that are dirty in the checkout
// at dir (tracked changes, staged changes, untracked files, both sides of a
// rename) and that the branch changes since it left base (`git diff
// base...branch`, with --no-relative so the owner's diff.relative setting
// cannot change what it reports). Paths are as git reports them: relative to the repository
// root, which is not dir when the project is a subdirectory of its repository.
// An error carries the failing git command as its prefix ("git status: ",
// "git diff: "). A clean checkout costs one git call.
func (e *Engine) dirtyOverlap(ctx context.Context, dir, base, branch string) ([]string, error) {
	dirty, err := e.changedPaths(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	if len(dirty) == 0 {
		return nil, nil
	}
	out, err := e.gitRead(ctx, dir, "diff", "--name-only", "-z", "--no-renames", "--no-relative", base+"..."+branch)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	changed := map[string]bool{}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			changed[p] = true
		}
	}
	var overlap []string
	seen := map[string]bool{}
	for _, p := range dirty {
		if changed[p] && !seen[p] {
			seen[p] = true
			overlap = append(overlap, p)
		}
	}
	sort.Strings(overlap)
	return overlap, nil
}

// nameList is up to maxConflictNames names, then "+N more". Names are the
// owner's bytes: a reason that contains them goes through runrec.PlainText
// (control characters and terminal escape sequences dropped) before it is
// drawn.
func nameList(files []string) string {
	names := files
	more := ""
	if len(names) > maxConflictNames {
		more = fmt.Sprintf(" +%d more", len(names)-maxConflictNames)
		names = names[:maxConflictNames]
	}
	return strings.Join(names, ", ") + more
}

// dirtyReason is the one line a landing pauses with when the main checkout
// holds uncommitted changes to files the slice changes.
func dirtyReason(dir string, files []string) string {
	return oneLine(runrec.PlainText("landing stopped: uncommitted changes in " + dir + " to files the slice changes: " + nameList(files) + "; commit or stash them"))
}

// trackingPath reports whether rel (relative to the project) is one of the files slicer writes for items, notes, the log, the
// index and the render: the project's .slicer folder without its two
// configuration files. A matrix or config edit can be a deliberate uncommitted
// owner edit, so those two are never ork's to commit (owner decision
// 2026-11-03, Q2).
func trackingPath(rel string) bool {
	return strings.HasPrefix(rel, ".slicer/") && rel != ".slicer/config.json" && rel != ".slicer/relay-roles.json"
}

// splitTracking sorts paths as git reports them (relative to the repository
// root) into the project's tracking files, returned relative to the project
// directory, and everything else, returned as given. prefix is the project's
// folder in the repository (`git rev-parse --show-prefix`: empty at the root,
// otherwise ending in a slash). A sibling project's .slicer is in "other".
func splitTracking(prefix string, files []string) (tracking, other []string) {
	for _, f := range files {
		if rel, ok := strings.CutPrefix(f, prefix); ok && trackingPath(rel) {
			tracking = append(tracking, rel)
		} else {
			other = append(other, f)
		}
	}
	return tracking, other
}

// commitLaunchTracking carries this item's tracking edits into a new worktree.
// When committing the index, include listed items' untracked slice files so
// the committed index does not refer to files left only in the main checkout.
func (e *Engine) commitLaunchTracking(ctx context.Context, item string) string {
	P := e.proj.Path
	files, err := e.changedPaths(ctx, P)
	if err != nil {
		return oneLine("git status: " + err.Error())
	}
	if len(files) == 0 {
		return ""
	}
	out, err := e.gitRead(ctx, P, "rev-parse", "--show-prefix")
	if err != nil {
		return oneLine("git rev-parse: " + err.Error())
	}
	tracking, _ := splitTracking(strings.TrimSpace(out), files)
	include := map[string]bool{}
	for _, rel := range tracking {
		if rel == ".slicer/index.json" {
			items, err := e.d.Slicer.List(ctx, P)
			if err != nil {
				return oneLine("slicer list: " + err.Error())
			}
			untracked, err := e.gitRead(ctx, P, "ls-files", "--others", "--exclude-standard", "-z")
			if err != nil {
				return oneLine("git ls-files: " + err.Error())
			}
			listed := map[string]bool{}
			for _, it := range items {
				if it.HasSlice {
					listed[".slicer/slices/"+it.ID+".json"] = true
					listed[".slicer/render/slices/"+it.ID+".md"] = true
				}
			}
			for _, path := range strings.Split(untracked, "\x00") {
				if listed[path] {
					include[path] = true
				}
			}
			break
		}
	}
	var own []string
	for _, rel := range tracking {
		if include[rel] || rel == ".slicer/index.json" || rel == ".slicer/slices/"+item+".json" || rel == ".slicer/render/slices/"+item+".md" {
			own = append(own, rel)
		}
	}
	if len(own) == 0 {
		return ""
	}
	return e.commitTracking(ctx, P, own, "Tracking edits on main before starting "+item)
}

// checkMain looks at the main checkout P before the landing merges the base
// branch in, and again before the fast-forward (S254). A non-empty reason
// pauses the landing. Dirty files that the slice does not change are left
// alone. When only the project's tracking files overlap and commit is true,
// ork commits them on the base branch by name, so the merge-in takes them into
// the slice branch with slicer's merge drivers, and returns committed true:
// the base moved. Any other overlap stops the landing and ork commits nothing.
func (e *Engine) checkMain(ctx context.Context, P, base, B, item string, commit bool) (committed bool, reason string) {
	files, err := e.dirtyOverlap(ctx, P, base, B)
	if err != nil {
		return false, oneLine(err.Error())
	}
	if len(files) == 0 {
		return false, ""
	}
	out, err := e.gitRead(ctx, P, "rev-parse", "--show-prefix")
	if err != nil {
		return false, oneLine("git rev-parse: " + err.Error())
	}
	tracking, other := splitTracking(strings.TrimSpace(out), files)
	if len(other) > 0 {
		return false, dirtyReason(P, other)
	}
	if !commit {
		return false, dirtyReason(P, files)
	}
	reason = e.commitTracking(ctx, P, tracking, "Tracking edits on main before landing "+item)
	return reason == "", reason
}

// commitTracking commits literal paths, preserving unrelated staged changes.
func (e *Engine) commitTracking(ctx context.Context, P string, tracking []string, msg string) string {
	// By name, never a pathspec of ".", and literal, so a name with a glob
	// character cannot take another file along: the owner's staged files stay
	// staged. git add cannot name a path that is in neither the index nor the
	// tree (a deletion the owner staged), so only paths that exist are added;
	// the commit still records the deletions.
	var specs, present []string
	for _, rel := range tracking {
		specs = append(specs, ":(literal)"+rel)
		if _, err := os.Lstat(filepath.Join(P, rel)); err == nil {
			present = append(present, ":(literal)"+rel)
		}
	}
	var staged []string
	if len(present) > 0 {
		if _, err := e.gitWrite(ctx, P, append([]string{"add", "-A", "--"}, present...)...); err != nil {
			return oneLine("git add: " + err.Error())
		}
		for _, rel := range tracking {
			if _, err := os.Lstat(filepath.Join(P, rel)); err == nil {
				staged = append(staged, rel)
			}
		}
	}
	if _, err := e.gitWrite(ctx, P, append([]string{"commit", "-m", msg, "--"}, specs...)...); err != nil {
		note := ""
		if len(staged) > 0 {
			note = "; ork staged " + nameList(staged)
		}
		return oneLine(runrec.PlainText("git commit: " + err.Error() + note))
	}
	return ""
}
