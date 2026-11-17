---
{
  "id": 404,
  "title": "Does the gate fail a slice with no landed-changed/ID.md entry?",
  "status": "decided",
  "key": "gate.changelog-entry",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S332 spec question, 2026-11-17.",
  "options": [
    {
      "label": "gate-checks",
      "summary": "the gate brief names the missing entry and fails, when the project uses the folder"
    },
    {
      "label": "no-check",
      "summary": "rely on slicer check and review"
    }
  ],
  "recommendation": {
    "label": "gate-checks",
    "reasoning": "a slice cannot land without its changelog entry"
  },
  "decision": "gate-checks",
  "rationale": "Owner chose 'Gate checks it (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S332"
  ],
  "history": [
    {
      "date": "2026-11-17",
      "event": "ask"
    },
    {
      "date": "2026-11-17",
      "event": "decide"
    }
  ]
}
---
