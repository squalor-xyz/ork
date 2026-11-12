// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// The harness fake slicer prints the recorded v12 contract by default.
func TestEngineStartWithV12CreatesARun(t *testing.T) {
	h := newHarness(t)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.Snapshot(); rec.Contract != 12 || rec.State != runrec.StateSetup {
		t.Fatalf("record: %+v", rec)
	}
}

func TestEngineStartRefusesV10NamingTheAcceptedSet(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cli", "slicer", "relay-v12.json"))
			if err != nil {
				t.Fatal(err)
			}
			return []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: strings.Replace(string(raw), `"contract": "relay v12"`, `"contract": "relay v10"`, 1)}}
		}
	})
	_, err := New(context.Background(), h.deps, h.cfg)
	if err == nil || !strings.Contains(err.Error(), "ork accepts relay v11 or v12 and slicer prints v10") {
		t.Fatalf("v10: %v", err)
	}
	if runs, _ := h.store.List(); len(runs) != 0 {
		t.Fatal("a run record was created")
	}
}
