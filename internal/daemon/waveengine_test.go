// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// planFrom decodes a plan-parallel answer, so the tests build plans the way
// slicer sends them.
func planFrom(t *testing.T, raw string) *slicercli.ParallelPlan {
	t.Helper()
	var plan slicercli.ParallelPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatal(err)
	}
	return &plan
}

// waveIDs lists the item ids of each wave.
func waveIDs(plan *slicercli.ParallelPlan) [][]string {
	var out [][]string
	for _, w := range plan.Waves {
		ids := []string{}
		for _, it := range w.Items {
			ids = append(ids, it.ID)
		}
		out = append(out, ids)
	}
	return out
}

// waveOf returns the index of the wave that holds id, or -1.
func waveOf(plan *slicercli.ParallelPlan, id string) int {
	for i, w := range plan.Waves {
		for _, it := range w.Items {
			if it.ID == id {
				return i
			}
		}
	}
	return -1
}

func reasonOf(plan *slicercli.ParallelPlan, id string) string {
	for _, w := range plan.Waves {
		for _, it := range w.Items {
			if it.ID == id {
				return it.Reason
			}
		}
	}
	return "<missing>"
}

func TestApplyEngineRuleLeavesAPlanWithOneEngineSlicePerWaveUnchanged(t *testing.T) {
	plan := planFrom(t, `{"width":2,"waves":[{"wave":1,"items":[{"id":"E1","title":"e1"},{"id":"N1","title":"n1"}]},{"wave":2,"items":[{"id":"E2","title":"e2","reason":"shares: E1 internal/engine/a.go"},{"id":"N2","title":"n2","reason":"after: N1"}]},{"wave":3,"items":[{"id":"N3","title":"n3"}]}],"in_flight":[],"unplaced":[],"blocked":[],"held":[],"unspecified":[]}`)
	got := ApplyEngineRule(plan, map[string]bool{"E1": true, "E2": true})
	if !reflect.DeepEqual(got, plan) {
		t.Fatalf("plan changed:\n got %+v\nwant %+v", got, plan)
	}
	if got == plan {
		t.Fatal("ApplyEngineRule returned the plan it was given, not a copy")
	}
}

func TestApplyEngineRuleKeepsTheFirstEngineSliceOfAWaveAndDefersTheRest(t *testing.T) {
	plan := planFrom(t, `{"waves":[{"wave":1,"items":[{"id":"E1","title":"e1"},{"id":"E2","title":"e2"},{"id":"N1","title":"n1"},{"id":"E3","title":"e3"}]},{"wave":2,"items":[{"id":"N2","title":"n2"}]}]}`)
	got := ApplyEngineRule(plan, map[string]bool{"E1": true, "E2": true, "E3": true})
	want := [][]string{{"E1", "N1"}, {"E2"}, {"E3"}, {"N2"}}
	if !reflect.DeepEqual(waveIDs(got), want) {
		t.Fatalf("waves %v, want %v", waveIDs(got), want)
	}
	// The input is slicer's answer; it is not edited in place.
	if in := [][]string{{"E1", "E2", "N1", "E3"}, {"N2"}}; !reflect.DeepEqual(waveIDs(plan), in) {
		t.Fatalf("input changed to %v", waveIDs(plan))
	}
}

func TestApplyEngineRuleRenumbersWaves(t *testing.T) {
	plan := planFrom(t, `{"waves":[{"wave":1,"items":[{"id":"E1","title":"e1"},{"id":"E2","title":"e2"}]},{"wave":2,"items":[{"id":"N1","title":"n1"}]},{"wave":3,"items":[{"id":"N2","title":"n2"}]}]}`)
	got := ApplyEngineRule(plan, map[string]bool{"E1": true, "E2": true})
	var numbers []int
	for _, w := range got.Waves {
		numbers = append(numbers, w.Wave)
	}
	if want := []int{1, 2, 3, 4}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("wave numbers %v, want %v", numbers, want)
	}
	// Slicer's own numbering may start late or skip; the result still runs 1..n.
	gapped := planFrom(t, `{"waves":[{"wave":2,"items":[{"id":"N1","title":"n1"}]},{"wave":5,"items":[{"id":"N2","title":"n2"}]}]}`)
	numbers = nil
	for _, w := range ApplyEngineRule(gapped, nil).Waves {
		numbers = append(numbers, w.Wave)
	}
	if want := []int{1, 2}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("gapped wave numbers %v, want %v", numbers, want)
	}
}

