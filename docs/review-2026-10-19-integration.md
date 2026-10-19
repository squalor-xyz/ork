# Integration review: caching state and working with slicer and adj, 2026-10-19

The owner asked: "should we implement or add some better way to cache state /
better integrate slicer and adj tools?" This report answers that question,
checks the roadmap against the answer, and lists what ork, slicer and adj
would each change. Note added on landing: "S14" in this report means the
proposed cache item; slicer gave S14 to the Charm v2 port filed after the
report was written, so the cache item takes the next free ID when filed.
Nothing here is decided. Everything ork, slicer or adj
would add is *proposed* until an adj record decides it.

Evidence is cited as `repo/path:line` for the sibling repos, as a path for
this repo, or as measured command output. Code in `.worktrees/s03` and
`.worktrees/s11` is in-flight, uncommitted work and may still change.
Facts and judgment are kept apart: each section says which is which.

## Summary

**The answer, in short:**

- **Polling the CLIs is correct and safe, but it costs more than we
  thought.** Under the load of running relays, one `slicer` call takes
  0.4 to 1.4 s and about 0.5 to 0.7 s of CPU. The 0.1 s figure behind
  `dash.refresh` is a quiet-machine number. Most of the cost is Python
  startup and imports, paid once per call. adj calls take 13 to 36 ms and
  are not a problem.
- **Reads never contend with writers.** slicer and adj readers take no
  lock, so polling never sees `locked` and never slows a role. There is one
  real gap: a lock-free read can catch a write halfway and fail as
  `corrupt` for one tick.
- **Yes, add a cache, but only in memory in the daemon.** It should answer
  every client from one shared copy, never run the same call twice at once,
  refresh each part on its own schedule, and keep the last good value when
  a call fails. No cache files on disk, no file watching, no reading of
  `.slicer/` or `decisions/`.
- **The biggest saving has to come from slicer: fewer Python starts.**
  One `slicer snapshot --json` call per refresh, ideally covering several
  projects, would replace three calls per project. Measured: one
  `slicer list -r` run read 17 projects for 1.84 s of CPU. Four separate
  `slicer list` calls cost 2.19 s. A long-running `slicer serve` would
  remove the startup cost entirely, but it is large and can wait.
- **Fix four integration seams now:** adj has no JSON error object; adj
  IDs collide across parallel branches; ork's exact contract pin will
  refuse slicer's next contract bump (s382 moves v7 to v8); and render
  files go stale after merges.

**Top recommendations, ranked:**

| # | What | Where | Cost | Roadmap |
|---|---|---|---|---|
| R1 | In-memory snapshot cache in the daemon: shared, deduplicated, last good value kept on error, at most 4 CLI processes at once | ork | S | new item S14, after S03, before S13 |
| R2 | A refresh schedule per part: items and runs on `dash.refresh`; goals and decided records every 5 min, on a HEAD change, on tab open and on `r` | ork | S | note on S11, done in S14 |
| R3 | Treat a failed read as stale, not blank: keep the last value, retry once on `corrupt`, `locked` or a timeout | ork | S | note on S11, done in S14 |
| R4 | Ask slicer for `slicer snapshot --json`: several `--root`s, chosen parts, and a revision per part | slicer | M | ork adopts it when shipped (new item) |
| R5 | Contract version negotiation: an accepted set of versions instead of an exact pin, and s378 says what each bump changed | ork + slicer | S | before S04 |
| R6 | Ask adj for slicer's error object on `--json` failures, and for an ID floor that counts sibling worktrees, both before adj 1.0 | adj | S each | none in ork |
| R7 | Record every write ork makes in `run.json` (`writes[]`) | ork | S | S04 |
| R8 | Render staleness after merges: slicer regenerates render files after a merge, or reports them stale; ork's landing view shows the fix | slicer + ork | S | S09 |
| R9 | Later, only if R1 to R4 measure short: a long-running `slicer serve --stdio` with change events | slicer | L | after v1 |

Not recommended: file watching, importing adj's Go packages, ork cache files
on disk, `slicer log --since` as a change signal, and git pathspecs over
`.slicer/`. Section 5 says why.

## 1. Terms

- **Polling:** asking the tool again on a timer, whether or not anything
  changed.
- **Snapshot:** ork's view of one project: items, goals, active relay runs,
  open and decided adj records (`internal/source/source.go:34-41`).
- **Part:** one field of a snapshot, filled by one CLI call.
- **Cache:** the last answer kept in memory, served to clients without a
  new call.
- **TTL (time to live):** how old a cached part may get before it is
  refreshed.
- **Single-flight (deduplication):** if a call for a part is already
  running, a second request waits for that call instead of starting
  another.
- **Change token (revision):** a short value that changes whenever the
  data changes, so a reader can skip work when it has not.
- **Push / event stream:** the tool tells ork that something changed,
  instead of ork asking.
- **Error envelope:** the JSON error object a CLI prints on failure.
  slicer prints `{"error":{"code","message","command"}}`.
- **Single-flight, TTL and push** are general techniques, not features of
  any of the three tools today.

## 2. What happens today (facts)

```
                      every 5 s (visible) / 30 s (others)       dash.refresh
  TUI (S11, in flight) ----------------------------------+
                                                         v
                                   source.Direct.Snapshot(project)
                                   |  S02 on main: one call after another
                                   |  S11 worktree: all at once
          +-----------+------------+--------------+-------------+
          v           v            v              v             v
     slicer list  slicer goals  slicer relay-run  adj inbox   adj list
     --json       --json        list --active     --json      --status decided
     --lean                     --json --lean
     (Python)     (Python)      (Python)          (Go)        (Go)
          \           |            /                 \          /
           .slicer/ in the checkout     ledger:       decisions/
           + sibling worktrees'         <git common   (adj store)
             .slicer/ (claims)          dir>/slicer/
                                        relay-runs.jsonl

  After S03 + S13:
  TUI ---socket---> daemon "snapshot" ---> source.Direct.Snapshot   (per request, no cache)
  ork status -----> daemon "status"   ---> Snapshot for every project at once
```

- `Direct.Snapshot` runs 5 calls one after another on main
  (`internal/source/source.go:59-69`), each with a 10 s deadline
  (`internal/clirun/clirun.go:20`). S11's worktree runs them at the same
  time (`.worktrees/s11/internal/source/source.go`, uncommitted diff).
