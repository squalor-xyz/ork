// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
)

// defaultBaseBranch is the base branch of a tracking commit that names none
// (S405, *proposed*; adj 461). Nothing is stored for it.
const defaultBaseBranch = "main"

// trackingCommit commits the project's uncommitted tracking files on the base
// branch of its main checkout. It holds specMu, so it does not commit half of
// a spec answer that is still being filed.
func (d *Daemon) trackingCommit(ctx context.Context, p TrackingCommitParams) (*TrackingCommitResult, error) {
	if p.Project == "" {
		return nil, badParams(errors.New("tracking commit needs a project"))
	}
	d.runs.specMu.Lock()
	defer d.runs.specMu.Unlock()
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	base := p.Base
	if base == "" {
		base = defaultBaseBranch
	}
	got, err := engine.CommitTrackingFiles(ctx, engine.PlainGit(d.runDeps(proj)), proj.Path, base, p.Leave)
	if err != nil {
		var refusal *engine.TrackingError
		if errors.As(err, &refusal) {
			return nil, &ipc.Error{Code: refusal.Code, Message: refusal.Message}
		}
		return nil, err
	}
	return &TrackingCommitResult{Hash: got.Hash, Subject: got.Subject, Files: len(got.Files)}, nil
}
