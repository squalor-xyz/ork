// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/projscan"
	"github.com/squalor-xyz/ork/internal/registry"
)

// scanResult is one row of the --json result of projects add --scan.
type scanResult struct {
	Name    string            `json:"name"`
	Path    string            `json:"path"`
	Added   bool              `json:"added"`
	Project *registry.Project `json:"project,omitempty"`
	Error   *ipc.Error        `json:"error,omitempty"`
}

type scanSkipped struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// resultLine is what a row did, as one plain line.
func resultLine(res scanResult) string {
	if res.Error != nil {
		return fmt.Sprintf("failed %s: %s", plainLine(res.Name), plainLine(res.Error.Message))
	}
	adj := ""
	if res.Project.Adj {
		adj = " (adj)"
	}
	return fmt.Sprintf("added %s %s%s", plainLine(res.Name), plainLine(res.Path), adj)
}

func stoppedLine(r projscan.Result) string {
	return r.Note() + "; scan a narrower folder to see more"
}

func scanDoc(dir string, r projscan.Result, results []scanResult, skipped []scanSkipped) map[string]any {
	return map[string]any{"root": dir, "folders": r.Folders, "stopped": r.Stopped, "results": results, "skipped": skipped}
}

func projectWord(n int) string {
	if n == 1 {
		return "project"
	}
	return "projects"
}

// projectsScan is projects add --scan DIR (S302, *proposed*): it lists the
// projects below DIR and adds the tracked ones (a .slicer or .adjudicate.json
// marker), after a yes on standard input or with --yes. A plain git repo is
// listed and left; add it with projects add PATH.
func (a *App) projectsScan(ctx context.Context, o *output, dir string, yes, named bool, pos []string) int {
	if len(pos) > 0 {
		return o.usage("projects add --scan takes no PATH, got %q", pos[0])
	}
	if named {
		return o.usage("projects add --scan does not take --name; each project is named after its folder")
	}
	if o.json && !yes {
		return o.usage("projects add --scan with --json needs --yes")
	}
	if !filepath.IsAbs(dir) {
		return o.usage("path %q is not absolute", dir)
	}
	found, err := projscan.Scan(dir)
	cands := found.Candidates
	if err != nil {
		msg := err.Error()
		if errors.Is(err, fs.ErrNotExist) {
			msg = fmt.Sprintf("path %q does not exist", dir)
		}
		return o.fail(ExitFailure, daemon.CodeInvalidPath, msg)
	}
	if len(cands) == 0 {
		msg := fmt.Sprintf("no projects found under %s (looked %d levels down", dir, projscan.MaxDepth)
		if found.Stopped {
			msg += fmt.Sprintf("; %s", stoppedLine(found))
		}
		return o.fail(ExitFailure, daemon.CodeNotFound, msg+")")
	}
	return a.withDaemon(ctx, o, func(c *daemon.Client) int {
		registered, err := c.Projects(ctx)
		if err != nil {
			return o.failErr(err)
		}
		var picked []projscan.Candidate
		skipped := []scanSkipped{}
		rows := projscan.Classify(cands, registered)
		for _, r := range rows {
			switch {
			case r.Preselected():
				picked = append(picked, r.Candidate)
			case r.Blocked():
				skipped = append(skipped, scanSkipped{r.Name, r.Path, r.Reason})
			default:
				skipped = append(skipped, scanSkipped{r.Name, r.Path, "plain git repo; add it with ork projects add PATH"})
			}
		}
		if !o.json {
			fmt.Fprintf(o.stdout, "found %d %s under %s:\n", len(rows), projectWord(len(rows)), plainLine(dir))
			tw := tabwriter.NewWriter(o.stdout, 0, 0, 2, ' ', 0)
			plainGit := false
			for _, r := range rows {
				box := "[ ]"
				switch {
				case r.Blocked():
					box = "[-]"
				case r.Preselected():
					box = "[x]"
				default:
					plainGit = true
				}
				fmt.Fprintf(tw, "  %s\t%s\t%s\t%s", box, plainLine(r.Name), r.Tag(), plainLine(r.Path))
				if r.Blocked() {
					fmt.Fprintf(tw, "\t%s", plainLine(r.Reason))
				}
				fmt.Fprintln(tw)
			}
			tw.Flush()
			if plainGit {
				fmt.Fprintln(o.stdout, "  [ ] plain git repos are not added; use ork projects add PATH")
			}
			if found.Stopped {
				fmt.Fprintln(o.stdout, stoppedLine(found))
			}
		}
		if len(picked) == 0 {
			if o.json {
				return o.printJSON(scanDoc(dir, found, []scanResult{}, skipped))
			}
			fmt.Fprintln(o.stdout, "nothing to add: no tracked project is free")
			return ExitOK
		}
		if !yes {
			fmt.Fprintf(o.stdout, "Add the %d marked %s? [y/N]: ", len(picked), projectWord(len(picked)))
			input := a.Input
			if input == nil {
				input = os.Stdin
			}
			answer, err := bufio.NewReader(input).ReadString('\n')
			if err != nil && err != io.EOF {
				return o.failErr(err)
			}
			if yesNo(strings.TrimSpace(answer)) != "yes" {
				return o.fail(ExitFailure, "cancelled", "cancelled")
			}
		}
		outcomes := projscan.AddAll(ctx, picked, func(ctx context.Context, cand projscan.Candidate) (registry.Project, error) {
			return c.AddProject(ctx, cand.Path, cand.Name)
		})
		results := make([]scanResult, 0, len(outcomes))
		failed := 0
		for _, out := range outcomes {
			res := scanResult{Name: out.Name, Path: out.Path}
			if out.Err != nil {
				failed++
				res.Error = ipc.ErrorOf(out.Err)
			} else {
				res.Added, res.Project = true, &out.Project
			}
			results = append(results, res)
		}
		if o.json {
			o.printJSON(scanDoc(dir, found, results, skipped))
			if failed > 0 {
				return ExitFailure
			}
			return ExitOK
		}
		for _, res := range results {
			fmt.Fprintln(o.stdout, resultLine(res))
		}
		if failed > 0 {
			return o.fail(ExitFailure, "add_failed", fmt.Sprintf("%d of %d projects were not added", failed, len(results)))
		}
		return ExitOK
	})
}
