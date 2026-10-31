// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/roles"
)

// BriefSection is one headed part of a brief that is specific to the round.
type BriefSection struct{ Title, Body string }

// EarlierReport is a report of an earlier round, given to a role that is not
// resumed.
type EarlierReport struct{ Name, Text string }

// BriefInput is what a brief is built from (docs/design.md, Roles).
type BriefInput struct {
	Finish       bool   // the finish step has no checks after its round
	CheckCommand string // the project check command; empty means only slicer check
	Role         string
	Vendor       string   // the selected lineup vendor
	UIPassSkills []string // skills advised only for a Claude UI pass
	UIDoc        string   // the project's UI design doc (registry ui_doc); only the UI pass's brief names it
	SliceID      string
	Worktree     string
	TempDir      string
	ReportPath   string
	SliceJSON    string   // `slicer show ID --json --lean`
	EnvLines     []string // the project's environment lines, verbatim
	Sections     []BriefSection
	Earlier      []EarlierReport
}

const trackingRule = "Do not open, search, parse, or edit tracking files; use the slicer commands."

// BuildBrief returns the brief for a role: the role prompt with its
// placeholders filled, then the slice, the tracking rule, the workspace
// limit, the environment lines, the role's temp directory and report path,
// the implementer's check instructions, the round's own sections and any earlier reports.
func BuildBrief(in BriefInput) (string, error) {
	for _, f := range []struct{ name, val string }{
		{"SliceID", in.SliceID}, {"Worktree", in.Worktree}, {"TempDir", in.TempDir},
		{"ReportPath", in.ReportPath}, {"SliceJSON", in.SliceJSON},
	} {
		if f.val == "" {
			return "", fmt.Errorf("brief for %s: %s is empty", in.Role, f.name)
		}
	}
	prompt, err := roles.Prompt(in.Role)
	if err != nil {
		return "", err
	}
	prompt = strings.NewReplacer(
		"{{slice_id}}", in.SliceID,
		"{{worktree}}", in.Worktree,
		"{{temp_dir}}", in.TempDir,
		"{{report_path}}", in.ReportPath,
	).Replace(prompt)
	if i := strings.Index(prompt, "{{"); i >= 0 {
		return "", fmt.Errorf("brief for %s: prompt keeps a placeholder near %q", in.Role, prompt[i:min(len(prompt), i+24)])
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(prompt, "\n"))
	b.WriteString("\n\n# Brief\n\n")
	fmt.Fprintf(&b, "Slice: %s\nWorktree: %s\n\n", in.SliceID, in.Worktree)
	fmt.Fprintf(&b, "## Slice\n\n```json\n%s\n```\n\n", strings.TrimSpace(in.SliceJSON))
	fmt.Fprintf(&b, "## Tracking rule\n\n%s\n\n", trackingRule)
	fmt.Fprintf(&b, "## Workspace limit\n\nWork only in %s and stay inside the workspace: do not read or write outside it, except your temp directory and your report path.\n\n", in.Worktree)
	if len(in.EnvLines) > 0 {
		b.WriteString("## Environment\n\n")
		for _, l := range in.EnvLines {
			b.WriteString(l + "\n")
		}
		b.WriteString("\n")
	}
	if in.Role == RoleUI {
		if in.Vendor == "claude" && len(in.UIPassSkills) > 0 {
			fmt.Fprintf(&b, "## Skills\n\nLoad these skills for the UI pass: %s. If one is not available, say so in your report and go on.\n\n", strings.Join(in.UIPassSkills, ", "))
		}
		b.WriteString("## UI design doc\n\n")
		if in.UIDoc != "" {
			fmt.Fprintf(&b, "The project's UI design doc is `%s`. It is a path in the worktree. Judge the screens against it.\n\n", in.UIDoc)
		} else {
			b.WriteString("The registry names no UI design doc for this project. Find the project's own UI docs (start with its `AGENTS.md` and its docs directory) and its own view snapshots, and judge the screens against them.\n\n")
		}
	}
	fmt.Fprintf(&b, "## Your files\n\nYour temp directory: %s\nYour report path: %s\n\n", in.TempDir, in.ReportPath)
	if in.Role == RoleImplementer && !in.Finish {
		b.WriteString("## Checks ork runs\n\nork runs ")
		if in.CheckCommand != "" {
			fmt.Fprintf(&b, "`%s` and ", in.CheckCommand)
		}
		b.WriteString("`slicer check` after the round, outside your sandbox.\n\n")
	}
	for _, s := range in.Sections {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", s.Title, strings.TrimSpace(s.Body))
	}
	if len(in.Earlier) > 0 {
		b.WriteString("## Earlier reports\n\n")
		for _, r := range in.Earlier {
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", r.Name, strings.TrimSpace(r.Text))
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}
