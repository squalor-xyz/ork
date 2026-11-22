// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/roles"
)

// roles prints the embedded role prompts (print, which never needs the daemon)
// or shows a project's role matrix (matrix).
func (a *App) roles(ctx context.Context, o *output, args []string) int {
	if len(args) > 0 && args[0] == "matrix" {
		return a.rolesMatrix(ctx, o, args[1:])
	}
	if len(args) == 0 || args[0] != "print" {
		return o.usage("roles needs the subcommand print or matrix")
	}
	fs := flag.NewFlagSet("roles print", flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args[1:])
	if err != nil {
		return o.usage("roles print: %v", err)
	}
	if len(pos) > 1 {
		return o.usage("roles print takes at most one ROLE, got %d arguments", len(pos))
	}
	names := roles.Names()
	if len(pos) == 1 {
		names = []string{pos[0]}
	}
	type entry struct {
		Name   string `json:"name"`
		Prompt string `json:"prompt"`
	}
	var entries []entry
	for _, name := range names {
		prompt, err := roles.Prompt(name)
		if err != nil {
			return o.usage("%v (roles: %s)", err, strings.Join(roles.Names(), ", "))
		}
		entries = append(entries, entry{name, prompt})
	}
	if o.json {
		return o.printJSON(map[string]any{"roles": entries})
	}
	for i, e := range entries {
		if len(pos) == 0 { // all four: a header line before each prompt
			if i > 0 {
				fmt.Fprintln(o.stdout)
			}
			fmt.Fprintf(o.stdout, "==== %s ====\n", e.Name)
		}
		fmt.Fprint(o.stdout, e.Prompt)
		if !strings.HasSuffix(e.Prompt, "\n") {
			fmt.Fprintln(o.stdout)
		}
	}
	return ExitOK
}

// rolesMatrix shows the role matrix of a project: slicer's text, then which
// rows ork uses. It reads through the daemon and changes nothing.
func (a *App) rolesMatrix(ctx context.Context, o *output, args []string) int {
	if len(args) > 0 && args[0] == "apply" {
		return a.rolesMatrixApply(ctx, o, args[1:])
	}
	fs := flag.NewFlagSet("roles matrix", flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("roles matrix: %v", err)
	}
	if len(pos) != 1 {
		return o.usage("roles matrix takes PROJECT, got %d arguments", len(pos))
	}
	var out daemon.RolesMatrixResult
	if code := a.call(ctx, o, daemon.MethodRolesMatrix, daemon.RolesMatrixParams{Project: pos[0]}, &out); code != ExitOK {
		return code
	}
	if o.json {
		return o.printJSON(out)
	}
	for _, line := range daemon.FormatMatrix(&out) {
		fmt.Fprintln(o.stdout, line)
	}
	return ExitOK
}

// rolesMatrixApply forwards a file or stdin to the daemon as slicer's set.
func (a *App) rolesMatrixApply(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("roles matrix apply", flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	file := fs.String("file", "", "read change set from PATH")
	stdin := fs.Bool("stdin", false, "read change set from stdin")
	dry := fs.Bool("dry-run", false, "validate without writing")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("roles matrix apply: %v", err)
	}
	fileGiven := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "file" {
			fileGiven = true
		}
	})
	if len(pos) != 1 || fileGiven == *stdin || fileGiven && *file == "" {
		return o.usage("roles matrix apply needs PROJECT and exactly one of --file PATH or --stdin")
	}
	var set []byte
	if *stdin {
		input := a.Input
		if input == nil {
			input = os.Stdin
		}
		set, err = io.ReadAll(input)
	} else {
		set, err = os.ReadFile(*file)
	}
	if err != nil {
		return o.usage("roles matrix apply: %v", err)
	}
	if !json.Valid(set) {
		return o.usage("roles matrix apply: change set must be valid JSON")
	}
	var out daemon.MatrixApplyResult
	if code := a.call(ctx, o, daemon.MethodMatrixApply, daemon.MatrixApplyParams{Project: pos[0], Set: set, DryRun: *dry}, &out); code != ExitOK {
		return code
	}
	if o.json {
		return o.printJSON(out)
	}
	var result struct {
		Steps []struct {
			Op      string `json:"op"`
			ID      string `json:"id"`
			Changed bool   `json:"changed"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(out.Slicer, &result); err != nil {
		return o.usage("roles matrix apply: %v", err)
	}
	if *dry {
		fmt.Fprintln(o.stdout, "dry run: nothing written")
	}
	if len(result.Steps) == 0 {
		fmt.Fprintln(o.stdout, "no steps: matrix unchanged")
	}
	for _, step := range result.Steps {
		state := "unchanged"
		if step.Changed {
			state = "changed"
		}
		fmt.Fprintf(o.stdout, "%s %s: %s\n", step.Op, step.ID, state)
	}
	for _, warning := range out.Warnings {
		fmt.Fprintln(o.stdout, warning)
	}
	return ExitOK
}
