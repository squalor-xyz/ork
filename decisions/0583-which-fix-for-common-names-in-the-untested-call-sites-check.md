---
{
  "id": 583,
  "title": "Which fix for common names in the Untested call sites check?",
  "status": "decided",
  "key": "untestedcallsites.common-names",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S564 finding: t.Run marks every function named Run as reached.",
  "options": [
    {
      "label": "both",
      "summary": "skip selector calls on a *testing.T receiver, plus a collisions line for names it cannot disambiguate"
    },
    {
      "label": "ignore-t",
      "summary": "skip *testing.T selector calls only"
    },
    {
      "label": "list-collisions",
      "summary": "list possible false reached names only"
    }
  ],
  "recommendation": {
    "label": "both",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "both",
  "rationale": "Owner chose 'Both' in an interactive question, relay session 8d711f67, 2026-11-25.",
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