- S11's poller measures each interval from when the last call returned
  and never starts a key twice (`.worktrees/s11/internal/poll/poll.go:54-79`).
- S03's daemon calls `Source.Snapshot` on every `snapshot` request and on
  every `status` request, for every project, with no cache
  (`.worktrees/s03/internal/daemon/handlers.go:119-137`, `:139-168`).
- S03's socket protocol is request and response only, answered in order
  (`.worktrees/s03/internal/ipc/ipc.go:3-7`). There is no message the
  daemon can send first.
- slicer readers take no lock: "Read-only commands need no lock"
  (`slicer/src/slicer/cli.py:4445-4447`); `-r` also takes none
  (`cli.py:4288`); ledger readers read without a lock and skip a torn last
  line (`slicer/src/slicer/relayrun.py:274`, `:502`).
- slicer writers take `.slicer/lock` for the whole command, waiting up to
  5 s, then fail with code `locked` (`slicer/src/slicer/store.py:50`,
  `:160-195`, `:201-221`). Each checkout has its own `.slicer/lock`, so a
  role writing in its worktree and ork writing in the main checkout never
  wait on each other. Ledger appends in a git repo take only
  `relay-runs.lock` beside the shared ledger (`slicer/src/slicer/ops.py:3434-3456`).
- A slicer mutation spans several files (`store.py:204-205`). A lock-free
  reader that loads while a slice file moves fails with `corrupt`: "slice
  file is gone; it was moved or removed after the roadmap loaded"
  (`store.py:285-290`). This race was read from source, not reproduced.
- `slicer list` also reads sibling worktrees: the `wt:s11` claim in
  `slicer --root ork list` comes from the s11 worktree (`store.py:761-775`,
  `:798-809`). The data behind one call is spread over the main checkout,
  each worktree and the git common dir. Only slicer knows that layout.
- adj takes its lock only to allocate an ID (`adjudicate/internal/store/store.go:65-88`,
  `:282-308`). Reads take none.
- adj on `--json` failure prints plain text on stderr and nothing on
  stdout. Measured, on a directory with no adj repo:
  `adj list --json --root <empty>` gave exit 1, empty stdout, and stderr
  `adj list: no repo root found: ...`. slicer in the same case gave exit 2
  and `{"error":{"code":"state","message":"no .slicer/ found ...","command":"list"}}`.

## 3. Measurements (facts)

**Method.** A Python stdlib script in the scratch directory ran each
read-only call once to warm up, then 5 times, and recorded wall time and
the child's CPU time (user + system, from `getrusage`). Machine: 8 CPUs,
macOS, slicer 1.15.0 on the venv's Python 3.14, adj 0.1.0, git. Load
average was 6.2 to 9.6 during the runs, because the S03 and S11 relays
were running. That is the load ork will normally see: it polls while
slices run.

**Per call** (median wall ms / worst wall ms / median CPU ms):

| Call | slicer | adjudicate | DataBall | agents | ork |
|---|---|---|---|---|---|
| `slicer --version` (startup only) | 937 / 1926 / 426 | 627 / 730 / 469 | 586 / 638 / 482 | 437 / 455 / 367 | 456 / 541 / 407 |
| `slicer list --json --lean` | 1208 / 1543 / 631 | 778 / 887 / 571 | 685 / 789 / 585 | 633 / 847 / 435 | 663 / 812 / 554 |
| `slicer show ID --json --lean` | 629 / 662 / 509 | 640 / 761 / 524 | 513 / 570 / 443 | 632 / 914 / 447 | 640 / 984 / 530 |
| `slicer goals --json` | 1057 / 1390 / 579 | 661 / 700 / 534 | 730 / 1053 / 560 | 508 / 552 / 428 | 587 / 864 / 502 |
| `slicer relay-run list --json --lean --active` | 1187 / 1427 / 707 | 1091 / 1847 / 697 | 780 / 917 / 629 | 584 / 792 / 477 | 756 / 1410 / 603 |
| `slicer relay-run list --json --lean` (all runs) | 1388 / 1740 / 704 | 1027 / 1074 / 678 | 744 / 1549 / 613 | 604 / 931 / 507 | 951 / 1276 / 629 |
| `slicer status --json` | 806 / 1004 / 519 | 787 / 1219 / 598 | 1195 / 1885 / 506 | 561 / 581 / 490 | 659 / 734 / 563 |
| `slicer log --since T --json` | 723 / 1282 / 556 | 827 / 1132 / 593 | 765 / 922 / 446 | 701 / 844 / 440 | 606 / 651 / 511 |
| `adj inbox --json` | - | 18 / 23 / 12 | - | - | 31 / 37 / 20 |
| `adj list --json` | - | 22 / 25 / 15 | - | - | 36 / 40 / 19 |
| `adj list --json --status decided` | - | 22 / 23 / 15 | - | - | 28 / 47 / 21 |
| `git rev-parse HEAD` | 26 / 30 / 14 | 24 / 26 / 17 | 23 / 31 / 16 | 21 / 23 / 16 | 25 / 26 / 19 |
| `git status --porcelain` | 44 / 48 / 31 | 44 / 47 / 31 | 37 / 46 / 27 | 33 / 44 / 24 | 34 / 36 / 27 |

Every call exited 0. Of the registered projects, only adjudicate has
`.adjudicate.json`. ork itself is not in the seeded registry
(`internal/registry/seed.go:13`) and is measured for comparison.

**One whole refresh** (median wall s / worst wall s / median CPU s):

| Refresh | Result |
|---|---|
| adjudicate snapshot (3 slicer + 2 adj calls), one after another | 1.91 / 2.34 / 1.58 |
| adjudicate snapshot, all at once | 1.29 / 1.78 / 1.58 |
| slicer snapshot (3 slicer calls), one after another | 2.69 / 3.10 / 1.67 |
| slicer snapshot, all at once | 1.32 / 1.92 / 1.75 |
| all 4 registered projects, 14 calls all at once | 3.80 / 4.12 / 7.13 |
| all 4 registered projects, 4 calls at a time | 3.31 / 5.40 / 6.85 |
| `slicer list` for the 4 projects, all at once | 0.92 / 2.29 / 2.19 |
| `slicer list -r --json --lean --root ~/code/squalor-xyz` (finds 17 projects) | 2.04 / 2.25 / 1.84 |
| `slicer status -r --json --root ~/code/squalor-xyz` (17 projects) | 2.29 / 2.47 / 1.87 |

