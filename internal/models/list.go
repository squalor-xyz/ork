// SPDX-License-Identifier: GPL-3.0-or-later

package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
)

// listTimeout bounds one list command.
const listTimeout = 15 * time.Second

// claudeAliases are the family names the claude CLI resolves itself with
// --model (the matrix's Claude rows use these). Claude has no list command.
var claudeAliases = []string{"opus", "sonnet", "haiku"}

// ClaudeAlias reports whether family is a claude CLI alias.
func ClaudeAlias(family string) bool { return slices.Contains(claudeAliases, family) }

// ListCodex runs `bin debug models --bundled` and returns the catalog's model
// names. The bundled catalog is the one shipped with the binary; the command
// makes no model call.
func ListCodex(ctx context.Context, bin string) ([]string, error) {
	res, err := clirun.Run(ctx, clirun.Call{Bin: bin, Args: []string{"debug", "models", "--bundled"}, Timeout: listTimeout})
	if err != nil {
		return nil, err
	}
	names, err := ParseCodex(res.Stdout)
	if err != nil {
		return nil, fmt.Errorf("%s debug models --bundled: %w", bin, err)
	}
	return names, nil
}

// ParseCodex reads the catalog JSON `codex debug models` prints: an object
// whose "models" array holds one object with a "slug" for each model
// (testdata/vendors/codex/models-bundled.json). The names are in catalog order.
func ParseCodex(data []byte) ([]string, error) {
	var catalog struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	var names []string
	for _, m := range catalog.Models {
		if m.Slug != "" {
			names = append(names, m.Slug)
		}
	}
	if len(names) == 0 {
		return nil, errors.New("the catalog lists no models")
	}
	return names, nil
}

// ListGrok runs `bin models` and returns the model names it lists.
func ListGrok(ctx context.Context, bin string) ([]string, error) {
	res, err := clirun.Run(ctx, clirun.Call{Bin: bin, Args: []string{"models"}, Timeout: listTimeout})
	if err != nil {
		return nil, err
	}
	names := ParseGrok(res.Stdout)
	if len(names) == 0 {
		return nil, fmt.Errorf("%s models: the output lists no models", bin)
	}
	return names, nil
}

// ParseGrok reads the output of `grok models` (testdata/vendors/grok/models.txt).
// Each model is a bullet line, "* NAME (default)" or "- NAME": the model is the
// first word after the bullet, so the "(default)" mark is dropped. The header
// lines have no bullet and are ignored.
func ParseGrok(data []byte) []string {
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[0] == "*" || fields[0] == "-") {
			names = append(names, fields[1])
		}
	}
	return names
}
