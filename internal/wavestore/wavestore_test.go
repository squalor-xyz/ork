// SPDX-License-Identifier: GPL-3.0-or-later

package wavestore

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func wave(project, name string, items ...string) Wave {
	return Wave{Project: project, Name: name, Items: items, CreatedAt: t0, UpdatedAt: t0.Add(time.Minute)}
}

func TestLoadMissingFileIsEmptyAndCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	missingDir := filepath.Join(dir, "config", "ork")
	path := filepath.Join(missingDir, "waves.json")
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != Version || f.Waves == nil || len(f.Waves) != 0 {
		t.Fatalf("empty file: %+v", f)
	}
	if _, err := os.Stat(missingDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load created %s: %v", missingDir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("directory after Load: %v, %v", entries, err)
	}
}

func TestSaveLoadRoundTripKeepsOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ork", "waves.json")
	want := &File{Version: Version, Waves: []Wave{
		wave("ork", "zeta", "S69", "S50", "S11"),
		wave("ork", "alpha", "S2"),
		wave("other", "mid", "S7", "S3"),
	}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, want)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(raw), "{\n  \"version\": 1,") || !strings.HasSuffix(string(raw), "}\n") {
		t.Fatalf("file text: %q, %v", raw, err)
	}
}

func TestSameNameInTwoProjectsAreTwoWaves(t *testing.T) {
	f := &File{Version: Version}
	if _, err := f.Put(wave("ork", "pair-a", "S1"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Put(wave("other", "pair-a", "S9"), false); err != nil {
		t.Fatal(err)
	}
	if len(f.Waves) != 2 {
		t.Fatalf("waves: %+v", f.Waves)
	}
	if w, ok := f.Find("other", "pair-a"); !ok || !reflect.DeepEqual(w.Items, []string{"S9"}) {
		t.Fatalf("Find other: %+v, %v", w, ok)
	}
	if w, ok := f.Find("ork", "pair-a"); !ok || !reflect.DeepEqual(w.Items, []string{"S1"}) {
		t.Fatalf("Find ork: %+v, %v", w, ok)
	}
	if _, ok := f.Find("third", "pair-a"); ok {
		t.Fatal("found a wave of a third project")
	}
	if !f.Remove("ork", "pair-a") || len(f.Waves) != 1 || f.Waves[0].Project != "other" {
		t.Fatalf("Remove: %+v", f.Waves)
	}
	if f.Remove("ork", "pair-a") {
		t.Fatal("removed twice")
	}
	// The same pair twice in one file is not a valid file.
	dup := &File{Version: Version, Waves: []Wave{wave("ork", "a", "S1"), wave("ork", "a", "S2")}}
	if err := dup.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate pair: %v", err)
	}
}

func TestPutDuplicateNeedsReplaceAndKeepsPositionAndCreatedAt(t *testing.T) {
	f := &File{Version: Version}
	first := wave("ork", "a", "S1")
	if _, err := f.Put(first, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Put(wave("ork", "b", "S2"), false); err != nil {
		t.Fatal(err)
	}
	next := wave("ork", "a", "S3", "S4")
	next.CreatedAt = t0.Add(time.Hour)
	next.UpdatedAt = t0.Add(2 * time.Hour)
	if _, err := f.Put(next, false); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("no replace: %v", err)
	}
	if !reflect.DeepEqual(f.Waves[0].Items, []string{"S1"}) {
		t.Fatalf("refused Put changed the file: %+v", f.Waves)
	}
	got, err := f.Put(next, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.CreatedAt != first.CreatedAt || got.UpdatedAt != next.UpdatedAt || !reflect.DeepEqual(got.Items, []string{"S3", "S4"}) {
		t.Fatalf("replaced: %+v", got)
	}
	if f.Waves[0].Name != "a" || f.Waves[1].Name != "b" || !reflect.DeepEqual(f.Waves[0], got) {
		t.Fatalf("order after replace: %+v", f.Waves)
	}
}

func TestLoadRefusesUnknownKeysAndNewerVersion(t *testing.T) {
	cases := []struct{ name, raw, want string }{
		{"newer version", `{"version":2,"waves":[]}`, "version 2"},
		{"version zero", `{"waves":[]}`, "version 0"},
		{"unknown top key", `{"version":1,"waves":[],"extra":true}`, "extra"},
		{"unknown wave key", `{"version":1,"waves":[{"project":"ork","name":"a","items":["S1"],"created_at":"2026-11-01T12:00:00Z","updated_at":"2026-11-01T12:00:00Z","status":"done"}]}`, "status"},
		{"trailing data", `{"version":1,"waves":[]} {}`, "trailing"},
		{"not json", `{`, "waves.json"},
		{"bad name", `{"version":1,"waves":[{"project":"ork","name":"Bad Name","items":["S1"],"created_at":"2026-11-01T12:00:00Z","updated_at":"2026-11-01T12:00:00Z"}]}`, "name"},
		{"empty project", `{"version":1,"waves":[{"project":"","name":"a","items":["S1"],"created_at":"2026-11-01T12:00:00Z","updated_at":"2026-11-01T12:00:00Z"}]}`, "project"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "waves.json")
			if err := os.WriteFile(path, []byte(c.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			f, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Load = %+v, %v; want an error naming %q", f, err, c.want)
			}
			if !strings.Contains(err.Error(), path) {
				t.Fatalf("error does not name the file: %v", err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "waves.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"waves":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var ve *VersionError
	if _, err := Load(path); !errors.As(err, &ve) || ve.Got != 2 {
		t.Fatalf("version error: %v", err)
	}
}

func TestSaveIsAtomicAndWritesTheModeFromNWQ5(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "waves.json")
	old := &File{Version: Version, Waves: []Wave{wave("ork", "old", "S1")}}
	if err := Save(path, old); err != nil {
		t.Fatal(err)
	}
	// NW-Q5, owner 2026-11-07: mode 0600 (the registry's 0644 is not used).
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v, %v", info, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// The data goes to a temporary file in the same directory first.
	var tempDir, tempBase string
	orig := writeTemp
	t.Cleanup(func() { writeTemp = orig })
	writeTemp = func(f *os.File, data []byte) error {
		tempDir, tempBase = filepath.Dir(f.Name()), filepath.Base(f.Name())
		if got, err := os.ReadFile(path); err != nil || string(got) != string(before) {
			t.Errorf("target changed before the rename: %q, %v", got, err)
		}
		return orig(f, data)
	}
	next := &File{Version: Version, Waves: []Wave{wave("ork", "new", "S2")}}
	if err := Save(path, next); err != nil {
		t.Fatal(err)
	}
	writeTemp = orig
	if tempDir != dir || tempBase == "waves.json" {
		t.Fatalf("temporary file %s in %s", tempBase, tempDir)
	}
	if got, err := Load(path); err != nil || got.Waves[0].Name != "new" {
		t.Fatalf("after replace: %+v, %v", got, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode after replace: %v, %v", info, err)
	}

	// A write failure leaves the old file and no temporary file.
	if err := Save(path, old); err != nil {
		t.Fatal(err)
	}
	before, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeTemp = func(f *os.File, data []byte) error {
		f.Write(data[:len(data)/2])
		return errors.New("disk full")
	}
	if err := Save(path, next); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Save: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(before) {
		t.Fatalf("old file after a failed write: %q, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "waves.json" {
		t.Fatalf("directory after a failed write: %v, %v", entries, err)
	}

	// An invalid file is refused before anything is written.
	writeTemp = orig
	bad := &File{Version: Version, Waves: []Wave{wave("ork", "Bad Name", "S1")}}
	if err := Save(path, bad); err == nil {
		t.Fatal("saved an invalid name")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(before) {
		t.Fatalf("old file after a refused save: %q, %v", got, err)
	}
}

func TestNameRulesFromNWQ2(t *testing.T) {
	// NW-Q2, owner 2026-11-07: 1 to 32 characters of [a-z0-9._-], starting with
	// a letter or digit, never starting with "wave-"; no repeated item, at most
	// 20 items.
	names := []struct {
		name string
		ok   bool
	}{
		{"a", true},
		{"0", true},
		{"pair-a", true},
		{"v1.2_final-x", true},
		{strings.Repeat("a", 32), true},
		{"wave", true},
		{"waves-a", true},
		{"", false},
		{strings.Repeat("a", 33), false},
		{"Pair", false},
		{"pair a", false},
		{"pair/a", false},
		{"-a", false},
		{".a", false},
		{"_a", false},
		{"wave-a", false},
		{"wave-", false},
		{"é", false},
		{"a\n", false},
		{"a\x00", false},
	}
	for _, c := range names {
		err := ValidateName(c.name)
		if (err == nil) != c.ok {
			t.Errorf("ValidateName(%q) = %v, want ok=%v", c.name, err, c.ok)
		}
	}

	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "S" + string(rune('A'+i))
		}
		return out
	}
	items := []struct {
		name  string
		items []string
		ok    bool
	}{
		{"one", []string{"S1"}, true},
		{"twenty", ids(MaxItems), true},
		{"twenty-one", ids(MaxItems + 1), false},
		{"none", nil, false},
		{"empty list", []string{}, false},
		{"empty id", []string{"S1", ""}, false},
		{"repeat", []string{"S1", "S2", "S1"}, false},
		{"case differs", []string{"S1", "s1"}, true},
	}
	for _, c := range items {
		err := ValidateItems(c.items)
		if (err == nil) != c.ok {
			t.Errorf("ValidateItems(%s) = %v, want ok=%v", c.name, err, c.ok)
		}
	}
	if MaxItems != 20 || MaxNameLength != 32 {
		t.Fatalf("limits: %d items, %d characters", MaxItems, MaxNameLength)
	}
	if err := ValidateItems([]string{"S1", "S2", "S1"}); err == nil || !strings.Contains(err.Error(), "S1") {
		t.Fatalf("repeat error does not name the item: %v", err)
	}
}
