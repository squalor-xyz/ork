---
{
  "id": 601,
  "title": "How should ork stop concurrent runs from colliding on main's tracking commits, merges and fast-forwards?",
  "status": "decided",
  "key": "landing.main-write-serialization",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S614: three runs paused on such races on 2026-11-25/26.",
  "options": [
    {
      "label": "lock-and-retry",
      "summary": "a cross-run file lock around every ork write to main, plus one automatic retry for blockers from writes outside ork's steps"
    },
    {
      "label": "lock",
      "summary": "lock only"
    },
    {
      "label": "retry",
      "summary": "retry only"
    }
  ],
  "recommendation": {
    "label": "lock-and-retry",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "lock-and-retry",
  "rationale": "Owner chose 'Lock + one retry' in an interactive question, relay session 8d711f67, 2026-11-25.",
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
