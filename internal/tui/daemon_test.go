// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func requestServer(t *testing.T, handler ipc.Handler) (*Daemon, chan struct{}, chan struct{}, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "req")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	listener, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	var cancel context.CancelFunc
	var done chan error
	started, release := make(chan struct{}), make(chan struct{})
	server := &ipc.Server{Handler: func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method == daemon.MethodRefresh {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return handler(ctx, method, params)
	}}
	start := func() {
		ctx, stop := context.WithCancel(context.Background())
		cancel = stop
		stopped := make(chan error, 1)
		done = stopped
		current := listener
		go func() { stopped <- server.Serve(ctx, current) }()
	}
	start()
	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("request server did not stop")
		}
	}
	t.Cleanup(stop)
	restart := func() {
		stop()
		var err error
		listener, err = ipc.Listen(socket)
		if err != nil {
			t.Fatal(err)
		}
		start()
	}
	return &Daemon{Connect: func(ctx context.Context) (*daemon.Client, error) { return daemon.Dial(ctx, socket) }}, started, release, restart
}

func snapshotReply(_ context.Context, _ string, params json.RawMessage) (any, error) {
	var p daemon.SnapshotParams
	json.Unmarshal(params, &p)
	return daemon.SnapshotResult{Project: registry.Project{Name: p.Project}, Items: daemon.Part[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S1", Title: "fresh " + p.Project}}}}, nil
}

func TestDaemonSnapshotRedialsAfterDeadRequest(t *testing.T) {
	d, _, _, restart := requestServer(t, snapshotReply)
	// Restart the server between snapshots, closing every old request connection.
	first := d.Snapshot(context.Background(), proj("alpha", false))
	if first.Items.Err != nil {
		t.Fatalf("first snapshot: %v", first.Items.Err)
	}
	restart()
	s := d.Snapshot(context.Background(), proj("beta", false))
	if s.Items.Err != nil || len(s.Items.Value) != 1 || s.Items.Value[0].Title != "fresh beta" {
		t.Fatalf("snapshot after dead connection: %+v", s.Items)
	}
}

func TestSlowRefreshDoesNotDelayOtherProjectSnapshot(t *testing.T) {
	d, started, release, _ := requestServer(t, snapshotReply)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	refreshed := make(chan error, 1)
	go func() { refreshed <- d.Refresh(ctx, "alpha") }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("refresh did not start")
	}
	result := make(chan source.Snapshot, 1)
	go func() { result <- d.Snapshot(ctx, proj("beta", false)) }()
	select {
	case s := <-result:
		if s.Items.Err != nil || len(s.Items.Value) != 1 || s.Items.Value[0].Title != "fresh beta" {
			t.Fatalf("other project snapshot: %+v", s.Items)
		}
	case <-ctx.Done():
		close(release)
		t.Fatal("slow refresh delayed beta snapshot")
	}
	close(release)
	if err := <-refreshed; err != nil {
		t.Fatalf("refresh: %v", err)
	}
}

type runActions interface {
	Cleanup(context.Context, string) error
	Approve(context.Context, string) error
	Stop(context.Context, string) error
	Retry(context.Context, string) error
}

func TestDaemonTypedActions(t *testing.T) {
	if _, ok := any(&Daemon{}).(runActions); !ok {
		t.Fatal("daemon adapter lacks typed run actions")
	}
	for _, verb := range []string{"approve", "stop", "retry", "cleanup"} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/error-%t", verb, fail), func(t *testing.T) {
				d, _, _, _ := requestServer(t, func(_ context.Context, method string, params json.RawMessage) (any, error) {
					if method != "run."+verb || string(params) != `{"run":"engine"}` {
						return nil, fmt.Errorf("wrong request: %s %s", method, params)
					}
					if fail {
						return nil, &ipc.Error{Code: "action_failed", Message: "Exact action failure."}
					}
					return map[string]string{"id": "engine"}, nil
				})
				a := any(d).(runActions)
				call := map[string]func(context.Context, string) error{"approve": a.Approve, "stop": a.Stop, "retry": a.Retry, "cleanup": a.Cleanup}[verb]
				err := call(context.Background(), "engine")
				if fail {
					var e *ipc.Error
					if !errors.As(err, &e) || e.Code != "action_failed" || e.Message != "Exact action failure." {
						t.Fatalf("wire error changed: %v", err)
					}
				} else if err != nil {
					t.Fatalf("typed action: %v", err)
				}
			})
		}
	}
}

func TestDaemonActionConnectFailure(t *testing.T) {
	a, ok := any(&Daemon{Connect: func(context.Context) (*daemon.Client, error) { return nil, errors.New("connect failed") }}).(runActions)
	if !ok {
		t.Fatal("daemon adapter lacks typed run actions")
	}
	for name, call := range map[string]func(context.Context, string) error{"approve": a.Approve, "stop": a.Stop, "retry": a.Retry, "cleanup": a.Cleanup} {
		t.Run(name, func(t *testing.T) {
			if err := call(context.Background(), "engine"); err == nil || err.Error() != "connect failed" {
				t.Fatalf("connect error changed: %v", err)
			}
		})
	}
}

