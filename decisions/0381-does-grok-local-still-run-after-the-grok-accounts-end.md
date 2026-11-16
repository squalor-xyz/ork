---
{
  "id": 381,
  "title": "Does grok-local still run after the grok accounts end?",
  "status": "decided",
  "key": "vendors.grok-local-after-cutover",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S374 spec check question.",
  "options": [
    {
      "label": "keep",
      "summary": "leave grok-local in the matrix; ork does not pass the cloud login to that launcher"
    },
    {
      "label": "retire",
      "summary": "treat grok-local as retired with grok"
    }
  ],
  "recommendation": {
    "label": "keep",
    "reasoning": "docs/boundaries.md gives the launcher its own credentials; ork vendor out grok does not mark grok-local"
  },
  "decision": "keep",
  "rationale": "Owner chose 'Keep it (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S374"
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
