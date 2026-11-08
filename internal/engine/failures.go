// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Limits of the failure excerpt (S239). Every number is a cap on how much one
// kind of failure may take, so that one noisy test does not push out the rest.
const (
	excerptLimit     = 8000 // the bytes of excerpt a fix-round brief carries
	excerptLineBytes = 400  // a longer line is cut at a rune boundary and ends in "..."
	excerptTestLines = 20   // lines kept under one `--- FAIL:` line
	excerptPanicRest = 5    // lines kept after a `panic: ` line
	excerptRaceLines = 25   // lines of one `WARNING: DATA RACE` block, the first included
)

// buildError matches the file:line:column lines of a Go build or vet error.
var buildError = regexp.MustCompile(`^\S+\.go:\d+:\d+: \S`)

// excerptLine is a kept line, the test it belongs to (-1 for none) and
// whether it is that test's `--- FAIL:` line.
type excerptLine struct {
	text   string
	test   int
	header bool
}

// FailureExcerpt picks the failing tests out of the output of `go test`: the
// `--- FAIL:` lines with the lines under them, the package FAIL lines, a panic
// with the lines after it, a data race report, and build errors. It returns
// the lines in output order, cut to at most limit bytes on a whole line, with a
// last line "[N more failing tests not shown]" when whole tests did not fit.
// tests holds every `--- FAIL:` name, in order and without repeats, shown or
// not. Output with no failing test, panic, race or build error gives "" and
// nil, so a brief for another tool's output stays as it was.
func FailureExcerpt(output string, limit int) (excerpt string, tests []string) {
	var lines []excerptLine
	index := map[string]int{}
	found := false
	keep := func(text string, test int) {
		lines = append(lines, excerptLine{text: cutLine(text), test: test})
	}
	// go test -v prints a test's log lines after its `=== RUN` line and
	// before its `--- FAIL:` line. They wait here, by test name, the last
	// excerptTestLines of them, until the result line says whether to keep them.
	pending := map[string][]string{}
	current := "" // the test the lines now belong to, from the last `=== RUN|CONT|NAME` line

	ctxIndent, ctxLeft, ctxTest := 0, 0, -1 // lines under the last `--- FAIL:` line
	panicLeft := 0                          // lines still to keep after a panic
	raceLeft := 0                           // lines still to keep of a race block
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimLeft(line, " \t")
		indent := len(line) - len(trimmed)
		if verb, name, ok := runLine(trimmed); ok {
			current = name
			if verb == "RUN" {
				delete(pending, name)
			}
			if ctxLeft > 0 && indent <= ctxIndent {
				ctxLeft = 0
			}
			continue
		}
		for _, verb := range []string{"--- PASS: ", "--- SKIP: "} {
			if name, ok := strings.CutPrefix(trimmed, verb); ok {
				name, _, _ = strings.Cut(name, " ")
				delete(pending, name)
			}
		}
		if name, ok := strings.CutPrefix(trimmed, "--- FAIL: "); ok {
			name, _, _ = strings.Cut(name, " ")
			n, seen := index[name]
			if !seen {
				n = len(tests)
				index[name] = n
				tests = append(tests, name)
			}
			found = true
			for _, before := range pending[name] {
				keep(before, n)
			}
			delete(pending, name)
			keep(line, n)
			lines[len(lines)-1].header = true
			ctxIndent, ctxLeft, ctxTest = indent, excerptTestLines, n
			panicLeft, raceLeft = 0, 0
			continue
		}
		if ctxLeft > 0 {
			switch {
			case trimmed == "" || indent <= ctxIndent:
				ctxLeft = 0
			case strings.HasPrefix(trimmed, "--- PASS:") || strings.HasPrefix(trimmed, "--- SKIP:") || strings.HasPrefix(trimmed, "=== "):
				continue
			default:
				keep(line, ctxTest)
				ctxLeft--
				continue
			}
		}
		if raceLeft > 0 {
			if trimmed == "" {
				raceLeft = 0
			} else {
				keep(line, -1)
				raceLeft--
				continue
			}
		}
		if panicLeft > 0 {
			keep(line, -1)
			panicLeft--
			continue
		}
		switch {
		case strings.HasPrefix(line, "panic: "):
			found = true
			keep(line, -1)
			panicLeft = excerptPanicRest
		case strings.Contains(line, "WARNING: DATA RACE"):
			found = true
			keep(line, -1)
			raceLeft = excerptRaceLines - 1
		case line == "FAIL" || strings.HasPrefix(line, "FAIL\t"):
			keep(line, -1)
		case strings.HasPrefix(line, "# "):
			keep(line, -1)
		case buildError.MatchString(line):
			found = true
			keep(line, -1)
		case indent > 0 && trimmed != "" && current != "":
			buf := append(pending[current], line)
			if len(buf) > excerptTestLines {
				buf = buf[len(buf)-excerptTestLines:]
			}
			pending[current] = buf
		}
	}
	if !found {
		return "", nil
	}
	return assembleExcerpt(lines, len(tests), limit), tests
}

