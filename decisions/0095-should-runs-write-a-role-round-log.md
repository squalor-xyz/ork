---
{
  "id": 95,
  "title": "Should runs write a <role>-<round>.log?",
  "status": "decided",
  "key": "state.role-log",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "docs/state.md promises it; runs never wrote it.",
  "options": [
    {
      "label": "drop",
      "summary": "remove the promise from docs"
    },
    {
      "label": "write",
      "summary": "engine writes the raw role output log"
    }
  ],
  "recommendation": {
    "label": "drop"
  },
  "decision": "drop",
  "rationale": "Owner 2026-10-20: remove the log promise from docs/state.md; no log is written.",
  "refs": [
    "S23"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
