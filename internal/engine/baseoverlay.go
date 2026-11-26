// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// baseOverlayLimit caps the files the reviewer's Base overlays section lists.
const baseOverlayLimit = 20

const baseOverlayVetTimeout = 2 * time.Minute

// baseOverlaySection checks base overlays for changed non-test Go files (S554,
// S593, S596), offering test commands unless vet exits non-zero. Per-file commands
// run tests with only that file reverted to the merge base; when none builds,
// it tries reverting all written files together. The reviewer can confirm
// that a test the slice added fails without the product change, with no write
// to the tree. ork writes the base copy and the overlay JSON into tmpDir, the
// reviewer's process temp directory. Like the decision paths check, a failure
// is advisory: the section says the check is incomplete and never pauses the
// run. It returns nil when no Go file changed.
func (e *Engine) baseOverlaySection(ctx context.Context, rec runrec.Record, tmpDir string, change branchChangeResult) *BriefSection {
	const title = "Base overlays"
	mergeBase, changed, err := change.MergeBase, change.Paths, change.Err
	if err == nil && mergeBase == "" {
		err = errors.New("the merge base is empty")
	}
	if err != nil {
		return &BriefSection{Title: title, Body: "Check incomplete: the branch diff against the base could not be read. Trace the base version of each changed file yourself with `git show` (`git -C " + ShellWord(rec.Worktree) + " show MERGEBASE:PATH`); this does not block the run."}
	}
	var goFiles []string
	otherChanged := false
	for _, p := range changed {
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			goFiles = append(goFiles, p)
		} else {
			otherChanged = true
		}
	}
	slices.Sort(goFiles)
	goFiles = slices.Compact(goFiles)
	if len(goFiles) == 0 {
		return nil
	}
	leftOut := 0
	if len(goFiles) > baseOverlayLimit {
		leftOut = len(goFiles) - baseOverlayLimit
		goFiles = goFiles[:baseOverlayLimit]
	}

	var entries, unread []string
	replacements := make(map[string]string)
	var packages []string
	commands := 0
	for _, p := range goFiles {
		isNew, key, replacement, err := e.writeBaseOverlay(ctx, rec, tmpDir, mergeBase, p, len(entries)+1)
		if err != nil {
			unread = append(unread, "`"+p+"`")
			continue
		}
		replacements[key] = replacement
		packages = append(packages, packageArg(p))
		n := len(entries) + 1
		overlayFile := filepath.Join(tmpDir, fmt.Sprintf("overlay-%d.json", n))
		line := "- `" + p + "`"
		if isNew {
			line += " (new in this slice; the overlay hides it)"
		}
		if message := e.overlayBuildError(ctx, rec, overlayFile, packageArg(p)); message != "" {
			entries = append(entries, line+": does not build: "+message+". Trace its base version with `git -C "+ShellWord(rec.Worktree)+" show "+ShellWord(mergeBase+":"+p)+"` instead.")
			continue
		}
		command := fmt.Sprintf("go -C %s test -overlay %s %s", ShellWord(rec.Worktree), ShellWord(overlayFile), ShellWord(packageArg(p)))
		entries = append(entries, line+": `"+command+"`")
		commands++
	}
	if len(replacements) >= 2 && commands == 0 {
		overlayFile := filepath.Join(tmpDir, "overlay-all.json")
		doc, err := json.Marshal(map[string]map[string]string{"Replace": replacements})
		if err == nil {
			err = os.WriteFile(overlayFile, append(doc, '\n'), 0o600)
		}
		line := fmt.Sprintf("- All changed Go files at once (%d files)", len(replacements))
		if err != nil {
			entries = append(entries, line+": Check incomplete: the combined overlay could not be written. Trace the base versions with `git show` instead.")
		} else {
			slices.Sort(packages)
			packages = slices.Compact(packages)
			if message := e.overlayBuildError(ctx, rec, overlayFile, packages...); message != "" {
				entries = append(entries, line+": does not build: "+message+". Trace the base versions with `git show` instead.")
			} else {
				command := "go -C " + ShellWord(rec.Worktree) + " test -overlay " + ShellWord(overlayFile)
				for _, pkg := range packages {
					command += " " + ShellWord(pkg)
				}
				entries = append(entries, line+": `"+command+"`. Because no single-file overlay builds, this one reverts every file listed above together. Add `-run NAME` before the package list, one test per command, and never join test names in one `-run` pattern. A command changes nothing in the tree.")
			}
		}
	}

	var body strings.Builder
	if commands > 0 {
		body.WriteString("Each command runs one package's tests with only that file reverted to the base version (the merge base with the main checkout's branch). ork wrote the base copies and the overlay files under your temp directory. A test this slice added for that file should fail with the file reverted. To run one test, add `-run NAME` before the package in a command. Run one test per command and never join test names in one `-run` pattern: a pipe makes the command compound and it is denied as multiple operations. A command changes nothing in the tree.\n\n")
	}
	if len(entries) > 0 {
		body.WriteString(strings.Join(entries, "\n"))
		body.WriteString("\n")
	}
	if leftOut > 0 {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		noun := "files were"
		if leftOut == 1 {
			noun = "file was"
		}
		fmt.Fprintf(&body, "%d more changed Go %s left out (ork lists at most %d). Trace the rest yourself with `git show`.\n", leftOut, noun, baseOverlayLimit)
	}
	if len(unread) > 0 {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString("Check incomplete: the base version of " + strings.Join(unread, ", ") + " could not be read. Trace it yourself with `git show` (`git -C " + ShellWord(rec.Worktree) + " show " + ShellWord(mergeBase) + ":PATH`); this does not block the run.\n")
	}
	if otherChanged {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString("The overlays cover non-test Go files only; other changed files are not overlaid. To read the base version of any other changed file, run `git -C " + ShellWord(rec.Worktree) + " show " + ShellWord(mergeBase) + ":PATH`. `git grep` is not available on the read-only list.\n")
	}
	return &BriefSection{Title: title, Body: strings.TrimRight(body.String(), "\n")}
}