// assembleExcerpt joins the lines that fit in limit bytes and, when whole
// tests were left out, ends with the count of them.
func assembleExcerpt(lines []excerptLine, total, limit int) string {
	join := func(budget int) (string, map[int]bool) {
		var b strings.Builder
		shown := map[int]bool{}
		for _, l := range lines {
			add := len(l.text)
			if b.Len() > 0 {
				add++
			}
			if b.Len()+add > budget {
				break
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(l.text)
			if l.header {
				shown[l.test] = true
			}
		}
		return b.String(), shown
	}
	text, shown := join(limit)
	if len(shown) == total {
		return text
	}
	// Whole tests are missing: take room for the count and join again.
	footer := func(n int) string { return fmt.Sprintf("[%d more failing tests not shown]", n) }
	text, shown = join(limit - len(footer(total)) - 1)
	if missing := total - len(shown); missing > 0 {
		if text != "" {
			text += "\n"
		}
		text += footer(missing)
	}
	return text
}

// cutLine keeps the first excerptLineBytes bytes of a line, on a rune
// boundary, and marks the cut.
func cutLine(s string) string {
	if len(s) <= excerptLineBytes {
		return s
	}
	n := excerptLineBytes
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "..."
}

// rerunMarker ends the heading line of the second run of the project command
// in a check log: "$ COMMAND (run again once: the first run failed)".
const rerunMarker = " (run again once: the first run failed)"

// checkLogOutput is one project run's output as the check log keeps it: the
// last 30000 bytes. The excerpt would not survive that cut when the failure
// came early, so the failing tests of the cut part go ahead of the tail.
func checkLogOutput(output string) string {
	kept := tail(output, 30000)
	if len(output) <= 30000 {
		return kept
	}
	if cut, _ := FailureExcerpt(output[:len(output)-30000], excerptLimit); cut != "" {
		return "[failing tests in the part of the output that was cut]\n" + cut + "\n" + kept
	}
	return kept
}

// splitCheckRuns splits a check log written with a rerun into the first and
// the second run of the project command. The heading is the whole line ork
// writes, "$ COMMAND (run again once: ...)" after a blank line, so that a test
// message that merely mentions the marker does not split the log. two is false
// when the log has no such heading.
func splitCheckRuns(log, cmd string) (first, second string, two bool) {
	heading := "\n\n$ " + cmd + rerunMarker + "\n"
	at := strings.Index(log, heading)
	if at < 0 {
		return log, "", false
	}
	first = log[:at]
	second = log[at+len(heading):]
	if i := strings.Index(second, "\n$ slicer check --json\n"); i >= 0 {
		second = second[:i]
	}
	return first, second, true
}

// checkFailureBody is what a fix-round brief shows of a red check log: the
// failing tests and assertion lines first, then the end of the log as before.
// A log of a check that ran twice says so, and names the tests that failed in
// the first run only. A log with nothing to pick out is shown as it was.
func checkFailureBody(log, cmd string, attempts int) string {
	src := log
	// Only a check recorded as run twice is split: a log of one run is never
	// read as two, whatever its output says.
	var first, second string
	two := false
	if attempts == 2 {
		first, second, two = splitCheckRuns(log, cmd)
	}
	if two {
		src = second
	}
	excerpt, tests := FailureExcerpt(src, excerptLimit)
	end := "```\n" + tail(log, 30000) + "\n```"
	if excerpt == "" {
		return end
	}
	var b strings.Builder
	if two {
		b.WriteString("The check failed in both runs: ork ran it again once before sending this round.\n\n")
		_, firstTests := FailureExcerpt(first, 1<<20)
		inSecond := map[string]bool{}
		for _, name := range tests {
			inSecond[name] = true
		}
		var only []string
		for _, name := range firstTests {
			if !inSecond[name] {
				only = append(only, name)
			}
		}
		if len(only) > 0 {
			fmt.Fprintf(&b, "Failed on the first run only: %s.\n\n", firstOnlyList(only))
		}
	}
	b.WriteString("The failing tests and assertion lines, cut to fit:\n\n```\n" + excerpt + "\n```\n\nThe end of the output:\n\n" + end)
	return b.String()
}

// runLine reads a `=== RUN`, `=== CONT` or `=== NAME` line of go test -v and
// returns the verb and the test name.
func runLine(trimmed string) (verb, name string, ok bool) {
	rest, ok := strings.CutPrefix(trimmed, "=== ")
	if !ok {
		return "", "", false
	}
	f := strings.Fields(rest)
	if len(f) < 2 || (f[0] != "RUN" && f[0] != "CONT" && f[0] != "NAME") {
		return "", "", false
	}
	return f[0], f[1], true
}

// firstOnlyLimit is the most test names the "failed on the first run only"
// line names.
const firstOnlyLimit = 20

// firstOnlyList names at most firstOnlyLimit tests, each cut like any excerpt
// line and in backticks (a backtick in a name becomes a quote), and counts the
// rest.
func firstOnlyList(names []string) string {
	var parts []string
	for i, name := range names {
		if i == firstOnlyLimit {
			break
		}
		parts = append(parts, "`"+strings.ReplaceAll(cutLine(name), "`", "'")+"`")
	}
	list := strings.Join(parts, ", ")
	if more := len(names) - len(parts); more > 0 {
		list += fmt.Sprintf(", and %d more", more)
	}
	return list
}
