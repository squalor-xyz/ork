---
{
  "id": 447,
  "title": "Which fields on the role record hold the session's last reported total?",
  "status": "decided",
  "key": "usage.session-total-fields",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S380 spec check question, 2026-11-17.",
  "options": [
    {
      "label": "two-fields",
      "summary": "session_tokens and session_cost_usd, omitempty"
    },
    {
      "label": "object",
      "summary": "session_total {tokens, cost_usd}"
    },
    {
      "label": "derive",
      "summary": "no field; sum earlier rows"
    }
  ],
  "recommendation": {
    "label": "two-fields",
    "reasoning": "flat fields match tokens and cost_usd on RoleRecord"
  },
  "decision": "two-fields",
  "rationale": "Owner chose 'Two flat fields (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S380"
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
