// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// The spec check (adj 164, roles.spec-check; docs/design.md, Wave lifecycle).
// The clients ask for it before they start a wave. It is a read-only job of
// the daemon, not a run: no run record, no worktree, and the role works in
// the project checkout with the tree fingerprint of a read-only role.
const (
	MethodSpecCheck  = "spec.check"  // proposed wire; params SpecCheckParams, result SpecCheckResult
	MethodSpecAnswer = "spec.answer" // proposed wire; params SpecAnswerParams, result SpecAnswerResult
)

// CodeSpecQuestions is the error code of an item that wave.start refuses
// because its spec check left questions open.
const CodeSpecQuestions = "spec_questions"

// SpecCheckParams names the project and the items to check in one pass.
type SpecCheckParams struct {
	Project string   `json:"project"`
	Items   []string `json:"items"`
}

// SpecItemResult is the check's answer for one item against its current
// spec: ready, or the questions that are still open.
type SpecItemResult struct {
	Item      string                `json:"item"`
	Ready     bool                  `json:"ready"`
	Questions []engine.SpecQuestion `json:"questions,omitempty"`
}

// SpecCheckResult has one entry for each requested item, in order.
type SpecCheckResult struct {
	Items       []SpecItemResult `json:"items"`
	OwnerNeeded string           `json:"owner_needed,omitempty"`
}

// SpecAnswerParams carries the owner's answers to one item's open questions,
// one for each question in the order the check gave them. Adj files the
// questions that have a decision key in adj, as By.
type SpecAnswerParams struct {
	Project string   `json:"project"`
	Item    string   `json:"item"`
	Answers []string `json:"answers"`
	Adj     bool     `json:"adj,omitempty"`
	By      string   `json:"by,omitempty"`
}

// SpecAnswerResult says the item is ready once its answers are in the spec.
// Notices say what went to adj, or why not.
type SpecAnswerResult struct {
	Item    string   `json:"item"`
	Ready   bool     `json:"ready"`
	Notices []string `json:"notices,omitempty"`
}

// specEntry is what the daemon remembers of a check: the verdict, and the
// fingerprint of the spec it was made against. A verdict counts only while
// the spec still has that fingerprint. Role verdicts live in memory; owner
// answers are also saved beside the check until run start or a spec change.
type specEntry struct {
	fingerprint string
	ready       bool
	questions   []engine.SpecQuestion
	ownerNeeded string
	decisions   string // the paragraph recorded for the answers; the item's run carries it
}

func specKey(project, item string) string { return project + "\x00" + item }

// specChain finds the lineup of the spec check: the row that the rule naming
// "Spec check row (rNN)" gives, taken from the reviewer rows slicer
// recommends for the item.
func (d *Daemon) specChain(ctx context.Context, deps engine.Deps, proj registry.Project, item string) ([]runrec.LineupEntry, error) {
	rules, err := deps.Slicer.RelayRules(ctx, proj.Path)
	if err != nil {
		return nil, err
	}
	row := slicercli.SpecCheckRowID(rules)
	if row == "" {
		return nil, errors.New(`the matrix has no Spec check row: add a rule that names it as "Spec check row (rNN)" (slicer relay-roles rule-add)`)
	}
	rec, err := deps.Slicer.RecommendRoles(ctx, proj.Path, item, false, false)
	if err != nil {
		return nil, err
	}
	var chain []runrec.LineupEntry
	for _, choice := range rec.Roles {
		if choice.Role != engine.RoleReviewer {
			continue
		}
		rows := choice.Alternatives
		if choice.Row != nil {
			rows = append([]slicercli.Row{*choice.Row}, rows...)
		}
		for _, r := range rows {
			if r.ID != row {
				continue
			}
			if vendor := matrixAdapters[strings.ToLower(r.Agent)]; vendor != "" {
				chain = appendEntry(chain, runrec.LineupEntry{Vendor: vendor, Model: r.Model, Effort: r.Reasoning})
			}
		}
	}
	if len(chain) == 0 {
		return nil, fmt.Errorf("the Spec check row (%s) is not among the rows with an adapter that slicer recommends for %s", row, item)
	}
	return chain, nil
}

