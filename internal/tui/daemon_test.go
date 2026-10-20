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
	Approve(context.Context, string) error
	Stop(context.Context, string) error
	Retry(context.Context, string) error
}

func TestDaemonTypedActions(t *testing.T) {
	if _, ok := any(&Daemon{}).(runActions); !ok {
		t.Fatal("daemon adapter lacks typed run actions")
	}
	for _, verb := range []string{"approve", "stop", "retry"} {
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
				call := map[string]func(context.Context, string) error{"approve": a.Approve, "stop": a.Stop, "retry": a.Retry}[verb]
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
	for name, call := range map[string]func(context.Context, string) error{"approve": a.Approve, "stop": a.Stop, "retry": a.Retry} {
		t.Run(name, func(t *testing.T) {
			if err := call(context.Background(), "engine"); err == nil || err.Error() != "connect failed" {
				t.Fatalf("connect error changed: %v", err)
			}
		})
	}
}
