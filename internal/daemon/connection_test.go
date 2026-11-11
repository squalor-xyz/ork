// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"github.com/squalor-xyz/ork/internal/agent/claude"
	"github.com/squalor-xyz/ork/internal/agent/grok"
	"github.com/squalor-xyz/ork/internal/engine"
	"testing"
)

func TestConnectionRunnerMapping(t *testing.T) {
	for name, runner := range map[string]engine.Runner{
		"claude": claudeRunner{round: func(context.Context, claude.Spec) (claude.Round, error) {
			return claude.Round{Class: claude.Class("connection"), Reason: "api_error connection_failed"}, nil
		}},
		"grok": grokRunner{round: func(context.Context, grok.Spec) (grok.Round, error) {
			return grok.Round{Exit: grok.Exit("connection"), Reason: "reqwest error sending request"}, nil
		}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := runner.Run(context.Background(), engine.Request{Vendor: name})
			want := "api_error connection_failed"
			if name == "grok" {
				want = "reqwest error sending request"
			}
			if err != nil || got.Exit != engine.ExitClass("connection") || got.Reason != want {
				t.Fatalf("connection mapping: %s / %q, %v", got.Exit, got.Reason, err)
			}
		})
	}
}
