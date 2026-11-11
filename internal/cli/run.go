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
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func (a *App) call(ctx context.Context, o *output, method string, params, result any) int {
	return a.callWith(ctx, o, "", method, params, result)
}

func (a *App) callWith(ctx context.Context, o *output, prefix, method string, params, result any) int {
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
			return o.failErrWith(prefix, err)
		}
		return finish()
	}
	return a.withDaemon(ctx, o, func(c *daemon.Client) int {
		if err := c.Call(ctx, method, params, result); err != nil {
			return o.failErrWith(prefix, err)
		}
		return finish()
	})
}
func (a *App) runSlice(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm the plan")
	well := fs.String("well-specified", "", "y/n")
	split := fs.String("split", "", "y/n")
	allowNoCheck := fs.Bool("allow-no-check", false, "start although the project has no check command")
	var lineup lineupFlags
	lineup.register(fs)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("run: %v", err)
	}
	if len(pos) != 2 {
		return o.usage("run takes PROJECT ID")
	}
	// Adj 189 launch.well-specified-default: every item plans as well specified
	// = yes and split = no, and the spec check holds an item that is not Ready.
	// The flags override the defaults; y, yes, n and no are accepted in any case.
	wellSpecified, splitItem, ok := lineupAnswers(*well, *split)
	if !ok {
		return o.usage("run: --well-specified y/n and --split y/n each take y or n")
	}
	var overrides daemon.RunParams
	if err := lineup.apply(&overrides); err != nil {
		return o.usage("run: %v", err)
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
	// Enter alone takes def; with no default it is an error. Input that ends
	// without a newline is read as typed, but closed input is not Enter.
	ask := func(label, hint, def string) (string, error) {
		fmt.Fprintf(prompts, "%s [%s]: ", label, hint)
		line, err := readLine(reader, prompts)
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
	params := daemon.RunParams{Project: pos[0], Item: pos[1], WellSpecified: wellSpecified, Split: splitItem}
	params.Lineup, params.Budgets, params.StopAfter = overrides.Lineup, overrides.Budgets, overrides.StopAfter
	params.AllowNoCheck = *allowNoCheck
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
		printRunPlan(o.stdout, noCheckPlanText(plan, params.AllowNoCheck), params.Budgets != nil)
	}
	// The daemon refuses the start too (adj 191); refusing here spares the
	// prompts and the spec check.
	if plan.CheckCommand == "" && !params.AllowNoCheck {
		return o.fail(ExitFailure, daemon.CodeRefused, daemon.NoCheckRefusalMessage(params.Project))
	}
	checked := daemon.WaveStartParams{Items: []daemon.RunParams{params}}
	held, _, code := a.specCheck(ctx, o, reader, prompts, &checked)
	if code != ExitOK {
		return code
	}
	if held == nil {
		held = []string{}
	}
	if len(checked.Items) == 0 {
		if o.json {
			if code := o.printJSON(map[string]any{"started": false, "held": held}); code != ExitOK {
				return code
			}
		}
		return ExitFailure
	}
	if !*yes {
		confirm, err := ask("Start this slice?", "y/N", "no")
		if err != nil {
			return o.usage("%v", err)
		}
		if confirm != "yes" {
			if o.json {
				return o.printJSON(map[string]any{"started": false, "held": held})
			}
			fmt.Fprintln(o.stdout, "cancelled")
			return ExitOK
		}
	}
	params.Expected = &plan
	var reply daemon.RunReply
	if code := a.call(ctx, o, daemon.MethodRunStart, params, &reply); code != 0 {
		return code
	}
	return printRunReply(o, reply)
}

// printRunReply prints the started run, and what the start did with a stopped
// run of the item (S296, *proposed*): resumed it, or started fresh and why.
func printRunReply(o *output, r daemon.RunReply) int {
	if o.json {
		return o.printJSON(r)
	}
	code := printRun(o, r.Record)
	if r.Message != "" {
		fmt.Fprintln(o.stdout, r.Message)
	}
	return code
}

