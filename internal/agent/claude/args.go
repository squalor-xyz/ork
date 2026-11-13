// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
)

// localDisallowedTools are the tools a relay role on claude-local never uses.
// "Task" is the name the stream's init event lists (testdata/vendors/claude);
// "Agent" is its newer name.
var localDisallowedTools = []string{"WebFetch", "WebSearch", "Task", "Agent", "NotebookEdit", "Skill"}

// Args returns the arguments after the binary. The shape follows the
// measured commands (testdata/vendors/claude/*.cmd) and `claude --help`:
//
//	-p [--resume SESSION] --output-format stream-json --verbose
//	   --include-partial-messages [--model M] [--effort E]
//	   [--disallowedTools a,b [--disable-slash-commands]]   (claude-local only)
//	   (--allowedTools a,b --permission-prompts none | --permission-mode plan)
//	   PROMPT
//
// A write role needs an allowlist. A cloud read-only role runs in plan mode
// and ignores it; claude-local uses a read-only allowlist to avoid the
// model-backed classifier (adj 307). `--include-partial-messages` gives the
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
	// A local role starts with less fixed context (S360, *proposed*): the tools
	// and commands it never uses are turned off. The flags come before
	// --allowedTools because --disallowedTools takes a variadic value list.
	if s.Bin == "claude-local" {
		denied := localDisallowedTools
		if s.KeepSkills {
			denied = slices.DeleteFunc(slices.Clone(denied), func(t string) bool { return t == "Skill" })
		}
		args = append(args, "--disallowedTools", strings.Join(denied, ","))
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
	return append(args, s.Prompt), nil
}
