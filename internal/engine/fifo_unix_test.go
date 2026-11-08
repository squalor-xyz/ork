// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package engine

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func staleEntry(t *testing.T) (string, string, fs.DirEntry) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "a-80x24.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	// Keep the original inode alive so the replacement cannot reuse it.
	original, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { original.Close() })
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	return dir, path, listedReportEntry{DirEntry: entries[0], info: info}
}

func fifoRead(t *testing.T, render bool) {
	t.Helper()
	dir, path, ent := staleEntry(t)
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	type result struct {
		text    string
		refused bool
	}
	done := make(chan result, 1)
	go func() {
		if render {
			text, err := readRender(dir, ent)
			done <- result{text, errors.Is(err, errNotThatFile)}
		} else {
			text, ok := readReport(dir, ent)
			done <- result{text, !ok}
		}
	}()
	select {
	case got := <-done:
		if !got.refused || got.text != "" {
			t.Fatalf("FIFO returned %+v, want refusal", got)
		}
	case <-time.After(2 * time.Second):
		// Release a blocking open before failing, so no test goroutine leaks.
		f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		<-done
		t.Fatal("read blocked on swapped FIFO")
	}
}

func TestReadRenderDoesNotBlockOnAFileSwappedForAFIFO(t *testing.T) { fifoRead(t, true) }
func TestReadReportDoesNotBlockOnAFileSwappedForAFIFO(t *testing.T) { fifoRead(t, false) }

func TestReadRenderAndReadReportRefuseASymlinkAtOpen(t *testing.T) {
	for _, render := range []bool{true, false} {
		name := "report"
		if render {
			name = "render"
		}
		t.Run(name, func(t *testing.T) {
			dir, path, ent := staleEntry(t)
			target := filepath.Join(dir, "target")
			if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			if render {
				if text, err := readRender(dir, ent); err == nil || text != "" {
					t.Fatalf("render followed link: %q, %v", text, err)
				}
			} else {
				if text, ok := readReport(dir, ent); ok || text != "" {
					t.Fatalf("report followed link: %q, %v", text, ok)
				}
			}
		})
	}
}

func TestOpenNoBlockReadsARegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "regular")
	want := "all of the regular file\n"
	if err := os.WriteFile(path, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := openNoBlock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil || string(b) != want {
		t.Fatalf("read %q, %v; want %q", b, err, want)
	}
}
