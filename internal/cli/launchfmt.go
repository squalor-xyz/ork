// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func fileLine(detail *slicercli.Detail, err error) string {
	if err != nil {
		return fmt.Sprintf("files: unknown (%s)", err)
	}
	if daemon.EngineSlice(detail) {
		return "files: touches internal/engine, internal/daemon or cmd (engine slice)"
	}
	if detail != nil && detail.Slice != nil {
		for _, section := range detail.Slice.Sections {
			if strings.EqualFold(section.Heading, "Files") && strings.TrimSpace(section.Body) != "" {
				return "files: no engine path"
			}
		}
	}
	return "files: none listed"
}

// A failed detail read is diagnostic, not a launch failure. Capture call's
// structured error so JSON stdout still contains only the launch result.
func (a *App) launchDetail(ctx context.Context, o *output, project, item string) *slicercli.Detail {
	var reply daemon.ItemShowResult
	var diagnostic bytes.Buffer
	quiet := &output{stdout: &diagnostic, stderr: &diagnostic, json: true}
	var err error
	if a.call(ctx, quiet, daemon.MethodItemShow, daemon.ItemShowParams{Project: project, ID: item}, &reply) != ExitOK {
		var failure struct{ Error struct{ Message string } }
		if json.Unmarshal(diagnostic.Bytes(), &failure) == nil && failure.Error.Message != "" {
			err = fmt.Errorf("%s", failure.Error.Message)
		} else {
			err = fmt.Errorf("%s", strings.TrimSpace(diagnostic.String()))
		}
		reply.Detail = nil
	}
	if !o.json {
		fmt.Fprintln(o.stdout, fileLine(reply.Detail, err))
	}
	return reply.Detail
}

// planSettings returns a function that adds the project's push and UI pass
// skills to a plan for display. The daemon's run.plan carries only the project
// name and path, so the settings come from projects.list. Only the shown copy
// changes: the plan sent back as Expected stays as the daemon made it. When
// the list cannot be read, or in JSON mode, the plan is shown as it is.
func (a *App) planSettings(ctx context.Context, o *output, name string) func(daemon.RunPlan) daemon.RunPlan {
	same := func(plan daemon.RunPlan) daemon.RunPlan { return plan }
	if o.json {
		return same
	}
	var list daemon.ProjectsResult
	if a.call(ctx, &output{stdout: io.Discard, stderr: io.Discard, json: true}, daemon.MethodProjectsList, nil, &list) != ExitOK {
		return same
	}
	for _, p := range list.Projects {
		if p.Name == name {
			return func(plan daemon.RunPlan) daemon.RunPlan {
				plan.Project.Push, plan.Project.UIPassSkills = p.Push, p.UIPassSkills
				return plan
			}
		}
	}
	return same
}

func pairRule(items []string, engine map[string]bool) (line string, conflict string) {
	if len(items) < 2 {
		return "", ""
	}
	first := ""
	for _, item := range items {
		if engine[item] {
			if first != "" {
				return "", daemon.EngineConflict(first, item)
			}
			first = item
		}
	}
	if first != "" {
		return "pair rule: ok, one engine slice (" + first + ")", ""
	}
	return "pair rule: ok, no engine slice", ""
}

func printLaunch(o *output, project, id string, wave bool) {
	fmt.Fprintf(o.stdout, "follow: ork status --project %s\n", project)
	if wave {
		fmt.Fprintf(o.stdout, "stop the wave: ork stop %s\n", id)
	} else {
		fmt.Fprintf(o.stdout, "stop it: ork stop %s\n", id)
	}
}
