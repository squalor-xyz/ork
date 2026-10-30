// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// This fake captures the outline while promote can still read it.
func newSliceCapture(t *testing.T) (Deps, NewSliceConfig, string) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$CAPTURE.calls"
case "$1" in
add) printf '%s' '{"id":"S184"}' ;;
show) printf '%s' '{"id":"S12","notes":["first","second\nnext"]}' ;;
promote) cp -p "$4" "$CAPTURE"; printf '%s' "$4" > "$CAPTURE.path"; printf '%s' '{}' ;;
esac
`
	bin := filepath.Join(dir, "slicer-fake")
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(dir, "capture")
	t.Setenv("CAPTURE", capture)
	return Deps{Slicer: slicercli.Client{Bin: bin}, Now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }}, NewSliceConfig{Project: registry.Project{Path: dir}, Title: "Title", Why: "Owner text", TempDir: dir}, capture
}
func TestFileNewSliceAddsThenPromotesAWhySection(t *testing.T) {
	d, c, p := newSliceCapture(t)
	id, existing, err := FileNewSlice(context.Background(), d, c)
	if err != nil || id != "S184" || existing {
		t.Fatalf("%s %v %v", id, existing, err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "## Title\n\n### Why\n\nOwner text\n" {
		t.Fatalf("outline %q", b)
	}
	path, _ := os.ReadFile(p + ".path")
	if _, err := os.Stat(string(path)); !os.IsNotExist(err) {
		t.Fatalf("outline remains: %v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}
func TestFileNewSliceFromANoteQuotesIt(t *testing.T) {
	d, c, p := newSliceCapture(t)
	c.Why = ""
	c.From = "S12"
	c.FromNote = 2
	_, _, err := FileNewSlice(context.Background(), d, c)
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(p + ".calls")
	if !strings.Contains(string(calls), "--discovered-from S12") || !strings.HasPrefix(string(calls), "show S12 --json --lean --root "+c.Project.Path) {
		t.Fatalf("calls %s", calls)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "> second\n> next") || !strings.Contains(string(b), "Discovered from S12.") {
		t.Fatalf("outline %s", b)
	}
}
func TestFileNewSliceRefusals(t *testing.T) {
	for _, change := range []func(*NewSliceConfig){
		func(c *NewSliceConfig) { c.Title = " " }, func(c *NewSliceConfig) { c.Title = "a\nb" }, func(c *NewSliceConfig) { c.FromNote = 1 }, func(c *NewSliceConfig) { c.FromNote = -1 }, func(c *NewSliceConfig) { c.From = "S12"; c.FromNote = 3 }, func(c *NewSliceConfig) { c.Importance = 4 }, func(c *NewSliceConfig) { c.Urgency = -1 }, func(c *NewSliceConfig) { c.Effort = 4 }, func(c *NewSliceConfig) { c.Size = "XL" },
	} {
		f := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"show"}, Prefix: true, Stdout: `{"notes":["one"]}`})
		c := NewSliceConfig{Project: registry.Project{Path: t.TempDir()}, Title: "Title", TempDir: t.TempDir()}
		change(&c)
		if _, _, err := FileNewSlice(context.Background(), Deps{Slicer: slicercli.Client{Bin: f.Bin("slicer")}}, c); err == nil {
			t.Fatalf("accepted %+v", c)
		}
		for _, call := range f.Calls() {
			if call.Args[0] != "show" {
				t.Fatalf("wrote: %v", call)
			}
		}
	}
}
func TestFileNewSliceRetryDoesNotPromoteTwice(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "retry promote"}[fail], func(t *testing.T) {
			second := `{"id":"S184","existing":true,"has_slice":true}`
			replies := []clitest.Reply{{Stdout: `{}`}}
			if fail {
				second = `{"id":"S184","existing":true}`
				replies = []clitest.Reply{{Stdout: `{"error":{"code":"io","message":"failed"}}`, Exit: 3}, {Stdout: `{}`}}
			}
			f := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Seq: []clitest.Reply{{Stdout: `{"id":"S184"}`}, {Stdout: second}}}, clitest.Route{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Seq: replies})
			c := NewSliceConfig{Project: registry.Project{Path: t.TempDir()}, Title: "Title", Why: "why", TempDir: t.TempDir()}
			d := Deps{Slicer: slicercli.Client{Bin: f.Bin("slicer")}}
			_, _, err := FileNewSlice(context.Background(), d, c)
			if fail {
				if err == nil || !strings.Contains(err.Error(), "filed S184") {
					t.Fatalf("error %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			id, existing, err := FileNewSlice(context.Background(), d, c)
			if err != nil || id != "S184" || existing == fail {
				t.Fatalf("retry %s %v %v", id, existing, err)
			}
			var adds, promotes []clitest.Call
			for _, call := range f.Calls() {
				if call.Args[0] == "add" {
					adds = append(adds, call)
				} else {
					promotes = append(promotes, call)
				}
			}
			want := 1
			if fail {
				want = 2
			}
			if len(promotes) != want || len(adds) != 2 || strings.Join(adds[0].Args, "|") != strings.Join(adds[1].Args, "|") {
				t.Fatalf("calls %v", f.Calls())
			}
		})
	}
}

func TestFileNewSliceDescriptionAndIdentity(t *testing.T) {
	d, c, p := newSliceCapture(t)
	c.Why = ""
	c.Title = "  A   title  "
	if _, _, err := FileNewSlice(context.Background(), d, c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "## A title\n\n### Why\n\nFiled in ork on 2026-10-30. No description was given.\n" {
		t.Fatalf("outline %s", b)
	}
	d.Now = func() time.Time { return time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC) }
	if _, _, err := FileNewSlice(context.Background(), d, c); err != nil {
		t.Fatal(err)
	}
	c.Title = "Different"
	FileNewSlice(context.Background(), d, c)
	c.Why = "changed"
	FileNewSlice(context.Background(), d, c)
	c.From = "S12"
	FileNewSlice(context.Background(), d, c)
	c.FromNote = 1
	FileNewSlice(context.Background(), d, c)
	c.Project.Path = filepath.Join(c.Project.Path, "other")
	FileNewSlice(context.Background(), d, c)
	calls, _ := os.ReadFile(p + ".calls")
	var keys []string
	for _, line := range strings.Split(string(calls), "\n") {
		if strings.HasPrefix(line, "add ") {
			words := strings.Fields(line)
			for i, w := range words {
				if w == "--key" {
					keys = append(keys, words[i+1])
				}
			}
		}
	}
	if len(keys) != 7 || keys[0] != keys[1] {
		t.Fatalf("unstable keys %v", keys)
	}
	seen := map[string]bool{keys[0]: true}
	for _, key := range keys[2:] {
		if seen[key] || !strings.HasPrefix(key, "ork-new-") || len(key) != 24 {
			t.Fatalf("identity %v", keys)
		}
		seen[key] = true
	}
	b, _ = os.ReadFile(p)
	if !strings.Contains(string(b), "changed") || strings.Contains(string(b), "> first") {
		t.Fatalf("owner Why precedence: %s", b)
	}
}

func TestFileNewSliceKeepsWhyHeadingsAsText(t *testing.T) {
	d, c, p := newSliceCapture(t)
	c.Why = "## Context\n### X\n# T\n#### More\nplain # text"
	if _, _, err := FileNewSlice(context.Background(), d, c); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "## Title\n\n### Why\n\n ## Context\n ### X\n # T\n #### More\nplain # text\n"
	if string(b) != want {
		t.Fatalf("heading text escaped incorrectly:\ngot %q\nwant %q", b, want)
	}
	var items, sections int
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			items++
		}
		if strings.HasPrefix(line, "### ") {
			sections++
		}
	}
	if items != 1 || sections != 1 {
		t.Fatalf("outline items=%d sections=%d", items, sections)
	}
}

func TestFileNewSliceQuotedNoteHeadingsStayText(t *testing.T) {
	d, c, p := newSliceCapture(t)
	script, err := os.ReadFile(d.Slicer.Bin)
	if err != nil {
		t.Fatal(err)
	}
	script = []byte(strings.Replace(string(script), `"first"`, `"# T\n## Context\n### X"`, 1))
	if err := os.WriteFile(d.Slicer.Bin, script, 0700); err != nil {
		t.Fatal(err)
	}
	c.Why = ""
	c.From = "S12"
	c.FromNote = 1
	if _, _, err := FileNewSlice(context.Background(), d, c); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "## Title\n\n### Why\n\n> # T\n> ## Context\n> ### X\n\nDiscovered from S12.\n"
	if string(b) != want {
		t.Fatalf("quoted note headings changed outline: %q", b)
	}
}
