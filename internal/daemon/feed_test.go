// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// changedItems is alphaItems with one more item, so the next read of items
// is a change.
func changedItems() source.Snapshot {
	s := alphaItems
	s.Items = source.Result[[]slicercli.Item]{Value: []slicercli.Item{{ID: "S1", Title: "one"}, {ID: "S2", Title: "two"}}}
	return s
}

// nextNotice runs feed.Next in a goroutine and returns the channel its result
// comes on, so a test can wait with a deadline.
func nextNotice(feed *Feed) <-chan noticeResult {
	out := make(chan noticeResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		n, err := feed.Next(ctx)
		out <- noticeResult{n, err}
	}()
	return out
}

type noticeResult struct {
	n   SnapshotChanged
	err error
}

func waitNotice(t *testing.T, ch <-chan noticeResult) SnapshotChanged {
	t.Helper()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Next: %v", r.err)
		}
		return r.n
	case <-time.After(5 * time.Second):
		t.Fatal("no notice within 5s")
		return SnapshotChanged{}
	}
}

// The feed's connection gets the notices of the projects it focuses, and
// nothing else.
func TestFeedDeliversTheNoticesOfTheProjectInFocus(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.src.put("alpha", alphaItems)
	e.src.put("beta", alphaItems)
	e.start(t)
	ctx := context.Background()

	req := e.dial(t)
	req.Snapshot(ctx, registry.Project{Name: "alpha"})

	feed := NewFeed(e.paths.Socket)
	t.Cleanup(func() { feed.Close() })
	if err := feed.Focus(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	e.src.put("alpha", changedItems())
	advanceFiveSeconds(e)

	n := waitNotice(t, nextNotice(feed))
	if n.Project != "alpha" || !slices.Contains(n.Parts, "items") {
		t.Fatalf("notice %+v, want items of alpha", n)
	}
}

// A new focus replaces the old one: the next notice comes from the project
// now in focus.
func TestFeedFocusMovesTheNoticesToTheNewProject(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.src.put("alpha", alphaItems)
	e.src.put("beta", alphaItems)
	e.start(t)
	ctx := context.Background()

	req := e.dial(t)
	req.Snapshot(ctx, registry.Project{Name: "alpha"})
	req.Snapshot(ctx, registry.Project{Name: "beta"})

	feed := NewFeed(e.paths.Socket)
	t.Cleanup(func() { feed.Close() })
	if err := feed.Focus(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := feed.Focus(ctx, []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	settle() // the daemon drops alpha's focus when it sees the old connection close
	alphaReads := e.src.count("items alpha")
	e.src.put("beta", changedItems())
	advanceFiveSeconds(e)
	if n := e.src.count("items alpha"); n != alphaReads {
		t.Fatalf("alpha's items were read %d more times after focus moved to beta, want none", n-alphaReads)
	}

	n := waitNotice(t, nextNotice(feed))
	if n.Project != "beta" {
		t.Fatalf("notice for %q after focus moved to beta", n.Project)
	}
}

// Focus does not wait for a notice, and a Next that is already waiting goes
// on with the connection Focus made.
func TestFocusDoesNotWaitForANoticeAndAWaitingNextMovesOn(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.src.put("alpha", alphaItems)
	e.src.put("beta", alphaItems)
	e.start(t)
	ctx := context.Background()

	feed := NewFeed(e.paths.Socket)
	t.Cleanup(func() { feed.Close() })
	if err := feed.Focus(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	waiting := nextNotice(feed) // nothing changes yet, so this waits
	settle()

	focused := make(chan error, 1)
	go func() { focused <- feed.Focus(ctx, []string{"beta"}) }()
	select {
	case err := <-focused:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Focus waited for a notice")
	}

	req := e.dial(t)
	req.Snapshot(ctx, registry.Project{Name: "beta"})
	e.src.put("beta", changedItems())
	advanceFiveSeconds(e)

	n := waitNotice(t, waiting)
	if n.Project != "beta" {
		t.Fatalf("waiting Next returned %q, want the notice of beta", n.Project)
	}
}

func TestFeedFocusFailsWhenNoDaemonAnswers(t *testing.T) {
	e := newEnv(t)
	feed := NewFeed(e.paths.Socket) // no daemon was started
	t.Cleanup(func() { feed.Close() })
	if err := feed.Focus(context.Background(), []string{"alpha"}); err == nil {
		t.Fatal("Focus with no daemon succeeded")
	}
}

func TestFeedNextBeforeFocusAndAfterCloseFails(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.start(t)
	ctx := context.Background()

	feed := NewFeed(e.paths.Socket)
	if _, err := feed.Next(ctx); !errors.Is(err, ErrNoFocus) {
		t.Fatalf("Next before Focus: %v, want ErrNoFocus", err)
	}
	if err := feed.Focus(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	waiting := nextNotice(feed)
	settle()
	if err := feed.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-waiting:
		if !errors.Is(r.err, ErrFeedClosed) {
			t.Fatalf("Next after Close: %v, want ErrFeedClosed", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Next did not return after Close")
	}
}

func TestFocusReusesNoticeConnection(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.start(t)
	f := NewFeed(e.paths.Socket)
	defer f.Close()
	if err := f.Focus(context.Background(), []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	first := f.conn
	if err := f.Focus(context.Background(), []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	if f.conn != first {
		t.Fatal("focus change dialed another notice connection")
	}
}

func TestRemovedProjectDoesNotDisableFeed(t *testing.T) {
	e := newEnv(t)
	twoProjects(t, e)
	e.start(t)
	f := NewFeed(e.paths.Socket)
	defer f.Close()
	ctx := context.Background()
	req := e.dial(t)
	if err := f.Focus(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := req.RemoveProject(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := f.Focus(ctx, []string{"alpha"}); err != nil {
		t.Fatalf("removed project disabled feed: %v", err)
	}
	if err := f.Focus(context.Background(), []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	req.Snapshot(context.Background(), registry.Project{Name: "beta"})
	e.src.put("beta", changedItems())
	advanceFiveSeconds(e)
	if n := waitNotice(t, nextNotice(f)); n.Project != "beta" {
		t.Fatalf("notice %+v", n)
	}
}
