// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/ipc"
)

func trackingGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func trackingWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// trackingDaemon is a daemon whose project p is a git repository on main with
// slicer files and an adj store committed.
func trackingDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(key, "Test")
	}
	for _, key := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(key, "test@example.invalid")
	}
	d, _, p := runDaemon(t, nil)
	d.runs.deps.GitBin = "git"
	trackingWrite(t, filepath.Join(p.Path, ".adjudicate.json"), "{\"store\":\"decisions\"}\n")
	trackingWrite(t, filepath.Join(p.Path, ".slicer", "index.json"), "{}\n")
	trackingGit(t, p.Path, "init", "-q", "-b", "main")
	trackingGit(t, p.Path, "add", "-A")
	trackingGit(t, p.Path, "commit", "-q", "-m", "init")
	return d, p.Path
}

func trackingCall(d *Daemon, raw string) (map[string]any, error) {
	v, err := d.handle(context.Background(), MethodTrackingCommit, json.RawMessage(raw))
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(v)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func TestTrackingCommitMethodCommitsOnTheBaseBranch(t *testing.T) {
	d, P := trackingDaemon(t)
	trackingWrite(t, filepath.Join(P, ".slicer", "slices", "S9.json"), "{}\n")
	trackingWrite(t, filepath.Join(P, ".slicer", "slices", "S8.json"), "{}\n")
	trackingWrite(t, filepath.Join(P, "decisions", "0007-x.md"), "x\n")
	out, err := trackingCall(d, `{"project":"p","leave":[".slicer/slices/S8.json"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["subject"] != "Tracking edits on main: S9; adj 7" || out["files"] != float64(2) || out["hash"] != trackingGit(t, P, "rev-parse", "--short", "HEAD") {
		t.Errorf("result %v", out)
	}
	if n := trackingGit(t, P, "rev-list", "--count", "HEAD"); n != "2" {
		t.Errorf("%s commits, want 2", n)
	}
	if left := trackingGit(t, P, "ls-files", "--others", "--exclude-standard"); left != ".slicer/slices/S8.json" {
		t.Errorf("untracked %q, want the left file", left)
	}
}

func TestTrackingCommitMethodErrorCodes(t *testing.T) {
	d, P := trackingDaemon(t)
	for _, tc := range []struct {
		name, raw, code, message string
	}{
		{"nothing", `{"project":"p"}`, CodeNothingToCommit, "no uncommitted tracking files"},
		{"other base", `{"project":"p","base":"x"}`, CodeRefused, "the main checkout is on main, not the base branch x"},
		{"unknown project", `{"project":"nope"}`, CodeNotFound, ""},
		{"no project", `{}`, CodeBadParams, ""},
		{"unknown field", `{"project":"p","message":"m"}`, CodeBadParams, ""},
		{"bad leave", `{"project":"p","leave":["README.md"]}`, CodeBadParams, "--leave README.md is not a tracking path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trackingWrite(t, filepath.Join(P, ".slicer", "index.json"), "{\"edit\":\""+tc.name+"\"}\n")
			if tc.name == "nothing" {
				trackingGit(t, P, "checkout", "-q", ".slicer/index.json")
			}
			before := trackingGit(t, P, "rev-parse", "HEAD")
			_, err := trackingCall(d, tc.raw)
			e := ipc.ErrorOf(err)
			if err == nil || e.Code != tc.code || (tc.message != "" && e.Message != tc.message) {
				t.Fatalf("error %v, want code %s message %q", err, tc.code, tc.message)
			}
			if after := trackingGit(t, P, "rev-parse", "HEAD"); after != before {
				t.Errorf("HEAD moved to %s", after)
			}
		})
	}
}

func TestTrackingCommitMethodWaitsForTheSpecLock(t *testing.T) {
	d, P := trackingDaemon(t)
	trackingWrite(t, filepath.Join(P, ".slicer", "index.json"), "{\"edit\":1}\n")
	d.runs.specMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := trackingCall(d, `{"project":"p"}`)
		done <- err
	}()
	select {
	case err := <-done:
		d.runs.specMu.Unlock()
		t.Fatalf("committed while a spec answer held the lock (error %v)", err)
	case <-time.After(200 * time.Millisecond):
	}
	d.runs.specMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
