// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestNotifySetWithUnfinishedRun(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	if _, err := store.Create(runrec.Record{ID: "a-run", Project: p.Name, State: runrec.StatePaused}); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(t.TempDir(), "notify")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"name": p.Name, "notify_command": command})
	if _, err := d.handle(context.Background(), MethodProjectsSet, raw); err != nil {
		t.Fatalf("notify with unfinished run: %v", err)
	}
	f, err := registry.Load(d.registryPath())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(f.Projects[0])
	var saved map[string]any
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["notify_command"] != command {
		t.Fatalf("notify not saved with unfinished run: %s", b)
	}
}

func notifyFixture(t *testing.T, body string) (*Daemon, *runrec.Store, registry.Project, string) {
	t.Helper()
	d, store, p := runDaemon(t, nil)
	p.NotifyCommand = filepath.Join(t.TempDir(), "notify command")
	if err := os.MkdirAll(p.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.NotifyCommand, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := registry.Save(d.registryPath(), &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	return d, store, p, p.NotifyCommand + ".events"
}

const notifyCapture = `printf '%s|%s|%s|%s|%s|%s|%s|%s\n' "$ORK_EVENT" "$ORK_PROJECT" "$ORK_RUN" "$ORK_ITEM" "$ORK_STATE" "$ORK_REASON" "$ORK_NEXT" "$#" >> "$0.events"
`

func notifyRead(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func notifyAwait(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := notifyRead(path); got != "" {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("notification output missing: %s", path)
	return ""
}

func notifyRun(t *testing.T, store *runrec.Store, p registry.Project, id, state, reason string, pending *runrec.Pending) *runrec.Run {
	t.Helper()
	r, err := store.Create(runrec.Record{ID: id, Project: p.Name, Item: "S1", State: runrec.StateSetup})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Update(func(rec *runrec.Record) {
		rec.State, rec.Reason, rec.Pending = state, reason, pending
		if state == runrec.StateCommitted {
			rec.LandedAt = time.Now().UTC()
		}
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNotifyTransitions(t *testing.T) {
	for _, tc := range []struct {
		event, state, next string
		pending            *runrec.Pending
	}{
		{"question", runrec.StatePaused, "ork answer run TEXT", &runrec.Pending{Kind: runrec.PendingQuestion, Reason: "ready"}},
		{"awaiting-approval", runrec.StateAwaitingApproval, "ork approve run", nil},
		{"paused", runrec.StatePaused, "ork retry run", nil},
		{"stopped", runrec.StateStopped, "", nil},
		{"landed", runrec.StateCommitted, "", nil},
	} {
		t.Run(tc.event, func(t *testing.T) {
			_, store, p, output := notifyFixture(t, notifyCapture)
			notifyRun(t, store, p, "run", tc.state, "ready", tc.pending)
			want := strings.Join([]string{tc.event, "p", "run", "S1", tc.state, "ready", tc.next, "0"}, "|") + "\n"
			if got := notifyAwait(t, output); got != want {
				t.Fatalf("event environment = %q, want %q", got, want)
			}
		})
	}
}

func TestNotifyDeduplicatesAcrossRestart(t *testing.T) {
	for _, tc := range []struct {
		event, state string
		pending      *runrec.Pending
	}{
		{"paused", runrec.StatePaused, nil},
		{"question", runrec.StatePaused, &runrec.Pending{Kind: runrec.PendingQuestion, Reason: "ready"}},
		{"awaiting-approval", runrec.StateAwaitingApproval, nil},
		{"stopped", runrec.StateStopped, nil},
		{"landed", runrec.StateCommitted, nil},
	} {
		t.Run(tc.event, func(t *testing.T) {
			d, store, p, output := notifyFixture(t, notifyCapture)
			now := time.Now().UTC()
			store.Now = func() time.Time { return now }
			r := notifyRun(t, store, p, "run", tc.state, "ready", tc.pending)
			first := notifyAwait(t, output)
			d.runs.notifications.wg.Wait()
			firstSnapshot := r.Snapshot()
			now = now.Add(time.Second)
			for range 3 {
				if err := r.Update(func(rec *runrec.Record) { rec.Reason = "polled" }); err != nil {
					t.Fatal(err)
				}
			}
			// Replayed notices retain the occurrence's clock even if metadata changed.
			replay := r.Snapshot()
			if tc.event == "stopped" {
				replay = firstSnapshot
			}
			d.runs.notifications.changed(r, runrec.Record{State: runrec.StateSetup}, replay)
			d.runs.notifications.wg.Wait()
			if got := notifyRead(output); got != first {
				t.Fatalf("notification repeated for same-occurrence polls: %q", got)
			}
			d.closeRuns()
			deps := engine.Deps{Store: store}
			restarted := New(Config{Paths: d.cfg.Paths, RegistryPath: d.registryPath(), Engine: &deps, Source: &stubSource{}})
			t.Cleanup(restarted.cache.Close)
			t.Cleanup(restarted.closeRuns)
			r, err := store.Open("run")
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Update(func(rec *runrec.Record) { rec.Reason = "after restart" }); err != nil {
				t.Fatal(err)
			}
			restarted.runs.notifications.changed(r, runrec.Record{State: runrec.StateSetup}, firstSnapshot)
			restarted.runs.notifications.wg.Wait()
			if got := notifyRead(output); got != first {
				t.Fatalf("notification repeated for same occurrence after restart: %q", got)
			}
			now = now.Add(time.Second)
			if err := r.Update(func(rec *runrec.Record) { rec.State = runrec.StateSetup }); err != nil {
				t.Fatal(err)
			}
			if err := r.Update(func(rec *runrec.Record) {
				rec.State = tc.state
				rec.Reason = "ready"
				if tc.event == "landed" {
					rec.LandedAt = now
				}
			}); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for notifyRead(output) != first+first && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			restarted.runs.notifications.wg.Wait()
			restarted.closeRuns()
			if got := notifyRead(output); got != first+first {
				t.Fatalf("fresh occurrence not delivered exactly once: %q, want %q", got, first+first)
			}
		})
	}
}

func TestNotifyUnsetWritesNothing(t *testing.T) {
	for _, artifact := range []string{"metadata", "output"} {
		t.Run(artifact, func(t *testing.T) {
			d, store, p, output := notifyFixture(t, notifyCapture)
			notifyRun(t, store, p, "baseline", runrec.StatePaused, "ready", nil)
			first := notifyAwait(t, output)
			dir := filepath.Dir(store.Dir)
			p.NotifyCommand = ""
			if err := registry.Save(filepath.Join(dir, "projects.json"), &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
				t.Fatal(err)
			}
			r := notifyRun(t, store, p, "unset", runrec.StatePaused, "ready", nil)
			path := filepath.Join(r.Dir(), "run.json")
			d.runs.notifications.wg.Wait()
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if artifact == "metadata" && strings.Contains(string(after), `"notified"`) {
				t.Fatalf("unset command wrote notification metadata: %s", after)
			}
			if got := notifyRead(output); artifact == "output" && got != first {
				t.Fatalf("unset command ran: %q", got)
			}
		})
	}
}

func TestNotifySanitizesReason(t *testing.T) {
	_, store, p, output := notifyFixture(t, notifyCapture)
	notifyRun(t, store, p, "run", runrec.StatePaused, "\x1b[31mred\x1b[0m\nline\x00\x07", nil)
	if got, want := notifyAwait(t, output), "paused|p|run|S1|paused|red line|ork retry run|0\n"; got != want {
		t.Fatalf("unclean notification environment: %q, want %q", got, want)
	}
}

func TestNotifyFailures(t *testing.T) {
	for _, tc := range []struct{ name, body, failure string }{
		{"exit", "exit 7\n", "exit 7"},
		{"missing", "", "no such file"},
		{"timeout", "sleep 11\n", "timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, store, p, _ := notifyFixture(t, tc.body)
			if tc.name == "missing" {
				if err := os.Remove(p.NotifyCommand); err != nil {
					t.Fatal(err)
				}
			}
			logPath := filepath.Join(t.TempDir(), "stderr")
			log, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			old := os.Stderr
			os.Stderr = log
			defer func() { d.closeRuns(); os.Stderr = old; log.Close() }()
			started := time.Now()
			r := notifyRun(t, store, p, "run", runrec.StatePaused, "ready", nil)
			deadline := time.Now().Add(14 * time.Second)
			for notifyRead(logPath) == "" && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			got := notifyRead(logPath)
			if !strings.Contains(got, "notify run paused:") || !strings.Contains(got, tc.failure) {
				t.Fatalf("notification failure not logged: %q, want %q", got, tc.failure)
			}
			d.runs.notifications.wg.Wait()
			if got := r.Snapshot().State; got != runrec.StatePaused {
				t.Fatalf("notification failure changed run state: %s", got)
			}
			if tc.name == "timeout" && time.Since(started) < 9*time.Second {
				t.Fatal("notification timeout shorter than ten seconds")
			}
			if err := r.Update(func(rec *runrec.Record) { rec.Reason = "polled" }); err != nil {
				t.Fatal(err)
			}
			time.Sleep(50 * time.Millisecond)
			d.closeRuns()
			if got := notifyRead(logPath); strings.Count(got, "\n") != 1 {
				t.Fatalf("failure logged more than once: %q", got)
			}
		})
	}
}

func TestNotifyDoesNotBlockRunManager(t *testing.T) {
	d, store, p, output := notifyFixture(t, "printf started > \"$0.events\"\nsleep 11\n")
	started := time.Now()
	func() {
		d.runs.mu.Lock()
		defer d.runs.mu.Unlock()
		notifyRun(t, store, p, "run", runrec.StatePaused, "ready", nil)
	}()
	if time.Since(started) > time.Second {
		t.Fatal("notification blocked run update under manager lock")
	}
	notifyAwait(t, output)
	started = time.Now()
	d.closeRuns()
	if time.Since(started) > 3*time.Second {
		t.Fatal("shutdown did not cancel and join notification")
	}
}
