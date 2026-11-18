// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

const BehindNotice = "daemon behind main: restart with ork daemon restart"

// BuildTimeout and BuildTailLines bound the self-host build step that the CLI's
// restart --rebuild and the daemon's own rebuild share (S431, *proposed*).
const (
	BuildTimeout   = 10 * time.Minute
	BuildTailLines = 20
)

// BuildError is a failed self-host build: why it failed and the last lines of
// its output.
type BuildError struct {
	Reason string   // "exit 1", "timed out" or "cancelled"
	Lines  []string // the last BuildTailLines lines of output, or the cause
}

func (e *BuildError) Error() string {
	return fmt.Sprintf("build failed (%s)\n%s", e.Reason, strings.Join(e.Lines, "\n"))
}

// RunBuild runs a project's build command in its directory through sh, with the
// ten-minute timeout and the process-group cleanup of clirun. It returns nil
// when the build exits 0. The CLI and the daemon both call it, so the build is
// one piece of code.
func RunBuild(ctx context.Context, dir, command string) *BuildError {
	res, err := clirun.Run(ctx, clirun.Call{Bin: "sh", Args: []string{"-c", command}, Dir: dir, Timeout: BuildTimeout})
	if err == nil {
		return nil
	}
	tail := strings.TrimSpace(string(res.Stdout) + "\n" + string(res.Stderr))
	if tail == "" {
		tail = err.Error()
	}
	lines := strings.Split(tail, "\n")
	if len(lines) > BuildTailLines {
		lines = lines[len(lines)-BuildTailLines:]
	}
	reason := fmt.Sprintf("exit %d", res.ExitCode)
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "timed out"
	}
	if errors.Is(err, context.Canceled) {
		reason = "cancelled"
	}
	return &BuildError{Reason: reason, Lines: lines}
}

// RebuildConfig holds the seams of the daemon's own rebuild (S431, *proposed*;
// adj 464). Every field may be nil.
type RebuildConfig struct {
	// Build runs the project's build command; nil means RunBuild.
	Build func(ctx context.Context, p registry.Project) *BuildError
	// Prepare validates the newly built executable and returns the function that
	// starts it as the successor daemon; nil means the executable of this process,
	// as the CLI's restart does (PrepareRestartExe).
	Prepare func(ctx context.Context) (func(context.Context) error, error)
	// Revision reads the revision of the executable the successor would run and
	// its path; nil means the executable of this process, read with
	// debug/buildinfo. A revision equal to the running build's means the build
	// did not replace the daemon's executable (S431 review).
	Revision func(ctx context.Context) (revision, path string, err error)
	// Poll is how often a drain looks for the last running role to end; 0 means
	// one second.
	Poll time.Duration
}

// Rebuild states, as a project's status reports them.
const (
	RebuildDraining = "draining"
	RebuildBuilding = "building"
	RebuildFailed   = "failed"
)

// RebuildStatus is the daemon's own rebuild of a project that opted in
// (S431, *proposed*; adj 464): a drain that waits for the running roles, the
// build, or the failure of the last build.
type RebuildStatus = source.Rebuild

// RebuildNotice is the message of the first project whose daemon rebuild is
// pending or failed, or "". Unlike BehindWarning it speaks while roles run: a
// drain is exactly then.
func RebuildNotice(status *StatusResult) string {
	if status == nil {
		return ""
	}
	for _, p := range status.Projects {
		if p.Rebuild != nil {
			return p.Rebuild.Message
		}
	}
	return ""
}

// rebuildFailure is the last failed build of a project, kept until main moves.
type rebuildFailure struct {
	head   string
	reason string
	lines  []string
}

// rebuildState is the daemon's rebuild bookkeeping. It has its own lock, a leaf
// lock: it is never held while another is taken.
type rebuildState struct {
	mu        sync.Mutex
	active    string // the path of the project that drains, "" when none
	state     string
	successor func(context.Context) error // starts the rebuilt daemon once this one has exited
	stop      context.CancelFunc          // ends Run
	failures  map[string]rebuildFailure   // by project path
	wake      chan struct{}               // a drain looks again when it receives; buffered by one
}

type buildEntry struct {
	selfHost, behind bool
}

