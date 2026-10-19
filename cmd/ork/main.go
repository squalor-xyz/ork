// SPDX-License-Identifier: GPL-3.0-or-later

// Command ork opens the dashboard: a terminal UI over the projects in the
// registry (docs/dashboard.md).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
	"github.com/squalor-xyz/ork/internal/tui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, runTUI))
}

// runTUI shows the TUI on the alternate screen until the owner quits.
func runTUI(cfg tui.Config) error {
	cfg.Theme = tui.ThemeFor(os.Getenv)
	_, err := tea.NewProgram(tui.New(cfg)).Run()
	return err
}

// run is main with its inputs passed in. It returns the exit code.
func run(args []string, stdout, stderr io.Writer, start func(tui.Config) error) int {
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

	path, err := registry.DefaultPath()
	if err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return 1
	}
	seedRoot, err := registry.DefaultSeedRoot()
	if err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return 1
	}
	reg, report, err := registry.LoadOrSeed(path, seedRoot)
	if err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return 1
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
		return 1
	}

	cfg := tui.Config{
		Source:   source.NewDirect(slicercli.Client{}, adjcli.Client{}),
		Projects: reg.Projects,
	}
	if err := start(cfg); err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return 1
	}
	return 0
}
