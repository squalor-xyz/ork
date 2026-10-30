// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
)

func TestVendorCommandListsMarksAndClears(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})

	for _, args := range [][]string{{"vendor"}, {"vendor", "list"}} {
		code, stdout, stderr := run(t, w.app, args...)
		if code != 0 || stdout != "no vendor is out\n" {
			t.Fatalf("%v: exit %d\nstdout: %q\nstderr: %q", args, code, stdout, stderr)
		}
	}

	// An owner mark has no end: it holds until back.
	code, stdout, stderr := run(t, w.app, "vendor", "out", "codex", "--reason", "usage limit")
	if code != 0 || !regexp.MustCompile(`^codex is out until back \(since \d\d:\d\d; usage limit\)\n$`).MatchString(stdout) {
		t.Fatalf("out: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	// The reason is optional; a plain one stands in for it.
	if code, stdout, _ = run(t, w.app, "vendor", "out", "claude"); code != 0 || !regexp.MustCompile(`^claude is out until back \(since \d\d:\d\d; marked out by the owner\)\n$`).MatchString(stdout) {
		t.Fatalf("out claude: exit %d: %q", code, stdout)
	}
	if code, stdout, _ = run(t, w.app, "vendor", "back", "claude"); code != 0 || stdout != "claude is back\n" {
		t.Fatalf("back claude: exit %d: %q", code, stdout)
	}

	want := regexp.MustCompile(`^codex is out until back \(since \d\d:\d\d; usage limit\)\n$`)
	for _, args := range [][]string{{"vendor"}, {"vendor", "list"}} {
		code, stdout, _ = run(t, w.app, args...)
		if code != 0 || !want.MatchString(stdout) {
			t.Fatalf("%v: exit %d: %q", args, code, stdout)
		}
	}

	code, stdout, _ = run(t, w.app, "vendor", "--json")
	var listed daemon.VendorsResult
	mustJSON(t, stdout, &listed)
	if code != 0 || len(listed.VendorsOut) != 1 || listed.VendorsOut[0].Vendor != "codex" || listed.VendorsOut[0].Reason != "usage limit" || !listed.VendorsOut[0].Until.IsZero() {
		t.Fatalf("list --json: exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `"vendors_out": [`) || strings.Count(stdout, `"vendor": "`) != 1 || strings.Contains(stdout, `"until"`) {
		t.Fatalf("list --json shape: %s", stdout)
	}

	// ork status says so, once for each vendor that is out.
	code, stdout, _ = run(t, w.app, "status")
	if code != 0 || !regexp.MustCompile(`(?m)^codex is out until back \(since \d\d:\d\d; usage limit\)$`).MatchString(stdout) || strings.Count(stdout, " is out until ") != 1 {
		t.Fatalf("status: exit %d: %q", code, stdout)
	}
	code, stdout, _ = run(t, w.app, "status", "--json")
	var status daemon.StatusResult
	mustJSON(t, stdout, &status)
	if code != 0 || len(status.VendorsOut) != 1 || status.VendorsOut[0].Vendor != "codex" {
		t.Fatalf("status --json: exit %d: %s", code, stdout)
	}

	code, stdout, _ = run(t, w.app, "vendor", "back", "codex", "--json")
	mustJSON(t, stdout, &listed)
	if code != 0 || len(listed.VendorsOut) != 0 || !strings.Contains(stdout, `"vendors_out": []`) {
		t.Fatalf("back --json: exit %d: %s", code, stdout)
	}
	// Back says so when the vendor was not out.
	if code, stdout, _ = run(t, w.app, "vendor", "back", "codex"); code != 0 || stdout != "codex was not out\n" {
		t.Fatalf("back when not out: exit %d: %q", code, stdout)
	}
	if code, stdout, _ = run(t, w.app, "status"); code != 0 || strings.Contains(stdout, " is out until ") {
		t.Fatalf("status after back: exit %d: %q", code, stdout)
	}
}

func TestVendorCommandUsageErrors(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	starts := w.starts.Load()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"vendor", "nope"}, `unknown vendor command "nope"`},
		{[]string{"vendor", "out"}, "vendor out needs a vendor name"},
		{[]string{"vendor", "out", "gemini"}, `unsupported vendor "gemini"`},
		{[]string{"vendor", "out", "codex", "claude"}, "vendor out takes one vendor name"},
		{[]string{"vendor", "out", "codex", "--nope"}, "flag provided but not defined: -nope"},
		{[]string{"vendor", "back"}, "vendor back needs a vendor name"},
		{[]string{"vendor", "back", "gemini"}, `unsupported vendor "gemini"`},
		{[]string{"vendor", "back", "codex", "--reason", "x"}, "flag provided but not defined: -reason"},
		{[]string{"vendor", "list", "extra"}, "vendor list takes no arguments"},
	} {
		code, _, stderr := run(t, w.app, tc.args...)
		if code != 2 || !strings.Contains(stderr, "vendor") || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: exit %d: %q, want %q", tc.args, code, stderr, tc.want)
		}
	}
	if w.starts.Load() != starts {
		t.Fatal("a usage error started the daemon")
	}
}

func TestHelpGroupsTheVendorMemoryNoteOnTheListLine(t *testing.T) {
	w := newWorld(t, stubSource{})
	_, stdout, _ := run(t, w.app, "help")
	list := regexp.MustCompile(`(?m)^  ork vendor \[list\].*\n( {10,}.*\n)*`).FindString(stdout)
	if !strings.Contains(list, "marks happen on their own; a restart clears them") || strings.Contains(list, "say the vendor is back") {
		t.Fatalf("the list entry:\n%s", list)
	}
	back := regexp.MustCompile(`(?m)^  ork vendor back NAME.*\n( {10,}.*\n)*`).FindString(stdout)
	if !strings.Contains(back, "say the vendor is back; ends the mark early") || strings.Contains(back, "restart") || strings.Contains(back, "60 minutes") {
		t.Fatalf("the back entry:\n%s", back)
	}
	out := regexp.MustCompile(`(?m)^  ork vendor out NAME.*\n( {10,}.*\n)*`).FindString(stdout)
	if strings.Contains(out, "60 minutes") || !strings.Contains(out, "until back") {
		t.Fatalf("an owner mark has no 60-minute expiry:\n%s", out)
	}
}
