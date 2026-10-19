// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/daemon"
)

func TestS17DetachedDaemonFailureIsLogged(t *testing.T) {
	paths := daemon.PathsIn(t.TempDir())
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"daemon", "run"}, Stderr: "startup: broken registry\n", Exit: 1})
	if err := startDaemonExe(context.Background(), fake.Bin("slicer"), paths); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		b, _ := os.ReadFile(filepath.Join(paths.Dir, "daemon.log"))
		if strings.Contains(string(b), "startup: broken registry") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("startup diagnostic missing from daemon.log: %q", b)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
