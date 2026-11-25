// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// S233 (*proposed*): ork ai instructions and ork ai skill print the agent guide
// and the SKILL.md built from it, with no daemon.

// aiApp is an App with no daemon: it has no socket and no Start function, and
// it fails the test on any daemon call. cwd is the directory the install option
// writes under.
func aiApp(t *testing.T, cwd string) *App {
	t.Helper()
	return &App{
		Getwd: func() (string, error) { return cwd, nil },
		Call: func(_ context.Context, method string, _, _ any) error {
			t.Errorf("the daemon was called with %s", method)
			return nil
		},
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAIInstructionsPrintsGuideWithoutDaemon(t *testing.T) {
	code, stdout, stderr := run(t, aiApp(t, t.TempDir()), "ai", "instructions")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q; want exit 0", code, stderr)
	}
	if !strings.HasPrefix(stdout, "# ") {
		t.Errorf("the guide does not start with a heading line: %.60q", stdout)
	}
	if !strings.Contains(stdout, "ork- id") {
		t.Errorf("the guide does not name the ork- id rule:\n%s", stdout)
	}
	if stdout != aiGuide {
		t.Errorf("ai instructions did not print the embedded guide")
	}
}

func TestAISkillPrintsSkillMD(t *testing.T) {
	code, stdout, stderr := run(t, aiApp(t, t.TempDir()), "ai", "skill")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q; want exit 0", code, stderr)
	}
	front, body, ok := strings.Cut(strings.TrimPrefix(stdout, "---\n"), "\n---\n")
	if !strings.HasPrefix(stdout, "---\n") || !ok {
		t.Fatalf("the skill does not start with YAML front matter:\n%.200s", stdout)
	}
	lines := strings.Split(front, "\n")
	if len(lines) != 2 || lines[0] != "name: ork" || !strings.HasPrefix(lines[1], "description: ") || len(lines[1]) < len("description: x") {
		t.Errorf("front matter is not name: ork and a one-line description: %q", front)
	}
	if strings.TrimPrefix(body, "\n") != aiGuide {
		t.Errorf("the skill body is not the guide")
	}
}

func TestAISkillOutputWritesOneFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "er", "SKILL.md")
	app := aiApp(t, dir)
	_, printed, _ := run(t, app, "ai", "skill")
	code, stdout, stderr := run(t, app, "ai", "skill", "--output", path)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if got := readFile(t, path); got != printed {
		t.Errorf("the file differs from the printed skill")
	}
	if !strings.Contains(stdout, path) {
		t.Errorf("stdout does not list %s: %q", path, stdout)
	}
	if strings.Contains(stdout, "name: ork") {
		t.Errorf("stdout printed the skill as well as writing it: %q", stdout)
	}
}

var skillDirs = []string{".claude", ".agents", ".grok"}

func TestAISkillInstallWritesThreeDirs(t *testing.T) {
	dir := t.TempDir()
	app := aiApp(t, dir)
	_, printed, _ := run(t, app, "ai", "skill")
	code, stdout, stderr := run(t, app, "ai", "skill", "--install")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, d := range skillDirs {
		path := filepath.Join(dir, d, "skills", "ork", "SKILL.md")
		if got := readFile(t, path); got != printed {
			t.Errorf("%s differs from the printed skill", path)
		}
		if !strings.Contains(stdout, path) {
			t.Errorf("stdout does not list %s:\n%s", path, stdout)
		}
	}
}

func TestAISkillInstallRefusesDifferentText(t *testing.T) {
	dir := t.TempDir()
	app := aiApp(t, dir)
	_, printed, _ := run(t, app, "ai", "skill")
	agents := filepath.Join(dir, ".agents", "skills", "ork", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(agents), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agents, []byte("my own skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := run(t, app, "ai", "skill", "--install")
	if code != 1 || !strings.Contains(stderr, agents) {
		t.Fatalf("exit %d, stderr %q; want exit 1 naming %s", code, stderr, agents)
	}
	if got := readFile(t, agents); got != "my own skill\n" {
		t.Errorf("the different text was replaced: %q", got)
	}
	// A refusal writes nothing: the other two destinations stay absent.
	for _, d := range []string{".claude", ".grok"} {
		if _, err := os.Stat(filepath.Join(dir, d)); err == nil {
			t.Errorf("%s was written although another destination was refused", d)
		}
	}

	code, _, stderr = run(t, app, "ai", "skill", "--install", "--force")
	if code != 0 {
		t.Fatalf("--force: exit %d, stderr %q", code, stderr)
	}
	if got := readFile(t, agents); got != printed {
		t.Errorf("--force did not replace the text")
	}

	// Identical text is left alone: its modification time stays.
	old := filepath.Join(dir, ".claude", "skills", "ork", "SKILL.md")
	before, err := os.Stat(old)
	if err != nil {
		t.Fatal(err)
	}
	past := before.ModTime().Add(-3600 * 1e9)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = run(t, app, "ai", "skill", "--install")
	if code != 0 {
		t.Fatalf("identical text: exit %d, stderr %q", code, stderr)
	}
	after, err := os.Stat(old)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(past) {
		t.Errorf("identical text was rewritten")
	}
}

func TestAIJSON(t *testing.T) {
	dir := t.TempDir()
	app := aiApp(t, dir)
	_, stdout, _ := run(t, app, "ai", "instructions", "--json")
	var text struct{ Text string }
	mustJSON(t, stdout, &text)
	if text.Text != aiGuide {
		t.Errorf("--json text is not the guide: %.80q", text.Text)
	}

	path := filepath.Join(dir, "out", "SKILL.md")
	code, stdout, stderr := run(t, app, "ai", "skill", "--output", path, "--json")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	var written struct{ Written []string }
	mustJSON(t, stdout, &written)
	if !slices.Equal(written.Written, []string{path}) {
		t.Errorf("written = %v, want [%s]", written.Written, path)
	}

	_, stdout, _ = run(t, app, "ai", "skill", "--json")
	mustJSON(t, stdout, &text)
	if !strings.HasPrefix(text.Text, "---\nname: ork\n") {
		t.Errorf("ai skill --json text is not the skill: %.80q", text.Text)
	}

	// A refusal is a --json error object, as for every command.
	_, stdout, _ = run(t, app, "ai", "bogus", "--json")
	var e struct{ Error struct{ Code string } }
	mustJSON(t, stdout, &e)
	if e.Error.Code != codeUsage {
		t.Errorf("error code %q, want %q", e.Error.Code, codeUsage)
	}
}

func TestAIUnknownSubcommandIsUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"ai"},
		{"ai", "bogus"},
		{"ai", "instructions", "extra"},
		{"ai", "skill", "extra"},
		{"ai", "skill", "--output", "a", "--install"},
		{"ai", "skill", "--force"},
	} {
		code, stdout, stderr := run(t, aiApp(t, t.TempDir()), args...)
		if code != ExitUsage || stdout != "" {
			t.Errorf("%v: exit %d, stdout %q; want exit 2 and no stdout", args, code, stdout)
		}
		if !strings.Contains(stderr, "ork ai instructions") || !strings.Contains(stderr, "ork ai skill") {
			t.Errorf("%v: the usage line does not list both subcommands: %q", args, stderr)
		}
	}
}

