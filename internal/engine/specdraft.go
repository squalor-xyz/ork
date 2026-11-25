// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// The spec draft (S179, scope.spec-drafting, roles.spec-draft-empty). A
// read-only role proposes the text of one slice's spec sections in the project
// checkout, under the read-only fingerprint of the spec check. ork writes
// nothing: the daemon applies a section only after the owner approves it.

// DraftSections are the spec sections a draft may propose, in slicer's order.
// Not in this slice is a boundary, not a section, and the draft leaves it.
var DraftSections = []string{"Why", "Files", "Failing tests", "Implement", "Check"}

// unchangedWord is the body of a block for a section the role leaves as it is.
const unchangedWord = "unchanged"

// SpecDraftSection is the draft's block for one section: the whole new text
// that replaces the section, or Unchanged when the role leaves it.
type SpecDraftSection struct {
	Name      string `json:"name"`
	Text      string `json:"text,omitempty"`
	Unchanged bool   `json:"unchanged,omitempty"`
}

// SpecDraft is a parsed draft report: one block for each section, in the order
// of DraftSections, and the Owner needed text the role wrote (empty for none).
type SpecDraft struct {
	Sections    []SpecDraftSection
	OwnerNeeded string
}

// SpecDraftProblem is a problem with a proposed section, which the owner sees
// beside it. The daemon refuses to apply a section that has one.
type SpecDraftProblem struct {
	Section string `json:"section"`
	Message string `json:"message"`
}

var sectionBlockHeading = regexp.MustCompile(`^## Section (.+?)\s*$`)

// ParseSpecDraft reads a draft report: one `## Section NAME` block for each of
// DraftSections, holding the section's new text or the word unchanged, and the
// `## Owner needed` section. Every way the report breaks the shape is listed
// in the *ShapeError: an unknown section name, a missing or a doubled block, an
// empty block, and no Owner needed ending.
func ParseSpecDraft(text string) (*SpecDraft, error) {
	text, _ = splitToolFeedback("", text) // advisory, no part of a draft
	var problems []string
	type block struct {
		name  string
		lines []string
	}
	var blocks []*block
	var cur *block
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if m := sectionBlockHeading.FindStringSubmatch(line); m != nil {
			cur = nil
			i := slices.IndexFunc(DraftSections, func(s string) bool { return strings.EqualFold(s, m[1]) })
			switch {
			case i < 0:
				problems = append(problems, fmt.Sprintf("a block for %s, which is not one of %s", m[1], strings.Join(DraftSections, ", ")))
			case slices.ContainsFunc(blocks, func(b *block) bool { return b.name == DraftSections[i] }):
				problems = append(problems, fmt.Sprintf("two blocks for %s", DraftSections[i]))
			default:
				cur = &block{name: DraftSections[i]}
				blocks = append(blocks, cur)
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			cur = nil
			continue
		}
		if cur != nil {
			cur.lines = append(cur.lines, line)
		}
	}
	owner, hasOwner := section(text, ownerHeading)
	if !hasOwner {
		problems = append(problems, fmt.Sprintf("no %q section", ownerHeading))
	}
	out := &SpecDraft{OwnerNeeded: SpecOwnerText(owner)}
	for _, name := range DraftSections {
		i := slices.IndexFunc(blocks, func(b *block) bool { return b.name == name })
		if i < 0 {
			problems = append(problems, "no block for "+name)
			continue
		}
		body := strings.TrimSpace(strings.Join(blocks[i].lines, "\n"))
		switch {
		case body == "":
			problems = append(problems, name+": empty; write the new text, or "+unchangedWord)
		case strings.EqualFold(strings.TrimRight(body, "."), unchangedWord):
			out.Sections = append(out.Sections, SpecDraftSection{Name: name, Unchanged: true})
		default:
			out.Sections = append(out.Sections, SpecDraftSection{Name: name, Text: body})
		}
	}
	if len(problems) > 0 {
		return nil, &ShapeError{Problems: problems}
	}
	return out, nil
}

// filesLine is a Files list item that starts with a backticked path; the rest
// of the line may hold a `(new)` mark or a note.
var filesLine = regexp.MustCompile("^\\s*[-*+]\\s+`([^`]+)`(.*)$")

