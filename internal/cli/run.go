// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func (a *App) call(ctx context.Context, o *output, method string, params, result any) int {
	finish := func() int {
		if method == daemon.MethodStatus {
			if status, ok := result.(*daemon.StatusResult); ok {
				if a.Call != nil {
					a.warnStale(o, status.BuildID)
				}
				if notice := daemon.BehindWarning(status); notice != "" {
					fmt.Fprintf(o.stderr, "ork: warning: %s\n", notice)
				}
			}
		}
		return ExitOK
	}
	if a.Call != nil {
		if err := a.Call(ctx, method, params, result); err != nil {
			return o.failErr(err)
		}
		return finish()
	}
	return a.withDaemon(ctx, o, func(c *daemon.Client) int {
		if err := c.Call(ctx, method, params, result); err != nil {
			return o.failErr(err)
		}
		return finish()
	})
}
func (a *App) runSlice(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm the plan")
	well := fs.String("well-specified", "", "y/n")
	split := fs.String("split", "", "y/n")
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("run: %v", err)
	}
	if len(pos) != 2 {
		return o.usage("run takes PROJECT ID")
	}
	// adj #86 run.lineup-inputs: prompt at confirmation, or require both
	// explicit y/n inputs when --yes skips the prompt. adj #87
	// run.prompt-answers: y, yes, n and no are accepted in any case.
	valid := func(v string) bool { return v == "" || yesNo(v) != "" }
	if !valid(*well) || !valid(*split) || (*yes && (*well == "" || *split == "")) {
		return o.usage("run: --well-specified y/n and --split y/n are required with --yes")
	}
	*well, *split = yesNo(*well), yesNo(*split)
	input := a.Input
	if input == nil {
		input = os.Stdin
	}
	reader := bufio.NewReader(input)
	prompts := o.stdout
	if o.json {
		prompts = o.stderr
	}
	// Enter alone takes def; with no default it is an error. Input that ends
	// without a newline is read as typed, but closed input is not Enter.
	ask := func(label, hint, def string) (string, error) {
		fmt.Fprintf(prompts, "%s [%s]: ", label, hint)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		line = strings.TrimSpace(line)
		answer := yesNo(line)
		if line == "" && err == nil {
			answer = def
		}
		if answer == "" {
			return "", fmt.Errorf("%s requires y or n", label)
		}
		return answer, nil
	}
	if *well == "" {
		*well, err = ask("Well specified?", "y/n", "")
		if err != nil {
			return o.usage("%v", err)
		}
	}
	if *split == "" {
		*split, err = ask("Split?", "y/n", "")
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
		confirm, err := ask("Start this slice?", "y/N", "no")
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

// yesNo maps y, yes, n and no, in any case, to "yes" or "no". Anything else
// gives "".
func yesNo(v string) string {
	switch strings.ToLower(v) {
	case "y", "yes":
		return "yes"
	case "n", "no":
		return "no"
	}
	return ""
}
func printRunPlan(w io.Writer, p daemon.RunPlan) {
	fmt.Fprintf(w, "%s %s\nworktree: %s\nbranch: %s\ncheck: %s\n", p.Project.Name, p.Item, p.Worktree, p.Branch, p.CheckCommand)
	for _, role := range []string{"implementer", "reviewer", "ui", "architect", "gate"} {
		if (role == "architect" || role == "ui") && len(p.Lineup[role]) == 0 {
			continue
		}
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
	if p.Note != "" {
		fmt.Fprintf(w, "note: %s\n", p.Note)
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
	if command == "answer" || command == "file" {
		want = 2
	}
	if len(pos) != want {
		if command == "stop" {
			return o.usage("stop takes RUN or WAVE")
		}
		if command == "file" {
			return o.usage("file takes RUN N")
		}
		return o.usage("%s takes RUN%s", command, map[bool]string{true: " TEXT"}[command == "answer"])
	}
	if command == "stop" && strings.HasPrefix(pos[0], "wave-") {
		var result daemon.WaveResult
		if code := a.call(ctx, o, daemon.MethodWaveStop, daemon.WaveAction{Wave: pos[0]}, &result); code != ExitOK {
			return code
		}
		return printWave(o, result)
	}
	p := daemon.RunAction{Run: pos[0]}
	if command == "file" {
		n, err := strconv.Atoi(pos[1])
		if err != nil || n < 1 {
			return o.usage("file N must be a positive finding number")
		}
		p.Finding = n
		var reply daemon.RunReply
		if code := a.call(ctx, o, daemon.MethodRunFile, p, &reply); code != 0 {
			return code
		}
		if o.json {
			return o.printJSON(reply)
		}
		fmt.Fprintln(o.stdout, reply.Message)
		return ExitOK
	}
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

// cardCommand is ork check and ork fix: the shell's side of the approval card,
// so that ork approve works on a slice with human checks (S141, *proposed*).
// ork check RUN N [ticked|later|open] marks the one-based check, ticked unless
// told; ork fix RUN TEXT sends the note to a fix round and returns when the run
// rests again. Both print the run record like the other owner commands.
func (a *App) cardCommand(ctx context.Context, o *output, command string, args []string) int {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("%s: %v", command, err)
	}
	p := daemon.RunAction{}
	if command == "fix" {
		if len(pos) != 2 || strings.TrimSpace(pos[1]) == "" {
			return o.usage("fix takes RUN TEXT")
		}
		p.Run, p.Text = pos[0], pos[1]
	} else {
		if len(pos) < 2 || len(pos) > 3 {
			return o.usage("check takes RUN N [ticked|later|open]")
		}
		n, err := strconv.Atoi(pos[1])
		if err != nil || n < 1 {
			return o.usage("check takes RUN N [ticked|later|open]: N must be a positive check number")
		}
		p.Run, p.Check, p.State = pos[0], n, runrec.CheckTicked
		if len(pos) == 3 {
			switch pos[2] {
			case runrec.CheckTicked, runrec.CheckLater:
				p.State = pos[2]
			case "open":
				p.State = ""
			default:
				return o.usage("check takes RUN N [ticked|later|open]: %q is not a state", pos[2])
			}
		}
	}
	var rec runrec.Record
	if code := a.call(ctx, o, "run."+command, p, &rec); code != ExitOK {
		return code
	}
	return printRun(o, rec)
}
