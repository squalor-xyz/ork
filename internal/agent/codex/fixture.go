// SPDX-License-Identifier: GPL-3.0-or-later

package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultFixtureDir is where candidates go: $XDG_STATE_HOME/ork/fixtures/codex,
// else ~/.local/state/ork/fixtures/codex (the daemon's state directory rule).
func DefaultFixtureDir() (string, error) {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "ork", "fixtures", "codex"), nil
}

// saveCandidate writes one directory under dir holding the command (cmd), the
// exit code (exit), and the raw stdout and stderr. Nothing is redacted here:
// redaction
// happens when the owner promotes a candidate into testdata/
// (vendor.fixture-candidates). The directory is mode 0700 and the files 0600.
func saveCandidate(dir string, round Round) (string, error) {
	if dir == "" {
		d, err := DefaultFixtureDir()
		if err != nil {
			return "", err
		}
		dir = d
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	out, err := os.MkdirTemp(dir, time.Now().UTC().Format("20060102T150405Z")+"-")
	if err != nil {
		return "", err
	}
	files := []struct {
		name string
		data []byte
	}{
		{"cmd", []byte(strings.Join(round.Command, " ") + "\n")},
		{"exit", []byte(fmt.Sprintf("%d\n", round.ExitCode))},
		{"stdout", round.Stdout},
		{"stderr", round.Stderr},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(out, f.name), f.data, 0o600); err != nil {
			return out, err
		}
	}
	return out, nil
}
