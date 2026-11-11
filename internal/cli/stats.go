// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"

	"github.com/squalor-xyz/ork/internal/slicercli"
)

// stats displays slicer's aggregates directly, without a daemon or local ledger.
func (a *App) stats(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	since := fs.String("since", "", "pass the date through to slicer")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("stats: %v", err)
	}
	if len(pos) > 1 {
		return o.usage("stats takes at most one PROJECT, got %d arguments", len(pos))
	}
	var root string
	if len(pos) == 1 {
		p, code, ok := a.registeredProject(o, pos[0])
		if !ok {
			return code
		}
		root = p.Path
	} else {
		getwd := a.Getwd
		if getwd == nil {
			getwd = os.Getwd
		}
		root, err = getwd()
		if err != nil {
			return o.failErr(err)
		}
	}
	stats, err := (slicercli.Client{Bin: a.Tools.Slicer}).RelayStats(ctx, root, *since)
	if err != nil {
		return o.failErr(err)
	}
	if o.json {
		return o.printJSON(stats)
	}
	printStats(o, stats)
	return ExitOK
}

// statsSeconds uses slicer's text units, including truncation below hours.
func statsSeconds(seconds *int) string {
	if seconds == nil {
		return "-"
	}
	s := *seconds
	if s >= 3600 {
		return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
	}
	if s >= 60 {
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%ds", s)
}

func printStats(o *output, s *slicercli.RelayStats) {
	fmt.Fprintf(o.stdout, "relay runs: %d (%d pass, %d fail, %d stopped, %d unended); %d measured\n", s.Runs.Total, s.Runs.Pass, s.Runs.Fail, s.Runs.Stopped, s.Runs.Unended, s.Measured)
	fmt.Fprintf(o.stdout, "wall time: median %s, longest %s\n", statsSeconds(s.WallSeconds.Median), statsSeconds(s.WallSeconds.Max))
	fmt.Fprintf(o.stdout, "fix rounds: %d (%d runs with any, %d with two or more)\n", s.FixRounds.Total, s.FixRounds.RunsWithAny, s.FixRounds.RunsWithTwoOrMore)
	fmt.Fprintf(o.stdout, "gates: %d rounds, %d FAIL; %d had a recorded verdict, %d counted by proxy\n", s.Gates.Rounds, s.Gates.Fails, s.Gates.WithVerdict, s.Gates.ByProxy)
	fmt.Fprintf(o.stdout, "owner wait: %d runs with a recorded wait, %s in all, median %s, longest %s\n", s.OwnerWait.RunsWithWait, statsSeconds(&s.OwnerWait.TotalSeconds), statsSeconds(s.OwnerWait.MedianSeconds), statsSeconds(s.OwnerWait.MaxSeconds))
	if len(s.Roles) == 0 {
		return
	}
	// Keep slicer's standard role order; append any new names deterministically.
	order := []string{"implementer", "reviewer", "gate", "architect", "orchestrator", "ui", "ui-pass"}
	var extra []string
	for name := range s.Roles {
		if !slices.Contains(order, name) {
			extra = append(extra, name)
		}
	}
	slices.Sort(extra)
	order = append(order, extra...)
	rows := [][]string{{"role", "steps", "total", "median"}}
	for _, name := range order {
		if role, ok := s.Roles[name]; ok {
			rows = append(rows, []string{name, fmt.Sprint(role.Steps), statsSeconds(&role.TotalSeconds), statsSeconds(role.MedianSeconds)})
		}
	}
	widths := make([]int, 4)
	for _, row := range rows {
		for i, col := range row {
			widths[i] = max(widths[i], len(col))
		}
	}
	for _, row := range rows {
		fmt.Fprintf(o.stdout, "%-*s  %*s  %*s  %*s\n", widths[0], row[0], widths[1], row[1], widths[2], row[2], widths[3], row[3])
	}
}
