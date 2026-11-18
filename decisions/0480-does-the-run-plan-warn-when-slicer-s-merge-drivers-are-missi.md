---
{
  "id": 480,
  "title": "Does the run plan warn when slicer's merge drivers are missing?",
  "status": "decided",
  "key": "plan.merge-driver-warning",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "MissingMergeDrivers (internal/engine/setup.go:19) is never called from the plan; adj 288 turned down refusing the start.",
  "options": [
    {
      "label": "warn",
      "summary": "One warning line in the plan, the ork doctor text; no refusal"
    },
    {
      "label": "doctor",
      "summary": "Doctor and pause only; close S348"
    },
    {
      "label": "refuse",
      "summary": "Refuse the start"
    }
  ],
  "recommendation": {
    "label": "warn",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "warn",
  "rationale": "Owner chose 'One plan warning (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S348"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