func TestApplyEngineRuleDeferredItemsGetTheEngineReason(t *testing.T) {
	plan := planFrom(t, `{"waves":[{"wave":1,"items":[{"id":"E1","title":"e1","reason":"r-e1"},{"id":"E2","title":"e2","reason":"shares: E1 internal/engine/a.go"},{"id":"N1","title":"n1","reason":"r-n1"},{"id":"E3","title":"e3"}]}]}`)
	got := ApplyEngineRule(plan, map[string]bool{"E1": true, "E2": true, "E3": true})
	for id, want := range map[string]string{"E1": "r-e1", "N1": "r-n1", "E2": "engine: E1", "E3": "engine: E1"} {
		if r := reasonOf(got, id); r != want {
			t.Errorf("reason of %s: %q, want %q", id, r, want)
		}
	}
	if len(got.Waves) < 2 || got.Waves[1].Items[0].Title != "e2" {
		t.Errorf("deferred item lost its title: %v", waveIDs(got))
	}
}

func TestApplyEngineRuleKeepsDependencyOrder(t *testing.T) {
	plan := planFrom(t, `{"waves":[{"wave":1,"items":[{"id":"E1","title":"e1"},{"id":"E2","title":"e2"}]},{"wave":2,"items":[{"id":"N1","title":"n1","reason":"after: E2"}]}]}`)
	got := ApplyEngineRule(plan, map[string]bool{"E1": true, "E2": true})
	if e1, e2 := waveOf(got, "E1"), waveOf(got, "E2"); e1 < 0 || e2 != e1+1 {
		t.Fatalf("E2 is in wave index %d, E1 in %d: %v", e2, e1, waveIDs(got))
	}
	if e2, n1 := waveOf(got, "E2"), waveOf(got, "N1"); e2 < 0 || n1 <= e2 {
		t.Fatalf("N1 is in wave index %d, E2 in %d: %v", n1, e2, waveIDs(got))
	}
	if r := reasonOf(got, "N1"); r != "after: E2" {
		t.Fatalf("N1 reason %q", r)
	}
}

func TestApplyEngineRuleDoesNotTouchUnplacedBlockedHeldOrInFlight(t *testing.T) {
	plan := planFrom(t, `{"width":3,"waves":[{"wave":1,"items":[{"id":"E1","title":"e1"},{"id":"N1","title":"n1"}]}],"in_flight":[{"id":"E9","title":"running"}],"unplaced":[{"id":"E2","title":"e2","reason":"no_files"}],"blocked":[{"id":"E3","waiting_on":["E1"]}],"held":[{"id":"E4","title":"e4","flags":["needs-owner"]}],"unspecified":[{"id":"E5","missing":["Implement"]}]}`)
	got := ApplyEngineRule(plan, map[string]bool{"E1": true, "E2": true, "E3": true, "E4": true, "E5": true, "E9": true})
	// E9 runs already; it is not a member of the wave and takes no slot in it.
	if want := [][]string{{"E1", "N1"}}; !reflect.DeepEqual(waveIDs(got), want) {
		t.Fatalf("waves %v, want %v", waveIDs(got), want)
	}
	if !reflect.DeepEqual(got, plan) {
		t.Fatalf("plan changed:\n got %+v\nwant %+v", got, plan)
	}
	// The lists are copies: editing the result leaves slicer's answer alone.
	got.Unplaced[0].Title = "edited"
	got.Blocked[0].ID = "edited"
	got.Held[0].Title = "edited"
	got.Unspecified[0].ID = "edited"
	got.InFlight[0] = json.RawMessage(`{}`)
	if plan.Unplaced[0].Title != "e2" || plan.Blocked[0].ID != "E3" || plan.Held[0].Title != "e4" || plan.Unspecified[0].ID != "E5" || string(plan.InFlight[0]) == `{}` {
		t.Fatalf("the result shares slices with the input: %+v", plan)
	}
}

func TestApplyEngineRuleIsStable(t *testing.T) {
	plan := planFrom(t, `{"width":4,"waves":[{"wave":1,"items":[{"id":"E1","title":"e1"},{"id":"E2","title":"e2"},{"id":"N1","title":"n1"},{"id":"E3","title":"e3"}]},{"wave":2,"items":[{"id":"E4","title":"e4"},{"id":"E5","title":"e5"}]}],"unplaced":[{"id":"U","title":"u","reason":"no_files"}]}`)
	engine := map[string]bool{"E1": true, "E2": true, "E3": true, "E4": true, "E5": true}
	once := ApplyEngineRule(plan, engine)
	twice := ApplyEngineRule(once, engine)
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("second pass changed the plan:\n once %v\ntwice %v", waveIDs(once), waveIDs(twice))
	}
	if len(once.Waves) != 5 {
		t.Fatalf("first pass: %v", waveIDs(once))
	}
}

// fakeShower answers Show from maps and records what it was asked.
type fakeShower struct {
	mu          sync.Mutex
	calls       []string
	roots       []string
	details     map[string]*slicercli.Detail
	errs        map[string]error
	inFlight    int
	maxInFlight int
	barrier     int           // when set, a read waits until this many are in flight
	reached     chan struct{} // closed when the barrier is reached
	delay       func(id string) time.Duration
}

