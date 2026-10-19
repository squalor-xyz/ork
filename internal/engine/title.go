// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"errors"
	"regexp"
	"strings"
)

var quoted = regexp.MustCompile(`"([^"]*)"`)

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
