// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// listedReportEntry keeps the identity seen at listing time.
type listedReportEntry struct {
	fs.DirEntry
	info fs.FileInfo
}

func (e listedReportEntry) Info() (fs.FileInfo, error) { return e.info, nil }

func TestReadReportReadsAPlainBoundedReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gate-1.md")
	for _, tc := range []struct{ input, want string }{
		{"\x1b[31mPASS\x1b[0m\nplain\x00", "PASS\nplain"},
		{strings.Repeat("line\n", (64<<10)/5) + strings.Repeat("X", 20), strings.TrimSuffix(strings.Repeat("line\n", (64<<10)/5), "\n") + "\n(cut: over 64 KiB)"},
	} {
		if err := os.WriteFile(path, []byte(tc.input), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := ReadReport(dir, "gate-1.md")
		if err != nil || got != tc.want {
			t.Fatalf("report length %d, error %v; want length %d: %q", len(got), err, len(tc.want), got)
		}
	}
}

func TestReadReportRefusesLinksPathsAndOtherFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gate-1.md")
	if err := os.WriteFile(path, []byte("real"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "dir.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"link.md", "../gate-1.md", path, "dir.md", "other.txt", "missing.md", ""} {
		if text, err := ReadReport(dir, name); !errors.Is(err, ErrNoReport) || text != "" {
			t.Errorf("%q: %q, %v", name, text, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if ent.Name() != "gate-1.md" {
			continue
		}
		// Capture the listing's identity, then replace it with a link to another regular file.
		info, err := ent.Info()
		if err != nil {
			t.Fatal(err)
		}
		ent = listedReportEntry{DirEntry: ent, info: info}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "other.txt"), path); err != nil {
			t.Fatal(err)
		}
		if text, ok := readReport(dir, ent); ok {
			t.Fatalf("swapped link read: %q", text)
		}
	}
}
