// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// show prints a run's state, the whole pause reason, its role reports, checks
// and findings, and the pending role's newest report; with NAME it prints one
// report or check log (S297, *proposed*). It only reads, through run.show and
// run.report.
func (a *App) show(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("show: %v", err)
	}
	if len(pos) < 1 || len(pos) > 2 {
		return o.usage("show takes RUN [NAME]")
	}
	return a.session(ctx, o, func(call func(method string, params, result any) error) int {
		run := pos[0]
		if len(pos) == 2 {
			return showName(o, call, run, pos[1])
		}
		var reply daemon.ShowReply
		if err := call(daemon.MethodRunShow, daemon.RunAction{Run: run}, &reply); err != nil {
			return o.failErr(err)
		}
		if o.json {
			return o.printJSON(reply)
		}
		writeShow(o, reply)
		if reply.State == runrec.StatePaused && reply.LatestReport != "" {
			fmt.Fprintf(o.stdout, "-- %s --", reply.LatestReport)
			var rep daemon.ReportReply
			if err := call(daemon.MethodRunReport, daemon.RunAction{Run: run, Name: reply.LatestReport}, &rep); err != nil {
				fmt.Fprintf(o.stdout, " (cannot be read: %s)\n", ipc.ErrorOf(err).Message)
				fmt.Fprintf(o.stdout, "next: %s\n", reply.Next)
				return ExitOK
			}
			fmt.Fprintf(o.stdout, "\n%s\n", strings.TrimSuffix(rep.Text, "\n"))
		}
		if reply.State == runrec.StatePaused && reply.Next != "" {
			// Again after the report, so a long report does not scroll the command away.
			fmt.Fprintf(o.stdout, "next: %s\n", reply.Next)
		}
		return ExitOK
	})
}

// session runs f with one call function: the App's Call double, or one daemon
// connection for every call f makes.
func (a *App) session(ctx context.Context, o *output, f func(call func(method string, params, result any) error) int) int {
	if a.Call != nil {
		return f(func(method string, params, result any) error { return a.Call(ctx, method, params, result) })
	}
	return a.withDaemon(ctx, o, func(c *daemon.Client) int {
		return f(func(method string, params, result any) error { return c.Call(ctx, method, params, result) })
	})
}

// showName prints one named report or check log. An unknown name lists the
// names the run has, so the owner can pick one.
func showName(o *output, call func(method string, params, result any) error, run, name string) int {
	var rep daemon.ReportReply
	err := call(daemon.MethodRunReport, daemon.RunAction{Run: run, Name: name}, &rep)
	if err != nil {
		if e := ipc.ErrorOf(err); e.Code == daemon.CodeNotFound {
			var reply daemon.ShowReply
			if call(daemon.MethodRunShow, daemon.RunAction{Run: run}, &reply) == nil {
				names := showNames(reply)
				if slices.Contains(names, name) {
					// The record lists it; the file is gone or cannot be read.
					return o.fail(ExitFailure, e.Code, fmt.Sprintf("%s is listed for run %s but cannot be read", name, run))
				}
				list := "no reports or logs yet"
				if len(names) > 0 {
					list = "names: " + strings.Join(names, ", ")
				}
				return o.fail(ExitFailure, e.Code, fmt.Sprintf("no report or log %s for run %s; %s", name, run, list))
			}
		}
		return o.failErr(err)
	}
	if o.json {
		return o.printJSON(rep)
	}
	fmt.Fprintf(o.stdout, "%s\n", strings.TrimSuffix(rep.Text, "\n"))
	return ExitOK
}

// showNames lists the report and check log names ork show NAME can print.
func showNames(r daemon.ShowReply) []string {
	var names []string
	for _, role := range r.Roles {
		if role.Report != "" {
			names = append(names, role.Report)
		}
	}
	for _, c := range r.Checks {
		if c.Log != "" {
			names = append(names, c.Log)
		}
	}
	return names
}

// indent puts two spaces (or lead) before each line of text, with terminal
// controls taken out.
func indent(text, lead string) string {
	return lead + strings.ReplaceAll(runrec.PlainText(text), "\n", "\n"+lead)
}

// labeled is LEAD LABEL TEXT with the continuation lines of TEXT indented two
// spaces under the label, terminal controls taken out.
func labeled(lead, label, text string) string {
	return lead + label + strings.ReplaceAll(runrec.PlainText(text), "\n", "\n"+lead+"  ")
}