**Where slicer's time goes.** The venv's Python starts bare in 0.09 to
0.11 s. `import slicer.cli` takes 0.39 to 0.52 s; `-X importtime` reports
283 ms in total for `slicer.cli`, with `slicer.check` at 181 ms and
`slicer.ops` at 74 ms. So about 0.4 s of every call is startup, before any
work.

**This matches slicer's own numbers.** slicer s310 (retired) measured
`list --json --lean` at about 124 ms on a quiet machine with Python 3.12,
and 650 to 850 ms under load (`slicer --root slicer show s310`, notes). The
0.1 s in `dash.refresh` (adj record 52, Context) and in the earlier review
(`docs/review-2026-10-18.md:162-170`) is the quiet-machine number.

**What `dash.refresh` costs as decided** (estimate built from the medians
above): the visible project uses about 1.7 s of CPU per refresh, every
5 s plus about 1.3 s of call time, which is about 0.27 of one core. Three
other projects at 1.7 s each every 30 s add about 0.16. In total that is
about 0.43 of one core, about 5% of this machine. It means about 46 Python
processes a minute, for as long as a TUI is open. Two TUIs, or a script
running `ork status` (7.1 s of CPU each time on S03's handler), multiply
this, because S03 has no cache.

## 4. Is polling good enough? (judgment)

- **Correctness: yes.** Reads take no lock, so polling cannot block a
  role or be blocked by one. The data is fresh within one interval.
- **Latency: yes.** 1.3 s for a project, under 4 s for all four, inside
  the 5 s and 30 s intervals. "Nothing blocks" (`docs/dashboard.md`)
  holds, because S11 shows pending rows.
- **Cost: acceptable for one TUI, wasteful beyond that.** About 5% of the
  machine is not a crisis. But it is spent while four slices run tests on
  the same CPUs, and it grows with every client.
- **Robustness: one gap.** A lock-free read can fail with `corrupt` for
  one tick while a writer moves files. The dashboard must not turn that
  into a red error or an empty list.

So the answer is: keep polling, make it cheaper and shared (R1 to R3),
and move the real saving into slicer (R4).

## 5. Options for change detection and caching

### (a) Keep polling; add concurrency and an in-memory cache in the daemon

- **What:** a `source.Cached` wrapper in the daemon (*proposed*). One
  entry per project and part. Single-flight. A TTL per part. The last
  good value is kept when a call fails. A limit of 4 CLI processes at
  once. Every client (TUIs and `ork status`) reads the cache.
- **Pros:** standard library only; no boundary question; cuts the cost
  from N clients to one; `ork status` answers at once; the TUI stays
  simple.
- **Cons:** saves nothing for a single TUI unless the schedule per part
  (R2) comes with it; the cache is lost when the daemon exits (a cold
  start costs one refresh, about 1 to 4 s).
- **Cost:** S. **Boundary:** clean. This is the "everything else read
  live" of `state.files`, held in memory for seconds.

### (b) Cheap change tokens

Facts found in the source:

- slicer prints no revision, generation or mtime in its JSON.
  `slicer list --json` is a bare array of items (keys: `claim`,
  `depends_on`, `fields`, `has_slice`, `id`, `in_work_elsewhere`,
  `note_records`, `notes`, `short_title`, `status`, `title`), so it has no
  place for a token without a breaking change.
- adj prints none either. Its JSON is an object (`{"records": [...]}`),
  and readers must ignore unknown keys (`adjudicate/docs/json.md:6-8`), so
  adj could add one without breaking anyone.
- slicer's own TUI uses `(path, mtime_ns, size)` of its log file as a
  cache key (`slicer/src/slicer/tui.py:481-502`). slicer may do this to its
  own files; ork may not.

The candidates:

| Token | Covers | Misses | Boundary | Verdict |
|---|---|---|---|---|
| `git rev-parse HEAD` (21 to 26 ms) | commits, merges, landings | uncommitted edits, sibling worktree claims, the ledger (untracked) | clean: git refs, not tracking files | use only to trigger the slow parts (R2) and landing detection (S09) |
| `git log -1 -- .slicer`, `git status -- .slicer` | committed or dirty tracking files | sibling claims, the ledger, a dirty-to-dirty change | grey: searching tracking files through git | do not use |
| `slicer log --since T` | most slicer mutations | merged entries keep their old timestamps; 1 s resolution; the ledger | clean | do not use: it costs a full Python start (0.6 to 0.8 s), as much as `list` |
| stat of `.slicer/` files | everything slicer writes, if ork knew every path | needs slicer's private layout (siblings, git common dir, fallback ledgers) | violates `TRACKING_RULE` in spirit; adj record 52 already called it "close to slicer's tracking rule" | do not use |
| a `revision` printed by slicer (*proposed*) | whatever slicer says | nothing | clean | good, but each check still pays Python startup (about 0.4 s of CPU), so it saves the work, not the start; best inside `snapshot` (R4) |

### (c) File-system watching

- **Facts:** Go's standard library has no watcher, but `syscall` on darwin
  exposes `Kqueue` and `Kevent` (`go doc syscall.Kqueue` with
  `GOOS=darwin`). Linux would need inotify separately. fsnotify would be a
  new dependency, which `stack.go-deps` does not allow.
- **Judgment:** kqueue needs an open file descriptor on each watched
  directory: the main `.slicer/`, every worktree's `.slicer/`, the git
  common dir's `slicer/` and each fallback ledger. That is opening
  tracking files and depending on slicer's private layout, which is
  exactly what the tracking rule forbids. slicer also writes by atomic
  rename, so file watches go stale and directory watches are needed.
- **Verdict:** not in ork. If watching is ever worth it, it belongs inside
  slicer, behind (d).

### (d) Upstream features

- `slicer snapshot --json` with several `--root`s (*proposed*, R4): one
  Python start per refresh instead of three per project. Measured analog:
  `list -r` covered 17 projects for 1.84 s of CPU, about 0.08 s per
  project after startup.
- A `revision` per part and `--if-changed REV` (*proposed*): slicer skips
  loading and printing when nothing changed. Small gain on its own, see
  (b).
- `slicer serve --stdio` or `slicer watch --json` (*proposed*, R9): one
  long-running process; ork sends JSON-line requests and gets answers and
  change events. Startup is paid once. slicer would watch or stat its own
  files, which is its right. Size L for slicer: reload rules, sibling
  worktrees and the ledger all need care.
- `slicer status -r` and `list -r` exist today, but they walk a directory
  tree (17 projects under `~/code/squalor-xyz`, measured). They cannot take
  ork's list of projects, so ork should not depend on them.
- adj: `adj repos` and `adj inbox --all` are on adj main
  (`adjudicate/internal/commands/repos.go`, `inbox.go:52-92`), not in the
  installed 0.1.0 (measured: `adj inbox --all` exits 2, "flag provided
  but not defined: -all"; `adj repos` is an unknown command). `--all`
  reads adj's own registry at `$XDG_CONFIG_HOME/adjudicate/config.json`
  (`adjudicate/internal/userconfig/config.go:37`), a second list of
  projects beside ork's `projects.json`. Per-project calls cost 20 to
  36 ms, so ork gains nothing from `--all` and should keep calling with
  `--root`.

### (e) A Go library: importing adj

- **Facts:** every adj package is under `internal/` (`adjudicate/internal/`
  holds `adr cli commands lint model query render store userconfig`). Go
  allows importing `internal/` packages only from within the same module
  tree (`go doc cmd/go`, "Code in or below a directory named internal is
  importable only by code that shares the same import path above the
  internal directory"). So ork cannot import adj today. Licenses would
  allow it: adj is GPL-3.0-or-later (`adjudicate/LICENSE`,
  `cmd/adj/main.go:1`), like ork. slicer is Python and Apache-2.0
  (`slicer/pyproject.toml:11`), so embedding it in Go is not possible.
- **Judgment:** adj would have to publish a public package. ork would
  then read `decisions/` through code compiled into ork, which may differ
  from the installed `adj` (shapes may change in any 0.x minor release,
  `adjudicate/docs/json.md:15-19`). Two versions of the store rules could
  then write one store. The gain is about 20 ms a call.
- **Verdict:** no.

### (f) ork cache files on disk

- **Facts:** `state.files` chose `registry-runs` over the option
  `snapshot` ("Also a collated snapshot of every project's roadmap and
  decisions", adj record 9). PROJECT.md non-goals say "No second copy of
  project state".
- **Judgment:** a disk cache helps only the first screen after the daemon
  starts, which costs one refresh (1 to 4 s). It can show state that is
  minutes or days old.
- **Verdict:** no. It would also reverse a decided record.

### Comparison

```
option                          CPU saved      freshness   boundary         cost  lives in
------------------------------  -------------  ----------  ---------------  ----  ---------
(a) daemon cache + single-      clients: all   same        clean            S     ork
    flight + parts schedule     1 TUI: ~1/3
(b) git HEAD trigger            slow parts     n/a         clean (HEAD      S     ork
                                                           only)
(b) slicer revision token       little (still  same        clean            S     slicer
                                pays startup)
(c) kqueue / fsnotify watch     most           best        violates         M-L   ork
                                                           tracking rule
(d) slicer snapshot, many roots large (~2/3+)  same        clean            M     slicer
(d) slicer serve / watch        most           best        clean            L     slicer
(e) import adj packages         ~20 ms a call  same        grey (version    M     adj + ork
                                                           skew)
(f) disk cache                  first paint    stale       reverses         S     ork
                                only                       state.files
```

The CPU-saved figures for (a) and (d) are estimates from Section 3, not
measurements of built code.

## 6. Recommended architecture (proposed)

```
   TUI (one or more)            ork status --json / scripts
          \                           /
           \   unix socket, JSON lines   /
            \  requests + notifications (proposed)
             v                         v
   +----------------------------------------------------------+
   | ork daemon                                               |
   |                                                          |
   |  focus: which projects a TUI shows   (from the clients)  |
   |  scheduler: per part, per project    (R2 table)          |
   |  cache: (project, part) -> value, error, fetched_at,     |
   |                            hash, in_flight               |
   |  single-flight; at most 4 CLI processes at once          |
   |  run records for ork's own runs      (state.md)          |
   +----------------------------------------------------------+
        |                  |                     |
        v                  v                     v
   slicer CLI          adj CLI              git -C P rev-parse HEAD
   today: list,        inbox, list          (trigger for slow parts;
   goals, relay-run    (13-36 ms)           merge detection in S09)
   later: snapshot
   (R4), serve (R9)
```

**Schedule per part (R2, proposed amendment to `dash.refresh`):**

| Part | Call | Visible project | Other projects | Also refreshed |
|---|---|---|---|---|
| items | `slicer list --json --lean` | 5 s | 30 s | `r`, tab open |
| runs | `slicer relay-run list --active --json --lean` | 5 s | 30 s | `r` |
| open | `adj inbox --json` | 5 s | 30 s | `r`, after ork writes to adj |
| goals | `slicer goals --json` | 5 min | 5 min | HEAD change, tab open, `r` |
| decided | `adj list --json --status decided` | 5 min | 5 min | HEAD change, tab open, `r`, after ork writes to adj |

This takes the visible project from 3 slicer calls per tick to 2. From
the medians, that is about 0.29 of a core instead of 0.43 (estimate).
With `slicer snapshot` (R4) it would fall to about 0.16 (estimate,
assuming one call costs about one `list`).

**Runs ork launched itself** come from ork's own run records and role
processes (`launch.mode`, `state.files`). ork writes the ledger
(`ledger.write`), but it does not need to poll the ledger to know its own
state. The `runs` part matters for relays started elsewhere
(`dash.external-runs`) and for reconciling after a restart (`docs/design.md`,
Shape).

**Errors (R3).** A failed part keeps its last good value, marked stale with
its age and the error code. `corrupt`, `locked` and a timeout get one
quick retry. The error is shown verbatim, per `docs/dashboard.md`, on the
row and in the status line, but the list does not go blank.

### Sequence of one refresh (proposed)

```
 TUI                 daemon (cache)               clirun              slicer / adj
  |  focus adjudicate    |                          |                      |
  |--------------------->|                          |                      |
  |                      | tick: items, runs, open  |                      |
  |                      | due for adjudicate       |                      |
  |                      |--- start (limit 4) ----->|-- slicer list ------>|
  |                      |--- start --------------->|-- slicer relay-run ->|
  |                      |--- start --------------->|-- adj inbox -------->|
  | snapshot adjudicate  |                          |                      |
  |--------------------->|                          |                      |
  |<-- cached value, ----|  (no new call: the       |                      |
  |    age 4.1 s         |   flights are running)   |                      |
  |                      |<------------------------ stdout, exit 0 --------|
  |                      | hash(stdout) changed?    |                      |
  |                      |  items: yes; runs: no    |                      |
  |<-- notification: ----|                          |                      |
  |  snapshot.changed    |                          |                      |
  |  {adjudicate, items} |                          |                      |
  | snapshot adjudicate  |                          |                      |
  |--------------------->|                          |                      |
  |<-- new value, age 0 -|                          |                      |
```

Until notifications exist, the TUI simply asks the daemon every second or
two. That is cheap, because the daemon answers from the cache.

### Formats (all proposed)

**Socket notification** (a message with `method` and no `id`, beside S03's
request and response shapes; `.worktrees/s03/internal/ipc/ipc.go:26-46`):

```json
{"method": "snapshot.changed",
 "params": {"project": "adjudicate", "parts": ["items"], "at": "2026-10-19T16:12:05Z"}}
```

A client opts in with a request such as
`{"id": 7, "method": "subscribe", "params": {"focus": ["adjudicate"]}}`.

**`ork status --json`, each project with the age of each part:**

```json
{"projects": [
  {"name": "adjudicate", "needs_you": 1,
   "parts": {
     "items":   {"age_ms": 2100, "stale": false},
     "goals":   {"age_ms": 180000, "stale": false},
     "runs":    {"age_ms": 2100, "stale": false},
     "open":    {"age_ms": 2100, "stale": false},
     "decided": {"age_ms": 180000, "stale": true,
                 "error": {"code": "timeout", "message": "adj list ... timed out after 10s"}}
   }}
]}
```

`--fresh` would wait for a new refresh instead of returning the cached
values.

**`slicer snapshot --json` (a slicer feature request, R4):**

```json
{
  "snapshot": 1,
  "slicer": "1.16.0",
  "contract": 8,
  "projects": [
    {
      "root": "/Users/jon/code/squalor-xyz/adjudicate",
      "revision": "9f2c41e0",
      "parts": {
        "items":  {"revision": "a1", "value": ["...rows as list --json --lean..."]},
        "goals":  {"revision": "b7", "value": {"...": "as goals --json"}},
        "runs":   {"revision": "c3", "value": {"runs": ["...as relay-run list --active --lean..."]}},
        "status": {"revision": "a1", "value": {"next": "S39", "blocked": []}},
        "health": {"value": {"render_stale": false, "merge_in_progress": false}}
      },
      "error": null
    },
    {
      "root": "/Users/jon/code/squalor-xyz/missing",
      "parts": {},
      "error": {"code": "state", "message": "no .slicer/ found ...", "command": "snapshot"}
    }
  ]
}
```

Flags: `--root` may repeat; `--parts items,runs` picks parts;
`--if-changed REV` replaces an unchanged part's `value` with
`"unchanged": true`. A failed project fills its own `error` and the rest
still print, as `-r` does today (`cli.py:4282-4289`).

**`slicer serve --stdio` (later, R9)**, using the same line shape as ork's
socket:

```
-> {"id": 1, "method": "snapshot", "params": {"roots": ["/.../adjudicate"], "parts": ["items", "runs"]}}
<- {"id": 1, "result": {"snapshot": 1, "projects": [ ... ]}}
-> {"id": 2, "method": "subscribe", "params": {"roots": ["/.../adjudicate"]}}
<- {"id": 2, "result": {"ok": true}}
<- {"method": "changed", "params": {"root": "/.../adjudicate", "parts": ["items"], "revision": "a2"}}
```

**adj error object (a feature request, R6),** the same shape as slicer's:

```json
{"error": {"code": "no_repo", "message": "no repo root found: ...", "command": "list"}}
```

**`run.json` writes (R7),** added to the shape in `docs/state.md`:

```json
"writes": [
  {"at": "2026-10-19T16:12:05Z", "tool": "slicer", "root": "/.../adjudicate/.worktrees/S18",
   "argv": ["relay-run", "begin", "S18", "--worktree", "/.../S18", "--json"],
   "exit": 0, "code": null, "ms": 640},
  {"at": "2026-10-19T16:40:11Z", "tool": "slicer", "root": "/.../S18",
   "argv": ["note", "S18", "--text", "Review FAIL: ..."],
   "exit": 3, "code": "locked", "retry": 1, "ms": 5100},
  {"at": "2026-10-19T17:02:40Z", "tool": "git", "root": "/.../S18",
   "argv": ["commit", "-m", "S18: ..."], "exit": 0, "code": null, "ms": 120}
]
```

## 7. Better integration, beyond caching

| Seam | Today (facts) | Recommendation | Owner |
|---|---|---|---|
| One-call snapshots | 3 slicer calls per project per refresh; `-r` walks a tree (Section 3) | `slicer snapshot` with repeated `--root` (R4) | slicer |
| Event feed | none in slicer or adj; slicer's own TUI polls its board on a timer (`slicer/src/slicer/tui.py:977-983`, `BOARD_REFRESH_TICKS = 8` at `:1524`) | none now; `slicer serve` later (R9). Inside ork, push from the daemon to its clients (R1) | slicer later; ork now |
| Contract JSON | `ai relay --json` is `{contract, relay}`; s378 adds Order steps, report fields, outcomes, stop rules (`slicer --root slicer show s378`) | s378 also lists which parts each version changed. ork validates the structure it uses | slicer (s378) |
| Version negotiation | ork pins v7 and refuses any other (`boundary.contract`, `docs/boundaries.md:36-38`). `RELAY_CONTRACT = 7` (`slicer/src/slicer/ai.py:157`); s382 changes it to 8 and touches only the orchestrator's waiting rule and Order steps 3 and 4 (`slicer --root slicer show s382`, Files) | an accepted set of versions in ork, reviewed at each bump; `ork doctor` warns on a newer version not yet reviewed and refuses an older one (R5). Also check `slicer --version` and `adj version` against minimums | ork, with slicer s378 |
| Error object | slicer: `{"error":{code,message,command}}`, exits 0 to 3 (`ai.py:41-51`, `errors.py:19-25`). adj: text on stderr, exit 1 or 2 (measured; `adj ai`, "Exit codes") | adj prints slicer's shape on `--json` failures, with stable codes, before adj 1.0 freezes its JSON (adj S39) (R6) | adj |
| IDs across parallel branches | slicer raises a new ID above every sibling worktree's counter (`cli.py:1153-1156`, `store.py:761-768`) and has an index merge driver for `next_id` conflicts (`slicer/src/slicer/verify.py:299-302`). adj: "Collisions between branches or clones are out of the lock's reach; lint finds them and renumber fixes them" (`adjudicate/internal/store/store.go:282-286`) | adj takes the same sibling-worktree floor when it allocates (R6). Until then ork, when it files owner questions (`owner.questions`), runs `adj ask` in the project's main checkout, one writer under adj's lock, never in slice worktrees | adj; ork for its own writes |
| Render staleness after merges | slicer's render merge driver keeps one side to avoid conflict markers (slicer S109, S118; `verify.py:299-310`); after a merge `slicer check` fails until `slicer render`. Both live ork worktrees modify `.slicer/render/ROADMAP.md` and `.html` (`git status` of s03 and s11) | slicer regenerates render files after a merge (for example a post-merge hook from `slicer setup-git`), or reports `render_stale` in `check --json` and in `snapshot`'s `health` (R8). ork's landing view (S09) shows the exact `slicer render` command | slicer; ork shows it |
| Recording ork's writes | design says "Show the command" (`docs/dashboard.md`, Principles) and the ledger records role steps (`ledger.write`); nothing records ork's own CLI writes | `writes[]` in `run.json` (R7): every slicer, adj and git write with argv, root, exit, code and retries | ork |
| Lock waits on ork's writes | 5 s default, `SLICER_LOCK_TIMEOUT` overrides (`store.py:47-50`); adj 5 s (`adjudicate/internal/store/store.go:69-71`) | retry `locked` with backoff, as PROJECT.md Risks says, and log each retry in `writes[]`. The risk is smaller than stated: roles write in their own worktree's `.slicer/lock`, not the main checkout's | ork |
| Consistent reads | lock-free readers can fail with `corrupt` while a writer moves files (`store.py:204-205`, `:285-290`) | ork retries once (R3). Optionally slicer reads under a shared lock, or retries a load once itself | ork now; slicer optional |
| Multi-project adj | `adj inbox --all` uses adj's own repo list | ork keeps per-project `--root` calls; no change asked of adj | ork |

## 8. Recommendations in detail

### R1. In-memory snapshot cache in the daemon

- **What:** `source.Cached` (*proposed*), implementing `source.Source`
  over `source.Direct`. One entry per project and part: value, error, the
  time it was fetched, a hash of stdout, and whether a call is in flight.
  Single-flight. At most 4 CLI processes at once, with the visible project
  first. `daemon.snapshot` and `daemon.status` read from it. A
  `subscribe` request and a `snapshot.changed` notification on the
  socket.
- **Why:** S03 runs the full snapshot on every request
  (`.worktrees/s03/internal/daemon/handlers.go:128`, `:158`). One
  `ork status` costs about 7 s of CPU (Section 3). Each extra client
  doubles the polling.
- **Cost:** S. Standard library only.
- **Where:** ork.
- **Owner decision:** `dash.cache` (new). Options: `none` (each request
  calls the CLIs, as S03 does now); `daemon-ttl` (the cache, with clients
  asking the daemon often); `daemon-push` (the cache plus notifications).
  Recommendation: `daemon-push`, with the notification half allowed to
  land in S07 if S14 runs long.
- **Roadmap:** a new item S14, "Daemon snapshot cache", size S, after S03
  and before S13. S13 then points the TUI at a daemon that already
  caches. S13 keeps size S.

### R2. A refresh schedule per part

- **What:** the table in Section 6. Goals and decided records refresh
  every 5 minutes, on a HEAD change of that project, on tab open and on
  `r`. Items, runs and open records keep the decided 5 s and 30 s.
- **Why:** goals and decided records change rarely. Dropping `goals` from
  the 5 s tick removes one Python start of three.
- **Cost:** S.
- **Where:** ork (S11's poller keys become project and part).
- **Owner decision:** amend `dash.refresh`. Options: `uniform` (as
  decided) or `per-part` (this table). Recommendation: `per-part`. A
  second question goes with it: `boundary.git-head` (new), whether ork may
  run `git -C P rev-parse HEAD` on a registered project as a trigger.
  Options: `head-only`, `none`, `pathspec` (`git log -- .slicer`).
  Recommendation: `head-only`. It reads git refs, not tracking files, and
  S09 needs it anyway.
- **Roadmap:** a note on S11 (in flight, do not widen it); S14 does it.

### R3. Stale, not blank, on a failed read

- **What:** keep the last good value on error; show it marked stale with
  its age and the verbatim error; retry once at once on `corrupt`,
  `locked` or a timeout.
- **Why:** reads are lock-free and can catch a write halfway
  (`store.py:285-290`). S11's spec says a failed call shows its error,
  which is right, but it should not also hide the rows.
- **Cost:** S.
- **Where:** ork.
- **Owner decision:** none beyond `dash.cache`; it follows "Errors
  verbatim" and "Nothing blocks".
- **Roadmap:** a note on S11; S14 does it in the cache.

### R4. `slicer snapshot --json`

- **What:** a slicer command that prints the parts a dashboard needs for
  one or more roots in one process. The shape is in Section 6.
- **Why:** startup is about 0.4 s of every slicer call under load. One
  process for several projects measured at about 0.08 s of CPU per project
  after startup (`list -r`, 17 projects, 1.84 s).
- **Cost:** M for slicer. ork's side is S: one adapter call, used when
  `slicer snapshot --help` succeeds, else the current calls.
- **Where:** slicer.
- **Owner decision:** file it in slicer: yes or no. Recommendation: yes,
  now, with the revision and `--if-changed` parts optional.
- **Roadmap:** a new ork item, "Use slicer snapshot when available",
  size S, after S14, filed when slicer accepts the request.

### R5. Contract version negotiation

- **What:** ork keeps a list of contract versions it has reviewed (for
  example 7 and 8). `ork doctor` and each wave launch refuse a version
  older than the list. A newer version not on the list gets a warning
  that names it, and the owner adds it to the list after review. s378's JSON says which parts each version changed, so ork
  can tell whether a bump touches what it implements.
- **Why:** s382 (open in slicer) bumps the contract from 7 to 8 for the
  orchestrator agent's waiting rule. ork has no orchestrator agent
  (`docs/boundaries.md`, The contract), but an exact pin would refuse
  every launch the day slicer ships it.
- **Cost:** S in ork; a few keys in s378 (M, already open).
- **Where:** ork, and slicer s378.
- **Owner decision:** amend `boundary.contract`. Options: `exact-pin`
  (now), `accepted-set` (this), `minimum` (accept anything newer).
  Recommendation: `accepted-set`.
- **Roadmap:** before S04, because S04 is the first item that launches.
  It can be part of S04, or a note on s378 now.

### R6. Two adj requests, before adj 1.0

- **What:** (1) on `--json`, a failure prints
  `{"error":{"code","message","command"}}` on stdout with stable codes;
  (2) `adj ask` and other creating commands allocate an ID above the
  highest ID in any sibling worktree's store, as slicer does.
- **Why:** (1) ork branches on codes, never on text
  (`docs/boundaries.md:51-55`), and today adj gives only text.
  (2) IDs collided between parallel branches and needed `adj renumber`;
  adj documents that its lock cannot prevent it (`store.go:282-286`).
  adj's next release is 1.0 (adj S39), which freezes the JSON.
- **Cost:** S each, for adj.
- **Where:** adj.
- **Owner decision:** file both in adjudicate's roadmap: yes or no.
  Recommendation: yes, both, before S39.
- **Roadmap:** no ork item. Until (2) lands, S04 runs `adj ask` for owner
  questions in the main checkout only.

### R7. Record ork's writes

- **What:** `writes[]` in `run.json` (shape in Section 6), and the Waves
  screen shows the last write for each slice.
- **Why:** after a crash or a `locked` retry, the owner and the restart
  reconcile need to know exactly what ork changed in slicer, adj and git.
  "Show the command" covers the screen, not the record.
- **Cost:** S.
- **Where:** ork.
- **Owner decision:** `state.writes` (new): `run-json` (this),
  `separate-log` (a `writes.jsonl` per run), or `none`. Recommendation:
  `run-json`; one directory per run already keeps it race-free.
- **Roadmap:** S04, when the first writes appear. Update `docs/state.md`.

### R8. Render staleness after merges

- **What:** slicer regenerates `render/` after a merge, or at least
  reports `render_stale` in `check --json` and in `snapshot`. ork's
  landing view shows "render stale: run `slicer render`" for a project
  where that is true.
- **Why:** the render merge driver keeps one side by design (slicer S109),
  so every merge of two branches that both rendered leaves stale files,
  and `slicer check` then fails.
- **Cost:** S for slicer; S for ork, inside S09.
- **Where:** slicer, and ork's S09.
- **Owner decision:** file it in slicer: yes or no. Recommendation: yes.
- **Roadmap:** add to S09's scope: merge detection also checks
  `slicer check --json` for render drift.

### R9. `slicer serve --stdio`, later

- **What:** the long-running process in Section 6.
- **Why:** it removes Python startup completely and gives true change
  events.
- **Cost:** L for slicer (reload rules, siblings, ledger), M for ork.
- **Where:** slicer.
- **Owner decision:** defer. Recommendation: decide after R1, R2 and R4
  are in and measured. If the polling cost is under about 0.1 of a core
  by then, do not ask for it.
- **Roadmap:** after v1, with the web front end, which would want push
  anyway.

## 9. The roadmap against these recommendations

Items as listed by `slicer --root ork list --all` and `slicer show`:

| Item | State | Change (proposed) |
|---|---|---|
| S11 TUI shell and Overview | in flight | Keep its scope. Add notes: the schedule per part (R2) and stale-not-blank (R3) arrive in S14; its poller keys may become project and part. Its concurrent `Snapshot` already answers S02's note. |
| S03 Daemon and CLI | in flight | Keep its scope. Add a note: the socket protocol should allow a message with `method` and no `id` (a notification), so S14 needs no protocol change. |
| S14 Daemon snapshot cache (new) | - | Add, size S, after S03, before S13: R1, R2, R3, `ork status` with ages. |
| S13 Wire binary and socket | spec'd | Order after S14. No scope change. Its human check gains: two TUIs open, the CLI calls per minute do not double. |
| S12 Roadmap and Decisions | spec'd | `show ID` on selection stays a direct call (it is one call, on demand). The "all projects" toggle merges per-project results from the cache and does not use `adj inbox --all`. |
| S04 Single-slice engine | not spec'd | Add: R5 (contract accepted set), R7 (`writes[]`), retry on `locked`, `adj ask` only in the main checkout until R6 lands, and ork's own runs from its run records, not from polling the ledger. |
| S05 Waves and caps | not spec'd | Add: the 4-process CLI limit (R1) is separate from the 4-slice cap; `plan-parallel` and `relay-roles recommend` are on-demand calls, not cached. |
| S06 Vendors and fallback | not spec'd | No change. |
| S07 Live role detail | not spec'd | Output tails come from ork's own role processes, never from slicer. `git diff --stat` per running slice is a git call (about 40 ms); schedule it with the cache. If S14 left out notifications, they land here. |
| S08 Architect, split, slivers | not spec'd | No change. |
| S09 Landing help | not spec'd | Add R8: render drift after merge, with the `slicer render` command. Use `git rev-parse HEAD` changes (R2) as the merge trigger. |
| New: use `slicer snapshot` | - | Add when slicer accepts R4, size S, after S14. |
| After v1: roadmap editing | PROJECT.md | Writes go through slicer commands; with R7 every one is recorded. The cache marks the part stale after each write. |
| After v1: decision editing | PROJECT.md | Needs R6 (adj error object) first, so ork can branch on codes. |
| After v1: web front end | PROJECT.md | Reads the daemon's cache and notifications; R9 if slicer offers it. |

Nothing needs to be dropped. Three documents need small fixes:

- `PROJECT.md` Risks has no line for polling cost. Add one: "slicer calls
  cost 0.4 to 1.4 s under load; mitigated by the daemon cache and the
  schedule per part (R1, R2), and upstream by `slicer snapshot` (R4)".
- `docs/boundaries.md:66` cites `adjudicate/AGENTS.md:455-456`. The
  tracking rule is now at `adjudicate/AGENTS.md:547-548`.
- `docs/boundaries.md:51-55` reads as if a state error exits 3. Measured:
  slicer with no `.slicer/` exits 2 with code `state`; only the codes in
  `INTERNAL_CODES` (`corrupt`, `locked`, `io`, `config`, `schema_too_new`,
  `slicer/src/slicer/errors.py:19-25`) exit 3. ork already branches on the
  code, so no code changes.

## 10. Feature requests to file (proposed, not filed)

**slicer:**

1. **"`slicer snapshot --json`: what a dashboard needs, for several roots
   in one process."** Why: each call starts Python, about 0.4 s of CPU
   under load (Section 3). Shape: Section 6; repeated `--root`, `--parts`,
   a `revision` per part, `--if-changed`, an `error` per project.
2. **"Render files are regenerated, or reported stale, after a merge."**
   Why: the render merge driver keeps one side, so `slicer check` fails
   after most parallel merges. Shape: a post-merge hook from
   `slicer setup-git`, or `render_stale` in `check --json` and `snapshot`.
3. **"s378: say what each contract version changed."** Why: a consumer
   that implements part of the contract can then accept a bump that does
   not touch its part (s382). Shape: in `ai relay --json`, a `changes`
   array per version naming the changed parts (`setup`, `order`,
   `orchestrator`, `report`, `outcomes`, `stop`). Add it to s378 rather
   than filing it alone.
4. **"Lock-free reads can fail with `corrupt` during a write."** Why:
   `store.py:285-290` turns a file moved by a writer into `corrupt`.
   Shape: the load retries once, or reads take a shared lock. Size S,
   priority low; ork works around it (R3).
5. **Later: "`slicer serve --stdio`: long-running JSON-lines reads and
   change events."** Only if R1, R2 and R4 measure short.

**adj:**

1. **"JSON error object on `--json` failures."** Why: today a failure is
   text on stderr only (measured), so scripts must match words. Shape:
   slicer's `{"error":{"code","message","command"}}` on stdout, codes such
   as `no_repo`, `not_found`, `not_open`, `locked`, `usage`; exit codes
   unchanged. Before S39 (1.0).
2. **"New IDs clear every sibling worktree's highest ID."** Why: parallel
   branches minted the same ID and needed `adj renumber`
   (`store.go:282-286`). Shape: as slicer's `_id_floor` and
   `sibling_ids` (`slicer/src/slicer/cli.py:1153-1156`, `store.py:761-768`):
   read `git worktree list` and each sibling's store under the same lock.
   Before S39.

## 11. Owner decisions needed

| Key (proposed) | Question | Options | Recommendation |
|---|---|---|---|
| `dash.cache` (new) | Does the daemon cache snapshots for its clients? | `none`, `daemon-ttl`, `daemon-push` | `daemon-push` (R1) |
| `dash.refresh` (amend) | One schedule for all parts, or one per part? | `uniform`, `per-part` | `per-part` (R2) |
| `boundary.git-head` (new) | May ork use a project's `git rev-parse HEAD` as a refresh trigger? | `head-only`, `none`, `pathspec` | `head-only` |
| `boundary.contract` (amend) | Exact pin, accepted set, or minimum? | `exact-pin`, `accepted-set`, `minimum` | `accepted-set` (R5), before S04 and before slicer ships s382 |
| `state.writes` (new) | Where does ork record its own writes? | `run-json`, `separate-log`, `none` | `run-json` (R7) |
| `plan.s14` (new) | Add S14 "Daemon snapshot cache" after S03 and before S13? | `add-s14`, `fold-into-s13`, `later` | `add-s14` |
| Upstream (not an adj key) | Which requests to file | slicer 1 to 5, adj 1 and 2 (Section 10) | file slicer 1, 2 and 3 (into s378) and adj 1 and 2 now; slicer 4 at low priority; hold slicer 5 |

The rest needs no new decision: no disk cache (`state.files` already
says so), no file watching (`dash.refresh` context and `stack.go-deps`),
no import of adj packages (they are `internal/`).

## 12. What I could not verify

- **Quiet-machine timings.** Every number was taken at load average 6 to 9
  on 8 CPUs, while two relays ran. A quiet machine will be faster: slicer's
  s310 saw about 124 ms. I did not measure that here.
- **Python 3.14 versus 3.12.** s310 measured 49.6 ms of imports on Python
  3.12; I measured 283 ms on the venv's Python 3.14 under load. I cannot
  say how much of the difference is the version and how much is load.
- **Lock contention in practice.** I made no writes, so I did not measure
  a reader racing a writer, or a `locked` wait. The `corrupt` race and the
  lock behavior are read from source.
- **Whether every slicer mutation writes a log entry.** I found 20 distinct
  logged actions in 41 `_record` calls in `ops.py`, but did not check
  each mutating command.
- **The estimates** of CPU saved by R2 and R4 are arithmetic on the
  measured medians, not measurements of built code.
- **adj main versus 0.1.0.** `adj repos` and `inbox --all` were read in
  source (tag v0.2.0) and confirmed missing in the installed 0.1.0. Other
  differences between the two were not checked.
- **S03 and S11** are uncommitted, in-flight work; their code may change
  before it lands.
- **How many clients** (TUIs, scripts) the owner will run at once is not
  known; R1's value grows with that number.
