// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestCommitStopsAtCommittedWhenStopAfterIsCommit(t *testing.T) {
	env := newLandEnv(t, func(p *registry.Project) { p.Push = true })
	if err := env.run.Update(func(r *runrec.Record) { r.StopAfter = "commit" }); err != nil {
		t.Fatal(err)
	}
	if err := env.e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := env.e.Snapshot()
	if rec.State != runrec.StateCommitted || !rec.LandedAt.IsZero() || rec.Pending != nil || rec.Commit == nil || !rec.CleanedAt.IsZero() {
		t.Fatalf("record: %+v", rec)
	}
	disk, err := env.store.Open(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if disk.Snapshot().StopAfter != "commit" {
		t.Fatal("stop point not persisted")
	}
	if _, err := os.Stat(env.W); err != nil {
		t.Fatal(err)
	}
	if gitRun(t, env.P, "rev-parse", "main") != env.baseTip {
		t.Fatal("base changed")
	}
	gitRun(t, env.P, "rev-parse", rec.Branch)
	for file, forbidden := range map[string][]string{"git.log": {" merge ", " push", "worktree remove", "branch -d"}, "slicer.log": {"land "}} {
		raw, err := os.ReadFile(filepath.Join(env.fakes, file))
		if err != nil {
			t.Fatal(err)
		}
		for _, word := range forbidden {
			if strings.Contains(string(raw), word) {
				t.Fatalf("unexpected %q in %s", word, raw)
			}
		}
	}
	raw, err := os.ReadFile(filepath.Join(env.fakes, "slicer.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "relay-run end run-1 --outcome pass") {
		t.Fatalf("ledger not ended pass: %s", raw)
	}
}

func TestCommitGoesToLandByDefault(t *testing.T) {
	for _, stop := range []string{"", "land"} {
		t.Run(stop, func(t *testing.T) {
			env := newLandEnv(t)
			if err := env.run.Update(func(r *runrec.Record) { r.StopAfter = stop; r.State = runrec.StateCommit }); err != nil {
				t.Fatal(err)
			}
			if err := env.e.commit(context.Background()); err != nil {
				t.Fatal(err)
			}
			if rec := env.e.Snapshot(); rec.State != runrec.StateLand {
				t.Fatalf("state %s", rec.State)
			}
			if err := env.e.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if rec := env.e.Snapshot(); rec.State != runrec.StateCommitted || rec.LandedAt.IsZero() {
				t.Fatalf("did not land: %+v", rec)
			}
		})
	}
}

func TestNewRecordsStopAfter(t *testing.T) {
	h := newHarness(t)
	h.cfg.StopAfter = "commit"
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if e.Snapshot().StopAfter != "commit" {
		t.Fatal("config stop point lost")
	}
}
