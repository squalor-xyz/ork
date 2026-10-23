// SPDX-License-Identifier: GPL-3.0-or-later

package source

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
)

func TestSnapshotReadsBoundedBoardRunsWithoutChangingActiveRead(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	routes := append(slicerRoutes(), clitest.Route{Tool: "slicer", Args: []string{"relay-run", "list", "--json", "--lean", "--root", root}, Stdout: fmt.Sprintf(`{"runs":[{"id":"recent","state":"ended","updated_at":%q},{"id":"old","state":"ended","updated_at":%q},{"id":"stale","state":"stale","updated_at":%q},{"id":"active","state":"active"}]}`, now, old, old)})
	fake := clitest.Install(t, routes...)
	s := direct(fake).Snapshot(context.Background(), registry.Project{Path: root})
	if s.BoardRuns.Err != nil || len(s.BoardRuns.Value) != 2 || s.BoardRuns.Value[0].ID != "recent" || s.BoardRuns.Value[1].ID != "stale" {
		t.Fatalf("board %+v", s.BoardRuns)
	}
	if len(s.Runs.Value) != 3 {
		t.Fatalf("active read changed: %+v", s.Runs)
	}
}
