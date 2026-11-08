// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// storedVendors returns a Vendors on the clock that keeps its owner marks in
// path, and fails the test when the store cannot be read.
func storedVendors(t *testing.T, clock *vendorClock, path string) *Vendors {
	t.Helper()
	v, err := NewVendors(nil).WithClock(clock.Now).WithStore(path, nil)
	if err != nil {
		t.Fatalf("WithStore: %v", err)
	}
	return v
}

func TestOwnerMarksSurviveARestartAndAutomaticMarksDoNot(t *testing.T) {
	clock := newVendorClock()
	path := filepath.Join(t.TempDir(), "state", "vendors-out.json")
	v := storedVendors(t, clock, path)
	at := clock.Now()
	if err := v.MarkOutOwner("codex", "credits tomorrow", at); err != nil {
		t.Fatal(err)
	}
	v.MarkOut("claude", "usage limit", at) // memory only

	restarted := storedVendors(t, clock, path) // a new daemon
	out := restarted.Out()
	if len(out) != 1 || out[0].Vendor != "codex" || out[0].Reason != "credits tomorrow" || !out[0].Since.Equal(at) || !out[0].Until.IsZero() || !out[0].Owner {
		t.Fatalf("the owner mark must come back as it was, and the exit mark must not: %+v", out)
	}
	clock.Advance(1000 * time.Hour)
	if !restarted.IsOut("codex") {
		t.Fatal("a reloaded owner mark with no end must hold until back")
	}
	// The reloaded mark is still the owner's: an exit mark leaves it alone.
	restarted.MarkOut("codex", "usage limit", clock.Now())
	if out := restarted.Out(); len(out) != 1 || !out[0].Until.IsZero() || out[0].Reason != "credits tomorrow" {
		t.Fatalf("an exit mark changed a reloaded owner mark: %+v", out)
	}
}

