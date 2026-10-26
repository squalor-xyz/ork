// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/agent/claude"
	"github.com/squalor-xyz/ork/internal/agent/codex"
	"github.com/squalor-xyz/ork/internal/engine"
)

func TestAdaptersForwardZeroUsage(t *testing.T) {
	for _, vendor := range []string{"claude", "codex"} {
		t.Run(vendor, func(t *testing.T) {
			known := false
			req := engine.Request{Vendor: vendor, OnLive: func(l engine.Live) { known = known || l.UsageKnown }}
			var runner engine.Runner
			if vendor == "claude" {
				runner = claudeRunner{env: []string{}, round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
					s.OnEvent(claude.Event{Kind: claude.EventUsage, UsageStart: true})
					return claude.Round{}, nil
				}}
			} else {
				runner = codexRunner{env: []string{}, round: func(_ context.Context, s codex.Spec) (codex.Round, error) {
					s.OnEvent(codex.Event{Type: "turn.completed"})
					return codex.Round{}, nil
				}}
			}
			if _, err := runner.Run(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if !known {
				t.Fatal("zero usage event was lost")
			}
		})
	}
}

func TestAdaptersMapTextAndUsageEvents(t *testing.T) {
	for _, vendor := range []string{"claude", "codex"} {
		t.Run(vendor, func(t *testing.T) {
			path := "../../testdata/vendors/" + vendor + "/01-plain-stream-jsonl.stdout"
			wantText := ""
			wantTokens := 17091
			if vendor == "claude" {
				path = "../../testdata/vendors/claude/02-stream-json.stdout"
				wantText = "1\n2\n3\n4\n5"
				wantTokens = 23763
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := ""
			tokens := 0
			events := 0
			beforeReturn := false
			req := engine.Request{Vendor: vendor, OnEvent: func() { events++ }, OnLive: func(l engine.Live) { text += l.Text; tokens += l.Tokens; beforeReturn = true }}
			var r engine.Runner
			if vendor == "claude" {
				r = claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
					for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
						ev, err := claude.ParseLine([]byte(line))
						if err != nil {
							t.Fatal(err)
						}
						s.OnEvent(ev)
					}
					if !beforeReturn {
						t.Fatal("no live callback")
					}
					return claude.Round{}, nil
				}}
			} else {
				r = codexRunner{round: func(_ context.Context, s codex.Spec) (codex.Round, error) {
					for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
						ev, err := codex.ParseLine([]byte(line))
						if err != nil {
							t.Fatal(err)
						}
						if ev.Type == "item.completed" && ev.Item != nil && ev.Item.Type == "agent_message" {
							wantText += ev.Item.Text + "\n"
						}
						s.OnEvent(ev)
					}
					if !beforeReturn {
						t.Fatal("no live callback")
					}
					return codex.Round{}, nil
				}}
			}
			if _, err := r.Run(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if text != wantText || tokens != wantTokens || events == 0 {
				t.Fatalf("text %q want %q; tokens %d want %d; events %d", text, wantText, tokens, wantTokens, events)
			}
		})
	}
}

func TestLiveRedactionAcrossDeltas(t *testing.T) {
	env := []string{"API_KEY=abcdefghijk", "TOKEN=short", "NAME=visible-value", "PASSWORD=abcdefgh", "CREDENTIAL=long-credential"}
	got := ""
	r := claudeRunner{env: env, round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
		for _, text := range []string{"prefix abc", "defghijk and abcdefgh and long-", "credential short visible-value"} {
			s.OnEvent(claude.Event{Kind: claude.EventTextDelta, Text: text})
		}
		return claude.Round{}, nil
	}}
	_, err := r.Run(context.Background(), engine.Request{Vendor: "claude", OnLive: func(l engine.Live) {
		got += l.Text
		if strings.Contains(got, "abcdefgh") {
			t.Fatal("secret entered callback")
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "prefix *** and *** and *** short visible-value" {
		t.Fatalf("redacted text %q", got)
	}
}

func TestClaudeLiveUsageResetsPerMessage(t *testing.T) {
	lines := []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":20,"cache_creation_input_tokens":30}}}}`,
		`{"type":"stream_event","event":{"type":"message_delta","usage":{"output_tokens":5}}}`,
		`{"type":"stream_event","event":{"type":"message_delta","usage":{"output_tokens":8}}}`,
		`{"type":"stream_event","event":{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":1}}}}`,
		`{"type":"stream_event","event":{"type":"message_delta","usage":{"output_tokens":4}}}`,
		`{"type":"result","usage":{"input_tokens":11,"output_tokens":12,"cache_read_input_tokens":20,"cache_creation_input_tokens":30}}`,
	}
	var totals []int
	sum := 0
	r := claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
		for _, line := range lines {
			ev, err := claude.ParseLine([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			s.OnEvent(ev)
		}
		return claude.Round{}, nil
	}}
	_, err := r.Run(context.Background(), engine.Request{Vendor: "claude", OnLive: func(l engine.Live) { sum += l.Tokens; totals = append(totals, sum) }})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{62, 65, 68, 70, 73}
	if len(totals) != len(want) {
		t.Fatal(totals)
	}
	for i := range want {
		if totals[i] != want[i] {
			t.Fatalf("totals %v want %v", totals, want)
		}
	}
}

func TestLiveRedactorSelectionAndFlush(t *testing.T) {
	env := []string{"low_api_key=abcdefgh", "OTHER_TOKEN=abcdefghi", "SECRET=界界界", "PATH=public-value", "PASSWORD=short", "MY_CREDENTIAL=credential-value"}
	var text string
	redactor := newLiveRedactor(env, func(l engine.Live) { text += l.Text })
	redactor.send("abcdefgh", 0, false)
	if text != "" {
		t.Fatal("longer secret prefix escaped")
	}
	redactor.send("i 界界界 public-value short credential-value partial abc", 0, false)
	redactor.send("", 0, true)
	if text != "*** 界界界 public-value short *** partial ***" {
		t.Fatal(text)
	}
}

func TestFinalRedactorFlushMasksSecretPrefix(t *testing.T) {
	var got string
	r := newLiveRedactor([]string{"API_KEY=abcdefghijk"}, func(l engine.Live) { got += l.Text })
	r.send("visible abc", 0, false)
	r.send("", 0, true)
	if got != "visible ***" {
		t.Fatalf("held prefix escaped at final flush: %q", got)
	}
}
