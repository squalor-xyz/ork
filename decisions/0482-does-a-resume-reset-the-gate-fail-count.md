---
{
  "id": 482,
  "title": "Does a resume reset the gate FAIL count?",
  "status": "decided",
  "key": "run.resume-gate-fails",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "internal/engine/states.go:999-1001; slicer s453 stopped at its first FAIL after a resume.",
  "options": [
    {
      "label": "reset",
      "summary": "Reset the gate FAIL count to 0"
    },
    {
      "label": "keep",
      "summary": "Keep it and say how many FAILs remain"
    }
  ],
  "recommendation": {
    "label": "reset",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "reset",
  "rationale": "Owner chose 'Reset to 0 (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S349"
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