// overlayBuildError is advisory: only a vet process that exits non-zero
// replaces the test command. Failure to start or finish vet leaves it intact.
func (e *Engine) overlayBuildError(ctx context.Context, rec runrec.Record, overlayFile string, pkgs ...string) string {
	result, err := clirun.Run(ctx, clirun.Call{
		Bin: e.d.goBin(), Args: append([]string{"-C", rec.Worktree, "vet", "-overlay", overlayFile}, pkgs...),
		Timeout: baseOverlayVetTimeout,
	})
	var exit *clirun.ExitError
	if !errors.As(err, &exit) {
		return ""
	}
	output := strings.TrimSpace(string(result.Stderr))
	if output == "" {
		output = strings.TrimSpace(string(result.Stdout))
	}
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.ReplaceAll(line, "|", "/")
		runes := []rune(line)
		if len(runes) > 200 {
			line = string(runes[:200])
		}
		lines = append(lines, line)
		if len(lines) == 3 {
			break
		}
	}
	if len(lines) == 0 {
		return fmt.Sprintf("go vet exit %d", result.ExitCode)
	}
	return strings.Join(lines, "; ")
}

// writeBaseOverlay writes the base copy of p (unless the base lacks it) and
// overlay-N.json. It reports whether p is new in the slice and the Replace key
// and value. A path that is not in the base maps to "", which `go` reads as a
// file that does not exist.
func (e *Engine) writeBaseOverlay(ctx context.Context, rec runrec.Record, tmpDir, mergeBase, p string, n int) (bool, string, string, error) {
	if !filepath.IsLocal(p) || path.IsAbs(p) {
		return false, "", "", fmt.Errorf("path %q is not a plain relative path", p)
	}
	replacement := ""
	isNew := false
	content, err := e.gitRead(ctx, rec.Worktree, "show", mergeBase+":"+p)
	switch {
	case err == nil:
		replacement = filepath.Join(tmpDir, "base", filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(replacement), 0o700); err != nil {
			return false, "", "", err
		}
		if err := os.WriteFile(replacement, []byte(content), 0o600); err != nil {
			return false, "", "", err
		}
	case notInBase(err):
		isNew = true
	default:
		return false, "", "", err
	}
	key := filepath.Join(rec.Worktree, filepath.FromSlash(p))
	doc, err := json.Marshal(map[string]map[string]string{"Replace": {key: replacement}})
	if err != nil {
		return false, "", "", err
	}
	return isNew, key, replacement, os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("overlay-%d.json", n)), append(doc, '\n'), 0o600)
}

// notInBase reports whether err is git saying the path is absent from the
// commit, as against any other failure of `git show`.
func notInBase(err error) bool {
	var exit *clirun.ExitError
	if !errors.As(err, &exit) || exit.Result.ExitCode != 128 {
		return false
	}
	stderr := string(exit.Result.Stderr)
	return strings.Contains(stderr, "does not exist in") || strings.Contains(stderr, "exists on disk, but not in")
}

// packageArg is the go package argument for the directory of file p.
func packageArg(p string) string {
	dir := path.Dir(p)
	if dir == "." {
		return "."
	}
	return "./" + dir
}
