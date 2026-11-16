---
{
  "id": 376,
  "title": "What does the restart brief say about the read record?",
  "status": "decided",
  "key": "roles.local-read-log-reuse",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S371 spec check question.",
  "options": [
    {
      "label": "do-not-reread",
      "summary": "'You already read these; do not read them again unless you are about to edit that range.'"
    },
    {
      "label": "list-only",
      "summary": "the list with no instruction"
    }
  ],
  "recommendation": {
    "label": "do-not-reread",
    "reasoning": "S304 trial 5 reread runs.go 7-12 times per session"
  },
  "decision": "do-not-reread",
  "rationale": "Owner chose 'Do not reread (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S371"
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
