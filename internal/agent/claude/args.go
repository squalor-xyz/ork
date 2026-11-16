// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"errors"
	"path/filepath"
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

// Args returns the arguments after the binary. The shape follows the
// measured commands (testdata/vendors/claude/*.cmd) and `claude --help`:
//
//	-p [--resume SESSION] --output-format stream-json --verbose
//	   --include-partial-messages [--model M] [--effort E]
//	   [--system-prompt-file F --tools a,b [--disable-slash-commands]]   (claude-local only)
//	   (--allowedTools a,b --permission-prompts none | --permission-mode plan)
//	   [--permission-mode manual --settings JSON]          (claude-local only)
//	   PROMPT
//
// A write role needs an allowlist. A cloud read-only role runs in plan mode
// and ignores it; claude-local uses manual mode and ork's PreToolUse hook
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
	if s.ReadOnly && s.Bin != "claude-local" {
		args = append(args, "--permission-mode", "plan")
	} else {
		if len(s.Allowlist) == 0 {
			return nil, errors.New("claude: a write role needs an allowlist")
		}
		for _, tool := range s.Allowlist {
			if strings.TrimSpace(tool) == "" {
				return nil, errors.New("claude: empty allowlist entry")
			}
			if strings.Contains(tool, ",") {
				return nil, errors.New("claude: allowlist entry " + tool + " holds a comma")
			}
		}
		args = append(args, "--allowedTools", strings.Join(s.Allowlist, ","), "--permission-prompts", "none")
	}
	if s.Bin == "claude-local" {
		args = append(args, "--permission-mode", "manual", "--settings", HookSettings(s.HookCommand))
	}
	return append(args, s.Prompt), nil
}
