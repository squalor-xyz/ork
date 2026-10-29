// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"
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
