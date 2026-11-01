// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// parsePorcelainZ reads status v1 with -z, where rename and copy records
// carry the destination first, then a separate NUL-terminated source path.
func parsePorcelainZ(out string) []string {
	records := strings.Split(out, "\x00")
	var paths []string
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 || record[2] != ' ' || record[:2] == "!!" {
			continue
		}
		paths = append(paths, record[3:])
		if strings.ContainsAny(record[:2], "RC") && i+1 < len(records) {
			i++
			if records[i] != "" {
				paths = append(paths, records[i])
			}
		}
	}
	return paths
}

// changedPaths reads tracked changes and untracked files in the worktree.
func (e *Engine) changedPaths(ctx context.Context, worktree string) ([]string, error) {
	out, err := e.gitRead(ctx, worktree, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	return parsePorcelainZ(out), nil
}

// scopeSection is advisory: unavailable scope or status never blocks a gate.
func (e *Engine) scopeSection(ctx context.Context, rec runrec.Record) *BriefSection {
	raw, err := e.d.Slicer.ShowRaw(ctx, rec.Worktree, rec.Item)
	if err != nil {
		return nil
	}
	var detail slicercli.Detail
	if json.Unmarshal(raw, &detail) != nil || len(detail.Paths) == 0 {
		return nil
	}
	changed, err := e.changedPaths(ctx, rec.Worktree)
	if err != nil {
		return nil
	}
	warnings := ScopeWarnings(changed, detail.Paths)
	if len(warnings) == 0 {
		return nil
	}
	return &BriefSection{Title: "Out of scope files", Body: "- " + strings.Join(warnings, "\n- ") +
		"\n\nThis is a warning from the Files list (adj 119); the gate decides whether each file is needed, and this warning does not fail the gate by itself."}
}
