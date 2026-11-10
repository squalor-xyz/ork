// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/registry"
)

// RunCheck runs the command with `sh -c` in dir, with a deadline. A command
// that exits non-zero or runs out of time is a result with OK false; only a
// failure to run it at all is an error.
func RunCheck(ctx context.Context, dir, command string, timeout time.Duration) (CheckOutput, error) {
	res, err := clirun.Run(ctx, clirun.Call{Bin: "sh", Args: []string{"-c", command}, Dir: dir, Timeout: timeout})
	out := string(res.Stdout) + string(res.Stderr)
	var exitErr *clirun.ExitError
	var timeoutErr *clirun.TimeoutError
	switch {
	case err == nil:
		return CheckOutput{OK: true, Output: out}, nil
	case errors.As(err, &exitErr):
		return CheckOutput{Output: out + fmt.Sprintf("\nexit %d\n", res.ExitCode)}, nil
	case errors.As(err, &timeoutErr):
		return CheckOutput{Output: out + fmt.Sprintf("\ntimed out after %v\n", timeout), TimedOut: true}, nil
	}
	return CheckOutput{}, err
}

// The static check of a check command (S295, *proposed*). It runs nothing but
// `sh -n`, which only parses: the first word of each simple command resolves,
// and the files the command names exist in the directory the check runs in.
// It is cheap and bounded, and it reads the command as text. It does not
// expand, so a word with an expansion or a glob is never looked at.

// CheckFault is one thing the static check found wrong with a check command.
type CheckFault struct {
	Syntax bool   // the shell cannot parse the command; Word is empty
	Word   string // the word the fault is about
	Detail string // what is wrong with it, or with the syntax
}

// Text is the fault as one phrase: the shell's message for a syntax fault,
// else the quoted word and what is wrong with it.
func (f CheckFault) Text() string {
	if f.Syntax {
		return f.Detail
	}
	return fmt.Sprintf("%q %s", f.Word, f.Detail)
}

// VerifyCheck returns one line for each problem that keeps command from
// running in dir, or nil. Each line holds the command; a problem about a file
// holds dir. The lines are for the owner: a refusal at `projects set`, the
// reason of a pause at setup, a launch warning.
func VerifyCheck(ctx context.Context, dir, command string) []string {
	return VerifyCheckIn(ctx, dir, command, dir)
}

// CheckDirLabel is how the lines of a message that already names the
// directory (the setup pause, the refusal, the launch warning) call it, so
// that the directory is written once.
const CheckDirLabel = "the check directory"

// CheckCannotRunPrefix starts the setup pause reason and the plan warning
// about a check command that cannot run; the daemon and the TUI recognize
// them by it.
const CheckCannotRunPrefix = "check command cannot run in "

// VerifyCheckIn is VerifyCheck with dirLabel as the name of the directory in
// the lines.
func VerifyCheckIn(ctx context.Context, dir, command, dirLabel string) []string {
	var out []string
	for _, f := range CheckFaults(ctx, dir, command, dirLabel) {
		text := f.Text()
		if f.Syntax {
			text = "shell syntax: " + text
		}
		out = append(out, fmt.Sprintf("%s (command: %s)", text, command))
	}
	return out
}

// CheckCommandProblems verifies, in dir, the commands a run of project p runs:
// the round check command, and the check command when it differs (S285). It
// returns the problems (VerifyCheck's lines) and the project flag of the first
// command that has any, --round-check or --check, for the fix text. The
// message that holds the lines names dir, so a nil verify is VerifyCheckIn
// with CheckDirLabel.
func CheckCommandProblems(ctx context.Context, p registry.Project, dir string, verify VerifyFunc) (problems []string, flag string) {
	if verify == nil {
		verify = func(ctx context.Context, dir, command string) []string {
			return VerifyCheckIn(ctx, dir, command, CheckDirLabel)
		}
	}
	commands := []string{roundCommandOf(p)}
	if p.CheckCommand != "" && commands[0] != p.CheckCommand {
		commands = append(commands, p.CheckCommand)
	}
	for i, command := range commands {
		if command == "" {
			continue
		}
		if found := verify(ctx, dir, command); len(found) > 0 {
			problems = append(problems, found...)
			if flag == "" {
				flag = "--check"
				if i == 0 && p.RoundCheckCommand != "" {
					flag = "--round-check"
				}
			}
		}
	}
	return problems, flag
}

