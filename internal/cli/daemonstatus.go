// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/squalor-xyz/ork/internal/daemon"
)

func shortBuild(build string) string {
	rev, suffix, _ := strings.Cut(build, "+modified=")
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if suffix == "true" {
		rev += "+modified"
	}
	if rev == "" {
		return "unknown"
	}
	return rev
}

func (a *App) daemonPID() int {
	b, err := os.ReadFile(a.Pid)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func behindMain(status daemon.StatusResult) bool {
	for _, p := range status.Projects {
		if p.SelfHost && p.BehindMain {
			return true
		}
	}
	return false
}

// behindRolesSuffix marks a behind-main notice read while roles run. Doctor
// text moves it to its own line so no line passes 80 columns.
const behindRolesSuffix = " (roles are running; restart waits)"

// doctorBehindNotice preserves the existing restart hint, including --rebuild,
// while showing the notice during active roles too.
func doctorBehindNotice(status daemon.StatusResult) string {
	running := status.RolesRunning
	status.RolesRunning = false
	notice := daemon.BehindWarning(&status)
	if running && notice != "" {
		notice += behindRolesSuffix
	}
	return notice
}

func (a *App) daemonStatus(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("daemon status", flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("daemon status: %v", err)
	}
	if len(pos) != 0 {
		return o.usage("daemon status takes no arguments")
	}
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	// Pointer booleans distinguish a failed status read from a known false.
	rep := struct {
		Running      bool   `json:"running"`
		PID          int    `json:"pid,omitempty"`
		Socket       string `json:"socket"`
		BuildID      string `json:"build_id,omitempty"`
		CLIBuildID   string `json:"cli_build_id"`
		Stale        *bool  `json:"stale,omitempty"`
		RolesRunning *bool  `json:"roles_running,omitempty"`
		BehindMain   *bool  `json:"behind_main,omitempty"`
	}{Socket: a.Socket, CLIBuildID: a.buildID()}
	call := a.Call
	if call == nil {
		c, err := daemon.Dial(ctx, a.Socket)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
				return o.fail(ExitFailure, codeDaemon, err.Error())
			}
			if o.json {
				return o.printJSON(rep)
			}
			fmt.Fprintln(o.stdout, "daemon: not running")
			return ExitOK
		}
		defer c.Close()
		call = c.Call
	}
	rep.Running = true
	build, err := daemonBuild(ctx, call)
	if err != nil {
		return o.failErr(err)
	}
	if build == "" {
		build = "unknown"
	}
	rep.BuildID = build
	rep.PID = a.daemonPID()
	stale, note := a.staleBuild(ctx, build)
	rep.Stale = &stale
	var status daemon.StatusResult
	if err := call(ctx, daemon.MethodStatus, daemon.StatusParams{}, &status); err == nil {
		rep.RolesRunning = &status.RolesRunning
		behind := behindMain(status)
		rep.BehindMain = &behind
	}
	if o.json {
		return o.printJSON(rep)
	}
	fmt.Fprintf(o.stdout, "daemon: running\nsocket: %s\n", rep.Socket)
	if rep.PID > 0 {
		fmt.Fprintf(o.stdout, "pid: %d\n", rep.PID)
	} else {
		fmt.Fprintln(o.stdout, "pid: unknown")
	}
	fmt.Fprintf(o.stdout, "build: %s\n", shortBuild(build))
	switch {
	case stale:
		fmt.Fprintln(o.stdout, "state: stale (the CLI build differs; run ork daemon restart)")
	case note != "":
		fmt.Fprintf(o.stdout, "state: current (%s)\n", note)
	default:
		fmt.Fprintln(o.stdout, "state: current")
	}
	if rep.RolesRunning != nil {
		if *rep.RolesRunning {
			fmt.Fprintln(o.stdout, "roles: running (ork daemon restart is refused; --force cancels them)")
		} else {
			fmt.Fprintln(o.stdout, "roles: idle")
		}
		if *rep.BehindMain {
			fmt.Fprintln(o.stdout, "main: behind (restart waits for the roles, or use --force)")
		} else {
			fmt.Fprintln(o.stdout, "main: current")
		}
	}
	return ExitOK
}
