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

	"github.com/squalor-xyz/ork/internal/adjcli"
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
// (adj 411, by kind). A feature request is asked in adj instead (S388).
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
// item's id (empty when nothing was filed), the adj record id of the question
// asked about it (0 when none was) and the failure, if any.
type ToolFeedbackResult struct {
	Key   string
	Filed string
	Asked int
	Near  []string
	Err   error
}

// ToolFeedbackAskKind is the kind that ork asks about in the target's adj
// instead of filing (S388, *proposed*; adj 415).
const ToolFeedbackAskKind = "feature"

// toolFeedbackAskPrefix starts the adj key of a tool feedback question; the
// filing key's own prefix is the same with "-" for ".".
const (
	toolFeedbackAskPrefix  = "tool-feedback."
	toolFeedbackFilePrefix = "tool-feedback-"
)

// ToolFeedbackAskKey is the adj key of the question about an entry: its filing
// key with the first "-" after "tool-feedback" as ".", which fits an adj
// decision key (S388, *proposed*).
func ToolFeedbackAskKey(filingKey string) string {
	return toolFeedbackAskPrefix + strings.TrimPrefix(filingKey, toolFeedbackFilePrefix)
}

// toolFeedbackAskTitleMax is the most runes of a question's title.
const toolFeedbackAskTitleMax = 120

// toolFeedbackAskSpec is the question about a feature entry: file or drop,
// with file recommended (adj 415). Its context starts with the proposal line,
// which filing on a decision reads back, and names the role, run, project,
// item and evidence, so the question reads on its own.
func toolFeedbackAskSpec(t FeedbackTarget, f ToolFeedback, own registry.Project, item string, src ToolFeedbackSource, key string) adjcli.AskSpec {
	title := []rune(fmt.Sprintf("Tool feedback (%s, %s): %s", f.Kind, ToolFeedbackSlug(f.Tool), f.Proposal))
	if len(title) > toolFeedbackAskTitleMax {
		title = title[:toolFeedbackAskTitleMax]
	}
	spec := adjcli.AskSpec{
		Title: string(title),
		Key:   ToolFeedbackAskKey(key),
		Context: fmt.Sprintf("%s%s\nRole: %s\n%s\nProject: %s\nItem: %s\nEvidence: %s",
			toolFeedbackProposalPrefix, f.Proposal, f.Role, src.contextLine(), own.Name, item, f.Evidence),
		Options: []adjcli.AskOption{
			{Label: "file", Summary: "ork files it as a slice in " + t.Project.Name},
			{Label: "drop", Summary: "not acted on"},
		},
		Recommend: "file",
		Reasoning: "A role proposed it; code cannot judge its cost.",
	}
	if t.DiscoveredFrom != "" {
		spec.Refs = []string{t.DiscoveredFrom}
	}
	return spec
}

// ToolFeedbackSource is where entries came from: a run (RunID) or a spec check
// of SpecItems, which has no run (S389, *proposed*; adj 417).
type ToolFeedbackSource struct {
	RunID     string
	SpecItems []string
}

// where names the source in the text of a slice or a question.
func (s ToolFeedbackSource) where() string {
	if s.RunID == "" && len(s.SpecItems) > 0 {
		return "spec check of " + strings.Join(s.SpecItems, ", ")
	}
	return "ork run " + s.RunID
}

// contextLine is the line of a question's context that names the source.
func (s ToolFeedbackSource) contextLine() string {
	if s.RunID == "" && len(s.SpecItems) > 0 {
		return "Spec check: " + strings.Join(s.SpecItems, ", ")
	}
	return "Run: " + s.RunID
}

// toolFeedbackProposalPrefix starts the first line of a question's context.
const toolFeedbackProposalPrefix = "Proposal: "

