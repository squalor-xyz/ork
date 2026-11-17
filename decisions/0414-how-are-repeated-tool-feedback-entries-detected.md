---
{
  "id": 414,
  "title": "How are repeated tool feedback entries detected?",
  "status": "decided",
  "key": "feedback.dedupe",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S320 spec question, 2026-11-17.",
  "options": [
    {
      "label": "stable-key",
      "summary": "hash of tool, kind and normalized proposal used as the slicer item key or adj key"
    },
    {
      "label": "ork-store",
      "summary": "an ork-owned seen list"
    },
    {
      "label": "per-run",
      "summary": "within one run only"
    }
  ],
  "recommendation": {
    "label": "stable-key",
    "reasoning": "drafter's recommendation from the S320 spec draft"
  },
  "decision": "stable-key",
  "rationale": "Owner chose 'Stable key (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S320"
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
