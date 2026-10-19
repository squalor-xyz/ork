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
	"github.com/squalor-xyz/ork/internal/runrec"
)

func (a *App) call(ctx context.Context, o *output, method string, params, result any) int {
	if a.Call != nil {
		if err := a.Call(ctx, method, params, result); err != nil {
			return o.failErr(err)
		}
		return ExitOK
	}
	return a.withDaemon(ctx, o, func(c *daemon.Client) int {
		if err := c.Call(ctx, method, params, result); err != nil {
			return o.failErr(err)
		}
		return ExitOK
	})
}
func (a *App) runSlice(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm the plan")
	well := fs.String("well-specified", "", "yes or no")
	split := fs.String("split", "", "yes or no")
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("run: %v", err)
	}
	if len(pos) != 2 {
		return o.usage("run takes PROJECT ID")
	}
	// adj #86 run.lineup-inputs: prompt at confirmation, or require both
	// explicit yes|no inputs when --yes skips the prompt.
	valid := func(v string) bool { return v == "" || v == "yes" || v == "no" }
	if !valid(*well) || !valid(*split) || (*yes && (*well == "" || *split == "")) {
		return o.usage("run: --well-specified yes|no and --split yes|no are required with --yes")
	}
	input := a.Input
	if input == nil {
		input = os.Stdin
	}
	reader := bufio.NewReader(input)
	prompts := o.stdout
	if o.json {
		prompts = o.stderr
	}
	ask := func(label string) (string, error) {
		fmt.Fprintf(prompts, "%s [yes/no]: ", label)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line != "yes" && line != "no" {
			return "", fmt.Errorf("%s requires yes or no", label)
		}
		return line, nil
	}
	if *well == "" {
		*well, err = ask("Well specified?")
		if err != nil {
			return o.usage("%v", err)
		}
	}
	if *split == "" {
		*split, err = ask("Split?")
		if err != nil {
			return o.usage("%v", err)
		}
	}
	params := daemon.RunParams{Project: pos[0], Item: pos[1], WellSpecified: *well == "yes", Split: *split == "yes"}
	var plan daemon.RunPlan
	if code := a.call(ctx, o, daemon.MethodRunPlan, params, &plan); code != 0 {
		return code
	}
	// In JSON mode the plan is one diagnostic JSON value on stderr; stdout
	// remains one result value for the command.
	if o.json {
		if code := (&output{stdout: o.stderr, stderr: o.stderr, json: true}).printJSON(plan); code != 0 {
			return code
		}
	} else {
		printRunPlan(o.stdout, plan)
	}
	if !*yes {
		confirm, err := ask("Start this slice?")
		if err != nil {
			return o.usage("%v", err)
		}
		if confirm != "yes" {
			if o.json {
				return o.printJSON(map[string]bool{"started": false})
			}
			fmt.Fprintln(o.stdout, "cancelled")
			return ExitOK
		}
	}
	params.Expected = &plan
	var rec runrec.Record
	if code := a.call(ctx, o, daemon.MethodRunStart, params, &rec); code != 0 {
		return code
	}
	return printRun(o, rec)
}
func printRunPlan(w io.Writer, p daemon.RunPlan) {
	fmt.Fprintf(w, "%s %s\nworktree: %s\nbranch: %s\ncheck: %s\n", p.Project.Name, p.Item, p.Worktree, p.Branch, p.CheckCommand)
	for _, role := range []string{"implementer", "reviewer", "gate"} {
		for _, entry := range p.Lineup[role] {
			fmt.Fprintf(w, "%s: %s %s %s\n", role, entry.Vendor, entry.Model, entry.Effort)
		}
		perm := p.Permissions[role]
		if perm.ReadOnly {
			fmt.Fprintf(w, "  permissions: read-only\n")
		} else {
			fmt.Fprintf(w, "  allowlist: %s\n", strings.Join(perm.Allow, ", "))
		}
	}
}
func printRun(o *output, r runrec.Record) int {
	if o.json {
		return o.printJSON(r)
	}
	fmt.Fprintf(o.stdout, "%s %s\n", r.ID, r.State)
	return ExitOK
}
func (a *App) ownerCommand(ctx context.Context, o *output, command string, args []string) int {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("%s: %v", command, err)
	}
	want := 1
	if command == "answer" {
		want = 2
	}
	if len(pos) != want {
		return o.usage("%s takes RUN%s", command, map[bool]string{true: " TEXT"}[command == "answer"])
	}
	p := daemon.RunAction{Run: pos[0]}
	if command == "answer" {
		p.Text = pos[1]
		if strings.TrimSpace(p.Text) == "" {
			return o.usage("answer TEXT is empty")
		}
	}
	var rec runrec.Record
	if code := a.call(ctx, o, "run."+command, p, &rec); code != 0 {
		return code
	}
	return printRun(o, rec)
}
