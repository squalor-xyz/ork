// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// The spec draft (S179, scope.spec-drafting, decided 2026-11-16). A read-only
// role proposes the text of one slice's spec sections; the daemon remembers the
// proposal with the fingerprint of the spec it was made against and writes
// nothing. An approval names the sections the owner approved, and the daemon
// replaces each with `slicer edit ID --section NAME --text ... --render`, after
// it checks that the spec still has that fingerprint. The job is modelled on the
// spec check: it runs on the spec check row (roles.spec-draft-row), outside any
// run, with the read-only fingerprint of the tree (role.readonly-fingerprint).
const (
	MethodSpecDraft      = "spec.draft"       // proposed wire; params SpecDraftParams, result SpecDraftResult
	MethodSpecDraftApply = "spec.draft-apply" // proposed wire; params SpecDraftApplyParams, result SpecDraftApplyResult
)

// SpecDraftParams names the project and the one item to draft.
type SpecDraftParams struct {
	Project string `json:"project"`
	Item    string `json:"item"`
}

// SpecDraftSectionView is one section of the draft as the owner sees it: the
// text the slice has now, the proposal, and the command an approval runs. An
// unchanged section has no proposal and no command. Problems are what ork found
// wrong in the proposal; a section with a problem cannot be approved.
type SpecDraftSectionView struct {
	Name      string   `json:"name"`
	Current   string   `json:"current,omitempty"`
	Text      string   `json:"text,omitempty"`
	Unchanged bool     `json:"unchanged,omitempty"`
	Command   string   `json:"command,omitempty"`
	Problems  []string `json:"problems,omitempty"`
}

// SpecDraftResult is the draft of one item. OwnerNeeded is the text the role
// wrote under Owner needed, empty for none.
type SpecDraftResult struct {
	Item        string                 `json:"item"`
	Sections    []SpecDraftSectionView `json:"sections"`
	OwnerNeeded string                 `json:"owner_needed,omitempty"`
}

// SpecDraftApplyParams names the sections of the item's draft that the owner
// approved.
type SpecDraftApplyParams struct {
	Project  string   `json:"project"`
	Item     string   `json:"item"`
	Sections []string `json:"sections"`
}

// SpecDraftApplyResult names the sections that were replaced, in order.
type SpecDraftApplyResult struct {
	Item    string   `json:"item"`
	Applied []string `json:"applied"`
}

// specDraftEntry is what the daemon remembers of a draft until it is replaced,
// fails to be redone or the daemon stops: the views, the fingerprint of the
// spec the draft is valid for, and the sections already applied. An approval
// moves the fingerprint on past its own edits and no further.
type specDraftEntry struct {
	fingerprint string
	ownerNeeded string
	sections    []SpecDraftSectionView
	applied     map[string]bool
}

// currentSections maps each heading of `slicer show --json` to its body.
func currentSections(raw []byte) (map[string]string, error) {
	var detail slicercli.Detail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return nil, fmt.Errorf("slicer show: %w", err)
	}
	out := map[string]string{}
	if detail.Slice != nil {
		for _, s := range detail.Slice.Sections {
			out[strings.ToLower(s.Heading)] = strings.TrimSpace(s.Body)
		}
	}
	return out, nil
}

func refused(format string, a ...any) error {
	return &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf(format, a...)}
}

// specDraft runs one read-only pass of the spec-draft role over the item and
// remembers the proposal. It writes nothing to the item. A draft that fails
// leaves no proposal, and a new draft replaces an old one. Drafts, spec checks
// and approvals run one at a time, so no edit lands under a running role.
func (d *Daemon) specDraft(ctx context.Context, p SpecDraftParams) (*SpecDraftResult, error) {
	if p.Project == "" || p.Item == "" {
		return nil, badParams(errors.New("spec draft needs a project and an item"))
	}
	d.runs.specMu.Lock()
	defer d.runs.specMu.Unlock()
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	key := specKey(proj.Name, p.Item)
	d.runs.mu.Lock()
	delete(d.runs.drafts, key)
	d.runs.mu.Unlock()
	deps := d.runDeps(proj)
	chain, err := d.specChain(ctx, deps, proj, p.Item)
	if err != nil {
		return nil, err
	}
	ctx, end, err := d.beginSpecJob(ctx)
	if err != nil {
		return nil, err
	}
	defer end()
	d.runs.specStateMu.Lock()
	_, err = d.registered(proj.Name)
	d.runs.specStateMu.Unlock()
	if err != nil {
		return nil, err
	}
	res, err := engine.RunSpecDraft(ctx, deps, engine.SpecDraftConfig{Project: proj, Item: p.Item, Lineup: chain, Dir: filepath.Join(d.specDir(proj.Name, p.Item), "draft")})
	if errors.Is(err, engine.ErrNoSpec) {
		return nil, refused("%s has no slice spec to draft into; run slicer promote %s first", p.Item, p.Item)
	}
	if err != nil {
		return nil, err
	}
	current, err := currentSections(res.Raw)
	if err != nil {
		return nil, err
	}
	problems := map[string][]string{}
	for _, pr := range res.Problems {
		problems[pr.Section] = append(problems[pr.Section], pr.Message)
	}
	out := &SpecDraftResult{Item: p.Item, OwnerNeeded: res.Draft.OwnerNeeded}
	for _, s := range res.Draft.Sections {
		v := SpecDraftSectionView{Name: s.Name, Current: current[strings.ToLower(s.Name)], Unchanged: s.Unchanged, Problems: problems[s.Name]}
		if !s.Unchanged {
			v.Text = s.Text
			v.Command = matrixCommand(slicercli.EditReplaceArgs(p.Item, s.Name, s.Text))
		}
		out.Sections = append(out.Sections, v)
	}
	d.runs.mu.Lock()
	if d.runs.drafts == nil {
		d.runs.drafts = map[string]specDraftEntry{}
	}
	d.runs.drafts[key] = specDraftEntry{fingerprint: res.Fingerprint, ownerNeeded: out.OwnerNeeded, sections: out.Sections, applied: map[string]bool{}}
	d.runs.mu.Unlock()
	return out, nil
}

