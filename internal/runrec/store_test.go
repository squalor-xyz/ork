// SPDX-License-Identifier: GPL-3.0-or-later

package runrec

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func requireMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode %04o, want %04o", path, got, want)
	}
}

func TestRunDirectoriesAre0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix modes")
	}
	s := newStore(t)
	run, err := s.Create(rec("private"))
	if err != nil {
		t.Fatal(err)
	}
	requireMode(t, s.Dir, 0o700)
	requireMode(t, run.Dir(), 0o700)
}

// oldModeRun models a run written before S51 and S75.
func oldModeRun(t *testing.T) (*Store, string) {
	t.Helper()
	s := newStore(t)
	run, err := s.Create(rec("old"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(run.Dir(), "run.json")
	for p, mode := range map[string]os.FileMode{s.Dir: 0o755, run.Dir(): 0o755, path: 0o644} {
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	return s, path
}

func TestSaveTightensAnOldRunDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix modes")
	}
	s, path := oldModeRun(t)
	run, err := s.Open("old")
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Update(func(r *Record) { r.Round++ }); err != nil {
		t.Fatal(err)
	}
	requireMode(t, run.Dir(), 0o700)
	requireMode(t, path, 0o600)
	// Saving one run does not change the existing runs root.
	requireMode(t, s.Dir, 0o755)
}

func TestLoadDoesNotChangeModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix modes")
	}
	s, path := oldModeRun(t)
	if _, err := s.Open("old"); err != nil {
		t.Fatal(err)
	}
	runs, err := s.List()
	if err != nil || len(runs) != 1 {
		t.Fatalf("List: %d runs, %v", len(runs), err)
	}
	requireMode(t, s.Dir, 0o755)
	requireMode(t, s.RunDir("old"), 0o755)
	requireMode(t, path, 0o644)
}

// A run id that is not a plain name is ErrBadRunID, so a caller can tell it
// from a record that is missing or unreadable (S147).
func TestOpenNamesABadRunID(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	for _, id := range []string{"", ".", "..", "../r1", `a\b`, "a/b"} {
		if _, err := s.Open(id); !errors.Is(err, ErrBadRunID) {
			t.Errorf("Open(%q) = %v, want ErrBadRunID", id, err)
		}
	}
	if _, err := s.Open("missing"); errors.Is(err, ErrBadRunID) || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Open(missing) = %v, want a not-exist error", err)
	}
}
