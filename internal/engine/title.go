// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"errors"
	"regexp"
	"strings"

	"github.com/squalor-xyz/ork/internal/slicercli"
)

var quoted = regexp.MustCompile(`"([^"]*)"`)

// sliceCommitTitle uses an explicit Git title when present, otherwise the
// item's ID and full title from slicer show (S139).
func sliceCommitTitle(detail slicercli.Detail) (string, error) {
	if detail.Slice != nil {
		for _, s := range detail.Slice.Sections {
			if strings.EqualFold(s.Heading, "Git") && strings.TrimSpace(s.Body) != "" {
				return ParseGitTitle(s.Body)
			}
		}
	}
	return detail.ID + ": " + detail.Title, nil
}

// ParseGitTitle returns the commit title from the Git section of a slice: the
// first double-quoted text, else the first non-blank line.
func ParseGitTitle(body string) (string, error) {
	if m := quoted.FindStringSubmatch(body); m != nil {
		if title := strings.TrimSpace(m[1]); title != "" {
			return title, nil
		}
		return "", errors.New("the Git section holds an empty title")
	}
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line, nil
		}
	}
	return "", errors.New("the Git section is empty")
}
