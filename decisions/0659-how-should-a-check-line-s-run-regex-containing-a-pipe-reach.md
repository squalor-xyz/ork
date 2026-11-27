---
{
  "id": 659,
  "title": "How should a Check line's -run regex containing a pipe reach role briefs?",
  "status": "decided",
  "key": "brief.check-run-pipe",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S526 spec check.",
  "options": [
    {
      "label": "quote-in-brief",
      "summary": "when ork renders a Check line into a brief it wraps an unquoted -run pattern containing | in single quotes"
    },
    {
      "label": "split",
      "summary": "one command per test"
    },
    {
      "label": "lint-only",
      "summary": "lint only"
    },
    {
      "label": "retire",
      "summary": "retire"
    }
  ],
  "recommendation": {
    "label": "quote-in-brief",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "quote-in-brief",
  "rationale": "Owner chose 'Quote in brief' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S526"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
