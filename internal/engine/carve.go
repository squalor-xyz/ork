// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// The carve (S359, *proposed*; adj 337, 338): when a slice above size S is
// run on a local implementer, the spec check row reads the slice once, read
// only, and proposes slivers. ork files each one under the slice with
// `slicer add --parent` and the run is the chain of those slivers (S62). The
// role is the spec check's (its row, its read-only permission, its fingerprint)
// with the carve prompt (roles.CarvePrompt) as its task; it writes no file but
// its report.

// CarvedSliver is one sliver the carve role proposes: what slicer needs to file
// it and what its implementer needs to start.
type CarvedSliver struct {
	Title  string
	Files  []string
	Tests  []string
	Checks []string
}

// CarveConfig describes one carve: the slice, the spec check row's chain and a
// directory that holds the role's temp directory and its report.
type CarveConfig struct {
	Project registry.Project
	Item    string
	Lineup  []runrec.LineupEntry // the spec check row, then its fallbacks
	Dir     string               // the caller owns it
}

var errCarveStatusChanged = errors.New("the main checkout changed during the carve")

// RunCarve runs the carve pass over cfg.Item and returns the slivers the role
// proposes. It runs read-only: a change in the project checkout, or a moving
// HEAD, refuses the carve. It writes nothing to slicer, and a carve that fails
// leaves nothing behind, so running it again is safe.
func RunCarve(ctx context.Context, d Deps, cfg CarveConfig) ([]CarvedSliver, error) {
	if len(cfg.Lineup) == 0 {
		return nil, errors.New("the lineup has no spec-check row")
	}
	if d.Runner == nil {
		return nil, errors.New("carve needs a runner")
	}
	perm, err := ResolvePermission(cfg.Project, RoleSpecCheck)
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
	before, err := gitStatusAt(ctx, d, root)
	if err != nil {
		return nil, fmt.Errorf("read-only fingerprint: %w", err)
	}
	raw, err := d.Slicer.ShowRaw(ctx, root, cfg.Item)
	if err != nil {
		return nil, fmt.Errorf("slicer show %s: %w", cfg.Item, err)
	}
	if _, err := SpecFingerprint(raw); err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.Item, err)
	}
	tempDir := filepath.Join(cfg.Dir, "tmp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return nil, fmt.Errorf("temp directory: %w", err)
	}
	reportPath := filepath.Join(cfg.Dir, "carve.md")
	exhausted := map[string]bool{}
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
			Role:  RoleSpecCheck, Vendor: entry.Vendor, ReadOnly: true, SliceID: cfg.Item, Worktree: root, TempDir: tempDir, ReportPath: reportPath,
			SliceJSON: "[" + strings.TrimSpace(string(raw)) + "]", EnvLines: cfg.Project.EnvironmentLines,
			Carve: true,
		})
		if err != nil {
			return nil, err
		}
		res, runErr := d.runSpecRole(ctx, Request{
			SpecProject: cfg.Project.Name, SpecItems: []string{cfg.Item}, ProjectRoot: root,
			Role: RoleSpecCheck, Round: 1, Vendor: entry.Vendor, Model: entry.Model, Effort: entry.Effort,
			Brief: brief, Worktree: root, TempDir: tempDir, ReportPath: reportPath,
			Allowlist: perm.Allow, ReadOnly: perm.ReadOnly,
		})
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		after, err := gitStatusAt(ctx, d, root)
		if err != nil {
			return nil, fmt.Errorf("read-only fingerprint: %w", err)
		}
		afterHead, err := specHeadAt(ctx, d, root)
		if err != nil {
			return nil, err
		}
		switch {
		case len(res.Denials) > 0:
			return nil, &specDenialError{text: denialTexts(res.Denials)}
		case afterHead != head:
			return nil, errSpecMainMoved
		case after != before:
			return nil, treeChangedError(errCarveStatusChanged, before, after)
		}
		class := res.Exit
		if runErr != nil || class == "" {
			class = ExitError
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
		msg := fmt.Sprintf("carve (%s %s %s) exited %s", entry.Vendor, entry.Model, entry.Effort, class)
		if runErr != nil {
			msg += ": " + runErr.Error()
		}
		return nil, errors.New(msg)
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("carve: the role gave no report")
	}
	if err := os.WriteFile(reportPath, []byte(text), 0o600); err != nil {
		return nil, fmt.Errorf("save carve report: %w", err)
	}
	return ParseCarve(text)
}

// carveLabel is the line that opens one of a sliver's lists.
var carveLabel = regexp.MustCompile(`(?i)^(files|failing tests|check):$`)

// carveTitleNumber is the "1." or "2)" a role may put before a sliver's title.
var carveTitleNumber = regexp.MustCompile(`^\d+[.)]\s*`)

// carvePathSpan is the first backticked path of a Files bullet and the `:N` or
// `:N-M` anchor that may follow its closing backtick.
var carvePathSpan = regexp.MustCompile("`([^`]+)`(:\\d+(?:-\\d+)?)?")

// carvePath is the path of a Files bullet: the first backticked span with its
// anchor, or the bullet itself when it has none.
func carvePath(item string) string {
	if m := carvePathSpan.FindStringSubmatch(item); m != nil {
		return strings.TrimSpace(m[1]) + m[2]
	}
	return strings.TrimSpace(strings.Trim(item, "`"))
}

