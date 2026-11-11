// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// resolveChangelog merges complete entries in the unmerged root changelog and
// stages the result. It does not commit: settleMerge commits the merge once
// no unmerged file is left. The caller names only the root CHANGELOG.md.
// Stage 2 is the slice branch, so its additions precede the base branch's.
func (e *Engine) resolveChangelog(ctx context.Context, rec runrec.Record) (bool, error) {
	var stages [3]string
	for i := range stages {
		text, err := e.gitRead(ctx, rec.Worktree, "show", fmt.Sprintf(":%d:CHANGELOG.md", i+1))
		if err != nil {
			return false, nil
		}
		stages[i] = text
	}
	merged, ok := mergeChangelogEntries(stages[0], stages[1], stages[2])
	if !ok {
		return false, nil
	}
	if err := e.writeChangelog(ctx, rec, merged, os.WriteFile); err != nil {
		return false, err
	}
	return true, nil
}

// writeChangelog stages the result, restoring conflict markers if a file
// operation or staging fails. writeFile permits testing partial write failures.
func (e *Engine) writeChangelog(ctx context.Context, rec runrec.Record, merged string, writeFile func(string, []byte, os.FileMode) error) (err error) {
	defer func() {
		if err != nil {
			_, _ = e.gitWrite(ctx, rec.Worktree, "checkout", "--merge", "--", "CHANGELOG.md")
		}
	}()
	path := filepath.Join(rec.Worktree, "CHANGELOG.md")
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat CHANGELOG.md: %w", err)
	}
	if err := writeFile(path, []byte(merged), info.Mode().Perm()); err != nil {
		return fmt.Errorf("write CHANGELOG.md: %w", err)
	}
	if _, err := e.gitWrite(ctx, rec.Worktree, "add", "--", "CHANGELOG.md"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	return nil
}

// changelogParts is an Unreleased body cut into complete entries. The text is
// prefix, then the entries with seps[i] between entries[i] and entries[i+1],
// then tail (what ends the body), then suffix. A separator is the entry's line
// break plus any blank lines after it, so "\n" is a compact boundary and
// "\n\n" a blank-line one.
type changelogParts struct {
	prefix, tail, suffix string
	entries, seps        []string
}

// splitChangelog accepts only entries and indented continuation lines in the
// Unreleased body. A `- ` line always starts a new entry, whatever precedes
// it. It keeps the surrounding text and every separator byte for byte.
func splitChangelog(text string) (changelogParts, bool) {
	lines := strings.Split(text, "\n")
	heading := -1
	for i, line := range lines {
		if line == "## Unreleased" {
			heading = i
			break
		}
	}
	if heading < 0 || heading+2 >= len(lines) || lines[heading+1] != "" {
		return changelogParts{}, false
	}
	end := len(lines)
	for i := heading + 2; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	// Each entry covers lines [start, stop): stop follows its last non-blank line.
	type span struct{ start, stop int }
	var spans []span
	for i := heading + 2; i < end; i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "- "):
			spans = append(spans, span{i, i + 1})
		case line == "":
		case len(spans) > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
			spans[len(spans)-1].stop = i + 1
		default:
			return changelogParts{}, false
		}
	}
	var parts changelogParts
	if end < len(lines) {
		parts.suffix = strings.Join(lines[end:], "\n")
	}
	if len(spans) == 0 {
		parts.prefix = strings.Join(lines[:heading+2], "\n") + "\n"
		parts.tail = strings.Join(lines[heading+2:end], "\n")
		if end < len(lines) && end > heading+2 {
			parts.tail += "\n"
		}
		return parts, true
	}
	// Blank lines before the first entry stay in the prefix.
	parts.prefix = strings.Join(lines[:spans[0].start], "\n") + "\n"
	for i, s := range spans {
		parts.entries = append(parts.entries, strings.Join(lines[s.start:s.stop], "\n"))
		if i+1 < len(spans) {
			parts.seps = append(parts.seps, strings.Repeat("\n", spans[i+1].start-s.stop+1))
		}
	}
	// The last line of the file has no line break of its own.
	breaks := end - spans[len(spans)-1].stop
	if end < len(lines) {
		breaks++
	}
	parts.tail = strings.Repeat("\n", breaks)
	return parts, true
}

