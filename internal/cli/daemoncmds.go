// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// status prints each project's needs-you count and active relays.
func (a *App) status(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	project := fs.String("project", "", "show only this project")
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("status: %v", err)
	}
	if len(pos) > 0 {
		return o.usage("status takes no arguments, got %q", pos[0])
	}
	return a.withDaemon(ctx, o, func(c *daemon.Client) int {
		res, err := c.Status(ctx, *project)
		if err != nil {
			return o.failErr(err)
		}
		if o.json {
			return o.printJSON(res)
		}
		if len(res.Projects) == 0 {
			fmt.Fprintln(o.stdout, "no projects registered")
			return ExitOK
		}
		for _, p := range res.Projects {
			fmt.Fprintf(o.stdout, "%s: needs you %d, active relays %d\n", p.Name, p.NeedsYou, len(p.ActiveRelays))
			for _, r := range p.ActiveRelays {
				items := strings.Join(r.Items, ",")
				if items == "" {
					items = "-"
				}
				fmt.Fprintf(o.stdout, "  %s %s %s (%d steps, %d running)\n", r.ID, items, r.State, r.Steps, r.Running)
			}
			for _, e := range p.Errors {
				fmt.Fprintf(o.stdout, "  error %s: %s (%s)\n", e.Part, e.Message, e.Code)
			}
		}
		return ExitOK
	})
}

// projects dispatches the projects subcommands.
func (a *App) projects(ctx context.Context, o *output, args []string) int {
	if len(args) == 0 {
		return o.usage("projects needs add, list or rm")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("projects "+sub, flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	switch sub {
	case "list":
		pos, err := parse(fs, rest)
		if err != nil {
			return o.usage("projects list: %v", err)
		}
		if len(pos) > 0 {
			return o.usage("projects list takes no arguments, got %q", pos[0])
		}
		return a.withDaemon(ctx, o, func(c *daemon.Client) int {
			list, err := c.Projects(ctx)
			if err != nil {
				return o.failErr(err)
			}
			if o.json {
				return o.printJSON(daemon.ProjectsResult{Projects: list})
			}
			tw := tabwriter.NewWriter(o.stdout, 0, 0, 2, ' ', 0)
			for _, p := range list {
				if p.Adj {
					fmt.Fprintf(tw, "%s\t%s\tadj\n", p.Name, p.Path)
				} else {
					fmt.Fprintf(tw, "%s\t%s\n", p.Name, p.Path)
				}
			}
			tw.Flush()
			return ExitOK
		})
	case "add":
		name := fs.String("name", "", "the project's name; default is the path's base name")
		pos, err := parse(fs, rest)
		if err != nil {
			return o.usage("projects add: %v", err)
		}
		if len(pos) != 1 {
			return o.usage("projects add takes one PATH, got %d arguments", len(pos))
		}
		if !filepath.IsAbs(pos[0]) {
			return o.usage("path %q is not absolute", pos[0])
		}
		return a.withDaemon(ctx, o, func(c *daemon.Client) int {
			p, err := c.AddProject(ctx, pos[0], *name)
			if err != nil {
				return o.failErr(err)
			}
			if o.json {
				return o.printJSON(map[string]any{"project": p})
			}
			adj := ""
			if p.Adj {
				adj = " (adj)"
			}
			fmt.Fprintf(o.stdout, "added %s %s%s\n", p.Name, p.Path, adj)
			return ExitOK
		})
	case "rm":
		pos, err := parse(fs, rest)
		if err != nil {
			return o.usage("projects rm: %v", err)
		}
		if len(pos) != 1 {
			return o.usage("projects rm takes one NAME, got %d arguments", len(pos))
		}
		return a.withDaemon(ctx, o, func(c *daemon.Client) int {
			if err := c.RemoveProject(ctx, pos[0]); err != nil {
				return o.failErr(err)
			}
			if o.json {
				return o.printJSON(map[string]any{"removed": pos[0]})
			}
			fmt.Fprintf(o.stdout, "removed %s\n", pos[0])
			return ExitOK
		})
	}
	return o.usage("unknown projects command %q", sub)
}
