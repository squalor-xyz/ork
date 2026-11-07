// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/roles"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// BriefSection is one headed part of a brief that is specific to the round.
type BriefSection struct{ Title, Body string }

// EarlierReport is a report of an earlier round, given to a role that is not
// resumed.
type EarlierReport struct{ Name, Text string }

// BriefInput is what a brief is built from (docs/design.md, Roles).
type BriefInput struct {
	Allow        []string // resolved sandbox command forms, verbatim
	Finish       bool     // the finish step has no checks after its round
	CheckCommand string   // the project check command; empty means only slicer check
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
	ReadOnly     bool   // the role cannot write its report path: it reports in its final message (S256, *proposed*)
	Resumed      bool   // ork resumed the role's earlier session (S256, *proposed*)
	Tree         string // `git status --porcelain` when the round started; read only for a resumed round
}

// fenceFor returns a code fence longer than any backtick run in text.
func fenceFor(text string) string {
	longest, run := 0, 0
	for _, c := range text {
		if c == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
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
	if in.ReadOnly {
		// A read-only role cannot write the report path; ork saves its final
		// message there (adj 27, role.report-io; S256, *proposed*).
		fmt.Fprintf(&b, "## Your files\n\nYour temp directory: %s\nYour report path: %s (ork saves your final message there; you cannot write it)\nGive your whole report as your final message, as plain text. Do not wrap it or any part of it in a code fence.\n\n", in.TempDir, in.ReportPath)
	} else {
		fmt.Fprintf(&b, "## Your files\n\nYour temp directory: %s\nYour report path: %s\n\n", in.TempDir, in.ReportPath)
	}
	if in.Resumed {
		b.WriteString("## Resumed session\n\nork resumed your earlier session. The worktree may have changed since your last report. Before you judge or change anything, run git status and git diff again and read again every file you cite. Do not repeat an earlier finding, verdict or answer without checking it against the tree as it is now.\n\n")
		if tree := strings.TrimRight(in.Tree, "\n"); tree != "" {
			fence := fenceFor(tree)
			fmt.Fprintf(&b, "git status --porcelain when this round started:\n\n%s\n%s\n%s\n\n", fence, tree, fence)
		} else {
			b.WriteString("The tree had no uncommitted changes when this round started.\n\n")
		}
	}
	if in.Role == RoleImplementer && !in.Finish {
		b.WriteString("## Checks ork runs\n\n")
		if strings.ContainsAny(in.CheckCommand, "`\n") {
			fence := fenceFor(in.CheckCommand)
			fmt.Fprintf(&b, "ork runs this command, then `slicer check`, after the round, outside your sandbox:\n\n%s\n%s\n%s\n\n", fence, in.CheckCommand, fence)
		} else {
			b.WriteString("ork runs ")
			if in.CheckCommand != "" {
				fmt.Fprintf(&b, "`%s` and ", in.CheckCommand)
			}
			b.WriteString("`slicer check` after the round, outside your sandbox.\n\n")
		}
	}
	b.WriteString(doneWhen(in))
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

// deferredSection tells a role about the slicer check problems the newest
// round check deferred (adj 178, S279). It is nil when that check deferred
// nothing. The reviewer and the gate name the problems in their reports as
// deferred, not as findings. The finish step's `--check` runs in the same
// behind worktree, so the implementer is told it may be red on exactly these.
func deferredSection(checks []runrec.CheckRecord, finish bool) *BriefSection {
	if len(checks) == 0 || len(checks[len(checks)-1].Deferred) == 0 {
		return nil
	}
	last := checks[len(checks)-1]
	body := "`slicer check` is red on the problems below. They name tracked items other than this slice, and the branch is behind its base branch, so main's code may be what fixes them. ork did not count them against this round. They are not findings against this diff: the landing merge and its re-check decide them.\n\n- " +
		strings.Join(last.Deferred, "\n- ") + "\n\n"
	if finish {
		body += "The `--check` in the finish step runs in the same worktree and may be red on exactly these problems and nothing else. That is expected: it is not an Owner needed item. If it is red on anything else, report that."
	} else {
		body += "If you mention them, name them in your report as deferred problems, not as findings. Do not fail the slice for them."
	}
	return &BriefSection{Title: "Deferred tracking problems", Body: body}
}

// doneWhen adds acceptance criteria without replacing any role's report format.
func doneWhen(in BriefInput) string {
	var items []string
	add := func(text string) { items = append(items, strings.Join(strings.Fields(text), " ")) }
	if in.Resumed {
		add("First re-run git status and git diff and re-read every cited file before judging or changing anything.")
	}
	if in.ReadOnly || readOnlyRole(in.Role) {
		add("You may only read the tree; no git or tracking writes. Give the whole report as your final message when the report path is read-only.")
		if in.Vendor == "grok" {
			add("Grok plan mode: the fixtures show git status --short, git diff -- PATH and git diff --check -- PATH running, plus the built-in file tools read_file, grep and list_dir. rg, slicer and test commands remain unverified.")
			add("Use one command per call. Do not use &&, ;, a pipe, echo, a redirect or python3 -c.")
			add("A command that needs approval ends the run and loses the report unless a finished report was already produced. Do not issue one. Give the complete report as your final message.")
		}
	} else {
		items = append(items, "Allowed forms, verbatim: "+strings.Join(in.Allow, "; ")+"; any other command is denied; no pipes, no cd, no env prefix unless an entry allows it.")
	}
	if in.Finish {
		for _, section := range in.Sections {
			if section.Title == "Finish step" {
				for _, line := range strings.Split(section.Body, "\n") {
					if strings.HasPrefix(line, "1. ") || strings.HasPrefix(line, "2. ") {
						add("Run in order: " + line)
					}
				}
			}
		}
		add("A denied finish command is not retried; report its verbatim command and denial under ## Owner needed as the orchestrator's to run (decision 93, engine.denials).")
		add("End the report with the exact heading ## Owner needed, or none beneath it when no question is open. Do not commit.")
	} else {
		switch in.Role {
		case RoleImplementer:
			add("Complete the report parts in the prompt in their stated order; docs updated with code in the same diff; self-check concurrency and handle lifetime, docs and mutation evidence.")
			add("Item 4 has the literal line Tests: N passed, M failed, with integer counts of tests actually run; nothing may follow the counts. End with the exact unnumbered heading ## Owner needed. Both lines stand alone; skipped tests go under item 6.")
			add("A denied or impossible check goes under item 6 with the verbatim command and denial text; never a made-up zero. Try allowed command forms; ork re-runs the checks outside the sandbox. Sandbox-limited checks are not owner questions.")
			sections := sliceChecklistSections(in.SliceJSON)
			for _, heading := range []string{"Failing tests", "Check"} {
				entries := checklistEntries(sections[heading])
				if len(entries) == 0 {
					add(heading + ": the slice lists none")
				}
				for _, entry := range entries {
					if heading == "Failing tests" {
						add(entry + ": red before the code, green after; mutation-check every new assertion.")
					} else if !strings.Contains(entry, "HUMAN CHECK") {
						add("Run or report blocked: " + entry)
					}
				}
			}
			for _, section := range in.Sections {
				switch section.Title {
				case "Review findings", "Gate FAIL", "Architect findings":
				default:
					continue
				}
				for _, line := range strings.Split(section.Body, "\n") {
					line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
					if finding, ok := strings.CutPrefix(line, "[fix] "); ok {
						add(finding + ": proving test or a stated reason it needs none; apply only this finding or explain with evidence why it is wrong.")
					}
				}
			}
		case RoleReviewer, RoleGate:
			add("Each [fix] has file:line and a reproduction or a test; each [later] has proposed Importance, Urgency and Effort scores (1 to 3). Apply owner answers and keep findings within the slice.")
			if in.Role == RoleReviewer {
				add("A tracking step denied to the implementer is a Deviation for the orchestrator, never a [fix]: one untagged line under section (C), not a finding or a reason to start a fix round.")
				add("Complete sections (A)-(D), marking findings CONFIRMED or PLAUSIBLE; end with the exact unnumbered heading ## Owner needed and questions or none. In gate mode use the gate's first line and one evidence line per criterion; put denied tracking in a Deviation for the orchestrator evidence line.")
			} else {
				add("Report first line exactly VERDICT: PASS or VERDICT: FAIL, with nothing else on that line; one evidence line per criterion. PASS requires resolved earlier blockers with proving tests, no [fix], no blocker owner finding, tests green, and docs match the code; otherwise FAIL.")
				add("For a tracking step denied to the implementer, add one Deviation for the orchestrator evidence line beside the criterion lines, never a [fix]; it is not a finding and starts no fix round.")
				add("Include the exact standalone line Tests: N passed, M failed with integer counts of tests actually run and nothing after the counts; end with the exact unnumbered heading ## Owner needed and questions or none.")
			}
			add("Report blocked checks with the verbatim command and denial text, apart from test counts; never claim an unrun check passed.")
		case RoleArchitect:
			add("Support placement, ownership, rollback, test-shape and direction findings with file:line and CONFIRMED or PLAUSIBLE; separate change-now from later. On second check, mark every earlier finding CONFIRMED-FIXED, PARTIAL or NOT-FIXED with evidence.")
			add("End the review with blockers or no blockers, then the exact unnumbered heading ## Owner needed with questions or none; in gate mode use its first line and evidence per criterion.")
			add("Name blocked checks and their verbatim command and denial text; do not claim unrun checks.")
		case RoleUI:
			add("Complete sections (A)-(D): findings name the view, size, visible defect, file:line, concrete edit and CONFIRMED or PLAUSIBLE. Produce the required renders or explain each blocked view; list the files produced.")
			add("End with the exact unnumbered heading ## Owner needed and questions or none; give no verdict. Each [later] has proposed Importance, Urgency and Effort scores (1 to 3).")
			add("Name checks or views you could not run or see, with the verbatim command and denial text when blocked.")
		case RoleSpecCheck:
			add("Give one ## Slice ID section per slice in order, with Ready or structured owner questions. Only an unresolved owner decision is a question; close code-readable gaps yourself and respect settled decisions.")
			add("End with the exact unnumbered heading ## Owner needed and none beneath it; questions belong under their slices. Use that final section only for a whole-check blocker.")
			add("State any unreadable slice or blocked read with its verbatim command and denial text; never invent readiness.")
		}
	}
	// Keep even large slice sections below 40 physical lines without dropping items.
	if len(items) > 36 {
		items = append(items[:35], strings.Join(items[35:], " | "))
	}
	return "## Done when\n\n- [ ] " + strings.Join(items, "\n- [ ] ") + "\n\n"
}

func sliceChecklistSections(raw string) map[string]string {
	var detail struct {
		Slice struct {
			Sections []struct{ Heading, Body string }
		}
	}
	sections := make(map[string]string)
	if json.Unmarshal([]byte(raw), &detail) == nil {
		for _, section := range detail.Slice.Sections {
			sections[section.Heading] = section.Body
		}
	}
	return sections
}

// checklistEntries keeps continuation lines with their bullet, including commands
// on indented lines. Unindented prose ends the current entry. Sections without
// bullets remain one item rather than disappearing.
func checklistEntries(body string) []string {
	var entries []string
	active := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "- ") {
			entries = append(entries, strings.TrimPrefix(line, "- "))
			active = true
		} else if strings.TrimSpace(line) == "" {
			continue
		} else if active && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			entries[len(entries)-1] += " " + strings.TrimSpace(line)
		} else {
			active = false
		}
	}
	if len(entries) == 0 && strings.TrimSpace(body) != "" {
		entries = append(entries, body)
	}
	return entries
}
