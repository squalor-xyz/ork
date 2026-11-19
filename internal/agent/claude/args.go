// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
)

// localTools are the built-in tools a relay role on claude-local is sent
// (S364, adj 352, *proposed*). A write role gets six. A read-only role loses
// Edit and Write unless it holds the progress-note grant (AddDir): that grant
// is an Edit rule the approval hook applies to Edit and Write. A UI pass loads
// skills, so it keeps Skill.
func (s Spec) localTools() []string {
	tools := []string{"Read", "Grep", "Glob"}
	if !s.ReadOnly || s.AddDir != "" {
		tools = append(tools, "Edit", "Write")
	}
	tools = append(tools, "Bash")
	if s.KeepSkills {
		tools = append(tools, "Skill")
	}
	return tools
}

// patternEscape makes a path literal in a rule pattern (gitignore syntax).
var patternEscape = strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`, "]", `\]`, "!", `\!`)

// editRules are the Edit and Write allow rules for everything under an
// absolute directory. The leading // makes the path absolute in a rule.
func editRules(dir string) []string {
	path := "//" + patternEscape.Replace(strings.TrimPrefix(filepath.ToSlash(dir), "/")) + "/**"
	return []string{"Edit(" + path + ")", "Write(" + path + ")"}
}

// Args returns the arguments after the binary. The shape follows the
// measured commands (testdata/vendors/claude/*.cmd) and `claude --help`:
//
//	-p [--resume SESSION] --output-format stream-json --verbose
//	   --include-partial-messages [--model M] [--effort E]
//	   [--system-prompt-file F --tools a,b [--disable-slash-commands]]   (claude-local only)
//	   (--allowedTools a,b --permission-prompts none | --permission-mode plan |
//	    --add-dir DIR --allowedTools a,b,Edit(//DIR/**),Write(//DIR/**) --permission-prompts none)
//	   [--permission-mode manual --settings JSON]          (claude-local only)
//	   PROMPT
//
// A write role needs an allowlist. A cloud read-only role runs in plan mode
// and ignores it (except a UI pass with a WriteDir, which uses it and has no
// mode flag); claude-local uses manual mode and ork's PreToolUse hook
// with a read-only allowlist for read-only roles (adj 307, 328-331).
// `--include-partial-messages` gives the
// text deltas for the output tail. `--effort <level>` is listed by
// `claude --help` ("Effort level
// for the current session", claude 2.1.x). The allowlist is one comma-joined
// value, one of the two forms `--help` documents; an entry holding a comma
// is refused rather than split wrongly.
func (s Spec) Args() ([]string, error) {
	if strings.TrimSpace(s.Prompt) == "" {
		return nil, errors.New("claude: empty prompt")
	}
	if strings.HasPrefix(s.Prompt, "-") {
		return nil, errors.New("claude: prompt starts with '-' and would read as a flag")
	}
	if strings.HasPrefix(s.Resume, "-") {
		return nil, errors.New("claude: resume id starts with '-'")
	}
	args := []string{"-p"}
	if s.Resume != "" {
		args = append(args, "--resume", s.Resume)
	}
	args = append(args, "--output-format", "stream-json", "--verbose", "--include-partial-messages")
	if s.Model != "" {
		args = append(args, "--model", s.Model)
	}
	if s.Effort != "" {
		args = append(args, "--effort", s.Effort)
	}
	// A local role starts with less fixed context (S360, S364, *proposed*):
	// ork's own short system prompt replaces the default one, only the tools
	// it uses are sent, and slash commands are off. The flags come before
	// --allowedTools because --tools takes a variadic value list.
	if s.Bin == "claude-local" {
		if s.SystemPromptFile != "" {
			if !filepath.IsAbs(s.SystemPromptFile) {
				return nil, errors.New("claude: system prompt file is not absolute")
			}
			args = append(args, "--system-prompt-file", s.SystemPromptFile)
		}
		args = append(args, "--tools", strings.Join(s.localTools(), ","))
		if !s.KeepSkills {
			args = append(args, "--disable-slash-commands")
		}
		if s.AddDir != "" {
			if !filepath.IsAbs(s.AddDir) {
				return nil, errors.New("claude: progress directory is not absolute")
			}
			args = append(args, "--add-dir", s.AddDir)
		}
	}
	cloudReadOnly := s.ReadOnly && s.Bin != "claude-local"
	if s.TMPDir != "" && (!s.ReadOnly || s.WriteDir != "") {
		if !filepath.IsAbs(s.TMPDir) || filepath.Clean(s.TMPDir) != s.TMPDir {
			return nil, errors.New("claude: TMPDIR is not an absolute clean path")
		}
		args = append(args, "--add-dir", s.TMPDir)
	}
	if cloudReadOnly && s.WriteDir == "" {
		args = append(args, "--permission-mode", "plan")
	} else {
		allow := s.Allowlist
		if cloudReadOnly {
			// A cloud UI pass (S369, adj 372, *proposed*). Plan mode refuses
			// every write, even under an allow rule (probe, claude 2.1,
			// 2026-11-16), and accept-edits mode auto-approves shell file
			// commands in the worktree (adj 371). So it has no permission
			// mode flag: the allowlist (the caller's read-only tools plus
			// Edit and Write under the write directory) and
			// --permission-prompts none deny every other call. --add-dir
			// matches the probe and the claude-local progress note, and comes
			// before --allowedTools like there.
			if !filepath.IsAbs(s.WriteDir) || filepath.Clean(s.WriteDir) != s.WriteDir {
				return nil, errors.New("claude: write directory is not an absolute clean path")
			}
			args = append(args, "--add-dir", s.WriteDir)
			allow = append(slices.Clone(allow), editRules(s.WriteDir)...)
			if s.TMPDir != "" {
				allow = append(allow, editRules(s.TMPDir)...)
			}
		} else if len(allow) == 0 {
			return nil, errors.New("claude: a write role needs an allowlist")
		}
		for _, tool := range allow {
			if strings.TrimSpace(tool) == "" {
				return nil, errors.New("claude: empty allowlist entry")
			}
			if strings.Contains(tool, ",") {
				return nil, errors.New("claude: allowlist entry " + tool + " holds a comma")
			}
		}
		args = append(args, "--allowedTools", strings.Join(allow, ","), "--permission-prompts", "none")
	}
	if s.Bin == "claude-local" {
		args = append(args, "--permission-mode", "manual", "--settings", HookSettings(s.HookCommand))
	}
	return append(args, s.Prompt), nil
}
