// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTailKeepsTheLastBytesOfVisibleText(t *testing.T) {
	e := &Engine{}
	_, end := e.startActivity()
	live := e.liveHook()
	live(Live{Text: strings.Repeat("界", 900) + "\nlast"})
	got := e.Live()
	if !got.TailTruncated || got.Text != strings.Repeat("界", 681)+"\nlast" || len(got.Text) > 2048 || !utf8.ValidString(got.Text) {
		t.Fatalf("tail %d bytes: %q", len(got.Text), got.Text)
	}
	live(Live{Text: "!"})
	if got := e.Live().Text; got != strings.Repeat("界", 680)+"\nlast!" || !utf8.ValidString(got) {
		t.Fatalf("rune boundary: %q", got)
	}
	_, end2 := e.startActivity()
	if e.Live() != (Live{}) {
		t.Fatal("new round retained live data")
	}
	live(Live{Text: "old", Tokens: 99})
	end()
	if e.Live() != (Live{}) {
		t.Fatal("stale hook changed new round")
	}
	e.liveHook()(Live{Text: "new", Tokens: 4})
	if e.Live() != (Live{Text: "new", Tokens: 4, UsageKnown: true}) {
		t.Fatal(e.Live())
	}
	end2()
	if e.Live() != (Live{}) {
		t.Fatal("ended round retained live data")
	}
}

func TestLiveTokensAndCostAccumulate(t *testing.T) {
	h := newHarness(t)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.runner.script = func(req Request, n int) (Result, error) {
		req.OnLive(Live{Tokens: 12})
		req.OnLive(Live{Tokens: 7})
		if e.Live().Tokens != 19 {
			t.Fatal(e.Live())
		}
		res := defaultResult(req)
		res.Tokens = 25
		return res, nil
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, r := range e.Snapshot().Roles {
		if r.Tokens != 25 || r.CostUSD != 0.5 {
			t.Fatalf("final result lost: %+v", r)
		}
	}
}

func TestTailNeverReachesRunJSON(t *testing.T) {
	h := newHarness(t)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "private-live-tail-marker"
	h.runner.script = func(req Request, n int) (Result, error) {
		req.OnLive(Live{Text: marker, Tokens: 9})
		return Result{}, nil
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(e.run.Dir(), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if bytes.Contains(b, []byte(marker)) {
			t.Errorf("tail persisted in %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestZeroUsageEventSurvivesLiveSnapshot(t *testing.T) {
	e := &Engine{}
	_, end := e.startActivity()
	hook := e.liveHook()
	hook(Live{Text: "assistant"})
	if e.Live().UsageKnown {
		t.Fatal("text implies usage")
	}
	hook(Live{UsageKnown: true})
	if !e.Live().UsageKnown || e.Live().Tokens != 0 {
		t.Fatal("zero usage lost")
	}
	end()
	if e.Live().UsageKnown {
		t.Fatal("usage presence survived round end")
	}
}

func TestParallelLiveTextSeparatedInRoleOrder(t *testing.T) {
	e := &Engine{}
	// Start in reverse name order and include an active role with no text.
	_, endUI := e.startActivity(RoleUI)
	defer endUI()
	e.liveHook(RoleUI)(Live{Text: "ui", Tokens: 5})
	_, endReviewer := e.startActivity(RoleReviewer)
	defer endReviewer()
	e.liveHook(RoleReviewer)(Live{Text: "reviewer", Tokens: 3})
	_, endEmpty := e.startActivity(RoleImplementer)
	defer endEmpty()
	if got := e.Live(); got.Text != "reviewer\nui" || got.Tokens != 8 {
		t.Fatalf("parallel live text: %+v, want reviewer\\nui and 8 tokens", got)
	}
	endReviewer()
	if got := e.Live().Text; got != "ui" {
		t.Fatalf("ended or empty role added separators: %q", got)
	}
}
