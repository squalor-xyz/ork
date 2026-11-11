// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func (a *App) wave(ctx context.Context, o *output, args []string) int {
	const needs = "wave needs define, list, rm, suggest or start"
	if len(args) == 0 {
		return o.usage(needs)
	}
	switch args[0] {
	case "suggest":
		return a.waveSuggest(ctx, o, args[1:])
	case "start":
		return a.waveStart(ctx, o, args[1:])
	case "define":
		return a.waveDefine(ctx, o, args[1:])
	case "list":
		return a.waveList(ctx, o, args[1:])
	case "rm":
		return a.waveRm(ctx, o, args[1:])
	}
	return o.usage(needs)
}

// Saved waves (S215, *proposed*): ork wave define, list and rm manage them, and
// ork wave start NAME starts one.

func (a *App) waveDefine(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("wave define", flag.ContinueOnError)
	project := fs.String("project", "", "the project of the wave")
	replace := fs.Bool("replace", false, "replace a saved wave of that name")
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("wave define: %v", err)
	}
	if *project == "" {
		return o.usage("wave define needs --project P")
	}
	if len(pos) < 2 {
		return o.usage("wave define takes NAME ITEM...")
	}
	var saved daemon.SavedWave
	params := daemon.WaveDefineParams{Project: *project, Name: pos[0], Items: pos[1:], Replace: *replace}
	if code := a.call(ctx, o, daemon.MethodWaveDefine, params, &saved); code != ExitOK {
		return code
	}
	if o.json {
		return o.printJSON(saved)
	}
	fmt.Fprintf(o.stdout, "%s  %s\n", saved.Name, saved.Project)
	for _, item := range saved.Items {
		fmt.Fprintf(o.stdout, "  %s\n", item)
	}
	return ExitOK
}

func (a *App) waveList(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("wave list", flag.ContinueOnError)
	project := fs.String("project", "", "list only this project's saved waves")
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("wave list: %v", err)
	}
	if len(pos) != 0 {
		return o.usage("wave list takes no argument besides --project P")
	}
	var list daemon.WaveListResult
	if code := a.call(ctx, o, daemon.MethodWaveList, daemon.WaveListParams{Project: *project}, &list); code != ExitOK {
		return code
	}
	if list.Waves == nil {
		list.Waves = []daemon.SavedWaveView{}
	}
	if o.json {
		return o.printJSON(list)
	}
	if len(list.Waves) == 0 {
		fmt.Fprintln(o.stdout, "no saved waves")
		return ExitOK
	}
	for i, w := range list.Waves {
		if i > 0 {
			fmt.Fprintln(o.stdout)
		}
		fmt.Fprintf(o.stdout, "%s  %s\n", w.Name, w.Project)
		if w.Error != nil {
			fmt.Fprintf(o.stdout, "  error: %s (%s)\n", w.Error.Message, w.Error.Code)
		}
		itemWidth, statusWidth := 0, 0
		for _, m := range w.Items {
			itemWidth, statusWidth = max(itemWidth, len(m.Item)), max(statusWidth, len(m.Status))
		}
		for _, m := range w.Items {
			line := "  " + m.Item
			if m.Status != "" {
				line = fmt.Sprintf("  %-*s  %-*s  %s", itemWidth, m.Item, statusWidth, m.Status, m.Title)
			}
			fmt.Fprintln(o.stdout, strings.TrimRight(line, " "))
		}
	}
	return ExitOK
}

func (a *App) waveRm(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("wave rm", flag.ContinueOnError)
	project := fs.String("project", "", "the project of the wave")
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("wave rm: %v", err)
	}
	if len(pos) != 1 {
		return o.usage("wave rm takes NAME")
	}
	name, from := pos[0], *project
	if from == "" {
		view, code := a.findSavedWave(ctx, o, "", name)
		if code != ExitOK {
			return code
		}
		from = view.Project
	}
	var removed daemon.SavedWave
	if code := a.call(ctx, o, daemon.MethodWaveRm, daemon.WaveRmParams{Project: from, Name: name}, &removed); code != ExitOK {
		return code
	}
	if o.json {
		return o.printJSON(removed)
	}
	fmt.Fprintf(o.stdout, "removed %s (%s)\n", removed.Name, removed.Project)
	return ExitOK
}