// specCheck answers for each item from the check it remembers, and runs one
// read-only pass of the spec-check role over the items whose spec has no
// check yet or changed since it. Checks run one at a time, so a second
// caller finds the first one's answers.
func (d *Daemon) specCheck(ctx context.Context, p SpecCheckParams) (*SpecCheckResult, error) {
	if p.Project == "" || len(p.Items) == 0 {
		return nil, badParams(errors.New("spec check needs a project and items"))
	}
	seen := map[string]bool{}
	var items []string
	for _, item := range p.Items {
		if item == "" {
			return nil, badParams(errors.New("spec check item is empty"))
		}
		if !seen[item] {
			seen[item] = true
			items = append(items, item)
		}
	}
	d.runs.specMu.Lock()
	defer d.runs.specMu.Unlock()
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	deps := d.runDeps(proj)
	var stale []string
	for _, item := range items {
		raw, err := deps.Slicer.ShowRaw(ctx, proj.Path, item)
		if err != nil {
			return nil, err
		}
		fp, err := engine.SpecFingerprint(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", item, err)
		}
		if err := d.loadSpecAnswers(proj.Name, item, fp); err != nil {
			return nil, err
		}
		d.runs.mu.Lock()
		e, ok := d.runs.specs[specKey(proj.Name, item)]
		d.runs.mu.Unlock()
		if !ok || e.fingerprint != fp {
			stale = append(stale, item)
		}
	}
	if len(stale) > 0 {
		if err := d.runSpecCheck(ctx, deps, proj, stale); err != nil {
			return nil, err
		}
	}
	out := &SpecCheckResult{Items: []SpecItemResult{}}
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	ownerTexts := map[string]bool{}
	for _, item := range items {
		e := d.runs.specs[specKey(proj.Name, item)]
		out.Items = append(out.Items, SpecItemResult{Item: item, Ready: e.ready, Questions: e.questions})
		if e.ownerNeeded != "" && !ownerTexts[e.ownerNeeded] {
			if out.OwnerNeeded != "" {
				out.OwnerNeeded += "\n\n"
			}
			out.OwnerNeeded += e.ownerNeeded
			ownerTexts[e.ownerNeeded] = true
		}
	}
	return out, nil
}

// runSpecCheck runs the role over items and remembers each verdict. The job
// counts as active work, so the daemon does not stop or restart under it.
func (d *Daemon) runSpecCheck(ctx context.Context, deps engine.Deps, proj registry.Project, items []string) error {
	chain, err := d.specChain(ctx, deps, proj, items[0])
	if err != nil {
		return err
	}
	d.runs.mu.Lock()
	if d.runs.closed {
		d.runs.mu.Unlock()
		return errors.New("run manager is closed")
	}
	// The job runs under the manager's context too, so closing the manager (a
	// forced daemon stop) cancels it without waiting for the role.
	jobCtx, cancel := context.WithCancel(d.runs.ctx)
	d.runs.specSeq++
	id := d.runs.specSeq
	d.runs.specCancels[id] = cancel
	d.runs.specJobs++
	d.runs.wg.Add(1)
	d.runs.mu.Unlock()
	stop := context.AfterFunc(ctx, cancel)
	defer func() {
		stop()
		cancel()
		d.runs.mu.Lock()
		delete(d.runs.specCancels, id)
		d.runs.specJobs--
		d.runs.mu.Unlock()
		d.runs.wg.Done()
	}()
	ctx = jobCtx
	d.runs.specStateMu.Lock()
	if _, err := d.registered(proj.Name); err != nil {
		d.runs.specStateMu.Unlock()
		return err
	}
	for _, item := range items {
		if err := writeSpecJSON(filepath.Join(d.specDir(proj.Name, item), "check.json"), specCheckIdentity{Project: proj.Name, Item: item}); err != nil {
			d.runs.specStateMu.Unlock()
			return err
		}
	}
	d.runs.specStateMu.Unlock()
	dir := d.specDir(proj.Name, items[0])
	res, err := engine.RunSpecCheck(ctx, deps, engine.SpecCheckConfig{Project: proj, Items: items, Lineup: chain, Dir: dir})
	if err != nil {
		var retried *engine.SpecCheckRetryError
		if errors.As(err, &retried) {
			return &ipc.Error{Code: "spec_check_retry", Message: err.Error()}
		}
		return err
	}
	d.runs.specStateMu.Lock()
	defer d.runs.specStateMu.Unlock()
	if _, err := d.registered(proj.Name); err != nil {
		return err
	}
	report, err := os.ReadFile(filepath.Join(dir, "spec-check.md"))
	if err != nil {
		return err
	}
	for _, item := range items[1:] {
		if err := os.WriteFile(filepath.Join(d.specDir(proj.Name, item), "spec-check.md"), report, 0600); err != nil {
			return err
		}
	}
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	for _, v := range res.Verdicts {
		d.runs.specs[specKey(proj.Name, v.Item)] = specEntry{fingerprint: res.Specs[v.Item], ready: v.Ready, questions: v.Questions, ownerNeeded: res.OwnerNeeded}
	}
	return nil
}

