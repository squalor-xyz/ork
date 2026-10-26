// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// OwnerQuestion and Option share the persisted question shape.
type OwnerQuestion = runrec.OwnerQuestion
type Option = runrec.Option

// Report is what the engine reads from a role's report.
type Report struct {
	Ask      *OwnerQuestion
	Verdict  string // "PASS" or "FAIL" for a gate, else empty
	Passed   int    // from the Tests lines
	Failed   int
	HasTests bool
	Key      string // optional adj decision key
	Question string // the Owner needed section; empty for none
}

// ShapeError lists every way a report breaks the shape the prompts ask for
// (role.report-shape). The owner sees it.
type ShapeError struct{ Problems []string }

func (e *ShapeError) Error() string {
	return "report shape: " + strings.Join(e.Problems, "; ")
}

var decisionKey = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)

var testsLine = regexp.MustCompile(`(?m)^Tests: (\d+) passed, (\d+) failed\r?$`)

const ownerHeading = "## Owner needed"

// CheckReport checks a role's report against the shape the prompts ask for:
// the gate's first line, a Tests line from the implementer and the gate, and
// a final Owner needed section from every role. finish is the implementer's
// finish step, which needs no Tests line. The architect needs only the shared
// Owner needed section; its whole report is findings context (adj 150).
// A shape error is a *ShapeError.
func CheckReport(role string, finish bool, text string) (*Report, error) {
	var rep Report
	var problems []string

	if role == RoleGate {
		first, _, _ := strings.Cut(text, "\n")
		switch first = strings.TrimSuffix(first, "\r"); first {
		case "VERDICT: PASS":
			rep.Verdict = "PASS"
		case "VERDICT: FAIL":
			rep.Verdict = "FAIL"
		default:
			problems = append(problems, `the first line must be exactly "VERDICT: PASS" or "VERDICT: FAIL"`)
		}
	}

	if (role == RoleImplementer && !finish) || role == RoleGate {
		matches := testsLine.FindAllStringSubmatch(text, -1)
		if len(matches) == 0 {
			problems = append(problems, `no "Tests: N passed, M failed" line`)
		}
		for _, m := range matches {
			p, _ := strconv.Atoi(m[1])
			f, _ := strconv.Atoi(m[2])
			rep.Passed += p
			rep.Failed += f
			rep.HasTests = true
		}
	}

	if role == RoleGate && rep.Verdict == "PASS" && rep.Failed > 0 {
		problems = append(problems, "PASS reports failed tests")
	}
	if body, ok := section(text, ownerHeading); !ok {
		problems = append(problems, fmt.Sprintf("no %q section", ownerHeading))
	} else if !isNone(body) {
		rep.Question = strings.TrimSpace(body)
		ask, err := parseOwnerQuestion(body)
		if err != nil {
			problems = append(problems, err.Error())
		} else {
			rep.Ask = ask
		}

		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Decision key:") {
				key := strings.TrimSpace(strings.TrimPrefix(line, "Decision key:"))
				if rep.Key != "" || !decisionKey.MatchString(key) {
					problems = append(problems, "Owner needed must have at most one Decision key: area.topic line with a lowercase dotted key")
				}
				rep.Key = key
			}
		}
	}

	if len(problems) > 0 {
		return nil, &ShapeError{Problems: problems}
	}
	return &rep, nil
}

var optionLine = regexp.MustCompile(`^([1-4])\. ([^:]+): (.+)$`)
var recommendLine = regexp.MustCompile(`^Recommend: ([1-4]) - (.+)$`)

func parseOwnerQuestion(body string) (*OwnerQuestion, error) {
	var lines []string
	structured := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Question:") || line == "Options:" || strings.HasPrefix(line, "Recommend:") {
			structured = true
		}
		if line != "" && !strings.HasPrefix(line, "Decision key:") {
			lines = append(lines, line)
		}
	}
	if !structured {
		return nil, nil
	}
	// Context before Question stays in the raw section, outside the strict block.
	for i, line := range lines {
		if strings.HasPrefix(line, "Question:") {
			lines = lines[i:]
			break
		}
	}
	bad := func() (*OwnerQuestion, error) {
		return nil, fmt.Errorf("Owner needed requires Question: title, Options: with 2 to 4 numbered unique labels and summaries, and Recommend: N - reason")
	}
	if len(lines) < 5 || len(lines) > 7 || !strings.HasPrefix(lines[0], "Question:") || lines[1] != "Options:" {
		return bad()
	}
	q := &OwnerQuestion{Title: strings.TrimSpace(strings.TrimPrefix(lines[0], "Question:"))}
	if q.Title == "" {
		return bad()
	}
	labels := map[string]bool{}
	for i, line := range lines[2 : len(lines)-1] {
		m := optionLine.FindStringSubmatch(line)
		if m == nil || m[1] != strconv.Itoa(i+1) {
			return bad()
		}
		label, summary := strings.TrimSpace(m[2]), strings.TrimSpace(m[3])
		if label == "" || summary == "" || labels[label] {
			return bad()
		}
		labels[label] = true
		q.Options = append(q.Options, Option{Label: label, Summary: summary})
	}
	m := recommendLine.FindStringSubmatch(lines[len(lines)-1])
	if m == nil {
		return bad()
	}
	q.Recommend, _ = strconv.Atoi(m[1])
	q.Reason = strings.TrimSpace(m[2])
	if q.Recommend > len(q.Options) || q.Reason == "" {
		return bad()
	}
	return q, nil
}