// firstSeparator is the first entry boundary of the first body that has one.
// A blank line is the default for bodies with no boundary at all.
func firstSeparator(bodies ...changelogParts) string {
	for _, b := range bodies {
		if len(b.seps) > 0 {
			return b.seps[0]
		}
	}
	return "\n\n"
}

// changelogAdditions requires the original entries unchanged and in order.
func changelogAdditions(base, side []string) ([]string, bool) {
	originals := make(map[string]bool, len(base))
	for _, entry := range base {
		originals[entry] = true
	}
	next := 0
	var added []string
	for _, entry := range side {
		if originals[entry] {
			if next >= len(base) || entry != base[next] {
				return nil, false
			}
			next++
		} else {
			added = append(added, entry)
		}
	}
	return added, next == len(base)
}

// mergeChangelogEntries only merges additions within Unreleased. It rejects
// ambiguous bodies and verifies each complete entry survives exactly once.
func mergeChangelogEntries(base, ours, theirs string) (string, bool) {
	b, ok := splitChangelog(base)
	if !ok {
		return "", false
	}
	o, ok := splitChangelog(ours)
	if !ok {
		return "", false
	}
	t, ok := splitChangelog(theirs)
	if !ok {
		return "", false
	}
	if o.prefix != b.prefix || t.prefix != b.prefix || o.suffix != b.suffix || t.suffix != b.suffix {
		return "", false
	}
	oa, ok := changelogAdditions(b.entries, o.entries)
	if !ok {
		return "", false
	}
	ta, ok := changelogAdditions(b.entries, t.entries)
	if !ok {
		return "", false
	}
	entries := append([]string(nil), oa...)
	oursAdded := make(map[string]bool, len(oa))
	for _, entry := range oa {
		oursAdded[entry] = true
	}
	for _, entry := range ta {
		if !oursAdded[entry] {
			entries = append(entries, entry)
		}
	}
	entries = append(entries, b.entries...)
	counts := make(map[string]int, len(entries))
	for _, entry := range entries {
		counts[entry]++
	}
	for _, source := range [][]string{b.entries, oa, ta} {
		for _, entry := range source {
			if counts[entry] != 1 {
				return "", false
			}
		}
	}
	// The base's entries keep their own separators. New entries go before
	// them, joined by the first separator found (the base's, which is the
	// top of Unreleased), and so does the boundary to the first old entry.
	tail, ok := changelogTail(b, o, t)
	if !ok {
		return "", false
	}
	sep := firstSeparator(b, o, t)
	added := entries[:len(entries)-len(b.entries)]
	body := strings.Join(added, sep)
	for i, entry := range b.entries {
		if i == 0 && len(added) > 0 {
			body += sep
		}
		body += entry
		if i < len(b.seps) {
			body += b.seps[i]
		}
	}
	return b.prefix + body + tail + b.suffix, true
}

// changelogTail returns how the merged body ends. A base with entries sets
// it and both sides must agree. A base without entries leaves it to the sides
// that have entries, which must agree with each other.
func changelogTail(base, ours, theirs changelogParts) (string, bool) {
	tail := base.tail
	if len(base.entries) > 0 {
		return tail, ours.tail == tail && theirs.tail == tail
	}
	set := false
	for _, side := range []changelogParts{ours, theirs} {
		switch {
		case len(side.entries) == 0:
			if side.tail != base.tail {
				return "", false
			}
		case !set:
			tail, set = side.tail, true
		case side.tail != tail:
			return "", false
		}
	}
	return tail, true
}
