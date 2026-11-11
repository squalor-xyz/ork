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
//
// The one exception is the item's own slice JSON (S334, adj 303 and 304).
// When the branch moved it to done/ (`slicer done`) and the base edited the
// old path, right after `git merge` stops ork merges the base's edit into the
// done/ copy; a clean merge stages the copy and removes the old path, a
// conflict leaves markers in the copy and the pause names it. On a retry an
// old path that is absent, with the done/ copy in place and free of markers,
// is staged as deleted.
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
	// The item's own slice JSON, moved to done/ by the branch while the base
	// edited the old path (S334, adj 303): ork merges the base's change into the
	// done/ copy. A clean merge needs no owner; a conflict leaves markers there.
	prefix, perr := projectPrefix(ctx, e.proj.Path)
	oldJSON := prefix + ".slicer/slices/" + rec.Item + ".json"
	doneJSON := prefix + ".slicer/slices/done/" + rec.Item + ".json"
	if perr == nil && !afterOwner && len(files) == 1 && files[0] == oldJSON && e.deletedByUs(ctx, W, oldJSON) {
		if _, herr := e.gitRead(ctx, W, "cat-file", "-e", "HEAD:"+doneJSON); herr == nil {
			clean, merr := e.mergeMovedSliceJSON(ctx, rec, base, oldJSON, doneJSON)
			switch {
			case clean:
				files, err = e.unmergedFiles(ctx, W)
				if err != nil {
					return pauseWith("git diff: " + err.Error())
				}
			case merr != nil:
				note = "; ork could not merge " + doneJSON + ": " + merr.Error()
			default:
				// The old path stays unmerged and absent: a retry stages its deletion.
				if rerr := os.Remove(filepath.Join(W, oldJSON)); rerr != nil && !os.IsNotExist(rerr) {
					return pauseWith("remove " + oldJSON + ": " + rerr.Error())
				}
			}
		}
	}
	if afterOwner {
		for _, f := range files {
			path := filepath.Join(W, f)
			if _, serr := os.Lstat(path); os.IsNotExist(serr) && perr == nil && f == oldJSON {
				// An absent old slice JSON that git marks deleted on one side (DU or
				// UD), with the item's done/ copy in place, is a deletion, not a
				// conflict (S334, adj 304), once the copy has no markers.
				done := filepath.Join(W, doneJSON)
				if _, derr := os.Lstat(done); derr == nil && !hasConflictMarkers(done) && e.deletedBySide(ctx, W, f) {
					if _, rerr := e.gitWrite(ctx, W, "rm", "-q", "--cached", "--", f); rerr != nil {
						return pauseWith("git rm " + f + ": " + rerr.Error())
					}
				}
				continue
			}
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
		reason := conflictReason(base, rec.Branch, W, rec.ID, files)
		if perr == nil {
			trackingOnly := true
			for _, f := range files {
				if !strings.HasPrefix(f, prefix+".slicer/slices/") {
					trackingOnly = false
					break
				}
			}
			if trackingOnly {
				reason = trackingConflictReason(base, rec.Branch, W, rec.ID, files)
				if _, derr := os.Lstat(filepath.Join(W, doneJSON)); derr == nil && len(files) == 1 && files[0] == oldJSON && e.deletedByUs(ctx, W, oldJSON) {
					reason = movedConflictReason(base, rec.Branch, W, rec.ID, oldJSON, doneJSON)
				}
			}
		}
		for _, f := range files {
			if strings.HasPrefix(f, ".slicer/") {
				if missing, err := MissingMergeDrivers(ctx, W, e.d.gitBin()); err == nil && len(missing) > 0 {
					note += "; slicer merge drivers are not configured in this clone; run slicer setup-git and the lines it prints, then git -C " + ShellWord(W) + " merge --abort and ork retry " + rec.ID
				}
				break
			}
		}
		return pauseWith(reason + note)
	}
	if _, cerr := e.gitWrite(ctx, W, "commit", "-m", "Merge "+base+" into "+rec.Branch); cerr != nil {
		return pauseWith("git commit: " + cerr.Error())
	}
	return true, nil
}

// trackingConflictReason leaves both sides for the owner to merge. Git's
// stage 2 is the slice branch and stage 3 is the base being merged into it.
func trackingConflictReason(base, branch, worktree, runID string, files []string) string {
	names, more := conflictNames(files)
	var commands []string
	for _, f := range names {
		commands = append(commands, "inspect branch with git -C "+ShellWord(worktree)+" show "+ShellWord(":2:"+f),
			"inspect base with git -C "+ShellWord(worktree)+" show "+ShellWord(":3:"+f))
	}
	return fmt.Sprintf("merging %s into %s stopped on conflicts in %s%s; slicer tracking files: both sides changed them; %s; keep both changes and remove the markers in %s (no need to git add), then ork retry %s; ork commits the merge",
		base, branch, strings.Join(names, ", "), more, strings.Join(commands, "; "), worktree, runID)
}

