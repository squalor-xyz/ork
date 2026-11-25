---
{
  "id": 577,
  "title": "What tighter form replaces git -C * SUBCMD* if the real matcher lets * span the subcommand?",
  "status": "decided",
  "key": "launch.read-only-git-c-form",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S575 finding: git -C * show* matches git -C /w commit -m show in ork's matcher model.",
  "options": [
    {
      "label": "pin-path-all",
      "summary": "LocalReadOnlyAllowlist and the implementer preset take the worktree path and emit Bash(git -C PATH SUBCMD*)"
    },
    {
      "label": "pin-path-read-only",
      "summary": "pin only read-only roles"
    },
    {
      "label": "probe-first",
      "summary": "probe only, decide later"
    },
    {
      "label": "drop-c",
      "summary": "keep only bare git SUBCMD* entries"
    }
  ],
  "recommendation": {
    "label": "pin-path-all",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "pin-path-all",
  "rationale": "Owner chose 'Pin path, all git -C' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S586"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