func TestDaemonTypedItemShow(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			d, _, _, _ := requestServer(t, func(_ context.Context, method string, params json.RawMessage) (any, error) {
				if method != daemon.MethodItemShow || string(params) != `{"project":"ork","id":"S12"}` {
					return nil, fmt.Errorf("wrong request: %s %s", method, params)
				}
				if fail {
					return nil, &ipc.Error{Code: "tool_failed", Message: "exact detail error"}
				}
				return daemon.ItemShowResult{Detail: &slicercli.Detail{Item: slicercli.Item{ID: "S12", Title: "Roadmap"}}}, nil
			})
			got, err := d.ItemShow(context.Background(), "ork", "S12")
			if fail {
				var wire *ipc.Error
				if !errors.As(err, &wire) || wire.Code != "tool_failed" || wire.Message != "exact detail error" {
					t.Fatalf("wire error: %v", err)
				}
			} else if err != nil || got == nil || got.ID != "S12" || got.Title != "Roadmap" {
				t.Fatalf("detail: %+v %v", got, err)
			}
		})
	}
}

func TestDaemonItemShowAndDepsConnectFailure(t *testing.T) {
	failed := errors.New("connect failed")
	d := &Daemon{Connect: func(context.Context) (*daemon.Client, error) { return nil, failed }}
	if _, err := d.ItemShow(context.Background(), "ork", "S12"); err != failed {
		t.Fatalf("detail error: %v", err)
	}
	if s := d.Snapshot(context.Background(), proj("ork", false)); s.Deps.Err != failed {
		t.Fatalf("deps error: %v", s.Deps.Err)
	}
}

func TestDaemonTypedWaves(t *testing.T) {
	for _, method := range []string{daemon.MethodWaveSuggest, daemon.MethodWaveOverlap, daemon.MethodWaveStart} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", method, fail), func(t *testing.T) {
				expected := &daemon.RunPlan{Item: "A", Branch: "a"}
				start := daemon.WaveStartParams{Items: []daemon.RunParams{{Project: "ork", Item: "A", WellSpecified: true, Expected: expected}}}
				d, _, _, _ := requestServer(t, func(_ context.Context, got string, raw json.RawMessage) (any, error) {
					if got != method {
						return nil, fmt.Errorf("method: %s", got)
					}
					var want any
					switch method {
					case daemon.MethodWaveSuggest:
						want = daemon.WaveSuggestParams{Project: "ork"}
					case daemon.MethodWaveOverlap:
						want = daemon.WaveOverlapParams{Project: "ork", Items: []string{"A", "B"}}
					default:
						want = start
					}
					data, _ := json.Marshal(want)
					if string(data) != string(raw) {
						return nil, fmt.Errorf("params: %s want %s", raw, data)
					}
					if fail {
						return nil, &ipc.Error{Code: "wave_failed", Message: "Exact WIRE failure."}
					}
					switch method {
					case daemon.MethodWaveSuggest:
						return slicercli.ParallelPlan{Width: 7}, nil
					case daemon.MethodWaveOverlap:
						return slicercli.Overlap{Overlap: []slicercli.OverlapPair{{A: "A", B: "B", Shared: []string{"x.go"}}}}, nil
					}
					return daemon.WaveResult{Wave: "wave-wire", Items: []daemon.WaveItemResult{{Item: "A", Error: &ipc.Error{Code: "item_failed", Message: "Exact ITEM failure."}}}}, nil
				})
				var err error
				switch method {
				case daemon.MethodWaveSuggest:
					var out *slicercli.ParallelPlan
					out, err = d.WaveSuggest(context.Background(), "ork")
					if !fail && (err != nil || out == nil || out.Width != 7) {
						t.Fatalf("suggest: %+v %v", out, err)
					}
				case daemon.MethodWaveOverlap:
					var out *slicercli.Overlap
					out, err = d.WaveOverlap(context.Background(), "ork", []string{"A", "B"})
					if !fail && (err != nil || out == nil || len(out.Overlap) != 1 || out.Overlap[0].Shared[0] != "x.go") {
						t.Fatalf("overlap: %+v %v", out, err)
					}
				case daemon.MethodWaveStart:
					var out *daemon.WaveResult
					out, err = d.WaveStart(context.Background(), start)
					if !fail && (err != nil || out == nil || out.Wave != "wave-wire" || len(out.Items) != 1 || out.Items[0].Error.Message != "Exact ITEM failure.") {
						t.Fatalf("start: %+v %v", out, err)
					}
				}
				if fail {
					var wire *ipc.Error
					if !errors.As(err, &wire) || wire.Code != "wave_failed" || wire.Message != "Exact WIRE failure." {
						t.Fatalf("error changed: %v", err)
					}
				}
			})
		}
	}
}
func TestDaemonWavesConnectFailure(t *testing.T) {
	failed := errors.New("connect failed")
	d := &Daemon{Connect: func(context.Context) (*daemon.Client, error) { return nil, failed }}
	if _, err := d.WaveSuggest(context.Background(), "ork"); err != failed {
		t.Fatal(err)
	}
	if _, err := d.WaveOverlap(context.Background(), "ork", []string{"A", "B"}); err != failed {
		t.Fatal(err)
	}
	if _, err := d.WaveStart(context.Background(), daemon.WaveStartParams{}); err != failed {
		t.Fatal(err)
	}
}
