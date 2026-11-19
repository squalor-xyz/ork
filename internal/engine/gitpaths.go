// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
)

// projectPrefix is the project directory relative to the repository root,
// with a trailing slash, or empty when root is the repository root.
func projectPrefix(ctx context.Context, root string) (string, error) {
	return projectPrefixWith(ctx, "git", root)
}

// projectPrefixWith is projectPrefix with the git binary of the caller's Deps.
func projectPrefixWith(ctx context.Context, bin, root string) (string, error) {
	res, err := clirun.Run(ctx, clirun.Call{Bin: bin, Args: []string{"-C", root, "rev-parse", "--show-prefix"}, Timeout: 30 * time.Second})
	if err != nil {
		return "", fmt.Errorf("git project prefix: %w", err)
	}
	return strings.TrimSuffix(string(res.Stdout), "\n"), nil
}