// deletedByUs reports whether git marks path unmerged as deleted by the slice
// branch: the merge base (stage 1) and the base branch (stage 3) have it and
// the branch (stage 2) does not.
func (e *Engine) deletedByUs(ctx context.Context, dir, path string) bool {
	stages := e.unmergedStages(ctx, dir, path)
	return stages["1"] && !stages["2"] && stages["3"]
}

// deletedBySide reports whether git marks path unmerged as deleted on one
// side only: deleted by us (stages 1 and 3) or by them (stages 1 and 2). A
// path both sides modified (stages 1, 2 and 3) is not.
func (e *Engine) deletedBySide(ctx context.Context, dir, path string) bool {
	stages := e.unmergedStages(ctx, dir, path)
	return stages["1"] && stages["2"] != stages["3"]
}

// unmergedStages lists the index stages ("1", "2", "3") git holds for path.
func (e *Engine) unmergedStages(ctx context.Context, dir, path string) map[string]bool {
	stages := map[string]bool{}
	out, err := e.gitRead(ctx, dir, "ls-files", "-u", "-z", "--", path)
	if err != nil {
		return stages
	}
	for _, entry := range strings.Split(out, "\x00") {
		meta, _, _ := strings.Cut(entry, "\t")
		if fields := strings.Fields(meta); len(fields) == 3 {
			stages[fields[2]] = true
		}
	}
	return stages
}

// mergeMovedSliceJSON three-way merges the base branch's edit of the old slice
// JSON into the branch's done/ copy (`git merge-file`, with the merge base as
// the ancestor). clean is true when it merged without markers: the copy is
// staged and the old path removed. With conflicts, clean is false and err nil:
// the markers are in the done/ copy. err is set when the merge could not run.
func (e *Engine) mergeMovedSliceJSON(ctx context.Context, rec runrec.Record, base, oldJSON, doneJSON string) (clean bool, err error) {
	W := rec.Worktree
	ancestor, err := e.gitRead(ctx, W, "show", ":1:"+oldJSON)
	if err != nil {
		return false, err
	}
	theirs, err := e.gitRead(ctx, W, "show", ":3:"+oldJSON)
	if err != nil {
		return false, err
	}
	dir, err := os.MkdirTemp(e.run.Dir(), "merge-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)
	ancestorFile, theirsFile := filepath.Join(dir, "ancestor"), filepath.Join(dir, "theirs")
	if err := os.WriteFile(ancestorFile, []byte(ancestor), 0o600); err != nil {
		return false, err
	}
	if err := os.WriteFile(theirsFile, []byte(theirs), 0o600); err != nil {
		return false, err
	}
	donePath := filepath.Join(W, doneJSON)
	if _, merr := e.gitWrite(ctx, W, "merge-file", "-L", rec.Branch, "-L", "merge base", "-L", base, donePath, ancestorFile, theirsFile); merr != nil {
		if hasConflictMarkers(donePath) {
			return false, nil
		}
		return false, merr
	}
	if _, err := e.gitWrite(ctx, W, "add", "--", doneJSON); err != nil {
		return false, err
	}
	if _, err := e.gitWrite(ctx, W, "rm", "-q", "-f", "--", oldJSON); err != nil {
		return false, err
	}
	return true, nil
}

// movedConflictReason is trackingConflictReason for the branch's done/ copy of
// the item's slice JSON: the branch's text is in HEAD, the base's edit of the
// old path is git's stage 3.
func movedConflictReason(base, branch, worktree, runID, oldJSON, doneJSON string) string {
	return fmt.Sprintf("merging %s into %s stopped on conflicts in %s; slicer tracking files: both sides changed them; inspect branch with git -C %s show %s; inspect base with git -C %s show %s; keep both changes and remove the markers in %s (no need to git add), then ork retry %s; ork commits the merge",
		base, branch, doneJSON, ShellWord(worktree), ShellWord("HEAD:"+doneJSON), ShellWord(worktree), ShellWord(":3:"+oldJSON), worktree, runID)
}

// conflictReason is the one line a paused landing shows: the files (at most
// five, then "+N more"), the worktree and how to go on.
func conflictReason(base, branch, worktree, runID string, files []string) string {
	names, more := conflictNames(files)
	return fmt.Sprintf("merging %s into %s stopped on conflicts in %s%s; resolve them in %s (no need to git add), then ork retry %s; ork commits the merge",
		base, branch, strings.Join(names, ", "), more, worktree, runID)
}

func conflictNames(files []string) ([]string, string) {
	names := files
	more := ""
	if len(names) > maxConflictNames {
		more = fmt.Sprintf(" +%d more", len(names)-maxConflictNames)
		names = names[:maxConflictNames]
	}
	return names, more
}

// oneLine folds a message onto one line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
