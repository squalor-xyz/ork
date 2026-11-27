// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestMainLockSecondHolderWaitsForTheFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ork-main.lock")
	release, err := lockMainFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		unlock, err := lockMainFile(ctx, path)
		if err == nil {
			unlock()
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("second holder returned before release: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	release()
	if err := <-done; err != nil {
		t.Fatalf("second holder after release: %v", err)
	}
}

func TestMainLockWaitGivesUpOnCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ork-main.lock")
	release, err := lockMainFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		unlock, err := lockMainFile(ctx, path)
		if err == nil {
			unlock()
		}
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting lock: %v, want context.Canceled", err)
	}
	release()
	nextCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	unlock, err := lockMainFile(nextCtx, path)
	if err != nil {
		t.Fatalf("next holder: %v", err)
	}
	unlock()
}

func TestMainLockFileIsOneFilePerRepository(t *testing.T) {
	for _, sub := range []bool{false, true} {
		t.Run(fmt.Sprint(sub), func(t *testing.T) {
			h := launchTrackingHarness(t, sub)
			e := &Engine{proj: h.proj, d: h.deps}
			path, err := e.mainLockPath(context.Background())
			if want := filepath.Join(h.P, ".git", "ork-main.lock"); err != nil || path != want {
				t.Fatalf("main lock path %q, %v; want %q", path, err, want)
			}
		})
	}
}