// projectBuild compares the running revision with the main checkout's branch.
// A failed read is not evidence that this is ork's repository or is behind.
func (d *Daemon) projectBuild(ctx context.Context, p registry.Project) (selfHost, behind bool) {
	revision, _, _ := strings.Cut(d.cfg.BuildID, "+modified=")
	if len(revision) != 40 && len(revision) != 64 {
		return false, false
	}
	if _, err := hex.DecodeString(revision); err != nil {
		return false, false
	}
	d.buildMu.Lock()
	defer d.buildMu.Unlock()
	// Keep only registered paths, even after projects are removed or relocated.
	registered := false
	f, regErr := registry.Load(d.registryPath())
	if regErr == nil {
		paths := make(map[string]bool, len(f.Projects))
		for _, project := range f.Projects {
			paths[project.Path] = true
		}
		registered = paths[p.Path]
		for key := range d.builds {
			path, _, _ := strings.Cut(key, "\x00")
			if !paths[path] {
				delete(d.builds, key)
			}
		}
	}
	head, readErr := d.git(ctx, p, "rev-parse", "HEAD", "--symbolic-full-name", "HEAD")
	parts := strings.Fields(string(head.Stdout))
	valid := readErr == nil && len(parts) == 2 && strings.HasPrefix(parts[1], "refs/heads/")
	key := ""
	if valid {
		key = strings.Join([]string{p.Path, d.cfg.BuildID, parts[0], parts[1]}, "\x00")
		if entry, ok := d.builds[key]; ok {
			return entry.selfHost, entry.behind
		}
	}
	// A changed key, detached checkout or failed read invalidates the old entry.
	for old := range d.builds {
		if strings.HasPrefix(old, p.Path+"\x00") {
			delete(d.builds, old)
		}
	}
	if _, err := d.git(ctx, p, "cat-file", "-e", revision+"^{commit}"); err != nil {
		return false, false
	}
	if !valid {
		return true, false
	}
	behind = false
	if parts[0] != revision {
		result, err := d.git(ctx, p, "merge-base", "--is-ancestor", revision, parts[0])
		if err != nil && result.ExitCode != 1 {
			return true, false
		}
		behind = err == nil
	}
	if registered {
		if d.builds == nil {
			d.builds = make(map[string]buildEntry)
		}
		d.builds[key] = buildEntry{selfHost: true, behind: behind}
	}
	return true, behind
}

// EngineSlice reports a Files path under the code that runs a relay.
func EngineSlice(detail *slicercli.Detail) bool {
	if detail == nil || detail.Slice == nil {
		return false
	}
	for _, section := range detail.Slice.Sections {
		if !strings.EqualFold(section.Heading, "Files") {
			continue
		}
		// The project Files convention puts each path inside backticks.
		parts := strings.Split(section.Body, "`")
		for i := 1; i < len(parts); i += 2 {
			file, _, _ := strings.Cut(parts[i], ":")
			file = path.Clean(file)
			for _, dir := range []string{"internal/engine", "internal/daemon", "cmd"} {
				if file == dir || strings.HasPrefix(file, dir+"/") {
					return true
				}
			}
		}
	}
	return false
}

// EngineConflict names the two slices that cannot share a wave.
func EngineConflict(a, b string) string {
	return fmt.Sprintf("engine conflict: %s and %s touch internal/engine, internal/daemon or cmd; launch separately", a, b)
}

// BlockedMessage names blocked items and their blockers in the supplied order.
func BlockedMessage(ids []string, deps []slicercli.ItemDeps, titles map[string]string) string {
	members := make(map[string]bool, len(ids))
	for _, id := range ids {
		members[id] = true
	}
	var messages, blockedIDs, blockerIDs []string
	seenBlockers := map[string]bool{}
	titled := map[string]bool{}
	inWave := false
	for _, item := range deps {
		var blockers []string
		for _, blocker := range item.BlockedBy {
			reason := "not done"
			if blocker != item.ID && members[blocker] {
				reason = "in this wave, " + reason
				inWave = true
			}
			name := blocker
			// A blocker shared by several items shows its title the first time only.
			if title := strings.Join(strings.Fields(runrec.PlainText(titles[blocker])), " "); title != "" && !titled[blocker] {
				titled[blocker] = true
				name += ` "` + title + `"`
			}
			blockers = append(blockers, name+" ("+reason+")")
			if !seenBlockers[blocker] {
				seenBlockers[blocker] = true
				blockerIDs = append(blockerIDs, blocker)
			}
		}
		if len(blockers) > 0 {
			messages = append(messages, item.ID+" waits on "+strings.Join(blockers, ", "))
			blockedIDs = append(blockedIDs, item.ID)
		}
	}
	if len(messages) == 0 {
		return ""
	}
	next := "land " + joinAnd(blockerIDs) + " first"
	if inWave {
		next = "untick " + joinAnd(blockedIDs) + ", or " + next
	}
	return strings.Join(append(messages, next), "; ")
}

