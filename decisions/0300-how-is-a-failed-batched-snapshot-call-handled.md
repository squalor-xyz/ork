---
{
  "id": 300,
  "title": "How is a failed batched snapshot call handled?",
  "status": "decided",
  "key": "poll.snapshot-errors",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "fallback-per-part",
      "summary": "A failed call falls back to per-part calls for that tick; one project's error marks only its parts failed"
    },
    {
      "label": "fail-all",
      "summary": "A failed call fails every project's parts for that tick"
    }
  ],
  "recommendation": {
    "label": "fallback-per-part",
    "reasoning": "keeps today's freshness when snapshot breaks"
  },
  "decision": "fallback-per-part",
  "rationale": "Owner picked fall back per-part",
  "refs": [
    "S335"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
