---
{
  "id": 564,
  "title": "How does the spec check carry the Check-command finding to clients?",
  "status": "decided",
  "key": "spec.check-command-finding",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S563 spec draft owner-needed 1.",
  "options": [
    {
      "label": "findings",
      "summary": "a findings list on the item's spec check result; Ready false, no questions, no role runs"
    },
    {
      "label": "findings-role",
      "summary": "same field, role still runs"
    },
    {
      "label": "check-error",
      "summary": "spec.check returns a refused error for the batch"
    }
  ],
  "recommendation": {
    "label": "findings",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "findings",
  "rationale": "Owner chose 'Findings field' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S563"
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
