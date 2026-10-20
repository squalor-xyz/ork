// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"runtime/debug"
	"testing"
)

func TestBuildIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, revision, modified, want string
		ok                             bool
	}{
		{"clean", "abc", "false", "abc+modified=false", true},
		{"dirty", "abc", "true", "abc+modified=true", true},
		{"no-revision", "", "false", "release", true},
		{"no-info", "abc", "false", "release", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: tc.revision}, {Key: "vcs.modified", Value: tc.modified}}}
			if got := buildIdentity(info, tc.ok, "release"); got != tc.want {
				t.Fatalf("build identity %q, want %q", got, tc.want)
			}
		})
	}
}
