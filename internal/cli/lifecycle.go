// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
)

func daemonBuild(ctx context.Context, call func(context.Context, string, any, any) error) (string, error) {
	var info daemon.DaemonInfoResult
	err := call(ctx, daemon.MethodDaemonInfo, nil, &info)
	var reply *ipc.Error
	if errors.As(err, &reply) && reply.Code == daemon.CodeUnknownMethod {
		return "unknown", nil
	}
	return info.BuildID, err
}

// CheckDaemonBuild warns about a stale daemon without reading any projects.
// An older daemon that does not know daemon.info has an unknown build.
func (a *App) CheckDaemonBuild(ctx context.Context, c *daemon.Client, stderr io.Writer) error {
	build, err := daemonBuild(ctx, c.Call)
	if err != nil {
		return err
	}
	a.warnStale(&output{stderr: stderr}, build)
	return nil
}

func (a *App) buildID() string {
	if a.BuildID == "" {
		return "dev"
	}
	return a.BuildID
}

func (a *App) staleWarning(build string) string {
	if build != "" && build != "unknown" && build == a.buildID() {
		return ""
	}
	if build == "" {
		build = "unknown"
	}
	return fmt.Sprintf("stale daemon build %s differs from CLI build %s; run ork daemon restart", build, a.buildID())
}

func (a *App) warnStale(o *output, build string) {
	if warning := a.staleWarning(build); warning != "" {
		fmt.Fprintf(o.stderr, "ork: warning: %s\n", warning)
	}
}

// daemonCommand never starts a daemon just to stop it. Restart waits until
// the old socket is removed, then uses the usual start-and-connect path.
func (a *App) daemonCommand(ctx context.Context, o *output, args []string) int {
	if len(args) == 0 || (args[0] != "stop" && args[0] != "restart") {
		return o.usage("daemon needs stop or restart [--force]")
	}
	sub := args[0]
	fs := flag.NewFlagSet("daemon "+sub, flag.ContinueOnError)
	force := fs.Bool("force", false, "cancel active work")
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args[1:])
	if err != nil {
		return o.usage("daemon %s: %v", sub, err)
	}
	if len(pos) != 0 {
		return o.usage("daemon %s takes no arguments", sub)
	}
	c, err := daemon.Dial(ctx, a.Socket)
	if err != nil {
		// A missing socket or a dead daemon's leftover socket is already stopped.
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
			return o.fail(ExitFailure, codeDaemon, err.Error())
		}
	} else {
		// A failed build probe must not prevent an explicit shutdown request.
		_ = a.CheckDaemonBuild(ctx, c, o.stderr)
		var result daemon.StopResult
		err = c.Call(ctx, daemon.MethodDaemonStop, daemon.StopParams{Force: *force}, &result)
		c.Close()
		if err != nil {
			return o.failErr(err)
		}
		wait := a.StartWait
		if wait <= 0 {
			wait = defaultStartWait
		}
		waitCtx, cancel := context.WithTimeout(ctx, wait)
		defer cancel()
		for {
			_, err := os.Lstat(a.Socket)
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			if err != nil {
				return o.fail(ExitFailure, codeDaemon, err.Error())
			}
			select {
			case <-waitCtx.Done():
				return o.fail(ExitFailure, codeDaemon, "daemon did not stop: "+waitCtx.Err().Error())
			case <-time.After(defaultPollEvery):
			}
		}
	}
	if sub == "restart" {
		c, err := a.connect(ctx)
		if err != nil {
			return o.fail(ExitFailure, codeDaemon, err.Error())
		}
		c.Close()
	}
	state := "stopped"
	if sub == "restart" {
		state = "restarted"
	}
	if o.json {
		return o.printJSON(map[string]bool{state: true})
	}
	fmt.Fprintf(o.stdout, "daemon %s\n", state)
	return ExitOK
}