// CheckDraftFiles reports each path a proposed Files section names that is not
// in the checkout at root. A line marked `(new)` names a file the slice
// creates, so it is not checked, and a path that leaves the checkout is
// refused. An unchanged section is the slice's own text and is not checked.
func CheckDraftFiles(root string, secs []SpecDraftSection) []SpecDraftProblem {
	var out []SpecDraftProblem
	for _, s := range secs {
		if s.Name != "Files" || s.Unchanged {
			continue
		}
		for _, line := range strings.Split(s.Text, "\n") {
			m := filesLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			path := lineAnchor.ReplaceAllString(m[1], "")
			if strings.Contains(m[2], "(new)") {
				continue
			}
			clean := filepath.Clean(path)
			if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				out = append(out, SpecDraftProblem{Section: s.Name, Message: fmt.Sprintf("`%s` is outside the checkout", path)})
				continue
			}
			if _, err := os.Stat(filepath.Join(root, clean)); err != nil {
				out = append(out, SpecDraftProblem{Section: s.Name, Message: fmt.Sprintf("`%s` is not in the checkout; mark a new file `(new)`", path)})
			}
		}
	}
	return out
}

// CheckDraftCheck reports non-command entries in a changed Check section.
// Changed Files replace the current slice's exemptions, including new paths.
func CheckDraftCheck(root string, secs []SpecDraftSection, raw []byte) []SpecDraftProblem {
	files := sliceChecklistSections(string(raw))["Files"]
	for _, s := range secs {
		if s.Name == "Files" && !s.Unchanged {
			files = s.Text
		}
	}
	var out []SpecDraftProblem
	for _, s := range secs {
		if s.Name == "Check" && !s.Unchanged {
			for _, warning := range CheckBodyCommandWarnings(s.Text, files, root) {
				out = append(out, SpecDraftProblem{Section: "Check", Message: warning})
			}
		}
	}
	return out
}

// SpecDraftConfig describes one spec draft: one slice, drafted in one
// read-only pass of the spec-check row's chain (owner decision 2026-11-16,
// roles.spec-draft-row).
type SpecDraftConfig struct {
	Project registry.Project
	Item    string
	Lineup  []runrec.LineupEntry // the spec check row, then its fallbacks
	Dir     string               // holds the role's temp directory and its report; the caller owns it
}

// SpecDraftResult is the draft: the sections the role proposed, the problems
// ork found in them, the Owner needed text, and the fingerprint of the spec
// that the draft was made against.
type SpecDraftResult struct {
	Draft       *SpecDraft
	Problems    []SpecDraftProblem
	Fingerprint string
	// Raw is `slicer show ID --json --lean` as the draft read it, so that the
	// caller can show each section's current text.
	Raw []byte
}

var errDraftTreeChanged = errors.New("spec-draft changed the tree, and it is a read-only role")

// RunSpecDraft drafts the item's spec read-only. It runs the role once per
// lineup entry until one finishes, as the spec check does. A change in the
// checkout or in HEAD while the role ran fails the draft and keeps nothing. A
// malformed report or a denied command resumes the role's own session once
// with the error; a second one fails the draft.
func RunSpecDraft(ctx context.Context, d Deps, cfg SpecDraftConfig) (*SpecDraftResult, error) {
	if cfg.Item == "" {
		return nil, errors.New("spec draft needs an item")
	}
	if len(cfg.Lineup) == 0 {
		return nil, errors.New("the lineup has no spec-check row")
	}
	if d.Runner == nil {
		return nil, errors.New("spec draft needs a runner")
	}
	perm, err := ResolvePermission(cfg.Project, RoleSpecDraft)
	if err != nil {
		return nil, err
	}
	implementerPerm, err := ResolvePermission(cfg.Project, RoleImplementer)
	if err != nil {
		return nil, err
	}
	root := cfg.Project.Path
	head, err := specHeadAt(ctx, d, root)
	if err != nil {
		return nil, err
	}
	baseline, err := gitStatusAt(ctx, d, root)
	if err != nil {
		return nil, fmt.Errorf("read-only fingerprint: %w", err)
	}
	raw, err := d.Slicer.ShowRaw(ctx, root, cfg.Item)
	if err != nil {
		return nil, fmt.Errorf("slicer show %s: %w", cfg.Item, err)
	}
	fp, err := SpecFingerprint(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.Item, err)
	}
	tempDir := filepath.Join(cfg.Dir, "tmp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return nil, fmt.Errorf("temp directory: %w", err)
	}
	reportPath := filepath.Join(cfg.Dir, "spec-draft.md")
	before, err := gitStatusAt(ctx, d, root)
	if err != nil {
		return nil, fmt.Errorf("read-only fingerprint: %w", err)
	}
	if before != baseline {
		return nil, errSpecStatusChanged
	}
	exhausted := map[string]bool{}
	resumed := false
	var text string
