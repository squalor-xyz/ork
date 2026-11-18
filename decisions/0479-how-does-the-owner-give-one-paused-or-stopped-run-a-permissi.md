---
{
  "id": 479,
  "title": "How does the owner give one paused or stopped run a permission it was denied?",
  "status": "decided",
  "key": "run.retry-allow",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "projects set --allow is refused while runs are unfinished (adj 279).",
  "options": [
    {
      "label": "retry-flag",
      "summary": "ork retry RUN --allow PATTERN, stored on that run only"
    },
    {
      "label": "live",
      "summary": "projects set --allow applies to unfinished runs; rebuild a paused engine on retry"
    },
    {
      "label": "both",
      "summary": "Both"
    }
  ],
  "recommendation": {
    "label": "retry-flag",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "retry-flag",
  "rationale": "Owner chose 'ork retry --allow (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S346"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
