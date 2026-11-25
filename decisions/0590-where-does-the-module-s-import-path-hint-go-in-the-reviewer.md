---
{
  "id": 590,
  "title": "Where does the module's import-path hint go in the reviewer's probe-test recipe?",
  "status": "decided",
  "key": "roles.probe-import-paths",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "adj 576 tool feedback.",
  "options": [
    {
      "label": "gomod-in-brief",
      "summary": "the reviewer brief reads the worktree go.mod and lists its require paths"
    },
    {
      "label": "static-prompt",
      "summary": "a reviewer.md sentence to read go.mod"
    },
    {
      "label": "hardcode",
      "summary": "ork's charm paths in reviewer.md"
    }
  ],
  "recommendation": {
    "label": "gomod-in-brief",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "gomod-in-brief",
  "rationale": "Owner chose 'go.mod in the brief' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S589"
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
