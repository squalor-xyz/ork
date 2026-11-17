---
{
  "id": 422,
  "title": "How does a retry brief every hung sibling?",
  "status": "decided",
  "key": "engine.hung-sibling-brief",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S227 spec question, 2026-11-17.",
  "options": [
    {
      "label": "role-record",
      "summary": "each role reads its lost-process note from its role record"
    },
    {
      "label": "pending-roles",
      "summary": "a new Pending.Roles field"
    },
    {
      "label": "leave",
      "summary": "retire"
    }
  ],
  "recommendation": {
    "label": "role-record",
    "reasoning": "drafter's recommendation from the S227 verification"
  },
  "decision": "role-record",
  "rationale": "Owner chose 'From role record (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S227"
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
