// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func TestItemShow(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	for _, tc := range []struct{ name, raw, code string }{
		{"ok", `{"project":"p","id":"S1"}`, ""},
		{"not_found", `{"project":"unknown","id":"S1"}`, CodeNotFound},
		{"bad_params", `{"project":"p","id":""}`, CodeBadParams},
		{"unknown_field", `{"project":"p","id":"S1","write":true}`, CodeBadParams},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := d.handle(context.Background(), "item.show", json.RawMessage(tc.raw))
			if tc.code != "" {
				var wire *ipc.Error
				if !errors.As(err, &wire) || wire.Code != tc.code {
					t.Fatalf("error=%v, want %s", err, tc.code)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(v)
			if !strings.Contains(string(b), `"slice"`) {
				t.Fatalf("detail missing: %s", b)
			}
			calls := lastFake.CallsTo("slicer")
			found := false
			for _, call := range calls {
				if strings.Join(call.Args, " ") == "show S1 --json --lean --root "+p.Path {
					found = true
				}
			}
			if !found {
				t.Fatalf("Show did not use registered root: %+v", calls)
			}
		})
	}
}

func TestSnapshotCarriesDeps(t *testing.T) {
	e := newEnv(t)
	p := registry.Project{Name: "p", Path: "/p"}
	e.writeRegistry(t, 0, p)
	e.src.snaps["p"] = source.Snapshot{Deps: source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S90"}}}}
	d := New(e.config())
	defer d.cache.Close()
	v, err := d.handle(context.Background(), "snapshot", json.RawMessage(`{"project":"p"}`))
	if err != nil {
		t.Fatal(err)
	}
	out := v.(*SnapshotResult)
	if len(out.Deps.Value) != 1 || out.Deps.Value[0].ID != "S90" || out.Deps.ReadAt == nil {
		t.Fatalf("deps lost: %+v", out.Deps)
	}
}

func TestDepsSnapshotErrorAndStatusBoundary(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0, registry.Project{Name: "p", Path: "/p"})
	e.src.snaps["p"] = source.Snapshot{Deps: source.Result[[]slicercli.Item]{Err: &ipc.Error{Code: "deps_failed", Message: "exact deps error"}}}
	d := New(e.config())
	defer d.cache.Close()
	out, err := d.snapshot(context.Background(), SnapshotParams{Project: "p"})
	if err != nil || out.Deps.Error == nil || out.Deps.Error.Code != "deps_failed" || out.Deps.Error.Message != "exact deps error" {
		t.Fatalf("snapshot: %+v %v", out, err)
	}
	status, err := d.status(context.Background(), StatusParams{Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), "deps") {
		t.Fatalf("deps leaked into status: %s", raw)
	}
}

