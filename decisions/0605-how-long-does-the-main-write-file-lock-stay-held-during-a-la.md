---
{
  "id": 605,
  "title": "How long does the main-write file lock stay held during a landing?",
  "status": "decided",
  "key": "landing.main-write-lock-scope",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S614 spec draft.",
  "options": [
    {
      "label": "whole-landing",
      "summary": "held for all of land() and around the run-start tracking commit; ork tracking commit is not locked"
    },
    {
      "label": "narrow",
      "summary": "each git write only"
    },
    {
      "label": "plus-tracking-commit",
      "summary": "also lock ork tracking commit"
    }
  ],
  "recommendation": {
    "label": "whole-landing",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "whole-landing",
  "rationale": "Owner chose 'Whole landing' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S614"
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
