// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
)

// startDaemon starts `ork daemon run` from this binary, detached: in its own
// session, with stderr in daemon.log beside the socket. The command that
// started it waits for the socket, as cli.App.connect does for StartWait. A
// child that finds the lock held by a daemon that is exiting retries it for
// two seconds (serveWithRetry), so the race with that exit still serves.
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ipc.MkdirPrivate(paths.Dir); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(paths.Dir, "daemon.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	cmd := exec.Command(exe, "daemon", "run")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devNull, devNull, log
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // the daemon outlives this command; the wait only reaps it
	return nil
}

// prepareDaemonRestartExe captures the launch target before daemon.stop.
func prepareDaemonRestartExe(ctx context.Context, exe string, paths daemon.Paths) (func(context.Context) error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	exe = strings.TrimSuffix(exe, " (deleted)")
	info, err := os.Stat(exe)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, fmt.Errorf("daemon executable %q is not executable", exe)
	}
	return func(ctx context.Context) error { return startDaemonExe(ctx, exe, paths) }, nil
}