// askToolFeedback asks about one feature entry in its target project's adj. A
// target without adj, or a key adj already holds, asks nothing and is an error
// the caller keeps on the entry.
func askToolFeedback(ctx context.Context, d Deps, own registry.Project, item string, src ToolFeedbackSource, f ToolFeedback, key string) (int, error) {
	t := ToolFeedbackTarget(own, item, d.Project, f)
	if !t.Project.Adj {
		return 0, fmt.Errorf("%s does not use adj; not asked", t.Project.Name)
	}
	id, err := AskRecord(ctx, d, AskRecordConfig{Project: t.Project, Spec: toolFeedbackAskSpec(t, f, own, item, src, key)})
	if err != nil {
		return 0, fmt.Errorf("adj ask in %s: %w", t.Project.Name, err)
	}
	return id, nil
}

// ToolFeedbackDecidedFile says whether a decided adj record is a tool feedback
// question that was answered "file".
func ToolFeedbackDecidedFile(rec adjcli.Record) bool {
	return strings.HasPrefix(rec.Key, toolFeedbackAskPrefix) && rec.Decision == "file"
}

// FileDecidedToolFeedback files the slice for a tool feedback question that
// was decided "file", in project p (S388, *proposed*; adj 415, 416). The title
// is the context's Proposal line, the slicer key is the filing key the adj key
// stands for, and the first ref is the item it was discovered from. A key
// slicer already holds returns the item it filed, which is not promoted
// again, so seeing the record twice files one slice. It never decides the
// record. An outline is written in dir. It returns the filed item's id.
func FileDecidedToolFeedback(ctx context.Context, d Deps, w runrec.Writer, dir string, p registry.Project, rec adjcli.Record) (string, error) {
	title := ""
	for _, line := range strings.Split(rec.Context, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), strings.TrimSpace(toolFeedbackProposalPrefix)); ok {
			title = strings.TrimSpace(rest)
			break
		}
	}
	if title == "" {
		return "", fmt.Errorf("adj #%d has no Proposal line in its context", rec.ID)
	}
	key := toolFeedbackFilePrefix + strings.TrimPrefix(rec.Key, toolFeedbackAskPrefix)
	from := ""
	if len(rec.Refs) > 0 {
		from = rec.Refs[0]
	}
	added, err := d.Slicer.Add(ctx, w, p.Path, title, from, key)
	if err != nil {
		return "", fmt.Errorf("slicer add in %s: %w", p.Name, err)
	}
	if !(added.Existing && added.HasSlice) {
		path := filepath.Join(dir, key+".md")
		why := fmt.Sprintf("## %s\n\n### Why\n\nA role proposed this as tool feedback, and adj #%d was decided to file it:\n\n%s\n", title, rec.ID, rec.Context)
		if err := os.WriteFile(path, []byte(why), 0o600); err != nil {
			return "", fmt.Errorf("outline: %w", err)
		}
		if err := d.Slicer.Promote(ctx, w, p.Path, added.ID, path); err != nil {
			return "", fmt.Errorf("filed %s in %s, but its Why section failed: slicer promote: %w", added.ID, p.Name, err)
		}
	}
	return added.ID, nil
}

// toolFeedbackWhy is the Why section of a filed entry. It names the role, the
// run, the project, the item and the evidence, so that the slice reads on its
// own in a project that has no link to the run.
func toolFeedbackWhy(t FeedbackTarget, f ToolFeedback, own registry.Project, item string, src ToolFeedbackSource) string {
	return fmt.Sprintf("## %s\n\n### Why\n\nThe %s reported this as tool feedback (%s) on %s in %s, project %s, item %s:\n\n%s\n\nEvidence: %s\n",
		t.Title, f.Role, f.Kind, f.Tool, src.where(), own.Name, item, f.Proposal, f.Evidence)
}

