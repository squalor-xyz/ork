// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

type ToolCall struct {
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	TranscriptPath string          `json:"transcript_path"`
	SessionID      string          `json:"session_id"`
	CWD            string          `json:"cwd"`
}
type Approval struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}
type ApprovalPolicy struct {
	Worktree, Scratch, AddDir, ToolResults string
	TempDir, ReportFile                    string
	Allowlist                              []string
}

// ApprovalDetail previews a call for the owner. Denial log details retain
// their command/path-only contract; other approval inputs use compact JSON
// with the same control-character handling and 200-rune cap.
func (c ToolCall) ApprovalDetail() string {
	d := Denial{ToolInput: c.ToolInput}
	if detail := d.Detail(); detail != "" {
		return detail
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, c.ToolInput); err != nil {
		return ""
	}
	encoded, _ := json.Marshal(map[string]string{"command": compact.String()})
	return (Denial{ToolInput: encoded}).Detail()
}

// Evaluate applies hard path/read rules before consulting the role allowlist.
// An ask is resolved by the daemon, never by Claude's permission classifier.
func (p ApprovalPolicy) Evaluate(call ToolCall) Approval {
	var in struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Pattern  string `json:"pattern"`
		Command  string `json:"command"`
		Limit    *int   `json:"limit"`
		Offset   *int   `json:"offset"`
	}
	deny := func(reason string) Approval { return Approval{"deny", reason} }
	if err := json.Unmarshal(call.ToolInput, &in); err != nil {
		return deny("invalid tool input: " + err.Error())
	}
	outside := func(path string) *Approval {
		if path != "" && !p.contains(path) {
			a := deny(fmt.Sprintf("path %q is outside worktree %s and its permitted directories", path, p.Worktree))
			return &a
		}
		return nil
	}
	for _, path := range []string{in.FilePath, in.Path} {
		if a := outside(path); a != nil {
			return *a
		}
	}
	if call.ToolName == "Glob" && (filepath.IsAbs(in.Pattern) || strings.Contains(in.Pattern, "..")) {
		if a := outside(in.Pattern); a != nil {
			return *a
		}
	}
	if call.CWD != "" && !p.contains(call.CWD) {
		return deny("tool cwd is outside worktree " + p.Worktree)
	}
	// This parser validates every path itself before the general Bash scan.
	if call.ToolName == "Bash" && p.readOnlyOwnFiles(in.Command) {
		return Approval{"allow", "read-only on own files"}
	}
	if call.ToolName == "Bash" {
		tokens, _, valid := scanShell(in.Command)
		expansion := false
		for _, word := range tokens {
			expansion = expansion || word.expansion
			token := word.text
			// Flags such as --root=/path also name a path. ./... is a Go package
			// pattern rather than a parent component and remains usable.
			path := token
			if _, after, ok := strings.Cut(path, "="); ok {
				path = after
			}
			if filepath.IsAbs(path) || strings.Contains(strings.ReplaceAll(path, "...", ""), "..") {
				if !p.contains(path) {
					return deny(fmt.Sprintf("Bash token %q is outside worktree %s and its permitted directories", token, p.Worktree))
				}
			}
		}
		// A known outside path always denies, even when another word needs review.
		if !valid {
			return Approval{"ask", "Bash quoting is incomplete"}
		}
		if expansion {
			return Approval{"ask", "Bash tilde or brace expansion requires owner approval"}
		}
	}
	if call.ToolName == "Read" {
		if in.Limit != nil && (*in.Limit < 1 || *in.Limit > 500) {
			return deny("Read limit must be 1 to 500; Grep first, then Read with offset and limit (at most 500 lines)")
		}
		if in.Offset != nil && *in.Offset < 1 {
			return deny("Read offset must be positive")
		}
		if in.Limit == nil {
			path := in.FilePath
			if !filepath.IsAbs(path) {
				path = filepath.Join(p.Worktree, path)
			}
			n, err := countLines(path)
			if err != nil && !os.IsNotExist(err) {
				return deny("cannot count Read lines: " + err.Error())
			}
			if n > 500 {
				return deny(fmt.Sprintf("this file has %d lines; Grep first, then Read with offset and limit (at most 500 lines)", n))
			}
		}
	}
	for _, rule := range p.Allowlist {
		if rule == call.ToolName {
			return Approval{"allow", "role allowlist"}
		}
		if strings.HasPrefix(rule, "Bash(") && strings.HasSuffix(rule, ")") && call.ToolName == "Bash" {
			pattern := rule[5 : len(rule)-1]
			if wildcard(pattern, in.Command) && shellOperators(pattern) == shellOperators(in.Command) {
				return Approval{"allow", "role allowlist"}
			}
		}
		if strings.HasPrefix(rule, "Edit(") && strings.HasSuffix(rule, ")") && (call.ToolName == "Edit" || call.ToolName == "Write") {
			// The runner's S358 grant is one escaped absolute file, not a glob.
			if in.FilePath == progressPath(rule) {
				return Approval{"allow", "exact progress file"}
			}
		}
	}
	return Approval{"ask", "call is not covered by the role allowlist"}
}

