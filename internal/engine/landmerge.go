// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// The landing merge (S238, *proposed*): ork resolves what it can of a merge
// conflict (the CHANGELOG.md entries), stages the files the owner resolved,
// commits the merge itself and pauses on the rest. No role is asked to
// commit: roles cannot (DefaultImplementerAllowlist has no git add or git
// commit; adj 34, git.commit). Conflicts other than CHANGELOG.md are the
// owner's, as the relay contract says (`slicer ai relay`, Land).

// maxConflictNames is how many files a conflict pause names before "+N more".
const maxConflictNames = 5

// mergeInProgress reports whether a merge is open in the worktree at dir:
// MERGE_HEAD exists.
func (e *Engine) mergeInProgress(ctx context.Context, dir string) bool {
	_, err := e.gitRead(ctx, dir, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	return err == nil
}

// hasConflictMarkers reports whether the file has a line that starts with
// "<<<<<<< " and a line that starts with ">>>>>>> ". A lone "=======" is no
// marker; markdown uses it. A file that cannot be read has none.
func hasConflictMarkers(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var open, closed bool
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		open = open || strings.HasPrefix(line, "<<<<<<< ")
		closed = closed || strings.HasPrefix(line, ">>>>>>> ")
		if err != nil {
			break
		}
	}
	return open && closed
}

// unmergedFiles lists the paths git still marks unmerged in the worktree.
func (e *Engine) unmergedFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := e.gitRead(ctx, dir, "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// settleMerge finishes the merge open in the worktree, or pauses the run on
// what ork cannot resolve. It merges the CHANGELOG.md entries when that file
// still holds conflict markers, stages every other unmerged file that exists
// and holds no markers, and commits the merge when nothing is left unmerged.
// It never stages with -A or a pathspec of ".", so a stray file stays out of
// the merge commit. done is true when the merge is committed; otherwise the
// run is paused and err is the result to return.
//
// A marker-free file counts as resolved only when afterOwner is true: ork
// ran this after the owner worked on the tree (a retry). Right after
// `git merge` stops, such a file is a conflict without markers (a binary
// file, a modify/delete) that no one has looked at, so it stays unmerged.
func (e *Engine) settleMerge(ctx context.Context, rec runrec.Record, base string, afterOwner bool) (done bool, err error) {
	W := rec.Worktree
	pauseWith := func(reason string) (bool, error) {
		return false, e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateLand, Reason: oneLine(reason)})
	}
	files, err := e.unmergedFiles(ctx, W)
	if err != nil {
		return pauseWith("git diff: " + err.Error())
	}
	note := ""
	for _, f := range files {
		if f == "CHANGELOG.md" && hasConflictMarkers(filepath.Join(W, f)) {
			if _, rerr := e.resolveChangelog(ctx, rec); rerr != nil {
				note = "; ork could not merge CHANGELOG.md: " + rerr.Error()
			}
		}
	}
	files, err = e.unmergedFiles(ctx, W)
	if err != nil {
		return pauseWith("git diff: " + err.Error())
	}
	if afterOwner {
		for _, f := range files {
			path := filepath.Join(W, f)
			if _, serr := os.Lstat(path); serr != nil || hasConflictMarkers(path) {
				continue
			}
			if _, aerr := e.gitWrite(ctx, W, "add", "--", f); aerr != nil {
				return pauseWith("git add " + f + ": " + aerr.Error())
			}
		}
		files, err = e.unmergedFiles(ctx, W)
		if err != nil {
			return pauseWith("git diff: " + err.Error())
		}
	}
	if len(files) > 0 {
		for _, f := range files {
			if strings.HasPrefix(f, ".slicer/") {
				if missing, err := MissingMergeDrivers(ctx, W, e.d.gitBin()); err == nil && len(missing) > 0 {
					note += "; slicer merge drivers are not configured in this clone; run slicer setup-git and the lines it prints, then git -C " + ShellWord(W) + " merge --abort and ork retry " + rec.ID
				}
				break
			}
		}
		return pauseWith(conflictReason(base, rec.Branch, W, rec.ID, files) + note)
	}
	if _, cerr := e.gitWrite(ctx, W, "commit", "-m", "Merge "+base+" into "+rec.Branch); cerr != nil {
		return pauseWith("git commit: " + cerr.Error())
	}
	return true, nil
}

// conflictReason is the one line a paused landing shows: the files (at most
// five, then "+N more"), the worktree and how to go on.
func conflictReason(base, branch, worktree, runID string, files []string) string {
	names := files
	more := ""
	if len(names) > maxConflictNames {
		more = fmt.Sprintf(" +%d more", len(names)-maxConflictNames)
		names = names[:maxConflictNames]
	}
	return fmt.Sprintf("merging %s into %s stopped on conflicts in %s%s; resolve them in %s (no need to git add), then ork retry %s; ork commits the merge",
		base, branch, strings.Join(names, ", "), more, worktree, runID)
}

// oneLine folds a message onto one line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
