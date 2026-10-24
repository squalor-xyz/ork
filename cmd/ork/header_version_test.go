// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"context"
	"runtime/debug"
	"testing"

	"github.com/squalor-xyz/ork/internal/tui"
)

func TestHeaderVersion(t *testing.T) {
	for _, tc := range []struct {
		version, revision, modified string
		ok                          bool
		want                        string
	}{
		{"v1", "abcdef0123", "true", true, "v1"}, {"dev", "abcdef0123", "true", true, "abcdef0+"},
		{"dev", "abcdef0123", "false", true, "abcdef0"}, {"dev", "abc", "", true, "abc"},
		{"dev", "", "true", true, "dev"}, {"dev", "abcdef0123", "true", false, "dev"},
	} {
		info := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: tc.revision}, {Key: "vcs.modified", Value: tc.modified}}}
		if got := headerVersion(info, tc.ok, tc.version); got != tc.want {
			t.Errorf("%+v: %q", tc, got)
		}
	}
	if got := headerVersion(nil, true, "dev"); got != "dev" {
		t.Fatal(got)
	}
}

func TestHeaderBuildsReachTUI(t *testing.T) {
	sandbox(t)
	called := false
	p := inTest(t, emptySource{}, func(cfg tui.Config) error {
		called = true
		if cfg.Version == "" || cfg.BuildID != currentBuildID() || cfg.DaemonBuild != "dev" {
			t.Fatalf("config: %+v", cfg)
		}
		return nil
	})
	var out, errOut bytes.Buffer
	if code := run(context.Background(), nil, &out, &errOut, p); code != 0 || !called {
		t.Fatalf("%d: %s", code, errOut.String())
	}
}
