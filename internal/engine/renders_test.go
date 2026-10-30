// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// The UI pass writes the changed views as text files in its renders directory
// (S121, *proposed*; adj 165). ork has no render code of its own: these tests
// play the role and check what ork stores on the approval card.

const (
	render80  = "Run engine  S04  awaiting-approval\nGate PASS (round 1)\n"
	render120 = "Run engine  S04  awaiting-approval\nGate PASS (round 1)  Tests: 12 passed, 0 failed\n"
	renderASC = "Run engine  S04  awaiting-approval\n+-------+\n"
)

// writeRenders puts the named files in the renders directory of a UI pass
// request, as the role would.
func writeRenders(t *testing.T, req Request, files map[string]string) {
	t.Helper()
	dir := filepath.Join(req.TempDir, "renders")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Error(err)
		return
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Error(err)
		}
	}
}

// renderTexts reads the text of each render the card lists, through ReadRender
// as the daemon's run.render does. Every listed render must read.
func renderTexts(t *testing.T, h *harness, rec runrec.Record) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, r := range rec.Card.Renders {
		text, err := ReadRender(h.store.RunDir(rec.ID), rec.Card.UIRound, r.Name)
		if err != nil {
			t.Errorf("render %s: %v", r.Name, err)
		}
		if r.Size != len(text) {
			t.Errorf("render %s: size %d, text %d bytes", r.Name, r.Size, len(text))
		}
		out[r.Name] = text
	}
	return out
}

func renderNames(card *runrec.ApprovalCard) []string {
	var names []string
	for _, r := range card.Renders {
		names = append(names, r.Name)
	}
	return names
}

