// SPDX-License-Identifier: GPL-3.0-or-later

package grok

import (
	"context"
	"testing"
)

func TestGrokConnection(t *testing.T) {
	s := fake(t, "", true)
	t.Setenv("ORK_GROK_STDERR", fixture(t, "connection", "stderr"))
	got, err := Run(context.Background(), s)
	if err != nil || got.Exit != Exit("connection") || got.Reason != "reqwest error sending request" {
		t.Fatalf("connection class/reason: %s / %q, %v", got.Exit, got.Reason, err)
	}
}
