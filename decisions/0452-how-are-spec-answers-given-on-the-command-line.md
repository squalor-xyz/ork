---
{
  "id": 452,
  "title": "How are spec answers given on the command line?",
  "status": "decided",
  "key": "cli.spec-answers",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "Automation slice question (owner rule: deterministic work in code), 2026-11-17.",
  "options": [
    {
      "label": "labels-refuse-pipes",
      "summary": "KEY=LABEL case-insensitive, numbers still work; non-terminal stdin refused with exit 3; keyless stays #N"
    },
    {
      "label": "labels-slug",
      "summary": "plus a title slug key"
    },
    {
      "label": "refuse-only",
      "summary": "no labels"
    }
  ],
  "recommendation": {
    "label": "labels-refuse-pipes",
    "reasoning": "drafter's recommendation, automation slices"
  },
  "decision": "labels-refuse-pipes",
  "rationale": "Owner chose 'Labels + refuse pipes (Rec)' in an interactive question, relay session 8d711f67, 2026-11-17.",
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
