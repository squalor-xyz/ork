// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/squalor-xyz/ork/internal/roles"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// BriefSection is one headed part of a brief that is specific to the round.
type BriefSection struct{ Title, Body string }

var blankLine = regexp.MustCompile(`\n[ \t]*\n`)

// ownerDecisionParagraphs returns the paragraphs of text, split on blank lines,
// that start with the "Owner decisions (" header RecordSpecAnswers writes. A
// paragraph holds the header line and one line for each question.
func ownerDecisionParagraphs(text string) []string {
	var out []string
	for _, paragraph := range blankLine.Split(text, -1) {
		if strings.HasPrefix(strings.TrimSpace(paragraph), "Owner decisions (") {
			out = append(out, paragraph)
		}
	}
	return out
}

// ownerDecisionBody checks only markers in RecordSpecAnswers' paragraphs.
// An adj id already identifies a record; other keys may never have been filed.
// A project without adj is not checked, and a failed check adds a soft line:
// it never fails the round.
func (e *Engine) ownerDecisionBody(ctx context.Context, text string) string {
	body := text
	if !e.proj.Adj {
		return body
	}
	marker := regexp.MustCompile(`\[([a-z][a-z0-9_-]*(?:\.[a-z0-9_-]+)+)(, adj [0-9]+)?\]`)
	seen := map[string]bool{}
	for _, paragraph := range ownerDecisionParagraphs(text) {
		for _, match := range marker.FindAllStringSubmatch(paragraph, -1) {
			key := match[1]
			if match[2] != "" || seen[key] {
				continue
			}
			seen[key] = true
			state, _, err := e.d.Adj.Check(ctx, e.wr, e.AdjRoot(), key)
			if err != nil {
				body += "\nadj check " + key + ": could not be read"
			} else if state == "not found" {
				body += "\nadj check " + key + ": not found"
			}
		}
	}
	return body
}

// EarlierReport is a report of an earlier round, given to a role that is not
// resumed.
type EarlierReport struct{ Name, Text string }

