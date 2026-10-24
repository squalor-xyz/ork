// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestEngineCountsStreamEventsPerThirtySeconds(t *testing.T) {
	h := newHarness(t)
	now := time.Date(2026, 10, 24, 12, 0, 29, 0, time.UTC)
	h.deps.Now = func() time.Time { return now }
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.runner.script = func(req Request, n int) (Result, error) {
		now = time.Date(2026, 10, 24, 12, 0, 29, 0, time.UTC).Add(time.Duration(n) * 10 * time.Minute)
		if req.OnEvent == nil {
			t.Fatal("missing event hook")
		}
		if got := e.Activity(now); !reflect.DeepEqual(got, make([]int, 10)) {
			t.Fatalf("round did not start empty: %v", got)
		}
		req.OnEvent()
		req.OnEvent()
		now = now.Add(time.Second)
		req.OnEvent()
		want := []int{0, 0, 0, 0, 0, 0, 0, 0, 2, 1}
		if got := e.Activity(now); !reflect.DeepEqual(got, want) {
			t.Fatalf("buckets %v, want %v", got, want)
		}
		now = now.Add(270 * time.Second)
		want = []int{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}
		if got := e.Activity(now); !reflect.DeepEqual(got, want) {
			t.Fatalf("expiry %v", got)
		}
		now = now.Add(30 * time.Second)
		if got := e.Activity(now); !reflect.DeepEqual(got, make([]int, 10)) {
			t.Fatalf("stalled ring %v", got)
		}
		// The bucket from five minutes ago shares this slot; its count must reset.
		req.OnEvent()
		want = []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
		if got := e.Activity(now); !reflect.DeepEqual(got, want) {
			t.Fatalf("reused slot %v, want %v", got, want)
		}
		return Result{}, nil
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := e.Activity(now); got != nil {
		t.Fatalf("activity after role end: %v", got)
	}
}

func TestSparklineIsNotWrittenToRunJSON(t *testing.T) {
	h := newHarness(t)
	var e *Engine
	var err error
	e, err = New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.runner.script = func(req Request, n int) (Result, error) {
		path := filepath.Join(e.run.Dir(), "run.json")
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		req.OnEvent()
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("event changed run.json")
		}
		return Result{}, nil
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}
