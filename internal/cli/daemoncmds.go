// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/source"
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
	var res daemon.StatusResult
	if code := a.call(ctx, o, daemon.MethodStatus, daemon.StatusParams{Project: *project}, &res); code != 0 {
		return code
	}
	if o.json {
		return o.printJSON(res)
	}
	if len(res.Projects) == 0 {
		fmt.Fprintln(o.stdout, daemon.NoProjectsGuide)
		return ExitOK
	}
	for _, p := range res.Projects {
		name := p.Name
		if p.Unavailable {
			name += " (removed)"
		}
		fmt.Fprintf(o.stdout, "%s: needs you %d, active relays %d\n", name, p.NeedsYou, len(p.ActiveRelays))
		for _, r := range p.Open {
			fmt.Fprintf(o.stdout, "  #%d %s  %s\n", r.ID, r.Key, r.Title)
		}
		for _, it := range p.Held {
			fmt.Fprintf(o.stdout, "  %s  %s  (needs-owner)\n", it.ID, it.Title)
		}
		for _, r := range p.ActiveRelays {
			items := strings.Join(r.Items, ",")
			if items == "" {
				items = "-"
			}
			fmt.Fprintf(o.stdout, "  %s %s %s (%d steps, %d running)\n", r.ID, items, r.State, r.Steps, r.Running)
			if step := r.RunningStep; step != nil {
				// Say what the Running row says: no "<id> <role>" label (S150).
				running := *step
				running.Task = daemon.StripTaskLabel(running.Task, r.Items, running.Role)
				fmt.Fprintf(o.stdout, "    %s\n", daemon.FormatRelayStep("running", running))
			}
			if step := r.FailedStep; step != nil {
				fmt.Fprintf(o.stdout, "    %s\n", daemon.FormatRelayStep("failed", *step))
			}
		}
		for _, section := range []source.Section{source.SectionNeeds, source.SectionRunning} {
			for _, r := range p.EngineRuns {
				if r.Section(p.EngineRuns) != section {
					continue
				}
				fmt.Fprintf(o.stdout, "  %s %s %s", r.ID, r.Item, r.State)
				if r.Wait != "" {
					fmt.Fprintf(o.stdout, " (wait: %s)", r.Wait)
				}
				if len(r.Denials) > 0 {
					fmt.Fprintf(o.stdout, " (denied: %s)", source.DenialsText(r.Denials))
				}
				fmt.Fprintln(o.stdout)
				if r.Error != "" {
					fmt.Fprintf(o.stdout, "    error: %s\n", strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(r.Error))
				}
			}
		}
		for _, e := range p.Errors {
			fmt.Fprintf(o.stdout, "  error %s: %s (%s)\n", e.Part, e.Message, e.Code)
		}
	}
	return ExitOK
}

