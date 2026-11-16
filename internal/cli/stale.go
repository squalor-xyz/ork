// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/registry"
)

// Exclude only files the binary is not built from. Embedded Markdown below
// internal/ remains significant; the glob here matches root Markdown only.
var engineChangePathspec = []string{".", ":(exclude).slicer", ":(exclude)docs", ":(exclude,glob)*.md", ":(exclude,glob)**/*_test.go", ":(exclude)testdata", ":(exclude).github"}

func buildRevision(build string) (string, bool) {
	rev, suffix, found := strings.Cut(build, "+modified=")
	if found && suffix != "false" {
		return "", false
	}
	if len(rev) != 40 && len(rev) != 64 {
		return "", false
	}
	_, err := hex.DecodeString(rev)
	return rev, err == nil
}

func (a *App) staleBuild(ctx context.Context, build string) (bool, string) {
	own := a.buildID()
	if build == own && build != "unknown" {
		return false, ""
	}
	theirs, ok := buildRevision(build)
	if !ok {
		return true, ""
	}
	ours, ok := buildRevision(own)
	if !ok {
		return true, ""
	}
	path, err := registry.DefaultPath()
	if err != nil {
		return true, ""
	}
	file, err := registry.Load(path)
	if err != nil {
		return true, ""
	}
	bin := a.Tools.Git
	if bin == "" {
		bin = "git"
	}
	// Bound the entire search as well as each subprocess.
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	for _, p := range file.Projects {
		hasBoth := true
		for _, rev := range []string{theirs, ours} {
			if _, err := clirun.Run(ctx, clirun.Call{Bin: bin, Args: []string{"-C", p.Path, "cat-file", "-e", rev + "^{commit}"}, Timeout: versionTimeout}); err != nil {
				hasBoth = false
				break
			}
		}
		if !hasBoth {
			continue
		}
		args := append([]string{"-C", p.Path, "diff", "--quiet", theirs, ours, "--"}, engineChangePathspec...)
		_, err := clirun.Run(ctx, clirun.Call{Bin: bin, Args: args, Timeout: versionTimeout})
		if err == nil {
			return false, "the CLI build differs only in files the binary is not built from"
		}
		return true, ""
	}
	return true, ""
}