// joinAnd writes names as "A", "A and B" or "A, B and C".
func joinAnd(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// BehindWarning is available only at the daemon's idle point.
func BehindWarning(status *StatusResult) string {
	if status == nil || status.RolesRunning {
		return ""
	}
	for _, p := range status.Projects {
		if p.SelfHost && p.BehindMain {
			if p.BuildCommand != "" {
				return BehindNotice + " --rebuild"
			}
			return BehindNotice
		}
	}
	return ""
}

// executableRevision reads the vcs revision stamped in the executable of this
// process, as a successor started from that path would carry it. A file with no
// stamp has the revision "".
func executableRevision(context.Context) (revision, path string, err error) {
	path, err = os.Executable()
	if err != nil {
		return "", "", err
	}
	path = strings.TrimSuffix(path, " (deleted)")
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return "", path, err
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value, path, nil
		}
	}
	return "", path, nil
}

// rebuildKick wakes a drain that waits for the running roles to end. It never
// blocks.
func (d *Daemon) rebuildKick() {
	select {
	case d.rebuild.wake <- struct{}{}:
	default:
	}
}

// mainHead is the commit the project's main checkout is at, or "".
func (d *Daemon) mainHead(ctx context.Context, p registry.Project) string {
	res, err := d.git(ctx, p, "rev-parse", "HEAD", "--symbolic-full-name", "HEAD")
	if err != nil {
		return ""
	}
	if parts := strings.Fields(string(res.Stdout)); len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// checkRebuild starts a drain for the first project that opted in to the
// daemon's own rebuild (S431, *proposed*; adj 464, selfhost.auto-rebuild), is
// self-host, has a build command and leaves the daemon behind main. It is called
// after a landing, when the daemon starts and when a project sets auto_rebuild.
// One drain runs at a time. A project whose last build failed is left alone
// until main moves again. It starts no goroutine when there is nothing to do.
func (d *Daemon) checkRebuild() {
	d.runs.mu.Lock()
	ctx, closed := d.runs.ctx, d.runs.closed
	d.runs.mu.Unlock()
	if closed {
		return
	}
	f, err := d.loadRegistry()
	if err != nil {
		return
	}
	for _, p := range f.Projects {
		if !p.AutoRebuild || p.BuildCommand == "" {
			continue
		}
		selfHost, behind := d.projectBuild(ctx, p)
		head := ""
		if selfHost && behind {
			head = d.mainHead(ctx, p)
		}
		d.rebuild.mu.Lock()
		if !selfHost || !behind {
			delete(d.rebuild.failures, p.Path)
			d.rebuild.mu.Unlock()
			continue
		}
		if fail, ok := d.rebuild.failures[p.Path]; ok {
			if fail.head == head {
				d.rebuild.mu.Unlock()
				continue
			}
			delete(d.rebuild.failures, p.Path)
		}
		if d.rebuild.active != "" {
			d.rebuild.mu.Unlock()
			return
		}
		d.rebuild.active, d.rebuild.state = p.Path, RebuildDraining
		d.rebuild.mu.Unlock()
		go d.drain(ctx, p, head)
		return
	}
}

// drain holds every new role start, waits until no role runs, builds, checks the
// new executable and then ends the daemon with a successor to start (S431,
// *proposed*; adj 464 and adj 465). It never cancels a role: the hold only
// blocks a start, and the daemon ends only when every run is held or at rest.
// A failed build keeps this daemon and releases the hold.
func (d *Daemon) drain(ctx context.Context, p registry.Project, head string) {
	hold := d.runs.deps.Hold
	hold.Set(true)
	d.changed(p.Name, source.Runs)
	build := d.cfg.Rebuild.Build
	if build == nil {
		build = func(ctx context.Context, p registry.Project) *BuildError {
			return RunBuild(ctx, p.Path, p.BuildCommand)
		}
	}
	prepare := d.cfg.Rebuild.Prepare
	if prepare == nil {
		prepare = func(ctx context.Context) (func(context.Context) error, error) {
			exe, err := os.Executable()
			if err != nil {
				return nil, err
			}
			return PrepareRestartExe(ctx, exe, d.cfg.Paths)
		}
	}
	revision := d.cfg.Rebuild.Revision
	if revision == nil {
		revision = executableRevision
	}
	poll := d.cfg.Rebuild.Poll
	if poll <= 0 {
		poll = time.Second
	}
	tick := time.NewTicker(poll)
	defer tick.Stop()
	var start func(context.Context) error
	for {
		d.runs.mu.Lock()
		_, _, idle := d.drainStateLocked()
		closed := d.runs.closed
		d.runs.mu.Unlock()
		if closed || ctx.Err() != nil {
			d.rebuildEnd(p)
			return
		}
		if idle {
			if start == nil {
				d.rebuildSetState(RebuildBuilding)
				d.changed(p.Name, source.Runs)
				if berr := build(ctx, p); berr != nil {
					d.rebuildFail(p, head, "build failed ("+berr.Reason+")", berr.Lines)
					return
				}
				s, err := prepare(ctx)
				if err != nil {
					d.rebuildFail(p, head, "new executable not usable", []string{err.Error()})
					return
				}
				// A build that did not replace the file this daemon runs from would
				// start the same build again, and the successor would rebuild again,
				// with no end. Compare the revisions and keep this daemon.
				rev, path, rerr := revision(ctx)
				if rerr != nil {
					d.rebuildFail(p, head, "new executable not readable", []string{rerr.Error()})
					return
				}
				if running, _, _ := strings.Cut(d.cfg.BuildID, "+modified="); rev != "" && rev == running {
					d.rebuildFail(p, head, "the build did not change the daemon executable "+path, []string{"the executable still holds revision " + rev, "the build command must write the file the daemon runs from"})
					return
				}
				start = s
				d.rebuildSetState(RebuildDraining)
				// Look again: a run may have started during the build, and then the
				// daemon waits for it too.
				continue
			}
			if d.exitForRebuild(start) {
				return
			}
		}
		select {
		case <-ctx.Done():
		case <-tick.C:
		case <-d.rebuild.wake:
		}
	}
}

func (d *Daemon) rebuildSetState(state string) {
	d.rebuild.mu.Lock()
	d.rebuild.state = state
	d.rebuild.mu.Unlock()
}

// rebuildEnd ends a drain that the daemon's own stop interrupted.
func (d *Daemon) rebuildEnd(p registry.Project) {
	d.rebuild.mu.Lock()
	d.rebuild.active, d.rebuild.state = "", ""
	d.rebuild.mu.Unlock()
	d.runs.deps.Hold.Set(false)
	d.changed(p.Name, source.Runs)
}

// rebuildFail records the failure, keeps this daemon and releases the hold, so
// held runs go on with the old build. It is not tried again until main moves.
func (d *Daemon) rebuildFail(p registry.Project, head, reason string, lines []string) {
	d.rebuild.mu.Lock()
	if d.rebuild.failures == nil {
		d.rebuild.failures = map[string]rebuildFailure{}
	}
	d.rebuild.failures[p.Path] = rebuildFailure{head: head, reason: reason, lines: lines}
	d.rebuild.active, d.rebuild.state = "", ""
	d.rebuild.mu.Unlock()
	d.runs.deps.Hold.Set(false)
	fmt.Fprintf(os.Stderr, "daemon not rebuilt: %s: %s\n", reason, strings.Join(lines, "\n"))
	d.changed(p.Name, source.Runs)
}

// exitForRebuild ends the daemon, with start as its successor, when every run is
// held or at rest. It checks under the manager lock and closes admission under
// it, so no role starts between the check and the exit. It reports whether the
// daemon is ending. The hold stays set: a held start must not go on while the
// runs are being joined.
func (d *Daemon) exitForRebuild(start func(context.Context) error) bool {
	d.runs.mu.Lock()
	if d.runs.closed {
		d.runs.mu.Unlock()
		return true
	}
	if _, _, idle := d.drainStateLocked(); !idle {
		d.runs.mu.Unlock()
		return false
	}
	// Only a role actually waiting at the hold gets automatic recovery. Owner
	// pauses, queued runs and approval waits retain their existing behavior.
	for id, m := range d.runs.runs {
		if m.engine == nil || !m.engine.HeldForRebuild() {
			continue
		}
		if err := m.engine.MarkRebuildResume(); err != nil {
			fmt.Fprintf(os.Stderr, "mark rebuild resume %s: %v\n", id, err)
			// A missing marker leaves this run at the ordinary interrupted
			// pause. It must not stall the rebuild or every other held run.
		}
	}
	d.runs.closed = true
	d.runs.mu.Unlock()
	d.rebuild.mu.Lock()
	d.rebuild.successor = start
	stop := d.rebuild.stop
	d.rebuild.mu.Unlock()
	if stop != nil {
		stop()
	}
	return true
}

// startSuccessor starts the rebuilt daemon, once Run has joined its runs and
// before it releases the lock: the successor retries the lock for two seconds.
func (d *Daemon) startSuccessor() {
	d.rebuild.mu.Lock()
	start := d.rebuild.successor
	d.rebuild.successor = nil
	d.rebuild.mu.Unlock()
	if start == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "start the rebuilt daemon: %v\n", err)
	}
}