// ParseOwnerQuestions reads every question block of a body: each block starts
// at a line that begins with "Question:" and runs to the next such line. The
// strict shape of parseOwnerQuestion holds for each block, which may carry one
// Decision key line. Lines before the first block are context and are
// dropped. A body without a block, or with a malformed one, is an error that
// names the block.
func ParseOwnerQuestions(body string) ([]SpecQuestion, error) {
	var blocks [][]string
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "Question:") {
			blocks = append(blocks, nil)
		}
		if len(blocks) == 0 {
			continue
		}
		blocks[len(blocks)-1] = append(blocks[len(blocks)-1], line)
	}
	if len(blocks) == 0 {
		return nil, errors.New("no Question: block")
	}
	var out []SpecQuestion
	for i, lines := range blocks {
		var q SpecQuestion
		for _, line := range lines {
			if !strings.HasPrefix(line, "Decision key:") {
				continue
			}
			key := strings.TrimSpace(strings.TrimPrefix(line, "Decision key:"))
			if q.Key != "" || !decisionKey.MatchString(key) {
				return nil, fmt.Errorf("question %d must have at most one Decision key: area.topic line with a lowercase dotted key", i+1)
			}
			q.Key = key
		}
		ask, err := parseOwnerQuestion(strings.Join(lines, "\n"))
		if err != nil {
			return nil, fmt.Errorf("question %d: %w", i+1, err)
		}
		q.Ask = ask
		q.Text = strings.TrimSpace(strings.Join(lines, "\n"))
		out = append(out, q)
	}
	return out, nil
}

// section returns the lines under the heading, up to the next "## " heading
// or the end of the text.
func section(text, heading string) (string, bool) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSuffix(line, "\r") != heading {
			continue
		}
		var body []string
		for _, l := range lines[i+1:] {
			if strings.HasPrefix(l, "## ") {
				break
			}
			body = append(body, strings.TrimSuffix(l, "\r"))
		}
		return strings.Join(body, "\n"), true
	}
	return "", false
}

// isNone reports whether an Owner needed body says there is no question.
func isNone(body string) bool {
	s := strings.ToLower(strings.TrimSpace(body))
	s = strings.TrimSpace(strings.TrimRight(s, "."))
	return s == "" || s == "none"
}

// sectionHead matches the line that opens one of the reviewer's sections
// (A) to (D), as a heading, a bullet or bold text.
var sectionHead = regexp.MustCompile(`^\s*(?:#{1,6}\s*|[-*+]\s+|\*\*)*\(([A-D])\)(.*)$`)

type reviewSection struct {
	remainder string
	body      []string
}

// parseSections splits a reviewer or UI pass report into its sections (A) to
// (D).
func parseSections(text string) map[string]*reviewSection {
	sections := map[string]*reviewSection{}
	var cur *reviewSection
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if m := sectionHead.FindStringSubmatch(line); m != nil {
			cur = &reviewSection{remainder: m[2]}
			if _, dup := sections[m[1]]; !dup {
				sections[m[1]] = cur
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			cur = nil
			continue
		}
		if cur != nil {
			cur.body = append(cur.body, line)
		}
	}
	return sections
}

// Findings returns the reviewer's fix findings: the text of sections (A), (B)
// and (D) of its report, and the fix-tagged findings of (C), empty when each
// says nothing (*proposed* rule: the prompt asks for "nothing" in an empty
// section). Section (C) holds notes for later and does not start a fix round
// unless a finding in it is tagged fix. A finding tagged owner or later is not
// a fix finding (Triage). A report without sections (A) and (B), or with a
// blank one, is a shape error.
func Findings(text string) (string, error) {
	t, err := Triage(text)
	if err != nil {
		return "", err
	}
	return t.Fix, nil
}

