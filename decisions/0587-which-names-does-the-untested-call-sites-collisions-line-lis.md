---
{
  "id": 587,
  "title": "Which names does the Untested call sites collisions line list?",
  "status": "decided",
  "key": "untestedcallsites.collisions",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S587 spec draft.",
  "options": [
    {
      "label": "declared-more-than-once",
      "summary": "a reached call-site function name declared by more than one function in the worktree"
    },
    {
      "label": "selector-only",
      "summary": "names reached only through selector calls on an unknown receiver"
    },
    {
      "label": "every-hidden",
      "summary": "every reached name that hid a finding"
    }
  ],
  "recommendation": {
    "label": "declared-more-than-once",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "declared-more-than-once",
  "rationale": "Owner chose 'Declared more than once' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S587"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
