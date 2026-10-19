// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// lockHeld is the error daemon.Run returns when another daemon holds the lock.
func lockHeld() error {
	return fmt.Errorf("%w (pid 1, socket elsewhere)", daemon.ErrAlreadyRunning)
}

// A start that finds the lock held while its socket does not answer is an
// idle exit that has unlinked its socket but not yet released the lock (S03).
// The start retries until the lock is free, and then serves.
func TestDaemonStartRetriesWhileAnExitingDaemonHoldsTheLock(t *testing.T) {
	sandbox(t)
	paths, err := daemon.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}

	old := daemon.New(daemon.Config{Paths: paths, Source: emptySource{}})
	oldCtx, stopOld := context.WithCancel(context.Background())
	defer stopOld()
	oldDone := make(chan error, 1)
	go func() { oldDone <- old.Run(oldCtx) }()
	select {
	case <-old.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("the old daemon did not start")
	}
	// The exiting daemon's socket is gone while it still holds the lock.
	if err := os.Remove(paths.Socket); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- serveDaemon(ctx) }()

	time.Sleep(200 * time.Millisecond)
	select {
	case err := <-served:
		t.Fatalf("the start gave up while the lock was still held: %v", err)
	default:
	}

	stopOld()
	if err := <-oldDone; err != nil {
		t.Fatalf("the old daemon: %v", err)
	}
	c := dialUntilUp(t, paths.Socket)
	if _, err := c.Projects(ctx); err != nil {
		t.Fatalf("the retried start does not answer: %v", err)
	}
	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serveDaemon: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveDaemon did not stop when its context ended")
	}
}

// When the lock never frees, a start gives up after a bounded wait, with the
// lock error.
func TestDaemonStartGivesUpAfterAShortWaitWhenTheLockNeverFrees(t *testing.T) {
	sandbox(t)
	paths, err := daemon.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	run := func(context.Context) error {
		calls++
		return lockHeld()
	}
	start := time.Now()
	err = serveWithRetry(context.Background(), paths.Socket, run)
	elapsed := time.Since(start)
	if !errors.Is(err, daemon.ErrAlreadyRunning) {
		t.Fatalf("error %v, want ErrAlreadyRunning", err)
	}
	if calls < 2 {
		t.Fatalf("run called %d time(s); a held lock with no socket must be retried", calls)
	}
	if elapsed < 1500*time.Millisecond || elapsed > 5*time.Second {
		t.Errorf("gave up after %v, want about 2 s", elapsed)
	}
}

// Guard for the other branch: while a live daemon answers on the socket, the
// lock error stands and nothing is retried. It passes before the retry too,
// since the wrapper then calls run once; mutation checks cover an always-retry.
func TestDaemonStartDoesNotRetryWhileALiveDaemonAnswers(t *testing.T) {
	sandbox(t)
	paths, err := daemon.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	live := daemon.New(daemon.Config{Paths: paths, Source: emptySource{}})
	liveCtx, stopLive := context.WithCancel(context.Background())
	liveDone := make(chan error, 1)
	go func() { liveDone <- live.Run(liveCtx) }()
	select {
	case <-live.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("the live daemon did not start")
	}
	t.Cleanup(func() {
		stopLive()
		<-liveDone
	})

	calls := 0
	run := func(context.Context) error {
		calls++
		return lockHeld()
	}
	err = serveWithRetry(context.Background(), paths.Socket, run)
	if !errors.Is(err, daemon.ErrAlreadyRunning) {
		t.Fatalf("error %v, want ErrAlreadyRunning", err)
	}
	if calls != 1 {
		t.Fatalf("run called %d times while a live daemon answered, want 1", calls)
	}
}
