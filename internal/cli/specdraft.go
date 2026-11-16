// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// spec is the ork spec command group (S179, cli.spec-draft, adj 393). It has one
// command, draft.
func (a *App) spec(ctx context.Context, o *output, args []string) int {
	if len(args) == 0 {
		return o.usage("spec: takes draft")
	}
	switch args[0] {
	case "draft":
		return a.specDraft(ctx, o, args[1:])
	default:
		return o.usage("spec: unknown command %q", args[0])
	}
}

// terminal reports whether an owner is at a terminal to answer: the Terminal
// hook when it is set; input given to the App is the owner's by definition; else
// the standard input, which must be a terminal device (not /dev/null, a file or
// a pipe).
func (a *App) terminal() bool {
	if a.Terminal != nil {
		return a.Terminal()
	}
	if a.Input != nil {
		return true
	}
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(info, null) {
		return false
	}
	return true
}

// specDraft is ork spec draft PROJECT ID [--json]. It drafts the item's spec
// sections with spec.draft, which writes nothing, and then prints each section's
// current text and its new text. For each section it can apply it asks y or n,
// and only a y calls spec.draft-apply, for that section alone (the same call
// the TUI makes). It needs a terminal to ask on; with --json it prints the draft
// and asks and applies nothing.
func (a *App) specDraft(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("spec draft", flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print the draft as JSON; apply nothing")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("spec draft: %v", err)
	}
	if len(pos) != 2 {
		return o.usage("spec draft takes PROJECT ID")
	}
	project, item := pos[0], pos[1]
	if !o.json && !a.terminal() {
		return o.fail(ExitFailure, daemon.CodeRefused, "spec draft needs a terminal to approve each section; run it in one, or use --json to print the draft and apply nothing")
	}
	if a.Call == nil {
		c, err := a.connect(ctx)
		if err != nil {
			return o.fail(ExitFailure, codeDaemon, "No daemon connection; action unavailable")
		}
		c.Close()
	}
	var draft daemon.SpecDraftResult
	if code := a.callWith(ctx, o, "spec draft failed: ", daemon.MethodSpecDraft, daemon.SpecDraftParams{Project: project, Item: item}, &draft); code != ExitOK {
		return code
	}
	if o.json {
		return o.printJSON(draft)
	}
	input := a.Input
	if input == nil {
		input = os.Stdin
	}
	reader := bufio.NewReader(input)
	w := o.stdout
	fmt.Fprintf(w, "Spec draft of %s (nothing is written until you answer y):\n", plainLine(draft.Item))
	applied := 0
	for _, s := range draft.Sections {
		fmt.Fprintf(w, "\n== %s ==\n", plainLine(s.Name))
		if s.Unchanged {
			fmt.Fprintln(w, "unchanged")
			continue
		}
		current := strings.TrimSpace(s.Current)
		if current == "" {
			current = "(empty)"
		}
		fmt.Fprintf(w, "Current:\n%s\nNew:\n%s\n", indent(current, "  "), indent(s.Text, "  "))
		for _, p := range s.Problems {
			fmt.Fprintf(w, "Problem: %s\n", plainLine(p))
		}
		if len(s.Problems) > 0 {
			fmt.Fprintf(w, "%s has a problem and cannot be applied.\n", plainLine(s.Name))
			continue
		}
		fmt.Fprintf(w, "Run: %s\nApply %s? [y/N] ", plainLine(s.Command), plainLine(s.Name))
		line, err := readLine(reader, w)
		if err != nil && err != io.EOF {
			return o.failErr(err)
		}
		if yesNo(strings.TrimSpace(line)) != "yes" {
			fmt.Fprintf(w, "Skipped %s\n", plainLine(s.Name))
			continue
		}
		var out daemon.SpecDraftApplyResult
		if code := a.callWith(ctx, o, "spec draft failed: ", daemon.MethodSpecDraftApply, daemon.SpecDraftApplyParams{Project: project, Item: item, Sections: []string{s.Name}}, &out); code != ExitOK {
			return code
		}
		applied++
		fmt.Fprintf(w, "Applied %s (uncommitted)\n", plainLine(s.Name))
	}
	if text := strings.TrimSpace(draft.OwnerNeeded); text != "" {
		fmt.Fprintf(w, "\nOwner needed:\n%s\n", indent(text, "  "))
	}
	fmt.Fprintf(w, "\n%d of %d sections applied\n", applied, len(draft.Sections))
	return ExitOK
}