func TestRendersProducedForChangedGoldens(t *testing.T) {
	t.Run("the brief says where and which", func(t *testing.T) {
		h := uiHarness(t, tuiFiles)
		requireState(t, h.start(), runrec.StateAwaitingApproval)
		var saw bool
		for _, req := range h.runner.requests() {
			if req.Role != RoleUI {
				continue
			}
			saw = true
			for _, text := range []string{filepath.Join(req.TempDir, "renders"), "-80x24.txt", "-120x40.txt", "-ascii.txt"} {
				if !strings.Contains(req.Brief, text) {
					t.Errorf("UI pass brief lacks %q", text)
				}
			}
		}
		if !saw {
			t.Fatal("the UI pass did not run")
		}
	})

	t.Run("the card holds what the role wrote", func(t *testing.T) {
		h := uiHarness(t, tuiFiles)
		h.runner.script = func(req Request, _ int) (Result, error) {
			if req.Role == RoleUI {
				writeRenders(t, req, map[string]string{
					"launch-80x24.txt": render80, "launch-120x40.txt": render120, "launch-ascii.txt": renderASC,
					"notes.md": "not a render", // not a .txt file
				})
				if err := os.MkdirAll(filepath.Join(req.TempDir, "renders", "sub.txt"), 0o755); err != nil {
					t.Error(err) // a directory named like a render
				}
			}
			return Result{}, nil
		}
		rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
		c := rec.Card
		if c == nil {
			t.Fatal("no card")
		}
		if want := []string{"launch-80x24.txt", "launch-120x40.txt", "launch-ascii.txt"}; !reflect.DeepEqual(renderNames(c), want) {
			t.Errorf("renders %v, want %v", renderNames(c), want)
		}
		texts := renderTexts(t, h, rec)
		for name, want := range map[string]string{"launch-80x24.txt": render80, "launch-120x40.txt": render120, "launch-ascii.txt": renderASC} {
			if texts[name] != strings.TrimRight(want, "\n") {
				t.Errorf("text of %s: %q", name, texts[name])
			}
		}
		if c.UIRound != 1 {
			t.Errorf("UIRound %d, want 1", c.UIRound)
		}
		// The files stay under the run directory (proposed path).
		path := filepath.Join(h.store.RunDir(rec.ID), "tmp", "ui-1", "renders", "launch-80x24.txt")
		if b, err := os.ReadFile(path); err != nil || string(b) != render80 {
			t.Errorf("stored render %s: %q, %v", path, b, err)
		}
	})

	t.Run("a slice without a UI pass has no renders", func(t *testing.T) {
		h := newHarness(t)
		rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
		if rec.Card == nil || len(rec.Card.Renders) != 0 || rec.Card.UIRound != 0 {
			t.Fatalf("card %+v", rec.Card)
		}
	})

	t.Run("a UI pass that wrote none says it ran", func(t *testing.T) {
		h := uiHarness(t, tuiFiles)
		rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
		if rec.Card.UIRound != 1 || len(rec.Card.Renders) != 0 {
			t.Fatalf("card %+v", rec.Card)
		}
	})

	t.Run("a rerun does not keep the earlier attempt's files", func(t *testing.T) {
		h := uiHarness(t, tuiFiles)
		var mu sync.Mutex
		runs := 0
		h.runner.script = func(req Request, _ int) (Result, error) {
			if req.Role != RoleUI {
				return Result{}, nil
			}
			mu.Lock()
			defer mu.Unlock()
			runs++
			if runs == 1 {
				writeRenders(t, req, map[string]string{"old-80x24.txt": "old"})
				return Result{Exit: ExitOK, Report: noSections}, nil
			}
			writeRenders(t, req, map[string]string{"new-80x24.txt": "new"})
			return Result{}, nil
		}
		e := h.start()
		requireState(t, e, runrec.StatePaused)
		if err := e.Retry(context.Background()); err != nil {
			t.Fatal(err)
		}
		rec := requireState(t, e, runrec.StateAwaitingApproval)
		if want := []string{"new-80x24.txt"}; !reflect.DeepEqual(renderNames(rec.Card), want) {
			t.Fatalf("renders %v, want %v", renderNames(rec.Card), want)
		}
		if got := renderTexts(t, h, rec)["new-80x24.txt"]; got != "new" {
			t.Fatalf("text %q", got)
		}
	})

	t.Run("the owner's fix round keeps the renders of the last UI pass", func(t *testing.T) {
		h := uiHarness(t, tuiFiles)
		h.runner.script = func(req Request, _ int) (Result, error) {
			if req.Role == RoleUI {
				writeRenders(t, req, map[string]string{"launch-80x24.txt": render80})
			}
			return Result{}, nil
		}
		e := h.start()
		requireState(t, e, runrec.StateAwaitingApproval)
		if err := e.Fix(context.Background(), "tidy up"); err != nil {
			t.Fatal(err)
		}
		rec := requireState(t, e, runrec.StateAwaitingApproval)
		if rec.Card.Round != 2 || rec.Card.UIRound != 1 || len(rec.Card.Renders) != 1 {
			t.Fatalf("card %+v", rec.Card)
		}
	})
}