func TestItemNoteWritesTheNoteAndReadsItemsAgain(t *testing.T) {
	d, _, p := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"note"}, Prefix: true, Stdout: `{"added":true}`})
	src := d.cfg.Source.(*stubSource)
	src.snaps = make(map[string]source.Snapshot)
	src.put("p", source.Snapshot{Items: source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S12", Notes: []string{"recorded"}}}}})
	v, err := d.handle(context.Background(), "item.note", json.RawMessage(`{"project":"p","item":"S12","text":"- text"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if string(raw) != `{"item":"S12"}` {
		t.Fatalf("result %s", raw)
	}
	calls := lastFake.CallsTo("slicer")
	want := []string{"note", "S12", "--text", "- text", "--render", "--json", "--root", p.Path}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("calls %v", calls)
	}
	if src.count("items p") != 1 {
		t.Fatal("items were not refreshed")
	}
	snap, err := d.snapshot(context.Background(), SnapshotParams{Project: "p"})
	if err != nil || len(snap.Items.Value) != 1 || !reflect.DeepEqual(snap.Items.Value[0].Notes, []string{"recorded"}) {
		t.Fatalf("snapshot %+v %v", snap, err)
	}
}
func TestItemNoteRefusals(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	for _, tc := range []struct{ raw, code string }{
		{`{"project":"p","item":"S12","text":"  "}`, CodeBadParams},
		{`{"project":"p","item":" ","text":"text"}`, CodeBadParams},
		{`{"project":"unknown","item":"S12","text":"text"}`, CodeNotFound},
		{`{"project":"p","item":"S12","text":"text","other":1}`, CodeBadParams},
	} {
		_, err := d.handle(context.Background(), "item.note", json.RawMessage(tc.raw))
		if e := ipc.ErrorOf(err); e == nil || e.Code != tc.code {
			t.Errorf("%s: %v", tc.raw, err)
		}
	}
	if calls := lastFake.CallsTo("slicer"); len(calls) != 0 {
		t.Fatalf("calls %v", calls)
	}
}
func TestItemNoteKeepsTheSlicerErrorCode(t *testing.T) {
	d, _, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"note"}, Prefix: true, Stdout: `{"error":{"code":"no_such_item","message":"missing"}}`, Exit: 2})
	_, err := d.handle(context.Background(), "item.note", json.RawMessage(`{"project":"p","item":"missing","text":"text"}`))
	if e := ipc.ErrorOf(err); e == nil || e.Code != "no_such_item" {
		t.Fatalf("error %v", err)
	}
	if d.cfg.Source.(*stubSource).count("items p") != 0 {
		t.Fatal("failed write refreshed")
	}
}

func TestItemNewFilesASliceAndReadsItemsAgain(t *testing.T) {
	t.Run("files", func(t *testing.T) {
		d, _, p := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"id":"S184"}`}, clitest.Route{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{}`})
		if err := os.MkdirAll(d.cfg.Paths.Dir, 0700); err != nil {
			t.Fatal(err)
		}
		// Warm the cache so its initial reads cannot mask a missing refresh.
		if err := d.cache.Refresh(context.Background(), p, source.Items, source.Deps); err != nil {
			t.Fatal(err)
		}
		src := d.cfg.Source.(*stubSource)
		src.mu.Lock()
		src.snaps = make(map[string]source.Snapshot)
		src.mu.Unlock()
		itemsBefore, depsBefore := src.count("items p"), src.count("deps p")
		src.put("p", source.Snapshot{Items: source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S184"}}}, Deps: source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S184"}}}})
		v, err := d.handle(context.Background(), "item.new", json.RawMessage(`{"project":"p","title":"Title","why":"why","size":"M"}`))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(v)
		if string(raw) != `{"id":"S184","existing":false}` {
			t.Fatalf("result %s", raw)
		}
		calls := lastFake.CallsTo("slicer")
		if len(calls) != 2 || calls[0].Args[0] != "add" || calls[1].Args[0] != "promote" {
			t.Fatalf("calls %v", calls)
		}
		for _, call := range calls {
			if call.Args[len(call.Args)-1] != p.Path {
				t.Fatalf("wrong root %v", call)
			}
		}
		if src.count("items p") != itemsBefore+1 || src.count("deps p") != depsBefore+1 {
			t.Fatalf("did not refresh Items and Deps")
		}
		snap, err := d.snapshot(context.Background(), SnapshotParams{Project: "p"})
		if err != nil || len(snap.Items.Value) != 1 || snap.Items.Value[0].ID != "S184" || len(snap.Deps.Value) != 1 || snap.Deps.Value[0].ID != "S184" {
			t.Fatalf("stale snapshot %+v %v", snap, err)
		}
	})
	for _, tc := range []struct{ name, raw, code string }{
		{"blank", `{"project":"p","title":" "}`, CodeBadParams},
		{"unknown project", `{"project":"unknown","title":"Title"}`, CodeNotFound},
		{"unknown field", `{"project":"p","title":"Title","other":true}`, CodeBadParams},
		{"invalid score", `{"project":"p","title":"Title","effort":4}`, CodeBadParams},
		{"slicer error", `{"project":"p","title":"Title"}`, "add_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"error":{"code":"add_failed","message":"failed"}}`, Exit: 2})
			_, err := d.handle(context.Background(), "item.new", json.RawMessage(tc.raw))
			if e := ipc.ErrorOf(err); e == nil || e.Code != tc.code {
				t.Fatalf("error %v want %s", err, tc.code)
			}
		})
	}
}

