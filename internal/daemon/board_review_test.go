// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

type countedBoardSource struct {
	*stubSource
	boardReads int
}

func (s *countedBoardSource) BoardRuns(ctx context.Context, p registry.Project) source.Result[[]slicercli.RelayRun] {
	s.boardReads++
	return source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{{ID: "stale", State: "stale"}, {ID: "recent", State: "ended"}}}
}

func TestStatusAndSnapshotReadBoard(t *testing.T) {
	dir := t.TempDir()
	e := &env{dir: dir, paths: PathsIn(dir), regPath: filepath.Join(dir, "projects.json"), clock: &fakeClock{}, src: &stubSource{snaps: map[string]source.Snapshot{}}}
	e.writeRegistry(t, 0, registry.Project{Name: "p", Path: "/p"})
	reader := &countedBoardSource{stubSource: e.src}
	cfg := e.config()
	cfg.Source = reader
	d := New(cfg)
	defer d.cache.Close()
	status, err := d.status(context.Background(), StatusParams{Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if got := status.Projects[0].ActiveRelays; len(got) != 1 || got[0].ID != "stale" || got[0].State != "stale" {
		t.Fatalf("status stale relays: %+v", got)
	}
	s, err := d.snapshot(context.Background(), SnapshotParams{Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if reader.boardReads != 2 || len(s.BoardRuns.Value) != 2 || s.BoardRuns.Value[1].ID != "recent" {
		t.Fatalf("snapshot board reads=%d, payload=%+v", reader.boardReads, s.BoardRuns)
	}
}
