# State

The files ork owns. Everything else is read live from slicer, adj and git
(`state.files`). File names and fields are *proposed*. Every file is JSON
(`state.format`). ork keeps the latest 100 finished runs
(`state.runs-retention`).

## What ork owns

| Data | File |
|---|---|
| Project registry and per-project settings | `~/.config/ork/projects.json` |
| UI preferences, free-form until v1 | `~/.config/ork/ui.json` |
| Daemon socket and pidfile (the pidfile is locked while a daemon lives) | `~/.local/state/ork/ork.sock`, `ork.pid` |
| One record per slice run | `~/.local/state/ork/runs/<run>/run.json` |
| Role reports and output logs | `~/.local/state/ork/runs/<run>/<role>-<round>.{md,log}` |
| Fixture candidates from non-ok vendor exits | `~/.local/state/ork/fixtures/<vendor>/` |

ork honors `XDG_CONFIG_HOME` and `XDG_STATE_HOME`. Nothing is written
inside a project except what git and slicer write for a slice: the
worktree, the branch, the commit, and the ledger and notes through slicer
commands.

## projects.json

```json
{
  "version": 2,
  "global_cap": 4,
  "daemon_idle_minutes": 10,
  "projects": [
    {
      "name": "DataBall",
      "path": "/Users/jon/code/squalor-xyz/DataBall",
      "adj": false,
      "cap": 2,
      "worktree_pattern": ".worktrees/{id}",
      "branch_pattern": "{id}",
      "check_command": "dotnet test",
      "check_minutes": 10,
      "environment_lines": ["zsh on darwin; no timeout command", "..."],
      "budgets": {"minutes": 120, "tokens": 2000000},
      "permissions": {
        "implementer": ["Edit", "Write", "Bash(dotnet test*)", "Bash(slicer *)"],
        "reviewer": "read-only"
      },
      "vendor_env": {"codex": {"VENDOR_CODEX_NETWORK": "1"}}
    }
  ]
}
```

Notes on the fields:
- `daemon_idle_minutes` is how long an idle daemon waits before it exits
  (`daemon.idle`). 0 or absent means 10. It is new in version 2.
- `check_command` is a fallback. ork prefers slicer's config key once
  slicer has one (`project.check-command`). The `check_minutes` value sets
  the stale flag and goes into briefs.
- `permissions` is the per-role allowlist (`launch.permissions`). Its exact
  syntax per vendor waits on the vendor spike (design.md).
- `vendor_env` is shown as an example. The DataBall entry reflects the
  agents repo's note that Codex needs network access for `dotnet test`
  (`agents/setup/lib/workflow-vendor.sh`).

## run.json

```json
{
  "version": 1,
  "id": "ork-20261018-161200-a1b2",
  "wave": "wave-20261018-161150-77aa",
  "project": "adjudicate",
  "item": "S18",
  "worktree": "/Users/jon/code/squalor-xyz/adjudicate/.worktrees/S18",
  "branch": "S18",
  "relay_run": "run-20261018-161205-c0de",
  "contract": 7,
  "state": "review",
  "round": 1,
  "lineup": {
    "implementer": [{"vendor": "claude", "model": "sonnet", "effort": "high"},
                    {"vendor": "codex", "model": "gpt-6.1-sol", "effort": "medium"}],
    "reviewer": [{"vendor": "claude", "model": "opus", "effort": "medium"}]
  },
  "roles": [
    {"role": "implementer", "round": 1, "vendor": "claude", "session": "...",
     "pgid": 12345, "step": "2", "started_at": "...", "ended_at": "...",
     "exit": "ok", "tokens": 41000, "cost_usd": 0.62, "report": "implementer-1.md"}
  ],
  "checks": [{"round": 1, "command": "go test ./...", "ok": true, "seconds": 58}],
  "budgets": {"minutes": 120, "tokens": 2000000},
  "approvals": [{"what": "wave", "at": "..."}],
  "questions": [],
  "writes": [
    {"at": "2026-10-19T16:12:05Z", "tool": "slicer", "root": "/.../adjudicate/.worktrees/S18",
     "argv": ["relay-run", "begin", "S18", "--worktree", "/.../S18", "--json"],
     "exit": 0, "code": null, "ms": 640},
    {"at": "2026-10-19T16:40:11Z", "tool": "slicer", "root": "/.../S18",
     "argv": ["note", "S18", "--text", "Review FAIL: ..."],
     "exit": 3, "code": "locked", "retry": 1, "ms": 5100}
  ],
  "commit": null
}
```

`state` and the role fields are ork's own record of the processes it
started. They are not a copy of slicer state. The ledger link is
`relay_run`.

`writes` lists every slicer, adj and git write ork makes for the run, with
its argv, root, exit, error code and retry count, so a restart or the
owner can see exactly what ork changed (`state.writes`, S04).

## Rules

- **Atomic writes:** write a temporary file in the same directory, then
  rename it.
- **One directory per run,** so parallel runs never write the same file.
- **Strict keys:** an unknown key in `projects.json` is an error naming it;
  new fields come with a version bump (`registry.unknown-keys`).
- **Zero means unset:** `global_cap`, `cap` and `check_minutes` of 0 read
  as absent, and the default applies (`registry.zero-values`).
- **Versioned files:** every file has a `version`, and ork refuses a
  version it does not know. `projects.json` is at version 2; ork still reads
  version 1 files, which lack `daemon_idle_minutes` and are strict about it
  (a version 1 file with that key is an error), and the next save writes
  version 2.
- **No secrets:** no tokens or credentials in any ork file. Raw vendor
  output saved as fixtures is scanned for key-like strings and redacted
  before it is saved (*proposed*).
- **Retention:** when more than 100 finished runs exist, the daemon removes
  the oldest run directories. It never removes a run whose branch is not
  landed, or a run that is still active.
