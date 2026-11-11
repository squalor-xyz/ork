---
{
  "id": 308,
  "title": "What does a local role's brief add?",
  "status": "decided",
  "key": "roles.local-brief-reads",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "targeted-reads",
      "summary": "Read in ranges, never a whole file over about 500 lines, keep reasoning brief"
    },
    {
      "label": "none",
      "summary": "No local-specific text"
    }
  ],
  "recommendation": {
    "label": "targeted-reads",
    "reasoning": "two compactions took 27 of 45 min after whole-file reads in the S304 trial"
  },
  "decision": "targeted-reads",
  "rationale": "Owner agreed to targeted reads in local briefs",
  "refs": [
    "S343",
    "S304"
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