// progressPath decodes the runner's exact, escaped Edit grant (also for Write).
func progressPath(rule string) string {
	if !strings.HasPrefix(rule, "Edit(/") || !strings.HasSuffix(rule, ")") {
		return ""
	}
	path := strings.TrimPrefix(rule[5:len(rule)-1], "/")
	return strings.NewReplacer(`\\`, `\`, `\*`, "*", `\?`, "?", `\[`, "[", `\]`, "]", `\!`, "!").Replace(path)
}

var ownReadTokens = regexp.MustCompile(`"[^"\n\r]*"|'[^'\n\r]*'|[^\s"']+`)

func (p ApprovalPolicy) readOnlyOwnFiles(command string) bool {
	// Accept only simple reads. Never interpret shell syntax, expansions or
	// escapes; an unrecognized form falls through to the role allowlist.
	if shellOperators(command) != "" || strings.ContainsAny(command, "\\*?[]{}") {
		return false
	}
	var tokens []string
	end := 0
	for _, span := range ownReadTokens.FindAllStringIndex(command, -1) {
		if strings.TrimSpace(command[end:span[0]]) != "" || (end > 0 && span[0] == end) {
			return false
		}
		token := command[span[0]:span[1]]
		if token[0] == '\'' || token[0] == '"' {
			token = token[1 : len(token)-1]
		}
		tokens = append(tokens, token)
		end = span[1]
	}
	if strings.TrimSpace(command[end:]) != "" || len(tokens) < 2 {
		return false
	}
	switch tokens[0] {
	case "ls", "wc", "head", "tail", "cat", "stat":
	default:
		return false
	}
	paths := tokens[1:]
	if (tokens[0] == "ls" && paths[0] == "-la") || (tokens[0] == "wc" && paths[0] == "-l") {
		paths = paths[1:]
	}
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return false
		}
		path = resolveAncestor(path)
		own := p.ReportFile != "" && path == resolveAncestor(p.ReportFile)
		if p.TempDir != "" {
			rel, err := filepath.Rel(resolveAncestor(p.TempDir), path)
			own = own || (err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
		}
		for _, rule := range p.Allowlist {
			if progress := progressPath(rule); progress != "" && path == resolveAncestor(progress) {
				own = true
			}
		}
		if !own {
			return false
		}
	}
	return true
}

func wildcard(pattern, text string) bool {
	expression := regexp.QuoteMeta(pattern)
	expression = strings.ReplaceAll(expression, `\*`, ".*")
	matched, _ := regexp.MatchString("^"+expression+"$", text)
	return matched
}

func shellOperators(command string) string {
	_, operators, _ := scanShell(command)
	return operators
}

type shellWord struct {
	text      string
	expansion bool
}

// scanShell shares quote and escape handling between path and operator checks.
// It does not execute expansions. Unquoted tilde and brace forms require review.
func scanShell(command string) ([]shellWord, string, bool) {
	var words []shellWord
	var word, operators, braces strings.Builder
	var quote rune
	expansion := false
	flush := func() {
		if word.Len() > 0 {
			brace := braces.String()
			words = append(words, shellWord{word.String(), expansion || (strings.Contains(brace, "{") && strings.Contains(brace, ","))})
		}
		word.Reset()
		braces.Reset()
		expansion = false
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote == '\'' {
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '\\' {
			if i+1 == len(runes) {
				flush()
				return words, operators.String(), false
			}
			next := runes[i+1]
			if quote == 0 || strings.ContainsRune("$`\"\\\n", next) {
				i++
				if next != '\n' {
					word.WriteRune(next)
				}
				continue
			}
		}
		if quote == '"' {
			if r == '"' {
				quote = 0
			} else {
				word.WriteRune(r)
				if r == '$' || r == '`' {
					operators.WriteRune(r)
				}
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if strings.ContainsRune(";&|<>()`$\n\r", r) {
			operators.WriteRune(r)
			flush()
		} else if unicode.IsSpace(r) {
			flush()
		} else {
			if r == '~' && (word.Len() == 0 || strings.HasSuffix(word.String(), "=")) {
				expansion = true
			}
			if strings.ContainsRune("{,}", r) {
				braces.WriteRune(r)
			}
			word.WriteRune(r)
		}
	}
	flush()
	return words, operators.String(), quote == 0
}

// contains resolves symlinked ancestors, including for a not-yet-created file.
func (p ApprovalPolicy) contains(path string) bool {
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.Worktree, path)
	}
	path = resolveAncestor(path)
	if p.ReportFile != "" && path == resolveAncestor(p.ReportFile) {
		return true
	}
	for _, rule := range p.Allowlist {
		if progress := progressPath(rule); progress != "" && path == resolveAncestor(progress) {
			return true
		}
	}
	for _, root := range []string{p.Worktree, p.Scratch, p.ToolResults, p.TempDir} {
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(resolveAncestor(root), path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func resolveAncestor(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(resolveAncestor(parent), filepath.Base(path))
}

func countLines(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("not a regular file")
	}
	reader := bufio.NewReader(f)
	n := 0
	for {
		line, err := reader.ReadSlice('\n')
		if err == nil {
			n++
			continue
		}
		if err == bufio.ErrBufferFull {
			for err == bufio.ErrBufferFull {
				_, err = reader.ReadSlice('\n')
			}
			if err == nil {
				n++
				continue
			}
		}
		if err == io.EOF {
			if len(line) > 0 {
				n++
			}
			return n, nil
		}
		return 0, err
	}
}

// HookSettings uses the documented PreToolUse command hook schema.
// The process deadline is shorter than Claude's timeout so timeout prints deny.
func HookSettings(command string) string {
	if command == "" {
		command = "ork hook pretool"
	}
	settings := map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"matcher": ".*", "hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 1860, "onFailure": "block"}}}}}}
	b, _ := json.Marshal(settings)
	return string(b)
}
