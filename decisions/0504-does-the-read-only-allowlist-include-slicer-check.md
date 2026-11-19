---
{
  "id": 504,
  "title": "Does the read-only allowlist include slicer check?",
  "status": "decided",
  "key": "roles.read-only-slicer-check",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "slicer check --root WT is denied for reviewers; the read-only list has slicer show, goals, list, deps and find (internal/engine/perm.go:23-33).",
  "options": [
    {
      "label": "add",
      "summary": "Add Bash(slicer check*) to the read-only allowlist"
    },
    {
      "label": "leave",
      "summary": "Leave it out"
    }
  ],
  "recommendation": {
    "label": "add",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "add",
  "rationale": "Owner chose 'Add slicer check (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "refs": [
    "S470"
  ],
  "history": [
    {
      "date": "2026-11-19",
      "event": "ask"
    },
    {
      "date": "2026-11-19",
      "event": "decide"
    }
  ]
}
---