func newFakeShower(engine ...string) *fakeShower {
	f := &fakeShower{details: map[string]*slicercli.Detail{}, errs: map[string]error{}, reached: make(chan struct{})}
	for _, id := range engine {
		f.details[id] = engineDetail("internal/engine/" + id + ".go")
	}
	return f
}

// engineDetail is a slice whose Files section names path.
func engineDetail(path string) *slicercli.Detail {
	return &slicercli.Detail{Slice: &slicercli.Spec{Sections: []slicercli.Section{{Heading: "Files", Body: "- `" + path + "`"}}}}
}

func (f *fakeShower) Show(ctx context.Context, root, id string) (*slicercli.Detail, error) {
	f.mu.Lock()
	f.calls = append(f.calls, id)
	f.roots = append(f.roots, root)
	f.inFlight++
	f.maxInFlight = max(f.maxInFlight, f.inFlight)
	if f.barrier > 0 && f.inFlight >= f.barrier {
		select {
		case <-f.reached:
		default:
			close(f.reached)
		}
	}
	f.mu.Unlock()
	if f.barrier > 0 {
		select {
		case <-f.reached:
		case <-time.After(5 * time.Second):
		}
	}
	if f.delay != nil {
		time.Sleep(f.delay(id))
	}
	f.mu.Lock()
	f.inFlight--
	err, detail := f.errs[id], f.details[id]
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if detail == nil {
		detail = &slicercli.Detail{Slice: &slicercli.Spec{Sections: []slicercli.Section{{Heading: "Files", Body: "- `docs/x.md`"}}}}
	}
	return detail, nil
}

func (f *fakeShower) sortedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.calls)
	sort.Strings(out)
	return out
}

func TestSuggestWithEngineRuleReadsEachPlacedItemOnce(t *testing.T) {
	plan := planFrom(t, `{"waves":[{"wave":1,"items":[{"id":"E1","title":"e1"},{"id":"E2","title":"e2"}]},{"wave":2,"items":[{"id":"N1","title":"n1"}]}],"unplaced":[{"id":"U1","title":"u","reason":"no_files"}],"blocked":[{"id":"B1","waiting_on":["E1"]}],"held":[{"id":"H1","title":"h","flags":["needs-owner"]}],"unspecified":[{"id":"X1","missing":["Check"]}]}`)
	shower := newFakeShower("E1", "E2")
	got := SuggestWithEngineRule(context.Background(), shower, "/p/root", plan)
	if want := []string{"E1", "E2", "N1"}; !reflect.DeepEqual(shower.sortedCalls(), want) {
		t.Fatalf("show calls %v, want %v", shower.sortedCalls(), want)
	}
	for _, root := range shower.roots {
		if root != "/p/root" {
			t.Fatalf("root %q", root)
		}
	}
	if want := [][]string{{"E1"}, {"E2"}, {"N1"}}; !reflect.DeepEqual(waveIDs(got), want) {
		t.Fatalf("waves %v, want %v", waveIDs(got), want)
	}
}

func TestSuggestWithEngineRuleSkipsReadsWhenEveryWaveHoldsOneItem(t *testing.T) {
	plan := planFrom(t, `{"waves":[{"wave":1,"items":[{"id":"E1","title":"e1"}]},{"wave":2,"items":[{"id":"E2","title":"e2"}]}]}`)
	shower := newFakeShower("E1", "E2")
	got := SuggestWithEngineRule(context.Background(), shower, "/p/root", plan)
	if len(shower.calls) != 0 {
		t.Fatalf("show calls %v, want none", shower.calls)
	}
	if !reflect.DeepEqual(got, plan) {
		t.Fatalf("plan changed: %v", waveIDs(got))
	}
}

func TestSuggestWithEngineRuleReadsInParallelButKeepsOrder(t *testing.T) {
	// Twelve items in one wave, E1 to E6 and N1 to N6 alternating. Eight reads
	// must be in flight at once before any returns; no more than eight ever.
	// The reads finish in reverse order of the items.
	items := ""
	shower := newFakeShower()
	order := []string{}
	for i := 1; i <= 6; i++ {
		for _, id := range []string{fmt.Sprintf("E%d", i), fmt.Sprintf("N%d", i)} {
			if items != "" {
				items += ","
			}
			items += fmt.Sprintf(`{"id":%q,"title":%q}`, id, id)
			order = append(order, id)
		}
		shower.details[fmt.Sprintf("E%d", i)] = engineDetail("cmd/ork/main.go")
	}
	shower.barrier = 8
	shower.delay = func(id string) time.Duration {
		return time.Duration(len(order)-slices.Index(order, id)) * 3 * time.Millisecond
	}
	plan := planFrom(t, `{"waves":[{"wave":1,"items":[`+items+`]}]}`)
	got := SuggestWithEngineRule(context.Background(), shower, "/p/root", plan)
	if shower.maxInFlight != 8 {
		t.Fatalf("most reads in flight %d, want 8", shower.maxInFlight)
	}
	want := [][]string{{"E1", "N1", "N2", "N3", "N4", "N5", "N6"}, {"E2"}, {"E3"}, {"E4"}, {"E5"}, {"E6"}}
	if !reflect.DeepEqual(waveIDs(got), want) {
		t.Fatalf("waves %v, want %v", waveIDs(got), want)
	}
}

