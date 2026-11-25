// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/ipc"
)

// aiGuide is the agent guide that ork ai instructions prints and ork ai skill
// builds its SKILL.md from (S233, *proposed*). It is plain text; the skill's
// front matter is added here. TestAIGuideCommandsAreInUsage keeps its command
// lines to the commands and options of the usage text.
//
//go:embed ai_instructions.md
var aiGuide string

// aiSkillFront is the YAML front matter of the skill, ahead of the guide.
const aiSkillFront = "---\n" +
	"name: ork\n" +
	"description: Drive ork from the command line. Use it to launch and steer slice runs, " +
	"answer run questions, and know which steps are the owner's alone.\n" +
	"---\n\n"

// aiUsage names the two subcommands in every usage error.
const aiUsage = "usage: ork ai instructions | ork ai skill [--output PATH | --install] [--force]"

// aiSkillDirs are the folders, under the current directory, that --install
// writes ork/SKILL.md into: Claude Code, Codex and Grok.
var aiSkillDirs = []string{".claude", ".agents", ".grok"}

// codeFileExists is the --json error code of a skill file that holds other text.
const codeFileExists = "file_exists"

// aiSkill is the SKILL.md text: the front matter, then the guide.
func aiSkill() string { return aiSkillFront + aiGuide }

// ai is `ork ai instructions` and `ork ai skill [--output PATH | --install]
// [--force]` (S233, *proposed*). Neither needs the daemon.
func (a *App) ai(_ context.Context, o *output, args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return o.usage("ai needs a subcommand; %s", aiUsage)
	}
	sub, rest := args[0], args[1:]
	flags := flag.NewFlagSet("ai "+sub, flag.ContinueOnError)
	flags.BoolVar(&o.json, "json", o.json, "print JSON")
	switch sub {
	case "instructions":
		pos, err := parse(flags, rest)
		if err != nil {
			return o.usage("ai instructions: %v", err)
		}
		if len(pos) > 0 {
			return o.usage("ai instructions takes no arguments, got %q; %s", pos[0], aiUsage)
		}
		return o.printText(aiGuide)
	case "skill":
		output := flags.String("output", "", "write the skill to this file")
		install := flags.Bool("install", false, "write the skill under .claude, .agents and .grok")
		force := flags.Bool("force", false, "replace a file that holds other text")
		pos, err := parse(flags, rest)
		if err != nil {
			return o.usage("ai skill: %v", err)
		}
		if len(pos) > 0 {
			return o.usage("ai skill takes no arguments, got %q; %s", pos[0], aiUsage)
		}
		outputGiven := false
		flags.Visit(func(f *flag.Flag) { outputGiven = outputGiven || f.Name == "output" })
		switch {
		case outputGiven && *output == "":
			return o.usage("ai skill --output needs a path; %s", aiUsage)
		case outputGiven && *install:
			return o.usage("ai skill: --output and --install cannot be used together; %s", aiUsage)
		case *force && !outputGiven && !*install:
			return o.usage("ai skill: --force needs --output or --install; %s", aiUsage)
		case outputGiven:
			return o.writeSkill([]string{*output}, *force)
		case *install:
			wd, err := a.getwd()
			if err != nil {
				return o.fail(ExitFailure, ipc.CodeInternal, err.Error())
			}
			var paths []string
			for _, d := range aiSkillDirs {
				paths = append(paths, filepath.Join(wd, d, "skills", "ork", "SKILL.md"))
			}
			return o.writeSkill(paths, *force)
		}
		return o.printText(aiSkill())
	}
	return o.usage("unknown ai subcommand %q; %s", sub, aiUsage)
}

// getwd is the directory --install writes under.
func (a *App) getwd() (string, error) {
	if a.Getwd != nil {
		return a.Getwd()
	}
	return os.Getwd()
}

// printText prints text as it is, or as {"text": ...} with --json.
func (o *output) printText(text string) int {
	if o.json {
		return o.printJSON(map[string]string{"text": text})
	}
	fmt.Fprint(o.stdout, text)
	return ExitOK
}

// writeSkill writes the SKILL.md text to each path. A path that already holds
// the text is left alone, and one that holds other text is kept and refused
// unless force is set. It checks every path before it writes any, so a refusal
// writes nothing.
func (o *output) writeSkill(paths []string, force bool) int {
	text := aiSkill()
	var write, same, refused []string
	for _, p := range paths {
		old, err := os.ReadFile(p)
		switch {
		case err == nil && string(old) == text:
			same = append(same, p)
		case err == nil && !force:
			refused = append(refused, p)
		case err == nil || errors.Is(err, fs.ErrNotExist):
			write = append(write, p)
		default:
			return o.fail(ExitFailure, ipc.CodeInternal, err.Error())
		}
	}
	if len(refused) > 0 {
		return o.fail(ExitFailure, codeFileExists,
			fmt.Sprintf("%s holds different text; --force replaces it", strings.Join(refused, ", ")))
	}
	for _, p := range write {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return o.fail(ExitFailure, ipc.CodeInternal, err.Error())
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			return o.fail(ExitFailure, ipc.CodeInternal, err.Error())
		}
	}
	if o.json {
		res := map[string][]string{"written": append([]string{}, write...)}
		if len(same) > 0 {
			res["unchanged"] = same
		}
		return o.printJSON(res)
	}
	for _, p := range paths {
		if slices.Contains(same, p) {
			fmt.Fprintf(o.stdout, "%s (unchanged)\n", p)
			continue
		}
		fmt.Fprintln(o.stdout, p)
	}
	return ExitOK
}
