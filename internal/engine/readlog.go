// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// ReadLogMax is the most entries a restart brief carries from a local role's
// read log (S371, *proposed*, adj roles.local-read-log-content).
const ReadLogMax = 80

// readLogInstruction is the restart brief's line about the read log (S371,
// *proposed*, adj roles.local-read-log-reuse).
const readLogInstruction = "You already read these; do not read them again unless you are about to edit that range."

// AppendReadLog adds one entry (a line) to a role's read log. The file only
// grows during a round; ReadReadLog merges it. One short write per call.
func AppendReadLog(path, entry string) error {
	if path == "" || entry == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, entry); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadReadLog returns the log's entries merged: an entry that appears more than
// once shows at its newest place, newest last, and only the newest ReadLogMax
// remain. A missing file is an empty log.
func ReadReadLog(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var merged []string
	for _, line := range slices.Backward(strings.Split(string(b), "\n")) {
		if line == "" || slices.Contains(merged, line) {
			continue
		}
		merged = append(merged, line)
		if len(merged) == ReadLogMax {
			break
		}
	}
	slices.Reverse(merged)
	return merged, nil
}

// readLogSection is the restart brief's `## Already read` section, or "" when
// the role read nothing.
func readLogSection(path string) (string, error) {
	entries, err := ReadReadLog(path)
	if err != nil || len(entries) == 0 {
		return "", err
	}
	return "## Already read\n\n" + readLogInstruction + "\n\n" + strings.Join(entries, "\n") + "\n\n", nil
}
