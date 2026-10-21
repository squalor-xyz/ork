---
{
  "id": 133,
  "title": "Does a launch-time lineup or budget edit persist beyond that run?",
  "status": "decided",
  "key": "lineup.override-scope",
  "asked_by": "claude",
  "decided_by": "Jon R",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "The role matrix is slicer's (docs/boundaries.md:14, record 24). Budgets default from the registry (record 46).",
  "options": [
    {
      "label": "run-only",
      "summary": "Kept in that run's run.json only; the matrix and the registry are unchanged"
    },
    {
      "label": "write-matrix",
      "summary": "Also written back with slicer relay-roles set (and the registry for budgets)"
    }
  ],
  "recommendation": {
    "label": "run-only",
    "reasoning": "ork does not own the matrix; a one-off choice should not change later launches."
  },
  "decision": "run-only",
  "rationale": "Owner accepted the recommendation (2026-10-20).",
  "refs": [
    "S80",
    "S83"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
