// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
const baseOverlayTestTimeout = 2 * time.Minute

const baseOverlayPassNote = "Passes in full with this file reverted: no test in the package fails for this change, so no test covers it."
const baseOverlayTestOnlyNote = "Every changed Go file is a test file, so a base overlay cannot isolate the product change: the tests are part of the change. Read each file's base version with `git show` instead."

// baseOverlayOlderBinaryNote opens the section when the running ork binary is
// behind main (S598, *proposed*; adj 591): that binary may have no vet step, or
// an older one, so the list may offer commands that do not build.
const baseOverlayOlderBinaryNote = "Note: an ork binary older than this slice's code made this list, so it may offer commands that do not build."

// baseOverlaySection lists base reads for changed files and checks overlays for
// non-test Go files (S554, S593, S596), offering test commands unless vet exits
// non-zero. Per-file commands run tests with only that file reverted to the
// merge base; when none builds,
// it tries reverting all written files together. The reviewer can confirm
// that a test the slice added fails without the product change, with no write
// to the tree. ork writes the base copy and the overlay JSON into tmpDir, the
// reviewer's process temp directory. Like the decision paths check, a failure
// is advisory: the section says the check is incomplete and never pauses the
// run. Other files get git show commands. It returns nil when no file changed.
func (e *Engine) baseOverlaySection(ctx context.Context, rec runrec.Record, tmpDir string, change branchChangeResult) *BriefSection {
	const title = "Base overlays"
	mergeBase, changed, err := change.MergeBase, change.Paths, change.Err
	if err == nil && mergeBase == "" {
		err = errors.New("the merge base is empty")
	}
	if err != nil {
		return &BriefSection{Title: title, Body: "Check incomplete: the branch diff against the base could not be read. Trace the base version of each changed file yourself with `git show` (`git -C " + ShellWord(rec.Worktree) + " show MERGEBASE:PATH`); this does not block the run."}
	}
	var goFiles, otherFiles []string
	hasGoTest := false
	for _, p := range changed {
		if baseOverlayTrackingPath(p) {
			continue
		}
		hasGoTest = hasGoTest || strings.HasSuffix(p, "_test.go")
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			goFiles = append(goFiles, p)
		} else {
			otherFiles = append(otherFiles, p)
		}
	}
	testOnly := len(goFiles) == 0 && hasGoTest
	slices.Sort(goFiles)
	goFiles = slices.Compact(goFiles)
	slices.Sort(otherFiles)
	otherFiles = slices.Compact(otherFiles)
	if len(goFiles) == 0 && len(otherFiles) == 0 {
		return nil
	}
	leftOut := 0
	if len(goFiles) > baseOverlayLimit {
		leftOut = len(goFiles) - baseOverlayLimit
		goFiles = goFiles[:baseOverlayLimit]
	}
	otherLeftOut := 0
	if len(otherFiles) > baseOverlayLimit {
		otherLeftOut = len(otherFiles) - baseOverlayLimit
		otherFiles = otherFiles[:baseOverlayLimit]
	}

	var entries, unread []string
	replacements := make(map[string]string)
	var packages []string
	commands := 0
	type builtOverlay struct{ file, overlay string }
	var built []builtOverlay
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
		command := fmt.Sprintf("go -C %s test -overlay %s -count=1 %s", ShellWord(rec.Worktree), ShellWord(overlayFile), ShellWord(packageArg(p)))
		entry := line + ": `" + command + "`"
		if e.overlayPackagePasses(ctx, rec, overlayFile, packageArg(p)) {
			entry += ". " + baseOverlayPassNote
		}
		entries = append(entries, entry)
		commands++
		built = append(built, builtOverlay{p, overlayFile})
	}
	var importerEntries []string
	importerLeftOut := 0
	importerIncomplete := false
	if commands > 0 {
		importers, err := e.baseOverlayImporters(ctx, rec)
		importerIncomplete = err != nil
		if err == nil {
			for _, entry := range built {
				for _, pkg := range importers[packageArg(entry.file)] {
					if len(importerEntries) >= baseOverlayLimit {
						importerLeftOut++
						continue
					}
					line := "- Importer `" + pkg + "` of `" + entry.file + "`: "
					if message := e.overlayBuildError(ctx, rec, entry.overlay, pkg); message != "" {
						line += "does not build: " + message + "."
					} else {
						line += fmt.Sprintf("`go -C %s test -overlay %s -count=1 %s`", ShellWord(rec.Worktree), ShellWord(entry.overlay), ShellWord(pkg))
					}
					importerEntries = append(importerEntries, line)
				}
			}
		}
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
				command := "go -C " + ShellWord(rec.Worktree) + " test -overlay " + ShellWord(overlayFile) + " -count=1"
				for _, pkg := range packages {
					command += " " + ShellWord(pkg)
				}
				entries = append(entries, line+": `"+command+"`. Because no single-file overlay builds, this one reverts every file listed above together. Add `-run NAME` before the package list, one test per command, and never join test names in one `-run` pattern. A command changes nothing in the tree.")
			}
		}
	}

	var body strings.Builder
	if len(goFiles) > 0 && e.d.behind(ctx, e.proj) {
		body.WriteString(baseOverlayOlderBinaryNote + "\n\n")
	}
	if commands > 0 {
		body.WriteString("Each command runs one package's tests with only that file reverted to the base version (the merge base with the main checkout's branch). ork wrote the base copies and the overlay files under your temp directory. A test this slice added for that file should fail with the file reverted. To run one test, add `-run NAME` before the package in a command. Run one test per command and never join test names in one `-run` pattern: a pipe makes the command compound and it is denied as multiple operations. A command changes nothing in the tree.\n\n")
	}
	if len(entries) > 0 {
		body.WriteString(strings.Join(entries, "\n"))
		body.WriteString("\n")
	}
	if len(importerEntries) > 0 {
		body.WriteString(strings.Join(importerEntries, "\n") + "\n")
	}
	if importerLeftOut > 0 {
		noun := "lines were"
		if importerLeftOut == 1 {
			noun = "line was"
		}
		fmt.Fprintf(&body, "\n%d more importer %s left out (ork lists at most %d).\n", importerLeftOut, noun, baseOverlayLimit)
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
	for _, p := range otherFiles {
		if !filepath.IsLocal(p) || path.IsAbs(p) {
			unread = append(unread, "`"+p+"`")
			continue
		}
		_, err := e.gitRead(ctx, rec.Worktree, "show", mergeBase+":"+p)
		switch {
		case err == nil:
			fmt.Fprintf(&body, "- Other file `%s`: `git -C %s show %s`\n", p, ShellWord(rec.Worktree), ShellWord(mergeBase+":"+p))
		case notInBase(err):
			fmt.Fprintf(&body, "- Other file `%s`: new in this slice, no base version\n", p)
		default:
			unread = append(unread, "`"+p+"`")
		}
	}
	if otherLeftOut > 0 {
		noun := "files were"
		if otherLeftOut == 1 {
			noun = "file was"
		}
		fmt.Fprintf(&body, "\n%d more changed other %s left out (ork lists at most %d of them). Trace the rest yourself with `git show`.\n", otherLeftOut, noun, baseOverlayLimit)
	}
	if len(unread) > 0 {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString("Check incomplete: the base version of " + strings.Join(unread, ", ") + " could not be read. Trace it yourself with `git show` (`git -C " + ShellWord(rec.Worktree) + " show " + ShellWord(mergeBase) + ":PATH`); this does not block the run.\n")
	}
	if importerIncomplete {
		body.WriteString("\nCheck incomplete: the packages that import the reverted files could not be listed, so only each file's own package is offered.\n")
	}
	if testOnly {
		body.WriteString("\n" + baseOverlayTestOnlyNote + "\n")
	}
	if len(otherFiles) > 0 {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString("The overlays cover non-test Go files only; other changed files are not overlaid. To read the base version of any other changed file, run `git -C " + ShellWord(rec.Worktree) + " show " + ShellWord(mergeBase) + ":PATH`. `git grep` is not available on the read-only list.\n")
	}
	return &BriefSection{Title: title, Body: strings.TrimRight(body.String(), "\n")}
}

