// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFailureExcerptKeepsFailHeaderAndAssertionLines(t *testing.T) {
	out := "ok  \tgithub.com/x/y/a\t0.1s\n" +
		"ok  \tgithub.com/x/y/b\t0.2s\n" +
		"--- FAIL: TestX (0.01s)\n" +
		"    x_test.go:12: got 1, want 2\n" +
		"FAIL\n" +
		"FAIL\tpkg\t0.5s\n"
	got, tests := FailureExcerpt(out, 8000)
	for _, want := range []string{"--- FAIL: TestX (0.01s)", "    x_test.go:12: got 1, want 2", "FAIL\tpkg\t0.5s"} {
		if !strings.Contains(got, want) {
			t.Errorf("the excerpt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ok  \t") {
		t.Errorf("passing packages do not belong in the excerpt:\n%s", got)
	}
	if !reflect.DeepEqual(tests, []string{"TestX"}) {
		t.Errorf("tests: %v", tests)
	}
}

func TestFailureExcerptSurvivesAnOutputLongerThanTheTail(t *testing.T) {
	var b strings.Builder
	b.WriteString("--- FAIL: TestEarly (0.01s)\n    early_test.go:7: got 1, want 2\n")
	for b.Len() < 60000 {
		b.WriteString("goroutine 17 [select]: some unrelated dump line that fills the tail\n")
	}
	out := b.String()
	if kept := tail(out, 30000); strings.Contains(kept, "--- FAIL: TestEarly") {
		t.Fatal("the premise is wrong: the tail still holds the failure")
	}
	got, tests := FailureExcerpt(out, 8000)
	if !strings.Contains(got, "--- FAIL: TestEarly (0.01s)") || !strings.Contains(got, "early_test.go:7: got 1, want 2") {
		t.Errorf("the failure was lost:\n%s", got)
	}
	if !reflect.DeepEqual(tests, []string{"TestEarly"}) {
		t.Errorf("tests: %v", tests)
	}
}

func TestFailureExcerptKeepsSubtestsAndTheirNames(t *testing.T) {
	out := "--- FAIL: TestA (0.1s)\n" +
		"    --- FAIL: TestA/sub (0.0s)\n" +
		"        a_test.go:30: sub broke\n" +
		"--- FAIL: TestA (0.1s)\n" + // the same name again: not repeated
		"FAIL\n"
	got, tests := FailureExcerpt(out, 8000)
	if !strings.Contains(got, "    --- FAIL: TestA/sub (0.0s)") || !strings.Contains(got, "        a_test.go:30: sub broke") {
		t.Errorf("the subtest is not in the excerpt:\n%s", got)
	}
	if want := []string{"TestA", "TestA/sub"}; !reflect.DeepEqual(tests, want) {
		t.Errorf("tests %v, want %v", tests, want)
	}
}

func TestFailureExcerptKeepsPanicRaceAndBuildErrors(t *testing.T) {
	panicOut := "=== RUN   TestP\npanic: runtime error: index out of range [3] with length 3\n\ngoroutine 9 [running]:\n" +
		"pkg.TestP(0xc000)\n\t/w/p_test.go:20 +0x1d\ntesting.tRunner(0xc000)\n\tfar away line six\nFAIL\tpkg\t0.2s\n"
	got, _ := FailureExcerpt(panicOut, 8000)
	for _, want := range []string{"panic: runtime error: index out of range", "goroutine 9 [running]:", "pkg.TestP(0xc000)", "\t/w/p_test.go:20 +0x1d", "testing.tRunner(0xc000)"} {
		if !strings.Contains(got, want) {
			t.Errorf("the panic excerpt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "far away line six") {
		t.Errorf("a panic keeps the five lines after it, no more:\n%s", got)
	}

	var race strings.Builder
	race.WriteString("==================\nWARNING: DATA RACE\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&race, "race line %d\n", i)
	}
	race.WriteString("\nafter the blank line\nFAIL\tpkg\t1s\n")
	got, _ = FailureExcerpt(race.String(), 8000)
	if !strings.Contains(got, "WARNING: DATA RACE") || !strings.Contains(got, "race line 10") || !strings.Contains(got, "race line 24\n") {
		t.Errorf("the race report is missing:\n%s", got)
	}
	// The warning line and 24 lines after it make the 25.
	if strings.Contains(got, "race line 25\n") || strings.Contains(got, "race line 40") {
		t.Errorf("a race block is cut at 25 lines:\n%s", got)
	}
	short := "WARNING: DATA RACE\nRead at 0x00c by goroutine 7:\n\nunrelated text after the blank line\n"
	got, _ = FailureExcerpt(short, 8000)
	if !strings.Contains(got, "Read at 0x00c by goroutine 7:") || strings.Contains(got, "unrelated text") {
		t.Errorf("a race block ends at its blank line:\n%s", got)
	}

	build := "# github.com/x/y/pkg\npkg/file.go:3:5: undefined: x\npkg/file.go:9:2: declared and not used: y\nnoise that is not an error\nFAIL\tpkg [build failed]\n"
	got, tests := FailureExcerpt(build, 8000)
	for _, want := range []string{"# github.com/x/y/pkg", "pkg/file.go:3:5: undefined: x", "pkg/file.go:9:2: declared and not used: y", "FAIL\tpkg [build failed]"} {
		if !strings.Contains(got, want) {
			t.Errorf("the build excerpt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "noise") {
		t.Errorf("noise is kept:\n%s", got)
	}
	if len(tests) != 0 {
		t.Errorf("a build error names no test: %v", tests)
	}
}

func TestFailureExcerptCapsTheLinesUnderOneTest(t *testing.T) {
	var b strings.Builder
	b.WriteString("--- FAIL: TestChatty (0.01s)\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "    chatty_test.go:%d: log line %d\n", i, i)
	}
	b.WriteString("--- FAIL: TestNext (0.01s)\n    next_test.go:1: boom\n")
	got, tests := FailureExcerpt(b.String(), 8000)
	if !strings.Contains(got, "log line 20\n") || strings.Contains(got, "log line 21\n") {
		t.Errorf("20 lines are kept under one test:\n%s", got)
	}
	if !strings.Contains(got, "next_test.go:1: boom") || !reflect.DeepEqual(tests, []string{"TestChatty", "TestNext"}) {
		t.Errorf("the next test is kept: %v\n%s", tests, got)
	}
}

func TestFailureExcerptLimitIsExact(t *testing.T) {
	out := "--- FAIL: TestA (0s)\n    a_test.go:1: x\nFAIL\n"
	const whole = "--- FAIL: TestA (0s)\n    a_test.go:1: x"
	if got, _ := FailureExcerpt(out, len(whole)); got != whole {
		t.Errorf("a limit that fits two lines exactly: %q", got)
	}
	if got, _ := FailureExcerpt(out, len(whole)-1); got != "--- FAIL: TestA (0s)" {
		t.Errorf("a limit one byte short drops the whole line: %q", got)
	}
	if got, _ := FailureExcerpt(out, len(whole)+len("\nFAIL")); got != whole+"\nFAIL" {
		t.Errorf("a limit that fits all three: %q", got)
	}
}

func TestFailureExcerptSkipsPassingSubtestsUnderAFailure(t *testing.T) {
	out := "--- FAIL: TestA (0s)\n    --- PASS: TestA/ok (0s)\n    === RUN   TestA/bad\n    --- SKIP: TestA/skip (0s)\n" +
		"    --- FAIL: TestA/bad (0s)\n        a_test.go:2: x\n"
	got, tests := FailureExcerpt(out, 8000)
	for _, bad := range []string{"--- PASS", "=== RUN", "--- SKIP"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q does not belong in the excerpt:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "a_test.go:2: x") || !reflect.DeepEqual(tests, []string{"TestA", "TestA/bad"}) {
		t.Errorf("tests %v:\n%s", tests, got)
	}
}

func TestFailureExcerptKeepsVerboseAssertionLines(t *testing.T) {
	// go test -v prints a test's log lines before its --- line.
	out := "=== RUN   TestX\n" +
		"    cli_test.go:811: want syntax finding\n" +
		"--- FAIL: TestX (0.01s)\n" +
		"=== RUN   TestOK\n" +
		"    ok_test.go:5: a log line of a passing test\n" +
		"--- PASS: TestOK (0.00s)\n" +
		"=== RUN   TestA\n" +
		"=== RUN   TestA/sub\n" +
		"    a_test.go:30: sub broke\n" +
		"=== NAME  TestA\n" +
		"    a_test.go:40: parent line\n" +
		"--- FAIL: TestA (0.10s)\n" +
		"    --- FAIL: TestA/sub (0.00s)\n" +
		"FAIL\n"
	got, tests := FailureExcerpt(out, 8000)
	for _, want := range []string{"    cli_test.go:811: want syntax finding\n--- FAIL: TestX (0.01s)", "    a_test.go:30: sub broke", "    a_test.go:40: parent line\n--- FAIL: TestA (0.10s)"} {
		if !strings.Contains(got, want) {
			t.Errorf("the verbose excerpt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "passing test") || strings.Contains(got, "TestOK") {
		t.Errorf("a passing test's lines are kept:\n%s", got)
	}
	if strings.Contains(got, "sub broke") && strings.Index(got, "sub broke") > strings.Index(got, "--- FAIL: TestA/sub") {
		t.Errorf("the subtest's lines come before its header:\n%s", got)
	}
	if want := []string{"TestX", "TestA", "TestA/sub"}; !reflect.DeepEqual(tests, want) {
		t.Errorf("tests %v, want %v", tests, want)
	}
}

func TestFailureExcerptCountsATestWhoseHeaderDidNotFit(t *testing.T) {
	// The verbose log line fits in the limit and the header after it does not:
	// the test is not shown, so it is counted.
	out := "=== RUN   TestA\n    a_test.go:1: boom boom boom\n--- FAIL: TestA (0.01s)\n"
	got, tests := FailureExcerpt(out, 40)
	if got != "[1 more failing tests not shown]" || !reflect.DeepEqual(tests, []string{"TestA"}) {
		t.Errorf("excerpt %q, tests %v", got, tests)
	}
}

func TestFailureExcerptKeepsAtMostTwentyVerboseLinesPerTest(t *testing.T) {
	var b strings.Builder
	b.WriteString("=== RUN   TestChatty\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "    chatty_test.go:%d: log line %d\n", i, i)
	}
	b.WriteString("--- FAIL: TestChatty (0.01s)\n")
	got, _ := FailureExcerpt(b.String(), 8000)
	// The lines nearest the header, where the assertion is, are the ones kept.
	if !strings.Contains(got, "log line 30\n") || !strings.Contains(got, "log line 11\n") || strings.Contains(got, "log line 10\n") {
		t.Errorf("20 lines are kept before the header:\n%s", got)
	}
}

func TestFailureExcerptIsBounded(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 500; i++ {
		fmt.Fprintf(&b, "--- FAIL: TestN%03d (0.00s)\n    n_test.go:%d: got %d, want 0\n", i, i, i)
	}
	b.WriteString("FAIL\tpkg\t1s\n")
	got, tests := FailureExcerpt(b.String(), 2000)
	if len(got) > 2000 {
		t.Fatalf("%d bytes, limit 2000", len(got))
	}
	if len(tests) != 500 {
		t.Errorf("tests holds every failing test: %d", len(tests))
	}
	lines := strings.Split(got, "\n")
	last := lines[len(lines)-1]
	var shown int
	for _, l := range lines {
		if strings.HasPrefix(l, "--- FAIL: TestN") {
			shown++
		}
		if l != "" && !strings.HasPrefix(l, "--- FAIL: TestN") && !strings.HasPrefix(l, "    n_test.go:") && l != last {
			t.Errorf("not a whole line: %q", l)
		}
	}
	if shown == 0 || shown >= 500 {
		t.Fatalf("shown %d", shown)
	}
	if want := fmt.Sprintf("[%d more failing tests not shown]", 500-shown); last != want {
		t.Errorf("last line %q, want %q", last, want)
	}
	// Whole lines only: every shown header has its assertion line.
	if strings.Count(got, "n_test.go:") < shown-1 {
		t.Errorf("a header lost its assertion line:\n%s", got)
	}
}

func TestFailureExcerptCutsLongLinesOnARuneBoundary(t *testing.T) {
	// "é" is two bytes, so a cut at byte 400 would split a rune after an
	// odd prefix.
	long := "    x_test.go:5: " + strings.Repeat("é", 2500)
	got, _ := FailureExcerpt("--- FAIL: TestL (0s)\n"+long+"\n", 8000)
	var line string
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "    x_test.go:5:") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("assertion line missing:\n%s", got)
	}
	if !strings.HasSuffix(line, "...") || len(line) > 403 || len(line) < 402 {
		t.Errorf("line of %d bytes: %q", len(line), line[max(0, len(line)-20):])
	}
	if !utf8.ValidString(line) {
		t.Errorf("a rune was split")
	}
	ascii := "    x_test.go:5: " + strings.Repeat("z", 5000)
	got, _ = FailureExcerpt("--- FAIL: TestL (0s)\n"+ascii+"\n", 8000)
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "    x_test.go:5:") && (len(l) != 403 || !strings.HasSuffix(l, "zzz...")) {
			t.Errorf("an ASCII line is cut at 400 bytes plus ...: %d bytes", len(l))
		}
	}
}

func TestFailureExcerptOfOtherOutputIsEmpty(t *testing.T) {
	pytest := "============ test session starts ============\ncollected 3 items\n\ntest_a.py F..\n\n=== FAILURES ===\n___ test_a ___\n    assert 1 == 2\nE   AssertionError\n=== 1 failed, 2 passed in 0.12s ===\n"
	for name, out := range map[string]string{"pytest": pytest, "boom": "boom", "empty": "", "bare FAIL": "FAIL\n", "heading": "# Notes\nsome text\n"} {
		got, tests := FailureExcerpt(out, 8000)
		if got != "" || tests != nil {
			t.Errorf("%s: excerpt %q, tests %v", name, got, tests)
		}
	}
}
