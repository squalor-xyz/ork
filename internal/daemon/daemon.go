// SPDX-License-Identifier: GPL-3.0-or-later

// Package daemon is the ork daemon: it owns the Unix socket, answers the
// requests of the TUI and the CLI, and exits when it has been idle long
// enough (daemon.lifetime, daemon.start, daemon.idle).
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/cache"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// socketProbeTimeout bounds the dial that tells a live socket from a stale one.
const socketProbeTimeout = 500 * time.Millisecond

// ErrAlreadyRunning is returned by Run when another daemon holds the pidfile.
var ErrAlreadyRunning = errors.New("ork daemon is already running")

// Paths are the daemon's files.
type Paths struct {
	Dir    string // holds the other two
	Socket string
	Pid    string
}

// PathsIn returns the daemon's files in dir.
func PathsIn(dir string) Paths {
	return Paths{Dir: dir, Socket: filepath.Join(dir, "ork.sock"), Pid: filepath.Join(dir, "ork.pid")}
}

// DefaultPaths returns the paths under $XDG_STATE_HOME/ork, else
// ~/.local/state/ork.
func DefaultPaths() (Paths, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return PathsIn(filepath.Join(dir, "ork")), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	return PathsIn(filepath.Join(home, ".local", "state", "ork")), nil
}

// Timer and Clock are the cache's; tests replace the clock.
type Timer = cache.Timer
type Clock = cache.Clock

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }

// Config configures a Daemon.
type Config struct {
	BuildID string       // build identity supplied by the executable; empty means dev
	Engine  *engine.Deps // nil uses the Claude adapter and the state directory

	Paths        Paths
	RegistryPath string         // projects.json; empty means registry.DefaultPath
	Source       source.Fetcher // nil means source.Direct over the real CLIs
	Clock        Clock          // nil means the real clock
	WaveRunning  func() bool    // true while a wave runs; nil means never; engine runs are checked separately
}

// Daemon is one daemon process.
type Daemon struct {
	runs  runManager
	cfg   Config
	ready chan struct{}
	regMu sync.Mutex   // serializes registry writes
	cache *cache.Cache // the project snapshots, shared by every client

	subsMu sync.Mutex
	focus  map[*ipc.Conn][]string // the projects each subscribed connection shows
}