// FileToolFeedback files each entry of a safe kind as an unscored slice and
// returns one result per entry, in order. An entry of the kind feature is
// asked in its target's adj instead (askToolFeedback). It runs `slicer add TITLE --key KEY`
// in the target project, then `slicer promote` with a Why outline written in
// dir; an item that the key already names and that has its spec is not
// promoted again. A failure is the entry's result, never the call's: the
// entries that follow are still filed. d.Project resolves the registered
// projects. src says where the entries came from: a run, or a spec check
// (S389, *proposed*), whose item is its one item or empty.
func FileToolFeedback(ctx context.Context, d Deps, w runrec.Writer, dir string, own registry.Project, item string, src ToolFeedbackSource, entries []ToolFeedback) []ToolFeedbackResult {
	out := make([]ToolFeedbackResult, len(entries))
	for i, f := range entries {
		key := f.Key
		if key == "" {
			key = ToolFeedbackKey(f.Tool, f.Kind, f.Proposal)
		}
		out[i].Key = key
		if f.Kind == ToolFeedbackAskKind {
			out[i].Asked, out[i].Err = askToolFeedback(ctx, d, own, item, src, f, key)
			continue
		}
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
			if err := os.WriteFile(path, []byte(toolFeedbackWhy(t, f, own, item, src)), 0o600); err != nil {
				out[i].Err = fmt.Errorf("outline: %w", err)
				continue
			}
			if err := d.Slicer.Promote(ctx, w, t.Project.Path, added.ID, path); err != nil {
				out[i].Err = fmt.Errorf("filed %s in %s, but its Why section failed: slicer promote: %w", added.ID, t.Project.Name, err)
				continue
			}
		}
		out[i].Filed = added.ID
		if !added.Existing {
			items, err := d.Slicer.List(ctx, t.Project.Path)
			if err != nil {
				out[i].Err = fmt.Errorf("near duplicates: slicer list in %s: %w", t.Project.Name, err)
				continue
			}
			out[i].Near = NearDuplicateTitles(t.Title, added.ID, items, nearDuplicateMin)
			if len(out[i].Near) > 0 {
				if err := d.Slicer.Note(ctx, w, t.Project.Path, added.ID, "possible duplicate of "+strings.Join(out[i].Near, ", ")); err != nil {
					out[i].Err = fmt.Errorf("near duplicates: slicer note in %s: %w", t.Project.Name, err)
				}
			}
			if done := NearDoneTitles(t.Title, added.ID, items, nearDuplicateMin); len(done) > 0 {
				if err := d.Slicer.Note(ctx, w, t.Project.Path, added.ID, "possibly covered by done "+strings.Join(done, ", ")); err != nil && out[i].Err == nil {
					out[i].Err = fmt.Errorf("near duplicates: slicer note in %s: %w", t.Project.Name, err)
				}
			}
		}
	}
	return out
}

// fileToolFeedback files the entries that a report newly added to the run and
// keeps each outcome on its entry. Tool feedback is advisory: a failure is
// written on the entry as file_error and the run goes on, so nothing is
// returned. It holds fileMu, as filing a later finding does.
func (e *Engine) fileToolFeedback(added []ToolFeedback) {
	var acted []ToolFeedback
	for _, f := range added {
		if ToolFeedbackSafe(f.Kind) || f.Kind == ToolFeedbackAskKind {
			acted = append(acted, f)
		}
	}
	if len(acted) == 0 {
		return
	}
	e.fileMu.Lock()
	defer e.fileMu.Unlock()
	rec := e.run.Snapshot()
	results := FileToolFeedback(context.Background(), e.d, e.wr, e.run.Dir(), e.proj, rec.Item, ToolFeedbackSource{RunID: rec.ID}, acted)
	now := e.d.now()
	e.run.Update(func(rr *runrec.Record) {
		for _, res := range results {
			for i := range rr.ToolFeedback {
				if rr.ToolFeedback[i].Key != res.Key {
					continue
				}
				if res.Filed != "" {
					rr.ToolFeedback[i].Filed, rr.ToolFeedback[i].FiledAt, rr.ToolFeedback[i].Near = res.Filed, now, res.Near
				}
				switch {
				case res.Err != nil:
					rr.ToolFeedback[i].FileError = res.Err.Error()
				case res.Asked != 0:
					rr.ToolFeedback[i].Asked, rr.ToolFeedback[i].FileError = res.Asked, ""
				case res.Filed != "":
					rr.ToolFeedback[i].FileError = ""
				}
			}
		}
	})
}