// ParseCarve reads the carve role's report: a `## Slivers` section of
// `### N. Title` blocks, each with Files, Failing tests and Check lists, and
// the shared `## Owner needed` section. A sliver has a title, one or two files,
// a failing test and a check. An Owner needed section that is not "none" means
// the role did not carve, and its text is the error.
func ParseCarve(text string) ([]CarvedSliver, error) {
	text, _ = splitToolFeedback("", text) // advisory, no part of the carve
	owner, ok := section(text, ownerHeading)
	if !ok {
		return nil, errors.New("the carve report has no `## Owner needed` section")
	}
	if o := SpecOwnerText(owner); o != "" {
		return nil, fmt.Errorf("the carve role did not carve; it needs an answer first: %s", o)
	}
	body, ok := section(text, "## Slivers")
	if !ok {
		return nil, errors.New("the carve report has no `## Slivers` section")
	}
	var out []CarvedSliver
	var list *[]string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "### "):
			title := strings.TrimSpace(carveTitleNumber.ReplaceAllString(strings.TrimSpace(strings.TrimPrefix(line, "### ")), ""))
			out = append(out, CarvedSliver{Title: strings.Join(strings.Fields(title), " ")})
			list = nil
		case len(out) == 0:
			continue
		case carveLabel.MatchString(trimmed):
			cur := &out[len(out)-1]
			switch strings.ToLower(trimmed) {
			case "files:":
				list = &cur.Files
			case "failing tests:":
				list = &cur.Tests
			default:
				list = &cur.Checks
			}
		case list != nil && listMarker.MatchString(trimmed):
			item := strings.Join(strings.Fields(listMarker.ReplaceAllString(trimmed, "")), " ")
			if list == &out[len(out)-1].Files {
				item = carvePath(item)
			}
			if item != "" {
				*list = append(*list, item)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the carve report has no sliver: a `## Slivers` section needs `### N. Title` blocks")
	}
	for i, sl := range out {
		switch {
		case sl.Title == "":
			return nil, fmt.Errorf("sliver %d has no title", i+1)
		case len(sl.Files) == 0:
			return nil, fmt.Errorf("sliver %d (%s) has no Files: it needs one or two files", i+1, sl.Title)
		case len(sl.Files) > 2:
			return nil, fmt.Errorf("sliver %d (%s) has %d files: a sliver has one or two files", i+1, sl.Title, len(sl.Files))
		case len(sl.Tests) == 0:
			return nil, fmt.Errorf("sliver %d (%s) has no Failing tests", i+1, sl.Title)
		case len(sl.Checks) == 0:
			return nil, fmt.Errorf("sliver %d (%s) has no Check", i+1, sl.Title)
		}
	}
	return out, nil
}

// FileSliver files one carved sliver under its parent at size S and effort 1
// (rule u6) and gives it the Files, Failing tests and Check sections: `slicer
// add TITLE --parent PARENT --size S --effort 1`, then `slicer promote`. The
// add key is the sliver's filing identity, so filing it again returns the item
// the first call filed. A failure after the add names the item.
func FileSliver(ctx context.Context, d Deps, proj registry.Project, parent string, index int, sl CarvedSliver, tempDir string) (string, error) {
	title := strings.Join(strings.Fields(sl.Title), " ")
	identity, _ := json.Marshal([]any{proj.Path, parent, index, title, sl.Files})
	sum := sha256.Sum256(identity)
	w := runrec.Writer{Now: d.now, Sleep: d.Sleep, Backoff: d.Backoff}
	added, err := d.Slicer.AddItem(ctx, w, proj.Path, slicercli.AddSpec{Title: title, Parent: parent, Key: fmt.Sprintf("ork-sliver-%x", sum[:8]), Size: "S", Effort: 1})
	if err != nil {
		return "", fmt.Errorf("slicer add: %w", err)
	}
	if added.Existing && added.HasSlice {
		return added.ID, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n### Why\n\nA sliver of %s, carved by ork's spec check row so that a local implementer can do it alone (rule u6: one or two files, effort 1). It runs with its siblings in %s's worktree: `ork run %s --slivers`.\n\n", title, parent, parent, parent)
	b.WriteString("### Files\n\n")
	for _, f := range sl.Files {
		fmt.Fprintf(&b, "- `%s`\n", f)
	}
	b.WriteString("\n### Failing tests\n\n")
	for _, t := range sl.Tests {
		fmt.Fprintf(&b, "- %s\n", t)
	}
	b.WriteString("\n### Check\n\n")
	for _, c := range sl.Checks {
		fmt.Fprintf(&b, "- %s\n", c)
	}
	f, err := os.CreateTemp(tempDir, "sliver-*.md")
	if err != nil {
		return "", fmt.Errorf("filed %s, but its sections failed (filing again retries them): %w", added.ID, err)
	}
	defer os.Remove(f.Name())
	_, werr := f.WriteString(b.String())
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		return "", fmt.Errorf("filed %s, but its sections failed (filing again retries them): %w", added.ID, err)
	}
	if err := d.Slicer.Promote(ctx, w, proj.Path, added.ID, f.Name()); err != nil {
		return "", fmt.Errorf("filed %s, but its sections failed (filing again retries them): slicer promote: %w", added.ID, err)
	}
	return added.ID, nil
}