// checkSyntaxTimeout bounds the `sh -n` parse.
const checkSyntaxTimeout = 10 * time.Second

// shellErrorPrefix strips the shell's "sh: -c: line 1: " from its message.
var shellErrorPrefix = regexp.MustCompile(`^.*?sh: (?:-c: line [0-9]+: |[0-9]+: )`)

// CheckFaults is the static check. dir is where the check will run; relative
// words are resolved against it. dirLabel is how a fault names that
// directory: VerifyCheck passes the path, ork doctor the words "project
// directory". It runs `sh -n -c command`, which parses and never executes,
// even substitutions and redirects.
func CheckFaults(ctx context.Context, dir, command, dirLabel string) []CheckFault {
	res, err := clirun.Run(ctx, clirun.Call{Bin: "sh", Args: []string{"-n", "-c", command}, Timeout: checkSyntaxTimeout})
	if err != nil {
		message := shellErrorPrefix.ReplaceAllString(firstNonEmptyLine(res.Stderr), "")
		if message == "" {
			message = "could not check shell syntax"
		}
		return []CheckFault{{Syntax: true, Detail: message}}
	}
	var faults []CheckFault
	seen := map[string]bool{}
	add := func(f CheckFault) {
		if !seen[f.Text()] {
			seen[f.Text()] = true
			faults = append(faults, f)
		}
	}
	for i, words := range simpleCommands(command) {
		// Leading assignments are not the command; a PATH assignment changes
		// where the command is found, so the lookup is skipped for it.
		pathSet := false
		for len(words) > 0 && assignmentPrefix.MatchString(words[0].text) {
			pathSet = pathSet || strings.HasPrefix(words[0].text, "PATH=")
			words = words[1:]
		}
		if len(words) == 0 || words[0].dynamic {
			continue
		}
		name := words[0].text
		if shellBuiltins[name] {
			if name == "cd" || name == "pushd" || name == "popd" {
				break // later relative paths are relative to a directory that is not known
			}
			continue
		}
		// A path built by an earlier step of the command is not there yet.
		built := i > 0 && strings.ContainsRune(name, '/') && !filepath.IsAbs(name)
		if !pathSet && !built {
			if detail := checkWordDetail(dir, name, dirLabel); detail != "" {
				add(CheckFault{Word: name, Detail: detail})
			}
		}
		if noFileArgs[filepath.Base(name)] {
			continue
		}
		prev := ""
		for _, w := range words[1:] {
			after := prev
			prev = w.text
			if w.text == "-m" {
				break // python -m MODULE ARGS: dotted names are modules, not files
			}
			if w.dynamic || outputFlags[after] || !namesAFile(w.text) {
				continue
			}
			path := w.text
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				add(CheckFault{Word: w.text, Detail: "does not exist in " + dirLabel})
			}
		}
	}
	return faults
}

// checkWordDetail says why the first word of a command cannot run, or "".
func checkWordDetail(dir, word, dirLabel string) string {
	bin := word
	if strings.ContainsRune(bin, '/') && !filepath.IsAbs(bin) {
		bin = filepath.Join(dir, bin)
	}
	if _, err := exec.LookPath(bin); err == nil {
		return ""
	}
	if !strings.ContainsRune(word, '/') {
		return "not found on PATH"
	}
	if _, err := os.Stat(bin); errors.Is(err, os.ErrNotExist) {
		if filepath.IsAbs(word) {
			return "not found"
		}
		return "not found in " + dirLabel
	}
	return "not executable"
}