// lineupAnswers is the planning inputs for well specified and split: the flag
// values, or the defaults (yes and no) for a flag left out (adj 189). ok is
// false when a flag holds anything but y, yes, n or no.
func lineupAnswers(well, split string) (wellSpecified, splitItem, ok bool) {
	valid := func(v string) bool { return v == "" || yesNo(v) != "" }
	if !valid(well) || !valid(split) {
		return false, false, false
	}
	return yesNo(well) != "no", yesNo(split) == "yes", true
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

// noCheckPlanText returns plan as ork shows it. The daemon's no-check warning
// says ork refuses, and the refusal prints that text as the error, so a
// refused start leaves it out of the plan; with --allow-no-check the plan
// warns in plain words instead (adj 191). The plan the daemon compares at
// start is the original, not this copy.
func noCheckPlanText(plan daemon.RunPlan, allowed bool) daemon.RunPlan {
	var warnings []string
	for _, w := range plan.Warnings {
		switch {
		case w != engine.NoCheckMessage(plan.Project.Name):
			warnings = append(warnings, w)
		case allowed:
			warnings = append(warnings, engine.NoCheckAllowedNote())
		}
	}
	plan.Warnings = warnings
	return plan
}

func printRunPlan(w io.Writer, p daemon.RunPlan, budgetsOverridden bool) {
	check := p.CheckCommand
	if check == "" {
		check = "none"
	}
	fmt.Fprintf(w, "%s %s\nworktree: %s\nbranch: %s\ncheck: %s\n", p.Project.Name, p.Item, p.Worktree, p.Branch, check)
	if p.RoundCheckCommand != "" {
		fmt.Fprintf(w, "round check: %s\n", p.RoundCheckCommand)
	}
	stop := "after landing"
	if p.StopAfter == runrec.StopAfterCommit {
		stop = "after commit (you land it)"
	}
	fmt.Fprintf(w, "stop: %s\n", stop)
	if budgetsOverridden {
		fmt.Fprintf(w, "budgets: %d min, %d tokens\n", p.Budgets.Minutes, p.Budgets.Tokens)
	}
	for _, role := range []string{"implementer", "reviewer", "ui", "architect", "gate"} {
		if (role == "architect" || role == "ui") && len(p.Lineup[role]) == 0 {
			continue
		}
		for _, entry := range p.Lineup[role] {
			fmt.Fprintf(w, "%s: %s %s %s\n", role, entry.Vendor, runrec.ModelLabel(entry), entry.Effort)
		}
		perm := p.Permissions[role]
		if perm.ReadOnly {
			fmt.Fprintf(w, "  permissions: read-only\n")
		} else {
			fmt.Fprintf(w, "  allowlist: %s\n", strings.Join(perm.Allow, ", "))
		}
	}
	for _, warning := range p.Warnings {
		fmt.Fprintf(w, "warning: %s\n", warning)
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
	// An answer or retry that waits for a slot leaves the run paused (S274).
	if r.State == runrec.StatePaused && r.Resume != nil {
		fmt.Fprintln(o.stdout, daemon.ResumeWaitNote)
	}
	return ExitOK
}
func (a *App) ownerCommand(ctx context.Context, o *output, command string, args []string) int {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	// answer and retry return once the run has resumed; --wait waits until it
	// rests again, as the TUI's calls do (S237, *proposed*).
	var wait, acceptShape, again bool
	var rounds int
	if command == "retry" {
		fs.IntVar(&rounds, "rounds", 0, "grant additional implementer rounds (default 2 at a rounds pause or a resume)")
		fs.BoolVar(&acceptShape, "accept-shape", false, "keep a refused report's owner findings that have no options (S298, proposed)")
		fs.BoolVar(&again, "again", false, "run the role once more after a repeated shape pause (S298, proposed)")
	}
	if command == "answer" || command == "retry" {
		fs.BoolVar(&wait, "wait", false, "wait until the run rests")
	}
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("%s: %v", command, err)
	}
	invalidRounds := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "rounds" && rounds < 1 {
			invalidRounds = true
		}
	})
	if invalidRounds {
		return o.usage("rounds must be a positive integer")
	}
	if acceptShape && again {
		return o.usage("retry: --accept-shape and --again cannot be used together")
	}
	want := 1
	if command == "answer" || command == "file" {
		want = 2
	}
	// retry takes an optional TEXT, a note for the rerun (S256, *proposed*).
	if len(pos) != want && !(command == "retry" && len(pos) == 2) {
		if command == "retry" {
			return o.usage("retry takes RUN [TEXT]")
		}
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
	p := daemon.RunAction{Run: pos[0], Rounds: rounds, AcceptShape: acceptShape, Again: again, NoWait: (command == "answer" || command == "retry") && !wait}
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
	if command == "answer" || (command == "retry" && len(pos) == 2) {
		p.Text = pos[1]
		if strings.TrimSpace(p.Text) == "" {
			return o.usage("%s TEXT is empty", command)
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
