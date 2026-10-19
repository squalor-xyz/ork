// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
	"github.com/squalor-xyz/ork/internal/tui"
)

// countingSource answers the items part with its current items and counts the
// reads of every part, keyed "part project".
type countingSource struct {
	mu    sync.Mutex
	items []slicercli.Item
	reads map[string]int
}

func (s *countingSource) Fetch(_ context.Context, p registry.Project, part source.Part) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads[string(part)+" "+p.Name]++
	switch part {
	case source.Items:
		return s.items, nil
	case source.Head:
		return "h1", nil
	}
	return nil, nil
}

func (s *countingSource) set(items []slicercli.Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = items
}

func (s *countingSource) readCount(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads[key]
}

// The TUI's Live, as the program builds it over the in-process daemon: Focus
// makes the daemon read the project every 5 s, Next delivers the notice of the
// change, and Refresh reads the project again now.
func TestTheProgramsLiveFocusesNoticesAndRefreshes(t *testing.T) {
	dir := sandbox(t)
	writeRegistry(t, dir, registry.Project{Name: "mine", Path: filepath.Join(dir, "mine")})
	src := &countingSource{items: []slicercli.Item{{ID: "S1", Title: "one"}}, reads: map[string]int{}}

	var errOut bytes.Buffer
	code := run(context.Background(), nil, &bytes.Buffer{}, &errOut, inTest(t, src, func(cfg tui.Config) error {
		ctx := context.Background()
		if cfg.Live == nil {
			t.Fatal("the TUI has no Live")
		}
		// The first read gives the cache the project's items; the change is
		// then the next read's value.
		cfg.Source.Snapshot(ctx, cfg.Projects[0])
		src.set([]slicercli.Item{{ID: "S1", Title: "one"}, {ID: "S2", Title: "two"}})

		if err := cfg.Live.Focus(ctx, []string{"mine"}); err != nil {
			t.Fatalf("Focus: %v", err)
		}
		nctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		name, err := cfg.Live.Next(nctx)
		if err != nil {
			t.Fatalf("Next after a change in focus: %v", err)
		}
		if name != "mine" {
			t.Fatalf("Next named %q, want %q", name, "mine")
		}

		before := src.readCount("items mine")
		if err := cfg.Live.Refresh(ctx, "mine"); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		if after := src.readCount("items mine"); after <= before {
			t.Errorf("items read %d times before Refresh and %d after; Refresh must read the project again", before, after)
		}
		return nil
	}))
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
}