func firstNonEmptyLine(b []byte) string {
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// namesAFile says whether a word that is not a flag is a plain path: it has
// an explicit directory part (a leading ./, ../ or /, or a trailing /), or an
// extension of letters. A word without those may be a git ref (origin/main), a
// test name (TestA/sub) or a module (tests.test_x), so it is left alone, as are
// the package patterns of Go (./... and ./pkg/...).
func namesAFile(w string) bool {
	switch {
	case w == "" || w[0] == '-' || w == "." || w == ".." || strings.HasSuffix(w, "/..."):
		return false
	case strings.ContainsAny(w, "*?[]{}$`~=:@,%"):
		return false
	case strings.HasPrefix(w, "./") || strings.HasPrefix(w, "../") || strings.HasPrefix(w, "/") || strings.HasSuffix(w, "/"):
		return true
	}
	dot := strings.LastIndexByte(w, '.')
	if dot <= 0 || dot == len(w)-1 {
		return false
	}
	for _, r := range w[dot+1:] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// shellBuiltins are the first words that need no program on PATH: the
// reserved words and the builtins of sh. A command that starts with one is not
// looked at. cd, pushd and popd also end the check, because the paths after
// them are relative to another directory.
var shellBuiltins = map[string]bool{}

// noFileArgs are the commands whose arguments often name files that do not
// exist yet (they create them, or they are the tool's own subcommands). Their
// first word is still looked up.
var noFileArgs = map[string]bool{}

// outputFlags are the flags whose value is a file the command writes.
var outputFlags = map[string]bool{}

func init() {
	for _, w := range strings.Fields("! { } case do done elif else esac fi for if in then until while " +
		". : [ break continue eval exec exit export readonly return set shift times trap unset cd command time " +
		"source pushd popd test echo printf true false pwd read kill wait umask ulimit alias unalias type hash getopts local") {
		shellBuiltins[w] = true
	}
	for _, w := range strings.Fields("echo printf rm rmdir mkdir touch cp mv ln tar zip unzip make npm yarn pnpm npx git curl wget") {
		noFileArgs[w] = true
	}
	for _, w := range strings.Fields("-o -O --output --out --outdir --output-dir -outputdir -coverprofile -cpuprofile -memprofile -trace --junitxml --log-file --cache-dir") {
		outputFlags[w] = true
	}
}

// assignmentPrefix recognizes only literal shell assignment names.
var assignmentPrefix = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*=`)

// shellWord is one word of a command as the shell would read it, with quotes
// and escapes removed. dynamic says it holds an expansion, a glob or a tilde,
// which the static check does not resolve.
type shellWord struct {
	text    string
	dynamic bool
}

// simpleCommands splits command into its simple commands, in order, each as
// its words. Redirects and their targets are dropped, and so are comments.
// It stops at a subshell, a group in parentheses, a function definition or a
// here-document, where the words that follow cannot be read without running
// the shell. It performs no expansion and runs nothing.
func simpleCommands(command string) [][]shellWord {
	var cmds [][]shellWord
	var cur []shellWord
	flush := func() {
		if len(cur) > 0 {
			cmds = append(cmds, cur)
		}
		cur = nil
	}
	n := len(command)
	redirected := false // the next word is a redirect's target
	lastEnd := -1       // where the last word ended, so `2>` finds its file descriptor
	for i := 0; i < n; {
		c := command[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '\n' || c == ';':
			flush()
			redirected = false
			i++
		case c == '\\' && i+1 < n && command[i+1] == '\n':
			i += 2
		case c == '#':
			for i < n && command[i] != '\n' {
				i++
			}
		case c == '(' || c == ')':
			return cmds // the partial command in cur is dropped
		case c == '<' || c == '>' || c == '&' && i+1 < n && command[i+1] == '>':
			if c == '<' && i+1 < n && command[i+1] == '<' {
				// A here-document: its lines are not commands. The words read so
				// far are a command and are kept.
				flush()
				return cmds
			}
			if len(cur) > 0 && lastEnd == i && isDigits(cur[len(cur)-1].text) {
				cur = cur[:len(cur)-1] // the file descriptor of `2>`
			}
			i++
			for i < n && strings.ContainsRune("&>|", rune(command[i])) {
				i++
			}
			redirected = true
		case c == '&' || c == '|':
			flush()
			redirected = false
			for i < n && (command[i] == '&' || command[i] == '|') {
				i++
			}
		default:
			w, end := scanWord(command, i)
			i, lastEnd = end, end
			if redirected {
				redirected = false
				continue
			}
			cur = append(cur, w)
		}
	}
	flush()
	return cmds
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// substitutionEnd returns the index just after the parenthesis that closes the
// one at command[open], counting nested parentheses and skipping quoted ones.
// An unclosed substitution runs to the end.
func substitutionEnd(command string, open int) int {
	depth := 0
	var quote byte
	for i := open; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\\' && quote != '\'' && i+1 < len(command):
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth--; depth == 0 {
				return i + 1
			}
		}
	}
	return len(command)
}

// scanWord reads the word that starts at command[i]: quotes and escapes are
// removed, and no expansion is performed.
func scanWord(command string, i int) (shellWord, int) {
	var word strings.Builder
	var quote byte
	for ; i < len(command); i++ {
		c := command[i]
		if quote == 0 && strings.ContainsRune(" \t\r\n;&|()<>", rune(c)) {
			break
		}
		if c == '$' && quote != '\'' && i+1 < len(command) && command[i+1] == '(' {
			// A command substitution is part of the word, read to its closing
			// parenthesis; the word is dynamic and the parse goes on after it.
			end := substitutionEnd(command, i+1)
			word.WriteString(command[i:end])
			i = end - 1
			continue
		}
		if c == '\\' && quote != '\'' && i+1 < len(command) {
			next := command[i+1]
			if quote == 0 || strings.ContainsRune("$`\\\"\n", rune(next)) {
				if next != '\n' {
					word.WriteByte(next)
				}
				i++
				continue
			}
		}
		if c == '\'' || c == '"' {
			if quote == 0 {
				quote = c
				continue
			}
			if quote == c {
				quote = 0
				continue
			}
		}
		word.WriteByte(c)
	}
	text := word.String()
	return shellWord{text: text, dynamic: strings.ContainsAny(text, "*?[]{}$`") || strings.HasPrefix(text, "~")}, i
}

