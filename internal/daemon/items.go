// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func (d *Daemon) itemShow(ctx context.Context, p ItemShowParams) (*ItemShowResult, error) {
	if strings.TrimSpace(p.ID) == "" {
		return nil, badParams(fmt.Errorf("id is required"))
	}
	f, err := d.loadRegistry()
	if err != nil {
		return nil, err
	}
	project, ok := find(f, p.Project)
	if !ok {
		return nil, notFound(p.Project, f)
	}
	detail, err := d.runs.deps.Slicer.Show(ctx, project.Path, p.ID)
	if err != nil {
		return nil, err
	}
	return &ItemShowResult{Detail: detail}, nil
}

func (d *Daemon) itemNote(ctx context.Context, p ItemNoteParams) (*ItemNoteResult, error) {
	if strings.TrimSpace(p.Item) == "" || strings.TrimSpace(p.Text) == "" {
		return nil, badParams(fmt.Errorf("item and text are required"))
	}
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	if err := d.runDeps(proj).Slicer.Note(ctx, runrec.Writer{}, proj.Path, p.Item, p.Text); err != nil {
		return nil, err
	}
	if err := d.cache.Refresh(ctx, proj, source.Items); err != nil {
		return nil, err
	}
	return &ItemNoteResult{Item: p.Item}, nil
}

func (d *Daemon) itemNew(ctx context.Context, p ItemNewParams) (*ItemNewResult, error) {
	cfg := engine.NewSliceConfig{Title: p.Title, Why: p.Why, From: p.From, FromNote: p.FromNote, Size: p.Size, Importance: p.Importance, Urgency: p.Urgency, Effort: p.Effort, TempDir: d.cfg.Paths.Dir}
	if err := engine.ValidateNewSlice(cfg); err != nil {
		return nil, badParams(err)
	}
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	cfg.Project = proj
	id, existing, err := engine.FileNewSlice(ctx, d.runDeps(proj), cfg)
	if err != nil {
		return nil, err
	}
	if err := d.cache.Refresh(ctx, proj, source.Items, source.Deps); err != nil {
		return nil, err
	}
	return &ItemNewResult{ID: id, Existing: existing}, nil
}

func (d *Daemon) itemPark(ctx context.Context, p ItemParkParams) (*ItemParkResult, error) {
	if strings.TrimSpace(p.Project) == "" || strings.TrimSpace(p.Item) == "" {
		return nil, badParams(fmt.Errorf("project and item are required"))
	}
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	if err := d.refuseUnfinishedItem(p.Project, p.Item); err != nil {
		return nil, err
	}
	if err := d.runDeps(proj).Slicer.Park(ctx, runrec.Writer{}, proj.Path, p.Item); err != nil {
		return nil, err
	}
	if err := d.cache.Refresh(ctx, proj, source.Items, source.Deps); err != nil {
		return nil, err
	}
	return &ItemParkResult{Item: p.Item}, nil
}

// refuseUnfinishedItem reads the same stored and live state as unfinishedRunsLocked.
func (d *Daemon) refuseUnfinishedItem(project, item string) error {
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		return err
	}
	for _, r := range stored {
		rec := r.Snapshot()
		if m := d.runs.runs[rec.ID]; m != nil && m.engine != nil {
			rec = m.engine.Snapshot()
		}
		if rec.Project == project && rec.Item == item && !runrec.IsFinal(rec.State) {
			return &ipc.Error{Code: CodeRefused, Message: fmt.Sprintf("Cannot park %s: run %s is unfinished", item, rec.ID)}
		}
	}
	return nil
}

func (d *Daemon) itemUnpark(ctx context.Context, p ItemParkParams) (*ItemParkResult, error) {
	if strings.TrimSpace(p.Project) == "" || strings.TrimSpace(p.Item) == "" {
		return nil, badParams(fmt.Errorf("project and item are required"))
	}
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	if err := d.runDeps(proj).Slicer.Unpark(ctx, runrec.Writer{}, proj.Path, p.Item); err != nil {
		return nil, err
	}
	if err := d.cache.Refresh(ctx, proj, source.Items, source.Deps); err != nil {
		return nil, err
	}
	return &ItemParkResult{Item: p.Item}, nil
}

func (d *Daemon) itemMove(ctx context.Context, p ItemMoveParams) (*ItemMoveResult, error) {
	if strings.TrimSpace(p.Project) == "" || strings.TrimSpace(p.Item) == "" || strings.TrimSpace(p.Other) == "" {
		return nil, badParams(fmt.Errorf("project, item and other are required"))
	}
	if p.Position != "before" && p.Position != "after" {
		return nil, badParams(fmt.Errorf("position must be before or after"))
	}
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	c := d.runDeps(proj).Slicer
	before, err := c.List(ctx, proj.Path)
	if err != nil {
		return nil, err
	}
	if err := c.Move(ctx, runrec.Writer{}, proj.Path, p.Item, p.Other, p.Position); err != nil {
		return nil, err
	}
	after, listErr := c.List(ctx, proj.Path)
	if err := d.cache.Refresh(ctx, proj, source.Items, source.Deps); err != nil {
		return nil, err
	}
	if listErr != nil {
		return nil, listErr
	}
	return &ItemMoveResult{Item: p.Item, Changed: itemIndex(before, p.Item) != itemIndex(after, p.Item)}, nil
}

func itemIndex(items []slicercli.Item, id string) int {
	for i, it := range items {
		if it.ID == id {
			return i
		}
	}
	return -1
}
