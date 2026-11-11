// SPDX-License-Identifier: GPL-3.0-or-later

package release

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func changelogLines(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(data), "\n")
}

// unreleasedIndex returns the line index of the `## Unreleased` heading.
func unreleasedIndex(t *testing.T, lines []string) int {
	t.Helper()
	for i, l := range lines {
		if l == "## Unreleased" {
			return i
		}
	}
	t.Fatal("CHANGELOG.md has no `## Unreleased` heading")
	return -1
}

// The v1.1.0 release section stays in the changelog. Entries for the next
// release may stand under `## Unreleased` above it.
func TestChangelogHasTheV110Section(t *testing.T) {
	want := regexp.MustCompile(`^## v1\.1\.0 \(\d{4}-\d{2}-\d{2}\)$`)
	lines := changelogLines(t)
	i := unreleasedIndex(t, lines)
	for _, l := range lines[i+1:] {
		if want.MatchString(l) {
			return
		}
	}
	t.Fatal("no `## v1.1.0 (YYYY-MM-DD)` heading under `## Unreleased`")
}

// The Publish release step reads the notes with awk on the second word of each
// line that starts with `## `, so every such heading must be Unreleased or
// `vN.N.N (YYYY-MM-DD)` (the regexp makes the second word the tag), and a tag
// must head one section only.
func TestChangelogHeadingsMatchWhatTheWorkflowReads(t *testing.T) {
	heading := regexp.MustCompile(`^## (v\d+\.\d+\.\d+) \(\d{4}-\d{2}-\d{2}\)$`)
	seen := map[string]bool{}
	for _, l := range changelogLines(t) {
		if !strings.HasPrefix(l, "## ") {
			continue
		}
		if l == "## Unreleased" {
			continue
		}
		m := heading.FindStringSubmatch(l)
		if m == nil {
			t.Errorf("heading %q is neither `## Unreleased` nor `## vN.N.N (YYYY-MM-DD)`", l)
			continue
		}
		if seen[m[1]] {
			t.Errorf("tag %s heads more than one section", m[1])
		}
		seen[m[1]] = true
	}
}
