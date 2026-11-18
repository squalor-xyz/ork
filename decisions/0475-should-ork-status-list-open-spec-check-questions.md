---
{
  "id": 475,
  "title": "Should ork status list open spec-check questions?",
  "status": "decided",
  "key": "status.spec-questions",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "Open spec questions are missing from ork status; the daemon caches them (internal/daemon/speccheck.go:92).",
  "options": [
    {
      "label": "yes",
      "summary": "List them with keys and the --questions command; count under needs you"
    },
    {
      "label": "close",
      "summary": "Close S340 and leave it to S318"
    }
  ],
  "recommendation": {
    "label": "yes",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "yes",
  "rationale": "Owner chose 'Yes, small step now (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S340"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
