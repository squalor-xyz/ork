// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
)

// RunCheck runs the command with `sh -c` in dir, with a deadline. A command
// that exits non-zero or runs out of time is a result with OK false; only a
// failure to run it at all is an error.
func RunCheck(ctx context.Context, dir, command string, timeout time.Duration) (CheckOutput, error) {
	res, err := clirun.Run(ctx, clirun.Call{Bin: "sh", Args: []string{"-c", command}, Dir: dir, Timeout: timeout})
	out := string(res.Stdout) + string(res.Stderr)
	var exitErr *clirun.ExitError
	var timeoutErr *clirun.TimeoutError
	switch {
	case err == nil:
		return CheckOutput{OK: true, Output: out}, nil
	case errors.As(err, &exitErr):
		return CheckOutput{Output: out + fmt.Sprintf("\nexit %d\n", res.ExitCode)}, nil
	case errors.As(err, &timeoutErr):
		return CheckOutput{Output: out + fmt.Sprintf("\ntimed out after %v\n", timeout), TimedOut: true}, nil
	}
	return CheckOutput{}, err
}

// CheckDir is the directory the project check command runs in (*proposed*,
// S293): the project's own folder in the worktree. The worktree is a checkout
// of the whole repository, so a project that lives in a subfolder of it (a
// monorepo) has its folder below the worktree root. git names that folder
// (`git rev-parse --show-prefix` in the project path: empty at the repository
// root, otherwise "libraries/p/"). A folder that is not in the worktree is an
// error, not a fall back to the root, where the command would run in the wrong
// place.
func (e *Engine) CheckDir(ctx context.Context, projectPath, worktree string) (string, error) {
	out, err := e.gitRead(ctx, projectPath, "rev-parse", "--show-prefix")
	if err != nil {
		return "", err
	}
	prefix := strings.TrimSuffix(out, "\n")
	if prefix == "" {
		return worktree, nil
	}
	dir := filepath.Join(worktree, filepath.FromSlash(prefix))
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("the project folder %s does not exist in the worktree", dir)
	case err != nil:
		return "", err
	case !info.IsDir():
		return "", fmt.Errorf("the project folder %s is not a directory in the worktree", dir)
	}
	return dir, nil
}

// tail keeps the last n bytes of s, so a huge test log does not fill a brief.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "[...cut...]\n" + s[len(s)-n:]
}

// ScopeWarnings returns the sorted, unique changed paths outside the Files list.
// Without a list there is no scope to judge. Tracking paths are always exempt.
func ScopeWarnings(changed, allowed []string) []string {
	if len(allowed) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	for _, path := range changed {
		if path == ".slicer" || strings.HasPrefix(path, ".slicer/") {
			continue
		}
		covered := false
		for _, entry := range allowed {
			entry = strings.TrimRight(entry, "/")
			if path == entry || strings.HasPrefix(path, entry+"/") {
				covered = true
				break
			}
		}
		if !covered {
			seen[path] = true
		}
	}
	var warnings []string
	for path := range seen {
		warnings = append(warnings, path)
	}
	sort.Strings(warnings)
	return warnings
}
