// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"os"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// startDaemon starts `ork daemon run` from this binary, detached: in its own
// session, with stderr in daemon.log beside the socket (daemon.StartExe, which
// a daemon that rebuilds itself uses too, S431).
func startDaemon(ctx context.Context) error {
	start, err := prepareDaemonRestart(ctx)
	if err != nil {
		return err
	}
	return start(ctx)
}

// prepareDaemonRestart resolves the installed build before the CLI stops the
// old daemon, and pins that path for the subsequent start.
func prepareDaemonRestart(ctx context.Context) (func(context.Context) error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	paths, err := daemon.DefaultPaths()
	if err != nil {
		return nil, err
	}
	return prepareDaemonRestartExe(ctx, exe, paths)
}

// startDaemonExe keeps the detached child's diagnostics in the state directory.
func startDaemonExe(ctx context.Context, exe string, paths daemon.Paths) error {
	return daemon.StartExe(ctx, exe, paths)
}

// prepareDaemonRestartExe captures the launch target before daemon.stop.
func prepareDaemonRestartExe(ctx context.Context, exe string, paths daemon.Paths) (func(context.Context) error, error) {
	return daemon.PrepareRestartExe(ctx, exe, paths)
}
