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

// A v12 project keeps the finish step as an implementer round, and its report
// needs no Outcome line (S384).
func TestFinishOnOlderContractResumesTheImplementer(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	before := len(h.runner.requests())
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateCommitted)
	reqs := h.runner.requests()
	if len(reqs) != before+1 {
		t.Fatalf("%d requests after approve, want %d", len(reqs), before+1)
	}
	fin := reqs[before]
	if fin.Role != RoleImplementer || fin.Resume != "sess-implementer" {
		t.Errorf("finish request: %+v", fin)
	}
	section := finishSection(t, fin.Brief)
	for _, want := range []string{`slicer note S04 --text "Describe the verified outcome" --render`, `slicer done S04 --note "Describe the verified outcome" --render --check`} {
		if !strings.Contains(section, want) {
			t.Errorf("finish brief lacks %q", want)
		}
	}
	if len(h.calls("slicer", "done")) != 0 || len(h.calls("slicer", "note")) != 0 {
		t.Errorf("ork ran the finish step on v12: %v", finishOrder(h))
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
	if err == nil || !strings.Contains(err.Error(), "ork accepts relay v12 or v13 and slicer prints v10") {
		t.Fatalf("v10: %v", err)
	}
	if runs, _ := h.store.List(); len(runs) != 0 {
		t.Fatal("a run record was created")
	}
}
