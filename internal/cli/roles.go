// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/roles"
)

// roles prints the embedded role prompts. It never needs the daemon.
func (a *App) roles(o *output, args []string) int {
	if len(args) == 0 || args[0] != "print" {
		return o.usage("roles needs the subcommand print")
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
