// SPDX-License-Identifier: GPL-3.0-or-later

package models

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "vendors", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseSpecReadsFamilyAtLatest(t *testing.T) {
	got, err := ParseSpec("gpt-*-sol@latest")
	if err != nil || !got.Latest || got.Family != "gpt-*-sol" {
		t.Fatalf("gpt-*-sol@latest: %+v, %v", got, err)
	}
	for _, pinned := range []string{"opus", "gpt-6.1-sol", "", "gpt-*-sol"} {
		got, err := ParseSpec(pinned)
		if err != nil || got.Latest {
			t.Errorf("%q must be a pinned name: %+v, %v", pinned, got, err)
		}
	}
	for text, want := range map[string]string{
		"latest":           `model "latest" names no family; write FAMILY@latest, for example grok-*@latest`,
		"@latest":          "names no family",
		"a*b*@latest":      "one *",
		"gpt-*-sol@newest": "@newest",
	} {
		_, err := ParseSpec(text)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want one containing %q", text, err, want)
		}
	}
}

func TestNewestComparesVersionsNumberByNumber(t *testing.T) {
	// grok-+99 and grok-4..9 have text where the version goes that is not
	// digits and dots, so they are not versions.
	names := []string{"grok-4.5", "grok-4.10", "grok-4.9", "grok-4.7-build-fast", "ollama-local-x", "grok-+99", "grok-4..99", "grok-4."}
	got, err := Newest("grok-*", names)
	if err != nil || got != "grok-4.10" {
		t.Fatalf("Newest = %q, %v; want grok-4.10", got, err)
	}
	// A whole number is older than the same number with a minor part.
	if got, err := Newest("gpt-*-sol", []string{"gpt-6-sol", "gpt-6.1-sol", "gpt-5.6-sol", "gpt-6.1-terra"}); err != nil || got != "gpt-6.1-sol" {
		t.Errorf("gpt-*-sol: %q, %v", got, err)
	}
	_, err = Newest("claude-*", names)
	if err == nil || !strings.Contains(err.Error(), "claude-*") {
		t.Errorf("no match: error %v must name the family", err)
	}
}

func TestListGrokReadsTheFirstWordOfEachLine(t *testing.T) {
	got := ParseGrok(fixture(t, "grok/models.txt"))
	want := []string{"grok-4.7", "grok-4.7-build-fast", "grok-4.6", "grok-4.5", "studio-local"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseGrok = %q, want %q", got, want)
	}
	if newest, err := Newest("grok-*", got); err != nil || newest != "grok-4.7" {
		t.Errorf("newest grok: %q, %v", newest, err)
	}
}

func TestListCodexReadsTheCatalog(t *testing.T) {
	got, err := ParseCodex(fixture(t, "codex/models-bundled.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 12 || got[0] != "gpt-6-astra" || got[1] != "gpt-6.1-sol" || got[11] != "codex-auto-review" {
		t.Fatalf("ParseCodex = %q", got)
	}
	if newest, err := Newest("gpt-*-sol", got); err != nil || newest != "gpt-6.1-sol" {
		t.Errorf("newest sol: %q, %v", newest, err)
	}
	if _, err := ParseCodex([]byte(`{"models":[]}`)); err == nil {
		t.Error("an empty catalog must be an error")
	}
	if _, err := ParseCodex([]byte(`not json`)); err == nil {
		t.Error("bad JSON must be an error")
	}
}

// listerScript is a stand-in for a vendor CLI: it records its arguments and
// prints a capture.
func listerScript(t *testing.T, capture string, exit int) (bin, args string) {
	t.Helper()
	dir := t.TempDir()
	args = filepath.Join(dir, "args")
	bin = filepath.Join(dir, "vendor")
	script := "#!/bin/sh\nprintf '%s' \"$*\" > " + args + "\n"
	if exit != 0 {
		script += "echo broken >&2\nexit 1\n"
	} else {
		script += "cat " + capture + "\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, args
}

func TestListersRunTheListCommand(t *testing.T) {
	ctx := context.Background()
	grok, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "vendors", "grok", "models.txt"))
	codex, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "vendors", "codex", "models-bundled.json"))

	bin, args := listerScript(t, grok, 0)
	names, err := ListGrok(ctx, bin)
	if err != nil || len(names) != 5 {
		t.Fatalf("ListGrok: %q, %v", names, err)
	}
	if b, _ := os.ReadFile(args); string(b) != "models" {
		t.Errorf("grok ran %q, want models", b)
	}

	bin, args = listerScript(t, codex, 0)
	names, err = ListCodex(ctx, bin)
	if err != nil || len(names) != 12 {
		t.Fatalf("ListCodex: %q, %v", names, err)
	}
	if b, _ := os.ReadFile(args); string(b) != "debug models --bundled" {
		t.Errorf("codex ran %q, want debug models --bundled", b)
	}

	bin, _ = listerScript(t, codex, 1)
	_, err = ListCodex(ctx, bin)
	if err == nil || !strings.Contains(err.Error(), "exit 1") || !strings.Contains(err.Error(), "debug models --bundled") {
		t.Errorf("a failing list must name the command and its exit: %v", err)
	}
}

func TestClaudeAliasResolvesWithoutAListCall(t *testing.T) {
	list := func(context.Context, string) ([]string, error) {
		t.Error("a claude alias must not run a list command")
		return nil, errors.New("called")
	}
	for _, alias := range []string{"opus", "sonnet", "haiku"} {
		model, requested, err := Resolve(context.Background(), "claude", alias+"@latest", list)
		if err != nil || model != alias || requested != alias+"@latest" {
			t.Errorf("%s@latest: %q, %q, %v", alias, model, requested, err)
		}
	}
	// A pinned name is passed unchanged and has no requested text.
	model, requested, err := Resolve(context.Background(), "claude", "opus", list)
	if err != nil || model != "opus" || requested != "" {
		t.Errorf("pinned: %q, %q, %v", model, requested, err)
	}
	// Only claude has aliases.
	if _, _, err := Resolve(context.Background(), "codex", "opus@latest", func(context.Context, string) ([]string, error) { return []string{"gpt-6.1-sol"}, nil }); err == nil {
		t.Error("opus@latest on codex must be an error")
	}
}

func TestResolveUsesTheVendorList(t *testing.T) {
	var asked []string
	list := func(_ context.Context, vendor string) ([]string, error) {
		asked = append(asked, vendor)
		return []string{"gpt-6.1-sol", "gpt-6.2-sol", "gpt-5.5"}, nil
	}
	model, requested, err := Resolve(context.Background(), "codex", "gpt-*-sol@latest", list)
	if err != nil || model != "gpt-6.2-sol" || requested != "gpt-*-sol@latest" || !reflect.DeepEqual(asked, []string{"codex"}) {
		t.Fatalf("Resolve: %q, %q, %v, asked %v", model, requested, err, asked)
	}
	_, _, err = Resolve(context.Background(), "codex", "gpt-*-sol@latest", func(context.Context, string) ([]string, error) {
		return nil, errors.New("codex debug models --bundled: exit 1")
	})
	if err == nil || !strings.Contains(err.Error(), "codex debug models --bundled: exit 1") || !strings.Contains(err.Error(), "gpt-*-sol@latest") {
		t.Errorf("a failed list must name the command, its exit and the family: %v", err)
	}
	_, _, err = Resolve(context.Background(), "codex", "gpt-*-luna@latest", list)
	if err == nil || !strings.Contains(err.Error(), "gpt-*-luna") {
		t.Errorf("no match must name the family: %v", err)
	}
}