func TestBackEndsAnOwnerMarkForTheNextRestartToo(t *testing.T) {
	clock := newVendorClock()
	path := filepath.Join(t.TempDir(), "vendors-out.json")
	v := storedVendors(t, clock, path)
	for _, name := range []string{"codex", "grok"} {
		if err := v.MarkOutOwner(name, "x", clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if was, err := v.Back("codex"); !was || err != nil {
		t.Fatalf("Back: %v %v", was, err)
	}
	out := storedVendors(t, clock, path).Out()
	if len(out) != 1 || out[0].Vendor != "grok" {
		t.Fatalf("only grok may come back out after codex was back: %+v", out)
	}
	if was, err := v.Back("grok"); !was || err != nil {
		t.Fatalf("Back: %v %v", was, err)
	}
	if out := storedVendors(t, clock, path).Out(); len(out) != 0 {
		t.Fatalf("no mark may survive when every vendor is back: %+v", out)
	}
}

func TestOwnerMarkWithAnEndLapsesAtThatTime(t *testing.T) {
	clock := newVendorClock()
	path := filepath.Join(t.TempDir(), "vendors-out.json")
	v := storedVendors(t, clock, path)
	at := clock.Now()
	until := at.Add(19 * time.Hour)
	if err := v.MarkOutOwnerUntil("codex", "weekly limit", at, until); err != nil {
		t.Fatal(err)
	}
	if out := v.Out(); len(out) != 1 || !out[0].Until.Equal(until) || !out[0].Since.Equal(at) || !out[0].Owner {
		t.Fatalf("the mark must end at the time given: %+v", out)
	}
	// An exit mark leaves an owner mark with an end alone: it keeps the owner's time.
	clock.Advance(time.Hour)
	v.MarkOut("codex", "usage limit", clock.Now())
	if out := v.Out(); len(out) != 1 || !out[0].Until.Equal(until) || out[0].Reason != "weekly limit" {
		t.Fatalf("an exit mark changed an owner mark with an end: %+v", out)
	}

	// The end survives a restart, and passes there.
	if out := storedVendors(t, clock, path).Out(); len(out) != 1 || !out[0].Until.Equal(until) || !out[0].Owner {
		t.Fatalf("the end must be stored: %+v", out)
	}
	clock.Advance(17*time.Hour + 59*time.Minute)
	if !v.IsOut("codex") || !storedVendors(t, clock, path).IsOut("codex") {
		t.Fatal("the mark must hold until its end")
	}
	clock.Advance(time.Minute)
	if v.IsOut("codex") {
		t.Fatal("the mark must lapse at its end")
	}
	if got := storedVendors(t, clock, path).Out(); len(got) != 0 {
		t.Fatalf("a restart after the end must not bring the mark back: %+v", got)
	}
}

func TestStoreDropsAMarkWhoseEndPassedWhileTheDaemonWasDown(t *testing.T) {
	clock := newVendorClock()
	path := filepath.Join(t.TempDir(), "vendors-out.json")
	v := storedVendors(t, clock, path)
	if err := v.MarkOutOwnerUntil("codex", "x", clock.Now(), clock.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := v.MarkOutOwner("grok", "y", clock.Now()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Hour)
	out := storedVendors(t, clock, path).Out()
	if len(out) != 1 || out[0].Vendor != "grok" {
		t.Fatalf("an owner mark past its end must not be reloaded: %+v", out)
	}
}

func TestStoreFileIsPrivateJSONAndHoldsOnlyOwnerMarks(t *testing.T) {
	clock := newVendorClock()
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "vendors-out.json")
	v := storedVendors(t, clock, path)
	if _, err := os.Stat(path); err == nil {
		t.Fatal("loading must not create the file")
	}
	v.MarkOut("claude", "usage limit", clock.Now())
	if _, err := os.Stat(path); err == nil {
		t.Fatal("an exit mark must not be written")
	}
	if err := v.MarkOutOwnerUntil("codex", "weekly limit", clock.Now(), clock.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", info.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	text := string(b)
	for _, want := range []string{`"version": 1`, `"vendor": "codex"`, `"reason": "weekly limit"`, `"since"`, `"until"`} {
		if !strings.Contains(text, want) {
			t.Errorf("file lacks %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, "claude") {
		t.Errorf("an exit mark is in the file:\n%s", text)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("a temp file was left behind: %v", entries)
	}
}

func TestStoreProblemsAreReportedAndKeepMemoryAndFileConsistent(t *testing.T) {
	clock := newVendorClock()
	dir := t.TempDir()

	// A file that is not valid JSON is an error, and the value starts empty.
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := NewVendors(nil).WithClock(clock.Now).WithStore(bad, nil)
	if err == nil || v == nil || len(v.Out()) != 0 {
		t.Fatalf("a corrupt store: %v, %+v", err, v)
	}
	// A file of a version this ork does not know is an error too.
	future := filepath.Join(dir, "future.json")
	if err := os.WriteFile(future, []byte(`{"version":9,"marks":[{"vendor":"codex","since":"2026-10-29T12:00:00Z"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if v, err := NewVendors(nil).WithClock(clock.Now).WithStore(future, nil); err == nil || len(v.Out()) != 0 {
		t.Fatalf("an unknown version: %v, %+v", err, v.Out())
	}

	// A store that cannot be written fails the mark, and nothing is left half done.
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stuck, err := NewVendors(nil).WithClock(clock.Now).WithStore(filepath.Join(blocker, "vendors-out.json"), nil)
	if err == nil {
		t.Fatal("a store under a file cannot be read")
	}
	if err := stuck.MarkOutOwner("codex", "x", clock.Now()); err == nil {
		t.Fatal("a mark that cannot be stored must fail")
	}
	if stuck.IsOut("codex") {
		t.Fatal("a mark that could not be stored must not be kept in memory")
	}
	if err := stuck.MarkOutOwnerUntil("codex", "x", clock.Now(), clock.Now().Add(time.Hour)); err == nil || stuck.IsOut("codex") {
		t.Fatalf("an end that cannot be stored: %v", err)
	}

	// A store that cannot be replaced fails the mark and leaves no temp file behind.
	asDir := filepath.Join(dir, "dirstore")
	if err := os.MkdirAll(filepath.Join(asDir, "vendors-out.json", "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	blocked, _ := NewVendors(nil).WithClock(clock.Now).WithStore(filepath.Join(asDir, "vendors-out.json"), nil)
	if err := blocked.MarkOutOwner("codex", "x", clock.Now()); err == nil || blocked.IsOut("codex") {
		t.Fatalf("a store that is a directory: %v", err)
	}
	if entries, _ := os.ReadDir(asDir); len(entries) != 1 {
		t.Fatalf("a failed store left a temp file behind: %v", entries)
	}

	// A back that cannot be stored fails and keeps the mark.
	path := filepath.Join(dir, "ok", "vendors-out.json")
	v = storedVendors(t, clock, path)
	if err := v.MarkOutOwner("codex", "x", clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Dir(path), 0o700)
	if os.Geteuid() != 0 { // root writes anywhere
		if was, err := v.Back("codex"); err == nil || was || !v.IsOut("codex") {
			t.Fatalf("a back that cannot be stored: was %v, err %v, out %v", was, err, v.IsOut("codex"))
		}
	}
}

func TestNoStoreKeepsMarksInMemoryOnly(t *testing.T) {
	clock := newVendorClock()
	v := NewVendors(nil).WithClock(clock.Now)
	if err := v.MarkOutOwner("codex", "x", clock.Now()); err != nil {
		t.Fatal(err)
	}
	if was, err := v.Back("codex"); !was || err != nil {
		t.Fatalf("Back: %v %v", was, err)
	}
	var nilVendors *Vendors
	if err := nilVendors.MarkOutOwner("codex", "x", clock.Now()); err != nil {
		t.Fatal(err)
	}
	if was, err := nilVendors.Back("codex"); was || err != nil {
		t.Fatalf("Back on nil: %v %v", was, err)
	}
}

// Owner marks, backs, exit marks and reads from many goroutines leave the store
// and the memory agreeing (run with -race).
func TestStoredVendorsAreSafeForConcurrentUse(t *testing.T) {
	clock := newVendorClock()
	path := filepath.Join(t.TempDir(), "vendors-out.json")
	v := storedVendors(t, clock, path)
	var wg sync.WaitGroup
	for _, name := range []string{"claude", "codex", "grok"} {
		for i := 0; i < 20; i++ {
			wg.Add(3)
			go func() { defer wg.Done(); _ = v.MarkOutOwner(name, "x", clock.Now()) }()
			go func() { defer wg.Done(); v.MarkOut(name, "limit", clock.Now()); _ = v.Out() }()
			go func() { defer wg.Done(); _, _ = v.Back(name) }()
		}
	}
	wg.Wait()
	for _, name := range []string{"claude", "codex", "grok"} {
		if _, err := v.Back(name); err != nil {
			t.Fatal(err)
		}
	}
	if out := storedVendors(t, clock, path).Out(); len(out) != 0 {
		t.Fatalf("after every vendor was back the store holds %+v", out)
	}
}

// A vendor name the caller does not know is not loaded, so that it cannot be
// stuck out with no command that can end it, and the next store drops it.
func TestStoreSkipsVendorNamesTheCallerDoesNotKnow(t *testing.T) {
	clock := newVendorClock()
	path := filepath.Join(t.TempDir(), "vendors-out.json")
	body := `{"version":1,"marks":[` +
		`{"vendor":"gemini","reason":"old","since":"2026-10-29T12:00:00Z"},` +
		`{"vendor":"codex","reason":"kept","since":"2026-10-29T12:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	known := func(name string) bool { return name == "claude" || name == "codex" || name == "grok" }
	v, err := NewVendors(nil).WithClock(clock.Now).WithStore(path, known)
	if err != nil {
		t.Fatal(err)
	}
	if out := v.Out(); len(out) != 1 || out[0].Vendor != "codex" || out[0].Reason != "kept" {
		t.Fatalf("only the known vendor may be loaded: %+v", out)
	}
	if v.IsOut("gemini") {
		t.Fatal("an unknown vendor is out")
	}
	if err := v.MarkOutOwner("grok", "x", clock.Now()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "gemini") {
		t.Fatalf("the next store kept the unknown vendor:\n%s", b)
	}
}
