// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package clirun

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadRegularFileRefusesAFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "report.md")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadRegularFile(fifo)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a FIFO was read as a file")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked on a FIFO")
	}

	real := filepath.Join(dir, "real.md")
	if err := os.WriteFile(real, []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRegularFile(link); err == nil {
		t.Error("a symlink was followed")
	}
	got, err := ReadRegularFile(real)
	if err != nil || string(got) != "text" {
		t.Errorf("regular file: %q, %v", got, err)
	}
	if _, err := ReadRegularFile(filepath.Join(dir, "missing.md")); !os.IsNotExist(err) {
		t.Errorf("a missing file gave %v, want not-exist", err)
	}
}

func TestOpenNoBlockWriteRefusesAFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "report.md")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := OpenNoBlockWrite(fifo, 0o600)
		if f != nil {
			f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a FIFO was opened for write")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked on a FIFO")
	}

	target := filepath.Join(dir, "target.md")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if f, err := OpenNoBlockWrite(link, 0o600); err == nil {
		f.Close()
		t.Error("a symlink was followed")
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "keep" {
		t.Errorf("the symlink's target changed: %q, %v", b, err)
	}

	path := filepath.Join(dir, "new.md")
	f, err := OpenNoBlockWrite(path, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("text"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "text" {
		t.Errorf("written file: %q, %v", b, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, %v", info.Mode(), err)
	}
}