// BriefInput is what a brief is built from (docs/design.md, Roles).
type BriefInput struct {
	RenderCLI          bool // the worktree has TestRenderCLIViews
	RenderTUI          bool // the worktree has TestRenderTUIViews
	TreeUnchangedSince int  // previous read-only round with the same full-content fingerprint
	SliceDone          bool // the slice's tracking status is done (S455, adj 645, *proposed*)
	FixRoundSince      bool // an implementer round ran after the role's last report (S455, adj 645, *proposed*)

	Orientation  string   // saved spec-check pack, shown only to a claude-local implementer
	ProgressFile string   // exact file the local role may write outside its worktree
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
	TMPDir       string // process temporary directory; empty uses TempDir
	ReportPath   string
	SliceJSON    string   // `slicer show ID --json --lean`
	EnvLines     []string // the project's environment lines, verbatim
	Sections     []BriefSection
	Earlier      []EarlierReport
	ReadOnly     bool   // the role cannot write its report path: it reports in its final message (S256, *proposed*)
	Resumed      bool   // ork resumed the role's earlier session (S256, *proposed*)
	Tree         string // `git status --porcelain` when the round started; read only for a resumed round
	Lead         bool   // the implementer's slot is the lead of a split run: the lead's prompt (S61, *proposed*)
	Carve        bool   // the spec-check role carves the slice into slivers: the carve prompt and its checklist (S359, *proposed*)
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

const originalReportSentence = "write your final report to {{report_path}} and exit. If you cannot\nwrite that file, put the whole report in your final message instead."

func readOnlyReportPrompt(role, prompt string) (string, error) {
	if !strings.Contains(prompt, originalReportSentence) {
		return "", fmt.Errorf("brief for %s: prompt is missing the report sentence", role)
	}
	return strings.Replace(prompt, originalReportSentence, "give your whole report as your final message and exit; ork saves it to {{report_path}}.", 1), nil
}

const deniedCommandForms = "Run one plain command per call: no pipes, && or ; chains, redirects, command substitution, shell variables, env prefixes or cd. Use literal absolute paths with git -C and go -C. Do not end a `go test -run` pattern with `$`: that command is denied because it cannot be checked before it runs. Use `-run NAME` with the full test name and no anchor; it also runs any test whose name starts with NAME."

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
	promptName := in.Role
	if in.Lead && in.Role == RoleImplementer {
		promptName = RoleLead // the implementer's slot in a split run, with the lead's prompt (S61)
	}
	prompt, err := roles.Prompt(promptName)
	if err != nil {
		return "", err
	}
	if in.Carve {
		if in.Role != RoleSpecCheck {
			return "", fmt.Errorf("brief for %s: only the spec-check role carves", in.Role)
		}
		prompt = roles.CarvePrompt()
	}
	localImplementer := in.Role == RoleImplementer && !in.Lead && in.Vendor == "claude-local"
	if localImplementer {
		prompt, err = roles.LocalImplementerPrompt()
		if err != nil {
			return "", err
		}
		line, rest, _ := strings.Cut(prompt, "\n")
		prompt = line + "\n\n## Tool rules\n\nAllowed forms, verbatim: " + strings.Join(in.Allow, "; ") + "; any other command is denied. " + deniedCommandForms + " An env prefix is allowed only where an allowed form starts with one. Change files with the Edit and Write tools only.\n\n## Note first\n\nWrite your progress note now to `" + in.ProgressFile + "` with Edit or Write, before reading any file. This exact file is an exception to the workspace limit. Record done, next and open problems, even when nothing is done yet. After each step, update the note. Append an anchor log every few reads (`file:line symbol: purpose`); keep existing anchors when updating the note. ork may start a fresh session past 70,000 context tokens; the note preserves your work.\n" + rest
	}
	if in.ReadOnly {
		prompt, err = readOnlyReportPrompt(in.Role, prompt)
		if err != nil {
			return "", err
		}
	}
	processTemp := in.TMPDir
	if processTemp == "" {
		processTemp = in.TempDir
	}
	prompt = strings.NewReplacer(
		"{{slice_id}}", in.SliceID,
		"{{worktree}}", in.Worktree,
		"{{temp_dir}}", processTemp,
		"{{renders_dir}}", filepath.Join(in.TempDir, "renders"),
		"{{report_path}}", in.ReportPath,
	).Replace(prompt)
	if i := strings.Index(prompt, "{{"); i >= 0 {
		return "", fmt.Errorf("brief for %s: prompt keeps a placeholder near %q", in.Role, prompt[i:min(len(prompt), i+24)])
	}

	// A reviewer may write its own process temp dir (S510,
	// roles.read-only-temp-write, *proposed*); the daemon's claude runner
	// grants it under the same condition. Codex and Grok reviewers get no
	// write grant, so their brief must not say they may write.
	reviewerWritesTMP := in.ReadOnly && in.Role == RoleReviewer && in.TMPDir != "" && (in.Vendor == "claude" || in.Vendor == "claude-local")

	var b strings.Builder
	b.WriteString(strings.TrimRight(prompt, "\n"))
	b.WriteString("\n\n# Brief\n\n")
	fmt.Fprintf(&b, "Slice: %s\nWorktree: %s\n\n", in.SliceID, in.Worktree)
	fmt.Fprintf(&b, "## Slice\n\n```json\n%s\n```\n\n", strings.TrimSpace(in.SliceJSON))
	if localImplementer && strings.TrimSpace(in.Orientation) != "" {
		fmt.Fprintf(&b, "## Orientation\n\n%s\n\n", strings.TrimSpace(in.Orientation))
	}
	fmt.Fprintf(&b, "## Tracking rule\n\n%s\n\n", trackingRule)
	if localImplementer && in.ProgressFile != "" {
		fmt.Fprintf(&b, "## Workspace limit\n\nWork only in %s and stay inside the workspace: do not read or write outside it, except your temp directory, your report path and your progress note `%s`.\n\n", in.Worktree, in.ProgressFile)
	} else {
		fmt.Fprintf(&b, "## Workspace limit\n\nWork only in %s and stay inside the workspace: do not read or write outside it, except your temp directory and your report path.\n\n", in.Worktree)
	}
	if in.ProgressFile != "" && in.Vendor == "claude-local" && !localImplementer {
		exception, only := "The sole exception", "this note is the only file it may change"
		if reviewerWritesTMP {
			exception, only = "Besides your temp directory, the sole exception", "this note and the files in its temp directory are the only files it may change"
		}
		fmt.Fprintf(&b, "## Local progress note\n\n%s to the read-only and workspace rules above is `%s`. After each step, replace this short note with what is done, what is next, and any open problems. Update the note with Edit or Write. For a read-only role, %s. ork may end this session when its context passes 70,000 tokens and start a fresh session with the same brief and this note. Keep the note current.\n\n", exception, in.ProgressFile, only)
	}
	if IsLocalVendor(in.Vendor) {
		if in.Role == RoleImplementer {
			b.WriteString("## Local role\n\nRead files in targeted ranges (offset and limit), or grep first. Never read a whole file over about 150 lines; Grep for the symbol and Read 150 lines around it. Keep reasoning brief.\n\n")
		} else {
			b.WriteString("## Local role\n\nRead files in targeted ranges (offset and limit), or grep first. Never read a whole file over about 500 lines. Keep reasoning brief.\n\n")
		}
	}
	if len(in.EnvLines) > 0 {
		b.WriteString("## Environment\n\n")
		for _, l := range in.EnvLines {
			b.WriteString(l + "\n")
		}
		b.WriteString("\n")
	}
	if in.Role == RoleUI {
		if in.Vendor == "claude" {
			fmt.Fprintf(&b, "The UI renders directory `%s` is also an exception to the workspace limit. Keep renders there, separately from TMPDIR.\n\n", filepath.Join(in.TempDir, "renders"))
		}
		renders := filepath.Join(in.TempDir, "renders")
		if in.RenderCLI {
			b.WriteString("## CLI text renders\n\nFor ork CLI views, run this from the worktree with the test flag:\n\n")
			fmt.Fprintf(&b, "`go test ./internal/cli -run TestRenderCLIViews -renders %s`\n\n", ShellWord(renders))
			b.WriteString("This writes status.txt, status-summary.txt, status-summary-long.txt, show.txt and help.txt into the renders directory, exactly as printed, without changing the tree. To write other views, add `-render-fixture NAME[,NAME...]` after `-renders`; it writes only NAME.txt for each name. The names are the five above (without .txt) and projects-set, projects-set-refusals, wave-start, status-queued-held, status-engine, summary-real, projects-setup, fake-reply, matrix-copy-preview, matrix-copy-ask, matrix-copy-stale-preview, wave-start-finding, wait-block, wait-lines and wait-stop. ork's approval card lists every `.txt` file in the renders directory.\n\n")
		}
		if in.RenderTUI {
			b.WriteString("## TUI renders\n\nFor ork TUI views, run this from the worktree with the test flag:\n\n")
			fmt.Fprintf(&b, "`go test ./internal/tui -run TestRenderTUIViews -renders %s`\n\n", ShellWord(renders))
			b.WriteString("This writes NAME-80x24.txt, NAME-120x40.txt and NAME-ascii.txt for each default fixture into the renders directory, as plain text, without changing the tree. The default fixtures are overview, roadmap, launch-confirm, approval-card and approval-render. To write others, add `-render-fixture NAME[,NAME...]`; it writes only those fixtures. To write another size, add `-render-size WxH[,WxH...]`; it writes one more NAME-WxH.txt for each size. To read a view without writing a file, add `-render-print` and `-v`; it prints each text to stdout under a `## NAME-SIZE` line. A TUI view with no fixture is not rendered: say so in your report.\n\n")
		}
		if !in.RenderCLI || !in.RenderTUI {
			missing := "CLI and TUI"
			if in.RenderCLI {
				missing = "TUI"
			} else if in.RenderTUI {
				missing = "CLI"
			}
			fmt.Fprintf(&b, "## Renders\n\nork's render test for the %s views is not in this worktree. Find how the project renders its views in its `AGENTS.md` and docs, and use only that tooling. Write the files into the renders directory `%s`. A view that tooling cannot render without changing the tree gets no file: say so in your report.\n\n", missing, renders)
		}
		fmt.Fprintf(&b, "## Test run patterns\n\nRun one test per command: `go -C %s test -run NAME PKG`", in.Worktree)
		if in.RenderCLI {
			fmt.Fprintf(&b, ", for example `go -C %s test -run TestRenderCLIViews ./internal/cli`", in.Worktree)
		}
		b.WriteString(", then the next test in its own command. A `go test -run` pattern must not contain a pipe: a pipe makes a compound command, and the permission check denies it as multiple operations. Do not join test names in one pattern.\n\n")
		if (in.Vendor == "claude" || in.Vendor == "claude-local") && len(in.UIPassSkills) > 0 {
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
		fmt.Fprintf(&b, "## Your files\n\nYour temp directory: %s\nYour report path: %s (ork saves your final message there; you cannot write it)\nGive your whole report as your final message, as plain text. Do not wrap it or any part of it in a code fence.\n\n", processTemp, in.ReportPath)
		if reviewerWritesTMP {
			b.WriteString("You may create and change files under your temp directory with Edit or Write (for example a probe test). That is the only place you may write; the tree stays unchanged.\n\n")
		}
	} else {
		fmt.Fprintf(&b, "## Your files\n\nYour temp directory: %s\nYour report path: %s\n\n", processTemp, in.ReportPath)
	}
	if in.Resumed {
		if in.TreeUnchangedSince > 0 {
			// The status and fix-round sentences say what happened since the
			// role's last report (S455, adj 645, *proposed*).
			since := " No fix round has run since then."
			if in.FixRoundSince {
				since = " A fix round ran after that report and left the tree unchanged."
			}
			done := ""
			if in.SliceDone {
				done = " The slice is marked done."
			}
			fmt.Fprintf(&b, "## Resumed session\n\nork resumed your earlier session. The tree is unchanged since round %d, when you last reported.%s%s Before you judge or change anything, read again every file you cite. Do not repeat an earlier finding, verdict or answer without checking it against the tree as it is now.\n\n", in.TreeUnchangedSince, done, since)
		} else {
			b.WriteString("## Resumed session\n\nork resumed your earlier session. The worktree may have changed since your last report. Before you judge or change anything, run git status and git diff again and read again every file you cite. Do not repeat an earlier finding, verdict or answer without checking it against the tree as it is now.\n\n")
		}
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
	if in.Role == RoleGate || in.Role == RoleReviewer || (in.Role == RoleSpecCheck && !in.Carve) {
		b.WriteString(checkCommandSections(in))
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

// specAnswersSection tells the implementer to apply the owner's spec-check
// answers to the slice's own text as its first step, and the reviewer to
// confirm that it did (S368). It is nil for the other roles and for the finish
// step, which edits no spec. The worktree's spec lacks the owner's answers, so
// a Check line or test name that an answer changed stays stale until then.
func specAnswersSection(role, slice string, finish bool) *BriefSection {
	if finish {
		return nil
	}
	switch role {
	case RoleImplementer:
		return &BriefSection{Title: "Apply the owner's spec answers first", Body: "As your first step, before any other work, apply every Owner decision above that changes the slice's own text (a Check line, a Failing test name, a Files entry) to the slice. Use `slicer edit " + slice + " --section NAME --text \"Body\" --render --strict`. ork runs a slice's backticked Check entries as commands. A dropped backticked flag is recorded as not a command and is not run, but any other dropped command is still run by ork and can pause the run. List each change under `Spec edits:` in report part 6. If no decision changes the slice's text, change nothing."}
	case RoleReviewer:
		return &BriefSection{Title: "Confirm the owner's spec answers were applied", Body: "Check that the slice's text (`slicer show " + slice + "`) matches every Owner decision above that changes it, and that the implementer listed those changes under `Spec edits:`. A decision that changed the slice's text and is missing from it is a [fix]."}
	}
	return nil
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

// uiNoRendersTitle titles the UI round's section for a diff that changes only
// test files under internal/tui, where no view changed (S584).
const uiNoRendersTitle = "No renders expected"

const uiNoRendersCLITitle = "No renders expected for CLI tests and docs"
const uiIdenticalRendersTitle = "Renders are identical at every size"

// doneWhen adds acceptance criteria without replacing any role's report format.
func doneWhen(in BriefInput) string {
	var items []string
	add := func(text string) { items = append(items, strings.Join(strings.Fields(text), " ")) }
	isGrok := in.Vendor == "grok" || in.Vendor == "grok-local"
	if in.Resumed {
		if in.TreeUnchangedSince > 0 {
			add("First re-read every cited file before judging or changing anything.")
		} else {
			add("First re-run git status and git diff and re-read every cited file before judging or changing anything.")
		}
	}
	if in.ReadOnly || readOnlyRole(in.Role) {
		add("You may only read the tree; no git or tracking writes. Give the whole report as your final message when the report path is read-only.")
		add(deniedCommandForms)
		if in.Role == RoleUI && in.Vendor == "claude" {
			// S416: a cloud UI pass runs on an allowlist, not plan mode (adj 372),
			// so it is told the forms it may run. Same list the runner sends.
			items = append(items, "Allowed forms, verbatim: "+strings.Join(CloudUIAllowlist(in.Worktree), "; ")+"; Edit and Write under "+in.TempDir+"; any other command is denied; run one plain command per shell call.")
		}
		if isGrok {
			add("Grok plan mode: the fixtures show git status --short, git diff -- PATH and git diff --check -- PATH running, plus the built-in file tools read_file, grep and list_dir. rg, slicer and test commands remain unverified.")
			add("Use one command per call. Do not use &&, ;, a pipe, echo, a redirect or python3 -c.")
			add("A command that needs approval ends the run and loses the report unless a finished report was already produced. Do not issue one. Give the complete report as your final message.")
		}
	} else if !(in.Role == RoleImplementer && !in.Lead && in.Vendor == "claude-local") {
		items = append(items, "Allowed forms, verbatim: "+strings.Join(in.Allow, "; ")+"; any other command is denied; no pipes, no cd, no env prefix unless an entry allows it.")
		if isGrok {
			// S305, adj 373: dontAsk mode with allow rules. A compound command is
			// denied (probe p2, 2026-11-07) and a denied command may end the run.
			add("Grok dontAsk mode: change files with the write and search_replace tools only (the Edit and Write entries above), never with a shell redirect, sed or a script.")
			add("Use one command per call. Do not use &&, ;, a pipe, echo, a redirect or python3 -c.")
			add("A command that needs approval ends the run and loses the report unless a finished report was already produced. Do not issue one. Give the complete report as your final message.")
		}
	}
	if (in.Role == RoleImplementer || in.Role == RolePart) && in.Vendor == "claude" && !in.ReadOnly {
		add("Change files with the Edit and Write tools only: no `sed -i`, no scripts (python or others), no heredocs, no redirects into files.")
		add("Run one plain command per shell call: no pipes, no `&&` or `;`, no command substitution, no `cd` (use absolute paths, `go -C DIR` and `git -C DIR`).")
		add("If a command is denied, use an allowed form; do not retry it another way.")
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
		case RolePart:
			add("Change only your part's files, listed under Your part; run no slicer command that writes; run only your own tests, not the full suite.")
			add("Complete the report parts in the prompt in their stated order; self-check concurrency and handle lifetime, docs and mutation evidence for your files.")
			add("Item 4 has the literal line Tests: N passed, M failed, with integer counts of your own tests actually run; nothing may follow the counts. End with the exact unnumbered heading ## Owner needed. Both lines stand alone; skipped tests go under item 6.")
			add("Each test of your part is red before the code and green after; mutation-check every new assertion.")
		case RoleImplementer:
			add("Socket-bound tests may fail in sandbox; report as sandbox-limited. ork's round check is the evidence.")
			if in.Lead {
				add("In a first round, list every part under `## Parts`, before `## Owner needed`, one `- ID: files PATH, PATH; tests NAME, NAME` line each, or write `none`. In a fix round or the finish step, write no `## Parts` section. In a fix round you may hand a file's findings back to its part under `## Hand back` (or write `none`).")
			}
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
						if in.Vendor == "claude-local" {
							add(entry + ": red before the code, green after.")
						} else {
							add(entry + ": red before the code, green after; mutation-check every new assertion.")
						}
					} else if !strings.Contains(entry, "HUMAN CHECK") {
						add("Run or report blocked: " + quoteRunPipeSpans(entry))
					}
				}
				if heading == "Failing tests" && in.Vendor == "claude-local" && len(entries) > 0 {
					add("After the last listed test, mutation-check every new assertion once: break the product, see that assertion fail for the right reason, restore it, then run the full test suite of each affected project once.")
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
			add("Socket-bound tests may fail in sandbox; report them as sandbox-limited, not as findings. ork's round check is the evidence: its result and captured log, with the failing lines, are in the Ork round check result section; you need not run go test ./....")
			add("Each [fix] has file:line and a reproduction or a test; each [later] has proposed Importance, Urgency and Effort scores (1 to 3). Apply owner answers and keep findings within the slice.")
			if in.Role == RoleReviewer {
				add("A tracking step denied to the implementer is a Deviation for the orchestrator, never a [fix]: one untagged line under section (C), not a finding or a reason to start a fix round.")
				add("Complete sections (A)-(D), marking findings CONFIRMED or PLAUSIBLE; end with the exact unnumbered heading ## Owner needed and questions or none. In gate mode use the gate's first line and one evidence line per criterion; put denied tracking in a Deviation for the orchestrator evidence line.")
			} else {
				add("Report first line exactly VERDICT: PASS or VERDICT: FAIL, with nothing else on that line; one evidence line per criterion. PASS requires resolved earlier blockers with proving tests, no [fix], no blocker owner finding, tests green, and docs match the code; otherwise FAIL.")
				add("For a tracking step denied to the implementer, add one Deviation for the orchestrator evidence line beside the criterion lines, never a [fix]; it is not a finding and starts no fix round.")
				add("Include the exact standalone line Tests: N passed, M failed with integer counts of tests actually run and nothing after the counts, or Tests: relied on ork round results when you ran no tests and relied on ork's round check; either Tests form may start with a list marker. End with the exact unnumbered heading ## Owner needed and questions or none.")
			}
			add("Report blocked checks with the verbatim command and denial text, apart from test counts; never claim an unrun check passed.")
		case RoleArchitect:
			add("Support placement, ownership, rollback, test-shape and direction findings with file:line and CONFIRMED or PLAUSIBLE; separate change-now from later. On second check, mark every earlier finding CONFIRMED-FIXED, PARTIAL or NOT-FIXED with evidence.")
			add("End the review with blockers or no blockers, then the exact unnumbered heading ## Owner needed with questions or none; in gate mode use its first line and evidence per criterion.")
			add("Name blocked checks and their verbatim command and denial text; do not claim unrun checks.")
		case RoleUI:
			renders := "Produce the required renders or explain each blocked view; list the files produced."
			for _, section := range in.Sections {
				if section.Title == uiNoRendersTitle {
					renders = "No renders are expected: the diff changes only test files under internal/tui. Write no render files and say so in your report."
				}
				if section.Title == uiNoRendersCLITitle {
					renders = "No renders are expected: the diff changes only CLI test files and docs. Write no render files and say so in your report."
				}
			}
			add("Complete sections (A)-(D): findings name the view, size, visible defect, file:line, concrete edit and CONFIRMED or PLAUSIBLE. " + renders)
			add("End with the exact unnumbered heading ## Owner needed and questions or none; give no verdict. Each [later] has proposed Importance, Urgency and Effort scores (1 to 3).")
			add("Name checks or views you could not run or see, with the verbatim command and denial text when blocked.")
		case RoleSpecCheck:
			if in.Carve {
				add("Give a ## Slivers section with one ### N. Title block per sliver, in the order they run. Each has Files (one or two backticked paths), Failing tests and Check lists. Cut the slice so that every sliver is size S and effort 1; read the files the slice names so that each sliver names real files.")
				add("End with the exact unnumbered heading ## Owner needed and none beneath it when you carved the slice. When you cannot carve it into slivers of one or two files each, write no ## Slivers section and say why there.")
				add("State any unreadable slice or blocked read with its verbatim command and denial text; never invent a sliver.")
				break
			}
			add("Give one ## Slice ID section per slice in order, with Ready or structured owner questions. Only an unresolved owner decision is a question; close code-readable gaps yourself and respect settled decisions.")
			add("End with the exact unnumbered heading ## Owner needed and none beneath it; questions belong under their slices. Use that final section only for a whole-check blocker.")
			add("State any unreadable slice or blocked read with its verbatim command and denial text; never invent readiness.")
		case RoleSpecDraft:
			add("Give one ## Section NAME block for each of Why, Files, Failing tests, Implement and Check, in that order, holding the section's whole new text or the word unchanged. Every path under Files exists in the checkout or its line says (new).")
			add("End with the exact unnumbered heading ## Owner needed and none beneath it, or one structured question for each decision that is the owner's.")
			add("State any unreadable file or blocked read with its verbatim command and denial text; never invent a path.")
		}
	}
	// Keep even large slice sections below 40 physical lines without dropping items.
	if len(items) > 36 {
		items = append(items[:35], strings.Join(items[35:], " | "))
	}
	return "## Done when\n\n- [ ] " + strings.Join(items, "\n- [ ] ") + "\n\n"
}

const (
	directionBudget        = 8000 // bytes, the most a Direction section holds (S383)
	directionFooter        = 400  // bytes kept free for the closing lines
	directionTitle         = 120  // bytes of one title in the digest
	directionWorkers       = 8    // `slicer show` calls at the same time
	briefTitleDuplicateMin = 0.5
)

// directionSection is the Direction section of the reviewer's and the
// architect's brief (S383, *proposed*). ork reads the goals, the open items and
// the diff's paths once and gives the role the part that matters. When it
// cannot, the section tells the role to read the two sources itself, and the
// run goes on.
func (e *Engine) directionSection(ctx context.Context, rec runrec.Record) BriefSection {
	body, err := e.directionDigest(ctx, rec)
	if err != nil {
		return BriefSection{Title: "Direction", Body: "ork could not build the direction digest. Read the direction sources yourself: run `slicer goals --json --lean` and `slicer list --json --lean`, and `slicer show ID --json --lean` for an item that needs detail."}
	}
	return BriefSection{Title: "Direction", Body: body}
}

// directionEntry is one open item of the digest.
type directionEntry struct {
	item  slicercli.Item
	files []string
}

func (e *Engine) directionDigest(ctx context.Context, rec runrec.Record) (string, error) {
	goals, err := e.d.Slicer.GoalsLean(ctx, rec.Worktree)
	if err != nil {
		return "", err
	}
	items, err := e.d.Slicer.List(ctx, rec.Worktree)
	if err != nil {
		return "", err
	}
	changed, err := e.branchPaths(ctx, rec)
	if err != nil {
		return "", err
	}
	current, err := e.d.Slicer.Show(ctx, rec.Worktree, rec.Item)
	if err != nil {
		return "", err
	}
	var why string
	if current.Slice != nil {
		for _, section := range current.Slice.Sections {
			if section.Heading == "Why" {
				why = section.Body
			}
		}
	}
	var open []directionEntry
	var selfTitle string
	for _, it := range items {
		if it.ID == rec.Item {
			selfTitle = it.Title
		}
		if it.ID != rec.Item && it.Status != "done" && it.Status != "retired" {
			open = append(open, directionEntry{item: it})
		}
	}
	// An item's Files come from its own `slicer show`; the list holds none.
	unread := make([]bool, len(open))
	var wg sync.WaitGroup
	slots := make(chan struct{}, directionWorkers)
	for i := range open {
		if !open[i].item.HasSlice {
			continue
		}
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			detail, err := e.d.Slicer.Show(ctx, rec.Worktree, open[i].item.ID)
			if err != nil {
				unread[i] = true
				return
			}
			open[i].files = detail.Paths
		}()
	}
	wg.Wait()
	var duplicates, touching, rest []directionEntry
	unreadCount := 0
	for i, entry := range open {
		if unread[i] {
			unreadCount++
		}
		if whyContainsTitle(why, entry.item.Title) || similarTitles(selfTitle, entry.item.Title, briefTitleDuplicateMin) {
			duplicates = append(duplicates, entry)
		} else if touchesPaths(entry.files, changed) {
			touching = append(touching, entry)
		} else {
			rest = append(rest, entry)
		}
	}
	return formatDirection(goals, duplicates, touching, rest, unreadCount), nil
}

// branchPaths lists the paths the slice changed against the base branch: the
// tracked changes since the merge base, which include the worktree's own, and
// the untracked files. The base is the main checkout's branch, as for a landing.
func (e *Engine) branchPaths(ctx context.Context, rec runrec.Record) ([]string, error) {
	_, paths, err := e.branchChange(ctx, rec)
	return paths, err
}

// branchChangeResult shares one branch read between reviewer sections.
type branchChangeResult struct {
	MergeBase string
	Paths     []string
	Err       error
}

// branchChange is branchPaths with the merge base the paths are measured from.
func (e *Engine) branchChange(ctx context.Context, rec runrec.Record) (string, []string, error) {
	base, err := e.gitRead(ctx, e.proj.Path, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", nil, fmt.Errorf("git symbolic-ref: %w", err)
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return "", nil, errors.New("the main checkout is not on a branch")
	}
	mergeBase, err := e.gitRead(ctx, rec.Worktree, "merge-base", base, "HEAD")
	if err != nil {
		return "", nil, fmt.Errorf("git merge-base: %w", err)
	}
	mergeBase = strings.TrimSpace(mergeBase)
	changed, err := e.gitRead(ctx, rec.Worktree, "diff", "--name-only", "-z", "--no-renames", "--no-relative", mergeBase)
	if err != nil {
		return "", nil, fmt.Errorf("git diff: %w", err)
	}
	untracked, err := e.gitRead(ctx, rec.Worktree, "ls-files", "--others", "--exclude-standard", "-z", "--full-name")
	if err != nil {
		return "", nil, fmt.Errorf("git ls-files: %w", err)
	}
	var paths []string
	for _, p := range strings.Split(changed+untracked, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return mergeBase, paths, nil
}

// touchesPaths reports whether a path of files equals a changed path or is a
// directory that holds one.
func touchesPaths(files, changed []string) bool {
	for _, f := range files {
		f = strings.TrimSuffix(strings.TrimSpace(f), "/")
		if f == "" {
			continue
		}
		for _, c := range changed {
			if c == f || strings.HasPrefix(c, f+"/") {
				return true
			}
		}
	}
	return false
}

// cutTitle keeps a title on one line and under directionTitle bytes.
func cutTitle(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	if len(title) <= directionTitle {
		return title
	}
	n := directionTitle - 3
	for n > 0 && !utf8.RuneStart(title[n]) {
		n--
	}
	return title[:n] + "..."
}

// formatDirection writes the digest: the goals, likely duplicates, items that touch the diff,
// then the others by id and title, in slicer's order, until the budget is used.
// It names how many open items it left out.
func formatDirection(goals *slicercli.Goals, duplicates, touching, rest []directionEntry, unread int) string {
	var b strings.Builder
	room := func(line string) bool { return b.Len()+len(line)+1 <= directionBudget-directionFooter }
	line := func(s string) bool {
		if !room(s) {
			return false
		}
		b.WriteString(s + "\n")
		return true
	}
	leftGoals := 0
	for _, group := range []struct {
		heading string
		records []slicercli.GoalRecord
	}{{"Goals:", goals.Records.Goal}, {"Non-goals:", goals.Records.NonGoal}} {
		line(group.heading)
		for _, r := range group.records {
			if r.Status != "" && r.Status != "active" {
				continue
			}
			if !line("- " + r.ID + " " + cutTitle(r.Title)) {
				leftGoals++
			}
		}
	}
	listed := 0
	line("\nLikely duplicates (at least 60% of title content words in this slice's Why, or a title like this slice's):")
	if len(duplicates) == 0 {
		line("- none")
	}
	for _, d := range duplicates {
		if line("- " + d.item.ID + " " + cutTitle(d.item.Title)) {
			listed++
		}
	}
	line("\nOpen items that touch this diff's files (id, title, status, dependencies):")
	if len(touching) == 0 {
		line("- none")
	}
	for _, t := range touching {
		deps := "no dependencies"
		if len(t.item.DependsOn) > 0 {
			deps = "depends on " + strings.Join(t.item.DependsOn, ", ")
		}
		if line("- " + t.item.ID + " " + cutTitle(t.item.Title) + " (" + t.item.Status + "; " + deps + ")") {
			listed++
		}
	}
	line("\nOther open items (id and title):")
	if len(rest) == 0 {
		line("- none")
	}
	for _, r := range rest {
		if line("- " + r.item.ID + " " + cutTitle(r.item.Title)) {
			listed++
		}
	}
	b.WriteString("\n")
	if unread > 0 {
		noun := "items"
		if unread == 1 {
			noun = "item"
		}
		fmt.Fprintf(&b, "Files unread for %d %s: ork could not read their Files, so they are not matched against the diff.\n", unread, noun)
	}
	if leftGoals > 0 {
		fmt.Fprintf(&b, "%d goals left out; run `slicer goals --json --lean` for all.\n", leftGoals)
	}
	total := len(duplicates) + len(touching) + len(rest)
	noun := "items"
	if total-listed == 1 {
		noun = "item"
	}
	fmt.Fprintf(&b, "%d open %s left out of %d. Read one with `slicer show ID --json --lean`.", total-listed, noun, total)
	return b.String()
}

// checkCommandSections uses the implementer's resolved allowlist, not the gate's.
func checkCommandSections(in BriefInput) string {
	var specs []json.RawMessage
	if json.Unmarshal([]byte(in.SliceJSON), &specs) != nil {
		specs = []json.RawMessage{json.RawMessage(in.SliceJSON)}
	}
	var b strings.Builder
	for _, raw := range specs {
		var spec struct{ ID string }
		_ = json.Unmarshal(raw, &spec)
		// The first group that fits: the implementer's allowlist, a line ork
		// runs itself outside the sandbox, else the orchestrator (S406, adj 454).
		var runnable, byOrk, orchestrator []string
		for _, entry := range sliceCheckCommands(string(raw)) {
			// List a -run pattern with | in single quotes, and judge the allowlist
			// on that form (S526, adj 659). ork runs the stored text, so the "ork
			// runs" group is judged on it too.
			command := quoteRunPipe(entry.command)
			switch {
			case checkCommandRunnable(command, in.Worktree, in.Allow):
				runnable = append(runnable, command)
			case checkRunByOrk(entry.command, in.Worktree):
				byOrk = append(byOrk, command)
			default:
				orchestrator = append(orchestrator, command)
			}
		}
		if in.Role == RoleSpecCheck {
			fmt.Fprintf(&b, "Check command classification for %s:\n\n", spec.ID)
		}
		for _, group := range []struct {
			title    string
			commands []string
		}{{"Implementer-runnable Check commands", runnable}, {"Check commands ork runs", byOrk}, {"Orchestrator-run Check commands", orchestrator}} {
			fmt.Fprintf(&b, "## %s\n\n", group.title)
			if len(group.commands) == 0 {
				b.WriteString("none\n")
			}
			for _, command := range group.commands {
				fmt.Fprintf(&b, "- %s\n", command)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// quoteRunPipe wraps an unquoted -run pattern that holds | in single quotes, so
// a read-only role can run the command as written: the permission check scans
// an unquoted | as a second operation (S526, adj 659). The command splits into
// words on whitespace outside quotes. A word -run is followed by the pattern
// word, or the word is -run=PATTERN. The pattern is left alone when its | is
// first, last or doubled, or when it holds a quote, a backslash, $, a backtick,
// ;, &, < or >. Every other word and all whitespace stay byte for byte.
func quoteRunPipe(command string) string {
	type word struct{ start, end int }
	var words []word
	var quote byte
	start := -1
	for i := 0; i < len(command); i++ {
		c := command[i]
		space := quote == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r')
		if space {
			if start >= 0 {
				words = append(words, word{start, i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
		switch {
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case c == quote:
			quote = 0
		}
	}
	if start >= 0 {
		words = append(words, word{start, len(command)})
	}
	var b strings.Builder
	last := 0
	wrap := func(from, to int) {
		pattern := command[from:to]
		if from < last || !quotablePipePattern(pattern) {
			return
		}
		b.WriteString(command[last:from])
		b.WriteString("'" + pattern + "'")
		last = to
	}
	for i, w := range words {
		text := command[w.start:w.end]
		switch {
		case text == "-run" && i+1 < len(words):
			wrap(words[i+1].start, words[i+1].end)
		case strings.HasPrefix(text, "-run="):
			wrap(w.start+len("-run="), w.end)
		}
	}
	b.WriteString(command[last:])
	return b.String()
}

func quotablePipePattern(pattern string) bool {
	return strings.Contains(pattern, "|") && !strings.HasPrefix(pattern, "|") && !strings.HasSuffix(pattern, "|") &&
		!strings.Contains(pattern, "||") && !strings.ContainsAny(pattern, "'\"\\$`;&<>\n")
}

// quoteRunPipeSpans applies quoteRunPipe to the backtick spans of a Check entry
// and leaves the text outside the backticks as written.
func quoteRunPipeSpans(entry string) string {
	parts := strings.Split(entry, "`")
	for i := 1; i < len(parts); i += 2 {
		parts[i] = quoteRunPipe(parts[i])
	}
	return strings.Join(parts, "`")
}

var envAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

type sliceCheckCommand struct {
	command string
	empty   bool
	// outside is the entry's text outside the backticks. It is set on the first
	// command of an entry only, so an entry warns once about a second command.
	outside string
}

// SliceCheckCommandWarnings flags unrunnable commands and missing path arguments.
// The caller supplies the worktree, or the checkout before that tree exists.
// A first word that names a Files path is exempt only while that path does not
// exist there.
func SliceCheckCommandWarnings(raw, worktree string) []string {
	sections := sliceChecklistSections(raw)
	return checkCommandWarnings(checkBodyCommands(sections["Check"]), sections["Files"], worktree)
}

// CheckBodyCommandWarnings checks draft text using the slice-check parser.
func CheckBodyCommandWarnings(body, files, worktree string) []string {
	return checkCommandWarnings(checkBodyCommands(body), files, worktree)
}

// filesPathNotYet reports whether a relative path inside the worktree does not
// exist yet. Any other Lstat result, or a path outside the worktree, is false.
func filesPathNotYet(path, worktree string) bool {
	if filepath.IsAbs(path) {
		return false
	}
	full := filepath.Join(worktree, path)
	rel, err := filepath.Rel(worktree, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	_, err = os.Lstat(full)
	return errors.Is(err, fs.ErrNotExist)
}

func isPathArgument(w shellWord) bool {
	first, _, _ := strings.Cut(w.text, "/")
	if strings.Contains(first, ":") || first != "." && first != ".." && strings.Contains(first, ".") {
		return false
	}
	return !w.quoted && !w.dynamic && !strings.HasPrefix(w.text, "-") &&
		!filepath.IsAbs(w.text) && strings.Contains(w.text, "/") &&
		!strings.Contains(w.text, "://") && !strings.HasSuffix(w.text, "...")
}

func missingPathArguments(command string, paths map[string]bool, worktree string) []string {
	var missing []string
	seen := map[string]bool{}
	for _, words := range simpleCommands(command) {
		i := 0
		for i < len(words) && envAssignment.MatchString(words[i].text) {
			i++
		}
		for i++; i < len(words); i++ {
			w := words[i]
			switch w.text {
			case "-run", "-bench", "-skip":
				i++ // The next word is a test pattern, not a path.
				continue
			}
			if !isPathArgument(w) || seen[w.text] || !filesPathNotYet(w.text, worktree) {
				continue
			}
			path := filepath.Clean(strings.TrimPrefix(w.text, "./"))
			exempt := paths[path]
			for file := range paths {
				if strings.HasPrefix(file, path+string(filepath.Separator)) {
					exempt = true
					break
				}
			}
			if !exempt {
				missing = append(missing, w.text)
				seen[w.text] = true
			}
		}
	}
	return missing
}

// checkCommandWarnings skips a command word that names a Files path only while
// that path does not exist; an existing one must be on PATH or be an
// executable regular file in the worktree.
func checkCommandWarnings(entries []sliceCheckCommand, files, worktree string) []string {
	paths := map[string]bool{}
	for _, m := range backticked.FindAllStringSubmatch(files, -1) {
		paths[filepath.Clean(strings.TrimPrefix(lineAnchor.ReplaceAllString(strings.TrimSpace(m[1]), ""), "./"))] = true
	}
	var warnings []string
	for _, entry := range entries {
		for _, name := range secondCommands(entry.outside) {
			warnings = append(warnings, fmt.Sprintf("Check entry %q: text outside the backticks names a second command %q; put each command in its own backticked entry", entry.command, name))
		}
		// Leading NAME=value words are environment, as plainCheckRow reads them.
		word, end := scanWord(entry.command, 0)
		for envAssignment.MatchString(word.text) && end < len(entry.command) {
			start := end
			for start < len(entry.command) && (entry.command[start] == ' ' || entry.command[start] == '\t') {
				start++
			}
			next, nextEnd := scanWord(entry.command, start)
			if nextEnd <= start {
				break
			}
			word, end = next, nextEnd
		}
		if envAssignment.MatchString(word.text) {
			warnings = append(warnings, fmt.Sprintf("Check entry %q: first word %q is not a runnable command", entry.command, word.text))
			continue
		}
		runnable := paths[filepath.Clean(strings.TrimPrefix(word.text, "./"))] && filesPathNotYet(word.text, worktree)
		if filepath.IsAbs(word.text) || !strings.ContainsRune(word.text, filepath.Separator) {
			if _, err := exec.LookPath(word.text); err == nil {
				runnable = true
			}
		}
		// os/exec finds a bare name only on PATH, never in the worktree.
		if filepath.IsAbs(word.text) || strings.ContainsRune(word.text, filepath.Separator) {
			path := word.text
			if !filepath.IsAbs(path) {
				path = filepath.Join(worktree, path)
			}
			rel, err := filepath.Rel(worktree, path)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
					runnable = true
				}
			}
		}
		if !runnable {
			warnings = append(warnings, fmt.Sprintf("Check entry %q: first word %q is not a runnable command", entry.command, word.text))
			continue
		}
		for _, path := range missingPathArguments(entry.command, paths, worktree) {
			warnings = append(warnings, fmt.Sprintf("Check entry %q: path argument %q does not exist in the worktree", entry.command, path))
		}
	}
	return warnings
}

// sliceCheckCommands shares the backtick splitter with the role briefs.
// Human and after-land checks remain owned by their existing workflows.
// Entries after an "Acceptance criteria:" line, up to a "Commands:" line or the
// end of the section, are prose and add no command (S407, adj 448).
func sliceCheckCommands(raw string) []sliceCheckCommand {
	return checkBodyCommands(sliceChecklistSections(raw)["Check"])
}

func checkBodyCommands(body string) []sliceCheckCommand {
	var commands []sliceCheckCommand
	for _, entry := range checklistEntries(withoutAcceptanceCriteria(body)) {
		if strings.Contains(entry, "HUMAN CHECK") || strings.Contains(entry, "[AFTER LAND]") {
			continue
		}
		parts := strings.Split(entry, "`")
		var outside []string
		for i := 0; i < len(parts); i += 2 {
			outside = append(outside, parts[i])
		}
		text := strings.Join(outside, " ")
		for i := 1; i+1 < len(parts); i += 2 {
			if command := strings.TrimSpace(parts[i]); command != "" {
				// These wordings share the empty-output rule (S419, S525, adj 468).
				lower := strings.ToLower(entry)
				empty := strings.Contains(lower, "prints nothing") || strings.Contains(lower, "must print nothing") ||
					strings.Contains(lower, "finds nothing") || strings.Contains(lower, "matches nothing") || strings.Contains(lower, "returns nothing")
				commands = append(commands, sliceCheckCommand{command: command, empty: empty, outside: text})
				text = ""
			}
		}
	}
	return commands
}

// secondCommands returns "NAME ARG" for each clause-leading listed command word
// in the text outside an entry's backticks, in text order (adj 628,
// spec.check-second-command). A clause starts after a word that ends in "," or
// ";", or in "." right after a letter or digit, and after the words "and" and
// "or". The first word is not a clause start.
func secondCommands(outside string) []string {
	words := strings.Fields(outside)
	var found []string
	for i := 1; i+1 < len(words); i++ {
		if !clauseEnd(words[i-1]) {
			continue
		}
		switch words[i] {
		case "go", "gofmt", "git", "slicer", "adj", "ork":
			found = append(found, words[i]+" "+strings.TrimRight(words[i+1], ",;."))
		}
	}
	return found
}

func clauseEnd(word string) bool {
	if word == "and" || word == "or" || strings.HasSuffix(word, ",") || strings.HasSuffix(word, ";") {
		return true
	}
	if !strings.HasSuffix(word, ".") || len(word) < 2 {
		return false
	}
	c := word[len(word)-2]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// withoutAcceptanceCriteria drops the lines from an "Acceptance criteria:" line
// up to the next "Commands:" line (or the end), and the marker lines themselves.
// A body without the marker comes back unchanged.
func withoutAcceptanceCriteria(body string) string {
	var kept []string
	skip := false
	for _, line := range strings.Split(body, "\n") {
		switch strings.TrimSpace(line) {
		case "Acceptance criteria:":
			skip = true
		case "Commands:":
			skip = false
		default:
			if !skip {
				kept = append(kept, line)
			}
		}
	}
	return strings.Join(kept, "\n")
}

type sliceCheck struct {
	sliceCheckCommand
	env, argv []string
	pipe      [][]string // stages after argv, executed without a shell (S524)
	notRun    string
	chain     int // nonzero for a part of an && chain: the parts of one line share it (S406)
}

// notRunChainFailed is the record of a chain part that follows a failed one.
const notRunChainFailed = "not run by ork: an earlier part of the chain failed"

func sliceChecks(raw, worktree string) []sliceCheck {
	var rows []sliceCheck
	for i, command := range sliceCheckCommands(raw) {
		rows = append(rows, checkRows(command, worktree, i+1)...)
	}
	return rows
}

// checkRows is the rows ork runs for one Check line. A plain line is one row. A
// chain of plain commands joined by && outside quotes is one row for each part,
// all with the chain number chain. When any part is not a plain command ork
// would run, the line stays one row marked not run (S406, adj 454).
func checkRows(command sliceCheckCommand, worktree string, chain int) []sliceCheck {
	if parts := splitAndChain(command.command); len(parts) > 1 {
		rows := make([]sliceCheck, 0, len(parts))
		for _, part := range parts {
			// The line's "must print nothing" binds every part, as it does for a
			// shell run of the whole line.
			row := plainCheckRow(sliceCheckCommand{command: part, empty: command.empty}, worktree)
			if row.notRun != "" {
				rows = nil
				break
			}
			row.chain = chain
			rows = append(rows, row)
		}
		if rows != nil {
			return rows
		}
	}
	return []sliceCheck{plainCheckRow(command, worktree)}
}

// checkRunByOrk reports whether ork runs the Check line itself: a plain
// command, a read-only pipeline, or an && chain of those (S406, S524).
func checkRunByOrk(command, worktree string) bool {
	for _, row := range checkRows(sliceCheckCommand{command: command}, worktree, 1) {
		if row.notRun != "" {
			return false
		}
	}
	return true
}

// splitAndChain splits command at each && outside quotes. A command without
// one comes back as the only part. The quote rules are those of
// shellSyntaxOutsideQuotes.
func splitAndChain(command string) []string {
	var parts []string
	var quote byte
	start := 0
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\\' && quote != '\'':
			i++
		case c == quote:
			quote = 0
		case quote != 0:
		case c == '\'' || c == '"':
			quote = c
		case c == '&' && i+1 < len(command) && command[i+1] == '&':
			parts = append(parts, strings.TrimSpace(command[start:i]))
			i++
			start = i + 1
		}
	}
	return append(parts, strings.TrimSpace(command[start:]))
}

// plainCheckRow is the row for one command with no && in it.
func plainCheckRow(command sliceCheckCommand, worktree string) sliceCheck {
	parts := splitPipeline(command.command)
	row := checkStage(parts[0], worktree)
	row.sliceCheckCommand = command
	if len(parts) == 1 {
		return row
	}
	for i, part := range parts {
		stage := checkStage(part, worktree)
		if stage.notRun != "" || len(stage.env) != 0 || len(stage.argv) == 0 || (stage.argv[0] != "ls" && stage.argv[0] != "rg") {
			row.notRun = "not run by ork: orchestrator"
			return row
		}
		if stage.argv[0] == "rg" {
			for _, arg := range stage.argv[1:] {
				if arg == "--pre" || strings.HasPrefix(arg, "--pre=") || arg == "--hostname-bin" || strings.HasPrefix(arg, "--hostname-bin=") {
					row.notRun = "not run by ork: orchestrator"
					return row
				}
			}
		}
		if i > 0 {
			row.pipe = append(row.pipe, stage.argv)
		}
	}
	return row
}

// splitPipeline uses the same quote and escape rules as splitAndChain.
func splitPipeline(command string) []string {
	var parts []string
	var quote byte
	start := 0
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\\' && quote != '\'':
			i++
		case c == quote:
			quote = 0
		case quote != 0:
		case c == '\'' || c == '"':
			quote = c
		case c == '|':
			parts = append(parts, strings.TrimSpace(command[start:i]))
			start = i + 1
		}
	}
	return append(parts, strings.TrimSpace(command[start:]))
}

// checkStage parses one stage; plain commands retain their existing rules.
func checkStage(text, worktree string) sliceCheck {
	command := sliceCheckCommand{command: text}
	row := sliceCheck{sliceCheckCommand: command}
	// No shell runs these commands. Reject shell syntax rather than changing
	// its meaning. scanWord handles literal quotes and escaped spaces.
	if !plainCheckSyntax(command.command) {
		row.notRun = "not run by ork: orchestrator"
		return row
	}
	for i := 0; i < len(command.command); {
		if command.command[i] == ' ' || command.command[i] == '\t' {
			i++
			continue
		}
		word, end := scanWord(command.command, i)
		if end <= i {
			row.notRun = "not run by ork: orchestrator"
			break
		}
		i = end
		value := word.text
		if _, v, ok := strings.Cut(value, "="); ok {
			value = v
		}
		path := value
		if !filepath.IsAbs(path) {
			path = filepath.Join(worktree, path)
		}
		rel, err := filepath.Rel(worktree, filepath.Clean(path))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			row.notRun = "not run by ork: orchestrator"
		}
		if len(row.argv) == 0 && envAssignment.MatchString(word.text) {
			row.env = append(row.env, word.text)
		} else {
			row.argv = append(row.argv, word.text)
		}
	}
	if len(row.argv) == 0 {
		row.notRun = "not run by ork: orchestrator"
	} else if strings.HasPrefix(row.argv[0], "-") {
		// A backticked flag, such as a spec answer left in the Check
		// ("drop the `--yes` flags"), is not a command (S368).
		row.notRun = "not run by ork: not a command (starts with -): " + row.argv[0]
	}
	return row
}

func plainCheckSyntax(command string) bool {
	return !shellSyntaxOutsideQuotes(command, "&;<>(){}*?[]~#")
}

// shellSyntaxOutsideQuotes reports whether command holds shell syntax: a
// newline, an unquoted character of unquoted, a `$` or backtick outside single
// quotes, an unterminated quote or a trailing backslash. Single quotes are
// literal; inside double quotes only `$` and a backtick count. A backslash
// escapes the next character outside single quotes.
func shellSyntaxOutsideQuotes(command, unquoted string) bool {
	var quote byte
	for i := 0; i < len(command); i++ {
		c := command[i]
		if c == '\n' || c == '\r' {
			return true
		}
		if c == '\\' && quote != '\'' {
			i++
			if i == len(command) {
				return true
			}
			continue
		}
		if c == quote {
			quote = 0
			continue
		}
		if quote == '\'' {
			continue
		}
		if c == '$' || c == '`' {
			return true
		}
		if quote == '"' {
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if strings.ContainsRune(unquoted, rune(c)) {
			return true
		}
	}
	return quote != 0
}

func checkCommandRunnable(command, worktree string, allow []string) bool {
	if regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`).MatchString(command) {
		return false
	}
	for _, word := range strings.Fields(command) {
		word = strings.Trim(word, "\"'")
		if _, value, ok := strings.Cut(word, "="); ok {
			word = strings.Trim(value, "\"'")
		}
		if filepath.IsAbs(word) {
			rel, err := filepath.Rel(worktree, filepath.Clean(word))
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return false
			}
		}
	}
	// A quoted ;|&<> is an argument, not shell syntax (S409).
	compound := shellSyntaxOutsideQuotes(command, ";|&<>")
	for _, form := range allow {
		pattern, ok := strings.CutPrefix(form, "Bash(")
		if !ok || !strings.HasSuffix(pattern, ")") {
			continue
		}
		pattern = strings.TrimSuffix(pattern, ")")
		// A compound command needs an explicit whole-command allowlist form.
		if compound && !strings.HasPrefix(command, strings.TrimSuffix(pattern, "*")) {
			continue
		}
		if compound && !strings.ContainsAny(pattern, ";|&<>\n`$") {
			continue
		}
		expr := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
		if regexp.MustCompile(expr).MatchString(command) {
			return true
		}
	}
	return false
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