func TestSuggestWithEngineRuleFailedReadLeavesThatItemAlone(t *testing.T) {
	plan := planFrom(t, `{"waves":[{"wave":1,"items":[{"id":"E1","title":"e1"},{"id":"E3","title":"e3"},{"id":"E2","title":"e2"}]}]}`)
	shower := newFakeShower("E1", "E2", "E3")
	shower.errs["E3"] = errors.New("slicer: show failed")
	got := SuggestWithEngineRule(context.Background(), shower, "/p/root", plan)
	if got == nil {
		t.Fatal("no plan")
	}
	// E3 could not be read, so it counts as a plain item and stays with E1.
	if want := [][]string{{"E1", "E3"}, {"E2"}}; !reflect.DeepEqual(waveIDs(got), want) {
		t.Fatalf("waves %v, want %v", waveIDs(got), want)
	}
}

func TestWaveSuggestRequestDefersEngineSlices(t *testing.T) {
	planJSON := `{"width":3,"waves":[{"wave":1,"items":[{"id":"S1","title":"one"},{"id":"S2","title":"two","reason":"shares: S1 x"},{"id":"S3","title":"three"}]},{"wave":2,"items":[{"id":"S4","title":"four","reason":"after: S2"}]}],"in_flight":[],"unplaced":[],"blocked":[],"held":[],"unspecified":[]}`
	routes := []clitest.Route{{Tool: "slicer", Args: []string{"plan-parallel", "--json", "--lean", "--root", waveRoot}, Stdout: planJSON}}
	for id, path := range map[string]string{"S1": "internal/engine/a.go", "S2": "internal/daemon/b.go", "S3": "docs/c.md", "S4": "docs/d.md"} {
		raw, err := json.Marshal(engineDetail(path))
		if err != nil {
			t.Fatal(err)
		}
		routes = append(routes, clitest.Route{Tool: "slicer", Args: []string{"show", id, "--json", "--lean", "--root", waveRoot}, Stdout: string(raw)})
	}
	d := readDaemon(t, routes...)
	var got slicercli.ParallelPlan
	if err := json.Unmarshal(request(t, d, "wave.suggest", `{"project":"p"}`), &got); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"S1", "S3"}, {"S2"}, {"S4"}}; !reflect.DeepEqual(waveIDs(&got), want) {
		t.Fatalf("waves %v, want %v", waveIDs(&got), want)
	}
	if r := reasonOf(&got, "S2"); r != "engine: S1" {
		t.Fatalf("S2 reason %q", r)
	}
	if got.Width != 3 || got.Waves[2].Wave != 3 {
		t.Fatalf("plan %+v", got)
	}
}

func TestWaveSuggestWithNoEngineSlicesIsByteIdenticalToSlicer(t *testing.T) {
	routes := []clitest.Route{
		{Tool: "slicer", Args: []string{"plan-parallel", "--json", "--lean", "--root", waveRoot}, File: recordedSlicer("plan-parallel.json")},
		{Tool: "slicer", Args: []string{"show"}, Prefix: true, Stdout: `{"id":"X","title":"t","slice":{"id":"X","title":"t","sections":[{"heading":"Files","body":"- ` + "`docs/x.md`" + `"}]}}`},
	}
	d := readDaemon(t, routes...)
	got := request(t, d, "wave.suggest", `{"project":"p"}`)
	raw, err := os.ReadFile(recordedSlicer("plan-parallel.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want slicercli.ParallelPlan
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(&want)
	if err != nil {
		t.Fatal(err)
	}
	var compact []byte
	compact, err = json.Marshal(json.RawMessage(got))
	if err != nil {
		t.Fatal(err)
	}
	if string(compact) != string(wantJSON) {
		t.Fatalf("suggest differs from slicer's plan:\n got %s\nwant %s", compact, wantJSON)
	}
	// The reads happened: this plan has waves with several items.
	reads := 0
	for _, c := range lastFake.CallsTo("slicer") {
		if len(c.Args) > 0 && c.Args[0] == "show" {
			reads++
		}
	}
	if reads == 0 {
		t.Fatal("the plan was not checked against the engine rule")
	}
}
