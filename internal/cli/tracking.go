// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/squalor-xyz/ork/internal/daemon"
)

func (a *App) tracking(ctx context.Context, o *output, args []string) int {
	if len(args) == 0 {
		return o.usage("tracking: takes commit")
	}
	switch args[0] {
	case "commit":
		return a.trackingCommit(ctx, o, args[1:])
	default:
		return o.usage("tracking: unknown command %q", args[0])
	}
}

// trackingCommit commits a project's uncommitted tracking files on its base
// branch (S405, *proposed*). The message is built from the paths, so there is
// no message flag (adj 453).
func (a *App) trackingCommit(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("tracking commit", flag.ContinueOnError)
	p := daemon.TrackingCommitParams{}
	fs.StringVar(&p.Base, "base", "", "base branch (default main)")
	fs.Func("leave", "PATH", func(v string) error { p.Leave = append(p.Leave, v); return nil })
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("tracking commit: %v", err)
	}
	if len(pos) != 1 {
		return o.usage("tracking commit: takes PROJECT")
	}
	p.Project = pos[0]
	var out daemon.TrackingCommitResult
	if code := a.call(ctx, o, daemon.MethodTrackingCommit, p, &out); code != 0 {
		return code
	}
	if o.json {
		return o.printJSON(out)
	}
	noun := "files"
	if out.Files == 1 {
		noun = "file"
	}
	fmt.Fprintf(o.stdout, "committed %s %s (%d %s)\n", plainLine(out.Hash), plainLine(out.Subject), out.Files, noun)
	return ExitOK
}
