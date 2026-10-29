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
	well := fs.String("well-specified", "", "y/n for every item")
	split := fs.String("split", "", "y/n for every item")
	var lineup lineupFlags
	lineup.register(fs)
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
		return o.usage("wave start: --well-specified y/n and --split y/n are required with --yes")
	}
	var overrides daemon.RunParams
	if err := lineup.apply(&overrides); err != nil {
		return o.usage("wave start: %v", err)
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
		p.Lineup, p.Budgets, p.StopAfter = overrides.Lineup, overrides.Budgets, overrides.StopAfter
		var plan daemon.RunPlan
		if code := a.call(ctx, o, daemon.MethodRunPlan, p, &plan); code != ExitOK {
			return code
		}
		if o.json {
			if code := (&output{stdout: o.stderr, stderr: o.stderr, json: true}).printJSON(plan); code != ExitOK {
				return code
			}
		} else {
			printRunPlan(o.stdout, plan, p.Budgets != nil)
		}
		p.Expected = &plan
		params.Items = append(params.Items, p)
	}
	held, code := a.specCheck(ctx, o, reader, prompts, &params)
	if held == nil {
		held = []string{}
	}
	if code != ExitOK {
		return code
	}
	if len(params.Items) == 0 {
		if o.json {
			o.printJSON(map[string]any{"started": false, "held": held})
		}
		fmt.Fprintln(prompts, "no item is ready to start")
		return ExitFailure
	}
	if !*yes {
		confirm, err := ask("Start this wave?", "y/N", "no")
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
	var result daemon.WaveResult
	if code := a.call(ctx, o, daemon.MethodWaveStart, params, &result); code != ExitOK {
		return code
	}
	if o.json {
		code := ExitOK
		for _, item := range result.Items {
			if item.Error != nil {
				code = ExitFailure
			}
		}
		if printed := o.printJSON(struct {
			daemon.WaveResult
			Held []string `json:"held"`
		}{result, held}); printed != ExitOK {
			return printed
		}
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

// specCheck runs the spec check on the planned items (adj 164). A ready item
// stays in the wave. For an item with open questions it asks them all in one
// sitting, records the answers, and keeps the item once they are in. An item
// whose questions stay open (an empty answer, or no more input) is dropped
// from the wave and named in held. The sitting goes to the prompts writer.
func (a *App) specCheck(ctx context.Context, o *output, in *bufio.Reader, w io.Writer, params *daemon.WaveStartParams) (held []string, code int) {
	var ids []string
	for _, item := range params.Items {
		ids = append(ids, item.Item)
	}
	var check daemon.SpecCheckResult
	if code := a.call(ctx, o, daemon.MethodSpecCheck, daemon.SpecCheckParams{Project: params.Items[0].Project, Items: ids}, &check); code != ExitOK {
		return nil, code
	}
	if check.OwnerNeeded != "" {
		fmt.Fprintln(w, "Owner needed: "+check.OwnerNeeded)
	}
	open := map[string]daemon.SpecItemResult{}
	for _, r := range check.Items {
		if !r.Ready {
			open[r.Item] = r
		}
	}
	by := os.Getenv("ADJ_DECIDED_BY")
	var projects *daemon.ProjectsResult
	var kept []daemon.RunParams
	for _, item := range params.Items {
		r, ok := open[item.Item]
		if !ok {
			kept = append(kept, item)
			continue
		}
		answers := askSpecQuestions(in, w, r)
		if answers == nil {
			fmt.Fprintf(w, "held: %s has %d open spec %s\n", r.Item, len(r.Questions), plural(len(r.Questions), "question"))
			held = append(held, r.Item)
			continue
		}
		if projects == nil {
			projects = &daemon.ProjectsResult{}
			if code := a.call(ctx, o, daemon.MethodProjectsList, nil, projects); code != ExitOK {
				return held, code
			}
		}
		adj := false
		for _, p := range projects.Projects {
			if p.Name == item.Project {
				adj = p.Adj
				break
			}
		}
		var out daemon.SpecAnswerResult
		p := daemon.SpecAnswerParams{Project: item.Project, Item: r.Item, Answers: answers, Adj: adj, By: by}
		if code := a.call(ctx, o, daemon.MethodSpecAnswer, p, &out); code != ExitOK {
			return held, code
		}
		for _, n := range out.Notices {
			fmt.Fprintln(w, n)
		}
		kept = append(kept, item)
	}
	params.Items = kept
	return held, ExitOK
}

func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// askSpecQuestions shows every question of an item and reads one answer for
// each: a number picks an option, other text is the answer. A number outside
// the options is asked again. An empty line, or the end of the input, holds
// the item back and returns nil.
func askSpecQuestions(in *bufio.Reader, w io.Writer, r daemon.SpecItemResult) []string {
	fmt.Fprintf(w, "%s needs %d %s before it can start.\n", r.Item, len(r.Questions), plural(len(r.Questions), "answer"))
	var answers []string
	for i, q := range r.Questions {
		fmt.Fprintf(w, "[%s %d/%d] ", r.Item, i+1, len(r.Questions))
		if q.Ask == nil {
			fmt.Fprintf(w, "%s\n", q.Text)
			fmt.Fprintf(w, "Answer in text (empty holds %s back): ", r.Item)
		} else {
			fmt.Fprintf(w, "%s\n", q.Ask.Title)
			for n, o := range q.Ask.Options {
				mark := ""
				if n+1 == q.Ask.Recommend {
					mark = " (recommended)"
				}
				fmt.Fprintf(w, "  %d. %s: %s%s\n", n+1, o.Label, o.Summary, mark)
			}
			fmt.Fprintf(w, "  Why %d: %s\n", q.Ask.Recommend, q.Ask.Reason)
			if q.Key != "" {
				fmt.Fprintf(w, "  Decision key: %s\n", q.Key)
			}
			fmt.Fprintf(w, "Answer 1-%d, or type text (empty holds %s back): ", len(q.Ask.Options), r.Item)
		}
		answer := ""
		for answer == "" {
			line, err := in.ReadString('\n')
			line = strings.TrimSpace(line)
			if line == "" {
				if err == nil {
					fmt.Fprintln(w)
				}
				return nil
			}
			if err != nil && err != io.EOF {
				return nil
			}
			answer = line
			if q.Ask != nil {
				if n, err := strconv.Atoi(line); err == nil {
					if n < 1 || n > len(q.Ask.Options) {
						fmt.Fprintf(w, "Pick 1-%d, or type text: ", len(q.Ask.Options))
						answer = ""
						continue
					}
					o := q.Ask.Options[n-1]
					answer = o.Label + ": " + o.Summary
				}
			}
		}
		answers = append(answers, answer)
	}
	return answers
}
