// SPDX-License-Identifier: GPL-3.0-or-later

// Package models resolves a matrix row's model text of the form FAMILY@latest
// to the newest model of that family (S244, *proposed*). A text without
// @latest is a pinned name and is passed unchanged.
package models

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Suffix is what a model text ends with to ask for the newest model of its
// family.
const Suffix = "@latest"

// Spec is a parsed model text.
type Spec struct {
	Family string // the text before @latest; one * stands for the version
	Latest bool   // false for a pinned name
}

// ParseSpec reads a model text. FAMILY@latest names a family, which holds at
// most one * (a family with none is a claude alias; see Resolve). Any other
// text without an @ is a pinned name, including the empty text. The bare text
// "latest", an empty family, a family with two stars and any @ suffix other
// than @latest are errors.
func ParseSpec(text string) (Spec, error) {
	if text == "latest" {
		return Spec{}, errors.New(`model "latest" names no family; write FAMILY@latest, for example grok-*@latest`)
	}
	family, suffix, found := strings.Cut(text, "@")
	if !found {
		return Spec{}, nil
	}
	if suffix != "latest" {
		return Spec{}, fmt.Errorf("model %q: only @latest is understood, not @%s", text, suffix)
	}
	if family == "" {
		return Spec{}, fmt.Errorf("model %q names no family; write FAMILY@latest, for example grok-*@latest", text)
	}
	if strings.Count(family, "*") > 1 {
		return Spec{}, fmt.Errorf("model %q: a family holds one * for the version", text)
	}
	return Spec{Family: family, Latest: true}, nil
}

// Newest returns the name in names that matches family with the highest
// version, compared number by number (4.10 is newer than 4.9). The * of the
// family stands for digits and dots only, so a name with more text where the
// * is (grok-4.7-build-fast against grok-*) does not match. When several names
// have the same version, the first one wins.
func Newest(family string, names []string) (string, error) {
	prefix, suffix, ok := strings.Cut(family, "*")
	if !ok || strings.Contains(suffix, "*") {
		return "", fmt.Errorf("family %q needs one * for the version", family)
	}
	best, bestVersion := "", []int(nil)
	for _, name := range names {
		if len(name) <= len(prefix)+len(suffix) || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		version, ok := parseVersion(name[len(prefix) : len(name)-len(suffix)])
		if !ok {
			continue
		}
		if best == "" || compare(version, bestVersion) > 0 {
			best, bestVersion = name, version
		}
	}
	if best == "" {
		return "", fmt.Errorf("no model matches family %q", family)
	}
	return best, nil
}

// parseVersion reads digits separated by single dots.
func parseVersion(s string) ([]int, bool) {
	var version []int
	for _, part := range strings.Split(s, ".") {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return nil, false
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		version = append(version, n)
	}
	return version, true
}

// compare orders two versions number by number; a missing number counts as 0.
func compare(a, b []int) int {
	for i := 0; i < max(len(a), len(b)); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Lister returns the model names a vendor offers.
type Lister func(ctx context.Context, vendor string) ([]string, error)

// Resolve turns the model text of a row on a vendor into the model to run, and
// the text the row asked for. A pinned text comes back unchanged with no
// requested text. For FAMILY@latest the requested text is text itself. A claude
// family that is one of the CLI's aliases resolves to the alias with no list
// call; any other family takes the newest match from list.
func Resolve(ctx context.Context, vendor, text string, list Lister) (model, requested string, err error) {
	spec, err := ParseSpec(text)
	if err != nil {
		return "", "", err
	}
	if !spec.Latest {
		return text, "", nil
	}
	if vendor == "claude" && ClaudeAlias(spec.Family) {
		return spec.Family, text, nil
	}
	if !strings.Contains(spec.Family, "*") {
		return "", "", fmt.Errorf("model %q: family %q needs one * for the version", text, spec.Family)
	}
	names, err := list(ctx, vendor)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve %s: %w", text, err)
	}
	model, err = Newest(spec.Family, names)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve %s: %w", text, err)
	}
	return model, text, nil
}
