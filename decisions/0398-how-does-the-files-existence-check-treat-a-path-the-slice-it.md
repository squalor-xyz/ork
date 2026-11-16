---
{
  "id": 398,
  "title": "How does the Files existence check treat a path the slice itself creates?",
  "status": "decided",
  "key": "roles.spec-draft-new-files",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S179 spec check question; first answered by the orchestrator's mistaken piped input, then confirmed by the owner.",
  "options": [
    {
      "label": "new-marker",
      "summary": "the draft marks a created file as '`path` (new)' and ork skips the existence check for it; any other missing path is reported with its section and the owner can still approve"
    }
  ],
  "recommendation": {
    "label": "new-marker"
  },
  "decision": "new-marker",
  "rationale": "Owner kept '(new) marker' in an interactive question, relay session 8d711f67, 2026-11-16, after the orchestrator reported its mistaken piped answer.",
  "refs": [
    "S179"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    }
  ]
}
---
