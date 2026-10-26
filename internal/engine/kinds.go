// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"regexp"
	"strings"
)

// The finding kinds a reviewer, UI pass or gate report may tag (adj 163,
// review.finding-kinds).
const (
	KindFix   = "fix"   // goes to the fix round
	KindOwner = "owner" // a design call: a structured owner question
	KindLater = "later" // later work, offered to the owner as a new slice
)

// Finding is one tagged finding of a report.
type Finding struct {
	Kind    string
	Blocker bool // an owner finding that blocks the gate
	Title   string
	Text    string         // the title and the lines under it, without the tag
	Ask     *OwnerQuestion // owner findings only
	Key     string         // the owner finding's optional adj decision key
}

// Triaged is a report's findings sorted by kind.
type Triaged struct {
	Fix    string // the fix findings, empty when there are none
	Owner  []Finding
	Later  []Finding
	Tagged bool // the report carries at least one kind tag
}

// Blocker returns the first owner finding marked as a blocker.
func (t *Triaged) Blocker() *Finding {
	for i := range t.Owner {
		if t.Owner[i].Blocker {
			return &t.Owner[i]
		}
	}
	return nil
}

// kindTag matches the line that opens a tagged finding: an optional list
// marker or bold, then [fix], [owner], [owner blocker] or [later].
var kindTag = regexp.MustCompile(`(?i)^(\s*)([-*+]\s+|\d+[.)]\s+)?(?:\*\*|__)?\[(fix|later|owner(?:[ ,]+blocker)?)\](?:\*\*|__)?[:\s]*(.*)$`)

// segment is a run of report lines: one tagged finding, or untagged text
// when kind is empty.
type segment struct {
	kind    string // "", KindFix, KindOwner or KindLater
	blocker bool
	title   string
	lines   []string
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// segments splits lines into tagged findings and the untagged text between
// them. A finding runs from its tag line to the next tag line, to the first
// line that is not indented under a bulleted tag line, or to the first line
// that is not indented after a blank line when the tag line has no bullet.
func segments(lines []string) []segment {
	var out []segment
	var cur *segment
	var blanks []string
	tagIndent, bulleted := 0, false
	add := func(line string) {
		if cur == nil || cur.kind != "" {
			out = append(out, segment{})
			cur = &out[len(out)-1]
		}
		cur.lines = append(cur.lines, line)
	}
	for _, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		if m := kindTag.FindStringSubmatch(line); m != nil {
			for _, b := range blanks {
				add(b)
			}
			blanks = nil
			kind := strings.ToLower(m[3])
			g := segment{kind: KindFix, title: strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[4]), "**")), lines: []string{line}}
			if strings.HasPrefix(kind, KindOwner) {
				g.kind = KindOwner
				g.blocker = strings.Contains(kind, "blocker")
			} else if kind == KindLater {
				g.kind = KindLater
			}
			out = append(out, g)
			cur = &out[len(out)-1]
			tagIndent, bulleted = len(m[1]), m[2] != ""
			continue
		}
		if strings.TrimSpace(line) == "" {
			blanks = append(blanks, line)
			continue
		}
		if cur != nil && cur.kind != "" {
			ends := indentOf(line) <= tagIndent && (bulleted || len(blanks) > 0)
			if strings.HasPrefix(line, "#") {
				ends = true
			}
			if !ends {
				cur.lines = append(append(cur.lines, blanks...), line)
				blanks = nil
				continue
			}
		}
		for _, b := range blanks {
			add(b)
		}
		blanks = nil
		add(line)
	}
	for _, b := range blanks {
		add(b)
	}
	return out
}

// finding turns a tagged owner or later segment into a Finding. The problem
// is non-empty when the finding is malformed.
func (g segment) finding() (Finding, string) {
	f := Finding{Kind: g.kind, Blocker: g.blocker, Title: g.title}
	if f.Title == "" {
		return f, fmt.Sprintf("a [%s] finding needs a one-line title after the tag", g.kind)
	}
	rest := dedent(g.lines[1:])
	f.Text = strings.TrimSpace(f.Title + "\n" + strings.Join(rest, "\n"))
	if g.kind != KindOwner {
		return f, ""
	}
	var body []string
	for _, line := range rest {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Decision key:") {
			key := strings.TrimSpace(strings.TrimPrefix(line, "Decision key:"))
			if f.Key != "" || !decisionKey.MatchString(key) {
				return f, fmt.Sprintf("owner finding %q must have at most one Decision key: area.topic line with a lowercase dotted key", f.Title)
			}
			f.Key = key
			continue
		}
		if line != "" {
			body = append(body, line)
		}
	}
	// The Question: line makes the block structured, so a shape that is not
	// right is always an error.
	ask, err := parseOwnerQuestion("Question: " + f.Title + "\n" + strings.Join(body, "\n"))
	if err != nil {
		return f, fmt.Sprintf("owner finding %q needs Options: with 2 to 4 numbered unique labels and summaries, and Recommend: N - reason, directly under its tag line", f.Title)
	}
	f.Ask = ask
	return f, ""
}

// dedent removes the indentation that all non-blank lines share.
func dedent(lines []string) []string {
	least := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if n := indentOf(l); least < 0 || n < least {
			least = n
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if least > 0 && len(l) >= least {
			l = l[least:]
		}
		out[i] = strings.TrimRight(l, " \t")
	}
	return out
}