func TestAIHelpPrintsItsBlocks(t *testing.T) {
	for _, args := range [][]string{{"ai", "--help"}, {"ai", "instructions", "-h"}, {"ai", "skill", "--help"}} {
		code, stdout, stderr := run(t, helpApp(t), args...)
		if code != 0 || stderr != "" || !strings.Contains(stdout, "  ork ai ") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
}

// guideCommandLines are the guide lines that start with "ork ", after a leading
// dash and indent.
func guideCommandLines() []string {
	var lines []string
	for _, line := range strings.Split(aiGuide, "\n") {
		line = strings.TrimLeft(line, " \t-")
		if strings.HasPrefix(line, "ork ") {
			lines = append(lines, line)
		}
	}
	return lines
}

var (
	leadingWords = regexp.MustCompile(`^ork((?: [a-z][a-z-]*)+)`)
	optionWord   = regexp.MustCompile(`--[a-z][a-z-]*`)
)

func TestAIGuideCommandsAreInUsage(t *testing.T) {
	table := commandWords(t)
	lines := guideCommandLines()
	if len(lines) < 10 {
		t.Fatalf("the guide has only %d command lines", len(lines))
	}
	for _, line := range lines {
		m := leadingWords.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("no command word in %q", line)
			continue
		}
		words := strings.Fields(m[1])
		// The longest run of words that is a command of the usage text.
		for len(words) > 0 && !slices.ContainsFunc(table, func(w []string) bool { return slices.Equal(w, words) }) {
			words = words[:len(words)-1]
		}
		if len(words) == 0 {
			t.Errorf("the usage text has no command %q: %q", strings.TrimSpace(m[1]), line)
		}
		for _, opt := range optionWord.FindAllString(line, -1) {
			if !regexp.MustCompile(`(^|[^a-z-])` + regexp.QuoteMeta(opt) + `($|[^a-z-])`).MatchString(usageText) {
				t.Errorf("the usage text has no option %s: %q", opt, line)
			}
		}
	}
}

// ownerSection is the guide's section about the owner's own steps.
func ownerSection(t *testing.T) string {
	t.Helper()
	var out []string
	in := false
	for _, line := range strings.Split(aiGuide, "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			in = strings.Contains(strings.ToLower(line), "owner-only")
		case in:
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		t.Fatal("the guide has no Owner-only section")
	}
	return strings.ToLower(strings.Join(strings.Fields(strings.Join(out, " ")), " "))
}

func TestAIGuideNamesOwnerOnlySteps(t *testing.T) {
	section := ownerSection(t)
	for _, want := range []string{
		"ork approve",
		"adj answers",
		"scope or design questions",
		"from an interactive question only",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the owner-only section lacks %q:\n%s", want, section)
		}
	}
}

func TestUsageListsAI(t *testing.T) {
	for _, want := range []string{"  ork ai instructions", "  ork ai skill "} {
		if !strings.Contains(usageText, want) {
			t.Errorf("the usage text lacks %q", want)
		}
	}
	if !slices.Contains(groupCommands, "ai") {
		t.Errorf("ai is not a group command, so ork ai --help prints nothing")
	}
	// TestLaunchUsageFitsEightyColumns and
	// TestUsageTextNamesTheUpgradeFlagAndFitsEightyColumns read the same text
	// for the new lines' width.
}

func TestAIGuideIsPlainASCII(t *testing.T) {
	for i, line := range strings.Split(aiGuide, "\n") {
		if strings.ContainsFunc(line, func(r rune) bool { return r > 126 }) {
			t.Errorf("line %d is not ASCII: %q", i+1, line)
		}
	}
	if n := strings.Count(aiGuide, "\n"); n > 90 {
		t.Errorf("the guide has %d lines, want about 80 at most", n)
	}
}
