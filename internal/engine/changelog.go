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

// resolveChangelog merges complete entries only in the root changelog.
// Stage 2 is the slice branch, so its additions precede the base branch's.
func (e *Engine) resolveChangelog(ctx context.Context, rec runrec.Record, base string, unmerged []string) (bool, error) {
	if len(unmerged) != 1 || unmerged[0] != "CHANGELOG.md" {
		return false, nil
	}
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
	if _, err := e.gitWrite(ctx, rec.Worktree, "commit", "-m", "Merge "+base+" into "+rec.Branch); err != nil {
		return false, fmt.Errorf("git commit: %w", err)
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

type changelogParts struct {
	prefix, suffix string
	entries        []string
}

// splitChangelog accepts only entries and indented continuation lines in the
// Unreleased body. It keeps the surrounding text byte for byte.
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
	parts := changelogParts{prefix: strings.Join(lines[:heading+2], "\n") + "\n"}
	if end < len(lines) {
		parts.suffix = strings.Join(lines[end:], "\n")
	}
	var entry []string
	flush := func() {
		for len(entry) > 0 && entry[len(entry)-1] == "" {
			entry = entry[:len(entry)-1]
		}
		if len(entry) > 0 {
			parts.entries = append(parts.entries, strings.Join(entry, "\n"))
		}
		entry = nil
	}
	for _, line := range lines[heading+2 : end] {
		if strings.HasPrefix(line, "- ") {
			if len(entry) > 0 && entry[len(entry)-1] != "" {
				return changelogParts{}, false
			}
			flush()
			entry = append(entry, line)
		} else if line == "" {
			if len(entry) > 0 {
				entry = append(entry, line)
			}
		} else if len(entry) > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			entry = append(entry, line)
		} else {
			return changelogParts{}, false
		}
	}
	flush()
	return parts, true
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
	body := ""
	if len(entries) > 0 {
		body = strings.Join(entries, "\n\n") + "\n\n"
	}
	return b.prefix + body + b.suffix, true
}