// specDraftApply replaces each approved section of the item's draft with its
// proposal, one `slicer edit` for each, in the order given. It refuses, before
// it edits anything, an item with no draft, a spec that changed since the draft
// (or since the last approval), and a section that is not in the draft, was left
// unchanged, has a problem or is already applied. A change in the spec made by
// anyone else between the draft and an approval is what the fingerprint catches.
func (d *Daemon) specDraftApply(ctx context.Context, p SpecDraftApplyParams) (*SpecDraftApplyResult, error) {
	if p.Project == "" || p.Item == "" || len(p.Sections) == 0 {
		return nil, badParams(errors.New("spec draft approval needs a project, an item and sections"))
	}
	d.runs.specMu.Lock()
	defer d.runs.specMu.Unlock()
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	key := specKey(proj.Name, p.Item)
	d.runs.mu.Lock()
	e, ok := d.runs.drafts[key]
	closed := d.runs.closed
	d.runs.mu.Unlock()
	if closed {
		return nil, errors.New("run manager is closed")
	}
	if !ok {
		return nil, refused("%s has no spec draft to approve; draft it first", p.Item)
	}
	deps := d.runDeps(proj)
	raw, err := deps.Slicer.ShowRaw(ctx, proj.Path, p.Item)
	if err != nil {
		return nil, err
	}
	if fp, err := engine.SpecFingerprint(raw); err != nil {
		return nil, err
	} else if fp != e.fingerprint {
		return nil, refused("spec changed since the draft of %s; draft it again", p.Item)
	}
	var names []string
	for _, want := range p.Sections {
		i := slices.IndexFunc(e.sections, func(v SpecDraftSectionView) bool { return strings.EqualFold(v.Name, strings.TrimSpace(want)) })
		if i < 0 {
			return nil, refused("%s has no section %q in its draft", p.Item, want)
		}
		v := e.sections[i]
		switch {
		case slices.Contains(names, v.Name):
			continue
		case v.Unchanged:
			return nil, refused("the draft leaves %s of %s unchanged; there is nothing to apply", v.Name, p.Item)
		case len(v.Problems) > 0:
			return nil, refused("%s of %s has a problem and cannot be applied: %s", v.Name, p.Item, strings.Join(v.Problems, "; "))
		case e.applied[v.Name]:
			return nil, refused("%s of %s is already applied", v.Name, p.Item)
		}
		names = append(names, v.Name)
	}
	out := &SpecDraftApplyResult{Item: p.Item, Applied: []string{}}
	forget := func() {
		d.runs.mu.Lock()
		delete(d.runs.drafts, key)
		d.runs.mu.Unlock()
	}
	for _, name := range names {
		v := e.sections[slices.IndexFunc(e.sections, func(v SpecDraftSectionView) bool { return v.Name == name })]
		if err := deps.Slicer.EditReplace(ctx, runrec.Writer{}, proj.Path, p.Item, name, v.Text); err != nil {
			// What was applied stays applied; the draft stays valid for the rest
			// only when the spec can be read again.
			d.keepDraft(ctx, deps, proj, p.Item, key, e, out.Applied)
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out.Applied = append(out.Applied, name)
	}
	raw, err = deps.Slicer.ShowRaw(ctx, proj.Path, p.Item)
	if err != nil {
		forget()
		return nil, fmt.Errorf("sections applied to %s, but its spec could not be read again: %w", p.Item, err)
	}
	fp, err := engine.SpecFingerprint(raw)
	if err != nil {
		forget()
		return nil, err
	}
	e.fingerprint = fp
	applied := map[string]bool{}
	for k := range e.applied {
		applied[k] = true
	}
	for _, name := range out.Applied {
		applied[name] = true
	}
	e.applied = applied
	d.runs.mu.Lock()
	d.runs.drafts[key] = e
	d.runs.mu.Unlock()
	// The edit is the write; the refresh is only freshness.
	_ = d.cache.Refresh(ctx, proj, source.Items)
	return out, nil
}

// keepDraft records the sections applied before a failed edit and moves the
// draft's fingerprint to the spec as it is now. When the spec cannot be read, the
// draft is forgotten, so the next approval asks for a new one.
func (d *Daemon) keepDraft(ctx context.Context, deps engine.Deps, proj registry.Project, item, key string, e specDraftEntry, applied []string) {
	forget := func() {
		d.runs.mu.Lock()
		delete(d.runs.drafts, key)
		d.runs.mu.Unlock()
	}
	raw, err := deps.Slicer.ShowRaw(ctx, proj.Path, item)
	if err != nil {
		forget()
		return
	}
	fp, err := engine.SpecFingerprint(raw)
	if err != nil {
		forget()
		return
	}
	e.fingerprint = fp
	marks := map[string]bool{}
	for k := range e.applied {
		marks[k] = true
	}
	for _, name := range applied {
		marks[name] = true
	}
	e.applied = marks
	d.runs.mu.Lock()
	d.runs.drafts[key] = e
	d.runs.mu.Unlock()
}
