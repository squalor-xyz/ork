---
{
  "id": 639,
  "title": "Which slicer subcommands may a reviewer run with --root set to its own temp dir?",
  "status": "decided",
  "key": "roles.read-only-temp-slicer-writes",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S527 spec draft.",
  "options": [
    {
      "label": "fixed-list",
      "summary": "init, add, set, note, done and relay-roles, each ending exactly --root TMPDIR"
    },
    {
      "label": "matrix-only",
      "summary": "init and relay-roles"
    },
    {
      "label": "any",
      "summary": "any subcommand"
    }
  ],
  "recommendation": {
    "label": "fixed-list",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "fixed-list",
  "rationale": "Owner chose 'Fixed list' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S527"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