// drainStateLocked counts what a drain waits for: waiting is the roles, checks
// and other jobs that still run, held the runs and spec checks that wait at the
// hold, and idle is true when nothing still runs. A queued run, a pause and an
// approval wait count for nothing: none has a role. A run counts as held only
// when a role start waits at the hold and none of its roles or checks runs. The
// caller holds the manager lock.
func (d *Daemon) drainStateLocked() (waiting, held int, idle bool) {
	hold := d.runs.deps.Hold
	specHeld := min(hold.SpecWaiting(), d.runs.specJobs)
	held += specHeld
	waiting += d.runs.specJobs - specHeld
	counted := map[string]bool{}
	for id, m := range d.runs.runs {
		if !m.busy || m.recoveryFailed {
			continue
		}
		counted[id] = true
		if m.engine != nil && m.engine.HeldForRebuild() {
			held++
			continue
		}
		waiting++
		if m.engine != nil {
			waiting += max(0, len(m.engine.Working())-1)
		}
	}
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		return waiting + 1, held, false // as activeRunsLocked: keep the daemon when the run state cannot be read
	}
	for _, r := range stored {
		rec := r.Snapshot()
		if counted[rec.ID] {
			continue
		}
		if m := d.runs.runs[rec.ID]; m != nil && m.recoveryFailed {
			continue
		}
		switch rec.State {
		case runrec.StateCommitted, runrec.StateStopped, runrec.StatePaused, runrec.StateAwaitingApproval, runrec.StateQueued:
		default:
			waiting++
		}
	}
	return waiting, held, waiting == 0
}

