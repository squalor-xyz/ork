// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// waveApproveRig is a wave of runs at the approval card, started through
// wave.start with a role runner that passes every gate.
type waveApproveRig struct {
	d       *Daemon
	store   *runrec.Store
	p       registry.Project
	fake    *clitest.Fake
	wave    string
	items   []string          // in start order
	ids     map[string]string // item -> run id
	redLeft atomic.Int32      // checks of redItem that fail from now on
}

type waveApproveItem struct{ item, stopAfter string }

const noHumanChecks = `{"slice":{"sections":[{"heading":"Git","body":"One commit: \"S1: test\"."}]}}`

// newWaveApproveRig starts the items in the order given, with slice as every
// item's spec. extra routes go before the daemon test's own. A check of
// redItem's worktree fails while rig.redLeft is above zero.
func newWaveApproveRig(t *testing.T, slice, redItem string, items []waveApproveItem, extra func(project string) []clitest.Route) *waveApproveRig {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "p")
	var routes []clitest.Route
	if extra != nil {
		routes = extra(project)
	}
	routes = append(routes,
		clitest.Route{Tool: "slicer", Args: []string{"show"}, Prefix: true, Stdout: slice},
		clitest.Route{Tool: "slicer", Args: []string{"note"}, Prefix: true, Stdout: `{"id":"S1","added":true}`},
	)
	d, store, p := runDaemonIn(t, root, passRunner(), routes...)
	rig := &waveApproveRig{d: d, store: store, p: p, fake: lastFake, ids: map[string]string{}}
	var tick atomic.Int64
	store.Now = func() time.Time {
		return time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC).Add(time.Duration(tick.Add(1)) * time.Second)
	}
	d.runs.deps.Check = func(_ context.Context, dir, _ string, _ time.Duration) (engine.CheckOutput, error) {
		if filepath.Base(dir) == redItem && rig.redLeft.Add(-1) >= 0 {
			return engine.CheckOutput{Output: "FAIL TestGolden\n"}, nil
		}
		return engine.CheckOutput{OK: true}, nil
	}
	if err := os.MkdirAll(p.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two runs of one project start at once only under a higher cap.
	if err := registry.Save(d.cfg.RegistryPath, &registry.File{Version: registry.Version, GlobalCap: 8, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	var params []string
	for _, it := range items {
		rig.items = append(rig.items, it.item)
		extraField := ""
		if it.stopAfter != "" {
			extraField = fmt.Sprintf(`,"stop_after":%q`, it.stopAfter)
		}
		params = append(params, fmt.Sprintf(`{"project":"p","item":%q,"well_specified":true,"split":false%s}`, it.item, extraField))
	}
	var result WaveResult
	if err := json.Unmarshal(request(t, d, "wave.start", `{"items":[`+strings.Join(params, ",")+`]}`), &result); err != nil {
		t.Fatal(err)
	}
	rig.wave = result.Wave
	for _, it := range result.Items {
		if it.Run == nil {
			t.Fatalf("start of %s failed: %+v", it.Item, it.Error)
		}
		rig.ids[it.Item] = it.Run.ID
	}
	for _, id := range rig.ids {
		waitState(t, store, id, runrec.StateAwaitingApproval)
		waitIdle(t, d, id)
	}
	return rig
}

func plainItems(items ...string) []waveApproveItem {
	var out []waveApproveItem
	for _, item := range items {
		out = append(out, waveApproveItem{item: item})
	}
	return out
}

// mark ticks the first n checks of the item's run, and marks the rest later.
func (r *waveApproveRig) mark(t *testing.T, item string, ticked, total int) {
	t.Helper()
	for i := 1; i <= total; i++ {
		state := runrec.CheckLater
		if i <= ticked {
			state = runrec.CheckTicked
		}
		request(t, r.d, "run.check", fmt.Sprintf(`{"run":%q,"check":%d,"state":%q}`, r.ids[item], i, state))
	}
}

func (r *waveApproveRig) markAll(t *testing.T, total int) {
	t.Helper()
	for _, item := range r.items {
		r.mark(t, item, total, total)
	}
}

func (r *waveApproveRig) record(t *testing.T, item string) runrec.Record {
	t.Helper()
	run, err := r.store.Open(r.ids[item])
	if err != nil {
		t.Fatal(err)
	}
	return run.Snapshot()
}

func (r *waveApproveRig) approve(t *testing.T) WaveApproveResult {
	t.Helper()
	var got WaveApproveResult
	if err := json.Unmarshal(request(t, r.d, MethodWaveApprove, fmt.Sprintf(`{"wave":%q}`, r.wave)), &got); err != nil {
		t.Fatal(err)
	}
	for _, id := range r.ids {
		waitIdle(t, r.d, id)
	}
	return got
}

// slicerCalls lists the item ids of the slicer calls of one verb, in order.
func (r *waveApproveRig) slicerCalls(verb string) []string {
	var out []string
	for _, c := range r.fake.CallsTo("slicer") {
		if len(c.Args) > 1 && c.Args[0] == verb {
			out = append(out, c.Args[1])
		}
	}
	return out
}

func outcomes(res WaveApproveResult) []string {
	var out []string
	for _, it := range res.Items {
		out = append(out, it.Item+":"+it.Outcome)
	}
	return out
}

func TestWaveApproveApprovesEachWaitingRunInStartOrder(t *testing.T) {
	// The start order is not the item id order.
	order := []string{"S23", "S21", "S22"}
	rig := newWaveApproveRig(t, twoHumanChecks, "", plainItems(order...), nil)
	rig.markAll(t, 2)
	got := rig.approve(t)
	if got.Wave != rig.wave {
		t.Errorf("wave %q, want %q", got.Wave, rig.wave)
	}
	want := []string{"S23:landed", "S21:landed", "S22:landed"}
	if !reflect.DeepEqual(outcomes(got), want) {
		t.Fatalf("outcomes %q, want %q", outcomes(got), want)
	}
	if lands := rig.slicerCalls("land"); !reflect.DeepEqual(lands, order) {
		t.Errorf("slicer land calls %q, want %q", lands, order)
	}
	// Each approval came after the previous run rested again: committed and landed.
	var prev runrec.Record
	for i, item := range order {
		rec := rig.record(t, item)
		if rec.State != runrec.StateCommitted || rec.LandedAt.IsZero() {
			t.Fatalf("%s: state %s, landed %v", item, rec.State, rec.LandedAt)
		}
		if i > 0 && rec.Approvals[0].At.Before(prev.LandedAt) {
			t.Errorf("%s was approved at %v, before %s landed at %v", item, rec.Approvals[0].At, order[i-1], prev.LandedAt)
		}
		prev = rec
		if got.Items[i].Run != rig.ids[item] || got.Items[i].State != runrec.StateCommitted {
			t.Errorf("item %d: %+v", i, got.Items[i])
		}
	}
}

func TestWaveApproveStopsAtTheFirstRunThatDoesNotLand(t *testing.T) {
	rig := newWaveApproveRig(t, noHumanChecks, "S22", plainItems("S21", "S22", "S23"), func(project string) []clitest.Route {
		w := filepath.Join(project, ".worktrees", "S22")
		// The base moved for S22 only: it merges in, and the re-check is red.
		return []clitest.Route{
			{Tool: "git", Args: []string{"-C", project, "merge-base", "--is-ancestor", "S22", "main"}, Exit: 1},
			{Tool: "git", Args: []string{"-C", w, "merge-base", "--is-ancestor", "main", "HEAD"}, Exit: 1},
			{Tool: "git", Args: []string{"-C", w, "rev-parse", "-q", "--verify", "MERGE_HEAD"}, Exit: 1},
			{Tool: "slicer", Args: []string{"render"}, Prefix: true, Stdout: `{}`},
		}
	})
	rig.redLeft.Store(2) // the re-check and its rerun
	got := rig.approve(t)
	want := []string{"S21:landed", "S22:failed", "S23:not-attempted"}
	if !reflect.DeepEqual(outcomes(got), want) {
		t.Fatalf("outcomes %q, want %q", outcomes(got), want)
	}
	if got.Items[1].State != runrec.StateAwaitingApproval || got.Items[1].Error != nil {
		t.Errorf("the fix round item: %+v", got.Items[1])
	}
	if rec := rig.record(t, "S22"); rec.State != runrec.StateAwaitingApproval || rec.LandFixes != 1 {
		t.Errorf("S22: state %s, land fixes %d", rec.State, rec.LandFixes)
	}
	if rec := rig.record(t, "S21"); rec.State != runrec.StateCommitted || rec.LandedAt.IsZero() {
		t.Errorf("S21 did not stay landed: %+v", rec)
	}
	third := rig.record(t, "S23")
	if third.State != runrec.StateAwaitingApproval || len(third.Approvals) != 0 {
		t.Errorf("S23 was approved: state %s, approvals %+v", third.State, third.Approvals)
	}
	if lands := rig.slicerCalls("land"); !reflect.DeepEqual(lands, []string{"S21"}) {
		t.Errorf("slicer land calls %q, want only S21", lands)
	}
}

func TestWaveApproveStopsAtAnApproveError(t *testing.T) {
	rig := newWaveApproveRig(t, twoHumanChecks, "", plainItems("S21", "S22", "S23"), func(string) []clitest.Route {
		// The slicer note that records the human checks fails for S22.
		return []clitest.Route{{Tool: "slicer", Args: []string{"note", "S22"}, Prefix: true, Exit: 1, Stderr: "disk full"}}
	})
	rig.markAll(t, 2)
	got := rig.approve(t)
	want := []string{"S21:landed", "S22:failed", "S23:not-attempted"}
	if !reflect.DeepEqual(outcomes(got), want) {
		t.Fatalf("outcomes %q, want %q", outcomes(got), want)
	}
	if e := got.Items[1].Error; e == nil || !strings.Contains(e.Message, "record the human checks") {
		t.Errorf("the error is not on the second item: %+v", got.Items[1])
	}
	if got.Items[1].State != runrec.StateAwaitingApproval {
		t.Errorf("the failed item's state %q, want the run's own", got.Items[1].State)
	}
	if got.Items[0].Error != nil || got.Items[2].Error != nil {
		t.Errorf("errors on the other items: %+v", got.Items)
	}
	third := rig.record(t, "S23")
	if third.State != runrec.StateAwaitingApproval || len(third.Approvals) != 0 {
		t.Errorf("S23 was touched: state %s, approvals %+v", third.State, third.Approvals)
	}
	if notes := rig.slicerCalls("note"); !reflect.DeepEqual(notes, []string{"S21", "S22"}) {
		t.Errorf("slicer note calls %q", notes)
	}
}

func TestWaveApproveRefusesWhenAnyHumanCheckIsOpenAndApprovesNothing(t *testing.T) {
	rig := newWaveApproveRig(t, twoHumanChecks, "", plainItems("S21", "S22", "S23"), nil)
	rig.mark(t, "S22", 1, 1) // one of two checks marked: one stays open
	rig.mark(t, "S23", 2, 2)
	_, err := rig.d.handle(context.Background(), MethodWaveApprove, json.RawMessage(fmt.Sprintf(`{"wave":%q}`, rig.wave)))
	if err == nil || ipc.ErrorOf(err).Code != CodeRefused {
		t.Fatalf("error %v, want code %q", err, CodeRefused)
	}
	msg := err.Error()
	for _, want := range []string{
		rig.ids["S21"], "1. Read the card at 80x24.", "2. Press F and send a note.", // S21: both open
		rig.ids["S22"],
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal does not name %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, rig.ids["S23"]) {
		t.Errorf("refusal names S23, whose checks are all marked: %s", msg)
	}
	for _, item := range rig.items {
		if rec := rig.record(t, item); rec.State != runrec.StateAwaitingApproval || len(rec.Approvals) != 0 {
			t.Errorf("%s: state %s, approvals %+v", item, rec.State, rec.Approvals)
		}
	}
	if calls := rig.slicerCalls("land"); len(calls) != 0 {
		t.Errorf("slicer land calls %q after a refusal", calls)
	}
}

func TestWaveApproveNeverMarksOrSkipsAHumanCheck(t *testing.T) {
	rig := newWaveApproveRig(t, twoHumanChecks, "", plainItems("S21", "S22"), nil)
	rig.mark(t, "S21", 1, 2) // one ticked, one later
	before := map[string]runrec.Record{}
	for _, item := range rig.items {
		before[item] = rig.record(t, item)
	}
	// S22 has two open checks: the refusal must leave every mark and write alone.
	if _, err := rig.d.handle(context.Background(), MethodWaveApprove, json.RawMessage(fmt.Sprintf(`{"wave":%q}`, rig.wave))); err == nil {
		t.Fatal("approved a wave with open checks")
	}
	for _, item := range rig.items {
		after := rig.record(t, item)
		if !reflect.DeepEqual(after.Card.HumanChecks, before[item].Card.HumanChecks) || len(after.Writes) != len(before[item].Writes) {
			t.Errorf("%s: checks or writes changed by the refusal:\nbefore %+v\nafter  %+v", item, before[item].Card.HumanChecks, after.Card.HumanChecks)
		}
	}
	// With every check resolved the call lands the wave and keeps each mark.
	rig.mark(t, "S22", 2, 2)
	want := map[string][]runrec.HumanCheck{}
	for _, item := range rig.items {
		want[item] = rig.record(t, item).Card.HumanChecks
	}
	if got := rig.approve(t); !reflect.DeepEqual(outcomes(got), []string{"S21:landed", "S22:landed"}) {
		t.Fatalf("outcomes %q", outcomes(got))
	}
	for _, item := range rig.items {
		if got := rig.record(t, item).Card.HumanChecks; !reflect.DeepEqual(got, want[item]) {
			t.Errorf("%s: checks after the landing %+v, want %+v", item, got, want[item])
		}
	}
}

func TestWaveApproveLeavesRunsThatAreNotWaiting(t *testing.T) {
	rig := newWaveApproveRig(t, noHumanChecks, "", plainItems("S21", "S22"), nil)
	extras := []struct{ id, state string }{
		{"x-impl", runrec.StateImplement},
		{"x-queued", runrec.StateQueued},
		{"x-paused", runrec.StatePaused},
		{"x-done", runrec.StateCommitted},
	}
	for _, x := range extras {
		if _, err := rig.store.Create(runrec.Record{ID: x.id, Project: "p", Item: x.id, Wave: rig.wave, State: x.state}); err != nil {
			t.Fatal(err)
		}
	}
	got := rig.approve(t)
	want := []string{"S21:landed", "S22:landed", "x-impl:not-waiting", "x-queued:not-waiting", "x-paused:not-waiting", "x-done:done"}
	if !reflect.DeepEqual(outcomes(got), want) {
		t.Fatalf("outcomes %q, want %q", outcomes(got), want)
	}
	for i, x := range extras {
		item := got.Items[2+i]
		if item.State != x.state || item.Run != x.id {
			t.Errorf("%s listed as %+v, want state %s", x.id, item, x.state)
		}
		run, err := rig.store.Open(x.id)
		if err != nil {
			t.Fatal(err)
		}
		if rec := run.Snapshot(); rec.State != x.state || len(rec.Approvals) != 0 {
			t.Errorf("%s was changed: %+v", x.id, rec)
		}
	}
}

func TestWaveApproveWithNoWaitingMemberIsRefused(t *testing.T) {
	d, store, _ := runDaemon(t, nil)
	const wave = "wave-nothing-waits"
	for id, state := range map[string]string{"paused": runrec.StatePaused, "done": runrec.StateCommitted} {
		if _, err := store.Create(runrec.Record{ID: id, Project: "p", Item: id, Wave: wave, State: state}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := d.handle(context.Background(), MethodWaveApprove, json.RawMessage(`{"wave":"`+wave+`"}`))
	if err == nil || ipc.ErrorOf(err).Code != CodeRefused || !strings.Contains(err.Error(), "no run of "+wave+" awaits approval") {
		t.Fatalf("error %v", err)
	}
}

func TestWaveApproveUnknownWaveIsNotFound(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	_, err := d.handle(context.Background(), MethodWaveApprove, json.RawMessage(`{"wave":"wave-missing"}`))
	if err == nil || ipc.ErrorOf(err).Code != CodeNotFound {
		t.Fatalf("error %v, want code %q", err, CodeNotFound)
	}
}

func TestWaveApproveNeedsTheWavePrefix(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	for _, raw := range []string{`{}`, `{"wave":"ork-1"}`} {
		_, err := d.handle(context.Background(), MethodWaveApprove, json.RawMessage(raw))
		if err == nil || ipc.ErrorOf(err).Code != CodeBadParams {
			t.Errorf("%s: error %v, want code %q", raw, err, CodeBadParams)
		}
	}
}

func TestWaveApproveStopAfterCommitCountsAsDone(t *testing.T) {
	rig := newWaveApproveRig(t, noHumanChecks, "", []waveApproveItem{{item: "S21", stopAfter: runrec.StopAfterCommit}, {item: "S22"}}, nil)
	got := rig.approve(t)
	want := []string{"S21:committed", "S22:landed"}
	if !reflect.DeepEqual(outcomes(got), want) {
		t.Fatalf("outcomes %q, want %q", outcomes(got), want)
	}
	if rec := rig.record(t, "S21"); rec.State != runrec.StateCommitted || !rec.LandedAt.IsZero() {
		t.Errorf("S21: state %s, landed %v", rec.State, rec.LandedAt)
	}
	if lands := rig.slicerCalls("land"); !reflect.DeepEqual(lands, []string{"S22"}) {
		t.Errorf("slicer land calls %q, want only S22", lands)
	}
}

// landedCtx reports itself cancelled once the item's run has landed, which is
// after that run rested and before the next one is approved.
type landedCtx struct {
	context.Context
	landed func() bool
}

func (c landedCtx) Err() error {
	if c.landed() {
		return context.Canceled
	}
	return c.Context.Err()
}

func TestWaveApproveStopsWhenTheCallIsCancelledBetweenRuns(t *testing.T) {
	rig := newWaveApproveRig(t, noHumanChecks, "", plainItems("S21", "S22", "S23"), nil)
	ctx := landedCtx{Context: context.Background(), landed: func() bool {
		run, err := rig.store.Open(rig.ids["S21"])
		return err == nil && !run.Snapshot().LandedAt.IsZero()
	}}
	v, err := rig.d.approveWave(ctx, WaveAction{Wave: rig.wave})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range rig.ids {
		waitIdle(t, rig.d, id)
	}
	want := []string{"S21:landed", "S22:not-attempted", "S23:not-attempted"}
	if !reflect.DeepEqual(outcomes(*v), want) {
		t.Fatalf("outcomes %q, want %q", outcomes(*v), want)
	}
	if rec := rig.record(t, "S21"); rec.State != runrec.StateCommitted {
		t.Errorf("the in-flight approval did not finish: %s", rec.State)
	}
	for _, item := range []string{"S22", "S23"} {
		if rec := rig.record(t, item); rec.State != runrec.StateAwaitingApproval || len(rec.Approvals) != 0 {
			t.Errorf("%s: state %s, approvals %+v", item, rec.State, rec.Approvals)
		}
	}
}

func TestEngineRunRowsCarryTheWave(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	for id, wave := range map[string]string{"in-wave": "wave-20261116-090000-abcd", "alone": ""} {
		if _, err := store.Create(runrec.Record{ID: id, Item: id, Project: p.Name, Wave: wave, State: runrec.StateAwaitingApproval}); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range d.engineRuns(p.Name) {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"in-wave": "wave-20261116-090000-abcd", "alone": ""}[row.ID]
		if row.Wave != want {
			t.Errorf("%s: wave %q, want %q", row.ID, row.Wave, want)
		}
		if has := strings.Contains(string(raw), `"wave"`); has != (want != "") {
			t.Errorf("%s: wave in the JSON = %v: %s", row.ID, has, raw)
		}
	}
}
