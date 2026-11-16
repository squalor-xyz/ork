// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// sliverRoutes makes slicer call a sliver a sliver: recommend sets the flag,
// show names the parent, and list --parent returns the parent's slivers in order.
func sliverRoutes(parent string, ids ...string) []clitest.Route {
	roles := `{"sliver":true,"roles":[{"role":"implementer","row":{"agent":"Claude Code","model":"haiku","reasoning":"xhigh"}},{"role":"reviewer","row":{"agent":"Claude Code","model":"opus","reasoning":"medium"}},{"role":"architect","row":null,"alternatives":[]}]}`
	var routes []clitest.Route
	var items []string
	for _, id := range ids {
		routes = append(routes,
			clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend", id}, Prefix: true, Stdout: roles},
			clitest.Route{Tool: "slicer", Args: []string{"show", id}, Prefix: true, Stdout: `{"id":"` + id + `","parent":"` + parent + `","title":"t","slice":{"sections":[{"heading":"Git","body":"One commit."}]}}`})
		items = append(items, `{"id":"`+id+`","parent":"`+parent+`"}`)
	}
	routes = append(routes, clitest.Route{Tool: "slicer", Args: []string{"list", "--parent", parent}, Prefix: true, Stdout: "[" + strings.Join(items, ",") + "]"})
	return routes
}

// A sliver with a parent plans and records the parent, whether it is started
// alone or as one of the parent's chain.
func TestSliverPlansWithItsParentLink(t *testing.T) {
	d, _, _ := runDaemon(t, nil, sliverRoutes("S1", "S1a", "S1b")...)
	lone := string(request(t, d, MethodRunPlan, `{"project":"p","item":"S1a"}`))
	if !strings.Contains(lone, `"parent":"S1"`) {
		t.Fatalf("a sliver plan does not record its parent: %s", lone)
	}
	chain := string(request(t, d, MethodRunPlan, `{"project":"p","item":"S1","slivers":true}`))
	for _, want := range []string{`"parent":"S1"`, `"item":"S1a"`, `"item":"S1b"`} {
		if !strings.Contains(chain, want) {
			t.Fatalf("a sliver chain plan does not record %s: %s", want, chain)
		}
	}
	if strings.Index(chain, `"item":"S1a"`) > strings.Index(chain, `"item":"S1b"`) {
		t.Fatalf("slivers are not in slicer's order: %s", chain)
	}
}

// A sliver run uses the parent's worktree and branch (record 124).
func TestSliverRunsInTheParentWorktree(t *testing.T) {
	w := newWaveRigRoutes(t, 10, 0, sliverRoutes("S1", "S1a")...)
	raw := request(t, w.d, MethodRunStart, `{"project":"p","item":"S1a"}`)
	var rec runrec.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(w.p.Path, ".worktrees", "S1")
	if rec.Worktree != want || rec.Branch != "S1" {
		t.Fatalf("sliver worktree %q branch %q, want parent worktree %q branch S1", rec.Worktree, rec.Branch, want)
	}
}

// A second sliver of one parent does not start until the first run ends.
func TestSecondSliverWaitsForTheFirst(t *testing.T) {
	w := newWaveRigRoutes(t, 10, 0, sliverRoutes("S1", "S1a", "S1b")...)
	first := w.start(t, "S1a")
	select {
	case <-w.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first sliver did not start")
	}
	second := w.start(t, "S1b")
	if second.State != runrec.StateQueued {
		t.Fatalf("second sliver of S1 started (%s) before the first run ended", second.State)
	}
	w.expectNoStart(t)
	if _, err := w.d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: first.ID}); err != nil {
		t.Fatal(err)
	}
	waitState(t, w.store, first.ID, runrec.StateStopped)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := w.store.Open(second.ID)
		if err == nil && run.Snapshot().State != runrec.StateQueued {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("second sliver stayed queued after the first run ended")
}

// With the project cap at 1, the chain's first sliver runs and the second waits.
func TestProjectCapHoldsSliverRuns(t *testing.T) {
	w := newWaveRigRoutes(t, 10, 1, sliverRoutes("S1", "S1a", "S1b")...)
	if _, err := w.d.handle(context.Background(), MethodRunStart, json.RawMessage(`{"project":"p","item":"S1","slivers":true}`)); err != nil {
		t.Fatal(err)
	}
	runs, err := w.store.List()
	if err != nil {
		t.Fatal(err)
	}
	var first, second runrec.Record
	for _, run := range runs {
		rec := run.Snapshot()
		switch rec.Item {
		case "S1a":
			first = rec
		case "S1b":
			second = rec
		}
	}
	if first.ID == "" || second.ID == "" || first.State == runrec.StateQueued || second.State != runrec.StateQueued {
		t.Fatalf("project cap 1 queues the second sliver and runs the first: first %s %q, second %s %q", first.Item, first.State, second.Item, second.State)
	}
	fb, _ := json.Marshal(first)
	sb, _ := json.Marshal(second)
	if !strings.Contains(string(fb), `"chain":`) || !strings.Contains(string(sb), `"chain":"`+firstChain(fb)+`"`) {
		t.Fatalf("the two slivers are not one chain: %s %s", fb, sb)
	}
}

