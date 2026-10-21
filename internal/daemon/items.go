// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"strings"
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
		return nil, notFound(p.Project)
	}
	detail, err := d.runs.deps.Slicer.Show(ctx, project.Path, p.ID)
	if err != nil {
		return nil, err
	}
	return &ItemShowResult{Detail: detail}, nil
}
