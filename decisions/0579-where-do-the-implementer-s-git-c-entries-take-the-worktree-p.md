---
{
  "id": 579,
  "title": "Where do the implementer's git -C entries take the worktree path?",
  "status": "decided",
  "key": "launch.git-c-pin-point",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S586 spec draft.",
  "options": [
    {
      "label": "pin-at-launch",
      "summary": "registry and presets keep git -C AGENTS.md CHANGELOG.md CLAUDE.md LICENSE NOTICE PROJECT.md README.md assets build-in-home-dir.sh build-only-in-home-dir.sh cmd decisions docs go.mod go.sum human-checks-todo.md internal landed-changed run-in-home-dir.sh spike testdata as a template; rolePermission pins the resolved list with the run's worktree"
    },
    {
      "label": "preset-takes-worktree",
      "summary": "ImplementerPreset pins the built-in default only"
    },
    {
      "label": "placeholder-token",
      "summary": "registry stores a {worktree} token"
    }
  ],
  "recommendation": {
    "label": "pin-at-launch",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "pin-at-launch",
  "rationale": "Owner chose 'Pin at launch' in an interactive question, relay session 8d711f67, 2026-11-25.",
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
