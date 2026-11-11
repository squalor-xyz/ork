---
{
  "id": 195,
  "title": "What time limit does a local role get before ork falls back to the next row?",
  "status": "superseded",
  "key": "roles.local-timeout",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Owner interview 2026-11-09 before S301 lands.",
  "options": [
    {
      "label": "20-minutes",
      "summary": "Fall back after 20 minutes"
    },
    {
      "label": "45-minutes",
      "summary": "Fall back after 45 minutes"
    },
    {
      "label": "same-as-cloud",
      "summary": "No special local limit"
    }
  ],
  "recommendation": {
    "label": "20-minutes",
    "reasoning": "Probe: a 300-line review did not finish in 60 minutes; 20 minutes is where the Grok review was capped"
  },
  "decision": "45-minutes",
  "rationale": "Owner chose 45 minutes in the 2026-11-09 interview, over the recommended 20, to give slow local runs more time.",
  "superseded_by": 315,
  "refs": [
    "S301",
    "S303"
  ],
  "history": [
    {
      "date": "2026-11-09",
      "event": "ask"
    },
    {
      "date": "2026-11-09",
      "event": "decide"
    },
    {
      "date": "2026-11-11",
      "event": "supersede",
      "note": "by 315"
    }
  ]
}
---