// projects dispatches the projects subcommands.
func (a *App) projects(ctx context.Context, o *output, args []string) int {
	if len(args) == 0 {
		return o.usage("projects needs add, list, rm or set")
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
		if len(pos) > 1 || (len(pos) == 0 && o.json) {
			return o.usage("projects add takes one PATH, got %d arguments", len(pos))
		}
		if len(pos) == 0 {
			getwd := a.Getwd
			if getwd == nil {
				getwd = os.Getwd
			}
			dir, err := getwd()
			if err != nil {
				return o.failErr(err)
			}
			projectName := *name
			if projectName == "" {
				projectName = filepath.Base(dir)
			}
			fmt.Fprintf(o.stdout, "Add %s as project %s? [y/N]: ", dir, projectName)
			input := a.Input
			if input == nil {
				input = os.Stdin
			}
			answer, err := bufio.NewReader(input).ReadString('\n')
			if err != nil && err != io.EOF {
				return o.failErr(err)
			}
			if yesNo(strings.TrimSpace(answer)) != "yes" {
				return o.fail(ExitFailure, "cancelled", "cancelled")
			}
			pos = []string{dir}
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
	case "set":
		return a.projectsSet(ctx, o, fs, rest)
	}
	return o.usage("unknown projects command %q", sub)
}

// projectsSet is `ork projects set NAME --field VALUE...` (S161, *proposed*).
// A flag that is given sets its field; a flag that is left out keeps the
// registry value. An empty string or 0 clears a field.
func (a *App) projectsSet(ctx context.Context, o *output, fs *flag.FlagSet, args []string) int {
	push := fs.String("push", "", "on or off: push the base branch after a landing")
	uiDoc := fs.String("ui-doc", "", "the UI design doc, a path inside the project")
	worktree := fs.String("worktree-pattern", "", "the worktree path, with {id} or {id_lower}")
	branch := fs.String("branch-pattern", "", "the branch name, with {id} or {id_lower}")
	check := fs.String("check", "", "the check command")
	checkMinutes := fs.Int("check-minutes", 0, "minutes the check may run")
	capN := fs.Int("cap", 0, "the project's concurrent run cap")
	budgetMinutes := fs.Int("budget-minutes", 0, "the run time budget in minutes")
	budgetTokens := fs.Int("budget-tokens", 0, "the run token budget")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("projects set: %v", err)
	}
	if len(pos) != 1 {
		return o.usage("projects set takes one NAME, got %d arguments", len(pos))
	}
	p := daemon.SetProjectParams{Name: pos[0]}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if given["push"] {
		switch *push {
		case "on", "off":
			on := *push == "on"
			p.Push = &on
		default:
			return o.usage("projects set: --push takes on or off, got %q", *push)
		}
	}
	if given["ui-doc"] {
		p.UIDoc = uiDoc
	}
	if given["worktree-pattern"] {
		p.WorktreePattern = worktree
	}
	if given["branch-pattern"] {
		p.BranchPattern = branch
	}
	if given["check"] {
		p.CheckCommand = check
	}
	if given["check-minutes"] {
		p.CheckMinutes = checkMinutes
	}
	if given["cap"] {
		p.Cap = capN
	}
	if given["budget-minutes"] {
		p.BudgetMinutes = budgetMinutes
	}
	if given["budget-tokens"] {
		p.BudgetTokens = budgetTokens
	}
	if p == (daemon.SetProjectParams{Name: p.Name}) {
		return o.usage("projects set needs at least one field flag (--push, --ui-doc, --worktree-pattern, --branch-pattern, --check, --check-minutes, --cap, --budget-minutes, --budget-tokens)")
	}
	return a.withDaemon(ctx, o, func(c *daemon.Client) int {
		proj, err := c.SetProject(ctx, p)
		if err != nil {
			return o.failErr(err)
		}
		if o.json {
			return o.printJSON(map[string]any{"project": proj})
		}
		// Say what the registry holds now: the daemon may clean a value.
		text := func(name, value string) string {
			if value == "" {
				return name + "=unset"
			}
			return fmt.Sprintf("%s=%q", name, value)
		}
		number := func(name string, value int) string {
			if value == 0 {
				return name + "=unset"
			}
			return fmt.Sprintf("%s=%d", name, value)
		}
		var changed []string
		if p.Push != nil {
			changed = append(changed, map[bool]string{true: "push=on", false: "push=off"}[proj.Push])
		}
		if p.UIDoc != nil {
			changed = append(changed, text("ui_doc", proj.UIDoc))
		}
		if p.WorktreePattern != nil {
			changed = append(changed, text("worktree_pattern", proj.WorktreePattern))
		}
		if p.BranchPattern != nil {
			changed = append(changed, text("branch_pattern", proj.BranchPattern))
		}
		if p.CheckCommand != nil {
			changed = append(changed, text("check_command", proj.CheckCommand))
		}
		if p.CheckMinutes != nil {
			changed = append(changed, number("check_minutes", proj.CheckMinutes))
		}
		if p.Cap != nil {
			changed = append(changed, number("cap", proj.Cap))
		}
		if p.BudgetMinutes != nil {
			changed = append(changed, number("budget_minutes", proj.Budgets.Minutes))
		}
		if p.BudgetTokens != nil {
			changed = append(changed, number("budget_tokens", proj.Budgets.Tokens))
		}
		fmt.Fprintf(o.stdout, "updated %s: %s\n", proj.Name, strings.Join(changed, ", "))
		return ExitOK
	})
}
