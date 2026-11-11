---
{
  "id": 304,
  "title": "Does ork retry stage a deleted-by-us slice JSON whose done/ copy exists?",
  "status": "decided",
  "key": "landing.retry-stage-deleted",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "stage",
      "summary": "Retry stages the absent path as deleted when the item's done/ copy exists"
    },
    {
      "label": "leave",
      "summary": "Leave retry as it is"
    }
  ],
  "recommendation": {
    "label": "stage",
    "reasoning": "the hand fix then needs no git rm"
  },
  "decision": "stage",
  "rationale": "Owner picked retry stages it",
  "refs": [
    "S334"
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
