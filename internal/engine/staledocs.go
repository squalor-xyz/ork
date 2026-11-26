// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// staleDocsLimit caps the hits the Stale doc references section lists.
const staleDocsLimit = 20

// staleDocHit is one docs/ line that names a command a role prompt change
// removed.
type staleDocHit struct {
	Path  string // relative to the worktree, with forward slashes
	Line  int    // 1-based
	Names []string
}

var (
	backtickSpan = regexp.MustCompile("`([^`\n]+)`")
	commandWord  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// removedPromptLines returns the removed lines of a `git diff --unified=0`
// text, without their leading "-". A file's header runs from its `diff --git`
// line to the next `@@` line, so a removed line that starts with "-- " (shown
// as "--- ") is kept once a hunk has begun.
func removedPromptLines(diff string) []string {
	var removed []string
	inHeader := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inHeader = true
		case strings.HasPrefix(line, "@@"):
			inHeader = false
		case inHeader:
		case strings.HasPrefix(line, "-"):
			removed = append(removed, line[1:])
		}
	}
	return removed
}

// staleDocNames returns the command names in lines (S569, adj 599): the first
// two words of each single-line backticked span, when both are lowercase
// words. A span with a flag or an uppercase second word gives no name. The
// names come back sorted and without repeats.
func staleDocNames(lines []string) []string {
	var names []string
	for _, line := range lines {
		for _, m := range backtickSpan.FindAllStringSubmatch(line, -1) {
			words := strings.Fields(m[1])
			if len(words) < 2 || !commandWord.MatchString(words[0]) || !commandWord.MatchString(words[1]) {
				continue
			}
			names = append(names, words[0]+" "+words[1])
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// staleDocWordByte reports whether b continues a command word.
func staleDocWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-'
}

// staleDocLineNames lists the names that occur in line as whole words, in the
// order of names.
func staleDocLineNames(line string, names []string) []string {
	var found []string
	for _, name := range names {
		for from := 0; from <= len(line)-len(name); {
			i := strings.Index(line[from:], name)
			if i < 0 {
				break
			}
			i += from
			end := i + len(name)
			if (i == 0 || !staleDocWordByte(line[i-1])) && (end == len(line) || !staleDocWordByte(line[end])) {
				found = append(found, name)
				break
			}
			from = i + 1
		}
	}
	return found
}

// staleDocHits finds the lines under worktree/docs that name any of names. It
// returns the hits sorted by file, then line, and the paths it could not read.
// A missing docs directory gives neither.
func staleDocHits(worktree string, names []string) ([]staleDocHit, []string) {
	docs := filepath.Join(worktree, "docs")
	var hits []staleDocHit
	var unread []string
	_ = filepath.WalkDir(docs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == docs && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			unread = append(unread, ucsRel(worktree, p))
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel := ucsRel(worktree, p)
		src, err := os.ReadFile(p)
		if err != nil {
			unread = append(unread, rel)
			return nil
		}
		for i, line := range strings.Split(string(src), "\n") {
			if found := staleDocLineNames(line, names); len(found) > 0 {
				hits = append(hits, staleDocHit{Path: rel, Line: i + 1, Names: found})
			}
		}
		return nil
	})
	slices.SortFunc(hits, func(a, b staleDocHit) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return a.Line - b.Line
	})
	return hits, unread
}

// staleDocsSection tells the reviewer and the gate which docs/ lines still name
// a command that this branch removed from role prompt text (S569, adj 598,
// adj 599). Like the other branch checks, a failure is advisory: the section
// says the check is incomplete and never pauses the run. It returns nil when
// no removed line names a command, or when docs/ holds no hit and no unreadable
// file.
func (e *Engine) staleDocsSection(ctx context.Context, rec runrec.Record, change branchChangeResult) *BriefSection {
	const title = "Stale doc references"
	if change.Err != nil || change.MergeBase == "" {
		return &BriefSection{Title: title, Body: "Check incomplete: the branch diff against the base could not be read. Search `docs/` yourself for any command name the branch removed from role prompt text in `internal/roles`; this does not block the run."}
	}
	diff, err := e.gitRead(ctx, rec.Worktree, "diff", "--no-color", "--no-ext-diff", "--no-renames", "--unified=0", change.MergeBase, "--", "internal/roles", ":(exclude)internal/roles/*_test.go")
	if err != nil {
		return &BriefSection{Title: title, Body: "Check incomplete: the role prompt lines this branch removed could not be read. Search `docs/` yourself for any command name the branch removed from role prompt text in `internal/roles`; this does not block the run."}
	}
	names := staleDocNames(removedPromptLines(diff))
	if len(names) == 0 {
		return nil
	}
	hits, unread := staleDocHits(rec.Worktree, names)
	if len(hits) == 0 && len(unread) == 0 {
		return nil
	}

	var body strings.Builder
	if len(hits) > 0 {
		quoted := make([]string, len(names))
		for i, name := range names {
			quoted[i] = "`" + name + "`"
		}
		body.WriteString("Role prompt lines this branch removed named " + strings.Join(quoted, ", ") + ". `docs/` still names them at the places below. Check whether each still describes the prompt as it now reads. The check goes by text, so it is advisory and never blocks the run.\n\n")
		listed := hits
		if len(listed) > staleDocsLimit {
			listed = listed[:staleDocsLimit]
		}
		for _, hit := range listed {
			found := make([]string, len(hit.Names))
			for i, name := range hit.Names {
				found[i] = "`" + name + "`"
			}
			fmt.Fprintf(&body, "- `%s:%d`: %s\n", hit.Path, hit.Line, strings.Join(found, ", "))
		}
		if leftOut := len(hits) - len(listed); leftOut > 0 {
			noun := "hits were"
			if leftOut == 1 {
				noun = "hit was"
			}
			fmt.Fprintf(&body, "\n%d more %s left out (ork lists at most %d). Search `docs/` for the rest yourself.\n", leftOut, noun, staleDocsLimit)
		}
	}
	if len(unread) > 0 {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString("Check incomplete: ork could not read " + ucsQuote(unread) + ". Search them yourself for the command names removed from role prompt text; this does not block the run.\n")
	}
	return &BriefSection{Title: title, Body: strings.TrimRight(body.String(), "\n")}
}
