// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
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
