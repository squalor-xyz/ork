// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

const BehindNotice = "daemon behind main: restart with ork daemon restart"

type buildEntry struct {
	selfHost, behind bool
}

// projectBuild compares the running revision with the main checkout's branch.
// A failed read is not evidence that this is ork's repository or is behind.
func (d *Daemon) projectBuild(ctx context.Context, p registry.Project) (selfHost, behind bool) {
	revision, _, _ := strings.Cut(d.cfg.BuildID, "+modified=")
	if len(revision) != 40 && len(revision) != 64 {
		return false, false
	}
	if _, err := hex.DecodeString(revision); err != nil {
		return false, false
	}
	d.buildMu.Lock()
	defer d.buildMu.Unlock()
	// Keep only registered paths, even after projects are removed or relocated.
	registered := false
	f, regErr := registry.Load(d.registryPath())
	if regErr == nil {
		paths := make(map[string]bool, len(f.Projects))
		for _, project := range f.Projects {
			paths[project.Path] = true
		}
		registered = paths[p.Path]
		for key := range d.builds {
			path, _, _ := strings.Cut(key, "\x00")
			if !paths[path] {
				delete(d.builds, key)
			}
		}
	}
	head, readErr := d.git(ctx, p, "rev-parse", "HEAD", "--symbolic-full-name", "HEAD")
	parts := strings.Fields(string(head.Stdout))
	valid := readErr == nil && len(parts) == 2 && strings.HasPrefix(parts[1], "refs/heads/")
	key := ""
	if valid {
		key = strings.Join([]string{p.Path, d.cfg.BuildID, parts[0], parts[1]}, "\x00")
		if entry, ok := d.builds[key]; ok {
			return entry.selfHost, entry.behind
		}
	}
	// A changed key, detached checkout or failed read invalidates the old entry.
	for old := range d.builds {
		if strings.HasPrefix(old, p.Path+"\x00") {
			delete(d.builds, old)
		}
	}
	if _, err := d.git(ctx, p, "cat-file", "-e", revision+"^{commit}"); err != nil {
		return false, false
	}
	if !valid {
		return true, false
	}
	behind = false
	if parts[0] != revision {
		result, err := d.git(ctx, p, "merge-base", "--is-ancestor", revision, parts[0])
		if err != nil && result.ExitCode != 1 {
			return true, false
		}
		behind = err == nil
	}
	if registered {
		if d.builds == nil {
			d.builds = make(map[string]buildEntry)
		}
		d.builds[key] = buildEntry{selfHost: true, behind: behind}
	}
	return true, behind
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

// BlockedMessage names blocked items and their blockers in the supplied order.
func BlockedMessage(ids []string, deps []slicercli.ItemDeps, titles map[string]string) string {
	members := make(map[string]bool, len(ids))
	for _, id := range ids {
		members[id] = true
	}
	var messages, blockedIDs, blockerIDs []string
	seenBlockers := map[string]bool{}
	titled := map[string]bool{}
	inWave := false
	for _, item := range deps {
		var blockers []string
		for _, blocker := range item.BlockedBy {
			reason := "not done"
			if blocker != item.ID && members[blocker] {
				reason = "in this wave, " + reason
				inWave = true
			}
			name := blocker
			// A blocker shared by several items shows its title the first time only.
			if title := strings.Join(strings.Fields(runrec.PlainText(titles[blocker])), " "); title != "" && !titled[blocker] {
				titled[blocker] = true
				name += ` "` + title + `"`
			}
			blockers = append(blockers, name+" ("+reason+")")
			if !seenBlockers[blocker] {
				seenBlockers[blocker] = true
				blockerIDs = append(blockerIDs, blocker)
			}
		}
		if len(blockers) > 0 {
			messages = append(messages, item.ID+" waits on "+strings.Join(blockers, ", "))
			blockedIDs = append(blockedIDs, item.ID)
		}
	}
	if len(messages) == 0 {
		return ""
	}
	next := "land " + joinAnd(blockerIDs) + " first"
	if inWave {
		next = "untick " + joinAnd(blockedIDs) + ", or " + next
	}
	return strings.Join(append(messages, next), "; ")
}

// joinAnd writes names as "A", "A and B" or "A, B and C".
func joinAnd(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
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