// findSavedWave reads the saved wave NAME. Without a project the name must be
// saved for one project only (NW-Q1): the saved wave then supplies the project.
// A nonzero code means the failure is printed.
func (a *App) findSavedWave(ctx context.Context, o *output, project, name string) (*daemon.SavedWaveView, int) {
	var list daemon.WaveListResult
	if code := a.call(ctx, o, daemon.MethodWaveList, daemon.WaveListParams{Project: project}, &list); code != ExitOK {
		return nil, code
	}
	var found []daemon.SavedWaveView
	var projects []string
	for _, w := range list.Waves {
		if w.Name == name {
			found = append(found, w)
			projects = append(projects, w.Project)
		}
	}
	switch len(found) {
	case 0:
		msg := fmt.Sprintf("no saved wave %q", name)
		if project != "" {
			msg += " for project " + project
		}
		return nil, o.fail(ExitFailure, daemon.CodeNotFound, msg)
	case 1:
		return &found[0], ExitOK
	}
	return nil, o.usage("wave %q is saved for several projects (%s); pass --project P", name, strings.Join(projects, ", "))
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
		client := slicercli.Client{Bin: a.Tools.Slicer}
		plan, err := client.PlanParallel(ctx, project.Path)
		if err != nil {
			return o.failErr(err)
		}
		// At most one engine, daemon or cmd slice per wave (S218, *proposed*).
		plan = daemon.SuggestWithEngineRule(ctx, client, project.Path, plan)
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

// waveOptions are the flags of ork wave start that apply to every item.
type waveOptions struct {
	spec          specFlags
	yes           bool
	wellSpecified bool
	split         bool
	allowNoCheck  bool
	overrides     daemon.RunParams // the lineup, budget and stop-after flags
}

func (a *App) waveStart(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("wave start", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm all plans")
	var spec specFlags
	spec.wave = true
	spec.register(fs)
	well := fs.String("well-specified", "", "y/n for every item")
	split := fs.String("split", "", "y/n for every item")
	allowNoCheck := fs.Bool("allow-no-check", false, "start although the project has no check command")
	project := fs.String("project", "", "the project of the saved wave NAME")
	var lineup lineupFlags
	lineup.register(fs)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("wave start: %v", err)
	}
	if err := spec.validate(*yes); err != nil {
		return o.usage("wave start: %v", err)
	}
	// One argument is the name of a saved wave; two or more are PROJECT ITEM...
	// as before (NW-Q1).
	if len(pos) == 0 {
		return o.usage("wave start takes NAME or PROJECT ITEM...")
	}
	if len(pos) > 1 && *project != "" {
		return o.usage("wave start: --project goes with a saved wave NAME; the typed form takes PROJECT ITEM...")
	}
	// Adj 189: the defaults are well specified = yes and split = no; the flags
	// override them for every item.
	wellSpecified, splitItem, ok := lineupAnswers(*well, *split)
	if !ok {
		return o.usage("wave start: --well-specified y/n and --split y/n each take y or n")
	}
	opts := waveOptions{spec: spec, yes: *yes, wellSpecified: wellSpecified, split: splitItem, allowNoCheck: *allowNoCheck}
	if err := lineup.apply(&opts.overrides); err != nil {
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
	if len(pos) > 1 {
		return a.startWave(ctx, o, reader, prompts, opts, pos[0], pos[1:], "", nil)
	}
	// A saved wave: read it, and start the members that are neither done,
	// retired nor missing, with the name stamped on the wave.
	view, code := a.findSavedWave(ctx, o, *project, pos[0])
	if code != ExitOK {
		return code
	}
	if view.Error != nil {
		return o.fail(ExitFailure, view.Error.Code, fmt.Sprintf("saved wave %q: %s", view.Name, view.Error.Message))
	}
	ids, skipped := view.Startable()
	for _, s := range skipped {
		fmt.Fprintf(prompts, "skipped %s: %s\n", s.Item, s.Status)
	}
	return a.startWave(ctx, o, reader, prompts, opts, view.Project, ids, view.Name, &skipped)
}

// startWave plans, spec-checks, confirms and starts the items of one project:
// the part of ork wave start that a typed list and a saved wave share. A saved
// wave's name goes to wave.start, and its skipped members to the output.
func (a *App) startWave(ctx context.Context, o *output, reader *bufio.Reader, prompts io.Writer, opts waveOptions, project string, items []string, name string, skipped *[]daemon.SkippedItem) int {
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
	params := daemon.WaveStartParams{Items: []daemon.RunParams{}, Name: name}
	for _, item := range items {
		p := daemon.RunParams{Project: project, Item: item, WellSpecified: opts.wellSpecified, Split: opts.split}
		p.Lineup, p.Budgets, p.StopAfter = opts.overrides.Lineup, opts.overrides.Budgets, opts.overrides.StopAfter
		p.AllowNoCheck = opts.allowNoCheck
		if opts.spec.questions {
			params.Items = append(params.Items, p)
			continue
		}
		var plan daemon.RunPlan
		if code := a.call(ctx, o, daemon.MethodRunPlan, p, &plan); code != ExitOK {
			return code
		}
		if o.json {
			if code := (&output{stdout: o.stderr, stderr: o.stderr, json: true}).printJSON(plan); code != ExitOK {
				return code
			}
		} else {
			printRunPlan(o.stdout, noCheckPlanText(plan, p.AllowNoCheck), p.Budgets != nil)
		}
		// The daemon refuses the whole wave too (adj 191).
		if plan.CheckCommand == "" && !p.AllowNoCheck {
			return o.fail(ExitFailure, daemon.CodeRefused, daemon.NoCheckRefusalMessage(p.Project))
		}
		p.Expected = &plan
		params.Items = append(params.Items, p)
	}
	// A saved wave with nothing startable has no item to check.
	held, noSpec, code := []string{}, []string{}, ExitOK
	if len(params.Items) > 0 {
		held, noSpec, code = a.specCheck(ctx, o, reader, prompts, &params, &opts.spec)
	} else if opts.spec.questions {
		if o.json {
			return o.printJSON(newSpecData())
		}
		fmt.Fprintf(prompts, "no open spec questions: %s\n", name)
		return ExitOK
	} else if len(opts.spec.answers) > 0 {
		key, _, _ := strings.Cut(opts.spec.answers[0], "=")
		return o.usage("%v", opts.spec.unknownAnswer(key))
	}
	if held == nil {
		held = []string{}
	}
	if noSpec == nil {
		noSpec = []string{}
	}
	if code != ExitOK || opts.spec.questions {
		return code
	}
	notStarted := func() map[string]any {
		out := map[string]any{"started": false, "held": held, "no_spec": noSpec}
		if skipped != nil {
			out["skipped"] = *skipped
		}
		return out
	}
	if len(params.Items) == 0 {
		if o.json {
			o.printJSON(notStarted())
		}
		fmt.Fprintln(prompts, "no item is ready to start")
		return ExitFailure
	}
	if !opts.yes {
		confirm, err := ask("Start this wave?", "y/N", "no")
		if err != nil {
			return o.usage("%v", err)
		}
		if confirm != "yes" {
			if o.json {
				if opts.spec.active() {
					out := notStarted()
					out["questions"], out["answered"] = opts.spec.data.Questions, opts.spec.data.Answered
					return o.printJSON(out)
				}
				return o.printJSON(notStarted())
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
			*specData
			Held    []string              `json:"held"`
			NoSpec  []string              `json:"no_spec"`
			Skipped *[]daemon.SkippedItem `json:"skipped,omitempty"`
		}{result, opts.spec.result(), held, noSpec, skipped}); printed != ExitOK {
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
			if item.Message != "" {
				fmt.Fprintf(o.stdout, "%s: %s\n", item.Item, item.Message)
			}
		}
	}
	return code
}

// specCheck runs the spec check on the planned items (adj 164). A ready item
// stays in the wave. For an item with open questions it asks them all in one
// sitting, records the answers, and keeps the item once they are in. An item
// whose questions stay open (an empty answer, or no more input) is dropped
// from the wave and named in held. An item with no slice spec is dropped too
// (adj 245, S283 *proposed*): it gets one line with the fix, `slicer promote`,
// is named in noSpec and is never asked about. The sitting and the lines go to
// the prompts writer.
func (a *App) specCheck(ctx context.Context, o *output, in *bufio.Reader, w io.Writer, params *daemon.WaveStartParams, flags *specFlags) (held, noSpec []string, code int) {
	var ids []string
	for _, item := range params.Items {
		ids = append(ids, item.Item)
	}
	// The call returns when the role has finished: say so first (S264, *proposed*).
	fmt.Fprintf(w, "spec check: %s; wait for it to finish\n", strings.Join(ids, ", "))
	var check daemon.SpecCheckResult
	if code := a.callWith(ctx, o, "spec check failed: ", daemon.MethodSpecCheck, daemon.SpecCheckParams{Project: params.Items[0].Project, Items: ids}, &check); code != ExitOK {
		return nil, nil, code
	}
	if check.OwnerNeeded != "" {
		fmt.Fprintln(w, "Owner needed: "+check.OwnerNeeded)
	}
	open := map[string]daemon.SpecItemResult{}
	missing := map[string]bool{}
	for _, r := range check.Items {
		switch {
		case r.NoSpec:
			missing[r.Item] = true
		case !r.Ready:
			open[r.Item] = r
		}
	}
	for _, item := range params.Items {
		if missing[item.Item] {
			fmt.Fprintf(w, "%s: no slice spec; run slicer promote %s\n", item.Item, item.Item)
			noSpec = append(noSpec, item.Item)
		}
	}
	flags.data = newSpecData()
	for _, item := range params.Items {
		if r, ok := open[item.Item]; ok {
			for i, q := range r.Questions {
				flags.data.Questions = append(flags.data.Questions, specListedQuestion{Item: r.Item, Address: flags.address(r.Item, q, i), SpecQuestion: q})
			}
		}
	}
	if flags.questions {
		for _, item := range params.Items {
			if r, ok := open[item.Item]; ok {
				for i, q := range r.Questions {
					printSpecQuestion(w, r, i, q, false, flags.address(r.Item, q, i))
				}
			}
		}
		if o.json {
			if code := o.printJSON(flags.data); code != ExitOK {
				return nil, nil, code
			}
		}
		if len(open) > 0 {
			return nil, nil, ExitWaitQuestion
		}
		if !o.json {
			fmt.Fprintf(w, "no open spec questions: %s\n", strings.Join(ids, ", "))
		}
		// Stop after a successful listing too; there is no launch to confirm.
		return nil, nil, ExitOK
	}
	var supplied map[string][]string
	if len(flags.answers) > 0 {
		var missing []string
		var err error
		supplied, missing, err = flags.mapAnswers(params.Items, open)
		if err != nil {
			return nil, nil, o.usage("%v", err)
		}
		if len(missing) > 0 {
			fmt.Fprintf(w, "held: unanswered spec questions: %s\n", strings.Join(missing, ", "))
			if o.json {
				if code := o.printJSON(flags.data); code != ExitOK {
					return nil, nil, code
				}
			}
			return nil, nil, ExitWaitQuestion
		}
	}
	by := os.Getenv("ADJ_DECIDED_BY")
	var projects *daemon.ProjectsResult
	var kept []daemon.RunParams
	for _, item := range params.Items {
		if missing[item.Item] {
			continue
		}
		r, ok := open[item.Item]
		if !ok {
			kept = append(kept, item)
			continue
		}
		var answers []string
		if supplied != nil {
			answers = supplied[item.Item]
		} else {
			answers = askSpecQuestions(in, w, r)
		}
		if answers == nil {
			fmt.Fprintf(w, "held: %s has %d open spec %s\n", r.Item, len(r.Questions), plural(len(r.Questions), "question"))
			held = append(held, r.Item)
			continue
		}
		if projects == nil {
			projects = &daemon.ProjectsResult{}
			if code := a.call(ctx, o, daemon.MethodProjectsList, nil, projects); code != ExitOK {
				return held, noSpec, code
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
			return held, noSpec, code
		}
		for _, n := range out.Notices {
			fmt.Fprintln(w, n)
		}
		for i, answer := range answers {
			flags.data.Answered = append(flags.data.Answered, specAccepted{Item: r.Item, Key: questionKey(r.Questions[i], i), Answer: answer})
		}
		kept = append(kept, item)
	}
	params.Items = kept
	return held, noSpec, ExitOK
}

func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// S312 (*proposed*): batch flags are handled entirely before the existing
// spec.answer path. A wave uses item-qualified addresses.
type specFlags struct {
	questions bool
	answers   []string
	wave      bool
	data      *specData
}

type specAccepted struct {
	Item   string `json:"item"`
	Key    string `json:"key"`
	Answer string `json:"answer"`
}

type specData struct {
	Questions []specListedQuestion `json:"questions"`
	Answered  []specAccepted       `json:"answered"`
}

type specListedQuestion struct {
	Item    string `json:"item"`
	Address string `json:"address"`
	engine.SpecQuestion
}

func newSpecData() *specData {
	return &specData{Questions: []specListedQuestion{}, Answered: []specAccepted{}}
}

func (f *specFlags) active() bool { return f.questions || len(f.answers) > 0 }
func (f *specFlags) result() *specData {
	if f.active() {
		return f.data
	}
	return nil
}

func (f *specFlags) register(fs *flag.FlagSet) {
	fs.BoolVar(&f.questions, "questions", false, "print all open spec questions and start nothing")
	fs.Func("answer", "answer KEY=N or KEY=text (wave: ITEM:KEY=value); repeat for each question", func(s string) error {
		f.answers = append(f.answers, s)
		return nil
	})
}

func (f *specFlags) validate(yes bool) error {
	if f.questions && yes {
		return fmt.Errorf("--questions cannot be used with --yes")
	}
	if f.questions && len(f.answers) > 0 {
		return fmt.Errorf("--questions cannot be used with --answer")
	}
	return nil
}

func questionKey(q engine.SpecQuestion, i int) string {
	if q.Key != "" {
		return q.Key
	}
	return fmt.Sprintf("#%d", i+1)
}

func (f *specFlags) address(item string, q engine.SpecQuestion, i int) string {
	key := questionKey(q, i)
	if f.wave {
		return item + ":" + key
	}
	return key
}

func (f *specFlags) unknownAnswer(key string) error {
	hint := ""
	if f.wave {
		hint = " (wave answers use ITEM:KEY)"
	}
	return fmt.Errorf("no open question matches %s%s", key, hint)
}

func (f *specFlags) mapAnswers(items []daemon.RunParams, open map[string]daemon.SpecItemResult) (map[string][]string, []string, error) {
	type target struct {
		item     string
		index    int
		question engine.SpecQuestion
	}
	targets := map[string]target{}
	answers := map[string][]string{}
	var order []string
	for _, item := range items {
		r, ok := open[item.Item]
		if !ok {
			continue
		}
		answers[r.Item] = make([]string, len(r.Questions))
		for i, q := range r.Questions {
			key := f.address(r.Item, q, i)
			if _, exists := targets[key]; exists {
				return nil, nil, fmt.Errorf("ambiguous question key %s", key)
			}
			targets[key] = target{r.Item, i, q}
			order = append(order, key)
		}
	}
	seen := map[string]bool{}
	for _, raw := range f.answers {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, nil, fmt.Errorf("answer %s needs KEY=value", key)
		}
		t, ok := targets[key]
		if !ok {
			return nil, nil, f.unknownAnswer(key)
		}
		if seen[key] {
			return nil, nil, fmt.Errorf("duplicate answer for %s", key)
		}
		seen[key] = true
		value = strings.Join(strings.Fields(value), " ")
		if value == "" {
			return nil, nil, fmt.Errorf("empty answer for %s", key)
		}
		if t.question.Ask != nil {
			if n, err := strconv.Atoi(value); err == nil || errors.Is(err, strconv.ErrRange) {
				if err != nil || n < 1 || n > len(t.question.Ask.Options) {
					return nil, nil, fmt.Errorf("answer for %s must be 1-%d or text", key, len(t.question.Ask.Options))
				}
				o := t.question.Ask.Options[n-1]
				value = o.Label + ": " + o.Summary
			}
		}
		// Match RecordSpecAnswers so JSON reports the text that is recorded.
		answers[t.item][t.index] = strings.Join(strings.Fields(value), " ")
	}
	var missing []string
	for _, key := range order {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	return answers, missing, nil
}

func printSpecQuestion(w io.Writer, r daemon.SpecItemResult, i int, q engine.SpecQuestion, interactive bool, address string) {
	printLine := func(format string, args ...any) {
		for _, line := range wrapLines(fmt.Sprintf(format, args...), 80, "  ") {
			fmt.Fprintln(w, line)
		}
	}
	prefix := fmt.Sprintf("[%s %d/%d] ", r.Item, i+1, len(r.Questions))
	if q.Ask == nil {
		printLine("%s%s", prefix, q.Text)
	} else {
		printLine("%s%s", prefix, q.Ask.Title)
		for n, o := range q.Ask.Options {
			mark := ""
			if n+1 == q.Ask.Recommend {
				mark = " (recommended)"
			}
			printLine("  %d. %s: %s%s", n+1, o.Label, o.Summary, mark)
		}
		printLine("  Why %d: %s", q.Ask.Recommend, q.Ask.Reason)
	}
	if interactive {
		if q.Ask != nil && q.Key != "" {
			printLine("  Decision key: %s", q.Key)
		}
	} else if q.Key == "" {
		printLine("  Answer as: %s", address)
	} else if address != q.Key {
		printLine("  Decision key: %s (answer as %s)", q.Key, address)
	} else {
		printLine("  Decision key: %s", q.Key)
	}
}

// askSpecQuestions shows every question of an item and reads one answer for
// each: a number picks an option, other text is the answer. A number outside
// the options is asked again. An empty line, or the end of the input, holds
// the item back and returns nil.
func askSpecQuestions(in *bufio.Reader, w io.Writer, r daemon.SpecItemResult) []string {
	fmt.Fprintf(w, "%s needs %d %s before it can start.\n", r.Item, len(r.Questions), plural(len(r.Questions), "answer"))
	var answers []string
	for i, q := range r.Questions {
		printSpecQuestion(w, r, i, q, true, "")
		if q.Ask == nil {
			fmt.Fprintf(w, "Answer in text (empty holds %s back): ", r.Item)
		} else {
			fmt.Fprintf(w, "Answer 1-%d, or type text (empty holds %s back): ", len(q.Ask.Options), r.Item)
		}
		answer := ""
		for answer == "" {
			line, err := readLine(in, w)
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
