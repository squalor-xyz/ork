// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

const BehindNotice = "daemon behind main: restart with ork daemon restart"

// projectBuild compares the running revision with the project's local main.
// A failed read is not evidence that this is ork's repository or is behind.
func (d *Daemon) projectBuild(ctx context.Context, p registry.Project) (selfHost, behind bool) {
	revision, _, _ := strings.Cut(d.cfg.BuildID, "+modified=")
	if len(revision) != 40 && len(revision) != 64 {
		return false, false
	}
	if _, err := hex.DecodeString(revision); err != nil {
		return false, false
	}
	if _, err := d.git(ctx, p, "cat-file", "-e", revision+"^{commit}"); err != nil {
		return false, false
	}
	head, err := d.git(ctx, p, "rev-parse", "--verify", "refs/heads/main^{commit}")
	if err != nil {
		return true, false
	}
	main := strings.TrimSpace(string(head.Stdout))
	if main == revision {
		return true, false
	}
	_, err = d.git(ctx, p, "merge-base", "--is-ancestor", revision, main)
	return true, err == nil
}

// EngineSlice reports a Files path under the code that runs a relay.
func EngineSlice(detail *slicercli.Detail) bool {
	if detail == nil || detail.Slice == nil {
		return false
	}
	for _, section := range detail.Slice.Sections {
		if !strings.EqualFold(section.Heading, "Files") {
			continue
		}
		// The project Files convention puts each path inside backticks.
		parts := strings.Split(section.Body, "`")
		for i := 1; i < len(parts); i += 2 {
			file, _, _ := strings.Cut(parts[i], ":")
			file = path.Clean(file)
			for _, dir := range []string{"internal/engine", "internal/daemon", "cmd"} {
				if file == dir || strings.HasPrefix(file, dir+"/") {
					return true
				}
			}
		}
	}
	return false
}

// EngineConflict names the two slices that cannot share a wave.
func EngineConflict(a, b string) string {
	return fmt.Sprintf("engine conflict: %s and %s touch internal/engine, internal/daemon or cmd; launch separately", a, b)
}

// BehindWarning is available only at the daemon's idle point.
func BehindWarning(status *StatusResult) string {
	if status == nil || status.RolesRunning {
		return ""
	}
	for _, p := range status.Projects {
		if p.SelfHost && p.BehindMain {
			return BehindNotice
		}
	}
	return ""
}

// StaleBuild reports a differing or unknown daemon build.
func StaleBuild(own, theirs string) bool {
	if own == "" {
		own = "dev"
	}
	return theirs == "" || theirs == "unknown" || theirs != own
}