// New returns a daemon for the config.
func New(cfg Config) *Daemon {
	if cfg.BuildID == "" {
		cfg.BuildID = "dev"
	}
	if cfg.Source == nil {
		cfg.Source = source.NewDirect(slicercli.Client{}, adjcli.Client{})
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	if cfg.WaveRunning == nil {
		cfg.WaveRunning = func() bool { return false }
	}
	d := &Daemon{cfg: cfg, ready: make(chan struct{}), focus: map[*ipc.Conn][]string{}}
	d.initRunManager()
	d.cache = cache.New(cache.Options{Clock: cfg.Clock, Fetcher: landingFetcher{Fetcher: cfg.Source, d: d}, OnChange: d.changed})
	return d
}

// Ready returns a channel that is closed once the daemon listens. Run closes
// it at most once, and not at all when it fails first.
func (d *Daemon) Ready() <-chan struct{} { return d.ready }

// Run serves until ctx ends or the daemon has been idle long enough, and
// returns nil for either. It returns ErrAlreadyRunning, wrapped with the
// running daemon's pid, when another daemon is live. A socket left by a dead
// daemon is removed and replaced. Run may be called once.
func (d *Daemon) Run(ctx context.Context) error {
	defer d.cache.Close()
	paths := d.cfg.Paths
	if err := ipc.MkdirPrivate(paths.Dir); err != nil {
		return err
	}
	lock, err := lockPidfile(paths.Pid)
	if err != nil {
		if errors.Is(err, ErrAlreadyRunning) {
			if b, rerr := os.ReadFile(paths.Pid); rerr == nil {
				if pid, perr := strconv.Atoi(strings.TrimSpace(string(b))); perr == nil {
					return fmt.Errorf("%w (pid %d, socket %s)", err, pid, paths.Socket)
				}
			}
		}
		return err
	}
	// The pidfile stays after exit: the lock, not its content, says whether a
	// daemon lives, and removing it would race with a daemon that is starting.
	defer lock.Close()
	// The lock is ours, so normally no live daemon owns a socket here and one
	// that is left is stale. But the lock lives with the pidfile's inode: if
	// the pidfile was deleted under a live daemon, its socket still answers.
	if info, err := os.Lstat(paths.Socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("%s exists and is not a socket", paths.Socket)
		}
		if c, err := net.DialTimeout("unix", paths.Socket, socketProbeTimeout); err == nil {
			c.Close()
			return fmt.Errorf("%w (the socket %s answers)", ErrAlreadyRunning, paths.Socket)
		}
		if err := os.Remove(paths.Socket); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Write the pid before shortening the file, so a reader never sees it empty.
	pid := []byte(strconv.Itoa(os.Getpid()) + "\n")
	if _, err := lock.WriteAt(pid, 0); err != nil {
		return err
	}
	if err := lock.Truncate(int64(len(pid))); err != nil {
		return err
	}
	l, err := ipc.Listen(paths.Socket)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := d.recoverRuns(ctx); err != nil {
		l.Close()
		return err
	}
	defer d.closeRuns()
	idle := &idleTimer{
		clock:       d.cfg.Clock,
		timeout:     d.idleTimeout,
		waveRunning: func() bool { return d.cfg.WaveRunning() || d.activeRuns() },
		onIdle:      cancel,
	}
	srv := &ipc.Server{Handler: d.handle, OnConnect: idle.connect, OnDisconnect: idle.disconnect,
		AfterReply: func(method string) {
			if method == MethodDaemonStop {
				cancel()
			}
		},
	}
	idle.start()
	close(d.ready)
	err = srv.Serve(ctx, l)
	idle.stop()
	return err
}

// idleTimeout reads daemon_idle_minutes from the registry, so an edit takes
// effect at the next arming. A missing or broken registry gives the default.
func (d *Daemon) idleTimeout() time.Duration {
	if f, err := registry.Load(d.registryPath()); err == nil {
		return f.DaemonIdle()
	}
	return (&registry.File{}).DaemonIdle()
}

// idleTimer runs the idle clock: it runs while no client is connected and no
// wave runs, and it fires onIdle when it runs out (daemon.idle).
type idleTimer struct {
	clock       Clock
	timeout     func() time.Duration
	waveRunning func() bool
	onIdle      func()

	mu      sync.Mutex
	clients int
	gen     int // identifies the current timer, so a stale one does nothing
	timer   Timer
	stopped bool
}

func (i *idleTimer) start() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.arm()
}

// arm starts a fresh timer. The caller holds mu.
func (i *idleTimer) arm() {
	i.disarm()
	gen := i.gen
	i.timer = i.clock.AfterFunc(i.timeout(), func() { i.fire(gen) })
}

// disarm cancels the timer. The caller holds mu.
func (i *idleTimer) disarm() {
	i.gen++
	if i.timer != nil {
		i.timer.Stop()
		i.timer = nil
	}
}

func (i *idleTimer) fire(gen int) {
	i.mu.Lock()
	if i.stopped || gen != i.gen || i.clients > 0 {
		i.mu.Unlock()
		return
	}
	if i.waveRunning() {
		i.arm()
		i.mu.Unlock()
		return
	}
	i.stopped = true
	i.mu.Unlock()
	i.onIdle()
}

func (i *idleTimer) connect() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.clients++
	i.disarm()
}

func (i *idleTimer) disconnect() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.clients--
	if i.clients == 0 && !i.stopped {
		i.arm()
	}
}

func (i *idleTimer) stop() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.stopped = true
	i.disarm()
}
