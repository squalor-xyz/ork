// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Report is what the engine reads from a role's report.
type Report struct {
	Verdict  string // "PASS" or "FAIL" for a gate, else empty
	Passed   int    // from the Tests lines
	Failed   int
	HasTests bool
	Question string // the Owner needed section; empty for none
}

// ShapeError lists every way a report breaks the shape the prompts ask for
// (role.report-shape). The owner sees it.
type ShapeError struct{ Problems []string }

func (e *ShapeError) Error() string {
	return "report shape: " + strings.Join(e.Problems, "; ")
}

var testsLine = regexp.MustCompile(`(?m)^Tests: (\d+) passed, (\d+) failed\r?$`)

const ownerHeading = "## Owner needed"

// CheckReport checks a role's report against the shape the prompts ask for:
// the gate's first line, a Tests line from the implementer and the gate, and
// a final Owner needed section from every role. finish is the implementer's
// finish step, which needs no Tests line. A shape error is a *ShapeError.
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

	if body, ok := section(text, ownerHeading); !ok {
		problems = append(problems, fmt.Sprintf("no %q section", ownerHeading))
	} else if !isNone(body) {
		rep.Question = strings.TrimSpace(body)
	}

	if len(problems) > 0 {
		return nil, &ShapeError{Problems: problems}
	}
	return &rep, nil
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

// Findings returns the reviewer's findings: the text of sections (A), (B) and
// (D) of its report, empty when each says nothing (*proposed* rule: the
// prompt asks for "nothing" in an empty section). Section (C) holds notes for
// later and does not start a fix round. A report without sections (A) and
// (B), or with a blank one, is a shape error.
func Findings(text string) (string, error) {
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

	var problems []string
	var found []string
	for _, letter := range []string{"A", "B", "D"} {
		s, ok := sections[letter]
		if !ok {
			if letter != "D" {
				problems = append(problems, fmt.Sprintf("no section (%s)", letter))
			}
			continue
		}
		content, blank := s.content()
		switch {
		case blank:
			problems = append(problems, fmt.Sprintf(`section (%s) is blank; the prompt asks for "nothing" in an empty section`, letter))
		case content != "":
			found = append(found, fmt.Sprintf("(%s)%s\n%s", letter, s.remainder, strings.TrimSpace(strings.Join(s.body, "\n"))))
		}
	}
	if len(problems) > 0 {
		return "", &ShapeError{Problems: problems}
	}
	return strings.TrimSpace(strings.Join(found, "\n\n")), nil
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