// CheckDir is the directory the project check command runs in (*proposed*,
// S293): the project's own folder in the worktree. The worktree is a checkout
// of the whole repository, so a project that lives in a subfolder of it (a
// monorepo) has its folder below the worktree root. git names that folder
// (`git rev-parse --show-prefix` in the project path: empty at the repository
// root, otherwise "libraries/p/"). A folder that is not in the worktree is an
// error, not a fall back to the root, where the command would run in the wrong
// place.
func (e *Engine) CheckDir(ctx context.Context, projectPath, worktree string) (string, error) {
	out, err := e.gitRead(ctx, projectPath, "rev-parse", "--show-prefix")
	if err != nil {
		return "", err
	}
	prefix := strings.TrimSuffix(out, "\n")
	if prefix == "" {
		return worktree, nil
	}
	dir := filepath.Join(worktree, filepath.FromSlash(prefix))
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("the project folder %s does not exist in the worktree", dir)
	case err != nil:
		return "", err
	case !info.IsDir():
		return "", fmt.Errorf("the project folder %s is not a directory in the worktree", dir)
	}
	return dir, nil
}

// tail keeps the last n bytes of s, so a huge test log does not fill a brief.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "[...cut...]\n" + s[len(s)-n:]
}

// ScopeWarnings returns the sorted, unique changed paths outside the Files list.
// Without a list there is no scope to judge. Tracking paths are always exempt.
func ScopeWarnings(changed, allowed []string) []string {
	if len(allowed) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	for _, path := range changed {
		if path == ".slicer" || strings.HasPrefix(path, ".slicer/") {
			continue
		}
		covered := false
		for _, entry := range allowed {
			entry = strings.TrimRight(entry, "/")
			if path == entry || strings.HasPrefix(path, entry+"/") {
				covered = true
				break
			}
		}
		if !covered {
			seen[path] = true
		}
	}
	var warnings []string
	for path := range seen {
		warnings = append(warnings, path)
	}
	sort.Strings(warnings)
	return warnings
}
