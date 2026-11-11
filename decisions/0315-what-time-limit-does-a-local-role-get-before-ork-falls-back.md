---
{
  "id": 315,
  "title": "What time limit does a local role get before ork falls back to the next row?",
  "status": "decided",
  "key": "roles.local-timeout",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "owner chat answer, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "90-minutes",
      "summary": "Fall back after 90 minutes"
    },
    {
      "label": "45-minutes",
      "summary": "Keep 45 minutes"
    }
  ],
  "recommendation": {
    "label": "90-minutes",
    "reasoning": "S304 trial 1: two compactions took 27 of 45 min; with S343's targeted reads a local role may finish in 90"
  },
  "decision": "90-minutes",
  "rationale": "Owner agreed to the 90 minute local limit (item 3 of the S304 trial fixes)",
  "supersedes": [
    195
  ],
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
    },
    {
      "date": "2026-11-11",
      "event": "supersede",
      "note": "supersedes 195"
    }
  ]
}
---
