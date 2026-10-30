// SPDX-License-Identifier: GPL-3.0-or-later

package source

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestVendorOutSaysTheEndFirst(t *testing.T) {
	since := time.Date(2026, 10, 29, 16, 2, 0, 0, time.UTC)
	exit := VendorOut{Vendor: "codex", Reason: "You've hit  your usage\nlimit", Since: since, Until: since.Add(time.Hour)}
	if got, want := exit.Sentence(), "codex is out until 17:02 (since 16:02; You've hit your usage limit)"; got != want {
		t.Errorf("exit mark: %q, want %q", got, want)
	}
	if exit.UntilText() != "17:02" {
		t.Errorf("UntilText %q", exit.UntilText())
	}
	owner := VendorOut{Vendor: "claude", Reason: "credits tomorrow", Since: since}
	if got, want := owner.Sentence(), "claude is out until back (since 16:02; credits tomorrow)"; got != want {
		t.Errorf("owner mark: %q, want %q", got, want)
	}
	if got, want := (VendorOut{Vendor: "codex", Since: since}).Sentence(), "codex is out until back (since 16:02)"; got != want {
		t.Errorf("no reason: %q, want %q", got, want)
	}
	// An owner mark names no end on the wire.
	b, _ := json.Marshal(owner)
	if strings.Contains(string(b), "until") {
		t.Errorf("owner mark JSON names an end: %s", b)
	}
}
