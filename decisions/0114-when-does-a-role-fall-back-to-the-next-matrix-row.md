---
{
  "id": 114,
  "title": "When does a role fall back to the next matrix row?",
  "status": "superseded",
  "key": "vendor.fallback-trigger",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S49 is specified as fall back on a rate limit or error, which conflicts with record 23.",
  "options": [
    {
      "label": "rate-limit-only",
      "summary": "Fall back on a rate limit only; resume only a session from the same vendor and model, otherwise start fresh; the claude adapter must classify rate limits"
    }
  ],
  "recommendation": {
    "label": "rate-limit-only",
    "reasoning": "Owner answer 2026-10-20 in the roadmap review."
  },
  "decision": "rate-limit-only",
  "rationale": "Owner 2026-10-20: matches docs/design.md:392-396 and keeps record 23 (unclassified exits stop for the owner).",
  "superseded_by": 156,
  "refs": [
    "S49"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    },
    {
      "date": "2026-10-23",
      "event": "supersede",
      "note": "by 156"
    }
  ]
}
---
