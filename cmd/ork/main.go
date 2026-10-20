// SPDX-License-Identifier: GPL-3.0-or-later

// Command ork opens the dashboard: a terminal UI over the projects in the
// registry (docs/dashboard.md). Its commands are the CLI of internal/cli, and
// `ork daemon run` serves the daemon's socket.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/cli"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/tui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func buildIdentity(info *debug.BuildInfo, ok bool, fallback string) string {
	if !ok || info == nil {
		return fallback
	}
	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}
	if revision == "" {
		return fallback
	}
	return revision + "+modified=" + modified
}

func currentBuildID() string {
	info, ok := debug.ReadBuildInfo()
	return buildIdentity(info, ok, version)
}

// program is what run does outside itself. main supplies the real functions;
// tests supply fakes.
type program struct {
	start  func(ctx context.Context) error // starts a daemon detached; nil means none can be started
	tui    func(tui.Config) error          // shows the TUI until the owner quits
	daemon func(ctx context.Context) error // serves the socket until ctx ends
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, program{
		start:  startDaemon,
		tui:    runTUI,
		daemon: serveDaemon,
	})
	stop()
	os.Exit(code)
}

// runTUI shows the TUI on the alternate screen until the owner quits.
func runTUI(cfg tui.Config) error {
	cfg.Theme = tui.ThemeFor(os.Getenv)
	_, err := tea.NewProgram(tui.New(cfg)).Run()
	return err
}

// run is main with its inputs passed in. It returns the exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, p program) int {
	if len(args) > 1 && args[0] == "daemon" && args[1] == "run" {
		return daemonCommand(ctx, args[1:], stderr, p)
	}
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || !strings.HasPrefix(args[0], "-")) {
		return command(ctx, args, stdout, stderr, p)
	}
	fs := flag.NewFlagSet("ork", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "ork: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "ork %s\n", version)
		return 0
	}
	return openTUI(ctx, stderr, p)
}

// command runs one command of internal/cli. The daemon it needs is started
// with p.start when nothing answers.
func command(ctx context.Context, args []string, stdout, stderr io.Writer, p program) int {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return 1
	}
	app := &cli.App{Socket: paths.Socket, Start: p.start, BuildID: currentBuildID()}
	return app.Run(ctx, args, stdout, stderr)
}

// daemonCommand runs `ork daemon run`, the daemon's own entry point.
func daemonCommand(ctx context.Context, args []string, stderr io.Writer, p program) int {
	if len(args) != 1 || args[0] != "run" {
		fmt.Fprintln(stderr, "ork: usage: ork daemon run")
		return 2
	}
	if err := p.daemon(ctx); err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return 1
	}
	return 0
}

// openTUI shows the TUI over the registry's projects. The TUI reads through
// the daemon, which is started when nothing answers.
func openTUI(ctx context.Context, stderr io.Writer, p program) int {
	reg, code := loadRegistry(stderr)
	if code != 0 {
		return code
	}
	paths, err := daemon.DefaultPaths()
	if err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return 1
	}
	app := &cli.App{Socket: paths.Socket, Start: p.start, BuildID: currentBuildID()}
	client, err := app.Connect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return 1
	}
	err = app.CheckDaemonBuild(ctx, client, stderr)
	client.Close()
	if err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return 1
	}
	feed := daemon.NewFeed(paths.Socket)
	defer feed.Close()

	live := &tui.Daemon{Connect: app.Connect, Feed: feed}
	cfg := tui.Config{
		Source:   live,
		Projects: reg.Projects,
		Live:     live,
	}
	if err := p.tui(cfg); err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return 1
	}
	return 0
}

// loadRegistry reads the registry, seeding it when it is missing, and says so
// on stderr. It returns exit code 1 when there are no projects to show.
func loadRegistry(stderr io.Writer) (*registry.File, int) {
	path, err := registry.DefaultPath()
	if err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return nil, 1
	}
	seedRoot, err := registry.DefaultSeedRoot()
	if err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return nil, 1
	}
	reg, report, err := registry.LoadOrSeed(path, seedRoot)
	if err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return nil, 1
	}
	if report != nil { // registry.seed: say so
		fmt.Fprintf(stderr, "ork: no registry found; wrote %s with %d project(s) from %s", report.Path, len(report.Written), seedRoot)
		if len(report.Skipped) > 0 {
			fmt.Fprintf(stderr, " (not found: %s)", strings.Join(report.Skipped, ", "))
		}
		fmt.Fprintln(stderr, ". Edit that file to change the projects.")
	}
	if len(reg.Projects) == 0 {
		fmt.Fprintf(stderr, "ork: no projects in %s; add some and run ork again\n", path)
		return nil, 1
	}
	return reg, 0
}

// serveDaemon serves the socket until ctx ends or the daemon has been idle
// long enough (daemon.idle).
func serveDaemon(ctx context.Context) error {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		return err
	}
	return serveWithRetry(ctx, paths.Socket, func(ctx context.Context) error {
		return daemon.New(daemon.Config{Paths: paths, BuildID: currentBuildID()}).Run(ctx)
	})
}

// The retry for a start that finds the lock held while nothing answers on the
// socket. An idle exit unlinks its socket before it releases the lock (S03),
// so such a start is a race with that exit, and the lock frees within moments.
const (
	lockRetryFor   = 2 * time.Second
	lockRetryEvery = 50 * time.Millisecond
)

// serveWithRetry runs run. When run fails with ErrAlreadyRunning and nothing
// answers on the socket, the lock is held by a daemon that is exiting, so run
// is tried again every lockRetryEvery, for up to lockRetryFor. A live daemon
// that answers keeps the error: the lock is its own, and nothing is retried.
// It returns nil when ctx ends during the wait, as Run does.
func serveWithRetry(ctx context.Context, socket string, run func(context.Context) error) error {
	deadline := time.Now().Add(lockRetryFor)
	for {
		err := run(ctx)
		if !errors.Is(err, daemon.ErrAlreadyRunning) || ctx.Err() != nil {
			return err
		}
		if c, derr := daemon.Dial(ctx, socket); derr == nil {
			c.Close()
			return err
		}
		if !time.Now().Before(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(lockRetryEvery):
		}
	}
}