func writeShow(o *output, r daemon.ShowReply) {
	w := o.stdout
	fmt.Fprintf(w, "%s %s %s\n", r.ID, r.Item, r.State)
	if p := r.Pending; p != nil {
		head := p.Kind + " pause"
		if p.Role != "" {
			head += ", " + p.Role
			if p.Round > 0 {
				head += fmt.Sprintf(" round %d", p.Round)
			}
		}
		fmt.Fprintf(w, "%s\n", head)
	}
	if r.Reason != "" {
		if r.Pending == nil {
			fmt.Fprintln(w, "reason:")
		}
		fmt.Fprintln(w, indent(r.Reason, "  "))
	}
	if p := r.Pending; p != nil && p.Kind == runrec.PendingQuestion {
		writeQuestion(o, p)
	}
	if r.Pending != nil && r.Pending.Key != "" {
		fmt.Fprintf(w, "decision key: %s\n", runrec.PlainText(r.Pending.Key))
	}
	if r.Next != "" {
		fmt.Fprintf(w, "next: %s\n", r.Next)
	}
	if len(r.Roles) > 0 {
		fmt.Fprintln(w, "roles:")
	}
	for _, role := range r.Roles {
		line := fmt.Sprintf("  %s round %d attempt %d", role.Role, role.Round, role.Attempt)
		if role.Exit != "" {
			line += "  exit " + role.Exit
		}
		if role.Report != "" {
			line += "  report " + role.Report
		}
		denied := ""
		if role.Denied > 0 {
			names := make([]string, len(role.DeniedFirst))
			for i, n := range role.DeniedFirst {
				names[i] = runrec.PlainText(n)
			}
			denied = fmt.Sprintf("    denied %d: %s", role.Denied, strings.Join(names, ", "))
		}
		fmt.Fprintln(w, line)
		if denied != "" {
			fmt.Fprintln(w, denied)
		}
	}
	if len(r.Checks) > 0 {
		fmt.Fprintln(w, "checks:")
	}
	for _, c := range r.Checks {
		result := "red"
		if c.OK {
			result = "ok"
		}
		line := fmt.Sprintf("  round %d  %s  %ds", c.Round, result, c.Seconds)
		if c.Command != "" {
			line += "  " + runrec.PlainText(c.Command)
		}
		if c.Log != "" {
			line += "  log " + c.Log
		}
		fmt.Fprintln(w, line)
		for _, d := range c.Deferred {
			fmt.Fprintln(w, labeled("    ", "deferred: ", d))
		}
	}
	if len(r.Findings) > 0 {
		fmt.Fprintln(w, "findings:")
	}
	for _, f := range r.Findings {
		fmt.Fprintf(w, "  %d %s  %s\n", f.N, f.Kind, runrec.PlainText(f.Title))
		if f.Text != "" {
			fmt.Fprintln(w, indent(f.Text, "    "))
		}
		if f.Answer != "" {
			fmt.Fprintln(w, labeled("    ", "answer: ", f.Answer))
		}
		if f.Filed != "" {
			fmt.Fprintf(w, "    filed as %s\n", f.Filed)
		}
	}
	if len(showNames(r)) > 0 {
		fmt.Fprintf(w, "read a report or log: ork show %s NAME\n", r.ID)
	}
}

// writeQuestion prints a question pause: the stored text, and the structured
// question when that text does not already hold its options.
func writeQuestion(o *output, p *daemon.ShowPending) {
	w := o.stdout
	ask := p.Ask
	if p.Question == "" && ask == nil {
		return
	}
	fmt.Fprintln(w, "question:")
	if p.Question != "" {
		fmt.Fprintln(w, indent(p.Question, "  "))
	}
	if ask == nil || strings.Contains(p.Question, "Question: "+ask.Title) {
		return
	}
	fmt.Fprintf(w, "  Question: %s\n  Options:\n", runrec.PlainText(ask.Title))
	for i, opt := range ask.Options {
		fmt.Fprintf(w, "  %d. %s: %s\n", i+1, runrec.PlainText(opt.Label), runrec.PlainText(opt.Summary))
	}
	if ask.Recommend >= 1 {
		fmt.Fprintf(w, "  Recommend: %d - %s\n", ask.Recommend, runrec.PlainText(ask.Reason))
	}
}
