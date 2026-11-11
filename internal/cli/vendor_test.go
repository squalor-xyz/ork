// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
)

func TestLocalVendorCommandsAcceptLauncherNames(t *testing.T) {
	for _, vendor := range []string{"claude-local", "codex-local", "grok-local"} {
		t.Run(vendor, func(t *testing.T) {
			code, out, err := run(t, &App{}, "vendor", "out", vendor, "--until", "invalid-time")
			if code != ExitUsage || out != "" || !strings.Contains(err, "--until: cannot read") {
				t.Fatalf("local name was rejected before time validation: exit=%d out=%q err=%q", code, out, err)
			}
		})
	}
}

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

func TestVendorOutAndBackAcceptGrok(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	code, stdout, stderr := run(t, w.app, "vendor", "out", "grok", "--reason", "credits")
	if code != 0 || !regexp.MustCompile(`^grok is out until back \(since \d\d:\d\d; credits\)\n$`).MatchString(stdout) {
		t.Fatalf("out: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	if code, stdout, _ = run(t, w.app, "vendor", "back", "grok"); code != 0 || stdout != "grok is back\n" {
		t.Fatalf("back: exit %d: %q", code, stdout)
	}
	code, _, stderr = run(t, w.app, "vendor", "out", "gemini")
	if code != 2 || !strings.Contains(stderr, "use "+strings.Join(daemon.Vendors(), ", ")) {
		t.Fatalf("refusal: exit %d: %q", code, stderr)
	}
	_, help, _ := run(t, w.app, "help")
	if !strings.Contains(help, "mark claude, codex or grok out until back, or") {
		t.Fatalf("usage text:\n%s", help)
	}
}

func TestVendorOutIsCaseInsensitive(t *testing.T) {
	// Without a daemon: a name in any case gets past the vendor check, to the
	// time check after it.
	for _, name := range []string{"Claude", "CODEX", " Grok-Local "} {
		code, out, err := run(t, &App{}, "vendor", "out", name, "--until", "invalid-time")
		if code != ExitUsage || out != "" || !strings.Contains(err, "--until: cannot read") {
			t.Fatalf("%q was rejected before time validation: exit=%d out=%q err=%q", name, code, out, err)
		}
	}
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	code, stdout, stderr := run(t, w.app, "vendor", "out", "Claude", "--reason", "limit")
	if code != 0 || !regexp.MustCompile(`^claude is out until back \(since \d\d:\d\d; limit\)\n$`).MatchString(stdout) {
		t.Fatalf("out Claude: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	code, stdout, _ = run(t, w.app, "vendor", "list", "--json")
	var listed daemon.VendorsResult
	mustJSON(t, stdout, &listed)
	if code != 0 || len(listed.VendorsOut) != 1 || listed.VendorsOut[0].Vendor != "claude" {
		t.Fatalf("the daemon holds %s", stdout)
	}
	if code, stdout, stderr = run(t, w.app, "vendor", "back", " CLAUDE "); code != 0 || stdout != "claude is back\n" {
		t.Fatalf("back CLAUDE: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
}

func TestUnknownVendorNameListsTheVendors(t *testing.T) {
	for _, sub := range []string{"out", "back"} {
		code, _, stderr := run(t, &App{}, "vendor", sub, "acme")
		want := "vendor " + sub + ": unsupported vendor \"acme\"; use " + strings.Join(daemon.Vendors(), ", ") + "\n"
		if code != ExitUsage || !strings.Contains(stderr, want) {
			t.Errorf("%s: exit %d: %q, want suffix %q", sub, code, stderr, want)
		}
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
		{[]string{"vendor", "back", "codex", "--until", "07:12"}, "flag provided but not defined: -until"},
		{[]string{"vendor", "out", "codex", "--until", "tomorrow-ish"}, `--until: cannot read "tomorrow-ish"; use HH:MM (07:12), YYYY-MM-DD HH:MM (2026-11-04 07:12) or a full date and zone (2026-11-04T07:12:00+01:00)`},
		{[]string{"vendor", "out", "codex", "--until", "2020-01-02 03:04"}, "--until: 2020-01-02 03:04 is not in the future"},
		{[]string{"vendor", "out", "codex", "--until", ""}, "--until: cannot read"},
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
	if !strings.Contains(strings.Join(strings.Fields(list), " "), "marks happen on their own and end after 60 minutes or at a restart; marks set with ork vendor out stay across a restart") || strings.Contains(list, "say the vendor is back") {
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
	if flat := strings.Join(strings.Fields(out), " "); !strings.Contains(flat, "[--until TIME]") || !strings.Contains(flat, "or until TIME (HH:MM, YYYY-MM-DD HH:MM or a full date and zone); a restart keeps it") {
		t.Fatalf("the out entry names --until and the restart:\n%s", out)
	}
}

func TestParseVendorUntil(t *testing.T) {
	zone := time.FixedZone("test", -5*3600)
	now := time.Date(2026, 11, 3, 23, 56, 0, 0, zone)
	for _, tc := range []struct {
		in   string
		want time.Time
	}{
		{"07:12", time.Date(2026, 11, 4, 7, 12, 0, 0, zone)},             // the clock time has passed today: tomorrow
		{"23:59", time.Date(2026, 11, 3, 23, 59, 0, 0, zone)},            // still ahead today
		{"7:12", time.Date(2026, 11, 4, 7, 12, 0, 0, zone)},              // no leading zero
		{" 07:12 ", time.Date(2026, 11, 4, 7, 12, 0, 0, zone)},           // blanks around it
		{"2026-11-05 16:30", time.Date(2026, 11, 5, 16, 30, 0, 0, zone)}, // in the owner's zone
		{"2026-11-05T16:30:00Z", time.Date(2026, 11, 5, 16, 30, 0, 0, time.UTC)},
		{"2026-11-05T16:30:00+01:00", time.Date(2026, 11, 5, 15, 30, 0, 0, time.UTC)},
	} {
		got, err := parseVendorUntil(tc.in, now)
		if err != nil || !got.Equal(tc.want) {
			t.Errorf("%q: %v %v, want %v", tc.in, got, err, tc.want)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"", "cannot read"},
		{"noon", "cannot read"},
		{"25:00", "cannot read"},
		{"2026-11-03 23:56", "is not in the future"}, // now is not in the future
		{"2026-11-03 23:55", "is not in the future"},
		{"2026-11-03T23:00:00-05:00", "is not in the future"},
	} {
		if got, err := parseVendorUntil(tc.in, now); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: %v %v, want an error with %q", tc.in, got, err, tc.want)
		}
	}
}

func TestVendorOutUntilSendsTheEndAndListsIt(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	end := time.Now().Add(3 * time.Hour).Truncate(time.Minute)

	code, stdout, stderr := run(t, w.app, "vendor", "out", "codex", "--reason", "weekly limit", "--until", end.Format("2006-01-02 15:04"), "--json")
	var set daemon.VendorsResult
	mustJSON(t, stdout, &set)
	if code != 0 || len(set.VendorsOut) != 1 || !set.VendorsOut[0].Until.Equal(end) || set.VendorsOut[0].Reason != "weekly limit" {
		t.Fatalf("out --until: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}

	// The text names the end where the other marks name theirs: the day when it
	// is not the day the mark began (the mark's own start, not the clock).
	since := set.VendorsOut[0].Since
	wantEnd := end.Format("15:04")
	if sy, sm, sd := since.In(end.Location()).Date(); end.Year() != sy || end.Month() != sm || end.Day() != sd {
		wantEnd = end.Format("2 Jan 15:04")
	}
	code, stdout, _ = run(t, w.app, "vendor", "out", "claude", "--until", end.Format(time.RFC3339))
	if code != 0 || !strings.HasPrefix(stdout, "claude is out until "+wantEnd+" (since ") || strings.Contains(stdout, "until back") {
		t.Fatalf("out claude --until: exit %d: %q", code, stdout)
	}
	code, stdout, _ = run(t, w.app, "vendor", "list")
	if code != 0 || strings.Count(stdout, " is out until "+wantEnd+" (") != 2 || strings.Contains(stdout, "until back") {
		t.Fatalf("list: exit %d: %q", code, stdout)
	}
	code, stdout, _ = run(t, w.app, "status")
	if code != 0 || strings.Count(stdout, " is out until "+wantEnd+" (") != 2 {
		t.Fatalf("status: exit %d: %q", code, stdout)
	}
	// Out again without --until makes it an owner mark with no end.
	if code, stdout, _ = run(t, w.app, "vendor", "out", "claude"); code != 0 || !strings.HasPrefix(stdout, "claude is out until back") {
		t.Fatalf("out without --until: exit %d: %q", code, stdout)
	}
}

func TestVendorOutUntilClockTimeIsTheNextTimeItIsThatOnTheClock(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	// A clock time two hours ahead is the next such time, today or tomorrow.
	start := time.Now()
	code, stdout, stderr := run(t, w.app, "vendor", "out", "codex", "--until", start.Add(2*time.Hour).Format("15:04"), "--json")
	var set daemon.VendorsResult
	mustJSON(t, stdout, &set)
	if code != 0 || len(set.VendorsOut) != 1 {
		t.Fatalf("exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	if end := set.VendorsOut[0].Until; !end.After(start.Add(time.Hour)) || end.After(start.Add(3*time.Hour)) || end.Second() != 0 {
		t.Fatalf("end %v, want the minute two hours after %v", end, start)
	}
}