// A render is the role's text: ork keeps it as plain text, so a terminal
// escape in it cannot reach the owner's terminal, and bounds what it keeps.
func TestRendersAreKeptAsPlainBoundedText(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	big := strings.Repeat("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n", 800) // 52 KB
	files := map[string]string{
		"a-80x24.txt": "\x1b[31mred\x1b[0m\tcol\r\nline\x07 two\x1b]0;PWNED\x07 \x1b]8;;http://x\x1b\\link\x1b[2K\n\n\n",
		"b-80x24.txt": big,
		"c-80x24.txt": "bad \xff byte",
	}
	for i := range maxRenders {
		files[string(rune('d'+i%20))+strings.Repeat("x", i)+"-80x24.txt"] = "x"
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleUI {
			writeRenders(t, req, files)
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	byName := renderTexts(t, h, rec)
	if got, want := byName["a-80x24.txt"], "red    col\nline two link"; got != want {
		t.Errorf("control characters: %q, want %q", got, want)
	}
	if got := byName["b-80x24.txt"]; len(got) > maxRenderBytes+200 || !strings.HasSuffix(got, "(cut: over 24 KiB)") {
		t.Errorf("big render: %d bytes, tail %q", len(got), got[max(len(got)-40, 0):])
	}
	if got := byName["c-80x24.txt"]; got != "bad ? byte" {
		t.Errorf("bad UTF-8: %q", got)
	}
	if len(rec.Card.Renders) != maxRenders || rec.Card.RendersDropped != len(files)-maxRenders {
		t.Errorf("%d renders kept, %d dropped; %d files written", len(rec.Card.Renders), rec.Card.RendersDropped, len(files))
	}
}

// A symlink in the renders directory is not followed: the role could point it
// at any file the daemon can read.
func TestRendersIgnoreSymlinks(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleUI {
			writeRenders(t, req, map[string]string{"real-80x24.txt": "real"})
			if err := os.Symlink(secret, filepath.Join(req.TempDir, "renders", "link-80x24.txt")); err != nil {
				t.Error(err)
			}
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if want := []string{"real-80x24.txt"}; !reflect.DeepEqual(renderNames(rec.Card), want) {
		t.Fatalf("renders %v, want %v", renderNames(rec.Card), want)
	}
	if text, err := ReadRender(h.store.RunDir(rec.ID), 1, "link-80x24.txt"); err == nil {
		t.Fatalf("a linked render was read: %q", text)
	}
}

// The renders directory is the role's own, so it may be a link too: ork does
// not read through it.
func TestRendersIgnoreASymlinkedDirectory(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	secrets := t.TempDir()
	if err := os.WriteFile(filepath.Join(secrets, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleUI {
			if err := os.Symlink(secrets, filepath.Join(req.TempDir, "renders")); err != nil {
				t.Error(err)
			}
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if rec.Card.UIRound != 1 || len(rec.Card.Renders) != 0 {
		t.Fatalf("card %+v: a linked renders directory was read", rec.Card)
	}
	if text, err := ReadRender(h.store.RunDir(rec.ID), 1, "secret.txt"); err == nil {
		t.Fatalf("a render was read through a linked directory: %q", text)
	}
}

// A file swapped for a link between the listing and the open is not read.
func TestReadRenderRefusesAFileSwappedForALink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a-80x24.txt")
	if err := os.WriteFile(path, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("%v %v", entries, err)
	}
	if text, err := readRender(dir, entries[0]); err != nil || text != "real" {
		t.Fatalf("an untouched file: %q, %v", text, err)
	}
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, path); err != nil {
		t.Fatal(err)
	}
	if text, err := readRender(dir, entries[0]); !errors.Is(err, errNotThatFile) {
		t.Fatalf("a swapped link was read: %q, %v", text, err)
	}
}

// The card lists each view's renders as 80x24, 120x40, ascii, the views by
// name, and then any other file by name (docs/dashboard.md).
func TestRendersAreOrderedByViewThenSize(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	files := map[string]string{}
	for _, name := range []string{"zeta.txt", "b-ascii.txt", "b-120x40.txt", "b-80x24.txt", "a-ascii.txt", "a-80x24.txt", "alpha-notes.txt", "b-extra-ascii.txt"} {
		files[name] = name
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleUI {
			writeRenders(t, req, files)
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	var got []string
	for _, r := range rec.Card.Renders {
		got = append(got, r.Name)
	}
	want := []string{"a-80x24.txt", "a-ascii.txt", "b-80x24.txt", "b-120x40.txt", "b-ascii.txt", "b-extra-ascii.txt", "alpha-notes.txt", "zeta.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

// ReadRender is what run.render serves: only a regular .txt file of the UI
// pass's renders directory, by its plain name.
func TestReadRenderRefusesWhatTheCardCouldNotList(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleUI {
			writeRenders(t, req, map[string]string{"a-80x24.txt": "real", "notes.md": "md"})
			dir := filepath.Join(req.TempDir, "renders")
			os.MkdirAll(filepath.Join(dir, "sub.txt"), 0o755)
			os.Symlink(secret, filepath.Join(dir, "link.txt"))
			os.WriteFile(filepath.Join(req.TempDir, "outside.txt"), []byte("outside"), 0o644)
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	runDir := h.store.RunDir(rec.ID)
	if text, err := ReadRender(runDir, 1, "a-80x24.txt"); err != nil || text != "real" {
		t.Fatalf("the listed render: %q, %v", text, err)
	}
	for _, name := range []string{"", "nope.txt", "notes.md", "sub.txt", "link.txt", "../outside.txt", "../renders/a-80x24.txt", "/etc/hosts", "a-80x24.txt/"} {
		if text, err := ReadRender(runDir, 1, name); !errors.Is(err, ErrNoRender) {
			t.Errorf("name %q: %q, %v; want ErrNoRender", name, text, err)
		}
	}
	if _, err := ReadRender(runDir, 2, "a-80x24.txt"); !errors.Is(err, ErrNoRender) {
		t.Errorf("a round with no renders: %v", err)
	}
}

// The run record holds names and sizes: the text is read on demand, so no
// snapshot or run.json carries it.
func TestRunRecordHoldsNoRenderText(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleUI {
			writeRenders(t, req, map[string]string{"launch-80x24.txt": render80})
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	b, err := os.ReadFile(filepath.Join(h.store.RunDir(rec.ID), "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "Run engine") {
		t.Errorf("run.json holds render text:\n%s", b)
	}
	var doc struct {
		Card struct {
			Renders []map[string]any `json:"renders"`
		} `json:"card"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Card.Renders) != 1 || doc.Card.Renders[0]["name"] != "launch-80x24.txt" || doc.Card.Renders[0]["size"] != float64(len(strings.TrimRight(render80, "\n"))) || len(doc.Card.Renders[0]) != 2 {
		t.Errorf("card renders in run.json: %v", doc.Card.Renders)
	}
}

// A renders directory or a render file that exists but cannot be read is not
// "wrote none": the card says why (S147). A directory the role never made is
// still just no renders.
func TestCardSaysWhyRendersCouldNotBeRead(t *testing.T) {
	// mode0 makes the path unreadable and skips the test when this user reads
	// it anyway (root), so the assertions below cannot pass vacuously.
	mode0 := func(t *testing.T, path string, probe func(string) error) {
		t.Helper()
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
		if probe(path) == nil {
			t.Skip("this user can read a mode 0 path; the unreadable case cannot be set up")
		}
	}
	readDir := func(p string) error { _, err := os.ReadDir(p); return err }
	readFile := func(p string) error { _, err := os.ReadFile(p); return err }

	for name, tc := range map[string]struct {
		script    func(t *testing.T, req Request)
		wantError bool
		wantNames []string
	}{
		"a directory with mode 0": {
			script: func(t *testing.T, req Request) {
				writeRenders(t, req, map[string]string{"a-80x24.txt": "a"})
				mode0(t, filepath.Join(req.TempDir, "renders"), readDir)
			},
			wantError: true,
		},
		"a file with mode 0": {
			script: func(t *testing.T, req Request) {
				writeRenders(t, req, map[string]string{"a-80x24.txt": "a", "b-80x24.txt": "b"})
				mode0(t, filepath.Join(req.TempDir, "renders", "b-80x24.txt"), readFile)
			},
			wantError: true,
			wantNames: []string{"a-80x24.txt"},
		},
		"no directory": {script: func(*testing.T, Request) {}},
		"an empty directory": {script: func(t *testing.T, req Request) {
			writeRenders(t, req, nil)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			h := uiHarness(t, tuiFiles)
			h.runner.script = func(req Request, _ int) (Result, error) {
				if req.Role == RoleUI {
					tc.script(t, req)
				}
				return Result{}, nil
			}
			rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
			card := rec.Card
			if card.UIRound != 1 {
				t.Fatalf("card %+v: the UI pass must have run", card)
			}
			if got := renderNames(card); !reflect.DeepEqual(got, tc.wantNames) {
				t.Errorf("renders %v, want %v", got, tc.wantNames)
			}
			if tc.wantError && !strings.Contains(card.RendersError, "permission denied") {
				t.Errorf("RendersError %q, want the reason (permission denied)", card.RendersError)
			}
			if !tc.wantError && card.RendersError != "" {
				t.Errorf("RendersError %q, want empty", card.RendersError)
			}
		})
	}
}
