// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
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
