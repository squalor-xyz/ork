// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/squalor-xyz/ork/internal/ipc"
)

// showProject is projects.show (S247, *proposed*): every setting of one
// project, from ork's registry, ork's global settings, slicer and adj. It
// writes nothing and takes no lock beyond the registry read.
//
// Slicer is read only through its CLI: `slicer config --json` and `slicer
// relay-roles show --json`. A read that slicer refuses leaves its part null
// and puts slicer's code and message in Slicer.Errors; the call still
// succeeds. adj is not read: the registry flag, and a stat of .adjudicate.json
// as addProject does.
func (d *Daemon) showProject(ctx context.Context, p ProjectsShowParams) (*ProjectsShowResult, error) {
	if p.Name == "" {
		return nil, badParams(errors.New("projects.show needs a project name"))
	}
	f, err := d.loadRegistry()
	if err != nil {
		return nil, err
	}
	project, ok := find(f, p.Name)
	if !ok {
		return nil, notFound(p.Name, f)
	}
	out := &ProjectsShowResult{
		Project: project,
		Global:  settingsResult(f),
		Slicer:  ShowSlicer{Errors: []PartError{}},
		Adj:     ShowAdj{Enabled: project.Adj},
	}
	slicer := d.runs.deps.Slicer
	partError := func(part string, err error) {
		e := ipc.ErrorOf(err)
		out.Slicer.Errors = append(out.Slicer.Errors, PartError{Part: part, Code: e.Code, Message: e.Message})
	}
	if config, err := slicer.ConfigAll(ctx, project.Path); err != nil {
		partError("config", err)
	} else {
		out.Slicer.Config = config
	}
	if m, err := slicer.RelayMatrix(ctx, project.Path); err != nil {
		partError("matrix", err)
	} else {
		rows, rules := matrixRows(project, m)
		out.Slicer.Matrix = &ShowMatrix{Rows: rows, Rules: rules, Revision: m.Revision}
	}
	if info, err := os.Stat(filepath.Join(project.Path, ".adjudicate.json")); err == nil && info.Mode().IsRegular() {
		out.Adj.ConfigFile = true
	}
	return out, nil
}
