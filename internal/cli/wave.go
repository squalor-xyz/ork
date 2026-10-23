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
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func (a *App) wave(ctx context.Context, o *output, args []string) int {
	if len(args) == 0 {
		return o.usage("wave needs suggest or start")
	}
	switch args[0] {
	case "suggest":
		return a.waveSuggest(ctx, o, args[1:])
	case "start":
		return a.waveStart(ctx, o, args[1:])
	}
	return o.usage("wave needs suggest or start")
}

func (a *App) waveSuggest(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("wave suggest", flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("wave suggest: %v", err)
	}
	if len(pos) != 1 {
		return o.usage("wave suggest takes PROJECT")
	}
	var projects daemon.ProjectsResult
	if code := a.call(ctx, o, daemon.MethodProjectsList, nil, &projects); code != ExitOK {
		return code
	}
	for _, project := range projects.Projects {
		if project.Name != pos[0] {
			continue
		}
		plan, err := (slicercli.Client{Bin: a.Tools.Slicer}).PlanParallel(ctx, project.Path)
		if err != nil {
			return o.failErr(err)
		}
		if o.json {
			return o.printJSON(plan)
		}
		fmt.Fprintf(o.stdout, "Width: %d\n", plan.Width)
		for _, wave := range plan.Waves {
			fmt.Fprintf(o.stdout, "Wave %d\n", wave.Wave)
			for _, item := range wave.Items {
				fmt.Fprintf(o.stdout, "  %s %s", item.ID, item.Title)
				if item.Reason != "" {
					fmt.Fprintf(o.stdout, " (%s)", item.Reason)
				}
				fmt.Fprintln(o.stdout)
			}
		}
		for _, item := range plan.Unplaced {
			fmt.Fprintf(o.stdout, "Unplaced: %s %s (%s)\n", item.ID, item.Title, item.Reason)
		}
		for _, item := range plan.Blocked {
			fmt.Fprintf(o.stdout, "Blocked: %s (waiting on %s)\n", item.ID, strings.Join(item.WaitingOn, ", "))
		}
		for _, item := range plan.Held {
			fmt.Fprintf(o.stdout, "Held: %s %s (%s)\n", item.ID, item.Title, strings.Join(item.Flags, ", "))
		}
		for _, item := range plan.Unspecified {
			fmt.Fprintf(o.stdout, "Unspecified: %s (missing %s)\n", item.ID, strings.Join(item.Missing, ", "))
		}
		return ExitOK
	}
	if len(projects.Projects) == 0 {
		return o.fail(ExitFailure, daemon.CodeNotFound, daemon.NoProjectsGuide)
	}
	return o.fail(ExitFailure, daemon.CodeNotFound, fmt.Sprintf("no project named %q", pos[0]))
}

func (a *App) waveStart(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("wave start", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm all plans")
	well := fs.String("well-specified", "", "yes or no for every item")
	split := fs.String("split", "", "yes or no for every item")
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("wave start: %v", err)
	}
	if len(pos) < 2 {
		return o.usage("wave start takes PROJECT ITEM...")
	}
	valid := func(v string) bool { return v == "" || yesNo(v) != "" }
	if !valid(*well) || !valid(*split) || (*yes && (*well == "" || *split == "")) {
		return o.usage("wave start: --well-specified yes|no and --split yes|no are required with --yes")
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
	params := daemon.WaveStartParams{Items: []daemon.RunParams{}}
	for _, item := range pos[1:] {
		w, s := yesNo(*well), yesNo(*split)
		if w == "" {
			w, err = ask(item+" Well specified?", "y/n", "")
			if err != nil {
				return o.usage("%v", err)
			}
		}
		if s == "" {
			s, err = ask(item+" Split?", "y/n", "")
			if err != nil {
				return o.usage("%v", err)
			}
		}
		p := daemon.RunParams{Project: pos[0], Item: item, WellSpecified: w == "yes", Split: s == "yes"}
		var plan daemon.RunPlan
		if code := a.call(ctx, o, daemon.MethodRunPlan, p, &plan); code != ExitOK {
			return code
		}
		if o.json {
			if code := (&output{stdout: o.stderr, stderr: o.stderr, json: true}).printJSON(plan); code != ExitOK {
				return code
			}
		} else {
			printRunPlan(o.stdout, plan)
		}
		p.Expected = &plan
		params.Items = append(params.Items, p)
	}
	if !*yes {
		confirm, err := ask("Start this wave?", "y/N", "no")
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
	var result daemon.WaveResult
	if code := a.call(ctx, o, daemon.MethodWaveStart, params, &result); code != ExitOK {
		return code
	}
	return printWave(o, result)
}

func printWave(o *output, result daemon.WaveResult) int {
	code := ExitOK
	for _, item := range result.Items {
		if item.Error != nil {
			code = ExitFailure
		}
	}
	if o.json {
		if printed := o.printJSON(result); printed != ExitOK {
			return printed
		}
		return code
	}
	fmt.Fprintln(o.stdout, result.Wave)
	for _, item := range result.Items {
		if item.Error != nil {
			fmt.Fprintf(o.stderr, "%s: %s (%s)\n", item.Item, item.Error.Message, item.Error.Code)
		} else if item.Run != nil {
			printRun(o, *item.Run)
		}
	}
	return code
}