// rebuildStatus is the project's rebuild for a status: the drain that runs, or
// the build that failed, or nil.
func (d *Daemon) rebuildStatus(p registry.Project) *RebuildStatus {
	d.rebuild.mu.Lock()
	active, state := d.rebuild.active, d.rebuild.state
	fail, failed := d.rebuild.failures[p.Path]
	d.rebuild.mu.Unlock()
	if active == p.Path && active != "" {
		d.runs.mu.Lock()
		waiting, held, _ := d.drainStateLocked()
		d.runs.mu.Unlock()
		message := "rebuild pending: building"
		if state != RebuildBuilding {
			message = fmt.Sprintf("rebuild pending: waiting for %d roles", waiting)
			if waiting == 1 {
				message = "rebuild pending: waiting for 1 role"
			}
		}
		return &RebuildStatus{State: state, Waiting: waiting, Held: held, Message: message}
	}
	if failed {
		return &RebuildStatus{State: RebuildFailed, Message: "daemon not rebuilt: " + fail.reason, Output: fail.lines}
	}
	return nil
}

// StaleBuild reports a differing or unknown daemon build.
func StaleBuild(own, theirs string) bool {
	if own == "" {
		own = "dev"
	}
	return theirs == "" || theirs == "unknown" || theirs != own
}
