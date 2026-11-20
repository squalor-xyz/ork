// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// Summary rows are shared by text and JSON. More counts omitted rows.
// Proposed CLI shape (S318); it adds no daemon method or dependency logic.
type summarySection struct {
	Rows []string `json:"rows"`
	More int      `json:"more"`
}

type statusSummary struct {
	Project  string         `json:"project"`
	Running  summarySection `json:"running"`
	NeedsYou summarySection `json:"needs_you"`
	Blocked  summarySection `json:"blocked"`
	Next     summarySection `json:"next"`
}

func (s *summarySection) add(row string) {
	if len(s.Rows) < 5 {
		s.Rows = append(s.Rows, strings.ReplaceAll(runrec.PlainText(row), "\n", " "))
	} else {
		s.More++
	}
}

func (a *App) statusSummary(ctx context.Context, o *output, res daemon.StatusResult, name string) int {
	var selected *daemon.ProjectStatus
	if name != "" {
		for i := range res.Projects {
			if res.Projects[i].Name == name {
				selected = &res.Projects[i]
				break
			}
		}
	} else {
		getwd := a.Getwd
		if getwd == nil {
			getwd = os.Getwd
		}
		cwd, err := getwd()
		if err != nil {
			return o.failErr(err)
		}
		for i := range res.Projects {
			p := &res.Projects[i]
			if p.Path == "" {
				continue
			}
			rel, err := filepath.Rel(p.Path, cwd)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && (selected == nil || len(p.Path) > len(selected.Path)) {
				selected = p
			}
		}
	}
	if selected == nil || selected.Unavailable {
		return o.fail(ExitFailure, daemon.CodeNotFound, "status --summary needs a registered current project; use --project P")
	}
	p := *selected
	// Only items (held flags) and open adj records feed this summary.
	for _, err := range p.Errors {
		if err.Part == "items" || err.Part == "open" {
			return o.fail(ExitFailure, err.Code, err.Message)
		}
	}
	c := slicercli.Client{Bin: a.Tools.Slicer}
	status, err := c.Status(ctx, p.Path)
	if err != nil {
		return o.failErr(err)
	}
	next, err := c.Deps(ctx, p.Path)
	if err != nil {
		return o.failErr(err)
	}
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	s := buildStatusSummary(p, *status, next, now)
	if o.json {
		return o.printJSON(s)
	}
	fmt.Fprintln(o.stdout, runrec.PlainText(s.Project))
	for _, section := range []struct {
		name string
		data summarySection
	}{{"Running", s.Running}, {"Needs you", s.NeedsYou}, {"Blocked", s.Blocked}, {"Next", s.Next}} {
		fmt.Fprintln(o.stdout, section.name)
		if len(section.data.Rows) == 0 {
			fmt.Fprintln(o.stdout, "  none")
		}
		for _, row := range section.data.Rows {
			for _, line := range wrapLines("  "+row, 80, "  ") {
				fmt.Fprintln(o.stdout, line)
			}
		}
		if section.data.More > 0 {
			fmt.Fprintf(o.stdout, "  +%d more\n", section.data.More)
		}
	}
	return ExitOK
}

func buildStatusSummary(p daemon.ProjectStatus, status slicercli.Status, next []slicercli.Item, now time.Time) statusSummary {
	s := statusSummary{Project: p.Name, Running: summarySection{Rows: []string{}}, NeedsYou: summarySection{Rows: []string{}}, Blocked: summarySection{Rows: []string{}}, Next: summarySection{Rows: []string{}}}
	if len(p.Open) > 0 {
		s.NeedsYou.add(fmt.Sprintf("adj: %d open %s (adj inbox --root %s)", len(p.Open), plural(len(p.Open), "question"), p.Path))
	}
	questions := map[string]int{}
	for _, q := range p.SpecQuestions {
		questions[q.Item] = len(q.Keys)
		s.NeedsYou.add(fmt.Sprintf("%s: %d open spec %s (%s)", q.Item, len(q.Keys), plural(len(q.Keys), "question"), q.List))
	}
	for _, r := range p.EngineRuns {
		switch r.Section(p.EngineRuns) {
		case source.SectionRunning:
			row := fmt.Sprintf("%s %s %s", r.ID, r.Item, r.State)
			for _, w := range r.Working {
				vendor := ""
				for _, role := range r.Roles {
					if role.EndedAt.IsZero() && strings.HasPrefix(w.What, role.Role+" round ") {
						vendor = role.Vendor
					}
				}
				if vendor != "" {
					row += "; " + vendor
				}
				row += "; " + workingLine(w, now)
			}
			if len(r.Working) == 0 {
				since := r.UpdatedAt
				if since.IsZero() {
					since = r.CreatedAt
				}
				if !since.IsZero() {
					row += "; " + workingDuration(now.Sub(since))
				}
			}
			s.Running.add(row)
		case source.SectionNeeds:
			command := "ork show " + r.ID
			if r.State == runrec.StateAwaitingApproval {
				command = "ork approve " + r.ID
			}
			s.NeedsYou.add(fmt.Sprintf("%s %s %s (%s)", r.ID, r.Item, r.State, command))
		}
		checks := r.HumanChecks
		if checks == nil && r.Card != nil {
			checks = r.Card.HumanChecks
		}
		if !r.Dismissed && !r.Superseded(p.EngineRuns) {
			for i, check := range checks {
				if check.State == runrec.CheckLater {
					command := fmt.Sprintf("ork check %s %d", r.ID, i+1)
					if r.State != runrec.StateAwaitingApproval {
						command = "ork show " + r.ID
					}
					s.NeedsYou.add(fmt.Sprintf("%s %s: human check %d later: %s (%s)", r.ID, r.Item, i+1, check.Text, command))
				}
			}
		}
	}
	for _, b := range status.Blocked {
		s.Blocked.add(b.ID + " waits on " + strings.Join(b.WaitingOn, ", "))
	}
	for _, h := range p.Held {
		s.Blocked.add(h.ID + " " + h.Title + " (needs-owner)")
	}
	seen := map[string]bool{}
	for _, h := range p.Held {
		seen[h.ID] = true
	}
	addNext := func(it slicercli.Item) {
		if seen[it.ID] {
			return
		}
		seen[it.ID] = true
		row := it.ID + " " + it.Title
		if n := questions[it.ID]; n > 0 {
			row += fmt.Sprintf(" (waits on %d spec %s)", n, plural(n, "question"))
		}
		s.Next.add(row)
	}
	if status.Next != nil {
		addNext(*status.Next)
	}
	for _, it := range next {
		addNext(it)
	}
	return s
}
