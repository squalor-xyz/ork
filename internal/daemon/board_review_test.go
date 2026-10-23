// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
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
	return source.Result[[]slicercli.RelayRun]{Value: []slicercli.RelayRun{{ID: "recent", State: "ended"}}}
}

func TestStatusDoesNotReadBoardButSnapshotDoes(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0, registry.Project{Name: "p", Path: "/p"})
	reader := &countedBoardSource{stubSource: e.src}
	cfg := e.config()
	cfg.Source = reader
	d := New(cfg)
	defer d.cache.Close()
	if _, err := d.status(context.Background(), StatusParams{Project: "p"}); err != nil {
		t.Fatal(err)
	}
	if reader.boardReads != 0 {
		t.Fatalf("status made %d board reads", reader.boardReads)
	}
	s, err := d.snapshot(context.Background(), SnapshotParams{Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if reader.boardReads != 1 || len(s.BoardRuns.Value) != 1 || s.BoardRuns.Value[0].ID != "recent" {
		t.Fatalf("snapshot board reads=%d, payload=%+v", reader.boardReads, s.BoardRuns)
	}
}
