// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
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
	Scores  *runrec.FindingScores
	Kind    string
	Blocker bool // an owner finding that blocks the gate
	Title   string
	Text    string         // the title and the lines under it, without the tag
	Ask     *OwnerQuestion // owner findings only
	Key     string         // the owner finding's optional adj decision key
	Context string         // owner findings: the body lines that are not the block
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

// text is the finding's lines as the report wrote them, tag line included,
// with the shared indentation removed.
func (g segment) text() string {
	return strings.TrimSpace(strings.Join(dedent(g.lines), "\n"))
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
// is non-empty when the finding is malformed. With lenient set (the owner
// accepted the report, S298, *proposed*) an owner finding whose options are
// not right is kept without an Ask, and is not a problem.
func (g segment) finding(lenient bool) (Finding, string) {
	f := Finding{Kind: g.kind, Blocker: g.blocker, Title: g.title}
	if f.Title == "" {
		return f, fmt.Sprintf("a [%s] finding needs a one-line title after the tag", g.kind)
	}
	rest := dedent(g.lines[1:])
	f.Text = strings.TrimSpace(f.Title + "\n" + strings.Join(rest, "\n"))
	if g.kind == KindLater {
		scores := map[string]int{}
		for _, line := range rest {
			name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			if !ok || (name != "Importance" && name != "Urgency" && name != "Effort") {
				continue
			}
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				continue
			}
			if n < 1 || n > 3 || scores[name] != 0 {
				return f, fmt.Sprintf("later finding %q needs unique Importance, Urgency and Effort values from 1 to 3", f.Title)
			}
			scores[name] = n
		}
		if len(scores) != 0 {
			if len(scores) != 3 {
				return f, fmt.Sprintf("later finding %q needs all three proposed scores: Importance, Urgency and Effort", f.Title)
			}
			f.Scores = &runrec.FindingScores{Importance: scores["Importance"], Urgency: scores["Urgency"], Effort: scores["Effort"]}
		}
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
	// right is always an error. Lines that are not the block stay in f.Text
	// as context (S288, *proposed*).
	lines, context, why := ownerBlockLines(f.Title, body)
	var ask *OwnerQuestion
	if why == "" {
		var err error
		if ask, err = parseOwnerQuestion(strings.Join(lines, "\n")); err != nil {
			why = shapeCause(err)
		}
	}
	if why != "" {
		if lenient {
			// The owner accepted the report as it is (S298, *proposed*): the
			// finding stays a plain one, with all its lines in Text.
			return f, ""
		}
		return f, fmt.Sprintf("owner finding %q %s (an owner finding is Options: with 2 to 4 numbered unique labels and summaries, and Recommend: N - reason, %s)", f.Title, why, ownerShapeTail)
	}
	f.Ask = ask
	f.Context = strings.Join(context, "\n")
	return f, ""
}

// ownerBlockLines picks the structured lines out of an owner finding's body:
// Options:, the numbered options and Recommend:, each after one leading list
// marker is stripped. Every other line is context and is returned apart. It
// reports false, because the meaning is not clear, for: a line between two
// numbered options, a numbered line that is not an option, a repeated Options:
// or Recommend:, a Recommend: before Options:, a Decision key: line behind a
// list marker, and an option or numbered line after Recommend:. A missing
// Options: or Recommend: is left to parseOwnerQuestion.
//
// why is empty when the body is read, and otherwise names the rule that the
// refused line broke, quoting the line (S298, *proposed*).
func ownerBlockLines(title string, body []string) (lines, context []string, why string) {
	lines = []string{"Question: " + title}
	const (
		before = iota
		inOptions
		after
	)
	state, options, gap := before, 0, false
	for _, raw := range body {
		line := stripListMarker(raw)
		if strings.HasPrefix(line, "Decision key:") {
			// The unmarked key lines are taken out before; a marked one would be
			// dropped silently.
			return nil, nil, "a Decision key: line must not have a list marker; write it as Decision key: area.topic"
		}
		switch state {
		case before:
			if line == "Options:" {
				lines = append(lines, line)
				state = inOptions
			} else if strings.HasPrefix(line, "Recommend:") {
				return nil, nil, "has no Options: line before its " + quoteLine(line)
			} else {
				context = append(context, raw)
			}
		case inOptions:
			switch {
			case strings.HasPrefix(line, "Recommend:"):
				lines = append(lines, line)
				state = after
			case line == "Options:":
				return nil, nil, "Options: appears twice"
			case optionLine.MatchString(line):
				if gap {
					return nil, nil, fmt.Sprintf("option line %s comes after a line that is not an option; keep the options together, with nothing between them", quoteLine(line))
				}
				lines = append(lines, line)
				options++
			case numberedLine.MatchString(line):
				return nil, nil, fmt.Sprintf("numbered line %s is not an option; an option is N. label: summary, with N from 1 to 4", quoteLine(line))
			default:
				gap = gap || options > 0
				context = append(context, raw)
			}
		case after:
			switch {
			case line == "Options:":
				return nil, nil, "Options: comes after Recommend:"
			case strings.HasPrefix(line, "Recommend:"):
				return nil, nil, "Recommend: appears twice"
			case optionLine.MatchString(line) || numberedLine.MatchString(line):
				return nil, nil, fmt.Sprintf("line %s comes after Recommend:; no numbered line may follow it", quoteLine(line))
			}
			context = append(context, raw)
		}
	}
	return lines, context, ""
}

// maxQuotedLine is the longest line a shape error quotes.
const maxQuotedLine = 120

// quoteLine quotes a report line for a shape error, cut to maxQuotedLine
// characters.
func quoteLine(s string) string {
	if r := []rune(s); len(r) > maxQuotedLine {
		s = string(r[:maxQuotedLine]) + "..."
	}
	return strconv.Quote(s)
}

// numberedLine is a line that opens like a numbered option, "3. ", "3.c" or
// "3) ", but not a decimal such as "3.5".
var numberedLine = regexp.MustCompile(`^\d+[.)]([^\d]|$)`)

// stripListMarker removes one leading "- ", "* " or "+ " from a line.
func stripListMarker(line string) string {
	if len(line) > 1 && strings.ContainsRune("-*+", rune(line[0])) && line[1] == ' ' {
		return strings.TrimSpace(line[2:])
	}
	return line
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