// specAnswer writes the owner's answers into the item's spec and marks the
// item ready for the spec it now has. It refuses an item with no open
// questions, and a spec that changed since the check.
func (d *Daemon) specAnswer(ctx context.Context, p SpecAnswerParams) (*SpecAnswerResult, error) {
	if p.Project == "" || p.Item == "" {
		return nil, badParams(errors.New("spec answer needs a project and an item"))
	}
	d.runs.specMu.Lock()
	defer d.runs.specMu.Unlock()
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	d.runs.mu.Lock()
	e, ok := d.runs.specs[specKey(proj.Name, p.Item)]
	closed := d.runs.closed
	d.runs.mu.Unlock()
	if closed {
		return nil, errors.New("run manager is closed")
	}
	if !ok || e.ready || len(e.questions) == 0 {
		return nil, fmt.Errorf("%s has no open spec questions", p.Item)
	}
	deps := d.runDeps(proj)
	raw, err := deps.Slicer.ShowRaw(ctx, proj.Path, p.Item)
	if err != nil {
		return nil, err
	}
	if fp, err := engine.SpecFingerprint(raw); err != nil {
		return nil, err
	} else if fp != e.fingerprint {
		return nil, fmt.Errorf("spec changed since the check of %s; run the check again", p.Item)
	}
	decisions, notices, err := engine.RecordSpecAnswers(ctx, deps, engine.SpecAnswerConfig{Project: proj, Item: p.Item, Questions: e.questions, Answers: p.Answers, Adj: p.Adj && proj.Adj, By: p.By})
	if err != nil {
		return nil, err
	}
	if p.Adj && !proj.Adj {
		for _, q := range e.questions {
			if q.Key != "" {
				notices = append(notices, "no adj in this project; not filed in adj: "+q.Key)
			}
		}
	}
	raw, err = deps.Slicer.ShowRaw(ctx, proj.Path, p.Item)
	if err != nil {
		return nil, fmt.Errorf("answers recorded in %s, but its spec could not be read again: %w", p.Item, err)
	}
	fp, err := engine.SpecFingerprint(raw)
	if err != nil {
		return nil, err
	}
	d.runs.specStateMu.Lock()
	defer d.runs.specStateMu.Unlock()
	if _, err := d.registered(proj.Name); err != nil {
		return nil, err
	}
	if err := writeSpecJSON(filepath.Join(d.specDir(proj.Name, p.Item), "answers.json"), storedSpecAnswers{Fingerprint: fp, Decisions: decisions, OwnerNeeded: e.ownerNeeded}); err != nil {
		return nil, fmt.Errorf("save owner answers %s: %w", filepath.Join(d.specDir(proj.Name, p.Item), "answers.json"), err)
	}
	d.runs.mu.Lock()
	d.runs.specs[specKey(proj.Name, p.Item)] = specEntry{fingerprint: fp, ready: true, decisions: decisions, ownerNeeded: e.ownerNeeded}
	d.runs.mu.Unlock()
	return &SpecAnswerResult{Item: p.Item, Ready: true, Notices: notices}, nil
}

// specGate refuses an item whose check, against its current spec, left
// questions open. An item that was never checked, or whose spec changed since,
// passes: the clients run the check before they start a wave.
func (d *Daemon) specGate(ctx context.Context, project, item string) error {
	d.runs.mu.Lock()
	e, ok := d.runs.specs[specKey(project, item)]
	d.runs.mu.Unlock()
	if !ok || e.ready || len(e.questions) == 0 {
		return nil
	}
	proj, err := d.registered(project)
	if err != nil {
		return err
	}
	raw, err := d.runDeps(proj).Slicer.ShowRaw(ctx, proj.Path, item)
	if err != nil {
		return err
	}
	fp, err := engine.SpecFingerprint(raw)
	if err != nil || fp != e.fingerprint {
		return nil
	}
	noun := "questions"
	if len(e.questions) == 1 {
		noun = "question"
	}
	return &ipc.Error{Code: CodeSpecQuestions, Message: fmt.Sprintf("%s has %d open spec %s; answer them first", item, len(e.questions), noun)}
}
