---
{
  "id": 684,
  "title": "One check row per pipeline line, or one per stage?",
  "status": "decided",
  "key": "checks.pipeline-rows",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S524 spec draft.",
  "options": [
    {
      "label": "one-row",
      "summary": "the row's command is the whole line; exit and ok from the last stage; log holds every stage's stderr and the last stage's stdout"
    },
    {
      "label": "per-stage",
      "summary": "one row per stage"
    }
  ],
  "recommendation": {
    "label": "one-row",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "one-row",
  "rationale": "Owner chose 'One row per line' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S524"
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
