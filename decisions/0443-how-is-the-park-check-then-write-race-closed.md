---
{
  "id": 443,
  "title": "How is the park check-then-write race closed?",
  "status": "decided",
  "key": "daemon.park-race",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S232 spec question, 2026-11-17.",
  "options": [
    {
      "label": "recheck-undo",
      "summary": "re-check after the write and unpark if a run started"
    },
    {
      "label": "hold-lock",
      "summary": "hold run admission across the write"
    }
  ],
  "recommendation": {
    "label": "recheck-undo",
    "reasoning": "drafter's recommendation from the S232 verification"
  },
  "decision": "recheck-undo",
  "rationale": "Owner chose 'Both recommended' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S232"
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
