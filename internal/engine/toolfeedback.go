// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// Tool feedback filing (S387, *proposed*; adj 410-414). A role's tool feedback
// entry of a safe kind is filed as an unscored slice, once per report, without
// asking the owner. The functions here hold no state: the engine calls them
// and keeps the outcome on the entry.

// ToolFeedbackSlug turns a tool name into the slug of its key and title: lower
// case, every rune outside [a-z0-9-] becomes "-", trimmed of "-". A name with
// nothing left is "tool".
func ToolFeedbackSlug(tool string) string {
	slug := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(tool))
	if slug = strings.Trim(slug, "-"); slug == "" {
		return "tool"
	}
	return slug
}

// ToolFeedbackKey is the stable filing identity of an entry, which a repeat in
// the same run or in a later run shares (adj 414): `tool-feedback-SLUG-HASH`,
// HASH the first 8 hex digits of the sha256 of the slug, the kind and the
// proposal (lower case, white space folded to one space), joined by NUL.
func ToolFeedbackKey(tool, kind, proposal string) string {
	slug := ToolFeedbackSlug(tool)
	norm := strings.ToLower(strings.Join(strings.Fields(proposal), " "))
	sum := sha256.Sum256([]byte(slug + "\x00" + kind + "\x00" + norm))
	return "tool-feedback-" + slug + "-" + hex.EncodeToString(sum[:])[:8]
}

// ToolFeedbackSafe says whether ork files an entry of this kind on its own
// (adj 411, by kind). A feature request stays kept.
func ToolFeedbackSafe(kind string) bool {
	return kind == "friction" || kind == "deterministic-check"
}

// FeedbackTarget is where an entry is filed: the project, the slice title, and
// the item to pass as --discovered-from (empty in another project, which has no
// such item).
type FeedbackTarget struct {
	Project        registry.Project
	Title          string
	DiscoveredFrom string
}

// ToolFeedbackTarget picks the project an entry is filed in (adj 412): the
// registered project that the tool's slug names, else the run's own project. In
// the run's own project the title names the tool unless the tool is that
// project, and the new slice is discovered from the run's item. lookup returns
// a registered project by slug; nil resolves nothing.
func ToolFeedbackTarget(own registry.Project, item string, lookup func(name string) (registry.Project, bool), f ToolFeedback) FeedbackTarget {
	slug := ToolFeedbackSlug(f.Tool)
	ownTool := slug == ToolFeedbackSlug(own.Name)
	if !ownTool && lookup != nil {
		if p, ok := lookup(slug); ok && p.Name != own.Name && p.Path != own.Path {
			return FeedbackTarget{Project: p, Title: f.Proposal}
		}
	}
	t := FeedbackTarget{Project: own, Title: f.Proposal, DiscoveredFrom: item}
	if !ownTool {
		t.Title = slug + ": " + f.Proposal
	}
	return t
}

// ToolFeedbackResult is what filing one entry came to: its key, the filed
// item's id (empty when nothing was filed) and the failure, if any. An entry
// of a kind that is only kept has a key and neither of the others.
type ToolFeedbackResult struct {
	Key   string
	Filed string
	Err   error
}

// toolFeedbackWhy is the Why section of a filed entry. It names the role, the
// run, the project, the item and the evidence, so that the slice reads on its
// own in a project that has no link to the run.
func toolFeedbackWhy(t FeedbackTarget, f ToolFeedback, own registry.Project, item, runID string) string {
	return fmt.Sprintf("## %s\n\n### Why\n\nThe %s reported this as tool feedback (%s) on %s in ork run %s, project %s, item %s:\n\n%s\n\nEvidence: %s\n",
		t.Title, f.Role, f.Kind, f.Tool, runID, own.Name, item, f.Proposal, f.Evidence)
}

// FileToolFeedback files each entry of a safe kind as an unscored slice and
// returns one result per entry, in order. It runs `slicer add TITLE --key KEY`
// in the target project, then `slicer promote` with a Why outline written in
// dir; an item that the key already names and that has its spec is not
// promoted again. A failure is the entry's result, never the call's: the
// entries that follow are still filed. d.Project resolves the registered
// projects.
func FileToolFeedback(ctx context.Context, d Deps, w runrec.Writer, dir string, own registry.Project, item, runID string, entries []ToolFeedback) []ToolFeedbackResult {
	out := make([]ToolFeedbackResult, len(entries))
	for i, f := range entries {
		key := f.Key
		if key == "" {
			key = ToolFeedbackKey(f.Tool, f.Kind, f.Proposal)
		}
		out[i].Key = key
		if !ToolFeedbackSafe(f.Kind) {
			continue
		}
		t := ToolFeedbackTarget(own, item, d.Project, f)
		added, err := d.Slicer.Add(ctx, w, t.Project.Path, t.Title, t.DiscoveredFrom, key)
		if err != nil {
			out[i].Err = fmt.Errorf("slicer add in %s: %w", t.Project.Name, err)
			continue
		}
		if !(added.Existing && added.HasSlice) {
			path := filepath.Join(dir, key+".md")
			if err := os.WriteFile(path, []byte(toolFeedbackWhy(t, f, own, item, runID)), 0o600); err != nil {
				out[i].Err = fmt.Errorf("outline: %w", err)
				continue
			}
			if err := d.Slicer.Promote(ctx, w, t.Project.Path, added.ID, path); err != nil {
				out[i].Err = fmt.Errorf("filed %s in %s, but its Why section failed: slicer promote: %w", added.ID, t.Project.Name, err)
				continue
			}
		}
		out[i].Filed = added.ID
	}
	return out
}

// fileToolFeedback files the entries that a report newly added to the run and
// keeps each outcome on its entry. Tool feedback is advisory: a failure is
// written on the entry as file_error and the run goes on, so nothing is
// returned. It holds fileMu, as filing a later finding does.
func (e *Engine) fileToolFeedback(added []ToolFeedback) {
	var safe []ToolFeedback
	for _, f := range added {
		if ToolFeedbackSafe(f.Kind) {
			safe = append(safe, f)
		}
	}
	if len(safe) == 0 {
		return
	}
	e.fileMu.Lock()
	defer e.fileMu.Unlock()
	rec := e.run.Snapshot()
	results := FileToolFeedback(context.Background(), e.d, e.wr, e.run.Dir(), e.proj, rec.Item, rec.ID, safe)
	now := e.d.now()
	e.run.Update(func(rr *runrec.Record) {
		for _, res := range results {
			for i := range rr.ToolFeedback {
				if rr.ToolFeedback[i].Key != res.Key {
					continue
				}
				switch {
				case res.Err != nil:
					rr.ToolFeedback[i].FileError = res.Err.Error()
				case res.Filed != "":
					rr.ToolFeedback[i].Filed, rr.ToolFeedback[i].FiledAt, rr.ToolFeedback[i].FileError = res.Filed, now, ""
				}
			}
		}
	})
}
