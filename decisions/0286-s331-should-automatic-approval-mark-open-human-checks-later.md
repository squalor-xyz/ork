---
{
  "id": 286,
  "title": "S331: should automatic approval mark open human checks later?",
  "status": "decided",
  "key": "landing.human-check-mark",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S331 reviewer round 1, 2026-11-11: the slice note said 'not marked later' (orchestrator's wording); adj 281 says checks never block and go to human-checks-todo.md.",
  "options": [
    {
      "label": "mark-later",
      "summary": "set each open check to later, record it in the approval note, and append the checks to human-checks-todo.md"
    },
    {
      "label": "todo-only",
      "summary": "leave the checks open on the card, land, record them only in the todo file"
    }
  ],
  "recommendation": {
    "label": "mark-later",
    "reasoning": "Matches adj 281 and the slice tests; the orchestrator does both by hand today."
  },
  "decision": "mark-later",
  "rationale": "Owner picked Mark later + todo (recommended); the earlier 'not marked later' slice note was the orchestrator's wording.",
  "refs": [
    "S331"
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
