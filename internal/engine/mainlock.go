// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"path/filepath"
	"strings"
)

func (e *Engine) mainLockPath(ctx context.Context) (string, error) {
	out, err := e.gitRead(ctx, e.proj.Path, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(out)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(e.proj.Path, dir)
	}
	return filepath.Join(dir, "ork-main.lock"), nil
}

func (e *Engine) lockMain(ctx context.Context) (func(), error) {
	path, err := e.mainLockPath(ctx)
	if err != nil {
		return nil, err
	}
	return lockMainFile(ctx, path)
}