func TestItemParkAndUnparkCallSlicerAndRefreshItems(t *testing.T) {
	for _, verb := range []string{"park", "unpark"} {
		t.Run(verb, func(t *testing.T) {
			d, _, p := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{verb}, Prefix: true, Stdout: `{}`})
			if err := d.cache.Refresh(context.Background(), p, source.Items, source.Deps); err != nil {
				t.Fatal(err)
			}
			src := d.cfg.Source.(*stubSource)
			beforeI, beforeD := src.count("items p"), src.count("deps p")
			v, err := d.handle(context.Background(), "item."+verb, json.RawMessage(`{"project":"p","item":"S12"}`))
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(v)
			if string(b) != `{"item":"S12"}` {
				t.Fatalf("result %s", b)
			}
			calls := lastFake.CallsTo("slicer")
			want := []string{verb, "S12", "--render", "--json", "--root", p.Path}
			if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
				t.Fatalf("calls %v", calls)
			}
			if src.count("items p") != beforeI+1 || src.count("deps p") != beforeD+1 {
				t.Fatal("Items and Deps not refreshed")
			}
		})
	}
}
func TestItemParkRefusedWhileTheItemHasAnUnfinishedRun(t *testing.T) {
	for _, state := range []string{"setup", "implement", "paused", "awaiting-approval", "stopped", "committed"} {
		for _, verb := range []string{"park", "unpark"} {
			t.Run(state+"/"+verb, func(t *testing.T) {
				d, store, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{verb}, Prefix: true, Stdout: `{}`})
				if _, err := store.Create(runrec.Record{ID: "run-blocker", Project: "p", Item: "S12", State: state}); err != nil {
					t.Fatal(err)
				}
				_, err := d.handle(context.Background(), "item."+verb, json.RawMessage(`{"project":"p","item":"S12"}`))
				if verb == "park" && !runrec.IsFinal(state) {
					e := ipc.ErrorOf(err)
					if e == nil || e.Code != CodeRefused || !strings.Contains(e.Message, "run-blocker") {
						t.Fatalf("error %v", err)
					}
					if len(lastFake.CallsTo("slicer")) != 0 {
						t.Fatal("refused park wrote")
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	for _, rec := range []runrec.Record{{ID: "other-item", Project: "p", Item: "S13", State: "implement"}, {ID: "other-project", Project: "other", Item: "S12", State: "implement"}} {
		t.Run(rec.ID, func(t *testing.T) {
			d, store, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"park"}, Prefix: true, Stdout: `{}`})
			if _, err := store.Create(rec); err != nil {
				t.Fatal(err)
			}
			if _, err := d.handle(context.Background(), "item.park", json.RawMessage(`{"project":"p","item":"S12"}`)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestItemParkBadParams(t *testing.T) {
	for _, verb := range []string{"park", "unpark"} {
		t.Run(verb, func(t *testing.T) {
			d, _, _ := runDaemon(t, nil)
			for _, tc := range []struct{ raw, code string }{
				{`{"project":" ","item":"S12"}`, CodeBadParams},
				{`{"project":"p","item":" "}`, CodeBadParams},
				{`{"project":"unknown","item":"S12"}`, CodeNotFound},
				{`{"project":"p","item":"S12","extra":true}`, CodeBadParams},
			} {
				_, err := d.handle(context.Background(), "item."+verb, json.RawMessage(tc.raw))
				if e := ipc.ErrorOf(err); e == nil || e.Code != tc.code {
					t.Errorf("%s: %v", tc.raw, err)
				}
			}
			if len(lastFake.CallsTo("slicer")) != 0 {
				t.Fatal("invalid params wrote")
			}
		})
	}
}
func TestItemMoveRunsSlicerMove(t *testing.T) {
	for _, pos := range []string{"before", "after"} {
		t.Run(pos, func(t *testing.T) {
			d, _, p := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"move"}, Prefix: true, Stdout: `{}`}, clitest.Route{Tool: "slicer", Args: []string{"list"}, Prefix: true, Stdout: `[{"id":"S12"},{"id":"S13"}]`})
			if err := d.cache.Refresh(context.Background(), p, source.Items, source.Deps); err != nil {
				t.Fatal(err)
			}
			src := d.cfg.Source.(*stubSource)
			beforeI, beforeD := src.count("items p"), src.count("deps p")
			raw := fmt.Sprintf(`{"project":"p","item":"S12","other":"S13","position":%q}`, pos)
			if _, err := d.handle(context.Background(), "item.move", json.RawMessage(raw)); err != nil {
				t.Fatal(err)
			}
			calls := lastFake.CallsTo("slicer")
			want := []string{"move", "S12", "--" + pos, "S13", "--render", "--json", "--root", p.Path}
			if len(calls) != 3 || !reflect.DeepEqual(calls[1].Args, want) {
				t.Fatalf("calls %v", calls)
			}
			if src.count("items p") != beforeI+1 || src.count("deps p") != beforeD+1 {
				t.Fatal("Items and Deps not refreshed")
			}
		})
	}
	d, _, _ := runDaemon(t, nil)
	for _, raw := range []string{
		`{"project":"p","item":"S12","other":"S13","position":"to"}`,
		`{"project":"p","item":"S12","other":" ","position":"before"}`,
		`{"project":"p","item":" ","other":"S13","position":"before"}`,
		`{"project":" ","item":"S12","other":"S13","position":"before"}`,
		`{"project":"p","item":"S12","other":"S13","position":"before","extra":true}`,
	} {
		_, err := d.handle(context.Background(), "item.move", json.RawMessage(raw))
		if e := ipc.ErrorOf(err); e == nil || e.Code != CodeBadParams {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if len(lastFake.CallsTo("slicer")) != 0 {
		t.Fatal("invalid move called slicer")
	}
}
func TestItemMoveReportsWhetherTheListOrderChanged(t *testing.T) {
	for _, changed := range []bool{true, false} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			before := `[{"id":"S12"},{"id":"S13"}]`
			after := before
			if changed {
				after = `[{"id":"S13"},{"id":"S12"}]`
			}
			d, _, p := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"move"}, Prefix: true, Stdout: `{}`}, clitest.Route{Tool: "slicer", Args: []string{"list"}, Prefix: true, Seq: []clitest.Reply{{Stdout: before}, {Stdout: after}}})
			v, err := d.handle(context.Background(), "item.move", json.RawMessage(`{"project":"p","item":"S12","other":"S13","position":"after"}`))
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(v)
			want := fmt.Sprintf(`{"item":"S12","changed":%t}`, changed)
			if string(b) != want {
				t.Fatalf("result %s want %s", b, want)
			}
			calls := lastFake.CallsTo("slicer")
			list := []string{"list", "--json", "--lean", "--root", p.Path}
			if len(calls) != 3 || !reflect.DeepEqual(calls[0].Args, list) || !reflect.DeepEqual(calls[2].Args, list) {
				t.Fatalf("calls %v", calls)
			}
		})
	}
}
func TestItemEditKeepsSlicerErrorCodeAndMessage(t *testing.T) {
	for _, verb := range []string{"park", "unpark", "move"} {
		t.Run(verb, func(t *testing.T) {
			d, _, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{verb}, Prefix: true, Stdout: `{"error":{"code":"cross_kind","message":"exact slicer refusal"}}`, Exit: 2}, clitest.Route{Tool: "slicer", Args: []string{"list"}, Prefix: true, Stdout: `[]`})
			raw := `{"project":"p","item":"S12"}`
			if verb == "move" {
				raw = `{"project":"p","item":"S12","other":"S13","position":"after"}`
			}
			_, err := d.handle(context.Background(), "item."+verb, json.RawMessage(raw))
			if e := ipc.ErrorOf(err); e == nil || e.Code != "cross_kind" || e.Message != "exact slicer refusal" {
				t.Fatalf("error %v", err)
			}
			if d.cfg.Source.(*stubSource).count("items p") != 0 {
				t.Fatal("failed edit refreshed")
			}
		})
	}
}
