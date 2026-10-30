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

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
)

func (a *App) adj(ctx context.Context, o *output, args []string) int {
	if len(args) == 0 {
		return o.usage("adj: takes answer or ask")
	}
	switch args[0] {
	case "answer":
		return a.adjAnswer(ctx, o, args[1:])
	case "ask":
		return a.adjAsk(ctx, o, args[1:])
	default:
		return o.usage("adj: unknown command %q", args[0])
	}
}

func (a *App) adjAnswer(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("adj answer", flag.ContinueOnError)
	p := daemon.AdjAnswerParams{Via: "cli"}
	fs.StringVar(&p.Option, "option", "", "option label")
	fs.StringVar(&p.Text, "text", "", "typed answer")
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("adj answer: %v", err)
	}
	if len(pos) != 2 {
		return o.usage("adj answer: takes PROJECT ID and --option LABEL or --text TEXT")
	}
	p.Project = pos[0]
	p.ID, err = strconv.Atoi(pos[1])
	if err != nil || p.ID <= 0 {
		return o.usage("adj answer: ID must be a positive number")
	}
	if (p.Option == "") == (p.Text == "") {
		return o.usage("adj answer: give exactly one of --option and --text")
	}
	getenv := a.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	p.By = getenv("ADJ_DECIDED_BY")
	if err := engine.ValidateRecordAnswer(engine.AnswerRecordConfig{ID: p.ID, Option: p.Option, Text: p.Text, By: p.By, Via: p.Via}); err != nil {
		return o.fail(ExitFailure, daemon.CodeBadParams, "adj answer: "+err.Error())
	}
	var snap daemon.SnapshotResult
	if code := a.call(ctx, o, daemon.MethodSnapshot, daemon.SnapshotParams{Project: p.Project}, &snap); code != 0 {
		return code
	}
	if snap.Open.Error != nil {
		return o.failErr(snap.Open.Error)
	}
	var record *adjcli.Record
	for i := range snap.Open.Value {
		r := &snap.Open.Value[i]
		if r.ID == p.ID && r.Status == "open" {
			record = r
			break
		}
	}
	if record == nil {
		return o.fail(ExitFailure, daemon.CodeNotFound, fmt.Sprintf("adj answer: #%d is not in project %s's open records", p.ID, p.Project))
	}
	if p.Option != "" {
		found := false
		for _, opt := range record.Options {
			if opt.Label == p.Option {
				found = true
				break
			}
		}
		if !found {
			return o.fail(ExitFailure, daemon.CodeBadParams, fmt.Sprintf("adj answer: #%d has no option %q", p.ID, p.Option))
		}
	}
	prompts := o.stdout
	if o.json {
		prompts = o.stderr
	}
	fmt.Fprintf(prompts, "#%d %s\n", record.ID, record.Title)
	for _, opt := range record.Options {
		suffix := ""
		if record.Recommendation != nil && record.Recommendation.Label == opt.Label {
			suffix = " (recommended)"
		}
		fmt.Fprintf(prompts, "  %s: %s%s\n", opt.Label, opt.Summary, suffix)
	}
	answer := p.Option
	if answer == "" {
		answer = strconv.Quote(p.Text)
	}
	fmt.Fprintf(prompts, "Decide #%d as %s in adj as %s? [y/N] ", p.ID, answer, p.By)
	input := a.Input
	if input == nil {
		input = os.Stdin
	}
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && err != io.EOF {
		return o.failErr(err)
	}
	if yesNo(strings.TrimSpace(line)) != "yes" {
		if o.json {
			return o.printJSON(struct {
				Cancelled bool `json:"cancelled"`
			}{true})
		}
		fmt.Fprintln(o.stdout, "cancelled")
		return ExitOK
	}
	var out daemon.AdjAnswerResult
	if code := a.call(ctx, o, daemon.MethodAdjAnswer, p, &out); code != 0 {
		return code
	}
	if o.json {
		return o.printJSON(out)
	}
	fmt.Fprintln(o.stdout, out.Message)
	return ExitOK
}

func (a *App) adjAsk(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("adj ask", flag.ContinueOnError)
	p := daemon.AdjAskParams{}
	var options, tradeoffs, refs, tags []string
	fs.StringVar(&p.Key, "key", "", "decision key")
	fs.StringVar(&p.Context, "context", "", "context, or - to read stdin")
	fs.StringVar(&p.Recommend, "recommend", "", "recommended option label")
	fs.StringVar(&p.Reasoning, "reasoning", "", "recommendation reasoning")
	fs.Func("option", "LABEL:SUMMARY", func(v string) error { options = append(options, v); return nil })
	fs.Func("tradeoffs", "LABEL:TEXT", func(v string) error { tradeoffs = append(tradeoffs, v); return nil })
	fs.Func("ref", "reference", func(v string) error { refs = append(refs, v); return nil })
	fs.Func("tag", "tag", func(v string) error { tags = append(tags, v); return nil })
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parseAdjAsk(fs, args)
	if err != nil {
		return o.usage("adj ask: %v", err)
	}
	if len(pos) != 2 {
		return o.usage("adj ask: takes PROJECT TITLE; quote TITLE")
	}
	p.Project, p.Title = pos[0], pos[1]
	p.Refs, p.Tags = refs, tags
	for _, raw := range options {
		label, summary, ok := strings.Cut(raw, ":")
		if !ok || strings.TrimSpace(label) == "" || strings.TrimSpace(summary) == "" {
			return o.usage("adj ask: --option needs LABEL:SUMMARY")
		}
		for _, opt := range p.Options {
			if opt.Label == label {
				return o.usage("adj ask: duplicate option %q", label)
			}
		}
		p.Options = append(p.Options, adjcli.AskOption{Label: label, Summary: summary})
	}
	for _, raw := range tradeoffs {
		label, text, ok := strings.Cut(raw, ":")
		if !ok || strings.TrimSpace(text) == "" {
			return o.usage("adj ask: --tradeoffs needs LABEL:TEXT")
		}
		found := false
		for i := range p.Options {
			if p.Options[i].Label == label {
				p.Options[i].Tradeoffs = text
				found = true
				break
			}
		}
		if !found {
			return o.usage("adj ask: tradeoffs label %q is not an option", label)
		}
	}
	if err := engine.ValidateRecordAsk(adjcli.AskSpec{Title: p.Title, Options: p.Options, Recommend: p.Recommend, Reasoning: p.Reasoning}); err != nil {
		return o.usage("adj ask: %v", err)
	}
	if p.Context == "-" {
		input := a.Input
		if input == nil {
			input = os.Stdin
		}
		b, err := io.ReadAll(input)
		if err != nil {
			return o.failErr(err)
		}
		p.Context = string(b)
	}
	getenv := a.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	p.By = getenv("ADJ_ASKED_BY")
	if p.By == "" {
		p.By = getenv("ADJ_DECIDED_BY")
	}
	var out daemon.AdjAskResult
	if code := a.call(ctx, o, daemon.MethodAdjAsk, p, &out); code != 0 {
		return code
	}
	if o.json {
		return o.printJSON(out)
	}
	fmt.Fprintln(o.stdout, out.ID)
	return ExitOK
}

// parseAdjAsk keeps TITLE intact so validation can explain a leading dash.
func parseAdjAsk(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	args = fs.Args()
	if len(args) == 0 {
		return nil, nil
	}
	pos := []string{args[0]}
	args = args[1:]
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return pos, nil
	}
	pos = append(pos, args[0])
	if err := fs.Parse(args[1:]); err != nil {
		return nil, err
	}
	return append(pos, fs.Args()...), nil
}