entries:
	for i, entry := range cfg.Lineup {
		if exhausted[entry.Vendor] {
			continue
		}
		if err := os.Remove(reportPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("old report: %w", err)
		}
		brief, err := BuildBrief(BriefInput{
			Allow: implementerPerm.Allow,
			Role:  RoleSpecDraft, Vendor: entry.Vendor, ReadOnly: true, SliceID: cfg.Item, Worktree: root, TempDir: tempDir, ReportPath: reportPath,
			SliceJSON: strings.TrimSpace(string(raw)), EnvLines: cfg.Project.EnvironmentLines,
			Sections: []BriefSection{{Title: "Spec draft", Body: "Draft the spec sections of " + cfg.Item + ". Report one `## Section NAME` block for each of " + strings.Join(DraftSections, ", ") + ", in this order. The slice may have no sections yet: draft them from its title. The project checkout is your working directory; you may not change it."}},
		})
		if err != nil {
			return nil, err
		}
		req := Request{
			SpecProject: cfg.Project.Name, SpecItems: []string{cfg.Item}, ProjectRoot: root,
			Role: RoleSpecDraft, Round: 1, Vendor: entry.Vendor, Model: entry.Model, Effort: entry.Effort,
			Brief: brief, Worktree: root, TempDir: tempDir, ReportPath: reportPath,
			Allowlist: perm.Allow, ReadOnly: perm.ReadOnly,
		}
		for {
			res, runErr := d.runSpecRole(ctx, req)
			class := res.Exit
			if runErr != nil || class == "" {
				class = ExitError
			}
			var problem error
			var nudge string
			switch {
			case len(res.Denials) > 0:
				problem = &specDenialError{text: denialTexts(res.Denials)}
				nudge = specDenialNudge(res.Denials, []string{cfg.Item})
			case class == ExitOK:
				if _, perr := ParseSpecDraft(readSpecReport(reportPath, res.Report)); perr != nil {
					problem = perr
					nudge = "ork checked your report and refused it: " + perr.Error() + "\n\nWrite the whole report again as your final message, as plain text with no code fence: one `## Section NAME` block for each of " + strings.Join(DraftSections, ", ") + " (the new text, or `unchanged`), then `## Owner needed`. You may only read the tree. Do not start the draft over; fix the report from what you already read.\n"
				}
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			after, statusErr := gitStatusAt(ctx, d, root)
			if statusErr != nil {
				return nil, fmt.Errorf("read-only fingerprint: %w", statusErr)
			}
			afterHead, err := specHeadAt(ctx, d, root)
			if err != nil {
				return nil, err
			}
			resumable := class == ExitOK || class == ExitError
			if problem != nil && resumable && !resumed && res.Session != "" && afterHead == head && after == before {
				resumed = true
				if err := os.Remove(reportPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Errorf("old report: %w", err)
				}
				req.Resume, req.Brief = res.Session, nudge
				continue
			}
			if afterHead != head {
				return nil, errSpecMainMoved
			}
			if after != before {
				return nil, treeChangedError(errDraftTreeChanged, before, after)
			}
			if problem != nil {
				return nil, problem
			}
			if class == ExitOK {
				text = readSpecReport(reportPath, res.Report)
				break entries
			}
			if class == ExitOutOfCredits {
				exhausted[entry.Vendor] = true
			}
			next := i + 1
			for next < len(cfg.Lineup) && exhausted[cfg.Lineup[next].Vendor] {
				next++
			}
			if fallbackExit(class) && next < len(cfg.Lineup) {
				continue entries
			}
			msg := fmt.Sprintf("spec-draft (%s %s %s) exited %s", entry.Vendor, entry.Model, entry.Effort, class)
			if runErr != nil {
				msg += ": " + runErr.Error()
			}
			return nil, errors.New(msg)
		}
	}
	draft, err := ParseSpecDraft(text)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(reportPath, []byte(text), 0o600); err != nil {
		return nil, fmt.Errorf("save spec draft: %w", err)
	}
	problems := append(CheckDraftFiles(root, draft.Sections), CheckDraftCheck(root, draft.Sections, raw)...)
	return &SpecDraftResult{Draft: draft, Problems: problems, Fingerprint: fp, Raw: raw}, nil
}