func baseOverlayTrackingPath(p string) bool {
	return strings.HasPrefix(p, ".slicer/") || strings.HasPrefix(p, "landed-changed/")
}

// baseOverlayImporters reads direct module importers, including test imports.
// The process and its timeout are owned by clirun.Run; decoding opens no files.
func (e *Engine) baseOverlayImporters(ctx context.Context, rec runrec.Record) (map[string][]string, error) {
	result, err := clirun.Run(ctx, clirun.Call{
		Bin: e.d.goBin(), Args: []string{"-C", rec.Worktree, "list", "-e", "-json", "./..."},
		Timeout: baseOverlayVetTimeout,
	})
	if err != nil {
		return nil, err
	}
	type listedPackage struct {
		ImportPath                         string
		Module                             *struct{ Path string }
		Imports, TestImports, XTestImports []string
	}
	var packages []listedPackage
	args := make(map[string]string)
	decoder := json.NewDecoder(strings.NewReader(string(result.Stdout)))
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if pkg.Module == nil {
			continue
		}
		arg := "."
		if pkg.ImportPath != pkg.Module.Path {
			arg = "./" + strings.TrimPrefix(pkg.ImportPath, pkg.Module.Path+"/")
		}
		args[pkg.ImportPath] = arg
		packages = append(packages, pkg)
	}
	importers := make(map[string][]string)
	for _, pkg := range packages {
		for _, imports := range [][]string{pkg.Imports, pkg.TestImports, pkg.XTestImports} {
			for _, imported := range imports {
				if arg, ok := args[imported]; ok && imported != pkg.ImportPath {
					importers[arg] = append(importers[arg], args[pkg.ImportPath])
				}
			}
		}
	}
	for pkg, values := range importers {
		slices.Sort(values)
		importers[pkg] = slices.Compact(values)
	}
	return importers, nil
}

// overlayPackagePasses annotates only completed, successful test runs. Any
// failure, including an unavailable tool or deadline, leaves the entry plain.
func (e *Engine) overlayPackagePasses(ctx context.Context, rec runrec.Record, overlayFile, pkg string) bool {
	_, err := clirun.Run(ctx, clirun.Call{
		Bin: e.d.goBin(), Args: []string{"-C", rec.Worktree, "test", "-overlay", overlayFile, "-count=1", pkg},
		Timeout: baseOverlayTestTimeout,
	})
	return err == nil
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
