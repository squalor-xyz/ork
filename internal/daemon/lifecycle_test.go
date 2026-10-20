// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/source"
)

type forbiddenBuildFetch struct{ t *testing.T }

func (s forbiddenBuildFetch) Fetch(context.Context, registry.Project, source.Part) (any, error) {
	s.t.Error("build probe fetched project data")
	return nil, nil
}

func TestDaemonInfoDoesNotReadProjects(t *testing.T) {
	for _, brokenRegistry := range []bool{false, true} {
		t.Run(fmt.Sprint(brokenRegistry), func(t *testing.T) {
			e := newEnv(t)
			e.writeRegistry(t, 0, registry.Project{Name: "p", Path: e.dir, Adj: true})
			if brokenRegistry {
				if err := os.WriteFile(e.regPath, []byte("invalid JSON"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := e.config()
			cfg.BuildID, cfg.Source = "installed", forbiddenBuildFetch{t}
			d := New(cfg)
			defer d.cache.Close()
			v, err := d.handle(context.Background(), "daemon.info", nil)
			if err != nil {
				t.Fatalf("project-free build probe: %v", err)
			}
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != `{"build_id":"installed"}` {
				t.Fatalf("daemon.info reply: %s", b)
			}
		})
	}
}

func TestStatusCarriesBuildID(t *testing.T) {
	for _, build := range []string{"", "installed"} {
		t.Run(build, func(t *testing.T) {
			e := newEnv(t)
			e.writeRegistry(t, 0)
			cfg := e.config()
			cfg.BuildID = build
			d := New(cfg)
			defer d.cache.Close()
			v, err := d.handle(context.Background(), MethodStatus, nil)
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			var status struct {
				BuildID string `json:"build_id"`
			}
			if err := json.Unmarshal(b, &status); err != nil {
				t.Fatal(err)
			}
			want := build
			if want == "" {
				want = "dev"
			}
			if status.BuildID != want {
				t.Fatalf("status build_id = %q, want %s", status.BuildID, want)
			}
		})
	}
}

func TestDaemonForceStopCancelsRole(t *testing.T) {
	started, ended := make(chan struct{}), make(chan struct{})
	var startOnce, endOnce sync.Once
	d, _, _ := runDaemon(t, func(ctx context.Context, _ engine.Request) (engine.Result, error) {
		startOnce.Do(func() { close(started) })
		<-ctx.Done()
		endOnce.Do(func() { close(ended) })
		return engine.Result{}, ctx.Err()
	})
	request(t, d, "run.start", `{"project":"p","item":"S1"}`)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("role did not start")
	}
	b := request(t, d, "status", `{}`)
	if !strings.Contains(string(b), `"roles_running":true`) {
		t.Errorf("role activity missing: %s", b)
	}
	_, err := d.handle(context.Background(), "daemon.stop", json.RawMessage(`{"force":true}`))
	if err != nil {
		t.Fatalf("force stop: %v", err)
	}
	select {
	case <-ended:
	default:
		t.Fatal("force stop returned with a live role")
	}
	_, err = d.handle(context.Background(), "run.start", json.RawMessage(`{"project":"p","item":"S2"}`))
	if err == nil {
		t.Fatal("shutdown admitted a new run")
	}
}

func TestDaemonStopRefusesBusyUnlessForced(t *testing.T) {
	e := newEnv(t)
	d := New(e.config())
	defer d.cache.Close()
	d.runs.runs["busy"] = &managedRun{busy: true}
	_, err := d.handle(context.Background(), "daemon.stop", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("busy stop: %v; want refusal naming --force", err)
	}
}

func TestDaemonStopIdleReply(t *testing.T) {
	e := newEnv(t)
	d := New(e.config())
	defer d.cache.Close()
	v, err := d.handle(context.Background(), "daemon.stop", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("idle stop: %v", err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"stopped":true}` {
		t.Fatalf("idle stop reply: %s", b)
	}
}
