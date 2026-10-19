// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"os"
	"os/exec"
)

// startDaemon starts `ork daemon run` from this binary, detached: in its own
// session, with its stdio on /dev/null. It does not wait for the socket; the
// command that started it waits, as cli.App.connect does for StartWait. A
// child that finds the lock held by a daemon that is exiting retries it for
// two seconds (serveWithRetry), so the race with that exit still serves.
func startDaemon(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	cmd := exec.Command(exe, "daemon", "run")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devNull, devNull, devNull
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // the daemon outlives this command; the wait only reaps it
	return nil
}