// firstChain is the chain id in a run record's JSON, or "".
func firstChain(raw []byte) string {
	var rec struct {
		Chain string `json:"chain"`
	}
	json.Unmarshal(raw, &rec)
	return rec.Chain
}

// chainRecord is one sliver of chain-1 in the parent's worktree.
func chainRecord(w *waveRig, id, item string, index int, state string) runrec.Record {
	return runrec.Record{ID: id, Project: "p", Item: item, Parent: "S1", Chain: "chain-1", SliverIndex: index, SliverCount: 2, State: state,
		Wave: "wave-x", Worktree: filepath.Join(w.p.Path, ".worktrees", "S1"), Branch: "S1",
		Lineup: map[string][]runrec.LineupEntry{engine.RoleImplementer: {{Vendor: "claude", Model: "haiku"}}, engine.RoleReviewer: {{Vendor: "claude", Model: "opus"}}}}
}

// A stopped sliver keeps the chain's worktree and branch for a retry. After a
// restart the later sliver of its chain, and any other run on that worktree,
// stay queued.
func TestStoppedChainStaysQueuedAfterARestart(t *testing.T) {
	w := newWaveRigRoutes(t, 10, 0, sliverRoutes("S1", "S1a", "S1b", "S1x")...)
	base := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	for i, rec := range []runrec.Record{chainRecord(w, "run-a", "S1a", 1, runrec.StateStopped), chainRecord(w, "run-b", "S1b", 2, runrec.StateQueued), {ID: "run-x", Project: "p", Item: "S1x", Parent: "S1", State: runrec.StateQueued, Wave: "wave-x",
		Worktree: filepath.Join(w.p.Path, ".worktrees", "S1"), Branch: "S1",
		Lineup: map[string][]runrec.LineupEntry{engine.RoleImplementer: {{Vendor: "claude", Model: "haiku"}}, engine.RoleReviewer: {{Vendor: "claude", Model: "opus"}}}}} {
		w.store.Now = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		if _, err := w.store.Create(rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.expectNoStart(t)
	for _, id := range []string{"run-b", "run-x"} {
		if rec := w.record(t, id); rec.State != runrec.StateQueued {
			t.Fatalf("%s is %s after the restart, want queued behind the stopped sliver", id, rec.State)
		}
	}
}

// A chain between two slivers keeps its one slot: the first is committed, the
// second waits, and a run of another item does not take the slot in the gap.
func TestChainKeepsItsSlotBetweenSlivers(t *testing.T) {
	w := newWaveRigRoutes(t, 1, 0, sliverRoutes("S1", "S1a", "S1b")...)
	for _, rec := range []runrec.Record{chainRecord(w, "run-a", "S1a", 1, runrec.StateCommitted), chainRecord(w, "run-b", "S1b", 2, runrec.StateQueued)} {
		run, err := w.store.Create(rec)
		if err != nil {
			t.Fatal(err)
		}
		w.d.runs.runs[rec.ID] = &managedRun{engine: engine.Load(w.d.runDeps(w.p), w.p, run)}
	}
	other := w.start(t, "S9")
	if other.State != runrec.StateQueued {
		t.Fatalf("another run took the slot between two slivers of a chain: %s", other.State)
	}
}

// The gap between two slivers keeps its slot across a restart: the committed
// first sliver and the queued second are recovered, and an older run of
// another item does not take the only slot.
func TestChainKeepsItsSlotAcrossARestart(t *testing.T) {
	w := newWaveRigRoutes(t, 1, 0, sliverRoutes("S1", "S1a", "S1b")...)
	base := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	other := runrec.Record{ID: "run-o", Project: "p", Item: "S9", State: runrec.StateQueued, Wave: "wave-x",
		Worktree: filepath.Join(w.p.Path, ".worktrees", "S9"), Branch: "S9",
		Lineup: map[string][]runrec.LineupEntry{engine.RoleImplementer: {{Vendor: "claude", Model: "haiku"}}, engine.RoleReviewer: {{Vendor: "claude", Model: "opus"}}}}
	for i, rec := range []runrec.Record{other, chainRecord(w, "run-a", "S1a", 1, runrec.StateCommitted), chainRecord(w, "run-b", "S1b", 2, runrec.StateQueued)} {
		w.store.Now = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		if _, err := w.store.Create(rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The chain's next sliver takes the slot it holds; the older run waits.
	w.expectStart(t, "S1")
	if rec := w.record(t, "run-o"); rec.State != runrec.StateQueued {
		t.Fatalf("an older run took the chain's slot across the restart: %s", rec.State)
	}
}

// A sliver occupies a global-cap slot. Cap 1 leaves the next run queued.
func TestGlobalCapCountsSliverRuns(t *testing.T) {
	w := newWaveRigRoutes(t, 1, 0, sliverRoutes("S1", "S1a")...)
	raw := request(t, w.d, MethodRunStart, `{"project":"p","item":"S1a"}`)
	other := w.start(t, "S9")
	if !strings.Contains(string(raw), `"parent":"S1"`) || other.State != runrec.StateQueued {
		t.Fatalf("global cap 1 did not count the sliver: record %s, next run %s", raw, other.State)
	}
}