// Triage sorts a reviewer or UI pass report's findings by kind (adj 163,
// review.finding-kinds). A finding is a list item or paragraph that starts
// with a tag in square brackets: [fix], [owner], [owner blocker] or [later].
// Lines indented under the tag line belong to the finding. A finding without
// a tag is a fix finding, so a report without tags is all fix. In section (C)
// only tagged findings count. A malformed owner or later finding is a shape
// error, with every other problem of the report.
func Triage(text string) (*Triaged, error) {
	sections := parseSections(text)
	t := &Triaged{}
	var problems []string
	var found []string
	for _, letter := range []string{"A", "B", "C", "D"} {
		s, ok := sections[letter]
		if !ok {
			if letter == "A" || letter == "B" {
				problems = append(problems, fmt.Sprintf("no section (%s)", letter))
			}
			continue
		}
		segs := segments(s.body)
		tagged := slices.ContainsFunc(segs, func(g segment) bool { return g.kind != "" })
		if !tagged {
			if letter == "C" {
				continue
			}
			content, blank := s.content()
			switch {
			case blank:
				problems = append(problems, fmt.Sprintf(`section (%s) is blank; the prompt asks for "nothing" in an empty section`, letter))
			case content != "":
				found = append(found, fmt.Sprintf("(%s)%s\n%s", letter, s.remainder, strings.TrimSpace(strings.Join(s.body, "\n"))))
			}
			continue
		}
		t.Tagged = true
		var keep []string
		for _, g := range segs {
			text := strings.TrimSpace(strings.Join(g.lines, "\n"))
			switch g.kind {
			case "":
				if letter != "C" && text != "" && !saysNothing(text) {
					keep = append(keep, text)
				}
			case KindFix:
				keep = append(keep, text)
			default:
				f, perr := g.finding()
				if perr != "" {
					problems = append(problems, perr)
				} else if f.Kind == KindOwner {
					t.Owner = append(t.Owner, f)
				} else {
					t.Later = append(t.Later, f)
				}
			}
		}
		if len(keep) > 0 {
			found = append(found, fmt.Sprintf("(%s)%s\n%s", letter, s.remainder, strings.Join(keep, "\n")))
		}
	}
	if len(problems) > 0 {
		return nil, &ShapeError{Problems: problems}
	}
	t.Fix = strings.TrimSpace(strings.Join(found, "\n\n"))
	return t, nil
}

// TriageGate sorts the tagged findings of a gate report by kind. Only
// tagged findings are findings: the verdict and the lines under it are the
// gate's own. Fix is the report without its owner and later findings, so a
// fix round after a FAIL reads only what must change. A report without tags
// comes back unchanged. The Owner needed section is not scanned.
func TriageGate(text string) (*Triaged, error) {
	t := &Triaged{Fix: text}
	var problems []string
	var kept []string
	var segs []segment
	skip := false
	var run []string
	flush := func() {
		segs = append(segs, segments(run)...)
		run = nil
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			skip = trimmed == ownerHeading
			if skip {
				segs = append(segs, segment{lines: []string{line}})
				continue
			}
		}
		if skip {
			segs[len(segs)-1].lines = append(segs[len(segs)-1].lines, line)
			continue
		}
		run = append(run, line)
	}
	flush()
	for _, g := range segs {
		switch g.kind {
		case "", KindFix:
			kept = append(kept, g.lines...)
			t.Tagged = t.Tagged || g.kind != ""
		default:
			t.Tagged = true
			f, perr := g.finding()
			switch {
			case perr != "":
				problems = append(problems, perr)
			case f.Kind == KindOwner:
				t.Owner = append(t.Owner, f)
			default:
				t.Later = append(t.Later, f)
			}
		}
	}
	if len(problems) > 0 {
		return nil, &ShapeError{Problems: problems}
	}
	if t.Tagged {
		t.Fix = strings.Join(kept, "\n")
	}
	return t, nil
}

// content returns the section's findings text, empty when the section says
// nothing. blank is true when the section holds nothing at all.
func (s *reviewSection) content() (text string, blank bool) {
	body := strings.TrimSpace(strings.Join(s.body, "\n"))
	if body != "" {
		if saysNothing(body) {
			return "", false
		}
		return body, false
	}
	// No body: the heading line may carry the content after a colon, or end in
	// "nothing".
	if _, after, ok := strings.Cut(s.remainder, ":"); ok && strings.TrimSpace(strings.Trim(after, "*_` ")) != "" {
		if saysNothing(after) {
			return "", false
		}
		return strings.TrimSpace(after), false
	}
	words := strings.Fields(strings.ToLower(strings.Trim(s.remainder, "*_`:.- ")))
	if len(words) > 0 {
		if last := strings.Trim(words[len(words)-1], "*_`:.-"); last == "nothing" || last == "none" {
			return "", false
		}
	}
	return "", true
}

func saysNothing(s string) bool {
	s = strings.ToLower(strings.Trim(strings.TrimSpace(s), "*_`:.-# \n"))
	return s == "nothing" || s == "none"
}
